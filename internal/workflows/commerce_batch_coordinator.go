package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	CommerceScriptUnitBatchCoordinatorWorkflowName = "CommerceScriptUnitBatchCoordinatorWorkflow"

	StartCommerceScriptUnitBatchCoordinatorActivityName    = "StartCommerceScriptUnitBatchCoordinator"
	StartCommerceScriptUnitBatchItemActivityName           = "StartCommerceScriptUnitBatchItem"
	CompleteCommerceScriptUnitBatchItemActivityName        = "CompleteCommerceScriptUnitBatchItem"
	FinalizeCommerceScriptUnitBatchCoordinatorActivityName = "FinalizeCommerceScriptUnitBatchCoordinator"
	AbortCommerceScriptUnitBatchCoordinatorActivityName    = "AbortCommerceScriptUnitBatchCoordinator"

	commerceCoordinatorDefaultConcurrency = 4
	commerceCoordinatorMaxConcurrency     = 16
	commerceCoordinatorItemsPerExecution  = 64
)

type CommerceScriptUnitBatchChild struct {
	CoordinatorItemID  string          `json:"coordinatorItemId"`
	ScriptUnitID       string          `json:"scriptUnitId"`
	UnitGenerationID   string          `json:"unitGenerationId"`
	WorkflowRunID      string          `json:"workflowRunId"`
	TemporalWorkflowID string          `json:"temporalWorkflowId"`
	WorkflowName       string          `json:"workflowName"`
	WorkflowInput      json.RawMessage `json:"workflowInput"`
	ProductionRunID    string          `json:"productionRunId,omitempty"`
}

type CommerceScriptUnitBatchCoordinatorInput struct {
	CoordinatorID       string                         `json:"coordinatorId"`
	WorkflowRunID       string                         `json:"workflowRunId"`
	OrganizationID      string                         `json:"organizationId"`
	ProjectID           string                         `json:"projectId"`
	ProjectGenerationID string                         `json:"projectGenerationId"`
	TargetStage         string                         `json:"targetStage"`
	MaxConcurrency      int                            `json:"maxConcurrency"`
	RequestedBy         string                         `json:"requestedBy"`
	Children            []CommerceScriptUnitBatchChild `json:"children"`
	Cursor              int                            `json:"cursor,omitempty"`
	ExecutionStart      int                            `json:"executionStart,omitempty"`
}

type CommerceScriptUnitBatchCoordinatorOutput struct {
	CoordinatorID string `json:"coordinatorId"`
	WorkflowRunID string `json:"workflowRunId"`
	TargetStage   string `json:"targetStage"`
	Status        string `json:"status"`
	Total         int    `json:"total"`
	Succeeded     int    `json:"succeeded"`
	Failed        int    `json:"failed"`
	Cancelled     int    `json:"cancelled"`
}

type CommerceScriptUnitBatchItemStart struct {
	CoordinatorID       string `json:"coordinatorId"`
	CoordinatorItemID   string `json:"coordinatorItemId"`
	WorkflowRunID       string `json:"workflowRunId"`
	ChildWorkflowRunID  string `json:"childWorkflowRunId"`
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	ProjectGenerationID string `json:"projectGenerationId"`
}

type CommerceScriptUnitBatchItemCompletion struct {
	CoordinatorID      string          `json:"coordinatorId"`
	CoordinatorItemID  string          `json:"coordinatorItemId"`
	WorkflowRunID      string          `json:"workflowRunId"`
	ChildWorkflowRunID string          `json:"childWorkflowRunId"`
	Status             string          `json:"status"`
	Output             json.RawMessage `json:"output"`
	ErrorCode          string          `json:"errorCode,omitempty"`
	ErrorMessage       string          `json:"errorMessage,omitempty"`
}

type CommerceScriptUnitBatchAbort struct {
	CoordinatorID string `json:"coordinatorId"`
	WorkflowRunID string `json:"workflowRunId"`
	Cancelled     bool   `json:"cancelled"`
	ErrorCode     string `json:"errorCode"`
	ErrorMessage  string `json:"errorMessage"`
}

type CommerceScriptUnitBatchCoordinatorPort interface {
	StartCommerceScriptUnitBatchCoordinator(context.Context, CommerceScriptUnitBatchCoordinatorInput) error
	StartCommerceScriptUnitBatchItem(context.Context, CommerceScriptUnitBatchItemStart) error
	CompleteCommerceScriptUnitBatchItem(context.Context, CommerceScriptUnitBatchItemCompletion) error
	FinalizeCommerceScriptUnitBatchCoordinator(context.Context, CommerceScriptUnitBatchCoordinatorInput) (CommerceScriptUnitBatchCoordinatorOutput, error)
	AbortCommerceScriptUnitBatchCoordinator(context.Context, CommerceScriptUnitBatchAbort) error
}

func RegisterCommerceScriptUnitBatchCoordinatorWorkflow(registrar CommerceWorkflowRegistrar) {
	registrar.RegisterWorkflowWithOptions(CommerceScriptUnitBatchCoordinatorWorkflow, workflow.RegisterOptions{Name: CommerceScriptUnitBatchCoordinatorWorkflowName})
}

func RegisterCommerceScriptUnitBatchCoordinatorActivities(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	registrar.RegisterActivityWithOptions(activities.StartCommerceScriptUnitBatchCoordinator, activity.RegisterOptions{Name: StartCommerceScriptUnitBatchCoordinatorActivityName})
	registrar.RegisterActivityWithOptions(activities.StartCommerceScriptUnitBatchItem, activity.RegisterOptions{Name: StartCommerceScriptUnitBatchItemActivityName})
	registrar.RegisterActivityWithOptions(activities.CompleteCommerceScriptUnitBatchItem, activity.RegisterOptions{Name: CompleteCommerceScriptUnitBatchItemActivityName})
	registrar.RegisterActivityWithOptions(activities.FinalizeCommerceScriptUnitBatchCoordinator, activity.RegisterOptions{Name: FinalizeCommerceScriptUnitBatchCoordinatorActivityName})
	registrar.RegisterActivityWithOptions(activities.AbortCommerceScriptUnitBatchCoordinator, activity.RegisterOptions{Name: AbortCommerceScriptUnitBatchCoordinatorActivityName})
}

func CommerceScriptUnitBatchCoordinatorWorkflow(
	ctx workflow.Context,
	input CommerceScriptUnitBatchCoordinatorInput,
) (result CommerceScriptUnitBatchCoordinatorOutput, resultErr error) {
	if err := validateCommerceScriptUnitBatchCoordinatorInput(input); err != nil {
		return result, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	continuingAsNew := false
	finished := false
	defer func() {
		if resultErr == nil || finished || continuingAsNew {
			return
		}
		code, message := commerceBatchChildError(resultErr)
		cancelled := temporal.IsCanceledError(resultErr)
		if cancelled {
			code = "USER_CANCELLED"
		}
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		disconnected = workflow.WithActivityOptions(disconnected, defaultActivityOptions())
		_ = workflow.ExecuteActivity(disconnected, AbortCommerceScriptUnitBatchCoordinatorActivityName, CommerceScriptUnitBatchAbort{
			CoordinatorID: input.CoordinatorID, WorkflowRunID: input.WorkflowRunID,
			Cancelled: cancelled, ErrorCode: code, ErrorMessage: message,
		}).Get(disconnected, nil)
	}()

	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	if err := workflow.ExecuteActivity(activityCtx, StartCommerceScriptUnitBatchCoordinatorActivityName, input).Get(activityCtx, nil); err != nil {
		return result, err
	}
	concurrency := input.MaxConcurrency
	if concurrency < 1 {
		concurrency = commerceCoordinatorDefaultConcurrency
	}
	if concurrency > commerceCoordinatorMaxConcurrency {
		concurrency = commerceCoordinatorMaxConcurrency
	}

	for batchStart := input.Cursor; batchStart < len(input.Children); batchStart += concurrency {
		batchEnd := batchStart + concurrency
		if batchEnd > len(input.Children) {
			batchEnd = len(input.Children)
		}
		futures := make([]workflow.ChildWorkflowFuture, 0, batchEnd-batchStart)
		children := input.Children[batchStart:batchEnd]
		for _, child := range children {
			if err := workflow.ExecuteActivity(activityCtx, StartCommerceScriptUnitBatchItemActivityName, CommerceScriptUnitBatchItemStart{
				CoordinatorID: input.CoordinatorID, CoordinatorItemID: child.CoordinatorItemID,
				WorkflowRunID: input.WorkflowRunID, ChildWorkflowRunID: child.WorkflowRunID,
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
				ProjectGenerationID: input.ProjectGenerationID,
			}).Get(activityCtx, nil); err != nil {
				return result, err
			}
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:          child.TemporalWorkflowID,
				ParentClosePolicy:   enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
				WaitForCancellation: true,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
			})
			future, err := executeCommerceCoordinatorChild(childCtx, child)
			if err != nil {
				return result, err
			}
			futures = append(futures, future)
		}
		for index, future := range futures {
			child := children[index]
			output, status, code, message, err := readCommerceCoordinatorChildResult(ctx, future, child.WorkflowName)
			if err != nil && temporal.IsCanceledError(err) {
				return result, err
			}
			if err != nil {
				status = "failed"
				code, message = commerceBatchChildError(err)
			}
			if err := workflow.ExecuteActivity(activityCtx, CompleteCommerceScriptUnitBatchItemActivityName, CommerceScriptUnitBatchItemCompletion{
				CoordinatorID: input.CoordinatorID, CoordinatorItemID: child.CoordinatorItemID,
				WorkflowRunID: input.WorkflowRunID, ChildWorkflowRunID: child.WorkflowRunID,
				Status: status, Output: output, ErrorCode: code, ErrorMessage: message,
			}).Get(activityCtx, nil); err != nil {
				return result, err
			}
		}
		nextCursor := batchEnd
		if nextCursor < len(input.Children) && (workflow.GetInfo(ctx).GetContinueAsNewSuggested() || nextCursor-input.ExecutionStart >= commerceCoordinatorItemsPerExecution) {
			input.Cursor = nextCursor
			input.ExecutionStart = nextCursor
			continuingAsNew = true
			return result, workflow.NewContinueAsNewError(ctx, CommerceScriptUnitBatchCoordinatorWorkflow, input)
		}
	}

	if err := workflow.ExecuteActivity(activityCtx, FinalizeCommerceScriptUnitBatchCoordinatorActivityName, input).Get(activityCtx, &result); err != nil {
		return result, err
	}
	finished = true
	return result, nil
}

func executeCommerceCoordinatorChild(ctx workflow.Context, child CommerceScriptUnitBatchChild) (workflow.ChildWorkflowFuture, error) {
	switch child.WorkflowName {
	case CommerceStoryboardPlanningWorkflowName:
		var input CommerceStoryboardPlanningInput
		if err := json.Unmarshal(child.WorkflowInput, &input); err != nil {
			return nil, fmt.Errorf("decode storyboard child input: %w", err)
		}
		return workflow.ExecuteChildWorkflow(ctx, CommerceStoryboardPlanningWorkflowName, input), nil
	case CommerceReferenceImageBatchWorkflowName:
		var input CommerceReferenceImageBatchInput
		if err := json.Unmarshal(child.WorkflowInput, &input); err != nil {
			return nil, fmt.Errorf("decode reference image child input: %w", err)
		}
		return workflow.ExecuteChildWorkflow(ctx, CommerceReferenceImageBatchWorkflowName, input), nil
	case CommerceVideoPromptBatchWorkflowName:
		var input CommerceVideoBatchInput
		if err := json.Unmarshal(child.WorkflowInput, &input); err != nil {
			return nil, fmt.Errorf("decode video prompt child input: %w", err)
		}
		return workflow.ExecuteChildWorkflow(ctx, CommerceVideoPromptBatchWorkflowName, input), nil
	case CommerceShotVideoBatchWorkflowName:
		var input CommerceVideoBatchInput
		if err := json.Unmarshal(child.WorkflowInput, &input); err != nil {
			return nil, fmt.Errorf("decode shot video child input: %w", err)
		}
		return workflow.ExecuteChildWorkflow(ctx, CommerceShotVideoBatchWorkflowName, input), nil
	case CommerceFinalComposeWorkflowName:
		var input CommerceFinalComposeInput
		if err := json.Unmarshal(child.WorkflowInput, &input); err != nil {
			return nil, fmt.Errorf("decode final compose child input: %w", err)
		}
		return workflow.ExecuteChildWorkflow(ctx, CommerceFinalComposeWorkflowName, input), nil
	default:
		return nil, fmt.Errorf("unsupported commerce coordinator child workflow %q", child.WorkflowName)
	}
}

func readCommerceCoordinatorChildResult(ctx workflow.Context, future workflow.ChildWorkflowFuture, workflowName string) (json.RawMessage, string, string, string, error) {
	var value any
	switch workflowName {
	case CommerceStoryboardPlanningWorkflowName:
		value = &CommerceStoryboardPlanningOutput{}
	case CommerceReferenceImageBatchWorkflowName:
		value = &CommerceReferenceImageBatchOutput{}
	case CommerceVideoPromptBatchWorkflowName, CommerceShotVideoBatchWorkflowName:
		value = &CommerceVideoBatchOutput{}
	case CommerceFinalComposeWorkflowName:
		value = &CommerceFinalComposeOutput{}
	default:
		return nil, "failed", "COMMERCE_BATCH_CHILD_INVALID", "批量子工作流类型无效", nil
	}
	if err := future.Get(ctx, value); err != nil {
		return nil, "failed", "", "", err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "failed", "COMMERCE_BATCH_CHILD_OUTPUT_INVALID", err.Error(), nil
	}
	status := "succeeded"
	var envelope struct {
		Status commerce.ProductionRunStatus `json:"status"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && strings.TrimSpace(string(envelope.Status)) != "" {
		switch envelope.Status {
		case commerce.RunSucceeded:
			status = "succeeded"
		case commerce.RunCancelled:
			status = "cancelled"
		default:
			status = "failed"
			return raw, status, "COMMERCE_BATCH_CHILD_INCOMPLETE", "脚本单元任务未完整成功", nil
		}
	}
	return raw, status, "", "", nil
}

func validateCommerceScriptUnitBatchCoordinatorInput(input CommerceScriptUnitBatchCoordinatorInput) error {
	if strings.TrimSpace(input.CoordinatorID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" ||
		strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.ProjectGenerationID) == "" || strings.TrimSpace(input.TargetStage) == "" ||
		strings.TrimSpace(input.RequestedBy) == "" || len(input.Children) == 0 {
		return errors.New("commerce batch coordinator identity is incomplete")
	}
	if input.Cursor < 0 || input.Cursor > len(input.Children) || input.ExecutionStart < 0 || input.ExecutionStart > input.Cursor {
		return errors.New("commerce batch coordinator cursor is invalid")
	}
	seenItems := make(map[string]struct{}, len(input.Children))
	seenUnits := make(map[string]struct{}, len(input.Children))
	for _, child := range input.Children {
		if strings.TrimSpace(child.CoordinatorItemID) == "" || strings.TrimSpace(child.ScriptUnitID) == "" ||
			strings.TrimSpace(child.UnitGenerationID) == "" || strings.TrimSpace(child.WorkflowRunID) == "" ||
			strings.TrimSpace(child.TemporalWorkflowID) == "" || strings.TrimSpace(child.WorkflowName) == "" || len(child.WorkflowInput) == 0 {
			return errors.New("commerce batch child identity is incomplete")
		}
		if _, exists := seenItems[child.CoordinatorItemID]; exists {
			return errors.New("commerce batch contains duplicate item")
		}
		if _, exists := seenUnits[child.ScriptUnitID]; exists {
			return errors.New("commerce batch contains duplicate script unit")
		}
		seenItems[child.CoordinatorItemID] = struct{}{}
		seenUnits[child.ScriptUnitID] = struct{}{}
	}
	return nil
}

func commerceBatchChildError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && strings.TrimSpace(appErr.Type()) != "" {
		return appErr.Type(), appErr.Error()
	}
	return "COMMERCE_BATCH_CHILD_FAILED", err.Error()
}

func (a CommerceActivities) batchCoordinatorPort() (CommerceScriptUnitBatchCoordinatorPort, bool) {
	port, ok := a.Ports.(CommerceScriptUnitBatchCoordinatorPort)
	return port, ok
}

func (a CommerceActivities) StartCommerceScriptUnitBatchCoordinator(ctx context.Context, input CommerceScriptUnitBatchCoordinatorInput) error {
	port, ok := a.batchCoordinatorPort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.StartCommerceScriptUnitBatchCoordinator(ctx, input)
}

func (a CommerceActivities) StartCommerceScriptUnitBatchItem(ctx context.Context, input CommerceScriptUnitBatchItemStart) error {
	port, ok := a.batchCoordinatorPort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.StartCommerceScriptUnitBatchItem(ctx, input)
}

func (a CommerceActivities) CompleteCommerceScriptUnitBatchItem(ctx context.Context, input CommerceScriptUnitBatchItemCompletion) error {
	port, ok := a.batchCoordinatorPort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.CompleteCommerceScriptUnitBatchItem(ctx, input)
}

func (a CommerceActivities) FinalizeCommerceScriptUnitBatchCoordinator(ctx context.Context, input CommerceScriptUnitBatchCoordinatorInput) (CommerceScriptUnitBatchCoordinatorOutput, error) {
	port, ok := a.batchCoordinatorPort()
	if !ok {
		return CommerceScriptUnitBatchCoordinatorOutput{}, commerceActivityPortError()
	}
	return port.FinalizeCommerceScriptUnitBatchCoordinator(ctx, input)
}

func (a CommerceActivities) AbortCommerceScriptUnitBatchCoordinator(ctx context.Context, input CommerceScriptUnitBatchAbort) error {
	port, ok := a.batchCoordinatorPort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.AbortCommerceScriptUnitBatchCoordinator(ctx, input)
}
