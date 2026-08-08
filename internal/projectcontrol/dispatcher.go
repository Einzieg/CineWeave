package projectcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/observability"
)

type Dispatcher struct {
	Repository      RuntimeRepository
	Registry        *RuntimeRegistry
	Owner           string
	ReleaseID       string
	LeaseDuration   time.Duration
	MaximumAttempts int
	ReconcileDelay  time.Duration
}

func (d *Dispatcher) RunOnce(ctx context.Context) (processed bool, runErr error) {
	startedAt := time.Now()
	actionName := "unknown"
	attemptNumber := 0
	reclaimed := false
	defer func() {
		if !processed && runErr == nil {
			return
		}
		result := "handled"
		if runErr != nil {
			result = "failed"
		}
		observability.RecordProjectControlDispatch(
			actionName, result, reclaimed, attemptNumber, time.Since(startedAt),
		)
		if runErr != nil {
			logCommandError(ctx, "project control dispatch attempt failed",
				"result", result, "durationMs", time.Since(startedAt).Milliseconds(), "error", runErr)
		}
	}()
	if err := d.validate(); err != nil {
		return false, err
	}
	claim, err := d.Repository.ClaimDispatch(ctx, d.Owner, d.ReleaseID, d.LeaseDuration)
	if err != nil || claim == nil {
		return claim != nil, err
	}
	actionName = claim.Command.ActionName
	attemptNumber = claim.AttemptNumber
	reclaimed = claim.Reclaimed
	ctx = withCommandLogContext(ctx, claim.Command, d.ReleaseID, claim.AttemptNumber, "dispatch")
	logCommandInfo(ctx, "project control dispatch attempt started", "reclaimed", claim.Reclaimed)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	registration, ok := d.Registry.Get(claim.Command.ActionName, claim.Command.ActionVersion)
	if !ok {
		return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
			"PROJECT_CONTROL_ACTION_NOT_REGISTERED",
			fmt.Sprintf("动作 %s v%d 未在当前 Worker 注册", claim.Command.ActionName, claim.Command.ActionVersion),
			false, nil,
		))
	}
	if registration.Descriptor.ExecutionMode != claim.Command.ExecutionMode {
		return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
			"PROJECT_CONTROL_ACTION_CONTRACT_MISMATCH",
			"命令执行模式与当前动作契约不一致",
			false, nil,
		))
	}
	if claim.Command.ExecutionMode == ExecutionModeSync {
		return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
			"PROJECT_CONTROL_SYNC_COMMAND_QUEUED",
			"同步命令不能进入异步派发队列",
			false, nil,
		))
	}
	items, err := d.Repository.Items(ctx, claim.Command.ID)
	if err != nil {
		return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
			"PROJECT_CONTROL_ITEMS_UNAVAILABLE", "读取命令执行单元失败", true, err,
		))
	}
	outcome, err := registration.Handler.Execute(ctx, DispatchRequest{
		Command: claim.Command, Items: items, AttemptNumber: claim.AttemptNumber,
	})
	if err != nil {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		return true, d.failDispatch(ctx, claim, classifyRuntimeFailure(err))
	}
	if err := validateDispatchOutcome(claim.Command, items, outcome); err != nil {
		return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
			"PROJECT_CONTROL_HANDLER_RESULT_INVALID", "动作执行器返回了无效结果", false, err,
		))
	}
	latest, err := d.Repository.Get(ctx, claim.Command.ID)
	if err != nil {
		return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
			"PROJECT_CONTROL_COMMAND_REFRESH_FAILED", "刷新命令状态失败", true, err,
		))
	}
	if latest.CancellationRequestedAt != nil {
		if len(outcome.WorkflowLinks) > 0 {
			delay := outcome.NextReconcileAfter
			if delay <= 0 {
				delay = d.reconcileDelay()
			}
			if _, err := d.Repository.AttachWorkflows(ctx, latest.ID, latest.Revision,
				outcome.WorkflowLinks, time.Now().Add(delay)); err != nil {
				return true, d.failDispatch(ctx, claimAtCommand(claim, latest), NewRuntimeFailure(
					"PROJECT_CONTROL_CANCEL_WORKFLOW_LINK_FAILED", "保存待取消子工作流失败", true, err,
				))
			}
			return true, d.finishAttempt(ctx, claim, "cancelled", "", "")
		}
		if _, err := d.Repository.Transition(ctx, TransitionCommand{
			CommandID: latest.ID, ExpectedRevision: latest.Revision, Status: CommandCancelled,
			EventPayload: map[string]any{"phase": "cancelled_before_workflow_attach"},
		}); err != nil {
			return true, err
		}
		return true, d.finishAttempt(ctx, claim, "cancelled", "", "")
	}

	current := latest
	if len(outcome.ItemResults) > 0 {
		current, err = d.Repository.ApplyItemResults(ctx, current.ID, current.Revision, outcome.ItemResults)
		if err != nil {
			return true, d.failDispatch(ctx, claim, NewRuntimeFailure(
				"PROJECT_CONTROL_ITEM_RESULT_PERSIST_FAILED", "保存命令执行单元结果失败", true, err,
			))
		}
	}
	if outcome.Prompt != nil {
		_, _, err = d.Repository.CreatePrompt(ctx, CreateCommandPrompt{
			CommandID: current.ID, ExpectedRevision: current.Revision,
			PromptKind: outcome.Prompt.Kind, Prompt: outcome.Prompt.Prompt,
			Options: outcome.Prompt.Options, CandidateRevisions: outcome.Prompt.CandidateRevisions,
			ExpiresAt: outcome.Prompt.ExpiresAt,
		})
		if err != nil {
			return true, d.failDispatch(ctx, claimAtCommand(claim, current), NewRuntimeFailure(
				"PROJECT_CONTROL_PROMPT_PERSIST_FAILED", "保存待用户确认的问题失败", true, err,
			))
		}
		logCommandInfo(ctx, "project control command is waiting for user input", "promptKind", outcome.Prompt.Kind)
		return true, d.finishAttempt(ctx, claim, "succeeded", "", "")
	}
	if len(outcome.WorkflowLinks) > 0 {
		delay := outcome.NextReconcileAfter
		if delay <= 0 {
			delay = d.reconcileDelay()
		}
		_, err = d.Repository.AttachWorkflows(ctx, current.ID, current.Revision,
			outcome.WorkflowLinks, time.Now().Add(delay))
		if err != nil {
			return true, d.failDispatch(ctx, claimAtCommand(claim, current), NewRuntimeFailure(
				"PROJECT_CONTROL_WORKFLOW_LINK_FAILED", "关联子工作流失败", true, err,
			))
		}
		logCommandInfo(ctx, "project control workflows attached", workflowLogFields(outcome.WorkflowLinks)...)
		return true, d.finishAttempt(ctx, claim, "succeeded", "", "")
	}

	output := outcome.Output
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	_, err = d.Repository.Transition(ctx, TransitionCommand{
		CommandID: current.ID, ExpectedRevision: current.Revision,
		Status: CommandSucceeded, Output: output,
	})
	if err != nil {
		return true, d.failDispatch(ctx, claimAtCommand(claim, current), NewRuntimeFailure(
			"PROJECT_CONTROL_COMMAND_FINALIZE_FAILED", "保存命令完成状态失败", true, err,
		))
	}
	logCommandInfo(ctx, "project control command completed", "status", CommandSucceeded)
	return true, d.finishAttempt(ctx, claim, "succeeded", "", "")
}

func (d *Dispatcher) failDispatch(ctx context.Context, claim *Claim, failure *RuntimeFailure) error {
	if failure == nil {
		failure = NewRuntimeFailure("PROJECT_CONTROL_EXECUTION_FAILED", "命令执行失败", true, nil)
	}
	code := strings.TrimSpace(failure.Code)
	if code == "" {
		code = "PROJECT_CONTROL_EXECUTION_FAILED"
	}
	message := strings.TrimSpace(failure.Message)
	if message == "" {
		message = "命令执行失败"
	}
	if err := d.finishAttempt(ctx, claim, "failed", code, message); err != nil {
		return err
	}
	if failure.Retryable && claim.AttemptNumber < d.maximumAttempts() {
		logCommandWarning(ctx, "project control dispatch retry queued",
			"errorCode", code, "nextAttemptNumber", claim.AttemptNumber+1)
		_, err := d.Repository.Transition(ctx, TransitionCommand{
			CommandID: claim.Command.ID, ExpectedRevision: claim.Command.Revision,
			Status:    CommandQueued,
			EventType: "project.control.command.progress",
			EventPayload: map[string]any{
				"phase": "automatic_retry_queued", "attemptNumber": claim.AttemptNumber,
				"errorCode": code, "nextAttemptNumber": claim.AttemptNumber + 1,
			},
		})
		return err
	}
	logCommandError(ctx, "project control dispatch exhausted",
		"errorCode", code, "retryable", failure.Retryable)
	_, err := d.Repository.Transition(ctx, TransitionCommand{
		CommandID: claim.Command.ID, ExpectedRevision: claim.Command.Revision,
		Status: CommandFailed, ErrorCode: code, ErrorMessage: message,
		EventPayload: map[string]any{
			"attemptNumber": claim.AttemptNumber, "retryable": failure.Retryable,
			"automaticAttemptsExhausted": failure.Retryable,
		},
	})
	return err
}

func (d *Dispatcher) finishAttempt(ctx context.Context, claim *Claim, status, code, message string) error {
	if claim == nil || strings.TrimSpace(claim.AttemptID) == "" {
		return fmt.Errorf("project control dispatch claim is invalid")
	}
	return d.Repository.FinishAttempt(ctx, claim.AttemptID, status, code, message)
}

func claimAtCommand(claim *Claim, command Command) *Claim {
	if claim == nil {
		return nil
	}
	copy := *claim
	copy.Command = command
	return &copy
}

func (d *Dispatcher) validate() error {
	if d == nil || d.Repository == nil || d.Registry == nil {
		return fmt.Errorf("project control dispatcher dependencies are unavailable")
	}
	if strings.TrimSpace(d.Owner) == "" || strings.TrimSpace(d.ReleaseID) == "" {
		return fmt.Errorf("project control dispatcher owner and release ID are required")
	}
	if d.LeaseDuration <= 0 {
		return fmt.Errorf("project control dispatcher lease duration must be positive")
	}
	return nil
}

func (d *Dispatcher) maximumAttempts() int {
	if d.MaximumAttempts <= 0 {
		return DefaultMaximumDispatchAttempts
	}
	return d.MaximumAttempts
}

func (d *Dispatcher) reconcileDelay() time.Duration {
	if d.ReconcileDelay <= 0 {
		return defaultReconcileDelay
	}
	return d.ReconcileDelay
}

func classifyRuntimeFailure(err error) *RuntimeFailure {
	var failure *RuntimeFailure
	if errors.As(err, &failure) {
		return failure
	}
	return NewRuntimeFailure("PROJECT_CONTROL_EXECUTION_FAILED", "命令执行失败", true, err)
}

func validateDispatchOutcome(command Command, items []CommandItem, outcome DispatchOutcome) error {
	if outcome.Prompt != nil && len(outcome.WorkflowLinks) > 0 {
		return fmt.Errorf("handler cannot request input and attach workflows in the same result")
	}
	itemKeys := make(map[string]string, len(items))
	for _, item := range items {
		itemKeys[item.ID] = item.ItemKey
	}
	resultItems := make(map[string]struct{}, len(outcome.ItemResults))
	for _, result := range outcome.ItemResults {
		if _, exists := itemKeys[result.CommandItemID]; !exists {
			return fmt.Errorf("handler returned unknown command item %s", result.CommandItemID)
		}
		if _, duplicated := resultItems[result.CommandItemID]; duplicated {
			return fmt.Errorf("handler returned command item %s more than once", result.CommandItemID)
		}
		resultItems[result.CommandItemID] = struct{}{}
	}
	workflowIDs := make(map[string]struct{}, len(outcome.WorkflowLinks))
	for _, link := range outcome.WorkflowLinks {
		itemKey := ""
		if link.CommandItemID != "" {
			var exists bool
			itemKey, exists = itemKeys[link.CommandItemID]
			if !exists {
				return fmt.Errorf("workflow link references unknown command item %s", link.CommandItemID)
			}
		}
		relationType := strings.TrimSpace(link.RelationType)
		switch relationType {
		case WorkflowRelationDeterministicChild:
			expectedID, err := TemporalWorkflowIdentity(command.ID, itemKey, command.ActionVersion)
			if err != nil {
				return err
			}
			if link.TemporalWorkflowID != expectedID {
				return fmt.Errorf("workflow identity %q does not match deterministic identity %q", link.TemporalWorkflowID, expectedID)
			}
		case WorkflowRelationDomainIdempotentChild:
			if strings.TrimSpace(link.WorkflowRunID) == "" {
				return fmt.Errorf("domain-idempotent workflow relation requires a workflow run ID")
			}
		default:
			return fmt.Errorf("workflow relation type %q is unsupported", link.RelationType)
		}
		if _, exists := workflowIDs[link.TemporalWorkflowID]; exists {
			return fmt.Errorf("workflow identity %q is duplicated", link.TemporalWorkflowID)
		}
		workflowIDs[link.TemporalWorkflowID] = struct{}{}
	}
	if outcome.Prompt != nil {
		if strings.TrimSpace(outcome.Prompt.Kind) == "" || strings.TrimSpace(outcome.Prompt.Prompt) == "" {
			return fmt.Errorf("prompt kind and text are required")
		}
		if !outcome.Prompt.ExpiresAt.After(time.Now()) {
			return fmt.Errorf("prompt expiration must be in the future")
		}
	}
	return nil
}
