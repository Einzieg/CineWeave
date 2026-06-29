package workflows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestVideoProductionWorkflowCancellationCleanup(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var cancelCalled bool
	var workflowCancelled bool
	var cancelOutput CancelStoryboardVideoTaskOutput

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateStoryboardTextInput) (GenerateStoryboardTextOutput, error) {
		return GenerateStoryboardTextOutput{
			StoryboardArtifactID: "storyboard-artifact",
			ProviderCallID:       "text-call",
			Storyboard:           json.RawMessage(`{"shots":[{"imagePrompt":"station","videoPrompt":"station video"}]}`),
		}, nil
	}, activity.RegisterOptions{Name: "GenerateStoryboardText"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateStoryboardImageInput) (GenerateStoryboardImageOutput, error) {
		return GenerateStoryboardImageOutput{
			ImageArtifactID:  "image-artifact",
			ImageMediaFileID: "image-media",
			ImageStorageKey:  "image-key",
			ProviderCallID:   "image-call",
		}, nil
	}, activity.RegisterOptions{Name: "GenerateStoryboardImage"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CreateStoryboardVideoTaskInput) (CreateStoryboardVideoTaskOutput, error) {
		return CreateStoryboardVideoTaskOutput{
			NodeRunID:           "video-node",
			ProviderCallID:      "create-call",
			ProviderAsyncTaskID: "provider-task",
			ExternalTaskID:      "external-task",
			Status:              "running",
			ModelID:             "video-model",
		}, nil
	}, activity.RegisterOptions{Name: "CreateStoryboardVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PollStoryboardVideoTaskInput) (PollStoryboardVideoTaskOutput, error) {
		return PollStoryboardVideoTaskOutput{
			ProviderCallID:      "poll-call",
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			Status:              "running",
		}, nil
	}, activity.RegisterOptions{Name: "PollStoryboardVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CancelStoryboardVideoTaskInput) (CancelStoryboardVideoTaskOutput, error) {
		cancelCalled = true
		if input.ProviderAsyncTaskID != "provider-task" || input.NodeRunID != "video-node" {
			t.Fatalf("cancel input = %+v", input)
		}
		cancelOutput = CancelStoryboardVideoTaskOutput{
			ProviderCallID:      "cancel-call",
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			Status:              "cancelled",
		}
		return cancelOutput, nil
	}, activity.RegisterOptions{Name: "CancelStoryboardVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output CancelStoryboardVideoTaskOutput, reason string) error {
		workflowCancelled = true
		if output.ProviderAsyncTaskID != "provider-task" || output.Status != "cancelled" {
			t.Fatalf("cancel workflow output = %+v", output)
		}
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
