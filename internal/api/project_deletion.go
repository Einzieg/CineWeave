package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/projectdeletion"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	codeProjectDeletionAlreadyRunning = "PROJECT_DELETION_ALREADY_RUNNING"
	codeProjectDeletionInProgress     = "PROJECT_DELETION_IN_PROGRESS"
	codeProjectDeletionBlocked        = "PROJECT_DELETION_BLOCKED"
	defaultProjectDeletionDrain       = 15 * time.Minute
)

type ProjectDeletionImpact struct {
	ProjectID               string    `json:"projectId"`
	ProjectName             string    `json:"projectName"`
	ProjectRevision         int64     `json:"projectRevision"`
	CurrentDeletionRevision int64     `json:"currentDeletionRevision"`
	ProductCount            int       `json:"productCount"`
	ScriptUnitCount         int       `json:"scriptUnitCount"`
	StoryboardShotCount     int       `json:"storyboardShotCount"`
	ArtifactCount           int       `json:"artifactCount"`
	MediaFileCount          int       `json:"mediaFileCount"`
	FinalVideoCount         int       `json:"finalVideoCount"`
	ActiveWorkflowCount     int       `json:"activeWorkflowCount"`
	ActiveAgentTaskCount    int       `json:"activeAgentTaskCount"`
	ActiveProviderTaskCount int       `json:"activeProviderTaskCount"`
	StorageObjectCount      int       `json:"storageObjectCount"`
	StorageByteSize         int64     `json:"storageByteSize"`
	GeneratedAt             time.Time `json:"generatedAt"`
	ImpactHash              string    `json:"impactHash"`
}

type ProjectDeletionRequest struct {
	ID                        string          `json:"id"`
	OrganizationID            string          `json:"organizationId"`
	WorkspaceID               string          `json:"workspaceId"`
	ProjectID                 string          `json:"projectId"`
	ProjectName               string          `json:"projectName"`
	ProjectRevision           int64           `json:"projectRevision"`
	DeletionRevision          int64           `json:"deletionRevision"`
	Status                    string          `json:"status"`
	ImpactSnapshot            json.RawMessage `json:"impactSnapshot"`
	ImpactHash                string          `json:"impactHash"`
	ManifestCursor            int64           `json:"manifestCursor"`
	StorageObjectCount        int             `json:"storageObjectCount"`
	StorageDeletedCount       int             `json:"storageDeletedCount"`
	StorageFailedCount        int             `json:"storageFailedCount"`
	StorageSkippedSharedCount int             `json:"storageSkippedSharedCount"`
	TemporalWorkflowID        string          `json:"temporalWorkflowId"`
	IdempotencyKey            string          `json:"idempotencyKey"`
	RequestedBy               *string         `json:"requestedBy,omitempty"`
	RequestedAt               time.Time       `json:"requestedAt"`
	StartedAt                 *time.Time      `json:"startedAt,omitempty"`
	DrainDeadlineAt           time.Time       `json:"drainDeadlineAt"`
	UpdatedAt                 time.Time       `json:"updatedAt"`
	CompletedAt               *time.Time      `json:"completedAt,omitempty"`
	ExpiresAt                 *time.Time      `json:"expiresAt,omitempty"`
	ErrorCode                 *string         `json:"errorCode,omitempty"`
	ErrorMessage              *string         `json:"errorMessage,omitempty"`
	RetryCount                int             `json:"retryCount"`
}

type createProjectDeletionRequestBody struct {
	ProjectName             string `json:"projectName"`
	ExpectedProjectRevision int64  `json:"expectedProjectRevision"`
	ImpactHash              string `json:"impactHash"`
}

func (s *Server) getProjectDeletionImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectDelete, authz.Resource{ProjectID: project.ID}) {
		return
	}
	impact, err := s.projectDeletionImpact(r.Context(), s.db, project)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, impact, nil)
}

func (s *Server) createProjectDeletionRequest(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	if s.temporal == nil {
		s.writeError(w, r, newAPIError(http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "工作流服务暂不可用"))
		return
	}
	var body createProjectDeletionRequestBody
	if !decode(w, r, &body) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		s.writeError(w, r, newAPIError(http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "必须提供 Idempotency-Key"))
		return
	}
	projectID := r.PathValue("projectId")
	project, err := s.projectIncludingDeleting(r.Context(), projectID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionProjectDelete, authz.Resource{ProjectID: project.ID}) {
		return
	}
	if strings.TrimSpace(body.ProjectName) != project.Name {
		s.writeError(w, r, newAPIError(http.StatusUnprocessableEntity, "PROJECT_NAME_CONFIRMATION_MISMATCH", "请输入完整项目名称以确认删除"))
		return
	}
	if body.ExpectedProjectRevision <= 0 || strings.TrimSpace(body.ImpactHash) == "" {
		s.writeError(w, r, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedProjectRevision 和 impactHash 不能为空"))
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	locked, err := scanProject(tx.QueryRow(r.Context(), projectSelectSQL(`WHERE p.id = $1 FOR UPDATE OF p`), projectID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if locked.LifecycleStatus == "deleting" {
		existing, found, findErr := projectDeletionRequestByProjectTx(r.Context(), tx, projectID)
		if findErr != nil {
			s.writeError(w, r, findErr)
			return
		}
		if found && existing.IdempotencyKey == idempotencyKey {
			if err := tx.Commit(r.Context()); err != nil {
				s.writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, r, http.StatusAccepted, existing, nil)
			return
		}
		conflict := newAPIError(http.StatusConflict, codeProjectDeletionAlreadyRunning, "项目已有删除任务正在执行")
		if found {
			conflict.Details = map[string]any{"requestId": existing.ID, "status": existing.Status}
		}
		s.writeError(w, r, conflict)
		return
	}
	if locked.Revision != body.ExpectedProjectRevision {
		s.writeError(w, r, projectRevisionConflict(locked, body.ExpectedProjectRevision))
		return
	}
	impact, err := s.projectDeletionImpact(r.Context(), tx, locked)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if impact.ImpactHash != strings.TrimSpace(body.ImpactHash) {
		conflict := newAPIError(http.StatusConflict, "PROJECT_DELETION_IMPACT_STALE", "项目删除影响已变化，请重新确认")
		conflict.Details = map[string]any{"currentImpact": impact}
		s.writeError(w, r, conflict)
		return
	}
	requestID := uuid.NewString()
	temporalWorkflowID := "project-deletion-" + requestID
	deletionRevision := locked.DeletionRevision + 1
	drainDeadline := time.Now().UTC().Add(projectDeletionDrainDuration())
	impactSnapshot, err := json.Marshal(impact)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	command, err := tx.Exec(r.Context(), `
		UPDATE projects
		SET lifecycle_status = 'deleting',
		    deletion_revision = $2,
		    deletion_requested_at = now(),
		    video_production_locked = true,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND lifecycle_status = 'active'
		  AND revision = $3
	`, projectID, deletionRevision, locked.Revision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if command.RowsAffected() != 1 {
		s.writeError(w, r, newAPIError(http.StatusConflict, codeProjectDeletionBlocked, "项目状态已变化，请重新确认删除"))
		return
	}
	var request ProjectDeletionRequest
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO project_deletion_requests(
			id, organization_id, workspace_id, project_id, project_name,
			project_revision, deletion_revision, status, impact_snapshot, impact_hash,
			temporal_workflow_id, idempotency_key, requested_by, drain_deadline_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, 'requested', $8, $9,
			$10, $11, $12, $13
		)
		RETURNING `+projectDeletionRequestColumns+`
	`, requestID, locked.OrganizationID, locked.WorkspaceID, locked.ID, locked.Name,
		locked.Revision, deletionRevision, impactSnapshot, impact.ImpactHash,
		temporalWorkflowID, idempotencyKey, principal.UserID, drainDeadline,
	).Scan(projectDeletionRequestScanTargets(&request)...); err != nil {
		s.writeError(w, r, err)
		return
	}
	input := workflows.ProjectDeletionInput{
		OrganizationID:   locked.OrganizationID,
		WorkspaceID:      locked.WorkspaceID,
		ProjectID:        locked.ID,
		RequestID:        request.ID,
		DeletionRevision: deletionRevision,
		RequestedBy:      principal.UserID,
	}
	if err := s.enqueueProjectDeletionStartTx(r.Context(), tx, request, input); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := events.AppendTx(
		r.Context(),
		tx,
		locked.OrganizationID,
		"",
		"project.deletion.requested",
		"project_deletion_request",
		request.ID,
		mustMarshal(map[string]any{
			"projectDeletionRequestId": request.ID,
			"projectId":                request.ProjectID,
			"deletionRevision":         request.DeletionRevision,
			"status":                   request.Status,
		}),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, request, nil)
}

func (s *Server) getProjectDeletionRequest(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	request, err := s.projectDeletionRequest(r.Context(), r.PathValue("projectId"), r.PathValue("requestId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.canReadProjectDeletionRequest(w, r, principal, request) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, request, nil)
}

func (s *Server) retryProjectDeletionRequest(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	if s.temporal == nil {
		s.writeError(w, r, newAPIError(http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "工作流服务暂不可用"))
		return
	}
	projectID := r.PathValue("projectId")
	requestID := r.PathValue("requestId")
	request, err := s.projectDeletionRequest(r.Context(), projectID, requestID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.canReadProjectDeletionRequest(w, r, principal, request) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err := tx.QueryRow(r.Context(), `
		SELECT `+projectDeletionRequestColumns+`
		FROM project_deletion_requests
		WHERE id = $1 AND project_id = $2
		FOR UPDATE
	`, requestID, projectID).Scan(projectDeletionRequestScanTargets(&request)...); err != nil {
		s.writeError(w, r, err)
		return
	}
	if request.Status != "failed_retryable" {
		conflict := newAPIError(http.StatusConflict, "PROJECT_DELETION_RETRY_NOT_ALLOWED", "当前删除任务不能重试")
		conflict.Details = map[string]any{"status": request.Status}
		s.writeError(w, r, conflict)
		return
	}
	var lifecycle string
	var deletionRevision int64
	if err := tx.QueryRow(r.Context(), `
		SELECT lifecycle_status, deletion_revision
		FROM projects
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, projectID, request.OrganizationID).Scan(&lifecycle, &deletionRevision); err != nil {
		s.writeError(w, r, err)
		return
	}
	if lifecycle != "deleting" || deletionRevision != request.DeletionRevision {
		s.writeError(w, r, newAPIError(http.StatusConflict, codeProjectDeletionBlocked, "项目删除身份已变化，不能重试"))
		return
	}
	drainDeadline := time.Now().UTC().Add(projectDeletionDrainDuration())
	if _, err := tx.Exec(r.Context(), `
		UPDATE project_deletion_requests
		SET status = 'requested',
		    retry_count = retry_count + 1,
		    drain_deadline_at = $2,
		    completed_at = NULL,
		    expires_at = NULL,
		    error_code = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE id = $1
	`, requestID, drainDeadline); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE workflow_start_outbox
		SET status = 'pending',
		    attempt_count = 0,
		    next_attempt_at = now(),
		    started_at = NULL,
		    completed_at = NULL,
		    locked_at = NULL,
		    locked_by = NULL,
		    last_error_code = NULL,
		    last_error_message = NULL,
		    updated_at = now()
		WHERE project_deletion_request_id = $1
	`, requestID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.QueryRow(r.Context(), `
		SELECT `+projectDeletionRequestColumns+`
		FROM project_deletion_requests
		WHERE id = $1
	`, requestID).Scan(projectDeletionRequestScanTargets(&request)...); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := events.AppendTx(
		r.Context(),
		tx,
		request.OrganizationID,
		"",
		"project.deletion.requested",
		"project_deletion_request",
		request.ID,
		mustMarshal(map[string]any{
			"projectDeletionRequestId": request.ID,
			"projectId":                request.ProjectID,
			"deletionRevision":         request.DeletionRevision,
			"status":                   request.Status,
			"retryCount":               request.RetryCount,
		}),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, request, nil)
}

func (s *Server) canReadProjectDeletionRequest(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	request ProjectDeletionRequest,
) bool {
	if organizationID(r, principal) != request.OrganizationID {
		s.writeError(w, r, auth.ErrForbidden)
		return false
	}
	if request.RequestedBy != nil && *request.RequestedBy == principal.UserID {
		return true
	}
	return s.authorize(
		w,
		r,
		principal,
		authz.PermissionProjectDelete,
		authz.Resource{OrganizationID: request.OrganizationID},
	)
}

func (s *Server) enqueueProjectDeletionStartTx(
	ctx context.Context,
	tx pgx.Tx,
	request ProjectDeletionRequest,
	input workflows.ProjectDeletionInput,
) error {
	if _, ok := workflowStartDefinitions["project_deletion"]; !ok {
		return errors.New("project deletion workflow is not registered")
	}
	raw, inputHash, err := marshalWorkflowStartInput(input)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id,
			agent_task_id,
			commerce_setup_run_id,
			project_deletion_request_id,
			organization_id,
			project_id,
			production_generation_id,
			workflow_type,
			workflow_handler,
			temporal_workflow_id,
			task_queue,
			input,
			input_hash,
			max_attempts
		)
		VALUES (
			NULL,
			NULL,
			NULL,
			$1,
			$2,
			$3,
			NULL,
			'project_deletion',
			'project_deletion',
			$4,
			$5,
			$6,
			$7,
			$8
		)
	`, request.ID, request.OrganizationID, request.ProjectID, request.TemporalWorkflowID,
		workflows.ScriptTaskQueue, raw, inputHash, workflowStartDefaultMaxAttempts)
	return err
}

func (s *Server) projectDeletionImpact(
	ctx context.Context,
	db interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	project Project,
) (ProjectDeletionImpact, error) {
	impact := ProjectDeletionImpact{
		ProjectID:               project.ID,
		ProjectName:             project.Name,
		ProjectRevision:         project.Revision,
		CurrentDeletionRevision: project.DeletionRevision,
		GeneratedAt:             time.Now().UTC().Truncate(time.Second),
	}
	err := db.QueryRow(ctx, `
		WITH storage_candidates AS (
			SELECT candidate.storage_key, candidate.byte_size
			FROM (`+projectdeletion.StorageCandidateUnion("$1")+`) candidate
		), storage_objects AS (
			SELECT btrim(storage_key) AS storage_key, max(byte_size) AS byte_size
			FROM storage_candidates
			WHERE NULLIF(btrim(storage_key), '') IS NOT NULL
			GROUP BY btrim(storage_key)
		)
		SELECT
			(SELECT count(*) FROM commerce_products WHERE project_id = $1),
			(SELECT count(*) FROM commerce_script_units WHERE project_id = $1 AND status <> 'archived'),
			(SELECT count(*) FROM storyboard_shots WHERE project_id = $1 AND deleted_at IS NULL),
			(SELECT count(*) FROM artifacts WHERE project_id = $1),
			(SELECT count(*) FROM media_files WHERE project_id = $1),
			(SELECT count(*) FROM final_video_versions WHERE project_id = $1),
			(SELECT count(*) FROM workflow_runs
			 WHERE project_id = $1 AND status IN ('pending', 'queued', 'running', 'cancelling', 'waiting_review')),
			(SELECT count(*) FROM agent_tasks
			 WHERE project_id = $1 AND status IN ('queued', 'planning', 'waiting_approval', 'running')),
			(SELECT count(*) FROM provider_async_tasks
			 WHERE project_id = $1 AND status IN ('queued', 'running', 'cancelling')),
			(SELECT count(*) FROM storage_objects),
			(SELECT COALESCE(sum(byte_size), 0) FROM storage_objects)
	`, project.ID).Scan(
		&impact.ProductCount,
		&impact.ScriptUnitCount,
		&impact.StoryboardShotCount,
		&impact.ArtifactCount,
		&impact.MediaFileCount,
		&impact.FinalVideoCount,
		&impact.ActiveWorkflowCount,
		&impact.ActiveAgentTaskCount,
		&impact.ActiveProviderTaskCount,
		&impact.StorageObjectCount,
		&impact.StorageByteSize,
	)
	if err != nil {
		return ProjectDeletionImpact{}, err
	}
	impact.ImpactHash = idempotencyRequestHash(struct {
		ProjectID               string `json:"projectId"`
		ProjectName             string `json:"projectName"`
		ProjectRevision         int64  `json:"projectRevision"`
		CurrentDeletionRevision int64  `json:"currentDeletionRevision"`
		ProductCount            int    `json:"productCount"`
		ScriptUnitCount         int    `json:"scriptUnitCount"`
		StoryboardShotCount     int    `json:"storyboardShotCount"`
		ArtifactCount           int    `json:"artifactCount"`
		MediaFileCount          int    `json:"mediaFileCount"`
		FinalVideoCount         int    `json:"finalVideoCount"`
		ActiveWorkflowCount     int    `json:"activeWorkflowCount"`
		ActiveAgentTaskCount    int    `json:"activeAgentTaskCount"`
		ActiveProviderTaskCount int    `json:"activeProviderTaskCount"`
		StorageObjectCount      int    `json:"storageObjectCount"`
		StorageByteSize         int64  `json:"storageByteSize"`
	}{
		ProjectID:               impact.ProjectID,
		ProjectName:             impact.ProjectName,
		ProjectRevision:         impact.ProjectRevision,
		CurrentDeletionRevision: impact.CurrentDeletionRevision,
		ProductCount:            impact.ProductCount,
		ScriptUnitCount:         impact.ScriptUnitCount,
		StoryboardShotCount:     impact.StoryboardShotCount,
		ArtifactCount:           impact.ArtifactCount,
		MediaFileCount:          impact.MediaFileCount,
		FinalVideoCount:         impact.FinalVideoCount,
		ActiveWorkflowCount:     impact.ActiveWorkflowCount,
		ActiveAgentTaskCount:    impact.ActiveAgentTaskCount,
		ActiveProviderTaskCount: impact.ActiveProviderTaskCount,
		StorageObjectCount:      impact.StorageObjectCount,
		StorageByteSize:         impact.StorageByteSize,
	})
	return impact, nil
}

func (s *Server) projectDeletionRequest(ctx context.Context, projectID, requestID string) (ProjectDeletionRequest, error) {
	var request ProjectDeletionRequest
	err := s.db.QueryRow(ctx, `
		SELECT `+projectDeletionRequestColumns+`
		FROM project_deletion_requests
		WHERE id = $1 AND project_id = $2
	`, requestID, projectID).Scan(projectDeletionRequestScanTargets(&request)...)
	return request, err
}

func projectDeletionRequestByProjectTx(ctx context.Context, tx pgx.Tx, projectID string) (ProjectDeletionRequest, bool, error) {
	var request ProjectDeletionRequest
	err := tx.QueryRow(ctx, `
		SELECT `+projectDeletionRequestColumns+`
		FROM project_deletion_requests
		WHERE project_id = $1
		  AND status NOT IN ('completed', 'failed_terminal')
		ORDER BY requested_at DESC
		LIMIT 1
	`, projectID).Scan(projectDeletionRequestScanTargets(&request)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectDeletionRequest{}, false, nil
	}
	return request, err == nil, err
}

const projectDeletionRequestColumns = `
	id::text,
	organization_id::text,
	workspace_id::text,
	project_id::text,
	project_name,
	project_revision,
	deletion_revision,
	status,
	impact_snapshot,
	impact_hash,
	manifest_cursor,
	storage_object_count,
	storage_deleted_count,
	storage_failed_count,
	storage_skipped_shared_count,
	temporal_workflow_id,
	idempotency_key,
	requested_by::text,
	requested_at,
	started_at,
	drain_deadline_at,
	updated_at,
	completed_at,
	expires_at,
	error_code,
	error_message,
	retry_count
`

func projectDeletionRequestScanTargets(request *ProjectDeletionRequest) []any {
	return []any{
		&request.ID,
		&request.OrganizationID,
		&request.WorkspaceID,
		&request.ProjectID,
		&request.ProjectName,
		&request.ProjectRevision,
		&request.DeletionRevision,
		&request.Status,
		&request.ImpactSnapshot,
		&request.ImpactHash,
		&request.ManifestCursor,
		&request.StorageObjectCount,
		&request.StorageDeletedCount,
		&request.StorageFailedCount,
		&request.StorageSkippedSharedCount,
		&request.TemporalWorkflowID,
		&request.IdempotencyKey,
		&request.RequestedBy,
		&request.RequestedAt,
		&request.StartedAt,
		&request.DrainDeadlineAt,
		&request.UpdatedAt,
		&request.CompletedAt,
		&request.ExpiresAt,
		&request.ErrorCode,
		&request.ErrorMessage,
		&request.RetryCount,
	}
}

func projectDeletionDrainDuration() time.Duration {
	duration := config.Duration("CINEWEAVE_PROJECT_DELETION_DRAIN_TIMEOUT", defaultProjectDeletionDrain)
	if duration <= 0 {
		return defaultProjectDeletionDrain
	}
	return duration
}
