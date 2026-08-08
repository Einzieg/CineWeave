package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestScriptRewritePreviewReplaysCompletedCommandRun(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID, versionID := insertScriptRewriteFixture(t, seed)
	command := createManualProjectControlCommand(t, seed, "script.rewrite_preview", "script-rewrite-preview-replay")
	var runID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO agent_runs(
			organization_id, project_id, agent_type, task_type, status, input, output,
			project_control_command_id, created_by, started_at, completed_at
		)
		VALUES (
			$1, $2, 'project_agent', 'rewrite_preview', 'succeeded', '{}',
			jsonb_build_object(
				'scriptId', $3::text, 'versionId', $4::text,
				'content', '已完成的改写预览', 'contentFormat', 'markdown',
				'previewOnly', true, 'modelId', 'model-replay'
			),
			$5, $6, now(), now()
		)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, versionID, command.ID, seed.ownerUserID).Scan(&runID); err != nil {
		t.Fatalf("insert replay agent run: %v", err)
	}
	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	raw := mustRawJSON(map[string]any{
		"scriptId": scriptID, "versionId": versionID, "instruction": "不应再次调用供应商",
	})
	result, err := seed.apiServer.executeScriptRewritePreviewAsyncAction(
		seed.ctx,
		auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		project,
		command,
		raw,
	)
	if err != nil {
		t.Fatalf("replay script preview: %v", err)
	}
	if result.Data["content"] != "已完成的改写预览" || result.Data["agentRunId"] != runID || result.Data["idempotentReplay"] != true {
		t.Fatalf("preview replay result=%+v", result.Data)
	}
	var count int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*) FROM agent_runs
		WHERE project_control_command_id = $1 AND task_type = 'rewrite_preview'
	`, command.ID).Scan(&count); err != nil {
		t.Fatalf("count replay agent runs: %v", err)
	}
	if count != 1 {
		t.Fatalf("replay agent run count=%d, want 1", count)
	}
}

func TestScriptRewriteReplaysCommittedVersion(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID, previousVersionID := insertScriptRewriteFixture(t, seed)
	command := createManualProjectControlCommand(t, seed, "script.rewrite", "script-rewrite-version-replay")
	var versionID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, source_type, metadata, created_by
		)
		VALUES (
			$1, $2, $3, 2, 2, '崩溃前已提交的改写正文', 'markdown', 'active', 'agent_rewrite',
			jsonb_build_object(
				'projectControlCommandId', $4::text,
				'agentRunId', 'run-before-crash',
				'modelId', 'model-replay',
				'activated', true,
				'previousVersionId', $5::text
			),
			$6
		)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, command.ID, previousVersionID, seed.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert replay script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate replay script version: %v", err)
	}
	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	result, err := seed.apiServer.executeScriptRewriteAsyncAction(
		seed.ctx,
		auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		project,
		command,
		mustRawJSON(map[string]any{
			"scriptId": scriptID, "versionId": previousVersionID,
			"instruction": "不应再次调用供应商", "activate": true,
		}),
	)
	if err != nil {
		t.Fatalf("replay script rewrite: %v", err)
	}
	if result.Data["versionId"] != versionID || result.Data["content"] != "崩溃前已提交的改写正文" || result.Data["idempotentReplay"] != true {
		t.Fatalf("rewrite replay result=%+v", result.Data)
	}
	var count int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*) FROM script_versions
		WHERE project_id = $1 AND metadata->>'projectControlCommandId' = $2
	`, seed.projectID, command.ID).Scan(&count); err != nil {
		t.Fatalf("count replay versions: %v", err)
	}
	if count != 1 {
		t.Fatalf("replay version count=%d, want 1", count)
	}
}

func insertScriptRewriteFixture(t *testing.T, seed *artifactPreviewSeed) (string, string) {
	t.Helper()
	var scriptID, versionID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO scripts(organization_id, project_id, title, status, created_by)
		VALUES ($1, $2, '待改写剧本', 'active', $3)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, seed.ownerUserID).Scan(&scriptID); err != nil {
		t.Fatalf("insert rewrite script: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, status, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, '原始剧本正文', 'markdown', 'active', '{}', $4)
		RETURNING id::text
	`, seed.organizationID, seed.projectID, scriptID, seed.ownerUserID).Scan(&versionID); err != nil {
		t.Fatalf("insert rewrite script version: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		t.Fatalf("activate rewrite fixture: %v", err)
	}
	return scriptID, versionID
}

func createManualProjectControlCommand(t *testing.T, seed *artifactPreviewSeed, actionName, idempotencyKey string) projectcontrol.Command {
	t.Helper()
	descriptor, ok := seed.apiServer.projectControl.registry.Get(actionName)
	if !ok {
		t.Fatalf("project control descriptor %s is missing", actionName)
	}
	command, _, err := seed.apiServer.projectControl.repository.Create(seed.ctx, projectcontrol.CreateCommand{
		OrganizationID: seed.organizationID,
		WorkspaceID:    seed.workspaceID,
		ProjectID:      seed.projectID,
		ActorUserID:    seed.ownerUserID,
		ControllerType: projectcontrol.ControllerManual,
		Descriptor:     descriptor,
		Input:          json.RawMessage(`{}`),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatalf("create project control command %s: %v", actionName, err)
	}
	return command
}
