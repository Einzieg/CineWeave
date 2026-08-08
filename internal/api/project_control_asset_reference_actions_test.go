package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlAssetReferenceLifecycleIsIdempotentAndRevisionSafe(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assetID := seed.insertCanonicalAsset(t, "character", "Reference Lifecycle", "approved", "")
	var revision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM canonical_assets WHERE id = $1`, assetID).Scan(&revision); err != nil {
		t.Fatalf("read initial asset revision: %v", err)
	}
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "asset-reference-test-key"},
	}

	firstArguments := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "asset-reference-create-first",
		"assetId": assetID, "expectedRevision": revision,
		"title": "正面参考", "storageKey": "references/front.png", "mimeType": "image/png",
	}
	first := executeProjectControlTestAction(t, seed, identity, "asset.reference.create", firstArguments)
	var firstData assetReferenceCreateActionOutcome
	if err := json.Unmarshal(first.Data, &firstData); err != nil {
		t.Fatalf("decode first reference result: %v", err)
	}
	if firstData.Reference.ID == "" || !firstData.Reference.IsPrimary || firstData.Revision <= revision {
		t.Fatalf("first reference result=%+v", firstData)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "asset.reference.create", firstArguments)
	if replayed.CommandID != first.CommandID {
		t.Fatalf("replayed command=%s want=%s", replayed.CommandID, first.CommandID)
	}
	var referenceCount, artifactCount, mediaCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM asset_references WHERE asset_id = $1`, assetID).Scan(&referenceCount); err != nil {
		t.Fatalf("count references after replay: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM artifacts WHERE project_id = $1 AND storage_key = 'references/front.png'`, seed.projectID).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts after replay: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM media_files WHERE project_id = $1 AND storage_key = 'references/front.png'`, seed.projectID).Scan(&mediaCount); err != nil {
		t.Fatalf("count media after replay: %v", err)
	}
	if referenceCount != 1 || artifactCount != 1 || mediaCount != 1 {
		t.Fatalf("replay duplicated data references=%d artifacts=%d media=%d", referenceCount, artifactCount, mediaCount)
	}

	second := executeProjectControlTestAction(t, seed, identity, "asset.reference.create", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "asset-reference-create-second",
		"assetId": assetID, "expectedRevision": firstData.Revision,
		"title": "侧面参考", "storageKey": "references/side.png", "mimeType": "image/png",
	})
	var secondData assetReferenceCreateActionOutcome
	if err := json.Unmarshal(second.Data, &secondData); err != nil {
		t.Fatalf("decode second reference result: %v", err)
	}
	if secondData.Reference.ID == "" || secondData.Reference.IsPrimary || secondData.Revision <= firstData.Revision {
		t.Fatalf("second reference result=%+v", secondData)
	}

	selected := executeProjectControlTestAction(t, seed, identity, "asset.reference.set_primary", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "asset-reference-select-second",
		"assetId": assetID, "referenceId": secondData.Reference.ID, "expectedRevision": secondData.Revision,
	})
	var selectedData assetReferenceSetPrimaryActionOutcome
	if err := json.Unmarshal(selected.Data, &selectedData); err != nil {
		t.Fatalf("decode selected reference result: %v", err)
	}
	if !selectedData.Reference.IsPrimary || selectedData.Revision <= secondData.Revision {
		t.Fatalf("selected reference result=%+v", selectedData)
	}

	staleRaw, err := json.Marshal(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "asset-reference-stale-select",
		"assetId": assetID, "referenceId": firstData.Reference.ID, "expectedRevision": secondData.Revision,
	})
	if err != nil {
		t.Fatalf("marshal stale reference selection: %v", err)
	}
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "asset.reference.set_primary", staleRaw)
	if err != nil {
		t.Fatalf("execute stale reference selection: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "ASSET_REVISION_CONFLICT" {
		t.Fatalf("stale reference selection=%+v", stale)
	}

	deleted := executeProjectControlTestAction(t, seed, identity, "asset.reference.delete", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "asset-reference-delete-second",
		"assetId": assetID, "referenceId": secondData.Reference.ID,
		"expectedRevision": selectedData.Revision, "reason": "测试主图回退",
	})
	var deletedData assetReferenceDeleteActionOutcome
	if err := json.Unmarshal(deleted.Data, &deletedData); err != nil {
		t.Fatalf("decode deleted reference result: %v", err)
	}
	if !deletedData.Deleted || deletedData.ReplacementPrimaryRef == nil || deletedData.ReplacementPrimaryRef.ID != firstData.Reference.ID || deletedData.Revision <= selectedData.Revision {
		t.Fatalf("deleted reference result=%+v", deletedData)
	}
	if deletedData.ArtifactDeleted || deletedData.MediaDeleted {
		t.Fatalf("reference archive unexpectedly deleted media: %+v", deletedData)
	}

	var firstPrimary bool
	var secondStatus string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT is_primary FROM asset_references WHERE id = $1`, firstData.Reference.ID).Scan(&firstPrimary); err != nil {
		t.Fatalf("read replacement primary reference: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM asset_references WHERE id = $1`, secondData.Reference.ID).Scan(&secondStatus); err != nil {
		t.Fatalf("read archived reference: %v", err)
	}
	if !firstPrimary || secondStatus != "archived" {
		t.Fatalf("reference terminal state firstPrimary=%v secondStatus=%s", firstPrimary, secondStatus)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM artifacts WHERE id IN ($1, $2)`, firstData.Reference.ArtifactID, secondData.Reference.ArtifactID).Scan(&artifactCount); err != nil {
		t.Fatalf("count preserved reference artifacts: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM media_files WHERE id IN ($1, $2)`, firstData.Reference.MediaFileID, secondData.Reference.MediaFileID).Scan(&mediaCount); err != nil {
		t.Fatalf("count preserved reference media: %v", err)
	}
	if artifactCount != 2 || mediaCount != 2 {
		t.Fatalf("archiving reference removed media artifacts=%d media=%d", artifactCount, mediaCount)
	}
}
