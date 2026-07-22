package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStoryboardShotImageReferenceOptionsAutoPrefersCanonicalPrimaryImage(t *testing.T) {
	primaryArtifactID := "primary-artifact"
	historicalArtifactID := "historical-artifact"
	asset := &CanonicalAsset{
		ID:                         "leader",
		Name:                       "正道首领",
		PrimaryReferenceArtifactID: &primaryArtifactID,
		References: []AssetReference{{
			ID:         "historical-reference",
			ArtifactID: &historicalArtifactID,
			IsPrimary:  true,
			Status:     "active",
		}},
	}
	requirements := []StoryboardShotRequirementDetail{{ShotAssetRequirement: ShotAssetRequirement{
		ID:      "leader-requirement",
		AssetID: asset.ID,
		Asset:   asset,
	}}}
	request := httptest.NewRequest(http.MethodGet, "http://cineweave.test/storyboard-shot", nil)
	options := (&Server{}).storyboardShotImageReferenceOptions(request, StoryboardShot{ImageReferenceMode: "auto"}, requirements)

	if len(options) != 1 {
		t.Fatalf("reference options = %+v, want only the current primary image", options)
	}
	selected := ""
	for _, option := range options {
		if option.SourceType == "asset_reference" {
			t.Fatalf("historical asset reference leaked into shot options: %+v", option)
		}
		if option.Selected {
			selected = option.Key
		}
	}
	if selected != "asset_primary:leader" {
		t.Fatalf("selected reference = %q, options = %+v", selected, options)
	}
}

func TestStoryboardShotImageReferenceOptionsAllowsOtherCurrentProjectAsset(t *testing.T) {
	linkedArtifactID := "linked-artifact"
	otherArtifactID := "other-artifact"
	linkedAsset := &CanonicalAsset{
		ID:                         "leader",
		AssetType:                  "character",
		Name:                       "正道首领",
		PrimaryReferenceArtifactID: &linkedArtifactID,
	}
	requirements := []StoryboardShotRequirementDetail{{ShotAssetRequirement: ShotAssetRequirement{
		ID:      "leader-requirement",
		AssetID: linkedAsset.ID,
		Asset:   linkedAsset,
	}}}
	other := StoryboardShotImageReferenceOption{
		Key:        "asset_primary:other",
		SourceType: "asset_primary",
		SourceID:   "other",
		AssetID:    "other",
		AssetType:  "prop",
		AssetName:  "镇魂铃",
		Title:      "镇魂铃 · 当前主图",
		ArtifactID: &otherArtifactID,
	}
	request := httptest.NewRequest(http.MethodGet, "http://cineweave.test/storyboard-shot", nil)
	options := (&Server{}).storyboardShotImageReferenceOptions(request, StoryboardShot{
		ImageReferenceMode: "custom",
		ImageReferenceKeys: []string{other.Key},
	}, requirements, other)

	if len(options) != 2 {
		t.Fatalf("reference options = %+v, want linked and other current assets", options)
	}
	for _, option := range options {
		if option.Key == other.Key {
			if !option.Selected || option.AutoSelected || option.IsShotAsset {
				t.Fatalf("other project asset option = %+v", option)
			}
			return
		}
	}
	t.Fatalf("other project asset was not returned: %+v", options)
}

func TestNormalizeShotProductionStatusKeepsExplicitMediaState(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		staleState  string
		hasArtifact bool
		want        string
	}{
		{
			name:        "new image succeeds while existing video is stale",
			current:     "succeeded",
			staleState:  "needs_regeneration",
			hasArtifact: true,
			want:        "succeeded",
		},
		{
			name:        "invalidated video remains stale",
			current:     "stale",
			staleState:  "needs_regeneration",
			hasArtifact: true,
			want:        "stale",
		},
		{
			name:        "explicit image stale remains stale",
			current:     "stale",
			staleState:  "needs_regeneration",
			hasArtifact: true,
			want:        "stale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeShotProductionStatus(tt.current, tt.staleState, tt.hasArtifact)
			if got != tt.want {
				t.Fatalf("normalizeShotProductionStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

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

func TestStoryboardPlanDetailHidesInternalSparseShotNumbers(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/numbering.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/numbering.mp4", "video/mp4")
	firstID := seed.insertStoryboardShot(t, workflowRunID, imageArtifactID, videoArtifactID)
	secondID := seed.insertStoryboardShot(t, workflowRunID, imageArtifactID, videoArtifactID)
	planID := seed.attachStoryboardPlan(t, scriptID, versionID, firstID)

	var episodeID string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT script_episode_id::text FROM storyboard_plans WHERE id = $1`, planID).Scan(&episodeID); err != nil {
		t.Fatalf("read storyboard episode: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET storyboard_plan_id = $3,
		    script_episode_id = $4,
		    shot_index = CASE id WHEN $1::uuid THEN 73 ELSE 20000 END,
		    shot_no = CASE id WHEN $1::uuid THEN 74 ELSE 20001 END,
		    episode_shot_index = CASE id WHEN $1::uuid THEN 73 ELSE 20000 END,
		    start_tick = CASE id WHEN $1::uuid THEN 0 ELSE 90000 END,
		    end_tick = CASE id WHEN $1::uuid THEN 90000 ELSE 180000 END,
		    duration_min_ticks = 90000,
		    duration_max_ticks = 90000
		WHERE id IN ($1::uuid, $2::uuid)
	`, firstID, secondID, planID, episodeID); err != nil {
		t.Fatalf("seed sparse storyboard numbering: %v", err)
	}

	var plan StoryboardPlan
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-plans/"+planID, seed.ownerToken, seed.organizationID, nil, &plan)
	if len(plan.Shots) != 2 {
		t.Fatalf("plan shots = %+v", plan.Shots)
	}
	for index, shot := range plan.Shots {
		if shot.ShotIndex != index || shot.ShotNo != index+1 || shot.EpisodeShotIndex == nil || *shot.EpisodeShotIndex != index {
			t.Fatalf("public shot %d numbering = index:%d no:%d episode:%v", index, shot.ShotIndex, shot.ShotNo, shot.EpisodeShotIndex)
		}
	}
}

func TestStoryboardWorkbenchAPIs(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET video_ratio = '9:16' WHERE id = $1`, seed.projectID); err != nil {
		t.Fatalf("set project video ratio: %v", err)
	}

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	sceneID := seed.insertScriptScene(t, scriptID, versionID, 1, "approved", "fresh")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/workbench-shot.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/workbench-shot.mp4", "video/mp4")
	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", imageArtifactID)
	otherAssetID := seed.insertCanonicalAsset(t, "prop", "Signal Bell", "approved", imageArtifactID)

	var first StoryboardShot
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots", seed.ownerToken, seed.organizationID, map[string]any{
		"workflowRunId":        workflowRunID,
		"scriptSceneId":        sceneID,
		"shotNo":               1,
		"plannedDurationTicks": 270000,
		"visual":               "Manual first shot",
		"camera":               "push",
		"motion":               "mist moves",
		"mood":                 "quiet",
		"imagePrompt":          "manual image",
		"videoPrompt":          "manual video",
	}, &first)
	if first.WorkflowRunID != workflowRunID || first.ScriptSceneID == nil || *first.ScriptSceneID != sceneID || !first.ManualOverride || first.StaleState != "needs_regeneration" {
		t.Fatalf("created first shot = %+v", first)
	}
	var second StoryboardShot
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots", seed.ownerToken, seed.organizationID, map[string]any{
		"workflowRunId":        workflowRunID,
		"scriptSceneId":        sceneID,
		"shotNo":               2,
		"plannedDurationTicks": 360000,
		"visual":               "Manual second shot",
	}, &second)

	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/reorder", seed.ownerToken, seed.organizationID, map[string]any{
		"items": []map[string]any{
			{"shotId": second.ID, "shotIndex": 0, "shotNo": 1},
			{"shotId": first.ID, "shotIndex": 1, "shotNo": 2},
		},
	}, &struct{}{})
	assertStoryboardShotPosition(t, seed, second.ID, 0, 1)
	assertStoryboardShotPosition(t, seed, first.ID, 1, 2)

	planID := seed.attachStoryboardPlan(t, scriptID, versionID, first.ID)
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+first.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"plannedDurationTicks": 180000,
	}, http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED")
	assertAPIErrorCode(t, server, http.MethodDelete, "/api/projects/"+seed.projectID+"/storyboard-shots/"+first.ID, seed.ownerToken, seed.organizationID, nil, http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/reorder", seed.ownerToken, seed.organizationID, map[string]any{
		"items": []map[string]any{
			{"shotId": first.ID, "shotIndex": 0, "shotNo": 1},
			{"shotId": second.ID, "shotIndex": 1, "shotNo": 2},
		},
	}, http.StatusConflict, "STORYBOARD_PLAN_REVISION_REQUIRED")
	assertStoryboardShotPosition(t, seed, second.ID, 0, 1)
	assertStoryboardShotPosition(t, seed, first.ID, 1, 2)
	var activePlan bool
	if err := seed.pool.QueryRow(seed.ctx, `SELECT active FROM storyboard_plans WHERE id = $1`, planID).Scan(&activePlan); err != nil {
		t.Fatalf("read protected storyboard plan: %v", err)
	}
	if !activePlan {
		t.Fatal("protected storyboard plan was unexpectedly deactivated")
	}

	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET image_artifact_id = $2, image_storage_key = 'org/project/workbench-shot.png',
		    video_artifact_id = $3, video_storage_key = 'org/project/workbench-shot.mp4',
		    image_status = 'succeeded', video_status = 'succeeded', stale_state = 'fresh'
		WHERE id = $1
	`, second.ID, imageArtifactID, videoArtifactID); err != nil {
		t.Fatalf("attach media to shot: %v", err)
	}
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, second.ID, assetID, "approved", imageArtifactID)
	var detail StoryboardShotDetail
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID+"/detail?previewExpiresSeconds=900", seed.ownerToken, seed.organizationID, nil, &detail)
	if detail.AspectRatio != "9:16" || detail.Shot.ID != second.ID || detail.ScriptScene == nil || detail.ScriptScene.ID != sceneID || detail.ImagePreviewURL == nil || detail.VideoPreviewURL == nil {
		t.Fatalf("detail media/scene = %+v", detail)
	}
	if len(detail.Requirements) != 1 || detail.Requirements[0].ID != requirementID || detail.Requirements[0].Asset == nil || detail.Requirements[0].DerivedPreviewURL == nil {
		t.Fatalf("detail requirements = %+v", detail.Requirements)
	}
	if len(detail.ImageReferenceOptions) != 3 {
		t.Fatalf("image reference options = %+v", detail.ImageReferenceOptions)
	}
	var primaryReferenceKey string
	var otherReferenceKey string
	for _, option := range detail.ImageReferenceOptions {
		if option.SourceType == "derived_asset" && (!option.AutoSelected || !option.Selected) {
			t.Fatalf("derived reference should be selected in auto mode: %+v", option)
		}
		if option.SourceType == "asset_primary" {
			if option.AssetID == assetID {
				primaryReferenceKey = option.Key
				if !option.IsShotAsset {
					t.Fatalf("linked asset must be marked as a shot asset: %+v", option)
				}
			} else if option.AssetID == otherAssetID {
				otherReferenceKey = option.Key
				if option.IsShotAsset || option.AutoSelected || option.Selected {
					t.Fatalf("other project asset must only be manually selectable: %+v", option)
				}
			}
		}
	}
	if primaryReferenceKey == "" || otherReferenceKey == "" {
		t.Fatalf("primary reference missing: %+v", detail.ImageReferenceOptions)
	}
	if len(detail.VideoReferenceOptions) != 4 {
		t.Fatalf("video reference options = %+v", detail.VideoReferenceOptions)
	}
	var shotImageReferenceKey string
	for _, option := range detail.VideoReferenceOptions {
		if option.SourceType == "shot_image" {
			shotImageReferenceKey = option.Key
			if !option.AutoSelected || !option.Selected || option.ReferenceType != "first_frame" {
				t.Fatalf("shot image video reference = %+v", option)
			}
		}
	}
	if shotImageReferenceKey == "" {
		t.Fatalf("shot image video reference missing: %+v", detail.VideoReferenceOptions)
	}
	var customVideoReferences StoryboardShot
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"videoReferenceMode": "custom",
		"videoReferenceKeys": []string{primaryReferenceKey},
		"videoPrompt":        "custom motion prompt",
	}, &customVideoReferences)
	if customVideoReferences.VideoReferenceMode != "custom" || len(customVideoReferences.VideoReferenceKeys) != 1 || customVideoReferences.VideoReferenceKeys[0] != primaryReferenceKey {
		t.Fatalf("custom video references = %+v", customVideoReferences)
	}
	if customVideoReferences.ImageStatus != "succeeded" || customVideoReferences.VideoStatus != "stale" {
		t.Fatalf("video-only update should preserve image status: %+v", customVideoReferences)
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID+"/detail", seed.ownerToken, seed.organizationID, nil, &detail)
	for _, option := range detail.VideoReferenceOptions {
		if option.Selected != (option.Key == primaryReferenceKey) {
			t.Fatalf("custom video selected option = %+v", option)
		}
	}
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"videoReferenceMode": "custom",
		"videoReferenceKeys": []string{"asset_reference:00000000-0000-0000-0000-000000000000"},
	}, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
	var customReferences StoryboardShot
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"imageReferenceMode": "custom",
		"imageReferenceKeys": []string{otherReferenceKey},
	}, &customReferences)
	if customReferences.ImageReferenceMode != "custom" || len(customReferences.ImageReferenceKeys) != 1 || customReferences.ImageReferenceKeys[0] != otherReferenceKey {
		t.Fatalf("custom image references = %+v", customReferences)
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID+"/detail", seed.ownerToken, seed.organizationID, nil, &detail)
	for _, option := range detail.ImageReferenceOptions {
		if option.Selected != (option.Key == otherReferenceKey) {
			t.Fatalf("custom selected option = %+v", option)
		}
	}
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"imageReferenceMode": "custom",
		"imageReferenceKeys": []string{"asset_reference:00000000-0000-0000-0000-000000000000"},
	}, http.StatusUnprocessableEntity, "VALIDATION_FAILED")

	var unlinkedVideo StoryboardShot
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID+"/media/unlink", seed.ownerToken, seed.organizationID, map[string]any{
		"kind": "video",
	}, &unlinkedVideo)
	if unlinkedVideo.VideoArtifactID != nil || unlinkedVideo.VideoMediaFileID != nil || unlinkedVideo.VideoStorageKey != nil || unlinkedVideo.VideoStatus != "not_started" || unlinkedVideo.StaleState != "needs_regeneration" {
		t.Fatalf("unlinked video shot = %+v", unlinkedVideo)
	}
	assertArtifactStillExists(t, seed, videoArtifactID)

	var unlinkedImage StoryboardShot
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID+"/media/unlink", seed.ownerToken, seed.organizationID, map[string]any{
		"kind": "image",
	}, &unlinkedImage)
	if unlinkedImage.ImageArtifactID != nil || unlinkedImage.ImageMediaFileID != nil || unlinkedImage.ImageStorageKey != nil || unlinkedImage.ImageStatus != "not_started" || unlinkedImage.StaleState != "needs_regeneration" {
		t.Fatalf("unlinked image shot = %+v", unlinkedImage)
	}
	assertArtifactStillExists(t, seed, imageArtifactID)

	var skipped ShotAssetRequirement
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/"+requirementID+"/skip", seed.ownerToken, seed.organizationID, nil, &skipped)
	if skipped.Status != "skipped" || skipped.ReviewStatus != "approved" || !skipped.ManualOverride || skipped.StaleState != "fresh" {
		t.Fatalf("skipped requirement = %+v", skipped)
	}
	assertShotAssetRequirementState(t, seed, requirementID, "skipped", "approved", "fresh", true)

	var updated StoryboardShot
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"visual": "Edited second shot",
	}, &updated)
	if updated.StaleState != "needs_regeneration" || !updated.ManualOverride || updated.Visual != "Edited second shot" {
		t.Fatalf("updated shot = %+v", updated)
	}

	if _, err := seed.pool.Exec(seed.ctx, `UPDATE storyboard_shots SET video_prompt_status = 'running' WHERE id = $1`, second.ID); err != nil {
		t.Fatalf("mark video prompt running: %v", err)
	}
	runningPromptResponse := doAPIRequest(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+second.ID, seed.ownerToken, seed.organizationID, map[string]any{
		"videoPrompt": "cannot change while running",
	})
	if runningPromptResponse.Code != http.StatusConflict {
		t.Fatalf("running video prompt update status = %d, want %d body=%s", runningPromptResponse.Code, http.StatusConflict, runningPromptResponse.Body.String())
	}
	var runningPromptEnvelope struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(runningPromptResponse.Body.Bytes(), &runningPromptEnvelope); err != nil {
		t.Fatalf("decode running video prompt error: %v", err)
	}
	if runningPromptEnvelope.Error == nil || runningPromptEnvelope.Error.Code != "SHOT_VIDEO_PROMPT_RUNNING" || runningPromptEnvelope.Error.Message != "镜头视频提示词正在生成，完成前不能修改视频提示词设置" {
		t.Fatalf("running video prompt error = %#v", runningPromptEnvelope.Error)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE storyboard_shots SET video_prompt_status = 'succeeded' WHERE id = $1`, second.ID); err != nil {
		t.Fatalf("reset video prompt status: %v", err)
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

func assertArtifactStillExists(t *testing.T, seed *artifactPreviewSeed, artifactID string) {
	t.Helper()
	var count int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT COUNT(*) FROM artifacts WHERE id = $1 AND project_id = $2`, artifactID, seed.projectID).Scan(&count); err != nil {
		t.Fatalf("count artifact: %v", err)
	}
	if count != 1 {
		t.Fatalf("artifact count = %d, want 1", count)
	}
}

func assertShotAssetRequirementState(t *testing.T, seed *artifactPreviewSeed, requirementID, wantStatus, wantReviewStatus, wantStaleState string, wantManualOverride bool) {
	t.Helper()
	var gotStatus, gotReviewStatus, gotStaleState string
	var gotManualOverride bool
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COALESCE(status, ''), COALESCE(review_status, ''), COALESCE(stale_state, ''), COALESCE(manual_override, false)
		FROM shot_asset_requirements
		WHERE id = $1 AND project_id = $2
	`, requirementID, seed.projectID).Scan(&gotStatus, &gotReviewStatus, &gotStaleState, &gotManualOverride); err != nil {
		t.Fatalf("read shot asset requirement state: %v", err)
	}
	if gotStatus != wantStatus || gotReviewStatus != wantReviewStatus || gotStaleState != wantStaleState || gotManualOverride != wantManualOverride {
		t.Fatalf("requirement state = (%s,%s,%s,%v), want (%s,%s,%s,%v)", gotStatus, gotReviewStatus, gotStaleState, gotManualOverride, wantStatus, wantReviewStatus, wantStaleState, wantManualOverride)
	}
}

func (s *artifactPreviewSeed) attachStoryboardPlan(t *testing.T, scriptID, versionID, shotID string) string {
	t.Helper()
	var episodeID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO script_episodes(
			organization_id, project_id, script_id, script_version_id,
			episode_index, episode_title, content, content_format, review_status, stale_state, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 1, '第 1 集', 'scene content', 'markdown', 'approved', 'fresh', '{}', $5)
		RETURNING id::text
	`, s.organizationID, s.projectID, scriptID, versionID, s.ownerUserID).Scan(&episodeID); err != nil {
		t.Fatalf("insert script episode for storyboard plan: %v", err)
	}
	var timingAnalysisID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO script_timing_analyses(
			organization_id, project_id, script_id, script_version_id, script_episode_id,
			revision, status, estimated_duration_ticks, minimum_duration_ticks, target_duration_ticks,
			timeline_timebase, fps_numerator, fps_denominator, method_version, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 1, 'ready', 450000, 450000, 450000, 90000, 24, 1, 'api-test', '{}', $6)
		RETURNING id::text
	`, s.organizationID, s.projectID, scriptID, versionID, episodeID, s.ownerUserID).Scan(&timingAnalysisID); err != nil {
		t.Fatalf("insert timing analysis for storyboard plan: %v", err)
	}
	var planID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO storyboard_plans(
			organization_id, project_id, script_id, script_version_id, script_episode_id, timing_analysis_id,
			revision, status, target_duration_ticks, estimated_shot_count, actual_shot_count,
			active, stale_state, metadata, created_by, activated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 'ready', 450000, 1, 1, true, 'fresh', '{}', $7, now())
		RETURNING id::text
	`, s.organizationID, s.projectID, scriptID, versionID, episodeID, timingAnalysisID, s.ownerUserID).Scan(&planID); err != nil {
		t.Fatalf("insert storyboard plan: %v", err)
	}
	if _, err := s.pool.Exec(s.ctx, `
		UPDATE storyboard_shots
		SET storyboard_plan_id = $2, script_episode_id = $3
		WHERE id = $1 AND project_id = $4
	`, shotID, planID, episodeID, s.projectID); err != nil {
		t.Fatalf("attach storyboard shot to plan: %v", err)
	}
	return planID
}

func (s *artifactPreviewSeed) insertStoryboardShot(t *testing.T, workflowRunID, imageArtifactID, videoArtifactID string) string {
	t.Helper()
	var shotID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			image_artifact_id, image_storage_key, video_artifact_id, video_storage_key, status, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 0, 450000, 450000, 450000, 'Wide station', 'slow push', 'mist drifting', 'hopeful', 'image prompt', 'video prompt', $4, 'org/project/shot-1.png', $5, 'org/project/shot-1.mp4', 'video_succeeded', '{}')
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
