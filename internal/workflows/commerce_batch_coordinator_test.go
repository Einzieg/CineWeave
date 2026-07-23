package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestCommerceScriptUnitBatchCoordinatorKeepsPartialSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	children := []CommerceScriptUnitBatchChild{
		testCommerceCoordinatorVideoChild(t, "item-1", "unit-1", "run-1", "temporal-1"),
		testCommerceCoordinatorVideoChild(t, "item-2", "unit-2", "run-fail", "temporal-2"),
		testCommerceCoordinatorVideoChild(t, "item-3", "unit-3", "run-3", "temporal-3"),
	}
	input := CommerceScriptUnitBatchCoordinatorInput{
		CoordinatorID: "coordinator", WorkflowRunID: "parent-run",
		OrganizationID: "organization", ProjectID: "project", ProjectGenerationID: "generation",
		TargetStage: "shot_videos", MaxConcurrency: 2, RequestedBy: "user", Children: children,
	}
	env.RegisterWorkflowWithOptions(func(_ workflow.Context, child CommerceVideoBatchInput) (CommerceVideoBatchOutput, error) {
		if child.ProductionRunID == "run-fail" {
			return CommerceVideoBatchOutput{}, temporal.NewNonRetryableApplicationError("provider rejected the unit", "UPSTREAM_REJECTED", errors.New("rejected"))
		}
		return CommerceVideoBatchOutput{ProductionRunID: child.ProductionRunID, Status: commerce.RunSucceeded}, nil
	}, workflow.RegisterOptions{Name: CommerceShotVideoBatchWorkflowName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchCoordinatorInput) error { return nil }, activity.RegisterOptions{Name: StartCommerceScriptUnitBatchCoordinatorActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchItemStart) error { return nil }, activity.RegisterOptions{Name: StartCommerceScriptUnitBatchItemActivityName})

	var mu sync.Mutex
	statuses := make(map[string]string)
	env.RegisterActivityWithOptions(func(_ context.Context, completion CommerceScriptUnitBatchItemCompletion) error {
		mu.Lock()
		defer mu.Unlock()
		statuses[completion.CoordinatorItemID] = completion.Status
		return nil
	}, activity.RegisterOptions{Name: CompleteCommerceScriptUnitBatchItemActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, current CommerceScriptUnitBatchCoordinatorInput) (CommerceScriptUnitBatchCoordinatorOutput, error) {
		mu.Lock()
		defer mu.Unlock()
		output := CommerceScriptUnitBatchCoordinatorOutput{
			CoordinatorID: current.CoordinatorID, WorkflowRunID: current.WorkflowRunID,
			TargetStage: current.TargetStage, Total: len(current.Children),
		}
		for _, status := range statuses {
			switch status {
			case "succeeded":
				output.Succeeded++
			case "failed":
				output.Failed++
			case "cancelled":
				output.Cancelled++
			}
		}
		if output.Failed > 0 && output.Succeeded > 0 {
			output.Status = "partially_succeeded"
		}
		return output, nil
	}, activity.RegisterOptions{Name: FinalizeCommerceScriptUnitBatchCoordinatorActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchAbort) error { return nil }, activity.RegisterOptions{Name: AbortCommerceScriptUnitBatchCoordinatorActivityName})

	env.ExecuteWorkflow(CommerceScriptUnitBatchCoordinatorWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var output CommerceScriptUnitBatchCoordinatorOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, "partially_succeeded", output.Status)
	require.Equal(t, 2, output.Succeeded)
	require.Equal(t, 1, output.Failed)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "succeeded", statuses["item-1"])
	require.Equal(t, "failed", statuses["item-2"])
	require.Equal(t, "succeeded", statuses["item-3"])
}

func TestCommerceScriptUnitBatchCoordinatorCancellationFinalizesOutstandingItems(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	input := CommerceScriptUnitBatchCoordinatorInput{
		CoordinatorID: "coordinator", WorkflowRunID: "parent-run",
		OrganizationID: "organization", ProjectID: "project", ProjectGenerationID: "generation",
		TargetStage: "shot_videos", MaxConcurrency: 1, RequestedBy: "user",
		Children: []CommerceScriptUnitBatchChild{testCommerceCoordinatorVideoChild(t, "item-1", "unit-1", "run-1", "temporal-1")},
	}
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, child CommerceVideoBatchInput) (CommerceVideoBatchOutput, error) {
		if err := workflow.Sleep(ctx, time.Hour); err != nil {
			return CommerceVideoBatchOutput{}, err
		}
		return CommerceVideoBatchOutput{ProductionRunID: child.ProductionRunID, Status: commerce.RunSucceeded}, nil
	}, workflow.RegisterOptions{Name: CommerceShotVideoBatchWorkflowName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchCoordinatorInput) error { return nil }, activity.RegisterOptions{Name: StartCommerceScriptUnitBatchCoordinatorActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchItemStart) error { return nil }, activity.RegisterOptions{Name: StartCommerceScriptUnitBatchItemActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchItemCompletion) error { return nil }, activity.RegisterOptions{Name: CompleteCommerceScriptUnitBatchItemActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CommerceScriptUnitBatchCoordinatorInput) (CommerceScriptUnitBatchCoordinatorOutput, error) {
		return CommerceScriptUnitBatchCoordinatorOutput{}, nil
	}, activity.RegisterOptions{Name: FinalizeCommerceScriptUnitBatchCoordinatorActivityName})
	aborted := false
	env.RegisterActivityWithOptions(func(_ context.Context, input CommerceScriptUnitBatchAbort) error {
		aborted = input.Cancelled
		return nil
	}, activity.RegisterOptions{Name: AbortCommerceScriptUnitBatchCoordinatorActivityName})
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)

	env.ExecuteWorkflow(CommerceScriptUnitBatchCoordinatorWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.True(t, aborted)
}

func testCommerceCoordinatorVideoChild(t *testing.T, itemID, unitID, productionRunID, temporalWorkflowID string) CommerceScriptUnitBatchChild {
	t.Helper()
	input := CommerceVideoBatchInput{
		WorkflowRunID: "workflow-" + itemID, ProductionRunID: productionRunID,
		Operation: "generate_videos", ShotIDs: []string{"shot-" + itemID},
	}
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	return CommerceScriptUnitBatchChild{
		CoordinatorItemID: itemID, ScriptUnitID: unitID, UnitGenerationID: "generation-" + unitID,
		WorkflowRunID: input.WorkflowRunID, TemporalWorkflowID: temporalWorkflowID,
		WorkflowName: CommerceShotVideoBatchWorkflowName, WorkflowInput: raw, ProductionRunID: productionRunID,
	}
}
