package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/serviceerror"
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
	registry, err := s.projectAgentRegistry(project)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items := make([]agent.ToolDescriptor, 0)
	providerAdministrationAllowed := true
	providerAdministrationChecked := false
	for _, tool := range registry.List() {
		if isProviderAdministrationAgentTool(tool.Name) {
			if !providerAdministrationChecked {
				providerAdministrationChecked = true
				providerAdministrationAllowed =
					s.requireProviderAdministration(
						r.Context(),
						principal.UserID,
					) == nil
			}
			if !providerAdministrationAllowed {
				continue
			}
		}
		if err := s.authorizeAgentToolPermissions(r.Context(), principal, project, tool); err != nil {
			continue
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	constraints, taskAttachments, err := canonicalizeAgentTaskImageAttachments(
		r.Context(), tx, project, constraints,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := scanAgentTask(tx.QueryRow(r.Context(), `
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
	if err := insertAgentTaskImageAttachmentLinks(
		r.Context(), tx, item.ID, taskAttachments,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.agentTaskTemporalEnabled() {
		if err := s.enqueueAgentTaskWorkflowTx(r.Context(), tx, principal, project, item, projectAgentTemporalWorkflowID(item.ID)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.agentTaskTemporalEnabled() {
		item, err = s.agentTaskWithDetails(r, project.ID, item.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, item, nil)
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

func projectAgentTemporalResumeWorkflowID(taskID string) string {
	return fmt.Sprintf("%s-resume-%d", projectAgentTemporalWorkflowID(taskID), time.Now().UnixNano())
}

func (s *Server) startAgentTaskWorkflow(r *http.Request, principal auth.Principal, project Project, task AgentTask) (AgentTask, error) {
	return s.startAgentTaskWorkflowWithID(r, principal, project, task, projectAgentTemporalWorkflowID(task.ID))
}

func (s *Server) startAgentTaskWorkflowWithID(r *http.Request, principal auth.Principal, project Project, task AgentTask, workflowID string) (AgentTask, error) {
	if s.temporal == nil {
		return AgentTask{}, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "Temporal client is not configured", Retryable: true}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return AgentTask{}, err
	}
	defer tx.Rollback(r.Context())
	if err := s.enqueueAgentTaskWorkflowTx(r.Context(), tx, principal, project, task, workflowID); err != nil {
		return AgentTask{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return AgentTask{}, err
	}
	return s.agentTaskWithDetails(r, project.ID, task.ID)
}

func (s *Server) enqueueAgentTaskWorkflowTx(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, task AgentTask, workflowID string) error {
	if project.ProductionGeneration == nil {
		return videoproduction.NewError(videoproduction.CodeGenerationMismatch, "项目没有活动的视频生产代", false)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_tasks
		SET temporal_workflow_id = $2,
		    summary = jsonb_set(COALESCE(summary, '{}'::jsonb), '{temporalWorkflowId}', to_jsonb($2::text), true),
		    updated_at = now()
		WHERE id = $1
	`, task.ID, workflowID); err != nil {
		return err
	}
	return s.enqueueWorkflowStartTx(
		ctx,
		tx,
		"",
		task.ID,
		project.OrganizationID,
		project.ID,
		project.ProductionGeneration.ID,
		"project_agent",
		"project_agent",
		workflowID,
		workflows.AgentTaskQueue,
		workflows.ProjectAgentWorkflowInput{
			OrganizationID: project.OrganizationID,
			ProjectID:      project.ID,
			TaskID:         task.ID,
			UserID:         principal.UserID,
		},
	)
}

func (s *Server) signalAgentTaskWorkflow(ctx context.Context, task AgentTask, signalName string, payload any) error {
	if !s.agentTaskHasTemporal(task) {
		return nil
	}
	return s.temporal.SignalWorkflow(ctx, strings.TrimSpace(*task.TemporalWorkflowID), "", signalName, payload)
}

func (s *Server) planAgentTask(r *http.Request, principal auth.Principal, project Project, task AgentTask) (AgentTask, error) {
	registry, err := s.projectAgentRegistry(project)
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
		autoPlan = normalizeStoryboardAgentPlan(autoPlan, task.UserGoal)
		plan, err := agent.ValidatePlan(autoPlan, registry, agentRuntimeMaxPlanSteps(task))
		if err == nil {
			err = s.normalizeAndValidateAgentPlanProjectContext(r.Context(), project, task, &plan)
		}
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

	plannerPrompt := prompt
	var lastPlanErr error
	var lastRawPlan string
	var lastParsed agent.Plan
	var lastGatewayResp provider.GatewayTextResponse
	invalidAttempts := make([]map[string]any, 0, agentRuntimeMaxInvalidPlans)
	for attempt := 1; attempt <= agentRuntimeMaxInvalidPlans; attempt++ {
		attemptPromptHash := promptsvc.HashText(plannerPrompt)
		gatewayResp, gatewayErr := provider.NewGatewayClientFromEnv().GenerateText(r.Context(), provider.GatewayTextRequest{
			GatewayBillingIdentity: gatewayBillingIdentityFromContext(
				r.Context(),
				authz.PermissionProjectRead,
				provider.BillingContextReasonAgentAction,
			),
			OrganizationID:    project.OrganizationID,
			WorkspaceID:       project.WorkspaceID,
			ProjectID:         project.ID,
			ModelProfileKey:   project.ScriptModelProfileKey,
			PromptTemplateKey: "project_agent_plan",
			PromptHash:        attemptPromptHash,
			PromptSource:      "inline",
			Input: mustMarshal(map[string]any{
				"prompt":         plannerPrompt,
				"responseFormat": "json",
			}),
			References: agentTaskImageReferences(task),
			Options: provider.GatewayTextOptions{
				IdempotencyKey: fmt.Sprintf("agent-task-plan:%s:%s:%d", task.ID, attemptPromptHash, attempt),
			},
		})
		if gatewayErr != nil {
			_, _ = s.db.Exec(r.Context(), `
				UPDATE agent_runs
				SET status = 'failed', error_code = 'PROVIDER_GATEWAY_ERROR', error_message = $2, completed_at = now()
				WHERE id = $1
			`, runID, gatewayErr.Error())
			_, _ = s.db.Exec(r.Context(), `
				UPDATE agent_tasks
				SET status = 'failed', error_code = 'PROVIDER_GATEWAY_ERROR', error_message = $2, completed_at = now()
				WHERE id = $1
			`, task.ID, gatewayErr.Error())
			return AgentTask{}, gatewayErr
		}
		lastGatewayResp = gatewayResp
		rawPlan := strings.TrimSpace(gatewayResp.Output.Text)
		if rawPlan == "" {
			rawPlan = strings.TrimSpace(string(gatewayResp.Output.Raw))
		}
		lastRawPlan = rawPlan
		parsed, parseErr := agent.ParsePlan(rawPlan)
		if parseErr == nil {
			parsed = normalizeStoryboardAgentPlan(parsed, task.UserGoal)
			lastParsed = parsed
			validated, validateErr := agent.ValidatePlan(parsed, registry, agentRuntimeMaxPlanSteps(task))
			if validateErr == nil {
				validateErr = s.normalizeAndValidateAgentPlanProjectContext(r.Context(), project, task, &validated)
			}
			if validateErr == nil {
				if err := s.persistAgentPlan(r, principal, project, task, registry, validated, runID, gatewayResp); err != nil {
					return AgentTask{}, err
				}
				return s.agentTaskWithDetails(r, project.ID, task.ID)
			}
			parseErr = validateErr
		}
		lastPlanErr = parseErr
		invalidAttempts = append(invalidAttempts, map[string]any{
			"attempt":        attempt,
			"error":          parseErr.Error(),
			"raw":            compactAgentObservationValue(rawPlan, 0),
			"providerCallId": gatewayResp.ProviderCallID,
		})
		if _, err := s.db.Exec(r.Context(), `
			UPDATE agent_tasks
			SET summary = COALESCE(summary, '{}'::jsonb) || $2::jsonb,
			    error_code = NULL,
			    error_message = NULL,
			    completed_at = NULL
			WHERE id = $1
		`, task.ID, mustMarshal(map[string]any{
			"runtimeInvalidPlanCount": attempt,
			"runtimeInvalidPlans":     invalidAttempts,
		})); err != nil {
			return AgentTask{}, err
		}
		plannerPrompt = prompt + "\n\n上一次输出无效，请只修正 JSON 结构、工具名或参数，不要重复解释。\n错误：" +
			parseErr.Error() + "\n无效输出：\n" + stringValueFromAny(compactAgentObservationValue(rawPlan, 0))
	}
	if lastPlanErr == nil {
		lastPlanErr = fmt.Errorf("agent planner returned invalid output")
	}
	_, _ = s.db.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'failed', error_code = 'AGENT_RUNTIME_INVALID_PLAN_LIMIT', error_message = $2,
		    output = $3, provider_call_id = NULLIF($4, '')::uuid, completed_at = now()
		WHERE id = $1
	`, runID, lastPlanErr.Error(), mustMarshal(map[string]any{
		"raw":      lastRawPlan,
		"parsed":   lastParsed,
		"attempts": invalidAttempts,
	}), lastGatewayResp.ProviderCallID)
	_, _ = s.db.Exec(r.Context(), `
		UPDATE agent_tasks
		SET status = 'failed', error_code = 'AGENT_RUNTIME_INVALID_PLAN_LIMIT', error_message = $2,
		    summary = COALESCE(summary, '{}'::jsonb) || $3::jsonb, completed_at = now()
		WHERE id = $1
	`, task.ID, lastPlanErr.Error(), mustMarshal(map[string]any{
		"runtimeInvalidPlanCount": agentRuntimeMaxInvalidPlans,
		"runtimeInvalidPlans":     invalidAttempts,
	}))
	return AgentTask{}, lastPlanErr
}

func normalizeStoryboardAgentPlan(plan agent.Plan, userGoal string) agent.Plan {
	containsStoryboard := false
	for _, step := range plan.Steps {
		if agentPlanWorkflowType(step) == "script_to_storyboard" {
			containsStoryboard = true
			break
		}
	}
	if !containsStoryboard {
		return plan
	}

	explicitSceneParse := containsAnyFold(userGoal, "解析场景", "解析剧本", "剧本分场", "拆分场景", "拆场")
	steps := make([]agent.PlanStep, 0, len(plan.Steps))
	storyboardStepIndex := -1
	allEpisodes := false
	episodeIDs := make([]string, 0)
	seenEpisodeIDs := map[string]bool{}
	for _, step := range plan.Steps {
		workflowType := agentPlanWorkflowType(step)
		if workflowType == "parse_script_scenes" && !explicitSceneParse {
			continue
		}
		if workflowType != "script_to_storyboard" {
			steps = append(steps, step)
			continue
		}
		args := rawObject(step.Args)
		input, _ := args["input"].(map[string]any)
		ids := stringSliceFromAny(input["scriptEpisodeIds"])
		if len(ids) == 0 {
			allEpisodes = true
		}
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" && !seenEpisodeIDs[id] {
				seenEpisodeIDs[id] = true
				episodeIDs = append(episodeIDs, id)
			}
		}
		if storyboardStepIndex < 0 {
			storyboardStepIndex = len(steps)
			steps = append(steps, step)
		}
	}
	if storyboardStepIndex >= 0 {
		args := rawObject(steps[storyboardStepIndex].Args)
		input, _ := args["input"].(map[string]any)
		if input == nil {
			input = map[string]any{}
		}
		if allEpisodes {
			delete(input, "scriptEpisodeIds")
		} else if len(episodeIDs) > 0 {
			input["scriptEpisodeIds"] = episodeIDs
		}
		args["input"] = input
		steps[storyboardStepIndex].Args = mustMarshal(args)
	}
	plan.Steps = steps
	return plan
}

func agentPlanWorkflowType(step agent.PlanStep) string {
	if step.Tool != "workflow.start" {
		return ""
	}
	return strings.TrimSpace(stringValueFromAny(rawObject(step.Args)["workflowType"]))
}

func containsAnyFold(value string, candidates ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func (s *Server) buildAgentPlannerPrompt(r *http.Request, principal auth.Principal, project Project, task AgentTask, registry *agent.Registry) (string, error) {
	policy, err := agent.PolicyForProjectKind(string(project.ProjectKind))
	if err != nil {
		return "", err
	}
	projectContext := map[string]any{
		"id":                        project.ID,
		"name":                      project.Name,
		"projectKind":               project.ProjectKind,
		"contentType":               stringValue(project.ContentType),
		"videoRatio":                project.VideoRatio,
		"artStyle":                  project.ArtStyle,
		"videoProductionProfileKey": project.VideoProductionBinding.ProfileKey,
		"scriptModelProfileKey":     project.ScriptModelProfileKey,
		"imageModelProfileKey":      project.ImageModelProfileKey,
		"videoModelProfileKey":      project.VideoModelProfileKey,
	}
	var production any
	if project.ProjectKind.IsCommerce() {
		commerceContext, contextErr := s.commerceAgentPlannerContext(r.Context(), project)
		if contextErr != nil {
			commerceContext = map[string]any{"error": contextErr.Error()}
		}
		projectContext["commerce"] = commerceContext
	} else if status, statusErr := s.productionStatus(r, project); statusErr == nil {
		production = status
	} else {
		production = map[string]any{"error": statusErr.Error()}
	}
	toolDescriptors := make([]agent.ToolDescriptor, 0)
	for _, tool := range registry.List() {
		if err := s.authorizeAgentToolPermissions(r.Context(), principal, project, tool); err != nil {
			continue
		}
		toolDescriptors = append(toolDescriptors, tool.Descriptor())
	}
	permissionMode := agentPermissionModeForTask(task)
	runtimeSnapshot, runtimeErr := s.loadAgentRuntimeSnapshot(r.Context(), project.ID, task.ID)
	var runtimeContext any = runtimeSnapshot
	if runtimeErr != nil {
		runtimeContext = map[string]any{"error": runtimeErr.Error()}
	}
	toolsJSON := string(mustMarshal(toolDescriptors))
	contextJSON := string(mustMarshal(map[string]any{
		"project":          projectContext,
		"productionStatus": production,
		"constraints":      json.RawMessage(task.Constraints),
		"permissionMode":   permissionMode,
		"agentRuntime":     runtimeContext,
	}))
	var builder strings.Builder
	builder.WriteString("你是 CineWeave Project Agent Planner。你的职责是把用户目标拆成受控工具计划，只输出 JSON。\n")
	builder.WriteString("不要执行工具，不要假装已经完成动作。不要虚构 sourceId、scriptId、workflowRunId、assetId、shotId；缺少 ID 时先安排读取类工具。\n")
	builder.WriteString("当目标缺少关键偏好、存在多个合理路径或你不确定用户真实意图时，先安排 agent.ask_user。它必须包含一个中文 question、2 到 4 个 options，并设置 allowCustom=true，等待用户选择或自定义下一步。\n")
	for _, rule := range policy.PlannerRules {
		builder.WriteString(rule)
		builder.WriteString("\n")
	}
	if project.ProjectKind.IsCommerce() {
		builder.WriteString("按脚本稳定排序处理“第几条脚本”；无法唯一定位时先 commerce.script.list，再用 agent.ask_user 让用户选择。修改脚本时把所选列表项的 revision 传为 expectedRevision。只有需要向用户展示、分析或引用正文时才调用 commerce.script.get。\n")
		builder.WriteString("用户用自然语言要求压缩、润色、改人物、改场景或调整现有广告脚本时，使用 commerce.script.revise；该工具会在后端读取完整正文并自动遵守当前视频模型长度上限，禁止先反复 commerce.script.get 再把截断正文传给 commerce.script.update。只有用户已经给出完整替换正文或精确字段值时才使用 commerce.script.update。\n")
		builder.WriteString("用户明确要求多个场景或其他维度变体时，先用 commerce.script.derive.preview 形成候选，再把最终完整 variations 交给 commerce.script.derive.batch；每个变体只能创建一个独立脚本，不能覆盖源脚本。\n")
		builder.WriteString("用户明确了源脚本、维度、数量和候选时，不增加无意义提问。未给候选但要求生成若干不同候选时，可以由助手提出差异明确且可执行的候选。\n")
		builder.WriteString("生成视频前先用 commerce.video.options 读取当前可执行时长、分辨率和参考图契约。未指定时长时使用可执行时长最大值，未指定参考图时使用商品活动参考图。\n")
		builder.WriteString("commerce.video.generate 和 commerce.script.derive.batch 启动子工作流后，由 Runtime 等待真实终态；不要再规划轮询步骤。\n")
		builder.WriteString("任务约束 attachments 是用户附加的真实图片。usage=unspecified 时必须先用 agent.ask_user 询问图片应作为商品公共参考图、指定脚本自定义参考图或仅供分析；用户确认后使用 commerce.attachment.assign 完成绑定。禁止虚构 attachmentId、artifactId 或 mediaFileId。\n")
	} else {
		builder.WriteString("用户明确要求删除、归档、覆盖、替换或改写已有数据时，可以使用 source.update/source.delete/source.delete_chapter/script.update_episode/script.delete/asset.delete 等写入工具；如果目标对象不明确，必须先读取列表或询问用户。\n")
		builder.WriteString("用户明确要求清空项目中除小说原文外的全部生产内容时，必须使用 project.clear_production_content，并传 confirmation=preserve_novel_sources。该工具会原子切换到新的空白生产代并清理剧本、事件、改编计划、资产、分镜、媒体生产结果和审阅数据；不要用一组只读清单代替实际清理。\n")
		builder.WriteString("删除小说中的单个分集/章节必须使用 source.delete_chapter，禁止用 source.update 读取并覆盖整本小说。可传真实 sourceId/chapterId，也可传 sourceTitle 与 chapterIndex，或 volumeIndex+sectionIndex；未知 ID 必须省略。计划是静态 JSON，严禁写入 <由上一步返回的ID>、{{sourceId}}、<完整正文> 等占位文本。\n")
		builder.WriteString("用户要求按自然语言调整现有资产生图提示词时，优先使用 asset.revise_prompt；资产准确名称已知时可直接传 assetName，不要虚构 assetId。只有用户提供了完整替换值时才使用 asset.update。\n")
		builder.WriteString("资产分析只使用 script_to_assets 且 generateImages=false。需要补全资产卡时使用 asset.batch_generate_prompts，需要生成参考图时使用 asset.batch_generate_images；两者都传明确 assetIds，默认 maxConcurrency=5。禁止再用 script_to_assets(generateImages=true) 串行生成图片。批处理允许部分完成；失败后先读取工作流节点并仅对失败 assetIds 重试。\n")
		builder.WriteString("生成小说剧本时必须按分集处理：script.generate_from_source 必须提供 chapterIds 或 chapterRange；用户说“1-10集/前十节/第一卷第一节到第十节”时写入 chapterRange。每条小说分集按其持久 chapterIndex 对应同序号剧本分集，禁止按本次批次位置从第1集重新编号，也禁止把多个小说分集合并成一个剧本分集。默认追加或更新该来源对应的项目当前剧本；只有用户明确要求另一套剧本时才设置 createNewScript=true，已知目标剧本时传 scriptId。若范围不明确，先用 agent.ask_user 询问。\n")
		builder.WriteString("生成分镜时直接使用 script_to_storyboard。该工作流会读取活动剧本的 script_episodes，按集串行生成并逐集写库；不要在它之前自动插入 parse_script_scenes，也不要为每集规划多个 workflow.start。需要限定集数时把 scriptEpisodeIds 放进 input。\n")
		builder.WriteString("workflow.start 执行器会等待子工作流到达真实终态。启动新工作流后不要再规划 workflow.read_runs 或 workflow.read_nodes 轮询，因为静态计划不能把上一步返回的 workflowRunId 写入后续参数；终态后直接使用对应业务读取工具核对结果。只有用户提供了真实 workflowRunId 时才可规划 workflow.read_nodes。\n")
		builder.WriteString("分镜生成后必须检查 productionStatus.stages.shotAssets。存在 reviewPendingCount 时，先用 shot_asset.list_requirements 检查具体需求，再用 shot_asset.review_requirements 批量校验确认；用户限定某一分集时，两个工具都必须传递该分集的 scriptEpisodeId，禁止把当前生产代的其他分集一并读取或审核。该工具只批准结构化校验通过的需求，失败项会转为 needs_edit 并返回原因。重新审核 needs_edit 时，必须把 list_requirements 返回的 requirementId 明确传给 review_requirements，不能省略 ID 后误用默认 pending 范围。若需求关联了错误资产，先用 asset.list 找到正确资产，再用 shot_asset.update_requirement 修正 assetId、requirementType 或镜头状态字段；确认该需求确实不适用时才使用 shot_asset.skip_requirement。修正后必须重新审核。存在 approvedMissingDerivedImageCount 时，再使用 batch_generate_derived_asset_images 按当前生产代并发补齐镜头衍生资产；用户限定某一分集时，该 workflow 的 input 也必须传递 scriptEpisodeId。不要为了补衍生图重新运行 script_to_storyboard。衍生资产完成后，先用 shot.generate_image_prompts 生成该分集镜头图片提示词，再用 shot.generate_missing_images 生成镜头图片；所有镜头生产工具在限定分集时都必须传 scriptEpisodeId。\n")
		builder.WriteString("视频模型能力不需要人工审批。视频路由只把目标时长和分辨率作为硬条件；任务类型、参考模式、画幅、语言和原生音频能力只用于排序、适配和结果提示。遇到 MODEL_CAPABILITY_UNAVAILABLE 时先读取项目时长、分辨率和当前业务模型绑定，再调整对应配置；不要规划 capability attestation。遇到 RENDER_PLAN_REPLAN_REQUIRED 时使用 shot.generate_video_prompts 重新生成并审核目标镜头提示词，完成后再生成视频。\n")
	}
	builder.WriteString("supervised 模式每轮只能返回一个下一步工具动作。工具执行结果、实体身份和子 Workflow 终态会在下一轮 agentRuntime 观察中提供，禁止一次规划依赖未知 UUID 的后续步骤。\n")
	builder.WriteString("目标已经真正完成时返回：{\"summary\":\"中文完成摘要\",\"complete\":true,\"steps\":[]}。仍需行动时返回：{\"summary\":\"中文摘要\",\"complete\":false,\"steps\":[{\"tool\":\"工具名\",\"args\":{},\"expectedResult\":\"预期结果\"}]}。\n")
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
	return s.persistAgentPlanWithSummaryPatch(r, principal, project, task, registry, plan, runID, gatewayResp, nil)
}

func (s *Server) persistAgentPlanWithSummaryPatch(
	r *http.Request,
	principal auth.Principal,
	project Project,
	task AgentTask,
	registry *agent.Registry,
	plan agent.Plan,
	runID string,
	gatewayResp provider.GatewayTextResponse,
	summaryPatch map[string]any,
) error {
	if err := s.normalizeAndValidateAgentPlanProjectContext(r.Context(), project, task, &plan); err != nil {
		return err
	}
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
	runtimeSnapshot, err := s.loadAgentRuntimeSnapshot(r.Context(), project.ID, task.ID)
	if err != nil {
		return err
	}
	for index, step := range plan.Steps {
		tool, ok := registry.Get(step.Tool)
		if !ok {
			return fmt.Errorf("unknown agent tool %q", step.Tool)
		}
		hasPermission := s.authorizeAgentToolPermissions(
			r.Context(), principal, project, tool,
		) == nil
		decision := agent.SuperviseTool(policy, agent.SupervisionRequest{
			Tool:              tool,
			Mode:              mode,
			PermissionMode:    permissionMode,
			UserHasPermission: hasPermission,
		})
		stateGate := s.superviseAgentStepState(r, project, task, tool, step.Args)
		var repeatedActionCount int
		if err := tx.QueryRow(r.Context(), `
			SELECT count(*)
			FROM agent_steps
			WHERE task_id = $1
			  AND tool_name = $2
			  AND input = $3::jsonb
			  AND COALESCE(supervisor_decision->>'runtimeObservationHash', '') = $4
			  AND status IN ('planned', 'approved', 'waiting_approval', 'running', 'succeeded')
		`, task.ID, step.Tool, step.Args, runtimeSnapshot.ObservationHash).Scan(&repeatedActionCount); err != nil {
			return err
		}
		if runtimeSnapshot.ActionCount >= agentRuntimeMaxActions {
			stateGate = agentStateGateDecision{
				Allowed: false,
				Reason:  "agent_runtime_action_limit",
				Message: "助手已达到单个任务的最大行动数，已停止以避免无限循环。",
				Details: map[string]any{"actionCount": runtimeSnapshot.ActionCount, "maxActions": agentRuntimeMaxActions},
			}
		} else if repeatedActionCount >= agentRuntimeMaxRepeatedAction {
			stateGate = agentStateGateDecision{
				Allowed: false,
				Reason:  "agent_runtime_repeated_action",
				Message: "助手连续重复了没有推进任务的相同操作，已停止以避免无限循环。请恢复任务，让助手改用可推进目标的工具。",
				Details: map[string]any{"tool": step.Tool, "repeatCount": repeatedActionCount, "maxRepeats": agentRuntimeMaxRepeatedAction},
			}
		}
		if !stateGate.Allowed {
			decision.Allowed = false
			decision.ExecutionAllowed = false
			decision.RequiresApproval = false
			decision.Reasons = agentReasonsWithout(decision.Reasons, "approval_required")
			decision.Reasons = append(decision.Reasons, stateGate.Reason)
		}
		if isAgentAskUserTool(step.Tool) && mode != agent.TaskModePlanOnly && decision.Allowed && stateGate.Allowed {
			decision = forceAgentQuestionDecision(decision)
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
			mustMarshal(map[string]any{
				"decision":               decision,
				"expectedResult":         step.ExpectedResult,
				"stateGate":              stateGate,
				"runtimeObservationHash": runtimeSnapshot.ObservationHash,
			}),
			agentStepErrorCode(decision), agentStepErrorMessage(decision)).Scan(&stepID); err != nil {
			return err
		}
		if decision.RequiresApproval {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO agent_approvals(task_id, step_id, approval_type, status, requested_payload, decision_payload)
				VALUES ($1, $2, $3, 'pending', $4, '{}'::jsonb)
			`, task.ID, stepID, agentApprovalTypeForStep(step.Tool, step.Risk), mustMarshal(agentApprovalRequestedPayload(
				step.Tool,
				step.Risk,
				tool.Permission,
				step.Args,
				step.ExpectedResult,
				decision,
				permissionMode,
				dryRunOutput,
				nil,
			))); err != nil {
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
		"summary":                 plan.Summary,
		"plannerProviderCallId":   gatewayResp.ProviderCallID,
		"plannerMode":             plannerMode,
		"modelId":                 gatewayResp.ModelID,
		"permissionMode":          permissionMode,
		"pendingApprovals":        pendingApprovals,
		"blockedSteps":            blockedSteps,
		"appendedAfterStepIndex":  stepOffset,
		"runtimePlannerComplete":  plan.Complete,
		"runtimeActionCount":      runtimeSnapshot.ActionCount,
		"runtimeInvalidPlanCount": 0,
		"runtimeObservationHash":  runtimeSnapshot.ObservationHash,
		"runtimeObservations":     runtimeSnapshot.Observations,
		"runtimeEntityReferences": runtimeSnapshot.EntityReferences,
	}
	if stepOffset > 0 {
		summary["continuation"] = true
	}
	for key, value := range summaryPatch {
		summary[key] = value
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_tasks
		SET status = $2,
		    plan = $3,
		    summary = COALESCE(summary, '{}'::jsonb) || $4,
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
	if strings.HasPrefix(toolName, "commerce.") {
		return s.agentCommerceStepDryRunOutput(r, project, toolName, args)
	}
	switch toolName {
	case agentAskUserToolName:
		question := firstNonEmpty(agentStringArg(args, "question"), "请选择下一步。")
		return map[string]any{
			"status":       "waiting_user",
			"summary":      "助手需要你确认下一步。",
			"question":     question,
			"options":      agentQuestionOptions(args["options"]),
			"allowCustom":  boolValueFromAny(args["allowCustom"]),
			"approvalType": "question",
		}
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
	case "shot.generate_image_prompts", "shot.generate_video_prompts", "shot.generate_missing_images", "shot.generate_missing_videos", "shot.cancel_running_videos":
		action := agentShotProductionAction(strings.TrimPrefix(toolName, "shot."), args)
		req := ShotProductionActionRequest{
			Action:          action,
			ScriptSceneID:   agentReferenceStringArg(args, "scriptSceneId"),
			ScriptEpisodeID: agentReferenceStringArg(args, "scriptEpisodeId"),
			WorkflowRunID:   agentReferenceStringArg(args, "workflowRunId"),
			ShotIDs:         agentReferenceStringSliceArg(args, "shotIds"),
			Options:         agentMapArg(args, "options"),
		}
		scriptSceneID, workflowRunID, scriptEpisodeID := shotProductionScopeFilters(req)
		status, err := s.loadShotProductionStatusForEpisode(r, project.ID, scriptSceneID, workflowRunID, scriptEpisodeID, "", false)
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
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
	case "shot_asset.review_requirements":
		preview, err := s.previewShotAssetRequirementReview(r.Context(), project, args)
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		preview["status"] = "ready"
		preview["summary"] = "将按结构化规则审核当前生产代的镜头资产需求。"
		preview["idempotencyScope"] = "production_generation_requirements"
		return preview
	case "shot_asset.update_requirement", "shot_asset.skip_requirement":
		requirementID := agentReferenceStringArg(args, "requirementId")
		if requirementID == "" {
			return map[string]any{"status": "blocked", "errorCode": "VALIDATION_FAILED", "errorMessage": "缺少镜头资产需求 ID"}
		}
		item, err := scanShotAssetRequirement(s.db.QueryRow(r.Context(), shotAssetRequirementSelectSQL(`
			WHERE r.project_id = $1
			  AND r.id = $2
			  AND r.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		`), project.ID, requirementID))
		if err != nil {
			return map[string]any{"status": "unavailable", "errorMessage": err.Error()}
		}
		if toolName == "shot_asset.update_requirement" && !hasShotAssetRequirementPatch(updateShotAssetRequirementRequestFromPatch(agentMapArg(args, "patch"))) {
			return map[string]any{"status": "blocked", "errorCode": "VALIDATION_FAILED", "errorMessage": "至少需要提供一个镜头资产需求字段"}
		}
		return map[string]any{
			"status":           "ready",
			"summary":          map[bool]string{true: "将跳过该镜头资产需求并保留审计记录。", false: "将修正该镜头资产需求，保存后重新进入审核。"}[toolName == "shot_asset.skip_requirement"],
			"requirementId":    item.ID,
			"storyboardShotId": item.StoryboardShotID,
			"assetId":          item.AssetID,
			"requirementType":  item.RequirementType,
			"patch":            agentMapArg(args, "patch"),
			"reason":           agentStringArg(args, "reason"),
			"idempotencyScope": "shot_asset_requirement",
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
	case "chapter_range_required":
		return []agentToolNextAction{{Label: "指定分集范围后重试", Tool: "agent.ask_user", Reason: "小说剧本生成必须明确分集范围，避免把多个分集合并成一个剧本分集", Arguments: map[string]any{"example": "chapterRange=1-10集"}}}
	case "shot_images_not_ready":
		return []agentToolNextAction{{Label: "先生成缺失镜头图片", Tool: "shot.generate_missing_images", Reason: "镜头视频生成依赖图片完成"}}
	case "no_target_shots":
		return []agentToolNextAction{{Label: "刷新镜头生产状态并确认目标镜头", Tool: "shot.status", Reason: "没有符合条件的目标镜头"}}
	case "shot_status_unavailable":
		return []agentToolNextAction{{Label: "刷新镜头生产状态后重试", Tool: "shot.status", Reason: "镜头状态暂时不可用"}}
	case "review_state_unavailable":
		return []agentToolNextAction{{Label: "刷新审阅状态后重试", Tool: "review.list_items", Reason: "审阅状态暂时不可用"}}
	case "shot_asset_requirement_review_required":
		return []agentToolNextAction{{
			Label:  "校验并确认镜头资产需求",
			Tool:   "shot_asset.review_requirements",
			Reason: "镜头衍生资产只能基于已确认且结构完整的需求生成",
			Arguments: map[string]any{
				"reviewStatus": "approved",
				"note":         "Project Agent 按结构化规则校验镜头资产需求",
			},
		}}
	default:
		return agentToolErrorNextActions("", "AGENT_STEP_BLOCKED")
	}
}

func (s *Server) superviseAgentStepState(r *http.Request, project Project, task AgentTask, tool agent.AgentTool, rawArgs json.RawMessage) agentStateGateDecision {
	ok := agentStateGateDecision{Allowed: true}
	fail := func(reason, message string, details map[string]any) agentStateGateDecision {
		return agentStateGateDecision{Allowed: false, Reason: reason, Message: message, Details: details}
	}
	args, err := agentStepArgs(rawArgs)
	if err != nil {
		return fail("invalid_tool_args", "工具参数不是有效 JSON 对象。", nil)
	}
	toolName := tool.Name
	effects := tool.EffectiveEffects()
	if task.Mode != string(agent.TaskModePlanOnly) && agentGlobalKillSwitchEnabled() && !effects.ReadOnly() {
		return fail("agent_kill_switch_enabled", "Agent 全局停止开关已开启，非只读步骤已暂停。", map[string]any{"tool": toolName})
	}
	constraints := rawObject(task.Constraints)
	if !agentConstraintAllowsVideo(constraints) && agentToolMayGenerateVideo(toolName, args) {
		return fail("video_generation_disabled", "当前任务约束禁止生成视频。", nil)
	}
	if toolName == "script.generate_from_source" {
		if scriptGate := s.superviseScriptGenerateFromSourceStep(r, project, task, args); !scriptGate.Allowed {
			return scriptGate
		}
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
	if effects.MaySpendProvider {
		if !agentConstraintAllowsProviderCost(constraints) {
			return fail("provider_cost_disabled", "当前任务约束禁止产生供应商成本。", map[string]any{"estimatedCostCents": estimatedCostCents})
		}
		if budgetCents, exists := agentConstraintFloat(constraints, "maxProviderCostCents"); exists {
			if estimatedCostCents > budgetCents {
				return fail("cost_budget_exceeded", "当前步骤的技术成本估算超过任务约束。", map[string]any{
					"budgetCents":        budgetCents,
					"estimatedCostCents": estimatedCostCents,
					"authoritative":      false,
				})
			}
		}
		ok.Details = mergeAgentStateDetails(ok.Details, map[string]any{
			"estimatedCostCents": estimatedCostCents,
			"authoritative":      false,
		})
	}

	workflowType, workflowErr := s.agentPlannedWorkflowType(r, project, toolName, args)
	if workflowErr != nil {
		return fail("invalid_workflow_request", workflowErr.Error(), nil)
	}
	if workflowType != "" {
		if project.ProductionGeneration == nil || strings.TrimSpace(project.ProductionGeneration.ID) == "" {
			return fail("workflow_state_unavailable", "项目没有活动的视频生产代。", nil)
		}
		states, err := s.loadProductionWorkflowState(r, project.ID, project.ProductionGeneration.ID)
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
		if workflowType == "batch_generate_derived_asset_images" {
			workflowInput := agentMapArg(args, "input")
			targetCount := len(agentReferenceStringSliceArg(workflowInput, "requirementIds"))
			if targetCount == 0 {
				status, err := s.productionStatus(r, project)
				if err != nil {
					return fail("production_status_unavailable", err.Error(), nil)
				}
				targetCount = status.Stages.ShotAssets.ApprovedMissingDerivedImageCount
				if targetCount == 0 && status.Stages.ShotAssets.ReviewPendingCount > 0 {
					return fail("shot_asset_requirement_review_required", "镜头资产需求尚未完成结构化校验和确认。", map[string]any{
						"reviewPendingCount": status.Stages.ShotAssets.ReviewPendingCount,
						"needsEditCount":     status.Stages.ShotAssets.NeedsEditCount,
					})
				}
			}
			if targetCount == 0 {
				return fail("no_target_derived_assets", "当前生产代没有待生成的镜头衍生资产。", nil)
			}
			estimatedCostCents = float64(targetCount) * 10
			if budgetCents, exists := agentConstraintFloat(constraints, "maxProviderCostCents"); exists {
				if estimatedCostCents > budgetCents {
					return fail("cost_budget_exceeded", "镜头衍生资产的技术成本估算超过当前任务约束。", map[string]any{
						"budgetCents": budgetCents, "estimatedCostCents": estimatedCostCents,
						"targetRequirementCount": targetCount, "authoritative": false,
					})
				}
			}
			ok.Details = mergeAgentStateDetails(ok.Details, map[string]any{
				"targetRequirementCount": targetCount,
				"estimatedCostCents":     estimatedCostCents,
				"authoritative":          false,
			})
		}
	}

	switch toolName {
	case "shot.generate_image_prompts", "shot.generate_video_prompts", "shot.generate_missing_images", "shot.generate_missing_videos", "shot.cancel_running_videos":
		action := agentShotProductionAction(strings.TrimPrefix(toolName, "shot."), args)
		req := ShotProductionActionRequest{
			Action:          action,
			ScriptSceneID:   agentReferenceStringArg(args, "scriptSceneId"),
			ScriptEpisodeID: agentReferenceStringArg(args, "scriptEpisodeId"),
			WorkflowRunID:   agentReferenceStringArg(args, "workflowRunId"),
			ShotIDs:         agentReferenceStringSliceArg(args, "shotIds"),
			Options:         agentMapArg(args, "options"),
		}
		scriptSceneID, workflowRunID, scriptEpisodeID := shotProductionScopeFilters(req)
		status, err := s.loadShotProductionStatusForEpisode(r, project.ID, scriptSceneID, workflowRunID, scriptEpisodeID, "", false)
		if err != nil {
			return fail("shot_status_unavailable", err.Error(), nil)
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
			if estimatedCostCents > budgetCents {
				return fail("cost_budget_exceeded", "当前步骤的技术成本估算超过任务约束。", map[string]any{
					"budgetCents":        budgetCents,
					"estimatedCostCents": estimatedCostCents,
					"targetShotCount":    len(targets),
					"authoritative":      false,
				})
			}
		}
		ok.Details = mergeAgentStateDetails(ok.Details, map[string]any{
			"targetShotCount":    len(targets),
			"action":             action,
			"estimatedCostCents": estimatedCostCents,
			"authoritative":      false,
		})
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

func (s *Server) superviseScriptGenerateFromSourceStep(r *http.Request, project Project, task AgentTask, args map[string]any) agentStateGateDecision {
	fail := func(reason, message string, details map[string]any) agentStateGateDecision {
		return agentStateGateDecision{Allowed: false, Reason: reason, Message: message, Details: details}
	}
	sourceID := agentReferenceStringArg(args, "sourceId")
	if sourceID == "" {
		if err := s.db.QueryRow(r.Context(), `
			SELECT id::text
			FROM project_sources
			WHERE project_id = $1 AND COALESCE(status, 'ready') <> 'archived'
			ORDER BY created_at DESC
			LIMIT 1
		`, project.ID).Scan(&sourceID); err != nil {
			return fail("script_source_unavailable", "未找到可用于生成剧本的来源。", nil)
		}
	}
	source, err := s.projectSource(r, project.ID, sourceID)
	if err != nil {
		return fail("script_source_unavailable", err.Error(), map[string]any{"sourceId": sourceID})
	}
	if source.SourceType != "novel" {
		return agentStateGateDecision{Allowed: true}
	}
	if len(agentReferenceStringSliceArg(args, "chapterIds")) > 0 {
		return agentStateGateDecision{Allowed: true}
	}
	scopeText := strings.Join([]string{
		task.UserGoal,
		agentStringArg(args, "title"),
		agentStringArg(args, "instruction"),
		agentStringArg(args, "chapterRange"),
	}, "\n")
	if _, ok := parseNovelChapterRangeScope(scopeText); ok {
		return agentStateGateDecision{Allowed: true}
	}
	if _, ok := parseNovelChapterScope(scopeText); ok {
		return agentStateGateDecision{Allowed: true}
	}
	chapterCount, err := s.countNovelChapters(r, project.ID, sourceID)
	if err != nil {
		return fail("script_source_unavailable", err.Error(), map[string]any{"sourceId": sourceID})
	}
	if chapterCount <= 1 {
		return agentStateGateDecision{Allowed: true}
	}
	return fail("chapter_range_required", "生成多分集小说剧本必须指定分集范围，不能把多个小说分集合并成一个剧本分集。", map[string]any{
		"sourceId":     sourceID,
		"chapterCount": chapterCount,
		"example":      "chapterRange=1-10集",
	})
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
	case "shot.generate_image_prompts", "shot.generate_video_prompts", "shot.generate_missing_images", "shot.generate_missing_videos", "shot.cancel_running_videos":
		action := agentShotProductionAction(strings.TrimPrefix(toolName, "shot."), args)
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
	effects, found := registeredAgentToolEffects(toolName)
	return found && effects.ReadOnly()
}

func registeredAgentToolEffects(toolName string) (agent.ToolEffects, bool) {
	for _, projectKind := range []string{agent.ProjectKindNarrative, agent.ProjectKindCommerceVideo} {
		policy, err := agent.PolicyForProjectKind(projectKind)
		if err != nil {
			continue
		}
		for _, tool := range policy.Tools() {
			if tool.Name == toolName {
				return tool.EffectiveEffects(), true
			}
		}
	}
	return agent.ToolEffects{}, false
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
	case "shot.generate_missing_videos", "timeline.compose", "commerce.video.generate":
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
	case "workflow.start", "shot.generate_image_prompts", "shot.generate_video_prompts", "shot.generate_missing_images", "shot.generate_missing_videos", "timeline.compose", "final_video.activate":
		return true
	default:
		return false
	}
}

func agentToolMaySpendProvider(toolName string, args map[string]any) bool {
	effects, found := registeredAgentToolEffects(toolName)
	return found && effects.MaySpendProvider
}

func agentEstimatedProviderCostCents(toolName string, args map[string]any, targetCount int) float64 {
	if toolName == "asset.batch_generate_prompts" || toolName == "asset.batch_generate_images" {
		if count := len(agentReferenceStringSliceArg(args, "assetIds")); count > 0 {
			targetCount = count
		}
	}
	if targetCount < 1 {
		targetCount = len(agentReferenceStringSliceArg(args, "shotIds"))
		if targetCount < 1 {
			targetCount = 1
		}
	}
	switch toolName {
	case "asset.batch_generate_prompts":
		return float64(targetCount) * 2
	case "asset.batch_generate_images":
		return float64(targetCount) * 10
	case "shot.generate_missing_videos":
		return float64(targetCount) * 50
	case "shot.generate_image_prompts":
		return float64(targetCount) * 2
	case "shot.generate_video_prompts":
		return float64(targetCount) * 2
	case "shot.generate_missing_images":
		return float64(targetCount) * 10
	case "workflow.start":
		switch agentStringArg(args, "workflowType") {
		case "script_to_video", "full_production", "video_production":
			return 100
		case "script_to_assets", "script_to_storyboard":
			return 25
		case "batch_generate_derived_asset_images":
			return 10
		default:
			return 5
		}
	case "timeline.compose":
		return 15
	case "commerce.script.derive.preview":
		return 2
	case "commerce.script.revise":
		return 2
	case "commerce.script.derive.batch", "commerce.script.derive.retry_failed":
		variations, _ := args["variations"].([]any)
		variationCount := len(variations)
		if variationCount < 1 {
			variationCount = targetCount
		}
		return float64(variationCount) * 6
	case "commerce.video.generate":
		return 50
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
	item, err := s.agentTaskWithDetails(r, project.ID, r.PathValue("taskId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
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
	}); err != nil && !isCompletedAgentWorkflowSignalError(err) {
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
	if item.Status == "succeeded" {
		if _, pending, pendingErr := s.pendingAgentQuestionContinuation(r.Context(), item.ID); pendingErr != nil {
			s.writeError(w, r, pendingErr)
			return
		} else if pending {
			if _, err := s.db.Exec(r.Context(), `
				UPDATE agent_tasks
				SET status = 'queued', completed_at = NULL, error_code = NULL, error_message = NULL
				WHERE id = $1 AND project_id = $2
			`, item.ID, project.ID); err != nil {
				s.writeError(w, r, err)
				return
			}
			item, err = s.agentTask(r, project.ID, item.ID)
			if err != nil {
				s.writeError(w, r, err)
				return
			}
		}
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
			if !isCompletedAgentWorkflowSignalError(err) {
				s.writeError(w, r, err)
				return
			}
			item, err = s.startAgentTaskWorkflowWithID(r, principal, project, item, projectAgentTemporalResumeWorkflowID(item.ID))
			if err != nil {
				s.writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, r, http.StatusOK, item, nil)
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

func isCompletedAgentWorkflowSignalError(err error) bool {
	if err == nil {
		return false
	}
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "workflow execution already completed") ||
		strings.Contains(message, "workflow not found")
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
		  AND status IN ('blocked', 'failed')
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
	registry, err := s.projectAgentRegistry(project)
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
		hasPermission := s.authorizeAgentToolPermissions(ctx, principal, project, tool) == nil
		decision := agent.SuperviseTool(policy, agent.SupervisionRequest{
			Tool:              tool,
			Mode:              mode,
			PermissionMode:    permissionMode,
			UserHasPermission: hasPermission,
		})
		stateGate := s.superviseAgentStepState(r, project, task, tool, step.Input)
		if !stateGate.Allowed {
			decision.Allowed = false
			decision.ExecutionAllowed = false
			decision.RequiresApproval = false
			decision.Reasons = agentReasonsWithout(decision.Reasons, "approval_required")
			decision.Reasons = append(decision.Reasons, stateGate.Reason)
		}
		if isAgentAskUserTool(step.ToolName) && mode != agent.TaskModePlanOnly && decision.Allowed && stateGate.Allowed {
			decision = forceAgentQuestionDecision(decision)
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
			`, task.ID, step.ID, agentApprovalTypeForStep(step.ToolName, tool.Risk), mustMarshal(agentApprovalRequestedPayload(
				step.ToolName,
				tool.Risk,
				tool.Permission,
				step.Input,
				stringValueFromAny(rawObject(step.SupervisorDecision)["expectedResult"]),
				decision,
				permissionMode,
				dryRunOutput,
				map[string]any{"resumed": true},
			))); err != nil {
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
			Decision:   req.Decision,
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
		"status":          decision,
		"approvalId":      approval.ID,
		"decidedBy":       principal.UserID,
		"decisionPayload": json.RawMessage(payload),
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
	if item.Status == "succeeded" && agentTaskSummaryStillWaiting(item.Summary) {
		if err := s.mergeAgentTaskCompletionSummary(r.Context(), projectID, taskID); err != nil {
			return AgentTask{}, err
		}
		item, err = s.agentTask(r, projectID, taskID)
		if err != nil {
			return AgentTask{}, err
		}
	}
	steps, err := s.listAgentTaskSteps(r, item.ID)
	if err != nil {
		return AgentTask{}, err
	}
	steps, err = s.withAgentWorkflowProgress(r, item.OrganizationID, projectID, steps)
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

func agentTaskSummaryStillWaiting(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var summary struct {
		Text                   string            `json:"summary"`
		WaitingForWorkflowRuns []json.RawMessage `json:"waitingForWorkflowRuns"`
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return false
	}
	return len(summary.WaitingForWorkflowRuns) > 0 || strings.Contains(summary.Text, "正在等待")
}

func (s *Server) withAgentWorkflowProgress(
	r *http.Request,
	organizationID string,
	projectID string,
	steps []AgentStep,
) ([]AgentStep, error) {
	ctx := r.Context()
	for index := range steps {
		output := rawObject(steps[index].Output)
		data, _ := output["data"].(map[string]any)
		if data == nil {
			data = map[string]any{}
		}
		if batchID := agentStepScriptDerivationBatchID(steps[index].ToolName, data); batchID != "" && s.commerceDerivations != nil {
			batch, err := s.commerceDerivations.GetBatch(
				ctx, s.db, organizationID, projectID, batchID, true,
			)
			if err != nil {
				var commerceErr commercepkg.Error
				if !errors.As(err, &commerceErr) ||
					commerceErr.Code != commercepkg.CodeScriptDerivationNotFound {
					return nil, err
				}
			} else {
				data["scriptDerivation"] = batch
			}
		}
		if jobID := agentStepCommerceDirectVideoJobID(steps[index].ToolName, data); jobID != "" && s.commerceDirect != nil {
			job, err := s.commerceDirect.GetJob(ctx, s.db, organizationID, projectID, jobID)
			if err != nil {
				var commerceErr commercepkg.Error
				if !errors.As(err, &commerceErr) ||
					commerceErr.Code != commercepkg.CodeDirectVideoNotFound {
					return nil, err
				}
			} else {
				items := []commercepkg.DirectVideoJob{job}
				s.attachCommerceDirectVideoPreviews(r, items)
				data["directVideo"] = items[0]
			}
		}
		workflowRunIDs := agentStepChildWorkflowRunIDs(steps[index].ToolName, steps[index].Output)
		progressItems := make([]map[string]any, 0, len(workflowRunIDs))
		for _, workflowRunID := range workflowRunIDs {
			progress, err := s.agentWorkflowProgress(ctx, projectID, workflowRunID)
			if err == pgx.ErrNoRows {
				continue
			}
			if err != nil {
				return nil, err
			}
			progressItems = append(progressItems, progress)
		}
		if len(progressItems) > 0 {
			data["workflowProgress"] = progressItems[0]
			if len(progressItems) > 1 {
				data["workflowProgressItems"] = progressItems
			}
		}
		if len(data) > 0 {
			output["data"] = data
			steps[index].Output = mustMarshal(output)
		}
	}
	return steps, nil
}

func agentStepScriptDerivationBatchID(toolName string, data map[string]any) string {
	switch toolName {
	case "commerce.script.derive.batch",
		"commerce.script.derivation.get",
		"commerce.script.derive.retry_failed",
		"commerce.script.derive.cancel":
	default:
		return ""
	}
	for _, key := range []string{"id", "batchId"} {
		value := strings.TrimSpace(stringValueFromAny(data[key]))
		if _, err := uuid.Parse(value); err == nil {
			return value
		}
	}
	return ""
}

func agentStepCommerceDirectVideoJobID(toolName string, data map[string]any) string {
	switch toolName {
	case "commerce.video.generate", "commerce.video.get", "commerce.video.cancel":
	default:
		return ""
	}
	for _, key := range []string{"id", "jobId"} {
		value := strings.TrimSpace(stringValueFromAny(data[key]))
		if _, err := uuid.Parse(value); err == nil {
			return value
		}
	}
	return ""
}

func (s *Server) agentWorkflowProgress(ctx context.Context, projectID, workflowRunID string) (map[string]any, error) {
	var (
		workflowType     string
		workflowStatus   string
		totalItems       int
		completedItems   int
		failedItems      int
		totalNodes       int
		completedNodes   int
		nodeID           sql.NullString
		nodeKey          sql.NullString
		nodeType         sql.NullString
		nodeStatus       sql.NullString
		nodeInput        []byte
		nodeOutput       []byte
		nodeErrorCode    sql.NullString
		nodeErrorMessage sql.NullString
	)
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(NULLIF(w.input->>'workflowType', ''), NULLIF(w.workflow_type, ''), ''),
			w.status,
			COALESCE(w.total_items, 0),
			COALESCE(w.completed_items, 0),
			COALESCE(w.failed_items, 0),
			(SELECT count(*) FROM workflow_node_runs n WHERE n.workflow_run_id = w.id),
			(SELECT count(*) FROM workflow_node_runs n WHERE n.workflow_run_id = w.id AND n.status IN ('succeeded', 'failed', 'cancelled')),
			n.id::text,
			n.node_key,
			n.node_type,
			n.status,
			COALESCE(n.input, '{}'::jsonb),
			COALESCE(n.output, '{}'::jsonb),
			n.error_code,
			n.error_message
		FROM workflow_runs w
		LEFT JOIN LATERAL (
			SELECT *
			FROM workflow_node_runs candidate
			WHERE candidate.workflow_run_id = w.id
			ORDER BY CASE WHEN candidate.status IN ('queued', 'running') THEN 0 ELSE 1 END,
			         candidate.created_at DESC
			LIMIT 1
		) n ON true
		WHERE w.project_id = $1 AND w.id = $2
	`, projectID, workflowRunID).Scan(
		&workflowType,
		&workflowStatus,
		&totalItems,
		&completedItems,
		&failedItems,
		&totalNodes,
		&completedNodes,
		&nodeID,
		&nodeKey,
		&nodeType,
		&nodeStatus,
		&nodeInput,
		&nodeOutput,
		&nodeErrorCode,
		&nodeErrorMessage,
	)
	if err != nil {
		return nil, err
	}
	progress := map[string]any{
		"workflowRunId":  workflowRunID,
		"workflowType":   workflowType,
		"status":         workflowStatus,
		"totalItems":     totalItems,
		"completedItems": completedItems,
		"failedItems":    failedItems,
		"totalNodes":     totalNodes,
		"completedNodes": completedNodes,
	}
	if nodeID.Valid {
		progress["activeNode"] = map[string]any{
			"id":           nodeID.String,
			"nodeKey":      nodeKey.String,
			"nodeType":     nodeType.String,
			"status":       nodeStatus.String,
			"input":        rawObject(json.RawMessage(nodeInput)),
			"output":       rawObject(json.RawMessage(nodeOutput)),
			"errorCode":    nodeErrorCode.String,
			"errorMessage": nodeErrorMessage.String,
		}
	}
	return progress, nil
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
	workflowRunIDs, err := s.agentTaskWorkflowRunIDs(r.Context(), taskID)
	if err != nil || len(workflowRunIDs) == 0 {
		return nil, err
	}
	rows, err := s.db.Query(r.Context(), workflowRunSelectSQL(`
		WHERE project_id = $1 AND id::text = ANY($2::text[])
	`), projectID, workflowRunIDs)
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
