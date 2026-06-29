package provider

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderGuardLimitPolicies(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider guard integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider guard integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, "http://127.0.0.1:65534")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	accountID := providerAccountIDForModel(t, ctx, pool, modelID)
	guard := NewProviderGuard(pool)

	base := GuardRequest{
		OrganizationID:    orgID,
		ProviderAccountID: accountID,
		ProviderModelID:   modelID,
		TaskType:          TaskTypeTextGenerate,
		EstimatedCost:     "0.00000000",
		Currency:          "USD",
		LeaseTTL:          time.Minute,
		AcquiredByService: "test",
	}

	lease, err := guard.Acquire(ctx, base)
	if err != nil {
		t.Fatalf("acquire without policy: %v", err)
	}
	if lease.LeaseToken == "" {
		t.Fatal("lease token is empty")
	}
	if err := guard.Release(ctx, lease, ""); err != nil {
		t.Fatalf("release without policy: %v", err)
	}

	insertLimitPolicy(t, ctx, pool, orgID, accountID, modelID, TaskTypeTextGenerate, map[string]any{"max_concurrency": 1})
	active, err := guard.Acquire(ctx, base)
	if err != nil {
		t.Fatalf("acquire first constrained lease: %v", err)
	}
	_, err = guard.Acquire(ctx, base)
	assertGuardCode(t, err, CodeProviderConcurrencyLimited)
	if err := guard.Release(ctx, active, ""); err != nil {
		t.Fatalf("release constrained lease: %v", err)
	}
	if lease, err := guard.Acquire(ctx, base); err != nil {
		t.Fatalf("released lease did not free capacity: %v", err)
	} else if err := guard.Release(ctx, lease, ""); err != nil {
		t.Fatalf("release reacquired lease: %v", err)
	}

	expiredToken := "expired-" + strings.ReplaceAll(modelID, "-", "")
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_leases(
			organization_id, provider_account_id, provider_model_id,
			task_type, status, acquired_by_service, lease_token, expires_at
		)
		VALUES ($1, $2, $3, $4, 'active', 'test', $5, now() - interval '1 second')
	`, orgID, accountID, modelID, TaskTypeTextGenerate, expiredToken); err != nil {
		t.Fatalf("insert expired lease: %v", err)
	}
	if lease, err := guard.Acquire(ctx, base); err != nil {
		t.Fatalf("expired lease should be ignored: %v", err)
	} else if err := guard.Release(ctx, lease, ""); err != nil {
		t.Fatalf("release after expired lease test: %v", err)
	}
	var expiredStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM provider_leases WHERE lease_token = $1`, expiredToken).Scan(&expiredStatus); err != nil {
		t.Fatalf("select expired lease: %v", err)
	}
	if expiredStatus != "expired" {
		t.Fatalf("expired lease status = %s, want expired", expiredStatus)
	}

	streamReq := base
	streamReq.TaskType = TaskTypeTextStream
	insertLimitPolicy(t, ctx, pool, orgID, accountID, modelID, TaskTypeTextStream, map[string]any{"requests_per_minute": 1})
	insertProviderCallForLimit(t, ctx, pool, orgID, accountID, modelID, TaskTypeTextStream, "succeeded")
	_, err = guard.Acquire(ctx, streamReq)
	assertGuardCode(t, err, CodeProviderRateLimited)

	imageReq := base
	imageReq.TaskType = TaskTypeImageGenerate
	imageReq.EstimatedCost = "0.00000000"
	insertLimitPolicy(t, ctx, pool, orgID, accountID, modelID, TaskTypeImageGenerate, map[string]any{"daily_budget": "0.00000000"})
	_, err = guard.Acquire(ctx, imageReq)
	assertGuardCode(t, err, CodeProviderDailyQuotaExceeded)

	circuitReq := base
	circuitReq.TaskType = TaskTypeVideoPollTask
	insertLimitPolicy(t, ctx, pool, orgID, accountID, modelID, TaskTypeVideoPollTask, map[string]any{
		"failure_threshold":        2,
		"failure_window_seconds":   300,
		"circuit_cooldown_seconds": 1,
	})
	if err := guard.RecordFailure(ctx, circuitReq, CodeUpstreamInternalError, "first failure"); err != nil {
		t.Fatalf("record first failure: %v", err)
	}
	if err := guard.RecordFailure(ctx, circuitReq, CodeUpstreamInternalError, "second failure"); err != nil {
		t.Fatalf("record second failure: %v", err)
	}
	_, err = guard.Acquire(ctx, circuitReq)
	assertGuardCode(t, err, CodeProviderCircuitOpen)
	if _, err := pool.Exec(ctx, `
		UPDATE provider_circuit_states
		SET next_attempt_at = now() - interval '1 second'
		WHERE organization_id = $1 AND provider_account_id = $2 AND provider_model_id = $3 AND task_type = $4
	`, orgID, accountID, modelID, TaskTypeVideoPollTask); err != nil {
		t.Fatalf("force circuit cooldown: %v", err)
	}
	halfOpenLease, err := guard.Acquire(ctx, circuitReq)
	if err != nil {
		t.Fatalf("half-open acquire: %v", err)
	}
	if err := guard.Release(ctx, halfOpenLease, ""); err != nil {
		t.Fatalf("release half-open lease: %v", err)
	}
	if err := guard.RecordSuccess(ctx, circuitReq); err != nil {
		t.Fatalf("record half-open success: %v", err)
	}
	var state string
	var failures int
	if err := pool.QueryRow(ctx, `
		SELECT state, failure_count
		FROM provider_circuit_states
		WHERE organization_id = $1 AND provider_account_id = $2 AND provider_model_id = $3 AND task_type = $4
	`, orgID, accountID, modelID, TaskTypeVideoPollTask).Scan(&state, &failures); err != nil {
		t.Fatalf("select circuit state: %v", err)
	}
	if state != "closed" || failures != 0 {
		t.Fatalf("circuit state = %s failures=%d, want closed/0", state, failures)
	}
}

func providerAccountIDForModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, modelID string) string {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, modelID).Scan(&accountID); err != nil {
		t.Fatalf("select provider account id: %v", err)
	}
	return accountID
}

func insertLimitPolicy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, accountID, modelID, taskType string, fields map[string]any) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_limit_policies(
			organization_id, provider_account_id, provider_model_id, task_type,
			max_concurrency, requests_per_minute, requests_per_day,
			daily_budget, monthly_budget, currency,
			failure_threshold, failure_window_seconds, circuit_cooldown_seconds
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8::numeric, $9::numeric, 'USD',
			$10, $11, $12
		)
	`, orgID, accountID, modelID, taskType, fields["max_concurrency"], fields["requests_per_minute"], fields["requests_per_day"], fields["daily_budget"], fields["monthly_budget"], fields["failure_threshold"], fields["failure_window_seconds"], fields["circuit_cooldown_seconds"]); err != nil {
		t.Fatalf("insert limit policy %s: %v", taskType, err)
	}
}

func insertProviderCallForLimit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, accountID, modelID, taskType, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_call_logs(
			organization_id, provider_account_id, provider_model_id,
			task_type, execution_mode, status, request_snapshot, normalized_output
		)
		VALUES ($1, $2, $3, $4, 'sync', $5, '{}', '{}')
	`, orgID, accountID, modelID, taskType, status); err != nil {
		t.Fatalf("insert provider call log: %v", err)
	}
}

func assertGuardCode(t *testing.T, err error, code string) {
	t.Helper()
	var guardErr *ProviderGuardError
	if !errors.As(err, &guardErr) {
		t.Fatalf("error = %v, want ProviderGuardError %s", err, code)
	}
	if guardErr.Standard.Code != code {
		t.Fatalf("guard code = %s, want %s", guardErr.Standard.Code, code)
	}
}
