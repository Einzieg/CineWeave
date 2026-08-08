package projectcontrol

import (
	"encoding/json"
	"fmt"
	"strings"
)

const SchemaVersionV1 = "project-control.v1"

type Risk string

const (
	RiskRead        Risk = "read"
	RiskDraft       Risk = "draft"
	RiskWrite       Risk = "write"
	RiskWorkflow    Risk = "workflow"
	RiskCosted      Risk = "costed"
	RiskDestructive Risk = "destructive"
	RiskAdmin       Risk = "admin"
)

type ScopeKind string

const (
	ScopeSystem       ScopeKind = "system"
	ScopeOrganization ScopeKind = "organization"
	ScopeWorkspace    ScopeKind = "workspace"
	ScopeProject      ScopeKind = "project"
)

type ExecutionMode string

const (
	ExecutionModeSync         ExecutionMode = "sync"
	ExecutionModeAsyncCommand ExecutionMode = "async_command"
	ExecutionModeWorkflow     ExecutionMode = "workflow"
)

type ActivityVisibility string

const (
	ActivityVisibilityPrimary   ActivityVisibility = "primary"
	ActivityVisibilityNested    ActivityVisibility = "nested"
	ActivityVisibilityAuditOnly ActivityVisibility = "audit_only"
)

type ControllerType string

const (
	ControllerEmbeddedAgent ControllerType = "embedded_agent"
	ControllerCodexMCP      ControllerType = "codex_mcp"
	ControllerManual        ControllerType = "manual"
)

type Effects struct {
	MaySpendProvider bool `json:"maySpendProvider"`
	StartsWorkflow   bool `json:"startsWorkflow"`
	WritesProject    bool `json:"writesProject"`
	Destructive      bool `json:"destructive"`
}

func (e Effects) ReadOnly() bool {
	return !e.MaySpendProvider && !e.StartsWorkflow && !e.WritesProject && !e.Destructive
}

type Descriptor struct {
	Name               string             `json:"name"`
	Version            int                `json:"version"`
	Label              string             `json:"label"`
	Summary            string             `json:"summary"`
	Description        string             `json:"description"`
	Risk               Risk               `json:"risk"`
	Scope              ScopeKind          `json:"scope"`
	Permission         string             `json:"permission,omitempty"`
	Permissions        []string           `json:"permissions"`
	ProjectKinds       []string           `json:"projectKinds"`
	InputSchema        json.RawMessage    `json:"inputSchema"`
	OutputSchema       json.RawMessage    `json:"outputSchema"`
	RequiresApproval   bool               `json:"requiresApproval"`
	Effects            Effects            `json:"effects"`
	ReadOnly           bool               `json:"readOnly"`
	Destructive        bool               `json:"destructive"`
	Idempotent         bool               `json:"idempotent"`
	Costed             bool               `json:"costed"`
	StartsWorkflow     bool               `json:"startsWorkflow"`
	SupportsDryRun     bool               `json:"supportsDryRun"`
	ExecutionMode      ExecutionMode      `json:"executionMode"`
	ActivityVisibility ActivityVisibility `json:"activityVisibility"`
	ExportToMCP        bool               `json:"exportToMcp"`
}

func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("project control action name is required")
	}
	if d.Version < 1 {
		return fmt.Errorf("project control action %s version must be positive", d.Name)
	}
	if strings.TrimSpace(d.Label) == "" || strings.TrimSpace(d.Summary) == "" {
		return fmt.Errorf("project control action %s label and summary are required", d.Name)
	}
	if !validRisk(d.Risk) {
		return fmt.Errorf("project control action %s risk %q is invalid", d.Name, d.Risk)
	}
	if !validScope(d.Scope) {
		return fmt.Errorf("project control action %s scope %q is invalid", d.Name, d.Scope)
	}
	if !validExecutionMode(d.ExecutionMode) {
		return fmt.Errorf("project control action %s execution mode %q is invalid", d.Name, d.ExecutionMode)
	}
	if !validActivityVisibility(d.ActivityVisibility) {
		return fmt.Errorf("project control action %s activity visibility %q is invalid", d.Name, d.ActivityVisibility)
	}
	if err := validateObjectSchema(d.Name, "input", d.InputSchema); err != nil {
		return err
	}
	if err := validateObjectSchema(d.Name, "output", d.OutputSchema); err != nil {
		return err
	}
	if d.ReadOnly != d.Effects.ReadOnly() {
		return fmt.Errorf("project control action %s read-only flag disagrees with effects", d.Name)
	}
	if d.Destructive != d.Effects.Destructive {
		return fmt.Errorf("project control action %s destructive flag disagrees with effects", d.Name)
	}
	if d.Costed != d.Effects.MaySpendProvider {
		return fmt.Errorf("project control action %s costed flag disagrees with effects", d.Name)
	}
	if d.StartsWorkflow != d.Effects.StartsWorkflow {
		return fmt.Errorf("project control action %s workflow flag disagrees with effects", d.Name)
	}
	if d.StartsWorkflow && d.ExecutionMode != ExecutionModeWorkflow {
		return fmt.Errorf("project control action %s starts a workflow but execution mode is %q", d.Name, d.ExecutionMode)
	}
	return nil
}

func (d Descriptor) Clone() Descriptor {
	d.Permissions = append([]string(nil), d.Permissions...)
	d.ProjectKinds = append([]string(nil), d.ProjectKinds...)
	d.InputSchema = cloneRawMessage(d.InputSchema)
	d.OutputSchema = cloneRawMessage(d.OutputSchema)
	return d
}

func validRisk(value Risk) bool {
	switch value {
	case RiskRead, RiskDraft, RiskWrite, RiskWorkflow, RiskCosted, RiskDestructive, RiskAdmin:
		return true
	default:
		return false
	}
}

func validScope(value ScopeKind) bool {
	switch value {
	case ScopeSystem, ScopeOrganization, ScopeWorkspace, ScopeProject:
		return true
	default:
		return false
	}
}

func validExecutionMode(value ExecutionMode) bool {
	switch value {
	case ExecutionModeSync, ExecutionModeAsyncCommand, ExecutionModeWorkflow:
		return true
	default:
		return false
	}
}

func validActivityVisibility(value ActivityVisibility) bool {
	switch value {
	case ActivityVisibilityPrimary, ActivityVisibilityNested, ActivityVisibilityAuditOnly:
		return true
	default:
		return false
	}
}

func validateObjectSchema(actionName, kind string, raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("project control action %s %s schema must be valid JSON", actionName, kind)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("decode project control action %s %s schema: %w", actionName, kind, err)
	}
	if schema["type"] != "object" {
		return fmt.Errorf("project control action %s %s schema must describe an object", actionName, kind)
	}
	return nil
}

func cloneRawMessage(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(in))
	copy(out, in)
	return out
}
