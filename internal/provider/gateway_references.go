package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
)

const gatewayReferenceURLTTL = 30 * time.Minute

type referenceURLStorage interface {
	PresignGetObject(ctx context.Context, key string, expires time.Duration) (storage.PresignedGetResult, error)
}

func (s *Service) hydrateGatewayImageReferences(ctx context.Context, organizationID string, references []GatewayImageReference) ([]GatewayImageReference, error) {
	if len(references) == 0 {
		return nil, nil
	}
	out := append([]GatewayImageReference(nil), references...)
	for index := range out {
		if strings.TrimSpace(out[index].URL) != "" {
			continue
		}
		storageKey := strings.TrimSpace(out[index].StorageKey)
		if storageKey == "" && strings.TrimSpace(out[index].ArtifactID) != "" {
			resolvedKey, _, err := s.referenceArtifactStorage(ctx, organizationID, out[index].ArtifactID)
			if err != nil {
				return nil, err
			}
			storageKey = resolvedKey
			out[index].StorageKey = resolvedKey
		}
		url, err := s.presignGatewayReference(ctx, storageKey)
		if err != nil {
			return nil, err
		}
		out[index].URL = url
	}
	return out, nil
}

func (s *Service) hydrateGatewayVideoReferences(ctx context.Context, organizationID string, references []GatewayVideoReference) ([]GatewayVideoReference, error) {
	if len(references) == 0 {
		return nil, nil
	}
	out := append([]GatewayVideoReference(nil), references...)
	for index := range out {
		if strings.TrimSpace(out[index].URL) != "" {
			continue
		}
		storageKey := strings.TrimSpace(out[index].StorageKey)
		mimeType := strings.TrimSpace(out[index].MimeType)
		if storageKey == "" && strings.TrimSpace(out[index].ArtifactID) != "" {
			resolvedKey, resolvedMimeType, err := s.referenceArtifactStorage(ctx, organizationID, out[index].ArtifactID)
			if err != nil {
				return nil, err
			}
			storageKey = resolvedKey
			out[index].StorageKey = resolvedKey
			if mimeType == "" {
				mimeType = resolvedMimeType
			}
		}
		if storageKey == "" && strings.TrimSpace(out[index].MediaFileID) != "" {
			resolvedKey, resolvedMimeType, err := s.referenceMediaFileStorage(ctx, organizationID, out[index].MediaFileID)
			if err != nil {
				return nil, err
			}
			storageKey = resolvedKey
			out[index].StorageKey = resolvedKey
			if mimeType == "" {
				mimeType = resolvedMimeType
			}
		}
		url, err := s.presignGatewayReference(ctx, storageKey)
		if err != nil {
			return nil, err
		}
		out[index].URL = url
		out[index].MimeType = mimeType
	}
	return out, nil
}

func (s *Service) referenceArtifactStorage(ctx context.Context, organizationID, artifactID string) (string, string, error) {
	var storageKey, mimeType string
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(storage_key, ''), COALESCE(mime_type, '')
		FROM artifacts
		WHERE organization_id::text = $1 AND id::text = $2
	`, organizationID, artifactID).Scan(&storageKey, &mimeType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("%w: reference artifact is not available", ErrValidation)
	}
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(storageKey), strings.TrimSpace(mimeType), nil
}

func (s *Service) referenceMediaFileStorage(ctx context.Context, organizationID, mediaFileID string) (string, string, error) {
	var storageKey, mimeType string
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(storage_key, ''), COALESCE(mime_type, '')
		FROM media_files
		WHERE organization_id::text = $1 AND id::text = $2
	`, organizationID, mediaFileID).Scan(&storageKey, &mimeType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("%w: reference media file is not available", ErrValidation)
	}
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(storageKey), strings.TrimSpace(mimeType), nil
}

func (s *Service) presignGatewayReference(ctx context.Context, storageKey string) (string, error) {
	storageKey = strings.TrimSpace(storageKey)
	if storageKey == "" {
		return "", fmt.Errorf("%w: reference requires a URL or stored media", ErrValidation)
	}
	presigner, ok := s.objectStorage.(referenceURLStorage)
	if !ok {
		return "", fmt.Errorf("%w: object storage cannot create provider reference URLs", ErrValidation)
	}
	result, err := presigner.PresignGetObject(ctx, storageKey, gatewayReferenceURLTTL)
	if err != nil {
		return "", fmt.Errorf("presign provider reference %q: %w", storageKey, err)
	}
	if strings.TrimSpace(result.URL) == "" {
		return "", fmt.Errorf("%w: object storage returned an empty provider reference URL", ErrValidation)
	}
	return result.URL, nil
}

func validateOutboundProviderReferenceURL(rawURL, mediaKind string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: provider %s reference URL is invalid", ErrValidation, mediaKind)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: provider %s reference URL must use http or https", ErrValidation, mediaKind)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_REFERENCE_URLS")), "true") {
		return nil
	}

	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	internalHost := host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") ||
		!strings.Contains(host, ".")
	if ip := net.ParseIP(host); ip != nil {
		internalHost = isPrivateProviderReferenceIP(ip)
	}
	if internalHost {
		return fmt.Errorf(
			"%w: provider %s reference URL must be externally reachable; configure S3_PUBLIC_ENDPOINT with a public object-storage URL",
			ErrValidation,
			mediaKind,
		)
	}
	return nil
}

func isPrivateProviderReferenceIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
