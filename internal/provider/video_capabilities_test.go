package provider

import (
	"errors"
	"testing"
)

func TestPlanVideoSegmentsContinuousRangePreservesExactTarget(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:   "continuous",
		Duration:     VideoDurationCapability{Mode: VideoDurationContinuousRange, MinSeconds: 1, MaxSeconds: 8},
		NativeAudio:  VideoNativeAudioCapability{Support: VideoSupportUnknown},
		Continuation: VideoContinuationCapability{SupportsExtension: true},
	}
	segments, err := planVideoSegments(12*90000, 90000, variant, "first_frame")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].RequestedDurationSeconds != 8 || segments[1].RequestedDurationSeconds != 4 {
		t.Fatalf("segments = %+v", segments)
	}
	if segments[0].ContinuityMode != "first_frame" || segments[1].ContinuityMode != "extension" || segments[1].PlannedEndTick != 12*90000 {
		t.Fatalf("continuity/coverage = %+v", segments)
	}
}

func TestPlanVideoSegmentsContinuousRangeDefaultsToWholeSecondRequests(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:   "continuous-whole-seconds",
		Duration:     VideoDurationCapability{Mode: VideoDurationContinuousRange, MinSeconds: 1, MaxSeconds: 8},
		NativeAudio:  VideoNativeAudioCapability{Support: VideoSupportUnknown},
		Continuation: VideoContinuationCapability{SupportsExtension: true},
	}
	segments, err := planVideoSegments(382500, 90000, variant, "first_frame")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].RequestedDurationSeconds != 5 || segments[0].PlannedDurationTicks != 382500 || segments[0].TrimEndTick != 382500 {
		t.Fatalf("whole-second segment = %+v", segments)
	}
}

func TestPlanVideoSegmentsPreservesExplicitFractionalStep(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:   "continuous-half-seconds",
		Duration:     VideoDurationCapability{Mode: VideoDurationContinuousRange, MinSeconds: 1, MaxSeconds: 8, StepSeconds: 0.5},
		NativeAudio:  VideoNativeAudioCapability{Support: VideoSupportUnknown},
		Continuation: VideoContinuationCapability{SupportsExtension: true},
	}
	segments, err := planVideoSegments(382500, 90000, variant, "first_frame")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].RequestedDurationSeconds != 4.5 {
		t.Fatalf("fractional-step segment = %+v", segments)
	}
}

func TestPlanVideoSegmentsDiscreteUsesSmallestNonShortCombination(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:   "discrete",
		Duration:     VideoDurationCapability{Mode: VideoDurationDiscrete, Values: []float64{4, 8}},
		NativeAudio:  VideoNativeAudioCapability{Support: VideoSupportFalse},
		Continuation: VideoContinuationCapability{SupportsFirstFrame: true, SupportsLastFrame: true},
	}
	segments, err := planVideoSegments(10*90000, 90000, variant, "first_frame")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].RequestedDurationSeconds != 8 || segments[1].RequestedDurationSeconds != 4 {
		t.Fatalf("segments = %+v", segments)
	}
	if segments[1].PlannedDurationTicks != 2*90000 || segments[1].TrimEndTick != 2*90000 {
		t.Fatalf("last segment trim = %+v", segments[1])
	}
}

func TestPlanVideoSegmentsRequiresStoryboardReplanWithoutContinuity(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:  "fixed",
		Duration:    VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{5}},
		NativeAudio: VideoNativeAudioCapability{Support: VideoSupportFalse},
	}
	_, err := planVideoSegments(9*90000, 90000, variant, "first_frame")
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, CodeStoryboardReplanRequired)
	}
}

func TestPlanVideoSegmentsMovesBoundaryToPreserveDialogueTurn(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey: "discrete", Duration: VideoDurationCapability{Mode: VideoDurationDiscrete, Values: []float64{4, 8}},
		Continuation: VideoContinuationCapability{SupportsFirstFrame: true, SupportsLastFrame: true},
	}
	segments, err := planVideoSegmentsWithDialogue(10*90000, 90000, 3750, variant, "first_frame", []GatewayVideoDialogueSpan{
		{TimingUnitID: "dialogue-1", Speaker: "方源", Text: "这一句必须完整保留。", StartTick: 6 * 90000, EndTick: 9 * 90000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].PlannedEndTick != 6*90000 || segments[0].RequestedDurationSeconds != 8 || segments[1].RequestedDurationSeconds != 4 {
		t.Fatalf("dialogue-safe segments = %+v", segments)
	}
	if len(segments[0].DialogueSpans) != 0 || len(segments[1].DialogueSpans) != 1 || segments[1].DialogueSpans[0].StartTick != 0 || segments[1].DialogueSpans[0].EndTick != 3*90000 {
		t.Fatalf("dialogue assignment = %+v", segments)
	}
}

func TestPlanVideoSegmentsRejectsDialogueLongerThanProviderCapacity(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey: "discrete", Duration: VideoDurationCapability{Mode: VideoDurationDiscrete, Values: []float64{4, 8}},
		Continuation: VideoContinuationCapability{SupportsFirstFrame: true, SupportsLastFrame: true},
	}
	_, err := planVideoSegmentsWithDialogue(10*90000, 90000, 3750, variant, "first_frame", []GatewayVideoDialogueSpan{
		{TimingUnitID: "dialogue-1", Speaker: "方源", Text: "无法安全切开的长台词", StartTick: 0, EndTick: 9 * 90000},
	})
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, CodeStoryboardReplanRequired)
	}
}

func TestMatchVideoGenerationVariantKeepsNativeAudioUnknownDistinct(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:   "native-unknown",
		When:         VideoGenerationVariantWhen{TaskTypes: []string{"video.image_to_video"}, ReferenceModes: []string{"first_frame"}},
		Duration:     VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{5}},
		AspectRatios: []string{"16:9"}, Resolutions: []string{"720p"},
		SupportedPromptLanguages: []string{"zh-CN"},
		NativeAudio:              VideoNativeAudioCapability{Support: VideoSupportUnknown},
	}
	matched, score, audioStatus, readiness := matchVideoGenerationVariant(variant, videoVariantMatchRequest{
		TaskType: "video.image_to_video", ReferenceMode: "first_frame", AspectRatio: "16:9", Resolution: "720p",
		PromptLanguage: "zh-CN", DialogueLanguage: "zh-CN", HasDialogue: true, AudioStrategy: "native_av", AudioRequirement: "preferred",
	})
	if !matched || score != 2 || audioStatus != "audio_unverified" || readiness != "preview_only" {
		t.Fatalf("match = %v %d %s %s", matched, score, audioStatus, readiness)
	}
	matched, _, _, _ = matchVideoGenerationVariant(variant, videoVariantMatchRequest{AudioStrategy: "native_av", AudioRequirement: "required"})
	if matched {
		t.Fatal("unknown native audio must not satisfy required")
	}
}

func TestVideoGenerationVariantsRejectDuplicateKeys(t *testing.T) {
	capability := Capability{ProviderOptionsSchema: []byte(`{"xCapabilities":{"videoGenerationVariants":[{"variantKey":"same","duration":{"mode":"fixed","values":[5]},"nativeAudio":{"support":"false"}},{"variantKey":"same","duration":{"mode":"fixed","values":[8]},"nativeAudio":{"support":"false"}}]}}`)}
	_, err := videoGenerationVariants([]Capability{capability}, Model{ModelKey: "video-model"})
	if err == nil {
		t.Fatal("duplicate variant keys should fail")
	}
}
