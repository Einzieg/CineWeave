package api

import (
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestValidateShotAssetRequirementReviewCandidate(t *testing.T) {
	t.Run("eligible with unapproved base asset warning", func(t *testing.T) {
		issues, warnings := validateShotAssetRequirementReviewCandidate(shotAssetRequirementReviewCandidate{
			AssetType:          "character",
			RequirementType:    "character_appearance",
			Status:             "pending",
			ReviewStatus:       "pending",
			StaleState:         "fresh",
			HasContext:         true,
			ShotStaleState:     "fresh",
			AssetStatus:        "prompt_ready",
			AssetReviewStatus:  "pending",
			AssetStaleState:    "fresh",
			BaseReferenceReady: true,
		})
		if len(issues) != 0 {
			t.Fatalf("issues = %#v", issues)
		}
		if len(warnings) != 1 || warnings[0].Code != "CANONICAL_ASSET_REVIEW_PENDING" {
			t.Fatalf("warnings = %#v", warnings)
		}
	})

	t.Run("reports every deterministic blocker", func(t *testing.T) {
		issues, warnings := validateShotAssetRequirementReviewCandidate(shotAssetRequirementReviewCandidate{
			AssetType:         "character",
			RequirementType:   "scene_state",
			Status:            "image_running",
			StaleState:        "upstream_changed",
			ShotDeleted:       true,
			ShotStaleState:    "needs_regeneration",
			AssetStatus:       "archived",
			AssetReviewStatus: "pending",
			AssetStaleState:   "upstream_changed",
		})
		codes := make(map[string]bool, len(issues))
		for _, issue := range issues {
			codes[issue.Code] = true
		}
		for _, code := range []string{
			"SHOT_ASSET_REQUIREMENT_IMAGE_RUNNING",
			"STORYBOARD_SHOT_DELETED",
			"CANONICAL_ASSET_ARCHIVED",
			"CANONICAL_ASSET_STALE",
			"SHOT_ASSET_REQUIREMENT_TYPE_MISMATCH",
			"SHOT_ASSET_REQUIREMENT_CONTEXT_MISSING",
			"DERIVED_ASSET_BASE_REFERENCE_REQUIRED",
		} {
			if !codes[code] {
				t.Fatalf("missing issue %s in %#v", code, issues)
			}
		}
		warningCodes := make(map[string]bool, len(warnings))
		for _, warning := range warnings {
			warningCodes[warning.Code] = true
		}
		if !warningCodes["SHOT_ASSET_REQUIREMENT_UPSTREAM_CHANGED"] {
			t.Fatalf("missing upstream change warning in %#v", warnings)
		}
	})

	t.Run("manual correction remains eligible for regeneration", func(t *testing.T) {
		issues, warnings := validateShotAssetRequirementReviewCandidate(shotAssetRequirementReviewCandidate{
			AssetType:          "scene",
			RequirementType:    "scene_environment",
			Status:             "pending",
			ReviewStatus:       "pending",
			StaleState:         "needs_regeneration",
			HasContext:         true,
			ShotStaleState:     "needs_regeneration",
			AssetStatus:        "prompt_ready",
			AssetReviewStatus:  "approved",
			AssetStaleState:    "fresh",
			BaseReferenceReady: true,
		})
		if len(issues) != 0 {
			t.Fatalf("issues = %#v", issues)
		}
		codes := make(map[string]bool, len(warnings))
		for _, warning := range warnings {
			codes[warning.Code] = true
		}
		if !codes["SHOT_ASSET_REQUIREMENT_REGENERATION_PENDING"] || !codes["STORYBOARD_SHOT_MEDIA_REGENERATION_PENDING"] {
			t.Fatalf("warnings = %#v", warnings)
		}
	})
}

func TestBatchReviewShotAssetRequirementsAPI(t *testing.T) {
	t.Setenv(videoproduction.FeatureFlagEnvironmentVariable, "true")
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	var project Project
	doAPISuccess(t, server, http.MethodPost, "/api/projects", seed.ownerToken, seed.organizationID, map[string]any{
		"workspaceId":                   seed.workspaceID,
		"name":                          "Shot Asset Review Project",
		"videoProductionProfileKey":     videoproduction.ProfileSingleFrameI2V,
		"videoProductionProfileVersion": 1,
		"compatibilityPolicy":           videoproduction.CompatibilityStrict,
	}, &project)
	seed.projectID = project.ID

	referenceArtifactID := seed.insertArtifact(t, "generated_image", "assets/base.png", "image/png")
	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "pending", referenceArtifactID)
	var workflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, 'script_to_storyboard', 'succeeded', '{}', '{}', $4, $5, $6, $7)
		RETURNING id
	`, seed.organizationID, seed.projectID, "shot-asset-review-workflow-"+randomStorageSegment(), seed.ownerUserID,
		project.ProductionGeneration.ID, project.VideoProductionBinding.ID, project.VideoProductionBinding.Revision).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	var shotID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, metadata, production_generation_id
		)
		VALUES ($1, $2, $3, 0, 1, 0, 450000, 450000, 450000,
		        'Wide station', 'slow push', 'mist drifting', 'hopeful', 'image prompt', 'video prompt',
		        'storyboard_ready', 'approved', '{}', $4)
		RETURNING id
	`, seed.organizationID, seed.projectID, workflowRunID, project.ProductionGeneration.ID).Scan(&shotID); err != nil {
		t.Fatalf("insert production shot: %v", err)
	}
	insertRequirement := func(assetID string) string {
		var requirementID string
		if err := seed.pool.QueryRow(seed.ctx, `
			INSERT INTO shot_asset_requirements(
				organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
				requirement_type, prompt, status, review_status, metadata, production_generation_id
			)
			VALUES ($1, $2, $3, $4, $5, 'character_appearance', 'prompt', 'pending', 'pending', '{}', $6)
			RETURNING id
		`, seed.organizationID, seed.projectID, workflowRunID, shotID, assetID, project.ProductionGeneration.ID).Scan(&requirementID); err != nil {
			t.Fatalf("insert shot asset requirement: %v", err)
		}
		return requirementID
	}
	eligibleID := insertRequirement(assetID)

	missingReferenceAssetID := seed.insertCanonicalAsset(t, "character", "No Reference", "pending", "")
	blockedID := insertRequirement(missingReferenceAssetID)

	var result BatchReviewShotAssetRequirementsResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/review-batch", seed.ownerToken, seed.organizationID, map[string]any{
		"requirementIds": []string{eligibleID, blockedID},
		"reviewStatus":   "approved",
		"note":           "batch validation",
	}, &result)
	if result.TotalItems != 2 || result.ApprovedCount != 1 || result.NeedsEditCount != 1 || result.BlockedCount != 1 {
		t.Fatalf("batch result = %+v", result)
	}
	assertShotAssetRequirementState(t, seed, eligibleID, "pending", "approved", "fresh", false)
	assertShotAssetRequirementState(t, seed, blockedID, "pending", "needs_edit", "fresh", false)
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v", result.Items)
	}
	foundBaseReferenceIssue := false
	for _, item := range result.Items {
		if item.RequirementID != blockedID {
			continue
		}
		for _, issue := range item.Issues {
			if issue.Code == "DERIVED_ASSET_BASE_REFERENCE_REQUIRED" {
				foundBaseReferenceIssue = true
			}
		}
	}
	if !foundBaseReferenceIssue {
		t.Fatalf("blocked item did not report missing base reference: %#v", result.Items)
	}

	var updated ShotAssetRequirement
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+seed.projectID+"/shot-asset-requirements/"+blockedID, seed.ownerToken, seed.organizationID, map[string]any{
		"assetId": assetID,
		"action":  "站立看向镜头",
	}, &updated)
	if updated.AssetID != assetID || updated.ReviewStatus != "pending" || updated.StaleState != "needs_regeneration" || !updated.ManualOverride {
		t.Fatalf("updated requirement = %+v", updated)
	}
	var repairedResult BatchReviewShotAssetRequirementsResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/review-batch", seed.ownerToken, seed.organizationID, map[string]any{
		"requirementIds": []string{blockedID},
		"reviewStatus":   "approved",
	}, &repairedResult)
	if repairedResult.ApprovedCount != 1 || repairedResult.BlockedCount != 0 {
		t.Fatalf("repaired batch result = %+v", repairedResult)
	}

	pendingID := insertRequirement(assetID)
	var pendingResult BatchReviewShotAssetRequirementsResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/review-batch", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewStatus": "approved",
	}, &pendingResult)
	if pendingResult.TotalItems != 1 || pendingResult.ApprovedCount != 1 {
		t.Fatalf("pending batch result = %+v", pendingResult)
	}
	assertShotAssetRequirementState(t, seed, pendingID, "pending", "approved", "fresh", false)

	skipped, err := seed.apiServer.skipShotAssetRequirementCore(seed.ctx, project, seed.ownerUserID, "project_agent", "与镜头内容重复", pendingID)
	if err != nil {
		t.Fatalf("skip requirement: %v", err)
	}
	if skipped.Status != "skipped" || skipped.ReviewStatus != "approved" || skipped.StaleState != "fresh" || !skipped.ManualOverride {
		t.Fatalf("skipped requirement = %+v", skipped)
	}
	var skipReason, skipSource string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COALESCE(metadata->>'skipReason', ''), COALESCE(metadata->>'skipSource', '')
		FROM shot_asset_requirements
		WHERE id = $1
	`, pendingID).Scan(&skipReason, &skipSource); err != nil {
		t.Fatalf("read skip audit: %v", err)
	}
	if skipReason != "与镜头内容重复" || skipSource != "project_agent" {
		t.Fatalf("skip audit = %q / %q", skipReason, skipSource)
	}

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	insertEpisode := func(index int, title string) string {
		var episodeID string
		if err := seed.pool.QueryRow(seed.ctx, `
			INSERT INTO script_episodes(
				organization_id, project_id, script_id, script_version_id,
				episode_index, episode_title, content, content_format, review_status, stale_state, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'episode content', 'markdown', 'approved', 'fresh', '{}', $7)
			RETURNING id::text
		`, seed.organizationID, seed.projectID, scriptID, versionID, index, title, seed.ownerUserID).Scan(&episodeID); err != nil {
			t.Fatalf("insert script episode: %v", err)
		}
		return episodeID
	}
	episodeOneID := insertEpisode(1, "第 1 集")
	episodeTwoID := insertEpisode(2, "第 2 集")
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE storyboard_shots
		SET script_id = $2, script_version_id = $3, script_episode_id = $4, episode_index = 1, episode_shot_index = 0
		WHERE id = $1
	`, shotID, scriptID, versionID, episodeOneID); err != nil {
		t.Fatalf("attach first episode to shot: %v", err)
	}
	firstEpisodePendingID := insertRequirement(assetID)
	var secondEpisodeShotID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, script_id, script_version_id, script_episode_id,
			shot_index, shot_no, episode_index, episode_shot_index,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, metadata, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 1, 2, 2, 0, 0, 450000, 450000, 450000,
		        'Second episode shot', 'locked camera', 'still', 'neutral', 'image prompt', 'video prompt',
		        'storyboard_ready', 'approved', '{}', $7)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID, scriptID, versionID, episodeTwoID, project.ProductionGeneration.ID).Scan(&secondEpisodeShotID); err != nil {
		t.Fatalf("insert second episode shot: %v", err)
	}
	var secondEpisodePendingID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, prompt, status, review_status, metadata, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, 'character_appearance', 'prompt', 'pending', 'pending', '{}', $6)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID, secondEpisodeShotID, assetID, project.ProductionGeneration.ID).Scan(&secondEpisodePendingID); err != nil {
		t.Fatalf("insert second episode requirement: %v", err)
	}
	var episodeScopedResult BatchReviewShotAssetRequirementsResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/shot-asset-requirements/review-batch", seed.ownerToken, seed.organizationID, map[string]any{
		"scriptEpisodeId": episodeOneID,
		"reviewStatus":    "approved",
	}, &episodeScopedResult)
	if episodeScopedResult.TotalItems != 1 || episodeScopedResult.ApprovedCount != 1 {
		t.Fatalf("episode-scoped batch result = %+v", episodeScopedResult)
	}
	assertShotAssetRequirementState(t, seed, firstEpisodePendingID, "pending", "approved", "fresh", false)
	assertShotAssetRequirementState(t, seed, secondEpisodePendingID, "pending", "pending", "fresh", false)
}
