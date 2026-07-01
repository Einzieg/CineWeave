package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/client"
)

type WorkflowRun struct {
	ID                 string          `json:"id"`
	OrganizationID     string          `json:"organizationId"`
	ProjectID          string          `json:"projectId"`
	TemplateID         *string         `json:"templateId,omitempty"`
	TemporalWorkflowID string          `json:"temporalWorkflowId"`
	Status             string          `json:"status"`
	Input              json.RawMessage `json:"input"`
	Output             json.RawMessage `json:"output"`
	ErrorCode          *string         `json:"errorCode,omitempty"`
	ErrorMessage       *string         `json:"errorMessage,omitempty"`
	CreatedBy          string          `json:"createdBy"`
	CreatedAt          time.Time       `json:"createdAt"`
	StartedAt          *time.Time      `json:"startedAt,omitempty"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
	CancelledAt        *time.Time      `json:"cancelledAt,omitempty"`
}

type WorkflowNodeRun struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	WorkflowRunID  string          `json:"workflowRunId"`
	NodeKey        string          `json:"nodeKey"`
	NodeType       string          `json:"nodeType"`
	Status         string          `json:"status"`
	Input          json.RawMessage `json:"input"`
	Output         json.RawMessage `json:"output"`
	RetryCount     int             `json:"retryCount"`
	ErrorCode      *string         `json:"errorCode,omitempty"`
	ErrorMessage   *string         `json:"errorMessage,omitempty"`
	StartedAt      *time.Time      `json:"startedAt,omitempty"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type Artifact struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        *string         `json:"projectId,omitempty"`
	WorkflowRunID    *string         `json:"workflowRunId,omitempty"`
	NodeRunID        *string         `json:"nodeRunId,omitempty"`
	Type             string          `json:"type"`
	StorageKey       *string         `json:"storageKey,omitempty"`
	MimeType         *string         `json:"mimeType,omitempty"`
	ContentHash      *string         `json:"contentHash,omitempty"`
	PromptHash       *string         `json:"promptHash,omitempty"`
	ModelID          *string         `json:"modelId,omitempty"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        time.Time       `json:"createdAt"`
	PreviewURL       *string         `json:"previewUrl,omitempty"`
	PreviewExpiresAt *time.Time      `json:"previewExpiresAt,omitempty"`
}

func (s *Server) createWorkflowRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		ProjectID      string          `json:"projectId"`
		WorkflowType   string          `json:"workflowType"`
		Prompt         string          `json:"prompt"`
		Input          json.RawMessage `json:"input,omitempty"`
		IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "projectId is required", nil, false)
		return
	}
	project, err := s.project(r, req.ProjectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowRun, authz.Resource{ProjectID: project.ID}) {
		return
	}

	workflowType := strings.TrimSpace(req.WorkflowType)
	if workflowType == "" {
		workflowType = "video_production"
	}
	if workflowType != "video_production" && workflowType != "text_to_storyboard" && workflowType != "extract_novel_events" && workflowType != "generate_adaptation_plan" && workflowType != "adaptation_plan_to_script" && workflowType != "source_to_script" && workflowType != "parse_script_scenes" && workflowType != "script_to_assets" && workflowType != "script_to_storyboard" && workflowType != "script_to_video" && workflowType != "full_production" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowType is not supported", nil, false)
		return
	}
	workflowRequestInput, err := normalizeWorkflowRequestInput(workflowType, req.Input, projectDefaultAspectRatio(project))
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
		return
	}
	idempotency := idempotencyKey(r, req.IdempotencyKey)
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId":    project.ID,
		"workflowType": workflowType,
		"prompt":       strings.TrimSpace(req.Prompt),
		"input":        string(workflowRequestInput),
	})
	idempotencyState, ok := s.prepareIdempotency(w, r, project.OrganizationID, "workflow-runs:create", idempotency, requestHash)
	if !ok {
		return
	}

	input := json.RawMessage(mustMarshal(map[string]any{"prompt": strings.TrimSpace(req.Prompt), "workflowType": workflowType, "input": workflowRequestInput}))
	var run WorkflowRun
	err = s.db.QueryRow(r.Context(), `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO workflow_runs(id, organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		SELECT id, $1, $2, 'workflow-' || id::text, 'queued', $3, '{}', $4
		FROM new_run
		RETURNING id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
	`, project.OrganizationID, project.ID, input, principal.UserID).Scan(
		&run.ID,
		&run.OrganizationID,
		&run.ProjectID,
		&run.TemplateID,
		&run.TemporalWorkflowID,
		&run.Status,
		&run.Input,
		&run.Output,
		&run.ErrorCode,
		&run.ErrorMessage,
		&run.CreatedBy,
		&run.CreatedAt,
		&run.StartedAt,
		&run.CompletedAt,
		&run.CancelledAt,
	)
	if err != nil {
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	workflowInput := workflows.TextToStoryboardInput{
		OrganizationID: run.OrganizationID,
		ProjectID:      run.ProjectID,
		WorkflowRunID:  run.ID,
		Prompt:         strings.TrimSpace(req.Prompt),
		CreatedBy:      principal.UserID,
		Input:          workflowRequestInput,
	}
	var workflowFunc any
	switch workflowType {
	case "extract_novel_events":
		workflowFunc = workflows.ExtractNovelEventsWorkflow
	case "generate_adaptation_plan":
		workflowFunc = workflows.GenerateAdaptationPlanWorkflow
	case "adaptation_plan_to_script":
		workflowFunc = workflows.AdaptationPlanToScriptWorkflow
	case "source_to_script":
		workflowFunc = workflows.SourceToScriptWorkflow
	case "parse_script_scenes":
		workflowFunc = workflows.ParseScriptScenesWorkflow
	case "script_to_assets":
		workflowFunc = workflows.ScriptToAssetsWorkflow
	case "video_production":
		workflowFunc = workflows.VideoProductionWorkflow
	case "script_to_video", "full_production":
		workflowFunc = workflows.VideoProductionWorkflow
	case "script_to_storyboard":
		workflowFunc = workflows.ScriptToStoryboardWorkflow
	default:
		workflowFunc = workflows.TextToStoryboardWorkflow
	}
	_, err = s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        run.TemporalWorkflowID,
		TaskQueue: workflows.ScriptTaskQueue,
	}, workflowFunc, workflowInput)
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE workflow_runs
			SET status = 'failed', error_code = 'TEMPORAL_START_FAILED', error_message = $2, completed_at = now()
			WHERE id = $1
		`, run.ID, err.Error())
		s.failIdempotency(r.Context(), idempotencyState)
		s.writeError(w, r, err)
		return
	}
	if err := s.completeIdempotency(r.Context(), idempotencyState, run); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, run, nil)
}

func (s *Server) listWorkflowRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	projectID := r.URL.Query().Get("filter[projectId]")
	if projectID != "" {
		if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: projectID}) {
			return
		}
	} else if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE organization_id = $1
		  AND ($2 = '' OR project_id = $2::uuid)
		ORDER BY created_at DESC
		LIMIT 100
	`, orgID, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]WorkflowRun, 0)
	for rows.Next() {
		item, err := scanWorkflowRun(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getWorkflowRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := scanWorkflowRun(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE id = $1
	`, r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: item.ProjectID}) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) cancelWorkflowRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	item, err := scanWorkflowRun(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE id = $1
	`, r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowCancel, authz.Resource{ProjectID: item.ProjectID}) {
		return
	}
	if isTerminalWorkflowStatus(item.Status) {
		httpx.WriteJSON(w, r, http.StatusOK, item, nil)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "User requested cancellation"
	}
	if err := workflows.MarkWorkflowCancelling(r.Context(), s.db, item.ID, reason); err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.temporal != nil {
		if err := s.temporal.CancelWorkflow(r.Context(), item.TemporalWorkflowID, ""); err != nil {
			_ = s.insertWorkflowCancelWarning(r.Context(), item, reason, err)
		}
	}
	updated, err := scanWorkflowRun(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE id = $1
	`, item.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func (s *Server) listWorkflowNodeRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	run, err := scanWorkflowRun(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE id = $1
	`, r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: run.ProjectID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id, node_key, node_type, status, input, output, retry_count, error_code, error_message, started_at, completed_at, created_at
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		ORDER BY created_at ASC
	`, run.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]WorkflowNodeRun, 0)
	for rows.Next() {
		var item WorkflowNodeRun
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID, &item.NodeKey, &item.NodeType, &item.Status, &item.Input, &item.Output, &item.RetryCount, &item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.CompletedAt, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func isTerminalWorkflowStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled"
}

func (s *Server) insertWorkflowCancelWarning(ctx context.Context, run WorkflowRun, reason string, cause error) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, 'workflow.run.cancel_warning', 'workflow_run', $3, $4)
	`, run.OrganizationID, run.ProjectID, run.ID, mustMarshal(map[string]any{
		"workflowRunId": run.ID,
		"reason":        reason,
		"message":       cause.Error(),
	}))
	return err
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	projectID := r.URL.Query().Get("filter[projectId]")
	if projectID != "" {
		if !s.authorize(w, r, principal, authz.PermissionArtifactRead, authz.Resource{ProjectID: projectID}) {
			return
		}
	} else if !s.authorize(w, r, principal, authz.PermissionArtifactRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	includePreviewURL := strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true")
	previewExpires := previewURLExpiryFromRequest(r)
	if includePreviewURL && s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_at
		FROM artifacts
		WHERE organization_id = $1
		  AND ($2 = '' OR project_id = $2::uuid)
		ORDER BY created_at DESC
		LIMIT 100
	`, orgID, projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]Artifact, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if includePreviewURL && artifactCanPreview(item) && item.StorageKey != nil && strings.TrimSpace(*item.StorageKey) != "" {
			presigned, err := s.storage.PresignGetObject(r.Context(), *item.StorageKey, previewExpires)
			if err != nil {
				s.writeError(w, r, err)
				return
			}
			item.PreviewURL = &presigned.URL
			item.PreviewExpiresAt = &presigned.ExpiresAt
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func scanWorkflowRun(row pgx.Row) (WorkflowRun, error) {
	var item WorkflowRun
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.TemplateID,
		&item.TemporalWorkflowID,
		&item.Status,
		&item.Input,
		&item.Output,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.StartedAt,
		&item.CompletedAt,
		&item.CancelledAt,
	)
	return item, err
}

func normalizeWorkflowRequestInput(workflowType string, raw json.RawMessage, projectAspectRatio *string) (json.RawMessage, error) {
	values := map[string]any{}
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
	}
	if workflowType == "video_production" {
		if _, ok := values["duration"]; !ok {
			values["duration"] = 5
		}
		if value, ok := values["aspectRatio"].(string); !ok || strings.TrimSpace(value) == "" {
			aspectRatio := "16:9"
			if projectAspectRatio != nil && strings.TrimSpace(*projectAspectRatio) != "" {
				aspectRatio = strings.TrimSpace(*projectAspectRatio)
			}
			values["aspectRatio"] = aspectRatio
		}
		if value, ok := values["resolution"].(string); !ok || strings.TrimSpace(value) == "" {
			values["resolution"] = "720p"
		}
		if _, ok := values["pollIntervalSeconds"]; !ok {
			values["pollIntervalSeconds"] = 5
		}
		if _, ok := values["maxPolls"]; !ok {
			values["maxPolls"] = 120
		}
		if value, ok := values["maxShots"].(float64); !ok || value <= 0 || value > 3 {
			values["maxShots"] = 3
		}
		if _, ok := values["skipCompose"].(bool); !ok {
			values["skipCompose"] = false
		}
	}
	if workflowType == "source_to_script" || workflowType == "extract_novel_events" || workflowType == "generate_adaptation_plan" {
		if value, ok := values["sourceId"].(string); !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("input.sourceId is required")
		}
	}
	if workflowType == "adaptation_plan_to_script" {
		if value, ok := values["planId"].(string); !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("input.planId is required")
		}
	}
	if workflowType == "parse_script_scenes" {
		if value, ok := values["scriptId"].(string); !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("input.scriptId is required")
		}
	}
	if workflowType == "script_to_assets" || workflowType == "script_to_storyboard" || workflowType == "script_to_video" || workflowType == "full_production" {
		if value, ok := values["maxShots"].(float64); ok && (value <= 0 || value > 3) {
			values["maxShots"] = 3
		}
	}
	return json.RawMessage(mustMarshal(values)), nil
}

func projectDefaultAspectRatio(project Project) *string {
	if project.AspectRatio != nil && strings.TrimSpace(*project.AspectRatio) != "" {
		return project.AspectRatio
	}
	if strings.TrimSpace(project.VideoRatio) != "" {
		value := strings.TrimSpace(project.VideoRatio)
		return &value
	}
	return nil
}

func mustMarshal(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}
