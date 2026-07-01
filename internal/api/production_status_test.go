package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestProductionStatusHelpers(t *testing.T) {
	if got := productionMediaStatus(0, 0, 0, 0); got != "not_started" {
		t.Fatalf("empty media status = %s", got)
	}
	if got := productionMediaStatus(3, 1, 0, 0); got != "partial" {
		t.Fatalf("partial media status = %s", got)
	}
	if got := productionMediaStatus(3, 3, 0, 0); got != "ready" {
		t.Fatalf("ready media status = %s", got)
	}
	if !validReviewStatus("pending") || !validReviewStatus("approved") || !validReviewStatus("rejected") || !validReviewStatus("needs_edit") || validReviewStatus("done") {
		t.Fatalf("review status validation failed")
	}
	if got := workflowTypeFromInput([]byte(`{"workflowType":"script_to_video","input":{}}`)); got != "script_to_video" {
		t.Fatalf("workflowTypeFromInput = %s", got)
	}
	if permission, ok := productionActionPermission("generate_asset_images"); !ok || permission != "asset.generate" {
		t.Fatalf("production action permission = %s ok=%v", permission, ok)
	}
	if permission, ok := productionActionPermission("parse_script_scenes"); !ok || permission != authz.PermissionScriptWrite {
		t.Fatalf("parse scene permission = %s ok=%v", permission, ok)
	}
	regenerationCases := []struct {
		targetType   string
		workflowType string
		permission   string
	}{
		{"canonical_asset_image", "regenerate_canonical_asset_image", authz.PermissionAssetGenerate},
		{"derived_asset_image", "regenerate_derived_asset_image", authz.PermissionAssetGenerate},
		{"shot_image", "regenerate_shot_image", authz.PermissionStoryboardGenerate},
		{"shot_video", "regenerate_shot_video", authz.PermissionWorkflowRun},
		{"final_video", "regenerate_final_video", authz.PermissionWorkflowRun},
		{"script_scene", "regenerate_script_scene", authz.PermissionScriptWrite},
		{"scene_storyboard", "regenerate_scene_storyboard", authz.PermissionStoryboardGenerate},
	}
	for _, tc := range regenerationCases {
		workflowType, workflowFunc, permissions, ok := regenerationWorkflow(tc.targetType)
		if !ok || workflowType != tc.workflowType || workflowFunc == nil || len(permissions) == 0 || permissions[0] != tc.permission {
			t.Fatalf("regeneration workflow %s = workflowType=%s permissions=%v ok=%v", tc.targetType, workflowType, permissions, ok)
		}
	}
	if _, _, _, ok := regenerationWorkflow("unknown"); ok {
		t.Fatalf("unknown regeneration target should be rejected")
	}
}

func TestProductionStatusEmptyProject(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var status ProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if status.ProjectID != seed.projectID {
		t.Fatalf("project id = %s, want %s", status.ProjectID, seed.projectID)
	}
	if status.Stages.Source.Status != "not_started" || status.Stages.Assets.Status != "not_started" || status.Stages.Storyboard.Status != "not_started" {
		t.Fatalf("empty stage statuses = %+v", status.Stages)
	}
	if status.Overall.Stage != "source" || status.Overall.Progress != 0 {
		t.Fatalf("empty overall = %+v", status.Overall)
	}
}

func TestProductionStatusCounts(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	seed.insertProjectSource(t, "novel", "Novel Source")
	seed.insertProjectSource(t, "script", "Script Source")
	scriptID := seed.insertActiveScript(t)
	scriptVersionID := seed.currentScriptVersionID(t, scriptID)
	seed.insertScriptScene(t, scriptID, scriptVersionID, 1, "approved", "fresh")
	seed.insertScriptScene(t, scriptID, scriptVersionID, 2, "pending", "needs_regeneration")
	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/asset.png", "image/png")
	videoArtifactID := seed.insertArtifact(t, "generated_video", "org/project/video.mp4", "video/mp4")
	finalArtifactID := seed.insertArtifact(t, "final_video", "org/project/final.mp4", "video/mp4")
	characterID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", imageArtifactID)
	seed.insertCanonicalAsset(t, "scene", "Morning Station", "pending", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, imageArtifactID, videoArtifactID, "approved", "video_succeeded")
	seed.insertShotAssetRequirement(t, workflowRunID, shotID, characterID, "approved", imageArtifactID)

	var status ProductionStatus
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/production/status", seed.ownerToken, seed.organizationID, nil, &status)
	if status.Stages.Source.NovelSourceCount != 1 || status.Stages.Source.ScriptSourceCount != 1 || status.Stages.Source.ActiveScriptID == nil || *status.Stages.Source.ActiveScriptID != scriptID {
		t.Fatalf("source stage = %+v", status.Stages.Source)
	}
	if status.Stages.Source.ScriptSceneCount != 2 || status.Stages.Source.ApprovedScriptSceneCount != 1 || status.Stages.Source.PendingScriptSceneCount != 1 || status.Stages.Source.StaleScriptSceneCount != 1 {
		t.Fatalf("source scene counts = %+v", status.Stages.Source)
	}
	if status.Stages.Assets.CharacterCount != 1 || status.Stages.Assets.SceneCount != 1 || status.Stages.Assets.ReferenceImageCount != 1 || status.Stages.Assets.PendingReviewCount != 1 {
		t.Fatalf("assets stage = %+v", status.Stages.Assets)
	}
	if status.Stages.Storyboard.ShotCount != 1 || status.Stages.Storyboard.ConfirmedShotCount != 1 {
		t.Fatalf("storyboard stage = %+v", status.Stages.Storyboard)
	}
	if status.Stages.ShotAssets.RequirementCount != 1 || status.Stages.ShotAssets.DerivedImageCount != 1 {
		t.Fatalf("shot assets stage = %+v", status.Stages.ShotAssets)
	}
	if status.Stages.ShotImages.Succeeded != 1 || status.Stages.ShotVideos.Succeeded != 1 {
		t.Fatalf("shot media stages image=%+v video=%+v", status.Stages.ShotImages, status.Stages.ShotVideos)
	}
	if status.Stages.FinalVideo.ArtifactID == nil || *status.Stages.FinalVideo.ArtifactID != finalArtifactID || status.Stages.FinalVideo.Status != "ready" {
		t.Fatalf("final video stage = %+v", status.Stages.FinalVideo)
	}
}

func TestProductionActionPermission(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/production/actions", seed.otherToken, seed.organizationID, map[string]any{
		"action": "analyze_assets",
	}, http.StatusForbidden, "ACCESS_DENIED")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/production/actions", seed.ownerToken, seed.organizationID, map[string]any{
		"action": "unknown_action",
	}, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
}

func TestProductionReviewAPI(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "pending", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, "", "", "pending", "pending")
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, shotID, assetID, "pending", "")

	var assetReview ReviewResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/assets/"+assetID+"/review", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewStatus": "approved",
		"note":         "asset approved",
	}, &assetReview)
	if assetReview.ReviewStatus != "approved" || assetReview.Note == nil || *assetReview.Note != "asset approved" {
		t.Fatalf("asset review = %+v", assetReview)
	}
	var shotReview ReviewResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID+"/review", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewStatus": "needs_edit",
	}, &shotReview)
	if shotReview.ReviewStatus != "needs_edit" {
		t.Fatalf("shot review = %+v", shotReview)
	}
	var requirementReview ReviewResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/"+requirementID+"/review", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewStatus": "approved",
	}, &requirementReview)
	if requirementReview.ReviewStatus != "approved" {
		t.Fatalf("requirement review = %+v", requirementReview)
	}
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/assets/"+assetID+"/review", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewStatus": "done",
	}, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
}

func TestCreativeObjectEditAPIMarksManualOverrideAndStale(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, "", "", "approved", "video_succeeded")
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, shotID, assetID, "approved", "")

	var asset CanonicalAsset
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/canonical-assets/"+assetID, seed.ownerToken, seed.organizationID, map[string]any{
		"name":         "Lin Chu Revised",
		"description":  "manual description",
		"visualTraits": map[string]any{"hair": "black"},
	}, &asset)
	if !asset.ManualOverride || asset.StaleState != "fresh" || asset.ReviewStatus != "pending" || asset.Name != "Lin Chu Revised" {
		t.Fatalf("updated asset = %+v", asset)
	}
	assertStaleState(t, seed, "shot_asset_requirements", requirementID, "upstream_changed")
	assertStaleState(t, seed, "storyboard_shots", shotID, "needs_regeneration")

	var shot StoryboardShot
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/storyboard-shots/"+shotID, seed.ownerToken, seed.organizationID, map[string]any{
		"visual":          "Manual shot visual",
		"durationSeconds": 6,
		"imagePrompt":     "manual image prompt",
	}, &shot)
	if !shot.ManualOverride || shot.StaleState != "needs_regeneration" || shot.ReviewStatus != "pending" || shot.Visual != "Manual shot visual" {
		t.Fatalf("updated shot = %+v", shot)
	}

	var requirement ShotAssetRequirement
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/shot-asset-requirements/"+requirementID, seed.ownerToken, seed.organizationID, map[string]any{
		"pose":   "standing",
		"prompt": "manual derived prompt",
	}, &requirement)
	if !requirement.ManualOverride || requirement.StaleState != "needs_regeneration" || requirement.ReviewStatus != "pending" || requirement.Pose == nil || *requirement.Pose != "standing" {
		t.Fatalf("updated requirement = %+v", requirement)
	}
	assertStaleState(t, seed, "storyboard_shots", shotID, "needs_regeneration")

	var eventCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*)
		FROM event_outbox
		WHERE project_id = $1
		  AND event_type IN ('asset.updated', 'storyboard.shot.updated', 'shot_asset_requirement.updated')
	`, seed.projectID).Scan(&eventCount); err != nil {
		t.Fatalf("count edit events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("edit event count = %d, want 3", eventCount)
	}
}

func TestStoreScriptScenesPreservesManualOverrideWhenForceFalse(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	tx, err := seed.pool.Begin(seed.ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	scenes, err := workflows.StoreScriptScenes(seed.ctx, tx, workflows.ScriptSceneStoreInput{
		OrganizationID:  seed.organizationID,
		ProjectID:       seed.projectID,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		CreatedBy:       seed.ownerUserID,
	}, []workflows.ScriptSceneCandidate{{
		SceneNo:    1,
		Title:      "Agent Scene",
		Characters: json.RawMessage(`["Lin"]`),
		Content:    "agent content",
	}})
	if err != nil {
		t.Fatalf("store scenes: %v", err)
	}
	if err := tx.Commit(seed.ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if len(scenes) != 1 {
		t.Fatalf("stored scenes len = %d", len(scenes))
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE script_scenes
		SET title = 'Manual Scene', content = 'manual content', manual_override = true
		WHERE id = $1
	`, scenes[0].ID); err != nil {
		t.Fatalf("mark manual scene: %v", err)
	}
	tx, err = seed.pool.Begin(seed.ctx)
	if err != nil {
		t.Fatalf("begin second tx: %v", err)
	}
	preserved, err := workflows.StoreScriptScenes(seed.ctx, tx, workflows.ScriptSceneStoreInput{
		OrganizationID:  seed.organizationID,
		ProjectID:       seed.projectID,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		CreatedBy:       seed.ownerUserID,
		Force:           false,
	}, []workflows.ScriptSceneCandidate{{SceneNo: 1, Title: "Agent Overwrite", Content: "agent overwrite"}})
	if err != nil {
		t.Fatalf("store scenes force false: %v", err)
	}
	if err := tx.Commit(seed.ctx); err != nil {
		t.Fatalf("commit second tx: %v", err)
	}
	if preserved[0].Title != "Manual Scene" || preserved[0].Content != "manual content" || !preserved[0].ManualOverride {
		t.Fatalf("manual scene overwritten: %+v", preserved[0])
	}

	tx, err = seed.pool.Begin(seed.ctx)
	if err != nil {
		t.Fatalf("begin third tx: %v", err)
	}
	overwritten, err := workflows.StoreScriptScenes(seed.ctx, tx, workflows.ScriptSceneStoreInput{
		OrganizationID:  seed.organizationID,
		ProjectID:       seed.projectID,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		CreatedBy:       seed.ownerUserID,
		Force:           true,
	}, []workflows.ScriptSceneCandidate{{SceneNo: 1, Title: "Agent Overwrite", Content: "agent overwrite"}})
	if err != nil {
		t.Fatalf("store scenes force true: %v", err)
	}
	if err := tx.Commit(seed.ctx); err != nil {
		t.Fatalf("commit third tx: %v", err)
	}
	if overwritten[0].Title != "Agent Overwrite" || overwritten[0].ManualOverride {
		t.Fatalf("force overwrite failed: %+v", overwritten[0])
	}
}

func TestScriptSceneEditAPIReviewsAndMarksDownstreamStale(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	sceneID := seed.insertScriptScene(t, scriptID, versionID, 1, "approved", "fresh")
	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	workflowRunID := seed.insertWorkflowRun(t, "succeeded")
	shotID := seed.insertProductionShot(t, workflowRunID, "", "", "approved", "video_succeeded")
	requirementID := seed.insertShotAssetRequirement(t, workflowRunID, shotID, assetID, "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO scene_asset_links(organization_id, project_id, script_scene_id, asset_id, asset_role, metadata)
		VALUES ($1, $2, $3, $4, 'main_character', '{}')
	`, seed.organizationID, seed.projectID, sceneID, assetID); err != nil {
		t.Fatalf("insert scene asset link: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE storyboard_shots SET script_scene_id = $2 WHERE id = $1`, shotID, sceneID); err != nil {
		t.Fatalf("link shot scene: %v", err)
	}

	var assetList struct {
		Items []CanonicalAsset `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/canonical-assets", seed.ownerToken, seed.organizationID, nil, &assetList)
	if len(assetList.Items) != 1 || assetList.Items[0].SceneCount != 1 || assetList.Items[0].StoryboardShotCount != 1 {
		t.Fatalf("asset scene links = %+v", assetList.Items)
	}

	var updated workflows.ScriptSceneRecord
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/script-scenes/"+sceneID, seed.ownerToken, seed.organizationID, map[string]any{
		"title":      "Manual Scene",
		"summary":    "Manual summary",
		"characters": []string{"Lin Chu", "Chen"},
		"content":    "manual scene content",
	}, &updated)
	if !updated.ManualOverride || updated.ReviewStatus != "pending" || updated.StaleState != "needs_regeneration" || updated.Title != "Manual Scene" {
		t.Fatalf("updated script scene = %+v", updated)
	}
	assertStaleState(t, seed, "canonical_assets", assetID, "upstream_changed")
	assertStaleState(t, seed, "shot_asset_requirements", requirementID, "upstream_changed")
	assertStaleState(t, seed, "storyboard_shots", shotID, "needs_regeneration")

	var review ReviewResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/script-scenes/"+sceneID+"/review", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewStatus": "approved",
		"note":         "scene approved",
	}, &review)
	if review.ReviewStatus != "approved" || review.Note == nil || *review.Note != "scene approved" {
		t.Fatalf("scene review = %+v", review)
	}

	var eventCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*)
		FROM event_outbox
		WHERE project_id = $1
		  AND event_type IN ('script.scene.updated', 'script.scene.reviewed')
	`, seed.projectID).Scan(&eventCount); err != nil {
		t.Fatalf("count script scene events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("script scene event count = %d, want 2", eventCount)
	}
}

func assertStaleState(t *testing.T, seed *artifactPreviewSeed, table, id, want string) {
	t.Helper()
	queryByTable := map[string]string{
		"canonical_assets":        `SELECT stale_state FROM canonical_assets WHERE id = $1 AND project_id = $2`,
		"storyboard_shots":        `SELECT stale_state FROM storyboard_shots WHERE id = $1 AND project_id = $2`,
		"shot_asset_requirements": `SELECT stale_state FROM shot_asset_requirements WHERE id = $1 AND project_id = $2`,
	}
	query, ok := queryByTable[table]
	if !ok {
		t.Fatalf("unsupported stale state table %s", table)
	}
	var got string
	if err := seed.pool.QueryRow(seed.ctx, query, id, seed.projectID).Scan(&got); err != nil {
		t.Fatalf("read stale state %s %s: %v", table, id, err)
	}
	if got != want {
		t.Fatalf("%s %s stale_state = %s, want %s", table, id, got, want)
	}
}

func (s *artifactPreviewSeed) insertProjectSource(t *testing.T, sourceType, title string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO project_sources(organization_id, project_id, source_type, title, content, content_format, status, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'content', 'plain_text', 'ready', '{}', $5)
		RETURNING id
	`, s.organizationID, s.projectID, sourceType, title, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert project source: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertActiveScript(t *testing.T) string {
	t.Helper()
	var scriptID, versionID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, 'Active Script', 'draft', $3)
		RETURNING id
	`, s.organizationID, s.projectID, s.ownerUserID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO script_versions(organization_id, project_id, script_id, version_no, version, content, content_format, metadata, created_by)
		VALUES ($1, $2, $3, 1, 1, 'script content', 'markdown', '{}', $4)
		RETURNING id
	`, s.organizationID, s.projectID, scriptID, s.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := s.pool.Exec(s.ctx, `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script: %v", err)
	}
	return scriptID
}

func (s *artifactPreviewSeed) currentScriptVersionID(t *testing.T, scriptID string) string {
	t.Helper()
	var versionID string
	if err := s.pool.QueryRow(s.ctx, `
		SELECT current_version_id::text
		FROM scripts
		WHERE id = $1 AND project_id = $2
	`, scriptID, s.projectID).Scan(&versionID); err != nil {
		t.Fatalf("read current script version: %v", err)
	}
	return versionID
}

func (s *artifactPreviewSeed) insertScriptScene(t *testing.T, scriptID, versionID string, sceneNo int, reviewStatus, staleState string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO script_scenes(
			organization_id, project_id, script_id, script_version_id,
			scene_index, scene_no, title, summary, characters, scenes, props, source_event_ids,
			content, content_format, review_status, stale_state, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'summary', '["Lin Chu"]', '["Station"]', '["Camera"]', '[]',
		        'scene content', 'markdown', $8, $9, '{}', $10)
		RETURNING id
	`, s.organizationID, s.projectID, scriptID, versionID, sceneNo-1, sceneNo, "Scene "+itoa(sceneNo), reviewStatus, staleState, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert script scene: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertCanonicalAsset(t *testing.T, assetType, name, reviewStatus, referenceArtifactID string) string {
	t.Helper()
	var id string
	referenceStorageKey := ""
	if referenceArtifactID != "" {
		referenceStorageKey = "storage/" + name + ".png"
	}
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, visual_traits,
			reference_artifact_id, reference_storage_key, status, review_status, source_script_ids, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, '{}', NULLIF($6, '')::uuid, NULLIF($7, ''), 'prompt_ready', $8, '[]', '{}', $9)
		RETURNING id
	`, s.organizationID, s.projectID, assetType, name, name+" description", referenceArtifactID, referenceStorageKey, reviewStatus, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert canonical asset: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertProductionShot(t *testing.T, workflowRunID, imageArtifactID, videoArtifactID, reviewStatus, status string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			duration_seconds, visual, camera, motion, mood, image_prompt, video_prompt,
			image_artifact_id, video_artifact_id, status, review_status, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 5, 'Wide station', 'slow push', 'mist drifting', 'hopeful',
		        'image prompt', 'video prompt', NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6, $7, '{}')
		RETURNING id
	`, s.organizationID, s.projectID, workflowRunID, imageArtifactID, videoArtifactID, status, reviewStatus).Scan(&id); err != nil {
		t.Fatalf("insert production shot: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertShotAssetRequirement(t *testing.T, workflowRunID, shotID, assetID, reviewStatus, derivedArtifactID string) string {
	t.Helper()
	var id string
	derivedStorageKey := ""
	if derivedArtifactID != "" {
		derivedStorageKey = "derived/" + assetID + ".png"
	}
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, prompt, derived_artifact_id, derived_storage_key, status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 'character_appearance', 'prompt',
		        NULLIF($6, '')::uuid, NULLIF($7, ''), 'pending', $8, '{}')
		RETURNING id
	`, s.organizationID, s.projectID, workflowRunID, shotID, assetID, derivedArtifactID, derivedStorageKey, reviewStatus).Scan(&id); err != nil {
		t.Fatalf("insert shot asset requirement: %v", err)
	}
	return id
}
