package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type RuntimeOperation struct {
	ID                string          `json:"id"`
	OrganizationID    string          `json:"organizationId"`
	ProjectID         string          `json:"projectId,omitempty"`
	OperationType     string          `json:"operationType"`
	Status            string          `json:"status"`
	WorkflowRunID     string          `json:"workflowRunId,omitempty"`
	RequestHash       string          `json:"requestHash"`
	HashSchemaVersion int             `json:"hashSchemaVersion"`
	ResultSnapshot    json.RawMessage `json:"resultSnapshot,omitempty"`
	ErrorCode         string          `json:"errorCode,omitempty"`
	ErrorMessage      string          `json:"errorMessage,omitempty"`
	LeaseExpiresAt    *time.Time      `json:"leaseExpiresAt,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	CompletedAt       *time.Time      `json:"completedAt,omitempty"`
	ReconcileRequired bool            `json:"reconcileRequired"`
	RetryAllowed      bool            `json:"retryAllowed"`
}

func (s *Server) getRuntimeOperation(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRead)
	if !ok {
		return
	}
	operation, err := loadRuntimeOperation(r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("operationId"), false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, operation, nil)
}

func (s *Server) reconcileRuntimeOperation(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionWorkflowRun)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())

	operation, err := loadRuntimeOperation(r.Context(), tx, project.OrganizationID, project.ID, r.PathValue("operationId"), true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if operation.Status == "processing" && operation.LeaseExpiresAt != nil && operation.LeaseExpiresAt.After(time.Now()) {
		conflict := newAPIError(http.StatusConflict, "OPERATION_IN_PROGRESS", "operation lease is still active")
		conflict.Details = map[string]any{"operationId": operation.ID, "leaseExpiresAt": operation.LeaseExpiresAt}
		s.writeError(w, r, conflict)
		return
	}
	if operation.Status == "succeeded" || operation.Status == "failed_terminal" || operation.Status == "failed_retryable" {
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, operation, map[string]any{"reconciled": false})
		return
	}

	if operation.WorkflowRunID != "" {
		run, runErr := scanWorkflowRun(tx.QueryRow(r.Context(), workflowRunSelectSQL(`WHERE id = $1 AND organization_id = $2 AND project_id = $3`), operation.WorkflowRunID, project.OrganizationID, project.ID))
		if runErr == nil {
			resultSnapshot, marshalErr := json.Marshal(run)
			if marshalErr != nil {
				s.writeError(w, r, marshalErr)
				return
			}
			if _, err := tx.Exec(r.Context(), `
				UPDATE runtime_operations
				SET status = 'succeeded', result_snapshot = $2, error_code = NULL, error_message = NULL,
				    lease_expires_at = NULL, completed_at = COALESCE(completed_at, now()), updated_at = now()
				WHERE id = $1
			`, operation.ID, resultSnapshot); err != nil {
				s.writeError(w, r, err)
				return
			}
			if _, err := tx.Exec(r.Context(), `
				UPDATE idempotency_keys
				SET status = 'succeeded', response_status = $2, response_snapshot = $3,
				    error_code = NULL, error_message = NULL, lease_expires_at = NULL, updated_at = now()
				WHERE operation_id = $1
			`, operation.ID, http.StatusAccepted, resultSnapshot); err != nil {
				s.writeError(w, r, err)
				return
			}
			operation.Status = "succeeded"
			operation.ResultSnapshot = resultSnapshot
			operation.ErrorCode = ""
			operation.ErrorMessage = ""
			operation.LeaseExpiresAt = nil
			operation.ReconcileRequired = false
			operation.RetryAllowed = false
			now := time.Now().UTC()
			operation.CompletedAt = &now
			if err := tx.Commit(r.Context()); err != nil {
				s.writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, r, http.StatusOK, operation, map[string]any{"reconciled": true, "resolution": "workflow_found"})
			return
		}
		if !errors.Is(runErr, pgx.ErrNoRows) {
			s.writeError(w, r, runErr)
			return
		}
	}

	if localTransactionalOperation(operation.OperationType) {
		if _, err := tx.Exec(r.Context(), `
			UPDATE runtime_operations
			SET status = 'failed_retryable', error_code = 'OPERATION_NOT_COMMITTED',
			    error_message = 'no committed workflow or side effect was found', lease_expires_at = NULL,
			    completed_at = now(), updated_at = now()
			WHERE id = $1
		`, operation.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE idempotency_keys
			SET status = 'failed_retryable', response_status = NULL, response_snapshot = NULL,
			    error_code = 'OPERATION_NOT_COMMITTED', error_message = 'no committed workflow or side effect was found',
			    lease_expires_at = NULL, updated_at = now()
			WHERE operation_id = $1
		`, operation.ID); err != nil {
			s.writeError(w, r, err)
			return
		}
		operation.Status = "failed_retryable"
		operation.ErrorCode = "OPERATION_NOT_COMMITTED"
		operation.ErrorMessage = "no committed workflow or side effect was found"
		operation.LeaseExpiresAt = nil
		operation.ReconcileRequired = false
		operation.RetryAllowed = true
		now := time.Now().UTC()
		operation.CompletedAt = &now
		if err := tx.Commit(r.Context()); err != nil {
			s.writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, r, http.StatusOK, operation, map[string]any{"reconciled": true, "resolution": "not_committed"})
		return
	}

	conflict := newAPIError(http.StatusConflict, "OPERATION_RECONCILIATION_REQUIRED", "operation outcome cannot be proven automatically")
	conflict.Details = map[string]any{"operationId": operation.ID, "operationType": operation.OperationType, "status": operation.Status}
	s.writeError(w, r, conflict)
}

func localTransactionalOperation(operationType string) bool {
	return strings.HasPrefix(strings.TrimSpace(operationType), "asset-batches:") ||
		strings.HasPrefix(strings.TrimSpace(operationType), "workflow-runs:")
}

type runtimeOperationQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadRuntimeOperation(ctx context.Context, db runtimeOperationQueryer, organizationID, projectID, operationID string, forUpdate bool) (RuntimeOperation, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	row := db.QueryRow(ctx, `
		SELECT id::text, organization_id::text, COALESCE(project_id::text, ''), operation_type, status,
		       COALESCE(workflow_run_id::text, ''), request_hash, hash_schema_version,
		       COALESCE(result_snapshot, 'null'::jsonb), COALESCE(error_code, ''), COALESCE(error_message, ''),
		       lease_expires_at, created_at, updated_at, completed_at
		FROM runtime_operations
		WHERE id = $1 AND organization_id = $2 AND project_id = $3`+lock,
		operationID, organizationID, projectID,
	)
	var operation RuntimeOperation
	if err := row.Scan(
		&operation.ID, &operation.OrganizationID, &operation.ProjectID, &operation.OperationType, &operation.Status,
		&operation.WorkflowRunID, &operation.RequestHash, &operation.HashSchemaVersion,
		&operation.ResultSnapshot, &operation.ErrorCode, &operation.ErrorMessage,
		&operation.LeaseExpiresAt, &operation.CreatedAt, &operation.UpdatedAt, &operation.CompletedAt,
	); err != nil {
		return RuntimeOperation{}, err
	}
	operation.ReconcileRequired = operation.Status == "unknown_outcome" || operation.Status == "processing"
	operation.RetryAllowed = operation.Status == "failed_retryable"
	if string(operation.ResultSnapshot) == "null" {
		operation.ResultSnapshot = nil
	}
	return operation, nil
}
