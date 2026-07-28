package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestProjectAgentSessionsSupportCommerceProjectsIntegration(t *testing.T) {
	handler, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var commerceProjectID string
	if err := seed.pool.QueryRow(seed.ctx, `
		INSERT INTO projects(
			organization_id, workspace_id, name, created_by,
			video_production_state, project_kind
		)
		VALUES ($1, $2, 'Commerce Agent Session Project', $3, 'unconfigured', 'commerce_video')
		RETURNING id::text
	`, seed.organizationID, seed.workspaceID, seed.ownerUserID).Scan(&commerceProjectID); err != nil {
		t.Fatalf("insert commerce project: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)
	`, commerceProjectID, seed.ownerUserID); err != nil {
		t.Fatalf("insert commerce project member: %v", err)
	}

	createPath := "/api/projects/" + commerceProjectID + "/agent/sessions"
	created := doAPIRequest(
		t, handler, http.MethodPost, createPath,
		seed.ownerToken, seed.organizationID,
		map[string]any{"title": "带货项目助手验收"},
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("create project agent session status=%d body=%s", created.Code, created.Body.String())
	}
	var createdEnvelope struct {
		Data AgentSession `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode created session: %v", err)
	}
	if createdEnvelope.Data.AgentType != projectAgentSessionType ||
		createdEnvelope.Data.ProjectID != commerceProjectID {
		t.Fatalf("created project agent session = %+v", createdEnvelope.Data)
	}

	listed := doAPIRequest(
		t, handler, http.MethodGet, createPath,
		seed.ownerToken, seed.organizationID, nil,
	)
	if listed.Code != http.StatusOK {
		t.Fatalf("list project agent sessions status=%d body=%s", listed.Code, listed.Body.String())
	}
	var listEnvelope struct {
		Data struct {
			Items []AgentSession `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode session list: %v", err)
	}
	if len(listEnvelope.Data.Items) != 1 ||
		listEnvelope.Data.Items[0].ID != createdEnvelope.Data.ID {
		t.Fatalf("project agent sessions = %+v", listEnvelope.Data.Items)
	}

	messages := doAPIRequest(
		t, handler, http.MethodGet,
		createPath+"/"+createdEnvelope.Data.ID+"/messages",
		seed.ownerToken, seed.organizationID, nil,
	)
	if messages.Code != http.StatusOK || !json.Valid(messages.Body.Bytes()) {
		t.Fatalf("list project agent messages status=%d body=%s", messages.Code, messages.Body.String())
	}

	assertAPIErrorCode(
		t, handler, http.MethodPost,
		"/api/projects/"+commerceProjectID+"/script-agent/sessions",
		seed.ownerToken, seed.organizationID,
		map[string]any{"title": "错误入口"},
		http.StatusConflict, "PROJECT_KIND_MISMATCH",
	)
}
