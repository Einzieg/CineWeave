package api

import (
	"context"
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
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"scriptUnitId":     strings.TrimSpace(r.PathValue("scriptUnitId")),
		"referenceId":      strings.TrimSpace(r.PathValue("referenceId")),
		"expectedRevision": req.ExpectedRevision,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "commerce.script.reference.archive", raw,
		strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := decodeAgentToolData[commercepkg.ScriptReferenceImage](result.Data)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listCommerceDirectVideos(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	items, err := s.listCommerceDirectVideosCore(r.Context(), project, strings.TrimSpace(r.URL.Query().Get("filter[scriptUnitId]")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) listCommerceDirectVideosCore(ctx context.Context, project Project, scriptUnitID string) ([]commercepkg.DirectVideoJob, error) {
	items, err := s.commerceDirect.ListJobs(
		ctx, s.db, project.OrganizationID, project.ID, strings.TrimSpace(scriptUnitID),
	)
	if err != nil {
		return nil, err
	}
	s.attachCommerceDirectVideoPreviews(requestWithContext(ctx), items)
	return items, nil
}

func (s *Server) getCommerceDirectVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	item, err := s.getCommerceDirectVideoCore(r.Context(), project, r.PathValue("jobId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) getCommerceDirectVideoCore(ctx context.Context, project Project, jobID string) (commercepkg.DirectVideoJob, error) {
	item, err := s.commerceDirect.GetJob(
		ctx, s.db, project.OrganizationID, project.ID, strings.TrimSpace(jobID),
	)
	if err != nil {
		return commercepkg.DirectVideoJob{}, err
	}
	single := []commercepkg.DirectVideoJob{item}
	s.attachCommerceDirectVideoPreviews(requestWithContext(ctx), single)
	return single[0], nil
}

func (s *Server) cancelCommerceDirectVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowCancel)
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
		r.Context(), principal, project, "commerce.video.cancel",
		map[string]any{
			"jobId":  strings.TrimSpace(r.PathValue("jobId")),
			"reason": strings.TrimSpace(req.Reason),
		},
		idempotencyKey,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeManualAsyncActionResult(w, r, result)
}

func commerceDirectVideoTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s *Server) createCommerceDirectVideo(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var req commercepkg.CreateDirectVideoJobInput
	if !decode(w, r, &req) {
		return
	}
	result, err := s.executeManualAsyncAction(
		r.Context(), principal, project, "commerce.video.generate",
		map[string]any{
			"scriptUnitId":    strings.TrimSpace(r.PathValue("scriptUnitId")),
			"durationSeconds": req.DurationSeconds, "resolution": req.Resolution,
			"aspectRatio": req.AspectRatio, "generateAudio": req.GenerateAudio,
			"references": req.References,
		},
		idempotencyKey,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeManualAsyncActionResult(w, r, result)
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
