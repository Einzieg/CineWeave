package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRuntimeOperationReconciliationIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run runtime operation integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for runtime operation integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	authService := auth.NewService(pool, "runtime-operation-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	server := New(pool, authService, provider.NewService(pool, vault), nil, nil)
	server.temporal = &fakeTemporalClient{}
	handler := server.Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email: "runtime-operation-" + suffix + "@example.test", Username: randomStorageSegment(), Password: "Password123!", DisplayName: "Runtime Operation",
		OrganizationName: "Runtime Operation Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	workspaceID := firstWorkspaceID(t, ctx, pool, owner.OrganizationID)
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": workspaceID, "name": "Runtime Operation Project", "settings": map[string]any{},
	}, &project)

	var workflowRunID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(organization_id, project_id, temporal_workflow_id, workflow_type, status, input, output, created_by)
		VALUES ($1, $2, $3, 'batch_generate_asset_cards', 'queued', '{}', '{}', $4)
		RETURNING id::text
	`, owner.OrganizationID, project.ID, "runtime-operation-"+suffix, owner.User.ID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	operationID := insertUnknownRuntimeOperation(t, ctx, pool, owner.OrganizationID, project.ID, "asset-batches:create", workflowRunID, "hash-workflow")
	insertUnknownIdempotencyOperation(t, ctx, pool, owner.OrganizationID, operationID, "asset-batches:create", "recover-workflow", "hash-workflow")

	var recovered RuntimeOperation
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/operations/"+operationID+"/reconcile", owner.AccessToken, owner.OrganizationID, map[string]any{}, &recovered)
	if recovered.Status != "succeeded" || recovered.WorkflowRunID != workflowRunID || recovered.ReconcileRequired || recovered.RetryAllowed || len(recovered.ResultSnapshot) == 0 {
		t.Fatalf("recovered operation = %+v", recovered)
	}
	var keyStatus string
	var responseStatus int
	if err := pool.QueryRow(ctx, `SELECT status, response_status FROM idempotency_keys WHERE operation_id = $1`, operationID).Scan(&keyStatus, &responseStatus); err != nil {
		t.Fatalf("load recovered idempotency key: %v", err)
	}
	if keyStatus != "succeeded" || responseStatus != http.StatusAccepted {
		t.Fatalf("recovered key status=%s response=%d", keyStatus, responseStatus)
	}

	retryOperationID := insertUnknownRuntimeOperation(t, ctx, pool, owner.OrganizationID, project.ID, "asset-batches:create", "", "hash-no-side-effect")
	insertUnknownIdempotencyOperation(t, ctx, pool, owner.OrganizationID, retryOperationID, "asset-batches:create", "safe-retry", "hash-no-side-effect")
	var retryable RuntimeOperation
	doAPISuccess(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/operations/"+retryOperationID+"/reconcile", owner.AccessToken, owner.OrganizationID, map[string]any{}, &retryable)
	if retryable.Status != "failed_retryable" || !retryable.RetryAllowed || retryable.ReconcileRequired || retryable.ErrorCode != "OPERATION_NOT_COMMITTED" {
		t.Fatalf("retryable operation = %+v", retryable)
	}

	unknownID := insertUnknownRuntimeOperation(t, ctx, pool, owner.OrganizationID, project.ID, "external.side_effect", "", "hash-unknown")
	assertAPIErrorCode(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/operations/"+unknownID+"/reconcile", owner.AccessToken, owner.OrganizationID, map[string]any{}, http.StatusConflict, "OPERATION_RECONCILIATION_REQUIRED")
	var unknown RuntimeOperation
	doAPISuccess(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/operations/"+unknownID, owner.AccessToken, owner.OrganizationID, nil, &unknown)
	if unknown.Status != "unknown_outcome" || !unknown.ReconcileRequired || unknown.RetryAllowed {
		t.Fatalf("unknown operation = %+v", unknown)
	}
}

func insertUnknownRuntimeOperation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, projectID, operationType, workflowRunID, requestHash string) string {
	t.Helper()
	var operationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO runtime_operations(
			organization_id, project_id, operation_type, status, workflow_run_id,
			request_hash, hash_schema_version, lease_expires_at
		)
		VALUES ($1, $2, $3, 'unknown_outcome', NULLIF($4, '')::uuid, $5, 1, now() - interval '1 minute')
		RETURNING id::text
	`, organizationID, projectID, operationType, workflowRunID, requestHash).Scan(&operationID); err != nil {
		t.Fatalf("insert runtime operation: %v", err)
	}
	return operationID
}

func insertUnknownIdempotencyOperation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, operationID, scope, key, requestHash string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO idempotency_keys(
			organization_id, scope, key, request_hash, status, operation_id,
			hash_schema_version, lease_expires_at, expires_at
		)
		VALUES ($1, $2, $3, $4, 'unknown_outcome', $5, 1, now() - interval '1 minute', now() + interval '1 day')
	`, organizationID, scope, key, requestHash, operationID); err != nil {
		t.Fatalf("insert idempotency key: %v", err)
	}
}
