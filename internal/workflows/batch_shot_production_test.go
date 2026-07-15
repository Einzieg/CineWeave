package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestBatchGenerateShotImagesWorkflowUsesBoundedConcurrency(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var mu sync.Mutex
	active := 0
	maxActive := 0
	release := make(chan struct{})
	var releaseOnce sync.Once

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		if active >= 2 {
			releaseOnce.Do(func() { close(release) })
		}
		mu.Unlock()

		select {
		case <-release:
		case <-time.After(time.Second):
			return GenerateShotImageOutput{}, errors.New("image activities did not overlap")
		case <-ctx.Done():
			return GenerateShotImageOutput{}, ctx.Err()
		}
		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		active--
		mu.Unlock()
		return GenerateShotImageOutput{ShotID: input.ShotID, ProviderCallID: "call-" + input.ShotID}, nil
	}, activity.RegisterOptions{Name: "GenerateShotImage"})
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, BatchShotProductionOutput) error {
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagesWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3","shot-4","shot-5"],"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	mu.Lock()
	gotMaxActive := maxActive
	mu.Unlock()
	if gotMaxActive != 2 {
		t.Fatalf("max concurrent image activities = %d, want 2", gotMaxActive)
	}
}

func TestResolveBatchShotImageConcurrencyDefaultsAndClamps(t *testing.T) {
	defaults := resolveBatchShotProductionOptions(nil, DefaultShotImageConcurrency, MaxShotImageConcurrency)
	if defaults.MaxConcurrency != DefaultShotImageConcurrency {
		t.Fatalf("default maxConcurrency = %d, want %d", defaults.MaxConcurrency, DefaultShotImageConcurrency)
	}
	clamped := resolveBatchShotProductionOptions(json.RawMessage(`{"maxConcurrency":99}`), DefaultShotImageConcurrency, MaxShotImageConcurrency)
	if clamped.MaxConcurrency != MaxShotImageConcurrency {
		t.Fatalf("clamped maxConcurrency = %d, want %d", clamped.MaxConcurrency, MaxShotImageConcurrency)
	}
}

func TestBatchGenerateShotVideoPromptsWorkflowUsesPromptOnlyActivities(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, input PlanShotVideoInput) (PlanShotVideoOutput, error) {
		if !input.PromptOnly || input.WorkflowPrompt != "batch_generate_shot_video_prompts" || !input.Force {
			t.Fatalf("plan input = %+v", input)
		}
		return PlanShotVideoOutput{GatewayVideoPlanResponse: provider.GatewayVideoPlanResponse{
			ExecutionPlanID: "plan-" + input.ShotID,
			AudioStrategy:   "native_av", AudioRequirement: "preferred",
			Segments: []provider.GatewayVideoPlanSegment{{
				SegmentID: "segment-" + input.ShotID, SegmentIndex: 0,
				PlannedDurationSeconds: 5, RequestedDurationSeconds: 5,
			}},
		}}, nil
	}, activity.RegisterOptions{Name: "PlanShotVideo"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		if !input.PromptOnly || input.WorkflowPrompt != "batch_generate_shot_video_prompts" || !input.Force || input.ExecutionPlanID == "" || input.RenderSegmentID == "" {
			t.Fatalf("prompt input = %+v", input)
		}
		if input.ShotID == "shot-2" {
			return PrepareShotVideoPromptOutput{}, errors.New("dialogue validation failed")
		}
		return PrepareShotVideoPromptOutput{ShotID: input.ShotID, Prompt: "prompt-" + input.ShotID}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, input FinalizeShotVideoPromptPlanInput) error {
		if input.ExecutionPlanID != "plan-"+input.ShotID {
			t.Fatalf("finalize input = %+v", input)
		}
		return nil
	}, activity.RegisterOptions{Name: "FinalizeShotVideoPromptPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideoPromptsWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3"],"force":true,"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(completed.SucceededShotIDs) != 2 || len(completed.FailedShotIDs) != 1 || completed.FailedShotIDs[0] != "shot-2" {
		t.Fatalf("completed = %+v", completed)
	}
	if len(completed.VideoPromptOutputs) != 2 {
		t.Fatalf("prompt outputs = %+v", completed.VideoPromptOutputs)
	}
}

func TestBatchGenerateShotImagePromptsWorkflowContinuesAfterFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareShotImagePromptInput) (PrepareShotImagePromptOutput, error) {
		if input.WorkflowPrompt != "batch_generate_shot_image_prompts" || !input.Force {
			t.Fatalf("prompt input = %+v", input)
		}
		if input.ShotID == "shot-2" {
			return PrepareShotImagePromptOutput{}, errors.New("source fact validation failed")
		}
		return PrepareShotImagePromptOutput{ShotID: input.ShotID, Prompt: "prompt-" + input.ShotID}, nil
	}, activity.RegisterOptions{Name: "PrepareShotImagePrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagePromptsWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3"],"force":true,"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(completed.SucceededShotIDs) != 2 || len(completed.FailedShotIDs) != 1 || completed.FailedShotIDs[0] != "shot-2" {
		t.Fatalf("completed = %+v", completed)
	}
	if len(completed.ImagePromptOutputs) != 2 {
		t.Fatalf("prompt outputs = %+v", completed.ImagePromptOutputs)
	}
}

func TestBatchGenerateShotImagePromptsWorkflowUsesBoundedConcurrency(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var mu sync.Mutex
	active := 0
	maxActive := 0
	release := make(chan struct{})
	var releaseOnce sync.Once
	var completed BatchShotProductionOutput

	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareShotImagePromptInput) (PrepareShotImagePromptOutput, error) {
		if input.AspectRatio != "21:9" || input.Size != "1080p" {
			return PrepareShotImagePromptOutput{}, fmt.Errorf("prompt media settings = %s/%s", input.AspectRatio, input.Size)
		}
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		if active >= 3 {
			releaseOnce.Do(func() { close(release) })
		}
		mu.Unlock()

		select {
		case <-release:
		case <-time.After(time.Second):
			return PrepareShotImagePromptOutput{}, errors.New("image prompt activities did not overlap")
		case <-ctx.Done():
			return PrepareShotImagePromptOutput{}, ctx.Err()
		}
		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		active--
		mu.Unlock()
		return PrepareShotImagePromptOutput{ShotID: input.ShotID, Prompt: "prompt-" + input.ShotID}, nil
	}, activity.RegisterOptions{Name: "PrepareShotImagePrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagePromptsWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3","shot-4","shot-5","shot-6"],"maxConcurrency":3,"aspectRatio":"21:9","resolution":"1080p"}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	mu.Lock()
	gotMaxActive := maxActive
	mu.Unlock()
	if gotMaxActive != 3 {
		t.Fatalf("max concurrent image prompt activities = %d, want 3", gotMaxActive)
	}
	if len(completed.SucceededShotIDs) != 6 || len(completed.FailedShotIDs) != 0 {
		t.Fatalf("completed = %+v", completed)
	}
}

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

func TestBatchShotImageErrorCode(t *testing.T) {
	tests := map[string]string{
		"activity failed (type: CONTENT_REJECTED): guardrail violation": provider.CodeContentRejected,
		"activity StartToClose timeout":                                 provider.CodeUpstreamTimeout,
		"Post gateway: context deadline exceeded":                       provider.CodeUpstreamTimeout,
		"provider returned RATE_LIMITED":                                provider.CodeRateLimited,
		"plain activity failure":                                        codeActivityFailed,
	}
	for message, want := range tests {
		if got := batchShotImageErrorCode(message); got != want {
			t.Errorf("batchShotImageErrorCode(%q) = %q, want %q", message, got, want)
		}
	}
}

func TestBatchGenerateShotVideosWorkflowRecordsVideoOutput(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	registerShotVideoExecutionGroupsTestActivity(env)
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, input EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		if input.ShotID != "shot-1" || input.AudioStrategy != "native_av" || input.AudioRequirement != "preferred" || !input.Force || input.WorkflowPrompt != "batch_generate_shot_videos" {
			t.Fatalf("load input = %+v", input)
		}
		plan := PlanShotVideoOutput{GatewayVideoPlanResponse: provider.GatewayVideoPlanResponse{
			ExecutionPlanID: "render-plan", CapabilitySnapshotHash: "sha256:capability",
		}}
		return LoadPreparedShotVideoPlanOutput{Plan: plan, Segments: []PreparedShotVideoSegment{
			{GatewayVideoPlanSegment: provider.GatewayVideoPlanSegment{SegmentID: "segment-1", SegmentIndex: 0, RequestedDurationSeconds: 8, ContinuityMode: "first_frame"}, Prompt: "reviewed segment 1", PromptHash: "sha256:reviewed-1", ReviewProviderCallID: "review-call-1"},
			{GatewayVideoPlanSegment: provider.GatewayVideoPlanSegment{SegmentID: "segment-2", SegmentIndex: 1, RequestedDurationSeconds: 4, ContinuityMode: "previous_last_frame"}, Prompt: "reviewed segment 2", PromptHash: "sha256:reviewed-2", ReviewProviderCallID: "review-call-2"},
		}}, nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		t.Fatalf("video generation must not prepare prompts: %+v", input)
		return PrepareShotVideoPromptOutput{}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		wantSegmentID := fmt.Sprintf("segment-%d", input.SegmentIndex+1)
		wantPrompt := fmt.Sprintf("reviewed segment %d", input.SegmentIndex+1)
		if input.WorkflowPrompt != "batch_generate_shot_videos" || input.ShotID != "shot-1" || input.AspectRatio != "16:9" || input.Resolution != "720p" || input.Prompt != wantPrompt || input.ExecutionPlanID != "render-plan" || input.RenderSegmentID != wantSegmentID {
			t.Fatalf("create input = %+v", input)
		}
		if input.SegmentIndex == 1 && (input.PreviousSegmentArtifactID != "video-artifact-0" || input.PreviousSegmentMediaFileID != "video-media-0" || input.PreviousSegmentStorageKey != "video-key-0.mp4") {
			t.Fatalf("second segment continuity input = %+v", input)
		}
		return CreateShotVideoTaskOutput{
			NodeRunID:           fmt.Sprintf("video-node-%d", input.SegmentIndex),
			ShotID:              input.ShotID,
			ProviderCallID:      fmt.Sprintf("create-call-%d", input.SegmentIndex),
			ProviderAsyncTaskID: fmt.Sprintf("provider-task-%d", input.SegmentIndex),
			ExternalTaskID:      fmt.Sprintf("external-task-%d", input.SegmentIndex),
			Status:              "running",
			ModelID:             "video-model",
			ExecutionPlanID:     input.ExecutionPlanID,
			RenderSegmentID:     input.RenderSegmentID,
			SegmentCount:        input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		if input.ProviderAsyncTaskID != fmt.Sprintf("provider-task-%d", input.SegmentIndex) || input.PollCount != 1 {
			t.Fatalf("poll input = %+v", input)
		}
		return PollShotVideoTaskOutput{
			ProviderCallID:      "poll-call",
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
			Status:              "succeeded",
			ArtifactID:          fmt.Sprintf("video-artifact-%d", input.SegmentIndex),
			MediaFileID:         fmt.Sprintf("video-media-%d", input.SegmentIndex),
			StorageKey:          fmt.Sprintf("video-key-%d.mp4", input.SegmentIndex),
			MimeType:            "video/mp4",
			PollCount:           input.PollCount,
			ExecutionPlanID:     input.ExecutionPlanID,
			RenderSegmentID:     input.RenderSegmentID,
			SegmentCount:        input.SegmentCount,
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
	if len(completed.SucceededShotIDs) != 1 || completed.SucceededShotIDs[0] != "shot-1" || completed.ProviderAsyncTaskIDs["shot-1"] != "provider-task-1" {
		t.Fatalf("completed = %+v", completed)
	}
	if len(completed.VideoOutputs) != 2 || completed.VideoOutputs[0].ArtifactID != "video-artifact-0" || completed.VideoOutputs[1].ArtifactID != "video-artifact-1" {
		t.Fatalf("video outputs = %+v", completed.VideoOutputs)
	}
}

func TestBatchGenerateShotVideosWorkflowRunsFiveIndependentGroupsConcurrentlyByDefault(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerShotVideoExecutionGroupsTestActivity(env)
	var mu sync.Mutex
	active := 0
	maxActive := 0
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return LoadPreparedShotVideoPlanOutput{}, temporal.NewNonRetryableApplicationError("prompt plan unavailable", provider.CodeRenderPlanReplanRequired, nil)
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, BatchShotProductionOutput) error { return nil }, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3","shot-4","shot-5","shot-6"]}`),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if DefaultShotVideoConcurrency != 5 {
		t.Fatalf("default video concurrency = %d, want 5", DefaultShotVideoConcurrency)
	}
	if maxActive != DefaultShotVideoConcurrency {
		t.Fatalf("max concurrent video groups = %d, want %d", maxActive, DefaultShotVideoConcurrency)
	}
}

func TestBatchGenerateShotVideosWorkflowRetriesPreparedSegmentWithoutPromptAgents(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	registerShotVideoExecutionGroupsTestActivity(env)
	loadCalls := 0
	createCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		loadCalls++
		return preparedVideoPlanTestOutput("shot-1", "plan-a", "segment-a", "reviewed"), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(context.Context, PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		t.Fatal("video generation must not call prompt agents")
		return PrepareShotVideoPromptOutput{}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		createCalls++
		if input.Prompt != "reviewed" {
			t.Fatalf("prepared prompt was not reused: %+v", input)
		}
		if createCalls == 1 {
			return CreateShotVideoTaskOutput{NodeRunID: "node-a", ShotID: input.ShotID, Status: "failed", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID, ErrorCode: provider.CodeUpstreamTimeout, ErrorMessage: "timeout"}, nil
		}
		if input.RetryGeneration != 1 {
			t.Fatalf("retry generation = %d, want 1", input.RetryGeneration)
		}
		return CreateShotVideoTaskOutput{NodeRunID: "node-b", ShotID: input.ShotID, ProviderAsyncTaskID: "task-b", Status: "running", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(context.Context, RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		return RetryShotVideoRenderSegmentOutput{GatewayVideoRetrySegmentResponse: provider.GatewayVideoRetrySegmentResponse{RetryGeneration: 1, RetryScope: "segment"}}, nil
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		return PollShotVideoTaskOutput{ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "succeeded", ArtifactID: "artifact", MediaFileID: "media", StorageKey: "video.mp4", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, BatchShotProductionOutput) error { return nil }, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})
	env.RegisterActivityWithOptions(func(context.Context, CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
		return CancelShotVideoTaskOutput{}, nil
	}, activity.RegisterOptions{Name: "CancelShotVideoTask"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1,"pollIntervalSeconds":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if loadCalls != 1 || createCalls != 2 {
		t.Fatalf("loadCalls=%d createCalls=%d", loadCalls, createCalls)
	}
}

func TestBatchGenerateShotVideosWorkflowRequiresPromptRegenerationForReplan(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerShotVideoExecutionGroupsTestActivity(env)
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		return preparedVideoPlanTestOutput("shot-1", "plan-a", "segment-a", "reviewed"), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(context.Context, PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		t.Fatal("video generation must not call prompt agents")
		return PrepareShotVideoPromptOutput{}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		return CreateShotVideoTaskOutput{ShotID: input.ShotID, Status: "failed", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID, ErrorCode: provider.CodeModelCapabilityUnavailable, ErrorMessage: "capability changed"}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(context.Context, RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		return RetryShotVideoRenderSegmentOutput{}, temporal.NewNonRetryableApplicationError("whole shot replan", provider.CodeRenderPlanReplanRequired, nil)
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if completed.Status != "failed" || !strings.Contains(completed.Errors["shot-1"], "重新批量生成视频提示词") {
		t.Fatalf("completed = %+v", completed)
	}
}

func preparedVideoPlanTestOutput(shotID, planID, segmentID, prompt string) LoadPreparedShotVideoPlanOutput {
	plan := PlanShotVideoOutput{GatewayVideoPlanResponse: provider.GatewayVideoPlanResponse{ExecutionPlanID: planID, CapabilitySnapshotHash: "hash-a"}}
	return LoadPreparedShotVideoPlanOutput{Plan: plan, Segments: []PreparedShotVideoSegment{{
		GatewayVideoPlanSegment: provider.GatewayVideoPlanSegment{SegmentID: segmentID, SegmentIndex: 0, PlannedDurationSeconds: 4, RequestedDurationSeconds: 4, ContinuityMode: "first_frame"},
		Prompt:                  prompt, PromptHash: "prompt-hash", ReviewProviderCallID: "review-call",
	}}}
}
