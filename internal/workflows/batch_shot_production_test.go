package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestBatchGenerateShotImagesWorkflowContinuesAfterFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var completed BatchShotProductionOutput

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		if input.WorkflowPrompt != "batch_generate_shot_images" || !input.Force {
			t.Fatalf("image input = %+v", input)
		}
		if input.ShotID == "shot-2" {
			return GenerateShotImageOutput{}, errors.New("image failed")
		}
		return GenerateShotImageOutput{
			NodeRunID:       "image-node-" + input.ShotID,
			ShotID:          input.ShotID,
			ImageArtifactID: "image-artifact-" + input.ShotID,
			ProviderCallID:  "image-call-" + input.ShotID,
		}, nil
	}, activity.RegisterOptions{Name: "GenerateShotImage"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagesWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		Prompt:         "batch_generate_shot_images",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3"],"force":true}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(completed.SucceededShotIDs) != 2 || completed.SucceededShotIDs[0] != "shot-1" || completed.SucceededShotIDs[1] != "shot-3" {
		t.Fatalf("succeeded = %+v", completed)
	}
	if len(completed.FailedShotIDs) != 1 || completed.FailedShotIDs[0] != "shot-2" || completed.Errors["shot-2"] == "" {
		t.Fatalf("failed = %+v", completed)
	}
}

func TestBatchGenerateShotVideosWorkflowRecordsVideoOutput(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var completed BatchShotProductionOutput

	env.RegisterActivityWithOptions(func(ctx context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		if input.WorkflowPrompt != "batch_generate_shot_videos" || input.ShotID != "shot-1" || input.AspectRatio != "16:9" || input.Resolution != "720p" {
			t.Fatalf("create input = %+v", input)
		}
		return CreateShotVideoTaskOutput{
			NodeRunID:           "video-node",
			ShotID:              input.ShotID,
			ProviderCallID:      "create-call",
			ProviderAsyncTaskID: "provider-task",
			ExternalTaskID:      "external-task",
			Status:              "running",
			ModelID:             "video-model",
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		if input.ProviderAsyncTaskID != "provider-task" || input.PollCount != 1 {
			t.Fatalf("poll input = %+v", input)
		}
		return PollShotVideoTaskOutput{
			ProviderCallID:      "poll-call",
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			Status:              "succeeded",
			ArtifactID:          "video-artifact",
			MediaFileID:         "video-media",
			StorageKey:          "video-key.mp4",
			MimeType:            "video/mp4",
			PollCount:           input.PollCount,
		}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
		t.Fatalf("cancel should not be called: %+v", input)
		return CancelShotVideoTaskOutput{}, nil
	}, activity.RegisterOptions{Name: "CancelShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		Prompt:         "batch_generate_shot_videos",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1"],"force":true,"maxPolls":1,"pollIntervalSeconds":1}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(completed.SucceededShotIDs) != 1 || completed.SucceededShotIDs[0] != "shot-1" || completed.ProviderAsyncTaskIDs["shot-1"] != "provider-task" {
		t.Fatalf("completed = %+v", completed)
	}
	if len(completed.VideoOutputs) != 1 || completed.VideoOutputs[0].ArtifactID != "video-artifact" {
		t.Fatalf("video outputs = %+v", completed.VideoOutputs)
	}
}
