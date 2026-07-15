package workflows

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCancellationReconcilerTerminalizesStuckProviderTask(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run cancellation reconciler integration tests")
	}
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	defer pool.Close()
	organizationID, _, projectID, workflowRunID, providerModelID, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	var providerAccountID string
	if err := pool.QueryRow(ctx, `SELECT provider_account_id::text FROM provider_models WHERE id = $1`, providerModelID).Scan(&providerAccountID); err != nil {
		t.Fatalf("load provider account: %v", err)
	}
	var nodeRunID, executionToken string
	var attemptGeneration int
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type,
			status, input, output, started_at
		)
		VALUES ($1, $2, $3, 'cancel-stuck', 'video.generate', 'running', '{}', '{}', now())
		RETURNING id::text, execution_token::text, attempt_generation
	`, organizationID, projectID, workflowRunID).Scan(&nodeRunID, &executionToken, &attemptGeneration); err != nil {
		t.Fatalf("insert node run: %v", err)
	}
	var providerRequestID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_requests(
			organization_id, project_id, workflow_run_id, node_run_id, task_type,
			idempotency_key, request_hash, status, started_at
		)
		VALUES ($1, $2, $3, $4, 'video.create_task', 'cancel-stuck', 'cancel-stuck-hash', 'running', now())
		RETURNING id::text
	`, organizationID, projectID, workflowRunID, nodeRunID).Scan(&providerRequestID); err != nil {
		t.Fatalf("insert provider request: %v", err)
	}
	var providerCallID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_call_logs(
			organization_id, project_id, workflow_run_id, node_run_id, provider_request_id,
			provider_account_id, provider_model_id, task_type, execution_mode, status,
			request_hash, request_snapshot, started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'video.create_task', 'async_create', 'running',
		        'cancel-stuck-hash', '{}', now())
		RETURNING id::text
	`, organizationID, projectID, workflowRunID, nodeRunID, providerRequestID, providerAccountID, providerModelID).Scan(&providerCallID); err != nil {
		t.Fatalf("insert provider call: %v", err)
	}
	var providerTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_async_tasks(
			provider_call_id, provider_request_id, organization_id, project_id, workflow_run_id, node_run_id,
			node_execution_token, node_attempt_generation,
			provider_account_id, provider_model_id, external_task_id, status, task_type, execution_mode,
			input, raw_status, started_at, cancellation_requested_at, cancellation_deadline_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'external-stuck', 'cancelling',
		        'video.generate', 'async_polling', '{}', '{}', now(), now() - interval '3 minutes', now() - interval '1 minute')
		RETURNING id::text
	`, providerCallID, providerRequestID, organizationID, projectID, workflowRunID, nodeRunID, executionToken, attemptGeneration, providerAccountID, providerModelID).Scan(&providerTaskID); err != nil {
		t.Fatalf("insert provider async task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelling', cancellation_requested_at = now() - interval '3 minutes',
		    cancellation_deadline_at = now() - interval '1 minute', error_message = 'user cancelled'
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("mark workflow cancelling: %v", err)
	}

	count, err := ReconcileExpiredWorkflowCancellations(ctx, pool, 10)
	if err != nil {
		t.Fatalf("reconcile expired cancellations: %v", err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d, want 1", count)
	}
	var workflowStatus, nodeStatus, taskStatus, requestStatus, callStatus, taskErrorCode string
	var settled bool
	if err := pool.QueryRow(ctx, `SELECT status, settled_at IS NOT NULL FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&workflowStatus, &settled); err != nil {
		t.Fatalf("load workflow status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_node_runs WHERE id = $1`, nodeRunID).Scan(&nodeStatus); err != nil {
		t.Fatalf("load node status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status, cancellation_error_code FROM provider_async_tasks WHERE id = $1`, providerTaskID).Scan(&taskStatus, &taskErrorCode); err != nil {
		t.Fatalf("load provider task status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM provider_requests WHERE id = $1`, providerRequestID).Scan(&requestStatus); err != nil {
		t.Fatalf("load provider request status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM provider_call_logs WHERE id = $1`, providerCallID).Scan(&callStatus); err != nil {
		t.Fatalf("load provider call status: %v", err)
	}
	if workflowStatus != "cancelled" || nodeStatus != "cancelled" || taskStatus != "unknown_outcome" || requestStatus != "unknown_outcome" || callStatus != "unknown_outcome" {
		t.Fatalf("statuses workflow=%s node=%s task=%s request=%s call=%s", workflowStatus, nodeStatus, taskStatus, requestStatus, callStatus)
	}
	if !settled {
		t.Fatal("reconciled workflow did not record settled_at")
	}
	if !strings.Contains(taskErrorCode, "OUTCOME_UNKNOWN") {
		t.Fatalf("task cancellation error code = %q", taskErrorCode)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM event_outbox
		WHERE project_id = $1 AND aggregate_id = $2 AND event_type = 'provider.video.task.cancel_failed'
	`, projectID, providerTaskID).Scan(&eventCount); err != nil {
		t.Fatalf("count cancellation warning events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("cancellation warning event count = %d, want 1", eventCount)
	}
}
