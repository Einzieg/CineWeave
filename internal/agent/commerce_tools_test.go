package agent

import (
	"encoding/json"
	"testing"
)

func TestCommerceDefaultToolsCoverDocumentedSurface(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	expected := []string{
		"commerce.product.get", "commerce.product.version.list", "commerce.product.version.create", "commerce.product.rebuild_impact", "commerce.product.rebuild",
		"commerce.product.reference.list", "commerce.product.reference.add", "commerce.product.reference.archive", "commerce.product.reference.set_primary",
		"commerce.script_unit.list", "commerce.script_unit.get", "commerce.script_unit.create", "commerce.script_unit.duplicate", "commerce.script_unit.create_language_variant", "commerce.script_unit.reorder", "commerce.script_unit.archive",
		"commerce.script_unit.version.list", "commerce.script_unit.version.create", "commerce.script_unit.version.activate",
		"commerce.script_unit.language.get", "commerce.script_unit.language.set", "commerce.script_unit.language.confirm",
		"commerce.script_unit.localization.list", "commerce.script_unit.localization.create", "commerce.script_unit.localization.activate",
		"commerce.script_unit.storyboard.generate", "commerce.script_unit.storyboard.list", "commerce.script_unit.storyboard.update_shot", "commerce.script_unit.storyboard.reorder",
		"commerce.script_unit.reference_images.generate", "commerce.script_unit.reference_images.retry_failed",
		"commerce.script_unit.video_prompts.generate", "commerce.script_unit.video_prompts.retry_failed",
		"commerce.script_unit.shot_videos.generate", "commerce.script_unit.shot_videos.retry_failed", "commerce.script_unit.shot_videos.cancel",
		"commerce.script_unit.timeline.get", "commerce.script_unit.timeline.update",
		"commerce.script_unit.final.list", "commerce.script_unit.final.compose", "commerce.script_unit.final.activate",
		"commerce.script_unit.batch.advance", "commerce.script_unit.batch.retry_failed", "commerce.script_unit.batch.cancel",
	}
	for _, name := range expected {
		tool, ok := registry.Get(name)
		if !ok {
			t.Errorf("missing Commerce tool %s", name)
			continue
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("%s input schema is empty", name)
		}
	}
}

func TestCommerceWorkflowToolsDeclareChildWorkflowContract(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	for _, name := range []string{
		"commerce.script_unit.storyboard.generate",
		"commerce.script_unit.reference_images.generate",
		"commerce.script_unit.reference_images.retry_failed",
		"commerce.script_unit.video_prompts.generate",
		"commerce.script_unit.video_prompts.retry_failed",
		"commerce.script_unit.shot_videos.generate",
		"commerce.script_unit.shot_videos.retry_failed",
		"commerce.script_unit.final.compose",
		"commerce.script_unit.batch.advance",
		"commerce.script_unit.batch.retry_failed",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("missing Commerce workflow tool %s", name)
		}
		if !tool.StartsWorkflow || tool.Risk != ToolRiskWorkflow || !tool.RequiresApproval {
			t.Errorf("%s = risk %s, startsWorkflow %v, requiresApproval %v", name, tool.Risk, tool.StartsWorkflow, tool.RequiresApproval)
		}
	}
	for _, name := range []string{
		"commerce.product.rebuild",
		"commerce.script_unit.shot_videos.cancel",
		"commerce.script_unit.batch.cancel",
		"commerce.script_unit.final.activate",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("missing Commerce tool %s", name)
		}
		if tool.StartsWorkflow {
			t.Errorf("%s must not claim a child workflow", name)
		}
	}
}

func TestCommerceReferenceImageSchemaRequiresFrozenSelection(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("default registry: %v", err)
	}
	tool, ok := registry.Get("commerce.script_unit.reference_images.generate")
	if !ok {
		t.Fatal("reference image tool is missing")
	}
	var schema struct {
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	for _, key := range []string{"scriptUnitId", "operation", "planId", "expectedPlanRevision", "expectedUnitGenerationId", "shotIds"} {
		if !containsAgentSchemaKey(schema.Required, key) {
			t.Errorf("required fields do not contain %s: %v", key, schema.Required)
		}
	}
	operationEnum, _ := schema.Properties["operation"]["enum"].([]any)
	if len(operationEnum) != 2 {
		t.Fatalf("operation enum = %#v", schema.Properties["operation"]["enum"])
	}
}

func containsAgentSchemaKey(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
