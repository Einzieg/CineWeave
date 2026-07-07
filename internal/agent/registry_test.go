package agent

import "testing"

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
