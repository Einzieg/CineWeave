package api

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlFinalVideoActionsShareRevisionedDomainPath(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, handler, seed, "Codex Final Video Project")

	timelineID := insertProjectTimeline(t, seed)
	firstID := insertFinalVideoVersion(t, seed, timelineID, 1, "active")
	secondID := insertFinalVideoVersion(t, seed, timelineID, 2, "ready")
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET active_final_video_version_id = $2 WHERE id = $1`, seed.projectID, firstID); err != nil {
		t.Fatalf("set active final video: %v", err)
	}

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

	activated := executeProjectControlTestAction(t, seed, identity, "final_video.activate", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "final-video-activate-codex",
		"versionId": secondID, "expectedRevision": finalVideoRevision(t, seed, secondID),
	})
	var activatedData struct {
		FinalVideo FinalVideoVersion `json:"finalVideo"`
		VersionID  string            `json:"versionId"`
		Revision   int64             `json:"revision"`
	}
	decodeProjectControlResultData(t, activated, &activatedData)
	if activatedData.VersionID != secondID || activatedData.FinalVideo.Status != "active" || activatedData.Revision != activatedData.FinalVideo.Revision || activatedData.Revision < 2 {
		t.Fatalf("activated final video=%+v", activatedData)
	}

	stale := executeProjectControlTestActionAllowFailure(t, seed, identity, "final_video.activate", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "final-video-activate-stale",
		"versionId": secondID, "expectedRevision": 1,
	})
	if stale.Error == nil || stale.Error.Code != "FINAL_VIDEO_REVISION_CONFLICT" {
		t.Fatalf("stale activation=%+v", stale)
	}

	deleted := executeProjectControlTestAction(t, seed, identity, "final_video.delete", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "final-video-delete-codex",
		"versionId": secondID, "expectedRevision": activatedData.Revision, "confirmActive": true,
	})
	var deletedData struct {
		Deleted   bool   `json:"deleted"`
		VersionID string `json:"versionId"`
	}
	decodeProjectControlResultData(t, deleted, &deletedData)
	if !deletedData.Deleted || deletedData.VersionID != secondID {
		t.Fatalf("deleted final video=%+v", deletedData)
	}

	var commandCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM project_control_commands
		WHERE project_id = $1 AND controller_type = 'codex_mcp'
		  AND action_name IN ('final_video.activate', 'final_video.delete')
		  AND status = 'succeeded'
	`, seed.projectID).Scan(&commandCount); err != nil {
		t.Fatalf("count final video commands: %v", err)
	}
	if commandCount != 2 {
		t.Fatalf("succeeded final video command count=%d, want 2", commandCount)
	}
}

func executeProjectControlTestActionAllowFailure(
	t *testing.T,
	seed *artifactPreviewSeed,
	identity controlmcp.Identity,
	action string,
	input map[string]any,
) projectcontrol.Result {
	t.Helper()
	result, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, action, mustRawJSON(input))
	if err != nil {
		t.Fatalf("execute %s: %v", action, err)
	}
	return result
}
