package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvitationInvalid    = errors.New("invitation is invalid or expired")
	ErrInvitationValidation = errors.New("invitation request is invalid")
	ErrInvitationConflict   = errors.New("invitation conflicts with current membership")
)

type InvitationBindingRequest struct {
	RoleID         string `json:"roleId"`
	ResourceType   string `json:"resourceType"`
	OrganizationID string `json:"organizationId,omitempty"`
	WorkspaceID    string `json:"workspaceId,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
}

type CreateInvitationRequest struct {
	Email         string                     `json:"email"`
	BaseRoleID    string                     `json:"baseRoleId"`
	ExpiresInDays int                        `json:"expiresInDays"`
	Bindings      []InvitationBindingRequest `json:"bindings,omitempty"`
}

type Invitation struct {
	ID                   string                     `json:"id"`
	OrganizationID       string                     `json:"organizationId"`
	Email                string                     `json:"email"`
	Status               string                     `json:"status"`
	BaseRoleID           string                     `json:"baseRoleId"`
	ExpiresAt            time.Time                  `json:"expiresAt"`
	AcceptedAt           *time.Time                 `json:"acceptedAt,omitempty"`
	AcceptedBy           *string                    `json:"acceptedBy,omitempty"`
	InvitedBy            string                     `json:"invitedBy"`
	CreatedAt            time.Time                  `json:"createdAt"`
	UpdatedAt            time.Time                  `json:"updatedAt"`
	Bindings             []InvitationBindingRequest `json:"bindings"`
	InvitationToken      string                     `json:"invitationToken,omitempty"`
	RequiresRegistration bool                       `json:"requiresRegistration,omitempty"`
	OrganizationName     string                     `json:"organizationName,omitempty"`
	BindingCount         int                        `json:"-"`
}

type InvitationList struct {
	Items    []Invitation `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Total    int          `json:"total"`
}

type ResolveInvitationRequest struct {
	InvitationToken string `json:"invitationToken"`
}

type AcceptInvitationRequest struct {
	InvitationToken string `json:"invitationToken"`
}

type RegisterWithInvitationRequest struct {
	InvitationToken string `json:"invitationToken"`
	Email           string `json:"email"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	DisplayName     string `json:"displayName"`
}

func (s *Service) CreateInvitation(ctx context.Context, organizationID, invitedBy string, req CreateInvitationRequest) (Invitation, error) {
	email := normalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") || strings.TrimSpace(req.BaseRoleID) == "" {
		return Invitation{}, ErrInvitationValidation
	}
	days := req.ExpiresInDays
	if days == 0 {
		days = 7
	}
	if days < 1 || days > 30 {
		return Invitation{}, ErrInvitationValidation
	}
	token, err := randomToken("inv_")
	if err != nil {
		return Invitation{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer rollback(ctx, tx)

	if err := validateInvitationRolesAndResources(ctx, tx, organizationID, req.BaseRoleID, req.Bindings); err != nil {
		return Invitation{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE organization_invitations
		SET status = 'revoked', updated_at = now()
		WHERE organization_id = $1 AND email = $2
		  AND status = 'pending' AND expires_at <= now()
	`, organizationID, email); err != nil {
		return Invitation{}, err
	}
	var conflictingMembership bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members om
			JOIN users u ON u.id = om.user_id
			WHERE om.organization_id = $1 AND u.email = $2 AND om.status IN ('active', 'disabled')
		)
	`, organizationID, email).Scan(&conflictingMembership); err != nil {
		return Invitation{}, err
	}
	if conflictingMembership {
		return Invitation{}, ErrInvitationConflict
	}

	var item Invitation
	err = tx.QueryRow(ctx, `
		INSERT INTO organization_invitations(
			organization_id, email, token_hash, base_role_id, binding_count, expires_at, invited_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, organization_id, email, status, base_role_id, expires_at,
		          accepted_at, accepted_by, invited_by, created_at, updated_at, binding_count
	`, organizationID, email, hashRefreshToken(token), req.BaseRoleID, len(req.Bindings), expiresAt, invitedBy).Scan(
		&item.ID, &item.OrganizationID, &item.Email, &item.Status, &item.BaseRoleID, &item.ExpiresAt,
		&item.AcceptedAt, &item.AcceptedBy, &item.InvitedBy, &item.CreatedAt, &item.UpdatedAt, &item.BindingCount,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Invitation{}, ErrInvitationConflict
		}
		return Invitation{}, err
	}
	for _, binding := range req.Bindings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_invitation_bindings(
				invitation_id, role_id, resource_type,
				resource_organization_id, resource_workspace_id, resource_project_id
			)
			VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid)
		`, item.ID, binding.RoleID, binding.ResourceType, binding.OrganizationID, binding.WorkspaceID, binding.ProjectID); err != nil {
			return Invitation{}, err
		}
	}
	if err := auditlog.Append(ctx, tx, organizationID, invitedBy, auditlog.ActionInvitationCreated, "organization_invitation", item.ID, map[string]any{
		"baseRoleId":   item.BaseRoleID,
		"bindingCount": len(req.Bindings),
		"expiresAt":    item.ExpiresAt,
	}); err != nil {
		return Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, err
	}
	item.Bindings = append([]InvitationBindingRequest(nil), req.Bindings...)
	item.InvitationToken = token
	return item, nil
}

func (s *Service) ListInvitations(ctx context.Context, organizationID string, page, pageSize int) (InvitationList, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM organization_invitations WHERE organization_id = $1`, organizationID).Scan(&total); err != nil {
		return InvitationList{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, organization_id, email,
		       CASE WHEN status = 'pending' AND expires_at <= now() THEN 'expired' ELSE status END,
		       base_role_id, expires_at, accepted_at, accepted_by, invited_by, created_at, updated_at
		FROM organization_invitations
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, organizationID, pageSize, (page-1)*pageSize)
	if err != nil {
		return InvitationList{}, err
	}
	defer rows.Close()
	items := make([]Invitation, 0)
	for rows.Next() {
		var item Invitation
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Email, &item.Status, &item.BaseRoleID,
			&item.ExpiresAt, &item.AcceptedAt, &item.AcceptedBy, &item.InvitedBy, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return InvitationList{}, err
		}
		bindings, err := listInvitationBindings(ctx, s.db, item.ID)
		if err != nil {
			return InvitationList{}, err
		}
		item.Bindings = bindings
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return InvitationList{}, err
	}
	return InvitationList{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) RevokeInvitation(ctx context.Context, organizationID, invitationID, actorID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	command, err := tx.Exec(ctx, `
		UPDATE organization_invitations
		SET status = 'revoked', updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND status = 'pending' AND expires_at > now()
	`, invitationID, organizationID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrInvitationInvalid
	}
	if err := auditlog.Append(ctx, tx, organizationID, actorID, auditlog.ActionInvitationRevoked, "organization_invitation", invitationID, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) ResolveInvitation(ctx context.Context, token string, r *http.Request) (Invitation, error) {
	subject := invitationRateSubject(token, "")
	if err := s.checkSecurityRateLimit(ctx, securityActionInvitationResolve, subject, r); err != nil {
		return Invitation{}, err
	}
	item, err := s.resolveInvitation(ctx, token)
	if errors.Is(err, ErrInvitationInvalid) {
		if recordErr := s.recordSecurityFailure(ctx, securityActionInvitationResolve, subject, r); recordErr != nil {
			return Invitation{}, recordErr
		}
	}
	if err == nil {
		if clearErr := s.clearSecurityIdentityFailures(ctx, securityActionInvitationResolve, subject); clearErr != nil {
			return Invitation{}, clearErr
		}
	}
	return item, err
}

func (s *Service) resolveInvitation(ctx context.Context, token string) (Invitation, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer rollback(ctx, tx)
	var item Invitation
	var userExists bool
	err = tx.QueryRow(ctx, `
		SELECT i.id, i.organization_id, o.name, i.email, i.status, i.base_role_id,
		       i.expires_at, i.invited_by, i.created_at, i.updated_at, i.binding_count,
		       EXISTS(SELECT 1 FROM users u WHERE u.email = i.email AND u.status = 'active')
		FROM organization_invitations i
		JOIN organizations o ON o.id = i.organization_id
		WHERE i.token_hash = $1 AND i.status = 'pending' AND i.expires_at > now()
	`, hashRefreshToken(token)).Scan(
		&item.ID, &item.OrganizationID, &item.OrganizationName, &item.Email, &item.Status,
		&item.BaseRoleID, &item.ExpiresAt, &item.InvitedBy, &item.CreatedAt, &item.UpdatedAt, &item.BindingCount, &userExists,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invitation{}, ErrInvitationInvalid
		}
		return Invitation{}, err
	}
	bindings, err := listInvitationBindings(ctx, tx, item.ID)
	if err != nil {
		return Invitation{}, err
	}
	if len(bindings) != item.BindingCount || validateInvitationRolesAndResources(ctx, tx, item.OrganizationID, item.BaseRoleID, bindings) != nil {
		return Invitation{}, ErrInvitationInvalid
	}
	item.Email = maskEmail(item.Email)
	item.RequiresRegistration = !userExists
	item.Bindings = []InvitationBindingRequest{}
	return item, nil
}

func (s *Service) AcceptInvitation(ctx context.Context, principal Principal, token string, r *http.Request) (TokenResponse, error) {
	subject := invitationRateSubject(token, principal.UserID)
	if err := s.checkSecurityRateLimit(ctx, securityActionInvitationAccept, subject, r); err != nil {
		return TokenResponse{}, err
	}
	response, err := s.acceptInvitation(ctx, principal, token, r)
	if errors.Is(err, ErrInvitationInvalid) || errors.Is(err, ErrInvitationConflict) {
		if recordErr := s.recordSecurityFailure(ctx, securityActionInvitationAccept, subject, r); recordErr != nil {
			return TokenResponse{}, recordErr
		}
	}
	if err == nil {
		if clearErr := s.clearSecurityIdentityFailures(ctx, securityActionInvitationAccept, subject); clearErr != nil {
			return TokenResponse{}, clearErr
		}
	}
	return response, err
}

func (s *Service) acceptInvitation(ctx context.Context, principal Principal, token string, r *http.Request) (TokenResponse, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)
	var user UserResponse
	if err := tx.QueryRow(ctx, `
		SELECT id, email, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(avatar_url, '')
		FROM users WHERE id = $1 AND status = 'active'
	`, principal.UserID).Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.AvatarURL); err != nil {
		return TokenResponse{}, ErrUnauthorized
	}
	invitation, err := acceptInvitationTx(ctx, tx, hashRefreshToken(token), user, principal.UserID)
	if err != nil {
		return TokenResponse{}, err
	}
	response, err := s.createSessionResponse(ctx, tx, user, invitation.OrganizationID, r)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) RegisterWithInvitation(ctx context.Context, req RegisterWithInvitationRequest, r *http.Request) (TokenResponse, error) {
	subject := invitationRateSubject(req.InvitationToken, registrationRateSubject(req.Email, req.Username))
	if err := s.checkSecurityRateLimit(ctx, securityActionInvitationRegister, subject, r); err != nil {
		return TokenResponse{}, err
	}
	response, err := s.registerWithInvitation(ctx, req, r)
	if errors.Is(err, ErrEmailExists) || errors.Is(err, ErrUsernameExists) {
		err = ErrRegistrationUnavailable
	}
	if isRegistrationFailure(err) || errors.Is(err, ErrInvitationInvalid) {
		if recordErr := s.recordSecurityFailure(ctx, securityActionInvitationRegister, subject, r); recordErr != nil {
			return TokenResponse{}, recordErr
		}
	}
	if err == nil {
		if clearErr := s.clearSecurityIdentityFailures(ctx, securityActionInvitationRegister, subject); clearErr != nil {
			return TokenResponse{}, clearErr
		}
	}
	return response, err
}

func (s *Service) registerWithInvitation(ctx context.Context, req RegisterWithInvitationRequest, r *http.Request) (TokenResponse, error) {
	email := normalizeEmail(req.Email)
	username, normalized, err := NormalizeUsername(req.Username)
	if err != nil {
		return TokenResponse{}, ErrInvalidUsername
	}
	if email == "" || len(req.Password) < 8 {
		return TokenResponse{}, ErrInvalidCredentials
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return TokenResponse{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)
	var invitationEmail string
	if err := tx.QueryRow(ctx, `
		SELECT email FROM organization_invitations
		WHERE token_hash = $1 AND status = 'pending' AND expires_at > now()
		FOR UPDATE
	`, hashRefreshToken(req.InvitationToken)).Scan(&invitationEmail); err != nil || invitationEmail != email {
		return TokenResponse{}, ErrInvitationInvalid
	}
	var user UserResponse
	err = tx.QueryRow(ctx, `
		INSERT INTO users(email, username, username_normalized, password_hash, display_name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, email, username, display_name
	`, email, username, normalized, string(passwordHash), displayName).Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName)
	if err != nil {
		if isUniqueConstraint(err, "users_username_normalized_unique") {
			return TokenResponse{}, ErrUsernameExists
		}
		if isUniqueViolation(err) {
			return TokenResponse{}, ErrEmailExists
		}
		return TokenResponse{}, err
	}
	controlKey, err := createControlKeyTx(ctx, tx, user.ID, false)
	if err != nil {
		return TokenResponse{}, err
	}
	invitation, err := acceptInvitationTx(ctx, tx, hashRefreshToken(req.InvitationToken), user, user.ID)
	if err != nil {
		return TokenResponse{}, err
	}
	response, err := s.createSessionResponse(ctx, tx, user, invitation.OrganizationID, r)
	if err != nil {
		return TokenResponse{}, err
	}
	response.CodexControlKey = &controlKey
	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func acceptInvitationTx(ctx context.Context, tx pgx.Tx, tokenHash string, user UserResponse, acceptedBy string) (Invitation, error) {
	var invitation Invitation
	err := tx.QueryRow(ctx, `
		SELECT id, organization_id, email, status, base_role_id, expires_at,
		       accepted_at, accepted_by, invited_by, created_at, updated_at, binding_count
		FROM organization_invitations
		WHERE token_hash = $1 AND status = 'pending' AND expires_at > now()
		FOR UPDATE
	`, tokenHash).Scan(
		&invitation.ID, &invitation.OrganizationID, &invitation.Email, &invitation.Status,
		&invitation.BaseRoleID, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.AcceptedBy,
		&invitation.InvitedBy, &invitation.CreatedAt, &invitation.UpdatedAt, &invitation.BindingCount,
	)
	if err != nil || invitation.Email != normalizeEmail(user.Email) {
		return Invitation{}, ErrInvitationInvalid
	}
	bindings, err := listInvitationBindings(ctx, tx, invitation.ID)
	if err != nil {
		return Invitation{}, err
	}
	if len(bindings) != invitation.BindingCount {
		return Invitation{}, ErrInvitationInvalid
	}
	if err := validateInvitationRolesAndResources(ctx, tx, invitation.OrganizationID, invitation.BaseRoleID, bindings); err != nil {
		return Invitation{}, ErrInvitationInvalid
	}
	var existingStatus sql.NullString
	err = tx.QueryRow(ctx, `
		SELECT status FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
		FOR UPDATE
	`, invitation.OrganizationID, user.ID).Scan(&existingStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, err
	}
	if existingStatus.Valid && existingStatus.String != "removed" {
		return Invitation{}, ErrInvitationConflict
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members(organization_id, user_id, status)
		VALUES ($1, $2, 'active')
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			status = 'active', updated_at = now(), disabled_at = NULL, disabled_by = NULL,
			removed_at = NULL, removed_by = NULL,
			authorization_version = organization_members.authorization_version + 1
	`, invitation.OrganizationID, user.ID); err != nil {
		return Invitation{}, err
	}
	if err := revokeMembershipAuthorizationArtifacts(ctx, tx, invitation.OrganizationID, user.ID); err != nil {
		return Invitation{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_organization_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'organization', $1, $4)
		ON CONFLICT DO NOTHING
	`, invitation.OrganizationID, invitation.BaseRoleID, user.ID, invitation.InvitedBy); err != nil {
		return Invitation{}, err
	}
	for _, binding := range bindings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_bindings(
				organization_id, role_id, subject_type, subject_user_id, resource_type,
				resource_organization_id, resource_workspace_id, resource_project_id, created_by
			)
			VALUES ($1, $2, 'user', $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, $8)
			ON CONFLICT DO NOTHING
		`, invitation.OrganizationID, binding.RoleID, user.ID, binding.ResourceType,
			binding.OrganizationID, binding.WorkspaceID, binding.ProjectID, invitation.InvitedBy); err != nil {
			return Invitation{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE organization_invitations
		SET status = 'accepted', accepted_at = now(), accepted_by = $2, updated_at = now()
		WHERE id = $1
	`, invitation.ID, acceptedBy); err != nil {
		return Invitation{}, err
	}
	if err := auditlog.Append(ctx, tx, invitation.OrganizationID, acceptedBy, auditlog.ActionInvitationAccepted, "organization_invitation", invitation.ID, map[string]any{
		"userId":       user.ID,
		"baseRoleId":   invitation.BaseRoleID,
		"bindingCount": len(bindings),
	}); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

type invitationBindingQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listInvitationBindings(ctx context.Context, queryer invitationBindingQueryer, invitationID string) ([]InvitationBindingRequest, error) {
	rows, err := queryer.Query(ctx, `
		SELECT role_id, resource_type,
		       COALESCE(resource_organization_id::text, ''),
		       COALESCE(resource_workspace_id::text, ''),
		       COALESCE(resource_project_id::text, '')
		FROM organization_invitation_bindings
		WHERE invitation_id = $1
		ORDER BY created_at, id
	`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]InvitationBindingRequest, 0)
	for rows.Next() {
		var item InvitationBindingRequest
		if err := rows.Scan(&item.RoleID, &item.ResourceType, &item.OrganizationID, &item.WorkspaceID, &item.ProjectID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validateInvitationRolesAndResources(ctx context.Context, tx pgx.Tx, organizationID, baseRoleID string, bindings []InvitationBindingRequest) error {
	var baseRoleScope string
	if err := tx.QueryRow(ctx, `
		SELECT scope FROM roles
		WHERE id = $1 AND scope = 'organization'
		  AND role_key IN ('org_member', 'organization_member')
		  AND (organization_id IS NULL OR organization_id = $2)
	`, baseRoleID, organizationID).Scan(&baseRoleScope); err != nil {
		return ErrInvitationValidation
	}
	for _, binding := range bindings {
		var roleScope string
		if err := tx.QueryRow(ctx, `
			SELECT scope FROM roles
			WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2)
		`, binding.RoleID, organizationID).Scan(&roleScope); err != nil || roleScope != binding.ResourceType {
			return ErrInvitationValidation
		}
		switch binding.ResourceType {
		case "organization":
			if binding.OrganizationID != organizationID || binding.WorkspaceID != "" || binding.ProjectID != "" {
				return ErrInvitationValidation
			}
		case "workspace":
			var resourceOrganizationID string
			if binding.OrganizationID != "" || binding.WorkspaceID == "" || binding.ProjectID != "" ||
				tx.QueryRow(ctx, `SELECT organization_id FROM workspaces WHERE id = $1`, binding.WorkspaceID).Scan(&resourceOrganizationID) != nil || resourceOrganizationID != organizationID {
				return ErrInvitationValidation
			}
		case "project":
			var resourceOrganizationID string
			if binding.OrganizationID != "" || binding.WorkspaceID != "" || binding.ProjectID == "" ||
				tx.QueryRow(ctx, `SELECT organization_id FROM projects WHERE id = $1`, binding.ProjectID).Scan(&resourceOrganizationID) != nil || resourceOrganizationID != organizationID {
				return ErrInvitationValidation
			}
		default:
			return ErrInvitationValidation
		}
	}
	return nil
}

func maskEmail(value string) string {
	parts := strings.SplitN(value, "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "***"
	}
	localRunes := []rune(parts[0])
	local := string(localRunes[0]) + "***"
	return local + "@" + parts[1]
}
