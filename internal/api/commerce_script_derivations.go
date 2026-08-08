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
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var req commercepkg.CreateScriptDerivationInput
	if !decode(w, r, &req) {
		return
	}
	input := map[string]any{
		"sourceScriptUnitId": strings.TrimSpace(r.PathValue("scriptUnitId")),
		"dimension":          req.Dimension, "instruction": req.Instruction,
		"preserve": req.Preserve, "variations": req.Variations,
	}
	result, err := s.executeManualAsyncAction(
		r.Context(), principal, project, "commerce.script.derive.batch", input, idempotencyKey,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeManualAsyncActionResult(w, r, result)
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
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var req struct{}
	if !decode(w, r, &req) {
		return
	}
	result, err := s.executeManualAsyncAction(
		r.Context(), principal, project, "commerce.script.derive.retry_failed",
		map[string]any{"batchId": strings.TrimSpace(r.PathValue("batchId"))},
		idempotencyKey,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeManualAsyncActionResult(w, r, result)
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
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	result, err := s.executeManualAsyncAction(
		r.Context(), principal, project, "commerce.script.derive.cancel",
		map[string]any{
			"batchId": strings.TrimSpace(r.PathValue("batchId")),
			"reason":  strings.TrimSpace(req.Reason),
		},
		idempotencyKey,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeManualAsyncActionResult(w, r, result)
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
