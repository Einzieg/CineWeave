package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	maxGatewayImageReferenceBytes      int64 = 50 << 20
	maxGatewayImageReferenceTotalBytes int64 = 128 << 20
)

type gatewayImageReferenceCapabilities struct {
	SupportsReferences bool
	MaxReferences      int
	RequestModes       map[string]bool
}

func resolveGatewayImageQuality(requested string, capabilities []Capability) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		return "", nil
	}
	qualities := make([]string, 0, 4)
	for _, capability := range capabilities {
		qualities = append(qualities, stringsFromRawJSON(capability.QualityTiers)...)
		for _, raw := range []json.RawMessage{capability.InputLimits, capability.ProviderOptionsSchema} {
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
				continue
			}
			if nested, ok := decoded["xCapabilities"].(map[string]any); ok {
				decoded = nested
			}
			qualities = append(qualities, stringsFromAny(decoded["quality"])...)
		}
	}
	qualities = uniqueStrings(qualities)
	if len(qualities) == 0 {
		return requested, nil
	}
	supported := make(map[string]bool, len(qualities))
	for _, quality := range qualities {
		supported[strings.ToLower(strings.TrimSpace(quality))] = true
	}
	if supported[requested] {
		return requested, nil
	}
	aliases := map[string][]string{
		"standard": {"medium", "low", "auto"},
		"hd":       {"high", "medium"},
	}
	for _, candidate := range aliases[requested] {
		if supported[candidate] {
			return candidate, nil
		}
	}
	return "", &StandardErrorError{Standard: StandardError{
		Code:      CodeModelCapabilityUnavailable,
		Message:   fmt.Sprintf("selected image model does not support quality %s", requested),
		Retryable: false,
	}}
}

func selectGatewayImageReferences(capabilities []Capability, references []GatewayImageReference, requireEditMode bool) ([]GatewayImageReference, error) {
	if len(references) == 0 {
		return nil, nil
	}
	modelCapabilities := parseGatewayImageReferenceCapabilities(capabilities)
	if !modelCapabilities.SupportsReferences {
		return nil, &StandardErrorError{Standard: StandardError{
			Code:      CodeModelCapabilityUnavailable,
			Message:   "selected image model does not support reference images",
			Retryable: false,
		}}
	}
	if requireEditMode && len(modelCapabilities.RequestModes) > 0 && !modelCapabilities.RequestModes["images.edit"] {
		return nil, &StandardErrorError{Standard: StandardError{
			Code:      CodeModelCapabilityUnavailable,
			Message:   "selected image model does not support the images.edit request mode required for reference images",
			Retryable: false,
		}}
	}
	limit := modelCapabilities.MaxReferences
	if limit <= 0 {
		limit = 1
	}
	if limit > len(references) {
		limit = len(references)
	}
	return append([]GatewayImageReference(nil), references[:limit]...), nil
}

func parseGatewayImageReferenceCapabilities(capabilities []Capability) gatewayImageReferenceCapabilities {
	result := gatewayImageReferenceCapabilities{RequestModes: map[string]bool{}}
	for _, capability := range capabilities {
		for _, raw := range []json.RawMessage{capability.InputLimits, capability.ProviderOptionsSchema} {
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
				continue
			}
			if nested, ok := decoded["xCapabilities"].(map[string]any); ok {
				decoded = nested
			}
			if truthyBool(decoded["supportsReferences"]) || truthyBool(decoded["supportsReferenceImages"]) {
				result.SupportsReferences = true
			}
			if value := int(floatField(decoded["maxReferenceImages"], "maxReferenceImages")); value > 0 && (result.MaxReferences == 0 || value < result.MaxReferences) {
				result.MaxReferences = value
			}
			for _, mode := range stringsFromAny(decoded["requestModes"]) {
				result.RequestModes[strings.ToLower(strings.TrimSpace(mode))] = true
			}
		}
	}
	return result
}

func (s *Service) materializeOpenAICompatibleImageReferences(ctx context.Context, account Account, references []GatewayImageReference, timeout time.Duration) ([]openAICompatibleImageReference, error) {
	if len(references) == 0 {
		return nil, nil
	}
	reader, canReadStoredObjects := s.objectStorage.(gatewayObjectReader)
	materials := make([]openAICompatibleImageReference, 0, len(references))
	var totalBytes int64
	for index, reference := range references {
		var body []byte
		var mimeType string
		storageKey := strings.TrimSpace(reference.StorageKey)
		if storageKey != "" && canReadStoredObjects {
			var err error
			body, mimeType, err = reader.GetObject(ctx, storageKey, maxGatewayImageReferenceBytes)
			if err != nil {
				return nil, &StandardErrorError{Standard: StandardError{
					Code:      CodeMediaDownloadFailed,
					Message:   "reference image could not be loaded from object storage",
					Retryable: true,
				}}
			}
		} else if strings.TrimSpace(reference.URL) != "" {
			media, err := s.downloadGatewayImageURL(ctx, account, reference.URL, "", timeout)
			if err != nil {
				return nil, err
			}
			body, err = readGatewayMediaFile(media.TempPath, maxGatewayImageReferenceBytes)
			media.close()
			if err != nil {
				return nil, err
			}
			mimeType = media.MimeType
		} else {
			return nil, fmt.Errorf("%w: reference image %d has no readable media", ErrValidation, index+1)
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("%w: reference image %d is empty", ErrValidation, index+1)
		}
		if int64(len(body)) > maxGatewayImageReferenceBytes {
			return nil, fmt.Errorf("%w: reference image %d exceeds the 50MB file limit", ErrValidation, index+1)
		}
		totalBytes += int64(len(body))
		if totalBytes > maxGatewayImageReferenceTotalBytes {
			return nil, fmt.Errorf("%w: reference images exceed the 128MB request limit", ErrValidation)
		}
		mimeType = normalizeMediaType(mimeType)
		if mimeType == "" || mimeType == "application/octet-stream" {
			mimeType = normalizeMediaType(http.DetectContentType(body))
		}
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, fmt.Errorf("%w: reference image %d is not an image", ErrValidation, index+1)
		}
		materials = append(materials, openAICompatibleImageReference{
			Reference: reference,
			FileName:  gatewayImageReferenceFileName(reference, index, mimeType),
			MimeType:  mimeType,
			Body:      body,
		})
	}
	return materials, nil
}

func gatewayImageReferenceFileName(reference GatewayImageReference, index int, mimeType string) string {
	fileName := path.Base(strings.TrimSpace(reference.StorageKey))
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = fmt.Sprintf("reference-%02d%s", index+1, gatewayImageReferenceExtension(mimeType))
	}
	return fileName
}

func gatewayImageReferenceExtension(mimeType string) string {
	switch normalizeMediaType(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func gatewayImageRequestSnapshot(modelKey string, input json.RawMessage, requested, selected []GatewayImageReference) json.RawMessage {
	requestBody, err := buildImageGenerationRequest(modelKey, input)
	if err != nil {
		return input
	}
	requestMode := "images.generate"
	if len(selected) > 0 {
		requestMode = "images.edit"
	}
	requestBody["requestMode"] = requestMode
	requestBody["referenceCountRequested"] = len(requested)
	requestBody["referenceCountUsed"] = len(selected)
	requestBody["referenceKeys"] = gatewayImageReferenceKeys(selected)
	requestBody["references"] = gatewayImageReferenceSnapshots(selected)
	return mustJSON(requestBody)
}

func gatewayImageReferenceKeys(references []GatewayImageReference) []string {
	keys := make([]string, 0, len(references))
	for _, reference := range references {
		if key := gatewayImageReferenceKey(reference); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func gatewayImageReferenceKey(reference GatewayImageReference) string {
	var metadata struct {
		ReferenceKey string `json:"referenceKey"`
	}
	if json.Unmarshal(reference.Metadata, &metadata) == nil && strings.TrimSpace(metadata.ReferenceKey) != "" {
		return strings.TrimSpace(metadata.ReferenceKey)
	}
	if strings.TrimSpace(reference.ArtifactID) != "" {
		return "artifact:" + strings.TrimSpace(reference.ArtifactID)
	}
	if strings.TrimSpace(reference.AssetID) != "" {
		return "asset:" + strings.TrimSpace(reference.AssetID)
	}
	return ""
}

func gatewayImageReferenceSnapshots(references []GatewayImageReference) []map[string]any {
	items := make([]map[string]any, 0, len(references))
	for _, reference := range references {
		items = append(items, map[string]any{
			"referenceKey": gatewayImageReferenceKey(reference),
			"type":         reference.Type,
			"assetId":      reference.AssetID,
			"artifactId":   reference.ArtifactID,
			"storageKey":   reference.StorageKey,
			"metadata":     rawJSONValue(reference.Metadata),
		})
	}
	return items
}
