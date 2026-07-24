package workflows

import (
	"encoding/json"
	"testing"
)

func TestFrozenCommercePromptAgentLimitsUseFrozenCapabilities(t *testing.T) {
	t.Parallel()

	snapshot := json.RawMessage(`{
		"videoPromptAgent": {
			"providerModelId": "model-1",
			"capabilities": [{
				"inputLimits": {"contextWindow": 64000, "maxPromptLength": 12000},
				"providerOptionsSchema": {}
			}]
		}
	}`)
	contextLimit, promptLimit, err := frozenCommercePromptAgentLimits(snapshot, "videoPromptAgent", "model-1")
	if err != nil {
		t.Fatalf("frozenCommercePromptAgentLimits() error = %v", err)
	}
	if contextLimit != 64000 || promptLimit != 12000 {
		t.Fatalf("limits = (%d, %d), want (64000, 12000)", contextLimit, promptLimit)
	}
}

func TestFrozenCommercePromptAgentLimitsUseConservativeDefaults(t *testing.T) {
	t.Parallel()

	snapshot := json.RawMessage(`{
		"videoPromptAgent": {
			"providerModelId": "model-1",
			"capabilities": [{"inputLimits": {"inputTypes": ["text"]}}]
		}
	}`)
	contextLimit, promptLimit, err := frozenCommercePromptAgentLimits(snapshot, "videoPromptAgent", "model-1")
	if err != nil {
		t.Fatalf("frozenCommercePromptAgentLimits() error = %v", err)
	}
	if contextLimit != 32768 || promptLimit != 16000 {
		t.Fatalf("limits = (%d, %d), want (32768, 16000)", contextLimit, promptLimit)
	}
}

func TestFrozenCommercePromptAgentLimitsRejectModelMismatch(t *testing.T) {
	t.Parallel()

	snapshot := json.RawMessage(`{
		"videoPromptAgent": {
			"providerModelId": "model-1",
			"capabilities": []
		}
	}`)
	if _, _, err := frozenCommercePromptAgentLimits(snapshot, "videoPromptAgent", "model-2"); err == nil {
		t.Fatal("frozenCommercePromptAgentLimits() error = nil, want generation mismatch")
	}
}

func TestCommerceAllowedVideoDurationsNormalizesFrozenFlatCapability(t *testing.T) {
	t.Parallel()

	snapshot := json.RawMessage(`{
		"videoGenerator": {
			"providerModelId": "model-1",
			"capabilities": [{
				"id": "capability-1",
				"providerModelId": "model-1",
				"taskTypes": [
					"video.text_to_video",
					"video.image_to_video",
					"video.create_task",
					"video.poll_task"
				],
				"qualityTiers": ["480p", "720p"],
				"providerOptionsSchema": {
					"xCapabilities": {
						"durations": [1, 2, 3, 4, 5],
						"resolutions": ["480p", "720p"],
						"supportsFirstFrame": true,
						"requestModes": ["async_create", "poll"]
					}
				},
				"source": "preset"
			}]
		}
	}`)

	values, err := commerceAllowedVideoDurations(snapshot)
	if err != nil {
		t.Fatalf("commerceAllowedVideoDurations() error = %v", err)
	}
	want := []int{1, 2, 3, 4, 5}
	if len(values) != len(want) {
		t.Fatalf("values = %+v, want %+v", values, want)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("values = %+v, want %+v", values, want)
		}
	}
}

func TestCommerceAllowedVideoDurationsRejectsCapabilityWithoutDuration(t *testing.T) {
	t.Parallel()

	snapshot := json.RawMessage(`{
		"videoGenerator": {
			"providerModelId": "model-1",
			"capabilities": [{
				"id": "capability-1",
				"providerModelId": "model-1",
				"taskTypes": ["video.image_to_video"],
				"providerOptionsSchema": {
					"xCapabilities": {
						"supportsFirstFrame": true,
						"requestModes": ["async_create"]
					}
				}
			}]
		}
	}`)

	if _, err := commerceAllowedVideoDurations(snapshot); err == nil {
		t.Fatal("commerceAllowedVideoDurations() error = nil, want generation mismatch")
	}
}

func TestCommerceAgentIdentityAcceptsEveryGenerationPhase(t *testing.T) {
	t.Parallel()

	identity := testCommerceReferenceImageIdentity()
	phases := []CommerceWorkflowPhase{
		CommercePhaseScriptOrganization,
		CommercePhaseStoryboard,
		CommercePhaseImagePrompt,
		CommercePhaseImageFidelity,
		CommercePhaseVideoPrompt,
	}
	for _, phase := range phases {
		phase := phase
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()
			execution, subjectID, _, err := commerceAgentIdentity(CommerceAgentCallInput{
				GenerationIdentity: &identity,
				Phase:              phase,
			})
			if err != nil {
				t.Fatalf("commerceAgentIdentity() error = %v", err)
			}
			if execution != identity.ExecutionIdentity {
				t.Fatalf("execution identity = %+v, want %+v", execution, identity.ExecutionIdentity)
			}
			if subjectID != identity.UnitGenerationID {
				t.Fatalf("subject id = %q, want %q", subjectID, identity.UnitGenerationID)
			}
		})
	}
}

func TestCommerceAgentIdentityRejectsNonAgentGenerationPhases(t *testing.T) {
	t.Parallel()

	identity := testCommerceReferenceImageIdentity()
	for _, phase := range []CommerceWorkflowPhase{CommercePhaseVideoRender, CommercePhaseFinalCompose} {
		if _, _, _, err := commerceAgentIdentity(CommerceAgentCallInput{
			GenerationIdentity: &identity,
			Phase:              phase,
		}); err == nil {
			t.Fatalf("commerceAgentIdentity(%q) error = nil, want invalid phase", phase)
		}
	}
}
