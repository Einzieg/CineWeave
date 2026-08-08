package projectcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommandRepositoryPersistsIdempotencyLeaseWorkflowAndEventsIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run project control integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect integration database: %v", err)
	}
	defer pool.Close()

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	userID := uuid.NewString()
	organizationID := uuid.NewString()
	workspaceID := uuid.NewString()
	projectID := uuid.NewString()
	controlKeyID := uuid.NewString()
	seedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin project control seed: %v", err)
	}
	defer seedTx.Rollback(ctx)
	seedStatements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id, email, display_name, status) VALUES ($1, $2, 'Project Control Test', 'active')`, []any{userID, fmt.Sprintf("project-control-%s@example.test", suffix)}},
		{`INSERT INTO organizations(id, name, slug) VALUES ($1, 'Project Control Test', $2)`, []any{organizationID, "project-control-" + suffix}},
		{`INSERT INTO workspaces(id, organization_id, name) VALUES ($1, $2, 'Test Workspace')`, []any{workspaceID, organizationID}},
		{`INSERT INTO projects(
			id, organization_id, workspace_id, name, created_by,
			video_production_state, active_video_production_generation_id,
			video_production_generation_no
		) VALUES ($1, $2, $3, 'Test Project', $4, 'unconfigured', NULL, NULL)`, []any{projectID, organizationID, workspaceID, userID}},
		{`INSERT INTO user_control_keys(id, user_id, public_id, prefix, secret_hash, credential_version) VALUES ($1, $2, $3, $4, $5, 0)`, []any{controlKeyID, userID, "key_" + suffix, "cwuk_v1_" + suffix[:8], strings.Repeat("a", 64)}},
	}
	for _, statement := range seedStatements {
		if _, err := seedTx.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed project control integration scope: %v", err)
		}
	}
	if err := seedTx.Commit(ctx); err != nil {
		t.Fatalf("commit project control seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	repository := NewRepository(pool)
	request := CreateCommand{
		OrganizationID: organizationID, WorkspaceID: workspaceID, ProjectID: projectID,
		ActorUserID: userID, ControllerType: ControllerCodexMCP, ControlKeyID: controlKeyID,
		Descriptor: testCommandDescriptor(), Input: json.RawMessage(`{"projectRevision":1}`),
		IdempotencyKey: "integration-" + suffix,
		Items: []CreateCommandItem{
			{ItemKey: "episode:1", TargetType: "source_chapter", Input: json.RawMessage(`{"revision":1}`)},
			{ItemKey: "episode:2", TargetType: "source_chapter", Input: json.RawMessage(`{"revision":1}`)},
		},
	}
	created, replayed, err := repository.Create(ctx, request)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if replayed || created.Status != CommandQueued || created.Revision != 1 {
		t.Fatalf("created command = %+v, replayed=%v", created, replayed)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_control_commands
		SET lease_owner = 'expired-integration-worker', lease_expires_at = now() - interval '1 second',
		    revision = revision + 1
		WHERE id = $1
	`, created.ID); err != nil {
		t.Fatalf("seed expired project control lease: %v", err)
	}
	runtimeSnapshot, err := repository.RuntimeSnapshot(ctx)
	if err != nil || runtimeSnapshot.ActiveCommands < 1 || runtimeSnapshot.ExpiredLeases < 1 {
		t.Fatalf("active runtime snapshot=%+v err=%v", runtimeSnapshot, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_control_commands
		SET lease_owner = NULL, lease_expires_at = NULL, revision = revision + 1
		WHERE id = $1
	`, created.ID); err != nil {
		t.Fatalf("clear expired project control lease: %v", err)
	}
	items, err := repository.Items(ctx, created.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("command items=%d err=%v", len(items), err)
	}
	var persistedItemInput struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(items[0].Input, &persistedItemInput); err != nil || persistedItemInput.Revision != 1 {
		t.Fatalf("command item input=%s err=%v", items[0].Input, err)
	}
	eventPage, err := repository.Events(ctx, created.ID, 0, 20)
	if err != nil || len(eventPage) != 1 || eventPage[0].EventType != "project.control.command.created" {
		t.Fatalf("created events=%+v err=%v", eventPage, err)
	}
	var outboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM event_outbox
		WHERE aggregate_type = 'project_control_command' AND aggregate_id = $1
	`, created.ID).Scan(&outboxCount); err != nil || outboxCount != 1 {
		t.Fatalf("created outbox count=%d err=%v", outboxCount, err)
	}

	replay, replayed, err := repository.Create(ctx, request)
	if err != nil || !replayed || replay.ID != created.ID {
		t.Fatalf("idempotent replay=%+v replayed=%v err=%v", replay, replayed, err)
	}
	conflictRequest := request
	conflictRequest.Items = append([]CreateCommandItem(nil), request.Items...)
	conflictRequest.Items[0].Input = json.RawMessage(`{"revision":2}`)
	if _, _, err := repository.Create(ctx, conflictRequest); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict err=%v", err)
	}

	page, err := repository.List(ctx, ListCommandsFilter{ActorUserID: userID, ProjectID: projectID, Limit: 1})
	if err != nil || len(page.Commands) != 1 || page.Commands[0].ID != created.ID {
		t.Fatalf("command page=%+v err=%v", page, err)
	}
	claim, err := repository.ClaimDispatch(ctx, "integration-worker", "integration-release", time.Minute)
	if err != nil || claim == nil || claim.Command.ID != created.ID || claim.Command.Status != CommandRunning {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if err := repository.FinishAttempt(ctx, claim.AttemptID, "succeeded", "", ""); err != nil {
		t.Fatalf("finish dispatch attempt: %v", err)
	}
	prompt, waitingInput, err := repository.CreatePrompt(ctx, CreateCommandPrompt{
		CommandID: claim.Command.ID, ExpectedRevision: claim.Command.Revision,
		PromptKind: "choose_target", Prompt: "请选择目标分集",
		Options:            json.RawMessage(`[{"value":"episode:1","label":"第 1 集"}]`),
		CandidateRevisions: json.RawMessage(`{"episode:1":1}`), ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil || waitingInput.Status != CommandWaitingInput || prompt.ExpectedCommandRevision != waitingInput.Revision {
		t.Fatalf("waiting input prompt=%+v command=%+v err=%v", prompt, waitingInput, err)
	}
	answered, resumed, replayedAnswer, err := repository.ResolvePrompt(ctx, ResolveCommandPrompt{
		CommandID: waitingInput.ID, PromptID: prompt.ID, ActorUserID: userID,
		ExpectedCommandRevision: waitingInput.Revision, IdempotencyKey: "answer-" + suffix,
		Answer: json.RawMessage(`{"value":"episode:1"}`), ResumeStatus: CommandQueued,
	})
	if err != nil || replayedAnswer || resumed.Status != CommandQueued || answered.Status != "answered" {
		t.Fatalf("resolved prompt=%+v command=%+v replayed=%v err=%v", answered, resumed, replayedAnswer, err)
	}
	_, replayedCommand, replayedAnswer, err := repository.ResolvePrompt(ctx, ResolveCommandPrompt{
		CommandID: resumed.ID, PromptID: prompt.ID, ActorUserID: userID,
		ExpectedCommandRevision: waitingInput.Revision, IdempotencyKey: "answer-" + suffix,
		Answer: json.RawMessage(`{"value":"episode:1"}`), ResumeStatus: CommandQueued,
	})
	if err != nil || !replayedAnswer || replayedCommand.ID != resumed.ID {
		t.Fatalf("prompt replay command=%+v replayed=%v err=%v", replayedCommand, replayedAnswer, err)
	}
	claim, err = repository.ClaimDispatch(ctx, "integration-worker", "integration-release", time.Minute)
	if err != nil || claim == nil || claim.Command.ID != created.ID || claim.AttemptNumber != 2 {
		t.Fatalf("resumed claim=%+v err=%v", claim, err)
	}
	if err := repository.FinishAttempt(ctx, claim.AttemptID, "succeeded", "", ""); err != nil {
		t.Fatalf("finish resumed dispatch attempt: %v", err)
	}
	waiting, err := repository.AttachWorkflow(ctx, created.ID, claim.Command.Revision, WorkflowLink{
		TemporalWorkflowID: "project-control-integration-" + created.ID,
		RelationType:       "primary",
	}, time.Now().Add(time.Second))
	if err != nil || waiting.Status != CommandWaitingWorkflow {
		t.Fatalf("waiting command=%+v err=%v", waiting, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE project_control_commands
		SET next_reconcile_at = now() - interval '1 second', revision = revision + 1
		WHERE id = $1
	`, waiting.ID); err != nil {
		t.Fatalf("make project control reconcile overdue: %v", err)
	}
	runtimeSnapshot, err = repository.RuntimeSnapshot(ctx)
	if err != nil || runtimeSnapshot.WaitingCommands < 1 ||
		runtimeSnapshot.OverdueReconciliations < 1 || runtimeSnapshot.OldestReconcileLagSeconds <= 0 {
		t.Fatalf("waiting runtime snapshot=%+v err=%v", runtimeSnapshot, err)
	}
	waiting, err = repository.Get(ctx, waiting.ID)
	if err != nil {
		t.Fatalf("refresh overdue waiting command: %v", err)
	}
	completed, err := repository.Transition(ctx, TransitionCommand{
		CommandID: created.ID, ExpectedRevision: waiting.Revision, Status: CommandSucceeded,
		Output: json.RawMessage(`{"completedItems":2}`),
	})
	if err != nil || !completed.Terminal() || completed.CompletedAt == nil {
		t.Fatalf("completed command=%+v err=%v", completed, err)
	}
	if _, err := repository.Transition(ctx, TransitionCommand{
		CommandID: created.ID, ExpectedRevision: completed.Revision, Status: CommandFailed,
		ErrorCode: "LATE_FAILURE",
	}); err == nil {
		t.Fatal("terminal command accepted another transition")
	}
	eventPage, err = repository.Events(ctx, created.ID, 0, 20)
	if err != nil || len(eventPage) != 7 {
		t.Fatalf("terminal events=%d err=%v", len(eventPage), err)
	}
	if eventPage[len(eventPage)-1].EventType != "project.control.command.succeeded" {
		t.Fatalf("terminal event=%s", eventPage[len(eventPage)-1].EventType)
	}
	activityPage, err := repository.List(ctx, ListCommandsFilter{
		ActorUserID: userID, ProjectID: projectID, ActivityView: true, Limit: 20,
	})
	if err != nil || len(activityPage.Commands) != 1 || activityPage.Commands[0].ID != completed.ID {
		t.Fatalf("activity page before watermark=%+v err=%v", activityPage, err)
	}
	auditRequest := request
	auditRequest.IdempotencyKey = "audit-" + suffix
	auditRequest.Items = nil
	auditRequest.Descriptor = testCommandDescriptor()
	auditRequest.Descriptor.ActivityVisibility = ActivityVisibilityAuditOnly
	auditCommand, _, err := repository.Create(ctx, auditRequest)
	if err != nil {
		t.Fatalf("create audit-only command: %v", err)
	}
	activityPage, err = repository.List(ctx, ListCommandsFilter{
		ActorUserID: userID, ProjectID: projectID, ActivityView: true, Limit: 20,
	})
	if err != nil || len(activityPage.Commands) != 1 || activityPage.Commands[0].ID == auditCommand.ID {
		t.Fatalf("activity page exposed audit-only command=%+v err=%v", activityPage, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workflow_activity_views(
			organization_id, project_id, user_id, cleared_terminal_through
		) VALUES ($1, $2, $3, now())
	`, organizationID, projectID, userID); err != nil {
		t.Fatalf("write activity watermark: %v", err)
	}
	activityPage, err = repository.List(ctx, ListCommandsFilter{
		ActorUserID: userID, ProjectID: projectID,
		Statuses: []CommandStatus{CommandSucceeded}, ActivityView: true, Limit: 20,
	})
	if err != nil || len(activityPage.Commands) != 0 {
		t.Fatalf("activity page after watermark=%+v err=%v", activityPage, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM event_outbox
		WHERE aggregate_type = 'project_control_command' AND aggregate_id = $1
	`, created.ID).Scan(&outboxCount); err != nil || outboxCount != len(eventPage) {
		t.Fatalf("terminal outbox count=%d events=%d err=%v", outboxCount, len(eventPage), err)
	}

	cancellableRequest := request
	cancellableRequest.IdempotencyKey = "cancel-" + suffix
	cancellableRequest.Items = nil
	cancellable, _, err := repository.Create(ctx, cancellableRequest)
	if err != nil {
		t.Fatalf("create cancellable command: %v", err)
	}
	cancelled, replayedCancel, err := repository.RequestCancellation(ctx, RequestCancellation{
		CommandID: cancellable.ID, ExpectedRevision: cancellable.Revision,
		ActorUserID: userID, IdempotencyKey: "cancel-request-" + suffix,
		Reason: "integration cancellation",
	})
	if err != nil || replayedCancel || cancelled.Status != CommandCancelled || cancelled.CancellationRequestedAt == nil {
		t.Fatalf("cancelled command=%+v replayed=%v err=%v", cancelled, replayedCancel, err)
	}
	replayedCancelled, replayedCancel, err := repository.RequestCancellation(ctx, RequestCancellation{
		CommandID: cancellable.ID, ExpectedRevision: cancellable.Revision,
		ActorUserID: userID, IdempotencyKey: "cancel-request-" + suffix,
	})
	if err != nil || !replayedCancel || replayedCancelled.ID != cancelled.ID {
		t.Fatalf("cancel replay=%+v replayed=%v err=%v", replayedCancelled, replayedCancel, err)
	}

	failedRequest := request
	failedRequest.IdempotencyKey = "failed-" + suffix
	failedRequest.Items = nil
	failed, _, err := repository.Create(ctx, failedRequest)
	if err != nil {
		t.Fatalf("create failed command: %v", err)
	}
	failed, err = repository.Transition(ctx, TransitionCommand{
		CommandID: failed.ID, ExpectedRevision: failed.Revision, Status: CommandFailed,
		ErrorCode: "TEST_FAILURE", ErrorMessage: "test failure",
	})
	if err != nil {
		t.Fatalf("fail command: %v", err)
	}
	retry, replayedRetry, err := repository.Retry(ctx, RetryCommand{
		CommandID: failed.ID, ExpectedRevision: failed.Revision,
		ActorUserID: userID, ControllerType: ControllerCodexMCP,
		ControlKeyID: controlKeyID, Descriptor: testCommandDescriptor(),
		IdempotencyKey: "retry-" + suffix,
	})
	if err != nil || replayedRetry || retry.RetryOfCommandID != failed.ID || retry.Status != CommandQueued {
		t.Fatalf("retry=%+v replayed=%v err=%v", retry, replayedRetry, err)
	}
	retryReplay, replayedRetry, err := repository.Retry(ctx, RetryCommand{
		CommandID: failed.ID, ExpectedRevision: failed.Revision,
		ActorUserID: userID, ControllerType: ControllerCodexMCP,
		ControlKeyID: controlKeyID, Descriptor: testCommandDescriptor(),
		IdempotencyKey: "retry-" + suffix,
	})
	if err != nil || !replayedRetry || retryReplay.ID != retry.ID {
		t.Fatalf("retry replay=%+v replayed=%v err=%v", retryReplay, replayedRetry, err)
	}
	if _, _, err := repository.Retry(ctx, RetryCommand{
		CommandID: failed.ID, ExpectedRevision: failed.Revision,
		ActorUserID: userID, ControllerType: ControllerCodexMCP,
		ControlKeyID: controlKeyID, Descriptor: testCommandDescriptor(),
		IdempotencyKey: "retry-conflict-" + suffix,
	}); !errors.Is(err, ErrRetryAlreadyActive) {
		t.Fatalf("parallel retry err=%v", err)
	}
}
