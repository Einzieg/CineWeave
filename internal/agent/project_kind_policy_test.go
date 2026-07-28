package agent

import (
	"strings"
	"testing"
)

func TestProjectKindPoliciesIsolateTools(t *testing.T) {
	narrative, err := PolicyForProjectKind(ProjectKindNarrative)
	if err != nil {
		t.Fatalf("narrative policy: %v", err)
	}
	commerce, err := PolicyForProjectKind(ProjectKindCommerceVideo)
	if err != nil {
		t.Fatalf("commerce policy: %v", err)
	}
	narrativeRegistry, err := NewRegistry(narrative.Tools()...)
	if err != nil {
		t.Fatalf("narrative registry: %v", err)
	}
	commerceRegistry, err := NewRegistry(commerce.Tools()...)
	if err != nil {
		t.Fatalf("commerce registry: %v", err)
	}
	for _, name := range []string{"agent.ask_user", "workflow.read_runs"} {
		if _, ok := narrativeRegistry.Get(name); !ok {
			t.Fatalf("narrative registry missing common tool %s", name)
		}
		if _, ok := commerceRegistry.Get(name); !ok {
			t.Fatalf("commerce registry missing common tool %s", name)
		}
	}
	for _, name := range []string{"source.list", "script.generate_from_source", "storyboard.list"} {
		if _, ok := narrativeRegistry.Get(name); !ok {
			t.Fatalf("narrative registry missing %s", name)
		}
		if _, ok := commerceRegistry.Get(name); ok {
			t.Fatalf("commerce registry must not contain narrative tool %s", name)
		}
	}
	for _, name := range []string{"commerce.product.get", "commerce.script.list", "commerce.video.generate", "commerce.script.derive.batch"} {
		if _, ok := commerceRegistry.Get(name); !ok {
			t.Fatalf("commerce registry missing %s", name)
		}
		if _, ok := narrativeRegistry.Get(name); ok {
			t.Fatalf("narrative registry must not contain Commerce tool %s", name)
		}
	}
	for _, legacy := range []string{"commerce.script_unit.storyboard.generate", "commerce.script_unit.video_prompts.generate", "commerce.script_unit.final.compose"} {
		if _, ok := commerceRegistry.Get(legacy); ok {
			t.Fatalf("active Commerce registry contains legacy tool %s", legacy)
		}
	}
}

func TestToolBelongsToDifferentProjectKind(t *testing.T) {
	if !ToolBelongsToDifferentProjectKind(ProjectKindCommerceVideo, "storyboard.list") {
		t.Fatal("storyboard.list must be recognized as a narrative-only tool")
	}
	if !ToolBelongsToDifferentProjectKind(ProjectKindNarrative, "commerce.video.generate") {
		t.Fatal("commerce.video.generate must be recognized as a Commerce-only tool")
	}
	if ToolBelongsToDifferentProjectKind(ProjectKindNarrative, "unknown.tool") {
		t.Fatal("unknown tool must not be classified as another project kind")
	}
}

func TestCommerceWorkflowEffectsAreStructured(t *testing.T) {
	policy, err := PolicyForProjectKind(ProjectKindCommerceVideo)
	if err != nil {
		t.Fatalf("commerce policy: %v", err)
	}
	registry, err := NewRegistry(policy.Tools()...)
	if err != nil {
		t.Fatalf("commerce registry: %v", err)
	}
	tool, ok := registry.Get("commerce.video.generate")
	if !ok {
		t.Fatal("commerce.video.generate missing")
	}
	effects := tool.Descriptor().Effects
	if !effects.MaySpendProvider || !effects.StartsWorkflow || !effects.WritesProject || effects.Destructive {
		t.Fatalf("commerce.video.generate effects = %+v", effects)
	}
}

func TestCommercePlannerRulesResolveOrdinalAndAskOnAmbiguity(t *testing.T) {
	policy, err := PolicyForProjectKind(ProjectKindCommerceVideo)
	if err != nil {
		t.Fatalf("commerce policy: %v", err)
	}
	rules := strings.Join(policy.PlannerRules, "\n")
	for _, fragment := range []string{
		"第 N 条脚本",
		"commerce.script.list",
		"stableOrdinal=N",
		"expectedRevision",
		"agent.ask_user",
		"禁止按标题",
		"commerce.script.revise",
		"后端读取完整正文",
	} {
		if !strings.Contains(rules, fragment) {
			t.Fatalf("commerce planner rules missing %q", fragment)
		}
	}
}
