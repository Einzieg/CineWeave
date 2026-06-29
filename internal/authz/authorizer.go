package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAccessDenied = errors.New("access denied")

type AccessError struct {
	Permission string
	Resource   Resource
}

func (e AccessError) Error() string {
	return fmt.Sprintf("%s: missing permission %s", ErrAccessDenied, e.Permission)
}

func (e AccessError) Unwrap() error {
	return ErrAccessDenied
}

type Authorizer struct {
	DB *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Authorizer {
	return &Authorizer{DB: db}
}

func (a *Authorizer) Authorize(ctx context.Context, principal auth.Principal, permission string, resource Resource) error {
	if a == nil || a.DB == nil {
		return ErrAccessDenied
	}
	permission = strings.TrimSpace(permission)
	if strings.TrimSpace(principal.UserID) == "" || permission == "" {
		return AccessError{Permission: permission, Resource: resource}
	}
	resolved, err := a.resolveResource(ctx, principal, resource)
	if err != nil {
		return err
	}
	if resolved.OrganizationID == "" {
		return AccessError{Permission: permission, Resource: resolved}
	}
	var member bool
	if err := a.DB.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND status = 'active'
		)
	`, resolved.OrganizationID, principal.UserID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return AccessError{Permission: permission, Resource: resolved}
	}
	var allowed bool
	if err := a.DB.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM role_bindings rb
			JOIN roles r ON r.id = rb.role_id
			JOIN role_permissions rp ON rp.role_id = r.id
			WHERE rb.organization_id = $1
			  AND (rb.expires_at IS NULL OR rb.expires_at > now())
			  AND (rp.permission_key = $3 OR rp.permission_key = 'admin.manage')
			  AND (
				(rb.subject_type = 'user' AND rb.subject_user_id = $2)
				OR (
					rb.subject_type = 'team'
					AND EXISTS (
						SELECT 1
						FROM team_members tm
						JOIN teams t ON t.id = tm.team_id
						WHERE tm.team_id = rb.subject_team_id
						  AND tm.user_id = $2
						  AND COALESCE(tm.status, 'active') = 'active'
						  AND t.organization_id = $1
						  AND COALESCE(t.status, 'active') = 'active'
					)
				)
			  )
			  AND (
				(rb.resource_type = 'organization' AND rb.resource_organization_id = $1)
				OR ($4::text IN ('workspace', 'project') AND rb.resource_type = 'workspace' AND rb.resource_workspace_id = $5::uuid)
				OR ($4::text = 'project' AND rb.resource_type = 'project' AND rb.resource_project_id = $6::uuid)
			  )
		)
	`, resolved.OrganizationID, principal.UserID, permission, string(resolved.Type), nullUUID(resolved.WorkspaceID), nullUUID(resolved.ProjectID)).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return AccessError{Permission: permission, Resource: resolved}
	}
	return nil
}

func (a *Authorizer) resolveResource(ctx context.Context, principal auth.Principal, resource Resource) (Resource, error) {
	if resource.ProjectID != "" {
		var organizationID, workspaceID string
		if err := a.DB.QueryRow(ctx, `
			SELECT organization_id, workspace_id
			FROM projects
			WHERE id = $1
		`, resource.ProjectID).Scan(&organizationID, &workspaceID); err != nil {
			return Resource{}, err
		}
		resource.Type = ResourceProject
		resource.OrganizationID = organizationID
		resource.WorkspaceID = workspaceID
		return resource, nil
	}
	if resource.WorkspaceID != "" {
		var organizationID string
		if err := a.DB.QueryRow(ctx, `
			SELECT organization_id
			FROM workspaces
			WHERE id = $1
		`, resource.WorkspaceID).Scan(&organizationID); err != nil {
			return Resource{}, err
		}
		resource.Type = ResourceWorkspace
		resource.OrganizationID = organizationID
		return resource, nil
	}
	if resource.OrganizationID == "" {
		resource.OrganizationID = principal.OrganizationID
	}
	if resource.OrganizationID == "" {
		return Resource{}, pgx.ErrNoRows
	}
	resource.Type = ResourceOrganization
	return resource, nil
}

func nullUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}
