package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/client"
)

type AgentTask struct {
	ID                 string          `json:"id"`
	OrganizationID     string          `json:"organizationId"`
	ProjectID          string          `json:"projectId"`
	SessionID          *string         `json:"sessionId,omitempty"`
	AgentType          string          `json:"agentType"`
	UserGoal           string          `json:"userGoal"`
	Mode               string          `json:"mode"`
	Status             string          `json:"status"`
	TemporalWorkflowID *string         `json:"temporalWorkflowId,omitempty"`
	Constraints        json.RawMessage `json:"constraints"`
	Plan               json.RawMessage `json:"plan"`
	Summary            json.RawMessage `json:"summary"`
	ErrorCode          *string         `json:"errorCode,omitempty"`
	ErrorMessage       *string         `json:"errorMessage,omitempty"`
	CreatedBy          *string         `json:"createdBy,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	StartedAt          *time.Time      `json:"startedAt,omitempty"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
	Steps              []AgentStep     `json:"steps,omitempty"`
	Approvals          []AgentApproval `json:"approvals,omitempty"`
}

type AgentStep struct {
	ID                 string          `json:"id"`
	TaskID             string          `json:"taskId"`
	StepIndex          int             `json:"stepIndex"`
	ToolName           string          `json:"toolName"`
	Risk               string          `json:"risk"`
	Permission         *string         `json:"permission,omitempty"`
	Status             string          `json:"status"`
	RequiresApproval   bool            `json:"requiresApproval"`
	Input              json.RawMessage `json:"input"`
	DryRunOutput       json.RawMessage `json:"dryRunOutput"`
	SupervisorDecision json.RawMessage `json:"supervisorDecision"`
	Output             json.RawMessage `json:"output"`
	VerifierOutput     json.RawMessage `json:"verifierOutput"`
	ErrorCode          *string         `json:"errorCode,omitempty"`
	ErrorMessage       *string         `json:"errorMessage,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	StartedAt          *time.Time      `json:"startedAt,omitempty"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
}

type AgentApproval struct {
	ID               string          `json:"id"`
	TaskID           string          `json:"taskId"`
	StepID           *string         `json:"stepId,omitempty"`
	ApprovalType     string          `json:"approvalType"`
	Status           string          `json:"status"`
	RequestedPayload json.RawMessage `json:"requestedPayload"`
	DecisionPayload  json.RawMessage `json:"decisionPayload"`
	DecidedBy        *string         `json:"decidedBy,omitempty"`
	DecidedAt        *time.Time      `json:"decidedAt,omitempty"`
	ExpiresAt        *time.Time      `json:"expiresAt,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

func (s *Server) listAgentTools(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	registry, err := s.projectAgentRegistry()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items := make([]agent.ToolDescriptor, 0)
	for _, tool := range registry.List() {
		if strings.TrimSpace(tool.Permission) != "" {
			resource := agentToolPermissionResource(project, tool.Permission)
			if err := s.authorizer.Authorize(r.Context(), principal, tool.Permission, resource); err != nil {
				continue
			}
		}
		items = append(items, tool.Descriptor())
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createAgentTask(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	var req struct {
		SessionID   *string         `json:"sessionId"`
		Goal        string          `json:"goal"`
		Mode        string          `json:"mode"`
		Constraints json.RawMessage `json:"constraints"`
	}
	if !decode(w, r, &req) {
		return
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "goal is required", nil, false)
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = string(agent.TaskModeSupervised)
	}
	if !validAgentTaskMode(mode) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "mode is invalid", nil, false)
		return
	}
	if mode != string(agent.TaskModePlanOnly) {
		if err := s.enforceAgentProjectTaskConcurrency(r.Context(), project.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	constraints, ok := jsonObjectOrDefault(w, r, req.Constraints)
	if !ok {
		return
	}
	if permissionMode := strings.TrimSpace(stringValueFromAny(rawObject(constraints)["permissionMode"])); permissionMode != "" && !validAgentPermissionMode(permissionMode) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "permissionMode is invalid", nil, false)
		return
	}
	sessionID := ""
	if req.SessionID != nil && strings.TrimSpace(*req.SessionID) != "" {
		trimmed := strings.TrimSpace(*req.SessionID)
		if !s.agentSessionBelongsToAnyProjectAgent(r, project.ID, trimmed) {
			httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "agent session was not found", nil, false)
			return
		}
		sessionID = trimmed
	}
	item, err := scanAgentTask(s.db.QueryRow(r.Context(), `
		INSERT INTO agent_tasks(
			organization_id, project_id, session_id, agent_type, user_goal, mode, status,
			constraints, plan, summary, created_by
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, 'project_agent', $4, $5, 'queued', $6, '{}'::jsonb, '{}'::jsonb, $7)
		RETURNING id, organization_id, project_id, session_id::text, agent_type, user_goal, mode, status,
		          temporal_workflow_id,
		          constraints, plan, summary, error_code, error_message, created_by::text, created_at,
		          updated_at, started_at, completed_at
	`, project.OrganizationID, project.ID, sessionID, goal, mode, constraints, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.agentTaskTemporalEnabled() {
		item, err = s.startAgentTaskWorkflow(r, principal, project, item)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
		return
	}
	item, err = s.planAgentTask(r, principal, project, item)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if item.Mode != string(agent.TaskModePlanOnly) {
		item, err = s.executeAgentTaskReadySteps(r, principal, project, item.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) agentTaskTemporalEnabled() bool {
	return s.temporal != nil
}

func (s *Server) agentTaskHasTemporal(task AgentTask) bool {
	return s.temporal != nil && task.TemporalWorkflowID != nil && strings.TrimSpace(*task.TemporalWorkflowID) != ""
}

func (s *Server) enforceAgentProjectTaskConcurrency(ctx context.Context, projectID string) error {
	limit := agentProjectTaskConcurrencyLimit()
	if limit <= 0 {
		return nil
	}
	var active int
	if err := s.db.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_tasks
		WHERE project_id = $1
		  AND status IN ('queued', 'planning', 'running', 'waiting_approval')
	`, projectID).Scan(&active); err != nil {
		return err
	}
	if active >= limit {
		return apiError{
			Status:  http.StatusConflict,
			Code:    "AGENT_PROJECT_CONCURRENCY_LIMIT",
			Message: "project has reached the active Agent task concurrency limit",
			Details: map[string]any{
				"activeTaskCount": active,
				"limit":           limit,
			},
			Retryable: true,
		}
	}
	return nil
}

func agentProjectTaskConcurrencyLimit() int {
	raw := strings.TrimSpace(os.Getenv("CINEWEAVE_AGENT_MAX_ACTIVE_TASKS_PER_PROJECT"))
	if raw == "" {
		return 3
	}
	var limit int
	if _, err := fmt.Sscan(raw, &limit); err != nil {
		return 3
	}
	return limit
}

func projectAgentTemporalWorkflowID(taskID string) string {
	return "project-agent-" + taskID
}

func (s *Server) startAgentTaskWorkflow(r *http.Request, principal auth.Principal, project Project, task AgentTask) (AgentTask, error) {
	workflowID := projectAgentTemporalWorkflowID(task.ID)
	if _, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: workflows.ScriptTaskQueue,
	}, workflows.ProjectAgentWorkflow, workflows.ProjectAgentWorkflowInput{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		UserID:         principal.UserID,
	}); err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET status = 'failed',
			    temporal_workflow_id = $2,
			    error_code = 'AGENT_WORKFLOW_START_FAILED',
			    error_message = $3,
			    completed_at = now()
			WHERE id = $1
		`, task.ID, workflowID, err.Error())
		return AgentTask{}, err
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE agent_tasks
		SET temporal_workflow_id = $2,
		    summary = jsonb_set(COALESCE(summary, '{}'::jsonb), '{temporalWorkflowId}', to_jsonb($2::text), true)
		WHERE id = $1
	`, task.ID, workflowID); err != nil {
		return AgentTask{}, err
	}
	return s.agentTaskWithDetails(r, project.ID, task.ID)
}

func (s *Server) signalAgentTaskWorkflow(ctx context.Context, task AgentTask, signalName string, payload any) error {
	if !s.agentTaskHasTemporal(task) {
		return nil
	}
	return s.temporal.SignalWorkflow(ctx, strings.TrimSpace(*task.TemporalWorkflowID), "", signalName, payload)
}

func (s *Server) planAgentTask(r *http.Request, principal auth.Principal, project Project, task AgentTask) (AgentTask, error) {
	registry, err := s.projectAgentRegistry()
	if err != nil {
		return AgentTask{}, err
	}
	prompt, err := s.buildAgentPlannerPrompt(r, principal, project, task, registry)
	if err != nil {
		return AgentTask{}, err
	}
	promptHash := promptsvc.HashText(prompt)
	var runID string
	if err := s.db.QueryRow(r.Context(), `
		INSERT INTO agent_runs(
			organization_id, project_id, session_id, agent_type, task_type, status,
			input, prompt_hash, task_id, created_by, started_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, 'project_agent', 'plan', 'running', $4, $5, $6, $7, now())
		RETURNING id
	`, project.OrganizationID, project.ID, stringValue(task.SessionID), mustMarshal(map[string]any{
		"goal":            task.UserGoal,
		"mode":            task.Mode,
		"constraints":     json.RawMessage(task.Constraints),
		"modelProfileKey": project.ScriptModelProfileKey,
	}), promptHash, task.ID, principal.UserID).Scan(&runID); err != nil {
		return AgentTask{}, err
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE agent_tasks
		SET status = 'planning', started_at = COALESCE(started_at, now())
		WHERE id = $1
	`, task.ID); err != nil {
		return AgentTask{}, err
	}
	if autoPlan, ok, err := s.agentAutoProductionPlan(r, project, task); err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_runs
			SET status = 'failed', error_code = 'AGENT_AUTO_PLAN_FAILED', error_message = $2, completed_at = now()
			WHERE id = $1
		`, runID, err.Error())
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET status = 'failed', error_code = 'AGENT_AUTO_PLAN_FAILED', error_message = $2, completed_at = now()
			WHERE id = $1
		`, task.ID, err.Error())
		return AgentTask{}, err
	} else if ok {
		plan, err := agent.ValidatePlan(autoPlan, registry, 20)
		if err != nil {
			_, _ = s.db.Exec(r.Context(), `
				UPDATE agent_runs
				SET status = 'failed', error_code = 'AGENT_PLAN_INVALID', error_message = $2,
				    output = $3, completed_at = now()
				WHERE id = $1
			`, runID, err.Error(), mustMarshal(map[string]any{"parsed": autoPlan, "plannerMode": "deterministic"}))
			_, _ = s.db.Exec(r.Context(), `
				UPDATE agent_tasks
				SET status = 'failed', error_code = 'AGENT_PLAN_INVALID', error_message = $2, plan = $3, completed_at = now()
				WHERE id = $1
			`, task.ID, err.Error(), mustMarshal(autoPlan))
			return AgentTask{}, err
		}
		if err := s.persistAgentPlan(r, principal, project, task, registry, plan, runID, provider.GatewayTextResponse{}); err != nil {
			return AgentTask{}, err
		}
		return s.agentTaskWithDetails(r, project.ID, task.ID)
	}

	gatewayResp, err := provider.NewGatewayClientFromEnv().GenerateText(r.Context(), provider.GatewayTextRequest{
		OrganizationID:    project.OrganizationID,
		WorkspaceID:       project.WorkspaceID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: "project_agent_plan",
		PromptHash:        promptHash,
		PromptSource:      "inline",
		Input: mustMarshal(map[string]any{
			"prompt":         prompt,
			"responseFormat": "json",
		}),
		Options: provider.GatewayTextOptions{
			IdempotencyKey: "agent-task-plan:" + task.ID,
		},
	})
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_runs
			SET status = 'failed', error_code = 'PROVIDER_GATEWAY_ERROR', error_message = $2, completed_at = now()
			WHERE id = $1
		`, runID, err.Error())
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET status = 'failed', error_code = 'PROVIDER_GATEWAY_ERROR', error_message = $2, completed_at = now()
			WHERE id = $1
		`, task.ID, err.Error())
		return AgentTask{}, err
	}
	rawPlan := strings.TrimSpace(gatewayResp.Output.Text)
	if rawPlan == "" {
		rawPlan = strings.TrimSpace(string(gatewayResp.Output.Raw))
	}
	parsed, err := agent.ParsePlan(rawPlan)
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_runs
			SET status = 'failed', error_code = 'AGENT_PLAN_INVALID', error_message = $2,
			    output = $3, provider_call_id = NULLIF($4, '')::uuid, completed_at = now()
			WHERE id = $1
		`, runID, err.Error(), mustMarshal(map[string]any{"raw": rawPlan}), gatewayResp.ProviderCallID)
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET status = 'failed', error_code = 'AGENT_PLAN_INVALID', error_message = $2, summary = $3, completed_at = now()
			WHERE id = $1
		`, task.ID, err.Error(), mustMarshal(map[string]any{"rawPlan": rawPlan}))
		return AgentTask{}, err
	}
	plan, err := agent.ValidatePlan(parsed, registry, 20)
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_runs
			SET status = 'failed', error_code = 'AGENT_PLAN_INVALID', error_message = $2,
			    output = $3, provider_call_id = NULLIF($4, '')::uuid, completed_at = now()
			WHERE id = $1
		`, runID, err.Error(), mustMarshal(map[string]any{"raw": rawPlan, "parsed": parsed}), gatewayResp.ProviderCallID)
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET status = 'failed', error_code = 'AGENT_PLAN_INVALID', error_message = $2, plan = $3, completed_at = now()
			WHERE id = $1
		`, task.ID, err.Error(), mustMarshal(parsed))
		return AgentTask{}, err
	}
	if err := s.persistAgentPlan(r, principal, project, task, registry, plan, runID, gatewayResp); err != nil {
		return AgentTask{}, err
	}
	return s.agentTaskWithDetails(r, project.ID, task.ID)
}

func (s *Server) buildAgentPlannerPrompt(r *http.Request, principal auth.Principal, project Project, task AgentTask, registry *agent.Registry) (string, error) {
	var production any
	if status, err := s.productionStatus(r, project); err == nil {
		production = status
	} else {
		production = map[string]any{"error": err.Error()}
	}
	toolDescriptors := make([]agent.ToolDescriptor, 0)
	for _, tool := range registry.List() {
		if strings.TrimSpace(tool.Permission) != "" {
			resource := agentToolPermissionResource(project, tool.Permission)
			if err := s.authorizer.Authorize(r.Context(), principal, tool.Permission, resource); err != nil {
				continue
			}
		}
		toolDescriptors = append(toolDescriptors, tool.Descriptor())
	}
	permissionMode := agentPermissionModeForTask(task)
	toolsJSON := string(mustMarshal(toolDescriptors))
	contextJSON := string(mustMarshal(map[string]any{
		"project": map[string]any{
			"id":                    project.ID,
			"name":                  project.Name,
			"contentType":           stringValue(project.ContentType),
			"videoRatio":            project.VideoRatio,
			"artStyle":              project.ArtStyle,
			"productionMode":        project.ProductionMode,
			"scriptModelProfileKey": project.ScriptModelProfileKey,
			"imageModelProfileKey":  project.ImageModelProfileKey,
			"videoModelProfileKey":  project.VideoModelProfileKey,
		},
		"productionStatus": production,
		"constraints":      json.RawMessage(task.Constraints),
		"permissionMode":   permissionMode,
	}))
	var builder strings.Builder
	builder.WriteString("你是 CineWeave Project Agent Planner。你的职责是把用户目标拆成受控工具计划，只输出 JSON。\n")
	builder.WriteString("不要执行工具，不要假装已经完成动作。不要虚构 sourceId、scriptId、workflowRunId、assetId、shotId；缺少 ID 时先安排读取类工具。\n")
	builder.WriteString("JSON 格式必须为：{\"summary\":\"中文摘要\",\"steps\":[{\"tool\":\"工具名\",\"args\":{},\"expectedResult\":\"预期结果\"}]}。\n")
	builder.WriteString("plan_only 模式只允许产出计划，不代表会执行。权限模式由后端监督器裁决，require_approval 需要人工批准，auto_approve 自动放行写入/工作流/成本步骤，full_access 自动放行管理步骤。\n\n")
	builder.WriteString("用户目标：\n")
	builder.WriteString(task.UserGoal)
	builder.WriteString("\n\n任务模式：")
	builder.WriteString(task.Mode)
	builder.WriteString("\n权限模式：")
	builder.WriteString(string(permissionMode))
	builder.WriteString("\n\n当前项目上下文 JSON：\n")
	builder.WriteString(contextJSON)
	builder.WriteString("\n\n可用工具 JSON：\n")
	builder.WriteString(toolsJSON)
	return builder.String(), nil
}

func (s *Server) persistAgentPlan(r *http.Request, principal auth.Principal, project Project, task AgentTask, registry *agent.Registry, plan agent.Plan, runID string, gatewayResp provider.GatewayTextResponse) error {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())

	mode := agent.TaskMode(task.Mode)
	permissionMode := agentPermissionModeForTask(task)
	pendingApprovals := 0
	blockedSteps := 0
	policy := agent.DefaultSupervisorPolicy()
	var stepOffset int
	if err := tx.QueryRow(r.Context(), `
		SELECT COALESCE(MAX(step_index), 0)
		FROM agent_steps
		WHERE task_id = $1
	`, task.ID).Scan(&stepOffset); err != nil {
		return err
	}
	for index, step := range plan.Steps {
		tool, ok := registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("unknown agent tool %q", step.Tool)
		}
		hasPermission := true
		if strings.TrimSpace(tool.Permission) != "" {
			resource := agentToolPermissionResource(project, tool.Permission)
			hasPermission = s.authorizer.Authorize(r.Context(), principal, tool.Permission, resource) == nil
		}
		decision := agent.SuperviseTool(policy, agent.SupervisionRequest{
			Tool:              tool,
			Mode:              mode,
			PermissionMode:    permissionMode,
			UserHasPermission: hasPermission,
		})
		stateGate := s.superviseAgentStepState(r, project, task, step.Tool, step.Args)
		if !stateGate.Allowed {
			decision.Allowed = false
			decision.ExecutionAllowed = false
			decision.RequiresApproval = false
			decision.Reasons = agentReasonsWithout(decision.Reasons, "approval_required")
			decision.Reasons = append(decision.Reasons, stateGate.Reason)
		}
		dryRunOutput := s.agentStepDryRunOutput(r, project, step.Tool, step.Args)
		if stateGate.Reason != "" || len(stateGate.Details) > 0 {
			dryRunOutput["stateGate"] = stateGate
		}
		stepStatus := "planned"
		if !decision.Allowed {
			stepStatus = "blocked"
			blockedSteps++
		} else if decision.RequiresApproval {
			stepStatus = "waiting_approval"
			pendingApprovals++
		}
		var stepID string
		stepIndex := stepOffset + index + 1
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO agent_steps(
				task_id, step_index, tool_name, risk, permission, status, requires_approval,
				input, dry_run_output, supervisor_decision, output, verifier_output,
				error_code, error_message
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10, '{}'::jsonb, '{}'::jsonb, NULLIF($11, ''), NULLIF($12, ''))
			RETURNING id
		`, task.ID, stepIndex, step.Tool, string(step.Risk), tool.Permission, stepStatus, decision.RequiresApproval, step.Args,
			mustMarshal(dryRunOutput),
			mustMarshal(map[string]any{"decision": decision, "expectedResult": step.ExpectedResult, "stateGate": stateGate}),
			agentStepErrorCode(decision), agentStepErrorMessage(decision)).Scan(&stepID); err != nil {
			return err
		}
		if decision.RequiresApproval {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO agent_approvals(task_id, step_id, approval_type, status, requested_payload, decision_payload)
				VALUES ($1, $2, $3, 'pending', $4, '{}'::jsonb)
			`, task.ID, stepID, string(step.Risk), mustMarshal(map[string]any{
				"tool":           step.Tool,
				"risk":           step.Risk,
				"permission":     tool.Permission,
				"args":           json.RawMessage(step.Args),
				"expectedResult": step.ExpectedResult,
				"decision":       decision,
				"permissionMode": permissionMode,
				"dryRunOutput":   dryRunOutput,
			})); err != nil {
				return err
			}
		}
	}
	taskStatus := "queued"
	completedAt := sql.NullTime{}
	if blockedSteps > 0 {
		taskStatus = "blocked"
	} else if mode == agent.TaskModePlanOnly {
		taskStatus = "succeeded"
		completedAt = sql.NullTime{Time: time.Now(), Valid: true}
	} else if pendingApprovals > 0 {
		taskStatus = "waiting_approval"
	}
	plannerMode := "provider_gateway"
	if strings.TrimSpace(gatewayResp.ProviderCallID) == "" {
		plannerMode = "deterministic"
	}
	summary := map[string]any{
		"summary":                plan.Summary,
		"plannerProviderCallId":  gatewayResp.ProviderCallID,
		"plannerMode":            plannerMode,
		"modelId":                gatewayResp.ModelID,
		"permissionMode":         permissionMode,
		"pendingApprovals":       pendingApprovals,
		"blockedSteps":           blockedSteps,
		"appendedAfterStepIndex": stepOffset,
	}
	if stepOffset > 0 {
		summary["continuation"] = true
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_tasks
		SET status = $2,
		    plan = $3,
		    summary = $4,
		    completed_at = COALESCE($5::timestamptz, completed_at)
		WHERE id = $1
	`, task.ID, taskStatus, mustMarshal(plan), mustMarshal(summary), completedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'succeeded',
		    output = $2,
		    provider_call_id = NULLIF($3, '')::uuid,
		    completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(map[string]any{"plan": plan, "summary": summary}), gatewayResp.ProviderCallID); err != nil {
		return err
	}
	return tx.Commit(r.Context())
}

func agentStepErrorCode(decision agent.SupervisionDecision) string {
	if decision.Allowed {
		return ""
	}
	if agentReasonsContain(decision.Reasons, "missing_permission") {
		return "ACCESS_DENIED"
	}
	return "AGENT_STEP_BLOCKED"
}

func agentStepErrorMessage(decision agent.SupervisionDecision) string {
	if decision.Allowed {
		return ""
	}
	if agentReasonsContain(decision.Reasons, "missing_permission") {
		return "missing permission " + decision.Permission
	}
	return strings.Join(decision.Reasons, ", ")
}

func agentReasonsContain(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func agentReasonsWithout(items []string, target string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			out = append(out, item)
		}
	}
	return out
}

func (s *Server) agentStepDryRunOutput(r *http.Request, project Project, toolName string, rawArgs json.RawMessage) map[string]any {
	args, err := agentStepArgs(rawArgs)
	if err != nil {
		return map[string]any{"status": "failed", "errorCode": "VALIDATION_FAILED", "errorMessage": "step input must be a JSON object"}
	}
	switch toolName {
	case "workflow.start":
		workflowType, err := s.agentPlannedWorkflowType(r, project, toolName, args)
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		return map[string]any{
			"status":             "ready",
			"summary":            "将启动受控生产工作流。",
			"workflowType":       workflowType,
			"estimatedCostCents": agentEstimatedProviderCostCents(toolName, args, 0),
			"idempotencyScope":   "agent_task_step",
		}
	case "shot.generate_missing_images", "shot.generate_missing_videos", "shot.cancel_running_videos":
		action := strings.TrimPrefix(toolName, "shot.")
		status, err := s.loadShotProductionStatus(r, project.ID, agentReferenceStringArg(args, "scriptSceneId"), agentReferenceStringArg(args, "workflowRunId"), false)
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		req := ShotProductionActionRequest{
			Action:        action,
			ScriptSceneID: agentReferenceStringArg(args, "scriptSceneId"),
			WorkflowRunID: agentReferenceStringArg(args, "workflowRunId"),
			ShotIDs:       agentReferenceStringSliceArg(args, "shotIds"),
			Options:       agentMapArg(args, "options"),
		}
		targets, code := selectShotProductionTargets(req, status.Shots)
		if code != "" {
			return map[string]any{
				"status":       "blocked",
				"errorCode":    code,
				"errorMessage": shotProductionActionErrorMessage(code),
				"summary": map[string]any{
					"total":          status.Summary.Total,
					"imageSucceeded": status.Summary.ImageSucceeded,
					"videoSucceeded": status.Summary.VideoSucceeded,
					"running":        status.Summary.Running,
				},
			}
		}
		return map[string]any{
			"status":             "ready",
			"summary":            "将启动镜头生产动作。",
			"action":             action,
			"targetShotCount":    len(targets),
			"targetShotIds":      targets,
			"estimatedCostCents": agentEstimatedProviderCostCents(toolName, args, len(targets)),
			"idempotencyScope":   "agent_task_step",
		}
	case "timeline.compose":
		status, err := s.productionStatus(r, project)
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		return map[string]any{
			"status":             "ready",
			"summary":            "将合成最终时间线预览。",
			"shotVideoTotal":     status.Stages.ShotVideos.Total,
			"shotVideoSucceeded": status.Stages.ShotVideos.Succeeded,
			"estimatedCostCents": agentEstimatedProviderCostCents(toolName, args, 0),
			"idempotencyScope":   "agent_task_step",
		}
	case "review.apply_fix":
		fixID := agentReferenceStringArg(args, "fixId")
		if fixID == "" {
			return map[string]any{}
		}
		fix, err := scanReviewFix(s.db.QueryRow(r.Context(), `
			SELECT `+reviewFixColumns()+`
			FROM review_fixes
			WHERE id = $1 AND project_id = $2
		`, fixID, project.ID))
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		return map[string]any{
			"status":            "ready",
			"summary":           "将应用审阅修复。",
			"reviewFixId":       fix.ID,
			"reviewItemId":      fix.ReviewItemID,
			"targetEntityType":  fix.TargetEntityType,
			"targetEntityId":    stringPtrValue(fix.TargetEntityID),
			"fixType":           fix.FixType,
			"title":             fix.Title,
			"explanation":       fix.Explanation,
			"beforeSnapshot":    rawObject(fix.BeforeSnapshot),
			"patch":             rawObject(fix.Patch),
			"afterPreview":      rawObject(fix.AfterPreview),
			"regenerateRequest": rawObject(fix.RegenerateRequest),
		}
	default:
		return map[string]any{}
	}
}

type agentStateGateDecision struct {
	Allowed bool           `json:"allowed"`
	Reason  string         `json:"reason,omitempty"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

func agentStateGateRetryable(decision agentStateGateDecision) bool {
	switch decision.Reason {
	case "workflow_state_unavailable", "shot_status_unavailable", "production_status_unavailable", "review_state_unavailable", "cost_state_unavailable":
		return true
	default:
		return false
	}
}

func agentStateGateNextActions(decision agentStateGateDecision) []agentToolNextAction {
	switch decision.Reason {
	case "agent_kill_switch_enabled":
		return []agentToolNextAction{{Label: "关闭 Agent 全局停止开关后重试", Reason: "当前环境禁止执行非只读 Agent 步骤"}}
	case "video_generation_disabled":
		return []agentToolNextAction{{Label: "允许视频生成或改为只生成图片", Reason: "任务约束禁止视频生成"}}
	case "open_blocking_review_items":
		return []agentToolNextAction{{Label: "查看并处理 high/critical 审阅问题", Tool: "review.list_items", Reason: "生产动作被高危审阅问题阻止", Arguments: map[string]any{"limit": 50}}}
	case "provider_cost_disabled":
		return []agentToolNextAction{{Label: "允许供应商成本或改用只读计划", Reason: "任务约束禁止产生供应商成本"}}
	case "cost_budget_exceeded":
		return []agentToolNextAction{{Label: "提高预算上限或减少目标数量", Reason: "预计成本超过任务预算"}}
	case "workflow_already_running":
		return []agentToolNextAction{{Label: "等待当前同类工作流完成或取消它", Tool: "workflow.read_runs", Reason: "已有同类工作流正在运行"}}
	case "shot_images_not_ready":
		return []agentToolNextAction{{Label: "先生成缺失镜头图片", Tool: "shot.generate_missing_images", Reason: "镜头视频生成依赖图片完成"}}
	case "no_target_shots":
		return []agentToolNextAction{{Label: "刷新镜头生产状态并确认目标镜头", Tool: "shot.status", Reason: "没有符合条件的目标镜头"}}
	case "shot_status_unavailable":
		return []agentToolNextAction{{Label: "刷新镜头生产状态后重试", Tool: "shot.status", Reason: "镜头状态暂时不可用"}}
	case "review_state_unavailable":
		return []agentToolNextAction{{Label: "刷新审阅状态后重试", Tool: "review.list_items", Reason: "审阅状态暂时不可用"}}
	default:
		return agentToolErrorNextActions("", "AGENT_STEP_BLOCKED")
	}
}

func (s *Server) superviseAgentStepState(r *http.Request, project Project, task AgentTask, toolName string, rawArgs json.RawMessage) agentStateGateDecision {
	ok := agentStateGateDecision{Allowed: true}
	fail := func(reason, message string, details map[string]any) agentStateGateDecision {
		return agentStateGateDecision{Allowed: false, Reason: reason, Message: message, Details: details}
	}
	args, err := agentStepArgs(rawArgs)
	if err != nil {
		return fail("invalid_tool_args", "工具参数不是有效 JSON 对象。", nil)
	}
	if task.Mode != string(agent.TaskModePlanOnly) && agentGlobalKillSwitchEnabled() && !agentToolReadOnly(toolName) {
		return fail("agent_kill_switch_enabled", "Agent 全局停止开关已开启，非只读步骤已暂停。", map[string]any{"tool": toolName})
	}
	constraints := rawObject(task.Constraints)
	if !agentConstraintAllowsVideo(constraints) && agentToolMayGenerateVideo(toolName, args) {
		return fail("video_generation_disabled", "当前任务约束禁止生成视频。", nil)
	}
	if agentToolRequiresReviewGate(toolName) {
		openReview, err := s.agentOpenBlockingReviewItems(r, project.ID)
		if err != nil {
			return fail("review_state_unavailable", err.Error(), nil)
		}
		if openReview.Count > 0 {
			return fail("open_blocking_review_items", "项目仍有 high/critical 审阅问题，生产动作已暂停。", map[string]any{
				"openBlockingReviewCount": openReview.Count,
				"criticalCount":           openReview.Critical,
				"highCount":               openReview.High,
			})
		}
	}
	estimatedCostCents := agentEstimatedProviderCostCents(toolName, args, 0)
	if agentToolMaySpendProvider(toolName, args) {
		if !agentConstraintAllowsProviderCost(constraints) {
			return fail("provider_cost_disabled", "当前任务约束禁止产生供应商成本。", map[string]any{"estimatedCostCents": estimatedCostCents})
		}
		if budgetCents, exists := agentConstraintFloat(constraints, "maxProviderCostCents"); exists {
			spentCents, err := s.agentProjectCostSpentCents(r, project.ID)
			if err != nil {
				return fail("cost_state_unavailable", err.Error(), nil)
			}
			if spentCents+estimatedCostCents > budgetCents {
				return fail("cost_budget_exceeded", "当前任务预计成本超过预算约束。", map[string]any{
					"budgetCents":        budgetCents,
					"spentCents":         spentCents,
					"estimatedCostCents": estimatedCostCents,
				})
			}
		}
		ok.Details = mergeAgentStateDetails(ok.Details, map[string]any{"estimatedCostCents": estimatedCostCents})
	}

	workflowType, workflowErr := s.agentPlannedWorkflowType(r, project, toolName, args)
	if workflowErr != nil {
		return fail("invalid_workflow_request", workflowErr.Error(), nil)
	}
	if workflowType != "" {
		states, err := s.loadProductionWorkflowState(r, project.ID)
		if err != nil {
			return fail("workflow_state_unavailable", err.Error(), nil)
		}
		if state := states[workflowType]; state.Running {
			return fail("workflow_already_running", "已有同类工作流正在运行。", map[string]any{
				"workflowType":  workflowType,
				"workflowRunId": state.LatestID,
				"status":        state.LatestStatus,
			})
		}
	}

	switch toolName {
	case "shot.generate_missing_images", "shot.generate_missing_videos", "shot.cancel_running_videos":
		action := strings.TrimPrefix(toolName, "shot.")
		status, err := s.loadShotProductionStatus(r, project.ID, agentReferenceStringArg(args, "scriptSceneId"), agentReferenceStringArg(args, "workflowRunId"), false)
		if err != nil {
			return fail("shot_status_unavailable", err.Error(), nil)
		}
		req := ShotProductionActionRequest{
			Action:        action,
			ScriptSceneID: agentReferenceStringArg(args, "scriptSceneId"),
			WorkflowRunID: agentReferenceStringArg(args, "workflowRunId"),
			ShotIDs:       agentReferenceStringSliceArg(args, "shotIds"),
			Options:       agentMapArg(args, "options"),
		}
		targets, code := selectShotProductionTargets(req, status.Shots)
		if code != "" {
			return fail(strings.ToLower(code), shotProductionActionErrorMessage(code), map[string]any{
				"action": action,
				"summary": map[string]any{
					"total":          status.Summary.Total,
					"imageSucceeded": status.Summary.ImageSucceeded,
					"videoSucceeded": status.Summary.VideoSucceeded,
					"running":        status.Summary.Running,
				},
			})
		}
		estimatedCostCents = agentEstimatedProviderCostCents(toolName, args, len(targets))
		if budgetCents, exists := agentConstraintFloat(constraints, "maxProviderCostCents"); exists {
			spentCents, err := s.agentProjectCostSpentCents(r, project.ID)
			if err != nil {
				return fail("cost_state_unavailable", err.Error(), nil)
			}
			if spentCents+estimatedCostCents > budgetCents {
				return fail("cost_budget_exceeded", "当前任务预计成本超过预算约束。", map[string]any{
					"budgetCents":        budgetCents,
					"spentCents":         spentCents,
					"estimatedCostCents": estimatedCostCents,
					"targetShotCount":    len(targets),
				})
			}
		}
		ok.Details = mergeAgentStateDetails(ok.Details, map[string]any{"targetShotCount": len(targets), "action": action, "estimatedCostCents": estimatedCostCents})
	case "timeline.compose":
		status, err := s.productionStatus(r, project)
		if err != nil {
			return fail("production_status_unavailable", err.Error(), nil)
		}
		if status.Stages.ShotVideos.Total == 0 {
			return fail("shot_videos_not_ready", "当前项目没有可合成的镜头视频。", map[string]any{
				"total":     status.Stages.ShotVideos.Total,
				"succeeded": status.Stages.ShotVideos.Succeeded,
			})
		}
		if status.Stages.ShotVideos.Succeeded < status.Stages.ShotVideos.Total {
			return fail("shot_videos_not_ready", "仍有镜头视频未完成，不能合成时间线。", map[string]any{
				"total":     status.Stages.ShotVideos.Total,
				"succeeded": status.Stages.ShotVideos.Succeeded,
				"running":   status.Stages.ShotVideos.Running,
				"failed":    status.Stages.ShotVideos.Failed,
				"pending":   status.Stages.ShotVideos.Pending,
			})
		}
	}
	return ok
}

func (s *Server) agentProjectCostSpentCents(r *http.Request, projectID string) (float64, error) {
	var spent float64
	err := s.db.QueryRow(r.Context(), `
		SELECT COALESCE(sum(amount), 0)::float8 * 100
		FROM cost_records
		WHERE project_id = $1
	`).Scan(&spent)
	return spent, err
}

type agentBlockingReviewSummary struct {
	Count    int
	Critical int
	High     int
}

func (s *Server) agentOpenBlockingReviewItems(r *http.Request, projectID string) (agentBlockingReviewSummary, error) {
	var summary agentBlockingReviewSummary
	err := s.db.QueryRow(r.Context(), `
		SELECT count(*),
		       count(*) FILTER (WHERE severity = 'critical'),
		       count(*) FILTER (WHERE severity = 'high')
		FROM review_items
		WHERE project_id = $1
		  AND status = 'open'
		  AND severity IN ('critical', 'high')
	`, projectID).Scan(&summary.Count, &summary.Critical, &summary.High)
	return summary, err
}

func (s *Server) agentPlannedWorkflowType(r *http.Request, project Project, toolName string, args map[string]any) (string, error) {
	switch toolName {
	case "workflow.start":
		workflowType := agentStringArg(args, "workflowType")
		if workflowType == "" {
			return "", fmt.Errorf("workflowType is required")
		}
		spec, err := s.agentWorkflowStartSpec(r, project, workflowType, agentMapArg(args, "input"))
		if err != nil {
			return "", err
		}
		return spec.WorkflowType, nil
	case "timeline.compose":
		return "compose_timeline", nil
	case "shot.generate_missing_images", "shot.generate_missing_videos", "shot.cancel_running_videos":
		action := strings.TrimPrefix(toolName, "shot.")
		workflowType, _, ok := shotProductionWorkflowForAction(action)
		if !ok {
			return "", fmt.Errorf("shot production action is not supported")
		}
		return workflowType, nil
	default:
		return "", nil
	}
}

func agentConstraintAllowsVideo(constraints map[string]any) bool {
	value, exists := constraints["allowVideoGeneration"]
	if !exists {
		return true
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return !strings.EqualFold(strings.TrimSpace(typed), "false")
	default:
		return true
	}
}

func agentConstraintAllowsProviderCost(constraints map[string]any) bool {
	value, exists := constraints["allowProviderCost"]
	if !exists {
		return true
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return !strings.EqualFold(strings.TrimSpace(typed), "false")
	default:
		return true
	}
}

func agentPermissionModeForTask(task AgentTask) agent.PermissionMode {
	constraints := rawObject(task.Constraints)
	value := strings.TrimSpace(stringValueFromAny(constraints["permissionMode"]))
	switch agent.PermissionMode(value) {
	case agent.PermissionModeRequireApproval, agent.PermissionModeAutoApprove, agent.PermissionModeFullAccess:
		return agent.PermissionMode(value)
	default:
		if task.Mode == string(agent.TaskModeAutoLowRisk) {
			return agent.PermissionModeAutoApprove
		}
		return agent.PermissionModeRequireApproval
	}
}

func agentGlobalKillSwitchEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(os.Getenv("CINEWEAVE_AGENT_KILL_SWITCH"), os.Getenv("CINEWEAVE_AGENT_DISABLED"))))
	switch value {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func agentToolReadOnly(toolName string) bool {
	switch toolName {
	case "project.read_summary",
		"source.list",
		"source.list_chapters",
		"script.list",
		"script.get",
		"asset.list",
		"storyboard.list",
		"workflow.read_runs",
		"workflow.read_nodes",
		"workflow.read_shots",
		"review.list_items",
		"artifact.list",
		"artifact.preview_url",
		"provider.list_status",
		"shot.status":
		return true
	default:
		return false
	}
}

func agentConstraintFloat(constraints map[string]any, key string) (float64, bool) {
	value, exists := constraints[key]
	if !exists {
		return 0, false
	}
	if typed, ok := value.(string); ok {
		var parsed float64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed, true
		}
		return 0, true
	}
	return float64Value(value), true
}

func agentToolMayGenerateVideo(toolName string, args map[string]any) bool {
	switch toolName {
	case "shot.generate_missing_videos", "timeline.compose":
		return true
	case "workflow.start":
		switch agentStringArg(args, "workflowType") {
		case "video_production", "script_to_video", "full_production", "compose_timeline":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func agentToolRequiresReviewGate(toolName string) bool {
	switch toolName {
	case "workflow.start", "shot.generate_missing_images", "shot.generate_missing_videos", "timeline.compose", "final_video.activate":
		return true
	default:
		return false
	}
}

func agentToolMaySpendProvider(toolName string, args map[string]any) bool {
	switch toolName {
	case "review.run":
		value, exists := agentBoolArg(args, "includeAgent")
		return exists && value
	case "review.generate_fix":
		return strings.EqualFold(agentStringArg(args, "mode"), "agent")
	case "prompt.render_test", "script.rewrite_preview", "script.generate_from_source", "script.rewrite", "provider.test_model":
		return true
	case "workflow.start", "shot.generate_missing_images", "shot.generate_missing_videos", "timeline.compose":
		return true
	default:
		return false
	}
}

func agentEstimatedProviderCostCents(toolName string, args map[string]any, targetCount int) float64 {
	if targetCount < 1 {
		targetCount = 1
	}
	switch toolName {
	case "shot.generate_missing_videos":
		return float64(targetCount) * 50
	case "shot.generate_missing_images":
		return float64(targetCount) * 10
	case "workflow.start":
		switch agentStringArg(args, "workflowType") {
		case "script_to_video", "full_production", "video_production":
			return 100
		case "script_to_assets", "script_to_storyboard":
			return 25
		default:
			return 5
		}
	case "timeline.compose":
		return 15
	case "provider.test_model", "prompt.render_test":
		return 1
	default:
		return 5
	}
}

func mergeAgentStateDetails(current map[string]any, next map[string]any) map[string]any {
	if len(next) == 0 {
		return current
	}
	if current == nil {
		current = map[string]any{}
	}
	for key, value := range next {
		current[key] = value
	}
	return current
}

func (s *Server) listAgentTasks(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	limit := queryInt(r, "limit", 20)
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("filter[sessionId]"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.URL.Query().Get("sessionId"))
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, session_id::text, agent_type, user_goal, mode, status,
		       temporal_workflow_id,
		       constraints, plan, summary, error_code, error_message, created_by::text, created_at,
		       updated_at, started_at, completed_at
		FROM agent_tasks
		WHERE project_id = $1
		  AND ($3 = '' OR session_id::text = $3)
		ORDER BY created_at DESC
		LIMIT $2
	`, project.ID, limit, sessionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]AgentTask, 0)
	for rows.Next() {
		item, err := scanAgentTask(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	meta := map[string]any{"limit": limit}
	if sessionID != "" {
		meta["sessionId"] = sessionID
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, meta)
}

func (s *Server) getAgentTask(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.agentTask(r, project.ID, r.PathValue("taskId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	steps, err := s.listAgentTaskSteps(r, item.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	approvals, err := s.listAgentTaskApprovals(r, item.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item.Steps = steps
	item.Approvals = approvals
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) cancelAgentTask(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowCancel)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	item, err := s.cancelAgentTaskCore(r.Context(), project, r.PathValue("taskId"), strings.TrimSpace(req.Reason))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.signalAgentTaskWorkflow(r.Context(), item, workflows.ProjectAgentCancelTaskSignalName, workflows.ProjectAgentCancelSignal{
		TaskID: item.ID,
		UserID: principal.UserID,
		Reason: strings.TrimSpace(req.Reason),
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) cancelAgentTaskCore(ctx context.Context, project Project, taskID, reason string) (AgentTask, error) {
	r := requestWithContext(ctx)
	item, err := scanAgentTask(s.db.QueryRow(ctx, `
		UPDATE agent_tasks
		SET status = CASE WHEN status IN ('succeeded', 'failed', 'cancelled') THEN status ELSE 'cancelled' END,
		    error_code = CASE WHEN status IN ('succeeded', 'failed', 'cancelled') THEN error_code ELSE 'AGENT_TASK_CANCELLED' END,
		    error_message = CASE WHEN status IN ('succeeded', 'failed', 'cancelled') THEN error_message ELSE NULLIF($3, '') END,
		    completed_at = CASE WHEN status IN ('succeeded', 'failed', 'cancelled') THEN completed_at ELSE now() END
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, session_id::text, agent_type, user_goal, mode, status,
		          temporal_workflow_id,
		          constraints, plan, summary, error_code, error_message, created_by::text, created_at,
		          updated_at, started_at, completed_at
	`, taskID, project.ID, strings.TrimSpace(reason)))
	if err != nil {
		return AgentTask{}, err
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_steps
		SET status = 'cancelled',
		    error_code = COALESCE(error_code, 'AGENT_TASK_CANCELLED'),
		    error_message = COALESCE(error_message, NULLIF($2, '')),
		    completed_at = COALESCE(completed_at, now())
		WHERE task_id = $1 AND status IN ('planned', 'waiting_approval', 'approved', 'running', 'blocked')
	`, item.ID, strings.TrimSpace(reason)); err != nil {
		return AgentTask{}, err
	}
	cancelledWorkflowRunIDs, err := s.cancelAgentTaskWorkflowRuns(r, project.ID, item.ID, strings.TrimSpace(reason))
	if err != nil {
		return AgentTask{}, err
	}
	if len(cancelledWorkflowRunIDs) > 0 {
		if _, err := s.db.Exec(ctx, `
			UPDATE agent_tasks
			SET summary = jsonb_set(COALESCE(summary, '{}'::jsonb), '{cancelledWorkflowRunIds}', $2::jsonb, true)
			WHERE id = $1
		`, item.ID, mustMarshal(cancelledWorkflowRunIDs)); err != nil {
			return AgentTask{}, err
		}
		item, err = s.agentTask(r, project.ID, item.ID)
		if err != nil {
			return AgentTask{}, err
		}
	}
	return item, nil
}

func (s *Server) approveAgentStep(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.decideAgentStepApproval(w, r, principal, "approved")
}

func (s *Server) rejectAgentStep(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	s.decideAgentStepApproval(w, r, principal, "rejected")
}

func (s *Server) resumeAgentTask(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	item, err := scanAgentTask(s.db.QueryRow(r.Context(), `
		UPDATE agent_tasks
		SET status = CASE WHEN status IN ('blocked', 'failed', 'waiting_approval') THEN 'queued' ELSE status END,
		    error_code = CASE WHEN status IN ('blocked', 'failed') THEN NULL ELSE error_code END,
		    error_message = CASE WHEN status IN ('blocked', 'failed') THEN NULL ELSE error_message END,
		    completed_at = CASE WHEN status IN ('failed') THEN NULL ELSE completed_at END
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, session_id::text, agent_type, user_goal, mode, status,
		          temporal_workflow_id,
		          constraints, plan, summary, error_code, error_message, created_by::text, created_at,
		          updated_at, started_at, completed_at
	`, r.PathValue("taskId"), project.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resetResult, err := s.resetBlockedAgentStepsForResume(r.Context(), principal, project, item)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if resetResult.BlockedSteps > 0 || resetResult.PendingApprovals > 0 {
		nextStatus := "waiting_approval"
		errorCode := ""
		errorMessage := ""
		if resetResult.BlockedSteps > 0 {
			nextStatus = "blocked"
			errorCode = "AGENT_STEP_BLOCKED"
			errorMessage = "agent step is blocked"
		}
		if _, err := s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET status = $2,
			    error_code = NULLIF($3, ''),
			    error_message = NULLIF($4, '')
			WHERE id = $1
		`, item.ID, nextStatus, errorCode, errorMessage); err != nil {
			s.writeError(w, r, err)
			return
		}
		item, err = s.agentTaskWithDetails(r, project.ID, item.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, item, nil)
		return
	}
	if s.agentTaskHasTemporal(item) {
		if err := s.signalAgentTaskWorkflow(r.Context(), item, workflows.ProjectAgentResumeTaskSignalName, workflows.ProjectAgentStepDecisionSignal{
			TaskID: item.ID,
			UserID: principal.UserID,
		}); err != nil {
			s.writeError(w, r, err)
			return
		}
		item, err = s.agentTaskWithDetails(r, project.ID, item.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, item, nil)
		return
	}
	item, err = s.executeAgentTaskReadySteps(r, principal, project, item.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

type blockedAgentStepForResume struct {
	ID                 string
	StepIndex          int
	ToolName           string
	Risk               string
	Permission         string
	Input              json.RawMessage
	SupervisorDecision json.RawMessage
}

type agentResumeResetResult struct {
	PendingApprovals int
	BlockedSteps     int
}

func (s *Server) resetBlockedAgentStepsForResume(ctx context.Context, principal auth.Principal, project Project, task AgentTask) (agentResumeResetResult, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, step_index, tool_name, risk, COALESCE(permission, ''), input, supervisor_decision
		FROM agent_steps
		WHERE task_id = $1
		  AND status = 'blocked'
		ORDER BY step_index ASC
	`, task.ID)
	if err != nil {
		return agentResumeResetResult{}, err
	}
	defer rows.Close()
	steps := make([]blockedAgentStepForResume, 0)
	for rows.Next() {
		var step blockedAgentStepForResume
		if err := rows.Scan(&step.ID, &step.StepIndex, &step.ToolName, &step.Risk, &step.Permission, &step.Input, &step.SupervisorDecision); err != nil {
			return agentResumeResetResult{}, err
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return agentResumeResetResult{}, err
	}
	if len(steps) == 0 {
		return agentResumeResetResult{}, nil
	}
	registry, err := s.projectAgentRegistry()
	if err != nil {
		return agentResumeResetResult{}, err
	}
	result := agentResumeResetResult{}
	r := requestWithContext(ctx)
	mode := agent.TaskMode(task.Mode)
	permissionMode := agentPermissionModeForTask(task)
	policy := agent.DefaultSupervisorPolicy()
	for _, step := range steps {
		tool, ok := registry.Get(step.ToolName)
		if !ok {
			tool = agent.AgentTool{
				Name:       step.ToolName,
				Risk:       agent.ToolRisk(step.Risk),
				Permission: step.Permission,
			}
		}
		hasPermission := true
		if strings.TrimSpace(tool.Permission) != "" {
			resource := agentToolPermissionResource(project, tool.Permission)
			hasPermission = s.authorizer.Authorize(ctx, principal, tool.Permission, resource) == nil
		}
		decision := agent.SuperviseTool(policy, agent.SupervisionRequest{
			Tool:              tool,
			Mode:              mode,
			PermissionMode:    permissionMode,
			UserHasPermission: hasPermission,
		})
		stateGate := s.superviseAgentStepState(r, project, task, step.ToolName, step.Input)
		if !stateGate.Allowed {
			decision.Allowed = false
			decision.ExecutionAllowed = false
			decision.RequiresApproval = false
			decision.Reasons = agentReasonsWithout(decision.Reasons, "approval_required")
			decision.Reasons = append(decision.Reasons, stateGate.Reason)
		}
		dryRunOutput := s.agentStepDryRunOutput(r, project, step.ToolName, step.Input)
		if stateGate.Reason != "" || len(stateGate.Details) > 0 {
			dryRunOutput["stateGate"] = stateGate
		}
		nextStatus := "planned"
		if !decision.Allowed {
			nextStatus = "blocked"
			result.BlockedSteps++
		} else if decision.RequiresApproval {
			nextStatus = "waiting_approval"
			result.PendingApprovals++
		}
		if _, err := s.db.Exec(ctx, `
			UPDATE agent_steps
			SET status = $2,
			    requires_approval = $3,
			    dry_run_output = $4,
			    supervisor_decision = $5,
			    output = '{}'::jsonb,
			    verifier_output = '{}'::jsonb,
			    error_code = NULLIF($6, ''),
			    error_message = NULLIF($7, ''),
			    started_at = NULL,
			    completed_at = NULL
			WHERE id = $1
		`, step.ID, nextStatus, decision.RequiresApproval, mustMarshal(dryRunOutput), mustMarshal(map[string]any{
			"decision":       decision,
			"expectedResult": stringValueFromAny(rawObject(step.SupervisorDecision)["expectedResult"]),
			"stateGate":      stateGate,
			"resumed":        true,
		}), agentStepErrorCode(decision), agentStepErrorMessage(decision)); err != nil {
			return agentResumeResetResult{}, err
		}
		if nextStatus == "waiting_approval" {
			if _, err := s.db.Exec(ctx, `
				UPDATE agent_approvals
				SET status = 'cancelled', updated_at = now()
				WHERE task_id = $1 AND step_id = $2 AND status = 'pending'
			`, task.ID, step.ID); err != nil {
				return agentResumeResetResult{}, err
			}
			if _, err := s.db.Exec(ctx, `
				INSERT INTO agent_approvals(task_id, step_id, approval_type, status, requested_payload, decision_payload)
				VALUES ($1, $2, $3, 'pending', $4, '{}'::jsonb)
			`, task.ID, step.ID, string(tool.Risk), mustMarshal(map[string]any{
				"tool":           step.ToolName,
				"risk":           tool.Risk,
				"permission":     tool.Permission,
				"args":           json.RawMessage(step.Input),
				"expectedResult": stringValueFromAny(rawObject(step.SupervisorDecision)["expectedResult"]),
				"decision":       decision,
				"permissionMode": permissionMode,
				"dryRunOutput":   dryRunOutput,
				"resumed":        true,
			})); err != nil {
				return agentResumeResetResult{}, err
			}
		}
	}
	return result, nil
}

func (s *Server) decideAgentStepApproval(w http.ResponseWriter, r *http.Request, principal auth.Principal, decision string) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req agentStepApprovalRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	approval, err := s.decideAgentStepApprovalCore(r.Context(), principal, project, r.PathValue("taskId"), r.PathValue("stepId"), decision, req)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	task, err := s.agentTask(r, project.ID, r.PathValue("taskId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.agentTaskHasTemporal(task) {
		signalName := workflows.ProjectAgentApproveStepSignalName
		if decision == "rejected" {
			signalName = workflows.ProjectAgentRejectStepSignalName
		}
		if err := s.signalAgentTaskWorkflow(r.Context(), task, signalName, workflows.ProjectAgentStepDecisionSignal{
			TaskID:     task.ID,
			StepID:     r.PathValue("stepId"),
			ApprovalID: approval.ID,
			UserID:     principal.UserID,
			Note:       strings.TrimSpace(req.Note),
		}); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, approval, nil)
		return
	}
	if _, err := s.executeAgentTaskReadySteps(r, principal, project, r.PathValue("taskId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, approval, nil)
}

type agentStepApprovalRequest struct {
	ApprovalID string          `json:"approvalId"`
	Note       string          `json:"note"`
	Decision   json.RawMessage `json:"decision"`
}

func (s *Server) decideAgentStepApprovalCore(ctx context.Context, principal auth.Principal, project Project, taskID, stepID, decision string, req agentStepApprovalRequest) (AgentApproval, error) {
	if strings.TrimSpace(stepID) == "" {
		return AgentApproval{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "stepId is required")
	}
	payload := json.RawMessage(`{}`)
	if len(req.Decision) > 0 {
		var value map[string]any
		if err := json.Unmarshal(req.Decision, &value); err != nil {
			return AgentApproval{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "decision must be a JSON object")
		}
		payload = req.Decision
	}
	if strings.TrimSpace(req.Note) != "" {
		payload = mustMarshal(map[string]any{"note": strings.TrimSpace(req.Note), "decision": json.RawMessage(payload)})
	}
	approvalID := strings.TrimSpace(req.ApprovalID)
	if approvalID == "" {
		if err := s.db.QueryRow(ctx, `
			SELECT id::text
			FROM agent_approvals
			WHERE task_id = $1 AND step_id = $2 AND status = 'pending'
			ORDER BY created_at DESC
			LIMIT 1
		`, taskID, stepID).Scan(&approvalID); err != nil {
			if err == pgx.ErrNoRows {
				existing, existingErr := s.agentStepExistingDecision(ctx, project.ID, taskID, stepID, decision)
				if existingErr == nil {
					return existing, nil
				}
			}
			return AgentApproval{}, err
		}
	}
	approval, err := scanAgentApproval(s.db.QueryRow(ctx, `
		UPDATE agent_approvals a
		SET status = $5,
		    decision_payload = $6,
		    decided_by = $7,
		    decided_at = now()
		FROM agent_tasks t
		WHERE a.id = $1 AND a.task_id = $2 AND a.step_id = $3 AND t.id = a.task_id AND t.project_id = $4
		RETURNING a.id, a.task_id, a.step_id::text, a.approval_type, a.status, a.requested_payload,
		          a.decision_payload, a.decided_by::text, a.decided_at, a.expires_at, a.created_at, a.updated_at
	`, approvalID, taskID, stepID, project.ID, decision, payload, principal.UserID))
	if err != nil {
		return AgentApproval{}, err
	}
	nextStepStatus := "approved"
	if decision == "rejected" {
		nextStepStatus = "skipped"
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_steps
		SET status = $3,
		    supervisor_decision = jsonb_set(
		      COALESCE(supervisor_decision, '{}'::jsonb),
		      '{approval}',
		      $4::jsonb,
		      true
		)
		WHERE id = $1 AND task_id = $2 AND status = 'waiting_approval'
	`, stepID, taskID, nextStepStatus, mustMarshal(map[string]any{
		"status":     decision,
		"approvalId": approval.ID,
		"decidedBy":  principal.UserID,
	})); err != nil {
		return AgentApproval{}, err
	}
	return approval, nil
}

func (s *Server) agentStepExistingDecision(ctx context.Context, projectID, taskID, stepID, decision string) (AgentApproval, error) {
	return scanAgentApproval(s.db.QueryRow(ctx, `
		SELECT a.id, a.task_id, a.step_id::text, a.approval_type, a.status, a.requested_payload,
		       a.decision_payload, a.decided_by::text, a.decided_at, a.expires_at, a.created_at, a.updated_at
		FROM agent_approvals a
		JOIN agent_tasks t ON t.id = a.task_id
		WHERE a.task_id = $1
		  AND a.step_id = $2
		  AND a.status = $3
		  AND t.project_id = $4
		ORDER BY a.updated_at DESC
		LIMIT 1
	`, taskID, stepID, decision, projectID))
}

func agentToolPermissionResource(project Project, permission string) authz.Resource {
	switch {
	case strings.HasPrefix(permission, "provider."),
		strings.HasPrefix(permission, "prompt."),
		strings.HasPrefix(permission, "role."),
		strings.HasPrefix(permission, "team."),
		strings.HasPrefix(permission, "admin."),
		strings.HasPrefix(permission, "organization."):
		return authz.Resource{OrganizationID: project.OrganizationID}
	default:
		return authz.Resource{ProjectID: project.ID}
	}
}

func validAgentTaskMode(value string) bool {
	switch agent.TaskMode(value) {
	case agent.TaskModePlanOnly, agent.TaskModeSupervised, agent.TaskModeAutoLowRisk:
		return true
	default:
		return false
	}
}

func validAgentPermissionMode(value string) bool {
	switch agent.PermissionMode(strings.TrimSpace(value)) {
	case agent.PermissionModeRequireApproval, agent.PermissionModeAutoApprove, agent.PermissionModeFullAccess:
		return true
	default:
		return false
	}
}

func (s *Server) agentSessionBelongsToAnyProjectAgent(r *http.Request, projectID, sessionID string) bool {
	var ok bool
	_ = s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM agent_sessions
			WHERE project_id = $1 AND id = $2 AND agent_type IN ('project_agent', 'script_agent', 'asset_agent', 'storyboard_agent', 'shot_asset_agent')
		)
	`, projectID, sessionID).Scan(&ok)
	return ok
}

func (s *Server) agentTask(r *http.Request, projectID, taskID string) (AgentTask, error) {
	return scanAgentTask(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, session_id::text, agent_type, user_goal, mode, status,
		       temporal_workflow_id,
		       constraints, plan, summary, error_code, error_message, created_by::text, created_at,
		       updated_at, started_at, completed_at
		FROM agent_tasks
		WHERE id = $1 AND project_id = $2
	`, taskID, projectID))
}

func (s *Server) agentTaskWithDetails(r *http.Request, projectID, taskID string) (AgentTask, error) {
	item, err := s.agentTask(r, projectID, taskID)
	if err != nil {
		return AgentTask{}, err
	}
	steps, err := s.listAgentTaskSteps(r, item.ID)
	if err != nil {
		return AgentTask{}, err
	}
	approvals, err := s.listAgentTaskApprovals(r, item.ID)
	if err != nil {
		return AgentTask{}, err
	}
	item.Steps = steps
	item.Approvals = approvals
	return item, nil
}

func (s *Server) listAgentTaskSteps(r *http.Request, taskID string) ([]AgentStep, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT id, task_id, step_index, tool_name, risk, permission, status, requires_approval,
		       input, dry_run_output, supervisor_decision, output, verifier_output,
		       error_code, error_message, created_at, updated_at, started_at, completed_at
		FROM agent_steps
		WHERE task_id = $1
		ORDER BY step_index ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AgentStep, 0)
	for rows.Next() {
		item, err := scanAgentStep(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) listAgentTaskApprovals(r *http.Request, taskID string) ([]AgentApproval, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT id, task_id, step_id::text, approval_type, status, requested_payload,
		       decision_payload, decided_by::text, decided_at, expires_at, created_at, updated_at
		FROM agent_approvals
		WHERE task_id = $1
		ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AgentApproval, 0)
	for rows.Next() {
		item, err := scanAgentApproval(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) cancelAgentTaskWorkflowRuns(r *http.Request, projectID, taskID, reason string) ([]string, error) {
	rows, err := s.db.Query(r.Context(), `
		WITH candidates AS (
		  SELECT input->>'workflowRunId' AS workflow_run_id
		  FROM agent_steps
		  WHERE task_id = $1
		  UNION
		  SELECT output->>'workflowRunId' AS workflow_run_id
		  FROM agent_steps
		  WHERE task_id = $1
		  UNION
		  SELECT output #>> '{workflowRun,id}' AS workflow_run_id
		  FROM agent_steps
		  WHERE task_id = $1
		  UNION
		  SELECT jsonb_array_elements_text(
		    CASE
		      WHEN jsonb_typeof(output->'workflowRunIds') = 'array' THEN output->'workflowRunIds'
		      ELSE '[]'::jsonb
		    END
		  ) AS workflow_run_id
		  FROM agent_steps
		  WHERE task_id = $1
		)
		SELECT DISTINCT w.id, w.organization_id, w.project_id, w.template_id, w.temporal_workflow_id,
		       w.status, w.input, w.output, w.error_code, w.error_message, w.created_by,
		       w.created_at, w.started_at, w.completed_at, w.cancelled_at
		FROM candidates c
		JOIN workflow_runs w ON w.id::text = c.workflow_run_id
		WHERE w.project_id = $2
	`, taskID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]WorkflowRun, 0)
	for rows.Next() {
		run, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cancelled := make([]string, 0, len(runs))
	for _, run := range runs {
		if isTerminalWorkflowStatus(run.Status) {
			continue
		}
		updated, err := s.cancelWorkflowRunItem(r.Context(), run, reason)
		if err != nil {
			return nil, err
		}
		cancelled = append(cancelled, updated.ID)
	}
	return cancelled, nil
}

func scanAgentTask(row rowScan) (AgentTask, error) {
	var item AgentTask
	var sessionID, temporalWorkflowID, errorCode, errorMessage, createdBy sql.NullString
	var startedAt, completedAt sql.NullTime
	var constraints, plan, summary []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&sessionID,
		&item.AgentType,
		&item.UserGoal,
		&item.Mode,
		&item.Status,
		&temporalWorkflowID,
		&constraints,
		&plan,
		&summary,
		&errorCode,
		&errorMessage,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&startedAt,
		&completedAt,
	)
	item.SessionID = stringPtrFromNull(sessionID)
	item.TemporalWorkflowID = stringPtrFromNull(temporalWorkflowID)
	item.Constraints = rawOrDefaultBytes(constraints, "{}")
	item.Plan = rawOrDefaultBytes(plan, "{}")
	item.Summary = rawOrDefaultBytes(summary, "{}")
	item.ErrorCode = stringPtrFromNull(errorCode)
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.StartedAt = timePtrFromNull(startedAt)
	item.CompletedAt = timePtrFromNull(completedAt)
	return item, err
}

func scanAgentStep(row rowScan) (AgentStep, error) {
	var item AgentStep
	var permission, errorCode, errorMessage sql.NullString
	var startedAt, completedAt sql.NullTime
	var input, dryRunOutput, supervisorDecision, output, verifierOutput []byte
	err := row.Scan(
		&item.ID,
		&item.TaskID,
		&item.StepIndex,
		&item.ToolName,
		&item.Risk,
		&permission,
		&item.Status,
		&item.RequiresApproval,
		&input,
		&dryRunOutput,
		&supervisorDecision,
		&output,
		&verifierOutput,
		&errorCode,
		&errorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
		&startedAt,
		&completedAt,
	)
	item.Permission = stringPtrFromNull(permission)
	item.Input = rawOrDefaultBytes(input, "{}")
	item.DryRunOutput = rawOrDefaultBytes(dryRunOutput, "{}")
	item.SupervisorDecision = rawOrDefaultBytes(supervisorDecision, "{}")
	item.Output = rawOrDefaultBytes(output, "{}")
	item.VerifierOutput = rawOrDefaultBytes(verifierOutput, "{}")
	item.ErrorCode = stringPtrFromNull(errorCode)
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	item.StartedAt = timePtrFromNull(startedAt)
	item.CompletedAt = timePtrFromNull(completedAt)
	return item, err
}

func scanAgentApproval(row rowScan) (AgentApproval, error) {
	var item AgentApproval
	var stepID, decidedBy sql.NullString
	var decidedAt, expiresAt sql.NullTime
	var requestedPayload, decisionPayload []byte
	err := row.Scan(
		&item.ID,
		&item.TaskID,
		&stepID,
		&item.ApprovalType,
		&item.Status,
		&requestedPayload,
		&decisionPayload,
		&decidedBy,
		&decidedAt,
		&expiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.StepID = stringPtrFromNull(stepID)
	item.RequestedPayload = rawOrDefaultBytes(requestedPayload, "{}")
	item.DecisionPayload = rawOrDefaultBytes(decisionPayload, "{}")
	item.DecidedBy = stringPtrFromNull(decidedBy)
	item.DecidedAt = timePtrFromNull(decidedAt)
	item.ExpiresAt = timePtrFromNull(expiresAt)
	return item, err
}
