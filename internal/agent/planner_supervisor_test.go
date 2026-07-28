package agent

import "testing"

func TestParsePlanFencedJSON(t *testing.T) {
	plan, err := ParsePlan("```json\n{\"summary\":\"先读取项目\",\"steps\":[{\"tool\":\"project.read_summary\",\"args\":{}}]}\n```")
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if plan.Summary != "先读取项目" || len(plan.Steps) != 1 || plan.Steps[0].Tool != "project.read_summary" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestParsePlanToolCalls(t *testing.T) {
	plan, err := ParsePlan(`{"message":"查任务","tool_calls":[{"name":"workflow.read_runs","arguments":{"limit":5}}]}`)
	if err != nil {
		t.Fatalf("parse tool calls: %v", err)
	}
	if plan.Summary != "查任务" || len(plan.Steps) != 1 || plan.Steps[0].Tool != "workflow.read_runs" {
		t.Fatalf("plan = %+v", plan)
	}
	if string(plan.Steps[0].Args) != `{"limit":5}` {
		t.Fatalf("args = %s", string(plan.Steps[0].Args))
	}
}

func TestParsePlanAllowsExplicitCompletion(t *testing.T) {
	plan, err := ParsePlan(`{"summary":"目标已经完成","complete":true,"steps":[]}`)
	if err != nil {
		t.Fatalf("parse completion: %v", err)
	}
	if !plan.Complete || len(plan.Steps) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if _, err := ValidatePlan(plan, registry, 1); err != nil {
		t.Fatalf("validate completion: %v", err)
	}
}

func TestValidatePlanRejectsEmptyIncompletePlan(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if _, err := ValidatePlan(Plan{Summary: "没有动作"}, registry, 1); err == nil {
		t.Fatal("expected incomplete empty plan to fail")
	}
}

func TestValidatePlanRejectsUnknownTools(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, err = ValidatePlan(Plan{Steps: []PlanStep{{Tool: "unknown.tool"}}}, registry, 10)
	if err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestParsePlanRejectsInvalidJSON(t *testing.T) {
	if _, err := ParsePlan(`{"steps":[`); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestValidatePlanClampsMaxSteps(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	plan, err := ValidatePlan(Plan{Steps: []PlanStep{
		{Tool: "project.read_summary"},
		{Tool: "workflow.read_runs"},
		{Tool: "artifact.list"},
	}}, registry, 2)
	if err != nil {
		t.Fatalf("validate plan: %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(plan.Steps))
	}
}

func TestValidatePlanFillsRiskAndApproval(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	plan, err := ValidatePlan(Plan{Steps: []PlanStep{{Tool: "workflow.start", Args: []byte(`{"workflowType":"script_to_storyboard"}`)}}}, registry, 10)
	if err != nil {
		t.Fatalf("validate plan: %v", err)
	}
	if plan.Steps[0].Risk != ToolRiskWorkflow || !plan.Steps[0].RequiresApproval {
		t.Fatalf("step = %+v", plan.Steps[0])
	}
}

func TestValidatePlanRejectsMissingRequiredArgs(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, err = ValidatePlan(Plan{Steps: []PlanStep{{Tool: "workflow.start", Args: []byte(`{}`)}}}, registry, 10)
	if err == nil {
		t.Fatal("expected missing workflowType error")
	}
}

func TestValidatePlanRejectsUnknownArgs(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, err = ValidatePlan(Plan{Steps: []PlanStep{{Tool: "workflow.read_runs", Args: []byte(`{"limit":5,"unexpected":true}`)}}}, registry, 10)
	if err == nil {
		t.Fatal("expected unknown arg error")
	}
}

func TestValidatePlanRejectsWrongArgType(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	_, err = ValidatePlan(Plan{Steps: []PlanStep{{Tool: "workflow.read_runs", Args: []byte(`{"limit":"five"}`)}}}, registry, 10)
	if err == nil {
		t.Fatal("expected wrong arg type error")
	}
}

func TestSuperviseToolPlanOnlyDoesNotExecute(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool:              AgentTool{Name: "workflow.start", Risk: ToolRiskWorkflow, Permission: "workflow.run", RequiresApproval: true},
		Mode:              TaskModePlanOnly,
		UserHasPermission: true,
	})
	if !decision.Allowed || decision.ExecutionAllowed || decision.RequiresApproval {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSuperviseToolRequiresApprovalForWorkflow(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool:              AgentTool{Name: "workflow.start", Risk: ToolRiskWorkflow, Permission: "workflow.run"},
		Mode:              TaskModeSupervised,
		UserHasPermission: true,
	})
	if !decision.Allowed || decision.ExecutionAllowed || !decision.RequiresApproval {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSuperviseToolUsesEffectsInsteadOfToolName(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool: AgentTool{
			Name:       "arbitrary.provider.operation",
			Risk:       ToolRiskDraft,
			Permission: "script.read",
			Effects: ToolEffects{
				MaySpendProvider: true,
			},
		},
		Mode:              TaskModeSupervised,
		UserHasPermission: true,
	})
	if !decision.Allowed || decision.ExecutionAllowed || !decision.RequiresApproval {
		t.Fatalf("decision = %+v", decision)
	}
	if !decision.Effects.MaySpendProvider {
		t.Fatalf("decision effects = %+v", decision.Effects)
	}
}

func TestSuperviseToolAutoApproveAllowsWorkflow(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool:              AgentTool{Name: "workflow.start", Risk: ToolRiskWorkflow, Permission: "workflow.run", RequiresApproval: true},
		Mode:              TaskModeSupervised,
		PermissionMode:    PermissionModeAutoApprove,
		UserHasPermission: true,
	})
	if !decision.Allowed || !decision.ExecutionAllowed || decision.RequiresApproval {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSuperviseToolAutoApproveStillProtectsAdmin(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool:              AgentTool{Name: "provider.update_account", Risk: ToolRiskAdmin, Permission: "provider.manage", RequiresApproval: true},
		Mode:              TaskModeSupervised,
		PermissionMode:    PermissionModeAutoApprove,
		UserHasPermission: true,
	})
	if !decision.Allowed || decision.ExecutionAllowed || !decision.RequiresApproval {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSuperviseToolFullAccessSkipsAdminApproval(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool:              AgentTool{Name: "provider.update_account", Risk: ToolRiskAdmin, Permission: "provider.manage", RequiresApproval: true},
		Mode:              TaskModeSupervised,
		PermissionMode:    PermissionModeFullAccess,
		UserHasPermission: true,
	})
	if !decision.Allowed || !decision.ExecutionAllowed || decision.RequiresApproval {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestSuperviseToolBlocksMissingPermission(t *testing.T) {
	decision := SuperviseTool(DefaultSupervisorPolicy(), SupervisionRequest{
		Tool:              AgentTool{Name: "provider.update_account", Risk: ToolRiskAdmin, Permission: "provider.manage"},
		Mode:              TaskModeSupervised,
		UserHasPermission: false,
	})
	if decision.Allowed || decision.ExecutionAllowed {
		t.Fatalf("decision = %+v", decision)
	}
}
