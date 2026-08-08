package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlPromptVersionLifecycleUsesSharedActions(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, handler, seed, "Codex Prompt Project")

	templateKey := "project_control_prompt_" + randomStorageSegment()
	var template PromptTemplate
	doAPISuccess(t, handler, http.MethodPost, "/api/prompt-templates", seed.ownerToken, seed.organizationID, map[string]any{
		"organizationId": seed.organizationID, "templateKey": templateKey,
		"name": "Project Control Prompt", "purpose": "test", "modality": "text", "taskType": "text.generate",
	}, &template)
	identity := projectControlTestCodexIdentity(t, seed)

	createInput := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "prompt-version-create-codex",
		"templateId": template.ID, "title": "Codex v1", "content": "你好，{{ input.name }}。",
		"contentFormat": "text", "variablesSchema": map[string]any{}, "metadata": map[string]any{"test": true},
	}
	created := executeProjectControlTestAction(t, seed, identity, "prompt.create_version", createInput)
	var createdData promptVersionCreateActionResult
	decodeProjectControlResultData(t, created, &createdData)
	if createdData.Version.ID == "" || createdData.Version.Status != "draft" || createdData.Activated {
		t.Fatalf("created prompt version=%+v", createdData)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "prompt.create_version", createInput)
	if replayed.CommandID != created.CommandID {
		t.Fatalf("replayed command=%s want=%s", replayed.CommandID, created.CommandID)
	}

	activated := executeProjectControlTestAction(t, seed, identity, "prompt.activate_version", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "prompt-version-activate-codex",
		"versionId": createdData.Version.ID,
	})
	var activatedData promptVersionActivateActionResult
	decodeProjectControlResultData(t, activated, &activatedData)
	if activatedData.Version.Status != "active" || activatedData.TemplateID != template.ID {
		t.Fatalf("activated prompt version=%+v", activatedData)
	}

	rendered := executeProjectControlTestAction(t, seed, identity, "prompt.render_test", map[string]any{
		"projectId": seed.projectID, "templateKey": templateKey,
		"input": map[string]any{"name": "CineWeave"},
	})
	var renderedData promptRenderActionResult
	if err := jsonUnmarshalProjectControlResult(rendered, &renderedData); err != nil {
		t.Fatalf("decode rendered prompt: %v", err)
	}
	if renderedData.PromptVersionID != createdData.Version.ID || renderedData.Text != "你好，CineWeave。" || !strings.HasPrefix(renderedData.RenderedHash, "sha256:") {
		t.Fatalf("rendered prompt=%+v", renderedData)
	}

	var commandCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM project_control_commands
		WHERE project_id = $1 AND controller_type = 'codex_mcp'
		  AND action_name LIKE 'prompt.%' AND status = 'succeeded'
	`, seed.projectID).Scan(&commandCount); err != nil {
		t.Fatalf("count prompt commands: %v", err)
	}
	if commandCount != 2 {
		t.Fatalf("prompt command count=%d, want 2", commandCount)
	}
}

func projectControlTestCodexIdentity(t *testing.T, seed *artifactPreviewSeed) controlmcp.Identity {
	t.Helper()
	var controlKeyID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text FROM user_control_keys WHERE user_id = $1 AND status = 'active'
	`, seed.ownerUserID).Scan(&controlKeyID); err != nil {
		t.Fatalf("read control key: %v", err)
	}
	return controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: controlKeyID},
	}
}

func jsonUnmarshalProjectControlResult(result projectcontrol.Result, target any) error {
	return json.Unmarshal(result.Data, target)
}
