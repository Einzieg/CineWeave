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
	t.Cleanup(pool.Close)

	authService := auth.NewService(pool, "rbac-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	providerService := provider.NewService(pool, vault)
	server := New(pool, authService, providerService, nil, nil).Handler()
	ensureRBACProviderConnector(t, ctx, pool)

	suffix := uuid.NewString()
	shortSuffix := strings.ReplaceAll(suffix, "-", "")[:12]
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "rbac-owner-" + suffix + "@example.test",
		Username:         "rbac-owner-" + shortSuffix,
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
		Username:         "rbac-member-" + shortSuffix,
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
	memberPrincipal, err := authService.ParseBearer("Bearer " + member.AccessToken)
	if err != nil {
		t.Fatalf("parse member access token: %v", err)
	}
	member, err = authService.SwitchOrganization(ctx, memberPrincipal, auth.SwitchOrganizationRequest{
		RefreshToken: member.RefreshToken, OrganizationID: owner.OrganizationID,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/switch-organization", nil))
	if err != nil {
		t.Fatalf("switch member organization: %v", err)
	}

	var listedRoles struct {
		Items []Role `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/roles", owner.AccessToken, owner.OrganizationID, nil, &listedRoles)
	roleKeys := make(map[string]bool, len(listedRoles.Items))
	for _, role := range listedRoles.Items {
		roleKeys[role.RoleKey] = true
	}
	for _, legacyRoleKey := range []string{"organization_owner", "organization_admin", "organization_member"} {
		if roleKeys[legacyRoleKey] {
			t.Fatalf("legacy role %q should not be listed", legacyRoleKey)
		}
	}
	for _, canonicalRoleKey := range []string{"org_owner", "org_admin", "org_member"} {
		if !roleKeys[canonicalRoleKey] {
			t.Fatalf("canonical role %q is missing", canonicalRoleKey)
		}
	}

	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, server, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID,
		"name":        "RBAC Project",
		"settings":    map[string]any{},
	}, &project)

	teamOnly := registerRBACOrgMember(t, ctx, pool, authService, owner.OrganizationID, suffix)
	var team Team
	doAPISuccess(t, server, http.MethodPost, "/api/teams", owner.AccessToken, owner.OrganizationID, map[string]any{
		"name": "Project Viewers", "description": "Read-only project access",
	}, &team)
	var teamMember TeamMember
	doAPISuccess(t, server, http.MethodPost, "/api/teams/"+team.ID+"/members", owner.AccessToken, owner.OrganizationID, map[string]any{
		"userId": teamOnly.User.ID,
	}, &teamMember)
	if teamMember.User.ID != teamOnly.User.ID || teamMember.User.Email != teamOnly.User.Email {
		t.Fatalf("team member user summary = %+v", teamMember.User)
	}
	projectViewerRoleID := roleIDByKey(t, ctx, pool, "project_viewer")
	var teamBinding RoleBinding
	doAPISuccess(t, server, http.MethodPost, "/api/role-bindings", owner.AccessToken, owner.OrganizationID, map[string]any{
		"organizationId":    owner.OrganizationID,
		"roleId":            projectViewerRoleID,
		"subjectType":       "team",
		"subjectTeamId":     team.ID,
		"resourceType":      "project",
		"resourceProjectId": project.ID,
	}, &teamBinding)
	if teamBinding.SubjectName != team.Name || teamBinding.ResourceName != project.Name || teamBinding.RoleName == "" {
		t.Fatalf("team role binding display summary = %+v", teamBinding)
	}
	var impact TeamImpact
	doAPISuccess(t, server, http.MethodGet, "/api/teams/"+team.ID+"/impact", owner.AccessToken, owner.OrganizationID, nil, &impact)
	if impact.ActiveMemberCount != 1 || impact.ActiveBindingCount != 1 {
		t.Fatalf("team impact = %+v", impact)
	}
	var teamMembers struct {
		Items []TeamMember `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/teams/"+team.ID+"/members", owner.AccessToken, owner.OrganizationID, nil, &teamMembers)
	if len(teamMembers.Items) != 1 || teamMembers.Items[0].User.Username == "" {
		t.Fatalf("team members = %+v", teamMembers.Items)
	}
	var roleDetail Role
	doAPISuccess(t, server, http.MethodGet, "/api/roles/"+projectViewerRoleID, owner.AccessToken, owner.OrganizationID, nil, &roleDetail)
	if len(roleDetail.Permissions) == 0 || roleDetail.BindingCount == 0 {
		t.Fatalf("role detail = %+v", roleDetail)
	}
	var filteredBindings struct {
		Items []RoleBinding `json:"items"`
		Page  int           `json:"page"`
		Total int           `json:"total"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/role-bindings?subjectType=team&subjectId="+team.ID+"&page=1&pageSize=1", owner.AccessToken, owner.OrganizationID, nil, &filteredBindings)
	if len(filteredBindings.Items) != 1 || filteredBindings.Items[0].ID != teamBinding.ID || filteredBindings.Page != 1 || filteredBindings.Total != 1 {
		t.Fatalf("filtered team bindings = %+v", filteredBindings.Items)
	}
	var teamReadable Project
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, nil, &teamReadable)
	var disabledTeam Team
	doAPISuccess(t, server, http.MethodPatch, "/api/teams/"+team.ID, owner.AccessToken, owner.OrganizationID, map[string]any{"status": "disabled"}, &disabledTeam)
	assertAPIErrorCode(t, server, http.MethodGet, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "ACCESS_DENIED")
	doAPISuccess(t, server, http.MethodPatch, "/api/teams/"+team.ID, owner.AccessToken, owner.OrganizationID, map[string]any{"status": "active"}, &team)
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, nil, &teamReadable)
	var deletedBinding map[string]bool
	doAPISuccess(t, server, http.MethodDelete, "/api/role-bindings/"+teamBinding.ID, owner.AccessToken, owner.OrganizationID, nil, &deletedBinding)
	assertAPIErrorCode(t, server, http.MethodGet, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "ACCESS_DENIED")
	var ownerBindingID string
	if err := pool.QueryRow(ctx, `
		SELECT rb.id FROM role_bindings rb
		JOIN roles role ON role.id = rb.role_id
		WHERE rb.organization_id = $1 AND rb.subject_user_id = $2
		  AND role.role_key IN ('org_owner', 'organization_owner')
		  AND rb.resource_type = 'organization'
		LIMIT 1
	`, owner.OrganizationID, owner.User.ID).Scan(&ownerBindingID); err != nil {
		t.Fatalf("select owner binding: %v", err)
	}
	assertAPIErrorCode(t, server, http.MethodDelete, "/api/role-bindings/"+ownerBindingID, owner.AccessToken, owner.OrganizationID, nil, http.StatusConflict, "LAST_OWNER_REQUIRED")

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

	assertAPIErrorCode(t, server, http.MethodGet, "/api/organizations/"+owner.OrganizationID+"/audit-logs", teamOnly.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "ACCESS_DENIED")
	var currentContext struct {
		User           auth.UserResponse       `json:"user"`
		OrganizationID string                  `json:"organizationId"`
		Membership     auth.OrganizationMember `json:"membership"`
		Permissions    []string                `json:"permissions"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/auth/me", owner.AccessToken, owner.OrganizationID, nil, &currentContext)
	if currentContext.OrganizationID != owner.OrganizationID || currentContext.Membership.User.ID != owner.User.ID || currentContext.Membership.Status != "active" {
		t.Fatalf("current organization context = %+v", currentContext)
	}
	hasMemberManage := false
	for _, permission := range currentContext.Permissions {
		if permission == "member.manage" {
			hasMemberManage = true
			break
		}
	}
	if !hasMemberManage {
		t.Fatalf("current context permissions = %v, want member.manage", currentContext.Permissions)
	}
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/auth/me", owner.AccessToken, owner.OrganizationID, map[string]any{
		"avatarUrl": "javascript:alert(1)",
	}, http.StatusUnprocessableEntity, "PROFILE_VALIDATION_FAILED")
	var profile auth.UserResponse
	doAPISuccess(t, server, http.MethodPatch, "/api/auth/me", owner.AccessToken, owner.OrganizationID, map[string]any{
		"displayName": "RBAC Owner Updated",
		"avatarUrl":   "https://example.test/avatar.png",
	}, &profile)
	if profile.DisplayName != "RBAC Owner Updated" || profile.AvatarURL != "https://example.test/avatar.png" {
		t.Fatalf("updated profile = %+v", profile)
	}
	var originalOrganization Organization
	doAPISuccess(t, server, http.MethodGet, "/api/organizations/"+owner.OrganizationID, owner.AccessToken, owner.OrganizationID, nil, &originalOrganization)
	var updatedOrganization Organization
	doAPISuccess(t, server, http.MethodPatch, "/api/organizations/"+owner.OrganizationID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"name": "RBAC Organization Updated",
	}, &updatedOrganization)
	if updatedOrganization.Name != "RBAC Organization Updated" || updatedOrganization.Slug != originalOrganization.Slug {
		t.Fatalf("updated organization = %+v, original slug = %s", updatedOrganization, originalOrganization.Slug)
	}
	assertAPIErrorCode(t, server, http.MethodPost, "/api/roles", owner.AccessToken, owner.OrganizationID, map[string]any{
		"roleKey": "invalid_project_provider", "name": "Invalid Project Provider", "scope": "project", "permissionKeys": []string{"provider.read"},
	}, http.StatusUnprocessableEntity, "ROLE_PERMISSION_NOT_ALLOWED")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/roles", owner.AccessToken, owner.OrganizationID, map[string]any{
		"roleKey": "invalid_org_admin", "name": "Invalid Wildcard", "scope": "organization", "permissionKeys": []string{"admin.manage"},
	}, http.StatusUnprocessableEntity, "ROLE_PERMISSION_NOT_ALLOWED")
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/roles/"+projectViewerRoleID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"name": "Cannot Rename System Role",
	}, http.StatusConflict, "SYSTEM_ROLE_IMMUTABLE")
	assertAPIErrorCode(t, server, http.MethodDelete, "/api/roles/"+projectViewerRoleID, owner.AccessToken, owner.OrganizationID, nil, http.StatusConflict, "SYSTEM_ROLE_IMMUTABLE")
	var customRole Role
	doAPISuccess(t, server, http.MethodPost, "/api/roles", owner.AccessToken, owner.OrganizationID, map[string]any{
		"roleKey": "custom_project_editor", "name": "自定义项目编辑", "description": "项目读取与编辑", "scope": "project",
		"permissionKeys": []string{"project.read", "project.write"},
	}, &customRole)
	if customRole.IsSystem || customRole.OrganizationID == nil || *customRole.OrganizationID != owner.OrganizationID || len(customRole.Permissions) != 2 {
		t.Fatalf("created custom role = %+v", customRole)
	}
	assertAPIErrorCode(t, server, http.MethodPost, "/api/roles", owner.AccessToken, owner.OrganizationID, map[string]any{
		"roleKey": "custom_project_editor", "name": "Duplicate Across Scope", "scope": "workspace", "permissionKeys": []string{"workspace.read"},
	}, http.StatusConflict, "ROLE_KEY_EXISTS")
	var customBinding RoleBinding
	doAPISuccess(t, server, http.MethodPost, "/api/role-bindings", owner.AccessToken, owner.OrganizationID, map[string]any{
		"organizationId": owner.OrganizationID, "roleId": customRole.ID, "subjectType": "user", "subjectUserId": teamOnly.User.ID,
		"resourceType": "project", "resourceProjectId": project.ID,
	}, &customBinding)
	var customImpact RoleImpact
	doAPISuccess(t, server, http.MethodGet, "/api/roles/"+customRole.ID+"/impact", owner.AccessToken, owner.OrganizationID, nil, &customImpact)
	if customImpact.BindingCount != 1 || customImpact.DirectUserCount != 1 || customImpact.AffectedUserCount != 1 || customImpact.ProjectBindings != 1 {
		t.Fatalf("custom role impact = %+v", customImpact)
	}
	var customReadable Project
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, nil, &customReadable)
	var customUpdated Project
	doAPISuccess(t, server, http.MethodPatch, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, map[string]any{"name": "Custom Role Edited Project"}, &customUpdated)
	var reducedCustomRole Role
	doAPISuccess(t, server, http.MethodPatch, "/api/roles/"+customRole.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"permissionKeys": []string{"project.read"},
	}, &reducedCustomRole)
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/projects/"+project.ID, teamOnly.AccessToken, owner.OrganizationID, map[string]any{"name": "Denied After Permission Removal"}, http.StatusForbidden, "ACCESS_DENIED")
	assertAPIErrorCode(t, server, http.MethodPatch, "/api/roles/"+customRole.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"scope": "workspace", "permissionKeys": []string{"workspace.read"},
	}, http.StatusConflict, "ROLE_SCOPE_IN_USE")
	assertAPIErrorCode(t, server, http.MethodDelete, "/api/roles/"+customRole.ID, owner.AccessToken, owner.OrganizationID, nil, http.StatusConflict, "ROLE_IN_USE")
	var deletedCustomBinding map[string]bool
	doAPISuccess(t, server, http.MethodDelete, "/api/role-bindings/"+customBinding.ID, owner.AccessToken, owner.OrganizationID, nil, &deletedCustomBinding)
	var deletedCustomRole map[string]bool
	doAPISuccess(t, server, http.MethodDelete, "/api/roles/"+customRole.ID, owner.AccessToken, owner.OrganizationID, nil, &deletedCustomRole)
	assertAPIErrorCode(t, server, http.MethodGet, "/api/roles/"+customRole.ID, owner.AccessToken, owner.OrganizationID, nil, http.StatusNotFound, "NOT_FOUND")
	var roleManager Role
	doAPISuccess(t, server, http.MethodPost, "/api/roles", owner.AccessToken, owner.OrganizationID, map[string]any{
		"roleKey": "custom_role_manager", "name": "自定义角色管理员", "scope": "organization", "permissionKeys": []string{"member.manage", "role.manage"},
	}, &roleManager)
	var roleManagerBinding RoleBinding
	doAPISuccess(t, server, http.MethodPost, "/api/role-bindings", owner.AccessToken, owner.OrganizationID, map[string]any{
		"organizationId": owner.OrganizationID, "roleId": roleManager.ID, "subjectType": "user", "subjectUserId": teamOnly.User.ID,
		"resourceType": "organization", "resourceOrganizationId": owner.OrganizationID,
	}, &roleManagerBinding)
	assertAPIErrorCode(t, server, http.MethodPost, "/api/roles", teamOnly.AccessToken, owner.OrganizationID, map[string]any{
		"roleKey": "privilege_escalation_attempt", "name": "提权尝试", "scope": "project", "permissionKeys": []string{"project.write"},
	}, http.StatusForbidden, "ACCESS_DENIED")
	orgOwnerRoleID := roleIDByKey(t, context.Background(), pool, "org_owner")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/role-bindings", teamOnly.AccessToken, owner.OrganizationID, map[string]any{
		"organizationId": owner.OrganizationID, "roleId": orgOwnerRoleID, "subjectType": "user", "subjectUserId": teamOnly.User.ID,
		"resourceType": "organization", "resourceOrganizationId": owner.OrganizationID,
	}, http.StatusForbidden, "ACCESS_DENIED")
	baseMemberRoleID := roleIDByKey(t, context.Background(), pool, "org_member")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/organizations/"+owner.OrganizationID+"/invitations", teamOnly.AccessToken, owner.OrganizationID, map[string]any{
		"email": "blocked-escalation-" + uuid.NewString() + "@example.test", "baseRoleId": baseMemberRoleID, "expiresInDays": 7,
		"bindings": []map[string]any{{"roleId": orgOwnerRoleID, "resourceType": "organization", "organizationId": owner.OrganizationID}},
	}, http.StatusForbidden, "ACCESS_DENIED")
	var deletedRoleManagerBinding map[string]bool
	doAPISuccess(t, server, http.MethodDelete, "/api/role-bindings/"+roleManagerBinding.ID, owner.AccessToken, owner.OrganizationID, nil, &deletedRoleManagerBinding)
	var deletedRoleManager map[string]bool
	doAPISuccess(t, server, http.MethodDelete, "/api/roles/"+roleManager.ID, owner.AccessToken, owner.OrganizationID, nil, &deletedRoleManager)
	assertAPIErrorCode(t, server, http.MethodPost, "/api/organizations/"+owner.OrganizationID+"/leave", owner.AccessToken, owner.OrganizationID, nil, http.StatusConflict, "LAST_OWNER_REQUIRED")
	var left map[string]bool
	doAPISuccess(t, server, http.MethodPost, "/api/organizations/"+owner.OrganizationID+"/leave", teamOnly.AccessToken, owner.OrganizationID, nil, &left)
	if !left["left"] {
		t.Fatalf("leave response = %+v", left)
	}
	assertAPIErrorCode(t, server, http.MethodGet, "/api/organizations/"+owner.OrganizationID, teamOnly.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "FORBIDDEN")
	var audits struct {
		Items           []AuditLog `json:"items"`
		Page            int        `json:"page"`
		PageSize        int        `json:"pageSize"`
		Total           int        `json:"total"`
		RetentionPolicy string     `json:"retentionPolicy"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/organizations/"+owner.OrganizationID+"/audit-logs?pageSize=100", owner.AccessToken, owner.OrganizationID, nil, &audits)
	if audits.Total == 0 || audits.RetentionPolicy != "organization_lifetime" {
		t.Fatalf("audit list = %+v", audits)
	}
	wantAuditActions := map[string]bool{
		"team.created":             false,
		"role_binding.created":     false,
		"organization.updated":     false,
		"user.profile.updated":     false,
		"organization.member.left": false,
		"role.created":             false,
		"role.updated":             false,
		"role.deleted":             false,
	}
	for _, item := range audits.Items {
		if _, ok := wantAuditActions[item.Action]; ok {
			wantAuditActions[item.Action] = true
		}
	}
	for action, found := range wantAuditActions {
		if !found {
			t.Errorf("audit action %s was not returned", action)
		}
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
		Username:         "rbac-viewer-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
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
	principal, err := authService.ParseBearer("Bearer " + resp.AccessToken)
	if err != nil {
		t.Fatalf("parse org member access token: %v", err)
	}
	targetSession, err := authService.SwitchOrganization(ctx, principal, auth.SwitchOrganizationRequest{
		RefreshToken: resp.RefreshToken, OrganizationID: orgID,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/switch-organization", nil))
	if err != nil {
		t.Fatalf("switch org member organization: %v", err)
	}
	return targetSession
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
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, status, input, output, created_by,
			production_generation_id, video_production_binding_id, video_production_binding_revision
		)
		SELECT $1, $2, $3, $4, '{}', '{}', $5, generation.id, binding.id, binding.revision
		FROM projects project
		JOIN project_video_production_generations generation ON generation.id = project.active_video_production_generation_id
		JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
		WHERE project.id = $2
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
