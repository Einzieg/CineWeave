package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type artifactListActionInput struct {
	Type    string `json:"type"`
	Limit   int    `json:"limit"`
	Cursor  string `json:"cursor"`
	Preview bool   `json:"includePreviewUrl"`
	Expires int    `json:"previewExpiresSeconds"`
}

type artifactGetActionInput struct {
	ArtifactID string `json:"artifactId"`
}

type artifactPreviewActionInput struct {
	ArtifactID     string `json:"artifactId"`
	ExpiresSeconds int    `json:"expiresSeconds"`
}

type artifactListActionPage struct {
	Items      []Artifact `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type artifactPreviewActionResult struct {
	ArtifactID string    `json:"artifactId"`
	StorageKey string    `json:"storageKey"`
	URL        string    `json:"url"`
	Method     string    `json:"method"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type artifactActionCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func decodeArtifactListActionInput(raw json.RawMessage) (artifactListActionInput, error) {
	var input artifactListActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return artifactListActionInput{}, controlValidationError("artifact.list 输入格式无效")
	}
	input.Type = strings.TrimSpace(input.Type)
	limit, err := normalizeProjectControlPageLimit(input.Limit, 50, 100)
	if err != nil {
		return artifactListActionInput{}, err
	}
	input.Limit = limit
	if input.Expires <= 0 {
		input.Expires = 900
	}
	if input.Expires < 60 || input.Expires > 3600 {
		return artifactListActionInput{}, controlValidationError("previewExpiresSeconds 必须在 60 到 3600 之间")
	}
	if _, err := decodeArtifactActionCursor(input.Cursor); err != nil {
		return artifactListActionInput{}, err
	}
	return input, nil
}

func decodeArtifactGetActionInput(raw json.RawMessage) (artifactGetActionInput, error) {
	var input artifactGetActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return artifactGetActionInput{}, controlValidationError("artifact.get 输入格式无效")
	}
	input.ArtifactID = strings.TrimSpace(input.ArtifactID)
	if uuid.Validate(input.ArtifactID) != nil {
		return artifactGetActionInput{}, controlValidationError("artifactId 无效")
	}
	return input, nil
}

func decodeArtifactPreviewActionInput(raw json.RawMessage) (artifactPreviewActionInput, error) {
	var input artifactPreviewActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return artifactPreviewActionInput{}, controlValidationError("artifact.preview_url 输入格式无效")
	}
	input.ArtifactID = strings.TrimSpace(input.ArtifactID)
	if uuid.Validate(input.ArtifactID) != nil {
		return artifactPreviewActionInput{}, controlValidationError("artifactId 无效")
	}
	if input.ExpiresSeconds <= 0 {
		input.ExpiresSeconds = 900
	}
	if input.ExpiresSeconds < 60 || input.ExpiresSeconds > 86400 {
		return artifactPreviewActionInput{}, controlValidationError("expiresSeconds 必须在 60 到 86400 之间")
	}
	return input, nil
}

func (s *Server) listProjectArtifactsAction(ctx context.Context, project Project, input artifactListActionInput) (artifactListActionPage, error) {
	cursor, err := decodeArtifactActionCursor(input.Cursor)
	if err != nil {
		return artifactListActionPage{}, err
	}
	if input.Preview && s.storage == nil {
		return artifactListActionPage{}, apiError{Status: http.StatusServiceUnavailable, Code: "STORAGE_UNAVAILABLE", Message: "对象存储尚未配置", Retryable: true}
	}
	rows, err := s.db.Query(ctx, `
		SELECT artifact.id, artifact.organization_id, artifact.project_id, artifact.workflow_run_id,
		       artifact.node_run_id, artifact.type, artifact.storage_key, artifact.mime_type,
		       artifact.content_hash, artifact.prompt_hash, artifact.model_id, artifact.metadata, artifact.created_at
		FROM artifacts artifact
		LEFT JOIN projects project ON project.id = artifact.project_id
		WHERE artifact.organization_id = $1
		  AND artifact.project_id = $2
		  AND ($3 = '' OR artifact.type = $3)
		  AND ($4::timestamptz IS NULL OR (artifact.created_at, artifact.id) < ($4::timestamptz, $5::uuid))
		  AND (
		    artifact.production_generation_id IS NULL
		    OR artifact.production_generation_id = project.active_video_production_generation_id
		    OR EXISTS (SELECT 1 FROM asset_references ref WHERE ref.artifact_id = artifact.id AND ref.status = 'ready')
		    OR EXISTS (
		      SELECT 1 FROM canonical_assets asset
		      WHERE asset.primary_reference_artifact_id = artifact.id OR asset.reference_artifact_id = artifact.id
		    )
		    OR EXISTS (SELECT 1 FROM novel_chapters chapter WHERE chapter.content_artifact_id = artifact.id)
		    OR EXISTS (SELECT 1 FROM novels novel WHERE novel.raw_artifact_id = artifact.id OR novel.clean_artifact_id = artifact.id)
		    OR EXISTS (SELECT 1 FROM script_versions version WHERE version.content_artifact_id = artifact.id)
		  )
		ORDER BY artifact.created_at DESC, artifact.id DESC
		LIMIT $6
	`, project.OrganizationID, project.ID, input.Type, nullableTime(cursor.CreatedAt), nullableUUID(cursor.ID), input.Limit+1)
	if err != nil {
		return artifactListActionPage{}, err
	}
	defer rows.Close()
	items := make([]Artifact, 0, input.Limit+1)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return artifactListActionPage{}, err
		}
		if input.Preview {
			if err := s.attachArtifactPreview(ctx, &item, input.Expires); err != nil {
				return artifactListActionPage{}, err
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return artifactListActionPage{}, err
	}
	page := artifactListActionPage{Items: items}
	if len(items) > input.Limit {
		last := items[input.Limit-1]
		page.Items = items[:input.Limit]
		page.NextCursor, err = encodeArtifactActionCursor(last)
		if err != nil {
			return artifactListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) getProjectArtifactAction(ctx context.Context, project Project, input artifactGetActionInput) (Artifact, error) {
	item, err := s.artifactByID(ctx, input.ArtifactID)
	if err != nil {
		return Artifact{}, err
	}
	if item.OrganizationID != project.OrganizationID || item.ProjectID == nil || *item.ProjectID != project.ID {
		return Artifact{}, newAPIError(http.StatusNotFound, "ARTIFACT_NOT_FOUND", "未找到项目成果")
	}
	return item, nil
}

func (s *Server) createProjectArtifactPreviewAction(ctx context.Context, project Project, input artifactPreviewActionInput) (artifactPreviewActionResult, error) {
	item, err := s.getProjectArtifactAction(ctx, project, artifactGetActionInput{ArtifactID: input.ArtifactID})
	if err != nil {
		return artifactPreviewActionResult{}, err
	}
	return s.presignArtifactPreview(ctx, item, input.ExpiresSeconds)
}

func (s *Server) artifactByID(ctx context.Context, artifactID string) (Artifact, error) {
	item, err := scanArtifact(s.db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_at
		FROM artifacts
		WHERE id = $1
	`, artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, newAPIError(http.StatusNotFound, "ARTIFACT_NOT_FOUND", "未找到成果")
	}
	return item, err
}

func (s *Server) presignArtifactPreview(ctx context.Context, item Artifact, expiresSeconds int) (artifactPreviewActionResult, error) {
	if s.storage == nil {
		return artifactPreviewActionResult{}, apiError{Status: http.StatusServiceUnavailable, Code: "STORAGE_UNAVAILABLE", Message: "对象存储尚未配置", Retryable: true}
	}
	if item.StorageKey == nil || strings.TrimSpace(*item.StorageKey) == "" {
		return artifactPreviewActionResult{}, newAPIError(http.StatusUnprocessableEntity, "ARTIFACT_HAS_NO_STORAGE_OBJECT", "成果没有可读取的存储对象")
	}
	if !artifactCanPreview(item) {
		return artifactPreviewActionResult{}, newAPIError(http.StatusUnprocessableEntity, "UNSUPPORTED_PREVIEW_TYPE", "当前成果类型不支持预览")
	}
	presigned, err := s.storage.PresignGetObject(ctx, *item.StorageKey, previewURLExpiry(expiresSeconds))
	if err != nil {
		return artifactPreviewActionResult{}, err
	}
	return artifactPreviewActionResult{
		ArtifactID: item.ID, StorageKey: presigned.StorageKey, URL: presigned.URL,
		Method: presigned.Method, ExpiresAt: presigned.ExpiresAt,
	}, nil
}

func (s *Server) attachArtifactPreview(ctx context.Context, item *Artifact, expiresSeconds int) error {
	if item.StorageKey == nil || strings.TrimSpace(*item.StorageKey) == "" || !artifactCanPreview(*item) {
		return nil
	}
	preview, err := s.presignArtifactPreview(ctx, *item, expiresSeconds)
	if err != nil {
		return err
	}
	item.PreviewURL = &preview.URL
	item.PreviewExpiresAt = &preview.ExpiresAt
	return nil
}

func encodeArtifactActionCursor(item Artifact) (string, error) {
	payload, err := json.Marshal(artifactActionCursor{Version: 1, CreatedAt: item.CreatedAt.UTC(), ID: item.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeArtifactActionCursor(value string) (artifactActionCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return artifactActionCursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return artifactActionCursor{}, controlValidationError("cursor 无效")
	}
	var cursor artifactActionCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 || cursor.CreatedAt.IsZero() || uuid.Validate(cursor.ID) != nil {
		return artifactActionCursor{}, controlValidationError("cursor 无效")
	}
	return cursor, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func artifactListAgentResult(arguments map[string]any, page artifactListActionPage) agentToolResult {
	return agentToolOK("artifact.list", arguments, fmt.Sprintf("读取到 %d 个项目成果。", len(page.Items)), map[string]any{
		"items": page.Items, "nextCursor": page.NextCursor,
	})
}

func artifactGetAgentResult(arguments map[string]any, item Artifact) agentToolResult {
	return agentToolOK("artifact.get", arguments, "已读取项目成果。", map[string]any{"artifact": item})
}

func artifactPreviewAgentResult(arguments map[string]any, preview artifactPreviewActionResult) agentToolResult {
	return agentToolOK("artifact.preview_url", arguments, "已生成成果预览链接。", map[string]any{
		"artifactId": preview.ArtifactID, "storageKey": preview.StorageKey, "url": preview.URL,
		"method": preview.Method, "expiresAt": preview.ExpiresAt,
	})
}
