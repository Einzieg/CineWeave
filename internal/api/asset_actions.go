package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

type assetUpdateActionInput struct {
	AssetID          string
	ExpectedRevision int64
	Patch            assetUpdateActionPatch
}

type assetUpdateActionPatch struct {
	AssetType         *string
	Name              *string
	Description       *string
	Profile           json.RawMessage
	ProfileSet        bool
	BasePrompt        *string
	ConsistencyPrompt *string
	NegativePrompt    *string
	LockReference     *bool
	VisualTraits      json.RawMessage
	VisualTraitsSet   bool
	Metadata          json.RawMessage
	MetadataSet       bool
	Status            *string
}

type assetUpdateActionOutcome struct {
	Asset CanonicalAsset `json:"asset"`
}

type assetDeleteActionInput struct {
	AssetID          string `json:"assetId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type assetDeleteActionOutcome struct {
	Deleted  bool   `json:"deleted"`
	Mode     string `json:"mode"`
	AssetID  string `json:"assetId"`
	Revision int64  `json:"revision"`
}

type assetUpdateActionWire struct {
	AssetID          string          `json:"assetId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Patch            json.RawMessage `json:"patch"`
}

type assetUpdatePatchWire struct {
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

func decodeAssetUpdateActionInput(raw json.RawMessage) (assetUpdateActionInput, error) {
	var wire assetUpdateActionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return assetUpdateActionInput{}, controlValidationError("asset.update 输入格式无效")
	}
	var patchFields map[string]json.RawMessage
	if err := json.Unmarshal(wire.Patch, &patchFields); err != nil || len(patchFields) == 0 {
		return assetUpdateActionInput{}, controlValidationError("patch 必须是非空对象")
	}
	var patchWire assetUpdatePatchWire
	if err := json.Unmarshal(wire.Patch, &patchWire); err != nil {
		return assetUpdateActionInput{}, controlValidationError("asset.update patch 格式无效")
	}
	input := assetUpdateActionInput{
		AssetID: strings.TrimSpace(wire.AssetID), ExpectedRevision: wire.ExpectedRevision,
		Patch: assetUpdateActionPatch{
			AssetType: patchWire.AssetType, Name: patchWire.Name, Description: patchWire.Description,
			Profile: patchWire.Profile, BasePrompt: patchWire.BasePrompt,
			ConsistencyPrompt: patchWire.ConsistencyPrompt, NegativePrompt: patchWire.NegativePrompt,
			LockReference: patchWire.LockReference, VisualTraits: patchWire.VisualTraits,
			Metadata: patchWire.Metadata, Status: patchWire.Status,
		},
	}
	_, input.Patch.ProfileSet = patchFields["profile"]
	_, input.Patch.VisualTraitsSet = patchFields["visualTraits"]
	_, input.Patch.MetadataSet = patchFields["metadata"]
	if input.AssetID == "" {
		return assetUpdateActionInput{}, controlValidationError("assetId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return assetUpdateActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func decodeAssetDeleteActionInput(raw json.RawMessage) (assetDeleteActionInput, error) {
	var input assetDeleteActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetDeleteActionInput{}, controlValidationError("asset.delete 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.AssetID == "" {
		return assetDeleteActionInput{}, controlValidationError("assetId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return assetDeleteActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func decodeJSONObject(raw json.RawMessage, field string) (json.RawMessage, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", field+" 必须是 JSON 对象")
	}
	return mustRawJSON(value), nil
}

func (s *Server) updateCanonicalAssetActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input assetUpdateActionInput,
) (assetUpdateActionOutcome, error) {
	current, err := scanCanonicalAsset(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		       revision, prompt_revision
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, project.ID, input.AssetID))
	if err != nil {
		return assetUpdateActionOutcome{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return assetUpdateActionOutcome{}, assetRevisionConflict(input.ExpectedRevision, current)
	}

	patch := input.Patch
	assetType := current.AssetType
	if patch.AssetType != nil {
		assetType = strings.TrimSpace(*patch.AssetType)
	}
	name := current.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
	}
	description := current.Description
	if patch.Description != nil {
		description = strings.TrimSpace(*patch.Description)
	}
	status := current.Status
	if patch.Status != nil {
		status = strings.TrimSpace(*patch.Status)
	}
	if !validCanonicalAssetType(assetType) || name == "" || description == "" || !validCanonicalAssetStatus(status) {
		return assetUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "资产字段无效")
	}

	profile := current.Profile
	if patch.ProfileSet {
		profile, err = decodeJSONObject(patch.Profile, "profile")
		if err != nil {
			return assetUpdateActionOutcome{}, err
		}
	}
	visualTraits := current.VisualTraits
	if patch.VisualTraitsSet {
		visualTraits, err = decodeJSONObject(patch.VisualTraits, "visualTraits")
		if err != nil {
			return assetUpdateActionOutcome{}, err
		}
	}
	metadata := current.Metadata
	if patch.MetadataSet {
		metadata, err = decodeJSONObject(patch.Metadata, "metadata")
		if err != nil {
			return assetUpdateActionOutcome{}, err
		}
	}

	basePromptSet := patch.BasePrompt != nil
	basePrompt := stringValue(current.BasePrompt)
	if patch.BasePrompt != nil {
		basePrompt = strings.TrimSpace(*patch.BasePrompt)
	}
	consistencyPromptSet := patch.ConsistencyPrompt != nil
	consistencyPrompt := stringValue(current.ConsistencyPrompt)
	if patch.ConsistencyPrompt != nil {
		consistencyPrompt = strings.TrimSpace(*patch.ConsistencyPrompt)
	}
	negativePromptSet := patch.NegativePrompt != nil
	negativePrompt := stringValue(current.NegativePrompt)
	if patch.NegativePrompt != nil {
		negativePrompt = strings.TrimSpace(*patch.NegativePrompt)
	}
	if patch.Status == nil && (basePromptSet || consistencyPromptSet || negativePromptSet || patch.ProfileSet) {
		if canonicalAssetPromptFieldsReady(basePrompt, consistencyPrompt) {
			status = "prompt_ready"
		} else if status == "prompt_ready" {
			status = "draft"
		}
	}
	lockReference := current.LockReference
	if patch.LockReference != nil {
		lockReference = *patch.LockReference
	}
	promptChanged := patch.AssetType != nil || patch.Name != nil || patch.Description != nil || patch.ProfileSet ||
		basePromptSet || consistencyPromptSet || negativePromptSet || patch.LockReference != nil || patch.VisualTraitsSet

	item, err := scanCanonicalAsset(tx.QueryRow(ctx, `
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
		    revision = revision + 1,
		    prompt_revision = prompt_revision + CASE WHEN $19 THEN 1 ELSE 0 END,
		    updated_at = now()
		WHERE id = $1 AND project_id = $2 AND revision = $18
		RETURNING id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		          primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		          reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		          manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		          revision, prompt_revision
	`, current.ID, project.ID, assetType, name, description, profile, basePromptSet, basePrompt,
		consistencyPromptSet, consistencyPrompt, negativePromptSet, negativePrompt, lockReference,
		visualTraits, metadata, status, actorUserID, input.ExpectedRevision, promptChanged))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return assetUpdateActionOutcome{}, assetRevisionConflict(input.ExpectedRevision, current)
		}
		return assetUpdateActionOutcome{}, err
	}
	if err := production.MarkAssetDownstreamStale(ctx, tx, project.ID, current.ID); err != nil {
		return assetUpdateActionOutcome{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return assetUpdateActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "asset.updated", "canonical_asset", item.ID, mustRawJSON(map[string]any{
		"assetId": item.ID, "manualOverride": item.ManualOverride, "staleState": item.StaleState,
	})); err != nil {
		return assetUpdateActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "asset.card.updated", "canonical_asset", item.ID, mustRawJSON(map[string]any{
		"assetId": item.ID, "manualOverride": item.ManualOverride, "lockReference": item.LockReference,
	})); err != nil {
		return assetUpdateActionOutcome{}, err
	}
	return assetUpdateActionOutcome{Asset: item}, nil
}

func (s *Server) deleteCanonicalAssetActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input assetDeleteActionInput,
) (assetDeleteActionOutcome, error) {
	current, err := scanCanonicalAsset(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		       revision, prompt_revision
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, project.ID, input.AssetID))
	if err != nil {
		return assetDeleteActionOutcome{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return assetDeleteActionOutcome{}, assetRevisionConflict(input.ExpectedRevision, current)
	}
	if current.Status == "archived" {
		return assetDeleteActionOutcome{}, newAPIError(http.StatusConflict, "ASSET_ALREADY_ARCHIVED", "资产已经归档")
	}

	archivedAt := time.Now().UTC().Format(time.RFC3339)
	metadataPatch := mustRawJSON(map[string]any{
		"archivedAt": archivedAt,
		"archivedBy": nullableMetadataValue(actorUserID),
		"reason":     input.Reason,
	})
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE canonical_assets
		SET status = 'archived',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    revision = revision + 1,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $4
		RETURNING revision
	`, project.ID, current.ID, metadataPatch, input.ExpectedRevision).Scan(&revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return assetDeleteActionOutcome{}, assetRevisionConflict(input.ExpectedRevision, current)
		}
		return assetDeleteActionOutcome{}, err
	}
	if err := production.MarkAssetDownstreamStale(ctx, tx, project.ID, current.ID); err != nil {
		return assetDeleteActionOutcome{}, err
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return assetDeleteActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "canonical_asset.archived", "canonical_asset", current.ID, mustRawJSON(map[string]any{
		"assetId": current.ID, "assetType": current.AssetType,
		"archivedAt": archivedAt, "archivedBy": nullableMetadataValue(actorUserID),
		"reason": input.Reason, "revision": revision,
	})); err != nil {
		return assetDeleteActionOutcome{}, err
	}
	return assetDeleteActionOutcome{Deleted: true, Mode: "archive", AssetID: current.ID, Revision: revision}, nil
}

func assetRevisionConflict(expected int64, current CanonicalAsset) error {
	return apiError{
		Status: http.StatusConflict, Code: "ASSET_REVISION_CONFLICT",
		Message: "资产已被其他操作更新，请合并最新内容后重试", Retryable: true,
		Details: map[string]any{
			"expectedRevision": expected, "currentRevision": current.Revision,
			"updatedAt": current.UpdatedAt, "status": current.Status,
		},
	}
}

func assetUpdateAgentResult(arguments map[string]any, outcome assetUpdateActionOutcome) agentToolResult {
	return agentToolOK("asset.update", arguments, "已更新资产卡并同步下游状态。", map[string]any{
		"asset": outcome.Asset, "assetId": outcome.Asset.ID, "revision": outcome.Asset.Revision,
		"promptRevision": outcome.Asset.PromptRevision, "status": outcome.Asset.Status,
	})
}

func assetDeleteAgentResult(arguments map[string]any, outcome assetDeleteActionOutcome) agentToolResult {
	return agentToolOK("asset.delete", arguments, "已归档核心资产并同步下游状态。", map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "assetId": outcome.AssetID, "revision": outcome.Revision,
	})
}
