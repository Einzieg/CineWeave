package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/production"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type assetReferenceCreateActionInput struct {
	AssetID          string          `json:"assetId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	StorageKey       string          `json:"storageKey"`
	MimeType         string          `json:"mimeType"`
	ReferenceType    string          `json:"referenceType"`
	SetPrimary       bool            `json:"setPrimary"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

type assetReferenceSetPrimaryActionInput struct {
	AssetID          string `json:"assetId"`
	ReferenceID      string `json:"referenceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type assetReferenceDeleteActionInput struct {
	AssetID          string `json:"assetId"`
	ReferenceID      string `json:"referenceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type assetReferenceCreateActionOutcome struct {
	AssetID   string         `json:"assetId"`
	Revision  int64          `json:"revision"`
	Reference AssetReference `json:"reference"`
}

type assetReferenceSetPrimaryActionOutcome struct {
	AssetID   string         `json:"assetId"`
	Revision  int64          `json:"revision"`
	Reference AssetReference `json:"reference"`
}

type assetReferenceDeleteActionOutcome struct {
	Deleted               bool            `json:"deleted"`
	Mode                  string          `json:"mode"`
	AssetID               string          `json:"assetId"`
	ReferenceID           string          `json:"referenceId"`
	Revision              int64           `json:"revision"`
	ArtifactDeleted       bool            `json:"artifactDeleted"`
	MediaDeleted          bool            `json:"mediaDeleted"`
	ReplacementPrimaryRef *AssetReference `json:"replacementPrimaryReference,omitempty"`
}

func decodeAssetReferenceCreateActionInput(raw json.RawMessage) (assetReferenceCreateActionInput, error) {
	var input assetReferenceCreateActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetReferenceCreateActionInput{}, controlValidationError("asset.reference.create 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.StorageKey = strings.TrimSpace(input.StorageKey)
	input.MimeType = strings.TrimSpace(input.MimeType)
	input.ReferenceType = strings.TrimSpace(input.ReferenceType)
	if input.ReferenceType == "" {
		input.ReferenceType = "uploaded"
	}
	if uuid.Validate(input.AssetID) != nil {
		return assetReferenceCreateActionInput{}, controlValidationError("assetId 无效")
	}
	if input.ExpectedRevision < 1 {
		return assetReferenceCreateActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	if input.StorageKey == "" || !validAssetReferenceMimeType(input.MimeType) || !validAssetReferenceType(input.ReferenceType) {
		return assetReferenceCreateActionInput{}, controlValidationError("storageKey、图片 mimeType 和 referenceType 必须有效")
	}
	if len(input.Metadata) == 0 {
		input.Metadata = json.RawMessage(`{}`)
	} else {
		metadata, err := decodeJSONObject(input.Metadata, "metadata")
		if err != nil {
			return assetReferenceCreateActionInput{}, err
		}
		input.Metadata = metadata
	}
	return input, nil
}

func decodeAssetReferenceSetPrimaryActionInput(raw json.RawMessage) (assetReferenceSetPrimaryActionInput, error) {
	var input assetReferenceSetPrimaryActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetReferenceSetPrimaryActionInput{}, controlValidationError("asset.reference.set_primary 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.ReferenceID = strings.TrimSpace(input.ReferenceID)
	if uuid.Validate(input.AssetID) != nil || uuid.Validate(input.ReferenceID) != nil {
		return assetReferenceSetPrimaryActionInput{}, controlValidationError("assetId 或 referenceId 无效")
	}
	if input.ExpectedRevision < 1 {
		return assetReferenceSetPrimaryActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func decodeAssetReferenceDeleteActionInput(raw json.RawMessage) (assetReferenceDeleteActionInput, error) {
	var input assetReferenceDeleteActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return assetReferenceDeleteActionInput{}, controlValidationError("asset.reference.delete 输入格式无效")
	}
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.ReferenceID = strings.TrimSpace(input.ReferenceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if uuid.Validate(input.AssetID) != nil || uuid.Validate(input.ReferenceID) != nil {
		return assetReferenceDeleteActionInput{}, controlValidationError("assetId 或 referenceId 无效")
	}
	if input.ExpectedRevision < 1 {
		return assetReferenceDeleteActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func (s *Server) createAssetReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input assetReferenceCreateActionInput,
) (assetReferenceCreateActionOutcome, error) {
	asset, err := lockCanonicalAssetForReferenceAction(ctx, tx, project.ID, input.AssetID)
	if err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	if err := validateAssetReferenceActionRevision(asset, input.ExpectedRevision); err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, 'asset_reference_image', $3, $4, $5, $6)
		RETURNING id::text
	`, project.OrganizationID, project.ID, input.StorageKey, input.MimeType, input.Metadata, actorUserID).Scan(&artifactID); err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	var mediaFileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id::text
	`, project.OrganizationID, project.ID, artifactID, input.StorageKey, input.MimeType, input.Metadata, actorUserID).Scan(&mediaFileID); err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	reference, err := scanAssetReference(tx.QueryRow(ctx, `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title, description,
			artifact_id, media_file_id, storage_key, is_primary, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, false, $10, $11)
		RETURNING id, organization_id, project_id, asset_id, reference_type, title, description,
		          artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		          is_primary, status, metadata, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, asset.ID, input.ReferenceType, input.Title, input.Description,
		artifactID, mediaFileID, input.StorageKey, input.Metadata, actorUserID))
	if err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	setPrimary := input.SetPrimary || !canonicalAssetHasPrimaryReference(asset)
	if setPrimary {
		reference, err = s.setPrimaryAssetReferenceTx(ctx, tx, project.ID, asset.ID, reference.ID)
		if err != nil {
			return assetReferenceCreateActionOutcome{}, normalizeAssetReferenceNotFound(err)
		}
	}
	revision, err := bumpCanonicalAssetReferenceRevision(ctx, tx, project.ID, asset.ID, input.ExpectedRevision, actorUserID)
	if err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	if setPrimary {
		if err := markAssetReferenceDownstreamStale(ctx, tx, project.ID, asset.ID); err != nil {
			return assetReferenceCreateActionOutcome{}, err
		}
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "asset.reference.created", "asset_reference", reference.ID, mustRawJSON(map[string]any{
		"assetId": asset.ID, "referenceId": reference.ID, "isPrimary": reference.IsPrimary,
		"revision": revision, "createdBy": actorUserID,
	})); err != nil {
		return assetReferenceCreateActionOutcome{}, err
	}
	return assetReferenceCreateActionOutcome{AssetID: asset.ID, Revision: revision, Reference: reference}, nil
}

func (s *Server) setPrimaryAssetReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input assetReferenceSetPrimaryActionInput,
) (assetReferenceSetPrimaryActionOutcome, error) {
	asset, err := lockCanonicalAssetForReferenceAction(ctx, tx, project.ID, input.AssetID)
	if err != nil {
		return assetReferenceSetPrimaryActionOutcome{}, err
	}
	if err := validateAssetReferenceActionRevision(asset, input.ExpectedRevision); err != nil {
		return assetReferenceSetPrimaryActionOutcome{}, err
	}
	reference, err := s.setPrimaryAssetReferenceTx(ctx, tx, project.ID, asset.ID, input.ReferenceID)
	if err != nil {
		return assetReferenceSetPrimaryActionOutcome{}, normalizeAssetReferenceNotFound(err)
	}
	revision, err := bumpCanonicalAssetReferenceRevision(ctx, tx, project.ID, asset.ID, input.ExpectedRevision, actorUserID)
	if err != nil {
		return assetReferenceSetPrimaryActionOutcome{}, err
	}
	if err := markAssetReferenceDownstreamStale(ctx, tx, project.ID, asset.ID); err != nil {
		return assetReferenceSetPrimaryActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "asset.reference.primary_set", "asset_reference", reference.ID, mustRawJSON(map[string]any{
		"assetId": asset.ID, "referenceId": reference.ID, "revision": revision, "updatedBy": actorUserID,
	})); err != nil {
		return assetReferenceSetPrimaryActionOutcome{}, err
	}
	return assetReferenceSetPrimaryActionOutcome{AssetID: asset.ID, Revision: revision, Reference: reference}, nil
}

func (s *Server) deleteAssetReferenceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input assetReferenceDeleteActionInput,
) (assetReferenceDeleteActionOutcome, error) {
	asset, err := lockCanonicalAssetForReferenceAction(ctx, tx, project.ID, input.AssetID)
	if err != nil {
		return assetReferenceDeleteActionOutcome{}, err
	}
	if err := validateAssetReferenceActionRevision(asset, input.ExpectedRevision); err != nil {
		return assetReferenceDeleteActionOutcome{}, err
	}
	reference, err := scanAssetReference(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, asset_id, reference_type, title, description,
		       artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		       is_primary, status, metadata, created_by, created_at, updated_at
		FROM asset_references
		WHERE project_id = $1 AND asset_id = $2 AND id = $3 AND status = 'ready'
		FOR UPDATE
	`, project.ID, asset.ID, input.ReferenceID))
	if err != nil {
		return assetReferenceDeleteActionOutcome{}, normalizeAssetReferenceNotFound(err)
	}
	wasPrimary := reference.IsPrimary
	reference, err = scanAssetReference(tx.QueryRow(ctx, `
		UPDATE asset_references
		SET status = 'archived', is_primary = false,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'archivedBy', $4::text, 'archiveReason', $5::text, 'archivedAt', now()
		    ),
		    updated_at = now()
		WHERE project_id = $1 AND asset_id = $2 AND id = $3 AND status = 'ready'
		RETURNING id, organization_id, project_id, asset_id, reference_type, title, description,
		          artifact_id, media_file_id, storage_key, preview_url, prompt, prompt_version_id, prompt_hash,
		          is_primary, status, metadata, created_by, created_at, updated_at
	`, project.ID, asset.ID, input.ReferenceID, actorUserID, input.Reason))
	if err != nil {
		return assetReferenceDeleteActionOutcome{}, normalizeAssetReferenceNotFound(err)
	}
	if err := clearCanonicalAssetReferenceTx(ctx, tx, project.ID, asset.ID, reference); err != nil {
		return assetReferenceDeleteActionOutcome{}, err
	}
	var replacement *AssetReference
	if wasPrimary {
		var replacementID string
		err := tx.QueryRow(ctx, `
			SELECT id::text
			FROM asset_references
			WHERE project_id = $1 AND asset_id = $2 AND status = 'ready'
			ORDER BY created_at DESC, id ASC
			LIMIT 1
		`, project.ID, asset.ID).Scan(&replacementID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return assetReferenceDeleteActionOutcome{}, err
		}
		if err == nil {
			selected, err := s.setPrimaryAssetReferenceTx(ctx, tx, project.ID, asset.ID, replacementID)
			if err != nil {
				return assetReferenceDeleteActionOutcome{}, normalizeAssetReferenceNotFound(err)
			}
			replacement = &selected
		}
	}
	revision, err := bumpCanonicalAssetReferenceRevision(ctx, tx, project.ID, asset.ID, input.ExpectedRevision, actorUserID)
	if err != nil {
		return assetReferenceDeleteActionOutcome{}, err
	}
	if err := markAssetReferenceDownstreamStale(ctx, tx, project.ID, asset.ID); err != nil {
		return assetReferenceDeleteActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "asset.reference.archived", "asset_reference", reference.ID, mustRawJSON(map[string]any{
		"assetId": asset.ID, "referenceId": reference.ID, "artifactDeleted": false,
		"mediaDeleted": false, "replacementPrimaryReferenceId": referenceIDOrEmpty(replacement),
		"revision": revision, "archivedBy": actorUserID, "reason": input.Reason,
	})); err != nil {
		return assetReferenceDeleteActionOutcome{}, err
	}
	return assetReferenceDeleteActionOutcome{
		Deleted: true, Mode: "archive", AssetID: asset.ID, ReferenceID: reference.ID,
		Revision: revision, ArtifactDeleted: false, MediaDeleted: false,
		ReplacementPrimaryRef: replacement,
	}, nil
}

func lockCanonicalAssetForReferenceAction(ctx context.Context, tx pgx.Tx, projectID, assetID string) (CanonicalAsset, error) {
	asset, err := scanCanonicalAsset(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, asset_type, name, description, profile, base_prompt, consistency_prompt, negative_prompt, visual_traits,
		       primary_reference_artifact_id, primary_reference_media_file_id, primary_reference_storage_key, lock_reference,
		       reference_artifact_id, reference_media_file_id, reference_storage_key, status, review_status,
		       manual_override, stale_state, edited_by, edited_at, source_script_ids, metadata, created_by, created_at, updated_at,
		       revision, prompt_revision
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, projectID, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CanonicalAsset{}, newAPIError(http.StatusNotFound, "ASSET_NOT_FOUND", "未找到资产")
	}
	return asset, err
}

func validateAssetReferenceActionRevision(asset CanonicalAsset, expectedRevision int64) error {
	if asset.Status == "archived" {
		return newAPIError(http.StatusUnprocessableEntity, "ASSET_ARCHIVED", "归档资产不能修改参考图")
	}
	if asset.Revision != expectedRevision {
		return assetRevisionConflict(expectedRevision, asset)
	}
	return nil
}

func bumpCanonicalAssetReferenceRevision(
	ctx context.Context,
	tx pgx.Tx,
	projectID, assetID string,
	expectedRevision int64,
	actorUserID string,
) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `
		UPDATE canonical_assets
		SET revision = revision + 1,
		    edited_by = NULLIF($4, '')::uuid,
		    edited_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $3
		RETURNING revision
	`, projectID, assetID, expectedRevision, actorUserID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, newAPIError(http.StatusConflict, "ASSET_REVISION_CONFLICT", "资产已被其他操作更新，请重新读取后重试")
	}
	return revision, err
}

func markAssetReferenceDownstreamStale(ctx context.Context, tx pgx.Tx, projectID, assetID string) error {
	if err := production.MarkAssetDownstreamStale(ctx, tx, projectID, assetID); err != nil {
		return err
	}
	return production.MarkFinalVideoStale(ctx, tx, projectID, "")
}

func normalizeAssetReferenceNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return newAPIError(http.StatusNotFound, "ASSET_REFERENCE_NOT_FOUND", "未找到可用的资产参考图")
	}
	return err
}

func referenceIDOrEmpty(reference *AssetReference) string {
	if reference == nil {
		return ""
	}
	return reference.ID
}

func assetReferenceCreateAgentResult(arguments map[string]any, outcome assetReferenceCreateActionOutcome) agentToolResult {
	return agentToolOK("asset.reference.create", arguments, "已添加资产参考图。", map[string]any{
		"assetId": outcome.AssetID, "revision": outcome.Revision, "reference": outcome.Reference,
	})
}

func assetReferenceSetPrimaryAgentResult(arguments map[string]any, outcome assetReferenceSetPrimaryActionOutcome) agentToolResult {
	return agentToolOK("asset.reference.set_primary", arguments, "已更新资产主参考图。", map[string]any{
		"assetId": outcome.AssetID, "revision": outcome.Revision, "reference": outcome.Reference,
	})
}

func assetReferenceDeleteAgentResult(arguments map[string]any, outcome assetReferenceDeleteActionOutcome) agentToolResult {
	return agentToolOK("asset.reference.delete", arguments, "已解除资产参考图关联。", map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "assetId": outcome.AssetID,
		"referenceId": outcome.ReferenceID, "revision": outcome.Revision,
		"artifactDeleted": outcome.ArtifactDeleted, "mediaDeleted": outcome.MediaDeleted,
		"replacementPrimaryReference": outcome.ReplacementPrimaryRef,
	})
}

func validateAssetReferenceActionCommand(commandProjectID, actorUserID, action string) error {
	if strings.TrimSpace(commandProjectID) == "" || strings.TrimSpace(actorUserID) == "" {
		return fmt.Errorf("%s command identity is incomplete", action)
	}
	return nil
}
