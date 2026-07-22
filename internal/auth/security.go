package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	securityActionLogin              = "login"
	securityActionRegister           = "register"
	securityActionInvitationResolve  = "invitation_resolve"
	securityActionInvitationRegister = "invitation_register"
	securityActionInvitationAccept   = "invitation_accept"
	securityActionPasswordReset      = "password_reset"

	securityFailureWindow = 15 * time.Minute
	securityBlockDuration = 15 * time.Minute
)

type securityRatePolicy struct {
	identityFailures int
	clientFailures   int
}

var securityRatePolicies = map[string]securityRatePolicy{
	securityActionLogin:              {identityFailures: 8, clientFailures: 40},
	securityActionRegister:           {identityFailures: 8, clientFailures: 40},
	securityActionInvitationResolve:  {identityFailures: 12, clientFailures: 60},
	securityActionInvitationRegister: {identityFailures: 8, clientFailures: 40},
	securityActionInvitationAccept:   {identityFailures: 12, clientFailures: 60},
	securityActionPasswordReset:      {identityFailures: 8, clientFailures: 40},
}

func (s *Service) checkSecurityRateLimit(ctx context.Context, action, subject string, r *http.Request) error {
	policy, ok := securityRatePolicies[action]
	if !ok {
		return errors.New("unknown auth security action")
	}
	identityHash := s.securitySubjectHash(action, "identity", subject)
	clientHash := s.securitySubjectHash(action, "client", s.clientIP(r))
	var blocked bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM auth_security_failures
			WHERE ((scope = $1 AND subject_hash = $2) OR (scope = $3 AND subject_hash = $4))
			  AND blocked_until > now()
		)
	`, securityScope(action, "identity", policy.identityFailures), identityHash,
		securityScope(action, "client", policy.clientFailures), clientHash).Scan(&blocked)
	if err != nil {
		return err
	}
	if blocked {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) recordSecurityFailure(ctx context.Context, action, subject string, r *http.Request) error {
	policy, ok := securityRatePolicies[action]
	if !ok {
		return errors.New("unknown auth security action")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	if err := upsertSecurityFailure(ctx, tx,
		securityScope(action, "identity", policy.identityFailures),
		s.securitySubjectHash(action, "identity", subject), policy.identityFailures); err != nil {
		return err
	}
	if err := upsertSecurityFailure(ctx, tx,
		securityScope(action, "client", policy.clientFailures),
		s.securitySubjectHash(action, "client", s.clientIP(r)), policy.clientFailures); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM auth_security_failures WHERE updated_at < now() - interval '30 days'`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) clearSecurityIdentityFailures(ctx context.Context, action, subject string) error {
	policy, ok := securityRatePolicies[action]
	if !ok {
		return errors.New("unknown auth security action")
	}
	_, err := s.db.Exec(ctx, `
		DELETE FROM auth_security_failures
		WHERE scope = $1 AND subject_hash = $2
	`, securityScope(action, "identity", policy.identityFailures), s.securitySubjectHash(action, "identity", subject))
	return err
}

func upsertSecurityFailure(ctx context.Context, tx pgx.Tx, scope, subjectHash string, limit int) error {
	windowSeconds := int(securityFailureWindow / time.Second)
	blockSeconds := int(securityBlockDuration / time.Second)
	_, err := tx.Exec(ctx, `
		INSERT INTO auth_security_failures(
			scope, subject_hash, failure_count, window_started_at, blocked_until, updated_at
		)
		VALUES ($1, $2, 1, now(), CASE WHEN $3 <= 1 THEN now() + make_interval(secs => $5) ELSE NULL END, now())
		ON CONFLICT (scope, subject_hash) DO UPDATE SET
			failure_count = CASE
				WHEN auth_security_failures.window_started_at <= now() - make_interval(secs => $4)
				  OR (auth_security_failures.blocked_until IS NOT NULL AND auth_security_failures.blocked_until <= now())
				THEN 1
				ELSE auth_security_failures.failure_count + 1
			END,
			window_started_at = CASE
				WHEN auth_security_failures.window_started_at <= now() - make_interval(secs => $4)
				  OR (auth_security_failures.blocked_until IS NOT NULL AND auth_security_failures.blocked_until <= now())
				THEN now()
				ELSE auth_security_failures.window_started_at
			END,
			blocked_until = CASE
				WHEN (CASE
					WHEN auth_security_failures.window_started_at <= now() - make_interval(secs => $4)
					  OR (auth_security_failures.blocked_until IS NOT NULL AND auth_security_failures.blocked_until <= now())
					THEN 1
					ELSE auth_security_failures.failure_count + 1
				END) >= $3
				THEN now() + make_interval(secs => $5)
				ELSE NULL
			END,
			updated_at = now()
	`, scope, subjectHash, limit, windowSeconds, blockSeconds)
	return err
}

func (s *Service) securitySubjectHash(action, kind, value string) string {
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(action))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(kind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(mac.Sum(nil))
}

func securityScope(action, kind string, limit int) string {
	return action + ":" + kind + ":" + strconv.Itoa(limit)
}

func registrationRateSubject(email, username string) string {
	return normalizeEmail(email) + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

func invitationRateSubject(token, email string) string {
	return hashRefreshToken(token) + "\x00" + normalizeEmail(email)
}
