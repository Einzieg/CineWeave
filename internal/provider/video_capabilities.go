package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/Einzieg/cineweave/internal/videocontracts"
)

const (
	VideoDurationContinuousRange = "continuous_range"
	VideoDurationDiscrete        = "discrete"
	VideoDurationFixed           = "fixed"
	VideoDurationSource          = "source_duration"

	VideoSupportTrue    = "true"
	VideoSupportFalse   = "false"
	VideoSupportUnknown = "unknown"

	VideoInputContractTextOnly                 = string(videocontracts.InputContractTextOnly)
	VideoInputContractFirstFrame               = string(videocontracts.InputContractFirstFrame)
	VideoInputContractFirstLastFrames          = string(videocontracts.InputContractFirstLastFrames)
	VideoInputContractSemanticReferences       = string(videocontracts.InputContractSemanticReferences)
	VideoInputContractFirstFramePlusReferences = string(videocontracts.InputContractFirstFramePlusReferences)
	VideoInputContractStoryboardSheetReference = string(videocontracts.InputContractStoryboardSheetReference)
	VideoInputContractVideoReference           = string(videocontracts.InputContractVideoReference)
	VideoInputContractVideoExtension           = string(videocontracts.InputContractVideoExtension)

	VideoCapabilityVerificationOfficial = "official"
	VideoCapabilityVerificationTested   = "tested"
	VideoCapabilityVerificationInferred = "inferred"
	VideoCapabilityVerificationUnknown  = "unknown"
)

type VideoGenerationVariant struct {
	VariantKey                 string                      `json:"variantKey"`
	ModelFamily                string                      `json:"modelFamily,omitempty"`
	When                       VideoGenerationVariantWhen  `json:"when"`
	Duration                   VideoDurationCapability     `json:"duration"`
	Resolutions                []string                    `json:"resolutions,omitempty"`
	AspectRatios               []string                    `json:"aspectRatios,omitempty"`
	FrameRate                  VideoFrameRateCapability    `json:"frameRate"`
	SupportedPromptLanguages   []string                    `json:"supportedPromptLanguages,omitempty"`
	NativeAudio                VideoNativeAudioCapability  `json:"nativeAudio"`
	Continuation               VideoContinuationCapability `json:"continuation"`
	InputContract              VideoInputContract          `json:"inputContract"`
	ContinuationInputContracts []VideoInputContract        `json:"continuationInputContracts,omitempty"`
	RequestModes               []string                    `json:"requestModes,omitempty"`
	Source                     string                      `json:"source,omitempty"`
	SourceURL                  string                      `json:"sourceUrl,omitempty"`
	VerifiedAt                 string                      `json:"verifiedAt,omitempty"`
	CapabilityVersion          string                      `json:"capabilityVersion,omitempty"`
	VerificationStatus         string                      `json:"verificationStatus"`
}

type VideoInputContract struct {
	ContractKey                      string           `json:"contractKey"`
	RequestMode                      string           `json:"requestMode"`
	Slots                            []VideoInputSlot `json:"slots"`
	MutuallyExclusiveRoles           [][]string       `json:"mutuallyExclusiveRoles"`
	SupportsStoryboardSheetReference bool             `json:"supportsStoryboardSheetReference"`
	SupportsVideoExtension           bool             `json:"supportsVideoExtension"`
}

type VideoInputSlot struct {
	Role      string `json:"role"`
	MediaType string `json:"mediaType"`
	Semantics string `json:"semantics"`
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Ordered   bool   `json:"ordered"`
}

type VideoGenerationVariantWhen struct {
	TaskTypes            []string `json:"taskTypes,omitempty"`
	ReferenceModes       []string `json:"referenceModes,omitempty"`
	NativeAudioRequested *bool    `json:"nativeAudioRequested,omitempty"`
}

type VideoDurationCapability struct {
	Mode        string    `json:"mode"`
	MinSeconds  float64   `json:"minSeconds,omitempty"`
	MaxSeconds  float64   `json:"maxSeconds,omitempty"`
	Values      []float64 `json:"values,omitempty"`
	StepSeconds float64   `json:"stepSeconds,omitempty"`
}

type VideoFrameRateCapability struct {
	Mode   string    `json:"mode"`
	Values []float64 `json:"values,omitempty"`
}

type VideoNativeAudioCapability struct {
	Support                    string   `json:"support"`
	CanDisable                 *bool    `json:"canDisable,omitempty"`
	SupportsDialogue           *bool    `json:"supportsDialogue,omitempty"`
	SupportsVoiceover          *bool    `json:"supportsVoiceover,omitempty"`
	SupportsAmbientSound       *bool    `json:"supportsAmbientSound,omitempty"`
	SupportsMusic              *bool    `json:"supportsMusic,omitempty"`
	SupportsLipSync            *bool    `json:"supportsLipSync,omitempty"`
	SupportedDialogueLanguages []string `json:"supportedDialogueLanguages,omitempty"`
	AudioTrackSeparable        bool     `json:"audioTrackSeparable"`
}

type VideoContinuationCapability struct {
	SupportsExtension      bool `json:"supportsExtension"`
	SupportsFirstFrame     bool `json:"supportsFirstFrame"`
	SupportsLastFrame      bool `json:"supportsLastFrame"`
	SupportsVideoReference bool `json:"supportsVideoReference"`
}

type GatewayVideoPlanRequest struct {
	OrganizationID                      string                     `json:"organizationId"`
	ProjectID                           string                     `json:"projectId"`
	OperationID                         string                     `json:"operationId,omitempty"`
	OperationItemID                     string                     `json:"operationItemId,omitempty"`
	OperationItemAttempt                int                        `json:"operationItemAttempt,omitempty"`
	ProductionGenerationID              string                     `json:"productionGenerationId"`
	VideoProductionBindingID            string                     `json:"videoProductionBindingId"`
	VideoProductionBindingRevision      int64                      `json:"videoProductionBindingRevision"`
	ProductionProfileVersionID          string                     `json:"productionProfileVersionId"`
	ProductionProfileSnapshotHash       string                     `json:"productionProfileSnapshotHash"`
	CompatibilityPolicy                 string                     `json:"compatibilityPolicy"`
	RequiredInitialInputContract        string                     `json:"requiredInitialInputContract"`
	AllowedContinuationInputContracts   []string                   `json:"allowedContinuationInputContracts,omitempty"`
	InputContractVersion                string                     `json:"inputContractVersion"`
	ShotStateRevision                   int                        `json:"shotStateRevision"`
	ShotStateHash                       string                     `json:"shotStateHash"`
	TransitionHash                      string                     `json:"transitionHash,omitempty"`
	ReferencePackID                     string                     `json:"referencePackId"`
	ReferencePackHash                   string                     `json:"referencePackHash"`
	PromptContextPlanID                 string                     `json:"promptContextPlanId"`
	PromptContextPlanHash               string                     `json:"promptContextPlanHash"`
	VideoPromptPlanID                   string                     `json:"videoPromptPlanId"`
	NativeAudioRequired                 bool                       `json:"nativeAudioRequired"`
	WorkflowRunID                       string                     `json:"workflowRunId,omitempty"`
	NodeRunID                           string                     `json:"nodeRunId,omitempty"`
	NodeExecutionToken                  string                     `json:"nodeExecutionToken,omitempty"`
	NodeAttemptGeneration               int                        `json:"nodeAttemptGeneration,omitempty"`
	StoryboardPlanID                    string                     `json:"storyboardPlanId,omitempty"`
	StoryboardShotID                    string                     `json:"storyboardShotId"`
	ModelProfileKey                     string                     `json:"modelProfileKey,omitempty"`
	ProviderModelID                     string                     `json:"providerModelId,omitempty"`
	TaskType                            string                     `json:"taskType"`
	TargetDurationTicks                 int64                      `json:"targetDurationTicks"`
	TimelineTimebase                    int64                      `json:"timelineTimebase"`
	FPSNumerator                        int64                      `json:"fpsNumerator"`
	FPSDenominator                      int64                      `json:"fpsDenominator"`
	AudioStrategy                       string                     `json:"audioStrategy"`
	AudioRequirement                    string                     `json:"audioRequirement"`
	DialogueLanguage                    string                     `json:"dialogueLanguage,omitempty"`
	HasDialogue                         bool                       `json:"hasDialogue"`
	ReferenceMode                       string                     `json:"referenceMode"`
	AspectRatio                         string                     `json:"aspectRatio"`
	Resolution                          string                     `json:"resolution"`
	PromptLanguage                      string                     `json:"promptLanguage,omitempty"`
	RequireApprovedLanguageCapabilities bool                       `json:"requireApprovedLanguageCapabilities,omitempty"`
	ExpiresInSeconds                    int                        `json:"expiresInSeconds,omitempty"`
	Force                               bool                       `json:"force,omitempty"`
	ExcludeProviderModelIDs             []string                   `json:"excludeProviderModelIds,omitempty"`
	PreviousExecutionPlanID             string                     `json:"previousExecutionPlanId,omitempty"`
	DialogueSpans                       []GatewayVideoDialogueSpan `json:"dialogueSpans,omitempty"`
	validatedContract                   *videoPlanProductionContract
}

type GatewayVideoDialogueSpan struct {
	TimingUnitID          string `json:"timingUnitId,omitempty"`
	Speaker               string `json:"speaker"`
	Text                  string `json:"text"`
	Delivery              string `json:"delivery,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	StartTick             int64  `json:"startTick"`
	EndTick               int64  `json:"endTick"`
	ContinuesFromPrevious bool   `json:"continuesFromPrevious,omitempty"`
	ContinuesToNext       bool   `json:"continuesToNext,omitempty"`
}

type GatewayVideoPlanSegment struct {
	SegmentID                string                     `json:"segmentId,omitempty"`
	SegmentIndex             int                        `json:"segmentIndex"`
	PlannedStartTick         int64                      `json:"plannedStartTick"`
	PlannedEndTick           int64                      `json:"plannedEndTick"`
	PlannedDurationTicks     int64                      `json:"plannedDurationTicks"`
	PlannedDurationSeconds   float64                    `json:"plannedDurationSeconds"`
	RequestedDurationSeconds float64                    `json:"requestedDurationSeconds"`
	ContinuityMode           string                     `json:"continuityMode"`
	InputContractKey         string                     `json:"inputContractKey"`
	InputContractHash        string                     `json:"inputContractHash"`
	TrimEndTick              int64                      `json:"trimEndTick,omitempty"`
	DialogueSpans            []GatewayVideoDialogueSpan `json:"dialogueSpans,omitempty"`
}

type GatewayVideoPlanResponse struct {
	ExecutionPlanID                   string                    `json:"executionPlanId"`
	ProviderModelID                   string                    `json:"providerModelId"`
	ProviderAccountID                 string                    `json:"providerAccountId"`
	ModelFamily                       string                    `json:"modelFamily"`
	VariantKey                        string                    `json:"variantKey"`
	CapabilitySnapshot                VideoGenerationVariant    `json:"capabilitySnapshot"`
	CapabilitySnapshotHash            string                    `json:"capabilitySnapshotHash"`
	InitialInputContractSnapshot      VideoInputContract        `json:"initialInputContractSnapshot"`
	InitialInputContractHash          string                    `json:"initialInputContractHash"`
	ContinuationInputContractSnapshot *VideoInputContract       `json:"continuationInputContractSnapshot,omitempty"`
	ContinuationInputContractHash     string                    `json:"continuationInputContractHash,omitempty"`
	CapabilityAttestationID           string                    `json:"capabilityAttestationId,omitempty"`
	ProductionProfileVersionID        string                    `json:"productionProfileVersionId"`
	ProductionProfileSnapshotHash     string                    `json:"productionProfileSnapshotHash"`
	CompatibilityPolicy               string                    `json:"compatibilityPolicy"`
	ShotStateRevision                 int                       `json:"shotStateRevision"`
	ShotStateHash                     string                    `json:"shotStateHash"`
	TransitionHash                    string                    `json:"transitionHash,omitempty"`
	ReferencePackID                   string                    `json:"referencePackId"`
	ReferencePackHash                 string                    `json:"referencePackHash"`
	PromptContextPlanID               string                    `json:"promptContextPlanId"`
	PromptContextPlanHash             string                    `json:"promptContextPlanHash"`
	VideoPromptPlanID                 string                    `json:"videoPromptPlanId"`
	NativeAudioRequired               bool                      `json:"nativeAudioRequired"`
	TimelineTimebase                  int64                     `json:"timelineTimebase"`
	FPSNumerator                      int64                     `json:"fpsNumerator"`
	FPSDenominator                    int64                     `json:"fpsDenominator"`
	ExpiresAt                         string                    `json:"expiresAt"`
	AudioStrategy                     string                    `json:"audioStrategy"`
	AudioRequirement                  string                    `json:"audioRequirement"`
	NativeAudioStatus                 string                    `json:"nativeAudioStatus"`
	ProductionReadiness               string                    `json:"productionReadiness"`
	Segments                          []GatewayVideoPlanSegment `json:"segments"`
}

type GatewayVideoRetrySegmentRequest struct {
	OrganizationID                 string `json:"organizationId"`
	ProjectID                      string `json:"projectId"`
	ProductionGenerationID         string `json:"productionGenerationId"`
	VideoProductionBindingID       string `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64  `json:"videoProductionBindingRevision"`
	WorkflowRunID                  string `json:"workflowRunId"`
	NodeRunID                      string `json:"nodeRunId"`
	NodeExecutionToken             string `json:"nodeExecutionToken"`
	NodeAttemptGeneration          int    `json:"nodeAttemptGeneration"`
	ExecutionPlanID                string `json:"executionPlanId"`
	RenderSegmentID                string `json:"renderSegmentId"`
	FailureCode                    string `json:"failureCode,omitempty"`
	FailureMessage                 string `json:"failureMessage,omitempty"`
}

type GatewayVideoRetrySegmentResponse struct {
	ExecutionPlanID        string `json:"executionPlanId"`
	RenderSegmentID        string `json:"renderSegmentId"`
	ProviderModelID        string `json:"providerModelId"`
	ProviderAccountID      string `json:"providerAccountId"`
	CapabilitySnapshotHash string `json:"capabilitySnapshotHash"`
	RetryGeneration        int    `json:"retryGeneration"`
	RetryScope             string `json:"retryScope"`
}

type videoVariantMatchRequest struct {
	TaskType                          string
	ReferenceMode                     string
	AspectRatio                       string
	Resolution                        string
	PromptLanguage                    string
	DialogueLanguage                  string
	HasDialogue                       bool
	AudioStrategy                     string
	AudioRequirement                  string
	RequiredInitialInputContract      string
	AllowedContinuationInputContracts []string
	CompatibilityPolicy               string
}

func videoGenerationVariants(capabilities []Capability, model Model) ([]VideoGenerationVariant, error) {
	variants := make([]VideoGenerationVariant, 0)
	for _, capability := range capabilities {
		var schema map[string]any
		if len(capability.ProviderOptionsSchema) == 0 || string(capability.ProviderOptionsSchema) == "null" {
			continue
		}
		if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
			return nil, fmt.Errorf("%w: video provider options schema is invalid", ErrValidation)
		}
		xCapabilities, _ := schema["xCapabilities"].(map[string]any)
		if xCapabilities == nil {
			continue
		}
		if rawVariants, ok := xCapabilities["videoGenerationVariants"]; ok {
			raw, err := json.Marshal(rawVariants)
			if err != nil {
				return nil, err
			}
			var parsed []VideoGenerationVariant
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return nil, fmt.Errorf("%w: videoGenerationVariants is invalid", ErrValidation)
			}
			variants = append(variants, parsed...)
		}
	}
	seen := map[string]bool{}
	for index := range variants {
		variant := &variants[index]
		variant.VariantKey = strings.TrimSpace(variant.VariantKey)
		if variant.VariantKey == "" || seen[variant.VariantKey] {
			return nil, fmt.Errorf("%w: every video generation variant requires a unique variantKey", ErrValidation)
		}
		seen[variant.VariantKey] = true
		if strings.TrimSpace(variant.ModelFamily) == "" {
			variant.ModelFamily = inferVideoModelFamily(model.ModelKey)
		}
		if err := normalizeVideoInputContract(variant); err != nil {
			return nil, err
		}
		if err := validateVideoGenerationVariant(*variant); err != nil {
			return nil, err
		}
	}
	return variants, nil
}

func normalizeVideoInputContract(variant *VideoGenerationVariant) error {
	if variant == nil {
		return fmt.Errorf("%w: video generation variant is required", ErrValidation)
	}
	variant.VerificationStatus = normalizeVideoCapabilityVerification(variant.VerificationStatus, variant.Source)
	if err := normalizeVideoInputContractValue(&variant.InputContract, *variant, true); err != nil {
		return err
	}
	if len(variant.ContinuationInputContracts) == 0 {
		variant.ContinuationInputContracts = inferredVideoContinuationInputContracts(*variant)
	}
	seen := make(map[string]struct{}, len(variant.ContinuationInputContracts))
	for index := range variant.ContinuationInputContracts {
		contract := &variant.ContinuationInputContracts[index]
		if err := normalizeVideoInputContractValue(contract, *variant, false); err != nil {
			return err
		}
		if _, exists := seen[contract.ContractKey]; exists {
			return fmt.Errorf("%w: variant %s has duplicate continuation input contract %s", ErrValidation, variant.VariantKey, contract.ContractKey)
		}
		seen[contract.ContractKey] = struct{}{}
	}
	return nil
}

func normalizeVideoInputContractValue(contract *VideoInputContract, variant VideoGenerationVariant, initial bool) error {
	if contract == nil {
		return fmt.Errorf("%w: video input contract is required", ErrValidation)
	}
	contract.ContractKey = strings.ToLower(strings.TrimSpace(contract.ContractKey))
	contract.RequestMode = strings.ToLower(strings.TrimSpace(contract.RequestMode))
	if contract.ContractKey == "" {
		if !initial {
			return fmt.Errorf("%w: continuation video input contract requires contractKey", ErrValidation)
		}
		contract.ContractKey = inferVideoInputContractKey(variant)
	}
	if contract.RequestMode == "" {
		contract.RequestMode = firstNormalizedVideoRequestMode(variant.RequestModes)
	}
	if contract.RequestMode == "" {
		contract.RequestMode = "async_create"
	}
	if len(contract.Slots) == 0 {
		contract.Slots = canonicalVideoInputSlots(contract.ContractKey, variant)
	}
	if contract.Slots == nil {
		contract.Slots = []VideoInputSlot{}
	}
	if contract.MutuallyExclusiveRoles == nil {
		contract.MutuallyExclusiveRoles = [][]string{}
	}
	if contract.ContractKey == VideoInputContractStoryboardSheetReference {
		contract.SupportsStoryboardSheetReference = true
	}
	if contract.ContractKey == VideoInputContractVideoExtension || variant.Continuation.SupportsExtension {
		if contract.ContractKey == VideoInputContractVideoExtension {
			contract.SupportsVideoExtension = true
		}
	}
	return validateVideoInputContract(*contract)
}

func inferredVideoContinuationInputContracts(variant VideoGenerationVariant) []VideoInputContract {
	result := make([]VideoInputContract, 0, 2)
	if variant.Continuation.SupportsExtension || variant.InputContract.SupportsVideoExtension {
		result = append(result, VideoInputContract{
			ContractKey:            VideoInputContractVideoExtension,
			RequestMode:            variant.InputContract.RequestMode,
			Slots:                  canonicalVideoInputSlots(VideoInputContractVideoExtension, variant),
			MutuallyExclusiveRoles: [][]string{},
			SupportsVideoExtension: true,
		})
	}
	if videoInputContractSatisfies(variant.InputContract, VideoInputContractFirstFrame) || variant.Continuation.SupportsFirstFrame {
		result = append(result, VideoInputContract{
			ContractKey:            VideoInputContractFirstFrame,
			RequestMode:            variant.InputContract.RequestMode,
			Slots:                  canonicalVideoInputSlots(VideoInputContractFirstFrame, variant),
			MutuallyExclusiveRoles: [][]string{},
		})
	}
	return result
}

func normalizeVideoCapabilityVerification(value, source string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case VideoCapabilityVerificationOfficial, VideoCapabilityVerificationTested,
		VideoCapabilityVerificationInferred, VideoCapabilityVerificationUnknown:
		return value
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "official":
		return VideoCapabilityVerificationOfficial
	case "test", "tested", "fixture":
		return VideoCapabilityVerificationTested
	case "derived", "inferred":
		return VideoCapabilityVerificationInferred
	default:
		return VideoCapabilityVerificationUnknown
	}
}

func inferVideoInputContractKey(variant VideoGenerationVariant) string {
	modes := normalizeVideoStringSlice(variant.When.ReferenceModes)
	switch {
	case containsNormalizedString(modes, "storyboard_sheet") || containsNormalizedString(modes, "storyboard_sheet_reference"):
		return VideoInputContractStoryboardSheetReference
	case containsNormalizedString(modes, "first_last_frames") ||
		(variant.Continuation.SupportsFirstFrame && variant.Continuation.SupportsLastFrame):
		return VideoInputContractFirstLastFrames
	case containsNormalizedString(modes, "first_frame_plus_references"):
		return VideoInputContractFirstFramePlusReferences
	case containsNormalizedString(modes, "semantic_references") || containsNormalizedString(modes, "reference"):
		return VideoInputContractSemanticReferences
	case containsNormalizedString(modes, "video_extension") || variant.Continuation.SupportsExtension:
		return VideoInputContractVideoExtension
	case containsNormalizedString(modes, "video_reference") || variant.Continuation.SupportsVideoReference:
		return VideoInputContractVideoReference
	case containsNormalizedString(modes, "first_frame") || variant.Continuation.SupportsFirstFrame:
		return VideoInputContractFirstFrame
	default:
		return VideoInputContractTextOnly
	}
}

func normalizeVideoStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func canonicalVideoInputSlots(contractKey string, variant VideoGenerationVariant) []VideoInputSlot {
	maxReferences := 1
	for _, mode := range variant.When.ReferenceModes {
		if strings.EqualFold(strings.TrimSpace(mode), "first_frame_plus_references") || strings.EqualFold(strings.TrimSpace(mode), "semantic_references") {
			maxReferences = 8
			break
		}
	}
	firstFrame := VideoInputSlot{Role: "first_frame", MediaType: "image", Semantics: "output_start_frame", Min: 1, Max: 1, Ordered: true}
	lastFrame := VideoInputSlot{Role: "last_frame", MediaType: "image", Semantics: "output_end_frame", Min: 1, Max: 1, Ordered: true}
	semantic := VideoInputSlot{Role: "semantic_reference", MediaType: "image", Semantics: "identity_scene_style_guidance", Min: 1, Max: maxReferences, Ordered: false}
	switch contractKey {
	case VideoInputContractTextOnly:
		return []VideoInputSlot{}
	case VideoInputContractFirstFrame:
		return []VideoInputSlot{firstFrame}
	case VideoInputContractFirstLastFrames:
		return []VideoInputSlot{firstFrame, lastFrame}
	case VideoInputContractSemanticReferences:
		return []VideoInputSlot{semantic}
	case VideoInputContractFirstFramePlusReferences:
		semantic.Min = 0
		return []VideoInputSlot{firstFrame, semantic}
	case VideoInputContractStoryboardSheetReference:
		return []VideoInputSlot{{Role: "storyboard_sheet", MediaType: "image", Semantics: "ordered_keyframe_sheet", Min: 1, Max: 1, Ordered: true}}
	case VideoInputContractVideoReference:
		return []VideoInputSlot{{Role: "video_reference", MediaType: "video", Semantics: "motion_identity_guidance", Min: 1, Max: 1, Ordered: true}}
	case VideoInputContractVideoExtension:
		return []VideoInputSlot{{Role: "video_extension_source", MediaType: "video", Semantics: "source_video_extension", Min: 1, Max: 1, Ordered: true}}
	default:
		return nil
	}
}

func validateVideoInputContract(contract VideoInputContract) error {
	if !validVideoInputContractKey(contract.ContractKey) {
		return fmt.Errorf("%w: unsupported video input contract %s", ErrValidation, contract.ContractKey)
	}
	if contract.RequestMode == "" {
		return fmt.Errorf("%w: video input contract %s requires requestMode", ErrValidation, contract.ContractKey)
	}
	seenRoles := map[string]bool{}
	for _, slot := range contract.Slots {
		role := strings.ToLower(strings.TrimSpace(slot.Role))
		mediaType := strings.ToLower(strings.TrimSpace(slot.MediaType))
		if role == "" || seenRoles[role] || (mediaType != "image" && mediaType != "video" && mediaType != "audio") || slot.Min < 0 || slot.Max < slot.Min || slot.Max <= 0 {
			return fmt.Errorf("%w: video input contract %s has an invalid or duplicate slot %s", ErrValidation, contract.ContractKey, role)
		}
		seenRoles[role] = true
	}
	for _, group := range contract.MutuallyExclusiveRoles {
		if len(group) < 2 {
			return fmt.Errorf("%w: mutually exclusive role groups require at least two roles", ErrValidation)
		}
		for _, role := range group {
			if !seenRoles[strings.ToLower(strings.TrimSpace(role))] {
				return fmt.Errorf("%w: mutually exclusive role %s is not declared as an input slot", ErrValidation, role)
			}
		}
	}
	return nil
}

func validVideoInputContractKey(value string) bool {
	_, err := videocontracts.ParseInputContractKey(value)
	return err == nil
}

func firstNormalizedVideoRequestMode(values []string) string {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "async_create" || value == "sync" || value == "stream" {
			return value
		}
	}
	return ""
}

func videoInputContractSatisfies(actual VideoInputContract, required string) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	actualKey := strings.ToLower(strings.TrimSpace(actual.ContractKey))
	if actualKey == required {
		return true
	}
	return required == VideoInputContractFirstFrame && actualKey == VideoInputContractFirstFramePlusReferences
}

func validateVideoGenerationVariant(variant VideoGenerationVariant) error {
	mode := strings.TrimSpace(variant.Duration.Mode)
	switch mode {
	case VideoDurationContinuousRange:
		if variant.Duration.MinSeconds <= 0 || variant.Duration.MaxSeconds < variant.Duration.MinSeconds || variant.Duration.StepSeconds < 0 {
			return fmt.Errorf("%w: variant %s has an invalid continuous duration range", ErrValidation, variant.VariantKey)
		}
	case VideoDurationDiscrete, VideoDurationFixed:
		values := normalizedPositiveDurations(variant.Duration.Values)
		if mode == VideoDurationFixed && len(values) != 1 {
			return fmt.Errorf("%w: fixed duration variant %s must contain exactly one value", ErrValidation, variant.VariantKey)
		}
		if len(values) == 0 {
			return fmt.Errorf("%w: variant %s requires positive duration values", ErrValidation, variant.VariantKey)
		}
	case VideoDurationSource:
	default:
		return fmt.Errorf("%w: variant %s has unsupported duration mode %s", ErrValidation, variant.VariantKey, mode)
	}
	support := normalizeVideoSupport(variant.NativeAudio.Support)
	if support == "" {
		return fmt.Errorf("%w: variant %s nativeAudio.support must be true, false, or unknown", ErrValidation, variant.VariantKey)
	}
	frameRateMode := strings.ToLower(strings.TrimSpace(variant.FrameRate.Mode))
	switch frameRateMode {
	case "unknown":
	case "fixed":
		if len(normalizedPositiveDurations(variant.FrameRate.Values)) != 1 {
			return fmt.Errorf("%w: fixed frame rate variant %s must contain exactly one positive value", ErrValidation, variant.VariantKey)
		}
	case "selectable":
		if len(normalizedPositiveDurations(variant.FrameRate.Values)) == 0 {
			return fmt.Errorf("%w: selectable frame rate variant %s requires positive values", ErrValidation, variant.VariantKey)
		}
	default:
		return fmt.Errorf("%w: variant %s has unsupported frame rate mode %s", ErrValidation, variant.VariantKey, frameRateMode)
	}
	return nil
}

func matchVideoGenerationVariant(variant VideoGenerationVariant, req videoVariantMatchRequest) (bool, int, string, string) {
	// Duration planning happens after this filter because it also needs dialogue
	// boundaries. Resolution is the only variant field that hard-rejects a
	// candidate here; the remaining declared capabilities are advisory ranking
	// signals and are still sent to the provider.
	if !matchesOptionalValue(variant.Resolutions, req.Resolution) {
		return false, 0, "", ""
	}

	score := 0
	requiredContract := strings.TrimSpace(req.RequiredInitialInputContract)
	if requiredContract != "" {
		switch {
		case strings.EqualFold(strings.TrimSpace(variant.InputContract.ContractKey), requiredContract):
			score += 32
		case videoInputContractSatisfies(variant.InputContract, requiredContract):
			score += 16
		}
	}
	score += videoOptionalCapabilityPreference(variant.When.TaskTypes, req.TaskType, 16)
	score += videoOptionalCapabilityPreference(variant.When.ReferenceModes, req.ReferenceMode, 8)
	score += videoOptionalCapabilityPreference(variant.AspectRatios, req.AspectRatio, 4)
	if strings.TrimSpace(req.PromptLanguage) != "" && matchesLanguage(variant.SupportedPromptLanguages, req.PromptLanguage) {
		score += 2
	}

	wantsNative := strings.EqualFold(strings.TrimSpace(req.AudioStrategy), "native_av") && !strings.EqualFold(strings.TrimSpace(req.AudioRequirement), "disabled")
	if variant.When.NativeAudioRequested != nil && *variant.When.NativeAudioRequested == wantsNative {
		score += 2
	}
	support := normalizeVideoSupport(variant.NativeAudio.Support)
	if !wantsNative {
		return true, score + 1, "not_requested", "ready"
	}
	if req.HasDialogue {
		if variant.NativeAudio.SupportsDialogue != nil && *variant.NativeAudio.SupportsDialogue {
			score += 2
		}
		if strings.TrimSpace(req.DialogueLanguage) != "" && matchesLanguage(variant.NativeAudio.SupportedDialogueLanguages, req.DialogueLanguage) {
			score++
		}
	}
	switch support {
	case VideoSupportTrue:
		return true, score + 3, "audio_unverified", "preview_only"
	case VideoSupportUnknown:
		return true, score + 2, "audio_unverified", "preview_only"
	case VideoSupportFalse:
		return true, score + 1, "native_audio_unavailable", "preview_only"
	default:
		return true, score, "audio_unverified", "preview_only"
	}
}

func videoOptionalCapabilityPreference(values []string, requested string, score int) int {
	requested = strings.TrimSpace(requested)
	if requested == "" || len(normalizeVideoStringSlice(values)) == 0 {
		return 0
	}
	if matchesOptionalValue(values, requested) {
		return score
	}
	return 0
}

func planVideoSegments(targetTicks, timebase int64, variant VideoGenerationVariant, referenceMode string, continuation *VideoInputContract) ([]GatewayVideoPlanSegment, error) {
	if targetTicks <= 0 || timebase <= 0 {
		return nil, fmt.Errorf("%w: targetDurationTicks and timelineTimebase must be positive", ErrValidation)
	}
	targetSeconds := float64(targetTicks) / float64(timebase)
	requested, err := requestedVideoDurations(targetSeconds, variant.Duration)
	if err != nil {
		return nil, err
	}
	if len(requested) > 1 && continuation == nil {
		return nil, &StandardErrorError{Standard: StandardError{
			Code: CodeStoryboardReplanRequired, Message: "selected video capability cannot preserve continuity across multiple render segments", Retryable: false,
		}}
	}
	segments := make([]GatewayVideoPlanSegment, 0, len(requested))
	remaining := targetTicks
	start := int64(0)
	for index, seconds := range requested {
		requestedTicks := int64(math.Round(seconds * float64(timebase)))
		planned := requestedTicks
		if planned > remaining || index == len(requested)-1 {
			planned = remaining
		}
		if planned <= 0 {
			return nil, fmt.Errorf("%w: video duration plan contains an empty segment", ErrValidation)
		}
		continuityMode := normalizeReferenceMode(referenceMode)
		if index > 0 {
			continuityMode = nextVideoContinuityMode(continuation)
		}
		segment := GatewayVideoPlanSegment{
			SegmentIndex: index, PlannedStartTick: start, PlannedEndTick: start + planned,
			PlannedDurationTicks: planned, PlannedDurationSeconds: float64(planned) / float64(timebase), RequestedDurationSeconds: seconds, ContinuityMode: continuityMode,
		}
		if requestedTicks > planned {
			segment.TrimEndTick = planned
		}
		segments = append(segments, segment)
		start += planned
		remaining -= planned
	}
	if remaining != 0 || start != targetTicks {
		return nil, fmt.Errorf("%w: video duration plan does not cover the target duration", ErrValidation)
	}
	return segments, nil
}

type videoDialoguePlanState struct {
	reachable       bool
	requestCount    int
	paddingTicks    int64
	firstEndFrame   int64
	previousFrame   int64
	requestedSecond float64
}

func planVideoSegmentsWithDialogue(targetTicks, timebase, frameTick int64, variant VideoGenerationVariant, referenceMode string, dialogue []GatewayVideoDialogueSpan, continuation *VideoInputContract) ([]GatewayVideoPlanSegment, error) {
	if len(dialogue) == 0 {
		return planVideoSegments(targetTicks, timebase, variant, referenceMode, continuation)
	}
	normalized, err := validateGatewayVideoDialogueSpans(dialogue, targetTicks, frameTick)
	if err != nil {
		return nil, err
	}
	requestOptions, continuous, err := videoRequestDurationOptions(variant.Duration)
	if err != nil {
		return nil, err
	}
	maxSeconds := variant.Duration.MaxSeconds
	if !continuous {
		maxSeconds = requestOptions[len(requestOptions)-1]
	}
	maxPlannedFrames := int64(math.Floor(maxSeconds * float64(timebase) / float64(frameTick)))
	targetFrames := targetTicks / frameTick
	if maxPlannedFrames <= 0 || targetFrames <= 0 {
		return nil, fmt.Errorf("%w: video capability cannot cover a frame-aligned render segment", ErrValidation)
	}
	normalized, err = splitLongGatewayVideoDialogueSpans(normalized, maxPlannedFrames*frameTick, frameTick)
	if err != nil {
		return nil, err
	}
	for _, line := range normalized {
		if line.EndTick-line.StartTick > maxPlannedFrames*frameTick {
			return nil, &StandardErrorError{Standard: StandardError{
				Code: CodeStoryboardReplanRequired, Message: "a complete dialogue turn is longer than the selected video model can generate without an unsafe split", Retryable: false,
			}}
		}
	}
	specialFrames := map[int64]bool{targetFrames: true}
	for _, line := range normalized {
		specialFrames[line.StartTick/frameTick] = true
		specialFrames[line.EndTick/frameTick] = true
	}
	states := make([]videoDialoguePlanState, targetFrames+1)
	states[0] = videoDialoguePlanState{reachable: true, previousFrame: -1}
	for startFrame := int64(0); startFrame < targetFrames; startFrame++ {
		state := states[startFrame]
		if !state.reachable || !videoDialogueBoundarySafe(startFrame*frameTick, normalized) {
			continue
		}
		candidateEnds := map[int64]bool{}
		for _, seconds := range requestOptions {
			capacityFrames := int64(math.Floor(seconds * float64(timebase) / float64(frameTick)))
			endFrame := startFrame + capacityFrames
			if endFrame > targetFrames {
				endFrame = targetFrames
			}
			if endFrame > startFrame {
				candidateEnds[endFrame] = true
			}
		}
		if continuous {
			endFrame := startFrame + maxPlannedFrames
			if endFrame > targetFrames {
				endFrame = targetFrames
			}
			candidateEnds[endFrame] = true
		}
		for boundary := range specialFrames {
			if boundary > startFrame && boundary-startFrame <= maxPlannedFrames {
				candidateEnds[boundary] = true
			}
		}
		for endFrame := range candidateEnds {
			endTick := endFrame * frameTick
			if endFrame <= startFrame || !videoDialogueBoundarySafe(endTick, normalized) {
				continue
			}
			plannedTicks := (endFrame - startFrame) * frameTick
			requestedSeconds, ok := requestDurationForPlannedTicks(plannedTicks, timebase, variant.Duration, requestOptions, continuous)
			if !ok {
				continue
			}
			requestedTicks := int64(math.Round(requestedSeconds * float64(timebase)))
			candidate := videoDialoguePlanState{
				reachable: true, requestCount: state.requestCount + 1,
				paddingTicks:  state.paddingTicks + maxInt64(0, requestedTicks-plannedTicks),
				firstEndFrame: state.firstEndFrame, previousFrame: startFrame, requestedSecond: requestedSeconds,
			}
			if startFrame == 0 {
				candidate.firstEndFrame = endFrame
			}
			if betterVideoDialoguePlanState(candidate, states[endFrame]) {
				states[endFrame] = candidate
			}
		}
	}
	if !states[targetFrames].reachable {
		return nil, &StandardErrorError{Standard: StandardError{
			Code: CodeStoryboardReplanRequired, Message: "video model duration choices cannot cover this shot without splitting a dialogue turn", Retryable: false,
		}}
	}
	type plannedEdge struct {
		startFrame, endFrame int64
		requestedSeconds     float64
	}
	edges := make([]plannedEdge, 0, states[targetFrames].requestCount)
	for endFrame := targetFrames; endFrame > 0; {
		state := states[endFrame]
		if state.previousFrame < 0 {
			return nil, fmt.Errorf("%w: dialogue-aware render plan path is incomplete", ErrValidation)
		}
		edges = append(edges, plannedEdge{startFrame: state.previousFrame, endFrame: endFrame, requestedSeconds: state.requestedSecond})
		endFrame = state.previousFrame
	}
	for left, right := 0, len(edges)-1; left < right; left, right = left+1, right-1 {
		edges[left], edges[right] = edges[right], edges[left]
	}
	if len(edges) > 1 && continuation == nil {
		return nil, &StandardErrorError{Standard: StandardError{
			Code: CodeStoryboardReplanRequired, Message: "selected video capability cannot preserve continuity across dialogue-safe render segments", Retryable: false,
		}}
	}
	segments := make([]GatewayVideoPlanSegment, 0, len(edges))
	for index, edge := range edges {
		startTick := edge.startFrame * frameTick
		endTick := edge.endFrame * frameTick
		continuityMode := normalizeReferenceMode(referenceMode)
		if index > 0 {
			continuityMode = nextVideoContinuityMode(continuation)
		}
		segment := GatewayVideoPlanSegment{
			SegmentIndex: index, PlannedStartTick: startTick, PlannedEndTick: endTick,
			PlannedDurationTicks: endTick - startTick, PlannedDurationSeconds: float64(endTick-startTick) / float64(timebase), RequestedDurationSeconds: edge.requestedSeconds,
			ContinuityMode: continuityMode, DialogueSpans: dialogueSpansForRenderSegment(normalized, startTick, endTick),
		}
		if requestedTicks := int64(math.Round(edge.requestedSeconds * float64(timebase))); requestedTicks > segment.PlannedDurationTicks {
			segment.TrimEndTick = segment.PlannedDurationTicks
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func splitLongGatewayVideoDialogueSpans(dialogue []GatewayVideoDialogueSpan, maxTicks, frameTick int64) ([]GatewayVideoDialogueSpan, error) {
	result := make([]GatewayVideoDialogueSpan, 0, len(dialogue))
	for _, line := range dialogue {
		if line.EndTick-line.StartTick <= maxTicks {
			result = append(result, line)
			continue
		}
		parts, err := splitGatewayVideoDialogueSpan(line, maxTicks, frameTick)
		if err != nil {
			return nil, err
		}
		result = append(result, parts...)
	}
	return result, nil
}

type weightedDialogueClause struct {
	text   string
	weight int64
}

func splitGatewayVideoDialogueSpan(line GatewayVideoDialogueSpan, maxTicks, frameTick int64) ([]GatewayVideoDialogueSpan, error) {
	clauses := gatewayVideoDialogueClauses(line.Text)
	if len(clauses) < 2 || maxTicks <= 0 || frameTick <= 0 {
		return nil, storyboardDialogueSplitRequiredError()
	}
	totalFrames := (line.EndTick - line.StartTick) / frameTick
	maxFrames := maxTicks / frameTick
	if totalFrames <= 0 || maxFrames <= 0 {
		return nil, storyboardDialogueSplitRequiredError()
	}
	totalWeight := int64(0)
	for _, clause := range clauses {
		totalWeight += clause.weight
	}
	if totalWeight <= 0 {
		return nil, storyboardDialogueSplitRequiredError()
	}

	type dialogueBoundary struct {
		clauseIndex int
		frame       int64
	}
	boundaries := make([]dialogueBoundary, 0, len(clauses)-1)
	cumulativeWeight := int64(0)
	previousFrame := int64(0)
	for index := 0; index < len(clauses)-1; index++ {
		cumulativeWeight += clauses[index].weight
		frame := int64(math.Round(float64(totalFrames) * float64(cumulativeWeight) / float64(totalWeight)))
		if frame <= previousFrame {
			frame = previousFrame + 1
		}
		if frame >= totalFrames {
			continue
		}
		boundaries = append(boundaries, dialogueBoundary{clauseIndex: index + 1, frame: frame})
		previousFrame = frame
	}
	if len(boundaries) == 0 {
		return nil, storyboardDialogueSplitRequiredError()
	}

	parts := make([]GatewayVideoDialogueSpan, 0, int(math.Ceil(float64(totalFrames)/float64(maxFrames))))
	startClause := 0
	startFrame := int64(0)
	for startFrame < totalFrames {
		if totalFrames-startFrame <= maxFrames {
			parts = append(parts, gatewayVideoDialoguePart(line, clauses, startClause, len(clauses), startFrame, totalFrames, len(parts), frameTick))
			break
		}
		selected := dialogueBoundary{clauseIndex: -1}
		for _, boundary := range boundaries {
			if boundary.clauseIndex <= startClause || boundary.frame <= startFrame {
				continue
			}
			if boundary.frame-startFrame > maxFrames {
				break
			}
			selected = boundary
		}
		if selected.clauseIndex < 0 {
			return nil, storyboardDialogueSplitRequiredError()
		}
		parts = append(parts, gatewayVideoDialoguePart(line, clauses, startClause, selected.clauseIndex, startFrame, selected.frame, len(parts), frameTick))
		startClause = selected.clauseIndex
		startFrame = selected.frame
	}
	for index := range parts {
		parts[index].ContinuesFromPrevious = index > 0 || line.ContinuesFromPrevious
		parts[index].ContinuesToNext = index < len(parts)-1 || line.ContinuesToNext
	}
	return parts, nil
}

func gatewayVideoDialogueClauses(text string) []weightedDialogueClause {
	clauses := make([]weightedDialogueClause, 0)
	var builder strings.Builder
	flush := func() {
		value := strings.TrimSpace(builder.String())
		builder.Reset()
		if value == "" {
			return
		}
		weight := int64(0)
		for _, char := range value {
			if !unicode.IsSpace(char) {
				weight++
			}
		}
		clauses = append(clauses, weightedDialogueClause{text: value, weight: maxInt64(1, weight)})
	}
	for _, char := range strings.TrimSpace(text) {
		builder.WriteRune(char)
		if strings.ContainsRune("，。！？；：,.!?;:", char) {
			flush()
		}
	}
	flush()
	return clauses
}

func gatewayVideoDialoguePart(
	line GatewayVideoDialogueSpan,
	clauses []weightedDialogueClause,
	startClause, endClause int,
	startFrame, endFrame int64,
	partIndex int,
	frameTick int64,
) GatewayVideoDialogueSpan {
	var text strings.Builder
	for _, clause := range clauses[startClause:endClause] {
		text.WriteString(clause.text)
	}
	part := line
	part.Text = text.String()
	part.StartTick = line.StartTick + startFrame*frameTick
	part.EndTick = line.StartTick + endFrame*frameTick
	if strings.TrimSpace(line.TimingUnitID) != "" {
		part.TimingUnitID = fmt.Sprintf("%s:part:%d", line.TimingUnitID, partIndex+1)
	}
	return part
}

func storyboardDialogueSplitRequiredError() error {
	return &StandardErrorError{Standard: StandardError{
		Code: CodeStoryboardReplanRequired, Message: "a complete dialogue turn is longer than the selected video model can generate without an unsafe split", Retryable: false,
	}}
}

func validateGatewayVideoDialogueSpans(dialogue []GatewayVideoDialogueSpan, targetTicks, frameTick int64) ([]GatewayVideoDialogueSpan, error) {
	if frameTick <= 0 {
		return nil, fmt.Errorf("%w: frameTick must be positive", ErrValidation)
	}
	result := append([]GatewayVideoDialogueSpan(nil), dialogue...)
	for index := range result {
		line := &result[index]
		line.Speaker = strings.TrimSpace(line.Speaker)
		line.Text = strings.TrimSpace(line.Text)
		line.Delivery = strings.TrimSpace(line.Delivery)
		line.Kind = strings.ToLower(strings.TrimSpace(line.Kind))
		if line.Kind == "" {
			line.Kind = "dialogue"
		}
		if line.Kind != "dialogue" && line.Kind != "voiceover" && line.Kind != "narration" {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeStoryboardReplanRequired, Message: "视频台词片段只能包含角色对白、旁白或解说，非语言音效必须使用独立音效契约", Retryable: false}}
		}
		if line.Text == "" || (line.Kind == "dialogue" && line.Speaker == "") || line.StartTick < 0 || line.EndTick <= line.StartTick || line.EndTick > targetTicks {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeStoryboardReplanRequired, Message: "视频音轨片段必须包含准确文本和有效的镜头内时间范围；角色对白还必须包含说话人", Retryable: false}}
		}
		if line.StartTick%frameTick != 0 || line.EndTick%frameTick != 0 {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeStoryboardReplanRequired, Message: "视频音轨片段必须与分镜帧边界对齐，需要重新生成分镜计划", Retryable: false}}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].StartTick == result[right].StartTick {
			return result[left].EndTick < result[right].EndTick
		}
		return result[left].StartTick < result[right].StartTick
	})
	return result, nil
}

func videoRequestDurationOptions(capability VideoDurationCapability) ([]float64, bool, error) {
	switch strings.TrimSpace(capability.Mode) {
	case VideoDurationContinuousRange:
		if capability.MinSeconds <= 0 || capability.MaxSeconds < capability.MinSeconds {
			return nil, false, fmt.Errorf("%w: invalid continuous video duration range", ErrValidation)
		}
		if capability.StepSeconds <= 0 {
			values, err := wholeSecondVideoDurations(capability)
			return values, false, err
		}
		values := make([]float64, 0)
		for value, count := capability.MinSeconds, 0; value <= capability.MaxSeconds+1e-9 && count < 1000; value, count = value+capability.StepSeconds, count+1 {
			values = append(values, value)
		}
		if len(values) == 0 || values[len(values)-1] < capability.MaxSeconds-1e-9 {
			values = append(values, capability.MaxSeconds)
		}
		return normalizedPositiveDurations(values), false, nil
	case VideoDurationDiscrete, VideoDurationFixed:
		values := normalizedPositiveDurations(capability.Values)
		if len(values) == 0 {
			return nil, false, fmt.Errorf("%w: video duration values are required", ErrValidation)
		}
		return values, false, nil
	case VideoDurationSource:
		return nil, false, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "source-duration video generation requires an input video duration", Retryable: false}}
	default:
		return nil, false, fmt.Errorf("%w: unsupported video duration mode", ErrValidation)
	}
}

func requestDurationForPlannedTicks(plannedTicks, timebase int64, capability VideoDurationCapability, options []float64, continuous bool) (float64, bool) {
	plannedSeconds := float64(plannedTicks) / float64(timebase)
	if continuous {
		if plannedSeconds > capability.MaxSeconds+1e-9 {
			return 0, false
		}
		requested := math.Ceil(math.Max(plannedSeconds, capability.MinSeconds) - 1e-9)
		if requested > capability.MaxSeconds+1e-9 {
			return 0, false
		}
		return requested, true
	}
	for _, option := range options {
		if option+1e-9 >= plannedSeconds {
			return option, true
		}
	}
	return 0, false
}

func betterVideoDialoguePlanState(candidate, current videoDialoguePlanState) bool {
	if !current.reachable {
		return true
	}
	if candidate.requestCount != current.requestCount {
		return candidate.requestCount < current.requestCount
	}
	if candidate.paddingTicks != current.paddingTicks {
		return candidate.paddingTicks < current.paddingTicks
	}
	return candidate.firstEndFrame > current.firstEndFrame
}

func videoDialogueBoundarySafe(tick int64, dialogue []GatewayVideoDialogueSpan) bool {
	for _, line := range dialogue {
		if tick > line.StartTick && tick < line.EndTick {
			return false
		}
	}
	return true
}

func dialogueSpansForRenderSegment(dialogue []GatewayVideoDialogueSpan, startTick, endTick int64) []GatewayVideoDialogueSpan {
	result := make([]GatewayVideoDialogueSpan, 0)
	for _, line := range dialogue {
		if line.StartTick < startTick || line.EndTick > endTick {
			continue
		}
		line.StartTick -= startTick
		line.EndTick -= startTick
		result = append(result, line)
	}
	return result
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func requestedVideoDurations(targetSeconds float64, capability VideoDurationCapability) ([]float64, error) {
	if targetSeconds <= 0 {
		return nil, fmt.Errorf("%w: target video duration must be positive", ErrValidation)
	}
	switch strings.TrimSpace(capability.Mode) {
	case VideoDurationContinuousRange:
		return planContinuousDurations(targetSeconds, capability)
	case VideoDurationDiscrete, VideoDurationFixed:
		return planDiscreteDurations(targetSeconds, normalizedPositiveDurations(capability.Values))
	case VideoDurationSource:
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "source-duration video generation requires an input video duration", Retryable: false}}
	default:
		return nil, fmt.Errorf("%w: unsupported video duration mode", ErrValidation)
	}
}

func planContinuousDurations(target float64, capability VideoDurationCapability) ([]float64, error) {
	minValue := capability.MinSeconds
	maxValue := capability.MaxSeconds
	if minValue <= 0 || maxValue < minValue {
		return nil, fmt.Errorf("%w: invalid continuous video duration range", ErrValidation)
	}
	step := capability.StepSeconds
	if step > 0 {
		values := make([]float64, 0)
		for value, count := minValue, 0; value <= maxValue+1e-9 && count < 1000; value, count = value+step, count+1 {
			values = append(values, roundVideoDuration(value))
		}
		if len(values) == 0 || values[len(values)-1] < maxValue-1e-9 {
			values = append(values, maxValue)
		}
		return planDiscreteDurations(target, normalizedPositiveDurations(values))
	}
	values, err := wholeSecondVideoDurations(capability)
	if err != nil {
		return nil, err
	}
	return planDiscreteDurations(target, values)
}

func wholeSecondVideoDurations(capability VideoDurationCapability) ([]float64, error) {
	minimum := int(math.Ceil(capability.MinSeconds - 1e-9))
	maximum := int(math.Floor(capability.MaxSeconds + 1e-9))
	if minimum < 1 {
		minimum = 1
	}
	if maximum < minimum {
		return nil, &StandardErrorError{Standard: StandardError{
			Code: CodeModelCapabilityUnavailable, Message: "continuous video duration range has no whole-second request value; configure an explicit fractional step when the provider supports one", Retryable: false,
		}}
	}
	values := make([]float64, 0, maximum-minimum+1)
	for value := minimum; value <= maximum; value++ {
		values = append(values, float64(value))
	}
	return values, nil
}

func planDiscreteDurations(target float64, values []float64) ([]float64, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: discrete video duration values are required", ErrValidation)
	}
	const unitsPerSecond = 1000
	targetUnits := int(math.Ceil(target * unitsPerSecond))
	valueUnits := make([]int, 0, len(values))
	valueByUnits := map[int]float64{}
	maxUnits := 0
	for _, value := range values {
		units := int(math.Round(value * unitsPerSecond))
		if units <= 0 {
			continue
		}
		valueUnits = append(valueUnits, units)
		valueByUnits[units] = value
		if units > maxUnits {
			maxUnits = units
		}
	}
	if len(valueUnits) == 0 {
		return nil, fmt.Errorf("%w: discrete video duration values are required", ErrValidation)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(valueUnits)))
	limit := targetUnits + maxUnits
	const unreachable = int(^uint(0) >> 1)
	counts := make([]int, limit+1)
	previous := make([]int, limit+1)
	used := make([]int, limit+1)
	for index := 1; index <= limit; index++ {
		counts[index] = unreachable
		previous[index] = -1
	}
	for total := 0; total <= limit; total++ {
		if counts[total] == unreachable {
			continue
		}
		for _, units := range valueUnits {
			next := total + units
			if next > limit {
				continue
			}
			if counts[total]+1 < counts[next] {
				counts[next] = counts[total] + 1
				previous[next] = total
				used[next] = units
			}
		}
	}
	best := -1
	for total := targetUnits; total <= limit; total++ {
		if counts[total] == unreachable {
			continue
		}
		if best == -1 || total < best || (total == best && counts[total] < counts[best]) {
			best = total
		}
		if best == targetUnits {
			break
		}
	}
	if best < 0 {
		return nil, &StandardErrorError{Standard: StandardError{Code: CodeModelCapabilityUnavailable, Message: "target duration cannot be represented by the selected discrete values", Retryable: false}}
	}
	result := make([]float64, 0, counts[best])
	for cursor := best; cursor > 0; cursor = previous[cursor] {
		units := used[cursor]
		if units <= 0 {
			return nil, fmt.Errorf("%w: invalid discrete duration plan", ErrValidation)
		}
		result = append(result, valueByUnits[units])
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(result)))
	return result, nil
}

func capabilitySnapshotHash(variant VideoGenerationVariant) (string, error) {
	raw, err := json.Marshal(variant)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func variantSupportsContinuation(variant VideoGenerationVariant) bool {
	return len(variant.ContinuationInputContracts) > 0
}

func selectVideoContinuationInputContract(variant VideoGenerationVariant, allowed []string) (*VideoInputContract, error) {
	allowed = normalizeVideoStringSlice(allowed)
	for _, required := range allowed {
		for _, candidate := range variant.ContinuationInputContracts {
			if strings.EqualFold(candidate.ContractKey, required) {
				selected := candidate
				return &selected, nil
			}
		}
	}
	return nil, nil
}

func nextVideoContinuityMode(contract *VideoInputContract) string {
	if contract == nil {
		return "none"
	}
	switch contract.ContractKey {
	case VideoInputContractVideoExtension:
		return "video_extension"
	case VideoInputContractFirstFrame, VideoInputContractFirstFramePlusReferences:
		return "previous_segment_tail"
	default:
		return "none"
	}
}

func inferVideoModelFamily(modelKey string) string {
	value := strings.ToLower(strings.TrimSpace(modelKey))
	for _, separator := range []string{"/", ":"} {
		if index := strings.Index(value, separator); index > 0 {
			value = value[:index]
		}
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	if len(parts) == 0 {
		return "unknown"
	}
	if len(parts) > 1 && (parts[0] == "grok" || parts[0] == "veo" || parts[0] == "sora" || parts[0] == "kling" || parts[0] == "seedance") {
		return parts[0]
	}
	return parts[0]
}

func normalizeVideoSupport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "supported", "yes":
		return VideoSupportTrue
	case "false", "unsupported", "no":
		return VideoSupportFalse
	case "unknown", "":
		return VideoSupportUnknown
	default:
		return ""
	}
}

func normalizeReferenceMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto", "image", "image_to_video", "first_frame":
		return "first_frame"
	case "custom":
		return "first_frame"
	case "none", "text_to_video":
		return "none"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func matchesOptionalValue(values []string, value string) bool {
	if len(values) == 0 || strings.TrimSpace(value) == "" {
		return true
	}
	want := strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range values {
		if strings.ToLower(strings.TrimSpace(candidate)) == want {
			return true
		}
	}
	return false
}

func matchesLanguage(values []string, value string) bool {
	if len(values) == 0 || strings.TrimSpace(value) == "" {
		return true
	}
	want := strings.ToLower(strings.TrimSpace(value))
	base := strings.SplitN(want, "-", 2)[0]
	for _, candidate := range values {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		if normalized == "*" || normalized == want || normalized == base || strings.SplitN(normalized, "-", 2)[0] == base {
			return true
		}
	}
	return false
}

func normalizedPositiveDurations(values []float64) []float64 {
	seen := map[int64]bool{}
	result := make([]float64, 0, len(values))
	for _, value := range values {
		if value <= 0 || math.IsInf(value, 0) || math.IsNaN(value) {
			continue
		}
		value = roundVideoDuration(value)
		key := int64(math.Round(value * 1000))
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.Float64s(result)
	return result
}

func roundVideoDuration(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func stringSliceFromAny(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	if direct, ok := value.([]string); ok {
		return append([]string(nil), direct...)
	}
	return result
}

func appendUniqueVideoString(values []string, value string) []string {
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(value)) {
			return values
		}
	}
	return append(values, value)
}
