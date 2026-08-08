package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/assetprompts"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type CanonicalAsset struct {
	ID                          string                 `json:"id"`
	OrganizationID              string                 `json:"organizationId"`
	ProjectID                   string                 `json:"projectId"`
	AssetType                   string                 `json:"assetType"`
	Name                        string                 `json:"name"`
	Description                 string                 `json:"description"`
	Profile                     json.RawMessage        `json:"profile"`
	BasePrompt                  *string                `json:"basePrompt,omitempty"`
	ConsistencyPrompt           *string                `json:"consistencyPrompt,omitempty"`
	NegativePrompt              *string                `json:"negativePrompt,omitempty"`
	VisualTraits                json.RawMessage        `json:"visualTraits"`
	PrimaryReferenceArtifactID  *string                `json:"primaryReferenceArtifactId,omitempty"`
	PrimaryReferenceMediaFileID *string                `json:"primaryReferenceMediaFileId,omitempty"`
	PrimaryReferenceStorageKey  *string                `json:"primaryReferenceStorageKey,omitempty"`
	LockReference               bool                   `json:"lockReference"`
	ReferenceArtifactID         *string                `json:"referenceArtifactId,omitempty"`
	ReferenceMediaFileID        *string                `json:"referenceMediaFileId,omitempty"`
	ReferenceStorageKey         *string                `json:"referenceStorageKey,omitempty"`
	Status                      string                 `json:"status"`
	ReviewStatus                string                 `json:"reviewStatus"`
	ManualOverride              bool                   `json:"manualOverride"`
	StaleState                  string                 `json:"staleState"`
	EditedBy                    *string                `json:"editedBy,omitempty"`
	EditedAt                    *time.Time             `json:"editedAt,omitempty"`
	SourceScriptIDs             json.RawMessage        `json:"sourceScriptIds"`
	Metadata                    json.RawMessage        `json:"metadata"`
	CreatedBy                   *string                `json:"createdBy,omitempty"`
	CreatedAt                   time.Time              `json:"createdAt"`
	UpdatedAt                   time.Time              `json:"updatedAt"`
	Revision                    int64                  `json:"revision"`
	PromptRevision              int64                  `json:"promptRevision"`
	SceneLinks                  []AssetSceneLink       `json:"sceneLinks,omitempty"`
	References                  []AssetReference       `json:"references,omitempty"`
	ShotRequirements            []ShotAssetRequirement `json:"shotRequirements,omitempty"`
	SceneCount                  int                    `json:"sceneCount"`
	StoryboardShotCount         int                    `json:"storyboardShotCount"`
	ReferenceCount              int                    `json:"referenceCount"`
	ShotRequirementCount        int                    `json:"shotRequirementCount"`
}

type AssetSceneLink struct {
	ScriptSceneID       string  `json:"scriptSceneId"`
	SceneNo             int     `json:"sceneNo"`
	Title               string  `json:"title"`
	Location            string  `json:"location,omitempty"`
	AssetRole           *string `json:"assetRole,omitempty"`
	UsageNote           *string `json:"usageNote,omitempty"`
	StoryboardShotCount int     `json:"storyboardShotCount"`
}

type AssetReference struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	AssetID          string          `json:"assetId"`
	ReferenceType    string          `json:"referenceType"`
	Title            *string         `json:"title,omitempty"`
	Description      *string         `json:"description,omitempty"`
	ArtifactID       *string         `json:"artifactId,omitempty"`
	MediaFileID      *string         `json:"mediaFileId,omitempty"`
	StorageKey       *string         `json:"storageKey,omitempty"`
	PreviewURL       *string         `json:"previewUrl,omitempty"`
	PreviewExpiresAt *time.Time      `json:"previewExpiresAt,omitempty"`
	Prompt           *string         `json:"prompt,omitempty"`
	PromptVersionID  *string         `json:"promptVersionId,omitempty"`
	PromptHash       *string         `json:"promptHash,omitempty"`
	IsPrimary        bool            `json:"isPrimary"`
	Status           string          `json:"status"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedBy        *string         `json:"createdBy,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type GenerateAssetCardRequest struct {
	Force                       bool   `json:"force"`
	VisualManualPromptVersionID string `json:"visualManualPromptVersionId,omitempty"`
}

type GenerateAssetCardResponse struct {
	AssetID                     string          `json:"assetId"`
	Profile                     json.RawMessage `json:"profile"`
	BasePrompt                  string          `json:"basePrompt"`
	ConsistencyPrompt           string          `json:"consistencyPrompt"`
	NegativePrompt              string          `json:"negativePrompt"`
	ProviderCallID              string          `json:"providerCallId,omitempty"`
	ModelID                     string          `json:"modelId,omitempty"`
	VisualManualPromptVersionID string          `json:"visualManualPromptVersionId,omitempty"`
	VisualManualTemplateKey     string          `json:"visualManualTemplateKey,omitempty"`
	VisualStyleSlug             string          `json:"visualStyleSlug,omitempty"`
	AssetTypeTemplateKey        string          `json:"assetTypeTemplateKey,omitempty"`
	Applied                     bool            `json:"applied"`
}

type assetCardDraft = assetprompts.CardDraft

type ShotAssetRequirement struct {
	ID                 string          `json:"id"`
	OrganizationID     string          `json:"organizationId"`
	ProjectID          string          `json:"projectId"`
	WorkflowRunID      *string         `json:"workflowRunId,omitempty"`
	StoryboardShotID   string          `json:"storyboardShotId"`
	AssetID            string          `json:"assetId"`
	AssetType          string          `json:"assetType,omitempty"`
	AssetName          string          `json:"assetName,omitempty"`
	RequirementType    string          `json:"requirementType"`
	RoleInShot         *string         `json:"roleInShot,omitempty"`
	Costume            *string         `json:"costume,omitempty"`
	Pose               *string         `json:"pose,omitempty"`
	Expression         *string         `json:"expression,omitempty"`
	Action             *string         `json:"action,omitempty"`
	CameraRelation     *string         `json:"cameraRelation,omitempty"`
	SceneState         *string         `json:"sceneState,omitempty"`
	PropState          *string         `json:"propState,omitempty"`
	Prompt             *string         `json:"prompt,omitempty"`
	DerivedArtifactID  *string         `json:"derivedArtifactId,omitempty"`
	DerivedMediaFileID *string         `json:"derivedMediaFileId,omitempty"`
	DerivedStorageKey  *string         `json:"derivedStorageKey,omitempty"`
	Status             string          `json:"status"`
	ReviewStatus       string          `json:"reviewStatus"`
	ManualOverride     bool            `json:"manualOverride"`
	StaleState         string          `json:"staleState"`
	EditedBy           *string         `json:"editedBy,omitempty"`
	EditedAt           *time.Time      `json:"editedAt,omitempty"`
	Metadata           json.RawMessage `json:"metadata"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	Asset              *CanonicalAsset `json:"asset,omitempty"`
}

func (s *Server) listCanonicalAssets(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	assetType := strings.TrimSpace(r.URL.Query().Get("filter[type]"))
	includePreview := strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true")
	if includePreview && s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	statusFilter, valid := parseArchivedStatusFilter(r.URL.Query().Get("filter[status]"))
	if !valid {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "canonical asset status filter is invalid", nil, false)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		       revision, prompt_revision
		FROM canonical_assets
		WHERE project_id = $1
		  AND ($2 = '' OR asset_type = $2)
		  AND (
		    $3 = 'all'
		    OR ($3 = 'archived' AND COALESCE(status, 'draft') = 'archived')
		    OR ($3 = 'active' AND COALESCE(status, 'draft') <> 'archived')
		  )
		ORDER BY asset_type, name
	`, project.ID, assetType, statusFilter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]CanonicalAsset, 0)
	for rows.Next() {
		item, err := scanCanonicalAsset(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := s.attachCanonicalAssetSceneLinks(r, project.ID, items); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.attachCanonicalAssetReferences(r, project.ID, items, includePreview); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCanonicalAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	item, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items := []CanonicalAsset{item}
	if err := s.attachCanonicalAssetSceneLinks(r, project.ID, items); err != nil {
		s.writeError(w, r, err)
		return
	}
	includePreview := strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true")
	if includePreview && s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	if err := s.attachCanonicalAssetReferences(r, project.ID, items, includePreview); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.attachCanonicalAssetShotRequirements(r, project.ID, items); err != nil {
		s.writeError(w, r, err)
		return
	}
	item = items[0]
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateCanonicalAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		IdempotencyKey    string          `json:"idempotencyKey"`
		AssetType         *string         `json:"assetType"`
		Name              *string         `json:"name"`
		Description       *string         `json:"description"`
		Profile           json.RawMessage `json:"profile"`
		BasePrompt        *string         `json:"basePrompt"`
		ConsistencyPrompt *string         `json:"consistencyPrompt"`
		NegativePrompt    *string         `json:"negativePrompt"`
		LockReference     *bool           `json:"lockReference"`
		VisualTraits      json.RawMessage `json:"visualTraits"`
		Metadata          json.RawMessage `json:"metadata"`
		Status            *string         `json:"status"`
		ExpectedRevision  int64           `json:"expectedRevision"`
	}
	if !decode(w, r, &req) {
		return
	}
	patch := map[string]any{}
	if req.AssetType != nil {
		patch["assetType"] = *req.AssetType
	}
	if req.Name != nil {
		patch["name"] = *req.Name
	}
	if req.Description != nil {
		patch["description"] = *req.Description
	}
	if len(req.Profile) > 0 {
		patch["profile"] = json.RawMessage(req.Profile)
	}
	if req.BasePrompt != nil {
		patch["basePrompt"] = *req.BasePrompt
	}
	if req.ConsistencyPrompt != nil {
		patch["consistencyPrompt"] = *req.ConsistencyPrompt
	}
	if req.NegativePrompt != nil {
		patch["negativePrompt"] = *req.NegativePrompt
	}
	if req.LockReference != nil {
		patch["lockReference"] = *req.LockReference
	}
	if len(req.VisualTraits) > 0 {
		patch["visualTraits"] = json.RawMessage(req.VisualTraits)
	}
	if len(req.Metadata) > 0 {
		patch["metadata"] = json.RawMessage(req.Metadata)
	}
	if req.Status != nil {
		patch["status"] = *req.Status
	}
	actionInput := mustRawJSON(map[string]any{
		"assetId": r.PathValue("assetId"), "expectedRevision": req.ExpectedRevision, "patch": patch,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "asset.update", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	encodedAsset, err := json.Marshal(result.Data["asset"])
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var item CanonicalAsset
	if err := json.Unmarshal(encodedAsset, &item); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteCanonicalAsset(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
		Reason           string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	actionInput := mustRawJSON(map[string]any{
		"assetId": r.PathValue("assetId"), "expectedRevision": req.ExpectedRevision, "reason": req.Reason,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "asset.delete", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, result.Data, nil)
}

func (s *Server) getCanonicalAssetImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	impact, err := s.canonicalAssetImpact(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, impact, nil)
}

func (s *Server) canonicalAssetImpact(r *http.Request, projectID, assetID string) (OutputImpact, error) {
	if _, err := s.canonicalAsset(r, projectID, assetID); err != nil {
		return OutputImpact{}, err
	}
	var sceneLinkCount, referenceCount, requirementCount, generatedMediaCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			(SELECT count(*) FROM scene_asset_links WHERE project_id = $1 AND asset_id = $2),
			(SELECT count(*) FROM asset_references WHERE project_id = $1 AND asset_id = $2 AND status <> 'archived'),
			(
				SELECT count(*) FROM shot_asset_requirements
				WHERE project_id = $1 AND asset_id = $2
				  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
			),
			(
				SELECT count(*)
				FROM (
					SELECT id FROM asset_references
					WHERE project_id = $1 AND asset_id = $2
					  AND status <> 'archived'
					  AND (artifact_id IS NOT NULL OR media_file_id IS NOT NULL OR COALESCE(storage_key, '') <> '')
					UNION ALL
					SELECT id FROM shot_asset_requirements
					WHERE project_id = $1 AND asset_id = $2
					  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
					  AND (derived_artifact_id IS NOT NULL OR derived_media_file_id IS NOT NULL OR COALESCE(derived_storage_key, '') <> '')
				) media
			)
	`, projectID, assetID).Scan(&sceneLinkCount, &referenceCount, &requirementCount, &generatedMediaCount); err != nil {
		return OutputImpact{}, err
	}
	affected := make([]OutputImpactAffected, 0, 4)
	addAffected := func(entityType string, count int) {
		if count > 0 {
			affected = append(affected, OutputImpactAffected{EntityType: entityType, Count: count})
		}
	}
	addAffected("scene_asset_link", sceneLinkCount)
	addAffected("asset_reference", referenceCount)
	addAffected("shot_asset_requirement", requirementCount)
	addAffected("generated_media", generatedMediaCount)
	warnings := []string{"归档会从默认核心资产列表隐藏该资产，但不会删除参考图、镜头需求、产物或媒体文件。"}
	if sceneLinkCount > 0 || requirementCount > 0 {
		warnings = append(warnings, "该资产已被分场或镜头需求引用，归档后相关下游会标记为需重新生成。")
	}
	return OutputImpact{
		EntityType:      "canonical_asset",
		EntityID:        assetID,
		CanDelete:       true,
		RecommendedMode: "archive",
		DeleteModes:     []string{"archive"},
		Affected:        affected,
		Warnings:        warnings,
	}, nil
}

func (s *Server) generateAssetCard(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	asset, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if asset.Status == "archived" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "canonical asset is archived", nil, false)
		return
	}
	var req GenerateAssetCardRequest
	if !decode(w, r, &req) {
		return
	}
	visualContext, err := s.resolveAssetCardVisualContext(r.Context(), project, req.VisualManualPromptVersionID, asset.AssetType)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	scenes, err := s.assetScenePromptContext(r, project.ID, asset.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	canonicalSource := assetprompts.BuildCanonicalCardSource(asset.AssetType, asset.Description, asset.VisualTraits, scenes)
	variables := map[string]any{
		"project": map[string]any{
			"id":                        project.ID,
			"aspectRatio":               stringValue(project.AspectRatio),
			"videoRatio":                project.VideoRatio,
			"artStyle":                  project.ArtStyle,
			"imageQuality":              project.ImageQuality,
			"videoProductionProfileKey": project.VideoProductionBinding.ProfileKey,
		},
		"visualContext": visualContext.promptVariables(),
		"asset": map[string]any{
			"id":           asset.ID,
			"assetType":    asset.AssetType,
			"name":         asset.Name,
			"description":  canonicalSource.Description,
			"visualTraits": canonicalSource.VisualTraits,
			"canonicalBaselinePolicy": map[string]any{
				"stableIdentityOnly":   true,
				"transientStateTarget": "shot_derived_asset",
			},
		},
		"scenes":                canonicalSource.SceneContext,
		"validationFeedback":    "",
		"previousRejectedDraft": map[string]any{},
	}
	var rendered promptsvc.RenderedPrompt
	var gatewayResp provider.GatewayTextResponse
	var draft assetCardDraft
	providerCallIDs := make([]string, 0, 2)
	for attempt := 0; attempt < 3; attempt++ {
		rendered, gatewayResp, err = s.runTextGatewayPrompt(
			r,
			project,
			"asset_card_generation",
			variables,
			true,
			authz.PermissionAssetWrite,
			provider.BillingContextReasonManualProvider,
		)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		providerCallIDs = append(providerCallIDs, gatewayResp.ProviderCallID)
		draft, err = assetprompts.NormalizeCardDraft(gatewayResp.Output.Text)
		if err == nil {
			err = assetprompts.ValidateGeneratedCardStyle(visualContext.StyleSlug, draft.BasePrompt, draft.ConsistencyPrompt)
		}
		if err == nil {
			err = assetprompts.ValidateCanonicalAssetBaseline(asset.AssetType, draft.BasePrompt, draft.ConsistencyPrompt)
		}
		if err == nil {
			break
		}
		if attempt == 2 {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ASSET_CARD_VISUAL_CONTRACT_FAILED", err.Error(), nil, false)
			return
		}
		variables["validationFeedback"] = err.Error()
		variables["previousRejectedDraft"] = map[string]any{
			"profile":           json.RawMessage(draft.Profile),
			"basePrompt":        draft.BasePrompt,
			"consistencyPrompt": draft.ConsistencyPrompt,
			"negativePrompt":    draft.NegativePrompt,
		}
	}
	applied := !asset.ManualOverride || req.Force
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	metadata := mustRawJSON(map[string]any{
		"providerCallId":              gatewayResp.ProviderCallID,
		"providerCallIds":             providerCallIDs,
		"modelId":                     gatewayResp.ModelID,
		"promptTemplateKey":           rendered.TemplateKey,
		"promptVersionId":             rendered.PromptVersionID,
		"promptHash":                  rendered.RenderedHash,
		"agentSuggestion":             draft,
		"generationMode":              "fresh_from_source",
		"visualManualTemplateKey":     visualContext.ManualTemplateKey,
		"visualManualPromptVersionId": visualContext.ManualPromptVersionID,
		"visualManualContentHash":     visualContext.ManualContentHash,
		"visualStyleSlug":             visualContext.StyleSlug,
		"visualPrefixTemplateKey":     visualContext.PrefixTemplateKey,
		"visualPrefixPromptVersionId": visualContext.PrefixPromptVersionID,
		"assetTypeTemplateKey":        visualContext.AssetTypeTemplateKey,
		"assetTypePromptVersionId":    visualContext.AssetTypePromptVersionID,
	})
	if applied {
		if _, err := tx.Exec(r.Context(), `
			UPDATE canonical_assets
			SET profile = $3,
			    base_prompt = NULLIF($4, ''),
			    consistency_prompt = NULLIF($5, ''),
			    negative_prompt = NULLIF($6, ''),
			    manual_override = false,
			    status = 'prompt_ready',
			    stale_state = 'fresh',
			    metadata = COALESCE(metadata, '{}'::jsonb) || $7,
			    updated_at = now()
			WHERE id = $1 AND project_id = $2
		`, asset.ID, project.ID, draft.Profile, draft.BasePrompt, draft.ConsistencyPrompt, draft.NegativePrompt, metadata); err != nil {
			s.writeError(w, r, err)
			return
		}
		if err := production.MarkAssetDownstreamStale(r.Context(), tx, project.ID, asset.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	} else {
		if _, err := tx.Exec(r.Context(), `
			UPDATE canonical_assets
			SET metadata = COALESCE(metadata, '{}'::jsonb) || $3,
			    updated_at = now()
			WHERE id = $1 AND project_id = $2
		`, asset.ID, project.ID, metadata); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "asset.card.generated", "canonical_asset", asset.ID, mustRawJSON(map[string]any{
		"assetId":                     asset.ID,
		"applied":                     applied,
		"manualOverride":              asset.ManualOverride,
		"force":                       req.Force,
		"visualManualPromptVersionId": visualContext.ManualPromptVersionID,
		"visualManualTemplateKey":     visualContext.ManualTemplateKey,
		"visualStyleSlug":             visualContext.StyleSlug,
		"assetTypeTemplateKey":        visualContext.AssetTypeTemplateKey,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, GenerateAssetCardResponse{
		AssetID:                     asset.ID,
		Profile:                     draft.Profile,
		BasePrompt:                  draft.BasePrompt,
		ConsistencyPrompt:           draft.ConsistencyPrompt,
		NegativePrompt:              draft.NegativePrompt,
		ProviderCallID:              gatewayResp.ProviderCallID,
		ModelID:                     gatewayResp.ModelID,
		VisualManualPromptVersionID: visualContext.ManualPromptVersionID,
		VisualManualTemplateKey:     visualContext.ManualTemplateKey,
		VisualStyleSlug:             visualContext.StyleSlug,
		AssetTypeTemplateKey:        visualContext.AssetTypeTemplateKey,
		Applied:                     applied,
	}, nil)
}

func (s *Server) listAssetReferences(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	asset, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	includePreview := strings.EqualFold(r.URL.Query().Get("includePreviewUrl"), "true")
	if includePreview && s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
		return
	}
	items, err := s.assetReferences(r, project.ID, asset.ID, includePreview)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createAssetReferenceUploadURL(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	if _, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if s.storage == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "object storage is not configured", nil, true)
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
	mimeType := strings.TrimSpace(req.MimeType)
	if fileName == "" || !validAssetReferenceMimeType(mimeType) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "fileName and previewable image mimeType are required", nil, false)
		return
	}
	expires := time.Duration(req.ExpiresSeconds) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	if expires > time.Hour {
		expires = time.Hour
	}
	storageKey := fmt.Sprintf("uploads/%s/%s/asset-references/%s/%s/%s", project.OrganizationID, project.ID, r.PathValue("assetId"), randomStorageSegment(), fileName)
	put, err := s.storage.PresignPutObject(r.Context(), storageKey, mimeType, expires)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, map[string]any{
		"storageKey": put.StorageKey,
		"uploadUrl":  put.URL,
		"method":     put.Method,
		"headers":    put.Headers,
		"expiresAt":  put.ExpiresAt,
	}, nil)
}

func (s *Server) createAssetReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64           `json:"expectedRevision"`
		Title            string          `json:"title"`
		Description      string          `json:"description"`
		StorageKey       string          `json:"storageKey"`
		MimeType         string          `json:"mimeType"`
		ReferenceType    string          `json:"referenceType"`
		SetPrimary       bool            `json:"setPrimary"`
		Metadata         json.RawMessage `json:"metadata"`
		IdempotencyKey   string          `json:"idempotencyKey"`
	}
	if !decode(w, r, &req) {
		return
	}
	actionInput := mustRawJSON(assetReferenceCreateActionInput{
		AssetID: r.PathValue("assetId"), ExpectedRevision: req.ExpectedRevision,
		Title: req.Title, Description: req.Description, StorageKey: req.StorageKey,
		MimeType: req.MimeType, ReferenceType: req.ReferenceType,
		SetPrimary: req.SetPrimary, Metadata: req.Metadata,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "asset.reference.create", actionInput,
		idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	encoded, err := json.Marshal(result.Data["reference"])
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var reference AssetReference
	if err := json.Unmarshal(encoded, &reference); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	w.Header().Set("X-CineWeave-Asset-Revision", fmt.Sprint(result.Data["revision"]))
	httpx.WriteJSON(w, r, http.StatusCreated, reference, nil)
}

func (s *Server) setPrimaryAssetReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
	}
	if !decode(w, r, &req) {
		return
	}
	actionInput := mustRawJSON(assetReferenceSetPrimaryActionInput{
		AssetID: r.PathValue("assetId"), ReferenceID: r.PathValue("referenceId"),
		ExpectedRevision: req.ExpectedRevision,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "asset.reference.set_primary", actionInput,
		idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, result.Data, nil)
}

func (s *Server) deleteAssetReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
		Reason           string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	actionInput := mustRawJSON(assetReferenceDeleteActionInput{
		AssetID: r.PathValue("assetId"), ReferenceID: r.PathValue("referenceId"),
		ExpectedRevision: req.ExpectedRevision, Reason: req.Reason,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "asset.reference.delete", actionInput,
		idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, result.Data, nil)
}

func (s *Server) listShotAssetRequirements(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	page, err := s.listShotAssetRequirementsAction(r.Context(), project, shotAssetRequirementListActionInput{
		StoryboardShotID: strings.TrimSpace(r.URL.Query().Get("filter[storyboardShotId]")),
		ScriptEpisodeID:  strings.TrimSpace(r.URL.Query().Get("filter[scriptEpisodeId]")),
		ReviewStatus:     strings.TrimSpace(r.URL.Query().Get("filter[reviewStatus]")),
		Limit:            queryInt(r, "limit", maxShotAssetReviewBatchItems),
		Cursor:           strings.TrimSpace(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items":             page.Items,
		"reviewItems":       page.ReviewItems,
		"validationVersion": page.ValidationVersion,
		"eligibleCount":     page.EligibleCount,
		"blockedCount":      page.BlockedCount,
		"hasMore":           page.NextCursor != "",
		"nextCursor":        page.NextCursor,
	}, nil)
}

func (s *Server) analyzeScriptAssets(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetAnalyze)
	if !ok {
		return
	}
	script, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		MergeExisting  *bool `json:"mergeExisting"`
		GenerateImages bool  `json:"generateImages"`
	}
	if !decode(w, r, &req) {
		return
	}
	mergeExisting := true
	if req.MergeExisting != nil {
		mergeExisting = *req.MergeExisting
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, "script_to_assets", map[string]any{
		"scriptId":       script.ID,
		"mergeExisting":  mergeExisting,
		"generateImages": req.GenerateImages,
	}, workflows.ScriptToAssetsWorkflow)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) generateScriptStoryboard(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionStoryboardGenerate)
	if !ok {
		return
	}
	script, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		ScriptEpisodeIDs      []string `json:"scriptEpisodeIds"`
		PacingProfile         string   `json:"pacingProfile"`
		TargetDurationSeconds *float64 `json:"targetDurationSeconds,omitempty"`
		AudioStrategy         string   `json:"audioStrategy"`
		AudioRequirement      string   `json:"audioRequirement"`
		PlannerBatchMaxShots  int      `json:"plannerBatchMaxShots"`
		MaxSceneConcurrency   int      `json:"maxSceneConcurrency"`
		ShotBudget            int      `json:"shotBudget"`
		Force                 bool     `json:"force"`
		GenerateDerivedAssets bool     `json:"generateDerivedAssets"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.PacingProfile = strings.ToLower(strings.TrimSpace(req.PacingProfile))
	if req.PacingProfile == "" {
		req.PacingProfile = "standard"
	}
	if req.PacingProfile != "standard" && req.PacingProfile != "fast" && req.PacingProfile != "slow" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "pacingProfile must be standard, fast, or slow", nil, false)
		return
	}
	if req.TargetDurationSeconds != nil && *req.TargetDurationSeconds <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "targetDurationSeconds must be positive", nil, false)
		return
	}
	if req.PlannerBatchMaxShots == 0 {
		req.PlannerBatchMaxShots = 12
	}
	if req.PlannerBatchMaxShots < 8 || req.PlannerBatchMaxShots > 16 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "plannerBatchMaxShots must be between 8 and 16", nil, false)
		return
	}
	if req.MaxSceneConcurrency == 0 {
		req.MaxSceneConcurrency = 3
	}
	if req.MaxSceneConcurrency < 1 || req.MaxSceneConcurrency > 8 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "maxSceneConcurrency must be between 1 and 8", nil, false)
		return
	}
	if req.ShotBudget < 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotBudget cannot be negative", nil, false)
		return
	}
	req.AudioStrategy = strings.ToLower(strings.TrimSpace(req.AudioStrategy))
	if req.AudioStrategy == "" {
		req.AudioStrategy = project.AudioStrategy
	}
	req.AudioRequirement = strings.ToLower(strings.TrimSpace(req.AudioRequirement))
	if req.AudioRequirement == "" {
		req.AudioRequirement = project.AudioRequirement
	}
	if !validProjectAudioSettings(req.AudioStrategy, req.AudioRequirement) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "audioStrategy or audioRequirement is not supported", nil, false)
		return
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, "script_to_storyboard", map[string]any{
		"scriptId":              script.ID,
		"scriptEpisodeIds":      req.ScriptEpisodeIDs,
		"pacingProfile":         req.PacingProfile,
		"targetDurationSeconds": req.TargetDurationSeconds,
		"audioStrategy":         req.AudioStrategy,
		"audioRequirement":      req.AudioRequirement,
		"plannerBatchMaxShots":  req.PlannerBatchMaxShots,
		"maxSceneConcurrency":   req.MaxSceneConcurrency,
		"shotBudget":            req.ShotBudget,
		"force":                 req.Force,
		"generateDerivedAssets": req.GenerateDerivedAssets,
	}, workflows.ScriptToStoryboardWorkflow)
	if !ok {
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, run, nil)
}

func (s *Server) generateCanonicalAssetImage(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetGenerate)
	if !ok {
		return
	}
	var req struct {
		SetPrimary bool `json:"setPrimary"`
	}
	if !decode(w, r, &req) {
		return
	}
	asset, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if asset.Status == "archived" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "canonical asset is archived", nil, false)
		return
	}
	if !canonicalAssetPromptReady(asset) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ASSET_PROMPT_NOT_READY", "canonical asset prompt card is not ready", nil, false)
		return
	}
	rendered, err := s.renderAPIProjectPrompt(r, project, "canonical_asset_image_prompt", map[string]any{
		"project": projectImagePromptVariables(project),
		"asset":   canonicalAssetImagePromptVariables(asset),
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered, err = s.withToonflowVisualPrompt(r, project, rendered, asset.AssetType, false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered = withCanonicalAssetImageRequirements(rendered, asset.AssetType)
	rendered = withRuntimeImagePromptLimit(rendered)
	if _, err := s.db.Exec(r.Context(), `
		UPDATE canonical_assets
		SET status = 'image_running',
		    metadata = (COALESCE(metadata, '{}'::jsonb) - 'imageFailedAt' - 'imageFailedReason')
		               || jsonb_build_object('imageStartedAt', now()),
		    updated_at = now()
		WHERE id = $1
	`, asset.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	gatewayResp, err := provider.NewGatewayClientFromEnv().GenerateImage(r.Context(), provider.GatewayImageRequest{
		GatewayBillingIdentity: gatewayBillingIdentityFromContext(
			r.Context(),
			authz.PermissionAssetGenerate,
			provider.BillingContextReasonManualProvider,
		),
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ImageModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustMarshal(assetprompts.CanonicalImageInput(rendered.RenderedText, asset.AssetType, project.ImageQuality)),
		References:        lockedCanonicalAssetImageReferences(asset),
		Options: provider.GatewayImageOptions{
			IdempotencyKey: gatewayProviderIdempotencyKey(
				r.Context(),
				provider.TaskTypeImageGenerate,
				project.ID,
				asset.ID,
				rendered.RenderedHash,
			),
		},
	})
	if err != nil {
		if markErr := s.markCanonicalAssetImageFailed(asset.ID, err); markErr != nil {
			s.writeError(w, r, markErr)
			return
		}
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	shouldPrimary := req.SetPrimary || !canonicalAssetHasPrimaryReference(asset)
	item, err := scanCanonicalAsset(tx.QueryRow(r.Context(), `
		UPDATE canonical_assets
		SET reference_artifact_id = NULLIF($3, '')::uuid,
		    reference_media_file_id = NULLIF($4, '')::uuid,
		    reference_storage_key = NULLIF($5, ''),
		    primary_reference_artifact_id = CASE WHEN $6 THEN NULLIF($3, '')::uuid ELSE primary_reference_artifact_id END,
		    primary_reference_media_file_id = CASE WHEN $6 THEN NULLIF($4, '')::uuid ELSE primary_reference_media_file_id END,
		    primary_reference_storage_key = CASE WHEN $6 THEN NULLIF($5, '') ELSE primary_reference_storage_key END,
		    status = 'image_succeeded',
		    stale_state = 'fresh'
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		          primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		          reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		          manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		          revision, prompt_revision
	`, asset.ID, project.ID, gatewayResp.Output.ArtifactID, gatewayResp.Output.MediaFileID, gatewayResp.Output.StorageKey, shouldPrimary))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var referenceID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title, description,
			artifact_id, media_file_id, storage_key, prompt, prompt_version_id, prompt_hash,
			is_primary, metadata, created_by
		)
		VALUES ($1, $2, $3, 'generated', $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, NULLIF($8, ''),
		        $9, NULLIF($10, '')::uuid, NULLIF($11, ''), false, $12, $13)
		RETURNING id::text
	`, project.OrganizationID, project.ID, asset.ID, "Generated reference", asset.Description, gatewayResp.Output.ArtifactID, gatewayResp.Output.MediaFileID, gatewayResp.Output.StorageKey,
		rendered.RenderedText, rendered.PromptVersionID, rendered.RenderedHash, mustRawJSON(map[string]any{
			"source":         "canonical_asset_image_prompt",
			"providerCallId": gatewayResp.ProviderCallID,
			"modelId":        gatewayResp.ModelID,
		}), principal.UserID).Scan(&referenceID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if shouldPrimary {
		if _, err := s.setPrimaryAssetReferenceTx(r.Context(), tx, project.ID, asset.ID, referenceID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "asset.reference.created", "asset_reference", referenceID, mustRawJSON(map[string]any{
		"assetId":     asset.ID,
		"referenceId": referenceID,
		"isPrimary":   shouldPrimary,
		"source":      "generated",
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"asset": item, "providerCallId": gatewayResp.ProviderCallID}, nil)
}

func (s *Server) generateDerivedAssetImage(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetGenerate)
	if !ok {
		return
	}
	result, err := s.createDerivedAssetImageAction(
		r.Context(), principal, project, r.PathValue("requirementId"), idempotencyKey(r, ""),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, result, nil)
}

func (s *Server) startProjectWorkflow(w http.ResponseWriter, r *http.Request, principal auth.Principal, project Project, workflowType string, input map[string]any, workflowFunc any) (WorkflowRun, bool) {
	run, err := s.startProjectWorkflowCore(r.Context(), principal, project, workflowType, input, workflowFunc)
	if err != nil {
		s.writeError(w, r, err)
		return WorkflowRun{}, false
	}
	return run, true
}

func (s *Server) startProjectWorkflowCore(ctx context.Context, principal auth.Principal, project Project, workflowType string, input map[string]any, workflowFunc any) (WorkflowRun, error) {
	return s.startProjectWorkflowCoreWithHook(ctx, principal, project, workflowType, input, workflowFunc, nil)
}

func (s *Server) startProjectWorkflowCoreWithHook(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	workflowType string,
	input map[string]any,
	workflowFunc any,
	afterEnqueue func(context.Context, pgx.Tx, WorkflowRun) error,
) (WorkflowRun, error) {
	inputJSON := json.RawMessage(mustMarshal(input))
	runInput := json.RawMessage(mustMarshal(map[string]any{
		"prompt":       "",
		"workflowType": workflowType,
		"input":        input,
	}))
	return s.enqueueProjectWorkflowWithHook(
		ctx,
		principal,
		project,
		workflowType,
		runInput,
		workflows.ScriptTaskQueue,
		workflowFunc,
		func(run WorkflowRun) any {
			return workflows.TextToStoryboardInput{
				OrganizationID: project.OrganizationID,
				ProjectID:      project.ID,
				WorkflowRunID:  run.ID,
				Prompt:         workflowType,
				CreatedBy:      principal.UserID,
				Input:          inputJSON,
			}
		},
		afterEnqueue,
	)
}

func (s *Server) renderAPIProjectPrompt(r *http.Request, project Project, templateKey string, variables map[string]any) (promptsvc.RenderedPrompt, error) {
	resolved, err := promptsvc.NewService(s.db).Resolve(r.Context(), promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, err
	}
	return promptsvc.Render(resolved, variables)
}

func projectImagePromptVariables(project Project) map[string]any {
	return map[string]any{
		"id":                        project.ID,
		"projectType":               stringValue(project.ProjectType),
		"contentType":               stringValue(project.ContentType),
		"aspectRatio":               stringValue(project.AspectRatio),
		"videoRatio":                project.VideoRatio,
		"artStyle":                  project.ArtStyle,
		"directorManual":            assetprompts.RuntimeManualSummary(project.DirectorManual, assetprompts.RuntimeDirectorManualMaxRunes),
		"visualManual":              assetprompts.RuntimeManualSummary(project.VisualManual, assetprompts.RuntimeVisualManualMaxRunes),
		"imageQuality":              project.ImageQuality,
		"videoProductionProfileKey": project.VideoProductionBinding.ProfileKey,
	}
}

func canonicalAssetImagePromptVariables(asset CanonicalAsset) map[string]any {
	return map[string]any{
		"assetType":         asset.AssetType,
		"type":              asset.AssetType,
		"name":              asset.Name,
		"description":       assetprompts.RuntimePromptField(asset.Description, 900),
		"profile":           assetprompts.RuntimePromptField(string(asset.Profile), assetprompts.RuntimeAssetProfileMaxRunes),
		"basePrompt":        assetprompts.RuntimePromptField(stringValue(asset.BasePrompt), assetprompts.RuntimeAssetBasePromptMaxRunes),
		"consistencyPrompt": assetprompts.RuntimePromptField(stringValue(asset.ConsistencyPrompt), assetprompts.RuntimeAssetConsistencyMaxRunes),
		"negativePrompt":    assetprompts.RuntimePromptField(stringValue(asset.NegativePrompt), assetprompts.RuntimeAssetNegativeMaxRunes),
		"visualTraits":      assetprompts.RuntimePromptField(string(asset.VisualTraits), assetprompts.RuntimeAssetVisualTraitsMaxRunes),
	}
}

func (s *Server) withToonflowVisualPrompt(r *http.Request, project Project, rendered promptsvc.RenderedPrompt, assetType string, derivative bool) (promptsvc.RenderedPrompt, error) {
	style := assetprompts.ToonflowStyleSlug(project.ArtStyle)
	if style == "" {
		return rendered, nil
	}
	suffix := assetprompts.ToonflowVisualTemplateSuffix(assetType, derivative)
	if suffix == "" {
		return rendered, nil
	}
	prefix, ok, err := s.systemPromptContent(r, "toonflow_visual_"+style+"_prefix")
	if err != nil || !ok {
		return rendered, err
	}
	target, ok, err := s.systemPromptContent(r, "toonflow_visual_"+style+"_"+suffix)
	if err != nil || !ok {
		return rendered, err
	}
	prefix = assetprompts.RuntimeManualSummary(prefix, assetprompts.RuntimeToonflowPrefixMaxRunes)
	target = assetprompts.RuntimeManualSummary(target, assetprompts.RuntimeToonflowTemplateMaxRunes)
	toonflowPrompt := strings.TrimSpace(strings.Join(compactStrings([]string{prefix, target}), "\n\n"))
	if toonflowPrompt == "" {
		return rendered, nil
	}
	rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) + "\n\n" + toonflowPrompt
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmpty(rendered.Source, "system_active") + "+toonflow_visual_compact"
	return rendered, nil
}

func withCanonicalAssetImageRequirements(rendered promptsvc.RenderedPrompt, assetType string) promptsvc.RenderedPrompt {
	rendered.RenderedText = assetprompts.CanonicalImagePrompt(rendered.RenderedText, assetType)
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmpty(rendered.Source, "system_active") + "+canonical_asset_layout"
	return rendered
}

func withRuntimeImagePromptLimit(rendered promptsvc.RenderedPrompt) promptsvc.RenderedPrompt {
	rendered.RenderedText = assetprompts.RuntimeImagePrompt(rendered.RenderedText)
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	return rendered
}

func (s *Server) markCanonicalAssetImageFailed(assetID string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.Exec(ctx, `
		UPDATE canonical_assets
		SET status = 'image_failed',
		    metadata = COALESCE(metadata, '{}'::jsonb)
		               || jsonb_build_object('imageFailedAt', now(), 'imageFailedReason', $2::text),
		    updated_at = now()
		WHERE id = $1
	`, assetID, errorString(cause))
	return err
}

func (s *Server) markShotAssetRequirementImageFailed(requirementID string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET status = 'image_failed',
		    metadata = COALESCE(metadata, '{}'::jsonb)
		               || jsonb_build_object('imageFailedAt', now(), 'imageFailedReason', $2::text),
		    updated_at = now()
		WHERE id = $1
	`, requirementID, errorString(cause))
	return err
}

func errorString(cause error) string {
	if cause == nil {
		return ""
	}
	return cause.Error()
}

func (s *Server) systemPromptContent(r *http.Request, templateKey string) (string, bool, error) {
	var content string
	err := s.db.QueryRow(r.Context(), `
		SELECT pv.content
		FROM prompt_templates pt
		JOIN prompt_versions pv ON pv.template_id = pt.id
		WHERE pt.organization_id IS NULL
		  AND pt.template_key = $1
		  AND pt.status = 'active'
		  AND pv.status = 'active'
		ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
		LIMIT 1
	`, templateKey).Scan(&content)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return content, true, nil
}

func (s *Server) canonicalAsset(r *http.Request, projectID, assetID string) (CanonicalAsset, error) {
	return s.canonicalAssetWithDB(r.Context(), s.db, projectID, assetID)
}

func (s *Server) canonicalAssetWithDB(ctx context.Context, db snapshotQuerier, projectID, assetID string) (CanonicalAsset, error) {
	return scanCanonicalAsset(db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		       revision, prompt_revision
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID))
}

func (s *Server) assetScenePromptContext(r *http.Request, projectID, assetID string) (string, error) {
	return s.assetScenePromptContextWithDB(r.Context(), s.db, projectID, assetID)
}

func (s *Server) assetScenePromptContextWithDB(ctx context.Context, db snapshotQuerier, projectID, assetID string) (string, error) {
	rows, err := db.Query(ctx, `
		SELECT sc.scene_no, sc.title, COALESCE(sc.location, ''), COALESCE(l.usage_note, ''), COALESCE(sc.content, '')
		FROM scene_asset_links l
		JOIN script_scenes sc ON sc.id = l.script_scene_id
		WHERE l.project_id = $1 AND l.asset_id = $2
		ORDER BY sc.scene_index ASC
		LIMIT 12
	`, projectID, assetID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	lines := []string{}
	for rows.Next() {
		var sceneNo int
		var title, location, usage, content string
		if err := rows.Scan(&sceneNo, &title, &location, &usage, &content); err != nil {
			return "", err
		}
		lines = append(lines, strings.Join(compactStrings([]string{
			fmt.Sprintf("Scene %d: %s", sceneNo, title),
			"Location: " + location,
			"Usage: " + usage,
			"Content: " + content,
		}), "\n"))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n\n"), nil
}

func (s *Server) assetReferences(r *http.Request, projectID, assetID string, includePreview bool) ([]AssetReference, error) {
	previewExpires := previewURLExpiryFromRequest(r)
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_id, reference_type, title, description,
		       artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		       is_primary, status, metadata, created_by, created_at, updated_at
		FROM asset_references
		WHERE project_id = $1 AND asset_id = $2 AND status = 'ready'
		ORDER BY is_primary DESC, created_at DESC
	`, projectID, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AssetReference, 0)
	for rows.Next() {
		item, err := scanAssetReference(rows)
		if err != nil {
			return nil, err
		}
		if includePreview && s.storage != nil && item.StorageKey != nil && strings.TrimSpace(*item.StorageKey) != "" {
			if presigned, err := s.storage.PresignGetObject(r.Context(), *item.StorageKey, previewExpires); err == nil {
				item.PreviewURL = &presigned.URL
				item.PreviewExpiresAt = &presigned.ExpiresAt
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) attachCanonicalAssetReferences(r *http.Request, projectID string, items []CanonicalAsset, includePreview bool) error {
	if len(items) == 0 {
		return nil
	}
	previewExpires := previewURLExpiryFromRequest(r)
	index := map[string]int{}
	for i := range items {
		index[items[i].ID] = i
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_id, reference_type, title, description,
		       artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		       is_primary, status, metadata, created_by, created_at, updated_at
		FROM asset_references
		WHERE project_id = $1 AND status = 'ready'
		ORDER BY asset_id, is_primary DESC, created_at DESC
	`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		ref, err := scanAssetReference(rows)
		if err != nil {
			return err
		}
		i, ok := index[ref.AssetID]
		if !ok {
			continue
		}
		if includePreview && s.storage != nil && ref.StorageKey != nil && strings.TrimSpace(*ref.StorageKey) != "" {
			if presigned, err := s.storage.PresignGetObject(r.Context(), *ref.StorageKey, previewExpires); err == nil {
				ref.PreviewURL = &presigned.URL
				ref.PreviewExpiresAt = &presigned.ExpiresAt
			}
		}
		items[i].References = append(items[i].References, ref)
		items[i].ReferenceCount++
	}
	return rows.Err()
}

func (s *Server) attachCanonicalAssetShotRequirements(r *http.Request, projectID string, items []CanonicalAsset) error {
	if len(items) == 0 {
		return nil
	}
	index := map[string]int{}
	for i := range items {
		index[items[i].ID] = i
	}
	rows, err := s.db.Query(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1
		  AND r.production_generation_id = (
		    SELECT active_video_production_generation_id FROM projects WHERE id = $1
		  )
		ORDER BY r.created_at DESC
	`), projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		req, err := scanShotAssetRequirement(rows)
		if err != nil {
			return err
		}
		i, ok := index[req.AssetID]
		if !ok {
			continue
		}
		items[i].ShotRequirements = append(items[i].ShotRequirements, req)
		items[i].ShotRequirementCount++
	}
	return rows.Err()
}

func (s *Server) setPrimaryAssetReferenceTx(ctx context.Context, tx pgx.Tx, projectID, assetID, referenceID string) (AssetReference, error) {
	var ref AssetReference
	if _, err := tx.Exec(ctx, `
		UPDATE asset_references
		SET is_primary = false, updated_at = now()
		WHERE project_id = $1 AND asset_id = $2 AND id <> $3 AND status = 'ready'
	`, projectID, assetID, referenceID); err != nil {
		return AssetReference{}, err
	}
	ref, err := scanAssetReference(tx.QueryRow(ctx, `
		UPDATE asset_references
		SET is_primary = true, status = 'ready', updated_at = now()
		WHERE project_id = $1 AND asset_id = $2 AND id = $3 AND status = 'ready'
		RETURNING id, organization_id, project_id, asset_id, reference_type, title, description,
		          artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		          is_primary, status, metadata, created_by, created_at, updated_at
	`, projectID, assetID, referenceID))
	if err != nil {
		return AssetReference{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET primary_reference_artifact_id = NULLIF($3, '')::uuid,
		    primary_reference_media_file_id = NULLIF($4, '')::uuid,
		    primary_reference_storage_key = NULLIF($5, ''),
		    reference_artifact_id = NULLIF($3, '')::uuid,
		    reference_media_file_id = NULLIF($4, '')::uuid,
		    reference_storage_key = NULLIF($5, ''),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID, stringValue(ref.ArtifactID), stringValue(ref.MediaFileID), stringValue(ref.StorageKey)); err != nil {
		return AssetReference{}, err
	}
	return ref, nil
}

func clearCanonicalAssetReferenceTx(ctx context.Context, tx pgx.Tx, projectID, assetID string, reference AssetReference) error {
	artifactID := stringValue(reference.ArtifactID)
	mediaFileID := stringValue(reference.MediaFileID)
	storageKey := stringValue(reference.StorageKey)
	_, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET primary_reference_artifact_id = CASE
		      WHEN COALESCE(primary_reference_artifact_id::text, '') = $3 THEN NULL
		      ELSE primary_reference_artifact_id
		    END,
		    primary_reference_media_file_id = CASE
		      WHEN COALESCE(primary_reference_media_file_id::text, '') = $4 THEN NULL
		      ELSE primary_reference_media_file_id
		    END,
		    primary_reference_storage_key = CASE
		      WHEN COALESCE(primary_reference_storage_key, '') = $5 THEN NULL
		      ELSE primary_reference_storage_key
		    END,
		    reference_artifact_id = CASE
		      WHEN COALESCE(reference_artifact_id::text, '') = $3 THEN NULL
		      ELSE reference_artifact_id
		    END,
		    reference_media_file_id = CASE
		      WHEN COALESCE(reference_media_file_id::text, '') = $4 THEN NULL
		      ELSE reference_media_file_id
		    END,
		    reference_storage_key = CASE
		      WHEN COALESCE(reference_storage_key, '') = $5 THEN NULL
		      ELSE reference_storage_key
		    END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID, artifactID, mediaFileID, storageKey)
	return err
}

func (s *Server) attachCanonicalAssetSceneLinks(r *http.Request, projectID string, items []CanonicalAsset) error {
	if len(items) == 0 {
		return nil
	}
	index := map[string]int{}
	for i := range items {
		index[items[i].ID] = i
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT
			l.asset_id::text,
			l.script_scene_id::text,
			sc.scene_no,
			sc.title,
			COALESCE(sc.location, ''),
			l.asset_role,
			l.usage_note,
			COUNT(DISTINCT ss.id)
		FROM scene_asset_links l
		JOIN script_scenes sc ON sc.id = l.script_scene_id
		LEFT JOIN storyboard_shots ss ON ss.project_id = l.project_id AND ss.script_scene_id = l.script_scene_id AND ss.deleted_at IS NULL
		WHERE l.project_id = $1
		GROUP BY l.asset_id, l.script_scene_id, sc.scene_index, sc.scene_no, sc.title, sc.location, l.asset_role, l.usage_note
		ORDER BY sc.scene_index ASC, sc.scene_no ASC
	`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID string
		var link AssetSceneLink
		var role, usageNote sql.NullString
		if err := rows.Scan(&assetID, &link.ScriptSceneID, &link.SceneNo, &link.Title, &link.Location, &role, &usageNote, &link.StoryboardShotCount); err != nil {
			return err
		}
		i, ok := index[assetID]
		if !ok {
			continue
		}
		link.AssetRole = stringPtrFromNull(role)
		link.UsageNote = stringPtrFromNull(usageNote)
		items[i].SceneLinks = append(items[i].SceneLinks, link)
		items[i].SceneCount = len(items[i].SceneLinks)
		items[i].StoryboardShotCount += link.StoryboardShotCount
	}
	return rows.Err()
}

func (s *Server) shotAssetRequirement(r *http.Request, projectID, requirementID string) (ShotAssetRequirement, error) {
	return scanShotAssetRequirement(s.db.QueryRow(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1 AND r.id = $2
	`), projectID, requirementID))
}

func (s *Server) storyboardShotByID(r *http.Request, projectID, shotID string) (StoryboardShot, error) {
	return s.storyboardShotByIDContext(r.Context(), projectID, shotID)
}

func (s *Server) storyboardShotByIDContext(ctx context.Context, projectID, shotID string) (StoryboardShot, error) {
	return scanStoryboardShot(s.db.QueryRow(ctx, storyboardShotSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2 AND s.deleted_at IS NULL
	`), projectID, shotID))
}

func scanCanonicalAsset(row rowScan) (CanonicalAsset, error) {
	var item CanonicalAsset
	var basePrompt, consistencyPrompt, negativePrompt sql.NullString
	var primaryReferenceArtifactID, primaryReferenceMediaFileID, primaryReferenceStorageKey sql.NullString
	var referenceArtifactID, referenceMediaFileID, referenceStorageKey, editedBy, createdBy sql.NullString
	var editedAt sql.NullTime
	var profile, visualTraits, sourceScriptIDs, metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.AssetType,
		&item.Name,
		&item.Description,
		&profile,
		&basePrompt,
		&consistencyPrompt,
		&negativePrompt,
		&visualTraits,
		&primaryReferenceArtifactID,
		&primaryReferenceMediaFileID,
		&primaryReferenceStorageKey,
		&item.LockReference,
		&referenceArtifactID,
		&referenceMediaFileID,
		&referenceStorageKey,
		&item.Status,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&editedBy,
		&editedAt,
		&sourceScriptIDs,
		&metadata,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.Revision,
		&item.PromptRevision,
	)
	item.Profile = rawOrDefaultBytes(profile, "{}")
	item.BasePrompt = stringPtrFromNull(basePrompt)
	item.ConsistencyPrompt = stringPtrFromNull(consistencyPrompt)
	item.NegativePrompt = stringPtrFromNull(negativePrompt)
	item.VisualTraits = rawOrDefaultBytes(visualTraits, "{}")
	item.PrimaryReferenceArtifactID = stringPtrFromNull(primaryReferenceArtifactID)
	item.PrimaryReferenceMediaFileID = stringPtrFromNull(primaryReferenceMediaFileID)
	item.PrimaryReferenceStorageKey = stringPtrFromNull(primaryReferenceStorageKey)
	item.ReferenceArtifactID = stringPtrFromNull(referenceArtifactID)
	item.ReferenceMediaFileID = stringPtrFromNull(referenceMediaFileID)
	item.ReferenceStorageKey = stringPtrFromNull(referenceStorageKey)
	item.EditedBy = stringPtrFromNull(editedBy)
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	item.SourceScriptIDs = rawOrDefaultBytes(sourceScriptIDs, "[]")
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func scanAssetReference(row rowScan) (AssetReference, error) {
	var item AssetReference
	var title, description, artifactID, mediaFileID, storageKey, previewURL sql.NullString
	var prompt, promptVersionID, promptHash, createdBy sql.NullString
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.AssetID,
		&item.ReferenceType,
		&title,
		&description,
		&artifactID,
		&mediaFileID,
		&storageKey,
		&previewURL,
		&prompt,
		&promptVersionID,
		&promptHash,
		&item.IsPrimary,
		&item.Status,
		&metadata,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.Title = stringPtrFromNull(title)
	item.Description = stringPtrFromNull(description)
	item.ArtifactID = stringPtrFromNull(artifactID)
	item.MediaFileID = stringPtrFromNull(mediaFileID)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.PreviewURL = stringPtrFromNull(previewURL)
	item.Prompt = stringPtrFromNull(prompt)
	item.PromptVersionID = stringPtrFromNull(promptVersionID)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func shotAssetRequirementSelectSQL(where string) string {
	return `
		SELECT
			r.id, r.organization_id, r.project_id, r.workflow_run_id, r.storyboard_shot_id,
			r.asset_id, COALESCE(a.asset_type, ''), COALESCE(a.name, ''),
			r.requirement_type, r.role_in_shot, r.costume, r.pose,
			r.expression, r.action, r.camera_relation, r.scene_state, r.prop_state,
			r.prompt, r.derived_artifact_id, r.derived_media_file_id, r.derived_storage_key,
			r.status, r.review_status, r.manual_override, r.stale_state, r.edited_by, r.edited_at,
			r.metadata, r.created_at, r.updated_at
		FROM shot_asset_requirements r
		LEFT JOIN canonical_assets a ON a.id = r.asset_id
	` + where
}

func scanShotAssetRequirement(row rowScan) (ShotAssetRequirement, error) {
	var item ShotAssetRequirement
	var workflowRunID, roleInShot, costume, pose, expression, action, cameraRelation, sceneState, propState, prompt sql.NullString
	var derivedArtifactID, derivedMediaFileID, derivedStorageKey, editedBy sql.NullString
	var editedAt sql.NullTime
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&workflowRunID,
		&item.StoryboardShotID,
		&item.AssetID,
		&item.AssetType,
		&item.AssetName,
		&item.RequirementType,
		&roleInShot,
		&costume,
		&pose,
		&expression,
		&action,
		&cameraRelation,
		&sceneState,
		&propState,
		&prompt,
		&derivedArtifactID,
		&derivedMediaFileID,
		&derivedStorageKey,
		&item.Status,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&editedBy,
		&editedAt,
		&metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.WorkflowRunID = stringPtrFromNull(workflowRunID)
	item.RoleInShot = stringPtrFromNull(roleInShot)
	item.Costume = stringPtrFromNull(costume)
	item.Pose = stringPtrFromNull(pose)
	item.Expression = stringPtrFromNull(expression)
	item.Action = stringPtrFromNull(action)
	item.CameraRelation = stringPtrFromNull(cameraRelation)
	item.SceneState = stringPtrFromNull(sceneState)
	item.PropState = stringPtrFromNull(propState)
	item.Prompt = stringPtrFromNull(prompt)
	item.DerivedArtifactID = stringPtrFromNull(derivedArtifactID)
	item.DerivedMediaFileID = stringPtrFromNull(derivedMediaFileID)
	item.DerivedStorageKey = stringPtrFromNull(derivedStorageKey)
	item.EditedBy = stringPtrFromNull(editedBy)
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func validCanonicalAssetType(value string) bool {
	return value == "character" || value == "scene" || value == "prop"
}

func validCanonicalAssetStatus(value string) bool {
	return value == "draft" || value == "prompt_ready" || value == "image_running" || value == "image_succeeded" || value == "image_failed" || value == "archived"
}

func canonicalAssetPromptReady(asset CanonicalAsset) bool {
	return canonicalAssetPromptFieldsReady(stringValue(asset.BasePrompt), stringValue(asset.ConsistencyPrompt))
}

func canonicalAssetPromptFieldsReady(basePrompt, consistencyPrompt string) bool {
	return strings.TrimSpace(basePrompt) != "" && strings.TrimSpace(consistencyPrompt) != ""
}

func validAssetReferenceType(value string) bool {
	return value == "generated" || value == "uploaded" || value == "derived" || value == "selected"
}

func validAssetReferenceMimeType(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "image/") && canPreviewMimeType(value)
}

func canonicalAssetHasPrimaryReference(asset CanonicalAsset) bool {
	return stringValue(asset.PrimaryReferenceArtifactID) != "" ||
		stringValue(asset.PrimaryReferenceMediaFileID) != "" ||
		stringValue(asset.PrimaryReferenceStorageKey) != "" ||
		stringValue(asset.ReferenceArtifactID) != "" ||
		stringValue(asset.ReferenceMediaFileID) != "" ||
		stringValue(asset.ReferenceStorageKey) != ""
}

func lockedCanonicalAssetImageReferences(asset CanonicalAsset) []provider.GatewayImageReference {
	if !asset.LockReference {
		return nil
	}
	artifactID := firstNonEmpty(stringValue(asset.PrimaryReferenceArtifactID), stringValue(asset.ReferenceArtifactID))
	storageKey := firstNonEmpty(stringValue(asset.PrimaryReferenceStorageKey), stringValue(asset.ReferenceStorageKey))
	if artifactID == "" && storageKey == "" {
		return nil
	}
	return []provider.GatewayImageReference{{
		Type:       "image",
		AssetID:    asset.ID,
		ArtifactID: artifactID,
		StorageKey: storageKey,
		Metadata: mustRawJSON(map[string]any{
			"source":    "lock_reference",
			"isPrimary": stringValue(asset.PrimaryReferenceArtifactID) != "" || stringValue(asset.PrimaryReferenceStorageKey) != "",
		}),
	}}
}

func shotSummary(shot StoryboardShot) string {
	return strings.Join(compactStrings([]string{
		fmt.Sprintf("Shot %d", shot.ShotNo),
		shot.Visual,
		shot.Camera,
		shot.Motion,
		shot.Mood,
	}), "\n")
}

func requirementSummary(req ShotAssetRequirement) string {
	return strings.Join(compactStrings([]string{
		"Type: " + req.RequirementType,
		"Role: " + stringValue(req.RoleInShot),
		"Costume: " + stringValue(req.Costume),
		"Pose: " + stringValue(req.Pose),
		"Expression: " + stringValue(req.Expression),
		"Action: " + stringValue(req.Action),
		"Camera: " + stringValue(req.CameraRelation),
		"Scene state: " + stringValue(req.SceneState),
		"Prop state: " + stringValue(req.PropState),
		"Prompt: " + stringValue(req.Prompt),
	}), "\n")
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !strings.HasSuffix(value, ":") {
			out = append(out, value)
		}
	}
	return out
}
