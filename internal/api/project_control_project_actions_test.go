package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

func TestProjectControlProjectUpdateUsesCASAndIdempotentReplay(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var originalRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT revision FROM projects WHERE id = $1`, seed.projectID).Scan(&originalRevision); err != nil {
		t.Fatalf("read project revision: %v", err)
	}
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "test-key"},
	}
	arguments := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "project-update-shared-action-test",
		"expectedRevision": originalRevision, "name": "共享项目名称", "description": "共享项目简介",
	}
	updated := executeProjectControlTestAction(t, seed, identity, "project.update", arguments)
	if updated.CommandID == "" || updated.Status != string(projectcontrol.CommandSucceeded) {
		t.Fatalf("project.update result=%+v", updated)
	}
	replayed := executeProjectControlTestAction(t, seed, identity, "project.update", arguments)
	if replayed.CommandID != updated.CommandID {
		t.Fatalf("project.update replay command=%s want=%s", replayed.CommandID, updated.CommandID)
	}

	var name, description string
	var currentRevision int64
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT name, COALESCE(description, ''), revision
		FROM projects
		WHERE id = $1
	`, seed.projectID).Scan(&name, &description, &currentRevision); err != nil {
		t.Fatalf("read updated project: %v", err)
	}
	if name != "共享项目名称" || description != "共享项目简介" || currentRevision != originalRevision+1 {
		t.Fatalf("updated project name=%q description=%q revision=%d", name, description, currentRevision)
	}

	staleRaw, err := json.Marshal(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "project-update-stale-test",
		"expectedRevision": originalRevision, "name": "不应覆盖",
	})
	if err != nil {
		t.Fatalf("marshal stale project.update: %v", err)
	}
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "project.update", staleRaw)
	if err != nil {
		t.Fatalf("execute stale project.update: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "PROJECT_REVISION_CONFLICT" {
		t.Fatalf("stale project.update result=%+v", stale)
	}
}

func TestProjectUpdateRejectsProductionConfigurationFields(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"expectedRevision": 1,
		"aspectRatio":      "9:16",
	})
	if err != nil {
		t.Fatalf("marshal project update: %v", err)
	}
	_, err = decodeProjectUpdateActionInput(raw)
	if err == nil {
		t.Fatal("expected production configuration rejection")
	}
	typed, ok := videoproduction.AsError(err)
	if !ok || typed.Code != videoproduction.CodeConfigurationRebuildRequired {
		t.Fatalf("production configuration error=%v", err)
	}
}
