package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TaskTypeAny             = "any"
	TaskTypeTextGenerate    = "text.generate"
	TaskTypeTextStream      = "text.stream"
	TaskTypeImageGenerate   = "image.generate"
	TaskTypeVideoCreateTask = "video.create_task"
	TaskTypeVideoPollTask   = "video.poll_task"
	TaskTypeVideoCancelTask = "video.cancel_task"

	defaultLeaseTTL               = 2 * time.Minute
	defaultFailureWindowSeconds   = 300
	defaultCircuitCooldownSeconds = 60
)

type ProviderGuard struct {
	DB *pgxpool.Pool
}

type GuardRequest struct {
	OrganizationID    string
	ProviderAccountID string
	ProviderModelID   string
	TaskType          string
	EstimatedCost     string
	Currency          string
	LeaseTTL          time.Duration
	AcquiredByService string
}

type GuardLease struct {
	LeaseID    string
	LeaseToken string
	ExpiresAt  time.Time
}

type ProviderGuardError struct {
	Standard StandardError
}

func (e *ProviderGuardError) Error() string {
	if e == nil {
		return ""
	}
	return e.Standard.Code + ": " + e.Standard.Message
}

type effectiveLimitPolicy struct {
	ID                     string
	ProviderAccountID      string
	ProviderModelID        string
	TaskType               string
	MaxConcurrency         *int
	RequestsPerMinute      *int
	RequestsPerDay         *int
	DailyBudget            *string
	MonthlyBudget          *string
	Currency               string
	FailureThreshold       *int
	FailureWindowSeconds   *int
	CircuitCooldownSeconds *int
}

func NewProviderGuard(db *pgxpool.Pool) *ProviderGuard {
	return &ProviderGuard{DB: db}
}

func (g *ProviderGuard) Acquire(ctx context.Context, req GuardRequest) (GuardLease, error) {
	if g == nil || g.DB == nil {
		return GuardLease{}, nil
	}
	req = normalizeGuardRequest(req)
	if req.OrganizationID == "" || req.ProviderAccountID == "" || req.TaskType == "" {
		return GuardLease{}, fmt.Errorf("%w: organizationId, providerAccountId, and taskType are required", ErrValidation)
	}

	tx, err := g.DB.Begin(ctx)
	if err != nil {
		return GuardLease{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE provider_leases
		SET status = 'expired'
		WHERE status = 'active'
		  AND expires_at < now()
	`); err != nil {
		return GuardLease{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, guardLockKey(req)); err != nil {
		return GuardLease{}, err
	}

	policy, err := g.resolveEffectivePolicyTx(ctx, tx, req)
	if err != nil {
		return GuardLease{}, err
	}
	if policy != nil {
		if err := g.checkCircuitTx(ctx, tx, req, *policy); err != nil {
			return GuardLease{}, err
		}
		if err := g.checkConcurrencyTx(ctx, tx, req, *policy); err != nil {
			return GuardLease{}, err
		}
		if err := g.checkRequestRateTx(ctx, tx, req, *policy); err != nil {
			return GuardLease{}, err
		}
		if err := g.checkBudgetTx(ctx, tx, req, *policy); err != nil {
			return GuardLease{}, err
		}
	}

	token := uuid.NewString()
	var lease GuardLease
	err = tx.QueryRow(ctx, `
		INSERT INTO provider_leases(
			organization_id, provider_account_id, provider_model_id,
			task_type, status, acquired_by_service, lease_token, expires_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'active', $5, $6, now() + ($7::int * interval '1 second'))
		RETURNING id::text, lease_token, expires_at
	`, req.OrganizationID, req.ProviderAccountID, req.ProviderModelID, req.TaskType, req.AcquiredByService, token, int(req.LeaseTTL.Seconds())).Scan(&lease.LeaseID, &lease.LeaseToken, &lease.ExpiresAt)
	if err != nil {
		return GuardLease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GuardLease{}, err
	}
	return lease, nil
}

func (g *ProviderGuard) resolveEffectivePolicyTx(ctx context.Context, tx pgx.Tx, req GuardRequest) (*effectiveLimitPolicy, error) {
	row := tx.QueryRow(ctx, `
		SELECT
			id::text,
			COALESCE(provider_account_id::text, ''),
			COALESCE(provider_model_id::text, ''),
			task_type,
			max_concurrency,
			requests_per_minute,
			requests_per_day,
			daily_budget::text,
			monthly_budget::text,
			currency,
			failure_threshold,
			failure_window_seconds,
			circuit_cooldown_seconds
		FROM provider_limit_policies
		WHERE organization_id = $1
		  AND enabled = true
		  AND task_type IN ($4, 'any')
		  AND (provider_account_id IS NULL OR provider_account_id = $2::uuid)
		  AND (provider_model_id IS NULL OR provider_model_id = NULLIF($3, '')::uuid)
		ORDER BY
		  CASE
		    WHEN provider_model_id IS NOT NULL AND task_type = $4 THEN 1
		    WHEN provider_model_id IS NOT NULL AND task_type = 'any' THEN 2
		    WHEN provider_account_id IS NOT NULL AND task_type = $4 THEN 3
		    WHEN provider_account_id IS NOT NULL AND task_type = 'any' THEN 4
		    WHEN task_type = $4 THEN 5
		    ELSE 6
		  END
		LIMIT 1
	`, req.OrganizationID, req.ProviderAccountID, req.ProviderModelID, req.TaskType)
	policy, err := scanEffectiveLimitPolicy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &policy, nil
}

func (g *ProviderGuard) checkCircuitTx(ctx context.Context, tx pgx.Tx, req GuardRequest, policy effectiveLimitPolicy) error {
	if intValue(policy.FailureThreshold) <= 0 {
		return nil
	}
	key := circuitKeyFor(req, policy)
	var state string
	var nextAttemptAt sql.NullTime
	err := tx.QueryRow(ctx, `
		SELECT state, next_attempt_at
		FROM provider_circuit_states
		WHERE organization_id = $1
		  AND provider_account_id = $2
		  AND provider_model_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
		  AND task_type = $4
		FOR UPDATE
	`, req.OrganizationID, key.ProviderAccountID, key.ProviderModelID, req.TaskType).Scan(&state, &nextAttemptAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	now := time.Now()
	if state == "open" {
		if nextAttemptAt.Valid && nextAttemptAt.Time.After(now) {
			return newGuardError(CodeProviderCircuitOpen, "provider circuit is open", true, retryAfterMs(now, nextAttemptAt.Time))
		}
		_, err := tx.Exec(ctx, `
			UPDATE provider_circuit_states
			SET state = 'half_open',
			    half_open_at = now(),
			    updated_at = now()
			WHERE organization_id = $1
			  AND provider_account_id = $2
			  AND provider_model_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
			  AND task_type = $4
		`, req.OrganizationID, key.ProviderAccountID, key.ProviderModelID, req.TaskType)
		return err
	}
	return nil
}

func (g *ProviderGuard) checkConcurrencyTx(ctx context.Context, tx pgx.Tx, req GuardRequest, policy effectiveLimitPolicy) error {
	limit := intValue(policy.MaxConcurrency)
	if limit <= 0 {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM provider_leases
		WHERE organization_id = $1
		  AND ($2 = '' OR provider_account_id = NULLIF($2, '')::uuid)
		  AND ($3 = '' OR provider_model_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid)
		  AND ($4 = false OR task_type = $5)
		  AND status = 'active'
		  AND expires_at > now()
	`, req.OrganizationID, scopeAccountID(req, policy), scopeModelID(req, policy), policy.TaskType != TaskTypeAny, req.TaskType).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return newGuardError(CodeProviderConcurrencyLimited, "provider concurrency limit was reached", true, 1000)
	}
	return nil
}

func (g *ProviderGuard) checkRequestRateTx(ctx context.Context, tx pgx.Tx, req GuardRequest, policy effectiveLimitPolicy) error {
	perMinute := intValue(policy.RequestsPerMinute)
	if perMinute > 0 {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM provider_call_logs
			WHERE organization_id = $1
			  AND ($2 = '' OR provider_account_id = NULLIF($2, '')::uuid)
			  AND ($3 = '' OR provider_model_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid)
			  AND ($4 = false OR task_type = $5)
			  AND created_at >= now() - interval '1 minute'
		`, req.OrganizationID, scopeAccountID(req, policy), scopeModelID(req, policy), policy.TaskType != TaskTypeAny, req.TaskType).Scan(&count); err != nil {
			return err
		}
		if count >= perMinute {
			return newGuardError(CodeProviderRateLimited, "provider request rate limit was reached", true, 60000)
		}
	}
	perDay := intValue(policy.RequestsPerDay)
	if perDay > 0 {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM provider_call_logs
			WHERE organization_id = $1
			  AND ($2 = '' OR provider_account_id = NULLIF($2, '')::uuid)
			  AND ($3 = '' OR provider_model_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid)
			  AND ($4 = false OR task_type = $5)
			  AND created_at >= date_trunc('day', now())
		`, req.OrganizationID, scopeAccountID(req, policy), scopeModelID(req, policy), policy.TaskType != TaskTypeAny, req.TaskType).Scan(&count); err != nil {
			return err
		}
		if count >= perDay {
			return newGuardError(CodeProviderDailyQuotaExceeded, "provider daily request quota was exceeded", false, 0)
		}
	}
	return nil
}

func (g *ProviderGuard) checkBudgetTx(ctx context.Context, tx pgx.Tx, req GuardRequest, policy effectiveLimitPolicy) error {
	currency := currencyOrDefault(firstNonEmpty(policy.Currency, req.Currency))
	estimatedCost := decimalValue(req.EstimatedCost)
	if policy.DailyBudget != nil {
		limit := decimalValue(*policy.DailyBudget)
		if limit <= 0 {
			return newGuardError(CodeProviderDailyQuotaExceeded, "provider daily budget was exceeded", false, 0)
		}
		spent, err := g.costSpentTx(ctx, tx, req, policy, currency, "day")
		if err != nil {
			return err
		}
		if spent+estimatedCost >= limit {
			return newGuardError(CodeProviderDailyQuotaExceeded, "provider daily budget was exceeded", false, 0)
		}
	}
	if policy.MonthlyBudget != nil {
		limit := decimalValue(*policy.MonthlyBudget)
		if limit <= 0 {
			return newGuardError(CodeProviderMonthlyBudgetExceeded, "provider monthly budget was exceeded", false, 0)
		}
		spent, err := g.costSpentTx(ctx, tx, req, policy, currency, "month")
		if err != nil {
			return err
		}
		if spent+estimatedCost >= limit {
			return newGuardError(CodeProviderMonthlyBudgetExceeded, "provider monthly budget was exceeded", false, 0)
		}
	}
	return nil
}

func (g *ProviderGuard) costSpentTx(ctx context.Context, tx pgx.Tx, req GuardRequest, policy effectiveLimitPolicy, currency, window string) (float64, error) {
	var raw sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(cr.amount), 0)::text
		FROM cost_records cr
		LEFT JOIN provider_models pm ON pm.id = cr.provider_model_id
		WHERE cr.organization_id = $1
		  AND cr.currency = $2
		  AND (
		    $3 = 'org'
		    OR ($3 = 'account' AND pm.provider_account_id = NULLIF($4, '')::uuid)
		    OR ($3 = 'model' AND cr.provider_model_id = NULLIF($5, '')::uuid)
		  )
		  AND (
		    ($6 = 'day' AND cr.created_at >= date_trunc('day', now()))
		    OR ($6 = 'month' AND cr.created_at >= date_trunc('month', now()))
		  )
	`, req.OrganizationID, currency, budgetScope(policy), scopeAccountID(req, policy), scopeModelID(req, policy), window).Scan(&raw)
	if err != nil {
		return 0, err
	}
	return decimalValue(raw.String), nil
}

func (s *Service) ListProviderLimitPolicies(ctx context.Context, organizationID string) ([]ProviderLimitPolicy, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			id::text, organization_id::text, provider_account_id::text, provider_model_id::text,
			task_type, max_concurrency, requests_per_minute, requests_per_day,
			daily_budget::text, monthly_budget::text, currency,
			failure_threshold, failure_window_seconds, circuit_cooldown_seconds,
			enabled, created_by::text, created_at, updated_at
		FROM provider_limit_policies
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProviderLimitPolicy, 0)
	for rows.Next() {
		item, err := scanProviderLimitPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetProviderLimitPolicy(ctx context.Context, organizationID, policyID string) (ProviderLimitPolicy, error) {
	return scanProviderLimitPolicy(s.db.QueryRow(ctx, `
		SELECT
			id::text, organization_id::text, provider_account_id::text, provider_model_id::text,
			task_type, max_concurrency, requests_per_minute, requests_per_day,
			daily_budget::text, monthly_budget::text, currency,
			failure_threshold, failure_window_seconds, circuit_cooldown_seconds,
			enabled, created_by::text, created_at, updated_at
		FROM provider_limit_policies
		WHERE organization_id = $1 AND id = $2
	`, organizationID, policyID))
}

func (s *Service) CreateProviderLimitPolicy(ctx context.Context, organizationID, userID string, req CreateProviderLimitPolicyRequest) (ProviderLimitPolicy, error) {
	if strings.TrimSpace(req.OrganizationID) != "" && strings.TrimSpace(req.OrganizationID) != organizationID {
		return ProviderLimitPolicy{}, fmt.Errorf("%w: organizationId does not match request context", ErrValidation)
	}
	normalized, err := s.normalizeCreateLimitPolicy(ctx, organizationID, req)
	if err != nil {
		return ProviderLimitPolicy{}, err
	}
	var id string
	if err := s.db.QueryRow(ctx, `
		INSERT INTO provider_limit_policies(
			organization_id, provider_account_id, provider_model_id, task_type,
			max_concurrency, requests_per_minute, requests_per_day,
			daily_budget, monthly_budget, currency,
			failure_threshold, failure_window_seconds, circuit_cooldown_seconds,
			enabled, created_by
		)
		VALUES (
			$1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4,
			$5, $6, $7,
			NULLIF($8, '')::numeric, NULLIF($9, '')::numeric, $10,
			$11, $12, $13,
			$14, NULLIF($15, '')::uuid
		)
		RETURNING id::text
	`, organizationID, normalized.ProviderAccountID, normalized.ProviderModelID, normalized.TaskType, normalized.MaxConcurrency, normalized.RequestsPerMinute, normalized.RequestsPerDay, normalized.DailyBudget, normalized.MonthlyBudget, normalized.Currency, normalized.FailureThreshold, normalized.FailureWindowSeconds, normalized.CircuitCooldownSeconds, normalized.Enabled, userID).Scan(&id); err != nil {
		return ProviderLimitPolicy{}, err
	}
	return s.GetProviderLimitPolicy(ctx, organizationID, id)
}

func (s *Service) UpdateProviderLimitPolicy(ctx context.Context, organizationID, policyID string, req UpdateProviderLimitPolicyRequest) (ProviderLimitPolicy, error) {
	current, err := s.GetProviderLimitPolicy(ctx, organizationID, policyID)
	if err != nil {
		return ProviderLimitPolicy{}, err
	}
	normalized, err := s.normalizeUpdateLimitPolicy(ctx, organizationID, current, req)
	if err != nil {
		return ProviderLimitPolicy{}, err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE provider_limit_policies
		SET provider_account_id = NULLIF($3, '')::uuid,
		    provider_model_id = NULLIF($4, '')::uuid,
		    task_type = $5,
		    max_concurrency = $6,
		    requests_per_minute = $7,
		    requests_per_day = $8,
		    daily_budget = NULLIF($9, '')::numeric,
		    monthly_budget = NULLIF($10, '')::numeric,
		    currency = $11,
		    failure_threshold = $12,
		    failure_window_seconds = $13,
		    circuit_cooldown_seconds = $14,
		    enabled = $15
		WHERE organization_id = $1 AND id = $2
	`, organizationID, policyID, normalized.ProviderAccountID, normalized.ProviderModelID, normalized.TaskType, normalized.MaxConcurrency, normalized.RequestsPerMinute, normalized.RequestsPerDay, normalized.DailyBudget, normalized.MonthlyBudget, normalized.Currency, normalized.FailureThreshold, normalized.FailureWindowSeconds, normalized.CircuitCooldownSeconds, normalized.Enabled)
	if err != nil {
		return ProviderLimitPolicy{}, err
	}
	if tag.RowsAffected() == 0 {
		return ProviderLimitPolicy{}, pgx.ErrNoRows
	}
	return s.GetProviderLimitPolicy(ctx, organizationID, policyID)
}

func (s *Service) DeleteProviderLimitPolicy(ctx context.Context, organizationID, policyID string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM provider_limit_policies WHERE organization_id = $1 AND id = $2`, organizationID, policyID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Service) ListProviderCircuitStates(ctx context.Context, organizationID string) ([]ProviderCircuitState, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			id::text, organization_id::text, provider_account_id::text, provider_model_id::text,
			task_type, state, failure_count, success_count,
			opened_at, half_open_at, next_attempt_at,
			last_error_code, last_error_message, updated_at
		FROM provider_circuit_states
		WHERE organization_id = $1
		ORDER BY updated_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProviderCircuitState, 0)
	for rows.Next() {
		item, err := scanProviderCircuitState(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ResetProviderCircuitState(ctx context.Context, organizationID, stateID string) (ProviderCircuitState, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE provider_circuit_states
		SET state = 'closed',
		    failure_count = 0,
		    success_count = 0,
		    opened_at = NULL,
		    half_open_at = NULL,
		    next_attempt_at = NULL,
		    last_error_code = NULL,
		    last_error_message = NULL,
		    updated_at = now()
		WHERE organization_id = $1 AND id = $2
	`, organizationID, stateID)
	if err != nil {
		return ProviderCircuitState{}, err
	}
	if tag.RowsAffected() == 0 {
		return ProviderCircuitState{}, pgx.ErrNoRows
	}
	return scanProviderCircuitState(s.db.QueryRow(ctx, `
		SELECT
			id::text, organization_id::text, provider_account_id::text, provider_model_id::text,
			task_type, state, failure_count, success_count,
			opened_at, half_open_at, next_attempt_at,
			last_error_code, last_error_message, updated_at
		FROM provider_circuit_states
		WHERE organization_id = $1 AND id = $2
	`, organizationID, stateID))
}

type normalizedLimitPolicy struct {
	ProviderAccountID      string
	ProviderModelID        string
	TaskType               string
	MaxConcurrency         any
	RequestsPerMinute      any
	RequestsPerDay         any
	DailyBudget            string
	MonthlyBudget          string
	Currency               string
	FailureThreshold       any
	FailureWindowSeconds   any
	CircuitCooldownSeconds any
	Enabled                bool
}

func (s *Service) normalizeCreateLimitPolicy(ctx context.Context, organizationID string, req CreateProviderLimitPolicyRequest) (normalizedLimitPolicy, error) {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return s.normalizeLimitPolicyFields(ctx, organizationID, normalizedLimitPolicy{
		ProviderAccountID:      derefString(req.ProviderAccountID),
		ProviderModelID:        derefString(req.ProviderModelID),
		TaskType:               req.TaskType,
		MaxConcurrency:         nullableNonNegativeInt(req.MaxConcurrency),
		RequestsPerMinute:      nullableNonNegativeInt(req.RequestsPerMinute),
		RequestsPerDay:         nullableNonNegativeInt(req.RequestsPerDay),
		DailyBudget:            derefString(req.DailyBudget),
		MonthlyBudget:          derefString(req.MonthlyBudget),
		Currency:               req.Currency,
		FailureThreshold:       nullableNonNegativeInt(req.FailureThreshold),
		FailureWindowSeconds:   nullableNonNegativeInt(req.FailureWindowSeconds),
		CircuitCooldownSeconds: nullableNonNegativeInt(req.CircuitCooldownSeconds),
		Enabled:                enabled,
	})
}

func (s *Service) normalizeUpdateLimitPolicy(ctx context.Context, organizationID string, current ProviderLimitPolicy, req UpdateProviderLimitPolicyRequest) (normalizedLimitPolicy, error) {
	taskType := current.TaskType
	if req.TaskType != nil {
		taskType = *req.TaskType
	}
	currency := current.Currency
	if req.Currency != nil {
		currency = *req.Currency
	}
	enabled := current.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return s.normalizeLimitPolicyFields(ctx, organizationID, normalizedLimitPolicy{
		ProviderAccountID:      firstNonEmpty(derefString(req.ProviderAccountID), derefString(current.ProviderAccountID)),
		ProviderModelID:        firstNonEmpty(derefString(req.ProviderModelID), derefString(current.ProviderModelID)),
		TaskType:               taskType,
		MaxConcurrency:         nullableNonNegativeInt(firstIntPtr(req.MaxConcurrency, current.MaxConcurrency)),
		RequestsPerMinute:      nullableNonNegativeInt(firstIntPtr(req.RequestsPerMinute, current.RequestsPerMinute)),
		RequestsPerDay:         nullableNonNegativeInt(firstIntPtr(req.RequestsPerDay, current.RequestsPerDay)),
		DailyBudget:            firstNonEmpty(derefString(req.DailyBudget), derefString(current.DailyBudget)),
		MonthlyBudget:          firstNonEmpty(derefString(req.MonthlyBudget), derefString(current.MonthlyBudget)),
		Currency:               currency,
		FailureThreshold:       nullableNonNegativeInt(firstIntPtr(req.FailureThreshold, current.FailureThreshold)),
		FailureWindowSeconds:   nullableNonNegativeInt(firstIntPtr(req.FailureWindowSeconds, current.FailureWindowSeconds)),
		CircuitCooldownSeconds: nullableNonNegativeInt(firstIntPtr(req.CircuitCooldownSeconds, current.CircuitCooldownSeconds)),
		Enabled:                enabled,
	})
}

func (s *Service) normalizeLimitPolicyFields(ctx context.Context, organizationID string, policy normalizedLimitPolicy) (normalizedLimitPolicy, error) {
	policy.TaskType = strings.TrimSpace(policy.TaskType)
	if !validProviderLimitTaskType(policy.TaskType) {
		return normalizedLimitPolicy{}, fmt.Errorf("%w: taskType is invalid", ErrValidation)
	}
	policy.Currency = currencyOrDefault(policy.Currency)
	if err := validateBudget(policy.DailyBudget); err != nil {
		return normalizedLimitPolicy{}, fmt.Errorf("%w: dailyBudget must be non-negative", ErrValidation)
	}
	if err := validateBudget(policy.MonthlyBudget); err != nil {
		return normalizedLimitPolicy{}, fmt.Errorf("%w: monthlyBudget must be non-negative", ErrValidation)
	}
	if hasNegativeLimit(policy.MaxConcurrency, policy.RequestsPerMinute, policy.RequestsPerDay, policy.FailureThreshold, policy.FailureWindowSeconds, policy.CircuitCooldownSeconds) {
		return normalizedLimitPolicy{}, fmt.Errorf("%w: numeric limit fields must be non-negative", ErrValidation)
	}
	if policy.ProviderAccountID != "" {
		if _, err := s.GetAccount(ctx, organizationID, policy.ProviderAccountID); err != nil {
			return normalizedLimitPolicy{}, err
		}
	}
	if policy.ProviderModelID != "" {
		model, err := s.GetModel(ctx, organizationID, policy.ProviderModelID)
		if err != nil {
			return normalizedLimitPolicy{}, err
		}
		if policy.ProviderAccountID != "" && model.ProviderAccountID != policy.ProviderAccountID {
			return normalizedLimitPolicy{}, fmt.Errorf("%w: providerModelId must belong to providerAccountId", ErrValidation)
		}
	}
	return policy, nil
}

func scanEffectiveLimitPolicy(row rowScanner) (effectiveLimitPolicy, error) {
	var policy effectiveLimitPolicy
	var accountID, modelID sql.NullString
	var maxConcurrency, rpm, rpd, failureThreshold, failureWindow, cooldown sql.NullInt64
	var dailyBudget, monthlyBudget sql.NullString
	err := row.Scan(
		&policy.ID,
		&accountID,
		&modelID,
		&policy.TaskType,
		&maxConcurrency,
		&rpm,
		&rpd,
		&dailyBudget,
		&monthlyBudget,
		&policy.Currency,
		&failureThreshold,
		&failureWindow,
		&cooldown,
	)
	policy.ProviderAccountID = nullStringText(accountID)
	policy.ProviderModelID = nullStringText(modelID)
	policy.MaxConcurrency = intPtrFromNull(maxConcurrency)
	policy.RequestsPerMinute = intPtrFromNull(rpm)
	policy.RequestsPerDay = intPtrFromNull(rpd)
	policy.DailyBudget = stringPtr(dailyBudget)
	policy.MonthlyBudget = stringPtr(monthlyBudget)
	policy.FailureThreshold = intPtrFromNull(failureThreshold)
	policy.FailureWindowSeconds = intPtrFromNull(failureWindow)
	policy.CircuitCooldownSeconds = intPtrFromNull(cooldown)
	return policy, err
}

func scanProviderLimitPolicy(row rowScanner) (ProviderLimitPolicy, error) {
	var item ProviderLimitPolicy
	var accountID, modelID, createdBy sql.NullString
	var maxConcurrency, rpm, rpd, failureThreshold, failureWindow, cooldown sql.NullInt64
	var dailyBudget, monthlyBudget sql.NullString
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&accountID,
		&modelID,
		&item.TaskType,
		&maxConcurrency,
		&rpm,
		&rpd,
		&dailyBudget,
		&monthlyBudget,
		&item.Currency,
		&failureThreshold,
		&failureWindow,
		&cooldown,
		&item.Enabled,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.ProviderAccountID = stringPtr(accountID)
	item.ProviderModelID = stringPtr(modelID)
	item.MaxConcurrency = intPtrFromNull(maxConcurrency)
	item.RequestsPerMinute = intPtrFromNull(rpm)
	item.RequestsPerDay = intPtrFromNull(rpd)
	item.DailyBudget = stringPtr(dailyBudget)
	item.MonthlyBudget = stringPtr(monthlyBudget)
	item.FailureThreshold = intPtrFromNull(failureThreshold)
	item.FailureWindowSeconds = intPtrFromNull(failureWindow)
	item.CircuitCooldownSeconds = intPtrFromNull(cooldown)
	item.CreatedBy = stringPtr(createdBy)
	return item, err
}

func scanProviderCircuitState(row rowScanner) (ProviderCircuitState, error) {
	var item ProviderCircuitState
	var modelID, errorCode, errorMessage sql.NullString
	var openedAt, halfOpenAt, nextAttemptAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProviderAccountID,
		&modelID,
		&item.TaskType,
		&item.State,
		&item.FailureCount,
		&item.SuccessCount,
		&openedAt,
		&halfOpenAt,
		&nextAttemptAt,
		&errorCode,
		&errorMessage,
		&item.UpdatedAt,
	)
	item.ProviderModelID = stringPtr(modelID)
	if openedAt.Valid {
		item.OpenedAt = &openedAt.Time
	}
	if halfOpenAt.Valid {
		item.HalfOpenAt = &halfOpenAt.Time
	}
	if nextAttemptAt.Valid {
		item.NextAttemptAt = &nextAttemptAt.Time
	}
	item.LastErrorCode = stringPtr(errorCode)
	item.LastErrorMessage = stringPtr(errorMessage)
	return item, err
}

func normalizeGuardRequest(req GuardRequest) GuardRequest {
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.ProviderAccountID = strings.TrimSpace(req.ProviderAccountID)
	req.ProviderModelID = strings.TrimSpace(req.ProviderModelID)
	req.TaskType = strings.TrimSpace(req.TaskType)
	req.Currency = currencyOrDefault(req.Currency)
	req.AcquiredByService = strings.TrimSpace(req.AcquiredByService)
	if req.AcquiredByService == "" {
		req.AcquiredByService = "provider-gateway"
	}
	if req.LeaseTTL <= 0 {
		req.LeaseTTL = defaultLeaseTTL
	}
	return req
}

func guardLockKey(req GuardRequest) string {
	return strings.Join([]string{req.OrganizationID, req.ProviderAccountID, req.ProviderModelID, req.TaskType}, ":")
}

type providerCircuitKey struct {
	ProviderAccountID string
	ProviderModelID   string
}

func circuitKeyFor(req GuardRequest, policy effectiveLimitPolicy) providerCircuitKey {
	key := providerCircuitKey{ProviderAccountID: req.ProviderAccountID}
	if policy.ProviderModelID != "" {
		key.ProviderModelID = req.ProviderModelID
	}
	return key
}

func scopeAccountID(req GuardRequest, policy effectiveLimitPolicy) string {
	if policy.ProviderAccountID != "" || policy.ProviderModelID != "" {
		return req.ProviderAccountID
	}
	return ""
}

func scopeModelID(req GuardRequest, policy effectiveLimitPolicy) string {
	if policy.ProviderModelID != "" {
		return req.ProviderModelID
	}
	return ""
}

func budgetScope(policy effectiveLimitPolicy) string {
	if policy.ProviderModelID != "" {
		return "model"
	}
	if policy.ProviderAccountID != "" {
		return "account"
	}
	return "org"
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func intPtrFromNull(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	parsed := int(value.Int64)
	return &parsed
}

func nullableNonNegativeInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func hasNegativeLimit(values ...any) bool {
	for _, value := range values {
		if typed, ok := value.(int); ok && typed < 0 {
			return true
		}
	}
	return false
}

func firstIntPtr(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func decimalValue(raw string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return parsed
}

func validateBudget(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		return fmt.Errorf("invalid budget")
	}
	return nil
}

func validProviderLimitTaskType(taskType string) bool {
	switch strings.TrimSpace(taskType) {
	case TaskTypeAny, TaskTypeTextGenerate, TaskTypeTextStream, TaskTypeImageGenerate, TaskTypeVideoCreateTask, TaskTypeVideoPollTask, TaskTypeVideoCancelTask:
		return true
	default:
		return false
	}
}

func newGuardError(code, message string, retryable bool, retryAfterMs int) error {
	return &ProviderGuardError{Standard: StandardError{
		Code:         code,
		Message:      message,
		Retryable:    retryable,
		RetryAfterMs: retryAfterMs,
	}}
}

func standardErrorFromGuard(err error) (*StandardError, bool) {
	var guardErr *ProviderGuardError
	if errors.As(err, &guardErr) {
		return &guardErr.Standard, true
	}
	return nil, false
}

func retryAfterMs(now, next time.Time) int {
	if !next.After(now) {
		return 0
	}
	return int(next.Sub(now).Milliseconds())
}
