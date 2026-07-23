package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const commerceScriptUnitBatchCoordinatorHandler = "commerce_script_unit_batch_coordinator"

type commerceScriptUnitBatchAdvanceItem struct {
	ScriptUnitID             string   `json:"scriptUnitId"`
	ExpectedUnitGenerationID string   `json:"expectedUnitGenerationId"`
	PlanID                   string   `json:"planId,omitempty"`
	ExpectedPlanRevision     int64    `json:"expectedPlanRevision,omitempty"`
	TimelineID               string   `json:"timelineId,omitempty"`
	ExpectedTimelineRevision int64    `json:"expectedTimelineRevision,omitempty"`
	ShotIDs                  []string `json:"shotIds,omitempty"`
	Force                    bool     `json:"force,omitempty"`
	Resolution               string   `json:"resolution,omitempty"`
	Title                    string   `json:"title,omitempty"`
	AttemptGeneration        int      `json:"attemptGeneration,omitempty"`
}

type commerceScriptUnitBatchAdvanceRequest struct {
	TargetStage     string                               `json:"targetStage"`
	Items           []commerceScriptUnitBatchAdvanceItem `json:"items"`
	UnitConcurrency int                                  `json:"unitConcurrency"`
	MaxConcurrency  int                                  `json:"maxConcurrency"`
}

type commerceScriptUnitBatchRetryRequest struct {
	ScriptUnitIDs  []string `json:"scriptUnitIds"`
	MaxConcurrency int      `json:"maxConcurrency"`
}

type commerceScriptUnitBatchCancelRequest struct {
	Reason string `json:"reason"`
}

type commerceScriptUnitBatchCoordinator struct {
	ID                   string                                   `json:"id"`
	OrganizationID       string                                   `json:"organizationId"`
	ProjectID            string                                   `json:"projectId"`
	ProjectGenerationID  string                                   `json:"projectGenerationId"`
	TargetStage          string                                   `json:"targetStage"`
	Status               string                                   `json:"status"`
	MaxConcurrency       int                                      `json:"maxConcurrency"`
	RetryOfCoordinatorID string                                   `json:"retryOfCoordinatorId,omitempty"`
	WorkflowRunID        string                                   `json:"workflowRunId"`
	TotalItems           int                                      `json:"totalItems"`
	CompletedItems       int                                      `json:"completedItems"`
	FailedItems          int                                      `json:"failedItems"`
	CancelledItems       int                                      `json:"cancelledItems"`
	Revision             int64                                    `json:"revision"`
	CreatedAt            time.Time                                `json:"createdAt"`
	StartedAt            *time.Time                               `json:"startedAt,omitempty"`
	CompletedAt          *time.Time                               `json:"completedAt,omitempty"`
	CancelledAt          *time.Time                               `json:"cancelledAt,omitempty"`
	ErrorCode            string                                   `json:"errorCode,omitempty"`
	ErrorMessage         string                                   `json:"errorMessage,omitempty"`
	Items                []commerceScriptUnitBatchCoordinatorItem `json:"items"`
}

type commerceScriptUnitBatchCoordinatorItem struct {
	ID                 string          `json:"id"`
	ScriptUnitID       string          `json:"scriptUnitId"`
	UnitGenerationID   string          `json:"unitGenerationId"`
	ChildRunID         string          `json:"childRunId,omitempty"`
	ChildWorkflowRunID string          `json:"childWorkflowRunId,omitempty"`
	Ordinal            int             `json:"ordinal"`
	Status             string          `json:"status"`
	AttemptGeneration  int             `json:"attemptGeneration"`
	InputSnapshot      json.RawMessage `json:"inputSnapshot"`
	ErrorCode          string          `json:"errorCode,omitempty"`
	ErrorMessage       string          `json:"errorMessage,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
}

type commercePreparedBatchChild struct {
	workflow workflows.CommerceScriptUnitBatchChild
}

func (s *Server) createCommerceScriptUnitBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req commerceScriptUnitBatchAdvanceRequest
	if !decode(w, r, &req) {
		return
	}
	if err := normalizeCommerceScriptUnitBatchAdvanceRequest(&req); err != nil {
		s.writeError(w, r, err)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "跨脚本批量任务需要请求标识", nil, false)
		return
	}
	response, replayed, err := s.createCommerceScriptUnitBatchWithIdempotency(
		r.Context(), project, principal.UserID, req,
		"commerce_script_unit_batch:"+req.TargetStage, idempotencyKey, "",
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, response, map[string]any{"idempotentReplay": replayed})
}

func (s *Server) getCommerceScriptUnitBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	item, err := getCommerceScriptUnitBatchCoordinator(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("coordinatorId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listCommerceScriptUnitBatches(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text
		FROM commerce_script_unit_batch_coordinators
		WHERE organization_id = $1 AND project_id = $2
		ORDER BY created_at DESC
		LIMIT 50
	`, project.OrganizationID, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			s.writeError(w, r, err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	items := make([]commerceScriptUnitBatchCoordinator, 0, len(ids))
	for _, id := range ids {
		item, err := getCommerceScriptUnitBatchCoordinator(r.Context(), s.db, project.OrganizationID, project.ID, id)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) retryCommerceScriptUnitBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req commerceScriptUnitBatchRetryRequest
	if !decode(w, r, &req) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "重试跨脚本批量任务需要请求标识", nil, false)
		return
	}
	original, err := getCommerceScriptUnitBatchCoordinator(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("coordinatorId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if original.Status != "failed" && original.Status != "partially_succeeded" {
		s.writeError(w, r, apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_NOT_RETRYABLE", Message: "当前跨脚本批量任务没有可重试失败项"})
		return
	}
	selected := make(map[string]struct{}, len(req.ScriptUnitIDs))
	for _, raw := range req.ScriptUnitIDs {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			s.writeError(w, r, apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_SELECTION_INVALID", Message: "重试脚本单元标识无效"})
			return
		}
		selected[id] = struct{}{}
	}
	advance := commerceScriptUnitBatchAdvanceRequest{TargetStage: original.TargetStage, MaxConcurrency: req.MaxConcurrency}
	for _, item := range original.Items {
		if item.Status != "failed" {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[item.ScriptUnitID]; !ok {
				continue
			}
		}
		var snapshot commerceScriptUnitBatchAdvanceItem
		if err := json.Unmarshal(item.InputSnapshot, &snapshot); err != nil {
			s.writeError(w, r, apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_SNAPSHOT_INVALID", Message: "失败项冻结参数无法读取"})
			return
		}
		snapshot.AttemptGeneration = item.AttemptGeneration + 1
		advance.Items = append(advance.Items, snapshot)
	}
	if len(advance.Items) == 0 {
		s.writeError(w, r, apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_NOT_RETRYABLE", Message: "没有符合条件的失败脚本单元"})
		return
	}
	if advance.MaxConcurrency < 1 {
		advance.MaxConcurrency = original.MaxConcurrency
	}
	if err := normalizeCommerceScriptUnitBatchAdvanceRequest(&advance); err != nil {
		s.writeError(w, r, err)
		return
	}
	response, replayed, err := s.createCommerceScriptUnitBatchWithIdempotency(
		r.Context(), project, principal.UserID, advance,
		"commerce_script_unit_batch_retry:"+original.ID, idempotencyKey, original.ID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, response, map[string]any{"idempotentReplay": replayed})
}

func (s *Server) cancelCommerceScriptUnitBatch(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowCancel)
	if !ok {
		return
	}
	var req commerceScriptUnitBatchCancelRequest
	if !decode(w, r, &req) {
		return
	}
	coordinator, err := getCommerceScriptUnitBatchCoordinator(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("coordinatorId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if coordinator.Status == "succeeded" || coordinator.Status == "partially_succeeded" || coordinator.Status == "failed" || coordinator.Status == "cancelled" {
		httpx.WriteJSON(w, r, http.StatusOK, coordinator, nil)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "用户取消了跨脚本批量任务"
	}
	workflowRun, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), coordinator.WorkflowRunID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updatedWorkflowRun, err := s.cancelWorkflowRunItem(r.Context(), workflowRun, reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if updatedWorkflowRun.Status == "cancelled" {
		if err := s.finalizePreStartCommerceBatchCancellation(r.Context(), coordinator, reason); err != nil {
			s.writeError(w, r, err)
			return
		}
	} else if _, err := s.db.Exec(r.Context(), `
		UPDATE commerce_script_unit_batch_coordinators
		SET status = 'cancelling', error_code = 'USER_CANCELLED', error_message = $4,
		    revision = revision + 1
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('queued', 'running')
	`, coordinator.ID, project.OrganizationID, project.ID, reason); err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := getCommerceScriptUnitBatchCoordinator(r.Context(), s.db, project.OrganizationID, project.ID, coordinator.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated, nil)
}

func (s *Server) finalizePreStartCommerceBatchCancellation(ctx context.Context, coordinator commerceScriptUnitBatchCoordinator, reason string) error {
	for _, item := range coordinator.Items {
		if item.ChildWorkflowRunID == "" || item.Status == "succeeded" || item.Status == "failed" || item.Status == "cancelled" || item.Status == "skipped" {
			continue
		}
		if err := workflows.CancelWorkflowRun(ctx, s.db, item.ChildWorkflowRunID, json.RawMessage(`{}`), reason); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, item := range coordinator.Items {
		if item.ChildRunID == "" || item.Status == "succeeded" || item.Status == "failed" || item.Status == "cancelled" || item.Status == "skipped" {
			continue
		}
		if _, err := s.commerceCatalog.CancelProductionRun(ctx, tx, coordinator.OrganizationID, coordinator.ProjectID, item.ChildRunID, reason); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_items
		SET status = 'cancelled', error_code = 'USER_CANCELLED', error_message = $2,
		    completed_at = COALESCE(completed_at, now())
		WHERE coordinator_id = $1 AND status IN ('queued', 'running')
	`, coordinator.ID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_coordinators
		SET status = 'cancelled', completed_items = 0, failed_items = 0,
		    cancelled_items = total_items, completed_at = COALESCE(completed_at, now()),
		    cancelled_at = COALESCE(cancelled_at, now()), error_code = 'USER_CANCELLED',
		    error_message = $2, revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'cancelling')
	`, coordinator.ID, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) createCommerceScriptUnitBatchWithIdempotency(
	ctx context.Context,
	project Project,
	createdBy string,
	req commerceScriptUnitBatchAdvanceRequest,
	idempotencyScope string,
	idempotencyKey string,
	retryOfCoordinatorID string,
) (commerceScriptUnitBatchCoordinator, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, false, err
	}
	defer tx.Rollback(ctx)
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "targetStage": req.TargetStage, "items": req.Items,
		"unitConcurrency": req.UnitConcurrency, "maxConcurrency": req.MaxConcurrency,
		"retryOfCoordinatorId": retryOfCoordinatorID,
	})
	claim, err := claimIdempotencyTx(ctx, tx, project.OrganizationID, idempotencyScope, idempotencyKey, requestHash)
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, false, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commerceScriptUnitBatchCoordinator
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return commerceScriptUnitBatchCoordinator{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return commerceScriptUnitBatchCoordinator{}, false, err
		}
		current, err := getCommerceScriptUnitBatchCoordinator(ctx, s.db, project.OrganizationID, project.ID, replay.ID)
		return current, true, err
	}
	response, err := s.createCommerceScriptUnitBatchTx(ctx, tx, project, createdBy, req, idempotencyScope, idempotencyKey, requestHash, retryOfCoordinatorID)
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, false, err
	}
	if err := completeIdempotencyTxWithStatus(ctx, tx, claim.state, http.StatusAccepted, response); err != nil {
		return commerceScriptUnitBatchCoordinator{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commerceScriptUnitBatchCoordinator{}, false, err
	}
	return response, false, nil
}

func (s *Server) createCommerceScriptUnitBatchTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	createdBy string,
	req commerceScriptUnitBatchAdvanceRequest,
	idempotencyScope string,
	idempotencyKey string,
	payloadHash string,
	retryOfCoordinatorID string,
) (commerceScriptUnitBatchCoordinator, error) {
	firstIdentity, err := s.commerceCatalog.LockActiveStoryboardGeneration(ctx, tx, project.OrganizationID, project.ID, req.Items[0].ScriptUnitID)
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	if firstIdentity.UnitGenerationID != req.Items[0].ExpectedUnitGenerationID {
		return commerceScriptUnitBatchCoordinator{}, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试"}
	}
	coordinatorID := uuid.NewString()
	workflowRunID := uuid.NewString()
	temporalWorkflowID := "commerce-script-unit-batch-" + coordinatorID
	requestSnapshot := mustRawJSON(map[string]any{
		"targetStage": req.TargetStage, "items": req.Items,
		"unitConcurrency": req.UnitConcurrency, "maxConcurrency": req.MaxConcurrency,
		"retryOfCoordinatorId": retryOfCoordinatorID,
	})
	placeholderInput := workflows.CommerceScriptUnitBatchCoordinatorInput{
		CoordinatorID: coordinatorID, WorkflowRunID: workflowRunID,
		OrganizationID: firstIdentity.OrganizationID, ProjectID: firstIdentity.ProjectID,
		ProjectGenerationID: firstIdentity.ProjectGenerationID, TargetStage: req.TargetStage,
		MaxConcurrency: req.MaxConcurrency, RequestedBy: createdBy,
	}
	placeholderRaw, _, err := marshalWorkflowStartInput(placeholderInput)
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			attempt_generation, total_items
		)
		VALUES ($1, $2, $3, $4, 'commerce_script_unit_batch_coordinator',
		        'queued', $5, '{}', $6, $7, $8, $9, 1, $10)
	`, workflowRunID, firstIdentity.OrganizationID, firstIdentity.ProjectID, temporalWorkflowID,
		placeholderRaw, createdBy, firstIdentity.ProjectGenerationID,
		firstIdentity.VideoProductionBindingID, firstIdentity.VideoProductionBindingRevision, len(req.Items)); err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_unit_batch_coordinators(
			id, organization_id, project_id, project_production_generation_id,
			target_stage, status, idempotency_scope, idempotency_key, payload_hash,
			input_snapshot, max_concurrency, retry_of_coordinator_id, workflow_run_id,
			total_items, requested_by
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, $7, $8, $9, $10,
		        NULLIF($11, '')::uuid, $12, $13, $14)
	`, coordinatorID, project.OrganizationID, project.ID, firstIdentity.ProjectGenerationID,
		req.TargetStage, idempotencyScope, idempotencyKey, payloadHash, requestSnapshot,
		req.MaxConcurrency, retryOfCoordinatorID, workflowRunID, len(req.Items), createdBy); err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	children := make([]workflows.CommerceScriptUnitBatchChild, 0, len(req.Items))
	for ordinal, item := range req.Items {
		identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(ctx, tx, project.OrganizationID, project.ID, item.ScriptUnitID)
		if err != nil {
			return commerceScriptUnitBatchCoordinator{}, err
		}
		if identity.UnitGenerationID != item.ExpectedUnitGenerationID || identity.ProjectGenerationID != firstIdentity.ProjectGenerationID ||
			identity.VideoProductionBindingID != firstIdentity.VideoProductionBindingID || identity.VideoProductionBindingRevision != firstIdentity.VideoProductionBindingRevision {
			return commerceScriptUnitBatchCoordinator{}, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "批量脚本单元不属于同一活动生产代"}
		}
		itemID := uuid.NewString()
		if item.AttemptGeneration < 1 {
			item.AttemptGeneration = 1
		}
		itemSnapshot := mustRawJSON(item)
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_script_unit_batch_items(
				id, organization_id, project_id, coordinator_id, script_unit_id,
				script_unit_generation_id, input_snapshot, attempt_generation, ordinal, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'queued')
		`, itemID, project.OrganizationID, project.ID, coordinatorID, item.ScriptUnitID,
			item.ExpectedUnitGenerationID, itemSnapshot, item.AttemptGeneration, ordinal); err != nil {
			return commerceScriptUnitBatchCoordinator{}, err
		}
		prepared, err := s.prepareCommerceScriptUnitBatchChildTx(ctx, tx, project, createdBy, workflowRunID, coordinatorID, itemID, req.TargetStage, req.UnitConcurrency, item)
		if err != nil {
			return commerceScriptUnitBatchCoordinator{}, err
		}
		children = append(children, prepared.workflow)
	}
	workflowInput := placeholderInput
	workflowInput.Children = children
	workflowRaw, _, err := marshalWorkflowStartInput(workflowInput)
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_runs SET input = $2 WHERE id = $1 AND status = 'queued'`, workflowRunID, workflowRaw); err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	if err := s.enqueueWorkflowStartTx(ctx, tx, workflowRunID, "", project.OrganizationID, project.ID,
		firstIdentity.ProjectGenerationID, "commerce_script_unit_batch_coordinator",
		commerceScriptUnitBatchCoordinatorHandler, temporalWorkflowID, workflows.ScriptTaskQueue, workflowInput); err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	workflowRun, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), workflowRunID))
	if err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, workflowRun, workflowRun.WorkflowType); err != nil {
		return commerceScriptUnitBatchCoordinator{}, err
	}
	return getCommerceScriptUnitBatchCoordinator(ctx, tx, project.OrganizationID, project.ID, coordinatorID)
}

func (s *Server) prepareCommerceScriptUnitBatchChildTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	createdBy string,
	parentWorkflowRunID string,
	coordinatorID string,
	coordinatorItemID string,
	targetStage string,
	unitConcurrency int,
	item commerceScriptUnitBatchAdvanceItem,
) (commercePreparedBatchChild, error) {
	prepared := commercePreparedBatchChild{}
	var childRunID, childWorkflowRunID, workflowName string
	childScope := "commerce_batch_child:" + coordinatorID + ":" + targetStage
	childKey := coordinatorItemID
	switch targetStage {
	case "storyboard":
		if err := ensureNoActiveCommerceUnitWorkflowTx(ctx, tx, item.ExpectedUnitGenerationID, "commerce_storyboard_planning"); err != nil {
			return prepared, err
		}
		identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(ctx, tx, project.OrganizationID, project.ID, item.ScriptUnitID)
		if err != nil {
			return prepared, err
		}
		childWorkflowRunID, err = workflows.EnqueueCommerceStoryboardPlanningTx(ctx, tx, identity, createdBy, parentWorkflowRunID)
		if err != nil {
			return prepared, err
		}
		workflowName = workflows.CommerceStoryboardPlanningWorkflowName
	case "reference_images":
		if err := ensureNoActiveCommerceUnitWorkflowTx(ctx, tx, item.ExpectedUnitGenerationID, "commerce_reference_images"); err != nil {
			return prepared, err
		}
		req := commerceReferenceImageBatchRequest{
			Operation: "generate_images", PlanID: item.PlanID,
			ExpectedPlanRevision: item.ExpectedPlanRevision, ExpectedUnitGenerationID: item.ExpectedUnitGenerationID,
			ShotIDs: item.ShotIDs, Force: item.Force, Concurrency: unitConcurrency,
		}
		run, created, err := s.createCommerceReferenceImageRunTx(ctx, tx, project, createdBy, item.ScriptUnitID, req, childScope, childKey, "")
		if err != nil {
			return prepared, err
		}
		if !created {
			return prepared, apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_CHILD_ALREADY_EXISTS", Message: "脚本单元参考图任务已存在"}
		}
		childRunID, childWorkflowRunID = run.ID, run.WorkflowRunID
		workflowName = workflows.CommerceReferenceImageBatchWorkflowName
	case "video_prompts", "shot_videos":
		operation := "generate_prompts"
		workflowType := "commerce_video_prompts"
		workflowName = workflows.CommerceVideoPromptBatchWorkflowName
		if targetStage == "shot_videos" {
			operation = "generate_videos"
			workflowType = "commerce_shot_videos"
			workflowName = workflows.CommerceShotVideoBatchWorkflowName
		}
		if err := ensureNoActiveCommerceUnitWorkflowTx(ctx, tx, item.ExpectedUnitGenerationID, workflowType); err != nil {
			return prepared, err
		}
		req := commerceVideoBatchRequest{
			PlanID: item.PlanID, ExpectedPlanRevision: item.ExpectedPlanRevision,
			ExpectedUnitGenerationID: item.ExpectedUnitGenerationID, ShotIDs: item.ShotIDs,
			Force: item.Force, Concurrency: unitConcurrency, Resolution: item.Resolution,
		}
		run, created, err := s.createCommerceVideoRunTx(ctx, tx, project, createdBy, item.ScriptUnitID, operation, req, childScope, childKey, "")
		if err != nil {
			return prepared, err
		}
		if !created {
			return prepared, apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_CHILD_ALREADY_EXISTS", Message: "脚本单元视频任务已存在"}
		}
		childRunID, childWorkflowRunID = run.ID, run.WorkflowRunID
	case "final_compose":
		if err := ensureNoActiveCommerceUnitWorkflowTx(ctx, tx, item.ExpectedUnitGenerationID, "commerce_final_compose"); err != nil {
			return prepared, err
		}
		req := commerceFinalComposeRequest{
			TimelineID: item.TimelineID, ExpectedTimelineRevision: item.ExpectedTimelineRevision,
			ExpectedUnitGenerationID: item.ExpectedUnitGenerationID, Title: item.Title, Resolution: item.Resolution,
		}
		run, created, err := s.createCommerceFinalComposeRunTx(ctx, tx, project, createdBy, item.ScriptUnitID, req, childScope, childKey, "")
		if err != nil {
			return prepared, err
		}
		if !created {
			return prepared, apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_CHILD_ALREADY_EXISTS", Message: "脚本单元成片任务已存在"}
		}
		childRunID, childWorkflowRunID = run.ID, run.WorkflowRunID
		workflowName = workflows.CommerceFinalComposeWorkflowName
	default:
		return prepared, apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_STAGE_INVALID", Message: "跨脚本批量目标阶段无效"}
	}
	if childRunID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_production_runs
			SET coordinator_item_id = $2
			WHERE id = $1 AND coordinator_item_id IS NULL
		`, childRunID, coordinatorItemID); err != nil {
			return prepared, err
		}
	}
	if err := markCommerceChildWorkflowParentControlledTx(ctx, tx, childWorkflowRunID); err != nil {
		return prepared, err
	}
	var temporalWorkflowID string
	var workflowInput json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT temporal_workflow_id, input
		FROM workflow_runs
		WHERE id = $1
	`, childWorkflowRunID).Scan(&temporalWorkflowID, &workflowInput); err != nil {
		return prepared, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_items
		SET child_run_id = NULLIF($2, '')::uuid, child_workflow_run_id = $3
		WHERE id = $1
	`, coordinatorItemID, childRunID, childWorkflowRunID); err != nil {
		return prepared, err
	}
	prepared.workflow = workflows.CommerceScriptUnitBatchChild{
		CoordinatorItemID: coordinatorItemID, ScriptUnitID: item.ScriptUnitID,
		UnitGenerationID: item.ExpectedUnitGenerationID, WorkflowRunID: childWorkflowRunID,
		TemporalWorkflowID: temporalWorkflowID, WorkflowName: workflowName,
		WorkflowInput: workflowInput, ProductionRunID: childRunID,
	}
	return prepared, nil
}

func markCommerceChildWorkflowParentControlledTx(ctx context.Context, tx pgx.Tx, workflowRunID string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_start_outbox
		SET status = 'started', attempt_count = 1, max_attempts = 1,
		    started_at = COALESCE(started_at, now()), completed_at = COALESCE(completed_at, now()),
		    next_attempt_at = now(), locked_at = NULL, locked_by = NULL,
		    last_error_code = NULL, last_error_message = NULL, updated_at = now()
		WHERE workflow_run_id = $1 AND status = 'pending'
	`, workflowRunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_CHILD_START_CONFLICT", Message: "脚本单元工作流已被其他调度器接管"}
	}
	return nil
}

func ensureNoActiveCommerceUnitWorkflowTx(ctx context.Context, tx pgx.Tx, unitGenerationID, workflowType string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM workflow_runs
			WHERE workflow_type = $2
			  AND input->'identity'->>'scriptUnitGenerationId' = $1
			  AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
		)
	`, unitGenerationID, workflowType).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return apiError{Status: http.StatusConflict, Code: "COMMERCE_BATCH_CHILD_ACTIVE", Message: "所选脚本单元已有同阶段任务正在运行"}
	}
	return nil
}

func normalizeCommerceScriptUnitBatchAdvanceRequest(req *commerceScriptUnitBatchAdvanceRequest) error {
	req.TargetStage = strings.TrimSpace(req.TargetStage)
	switch req.TargetStage {
	case "storyboard", "reference_images", "video_prompts", "shot_videos", "final_compose":
	default:
		return apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_STAGE_INVALID", Message: "跨脚本批量目标阶段无效"}
	}
	if len(req.Items) == 0 || len(req.Items) > 200 {
		return apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_SELECTION_INVALID", Message: "请选择 1 至 200 个脚本单元"}
	}
	if req.UnitConcurrency < 1 {
		req.UnitConcurrency = 5
	}
	if req.UnitConcurrency > 16 {
		req.UnitConcurrency = 16
	}
	if req.MaxConcurrency < 1 {
		req.MaxConcurrency = 4
	}
	if req.MaxConcurrency > 16 {
		req.MaxConcurrency = 16
	}
	seen := make(map[string]struct{}, len(req.Items))
	for index := range req.Items {
		item := &req.Items[index]
		item.ScriptUnitID = strings.TrimSpace(item.ScriptUnitID)
		item.ExpectedUnitGenerationID = strings.TrimSpace(item.ExpectedUnitGenerationID)
		item.PlanID = strings.TrimSpace(item.PlanID)
		item.TimelineID = strings.TrimSpace(item.TimelineID)
		item.Resolution = strings.ToLower(strings.TrimSpace(item.Resolution))
		item.Title = strings.TrimSpace(item.Title)
		if _, err := uuid.Parse(item.ScriptUnitID); err != nil {
			return apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_SELECTION_INVALID", Message: "脚本单元标识无效"}
		}
		if _, err := uuid.Parse(item.ExpectedUnitGenerationID); err != nil {
			return commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代标识无效"}
		}
		if _, exists := seen[item.ScriptUnitID]; exists {
			return apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_SELECTION_INVALID", Message: "同一脚本单元不能重复选择"}
		}
		seen[item.ScriptUnitID] = struct{}{}
		if item.AttemptGeneration < 1 {
			item.AttemptGeneration = 1
		}
		shotIDs, err := normalizedUniqueUUIDs(item.ShotIDs)
		if err != nil {
			return err
		}
		item.ShotIDs = shotIDs
		switch req.TargetStage {
		case "storyboard":
		case "reference_images":
			child := commerceReferenceImageBatchRequest{Operation: "generate_images", PlanID: item.PlanID,
				ExpectedPlanRevision: item.ExpectedPlanRevision, ExpectedUnitGenerationID: item.ExpectedUnitGenerationID,
				ShotIDs: item.ShotIDs, Force: item.Force, Concurrency: req.UnitConcurrency}
			if err := normalizeCommerceReferenceImageBatchRequest(&child); err != nil {
				return err
			}
		case "video_prompts", "shot_videos":
			child := commerceVideoBatchRequest{PlanID: item.PlanID, ExpectedPlanRevision: item.ExpectedPlanRevision,
				ExpectedUnitGenerationID: item.ExpectedUnitGenerationID, ShotIDs: item.ShotIDs,
				Force: item.Force, Concurrency: req.UnitConcurrency, Resolution: item.Resolution}
			if err := normalizeCommerceVideoBatchRequest(&child); err != nil {
				return err
			}
			item.Resolution = child.Resolution
		case "final_compose":
			child := commerceFinalComposeRequest{TimelineID: item.TimelineID, ExpectedTimelineRevision: item.ExpectedTimelineRevision,
				ExpectedUnitGenerationID: item.ExpectedUnitGenerationID, Title: item.Title, Resolution: item.Resolution}
			if err := normalizeCommerceFinalComposeRequest(&child); err != nil {
				return err
			}
			item.Resolution = child.Resolution
		}
	}
	return nil
}

func normalizedUniqueUUIDs(values []string) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if _, err := uuid.Parse(value); err != nil {
			return nil, apiError{Status: http.StatusUnprocessableEntity, Code: "COMMERCE_BATCH_SELECTION_INVALID", Message: "批量镜头标识无效"}
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

type commerceBatchQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func getCommerceScriptUnitBatchCoordinator(ctx context.Context, db commerceBatchQuerier, organizationID, projectID, coordinatorID string) (commerceScriptUnitBatchCoordinator, error) {
	var item commerceScriptUnitBatchCoordinator
	if err := db.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text,
		       project_production_generation_id::text, target_stage, status,
		       max_concurrency, COALESCE(retry_of_coordinator_id::text, ''),
		       COALESCE(workflow_run_id::text, ''), total_items, completed_items,
		       failed_items, cancelled_items, revision, created_at, started_at,
		       completed_at, cancelled_at, COALESCE(error_code, ''), COALESCE(error_message, '')
		FROM commerce_script_unit_batch_coordinators
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`, coordinatorID, organizationID, projectID).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProjectGenerationID,
		&item.TargetStage, &item.Status, &item.MaxConcurrency, &item.RetryOfCoordinatorID,
		&item.WorkflowRunID, &item.TotalItems, &item.CompletedItems, &item.FailedItems,
		&item.CancelledItems, &item.Revision, &item.CreatedAt, &item.StartedAt,
		&item.CompletedAt, &item.CancelledAt, &item.ErrorCode, &item.ErrorMessage,
	); err != nil {
		return item, err
	}
	rows, err := db.Query(ctx, `
		SELECT id::text, script_unit_id::text, script_unit_generation_id::text,
		       COALESCE(child_run_id::text, ''), COALESCE(child_workflow_run_id::text, ''),
		       ordinal, status, attempt_generation, input_snapshot,
		       COALESCE(error_code, ''), COALESCE(error_message, ''), created_at, completed_at
		FROM commerce_script_unit_batch_items
		WHERE coordinator_id = $1
		ORDER BY ordinal
	`, item.ID)
	if err != nil {
		return item, err
	}
	defer rows.Close()
	item.Items = make([]commerceScriptUnitBatchCoordinatorItem, 0)
	for rows.Next() {
		var child commerceScriptUnitBatchCoordinatorItem
		if err := rows.Scan(&child.ID, &child.ScriptUnitID, &child.UnitGenerationID,
			&child.ChildRunID, &child.ChildWorkflowRunID, &child.Ordinal, &child.Status,
			&child.AttemptGeneration, &child.InputSnapshot, &child.ErrorCode,
			&child.ErrorMessage, &child.CreatedAt, &child.CompletedAt); err != nil {
			return item, err
		}
		item.Items = append(item.Items, child)
	}
	return item, rows.Err()
}
