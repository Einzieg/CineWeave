package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSystemMemberValidation = errors.New("system member request is invalid")
	ErrSystemMemberConflict   = errors.New("system member already belongs to the organization")
	ErrSystemMemberNotFound   = errors.New("system member account was not found")
)

type CreateSystemOrganizationMemberRequest struct {
	ExistingUserIdentifier string `json:"existingUserIdentifier,omitempty"`
	Email                  string `json:"email,omitempty"`
	Username               string `json:"username,omitempty"`
	Password               string `json:"password,omitempty"`
	DisplayName            string `json:"displayName,omitempty"`
	AvatarURL              string `json:"avatarUrl,omitempty"`
}

type UpdateSystemOrganizationMemberRequest struct {
	Email       *string `json:"email,omitempty"`
	Username    *string `json:"username,omitempty"`
	Password    *string `json:"password,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type normalizedCreateSystemOrganizationMemberRequest struct {
	AttachExisting            bool
	ExistingIdentifier        string
	ExistingIdentifierIsEmail bool
	Email                     string
	Username                  string
	UsernameNormalized        string
	Password                  string
	DisplayName               string
	AvatarURL                 string
}

type normalizedUpdateSystemOrganizationMemberRequest struct {
	SetEmail           bool
	Email              string
	SetUsername        bool
	Username           string
	UsernameNormalized string
	SetPassword        bool
	Password           string
	SetDisplayName     bool
	DisplayName        string
	SetAvatarURL       bool
	AvatarURL          string
	SetStatus          bool
	Status             string
	ChangedFields      []string
}

func (s *Service) ListSystemOrganizationMembers(
	ctx context.Context,
	actorUserID, organizationID, search, status string,
	page, pageSize int,
) (MemberList, error) {
	if err := s.RequireSystemAdministrator(ctx, actorUserID); err != nil {
		return MemberList{}, err
	}
	return s.ListOrganizationMembers(ctx, organizationID, search, status, page, pageSize)
}

func (s *Service) CreateSystemOrganizationMember(
	ctx context.Context,
	actorUserID, organizationID string,
	req CreateSystemOrganizationMemberRequest,
) (OrganizationMember, error) {
	normalized, err := normalizeCreateSystemOrganizationMemberRequest(req)
	if err != nil {
		return OrganizationMember{}, err
	}
	var passwordHash []byte
	if !normalized.AttachExisting {
		passwordHash, err = bcrypt.GenerateFromPassword([]byte(normalized.Password), bcrypt.DefaultCost)
		if err != nil {
			return OrganizationMember{}, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OrganizationMember{}, err
	}
	defer rollback(ctx, tx)
	if err := requireSystemAdministratorTx(ctx, tx, actorUserID); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id = $1 FOR SHARE`, organizationID).Scan(&organizationID); err != nil {
		return OrganizationMember{}, err
	}

	var user UserResponse
	accountCreated := false
	var controlKey *ControlKeySecret
	if normalized.AttachExisting {
		err = tx.QueryRow(ctx, `
			SELECT id, email, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), is_system_admin
			FROM users
			WHERE status = 'active'
			  AND (($2::boolean AND email = $1) OR (NOT $2::boolean AND username_normalized = $1))
			FOR UPDATE
		`, normalized.ExistingIdentifier, normalized.ExistingIdentifierIsEmail).Scan(
			&user.ID,
			&user.Email,
			&user.Username,
			&user.DisplayName,
			&user.AvatarURL,
			&user.SystemAdministrator,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return OrganizationMember{}, ErrSystemMemberNotFound
		}
		if err != nil {
			return OrganizationMember{}, err
		}
	} else {
		err = tx.QueryRow(ctx, `
			INSERT INTO users(email, username, username_normalized, password_hash, display_name, avatar_url)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''))
			RETURNING id, email, username, COALESCE(display_name, ''), COALESCE(avatar_url, ''), is_system_admin
		`, normalized.Email, normalized.Username, normalized.UsernameNormalized, string(passwordHash), normalized.DisplayName, normalized.AvatarURL).Scan(
			&user.ID,
			&user.Email,
			&user.Username,
			&user.DisplayName,
			&user.AvatarURL,
			&user.SystemAdministrator,
		)
		if err != nil {
			return OrganizationMember{}, systemMemberIdentityError(err)
		}
		accountCreated = true
		createdKey, createKeyErr := createControlKeyTx(ctx, tx, user.ID, false)
		if createKeyErr != nil {
			return OrganizationMember{}, createKeyErr
		}
		controlKey = &createdKey
	}

	membershipRestored := false
	var currentStatus string
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
		FOR UPDATE
	`, organizationID, user.ID).Scan(&currentStatus)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_members(organization_id, user_id, status)
			VALUES ($1, $2, 'active')
		`, organizationID, user.ID); err != nil {
			return OrganizationMember{}, err
		}
	case err != nil:
		return OrganizationMember{}, err
	case currentStatus == "disabled" || currentStatus == "removed":
		if _, err := tx.Exec(ctx, `
			UPDATE organization_members
			SET status = 'active',
			    disabled_at = NULL, disabled_by = NULL,
			    removed_at = NULL, removed_by = NULL,
			    authorization_version = authorization_version + 1,
			    updated_at = now()
			WHERE organization_id = $1 AND user_id = $2
		`, organizationID, user.ID); err != nil {
			return OrganizationMember{}, err
		}
		membershipRestored = true
	default:
		return OrganizationMember{}, ErrSystemMemberConflict
	}

	var memberRoleID string
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM roles
		WHERE organization_id IS NULL
		  AND role_key IN ('org_member', 'organization_member')
		  AND scope = 'organization'
		ORDER BY CASE WHEN role_key = 'org_member' THEN 0 ELSE 1 END
		LIMIT 1
	`).Scan(&memberRoleID); err != nil {
		return OrganizationMember{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_organization_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'organization', $1, $4)
		ON CONFLICT DO NOTHING
	`, organizationID, memberRoleID, user.ID, actorUserID); err != nil {
		return OrganizationMember{}, err
	}
	revokedInvitations, err := tx.Exec(ctx, `
		UPDATE organization_invitations
		SET status = 'revoked', updated_at = now()
		WHERE organization_id = $1 AND email = $2 AND status = 'pending'
	`, organizationID, user.Email)
	if err != nil {
		return OrganizationMember{}, err
	}
	if err := auditlog.Append(ctx, tx, organizationID, actorUserID, auditlog.ActionSystemOrganizationMemberCreated, "user", user.ID, map[string]any{
		"accountCreated":     accountCreated,
		"membershipRestored": membershipRestored,
		"revokedInvitations": revokedInvitations.RowsAffected(),
		"baseRoleId":         memberRoleID,
	}); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationMember{}, err
	}
	member, err := s.GetOrganizationMember(ctx, organizationID, user.ID)
	if err != nil {
		return OrganizationMember{}, err
	}
	member.CodexControlKey = controlKey
	return member, nil
}

func (s *Service) UpdateSystemOrganizationMember(
	ctx context.Context,
	actorUserID, organizationID, userID string,
	req UpdateSystemOrganizationMemberRequest,
) (OrganizationMember, error) {
	normalized, err := normalizeUpdateSystemOrganizationMemberRequest(req)
	if err != nil {
		return OrganizationMember{}, err
	}
	var passwordHash []byte
	if normalized.SetPassword {
		passwordHash, err = bcrypt.GenerateFromPassword([]byte(normalized.Password), bcrypt.DefaultCost)
		if err != nil {
			return OrganizationMember{}, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OrganizationMember{}, err
	}
	defer rollback(ctx, tx)
	if err := requireSystemAdministratorTx(ctx, tx, actorUserID); err != nil {
		return OrganizationMember{}, err
	}
	if normalized.SetStatus {
		if _, err := tx.Exec(ctx, `LOCK TABLE organization_members IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return OrganizationMember{}, err
		}
	}
	var currentMemberStatus, userStatus string
	var targetSystemAdministrator bool
	if err := tx.QueryRow(ctx, `
		SELECT om.status, u.status, u.is_system_admin
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
		FOR UPDATE OF om, u
	`, organizationID, userID).Scan(&currentMemberStatus, &userStatus, &targetSystemAdministrator); err != nil {
		return OrganizationMember{}, err
	}
	if targetSystemAdministrator {
		return OrganizationMember{}, ErrMemberAccountProtected
	}
	if currentMemberStatus == "removed" || userStatus != "active" {
		return OrganizationMember{}, ErrMemberLifecycle
	}

	if normalized.SetEmail || normalized.SetUsername || normalized.SetPassword || normalized.SetDisplayName || normalized.SetAvatarURL {
		_, err := tx.Exec(ctx, `
			UPDATE users
			SET email = CASE WHEN $2::boolean THEN $3 ELSE email END,
			    username = CASE WHEN $4::boolean THEN $5 ELSE username END,
			    username_normalized = CASE WHEN $4::boolean THEN $6 ELSE username_normalized END,
			    password_hash = CASE WHEN $7::boolean THEN $8 ELSE password_hash END,
			    credential_version = credential_version + CASE WHEN $7::boolean THEN 1 ELSE 0 END,
			    display_name = CASE WHEN $9::boolean THEN NULLIF($10, '') ELSE display_name END,
			    avatar_url = CASE WHEN $11::boolean THEN NULLIF($12, '') ELSE avatar_url END,
			    updated_at = now()
			WHERE id = $1 AND status = 'active'
		`, userID,
			normalized.SetEmail, normalized.Email,
			normalized.SetUsername, normalized.Username, normalized.UsernameNormalized,
			normalized.SetPassword, string(passwordHash),
			normalized.SetDisplayName, normalized.DisplayName,
			normalized.SetAvatarURL, normalized.AvatarURL,
		)
		if err != nil {
			return OrganizationMember{}, systemMemberIdentityError(err)
		}
	}
	if normalized.SetPassword {
		if _, err := tx.Exec(ctx, `
			UPDATE auth_sessions
			SET revoked_at = now()
			WHERE user_id = $1 AND revoked_at IS NULL
		`, userID); err != nil {
			return OrganizationMember{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE auth_password_reset_tokens
			SET revoked_at = now()
			WHERE user_id = $1 AND consumed_at IS NULL AND revoked_at IS NULL
		`, userID); err != nil {
			return OrganizationMember{}, err
		}
	}

	statusChanged := normalized.SetStatus && normalized.Status != currentMemberStatus
	if statusChanged {
		if currentMemberStatus != "active" && currentMemberStatus != "disabled" {
			return OrganizationMember{}, ErrMemberLifecycle
		}
		if normalized.Status == "disabled" {
			if currentMemberStatus != "active" {
				return OrganizationMember{}, ErrMemberLifecycle
			}
			if err := ensureNotLastDirectOwner(ctx, tx, organizationID, userID); err != nil {
				return OrganizationMember{}, err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE organization_members
				SET status = 'disabled', disabled_at = now(), disabled_by = $3,
				    authorization_version = authorization_version + 1, updated_at = now()
				WHERE organization_id = $1 AND user_id = $2
			`, organizationID, userID, actorUserID); err != nil {
				return OrganizationMember{}, err
			}
		} else {
			if currentMemberStatus != "disabled" {
				return OrganizationMember{}, ErrMemberLifecycle
			}
			if _, err := tx.Exec(ctx, `
				UPDATE organization_members
				SET status = 'active', disabled_at = NULL, disabled_by = NULL,
				    authorization_version = authorization_version + 1, updated_at = now()
				WHERE organization_id = $1 AND user_id = $2
			`, organizationID, userID); err != nil {
				return OrganizationMember{}, err
			}
		}
		if err := revokeMembershipAuthorizationArtifacts(ctx, tx, organizationID, userID); err != nil {
			return OrganizationMember{}, err
		}
	}

	if err := auditlog.Append(ctx, tx, organizationID, actorUserID, auditlog.ActionSystemOrganizationMemberUpdated, "user", userID, map[string]any{
		"changedFields":  normalized.ChangedFields,
		"previousStatus": currentMemberStatus,
		"status":         normalized.Status,
	}); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationMember{}, err
	}
	return s.GetOrganizationMember(ctx, organizationID, userID)
}

func normalizeCreateSystemOrganizationMemberRequest(
	req CreateSystemOrganizationMemberRequest,
) (normalizedCreateSystemOrganizationMemberRequest, error) {
	existingIdentifier := strings.TrimSpace(req.ExistingUserIdentifier)
	if existingIdentifier != "" {
		if strings.TrimSpace(req.Email) != "" || strings.TrimSpace(req.Username) != "" || req.Password != "" ||
			strings.TrimSpace(req.DisplayName) != "" || strings.TrimSpace(req.AvatarURL) != "" {
			return normalizedCreateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		identifier, isEmail := NormalizeLoginIdentifier(existingIdentifier)
		if identifier == "" || len(identifier) > 320 {
			return normalizedCreateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		return normalizedCreateSystemOrganizationMemberRequest{
			AttachExisting:            true,
			ExistingIdentifier:        identifier,
			ExistingIdentifierIsEmail: isEmail,
		}, nil
	}

	email := normalizeEmail(req.Email)
	if email == "" || len(email) > 320 || !strings.Contains(email, "@") || len(req.Password) < 8 || len(req.Password) > 72 {
		return normalizedCreateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
	}
	username, usernameNormalized, err := NormalizeUsername(req.Username)
	if err != nil {
		return normalizedCreateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}
	if len([]rune(displayName)) > 100 {
		return normalizedCreateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
	}
	avatarURL, err := normalizeSystemMemberAvatarURL(req.AvatarURL)
	if err != nil {
		return normalizedCreateSystemOrganizationMemberRequest{}, err
	}
	return normalizedCreateSystemOrganizationMemberRequest{
		Email:              email,
		Username:           username,
		UsernameNormalized: usernameNormalized,
		Password:           req.Password,
		DisplayName:        displayName,
		AvatarURL:          avatarURL,
	}, nil
}

func normalizeUpdateSystemOrganizationMemberRequest(
	req UpdateSystemOrganizationMemberRequest,
) (normalizedUpdateSystemOrganizationMemberRequest, error) {
	if req.Email == nil && req.Username == nil && req.Password == nil && req.DisplayName == nil && req.AvatarURL == nil && req.Status == nil {
		return normalizedUpdateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
	}
	normalized := normalizedUpdateSystemOrganizationMemberRequest{
		ChangedFields: make([]string, 0, 6),
	}
	if req.Email != nil {
		normalized.SetEmail = true
		normalized.Email = normalizeEmail(*req.Email)
		if normalized.Email == "" || len(normalized.Email) > 320 || !strings.Contains(normalized.Email, "@") {
			return normalizedUpdateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		normalized.ChangedFields = append(normalized.ChangedFields, "email")
	}
	if req.Username != nil {
		username, usernameNormalized, err := NormalizeUsername(*req.Username)
		if err != nil {
			return normalizedUpdateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		normalized.SetUsername = true
		normalized.Username = username
		normalized.UsernameNormalized = usernameNormalized
		normalized.ChangedFields = append(normalized.ChangedFields, "username")
	}
	if req.Password != nil {
		if len(*req.Password) < 8 || len(*req.Password) > 72 {
			return normalizedUpdateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		normalized.SetPassword = true
		normalized.Password = *req.Password
		normalized.ChangedFields = append(normalized.ChangedFields, "password")
	}
	if req.DisplayName != nil {
		normalized.SetDisplayName = true
		normalized.DisplayName = strings.TrimSpace(*req.DisplayName)
		if len([]rune(normalized.DisplayName)) > 100 {
			return normalizedUpdateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		normalized.ChangedFields = append(normalized.ChangedFields, "displayName")
	}
	if req.AvatarURL != nil {
		avatarURL, err := normalizeSystemMemberAvatarURL(*req.AvatarURL)
		if err != nil {
			return normalizedUpdateSystemOrganizationMemberRequest{}, err
		}
		normalized.SetAvatarURL = true
		normalized.AvatarURL = avatarURL
		normalized.ChangedFields = append(normalized.ChangedFields, "avatarUrl")
	}
	if req.Status != nil {
		normalized.SetStatus = true
		normalized.Status = strings.TrimSpace(*req.Status)
		if normalized.Status != "active" && normalized.Status != "disabled" {
			return normalizedUpdateSystemOrganizationMemberRequest{}, ErrSystemMemberValidation
		}
		normalized.ChangedFields = append(normalized.ChangedFields, "status")
	}
	return normalized, nil
}

func normalizeSystemMemberAvatarURL(value string) (string, error) {
	avatarURL := strings.TrimSpace(value)
	if len(avatarURL) > 2048 {
		return "", ErrSystemMemberValidation
	}
	if avatarURL == "" {
		return "", nil
	}
	parsed, err := url.ParseRequestURI(avatarURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", ErrSystemMemberValidation
	}
	return avatarURL, nil
}

func requireSystemAdministratorTx(ctx context.Context, tx pgx.Tx, actorUserID string) error {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT is_system_admin
		FROM users
		WHERE id = $1 AND status = 'active'
		FOR SHARE
	`, strings.TrimSpace(actorUserID)).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return ErrSystemAdministratorRequired
	}
	return err
}

func systemMemberIdentityError(err error) error {
	if isUniqueConstraint(err, "users_username_normalized_unique") {
		return ErrUsernameExists
	}
	if isUniqueViolation(err) {
		return ErrEmailExists
	}
	return err
}
