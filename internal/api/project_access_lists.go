package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maximumAccessibleListPageSize = 100

type accessibleListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type accessiblePage[T any] struct {
	Items      []T
	NextCursor string
}

func (s *Server) listAccessibleOrganizations(ctx context.Context, actor auth.Principal, limit int, cursor string) (accessiblePage[Organization], error) {
	limit = boundedAccessibleLimit(limit)
	position, err := decodeAccessibleCursor(cursor)
	if err != nil {
		return accessiblePage[Organization]{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT o.id::text, o.name, o.slug, o.created_at
		FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id
		WHERE om.user_id = $1
		  AND om.status = 'active'
		  AND ($2::timestamptz IS NULL OR (o.created_at, o.id) < ($2::timestamptz, NULLIF($3, '')::uuid))
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT 1000
	`, actor.UserID, positionTime(position), positionID(position))
	if err != nil {
		return accessiblePage[Organization]{}, err
	}
	defer rows.Close()
	items := make([]Organization, 0, limit+1)
	for rows.Next() {
		var item Organization
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.CreatedAt); err != nil {
			return accessiblePage[Organization]{}, err
		}
		principal := actor
		principal.OrganizationID = item.ID
		if err := s.authorizer.Authorize(ctx, principal, authz.PermissionOrganizationRead, authz.Resource{OrganizationID: item.ID}); err != nil {
			if errors.Is(err, authz.ErrAccessDenied) {
				continue
			}
			return accessiblePage[Organization]{}, err
		}
		items = append(items, item)
		if len(items) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return accessiblePage[Organization]{}, err
	}
	return finalizeAccessiblePage(items, limit, func(item Organization) accessibleListCursor {
		return accessibleListCursor{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func (s *Server) listAccessibleWorkspaces(ctx context.Context, actor auth.Principal, organizationID string, limit int, cursor string) (accessiblePage[Workspace], error) {
	limit = boundedAccessibleLimit(limit)
	position, err := decodeAccessibleCursor(cursor)
	if err != nil {
		return accessiblePage[Workspace]{}, err
	}
	if err := s.ensureActiveOrganizationMembership(ctx, actor.UserID, organizationID); err != nil {
		return accessiblePage[Workspace]{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text, organization_id::text, name, created_at
		FROM workspaces
		WHERE organization_id = $1
		  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2::timestamptz, NULLIF($3, '')::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT 1000
	`, organizationID, positionTime(position), positionID(position))
	if err != nil {
		return accessiblePage[Workspace]{}, err
	}
	defer rows.Close()
	principal := actor
	principal.OrganizationID = organizationID
	items := make([]Workspace, 0, limit+1)
	for rows.Next() {
		var item Workspace
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Name, &item.CreatedAt); err != nil {
			return accessiblePage[Workspace]{}, err
		}
		if err := s.authorizer.Authorize(ctx, principal, authz.PermissionWorkspaceRead, authz.Resource{WorkspaceID: item.ID}); err != nil {
			if errors.Is(err, authz.ErrAccessDenied) {
				continue
			}
			return accessiblePage[Workspace]{}, err
		}
		items = append(items, item)
		if len(items) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return accessiblePage[Workspace]{}, err
	}
	return finalizeAccessiblePage(items, limit, func(item Workspace) accessibleListCursor {
		return accessibleListCursor{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func (s *Server) listAccessibleProjects(ctx context.Context, actor auth.Principal, organizationID, workspaceID string, limit int, cursor string) (accessiblePage[Project], error) {
	limit = boundedAccessibleLimit(limit)
	position, err := decodeAccessibleCursor(cursor)
	if err != nil {
		return accessiblePage[Project]{}, err
	}
	if err := s.ensureActiveOrganizationMembership(ctx, actor.UserID, organizationID); err != nil {
		return accessiblePage[Project]{}, err
	}
	where := `
		WHERE p.organization_id = $1
		  AND p.lifecycle_status = 'active'
		  AND ($2 = '' OR p.workspace_id = NULLIF($2, '')::uuid)
		  AND ($3::timestamptz IS NULL OR (p.created_at, p.id) < ($3::timestamptz, NULLIF($4, '')::uuid))
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT 1000
	`
	rows, err := s.db.Query(ctx, projectSelectSQL(where), organizationID, workspaceID, positionTime(position), positionID(position))
	if err != nil {
		return accessiblePage[Project]{}, err
	}
	defer rows.Close()
	principal := actor
	principal.OrganizationID = organizationID
	items := make([]Project, 0, limit+1)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return accessiblePage[Project]{}, err
		}
		if err := s.authorizer.Authorize(ctx, principal, authz.PermissionProjectRead, authz.Resource{ProjectID: item.ID}); err != nil {
			if errors.Is(err, authz.ErrAccessDenied) {
				continue
			}
			return accessiblePage[Project]{}, err
		}
		if err := s.attachCommerceSetupContext(ctx, s.db, &item); err != nil {
			return accessiblePage[Project]{}, err
		}
		if err := s.attachVideoProductionContext(ctx, s.db, &item); err != nil {
			return accessiblePage[Project]{}, err
		}
		items = append(items, item)
		if len(items) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return accessiblePage[Project]{}, err
	}
	return finalizeAccessiblePage(items, limit, func(item Project) accessibleListCursor {
		return accessibleListCursor{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func (s *Server) ensureActiveOrganizationMembership(ctx context.Context, userID, organizationID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(organizationID) == "" {
		return auth.ErrForbidden
	}
	var active bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM organization_members
			WHERE user_id = $1 AND organization_id = $2 AND status = 'active'
		)
	`, userID, organizationID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return auth.ErrForbidden
	}
	return nil
}

func boundedAccessibleLimit(value int) int {
	if value <= 0 {
		return 20
	}
	if value > maximumAccessibleListPageSize {
		return maximumAccessibleListPageSize
	}
	return value
}

func finalizeAccessiblePage[T any](items []T, limit int, cursorFor func(T) accessibleListCursor) (accessiblePage[T], error) {
	page := accessiblePage[T]{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	next, err := encodeAccessibleCursor(cursorFor(page.Items[len(page.Items)-1]))
	if err != nil {
		return accessiblePage[T]{}, err
	}
	page.NextCursor = next
	return page, nil
}

func encodeAccessibleCursor(cursor accessibleListCursor) (string, error) {
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return "", fmt.Errorf("list cursor is incomplete")
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeAccessibleCursor(value string) (*accessibleListCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, newAPIError(422, "CURSOR_INVALID", "分页游标无效")
	}
	var cursor accessibleListCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.CreatedAt.IsZero() || uuid.Validate(cursor.ID) != nil {
		return nil, newAPIError(422, "CURSOR_INVALID", "分页游标无效")
	}
	return &cursor, nil
}

func positionTime(cursor *accessibleListCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.CreatedAt
}

func positionID(cursor *accessibleListCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.ID
}

func projectByIDForControl(ctx context.Context, server *Server, projectID string) (Project, error) {
	item, err := server.projectIncludingDeleting(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, newAPIError(404, "PROJECT_NOT_FOUND", "项目不存在")
	}
	return item, err
}
