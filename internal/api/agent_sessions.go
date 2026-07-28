package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
)

const projectAgentSessionType = "project_agent"

func (s *Server) createProjectAgentSession(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead,
	)
	if !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > 120 {
		httpx.WriteError(
			w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED",
			"助手会话标题不能超过 120 个字符", nil, false,
		)
		return
	}
	var titleValue any
	if title != "" {
		titleValue = title
	}
	item, err := scanAgentSession(s.db.QueryRow(r.Context(), `
		INSERT INTO agent_sessions(
			organization_id, project_id, agent_type, title, status, created_by
		)
		VALUES ($1, $2, $3, $4, 'active', $5)
		RETURNING id, organization_id, project_id, agent_type, title, status,
		          created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, projectAgentSessionType, titleValue, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) listProjectAgentSessions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead,
	)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, agent_type, title, status,
		       created_by, created_at, updated_at
		FROM agent_sessions
		WHERE organization_id = $1
		  AND project_id = $2
		  AND agent_type = $3
		  AND status = 'active'
		ORDER BY created_at DESC
	`, project.OrganizationID, project.ID, projectAgentSessionType)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]AgentSession, 0)
	for rows.Next() {
		item, scanErr := scanAgentSession(rows)
		if scanErr != nil {
			s.writeError(w, r, scanErr)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) listProjectAgentMessages(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(
		w, r, principal, r.PathValue("projectId"), authz.PermissionProjectRead,
	)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(r.PathValue("sessionId"))
	if !s.agentSessionBelongsToProject(r, project.ID, sessionID, projectAgentSessionType) {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource was not found", nil, false)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, session_id, role, content, metadata, created_at
		FROM agent_messages
		WHERE organization_id = $1 AND project_id = $2 AND session_id = $3
		ORDER BY created_at ASC
	`, project.OrganizationID, project.ID, sessionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]AgentMessage, 0)
	for rows.Next() {
		item, scanErr := scanAgentMessage(rows)
		if scanErr != nil {
			s.writeError(w, r, scanErr)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}
