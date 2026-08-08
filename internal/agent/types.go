package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type ToolRisk = projectcontrol.Risk

const (
	ToolRiskRead        = projectcontrol.RiskRead
	ToolRiskDraft       = projectcontrol.RiskDraft
	ToolRiskWrite       = projectcontrol.RiskWrite
	ToolRiskWorkflow    = projectcontrol.RiskWorkflow
	ToolRiskCosted      = projectcontrol.RiskCosted
	ToolRiskDestructive = projectcontrol.RiskDestructive
	ToolRiskAdmin       = projectcontrol.RiskAdmin
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
	Name                string           `json:"name"`
	Label               string           `json:"label"`
	Status              string           `json:"status"`
	Summary             string           `json:"summary"`
	Arguments           map[string]any   `json:"arguments,omitempty"`
	Data                map[string]any   `json:"data,omitempty"`
	ChildWorkflowRunIDs []string         `json:"childWorkflowRunIds,omitempty"`
	Retryable           bool             `json:"retryable,omitempty"`
	NextActions         []ToolNextAction `json:"nextActions,omitempty"`
	ErrorCode           string           `json:"errorCode,omitempty"`
	ErrorMessage        string           `json:"errorMessage,omitempty"`
}

type ToolNextAction struct {
	Label     string         `json:"label"`
	Reason    string         `json:"reason,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolFunc func(context.Context, ToolContext, json.RawMessage) (ToolResult, error)

type ToolEffects = projectcontrol.Effects

type AgentTool struct {
	Name               string
	Version            int
	Label              string
	Description        string
	Risk               ToolRisk
	Permission         string
	Permissions        []string
	ProjectKinds       []string
	InputSchema        json.RawMessage
	OutputSchema       json.RawMessage
	RequiresApproval   bool
	Effects            ToolEffects
	ActivityVisibility projectcontrol.ActivityVisibility
	ExportToMCP        *bool
	// StartsWorkflow is retained while older tool declarations migrate to Effects.
	StartsWorkflow bool
	DryRun         ToolFunc
	Execute        ToolFunc
	Verifier       ToolFunc
}

type ToolDescriptor = projectcontrol.Descriptor

func (t AgentTool) RequiredPermissions() []string {
	permissions := make([]string, 0, len(t.Permissions)+1)
	seen := make(map[string]struct{}, len(t.Permissions)+1)
	appendPermission := func(permission string) {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			return
		}
		if _, exists := seen[permission]; exists {
			return
		}
		seen[permission] = struct{}{}
		permissions = append(permissions, permission)
	}
	appendPermission(t.Permission)
	for _, permission := range t.Permissions {
		appendPermission(permission)
	}
	return permissions
}

func (t AgentTool) EffectiveEffects() ToolEffects {
	effects := t.Effects
	if t.StartsWorkflow {
		effects.StartsWorkflow = true
	}
	switch t.Risk {
	case ToolRiskWrite:
		effects.WritesProject = true
	case ToolRiskWorkflow:
		effects.WritesProject = true
	case ToolRiskCosted:
		effects.MaySpendProvider = true
	case ToolRiskDestructive:
		effects.WritesProject = true
		effects.Destructive = true
	case ToolRiskAdmin:
		effects.WritesProject = true
	}
	return effects
}

func (t AgentTool) Descriptor() ToolDescriptor {
	inputSchema := t.InputSchema
	if len(inputSchema) == 0 {
		inputSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
	}
	outputSchema := t.OutputSchema
	if len(outputSchema) == 0 {
		outputSchema = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	version := t.Version
	if version < 1 {
		version = 1
	}
	effects := t.EffectiveEffects()
	executionMode := projectcontrol.ExecutionModeSync
	if effects.StartsWorkflow {
		executionMode = projectcontrol.ExecutionModeWorkflow
	} else if effects.MaySpendProvider {
		executionMode = projectcontrol.ExecutionModeAsyncCommand
	}
	visibility := t.ActivityVisibility
	if visibility == "" {
		visibility = projectcontrol.ActivityVisibilityPrimary
		if effects.ReadOnly() {
			visibility = projectcontrol.ActivityVisibilityAuditOnly
		}
	}
	exportToMCP := t.Name != "agent.ask_user"
	if t.ExportToMCP != nil {
		exportToMCP = *t.ExportToMCP
	}
	return ToolDescriptor{
		Name:               t.Name,
		Version:            version,
		Label:              t.Label,
		Summary:            t.Label,
		Description:        t.Description,
		Risk:               t.Risk,
		Scope:              projectcontrol.ScopeProject,
		Permission:         t.Permission,
		Permissions:        t.RequiredPermissions(),
		ProjectKinds:       append([]string(nil), t.ProjectKinds...),
		InputSchema:        cloneRawMessage(inputSchema),
		OutputSchema:       cloneRawMessage(outputSchema),
		RequiresApproval:   t.RequiresApproval,
		Effects:            effects,
		ReadOnly:           effects.ReadOnly(),
		Destructive:        effects.Destructive,
		Idempotent:         true,
		Costed:             effects.MaySpendProvider,
		StartsWorkflow:     effects.StartsWorkflow,
		SupportsDryRun:     t.DryRun != nil,
		ExecutionMode:      executionMode,
		ActivityVisibility: visibility,
		ExportToMCP:        exportToMCP,
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
