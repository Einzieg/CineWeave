package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/client"
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
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       string          `json:"projectId"`
	AssetID         string          `json:"assetId"`
	ReferenceType   string          `json:"referenceType"`
	Title           *string         `json:"title,omitempty"`
	Description     *string         `json:"description,omitempty"`
	ArtifactID      *string         `json:"artifactId,omitempty"`
	MediaFileID     *string         `json:"mediaFileId,omitempty"`
	StorageKey      *string         `json:"storageKey,omitempty"`
	PreviewURL      *string         `json:"previewUrl,omitempty"`
	Prompt          *string         `json:"prompt,omitempty"`
	PromptVersionID *string         `json:"promptVersionId,omitempty"`
	PromptHash      *string         `json:"promptHash,omitempty"`
	IsPrimary       bool            `json:"isPrimary"`
	Status          string          `json:"status"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedBy       *string         `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type GenerateAssetCardResponse struct {
	AssetID           string          `json:"assetId"`
	Profile           json.RawMessage `json:"profile"`
	BasePrompt        string          `json:"basePrompt"`
	ConsistencyPrompt string          `json:"consistencyPrompt"`
	NegativePrompt    string          `json:"negativePrompt"`
	ProviderCallID    string          `json:"providerCallId,omitempty"`
	ModelID           string          `json:"modelId,omitempty"`
	Applied           bool            `json:"applied"`
}

type assetCardDraft struct {
	Profile           json.RawMessage `json:"profile"`
	BasePrompt        string          `json:"basePrompt"`
	ConsistencyPrompt string          `json:"consistencyPrompt"`
	NegativePrompt    string          `json:"negativePrompt"`
}

type ShotAssetRequirement struct {
	ID                 string          `json:"id"`
	OrganizationID     string          `json:"organizationId"`
	ProjectID          string          `json:"projectId"`
	WorkflowRunID      *string         `json:"workflowRunId,omitempty"`
	StoryboardShotID   string          `json:"storyboardShotId"`
	AssetID            string          `json:"assetId"`
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
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at
		FROM canonical_assets
		WHERE project_id = $1
		  AND ($2 = '' OR asset_type = $2)
		ORDER BY asset_type, name
	`, project.ID, assetType)
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
	if err := s.attachCanonicalAssetReferences(r, project.ID, items, false); err != nil {
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
	current, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
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
	}
	if !decode(w, r, &req) {
		return
	}
	assetType := current.AssetType
	if req.AssetType != nil {
		assetType = strings.TrimSpace(*req.AssetType)
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	description := current.Description
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
	}
	status := current.Status
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	if !validCanonicalAssetType(assetType) || name == "" || description == "" || !validCanonicalAssetStatus(status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "canonical asset fields are invalid", nil, false)
		return
	}
	visualTraits := current.VisualTraits
	if len(req.VisualTraits) > 0 {
		var ok bool
		visualTraits, ok = jsonObjectOrDefault(w, r, req.VisualTraits)
		if !ok {
			return
		}
	}
	profile := current.Profile
	if len(req.Profile) > 0 {
		var ok bool
		profile, ok = jsonObjectOrDefault(w, r, req.Profile)
		if !ok {
			return
		}
	}
	metadata := current.Metadata
	if len(req.Metadata) > 0 {
		var ok bool
		metadata, ok = jsonObjectOrDefault(w, r, req.Metadata)
		if !ok {
			return
		}
	}
	basePromptSet := req.BasePrompt != nil
	basePrompt := ""
	if req.BasePrompt != nil {
		basePrompt = strings.TrimSpace(*req.BasePrompt)
	}
	consistencyPromptSet := req.ConsistencyPrompt != nil
	consistencyPrompt := ""
	if req.ConsistencyPrompt != nil {
		consistencyPrompt = strings.TrimSpace(*req.ConsistencyPrompt)
	}
	negativePromptSet := req.NegativePrompt != nil
	negativePrompt := ""
	if req.NegativePrompt != nil {
		negativePrompt = strings.TrimSpace(*req.NegativePrompt)
	}
	lockReference := current.LockReference
	if req.LockReference != nil {
		lockReference = *req.LockReference
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanCanonicalAsset(tx.QueryRow(r.Context(), `
		UPDATE canonical_assets
		SET asset_type = $3,
		    name = $4,
		    description = $5,
		    profile = $6,
		    base_prompt = CASE WHEN $7 THEN NULLIF($8, '') ELSE base_prompt END,
		    consistency_prompt = CASE WHEN $9 THEN NULLIF($10, '') ELSE consistency_prompt END,
		    negative_prompt = CASE WHEN $11 THEN NULLIF($12, '') ELSE negative_prompt END,
		    lock_reference = $13,
		    visual_traits = $14,
		    metadata = $15,
		    status = $16,
		    review_status = 'pending',
		    manual_override = true,
		    stale_state = 'fresh',
		    edited_by = $17,
		    edited_at = now(),
		    updated_at = now()
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		          primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		          reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		          manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at
	`, current.ID, project.ID, assetType, name, description, profile, basePromptSet, basePrompt, consistencyPromptSet, consistencyPrompt, negativePromptSet, negativePrompt, lockReference, visualTraits, metadata, status, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkAssetDownstreamStale(r.Context(), tx, project.ID, current.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := production.MarkFinalVideoStale(r.Context(), tx, project.ID, ""); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "asset.updated", "canonical_asset", item.ID, mustRawJSON(map[string]any{
		"assetId":        item.ID,
		"manualOverride": item.ManualOverride,
		"staleState":     item.StaleState,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "asset.card.updated", "canonical_asset", item.ID, mustRawJSON(map[string]any{
		"assetId":        item.ID,
		"manualOverride": item.ManualOverride,
		"lockReference":  item.LockReference,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
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
	var req struct {
		Force bool `json:"force"`
	}
	if !decode(w, r, &req) {
		return
	}
	scenes, err := s.assetScenePromptContext(r, project.ID, asset.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered, gatewayResp, err := s.runTextGatewayPrompt(r, project, "asset_card_generation", map[string]any{
		"project": projectPromptVariables(project),
		"asset": map[string]any{
			"id":                asset.ID,
			"assetType":         asset.AssetType,
			"name":              asset.Name,
			"description":       asset.Description,
			"profile":           string(asset.Profile),
			"basePrompt":        stringValue(asset.BasePrompt),
			"consistencyPrompt": stringValue(asset.ConsistencyPrompt),
			"negativePrompt":    stringValue(asset.NegativePrompt),
		},
		"scenes": scenes,
	}, true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	draft, err := normalizeAssetCardDraft(gatewayResp.Output.Text)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error(), nil, false)
		return
	}
	applied := !asset.ManualOverride || req.Force
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	metadata := mustRawJSON(map[string]any{
		"providerCallId":    gatewayResp.ProviderCallID,
		"modelId":           gatewayResp.ModelID,
		"promptTemplateKey": rendered.TemplateKey,
		"promptVersionId":   rendered.PromptVersionID,
		"promptHash":        rendered.RenderedHash,
		"agentSuggestion":   draft,
	})
	if applied {
		if _, err := tx.Exec(r.Context(), `
			UPDATE canonical_assets
			SET profile = $3,
			    base_prompt = NULLIF($4, ''),
			    consistency_prompt = NULLIF($5, ''),
			    negative_prompt = NULLIF($6, ''),
			    manual_override = false,
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
		"assetId":        asset.ID,
		"applied":        applied,
		"manualOverride": asset.ManualOverride,
		"force":          req.Force,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, GenerateAssetCardResponse{
		AssetID:           asset.ID,
		Profile:           draft.Profile,
		BasePrompt:        draft.BasePrompt,
		ConsistencyPrompt: draft.ConsistencyPrompt,
		NegativePrompt:    draft.NegativePrompt,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ModelID:           gatewayResp.ModelID,
		Applied:           applied,
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
	asset, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		Title         string          `json:"title"`
		Description   string          `json:"description"`
		StorageKey    string          `json:"storageKey"`
		MimeType      string          `json:"mimeType"`
		ReferenceType string          `json:"referenceType"`
		SetPrimary    bool            `json:"setPrimary"`
		Metadata      json.RawMessage `json:"metadata"`
	}
	if !decode(w, r, &req) {
		return
	}
	storageKey := strings.TrimSpace(req.StorageKey)
	mimeType := strings.TrimSpace(req.MimeType)
	referenceType := strings.TrimSpace(req.ReferenceType)
	if referenceType == "" {
		referenceType = "uploaded"
	}
	if storageKey == "" || !validAssetReferenceType(referenceType) || !validAssetReferenceMimeType(mimeType) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "storageKey, image mimeType, and referenceType are required", nil, false)
		return
	}
	metadata := json.RawMessage(`{}`)
	if len(req.Metadata) > 0 {
		var valid bool
		metadata, valid = jsonObjectOrDefault(w, r, req.Metadata)
		if !valid {
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var artifactID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, 'asset_reference_image', $3, $4, $5, $6)
		RETURNING id::text
	`, project.OrganizationID, project.ID, storageKey, mimeType, metadata, principal.UserID).Scan(&artifactID); err != nil {
		s.writeError(w, r, err)
		return
	}
	var mediaFileID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text
	`, project.OrganizationID, project.ID, artifactID, storageKey, mimeType, metadata, principal.UserID).Scan(&mediaFileID); err != nil {
		s.writeError(w, r, err)
		return
	}
	isPrimary := req.SetPrimary || !canonicalAssetHasPrimaryReference(asset)
	reference, err := scanAssetReference(tx.QueryRow(r.Context(), `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title, description,
			artifact_id, media_file_id, storage_key, is_primary, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, NULLIF($9, ''), false, $10, $11)
		RETURNING id, organization_id, project_id, asset_id, reference_type, title, description,
		          artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		          is_primary, status, metadata, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, asset.ID, referenceType, strings.TrimSpace(req.Title), strings.TrimSpace(req.Description), artifactID, mediaFileID, storageKey, metadata, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if isPrimary {
		reference, err = s.setPrimaryAssetReferenceTx(r.Context(), tx, project.ID, asset.ID, reference.ID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "asset.reference.created", "asset_reference", reference.ID, mustRawJSON(map[string]any{
		"assetId":     asset.ID,
		"referenceId": reference.ID,
		"isPrimary":   reference.IsPrimary,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, reference, nil)
}

func (s *Server) setPrimaryAssetReference(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetWrite)
	if !ok {
		return
	}
	asset, err := s.canonicalAsset(r, project.ID, r.PathValue("assetId"))
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
	reference, err := s.setPrimaryAssetReferenceTx(r.Context(), tx, project.ID, asset.ID, r.PathValue("referenceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "asset.reference.primary_set", "asset_reference", reference.ID, mustRawJSON(map[string]any{
		"assetId":     asset.ID,
		"referenceId": reference.ID,
	})); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"assetId": asset.ID, "reference": reference}, nil)
}

func (s *Server) listShotAssetRequirements(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	shotID := strings.TrimSpace(r.URL.Query().Get("filter[storyboardShotId]"))
	rows, err := s.db.Query(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1
		  AND ($2 = '' OR r.storyboard_shot_id = $2::uuid)
		ORDER BY r.created_at ASC
	`), project.ID, shotID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ShotAssetRequirement, 0)
	for rows.Next() {
		item, err := scanShotAssetRequirement(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
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
		MaxShots              int  `json:"maxShots"`
		GenerateDerivedAssets bool `json:"generateDerivedAssets"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.MaxShots <= 0 {
		req.MaxShots = 3
	}
	run, ok := s.startProjectWorkflow(w, r, principal, project, "script_to_storyboard", map[string]any{
		"scriptId":              script.ID,
		"maxShots":              req.MaxShots,
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
	rendered, err := s.renderAPIProjectPrompt(r, project, "canonical_asset_image_prompt", map[string]any{
		"project": projectPromptVariables(project),
		"asset": map[string]any{
			"assetType":         asset.AssetType,
			"type":              asset.AssetType,
			"name":              asset.Name,
			"description":       asset.Description,
			"profile":           string(asset.Profile),
			"basePrompt":        stringValue(asset.BasePrompt),
			"consistencyPrompt": stringValue(asset.ConsistencyPrompt),
			"negativePrompt":    stringValue(asset.NegativePrompt),
			"visualTraits":      string(asset.VisualTraits),
		},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `UPDATE canonical_assets SET status = 'image_running' WHERE id = $1`, asset.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	gatewayResp, err := provider.NewGatewayClientFromEnv().GenerateImage(r.Context(), provider.GatewayImageRequest{
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ImageModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustMarshal(map[string]any{
			"prompt":  rendered.RenderedText,
			"size":    "1024x1024",
			"n":       1,
			"quality": project.ImageQuality,
		}),
	})
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `UPDATE canonical_assets SET status = 'image_failed' WHERE id = $1`, asset.ID)
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
		          manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at
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
	requirement, err := s.shotAssetRequirement(r, project.ID, r.PathValue("requirementId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	asset, err := s.canonicalAsset(r, project.ID, requirement.AssetID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	shot, err := s.storyboardShotByID(r, project.ID, requirement.StoryboardShotID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rendered, err := s.renderAPIProjectPrompt(r, project, "derived_asset_image_prompt", map[string]any{
		"project": projectPromptVariables(project),
		"baseAsset": map[string]any{
			"name":        asset.Name,
			"description": asset.Description,
		},
		"shot":        map[string]any{"summary": shotSummary(shot)},
		"requirement": map[string]any{"summary": requirementSummary(requirement)},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `UPDATE shot_asset_requirements SET status = 'image_running' WHERE id = $1`, requirement.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	refs := make([]provider.GatewayImageReference, 0, 1)
	if asset.ReferenceArtifactID != nil || asset.ReferenceStorageKey != nil {
		refs = append(refs, provider.GatewayImageReference{
			Type:       "image",
			AssetID:    asset.ID,
			ArtifactID: stringValue(asset.ReferenceArtifactID),
			StorageKey: stringValue(asset.ReferenceStorageKey),
		})
	}
	gatewayResp, err := provider.NewGatewayClientFromEnv().GenerateImage(r.Context(), provider.GatewayImageRequest{
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ImageModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustMarshal(map[string]any{
			"prompt":  rendered.RenderedText,
			"size":    "1024x1024",
			"n":       1,
			"quality": project.ImageQuality,
		}),
		References: refs,
	})
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `UPDATE shot_asset_requirements SET status = 'image_failed' WHERE id = $1`, requirement.ID)
		s.writeError(w, r, err)
		return
	}
	item, err := scanShotAssetRequirement(s.db.QueryRow(r.Context(), shotAssetRequirementSelectSQL(`
		WHERE r.project_id = $1 AND r.id = $2
	`), project.ID, requirement.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE shot_asset_requirements
		SET derived_artifact_id = NULLIF($2, '')::uuid,
		    derived_media_file_id = NULLIF($3, '')::uuid,
		    derived_storage_key = NULLIF($4, ''),
		    status = 'image_succeeded',
		    stale_state = 'fresh'
		WHERE id = $1
	`, item.ID, gatewayResp.Output.ArtifactID, gatewayResp.Output.MediaFileID, gatewayResp.Output.StorageKey); err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := s.shotAssetRequirement(r, project.ID, requirement.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"requirement": updated, "providerCallId": gatewayResp.ProviderCallID}, nil)
}

func (s *Server) startProjectWorkflow(w http.ResponseWriter, r *http.Request, principal auth.Principal, project Project, workflowType string, input map[string]any, workflowFunc any) (WorkflowRun, bool) {
	if s.temporal == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "TEMPORAL_UNAVAILABLE", "Temporal client is not configured", nil, true)
		return WorkflowRun{}, false
	}
	inputJSON := json.RawMessage(mustMarshal(input))
	runInput := json.RawMessage(mustMarshal(map[string]any{
		"prompt":       "",
		"workflowType": workflowType,
		"input":        input,
	}))
	var run WorkflowRun
	err := s.db.QueryRow(r.Context(), `
		WITH new_run AS (SELECT gen_random_uuid() AS id)
		INSERT INTO workflow_runs(id, organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		SELECT id, $1, $2, 'workflow-' || id::text, 'queued', $3, '{}', $4
		FROM new_run
		RETURNING id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
	`, project.OrganizationID, project.ID, runInput, principal.UserID).Scan(
		&run.ID,
		&run.OrganizationID,
		&run.ProjectID,
		&run.TemplateID,
		&run.TemporalWorkflowID,
		&run.Status,
		&run.Input,
		&run.Output,
		&run.ErrorCode,
		&run.ErrorMessage,
		&run.CreatedBy,
		&run.CreatedAt,
		&run.StartedAt,
		&run.CompletedAt,
		&run.CancelledAt,
	)
	if err != nil {
		s.writeError(w, r, err)
		return WorkflowRun{}, false
	}
	workflowInput := workflows.TextToStoryboardInput{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		WorkflowRunID:  run.ID,
		Prompt:         workflowType,
		CreatedBy:      principal.UserID,
		Input:          inputJSON,
	}
	if _, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        run.TemporalWorkflowID,
		TaskQueue: workflows.ScriptTaskQueue,
	}, workflowFunc, workflowInput); err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE workflow_runs
			SET status = 'failed', error_code = 'TEMPORAL_START_FAILED', error_message = $2, completed_at = now()
			WHERE id = $1
		`, run.ID, err.Error())
		s.writeError(w, r, err)
		return WorkflowRun{}, false
	}
	return run, true
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

func (s *Server) canonicalAsset(r *http.Request, projectID, assetID string) (CanonicalAsset, error) {
	return scanCanonicalAsset(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
	`, projectID, assetID))
}

func (s *Server) assetScenePromptContext(r *http.Request, projectID, assetID string) (string, error) {
	rows, err := s.db.Query(r.Context(), `
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
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_id, reference_type, title, description,
		       artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		       is_primary, status, metadata, created_by, created_at, updated_at
		FROM asset_references
		WHERE project_id = $1 AND asset_id = $2
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
			if presigned, err := s.storage.PresignGetObject(r.Context(), *item.StorageKey, 15*time.Minute); err == nil {
				item.PreviewURL = &presigned.URL
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
	index := map[string]int{}
	for i := range items {
		index[items[i].ID] = i
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, asset_id, reference_type, title, description,
		       artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		       is_primary, status, metadata, created_by, created_at, updated_at
		FROM asset_references
		WHERE project_id = $1
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
			if presigned, err := s.storage.PresignGetObject(r.Context(), *ref.StorageKey, 15*time.Minute); err == nil {
				ref.PreviewURL = &presigned.URL
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
		WHERE project_id = $1 AND asset_id = $2 AND id <> $3
	`, projectID, assetID, referenceID); err != nil {
		return AssetReference{}, err
	}
	ref, err := scanAssetReference(tx.QueryRow(ctx, `
		UPDATE asset_references
		SET is_primary = true, status = 'ready', updated_at = now()
		WHERE project_id = $1 AND asset_id = $2 AND id = $3
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
		LEFT JOIN storyboard_shots ss ON ss.project_id = l.project_id AND ss.script_scene_id = l.script_scene_id
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
	return scanStoryboardShot(s.db.QueryRow(r.Context(), `
		SELECT
			s.id,
			COALESCE(s.workflow_run_id::text, ''),
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			s.duration_seconds,
			COALESCE(s.visual, ''),
			COALESCE(s.camera, ''),
			COALESCE(s.motion, ''),
			COALESCE(s.mood, ''),
			COALESCE(s.image_prompt, ''),
			COALESCE(s.video_prompt, ''),
			s.image_artifact_id,
			s.image_media_file_id,
			COALESCE(s.image_storage_key, ia.storage_key),
			ia.storage_key,
			ia.mime_type,
			s.video_artifact_id,
			s.video_media_file_id,
			COALESCE(s.video_storage_key, va.storage_key),
			va.storage_key,
			va.mime_type,
			s.video_provider_async_task_id,
			s.video_external_task_id,
			COALESCE(s.status, 'pending'),
			COALESCE(s.review_status, 'pending'),
			COALESCE(s.manual_override, false),
			COALESCE(s.stale_state, 'fresh'),
			s.edited_by,
			s.edited_at,
			s.script_scene_id::text,
			sc.id::text,
			COALESCE(sc.scene_no, 0),
			COALESCE(sc.title, ''),
			COALESCE(sc.location, ''),
			COALESCE(sc.characters, '[]'::jsonb)
		FROM storyboard_shots s
		LEFT JOIN artifacts ia ON ia.id = s.image_artifact_id
		LEFT JOIN artifacts va ON va.id = s.video_artifact_id
		LEFT JOIN script_scenes sc ON sc.id = s.script_scene_id
		WHERE s.project_id = $1 AND s.id = $2
	`, projectID, shotID))
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

func normalizeAssetCardDraft(text string) (assetCardDraft, error) {
	candidate := strings.TrimSpace(text)
	if strings.HasPrefix(candidate, "```") {
		candidate = strings.TrimPrefix(candidate, "```json")
		candidate = strings.TrimPrefix(candidate, "```")
		candidate = strings.TrimSuffix(candidate, "```")
		candidate = strings.TrimSpace(candidate)
	}
	var draft assetCardDraft
	if err := json.Unmarshal([]byte(candidate), &draft); err != nil {
		return assetCardDraft{}, err
	}
	if len(draft.Profile) == 0 {
		draft.Profile = json.RawMessage(`{}`)
	}
	var profile map[string]any
	if err := json.Unmarshal(draft.Profile, &profile); err != nil {
		return assetCardDraft{}, fmt.Errorf("profile must be a JSON object")
	}
	normalized, err := json.Marshal(profile)
	if err != nil {
		return assetCardDraft{}, err
	}
	draft.Profile = normalized
	draft.BasePrompt = strings.TrimSpace(draft.BasePrompt)
	draft.ConsistencyPrompt = strings.TrimSpace(draft.ConsistencyPrompt)
	draft.NegativePrompt = strings.TrimSpace(draft.NegativePrompt)
	return draft, nil
}

func shotAssetRequirementSelectSQL(where string) string {
	return `
		SELECT
			r.id, r.organization_id, r.project_id, r.workflow_run_id, r.storyboard_shot_id,
			r.asset_id, r.requirement_type, r.role_in_shot, r.costume, r.pose,
			r.expression, r.action, r.camera_relation, r.scene_state, r.prop_state,
			r.prompt, r.derived_artifact_id, r.derived_media_file_id, r.derived_storage_key,
			r.status, r.review_status, r.manual_override, r.stale_state, r.edited_by, r.edited_at,
			r.metadata, r.created_at, r.updated_at
		FROM shot_asset_requirements r
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
	return value == "draft" || value == "prompt_ready" || value == "image_running" || value == "image_succeeded" || value == "image_failed"
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
