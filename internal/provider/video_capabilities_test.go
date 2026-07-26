package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanVideoSegmentsContinuousRangePreservesExactTarget(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey:   "continuous",
		Duration:     VideoDurationCapability{Mode: VideoDurationContinuousRange, MinSeconds: 1, MaxSeconds: 8},
		NativeAudio:  VideoNativeAudioCapability{Support: VideoSupportUnknown},
		Continuation: VideoContinuationCapability{SupportsExtension: true},
	}
	continuation := &VideoInputContract{ContractKey: VideoInputContractVideoExtension}
	segments, err := planVideoSegments(12*90000, 90000, variant, "first_frame", continuation)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].RequestedDurationSeconds != 8 || segments[1].RequestedDurationSeconds != 4 {
		t.Fatalf("segments = %+v", segments)
	}
	if segments[0].ContinuityMode != "first_frame" || segments[1].ContinuityMode != "video_extension" || segments[1].PlannedEndTick != 12*90000 {
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
	segments, err := planVideoSegments(382500, 90000, variant, "first_frame", nil)
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
	segments, err := planVideoSegments(382500, 90000, variant, "first_frame", nil)
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
	continuation := &VideoInputContract{ContractKey: VideoInputContractFirstFrame}
	segments, err := planVideoSegments(10*90000, 90000, variant, "first_frame", continuation)
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
	_, err := planVideoSegments(9*90000, 90000, variant, "first_frame", nil)
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
	continuation := &VideoInputContract{ContractKey: VideoInputContractFirstFrame}
	segments, err := planVideoSegmentsWithDialogue(10*90000, 90000, 3750, variant, "first_frame", []GatewayVideoDialogueSpan{
		{TimingUnitID: "dialogue-1", Speaker: "方源", Text: "这一句必须完整保留。", StartTick: 6 * 90000, EndTick: 9 * 90000},
	}, continuation)
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

func TestPlanVideoSegmentsUsesNextDiscreteTierForSingleTakeDialogue(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey: "single-take",
		Duration: VideoDurationCapability{
			Mode:   VideoDurationDiscrete,
			Values: []float64{6, 10, 12, 16},
		},
	}
	segments, err := planVideoSegmentsWithDialogue(15*90000, 90000, 3750, variant, "first_frame", []GatewayVideoDialogueSpan{{
		TimingUnitID: "voiceover-1",
		Speaker:      "旁白",
		Text:         "冻结脚本旁白",
		StartTick:    0,
		EndTick:      15 * 90000,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 ||
		segments[0].PlannedDurationTicks != 15*90000 ||
		segments[0].RequestedDurationSeconds != 16 ||
		segments[0].TrimEndTick != 15*90000 {
		t.Fatalf("single-take segment = %+v", segments)
	}
}

func TestPlanVideoSegmentsRejectsDialogueLongerThanProviderCapacity(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey: "discrete", Duration: VideoDurationCapability{Mode: VideoDurationDiscrete, Values: []float64{4, 8}},
		Continuation: VideoContinuationCapability{SupportsFirstFrame: true, SupportsLastFrame: true},
	}
	continuation := &VideoInputContract{ContractKey: VideoInputContractFirstFrame}
	_, err := planVideoSegmentsWithDialogue(10*90000, 90000, 3750, variant, "first_frame", []GatewayVideoDialogueSpan{
		{TimingUnitID: "dialogue-1", Speaker: "方源", Text: "无法安全切开的长台词", StartTick: 0, EndTick: 9 * 90000},
	}, continuation)
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, CodeStoryboardReplanRequired)
	}
}

func TestPlanVideoSegmentsSplitsLongDialogueAtChinesePunctuation(t *testing.T) {
	variant := VideoGenerationVariant{
		VariantKey: "discrete", Duration: VideoDurationCapability{Mode: VideoDurationDiscrete, Values: []float64{4, 8}},
		Continuation: VideoContinuationCapability{SupportsFirstFrame: true, SupportsLastFrame: true},
	}
	continuation := &VideoInputContract{ContractKey: VideoInputContractFirstFrame}
	original := "方老魔，你不要妄图反抗了，今日我们正道各大派联合起来。"
	segments, err := planVideoSegmentsWithDialogue(10*90000, 90000, 3750, variant, "first_frame", []GatewayVideoDialogueSpan{
		{TimingUnitID: "dialogue-1", Speaker: "正道群雄", Text: original, StartTick: 0, EndTick: 9 * 90000},
	}, continuation)
	if err != nil {
		t.Fatalf("planVideoSegmentsWithDialogue: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments = %+v, want two dialogue-safe segments", segments)
	}
	var combined strings.Builder
	var spans []GatewayVideoDialogueSpan
	for _, segment := range segments {
		if segment.RequestedDurationSeconds != 8 {
			t.Fatalf("requested duration = %v, want 8", segment.RequestedDurationSeconds)
		}
		for _, line := range segment.DialogueSpans {
			combined.WriteString(line.Text)
			spans = append(spans, line)
		}
	}
	if combined.String() != original || len(spans) < 2 {
		t.Fatalf("split dialogue = %q / %+v", combined.String(), spans)
	}
	if !spans[0].ContinuesToNext || !spans[len(spans)-1].ContinuesFromPrevious {
		t.Fatalf("continuation flags = %+v", spans)
	}
}

func TestFirstLastFrameProfileRejectsMultiSegmentRenderPlan(t *testing.T) {
	err := validateProfileVideoSegments(VideoInputContractFirstLastFrames, []GatewayVideoPlanSegment{
		{SegmentIndex: 0},
		{SegmentIndex: 1},
	})
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeStoryboardReplanRequired || standard.Standard.Retryable {
		t.Fatalf("error = %v, want non-retryable %s", err, CodeStoryboardReplanRequired)
	}
	if err := validateProfileVideoSegments(VideoInputContractFirstLastFrames, []GatewayVideoPlanSegment{{SegmentIndex: 0}}); err != nil {
		t.Fatalf("single segment should be valid: %v", err)
	}
}

func TestFirstLastFrameInputContractRequiresBothRoles(t *testing.T) {
	contract := VideoInputContract{
		ContractKey: VideoInputContractFirstLastFrames,
		Slots: []VideoInputSlot{
			{Role: "first_frame", MediaType: "image", Min: 1, Max: 1},
			{Role: "last_frame", MediaType: "image", Min: 1, Max: 1},
		},
	}
	valid := []GatewayVideoReference{
		{Role: "first_frame", Type: "image_reference", StorageKey: "first.png"},
		{Role: "last_frame", Type: "image_reference", StorageKey: "last.png"},
	}
	if err := validateGatewayVideoReferencesForContract(valid, contract); err != nil {
		t.Fatalf("valid references rejected: %v", err)
	}
	if err := validateGatewayVideoReferencesForContract(valid[:1], contract); err == nil {
		t.Fatal("missing last frame should be rejected before the upstream call")
	}
}

func TestMultimodalInputContractMapsTypedSemanticRolesWithoutMixingMedia(t *testing.T) {
	contract := VideoInputContract{
		ContractKey: VideoInputContractFirstFramePlusReferences,
		Slots: []VideoInputSlot{
			{Role: "first_frame", MediaType: "image", Min: 1, Max: 1},
			{Role: "semantic_reference", MediaType: "image", Min: 1, Max: 4},
			{Role: "video_reference", MediaType: "video", Min: 0, Max: 1},
			{Role: "audio_reference", MediaType: "audio", Min: 0, Max: 1},
		},
	}
	valid := []GatewayVideoReference{
		{Role: "first_frame", Type: "image", StorageKey: "first.png"},
		{Role: "character_identity", Type: "image", StorageKey: "character.png"},
		{Role: "scene_identity", Type: "image", StorageKey: "scene.png"},
		{Role: "video_reference", Type: "video", StorageKey: "motion.mp4"},
		{Role: "audio_reference", Type: "audio", StorageKey: "voice.wav"},
	}
	if err := validateGatewayVideoReferencesForContract(valid, contract); err != nil {
		t.Fatalf("valid multimodal references rejected: %v", err)
	}
	wrongMedia := append([]GatewayVideoReference(nil), valid...)
	wrongMedia[1].Type = "video"
	if err := validateGatewayVideoReferencesForContract(wrongMedia, contract); err == nil {
		t.Fatal("character identity video was accepted as a semantic reference image")
	}
	missingSemantic := []GatewayVideoReference{valid[0]}
	if err := validateGatewayVideoReferencesForContract(missingSemantic, contract); err == nil {
		t.Fatal("missing semantic references were accepted")
	}
}

func TestStoryboardSheetInputContractRequiresOnlyOrderedSheetRole(t *testing.T) {
	contract := VideoInputContract{
		ContractKey: VideoInputContractStoryboardSheetReference,
		Slots: []VideoInputSlot{{
			Role: "storyboard_sheet", MediaType: "image", Semantics: "ordered_keyframe_sheet", Min: 1, Max: 1, Ordered: true,
		}},
	}
	valid := []GatewayVideoReference{{
		Role: "storyboard_sheet", Type: "image", StorageKey: "storyboard-sheet.png",
	}}
	if err := validateGatewayVideoReferencesForContract(valid, contract); err != nil {
		t.Fatalf("valid storyboard sheet rejected: %v", err)
	}
	if err := validateGatewayVideoReferencesForContract([]GatewayVideoReference{{
		Role: "first_frame", Type: "image", StorageKey: "first-frame.png",
	}}, contract); err == nil {
		t.Fatal("first frame was accepted as a storyboard sheet")
	}
	if err := validateGatewayVideoReferencesForContract(append(valid, valid[0]), contract); err == nil {
		t.Fatal("multiple storyboard sheets were accepted")
	}
}

func TestValidateGatewayVideoDialogueSpansRejectsSystemAudio(t *testing.T) {
	targetTicks := int64(5 * 90000)
	_, err := validateGatewayVideoDialogueSpans([]GatewayVideoDialogueSpan{
		{TimingUnitID: "system-1", Text: "一声清越蝉鸣，骤然响彻天地。", Kind: "system", StartTick: 2 * 90000, EndTick: targetTicks, ContinuesToNext: true},
	}, targetTicks, 3750)
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, CodeStoryboardReplanRequired)
	}
}

func TestValidateGatewayVideoDialogueSpansRejectsSpeakerlessDialogue(t *testing.T) {
	_, err := validateGatewayVideoDialogueSpans([]GatewayVideoDialogueSpan{
		{Text: "缺少说话人", Kind: "dialogue", StartTick: 0, EndTick: 90000},
	}, 5*90000, 3750)
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeStoryboardReplanRequired {
		t.Fatalf("error = %v, want %s", err, CodeStoryboardReplanRequired)
	}
}

func TestSameGatewayVideoDialogueCuesComparesCompleteAudioContract(t *testing.T) {
	base := GatewayVideoDialogueSpan{
		TimingUnitID: "system-1", Speaker: "系统音频", Text: "山风穿过崖壁", Kind: "system",
		StartTick: 0, EndTick: 90000, ContinuesToNext: true,
	}
	if !sameGatewayVideoDialogueCues([]GatewayVideoDialogueSpan{base}, []GatewayVideoDialogueSpan{base}) {
		t.Fatal("identical system audio cues must match")
	}
	changedKind := base
	changedKind.Kind = "dialogue"
	if sameGatewayVideoDialogueCues([]GatewayVideoDialogueSpan{base}, []GatewayVideoDialogueSpan{changedKind}) {
		t.Fatal("system audio must not compare equal to character dialogue")
	}
	changedContinuation := base
	changedContinuation.ContinuesToNext = false
	if sameGatewayVideoDialogueCues([]GatewayVideoDialogueSpan{base}, []GatewayVideoDialogueSpan{changedContinuation}) {
		t.Fatal("continuation metadata is part of the audio cue contract")
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
	if !matched || score < 2 || audioStatus != "audio_unverified" || readiness != "preview_only" {
		t.Fatalf("match = %v %d %s %s", matched, score, audioStatus, readiness)
	}
	matched, _, _, _ = matchVideoGenerationVariant(variant, videoVariantMatchRequest{AudioStrategy: "native_av", AudioRequirement: "required"})
	if !matched {
		t.Fatal("native audio support is advisory and must not reject an otherwise executable variant")
	}
}

func TestMatchVideoGenerationVariantHardFiltersOnlyResolution(t *testing.T) {
	supportsDialogue := false
	variant := VideoGenerationVariant{
		VariantKey: "soft-capability-mismatch",
		When: VideoGenerationVariantWhen{
			TaskTypes:      []string{"video.text_to_video"},
			ReferenceModes: []string{"none"},
		},
		InputContract:            VideoInputContract{ContractKey: VideoInputContractTextOnly},
		Duration:                 VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{5}},
		AspectRatios:             []string{"9:16"},
		Resolutions:              []string{"720p"},
		SupportedPromptLanguages: []string{"en-US"},
		NativeAudio: VideoNativeAudioCapability{
			Support: VideoSupportFalse, SupportsDialogue: &supportsDialogue,
		},
	}
	request := videoVariantMatchRequest{
		TaskType: "video.image_to_video", ReferenceMode: "first_frame",
		RequiredInitialInputContract: VideoInputContractFirstFrame,
		AspectRatio:                  "16:9", Resolution: "720p",
		PromptLanguage: "zh-CN", DialogueLanguage: "zh-CN",
		HasDialogue: true, AudioStrategy: "native_av", AudioRequirement: "required",
	}

	matched, _, audioStatus, _ := matchVideoGenerationVariant(variant, request)
	if !matched || audioStatus != "native_audio_unavailable" {
		t.Fatalf("soft capability mismatch = %v / %s", matched, audioStatus)
	}
	request.Resolution = "1080p"
	matched, _, _, _ = matchVideoGenerationVariant(variant, request)
	if matched {
		t.Fatal("unsupported resolution must remain a hard rejection")
	}
}

func TestVideoGenerationVariantsRejectDuplicateKeys(t *testing.T) {
	capability := Capability{ProviderOptionsSchema: []byte(`{"xCapabilities":{"videoGenerationVariants":[{"variantKey":"same","duration":{"mode":"fixed","values":[5]},"nativeAudio":{"support":"false"}},{"variantKey":"same","duration":{"mode":"fixed","values":[8]},"nativeAudio":{"support":"false"}}]}}`)}
	_, err := videoGenerationVariants([]Capability{capability}, Model{ModelKey: "video-model"})
	if err == nil {
		t.Fatal("duplicate variant keys should fail")
	}
}

func TestVideoGenerationVariantsNormalizeFlatFirstFrameCapability(t *testing.T) {
	capability := Capability{
		ID:                    "capability-1",
		TaskTypes:             []byte(`["video.image_to_video"]`),
		ProviderOptionsSchema: []byte(`{"xCapabilities":{"durations":[5,10],"resolutions":["720p"],"supportsFirstFrame":true,"requestModes":["async_create","poll"]}}`),
	}
	variants, err := videoGenerationVariants([]Capability{capability}, Model{ModelKey: "video-model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 1 {
		t.Fatalf("variants = %+v, want one normalized variant", variants)
	}
	variant := variants[0]
	if variant.InputContract.ContractKey != VideoInputContractFirstFrame ||
		variant.InputContract.RequestMode != "async_create" ||
		len(variant.InputContract.Slots) != 1 ||
		variant.InputContract.Slots[0].Role != "first_frame" {
		t.Fatalf("input contract = %+v, want canonical first-frame contract", variant.InputContract)
	}
	if variant.Duration.Mode != VideoDurationDiscrete ||
		len(variant.Duration.Values) != 2 ||
		variant.Duration.Values[0] != 5 ||
		variant.Duration.Values[1] != 10 {
		t.Fatalf("duration = %+v, want discrete 5/10 seconds", variant.Duration)
	}
	if len(variant.Resolutions) != 1 || variant.Resolutions[0] != "720p" {
		t.Fatalf("resolutions = %+v, want 720p", variant.Resolutions)
	}
}

func TestExecutableWholeSecondVideoDurationsNormalizesFlatAndStructuredCapabilities(t *testing.T) {
	capabilities := []Capability{
		{
			ID:                    "flat",
			TaskTypes:             []byte(`["video.image_to_video"]`),
			ProviderOptionsSchema: []byte(`{"xCapabilities":{"durations":[5,10,10.5],"supportsFirstFrame":true,"requestModes":["async_create"]}}`),
		},
		{
			ID: "structured",
			ProviderOptionsSchema: []byte(`{"xCapabilities":{"videoGenerationVariants":[{
				"variantKey":"continuous",
				"duration":{"mode":"continuous_range","minSeconds":1.5,"maxSeconds":4.5,"stepSeconds":0.5},
				"frameRate":{"mode":"unknown"},
				"nativeAudio":{"support":"unknown"},
				"requestModes":["async_create"]
			}]}}`),
		},
	}

	values, err := ExecutableWholeSecondVideoDurations(capabilities, Model{ModelKey: "video-model"})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{2, 3, 4, 5, 10}
	if len(values) != len(want) {
		t.Fatalf("values = %+v, want %+v", values, want)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("values = %+v, want %+v", values, want)
		}
	}
}

func TestVideoGenerationVariantsInferCanonicalFirstFrameInputContract(t *testing.T) {
	capability := Capability{ProviderOptionsSchema: []byte(`{"xCapabilities":{"videoGenerationVariants":[{"variantKey":"inferred-first-frame","when":{"taskTypes":["video.image_to_video"],"referenceModes":["first_frame"]},"duration":{"mode":"fixed","values":[5]},"frameRate":{"mode":"unknown"},"nativeAudio":{"support":"false"},"continuation":{"supportsFirstFrame":true},"requestModes":["async_create"],"source":"derived"}]}}`)}
	variants, err := videoGenerationVariants([]Capability{capability}, Model{ModelKey: "video-model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 1 {
		t.Fatalf("variants = %d, want 1", len(variants))
	}
	contract := variants[0].InputContract
	if contract.ContractKey != VideoInputContractFirstFrame || contract.RequestMode != "async_create" {
		t.Fatalf("contract = %+v", contract)
	}
	if len(contract.Slots) != 1 || contract.Slots[0].Role != "first_frame" || contract.Slots[0].MediaType != "image" || contract.Slots[0].Min != 1 || contract.Slots[0].Max != 1 {
		t.Fatalf("slots = %+v", contract.Slots)
	}
	if variants[0].VerificationStatus != VideoCapabilityVerificationInferred {
		t.Fatalf("verification = %s, want inferred", variants[0].VerificationStatus)
	}
}

func TestVideoGenerationVariantsRejectMalformedExplicitInputContract(t *testing.T) {
	capability := Capability{ProviderOptionsSchema: []byte(`{"xCapabilities":{"videoGenerationVariants":[{"variantKey":"bad-contract","duration":{"mode":"fixed","values":[5]},"frameRate":{"mode":"unknown"},"nativeAudio":{"support":"false"},"inputContract":{"contractKey":"first_frame","requestMode":"async_create","slots":[{"role":"first_frame","mediaType":"image","min":2,"max":1}]}}]}}`)}
	if _, err := videoGenerationVariants([]Capability{capability}, Model{ModelKey: "video-model"}); err == nil {
		t.Fatal("malformed explicit input contract should fail")
	}
}

func TestVideoInputContractSatisfiesCompatibleSuperset(t *testing.T) {
	if !videoInputContractSatisfies(VideoInputContract{ContractKey: VideoInputContractFirstFramePlusReferences}, VideoInputContractFirstFrame) {
		t.Fatal("first_frame_plus_references should satisfy first_frame")
	}
	if videoInputContractSatisfies(VideoInputContract{ContractKey: VideoInputContractFirstFrame}, VideoInputContractFirstFramePlusReferences) {
		t.Fatal("first_frame must not satisfy first_frame_plus_references")
	}
}
