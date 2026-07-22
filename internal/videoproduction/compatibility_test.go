package videoproduction

import (
	"encoding/json"
	"testing"
)

func TestEvaluateModelCompatibilitySingleFrameWithNativeAudio(t *testing.T) {
	profile := ProfileVersion{CapabilityRequirements: json.RawMessage(`{
		"taskType":"video.image_to_video",
		"inputContract":"first_frame",
		"maxFirstFrames":{"minimum":1}
	}`)}
	capability := ModelCapability{
		TaskTypes: json.RawMessage(`["video.image_to_video"]`),
		ProviderOptionsSchema: json.RawMessage(`{
			"xCapabilities": {
				"supportsFirstFrame": true,
				"maxReferenceImages": 1,
				"videoGenerationVariants": [{
					"when": {"taskTypes":["video.image_to_video"],"referenceModes":["first_frame"],"nativeAudioRequested":true},
					"nativeAudio": {"support":"true","supportedDialogueLanguages":["zh-CN"]},
					"continuation": {"supportsFirstFrame":true}
				}]
			}
		}`),
	}
	result := EvaluateModelCompatibility(profile, capability, true)
	if !result.Compatible {
		t.Fatalf("expected compatible model, got issues %#v", result.Issues)
	}
}

func TestEvaluateModelCompatibilityReportsIndependentContractFailures(t *testing.T) {
	profile := ProfileVersion{CapabilityRequirements: json.RawMessage(`{
		"taskType":"video.image_to_video",
		"inputContract":"first_last_frames",
		"supportsFirstFrame":true,
		"supportsLastFrame":true
	}`)}
	capability := ModelCapability{
		TaskTypes:             json.RawMessage(`["video.text_to_video"]`),
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{}}`),
	}
	result := EvaluateModelCompatibility(profile, capability, true)
	if result.Compatible {
		t.Fatal("expected incompatible model")
	}
	want := map[string]bool{
		"TASK_TYPE_UNSUPPORTED":    false,
		"FIRST_FRAME_UNSUPPORTED":  false,
		"LAST_FRAME_UNSUPPORTED":   false,
		"NATIVE_AUDIO_UNSUPPORTED": false,
	}
	for _, issue := range result.Issues {
		if _, ok := want[issue.Code]; ok {
			want[issue.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Fatalf("expected issue %s, got %#v", code, result.Issues)
		}
	}
}
