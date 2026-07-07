package agent

type SupervisorPolicy struct {
	RequireApprovalForWrite       bool
	RequireApprovalForWorkflow    bool
	RequireApprovalForCosted      bool
	RequireApprovalForDestructive bool
	RequireApprovalForAdmin       bool
}

type SupervisionRequest struct {
	Tool              AgentTool
	Mode              TaskMode
	PermissionMode    PermissionMode
	UserHasPermission bool
}

type SupervisionDecision struct {
	Allowed          bool     `json:"allowed"`
	ExecutionAllowed bool     `json:"executionAllowed"`
	RequiresApproval bool     `json:"requiresApproval"`
	Risk             ToolRisk `json:"risk"`
	Permission       string   `json:"permission,omitempty"`
	Reasons          []string `json:"reasons,omitempty"`
}

func DefaultSupervisorPolicy() SupervisorPolicy {
	return SupervisorPolicy{
		RequireApprovalForWrite:       true,
		RequireApprovalForWorkflow:    true,
		RequireApprovalForCosted:      true,
		RequireApprovalForDestructive: true,
		RequireApprovalForAdmin:       true,
	}
}

func SuperviseTool(policy SupervisorPolicy, req SupervisionRequest) SupervisionDecision {
	decision := SupervisionDecision{
		Allowed:          true,
		ExecutionAllowed: req.Mode != TaskModePlanOnly,
		RequiresApproval: req.Tool.RequiresApproval,
		Risk:             req.Tool.Risk,
		Permission:       req.Tool.Permission,
	}
	if req.Tool.Permission != "" && !req.UserHasPermission {
		decision.Allowed = false
		decision.ExecutionAllowed = false
		decision.Reasons = append(decision.Reasons, "missing_permission")
		return decision
	}
	if req.Mode == TaskModePlanOnly {
		decision.RequiresApproval = false
		decision.Reasons = append(decision.Reasons, "plan_only")
		return decision
	}
	permissionMode := normalizePermissionMode(req.PermissionMode, req.Mode)
	switch req.Tool.Risk {
	case ToolRiskRead:
		decision.RequiresApproval = false
	case ToolRiskDraft:
		decision.RequiresApproval = req.Tool.RequiresApproval
	case ToolRiskWrite:
		decision.RequiresApproval = policy.RequireApprovalForWrite || req.Tool.RequiresApproval
	case ToolRiskWorkflow:
		decision.RequiresApproval = policy.RequireApprovalForWorkflow || req.Tool.RequiresApproval
	case ToolRiskCosted:
		decision.RequiresApproval = policy.RequireApprovalForCosted || req.Tool.RequiresApproval
	case ToolRiskDestructive:
		decision.RequiresApproval = policy.RequireApprovalForDestructive || req.Tool.RequiresApproval
	case ToolRiskAdmin:
		decision.RequiresApproval = policy.RequireApprovalForAdmin || req.Tool.RequiresApproval
	default:
		decision.Allowed = false
		decision.ExecutionAllowed = false
		decision.RequiresApproval = true
		decision.Reasons = append(decision.Reasons, "unknown_risk")
	}
	switch permissionMode {
	case PermissionModeAutoApprove:
		switch req.Tool.Risk {
		case ToolRiskWrite, ToolRiskWorkflow, ToolRiskCosted:
			decision.RequiresApproval = false
		}
	case PermissionModeFullAccess:
		decision.RequiresApproval = false
	}
	if decision.RequiresApproval {
		decision.ExecutionAllowed = false
		decision.Reasons = append(decision.Reasons, "approval_required")
	}
	return decision
}

func normalizePermissionMode(value PermissionMode, taskMode TaskMode) PermissionMode {
	switch value {
	case PermissionModeRequireApproval, PermissionModeAutoApprove, PermissionModeFullAccess:
		return value
	default:
		if taskMode == TaskModeAutoLowRisk {
			return PermissionModeAutoApprove
		}
		return PermissionModeRequireApproval
	}
}
