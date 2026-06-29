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
	"github.com/jackc/pgx/v5"
)

type Team struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	Status         string     `json:"status"`
	CreatedBy      *string    `json:"createdBy,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      *time.Time `json:"updatedAt,omitempty"`
}

type TeamMember struct {
	TeamID    string    `json:"teamId"`
	UserID    string    `json:"userId"`
	Status    string    `json:"status"`
	CreatedBy *string   `json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Role struct {
	ID             string     `json:"id"`
	OrganizationID *string    `json:"organizationId,omitempty"`
	RoleKey        string     `json:"roleKey"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	Scope          string     `json:"scope"`
	IsSystem       bool       `json:"isSystem"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      *time.Time `json:"updatedAt,omitempty"`
}

type Permission struct {
	ID            *string   `json:"id,omitempty"`
	PermissionKey string    `json:"permissionKey"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"createdAt"`
}

type RoleBinding struct {
	ID                     string    `json:"id"`
	OrganizationID         string    `json:"organizationId"`
	RoleID                 string    `json:"roleId"`
	RoleKey                string    `json:"roleKey,omitempty"`
	SubjectType            string    `json:"subjectType"`
	SubjectUserID          *string   `json:"subjectUserId,omitempty"`
	SubjectTeamID          *string   `json:"subjectTeamId,omitempty"`
	ResourceType           string    `json:"resourceType"`
	ResourceOrganizationID *string   `json:"resourceOrganizationId,omitempty"`
	ResourceWorkspaceID    *string   `json:"resourceWorkspaceId,omitempty"`
	ResourceProjectID      *string   `json:"resourceProjectId,omitempty"`
	CreatedBy              *string   `json:"createdBy,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, principal auth.Principal, permission string, resource authz.Resource) bool {
	if err := s.authorizer.Authorize(r.Context(), principal, permission, resource); err != nil {
		s.writeError(w, r, err)
		return false
	}
	return true
}

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionTeamRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, name, description, status, created_by, created_at, updated_at
		FROM teams
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]Team, 0)
	for rows.Next() {
		item, err := scanTeam(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if !decode(w, r, &req) {
		return
	}
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionTeamManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "name is required", nil, false)
		return
	}
	item, err := scanTeam(s.db.QueryRow(r.Context(), `
		INSERT INTO teams(organization_id, name, slug, description, status, created_by)
		VALUES ($1, $2, $3, $4, 'active', $5)
		RETURNING id, organization_id, name, description, status, created_by, created_at, updated_at
	`, orgID, name, accessSlug(name), req.Description, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) getTeam(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamRead, authz.Resource{OrganizationID: item.OrganizationID}) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	current, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamManage, authz.Resource{OrganizationID: current.OrganizationID}) {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	status := current.Status
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	if name == "" || (status != "active" && status != "disabled") {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "name and valid status are required", nil, false)
		return
	}
	item, err := scanTeam(s.db.QueryRow(r.Context(), `
		UPDATE teams
		SET name = $2, description = COALESCE($3, description), status = $4
		WHERE id = $1
		RETURNING id, organization_id, name, description, status, created_by, created_at, updated_at
	`, current.ID, name, req.Description, status))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamManage, authz.Resource{OrganizationID: item.OrganizationID}) {
		return
	}
	if _, err := s.db.Exec(r.Context(), `UPDATE teams SET status = 'disabled' WHERE id = $1`, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) listTeamMembers(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	team, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamRead, authz.Resource{OrganizationID: team.OrganizationID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT team_id, user_id, status, created_by, created_at
		FROM team_members
		WHERE team_id = $1
		ORDER BY created_at DESC
	`, team.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]TeamMember, 0)
	for rows.Next() {
		item, err := scanTeamMember(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) addTeamMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	team, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamManage, authz.Resource{OrganizationID: team.OrganizationID}) {
		return
	}
	var req struct {
		UserID string `json:"userId"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !s.userInOrganization(r, team.OrganizationID, req.UserID) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "user is not an active organization member", nil, false)
		return
	}
	item, err := scanTeamMember(s.db.QueryRow(r.Context(), `
		INSERT INTO team_members(team_id, user_id, status, created_by)
		VALUES ($1, $2, 'active', $3)
		ON CONFLICT (team_id, user_id) DO UPDATE SET status = 'active'
		RETURNING team_id, user_id, status, created_by, created_at
	`, team.ID, strings.TrimSpace(req.UserID), principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) removeTeamMember(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	team, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamManage, authz.Resource{OrganizationID: team.OrganizationID}) {
		return
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE team_members
		SET status = 'disabled'
		WHERE team_id = $1 AND user_id = $2
	`, team.ID, r.PathValue("userId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, role_key, name, description, scope, is_system, created_at, updated_at
		FROM roles
		WHERE organization_id IS NULL OR organization_id = $1
		ORDER BY organization_id NULLS FIRST, scope, role_key
	`, orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]Role, 0)
	for rows.Next() {
		item, err := scanRole(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, permission_key, name, description, created_at
		FROM permissions
		ORDER BY permission_key
	`)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]Permission, 0)
	for rows.Next() {
		var item Permission
		var id sql.NullString
		if err := rows.Scan(&id, &item.PermissionKey, &item.Name, &item.Description, &item.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		item.ID = stringPtrFromNull(id)
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) listRoleBindings(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	rows, err := s.db.Query(r.Context(), roleBindingSelect(`
		WHERE rb.organization_id = $1
		ORDER BY rb.created_at DESC
	`), orgID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]RoleBinding, 0)
	for rows.Next() {
		item, err := scanRoleBinding(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createRoleBinding(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	var req RoleBinding
	if !decode(w, r, &req) {
		return
	}
	orgID := req.OrganizationID
	if strings.TrimSpace(orgID) == "" {
		orgID = organizationID(r, principal)
	}
	if !s.authorize(w, r, principal, authz.PermissionRoleManage, authz.Resource{OrganizationID: orgID}) {
		return
	}
	role, err := s.roleForBinding(r, orgID, req.RoleID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.validateRoleBindingRequest(r, orgID, role, req); err != nil {
		s.writeError(w, r, err)
		return
	}
	var roleBindingID string
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id, subject_team_id,
			resource_type, resource_organization_id, resource_workspace_id, resource_project_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
		RETURNING id
	`, orgID, req.RoleID, req.SubjectType, req.SubjectUserID, req.SubjectTeamID, req.ResourceType, req.ResourceOrganizationID, req.ResourceWorkspaceID, req.ResourceProjectID, principal.UserID).Scan(&roleBindingID)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteError(w, r, http.StatusConflict, "CONFLICT", "role binding already exists or could not be created", nil, false)
			return
		}
		s.writeError(w, r, err)
		return
	}
	item, err := scanRoleBinding(s.db.QueryRow(r.Context(), roleBindingSelect(`WHERE rb.id = $1`), roleBindingID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) deleteRoleBinding(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	item, err := scanRoleBinding(s.db.QueryRow(r.Context(), roleBindingSelect(`WHERE rb.id = $1`), r.PathValue("roleBindingId")))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionRoleManage, authz.Resource{OrganizationID: item.OrganizationID}) {
		return
	}
	if item.SubjectUserID != nil && *item.SubjectUserID == principal.UserID && item.RoleKey == "org_owner" && s.lastOrgOwnerBinding(r, item.OrganizationID, item.ID) {
		httpx.WriteError(w, r, http.StatusConflict, "LAST_OWNER_BINDING", "cannot delete your last org_owner binding", nil, false)
		return
	}
	if _, err := s.db.Exec(r.Context(), `DELETE FROM role_bindings WHERE id = $1`, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) team(r *http.Request, teamID string) (Team, error) {
	return scanTeam(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, name, description, status, created_by, created_at, updated_at
		FROM teams
		WHERE id = $1
	`, teamID))
}

func (s *Server) userInOrganization(r *http.Request, orgID, userID string) bool {
	var ok bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND status = 'active'
		)
	`, orgID, strings.TrimSpace(userID)).Scan(&ok)
	return err == nil && ok
}

func (s *Server) roleForBinding(r *http.Request, orgID, roleID string) (Role, error) {
	return scanRole(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, role_key, name, description, scope, is_system, created_at, updated_at
		FROM roles
		WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2)
	`, roleID, orgID))
}

func (s *Server) validateRoleBindingRequest(r *http.Request, orgID string, role Role, req RoleBinding) error {
	if req.SubjectType != "user" && req.SubjectType != "team" {
		return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
	}
	if req.SubjectType == "user" {
		if req.SubjectUserID == nil || strings.TrimSpace(*req.SubjectUserID) == "" || req.SubjectTeamID != nil {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
		if !s.userInOrganization(r, orgID, *req.SubjectUserID) {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
	}
	if req.SubjectType == "team" {
		if req.SubjectTeamID == nil || strings.TrimSpace(*req.SubjectTeamID) == "" || req.SubjectUserID != nil {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
		team, err := s.team(r, *req.SubjectTeamID)
		if err != nil {
			return err
		}
		if team.OrganizationID != orgID {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
	}
	if role.Scope != req.ResourceType {
		return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
	}
	switch req.ResourceType {
	case "organization":
		if req.ResourceOrganizationID == nil || *req.ResourceOrganizationID != orgID || req.ResourceWorkspaceID != nil || req.ResourceProjectID != nil {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
	case "workspace":
		if req.ResourceWorkspaceID == nil || req.ResourceOrganizationID != nil || req.ResourceProjectID != nil {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
		var resourceOrgID string
		if err := s.db.QueryRow(r.Context(), `SELECT organization_id FROM workspaces WHERE id = $1`, *req.ResourceWorkspaceID).Scan(&resourceOrgID); err != nil {
			return err
		}
		if resourceOrgID != orgID {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
	case "project":
		if req.ResourceProjectID == nil || req.ResourceOrganizationID != nil || req.ResourceWorkspaceID != nil {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
		var resourceOrgID string
		if err := s.db.QueryRow(r.Context(), `SELECT organization_id FROM projects WHERE id = $1`, *req.ResourceProjectID).Scan(&resourceOrgID); err != nil {
			return err
		}
		if resourceOrgID != orgID {
			return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
		}
	default:
		return authz.AccessError{Permission: authz.PermissionRoleManage, Resource: authz.Resource{OrganizationID: orgID}}
	}
	return nil
}

func (s *Server) lastOrgOwnerBinding(r *http.Request, orgID, excludingBindingID string) bool {
	var count int
	err := s.db.QueryRow(r.Context(), `
		SELECT count(*)
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		WHERE rb.organization_id = $1
		  AND rb.id <> $2
		  AND rb.subject_type = 'user'
		  AND r.role_key IN ('org_owner', 'organization_owner')
		  AND rb.resource_type = 'organization'
		  AND rb.resource_organization_id = $1
	`, orgID, excludingBindingID).Scan(&count)
	return err == nil && count == 0
}

func scanTeam(row pgx.Row) (Team, error) {
	var item Team
	var description, createdBy sql.NullString
	var updatedAt sql.NullTime
	err := row.Scan(&item.ID, &item.OrganizationID, &item.Name, &description, &item.Status, &createdBy, &item.CreatedAt, &updatedAt)
	item.Description = stringPtrFromNull(description)
	item.CreatedBy = stringPtrFromNull(createdBy)
	if updatedAt.Valid {
		item.UpdatedAt = &updatedAt.Time
	}
	return item, err
}

func scanTeamMember(row pgx.Row) (TeamMember, error) {
	var item TeamMember
	var createdBy sql.NullString
	err := row.Scan(&item.TeamID, &item.UserID, &item.Status, &createdBy, &item.CreatedAt)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func scanRole(row pgx.Row) (Role, error) {
	var item Role
	var organizationID, description sql.NullString
	var updatedAt sql.NullTime
	err := row.Scan(&item.ID, &organizationID, &item.RoleKey, &item.Name, &description, &item.Scope, &item.IsSystem, &item.CreatedAt, &updatedAt)
	item.OrganizationID = stringPtrFromNull(organizationID)
	item.Description = stringPtrFromNull(description)
	if updatedAt.Valid {
		item.UpdatedAt = &updatedAt.Time
	}
	return item, err
}

func scanRoleBinding(row pgx.Row) (RoleBinding, error) {
	var item RoleBinding
	var subjectUserID, subjectTeamID, resourceOrganizationID, resourceWorkspaceID, resourceProjectID, createdBy sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.RoleID,
		&item.RoleKey,
		&item.SubjectType,
		&subjectUserID,
		&subjectTeamID,
		&item.ResourceType,
		&resourceOrganizationID,
		&resourceWorkspaceID,
		&resourceProjectID,
		&createdBy,
		&item.CreatedAt,
	)
	item.SubjectUserID = stringPtrFromNull(subjectUserID)
	item.SubjectTeamID = stringPtrFromNull(subjectTeamID)
	item.ResourceOrganizationID = stringPtrFromNull(resourceOrganizationID)
	item.ResourceWorkspaceID = stringPtrFromNull(resourceWorkspaceID)
	item.ResourceProjectID = stringPtrFromNull(resourceProjectID)
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func roleBindingSelect(where string) string {
	return `
		SELECT rb.id, rb.organization_id, rb.role_id, r.role_key, rb.subject_type,
		       rb.subject_user_id, rb.subject_team_id, rb.resource_type,
		       rb.resource_organization_id, rb.resource_workspace_id, rb.resource_project_id,
		       rb.created_by, rb.created_at
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
	` + where
}

func accessSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	builder := strings.Builder{}
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "team"
	}
	return slug + "-" + randomStorageSegment()
}

func mustRawJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
