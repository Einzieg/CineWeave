package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

func (s *Server) getCommerceDirectVideoOptions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	options, err := s.commerceDirect.Options(r.Context(), s.db, project.OrganizationID, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, options, nil)
}

func (s *Server) listCommerceScriptReferences(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	items, err := s.commerceDirect.ListScriptReferences(
		r.Context(), s.db, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.URL.Query().Get("filter[status]"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceScriptReferencePreviews(r, items)
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createCommerceScriptReferenceUploadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储当前不可用", nil, true)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "上传自定义参考图需要请求标识", nil, false)
		return
	}
	var req struct {
		FileName       string `json:"fileName"`
		MimeType       string `json:"mimeType"`
		ExpiresSeconds int    `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	fileName := cleanFileName(req.FileName)
	mimeType := normalizeCommerceImageMime(req.MimeType)
	if fileName == "" || !validCommerceImageMime(mimeType) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请选择 JPEG、PNG 或 WebP 参考图", nil, false)
		return
	}
	product, err := s.commerceCatalog.GetProduct(r.Context(), s.db, project.OrganizationID, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	unit, err := s.commerceCatalog.GetScriptUnit(
		r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("scriptUnitId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if unit.Status == "archived" || unit.ProductID != product.ID {
		s.writeError(w, r, commercepkg.Error{
			Code: commercepkg.CodeScriptUnitArchived, Message: "已归档脚本不能上传自定义参考图",
		})
		return
	}
	expires := time.Duration(req.ExpiresSeconds) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	if expires > time.Hour {
		expires = time.Hour
	}
	storageKey := fmt.Sprintf(
		"uploads/%s/%s/commerce-script/%s/%s/%s",
		project.OrganizationID, project.ID, unit.ID, randomStorageSegment(), fileName,
	)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	upload, replay, err := s.commerceDirect.ClaimScriptReferenceUpload(
		r.Context(), tx, project.OrganizationID, project.ID, product.ID, unit.ID,
		storageKey, mimeType, fileName, idempotencyKey, principal.UserID, time.Now().Add(expires),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	put, err := s.storage.PresignPutObject(
		r.Context(), upload.StorageKey, upload.RequestedMimeType, time.Until(upload.ExpiresAt),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"uploadId": upload.ID, "uploadUrl": put.URL, "method": put.Method,
		"headers": put.Headers, "expiresAt": put.ExpiresAt,
	}, map[string]any{"idempotentReplay": replay})
}

func (s *Server) completeCommerceScriptReferenceUpload(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储当前不可用", nil, true)
		return
	}
	var req struct {
		UploadID string `json:"uploadId"`
	}
	if !decode(w, r, &req) {
		return
	}
	scriptUnitID := r.PathValue("scriptUnitId")
	upload, err := s.commerceDirect.GetScriptReferenceUpload(
		r.Context(), s.db, project.OrganizationID, project.ID, scriptUnitID,
		strings.TrimSpace(req.UploadID), false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if upload.Status == "completed" && upload.ReferenceImageID != nil {
		items, listErr := s.commerceDirect.ListScriptReferences(
			r.Context(), s.db, project.OrganizationID, project.ID, scriptUnitID, "all",
		)
		if listErr != nil {
			s.writeError(w, r, listErr)
			return
		}
		for _, item := range items {
			if item.ID == *upload.ReferenceImageID {
				single := []commercepkg.ScriptReferenceImage{item}
				s.attachCommerceScriptReferencePreviews(r, single)
				item = single[0]
				httpx.WriteJSON(w, r, http.StatusOK, item, map[string]any{"idempotentReplay": true})
				return
			}
		}
	}
	if upload.Status != "pending" || time.Now().After(upload.ExpiresAt) {
		httpx.WriteError(w, r, http.StatusConflict, commercepkg.CodeDirectVideoStateConflict, "自定义参考图上传凭据已失效", nil, false)
		return
	}
	body, reportedMime, err := s.storage.GetObject(r.Context(), upload.StorageKey, maxCommerceProductReferenceBytes)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	actualMime, width, height, inspectErr := inspectCommerceImage(body, reportedMime)
	if inspectErr != nil || actualMime != upload.RequestedMimeType {
		if abandonErr := s.abandonCommerceScriptReferenceUpload(r, project, upload); abandonErr != nil {
			s.writeError(w, r, abandonErr)
			return
		}
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "上传文件与声明的参考图格式不一致", nil, false)
		return
	}
	sum := sha256.Sum256(body)
	contentHash := hex.EncodeToString(sum[:])
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	upload, err = s.commerceDirect.GetScriptReferenceUpload(
		r.Context(), tx, project.OrganizationID, project.ID, scriptUnitID, upload.ID, true,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	duplicate, duplicateFound, err := s.commerceDirect.FindScriptReferenceByHash(
		r.Context(), tx, project.OrganizationID, project.ID, scriptUnitID, contentHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item := duplicate
	if !duplicateFound {
		item, err = s.commerceDirect.CreateScriptReference(
			r.Context(), tx, commercepkg.CreateScriptReferenceParams{
				OrganizationID: project.OrganizationID, ProjectID: project.ID,
				ProductID: upload.ProductID, ScriptUnitID: scriptUnitID,
				StorageKey: upload.StorageKey, OriginalFileName: upload.OriginalFileName,
				MimeType: actualMime, Width: width, Height: height,
				ByteSize: int64(len(body)), ContentHash: contentHash, CreatedBy: principal.UserID,
			},
		)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := s.commerceDirect.CompleteScriptReferenceUpload(r.Context(), tx, upload, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if !duplicateFound {
		if err := insertAPIEvent(
			r.Context(), tx, project.OrganizationID, project.ID,
			"commerce.script_reference.added", "commerce_script_reference_image", item.ID,
			mustRawJSON(map[string]any{
				"commerceScriptUnitId": item.ScriptUnitID,
				"scriptReferenceId":    item.ID,
				"revision":             item.Revision,
			}),
		); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if duplicateFound {
		_ = s.storage.DeleteObject(r.Context(), upload.StorageKey)
	}
	single := []commercepkg.ScriptReferenceImage{item}
	s.attachCommerceScriptReferencePreviews(r, single)
	item = single[0]
	httpx.WriteJSON(w, r, http.StatusCreated, item, map[string]any{"duplicate": duplicateFound})
}

func (s *Server) archiveCommerceScriptReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetDelete)
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
	item, err := s.commerceDirect.ArchiveScriptReference(
		r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("scriptUnitId"), r.PathValue("referenceId"), req.ExpectedRevision,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.script_reference.archived", "commerce_script_reference_image", item.ID,
		mustRawJSON(map[string]any{
			"commerceScriptUnitId": item.ScriptUnitID,
			"scriptReferenceId":    item.ID,
			"revision":             item.Revision,
		}),
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listCommerceDirectVideos(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	items, err := s.commerceDirect.ListJobs(
		r.Context(), s.db, project.OrganizationID, project.ID,
		strings.TrimSpace(r.URL.Query().Get("filter[scriptUnitId]")),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceDirectVideoPreviews(r, items)
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCommerceDirectVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	item, err := s.commerceDirect.GetJob(
		r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("jobId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	single := []commercepkg.DirectVideoJob{item}
	s.attachCommerceDirectVideoPreviews(r, single)
	item = single[0]
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) cancelCommerceDirectVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowCancel)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(
			w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED",
			"取消视频任务需要请求标识", nil, false,
		)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	jobID := strings.TrimSpace(r.PathValue("jobId"))
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "用户取消带货视频任务"
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "jobId": jobID,
		"userId": principal.UserID, "reason": reason,
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(
		r.Context(), tx, project.OrganizationID,
		"commerce_direct_video:cancel:"+jobID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.DirectVideoJob
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		current, loadErr := s.commerceDirect.GetJob(
			r.Context(), s.db, project.OrganizationID, project.ID, jobID,
		)
		if loadErr != nil {
			s.writeError(w, r, loadErr)
			return
		}
		if !commerceDirectVideoTerminal(current.Status) {
			if err := s.requestCommerceDirectVideoCancellation(
				r, project, current, reason,
			); err != nil {
				s.writeError(w, r, err)
				return
			}
		}
		status := claim.replayStatus
		if status != http.StatusOK && status != http.StatusAccepted {
			status = http.StatusAccepted
		}
		items := []commercepkg.DirectVideoJob{replay}
		s.attachCommerceDirectVideoPreviews(r, items)
		httpx.WriteJSON(
			w, r, status, items[0],
			map[string]any{"idempotentReplay": true},
		)
		return
	}
	job, err := s.commerceDirect.GetJob(
		r.Context(), tx, project.OrganizationID, project.ID, jobID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if commerceDirectVideoTerminal(job.Status) {
		if err := completeIdempotencyTxWithStatus(
			r.Context(), tx, claim.state, http.StatusOK, job,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		items := []commercepkg.DirectVideoJob{job}
		s.attachCommerceDirectVideoPreviews(r, items)
		httpx.WriteJSON(w, r, http.StatusOK, items[0], nil)
		return
	}
	if job.WorkflowRunID == nil || strings.TrimSpace(*job.WorkflowRunID) == "" {
		httpx.WriteError(
			w, r, http.StatusConflict, commercepkg.CodeDirectVideoStateConflict,
			"视频任务缺少可取消的工作流", nil, false,
		)
		return
	}
	tag, err := tx.Exec(r.Context(), `
		UPDATE commerce_direct_video_jobs
		SET status = 'cancelling',
		    error_code = 'USER_CANCELLED',
		    error_message = $4,
		    updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('queued', 'running', 'cancelling')
	`, job.ID, project.OrganizationID, project.ID, reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if tag.RowsAffected() != 1 {
		s.writeError(w, r, newAPIError(
			http.StatusConflict, commercepkg.CodeDirectVideoStateConflict,
			"视频任务状态已变化，请刷新后重试",
		))
		return
	}
	job.Status = "cancelling"
	errorCode := "USER_CANCELLED"
	job.ErrorCode = &errorCode
	job.ErrorMessage = &reason
	if err := completeIdempotencyTxWithStatus(
		r.Context(), tx, claim.state, http.StatusAccepted, job,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.requestCommerceDirectVideoCancellation(r, project, job, reason); err != nil {
		s.writeError(w, r, err)
		return
	}
	items := []commercepkg.DirectVideoJob{job}
	s.attachCommerceDirectVideoPreviews(r, items)
	httpx.WriteJSON(w, r, http.StatusAccepted, items[0], nil)
}

func (s *Server) requestCommerceDirectVideoCancellation(
	r *http.Request,
	project Project,
	job commercepkg.DirectVideoJob,
	reason string,
) error {
	if job.WorkflowRunID == nil || strings.TrimSpace(*job.WorkflowRunID) == "" {
		return newAPIError(
			http.StatusConflict, commercepkg.CodeDirectVideoStateConflict,
			"视频任务缺少可取消的工作流",
		)
	}
	run, err := scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`), *job.WorkflowRunID, project.OrganizationID, project.ID))
	if err != nil {
		return err
	}
	updatedRun, err := s.cancelWorkflowRunItem(r.Context(), run, reason)
	if err != nil {
		return err
	}
	if updatedRun.Status == "cancelled" {
		return s.finalizePreStartCommerceDirectVideoCancellation(r, project, job, reason)
	}
	return nil
}

func commerceDirectVideoTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s *Server) finalizePreStartCommerceDirectVideoCancellation(
	r *http.Request,
	project Project,
	job commercepkg.DirectVideoJob,
	reason string,
) error {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `
		UPDATE commerce_direct_video_jobs
		SET status = 'cancelled',
		    completed_at = now(),
		    cancelled_at = now(),
		    error_code = 'USER_CANCELLED',
		    error_message = $4,
		    updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status = 'cancelling'
	`, job.ID, project.OrganizationID, project.ID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err := insertAPIEvent(
			r.Context(), tx, project.OrganizationID, project.ID,
			"commerce.direct_video.cancelled", "commerce_direct_video_job", job.ID,
			mustRawJSON(map[string]any{
				"workflowRunId":        job.WorkflowRunID,
				"commerceScriptUnitId": job.ScriptUnitID,
				"reason":               reason,
			}),
		); err != nil {
			return err
		}
	}
	return tx.Commit(r.Context())
}

func (s *Server) createCommerceDirectVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "生成视频需要请求标识", nil, false)
		return
	}
	var req commercepkg.CreateDirectVideoJobInput
	if !decode(w, r, &req) {
		return
	}
	scriptUnitID := r.PathValue("scriptUnitId")
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "scriptUnitId": scriptUnitID, "input": req,
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(
		r.Context(), tx, project.OrganizationID,
		"commerce_direct_video:create:"+scriptUnitID, idempotencyKey, requestHash,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.DirectVideoJob
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		single := []commercepkg.DirectVideoJob{replay}
		s.attachCommerceDirectVideoPreviews(r, single)
		replay = single[0]
		httpx.WriteJSON(w, r, http.StatusAccepted, replay, map[string]any{"idempotentReplay": true})
		return
	}
	jobID := uuid.NewString()
	workflowRunID := uuid.NewString()
	prepared, err := s.commerceDirect.PrepareJob(
		r.Context(), tx, commercepkg.PrepareDirectVideoJobParams{
			JobID: jobID, WorkflowRunID: workflowRunID,
			OrganizationID: project.OrganizationID, ProjectID: project.ID,
			ScriptUnitID: scriptUnitID, CreatedBy: principal.UserID,
			IdempotencyKey: idempotencyKey, Input: req,
		},
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	workflowInput := workflows.CommerceDirectVideoInput{
		OrganizationID: project.OrganizationID, ProjectID: project.ID,
		ScriptUnitID: scriptUnitID, JobID: jobID,
		WorkflowRunID: workflowRunID, CreatedBy: principal.UserID,
	}
	if err := workflows.EnqueueCommerceDirectVideoTx(
		r.Context(), tx, workflowInput, prepared.Production,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.commerceDirect.InsertPreparedJob(r.Context(), tx, prepared, idempotencyKey)
	if err != nil {
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
	if err := completeIdempotencyTxWithStatus(
		r.Context(), tx, claim.state, http.StatusAccepted, item,
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, item, nil)
}

func (s *Server) abandonCommerceScriptReferenceUpload(
	r *http.Request,
	project Project,
	upload commercepkg.ScriptReferenceUpload,
) error {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	locked, err := s.commerceDirect.GetScriptReferenceUpload(
		r.Context(), tx, project.OrganizationID, project.ID, upload.ScriptUnitID, upload.ID, true,
	)
	if err != nil {
		return err
	}
	deleteTemporary := locked.Status == "pending"
	if deleteTemporary {
		if err := s.commerceDirect.AbandonScriptReferenceUpload(r.Context(), tx, locked); err != nil {
			return err
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	if deleteTemporary {
		_ = s.storage.DeleteObject(r.Context(), locked.StorageKey)
	}
	return nil
}

func (s *Server) attachCommerceScriptReferencePreviews(
	r *http.Request,
	items []commercepkg.ScriptReferenceImage,
) {
	if s.storage == nil {
		return
	}
	for index := range items {
		if preview, err := s.storage.PresignGetObject(r.Context(), items[index].StorageKey, 15*time.Minute); err == nil {
			items[index].PreviewURL = preview.URL
		}
	}
}

func (s *Server) attachCommerceDirectVideoPreviews(
	r *http.Request,
	items []commercepkg.DirectVideoJob,
) {
	if s.storage == nil {
		return
	}
	for index := range items {
		if items[index].OutputStorageKey != nil {
			if preview, err := s.storage.PresignGetObject(
				r.Context(), *items[index].OutputStorageKey, 15*time.Minute,
			); err == nil {
				items[index].OutputPreviewURL = preview.URL
			}
		}
		for referenceIndex := range items[index].References {
			if preview, err := s.storage.PresignGetObject(
				r.Context(), items[index].References[referenceIndex].StorageKey, 15*time.Minute,
			); err == nil {
				items[index].References[referenceIndex].PreviewURL = preview.URL
			}
		}
	}
}
