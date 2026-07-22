package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

const nodeCompileStoryboardSheetManifestPrefix = "storyboard_sheet_manifest_compile"

type storyboardSheetManifestRuntime struct {
	ID                     string                        `json:"id"`
	ProductionGenerationID string                        `json:"productionGenerationId"`
	Revision               int                           `json:"revision"`
	Status                 string                        `json:"status"`
	ReviewStatus           string                        `json:"reviewStatus"`
	Manifest               videoproduction.PanelManifest `json:"manifest"`
}

func (a Activities) ensureStoryboardSheetManifest(
	ctx context.Context,
	input PrepareShotImagePromptInput,
	project ProjectProductionSettings,
	shot StoryboardShotRecord,
	contract shotProductionContractContext,
) (storyboardSheetManifestRuntime, error) {
	manifest, err := videoproduction.CompilePanelManifest(videoproduction.PanelManifestCompileInput{
		StoryboardShotID: shot.ID, PlannedDurationTicks: shot.PlannedDurationTicks,
		TimelineTimebase: shot.TimelineTimebase,
		VideoAspectRatio: firstNonEmptyString(project.VideoRatio, project.AspectRatio, input.AspectRatio, "16:9"),
		EntryState:       contract.EntryState, ExitState: contract.ExitState,
	})
	if err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if existing, ok, err := a.findStoryboardSheetManifest(ctx, shot.ID, manifest.ManifestHash, false); err != nil {
		return storyboardSheetManifestRuntime{}, err
	} else if ok {
		return existing, nil
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		NodeKey:  nodeKeyForID(nodeCompileStoryboardSheetManifestPrefix, shot.ID) + "_" + manifest.ManifestHash[:12],
		NodeType: "storyboard_sheet.manifest.compile",
		Input: mustJSON(map[string]any{
			"storyboardShotId": shot.ID, "manifestHash": manifest.ManifestHash,
			"panelCount": manifest.PanelCount, "rows": manifest.Rows, "columns": manifest.Columns,
		}),
	})
	if err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if existing, ok, err := findStoryboardSheetManifestTx(ctx, tx, shot.ID, manifest.ManifestHash, true); err != nil {
		return storyboardSheetManifestRuntime{}, err
	} else if ok {
		if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(existing)); err != nil {
			return storyboardSheetManifestRuntime{}, err
		} else if !applied {
			return storyboardSheetManifestRuntime{}, ErrWorkflowWriteFenced
		}
		if err := tx.Commit(ctx); err != nil {
			return storyboardSheetManifestRuntime{}, err
		}
		return existing, nil
	}
	var activeGenerationID string
	if err := tx.QueryRow(ctx, `
		SELECT production_generation_id::text
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2 AND production_generation_id = $3 AND deleted_at IS NULL
		FOR UPDATE
	`, shot.ID, input.ProjectID, project.ProductionGenerationID).Scan(&activeGenerationID); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if activeGenerationID != project.ProductionGenerationID {
		return storyboardSheetManifestRuntime{}, ErrWorkflowWriteFenced
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_sheet_manifests
		SET status = 'stale', review_status = 'needs_edit',
		    metadata = metadata || jsonb_build_object('staleAt', now(), 'staleReason', 'panel_manifest_recompiled')
		WHERE storyboard_shot_id = $1 AND status IN ('draft', 'processing', 'ready')
	`, shot.ID); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_sheet_panels
		SET status = 'stale', review_status = 'needs_edit', updated_at = now()
		WHERE storyboard_shot_id = $1 AND status NOT IN ('stale', 'archived')
	`, shot.ID); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET status = 'stale', review_status = 'needs_edit', updated_at = now(),
		    metadata = metadata || jsonb_build_object('staleAt', now(), 'staleReason', 'panel_manifest_recompiled')
		WHERE storyboard_shot_id = $1 AND anchor_role = 'storyboard_panel' AND status <> 'archived'
	`, shot.ID); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	var revision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM storyboard_sheet_manifests WHERE storyboard_shot_id = $1`, shot.ID).Scan(&revision); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	var runtime storyboardSheetManifestRuntime
	runtime.Revision = revision
	runtime.ProductionGenerationID = project.ProductionGenerationID
	runtime.Status = "draft"
	runtime.ReviewStatus = "pending"
	runtime.Manifest = manifest
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_sheet_manifests(
			organization_id, project_id, production_generation_id, storyboard_shot_id,
			sheet_anchor_id, revision, contract_version, planned_duration_ticks,
			timeline_timebase, video_aspect_ratio, sheet_aspect_ratio,
			grid_rows, grid_columns, panel_count, entry_state_hash, exit_state_hash,
			manifest, manifest_hash, status, review_status, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
		        $15, $16, $17, $18, 'draft', 'pending', $19, NULLIF($20, '')::uuid)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, project.ProductionGenerationID, shot.ID,
		contract.VisualAnchorID, revision, manifest.ContractVersion, manifest.PlannedDurationTicks,
		manifest.TimelineTimebase, manifest.VideoAspectRatio, manifest.SheetAspectRatio,
		manifest.Rows, manifest.Columns, manifest.PanelCount, manifest.EntryStateHash, manifest.ExitStateHash,
		mustJSON(manifest), manifest.ManifestHash, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "profileKey": project.VideoProductionProfileKey,
			"profileVersionId":    project.VideoProductionProfileVersionID,
			"profileSnapshotHash": project.VideoProductionProfileHash,
		}), input.CreatedBy).Scan(&runtime.ID); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	var nextPanelAnchorRevision int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(revision), 0) + 1
		FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1 AND anchor_role = 'storyboard_panel'
	`, shot.ID).Scan(&nextPanelAnchorRevision); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	for _, panel := range manifest.Panels {
		stateVersionID := contract.EntryStateVersionID
		if panel.Stage == "exit" {
			stateVersionID = contract.ExitStateVersionID
		}
		stateHash, hashErr := videoproduction.HashShotState(panel.ExpectedState)
		if hashErr != nil {
			return storyboardSheetManifestRuntime{}, hashErr
		}
		var anchorID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, 'storyboard_panel', $6, 'draft', 'pending', $7)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, project.ProductionGenerationID, shot.ID,
			stateVersionID, nextPanelAnchorRevision+panel.Ordinal-1, mustJSON(map[string]any{
				"workflowRunId": input.WorkflowRunID, "source": "panel_manifest",
				"panelManifestId": runtime.ID, "panelManifestHash": manifest.ManifestHash,
				"panelOrdinal": panel.Ordinal, "gridRow": panel.GridRow, "gridColumn": panel.GridColumn,
				"timeTick": panel.TimeTick, "normalizedPosition": panel.NormalizedPosition,
				"stage": panel.Stage, "actionStage": panel.ActionStage,
			})).Scan(&anchorID); err != nil {
			return storyboardSheetManifestRuntime{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO storyboard_sheet_panels(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				manifest_id, visual_anchor_id, ordinal, grid_row, grid_column,
				time_tick, normalized_position, stage, action_stage,
				expected_state, expected_state_hash, status, review_status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			        $14, $15, 'planned', 'pending', $16)
		`, input.OrganizationID, input.ProjectID, project.ProductionGenerationID, shot.ID,
			runtime.ID, anchorID, panel.Ordinal, panel.GridRow, panel.GridColumn,
			panel.TimeTick, panel.NormalizedPosition, panel.Stage, panel.ActionStage,
			mustJSON(panel.ExpectedState), stateHash, mustJSON(map[string]any{
				"manifestHash": manifest.ManifestHash, "contractVersion": manifest.ContractVersion,
			})); err != nil {
			return storyboardSheetManifestRuntime{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET metadata = metadata || jsonb_build_object(
		      'panelManifestId', $2::text, 'panelManifestHash', $3::text,
		      'panelCount', $4::integer, 'sheetAspectRatio', $5::text
		    ), updated_at = now()
		WHERE id = $1
	`, contract.VisualAnchorID, runtime.ID, manifest.ManifestHash, manifest.PanelCount, manifest.SheetAspectRatio); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	payload := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "storyboardShotId": shot.ID,
		"panelManifestId": runtime.ID, "panelManifestHash": manifest.ManifestHash,
		"panelCount": manifest.PanelCount, "productionGenerationId": project.ProductionGenerationID,
		"bindingId": project.VideoProductionBindingID, "bindingRevision": project.VideoProductionBindingRevision,
		"episodeId": shot.ScriptEpisodeID,
	})
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID,
		"storyboard.shot.panel_manifest.compiled", "storyboard_sheet_manifest", runtime.ID, payload); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if applied, err := completeNodeRunTx(ctx, tx, execution, mustJSON(runtime)); err != nil {
		return storyboardSheetManifestRuntime{}, err
	} else if !applied {
		return storyboardSheetManifestRuntime{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	return runtime, nil
}

func (a Activities) findStoryboardSheetManifest(ctx context.Context, shotID, manifestHash string, requireApproved bool) (storyboardSheetManifestRuntime, bool, error) {
	return findStoryboardSheetManifestQuery(ctx, a.db, shotID, manifestHash, requireApproved, false)
}

func findStoryboardSheetManifestTx(ctx context.Context, tx pgx.Tx, shotID, manifestHash string, lock bool) (storyboardSheetManifestRuntime, bool, error) {
	return findStoryboardSheetManifestQuery(ctx, tx, shotID, manifestHash, false, lock)
}

type storyboardSheetManifestQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findStoryboardSheetManifestQuery(ctx context.Context, db storyboardSheetManifestQuerier, shotID, manifestHash string, requireApproved, lock bool) (storyboardSheetManifestRuntime, bool, error) {
	query := `
		SELECT id::text, production_generation_id::text, revision, status, review_status, manifest
		FROM storyboard_sheet_manifests
		WHERE storyboard_shot_id = $1 AND status IN ('draft', 'processing', 'ready')`
	args := []any{shotID}
	if strings.TrimSpace(manifestHash) != "" {
		query += ` AND manifest_hash = $2`
		args = append(args, strings.TrimPrefix(strings.ToLower(strings.TrimSpace(manifestHash)), "sha256:"))
	}
	if requireApproved {
		query += ` AND status = 'ready' AND review_status = 'approved'`
	}
	query += ` ORDER BY revision DESC LIMIT 1`
	if lock {
		query += ` FOR UPDATE`
	}
	var runtime storyboardSheetManifestRuntime
	var raw []byte
	err := db.QueryRow(ctx, query, args...).Scan(
		&runtime.ID, &runtime.ProductionGenerationID, &runtime.Revision,
		&runtime.Status, &runtime.ReviewStatus, &raw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storyboardSheetManifestRuntime{}, false, nil
	}
	if err != nil {
		return storyboardSheetManifestRuntime{}, false, err
	}
	if err := json.Unmarshal(raw, &runtime.Manifest); err != nil {
		return storyboardSheetManifestRuntime{}, false, fmt.Errorf("decode PanelManifest: %w", err)
	}
	if err := videoproduction.ValidatePanelManifest(runtime.Manifest); err != nil {
		return storyboardSheetManifestRuntime{}, false, err
	}
	return runtime, true, nil
}

func (a Activities) loadApprovedStoryboardSheetManifest(ctx context.Context, shotID string) (storyboardSheetManifestRuntime, error) {
	runtime, ok, err := a.findStoryboardSheetManifest(ctx, shotID, "", true)
	if err != nil {
		return storyboardSheetManifestRuntime{}, err
	}
	if !ok {
		return storyboardSheetManifestRuntime{}, workflowError{
			Code: videoproduction.CodeReferencePackIncomplete, Message: "当前镜头没有已审核通过的分镜板 PanelManifest",
		}
	}
	return runtime, nil
}
