package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

type shotProductionContractContext struct {
	EntryStateVersionID  string
	EntryStateRevision   int
	EntryState           videoproduction.ShotState
	EntryStateHash       string
	ExitStateVersionID   string
	ExitStateRevision    int
	ExitState            videoproduction.ShotState
	ExitStateHash        string
	AnchorStateVersionID string
	AnchorStateRevision  int
	AnchorState          videoproduction.ShotState
	AnchorStateHash      string
	TransitionID         string
	Transition           videoproduction.ShotTransition
	TransitionHash       string
	VisualAnchorID       string
	VisualAnchorRole     string
	VisualAnchorRevision int
}

func (a Activities) loadShotProductionContract(ctx context.Context, projectID, shotID string, requestedAnchorRole ...string) (shotProductionContractContext, error) {
	var result shotProductionContractContext
	anchorRole := videoproduction.AnchorRolePlannedFirstFrame
	if len(requestedAnchorRole) > 0 && strings.TrimSpace(requestedAnchorRole[0]) != "" {
		anchorRole = strings.TrimSpace(requestedAnchorRole[0])
	}
	var entryStateRaw, exitStateRaw, anchorStateRaw, carryRaw, resetRaw []byte
	var sourceShotID sql.NullString
	err := a.db.QueryRow(ctx, `
		SELECT entry_state.id::text, entry_state.revision, entry_state.state, entry_state.state_hash,
		       exit_state.id::text, exit_state.revision, exit_state.state, exit_state.state_hash,
		       anchor_state.id::text, anchor_state.revision, anchor_state.state, anchor_state.state_hash,
		       transition.id::text, transition.source_shot_id::text,
		       transition.transition_type, transition.tail_policy, transition.anchor_policy,
		       transition.carry_constraints, transition.reset_constraints,
		       transition.confidence::float8,
		       COALESCE(transition.metadata->>'transitionHash', ''),
		       anchor.id::text, anchor.anchor_role, anchor.revision
		FROM storyboard_shots shot
		JOIN storyboard_shot_state_versions entry_state
		  ON entry_state.storyboard_shot_id = shot.id
		 AND entry_state.project_id = shot.project_id
		 AND entry_state.production_generation_id = shot.production_generation_id
		 AND entry_state.state_role = 'planned_entry'
		 AND entry_state.status = 'approved'
		JOIN storyboard_shot_state_versions exit_state
		  ON exit_state.storyboard_shot_id = shot.id
		 AND exit_state.project_id = shot.project_id
		 AND exit_state.production_generation_id = shot.production_generation_id
		 AND exit_state.state_role = 'planned_exit'
		 AND exit_state.status = 'approved'
		JOIN shot_visual_anchors anchor
		  ON anchor.storyboard_shot_id = shot.id
		 AND anchor.production_generation_id = shot.production_generation_id
		 AND anchor.anchor_role = $3
		 AND anchor.status <> 'archived'
		JOIN storyboard_shot_state_versions anchor_state
		  ON anchor_state.id = anchor.shot_state_version_id
		 AND anchor_state.production_generation_id = shot.production_generation_id
		 AND anchor_state.status = 'approved'
		JOIN storyboard_shot_transitions transition
		  ON transition.target_shot_id = shot.id
		 AND transition.production_generation_id = shot.production_generation_id
		 AND transition.status = 'active'
		WHERE shot.project_id = $1
		  AND shot.id = $2
		  AND shot.deleted_at IS NULL
		ORDER BY anchor.revision DESC
		LIMIT 1
	`, projectID, shotID, anchorRole).Scan(
		&result.EntryStateVersionID, &result.EntryStateRevision, &entryStateRaw, &result.EntryStateHash,
		&result.ExitStateVersionID, &result.ExitStateRevision, &exitStateRaw, &result.ExitStateHash,
		&result.AnchorStateVersionID, &result.AnchorStateRevision, &anchorStateRaw, &result.AnchorStateHash,
		&result.TransitionID, &sourceShotID, &result.Transition.TransitionType,
		&result.Transition.TailPolicy, &result.Transition.AnchorPolicy,
		&carryRaw, &resetRaw, &result.Transition.Confidence, &result.TransitionHash,
		&result.VisualAnchorID, &result.VisualAnchorRole, &result.VisualAnchorRevision,
	)
	if err != nil {
		return shotProductionContractContext{}, err
	}
	if err := json.Unmarshal(entryStateRaw, &result.EntryState); err != nil {
		return shotProductionContractContext{}, fmt.Errorf("decode planned entry state: %w", err)
	}
	if err := json.Unmarshal(exitStateRaw, &result.ExitState); err != nil {
		return shotProductionContractContext{}, fmt.Errorf("decode planned exit state: %w", err)
	}
	if err := json.Unmarshal(anchorStateRaw, &result.AnchorState); err != nil {
		return shotProductionContractContext{}, fmt.Errorf("decode anchor state: %w", err)
	}
	if err := json.Unmarshal(carryRaw, &result.Transition.Carry); err != nil {
		return shotProductionContractContext{}, fmt.Errorf("decode transition carry policy: %w", err)
	}
	if err := json.Unmarshal(resetRaw, &result.Transition.Reset); err != nil {
		return shotProductionContractContext{}, fmt.Errorf("decode transition reset policy: %w", err)
	}
	stateHash, err := videoproduction.HashShotState(result.EntryState)
	if err != nil {
		return shotProductionContractContext{}, err
	}
	if stateHash != result.EntryStateHash {
		return shotProductionContractContext{}, workflowError{Code: "SHOT_STATE_HASH_MISMATCH", Message: "镜头状态内容与已保存 hash 不一致"}
	}
	exitStateHash, err := videoproduction.HashShotState(result.ExitState)
	if err != nil {
		return shotProductionContractContext{}, err
	}
	if exitStateHash != result.ExitStateHash {
		return shotProductionContractContext{}, workflowError{Code: "SHOT_STATE_HASH_MISMATCH", Message: "镜头尾状态内容与已保存 hash 不一致"}
	}
	anchorStateHash, err := videoproduction.HashShotState(result.AnchorState)
	if err != nil {
		return shotProductionContractContext{}, err
	}
	if anchorStateHash != result.AnchorStateHash {
		return shotProductionContractContext{}, workflowError{Code: "SHOT_STATE_HASH_MISMATCH", Message: "视觉锚点状态内容与已保存 hash 不一致"}
	}
	transitionHash, err := videoproduction.HashTransition(result.Transition)
	if err != nil {
		return shotProductionContractContext{}, err
	}
	if strings.TrimSpace(result.TransitionHash) == "" {
		result.TransitionHash = transitionHash
	}
	if transitionHash != result.TransitionHash {
		return shotProductionContractContext{}, workflowError{Code: "SHOT_TRANSITION_HASH_MISMATCH", Message: "镜头转场内容与已保存 hash 不一致"}
	}
	return result, nil
}

type shotReferenceAssetRow struct {
	AssetID               string
	AssetType             string
	AssetName             string
	AssetStatus           string
	AssetStaleState       string
	RequirementID         string
	RequirementStaleState string
	DerivedArtifactID     string
	DerivedMediaFileID    string
	DerivedStorageKey     string
	DerivedContentHash    string
	PrimaryArtifactID     string
	PrimaryMediaFileID    string
	PrimaryStorageKey     string
	PrimaryContentHash    string
	ReferenceArtifactID   string
	ReferenceMediaFileID  string
	ReferenceStorageKey   string
	ReferenceContentHash  string
}

type shotReferencePackPersistenceInput struct {
	OrganizationID string
	ProjectID      string
	WorkflowRunID  string
	ShotID         string
	LinkAnchor     bool
}

func (a Activities) loadShotReferenceCandidates(ctx context.Context, projectID, shotID string, state videoproduction.ShotState) ([]videoproduction.ReferenceCandidate, error) {
	assetIDs := videoproduction.RequiredReferenceAssetIDs(state)
	if len(assetIDs) == 0 {
		return nil, workflowError{Code: videoproduction.CodeReferencePackIncomplete, Message: "镜头状态没有可解析的必需资产"}
	}
	rows, err := a.db.Query(ctx, `
		SELECT a.id::text, a.asset_type, a.name, a.status, COALESCE(a.stale_state, 'fresh'),
		       COALESCE(requirement.id::text, ''), COALESCE(requirement.stale_state, 'fresh'),
		       COALESCE(requirement.derived_artifact_id::text, ''),
		       COALESCE(requirement.derived_media_file_id::text, ''),
		       COALESCE(requirement.derived_storage_key, ''),
		       COALESCE(derived_artifact.content_hash, derived_media.checksum, ''),
		       COALESCE(a.primary_reference_artifact_id::text, ''),
		       COALESCE(a.primary_reference_media_file_id::text, ''),
		       COALESCE(a.primary_reference_storage_key, ''),
		       COALESCE(primary_artifact.content_hash, primary_media.checksum, ''),
		       COALESCE(a.reference_artifact_id::text, ''),
		       COALESCE(a.reference_media_file_id::text, ''),
		       COALESCE(a.reference_storage_key, ''),
		       COALESCE(reference_artifact.content_hash, reference_media.checksum, '')
		FROM canonical_assets a
		LEFT JOIN LATERAL (
			SELECT requirement.*
			FROM shot_asset_requirements requirement
			WHERE requirement.project_id = $1
			  AND requirement.storyboard_shot_id = $2
			  AND requirement.asset_id = a.id
			ORDER BY CASE WHEN requirement.derived_artifact_id IS NOT NULL
			                       OR requirement.derived_media_file_id IS NOT NULL
			                       OR COALESCE(requirement.derived_storage_key, '') <> '' THEN 0 ELSE 1 END,
			         requirement.created_at ASC
			LIMIT 1
		) requirement ON true
		LEFT JOIN artifacts derived_artifact ON derived_artifact.id = requirement.derived_artifact_id
		LEFT JOIN media_files derived_media ON derived_media.id = requirement.derived_media_file_id
		LEFT JOIN artifacts primary_artifact ON primary_artifact.id = a.primary_reference_artifact_id
		LEFT JOIN media_files primary_media ON primary_media.id = a.primary_reference_media_file_id
		LEFT JOIN artifacts reference_artifact ON reference_artifact.id = a.reference_artifact_id
		LEFT JOIN media_files reference_media ON reference_media.id = a.reference_media_file_id
		WHERE a.project_id = $1
		  AND a.id::text = ANY($3::text[])
		ORDER BY a.asset_type, a.name, a.id
	`, projectID, shotID, assetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]videoproduction.ReferenceCandidate, 0, len(assetIDs))
	for rows.Next() {
		var row shotReferenceAssetRow
		if err := rows.Scan(
			&row.AssetID, &row.AssetType, &row.AssetName, &row.AssetStatus, &row.AssetStaleState,
			&row.RequirementID, &row.RequirementStaleState,
			&row.DerivedArtifactID, &row.DerivedMediaFileID, &row.DerivedStorageKey, &row.DerivedContentHash,
			&row.PrimaryArtifactID, &row.PrimaryMediaFileID, &row.PrimaryStorageKey, &row.PrimaryContentHash,
			&row.ReferenceArtifactID, &row.ReferenceMediaFileID, &row.ReferenceStorageKey, &row.ReferenceContentHash,
		); err != nil {
			return nil, err
		}
		candidate, ok, err := preferredReferenceCandidate(row)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, candidate)
		}
	}
	return result, rows.Err()
}

func preferredReferenceCandidate(row shotReferenceAssetRow) (videoproduction.ReferenceCandidate, bool, error) {
	active := strings.TrimSpace(row.AssetStatus) != "archived" && strings.TrimSpace(row.AssetStatus) != "disabled"
	fresh := strings.TrimSpace(row.AssetStaleState) == "" || strings.TrimSpace(row.AssetStaleState) == "fresh"
	if !active || !fresh {
		return videoproduction.ReferenceCandidate{}, false, nil
	}
	type source struct {
		key, sourceType, sourceID, artifactID, mediaFileID, storageKey, contentHash string
		primary, derived                                                            bool
	}
	selected := source{}
	if row.RequirementID != "" && (row.RequirementStaleState == "" || row.RequirementStaleState == "fresh") && hasReferenceMedia(row.DerivedArtifactID, row.DerivedMediaFileID, row.DerivedStorageKey) {
		selected = source{key: "derived:" + row.RequirementID, sourceType: "shot_asset_requirement", sourceID: row.RequirementID, artifactID: row.DerivedArtifactID, mediaFileID: row.DerivedMediaFileID, storageKey: row.DerivedStorageKey, contentHash: row.DerivedContentHash, derived: true}
	} else if hasReferenceMedia(row.PrimaryArtifactID, row.PrimaryMediaFileID, row.PrimaryStorageKey) {
		selected = source{key: "asset_primary:" + row.AssetID, sourceType: "canonical_asset", sourceID: row.AssetID, artifactID: row.PrimaryArtifactID, mediaFileID: row.PrimaryMediaFileID, storageKey: row.PrimaryStorageKey, contentHash: row.PrimaryContentHash, primary: true}
	} else if hasReferenceMedia(row.ReferenceArtifactID, row.ReferenceMediaFileID, row.ReferenceStorageKey) {
		selected = source{key: "asset_reference:" + row.AssetID, sourceType: "canonical_asset", sourceID: row.AssetID, artifactID: row.ReferenceArtifactID, mediaFileID: row.ReferenceMediaFileID, storageKey: row.ReferenceStorageKey, contentHash: row.ReferenceContentHash}
	} else {
		return videoproduction.ReferenceCandidate{}, false, nil
	}
	contentHash := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(selected.contentHash)), "sha256:")
	if len(contentHash) != 64 {
		var err error
		contentHash, err = videoproduction.HashCanonicalContract(map[string]string{
			"referenceKey": selected.key, "artifactId": selected.artifactID,
			"mediaFileId": selected.mediaFileID, "storageKey": selected.storageKey,
		})
		if err != nil {
			return videoproduction.ReferenceCandidate{}, false, err
		}
	}
	referenceRole := referenceRoleForAssetType(row.AssetType)
	return videoproduction.ReferenceCandidate{
		ReferenceKey: selected.key,
		Role:         referenceRole,
		Required:     true,
		Primary:      selected.primary,
		Derived:      selected.derived,
		Priority:     referencePriorityForAssetType(row.AssetType),
		SourceType:   selected.sourceType,
		SourceID:     selected.sourceID,
		AssetID:      row.AssetID,
		ArtifactID:   selected.artifactID,
		MediaFileID:  selected.mediaFileID,
		StorageKey:   selected.storageKey,
		MediaType:    "image",
		Semantics:    videoproduction.ReferenceSemanticsForRole(referenceRole),
		ContentHash:  contentHash,
		Active:       true,
		Fresh:        true,
		Metadata: map[string]any{
			"assetType": row.AssetType,
			"assetName": row.AssetName,
		},
	}, true, nil
}

func hasReferenceMedia(artifactID, mediaFileID, storageKey string) bool {
	return strings.TrimSpace(artifactID) != "" || strings.TrimSpace(mediaFileID) != "" || strings.TrimSpace(storageKey) != ""
}

func referenceRoleForAssetType(assetType string) string {
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "character":
		return videoproduction.ReferenceRoleCharacterIdentity
	case "scene":
		return videoproduction.ReferenceRoleSceneIdentity
	case "prop":
		return videoproduction.ReferenceRolePropIdentity
	default:
		return videoproduction.ReferenceRoleStyle
	}
}

func referencePriorityForAssetType(assetType string) int {
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "character":
		return 1000
	case "scene":
		return 900
	case "prop":
		return 800
	default:
		return 100
	}
}

func resolveAnchorReferencePack(project ProjectProductionSettings, contract shotProductionContractContext, candidates []videoproduction.ReferenceCandidate, constraints []provider.GatewayModelConstraintCandidate) (videoproduction.ReferencePack, error) {
	capabilityHash, err := videoproduction.HashCanonicalContract(constraints)
	if err != nil {
		return videoproduction.ReferencePack{}, err
	}
	maxReferences := 0
	for _, candidate := range constraints {
		if !candidate.References.Supported {
			continue
		}
		if candidate.References.MaxReferences > 0 && (maxReferences == 0 || candidate.References.MaxReferences < maxReferences) {
			maxReferences = candidate.References.MaxReferences
		}
	}
	if maxReferences == 0 {
		return videoproduction.ReferencePack{}, workflowError{Code: provider.CodeModelCapabilityUnavailable, Message: "当前图片模型没有声明参考图能力，无法生成可靠首帧"}
	}
	return videoproduction.ResolveReferencePack(videoproduction.ReferenceResolveInput{
		ProfileKey:             project.VideoProductionProfileKey,
		Purpose:                videoproduction.ReferencePurposeAnchor,
		ShotStateRevision:      contract.AnchorStateRevision,
		ProfileSnapshotHash:    project.VideoProductionProfileHash,
		ShotStateHash:          contract.AnchorStateHash,
		CapabilitySnapshotHash: capabilityHash,
		RequiredAssetIDs:       videoproduction.RequiredReferenceAssetIDs(contract.AnchorState),
		MaxReferences:          maxReferences,
		Candidates:             candidates,
	})
}

func (a Activities) loadApprovedPlannedAnchorCandidate(ctx context.Context, projectID, shotID, anchorRole string) (videoproduction.ReferenceCandidate, error) {
	referenceRole := ""
	switch strings.TrimSpace(anchorRole) {
	case videoproduction.AnchorRolePlannedFirstFrame:
		referenceRole = videoproduction.ReferenceRoleFirstFrame
	case videoproduction.AnchorRolePlannedLastFrame:
		referenceRole = videoproduction.ReferenceRoleLastFrame
	case videoproduction.AnchorRoleStoryboardSheet:
		referenceRole = videoproduction.ReferenceRoleStoryboardSheet
	default:
		return videoproduction.ReferenceCandidate{}, workflowError{Code: videoproduction.CodeReferencePackIncomplete, Message: "不支持的视频视觉锚点角色：" + anchorRole}
	}
	var anchorID, artifactID, mediaFileID, storageKey, contentHash string
	err := a.db.QueryRow(ctx, `
		SELECT anchor.id::text,
		       COALESCE(anchor.artifact_id::text, ''),
		       COALESCE(anchor.media_file_id::text, ''),
		       COALESCE(anchor.storage_key, ''),
		       COALESCE(artifact.content_hash, media.checksum, '')
		FROM shot_visual_anchors anchor
		JOIN storyboard_shots shot
		  ON shot.id = anchor.storyboard_shot_id
		 AND shot.project_id = anchor.project_id
		 AND shot.production_generation_id = anchor.production_generation_id
		LEFT JOIN artifacts artifact ON artifact.id = anchor.artifact_id
		LEFT JOIN media_files media ON media.id = anchor.media_file_id
		WHERE anchor.project_id = $1
		  AND anchor.storyboard_shot_id = $2
		  AND anchor.anchor_role = $3
		  AND anchor.status = 'ready'
		  AND anchor.review_status = 'approved'
		  AND shot.deleted_at IS NULL
		ORDER BY anchor.revision DESC
		LIMIT 1
	`, projectID, shotID, anchorRole).Scan(&anchorID, &artifactID, &mediaFileID, &storageKey, &contentHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return videoproduction.ReferenceCandidate{}, workflowError{
				Code:    videoproduction.CodeReferencePackIncomplete,
				Message: "当前镜头没有已审核通过的" + anchorRole + "，不能生成视频提示词或视频",
			}
		}
		return videoproduction.ReferenceCandidate{}, err
	}
	if !hasReferenceMedia(artifactID, mediaFileID, storageKey) {
		return videoproduction.ReferenceCandidate{}, workflowError{
			Code:    videoproduction.CodeReferencePackIncomplete,
			Message: "已审核视觉锚点缺少可用媒体：" + anchorRole,
		}
	}
	contentHash = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(contentHash)), "sha256:")
	if len(contentHash) != 64 {
		var hashErr error
		contentHash, hashErr = videoproduction.HashCanonicalContract(map[string]string{
			"anchorId": anchorID, "artifactId": artifactID, "mediaFileId": mediaFileID, "storageKey": storageKey,
		})
		if hashErr != nil {
			return videoproduction.ReferenceCandidate{}, hashErr
		}
	}
	metadata := map[string]any{"anchorRole": anchorRole, "reviewStatus": "approved"}
	if anchorRole == videoproduction.AnchorRoleStoryboardSheet {
		manifest, manifestErr := a.loadApprovedStoryboardSheetManifest(ctx, shotID)
		if manifestErr != nil {
			return videoproduction.ReferenceCandidate{}, manifestErr
		}
		metadata["panelManifestId"] = manifest.ID
		metadata["panelManifestHash"] = manifest.Manifest.ManifestHash
		metadata["panelCount"] = manifest.Manifest.PanelCount
		metadata["panelManifestContractVersion"] = manifest.Manifest.ContractVersion
	}
	return videoproduction.ReferenceCandidate{
		ReferenceKey: anchorRole + ":" + anchorID,
		Role:         referenceRole,
		Required:     true,
		Primary:      true,
		Priority:     1000,
		SourceType:   "shot_visual_anchor",
		SourceID:     anchorID,
		ArtifactID:   artifactID,
		MediaFileID:  mediaFileID,
		StorageKey:   storageKey,
		MediaType:    "image",
		Semantics:    videoproduction.ReferenceSemanticsForRole(referenceRole),
		ContentHash:  contentHash,
		Active:       true,
		Fresh:        true,
		Metadata:     metadata,
	}, nil
}

func (a Activities) loadApprovedProfileAnchorCandidates(ctx context.Context, projectID, shotID, profileKey string) ([]videoproduction.ReferenceCandidate, error) {
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return nil, err
	}
	candidates := make([]videoproduction.ReferenceCandidate, 0, len(strategy.Anchors().Requirements()))
	for _, requirement := range strategy.Anchors().Requirements() {
		if !requirement.Required || requirement.Role == videoproduction.AnchorRoleStoryboardPanel {
			continue
		}
		candidate, err := a.loadApprovedPlannedAnchorCandidate(ctx, projectID, shotID, requirement.Role)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (a Activities) loadProfileVideoReferenceCandidates(
	ctx context.Context,
	projectID, shotID, profileKey string,
	contract shotProductionContractContext,
) ([]videoproduction.ReferenceCandidate, error) {
	candidates, err := a.loadApprovedProfileAnchorCandidates(ctx, projectID, shotID, profileKey)
	if err != nil {
		return nil, err
	}
	if profileKey != videoproduction.ProfileMultimodalReference {
		return candidates, nil
	}
	assetCandidates, err := a.loadShotReferenceCandidates(ctx, projectID, shotID, contract.EntryState)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, assetCandidates...)
	continuityHint, ok, err := a.loadApprovedContinuityHintCandidate(ctx, projectID, shotID)
	if err != nil {
		return nil, err
	}
	if ok {
		candidates = append(candidates, continuityHint)
	}
	return candidates, nil
}

func (a Activities) loadApprovedContinuityHintCandidate(ctx context.Context, projectID, shotID string) (videoproduction.ReferenceCandidate, bool, error) {
	var anchorID, artifactID, mediaFileID, storageKey, contentHash string
	err := a.db.QueryRow(ctx, `
		SELECT anchor.id::text,
		       COALESCE(anchor.artifact_id::text, ''), COALESCE(anchor.media_file_id::text, ''),
		       COALESCE(anchor.storage_key, ''), COALESCE(artifact.content_hash, media.checksum, '')
		FROM storyboard_shot_transitions transition
		JOIN storyboard_shots target ON target.id = transition.target_shot_id
		JOIN shot_visual_anchors anchor
		  ON anchor.storyboard_shot_id = transition.source_shot_id
		 AND anchor.production_generation_id = target.production_generation_id
		 AND anchor.anchor_role = 'observed_tail_frame'
		 AND anchor.status = 'ready' AND anchor.review_status = 'approved'
		LEFT JOIN artifacts artifact ON artifact.id = anchor.artifact_id
		LEFT JOIN media_files media ON media.id = anchor.media_file_id
		WHERE transition.project_id = $1 AND transition.target_shot_id = $2
		  AND transition.status = 'active' AND transition.review_status = 'approved'
		  AND transition.tail_policy = 'soft'
		ORDER BY anchor.revision DESC
		LIMIT 1
	`, projectID, shotID).Scan(&anchorID, &artifactID, &mediaFileID, &storageKey, &contentHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return videoproduction.ReferenceCandidate{}, false, nil
	}
	if err != nil {
		return videoproduction.ReferenceCandidate{}, false, err
	}
	if !hasReferenceMedia(artifactID, mediaFileID, storageKey) {
		return videoproduction.ReferenceCandidate{}, false, nil
	}
	contentHash = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(contentHash)), "sha256:")
	if len(contentHash) != 64 {
		contentHash, err = videoproduction.HashCanonicalContract(map[string]string{
			"anchorId": anchorID, "artifactId": artifactID, "mediaFileId": mediaFileID, "storageKey": storageKey,
		})
		if err != nil {
			return videoproduction.ReferenceCandidate{}, false, err
		}
	}
	return videoproduction.ReferenceCandidate{
		ReferenceKey: "continuity_hint:" + anchorID,
		Role:         videoproduction.ReferenceRoleContinuityHint,
		Priority:     100,
		SourceType:   "shot_visual_anchor",
		SourceID:     anchorID,
		ArtifactID:   artifactID,
		MediaFileID:  mediaFileID,
		StorageKey:   storageKey,
		MediaType:    "image",
		Semantics:    videoproduction.ReferenceSemanticsForRole(videoproduction.ReferenceRoleContinuityHint),
		ContentHash:  contentHash,
		Active:       true,
		Fresh:        true,
		Metadata:     map[string]any{"softReference": true},
	}, true, nil
}

func (a Activities) loadApprovedPlannedFirstFrameCandidate(ctx context.Context, projectID, shotID string) (videoproduction.ReferenceCandidate, error) {
	return a.loadApprovedPlannedAnchorCandidate(ctx, projectID, shotID, videoproduction.AnchorRolePlannedFirstFrame)
}

func resolveVideoReferencePack(project ProjectProductionSettings, contract shotProductionContractContext, anchorCandidates []videoproduction.ReferenceCandidate, constraints []provider.GatewayModelConstraintCandidate) (videoproduction.ReferencePack, error) {
	capabilityHash, err := videoproduction.HashCanonicalContract(constraints)
	if err != nil {
		return videoproduction.ReferencePack{}, err
	}
	strategy, err := videoproduction.ProfileStrategyFor(project.VideoProductionProfileKey)
	if err != nil {
		return videoproduction.ReferencePack{}, err
	}
	compatible := false
	maxReferences, maxImages, maxVideos, maxAudios := 0, 0, 0, 0
	initialContract := strategy.InputAdapter().InitialContract()
	for _, candidate := range constraints {
		if !candidate.References.Supported {
			continue
		}
		candidateCompatible := false
		switch initialContract {
		case videoproduction.InputContractFirstFrame:
			candidateCompatible = candidate.References.SupportsFirstFrame
		case videoproduction.InputContractFirstLastFrames:
			candidateCompatible = candidate.References.SupportsFirstFrame && candidate.References.SupportsLastFrame
		case videoproduction.InputContractFirstFramePlusReferences:
			candidateCompatible = candidate.References.SupportsFirstFrame && candidate.References.SupportsSemanticReferenceImages
		case videoproduction.InputContractStoryboardSheetReference:
			candidateCompatible = candidate.References.SupportsStoryboardSheetReference
		}
		if len(candidate.References.InputContracts) > 0 && !containsStringFold(candidate.References.InputContracts, initialContract) {
			candidateCompatible = false
		}
		if !candidateCompatible {
			continue
		}
		compatible = true
		maxReferences = maxReferenceInt(maxReferences, candidate.References.MaxReferences)
		maxImages = maxReferenceInt(maxImages, candidate.References.MaxImageReferences)
		maxVideos = maxReferenceInt(maxVideos, candidate.References.MaxVideoReferences)
		maxAudios = maxReferenceInt(maxAudios, candidate.References.MaxAudioReferences)
	}
	if !compatible {
		return videoproduction.ReferencePack{}, workflowError{
			Code:    provider.CodeModelCapabilityUnavailable,
			Message: "当前视频模型没有满足生产方案输入契约的参考能力：" + initialContract,
		}
	}
	return videoproduction.ResolveReferencePack(videoproduction.ReferenceResolveInput{
		ProfileKey:             project.VideoProductionProfileKey,
		Purpose:                videoproduction.ReferencePurposeVideo,
		ShotStateRevision:      contract.EntryStateRevision,
		ProfileSnapshotHash:    project.VideoProductionProfileHash,
		ShotStateHash:          contract.EntryStateHash,
		CapabilitySnapshotHash: capabilityHash,
		MaxReferences:          maxReferences,
		MaxImageReferences:     maxImages,
		MaxVideoReferences:     maxVideos,
		MaxAudioReferences:     maxAudios,
		Candidates:             anchorCandidates,
	})
}

func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func maxReferenceInt(left, right int) int {
	if right > left {
		return right
	}
	return left
}

func videoReferenceModeForProfile(profileKey string) string {
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return "first_frame"
	}
	return strategy.InputAdapter().InitialContract()
}

func (a Activities) persistShotReferencePack(ctx context.Context, input PrepareShotImagePromptInput, execution NodeExecution, project ProjectProductionSettings, contract shotProductionContractContext, pack videoproduction.ReferencePack) (string, error) {
	return a.persistShotReferencePackForPurpose(ctx, shotReferencePackPersistenceInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		ShotID:         input.ShotID,
		LinkAnchor:     true,
	}, execution, project, contract, pack)
}

func (a Activities) persistShotReferencePackForPurpose(ctx context.Context, input shotReferencePackPersistenceInput, execution NodeExecution, project ProjectProductionSettings, contract shotProductionContractContext, pack videoproduction.ReferencePack) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	runContext, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution)
	if err != nil {
		return "", err
	}
	if runContext.ProductionGenerationID != project.ProductionGenerationID || runContext.VideoProductionBindingID != project.VideoProductionBindingID || runContext.VideoProductionBindingRevision != project.VideoProductionBindingRevision {
		return "", ErrWorkflowWriteFenced
	}
	var existingID, existingHash string
	err = tx.QueryRow(ctx, `
		SELECT id::text, manifest_hash
		FROM shot_reference_packs
		WHERE storyboard_shot_id = $1 AND purpose = $2 AND status = 'active'
		FOR UPDATE
	`, input.ShotID, pack.Manifest.Purpose).Scan(&existingID, &existingHash)
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}
	if err == nil && existingHash == pack.ManifestHash {
		if input.LinkAnchor {
			if _, err := tx.Exec(ctx, `UPDATE shot_visual_anchors SET reference_pack_id = $2 WHERE id = $1`, contract.VisualAnchorID, existingID); err != nil {
				return "", err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return existingID, nil
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE shot_reference_packs SET status = 'stale' WHERE id = $1`, existingID); err != nil {
			return "", err
		}
	}
	var packID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO shot_reference_packs(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			purpose,
			profile_snapshot_hash, shot_state_hash, capability_snapshot_hash,
			manifest, manifest_hash, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'active')
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, project.ProductionGenerationID, input.ShotID, pack.Manifest.Purpose,
		pack.ProfileSnapshotHash, pack.ShotStateHash, pack.CapabilitySnapshotHash,
		mustJSON(pack.Manifest), pack.ManifestHash).Scan(&packID); err != nil {
		return "", err
	}
	for _, item := range pack.Manifest.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO shot_reference_pack_items(
				reference_pack_id, reference_key, role, required, priority,
				source_type, source_id, asset_id, artifact_id, media_file_id,
				storage_key, media_type, semantics, content_hash, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid,
			        NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, NULLIF($10, '')::uuid,
			        NULLIF($11, ''), $12, $13, $14, $15)
		`, packID, item.ReferenceKey, item.Role, item.Required, item.Priority,
			item.SourceType, item.SourceID, item.AssetID, item.ArtifactID, item.MediaFileID,
			item.StorageKey, item.MediaType, item.Semantics, item.ContentHash, mustJSON(item.Metadata)); err != nil {
			return "", err
		}
	}
	if input.LinkAnchor {
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET reference_pack_id = $2,
			    metadata = metadata || jsonb_build_object(
			      'referencePackHash', $3::text,
			      'capabilitySnapshotHash', $4::text
			    )
			WHERE id = $1
		`, contract.VisualAnchorID, packID, pack.ManifestHash, pack.CapabilitySnapshotHash); err != nil {
			return "", err
		}
	}
	payload := mustJSON(map[string]any{
		"bindingId":              project.VideoProductionBindingID,
		"bindingRevision":        project.VideoProductionBindingRevision,
		"productionGenerationId": project.ProductionGenerationID,
		"episodeId":              "",
		"storyboardShotId":       input.ShotID,
		"workflowRunId":          input.WorkflowRunID,
		"referencePackId":        packID,
		"manifestHash":           pack.ManifestHash,
		"referenceCount":         len(pack.Manifest.Items),
	})
	var episodeID string
	if err := tx.QueryRow(ctx, `SELECT script_episode_id::text FROM storyboard_shots WHERE id = $1`, input.ShotID).Scan(&episodeID); err != nil {
		return "", err
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return "", err
	}
	payloadMap["episodeId"] = episodeID
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.reference_pack.compiled", "shot_reference_pack", packID, mustJSON(payloadMap)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return packID, nil
}

func gatewayImageReferencesFromPack(pack videoproduction.ReferencePack) []provider.GatewayImageReference {
	result := make([]provider.GatewayImageReference, 0, len(pack.Manifest.Items))
	for _, item := range pack.Manifest.Items {
		result = append(result, provider.GatewayImageReference{
			Type:       item.MediaType,
			AssetID:    item.AssetID,
			ArtifactID: item.ArtifactID,
			StorageKey: item.StorageKey,
			Metadata: mustJSON(map[string]any{
				"referenceKey": item.ReferenceKey,
				"role":         item.Role,
				"required":     item.Required,
				"contentHash":  item.ContentHash,
				"mediaType":    item.MediaType,
				"semantics":    item.Semantics,
			}),
		})
	}
	return result
}
