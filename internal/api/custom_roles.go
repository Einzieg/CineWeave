package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var customRoleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

type CreateCustomRoleRequest struct {
	RoleKey        string   `json:"roleKey"`
	Name           string   `json:"name"`
	Description    *string  `json:"description"`
	Scope          string   `json:"scope"`
	PermissionKeys []string `json:"permissionKeys"`
}

type UpdateCustomRoleRequest struct {
	Name           *string   `json:"name"`
	Description    *string   `json:"description"`
	Scope          *string   `json:"scope"`
	PermissionKeys *[]string `json:"permissionKeys"`
}

type RoleImpact struct {
	RoleID               string   `json:"roleId"`
	BindingCount         int      `json:"bindingCount"`
	DirectUserCount      int      `json:"directUserCount"`
	TeamCount            int      `json:"teamCount"`
	AffectedUserCount    int      `json:"affectedUserCount"`
	OrganizationBindings int      `json:"organizationBindings"`
	WorkspaceBindings    int      `json:"workspaceBindings"`
	ProjectBindings      int      `json:"projectBindings"`
	PermissionKeys       []string `json:"permissionKeys"`
}

func (s *Server) createCustomRole(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	var req CreateCustomRoleRequest
	if !decode(w, r, &req) {
		return
	}
	roleKey := strings.ToLower(strings.TrimSpace(req.RoleKey))
	name := strings.TrimSpace(req.Name)
	scope := strings.TrimSpace(req.Scope)
	description, err := normalizeRoleDescription(req.Description)
	if err != nil || !customRoleKeyPattern.MatchString(roleKey) || name == "" || len([]rune(name)) > 100 || !validRoleScope(scope) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CUSTOM_ROLE_VALIDATION_FAILED", "custom role request is invalid", nil, false)
		return
	}
	var reserved bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM roles WHERE organization_id IS NULL AND role_key = $1)`, roleKey).Scan(&reserved); err != nil {
		s.writeError(w, r, err)
		return
	}
	if reserved {
		httpx.WriteError(w, r, http.StatusConflict, "ROLE_KEY_EXISTS", "role key is reserved or already exists", nil, false)
		return
	}
	permissionKeys, err := s.validateCustomRolePermissions(r.Context(), principal, organizationID, scope, req.PermissionKeys)
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
	var roleID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO roles(organization_id, role_key, name, description, scope, is_system, managed_by)
		VALUES ($1, $2, $3, $4, $5, false, 'user')
		RETURNING id
	`, organizationID, roleKey, name, description, scope).Scan(&roleID)
	if err != nil {
		if isRoleUniqueViolation(err) {
			httpx.WriteError(w, r, http.StatusConflict, "ROLE_KEY_EXISTS", "role key is reserved or already exists", nil, false)
			return
		}
		s.writeError(w, r, err)
		return
	}
	if err := replaceRolePermissions(r.Context(), tx, roleID, permissionKeys); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := auditlog.Append(r.Context(), tx, organizationID, principal.UserID, auditlog.ActionRoleCreated, "role", roleID, map[string]any{
		"roleKey": roleKey, "scope": scope, "permissionKeys": permissionKeys,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.loadRoleDetail(r.Context(), organizationID, roleID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
}

func (s *Server) updateCustomRole(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	var req UpdateCustomRoleRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == nil && req.Description == nil && req.Scope == nil && req.PermissionKeys == nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CUSTOM_ROLE_VALIDATION_FAILED", "custom role request is invalid", nil, false)
		return
	}
	current, err := s.loadRoleDetail(r.Context(), organizationID, r.PathValue("roleId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if current.IsSystem || current.OrganizationID == nil || *current.OrganizationID != organizationID {
		httpx.WriteError(w, r, http.StatusConflict, "SYSTEM_ROLE_IMMUTABLE", "system roles cannot be modified", nil, false)
		return
	}
	name := current.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	description := current.Description
	if req.Description != nil {
		description, err = normalizeRoleDescription(req.Description)
	}
	scope := current.Scope
	if req.Scope != nil {
		scope = strings.TrimSpace(*req.Scope)
	}
	if err != nil || name == "" || len([]rune(name)) > 100 || !validRoleScope(scope) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CUSTOM_ROLE_VALIDATION_FAILED", "custom role request is invalid", nil, false)
		return
	}
	permissionKeys := permissionKeysFromRole(current)
	if req.PermissionKeys != nil {
		permissionKeys, err = s.validateCustomRolePermissions(r.Context(), principal, organizationID, scope, *req.PermissionKeys)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
	} else if scope != current.Scope {
		permissionKeys, err = s.validateCustomRolePermissions(r.Context(), principal, organizationID, scope, permissionKeys)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
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
	var bindingCount int
	if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM role_bindings WHERE organization_id = $1 AND role_id = $2`, organizationID, current.ID).Scan(&bindingCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	if scope != current.Scope && bindingCount > 0 {
		httpx.WriteError(w, r, http.StatusConflict, "ROLE_SCOPE_IN_USE", "role scope cannot change while bindings exist", map[string]any{"bindingCount": bindingCount}, false)
		return
	}
	command, err := tx.Exec(r.Context(), `
		UPDATE roles
		SET name = $3, description = $4, scope = $5, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND is_system = false
	`, current.ID, organizationID, name, description, scope)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if command.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if req.PermissionKeys != nil || scope != current.Scope {
		if err := replaceRolePermissions(r.Context(), tx, current.ID, permissionKeys); err != nil {
			s.writeError(w, r, err)
			return
		}
	}
	changedFields := make([]string, 0, 3)
	if name != current.Name {
		changedFields = append(changedFields, "name")
	}
	if req.Description != nil {
		changedFields = append(changedFields, "description")
	}
	if scope != current.Scope {
		changedFields = append(changedFields, "scope")
	}
	if req.PermissionKeys != nil {
		changedFields = append(changedFields, "permissions")
	}
	if err := auditlog.Append(r.Context(), tx, organizationID, principal.UserID, auditlog.ActionRoleUpdated, "role", current.ID, map[string]any{
		"changedFields": changedFields, "bindingCount": bindingCount, "permissionKeys": permissionKeys,
	}); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.loadRoleDetail(r.Context(), organizationID, current.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteCustomRole(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleManage, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	current, err := s.loadRoleDetail(r.Context(), organizationID, r.PathValue("roleId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if current.IsSystem || current.OrganizationID == nil || *current.OrganizationID != organizationID {
		httpx.WriteError(w, r, http.StatusConflict, "SYSTEM_ROLE_IMMUTABLE", "system roles cannot be deleted", nil, false)
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
	var bindingCount int
	if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM role_bindings WHERE organization_id = $1 AND role_id = $2`, organizationID, current.ID).Scan(&bindingCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	if bindingCount > 0 {
		httpx.WriteError(w, r, http.StatusConflict, "ROLE_IN_USE", "role cannot be deleted while bindings exist", map[string]any{"bindingCount": bindingCount}, false)
		return
	}
	command, err := tx.Exec(r.Context(), `DELETE FROM roles WHERE id = $1 AND organization_id = $2 AND is_system = false`, current.ID, organizationID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if command.RowsAffected() == 0 {
		s.writeError(w, r, pgx.ErrNoRows)
		return
	}
	if err := auditlog.Append(r.Context(), tx, organizationID, principal.UserID, auditlog.ActionRoleDeleted, "role", current.ID, map[string]any{
		"roleKey": current.RoleKey, "scope": current.Scope, "permissionKeys": permissionKeysFromRole(current),
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

func (s *Server) getRoleImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	organizationID := organizationID(r, principal)
	if !s.authorize(w, r, principal, authz.PermissionRoleRead, authz.Resource{OrganizationID: organizationID}) {
		return
	}
	role, err := s.loadRoleDetail(r.Context(), organizationID, r.PathValue("roleId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	impact := RoleImpact{RoleID: role.ID, PermissionKeys: permissionKeysFromRole(role)}
	err = s.db.QueryRow(r.Context(), `
		SELECT count(*),
		       count(DISTINCT subject_user_id) FILTER (WHERE subject_type = 'user'),
		       count(DISTINCT subject_team_id) FILTER (WHERE subject_type = 'team'),
		       count(*) FILTER (WHERE resource_type = 'organization'),
		       count(*) FILTER (WHERE resource_type = 'workspace'),
		       count(*) FILTER (WHERE resource_type = 'project')
		FROM role_bindings
		WHERE organization_id = $1 AND role_id = $2
	`, organizationID, role.ID).Scan(
		&impact.BindingCount, &impact.DirectUserCount, &impact.TeamCount,
		&impact.OrganizationBindings, &impact.WorkspaceBindings, &impact.ProjectBindings,
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.db.QueryRow(r.Context(), `
		SELECT count(DISTINCT affected.user_id)
		FROM (
			SELECT rb.subject_user_id AS user_id
			FROM role_bindings rb
			JOIN organization_members om ON om.organization_id = rb.organization_id AND om.user_id = rb.subject_user_id AND om.status = 'active'
			WHERE rb.organization_id = $1 AND rb.role_id = $2 AND rb.subject_type = 'user'
			  AND (rb.expires_at IS NULL OR rb.expires_at > now())
			UNION
			SELECT tm.user_id
			FROM role_bindings rb
			JOIN teams t ON t.id = rb.subject_team_id AND t.status = 'active'
			JOIN team_members tm ON tm.team_id = t.id AND tm.status = 'active'
			JOIN organization_members om ON om.organization_id = rb.organization_id AND om.user_id = tm.user_id AND om.status = 'active'
			WHERE rb.organization_id = $1 AND rb.role_id = $2 AND rb.subject_type = 'team'
			  AND (rb.expires_at IS NULL OR rb.expires_at > now())
		) affected
	`, organizationID, role.ID).Scan(&impact.AffectedUserCount); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, impact, nil)
}

func (s *Server) validateCustomRolePermissions(ctx context.Context, principal auth.Principal, organizationID, scope string, values []string) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" || !permissionAllowedForRoleScope(scope, key) {
			return nil, newAPIError(http.StatusUnprocessableEntity, "ROLE_PERMISSION_NOT_ALLOWED", "permission is not assignable to this role scope")
		}
		unique[key] = struct{}{}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return keys, nil
	}
	rows, err := s.db.Query(ctx, `SELECT permission_key FROM permissions WHERE permission_key = ANY($1::text[])`, keys)
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(keys))
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		found[key] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, key := range keys {
		if !found[key] {
			return nil, newAPIError(http.StatusUnprocessableEntity, "ROLE_PERMISSION_NOT_ALLOWED", "permission is not assignable to this role scope")
		}
		if err := s.authorizer.Authorize(ctx, principal, key, authz.Resource{OrganizationID: organizationID}); err != nil {
			return nil, authz.AccessError{Permission: key, Resource: authz.Resource{OrganizationID: organizationID}}
		}
	}
	return keys, nil
}

func permissionAllowedForRoleScope(scope, key string) bool {
	if key == authz.PermissionAdminManage {
		return false
	}
	if scope == "organization" {
		return true
	}
	workspaceAllowed := []string{"workspace.read", "workspace.manage", "project.", "source.", "novel_event.", "adaptation_plan.", "script.", "asset.", "storyboard.", "artifact.", "media.", "workflow."}
	projectAllowed := []string{"project.read", "project.write", "project.update", "project.delete", "project.members.manage", "project.video_production.", "source.", "novel_event.", "adaptation_plan.", "script.", "asset.", "storyboard.", "artifact.", "media.", "workflow."}
	allowed := projectAllowed
	if scope == "workspace" {
		allowed = workspaceAllowed
	} else if scope != "project" {
		return false
	}
	for _, prefix := range allowed {
		if (strings.HasSuffix(prefix, ".") && strings.HasPrefix(key, prefix)) || key == prefix {
			return true
		}
	}
	return false
}

func validRoleScope(value string) bool {
	return value == "organization" || value == "workspace" || value == "project"
}

func normalizeRoleDescription(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if len([]rune(normalized)) > 500 {
		return nil, errors.New("role description is too long")
	}
	if normalized == "" {
		return nil, nil
	}
	return &normalized, nil
}

func replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleID string, keys []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions(role_id, permission_key, managed_by) VALUES ($1, $2, 'user')`, roleID, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) loadRoleDetail(ctx context.Context, organizationID, roleID string) (Role, error) {
	item, err := scanRole(s.db.QueryRow(ctx, `
		SELECT id, organization_id, role_key, name, description, scope, is_system, created_at, updated_at
		FROM roles
		WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2)
	`, roleID, organizationID))
	if err != nil {
		return Role{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.id::text, p.permission_key, p.name, p.description, p.created_at
		FROM role_permissions rp
		JOIN permissions p ON p.permission_key = rp.permission_key
		WHERE rp.role_id = $1
		ORDER BY p.permission_key
	`, item.ID)
	if err != nil {
		return Role{}, err
	}
	defer rows.Close()
	item.Permissions = make([]Permission, 0)
	for rows.Next() {
		var permission Permission
		var id sql.NullString
		if err := rows.Scan(&id, &permission.PermissionKey, &permission.Name, &permission.Description, &permission.CreatedAt); err != nil {
			return Role{}, err
		}
		permission.ID = stringPtrFromNull(id)
		item.Permissions = append(item.Permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return Role{}, err
	}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM role_bindings WHERE organization_id = $1 AND role_id = $2`, organizationID, item.ID).Scan(&item.BindingCount); err != nil {
		return Role{}, err
	}
	return item, nil
}

func permissionKeysFromRole(role Role) []string {
	keys := make([]string, 0, len(role.Permissions))
	for _, permission := range role.Permissions {
		keys = append(keys, permission.PermissionKey)
	}
	sort.Strings(keys)
	return keys
}

func isRoleUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
