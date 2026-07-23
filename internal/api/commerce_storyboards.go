package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func (s *Server) listCommerceStoryboardPlans(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	items, err := s.commerceCatalog.ListStoryboardPlans(
		r.Context(), s.db, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.URL.Query().Get("filter[status]"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createCommerceStoryboardPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	if s.temporal == nil {
		s.writeError(w, r, apiError{Status: http.StatusServiceUnavailable, Code: "TEMPORAL_UNAVAILABLE", Message: "工作流服务暂不可用", Retryable: true})
		return
	}
	var req struct {
		ExpectedUnitGenerationID string `json:"expectedUnitGenerationId"`
	}
	if !decode(w, r, &req) {
		return
	}
	idempotency := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotency == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "生成分镜方案需要请求标识", nil, false)
		return
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "scriptUnitId": r.PathValue("scriptUnitId"),
		"expectedUnitGenerationId": strings.TrimSpace(req.ExpectedUnitGenerationID),
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID,
		"commerce_storyboard_plan:create:"+r.PathValue("scriptUnitId"), idempotency, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var response WorkflowRun
		if err := json.Unmarshal(claim.replaySnapshot, &response); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, response, map[string]any{"idempotentReplay": true})
		return
	}
	identity, err := s.commerceCatalog.LockActiveStoryboardGeneration(
		r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if expected := strings.TrimSpace(req.ExpectedUnitGenerationID); expected != "" && expected != identity.UnitGenerationID {
		s.writeError(w, r, commercepkg.Error{Code: commercepkg.CodeGenerationMismatch, Message: "脚本单元生产代已变化，请刷新后重试"})
		return
	}
	runID, err := workflows.EnqueueCommerceStoryboardPlanningTx(r.Context(), tx, identity, principal.UserID, "")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := scanWorkflowRun(tx.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1`), runID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertWorkflowQueuedEventTx(r.Context(), tx, run, run.WorkflowType); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.storyboard.plan.started", "workflow_run", run.ID, mustRawJSON(map[string]any{
			"workflowRunId":                 run.ID,
			"commerceScriptUnitId":          identity.ScriptUnitID,
			"scriptUnitGenerationId":        identity.UnitGenerationID,
			"projectProductionGenerationId": identity.ProjectGenerationID,
			"status":                        "queued",
		})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusAccepted, run); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) getCommerceStoryboardPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetStoryboardPlan(
		r.Context(), s.db, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("planId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceStoryboardPreviews(r, &item)
	w.Header().Set("ETag", commerceStoryboardETag(item.Plan.ID, item.Plan.EditRevision))
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listCommerceStoryboardShots(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetStoryboardPlan(
		r.Context(), s.db, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("planId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceStoryboardPreviews(r, &item)
	w.Header().Set("ETag", commerceStoryboardETag(item.Plan.ID, item.Plan.EditRevision))
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) activateCommerceStoryboardPlan(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.ActivateStoryboardPlan(
		r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("planId"), req.ExpectedRevision,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceStoryboardPlanEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.storyboard.plan.activated", item.Plan,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceStoryboardPreviews(r, &item)
	w.Header().Set("ETag", commerceStoryboardETag(item.Plan.ID, item.Plan.EditRevision))
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateCommerceStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	var req commercepkg.UpdateStoryboardShotInput
	if !decode(w, r, &req) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.UpdateStoryboardShot(
		r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("shotId"), principal.UserID, req,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceShotEvent(
		r.Context(), tx, project.OrganizationID, project.ID, item.Plan,
		r.PathValue("shotId"), "updated",
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceStoryboardPreviews(r, &item)
	w.Header().Set("ETag", commerceStoryboardETag(item.Plan.ID, item.Plan.EditRevision))
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteCommerceStoryboardShot(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	expectedRevision, ok := parseCommerceStoryboardIfMatch(r.Header.Get("If-Match"))
	if !ok {
		httpx.WriteError(w, r, http.StatusPreconditionRequired, "IF_MATCH_REQUIRED", "删除镜头需要当前分镜方案版本", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.ArchiveStoryboardShot(
		r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"),
		r.PathValue("shotId"), principal.UserID, expectedRevision, 0,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceShotEvent(
		r.Context(), tx, project.OrganizationID, project.ID, item.Plan,
		r.PathValue("shotId"), "archived",
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceStoryboardPreviews(r, &item)
	w.Header().Set("ETag", commerceStoryboardETag(item.Plan.ID, item.Plan.EditRevision))
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) reorderCommerceStoryboardShots(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	var req struct {
		PlanID string `json:"planId"`
		commercepkg.ReorderStoryboardShotsInput
	}
	if !decode(w, r, &req) {
		return
	}
	req.PlanID = strings.TrimSpace(req.PlanID)
	if req.PlanID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "planId 不能为空", nil, false)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := s.commerceCatalog.ReorderStoryboardShots(
		r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"),
		req.PlanID, principal.UserID, req.ReorderStoryboardShotsInput,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	for _, reordered := range req.Items {
		if err := appendCommerceShotEvent(
			r.Context(), tx, project.OrganizationID, project.ID, item.Plan,
			reordered.ShotID, "reordered",
		); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceStoryboardPreviews(r, &item)
	w.Header().Set("ETag", commerceStoryboardETag(item.Plan.ID, item.Plan.EditRevision))
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) attachCommerceStoryboardPreviews(r *http.Request, item *commercepkg.StoryboardPlanDetail) {
	if s.storage == nil || item == nil {
		return
	}
	cache := make(map[string]string)
	presign := func(storageKey string) string {
		if storageKey == "" {
			return ""
		}
		if value, exists := cache[storageKey]; exists {
			return value
		}
		preview, err := s.storage.PresignGetObject(r.Context(), storageKey, 15*time.Minute)
		if err != nil {
			return ""
		}
		cache[storageKey] = preview.URL
		return preview.URL
	}
	for index := range item.Shots {
		shot := &item.Shots[index]
		if value := presign(shot.ImageStorageKey); value != "" {
			shot.ImagePreviewURL = &value
		}
		if value := presign(shot.VideoStorageKey); value != "" {
			shot.VideoPreviewURL = &value
		}
		for referenceIndex := range shot.ProductReferences {
			shot.ProductReferences[referenceIndex].PreviewURL = presign(shot.ProductReferences[referenceIndex].StorageKey)
		}
	}
}

func commerceStoryboardETag(planID string, revision int64) string {
	return fmt.Sprintf(`W/"commerce-storyboard:%s:%d"`, planID, revision)
}

func parseCommerceStoryboardIfMatch(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, `W/"commerce-storyboard:`) || !strings.HasSuffix(value, `"`) {
		return 0, false
	}
	value = strings.TrimSuffix(strings.TrimPrefix(value, `W/"commerce-storyboard:`), `"`)
	separator := strings.LastIndex(value, ":")
	if separator <= 0 || separator == len(value)-1 {
		return 0, false
	}
	revision, err := strconv.ParseInt(value[separator+1:], 10, 64)
	return revision, err == nil && revision > 0
}
