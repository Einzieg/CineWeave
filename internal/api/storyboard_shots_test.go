package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestStoryboardShots(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot-1.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/shot-1.mp4", "video/mp4")
	seed.insertStoryboardShot(t, workflowRunID, imageArtifactID, videoArtifactID)

	assertAPIErrorCode(t, server, http.MethodGet, "/api/workflow-runs/"+workflowRunID+"/shots", seed.otherToken, seed.organizationID, nil, http.StatusForbidden, "ACCESS_DENIED")

	var listed struct {
		Items []StoryboardShot `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/workflow-runs/"+workflowRunID+"/shots?includePreviewUrl=true&previewExpiresSeconds=900", seed.ownerToken, seed.organizationID, nil, &listed)
	if len(listed.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(listed.Items))
	}
	item := listed.Items[0]
	if item.WorkflowRunID != workflowRunID || item.ShotNo != 1 || item.Status != "video_succeeded" || item.ImageArtifactID == nil || *item.ImageArtifactID != imageArtifactID || item.VideoArtifactID == nil || *item.VideoArtifactID != videoArtifactID {
		t.Fatalf("shot item = %+v", item)
	}
	if item.ImagePreviewURL == nil || item.VideoPreviewURL == nil || !strings.Contains(*item.ImagePreviewURL, "localhost:9000") || !strings.Contains(*item.VideoPreviewURL, "localhost:9000") {
		t.Fatalf("preview URLs missing: image=%v video=%v", item.ImagePreviewURL, item.VideoPreviewURL)
	}
}

func (s *artifactPreviewSeed) insertStoryboardShot(t *testing.T, workflowRunID, imageArtifactID, videoArtifactID string) string {
	t.Helper()
	var shotID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			duration_seconds, visual, camera, motion, mood, image_prompt, video_prompt,
			image_artifact_id, image_storage_key, video_artifact_id, video_storage_key, status, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 5, 'Wide station', 'slow push', 'mist drifting', 'hopeful', 'image prompt', 'video prompt', $4, 'org/project/shot-1.png', $5, 'org/project/shot-1.mp4', 'video_succeeded', '{}')
		RETURNING id
	`, s.organizationID, s.projectID, workflowRunID, imageArtifactID, videoArtifactID).Scan(&shotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	return shotID
}
