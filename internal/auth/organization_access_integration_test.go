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

func TestOrganizationInvitationAndMemberLifecycle(t *testing.T) {
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
	service := NewService(pool, "organization-access-test-secret", time.Hour, 24*time.Hour)
	request := func(path string) *http.Request { return httptest.NewRequest(http.MethodPost, path, nil) }
	owner, err := service.Register(ctx, RegisterRequest{
		Email:            "access-owner-" + suffix + "@example.test",
		Username:         "access-owner-" + suffix,
		Password:         "Password123!",
		DisplayName:      "Access Owner",
		OrganizationName: "Access Org " + suffix,
	}, request("/api/auth/register"))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}

	existing, err := service.Register(ctx, RegisterRequest{
		Email:            "access-member-" + suffix + "@example.test",
		Username:         "access-member-" + suffix,
		Password:         "Password123!",
		DisplayName:      "Access Member",
		OrganizationName: "Member Personal Org " + suffix,
	}, request("/api/auth/register"))
	if err != nil {
		t.Fatalf("register existing user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id IN ($1, $2)`, owner.OrganizationID, existing.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, owner.User.ID, existing.User.ID)
	})

	var memberRoleID string
	if err := pool.QueryRow(ctx, `
		SELECT id FROM roles
		WHERE organization_id IS NULL AND role_key = 'org_member'
		LIMIT 1
	`).Scan(&memberRoleID); err != nil {
		t.Fatalf("select member role: %v", err)
	}
	var projectViewerRoleID string
	if err := pool.QueryRow(ctx, `
		SELECT id FROM roles
		WHERE organization_id IS NULL AND role_key = 'project_viewer'
		LIMIT 1
	`).Scan(&projectViewerRoleID); err != nil {
		t.Fatalf("select project viewer role: %v", err)
	}
	var ownerWorkspaceID, existingWorkspaceID string
	if err := pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE organization_id = $1 ORDER BY created_at LIMIT 1`, owner.OrganizationID).Scan(&ownerWorkspaceID); err != nil {
		t.Fatalf("select owner workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE organization_id = $1 ORDER BY created_at LIMIT 1`, existing.OrganizationID).Scan(&existingWorkspaceID); err != nil {
		t.Fatalf("select existing-user workspace: %v", err)
	}
	var ownerProjectID, otherProjectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by, video_production_state)
		VALUES ($1, $2, 'Invitation Project', $3, 'unconfigured')
		RETURNING id
	`, owner.OrganizationID, ownerWorkspaceID, owner.User.ID).Scan(&ownerProjectID); err != nil {
		t.Fatalf("create owner invitation project: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by, video_production_state)
		VALUES ($1, $2, 'Cross Organization Project', $3, 'unconfigured')
		RETURNING id
	`, existing.OrganizationID, existingWorkspaceID, existing.User.ID).Scan(&otherProjectID); err != nil {
		t.Fatalf("create cross-organization project: %v", err)
	}
	if _, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: "invalid-binding-" + suffix + "@example.test", BaseRoleID: memberRoleID,
		Bindings: []InvitationBindingRequest{{RoleID: projectViewerRoleID, ResourceType: "project", ProjectID: otherProjectID}},
	}); !errors.Is(err, ErrInvitationValidation) {
		t.Fatalf("cross-organization invitation binding error = %v", err)
	}

	newEmail := "invited-new-" + suffix + "@example.test"
	newInvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: newEmail, BaseRoleID: memberRoleID, ExpiresInDays: 7,
		Bindings: []InvitationBindingRequest{{RoleID: projectViewerRoleID, ResourceType: "project", ProjectID: ownerProjectID}},
	})
	if err != nil {
		t.Fatalf("create new-user invitation: %v", err)
	}
	if newInvite.InvitationToken == "" {
		t.Fatal("create invitation did not return its one-time plaintext token")
	}
	resolved, err := service.ResolveInvitation(ctx, newInvite.InvitationToken, request("/api/organization-invitations/resolve"))
	if err != nil {
		t.Fatalf("resolve new-user invitation: %v", err)
	}
	if !resolved.RequiresRegistration || resolved.Email == newEmail || !strings.Contains(resolved.Email, "***") {
		t.Fatalf("resolved new-user invitation = %+v", resolved)
	}
	newUser, err := service.RegisterWithInvitation(ctx, RegisterWithInvitationRequest{
		InvitationToken: newInvite.InvitationToken,
		Email:           newEmail,
		Username:        "invited-new-" + suffix,
		Password:        "Password123!",
		DisplayName:     "Invited New User",
	}, request("/api/auth/register-with-invitation"))
	if err != nil {
		t.Fatalf("register with invitation: %v", err)
	}
	if newUser.OrganizationID != owner.OrganizationID {
		t.Fatalf("registered target organization = %s, want %s", newUser.OrganizationID, owner.OrganizationID)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, newUser.User.ID)
	})
	var newUserOrganizationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM organization_members WHERE user_id = $1`, newUser.User.ID).Scan(&newUserOrganizationCount); err != nil {
		t.Fatalf("count new-user memberships: %v", err)
	}
	if newUserOrganizationCount != 1 {
		t.Fatalf("new-user membership count = %d, want 1", newUserOrganizationCount)
	}
	var newUserProjectBindingCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM role_bindings
		WHERE organization_id = $1 AND subject_user_id = $2 AND role_id = $3 AND resource_project_id = $4
	`, owner.OrganizationID, newUser.User.ID, projectViewerRoleID, ownerProjectID).Scan(&newUserProjectBindingCount); err != nil {
		t.Fatalf("count invited project binding: %v", err)
	}
	if newUserProjectBindingCount != 1 {
		t.Fatalf("invited project binding count = %d, want 1", newUserProjectBindingCount)
	}
	if _, err := service.RegisterWithInvitation(ctx, RegisterWithInvitationRequest{
		InvitationToken: newInvite.InvitationToken,
		Email:           newEmail, Username: "replay-" + suffix, Password: "Password123!",
	}, request("/api/auth/register-with-invitation")); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("registration invitation replay error = %v", err)
	}

	var staleProjectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by, video_production_state)
		VALUES ($1, $2, 'Disposable Invitation Project', $3, 'unconfigured')
		RETURNING id
	`, owner.OrganizationID, ownerWorkspaceID, owner.User.ID).Scan(&staleProjectID); err != nil {
		t.Fatalf("create disposable invitation project: %v", err)
	}
	staleInvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: existing.User.Email, BaseRoleID: memberRoleID,
		Bindings: []InvitationBindingRequest{{RoleID: projectViewerRoleID, ResourceType: "project", ProjectID: staleProjectID}},
	})
	if err != nil {
		t.Fatalf("create stale-resource invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, staleProjectID); err != nil {
		t.Fatalf("delete invited project: %v", err)
	}
	if _, err := service.ResolveInvitation(ctx, staleInvite.InvitationToken, request("/api/organization-invitations/resolve")); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("stale-resource invitation resolution error = %v", err)
	}
	if err := service.RevokeInvitation(ctx, owner.OrganizationID, staleInvite.ID, owner.User.ID); err != nil {
		t.Fatalf("revoke stale-resource invitation: %v", err)
	}

	existingInvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: existing.User.Email, BaseRoleID: memberRoleID,
		Bindings: []InvitationBindingRequest{{RoleID: projectViewerRoleID, ResourceType: "project", ProjectID: ownerProjectID}},
	})
	if err != nil {
		t.Fatalf("create existing-user invitation: %v", err)
	}
	resolved, err = service.ResolveInvitation(ctx, existingInvite.InvitationToken, request("/api/organization-invitations/resolve"))
	if err != nil || resolved.RequiresRegistration {
		t.Fatalf("resolve existing-user invitation = %+v, %v", resolved, err)
	}
	ownerPrincipal, err := service.ParseBearer("Bearer " + owner.AccessToken)
	if err != nil {
		t.Fatalf("parse owner access token: %v", err)
	}
	if _, err := service.AcceptInvitation(ctx, ownerPrincipal, existingInvite.InvitationToken, request("/api/organization-invitations/accept")); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("mismatched-email acceptance error = %v", err)
	}
	existingPrincipal, err := service.ParseBearer("Bearer " + existing.AccessToken)
	if err != nil {
		t.Fatalf("parse existing access token: %v", err)
	}
	targetSession, err := service.AcceptInvitation(ctx, existingPrincipal, existingInvite.InvitationToken, request("/api/organization-invitations/accept"))
	if err != nil {
		t.Fatalf("accept existing-user invitation: %v", err)
	}
	if targetSession.OrganizationID != owner.OrganizationID {
		t.Fatalf("accepted target organization = %s, want %s", targetSession.OrganizationID, owner.OrganizationID)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO project_members(project_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, ownerProjectID, existing.User.ID); err != nil {
		t.Fatalf("insert project membership for lifecycle test: %v", err)
	}
	if _, err := service.AcceptInvitation(ctx, existingPrincipal, existingInvite.InvitationToken, request("/api/organization-invitations/accept")); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("accept invitation replay error = %v", err)
	}

	var teamID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO teams(organization_id, name, slug, status, created_by)
		VALUES ($1, 'Lifecycle Team', $2, 'active', $3)
		RETURNING id
	`, owner.OrganizationID, "lifecycle-team-"+suffix, owner.User.ID).Scan(&teamID); err != nil {
		t.Fatalf("create lifecycle team: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO team_members(team_id, user_id, status, created_by)
		VALUES ($1, $2, 'active', $3)
	`, teamID, existing.User.ID, owner.User.ID); err != nil {
		t.Fatalf("add lifecycle team member: %v", err)
	}

	disabled, err := service.SetOrganizationMemberStatus(ctx, owner.OrganizationID, existing.User.ID, owner.User.ID, "disabled")
	if err != nil {
		t.Fatalf("disable member: %v", err)
	}
	if disabled.Status != "disabled" || len(disabled.Teams) != 1 || len(disabled.Roles) == 0 {
		t.Fatalf("disabled member did not preserve access relationships: %+v", disabled)
	}
	targetPrincipal, err := service.ParseBearer("Bearer " + targetSession.AccessToken)
	if err != nil {
		t.Fatalf("parse target access token: %v", err)
	}
	if err := service.ValidatePrincipalActive(ctx, targetPrincipal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled member principal validation error = %v", err)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: targetSession.RefreshToken}, request("/api/auth/refresh")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("disabled member refresh error = %v", err)
	}

	restored, err := service.SetOrganizationMemberStatus(ctx, owner.OrganizationID, existing.User.ID, owner.User.ID, "active")
	if err != nil {
		t.Fatalf("restore member: %v", err)
	}
	if restored.Status != "active" || len(restored.Teams) != 1 || len(restored.Roles) == 0 {
		t.Fatalf("restored member access relationships = %+v", restored)
	}
	if err := service.ValidatePrincipalActive(ctx, targetPrincipal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("pre-disable access token revived after restore: %v", err)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: targetSession.RefreshToken}, request("/api/auth/refresh")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("pre-disable refresh token revived after restore: %v", err)
	}
	preRemovalLogin, err := service.Login(ctx, LoginRequest{Identifier: existing.User.Username, Password: "Password123!"}, request("/api/auth/login"))
	if err != nil || !preRemovalLogin.RequiresOrganizationSelection {
		t.Fatalf("pre-removal login = %+v, %v", preRemovalLogin, err)
	}
	preRemovalSession, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: preRemovalLogin.OrganizationSelectionToken,
		OrganizationID:             owner.OrganizationID,
	}, request("/api/auth/select-organization"))
	if err != nil {
		t.Fatalf("select organization before removal: %v", err)
	}
	preRemovalPrincipal, err := service.ParseBearer("Bearer " + preRemovalSession.AccessToken)
	if err != nil {
		t.Fatalf("parse pre-removal access token: %v", err)
	}
	if err := service.ValidatePrincipalActive(ctx, preRemovalPrincipal); err != nil {
		t.Fatalf("validate pre-removal access token: %v", err)
	}
	staleSelectionLogin, err := service.Login(ctx, LoginRequest{Identifier: existing.User.Username, Password: "Password123!"}, request("/api/auth/login"))
	if err != nil || !staleSelectionLogin.RequiresOrganizationSelection {
		t.Fatalf("login for stale organization selection token = %+v, %v", staleSelectionLogin, err)
	}
	preRemovalSelectionToken := staleSelectionLogin.OrganizationSelectionToken
	var versionBeforeRemoval int64
	if err := pool.QueryRow(ctx, `
		SELECT authorization_version FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`, owner.OrganizationID, existing.User.ID).Scan(&versionBeforeRemoval); err != nil {
		t.Fatalf("select pre-removal authorization version: %v", err)
	}
	if err := service.RemoveOrganizationMember(ctx, owner.OrganizationID, existing.User.ID, owner.User.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if err := service.ValidatePrincipalActive(ctx, preRemovalPrincipal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("removed member access token error = %v, want ErrForbidden", err)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: preRemovalSession.RefreshToken}, request("/api/auth/refresh")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("removed member refresh token error = %v, want ErrUnauthorized", err)
	}
	if err := service.ValidatePrincipalActive(ctx, existingPrincipal); err != nil {
		t.Fatalf("removal from one organization invalidated another organization token: %v", err)
	}
	removed, err := service.GetOrganizationMember(ctx, owner.OrganizationID, existing.User.ID)
	if err != nil {
		t.Fatalf("get removed member: %v", err)
	}
	if removed.Status != "removed" || len(removed.Teams) != 0 || len(removed.Roles) != 0 {
		t.Fatalf("removed member retained access relationships: %+v", removed)
	}
	var removedProjectMemberships, removedRoleBindings int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE project_id = $1 AND user_id = $2`, ownerProjectID, existing.User.ID).Scan(&removedProjectMemberships); err != nil {
		t.Fatalf("count removed project memberships: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_bindings WHERE organization_id = $1 AND subject_user_id = $2`, owner.OrganizationID, existing.User.ID).Scan(&removedRoleBindings); err != nil {
		t.Fatalf("count removed role bindings: %v", err)
	}
	if removedProjectMemberships != 0 || removedRoleBindings != 0 {
		t.Fatalf("removed access relationships: projectMemberships=%d roleBindings=%d", removedProjectMemberships, removedRoleBindings)
	}
	if _, err := service.SetOrganizationMemberStatus(ctx, owner.OrganizationID, existing.User.ID, owner.User.ID, "active"); !errors.Is(err, ErrMemberLifecycle) {
		t.Fatalf("direct restore of removed member error = %v", err)
	}

	reinvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: existing.User.Email, BaseRoleID: memberRoleID,
	})
	if err != nil {
		t.Fatalf("reinvite removed member: %v", err)
	}
	rejoinedSession, err := service.AcceptInvitation(ctx, existingPrincipal, reinvite.InvitationToken, request("/api/organization-invitations/accept"))
	if err != nil {
		t.Fatalf("accept reinvitation: %v", err)
	}
	rejoined, err := service.GetOrganizationMember(ctx, owner.OrganizationID, existing.User.ID)
	if err != nil || rejoined.Status != "active" {
		t.Fatalf("rejoined member = %+v, %v", rejoined, err)
	}
	if err := service.ValidatePrincipalActive(ctx, preRemovalPrincipal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("pre-removal access token revived after reinvitation: %v", err)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: preRemovalSession.RefreshToken}, request("/api/auth/refresh")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("pre-removal refresh token revived after reinvitation: %v", err)
	}
	if _, err := service.SelectOrganization(ctx, SelectOrganizationRequest{
		OrganizationSelectionToken: preRemovalSelectionToken,
		OrganizationID:             owner.OrganizationID,
	}, request("/api/auth/select-organization")); !errors.Is(err, ErrOrganizationSelection) {
		t.Fatalf("pre-removal organization selection token revived after reinvitation: %v", err)
	}
	rejoinedPrincipal, err := service.ParseBearer("Bearer " + rejoinedSession.AccessToken)
	if err != nil {
		t.Fatalf("parse rejoined access token: %v", err)
	}
	if err := service.ValidatePrincipalActive(ctx, rejoinedPrincipal); err != nil {
		t.Fatalf("validate rejoined access token: %v", err)
	}
	if _, err := service.Refresh(ctx, RefreshRequest{RefreshToken: rejoinedSession.RefreshToken}, request("/api/auth/refresh")); err != nil {
		t.Fatalf("refresh rejoined session: %v", err)
	}
	var versionAfterRejoin int64
	if err := pool.QueryRow(ctx, `
		SELECT authorization_version FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`, owner.OrganizationID, existing.User.ID).Scan(&versionAfterRejoin); err != nil {
		t.Fatalf("select rejoined authorization version: %v", err)
	}
	if versionAfterRejoin <= versionBeforeRemoval {
		t.Fatalf("authorization version after rejoin = %d, want greater than %d", versionAfterRejoin, versionBeforeRemoval)
	}

	if _, err := service.SetOrganizationMemberStatus(ctx, owner.OrganizationID, owner.User.ID, owner.User.ID, "disabled"); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("disable last direct owner error = %v", err)
	}
	if err := service.RemoveOrganizationMember(ctx, owner.OrganizationID, owner.User.ID, owner.User.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("remove last direct owner error = %v", err)
	}

	expiredInvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: "expired-" + suffix + "@example.test", BaseRoleID: memberRoleID,
	})
	if err != nil {
		t.Fatalf("create expiring invitation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE organization_invitations
		SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day'
		WHERE id = $1
	`, expiredInvite.ID); err != nil {
		t.Fatalf("expire invitation: %v", err)
	}
	if _, err := service.ResolveInvitation(ctx, expiredInvite.InvitationToken, request("/api/organization-invitations/resolve")); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("expired invitation resolution error = %v", err)
	}
	reissuedInvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: "expired-" + suffix + "@example.test", BaseRoleID: memberRoleID,
	})
	if err != nil {
		t.Fatalf("reissue expired invitation: %v", err)
	}
	if err := service.RevokeInvitation(ctx, owner.OrganizationID, reissuedInvite.ID, owner.User.ID); err != nil {
		t.Fatalf("revoke reissued invitation: %v", err)
	}

	revokedInvite, err := service.CreateInvitation(ctx, owner.OrganizationID, owner.User.ID, CreateInvitationRequest{
		Email: "revoked-" + suffix + "@example.test", BaseRoleID: memberRoleID,
	})
	if err != nil {
		t.Fatalf("create revocable invitation: %v", err)
	}
	if err := service.RevokeInvitation(ctx, owner.OrganizationID, revokedInvite.ID, owner.User.ID); err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	invitationPage, err := service.ListInvitations(ctx, owner.OrganizationID, 1, 2)
	if err != nil {
		t.Fatalf("list paginated invitations: %v", err)
	}
	if invitationPage.Page != 1 || invitationPage.PageSize != 2 || len(invitationPage.Items) != 2 || invitationPage.Total < 6 {
		t.Fatalf("invitation page = %+v", invitationPage)
	}
	wantAuditActions := []string{
		"organization.invitation.created",
		"organization.invitation.accepted",
		"organization.invitation.revoked",
		"organization.member.disabled",
		"organization.member.restored",
		"organization.member.removed",
	}
	for _, action := range wantAuditActions {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND action = $2`, owner.OrganizationID, action).Scan(&count); err != nil {
			t.Fatalf("count audit action %s: %v", action, err)
		}
		if count == 0 {
			t.Errorf("audit action %s was not recorded", action)
		}
	}
	for _, plaintextToken := range []string{newInvite.InvitationToken, existingInvite.InvitationToken, reinvite.InvitationToken, expiredInvite.InvitationToken, reissuedInvite.InvitationToken, revokedInvite.InvitationToken} {
		var leaked bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM audit_logs
				WHERE organization_id = $1 AND metadata::text LIKE '%' || $2 || '%'
			)
		`, owner.OrganizationID, plaintextToken).Scan(&leaked); err != nil {
			t.Fatalf("check plaintext invitation token in audit metadata: %v", err)
		}
		if leaked {
			t.Fatal("plaintext invitation token was written to audit metadata")
		}
	}
}
