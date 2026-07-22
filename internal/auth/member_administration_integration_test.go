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

func TestOrganizationMemberAccountManagement(t *testing.T) {
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

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	service := NewService(pool, "member-account-management-test-secret", time.Hour, 24*time.Hour)
	request := func(path string) *http.Request { return httptest.NewRequest(http.MethodPost, path, nil) }
	register := func(prefix, displayName string) TokenResponse {
		t.Helper()
		response, registerErr := service.Register(ctx, RegisterRequest{
			Email:            prefix + "-" + suffix + "@example.test",
			Username:         prefix + "-" + suffix,
			Password:         "Password123!",
			DisplayName:      displayName,
			OrganizationName: prefix + " org " + suffix,
		}, request("/api/auth/register"))
		if registerErr != nil {
			t.Fatalf("register %s: %v", prefix, registerErr)
		}
		return response
	}

	owner := register("ma-owner", "Account Owner")
	member := register("ma-member", "Account Member")
	admin := register("ma-admin", "Account Admin")
	memberPersonalOrganizationID := member.OrganizationID
	adminPersonalOrganizationID := admin.OrganizationID
	var extraOrganizationID string
	t.Cleanup(func() {
		organizationIDs := []string{owner.OrganizationID, memberPersonalOrganizationID, adminPersonalOrganizationID}
		if extraOrganizationID != "" {
			organizationIDs = append(organizationIDs, extraOrganizationID)
		}
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1::uuid[])`, organizationIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, []string{owner.User.ID, member.User.ID, admin.User.ID})
	})

	if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1, $2)`, memberPersonalOrganizationID, adminPersonalOrganizationID); err != nil {
		t.Fatalf("delete personal organizations: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_system_admin = true WHERE id = $1`, admin.User.ID); err != nil {
		t.Fatalf("promote protected system administrator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO organization_members(organization_id, user_id, status)
		VALUES ($1, $2, 'active'), ($1, $3, 'active')
	`, owner.OrganizationID, member.User.ID, admin.User.ID); err != nil {
		t.Fatalf("insert organization members: %v", err)
	}
	var memberRoleID, adminRoleID string
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id IS NULL AND role_key = 'org_member' LIMIT 1`).Scan(&memberRoleID); err != nil {
		t.Fatalf("select member role: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE organization_id IS NULL AND role_key = 'org_admin' LIMIT 1`).Scan(&adminRoleID); err != nil {
		t.Fatalf("select admin role: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_organization_id, created_by
		)
		VALUES
			($1, $2, 'user', $3, 'organization', $1, $4),
			($1, $5, 'user', $6, 'organization', $1, $4)
	`, owner.OrganizationID, memberRoleID, member.User.ID, owner.User.ID, adminRoleID, admin.User.ID); err != nil {
		t.Fatalf("insert role bindings: %v", err)
	}

	memberLogin, err := service.Login(ctx, LoginRequest{Identifier: member.User.Username, Password: "Password123!"}, request("/api/auth/login"))
	if err != nil {
		t.Fatalf("login member: %v", err)
	}
	if memberLogin.TokenResponse == nil {
		t.Fatal("member login did not create a session")
	}
	memberPrincipal, err := service.ParseBearer("Bearer " + memberLogin.AccessToken)
	if err != nil {
		t.Fatalf("parse member access token: %v", err)
	}
	if err := service.ValidatePrincipalActive(ctx, memberPrincipal); err != nil {
		t.Fatalf("validate member before reset: %v", err)
	}

	displayName := "Managed Member"
	avatarURL := "https://example.test/member.png"
	updated, err := service.UpdateOrganizationMemberProfile(ctx, owner.OrganizationID, member.User.ID, owner.User.ID, UpdateProfileRequest{
		DisplayName: &displayName,
		AvatarURL:   &avatarURL,
	})
	if err != nil {
		t.Fatalf("update member profile: %v", err)
	}
	if updated.User.DisplayName != displayName || updated.User.AvatarURL != avatarURL || !updated.AccountManagementAllowed {
		t.Fatalf("updated member = %+v", updated)
	}

	if _, err := service.IssueOrganizationMemberPasswordReset(ctx, owner.OrganizationID, owner.User.ID, admin.User.ID); !errors.Is(err, ErrMemberAccountProtected) {
		t.Fatalf("admin reset owner error = %v, want ErrMemberAccountProtected", err)
	}

	protectedMember, err := service.GetOrganizationMember(ctx, owner.OrganizationID, admin.User.ID)
	if err != nil {
		t.Fatalf("get protected system administrator: %v", err)
	}
	if protectedMember.AccountManagementAllowed {
		t.Fatal("system administrator must not be manageable through organization account operations")
	}
	protectedDisplayName := "Organization Managed System Administrator"
	if _, err := service.UpdateOrganizationMemberProfile(ctx, owner.OrganizationID, admin.User.ID, owner.User.ID, UpdateProfileRequest{DisplayName: &protectedDisplayName}); !errors.Is(err, ErrMemberAccountProtected) {
		t.Fatalf("system administrator profile error = %v, want ErrMemberAccountProtected", err)
	}
	if _, err := service.IssueOrganizationMemberPasswordReset(ctx, owner.OrganizationID, admin.User.ID, owner.User.ID); !errors.Is(err, ErrMemberAccountProtected) {
		t.Fatalf("system administrator reset error = %v, want ErrMemberAccountProtected", err)
	}
	if _, err := service.SetOrganizationMemberStatus(ctx, owner.OrganizationID, admin.User.ID, owner.User.ID, "disabled"); !errors.Is(err, ErrMemberAccountProtected) {
		t.Fatalf("system administrator disable error = %v, want ErrMemberAccountProtected", err)
	}
	if err := service.RemoveOrganizationMember(ctx, owner.OrganizationID, admin.User.ID, owner.User.ID); !errors.Is(err, ErrMemberAccountProtected) {
		t.Fatalf("system administrator remove error = %v, want ErrMemberAccountProtected", err)
	}
	if err := service.LeaveOrganization(ctx, owner.OrganizationID, admin.User.ID); !errors.Is(err, ErrMemberAccountProtected) {
		t.Fatalf("system administrator leave error = %v, want ErrMemberAccountProtected", err)
	}
	var protectedStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM organization_members WHERE organization_id = $1 AND user_id = $2
	`, owner.OrganizationID, admin.User.ID).Scan(&protectedStatus); err != nil {
		t.Fatalf("select protected membership status: %v", err)
	}
	if protectedStatus != "active" {
		t.Fatalf("protected membership status = %q, want active", protectedStatus)
	}
	var protectedResetCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_password_reset_tokens WHERE user_id = $1`, admin.User.ID).Scan(&protectedResetCount); err != nil {
		t.Fatalf("count protected reset tokens: %v", err)
	}
	if protectedResetCount != 0 {
		t.Fatalf("protected reset token count = %d, want 0", protectedResetCount)
	}

	reset, err := service.IssueOrganizationMemberPasswordReset(ctx, owner.OrganizationID, member.User.ID, owner.User.ID)
	if err != nil {
		t.Fatalf("issue password reset: %v", err)
	}
	if reset.ResetToken == "" || !strings.HasPrefix(reset.ResetToken, "pwr_") || !reset.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("password reset = %+v", reset)
	}
	var storedHash string
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM auth_password_reset_tokens WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, member.User.ID).Scan(&storedHash); err != nil {
		t.Fatalf("select password reset token hash: %v", err)
	}
	if storedHash == reset.ResetToken || storedHash != hashRefreshToken(reset.ResetToken) {
		t.Fatalf("stored password reset token is not a one-way hash")
	}
	if err := service.ValidatePrincipalActive(ctx, memberPrincipal); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old access token validation error = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: memberLogin.RefreshToken}, request("/api/auth/refresh")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old refresh token error = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Login(ctx, LoginRequest{Identifier: member.User.Username, Password: "Password123!"}, request("/api/auth/login")); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password login error = %v, want ErrInvalidCredentials", err)
	}
	if err := service.CompletePasswordReset(ctx, CompletePasswordResetRequest{ResetToken: reset.ResetToken, Password: "short"}, request("/api/auth/password-reset/complete")); !errors.Is(err, ErrPasswordResetValidation) {
		t.Fatalf("short password error = %v, want ErrPasswordResetValidation", err)
	}
	if err := service.CompletePasswordReset(ctx, CompletePasswordResetRequest{ResetToken: reset.ResetToken, Password: "NewPassword456!"}, request("/api/auth/password-reset/complete")); err != nil {
		t.Fatalf("complete password reset: %v", err)
	}
	if err := service.CompletePasswordReset(ctx, CompletePasswordResetRequest{ResetToken: reset.ResetToken, Password: "AnotherPassword789!"}, request("/api/auth/password-reset/complete")); !errors.Is(err, ErrPasswordResetInvalid) {
		t.Fatalf("password reset replay error = %v, want ErrPasswordResetInvalid", err)
	}
	if _, err := service.Login(ctx, LoginRequest{Identifier: member.User.Username, Password: "NewPassword456!"}, request("/api/auth/login")); err != nil {
		t.Fatalf("login with new password: %v", err)
	}

	extraOrganizationID = createTestOrganizationForUser(t, ctx, service, member.User.ID, "Shared Account Org "+suffix)
	sharedMember, err := service.GetOrganizationMember(ctx, owner.OrganizationID, member.User.ID)
	if err != nil {
		t.Fatalf("get shared member: %v", err)
	}
	if sharedMember.AccountManagementAllowed {
		t.Fatal("multi-organization member should not allow account management")
	}
	if _, err := service.UpdateOrganizationMemberProfile(ctx, owner.OrganizationID, member.User.ID, owner.User.ID, UpdateProfileRequest{DisplayName: &displayName}); !errors.Is(err, ErrSharedAccountManagement) {
		t.Fatalf("shared member profile error = %v, want ErrSharedAccountManagement", err)
	}
	if _, err := service.IssueOrganizationMemberPasswordReset(ctx, owner.OrganizationID, member.User.ID, owner.User.ID); !errors.Is(err, ErrSharedAccountManagement) {
		t.Fatalf("shared member password reset error = %v, want ErrSharedAccountManagement", err)
	}

	for _, action := range []string{
		"organization.member.profile_updated",
		"organization.member.password_reset_requested",
		"organization.member.password_reset_completed",
	} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND action = $2`, owner.OrganizationID, action).Scan(&count); err != nil {
			t.Fatalf("count audit action %s: %v", action, err)
		}
		if count != 1 {
			t.Fatalf("audit action %s count = %d, want 1", action, count)
		}
	}
}
