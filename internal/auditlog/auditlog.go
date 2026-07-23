package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ActionInvitationCreated               = "organization.invitation.created"
	ActionInvitationRevoked               = "organization.invitation.revoked"
	ActionInvitationAccepted              = "organization.invitation.accepted"
	ActionMemberDisabled                  = "organization.member.disabled"
	ActionMemberRestored                  = "organization.member.restored"
	ActionMemberRemoved                   = "organization.member.removed"
	ActionMemberLeft                      = "organization.member.left"
	ActionMemberProfileUpdated            = "organization.member.profile_updated"
	ActionMemberPasswordResetRequested    = "organization.member.password_reset_requested"
	ActionMemberPasswordResetCompleted    = "organization.member.password_reset_completed"
	ActionTeamCreated                     = "team.created"
	ActionTeamUpdated                     = "team.updated"
	ActionTeamDisabled                    = "team.disabled"
	ActionTeamMemberAdded                 = "team.member.added"
	ActionTeamMemberRemoved               = "team.member.removed"
	ActionRoleBindingCreated              = "role_binding.created"
	ActionRoleBindingRevoked              = "role_binding.revoked"
	ActionOrganizationUpdated             = "organization.updated"
	ActionSystemOrganizationCreated       = "system.organization.created"
	ActionSystemOrganizationMemberCreated = "system.organization.member.created"
	ActionSystemOrganizationMemberUpdated = "system.organization.member.updated"
	ActionUserProfileUpdated              = "user.profile.updated"
	ActionUsernameSet                     = "user.username.set"
	ActionRoleCreated                     = "role.created"
	ActionRoleUpdated                     = "role.updated"
	ActionRoleDeleted                     = "role.deleted"
)

type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Append writes one organization-scoped audit record through the repository's
// audit_logs table. Callers should pass their current transaction when the
// audited change is transactional.
func Append(
	ctx context.Context,
	execer Execer,
	organizationID string,
	actorUserID string,
	action string,
	resourceType string,
	resourceID string,
	metadata map[string]any,
) error {
	organizationID = strings.TrimSpace(organizationID)
	action = strings.TrimSpace(action)
	resourceType = strings.TrimSpace(resourceType)
	if organizationID == "" || action == "" || resourceType == "" {
		return fmt.Errorf("organization id, action, and resource type are required for audit records")
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	_, err = execer.Exec(ctx, `
		INSERT INTO audit_logs(
			organization_id, actor_user_id, action, resource_type, resource_id, metadata
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, '')::uuid, $6)
	`, organizationID, strings.TrimSpace(actorUserID), action, resourceType, strings.TrimSpace(resourceID), payload)
	return err
}
