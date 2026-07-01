package api

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
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRBAC(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run RBAC API integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for RBAC API integration test")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	authService := auth.NewService(pool, "rbac-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	providerService := provider.NewService(pool, vault)
	server := New(pool, authService, providerService, nil, nil).Handler()
	ensureRBACProviderConnector(t, ctx, pool)

	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "rbac-owner-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "RBAC Owner",
		OrganizationName: "RBAC Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	member, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "rbac-member-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "RBAC Member",
		OrganizationName: "RBAC Member Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, member.OrganizationID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, owner.OrganizationID, member.User.ID); err != nil {
		t.Fatalf("insert member org membership: %v", err)
	}

	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, server, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID,
		"name":        "RBAC Project",
		"settings":    map[string]any{},
	}, &project)

	assertAPIErrorCode(t, server, http.MethodGet, "/api/projects/"+project.ID, member.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "ACCESS_DENIED")

	createUserRoleBinding(t, server, pool, owner, member.User.ID, "project_viewer", "project", "", "", project.ID)
	var readable Project
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+project.ID, member.AccessToken, owner.OrganizationID, nil, &readable)
	if readable.ID != project.ID {
		t.Fatalf("read project id = %s, want %s", readable.ID, project.ID)
	}
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+project.ID, member.AccessToken, owner.OrganizationID, map[string]any{"name": "Denied"}, http.StatusForbidden, "ACCESS_DENIED")

	createUserRoleBinding(t, server, pool, owner, member.User.ID, "project_editor", "project", "", "", project.ID)
	var updated Project
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+project.ID, member.AccessToken, owner.OrganizationID, map[string]any{"name": "Edited Project"}, &updated)
	if updated.Name != "Edited Project" {
		t.Fatalf("updated project name = %q", updated.Name)
	}

	assertAPIErrorCode(t, server, http.MethodPost, "/api/providers/accounts", member.AccessToken, owner.OrganizationID, providerAccountBody(owner.OrganizationID), http.StatusForbidden, "ACCESS_DENIED")
	ensureRBACProviderCatalog(t, ctx, pool)
	assertAPIErrorCode(t, server, http.MethodPost, "/api/provider-catalog/deepseek/install", member.AccessToken, owner.OrganizationID, providerCatalogInstallBody(owner.OrganizationID), http.StatusForbidden, "ACCESS_DENIED")
	createUserRoleBinding(t, server, pool, owner, member.User.ID, "provider_admin", "organization", owner.OrganizationID, "", "")
	var account provider.Account
	doAPISuccess(t, server, http.MethodPost, "/api/providers/accounts", member.AccessToken, owner.OrganizationID, providerAccountBody(owner.OrganizationID), &account)
	if account.OrganizationID != owner.OrganizationID {
		t.Fatalf("provider account org = %s", account.OrganizationID)
	}
	var installed provider.InstallCatalogResponse
	doAPISuccess(t, server, http.MethodPost, "/api/provider-catalog/deepseek/install", member.AccessToken, owner.OrganizationID, providerCatalogInstallBody(owner.OrganizationID), &installed)
	if installed.Account.OrganizationID != owner.OrganizationID || len(installed.Models) == 0 {
		t.Fatalf("catalog install response = %+v", installed)
	}

	viewerOnly := registerRBACOrgMember(t, ctx, pool, authService, owner.OrganizationID, suffix)
	createUserRoleBinding(t, server, pool, owner, viewerOnly.User.ID, "project_viewer", "project", "", "", project.ID)
	workflowID := insertRBACWorkflowRun(t, ctx, pool, owner.OrganizationID, project.ID, owner.User.ID, "running")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/workflow-runs/"+workflowID+"/cancel", viewerOnly.AccessToken, owner.OrganizationID, map[string]any{"reason": "no permission"}, http.StatusForbidden, "ACCESS_DENIED")
	var cancelled WorkflowRun
	doAPISuccess(t, server, http.MethodPost, "/api/workflow-runs/"+workflowID+"/cancel", owner.AccessToken, owner.OrganizationID, map[string]any{"reason": "owner cancel"}, &cancelled)
	if cancelled.Status != "cancelling" {
		t.Fatalf("cancel status = %s", cancelled.Status)
	}
}

func createUserRoleBinding(t *testing.T, handler http.Handler, pool dbQueryer, owner auth.TokenResponse, userID, roleKey, resourceType, resourceOrgID, resourceWorkspaceID, resourceProjectID string) {
	t.Helper()
	roleID := roleIDByKey(t, context.Background(), pool, roleKey)
	body := map[string]any{
		"organizationId": owner.OrganizationID,
		"roleId":         roleID,
		"subjectType":    "user",
		"subjectUserId":  userID,
		"resourceType":   resourceType,
	}
	if resourceOrgID != "" {
		body["resourceOrganizationId"] = resourceOrgID
	}
	if resourceWorkspaceID != "" {
		body["resourceWorkspaceId"] = resourceWorkspaceID
	}
	if resourceProjectID != "" {
		body["resourceProjectId"] = resourceProjectID
	}
	var binding RoleBinding
	doAPISuccess(t, handler, http.MethodPost, "/api/role-bindings", owner.AccessToken, owner.OrganizationID, body, &binding)
	if binding.RoleID != roleID {
		t.Fatalf("role binding role = %s, want %s", binding.RoleID, roleID)
	}
}

func registerRBACOrgMember(t *testing.T, ctx context.Context, pool dbQueryer, authService *auth.Service, orgID, suffix string) auth.TokenResponse {
	t.Helper()
	resp, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "rbac-viewer-" + uuid.NewString() + "-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "RBAC Viewer",
		OrganizationName: "RBAC Viewer Org " + uuid.NewString(),
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register org member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, resp.OrganizationID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, orgID, resp.User.ID); err != nil {
		t.Fatalf("insert org member: %v", err)
	}
	return resp
}

func ensureRBACProviderConnector(t *testing.T, ctx context.Context, pool dbQueryer) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ('openai_compatible', 'OpenAI Compatible', 'http', true, '{}', 'v1')
		ON CONFLICT (connector_key) DO NOTHING
	`); err != nil {
		t.Fatalf("ensure provider connector: %v", err)
	}
}

func ensureRBACProviderCatalog(t *testing.T, ctx context.Context, pool dbQueryer) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_catalog_entries(
			provider_key, name, display_name, provider_type, category,
			default_base_url, default_auth_type, connector_manifest,
			model_templates, supported_task_types, setup_schema,
			enabled, is_official
		)
		VALUES (
			'deepseek', 'deepseek', 'DeepSeek', 'openai_compatible', 'text',
			'https://api.deepseek.com', 'bearer', '{}',
			'[{"modelKey":"deepseek-chat","displayName":"DeepSeek Chat","modality":"text","taskTypes":["text.generate","text.stream"]}]',
			'["text.generate","text.stream"]',
			'{"defaultConfig":{"disableV1Prefix":true,"chatCompletionsEndpoint":"/chat/completions","modelsEndpoint":"/models"},"fields":[]}',
			true, true
		)
		ON CONFLICT (provider_key) DO UPDATE SET
			model_templates = EXCLUDED.model_templates,
			setup_schema = EXCLUDED.setup_schema
	`); err != nil {
		t.Fatalf("ensure provider catalog: %v", err)
	}
}

func firstWorkspaceID(t *testing.T, ctx context.Context, pool dbQueryer, orgID string) string {
	t.Helper()
	var workspaceID string
	if err := pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE organization_id = $1 ORDER BY created_at LIMIT 1`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("select workspace: %v", err)
	}
	return workspaceID
}

func roleIDByKey(t *testing.T, ctx context.Context, pool dbQueryer, roleKey string) string {
	t.Helper()
	var roleID string
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id IS NULL AND role_key = $1 LIMIT 1`, roleKey).Scan(&roleID); err != nil {
		t.Fatalf("select role %s: %v", roleKey, err)
	}
	return roleID
}

func insertRBACWorkflowRun(t *testing.T, ctx context.Context, pool dbQueryer, orgID, projectID, userID, status string) string {
	t.Helper()
	var workflowID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		VALUES ($1, $2, $3, $4, '{}', '{}', $5)
		RETURNING id
	`, orgID, projectID, "rbac-workflow-"+uuid.NewString(), status, userID).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	return workflowID
}

func providerAccountBody(orgID string) map[string]any {
	return map[string]any{
		"organizationId": orgID,
		"connectorKey":   "openai_compatible",
		"name":           "RBAC Provider " + uuid.NewString(),
		"baseUrl":        "http://127.0.0.1:19180/v1",
		"authType":       "bearer",
		"credential": map[string]any{
			"apiKey": "sk-rbac-test",
		},
		"config": map[string]any{},
	}
}

func providerCatalogInstallBody(orgID string) map[string]any {
	return map[string]any{
		"organizationId": orgID,
		"name":           "RBAC Catalog DeepSeek " + uuid.NewString(),
		"baseUrl":        "https://api.deepseek.com",
		"apiKey":         "sk-rbac-catalog-test",
		"models": []map[string]any{{
			"modelKey":    "deepseek-chat",
			"displayName": "DeepSeek Chat",
			"modality":    "text",
			"taskTypes":   []string{"text.generate", "text.stream"},
		}},
	}
}

type dbQueryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
