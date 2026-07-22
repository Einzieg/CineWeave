package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSharedAccountManagement = errors.New("account belongs to multiple organizations")
	ErrMemberAccountProtected  = errors.New("member account is protected")
	ErrMemberProfileValidation = errors.New("member profile request is invalid")
	ErrPasswordResetInvalid    = errors.New("password reset is invalid")
	ErrPasswordResetValidation = errors.New("password reset request is invalid")
)

const organizationPasswordResetTTL = 30 * time.Minute

type MemberPasswordReset struct {
	UserID     string    `json:"userId"`
	ResetToken string    `json:"resetToken"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type CompletePasswordResetRequest struct {
	ResetToken string `json:"resetToken"`
	Password   string `json:"password"`
}

func (s *Service) UpdateOrganizationMemberProfile(
	ctx context.Context,
	organizationID, userID, actorID string,
	req UpdateProfileRequest,
) (OrganizationMember, error) {
	displayName, avatarURL, changedFields, err := normalizeMemberProfileUpdate(req)
	if err != nil {
		return OrganizationMember{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OrganizationMember{}, err
	}
	defer rollback(ctx, tx)
	if err := ensureOrganizationCanManageMemberAccount(ctx, tx, organizationID, userID, actorID); err != nil {
		return OrganizationMember{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE users
		SET display_name = CASE WHEN $2::boolean THEN NULLIF($3, '') ELSE display_name END,
		    avatar_url = CASE WHEN $4::boolean THEN NULLIF($5, '') ELSE avatar_url END,
		    updated_at = now()
		WHERE id = $1 AND status = 'active'
	`, userID, req.DisplayName != nil, displayName, req.AvatarURL != nil, avatarURL)
	if err != nil {
		return OrganizationMember{}, err
	}
	if command.RowsAffected() != 1 {
		return OrganizationMember{}, pgx.ErrNoRows
	}
	if err := auditlog.Append(ctx, tx, organizationID, actorID, auditlog.ActionMemberProfileUpdated, "user", userID, map[string]any{
		"changedFields": changedFields,
	}); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationMember{}, err
	}
	return s.GetOrganizationMember(ctx, organizationID, userID)
}

func (s *Service) IssueOrganizationMemberPasswordReset(
	ctx context.Context,
	organizationID, userID, actorID string,
) (MemberPasswordReset, error) {
	token, err := randomToken("pwr_")
	if err != nil {
		return MemberPasswordReset{}, err
	}
	expiresAt := time.Now().UTC().Add(organizationPasswordResetTTL)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MemberPasswordReset{}, err
	}
	defer rollback(ctx, tx)
	if err := ensureOrganizationCanManageMemberAccount(ctx, tx, organizationID, userID, actorID); err != nil {
		return MemberPasswordReset{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_password_reset_tokens
		SET revoked_at = now()
		WHERE user_id = $1 AND consumed_at IS NULL AND revoked_at IS NULL
	`, userID); err != nil {
		return MemberPasswordReset{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE users
		SET password_hash = NULL, credential_version = credential_version + 1, updated_at = now()
		WHERE id = $1 AND status = 'active'
	`, userID)
	if err != nil {
		return MemberPasswordReset{}, err
	}
	if command.RowsAffected() != 1 {
		return MemberPasswordReset{}, pgx.ErrNoRows
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID); err != nil {
		return MemberPasswordReset{}, err
	}
	var resetID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO auth_password_reset_tokens(
			organization_id, user_id, token_hash, expires_at, created_by
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, organizationID, userID, hashRefreshToken(token), expiresAt, actorID).Scan(&resetID); err != nil {
		return MemberPasswordReset{}, err
	}
	if err := auditlog.Append(ctx, tx, organizationID, actorID, auditlog.ActionMemberPasswordResetRequested, "user", userID, map[string]any{
		"resetRequestId": resetID,
		"expiresAt":      expiresAt,
	}); err != nil {
		return MemberPasswordReset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemberPasswordReset{}, err
	}
	return MemberPasswordReset{UserID: userID, ResetToken: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) CompletePasswordReset(ctx context.Context, req CompletePasswordResetRequest, r *http.Request) error {
	tokenHash := hashRefreshToken(req.ResetToken)
	if tokenHash == "" || len(req.Password) < 8 || len(req.Password) > 72 {
		return ErrPasswordResetValidation
	}
	if err := s.checkSecurityRateLimit(ctx, securityActionPasswordReset, tokenHash, r); err != nil {
		return err
	}
	err := s.completePasswordReset(ctx, tokenHash, req.Password)
	if errors.Is(err, ErrPasswordResetInvalid) {
		if failureErr := s.recordSecurityFailure(ctx, securityActionPasswordReset, tokenHash, r); failureErr != nil {
			return failureErr
		}
		return err
	}
	return err
}

func (s *Service) completePasswordReset(ctx context.Context, tokenHash, password string) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var resetID, organizationID, userID string
	err = tx.QueryRow(ctx, `
		SELECT id, organization_id, user_id
		FROM auth_password_reset_tokens
		WHERE token_hash = $1
		  AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		FOR UPDATE
	`, tokenHash).Scan(&resetID, &organizationID, &userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPasswordResetInvalid
		}
		return err
	}
	var accountValid bool
	if err := tx.QueryRow(ctx, `
		SELECT u.status = 'active'
		       AND om.status <> 'removed'
		       AND (
		           SELECT count(*) FROM organization_members memberships
		           WHERE memberships.user_id = u.id AND memberships.status <> 'removed'
		       ) = 1
		FROM users u
		JOIN organization_members om ON om.user_id = u.id AND om.organization_id = $2
		WHERE u.id = $1
		FOR UPDATE OF u, om
	`, userID, organizationID).Scan(&accountValid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPasswordResetInvalid
		}
		return err
	}
	if !accountValid {
		return ErrPasswordResetInvalid
	}
	command, err := tx.Exec(ctx, `
		UPDATE users
		SET password_hash = $2, credential_version = credential_version + 1, updated_at = now()
		WHERE id = $1 AND status = 'active' AND password_hash IS NULL
	`, userID, string(passwordHash))
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrPasswordResetInvalid
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_password_reset_tokens SET consumed_at = now()
		WHERE id = $1 AND consumed_at IS NULL AND revoked_at IS NULL
	`, resetID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID); err != nil {
		return err
	}
	if err := auditlog.Append(ctx, tx, organizationID, "", auditlog.ActionMemberPasswordResetCompleted, "user", userID, map[string]any{
		"resetRequestId": resetID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func normalizeMemberProfileUpdate(req UpdateProfileRequest) (string, string, []string, error) {
	if req.DisplayName == nil && req.AvatarURL == nil {
		return "", "", nil, ErrMemberProfileValidation
	}
	displayName := ""
	avatarURL := ""
	changedFields := make([]string, 0, 2)
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if len([]rune(displayName)) > 100 {
			return "", "", nil, ErrMemberProfileValidation
		}
		changedFields = append(changedFields, "displayName")
	}
	if req.AvatarURL != nil {
		avatarURL = strings.TrimSpace(*req.AvatarURL)
		if len(avatarURL) > 2048 {
			return "", "", nil, ErrMemberProfileValidation
		}
		if avatarURL != "" {
			parsed, err := url.ParseRequestURI(avatarURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return "", "", nil, ErrMemberProfileValidation
			}
		}
		changedFields = append(changedFields, "avatarUrl")
	}
	return displayName, avatarURL, changedFields, nil
}

func ensureOrganizationCanManageMemberAccount(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, userID, actorID string,
) error {
	var memberStatus, userStatus string
	var systemAdministrator bool
	var membershipCount int
	err := tx.QueryRow(ctx, `
		SELECT om.status, u.status, u.is_system_admin,
		       (
		           SELECT count(*) FROM organization_members memberships
		           WHERE memberships.user_id = u.id AND memberships.status <> 'removed'
		       )
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
		FOR UPDATE OF om, u
	`, organizationID, userID).Scan(&memberStatus, &userStatus, &systemAdministrator, &membershipCount)
	if err != nil {
		return err
	}
	if systemAdministrator {
		return ErrMemberAccountProtected
	}
	if memberStatus == "removed" || userStatus != "active" {
		return ErrMemberLifecycle
	}
	if membershipCount != 1 {
		return ErrSharedAccountManagement
	}
	if actorID == userID {
		return nil
	}
	var targetIsOwner, actorIsOwner bool
	if err := tx.QueryRow(ctx, `
		SELECT
			EXISTS(
				SELECT 1 FROM role_bindings rb
				JOIN roles r ON r.id = rb.role_id
				WHERE rb.organization_id = $1 AND rb.subject_type = 'user' AND rb.subject_user_id = $2
				  AND rb.resource_type = 'organization' AND rb.resource_organization_id = $1
				  AND r.role_key IN ('org_owner', 'organization_owner')
				  AND (rb.expires_at IS NULL OR rb.expires_at > now())
			),
			EXISTS(
				SELECT 1 FROM role_bindings rb
				JOIN roles r ON r.id = rb.role_id
				WHERE rb.organization_id = $1 AND rb.subject_type = 'user' AND rb.subject_user_id = $3
				  AND rb.resource_type = 'organization' AND rb.resource_organization_id = $1
				  AND r.role_key IN ('org_owner', 'organization_owner')
				  AND (rb.expires_at IS NULL OR rb.expires_at > now())
			)
	`, organizationID, userID, actorID).Scan(&targetIsOwner, &actorIsOwner); err != nil {
		return err
	}
	if targetIsOwner && !actorIsOwner {
		return ErrMemberAccountProtected
	}
	return nil
}
