package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	controlKeyTokenPrefix = "cwuk_v1"
	controlKeyName        = "Codex 项目控制"
)

var (
	ErrControlKeyInvalid  = errors.New("Codex control key is invalid")
	ErrControlKeyNotFound = errors.New("Codex control key was not found")
	ErrControlKeyConflict = errors.New("active Codex control key already exists")
)

type ControlKeyMetadata struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	RotatedAt     *time.Time `json:"rotatedAt,omitempty"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
	CanRotate     bool       `json:"canRotate"`
	CanRevoke     bool       `json:"canRevoke"`
	RequiresSetup bool       `json:"requiresSetup"`
}

type ControlKeySecret struct {
	ID        string    `json:"id"`
	Prefix    string    `json:"prefix"`
	Secret    string    `json:"secret"`
	CreatedAt time.Time `json:"createdAt"`
}

type controlKeyRow struct {
	ID                string
	UserID            string
	Name              string
	PublicID          string
	Prefix            string
	SecretHash        string
	CredentialVersion int
	Status            string
	CreatedAt         time.Time
	LastUsedAt        *time.Time
	RotatedAt         *time.Time
	RevokedAt         *time.Time
	UserStatus        string
	UserCredential    int
}

func (s *Service) GetControlKey(ctx context.Context, userID string) (ControlKeyMetadata, error) {
	row, err := getLatestControlKey(ctx, s.db, userID, false)
	if err != nil {
		return ControlKeyMetadata{}, err
	}
	return controlKeyMetadata(row), nil
}

func (s *Service) CreateControlKey(ctx context.Context, userID string) (ControlKeySecret, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ControlKeySecret{}, err
	}
	defer rollback(ctx, tx)
	if _, err := getActiveControlKey(ctx, tx, userID, true); err == nil {
		return ControlKeySecret{}, ErrControlKeyConflict
	} else if !errors.Is(err, ErrControlKeyNotFound) {
		return ControlKeySecret{}, err
	}
	secret, err := createControlKeyTx(ctx, tx, userID, false)
	if err != nil {
		return ControlKeySecret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ControlKeySecret{}, err
	}
	return secret, nil
}

func (s *Service) RotateControlKey(ctx context.Context, userID string) (ControlKeySecret, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ControlKeySecret{}, err
	}
	defer rollback(ctx, tx)
	current, err := getActiveControlKey(ctx, tx, userID, true)
	if err != nil && !errors.Is(err, ErrControlKeyNotFound) {
		return ControlKeySecret{}, err
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE user_control_keys
			SET status = 'revoked', revoked_at = now()
			WHERE id = $1 AND status = 'active'
		`, current.ID); err != nil {
			return ControlKeySecret{}, err
		}
	}
	secret, err := createControlKeyTx(ctx, tx, userID, true)
	if err != nil {
		return ControlKeySecret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ControlKeySecret{}, err
	}
	return secret, nil
}

func (s *Service) RevokeControlKey(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE user_control_keys
		SET status = 'revoked', revoked_at = COALESCE(revoked_at, now())
		WHERE user_id = $1 AND status = 'active'
	`, userID)
	return err
}

func (s *Service) AuthenticateControlKey(ctx context.Context, token string) (Principal, ControlKeyMetadata, error) {
	publicID, secret, parseErr := parseControlKeyToken(token)
	providedHash := sha256.Sum256([]byte(secret))
	row, err := getControlKeyByPublicID(ctx, s.db, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		expected := sha256.Sum256([]byte("invalid-control-key"))
		_ = subtle.ConstantTimeCompare(providedHash[:], expected[:])
		return Principal{}, ControlKeyMetadata{}, ErrControlKeyInvalid
	}
	if err != nil {
		return Principal{}, ControlKeyMetadata{}, err
	}
	expectedHash, decodeErr := hex.DecodeString(row.SecretHash)
	validHash := decodeErr == nil && subtle.ConstantTimeCompare(providedHash[:], expectedHash) == 1
	if parseErr != nil || !validHash || row.Status != "active" || row.UserStatus != "active" ||
		row.CredentialVersion != row.UserCredential {
		return Principal{}, ControlKeyMetadata{}, ErrControlKeyInvalid
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE user_control_keys
		SET last_used_at = now()
		WHERE id = $1 AND status = 'active'
		  AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')
	`, row.ID); err != nil {
		return Principal{}, ControlKeyMetadata{}, err
	}
	return Principal{
		UserID: row.UserID, CredentialVersion: row.UserCredential,
	}, controlKeyMetadata(row), nil
}

func createControlKeyTx(ctx context.Context, tx pgx.Tx, userID string, rotated bool) (ControlKeySecret, error) {
	var userStatus string
	var credentialVersion int
	if err := tx.QueryRow(ctx, `
		SELECT status, credential_version
		FROM users
		WHERE id = $1
		FOR SHARE
	`, userID).Scan(&userStatus, &credentialVersion); errors.Is(err, pgx.ErrNoRows) {
		return ControlKeySecret{}, ErrControlKeyNotFound
	} else if err != nil {
		return ControlKeySecret{}, err
	}
	if userStatus != "active" {
		return ControlKeySecret{}, ErrControlKeyInvalid
	}
	publicBytes := make([]byte, 12)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(publicBytes); err != nil {
		return ControlKeySecret{}, fmt.Errorf("generate control key public ID: %w", err)
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return ControlKeySecret{}, fmt.Errorf("generate control key secret: %w", err)
	}
	publicID := base64.RawURLEncoding.EncodeToString(publicBytes)
	secretPart := base64.RawURLEncoding.EncodeToString(secretBytes)
	plaintext := controlKeyTokenPrefix + "_" + publicID + "_" + secretPart
	prefix := controlKeyTokenPrefix + "_" + publicID[:8]
	hash := sha256.Sum256([]byte(secretPart))
	keyID := uuid.NewString()
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO user_control_keys(
			id, user_id, name, public_id, prefix, secret_hash,
			credential_version, rotated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $8 THEN now() ELSE NULL END)
		RETURNING created_at
	`, keyID, userID, controlKeyName, publicID, prefix, hex.EncodeToString(hash[:]),
		credentialVersion, rotated).Scan(&createdAt); err != nil {
		return ControlKeySecret{}, err
	}
	return ControlKeySecret{ID: keyID, Prefix: prefix, Secret: plaintext, CreatedAt: createdAt}, nil
}

func getLatestControlKey(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID string, lock bool) (controlKeyRow, error) {
	query := controlKeySelectSQL + `
		WHERE key.user_id = $1
		ORDER BY CASE WHEN key.status = 'active' THEN 0 ELSE 1 END, key.created_at DESC
		LIMIT 1`
	if lock {
		query += ` FOR UPDATE OF key`
	}
	row, err := scanControlKey(queryer.QueryRow(ctx, query, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return controlKeyRow{}, ErrControlKeyNotFound
	}
	return row, err
}

func getActiveControlKey(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID string, lock bool) (controlKeyRow, error) {
	query := controlKeySelectSQL + ` WHERE key.user_id = $1 AND key.status = 'active'`
	if lock {
		query += ` FOR UPDATE OF key`
	}
	row, err := scanControlKey(queryer.QueryRow(ctx, query, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return controlKeyRow{}, ErrControlKeyNotFound
	}
	return row, err
}

func getControlKeyByPublicID(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, publicID string) (controlKeyRow, error) {
	return scanControlKey(queryer.QueryRow(ctx, controlKeySelectSQL+` WHERE key.public_id = $1`, publicID))
}

const controlKeySelectSQL = `
	SELECT key.id::text, key.user_id::text, key.name, key.public_id, key.prefix,
	       key.secret_hash, key.credential_version, key.status, key.created_at,
	       key.last_used_at, key.rotated_at, key.revoked_at,
	       users.status, users.credential_version
	FROM user_control_keys key
	JOIN users ON users.id = key.user_id
`

func scanControlKey(row pgx.Row) (controlKeyRow, error) {
	var item controlKeyRow
	err := row.Scan(
		&item.ID, &item.UserID, &item.Name, &item.PublicID, &item.Prefix,
		&item.SecretHash, &item.CredentialVersion, &item.Status, &item.CreatedAt,
		&item.LastUsedAt, &item.RotatedAt, &item.RevokedAt,
		&item.UserStatus, &item.UserCredential,
	)
	return item, err
}

func controlKeyMetadata(row controlKeyRow) ControlKeyMetadata {
	status := row.Status
	if status == "active" && (row.UserStatus != "active" || row.CredentialVersion != row.UserCredential) {
		status = "requires_rotation"
	}
	return ControlKeyMetadata{
		ID: row.ID, Name: row.Name, Prefix: row.Prefix, Status: status,
		CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt, RotatedAt: row.RotatedAt,
		RevokedAt: row.RevokedAt, CanRotate: status == "active" || status == "requires_rotation",
		CanRevoke: status == "active", RequiresSetup: false,
	}
}

func parseControlKeyToken(token string) (string, string, error) {
	remainder := strings.TrimPrefix(strings.TrimSpace(token), controlKeyTokenPrefix+"_")
	if len(remainder) < 18 || remainder == strings.TrimSpace(token) {
		return "", "", ErrControlKeyInvalid
	}
	// A 12-byte public ID is exactly 16 Raw URL base64 characters. The secret
	// may itself contain underscores, so the separator cannot be found by split.
	if remainder[16] != '_' {
		return "", "", ErrControlKeyInvalid
	}
	publicID := remainder[:16]
	secret := remainder[17:]
	publicBytes, publicErr := base64.RawURLEncoding.DecodeString(publicID)
	secretBytes, secretErr := base64.RawURLEncoding.DecodeString(secret)
	if publicErr != nil || secretErr != nil || len(publicBytes) != 12 || len(secretBytes) != 32 {
		return "", "", ErrControlKeyInvalid
	}
	return publicID, secret, nil
}
