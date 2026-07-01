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

func TestStoryboardWorkbenchAPIs(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	sceneID := seed.insertScriptScene(t, scriptID, versionID, 1, "approved", "fresh")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/workbench-shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/workbench-shot.mp4", "video/mp4")
	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", imageArtifactID)

	var first StoryboardShot
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots", seed.ownerToken, seed.organizationID, map[string]any{
		"workflowRunId":   workflowRunID,
		"scriptSceneId":   sceneID,
		"shotNo":          1,
		"durationSeconds": 3,
		"visual":          "Manual first shot",
		"camera":          "push",
		"motion":          "mist moves",
		"mood":            "quiet",
		"imagePrompt":     "manual image",
		"videoPrompt":     "manual video",
	}, &first)
	if first.WorkflowRunID != workflowRunID || first.ScriptSceneID == nil || *first.ScriptSceneID != sceneID || !first.ManualOverride || first.StaleState != "needs_regeneration" {
		t.Fatalf("created first shot = %+v", first)
	}
	var second StoryboardShot
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots", seed.ownerToken, seed.organizationID, map[string]any{
		"workflowRunId":   workflowRunID,
		"scriptSceneId":   sceneID,
		"shotNo":          2,
		"durationSeconds": 4,
		"visual":          "Manual second shot",
	}, &second)

	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/reorder", seed.ownerToken, seed.organizationID, map[string]any{
		"items": []map[string]any{
			{"shotId": second.ID, "shotIndex": 0, "shotNo": 1},
			{"shotId": first.ID, "shotIndex": 1, "shotNo": 2},
		},
	}, &struct{}{})
	assertStoryboardShotPosition(t, seed, second.ID, 0, 1)
	assertStoryboardShotPosition(t, seed, first.ID, 1, 2)

	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET image_artifact_id = $2, image_storage_key = 'org/project/workbench-shot.png',
		    video_artifact_id = $3, video_storage_key = 'org/project/workbench-shot.mp4'
		WHERE id = $1
	`, second.ID, imageArtifactID, videoArtifactID); err != nil {
		t.Fatalf("attach media to shot: %v", err)
	}
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, second.ID, assetID, "approved", imageArtifactID)
	var detail StoryboardShotDetail
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID+"/detail?previewExpiresSeconds=900", seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.Shot.ID != second.ID || detail.ScriptScene == nil || detail.ScriptScene.ID != sceneID || detail.ImagePreviewURL == nil || detail.VideoPreviewURL == nil {
		t.Fatalf("detail media/scene = %+v", detail)
	}
	if len(detail.Requirements) != 1 || detail.Requirements[0].ID != requirementID || detail.Requirements[0].Asset == nil || detail.Requirements[0].DerivedPreviewURL == nil {
		t.Fatalf("detail requirements = %+v", detail.Requirements)
	}

	var updated StoryboardShot
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"visual": "Edited second shot",
	}, &updated)
	if updated.StaleState != "needs_regeneration" || !updated.ManualOverride || updated.Visual != "Edited second shot" {
		t.Fatalf("updated shot = %+v", updated)
	}

	doAPISuccess(t, server, http.MethodDelete, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, nil, &struct{}{})
	var listed struct {
		Items []StoryboardShot `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/workflow-runs/"+workflowRunID+"/shots", seed.ownerToken, seed.organizationID, nil, &listed)
	for _, item := range listed.Items {
		if item.ID == second.ID {
			t.Fatalf("deleted shot returned in list: %+v", listed.Items)
		}
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

func assertStoryboardShotPosition(t *testing.T, seed *artifactPreviewSeed, shotID string, wantIndex, wantNo int) {
	t.Helper()
	var gotIndex, gotNo int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT shot_index, COALESCE(shot_no, shot_index + 1)
		FROM storyboard_shots
		WHERE id = $1 AND project_id = $2
	`, shotID, seed.projectID).Scan(&gotIndex, &gotNo); err != nil {
		t.Fatalf("read shot position: %v", err)
	}
	if gotIndex != wantIndex || gotNo != wantNo {
		t.Fatalf("shot %s position = (%d,%d), want (%d,%d)", shotID, gotIndex, gotNo, wantIndex, wantNo)
	}
}
