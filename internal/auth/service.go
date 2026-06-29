package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailExists        = errors.New("email already exists")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
)

type Service struct {
	db         *pgxpool.Pool
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type Principal struct {
	UserID         string
	OrganizationID string
}

type RegisterRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	DisplayName      string `json:"displayName"`
	OrganizationName string `json:"organizationName"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
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
}

type UserResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName,omitempty"`
}

type claims struct {
	OrganizationID string `json:"organizationId,omitempty"`
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
	email := normalizeEmail(req.Email)
	if email == "" || len(req.Password) < 8 {
		return TokenResponse{}, ErrInvalidCredentials
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = email
	}
	orgName := strings.TrimSpace(req.OrganizationName)
	if orgName == "" {
		orgName = displayName + "'s Organization"
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

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users(email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id
	`, email, string(passwordHash), displayName).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			return TokenResponse{}, ErrEmailExists
		}
		return TokenResponse{}, err
	}

	orgID, err := createOrganizationForUser(ctx, tx, userID, orgName)
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
		User: UserResponse{
			ID:          userID,
			Email:       email,
			DisplayName: displayName,
		},
	}, nil
}

func (s *Service) Login(ctx context.Context, req LoginRequest, r *http.Request) (TokenResponse, error) {
	email := normalizeEmail(req.Email)
	var userID, passwordHash, displayName, status string
	err := s.db.QueryRow(ctx, `
		SELECT id, password_hash, COALESCE(display_name, ''), status
		FROM users
		WHERE email = $1
	`, email).Scan(&userID, &passwordHash, &displayName, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenResponse{}, ErrInvalidCredentials
		}
		return TokenResponse{}, err
	}
	if status != "active" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) != nil {
		return TokenResponse{}, ErrInvalidCredentials
	}

	orgID, err := s.defaultOrganization(ctx, userID)
	if err != nil {
		return TokenResponse{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TokenResponse{}, err
	}
	defer rollback(ctx, tx)

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
		User: UserResponse{
			ID:          userID,
			Email:       email,
			DisplayName: displayName,
		},
	}, nil
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

	var sessionID, userID, email, displayName string
	var orgID *string
	err = tx.QueryRow(ctx, `
		SELECT s.id, u.id, u.email, COALESCE(u.display_name, ''), s.organization_id
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.refresh_token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
		  AND u.status = 'active'
	`, hash).Scan(&sessionID, &userID, &email, &displayName, &orgID)
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
		User: UserResponse{
			ID:          userID,
			Email:       email,
			DisplayName: displayName,
		},
	}, nil
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
	return Principal{UserID: parsed.Subject, OrganizationID: parsed.OrganizationID}, nil
}

func (s *Service) Me(ctx context.Context, principal Principal) (UserResponse, error) {
	var user UserResponse
	err := s.db.QueryRow(ctx, `
		SELECT id, email, COALESCE(display_name, '')
		FROM users
		WHERE id = $1 AND status = 'active'
	`, principal.UserID).Scan(&user.ID, &user.Email, &user.DisplayName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserResponse{}, ErrUnauthorized
		}
		return UserResponse{}, err
	}
	return user, nil
}

func (s *Service) CreateOrganization(ctx context.Context, userID, name string) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer rollback(ctx, tx)

	orgID, err := createOrganizationForUser(ctx, tx, userID, name)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return orgID, nil
}

func (s *Service) createSession(ctx context.Context, tx pgx.Tx, userID, orgID string, r *http.Request) (string, string, error) {
	accessToken, err := s.accessToken(userID, orgID)
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
		INSERT INTO auth_sessions(user_id, organization_id, refresh_token_hash, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, orgValue, hashRefreshToken(refreshToken), r.UserAgent(), clientIP(r), expiresAt)
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func (s *Service) accessToken(userID, orgID string) (string, error) {
	now := time.Now().UTC()
	claims := claims{
		OrganizationID: orgID,
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

func createOrganizationForUser(ctx context.Context, tx pgx.Tx, userID, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Organization"
	}
	slug, err := uniqueSlug(name)
	if err != nil {
		return "", err
	}

	var orgID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations(name, slug)
		VALUES ($1, $2)
		RETURNING id
	`, name, slug).Scan(&orgID); err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members(organization_id, user_id, status)
		VALUES ($1, $2, 'active')
	`, orgID, userID); err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, 'Default Workspace')
	`, orgID); err != nil {
		return "", err
	}

	var ownerRoleID string
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM roles
		WHERE organization_id IS NULL AND role_key = 'organization_owner' AND scope = 'organization'
	`).Scan(&ownerRoleID); err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO role_bindings(
			organization_id, role_id, subject_type, subject_user_id,
			resource_type, resource_organization_id, created_by
		)
		VALUES ($1, $2, 'user', $3, 'organization', $1, $3)
		ON CONFLICT DO NOTHING
	`, orgID, ownerRoleID, userID); err != nil {
		return "", err
	}

	return orgID, nil
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

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
