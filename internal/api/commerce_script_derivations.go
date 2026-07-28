package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
)

func (s *Server) createCommerceScriptDerivation(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite,
	)
	if !ok {
		return
	}
	if !s.authorize(
		w, r, principal, authz.PermissionWorkflowRun,
		authz.Resource{ProjectID: project.ID},
	) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(
			w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED",
			"创建脚本裂变任务需要请求标识", nil, false,
		)
		return
	}
	var req commercepkg.CreateScriptDerivationInput
	if !decode(w, r, &req) {
		return
	}
	if err := commercepkg.NormalizeScriptDerivationInput(&req); err != nil {
		s.writeError(w, r, err)
		return
	}
	scriptUnitID := strings.TrimSpace(r.PathValue("scriptUnitId"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	repository := commercepkg.NewRepository()
	source, err := repository.LoadScriptUnit(
		r.Context(), tx, project.OrganizationID, project.ID, scriptUnitID, true,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "sourceScriptUnitId": scriptUnitID,
		"sourceContentHash": source.CurrentContentHash, "input": req,
	})
	claim, err := claimIdempotencyTx(
		r.Context(), tx, project.OrganizationID,
		"commerce_script_derivation:create:"+scriptUnitID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.ScriptDerivationBatch
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(
			w, r, http.StatusAccepted, replay,
			map[string]any{"idempotentReplay": true},
		)
		return
	}
	batchID := uuid.NewString()
	workflowRunID := uuid.NewString()
	prepared, err := s.commerceDerivations.PrepareBatch(
		r.Context(), tx, commercepkg.PrepareScriptDerivationParams{
			BatchID: batchID, WorkflowRunID: workflowRunID,
			OrganizationID: project.OrganizationID, ProjectID: project.ID,
			ScriptUnitID: scriptUnitID, CreatedBy: principal.UserID,
			IdempotencyKey: idempotencyKey, RequestHash: requestHash, Input: req,
		},
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := workflows.EnqueueCommerceScriptDerivationBatchTx(
		r.Context(), tx, workflows.CommerceScriptDerivationBatchInput{
			OrganizationID: project.OrganizationID, ProjectID: project.ID,
			BatchID: prepared.Batch.ID, WorkflowRunID: workflowRunID,
			MaxConcurrency: 5,
		},
		prepared.Production, principal.UserID,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := scanWorkflowRun(tx.QueryRow(
		r.Context(), workflowRunSelectSQL(`WHERE id = $1`), workflowRunID,
	))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertWorkflowQueuedEventTx(r.Context(), tx, run, run.WorkflowType); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.script_derivation.batch.created",
		"commerce_script_derivation_batch", prepared.Batch.ID,
		commerceScriptDerivationBatchEventPayload(prepared.Batch),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(
		r.Context(), tx, claim.state, http.StatusAccepted, prepared.Batch,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, prepared.Batch, nil)
}

func (s *Server) listCommerceScriptDerivations(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead,
	)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.commerceDerivations.ListBatches(
		r.Context(), s.db, project.OrganizationID, project.ID,
		r.URL.Query().Get("filter[status]"),
		r.URL.Query().Get("filter[sourceScriptUnitId]"),
		r.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, items, nil)
}

func (s *Server) getCommerceScriptDerivation(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead,
	)
	if !ok {
		return
	}
	item, err := s.commerceDerivations.GetBatch(
		r.Context(), s.db, project.OrganizationID, project.ID,
		r.PathValue("batchId"),
		commerceIncludeRequested(r.URL.Query().Get("include"), "lineage"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) retryCommerceScriptDerivation(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite,
	)
	if !ok {
		return
	}
	if !s.authorize(
		w, r, principal, authz.PermissionWorkflowRun,
		authz.Resource{ProjectID: project.ID},
	) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(
			w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED",
			"重试脚本裂变任务需要请求标识", nil, false,
		)
		return
	}
	var req struct{}
	if !decode(w, r, &req) {
		return
	}
	sourceBatchID := strings.TrimSpace(r.PathValue("batchId"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	source, err := s.commerceDerivations.GetBatch(
		r.Context(), tx, project.OrganizationID, project.ID, sourceBatchID, false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	failedInputHashes := make([]string, 0)
	for _, item := range source.Items {
		if item.Status == "failed_retryable" {
			failedInputHashes = append(failedInputHashes, item.InputHash)
		}
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "sourceBatchId": sourceBatchID,
		"failedInputHashes": failedInputHashes,
	})
	claim, err := claimIdempotencyTx(
		r.Context(), tx, project.OrganizationID,
		"commerce_script_derivation:retry_failed:"+sourceBatchID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.ScriptDerivationBatch
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(
			w, r, http.StatusAccepted, replay,
			map[string]any{"idempotentReplay": true},
		)
		return
	}
	workflowRunID := uuid.NewString()
	prepared, err := s.commerceDerivations.PrepareRetryBatch(
		r.Context(), tx, sourceBatchID, workflowRunID,
		project.OrganizationID, project.ID, principal.UserID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := workflows.EnqueueCommerceScriptDerivationBatchTx(
		r.Context(), tx, workflows.CommerceScriptDerivationBatchInput{
			OrganizationID: project.OrganizationID, ProjectID: project.ID,
			BatchID: prepared.Batch.ID, WorkflowRunID: workflowRunID,
			MaxConcurrency: 5,
		},
		prepared.Production, principal.UserID,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := scanWorkflowRun(tx.QueryRow(
		r.Context(), workflowRunSelectSQL(`WHERE id = $1`), workflowRunID,
	))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertWorkflowQueuedEventTx(r.Context(), tx, run, run.WorkflowType); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.script_derivation.batch.created",
		"commerce_script_derivation_batch", prepared.Batch.ID,
		commerceScriptDerivationBatchEventPayload(prepared.Batch),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(
		r.Context(), tx, claim.state, http.StatusAccepted, prepared.Batch,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, prepared.Batch, nil)
}

func (s *Server) cancelCommerceScriptDerivation(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowCancel,
	)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(
			w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED",
			"取消脚本裂变任务需要请求标识", nil, false,
		)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	batchID := strings.TrimSpace(r.PathValue("batchId"))
	batch, err := s.commerceDerivations.GetBatch(
		r.Context(), s.db, project.OrganizationID, project.ID, batchID, false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if commerceScriptDerivationTerminal(batch.Status) {
		httpx.WriteJSON(w, r, http.StatusOK, batch, nil)
		return
	}
	if batch.WorkflowRunID == nil || strings.TrimSpace(*batch.WorkflowRunID) == "" {
		s.writeError(w, r, commercepkg.Error{
			Code:    commercepkg.CodeScriptDerivationState,
			Message: "脚本裂变任务缺少可取消的工作流",
		})
		return
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "batchId": batchID, "reason": strings.TrimSpace(req.Reason),
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(
		r.Context(), tx, project.OrganizationID,
		"commerce_script_derivation:cancel:"+batchID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		current, loadErr := s.commerceDerivations.GetBatch(
			r.Context(), s.db, project.OrganizationID, project.ID, batchID, false,
		)
		if loadErr != nil {
			s.writeError(w, r, loadErr)
			return
		}
		if !commerceScriptDerivationTerminal(current.Status) {
			if err := s.requestCommerceScriptDerivationCancellation(
				r, project, current, strings.TrimSpace(req.Reason),
			); err != nil {
				s.writeError(w, r, err)
				return
			}
			current, loadErr = s.commerceDerivations.GetBatch(
				r.Context(), s.db, project.OrganizationID, project.ID, batchID, false,
			)
			if loadErr != nil {
				s.writeError(w, r, loadErr)
				return
			}
		}
		httpx.WriteJSON(
			w, r, http.StatusAccepted, current,
			map[string]any{"idempotentReplay": true},
		)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE commerce_script_derivation_batches
		SET status = 'cancelling', revision = revision + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('queued', 'running')
	`, batch.ID, project.OrganizationID, project.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	batch.Status = "cancelling"
	if err := insertAPIEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.script_derivation.batch.cancelling",
		"commerce_script_derivation_batch", batch.ID,
		commerceScriptDerivationBatchEventPayload(batch),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(
		r.Context(), tx, claim.state, http.StatusAccepted, batch,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.requestCommerceScriptDerivationCancellation(
		r, project, batch, strings.TrimSpace(req.Reason),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	current, err := s.commerceDerivations.GetBatch(
		r.Context(), s.db, project.OrganizationID, project.ID, batch.ID, false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, current, nil)
}

func (s *Server) requestCommerceScriptDerivationCancellation(
	r *http.Request,
	project Project,
	batch commercepkg.ScriptDerivationBatch,
	reason string,
) error {
	if batch.WorkflowRunID == nil || strings.TrimSpace(*batch.WorkflowRunID) == "" {
		return commercepkg.Error{
			Code:    commercepkg.CodeScriptDerivationState,
			Message: "脚本裂变任务缺少可取消的工作流",
		}
	}
	run, err := scanWorkflowRun(s.db.QueryRow(
		r.Context(), workflowRunSelectSQL(`
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
		`), *batch.WorkflowRunID, project.OrganizationID, project.ID,
	))
	if err != nil {
		return err
	}
	if reason == "" {
		reason = "用户取消脚本裂变任务"
	}
	_, err = s.cancelWorkflowRunItem(r.Context(), run, reason)
	return err
}

func commerceScriptDerivationTerminal(status string) bool {
	switch status {
	case "succeeded", "partial_succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func commerceScriptDerivationBatchEventPayload(
	batch commercepkg.ScriptDerivationBatch,
) json.RawMessage {
	return mustRawJSON(map[string]any{
		"batchId": batch.ID, "sourceScriptUnitId": batch.SourceScriptUnitID,
		"workflowRunId": batch.WorkflowRunID, "status": batch.Status,
		"requestedCount": batch.RequestedCount, "queuedCount": batch.QueuedCount,
		"runningCount": batch.RunningCount, "succeededCount": batch.SucceededCount,
		"failedRetryableCount": batch.FailedRetryableCount,
		"failedTerminalCount":  batch.FailedTerminalCount,
		"cancelledCount":       batch.CancelledCount,
		"rootBatchId":          batch.RootBatchID, "retryOfBatchId": batch.RetryOfBatchID,
	})
}
