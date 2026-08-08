package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/httpx"
)

func TestTimelineCreateFromStoryboardAndReorder(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, server, seed, "Timeline Create Project")

	workflowRunID := insertTimelineWorkflowRun(t, seed, "succeeded")
	videoA := seed.insertArtifact(t, "generated_video", "org/project/shot-a.mp4", "video/mp4")
	videoB := seed.insertArtifact(t, "generated_video", "org/project/shot-b.mp4", "video/mp4")
	insertTimelineStoryboardShot(t, seed, workflowRunID, 0, videoA)
	insertTimelineStoryboardShot(t, seed, workflowRunID, 1, videoB)

	var timeline ProjectTimeline
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/timelines", seed.ownerToken, seed.organizationID, map[string]any{
		"title":               "主时间线",
		"fromStoryboardShots": true,
	}, &timeline, map[string]string{"Idempotency-Key": "timeline-create-from-storyboard"})
	if timeline.ID == "" || timeline.Title != "主时间线" || timeline.Revision != 1 {
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
		"expectedTimelineRevision": timeline.Revision,
		"items": []map[string]any{
			{"clipId": listed.Items[0].ID, "expectedRevision": listed.Items[0].Revision, "clipIndex": 1},
			{"clipId": listed.Items[1].ID, "expectedRevision": listed.Items[1].Revision, "clipIndex": 0},
		},
	}, &struct{}{}, map[string]string{"Idempotency-Key": "timeline-reorder-clips"})

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
	configureTimelineTestProject(t, server, seed, "Final Video Activation Project")

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/timelines", seed.otherToken, seed.organizationID, map[string]any{
		"title": "blocked",
	}, http.StatusForbidden, "ACCESS_DENIED")

	timelineID := insertProjectTimeline(t, seed)
	first := insertFinalVideoVersion(t, seed, timelineID, 1, "active")
	second := insertFinalVideoVersion(t, seed, timelineID, 2, "ready")
	blocked := insertFinalVideoVersion(t, seed, timelineID, 3, "ready")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE final_video_versions SET native_audio_status = 'audio_unverified', production_readiness = 'preview_only' WHERE id = $1`, blocked); err != nil {
		t.Fatalf("mark preview-only final video: %v", err)
	}
	assertAPIErrorCodeWithHeaders(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/final-videos/"+blocked+"/activate", seed.ownerToken, seed.organizationID,
		map[string]any{"expectedRevision": finalVideoRevision(t, seed, blocked)},
		map[string]string{"Idempotency-Key": "final-video-activate-blocked"}, http.StatusConflict, "AUDIO_VERIFICATION_REQUIRED")

	var activated FinalVideoVersion
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/final-videos/"+second+"/activate", seed.ownerToken, seed.organizationID,
		map[string]any{"expectedRevision": finalVideoRevision(t, seed, second)}, &activated,
		map[string]string{"Idempotency-Key": "final-video-activate-second"})
	if activated.ID != second || activated.Status != "active" || activated.Revision < 2 {
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

	assertAPIErrorCodeWithHeaders(t, server, http.MethodDelete, "/api/projects/"+seed.projectID+"/final-videos/"+second, seed.ownerToken, seed.organizationID,
		map[string]any{"expectedRevision": activated.Revision},
		map[string]string{"Idempotency-Key": "final-video-delete-unconfirmed"}, http.StatusUnprocessableEntity, "ACTIVE_FINAL_VIDEO_REQUIRES_CONFIRMATION")
	doAPISuccess(t, server, http.MethodDelete, "/api/projects/"+seed.projectID+"/final-videos/"+second, seed.ownerToken, seed.organizationID,
		map[string]any{"expectedRevision": activated.Revision, "confirmActive": true}, &struct{}{},
		map[string]string{"Idempotency-Key": "final-video-delete-confirmed"})
	var activeAfterDelete sql.NullString
	if err := seed.pool.QueryRow(seed.ctx, `SELECT active_final_video_version_id::text FROM projects WHERE id = $1`, seed.projectID).Scan(&activeAfterDelete); err != nil {
		t.Fatalf("select active project version after delete: %v", err)
	}
	if activeAfterDelete.Valid {
		t.Fatalf("active version after confirmed delete = %v, want nil", activeAfterDelete.String)
	}
}

func finalVideoRevision(t *testing.T, seed *artifactPreviewSeed, versionID string) int64 {
	t.Helper()
	var revision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM final_video_versions WHERE id = $1`, versionID).Scan(&revision); err != nil {
		t.Fatalf("read final video revision: %v", err)
	}
	return revision
}

func assertAPIErrorCodeWithHeaders(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	token string,
	orgID string,
	body any,
	headers map[string]string,
	status int,
	code string,
) {
	t.Helper()
	recorder := doAPIRequest(t, handler, method, path, token, orgID, body, headers)
	if recorder.Code != status {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	var envelope httpx.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != code {
		t.Fatalf("error code = %#v, want %s", envelope.Error, code)
	}
}

func TestComposeTimelineRequiresCompletedShotVideos(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, server, seed, "Timeline Compose Project")

	workflowRunID := insertTimelineWorkflowRun(t, seed, "succeeded")
	timelineID := insertProjectTimeline(t, seed)
	insertTimelineStoryboardShotWithoutVideo(t, seed, workflowRunID)

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/timelines/"+timelineID+"/compose", seed.ownerToken, seed.organizationID, map[string]any{
		"title": "blocked",
	}, http.StatusUnprocessableEntity, "SHOT_VIDEOS_REQUIRED")
}

func insertTimelineStoryboardShot(t *testing.T, seed *artifactPreviewSeed, workflowRunID string, shotIndex int, videoArtifactID string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, production_generation_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			video_artifact_id, status, video_status, review_status, metadata
		)
		SELECT $1, $2, $3, run.production_generation_id, $4, $5, $6, $7, 450000, 450000,
		       $8, 'slow push', 'mist drifting', 'hopeful', 'image prompt', 'video prompt',
		       $9, 'video_succeeded', 'succeeded', 'approved', '{}'
		FROM workflow_runs run
		WHERE run.id = $3 AND run.project_id = $2
		RETURNING id
	`, seed.organizationID, seed.projectID, workflowRunID, shotIndex, shotIndex+1,
		int64(shotIndex)*450000, int64(shotIndex+1)*450000, "Shot visual", videoArtifactID).Scan(&id); err != nil {
		t.Fatalf("insert timeline storyboard shot: %v", err)
	}
	return id
}

func insertTimelineStoryboardShotWithoutVideo(t *testing.T, seed *artifactPreviewSeed, workflowRunID string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, production_generation_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, video_status, review_status, metadata
		)
		SELECT $1, $2, $3, run.production_generation_id, 0, 1, 0, 450000, 450000, 450000,
		       'Shot without video', 'slow push', 'mist drifting', 'hopeful', 'image prompt', 'video prompt',
		       'storyboard_ready', 'not_started', 'approved', '{}'
		FROM workflow_runs run
		WHERE run.id = $3 AND run.project_id = $2
		RETURNING id
	`, seed.organizationID, seed.projectID, workflowRunID).Scan(&id); err != nil {
		t.Fatalf("insert timeline storyboard shot without video: %v", err)
	}
	return id
}

func insertTimelineWorkflowRun(t *testing.T, seed *artifactPreviewSeed, status string) string {
	t.Helper()
	var workflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, status, input, output, created_by,
			production_generation_id, video_production_binding_id, video_production_binding_revision
		)
		SELECT $1, $2, $3, $4, '{}', '{}', $5,
		       generation.id, binding.id, binding.revision
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id
		 AND generation.project_id = project.id
		JOIN project_video_production_bindings binding
		  ON binding.id = generation.binding_id
		 AND binding.project_id = project.id
		WHERE project.id = $2
		RETURNING id::text
	`, seed.organizationID, seed.projectID, "timeline-test-"+status+"-"+randomStorageSegment(), status, seed.ownerUserID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert timeline workflow run: %v", err)
	}
	return workflowRunID
}

func insertProjectTimeline(t *testing.T, seed *artifactPreviewSeed) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO project_timelines(
			organization_id, project_id, production_generation_id,
			title, status, aspect_ratio, resolution, metadata, created_by
		)
		SELECT $1, $2, project.active_video_production_generation_id,
		       'Test Timeline', 'active', '16:9', '720p', '{}', $3
		FROM projects project
		WHERE project.id = $2
		RETURNING id::text
	`, seed.organizationID, seed.projectID, seed.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert project timeline: %v", err)
	}
	return id
}

func configureTimelineTestProject(t *testing.T, handler http.Handler, seed *artifactPreviewSeed, name string) Project {
	t.Helper()
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", seed.ownerToken, seed.organizationID, map[string]any{
		"workspaceId": seed.workspaceID,
		"name":        name,
	}, &project)
	if project.ProductionGeneration == nil || project.VideoProductionBinding == nil {
		t.Fatalf("configured project is missing production identity: %+v", project)
	}
	seed.projectID = project.ID
	return project
}

func insertFinalVideoVersion(t *testing.T, seed *artifactPreviewSeed, timelineID string, version int, status string) string {
	t.Helper()
	var id string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO final_video_versions(
			organization_id, project_id, production_generation_id, timeline_id, version, title, status,
			resolution, aspect_ratio, compose_settings, metadata, created_by
		)
		SELECT $1, $2, project.active_video_production_generation_id, $3, $4, $5, $6,
		       '720p', '16:9', '{}', '{}', $7
		FROM projects project
		WHERE project.id = $2
		RETURNING id::text
	`, seed.organizationID, seed.projectID, timelineID, version, "Version", status, seed.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert final video version: %v", err)
	}
	return id
}
