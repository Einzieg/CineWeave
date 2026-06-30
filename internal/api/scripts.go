package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

type Script struct {
	ID               string         `json:"id"`
	OrganizationID   string         `json:"organizationId"`
	ProjectID        string         `json:"projectId"`
	SourceID         *string        `json:"sourceId,omitempty"`
	Title            string         `json:"title"`
	Status           string         `json:"status"`
	CurrentVersionID *string        `json:"currentVersionId,omitempty"`
	CreatedBy        *string        `json:"createdBy,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	CurrentVersion   *ScriptVersion `json:"currentVersion,omitempty"`
}

type ScriptVersion struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       string          `json:"projectId"`
	ScriptID        string          `json:"scriptId"`
	Version         int             `json:"version"`
	Content         string          `json:"content"`
	ContentFormat   string          `json:"contentFormat"`
	SourceType      *string         `json:"sourceType,omitempty"`
	PromptVersionID *string         `json:"promptVersionId,omitempty"`
	PromptHash      *string         `json:"promptHash,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedBy       *string         `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
}

type AgentSession struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	ProjectID      string    `json:"projectId"`
	AgentType      string    `json:"agentType"`
	Title          *string   `json:"title,omitempty"`
	Status         string    `json:"status"`
	CreatedBy      *string   `json:"createdBy,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type AgentMessage struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	SessionID      string          `json:"sessionId"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"createdAt"`
}

func (s *Server) listScripts(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), scriptSelectSQL(`
		WHERE s.project_id = $1
		ORDER BY s.created_at DESC
	`), project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]Script, 0)
	for rows.Next() {
		item, err := scanScript(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createScript(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		SourceID      *string         `json:"sourceId"`
		Title         string          `json:"title"`
		Content       string          `json:"content"`
		ContentFormat string          `json:"contentFormat"`
		SourceType    *string         `json:"sourceType"`
		Metadata      json.RawMessage `json:"metadata"`
	}
	if !decode(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "title is required", nil, false)
		return
	}
	if req.SourceID != nil && strings.TrimSpace(*req.SourceID) != "" {
		if _, err := s.projectSource(r, project.ID, strings.TrimSpace(*req.SourceID)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	contentFormat := strings.TrimSpace(req.ContentFormat)
	if contentFormat == "" {
		contentFormat = "markdown"
	}
	if !validScriptContentFormat(contentFormat) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "contentFormat is invalid", nil, false)
		return
	}
	metadata, ok := jsonObjectOrDefault(w, r, req.Metadata)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanScript(tx.QueryRow(r.Context(), scriptInsertSQL(), project.OrganizationID, project.ID, req.SourceID, title, "draft", principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	content := strings.TrimSpace(req.Content)
	if content != "" {
		version, err := insertScriptVersionTx(r, tx, project, item.ID, 1, content, contentFormat, req.SourceType, "", "", metadata, principal.UserID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, item.ID, version.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
		item.CurrentVersionID = &version.ID
		item.Status = "active"
		item.CurrentVersion = &version
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getScript(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	item, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if item.CurrentVersionID != nil {
		version, err := s.scriptVersion(r, project.ID, item.ID, *item.CurrentVersionID)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.CurrentVersion = &version
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateScript(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	current, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		SourceID *string `json:"sourceId"`
		Title    *string `json:"title"`
		Status   *string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	title := current.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}
	status := current.Status
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	if title == "" || !validScriptStatus(status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "script fields are invalid", nil, false)
		return
	}
	if req.SourceID != nil && strings.TrimSpace(*req.SourceID) != "" {
		if _, err := s.projectSource(r, project.ID, strings.TrimSpace(*req.SourceID)); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	item, err := scanScript(s.db.QueryRow(r.Context(), scriptSelectSQL(`
		WHERE s.id = $1 AND s.project_id = $2
	`), current.ID, project.ID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	err = s.db.QueryRow(r.Context(), `
		UPDATE scripts
		SET title = $3,
		    source_id = COALESCE($4, source_id),
		    status = $5
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, source_id, title, status, current_version_id, created_by, created_at, updated_at
	`, current.ID, project.ID, title, req.SourceID, status).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.SourceID, &item.Title, &item.Status,
		&item.CurrentVersionID, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listScriptVersions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	if _, err := s.script(r, project.ID, r.PathValue("scriptId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, script_id, version, content, content_format,
		       source_type, prompt_version_id, prompt_hash, metadata, created_by, created_at
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2
		ORDER BY version DESC
	`, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ScriptVersion, 0)
	for rows.Next() {
		item, err := scanScriptVersion(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createScriptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	script, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		Content       string          `json:"content"`
		ContentFormat string          `json:"contentFormat"`
		SourceType    *string         `json:"sourceType"`
		Metadata      json.RawMessage `json:"metadata"`
		Activate      bool            `json:"activate"`
	}
	if !decode(w, r, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "content is required", nil, false)
		return
	}
	contentFormat := strings.TrimSpace(req.ContentFormat)
	if contentFormat == "" {
		contentFormat = "markdown"
	}
	if !validScriptContentFormat(contentFormat) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "contentFormat is invalid", nil, false)
		return
	}
	metadata, ok := jsonObjectOrDefault(w, r, req.Metadata)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	nextVersion, err := nextScriptVersion(r, tx, script.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, nextVersion, content, contentFormat, req.SourceType, "", "", metadata, principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Activate {
		if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, version, nil)
}

func (s *Server) activateScriptVersion(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	script, err := s.script(r, project.ID, r.PathValue("scriptId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		VersionID string `json:"versionId"`
	}
	if !decode(w, r, &req) {
		return
	}
	versionID := strings.TrimSpace(req.VersionID)
	if versionID == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "versionId is required", nil, false)
		return
	}
	version, err := s.scriptVersion(r, project.ID, script.ID, versionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"scriptId": script.ID, "versionId": version.ID, "active": true}, nil)
}

func (s *Server) createScriptAgentSession(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		Title *string `json:"title"`
	}
	if !decode(w, r, &req) {
		return
	}
	item, err := scanAgentSession(s.db.QueryRow(r.Context(), `
		INSERT INTO agent_sessions(organization_id, project_id, agent_type, title, status, created_by)
		VALUES ($1, $2, 'script_agent', $3, 'active', $4)
		RETURNING id, organization_id, project_id, agent_type, title, status, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, req.Title, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) listScriptAgentSessions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, agent_type, title, status, created_by, created_at, updated_at
		FROM agent_sessions
		WHERE project_id = $1 AND agent_type = 'script_agent'
		ORDER BY created_at DESC
	`, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]AgentSession, 0)
	for rows.Next() {
		item, err := scanAgentSession(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) listScriptAgentMessages(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionId")
	if !s.agentSessionBelongsToProject(r, project.ID, sessionID, "script_agent") {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource was not found", nil, false)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, session_id, role, content, metadata, created_at
		FROM agent_messages
		WHERE project_id = $1 AND session_id = $2
		ORDER BY created_at ASC
	`, project.ID, sessionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]AgentMessage, 0)
	for rows.Next() {
		item, err := scanAgentMessage(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createScriptAgentMessage(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	sessionID := r.PathValue("sessionId")
	if !s.agentSessionBelongsToProject(r, project.ID, sessionID, "script_agent") {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource was not found", nil, false)
		return
	}
	var req struct {
		Role     string          `json:"role"`
		Content  string          `json:"content"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if !decode(w, r, &req) {
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "user"
	}
	content := strings.TrimSpace(req.Content)
	if !validAgentRole(role) || content == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "role or content is invalid", nil, false)
		return
	}
	metadata, ok := jsonObjectOrDefault(w, r, req.Metadata)
	if !ok {
		return
	}
	item, err := scanAgentMessage(s.db.QueryRow(r.Context(), `
		INSERT INTO agent_messages(organization_id, project_id, session_id, role, content, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, organization_id, project_id, session_id, role, content, metadata, created_at
	`, project.OrganizationID, project.ID, sessionID, role, content, metadata))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) generateScriptFromAgent(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		SourceID    string  `json:"sourceId"`
		Instruction string  `json:"instruction"`
		Title       string  `json:"title"`
		SessionID   *string `json:"sessionId"`
	}
	if !decode(w, r, &req) {
		return
	}
	source, err := s.projectSource(r, project.ID, strings.TrimSpace(req.SourceID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = source.Title + " Script"
	}
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPrompt(r, principal, project, req.SessionID, "generate_script", "script_agent_generate", map[string]any{
		"project": projectPromptVariables(project),
		"source": map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"sourceType": source.SourceType,
			"content":    source.Content,
		},
		"input": map[string]any{"instruction": strings.TrimSpace(req.Instruction)},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	script, err := scanScript(tx.QueryRow(r.Context(), scriptInsertSQL(), project.OrganizationID, project.ID, &source.ID, title, "active", principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, 1, content, "markdown", stringPtrFromValue("agent_generated"), rendered.PromptVersionID, rendered.RenderedHash, json.RawMessage(`{}`), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, script.ID, version.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = $4, prompt_hash = $5, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(map[string]any{"scriptId": script.ID, "versionId": version.ID, "content": content}), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"scriptId":   script.ID,
		"versionId":  version.ID,
		"content":    content,
		"agentRunId": runID,
	}, nil)
}

func (s *Server) rewriteScriptFromAgent(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ScriptID    string  `json:"scriptId"`
		VersionID   string  `json:"versionId"`
		Instruction string  `json:"instruction"`
		SessionID   *string `json:"sessionId"`
		Activate    bool    `json:"activate"`
	}
	if !decode(w, r, &req) {
		return
	}
	script, err := s.script(r, project.ID, strings.TrimSpace(req.ScriptID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	versionID := strings.TrimSpace(req.VersionID)
	if versionID == "" && script.CurrentVersionID != nil {
		versionID = *script.CurrentVersionID
	}
	current, err := s.scriptVersion(r, project.ID, script.ID, versionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPrompt(r, principal, project, req.SessionID, "rewrite_script", "script_agent_rewrite", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": current.ID, "content": current.Content},
		"input":   map[string]any{"instruction": strings.TrimSpace(req.Instruction)},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	nextVersion, err := nextScriptVersion(r, tx, script.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	newVersion, err := insertScriptVersionTx(r, tx, project, script.ID, nextVersion, content, current.ContentFormat, stringPtrFromValue("agent_rewrite"), rendered.PromptVersionID, rendered.RenderedHash, json.RawMessage(`{}`), principal.UserID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Activate {
		if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, newVersion.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = $4, prompt_hash = $5, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(map[string]any{"scriptId": script.ID, "versionId": newVersion.ID, "content": content}), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"scriptId":   script.ID,
		"versionId":  newVersion.ID,
		"content":    content,
		"agentRunId": runID,
	}, nil)
}

func (s *Server) runScriptAgentPrompt(r *http.Request, principal auth.Principal, project Project, sessionID *string, taskType, templateKey string, variables map[string]any) (string, string, promptsvc.RenderedPrompt, provider.GatewayTextResponse, error) {
	if sessionID != nil && strings.TrimSpace(*sessionID) != "" && !s.agentSessionBelongsToProject(r, project.ID, strings.TrimSpace(*sessionID), "script_agent") {
		return "", "", promptsvc.RenderedPrompt{}, provider.GatewayTextResponse{}, pgx.ErrNoRows
	}
	resolved, err := promptsvc.NewService(s.db).Resolve(r.Context(), promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return "", "", promptsvc.RenderedPrompt{}, provider.GatewayTextResponse{}, err
	}
	rendered, err := promptsvc.Render(resolved, variables)
	if err != nil {
		return "", "", promptsvc.RenderedPrompt{}, provider.GatewayTextResponse{}, err
	}
	var runID string
	if err := s.db.QueryRow(r.Context(), `
		INSERT INTO agent_runs(
			organization_id, project_id, session_id, agent_type, task_type, status,
			input, prompt_version_id, prompt_hash, created_by, started_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, 'script_agent', $4, 'running', $5, $6, $7, $8, now())
		RETURNING id
	`, project.OrganizationID, project.ID, optionalStringValue(sessionID), taskType, mustMarshal(variables), rendered.PromptVersionID, rendered.RenderedHash, principal.UserID).Scan(&runID); err != nil {
		return "", "", promptsvc.RenderedPrompt{}, provider.GatewayTextResponse{}, err
	}
	gatewayClient := provider.NewGatewayClientFromEnv()
	resp, err := gatewayClient.GenerateText(r.Context(), provider.GatewayTextRequest{
		OrganizationID:    project.OrganizationID,
		ProjectID:         project.ID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustMarshal(map[string]any{
			"prompt": rendered.RenderedText,
		}),
	})
	if err != nil {
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_runs
			SET status = 'failed', error_code = 'PROVIDER_GATEWAY_ERROR', error_message = $2, completed_at = now()
			WHERE id = $1
		`, runID, err.Error())
		return "", runID, rendered, provider.GatewayTextResponse{}, err
	}
	content := strings.TrimSpace(resp.Output.Text)
	if content == "" {
		content = strings.TrimSpace(string(resp.Output.Raw))
	}
	return content, runID, rendered, resp, nil
}

func (s *Server) script(r *http.Request, projectID, scriptID string) (Script, error) {
	return scanScript(s.db.QueryRow(r.Context(), scriptSelectSQL(`
		WHERE s.project_id = $1 AND s.id = $2
	`), projectID, scriptID))
}

func (s *Server) scriptVersion(r *http.Request, projectID, scriptID, versionID string) (ScriptVersion, error) {
	return scanScriptVersion(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, script_id, version, content, content_format,
		       source_type, prompt_version_id, prompt_hash, metadata, created_by, created_at
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2 AND id = $3
	`, projectID, scriptID, versionID))
}

func scriptSelectSQL(where string) string {
	return `
		SELECT s.id, s.organization_id, s.project_id, s.source_id, s.title,
		       COALESCE(s.status, 'draft'), s.current_version_id, s.created_by, s.created_at, s.updated_at
		FROM scripts s
	` + where
}

func scriptInsertSQL() string {
	return `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, organization_id, project_id, source_id, title, status, current_version_id, created_by, created_at, updated_at
	`
}

func insertScriptVersionTx(r *http.Request, tx pgx.Tx, project Project, scriptID string, version int, content, contentFormat string, sourceType *string, promptVersionID, promptHash string, metadata json.RawMessage, createdBy string) (ScriptVersion, error) {
	return scanScriptVersion(tx.QueryRow(r.Context(), `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, source_type, prompt_version_id, prompt_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $4, $5, $6, $7, NULLIF($8, '')::uuid, NULLIF($9, ''), $10, $11)
		RETURNING id, organization_id, project_id, script_id, version, content, content_format,
		          source_type, prompt_version_id, prompt_hash, metadata, created_by, created_at
	`, project.OrganizationID, project.ID, scriptID, version, content, contentFormat, sourceType, promptVersionID, promptHash, metadata, createdBy))
}

func nextScriptVersion(r *http.Request, tx pgx.Tx, scriptID string) (int, error) {
	var next int
	err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(version), 0) + 1 FROM script_versions WHERE script_id = $1`, scriptID).Scan(&next)
	return next, err
}

func scanScript(row rowScan) (Script, error) {
	var item Script
	var sourceID, currentVersionID, createdBy sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&sourceID,
		&item.Title,
		&item.Status,
		&currentVersionID,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.SourceID = stringPtrFromNull(sourceID)
	item.CurrentVersionID = stringPtrFromNull(currentVersionID)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func scanScriptVersion(row rowScan) (ScriptVersion, error) {
	var item ScriptVersion
	var sourceType, promptVersionID, promptHash, createdBy sql.NullString
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.ScriptID,
		&item.Version,
		&item.Content,
		&item.ContentFormat,
		&sourceType,
		&promptVersionID,
		&promptHash,
		&metadata,
		&createdBy,
		&item.CreatedAt,
	)
	item.SourceType = stringPtrFromNull(sourceType)
	item.PromptVersionID = stringPtrFromNull(promptVersionID)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func scanAgentSession(row rowScan) (AgentSession, error) {
	var item AgentSession
	var title, createdBy sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.AgentType,
		&title,
		&item.Status,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.Title = stringPtrFromNull(title)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func scanAgentMessage(row rowScan) (AgentMessage, error) {
	var item AgentMessage
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.SessionID,
		&item.Role,
		&item.Content,
		&metadata,
		&item.CreatedAt,
	)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func (s *Server) agentSessionBelongsToProject(r *http.Request, projectID, sessionID, agentType string) bool {
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM agent_sessions
			WHERE project_id = $1 AND id = $2 AND agent_type = $3
		)
	`, projectID, sessionID, agentType).Scan(&ok)
	return err == nil && ok
}

func validScriptContentFormat(value string) bool {
	return value == "plain_text" || value == "markdown"
}

func validScriptStatus(value string) bool {
	return value == "draft" || value == "active" || value == "archived"
}

func validAgentRole(value string) bool {
	return value == "user" || value == "assistant" || value == "system" || value == "tool"
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func projectPromptVariables(project Project) map[string]any {
	return map[string]any{
		"id":             project.ID,
		"projectType":    stringValue(project.ProjectType),
		"contentType":    stringValue(project.ContentType),
		"aspectRatio":    stringValue(project.AspectRatio),
		"videoRatio":     project.VideoRatio,
		"artStyle":       project.ArtStyle,
		"directorManual": project.DirectorManual,
		"visualManual":   project.VisualManual,
		"imageQuality":   project.ImageQuality,
		"productionMode": project.ProductionMode,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
