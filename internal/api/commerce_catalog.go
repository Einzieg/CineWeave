package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/workflows"
)

const maxCommerceProductReferenceBytes = int64(20 << 20)

func (s *Server) getCommerceSetupSession(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetSetupSession(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("setupSessionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) abandonCommerceSetupSession(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
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
	item, keys, err := s.commerceCatalog.AbandonSetupSession(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("setupSessionId"), req.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.storage != nil {
		for _, key := range keys {
			_ = s.storage.DeleteObject(r.Context(), key)
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) completeCommerceSetupSession(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "启动创建流程需要请求标识", nil, false)
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ExpectedRevision <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expectedRevision 必须大于 0", nil, false)
		return
	}
	options, err := s.loadCommerceProjectOptions(r.Context(), project.OrganizationID, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !options.Available {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCE_MODEL_CAPABILITY_UNAVAILABLE", "当前业务模型不能满足带货视频生产要求", map[string]any{"blockers": options.Blockers}, false)
		return
	}

	setupSessionID := strings.TrimSpace(r.PathValue("setupSessionId"))
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "setupSessionId": setupSessionID, "expectedRevision": req.ExpectedRevision,
	})
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(r.Context(), tx, project.OrganizationID, "commerce_setup_complete:"+setupSessionID, idempotencyKey, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(claim.replaySnapshot) > 0 {
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		var response map[string]any
		if err := json.Unmarshal(claim.replaySnapshot, &response); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusAccepted, response, map[string]any{"idempotentReplay": true})
		return
	}

	preparation, err := s.commerceCatalog.PrepareSetupCompletion(
		r.Context(), tx, project.OrganizationID, project.ID, setupSessionID, req.ExpectedRevision,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceProductMutationEvents(
		r.Context(), tx, project.OrganizationID, project.ID,
		commercepkg.ProductMutationResult{
			Product: preparation.Product, Version: preparation.ProductVersion, Activated: true,
		},
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceScriptMutationEvents(
		r.Context(), tx, project.OrganizationID, project.ID,
		commercepkg.ScriptVersionMutation{
			ScriptUnit: preparation.ScriptUnit, Version: preparation.SourceScriptVersion, Activated: true,
		},
		"commerce.script_unit.created",
	); err != nil {
		s.writeError(w, r, err)
		return
	}
	if preparation.Session.WorkflowTemplateVersionID != options.WorkflowTemplateVersionID {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCE_WORKFLOW_TEMPLATE_STALE", "项目创建时选择的工作流模板已不再可用，请重新创建项目", nil, false)
		return
	}
	workflowInput := workflows.CommerceProjectSetupInput{
		OrganizationID:            project.OrganizationID,
		ProjectID:                 project.ID,
		SetupSessionID:            setupSessionID,
		ExpectedSessionRevision:   preparation.Session.Revision,
		WorkflowTemplateVersionID: preparation.Session.WorkflowTemplateVersionID,
		ProductID:                 preparation.Product.ID,
		ProductVersionID:          preparation.ProductVersion.ID,
		ScriptUnitID:              preparation.ScriptUnit.ID,
		SourceScriptVersionID:     preparation.SourceScriptVersion.ID,
		RequestedBy:               principal.UserID,
	}
	runID, err := s.enqueueCommerceSetupRunTx(r.Context(), tx, principal, project, setupSessionID, workflowInput)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	session, err := s.commerceCatalog.AttachSetupRun(r.Context(), tx, preparation, runID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	response := map[string]any{"setupWorkflowRunId": runID, "setupSession": session}
	if err := completeIdempotencyTxWithStatus(r.Context(), tx, claim.state, http.StatusAccepted, response); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, response, nil)
}

func (s *Server) confirmCommerceSetupLanguage(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	if s.temporal == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "工作流服务暂不可用", nil, true)
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		ResolutionID     string `json:"resolutionId"`
		TargetLanguage   string `json:"targetLanguage"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.ResolutionID = strings.TrimSpace(req.ResolutionID)
	req.TargetLanguage = strings.TrimSpace(req.TargetLanguage)
	if req.ExpectedRevision <= 0 || req.ResolutionID == "" || req.TargetLanguage == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "语言确认参数不完整", nil, false)
		return
	}
	setupSessionID := strings.TrimSpace(r.PathValue("setupSessionId"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	session, err := s.commerceCatalog.GetSetupSession(r.Context(), tx, project.OrganizationID, project.ID, setupSessionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if session.SetupWorkflowRunID == nil || session.ScriptUnitID == nil {
		httpx.WriteError(w, r, http.StatusConflict, commercepkg.CodeSetupIncomplete, "创建流程尚未进入语言确认阶段", nil, false)
		return
	}
	resolution, err := s.commerceCatalog.ConfirmLanguage(
		r.Context(), tx, project.OrganizationID, project.ID, *session.ScriptUnitID,
		req.ResolutionID, req.TargetLanguage, principal.UserID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := s.commerceCatalog.MarkSetupLanguageConfirmed(
		r.Context(), tx, project.OrganizationID, project.ID, setupSessionID,
		*session.SetupWorkflowRunID, req.ExpectedRevision,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	run, err := s.commerceCatalog.GetSetupRun(r.Context(), tx, project.OrganizationID, project.ID, *session.SetupWorkflowRunID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceLanguageEvent(r.Context(), tx, project.OrganizationID, project.ID, resolution); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	signal := workflows.CommerceSetupLanguageConfirmationSignal{
		SetupSessionID: setupSessionID, LanguageResolutionID: resolution.ID, TargetLanguage: req.TargetLanguage,
	}
	if err := s.temporal.SignalWorkflow(r.Context(), run.TemporalWorkflowID, "", workflows.CommerceSetupLanguageConfirmationSignalName, signal); err != nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "WORKFLOW_SIGNAL_FAILED", "语言已保存，但工作流暂未收到确认，请重试", nil, true)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, map[string]any{
		"setupSession": updated, "setupRun": run,
	}, nil)
}

func (s *Server) getCommerceProduct(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetProduct(r.Context(), s.db, project.OrganizationID, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) createCommerceProductVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision  *int64          `json:"expectedRevision"`
		Name              string          `json:"name"`
		Brand             string          `json:"brand"`
		SellingPoints     json.RawMessage `json:"sellingPoints"`
		ImmutableFeatures json.RawMessage `json:"immutableFeatures"`
		ProhibitedClaims  json.RawMessage `json:"prohibitedClaims"`
		Metadata          json.RawMessage `json:"metadata"`
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
	result, err := s.commerceCatalog.CreateProductVersion(r.Context(), tx, project.OrganizationID, project.ID, principal.UserID, req.ExpectedRevision, commercepkg.ProductVersionInput{
		Name: req.Name, Brand: req.Brand, SellingPoints: req.SellingPoints,
		ImmutableFeatures: req.ImmutableFeatures, ProhibitedClaims: req.ProhibitedClaims,
		Metadata: req.Metadata,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceProductMutationEvents(r.Context(), tx, project.OrganizationID, project.ID, result); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	status := http.StatusCreated
	if r.Method == http.MethodPatch {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, r, status, result, nil)
}

func (s *Server) listCommerceProductVersions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	items, err := s.commerceCatalog.ListProductVersions(r.Context(), s.db, project.OrganizationID, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCommerceProductVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetProductVersion(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("versionId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listCommerceProductReferences(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	items, err := s.commerceCatalog.ListProductReferences(r.Context(), s.db, project.OrganizationID, project.ID, r.URL.Query().Get("filter[status]"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	for index := range items {
		if s.storage == nil {
			break
		}
		var storageKey string
		if err := s.db.QueryRow(r.Context(), `SELECT storage_key FROM artifacts WHERE id = $1 AND project_id = $2`, items[index].ArtifactID, project.ID).Scan(&storageKey); err != nil {
			continue
		}
		if preview, err := s.storage.PresignGetObject(r.Context(), storageKey, 15*time.Minute); err == nil {
			items[index].PreviewURL = preview.URL
		}
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createCommerceProductReferenceUploadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
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
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "上传商品图片需要请求标识", nil, false)
		return
	}
	var req struct {
		SetupSessionID *string `json:"setupSessionId"`
		FileName       string  `json:"fileName"`
		MimeType       string  `json:"mimeType"`
		ExpiresSeconds int     `json:"expiresSeconds"`
	}
	if !decode(w, r, &req) {
		return
	}
	fileName := cleanFileName(req.FileName)
	mimeType := normalizeCommerceImageMime(req.MimeType)
	if fileName == "" || !validCommerceImageMime(mimeType) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请选择 JPEG、PNG 或 WebP 商品图片", nil, false)
		return
	}
	product, err := s.commerceCatalog.GetProduct(r.Context(), s.db, project.OrganizationID, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	expires := time.Duration(req.ExpiresSeconds) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	if expires > time.Hour {
		expires = time.Hour
	}
	storageKey := fmt.Sprintf("uploads/%s/%s/commerce-product/%s/%s", project.OrganizationID, project.ID, randomStorageSegment(), fileName)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	upload, replay, err := s.commerceCatalog.ClaimProductReferenceUpload(r.Context(), tx, project.OrganizationID, project.ID, product.ID,
		req.SetupSessionID, storageKey, mimeType, fileName, idempotencyKey, principal.UserID, time.Now().Add(expires))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !replay && req.SetupSessionID != nil {
		if _, err := s.commerceCatalog.TrackSetupUpload(r.Context(), tx, project.OrganizationID, project.ID, *req.SetupSessionID, upload.StorageKey); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	put, err := s.storage.PresignPutObject(r.Context(), upload.StorageKey, upload.RequestedMimeType, time.Until(upload.ExpiresAt))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"uploadId": upload.ID, "uploadUrl": put.URL, "method": put.Method,
		"headers": put.Headers, "expiresAt": put.ExpiresAt,
	}, map[string]any{"idempotentReplay": replay})
}

func (s *Server) completeCommerceProductReferenceUpload(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储当前不可用", nil, true)
		return
	}
	var req struct {
		UploadID      string `json:"uploadId"`
		ReferenceRole string `json:"referenceRole"`
		SetPrimary    bool   `json:"setPrimary"`
	}
	if !decode(w, r, &req) {
		return
	}
	upload, err := s.commerceCatalog.GetProductReferenceUpload(r.Context(), s.db, project.OrganizationID, project.ID, strings.TrimSpace(req.UploadID), false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if upload.Status == "completed" && upload.ReferenceID != nil {
		items, listErr := s.commerceCatalog.ListProductReferences(r.Context(), s.db, project.OrganizationID, project.ID, "all")
		if listErr != nil {
			s.writeError(w, r, listErr)
			return
		}
		for _, item := range items {
			if item.ID == *upload.ReferenceID {
				httpx.WriteJSON(w, r, http.StatusOK, item, map[string]any{"idempotentReplay": true})
				return
			}
		}
	}
	if upload.Status != "pending" || time.Now().After(upload.ExpiresAt) {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCE_SETUP_INCOMPLETE", "商品图片上传凭据已失效", nil, false)
		return
	}
	body, reportedMime, err := s.storage.GetObject(r.Context(), upload.StorageKey, maxCommerceProductReferenceBytes)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	actualMime, width, height, err := inspectCommerceImage(body, reportedMime)
	if err != nil || actualMime != upload.RequestedMimeType {
		tx, rejectErr := s.db.Begin(r.Context())
		if rejectErr != nil {
			s.writeError(w, r, rejectErr)
			return
		}
		defer tx.Rollback(r.Context())
		lockedUpload, rejectErr := s.commerceCatalog.GetProductReferenceUpload(
			r.Context(), tx, project.OrganizationID, project.ID, upload.ID, true,
		)
		if rejectErr != nil {
			s.writeError(w, r, rejectErr)
			return
		}
		deleteTemporary := false
		if lockedUpload.Status == "pending" {
			if _, rejectErr = s.commerceCatalog.AbandonProductReferenceUpload(r.Context(), tx, lockedUpload); rejectErr != nil {
				s.writeError(w, r, rejectErr)
				return
			}
			if lockedUpload.SetupSessionID != nil {
				if _, rejectErr = s.commerceCatalog.CompleteSetupUpload(
					r.Context(), tx, project.OrganizationID, project.ID,
					*lockedUpload.SetupSessionID, lockedUpload.StorageKey,
				); rejectErr != nil {
					s.writeError(w, r, rejectErr)
					return
				}
			}
			deleteTemporary = true
		}
		if rejectErr = tx.Commit(r.Context()); rejectErr != nil {
			s.writeError(w, r, rejectErr)
			return
		}
		if deleteTemporary {
			_ = s.storage.DeleteObject(r.Context(), lockedUpload.StorageKey)
		}
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "上传文件与声明的商品图片格式不一致", nil, false)
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
	upload, err = s.commerceCatalog.GetProductReferenceUpload(r.Context(), tx, project.OrganizationID, project.ID, upload.ID, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	duplicate, duplicateFound, err := s.commerceCatalog.FindProductReferenceByHash(r.Context(), tx, project.OrganizationID, project.ID, contentHash)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item := duplicate
	deleteTemporary := duplicateFound
	if !duplicateFound {
		item, err = s.commerceCatalog.CreateProductReference(r.Context(), tx, commercepkg.CreateProductReferenceParams{
			OrganizationID: project.OrganizationID, ProjectID: project.ID, ProductID: upload.ProductID,
			StorageKey: upload.StorageKey, MimeType: actualMime, ContentHash: contentHash,
			ByteSize: int64(len(body)), Width: width, Height: height,
			ReferenceRole: req.ReferenceRole, SetPrimary: req.SetPrimary,
			QualityReview: json.RawMessage(`{"status":"accepted","source":"server_validation"}`), CreatedBy: principal.UserID,
		})
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if _, err := s.commerceCatalog.CompleteProductReferenceUpload(r.Context(), tx, upload, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if upload.SetupSessionID != nil {
		if _, err := s.commerceCatalog.CompleteSetupUpload(r.Context(), tx, project.OrganizationID, project.ID, *upload.SetupSessionID, upload.StorageKey); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if !duplicateFound {
		if err := appendCommerceProductReferenceEvent(
			r.Context(), tx, project.OrganizationID, project.ID,
			"commerce.product.reference.added", item,
		); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	if deleteTemporary {
		_ = s.storage.DeleteObject(r.Context(), upload.StorageKey)
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, map[string]any{"duplicate": duplicateFound})
}

func (s *Server) updateCommerceProductReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		ReferenceRole    string `json:"referenceRole"`
		Ordinal          *int   `json:"ordinal"`
		SetPrimary       *bool  `json:"setPrimary"`
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
	item, err := s.commerceCatalog.UpdateProductReference(r.Context(), tx, project.OrganizationID, project.ID,
		r.PathValue("referenceId"), req.ExpectedRevision, req.ReferenceRole, req.Ordinal, req.SetPrimary)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceProductReferenceEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.product.reference.updated", item,
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

func (s *Server) archiveCommerceProductReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
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
	item, err := s.commerceCatalog.ArchiveProductReference(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("referenceId"), req.ExpectedRevision)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := appendCommerceProductReferenceEvent(
		r.Context(), tx, project.OrganizationID, project.ID,
		"commerce.product.reference.archived", item,
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

func (s *Server) getCommerceProductRebuildImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionProjectWrite)
	if !ok {
		return
	}
	var req struct {
		TargetProductVersionID  string   `json:"targetProductVersionId"`
		TargetReferenceIDs      []string `json:"targetReferenceIds"`
		ExpectedProductRevision int64    `json:"expectedProductRevision"`
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
	impact, err := s.commerceCatalog.PlanProductRebuild(r.Context(), tx, project.OrganizationID, project.ID,
		strings.TrimSpace(req.TargetProductVersionID), req.TargetReferenceIDs, req.ExpectedProductRevision, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, impact, nil)
}

func (s *Server) createCommerceProductRebuild(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "商品换版需要请求标识", nil, false)
		return
	}
	var req struct {
		ImpactToken             string `json:"impactToken"`
		ExpectedProductRevision int64  `json:"expectedProductRevision"`
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
	result, err := s.commerceCatalog.ExecuteProductRebuild(r.Context(), tx, project.OrganizationID, project.ID,
		strings.TrimSpace(req.ImpactToken), req.ExpectedProductRevision, idempotencyKey, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !result.IdempotentReplay {
		product, loadErr := s.commerceCatalog.GetProduct(r.Context(), tx, project.OrganizationID, project.ID)
		if loadErr != nil {
			s.writeError(w, r, loadErr)
			return
		}
		version, loadErr := s.commerceCatalog.GetProductVersion(
			r.Context(), tx, project.OrganizationID, project.ID, result.ProductVersionID,
		)
		if loadErr != nil {
			s.writeError(w, r, loadErr)
			return
		}
		pack, loadErr := s.commerceCatalog.GetProductReferencePack(
			r.Context(), tx, project.OrganizationID, project.ID, result.ReferencePackID,
		)
		if loadErr != nil {
			s.writeError(w, r, loadErr)
			return
		}
		versionPayload := mustRawJSON(map[string]any{
			"productId": product.ID, "productVersionId": version.ID, "version": version.Version,
		})
		if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID,
			"commerce.product.version.activated", "commerce_product_version", version.ID, versionPayload); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID,
			"commerce.product.updated", "commerce_product", product.ID, mustRawJSON(map[string]any{
				"productId": product.ID, "productVersionId": version.ID, "revision": product.Revision,
				"activated": true, "requiresRebuild": false,
			})); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID,
			"commerce.reference_pack.created", "commerce_product_reference_pack", pack.ID, mustRawJSON(map[string]any{
				"productVersionId": pack.ProductVersionID, "referencePackId": pack.ID, "status": pack.Status,
			})); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, result, nil)
}

func normalizeCommerceImageMime(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if value == "image/jpg" {
		return "image/jpeg"
	}
	return value
}

func validCommerceImageMime(value string) bool {
	return value == "image/jpeg" || value == "image/png" || value == "image/webp"
}

func inspectCommerceImage(body []byte, reportedMime string) (string, int, int, error) {
	mimeType := normalizeCommerceImageMime(http.DetectContentType(body))
	if len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP" {
		mimeType = "image/webp"
	}
	if reported := normalizeCommerceImageMime(reportedMime); reported != "" && reported != "application/octet-stream" && mimeType != reported {
		return "", 0, 0, errors.New("reported image mime type does not match content")
	}
	if mimeType == "image/webp" {
		width, height, err := webPDimensions(body)
		return mimeType, width, height, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || !validCommerceImageMime(mimeType) || config.Width <= 0 || config.Height <= 0 {
		return "", 0, 0, errors.New("uploaded object is not a supported image")
	}
	return mimeType, config.Width, config.Height, nil
}

func webPDimensions(body []byte) (int, int, error) {
	if len(body) < 30 {
		return 0, 0, errors.New("webp image is truncated")
	}
	switch string(body[12:16]) {
	case "VP8X":
		width := 1 + int(body[24]) + int(body[25])<<8 + int(body[26])<<16
		height := 1 + int(body[27]) + int(body[28])<<8 + int(body[29])<<16
		return width, height, nil
	case "VP8 ":
		if len(body) < 30 || body[23] != 0x9d || body[24] != 0x01 || body[25] != 0x2a {
			return 0, 0, errors.New("webp VP8 frame header is invalid")
		}
		return int(binary.LittleEndian.Uint16(body[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(body[28:30]) & 0x3fff), nil
	case "VP8L":
		if body[20] != 0x2f {
			return 0, 0, errors.New("webp VP8L frame header is invalid")
		}
		bits := binary.LittleEndian.Uint32(body[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, nil
	default:
		return 0, 0, errors.New("webp chunk type is unsupported")
	}
}
