package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestShotProductionShotFromStoryboardPreservesEpisodeScope(t *testing.T) {
	episodeID := "episode-1"
	episodeIndex := 1
	episodeShotIndex := 4
	duration := 6.5
	shot := shotProductionShotFromStoryboard(StoryboardShot{
		ID:               "shot-1",
		WorkflowRunID:    "workflow-1",
		ScriptEpisodeID:  &episodeID,
		EpisodeIndex:     &episodeIndex,
		EpisodeShotIndex: &episodeShotIndex,
		EpisodeTitle:     "第一集",
		ShotIndex:        4,
		ShotNo:           5,
		Title:            "山门雨夜",
		DurationSeconds:  &duration,
		ImageStatus:      "not_started",
		VideoStatus:      "not_started",
		StaleState:       "fresh",
	})
	if shot.ScriptEpisodeID == nil || *shot.ScriptEpisodeID != episodeID || shot.EpisodeIndex == nil || *shot.EpisodeIndex != 1 || shot.EpisodeShotIndex == nil || *shot.EpisodeShotIndex != 4 {
		t.Fatalf("episode scope = %+v", shot)
	}
	if shot.Title != "山门雨夜" || shot.DurationSeconds == nil || *shot.DurationSeconds != duration {
		t.Fatalf("shot display fields = %+v", shot)
	}
}

func TestShotProductionStatusEmptyProject(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET video_ratio = '9:16' WHERE id = $1`, seed.projectID); err != nil {
		t.Fatalf("set project video ratio: %v", err)
	}

	var status ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if status.ProjectID != seed.projectID || status.AspectRatio != "9:16" || status.Summary.Total != 0 || len(status.Shots) != 0 {
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

func TestShotProductionStatusDefaultsToActivePlanAndCanInspectHistoricalPlan(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	var episodeID, timingAnalysisID, historicalPlanID, activePlanID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id,
			episode_index, episode_title, content, content_format, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第一集', 'script content', 'markdown', $5)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionID, seed.ownerUserID).Scan(&episodeID); err != nil {
		t.Fatalf("insert script episode: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks,
			timeline_timebase, fps_numerator, fps_denominator, method_version, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 900000, 900000, 90000, 24, 1, 'test', $6)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionID, episodeID, seed.ownerUserID).Scan(&timingAnalysisID); err != nil {
		t.Fatalf("insert timing analysis: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, target_duration_ticks, actual_shot_count, active, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', 900000, 1, false, $7)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionID, episodeID, timingAnalysisID, seed.ownerUserID).Scan(&historicalPlanID); err != nil {
		t.Fatalf("insert historical storyboard plan: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			timing_analysis_id, revision, status, target_duration_ticks, actual_shot_count, active, activated_at, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 2, 'ready', 900000, 1, true, now(), $7)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionID, episodeID, timingAnalysisID, seed.ownerUserID).Scan(&activePlanID); err != nil {
		t.Fatalf("insert active storyboard plan: %v", err)
	}

	historicalWorkflowID := seed.insertWorkflowRun(t, "succeeded")
	activeWorkflowID := seed.insertWorkflowRun(t, "succeeded")
	unplannedWorkflowID := seed.insertWorkflowRun(t, "succeeded")
	historicalShotID := insertShotProductionShot(t, seed, historicalWorkflowID, 0, "", "", "not_started", "not_started")
	activeShotID := insertShotProductionShot(t, seed, activeWorkflowID, 0, "", "", "not_started", "not_started")
	insertShotProductionShot(t, seed, unplannedWorkflowID, 0, "", "", "not_started", "not_started")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET storyboard_plan_id = $2, script_episode_id = $3, episode_index = 1,
		    episode_shot_index = NULL, shot_index = 20000, shot_no = 20001
		WHERE id = $1
	`, historicalShotID, historicalPlanID, episodeID); err != nil {
		t.Fatalf("scope historical shot: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET storyboard_plan_id = $2, script_episode_id = $3, episode_index = 1, episode_shot_index = 0
		WHERE id = $1
	`, activeShotID, activePlanID, episodeID); err != nil {
		t.Fatalf("scope active shot: %v", err)
	}

	var current ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status", seed.ownerToken, seed.organizationID, nil, &current)
	if current.Summary.Total != 1 || len(current.Shots) != 1 || current.Shots[0].ID != activeShotID || stringValue(current.Shots[0].StoryboardPlanID) != activePlanID {
		t.Fatalf("default production scope = %+v", current)
	}

	var historical ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status?storyboardPlanId="+historicalPlanID, seed.ownerToken, seed.organizationID, nil, &historical)
	if historical.Summary.Total != 1 || len(historical.Shots) != 1 || historical.Shots[0].ID != historicalShotID || stringValue(historical.Shots[0].StoryboardPlanID) != historicalPlanID || historical.Shots[0].ShotNo != 1 || historical.Shots[0].ShotIndex != 0 {
		t.Fatalf("historical production scope = %+v", historical)
	}
}

func TestShotProductionStatusReconcilesTerminalImageWorkflow(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "cancelled")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "running", "not_started")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET image_status = 'running',
		    image_workflow_run_id = $2,
		    image_prompt_status = 'running',
		    image_prompt_workflow_run_id = $2
		WHERE id = $1
	`, shotID, workflowRunID); err != nil {
		t.Fatalf("set running shot state: %v", err)
	}

	var status ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if len(status.Shots) != 1 || status.Shots[0].ImageStatus != "failed" || status.Shots[0].ImagePromptStatus != "failed" {
		t.Fatalf("status = %+v", status)
	}
	if stringValue(status.Shots[0].ImageErrorCode) != "USER_CANCELLED" || stringValue(status.Shots[0].ImagePromptErrorCode) != "USER_CANCELLED" {
		t.Fatalf("error state = image:%v prompt:%v", status.Shots[0].ImageErrorCode, status.Shots[0].ImagePromptErrorCode)
	}
}

func TestShotProductionStatusReconcilesSucceededVideoWithoutDurableMedia(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/missing-video.mp4", "video/mp4")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, imageArtifactID, videoArtifactID, "succeeded", "succeeded")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET video_media_file_id = NULL, video_storage_key = NULL
		WHERE id = $1
	`, shotID); err != nil {
		t.Fatalf("remove durable video output: %v", err)
	}

	var status ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if len(status.Shots) != 1 || status.Shots[0].VideoStatus != "failed" || !status.Shots[0].CanRetryVideo {
		t.Fatalf("status = %+v", status)
	}
	if stringValue(status.Shots[0].VideoErrorCode) != "VIDEO_OUTPUT_MISSING" {
		t.Fatalf("video error = %v", status.Shots[0].VideoErrorCode)
	}
}

func TestShotProductionStatusKeepsSucceededProviderResultWhileWorkflowComposesMedia(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "running")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/provider-video.mp4", "video/mp4")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, imageArtifactID, videoArtifactID, "succeeded", "succeeded")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET video_workflow_run_id = $2,
		    video_media_file_id = NULL,
		    video_storage_key = NULL
		WHERE id = $1
	`, shotID, workflowRunID); err != nil {
		t.Fatalf("set composing video state: %v", err)
	}

	var status ShotProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/shot-production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if len(status.Shots) != 1 || status.Shots[0].VideoStatus != "succeeded" {
		t.Fatalf("status = %+v", status)
	}
	if status.Shots[0].VideoErrorCode != nil || status.Shots[0].VideoErrorMessage != nil {
		t.Fatalf("video error = code:%v message:%v", status.Shots[0].VideoErrorCode, status.Shots[0].VideoErrorMessage)
	}
}

func TestUpdateProjectVideoRatioStalesExistingShotMedia(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/ratio-shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/ratio-shot.mp4", "video/mp4")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, imageArtifactID, videoArtifactID, "succeeded", "succeeded")

	var project Project
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID, seed.ownerToken, seed.organizationID, map[string]any{
		"videoRatio": "9:16",
	}, &project)
	if project.VideoRatio != "9:16" {
		t.Fatalf("project video ratio = %q", project.VideoRatio)
	}

	var imageStatus, videoStatus, staleState, expectedAspectRatio string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT image_status, video_status, stale_state, COALESCE(metadata->>'expectedAspectRatio', '')
		FROM storyboard_shots
		WHERE id = $1
	`, shotID).Scan(&imageStatus, &videoStatus, &staleState, &expectedAspectRatio); err != nil {
		t.Fatalf("select stale shot: %v", err)
	}
	if imageStatus != "stale" || videoStatus != "stale" || staleState != "needs_regeneration" || expectedAspectRatio != "9:16" {
		t.Fatalf("shot ratio state = image:%s video:%s stale:%s ratio:%s", imageStatus, videoStatus, staleState, expectedAspectRatio)
	}
}

func TestShotProductionGenerateMissingImagesTargetsMissingAndStale(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET video_ratio = '9:16' WHERE id = $1`, seed.projectID); err != nil {
		t.Fatalf("set project video ratio: %v", err)
	}
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	missingID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")
	staleID := insertShotProductionShot(t, seed, workflowRunID, 1, imageArtifactID, "", "stale", "not_started")
	insertShotProductionShot(t, seed, workflowRunID, 2, imageArtifactID, "", "succeeded", "not_started")

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":        "generate_missing_images",
		"workflowRunId": workflowRunID,
		"options":       map[string]any{"aspectRatio": "1:1"},
	}, &response)
	dispatchWorkflowStartsForTest(t, server)
	assertStringSet(t, response.TargetShotIDs, []string{missingID, staleID})
	if response.WorkflowType != "batch_generate_shot_images" || temporal.workflow == nil {
		t.Fatalf("response=%+v temporal=%+v", response, temporal)
	}
	input := temporal.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		ShotIDs        []string `json:"shotIds"`
		Force          bool     `json:"force"`
		MaxConcurrency int      `json:"maxConcurrency"`
		AspectRatio    string   `json:"aspectRatio"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	assertStringSet(t, options.ShotIDs, []string{missingID, staleID})
	if !options.Force {
		t.Fatalf("force = false")
	}
	if options.MaxConcurrency != workflows.DefaultShotImageConcurrency {
		t.Fatalf("maxConcurrency = %d, want %d", options.MaxConcurrency, workflows.DefaultShotImageConcurrency)
	}
	if options.AspectRatio != "9:16" {
		t.Fatalf("aspectRatio = %q, want project video ratio", options.AspectRatio)
	}
}

func TestShotProductionActionScopesTargetsToScriptEpisode(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	episodeIDs := make([]string, 2)
	for index := range episodeIDs {
		if err := seed.pool.QueryRow(seed.ctx, `
			INSERT INTO script_episodes(
				organization_id, project_id, script_id, script_version_id,
				episode_index, episode_title, content, content_format, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'markdown', $8)
			RETURNING id::text
		`, seed.organizationID, seed.projectID, scriptID, versionID, index+1,
			fmt.Sprintf("第%d集", index+1), fmt.Sprintf("episode %d", index+1), seed.ownerUserID).Scan(&episodeIDs[index]); err != nil {
			t.Fatalf("insert script episode %d: %v", index+1, err)
		}
	}

	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotIDs := []string{
		insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started"),
		insertShotProductionShot(t, seed, workflowRunID, 1, "", "", "not_started", "not_started"),
	}
	for index, shotID := range shotIDs {
		if _, err := seed.pool.Exec(seed.ctx, `
			UPDATE storyboard_shots
			SET script_episode_id = $2, episode_index = $3, episode_shot_index = 0
			WHERE id = $1
		`, shotID, episodeIDs[index], index+1); err != nil {
			t.Fatalf("scope shot %d to episode: %v", index+1, err)
		}
	}

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":          "generate_missing_images",
		"scriptEpisodeId": episodeIDs[0],
	}, &response)
	dispatchWorkflowStartsForTest(t, server)
	assertStringSet(t, response.TargetShotIDs, []string{shotIDs[0]})
	if temporal.workflow == nil {
		t.Fatal("episode-scoped workflow was not started")
	}
	input := temporal.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		ScriptEpisodeID string   `json:"scriptEpisodeId"`
		ShotIDs         []string `json:"shotIds"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	if options.ScriptEpisodeID != episodeIDs[0] {
		t.Fatalf("scriptEpisodeId = %q, want %q", options.ScriptEpisodeID, episodeIDs[0])
	}
	assertStringSet(t, options.ShotIDs, []string{shotIDs[0]})
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
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE storyboard_shots SET video_prompt_status = 'succeeded' WHERE id = $1`, withImageID); err != nil {
		t.Fatalf("mark video prompt ready: %v", err)
	}

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":        "generate_missing_videos",
		"workflowRunId": workflowRunID,
	}, &response)
	dispatchWorkflowStartsForTest(t, server)
	assertStringSet(t, response.TargetShotIDs, []string{withImageID})
	if response.WorkflowType != "batch_generate_shot_videos" {
		t.Fatalf("response = %+v", response)
	}
	input := temporal.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		MaxConcurrency int `json:"maxConcurrency"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	if options.MaxConcurrency != workflows.DefaultShotVideoConcurrency {
		t.Fatalf("maxConcurrency = %d, want %d", options.MaxConcurrency, workflows.DefaultShotVideoConcurrency)
	}
}

func TestShotVideoBatchEndpointStartsEpisodeCheckpointWorkflow(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporalClient := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporalClient
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/shot.png", "image/png")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, imageArtifactID, "", "succeeded", "not_started")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE storyboard_shots SET video_prompt_status = 'succeeded' WHERE id = $1`, shotID); err != nil {
		t.Fatalf("mark video prompt ready: %v", err)
	}

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-videos/generate-batch", seed.ownerToken, seed.organizationID, map[string]any{
		"shotIds": []string{shotID}, "maxConcurrency": 3,
	}, &response)
	dispatchWorkflowStartsForTest(t, server)

	if response.WorkflowType != "batch_generate_shot_videos" || len(response.TargetShotIDs) != 1 || response.TargetShotIDs[0] != shotID {
		t.Fatalf("response = %+v", response)
	}
	if temporalClient.workflow == nil || reflect.ValueOf(temporalClient.workflow).Pointer() != reflect.ValueOf(workflows.EpisodeBatchGenerateShotVideosWorkflow).Pointer() {
		t.Fatalf("shot video batch started unexpected workflow %T", temporalClient.workflow)
	}
	input := temporalClient.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		MaxConcurrency int `json:"maxConcurrency"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	if options.MaxConcurrency != 3 {
		t.Fatalf("maxConcurrency = %d, want 3", options.MaxConcurrency)
	}
}

func TestVideoPromptBatchEndpointRemainsSeparateFromVideoExecution(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporalClient := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporalClient
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/video-prompts/generate-batch", seed.ownerToken, seed.organizationID, map[string]any{
		"shotIds": []string{shotID},
	}, &response)
	dispatchWorkflowStartsForTest(t, server)

	if response.WorkflowType != "batch_generate_shot_video_prompts" {
		t.Fatalf("response = %+v", response)
	}
	if temporalClient.workflow == nil || reflect.ValueOf(temporalClient.workflow).Pointer() != reflect.ValueOf(workflows.BatchGenerateShotVideoPromptsWorkflow).Pointer() {
		t.Fatalf("video prompt batch started unexpected workflow %T", temporalClient.workflow)
	}
}

func TestShotProductionGenerateSelectedVideoPromptDoesNotRequireImage(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")
	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":  "generate_selected_video_prompts",
		"shotIds": []string{shotID},
	}, &response)
	dispatchWorkflowStartsForTest(t, server)
	assertStringSet(t, response.TargetShotIDs, []string{shotID})
	if response.WorkflowType != "batch_generate_shot_video_prompts" || temporal.workflow == nil {
		t.Fatalf("response=%+v temporal=%+v", response, temporal)
	}
	input := temporal.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		MaxConcurrency int `json:"maxConcurrency"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	if options.MaxConcurrency != workflows.DefaultShotVideoPromptConcurrency {
		t.Fatalf("maxConcurrency = %d, want %d", options.MaxConcurrency, workflows.DefaultShotVideoPromptConcurrency)
	}
	var promptStatus, videoStatus, promptWorkflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT video_prompt_status, video_status, COALESCE(video_prompt_workflow_run_id::text, '')
		FROM storyboard_shots
		WHERE id = $1
	`, shotID).Scan(&promptStatus, &videoStatus, &promptWorkflowRunID); err != nil {
		t.Fatalf("select queued prompt state: %v", err)
	}
	if promptStatus != "queued" || videoStatus != "not_started" || promptWorkflowRunID != response.WorkflowRunID {
		t.Fatalf("prompt state = prompt:%s video:%s workflow:%s", promptStatus, videoStatus, promptWorkflowRunID)
	}
}

func TestShotProductionGenerateSelectedImagePromptQueuesPromptOnly(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := insertShotProductionShot(t, seed, workflowRunID, 0, "", "", "not_started", "not_started")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE storyboard_shots SET image_prompt_status = 'not_started' WHERE id = $1`, shotID); err != nil {
		t.Fatalf("reset image prompt status: %v", err)
	}

	var response ShotProductionActionResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action":  "generate_selected_image_prompts",
		"shotIds": []string{shotID},
	}, &response)
	dispatchWorkflowStartsForTest(t, server)
	if response.WorkflowType != "batch_generate_shot_image_prompts" || temporal.workflow == nil {
		t.Fatalf("response=%+v temporal=%+v", response, temporal)
	}
	input := temporal.args[0].(workflows.TextToStoryboardInput)
	var options struct {
		MaxConcurrency int `json:"maxConcurrency"`
	}
	if err := json.Unmarshal(input.Input, &options); err != nil {
		t.Fatalf("decode workflow input: %v", err)
	}
	if options.MaxConcurrency != workflows.DefaultShotImagePromptConcurrency {
		t.Fatalf("maxConcurrency = %d, want %d", options.MaxConcurrency, workflows.DefaultShotImagePromptConcurrency)
	}
	var promptStatus, imageStatus, promptWorkflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT image_prompt_status, image_status, COALESCE(image_prompt_workflow_run_id::text, '')
		FROM storyboard_shots
		WHERE id = $1
	`, shotID).Scan(&promptStatus, &imageStatus, &promptWorkflowRunID); err != nil {
		t.Fatalf("select queued prompt state: %v", err)
	}
	if promptStatus != "queued" || imageStatus != "not_started" || promptWorkflowRunID != response.WorkflowRunID {
		t.Fatalf("prompt state = prompt:%s image:%s workflow:%s", promptStatus, imageStatus, promptWorkflowRunID)
	}
	var totalItems int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT total_items FROM workflow_runs WHERE id = $1`, response.WorkflowRunID).Scan(&totalItems); err != nil {
		t.Fatalf("select workflow item total: %v", err)
	}
	if totalItems != 1 {
		t.Fatalf("workflow total_items = %d, want 1", totalItems)
	}
}

func TestSelectShotProductionTargetsRequiresReadyImagePromptForImageGeneration(t *testing.T) {
	shots := []ShotProductionShot{{
		ID:                     "shot-1",
		ImagePromptStatus:      "not_started",
		ImageStatus:            "not_started",
		CanGenerateImage:       false,
		CanGenerateImagePrompt: true,
	}}
	if _, code := selectShotProductionTargets(ShotProductionActionRequest{Action: "generate_selected_images", ShotIDs: []string{"shot-1"}}, shots); code != "SHOT_IMAGE_PROMPT_REQUIRED" {
		t.Fatalf("error code = %q", code)
	}
	targets, code := selectShotProductionTargets(ShotProductionActionRequest{Action: "generate_selected_image_prompts", ShotIDs: []string{"shot-1"}}, shots)
	if code != "" || len(targets) != 1 || targets[0] != "shot-1" {
		t.Fatalf("targets=%v code=%q", targets, code)
	}
}

func TestSelectShotProductionTargetsGenerateVideoPromptsSkipsReadyAndRunning(t *testing.T) {
	shots := []ShotProductionShot{
		{ID: "missing", VideoPromptStatus: "not_started", CanGenerateVideoPrompt: true},
		{ID: "failed", VideoPromptStatus: "failed", CanGenerateVideoPrompt: true},
		{ID: "ready", VideoPromptStatus: "succeeded", CanGenerateVideoPrompt: true},
		{ID: "running", VideoPromptStatus: "running", CanGenerateVideoPrompt: false},
	}
	targets, code := selectShotProductionTargets(ShotProductionActionRequest{Action: "generate_video_prompts"}, shots)
	if code != "" {
		t.Fatalf("error code = %q", code)
	}
	assertStringSet(t, targets, []string{"missing", "failed"})
	forcedTargets, code := selectShotProductionTargets(ShotProductionActionRequest{
		Action: "generate_video_prompts", Options: map[string]any{"force": true},
	}, shots)
	if code != "" {
		t.Fatalf("forced error code = %q", code)
	}
	assertStringSet(t, forcedTargets, []string{"missing", "failed", "ready"})
}

func TestSelectShotProductionTargetsRequiresReadyVideoPromptForVideoGeneration(t *testing.T) {
	imageArtifactID := "image-artifact"
	shots := []ShotProductionShot{{
		ID: "shot-1", VideoPromptStatus: "not_started", VideoStatus: "not_started",
		ImageArtifactID: &imageArtifactID,
	}}
	if _, code := selectShotProductionTargets(ShotProductionActionRequest{Action: "generate_selected_videos", ShotIDs: []string{"shot-1"}}, shots); code != "SHOT_VIDEO_PROMPT_REQUIRED" {
		t.Fatalf("error code = %q", code)
	}
	if targets, code := selectShotProductionTargets(ShotProductionActionRequest{Action: "generate_missing_videos"}, shots); code != "NO_TARGET_SHOTS" || len(targets) != 0 {
		t.Fatalf("targets=%v code=%q", targets, code)
	}
	shots[0].VideoPromptStatus = "succeeded"
	if targets, code := selectShotProductionTargets(ShotProductionActionRequest{Action: "generate_missing_videos"}, shots); code != "" || len(targets) != 1 {
		t.Fatalf("ready targets=%v code=%q", targets, code)
	}
}

func TestShotProductionScopeFiltersTreatExplicitShotIDsAsAuthoritative(t *testing.T) {
	sceneID, workflowRunID, episodeID := shotProductionScopeFilters(ShotProductionActionRequest{
		ScriptSceneID: "stale-scene", ScriptEpisodeID: "hallucinated-episode", WorkflowRunID: "old-workflow",
		ShotIDs: []string{"shot-1", "shot-2"},
	})
	if sceneID != "" || workflowRunID != "" || episodeID != "" {
		t.Fatalf("filters = scene:%q workflow:%q episode:%q, want empty explicit-shot scope", sceneID, workflowRunID, episodeID)
	}

	sceneID, workflowRunID, episodeID = shotProductionScopeFilters(ShotProductionActionRequest{
		ScriptSceneID: " scene ", ScriptEpisodeID: " episode ", WorkflowRunID: " workflow ",
	})
	if sceneID != "scene" || workflowRunID != "workflow" || episodeID != "episode" {
		t.Fatalf("filters = scene:%q workflow:%q episode:%q", sceneID, workflowRunID, episodeID)
	}
}

func TestShotProductionTargetEpisodeIDDerivesValidatedShotScope(t *testing.T) {
	episodeOne := "episode-1"
	episodeTwo := "episode-2"
	shots := []ShotProductionShot{
		{ID: "shot-1", ScriptEpisodeID: &episodeOne},
		{ID: "shot-2", ScriptEpisodeID: &episodeOne},
		{ID: "shot-3", ScriptEpisodeID: &episodeTwo},
	}
	req := ShotProductionActionRequest{ScriptEpisodeID: "hallucinated-episode", ShotIDs: []string{"shot-1", "shot-2"}}
	if got := shotProductionTargetEpisodeID(req, shots, []string{"shot-1", "shot-2"}); got != episodeOne {
		t.Fatalf("episode = %q, want %q", got, episodeOne)
	}
	if got := shotProductionTargetEpisodeID(req, shots, []string{"shot-1", "shot-3"}); got != "" {
		t.Fatalf("mixed episode = %q, want empty", got)
	}
}

func TestShotProductionActionPermission(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-production/actions", seed.otherToken, seed.organizationID, map[string]any{
		"action": "generate_missing_images",
	}, http.StatusForbidden, "ACCESS_DENIED")
}

func TestShotProductionMaxConcurrencyDefaultsAndClamps(t *testing.T) {
	if got := shotProductionMaxConcurrency("generate_image_prompts", nil); got != workflows.DefaultShotImagePromptConcurrency {
		t.Fatalf("image prompt default = %d, want %d", got, workflows.DefaultShotImagePromptConcurrency)
	}
	if got := shotProductionMaxConcurrency("generate_selected_image_prompts", map[string]any{"maxConcurrency": 99}); got != workflows.MaxShotImagePromptConcurrency {
		t.Fatalf("image prompt clamp = %d, want %d", got, workflows.MaxShotImagePromptConcurrency)
	}
	if got := shotProductionMaxConcurrency("generate_missing_images", nil); got != workflows.DefaultShotImageConcurrency {
		t.Fatalf("image default = %d, want %d", got, workflows.DefaultShotImageConcurrency)
	}
	if got := shotProductionMaxConcurrency("generate_missing_images", map[string]any{"maxConcurrency": 99}); got != workflows.MaxShotImageConcurrency {
		t.Fatalf("image clamp = %d, want %d", got, workflows.MaxShotImageConcurrency)
	}
	if got := shotProductionMaxConcurrency("generate_missing_videos", nil); got != workflows.DefaultShotVideoConcurrency {
		t.Fatalf("video default = %d, want %d", got, workflows.DefaultShotVideoConcurrency)
	}
	if workflows.DefaultShotVideoConcurrency != 5 {
		t.Fatalf("video default concurrency = %d, want 5", workflows.DefaultShotVideoConcurrency)
	}
	if got := shotProductionMaxConcurrency("generate_video_prompts", nil); got != workflows.DefaultShotVideoPromptConcurrency {
		t.Fatalf("video prompt default = %d, want %d", got, workflows.DefaultShotVideoPromptConcurrency)
	}
	if got := shotProductionMaxConcurrency("generate_selected_video_prompts", map[string]any{"maxConcurrency": 99}); got != workflows.MaxShotVideoPromptConcurrency {
		t.Fatalf("video prompt clamp = %d, want %d", got, workflows.MaxShotVideoPromptConcurrency)
	}
}

func insertShotProductionShot(t *testing.T, seed *artifactPreviewSeed, workflowRunID string, index int, imageArtifactID, videoArtifactID, imageStatus, videoStatus string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			image_prompt_status, image_artifact_id, video_artifact_id, image_status, video_status, status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $11, $12, 450000, 450000, $6, 'slow push', 'mist drifting', 'hopeful',
		        'image prompt', 'video prompt', 'succeeded', NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, $9, $10, 'pending', 'pending', '{}')
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID, index, index+1, "Shot visual", imageArtifactID, videoArtifactID, imageStatus, videoStatus,
		int64(index)*450000, int64(index+1)*450000).Scan(&id); err != nil {
		t.Fatalf("insert shot production shot: %v", err)
	}
	if videoArtifactID != "" {
		storageKey := fmt.Sprintf("org/project/shot-%d.mp4", index)
		mediaFileID := seed.insertMediaFile(t, videoArtifactID, storageKey, "video/mp4")
		if _, err := seed.pool.Exec(seed.ctx, `
			UPDATE storyboard_shots
			SET video_media_file_id = $2, video_storage_key = $3
			WHERE id = $1
		`, id, mediaFileID, storageKey); err != nil {
			t.Fatalf("attach shot video media: %v", err)
		}
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
