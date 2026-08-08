package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/serviceerror"
)

type WorkflowRun struct {
	ID                             string          `json:"id"`
	OrganizationID                 string          `json:"organizationId"`
	ProjectID                      string          `json:"projectId"`
	ProductionGenerationID         string          `json:"productionGenerationId"`
	VideoProductionBindingID       string          `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64           `json:"videoProductionBindingRevision"`
	TemplateID                     *string         `json:"templateId,omitempty"`
	TemporalWorkflowID             string          `json:"temporalWorkflowId"`
	Status                         string          `json:"status"`
	Input                          json.RawMessage `json:"input"`
	Output                         json.RawMessage `json:"output"`
	ErrorCode                      *string         `json:"errorCode,omitempty"`
	ErrorMessage                   *string         `json:"errorMessage,omitempty"`
	CreatedBy                      string          `json:"createdBy"`
	CreatedAt                      time.Time       `json:"createdAt"`
	StartedAt                      *time.Time      `json:"startedAt,omitempty"`
	CompletedAt                    *time.Time      `json:"completedAt,omitempty"`
	CancelledAt                    *time.Time      `json:"cancelledAt,omitempty"`
	WorkflowType                   string          `json:"workflowType"`
	TotalItems                     int             `json:"totalItems"`
	CompletedItems                 int             `json:"completedItems"`
	FailedItems                    int             `json:"failedItems"`
	Revision                       int64           `json:"revision"`
	AttemptGeneration              int             `json:"attemptGeneration"`
	RootWorkflowRunID              *string         `json:"rootWorkflowRunId,omitempty"`
	RetryOfWorkflowRunID           *string         `json:"retryOfWorkflowRunId,omitempty"`
	UpdatedAt                      time.Time       `json:"updatedAt"`
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
	Revision       int64           `json:"revision"`
	UpdatedAt      time.Time       `json:"updatedAt"`
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

type workflowRunCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
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
	if project.ProjectKind.IsCommerce() {
		httpx.WriteError(w, r, http.StatusConflict, "PROJECT_KIND_MISMATCH", "带货视频必须通过专用生产流程启动任务", map[string]any{
			"actualProjectKind":   project.ProjectKind,
			"expectedProjectKind": commercepkg.ProjectKindNarrative,
		}, false)
		return
	}

	workflowType := strings.TrimSpace(req.WorkflowType)
	if workflowType == "" {
		workflowType = "video_production"
	}
	if workflowType != "video_production" && workflowType != "text_to_storyboard" && workflowType != "extract_novel_events" && workflowType != "generate_adaptation_plan" && workflowType != "adaptation_plan_to_script" && workflowType != "source_to_script" && workflowType != "parse_script_scenes" && workflowType != "script_to_assets" && workflowType != "script_to_storyboard" && workflowType != "script_episode_timing" && workflowType != "script_to_video" && workflowType != "full_production" && workflowType != "compose_timeline" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowType is not supported", nil, false)
		return
	}
	workflowRequestInput, err := normalizeWorkflowRequestInput(workflowType, req.Input, project)
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
	input := json.RawMessage(mustMarshal(map[string]any{"prompt": strings.TrimSpace(req.Prompt), "workflowType": workflowType, "input": workflowRequestInput}))
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
	case "script_episode_timing":
		workflowFunc = workflows.AnalyzeScriptEpisodeTimingWorkflow
	case "compose_timeline":
		workflowFunc = workflows.ComposeTimelineWorkflow
	default:
		workflowFunc = workflows.TextToStoryboardWorkflow
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	lockedProject, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), project.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if lockedProject.OrganizationID != project.OrganizationID {
		s.writeError(w, r, newAPIError(http.StatusNotFound, "NOT_FOUND", "project was not found"))
		return
	}
	claim, err := claimIdempotencyTx(r.Context(), tx, lockedProject.OrganizationID, "workflow-runs:create", idempotency, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay WorkflowRun
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		_ = tx.Rollback(r.Context())
		status := claim.replayStatus
		if status < 200 || status > 299 {
			status = http.StatusOK
		}
		httpx.WriteJSON(w, r, status, replay, map[string]any{"idempotentReplay": true, "operationId": claim.state.operationID})
		return
	}
	operationID, err := ensureRuntimeOperationTx(
		r.Context(), tx, &claim, lockedProject.OrganizationID, lockedProject.ID, "workflow-runs:create", requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := s.enqueueProjectWorkflowTx(
		r.Context(), tx, principal, lockedProject, workflowType, input, workflows.ScriptTaskQueue, workflowFunc,
		func(run WorkflowRun) any {
			return workflows.TextToStoryboardInput{
				OrganizationID: run.OrganizationID,
				ProjectID:      run.ProjectID,
				WorkflowRunID:  run.ID,
				Prompt:         strings.TrimSpace(req.Prompt),
				CreatedBy:      principal.UserID,
				Input:          workflowRequestInput,
			}
		},
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			startInput := workflows.TextToStoryboardInput{
				OrganizationID: run.OrganizationID,
				ProjectID:      run.ProjectID,
				WorkflowRunID:  run.ID,
				Prompt:         strings.TrimSpace(req.Prompt),
				CreatedBy:      principal.UserID,
				Input:          workflowRequestInput,
			}
			snapshotRaw, snapshotHash, err := marshalWorkflowStartInput(startInput)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO workflow_input_snapshots(
					workflow_run_id, organization_id, project_id, project_revision, snapshot, snapshot_hash,
					production_generation_id
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, run.ID, run.OrganizationID, run.ProjectID, lockedProject.Revision, snapshotRaw, snapshotHash,
				run.ProductionGenerationID); err != nil {
				return err
			}
			updated, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), run.ID))
			if err != nil {
				return err
			}
			if _, err := completeRuntimeOperationTx(ctx, tx, operationID, run.ID, updated); err != nil {
				return err
			}
			return completeIdempotencyTxWithStatus(ctx, tx, claim.state, http.StatusAccepted, updated)
		},
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), run.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, updated, map[string]any{"operationId": operationID})
}

func (s *Server) listWorkflowRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	projectID := strings.TrimSpace(r.URL.Query().Get("filter[projectId]"))
	if projectID != "" {
		if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: projectID}) {
			return
		}
	} else if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{OrganizationID: orgID}) {
		return
	}

	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter[status]")))
	if statusFilter == "" {
		statusFilter = "all"
	}
	if statusFilter != "active" && statusFilter != "terminal" && statusFilter != "all" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "filter[status] must be active, terminal, or all", nil, false)
		return
	}
	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	if view != "" && view != "activity" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "view must be activity when provided", nil, false)
		return
	}
	activityView := view == "activity"
	if activityView && projectID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "filter[projectId] is required for the activity view", nil, false)
		return
	}
	limit := queryInt(r, "limit", 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var cursorCreatedAt *time.Time
	var cursorID *string
	if rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor")); rawCursor != "" {
		cursor, err := decodeWorkflowRunCursor(rawCursor)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "cursor is invalid", nil, false)
			return
		}
		cursorCreatedAt = &cursor.CreatedAt
		cursorID = &cursor.ID
	}
	if projectID != "" {
		project, err := projectByIDForControl(r.Context(), s, projectID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		page, err := s.listProjectWorkflowRunsAction(r.Context(), project, workflowRunListActionInput{
			Status: statusFilter, Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
			ActivityView: activityView, ActorUserID: principal.UserID,
		})
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
			"items": page.Items, "hasMore": page.NextCursor != "", "nextCursor": page.NextCursor,
		}, nil)
		return
	}

	rows, err := s.db.Query(r.Context(), workflowRunSelectSQL(`
		WHERE organization_id = $1
		  AND ($2 = '' OR project_id = NULLIF($2, '')::uuid)
		  AND (
		        $3 = 'all'
		        OR ($3 = 'active' AND status NOT IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped'))
		        OR ($3 = 'terminal' AND status IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped'))
		      )
		  AND (
		        $4::timestamptz IS NULL
		        OR created_at < $4::timestamptz
		        OR (created_at = $4::timestamptz AND id < $5::uuid)
		      )
		  AND (
		        NOT $6::boolean
		        OR status NOT IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped')
		        OR COALESCE(completed_at, cancelled_at, updated_at) > COALESCE((
		            SELECT cleared_terminal_through
		            FROM workflow_activity_views
		            WHERE organization_id = $1
		              AND project_id = NULLIF($2, '')::uuid
		              AND user_id = $7::uuid
		        ), '-infinity'::timestamptz)
		      )
		ORDER BY created_at DESC, id DESC
		LIMIT $8
	`), orgID, projectID, statusFilter, cursorCreatedAt, cursorID, activityView, principal.UserID, limit+1)
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
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = encodeWorkflowRunCursor(items[len(items)-1])
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items":      items,
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	}, nil)
}

func (s *Server) clearCompletedWorkflowActivity(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	var clearedThrough time.Time
	if err := tx.QueryRow(r.Context(), `SELECT now()`).Scan(&clearedThrough); err != nil {
		s.writeError(w, r, err)
		return
	}
	var workflowClearedCount int
	if err := tx.QueryRow(r.Context(), `
		SELECT count(*)
		FROM workflow_runs
		WHERE organization_id = $1
		  AND project_id = $2
		  AND status IN ('succeeded', 'partial_succeeded', 'completed', 'failed', 'cancelled', 'skipped')
		  AND COALESCE(completed_at, cancelled_at, updated_at) <= $4
		  AND COALESCE(completed_at, cancelled_at, updated_at) > COALESCE((
		      SELECT cleared_terminal_through
		      FROM workflow_activity_views
		      WHERE organization_id = $1
		        AND project_id = $2
		        AND user_id = $3
		  ), '-infinity'::timestamptz)
	`, project.OrganizationID, project.ID, principal.UserID, clearedThrough).Scan(&workflowClearedCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	var commandClearedCount int
	if err := tx.QueryRow(r.Context(), `
		SELECT count(*)
		FROM project_control_commands
		WHERE organization_id = $1
		  AND project_id = $2
		  AND actor_user_id = $3
		  AND activity_visibility = 'primary'
		  AND status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled')
		  AND COALESCE(completed_at, updated_at) <= $4
		  AND COALESCE(completed_at, updated_at) > COALESCE((
		      SELECT cleared_terminal_through
		      FROM workflow_activity_views
		      WHERE organization_id = $1
		        AND project_id = $2
		        AND user_id = $3
		  ), '-infinity'::timestamptz)
	`, project.OrganizationID, project.ID, principal.UserID, clearedThrough).Scan(&commandClearedCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO workflow_activity_views(
		    organization_id, project_id, user_id, cleared_terminal_through, updated_at
		)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (project_id, user_id) DO UPDATE
		SET organization_id = EXCLUDED.organization_id,
		    cleared_terminal_through = GREATEST(
		        workflow_activity_views.cleared_terminal_through,
		        EXCLUDED.cleared_terminal_through
		    ),
		    updated_at = now()
	`, project.OrganizationID, project.ID, principal.UserID, clearedThrough); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"clearedCount":         workflowClearedCount + commandClearedCount,
		"workflowClearedCount": workflowClearedCount,
		"commandClearedCount":  commandClearedCount,
		"clearedThrough":       clearedThrough,
	}, nil)
}

func (s *Server) getWorkflowRun(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1
	`), r.PathValue("workflowRunId")))
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
	item, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1
	`), r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowCancel, authz.Resource{ProjectID: item.ProjectID}) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "User requested cancellation"
	}
	updated, err := s.cancelWorkflowRunItem(r.Context(), item, reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func (s *Server) cancelWorkflowRunItem(ctx context.Context, item WorkflowRun, reason string) (WorkflowRun, error) {
	if isTerminalWorkflowStatus(item.Status) {
		return item, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "User requested cancellation"
	}
	preStart, err := s.cancelWorkflowStartOutbox(ctx, item.ID, reason)
	if err != nil {
		return WorkflowRun{}, err
	}
	if preStart {
		if err := workflows.CancelWorkflowRun(ctx, s.db, item.ID, json.RawMessage(`{}`), reason); err != nil {
			return WorkflowRun{}, err
		}
		if s.temporal != nil {
			if err := s.temporal.CancelWorkflow(ctx, item.TemporalWorkflowID, ""); err != nil {
				var notFound *serviceerror.NotFound
				if !errors.As(err, &notFound) {
					_ = s.insertWorkflowCancelWarning(ctx, item, reason, err)
				}
			}
		}
		return scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`
			WHERE id = $1
		`), item.ID))
	}
	if err := workflows.MarkWorkflowCancelling(ctx, s.db, item.ID, reason); err != nil {
		return WorkflowRun{}, err
	}
	if s.temporal != nil {
		if err := s.temporal.CancelWorkflow(ctx, item.TemporalWorkflowID, ""); err != nil {
			_ = s.insertWorkflowCancelWarning(ctx, item, reason, err)
		}
	}
	return scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE id = $1
	`), item.ID))
}

func (s *Server) cancelWorkflowStartOutbox(ctx context.Context, workflowRunID, reason string) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM workflow_start_outbox
		WHERE workflow_run_id = $1
		FOR UPDATE
	`, workflowRunID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	if status != "pending" && status != "processing" && status != "cancelled" {
		return false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'cancelled', completed_at = COALESCE(completed_at, now()),
		    locked_at = NULL, locked_by = NULL,
		    last_error_code = 'USER_CANCELLED', last_error_message = $2,
		    updated_at = now()
		WHERE workflow_run_id = $1
	`, workflowRunID, reason); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Server) listWorkflowNodeRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	run, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1
	`), r.PathValue("workflowRunId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionWorkflowRead, authz.Resource{ProjectID: run.ProjectID}) {
		return
	}
	project, err := projectByIDForControl(r.Context(), s, run.ProjectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := s.listWorkflowNodesAction(r.Context(), project, workflowRunChildrenActionInput{
		WorkflowRunID: run.ID,
		Limit:         queryInt(r, "limit", 200),
		Cursor:        strings.TrimSpace(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items": page.Items, "hasMore": page.NextCursor != "", "nextCursor": page.NextCursor,
	}, nil)
}

func isTerminalWorkflowStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "partial_succeeded", "completed", "failed", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func encodeWorkflowRunCursor(run WorkflowRun) string {
	payload, _ := json.Marshal(workflowRunCursor{CreatedAt: run.CreatedAt.UTC(), ID: run.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeWorkflowRunCursor(raw string) (workflowRunCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return workflowRunCursor{}, err
	}
	var cursor workflowRunCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return workflowRunCursor{}, err
	}
	if cursor.CreatedAt.IsZero() {
		return workflowRunCursor{}, errors.New("workflow run cursor createdAt is required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(cursor.ID)); err != nil {
		return workflowRunCursor{}, errors.New("workflow run cursor id is invalid")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	cursor.ID = strings.TrimSpace(cursor.ID)
	return cursor, nil
}

func (s *Server) insertWorkflowCancelWarning(ctx context.Context, run WorkflowRun, reason string, cause error) error {
	return insertAPIEvent(ctx, s.db, run.OrganizationID, run.ProjectID, "workflow.run.cancel_warning", "workflow_run", run.ID, mustMarshal(map[string]any{
		"workflowRunId": run.ID,
		"reason":        reason,
		"message":       cause.Error(),
	}))
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
	if projectID != "" {
		project, err := s.project(r, projectID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		page, err := s.listProjectArtifactsAction(r.Context(), project, artifactListActionInput{
			Type:  firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("filter[type]")), strings.TrimSpace(r.URL.Query().Get("type"))),
			Limit: 100, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")), Preview: includePreviewURL,
			Expires: int(previewExpires / time.Second),
		})
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": page.Items}, map[string]any{"nextCursor": page.NextCursor})
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT artifact.id, artifact.organization_id, artifact.project_id, artifact.workflow_run_id,
		       artifact.node_run_id, artifact.type, artifact.storage_key, artifact.mime_type,
		       artifact.content_hash, artifact.prompt_hash, artifact.model_id, artifact.metadata, artifact.created_at
		FROM artifacts artifact
		LEFT JOIN projects project ON project.id = artifact.project_id
		WHERE artifact.organization_id = $1
		  AND ($2 = '' OR artifact.project_id = $2::uuid)
		  AND (
		    artifact.project_id IS NULL
		    OR artifact.production_generation_id IS NULL
		    OR artifact.production_generation_id = project.active_video_production_generation_id
		    OR EXISTS (SELECT 1 FROM asset_references ref WHERE ref.artifact_id = artifact.id AND ref.status = 'ready')
		    OR EXISTS (
		      SELECT 1 FROM canonical_assets asset
		      WHERE asset.primary_reference_artifact_id = artifact.id OR asset.reference_artifact_id = artifact.id
		    )
		    OR EXISTS (SELECT 1 FROM novel_chapters chapter WHERE chapter.content_artifact_id = artifact.id)
		    OR EXISTS (SELECT 1 FROM novels novel WHERE novel.raw_artifact_id = artifact.id OR novel.clean_artifact_id = artifact.id)
		    OR EXISTS (SELECT 1 FROM script_versions version WHERE version.content_artifact_id = artifact.id)
		  )
		ORDER BY artifact.created_at DESC
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
		&item.ProductionGenerationID,
		&item.VideoProductionBindingID,
		&item.VideoProductionBindingRevision,
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
		&item.WorkflowType,
		&item.TotalItems,
		&item.CompletedItems,
		&item.FailedItems,
		&item.Revision,
		&item.AttemptGeneration,
		&item.RootWorkflowRunID,
		&item.RetryOfWorkflowRunID,
		&item.UpdatedAt,
	)
	return item, err
}

func workflowRunSelectSQL(where string) string {
	return `
		SELECT id, organization_id, project_id, production_generation_id,
		       video_production_binding_id, video_production_binding_revision,
		       template_id, temporal_workflow_id, status,
		       input, output, error_code, error_message, created_by, created_at,
		       started_at, completed_at, cancelled_at, workflow_type, total_items,
		       completed_items, failed_items, revision, attempt_generation, root_workflow_run_id,
		       retry_of_workflow_run_id, updated_at
		FROM workflow_runs
	` + where
}

func normalizeWorkflowRequestInput(workflowType string, raw json.RawMessage, project Project) (json.RawMessage, error) {
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
			if projectAspectRatio := projectDefaultAspectRatio(project); projectAspectRatio != nil {
				aspectRatio = *projectAspectRatio
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
	if workflowType == "compose_timeline" {
		if value, ok := values["timelineId"].(string); !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("input.timelineId is required")
		}
		if value, ok := values["aspectRatio"].(string); !ok || strings.TrimSpace(value) == "" {
			aspectRatio := "16:9"
			if projectAspectRatio := projectDefaultAspectRatio(project); projectAspectRatio != nil {
				aspectRatio = *projectAspectRatio
			}
			values["aspectRatio"] = aspectRatio
		}
		if value, ok := values["resolution"].(string); !ok || strings.TrimSpace(value) == "" {
			values["resolution"] = "720p"
		}
	}
	if workflowType == "script_to_storyboard" {
		if value, ok := values["scriptId"].(string); !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("input.scriptId is required")
		}
		pacing, _ := values["pacingProfile"].(string)
		pacing = strings.ToLower(strings.TrimSpace(pacing))
		if pacing == "" {
			pacing = "standard"
		}
		if pacing != "standard" && pacing != "fast" && pacing != "slow" {
			return nil, fmt.Errorf("input.pacingProfile is not supported")
		}
		values["pacingProfile"] = pacing
		if value, ok := values["targetDurationSeconds"].(float64); ok && value <= 0 {
			return nil, fmt.Errorf("input.targetDurationSeconds must be positive")
		}
		if value, ok := values["plannerBatchMaxShots"].(float64); !ok || value == 0 {
			values["plannerBatchMaxShots"] = 12
		} else if value < 8 || value > 16 {
			return nil, fmt.Errorf("input.plannerBatchMaxShots must be between 8 and 16")
		}
		if value, ok := values["maxSceneConcurrency"].(float64); !ok || value == 0 {
			values["maxSceneConcurrency"] = 3
		} else if value < 1 || value > 8 {
			return nil, fmt.Errorf("input.maxSceneConcurrency must be between 1 and 8")
		}
		if value, ok := values["shotBudget"].(float64); ok && value < 0 {
			return nil, fmt.Errorf("input.shotBudget cannot be negative")
		}
		audioStrategy, _ := values["audioStrategy"].(string)
		audioStrategy = firstNonEmptyString(strings.ToLower(strings.TrimSpace(audioStrategy)), project.AudioStrategy, "native_av")
		audioRequirement, _ := values["audioRequirement"].(string)
		audioRequirement = firstNonEmptyString(strings.ToLower(strings.TrimSpace(audioRequirement)), project.AudioRequirement, "preferred")
		if !validProjectAudioSettings(audioStrategy, audioRequirement) {
			return nil, fmt.Errorf("input.audioStrategy or input.audioRequirement is not supported")
		}
		values["audioStrategy"] = audioStrategy
		values["audioRequirement"] = audioRequirement
	}
	if workflowType == "script_episode_timing" {
		for _, key := range []string{"scriptId", "scriptVersionId", "scriptEpisodeId"} {
			if value, ok := values[key].(string); !ok || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("input.%s is required", key)
			}
		}
		if value, ok := values["targetDurationSeconds"].(float64); ok && value <= 0 {
			return nil, fmt.Errorf("input.targetDurationSeconds must be positive")
		}
	} else if workflowType == "script_to_assets" || workflowType == "script_to_video" || workflowType == "full_production" {
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
