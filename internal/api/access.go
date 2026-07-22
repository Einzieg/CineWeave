package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auditlog"
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
	MemberCount    int        `json:"memberCount"`
	BindingCount   int        `json:"bindingCount"`
}

type TeamMember struct {
	TeamID    string            `json:"teamId"`
	UserID    string            `json:"userId"`
	Status    string            `json:"status"`
	CreatedBy *string           `json:"createdBy,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	User      auth.UserResponse `json:"user"`
}

type TeamImpact struct {
	TeamID             string `json:"teamId"`
	ActiveMemberCount  int    `json:"activeMemberCount"`
	ActiveBindingCount int    `json:"activeBindingCount"`
}

type Role struct {
	ID             string       `json:"id"`
	OrganizationID *string      `json:"organizationId,omitempty"`
	RoleKey        string       `json:"roleKey"`
	Name           string       `json:"name"`
	Description    *string      `json:"description,omitempty"`
	Scope          string       `json:"scope"`
	IsSystem       bool         `json:"isSystem"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      *time.Time   `json:"updatedAt,omitempty"`
	Permissions    []Permission `json:"permissions,omitempty"`
	BindingCount   int          `json:"bindingCount,omitempty"`
}

type Permission struct {
	ID            *string   `json:"id,omitempty"`
	PermissionKey string    `json:"permissionKey"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"createdAt"`
}

type RoleBinding struct {
	ID                     string     `json:"id"`
	OrganizationID         string     `json:"organizationId"`
	RoleID                 string     `json:"roleId"`
	RoleKey                string     `json:"roleKey,omitempty"`
	RoleName               string     `json:"roleName,omitempty"`
	SubjectType            string     `json:"subjectType"`
	SubjectUserID          *string    `json:"subjectUserId,omitempty"`
	SubjectTeamID          *string    `json:"subjectTeamId,omitempty"`
	SubjectName            string     `json:"subjectName,omitempty"`
	ResourceType           string     `json:"resourceType"`
	ResourceOrganizationID *string    `json:"resourceOrganizationId,omitempty"`
	ResourceWorkspaceID    *string    `json:"resourceWorkspaceId,omitempty"`
	ResourceProjectID      *string    `json:"resourceProjectId,omitempty"`
	ResourceName           string     `json:"resourceName,omitempty"`
	ExpiresAt              *time.Time `json:"expiresAt,omitempty"`
	CreatedBy              *string    `json:"createdBy,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
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
	rows, err := s.db.Query(r.Context(), teamSelectSQL(`
		WHERE t.organization_id = $1
		ORDER BY t.created_at DESC
	`), orgID)
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var teamID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO teams(organization_id, name, slug, description, status, created_by)
		VALUES ($1, $2, $3, $4, 'active', $5)
		RETURNING id
	`, orgID, name, accessSlug(name), req.Description, principal.UserID).Scan(&teamID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, orgID, principal.UserID, auditlog.ActionTeamCreated, "team", teamID, map[string]any{
		"name": name,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.team(r, teamID)
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

func (s *Server) getTeamImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	team, err := s.team(r, r.PathValue("teamId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorize(w, r, principal, authz.PermissionTeamManage, authz.Resource{OrganizationID: team.OrganizationID}) {
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, TeamImpact{
		TeamID:             team.ID,
		ActiveMemberCount:  team.MemberCount,
		ActiveBindingCount: team.BindingCount,
	}, nil)
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `
		UPDATE teams
		SET name = $2, description = COALESCE($3, description), status = $4
		WHERE id = $1
	`, current.ID, name, req.Description, status)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	changedFields := make([]string, 0, 3)
	if req.Name != nil && name != current.Name {
		changedFields = append(changedFields, "name")
	}
	if req.Description != nil {
		changedFields = append(changedFields, "description")
	}
	if req.Status != nil && status != current.Status {
		changedFields = append(changedFields, "status")
	}
	if err := auditlog.Append(r.Context(), tx, current.OrganizationID, principal.UserID, auditlog.ActionTeamUpdated, "team", current.ID, map[string]any{
		"changedFields":  changedFields,
		"previousStatus": current.Status,
		"status":         status,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.team(r, current.ID)
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE teams SET status = 'disabled' WHERE id = $1`, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, item.OrganizationID, principal.UserID, auditlog.ActionTeamDisabled, "team", item.ID, map[string]any{
		"activeMemberCount":  item.MemberCount,
		"activeBindingCount": item.BindingCount,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
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
		SELECT tm.team_id, tm.user_id, tm.status, tm.created_by, tm.created_at,
		       u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, '')
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = $1
		ORDER BY tm.created_at DESC
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanTeamMember(tx.QueryRow(r.Context(), `
		WITH changed AS (
			INSERT INTO team_members(team_id, user_id, status, created_by)
			VALUES ($1, $2, 'active', $3)
			ON CONFLICT (team_id, user_id) DO UPDATE SET status = 'active'
			RETURNING team_id, user_id, status, created_by, created_at
		)
		SELECT changed.team_id, changed.user_id, changed.status, changed.created_by, changed.created_at,
		       u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, '')
		FROM changed
		JOIN users u ON u.id = changed.user_id
	`, team.ID, strings.TrimSpace(req.UserID), principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, team.OrganizationID, principal.UserID, auditlog.ActionTeamMemberAdded, "team", team.ID, map[string]any{
		"userId": item.UserID,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	command, err := tx.Exec(r.Context(), `
		UPDATE team_members
		SET status = 'disabled'
		WHERE team_id = $1 AND user_id = $2 AND status = 'active'
	`, team.ID, r.PathValue("userId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if command.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if err := auditlog.Append(r.Context(), tx, team.OrganizationID, principal.UserID, auditlog.ActionTeamMemberRemoved, "team", team.ID, map[string]any{
		"userId": r.PathValue("userId"),
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
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
		WHERE organization_id = $1
		   OR (
			organization_id IS NULL
			AND role_key NOT IN ('organization_owner', 'organization_admin', 'organization_member')
		   )
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

func (s *Server) getRole(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	orgID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleRead, authz.Resource{OrganizationID: orgID}) {
		return
	}
	item, err := s.roleForBinding(r, orgID, r.PathValue("roleId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT p.id::text, p.permission_key, p.name, p.description, p.created_at
		FROM role_permissions rp
		JOIN permissions p ON p.permission_key = rp.permission_key
		WHERE rp.role_id = $1
		ORDER BY p.permission_key
	`, item.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	item.Permissions = make([]Permission, 0)
	for rows.Next() {
		var permission Permission
		var id sql.NullString
		if err := rows.Scan(&id, &permission.PermissionKey, &permission.Name, &permission.Description, &permission.CreatedAt); err != nil {
			s.writeError(w, r, err)
			return
		}
		permission.ID = stringPtrFromNull(id)
		item.Permissions = append(item.Permissions, permission)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.db.QueryRow(r.Context(), `SELECT count(*) FROM role_bindings WHERE organization_id = $1 AND role_id = $2`, orgID, item.ID).Scan(&item.BindingCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
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
	subjectType := strings.TrimSpace(r.URL.Query().Get("subjectType"))
	subjectID := strings.TrimSpace(r.URL.Query().Get("subjectId"))
	resourceType := strings.TrimSpace(r.URL.Query().Get("resourceType"))
	resourceID := strings.TrimSpace(r.URL.Query().Get("resourceId"))
	roleID := strings.TrimSpace(r.URL.Query().Get("roleId"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `
		SELECT count(*)
		FROM role_bindings rb
		WHERE rb.organization_id = $1
		  AND ($2 = '' OR rb.subject_type = $2)
		  AND ($3 = '' OR COALESCE(rb.subject_user_id::text, rb.subject_team_id::text, '') = $3)
		  AND ($4 = '' OR rb.resource_type = $4)
		  AND ($5 = '' OR COALESCE(rb.resource_organization_id::text, rb.resource_workspace_id::text, rb.resource_project_id::text, '') = $5)
		  AND ($6 = '' OR rb.role_id::text = $6)
	`, orgID, subjectType, subjectID, resourceType, resourceID, roleID).Scan(&total); err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), roleBindingSelect(`
		WHERE rb.organization_id = $1
		  AND ($2 = '' OR rb.subject_type = $2)
		  AND ($3 = '' OR COALESCE(rb.subject_user_id::text, rb.subject_team_id::text, '') = $3)
		  AND ($4 = '' OR rb.resource_type = $4)
		  AND ($5 = '' OR COALESCE(rb.resource_organization_id::text, rb.resource_workspace_id::text, rb.resource_project_id::text, '') = $5)
		  AND ($6 = '' OR rb.role_id::text = $6)
		ORDER BY rb.created_at DESC
		LIMIT $7 OFFSET $8
	`), orgID, subjectType, subjectID, resourceType, resourceID, roleID, pageSize, (page-1)*pageSize)
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
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "page": page, "pageSize": pageSize, "total": total,
	}, nil)
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
	if err := s.validateRoleDelegation(r, principal, orgID, role, roleBindingResource(orgID, req)); err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var roleBindingID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id, subject_team_id,
			resource_type, resource_organization_id, resource_workspace_id, resource_project_id, expires_at, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT DO NOTHING
		RETURNING id
	`, orgID, req.RoleID, req.SubjectType, req.SubjectUserID, req.SubjectTeamID, req.ResourceType, req.ResourceOrganizationID, req.ResourceWorkspaceID, req.ResourceProjectID, req.ExpiresAt, principal.UserID).Scan(&roleBindingID)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteError(w, r, http.StatusConflict, "CONFLICT", "role binding already exists or could not be created", nil, false)
			return
		}
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, orgID, principal.UserID, auditlog.ActionRoleBindingCreated, "role_binding", roleBindingID, roleBindingAuditMetadata(req)); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
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
	role, err := s.roleForBinding(r, item.OrganizationID, item.RoleID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.validateRoleDelegation(r, principal, item.OrganizationID, role, roleBindingResource(item.OrganizationID, item)); err != nil {
		s.writeError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `LOCK TABLE role_bindings IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		s.writeError(w, r, err)
		return
	}
	if item.SubjectUserID != nil && (item.RoleKey == "org_owner" || item.RoleKey == "organization_owner") &&
		item.ResourceType == "organization" && item.ResourceOrganizationID != nil {
		var otherOwners int
		if err := tx.QueryRow(r.Context(), `
			SELECT count(DISTINCT rb.subject_user_id)
			FROM role_bindings rb
			JOIN roles role ON role.id = rb.role_id
			JOIN organization_members member
			  ON member.organization_id = rb.organization_id
			 AND member.user_id = rb.subject_user_id
			 AND member.status = 'active'
			WHERE rb.organization_id = $1 AND rb.id <> $2
			  AND rb.subject_type = 'user'
			  AND role.role_key IN ('org_owner', 'organization_owner')
			  AND rb.resource_type = 'organization' AND rb.resource_organization_id = $1
			  AND (rb.expires_at IS NULL OR rb.expires_at > now())
		`, item.OrganizationID, item.ID).Scan(&otherOwners); err != nil {
			s.writeError(w, r, err)
			return
		}
		if otherOwners == 0 {
			s.writeError(w, r, auth.ErrLastOwner)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM role_bindings WHERE id = $1`, item.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, item.OrganizationID, principal.UserID, auditlog.ActionRoleBindingRevoked, "role_binding", item.ID, roleBindingAuditMetadata(item)); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
}

func (s *Server) team(r *http.Request, teamID string) (Team, error) {
	return scanTeam(s.db.QueryRow(r.Context(), teamSelectSQL(`WHERE t.id = $1`), teamID))
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

func (s *Server) validateRoleDelegation(r *http.Request, principal auth.Principal, orgID string, role Role, resource authz.Resource) error {
	rows, err := s.db.Query(r.Context(), `
		SELECT permission_key
		FROM role_permissions
		WHERE role_id = $1
		ORDER BY permission_key
	`, role.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	permissions := make([]string, 0)
	sensitive := role.RoleKey == "org_owner" || role.RoleKey == "organization_owner" ||
		role.RoleKey == "org_admin" || role.RoleKey == "organization_admin"
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return err
		}
		permissions = append(permissions, permission)
		if permission == authz.PermissionAdminManage {
			sensitive = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if sensitive {
		directOwner, err := s.isDirectOrganizationOwner(r, orgID, principal.UserID)
		if err != nil {
			return err
		}
		if !directOwner {
			return authz.AccessError{Permission: authz.PermissionAdminManage, Resource: resource}
		}
	}
	for _, permission := range permissions {
		if err := s.authorizer.Authorize(r.Context(), principal, permission, resource); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) isDirectOrganizationOwner(r *http.Request, orgID, userID string) (bool, error) {
	var directOwner bool
	err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1
			FROM role_bindings binding
			JOIN roles role ON role.id = binding.role_id
			JOIN organization_members member
			  ON member.organization_id = binding.organization_id
			 AND member.user_id = binding.subject_user_id
			 AND member.status = 'active'
			WHERE binding.organization_id = $1
			  AND binding.subject_type = 'user'
			  AND binding.subject_user_id = $2
			  AND binding.resource_type = 'organization'
			  AND binding.resource_organization_id = $1
			  AND role.role_key IN ('org_owner', 'organization_owner')
			  AND (binding.expires_at IS NULL OR binding.expires_at > now())
		)
	`, orgID, userID).Scan(&directOwner)
	return directOwner, err
}

func roleBindingResource(orgID string, binding RoleBinding) authz.Resource {
	resource := authz.Resource{OrganizationID: orgID}
	if binding.ResourceWorkspaceID != nil {
		resource.WorkspaceID = strings.TrimSpace(*binding.ResourceWorkspaceID)
	}
	if binding.ResourceProjectID != nil {
		resource.ProjectID = strings.TrimSpace(*binding.ResourceProjectID)
	}
	return resource
}

func (s *Server) validateRoleBindingRequest(r *http.Request, orgID string, role Role, req RoleBinding) error {
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "expiresAt must be in the future")
	}
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
		if team.OrganizationID != orgID || team.Status != "active" {
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

func scanTeam(row pgx.Row) (Team, error) {
	var item Team
	var description, createdBy sql.NullString
	var updatedAt sql.NullTime
	err := row.Scan(&item.ID, &item.OrganizationID, &item.Name, &description, &item.Status, &createdBy, &item.CreatedAt, &updatedAt, &item.MemberCount, &item.BindingCount)
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
	err := row.Scan(
		&item.TeamID, &item.UserID, &item.Status, &createdBy, &item.CreatedAt,
		&item.User.ID, &item.User.Email, &item.User.Username, &item.User.DisplayName, &item.User.AvatarURL,
	)
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
	var expiresAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.RoleID,
		&item.RoleKey,
		&item.RoleName,
		&item.SubjectType,
		&subjectUserID,
		&subjectTeamID,
		&item.SubjectName,
		&item.ResourceType,
		&resourceOrganizationID,
		&resourceWorkspaceID,
		&resourceProjectID,
		&item.ResourceName,
		&expiresAt,
		&createdBy,
		&item.CreatedAt,
	)
	item.SubjectUserID = stringPtrFromNull(subjectUserID)
	item.SubjectTeamID = stringPtrFromNull(subjectTeamID)
	item.ResourceOrganizationID = stringPtrFromNull(resourceOrganizationID)
	item.ResourceWorkspaceID = stringPtrFromNull(resourceWorkspaceID)
	item.ResourceProjectID = stringPtrFromNull(resourceProjectID)
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	item.CreatedBy = stringPtrFromNull(createdBy)
	return item, err
}

func roleBindingSelect(where string) string {
	return `
		SELECT rb.id, rb.organization_id, rb.role_id, r.role_key, r.name, rb.subject_type,
		       rb.subject_user_id, rb.subject_team_id,
		       COALESCE(NULLIF(COALESCE(u.display_name, u.username, u.email), ''), subject_team.name, ''),
		       rb.resource_type,
		       rb.resource_organization_id, rb.resource_workspace_id, rb.resource_project_id,
		       COALESCE(resource_org.name, resource_workspace.name, resource_project.name, ''),
		       rb.expires_at,
		       rb.created_by, rb.created_at
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		LEFT JOIN users u ON u.id = rb.subject_user_id
		LEFT JOIN teams subject_team ON subject_team.id = rb.subject_team_id
		LEFT JOIN organizations resource_org ON resource_org.id = rb.resource_organization_id
		LEFT JOIN workspaces resource_workspace ON resource_workspace.id = rb.resource_workspace_id
		LEFT JOIN projects resource_project ON resource_project.id = rb.resource_project_id
	` + where
}

func roleBindingAuditMetadata(item RoleBinding) map[string]any {
	subjectID := ""
	if item.SubjectUserID != nil {
		subjectID = *item.SubjectUserID
	} else if item.SubjectTeamID != nil {
		subjectID = *item.SubjectTeamID
	}
	resourceID := ""
	if item.ResourceOrganizationID != nil {
		resourceID = *item.ResourceOrganizationID
	} else if item.ResourceWorkspaceID != nil {
		resourceID = *item.ResourceWorkspaceID
	} else if item.ResourceProjectID != nil {
		resourceID = *item.ResourceProjectID
	}
	return map[string]any{
		"roleId":       item.RoleID,
		"subjectType":  item.SubjectType,
		"subjectId":    subjectID,
		"resourceType": item.ResourceType,
		"resourceId":   resourceID,
		"expiresAt":    item.ExpiresAt,
	}
}

func teamSelectSQL(where string) string {
	return `
		SELECT t.id, t.organization_id, t.name, t.description, t.status, t.created_by, t.created_at, t.updated_at,
		       (SELECT count(*) FROM team_members tm WHERE tm.team_id = t.id AND tm.status = 'active'),
		       (SELECT count(*) FROM role_bindings rb WHERE rb.subject_type = 'team' AND rb.subject_team_id = t.id AND (rb.expires_at IS NULL OR rb.expires_at > now()))
		FROM teams t
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
