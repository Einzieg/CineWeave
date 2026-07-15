package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailedRetryableIdempotencyKeyCanBeClaimedAgain(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	const scope = "runtime-contract:test"
	const key = "retryable-key"
	const requestHash = "hash-a"
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO idempotency_keys(
			organization_id, key, scope, request_hash, status, expires_at, lease_expires_at
		)
		VALUES ($1, $2, $3, $4, 'failed_retryable', now() + interval '1 hour', now() - interval '1 minute')
	`, seed.organizationID, key, scope, requestHash); err != nil {
		t.Fatalf("insert retryable idempotency record: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/test", nil)
	response := httptest.NewRecorder()
	state, ok := seed.apiServer.prepareIdempotency(response, request, seed.organizationID, scope, key, requestHash)
	if !ok {
		t.Fatalf("failed_retryable key was not reclaimed: status=%d body=%s", response.Code, response.Body.String())
	}
	if !state.enabled {
		t.Fatal("reclaimed idempotency state is disabled")
	}
	var status string
	var retryCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT status, retry_count
		FROM idempotency_keys
		WHERE organization_id = $1 AND scope = $2 AND key = $3
	`, seed.organizationID, scope, key).Scan(&status, &retryCount); err != nil {
		t.Fatalf("load reclaimed idempotency record: %v", err)
	}
	if status != "processing" || retryCount != 1 {
		t.Fatalf("status=%s retryCount=%d, want processing/1", status, retryCount)
	}
}

func TestUnknownOutcomeIdempotencyKeyRequiresReconciliation(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	const scope = "runtime-contract:test"
	const key = "unknown-key"
	const requestHash = "hash-a"
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO idempotency_keys(
			organization_id, key, scope, request_hash, status, expires_at, lease_expires_at
		)
		VALUES ($1, $2, $3, $4, 'unknown_outcome', now() + interval '1 hour', now() - interval '1 minute')
	`, seed.organizationID, key, scope, requestHash); err != nil {
		t.Fatalf("insert unknown idempotency record: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/test", nil)
	response := httptest.NewRecorder()
	_, ok := seed.apiServer.prepareIdempotency(response, request, seed.organizationID, scope, key, requestHash)
	if ok {
		t.Fatal("unknown outcome was automatically reclaimed")
	}
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "IDEMPOTENCY_UNKNOWN_OUTCOME") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
