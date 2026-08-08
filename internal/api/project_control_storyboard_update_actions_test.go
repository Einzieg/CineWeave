package api

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
)

func TestStoryboardUpdateShotUsesSharedMutationAndReplaysWithoutDuplicateEvent(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	var shotID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt, status, metadata
		)
		VALUES (
			$1, $2, $3, 0, 1, 0, 450000, 450000, 450000,
			'旧画面', '固定机位', '静止', '平静', '旧图片提示词', '旧视频提示词', 'ready', '{}'
		)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID).Scan(&shotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	command := createManualProjectControlCommand(t, seed, "storyboard.update_shot", "storyboard-update-shared")
	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	input := mustRawJSON(map[string]any{
		"shotId": shotID,
		"patch":  map[string]any{"visual": "新画面"},
	})
	for attempt := 0; attempt < 2; attempt++ {
		result, err := seed.apiServer.executeStoryboardUpdateShotAsyncAction(
			seed.ctx,
			auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
			project,
			command,
			input,
		)
		if err != nil {
			t.Fatalf("update storyboard shot attempt %d: %v", attempt+1, err)
		}
		if result.Data["shotId"] != shotID {
			t.Fatalf("storyboard update result=%+v", result.Data)
		}
	}
	var visual, imageStatus, videoStatus, commandID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT visual, image_status, video_status, COALESCE(metadata->>'projectControlCommandId', '')
		FROM storyboard_shots
		WHERE id = $1
	`, shotID).Scan(&visual, &imageStatus, &videoStatus, &commandID); err != nil {
		t.Fatalf("read updated storyboard shot: %v", err)
	}
	if visual != "新画面" || commandID != command.ID || imageStatus == "succeeded" || videoStatus == "succeeded" {
		t.Fatalf("updated shot visual=%q image=%q video=%q command=%q", visual, imageStatus, videoStatus, commandID)
	}
	var eventCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM event_outbox
		WHERE project_id = $1 AND event_type = 'storyboard.shot.updated' AND aggregate_id = $2
	`, seed.projectID, shotID).Scan(&eventCount); err != nil {
		t.Fatalf("count storyboard update events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("storyboard update event count=%d, want 1", eventCount)
	}
}
