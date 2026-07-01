package api

import (
	"net/http"
	"testing"
)

func TestTimelineCreateFromStoryboardAndReorder(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	videoA := seed.insertArtifact(t, "generated_video", "org/project/shot-a.mp4", "video/mp4")
	videoB := seed.insertArtifact(t, "generated_video", "org/project/shot-b.mp4", "video/mp4")
	insertTimelineStoryboardShot(t, seed, workflowRunID, 0, videoA)
	insertTimelineStoryboardShot(t, seed, workflowRunID, 1, videoB)

	var timeline ProjectTimeline
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/timelines", seed.ownerToken, seed.organizationID, map[string]any{
		"title":               "主时间线",
		"fromStoryboardShots": true,
	}, &timeline)
	if timeline.ID == "" || timeline.Title != "主时间线" {
		t.Fatalf("timeline = %+v", timeline)
	}

	var listed struct {
		Items []TimelineClip `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/timelines/"+timeline.ID+"/clips", seed.ownerToken, seed.organizationID, nil, &listed)
	if len(listed.Items) != 2 || listed.Items[0].ClipIndex != 0 || listed.Items[1].ClipIndex != 1 || !listed.Items[0].Enabled || !listed.Items[1].Enabled {
		t.Fatalf("clips = %+v", listed.Items)
	}

	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/timelines/"+timeline.ID+"/clips/reorder", seed.ownerToken, seed.organizationID, map[string]any{
		"items": []map[string]any{
			{"clipId": listed.Items[0].ID, "clipIndex": 1},
			{"clipId": listed.Items[1].ID, "clipIndex": 0},
		},
	}, &struct{}{})

	var reordered struct {
		Items []TimelineClip `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/timelines/"+timeline.ID+"/clips", seed.ownerToken, seed.organizationID, nil, &reordered)
	if len(reordered.Items) != 2 || reordered.Items[0].ID != listed.Items[1].ID || reordered.Items[1].ID != listed.Items[0].ID {
		t.Fatalf("reordered = %+v", reordered.Items)
	}
}

func TestTimelineAccessAndFinalVideoActivation(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/timelines", seed.otherToken, seed.organizationID, map[string]any{
		"title": "blocked",
	}, http.StatusForbidden, "ACCESS_DENIED")

	timelineID := insertProjectTimeline(t, seed)
	first := insertFinalVideoVersion(t, seed, timelineID, 1, "active")
	second := insertFinalVideoVersion(t, seed, timelineID, 2, "ready")

	var activated FinalVideoVersion
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/final-videos/"+second+"/activate", seed.ownerToken, seed.organizationID, nil, &activated)
	if activated.ID != second || activated.Status != "active" {
		t.Fatalf("activated = %+v", activated)
	}
	var firstStatus, secondStatus, activeProjectID string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM final_video_versions WHERE id = $1`, first).Scan(&firstStatus); err != nil {
		t.Fatalf("select first version: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM final_video_versions WHERE id = $1`, second).Scan(&secondStatus); err != nil {
		t.Fatalf("select second version: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT active_final_video_version_id::text FROM projects WHERE id = $1`, seed.projectID).Scan(&activeProjectID); err != nil {
		t.Fatalf("select active project version: %v", err)
	}
	if firstStatus != "ready" || secondStatus != "active" || activeProjectID != second {
		t.Fatalf("statuses first=%s second=%s active=%s", firstStatus, secondStatus, activeProjectID)
	}
}

func insertTimelineStoryboardShot(t *testing.T, seed *artifactPreviewSeed, workflowRunID string, shotIndex int, videoArtifactID string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			duration_seconds, visual, camera, motion, mood, image_prompt, video_prompt,
			video_artifact_id, status, video_status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 5, $6, 'slow push', 'mist drifting', 'hopeful',
		        'image prompt', 'video prompt', $7, 'video_succeeded', 'succeeded', 'approved', '{}')
		RETURNING id
	`, seed.organizationID, seed.projectID, workflowRunID, shotIndex, shotIndex+1, "Shot visual", videoArtifactID).Scan(&id); err != nil {
		t.Fatalf("insert timeline storyboard shot: %v", err)
	}
	return id
}

func insertProjectTimeline(t *testing.T, seed *artifactPreviewSeed) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO project_timelines(organization_id, project_id, title, status, aspect_ratio, resolution, metadata, created_by)
		VALUES ($1, $2, 'Test Timeline', 'active', '16:9', '720p', '{}', $3)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, seed.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert project timeline: %v", err)
	}
	return id
}

func insertFinalVideoVersion(t *testing.T, seed *artifactPreviewSeed, timelineID string, version int, status string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO final_video_versions(
			organization_id, project_id, timeline_id, version, title, status,
			resolution, aspect_ratio, compose_settings, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, '720p', '16:9', '{}', '{}', $7)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, timelineID, version, "Version", status, seed.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert final video version: %v", err)
	}
	return id
}
