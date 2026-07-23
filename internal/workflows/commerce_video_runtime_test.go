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
