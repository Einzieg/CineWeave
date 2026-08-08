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

func TestSystemAdministratorOrganizationManagement(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run system administration API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for system administration API tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	authService := auth.NewService(pool, "system-administration-test-secret", time.Hour, 24*time.Hour)
	register := func(prefix string) auth.TokenResponse {
		t.Helper()
		response, registerErr := authService.Register(ctx, auth.RegisterRequest{
			Email:            prefix + "-" + suffix + "@example.test",
			Username:         prefix + "-" + suffix,
			Password:         "Password123!",
			DisplayName:      prefix,
			OrganizationName: prefix + " personal " + suffix,
		}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
		if registerErr != nil {
			t.Fatalf("register %s: %v", prefix, registerErr)
		}
		return response
	}
	administrator := register("sysadmin")
	owner := register("new-owner")
	ordinary := register("ordinary")
	organizationIDs := []string{administrator.OrganizationID, owner.OrganizationID, ordinary.OrganizationID}
	userIDs := []string{administrator.User.ID, owner.User.ID, ordinary.User.ID}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = ANY($1::uuid[])`, organizationIDs)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
	})

	if _, err := pool.Exec(ctx, `UPDATE users SET is_system_admin = true WHERE id = $1`, administrator.User.ID); err != nil {
		t.Fatalf("promote system administrator: %v", err)
	}
	handler := New(pool, authService, nil, nil, nil).Handler()

	assertAPIErrorCode(t, handler, http.MethodGet, "/api/system/organizations", ordinary.AccessToken, ordinary.OrganizationID, nil, http.StatusForbidden, "SYSTEM_ADMINISTRATOR_REQUIRED")
	assertAPIErrorCode(t, handler, http.MethodPost, "/api/system/organizations", ordinary.AccessToken, ordinary.OrganizationID, map[string]any{
		"name": "Forbidden " + suffix, "ownerIdentifier": owner.User.Username,
	}, http.StatusForbidden, "SYSTEM_ADMINISTRATOR_REQUIRED")

	var created auth.CreatedSystemOrganization
	doAPISuccess(t, handler, http.MethodPost, "/api/system/organizations", administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"name":            "平台组织 " + suffix,
		"workspaceName":   "制作工作区",
		"ownerIdentifier": strings.ToUpper(owner.User.Username),
	}, &created)
	organizationIDs = append(organizationIDs, created.Organization.ID)
	if created.Organization.ID == "" || created.InitialOwner.ID != owner.User.ID || created.DefaultWorkspaceID == "" {
		t.Fatalf("created system organization = %+v", created)
	}
	if created.Organization.ActiveMemberCount != 1 || created.Organization.WorkspaceCount != 1 || created.Organization.ProjectCount != 0 || created.Organization.OwnerCount != 1 {
		t.Fatalf("created organization counts = %+v", created.Organization)
	}

	var ownerBindingCount, administratorMembershipCount, auditCount int
	var workspaceName string
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		WHERE rb.organization_id = $1
		  AND rb.subject_user_id = $2
		  AND rb.created_by = $3
		  AND rb.resource_organization_id = $1
		  AND r.role_key IN ('org_owner', 'organization_owner')
	`, created.Organization.ID, owner.User.ID, administrator.User.ID).Scan(&ownerBindingCount); err != nil {
		t.Fatalf("count initial owner binding: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM organization_members WHERE organization_id = $1 AND user_id = $2`, created.Organization.ID, administrator.User.ID).Scan(&administratorMembershipCount); err != nil {
		t.Fatalf("count administrator membership: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT name FROM workspaces WHERE id = $1`, created.DefaultWorkspaceID).Scan(&workspaceName); err != nil {
		t.Fatalf("read default workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND actor_user_id = $2 AND action = 'system.organization.created'`, created.Organization.ID, administrator.User.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count system organization audit: %v", err)
	}
	if ownerBindingCount != 1 || administratorMembershipCount != 0 || workspaceName != "制作工作区" || auditCount != 1 {
		t.Fatalf("created organization persistence mismatch: ownerBinding=%d adminMembership=%d workspace=%q audits=%d", ownerBindingCount, administratorMembershipCount, workspaceName, auditCount)
	}

	var listed auth.SystemOrganizationList
	doAPISuccess(t, handler, http.MethodGet, "/api/system/organizations?search="+suffix+"&page=1&pageSize=10", administrator.AccessToken, administrator.OrganizationID, nil, &listed)
	if listed.Total < 1 || len(listed.Items) < 1 {
		t.Fatalf("system organization search = %+v", listed)
	}
	found := false
	for _, item := range listed.Items {
		if item.ID == created.Organization.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created organization %s missing from %+v", created.Organization.ID, listed.Items)
	}

	systemMembersPath := "/api/system/organizations/" + created.Organization.ID + "/members"
	assertAPIErrorCode(t, handler, http.MethodGet, systemMembersPath, ordinary.AccessToken, ordinary.OrganizationID, nil, http.StatusForbidden, "SYSTEM_ADMINISTRATOR_REQUIRED")
	assertAPIErrorCode(t, handler, http.MethodPost, systemMembersPath, ordinary.AccessToken, ordinary.OrganizationID, map[string]any{
		"email": "forbidden-" + suffix + "@example.test", "username": "forbidden-" + suffix, "password": "Password123!",
	}, http.StatusForbidden, "SYSTEM_ADMINISTRATOR_REQUIRED")

	var directMember auth.OrganizationMember
	directEmail := "direct-" + suffix + "@example.test"
	directUsername := "direct-" + suffix
	doAPISuccess(t, handler, http.MethodPost, systemMembersPath, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"email":       strings.ToUpper(directEmail),
		"username":    directUsername,
		"password":    "Password123!",
		"displayName": "直接成员",
	}, &directMember)
	userIDs = append(userIDs, directMember.User.ID)
	if directMember.OrganizationID != created.Organization.ID || directMember.Status != "active" ||
		directMember.User.Email != directEmail || directMember.User.Username != directUsername {
		t.Fatalf("direct member = %+v", directMember)
	}
	if directMember.CodexControlKey == nil || directMember.CodexControlKey.Secret == "" {
		t.Fatal("new system-created account did not return its one-time Codex control key")
	}
	if controlPrincipal, _, controlErr := authService.AuthenticateControlKey(ctx, directMember.CodexControlKey.Secret); controlErr != nil || controlPrincipal.UserID != directMember.User.ID {
		t.Fatalf("authenticate system-created member control key: userMatch=%t err=%v", controlPrincipal.UserID == directMember.User.ID, controlErr)
	}

	var directRoleCount, directCreateAuditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		WHERE rb.organization_id = $1 AND rb.subject_user_id = $2
		  AND rb.resource_organization_id = $1
		  AND r.role_key IN ('org_member', 'organization_member')
	`, created.Organization.ID, directMember.User.ID).Scan(&directRoleCount); err != nil {
		t.Fatalf("count direct member role: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_logs
		WHERE organization_id = $1 AND actor_user_id = $2
		  AND resource_id = $3 AND action = 'system.organization.member.created'
	`, created.Organization.ID, administrator.User.ID, directMember.User.ID).Scan(&directCreateAuditCount); err != nil {
		t.Fatalf("count direct member audit: %v", err)
	}
	if directRoleCount != 1 || directCreateAuditCount != 1 {
		t.Fatalf("direct member persistence mismatch: roles=%d audits=%d", directRoleCount, directCreateAuditCount)
	}

	var systemMembers auth.MemberList
	doAPISuccess(t, handler, http.MethodGet, systemMembersPath+"?search="+directUsername, administrator.AccessToken, administrator.OrganizationID, nil, &systemMembers)
	if systemMembers.Total != 1 || len(systemMembers.Items) != 1 || systemMembers.Items[0].User.ID != directMember.User.ID {
		t.Fatalf("system member search = %+v", systemMembers)
	}

	updatedEmail := "updated-" + suffix + "@example.test"
	updatedUsername := "updated-" + suffix
	var updatedMember auth.OrganizationMember
	doAPISuccess(t, handler, http.MethodPatch, systemMembersPath+"/"+directMember.User.ID, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"email":       strings.ToUpper(updatedEmail),
		"username":    updatedUsername,
		"password":    "NewPassword456!",
		"displayName": "更新成员",
		"avatarUrl":   "https://example.test/member.png",
	}, &updatedMember)
	if updatedMember.User.Email != updatedEmail || updatedMember.User.Username != updatedUsername ||
		updatedMember.User.DisplayName != "更新成员" || updatedMember.User.AvatarURL != "https://example.test/member.png" {
		t.Fatalf("updated system member = %+v", updatedMember)
	}
	if _, err := authService.Login(ctx, auth.LoginRequest{Identifier: directUsername, Password: "Password123!"}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("old member credentials error = %v, want ErrInvalidCredentials", err)
	}
	updatedLogin, err := authService.Login(ctx, auth.LoginRequest{Identifier: updatedUsername, Password: "NewPassword456!"}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if err != nil || updatedLogin.TokenResponse == nil || updatedLogin.OrganizationID != created.Organization.ID {
		t.Fatalf("updated member login = %+v, %v", updatedLogin, err)
	}

	doAPISuccess(t, handler, http.MethodPatch, systemMembersPath+"/"+directMember.User.ID, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"status": "disabled",
	}, &updatedMember)
	if updatedMember.Status != "disabled" {
		t.Fatalf("disabled system member = %+v", updatedMember)
	}
	doAPISuccess(t, handler, http.MethodPatch, systemMembersPath+"/"+directMember.User.ID, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"status": "active",
	}, &updatedMember)
	if updatedMember.Status != "active" {
		t.Fatalf("restored system member = %+v", updatedMember)
	}

	var attachedMember auth.OrganizationMember
	doAPISuccess(t, handler, http.MethodPost, systemMembersPath, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"existingUserIdentifier": strings.ToUpper(ordinary.User.Username),
	}, &attachedMember)
	if attachedMember.User.ID != ordinary.User.ID || attachedMember.OrganizationID != created.Organization.ID || attachedMember.Status != "active" {
		t.Fatalf("attached existing member = %+v", attachedMember)
	}
	if attachedMember.CodexControlKey != nil {
		t.Fatal("attaching an existing account unexpectedly returned or rotated a Codex control key")
	}
	assertAPIErrorCode(t, handler, http.MethodPost, systemMembersPath, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"existingUserIdentifier": ordinary.User.Email,
	}, http.StatusConflict, "SYSTEM_MEMBER_CONFLICT")
	if err := authService.RemoveOrganizationMember(ctx, created.Organization.ID, ordinary.User.ID, owner.User.ID); err != nil {
		t.Fatalf("remove attached member before direct restore: %v", err)
	}
	doAPISuccess(t, handler, http.MethodPost, systemMembersPath, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"existingUserIdentifier": ordinary.User.Email,
	}, &attachedMember)
	if attachedMember.Status != "active" {
		t.Fatalf("directly restored member = %+v", attachedMember)
	}

	var protectedMember auth.OrganizationMember
	doAPISuccess(t, handler, http.MethodPost, systemMembersPath, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"existingUserIdentifier": administrator.User.Username,
	}, &protectedMember)
	if !protectedMember.User.SystemAdministrator {
		t.Fatalf("attached system administrator = %+v", protectedMember)
	}
	assertAPIErrorCode(t, handler, http.MethodPatch, systemMembersPath+"/"+administrator.User.ID, administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"displayName": "不允许修改",
	}, http.StatusForbidden, "MEMBER_ACCOUNT_PROTECTED")

	var directUpdateAuditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_logs
		WHERE organization_id = $1 AND actor_user_id = $2
		  AND resource_id = $3 AND action = 'system.organization.member.updated'
	`, created.Organization.ID, administrator.User.ID, directMember.User.ID).Scan(&directUpdateAuditCount); err != nil {
		t.Fatalf("count direct update audits: %v", err)
	}
	if directUpdateAuditCount != 3 {
		t.Fatalf("direct update audit count = %d, want 3", directUpdateAuditCount)
	}

	assertAPIErrorCode(t, handler, http.MethodPost, "/api/system/organizations", administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"name": "Missing Owner", "ownerIdentifier": "missing-" + suffix,
	}, http.StatusNotFound, "SYSTEM_OWNER_NOT_FOUND")
	assertAPIErrorCode(t, handler, http.MethodPost, "/api/system/organizations", administrator.AccessToken, administrator.OrganizationID, map[string]any{
		"name": "", "ownerIdentifier": owner.User.Email,
	}, http.StatusUnprocessableEntity, "SYSTEM_ORGANIZATION_VALIDATION_FAILED")

	legacyCreate := doAPIRequest(t, handler, http.MethodPost, "/api/organizations", administrator.AccessToken, administrator.OrganizationID, map[string]any{"name": "Legacy"})
	if legacyCreate.Code != http.StatusMethodNotAllowed {
		t.Fatalf("legacy organization create status = %d, want %d; body=%s", legacyCreate.Code, http.StatusMethodNotAllowed, legacyCreate.Body.String())
	}

	login, err := authService.Login(ctx, auth.LoginRequest{Identifier: administrator.User.Username, Password: "Password123!"}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if err != nil || !login.RequiresOrganizationSelection || len(login.Organizations) != 2 {
		t.Fatalf("system administrator login = %+v, %v", login, err)
	}
	administratorUser, err := authService.Me(ctx, auth.Principal{UserID: administrator.User.ID})
	if err != nil || !administratorUser.SystemAdministrator {
		t.Fatalf("system administrator identity = %+v, %v", administratorUser, err)
	}
}
