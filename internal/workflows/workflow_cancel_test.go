package workflows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestVideoProductionWorkflowCancellationCleanup(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	registerShotVideoExecutionGroupsTestActivity(env)
	var cancelCalled bool
	var workflowCancelled bool
	var cancelOutput CancelShotVideoTaskOutput
	shots := []StoryboardShotRecord{{ID: "shot-1", WorkflowRunID: "workflow", ShotIndex: 0, ShotNo: 1, Duration: 5, ImagePrompt: "station", VideoPrompt: "station video", Status: "storyboard_ready"}}

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateStoryboardTextInput) (GenerateStoryboardTextOutput, error) {
		return GenerateStoryboardTextOutput{
			StoryboardArtifactID: "storyboard-artifact",
			ProviderCallID:       "text-call",
			Storyboard:           json.RawMessage(`{"shots":[{"imagePrompt":"station","videoPrompt":"station video"}]}`),
			Shots:                shots,
		}, nil
	}, activity.RegisterOptions{Name: "GenerateStoryboardText"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input ListStoryboardShotsInput) ([]StoryboardShotRecord, error) {
		return shots, nil
	}, activity.RegisterOptions{Name: "ListStoryboardShots"})
	env.RegisterActivityWithOptions(func(_ context.Context, input ResolveShotAnchorWorkItemsInput) ([]ShotAnchorWorkItem, error) {
		return []ShotAnchorWorkItem{{ShotID: input.ShotIDs[0], ShotIndex: 0, ShotNo: 1, AnchorRole: "planned_first_frame"}}, nil
	}, activity.RegisterOptions{Name: "ResolveShotAnchorWorkItems"})
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		output := preparedVideoPlanTestOutput("shot-1", "render-plan", "render-segment", "reviewed video prompt")
		output.Plan.CapabilitySnapshotHash = "sha256:capability"
		output.Segments[0].RequestedDurationSeconds = 5
		return output, nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareShotImagePromptInput) (PrepareShotImagePromptOutput, error) {
		return PrepareShotImagePromptOutput{ShotID: input.ShotID, Prompt: "reviewed image prompt", PromptHash: "sha256:image"}, nil
	}, activity.RegisterOptions{Name: "PrepareShotImagePrompt"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		return GenerateShotImageOutput{
			NodeRunID:        "image-node",
			ShotID:           input.ShotID,
			ImageArtifactID:  "image-artifact",
			ImageMediaFileID: "image-media",
			ImageStorageKey:  "image-key",
			ProviderCallID:   "image-call",
		}, nil
	}, activity.RegisterOptions{Name: "GenerateShotImage"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		t.Fatalf("video generation must not call prompt agents: %+v", input)
		return PrepareShotVideoPromptOutput{}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		return CreateShotVideoTaskOutput{
			NodeRunID:           "video-node",
			ShotID:              input.ShotID,
			ProviderCallID:      "create-call",
			ProviderAsyncTaskID: "provider-task",
			ExternalTaskID:      "external-task",
			Status:              "running",
			ModelID:             "video-model",
			ExecutionPlanID:     input.ExecutionPlanID,
			RenderSegmentID:     input.RenderSegmentID,
			SegmentCount:        input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		return PollShotVideoTaskOutput{
			ProviderCallID:      "poll-call",
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			Status:              "running",
			ExecutionPlanID:     input.ExecutionPlanID,
			RenderSegmentID:     input.RenderSegmentID,
			SegmentCount:        input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
		cancelCalled = true
		if input.ProviderAsyncTaskID != "provider-task" || input.NodeRunID != "video-node" || input.ShotID != "shot-1" {
			t.Fatalf("cancel input = %+v", input)
		}
		cancelOutput = CancelShotVideoTaskOutput{
			ProviderCallID:      "cancel-call",
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			ShotID:              input.ShotID,
			ShotIndex:           input.ShotIndex,
			ShotNo:              input.ShotNo,
			Status:              "cancelled",
			ExecutionPlanID:     input.ExecutionPlanID,
			RenderSegmentID:     input.RenderSegmentID,
		}
		return cancelOutput, nil
	}, activity.RegisterOptions{Name: "CancelShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output CancelShotVideoTaskOutput, reason string) error {
		workflowCancelled = true
		return nil
	}, activity.RegisterOptions{Name: "CancelVideoProductionWorkflow"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output VideoProductionOutput) error {
		t.Fatal("workflow should not complete after cancellation")
		return nil
	}, activity.RegisterOptions{Name: "CompleteVideoProductionWorkflow"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, nodeRunID, code, message string) error {
		t.Fatal("workflow should not fail by timeout in cancellation test")
		return nil
	}, activity.RegisterOptions{Name: "FailVideoProductionWorkflow"})

	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, time.Second)
	env.ExecuteWorkflow(VideoProductionWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		Prompt:         "train station",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"maxPolls":120,"pollIntervalSeconds":5}`),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("workflow error is nil, want cancellation")
	}
	if !cancelCalled || !workflowCancelled {
		t.Fatalf("cleanup not called: cancel=%v workflow=%v output=%+v", cancelCalled, workflowCancelled, cancelOutput)
	}
}

func TestShotRenderExecutionCancellationTargetsActiveSegmentInMultiSegmentPlan(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)

	prepared := preparedVideoPlanTestOutput("shot-1", "render-plan", "segment-1", "first reviewed prompt")
	prepared.Plan.CapabilitySnapshotHash = "sha256:capability"
	second := prepared.Segments[0]
	second.SegmentID = "segment-2"
	second.SegmentIndex = 1
	second.PlannedStartTick = 360000
	second.PlannedEndTick = 720000
	second.Prompt = "second reviewed prompt"
	second.PromptHash = "prompt-hash-2"
	prepared.Segments = append(prepared.Segments, second)

	env.RegisterActivityWithOptions(func(_ context.Context, input EnsurePreparedShotVideoPlanV2Input) (LoadPreparedShotVideoPlanOutput, error) {
		if input.OperationID != "checkpoint-1" || input.OperationItemID != "item-1" || input.OperationItemAttempt != 1 {
			t.Fatalf("materialize identity = %+v", input)
		}
		return prepared, nil
	}, activity.RegisterOptions{Name: "MaterializeAndBindExecutableShotVideoPlanV2"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		return CreateShotVideoTaskOutput{
			NodeRunID: "node-" + input.RenderSegmentID, ShotID: input.ShotID,
			ProviderAsyncTaskID: "task-" + input.RenderSegmentID,
			ExternalTaskID:      "external-" + input.RenderSegmentID,
			Status:              "running", ExecutionPlanID: input.ExecutionPlanID,
			RenderSegmentID: input.RenderSegmentID, SegmentIndex: input.SegmentIndex, SegmentCount: input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		if input.RenderSegmentID == "segment-1" {
			return PollShotVideoTaskOutput{
				ProviderAsyncTaskID: input.ProviderAsyncTaskID, ExternalTaskID: input.ExternalTaskID,
				Status: "succeeded", ArtifactID: "artifact-1", MediaFileID: "media-1", StorageKey: "segment-1.mp4",
				ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
				SegmentIndex: input.SegmentIndex, SegmentCount: input.SegmentCount,
			}, nil
		}
		return PollShotVideoTaskOutput{
			ProviderAsyncTaskID: input.ProviderAsyncTaskID, ExternalTaskID: input.ExternalTaskID,
			Status: "running", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
			SegmentIndex: input.SegmentIndex, SegmentCount: input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	cancelledTaskID := ""
	env.RegisterActivityWithOptions(func(_ context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
		cancelledTaskID = input.ProviderAsyncTaskID
		if input.ProviderAsyncTaskID != "task-segment-2" || input.RenderSegmentID != "segment-2" || input.SegmentIndex != 1 {
			t.Fatalf("cancel input = %+v", input)
		}
		return CancelShotVideoTaskOutput{
			ProviderAsyncTaskID: input.ProviderAsyncTaskID, ExternalTaskID: input.ExternalTaskID,
			Status: "cancelled", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
		}, nil
	}, activity.RegisterOptions{Name: "CancelShotVideoTask"})

	testWorkflow := func(ctx workflow.Context, input ShotRenderExecutionInput) (ShotRenderExecutionResult, error) {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
		return executePreparedShotRenderPlan(ctx, ctx, input)
	}
	env.RegisterWorkflow(testWorkflow)
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
	env.ExecuteWorkflow(testWorkflow, ShotRenderExecutionInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow",
		OperationID: "checkpoint-1", OperationItemID: "item-1", OperationAttempt: 1,
		CreatedBy: "user", ShotID: "shot-1", ShotNo: 1, MaxPolls: 120, PollInterval: 5 * time.Second,
	})
	if env.GetWorkflowError() == nil {
		t.Fatal("workflow error is nil, want cancellation")
	}
	if cancelledTaskID != "task-segment-2" {
		t.Fatalf("cancelled task = %q, want active second segment task; workflow error=%v", cancelledTaskID, env.GetWorkflowError())
	}
}
