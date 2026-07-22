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

const (
	idempotencyTTL               = "24 hours"
	idempotencyLease             = "2 minutes"
	idempotencyHashSchemaVersion = 1
)

type idempotencyState struct {
	enabled           bool
	organizationID    string
	scope             string
	key               string
	requestHash       string
	hashSchemaVersion int
	operationID       string
}

type idempotencyClaim struct {
	state          idempotencyState
	replaySnapshot json.RawMessage
	replayStatus   int
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
	if len(key) > 200 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Idempotency-Key is too long", nil, false)
		return idempotencyState{}, false
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		s.writeError(w, r, err)
		return idempotencyState{}, false
	}
	defer tx.Rollback(r.Context())
	claim, err := claimIdempotencyTx(r.Context(), tx, organizationID, scope, key, requestHash)
	if err != nil {
		s.writeError(w, r, err)
		return claim.state, false
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return claim.state, false
	}
	if len(claim.replaySnapshot) > 0 {
		var data any
		if err := json.Unmarshal(claim.replaySnapshot, &data); err != nil {
			s.writeError(w, r, err)
			return claim.state, false
		}
		status := claim.replayStatus
		if status < 200 || status > 299 {
			status = http.StatusOK
		}
		httpx.WriteJSON(w, r, status, data, map[string]any{"idempotentReplay": true, "operationId": claim.state.operationID})
		return claim.state, false
	}
	return claim.state, true
}

// claimIdempotencyTx performs only database work. Callers that create a
// durable operation should invoke it inside the same transaction as the
// operation, its outbox row, and the final idempotency snapshot.
func claimIdempotencyTx(ctx context.Context, tx pgx.Tx, organizationID, scope, key, requestHash string) (idempotencyClaim, error) {
	key = strings.TrimSpace(key)
	state := idempotencyState{
		enabled:           key != "",
		organizationID:    organizationID,
		scope:             scope,
		key:               key,
		requestHash:       requestHash,
		hashSchemaVersion: idempotencyHashSchemaVersion,
	}
	claim := idempotencyClaim{state: state}
	if key == "" {
		return claim, nil
	}
	if len(key) > 200 {
		return claim, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Idempotency-Key is too long")
	}

	// Unknown outcomes are audit records and must never disappear merely
	// because their normal replay TTL elapsed.
	if _, err := tx.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE organization_id = $1 AND scope = $2 AND key = $3
		  AND expires_at < now()
		  AND status IN ('succeeded', 'failed_retryable', 'failed_terminal')
	`, organizationID, scope, key); err != nil {
		return claim, err
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys(
			organization_id, key, scope, request_hash, hash_schema_version,
			status, expires_at, lease_expires_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'processing', now() + ($6::interval), now() + ($7::interval), now())
		ON CONFLICT (organization_id, scope, key) DO NOTHING
	`, organizationID, key, scope, requestHash, idempotencyHashSchemaVersion, idempotencyTTL, idempotencyLease)
	if err != nil {
		return claim, err
	}
	if tag.RowsAffected() == 1 {
		return claim, nil
	}

	var existingHash, status, errorCode, errorMessage, operationID string
	var hashSchemaVersion, responseStatus int
	var snapshot []byte
	var leaseActive bool
	err = tx.QueryRow(ctx, `
		SELECT request_hash, hash_schema_version, status,
		       COALESCE(response_snapshot, 'null'::jsonb), COALESCE(response_status, 0),
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       COALESCE(operation_id::text, ''), COALESCE(lease_expires_at > now(), false)
		FROM idempotency_keys
		WHERE organization_id = $1 AND scope = $2 AND key = $3
		FOR UPDATE
	`, organizationID, scope, key).Scan(
		&existingHash, &hashSchemaVersion, &status, &snapshot, &responseStatus,
		&errorCode, &errorMessage, &operationID, &leaseActive,
	)
	if err != nil {
		return claim, err
	}
	claim.state.operationID = operationID
	if existingHash != requestHash || hashSchemaVersion != idempotencyHashSchemaVersion {
		conflict := newAPIError(http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", "idempotency key was used with a different request")
		conflict.Details = map[string]any{"operationId": operationID}
		return claim, conflict
	}

	switch status {
	case "succeeded":
		if string(snapshot) == "null" || len(snapshot) == 0 {
			return claim, newAPIError(http.StatusConflict, "IDEMPOTENCY_RESULT_MISSING", "idempotency result snapshot is missing")
		}
		claim.replaySnapshot = snapshot
		claim.replayStatus = responseStatus
		return claim, nil
	case "failed_terminal":
		if errorCode == "" {
			errorCode = "IDEMPOTENCY_TERMINAL_FAILURE"
		}
		if errorMessage == "" {
			errorMessage = "the original operation failed permanently"
		}
		statusCode := responseStatus
		if statusCode < 400 || statusCode > 599 {
			statusCode = http.StatusUnprocessableEntity
		}
		terminal := newAPIError(statusCode, errorCode, errorMessage)
		terminal.Details = map[string]any{"operationId": operationID, "idempotentReplay": true}
		return claim, terminal
	case "failed_retryable":
		if _, err := tx.Exec(ctx, `
			UPDATE idempotency_keys
			SET status = 'processing', response_snapshot = NULL, response_status = NULL,
			    error_code = NULL, error_message = NULL,
			    lease_expires_at = now() + ($5::interval), retry_count = retry_count + 1,
			    expires_at = now() + ($6::interval), updated_at = now()
			WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
		`, organizationID, scope, key, requestHash, idempotencyLease, idempotencyTTL); err != nil {
			return claim, err
		}
		return claim, nil
	case "unknown_outcome":
		unknown := newAPIError(http.StatusConflict, "IDEMPOTENCY_UNKNOWN_OUTCOME", "the original operation outcome is not yet known")
		unknown.Retryable = false
		unknown.Details = map[string]any{"operationId": operationID, "reconcileRequired": true}
		return claim, unknown
	case "processing":
		if !leaseActive {
			if _, err := tx.Exec(ctx, `
				UPDATE idempotency_keys
				SET status = 'unknown_outcome', error_code = 'IDEMPOTENCY_LEASE_EXPIRED',
				    error_message = 'processing lease expired before the outcome was confirmed',
				    lease_expires_at = NULL, updated_at = now()
				WHERE organization_id = $1 AND scope = $2 AND key = $3 AND status = 'processing'
			`, organizationID, scope, key); err != nil {
				return claim, err
			}
			unknown := newAPIError(http.StatusConflict, "IDEMPOTENCY_UNKNOWN_OUTCOME", "the original operation outcome is not yet known")
			unknown.Details = map[string]any{"operationId": operationID, "reconcileRequired": true}
			return claim, unknown
		}
		inProgress := newAPIError(http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS", "idempotency key is already processing")
		inProgress.Retryable = true
		inProgress.Details = map[string]any{"operationId": operationID}
		return claim, inProgress
	default:
		return claim, newAPIError(http.StatusConflict, "IDEMPOTENCY_STATE_INVALID", "idempotency record has an invalid state")
	}
}

func ensureRuntimeOperationTx(
	ctx context.Context,
	tx pgx.Tx,
	claim *idempotencyClaim,
	organizationID string,
	projectID string,
	operationType string,
	requestHash string,
) (string, error) {
	operationID := claim.state.operationID
	if operationID == "" {
		if err := tx.QueryRow(ctx, `
			INSERT INTO runtime_operations(
				organization_id, project_id, operation_type, status, request_hash,
				hash_schema_version, lease_expires_at
			)
			VALUES ($1, $2, $3, 'processing', $4, $5, now() + ($6::interval))
			RETURNING id::text
		`, organizationID, projectID, operationType, requestHash, idempotencyHashSchemaVersion, idempotencyLease).Scan(&operationID); err != nil {
			return "", err
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE runtime_operations
		SET status = 'processing', request_hash = $2, hash_schema_version = $3,
		    result_snapshot = NULL, error_code = NULL, error_message = NULL,
		    lease_expires_at = now() + ($4::interval), completed_at = NULL, updated_at = now()
		WHERE id = $1
	`, operationID, requestHash, idempotencyHashSchemaVersion, idempotencyLease); err != nil {
		return "", err
	}
	claim.state.operationID = operationID
	if claim.state.enabled {
		if _, err := tx.Exec(ctx, `
			UPDATE idempotency_keys
			SET operation_id = $5, updated_at = now()
			WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
		`, claim.state.organizationID, claim.state.scope, claim.state.key, claim.state.requestHash, operationID); err != nil {
			return "", err
		}
	}
	return operationID, nil
}

func completeRuntimeOperationTx(ctx context.Context, tx pgx.Tx, operationID, workflowRunID string, response any) (json.RawMessage, error) {
	resultSnapshot, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE runtime_operations
		SET status = 'succeeded', workflow_run_id = $2, result_snapshot = $3,
		    lease_expires_at = NULL, completed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'processing'
	`, operationID, workflowRunID, resultSnapshot); err != nil {
		return nil, err
	}
	return resultSnapshot, nil
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
		SET status = 'succeeded', response_snapshot = $5, response_status = $6,
		    error_code = NULL, error_message = NULL, lease_expires_at = NULL,
		    expires_at = now() + ($7::interval), updated_at = now()
		WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
	`, state.organizationID, state.scope, state.key, state.requestHash, snapshot, http.StatusOK, idempotencyTTL)
	return err
}

func completeIdempotencyTxWithStatus(ctx context.Context, tx pgx.Tx, state idempotencyState, responseStatus int, response any) error {
	if !state.enabled {
		return nil
	}
	snapshot, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'succeeded', response_snapshot = $5, response_status = $6,
		    error_code = NULL, error_message = NULL, lease_expires_at = NULL,
		    expires_at = now() + ($7::interval), updated_at = now()
		WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
	`, state.organizationID, state.scope, state.key, state.requestHash, snapshot, responseStatus, idempotencyTTL)
	return err
}

// failIdempotency is reserved for call sites that know the business side
// effect did not commit.
func (s *Server) failIdempotency(ctx context.Context, state idempotencyState) {
	if !state.enabled {
		return
	}
	_, _ = s.db.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'failed_retryable', response_snapshot = NULL, response_status = NULL,
		    error_code = 'OPERATION_ROLLED_BACK', error_message = 'operation did not commit',
		    lease_expires_at = NULL, expires_at = now() + ($5::interval), updated_at = now()
		WHERE organization_id = $1 AND scope = $2 AND key = $3 AND request_hash = $4
		  AND status = 'processing'
	`, state.organizationID, state.scope, state.key, state.requestHash, idempotencyTTL)
}
