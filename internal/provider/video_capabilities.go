package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	VideoDurationContinuousRange = "continuous_range"
	VideoDurationDiscrete        = "discrete"
	VideoDurationFixed           = "fixed"
	VideoDurationSource          = "source_duration"

	VideoSupportTrue    = "true"
	VideoSupportFalse   = "false"
	VideoSupportUnknown = "unknown"
)

type VideoGenerationVariant struct {
	VariantKey               string                      `json:"variantKey"`
	ModelFamily              string                      `json:"modelFamily,omitempty"`
	When                     VideoGenerationVariantWhen  `json:"when"`
	Duration                 VideoDurationCapability     `json:"duration"`
	Resolutions              []string                    `json:"resolutions,omitempty"`
	AspectRatios             []string                    `json:"aspectRatios,omitempty"`
	FrameRate                VideoFrameRateCapability    `json:"frameRate"`
	SupportedPromptLanguages []string                    `json:"supportedPromptLanguages,omitempty"`
	NativeAudio              VideoNativeAudioCapability  `json:"nativeAudio"`
	Continuation             VideoContinuationCapability `json:"continuation"`
	RequestModes             []string                    `json:"requestModes,omitempty"`
	Source                   string                      `json:"source,omitempty"`
	SourceURL                string                      `json:"sourceUrl,omitempty"`
	VerifiedAt               string                      `json:"verifiedAt,omitempty"`
	CapabilityVersion        string                      `json:"capabilityVersion,omitempty"`
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
	OrganizationID          string                     `json:"organizationId"`
	ProjectID               string                     `json:"projectId"`
	WorkflowRunID           string                     `json:"workflowRunId,omitempty"`
	NodeRunID               string                     `json:"nodeRunId,omitempty"`
	NodeExecutionToken      string                     `json:"nodeExecutionToken,omitempty"`
	NodeAttemptGeneration   int                        `json:"nodeAttemptGeneration,omitempty"`
	StoryboardPlanID        string                     `json:"storyboardPlanId,omitempty"`
	StoryboardShotID        string                     `json:"storyboardShotId"`
	ModelProfileKey         string                     `json:"modelProfileKey,omitempty"`
	ProviderModelID         string                     `json:"providerModelId,omitempty"`
	TaskType                string                     `json:"taskType"`
	TargetDurationTicks     int64                      `json:"targetDurationTicks"`
	TimelineTimebase        int64                      `json:"timelineTimebase"`
	FPSNumerator            int64                      `json:"fpsNumerator"`
	FPSDenominator          int64                      `json:"fpsDenominator"`
	AudioStrategy           string                     `json:"audioStrategy"`
	AudioRequirement        string                     `json:"audioRequirement"`
	DialogueLanguage        string                     `json:"dialogueLanguage,omitempty"`
	HasDialogue             bool                       `json:"hasDialogue"`
	ReferenceMode           string                     `json:"referenceMode"`
	AspectRatio             string                     `json:"aspectRatio"`
	Resolution              string                     `json:"resolution"`
	PromptLanguage          string                     `json:"promptLanguage,omitempty"`
	ExpiresInSeconds        int                        `json:"expiresInSeconds,omitempty"`
	Force                   bool                       `json:"force,omitempty"`
	ExcludeProviderModelIDs []string                   `json:"excludeProviderModelIds,omitempty"`
	PreviousExecutionPlanID string                     `json:"previousExecutionPlanId,omitempty"`
	DialogueSpans           []GatewayVideoDialogueSpan `json:"dialogueSpans,omitempty"`
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
	TrimEndTick              int64                      `json:"trimEndTick,omitempty"`
	DialogueSpans            []GatewayVideoDialogueSpan `json:"dialogueSpans,omitempty"`
}

type GatewayVideoPlanResponse struct {
	ExecutionPlanID        string                    `json:"executionPlanId"`
	ProviderModelID        string                    `json:"providerModelId"`
	ProviderAccountID      string                    `json:"providerAccountId"`
	ModelFamily            string                    `json:"modelFamily"`
	VariantKey             string                    `json:"variantKey"`
	CapabilitySnapshot     VideoGenerationVariant    `json:"capabilitySnapshot"`
	CapabilitySnapshotHash string                    `json:"capabilitySnapshotHash"`
	TimelineTimebase       int64                     `json:"timelineTimebase"`
	FPSNumerator           int64                     `json:"fpsNumerator"`
	FPSDenominator         int64                     `json:"fpsDenominator"`
	ExpiresAt              string                    `json:"expiresAt"`
	AudioStrategy          string                    `json:"audioStrategy"`
	AudioRequirement       string                    `json:"audioRequirement"`
	NativeAudioStatus      string                    `json:"nativeAudioStatus"`
	ProductionReadiness    string                    `json:"productionReadiness"`
	Segments               []GatewayVideoPlanSegment `json:"segments"`
}

type GatewayVideoRetrySegmentRequest struct {
	OrganizationID        string `json:"organizationId"`
	ProjectID             string `json:"projectId"`
	WorkflowRunID         string `json:"workflowRunId"`
	NodeRunID             string `json:"nodeRunId"`
	NodeExecutionToken    string `json:"nodeExecutionToken"`
	NodeAttemptGeneration int    `json:"nodeAttemptGeneration"`
	ExecutionPlanID       string `json:"executionPlanId"`
	RenderSegmentID       string `json:"renderSegmentId"`
	FailureCode           string `json:"failureCode,omitempty"`
	FailureMessage        string `json:"failureMessage,omitempty"`
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
	TaskType         string
	ReferenceMode    string
	AspectRatio      string
	Resolution       string
	PromptLanguage   string
	DialogueLanguage string
	HasDialogue      bool
	AudioStrategy    string
	AudioRequirement string
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
			continue
		}
		if legacy, ok := legacyVideoGenerationVariant(xCapabilities, capability, model); ok {
			variants = append(variants, legacy)
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
		if err := validateVideoGenerationVariant(*variant); err != nil {
			return nil, err
		}
	}
	return variants, nil
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
	if !matchesOptionalValue(variant.When.TaskTypes, req.TaskType) || !matchesOptionalValue(variant.When.ReferenceModes, req.ReferenceMode) {
		return false, 0, "", ""
	}
	if !matchesOptionalValue(variant.AspectRatios, req.AspectRatio) || !matchesOptionalValue(variant.Resolutions, req.Resolution) {
		return false, 0, "", ""
	}
	if !matchesLanguage(variant.SupportedPromptLanguages, req.PromptLanguage) {
		return false, 0, "", ""
	}
	wantsNative := strings.EqualFold(strings.TrimSpace(req.AudioStrategy), "native_av") && !strings.EqualFold(strings.TrimSpace(req.AudioRequirement), "disabled")
	if variant.When.NativeAudioRequested != nil && *variant.When.NativeAudioRequested != wantsNative {
		return false, 0, "", ""
	}
	support := normalizeVideoSupport(variant.NativeAudio.Support)
	if !wantsNative {
		if variant.NativeAudio.CanDisable != nil && !*variant.NativeAudio.CanDisable && support == VideoSupportTrue {
			return false, 0, "", ""
		}
		return true, 1, "not_requested", "ready"
	}
	if strings.EqualFold(strings.TrimSpace(req.AudioRequirement), "required") && support != VideoSupportTrue {
		return false, 0, "", ""
	}
	if req.HasDialogue {
		if support == VideoSupportTrue && variant.NativeAudio.SupportsDialogue != nil && !*variant.NativeAudio.SupportsDialogue {
			return false, 0, "", ""
		}
		if support == VideoSupportTrue && !matchesLanguage(variant.NativeAudio.SupportedDialogueLanguages, req.DialogueLanguage) {
			return false, 0, "", ""
		}
	}
	switch support {
	case VideoSupportTrue:
		return true, 3, "audio_unverified", "preview_only"
	case VideoSupportUnknown:
		return true, 2, "audio_unverified", "preview_only"
	case VideoSupportFalse:
		if strings.EqualFold(strings.TrimSpace(req.AudioRequirement), "required") {
			return false, 0, "", ""
		}
		return true, 1, "native_audio_unavailable", "preview_only"
	default:
		return false, 0, "", ""
	}
}

func planVideoSegments(targetTicks, timebase int64, variant VideoGenerationVariant, referenceMode string) ([]GatewayVideoPlanSegment, error) {
	if targetTicks <= 0 || timebase <= 0 {
		return nil, fmt.Errorf("%w: targetDurationTicks and timelineTimebase must be positive", ErrValidation)
	}
	targetSeconds := float64(targetTicks) / float64(timebase)
	requested, err := requestedVideoDurations(targetSeconds, variant.Duration)
	if err != nil {
		return nil, err
	}
	if len(requested) > 1 && !variantSupportsContinuation(variant) {
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
			continuityMode = nextVideoContinuityMode(variant)
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

func planVideoSegmentsWithDialogue(targetTicks, timebase, frameTick int64, variant VideoGenerationVariant, referenceMode string, dialogue []GatewayVideoDialogueSpan) ([]GatewayVideoPlanSegment, error) {
	if len(dialogue) == 0 {
		return planVideoSegments(targetTicks, timebase, variant, referenceMode)
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
	if len(edges) > 1 && !variantSupportsContinuation(variant) {
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
			continuityMode = nextVideoContinuityMode(variant)
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
		line.Kind = strings.TrimSpace(line.Kind)
		if line.Speaker == "" || line.Text == "" || line.StartTick < 0 || line.EndTick <= line.StartTick || line.EndTick > targetTicks {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeStoryboardReplanRequired, Message: "video dialogue spans must contain exact text and valid shot-relative timing", Retryable: false}}
		}
		if line.StartTick%frameTick != 0 || line.EndTick%frameTick != 0 {
			return nil, &StandardErrorError{Standard: StandardError{Code: CodeStoryboardReplanRequired, Message: "video dialogue spans must align to storyboard frame boundaries", Retryable: false}}
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
	continuation := variant.Continuation
	return continuation.SupportsExtension || (continuation.SupportsFirstFrame && continuation.SupportsLastFrame) || continuation.SupportsVideoReference
}

func nextVideoContinuityMode(variant VideoGenerationVariant) string {
	switch {
	case variant.Continuation.SupportsExtension:
		return "extension"
	case variant.Continuation.SupportsFirstFrame && variant.Continuation.SupportsLastFrame:
		return "previous_last_frame"
	case variant.Continuation.SupportsVideoReference:
		return "previous_segment"
	default:
		return "none"
	}
}

func legacyVideoGenerationVariant(xCapabilities map[string]any, capability Capability, model Model) (VideoGenerationVariant, bool) {
	durations := floatSliceFromAny(xCapabilities["durations"])
	minDuration := positiveFloatFromAny(xCapabilities["minDurationSeconds"])
	maxDuration := positiveFloatFromAny(xCapabilities["maxDurationSeconds"])
	duration := VideoDurationCapability{}
	switch {
	case len(durations) == 1:
		duration = VideoDurationCapability{Mode: VideoDurationFixed, Values: durations}
	case len(durations) > 1:
		duration = VideoDurationCapability{Mode: VideoDurationDiscrete, Values: durations}
	case minDuration > 0 && maxDuration >= minDuration:
		duration = VideoDurationCapability{Mode: VideoDurationContinuousRange, MinSeconds: minDuration, MaxSeconds: maxDuration}
	default:
		return VideoGenerationVariant{}, false
	}
	taskTypes := stringSliceFromRaw(capability.TaskTypes)
	referenceModes := []string{"none"}
	supportsFirst := boolFromAny(xCapabilities["supportsFirstFrame"])
	supportsLast := boolFromAny(xCapabilities["supportsLastFrame"])
	supportsVideo := boolFromAny(xCapabilities["supportsVideoReference"])
	if boolFromAny(xCapabilities["supportsReferenceImages"]) || supportsFirst {
		referenceModes = append(referenceModes, "first_frame")
		taskTypes = appendUniqueVideoString(taskTypes, "video.image_to_video")
	}
	taskTypes = appendUniqueVideoString(taskTypes, "video.text_to_video")
	if supportsLast {
		referenceModes = append(referenceModes, "last_frame")
	}
	if supportsVideo {
		referenceModes = append(referenceModes, "video_reference")
	}
	return VideoGenerationVariant{
		VariantKey:   "default",
		ModelFamily:  inferVideoModelFamily(model.ModelKey),
		When:         VideoGenerationVariantWhen{TaskTypes: taskTypes, ReferenceModes: referenceModes},
		Duration:     duration,
		Resolutions:  stringSliceFromAny(xCapabilities["supportedResolutions"]),
		AspectRatios: stringSliceFromAny(xCapabilities["supportedAspectRatios"]),
		FrameRate:    VideoFrameRateCapability{Mode: "unknown"},
		NativeAudio:  VideoNativeAudioCapability{Support: VideoSupportUnknown},
		Continuation: VideoContinuationCapability{SupportsFirstFrame: supportsFirst, SupportsLastFrame: supportsLast, SupportsVideoReference: supportsVideo},
		RequestModes: stringSliceFromAny(xCapabilities["requestModes"]),
		Source:       "derived_legacy", CapabilityVersion: "1",
	}, true
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

func stringSliceFromRaw(raw json.RawMessage) []string {
	var values []string
	_ = json.Unmarshal(raw, &values)
	return values
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

func floatSliceFromAny(value any) []float64 {
	items, _ := value.([]any)
	result := make([]float64, 0, len(items))
	for _, item := range items {
		if number := positiveFloatFromAny(item); number > 0 {
			result = append(result, number)
		}
	}
	return normalizedPositiveDurations(result)
}

func positiveFloatFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return typed
		}
	case int:
		if typed > 0 {
			return float64(typed)
		}
	case json.Number:
		parsed, _ := typed.Float64()
		if parsed > 0 {
			return parsed
		}
	}
	return 0
}

func boolFromAny(value any) bool {
	result, _ := value.(bool)
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
