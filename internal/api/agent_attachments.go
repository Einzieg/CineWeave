package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxAgentImageAttachments = 8
	agentImageAttachmentTTL  = 15 * time.Minute
)

type AgentImageAttachment struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	FileName    string     `json:"fileName"`
	MimeType    string     `json:"mimeType"`
	ByteSize    int64      `json:"byteSize"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	ContentHash string     `json:"contentHash"`
	Status      string     `json:"status"`
	ArtifactID  string     `json:"artifactId,omitempty"`
	MediaFileID string     `json:"mediaFileId,omitempty"`
	PreviewURL  string     `json:"previewUrl,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`

	OrganizationID string `json:"-"`
	StorageKey     string `json:"-"`
	IdempotencyKey string `json:"-"`
}

type agentTaskImageAttachmentLink struct {
	AttachmentID string
	Usage        string
	Ordinal      int
}

func (s *Server) createAgentImageAttachmentUploadURL(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite,
	)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储当前不可用", nil, true)
		return
	}
	idempotency := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotency == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED", "上传助手图片需要请求标识", nil, false)
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
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请选择 JPEG、PNG 或 WebP 图片", nil, false)
		return
	}
	expires := time.Duration(req.ExpiresSeconds) * time.Second
	if expires <= 0 {
		expires = agentImageAttachmentTTL
	}
	if expires > time.Hour {
		expires = time.Hour
	}
	storageKey := fmt.Sprintf(
		"uploads/%s/%s/agent-attachments/%s/%s",
		project.OrganizationID, project.ID, randomStorageSegment(), fileName,
	)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	attachment, replay, err := claimAgentImageAttachment(
		r.Context(), tx, project, storageKey, fileName, mimeType,
		idempotency, principal.UserID, time.Now().Add(expires),
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
		r.Context(), attachment.StorageKey, attachment.MimeType,
		time.Until(attachment.ExpiresAt),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"attachmentId": attachment.ID,
		"uploadUrl":    put.URL,
		"method":       put.Method,
		"headers":      put.Headers,
		"expiresAt":    put.ExpiresAt,
	}, map[string]any{"idempotentReplay": replay})
}

func (s *Server) completeAgentImageAttachment(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite,
	)
	if !ok {
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储当前不可用", nil, true)
		return
	}
	attachment, err := loadAgentImageAttachment(
		r.Context(), s.db, project.OrganizationID, project.ID,
		strings.TrimSpace(r.PathValue("attachmentId")), false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if attachment.Status == "completed" {
		s.attachAgentImagePreview(r, &attachment)
		httpx.WriteJSON(w, r, http.StatusOK, attachment, map[string]any{"idempotentReplay": true})
		return
	}
	if attachment.Status != "pending" || time.Now().After(attachment.ExpiresAt) {
		httpx.WriteError(w, r, http.StatusConflict, "AGENT_IMAGE_ATTACHMENT_EXPIRED", "助手图片上传凭据已失效", nil, false)
		return
	}
	body, reportedMIME, err := s.storage.GetObject(
		r.Context(), attachment.StorageKey, maxCommerceProductReferenceBytes,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	actualMIME, width, height, inspectErr := inspectCommerceImage(body, reportedMIME)
	if inspectErr != nil || actualMIME != attachment.MimeType {
		if abandonErr := abandonAgentImageAttachment(
			r.Context(), s.db, project.OrganizationID, project.ID, attachment.ID,
		); abandonErr != nil {
			s.writeError(w, r, abandonErr)
			return
		}
		_ = s.storage.DeleteObject(r.Context(), attachment.StorageKey)
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "上传文件与声明的图片格式不一致", nil, false)
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
	attachment, err = loadAgentImageAttachment(
		r.Context(), tx, project.OrganizationID, project.ID, attachment.ID, true,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if attachment.Status == "completed" {
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		s.attachAgentImagePreview(r, &attachment)
		httpx.WriteJSON(w, r, http.StatusOK, attachment, map[string]any{"idempotentReplay": true})
		return
	}
	if attachment.Status != "pending" || time.Now().After(attachment.ExpiresAt) {
		httpx.WriteError(w, r, http.StatusConflict, "AGENT_IMAGE_ATTACHMENT_EXPIRED", "助手图片上传凭据已失效", nil, false)
		return
	}
	metadata := mustMarshal(map[string]any{
		"source":       "agent_image_attachment",
		"attachmentId": attachment.ID,
		"fileName":     attachment.FileName,
		"contentHash":  contentHash,
		"width":        width,
		"height":       height,
	})
	var artifactID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type,
			content_hash, metadata, created_by
		)
		VALUES ($1, $2, 'agent_image_attachment', $3, $4, $5, $6, $7)
		RETURNING id::text
	`, project.OrganizationID, project.ID, attachment.StorageKey, actualMIME,
		contentHash, metadata, principal.UserID).Scan(&artifactID); err != nil {
		s.writeError(w, r, err)
		return
	}
	var mediaFileID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id::text
	`, project.OrganizationID, project.ID, artifactID, attachment.StorageKey,
		actualMIME, len(body), width, height, contentHash, metadata, principal.UserID,
	).Scan(&mediaFileID); err != nil {
		s.writeError(w, r, err)
		return
	}
	err = tx.QueryRow(r.Context(), `
		UPDATE agent_image_attachments
		SET status = 'completed', requested_mime_type = $2,
		    byte_size = $3, width = $4, height = $5, content_hash = $6,
		    artifact_id = $7, media_file_id = $8, completed_at = now()
		WHERE id = $1 AND status = 'pending'
		RETURNING id::text, organization_id::text, project_id::text,
		          storage_key, original_file_name, requested_mime_type,
		          COALESCE(byte_size, 0), COALESCE(width, 0), COALESCE(height, 0),
		          COALESCE(content_hash, ''), status, idempotency_key,
		          COALESCE(artifact_id::text, ''), COALESCE(media_file_id::text, ''),
		          created_at, expires_at, completed_at
	`, attachment.ID, actualMIME, len(body), width, height, contentHash,
		artifactID, mediaFileID,
	).Scan(agentImageAttachmentScanTargets(&attachment)...)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachAgentImagePreview(r, &attachment)
	httpx.WriteJSON(w, r, http.StatusCreated, attachment, nil)
}

func (s *Server) assignAgentImageAttachment(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite,
	)
	if !ok {
		return
	}
	if !project.ProjectKind.IsCommerce() {
		httpx.WriteError(w, r, http.StatusConflict, "PROJECT_KIND_MISMATCH", "该图片绑定操作仅适用于带货视频项目", nil, false)
		return
	}
	var req struct {
		Scope         string `json:"scope"`
		ScriptUnitID  string `json:"scriptUnitId"`
		ReferenceRole string `json:"referenceRole"`
		SetPrimary    bool   `json:"setPrimary"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Scope = strings.TrimSpace(req.Scope)
	req.ScriptUnitID = strings.TrimSpace(req.ScriptUnitID)
	if req.Scope != "product_common" && req.Scope != "script_custom" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "图片用途必须是商品公共参考图或指定脚本自定义参考图", nil, false)
		return
	}
	if req.Scope == "script_custom" && req.ScriptUnitID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "绑定脚本自定义参考图时必须指定广告脚本", nil, false)
		return
	}
	attachment, err := loadAgentImageAttachment(
		r.Context(), s.db, project.OrganizationID, project.ID,
		strings.TrimSpace(r.PathValue("attachmentId")), false,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if attachment.Status != "completed" {
		httpx.WriteError(w, r, http.StatusConflict, "AGENT_IMAGE_ATTACHMENT_NOT_READY", "助手图片尚未完成入库", nil, false)
		return
	}
	source, err := commercepkg.LoadExistingImageReference(
		r.Context(), s.db, project.OrganizationID, project.ID,
		attachment.ArtifactID, attachment.MediaFileID, attachment.FileName,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	product, err := s.commerceCatalog.GetProduct(
		r.Context(), s.db, project.OrganizationID, project.ID,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	response := map[string]any{
		"attachmentId": attachment.ID,
		"scope":        req.Scope,
	}
	if req.Scope == "product_common" {
		item, duplicate, bindErr := s.commerceCatalog.BindExistingProductReference(
			r.Context(), tx, project.OrganizationID, project.ID, product.ID,
			source, req.ReferenceRole, req.SetPrimary, principal.UserID,
		)
		if bindErr != nil {
			s.writeError(w, r, bindErr)
			return
		}
		if !duplicate {
			if err := appendCommerceProductReferenceEvent(
				r.Context(), tx, project.OrganizationID, project.ID,
				"commerce.product.reference.added", item,
			); err != nil {
				s.writeError(w, r, err)
				return
			}
		}
		response["productReference"] = item
		response["productReferenceId"] = item.ID
		response["duplicate"] = duplicate
	} else {
		item, duplicate, bindErr := s.commerceDirect.BindExistingScriptReference(
			r.Context(), tx, project.OrganizationID, project.ID, product.ID,
			req.ScriptUnitID, source, principal.UserID,
		)
		if bindErr != nil {
			s.writeError(w, r, bindErr)
			return
		}
		if !duplicate {
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
		response["scriptReference"] = item
		response["scriptReferenceId"] = item.ID
		response["commerceScriptUnitId"] = item.ScriptUnitID
		response["duplicate"] = duplicate
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (s *Server) attachAgentImagePreview(r *http.Request, attachment *AgentImageAttachment) {
	if s.storage == nil || attachment == nil || attachment.StorageKey == "" {
		return
	}
	preview, err := s.storage.PresignGetObject(r.Context(), attachment.StorageKey, 15*time.Minute)
	if err == nil {
		attachment.PreviewURL = preview.URL
	}
}

func claimAgentImageAttachment(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	storageKey string,
	fileName string,
	mimeType string,
	idempotencyKey string,
	createdBy string,
	expiresAt time.Time,
) (AgentImageAttachment, bool, error) {
	var item AgentImageAttachment
	err := tx.QueryRow(ctx, `
		INSERT INTO agent_image_attachments(
			organization_id, project_id, storage_key, original_file_name,
			requested_mime_type, idempotency_key, created_by, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
		RETURNING id::text, organization_id::text, project_id::text,
		          storage_key, original_file_name, requested_mime_type,
		          COALESCE(byte_size, 0), COALESCE(width, 0), COALESCE(height, 0),
		          COALESCE(content_hash, ''), status, idempotency_key,
		          COALESCE(artifact_id::text, ''), COALESCE(media_file_id::text, ''),
		          created_at, expires_at, completed_at
	`, project.OrganizationID, project.ID, storageKey, fileName, mimeType,
		idempotencyKey, createdBy, expiresAt,
	).Scan(agentImageAttachmentScanTargets(&item)...)
	if err == nil {
		return item, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AgentImageAttachment{}, false, err
	}
	item, err = loadAgentImageAttachmentByIdempotency(
		ctx, tx, project.OrganizationID, idempotencyKey, true,
	)
	if err != nil {
		return AgentImageAttachment{}, false, err
	}
	if item.ProjectID != project.ID || item.FileName != fileName || item.MimeType != mimeType {
		return AgentImageAttachment{}, false, apiError{
			Status: http.StatusConflict, Code: "IDEMPOTENCY_KEY_REUSED",
			Message: "请求标识已用于另一张助手图片",
		}
	}
	if item.Status != "pending" {
		return AgentImageAttachment{}, false, apiError{
			Status: http.StatusConflict, Code: "IDEMPOTENCY_KEY_REUSED",
			Message: "请求标识对应的助手图片已结束，请使用新的请求标识",
		}
	}
	if time.Now().After(item.ExpiresAt) {
		return AgentImageAttachment{}, false, apiError{
			Status: http.StatusConflict, Code: "AGENT_IMAGE_ATTACHMENT_EXPIRED",
			Message: "助手图片上传凭据已失效，请重新选择图片",
		}
	}
	return item, true, nil
}

func loadAgentImageAttachment(
	ctx context.Context,
	db agentAttachmentQuerier,
	organizationID string,
	projectID string,
	attachmentID string,
	lock bool,
) (AgentImageAttachment, error) {
	query := `
		SELECT id::text, organization_id::text, project_id::text,
		       storage_key, original_file_name, requested_mime_type,
		       COALESCE(byte_size, 0), COALESCE(width, 0), COALESCE(height, 0),
		       COALESCE(content_hash, ''), status, idempotency_key,
		       COALESCE(artifact_id::text, ''), COALESCE(media_file_id::text, ''),
		       created_at, expires_at, completed_at
		FROM agent_image_attachments
		WHERE organization_id = $1 AND project_id = $2 AND id = $3`
	if lock {
		query += " FOR UPDATE"
	}
	var item AgentImageAttachment
	err := db.QueryRow(ctx, query, organizationID, projectID, attachmentID).
		Scan(agentImageAttachmentScanTargets(&item)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentImageAttachment{}, apiError{
			Status: http.StatusNotFound, Code: "AGENT_IMAGE_ATTACHMENT_NOT_FOUND",
			Message: "助手图片不存在",
		}
	}
	return item, err
}

func loadAgentImageAttachmentByIdempotency(
	ctx context.Context,
	db agentAttachmentQuerier,
	organizationID string,
	idempotencyKey string,
	lock bool,
) (AgentImageAttachment, error) {
	query := `
		SELECT id::text, organization_id::text, project_id::text,
		       storage_key, original_file_name, requested_mime_type,
		       COALESCE(byte_size, 0), COALESCE(width, 0), COALESCE(height, 0),
		       COALESCE(content_hash, ''), status, idempotency_key,
		       COALESCE(artifact_id::text, ''), COALESCE(media_file_id::text, ''),
		       created_at, expires_at, completed_at
		FROM agent_image_attachments
		WHERE organization_id = $1 AND idempotency_key = $2`
	if lock {
		query += " FOR UPDATE"
	}
	var item AgentImageAttachment
	err := db.QueryRow(ctx, query, organizationID, idempotencyKey).
		Scan(agentImageAttachmentScanTargets(&item)...)
	return item, err
}

func agentImageAttachmentScanTargets(item *AgentImageAttachment) []any {
	return []any{
		&item.ID, &item.OrganizationID, &item.ProjectID,
		&item.StorageKey, &item.FileName, &item.MimeType,
		&item.ByteSize, &item.Width, &item.Height, &item.ContentHash,
		&item.Status, &item.IdempotencyKey, &item.ArtifactID, &item.MediaFileID,
		&item.CreatedAt, &item.ExpiresAt, &item.CompletedAt,
	}
}

func abandonAgentImageAttachment(
	ctx context.Context,
	db agentAttachmentExecer,
	organizationID string,
	projectID string,
	attachmentID string,
) error {
	_, err := db.Exec(ctx, `
		UPDATE agent_image_attachments
		SET status = 'abandoned', abandoned_at = now()
		WHERE organization_id = $1 AND project_id = $2 AND id = $3 AND status = 'pending'
	`, organizationID, projectID, attachmentID)
	return err
}

func canonicalizeAgentTaskImageAttachments(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	constraints []byte,
) ([]byte, []agentTaskImageAttachmentLink, error) {
	value := rawObject(constraints)
	rawAttachments, exists := value["attachments"]
	if !exists {
		return constraints, nil, nil
	}
	entries, ok := rawAttachments.([]any)
	if !ok {
		return nil, nil, apiError{
			Status: http.StatusUnprocessableEntity, Code: "AGENT_IMAGE_ATTACHMENTS_INVALID",
			Message: "助手图片附件格式无效",
		}
	}
	if len(entries) == 0 {
		delete(value, "attachments")
		return mustMarshal(value), nil, nil
	}
	if len(entries) > maxAgentImageAttachments {
		return nil, nil, apiError{
			Status: http.StatusUnprocessableEntity, Code: "AGENT_IMAGE_ATTACHMENTS_LIMIT_EXCEEDED",
			Message: fmt.Sprintf("一次最多附加 %d 张图片", maxAgentImageAttachments),
		}
	}
	seen := make(map[string]bool)
	canonical := make([]map[string]any, 0, len(entries))
	links := make([]agentTaskImageAttachmentLink, 0, len(entries))
	for ordinal, entry := range entries {
		payload := rawObject(mustMarshal(entry))
		attachmentID := strings.TrimSpace(stringValueFromAny(payload["attachmentId"]))
		usage := strings.TrimSpace(stringValueFromAny(payload["usage"]))
		if usage == "" {
			usage = "unspecified"
		}
		if attachmentID == "" || seen[attachmentID] || !validAgentImageAttachmentUsage(usage) {
			return nil, nil, apiError{
				Status: http.StatusUnprocessableEntity, Code: "AGENT_IMAGE_ATTACHMENTS_INVALID",
				Message: "助手图片附件标识或用途无效",
			}
		}
		seen[attachmentID] = true
		attachment, err := loadAgentImageAttachment(
			ctx, tx, project.OrganizationID, project.ID, attachmentID, false,
		)
		if err != nil {
			return nil, nil, err
		}
		if attachment.Status != "completed" || attachment.ArtifactID == "" || attachment.MediaFileID == "" {
			return nil, nil, apiError{
				Status: http.StatusConflict, Code: "AGENT_IMAGE_ATTACHMENT_NOT_READY",
				Message: "助手图片尚未完成入库",
			}
		}
		canonical = append(canonical, map[string]any{
			"attachmentId": attachment.ID,
			"artifactId":   attachment.ArtifactID,
			"mediaFileId":  attachment.MediaFileID,
			"fileName":     attachment.FileName,
			"mimeType":     attachment.MimeType,
			"width":        attachment.Width,
			"height":       attachment.Height,
			"contentHash":  attachment.ContentHash,
			"usage":        usage,
		})
		links = append(links, agentTaskImageAttachmentLink{
			AttachmentID: attachment.ID, Usage: usage, Ordinal: ordinal,
		})
	}
	value["attachments"] = canonical
	return mustMarshal(value), links, nil
}

func insertAgentTaskImageAttachmentLinks(
	ctx context.Context,
	tx pgx.Tx,
	taskID string,
	links []agentTaskImageAttachmentLink,
) error {
	for _, link := range links {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_task_image_attachments(task_id, attachment_id, usage, ordinal)
			VALUES ($1, $2, $3, $4)
		`, taskID, link.AttachmentID, link.Usage, link.Ordinal); err != nil {
			return err
		}
	}
	return nil
}

func validAgentImageAttachmentUsage(value string) bool {
	switch value {
	case "unspecified", "product_common", "script_custom", "visual_reference":
		return true
	default:
		return false
	}
}

func agentTaskImageReferences(task AgentTask) []provider.GatewayImageReference {
	constraints := rawObject(task.Constraints)
	entries, _ := constraints["attachments"].([]any)
	references := make([]provider.GatewayImageReference, 0, len(entries))
	for _, entry := range entries {
		value := rawObject(mustMarshal(entry))
		artifactID := strings.TrimSpace(stringValueFromAny(value["artifactId"]))
		if artifactID == "" {
			continue
		}
		references = append(references, provider.GatewayImageReference{
			Type:       "image",
			ArtifactID: artifactID,
			Metadata: mustMarshal(map[string]any{
				"attachmentId": stringValueFromAny(value["attachmentId"]),
				"fileName":     stringValueFromAny(value["fileName"]),
				"usage":        stringValueFromAny(value["usage"]),
			}),
		})
	}
	return references
}

func agentTaskHasImageAttachment(task AgentTask, attachmentID string) bool {
	attachmentID = strings.TrimSpace(attachmentID)
	if attachmentID == "" {
		return false
	}
	constraints := rawObject(task.Constraints)
	entries, _ := constraints["attachments"].([]any)
	for _, entry := range entries {
		value := rawObject(mustMarshal(entry))
		if strings.TrimSpace(stringValueFromAny(value["attachmentId"])) == attachmentID {
			return true
		}
	}
	return false
}

func (s *Server) recordAgentTaskImageAttachmentUsage(
	ctx context.Context,
	taskID string,
	attachmentID string,
	usage string,
) error {
	taskID = strings.TrimSpace(taskID)
	attachmentID = strings.TrimSpace(attachmentID)
	usage = strings.TrimSpace(usage)
	if taskID == "" || attachmentID == "" || !validAgentImageAttachmentUsage(usage) || usage == "unspecified" {
		return apiError{
			Status: http.StatusUnprocessableEntity, Code: "AGENT_IMAGE_ATTACHMENTS_INVALID",
			Message: "助手图片最终用途无效",
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE agent_task_image_attachments
		SET usage = $3
		WHERE task_id = $1 AND attachment_id = $2
	`, taskID, attachmentID, usage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apiError{
			Status: http.StatusUnprocessableEntity, Code: "AGENT_IMAGE_ATTACHMENTS_INVALID",
			Message: "助手图片不属于当前任务",
		}
	}
	tag, err = tx.Exec(ctx, `
		UPDATE agent_tasks
		SET constraints = jsonb_set(
		        constraints,
		        '{attachments}',
		        COALESCE((
		          SELECT jsonb_agg(
		            CASE
		              WHEN item->>'attachmentId' = $2
		              THEN jsonb_set(item, '{usage}', to_jsonb($3::text), true)
		              ELSE item
		            END
		            ORDER BY ordinal
		          )
		          FROM jsonb_array_elements(COALESCE(constraints->'attachments', '[]'::jsonb))
		               WITH ORDINALITY AS attachment(item, ordinal)
		        ), '[]'::jsonb),
		        true
		    ),
		    updated_at = now()
		WHERE id = $1
	`, taskID, attachmentID, usage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apiError{
			Status: http.StatusNotFound, Code: "AGENT_TASK_NOT_FOUND",
			Message: "助手任务不存在",
		}
	}
	return tx.Commit(ctx)
}

type agentAttachmentQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type agentAttachmentExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
