package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/google/uuid"
)

func TestUsernameLoginOrganizationSelectionAndSwitch(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run auth integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for auth integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	service := NewService(pool, "organization-selection-test-secret", time.Hour, 24*time.Hour)
	request := func(path string) *http.Request { return httptest.NewRequest(http.MethodPost, path, nil) }
	registered, err := service.Register(ctx, RegisterRequest{
		Email:            "identity-" + suffix + "@example.test",
		Username:         "identity-" + suffix,
		Password:         "Password123!",
		DisplayName:      "Identity Test",
		OrganizationName: "Identity Org One " + suffix,
	}, request("/api/auth/register"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registered.User.ID)
	})
	secondOrganizationID := createTestOrganizationForUser(t, ctx, service, registered.User.ID, "Identity Org Two "+suffix)

	login, err := service.Login(ctx, LoginRequest{Identifier: strings.ToUpper(registered.User.Username), Password: "Password123!"}, request("/api/auth/login"))
	if err != nil {
		t.Fatalf("username login: %v", err)
	}
	if !login.RequiresOrganizationSelection || login.TokenResponse != nil || len(login.Organizations) != 2 || login.OrganizationSelectionToken == "" {
		t.Fatalf("multi-organization login = %+v", login)
	}
	selected, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: login.OrganizationSelectionToken,
		OrganizationID:             secondOrganizationID,
	}, request("/api/auth/select-organization"))
	if err != nil {
		t.Fatalf("select organization: %v", err)
	}
	if selected.OrganizationID != secondOrganizationID || selected.AccessToken == "" || selected.RefreshToken == "" {
		t.Fatalf("selected session = %+v", selected)
	}
	if _, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: login.OrganizationSelectionToken,
		OrganizationID:             secondOrganizationID,
	}, request("/api/auth/select-organization")); !errors.Is(err, ErrOrganizationSelection) {
		t.Fatalf("selection token replay error = %v", err)
	}

	principal, err := service.ParseBearer("Bearer " + selected.AccessToken)
	if err != nil {
		t.Fatalf("parse selected access token: %v", err)
	}
	switched, err := service.SwitchOrganization(ctx, principal, SwitchOrganizationRequest{
		RefreshToken:   selected.RefreshToken,
		OrganizationID: registered.OrganizationID,
	}, request("/api/auth/switch-organization"))
	if err != nil {
		t.Fatalf("switch organization: %v", err)
	}
	if switched.OrganizationID != registered.OrganizationID || switched.RefreshToken == selected.RefreshToken {
		t.Fatalf("switched session = %+v", switched)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: selected.RefreshToken}, request("/api/auth/refresh")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old refresh token error = %v", err)
	}

	emailLogin, err := service.Login(ctx, LoginRequest{Identifier: strings.ToUpper(registered.User.Email), Password: "Password123!"}, request("/api/auth/login"))
	if err != nil || !emailLogin.RequiresOrganizationSelection {
		t.Fatalf("email login = %+v, %v", emailLogin, err)
	}

	concurrentLogin, err := service.Login(ctx, LoginRequest{Identifier: registered.User.Username, Password: "Password123!"}, request("/api/auth/login"))
	if err != nil {
		t.Fatalf("login for concurrent switch: %v", err)
	}
	concurrentSession, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: concurrentLogin.OrganizationSelectionToken,
		OrganizationID:             registered.OrganizationID,
	}, request("/api/auth/select-organization"))
	if err != nil {
		t.Fatalf("select source organization for concurrent switch: %v", err)
	}
	concurrentPrincipal, err := service.ParseBearer("Bearer " + concurrentSession.AccessToken)
	if err != nil {
		t.Fatalf("parse concurrent switch principal: %v", err)
	}
	switchResults := make(chan error, 2)
	for range 2 {
		go func() {
			_, switchErr := service.SwitchOrganization(ctx, concurrentPrincipal, SwitchOrganizationRequest{
				RefreshToken: concurrentSession.RefreshToken, OrganizationID: secondOrganizationID,
			}, request("/api/auth/switch-organization"))
			switchResults <- switchErr
		}()
	}
	switchSuccesses := 0
	switchRejections := 0
	for range 2 {
		switchErr := <-switchResults
		if switchErr == nil {
			switchSuccesses++
		} else if errors.Is(switchErr, ErrUnauthorized) {
			switchRejections++
		} else {
			t.Fatalf("concurrent switch error = %v", switchErr)
		}
	}
	if switchSuccesses != 1 || switchRejections != 1 {
		t.Fatalf("concurrent switch results: success=%d rejected=%d", switchSuccesses, switchRejections)
	}

	expiringLogin, err := service.Login(ctx, LoginRequest{Identifier: registered.User.Username, Password: "Password123!"}, request("/api/auth/login"))
	if err != nil {
		t.Fatalf("login for expiring selection token: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE auth_organization_selection_nonces
		SET expires_at = now() - interval '1 second'
		WHERE user_id = $1 AND consumed_at IS NULL
	`, registered.User.ID); err != nil {
		t.Fatalf("expire organization selection nonce: %v", err)
	}
	if _, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: expiringLogin.OrganizationSelectionToken,
		OrganizationID:             secondOrganizationID,
	}, request("/api/auth/select-organization")); !errors.Is(err, ErrOrganizationSelection) {
		t.Fatalf("expired organization selection error = %v", err)
	}
	if _, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: registered.AccessToken,
		OrganizationID:             registered.OrganizationID,
	}, request("/api/auth/select-organization")); !errors.Is(err, ErrOrganizationSelection) {
		t.Fatalf("access token accepted as organization selection token: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET username = NULL, username_normalized = NULL WHERE id = $1`, registered.User.ID); err != nil {
		t.Fatalf("convert account to legacy username state: %v", err)
	}
	legacyLogin, err := service.Login(ctx, LoginRequest{Identifier: registered.User.Email, Password: "Password123!"}, request("/api/auth/login"))
	if err != nil || !legacyLogin.RequiresOrganizationSelection {
		t.Fatalf("legacy email login = %+v, %v", legacyLogin, err)
	}
	legacySession, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: legacyLogin.OrganizationSelectionToken,
		OrganizationID:             registered.OrganizationID,
	}, request("/api/auth/select-organization"))
	if err != nil {
		t.Fatalf("select organization for legacy account: %v", err)
	}
	if legacySession.User.Username != "" {
		t.Fatalf("legacy account unexpectedly has username %q", legacySession.User.Username)
	}
	legacyPrincipal, err := service.ParseBearer("Bearer " + legacySession.AccessToken)
	if err != nil {
		t.Fatalf("parse legacy principal: %v", err)
	}
	legacyUsername := "legacy-" + suffix
	updatedLegacyUser, err := service.SetInitialUsername(ctx, legacyPrincipal, legacyUsername)
	if err != nil || updatedLegacyUser.Username != legacyUsername {
		t.Fatalf("set legacy username = %+v, %v", updatedLegacyUser, err)
	}
	if _, err := service.SetInitialUsername(ctx, legacyPrincipal, "again-"+suffix); !errors.Is(err, ErrForbidden) {
		t.Fatalf("second legacy username update error = %v", err)
	}
	legacyUsernameLogin, err := service.Login(ctx, LoginRequest{Identifier: strings.ToUpper(legacyUsername), Password: "Password123!"}, request("/api/auth/login"))
	if err != nil || !legacyUsernameLogin.RequiresOrganizationSelection {
		t.Fatalf("legacy account username login = %+v, %v", legacyUsernameLogin, err)
	}
	var usernameAuditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE organization_id = $1 AND actor_user_id = $2 AND action = 'user.username.set'
	`, registered.OrganizationID, registered.User.ID).Scan(&usernameAuditCount); err != nil {
		t.Fatalf("count username audit records: %v", err)
	}
	if usernameAuditCount != 1 {
		t.Fatalf("username audit count = %d, want 1", usernameAuditCount)
	}
}

func createTestOrganizationForUser(t *testing.T, ctx context.Context, service *Service, userID, name string) string {
	t.Helper()
	tx, err := service.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin test organization transaction: %v", err)
	}
	defer rollback(ctx, tx)
	organizationID, _, err := createOrganizationForUser(ctx, tx, userID, userID, name, "Default Workspace")
	if err != nil {
		t.Fatalf("create test organization: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit test organization: %v", err)
	}
	return organizationID
}

func TestAuthenticationFailuresAreGenericAndRateLimited(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run auth integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for auth integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	service := NewService(pool, "auth-security-test-secret-"+suffix, time.Hour, 24*time.Hour)
	request := func(path string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "198.51.100.44:49152"
		return req
	}
	registered, err := service.Register(ctx, RegisterRequest{
		Email:            "security-" + suffix + "@example.test",
		Username:         "security-" + suffix,
		Password:         "Password123!",
		DisplayName:      "Security Test",
		OrganizationName: "Security Org " + suffix,
	}, request("/api/auth/register"))
	if err != nil {
		t.Fatalf("register security test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registered.User.ID)
	})

	_, duplicateEmailErr := service.Register(ctx, RegisterRequest{
		Email: registered.User.Email, Username: "other-" + suffix, Password: "Password123!",
	}, request("/api/auth/register"))
	_, duplicateUsernameErr := service.Register(ctx, RegisterRequest{
		Email: "other-" + suffix + "@example.test", Username: registered.User.Username, Password: "Password123!",
	}, request("/api/auth/register"))
	if !errors.Is(duplicateEmailErr, ErrRegistrationUnavailable) || !errors.Is(duplicateUsernameErr, ErrRegistrationUnavailable) {
		t.Fatalf("registration conflicts were distinguishable: email=%v username=%v", duplicateEmailErr, duplicateUsernameErr)
	}

	unknownIdentifier := "unknown-" + suffix
	if _, err := service.Login(ctx, LoginRequest{Identifier: unknownIdentifier, Password: "wrong-password"}, request("/api/auth/login")); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown-account login error = %v", err)
	}
	for attempt := 0; attempt < securityRatePolicies[securityActionLogin].identityFailures; attempt++ {
		_, err := service.Login(ctx, LoginRequest{Identifier: registered.User.Username, Password: "wrong-password"}, request("/api/auth/login"))
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("wrong-password attempt %d error = %v", attempt+1, err)
		}
	}
	if _, err := service.Login(ctx, LoginRequest{Identifier: registered.User.Username, Password: "Password123!"}, request("/api/auth/login")); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate-limited login error = %v", err)
	}

	hashes := []string{
		service.securitySubjectHash(securityActionLogin, "identity", strings.ToLower(registered.User.Username)),
		service.securitySubjectHash(securityActionLogin, "identity", unknownIdentifier),
		service.securitySubjectHash(securityActionLogin, "client", "198.51.100.44"),
		service.securitySubjectHash(securityActionRegister, "identity", registrationRateSubject(registered.User.Email, "other-"+suffix)),
		service.securitySubjectHash(securityActionRegister, "identity", registrationRateSubject("other-"+suffix+"@example.test", registered.User.Username)),
		service.securitySubjectHash(securityActionRegister, "client", "198.51.100.44"),
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM auth_security_failures WHERE subject_hash = ANY($1::text[])`, hashes)
	})
	var invalidHashes int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM auth_security_failures
		WHERE subject_hash = ANY($1::text[])
		  AND (length(subject_hash) <> 64 OR subject_hash ~* $2)
	`, hashes, "security-"+suffix).Scan(&invalidHashes); err != nil {
		t.Fatalf("inspect persisted auth security hashes: %v", err)
	}
	if invalidHashes != 0 {
		t.Fatalf("auth security rows exposed sensitive identifiers: %d", invalidHashes)
	}
}
