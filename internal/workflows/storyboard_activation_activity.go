package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
)

const nodeActivateStoryboardPlanPrefix = "storyboard_plan_activate"

type ActivateStoryboardPlanActivityInput struct {
	OrganizationID   string `json:"organizationId"`
	ProjectID        string `json:"projectId"`
	WorkflowRunID    string `json:"workflowRunId"`
	CreatedBy        string `json:"createdBy"`
	ScriptID         string `json:"scriptId"`
	ScriptVersionID  string `json:"scriptVersionId"`
	ScriptEpisodeID  string `json:"scriptEpisodeId"`
	EpisodeIndex     int    `json:"episodeIndex"`
	EpisodeTotal     int    `json:"episodeTotal"`
	EpisodeTitle     string `json:"episodeTitle"`
	StoryboardPlanID string `json:"storyboardPlanId"`
}

func (a Activities) ActivateStoryboardPlan(ctx context.Context, input ActivateStoryboardPlanActivityInput) (_ ScriptStoryboardOutput, err error) {
	var nodeRunID NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, nodeRunID, err)
	}()
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: input.StoryboardPlanID, CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.StoryboardPlanID) == "" || strings.TrimSpace(input.ScriptEpisodeID) == "" {
		return ScriptStoryboardOutput{}, fmt.Errorf("storyboardPlanId and scriptEpisodeId are required")
	}
	nodeKey := nodeKeyForID(nodeActivateStoryboardPlanPrefix, input.StoryboardPlanID)
	if existing, ok, err := a.existingActivatedStoryboardOutput(ctx, input.WorkflowRunID, nodeKey); err != nil {
		return ScriptStoryboardOutput{}, err
	} else if ok {
		return existing, nil
	}
	shots, err := a.listStoryboardPlanShots(ctx, input.StoryboardPlanID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, NodeExecution{}, err)
	}
	if len(shots) == 0 {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, NodeExecution{}, fmt.Errorf("storyboard plan has no shots"))
	}
	requirements, err := a.listStoryboardPlanRequirements(ctx, input.StoryboardPlanID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, NodeExecution{}, err)
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, NodeExecution{}, err)
	}
	payload := map[string]any{
		"storyboardPlanId": input.StoryboardPlanID,
		"scriptId":         input.ScriptID,
		"scriptVersionId":  input.ScriptVersionID,
		"scriptEpisodeId":  input.ScriptEpisodeID,
		"episodeIndex":     input.EpisodeIndex,
		"episodeTitle":     input.EpisodeTitle,
		"timelineTimebase": project.TimelineTimebase,
		"fpsNumerator":     project.FPSNumerator,
		"fpsDenominator":   project.FPSDenominator,
		"shots":            shots,
	}
	storageKey := fmt.Sprintf("org/%s/project/%s/workflow/%s/storyboard/episode-%04d-plan-%s.json",
		input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.EpisodeIndex, input.StoryboardPlanID)
	put, err := a.storage.PutJSON(ctx, storageKey, payload)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, NodeExecution{}, err)
	}
	nodeRunID, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "storyboard.plan_activate",
		Input: mustJSON(map[string]any{
			"storyboardPlanId": input.StoryboardPlanID,
			"scriptEpisodeId":  input.ScriptEpisodeID,
		}),
	})
	if err != nil {
		return ScriptStoryboardOutput{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, nodeRunID, err)
	}
	defer tx.Rollback(ctx)
	failAfterRollback := func(cause error) error {
		_ = tx.Rollback(ctx)
		return a.failStoryboardActivation(ctx, baseInput, nodeRunID, cause)
	}
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeRunID); err != nil {
		return ScriptStoryboardOutput{}, failAfterRollback(err)
	}
	activation, err := storyboardpkg.ActivateStoryboardPlanTx(ctx, tx, storyboardpkg.ActivateStoryboardPlanRequest{
		ProjectID:        input.ProjectID,
		ScriptEpisodeID:  input.ScriptEpisodeID,
		StoryboardPlanID: input.StoryboardPlanID,
		ActorID:          input.CreatedBy,
	})
	if err != nil {
		return ScriptStoryboardOutput{}, failAfterRollback(err)
	}
	var artifactID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM artifacts
		WHERE project_id = $1
		  AND workflow_run_id = $2
		  AND type = 'storyboard_json'
		  AND metadata->>'storyboardPlanId' = $3
		LIMIT 1
	`, input.ProjectID, input.WorkflowRunID, input.StoryboardPlanID).Scan(&artifactID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO artifacts(
				organization_id, project_id, workflow_run_id, node_run_id, type,
				storage_key, mime_type, content_hash, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, 'storyboard_json', $5, 'application/json', $6, $7, NULLIF($8, '')::uuid)
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID.NodeRunID,
			put.StorageKey, put.ContentHash, mustJSON(map[string]any{
				"storyboardPlanId":         input.StoryboardPlanID,
				"scriptEpisodeId":          input.ScriptEpisodeID,
				"episodeIndex":             input.EpisodeIndex,
				"shotCount":                len(shots),
				"targetDurationTicks":      activation.TargetDurationTicks,
				"previousStoryboardPlanId": activation.PreviousStoryboardPlanID,
				"timelineTimebase":         project.TimelineTimebase,
				"fpsNumerator":             project.FPSNumerator,
				"fpsDenominator":           project.FPSDenominator,
			}), input.CreatedBy).Scan(&artifactID)
	}
	if err != nil {
		return ScriptStoryboardOutput{}, failAfterRollback(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET metadata = metadata || jsonb_build_object('storyboardArtifactId', $2::text, 'storageKey', $3::text)
		WHERE id = $1
	`, input.StoryboardPlanID, artifactID, put.StorageKey); err != nil {
		return ScriptStoryboardOutput{}, failAfterRollback(err)
	}
	durationMetrics := storyboardPlanDurationMetrics(shots)
	storyboardJSON := mustJSON(payload)
	output := ScriptStoryboardOutput{
		ScriptID:             input.ScriptID,
		ScriptVersionID:      input.ScriptVersionID,
		ScriptEpisodeID:      input.ScriptEpisodeID,
		EpisodeIndex:         input.EpisodeIndex,
		EpisodeTotal:         input.EpisodeTotal,
		EpisodeTitle:         input.EpisodeTitle,
		EpisodeCount:         1,
		StoryboardArtifactID: artifactID,
		StorageKey:           put.StorageKey,
		Storyboard:           storyboardJSON,
		Shots:                shots,
		Requirements:         requirements,
		DurationMetrics:      durationMetrics,
	}
	if applied, err := completeNodeRunTx(ctx, tx, nodeRunID, mustJSON(output)); err != nil {
		return ScriptStoryboardOutput{}, failAfterRollback(err)
	} else if !applied {
		return ScriptStoryboardOutput{}, failAfterRollback(ErrWorkflowWriteFenced)
	}
	if err := tx.Commit(ctx); err != nil {
		return ScriptStoryboardOutput{}, a.failStoryboardActivation(ctx, baseInput, nodeRunID, err)
	}
	return output, nil
}

func (a Activities) failStoryboardActivation(ctx context.Context, input TextToStoryboardInput, execution NodeExecution, cause error) error {
	if isWorkflowWriteFenced(cause) {
		return discardWorkflowResult(ctx, a.db, execution, cause.Error())
	}
	code, message := workflowErrorFields(cause, codeActivityFailed)
	if execution.valid() {
		persistCtx, cancel := workflowFailurePersistenceContext(ctx)
		defer cancel()
		tx, err := a.db.Begin(persistCtx)
		if err == nil {
			defer tx.Rollback(persistCtx)
			if _, err = lockNodeBusinessWrite(persistCtx, tx, input.WorkflowRunID, execution); err == nil {
				_, err = failNodeRunTx(persistCtx, tx, execution, code, message, mustJSON(map[string]any{
					"storyboardPlanId": input.Prompt,
					"code":             code,
					"message":          message,
				}))
			}
			if err == nil {
				_ = tx.Commit(persistCtx)
			}
		}
	}
	return newWorkflowApplicationError(cause, code, message)
}

func (a Activities) listStoryboardPlanRequirements(ctx context.Context, planID string) ([]ShotAssetRequirementRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT requirement.id::text, requirement.storyboard_shot_id::text,
		       requirement.asset_id::text, asset.asset_type, asset.name,
		       requirement.requirement_type, COALESCE(requirement.role_in_shot, ''),
		       COALESCE(requirement.costume, ''), COALESCE(requirement.pose, ''),
		       COALESCE(requirement.expression, ''), COALESCE(requirement.action, ''),
		       COALESCE(requirement.camera_relation, ''), COALESCE(requirement.scene_state, ''),
		       COALESCE(requirement.prop_state, ''), COALESCE(requirement.prompt, ''),
		       COALESCE(requirement.derived_artifact_id::text, ''),
		       COALESCE(requirement.derived_media_file_id::text, ''),
		       COALESCE(requirement.derived_storage_key, ''), requirement.status,
		       COALESCE(requirement.manual_override, false), COALESCE(requirement.stale_state, 'fresh')
		FROM shot_asset_requirements requirement
		JOIN storyboard_shots shot ON shot.id = requirement.storyboard_shot_id
		JOIN canonical_assets asset ON asset.id = requirement.asset_id
		WHERE shot.storyboard_plan_id = $1 AND shot.deleted_at IS NULL
		ORDER BY shot.shot_index, requirement.created_at, requirement.id
	`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ShotAssetRequirementRecord, 0)
	for rows.Next() {
		var item ShotAssetRequirementRecord
		if err := rows.Scan(
			&item.ID, &item.StoryboardShotID, &item.AssetID, &item.AssetType, &item.AssetName,
			&item.RequirementType, &item.RoleInShot, &item.Costume, &item.Pose,
			&item.Expression, &item.Action, &item.CameraRelation, &item.SceneState,
			&item.PropState, &item.Prompt, &item.DerivedArtifactID, &item.DerivedMediaFileID,
			&item.DerivedStorageKey, &item.Status, &item.ManualOverride, &item.StaleState,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a Activities) listStoryboardPlanShots(ctx context.Context, planID string) ([]StoryboardShotRecord, error) {
	rows, err := a.db.Query(ctx, storyboardShotRecordSelectSQL(`
		s.storyboard_plan_id = $1
		  AND s.deleted_at IS NULL
		ORDER BY s.start_tick, s.shot_index
	`), planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shots := make([]StoryboardShotRecord, 0)
	for rows.Next() {
		shot, err := scanStoryboardShotRecord(rows)
		if err != nil {
			return nil, err
		}
		shots = append(shots, shot)
	}
	return shots, rows.Err()
}

func storyboardPlanDurationMetrics(shots []StoryboardShotRecord) StoryboardDurationMetrics {
	metrics := StoryboardDurationMetrics{
		RawShotCount:     len(shots),
		PlannedShotCount: len(shots),
		StoredShotCount:  len(shots),
	}
	for _, shot := range shots {
		metrics.RawDurationSeconds += shot.Duration
		metrics.PlannedDurationSeconds += shot.Duration
		metrics.StoredDurationSeconds += shot.Duration
	}
	return metrics
}

func (a Activities) existingActivatedStoryboardOutput(ctx context.Context, workflowRunID, nodeKey string) (ScriptStoryboardOutput, bool, error) {
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT output FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = $2 AND status = 'succeeded'
	`, workflowRunID, nodeKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return ScriptStoryboardOutput{}, false, nil
	}
	if err != nil {
		return ScriptStoryboardOutput{}, false, err
	}
	var output ScriptStoryboardOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return ScriptStoryboardOutput{}, false, err
	}
	return output, output.StoryboardArtifactID != "", nil
}
