package agent

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Einzieg/cineweave/internal/authz"
)

func TestCommerceVideoToolsExposeScriptDerivationLifecycle(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	expected := map[string]struct {
		maySpendProvider bool
		startsWorkflow   bool
	}{
		"commerce.script.derive.preview":      {maySpendProvider: true},
		"commerce.script.derive.batch":        {maySpendProvider: true, startsWorkflow: true},
		"commerce.script.derivation.get":      {},
		"commerce.script.derive.retry_failed": {maySpendProvider: true, startsWorkflow: true},
		"commerce.script.derive.cancel":       {},
		"commerce.attachment.assign":          {},
	}
	for name, want := range expected {
		tool, ok := registry.Get(name)
		if !ok {
			t.Errorf("missing derivation tool %s", name)
			continue
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("%s input schema is empty", name)
		}
		if tool.Effects.MaySpendProvider != want.maySpendProvider {
			t.Errorf("%s MaySpendProvider = %v, want %v", name, tool.Effects.MaySpendProvider, want.maySpendProvider)
		}
		if tool.StartsWorkflow != want.startsWorkflow {
			t.Errorf("%s StartsWorkflow = %v, want %v", name, tool.StartsWorkflow, want.startsWorkflow)
		}
	}
}

func TestCommerceVideoToolsExposeCompletePermissionContracts(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	expected := map[string][]string{
		"commerce.script.derive.batch":        {authz.PermissionScriptWrite, authz.PermissionWorkflowRun},
		"commerce.script.derive.retry_failed": {authz.PermissionScriptWrite, authz.PermissionWorkflowRun},
		"commerce.script.derive.cancel":       {authz.PermissionWorkflowCancel},
		"commerce.video.cancel":               {authz.PermissionWorkflowCancel},
	}
	for name, permissions := range expected {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("missing commerce tool %s", name)
		}
		if got := tool.RequiredPermissions(); !reflect.DeepEqual(got, permissions) {
			t.Errorf("%s permissions = %v, want %v", name, got, permissions)
		}
		if got := tool.Descriptor().Permissions; !reflect.DeepEqual(got, permissions) {
			t.Errorf("%s descriptor permissions = %v, want %v", name, got, permissions)
		}
	}
}

func TestCommerceAttachmentAssignmentIsWriteOnlyAndRequiresApproval(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	tool, ok := registry.Get("commerce.attachment.assign")
	if !ok {
		t.Fatal("commerce.attachment.assign missing")
	}
	effects := tool.EffectiveEffects()
	if !effects.WritesProject || effects.MaySpendProvider || effects.StartsWorkflow || effects.Destructive {
		t.Fatalf("attachment assignment effects = %+v", effects)
	}
	if !tool.RequiresApproval {
		t.Fatal("attachment assignment must require approval")
	}
}

func TestCommerceVideoToolsDoNotExposeLegacyStoryboardProduction(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	for _, name := range []string{
		"commerce.script_unit.storyboard.generate",
		"commerce.script_unit.reference_images.generate",
		"commerce.script_unit.video_prompts.generate",
		"commerce.script_unit.shot_videos.generate",
		"commerce.script_unit.final.compose",
	} {
		if _, ok := registry.Get(name); ok {
			t.Errorf("legacy tool %s must not be active for commerce video projects", name)
		}
	}
}

func TestCommerceScriptToolsAcceptStableOrdinalWithoutCopiedUUID(t *testing.T) {
	registry, err := NewRegistry(CommerceVideoTools()...)
	if err != nil {
		t.Fatalf("commerce video registry: %v", err)
	}
	tests := []PlanStep{
		{
			Tool: "commerce.script.get",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7}`),
		},
		{
			Tool: "commerce.script.derive.preview",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7,"count":5,"dimension":"scene","instruction":"替换场景"}`),
		},
		{
			Tool: "commerce.video.generate",
			Args: json.RawMessage(`{"stableOrdinal":2,"expectedScriptUnitsRevision":7}`),
		},
	}
	for _, step := range tests {
		if _, err := ValidatePlan(Plan{Steps: []PlanStep{step}}, registry, 1); err != nil {
			t.Errorf("%s stable ordinal args rejected: %v", step.Tool, err)
		}
	}
}
