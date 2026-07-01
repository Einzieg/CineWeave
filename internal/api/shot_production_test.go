package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestShotProductionStatusEmptyProject(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var status ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if status.ProjectID != seed.projectID || status.Summary.Total != 0 || len(status.Shots) != 0 {
		t.Fatalf("status = %+v", status)
	}
}

func TestShotProductionStatusSummary(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/shot.mp4", "video/mp4")
	insertShotProductionShot(t, seed, workflowRunID, 0, imageArtifactID, videoArtifactID, "succeeded", "succeeded")
	insertShotProductionShot(t, seed, workflowRunID, 1, "", "", "not_started", "not_started")
	insertShotProductionShot(t, seed, workflowRunID, 2, imageArtifactID, videoArtifactID, "stale", "stale")
	insertShotProductionShot(t, seed, workflowRunID, 3, "", "", "failed", "failed")

	var status ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status?workflowRunId="+workflowRunID, seed.ownerToken, seed.organizationID, nil, &status)
	if status.Summary.Total != 4 ||
		status.Summary.ImageSucceeded != 1 ||
		status.Summary.ImageMissing != 1 ||
		status.Summary.ImageStale != 1 ||
		status.Summary.ImageFailed != 1 ||
		status.Summary.VideoSucceeded != 1 ||
		status.Summary.VideoMissing != 1 ||
		status.Summary.VideoStale != 1 ||
		status.Summary.VideoFailed != 1 {
		t.Fatalf("summary = %+v", status.Summary)
	}
}

func TestShotProductionGenerateMissingImagesTargetsMissingAndStale(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	missingID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")
	staleID := insertShotProductionShot(t, seed, workflowRunID, 1, imageArtifactID, "", "stale", "not_started")
	insertShotProductionShot(t, seed, workflowRunID, 2, imageArtifactID, "", "succeeded", "not_started")

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":        "generate_missing_images",
		"workflowRunId": workflowRunID,
	}, &response)
	assertStringSet(t, response.TargetShotIDs, []string{missingID, staleID})
	if response.WorkflowType != "batch_generate_shot_images" || temporal.workflow == nil {
		t.Fatalf("response=%+v temporal=%+v", response, temporal)
	}
	input := temporal.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		ShotIDs []string `json:"shotIds"`
		Force   bool     `json:"force"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	assertStringSet(t, options.ShotIDs, []string{missingID, staleID})
	if !options.Force {
		t.Fatalf("force = false")
	}
}

func TestShotProductionGenerateMissingVideosSkipsNoImage(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")
	withImageID := insertShotProductionShot(t, seed, workflowRunID, 1, imageArtifactID, "", "succeeded", "not_started")

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":        "generate_missing_videos",
		"workflowRunId": workflowRunID,
	}, &response)
	assertStringSet(t, response.TargetShotIDs, []string{withImageID})
	if response.WorkflowType != "batch_generate_shot_videos" {
		t.Fatalf("response = %+v", response)
	}
}

func TestShotProductionActionPermission(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.otherToken, seed.organizationID, map[string]any{
		"action": "generate_missing_images",
	}, http.StatusForbidden, "ACCESS_DENIED")
}

func insertShotProductionShot(t *testing.T, seed *artifactPreviewSeed, workflowRunID string, index int, imageArtifactID, videoArtifactID, imageStatus, videoStatus string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			duration_seconds, visual, camera, motion, mood, image_prompt, video_prompt,
			image_artifact_id, video_artifact_id, image_status, video_status, status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 5, $6, 'slow push', 'mist drifting', 'hopeful',
		        'image prompt', 'video prompt', NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, $9, $10, 'pending', 'pending', '{}')
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID, index, index+1, "Shot visual", imageArtifactID, videoArtifactID, imageStatus, videoStatus).Scan(&id); err != nil {
		t.Fatalf("insert shot production shot: %v", err)
	}
	return id
}

func assertStringSet(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	gotSet := map[string]bool{}
	for _, value := range got {
		gotSet[value] = true
	}
	for _, value := range want {
		if !gotSet[value] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}
