package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthorizerRBAC(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run authz integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for authz integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	authService := auth.NewService(pool, "authz-test-secret", time.Hour, 24*time.Hour)
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "authz-owner-" + uuid.NewString() + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Authz Owner",
		OrganizationName: "Authz Org " + uuid.NewString(),
	}, testRequest())
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})

	viewer, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "authz-viewer-" + uuid.NewString() + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Authz Viewer",
		OrganizationName: "Authz Viewer Org " + uuid.NewString(),
	}, testRequest())
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, viewer.OrganizationID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, owner.OrganizationID, viewer.User.ID); err != nil {
		t.Fatalf("insert viewer org member: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE organization_id = $1 LIMIT 1`, owner.OrganizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("select workspace: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Authz Project', $3)
		RETURNING id
	`, owner.OrganizationID, workspaceID, owner.User.ID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	authorizer := New(pool)
	ownerPrincipal := auth.Principal{UserID: owner.User.ID, OrganizationID: owner.OrganizationID}
	if err := authorizer.Authorize(ctx, ownerPrincipal, PermissionProviderManage, Resource{OrganizationID: owner.OrganizationID}); err != nil {
		t.Fatalf("org_owner provider.manage: %v", err)
	}

	viewerPrincipal := auth.Principal{UserID: viewer.User.ID, OrganizationID: owner.OrganizationID}
	if err := authorizer.Authorize(ctx, viewerPrincipal, PermissionProjectRead, Resource{ProjectID: projectID}); err == nil {
		t.Fatal("viewer without role unexpectedly read project")
	}
	bindRole(t, ctx, pool, owner.OrganizationID, "project_viewer", "user", viewer.User.ID, "", "project", "", "", projectID, owner.User.ID)
	if err := authorizer.Authorize(ctx, viewerPrincipal, PermissionProjectRead, Resource{ProjectID: projectID}); err != nil {
		t.Fatalf("project_viewer project.read: %v", err)
	}
	if err := authorizer.Authorize(ctx, viewerPrincipal, PermissionProjectWrite, Resource{ProjectID: projectID}); err == nil {
		t.Fatal("project_viewer unexpectedly wrote project")
	}
	bindRole(t, ctx, pool, owner.OrganizationID, "project_editor", "user", viewer.User.ID, "", "project", "", "", projectID, owner.User.ID)
	if err := authorizer.Authorize(ctx, viewerPrincipal, PermissionAssetWrite, Resource{ProjectID: projectID}); err != nil {
		t.Fatalf("project_editor asset.write: %v", err)
	}
	bindRole(t, ctx, pool, owner.OrganizationID, "org_member", "user", viewer.User.ID, "", "organization", owner.OrganizationID, "", "", owner.User.ID)
	if err := authorizer.Authorize(ctx, viewerPrincipal, PermissionProjectRead, Resource{ProjectID: projectID}); err != nil {
		t.Fatalf("organization binding project.read inheritance: %v", err)
	}
	workspaceViewer := registerUserInOrg(t, ctx, pool, authService, owner.OrganizationID)
	bindRole(t, ctx, pool, owner.OrganizationID, "project_viewer", "user", workspaceViewer.User.ID, "", "workspace", "", workspaceID, "", owner.User.ID)
	if err := authorizer.Authorize(ctx, auth.Principal{UserID: workspaceViewer.User.ID, OrganizationID: owner.OrganizationID}, PermissionProjectRead, Resource{ProjectID: projectID}); err != nil {
		t.Fatalf("workspace binding project.read inheritance: %v", err)
	}
	teamMember := registerUserInOrg(t, ctx, pool, authService, owner.OrganizationID)
	teamID := createAuthzTeam(t, ctx, pool, owner.OrganizationID, owner.User.ID)
	if _, err := pool.Exec(ctx, `INSERT INTO team_members(team_id, user_id, status, created_by) VALUES ($1, $2, 'active', $3)`, teamID, teamMember.User.ID, owner.User.ID); err != nil {
		t.Fatalf("insert active team member: %v", err)
	}
	bindRole(t, ctx, pool, owner.OrganizationID, "project_viewer", "team", "", teamID, "project", "", "", projectID, owner.User.ID)
	teamPrincipal := auth.Principal{UserID: teamMember.User.ID, OrganizationID: owner.OrganizationID}
	if err := authorizer.Authorize(ctx, teamPrincipal, PermissionProjectRead, Resource{ProjectID: projectID}); err != nil {
		t.Fatalf("active team member project.read: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE team_members SET status = 'disabled' WHERE team_id = $1 AND user_id = $2`, teamID, teamMember.User.ID); err != nil {
		t.Fatalf("disable team member: %v", err)
	}
	if err := authorizer.Authorize(ctx, teamPrincipal, PermissionProjectRead, Resource{ProjectID: projectID}); err == nil {
		t.Fatal("inactive team member unexpectedly retained permission")
	}
}

func testRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/auth/register", nil)
}

func registerUserInOrg(t *testing.T, ctx context.Context, pool *pgxpool.Pool, authService *auth.Service, orgID string) auth.TokenResponse {
	t.Helper()
	resp, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "authz-user-" + uuid.NewString() + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Authz User",
		OrganizationName: "Authz Temp Org " + uuid.NewString(),
	}, testRequest())
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, resp.OrganizationID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, orgID, resp.User.ID); err != nil {
		t.Fatalf("insert org member: %v", err)
	}
	return resp
}

func bindRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, roleKey, subjectType, subjectUserID, subjectTeamID, resourceType, resourceOrgID, resourceWorkspaceID, resourceProjectID, createdBy string) {
	t.Helper()
	var roleID string
	if err := pool.QueryRow(ctx, `
		SELECT id
		FROM roles
		WHERE organization_id IS NULL AND role_key = $1
		LIMIT 1
	`, roleKey).Scan(&roleID); err != nil {
		t.Fatalf("select role %s: %v", roleKey, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id, subject_team_id,
			resource_type, resource_organization_id, resource_workspace_id, resource_project_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
	`, orgID, roleID, subjectType, nullString(subjectUserID), nullString(subjectTeamID), resourceType, nullString(resourceOrgID), nullString(resourceWorkspaceID), nullString(resourceProjectID), createdBy); err != nil {
		t.Fatalf("bind role %s: %v", roleKey, err)
	}
}

func createAuthzTeam(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, createdBy string) string {
	t.Helper()
	var teamID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO teams(organization_id, name, slug, status, created_by)
		VALUES ($1, 'Authz Team', $2, 'active', $3)
		RETURNING id
	`, orgID, "authz-team-"+uuid.NewString(), createdBy).Scan(&teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	return teamID
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
