package agent

import (
	"context"
	"encoding/json"
)

type ToolRisk string

const (
	ToolRiskRead        ToolRisk = "read"
	ToolRiskDraft       ToolRisk = "draft"
	ToolRiskWrite       ToolRisk = "write"
	ToolRiskWorkflow    ToolRisk = "workflow"
	ToolRiskCosted      ToolRisk = "costed"
	ToolRiskDestructive ToolRisk = "destructive"
	ToolRiskAdmin       ToolRisk = "admin"
)

type TaskMode string

const (
	TaskModePlanOnly    TaskMode = "plan_only"
	TaskModeSupervised  TaskMode = "supervised"
	TaskModeAutoLowRisk TaskMode = "auto_low_risk"
)

type PermissionMode string

const (
	PermissionModeRequireApproval PermissionMode = "require_approval"
	PermissionModeAutoApprove     PermissionMode = "auto_approve"
	PermissionModeFullAccess      PermissionMode = "full_access"
)

type ToolContext struct {
	OrganizationID string
	ProjectID      string
	SessionID      string
	TaskID         string
	StepID         string
	UserID         string
	IdempotencyKey string
	Constraints    map[string]any
	Metadata       map[string]any
}

type ToolResult struct {
	Name         string           `json:"name"`
	Label        string           `json:"label"`
	Status       string           `json:"status"`
	Summary      string           `json:"summary"`
	Arguments    map[string]any   `json:"arguments,omitempty"`
	Data         map[string]any   `json:"data,omitempty"`
	Retryable    bool             `json:"retryable,omitempty"`
	NextActions  []ToolNextAction `json:"nextActions,omitempty"`
	ErrorCode    string           `json:"errorCode,omitempty"`
	ErrorMessage string           `json:"errorMessage,omitempty"`
}

type ToolNextAction struct {
	Label     string         `json:"label"`
	Reason    string         `json:"reason,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolFunc func(context.Context, ToolContext, json.RawMessage) (ToolResult, error)

type AgentTool struct {
	Name             string
	Label            string
	Description      string
	Risk             ToolRisk
	Permission       string
	InputSchema      json.RawMessage
	RequiresApproval bool
	DryRun           ToolFunc
	Execute          ToolFunc
	Verifier         ToolFunc
}

type ToolDescriptor struct {
	Name             string          `json:"name"`
	Label            string          `json:"label"`
	Description      string          `json:"description"`
	Risk             ToolRisk        `json:"risk"`
	Permission       string          `json:"permission,omitempty"`
	InputSchema      json.RawMessage `json:"inputSchema"`
	RequiresApproval bool            `json:"requiresApproval"`
}

func (t AgentTool) Descriptor() ToolDescriptor {
	inputSchema := t.InputSchema
	if len(inputSchema) == 0 {
		inputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
	}
	return ToolDescriptor{
		Name:             t.Name,
		Label:            t.Label,
		Description:      t.Description,
		Risk:             t.Risk,
		Permission:       t.Permission,
		InputSchema:      cloneRawMessage(inputSchema),
		RequiresApproval: t.RequiresApproval,
	}
}

func cloneRawMessage(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(in))
	copy(out, in)
	return out
}
