package workflows

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestBatchGenerateDerivedAssetImagesWorkflowUsesDurableV2Activities(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	items := []DerivedAssetBatchWorkItem{
		{ExecutionItemID: "execution-1", RequestItemID: "request-1", BatchID: "batch-1", InputOrdinal: 1, RequirementID: "requirement-1", NodeRunID: "node-1", NodeKey: "derived-asset:1", AttemptNo: 1, Status: "queued"},
		{ExecutionItemID: "execution-2", RequestItemID: "request-2", BatchID: "batch-1", InputOrdinal: 2, RequirementID: "requirement-2", NodeRunID: "node-2", NodeKey: "derived-asset:2", AttemptNo: 1, Status: "queued"},
	}
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, string) ([]DerivedAssetBatchWorkItem, error) {
		return items, nil
	}, activity.RegisterOptions{Name: "LoadDerivedAssetExecutionItems"})
	env.RegisterActivityWithOptions(func(_ context.Context, input TextToStoryboardInput, item DerivedAssetBatchWorkItem, owner string) (DerivedAssetExecutionLease, error) {
		return DerivedAssetExecutionLease{
			DerivedAssetBatchWorkItem: item,
			OrganizationID:            input.OrganizationID,
			ProjectID:                 input.ProjectID,
			WorkflowRunID:             input.WorkflowRunID,
			LeaseOwner:                owner,
			LeaseToken:                "lease-" + item.ExecutionItemID,
			Execution: NodeExecution{
				NodeRunID: item.NodeRunID, ExecutionToken: "token-" + item.ExecutionItemID, AttemptGeneration: 1,
			},
		}, nil
	}, activity.RegisterOptions{Name: "ClaimDerivedAssetExecution"})

	var mu sync.Mutex
	providerCalls := make([]string, 0, len(items))
	failedItems := make([]string, 0, 1)
	env.RegisterActivityWithOptions(func(_ context.Context, input DerivedAssetProviderExecutionInput) (DerivedAssetProviderExecutionOutput, error) {
		mu.Lock()
		providerCalls = append(providerCalls, input.Lease.ExecutionItemID)
		mu.Unlock()
		if input.Lease.ExecutionItemID == "execution-2" {
			return DerivedAssetProviderExecutionOutput{}, temporal.NewNonRetryableApplicationError(
				"模型能力快照需要批准",
				"MODEL_CAPABILITY_APPROVAL_REQUIRED",
				nil,
			)
		}
		return DerivedAssetProviderExecutionOutput{
			Lease: input.Lease,
			Response: provider.GatewayImageResponse{
				ProviderCallID: "call-1", ModelID: "model-1", Status: "succeeded",
				Output: provider.GatewayImageOutput{ArtifactID: "artifact-1", MediaFileID: "media-1", StorageKey: "derived/1.png"},
			},
		}, nil
	}, activity.RegisterOptions{Name: "RunDerivedAssetProvider"})
	env.RegisterActivityWithOptions(func(_ context.Context, input DerivedAssetProviderExecutionOutput) (DerivedAssetMediaVerification, error) {
		return DerivedAssetMediaVerification{
			Lease: input.Lease, ProviderCallID: input.Response.ProviderCallID, ModelID: input.Response.ModelID,
			ArtifactID: input.Response.Output.ArtifactID, MediaFileID: input.Response.Output.MediaFileID,
			StorageKey: input.Response.Output.StorageKey,
		}, nil
	}, activity.RegisterOptions{Name: "VerifyDerivedAssetMedia"})
	env.RegisterActivityWithOptions(func(context.Context, DerivedAssetMediaVerification) error {
		return nil
	}, activity.RegisterOptions{Name: "CommitDerivedAssetExecution"})
	env.RegisterActivityWithOptions(func(_ context.Context, failure DerivedAssetExecutionFailure) (bool, error) {
		mu.Lock()
		failedItems = append(failedItems, failure.Lease.ExecutionItemID)
		mu.Unlock()
		return true, nil
	}, activity.RegisterOptions{Name: "FailDerivedAssetExecution"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, batchID string) (DerivedAssetBatchOutput, error) {
		return DerivedAssetBatchOutput{
			BatchID: batchID, WorkflowRunID: "workflow-1", Status: "partial_succeeded",
			TotalItems: 2, ExecutableItems: 2, SucceededItems: 1, FailedTerminalItems: 1,
			CompletedItems: 1, FailedItems: 1,
		}, nil
	}, activity.RegisterOptions{Name: "CompleteDerivedAssetBatchWorkflowV2"})

	env.ExecuteWorkflow(BatchGenerateDerivedAssetImagesWorkflow, TextToStoryboardInput{
		OrganizationID: "org-1", ProjectID: "project-1", WorkflowRunID: "workflow-1", CreatedBy: "user-1",
		Input: json.RawMessage(`{"batchId":"batch-1","maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	var output DerivedAssetBatchOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "partial_succeeded" || output.TotalItems != 2 || output.SucceededItems != 1 || output.FailedItems != 1 {
		t.Fatalf("output = %+v", output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(providerCalls) != 2 {
		t.Fatalf("provider calls = %v, want both durable execution items", providerCalls)
	}
	if len(failedItems) != 1 || failedItems[0] != "execution-2" {
		t.Fatalf("failed items = %v, want execution-2", failedItems)
	}
}

func TestResolveDerivedAssetBatchOptionsPreservesInputOrderAndDuplicates(t *testing.T) {
	options := resolveDerivedAssetBatchOptions(json.RawMessage(`{
		"scriptEpisodeId":" episode-2 ",
		"requirementIds":[" requirement-2 ","requirement-1","requirement-2"],
		"shotIds":[" shot-2 ","shot-1","shot-2"],
		"maxConcurrency":99
	}`))
	if options.ScriptEpisodeID != "episode-2" {
		t.Fatalf("scriptEpisodeId = %q", options.ScriptEpisodeID)
	}
	wantRequirements := []string{"requirement-2", "requirement-1", "requirement-2"}
	if !equalStrings(options.RequirementIDs, wantRequirements) {
		t.Fatalf("requirementIds = %v, want %v", options.RequirementIDs, wantRequirements)
	}
	wantShots := []string{"shot-2", "shot-1", "shot-2"}
	if !equalStrings(options.ShotIDs, wantShots) {
		t.Fatalf("shotIds = %v, want %v", options.ShotIDs, wantShots)
	}
	if options.MaxConcurrency != MaxDerivedAssetImageConcurrency {
		t.Fatalf("maxConcurrency = %d", options.MaxConcurrency)
	}
}

func TestDerivedAssetBatchTerminalStatusIncludesBlockedAndSkippedWorkset(t *testing.T) {
	status, code, _ := derivedAssetBatchTerminalStatus(DerivedAssetBatchOutput{
		TotalItems: 4, ExecutableItems: 1, SucceededItems: 1,
		NotFoundItems: 1, DuplicateItems: 1, SkippedItems: 1,
	})
	if status != "partial_succeeded" || code != "DERIVED_ASSET_BATCH_PARTIAL" {
		t.Fatalf("terminal status = %s/%s, want partial_succeeded/DERIVED_ASSET_BATCH_PARTIAL", status, code)
	}
}

func TestBatchShotWorkflowFailurePreservesSharedActionableCode(t *testing.T) {
	code, message := batchShotWorkflowFailure(BatchShotProductionOutput{
		Status: "failed", Action: "batch_generate_shot_videos",
		TargetShotIDs: []string{"shot-1", "shot-2"}, FailedShotIDs: []string{"shot-1", "shot-2"},
		ErrorCodes: map[string]string{"shot-1": "RENDER_PLAN_REPLAN_REQUIRED", "shot-2": "RENDER_PLAN_REPLAN_REQUIRED"},
		Errors:     map[string]string{"shot-1": "没有可执行的已审核视频提示词契约", "shot-2": "没有可执行的已审核视频提示词契约"},
	})
	if code != "RENDER_PLAN_REPLAN_REQUIRED" || message != "没有可执行的已审核视频提示词契约" {
		t.Fatalf("failure = %s: %s", code, message)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
