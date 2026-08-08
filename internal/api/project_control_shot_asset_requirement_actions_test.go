package api

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlShotAssetRequirementLifecycleUsesSharedActions(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	project := configureTimelineTestProject(t, handler, seed, "Codex Shot Asset Requirement Project")

	var controlKeyID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text FROM user_control_keys WHERE user_id = $1 AND status = 'active'
	`, seed.ownerUserID).Scan(&controlKeyID); err != nil {
		t.Fatalf("read control key: %v", err)
	}
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: controlKeyID},
	}

	referenceArtifactID := seed.insertArtifact(t, "generated_image", "assets/codex-shot-asset.png", "image/png")
	assetID := seed.insertCanonicalAsset(t, "character", "共享动作角色", "approved", referenceArtifactID)
	requirementID := insertProjectControlShotAssetRequirement(t, seed, project, assetID)

	reviewed := executeProjectControlTestAction(t, seed, identity, "shot_asset.review_requirements", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "shot-asset-review-codex",
		"requirementIds": []string{requirementID}, "reviewStatus": "approved", "note": "Codex 审核",
	})
	var reviewedData struct {
		Report BatchReviewShotAssetRequirementsResponse `json:"report"`
	}
	decodeProjectControlResultData(t, reviewed, &reviewedData)
	if reviewedData.Report.ApprovedCount != 1 || len(reviewedData.Report.Items) != 1 {
		t.Fatalf("review report=%+v", reviewedData.Report)
	}

	updated := executeProjectControlTestAction(t, seed, identity, "shot_asset.update_requirement", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "shot-asset-update-codex",
		"requirementId": requirementID,
		"patch":         map[string]any{"action": "转身看向镜头", "cameraRelation": "中景平视"},
	})
	var updatedData struct {
		Requirement ShotAssetRequirement `json:"requirement"`
	}
	decodeProjectControlResultData(t, updated, &updatedData)
	if updatedData.Requirement.ReviewStatus != "pending" || updatedData.Requirement.StaleState != "needs_regeneration" || !updatedData.Requirement.ManualOverride {
		t.Fatalf("updated requirement=%+v", updatedData.Requirement)
	}

	skipped := executeProjectControlTestAction(t, seed, identity, "shot_asset.skip_requirement", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "shot-asset-skip-codex",
		"requirementId": requirementID, "reason": "当前镜头不需要该衍生状态",
	})
	var skippedData struct {
		Requirement ShotAssetRequirement `json:"requirement"`
	}
	decodeProjectControlResultData(t, skipped, &skippedData)
	if skippedData.Requirement.Status != "skipped" || skippedData.Requirement.ReviewStatus != "approved" || skippedData.Requirement.StaleState != "fresh" {
		t.Fatalf("skipped requirement=%+v", skippedData.Requirement)
	}

	var commandCount, eventCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM project_control_commands
		WHERE project_id = $1 AND controller_type = 'codex_mcp'
		  AND action_name LIKE 'shot_asset.%' AND status = 'succeeded'
	`, seed.projectID).Scan(&commandCount); err != nil {
		t.Fatalf("count commands: %v", err)
	}
	if commandCount != 3 {
		t.Fatalf("command count=%d, want 3", commandCount)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM event_outbox
		WHERE project_id = $1 AND aggregate_id = $2
		  AND event_type IN ('shot_asset_requirement.updated', 'shot_asset_requirement.skipped')
	`, seed.projectID, requirementID).Scan(&eventCount); err != nil {
		t.Fatalf("count requirement events: %v", err)
	}
	if eventCount < 3 {
		t.Fatalf("requirement event count=%d, want at least 3", eventCount)
	}
}

func insertProjectControlShotAssetRequirement(
	t *testing.T,
	seed *artifactPreviewSeed,
	project Project,
	assetID string,
) string {
	t.Helper()
	var workflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, workflow_type, status,
			input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, 'script_to_storyboard', 'succeeded', '{}', '{}', $4, $5, $6, $7)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, "project-control-shot-asset-"+randomStorageSegment(), seed.ownerUserID,
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
		        '角色站在窗边', '中景平视', '轻微转身', '平静', 'image prompt', 'video prompt',
		        'storyboard_ready', 'approved', '{}', $4)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID, project.ProductionGeneration.ID).Scan(&shotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	var requirementID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, action, camera_relation, prompt, status, review_status,
			metadata, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, 'character_appearance', '站立', '中景', 'prompt',
		        'pending', 'pending', '{}', $6)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, workflowRunID, shotID, assetID, project.ProductionGeneration.ID).Scan(&requirementID); err != nil {
		t.Fatalf("insert shot asset requirement: %v", err)
	}
	return requirementID
}
