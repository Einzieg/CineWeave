package projectcontrol

import (
	"encoding/json"
	"testing"
)

func TestRegistryReturnsSortedClonedDescriptors(t *testing.T) {
	registry, err := NewRegistry(
		testDescriptor("workflow.start", Effects{StartsWorkflow: true, WritesProject: true}),
		testDescriptor("asset.list", Effects{}),
	)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	items := registry.List()
	if len(items) != 2 || items[0].Name != "asset.list" || items[1].Name != "workflow.start" {
		t.Fatalf("unexpected descriptor order: %#v", items)
	}
	items[0].Permissions[0] = "changed"
	actual, ok := registry.Get("asset.list")
	if !ok || actual.Permissions[0] != "project.read" {
		t.Fatalf("registry descriptor was mutated through a caller: %#v", actual)
	}
}

func TestRegistryRejectsDuplicateAndInconsistentDescriptors(t *testing.T) {
	descriptor := testDescriptor("asset.list", Effects{})
	if _, err := NewRegistry(descriptor, descriptor); err == nil {
		t.Fatal("expected duplicate descriptor error")
	}

	descriptor.ReadOnly = false
	if _, err := NewRegistry(descriptor); err == nil {
		t.Fatal("expected read-only/effects mismatch error")
	}
}

func testDescriptor(name string, effects Effects) Descriptor {
	mode := ExecutionModeSync
	if effects.StartsWorkflow {
		mode = ExecutionModeWorkflow
	}
	return Descriptor{
		Name:               name,
		Version:            1,
		Label:              name,
		Summary:            name,
		Description:        name,
		Risk:               RiskRead,
		Scope:              ScopeProject,
		Permissions:        []string{"project.read"},
		ProjectKinds:       []string{"narrative"},
		InputSchema:        json.RawMessage(`{"type":"object","additionalProperties":false}`),
		OutputSchema:       json.RawMessage(`{"type":"object","additionalProperties":true}`),
		Effects:            effects,
		ReadOnly:           effects.ReadOnly(),
		Destructive:        effects.Destructive,
		Idempotent:         true,
		Costed:             effects.MaySpendProvider,
		StartsWorkflow:     effects.StartsWorkflow,
		ExecutionMode:      mode,
		ActivityVisibility: ActivityVisibilityAuditOnly,
		ExportToMCP:        true,
	}
}
