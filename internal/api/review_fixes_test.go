package api

import (
	"net/http"
	"testing"

	reviewpkg "github.com/Einzieg/cineweave/internal/review"
)

func TestReviewFixUnsupportedEntityType(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	itemID := seed.insertReviewItemForTarget(t, "open", "final_video", "high", "final_video_version", seed.projectID)
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-items/"+itemID+"/fixes/generate", seed.ownerToken, seed.organizationID, map[string]any{"mode": "deterministic"}, http.StatusUnprocessableEntity, "REVIEW_FIX_UNSUPPORTED")
}

func TestReviewFixRejectsIllegalPatchField(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)
	fixID := seed.insertReviewFixDraft(t, itemID, "canonical_asset", assetID, map[string]any{"id": "not-editable"}, nil)
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-fixes/"+fixID+"/apply", seed.ownerToken, seed.organizationID, map[string]any{"resolveReviewItem": true}, http.StatusUnprocessableEntity, "VALIDATION_FAILED")
}

func TestDeterministicReviewFixCreatesDraft(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE canonical_assets SET description = '' WHERE id = $1`, assetID); err != nil {
		t.Fatalf("clear asset description: %v", err)
	}
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)

	var fix ReviewFix
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-items/"+itemID+"/fixes/generate", seed.ownerToken, seed.organizationID, map[string]any{"mode": "deterministic"}, &fix)
	if fix.Status != "draft" || fix.FixType != "patch" || len(fix.Patch) == 0 || len(fix.AfterPreview) == 0 {
		t.Fatalf("fix = %+v", fix)
	}
}

func TestApplyReviewFixUpdatesAssetAndResolvesItem(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)
	fixID := seed.insertReviewFixDraft(t, itemID, "canonical_asset", assetID, map[string]any{"description": "Updated description"}, nil)

	var response ApplyReviewFixResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-fixes/"+fixID+"/apply", seed.ownerToken, seed.organizationID, map[string]any{"resolveReviewItem": true}, &response)
	if response.Status != "applied" || response.ReviewItemStatus == nil || *response.ReviewItemStatus != "resolved" {
		t.Fatalf("apply response = %+v", response)
	}
	var description, itemStatus string
	var manualOverride bool
	if err := seed.pool.QueryRow(seed.ctx, `SELECT description, manual_override FROM canonical_assets WHERE id = $1`, assetID).Scan(&description, &manualOverride); err != nil {
		t.Fatalf("read asset: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM review_items WHERE id = $1`, itemID).Scan(&itemStatus); err != nil {
		t.Fatalf("read review item: %v", err)
	}
	if description != "Updated description" || !manualOverride || itemStatus != "resolved" {
		t.Fatalf("description=%q manualOverride=%v itemStatus=%s", description, manualOverride, itemStatus)
	}
}

func TestApplyReviewFixDetectsChangedTarget(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)
	fixID := seed.insertReviewFixDraft(t, itemID, "canonical_asset", assetID, map[string]any{"description": "Updated description"}, nil)
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE canonical_assets SET description = 'Changed elsewhere' WHERE id = $1`, assetID); err != nil {
		t.Fatalf("change asset: %v", err)
	}
	assertAPIErrorCode(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-fixes/"+fixID+"/apply", seed.ownerToken, seed.organizationID, map[string]any{"resolveReviewItem": true}, http.StatusConflict, "TARGET_CHANGED")
}

func TestDismissReviewFix(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)
	fixID := seed.insertReviewFixDraft(t, itemID, "canonical_asset", assetID, map[string]any{"description": "Updated description"}, nil)

	var response DismissReviewFixResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-fixes/"+fixID+"/dismiss", seed.ownerToken, seed.organizationID, nil, &response)
	if response.Status != "dismissed" {
		t.Fatalf("dismiss response = %+v", response)
	}
}

func TestApplyReviewFixCanTriggerRegeneration(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = &fakeTemporalClient{}
	assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")
	itemID := seed.insertReviewItemForTarget(t, "open", "asset", "medium", "canonical_asset", assetID)
	fixID := seed.insertReviewFixDraft(t, itemID, "canonical_asset", assetID, map[string]any{"description": "Updated description"}, map[string]any{
		"targetType": "canonical_asset_image",
		"targetId":   assetID,
	})

	var response ApplyReviewFixResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/review-fixes/"+fixID+"/apply", seed.ownerToken, seed.organizationID, map[string]any{"resolveReviewItem": true, "triggerRegeneration": true}, &response)
	if response.WorkflowRunID == nil || *response.WorkflowRunID == "" {
		t.Fatalf("apply response = %+v", response)
	}
}

func (s *artifactPreviewSeed) insertReviewItemForTarget(t *testing.T, status, category, severity, entityType, entityID string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO review_items(
			organization_id, project_id, item_type, category, severity, title, description,
			entity_type, entity_id, status, metadata, created_by
		)
		VALUES ($1, $2, 'issue', $3, $4, 'Review item', 'Description', $5, NULLIF($6, '')::uuid, $7, '{"actions":[]}', $8)
		RETURNING id::text
	`, s.organizationID, s.projectID, category, severity, entityType, entityID, status, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert review item: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) insertReviewFixDraft(t *testing.T, itemID, entityType, entityID string, patch map[string]any, regenerate map[string]any) string {
	t.Helper()
	target, err := reviewpkg.LoadReviewFixTarget(s.ctx, s.pool, s.projectID, entityType, entityID)
	if err != nil {
		t.Fatalf("load review fix target: %v", err)
	}
	after := reviewpkg.ApplyReviewPatchPreview(target.Snapshot, patch)
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO review_fixes(
			organization_id, project_id, review_item_id, target_entity_type, target_entity_id,
			status, fix_type, title, explanation, before_snapshot, patch, after_preview, regenerate_request, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 'draft', 'patch', 'Fix title', 'Fix explanation', $6, $7, $8, $9, $10)
		RETURNING id::text
	`, s.organizationID, s.projectID, itemID, entityType, entityID, mustRawJSON(target.Snapshot), mustRawJSON(patch), mustRawJSON(after), rawNullableObject(regenerate), s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert review fix: %v", err)
	}
	return id
}
