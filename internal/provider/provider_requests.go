package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/jackc/pgx/v5"
)

type ProviderRequest struct {
	ID                         string          `json:"id"`
	OrganizationID             string          `json:"organizationId"`
	ProjectID                  *string         `json:"projectId,omitempty"`
	WorkflowRunID              *string         `json:"workflowRunId,omitempty"`
	NodeRunID                  *string         `json:"nodeRunId,omitempty"`
	TaskType                   string          `json:"taskType"`
	IdempotencyKey             *string         `json:"idempotencyKey,omitempty"`
	RequestHash                string          `json:"requestHash"`
	HashSchemaVersion          int             `json:"hashSchemaVersion"`
	Status                     string          `json:"status"`
	AttemptGeneration          int             `json:"attemptGeneration"`
	ResultSnapshot             json.RawMessage `json:"resultSnapshot"`
	ArtifactIDs                json.RawMessage `json:"artifactIds"`
	MediaFileIDs               json.RawMessage `json:"mediaFileIds"`
	ErrorCode                  *string         `json:"errorCode,omitempty"`
	ErrorMessage               *string         `json:"errorMessage,omitempty"`
	RequestedByUserID          *string         `json:"requestedByUserId,omitempty"`
	BillingContextID           *string         `json:"billingContextId,omitempty"`
	BillingContextRevision     *int64          `json:"billingContextRevision,omitempty"`
	BillingContextSnapshotHash *string         `json:"billingContextSnapshotHash,omitempty"`
	ExpiresAt                  *time.Time      `json:"expiresAt,omitempty"`
	CreatedAt                  time.Time       `json:"createdAt"`
	StartedAt                  *time.Time      `json:"startedAt,omitempty"`
	CompletedAt                *time.Time      `json:"completedAt,omitempty"`
	UpdatedAt                  time.Time       `json:"updatedAt"`
}

type providerRequestStartInput struct {
	OrganizationID                 string
	ProjectID                      string
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
	WorkflowRunID                  string
	NodeRunID                      string
	OperationID                    string
	OperationItemID                string
	OperationItemAttempt           int
	ExecutionPlanID                string
	RenderSegmentID                string
	TaskType                       string
	IdempotencyKey                 string
	RequestHash                    string
	HashSchemaVersion              int
	Retry                          bool
	RequestedByUserID              string
	BillingContextID               string
	BillingContextRevision         int64
	BillingContextSnapshotHash     string
}

type providerRequestDisposition string

const (
	providerRequestExecute    providerRequestDisposition = "execute"
	providerRequestReplay     providerRequestDisposition = "replay"
	providerRequestInProgress providerRequestDisposition = "in_progress"
)

type providerRequestStart struct {
	Request     ProviderRequest
	Disposition providerRequestDisposition
}

func (s *Service) beginProviderRequest(ctx context.Context, input providerRequestStartInput) (providerRequestStart, error) {
	input.OrganizationID = strings.TrimSpace(input.OrganizationID)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ProductionGenerationID = strings.TrimSpace(input.ProductionGenerationID)
	input.VideoProductionBindingID = strings.TrimSpace(input.VideoProductionBindingID)
	input.WorkflowRunID = strings.TrimSpace(input.WorkflowRunID)
	input.NodeRunID = strings.TrimSpace(input.NodeRunID)
	input.OperationID = strings.TrimSpace(input.OperationID)
	input.OperationItemID = strings.TrimSpace(input.OperationItemID)
	input.ExecutionPlanID = strings.TrimSpace(input.ExecutionPlanID)
	input.RenderSegmentID = strings.TrimSpace(input.RenderSegmentID)
	input.TaskType = strings.TrimSpace(input.TaskType)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.RequestHash = strings.TrimSpace(input.RequestHash)
	input.RequestedByUserID = strings.TrimSpace(input.RequestedByUserID)
	input.BillingContextID = strings.TrimSpace(input.BillingContextID)
	input.BillingContextSnapshotHash = strings.TrimSpace(input.BillingContextSnapshotHash)
	if input.HashSchemaVersion <= 0 {
		input.HashSchemaVersion = gatewayRequestHashSchemaVersion
	}
	if input.OrganizationID == "" || input.TaskType == "" || input.RequestHash == "" {
		return providerRequestStart{}, fmt.Errorf("%w: organizationId, taskType, and requestHash are required", ErrValidation)
	}
	if (input.OperationItemID == "") != (input.OperationItemAttempt <= 0) {
		return providerRequestStart{}, fmt.Errorf("%w: operationItemId and operationItemAttempt must be provided together", ErrValidation)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return providerRequestStart{}, err
	}
	defer tx.Rollback(ctx)

	created, err := insertProviderRequest(ctx, tx, input)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return providerRequestStart{}, err
		}
		observability.RecordProviderIdempotency("execute")
		observability.RecordProviderRequest(created.TaskType, created.Status, created.AttemptGeneration)
		logProviderRequestTransition(ctx, created, "created")
		return providerRequestStart{Request: created, Disposition: providerRequestExecute}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return providerRequestStart{}, err
	}

	existing, err := selectProviderRequestForUpdate(ctx, tx, input.OrganizationID, input.TaskType, input.IdempotencyKey)
	if err != nil {
		return providerRequestStart{}, err
	}
	if existing.RequestHash != input.RequestHash {
		observability.RecordProviderIdempotency("hash_conflict")
		logProviderRequestTransition(ctx, existing, "hash_conflict")
		return providerRequestStart{}, &StandardErrorError{Standard: StandardError{
			Code:      CodeProviderIdempotencyConflict,
			Message:   "idempotency key was already used with a different provider request",
			Retryable: false,
		}}
	}

	disposition := providerRequestReplay
	switch existing.Status {
	case "pending", "running":
		disposition = providerRequestInProgress
		observability.RecordProviderIdempotency("in_progress")
	case "failed", "cancelled", "unknown_outcome":
		if input.Retry {
			existing, err = restartProviderRequest(ctx, tx, existing.ID, existing.AttemptGeneration)
			if err != nil {
				return providerRequestStart{}, err
			}
			disposition = providerRequestExecute
			observability.RecordProviderIdempotency("explicit_retry")
			observability.RecordProviderRequest(existing.TaskType, existing.Status, existing.AttemptGeneration)
		}
	}
	if disposition == providerRequestReplay {
		observability.RecordProviderIdempotency("dedupe_hit")
	}
	if err := tx.Commit(ctx); err != nil {
		return providerRequestStart{}, err
	}
	logProviderRequestTransition(ctx, existing, string(disposition))
	return providerRequestStart{Request: existing, Disposition: disposition}, nil
}

func insertProviderRequest(ctx context.Context, tx pgx.Tx, input providerRequestStartInput) (ProviderRequest, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO provider_requests(
			organization_id, project_id, workflow_run_id, node_run_id,
			production_generation_id, video_production_binding_id, video_production_binding_revision,
			operation_id, operation_item_id, operation_item_attempt,
			video_render_plan_id, video_render_segment_id,
			task_type, idempotency_key, request_hash, status,
			attempt_generation, hash_schema_version, started_at, updated_at,
			requested_by_user_id, billing_context_id,
			billing_context_revision, billing_context_snapshot_hash
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, NULLIF($7, 0),
		        NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, NULLIF($10, 0),
		        NULLIF($11, '')::uuid, NULLIF($12, '')::uuid,
		        $13, $14, $15, 'running', 1, $16, now(), now(),
		        NULLIF($17, '')::uuid, NULLIF($18, '')::uuid,
		        NULLIF($19, 0), NULLIF($20, ''))
		ON CONFLICT (organization_id, task_type, idempotency_key)
			WHERE idempotency_key IS NOT NULL
		DO NOTHING
		RETURNING
			id, organization_id, project_id, workflow_run_id, node_run_id,
			task_type, idempotency_key, request_hash, status, attempt_generation,
			result_snapshot, artifact_ids, media_file_ids, error_code, error_message,
			expires_at, created_at, started_at, completed_at, updated_at, hash_schema_version,
			requested_by_user_id, billing_context_id,
			billing_context_revision, billing_context_snapshot_hash
	`, input.OrganizationID, nullString(input.ProjectID), nullString(input.WorkflowRunID), nullString(input.NodeRunID),
		input.ProductionGenerationID, input.VideoProductionBindingID, input.VideoProductionBindingRevision,
		input.OperationID, input.OperationItemID, input.OperationItemAttempt,
		input.ExecutionPlanID, input.RenderSegmentID,
		input.TaskType, nullString(input.IdempotencyKey), input.RequestHash, input.HashSchemaVersion,
		input.RequestedByUserID, input.BillingContextID,
		input.BillingContextRevision, input.BillingContextSnapshotHash)
	return scanProviderRequest(row)
}

func selectProviderRequestForUpdate(ctx context.Context, tx pgx.Tx, organizationID, taskType, idempotencyKey string) (ProviderRequest, error) {
	row := tx.QueryRow(ctx, `
		SELECT
			id, organization_id, project_id, workflow_run_id, node_run_id,
			task_type, idempotency_key, request_hash, status, attempt_generation,
			result_snapshot, artifact_ids, media_file_ids, error_code, error_message,
			expires_at, created_at, started_at, completed_at, updated_at, hash_schema_version,
			requested_by_user_id, billing_context_id,
			billing_context_revision, billing_context_snapshot_hash
		FROM provider_requests
		WHERE organization_id = $1 AND task_type = $2 AND idempotency_key = $3
		FOR UPDATE
	`, organizationID, taskType, idempotencyKey)
	return scanProviderRequest(row)
}

func restartProviderRequest(ctx context.Context, tx pgx.Tx, requestID string, generation int) (ProviderRequest, error) {
	row := tx.QueryRow(ctx, `
		UPDATE provider_requests
		SET status = 'running',
			attempt_generation = attempt_generation + 1,
			result_snapshot = '{}'::jsonb,
			artifact_ids = '[]'::jsonb,
			media_file_ids = '[]'::jsonb,
			error_code = NULL,
			error_message = NULL,
			started_at = now(),
			completed_at = NULL,
			updated_at = now()
		WHERE id = $1 AND attempt_generation = $2
		RETURNING
			id, organization_id, project_id, workflow_run_id, node_run_id,
			task_type, idempotency_key, request_hash, status, attempt_generation,
			result_snapshot, artifact_ids, media_file_ids, error_code, error_message,
			expires_at, created_at, started_at, completed_at, updated_at, hash_schema_version,
			requested_by_user_id, billing_context_id,
			billing_context_revision, billing_context_snapshot_hash
	`, requestID, generation)
	return scanProviderRequest(row)
}

func (s *Service) completeProviderRequest(ctx context.Context, requestID string, generation int, status string, result any, artifactIDs, mediaFileIDs []string, standard *StandardError) error {
	status = providerRequestTerminalStatus(status)
	resultSnapshot, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if !json.Valid(resultSnapshot) || len(resultSnapshot) == 0 || resultSnapshot[0] != '{' {
		return fmt.Errorf("%w: provider request result must be a JSON object", ErrValidation)
	}
	artifactSnapshot, err := json.Marshal(nonNilStrings(artifactIDs))
	if err != nil {
		return err
	}
	mediaSnapshot, err := json.Marshal(nonNilStrings(mediaFileIDs))
	if err != nil {
		return err
	}
	var errorCode, errorMessage any
	if standard != nil {
		errorCode = nullString(standard.Code)
		errorMessage = nullString(standard.Message)
	}
	var taskType string
	row := s.db.QueryRow(ctx, `
		UPDATE provider_requests
		SET status = $3,
			result_snapshot = $4,
			artifact_ids = $5,
			media_file_ids = $6,
			error_code = $7,
			error_message = $8,
			completed_at = now(),
			updated_at = now()
		WHERE id = $1 AND attempt_generation = $2 AND status = 'running'
		RETURNING task_type
	`, requestID, generation, status, resultSnapshot, artifactSnapshot, mediaSnapshot, errorCode, errorMessage)
	if scanErr := row.Scan(&taskType); scanErr != nil {
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		return &StandardErrorError{Standard: StandardError{
			Code:      CodeProviderUnknownOutcome,
			Message:   "provider request was no longer running when its result was persisted",
			Retryable: false,
		}}
	}
	observability.RecordProviderRequest(taskType, status, generation)
	correlation := []any{
		"providerRequestId", requestID,
		"taskType", taskType,
		"status", status,
		"attempt", generation,
	}
	if providerCallID := stringFieldFromJSON(resultSnapshot, "providerCallId"); providerCallID != "" {
		correlation = append(correlation, "providerCallId", providerCallID)
	}
	if len(artifactIDs) > 0 {
		correlation = append(correlation, "artifactIds", nonNilStrings(artifactIDs))
	}
	if len(mediaFileIDs) > 0 {
		correlation = append(correlation, "mediaFileIds", nonNilStrings(mediaFileIDs))
	}
	observability.Log(ctx, slog.LevelInfo, "provider request completed", correlation...)
	return nil
}

func (s *Service) markProviderRequestUnknown(ctx context.Context, requestID string, generation int, cause error) error {
	message := "provider request outcome could not be confirmed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = strings.TrimSpace(cause.Error())
	}
	var taskType string
	err := s.db.QueryRow(ctx, `
		WITH request_update AS (
			UPDATE provider_requests
			SET status = 'unknown_outcome',
				error_code = $3,
				error_message = $4,
				completed_at = now(),
				updated_at = now()
			WHERE id = $1 AND attempt_generation = $2 AND status = 'running'
			RETURNING id, task_type
		), call_update AS (
			UPDATE provider_call_logs
			SET status = 'unknown_outcome',
				error_code = $3,
				error_message = $4,
				completed_at = now()
			WHERE provider_request_id IN (SELECT id FROM request_update)
				AND attempt_generation = $2
				AND status = 'running'
			RETURNING id
		)
		SELECT task_type FROM request_update
	`, requestID, generation, CodeProviderUnknownOutcome, message).Scan(&taskType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	observability.RecordProviderRequest(taskType, "unknown_outcome", generation)
	observability.RecordProviderIdempotency("unknown_outcome")
	observability.Log(ctx, slog.LevelWarn, "provider request outcome is unknown",
		"providerRequestId", requestID,
		"taskType", taskType,
		"status", "unknown_outcome",
		"attempt", generation,
		"errorCode", CodeProviderUnknownOutcome,
	)
	return nil
}

func (s *Service) ReconcileStaleProviderRequests(ctx context.Context, staleAfter time.Duration, limit int) (int64, error) {
	if staleAfter <= 0 {
		staleAfter = 30 * time.Minute
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var runningCount int64
	var oldestRunningAge float64
	if metricErr := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(EXTRACT(EPOCH FROM now() - min(started_at)), 0)
		FROM provider_requests
		WHERE status = 'running'
	`).Scan(&runningCount, &oldestRunningAge); metricErr == nil {
		observability.SetProviderRequestRuntime(runningCount, oldestRunningAge)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, attempt_generation, task_type
		FROM provider_requests
		WHERE status = 'running' AND updated_at < now() - ($1 * interval '1 millisecond')
		ORDER BY updated_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, staleAfter.Milliseconds(), limit)
	if err != nil {
		return 0, err
	}
	type staleRequest struct {
		ID         string
		Generation int
		TaskType   string
	}
	stale := make([]staleRequest, 0, limit)
	for rows.Next() {
		var item staleRequest
		if err := rows.Scan(&item.ID, &item.Generation, &item.TaskType); err != nil {
			rows.Close()
			return 0, err
		}
		stale = append(stale, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, item := range stale {
		if _, err := tx.Exec(ctx, `
			UPDATE provider_requests
			SET status = 'unknown_outcome',
				error_code = $3,
				error_message = 'provider gateway stopped before the result was durably confirmed',
				completed_at = now(),
				updated_at = now()
			WHERE id = $1 AND attempt_generation = $2 AND status = 'running'
		`, item.ID, item.Generation, CodeProviderUnknownOutcome); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE provider_call_logs
			SET status = 'unknown_outcome',
				error_code = $3,
				error_message = 'provider gateway stopped before the result was durably confirmed',
				completed_at = now()
			WHERE provider_request_id = $1
				AND attempt_generation = $2
				AND status = 'running'
		`, item.ID, item.Generation, CodeProviderUnknownOutcome); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	for _, item := range stale {
		observability.RecordProviderRequest(item.TaskType, "unknown_outcome", item.Generation)
		observability.RecordProviderIdempotency("unknown_outcome")
	}
	return int64(len(stale)), nil
}

func logProviderRequestTransition(ctx context.Context, request ProviderRequest, transition string) {
	args := []any{
		"providerRequestId", request.ID,
		"taskType", request.TaskType,
		"status", request.Status,
		"attempt", request.AttemptGeneration,
		"transition", transition,
	}
	if request.WorkflowRunID != nil {
		args = append(args, "workflowRunId", *request.WorkflowRunID)
	}
	if request.NodeRunID != nil {
		args = append(args, "nodeRunId", *request.NodeRunID)
	}
	observability.Log(ctx, slog.LevelInfo, "provider request transition", args...)
}

func scanProviderRequest(row rowScanner) (ProviderRequest, error) {
	var item ProviderRequest
	var projectID, workflowRunID, nodeRunID, idempotencyKey sql.NullString
	var errorCode, errorMessage sql.NullString
	var requestedByUserID, billingContextID, billingContextSnapshotHash sql.NullString
	var billingContextRevision sql.NullInt64
	var resultSnapshot, artifactIDs, mediaFileIDs []byte
	var expiresAt, startedAt, completedAt sql.NullTime
	err := row.Scan(
		&item.ID, &item.OrganizationID, &projectID, &workflowRunID, &nodeRunID,
		&item.TaskType, &idempotencyKey, &item.RequestHash, &item.Status, &item.AttemptGeneration,
		&resultSnapshot, &artifactIDs, &mediaFileIDs, &errorCode, &errorMessage,
		&expiresAt, &item.CreatedAt, &startedAt, &completedAt, &item.UpdatedAt, &item.HashSchemaVersion,
		&requestedByUserID, &billingContextID,
		&billingContextRevision, &billingContextSnapshotHash,
	)
	item.ProjectID = stringPtr(projectID)
	item.WorkflowRunID = stringPtr(workflowRunID)
	item.NodeRunID = stringPtr(nodeRunID)
	item.IdempotencyKey = stringPtr(idempotencyKey)
	item.ErrorCode = stringPtr(errorCode)
	item.ErrorMessage = stringPtr(errorMessage)
	item.RequestedByUserID = stringPtr(requestedByUserID)
	item.BillingContextID = stringPtr(billingContextID)
	item.BillingContextSnapshotHash = stringPtr(billingContextSnapshotHash)
	if billingContextRevision.Valid {
		item.BillingContextRevision = &billingContextRevision.Int64
	}
	item.ResultSnapshot = rawOrDefault(resultSnapshot, "{}")
	item.ArtifactIDs = rawOrDefault(artifactIDs, "[]")
	item.MediaFileIDs = rawOrDefault(mediaFileIDs, "[]")
	if expiresAt.Valid {
		item.ExpiresAt = &expiresAt.Time
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, err
}

const gatewayRequestHashSchemaVersion = 2

func gatewayRequestHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	removeProviderExecutionFields(normalized)
	canonical, err := json.Marshal(map[string]any{
		"schemaVersion": gatewayRequestHashSchemaVersion,
		"request":       normalized,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func removeProviderExecutionFields(value any) {
	typed, ok := value.(map[string]any)
	if !ok {
		return
	}
	delete(typed, "idempotencyKey")
	delete(typed, "retry")
	if options, ok := typed["options"].(map[string]any); ok {
		delete(options, "idempotencyKey")
		delete(options, "retry")
	}
}

func decodeProviderRequestResult[T any](request ProviderRequest) (T, error) {
	var result T
	if err := json.Unmarshal(request.ResultSnapshot, &result); err != nil {
		return result, fmt.Errorf("decode provider request result: %w", err)
	}
	return result, nil
}

func providerRequestStatusError(request ProviderRequest) *StandardError {
	code := CodeProviderRequestInProgress
	message := "provider request is already running"
	retryable := true
	if request.Status == "unknown_outcome" {
		code = CodeProviderUnknownOutcome
		message = "provider request outcome is unknown; an explicit retry is required"
		retryable = false
	}
	if request.ErrorCode != nil && strings.TrimSpace(*request.ErrorCode) != "" {
		code = *request.ErrorCode
	}
	if request.ErrorMessage != nil && strings.TrimSpace(*request.ErrorMessage) != "" {
		message = *request.ErrorMessage
	}
	return &StandardError{Code: code, Message: message, Retryable: retryable}
}

func providerRequestTerminalStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "succeeded":
		return "succeeded"
	case "cancelled":
		return "cancelled"
	case "unknown_outcome":
		return "unknown_outcome"
	default:
		return "failed"
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
