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
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func registerSingleFrameShotAnchorWorkItemsTestActivity(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(_ context.Context, input ResolveShotAnchorWorkItemsInput) ([]ShotAnchorWorkItem, error) {
		items := make([]ShotAnchorWorkItem, 0, len(input.ShotIDs))
		for index, shotID := range input.ShotIDs {
			items = append(items, ShotAnchorWorkItem{ShotID: shotID, ShotIndex: index, ShotNo: index + 1, AnchorRole: "planned_first_frame"})
		}
		return items, nil
	}, activity.RegisterOptions{Name: "ResolveShotAnchorWorkItems"})
}

func registerFirstLastShotAnchorWorkItemsTestActivity(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(_ context.Context, input ResolveShotAnchorWorkItemsInput) ([]ShotAnchorWorkItem, error) {
		items := make([]ShotAnchorWorkItem, 0, len(input.ShotIDs)*2)
		for index, shotID := range input.ShotIDs {
			for _, role := range []string{videoproduction.AnchorRolePlannedFirstFrame, videoproduction.AnchorRolePlannedLastFrame} {
				items = append(items, ShotAnchorWorkItem{ShotID: shotID, ShotIndex: index, ShotNo: index + 1, AnchorRole: role})
			}
		}
		return items, nil
	}, activity.RegisterOptions{Name: "ResolveShotAnchorWorkItems"})
}

func TestBatchShotProgressCountsUsesUniqueTargetsAndFailurePrecedence(t *testing.T) {
	total, completed, failed := batchShotProgressCounts(BatchShotProductionOutput{
		TargetShotIDs:    []string{"shot-1", "shot-2", "shot-2", "shot-3"},
		SucceededShotIDs: []string{"shot-1", "shot-2", "shot-2", "outside"},
		FailedShotIDs:    []string{"shot-2", "shot-3", "shot-3", "outside"},
	})
	if total != 3 || completed != 1 || failed != 2 {
		t.Fatalf("counts = total:%d completed:%d failed:%d", total, completed, failed)
	}
}

func TestBatchGenerateShotImagePromptsWorkflowPersistsFailureWhenResolverCannotRun(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagePromptsWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1","shot-2"],"force":true,"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if completed.Status != "failed" || len(completed.FailedShotIDs) != 2 || completed.FailedShotIDs[0] != "shot-1" || completed.FailedShotIDs[1] != "shot-2" {
		t.Fatalf("completed = %+v", completed)
	}
	if completed.Errors["shot-1"] == "" || completed.Errors["shot-2"] == "" {
		t.Fatalf("failure details were not persisted: %+v", completed.Errors)
	}
}

func TestBatchGenerateShotImagesWorkflowExpandsFirstLastAnchors(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerFirstLastShotAnchorWorkItemsTestActivity(env)
	var completed BatchShotProductionOutput
	var rolesMu sync.Mutex
	roles := map[string]int{}
	env.RegisterActivityWithOptions(func(_ context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		rolesMu.Lock()
		roles[input.AnchorRole]++
		rolesMu.Unlock()
		return GenerateShotImageOutput{ShotID: input.ShotID, AnchorRole: input.AnchorRole, ImageArtifactID: "artifact-" + input.AnchorRole}, nil
	}, activity.RegisterOptions{Name: "GenerateShotImage"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagesWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1"],"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	rolesMu.Lock()
	firstCount := roles[videoproduction.AnchorRolePlannedFirstFrame]
	lastCount := roles[videoproduction.AnchorRolePlannedLastFrame]
	rolesMu.Unlock()
	if firstCount != 1 || lastCount != 1 {
		t.Fatalf("generated anchor roles = %+v", roles)
	}
	if len(completed.ImageOutputs) != 2 || len(completed.SucceededShotIDs) != 1 || completed.SucceededShotIDs[0] != "shot-1" || len(completed.FailedShotIDs) != 0 {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestBatchGenerateShotImagePromptsWorkflowFailsShotWhenLastAnchorFails(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerFirstLastShotAnchorWorkItemsTestActivity(env)
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, input PrepareShotImagePromptInput) (PrepareShotImagePromptOutput, error) {
		if input.AnchorRole == videoproduction.AnchorRolePlannedLastFrame {
			return PrepareShotImagePromptOutput{}, errors.New("last frame prompt rejected")
		}
		return PrepareShotImagePromptOutput{ShotID: input.ShotID, AnchorRole: input.AnchorRole, Prompt: "first frame"}, nil
	}, activity.RegisterOptions{Name: "PrepareShotImagePrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagePromptsWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1"],"force":true,"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(completed.SucceededShotIDs) != 0 || len(completed.FailedShotIDs) != 1 || completed.FailedShotIDs[0] != "shot-1" ||
		!strings.Contains(completed.Errors["shot-1"], videoproduction.AnchorRolePlannedLastFrame) {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestBatchGenerateShotImagesWorkflowUsesBoundedConcurrency(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerSingleFrameShotAnchorWorkItemsTestActivity(env)
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
	env.RegisterActivityWithOptions(func(context.Context, ReconcileStoryboardDialogueAssignmentsInput) (ReconcileStoryboardDialogueAssignmentsOutput, error) {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, nil
	}, activity.RegisterOptions{Name: "ReconcileStoryboardDialogueAssignments"})
	var completed BatchShotProductionOutput
	var mu sync.Mutex
	preparedContracts := map[string]bool{"shot-1": true}
	coarsePromptCalls := 0
	planCalls := 0
	segmentPromptCalls := 0
	finalizeCalls := 0
	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		if !input.PromptOnly || input.WorkflowPrompt != "batch_generate_shot_video_prompts" || !input.Force {
			t.Fatalf("prompt input = %+v", input)
		}
		if input.RenderSegmentID != "" {
			if input.ExecutionPlanID != "plan-"+input.ShotID || input.RequestedDuration != 8 || input.Duration != 8 || input.SegmentCount != 1 {
				t.Fatalf("segment prompt input = %+v", input)
			}
			mu.Lock()
			segmentPromptCalls++
			mu.Unlock()
			return PrepareShotVideoPromptOutput{ShotID: input.ShotID, Prompt: "segment-prompt-" + input.ShotID}, nil
		}
		if input.ShotID == "shot-2" {
			return PrepareShotVideoPromptOutput{}, errors.New("dialogue validation failed")
		}
		mu.Lock()
		coarsePromptCalls++
		preparedContracts[input.ShotID] = true
		mu.Unlock()
		return PrepareShotVideoPromptOutput{ShotID: input.ShotID, Prompt: "prompt-" + input.ShotID, VideoPromptPlanID: "contract-" + input.ShotID}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PlanShotVideoInput) (PlanShotVideoOutput, error) {
		mu.Lock()
		contractPrepared := preparedContracts[input.ShotID]
		planCalls++
		mu.Unlock()
		if !contractPrepared {
			return PlanShotVideoOutput{}, temporal.NewNonRetryableApplicationError(
				"没有可执行的已审核视频提示词契约",
				provider.CodeRenderPlanReplanRequired,
				nil,
			)
		}
		if !input.PromptOnly || input.WorkflowPrompt != "batch_generate_shot_video_prompts" || !input.Force {
			t.Fatalf("plan input = %+v", input)
		}
		return PlanShotVideoOutput{GatewayVideoPlanResponse: provider.GatewayVideoPlanResponse{
			ExecutionPlanID: "plan-" + input.ShotID,
			Segments: []provider.GatewayVideoPlanSegment{{
				SegmentID: "segment-" + input.ShotID, SegmentIndex: 0,
				PlannedStartTick: 0, PlannedEndTick: 7 * 90000, RequestedDurationSeconds: 8,
			}},
		}}, nil
	}, activity.RegisterOptions{Name: "PlanShotVideo"})
	env.RegisterActivityWithOptions(func(_ context.Context, input FinalizeShotVideoPromptPlanInput) error {
		if input.ExecutionPlanID != "plan-"+input.ShotID || input.PromptSource != "segment_video_prompt_agents" {
			t.Fatalf("finalize input = %+v", input)
		}
		mu.Lock()
		finalizeCalls++
		mu.Unlock()
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
	if len(completed.VideoPromptOutputs) != 3 {
		t.Fatalf("prompt outputs = %+v", completed.VideoPromptOutputs)
	}
	mu.Lock()
	gotCoarsePromptCalls := coarsePromptCalls
	gotPlanCalls := planCalls
	gotSegmentPromptCalls := segmentPromptCalls
	gotFinalizeCalls := finalizeCalls
	mu.Unlock()
	if gotCoarsePromptCalls != 1 {
		t.Fatalf("coarse prompt calls = %d, want only shot-3 regenerated", gotCoarsePromptCalls)
	}
	if gotPlanCalls != 4 {
		t.Fatalf("render plan calls = %d, want shot-1 reused plus shot-2/3 recovery checks", gotPlanCalls)
	}
	if gotSegmentPromptCalls != 2 || gotFinalizeCalls != 2 {
		t.Fatalf("segment prompt/finalize calls = %d/%d, want 2/2", gotSegmentPromptCalls, gotFinalizeCalls)
	}
}

func TestBatchGenerateShotVideoPromptsWorkflowFinalizesCancellation(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ReconcileStoryboardDialogueAssignmentsInput) (ReconcileStoryboardDialogueAssignmentsOutput, error) {
		return ReconcileStoryboardDialogueAssignmentsOutput{}, nil
	}, activity.RegisterOptions{Name: "ReconcileStoryboardDialogueAssignments"})
	var finalized BatchShotProductionOutput

	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		<-ctx.Done()
		return PrepareShotVideoPromptOutput{}, ctx.Err()
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		finalized = output
		return nil
	}, activity.RegisterOptions{Name: "FinalizeBatchShotProductionCancellation"})
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)

	env.ExecuteWorkflow(BatchGenerateShotVideoPromptsWorkflow, TextToStoryboardInput{
		OrganizationID: "org",
		ProjectID:      "project",
		WorkflowRunID:  "workflow",
		CreatedBy:      "user",
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2"],"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if finalized.Status != "cancelled" || len(finalized.CancelledShotIDs) != 2 || finalized.CancelledShotIDs[0] != "shot-1" || finalized.CancelledShotIDs[1] != "shot-2" {
		t.Fatalf("finalized cancellation = %+v", finalized)
	}
}

func TestBatchGenerateShotImagePromptsWorkflowContinuesAfterFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerSingleFrameShotAnchorWorkItemsTestActivity(env)
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
	registerSingleFrameShotAnchorWorkItemsTestActivity(env)
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
	registerSingleFrameShotAnchorWorkItemsTestActivity(env)
	var completed BatchShotProductionOutput
	var calledMu sync.Mutex
	called := map[string]bool{}

	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		calledMu.Lock()
		called[input.ShotID] = true
		calledMu.Unlock()
		if input.WorkflowPrompt != "batch_generate_shot_images" || !input.Force || input.FailureScope != workflowFailureScopeBatchItem {
			t.Fatalf("image input = %+v", input)
		}
		if input.ShotID == "shot-2" {
			return GenerateShotImageOutput{}, temporal.NewNonRetryableApplicationError(
				"provider request timed out", provider.CodeUpstreamTimeout, context.DeadlineExceeded,
			)
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
		Input:          json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3","shot-4","shot-5","shot-6"],"force":true,"maxConcurrency":2}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if len(completed.SucceededShotIDs) != 5 || completed.SucceededShotIDs[0] != "shot-1" || completed.SucceededShotIDs[1] != "shot-3" {
		t.Fatalf("succeeded = %+v", completed)
	}
	if len(completed.FailedShotIDs) != 1 || completed.FailedShotIDs[0] != "shot-2" || completed.Errors["shot-2"] == "" {
		t.Fatalf("failed = %+v", completed)
	}
	if completed.ErrorCodes["shot-2"] != provider.CodeUpstreamTimeout {
		t.Fatalf("error code = %q, want %q", completed.ErrorCodes["shot-2"], provider.CodeUpstreamTimeout)
	}
	for _, shotID := range []string{"shot-1", "shot-2", "shot-3", "shot-4", "shot-5", "shot-6"} {
		calledMu.Lock()
		wasCalled := called[shotID]
		calledMu.Unlock()
		if !wasCalled {
			t.Fatalf("%s was not scheduled after an item timeout", shotID)
		}
	}
}

func TestBatchGenerateShotImagesWorkflowStopsUnstartedItemsOnInsufficientBalance(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerSingleFrameShotAnchorWorkItemsTestActivity(env)
	var mu sync.Mutex
	called := make([]string, 0, 2)
	started := make(chan struct{})
	var startedOnce sync.Once
	active := 0
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
		mu.Lock()
		called = append(called, input.ShotID)
		active++
		if active == 2 {
			startedOnce.Do(func() { close(started) })
		}
		mu.Unlock()
		select {
		case <-started:
		case <-time.After(time.Second):
			return GenerateShotImageOutput{}, errors.New("initial billing window did not start")
		case <-ctx.Done():
			return GenerateShotImageOutput{}, ctx.Err()
		}
		if input.ShotID == "shot-1" {
			return GenerateShotImageOutput{}, temporal.NewNonRetryableApplicationError(
				"New API 账户余额不足",
				billingInsufficientBalanceCode,
				nil,
			)
		}
		time.Sleep(30 * time.Millisecond)
		return GenerateShotImageOutput{
			ShotID: input.ShotID, ProviderCallID: "call-" + input.ShotID,
		}, nil
	}, activity.RegisterOptions{Name: "GenerateShotImage"})
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(
		_ context.Context,
		_ TextToStoryboardInput,
		output BatchShotProductionOutput,
	) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotImagesWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3","shot-4","shot-5"],"force":true,"maxConcurrency":2}`),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	mu.Lock()
	gotCalls := append([]string(nil), called...)
	mu.Unlock()
	if len(gotCalls) != 2 ||
		!((gotCalls[0] == "shot-1" && gotCalls[1] == "shot-2") ||
			(gotCalls[0] == "shot-2" && gotCalls[1] == "shot-1")) {
		t.Fatalf("provider-backed image calls = %v, want only initial window", gotCalls)
	}
	for _, shotID := range []string{"shot-1", "shot-3", "shot-4", "shot-5"} {
		if completed.ErrorCodes[shotID] != billingInsufficientBalanceCode {
			t.Fatalf("%s error code = %q, want %q", shotID, completed.ErrorCodes[shotID], billingInsufficientBalanceCode)
		}
	}
	if !strings.Contains(completed.Errors["shot-3"], "未发起") {
		t.Fatalf("unstarted item message = %q", completed.Errors["shot-3"])
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
			{GatewayVideoPlanSegment: provider.GatewayVideoPlanSegment{SegmentID: "segment-2", SegmentIndex: 1, RequestedDurationSeconds: 4, ContinuityMode: "previous_segment_tail"}, Prompt: "reviewed segment 2", PromptHash: "sha256:reviewed-2", ReviewProviderCallID: "review-call-2"},
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

func TestBatchGenerateShotVideosWorkflowCancelsTimedOutAttemptBeforeRetry(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	registerShotVideoExecutionGroupsTestActivity(env)
	createCalls := 0
	pollCalls := 0
	cancelCalled := false
	retryCalled := false
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		return preparedVideoPlanTestOutput("shot-1", "plan-a", "segment-a", "reviewed"), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		createCalls++
		if createCalls == 2 && input.RetryGeneration != 1 {
			t.Fatalf("retry generation = %d, want 1", input.RetryGeneration)
		}
		return CreateShotVideoTaskOutput{
			NodeRunID: fmt.Sprintf("node-%d", createCalls), ShotID: input.ShotID,
			ProviderAsyncTaskID: fmt.Sprintf("task-%d", createCalls), ExternalTaskID: fmt.Sprintf("external-%d", createCalls),
			Status: "running", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		pollCalls++
		if pollCalls == 1 {
			return PollShotVideoTaskOutput{ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "queued", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
		}
		return PollShotVideoTaskOutput{ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "succeeded", ArtifactID: "artifact", MediaFileID: "media", StorageKey: "video.mp4", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
		cancelCalled = true
		if input.ProviderAsyncTaskID != "task-1" || input.NodeRunID != "node-1" || input.RenderSegmentID != "segment-a" {
			t.Fatalf("cancel input = %+v", input)
		}
		return CancelShotVideoTaskOutput{ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "cancelled", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "CancelShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		if !cancelCalled {
			t.Fatal("retry was attempted before the previous provider task reached a cancelled state")
		}
		retryCalled = true
		return RetryShotVideoRenderSegmentOutput{GatewayVideoRetrySegmentResponse: provider.GatewayVideoRetrySegmentResponse{RetryGeneration: 1, RetryScope: "segment"}}, nil
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1,"pollIntervalSeconds":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if !cancelCalled || !retryCalled || createCalls != 2 || pollCalls != 2 {
		t.Fatalf("cancel=%v retry=%v createCalls=%d pollCalls=%d", cancelCalled, retryCalled, createCalls, pollCalls)
	}
}

func TestBatchGenerateShotVideosWorkflowDoesNotRetryWhenTimedOutAttemptCannotBeCancelled(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerShotVideoExecutionGroupsTestActivity(env)
	var completed BatchShotProductionOutput
	retryCalled := false
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		return preparedVideoPlanTestOutput("shot-1", "plan-a", "segment-a", "reviewed"), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		return CreateShotVideoTaskOutput{NodeRunID: "node-a", ShotID: input.ShotID, ProviderAsyncTaskID: "task-a", Status: "running", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		return PollShotVideoTaskOutput{ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "queued", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
		return CancelShotVideoTaskOutput{ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "failed", ErrorMessage: "upstream cancel rejected", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID}, nil
	}, activity.RegisterOptions{Name: "CancelShotVideoTask"})
	env.RegisterActivityWithOptions(func(context.Context, RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		retryCalled = true
		return RetryShotVideoRenderSegmentOutput{}, nil
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1,"pollIntervalSeconds":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if retryCalled {
		t.Fatal("retry must not start while the previous provider task is still active")
	}
	if completed.Status != "failed" || completed.ErrorCodes["shot-1"] != provider.CodeProviderCancelFailed {
		t.Fatalf("completed = %+v", completed)
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
		return CreateShotVideoTaskOutput{ShotID: input.ShotID, Status: "failed", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID, ErrorCode: provider.CodeRenderPlanReplanRequired, ErrorMessage: "capability changed"}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(context.Context, RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		t.Fatal("deterministic render-plan failures must not create provider retry attempts")
		return RetryShotVideoRenderSegmentOutput{}, nil
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if completed.Status != "failed" || completed.ErrorCodes["shot-1"] != provider.CodeRenderPlanReplanRequired || completed.Errors["shot-1"] != "capability changed" {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestBatchGenerateShotVideosWorkflowPreservesFailureWhenFallbackIsExhausted(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerShotVideoExecutionGroupsTestActivity(env)
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		return preparedVideoPlanTestOutput("shot-1", "plan-a", "segment-a", "reviewed"), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		return CreateShotVideoTaskOutput{
			ShotID: input.ShotID, Status: "failed", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
			ErrorCode: provider.CodeUpstreamTimeout, ErrorMessage: "Video task exceeded total timeout after 500 seconds",
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(context.Context, RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		return RetryShotVideoRenderSegmentOutput{}, temporal.NewNonRetryableApplicationError(
			"no active video fallback candidate remains for this render segment",
			provider.CodeModelCapabilityUnavailable,
			nil,
		)
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if completed.Status != "failed" || completed.ErrorCodes["shot-1"] != provider.CodeUpstreamTimeout || completed.Errors["shot-1"] != "Video task exceeded total timeout after 500 seconds" {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestBatchGenerateShotVideosWorkflowStopsAtAttemptBudgetWithoutPreparingDanglingRetry(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerShotVideoExecutionGroupsTestActivity(env)
	var completed BatchShotProductionOutput
	createCalls := 0
	retryCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		return preparedVideoPlanTestOutput("shot-1", "plan-a", "segment-a", "reviewed"), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		createCalls++
		return CreateShotVideoTaskOutput{
			ShotID: input.ShotID, Status: "failed", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
			ErrorCode: provider.CodeUpstreamInternalError, ErrorMessage: "upstream create failed",
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input RetryShotVideoRenderSegmentInput) (RetryShotVideoRenderSegmentOutput, error) {
		retryCalls++
		return RetryShotVideoRenderSegmentOutput{GatewayVideoRetrySegmentResponse: provider.GatewayVideoRetrySegmentResponse{
			RetryGeneration: input.CurrentRetryGeneration + 1,
			RetryScope:      "segment",
		}}, nil
	}, activity.RegisterOptions{Name: "RetryShotVideoRenderSegment"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user", Input: json.RawMessage(`{"shotIds":["shot-1"],"maxPolls":1}`)})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if createCalls != maxVideoRenderSegmentAttempts || retryCalls != maxVideoRenderSegmentAttempts-1 {
		t.Fatalf("createCalls=%d retryCalls=%d, want %d creates and %d retries", createCalls, retryCalls, maxVideoRenderSegmentAttempts, maxVideoRenderSegmentAttempts-1)
	}
	if completed.Status != "failed" || completed.ErrorCodes["shot-1"] != provider.CodeUpstreamInternalError || completed.Errors["shot-1"] != "upstream create failed" {
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
