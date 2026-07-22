package workflows

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestAssetBatchCompletionEventContextSatisfiesCatalog(t *testing.T) {
	batch := AssetBatchWorkflowInput{
		WorkflowRunID:     "workflow-run",
		Operation:         AssetBatchOperationGeneratePrompts,
		AttemptGeneration: 2,
	}
	item := AssetBatchItemSnapshot{AssetID: "asset-1"}
	payload := mustJSON(assetBatchEventContext(batch, item, "node-run"))

	for _, eventName := range []string{"asset.batch.prompt.completed", "asset.batch.image.completed"} {
		if _, err := events.Validate(eventName, payload); err != nil {
			t.Fatalf("validate %s payload: %v", eventName, err)
		}
	}
}

func TestBatchGenerateAssetCardsWorkflowUsesBoundedChildConcurrency(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	active := 0
	maxActive := 0

	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
		active++
		if active > maxActive {
			maxActive = active
		}
		if err := workflow.Sleep(ctx, time.Minute); err != nil {
			return AssetBatchItemOutput{}, err
		}
		active--
		return AssetBatchItemOutput{AssetID: input.Item.AssetID, Status: "succeeded"}, nil
	}, workflow.RegisterOptions{Name: "GenerateAssetCardItemWorkflow"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ AssetBatchWorkflowInput, requested AssetBatchWorkflowOutput) (AssetBatchWorkflowOutput, error) {
		requested.Status = "succeeded"
		requested.CompletedItems = len(requested.Items)
		return requested, nil
	}, activity.RegisterOptions{Name: "CompleteAssetBatchWorkflow"})

	env.ExecuteWorkflow(BatchGenerateAssetCardsWorkflow, assetBatchTestInput(5, 2))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if maxActive != 2 {
		t.Fatalf("max concurrent child workflows = %d, want 2", maxActive)
	}
}

func TestBatchGenerateAssetImagesWorkflowReturnsPartialSucceeded(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterWorkflowWithOptions(func(_ workflow.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
		if input.Item.AssetID == "asset-2" {
			return AssetBatchItemOutput{}, errors.New("provider rejected image")
		}
		return AssetBatchItemOutput{AssetID: input.Item.AssetID, Status: "succeeded"}, nil
	}, workflow.RegisterOptions{Name: "GenerateCanonicalAssetImageItemWorkflow"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ AssetBatchWorkflowInput, requested AssetBatchWorkflowOutput) (AssetBatchWorkflowOutput, error) {
		requested.CompletedItems = 0
		requested.FailedItems = 0
		for _, item := range requested.Items {
			if item.Status == "failed" {
				requested.FailedItems++
			} else if item.Status == "succeeded" || item.Status == "skipped" {
				requested.CompletedItems++
			}
		}
		switch {
		case requested.FailedItems == 0:
			requested.Status = "succeeded"
		case requested.CompletedItems == 0:
			requested.Status = "failed"
		default:
			requested.Status = "partial_succeeded"
		}
		return requested, nil
	}, activity.RegisterOptions{Name: "CompleteAssetBatchWorkflow"})

	env.ExecuteWorkflow(BatchGenerateCanonicalAssetImagesWorkflow, assetBatchTestInput(3, 2))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output AssetBatchWorkflowOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if output.Status != "partial_succeeded" || output.CompletedItems != 2 || output.FailedItems != 1 {
		t.Fatalf("output = %+v, want partial_succeeded with 2 completed and 1 failed", output)
	}
	if len(output.Items) != 3 || output.Items[1].AssetID != "asset-2" || output.Items[1].Status != "failed" {
		t.Fatalf("item results = %+v", output.Items)
	}
}

func TestAssetBatchHundredItemsContinuesAsNewIsolatesFailuresAndRetriesFailedOnly(t *testing.T) {
	const (
		itemCount      = 100
		maxConcurrency = 5
	)
	input := assetBatchTestInput(itemCount, maxConcurrency)
	rejected := make(map[string]bool, itemCount/10)
	for index := 10; index <= itemCount; index += 10 {
		rejected[fmt.Sprintf("asset-%d", index)] = true
	}
	calls := make(map[string]int, itemCount)
	active := 0
	maxActive := 0
	register := func(env *testsuite.TestWorkflowEnvironment) {
		env.RegisterWorkflowWithOptions(func(ctx workflow.Context, activityInput AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
			assetID := activityInput.Item.AssetID
			calls[assetID]++
			active++
			if active > maxActive {
				maxActive = active
			}
			if err := workflow.Sleep(ctx, time.Minute); err != nil {
				active--
				return AssetBatchItemOutput{}, err
			}
			active--
			if activityInput.Batch.AttemptGeneration == 1 && rejected[assetID] {
				return AssetBatchItemOutput{}, temporal.NewNonRetryableApplicationError(
					"provider rejected image", "PROVIDER_REJECTED", nil,
				)
			}
			return AssetBatchItemOutput{AssetID: assetID, Status: "succeeded"}, nil
		}, workflow.RegisterOptions{Name: "GenerateCanonicalAssetImageItemWorkflow"})
		env.RegisterActivityWithOptions(func(_ context.Context, _ AssetBatchWorkflowInput, requested AssetBatchWorkflowOutput) (AssetBatchWorkflowOutput, error) {
			requested.CompletedItems = 0
			requested.FailedItems = 0
			for _, item := range requested.Items {
				switch item.Status {
				case "succeeded", "skipped":
					requested.CompletedItems++
				case "failed":
					requested.FailedItems++
				}
			}
			switch {
			case requested.FailedItems == 0:
				requested.Status = "succeeded"
			case requested.CompletedItems == 0:
				requested.Status = "failed"
			default:
				requested.Status = "partial_succeeded"
			}
			return requested, nil
		}, activity.RegisterOptions{Name: "CompleteAssetBatchWorkflow"})
	}

	var suite testsuite.WorkflowTestSuite
	firstRun := suite.NewTestWorkflowEnvironment()
	register(firstRun)
	firstRun.ExecuteWorkflow(BatchGenerateCanonicalAssetImagesWorkflow, input)
	if !firstRun.IsWorkflowCompleted() || !workflow.IsContinueAsNewError(firstRun.GetWorkflowError()) {
		t.Fatalf("first run error = %v, want ContinueAsNew", firstRun.GetWorkflowError())
	}
	var continueErr *workflow.ContinueAsNewError
	if !errors.As(firstRun.GetWorkflowError(), &continueErr) {
		t.Fatalf("first run error type = %T", firstRun.GetWorkflowError())
	}
	var continued AssetBatchWorkflowInput
	if err := converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &continued); err != nil {
		t.Fatalf("decode asset batch checkpoint: %v", err)
	}
	if continued.NextIndex != assetBatchItemsPerRun || len(continued.Results) != assetBatchItemsPerRun {
		t.Fatalf("checkpoint next=%d results=%d, want %d", continued.NextIndex, len(continued.Results), assetBatchItemsPerRun)
	}

	secondRun := suite.NewTestWorkflowEnvironment()
	register(secondRun)
	secondRun.ExecuteWorkflow(BatchGenerateCanonicalAssetImagesWorkflow, continued)
	if !secondRun.IsWorkflowCompleted() || secondRun.GetWorkflowError() != nil {
		t.Fatalf("second run completed=%v error=%v", secondRun.IsWorkflowCompleted(), secondRun.GetWorkflowError())
	}
	var output AssetBatchWorkflowOutput
	if err := secondRun.GetWorkflowResult(&output); err != nil {
		t.Fatalf("second run result: %v", err)
	}
	if output.Status != "partial_succeeded" || output.CompletedItems != 90 || output.FailedItems != 10 || len(output.Items) != itemCount {
		t.Fatalf("long batch output = %+v", output)
	}
	if maxActive != maxConcurrency {
		t.Fatalf("max active children = %d, want %d", maxActive, maxConcurrency)
	}

	retryInput := input
	retryInput.WorkflowRunID = "workflow-run-retry"
	retryInput.AttemptGeneration = 2
	retryInput.NextIndex = 0
	retryInput.Results = nil
	retryInput.Items = nil
	for _, item := range input.Items {
		if rejected[item.AssetID] {
			retryInput.Items = append(retryInput.Items, item)
		}
	}
	retryRun := suite.NewTestWorkflowEnvironment()
	register(retryRun)
	retryRun.ExecuteWorkflow(BatchGenerateCanonicalAssetImagesWorkflow, retryInput)
	if !retryRun.IsWorkflowCompleted() || retryRun.GetWorkflowError() != nil {
		t.Fatalf("retry run completed=%v error=%v", retryRun.IsWorkflowCompleted(), retryRun.GetWorkflowError())
	}
	var retryOutput AssetBatchWorkflowOutput
	if err := retryRun.GetWorkflowResult(&retryOutput); err != nil {
		t.Fatalf("retry result: %v", err)
	}
	if retryOutput.Status != "succeeded" || retryOutput.CompletedItems != len(rejected) || retryOutput.FailedItems != 0 {
		t.Fatalf("retry output = %+v", retryOutput)
	}
	for _, item := range input.Items {
		wantCalls := 1
		if rejected[item.AssetID] {
			wantCalls = 2
		}
		if calls[item.AssetID] != wantCalls {
			t.Fatalf("asset %s calls=%d, want %d", item.AssetID, calls[item.AssetID], wantCalls)
		}
	}
}

func TestAssetBatchCancellationDrainsAllStartedChildrenBeforeFinalizing(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	const childCount = 3
	cancelledChildren := 0
	finalizerObserved := -1

	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
		if err := workflow.Sleep(ctx, time.Hour); err != nil {
			cleanup, _ := workflow.NewDisconnectedContext(ctx)
			cleanupDelay := 20 * time.Second
			if input.Item.AssetID == "asset-1" {
				cleanupDelay = time.Second
			} else if input.Item.AssetID == "asset-2" {
				cleanupDelay = 10 * time.Second
			}
			if cleanupErr := workflow.Sleep(cleanup, cleanupDelay); cleanupErr != nil {
				return AssetBatchItemOutput{}, cleanupErr
			}
			cancelledChildren++
			return AssetBatchItemOutput{AssetID: input.Item.AssetID, Status: "cancelled"}, err
		}
		return AssetBatchItemOutput{AssetID: input.Item.AssetID, Status: "succeeded"}, nil
	}, workflow.RegisterOptions{Name: "GenerateAssetCardItemWorkflow"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ AssetBatchWorkflowInput, requested AssetBatchWorkflowOutput) (AssetBatchWorkflowOutput, error) {
		finalizerObserved = cancelledChildren
		requested.Status = "cancelled"
		return requested, nil
	}, activity.RegisterOptions{Name: "CompleteAssetBatchWorkflow"})
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)

	env.ExecuteWorkflow(BatchGenerateAssetCardsWorkflow, assetBatchTestInput(childCount, childCount))

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if cancelledChildren != childCount {
		t.Fatalf("cancelled child cleanups=%d, want %d", cancelledChildren, childCount)
	}
	if finalizerObserved != childCount {
		t.Fatalf("finalizer observed %d child cleanups, want %d", finalizerObserved, childCount)
	}
}

func TestRecoveredAssetBatchImageRequiresMatchingProviderAndPrompt(t *testing.T) {
	item := AssetBatchItemSnapshot{RecoveredImage: &AssetBatchRecoveredImageSnapshot{
		ProviderCallID: "call-1", ProviderModelID: "model-1", PromptHash: "sha256:prompt-1",
		ArtifactID: "artifact-1", MediaFileID: "media-1", StorageKey: "images/result.png",
	}}
	response, ok := recoveredAssetBatchImage(item, "model-1", "sha256:prompt-1")
	if !ok || response.ProviderCallID != "call-1" || response.Output.ArtifactID != "artifact-1" {
		t.Fatalf("recovered response=%+v ok=%v", response, ok)
	}
	if _, ok := recoveredAssetBatchImage(item, "model-2", "sha256:prompt-1"); ok {
		t.Fatal("recovered image accepted a different provider model")
	}
	if _, ok := recoveredAssetBatchImage(item, "model-1", "sha256:prompt-2"); ok {
		t.Fatal("recovered image accepted a different prompt hash")
	}
}

func TestAssetBatchOutputCannotSucceedWithActiveItems(t *testing.T) {
	output := AssetBatchWorkflowOutput{
		TotalItems:     12,
		CompletedItems: 11,
		ActiveItems:    1,
	}
	classifyAssetBatchOutput(&output)
	if output.Status != "running" {
		t.Fatalf("status = %q, want running while one item is active", output.Status)
	}
}

func assetBatchTestInput(itemCount, maxConcurrency int) AssetBatchWorkflowInput {
	items := make([]AssetBatchItemSnapshot, 0, itemCount)
	for index := 1; index <= itemCount; index++ {
		items = append(items, AssetBatchItemSnapshot{AssetID: fmt.Sprintf("asset-%d", index)})
	}
	return AssetBatchWorkflowInput{
		OrganizationID: "organization",
		ProjectID:      "project",
		WorkflowRunID:  "workflow-run",
		CreatedBy:      "user",
		Operation:      AssetBatchOperationGeneratePrompts,
		MaxConcurrency: maxConcurrency,
		Items:          items,
	}
}
