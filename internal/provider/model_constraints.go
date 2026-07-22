package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	PromptLengthUnitCharacters = "characters"
	PromptLengthUnitUTF8Bytes  = "utf8_bytes"
)

type PromptLengthConstraint struct {
	MaxLength int    `json:"maxLength"`
	Unit      string `json:"unit"`
}

type GatewayModelConstraintsRequest struct {
	OrganizationID  string `json:"organizationId"`
	ModelProfileKey string `json:"modelProfileKey"`
	TaskType        string `json:"taskType"`
	Modality        string `json:"modality"`
}

type GatewayModelConstraintCandidate struct {
	ProviderModelID string                 `json:"providerModelId"`
	ModelKey        string                 `json:"modelKey"`
	Modality        string                 `json:"modality"`
	Prompt          PromptLengthConstraint `json:"prompt"`
	ContextWindow   int                    `json:"contextWindow,omitempty"`
	References      ReferenceConstraint    `json:"references"`
	NativeAudio     NativeAudioConstraint  `json:"nativeAudio"`
}

type ReferenceConstraint struct {
	Supported                        bool     `json:"supported"`
	MaxReferences                    int      `json:"maxReferences"`
	MaxImageReferences               int      `json:"maxImageReferences"`
	MaxVideoReferences               int      `json:"maxVideoReferences"`
	MaxAudioReferences               int      `json:"maxAudioReferences"`
	SupportsFirstFrame               bool     `json:"supportsFirstFrame"`
	SupportsLastFrame                bool     `json:"supportsLastFrame"`
	SupportsStoryboardSheetReference bool     `json:"supportsStoryboardSheetReference"`
	SupportsSemanticReferenceImages  bool     `json:"supportsSemanticReferenceImages"`
	SupportsVideoReference           bool     `json:"supportsVideoReference"`
	SupportsAudioReference           bool     `json:"supportsAudioReference"`
	InputContracts                   []string `json:"inputContracts"`
}

type NativeAudioConstraint struct {
	Support          string `json:"support"`
	SupportsDialogue bool   `json:"supportsDialogue"`
	SupportsLipSync  bool   `json:"supportsLipSync"`
}

type GatewayModelConstraintsResponse struct {
	ModelProfileKey string                            `json:"modelProfileKey"`
	TaskType        string                            `json:"taskType"`
	Modality        string                            `json:"modality"`
	Candidates      []GatewayModelConstraintCandidate `json:"candidates"`
}

func (s *Service) ResolveModelConstraints(ctx context.Context, req GatewayModelConstraintsRequest) (GatewayModelConstraintsResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.ModelProfileKey) == "" {
		return GatewayModelConstraintsResponse{}, fmt.Errorf("%w: organizationId and modelProfileKey are required", ErrValidation)
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  req.OrganizationID,
		ModelProfileKey: req.ModelProfileKey,
		TaskType:        req.TaskType,
		Modality:        req.Modality,
	})
	if err != nil {
		return GatewayModelConstraintsResponse{}, err
	}
	response := GatewayModelConstraintsResponse{
		ModelProfileKey: strings.TrimSpace(req.ModelProfileKey),
		TaskType:        strings.TrimSpace(req.TaskType),
		Modality:        strings.TrimSpace(req.Modality),
		Candidates:      make([]GatewayModelConstraintCandidate, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		references := ModelReferenceConstraint(candidate.Capabilities)
		nativeAudio := NativeAudioConstraint{Support: VideoSupportUnknown}
		if strings.EqualFold(candidate.Modality, "video") || strings.EqualFold(req.Modality, "video") {
			references, nativeAudio = ModelVideoRuntimeConstraints(candidate)
		}
		response.Candidates = append(response.Candidates, GatewayModelConstraintCandidate{
			ProviderModelID: candidate.ProviderModelID,
			ModelKey:        candidate.ModelKey,
			Modality:        candidate.Modality,
			Prompt:          ModelPromptLengthConstraint(candidate.Capabilities),
			ContextWindow:   ModelContextWindow(candidate.Capabilities),
			References:      references,
			NativeAudio:     nativeAudio,
		})
	}
	return response, nil
}

func ModelVideoRuntimeConstraints(candidate RoutingCandidate) (ReferenceConstraint, NativeAudioConstraint) {
	references := ModelReferenceConstraint(candidate.Capabilities)
	nativeAudio := NativeAudioConstraint{Support: VideoSupportUnknown}
	variants, err := videoGenerationVariants(candidate.Capabilities, Model{
		ID: candidate.ProviderModelID, ModelKey: candidate.ModelKey, Modality: candidate.Modality,
	})
	if err != nil || len(variants) == 0 {
		return references, nativeAudio
	}
	// Declared video input contracts are authoritative. Generic image-reference
	// capability fields cannot distinguish a first frame from semantic guidance.
	references = ReferenceConstraint{}
	allFalse := true
	inputContracts := map[string]bool{}
	for _, variant := range variants {
		contractKey := strings.ToLower(strings.TrimSpace(variant.InputContract.ContractKey))
		if contractKey != "" {
			inputContracts[contractKey] = true
		}
		variantTotal, variantImages, variantVideos, variantAudios := 0, 0, 0, 0
		for _, slot := range variant.InputContract.Slots {
			role := strings.ToLower(strings.TrimSpace(slot.Role))
			mediaType := strings.ToLower(strings.TrimSpace(slot.MediaType))
			variantTotal += slot.Max
			switch mediaType {
			case "image":
				variantImages += slot.Max
			case "video":
				variantVideos += slot.Max
			case "audio":
				variantAudios += slot.Max
			}
			switch role {
			case "first_frame":
				references.SupportsFirstFrame = true
			case "last_frame":
				references.SupportsLastFrame = true
			case "storyboard_sheet":
				references.SupportsStoryboardSheetReference = mediaType == "image"
			case "semantic_reference":
				references.SupportsSemanticReferenceImages = mediaType == "image"
			case "video_reference":
				references.SupportsVideoReference = mediaType == "video"
			case "audio_reference":
				references.SupportsAudioReference = mediaType == "audio"
			}
		}
		if variantTotal > references.MaxReferences {
			references.MaxReferences = variantTotal
		}
		if variantImages > references.MaxImageReferences {
			references.MaxImageReferences = variantImages
		}
		if variantVideos > references.MaxVideoReferences {
			references.MaxVideoReferences = variantVideos
		}
		if variantAudios > references.MaxAudioReferences {
			references.MaxAudioReferences = variantAudios
		}
		if variant.Continuation.SupportsFirstFrame || containsNormalizedString(variant.When.ReferenceModes, "first_frame") {
			references.Supported = true
			references.SupportsFirstFrame = true
			if references.MaxReferences == 0 {
				references.MaxReferences = 1
			}
		}
		if variant.Continuation.SupportsLastFrame {
			references.SupportsLastFrame = true
		}
		if variant.Continuation.SupportsVideoReference {
			references.SupportsVideoReference = true
		}
		switch variant.NativeAudio.Support {
		case VideoSupportTrue:
			nativeAudio.Support = VideoSupportTrue
			allFalse = false
		case VideoSupportUnknown:
			allFalse = false
		}
		if variant.NativeAudio.SupportsDialogue != nil && *variant.NativeAudio.SupportsDialogue {
			nativeAudio.SupportsDialogue = true
		}
		if variant.NativeAudio.SupportsLipSync != nil && *variant.NativeAudio.SupportsLipSync {
			nativeAudio.SupportsLipSync = true
		}
	}
	if allFalse {
		nativeAudio.Support = VideoSupportFalse
	}
	if references.MaxReferences > 0 {
		references.Supported = true
	}
	references.InputContracts = make([]string, 0, len(inputContracts))
	for contractKey := range inputContracts {
		references.InputContracts = append(references.InputContracts, contractKey)
	}
	sort.Strings(references.InputContracts)
	return references, nativeAudio
}

func ModelReferenceConstraint(capabilities []Capability) ReferenceConstraint {
	parsed := parseGatewayImageReferenceCapabilities(capabilities)
	return ReferenceConstraint{
		Supported: parsed.SupportsReferences, MaxReferences: parsed.MaxReferences,
		MaxImageReferences:              parsed.MaxReferences,
		SupportsSemanticReferenceImages: parsed.SupportsReferences,
	}
}

func ModelContextWindow(capabilities []Capability) int {
	limit := 0
	for _, capability := range capabilities {
		for _, raw := range []json.RawMessage{capability.InputLimits, capability.ProviderOptionsSchema} {
			values := promptConstraintValues(raw)
			for _, key := range []string{"contextWindow", "maxContextTokens", "maxInputTokens", "maxTokens"} {
				candidate := int(floatField(values[key], key))
				if candidate > 0 && (limit == 0 || candidate < limit) {
					limit = candidate
				}
			}
		}
	}
	return limit
}

func ModelPromptLengthConstraint(capabilities []Capability) PromptLengthConstraint {
	constraint := PromptLengthConstraint{}
	for _, capability := range capabilities {
		for _, raw := range []json.RawMessage{capability.InputLimits, capability.ProviderOptionsSchema} {
			values := promptConstraintValues(raw)
			candidate := firstPositivePromptLimit(values)
			if candidate <= 0 || (constraint.MaxLength > 0 && candidate >= constraint.MaxLength) {
				continue
			}
			constraint.MaxLength = candidate
			constraint.Unit = normalizePromptLengthUnit(promptConstraintString(values, "promptLengthUnit", "promptLimitUnit"))
		}
	}
	if constraint.Unit == "" {
		constraint.Unit = PromptLengthUnitCharacters
	}
	return constraint
}

func MeasurePromptLength(prompt, unit string) int {
	switch normalizePromptLengthUnit(unit) {
	case PromptLengthUnitUTF8Bytes:
		return len([]byte(strings.TrimSpace(prompt)))
	default:
		return len([]rune(strings.TrimSpace(prompt)))
	}
}

func PromptWithinConstraint(prompt string, constraint PromptLengthConstraint) bool {
	return constraint.MaxLength <= 0 || MeasurePromptLength(prompt, constraint.Unit) <= constraint.MaxLength
}

func promptConstraintValues(raw json.RawMessage) map[string]any {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	if nested, ok := values["xCapabilities"].(map[string]any); ok {
		return nested
	}
	return values
}

func firstPositivePromptLimit(values map[string]any) int {
	for _, key := range []string{"promptMaxLength", "maxPromptLength", "maxPromptCharacters"} {
		if value := int(floatField(values[key], key)); value > 0 {
			return value
		}
	}
	return 0
}

func promptConstraintString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizePromptLengthUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "byte", "bytes", "utf8_byte", "utf8_bytes", "utf-8-byte", "utf-8-bytes":
		return PromptLengthUnitUTF8Bytes
	case "character", "characters", "char", "chars", "rune", "runes":
		return PromptLengthUnitCharacters
	default:
		return ""
	}
}
