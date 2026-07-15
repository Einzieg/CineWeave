package storyboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/jackc/pgx/v5"
)

type StoryboardPlanEditResult struct {
	StoryboardPlanID       string                         `json:"storyboardPlanId"`
	SourceStoryboardPlanID string                         `json:"sourceStoryboardPlanId"`
	ScriptEpisodeID        string                         `json:"scriptEpisodeId"`
	Revision               int                            `json:"revision"`
	ShotIDs                []string                       `json:"shotIds"`
	Validation             StoryboardPlanValidationReport `json:"validation"`
}

type SplitStoryboardShotRequest struct {
	ProjectID  string
	ShotID     string
	SplitTick  int64
	ActorID    string
	RightTitle string
}

type MergeStoryboardShotsRequest struct {
	ProjectID string
	ShotIDs   []string
	ActorID   string
	Title     string
	Visual    string
	Camera    string
	Motion    string
	Mood      string
}

type RetimeStoryboardShotRequest struct {
	ProjectID string
	ShotID    string
	StartTick *int64
	EndTick   *int64
	ActorID   string
}

type storyboardRevisionSource struct {
	OrganizationID   string
	ProjectID        string
	PlanID           string
	ScriptEpisodeID  string
	TimingAnalysisID string
	Revision         int
	TargetTicks      int64
	Timebase         Timebase
}

type storyboardRevisionShot struct {
	ID            string
	PlanID        string
	ShotIndex     int
	StartTick     int64
	EndTick       int64
	ScriptSceneID sql.NullString
}

type storyboardDraftRevision struct {
	Source    storyboardRevisionSource
	PlanID    string
	Revision  int
	ShotByOld map[string]string
}

func SplitStoryboardShotTx(ctx context.Context, tx pgx.Tx, req SplitStoryboardShotRequest) (StoryboardPlanEditResult, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ShotID = strings.TrimSpace(req.ShotID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.ProjectID == "" || req.ShotID == "" || req.SplitTick <= 0 {
		return StoryboardPlanEditResult{}, fmt.Errorf("projectId, shotId, and splitTick are required")
	}
	shot, source, err := loadEditableStoryboardShot(ctx, tx, req.ProjectID, req.ShotID)
	if err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if req.SplitTick <= shot.StartTick || req.SplitTick >= shot.EndTick {
		return StoryboardPlanEditResult{}, fmt.Errorf("splitTick must lie inside the shot interval")
	}
	if !source.Timebase.IsFrameAligned(req.SplitTick) {
		return StoryboardPlanEditResult{}, fmt.Errorf("splitTick must be aligned to a project frame")
	}
	draft, err := deriveStoryboardPlanRevision(ctx, tx, source, req.ActorID, "split", map[string]any{
		"sourceShotId": req.ShotID, "splitTick": req.SplitTick,
	})
	if err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("derive split storyboard plan revision: %w", err)
	}
	leftID := draft.ShotByOld[req.ShotID]
	leftDuration := req.SplitTick - shot.StartTick
	rightDuration := shot.EndTick - req.SplitTick
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET end_tick = $2::bigint, duration_min_ticks = $3::bigint, duration_max_ticks = $3::bigint,
		    duration_source = 'manual_locked', timing_confidence = 1,
		    duration_locked = true, timing_revision = timing_revision + 1,
		    manual_override = true, edited_by = NULLIF($4, '')::uuid, edited_at = now(),
		    metadata = metadata || jsonb_build_object('editType', 'split', 'splitPart', 'left', 'splitTick', $2::bigint)
		WHERE id = $1
	`, leftID, req.SplitTick, leftDuration, req.ActorID); err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("update split left shot: %w", err)
	}
	var rightID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, storyboard_id, workflow_run_id, script_id, script_version_id,
			script_scene_id, script_episode_id, episode_index, episode_shot_index, storyboard_plan_id,
			shot_index, shot_no, title, visual, camera, motion, mood, shot_size, camera_move,
			action, dialogue, asset_bindings, image_prompt, video_prompt, script_dialogue,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source,
			timing_confidence, duration_locked, shot_group_id, continuity_group_id, one_take,
			timing_revision, image_reference_mode, image_reference_keys, video_reference_mode,
			video_reference_keys, status, review_status, manual_override, stale_state,
			edited_by, edited_at, metadata
		)
		SELECT organization_id, project_id, storyboard_id, workflow_run_id, script_id, script_version_id,
		       script_scene_id, script_episode_id, episode_index, episode_shot_index, storyboard_plan_id,
		       (SELECT COALESCE(MAX(shot_index), 0) + 1 FROM storyboard_shots WHERE storyboard_plan_id = $2::uuid),
		       shot_no + 1,
		       COALESCE(NULLIF($5::text, ''), CASE WHEN COALESCE(title, '') = '' THEN '' ELSE title || '（续）' END),
		       visual, camera, motion, mood, shot_size, camera_move, action, dialogue, asset_bindings,
		       image_prompt, video_prompt, script_dialogue, $3::bigint, $7::bigint, $4::bigint, $4::bigint,
		       'manual_locked', 1, true, shot_group_id, continuity_group_id, one_take,
		       timing_revision + 1, image_reference_mode, image_reference_keys,
		       video_reference_mode, video_reference_keys, 'storyboard_ready', 'pending', true,
		       'fresh', NULLIF($6::text, '')::uuid, now(),
		       metadata || jsonb_build_object('editType', 'split', 'splitPart', 'right', 'splitTick', $3::bigint)
		FROM storyboard_shots
		WHERE id = $1::uuid
		RETURNING id::text
	`, leftID, draft.PlanID, req.SplitTick, rightDuration, strings.TrimSpace(req.RightTitle), req.ActorID, shot.EndTick).Scan(&rightID); err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("insert split right shot: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, role_in_shot, costume, pose, expression, action,
			camera_relation, scene_state, prop_state, prompt, status, metadata,
			manual_override, stale_state, edited_by, edited_at
		)
		SELECT organization_id, project_id, workflow_run_id, $2, asset_id,
		       requirement_type, role_in_shot, costume, pose, expression, action,
		       camera_relation, scene_state, prop_state, prompt, 'pending',
		       metadata || jsonb_build_object('splitFromRequirementId', id::text),
		       manual_override, 'upstream_changed', NULLIF($3, '')::uuid, now()
		FROM shot_asset_requirements
		WHERE storyboard_shot_id = $1
	`, leftID, rightID, req.ActorID); err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("clone split right requirements: %w", err)
	}
	result, err := finalizeStoryboardPlanRevision(ctx, tx, draft, req.ActorID, []string{leftID, rightID})
	if err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("finalize split storyboard plan revision: %w", err)
	}
	return result, nil
}

func MergeStoryboardShotsTx(ctx context.Context, tx pgx.Tx, req MergeStoryboardShotsRequest) (StoryboardPlanEditResult, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.ProjectID == "" || len(req.ShotIDs) != 2 || strings.TrimSpace(req.ShotIDs[0]) == "" || strings.TrimSpace(req.ShotIDs[1]) == "" {
		return StoryboardPlanEditResult{}, fmt.Errorf("projectId and exactly two shotIds are required")
	}
	first, source, err := loadEditableStoryboardShot(ctx, tx, req.ProjectID, req.ShotIDs[0])
	if err != nil {
		return StoryboardPlanEditResult{}, err
	}
	second, secondSource, err := loadEditableStoryboardShot(ctx, tx, req.ProjectID, req.ShotIDs[1])
	if err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if first.ShotIndex > second.ShotIndex {
		first, second = second, first
	}
	if source.PlanID != secondSource.PlanID || second.ShotIndex != first.ShotIndex+1 || first.EndTick != second.StartTick {
		return StoryboardPlanEditResult{}, fmt.Errorf("shots must be adjacent, continuous, and belong to the same active plan")
	}
	if !sameNullableString(first.ScriptSceneID, second.ScriptSceneID) {
		return StoryboardPlanEditResult{}, fmt.Errorf("shots from different scenes cannot be merged")
	}
	draft, err := deriveStoryboardPlanRevision(ctx, tx, source, req.ActorID, "merge", map[string]any{"sourceShotIds": []string{first.ID, second.ID}})
	if err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("derive merge storyboard plan revision: %w", err)
	}
	leftID := draft.ShotByOld[first.ID]
	rightID := draft.ShotByOld[second.ID]
	duration := second.EndTick - first.StartTick
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots target
		SET end_tick = $2, duration_min_ticks = $3, duration_max_ticks = $3,
		    duration_source = 'manual_locked', timing_confidence = 1,
		    duration_locked = true, timing_revision = target.timing_revision + 1,
		    title = COALESCE(NULLIF($4, ''), target.title),
		    visual = COALESCE(NULLIF($5, ''), concat_ws(E'\n', target.visual, source.visual)),
		    camera = COALESCE(NULLIF($6, ''), target.camera),
		    motion = COALESCE(NULLIF($7, ''), target.motion),
		    mood = COALESCE(NULLIF($8, ''), target.mood),
		    manual_override = true, edited_by = NULLIF($9, '')::uuid, edited_at = now(),
		    metadata = target.metadata || jsonb_build_object('editType', 'merge', 'mergedSourceShotId', $10::text)
		FROM storyboard_shots source
		WHERE target.id = $1 AND source.id = $11
	`, leftID, second.EndTick, duration, strings.TrimSpace(req.Title), strings.TrimSpace(req.Visual),
		strings.TrimSpace(req.Camera), strings.TrimSpace(req.Motion), strings.TrimSpace(req.Mood),
		req.ActorID, second.ID, rightID); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM shot_asset_requirements duplicate
		WHERE duplicate.storyboard_shot_id = $2
		  AND EXISTS (
		    SELECT 1 FROM shot_asset_requirements kept
		    WHERE kept.storyboard_shot_id = $1
		      AND kept.asset_id = duplicate.asset_id
		      AND kept.requirement_type = duplicate.requirement_type
		  )
	`, leftID, rightID); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE shot_asset_requirements SET storyboard_shot_id = $1, stale_state = 'upstream_changed' WHERE storyboard_shot_id = $2`, leftID, rightID); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM storyboard_shots WHERE id = $1`, rightID); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	result, err := finalizeStoryboardPlanRevision(ctx, tx, draft, req.ActorID, []string{leftID})
	if err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("finalize merge storyboard plan revision: %w", err)
	}
	return result, nil
}

func RetimeStoryboardShotTx(ctx context.Context, tx pgx.Tx, req RetimeStoryboardShotRequest) (StoryboardPlanEditResult, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ShotID = strings.TrimSpace(req.ShotID)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.ProjectID == "" || req.ShotID == "" || (req.StartTick == nil && req.EndTick == nil) {
		return StoryboardPlanEditResult{}, fmt.Errorf("projectId, shotId, and a timing boundary are required")
	}
	shot, source, err := loadEditableStoryboardShot(ctx, tx, req.ProjectID, req.ShotID)
	if err != nil {
		return StoryboardPlanEditResult{}, err
	}
	newStart, newEnd := shot.StartTick, shot.EndTick
	if req.StartTick != nil {
		newStart = *req.StartTick
	}
	if req.EndTick != nil {
		newEnd = *req.EndTick
	}
	if newStart < 0 || newEnd <= newStart || !source.Timebase.IsFrameAligned(newStart) || !source.Timebase.IsFrameAligned(newEnd) {
		return StoryboardPlanEditResult{}, fmt.Errorf("shot timing must be a positive frame-aligned interval")
	}
	var previous, next *storyboardRevisionShot
	if newStart != shot.StartTick {
		value, err := loadAdjacentStoryboardShot(ctx, tx, source.PlanID, shot.ShotIndex-1)
		if err != nil {
			return StoryboardPlanEditResult{}, fmt.Errorf("cannot move the first shot start boundary: %w", err)
		}
		if !sameNullableString(shot.ScriptSceneID, value.ScriptSceneID) || newStart <= value.StartTick {
			return StoryboardPlanEditResult{}, fmt.Errorf("start boundary must remain inside the same scene and after the previous shot start")
		}
		previous = &value
	}
	if newEnd != shot.EndTick {
		value, err := loadAdjacentStoryboardShot(ctx, tx, source.PlanID, shot.ShotIndex+1)
		if err != nil {
			return StoryboardPlanEditResult{}, fmt.Errorf("cannot move the final shot end boundary: %w", err)
		}
		if !sameNullableString(shot.ScriptSceneID, value.ScriptSceneID) || newEnd >= value.EndTick {
			return StoryboardPlanEditResult{}, fmt.Errorf("end boundary must remain inside the same scene and before the next shot end")
		}
		next = &value
	}
	draft, err := deriveStoryboardPlanRevision(ctx, tx, source, req.ActorID, "timing", map[string]any{
		"sourceShotId": req.ShotID, "startTick": newStart, "endTick": newEnd,
	})
	if err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("derive timing storyboard plan revision: %w", err)
	}
	affected := make([]string, 0, 3)
	if previous != nil {
		previousID := draft.ShotByOld[previous.ID]
		if err := updateStoryboardShotTiming(ctx, tx, previousID, previous.StartTick, newStart, req.ActorID); err != nil {
			return StoryboardPlanEditResult{}, err
		}
		affected = append(affected, previousID)
	}
	targetID := draft.ShotByOld[shot.ID]
	if err := updateStoryboardShotTiming(ctx, tx, targetID, newStart, newEnd, req.ActorID); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	affected = append(affected, targetID)
	if next != nil {
		nextID := draft.ShotByOld[next.ID]
		if err := updateStoryboardShotTiming(ctx, tx, nextID, newEnd, next.EndTick, req.ActorID); err != nil {
			return StoryboardPlanEditResult{}, err
		}
		affected = append(affected, nextID)
	}
	result, err := finalizeStoryboardPlanRevision(ctx, tx, draft, req.ActorID, affected)
	if err != nil {
		return StoryboardPlanEditResult{}, fmt.Errorf("finalize timing storyboard plan revision: %w", err)
	}
	return result, nil
}

func loadEditableStoryboardShot(ctx context.Context, tx pgx.Tx, projectID, shotID string) (storyboardRevisionShot, storyboardRevisionSource, error) {
	var shot storyboardRevisionShot
	var source storyboardRevisionSource
	var fpsNumerator, fpsDenominator int64
	err := tx.QueryRow(ctx, `
		SELECT shot.id::text, shot.storyboard_plan_id::text, shot.shot_index,
		       shot.start_tick, shot.end_tick, shot.script_scene_id::text,
		       plan.organization_id::text, plan.script_episode_id::text,
		       plan.timing_analysis_id::text, plan.revision, plan.target_duration_ticks,
		       analysis.timeline_timebase, analysis.fps_numerator, analysis.fps_denominator
		FROM storyboard_shots shot
		JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
		JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		WHERE shot.project_id = $1 AND shot.id = $2 AND shot.deleted_at IS NULL
		  AND plan.active = true AND plan.status = 'ready'
		FOR UPDATE OF shot, plan
	`, projectID, shotID).Scan(
		&shot.ID, &shot.PlanID, &shot.ShotIndex, &shot.StartTick, &shot.EndTick, &shot.ScriptSceneID,
		&source.OrganizationID, &source.ScriptEpisodeID, &source.TimingAnalysisID,
		&source.Revision, &source.TargetTicks, &source.Timebase.TicksPerSecond,
		&fpsNumerator, &fpsDenominator,
	)
	if err != nil {
		return storyboardRevisionShot{}, storyboardRevisionSource{}, err
	}
	source.PlanID = shot.PlanID
	source.ProjectID = projectID
	source.Timebase.FPSNumerator = fpsNumerator
	source.Timebase.FPSDenominator = fpsDenominator
	if err := source.Timebase.Validate(); err != nil {
		return storyboardRevisionShot{}, storyboardRevisionSource{}, err
	}
	return shot, source, nil
}

func loadAdjacentStoryboardShot(ctx context.Context, tx pgx.Tx, planID string, shotIndex int) (storyboardRevisionShot, error) {
	var shot storyboardRevisionShot
	err := tx.QueryRow(ctx, `
		SELECT id::text, storyboard_plan_id::text, shot_index, start_tick, end_tick, script_scene_id::text
		FROM storyboard_shots
		WHERE storyboard_plan_id = $1 AND shot_index = $2 AND deleted_at IS NULL
	`, planID, shotIndex).Scan(&shot.ID, &shot.PlanID, &shot.ShotIndex, &shot.StartTick, &shot.EndTick, &shot.ScriptSceneID)
	return shot, err
}

func deriveStoryboardPlanRevision(ctx context.Context, tx pgx.Tx, source storyboardRevisionSource, actorID, editType string, editMetadata map[string]any) (storyboardDraftRevision, error) {
	var lockedEpisode string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM script_episodes WHERE id = $1 FOR UPDATE`, source.ScriptEpisodeID).Scan(&lockedEpisode); err != nil {
		return storyboardDraftRevision{}, fmt.Errorf("lock script episode: %w", err)
	}
	var revision int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM storyboard_plans WHERE script_episode_id = $1`, source.ScriptEpisodeID).Scan(&revision); err != nil {
		return storyboardDraftRevision{}, fmt.Errorf("allocate storyboard plan revision: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"sourceStoryboardPlanId": source.PlanID,
		"editType":               editType,
		"edit":                   editMetadata,
	})
	if err != nil {
		return storyboardDraftRevision{}, err
	}
	var planID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, pacing_profile, target_duration_ticks,
			estimated_shot_count, actual_shot_count, active, stale_state, metadata, created_by
		)
		SELECT organization_id, project_id, script_id, script_version_id, script_episode_id,
		       timing_analysis_id, $2, 'planning', pacing_profile, target_duration_ticks,
		       estimated_shot_count, actual_shot_count, false, 'fresh',
		       metadata || $3::jsonb, NULLIF($4, '')::uuid
		FROM storyboard_plans
		WHERE id = $1 AND active = true AND status = 'ready'
		RETURNING id::text
	`, source.PlanID, revision, metadata, actorID).Scan(&planID); err != nil {
		return storyboardDraftRevision{}, fmt.Errorf("insert storyboard plan revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO storyboard_scene_plans(
			organization_id, project_id, storyboard_plan_id, blueprint_id, script_scene_id,
			scene_key, scene_ordinal, dependency_group, status, retry_generation,
			start_tick, end_tick, shot_count, planner_input, planner_output, reviewer_output,
			entry_state, exit_state, prompt_version_id, prompt_hash, provider_call_id, model_id,
			metadata, created_by, completed_at
		)
		SELECT organization_id, project_id, $2, blueprint_id, script_scene_id,
		       scene_key, scene_ordinal, dependency_group, 'ready', retry_generation,
		       start_tick, end_tick, shot_count, planner_input, planner_output, reviewer_output,
		       entry_state, exit_state, prompt_version_id, prompt_hash, provider_call_id, model_id,
		       metadata || jsonb_build_object('sourceScenePlanId', id::text, 'editType', $3::text),
		       NULLIF($4, '')::uuid, now()
		FROM storyboard_scene_plans
		WHERE storyboard_plan_id = $1
	`, source.PlanID, planID, editType, actorID); err != nil {
		return storyboardDraftRevision{}, fmt.Errorf("clone storyboard scene plans: %w", err)
	}
	rows, err := tx.Query(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, storyboard_id, workflow_run_id, script_id, script_version_id,
			script_scene_id, script_episode_id, episode_index, episode_shot_index, storyboard_plan_id,
			shot_index, shot_no, title, visual, camera, motion, mood, shot_size, camera_move,
			action, dialogue, asset_bindings, image_prompt, video_prompt, script_dialogue,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source,
			timing_confidence, duration_locked, shot_group_id, continuity_group_id, one_take,
			timing_revision, image_reference_mode, image_reference_keys, video_reference_mode,
			video_reference_keys, status, review_status, manual_override, stale_state,
			edited_by, edited_at, metadata
		)
		SELECT organization_id, project_id, storyboard_id, workflow_run_id, script_id, script_version_id,
		       script_scene_id, script_episode_id, episode_index, episode_shot_index, $2,
		       shot_index, shot_no, title, visual, camera, motion, mood, shot_size, camera_move,
		       action, dialogue, asset_bindings, image_prompt, video_prompt, script_dialogue,
		       start_tick, end_tick, duration_min_ticks, duration_max_ticks, duration_source,
		       timing_confidence, duration_locked, shot_group_id, continuity_group_id, one_take,
		       timing_revision, image_reference_mode, image_reference_keys, video_reference_mode,
		       video_reference_keys, 'storyboard_ready', 'pending', manual_override, 'fresh',
		       NULLIF($3, '')::uuid, now(),
		       metadata || jsonb_build_object('sourceShotId', id::text, 'sourceStoryboardPlanId', $1::uuid::text, 'editType', $4::text)
		FROM storyboard_shots
		WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		ORDER BY shot_index
		RETURNING id::text, metadata->>'sourceShotId'
	`, source.PlanID, planID, actorID, editType)
	if err != nil {
		return storyboardDraftRevision{}, fmt.Errorf("clone storyboard shots: %w", err)
	}
	shotByOld := map[string]string{}
	for rows.Next() {
		var newID, oldID string
		if err := rows.Scan(&newID, &oldID); err != nil {
			rows.Close()
			return storyboardDraftRevision{}, err
		}
		shotByOld[oldID] = newID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return storyboardDraftRevision{}, err
	}
	rows.Close()
	if len(shotByOld) == 0 {
		return storyboardDraftRevision{}, fmt.Errorf("source storyboard plan has no shots")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, role_in_shot, costume, pose, expression, action,
			camera_relation, scene_state, prop_state, prompt, status, metadata,
			manual_override, stale_state, edited_by, edited_at
		)
		SELECT requirement.organization_id, requirement.project_id, requirement.workflow_run_id,
		       target.id, requirement.asset_id, requirement.requirement_type,
		       requirement.role_in_shot, requirement.costume, requirement.pose,
		       requirement.expression, requirement.action, requirement.camera_relation,
		       requirement.scene_state, requirement.prop_state, requirement.prompt, 'pending',
		       requirement.metadata || jsonb_build_object('sourceRequirementId', requirement.id::text, 'sourceShotId', source.id::text),
		       requirement.manual_override, 'upstream_changed', NULLIF($3, '')::uuid, now()
		FROM shot_asset_requirements requirement
		JOIN storyboard_shots source ON source.id = requirement.storyboard_shot_id
		JOIN storyboard_shots target ON target.storyboard_plan_id = $2
		  AND target.metadata->>'sourceShotId' = source.id::text
		WHERE source.storyboard_plan_id = $1
	`, source.PlanID, planID, actorID); err != nil {
		return storyboardDraftRevision{}, fmt.Errorf("clone shot asset requirements: %w", err)
	}
	return storyboardDraftRevision{Source: source, PlanID: planID, Revision: revision, ShotByOld: shotByOld}, nil
}

func finalizeStoryboardPlanRevision(ctx context.Context, tx pgx.Tx, draft storyboardDraftRevision, actorID string, affectedShotIDs []string) (StoryboardPlanEditResult, error) {
	if err := rebuildStoryboardPlanDerivedTiming(ctx, tx, draft.PlanID); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	report, err := ValidateStoryboardPlanTx(ctx, tx, draft.Source.ProjectID, draft.Source.ScriptEpisodeID, draft.PlanID)
	if err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_plans
		SET status = 'ready', actual_shot_count = $2,
		    metadata = metadata || jsonb_build_object('manualEditValidatedAt', now(), 'validation', $3::jsonb)
		WHERE id = $1
	`, draft.PlanID, report.ShotCount, mustStoryboardJSON(report)); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	if err := events.AppendTx(ctx, tx, draft.Source.OrganizationID, draft.Source.ProjectID, "storyboard.plan.revision.ready", "storyboard_plan", draft.PlanID, mustStoryboardJSON(map[string]any{
		"storyboardPlanId":       draft.PlanID,
		"sourceStoryboardPlanId": draft.Source.PlanID,
		"revision":               draft.Revision,
		"shotIds":                affectedShotIDs,
		"editedBy":               actorID,
	})); err != nil {
		return StoryboardPlanEditResult{}, err
	}
	return StoryboardPlanEditResult{
		StoryboardPlanID: draft.PlanID, SourceStoryboardPlanID: draft.Source.PlanID,
		ScriptEpisodeID: draft.Source.ScriptEpisodeID, Revision: draft.Revision,
		ShotIDs: affectedShotIDs, Validation: report,
	}, nil
}

func rebuildStoryboardPlanDerivedTiming(ctx context.Context, tx pgx.Tx, planID string) error {
	if _, err := tx.Exec(ctx, `
		WITH ordered AS (
		  SELECT id, ROW_NUMBER() OVER (ORDER BY start_tick, end_tick, id) - 1 AS ordinal
		  FROM storyboard_shots WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		)
		UPDATE storyboard_shots shot
		SET shot_index = -ordered.ordinal - 1,
		    shot_no = ordered.ordinal + 1,
		    episode_shot_index = ordered.ordinal
		FROM ordered WHERE shot.id = ordered.id
	`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots SET shot_index = -shot_index - 1
		WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
	`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM storyboard_shot_timing_spans WHERE storyboard_plan_id = $1`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		WITH interval_spans AS (
		  SELECT shot.id AS shot_id, unit.id AS unit_id,
		         GREATEST(shot.start_tick, unit.start_tick) AS span_start,
		         LEAST(shot.end_tick, unit.end_tick) AS span_end,
		         ROW_NUMBER() OVER (PARTITION BY shot.id ORDER BY unit.start_tick, unit.unit_ordinal, unit.id) - 1 AS ordinal,
		         unit.metadata->>'sceneKey' AS scene_key
		  FROM storyboard_shots shot
		  JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
		  JOIN script_timing_units unit ON unit.timing_analysis_id = plan.timing_analysis_id
		   AND unit.start_tick < shot.end_tick AND unit.end_tick > shot.start_tick
		  WHERE shot.storyboard_plan_id = $1 AND shot.deleted_at IS NULL
		)
		INSERT INTO storyboard_shot_timing_spans(
			storyboard_plan_id, storyboard_shot_id, timing_unit_id,
			span_start_tick, span_end_tick, ordinal, metadata
		)
		SELECT $1, shot_id, unit_id, span_start, span_end, ordinal,
		       jsonb_build_object('sceneKey', scene_key)
		FROM interval_spans WHERE span_start < span_end
	`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots shot
		SET script_dialogue = COALESCE((
		  SELECT jsonb_agg(jsonb_build_object(
		    'timingUnitId', unit.id::text,
		    'speaker', COALESCE(unit.speaker, ''),
		    'text', unit.source_text,
		    'delivery', COALESCE(unit.delivery, ''),
		    'kind', unit.unit_type,
		    'spanStartTick', span.span_start_tick,
		    'spanEndTick', span.span_end_tick,
		    'sourceStartOffset', unit.source_start_offset,
		    'sourceEndOffset', unit.source_end_offset,
		    'continuesFromPrevious', span.span_start_tick > unit.start_tick,
		    'continuesToNext', span.span_end_tick < unit.end_tick
		  ) ORDER BY unit.unit_ordinal)
		  FROM storyboard_shot_timing_spans span
		  JOIN script_timing_units unit ON unit.id = span.timing_unit_id
		  WHERE span.storyboard_plan_id = $1 AND span.storyboard_shot_id = shot.id
		    AND unit.unit_type IN ('dialogue', 'voiceover', 'narration', 'system')
		), '[]'::jsonb)
		WHERE shot.storyboard_plan_id = $1 AND shot.deleted_at IS NULL
	`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_scene_plans scene
		SET shot_count = (
		  SELECT COUNT(*) FROM storyboard_shots shot
		  WHERE shot.storyboard_plan_id = scene.storyboard_plan_id
		    AND shot.metadata->>'sceneKey' = scene.scene_key
		    AND shot.deleted_at IS NULL
		)
		WHERE scene.storyboard_plan_id = $1
	`, planID); err != nil {
		return err
	}
	return nil
}

func updateStoryboardShotTiming(ctx context.Context, tx pgx.Tx, shotID string, startTick, endTick int64, actorID string) error {
	duration := endTick - startTick
	if duration <= 0 {
		return fmt.Errorf("shot timing must remain positive")
	}
	_, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET start_tick = $2, end_tick = $3,
		    duration_min_ticks = $4, duration_max_ticks = $4,
		    duration_source = 'manual_locked', timing_confidence = 1,
		    duration_locked = true, timing_revision = timing_revision + 1,
		    manual_override = true, edited_by = NULLIF($5, '')::uuid, edited_at = now(),
		    metadata = metadata || jsonb_build_object('editType', 'timing')
		WHERE id = $1
	`, shotID, startTick, endTick, duration, actorID)
	return err
}

func sameNullableString(left, right sql.NullString) bool {
	return left.Valid == right.Valid && (!left.Valid || left.String == right.String)
}

func mustStoryboardJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
