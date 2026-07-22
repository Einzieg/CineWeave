package workflows

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videocontracts"
	"github.com/jackc/pgx/v5"
)

func (a Activities) prepareVideoReferenceManifest(
	ctx context.Context,
	organizationID string,
	contractKey string,
	capabilitySnapshotHash string,
	references []provider.GatewayVideoReference,
) ([]provider.GatewayVideoReference, *videocontracts.ReferenceManifestV2, string, error) {
	prepared := append([]provider.GatewayVideoReference(nil), references...)
	for index := range prepared {
		reference := &prepared[index]
		if err := a.completeVideoReferenceProvenance(ctx, organizationID, index, reference); err != nil {
			return nil, nil, "", err
		}
	}
	sort.SliceStable(prepared, func(left, right int) bool {
		return videocontracts.CanonicalReferenceOrderLess(contractKey,
			videocontracts.ReferenceOrderKey{
				Role: prepared[left].Role, Required: prepared[left].Required,
				Priority: prepared[left].Priority, ReferenceKey: prepared[left].ReferenceKey,
			},
			videocontracts.ReferenceOrderKey{
				Role: prepared[right].Role, Required: prepared[right].Required,
				Priority: prepared[right].Priority, ReferenceKey: prepared[right].ReferenceKey,
			},
		)
	})
	items := make([]videocontracts.ReferenceManifestItemV2, 0, len(prepared))
	for index := range prepared {
		reference := &prepared[index]
		items = append(items, videocontracts.ReferenceManifestItemV2{
			Order: index, ReferenceKey: reference.ReferenceKey, Role: reference.Role,
			Required: reference.Required, Priority: reference.Priority,
			MediaType: videoReferenceMediaType(*reference), Semantics: reference.Semantics,
			SourceType: reference.SourceType, SourceID: reference.SourceID, SourceVersion: reference.SourceVersion,
			AssetID: reference.AssetID, ArtifactID: reference.ArtifactID, MediaFileID: reference.MediaFileID,
			ContentHash: reference.ContentHash, GeneratedAt: reference.GeneratedAt,
		})
	}
	manifest, manifestHash, err := videocontracts.BuildReferenceManifestV2(contractKey, capabilitySnapshotHash, items)
	if err != nil {
		return nil, nil, "", workflowError{Code: provider.CodeModelInputContractUnsupported, Message: "视频引用清单无效：" + err.Error()}
	}
	return prepared, &manifest, manifestHash, nil
}

func (a Activities) completeVideoReferenceProvenance(ctx context.Context, organizationID string, index int, reference *provider.GatewayVideoReference) error {
	if reference == nil {
		return workflowError{Code: provider.CodeModelInputContractUnsupported, Message: "视频引用不能为空"}
	}
	reference.ReferenceKey = strings.TrimSpace(reference.ReferenceKey)
	reference.Role = strings.ToLower(strings.TrimSpace(reference.Role))
	reference.Type = videoReferenceMediaType(*reference)
	reference.Semantics = strings.ToLower(strings.TrimSpace(reference.Semantics))
	reference.SourceType = strings.ToLower(strings.TrimSpace(reference.SourceType))
	reference.SourceID = strings.TrimSpace(reference.SourceID)
	reference.SourceVersion = strings.TrimSpace(reference.SourceVersion)
	reference.ContentHash = strings.TrimSpace(reference.ContentHash)
	if reference.ReferenceKey == "" {
		reference.ReferenceKey = fmt.Sprintf("execution:%03d:%s:%s", index, reference.Role, firstNonEmptyString(reference.ArtifactID, reference.MediaFileID))
	}
	if reference.SourceType == "" {
		switch {
		case reference.AssetID != "":
			reference.SourceType = "canonical_asset"
		case reference.ArtifactID != "":
			reference.SourceType = "artifact"
		default:
			reference.SourceType = "media_file"
		}
	}
	if reference.SourceID == "" {
		reference.SourceID = firstNonEmptyString(reference.AssetID, reference.ArtifactID, reference.MediaFileID)
	}
	if reference.ContentHash == "" {
		contentHash, err := a.videoReferenceContentHash(ctx, organizationID, *reference)
		if err != nil {
			return err
		}
		reference.ContentHash = contentHash
	}
	if reference.SourceVersion == "" {
		reference.SourceVersion = reference.ContentHash
	}
	return nil
}

func (a Activities) videoReferenceContentHash(ctx context.Context, organizationID string, reference provider.GatewayVideoReference) (string, error) {
	var contentHash string
	var err error
	switch {
	case strings.TrimSpace(reference.ArtifactID) != "":
		err = a.db.QueryRow(ctx, `
			SELECT COALESCE(content_hash, '')
			FROM artifacts
			WHERE organization_id = $1 AND id = $2
		`, organizationID, reference.ArtifactID).Scan(&contentHash)
	case strings.TrimSpace(reference.MediaFileID) != "":
		err = a.db.QueryRow(ctx, `
			SELECT COALESCE(checksum, '')
			FROM media_files
			WHERE organization_id = $1 AND id = $2
		`, organizationID, reference.MediaFileID).Scan(&contentHash)
	default:
		return "", workflowError{Code: provider.CodeModelInputContractUnsupported, Message: "视频引用缺少可追溯的 artifact 或 media file"}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", workflowError{Code: provider.CodeModelInputContractUnsupported, Message: "视频引用媒体不存在或不属于当前组织"}
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(contentHash) == "" {
		return "", workflowError{Code: provider.CodeModelInputContractUnsupported, Message: "视频引用媒体缺少内容哈希"}
	}
	return contentHash, nil
}

func videoReferenceMediaType(reference provider.GatewayVideoReference) string {
	typeName := strings.ToLower(strings.TrimSpace(reference.Type))
	switch {
	case typeName == "image" || strings.Contains(typeName, "image") || strings.Contains(typeName, "frame"):
		return "image"
	case typeName == "video" || strings.Contains(typeName, "video"):
		return "video"
	case typeName == "audio" || strings.Contains(typeName, "audio"):
		return "audio"
	}
	mimeType := strings.ToLower(strings.TrimSpace(reference.MimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	default:
		return typeName
	}
}
