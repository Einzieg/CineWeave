package projectcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/observability"
)

type WorkflowExecutionState struct {
	Link                WorkflowLink
	Status              string
	Active              bool
	ActiveNodeRuns      int
	ActiveProviderTasks int
	ActiveCheckpoints   int
	ErrorCode           string
	ErrorMessage        string
	Output              json.RawMessage
}

func (s WorkflowExecutionState) Terminal() bool {
	return !s.Active && terminalWorkflowStatus(s.Status)
}

type WorkflowTracker interface {
	Inspect(context.Context, Command, []WorkflowLink) ([]WorkflowExecutionState, error)
}

type WorkflowCanceller interface {
	Cancel(context.Context, Command, []WorkflowLink) error
}

type WorkflowTrackerFunc func(context.Context, Command, []WorkflowLink) ([]WorkflowExecutionState, error)

func (f WorkflowTrackerFunc) Inspect(ctx context.Context, command Command, links []WorkflowLink) ([]WorkflowExecutionState, error) {
	return f(ctx, command, links)
}

type Reconciler struct {
	Repository     RuntimeRepository
	Tracker        WorkflowTracker
	Canceller      WorkflowCanceller
	Owner          string
	ReleaseID      string
	LeaseDuration  time.Duration
	ReconcileDelay time.Duration
}

func (r *Reconciler) RunOnce(ctx context.Context) (processed bool, runErr error) {
	startedAt := time.Now()
	defer func() {
		if !processed && runErr == nil {
			return
		}
		result := "handled"
		if runErr != nil {
			result = "failed"
		}
		observability.RecordProjectControlReconcile(result, time.Since(startedAt))
	}()
	if err := r.validate(); err != nil {
		return false, err
	}
	if _, expired, err := r.Repository.ExpireNextPrompt(ctx); err != nil || expired {
		return expired, err
	}
	claim, err := r.Repository.ClaimReconcile(ctx, r.Owner, r.ReleaseID, r.LeaseDuration)
	if err != nil || claim == nil {
		return claim != nil, err
	}
	ctx = withCommandLogContext(ctx, claim.Command, r.ReleaseID, claim.AttemptNumber, "reconcile")
	links, err := r.Repository.WorkflowLinks(ctx, claim.Command.ID)
	if err != nil {
		return true, r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_WORKFLOW_LINKS_UNAVAILABLE", "读取子工作流关联失败", err)
	}
	if len(links) == 0 && claim.Command.CancellationRequestedAt != nil {
		return true, r.cancelCommand(ctx, claim, nil, "cancelled_without_child_workflow")
	}
	if len(links) == 0 {
		return true, r.failCommand(ctx, claim, "PROJECT_CONTROL_WORKFLOW_LINK_MISSING", "命令没有可对账的子工作流")
	}
	states, err := r.Tracker.Inspect(ctx, claim.Command, links)
	if err != nil {
		return true, r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_WORKFLOW_INSPECTION_FAILED", "读取子工作流状态失败", err)
	}
	if err := validateWorkflowStates(links, states); err != nil {
		return true, r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_WORKFLOW_STATE_INCOMPLETE", "子工作流状态尚不完整", err)
	}
	if claim.Command.CancellationRequestedAt != nil {
		activeLinks := make([]WorkflowLink, 0, len(states))
		for _, state := range states {
			if state.Active || !state.Terminal() {
				activeLinks = append(activeLinks, state.Link)
			}
		}
		if len(activeLinks) > 0 {
			if r.Canceller == nil {
				return true, r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_CANCELLER_UNAVAILABLE", "子工作流取消器不可用", nil)
			}
			if err := r.Canceller.Cancel(ctx, claim.Command, activeLinks); err != nil {
				return true, r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_CHILD_CANCEL_FAILED", "请求取消子工作流失败", err)
			}
			_, err := r.Repository.RescheduleReconcile(ctx, claim.Command.ID, claim.Command.Revision, time.Now().Add(r.reconcileDelay()))
			if err != nil {
				_ = r.Repository.FinishAttempt(ctx, claim.AttemptID, "failed", "PROJECT_CONTROL_RECONCILE_RESCHEDULE_FAILED", "安排取消对账失败")
				return true, err
			}
			return true, r.Repository.FinishAttempt(ctx, claim.AttemptID, "succeeded", "", "")
		}
		return true, r.cancelCommand(ctx, claim, states, "child_workflows_inactive")
	}
	for _, state := range states {
		if state.Active || !state.Terminal() {
			_, err := r.Repository.RescheduleReconcile(ctx, claim.Command.ID, claim.Command.Revision, time.Now().Add(r.reconcileDelay()))
			if err != nil {
				_ = r.Repository.FinishAttempt(ctx, claim.AttemptID, "failed", "PROJECT_CONTROL_RECONCILE_RESCHEDULE_FAILED", "安排下一次对账失败")
				return true, err
			}
			return true, r.Repository.FinishAttempt(ctx, claim.AttemptID, "succeeded", "", "")
		}
	}

	items, err := r.Repository.Items(ctx, claim.Command.ID)
	if err != nil {
		return true, r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_ITEMS_UNAVAILABLE", "读取命令执行单元失败", err)
	}
	current := claim.Command
	results := reconcileItemResults(items, states)
	if len(results) > 0 {
		current, err = r.Repository.ApplyItemResults(ctx, current.ID, current.Revision, results)
		if err != nil {
			return true, r.rescheduleFailure(ctx, claimAtCommand(claim, current), "PROJECT_CONTROL_ITEM_RECONCILE_FAILED", "保存执行单元终态失败", err)
		}
		items, err = r.Repository.Items(ctx, current.ID)
		if err != nil {
			return true, r.rescheduleFailure(ctx, claimAtCommand(claim, current), "PROJECT_CONTROL_ITEMS_UNAVAILABLE", "读取命令执行单元失败", err)
		}
		observability.RecordProjectControlCorrection("item_terminal_state")
	}
	status, output, errorCode, errorMessage := aggregateCommandTerminal(items, states)
	if !status.Terminal() {
		return true, r.rescheduleFailure(ctx, claimAtCommand(claim, current), "PROJECT_CONTROL_TERMINAL_AGGREGATION_INCOMPLETE", "子工作流终态无法完成聚合", nil)
	}
	_, err = r.Repository.Transition(ctx, TransitionCommand{
		CommandID: current.ID, ExpectedRevision: current.Revision, Status: status,
		Output: output, ErrorCode: errorCode, ErrorMessage: errorMessage,
		EventPayload: map[string]any{
			"workflowCount": len(states), "itemCount": len(items), "phase": "reconciled",
		},
	})
	if err != nil {
		_ = r.Repository.FinishAttempt(ctx, claim.AttemptID, "failed", "PROJECT_CONTROL_COMMAND_FINALIZE_FAILED", "保存命令终态失败")
		return true, err
	}
	logCommandInfo(ctx, "project control command reconciled",
		"status", status, "succeededItems", countItemsWithStatus(items, "succeeded"),
		"failedItems", countItemsWithStatus(items, "failed"), "workflowCount", len(states))
	observability.RecordProjectControlCorrection("command_terminal_state")
	return true, r.Repository.FinishAttempt(ctx, claim.AttemptID, "succeeded", "", "")
}

func (r *Reconciler) rescheduleFailure(ctx context.Context, claim *Claim, code, message string, cause error) error {
	logCommandWarning(ctx, "project control reconciliation rescheduled", "errorCode", code, "error", cause)
	if err := r.Repository.FinishAttempt(ctx, claim.AttemptID, "failed", code, message); err != nil {
		return err
	}
	_, err := r.Repository.RescheduleReconcile(ctx, claim.Command.ID, claim.Command.Revision, time.Now().Add(r.reconcileDelay()))
	if err != nil {
		if cause != nil {
			return fmt.Errorf("%s: %w; reschedule: %v", message, cause, err)
		}
		return err
	}
	return nil
}

func (r *Reconciler) failCommand(ctx context.Context, claim *Claim, code, message string) error {
	logCommandError(ctx, "project control command reconciliation failed", "errorCode", code)
	if err := r.Repository.FinishAttempt(ctx, claim.AttemptID, "failed", code, message); err != nil {
		return err
	}
	_, err := r.Repository.Transition(ctx, TransitionCommand{
		CommandID: claim.Command.ID, ExpectedRevision: claim.Command.Revision,
		Status: CommandFailed, ErrorCode: code, ErrorMessage: message,
	})
	return err
}

func (r *Reconciler) cancelCommand(ctx context.Context, claim *Claim, states []WorkflowExecutionState, phase string) error {
	current := claim.Command
	items, err := r.Repository.Items(ctx, current.ID)
	if err != nil {
		return r.rescheduleFailure(ctx, claim, "PROJECT_CONTROL_ITEMS_UNAVAILABLE", "读取待取消执行单元失败", err)
	}
	results := make([]ItemResult, 0, len(items))
	for _, item := range items {
		if itemStatusTerminal(item.Status) {
			continue
		}
		results = append(results, ItemResult{
			CommandItemID: item.ID, Status: "cancelled", Retryable: false, Output: item.Output,
		})
	}
	if len(results) > 0 {
		current, err = r.Repository.ApplyItemResults(ctx, current.ID, current.Revision, results)
		if err != nil {
			return r.rescheduleFailure(ctx, claimAtCommand(claim, current), "PROJECT_CONTROL_ITEM_CANCEL_FAILED", "保存取消执行单元失败", err)
		}
	}
	if err := r.Repository.FinishAttempt(ctx, claim.AttemptID, "cancelled", "", ""); err != nil {
		return err
	}
	_, err = r.Repository.Transition(ctx, TransitionCommand{
		CommandID: current.ID, ExpectedRevision: current.Revision, Status: CommandCancelled,
		EventPayload: map[string]any{
			"phase": phase, "workflowCount": len(states), "itemCount": len(items),
		},
	})
	if err == nil {
		logCommandInfo(ctx, "project control command cancelled", "phase", phase, "workflowCount", len(states))
	}
	return err
}

func countItemsWithStatus(items []CommandItem, status string) int {
	count := 0
	for _, item := range items {
		if item.Status == status {
			count++
		}
	}
	return count
}

func (r *Reconciler) validate() error {
	if r == nil || r.Repository == nil || r.Tracker == nil {
		return fmt.Errorf("project control reconciler dependencies are unavailable")
	}
	if strings.TrimSpace(r.Owner) == "" || strings.TrimSpace(r.ReleaseID) == "" {
		return fmt.Errorf("project control reconciler owner and release ID are required")
	}
	if r.LeaseDuration <= 0 {
		return fmt.Errorf("project control reconciler lease duration must be positive")
	}
	return nil
}

func (r *Reconciler) reconcileDelay() time.Duration {
	if r.ReconcileDelay <= 0 {
		return defaultReconcileDelay
	}
	return r.ReconcileDelay
}

func validateWorkflowStates(links []WorkflowLink, states []WorkflowExecutionState) error {
	if len(states) != len(links) {
		return fmt.Errorf("received %d workflow states for %d links", len(states), len(links))
	}
	want := make(map[string]WorkflowLink, len(links))
	for _, link := range links {
		want[link.TemporalWorkflowID] = link
	}
	seen := make(map[string]struct{}, len(states))
	for _, state := range states {
		workflowID := state.Link.TemporalWorkflowID
		if _, exists := want[workflowID]; !exists {
			return fmt.Errorf("unexpected workflow state %s", workflowID)
		}
		if _, duplicate := seen[workflowID]; duplicate {
			return fmt.Errorf("workflow state %s is duplicated", workflowID)
		}
		seen[workflowID] = struct{}{}
	}
	return nil
}

func reconcileItemResults(items []CommandItem, states []WorkflowExecutionState) []ItemResult {
	byItem := make(map[string][]WorkflowExecutionState)
	for _, state := range states {
		if state.Link.CommandItemID != "" {
			byItem[state.Link.CommandItemID] = append(byItem[state.Link.CommandItemID], state)
		}
	}
	results := make([]ItemResult, 0, len(items))
	for _, item := range items {
		if itemStatusTerminal(item.Status) {
			continue
		}
		linked := byItem[item.ID]
		if len(linked) == 0 {
			results = append(results, ItemResult{
				CommandItemID: item.ID, Status: "failed", Retryable: false, Output: item.Output,
				ErrorCode:    "PROJECT_CONTROL_ITEM_WORKFLOW_MISSING",
				ErrorMessage: "执行单元没有关联子工作流",
			})
			continue
		}
		status, code, message, retryable := aggregateWorkflowStates(linked)
		results = append(results, ItemResult{
			CommandItemID: item.ID, Status: status, Retryable: retryable,
			Output: item.Output, ErrorCode: code, ErrorMessage: message,
		})
	}
	return results
}

func aggregateWorkflowStates(states []WorkflowExecutionState) (status, code, message string, retryable bool) {
	for _, state := range states {
		if workflowStatusFailed(state.Status) {
			return "failed", firstNonEmptyString(state.ErrorCode, "CHILD_WORKFLOW_FAILED"),
				firstNonEmptyString(state.ErrorMessage, "子工作流执行失败"), true
		}
	}
	for _, state := range states {
		if workflowStatusCancelled(state.Status) {
			return "cancelled", "", "", false
		}
	}
	return "succeeded", "", "", false
}

func aggregateCommandTerminal(items []CommandItem, states []WorkflowExecutionState) (CommandStatus, json.RawMessage, string, string) {
	succeeded := 0
	failed := 0
	cancelled := 0
	skipped := 0
	firstCode := ""
	firstMessage := ""
	partialWorkflow := false
	if len(items) > 0 {
		for _, item := range items {
			switch item.Status {
			case "succeeded":
				succeeded++
			case "skipped":
				skipped++
			case "failed":
				failed++
				firstCode = firstNonEmptyString(firstCode, item.ErrorCode)
				firstMessage = firstNonEmptyString(firstMessage, item.ErrorMessage)
			case "cancelled":
				cancelled++
			default:
				return "", nil, "", ""
			}
		}
	} else {
		for _, state := range states {
			switch {
			case workflowStatusFailed(state.Status):
				failed++
				firstCode = firstNonEmptyString(firstCode, state.ErrorCode)
				firstMessage = firstNonEmptyString(firstMessage, state.ErrorMessage)
			case workflowStatusCancelled(state.Status):
				cancelled++
			case strings.EqualFold(state.Status, "partial_succeeded"):
				succeeded++
				partialWorkflow = true
			default:
				succeeded++
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"succeededItems": succeeded, "failedItems": failed,
		"cancelledItems": cancelled, "skippedItems": skipped,
		"workflowCount": len(states),
	})
	positive := succeeded + skipped
	switch {
	case failed > 0 && positive == 0:
		return CommandFailed, payload,
			firstNonEmptyString(firstCode, "CHILD_WORKFLOW_FAILED"),
			firstNonEmptyString(firstMessage, "子工作流执行失败")
	case cancelled > 0 && positive == 0:
		return CommandCancelled, payload, "", ""
	case failed > 0 || cancelled > 0 || partialWorkflow:
		return CommandPartialSucceeded, payload, "", ""
	default:
		return CommandSucceeded, payload, "", ""
	}
}

func terminalWorkflowStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "partial_succeeded", "completed", "failed", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func workflowStatusFailed(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "failed")
}

func workflowStatusCancelled(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "cancelled")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
