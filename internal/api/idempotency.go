package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

const idempotencyTTL = "24 hours"

type idempotencyState struct {
	enabled        bool
	organizationID string
	scope          string
	key            string
	requestHash    string
}

func idempotencyKey(r *http.Request, bodyValue string) string {
	if header := strings.TrimSpace(r.Header.Get("Idempotency-Key")); header != "" {
		return header
	}
	return strings.TrimSpace(bodyValue)
}

func idempotencyRequestHash(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Server) prepareIdempotency(w http.ResponseWriter, r *http.Request, organizationID, scope, key, requestHash string) (idempotencyState, bool) {
	key = strings.TrimSpace(key)
	state := idempotencyState{
		enabled:        key != "",
		organizationID: organizationID,
		scope:          scope,
		key:            key,
		requestHash:    requestHash,
	}
	if key == "" {
		return state, true
	}
	if len(key) > 200 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Idempotency-Key is too long", nil, false)
		return state, false
	}

	_, _ = s.db.Exec(r.Context(), `
		DELETE FROM idempotency_keys
		WHERE organization_id = $1 AND scope = $2 AND key = $3 AND expires_at < now()
	`, organizationID, scope, key)

	tag, err := s.db.Exec(r.Context(), `
		INSERT INTO idempotency_keys(organization_id, key, scope, request_hash, expires_at)
		VALUES ($1, $2, $3, $4, now() + ($5::interval))
		ON CONFLICT (organization_id, scope, key) DO NOTHING
	`, organizationID, key, scope, requestHash, idempotencyTTL)
	if err != nil {
		s.writeError(w, r, err)
		return state, false
	}
	if tag.RowsAffected() == 1 {
		return state, true
	}

	var existingHash, status string
	var snapshot []byte
	err = s.db.QueryRow(r.Context(), `
		SELECT request_hash, status, response_snapshot
		FROM idempotency_keys
		WHERE organization_id = $1 AND scope = $2 AND key = $3
	`, organizationID, scope, key).Scan(&existingHash, &status, &snapshot)
	if err != nil {
		if err == pgx.ErrNoRows {
			httpx.WriteError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency key is not available", nil, true)
			return state, false
		}
		s.writeError(w, r, err)
		return state, false
	}
	if existingHash != requestHash {
		httpx.WriteError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency key was used with a different request", nil, false)
		return state, false
	}
	if status == "succeeded" && len(snapshot) > 0 {
		var data any
		if err := json.Unmarshal(snapshot, &data); err != nil {
			s.writeError(w, r, err)
			return state, false
		}
		httpx.WriteJSON(w, r, http.StatusOK, data, map[string]any{"idempotentReplay": true})
		return state, false
	}
	httpx.WriteError(w, r, http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS", "idempotency key is already processing", nil, true)
	return state, false
}

func (s *Server) completeIdempotency(ctx context.Context, state idempotencyState, response any) error {
	if !state.enabled {
		return nil
	}
	snapshot, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'succeeded', response_snapshot = $5, expires_at = now() + ($6::interval)
		WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
	`, state.organizationID, state.scope, state.key, state.requestHash, snapshot, idempotencyTTL)
	return err
}

func (s *Server) failIdempotency(ctx context.Context, state idempotencyState) {
	if !state.enabled {
		return
	}
	_, _ = s.db.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'failed', expires_at = now() + ($5::interval)
		WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
	`, state.organizationID, state.scope, state.key, state.requestHash, idempotencyTTL)
}
