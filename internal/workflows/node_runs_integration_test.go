package workflows

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

func TestWorkflowWriteFenceIsNonRetryable(t *testing.T) {
	var applicationErr *temporal.ApplicationError
	if !errors.As(ErrWorkflowWriteFenced, &applicationErr) {
		t.Fatalf("workflow write fence type = %T, want *temporal.ApplicationError", ErrWorkflowWriteFenced)
	}
	if !applicationErr.NonRetryable() {
		t.Fatal("workflow write fence must be non-retryable")
	}
	if applicationErr.Type() != CodeWorkflowResultDiscarded {
		t.Fatalf("workflow write fence type = %q, want %q", applicationErr.Type(), CodeWorkflowResultDiscarded)
	}
}

func TestCompleteNodeRunCannotResurrectCancelledNode(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run node run integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for node run integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	organizationID, _, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
	})
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "late-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if err := CancelNodeRun(ctx, pool, execution.NodeRunID, []byte(`{"cancelled":true}`), "test cancellation"); err != nil {
		t.Fatalf("cancel node: %v", err)
	}
	if err := CompleteNodeRun(ctx, pool, execution, []byte(`{"late":true}`)); err != nil {
		t.Fatalf("late completion should be discarded without failing the activity: %v", err)
	}
	var status string
	var output string
	if err := pool.QueryRow(ctx, `SELECT status, output::text FROM workflow_node_runs WHERE id = $1`, execution.NodeRunID).Scan(&status, &output); err != nil {
		t.Fatalf("load node: %v", err)
	}
	if status != "cancelled" || strings.Contains(output, "late") {
		t.Fatalf("cancelled node was resurrected: status=%s output=%s", status, output)
	}
	assertWorkflowResultDiscardedEvent(t, ctx, pool, projectID, execution.NodeRunID, 1)
}

func TestRestartedNodeRotatesExecutionTokenAndFencesStaleAttempt(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run node run integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for node run integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	organizationID, _, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
	})
	input := NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "retry-node",
		NodeType:       "test",
	}
	staleExecution, err := StartNodeRun(ctx, pool, input)
	if err != nil {
		t.Fatalf("start stale execution: %v", err)
	}
	freshExecution, err := StartNodeRun(ctx, pool, input)
	if err != nil {
		t.Fatalf("restart node: %v", err)
	}
	if staleExecution.NodeRunID != freshExecution.NodeRunID {
		t.Fatalf("node id changed across restart: stale=%s fresh=%s", staleExecution.NodeRunID, freshExecution.NodeRunID)
	}
	if staleExecution.ExecutionToken == freshExecution.ExecutionToken {
		t.Fatal("execution token was not rotated on restart")
	}
	if err := CompleteNodeRun(ctx, pool, staleExecution, []byte(`{"stale":true}`)); err != nil {
		t.Fatalf("stale completion should be discarded: %v", err)
	}
	var status string
	var output string
	if err := pool.QueryRow(ctx, `SELECT status, output::text FROM workflow_node_runs WHERE id = $1`, freshExecution.NodeRunID).Scan(&status, &output); err != nil {
		t.Fatalf("load restarted node: %v", err)
	}
	if status != "running" || strings.Contains(output, "stale") {
		t.Fatalf("stale execution mutated restarted node: status=%s output=%s", status, output)
	}
	assertWorkflowResultDiscardedEvent(t, ctx, pool, projectID, staleExecution.NodeRunID, 1)
	if err := CompleteNodeRun(ctx, pool, freshExecution, []byte(`{"fresh":true}`)); err != nil {
		t.Fatalf("fresh completion: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status, output::text FROM workflow_node_runs WHERE id = $1`, freshExecution.NodeRunID).Scan(&status, &output); err != nil {
		t.Fatalf("load completed node: %v", err)
	}
	if status != "succeeded" || !strings.Contains(output, "fresh") {
		t.Fatalf("fresh execution did not complete node: status=%s output=%s", status, output)
	}
}

func TestFailedNodeCanRestartWithinWorkflowAttempt(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	input := NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "failed-retry-node",
		NodeType:       "test",
	}
	failedExecution, err := StartNodeRun(ctx, pool, input)
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if err := FailNodeRun(ctx, pool, failedExecution, "RETRYABLE", "retry this node"); err != nil {
		t.Fatalf("fail node: %v", err)
	}

	retryExecution, err := StartNodeRun(ctx, pool, input)
	if err != nil {
		t.Fatalf("restart failed node: %v", err)
	}
	if retryExecution.NodeRunID != failedExecution.NodeRunID {
		t.Fatalf("node id changed across failed retry: failed=%s retry=%s", failedExecution.NodeRunID, retryExecution.NodeRunID)
	}
	if retryExecution.ExecutionToken == failedExecution.ExecutionToken {
		t.Fatal("execution token was not rotated for failed retry")
	}
	if retryExecution.AttemptGeneration != failedExecution.AttemptGeneration {
		t.Fatalf("attempt generation changed: failed=%d retry=%d", failedExecution.AttemptGeneration, retryExecution.AttemptGeneration)
	}

	var status string
	var retryCount int
	if err := pool.QueryRow(ctx, `SELECT status, retry_count FROM workflow_node_runs WHERE id = $1`, retryExecution.NodeRunID).Scan(&status, &retryCount); err != nil {
		t.Fatalf("load retried node: %v", err)
	}
	if status != "running" || retryCount != 1 {
		t.Fatalf("retried node status/retry count = %s/%d, want running/1", status, retryCount)
	}
	if err := CompleteNodeRun(ctx, pool, retryExecution, []byte(`{"retried":true}`)); err != nil {
		t.Fatalf("complete retried node: %v", err)
	}
}

func assertWorkflowResultDiscardedEvent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID, nodeRunID string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM project_event_log
		WHERE project_id = $1
		  AND event_type = 'workflow.result.discarded'
		  AND aggregate_type = 'workflow_node_run'
		  AND aggregate_id = $2
	`, projectID, nodeRunID).Scan(&count); err != nil {
		t.Fatalf("count discarded result events: %v", err)
	}
	if count != want {
		t.Fatalf("discarded result event count = %d, want %d", count, want)
	}
}

func TestCancelledWorkflowRunRejectsLateTerminalTransition(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "cancelled-run-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if err := CancelWorkflowRun(ctx, pool, workflowRunID, []byte(`{"cancelled":true}`), "test cancellation"); err != nil {
		t.Fatalf("cancel workflow: %v", err)
	}
	var cancelledRevision int64
	var settledAt time.Time
	if err := pool.QueryRow(ctx, `SELECT revision, settled_at FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&cancelledRevision, &settledAt); err != nil {
		t.Fatalf("load cancelled workflow: %v", err)
	}
	if settledAt.IsZero() {
		t.Fatal("cancelled workflow did not record settled_at")
	}
	if err := TransitionWorkflowRun(ctx, pool, workflowRunID, "succeeded", "", "", []byte(`{"late":true}`)); err != nil {
		t.Fatalf("late workflow completion should be discarded: %v", err)
	}
	if err := CompleteNodeRun(ctx, pool, execution, []byte(`{"late":true}`)); err != nil {
		t.Fatalf("late node completion should be discarded: %v", err)
	}
	var status, output string
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT status, output::text, revision FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&status, &output, &revision); err != nil {
		t.Fatalf("reload cancelled workflow: %v", err)
	}
	if status != "cancelled" || strings.Contains(output, "late") || revision != cancelledRevision {
		t.Fatalf("late transition mutated cancelled workflow: status=%s output=%s revision=%d wantRevision=%d", status, output, revision, cancelledRevision)
	}
}

func TestWorkflowTerminalTransitionIsIdempotent(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "terminal-run-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if err := CompleteNodeRun(ctx, pool, execution, []byte(`{"done":true}`)); err != nil {
		t.Fatalf("complete node: %v", err)
	}
	if err := TransitionWorkflowRun(ctx, pool, workflowRunID, "succeeded", "", "", []byte(`{"result":"first"}`)); err != nil {
		t.Fatalf("complete workflow: %v", err)
	}
	var terminalRevision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&terminalRevision); err != nil {
		t.Fatalf("load terminal revision: %v", err)
	}
	if err := TransitionWorkflowRun(ctx, pool, workflowRunID, "failed", "LATE_FAILURE", "late", []byte(`{"result":"late"}`)); err != nil {
		t.Fatalf("duplicate terminal transition should be discarded: %v", err)
	}
	var status, output string
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT status, output::text, revision FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&status, &output, &revision); err != nil {
		t.Fatalf("reload terminal workflow: %v", err)
	}
	if status != "succeeded" || !strings.Contains(output, "first") || strings.Contains(output, "late") || revision != terminalRevision {
		t.Fatalf("duplicate transition changed terminal workflow: status=%s output=%s revision=%d wantRevision=%d", status, output, revision, terminalRevision)
	}
	var completedEvents, failedEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE event_type = 'workflow.run.completed'),
		       count(*) FILTER (WHERE event_type = 'workflow.run.failed')
		FROM event_outbox
		WHERE aggregate_type = 'workflow_run' AND aggregate_id = $1
	`, workflowRunID).Scan(&completedEvents, &failedEvents); err != nil {
		t.Fatalf("count workflow terminal events: %v", err)
	}
	if completedEvents != 1 || failedEvents != 0 {
		t.Fatalf("terminal event counts completed=%d failed=%d", completedEvents, failedEvents)
	}
}

func TestFailedWorkflowTransitionSettlesRunningNodes(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "failed-workflow-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if err := TransitionWorkflowRun(ctx, pool, workflowRunID, "failed", "TEST_FAILURE", "test failure", []byte(`{"failed":true}`)); err != nil {
		t.Fatalf("fail workflow: %v", err)
	}
	var workflowStatus, nodeStatus, nodeCode string
	if err := pool.QueryRow(ctx, `
		SELECT run.status, node.status, COALESCE(node.error_code, '')
		FROM workflow_runs run JOIN workflow_node_runs node ON node.workflow_run_id = run.id
		WHERE run.id = $1 AND node.id = $2
	`, workflowRunID, execution.NodeRunID).Scan(&workflowStatus, &nodeStatus, &nodeCode); err != nil {
		t.Fatalf("load terminal state: %v", err)
	}
	if workflowStatus != "failed" || nodeStatus != "failed" || nodeCode != "TEST_FAILURE" {
		t.Fatalf("workflow=%s node=%s code=%s", workflowStatus, nodeStatus, nodeCode)
	}
	var failedEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM event_outbox
		WHERE event_type = 'workflow.node.failed' AND aggregate_id = $1
	`, execution.NodeRunID).Scan(&failedEvents); err != nil {
		t.Fatalf("count node failure events: %v", err)
	}
	if failedEvents != 1 {
		t.Fatalf("node failure events = %d, want 1", failedEvents)
	}
}

func TestReconcileFailedWorkflowNodesRepairsExistingInconsistency(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "orphaned-terminal-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'failed', error_code = 'LEGACY_FAILURE', error_message = 'legacy failure',
		    completed_at = now(), terminalized_at = now(), settled_at = now()
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("seed terminal inconsistency: %v", err)
	}
	settled, err := ReconcileFailedWorkflowNodes(ctx, pool)
	if err != nil {
		t.Fatalf("reconcile failed nodes: %v", err)
	}
	if settled < 1 {
		t.Fatalf("settled nodes = %d, want at least the seeded node", settled)
	}
	var status, code string
	if err := pool.QueryRow(ctx, `SELECT status, COALESCE(error_code, '') FROM workflow_node_runs WHERE id = $1`, execution.NodeRunID).Scan(&status, &code); err != nil {
		t.Fatalf("load reconciled node: %v", err)
	}
	if status != "failed" || code != "LEGACY_FAILURE" {
		t.Fatalf("node status=%s code=%s", status, code)
	}
	settled, err = ReconcileFailedWorkflowNodes(ctx, pool)
	if err != nil || settled != 0 {
		t.Fatalf("idempotent reconcile settled=%d err=%v", settled, err)
	}
}

func seedNodeRunIntegrationTest(t *testing.T) (context.Context, *pgxpool.Pool, string, string, string) {
	t.Helper()
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run node run integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for node run integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	organizationID, _, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
	})
	return ctx, pool, organizationID, projectID, workflowRunID
}
