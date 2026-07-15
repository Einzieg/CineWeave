package storyboard

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoryboardPlanRevisionEditsPreserveCoverage(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for storyboard revision integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	organizationID, userID, projectID, scriptID, versionID, episodeID, analysisID := seedStoryboardRevisionFixture(t, ctx, tx)
	var blueprintID, sourcePlanID, scenePlanID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO episode_continuity_blueprints(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, blueprint, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready',
		        '{"scenes":[{"sceneKey":"scene-1","sceneOrdinal":0,"pacingProfile":"standard","suggestedShotMinimum":1,"suggestedShotMaximum":4,"entryState":{},"exitState":{}}],"dependencies":[]}', $7)
		RETURNING id::text
	`, organizationID, projectID, scriptID, versionID, episodeID, analysisID, userID).Scan(&blueprintID); err != nil {
		t.Fatalf("insert blueprint: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, pacing_profile, target_duration_ticks,
			estimated_shot_count, actual_shot_count, active, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', '{"key":"standard"}', 180000, 2, 2, true, $7)
		RETURNING id::text
	`, organizationID, projectID, scriptID, versionID, episodeID, analysisID, userID).Scan(&sourcePlanID); err != nil {
		t.Fatalf("insert source plan: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO storyboard_scene_plans(
			organization_id, project_id, storyboard_plan_id, blueprint_id, scene_key,
			scene_ordinal, status, start_tick, end_tick, shot_count,
			planner_input, planner_output, entry_state, exit_state, created_by, completed_at
		)
		VALUES ($1, $2, $3, $4, 'scene-1', 0, 'ready', 0, 180000, 2,
		        '{}', '{}', '{}', '{}', $5, now())
		RETURNING id::text
	`, organizationID, projectID, sourcePlanID, blueprintID, userID).Scan(&scenePlanID); err != nil {
		t.Fatalf("insert scene plan: %v", err)
	}
	_ = scenePlanID

	unitIDs := make([]string, 2)
	shotIDs := make([]string, 2)
	for index, bounds := range [][2]int64{{0, 90000}, {90000, 180000}} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO script_timing_units(
				timing_analysis_id, unit_ordinal, unit_type, track, source_text,
				start_tick, end_tick, duration_source, metadata
			)
			VALUES ($1, $2, 'action', 'visual', $3, $4, $5, 'rule_estimated', '{"sceneKey":"scene-1"}')
			RETURNING id::text
		`, analysisID, index, "action", bounds[0], bounds[1]).Scan(&unitIDs[index]); err != nil {
			t.Fatalf("insert timing unit %d: %v", index, err)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shots(
				organization_id, project_id, script_id, script_version_id, script_episode_id,
				storyboard_plan_id, shot_index, shot_no, episode_index, episode_shot_index,
				title, visual, start_tick, end_tick, duration_min_ticks, duration_max_ticks,
				status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $7,
			        $9, $10, $11, $12, 90000, 90000, 'storyboard_ready', '{"sceneKey":"scene-1"}')
			RETURNING id::text
		`, organizationID, projectID, scriptID, versionID, episodeID, sourcePlanID,
			index, index+1, "Shot", "Visual", bounds[0], bounds[1]).Scan(&shotIDs[index]); err != nil {
			t.Fatalf("insert source shot %d: %v", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO storyboard_shot_timing_spans(
				storyboard_plan_id, storyboard_shot_id, timing_unit_id,
				span_start_tick, span_end_tick, ordinal, metadata
			)
			VALUES ($1, $2, $3, $4, $5, 0, '{"sceneKey":"scene-1"}')
		`, sourcePlanID, shotIDs[index], unitIDs[index], bounds[0], bounds[1]); err != nil {
			t.Fatalf("insert source span %d: %v", index, err)
		}
	}

	split, err := SplitStoryboardShotTx(ctx, tx, SplitStoryboardShotRequest{
		ProjectID: projectID, ShotID: shotIDs[0], SplitTick: 45000, ActorID: userID,
	})
	if err != nil {
		t.Fatalf("split storyboard shot: %v", err)
	}
	if split.Revision != 2 || split.Validation.ShotCount != 3 || len(split.ShotIDs) != 2 {
		t.Fatalf("split result = %+v", split)
	}
	if _, err := ActivateStoryboardPlanTx(ctx, tx, ActivateStoryboardPlanRequest{
		ProjectID: projectID, ScriptEpisodeID: episodeID, StoryboardPlanID: split.StoryboardPlanID, ActorID: userID,
	}); err != nil {
		t.Fatalf("activate split plan: %v", err)
	}

	merge, err := MergeStoryboardShotsTx(ctx, tx, MergeStoryboardShotsRequest{
		ProjectID: projectID, ShotIDs: split.ShotIDs, ActorID: userID, Visual: "Merged visual",
	})
	if err != nil {
		t.Fatalf("merge storyboard shots: %v", err)
	}
	if merge.Revision != 3 || merge.Validation.ShotCount != 2 {
		t.Fatalf("merge result = %+v", merge)
	}
	if _, err := ActivateStoryboardPlanTx(ctx, tx, ActivateStoryboardPlanRequest{
		ProjectID: projectID, ScriptEpisodeID: episodeID, StoryboardPlanID: merge.StoryboardPlanID, ActorID: userID,
	}); err != nil {
		t.Fatalf("activate merge plan: %v", err)
	}

	var firstMergedShotID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM storyboard_shots
		WHERE storyboard_plan_id = $1 AND deleted_at IS NULL
		ORDER BY shot_index LIMIT 1
	`, merge.StoryboardPlanID).Scan(&firstMergedShotID); err != nil {
		t.Fatalf("read merged shot: %v", err)
	}
	newEnd := int64(101250)
	retimed, err := RetimeStoryboardShotTx(ctx, tx, RetimeStoryboardShotRequest{
		ProjectID: projectID, ShotID: firstMergedShotID, EndTick: &newEnd, ActorID: userID,
	})
	if err != nil {
		t.Fatalf("retime storyboard shot: %v", err)
	}
	if retimed.Revision != 4 || retimed.Validation.ShotCount != 2 || !retimed.Validation.Valid {
		t.Fatalf("retime result = %+v", retimed)
	}
	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM storyboard_plans WHERE script_episode_id = $1 AND active`, episodeID).Scan(&activeCount); err != nil {
		t.Fatalf("count active plans: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active plans = %d, want 1", activeCount)
	}
}

func seedStoryboardRevisionFixture(t *testing.T, ctx context.Context, tx pgx.Tx) (organizationID, userID, projectID, scriptID, versionID, episodeID, analysisID string) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := tx.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Revision Test', $1) RETURNING id::text`, "revision-"+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, 'Revision Test') RETURNING id::text`, "revision-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var workspaceID string
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Revision') RETURNING id::text`, organizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by, timeline_timebase, fps_numerator, fps_denominator)
		VALUES ($1, $2, 'Revision', $3, 90000, 24, 1) RETURNING id::text
	`, organizationID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO scripts(organization_id, project_id, title, created_by) VALUES ($1, $2, 'Revision Script', $3) RETURNING id::text`, organizationID, projectID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_versions(script_id, organization_id, project_id, version_no, version, content, created_by)
		VALUES ($1, $2, $3, 1, 1, 'test', $4) RETURNING id::text
	`, scriptID, organizationID, projectID, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id,
			episode_index, episode_title, content, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第 1 集', 'test', $5) RETURNING id::text
	`, organizationID, projectID, scriptID, versionID, userID).Scan(&episodeID); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			method_version, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 180000, 180000, 180000, 90000, 24, 1, 'test-v1', $6)
		RETURNING id::text
	`, organizationID, projectID, scriptID, versionID, episodeID, userID).Scan(&analysisID); err != nil {
		t.Fatalf("insert analysis: %v", err)
	}
	return
}
