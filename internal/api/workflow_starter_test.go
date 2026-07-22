package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

func TestCreateWorkflowRunPersistsIdempotencyWithOutbox(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	body := map[string]any{
		"projectId":      seed.projectID,
		"workflowType":   "text_to_storyboard",
		"prompt":         "outbox idempotency test",
		"input":          map[string]any{},
		"idempotencyKey": "workflow-outbox-test-key",
	}
	var first WorkflowRun
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/workflow-runs", seed.ownerToken, seed.organizationID, body, &first)
	var replay WorkflowRun
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/workflow-runs", seed.ownerToken, seed.organizationID, body, &replay)
	if first.ID == "" || replay.ID != first.ID {
		t.Fatalf("first=%+v replay=%+v", first, replay)
	}
	if temporal.Count() != 0 {
		t.Fatalf("HTTP requests started Temporal directly: calls=%d", temporal.Count())
	}
	var keyStatus, snapshotID, operationID, operationStatus string
	var outboxCount, inputSnapshotCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT key.status, COALESCE(key.response_snapshot->>'id', ''), COALESCE(key.operation_id::text, ''),
		       COALESCE((SELECT status FROM runtime_operations WHERE id = key.operation_id), ''),
		       (SELECT count(*) FROM workflow_start_outbox WHERE workflow_run_id = $4),
		       (SELECT count(*) FROM workflow_input_snapshots WHERE workflow_run_id = $4)
		FROM idempotency_keys key
		WHERE key.organization_id = $1 AND key.scope = $2 AND key.key = $3
	`, seed.organizationID, "workflow-runs:create", "workflow-outbox-test-key", first.ID).Scan(
		&keyStatus, &snapshotID, &operationID, &operationStatus, &outboxCount, &inputSnapshotCount,
	); err != nil {
		t.Fatalf("read idempotency and outbox: %v", err)
	}
	if keyStatus != "succeeded" || snapshotID != first.ID || operationID == "" || operationStatus != "succeeded" || outboxCount != 1 || inputSnapshotCount != 1 {
		t.Fatalf("idempotency status=%s snapshot=%s operation=%s/%s outboxes=%d inputSnapshots=%d", keyStatus, snapshotID, operationID, operationStatus, outboxCount, inputSnapshotCount)
	}
	dispatchWorkflowStartsForTest(t, server)
	if temporal.Count() != 1 {
		t.Fatalf("Temporal calls=%d, want 1", temporal.Count())
	}
}

func TestWorkflowStartOutboxDispatchesExactlyOnce(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	run, err := server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "script_to_assets", map[string]any{"scriptId": "script-1"}, workflows.ScriptToAssetsWorkflow)
	if err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}
	if temporal.Count() != 0 {
		t.Fatalf("API request started Temporal directly: calls=%d", temporal.Count())
	}
	assertWorkflowStartState(t, seed, run.ID, "queued", "pending", 0)

	var wait sync.WaitGroup
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			_, dispatchErr := server.dispatchWorkflowStarts(seed.ctx, "test-worker-"+string(rune('a'+worker)), time.Second, time.Second, 1)
			results <- dispatchErr
		}(index)
	}
	wait.Wait()
	close(results)
	for dispatchErr := range results {
		if dispatchErr != nil {
			t.Fatalf("dispatch workflow: %v", dispatchErr)
		}
	}
	if temporal.Count() != 1 {
		t.Fatalf("Temporal calls=%d, want 1", temporal.Count())
	}
	assertWorkflowStartState(t, seed, run.ID, "running", "started", 1)
	if processed, err := server.dispatchWorkflowStarts(seed.ctx, "test-worker-c", time.Second, time.Second, 1); err != nil || processed != 0 {
		t.Fatalf("second dispatch processed=%d err=%v", processed, err)
	}
	if temporal.Count() != 1 {
		t.Fatalf("re-dispatch started duplicate workflow: calls=%d", temporal.Count())
	}

	options, args := temporal.LastStart()
	if options.ID != run.TemporalWorkflowID || options.TaskQueue != workflows.ScriptTaskQueue || len(args) != 1 {
		t.Fatalf("Temporal start options=%+v args=%#v", options, args)
	}
	input, ok := args[0].(workflows.TextToStoryboardInput)
	if !ok || input.WorkflowRunID != run.ID || input.ProjectID != seed.projectID {
		t.Fatalf("Temporal input=%#v", args[0])
	}
}

func TestWorkflowStartOutboxCancellationFencesClaimedStart(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	run, err := server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "script_to_assets", map[string]any{"scriptId": "script-1"}, workflows.ScriptToAssetsWorkflow)
	if err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type, status, input, output
		) VALUES ($1, $2, $3, 'cancel-fence-node', 'asset.image.generate', 'queued', '{}', '{}')
	`, seed.organizationID, seed.projectID, run.ID); err != nil {
		t.Fatalf("insert queued node: %v", err)
	}

	claimed, ok, err := server.claimWorkflowStart(seed.ctx, "cancel-fence-worker", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim workflow start ok=%v err=%v", ok, err)
	}
	cancelled, err := server.cancelWorkflowRunItem(seed.ctx, run, "cancel before Temporal start")
	if err != nil {
		t.Fatalf("cancel claimed workflow: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled status=%q, want cancelled", cancelled.Status)
	}
	result, err := server.executeWorkflowStart(seed.ctx, "cancel-fence-worker", claimed)
	if err != nil {
		t.Fatalf("execute fenced workflow: %v", err)
	}
	if result != workflowStartResultCancelledFenced {
		t.Fatalf("execution result = %q, want %q", result, workflowStartResultCancelledFenced)
	}
	if temporal.Count() != 0 {
		t.Fatalf("fenced workflow reached Temporal: calls=%d", temporal.Count())
	}
	assertWorkflowStartState(t, seed, run.ID, "cancelled", "cancelled", 1)
	var nodeStatus, nodeErrorCode string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT status, COALESCE(error_code, '')
		FROM workflow_node_runs
		WHERE workflow_run_id = $1 AND node_key = 'cancel-fence-node'
	`, run.ID).Scan(&nodeStatus, &nodeErrorCode); err != nil {
		t.Fatalf("read cancelled node: %v", err)
	}
	if nodeStatus != "cancelled" || nodeErrorCode != "USER_CANCELLED" {
		t.Fatalf("node status=%s error=%s, want cancelled/USER_CANCELLED", nodeStatus, nodeErrorCode)
	}
}

func TestWorkflowStartOutboxProductionLockFencesClaimedStart(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	run, err := server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "script_to_assets", map[string]any{"scriptId": "script-1"}, workflows.ScriptToAssetsWorkflow)
	if err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}
	claimed, ok, err := server.claimWorkflowStart(seed.ctx, "generation-fence-worker", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim workflow start ok=%v err=%v", ok, err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET video_production_locked = true WHERE id = $1`, seed.projectID); err != nil {
		t.Fatalf("lock project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = seed.pool.Exec(context.Background(), `UPDATE projects SET video_production_locked = false WHERE id = $1`, seed.projectID)
	})

	result, err := server.executeWorkflowStart(seed.ctx, "generation-fence-worker", claimed)
	if err != nil {
		t.Fatalf("execute fenced workflow: %v", err)
	}
	if result != workflowStartResultCancelledFenced {
		t.Fatalf("execution result = %q, want %q", result, workflowStartResultCancelledFenced)
	}
	if temporal.Count() != 0 {
		t.Fatalf("locked workflow reached Temporal: calls=%d", temporal.Count())
	}
	assertWorkflowStartState(t, seed, run.ID, "cancelled", "cancelled", 1)
	var errorCode string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT error_code FROM workflow_runs WHERE id = $1`, run.ID).Scan(&errorCode); err != nil {
		t.Fatalf("read fenced workflow error: %v", err)
	}
	if errorCode != "PROJECT_VIDEO_PRODUCTION_LOCKED" {
		t.Fatalf("error code=%q, want PROJECT_VIDEO_PRODUCTION_LOCKED", errorCode)
	}
}

func TestWorkflowEnqueueRejectsLockedProject(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = &workflowStartTestTemporal{}
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	var before int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM workflow_runs WHERE project_id = $1 AND workflow_type = 'script_to_assets'`, seed.projectID).Scan(&before); err != nil {
		t.Fatalf("count workflows before enqueue: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE projects SET video_production_locked = true WHERE id = $1`, seed.projectID); err != nil {
		t.Fatalf("lock project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = seed.pool.Exec(context.Background(), `UPDATE projects SET video_production_locked = false WHERE id = $1`, seed.projectID)
	})

	_, err = server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "script_to_assets", map[string]any{"scriptId": "script-1"}, workflows.ScriptToAssetsWorkflow)
	var productionErr videoproduction.Error
	if !errors.As(err, &productionErr) || productionErr.Code != videoproduction.CodeProjectLocked {
		t.Fatalf("enqueue error = %v, want %s", err, videoproduction.CodeProjectLocked)
	}
	var count int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM workflow_runs WHERE project_id = $1 AND workflow_type = 'script_to_assets'`, seed.projectID).Scan(&count); err != nil {
		t.Fatalf("count workflows: %v", err)
	}
	if count != before {
		t.Fatalf("locked enqueue changed workflow run count from %d to %d", before, count)
	}
}

func TestWorkflowStartOutboxRecoversExpiredLeaseAndAlreadyStarted(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{executeErr: serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "request-1", "run-1")}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	run, err := server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "compose_timeline", map[string]any{"timelineId": "timeline-1"}, workflows.ComposeTimelineWorkflow)
	if err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `
		UPDATE workflow_start_outbox
		SET status = 'processing', attempt_count = 1, locked_by = 'crashed-api', locked_at = now() - interval '5 minutes'
		WHERE workflow_run_id = $1
	`, run.ID); err != nil {
		t.Fatalf("stage expired lease: %v", err)
	}
	processed, err := server.dispatchWorkflowStarts(seed.ctx, "recovery-worker", time.Second, time.Second, 1)
	if err != nil || processed != 1 {
		t.Fatalf("recover dispatch processed=%d err=%v", processed, err)
	}
	assertWorkflowStartState(t, seed, run.ID, "running", "started", 1)
	if temporal.Count() != 1 {
		t.Fatalf("Temporal calls=%d, want one idempotent recovery start", temporal.Count())
	}
}

func TestWorkflowStartOutboxRetriesTransientFailure(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{executeErr: errors.New("Temporal unavailable")}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	run, err := server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "script_to_assets", map[string]any{"scriptId": "script-1"}, workflows.ScriptToAssetsWorkflow)
	if err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}
	if _, err := server.dispatchWorkflowStarts(seed.ctx, "retry-worker", time.Second, time.Second, 1); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	assertWorkflowStartState(t, seed, run.ID, "queued", "pending", 1)

	temporal.SetError(nil)
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE workflow_start_outbox SET next_attempt_at = now() WHERE workflow_run_id = $1`, run.ID); err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	if _, err := server.dispatchWorkflowStarts(seed.ctx, "retry-worker", time.Second, time.Second, 1); err != nil {
		t.Fatalf("retry dispatch: %v", err)
	}
	assertWorkflowStartState(t, seed, run.ID, "running", "started", 2)
	if temporal.Count() != 2 {
		t.Fatalf("Temporal calls=%d, want 2", temporal.Count())
	}
}

func TestWorkflowStartOutboxFailsCorruptPayloadWithoutTemporalCall(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	temporal := &workflowStartTestTemporal{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal
	project, err := server.project(requestWithContext(seed.ctx), seed.projectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	run, err := server.startProjectWorkflowCore(seed.ctx, auth.Principal{
		UserID: seed.ownerUserID, OrganizationID: seed.organizationID,
	}, project, "script_to_assets", map[string]any{"scriptId": "script-1"}, workflows.ScriptToAssetsWorkflow)
	if err != nil {
		t.Fatalf("enqueue workflow: %v", err)
	}
	if _, err := seed.pool.Exec(seed.ctx, `UPDATE workflow_start_outbox SET input = jsonb_set(input, '{projectId}', '"tampered"') WHERE workflow_run_id = $1`, run.ID); err != nil {
		t.Fatalf("corrupt payload: %v", err)
	}
	if _, err := server.dispatchWorkflowStarts(seed.ctx, "validation-worker", time.Second, time.Second, 1); err != nil {
		t.Fatalf("dispatch corrupt payload: %v", err)
	}
	assertWorkflowStartState(t, seed, run.ID, "failed", "failed", 1)
	if temporal.Count() != 0 {
		t.Fatalf("corrupt payload reached Temporal: calls=%d", temporal.Count())
	}
	var errorCode string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT error_code FROM workflow_runs WHERE id = $1`, run.ID).Scan(&errorCode); err != nil {
		t.Fatalf("read workflow error: %v", err)
	}
	if errorCode != "WORKFLOW_START_INPUT_HASH_MISMATCH" {
		t.Fatalf("error code=%q", errorCode)
	}
}

func assertWorkflowStartState(t *testing.T, seed *artifactPreviewSeed, workflowRunID, wantRunStatus, wantOutboxStatus string, wantAttempts int) {
	t.Helper()
	var runStatus, outboxStatus string
	var attempts int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT run.status, outbox.status, outbox.attempt_count
		FROM workflow_runs run
		JOIN workflow_start_outbox outbox ON outbox.workflow_run_id = run.id
		WHERE run.id = $1
	`, workflowRunID).Scan(&runStatus, &outboxStatus, &attempts); err != nil {
		t.Fatalf("read workflow start state: %v", err)
	}
	if runStatus != wantRunStatus || outboxStatus != wantOutboxStatus || attempts != wantAttempts {
		t.Fatalf("workflow state run=%s outbox=%s attempts=%d, want %s/%s/%d", runStatus, outboxStatus, attempts, wantRunStatus, wantOutboxStatus, wantAttempts)
	}
}

func dispatchWorkflowStartsForTest(t *testing.T, server *Server) int {
	t.Helper()
	processed, err := server.dispatchWorkflowStarts(context.Background(), "integration-test-dispatcher", time.Second, 5*time.Second, 100)
	if err != nil {
		t.Fatalf("dispatch workflow starts: %v", err)
	}
	return processed
}

type workflowStartTestTemporal struct {
	mu         sync.Mutex
	executeErr error
	count      int
	options    client.StartWorkflowOptions
	args       []any
}

func (c *workflowStartTestTemporal) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, _ interface{}, args ...interface{}) (client.WorkflowRun, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	c.options = options
	c.args = append([]any(nil), args...)
	if c.executeErr != nil {
		return nil, c.executeErr
	}
	return fakeWorkflowRun{id: options.ID, runID: "test-run"}, nil
}

func (c *workflowStartTestTemporal) CancelWorkflow(context.Context, string, string) error {
	return nil
}

func (c *workflowStartTestTemporal) SignalWorkflow(context.Context, string, string, string, interface{}) error {
	return nil
}

func (c *workflowStartTestTemporal) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func (c *workflowStartTestTemporal) LastStart() (client.StartWorkflowOptions, []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.options, append([]any(nil), c.args...)
}

func (c *workflowStartTestTemporal) SetError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.executeErr = err
}

func TestWorkflowStartInputHashCanonicalizesObjectKeys(t *testing.T) {
	left, leftHash, err := marshalWorkflowStartInput(map[string]any{"b": 2, "a": map[string]any{"d": 4, "c": 3}})
	if err != nil {
		t.Fatalf("marshal left: %v", err)
	}
	right, rightHash, err := marshalWorkflowStartInput(json.RawMessage(`{"a":{"c":3,"d":4},"b":2}`))
	if err != nil {
		t.Fatalf("marshal right: %v", err)
	}
	if string(left) != string(right) || leftHash != rightHash {
		t.Fatalf("canonical values differ: %s/%s %s/%s", left, leftHash, right, rightHash)
	}
}

func TestWorkflowStartVisibilityIncludesProductionAndEpisodeIdentity(t *testing.T) {
	workflowRunID := "workflow-run"
	searchAttributes, memo := workflowStartVisibility(workflowStartOutboxItem{
		WorkflowRunID: &workflowRunID,
		ProjectID:     "project", ProductionGenerationID: "generation", ProfileVersionID: "profile-version",
		WorkflowType: "batch_generate_shot_videos",
		Input:        json.RawMessage(`{"organizationId":"org","input":{"scriptEpisodeId":"episode","rebuildId":"rebuild"}}`),
	})
	for key, want := range map[string]string{
		"ProjectId": "project", "ProductionGenerationId": "generation", "EpisodeId": "episode", "ProfileVersionId": "profile-version", "RebuildId": "rebuild",
	} {
		if got := searchAttributes[key]; got != want || memo[key] != want {
			t.Fatalf("%s search=%v memo=%v, want %s", key, got, memo[key], want)
		}
	}
	if memo["WorkflowType"] != "batch_generate_shot_videos" || memo["WorkflowRunId"] != workflowRunID {
		t.Fatalf("memo = %+v", memo)
	}
}
