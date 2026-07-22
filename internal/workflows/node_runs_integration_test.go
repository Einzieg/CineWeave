package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

func TestProjectProductionSettingsUseWorkflowGenerationSnapshot(t *testing.T) {
	ctx, pool, _, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	activities := Activities{db: pool}

	oldSettings, err := activities.projectProductionSettings(ctx, projectID, workflowRunID)
	if err != nil {
		t.Fatalf("load old workflow production settings: %v", err)
	}
	newGenerationID, newBindingID, newBindingRevision := supersedeVideoProductionGenerationForTest(t, ctx, pool, projectID, "4:3")
	if _, err := pool.Exec(ctx, `
		UPDATE projects SET aspect_ratio = '9:16', video_ratio = '9:16' WHERE id = $1
	`, projectID); err != nil {
		t.Fatalf("mutate current project fields: %v", err)
	}

	activeSettings, err := activities.projectProductionSettings(ctx, projectID)
	if err != nil {
		t.Fatalf("load active production settings: %v", err)
	}
	if activeSettings.VideoRatio != "4:3" || activeSettings.ProductionGenerationID != newGenerationID ||
		activeSettings.VideoProductionBindingID != newBindingID || activeSettings.VideoProductionBindingRevision != newBindingRevision {
		t.Fatalf("active settings do not match replacement snapshot: %+v", activeSettings)
	}

	workflowSettings, err := activities.projectProductionSettings(ctx, projectID, workflowRunID)
	if err != nil {
		t.Fatalf("reload old workflow production settings: %v", err)
	}
	if workflowSettings.VideoRatio != oldSettings.VideoRatio ||
		workflowSettings.ProductionGenerationID != oldSettings.ProductionGenerationID ||
		workflowSettings.VideoProductionBindingID != oldSettings.VideoProductionBindingID ||
		workflowSettings.VideoProductionBindingRevision != oldSettings.VideoProductionBindingRevision {
		t.Fatalf("old workflow settings crossed production generations: old=%+v current=%+v", oldSettings, workflowSettings)
	}
	if workflowSettings.VideoRatio == activeSettings.VideoRatio || workflowSettings.VideoRatio == "9:16" {
		t.Fatalf("workflow settings used mutable or active project ratio: workflow=%s active=%s", workflowSettings.VideoRatio, activeSettings.VideoRatio)
	}
}

func TestOldProductionGenerationCannotCommitLateBusinessResult(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "old-generation-late-result",
		NodeType:       "test.production_write",
	})
	if err != nil {
		t.Fatalf("start old generation node: %v", err)
	}
	oldGenerationID := execution.ProductionGenerationID
	newGenerationID, _, _ := supersedeVideoProductionGenerationForTest(t, ctx, pool, projectID)
	if oldGenerationID == "" || newGenerationID == "" || oldGenerationID == newGenerationID {
		t.Fatalf("invalid generation switch old=%q new=%q", oldGenerationID, newGenerationID)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin late application write: %v", err)
	}
	_, appFenceErr := lockNodeBusinessWrite(ctx, tx, workflowRunID, execution)
	_ = tx.Rollback(ctx)
	if !isWorkflowWriteFenced(appFenceErr) {
		t.Fatalf("late application write error = %v, want workflow write fence", appFenceErr)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO project_timelines(
			organization_id, project_id, workflow_run_id, title, production_generation_id
		)
		VALUES ($1, $2, $3, 'late old generation timeline', $4)
	`, organizationID, projectID, workflowRunID, oldGenerationID); err == nil {
		t.Fatal("database generation guard accepted a late old-generation timeline")
	}
	var lateTimelineCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM project_timelines
		WHERE project_id = $1 AND workflow_run_id = $2 AND production_generation_id = $3
	`, projectID, workflowRunID, oldGenerationID).Scan(&lateTimelineCount); err != nil {
		t.Fatalf("count late timelines: %v", err)
	}
	if lateTimelineCount != 0 {
		t.Fatalf("late old-generation timelines = %d, want 0", lateTimelineCount)
	}

	if err := finalizeWorkflowActivityError(ctx, pool, execution, appFenceErr); !isWorkflowWriteFenced(err) {
		t.Fatalf("finalize late result error = %v, want workflow write fence", err)
	}
	var payload json.RawMessage
	if err := pool.QueryRow(ctx, `
		SELECT payload
		FROM project_event_log
		WHERE project_id = $1
		  AND event_type = 'workflow.result.discarded'
		  AND aggregate_id = $2
		ORDER BY stream_position DESC
		LIMIT 1
	`, projectID, execution.NodeRunID).Scan(&payload); err != nil {
		t.Fatalf("load late-result audit event: %v", err)
	}
	var audit map[string]any
	if err := json.Unmarshal(payload, &audit); err != nil {
		t.Fatalf("decode late-result audit event: %v", err)
	}
	if audit["errorCode"] != CodeWorkflowResultDiscarded || audit["reasonCode"] != "PRODUCTION_GENERATION_MISMATCH" {
		t.Fatalf("late-result audit codes = %#v", audit)
	}
	if audit["productionGenerationId"] != oldGenerationID || audit["currentProductionGenerationId"] != newGenerationID {
		t.Fatalf("late-result audit generations = %#v", audit)
	}
}

func supersedeVideoProductionGenerationForTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID string, targetRatio ...string) (string, string, int64) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin generation switch: %v", err)
	}
	defer tx.Rollback(ctx)
	var oldGenerationID, oldBindingID string
	var oldGenerationNo, oldBindingRevision int64
	var replacementSnapshot json.RawMessage
	var replacementSnapshotHash string
	if err := tx.QueryRow(ctx, `
		SELECT generation.id::text, generation.generation_no,
		       binding.id::text, binding.revision,
		       binding.profile_snapshot, binding.profile_snapshot_hash
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id
		JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
		WHERE project.id = $1
		FOR UPDATE OF project, generation, binding
	`, projectID).Scan(
		&oldGenerationID,
		&oldGenerationNo,
		&oldBindingID,
		&oldBindingRevision,
		&replacementSnapshot,
		&replacementSnapshotHash,
	); err != nil {
		t.Fatalf("load active generation: %v", err)
	}
	if len(targetRatio) > 0 && strings.TrimSpace(targetRatio[0]) != "" {
		var document map[string]any
		if err := json.Unmarshal(replacementSnapshot, &document); err != nil {
			t.Fatalf("decode replacement profile snapshot: %v", err)
		}
		configuration, ok := document["productionConfiguration"].(map[string]any)
		if !ok {
			t.Fatalf("replacement snapshot production configuration = %#v", document["productionConfiguration"])
		}
		configuration["aspectRatio"] = targetRatio[0]
		configuration["videoRatio"] = targetRatio[0]
		updatedSnapshot, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("encode replacement profile snapshot: %v", err)
		}
		digest := sha256.Sum256(updatedSnapshot)
		replacementSnapshot = updatedSnapshot
		replacementSnapshotHash = hex.EncodeToString(digest[:])
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_video_production_bindings
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1
	`, oldBindingID); err != nil {
		t.Fatalf("supersede binding: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_video_production_generations
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1
	`, oldGenerationID); err != nil {
		t.Fatalf("supersede generation: %v", err)
	}
	var newBindingID string
	newBindingRevision := oldBindingRevision + 1
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_video_production_bindings(
			project_id, profile_version_id, status, compatibility_policy, overrides,
			profile_snapshot, profile_snapshot_hash, revision, created_by
		)
		SELECT project_id, profile_version_id, 'active', compatibility_policy, overrides,
		       $3::jsonb, $4, $2, created_by
		FROM project_video_production_bindings
		WHERE id = $1
		RETURNING id::text
	`, oldBindingID, newBindingRevision, replacementSnapshot, replacementSnapshotHash).Scan(&newBindingID); err != nil {
		t.Fatalf("insert replacement binding: %v", err)
	}
	var newGenerationID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_video_production_generations(
			organization_id, project_id, binding_id, generation_no, status,
			source_generation_id, activated_at
		)
		SELECT organization_id, project_id, $2, $3, 'active', id, now()
		FROM project_video_production_generations
		WHERE id = $1
		RETURNING id::text
	`, oldGenerationID, newBindingID, oldGenerationNo+1).Scan(&newGenerationID); err != nil {
		t.Fatalf("insert replacement generation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_video_production_generation_id = $2,
		    video_production_generation_no = $3,
		    video_production_state = 'storyboard_required',
		    updated_at = now()
		WHERE id = $1
	`, projectID, newGenerationID, oldGenerationNo+1); err != nil {
		t.Fatalf("activate replacement generation: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit generation switch: %v", err)
	}
	return newGenerationID, newBindingID, newBindingRevision
}

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

func TestFinalizeWorkflowActivityErrorFailsRunningNode(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "activity-error-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	cause := errors.New("persistence failed")
	if got := finalizeWorkflowActivityError(ctx, pool, execution, cause); !errors.Is(got, cause) {
		t.Fatalf("finalized error = %v, want original cause", got)
	}
	var status, code, message string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_code, ''), COALESCE(error_message, '')
		FROM workflow_node_runs WHERE id = $1
	`, execution.NodeRunID).Scan(&status, &code, &message); err != nil {
		t.Fatalf("load finalized node: %v", err)
	}
	if status != "failed" || code != codeActivityFailed || message != cause.Error() {
		t.Fatalf("node status=%s code=%s message=%s", status, code, message)
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

func TestPartialSucceededWorkflowTransitionSettlesRunningNodes(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "partial-workflow-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if err := TransitionWorkflowRun(ctx, pool, workflowRunID, "partial_succeeded", "", "", []byte(`{"partial":true}`)); err != nil {
		t.Fatalf("partially complete workflow: %v", err)
	}
	var workflowStatus, nodeStatus, nodeCode string
	if err := pool.QueryRow(ctx, `
		SELECT run.status, node.status, COALESCE(node.error_code, '')
		FROM workflow_runs run JOIN workflow_node_runs node ON node.workflow_run_id = run.id
		WHERE run.id = $1 AND node.id = $2
	`, workflowRunID, execution.NodeRunID).Scan(&workflowStatus, &nodeStatus, &nodeCode); err != nil {
		t.Fatalf("load partial terminal state: %v", err)
	}
	if workflowStatus != "partial_succeeded" || nodeStatus != "failed" || nodeCode != "WORKFLOW_PARTIAL_SUCCEEDED" {
		t.Fatalf("workflow=%s node=%s code=%s", workflowStatus, nodeStatus, nodeCode)
	}
}

func TestReconcileTerminalWorkflowNodesRepairsExistingInconsistency(t *testing.T) {
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
		SET status = 'failed', error_code = 'PREEXISTING_FAILURE', error_message = 'preexisting failure',
		    completed_at = now(), terminalized_at = now(), settled_at = now()
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("seed terminal inconsistency: %v", err)
	}
	settled, err := ReconcileTerminalWorkflowNodes(ctx, pool)
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
	if status != "failed" || code != "PREEXISTING_FAILURE" {
		t.Fatalf("node status=%s code=%s", status, code)
	}
	settled, err = ReconcileTerminalWorkflowNodes(ctx, pool)
	if err != nil || settled != 0 {
		t.Fatalf("idempotent reconcile settled=%d err=%v", settled, err)
	}
}

func TestReconcileTerminalWorkflowNodesRepairsPartialSuccess(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "orphaned-partial-node",
		NodeType:       "test",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'partial_succeeded', completed_at = now(), terminalized_at = now(), settled_at = now()
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("seed partial terminal inconsistency: %v", err)
	}
	settled, err := ReconcileTerminalWorkflowNodes(ctx, pool)
	if err != nil {
		t.Fatalf("reconcile partial nodes: %v", err)
	}
	if settled < 1 {
		t.Fatalf("settled nodes = %d, want at least the seeded node", settled)
	}
	var status, code string
	if err := pool.QueryRow(ctx, `SELECT status, COALESCE(error_code, '') FROM workflow_node_runs WHERE id = $1`, execution.NodeRunID).Scan(&status, &code); err != nil {
		t.Fatalf("load reconciled partial node: %v", err)
	}
	if status != "failed" || code != "WORKFLOW_PARTIAL_SUCCEEDED" {
		t.Fatalf("node status=%s code=%s", status, code)
	}
}

func TestCompleteAssetBatchWorkflowReconcilesFailedChildNode(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	if _, err := pool.Exec(ctx, `UPDATE workflow_runs SET total_items = 2 WHERE id = $1`, workflowRunID); err != nil {
		t.Fatalf("set workflow total: %v", err)
	}

	input := AssetBatchWorkflowInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		Operation:      AssetBatchOperationGenerateImages,
		Items: []AssetBatchItemSnapshot{
			{AssetID: "asset-success"},
			{AssetID: "asset-failed"},
		},
	}
	succeeded, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        AssetBatchNodeKey(input.Operation, "asset-success"),
		NodeType:       "asset.image.generate",
		Input:          mustJSON(input.Items[0]),
	})
	if err != nil {
		t.Fatalf("start succeeded item: %v", err)
	}
	if err := CompleteNodeRun(ctx, pool, succeeded, mustJSON(AssetBatchItemOutput{AssetID: "asset-success", Status: "succeeded"})); err != nil {
		t.Fatalf("complete succeeded item: %v", err)
	}
	failed, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        AssetBatchNodeKey(input.Operation, "asset-failed"),
		NodeType:       "asset.image.generate",
		Input:          mustJSON(input.Items[1]),
	})
	if err != nil {
		t.Fatalf("start failed item: %v", err)
	}

	requested := AssetBatchWorkflowOutput{Items: []AssetBatchItemOutput{
		{AssetID: "asset-success", NodeRunID: succeeded.NodeRunID, Status: "succeeded"},
		{AssetID: "asset-failed", NodeRunID: failed.NodeRunID, Status: "failed", ErrorCode: CodeWorkflowResultDiscarded, ErrorMessage: workflowWriteFenceMessage},
	}}
	output, err := (Activities{db: pool}).CompleteAssetBatchWorkflow(ctx, input, requested)
	if err != nil {
		t.Fatalf("complete asset batch: %v", err)
	}
	if output.Status != "partial_succeeded" || output.CompletedItems != 1 || output.FailedItems != 1 || output.ActiveItems != 0 {
		t.Fatalf("output = %+v, want one completed and one reconciled failure", output)
	}
	var workflowStatus, nodeStatus, nodeErrorCode string
	if err := pool.QueryRow(ctx, `
		SELECT run.status, node.status, COALESCE(node.error_code, '')
		FROM workflow_runs run
		JOIN workflow_node_runs node ON node.workflow_run_id = run.id
		WHERE run.id = $1 AND node.id = $2
	`, workflowRunID, failed.NodeRunID).Scan(&workflowStatus, &nodeStatus, &nodeErrorCode); err != nil {
		t.Fatalf("load reconciled statuses: %v", err)
	}
	if workflowStatus != "partial_succeeded" || nodeStatus != "failed" || nodeErrorCode != CodeWorkflowResultDiscarded {
		t.Fatalf("statuses workflow=%s node=%s code=%s", workflowStatus, nodeStatus, nodeErrorCode)
	}
}

func TestCompleteAssetBatchWorkflowRejectsActiveNodeAsSuccess(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	if _, err := pool.Exec(ctx, `UPDATE workflow_runs SET total_items = 1 WHERE id = $1`, workflowRunID); err != nil {
		t.Fatalf("set workflow total: %v", err)
	}
	item := AssetBatchItemSnapshot{AssetID: "asset-active"}
	input := AssetBatchWorkflowInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		Operation:      AssetBatchOperationGenerateImages,
		Items:          []AssetBatchItemSnapshot{item},
	}
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        AssetBatchNodeKey(input.Operation, item.AssetID),
		NodeType:       "asset.image.generate",
		Input:          mustJSON(item),
	})
	if err != nil {
		t.Fatalf("start active item: %v", err)
	}
	_, err = (Activities{db: pool}).CompleteAssetBatchWorkflow(ctx, input, AssetBatchWorkflowOutput{Items: []AssetBatchItemOutput{
		{AssetID: item.AssetID, NodeRunID: execution.NodeRunID, Status: "succeeded"},
	}})
	var applicationError *temporal.ApplicationError
	if !errors.As(err, &applicationError) || applicationError.Type() != "ASSET_BATCH_ITEMS_ACTIVE" {
		t.Fatalf("completion error = %v, want ASSET_BATCH_ITEMS_ACTIVE", err)
	}
	var workflowStatus, nodeStatus string
	if err := pool.QueryRow(ctx, `
		SELECT run.status, node.status
		FROM workflow_runs run
		JOIN workflow_node_runs node ON node.workflow_run_id = run.id
		WHERE run.id = $1 AND node.id = $2
	`, workflowRunID, execution.NodeRunID).Scan(&workflowStatus, &nodeStatus); err != nil {
		t.Fatalf("load active statuses: %v", err)
	}
	if workflowStatus != "running" || nodeStatus != "running" {
		t.Fatalf("statuses workflow=%s node=%s, want both running", workflowStatus, nodeStatus)
	}
}

func TestNodeBusinessWriteLocksProjectBeforeWorkflow(t *testing.T) {
	ctx, pool, organizationID, projectID, workflowRunID := seedNodeRunIntegrationTest(t)
	execution, err := StartNodeRun(ctx, pool, NodeRunInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		NodeKey:        "lock-order-check",
		NodeType:       "test.lock_order",
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}

	providerTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin provider-like transaction: %v", err)
	}
	defer providerTx.Rollback(ctx)
	var lockedProjectID string
	if err := providerTx.QueryRow(ctx, `SELECT id::text FROM projects WHERE id = $1 FOR KEY SHARE`, projectID).Scan(&lockedProjectID); err != nil {
		t.Fatalf("lock project like provider foreign key: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, beginErr := pool.Begin(writeCtx)
		if beginErr != nil {
			result <- beginErr
			return
		}
		defer tx.Rollback(writeCtx)
		_, lockErr := lockNodeBusinessWrite(writeCtx, tx, workflowRunID, execution)
		result <- lockErr
	}()

	// The workflow-side transaction must block on the project row without
	// already holding the workflow row, so this provider-style FK lock can
	// safely continue to the workflow row.
	time.Sleep(150 * time.Millisecond)
	providerCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var lockedWorkflowID string
	if err := providerTx.QueryRow(providerCtx, `SELECT id::text FROM workflow_runs WHERE id = $1 FOR KEY SHARE`, workflowRunID).Scan(&lockedWorkflowID); err != nil {
		t.Fatalf("provider-style workflow lock deadlocked: %v", err)
	}
	if err := providerTx.Commit(ctx); err != nil {
		t.Fatalf("commit provider-like transaction: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("lock node business write: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("node business write did not resume after project lock released")
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
