package videoproduction

import (
	"sort"
	"strings"
)

const (
	CodeReferencePackIncomplete = "REFERENCE_PACK_INCOMPLETE"

	ReferencePurposeAnchor = "anchor"
	ReferencePurposeVideo  = "video"

	ReferenceRoleFirstFrame        = "first_frame"
	ReferenceRoleLastFrame         = "last_frame"
	ReferenceRoleStoryboardSheet   = "storyboard_sheet"
	ReferenceRoleCharacterIdentity = "character_identity"
	ReferenceRoleCharacterCostume  = "character_costume"
	ReferenceRoleSceneIdentity     = "scene_identity"
	ReferenceRoleSceneSpatial      = "scene_spatial"
	ReferenceRolePropIdentity      = "prop_identity"
	ReferenceRoleContinuityHint    = "continuity_hint"
	ReferenceRoleMotion            = "motion_reference"
	ReferenceRoleVideo             = "video_reference"
	ReferenceRoleAudio             = "audio_reference"
	ReferenceRoleStyle             = "style_reference"
)

type ReferenceCandidate struct {
	ReferenceKey string         `json:"referenceKey"`
	Role         string         `json:"role"`
	Required     bool           `json:"required"`
	Primary      bool           `json:"primary"`
	Derived      bool           `json:"derived"`
	Priority     int            `json:"priority"`
	SourceType   string         `json:"sourceType"`
	SourceID     string         `json:"sourceId,omitempty"`
	AssetID      string         `json:"assetId,omitempty"`
	ArtifactID   string         `json:"artifactId,omitempty"`
	MediaFileID  string         `json:"mediaFileId,omitempty"`
	StorageKey   string         `json:"storageKey,omitempty"`
	MediaType    string         `json:"mediaType"`
	Semantics    string         `json:"semantics"`
	ContentHash  string         `json:"contentHash"`
	Active       bool           `json:"active"`
	Fresh        bool           `json:"fresh"`
	Archived     bool           `json:"archived"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type ReferencePackItem struct {
	ReferenceKey string         `json:"referenceKey"`
	Role         string         `json:"role"`
	Required     bool           `json:"required"`
	Priority     int            `json:"priority"`
	SourceType   string         `json:"sourceType"`
	SourceID     string         `json:"sourceId,omitempty"`
	AssetID      string         `json:"assetId,omitempty"`
	ArtifactID   string         `json:"artifactId,omitempty"`
	MediaFileID  string         `json:"mediaFileId,omitempty"`
	StorageKey   string         `json:"storageKey,omitempty"`
	MediaType    string         `json:"mediaType"`
	Semantics    string         `json:"semantics"`
	ContentHash  string         `json:"contentHash"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type ReferencePackManifest struct {
	ProfileKey        string              `json:"profileKey"`
	Purpose           string              `json:"purpose"`
	ShotStateRevision int                 `json:"shotStateRevision"`
	Items             []ReferencePackItem `json:"items"`
}

type ReferencePack struct {
	ProfileSnapshotHash    string                `json:"profileSnapshotHash"`
	ShotStateHash          string                `json:"shotStateHash"`
	CapabilitySnapshotHash string                `json:"capabilitySnapshotHash"`
	Manifest               ReferencePackManifest `json:"manifest"`
	ManifestHash           string                `json:"manifestHash"`
}

type ReferenceResolveInput struct {
	ProfileKey             string
	Purpose                string
	ShotStateRevision      int
	ProfileSnapshotHash    string
	ShotStateHash          string
	CapabilitySnapshotHash string
	RequiredAssetIDs       []string
	MaxReferences          int
	MaxImageReferences     int
	MaxVideoReferences     int
	MaxAudioReferences     int
	Candidates             []ReferenceCandidate
}

func ResolveReferencePack(input ReferenceResolveInput) (ReferencePack, error) {
	input.ProfileKey = enumValue(input.ProfileKey)
	input.Purpose = enumValue(input.Purpose)
	if input.ProfileKey == "" || input.ShotStateRevision <= 0 {
		return ReferencePack{}, Error{Code: CodeReferencePackIncomplete, Message: "参考包缺少 Profile 或 ShotState revision"}
	}
	if input.Purpose != ReferencePurposeAnchor && input.Purpose != ReferencePurposeVideo {
		return ReferencePack{}, Error{Code: CodeReferencePackIncomplete, Message: "参考包 purpose 必须是 anchor 或 video"}
	}
	strategy, err := ProfileStrategyFor(input.ProfileKey)
	if err != nil {
		return ReferencePack{}, err
	}
	for field, value := range map[string]string{
		"profileSnapshotHash":    input.ProfileSnapshotHash,
		"shotStateHash":          input.ShotStateHash,
		"capabilitySnapshotHash": input.CapabilitySnapshotHash,
	} {
		if !validSHA256(value) {
			return ReferencePack{}, Error{Code: CodeReferencePackIncomplete, Message: field + " 必须是 SHA-256"}
		}
	}

	candidates := make([]ReferenceCandidate, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		candidate.ReferenceKey = strings.TrimSpace(candidate.ReferenceKey)
		candidate.Role = enumValue(candidate.Role)
		candidate.MediaType = enumValue(candidate.MediaType)
		if candidate.MediaType == "" {
			candidate.MediaType = defaultReferenceMediaType(candidate.Role)
		}
		candidate.Semantics = enumValue(candidate.Semantics)
		if candidate.Semantics == "" {
			candidate.Semantics = defaultReferenceSemantics(candidate.Role)
		}
		candidate.AssetID = strings.TrimSpace(candidate.AssetID)
		candidate.ContentHash = normalizeSHA256(candidate.ContentHash)
		if !candidate.Active || !candidate.Fresh || candidate.Archived || candidate.ReferenceKey == "" || !validReferenceRole(candidate.Role) ||
			!oneOf(candidate.MediaType, "image", "video", "audio") || candidate.Semantics == "" || !validSHA256(candidate.ContentHash) {
			continue
		}
		if !strategy.References().Allows(input.Purpose, candidate.Role) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		leftRank := referenceRank(candidates[left])
		rightRank := referenceRank(candidates[right])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if candidates[left].Priority != candidates[right].Priority {
			return candidates[left].Priority > candidates[right].Priority
		}
		return candidates[left].ReferenceKey < candidates[right].ReferenceKey
	})

	seenKeys := map[string]bool{}
	selected := make([]ReferenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if seenKeys[candidate.ReferenceKey] {
			continue
		}
		seenKeys[candidate.ReferenceKey] = true
		selected = append(selected, candidate)
	}
	for _, limit := range []struct {
		mediaType string
		maximum   int
	}{
		{mediaType: "image", maximum: input.MaxImageReferences},
		{mediaType: "video", maximum: input.MaxVideoReferences},
		{mediaType: "audio", maximum: input.MaxAudioReferences},
	} {
		selected, err = trimReferenceCandidatesByMedia(selected, limit.mediaType, limit.maximum)
		if err != nil {
			return ReferencePack{}, err
		}
	}
	if input.MaxReferences > 0 && len(selected) > input.MaxReferences {
		for _, candidate := range selected[input.MaxReferences:] {
			if candidate.Required {
				return ReferencePack{}, Error{Code: CodeReferencePackIncomplete, Message: "模型参考图上限会丢失必需引用 " + candidate.ReferenceKey}
			}
		}
		selected = selected[:input.MaxReferences]
	}

	selectedAssets := map[string]bool{}
	items := make([]ReferencePackItem, 0, len(selected))
	for _, candidate := range selected {
		if candidate.AssetID != "" {
			selectedAssets[candidate.AssetID] = true
		}
		items = append(items, ReferencePackItem{
			ReferenceKey: candidate.ReferenceKey,
			Role:         candidate.Role,
			Required:     candidate.Required,
			Priority:     candidate.Priority,
			SourceType:   strings.TrimSpace(candidate.SourceType),
			SourceID:     strings.TrimSpace(candidate.SourceID),
			AssetID:      candidate.AssetID,
			ArtifactID:   strings.TrimSpace(candidate.ArtifactID),
			MediaFileID:  strings.TrimSpace(candidate.MediaFileID),
			StorageKey:   strings.TrimSpace(candidate.StorageKey),
			MediaType:    candidate.MediaType,
			Semantics:    candidate.Semantics,
			ContentHash:  candidate.ContentHash,
			Metadata:     candidate.Metadata,
		})
	}
	for _, assetID := range normalizedReferenceAssetIDs(input.RequiredAssetIDs) {
		if !selectedAssets[assetID] {
			return ReferencePack{}, Error{Code: CodeReferencePackIncomplete, Message: "缺少必需资产引用 " + assetID}
		}
	}
	if err := strategy.References().Validate(input.Purpose, items); err != nil {
		return ReferencePack{}, err
	}
	if input.Purpose == ReferencePurposeVideo {
		if err := strategy.InputAdapter().ValidateReferenceRoles(items); err != nil {
			return ReferencePack{}, err
		}
	}
	manifest := ReferencePackManifest{
		ProfileKey:        input.ProfileKey,
		Purpose:           input.Purpose,
		ShotStateRevision: input.ShotStateRevision,
		Items:             items,
	}
	hash, err := canonicalHash(manifest)
	if err != nil {
		return ReferencePack{}, err
	}
	return ReferencePack{
		ProfileSnapshotHash:    normalizeSHA256(input.ProfileSnapshotHash),
		ShotStateHash:          normalizeSHA256(input.ShotStateHash),
		CapabilitySnapshotHash: normalizeSHA256(input.CapabilitySnapshotHash),
		Manifest:               manifest,
		ManifestHash:           hash,
	}, nil
}

func trimReferenceCandidatesByMedia(candidates []ReferenceCandidate, mediaType string, maximum int) ([]ReferenceCandidate, error) {
	if maximum <= 0 {
		return candidates, nil
	}
	result := make([]ReferenceCandidate, 0, len(candidates))
	count := 0
	for _, candidate := range candidates {
		if candidate.MediaType != mediaType {
			result = append(result, candidate)
			continue
		}
		if count < maximum {
			result = append(result, candidate)
			count++
			continue
		}
		if candidate.Required {
			return nil, Error{Code: CodeReferencePackIncomplete, Message: "模型" + mediaType + "引用上限会丢失必需引用 " + candidate.ReferenceKey}
		}
	}
	return result, nil
}

func defaultReferenceMediaType(role string) string {
	return ReferenceMediaTypeForRole(role)
}

func ReferenceMediaTypeForRole(role string) string {
	switch enumValue(role) {
	case ReferenceRoleVideo, ReferenceRoleMotion:
		return "video"
	case ReferenceRoleAudio:
		return "audio"
	default:
		return "image"
	}
}

func defaultReferenceSemantics(role string) string {
	return ReferenceSemanticsForRole(role)
}

func ReferenceSemanticsForRole(role string) string {
	switch enumValue(role) {
	case ReferenceRoleFirstFrame:
		return "output_start_frame"
	case ReferenceRoleLastFrame:
		return "output_end_frame"
	case ReferenceRoleStoryboardSheet:
		return "ordered_keyframe_sheet"
	case ReferenceRoleCharacterIdentity:
		return "character_identity"
	case ReferenceRoleCharacterCostume:
		return "character_costume"
	case ReferenceRoleSceneIdentity:
		return "scene_identity"
	case ReferenceRoleSceneSpatial:
		return "scene_spatial_layout"
	case ReferenceRolePropIdentity:
		return "prop_identity"
	case ReferenceRoleContinuityHint:
		return "cross_shot_continuity_hint"
	case ReferenceRoleMotion:
		return "motion_guidance"
	case ReferenceRoleVideo:
		return "video_guidance"
	case ReferenceRoleAudio:
		return "audio_guidance"
	case ReferenceRoleStyle:
		return "visual_style_guidance"
	default:
		return ""
	}
}

func normalizedReferenceAssetIDs(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validReferenceRole(value string) bool {
	return oneOf(value,
		ReferenceRoleFirstFrame, ReferenceRoleLastFrame, ReferenceRoleStoryboardSheet,
		ReferenceRoleCharacterIdentity, ReferenceRoleCharacterCostume,
		ReferenceRoleSceneIdentity, ReferenceRoleSceneSpatial, ReferenceRolePropIdentity,
		ReferenceRoleContinuityHint, ReferenceRoleMotion, ReferenceRoleVideo,
		ReferenceRoleAudio, ReferenceRoleStyle,
	)
}

func referenceRank(candidate ReferenceCandidate) int {
	switch {
	case candidate.Required:
		return 0
	case candidate.Primary:
		return 1
	case candidate.Derived:
		return 2
	case candidate.Role == ReferenceRoleContinuityHint:
		return 3
	case candidate.Role == ReferenceRoleStyle:
		return 4
	default:
		return 2
	}
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimPrefix(value, "sha256:")
}

func validSHA256(value string) bool {
	value = normalizeSHA256(value)
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
