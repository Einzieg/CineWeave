package workflows

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type expiredCancellation struct {
	WorkflowRunID  string
	OrganizationID string
	ProjectID      string
	Reason         string
}

type expiredProviderTask struct {
	ID             string
	OrganizationID string
	ProjectID      string
}

func ReconcileExpiredWorkflowCancellations(ctx context.Context, pool *pgxpool.Pool, limit int) (int, error) {
	if pool == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 32
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT id::text, organization_id::text, project_id::text, COALESCE(error_message, '')
		FROM workflow_runs
		WHERE status = 'cancelling'
		  AND cancellation_deadline_at IS NOT NULL
		  AND cancellation_deadline_at <= now()
		ORDER BY cancellation_deadline_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}
	items := make([]expiredCancellation, 0, limit)
	for rows.Next() {
		var item expiredCancellation
		if err := rows.Scan(&item.WorkflowRunID, &item.OrganizationID, &item.ProjectID, &item.Reason); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, item := range items {
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = "workflow cancellation deadline elapsed"
		}
		taskRows, err := tx.Query(ctx, `
			WITH expired_tasks AS (
				UPDATE provider_async_tasks
				SET status = 'unknown_outcome',
				    cancellation_error_code = 'PROVIDER_CANCEL_OUTCOME_UNKNOWN',
				    cancellation_error_message = $2,
				    error_code = COALESCE(error_code, 'PROVIDER_CANCEL_OUTCOME_UNKNOWN'),
				    error_message = COALESCE(error_message, $2),
				    finalized_at = COALESCE(finalized_at, now()),
				    completed_at = COALESCE(completed_at, now()),
				    updated_at = now()
				WHERE workflow_run_id = $1
				  AND status IN ('queued', 'running', 'cancelling')
				RETURNING id, provider_request_id, provider_call_id, organization_id, project_id
			), request_updates AS (
				UPDATE provider_requests request
				SET status = 'unknown_outcome',
				    error_code = 'PROVIDER_CANCEL_OUTCOME_UNKNOWN',
				    error_message = $2,
				    completed_at = COALESCE(completed_at, now()),
				    updated_at = now()
				FROM expired_tasks task
				WHERE request.id = task.provider_request_id
				  AND request.status IN ('pending', 'running')
				RETURNING request.id
			), call_updates AS (
				UPDATE provider_call_logs call
				SET status = 'unknown_outcome',
				    error_code = COALESCE(call.error_code, 'PROVIDER_CANCEL_OUTCOME_UNKNOWN'),
				    error_message = COALESCE(call.error_message, $2),
				    completed_at = COALESCE(call.completed_at, now())
				FROM expired_tasks task
				WHERE call.id = task.provider_call_id
				  AND call.status IN ('queued', 'running')
				RETURNING call.id
			)
			SELECT id::text, organization_id::text, project_id::text
			FROM expired_tasks
		`, item.WorkflowRunID, reason)
		if err != nil {
			return 0, err
		}
		expiredTasks := make([]expiredProviderTask, 0)
		for taskRows.Next() {
			var task expiredProviderTask
			if err := taskRows.Scan(&task.ID, &task.OrganizationID, &task.ProjectID); err != nil {
				taskRows.Close()
				return 0, err
			}
			expiredTasks = append(expiredTasks, task)
		}
		if err := taskRows.Err(); err != nil {
			taskRows.Close()
			return 0, err
		}
		taskRows.Close()
		for _, task := range expiredTasks {
			if err := insertEvent(ctx, tx, task.OrganizationID, task.ProjectID, "provider.video.task.cancel_failed", "provider_async_task", task.ID, mustJSON(map[string]any{
				"providerAsyncTaskId": task.ID,
				"workflowRunId":       item.WorkflowRunID,
				"status":              "unknown_outcome",
				"code":                "PROVIDER_CANCEL_OUTCOME_UNKNOWN",
				"message":             reason,
			})); err != nil {
				return 0, err
			}
		}
		output, _ := json.Marshal(map[string]any{
			"status": "cancelled", "reason": reason, "reconciled": true,
		})
		if _, _, err := cancelWorkflowRunTx(ctx, tx, item.WorkflowRunID, output, reason, "CANCELLATION_DEADLINE_EXCEEDED"); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(items), nil
}
