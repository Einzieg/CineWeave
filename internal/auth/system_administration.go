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
	ErrSystemAdministratorRequired  = errors.New("system administrator is required")
	ErrSystemOrganizationValidation = errors.New("system organization request is invalid")
	ErrSystemOwnerNotFound          = errors.New("initial organization owner was not found")
)

type CreateSystemOrganizationRequest struct {
	Name            string `json:"name"`
	WorkspaceName   string `json:"workspaceName"`
	OwnerIdentifier string `json:"ownerIdentifier"`
}

type SystemOrganization struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	CreatedAt         time.Time `json:"createdAt"`
	ActiveMemberCount int       `json:"activeMemberCount"`
	WorkspaceCount    int       `json:"workspaceCount"`
	ProjectCount      int       `json:"projectCount"`
	OwnerCount        int       `json:"ownerCount"`
}

type SystemOrganizationList struct {
	Items    []SystemOrganization `json:"items"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
	Total    int                  `json:"total"`
}

type CreatedSystemOrganization struct {
	Organization       SystemOrganization `json:"organization"`
	InitialOwner       UserResponse       `json:"initialOwner"`
	DefaultWorkspaceID string             `json:"defaultWorkspaceId"`
}

func (s *Service) RequireSystemAdministrator(ctx context.Context, userID string) error {
	var allowed bool
	err := s.db.QueryRow(ctx, `
		SELECT is_system_admin
		FROM users
		WHERE id = $1 AND status = 'active'
	`, strings.TrimSpace(userID)).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return ErrSystemAdministratorRequired
	}
	return err
}

func (s *Service) ListSystemOrganizations(ctx context.Context, actorUserID, search string, page, pageSize int) (SystemOrganizationList, error) {
	if err := s.RequireSystemAdministrator(ctx, actorUserID); err != nil {
		return SystemOrganizationList{}, err
	}
	search = strings.TrimSpace(search)
	if len([]rune(search)) > 100 {
		return SystemOrganizationList{}, ErrSystemOrganizationValidation
	}
	page, pageSize = normalizeSystemPagination(page, pageSize)

	var total int
	if err := s.db.QueryRow(ctx, `
		SELECT count(*)
		FROM organizations o
		WHERE $1 = '' OR o.name ILIKE '%' || $1 || '%' OR o.slug ILIKE '%' || $1 || '%'
	`, search).Scan(&total); err != nil {
		return SystemOrganizationList{}, err
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			o.id,
			o.name,
			o.slug,
			o.created_at,
			(SELECT count(*) FROM organization_members om WHERE om.organization_id = o.id AND om.status = 'active'),
			(SELECT count(*) FROM workspaces w WHERE w.organization_id = o.id),
			(SELECT count(*) FROM projects p WHERE p.organization_id = o.id),
			(
				SELECT count(DISTINCT rb.subject_user_id)
				FROM role_bindings rb
				JOIN roles r ON r.id = rb.role_id
				JOIN organization_members om ON om.organization_id = o.id AND om.user_id = rb.subject_user_id AND om.status = 'active'
				WHERE rb.organization_id = o.id
				  AND rb.subject_type = 'user'
				  AND rb.resource_type = 'organization'
				  AND rb.resource_organization_id = o.id
				  AND (rb.expires_at IS NULL OR rb.expires_at > now())
				  AND r.role_key IN ('org_owner', 'organization_owner')
			)
		FROM organizations o
		WHERE $1 = '' OR o.name ILIKE '%' || $1 || '%' OR o.slug ILIKE '%' || $1 || '%'
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT $2 OFFSET $3
	`, search, pageSize, (page-1)*pageSize)
	if err != nil {
		return SystemOrganizationList{}, err
	}
	defer rows.Close()

	items := make([]SystemOrganization, 0)
	for rows.Next() {
		var item SystemOrganization
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Slug,
			&item.CreatedAt,
			&item.ActiveMemberCount,
			&item.WorkspaceCount,
			&item.ProjectCount,
			&item.OwnerCount,
		); err != nil {
			return SystemOrganizationList{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return SystemOrganizationList{}, err
	}
	return SystemOrganizationList{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) CreateSystemOrganization(ctx context.Context, actorUserID string, req CreateSystemOrganizationRequest) (CreatedSystemOrganization, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedSystemOrganization{}, err
	}
	defer rollback(ctx, tx)

	var administrator bool
	if err := tx.QueryRow(ctx, `
		SELECT is_system_admin
		FROM users
		WHERE id = $1 AND status = 'active'
		FOR SHARE
	`, strings.TrimSpace(actorUserID)).Scan(&administrator); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreatedSystemOrganization{}, ErrSystemAdministratorRequired
		}
		return CreatedSystemOrganization{}, err
	}
	if !administrator {
		return CreatedSystemOrganization{}, ErrSystemAdministratorRequired
	}
	name, workspaceName, ownerIdentifier, ownerIsEmail, err := normalizeCreateSystemOrganizationRequest(req)
	if err != nil {
		return CreatedSystemOrganization{}, err
	}

	var owner UserResponse
	err = tx.QueryRow(ctx, `
		SELECT id, email, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), is_system_admin
		FROM users
		WHERE status = 'active'
		  AND (($2::boolean AND email = $1) OR (NOT $2::boolean AND username_normalized = $1))
	`, ownerIdentifier, ownerIsEmail).Scan(
		&owner.ID,
		&owner.Email,
		&owner.Username,
		&owner.DisplayName,
		&owner.AvatarURL,
		&owner.SystemAdministrator,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreatedSystemOrganization{}, ErrSystemOwnerNotFound
	}
	if err != nil {
		return CreatedSystemOrganization{}, err
	}

	organizationID, workspaceID, err := createOrganizationForUser(ctx, tx, owner.ID, actorUserID, name, workspaceName)
	if err != nil {
		return CreatedSystemOrganization{}, err
	}
	var organization SystemOrganization
	if err := tx.QueryRow(ctx, `
		SELECT id, name, slug, created_at
		FROM organizations
		WHERE id = $1
	`, organizationID).Scan(&organization.ID, &organization.Name, &organization.Slug, &organization.CreatedAt); err != nil {
		return CreatedSystemOrganization{}, err
	}
	organization.ActiveMemberCount = 1
	organization.WorkspaceCount = 1
	organization.OwnerCount = 1

	if err := auditlog.Append(ctx, tx, organizationID, actorUserID, auditlog.ActionSystemOrganizationCreated, "organization", organizationID, map[string]any{
		"defaultWorkspaceId": workspaceID,
		"initialOwnerUserId": owner.ID,
	}); err != nil {
		return CreatedSystemOrganization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedSystemOrganization{}, err
	}
	return CreatedSystemOrganization{
		Organization:       organization,
		InitialOwner:       owner,
		DefaultWorkspaceID: workspaceID,
	}, nil
}

func normalizeCreateSystemOrganizationRequest(req CreateSystemOrganizationRequest) (string, string, string, bool, error) {
	name := strings.TrimSpace(req.Name)
	workspaceName := strings.TrimSpace(req.WorkspaceName)
	if workspaceName == "" {
		workspaceName = "默认工作区"
	}
	ownerIdentifier, ownerIsEmail := NormalizeLoginIdentifier(req.OwnerIdentifier)
	if len([]rune(name)) < 1 || len([]rune(name)) > 100 || len([]rune(workspaceName)) > 100 || ownerIdentifier == "" || len(ownerIdentifier) > 320 {
		return "", "", "", false, ErrSystemOrganizationValidation
	}
	return name, workspaceName, ownerIdentifier, ownerIsEmail, nil
}

func normalizeSystemPagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
