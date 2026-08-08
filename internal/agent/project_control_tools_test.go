package agent

import "testing"

func TestProjectControlToolsDeduplicatesCommonToolsAndPreservesProjectKinds(t *testing.T) {
	tools := ProjectControlTools()
	seen := make(map[string]AgentTool, len(tools))
	for _, tool := range tools {
		if _, exists := seen[tool.Name]; exists {
			t.Fatalf("tool %s is duplicated", tool.Name)
		}
		seen[tool.Name] = tool
	}

	common, ok := seen["workflow.cancel"]
	if !ok {
		t.Fatal("workflow.cancel is missing")
	}
	if len(common.ProjectKinds) != 2 || common.ProjectKinds[0] != ProjectKindCommerceVideo || common.ProjectKinds[1] != ProjectKindNarrative {
		t.Fatalf("workflow.cancel project kinds=%v", common.ProjectKinds)
	}
	if _, ok := seen["script.generate_from_source"]; !ok {
		t.Fatal("narrative tool is missing")
	}
	if _, ok := seen["commerce.video.generate"]; !ok {
		t.Fatal("commerce tool is missing")
	}
	for _, name := range []string{
		"commerce.product.versions.list",
		"commerce.product.version.create",
		"commerce.product.rebuild_impact",
		"commerce.product.rebuild",
		"commerce.product.reference.archive",
		"commerce.product.reference.set_primary",
		"commerce.script.duplicate",
		"commerce.script.create_language_variant",
		"commerce.script.reorder",
	} {
		tool, ok := seen[name]
		if !ok {
			t.Fatalf("commerce tool %s is missing", name)
		}
		if len(tool.ProjectKinds) != 1 || tool.ProjectKinds[0] != ProjectKindCommerceVideo {
			t.Fatalf("commerce tool %s project kinds=%v", name, tool.ProjectKinds)
		}
	}
	if seen["agent.ask_user"].Descriptor().ExportToMCP {
		t.Fatal("agent.ask_user must remain embedded-assistant only")
	}
}
