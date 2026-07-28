package agent

import (
	"encoding/json"
	"testing"
)

func TestDefaultRegistryContainsCoreTools(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	tests := []struct {
		name             string
		risk             ToolRisk
		requiresApproval bool
	}{
		{"project.read_summary", ToolRiskRead, false},
		{"workflow.start", ToolRiskWorkflow, true},
		{"review.apply_fix", ToolRiskWrite, true},
		{"provider.update_account", ToolRiskAdmin, true},
	}
	for _, tt := range tests {
		tool, ok := registry.Get(tt.name)
		if !ok {
			t.Fatalf("expected tool %s", tt.name)
		}
		if tool.Risk != tt.risk {
			t.Fatalf("%s risk = %s, want %s", tt.name, tool.Risk, tt.risk)
		}
		if tool.RequiresApproval != tt.requiresApproval {
			t.Fatalf("%s requiresApproval = %v, want %v", tt.name, tool.RequiresApproval, tt.requiresApproval)
		}
		if len(tool.InputSchema) == 0 {
			t.Fatalf("%s input schema is empty", tt.name)
		}
	}
}

func TestDefaultRegistryDeclaresChildWorkflowTools(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	for _, name := range []string{
		"workflow.start",
		"script.generate_from_source",
		"asset.batch_generate_prompts",
		"asset.batch_generate_images",
		"shot.generate_image_prompts",
		"shot.generate_video_prompts",
		"shot.generate_missing_images",
		"shot.generate_missing_videos",
		"shot.cancel_running_videos",
		"timeline.compose",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected tool %s", name)
		}
		if !tool.EffectiveEffects().StartsWorkflow {
			t.Errorf("%s startsWorkflow effect = false, want true", name)
		}
	}
	for _, name := range []string{"shot.status", "workflow.cancel", "final_video.activate", "storyboard.reorder"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("expected tool %s", name)
		}
		if tool.EffectiveEffects().StartsWorkflow {
			t.Errorf("%s startsWorkflow effect = true, want false", name)
		}
	}
}

func TestRegistryRejectsDuplicateTools(t *testing.T) {
	_, err := NewRegistry(
		AgentTool{Name: "project.read_summary", Risk: ToolRiskRead},
		AgentTool{Name: "project.read_summary", Risk: ToolRiskRead},
	)
	if err == nil {
		t.Fatal("expected duplicate tool error")
	}
}

func TestRegistryDescriptorsAreSorted(t *testing.T) {
	registry, err := NewRegistry(
		AgentTool{Name: "workflow.start", Risk: ToolRiskWorkflow},
		AgentTool{Name: "asset.list", Risk: ToolRiskRead},
	)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	items := registry.Descriptors()
	if len(items) != 2 {
		t.Fatalf("descriptor count = %d, want 2", len(items))
	}
	if items[0].Name != "asset.list" || items[1].Name != "workflow.start" {
		t.Fatalf("descriptors not sorted: %#v", items)
	}
}

func TestAssetPromptToolsExposeStructuredSchemas(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	getTool, ok := registry.Get("asset.get")
	if !ok || getTool.Risk != ToolRiskRead || getTool.RequiresApproval {
		t.Fatalf("asset.get = %+v", getTool)
	}
	reviseTool, ok := registry.Get("asset.revise_prompt")
	if !ok || reviseTool.Risk != ToolRiskWrite || !reviseTool.RequiresApproval {
		t.Fatalf("asset.revise_prompt = %+v", reviseTool)
	}
	updateTool, ok := registry.Get("asset.update")
	if !ok {
		t.Fatal("asset.update is missing")
	}
	var schema map[string]any
	if err := json.Unmarshal(updateTool.InputSchema, &schema); err != nil {
		t.Fatalf("decode asset.update schema: %v", err)
	}
	properties := schema["properties"].(map[string]any)
	patch := properties["patch"].(map[string]any)
	patchProperties := patch["properties"].(map[string]any)
	for _, field := range []string{"basePrompt", "consistencyPrompt", "negativePrompt", "profile"} {
		if _, exists := patchProperties[field]; !exists {
			t.Fatalf("asset.update patch schema is missing %s", field)
		}
	}
}
