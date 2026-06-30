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
)

func TestPublicRegistrationDisabledByDefault(t *testing.T) {
	t.Setenv("CINEWEAVE_ALLOW_PUBLIC_REGISTRATION", "")
	server := (&Server{}).Handler()

	assertAPIErrorCode(t, server, http.MethodPost, "/api/auth/register", "", "", map[string]any{
		"email":            "admin@example.test",
		"password":         "Password123!",
		"displayName":      "Admin",
		"organizationName": "Org",
	}, http.StatusForbidden, "PUBLIC_REGISTRATION_DISABLED")
}

func TestSystemSetupFlow(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run system setup API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for system setup API tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	var existingUsers int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&existingUsers); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if existingUsers > 0 {
		t.Skip("system setup integration test requires a clean users table")
	}

	authService := auth.NewService(pool, "setup-test-secret", time.Hour, 24*time.Hour)
	server := New(pool, authService, nil, nil, nil).Handler()

	var state SetupStateResponse
	doAPISuccess(t, server, http.MethodGet, "/api/system/setup-state", "", "", nil, &state)
	if !state.NeedsSetup || state.UserCount != 0 {
		t.Fatalf("initial setup state = %+v", state)
	}

	var setup auth.TokenResponse
	doAPISuccess(t, server, http.MethodPost, "/api/system/setup", "", "", map[string]any{
		"email":            "setup-admin@example.test",
		"password":         "Password123!",
		"displayName":      "管理员",
		"organizationName": "影织工作室",
		"workspaceName":    "默认工作区",
	}, &setup)
	t.Cleanup(func() {
		if setup.OrganizationID != "" {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, setup.OrganizationID)
		}
		if setup.User.ID != "" {
			_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, setup.User.ID)
		}
	})
	if setup.AccessToken == "" || setup.RefreshToken == "" || setup.OrganizationID == "" || setup.WorkspaceID == "" || setup.User.ID == "" {
		t.Fatalf("setup response missing session fields: %+v", setup)
	}

	assertAPIErrorCode(t, server, http.MethodPost, "/api/system/setup", "", "", map[string]any{
		"email":            "second-admin@example.test",
		"password":         "Password123!",
		"displayName":      "Second",
		"organizationName": "Second Org",
		"workspaceName":    "Default",
	}, http.StatusConflict, "SETUP_ALREADY_COMPLETED")

	var ownerBindingCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		WHERE rb.organization_id = $1
		  AND rb.subject_user_id = $2
		  AND rb.resource_type = 'organization'
		  AND r.role_key IN ('org_owner', 'organization_owner')
	`, setup.OrganizationID, setup.User.ID).Scan(&ownerBindingCount); err != nil {
		t.Fatalf("count owner bindings: %v", err)
	}
	if ownerBindingCount == 0 {
		t.Fatalf("setup did not create an org owner binding")
	}

	login, err := authService.Login(ctx, auth.LoginRequest{Email: "setup-admin@example.test", Password: "Password123!"}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if err != nil {
		t.Fatalf("login after setup: %v", err)
	}
	if login.OrganizationID != setup.OrganizationID || login.WorkspaceID != setup.WorkspaceID {
		t.Fatalf("login session = %+v, setup = %+v", login, setup)
	}
}
