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
	defer pool.Close()

	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	artifactID := insertWorkflowArtifact(t, ctx, pool, orgID, projectID, userID, "generated_image", "primary/lin.png", "image/png")
	var shotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no,
			duration_seconds, visual, camera, motion, mood, image_prompt, video_prompt,
			status, review_status, metadata
		)
		VALUES ($1, $2, $3, 0, 1, 5, 'Lin waits', 'static', 'wind moves', 'quiet', 'image prompt', 'video prompt',
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
	if len(assetContext.ImageReferences) != 1 || assetContext.ImageReferences[0].StorageKey != "primary/lin.png" {
		t.Fatalf("image references = %+v", assetContext.ImageReferences)
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
