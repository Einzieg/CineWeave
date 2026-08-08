package api

import (
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlCharacterVoiceActionsShareRevisionedDomainPath(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	createBody := map[string]any{
		"characterName": "旁白",
		"displayName":   "默认旁白",
		"voiceKey":      "voice-zh-default",
		"language":      "zh-CN",
	}
	assertAPIErrorCode(t, handler, http.MethodPost,
		"/api/projects/"+seed.projectID+"/character-voices", seed.ownerToken, seed.organizationID,
		createBody, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED")

	var created CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPost,
		"/api/projects/"+seed.projectID+"/character-voices", seed.ownerToken, seed.organizationID,
		createBody, &created, map[string]string{"Idempotency-Key": "voice-create-manual"})
	if created.ID == "" || created.Revision != 1 || !created.IsDefault {
		t.Fatalf("created voice=%+v", created)
	}

	var manualCreateCommandID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT id::text
		FROM project_control_commands
		WHERE project_id = $1 AND controller_type = 'manual' AND action_name = 'character_voice.create'
	`, seed.projectID).Scan(&manualCreateCommandID); err != nil {
		t.Fatalf("read manual create command: %v", err)
	}

	var updated CharacterVoiceProfile
	doAPISuccess(t, handler, http.MethodPatch,
		"/api/projects/"+seed.projectID+"/character-voices/"+created.ID, seed.ownerToken, seed.organizationID,
		map[string]any{"expectedRevision": created.Revision, "displayName": "主旁白"}, &updated,
		map[string]string{"Idempotency-Key": "voice-update-manual"})
	if updated.Revision != 2 || updated.DisplayName != "主旁白" {
		t.Fatalf("updated voice=%+v", updated)
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
	codexUpdate := executeProjectControlTestAction(t, seed, identity, "character_voice.update", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "voice-update-codex",
		"voiceId": created.ID, "expectedRevision": updated.Revision,
		"patch": map[string]any{"instructions": "沉稳、清晰"},
	})
	var codexUpdateData struct {
		Voice CharacterVoiceProfile `json:"voice"`
	}
	decodeProjectControlResultData(t, codexUpdate, &codexUpdateData)
	if codexUpdateData.Voice.Revision != 3 || codexUpdateData.Voice.Instructions == nil || *codexUpdateData.Voice.Instructions != "沉稳、清晰" {
		t.Fatalf("Codex updated voice=%+v", codexUpdateData.Voice)
	}

	staleRaw := mustRawJSON(map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "voice-update-stale",
		"voiceId": created.ID, "expectedRevision": 1,
		"patch": map[string]any{"displayName": "不应覆盖"},
	})
	stale, err := seed.apiServer.projectControl.Execute(seed.ctx, identity, "character_voice.update", staleRaw)
	if err != nil {
		t.Fatalf("execute stale voice update: %v", err)
	}
	if stale.Error == nil || stale.Error.Code != "CHARACTER_VOICE_REVISION_CONFLICT" {
		t.Fatalf("stale voice result=%+v", stale)
	}

	recorder := doAPIRequest(t, handler, http.MethodDelete,
		"/api/projects/"+seed.projectID+"/character-voices/"+created.ID, seed.ownerToken, seed.organizationID,
		map[string]any{"expectedRevision": codexUpdateData.Voice.Revision},
		map[string]string{"Idempotency-Key": "voice-delete-manual"})
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete voice status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var status string
	var revision int64
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status, revision FROM character_voice_profiles WHERE id = $1`, created.ID).Scan(&status, &revision); err != nil {
		t.Fatalf("read archived voice: %v", err)
	}
	if status != "archived" || revision != 4 {
		t.Fatalf("archived voice status=%s revision=%d", status, revision)
	}
}
