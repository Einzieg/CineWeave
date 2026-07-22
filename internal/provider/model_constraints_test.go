package provider

import (
	"encoding/json"
	"testing"
)

func TestModelPromptLengthConstraintUsesConfiguredUnit(t *testing.T) {
	constraint := ModelPromptLengthConstraint([]Capability{{
		InputLimits: json.RawMessage(`{"promptMaxLength":4096,"promptLengthUnit":"utf8_bytes"}`),
	}})
	if constraint.MaxLength != 4096 || constraint.Unit != PromptLengthUnitUTF8Bytes {
		t.Fatalf("constraint = %+v", constraint)
	}
	if got := MeasurePromptLength("蛊真人", constraint.Unit); got != 9 {
		t.Fatalf("UTF-8 byte length = %d, want 9", got)
	}
}

func TestModelPromptLengthConstraintDefaultsToCharacters(t *testing.T) {
	constraint := ModelPromptLengthConstraint([]Capability{{
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"promptMaxLength":4}}`),
	}})
	if constraint.MaxLength != 4 || constraint.Unit != PromptLengthUnitCharacters {
		t.Fatalf("constraint = %+v", constraint)
	}
	if !PromptWithinConstraint("蛊真人啊", constraint) {
		t.Fatal("four Chinese characters should fit a four-character limit")
	}
}

func TestModelRuntimeConstraintsUseStrictestConfiguredLimits(t *testing.T) {
	capabilities := []Capability{
		{InputLimits: json.RawMessage(`{"contextWindow":32768,"maxReferenceImages":4,"supportsReferenceImages":true}`)},
		{ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"maxInputTokens":16384,"maxReferenceImages":2,"supportsReferences":true}}`)},
	}
	if got := ModelContextWindow(capabilities); got != 16384 {
		t.Fatalf("context window = %d, want 16384", got)
	}
	references := ModelReferenceConstraint(capabilities)
	if !references.Supported || references.MaxReferences != 2 {
		t.Fatalf("reference constraint = %+v", references)
	}
}

func TestModelVideoRuntimeConstraintsAggregateFirstFrameAndNativeAudio(t *testing.T) {
	supportsDialogue := true
	supportsLipSync := true
	candidate := RoutingCandidate{
		ProviderModelID: "model-id",
		ModelKey:        "video-model",
		Modality:        "video",
		Capabilities: []Capability{{
			InputLimits: json.RawMessage(`{"maxReferenceImages":2,"supportsReferenceImages":true}`),
			ProviderOptionsSchema: mustJSONRaw(map[string]any{
				"xCapabilities": map[string]any{
					"videoGenerationVariants": []VideoGenerationVariant{{
						VariantKey: "image-to-video",
						When: VideoGenerationVariantWhen{
							ReferenceModes: []string{"first_frame"},
						},
						Duration:  VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{5}},
						FrameRate: VideoFrameRateCapability{Mode: "unknown"},
						NativeAudio: VideoNativeAudioCapability{
							Support:          VideoSupportTrue,
							SupportsDialogue: &supportsDialogue,
							SupportsLipSync:  &supportsLipSync,
						},
						Continuation: VideoContinuationCapability{SupportsFirstFrame: true},
					}},
				},
			}),
		}},
	}
	references, nativeAudio := ModelVideoRuntimeConstraints(candidate)
	if !references.Supported || !references.SupportsFirstFrame || references.SupportsLastFrame ||
		references.SupportsSemanticReferenceImages || references.MaxReferences != 1 || references.MaxImageReferences != 1 {
		t.Fatalf("reference constraint = %+v", references)
	}
	if nativeAudio.Support != VideoSupportTrue || !nativeAudio.SupportsDialogue || !nativeAudio.SupportsLipSync {
		t.Fatalf("native audio constraint = %+v", nativeAudio)
	}
}

func TestModelVideoRuntimeConstraintsDoNotPromoteGenericImageReferencesToSemanticSlots(t *testing.T) {
	candidate := RoutingCandidate{
		ProviderModelID: "model-id",
		ModelKey:        "first-frame-only",
		Modality:        "video",
		Capabilities: []Capability{{
			InputLimits: json.RawMessage(`{"maxReferenceImages":8,"supportsReferenceImages":true}`),
			ProviderOptionsSchema: mustJSONRaw(map[string]any{
				"xCapabilities": map[string]any{
					"videoGenerationVariants": []VideoGenerationVariant{{
						VariantKey: "image-to-video",
						When: VideoGenerationVariantWhen{
							ReferenceModes: []string{"first_frame"},
						},
						Duration:  VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{5}},
						FrameRate: VideoFrameRateCapability{Mode: "unknown"},
					}},
				},
			}),
		}},
	}
	references, _ := ModelVideoRuntimeConstraints(candidate)
	if references.SupportsSemanticReferenceImages || references.MaxReferences != 1 || references.MaxImageReferences != 1 {
		t.Fatalf("first-frame-only model was promoted to semantic references: %+v", references)
	}
}

func TestModelVideoRuntimeConstraintsKeepUnknownDistinctFromFalse(t *testing.T) {
	candidate := RoutingCandidate{
		ProviderModelID: "model-id",
		ModelKey:        "video-model",
		Modality:        "video",
		Capabilities: []Capability{{ProviderOptionsSchema: mustJSONRaw(map[string]any{
			"xCapabilities": map[string]any{"videoGenerationVariants": []VideoGenerationVariant{
				{VariantKey: "known-false", Duration: VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{5}}, FrameRate: VideoFrameRateCapability{Mode: "unknown"}, NativeAudio: VideoNativeAudioCapability{Support: VideoSupportFalse}},
				{VariantKey: "not-verified", Duration: VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{8}}, FrameRate: VideoFrameRateCapability{Mode: "unknown"}, NativeAudio: VideoNativeAudioCapability{Support: VideoSupportUnknown}},
			}},
		})}},
	}
	_, nativeAudio := ModelVideoRuntimeConstraints(candidate)
	if nativeAudio.Support != VideoSupportUnknown {
		t.Fatalf("native audio support = %q, want unknown", nativeAudio.Support)
	}
}

func TestModelVideoRuntimeConstraintsExposeTypedMultimodalSlots(t *testing.T) {
	candidate := RoutingCandidate{
		ProviderModelID: "model-id", ModelKey: "multimodal-video", Modality: "video",
		Capabilities: []Capability{{ProviderOptionsSchema: mustJSONRaw(map[string]any{
			"xCapabilities": map[string]any{"videoGenerationVariants": []VideoGenerationVariant{{
				VariantKey:  "typed-references",
				When:        VideoGenerationVariantWhen{TaskTypes: []string{"video.image_to_video"}, ReferenceModes: []string{"first_frame_plus_references"}},
				Duration:    VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{10}},
				FrameRate:   VideoFrameRateCapability{Mode: "fixed", Values: []float64{24}},
				NativeAudio: VideoNativeAudioCapability{Support: VideoSupportTrue},
				InputContract: VideoInputContract{
					ContractKey: VideoInputContractFirstFramePlusReferences, RequestMode: "async_create",
					Slots: []VideoInputSlot{
						{Role: "first_frame", MediaType: "image", Semantics: "output_start_frame", Min: 1, Max: 1},
						{Role: "semantic_reference", MediaType: "image", Semantics: "identity_scene_style_guidance", Min: 1, Max: 8},
						{Role: "video_reference", MediaType: "video", Semantics: "motion_guidance", Min: 0, Max: 2},
						{Role: "audio_reference", MediaType: "audio", Semantics: "audio_guidance", Min: 0, Max: 1},
					},
				},
				VerificationStatus: VideoCapabilityVerificationTested,
			}}},
		})}},
	}
	references, _ := ModelVideoRuntimeConstraints(candidate)
	if !references.Supported || !references.SupportsFirstFrame || !references.SupportsSemanticReferenceImages ||
		!references.SupportsVideoReference || !references.SupportsAudioReference ||
		references.MaxReferences != 12 || references.MaxImageReferences != 9 || references.MaxVideoReferences != 2 || references.MaxAudioReferences != 1 ||
		len(references.InputContracts) != 1 || references.InputContracts[0] != VideoInputContractFirstFramePlusReferences {
		t.Fatalf("typed reference constraint = %+v", references)
	}
}

func TestModelVideoRuntimeConstraintsExposeStoryboardSheetSlot(t *testing.T) {
	candidate := RoutingCandidate{
		ProviderModelID: "model-id", ModelKey: "storyboard-sheet-video", Modality: "video",
		Capabilities: []Capability{{ProviderOptionsSchema: mustJSONRaw(map[string]any{
			"xCapabilities": map[string]any{"videoGenerationVariants": []VideoGenerationVariant{{
				VariantKey: "storyboard-sheet",
				Duration:   VideoDurationCapability{Mode: VideoDurationFixed, Values: []float64{10}},
				FrameRate:  VideoFrameRateCapability{Mode: "fixed", Values: []float64{24}},
				InputContract: VideoInputContract{
					ContractKey: VideoInputContractStoryboardSheetReference,
					RequestMode: "async_create",
					Slots: []VideoInputSlot{{
						Role: "storyboard_sheet", MediaType: "image", Semantics: "ordered_keyframe_sheet", Min: 1, Max: 1,
					}},
				},
				VerificationStatus: VideoCapabilityVerificationTested,
			}}},
		})}},
	}
	references, _ := ModelVideoRuntimeConstraints(candidate)
	if !references.Supported || !references.SupportsStoryboardSheetReference ||
		references.SupportsFirstFrame || references.SupportsSemanticReferenceImages ||
		references.MaxReferences != 1 || references.MaxImageReferences != 1 ||
		len(references.InputContracts) != 1 || references.InputContracts[0] != VideoInputContractStoryboardSheetReference {
		t.Fatalf("storyboard sheet reference constraint = %+v", references)
	}
}

func mustJSONRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
