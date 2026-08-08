package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const assetActionMaximumPageSize = 100

type assetListActionInput struct {
	AssetType    string `json:"assetType"`
	Status       string `json:"status"`
	ReviewStatus string `json:"reviewStatus"`
	StaleState   string `json:"staleState"`
	PromptReady  *bool  `json:"promptReady,omitempty"`
	Limit        int    `json:"limit"`
	Cursor       string `json:"cursor"`
}

type assetGetActionInput struct {
	AssetID           string `json:"assetId"`
	AssetName         string `json:"assetName"`
	IncludePreviewURL bool   `json:"includePreviewUrl"`
}

type assetImpactActionInput struct {
	AssetID string `json:"assetId"`
}

type assetReferenceListActionInput struct {
	AssetID           string `json:"assetId"`
	Status            string `json:"status"`
	IncludePreviewURL bool   `json:"includePreviewUrl"`
	Limit             int    `json:"limit"`
	Cursor            string `json:"cursor"`
}

type assetActionSummary struct {
	ID                   string    `json:"id"`
	AssetType            string    `json:"assetType"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	Status               string    `json:"status"`
	ReviewStatus         string    `json:"reviewStatus"`
	StaleState           string    `json:"staleState"`
	ManualOverride       bool      `json:"manualOverride"`
	LockReference        bool      `json:"lockReference"`
	Revision             int64     `json:"revision"`
	PromptRevision       int64     `json:"promptRevision"`
	PromptReady          bool      `json:"promptReady"`
	HasPrimaryReference  bool      `json:"hasPrimaryReference"`
	SceneCount           int       `json:"sceneCount"`
	StoryboardShotCount  int       `json:"storyboardShotCount"`
	ReferenceCount       int       `json:"referenceCount"`
	ShotRequirementCount int       `json:"shotRequirementCount"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type assetListActionPage struct {
	Items      []assetActionSummary `json:"items"`
	Limit      int                  `json:"limit"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

type assetReferenceListActionPage struct {
	AssetID    string           `json:"assetId"`
	Items      []AssetReference `json:"items"`
	Limit      int              `json:"limit"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

func decodeAssetListActionInput(raw json.RawMessage) (assetListActionInput, error) {
	var input assetListActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetListActionInput{}, controlValidationError("asset.list 输入格式无效")
	}
	return input, nil
}

func decodeAssetGetActionInput(raw json.RawMessage) (assetGetActionInput, error) {
	var input assetGetActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetGetActionInput{}, controlValidationError("asset.get 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.AssetName = strings.TrimSpace(input.AssetName)
	if input.AssetID == "" && input.AssetName == "" {
		return assetGetActionInput{}, controlValidationError("assetId 与 assetName 至少提供一个；优先使用 asset.list 返回的准确 assetId")
	}
	if input.AssetID != "" && uuid.Validate(input.AssetID) != nil {
		return assetGetActionInput{}, controlValidationError("assetId 无效")
	}
	return input, nil
}

func decodeAssetImpactActionInput(raw json.RawMessage) (assetImpactActionInput, error) {
	var input assetImpactActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetImpactActionInput{}, controlValidationError("asset.impact 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	if uuid.Validate(input.AssetID) != nil {
		return assetImpactActionInput{}, controlValidationError("assetId 无效")
	}
	return input, nil
}

func decodeAssetReferenceListActionInput(raw json.RawMessage) (assetReferenceListActionInput, error) {
	var input assetReferenceListActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetReferenceListActionInput{}, controlValidationError("asset.reference.list 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	if uuid.Validate(input.AssetID) != nil {
		return assetReferenceListActionInput{}, controlValidationError("assetId 无效")
	}
	return input, nil
}

func (s *Server) listCanonicalAssetsAction(ctx context.Context, project Project, input assetListActionInput) (assetListActionPage, error) {
	limit, err := normalizeProjectControlPageLimit(input.Limit, 30, assetActionMaximumPageSize)
	if err != nil {
		return assetListActionPage{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	if _, valid := parseArchivedStatusFilter(status); !valid {
		return assetListActionPage{}, controlValidationError("status 必须是 active、archived 或 all")
	}
	assetType := strings.TrimSpace(input.AssetType)
	if assetType != "" && !validCanonicalAssetType(assetType) {
		return assetListActionPage{}, controlValidationError("assetType 必须是 character、scene 或 prop")
	}
	reviewStatus := strings.TrimSpace(input.ReviewStatus)
	if reviewStatus != "" && !validAssetActionReviewStatus(reviewStatus) {
		return assetListActionPage{}, controlValidationError("reviewStatus 必须是 pending、approved、rejected 或 needs_edit")
	}
	staleState := strings.TrimSpace(input.StaleState)
	if staleState != "" && !validAssetActionStaleState(staleState) {
		return assetListActionPage{}, controlValidationError("staleState 必须是 fresh、upstream_changed 或 needs_regeneration")
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return assetListActionPage{}, err
	}
	var promptReady any
	if input.PromptReady != nil {
		promptReady = *input.PromptReady
	}
	rows, err := s.db.Query(ctx, `
		SELECT
			a.id::text,
			a.asset_type,
			a.name,
			a.description,
			a.status,
			a.review_status,
			a.stale_state,
			a.manual_override,
			a.lock_reference,
			a.revision,
			a.prompt_revision,
			(btrim(COALESCE(a.base_prompt, '')) <> '' AND btrim(COALESCE(a.consistency_prompt, '')) <> '') AS prompt_ready,
			(
				a.primary_reference_artifact_id IS NOT NULL
				OR a.primary_reference_media_file_id IS NOT NULL
				OR btrim(COALESCE(a.primary_reference_storage_key, '')) <> ''
				OR a.reference_artifact_id IS NOT NULL
				OR a.reference_media_file_id IS NOT NULL
				OR btrim(COALESCE(a.reference_storage_key, '')) <> ''
			) AS has_primary_reference,
			(
				SELECT count(*)::int
				FROM scene_asset_links link
				WHERE link.project_id = a.project_id AND link.asset_id = a.id
			) AS scene_count,
			(
				SELECT count(DISTINCT shot.id)::int
				FROM scene_asset_links link
				JOIN storyboard_shots shot
				  ON shot.project_id = link.project_id
				 AND shot.script_scene_id = link.script_scene_id
				 AND shot.deleted_at IS NULL
				WHERE link.project_id = a.project_id AND link.asset_id = a.id
			) AS storyboard_shot_count,
			(
				SELECT count(*)::int
				FROM asset_references reference
				WHERE reference.project_id = a.project_id
				  AND reference.asset_id = a.id
				  AND reference.status = 'ready'
			) AS reference_count,
			(
				SELECT count(*)::int
				FROM shot_asset_requirements requirement
				WHERE requirement.project_id = a.project_id
				  AND requirement.asset_id = a.id
				  AND requirement.production_generation_id = (
					SELECT active_video_production_generation_id FROM projects WHERE id = a.project_id
				  )
			) AS shot_requirement_count,
			a.updated_at
		FROM canonical_assets a
		WHERE a.project_id = $1
		  AND ($2 = '' OR a.asset_type = $2)
		  AND (
			$3 = 'all'
			OR ($3 = 'archived' AND COALESCE(a.status, 'draft') = 'archived')
			OR ($3 = 'active' AND COALESCE(a.status, 'draft') <> 'archived')
		  )
		  AND ($4 = '' OR a.review_status = $4)
		  AND ($5 = '' OR a.stale_state = $5)
		  AND (
			$6::boolean IS NULL
			OR (btrim(COALESCE(a.base_prompt, '')) <> '' AND btrim(COALESCE(a.consistency_prompt, '')) <> '') = $6
		  )
		ORDER BY a.asset_type ASC, lower(a.name) ASC, a.id ASC
		LIMIT $7 OFFSET $8
	`, project.ID, assetType, status, reviewStatus, staleState, promptReady, limit+1, offset)
	if err != nil {
		return assetListActionPage{}, err
	}
	defer rows.Close()
	items := make([]assetActionSummary, 0, limit+1)
	for rows.Next() {
		var item assetActionSummary
		if err := rows.Scan(
			&item.ID, &item.AssetType, &item.Name, &item.Description,
			&item.Status, &item.ReviewStatus, &item.StaleState,
			&item.ManualOverride, &item.LockReference, &item.Revision,
			&item.PromptRevision, &item.PromptReady, &item.HasPrimaryReference,
			&item.SceneCount, &item.StoryboardShotCount, &item.ReferenceCount,
			&item.ShotRequirementCount, &item.UpdatedAt,
		); err != nil {
			return assetListActionPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return assetListActionPage{}, err
	}
	page := assetListActionPage{Items: items, Limit: limit}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return assetListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) getCanonicalAssetAction(ctx context.Context, project Project, input assetGetActionInput) (CanonicalAsset, error) {
	asset, err := s.resolveCanonicalAssetReferenceContext(ctx, project.ID, input.AssetID, input.AssetName)
	if err != nil {
		return CanonicalAsset{}, err
	}
	if input.IncludePreviewURL && s.storage == nil {
		return CanonicalAsset{}, newAPIError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储未配置，无法生成预览地址")
	}
	request := requestWithContext(ctx)
	items := []CanonicalAsset{asset}
	if err := s.attachCanonicalAssetSceneLinks(request, project.ID, items); err != nil {
		return CanonicalAsset{}, err
	}
	if err := s.attachCanonicalAssetReferences(request, project.ID, items, input.IncludePreviewURL); err != nil {
		return CanonicalAsset{}, err
	}
	if err := s.attachCanonicalAssetShotRequirements(request, project.ID, items); err != nil {
		return CanonicalAsset{}, err
	}
	return items[0], nil
}

func (s *Server) getCanonicalAssetImpactAction(ctx context.Context, project Project, input assetImpactActionInput) (OutputImpact, error) {
	return s.canonicalAssetImpact(requestWithContext(ctx), project.ID, input.AssetID)
}

func (s *Server) listAssetReferencesAction(
	ctx context.Context,
	project Project,
	input assetReferenceListActionInput,
) (assetReferenceListActionPage, error) {
	if _, err := s.resolveCanonicalAssetReferenceContext(ctx, project.ID, input.AssetID, ""); err != nil {
		return assetReferenceListActionPage{}, err
	}
	limit, err := normalizeProjectControlPageLimit(input.Limit, 30, assetActionMaximumPageSize)
	if err != nil {
		return assetReferenceListActionPage{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "failed" && status != "all" {
		return assetReferenceListActionPage{}, controlValidationError("status 必须是 active、archived、failed 或 all")
	}
	if input.IncludePreviewURL && s.storage == nil {
		return assetReferenceListActionPage{}, newAPIError(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "对象存储未配置，无法生成预览地址")
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return assetReferenceListActionPage{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, organization_id, project_id, asset_id, reference_type, title, description,
		       artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		       is_primary, status, metadata, created_by, created_at, updated_at
		FROM asset_references
		WHERE project_id = $1 AND asset_id = $2
		  AND (
			$3 = 'all'
			OR ($3 = 'active' AND status = 'ready')
			OR ($3 = 'archived' AND status = 'archived')
			OR ($3 = 'failed' AND status = 'failed')
		  )
		ORDER BY is_primary DESC, created_at DESC, id ASC
		LIMIT $4 OFFSET $5
	`, project.ID, input.AssetID, status, limit+1, offset)
	if err != nil {
		return assetReferenceListActionPage{}, err
	}
	defer rows.Close()
	items := make([]AssetReference, 0, limit+1)
	request := requestWithContext(ctx)
	previewExpires := previewURLExpiryFromRequest(request)
	for rows.Next() {
		item, err := scanAssetReference(rows)
		if err != nil {
			return assetReferenceListActionPage{}, err
		}
		if input.IncludePreviewURL && item.StorageKey != nil && strings.TrimSpace(*item.StorageKey) != "" {
			presigned, err := s.storage.PresignGetObject(ctx, *item.StorageKey, previewExpires)
			if err != nil {
				return assetReferenceListActionPage{}, err
			}
			item.PreviewURL = &presigned.URL
			item.PreviewExpiresAt = &presigned.ExpiresAt
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return assetReferenceListActionPage{}, err
	}
	page := assetReferenceListActionPage{AssetID: input.AssetID, Items: items, Limit: limit}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return assetReferenceListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) resolveCanonicalAssetReferenceContext(
	ctx context.Context,
	projectID, assetID, assetName string,
) (CanonicalAsset, error) {
	assetID = strings.TrimSpace(assetID)
	assetName = strings.TrimSpace(assetName)
	if assetID != "" {
		if uuid.Validate(assetID) != nil {
			return CanonicalAsset{}, controlValidationError("assetId 无效")
		}
		asset, err := s.canonicalAssetWithDB(ctx, s.db, projectID, assetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return CanonicalAsset{}, newAPIError(http.StatusNotFound, "ASSET_NOT_FOUND", "未找到资产")
		}
		return asset, err
	}
	if assetName == "" {
		return CanonicalAsset{}, controlValidationError("assetId 与 assetName 至少提供一个")
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text
		FROM canonical_assets
		WHERE project_id = $1
		  AND lower(name) = lower($2)
		  AND COALESCE(status, 'draft') <> 'archived'
		ORDER BY updated_at DESC, created_at DESC, id ASC
		LIMIT 2
	`, projectID, assetName)
	if err != nil {
		return CanonicalAsset{}, err
	}
	defer rows.Close()
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return CanonicalAsset{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return CanonicalAsset{}, err
	}
	if len(ids) == 0 {
		return CanonicalAsset{}, newAPIError(http.StatusNotFound, "ASSET_NOT_FOUND", "未找到该名称的资产")
	}
	if len(ids) > 1 {
		return CanonicalAsset{}, newAPIError(http.StatusUnprocessableEntity, "AMBIGUOUS_ASSET_NAME", "存在多个同名资产，请使用 asset.list 返回的准确 assetId")
	}
	return s.canonicalAssetWithDB(ctx, s.db, projectID, ids[0])
}

func assetListAgentResult(arguments map[string]any, page assetListActionPage) agentToolResult {
	data := map[string]any{"items": page.Items, "limit": page.Limit}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return agentToolOK("asset.list", arguments, fmt.Sprintf("找到 %d 个核心资产。", len(page.Items)), data)
}

func assetGetAgentResult(arguments map[string]any, asset CanonicalAsset) agentToolResult {
	return agentToolOK("asset.get", arguments, "已读取完整资产卡《"+asset.Name+"》。", map[string]any{
		"asset": asset,
	})
}

func assetImpactAgentResult(arguments map[string]any, impact OutputImpact) agentToolResult {
	return agentToolOK("asset.impact", arguments, "已读取资产归档影响。", map[string]any{"impact": impact})
}

func assetReferenceListAgentResult(arguments map[string]any, page assetReferenceListActionPage) agentToolResult {
	data := map[string]any{"assetId": page.AssetID, "items": page.Items, "limit": page.Limit}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return agentToolOK("asset.reference.list", arguments, fmt.Sprintf("找到 %d 个资产参考图版本。", len(page.Items)), data)
}

func validAssetActionReviewStatus(value string) bool {
	return value == "pending" || value == "approved" || value == "rejected" || value == "needs_edit"
}

func validAssetActionStaleState(value string) bool {
	return value == "fresh" || value == "upstream_changed" || value == "needs_regeneration"
}
