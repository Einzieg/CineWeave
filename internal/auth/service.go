package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auditlog"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials         = errors.New("invalid credentials")
	ErrEmailExists                = errors.New("email already exists")
	ErrUsernameExists             = errors.New("username already exists")
	ErrRegistrationUnavailable    = errors.New("registration cannot be completed")
	ErrInvalidUsername            = errors.New("invalid username")
	ErrRateLimited                = errors.New("authentication attempts are rate limited")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrForbidden                  = errors.New("forbidden")
	ErrNoActiveOrganization       = errors.New("no active organization")
	ErrOrganizationSelection      = errors.New("organization selection is invalid")
	ErrProfileValidation          = errors.New("profile request is invalid")
	ErrSetupComplete              = errors.New("setup already completed")
	ErrPublicRegistrationDisabled = errors.New("public registration disabled")
)

var dummyPasswordHash = func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("cineweave-auth-timing-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return hash
}()

type Service struct {
	db             *pgxpool.Pool
	jwtSecret      []byte
	accessTTL      time.Duration
	refreshTTL     time.Duration
	trustedProxies trustedProxySet
}

type Principal struct {
	UserID                         string
	OrganizationID                 string
	CredentialVersion              int
	MembershipAuthorizationVersion int64
}

type RegisterRequest struct {
	Email            string `json:"email"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	DisplayName      string `json:"displayName"`
	OrganizationName string `json:"organizationName"`
	WorkspaceName    string `json:"workspaceName"`
}

type LoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type SelectOrganizationRequest struct {
	OrganizationSelectionToken string `json:"organizationSelectionToken"`
	OrganizationID             string `json:"organizationId"`
}

type SwitchOrganizationRequest struct {
	RefreshToken   string `json:"refreshToken"`
	OrganizationID string `json:"organizationId"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type TokenResponse struct {
	AccessToken    string       `json:"accessToken"`
	ExpiresIn      int64        `json:"expiresIn"`
	RefreshToken   string       `json:"refreshToken"`
	User           UserResponse `json:"user"`
	OrganizationID string       `json:"organizationId,omitempty"`
	WorkspaceID    string       `json:"workspaceId,omitempty"`
}

type OrganizationChoice struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	AuthorizationVersion int64  `json:"-"`
}

type LoginResponse struct {
	*TokenResponse
	RequiresOrganizationSelection bool                 `json:"requiresOrganizationSelection"`
	OrganizationSelectionToken    string               `json:"organizationSelectionToken,omitempty"`
	Organizations                 []OrganizationChoice `json:"organizations,omitempty"`
}

type UserResponse struct {
	ID                  string `json:"id"`
	Email               string `json:"email"`
	Username            string `json:"username,omitempty"`
	DisplayName         string `json:"displayName,omitempty"`
	AvatarURL           string `json:"avatarUrl,omitempty"`
	SystemAdministrator bool   `json:"systemAdministrator,omitempty"`
}

type UpdateProfileRequest struct {
	DisplayName *string `json:"displayName"`
	AvatarURL   *string `json:"avatarUrl"`
}

type claims struct {
	OrganizationID                 string `json:"organizationId,omitempty"`
	CredentialVersion              int    `json:"credentialVersion"`
	MembershipAuthorizationVersion int64  `json:"membershipAuthorizationVersion"`
	jwt.RegisteredClaims
}

type organizationSelectionClaims struct {
	Purpose                           string           `json:"purpose"`
	OrganizationIDs                   []string         `json:"organizationIds"`
	OrganizationAuthorizationVersions map[string]int64 `json:"organizationAuthorizationVersions"`
	jwt.RegisteredClaims
}

func NewService(pool *pgxpool.Pool, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	if jwtSecret == "" {
		jwtSecret = "dev-insecure-cineweave-secret"
	}
	return &Service{
		db:         pool,
		jwtSecret:  []byte(jwtSecret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest, r *http.Request) (TokenResponse, error) {
	subject := registrationRateSubject(req.Email, req.Username)
	if err := s.checkSecurityRateLimit(ctx, securityActionRegister, subject, r); err != nil {
		return TokenResponse{}, err
	}
	response, err := s.createUserOrganizationSession(ctx, req, r, false)
	if errors.Is(err, ErrEmailExists) || errors.Is(err, ErrUsernameExists) {
		err = ErrRegistrationUnavailable
	}
	if isRegistrationFailure(err) {
		if recordErr := s.recordSecurityFailure(ctx, securityActionRegister, subject, r); recordErr != nil {
			return TokenResponse{}, recordErr
		}
	}
	if err == nil {
		if clearErr := s.clearSecurityIdentityFailures(ctx, securityActionRegister, subject); clearErr != nil {
			return TokenResponse{}, clearErr
		}
	}
	return response, err
}

func (s *Service) Setup(ctx context.Context, req RegisterRequest, r *http.Request) (TokenResponse, error) {
	return s.createUserOrganizationSession(ctx, req, r, true)
}

func (s *Service) createUserOrganizationSession(ctx context.Context, req RegisterRequest, r *http.Request, requireNoUsers bool) (TokenResponse, error) {
	email := normalizeEmail(req.Email)
	if email == "" || len(req.Password) < 8 {
		return TokenResponse{}, ErrInvalidCredentials
	}
	username, usernameNormalized, err := NormalizeUsername(req.Username)
	if err != nil {
		return TokenResponse{}, err
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = email
	}
	orgName := strings.TrimSpace(req.OrganizationName)
	if orgName == "" {
		orgName = displayName + "'s Organization"
	}
	workspaceName := strings.TrimSpace(req.WorkspaceName)
	if workspaceName == "" {
		workspaceName = "Default Workspace"
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("hash password: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)

	if requireNoUsers {
		if _, err := tx.Exec(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
			return TokenResponse{}, err
		}
		var userCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&userCount); err != nil {
			return TokenResponse{}, err
		}
		if userCount > 0 {
			return TokenResponse{}, ErrSetupComplete
		}
	}

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users(email, username, username_normalized, password_hash, display_name, is_system_admin)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, email, username, usernameNormalized, string(passwordHash), displayName, requireNoUsers).Scan(&userID)
	if err != nil {
		if isUniqueConstraint(err, "users_username_normalized_unique") {
			return TokenResponse{}, ErrUsernameExists
		}
		if isUniqueViolation(err) {
			return TokenResponse{}, ErrEmailExists
		}
		return TokenResponse{}, err
	}

	orgID, workspaceID, err := createOrganizationForUser(ctx, tx, userID, userID, orgName, workspaceName)
	if err != nil {
		return TokenResponse{}, err
	}

	token, refresh, err := s.createSession(ctx, tx, userID, orgID, r)
	if err != nil {
		return TokenResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}

	return TokenResponse{
		AccessToken:    token,
		ExpiresIn:      int64(s.accessTTL.Seconds()),
		RefreshToken:   refresh,
		OrganizationID: orgID,
		WorkspaceID:    workspaceID,
		User: UserResponse{
			ID:                  userID,
			Email:               email,
			Username:            username,
			DisplayName:         displayName,
			SystemAdministrator: requireNoUsers,
		},
	}, nil
}

func (s *Service) Login(ctx context.Context, req LoginRequest, r *http.Request) (LoginResponse, error) {
	identifier, isEmail := NormalizeLoginIdentifier(req.Identifier)
	subject := identifier
	if subject == "" {
		subject = strings.ToLower(strings.TrimSpace(req.Identifier))
	}
	if err := s.checkSecurityRateLimit(ctx, securityActionLogin, subject, r); err != nil {
		return LoginResponse{}, err
	}
	if identifier == "" || req.Password == "" {
		if req.Password != "" {
			_ = passwordMatches("", req.Password)
		}
		return LoginResponse{}, s.recordLoginFailure(ctx, subject, r)
	}
	var userID, email, username, passwordHash, displayName, avatarURL, status string
	err := s.db.QueryRow(ctx, `
		SELECT id, email, COALESCE(username, ''), COALESCE(password_hash, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), status
		FROM users
		WHERE ($2::boolean AND email = $1)
		   OR (NOT $2::boolean AND username_normalized = $1)
	`, identifier, isEmail).Scan(&userID, &email, &username, &passwordHash, &displayName, &avatarURL, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = passwordMatches("", req.Password)
			return LoginResponse{}, s.recordLoginFailure(ctx, subject, r)
		}
		return LoginResponse{}, err
	}
	if !passwordMatches(passwordHash, req.Password) || status != "active" {
		return LoginResponse{}, s.recordLoginFailure(ctx, subject, r)
	}
	if err := s.clearSecurityIdentityFailures(ctx, securityActionLogin, subject); err != nil {
		return LoginResponse{}, err
	}

	organizations, err := s.activeOrganizations(ctx, userID)
	if err != nil {
		return LoginResponse{}, err
	}
	if len(organizations) == 0 {
		return LoginResponse{}, ErrNoActiveOrganization
	}
	user := UserResponse{ID: userID, Email: email, Username: username, DisplayName: displayName, AvatarURL: avatarURL}
	if len(organizations) > 1 {
		selectionToken, err := s.createOrganizationSelectionToken(ctx, userID, organizations)
		if err != nil {
			return LoginResponse{}, err
		}
		return LoginResponse{
			RequiresOrganizationSelection: true,
			OrganizationSelectionToken:    selectionToken,
			Organizations:                 organizations,
		}, nil
	}
	response, err := s.createCommittedSession(ctx, user, organizations[0].ID, r)
	if err != nil {
		return LoginResponse{}, err
	}
	return LoginResponse{TokenResponse: &response}, nil
}

func (s *Service) recordLoginFailure(ctx context.Context, subject string, r *http.Request) error {
	if err := s.recordSecurityFailure(ctx, securityActionLogin, subject, r); err != nil {
		return err
	}
	return ErrInvalidCredentials
}

func isRegistrationFailure(err error) bool {
	return errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidUsername) || errors.Is(err, ErrRegistrationUnavailable)
}

func passwordMatches(passwordHash, password string) bool {
	storedHash := []byte(passwordHash)
	hasStoredPassword := strings.HasPrefix(passwordHash, "$2")
	if !hasStoredPassword {
		storedHash = dummyPasswordHash
	}
	return bcrypt.CompareHashAndPassword(storedHash, []byte(password)) == nil && hasStoredPassword
}

func (s *Service) Refresh(ctx context.Context, req RefreshRequest, r *http.Request) (TokenResponse, error) {
	hash := hashRefreshToken(req.RefreshToken)
	if hash == "" {
		return TokenResponse{}, ErrUnauthorized
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)

	var sessionID, userID, email, username, displayName, avatarURL string
	var systemAdministrator bool
	var orgID *string
	err = tx.QueryRow(ctx, `
		SELECT s.id, u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''), u.is_system_admin, s.organization_id
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.refresh_token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
		  AND u.status = 'active'
		  AND (
		      (s.organization_id IS NULL AND s.membership_authorization_version IS NULL)
		      OR EXISTS (
		          SELECT 1 FROM organization_members om
		          WHERE om.organization_id = s.organization_id
		            AND om.user_id = s.user_id
		            AND om.status = 'active'
		            AND om.authorization_version = s.membership_authorization_version
		      )
		  )
		FOR UPDATE OF s
	`, hash).Scan(&sessionID, &userID, &email, &username, &displayName, &avatarURL, &systemAdministrator, &orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenResponse{}, ErrUnauthorized
		}
		return TokenResponse{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at = now() WHERE id = $1`, sessionID); err != nil {
		return TokenResponse{}, err
	}

	orgValue := ""
	if orgID != nil {
		orgValue = *orgID
	}
	workspaceID, err := s.defaultWorkspace(ctx, orgValue)
	if err != nil {
		return TokenResponse{}, err
	}
	token, refresh, err := s.createSession(ctx, tx, userID, orgValue, r)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}

	return TokenResponse{
		AccessToken:    token,
		ExpiresIn:      int64(s.accessTTL.Seconds()),
		RefreshToken:   refresh,
		OrganizationID: orgValue,
		WorkspaceID:    workspaceID,
		User: UserResponse{
			ID:                  userID,
			Email:               email,
			Username:            username,
			DisplayName:         displayName,
			AvatarURL:           avatarURL,
			SystemAdministrator: systemAdministrator,
		},
	}, nil
}

func (s *Service) SelectOrganization(ctx context.Context, req SelectOrganizationRequest, r *http.Request) (TokenResponse, error) {
	claims, err := s.parseOrganizationSelectionToken(req.OrganizationSelectionToken)
	targetOrganizationID := strings.TrimSpace(req.OrganizationID)
	expectedAuthorizationVersion, versionAllowed := claims.OrganizationAuthorizationVersions[targetOrganizationID]
	if err != nil || !containsString(claims.OrganizationIDs, targetOrganizationID) || !versionAllowed || expectedAuthorizationVersion <= 0 {
		return TokenResponse{}, ErrOrganizationSelection
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)

	var user UserResponse
	var currentAuthorizationVersion int64
	var nonceAuthorizationVersionsJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''),
		       om.authorization_version, n.organization_authorization_versions
		FROM auth_organization_selection_nonces n
		JOIN users u ON u.id = n.user_id
		JOIN organization_members om
		  ON om.organization_id = $3 AND om.user_id = u.id AND om.status = 'active'
		WHERE n.user_id = $1
		  AND n.nonce_hash = $2
		  AND n.consumed_at IS NULL
		  AND n.expires_at > now()
		  AND $3 = ANY(n.organization_ids)
		  AND u.status = 'active'
		FOR UPDATE OF n, om
	`, claims.Subject, hashSelectionNonce(claims.ID), targetOrganizationID).Scan(
		&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.AvatarURL,
		&currentAuthorizationVersion, &nonceAuthorizationVersionsJSON,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenResponse{}, ErrOrganizationSelection
		}
		return TokenResponse{}, err
	}
	var nonceAuthorizationVersions map[string]int64
	if err := json.Unmarshal(nonceAuthorizationVersionsJSON, &nonceAuthorizationVersions); err != nil ||
		!sameAuthorizationVersions(claims.OrganizationAuthorizationVersions, nonceAuthorizationVersions) ||
		currentAuthorizationVersion != expectedAuthorizationVersion ||
		nonceAuthorizationVersions[targetOrganizationID] != expectedAuthorizationVersion {
		return TokenResponse{}, ErrOrganizationSelection
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_organization_selection_nonces
		SET consumed_at = now()
		WHERE nonce_hash = $1
	`, hashSelectionNonce(claims.ID)); err != nil {
		return TokenResponse{}, err
	}
	response, err := s.createSessionResponse(ctx, tx, user, targetOrganizationID, r)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) SwitchOrganization(ctx context.Context, principal Principal, req SwitchOrganizationRequest, r *http.Request) (TokenResponse, error) {
	hash := hashRefreshToken(req.RefreshToken)
	targetOrganizationID := strings.TrimSpace(req.OrganizationID)
	if hash == "" || targetOrganizationID == "" {
		return TokenResponse{}, ErrUnauthorized
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)

	var sessionID string
	var user UserResponse
	err = tx.QueryRow(ctx, `
		SELECT s.id, u.id, u.email, COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.avatar_url, '')
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		JOIN organization_members source_membership
		  ON source_membership.organization_id = s.organization_id
		 AND source_membership.user_id = s.user_id
		 AND source_membership.status = 'active'
		 AND source_membership.authorization_version = s.membership_authorization_version
		WHERE s.refresh_token_hash = $1
		  AND s.user_id = $2
		  AND s.organization_id = $3
		  AND source_membership.authorization_version = $4
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
		  AND u.status = 'active'
		FOR UPDATE OF s
	`, hash, principal.UserID, principal.OrganizationID, principal.MembershipAuthorizationVersion).Scan(
		&sessionID, &user.ID, &user.Email, &user.Username, &user.DisplayName, &user.AvatarURL,
	)
	if err != nil {
		return TokenResponse{}, ErrUnauthorized
	}
	var targetAuthorizationVersion int64
	if err := tx.QueryRow(ctx, `
		SELECT authorization_version
		FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND status = 'active'
		FOR SHARE
	`, targetOrganizationID, principal.UserID).Scan(&targetAuthorizationVersion); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return TokenResponse{}, err
		}
		return TokenResponse{}, ErrForbidden
	}
	if targetAuthorizationVersion <= 0 {
		return TokenResponse{}, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at = now() WHERE id = $1`, sessionID); err != nil {
		return TokenResponse{}, err
	}
	response, err := s.createSessionResponse(ctx, tx, user, targetOrganizationID, r)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) Logout(ctx context.Context, req RefreshRequest) error {
	hash := hashRefreshToken(req.RefreshToken)
	if hash == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `UPDATE auth_sessions SET revoked_at = now() WHERE refresh_token_hash = $1`, hash)
	return err
}

func (s *Service) ParseBearer(header string) (Principal, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return Principal{}, ErrUnauthorized
	}
	tokenString := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	parsed := &claims{}
	token, err := jwt.ParseWithClaims(tokenString, parsed, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrUnauthorized
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid || parsed.Subject == "" {
		return Principal{}, ErrUnauthorized
	}
	return Principal{
		UserID:                         parsed.Subject,
		OrganizationID:                 parsed.OrganizationID,
		CredentialVersion:              parsed.CredentialVersion,
		MembershipAuthorizationVersion: parsed.MembershipAuthorizationVersion,
	}, nil
}

func (s *Service) ValidatePrincipalActive(ctx context.Context, principal Principal) error {
	if strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.OrganizationID) == "" {
		return ErrUnauthorized
	}
	var credentialsActive, membershipActive bool
	if err := s.db.QueryRow(ctx, `
		SELECT u.status = 'active' AND u.credential_version = $3,
		       EXISTS(
			       SELECT 1
			       FROM organization_members om
			       WHERE om.user_id = u.id
			         AND om.organization_id = $2
			         AND om.status = 'active'
			         AND om.authorization_version = $4
		       )
		FROM users u
		WHERE u.id = $1
	`, principal.UserID, principal.OrganizationID, principal.CredentialVersion, principal.MembershipAuthorizationVersion).Scan(&credentialsActive, &membershipActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return err
	}
	if !credentialsActive {
		return ErrUnauthorized
	}
	if !membershipActive {
		return ErrForbidden
	}
	return nil
}

func (s *Service) Me(ctx context.Context, principal Principal) (UserResponse, error) {
	var user UserResponse
	err := s.db.QueryRow(ctx, `
		SELECT id, email, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), is_system_admin
		FROM users
		WHERE id = $1 AND status = 'active'
	`, principal.UserID).Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.AvatarURL, &user.SystemAdministrator)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserResponse{}, ErrUnauthorized
		}
		return UserResponse{}, err
	}
	return user, nil
}

func (s *Service) Organizations(ctx context.Context, userID string) ([]OrganizationChoice, error) {
	return s.activeOrganizations(ctx, userID)
}

func (s *Service) SetInitialUsername(ctx context.Context, principal Principal, value string) (UserResponse, error) {
	username, normalized, err := NormalizeUsername(value)
	if err != nil {
		return UserResponse{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UserResponse{}, err
	}
	defer rollback(ctx, tx)
	var user UserResponse
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET username = $2, username_normalized = $3, updated_at = now()
		WHERE id = $1 AND status = 'active' AND username_normalized IS NULL
		RETURNING id, email, username, COALESCE(display_name, ''), COALESCE(avatar_url, ''), is_system_admin
	`, principal.UserID, username, normalized).Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.AvatarURL, &user.SystemAdministrator)
	if err != nil {
		if isUniqueConstraint(err, "users_username_normalized_unique") {
			return UserResponse{}, ErrUsernameExists
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return UserResponse{}, ErrForbidden
		}
		return UserResponse{}, err
	}
	if err := auditlog.Append(ctx, tx, principal.OrganizationID, principal.UserID, auditlog.ActionUsernameSet, "user", principal.UserID, map[string]any{
		"username": username,
	}); err != nil {
		return UserResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserResponse{}, err
	}
	return user, nil
}

func (s *Service) UpdateProfile(ctx context.Context, principal Principal, req UpdateProfileRequest) (UserResponse, error) {
	if req.DisplayName == nil && req.AvatarURL == nil {
		return UserResponse{}, ErrProfileValidation
	}
	displayName := ""
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if len([]rune(displayName)) > 100 {
			return UserResponse{}, ErrProfileValidation
		}
	}
	avatarURL := ""
	if req.AvatarURL != nil {
		avatarURL = strings.TrimSpace(*req.AvatarURL)
		if len(avatarURL) > 2048 {
			return UserResponse{}, ErrProfileValidation
		}
		if avatarURL != "" {
			parsed, err := url.ParseRequestURI(avatarURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return UserResponse{}, ErrProfileValidation
			}
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UserResponse{}, err
	}
	defer rollback(ctx, tx)
	var user UserResponse
	err = tx.QueryRow(ctx, `
		UPDATE users
		SET display_name = CASE WHEN $2::boolean THEN NULLIF($3, '') ELSE display_name END,
		    avatar_url = CASE WHEN $4::boolean THEN NULLIF($5, '') ELSE avatar_url END,
		    updated_at = now()
		WHERE id = $1 AND status = 'active'
		RETURNING id, email, COALESCE(username, ''), COALESCE(display_name, ''), COALESCE(avatar_url, ''), is_system_admin
	`, principal.UserID, req.DisplayName != nil, displayName, req.AvatarURL != nil, avatarURL).Scan(
		&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.AvatarURL, &user.SystemAdministrator,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserResponse{}, ErrUnauthorized
		}
		return UserResponse{}, err
	}
	changedFields := make([]string, 0, 2)
	if req.DisplayName != nil {
		changedFields = append(changedFields, "displayName")
	}
	if req.AvatarURL != nil {
		changedFields = append(changedFields, "avatarUrl")
	}
	if err := auditlog.Append(ctx, tx, principal.OrganizationID, principal.UserID, auditlog.ActionUserProfileUpdated, "user", principal.UserID, map[string]any{
		"changedFields": changedFields,
	}); err != nil {
		return UserResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserResponse{}, err
	}
	return user, nil
}

func (s *Service) createCommittedSession(ctx context.Context, user UserResponse, orgID string, r *http.Request) (TokenResponse, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)
	response, err := s.createSessionResponse(ctx, tx, user, orgID, r)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenResponse{}, err
	}
	return response, nil
}

func (s *Service) createSessionResponse(ctx context.Context, tx pgx.Tx, user UserResponse, orgID string, r *http.Request) (TokenResponse, error) {
	if err := tx.QueryRow(ctx, `
		SELECT is_system_admin
		FROM users
		WHERE id = $1 AND status = 'active'
	`, user.ID).Scan(&user.SystemAdministrator); err != nil {
		return TokenResponse{}, err
	}
	var workspaceID string
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM workspaces
		WHERE organization_id = $1
		ORDER BY created_at
		LIMIT 1
	`, orgID).Scan(&workspaceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return TokenResponse{}, err
	}
	accessToken, refreshToken, err := s.createSession(ctx, tx, user.ID, orgID, r)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{
		AccessToken:    accessToken,
		ExpiresIn:      int64(s.accessTTL.Seconds()),
		RefreshToken:   refreshToken,
		OrganizationID: orgID,
		WorkspaceID:    workspaceID,
		User:           user,
	}, nil
}

func (s *Service) activeOrganizations(ctx context.Context, userID string) ([]OrganizationChoice, error) {
	rows, err := s.db.Query(ctx, `
		SELECT o.id, o.name, om.authorization_version
		FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id
		WHERE om.user_id = $1 AND om.status = 'active'
		ORDER BY o.created_at, o.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	organizations := make([]OrganizationChoice, 0)
	for rows.Next() {
		var item OrganizationChoice
		if err := rows.Scan(&item.ID, &item.Name, &item.AuthorizationVersion); err != nil {
			return nil, err
		}
		organizations = append(organizations, item)
	}
	return organizations, rows.Err()
}

func (s *Service) createOrganizationSelectionToken(ctx context.Context, userID string, organizations []OrganizationChoice) (string, error) {
	organizationIDs := make([]string, 0, len(organizations))
	authorizationVersions := make(map[string]int64, len(organizations))
	for _, organization := range organizations {
		if organization.AuthorizationVersion <= 0 {
			return "", ErrOrganizationSelection
		}
		organizationIDs = append(organizationIDs, organization.ID)
		authorizationVersions[organization.ID] = organization.AuthorizationVersion
	}
	nonce, err := randomToken("osn_")
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(10 * time.Minute)
	claims := organizationSelectionClaims{
		Purpose:                           "organization_selection",
		OrganizationIDs:                   organizationIDs,
		OrganizationAuthorizationVersions: authorizationVersions,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{"organization-selection"},
			Subject:   userID,
			ID:        nonce,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}
	authorizationVersionsJSON, err := json.Marshal(authorizationVersions)
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO auth_organization_selection_nonces(
			user_id, nonce_hash, organization_ids, organization_authorization_versions, expires_at
		)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, hashSelectionNonce(nonce), organizationIDs, authorizationVersionsJSON, expiresAt); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) parseOrganizationSelectionToken(value string) (organizationSelectionClaims, error) {
	parsed := &organizationSelectionClaims{}
	token, err := jwt.ParseWithClaims(strings.TrimSpace(value), parsed, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrOrganizationSelection
		}
		return s.jwtSecret, nil
	}, jwt.WithAudience("organization-selection"), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid || parsed.Subject == "" || parsed.ID == "" || parsed.Purpose != "organization_selection" ||
		len(parsed.OrganizationIDs) < 2 || len(parsed.OrganizationAuthorizationVersions) != len(parsed.OrganizationIDs) {
		return organizationSelectionClaims{}, ErrOrganizationSelection
	}
	for _, organizationID := range parsed.OrganizationIDs {
		if parsed.OrganizationAuthorizationVersions[organizationID] <= 0 {
			return organizationSelectionClaims{}, ErrOrganizationSelection
		}
	}
	return *parsed, nil
}

func hashSelectionNonce(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameAuthorizationVersions(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for organizationID, version := range left {
		if version <= 0 || right[organizationID] != version {
			return false
		}
	}
	return true
}

func (s *Service) createSession(ctx context.Context, tx pgx.Tx, userID, orgID string, r *http.Request) (string, string, error) {
	var credentialVersion int
	if err := tx.QueryRow(ctx, `
		SELECT credential_version
		FROM users
		WHERE id = $1 AND status = 'active'
	`, userID).Scan(&credentialVersion); err != nil {
		return "", "", err
	}
	var membershipAuthorizationVersion int64
	if orgID != "" {
		if err := tx.QueryRow(ctx, `
			SELECT authorization_version
			FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND status = 'active'
			FOR SHARE
		`, orgID, userID).Scan(&membershipAuthorizationVersion); err != nil {
			return "", "", err
		}
	}
	accessToken, err := s.accessToken(userID, orgID, credentialVersion, membershipAuthorizationVersion)
	if err != nil {
		return "", "", err
	}
	refreshToken, err := randomToken("rt_")
	if err != nil {
		return "", "", err
	}

	var orgValue any
	if orgID != "" {
		orgValue = orgID
	}
	expiresAt := time.Now().UTC().Add(s.refreshTTL)
	_, err = tx.Exec(ctx, `
		INSERT INTO auth_sessions(
			user_id, organization_id, membership_authorization_version,
			refresh_token_hash, user_agent, ip_address, expires_at
		)
		VALUES ($1, $2, NULLIF($3, 0), $4, $5, $6, $7)
	`, userID, orgValue, membershipAuthorizationVersion, hashRefreshToken(refreshToken), r.UserAgent(), s.clientIP(r), expiresAt)
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func (s *Service) accessToken(userID, orgID string, credentialVersion int, membershipAuthorizationVersion int64) (string, error) {
	now := time.Now().UTC()
	claims := claims{
		OrganizationID:                 orgID,
		CredentialVersion:              credentialVersion,
		MembershipAuthorizationVersion: membershipAuthorizationVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *Service) defaultOrganization(ctx context.Context, userID string) (string, error) {
	var orgID string
	err := s.db.QueryRow(ctx, `
		SELECT organization_id
		FROM organization_members
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at
		LIMIT 1
	`, userID).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return orgID, err
}

func (s *Service) defaultWorkspace(ctx context.Context, orgID string) (string, error) {
	if strings.TrimSpace(orgID) == "" {
		return "", nil
	}
	var workspaceID string
	err := s.db.QueryRow(ctx, `
		SELECT id
		FROM workspaces
		WHERE organization_id = $1
		ORDER BY created_at
		LIMIT 1
	`, orgID).Scan(&workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return workspaceID, err
}

func createOrganizationForUser(ctx context.Context, tx pgx.Tx, ownerUserID, createdByUserID, name, workspaceName string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Organization"
	}
	workspaceName = strings.TrimSpace(workspaceName)
	if workspaceName == "" {
		workspaceName = "Default Workspace"
	}
	slug, err := uniqueSlug(name)
	if err != nil {
		return "", "", err
	}

	var orgID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations(name, slug)
		VALUES ($1, $2)
		RETURNING id
	`, name, slug).Scan(&orgID); err != nil {
		return "", "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members(organization_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, orgID, ownerUserID); err != nil {
		return "", "", err
	}

	var workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, $2)
		RETURNING id
	`, orgID, workspaceName).Scan(&workspaceID); err != nil {
		return "", "", err
	}

	var ownerRoleID string
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM roles
		WHERE organization_id IS NULL AND role_key IN ('org_owner', 'organization_owner') AND scope = 'organization'
		ORDER BY CASE WHEN role_key = 'org_owner' THEN 0 ELSE 1 END
		LIMIT 1
	`).Scan(&ownerRoleID); err != nil {
		return "", "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_organization_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'organization', $1, $4)
		ON CONFLICT DO NOTHING
	`, orgID, ownerRoleID, ownerUserID, createdByUserID); err != nil {
		return "", "", err
	}

	return orgID, workspaceID, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashRefreshToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(prefix string) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

var slugCleanup = regexp.MustCompile(`[^a-z0-9]+`)

func uniqueSlug(name string) (string, error) {
	base := strings.ToLower(strings.TrimSpace(name))
	base = slugCleanup.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "org"
	}
	suffix, err := randomToken("")
	if err != nil {
		return "", err
	}
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return base + "-" + strings.ToLower(suffix), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isUniqueConstraint(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
