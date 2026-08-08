package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	projectControlDefaultContentChunkBytes  = 64 << 10
	projectControlMaximumContentChunkBytes  = 256 << 10
	projectControlMaximumContentUploadBytes = 64 << 20
	projectControlMaximumOpenContentUploads = 5
	projectControlMaximumActorStagingBytes  = 256 << 20
	projectControlContentUploadTTL          = 24 * time.Hour
)

type projectControlContentInput struct {
	ProjectID   string `json:"projectId"`
	TargetType  string `json:"targetType"`
	TargetID    string `json:"targetId"`
	ContentHash string `json:"contentHash"`
	Cursor      string `json:"cursor"`
	MaxBytes    int    `json:"maxBytes"`
}

type projectControlContentDocument struct {
	ProjectID   string
	TargetType  string
	TargetID    string
	Revision    int64
	Format      string
	Content     string
	ContentHash string
}

type projectControlContentCursor struct {
	Version     int    `json:"v"`
	TargetType  string `json:"targetType"`
	TargetID    string `json:"targetId"`
	Revision    int64  `json:"revision"`
	ContentHash string `json:"contentHash"`
	Offset      int    `json:"offset"`
}

type projectControlContentUpload struct {
	ID                 string
	OrganizationID     string
	WorkspaceID        string
	ProjectID          string
	ActorUserID        string
	ControlKeyID       string
	TargetType         string
	TargetID           string
	TargetRevision     int64
	ContentHash        string
	ContentFormat      string
	ExpectedSizeBytes  int64
	ExpectedChunkCount int
	Status             string
	StoragePrefix      string
	ExpiresAt          time.Time
	CommittedCommandID string
	CreatedAt          time.Time
}

type projectControlContentUploadChunk struct {
	Index      int
	ByteSize   int
	Hash       string
	StorageKey string
}

func (e *projectControlExecutor) contentDescribe(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	input, document, err := e.authorizedContentDocument(ctx, identity.Principal, raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("已读取内容信息", map[string]any{
		"content": projectControlContentMetadata(document),
		"request": map[string]any{"projectId": input.ProjectID},
	}), nil
}

func (e *projectControlExecutor) contentRead(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	input, document, err := e.authorizedContentDocument(ctx, identity.Principal, raw)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if input.ContentHash != "" && !strings.EqualFold(strings.TrimSpace(input.ContentHash), document.ContentHash) {
		return projectControlFailure("CONTENT_VERSION_CHANGED", "内容已更新，请重新读取内容信息", false, map[string]any{
			"currentContentHash": document.ContentHash, "currentRevision": document.Revision,
		}), nil
	}
	offset := 0
	if strings.TrimSpace(input.Cursor) != "" {
		cursor, decodeErr := decodeProjectControlContentCursor(input.Cursor)
		if decodeErr != nil {
			return projectcontrol.Result{}, controlValidationError("cursor 无效")
		}
		if cursor.TargetType != document.TargetType || cursor.TargetID != document.TargetID ||
			cursor.Revision != document.Revision || cursor.ContentHash != document.ContentHash {
			return projectControlFailure("CONTENT_CURSOR_STALE", "内容已更新，旧分块游标已失效", false, map[string]any{
				"currentContentHash": document.ContentHash, "currentRevision": document.Revision,
			}), nil
		}
		offset = cursor.Offset
	}
	chunkSize := input.MaxBytes
	if chunkSize <= 0 {
		chunkSize = projectControlDefaultContentChunkBytes
	}
	if chunkSize > projectControlMaximumContentChunkBytes {
		chunkSize = projectControlMaximumContentChunkBytes
	}
	chunk, nextOffset, readErr := utf8ContentChunk(document.Content, offset, chunkSize)
	if readErr != nil {
		return projectcontrol.Result{}, controlValidationError(readErr.Error())
	}
	data := map[string]any{
		"content": map[string]any{
			"targetType": document.TargetType, "targetId": document.TargetID,
			"revision": document.Revision, "contentHash": document.ContentHash,
			"format": document.Format, "totalBytes": len([]byte(document.Content)),
			"totalCharacters": utf8.RuneCountInString(document.Content),
			"offsetBytes":     offset, "chunkBytes": len([]byte(chunk)), "chunkText": chunk,
		},
	}
	result := projectControlSuccess("已读取内容分块", data)
	if nextOffset < len([]byte(document.Content)) {
		result.NextCursor, err = encodeProjectControlContentCursor(projectControlContentCursor{
			Version: 1, TargetType: document.TargetType, TargetID: document.TargetID,
			Revision: document.Revision, ContentHash: document.ContentHash, Offset: nextOffset,
		})
		if err != nil {
			return projectcontrol.Result{}, err
		}
	}
	return result, nil
}

func (e *projectControlExecutor) contentWriteBegin(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input struct {
		ProjectID         string `json:"projectId"`
		TargetType        string `json:"targetType"`
		TargetID          string `json:"targetId"`
		ExpectedRevision  int64  `json:"expectedRevision"`
		ContentHash       string `json:"contentHash"`
		ContentFormat     string `json:"contentFormat"`
		ExpectedSizeBytes int64  `json:"expectedSizeBytes"`
		ExpectedChunks    int    `json:"expectedChunkCount"`
		IdempotencyKey    string `json:"idempotencyKey"`
	}
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.ContentHash = strings.ToLower(strings.TrimSpace(input.ContentHash))
	input.ContentFormat = strings.TrimSpace(input.ContentFormat)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ExpectedRevision < 1 || len(input.ContentHash) != 64 || input.ContentFormat == "" ||
		input.ExpectedSizeBytes < 1 || input.ExpectedSizeBytes > projectControlMaximumContentUploadBytes ||
		input.ExpectedChunks < 1 || input.ExpectedChunks > 10000 || input.IdempotencyKey == "" {
		return projectcontrol.Result{}, controlValidationError("内容暂存参数无效")
	}
	project, principal, document, err := e.authorizedContentWriteTarget(
		ctx, identity.Principal, input.ProjectID, input.TargetType, input.TargetID,
	)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if document.Revision != input.ExpectedRevision {
		return projectControlFailure("REVISION_CONFLICT", "目标内容已更新，请刷新后重试", false, map[string]any{
			"currentRevision": document.Revision, "currentContentHash": document.ContentHash,
		}), nil
	}
	if err := validateProjectControlContentFormat(input.TargetType, input.ContentFormat); err != nil {
		return projectcontrol.Result{}, err
	}
	controller, controlKeyID, err := projectControlContentCommandIdentity(identity)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	uploadID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{
		"cineweave-content-upload", principal.UserID, string(controller), input.IdempotencyKey,
	}, ":"))).String()
	storagePrefix := strings.Join([]string{
		"project-control-staging", project.OrganizationID, principal.UserID, uploadID,
	}, "/")
	expiresAt := time.Now().UTC().Add(projectControlContentUploadTTL)
	tx, err := e.server.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return projectcontrol.Result{}, err
	}
	defer tx.Rollback(ctx)
	existing, existingErr := loadProjectControlContentUploadRow(ctx, tx.QueryRow(ctx, projectControlContentUploadSelectSQL+` WHERE id = $1 FOR UPDATE`, uploadID))
	if existingErr == nil {
		if existing.ActorUserID != principal.UserID || existing.ProjectID != project.ID || existing.TargetType != input.TargetType ||
			existing.TargetID != input.TargetID || existing.TargetRevision != input.ExpectedRevision ||
			existing.ContentHash != input.ContentHash || existing.ContentFormat != input.ContentFormat ||
			existing.ExpectedSizeBytes != input.ExpectedSizeBytes || existing.ExpectedChunkCount != input.ExpectedChunks {
			return projectcontrol.Result{}, projectcontrol.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return projectcontrol.Result{}, err
		}
		return projectControlContentUploadResult(existing, true), nil
	}
	if existingErr != pgx.ErrNoRows {
		return projectcontrol.Result{}, existingErr
	}
	var openCount int
	var openBytes int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(expected_size_bytes), 0)
		FROM project_control_content_uploads
		WHERE actor_user_id = $1 AND status = 'open' AND expires_at > now()
	`, principal.UserID).Scan(&openCount, &openBytes); err != nil {
		return projectcontrol.Result{}, err
	}
	if openCount >= projectControlMaximumOpenContentUploads || openBytes+input.ExpectedSizeBytes > projectControlMaximumActorStagingBytes {
		return projectControlFailure("CONTENT_UPLOAD_QUOTA_EXCEEDED", "暂存内容已达到并发或容量上限，请提交或放弃已有上传", true, map[string]any{
			"openUploads": openCount, "openBytes": openBytes,
		}), nil
	}
	upload, err := loadProjectControlContentUploadRow(ctx, tx.QueryRow(ctx, `
		INSERT INTO project_control_content_uploads(
			id, organization_id, workspace_id, project_id, actor_user_id, control_key_id,
			target_type, target_id, target_revision, content_hash, content_format,
			expected_size_bytes, expected_chunk_count, storage_prefix, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+projectControlContentUploadColumns,
		uploadID, project.OrganizationID, project.WorkspaceID, project.ID, principal.UserID, controlKeyID,
		input.TargetType, input.TargetID, input.ExpectedRevision, input.ContentHash, input.ContentFormat,
		input.ExpectedSizeBytes, input.ExpectedChunks, storagePrefix, expiresAt))
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return projectcontrol.Result{}, err
	}
	return projectControlContentUploadResult(upload, false), nil
}

func (e *projectControlExecutor) contentWriteChunk(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input struct {
		ProjectID  string `json:"projectId"`
		UploadID   string `json:"uploadId"`
		ChunkIndex int    `json:"chunkIndex"`
		ChunkHash  string `json:"chunkHash"`
		ChunkText  string `json:"chunkText"`
	}
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.UploadID = strings.TrimSpace(input.UploadID)
	input.ChunkHash = strings.ToLower(strings.TrimSpace(input.ChunkHash))
	chunkBytes := []byte(input.ChunkText)
	if input.ProjectID == "" || input.UploadID == "" || input.ChunkIndex < 0 || len(input.ChunkHash) != 64 ||
		len(chunkBytes) == 0 || len(chunkBytes) > projectControlMaximumContentChunkBytes || !utf8.Valid(chunkBytes) {
		return projectcontrol.Result{}, controlValidationError("内容分块参数无效或超过 256 KiB")
	}
	actualHash := sha256.Sum256(chunkBytes)
	if hex.EncodeToString(actualHash[:]) != input.ChunkHash {
		return projectControlFailure("CONTENT_CHUNK_HASH_MISMATCH", "内容分块 SHA-256 不匹配", false, nil), nil
	}
	upload, err := e.authorizedContentUpload(ctx, identity.Principal, input.ProjectID, input.UploadID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if err := validateOpenProjectControlContentUpload(upload); err != nil {
		return projectcontrol.Result{}, err
	}
	if input.ChunkIndex >= upload.ExpectedChunkCount {
		return projectcontrol.Result{}, controlValidationError("chunkIndex 超出预计分块范围")
	}
	storageKey := fmt.Sprintf("%s/%05d-%s.part", upload.StoragePrefix, input.ChunkIndex, input.ChunkHash)
	if _, err := e.server.storage.PutObject(ctx, storageKey, chunkBytes, "text/plain; charset=utf-8"); err != nil {
		return projectcontrol.Result{}, err
	}
	tx, err := e.server.db.Begin(ctx)
	if err != nil {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, err
	}
	defer tx.Rollback(ctx)
	locked, err := loadProjectControlContentUploadRow(ctx, tx.QueryRow(ctx, projectControlContentUploadSelectSQL+` WHERE id = $1 FOR UPDATE`, upload.ID))
	if err != nil {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, err
	}
	if err := validateOwnedProjectControlContentUpload(locked, identity.Principal.UserID, input.ProjectID); err != nil {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, err
	}
	if err := validateOpenProjectControlContentUpload(locked); err != nil {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, err
	}
	var existing projectControlContentUploadChunk
	existingErr := tx.QueryRow(ctx, `
		SELECT chunk_index, byte_size, content_hash, storage_key
		FROM project_control_content_upload_chunks
		WHERE upload_id = $1 AND chunk_index = $2
	`, upload.ID, input.ChunkIndex).Scan(&existing.Index, &existing.ByteSize, &existing.Hash, &existing.StorageKey)
	if existingErr == nil {
		if existing.ByteSize != len(chunkBytes) || existing.Hash != input.ChunkHash {
			_ = e.server.storage.DeleteObject(ctx, storageKey)
			return projectcontrol.Result{}, projectcontrol.ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return projectcontrol.Result{}, err
		}
		return projectControlSuccess("内容分块已存在", map[string]any{
			"uploadId": upload.ID, "chunkIndex": input.ChunkIndex, "chunkHash": input.ChunkHash,
			"byteSize": len(chunkBytes), "idempotentReplay": true,
		}), nil
	}
	if existingErr != pgx.ErrNoRows {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, existingErr
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_control_content_upload_chunks(upload_id, chunk_index, byte_size, content_hash, storage_key)
		VALUES ($1, $2, $3, $4, $5)
	`, upload.ID, input.ChunkIndex, len(chunkBytes), input.ChunkHash, storageKey); err != nil {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = e.server.storage.DeleteObject(ctx, storageKey)
		return projectcontrol.Result{}, err
	}
	return projectControlSuccess("内容分块已暂存", map[string]any{
		"uploadId": upload.ID, "chunkIndex": input.ChunkIndex, "chunkHash": input.ChunkHash,
		"byteSize": len(chunkBytes), "idempotentReplay": false,
	}), nil
}

func (e *projectControlExecutor) contentWriteCommit(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input struct {
		ProjectID      string `json:"projectId"`
		UploadID       string `json:"uploadId"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.UploadID = strings.TrimSpace(input.UploadID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || input.UploadID == "" || input.IdempotencyKey == "" {
		return projectcontrol.Result{}, controlValidationError("projectId、uploadId 和 idempotencyKey 不能为空")
	}
	upload, err := e.authorizedContentUpload(ctx, identity.Principal, input.ProjectID, input.UploadID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if upload.Status == "committed" && upload.CommittedCommandID != "" {
		command, err := e.repository.Get(ctx, upload.CommittedCommandID)
		if err != nil {
			return projectcontrol.Result{}, err
		}
		return projectControlCommittedUploadResult(upload, command, true), nil
	}
	if err := validateOpenProjectControlContentUpload(upload); err != nil {
		return projectcontrol.Result{}, err
	}
	chunks, content, err := e.assembleProjectControlContentUpload(ctx, upload)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	project, principal, document, err := e.authorizedContentWriteTarget(
		ctx, identity.Principal, upload.ProjectID, upload.TargetType, upload.TargetID,
	)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if document.Revision != upload.TargetRevision {
		return projectControlFailure("REVISION_CONFLICT", "目标内容已更新，暂存内容不能覆盖当前版本", false, map[string]any{
			"expectedRevision": upload.TargetRevision, "currentRevision": document.Revision,
		}), nil
	}
	if upload.TargetType == "commerce_script_unit" {
		if err := e.server.validateCommerceScriptContentForCurrentVideoModel(ctx, project, content); err != nil {
			return projectcontrol.Result{}, err
		}
	}
	controller, controlKeyID, err := projectControlContentCommandIdentity(identity)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	descriptor, ok := e.registry.Get("content.write.commit")
	if !ok {
		return projectcontrol.Result{}, fmt.Errorf("content.write.commit descriptor is unavailable")
	}
	commandInput := mustRawJSON(map[string]any{
		"uploadId": upload.ID, "targetType": upload.TargetType, "targetId": upload.TargetID,
		"expectedRevision": upload.TargetRevision, "contentHash": upload.ContentHash,
		"contentFormat": upload.ContentFormat, "expectedSizeBytes": upload.ExpectedSizeBytes,
	})
	command, replayed, err := e.repository.ExecuteSync(ctx, projectcontrol.CreateCommand{
		OrganizationID: project.OrganizationID, WorkspaceID: project.WorkspaceID, ProjectID: project.ID,
		ActorUserID: principal.UserID, ControllerType: controller, ControlKeyID: controlKeyID,
		Descriptor: descriptor, Input: commandInput, IdempotencyKey: input.IdempotencyKey,
	}, func(mutationCtx context.Context, tx pgx.Tx, command projectcontrol.Command) (json.RawMessage, error) {
		locked, err := loadProjectControlContentUploadRow(mutationCtx, tx.QueryRow(mutationCtx, projectControlContentUploadSelectSQL+` WHERE id = $1 FOR UPDATE`, upload.ID))
		if err != nil {
			return nil, err
		}
		if err := validateOwnedProjectControlContentUpload(locked, principal.UserID, project.ID); err != nil {
			return nil, err
		}
		if err := validateOpenProjectControlContentUpload(locked); err != nil {
			return nil, err
		}
		var chunkCount int
		var totalBytes int64
		if err := tx.QueryRow(mutationCtx, `
			SELECT count(*), COALESCE(sum(byte_size), 0)
			FROM project_control_content_upload_chunks
			WHERE upload_id = $1
		`, locked.ID).Scan(&chunkCount, &totalBytes); err != nil {
			return nil, err
		}
		if chunkCount != locked.ExpectedChunkCount || totalBytes != locked.ExpectedSizeBytes {
			return nil, newAPIError(http.StatusConflict, "CONTENT_UPLOAD_CHANGED", "暂存分块已变化，请重新校验后提交")
		}
		mutationOutput, err := e.applyProjectControlContentMutation(mutationCtx, tx, project, principal.UserID, locked, content)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(mutationCtx, `
			UPDATE project_control_content_uploads
			SET status = 'committed', committed_command_id = $2, committed_at = now()
			WHERE id = $1 AND status = 'open'
		`, locked.ID, command.ID); err != nil {
			return nil, err
		}
		return mustRawJSON(map[string]any{
			"uploadId": locked.ID, "targetType": locked.TargetType, "targetId": locked.TargetID,
			"contentHash": locked.ContentHash, "byteSize": locked.ExpectedSizeBytes,
			"mutation": mutationOutput,
		}), nil
	})
	if err != nil {
		return projectcontrol.Result{}, err
	}
	for _, chunk := range chunks {
		_ = e.server.storage.DeleteObject(ctx, chunk.StorageKey)
	}
	upload.Status = "committed"
	upload.CommittedCommandID = command.ID
	return projectControlCommittedUploadResult(upload, command, replayed), nil
}

func (e *projectControlExecutor) contentWriteAbort(ctx context.Context, identity controlmcp.Identity, raw json.RawMessage) (projectcontrol.Result, error) {
	var input struct {
		ProjectID      string `json:"projectId"`
		UploadID       string `json:"uploadId"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err := decodeControlInput(raw, &input); err != nil {
		return projectcontrol.Result{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.UploadID = strings.TrimSpace(input.UploadID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || input.UploadID == "" || input.IdempotencyKey == "" {
		return projectcontrol.Result{}, controlValidationError("projectId、uploadId 和 idempotencyKey 不能为空")
	}
	upload, err := e.authorizedContentUpload(ctx, identity.Principal, input.ProjectID, input.UploadID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if upload.Status == "committed" {
		return projectControlFailure("CONTENT_UPLOAD_ALREADY_COMMITTED", "内容已提交，不能再放弃", false, map[string]any{
			"commandId": upload.CommittedCommandID,
		}), nil
	}
	chunks, err := loadProjectControlContentUploadChunks(ctx, e.server.db, upload.ID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	tag, err := e.server.db.Exec(ctx, `
		UPDATE project_control_content_uploads
		SET status = 'aborted', aborted_at = COALESCE(aborted_at, now())
		WHERE id = $1 AND actor_user_id = $2 AND project_id = $3 AND status = 'open'
	`, upload.ID, identity.Principal.UserID, input.ProjectID)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	for _, chunk := range chunks {
		_ = e.server.storage.DeleteObject(ctx, chunk.StorageKey)
	}
	return projectControlSuccess("已放弃内容暂存", map[string]any{
		"uploadId": upload.ID, "status": "aborted", "idempotentReplay": tag.RowsAffected() == 0,
	}), nil
}

func (e *projectControlExecutor) authorizedContentDocument(
	ctx context.Context,
	principal auth.Principal,
	raw json.RawMessage,
) (projectControlContentInput, projectControlContentDocument, error) {
	var input projectControlContentInput
	if err := decodeControlInput(raw, &input); err != nil {
		return input, projectControlContentDocument{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	if input.ProjectID == "" || input.TargetType == "" || input.TargetID == "" {
		return input, projectControlContentDocument{}, controlValidationError("projectId、targetType 和 targetId 不能为空")
	}
	permission, err := projectControlContentReadPermission(input.TargetType)
	if err != nil {
		return input, projectControlContentDocument{}, err
	}
	if _, _, err := e.authorizedProjectID(ctx, principal, input.ProjectID, permission); err != nil {
		return input, projectControlContentDocument{}, err
	}
	document, err := e.loadProjectControlContent(ctx, input.ProjectID, input.TargetType, input.TargetID)
	return input, document, err
}

func (e *projectControlExecutor) loadProjectControlContent(ctx context.Context, projectID, targetType, targetID string) (projectControlContentDocument, error) {
	document := projectControlContentDocument{ProjectID: projectID, TargetType: targetType, TargetID: targetID}
	var err error
	switch targetType {
	case "project_source":
		err = e.server.db.QueryRow(ctx, `
			SELECT content, content_revision, content_format
			FROM project_sources
			WHERE id = $1 AND project_id = $2
		`, targetID, projectID).Scan(&document.Content, &document.Revision, &document.Format)
	case "novel_chapter":
		err = e.server.db.QueryRow(ctx, `
			SELECT chapter.content, chapter.content_revision, source.content_format
			FROM novel_chapters chapter
			JOIN project_sources source ON source.id = chapter.source_id
			WHERE chapter.id = $1 AND source.project_id = $2
		`, targetID, projectID).Scan(&document.Content, &document.Revision, &document.Format)
	case "script_episode":
		err = e.server.db.QueryRow(ctx, `
			SELECT content, revision, content_format
			FROM script_episodes
			WHERE id = $1 AND project_id = $2
		`, targetID, projectID).Scan(&document.Content, &document.Revision, &document.Format)
	case "script_version":
		err = e.server.db.QueryRow(ctx, `
			SELECT content, version::bigint, content_format
			FROM script_versions
			WHERE id = $1 AND project_id = $2 AND COALESCE(status, 'active') <> 'archived'
		`, targetID, projectID).Scan(&document.Content, &document.Revision, &document.Format)
	case "commerce_script_unit":
		err = e.server.db.QueryRow(ctx, `
			SELECT COALESCE(version.content, unit.draft_content), unit.revision, 'plain_text'
			FROM commerce_script_units unit
			LEFT JOIN commerce_ad_script_versions version ON version.id = unit.current_source_version_id
			WHERE unit.id = $1 AND unit.project_id = $2 AND unit.status <> 'archived'
		`, targetID, projectID).Scan(&document.Content, &document.Revision, &document.Format)
	default:
		return projectControlContentDocument{}, controlValidationError("targetType 不受支持")
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			return projectControlContentDocument{}, newAPIError(http.StatusNotFound, "CONTENT_NOT_FOUND", "内容不存在")
		}
		return projectControlContentDocument{}, err
	}
	if !utf8.ValidString(document.Content) {
		return projectControlContentDocument{}, fmt.Errorf("content %s is not valid UTF-8", targetID)
	}
	hash := sha256.Sum256([]byte(document.Content))
	document.ContentHash = hex.EncodeToString(hash[:])
	return document, nil
}

func projectControlContentReadPermission(targetType string) (string, error) {
	switch targetType {
	case "project_source", "novel_chapter":
		return authz.PermissionSourceRead, nil
	case "script_episode", "script_version", "commerce_script_unit":
		return authz.PermissionScriptRead, nil
	default:
		return "", controlValidationError("targetType 不受支持")
	}
}

func (e *projectControlExecutor) authorizedContentWriteTarget(
	ctx context.Context,
	principal auth.Principal,
	projectID, targetType, targetID string,
) (Project, auth.Principal, projectControlContentDocument, error) {
	permission := ""
	switch targetType {
	case "project_source", "novel_chapter":
		permission = authz.PermissionSourceWrite
	case "script_episode", "commerce_script_unit":
		permission = authz.PermissionScriptWrite
	default:
		return Project{}, auth.Principal{}, projectControlContentDocument{}, controlValidationError("targetType 不受支持")
	}
	project, currentPrincipal, err := e.authorizedProjectID(ctx, principal, projectID, permission)
	if err != nil {
		return Project{}, auth.Principal{}, projectControlContentDocument{}, err
	}
	document, err := e.loadProjectControlContent(ctx, project.ID, targetType, targetID)
	return project, currentPrincipal, document, err
}

func (e *projectControlExecutor) authorizedContentUpload(
	ctx context.Context,
	principal auth.Principal,
	projectID, uploadID string,
) (projectControlContentUpload, error) {
	upload, err := loadProjectControlContentUploadRow(ctx, e.server.db.QueryRow(ctx, projectControlContentUploadSelectSQL+` WHERE id = $1`, uploadID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return projectControlContentUpload{}, newAPIError(http.StatusNotFound, "CONTENT_UPLOAD_NOT_FOUND", "内容暂存不存在")
		}
		return projectControlContentUpload{}, err
	}
	if err := validateOwnedProjectControlContentUpload(upload, principal.UserID, projectID); err != nil {
		return projectControlContentUpload{}, err
	}
	if _, _, _, err := e.authorizedContentWriteTarget(ctx, principal, projectID, upload.TargetType, upload.TargetID); err != nil {
		return projectControlContentUpload{}, err
	}
	return upload, nil
}

func (e *projectControlExecutor) assembleProjectControlContentUpload(
	ctx context.Context,
	upload projectControlContentUpload,
) ([]projectControlContentUploadChunk, string, error) {
	chunks, err := loadProjectControlContentUploadChunks(ctx, e.server.db, upload.ID)
	if err != nil {
		return nil, "", err
	}
	if len(chunks) != upload.ExpectedChunkCount {
		return nil, "", newAPIError(http.StatusConflict, "CONTENT_UPLOAD_INCOMPLETE", "内容分块尚未全部上传")
	}
	var content bytes.Buffer
	content.Grow(int(upload.ExpectedSizeBytes))
	for index, chunk := range chunks {
		if chunk.Index != index {
			return nil, "", newAPIError(http.StatusConflict, "CONTENT_UPLOAD_INCOMPLETE", "内容分块序号不连续")
		}
		body, _, err := e.server.storage.GetObject(ctx, chunk.StorageKey, projectControlMaximumContentChunkBytes)
		if err != nil {
			return nil, "", err
		}
		if len(body) != chunk.ByteSize {
			return nil, "", newAPIError(http.StatusConflict, "CONTENT_CHUNK_SIZE_MISMATCH", "暂存内容分块大小不匹配")
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != chunk.Hash {
			return nil, "", newAPIError(http.StatusConflict, "CONTENT_CHUNK_HASH_MISMATCH", "暂存内容分块校验失败")
		}
		content.Write(body)
	}
	if int64(content.Len()) != upload.ExpectedSizeBytes {
		return nil, "", newAPIError(http.StatusConflict, "CONTENT_SIZE_MISMATCH", "暂存内容总字节数不匹配")
	}
	body := content.Bytes()
	if !utf8.Valid(body) {
		return nil, "", newAPIError(http.StatusUnprocessableEntity, "CONTENT_UTF8_INVALID", "暂存内容不是有效 UTF-8")
	}
	hash := sha256.Sum256(body)
	if hex.EncodeToString(hash[:]) != upload.ContentHash {
		return nil, "", newAPIError(http.StatusConflict, "CONTENT_HASH_MISMATCH", "暂存内容完整 SHA-256 不匹配")
	}
	return chunks, string(body), nil
}

func (e *projectControlExecutor) applyProjectControlContentMutation(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	upload projectControlContentUpload,
	content string,
) (map[string]any, error) {
	r := requestWithContext(ctx)
	switch upload.TargetType {
	case "project_source":
		var revision int64
		if err := tx.QueryRow(ctx, `
			UPDATE project_sources
			SET content = $4, content_format = $5, updated_at = now()
			WHERE id = $1 AND project_id = $2 AND content_revision = $3
			RETURNING content_revision
		`, upload.TargetID, project.ID, upload.TargetRevision, content, upload.ContentFormat).Scan(&revision); err != nil {
			return nil, projectControlContentRevisionError(err)
		}
		if err := e.server.markProjectSourceDownstreamStaleTx(r.Context(), tx, project, upload.TargetID, []string{"content"}, actorUserID); err != nil {
			return nil, err
		}
		return map[string]any{"revision": revision}, nil
	case "novel_chapter":
		var sourceID string
		var revision int64
		if err := tx.QueryRow(ctx, `
			UPDATE novel_chapters
			SET content = $4, updated_at = now()
			WHERE id = $1 AND project_id = $2 AND content_revision = $3
			RETURNING source_id::text, content_revision
		`, upload.TargetID, project.ID, upload.TargetRevision, content).Scan(&sourceID, &revision); err != nil {
			return nil, projectControlContentRevisionError(err)
		}
		var rebuiltContent string
		if err := tx.QueryRow(ctx, `
			WITH ordered AS (
				SELECT chapter_index, NULLIF(btrim(volume_title), '') AS volume_title,
				       NULLIF(btrim(chapter_title), '') AS chapter_title,
				       NULLIF(btrim(content), '') AS content,
				       lag(NULLIF(btrim(volume_title), '')) OVER (ORDER BY chapter_index) AS previous_volume_title
				FROM novel_chapters WHERE project_id = $1 AND source_id = $2
			)
			SELECT COALESCE(string_agg(concat_ws(E'\n',
				CASE WHEN volume_title IS NOT NULL AND volume_title IS DISTINCT FROM previous_volume_title THEN volume_title END,
				chapter_title, content), E'\n\n' ORDER BY chapter_index), '')
			FROM ordered
		`, project.ID, sourceID).Scan(&rebuiltContent); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE project_sources SET content = $3, updated_at = now() WHERE id = $2 AND project_id = $1`, project.ID, sourceID, rebuiltContent); err != nil {
			return nil, err
		}
		if err := e.server.markProjectSourceDownstreamStaleTx(r.Context(), tx, project, sourceID, []string{"content", "chapters"}, actorUserID); err != nil {
			return nil, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "source.chapter.updated", "novel_chapter", upload.TargetID, mustRawJSON(map[string]any{
			"sourceId": sourceID, "chapterId": upload.TargetID, "revision": revision, "changedBy": actorUserID,
		})); err != nil {
			return nil, err
		}
		return map[string]any{"revision": revision, "sourceId": sourceID}, nil
	case "script_episode":
		var scriptID, versionID string
		var episodeIndex int
		var revision int64
		if err := tx.QueryRow(ctx, `
			UPDATE script_episodes
			SET content = $4, content_format = $5, manual_override = true,
			    edited_by = $6, edited_at = now()
			WHERE id = $1 AND project_id = $2 AND revision = $3
			RETURNING script_id::text, script_version_id::text, episode_index, revision
		`, upload.TargetID, project.ID, upload.TargetRevision, content, upload.ContentFormat, actorUserID).Scan(
			&scriptID, &versionID, &episodeIndex, &revision,
		); err != nil {
			return nil, projectControlContentRevisionError(err)
		}
		if err := rebuildScriptVersionContentFromEpisodesTx(r, tx, project.ID, versionID); err != nil {
			return nil, err
		}
		if err := markScriptVersionDownstreamStale(r.Context(), tx, project.ID, versionID); err != nil {
			return nil, err
		}
		if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
			return nil, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.episode.updated", "script_episode", upload.TargetID, mustRawJSON(map[string]any{
			"scriptId": scriptID, "scriptVersionId": versionID, "episodeIndex": episodeIndex,
			"contentUpload": true, "changedBy": actorUserID,
		})); err != nil {
			return nil, err
		}
		return map[string]any{"revision": revision, "scriptId": scriptID, "scriptVersionId": versionID}, nil
	case "commerce_script_unit":
		updated, err := e.server.commerceCatalog.UpdateScriptUnit(ctx, tx, project.OrganizationID, project.ID,
			upload.TargetID, actorUserID, upload.TargetRevision, commercepkg.UpdateScriptUnitInput{DraftContent: &content})
		if err != nil {
			return nil, err
		}
		if err := appendCommerceScriptUnitEvent(ctx, tx, project.OrganizationID, project.ID, "commerce.script_unit.updated", updated); err != nil {
			return nil, err
		}
		return map[string]any{"revision": updated.Revision, "contentHash": updated.CurrentContentHash}, nil
	default:
		return nil, controlValidationError("targetType 不受支持")
	}
}

func projectControlContentRevisionError(err error) error {
	if err == pgx.ErrNoRows {
		return newAPIError(http.StatusConflict, "REVISION_CONFLICT", "目标内容已更新，请刷新后重试")
	}
	return err
}

func validateProjectControlContentFormat(targetType, format string) error {
	switch targetType {
	case "project_source", "novel_chapter":
		if !validContentFormat(format) {
			return controlValidationError("contentFormat 无效")
		}
	case "script_episode":
		if !validScriptContentFormat(format) {
			return controlValidationError("contentFormat 无效")
		}
	case "commerce_script_unit":
		if format != "plain_text" && format != "markdown" {
			return controlValidationError("带货脚本 contentFormat 必须是 plain_text 或 markdown")
		}
	default:
		return controlValidationError("targetType 不受支持")
	}
	return nil
}

func projectControlContentCommandIdentity(identity controlmcp.Identity) (projectcontrol.ControllerType, string, error) {
	switch identity.ControllerType {
	case projectcontrol.ControllerCodexMCP:
		if strings.TrimSpace(identity.Key.ID) == "" {
			return "", "", fmt.Errorf("Codex control key identity is missing")
		}
		return identity.ControllerType, identity.Key.ID, nil
	case projectcontrol.ControllerManual:
		return identity.ControllerType, "", nil
	default:
		return "", "", fmt.Errorf("content upload controller type %q is unsupported", identity.ControllerType)
	}
}

func validateOwnedProjectControlContentUpload(upload projectControlContentUpload, actorUserID, projectID string) error {
	if upload.ActorUserID != strings.TrimSpace(actorUserID) || upload.ProjectID != strings.TrimSpace(projectID) {
		return newAPIError(http.StatusNotFound, "CONTENT_UPLOAD_NOT_FOUND", "内容暂存不存在")
	}
	return nil
}

func validateOpenProjectControlContentUpload(upload projectControlContentUpload) error {
	if upload.Status != "open" {
		return newAPIError(http.StatusConflict, "CONTENT_UPLOAD_NOT_OPEN", "内容暂存已结束，不能继续写入或提交")
	}
	if !upload.ExpiresAt.After(time.Now()) {
		return newAPIError(http.StatusConflict, "CONTENT_UPLOAD_EXPIRED", "内容暂存已过期，请重新开始")
	}
	return nil
}

func projectControlContentUploadResult(upload projectControlContentUpload, replayed bool) projectcontrol.Result {
	return projectControlSuccess("内容暂存已准备", map[string]any{
		"uploadId": upload.ID, "status": upload.Status, "targetType": upload.TargetType,
		"targetId": upload.TargetID, "expectedRevision": upload.TargetRevision,
		"contentHash": upload.ContentHash, "expectedSizeBytes": upload.ExpectedSizeBytes,
		"expectedChunkCount":    upload.ExpectedChunkCount,
		"recommendedChunkBytes": projectControlMaximumContentChunkBytes,
		"expiresAt":             upload.ExpiresAt, "idempotentReplay": replayed,
	})
}

func projectControlCommittedUploadResult(upload projectControlContentUpload, command projectcontrol.Command, replayed bool) projectcontrol.Result {
	result := projectControlSuccess("暂存内容已提交", map[string]any{
		"uploadId": upload.ID, "targetType": upload.TargetType, "targetId": upload.TargetID,
		"contentHash": upload.ContentHash, "command": command, "idempotentReplay": replayed,
	})
	result.CommandID = command.ID
	result.Status = string(command.Status)
	return result
}

const projectControlContentUploadColumns = `
	id::text, organization_id::text, COALESCE(workspace_id::text, ''), COALESCE(project_id::text, ''),
	actor_user_id::text, COALESCE(control_key_id::text, ''), target_type, target_id::text,
	target_revision, content_hash, content_format, expected_size_bytes, expected_chunk_count,
	status, storage_prefix, expires_at, COALESCE(committed_command_id::text, ''), created_at`

const projectControlContentUploadSelectSQL = `SELECT ` + projectControlContentUploadColumns + ` FROM project_control_content_uploads`

func loadProjectControlContentUploadRow(ctx context.Context, row pgx.Row) (projectControlContentUpload, error) {
	_ = ctx
	var upload projectControlContentUpload
	err := row.Scan(
		&upload.ID, &upload.OrganizationID, &upload.WorkspaceID, &upload.ProjectID,
		&upload.ActorUserID, &upload.ControlKeyID, &upload.TargetType, &upload.TargetID,
		&upload.TargetRevision, &upload.ContentHash, &upload.ContentFormat,
		&upload.ExpectedSizeBytes, &upload.ExpectedChunkCount, &upload.Status,
		&upload.StoragePrefix, &upload.ExpiresAt, &upload.CommittedCommandID, &upload.CreatedAt,
	)
	return upload, err
}

type projectControlContentRows interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadProjectControlContentUploadChunks(ctx context.Context, db projectControlContentRows, uploadID string) ([]projectControlContentUploadChunk, error) {
	rows, err := db.Query(ctx, `
		SELECT chunk_index, byte_size, content_hash, storage_key
		FROM project_control_content_upload_chunks
		WHERE upload_id = $1
		ORDER BY chunk_index
	`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chunks := make([]projectControlContentUploadChunk, 0)
	for rows.Next() {
		var chunk projectControlContentUploadChunk
		if err := rows.Scan(&chunk.Index, &chunk.ByteSize, &chunk.Hash, &chunk.StorageKey); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

func (s *Server) CleanupExpiredProjectControlContentUploads(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT upload.id::text
		FROM project_control_content_uploads upload
		WHERE (upload.status = 'open' AND upload.expires_at <= now())
		   OR (upload.status IN ('committed', 'aborted', 'expired') AND EXISTS (
			SELECT 1 FROM project_control_content_upload_chunks chunk WHERE chunk.upload_id = upload.id
		   ))
		ORDER BY upload.created_at, upload.id
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}
	uploadIDs := make([]string, 0, limit)
	for rows.Next() {
		var uploadID string
		if err := rows.Scan(&uploadID); err != nil {
			rows.Close()
			return 0, err
		}
		uploadIDs = append(uploadIDs, uploadID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(uploadIDs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_content_uploads
		SET status = 'expired', aborted_at = now()
		WHERE id = ANY($1::uuid[]) AND status = 'open' AND expires_at <= now()
	`, uploadIDs); err != nil {
		return 0, err
	}
	storageKeys := make(map[string][]string, len(uploadIDs))
	chunkRows, err := tx.Query(ctx, `
		SELECT upload_id::text, storage_key
		FROM project_control_content_upload_chunks
		WHERE upload_id = ANY($1::uuid[])
		ORDER BY upload_id, chunk_index
	`, uploadIDs)
	if err != nil {
		return 0, err
	}
	for chunkRows.Next() {
		var uploadID, storageKey string
		if err := chunkRows.Scan(&uploadID, &storageKey); err != nil {
			chunkRows.Close()
			return 0, err
		}
		storageKeys[uploadID] = append(storageKeys[uploadID], storageKey)
	}
	if err := chunkRows.Err(); err != nil {
		chunkRows.Close()
		return 0, err
	}
	chunkRows.Close()
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	cleaned := 0
	for _, uploadID := range uploadIDs {
		allDeleted := true
		for _, storageKey := range storageKeys[uploadID] {
			if err := s.storage.DeleteObject(ctx, storageKey); err != nil {
				allDeleted = false
			}
		}
		if !allDeleted {
			continue
		}
		if _, err := s.db.Exec(ctx, `
			DELETE FROM project_control_content_upload_chunks
			WHERE upload_id = $1
			  AND EXISTS (
				SELECT 1 FROM project_control_content_uploads upload
				WHERE upload.id = $1 AND upload.status IN ('committed', 'aborted', 'expired')
			  )
		`, uploadID); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func projectControlContentMetadata(document projectControlContentDocument) map[string]any {
	return map[string]any{
		"targetType": document.TargetType, "targetId": document.TargetID,
		"revision": document.Revision, "contentHash": document.ContentHash,
		"format": document.Format, "totalBytes": len([]byte(document.Content)),
		"totalCharacters":       utf8.RuneCountInString(document.Content),
		"recommendedChunkBytes": projectControlDefaultContentChunkBytes,
		"maximumChunkBytes":     projectControlMaximumContentChunkBytes,
	}
}

func utf8ContentChunk(content string, offset, maxBytes int) (string, int, error) {
	encoded := []byte(content)
	if offset < 0 || offset > len(encoded) {
		return "", 0, fmt.Errorf("cursor 超出内容范围")
	}
	if offset < len(encoded) && !utf8.RuneStart(encoded[offset]) {
		return "", 0, fmt.Errorf("cursor 不在 UTF-8 字符边界")
	}
	end := offset + maxBytes
	if end > len(encoded) {
		end = len(encoded)
	}
	for end > offset && end < len(encoded) && !utf8.RuneStart(encoded[end]) {
		end--
	}
	if end == offset && end < len(encoded) {
		_, width := utf8.DecodeRune(encoded[offset:])
		if width <= 0 {
			return "", 0, fmt.Errorf("内容包含无效 UTF-8")
		}
		end = offset + width
	}
	return string(encoded[offset:end]), end, nil
}

func encodeProjectControlContentCursor(cursor projectControlContentCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeProjectControlContentCursor(value string) (projectControlContentCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return projectControlContentCursor{}, err
	}
	var cursor projectControlContentCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return projectControlContentCursor{}, err
	}
	if cursor.Version != 1 || cursor.TargetType == "" || cursor.TargetID == "" ||
		cursor.Revision < 1 || len(cursor.ContentHash) != 64 || cursor.Offset < 0 {
		return projectControlContentCursor{}, fmt.Errorf("invalid content cursor")
	}
	return cursor, nil
}
