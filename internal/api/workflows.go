package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
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
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	WorkflowRunID  *string         `json:"workflowRunId,omitempty"`
	NodeRunID      *string         `json:"nodeRunId,omitempty"`
	Type           string          `json:"type"`
	StorageKey     *string         `json:"storageKey,omitempty"`
	MimeType       *string         `json:"mimeType,omitempty"`
	ContentHash    *string         `json:"contentHash,omitempty"`
	PromptHash     *string         `json:"promptHash,omitempty"`
	ModelID        *string         `json:"modelId,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"createdAt"`
}

func (s *Server) createWorkflowRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		ProjectID      string `json:"projectId"`
		WorkflowType   string `json:"workflowType"`
		Prompt         string `json:"prompt"`
		IdempotencyKey string `json:"idempotencyKey,omitempty"`
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
	if err := s.ensureProjectMember(r, principal.UserID, project.ID); err != nil {
		s.writeError(w, r, err)
		return
	}

	workflowType := strings.TrimSpace(req.WorkflowType)
	if workflowType == "" {
		workflowType = "video_production"
	}
	if workflowType != "video_production" && workflowType != "text_to_storyboard" && workflowType != "script_to_storyboard" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowType is not supported", nil, false)
		return
	}
	idempotency := idempotencyKey(r, req.IdempotencyKey)
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId":    project.ID,
		"workflowType": workflowType,
		"prompt":       strings.TrimSpace(req.Prompt),
	})
	idempotencyState, ok := s.prepareIdempotency(w, r, project.OrganizationID, "workflow-runs:create", idempotency, requestHash)
	if !ok {
		return
	}

	input := json.RawMessage(mustMarshal(map[string]any{"prompt": strings.TrimSpace(req.Prompt), "workflowType": workflowType}))
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
	}
	var workflowFunc any
	switch workflowType {
	case "video_production":
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
	if !s.requireOrganization(w, r, principal, orgID) {
		return
	}
	projectID := r.URL.Query().Get("filter[projectId]")
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
	if err := s.ensureProjectMember(r, principal.UserID, item.ProjectID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
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
	if err := s.ensureProjectMember(r, principal.UserID, run.ProjectID); err != nil {
		s.writeError(w, r, err)
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

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.requireOrganization(w, r, principal, orgID) {
		return
	}
	projectID := r.URL.Query().Get("filter[projectId]")
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
		var item Artifact
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID, &item.NodeRunID, &item.Type, &item.StorageKey, &item.MimeType, &item.ContentHash, &item.PromptHash, &item.ModelID, &item.Metadata, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
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

func mustMarshal(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}
