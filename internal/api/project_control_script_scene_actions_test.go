package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func TestProjectControlScriptSceneLifecycleUsesSharedRevisionedCommands(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var scriptID, versionID, sceneID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, '场景生命周期剧本', 'active', $3)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, seed.ownerUserID).Scan(&scriptID); err != nil {
		t.Fatalf("insert script: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, '场景正文', 'markdown', 'active', '{}', $4)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, seed.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate script version: %v", err)
	}
	var initialRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_scenes(
			organization_id, project_id, script_id, script_version_id,
			scene_index, scene_no, title, summary, characters, scenes, props,
			source_event_ids, content, content_format, review_status, stale_state, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 0, 1, '旧场景', '旧摘要', '[]', '[]', '[]', '[]', '旧场景正文', 'markdown', 'pending', 'fresh', '{}', $5)
		RETURNING id::text, revision
	`, seed.organizationID, seed.projectID, scriptID, versionID, seed.ownerUserID).Scan(&sceneID, &initialRevision); err != nil {
		t.Fatalf("insert script scene: %v", err)
	}

	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "script-scene-test-key"},
	}
	updateArguments := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-scene-update-test",
		"sceneId": sceneID, "expectedRevision": initialRevision,
		"patch": map[string]any{"title": "新场景", "characters": []string{"方源", "白凝冰"}, "content": "新场景正文"},
	}
	updated := executeProjectControlTestAction(t, seed, identity, "script_scene.update", updateArguments)
	var updatedData struct {
		Scene workflows.ScriptSceneRecord `json:"scene"`
	}
	if err := json.Unmarshal(updated.Data, &updatedData); err != nil {
		t.Fatalf("decode updated scene: %v", err)
	}
	if updatedData.Scene.Title != "新场景" || updatedData.Scene.Content != "新场景正文" || updatedData.Scene.Revision <= initialRevision || updatedData.Scene.StaleState != "needs_regeneration" {
		t.Fatalf("updated scene=%+v", updatedData.Scene)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "script_scene.update", updateArguments)
	if replayed.CommandID != updated.CommandID {
		t.Fatalf("replayed scene command=%s want=%s", replayed.CommandID, updated.CommandID)
	}

	reviewed := executeProjectControlTestAction(t, seed, identity, "script_scene.review", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-scene-review-test",
		"sceneId": sceneID, "expectedRevision": updatedData.Scene.Revision,
		"reviewStatus": "approved", "note": "结构已确认",
	})
	var reviewedData scriptSceneReviewActionOutcome
	if err := json.Unmarshal(reviewed.Data, &reviewedData); err != nil {
		t.Fatalf("decode reviewed scene: %v", err)
	}
	if reviewedData.ReviewStatus != "approved" || reviewedData.Revision <= updatedData.Scene.Revision {
		t.Fatalf("reviewed scene=%+v", reviewedData)
	}

	staleRaw, err := json.Marshal(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-scene-stale-test",
		"sceneId": sceneID, "expectedRevision": updatedData.Scene.Revision,
		"patch": map[string]any{"title": "不应覆盖"},
	})
	if err != nil {
		t.Fatalf("marshal stale scene update: %v", err)
	}
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "script_scene.update", staleRaw)
	if err != nil {
		t.Fatalf("execute stale scene update: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "SCRIPT_SCENE_REVISION_CONFLICT" {
		t.Fatalf("stale scene update=%+v", stale)
	}

	deleted := executeProjectControlTestAction(t, seed, identity, "script_scene.delete", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "script-scene-delete-test",
		"sceneId": sceneID, "expectedRevision": reviewedData.Revision, "reason": "测试归档",
	})
	var deletedData scriptSceneDeleteActionOutcome
	if err := json.Unmarshal(deleted.Data, &deletedData); err != nil {
		t.Fatalf("decode deleted scene: %v", err)
	}
	if !deletedData.Deleted || deletedData.Mode != "archive" || deletedData.Revision <= reviewedData.Revision {
		t.Fatalf("deleted scene=%+v", deletedData)
	}
	var deletedAtPresent bool
	var finalRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT deleted_at IS NOT NULL, revision FROM script_scenes WHERE id = $1`, sceneID).Scan(&deletedAtPresent, &finalRevision); err != nil {
		t.Fatalf("read archived scene: %v", err)
	}
	if !deletedAtPresent || finalRevision != deletedData.Revision {
		t.Fatalf("archived scene deleted=%v revision=%d want=%d", deletedAtPresent, finalRevision, deletedData.Revision)
	}
}
