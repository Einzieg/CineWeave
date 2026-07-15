package workflows

import (
	"context"
	"testing"

	mediapkg "github.com/Einzieg/cineweave/internal/media"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestComposeTimelineClipsSkipsDisabledAndCarriesTrim(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, _, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	timelineID := insertWorkflowProjectTimeline(t, ctx, pool, orgID, projectID)
	videoArtifactID, mediaFileID := insertWorkflowTimelineVideo(t, ctx, pool, orgID, projectID, workflowRunID, "timeline/clip-a.mp4")
	shotID := insertWorkflowTimelineShot(t, ctx, pool, orgID, projectID, workflowRunID, videoArtifactID, mediaFileID, "timeline/clip-a.mp4")
	if _, err := pool.Exec(ctx, `
		INSERT INTO timeline_clips(
			organization_id, project_id, timeline_id, storyboard_shot_id, video_artifact_id, video_media_file_id,
			clip_index, title, enabled, source_storage_key, source_duration_ticks,
			trim_start_tick, trim_end_tick, start_tick, end_tick, metadata
		)
		VALUES
		  ($1, $2, $3, $4, $5, $6, 0, 'enabled', true, 'timeline/clip-a.mp4', 450000, 135000, 337500, 0, 360000, '{}'),
		  ($1, $2, $3, $4, $5, $6, 1, 'disabled', false, 'timeline/clip-b.mp4', 450000, 0, 450000, 360000, 810000, '{}')
	`, orgID, projectID, timelineID, shotID, videoArtifactID, mediaFileID); err != nil {
		t.Fatalf("insert timeline clips: %v", err)
	}

	activities := NewActivities(pool, nil, nil)
	clips, err := activities.composeTimelineClips(ctx, timelineID)
	if err != nil {
		t.Fatalf("composeTimelineClips: %v", err)
	}
	if len(clips) != 1 || clips[0].TimelineClipID == "" || clips[0].StorageKey != "timeline/clip-a.mp4" {
		t.Fatalf("clips = %+v", clips)
	}
	if clips[0].TrimStartSeconds != 1.5 || clips[0].TrimEndSeconds == nil || *clips[0].TrimEndSeconds != 3.75 || clips[0].TargetDurationSeconds == nil || *clips[0].TargetDurationSeconds != 4 {
		t.Fatalf("clip trims = %+v", clips[0])
	}
}

func TestCompleteComposeFinalVideoWritesFinalVideoVersion(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	timelineID := insertWorkflowProjectTimeline(t, ctx, pool, orgID, projectID)
	activities := NewActivities(pool, nil, nil)
	nodeExecution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        nodeComposeFinalVideoKey,
		NodeType:       "media.compose",
		Input:          mustJSON(map[string]any{"timelineId": timelineID}),
	})
	if err != nil {
		t.Fatalf("StartNodeRun: %v", err)
	}

	output, err := activities.completeComposeFinalVideo(ctx, ComposeFinalVideoInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		CreatedBy:      userID,
		TimelineID:     timelineID,
		Title:          "Final A",
		AspectRatio:    "16:9",
		Resolution:     "720p",
	}, nodeExecution, []composeClipRecord{{
		TimelineClipID:      "clip",
		ShotID:              "shot",
		ClipIndex:           0,
		Enabled:             true,
		StorageKey:          "timeline/clip-a.mp4",
		StartTick:           0,
		EndTick:             450000,
		DurationTicks:       450000,
		SourceDurationTicks: 450000,
		TimelineTimebase:    90000,
		FPSNumerator:        24,
		FPSDenominator:      1,
	}}, storage.PutResult{
		StorageKey:  "timeline/timeline.json",
		ContentHash: "sha256:timeline",
		ByteSize:    10,
	}, mediapkg.ComposeResult{
		StorageKey:      "timeline/final.mp4",
		MimeType:        "video/mp4",
		ContentHash:     "sha256:final",
		ByteSize:        100,
		DurationSeconds: 5,
		Width:           1280,
		Height:          720,
	})
	if err != nil {
		t.Fatalf("completeComposeFinalVideo: %v", err)
	}
	if output.FinalVideoVersionID == "" || output.ArtifactID == "" || output.MediaFileID == "" {
		t.Fatalf("output = %+v", output)
	}
	var versionStatus, activeID string
	if err := pool.QueryRow(ctx, `SELECT status FROM final_video_versions WHERE id = $1`, output.FinalVideoVersionID).Scan(&versionStatus); err != nil {
		t.Fatalf("select final video version: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT active_final_video_version_id::text FROM projects WHERE id = $1`, projectID).Scan(&activeID); err != nil {
		t.Fatalf("select project active version: %v", err)
	}
	if versionStatus != "active" || activeID != output.FinalVideoVersionID {
		t.Fatalf("version status=%s active=%s", versionStatus, activeID)
	}
}

func insertWorkflowProjectTimeline(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project_timelines(organization_id, project_id, title, status, aspect_ratio, resolution, metadata)
		VALUES ($1, $2, 'Timeline', 'active', '16:9', '720p', '{}')
		RETURNING id::text
	`, orgID, projectID).Scan(&id); err != nil {
		t.Fatalf("insert project timeline: %v", err)
	}
	return id
}

func insertWorkflowTimelineVideo(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, storageKey string) (string, string) {
	t.Helper()
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, type, storage_key, mime_type, content_hash, metadata)
		VALUES ($1, $2, $3, 'generated_video', $4, 'video/mp4', 'sha256:clip', '{}')
		RETURNING id::text
	`, orgID, projectID, workflowRunID, storageKey).Scan(&artifactID); err != nil {
		t.Fatalf("insert workflow timeline artifact: %v", err)
	}
	var mediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, duration_seconds, metadata)
		VALUES ($1, $2, $3, $4, 'video/mp4', 5, '{}')
		RETURNING id::text
	`, orgID, projectID, artifactID, storageKey).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert workflow timeline media: %v", err)
	}
	return artifactID, mediaFileID
}

func insertWorkflowTimelineShot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, artifactID, mediaFileID, storageKey string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, image_prompt, video_prompt, video_artifact_id, video_media_file_id, video_storage_key,
			status, video_status, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 0, 450000, 450000, 450000, 'visual', 'image', 'video', $4, $5, $6, 'video_succeeded', 'succeeded', '{}')
		RETURNING id::text
	`, orgID, projectID, workflowRunID, artifactID, mediaFileID, storageKey).Scan(&id); err != nil {
		t.Fatalf("insert workflow timeline shot: %v", err)
	}
	return id
}
