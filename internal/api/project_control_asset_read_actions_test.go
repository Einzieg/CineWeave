package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlAssetReadsAreFilteredPagedAndDetailed(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	insertAsset := func(assetType, name, status, basePrompt, consistencyPrompt string) string {
		t.Helper()
		var id string
		if err := seed.pool.QueryRow(seed.ctx, `
			INSERT INTO canonical_assets(
				organization_id, project_id, asset_type, name, description,
				base_prompt, consistency_prompt, status, review_status,
				stale_state, created_by
			)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, 'approved', 'fresh', $9)
			RETURNING id::text
		`, seed.organizationID, seed.projectID, assetType, name, name+"描述", basePrompt, consistencyPrompt, status, seed.ownerUserID).Scan(&id); err != nil {
			t.Fatalf("insert canonical asset %s: %v", name, err)
		}
		return id
	}

	primaryID := insertAsset("character", "主角", "prompt_ready", "角色基础提示词", "角色一致性提示词")
	insertAsset("prop", "道具", "draft", "", "")
	archivedID := insertAsset("scene", "旧场景", "archived", "场景提示词", "场景一致性")
	insertAsset("character", "同名角色", "draft", "", "")
	insertAsset("character", "同名角色", "draft", "", "")

	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title,
			storage_key, is_primary, status, metadata, created_by
		)
		VALUES ($1, $2, $3, 'uploaded', '正面参考', 'projects/test/primary.png', true, 'ready', '{}', $4)
	`, seed.organizationID, seed.projectID, primaryID, seed.ownerUserID); err != nil {
		t.Fatalf("insert asset reference: %v", err)
	}

	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "test-key"},
	}
	first := executeProjectControlTestAction(t, seed, identity, "asset.list", map[string]any{
		"projectId": seed.projectID, "limit": 2,
	})
	var firstPage assetListActionPage
	if err := json.Unmarshal(first.Data, &firstPage); err != nil {
		t.Fatalf("decode first asset page: %v", err)
	}
	if len(firstPage.Items) != 2 || firstPage.NextCursor == "" {
		t.Fatalf("first asset page=%+v", firstPage)
	}
	for _, item := range firstPage.Items {
		if item.ID == archivedID || item.Status == "archived" {
			t.Fatalf("default asset list included archived item: %+v", item)
		}
	}

	second := executeProjectControlTestAction(t, seed, identity, "asset.list", map[string]any{
		"projectId": seed.projectID, "limit": 2, "cursor": firstPage.NextCursor,
	})
	var secondPage assetListActionPage
	if err := json.Unmarshal(second.Data, &secondPage); err != nil {
		t.Fatalf("decode second asset page: %v", err)
	}
	if len(secondPage.Items) != 2 || secondPage.NextCursor != "" {
		t.Fatalf("second asset page=%+v", secondPage)
	}

	archived := executeProjectControlTestAction(t, seed, identity, "asset.list", map[string]any{
		"projectId": seed.projectID, "status": "archived",
	})
	var archivedPage assetListActionPage
	if err := json.Unmarshal(archived.Data, &archivedPage); err != nil {
		t.Fatalf("decode archived asset page: %v", err)
	}
	if len(archivedPage.Items) != 1 || archivedPage.Items[0].ID != archivedID {
		t.Fatalf("archived asset page=%+v", archivedPage)
	}

	promptReady := executeProjectControlTestAction(t, seed, identity, "asset.list", map[string]any{
		"projectId": seed.projectID, "promptReady": true,
	})
	var promptReadyPage assetListActionPage
	if err := json.Unmarshal(promptReady.Data, &promptReadyPage); err != nil {
		t.Fatalf("decode prompt-ready page: %v", err)
	}
	if len(promptReadyPage.Items) != 1 || promptReadyPage.Items[0].ID != primaryID || !promptReadyPage.Items[0].PromptReady || promptReadyPage.Items[0].ReferenceCount != 1 {
		t.Fatalf("prompt-ready page=%+v", promptReadyPage)
	}

	detail := executeProjectControlTestAction(t, seed, identity, "asset.get", map[string]any{
		"projectId": seed.projectID, "assetId": primaryID,
	})
	var detailData struct {
		Asset CanonicalAsset `json:"asset"`
	}
	if err := json.Unmarshal(detail.Data, &detailData); err != nil {
		t.Fatalf("decode asset detail: %v", err)
	}
	if detailData.Asset.ID != primaryID || detailData.Asset.Revision < 1 || detailData.Asset.PromptRevision < 1 || len(detailData.Asset.References) != 1 || detailData.Asset.ReferenceCount != 1 {
		t.Fatalf("asset detail=%+v", detailData.Asset)
	}
	if detailData.Asset.References[0].Title == nil || *detailData.Asset.References[0].Title != "正面参考" {
		t.Fatalf("asset reference=%+v", detailData.Asset.References[0])
	}

	references := executeProjectControlTestAction(t, seed, identity, "asset.reference.list", map[string]any{
		"projectId": seed.projectID, "assetId": primaryID, "status": "all", "limit": 1,
	})
	var referencePage assetReferenceListActionPage
	if err := json.Unmarshal(references.Data, &referencePage); err != nil {
		t.Fatalf("decode asset reference page: %v", err)
	}
	if referencePage.AssetID != primaryID || len(referencePage.Items) != 1 || !referencePage.Items[0].IsPrimary {
		t.Fatalf("asset reference page=%+v", referencePage)
	}

	impactResult := executeProjectControlTestAction(t, seed, identity, "asset.impact", map[string]any{
		"projectId": seed.projectID, "assetId": primaryID,
	})
	var impactData struct {
		Impact OutputImpact `json:"impact"`
	}
	if err := json.Unmarshal(impactResult.Data, &impactData); err != nil {
		t.Fatalf("decode asset impact: %v", err)
	}
	if impactData.Impact.EntityID != primaryID || !impactData.Impact.CanDelete || impactData.Impact.RecommendedMode != "archive" {
		t.Fatalf("asset impact=%+v", impactData.Impact)
	}

	ambiguousRaw, err := json.Marshal(map[string]any{
		"projectId": seed.projectID, "assetName": "同名角色",
	})
	if err != nil {
		t.Fatalf("marshal ambiguous asset.get: %v", err)
	}
	ambiguous, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "asset.get", ambiguousRaw)
	if err != nil {
		t.Fatalf("execute ambiguous asset.get: %v", err)
	}
	if ambiguous.Error == nil || ambiguous.Error.Code != "AMBIGUOUS_ASSET_NAME" {
		t.Fatalf("ambiguous asset.get=%+v", ambiguous)
	}
}
