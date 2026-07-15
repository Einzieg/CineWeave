package storyboard

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestActivateStoryboardPlanTxArchivesPreviousPlanAndStalesDownstream(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for storyboard activation integration test")
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

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	var organizationID, userID, workspaceID, projectID string
	if err := tx.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Timing Test', $1) RETURNING id::text`, "timing-"+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, 'Timing Test') RETURNING id::text`, "timing-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Timing') RETURNING id::text`, organizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by, timeline_timebase, fps_numerator, fps_denominator)
		VALUES ($1, $2, 'Timing', $3, 90000, 24, 1)
		RETURNING id::text
	`, organizationID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	var scriptID, versionID, episodeID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, title, created_by)
		VALUES ($1, $2, 'Timing Script', $3)
		RETURNING id::text
	`, organizationID, projectID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_versions(script_id, organization_id, project_id, version_no, version, content, created_by)
		VALUES ($1, $2, $3, 1, 1, 'test', $4)
		RETURNING id::text
	`, scriptID, organizationID, projectID, userID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id,
			episode_index, episode_title, content, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第 1 集', 'test', $5)
		RETURNING id::text
	`, organizationID, projectID, scriptID, versionID, userID).Scan(&episodeID); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	var analysisID string
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
		t.Fatalf("insert timing analysis: %v", err)
	}
	unitIDs := make([]string, 2)
	for index, bounds := range [][2]int64{{0, 90000}, {90000, 180000}} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO script_timing_units(
				timing_analysis_id, unit_ordinal, unit_type, track, source_text,
				start_tick, end_tick, duration_source
			)
			VALUES ($1, $2, 'action', 'visual', 'test', $3, $4, 'rule_estimated')
			RETURNING id::text
		`, analysisID, index, bounds[0], bounds[1]).Scan(&unitIDs[index]); err != nil {
			t.Fatalf("insert timing unit %d: %v", index, err)
		}
	}

	var previousPlanID, nextPlanID string
	for revision, destination := range []*string{&previousPlanID, &nextPlanID} {
		active := revision == 0
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_plans(
				organization_id, project_id, script_id, script_version_id, script_episode_id,
				timing_analysis_id, revision, status, target_duration_ticks,
				estimated_shot_count, actual_shot_count, active, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'ready', 180000, 2, 2, $8, $9)
			RETURNING id::text
		`, organizationID, projectID, scriptID, versionID, episodeID, analysisID, revision+1, active, userID).Scan(destination); err != nil {
			t.Fatalf("insert storyboard plan %d: %v", revision+1, err)
		}
	}

	oldShotIDs := make([]string, 2)
	newShotIDs := make([]string, 2)
	for index, bounds := range [][2]int64{{0, 90000}, {90000, 180000}} {
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shots(
				organization_id, project_id, script_id, script_version_id, script_episode_id,
				storyboard_plan_id, shot_index, shot_no, episode_index, episode_shot_index,
				start_tick, end_tick, duration_min_ticks, duration_max_ticks,
				image_storage_key, video_storage_key, image_status, video_status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $7, $9, $10, 90000, 90000, $11, $12, 'succeeded', 'succeeded')
			RETURNING id::text
		`, organizationID, projectID, scriptID, versionID, episodeID, previousPlanID, index, index+1, bounds[0], bounds[1],
			"old-image-"+suffix, "old-video-"+suffix).Scan(&oldShotIDs[index]); err != nil {
			t.Fatalf("insert previous shot %d: %v", index, err)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO storyboard_shots(
				organization_id, project_id, script_id, script_version_id, script_episode_id,
				storyboard_plan_id, shot_index, shot_no, episode_index, episode_shot_index,
				start_tick, end_tick, duration_min_ticks, duration_max_ticks
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $7, $9, $10, 90000, 90000)
			RETURNING id::text
		`, organizationID, projectID, scriptID, versionID, episodeID, nextPlanID, index+10, index+11, bounds[0], bounds[1]).Scan(&newShotIDs[index]); err != nil {
			t.Fatalf("insert next shot %d: %v", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO storyboard_shot_timing_spans(
				storyboard_plan_id, storyboard_shot_id, timing_unit_id,
				span_start_tick, span_end_tick, ordinal
			)
			VALUES ($1, $2, $3, $4, $5, 0)
		`, nextPlanID, newShotIDs[index], unitIDs[index], bounds[0], bounds[1]); err != nil {
			t.Fatalf("insert timing span %d: %v", index, err)
		}
	}

	var timelineID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_timelines(
			organization_id, project_id, title, status, timeline_timebase, fps_numerator, fps_denominator
		)
		VALUES ($1, $2, 'Old Timeline', 'active', 90000, 24, 1)
		RETURNING id::text
	`, organizationID, projectID).Scan(&timelineID); err != nil {
		t.Fatalf("insert timeline: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO timeline_clips(
			organization_id, project_id, timeline_id, storyboard_shot_id, clip_index,
			title, source_duration_ticks, trim_start_tick, trim_end_tick, start_tick, end_tick
		)
		VALUES ($1, $2, $3, $4, 0, 'Old Clip', 90000, 0, 90000, 0, 90000)
	`, organizationID, projectID, timelineID, oldShotIDs[0]); err != nil {
		t.Fatalf("insert timeline clip: %v", err)
	}

	result, err := ActivateStoryboardPlanTx(ctx, tx, ActivateStoryboardPlanRequest{
		ProjectID:        projectID,
		ScriptEpisodeID:  episodeID,
		StoryboardPlanID: nextPlanID,
		ActorID:          userID,
	})
	if err != nil {
		t.Fatalf("activate storyboard plan: %v", err)
	}
	if result.PreviousStoryboardPlanID != previousPlanID || result.ShotCount != 2 {
		t.Fatalf("activation result = %+v", result)
	}

	var previousStatus, previousStale string
	var previousActive bool
	if err := tx.QueryRow(ctx, `SELECT status, active, stale_state FROM storyboard_plans WHERE id = $1`, previousPlanID).Scan(&previousStatus, &previousActive, &previousStale); err != nil {
		t.Fatalf("read previous plan: %v", err)
	}
	if previousStatus != "archived" || previousActive || previousStale != "upstream_changed" {
		t.Fatalf("previous plan status=%s active=%v stale=%s", previousStatus, previousActive, previousStale)
	}
	var nextActive bool
	var nextShotCount int
	if err := tx.QueryRow(ctx, `SELECT active, actual_shot_count FROM storyboard_plans WHERE id = $1`, nextPlanID).Scan(&nextActive, &nextShotCount); err != nil {
		t.Fatalf("read next plan: %v", err)
	}
	if !nextActive || nextShotCount != 2 {
		t.Fatalf("next plan active=%v shotCount=%d", nextActive, nextShotCount)
	}
	var oldShotStale, oldImageStatus, oldVideoStatus string
	if err := tx.QueryRow(ctx, `SELECT stale_state, image_status, video_status FROM storyboard_shots WHERE id = $1`, oldShotIDs[0]).Scan(&oldShotStale, &oldImageStatus, &oldVideoStatus); err != nil {
		t.Fatalf("read previous shot: %v", err)
	}
	if oldShotStale != "upstream_changed" || oldImageStatus != "stale" || oldVideoStatus != "stale" {
		t.Fatalf("previous shot stale=%s image=%s video=%s", oldShotStale, oldImageStatus, oldVideoStatus)
	}
	var timelineStale, clipStale string
	if err := tx.QueryRow(ctx, `SELECT stale_state FROM project_timelines WHERE id = $1`, timelineID).Scan(&timelineStale); err != nil {
		t.Fatalf("read timeline stale state: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT stale_state FROM timeline_clips WHERE timeline_id = $1`, timelineID).Scan(&clipStale); err != nil {
		t.Fatalf("read clip stale state: %v", err)
	}
	if timelineStale != "needs_regeneration" || clipStale != "upstream_changed" {
		t.Fatalf("timeline stale=%s clip stale=%s", timelineStale, clipStale)
	}
	var eventCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM event_outbox
		WHERE project_id = $1 AND aggregate_id = $2 AND event_type = 'storyboard.plan.activated'
	`, projectID, nextPlanID).Scan(&eventCount); err != nil {
		t.Fatalf("read activation event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("activation events = %d, want 1", eventCount)
	}
}
