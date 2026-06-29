package provider

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type circuitStateSnapshot struct {
	State        string
	FailureCount int
	SuccessCount int
	UpdatedAt    time.Time
}

func (g *ProviderGuard) RecordSuccess(ctx context.Context, req GuardRequest) error {
	if g == nil || g.DB == nil {
		return nil
	}
	req = normalizeGuardRequest(req)
	tx, err := g.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	policy, err := g.resolveEffectivePolicyTx(ctx, tx, req)
	if err != nil || policy == nil || intValue(policy.FailureThreshold) <= 0 {
		return err
	}
	key := circuitKeyFor(req, *policy)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "circuit:"+guardLockKey(req)); err != nil {
		return err
	}
	snapshot, exists, err := g.circuitStateTx(ctx, tx, req, key)
	if err != nil {
		return err
	}
	successCount := 1
	if exists {
		successCount = snapshot.SuccessCount + 1
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provider_circuit_states(
			organization_id, provider_account_id, provider_model_id, task_type,
			state, failure_count, success_count, opened_at, half_open_at, next_attempt_at,
			last_error_code, last_error_message, updated_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'closed', 0, $5, NULL, NULL, NULL, NULL, NULL, now())
		ON CONFLICT (organization_id, provider_account_id, provider_model_id, task_type)
		DO UPDATE SET
			state = 'closed',
			failure_count = 0,
			success_count = $5,
			opened_at = NULL,
			half_open_at = NULL,
			next_attempt_at = NULL,
			last_error_code = NULL,
			last_error_message = NULL,
			updated_at = now()
	`, req.OrganizationID, key.ProviderAccountID, key.ProviderModelID, req.TaskType, successCount); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (g *ProviderGuard) RecordFailure(ctx context.Context, req GuardRequest, code, message string) error {
	if g == nil || g.DB == nil {
		return nil
	}
	req = normalizeGuardRequest(req)
	tx, err := g.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	policy, err := g.resolveEffectivePolicyTx(ctx, tx, req)
	if err != nil || policy == nil || intValue(policy.FailureThreshold) <= 0 {
		return err
	}
	key := circuitKeyFor(req, *policy)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "circuit:"+guardLockKey(req)); err != nil {
		return err
	}

	snapshot, exists, err := g.circuitStateTx(ctx, tx, req, key)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	windowSeconds := intValue(policy.FailureWindowSeconds)
	if windowSeconds <= 0 {
		windowSeconds = defaultFailureWindowSeconds
	}
	threshold := intValue(policy.FailureThreshold)
	cooldownSeconds := intValue(policy.CircuitCooldownSeconds)
	if cooldownSeconds <= 0 {
		cooldownSeconds = defaultCircuitCooldownSeconds
	}
	failureCount := 1
	if exists && now.Sub(snapshot.UpdatedAt) <= time.Duration(windowSeconds)*time.Second {
		failureCount = snapshot.FailureCount + 1
	}
	if exists && snapshot.State == "half_open" {
		failureCount = threshold
	}

	state := "closed"
	var openedAt *time.Time
	var nextAttemptAt *time.Time
	if failureCount >= threshold {
		state = "open"
		opened := now
		next := now.Add(time.Duration(cooldownSeconds) * time.Second)
		openedAt = &opened
		nextAttemptAt = &next
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO provider_circuit_states(
			organization_id, provider_account_id, provider_model_id, task_type,
			state, failure_count, success_count, opened_at, half_open_at, next_attempt_at,
			last_error_code, last_error_message, updated_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, 0, $7, NULL, $8, $9, $10, now())
		ON CONFLICT (organization_id, provider_account_id, provider_model_id, task_type)
		DO UPDATE SET
			state = $5,
			failure_count = $6,
			success_count = 0,
			opened_at = $7,
			half_open_at = NULL,
			next_attempt_at = $8,
			last_error_code = $9,
			last_error_message = $10,
			updated_at = now()
	`, req.OrganizationID, key.ProviderAccountID, key.ProviderModelID, req.TaskType, state, failureCount, nullableTime(openedAt), nullableTime(nextAttemptAt), nullString(code), nullString(message)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (g *ProviderGuard) circuitStateTx(ctx context.Context, tx pgx.Tx, req GuardRequest, key providerCircuitKey) (circuitStateSnapshot, bool, error) {
	var snapshot circuitStateSnapshot
	err := tx.QueryRow(ctx, `
		SELECT state, failure_count, success_count, updated_at
		FROM provider_circuit_states
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND provider_model_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
		  AND task_type = $4
		FOR UPDATE
	`, req.OrganizationID, key.ProviderAccountID, key.ProviderModelID, req.TaskType).Scan(&snapshot.State, &snapshot.FailureCount, &snapshot.SuccessCount, &snapshot.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return circuitStateSnapshot{}, false, nil
		}
		return circuitStateSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return sql.NullTime{}
	}
	return *value
}
