package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestShotAssetContextIncludesAssetCardConsistencyPrompt(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)

	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	artifactID := insertWorkflowArtifact(t, ctx, pool, orgID, projectID, userID, "generated_image", "primary/lin.png", "image/png")
	var shotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			start_tick, end_tick, duration_min_ticks, duration_max_ticks,
			visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 0, 450000, 450000, 450000, 'Lin waits', 'static', 'wind moves', 'quiet', 'image prompt', 'video prompt',
		        'pending', 'pending', '{}')
		RETURNING id::text
	`, orgID, projectID, workflowRunID).Scan(&shotID); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	var assetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description, profile,
			consistency_prompt, negative_prompt, primary_reference_artifact_id, primary_reference_storage_key,
			visual_traits, status, review_status, source_script_ids, metadata, created_by
		)
		VALUES ($1, $2, 'character', 'Lin Chu', 'Quiet traveler', '{"appearance":"dark coat"}',
		        'keep Lin Chu face and dark coat stable', 'no age changes', $3, 'primary/lin.png',
		        '{}', 'prompt_ready', 'approved', '[]', '{}', $4)
		RETURNING id::text
	`, orgID, projectID, artifactID, userID).Scan(&assetID); err != nil {
		t.Fatalf("insert canonical asset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO shot_asset_requirements(
			organization_id, project_id, workflow_run_id, storyboard_shot_id, asset_id,
			requirement_type, role_in_shot, costume, prompt, status, review_status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 'character_appearance', 'lead', 'dark coat', 'use canonical primary', 'pending', 'approved', '{}')
	`, orgID, projectID, workflowRunID, shotID, assetID); err != nil {
		t.Fatalf("insert shot asset requirement: %v", err)
	}

	activities := NewActivities(pool, newWorkflowMemoryStorage(), nil)
	assetContext, err := activities.shotAssetContext(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("shotAssetContext: %v", err)
	}
	if !strings.Contains(assetContext.AssetsSummary, "consistency=keep Lin Chu face and dark coat stable") {
		t.Fatalf("asset summary missing consistency prompt: %s", assetContext.AssetsSummary)
	}
	if !strings.Contains(assetContext.AssetsSummary, "negative=no age changes") {
		t.Fatalf("asset summary missing negative prompt: %s", assetContext.AssetsSummary)
	}
	if len(assetContext.PromptAssets) != 1 || assetContext.PromptAssets[0].Name != "Lin Chu" || assetContext.PromptAssets[0].ConsistencyPrompt != "keep Lin Chu face and dark coat stable" {
		t.Fatalf("structured video prompt assets = %+v", assetContext.PromptAssets)
	}
	if got := assetContext.PromptAssets[0].Requirement["costume"]; got != "dark coat" {
		t.Fatalf("structured shot requirement costume = %v", got)
	}
	if len(assetContext.ImageReferences) != 1 || assetContext.ImageReferences[0].StorageKey != "primary/lin.png" {
		t.Fatalf("image references = %+v", assetContext.ImageReferences)
	}
	otherArtifactID := insertWorkflowArtifact(t, ctx, pool, orgID, projectID, userID, "generated_image", "primary/signal-bell.png", "image/png")
	var otherAssetID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO canonical_assets(
			organization_id, project_id, asset_type, name, description,
			primary_reference_artifact_id, primary_reference_storage_key,
			visual_traits, status, review_status, source_script_ids, metadata, created_by
		)
		VALUES ($1, $2, 'prop', 'Signal Bell', 'A bronze signal bell', $3, 'primary/signal-bell.png',
		        '{}', 'prompt_ready', 'approved', '[]', '{}', $4)
		RETURNING id::text
	`, orgID, projectID, otherArtifactID, userID).Scan(&otherAssetID); err != nil {
		t.Fatalf("insert other canonical asset: %v", err)
	}
	customArtifactID := insertWorkflowArtifact(t, ctx, pool, orgID, projectID, userID, "asset_reference_image", "custom/lin-profile.png", "image/png")
	var customReferenceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title,
			artifact_id, storage_key, is_primary, status, metadata, created_by
		)
		VALUES ($1, $2, $3, 'uploaded', 'Lin profile', $4, 'custom/lin-profile.png', false, 'active', '{}', $5)
		RETURNING id::text
	`, orgID, projectID, assetID, customArtifactID, userID).Scan(&customReferenceID); err != nil {
		t.Fatalf("insert custom asset reference: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_reference_mode = 'custom', image_reference_keys = ARRAY[$2]::text[]
		WHERE id = $1
	`, shotID, "asset_primary:"+otherAssetID); err != nil {
		t.Fatalf("configure custom shot references: %v", err)
	}
	customContext, err := activities.shotAssetContext(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("custom shotAssetContext: %v", err)
	}
	if customContext.ImageReferenceMode != "custom" || len(customContext.ImageReferences) != 1 || customContext.ImageReferences[0].StorageKey != "primary/signal-bell.png" {
		t.Fatalf("custom image references = %+v", customContext)
	}
	if _, err := pool.Exec(ctx, `UPDATE storyboard_shots SET image_reference_mode = 'none', image_reference_keys = '{}' WHERE id = $1`, shotID); err != nil {
		t.Fatalf("disable shot references: %v", err)
	}
	noReferenceContext, err := activities.shotAssetContext(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("no-reference shotAssetContext: %v", err)
	}
	if noReferenceContext.ImageReferenceMode != "none" || len(noReferenceContext.ImageReferences) != 0 {
		t.Fatalf("none image references = %+v", noReferenceContext)
	}
	shot, err := activities.storyboardShotByID(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("storyboardShotByID: %v", err)
	}
	autoVideoContext, err := activities.shotVideoReferenceContext(ctx, projectID, shot, noReferenceContext)
	if err != nil {
		t.Fatalf("auto shotVideoReferenceContext: %v", err)
	}
	if autoVideoContext.ReferenceMode != "auto" || len(autoVideoContext.References) != 1 || autoVideoContext.References[0].StorageKey != "primary/lin.png" {
		t.Fatalf("auto video references = %+v", autoVideoContext)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_reference_mode = 'custom', video_reference_keys = ARRAY[$2]::text[]
		WHERE id = $1
	`, shotID, "asset_reference:"+customReferenceID); err != nil {
		t.Fatalf("configure custom video references: %v", err)
	}
	shot, err = activities.storyboardShotByID(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("reload storyboard shot: %v", err)
	}
	customVideoContext, err := activities.shotVideoReferenceContext(ctx, projectID, shot, assetContext)
	if err != nil {
		t.Fatalf("custom shotVideoReferenceContext: %v", err)
	}
	if customVideoContext.ReferenceMode != "custom" || len(customVideoContext.References) != 1 || customVideoContext.References[0].StorageKey != "custom/lin-profile.png" {
		t.Fatalf("custom video references = %+v", customVideoContext)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_reference_mode = 'none', video_reference_keys = '{}'
		WHERE id = $1
	`, shotID); err != nil {
		t.Fatalf("disable video references: %v", err)
	}
	shot, err = activities.storyboardShotByID(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("reload no-reference storyboard shot: %v", err)
	}
	noVideoReferenceContext, err := activities.shotVideoReferenceContext(ctx, projectID, shot, assetContext)
	if err != nil {
		t.Fatalf("none shotVideoReferenceContext: %v", err)
	}
	if noVideoReferenceContext.ReferenceMode != "none" || len(noVideoReferenceContext.References) != 0 {
		t.Fatalf("none video references = %+v", noVideoReferenceContext)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_reference_mode = 'auto',
			image_artifact_id = $2,
			image_storage_key = 'shot/current.png',
			image_status = 'succeeded',
			stale_state = 'fresh'
		WHERE id = $1
	`, shotID, artifactID); err != nil {
		t.Fatalf("attach current shot image: %v", err)
	}
	shot, err = activities.storyboardShotByID(ctx, projectID, shotID)
	if err != nil {
		t.Fatalf("reload storyboard shot image: %v", err)
	}
	shotFrameContext, err := activities.shotVideoReferenceContext(ctx, projectID, shot, assetContext)
	if err != nil {
		t.Fatalf("shot frame reference context: %v", err)
	}
	if len(shotFrameContext.References) != 1 || shotFrameContext.References[0].Type != "first_frame" || shotFrameContext.References[0].StorageKey != "shot/current.png" {
		t.Fatalf("shot frame video reference = %+v", shotFrameContext)
	}
}

func insertWorkflowArtifact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, userID, artifactType, storageKey, mimeType string) string {
	t.Helper()
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, '{}', $6)
		RETURNING id::text
	`, orgID, projectID, artifactType, storageKey, mimeType, userID).Scan(&artifactID); err != nil {
		t.Fatalf("insert workflow artifact: %v", err)
	}
	return artifactID
}
