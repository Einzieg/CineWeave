package projectcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestTemporalWorkflowIdentityIsDeterministicAndVersioned(t *testing.T) {
	first, err := TemporalWorkflowIdentity("command-1", "episode:1", 2)
	if err != nil {
		t.Fatalf("derive first workflow identity: %v", err)
	}
	second, err := TemporalWorkflowIdentity("command-1", "episode:1", 2)
	if err != nil {
		t.Fatalf("derive second workflow identity: %v", err)
	}
	otherItem, _ := TemporalWorkflowIdentity("command-1", "episode:2", 2)
	otherVersion, _ := TemporalWorkflowIdentity("command-1", "episode:1", 3)
	if first != second {
		t.Fatalf("deterministic identities differ: %s != %s", first, second)
	}
	if first == otherItem || first == otherVersion {
		t.Fatalf("workflow identity did not include item key and action version: %s", first)
	}
}

func TestDispatcherStopsAfterThreeRetryableAttempts(t *testing.T) {
	repository := newMemoryRuntimeRepository(testRuntimeCommand(ExecutionModeAsyncCommand), nil)
	registry := mustRuntimeRegistry(t, testRuntimeDescriptor(ExecutionModeAsyncCommand), HandlerFunc(
		func(context.Context, DispatchRequest) (DispatchOutcome, error) {
			return DispatchOutcome{}, NewRuntimeFailure("UPSTREAM_TRANSIENT", "临时故障", true, nil)
		},
	))
	dispatcher := testDispatcher(repository, registry)
	for attempt := 1; attempt <= 3; attempt++ {
		processed, err := dispatcher.RunOnce(context.Background())
		if err != nil || !processed {
			t.Fatalf("dispatch attempt %d processed=%v err=%v", attempt, processed, err)
		}
		if attempt < 3 && repository.command.Status != CommandQueued {
			t.Fatalf("attempt %d status=%s, want queued", attempt, repository.command.Status)
		}
	}
	if repository.command.Status != CommandFailed || repository.command.ErrorCode != "UPSTREAM_TRANSIENT" {
		t.Fatalf("terminal command=%+v", repository.command)
	}
	if repository.dispatchAttempts != 3 {
		t.Fatalf("dispatch attempts=%d, want 3", repository.dispatchAttempts)
	}
	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("terminal command was reclaimed: processed=%v err=%v", processed, err)
	}
}

func TestDispatcherReusesWorkflowIdentityAfterStartBeforeAssociationFailure(t *testing.T) {
	items := []CommandItem{{
		ID: "item-id-1", CommandID: "command-1", ItemKey: "episode:1",
		TargetType: "script_episode", Status: "queued", Output: json.RawMessage(`{}`),
	}}
	repository := newMemoryRuntimeRepository(testRuntimeCommand(ExecutionModeWorkflow), items)
	started := make(map[string]int)
	registry := mustRuntimeRegistry(t, testRuntimeDescriptor(ExecutionModeWorkflow), HandlerFunc(
		func(_ context.Context, request DispatchRequest) (DispatchOutcome, error) {
			workflowID, err := request.TemporalWorkflowID("episode:1")
			if err != nil {
				return DispatchOutcome{}, err
			}
			started[workflowID]++
			if request.AttemptNumber == 1 {
				return DispatchOutcome{}, NewRuntimeFailure(
					"START_ASSOCIATION_INTERRUPTED", "工作流已启动但关联提交中断", true, nil,
				)
			}
			return DispatchOutcome{WorkflowLinks: []WorkflowLink{{
				CommandItemID: "item-id-1", WorkflowRunID: "workflow-run-1",
				TemporalWorkflowID: workflowID, RelationType: WorkflowRelationDeterministicChild,
			}}}, nil
		},
	))
	dispatcher := testDispatcher(repository, registry)
	for attempt := 1; attempt <= 2; attempt++ {
		if processed, err := dispatcher.RunOnce(context.Background()); err != nil || !processed {
			t.Fatalf("dispatch attempt %d processed=%v err=%v", attempt, processed, err)
		}
	}
	if len(started) != 1 {
		t.Fatalf("dispatcher generated %d external workflow identities, want one", len(started))
	}
	for _, calls := range started {
		if calls != 2 {
			t.Fatalf("workflow identity was not reused across attempts: calls=%d", calls)
		}
	}
	if repository.command.Status != CommandWaitingWorkflow || len(repository.links) != 1 {
		t.Fatalf("command=%+v links=%+v", repository.command, repository.links)
	}
	if repository.items[0].Status != "waiting_workflow" {
		t.Fatalf("item status=%s, want waiting_workflow", repository.items[0].Status)
	}
}

func TestValidateDispatchOutcomeAcceptsPersistedDomainIdempotentWorkflow(t *testing.T) {
	command := testRuntimeCommand(ExecutionModeWorkflow)
	command.ID = "command-1"
	outcome := DispatchOutcome{WorkflowLinks: []WorkflowLink{{
		WorkflowRunID: "workflow-run-1", TemporalWorkflowID: "source-to-script:existing",
		RelationType: WorkflowRelationDomainIdempotentChild,
	}}}
	if err := validateDispatchOutcome(command, nil, outcome); err != nil {
		t.Fatalf("validate domain-idempotent workflow: %v", err)
	}
}

func TestValidateDispatchOutcomeRejectsDomainIdempotentWorkflowWithoutRun(t *testing.T) {
	command := testRuntimeCommand(ExecutionModeWorkflow)
	outcome := DispatchOutcome{WorkflowLinks: []WorkflowLink{{
		TemporalWorkflowID: "source-to-script:existing",
		RelationType:       WorkflowRelationDomainIdempotentChild,
	}}}
	if err := validateDispatchOutcome(command, nil, outcome); err == nil {
		t.Fatal("expected domain-idempotent workflow without run ID to fail validation")
	}
}

func TestReconcilerDoesNotCompleteTerminalWorkflowWithActiveProviderTask(t *testing.T) {
	command := testRuntimeCommand(ExecutionModeWorkflow)
	command.Status = CommandWaitingWorkflow
	command.Revision = 4
	repository := newMemoryRuntimeRepository(command, nil)
	repository.links = []WorkflowLink{{TemporalWorkflowID: "workflow-1", RelationType: "primary"}}
	reconciler := testReconciler(repository, WorkflowTrackerFunc(
		func(_ context.Context, _ Command, links []WorkflowLink) ([]WorkflowExecutionState, error) {
			return []WorkflowExecutionState{{
				Link: links[0], Status: "succeeded", Active: true, ActiveProviderTasks: 1,
			}}, nil
		},
	))
	processed, err := reconciler.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("reconcile processed=%v err=%v", processed, err)
	}
	if repository.command.Status != CommandWaitingWorkflow {
		t.Fatalf("command status=%s, want waiting_workflow", repository.command.Status)
	}
}

func TestReconcilerAggregatesIndependentItemsAsPartialSuccess(t *testing.T) {
	command := testRuntimeCommand(ExecutionModeWorkflow)
	command.Status = CommandWaitingWorkflow
	command.Revision = 7
	items := []CommandItem{
		{ID: "item-1", CommandID: command.ID, ItemKey: "episode:1", Status: "waiting_workflow", Output: json.RawMessage(`{}`)},
		{ID: "item-2", CommandID: command.ID, ItemKey: "episode:2", Status: "waiting_workflow", Output: json.RawMessage(`{}`)},
	}
	repository := newMemoryRuntimeRepository(command, items)
	repository.links = []WorkflowLink{
		{CommandItemID: "item-1", TemporalWorkflowID: "workflow-1", RelationType: "primary"},
		{CommandItemID: "item-2", TemporalWorkflowID: "workflow-2", RelationType: "primary"},
	}
	reconciler := testReconciler(repository, WorkflowTrackerFunc(
		func(_ context.Context, _ Command, links []WorkflowLink) ([]WorkflowExecutionState, error) {
			return []WorkflowExecutionState{
				{Link: links[0], Status: "succeeded"},
				{Link: links[1], Status: "failed", ErrorCode: "SHOT_FAILED", ErrorMessage: "镜头生成失败"},
			}, nil
		},
	))
	processed, err := reconciler.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("reconcile processed=%v err=%v", processed, err)
	}
	if repository.command.Status != CommandPartialSucceeded {
		t.Fatalf("command status=%s, want partial_succeeded", repository.command.Status)
	}
	if repository.items[0].Status != "succeeded" || repository.items[1].Status != "failed" {
		t.Fatalf("item statuses=%s/%s", repository.items[0].Status, repository.items[1].Status)
	}
}

func TestReconcilerPropagatesCancellationBeforeTerminalizingCommand(t *testing.T) {
	command := testRuntimeCommand(ExecutionModeWorkflow)
	requestedAt := time.Now()
	command.Status = CommandWaitingWorkflow
	command.CancellationRequestedAt = &requestedAt
	items := []CommandItem{{
		ID: "item-1", CommandID: command.ID, ItemKey: "shot:1",
		Status: "waiting_workflow", Output: json.RawMessage(`{}`),
	}}
	repository := newMemoryRuntimeRepository(command, items)
	repository.links = []WorkflowLink{{
		CommandItemID: "item-1", TemporalWorkflowID: "workflow-1", RelationType: "primary",
	}}
	canceller := &recordingWorkflowCanceller{}
	reconciler := testReconciler(repository, WorkflowTrackerFunc(
		func(context.Context, Command, []WorkflowLink) ([]WorkflowExecutionState, error) {
			return []WorkflowExecutionState{{Link: repository.links[0], Status: "running", Active: true}}, nil
		},
	))
	reconciler.Canceller = canceller
	processed, err := reconciler.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("active cancellation processed=%v err=%v", processed, err)
	}
	if canceller.calls != 1 || repository.command.Terminal() {
		t.Fatalf("canceller calls=%d command status=%s", canceller.calls, repository.command.Status)
	}

	reconciler.Tracker = WorkflowTrackerFunc(
		func(context.Context, Command, []WorkflowLink) ([]WorkflowExecutionState, error) {
			return []WorkflowExecutionState{{Link: repository.links[0], Status: "cancelled"}}, nil
		},
	)
	processed, err = reconciler.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("terminal cancellation processed=%v err=%v", processed, err)
	}
	if repository.command.Status != CommandCancelled || repository.items[0].Status != "cancelled" {
		t.Fatalf("command/item status=%s/%s", repository.command.Status, repository.items[0].Status)
	}
}

type recordingWorkflowCanceller struct {
	calls int
}

func (c *recordingWorkflowCanceller) Cancel(context.Context, Command, []WorkflowLink) error {
	c.calls++
	return nil
}

type memoryRuntimeRepository struct {
	command           Command
	items             []CommandItem
	links             []WorkflowLink
	dispatchAttempts  int
	reconcileAttempts int
	attempts          map[string]string
}

func newMemoryRuntimeRepository(command Command, items []CommandItem) *memoryRuntimeRepository {
	copyItems := append([]CommandItem(nil), items...)
	return &memoryRuntimeRepository{command: command, items: copyItems, attempts: make(map[string]string)}
}

func (r *memoryRuntimeRepository) Get(_ context.Context, commandID string) (Command, error) {
	if commandID != r.command.ID {
		return Command{}, ErrCommandNotFound
	}
	return r.command, nil
}

func (r *memoryRuntimeRepository) ClaimDispatch(_ context.Context, _, _ string, _ time.Duration) (*Claim, error) {
	if r.command.Status != CommandQueued && r.command.Status != CommandRunning {
		return nil, nil
	}
	r.dispatchAttempts++
	r.command.Status = CommandRunning
	r.command.Revision++
	attemptID := fmt.Sprintf("dispatch-%d", r.dispatchAttempts)
	r.attempts[attemptID] = "running"
	return &Claim{Command: r.command, AttemptID: attemptID, AttemptNumber: r.dispatchAttempts, AttemptKind: "dispatch"}, nil
}

func (r *memoryRuntimeRepository) ClaimReconcile(_ context.Context, _, _ string, _ time.Duration) (*Claim, error) {
	if r.command.Status != CommandWaitingWorkflow && r.command.CancellationRequestedAt == nil {
		return nil, nil
	}
	r.reconcileAttempts++
	r.command.Revision++
	attemptID := fmt.Sprintf("reconcile-%d", r.reconcileAttempts)
	r.attempts[attemptID] = "running"
	return &Claim{Command: r.command, AttemptID: attemptID, AttemptNumber: r.reconcileAttempts, AttemptKind: "reconcile"}, nil
}

func (r *memoryRuntimeRepository) Items(context.Context, string) ([]CommandItem, error) {
	return append([]CommandItem(nil), r.items...), nil
}

func (r *memoryRuntimeRepository) WorkflowLinks(context.Context, string) ([]WorkflowLink, error) {
	return append([]WorkflowLink(nil), r.links...), nil
}

func (r *memoryRuntimeRepository) ApplyItemResults(_ context.Context, commandID string, expectedRevision int64, results []ItemResult) (Command, error) {
	if commandID != r.command.ID || expectedRevision != r.command.Revision {
		return Command{}, ErrRevisionConflict
	}
	for _, result := range results {
		found := false
		for index := range r.items {
			if r.items[index].ID != result.CommandItemID {
				continue
			}
			found = true
			r.items[index].Status = result.Status
			r.items[index].Retryable = result.Retryable
			r.items[index].ErrorCode = result.ErrorCode
			r.items[index].ErrorMessage = result.ErrorMessage
			if len(result.Output) > 0 {
				r.items[index].Output = cloneRawMessage(result.Output)
			}
		}
		if !found {
			return Command{}, errors.New("item not found")
		}
	}
	r.command.Revision++
	return r.command, nil
}

func (r *memoryRuntimeRepository) AttachWorkflows(_ context.Context, commandID string, expectedRevision int64, links []WorkflowLink, _ time.Time) (Command, error) {
	if commandID != r.command.ID || expectedRevision != r.command.Revision {
		return Command{}, ErrRevisionConflict
	}
	r.links = append([]WorkflowLink(nil), links...)
	for _, link := range links {
		for index := range r.items {
			if r.items[index].ID == link.CommandItemID {
				r.items[index].Status = "waiting_workflow"
			}
		}
	}
	r.command.Status = CommandWaitingWorkflow
	r.command.Revision++
	return r.command, nil
}

func (r *memoryRuntimeRepository) CreatePrompt(_ context.Context, request CreateCommandPrompt) (CommandPrompt, Command, error) {
	if request.CommandID != r.command.ID || request.ExpectedRevision != r.command.Revision {
		return CommandPrompt{}, Command{}, ErrRevisionConflict
	}
	r.command.Status = CommandWaitingInput
	r.command.Revision++
	return CommandPrompt{ID: "prompt-1", CommandID: r.command.ID}, r.command, nil
}

func (r *memoryRuntimeRepository) ExpireNextPrompt(context.Context) (Command, bool, error) {
	return Command{}, false, nil
}

func (r *memoryRuntimeRepository) RescheduleReconcile(_ context.Context, commandID string, expectedRevision int64, _ time.Time) (Command, error) {
	if commandID != r.command.ID || expectedRevision != r.command.Revision {
		return Command{}, ErrRevisionConflict
	}
	r.command.Revision++
	return r.command, nil
}

func (r *memoryRuntimeRepository) Transition(_ context.Context, request TransitionCommand) (Command, error) {
	if request.CommandID != r.command.ID || request.ExpectedRevision != r.command.Revision {
		return Command{}, ErrRevisionConflict
	}
	if r.command.Terminal() {
		return Command{}, errors.New("terminal command is immutable")
	}
	r.command.Status = request.Status
	r.command.Output = cloneRawMessage(request.Output)
	r.command.ErrorCode = request.ErrorCode
	r.command.ErrorMessage = request.ErrorMessage
	r.command.Revision++
	return r.command, nil
}

func (r *memoryRuntimeRepository) FinishAttempt(_ context.Context, attemptID, status, _, _ string) error {
	if r.attempts[attemptID] != "running" {
		return errors.New("attempt not found")
	}
	r.attempts[attemptID] = status
	return nil
}

func testRuntimeCommand(mode ExecutionMode) Command {
	return Command{
		ID: "command-1", OrganizationID: "organization-1", WorkspaceID: "workspace-1",
		ProjectID: "project-1", ActorUserID: "user-1", ActionName: "test.runtime",
		ActionVersion: 1, ExecutionMode: mode, ActivityVisibility: ActivityVisibilityPrimary,
		Status: CommandQueued, Input: json.RawMessage(`{}`), Output: json.RawMessage(`{}`), Revision: 1,
	}
}

func testRuntimeDescriptor(mode ExecutionMode) Descriptor {
	descriptor := testCommandDescriptor()
	descriptor.Name = "test.runtime"
	descriptor.ExecutionMode = mode
	descriptor.StartsWorkflow = mode == ExecutionModeWorkflow
	descriptor.Effects.StartsWorkflow = descriptor.StartsWorkflow
	return descriptor
}

func mustRuntimeRegistry(t *testing.T, descriptor Descriptor, handler Handler) *RuntimeRegistry {
	t.Helper()
	registry, err := NewRuntimeRegistry(RegisteredHandler{Descriptor: descriptor, Handler: handler})
	if err != nil {
		t.Fatalf("create runtime registry: %v", err)
	}
	return registry
}

func testDispatcher(repository RuntimeRepository, registry *RuntimeRegistry) *Dispatcher {
	return &Dispatcher{
		Repository: repository, Registry: registry, Owner: "test-worker",
		ReleaseID: "test-release", LeaseDuration: time.Minute, MaximumAttempts: 3,
		ReconcileDelay: time.Second,
	}
}

func testReconciler(repository RuntimeRepository, tracker WorkflowTracker) *Reconciler {
	return &Reconciler{
		Repository: repository, Tracker: tracker, Owner: "test-worker",
		ReleaseID: "test-release", LeaseDuration: time.Minute, ReconcileDelay: time.Second,
	}
}
