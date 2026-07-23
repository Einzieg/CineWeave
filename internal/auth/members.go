package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/jackc/pgx/v5"
)

var (
	ErrMemberLifecycle = errors.New("member lifecycle transition is invalid")
	ErrLastOwner       = errors.New("organization must keep an active direct owner")
)

type MemberTeamSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MemberRoleSummary struct {
	BindingID    string     `json:"bindingId"`
	RoleID       string     `json:"roleId"`
	RoleKey      string     `json:"roleKey"`
	RoleName     string     `json:"roleName"`
	ResourceType string     `json:"resourceType"`
	ResourceID   string     `json:"resourceId"`
	ViaTeam      bool       `json:"viaTeam"`
	TeamID       *string    `json:"teamId,omitempty"`
	TeamName     *string    `json:"teamName,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}

type OrganizationMember struct {
	OrganizationID           string              `json:"organizationId"`
	User                     UserResponse        `json:"user"`
	Status                   string              `json:"status"`
	AccountManagementAllowed bool                `json:"accountManagementAllowed"`
	CreatedAt                time.Time           `json:"createdAt"`
	UpdatedAt                time.Time           `json:"updatedAt"`
	DisabledAt               *time.Time          `json:"disabledAt,omitempty"`
	RemovedAt                *time.Time          `json:"removedAt,omitempty"`
	Teams                    []MemberTeamSummary `json:"teams"`
	Roles                    []MemberRoleSummary `json:"roles"`
}

type MemberList struct {
	Items    []OrganizationMember `json:"items"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
	Total    int                  `json:"total"`
}

func (s *Service) ListOrganizationMembers(ctx context.Context, organizationID, search, status string, page, pageSize int) (MemberList, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	status = strings.TrimSpace(status)
	if status != "" && status != "active" && status != "disabled" && status != "removed" {
		return MemberList{}, ErrMemberLifecycle
	}
	search = strings.TrimSpace(search)
	filter := `om.organization_id = $1
		AND ($2 = '' OR om.status = $2)
		AND ($3 = '' OR u.email ILIKE '%' || $3 || '%' OR COALESCE(u.username, '') ILIKE '%' || $3 || '%' OR COALESCE(u.display_name, '') ILIKE '%' || $3 || '%')`
	var total int
	if err := s.db.QueryRow(ctx, `
		SELECT count(*)
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE `+filter, organizationID, status, search).Scan(&total); err != nil {
		return MemberList{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT om.organization_id, u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''),
		       u.is_system_admin, om.status,
		       om.status <> 'removed' AND NOT u.is_system_admin AND (
		           SELECT count(*) FROM organization_members memberships
		           WHERE memberships.user_id = u.id AND memberships.status <> 'removed'
		       ) = 1,
		       om.created_at, om.updated_at, om.disabled_at, om.removed_at
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE `+filter+`
		ORDER BY CASE om.status WHEN 'active' THEN 0 WHEN 'disabled' THEN 1 ELSE 2 END,
		         COALESCE(u.display_name, u.username, u.email), u.id
		LIMIT $4 OFFSET $5
	`, organizationID, status, search, pageSize, (page-1)*pageSize)
	if err != nil {
		return MemberList{}, err
	}
	defer rows.Close()
	items := make([]OrganizationMember, 0)
	for rows.Next() {
		var item OrganizationMember
		if err := rows.Scan(
			&item.OrganizationID, &item.User.ID, &item.User.Email, &item.User.Username, &item.User.DisplayName, &item.User.AvatarURL,
			&item.User.SystemAdministrator, &item.Status, &item.AccountManagementAllowed, &item.CreatedAt, &item.UpdatedAt, &item.DisabledAt, &item.RemovedAt,
		); err != nil {
			return MemberList{}, err
		}
		if err := s.loadMemberAccessSummaries(ctx, &item); err != nil {
			return MemberList{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MemberList{}, err
	}
	return MemberList{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) GetOrganizationMember(ctx context.Context, organizationID, userID string) (OrganizationMember, error) {
	var item OrganizationMember
	err := s.db.QueryRow(ctx, `
		SELECT om.organization_id, u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''),
		       u.is_system_admin, om.status,
		       om.status <> 'removed' AND NOT u.is_system_admin AND (
		           SELECT count(*) FROM organization_members memberships
		           WHERE memberships.user_id = u.id AND memberships.status <> 'removed'
		       ) = 1,
		       om.created_at, om.updated_at, om.disabled_at, om.removed_at
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
	`, organizationID, userID).Scan(
		&item.OrganizationID, &item.User.ID, &item.User.Email, &item.User.Username, &item.User.DisplayName, &item.User.AvatarURL,
		&item.User.SystemAdministrator, &item.Status, &item.AccountManagementAllowed, &item.CreatedAt, &item.UpdatedAt, &item.DisabledAt, &item.RemovedAt,
	)
	if err != nil {
		return OrganizationMember{}, err
	}
	if err := s.loadMemberAccessSummaries(ctx, &item); err != nil {
		return OrganizationMember{}, err
	}
	return item, nil
}

func (s *Service) SetOrganizationMemberStatus(ctx context.Context, organizationID, userID, actorID, status string) (OrganizationMember, error) {
	if status != "active" && status != "disabled" {
		return OrganizationMember{}, ErrMemberLifecycle
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OrganizationMember{}, err
	}
	defer rollback(ctx, tx)
	if _, err := tx.Exec(ctx, `LOCK TABLE organization_members IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return OrganizationMember{}, err
	}
	var current string
	var systemAdministrator bool
	if err := tx.QueryRow(ctx, `
		SELECT om.status, u.is_system_admin
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
		FOR UPDATE OF om, u
	`, organizationID, userID).Scan(&current, &systemAdministrator); err != nil {
		return OrganizationMember{}, err
	}
	if systemAdministrator {
		return OrganizationMember{}, ErrMemberAccountProtected
	}
	if current == "removed" || current == status {
		return OrganizationMember{}, ErrMemberLifecycle
	}
	if status == "disabled" {
		if err := ensureNotLastDirectOwner(ctx, tx, organizationID, userID); err != nil {
			return OrganizationMember{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE organization_members
			SET status = 'disabled', disabled_at = now(), disabled_by = $3,
			    authorization_version = authorization_version + 1, updated_at = now()
			WHERE organization_id = $1 AND user_id = $2
		`, organizationID, userID, actorID); err != nil {
			return OrganizationMember{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE organization_members
			SET status = 'active', disabled_at = NULL, disabled_by = NULL,
			    authorization_version = authorization_version + 1, updated_at = now()
			WHERE organization_id = $1 AND user_id = $2 AND status = 'disabled'
		`, organizationID, userID); err != nil {
			return OrganizationMember{}, err
		}
	}
	if err := revokeMembershipAuthorizationArtifacts(ctx, tx, organizationID, userID); err != nil {
		return OrganizationMember{}, err
	}
	action := auditlog.ActionMemberRestored
	if status == "disabled" {
		action = auditlog.ActionMemberDisabled
	}
	if err := auditlog.Append(ctx, tx, organizationID, actorID, action, "user", userID, map[string]any{
		"previousStatus": current,
		"status":         status,
	}); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationMember{}, err
	}
	return s.GetOrganizationMember(ctx, organizationID, userID)
}

func (s *Service) RemoveOrganizationMember(ctx context.Context, organizationID, userID, actorID string) error {
	return s.removeOrganizationMember(ctx, organizationID, userID, actorID, auditlog.ActionMemberRemoved)
}

func (s *Service) LeaveOrganization(ctx context.Context, organizationID, userID string) error {
	return s.removeOrganizationMember(ctx, organizationID, userID, userID, auditlog.ActionMemberLeft)
}

func (s *Service) removeOrganizationMember(ctx context.Context, organizationID, userID, actorID, action string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	if _, err := tx.Exec(ctx, `LOCK TABLE organization_members IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	var current string
	var systemAdministrator bool
	if err := tx.QueryRow(ctx, `
		SELECT om.status, u.is_system_admin
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
		FOR UPDATE OF om, u
	`, organizationID, userID).Scan(&current, &systemAdministrator); err != nil {
		return err
	}
	if systemAdministrator {
		return ErrMemberAccountProtected
	}
	if current == "removed" {
		return ErrMemberLifecycle
	}
	if err := ensureNotLastDirectOwner(ctx, tx, organizationID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM team_members tm
		USING teams t
		WHERE tm.team_id = t.id AND t.organization_id = $1 AND tm.user_id = $2
	`, organizationID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM project_members pm
		USING projects p
		WHERE pm.project_id = p.id AND p.organization_id = $1 AND pm.user_id = $2
	`, organizationID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM role_bindings
		WHERE organization_id = $1 AND subject_type = 'user' AND subject_user_id = $2
	`, organizationID, userID); err != nil {
		return err
	}
	if err := revokeMembershipAuthorizationArtifacts(ctx, tx, organizationID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE organization_members
		SET status = 'removed', removed_at = now(), removed_by = $3,
		    disabled_at = NULL, disabled_by = NULL,
		    authorization_version = authorization_version + 1, updated_at = now()
		WHERE organization_id = $1 AND user_id = $2
	`, organizationID, userID, actorID); err != nil {
		return err
	}
	if err := auditlog.Append(ctx, tx, organizationID, actorID, action, "user", userID, map[string]any{
		"previousStatus": current,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) loadMemberAccessSummaries(ctx context.Context, item *OrganizationMember) error {
	teamRows, err := s.db.Query(ctx, `
		SELECT t.id, t.name
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.user_id = $1 AND t.organization_id = $2
		  AND tm.status = 'active' AND t.status = 'active'
		ORDER BY t.name, t.id
	`, item.User.ID, item.OrganizationID)
	if err != nil {
		return err
	}
	item.Teams = make([]MemberTeamSummary, 0)
	for teamRows.Next() {
		var team MemberTeamSummary
		if err := teamRows.Scan(&team.ID, &team.Name); err != nil {
			teamRows.Close()
			return err
		}
		item.Teams = append(item.Teams, team)
	}
	if err := teamRows.Err(); err != nil {
		teamRows.Close()
		return err
	}
	teamRows.Close()

	roleRows, err := s.db.Query(ctx, `
		SELECT rb.id, r.id, r.role_key, r.name, rb.resource_type,
		       COALESCE(rb.resource_organization_id::text, rb.resource_workspace_id::text, rb.resource_project_id::text),
		       false, NULL::text, NULL::text, rb.expires_at
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		WHERE rb.organization_id = $1 AND rb.subject_type = 'user' AND rb.subject_user_id = $2
		  AND (rb.expires_at IS NULL OR rb.expires_at > now())
		UNION ALL
		SELECT rb.id, r.id, r.role_key, r.name, rb.resource_type,
		       COALESCE(rb.resource_organization_id::text, rb.resource_workspace_id::text, rb.resource_project_id::text),
		       true, t.id::text, t.name, rb.expires_at
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id AND t.organization_id = $1 AND t.status = 'active'
		JOIN role_bindings rb ON rb.subject_type = 'team' AND rb.subject_team_id = t.id AND rb.organization_id = $1
		JOIN roles r ON r.id = rb.role_id
		WHERE tm.user_id = $2 AND tm.status = 'active'
		  AND (rb.expires_at IS NULL OR rb.expires_at > now())
		ORDER BY 7, 4, 6
	`, item.OrganizationID, item.User.ID)
	if err != nil {
		return err
	}
	defer roleRows.Close()
	item.Roles = make([]MemberRoleSummary, 0)
	for roleRows.Next() {
		var role MemberRoleSummary
		if err := roleRows.Scan(
			&role.BindingID, &role.RoleID, &role.RoleKey, &role.RoleName, &role.ResourceType,
			&role.ResourceID, &role.ViaTeam, &role.TeamID, &role.TeamName, &role.ExpiresAt,
		); err != nil {
			return err
		}
		item.Roles = append(item.Roles, role)
	}
	return roleRows.Err()
}

func ensureNotLastDirectOwner(ctx context.Context, tx pgx.Tx, organizationID, targetUserID string) error {
	var targetIsOwner bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM role_bindings rb
			JOIN roles r ON r.id = rb.role_id
			WHERE rb.organization_id = $1 AND rb.subject_type = 'user' AND rb.subject_user_id = $2
			  AND rb.resource_type = 'organization' AND rb.resource_organization_id = $1
			  AND r.role_key IN ('org_owner', 'organization_owner')
			  AND (rb.expires_at IS NULL OR rb.expires_at > now())
		)
	`, organizationID, targetUserID).Scan(&targetIsOwner); err != nil {
		return err
	}
	if !targetIsOwner {
		return nil
	}
	var otherOwners int
	if err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT rb.subject_user_id)
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		JOIN organization_members om
		  ON om.organization_id = rb.organization_id AND om.user_id = rb.subject_user_id AND om.status = 'active'
		WHERE rb.organization_id = $1 AND rb.subject_type = 'user' AND rb.subject_user_id <> $2
		  AND rb.resource_type = 'organization' AND rb.resource_organization_id = $1
		  AND r.role_key IN ('org_owner', 'organization_owner')
		  AND (rb.expires_at IS NULL OR rb.expires_at > now())
	`, organizationID, targetUserID).Scan(&otherOwners); err != nil {
		return err
	}
	if otherOwners == 0 {
		return ErrLastOwner
	}
	return nil
}

func revokeOrganizationSessions(ctx context.Context, tx pgx.Tx, organizationID, userID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = now()
		WHERE organization_id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, organizationID, userID)
	return err
}

func revokeMembershipAuthorizationArtifacts(ctx context.Context, tx pgx.Tx, organizationID, userID string) error {
	if err := revokeOrganizationSessions(ctx, tx, organizationID, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE auth_organization_selection_nonces
		SET consumed_at = now()
		WHERE user_id = $1 AND consumed_at IS NULL
	`, userID)
	return err
}
