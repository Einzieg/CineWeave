package videocontracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type InputContractKey string

const (
	InputContractTextOnly                 InputContractKey = "text_only"
	InputContractFirstFrame               InputContractKey = "first_frame"
	InputContractFirstLastFrames          InputContractKey = "first_last_frames"
	InputContractSemanticReferences       InputContractKey = "semantic_references"
	InputContractFirstFramePlusReferences InputContractKey = "first_frame_plus_references"
	InputContractStoryboardSheetReference InputContractKey = "storyboard_sheet_reference"
	InputContractVideoReference           InputContractKey = "video_reference"
	InputContractVideoExtension           InputContractKey = "video_extension"
)

func ParseInputContractKey(value string) (InputContractKey, error) {
	key := InputContractKey(strings.ToLower(strings.TrimSpace(value)))
	switch key {
	case InputContractTextOnly,
		InputContractFirstFrame,
		InputContractFirstLastFrames,
		InputContractSemanticReferences,
		InputContractFirstFramePlusReferences,
		InputContractStoryboardSheetReference,
		InputContractVideoReference,
		InputContractVideoExtension:
		return key, nil
	default:
		return "", fmt.Errorf("unsupported video input contract %q", value)
	}
}

func RequiresPreviousTailFrame(value string) bool {
	key, err := ParseInputContractKey(value)
	if err != nil {
		return false
	}
	return key == InputContractFirstFrame || key == InputContractFirstFramePlusReferences
}

func SupportsSemanticReferences(value string) bool {
	key, err := ParseInputContractKey(value)
	if err != nil {
		return false
	}
	return key == InputContractSemanticReferences || key == InputContractFirstFramePlusReferences
}

func RequiresPreviousVideo(value string) bool {
	key, err := ParseInputContractKey(value)
	return err == nil && key == InputContractVideoExtension
}

const ReferenceManifestSchemaV2 = 2

type ReferenceManifestV2 struct {
	SchemaVersion          int                       `json:"schemaVersion"`
	ContractKey            InputContractKey          `json:"contractKey"`
	CapabilitySnapshotHash string                    `json:"capabilitySnapshotHash"`
	Items                  []ReferenceManifestItemV2 `json:"items"`
}

type ReferenceManifestItemV2 struct {
	Order         int    `json:"order"`
	ReferenceKey  string `json:"referenceKey"`
	Role          string `json:"role"`
	Required      bool   `json:"required"`
	Priority      int    `json:"priority"`
	MediaType     string `json:"mediaType"`
	Semantics     string `json:"semantics,omitempty"`
	SourceType    string `json:"sourceType"`
	SourceID      string `json:"sourceId,omitempty"`
	SourceVersion string `json:"sourceVersion"`
	AssetID       string `json:"assetId,omitempty"`
	ArtifactID    string `json:"artifactId,omitempty"`
	MediaFileID   string `json:"mediaFileId,omitempty"`
	ContentHash   string `json:"contentHash"`
	GeneratedAt   string `json:"generatedAt,omitempty"`
}

type ReferenceOrderKey struct {
	Role         string
	Required     bool
	Priority     int
	ReferenceKey string
}

func CanonicalReferenceOrderLess(contractKey string, left, right ReferenceOrderKey) bool {
	key, _ := ParseInputContractKey(contractKey)
	leftRank := referenceRoleRank(key, left.Role)
	rightRank := referenceRoleRank(key, right.Role)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if left.Required != right.Required {
		return left.Required
	}
	if left.Priority != right.Priority {
		return left.Priority > right.Priority
	}
	return strings.TrimSpace(left.ReferenceKey) < strings.TrimSpace(right.ReferenceKey)
}

func BuildReferenceManifestV2(contractKey, capabilitySnapshotHash string, items []ReferenceManifestItemV2) (ReferenceManifestV2, string, error) {
	key, err := ParseInputContractKey(contractKey)
	if err != nil {
		return ReferenceManifestV2{}, "", err
	}
	manifest := ReferenceManifestV2{
		SchemaVersion:          ReferenceManifestSchemaV2,
		ContractKey:            key,
		CapabilitySnapshotHash: normalizeSHA256(capabilitySnapshotHash),
		Items:                  append([]ReferenceManifestItemV2(nil), items...),
	}
	hash, err := ValidateReferenceManifestV2(manifest)
	return manifest, hash, err
}

func ValidateReferenceManifestV2(manifest ReferenceManifestV2) (string, error) {
	if manifest.SchemaVersion != ReferenceManifestSchemaV2 {
		return "", fmt.Errorf("reference manifest schemaVersion must be %d", ReferenceManifestSchemaV2)
	}
	key, err := ParseInputContractKey(string(manifest.ContractKey))
	if err != nil {
		return "", err
	}
	manifest.ContractKey = key
	manifest.CapabilitySnapshotHash = normalizeSHA256(manifest.CapabilitySnapshotHash)
	if !validSHA256(manifest.CapabilitySnapshotHash) {
		return "", fmt.Errorf("reference manifest capabilitySnapshotHash must be SHA-256")
	}
	seenKeys := make(map[string]struct{}, len(manifest.Items))
	roles := make([]string, 0, len(manifest.Items))
	for index := range manifest.Items {
		item := &manifest.Items[index]
		item.ReferenceKey = strings.TrimSpace(item.ReferenceKey)
		item.Role = normalizeEnum(item.Role)
		item.MediaType = normalizeEnum(item.MediaType)
		item.Semantics = normalizeEnum(item.Semantics)
		item.SourceType = normalizeEnum(item.SourceType)
		item.SourceID = strings.TrimSpace(item.SourceID)
		item.SourceVersion = strings.TrimSpace(item.SourceVersion)
		item.AssetID = strings.TrimSpace(item.AssetID)
		item.ArtifactID = strings.TrimSpace(item.ArtifactID)
		item.MediaFileID = strings.TrimSpace(item.MediaFileID)
		item.ContentHash = normalizeSHA256(item.ContentHash)
		item.GeneratedAt = strings.TrimSpace(item.GeneratedAt)
		if item.Order != index {
			return "", fmt.Errorf("reference manifest item %s has order %d, want %d", item.ReferenceKey, item.Order, index)
		}
		if item.ReferenceKey == "" || item.Role == "" || item.SourceType == "" || item.SourceVersion == "" {
			return "", fmt.Errorf("reference manifest item %d is missing stable provenance", index)
		}
		if _, exists := seenKeys[item.ReferenceKey]; exists {
			return "", fmt.Errorf("reference manifest contains duplicate referenceKey %s", item.ReferenceKey)
		}
		seenKeys[item.ReferenceKey] = struct{}{}
		if item.MediaType != "image" && item.MediaType != "video" && item.MediaType != "audio" {
			return "", fmt.Errorf("reference manifest item %s has unsupported mediaType %s", item.ReferenceKey, item.MediaType)
		}
		if !validSHA256(item.ContentHash) {
			return "", fmt.Errorf("reference manifest item %s contentHash must be SHA-256", item.ReferenceKey)
		}
		if item.ArtifactID == "" && item.MediaFileID == "" {
			return "", fmt.Errorf("reference manifest item %s requires artifactId or mediaFileId", item.ReferenceKey)
		}
		if item.GeneratedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, item.GeneratedAt); err != nil {
				return "", fmt.Errorf("reference manifest item %s generatedAt must be RFC3339", item.ReferenceKey)
			}
		}
		roles = append(roles, item.Role)
	}
	if err := ValidateReferenceRoleOrder(string(key), roles); err != nil {
		return "", err
	}
	for index := 1; index < len(manifest.Items); index++ {
		left := manifest.Items[index-1]
		right := manifest.Items[index]
		if CanonicalReferenceOrderLess(string(key), referenceOrderKey(right), referenceOrderKey(left)) {
			return "", fmt.Errorf("reference manifest items %s and %s are not in canonical contract order", left.ReferenceKey, right.ReferenceKey)
		}
	}
	return HashReferenceManifestV2(manifest)
}

func ValidateReferenceRoleOrder(contractKey string, roles []string) error {
	key, err := ParseInputContractKey(contractKey)
	if err != nil {
		return err
	}
	for index := range roles {
		roles[index] = normalizeEnum(roles[index])
	}
	switch key {
	case InputContractTextOnly:
		if len(roles) != 0 {
			return fmt.Errorf("text_only contract cannot contain references")
		}
	case InputContractFirstFrame:
		if len(roles) != 1 || roles[0] != "first_frame" {
			return fmt.Errorf("first_frame contract requires exactly one ordered first_frame")
		}
	case InputContractFirstLastFrames:
		if len(roles) != 2 || roles[0] != "first_frame" || roles[1] != "last_frame" {
			return fmt.Errorf("first_last_frames contract requires ordered first_frame then last_frame")
		}
	case InputContractFirstFramePlusReferences:
		if len(roles) == 0 || roles[0] != "first_frame" {
			return fmt.Errorf("first_frame_plus_references contract requires first_frame at order 0")
		}
		for _, role := range roles[1:] {
			if role == "first_frame" || role == "last_frame" {
				return fmt.Errorf("first_frame_plus_references contains an invalid frame role after order 0")
			}
		}
	case InputContractStoryboardSheetReference:
		if len(roles) != 1 || roles[0] != "storyboard_sheet" {
			return fmt.Errorf("storyboard_sheet_reference requires exactly one storyboard_sheet")
		}
	case InputContractVideoReference:
		if len(roles) != 1 || roles[0] != "video_reference" {
			return fmt.Errorf("video_reference contract requires exactly one video_reference")
		}
	case InputContractVideoExtension:
		if len(roles) != 1 || roles[0] != "video_extension_source" {
			return fmt.Errorf("video_extension contract requires exactly one video_extension_source")
		}
	}
	return nil
}

func HashReferenceManifestV2(manifest ReferenceManifestV2) (string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func referenceOrderKey(item ReferenceManifestItemV2) ReferenceOrderKey {
	return ReferenceOrderKey{
		Role: item.Role, Required: item.Required, Priority: item.Priority, ReferenceKey: item.ReferenceKey,
	}
}

func referenceRoleRank(contractKey InputContractKey, role string) int {
	role = normalizeEnum(role)
	switch contractKey {
	case InputContractFirstFrame, InputContractFirstFramePlusReferences:
		if role == "first_frame" {
			return 0
		}
		return 1
	case InputContractFirstLastFrames:
		switch role {
		case "first_frame":
			return 0
		case "last_frame":
			return 1
		default:
			return 2
		}
	case InputContractStoryboardSheetReference:
		if role == "storyboard_sheet" {
			return 0
		}
		return 1
	case InputContractVideoReference:
		if role == "video_reference" {
			return 0
		}
		return 1
	case InputContractVideoExtension:
		if role == "video_extension_source" {
			return 0
		}
		return 1
	default:
		return 0
	}
}

func normalizeEnum(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeSHA256(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
