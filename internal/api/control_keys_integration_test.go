package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/google/uuid"
)

func TestCodexControlKeyLifecycleIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run Codex control key integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for Codex control key integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	authService := auth.NewService(pool, "control-key-integration-secret", time.Hour, 24*time.Hour)
	registered, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "control-key-" + suffix + "@example.test",
		Username:         "control-key-" + suffix,
		Password:         "Password123!",
		DisplayName:      "Control Key Test",
		OrganizationName: "Control Key Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registered.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registered.User.ID)
	})
	if registered.CodexControlKey == nil || registered.CodexControlKey.Secret == "" {
		t.Fatal("registration did not return the one-time Codex control key")
	}
	initialSecret := registered.CodexControlKey.Secret
	initialID := registered.CodexControlKey.ID

	principal, metadata, err := authService.AuthenticateControlKey(ctx, initialSecret)
	if err != nil || principal.UserID != registered.User.ID || metadata.ID != initialID || metadata.Status != "active" {
		t.Fatalf("authenticate initial control key: userMatch=%t idMatch=%t status=%q err=%v",
			principal.UserID == registered.User.ID, metadata.ID == initialID, metadata.Status, err)
	}
	var storedHash string
	if err := pool.QueryRow(ctx, `SELECT secret_hash FROM user_control_keys WHERE id = $1`, initialID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored control key hash: %v", err)
	}
	if storedHash == "" || storedHash == initialSecret || strings.Contains(storedHash, initialSecret) {
		t.Fatal("database did not store the control key as a one-way hash")
	}

	handler := New(pool, authService, nil, nil, nil).Handler()
	keyPath := "/api/me/codex-control-key"
	diagnosticsPath := "/api/system/project-control-diagnostics"
	if _, err := pool.Exec(ctx, `UPDATE users SET is_system_admin = false WHERE id = $1`, registered.User.ID); err != nil {
		t.Fatalf("demote project control diagnostics member: %v", err)
	}
	assertAPIErrorCode(t, handler, http.MethodGet, diagnosticsPath, registered.AccessToken, registered.OrganizationID, nil,
		http.StatusForbidden, "SYSTEM_ADMINISTRATOR_REQUIRED")
	if _, err := pool.Exec(ctx, `UPDATE users SET is_system_admin = true WHERE id = $1`, registered.User.ID); err != nil {
		t.Fatalf("promote project control diagnostics administrator: %v", err)
	}
	var diagnostics projectControlDiagnosticsResponse
	doAPISuccess(t, handler, http.MethodGet, diagnosticsPath, registered.AccessToken, registered.OrganizationID, nil, &diagnostics)
	if !diagnostics.MCP.Enabled || len(diagnostics.MCP.ToolCatalogHash) != 64 ||
		len(diagnostics.ActionMatrixHash) != 64 || diagnostics.ReleaseID == "" {
		t.Fatalf("project control diagnostics = %+v", diagnostics)
	}
	var status controlKeyStatusResponse
	doAPISuccess(t, handler, http.MethodGet, keyPath, registered.AccessToken, registered.OrganizationID, nil, &status)
	if status.Key == nil || status.Key.ID != initialID || status.RequiresSetup || status.Key.Prefix == "" {
		t.Fatalf("initial control key status: keyPresent=%t idMatch=%t requiresSetup=%t",
			status.Key != nil, status.Key != nil && status.Key.ID == initialID, status.RequiresSetup)
	}
	statusResponse := doAPIRequest(t, handler, http.MethodGet, keyPath, registered.AccessToken, registered.OrganizationID, nil)
	if strings.Contains(statusResponse.Body.String(), initialSecret) {
		t.Fatal("control key status response exposed the plaintext secret")
	}
	assertAPIErrorCode(t, handler, http.MethodPost, keyPath, registered.AccessToken, registered.OrganizationID, nil,
		http.StatusConflict, "CODEX_CONTROL_KEY_EXISTS")

	var rotated controlKeySecretResponse
	doAPISuccess(t, handler, http.MethodPost, keyPath+"/rotate", registered.AccessToken, registered.OrganizationID, nil, &rotated)
	if rotated.CodexControlKey.Secret == "" || rotated.CodexControlKey.ID == initialID || rotated.CodexControlKey.Secret == initialSecret {
		t.Fatal("control key rotation did not return a distinct one-time key")
	}
	if _, _, err := authService.AuthenticateControlKey(ctx, initialSecret); !errors.Is(err, auth.ErrControlKeyInvalid) {
		t.Fatalf("old control key after rotation error = %v, want ErrControlKeyInvalid", err)
	}
	if _, _, err := authService.AuthenticateControlKey(ctx, rotated.CodexControlKey.Secret); err != nil {
		t.Fatalf("authenticate rotated control key: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET credential_version = credential_version + 1 WHERE id = $1`, registered.User.ID); err != nil {
		t.Fatalf("advance user credential version: %v", err)
	}
	if _, _, err := authService.AuthenticateControlKey(ctx, rotated.CodexControlKey.Secret); !errors.Is(err, auth.ErrControlKeyInvalid) {
		t.Fatalf("credential-version-stale control key error = %v, want ErrControlKeyInvalid", err)
	}
	login, err := authService.Login(ctx, auth.LoginRequest{
		Identifier: registered.User.Username,
		Password:   "Password123!",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if err != nil || login.TokenResponse == nil {
		t.Fatalf("login after credential version change: token=%t err=%v", login.TokenResponse != nil, err)
	}
	if login.CodexControlKey != nil {
		t.Fatal("ordinary login unexpectedly returned a plaintext Codex control key")
	}
	doAPISuccess(t, handler, http.MethodGet, keyPath, login.AccessToken, login.OrganizationID, nil, &status)
	if status.Key == nil || status.Key.Status != "requires_rotation" || !status.Key.CanRotate || status.Key.CanRevoke {
		t.Fatalf("credential-version-stale key status = %+v", status.Key)
	}

	var replacement controlKeySecretResponse
	doAPISuccess(t, handler, http.MethodPost, keyPath+"/rotate", login.AccessToken, login.OrganizationID, nil, &replacement)
	if _, _, err := authService.AuthenticateControlKey(ctx, replacement.CodexControlKey.Secret); err != nil {
		t.Fatalf("authenticate replacement control key: %v", err)
	}
	revoked := doAPIRequest(t, handler, http.MethodDelete, keyPath, login.AccessToken, login.OrganizationID, nil)
	if revoked.Code != http.StatusNoContent || revoked.Body.Len() != 0 {
		t.Fatalf("revoke response status=%d bodyBytes=%d, want 204 and empty body", revoked.Code, revoked.Body.Len())
	}
	if _, _, err := authService.AuthenticateControlKey(ctx, replacement.CodexControlKey.Secret); !errors.Is(err, auth.ErrControlKeyInvalid) {
		t.Fatalf("revoked control key error = %v, want ErrControlKeyInvalid", err)
	}
	doAPISuccess(t, handler, http.MethodGet, keyPath, login.AccessToken, login.OrganizationID, nil, &status)
	if status.Key == nil || status.Key.Status != "revoked" || status.Key.CanRotate || status.Key.CanRevoke {
		t.Fatalf("revoked key status = %+v", status.Key)
	}

	var recreated controlKeySecretResponse
	doAPISuccess(t, handler, http.MethodPost, keyPath, login.AccessToken, login.OrganizationID, nil, &recreated)
	if recreated.CodexControlKey.ID == replacement.CodexControlKey.ID || recreated.CodexControlKey.Secret == "" {
		t.Fatal("recreating after revoke did not create a distinct active key")
	}
	if _, _, err := authService.AuthenticateControlKey(ctx, recreated.CodexControlKey.Secret); err != nil {
		t.Fatalf("authenticate recreated control key: %v", err)
	}
}
