package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestTerminalVideoSegmentFailureDoesNotRetryDeterministicErrors(t *testing.T) {
	tests := []struct {
		code     string
		wantType string
	}{
		{code: provider.CodeRenderPlanReplanRequired, wantType: provider.CodeRenderPlanReplanRequired},
		{code: provider.CodeInvalidRequest, wantType: provider.CodeInvalidRequest},
		{code: provider.CodeContentRejected, wantType: provider.CodeContentRejected},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			err := terminalVideoSegmentFailure(test.code, "确定性失败")
			var applicationErr *temporal.ApplicationError
			if !errors.As(err, &applicationErr) {
				t.Fatalf("error = %v, want Temporal application error", err)
			}
			if applicationErr.Type() != test.wantType || !applicationErr.NonRetryable() {
				t.Fatalf("type=%s nonRetryable=%v, want type=%s non-retryable", applicationErr.Type(), applicationErr.NonRetryable(), test.wantType)
			}
		})
	}
}

func TestValidateExpectedShotRenderDurationUsesFrozenProviderRequestTier(t *testing.T) {
	segments := []PreparedShotVideoSegment{{
		GatewayVideoPlanSegment: provider.GatewayVideoPlanSegment{
			PlannedDurationSeconds:   15,
			RequestedDurationSeconds: 16,
		},
	}}

	if err := validateExpectedShotRenderDuration(16, segments); err != nil {
		t.Fatalf("expected frozen 16 second provider tier to pass: %v", err)
	}
	err := validateExpectedShotRenderDuration(15, segments)
	if err == nil {
		t.Fatal("expected mismatched frozen provider tier to fail")
	}
	var applicationErr *temporal.ApplicationError
	if !errors.As(err, &applicationErr) || applicationErr.Type() != provider.CodeRenderPlanReplanRequired || !applicationErr.NonRetryable() {
		t.Fatalf("error = %v, want non-retryable %s", err, provider.CodeRenderPlanReplanRequired)
	}
}

func TestShotRenderExecutionTailFrameContractsExtractFreshTailForNextSegment(t *testing.T) {
	for _, contractKey := range []string{
		provider.VideoInputContractFirstFrame,
		provider.VideoInputContractFirstFramePlusReferences,
	} {
		t.Run(contractKey, func(t *testing.T) {
			testShotRenderExecutionTailFrameContract(t, contractKey)
		})
	}
}

func testShotRenderExecutionTailFrameContract(t *testing.T, contractKey string) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)

	prepared := preparedVideoPlanTestOutput("shot-1", "render-plan", "segment-1", "first reviewed prompt")
	prepared.Plan.CapabilitySnapshotHash = "sha256:" + strings.Repeat("a", 64)
	prepared.Segments[0].InputContractKey = contractKey
	second := prepared.Segments[0]
	second.SegmentID = "segment-2"
	second.SegmentIndex = 1
	second.Prompt = "second reviewed prompt"
	second.PromptHash = "prompt-hash-2"
	prepared.Segments = append(prepared.Segments, second)

	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanV2Input) (LoadPreparedShotVideoPlanOutput, error) {
		return prepared, nil
	}, activity.RegisterOptions{Name: "MaterializeAndBindExecutableShotVideoPlanV2"})
	createCalls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		createCalls++
		if input.SegmentIndex == 0 && input.PreviousSegmentTailFrame != nil {
			t.Fatal("first segment must not receive a continuation tail")
		}
		if input.SegmentIndex == 1 {
			frame := input.PreviousSegmentTailFrame
			if frame == nil || frame.SourceRenderSegmentID != "segment-1" || frame.ArtifactID != "tail-artifact" || frame.ContentHash == "" {
				t.Fatalf("second segment tail = %+v", frame)
			}
			if input.InputContractKey != contractKey {
				t.Fatalf("second segment contract = %s", input.InputContractKey)
			}
		}
		return CreateShotVideoTaskOutput{
			NodeRunID: "node-" + input.RenderSegmentID, ShotID: input.ShotID,
			ProviderAsyncTaskID: "task-" + input.RenderSegmentID, ExternalTaskID: "external-" + input.RenderSegmentID,
			Status: "running", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
			SegmentIndex: input.SegmentIndex, SegmentCount: input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		return PollShotVideoTaskOutput{
			ProviderAsyncTaskID: input.ProviderAsyncTaskID, ExternalTaskID: input.ExternalTaskID, Status: "succeeded",
			ArtifactID: "artifact-" + input.RenderSegmentID, MediaFileID: "media-" + input.RenderSegmentID,
			StorageKey: input.RenderSegmentID + ".mp4", MimeType: "video/mp4",
			ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
			SegmentIndex: input.SegmentIndex, SegmentCount: input.SegmentCount,
		}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
	generatedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	env.RegisterActivityWithOptions(func(_ context.Context, input ExtractRenderSegmentTailAnchorInput) (ExtractRenderSegmentTailAnchorOutput, error) {
		if input.SourceRenderSegmentID != "segment-1" || input.SourceVideoArtifactID != "artifact-segment-1" {
			t.Fatalf("tail extraction input = %+v", input)
		}
		return ExtractRenderSegmentTailAnchorOutput{
			AnchorID: "tail-anchor", SourceShotID: input.ShotID, SourceRenderSegmentID: input.SourceRenderSegmentID,
			SourceVideoArtifactID: input.SourceVideoArtifactID, ArtifactID: "tail-artifact", MediaFileID: "tail-media",
			StorageKey: "tail.png", MimeType: "image/png", ContentHash: strings.Repeat("b", 64), GeneratedAt: generatedAt,
		}, nil
	}, activity.RegisterOptions{Name: "ExtractRenderSegmentTailAnchor"})

	testWorkflow := func(ctx workflow.Context, input ShotRenderExecutionInput) (ShotRenderExecutionResult, error) {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
		return executePreparedShotRenderPlan(ctx, ctx, input)
	}
	env.RegisterWorkflow(testWorkflow)
	env.ExecuteWorkflow(testWorkflow, ShotRenderExecutionInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow",
		OperationID: "checkpoint", OperationItemID: "item", OperationAttempt: 1,
		CreatedBy: "user", ShotID: "shot-1", ShotNo: 1, MaxPolls: 1,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if createCalls != 2 {
		t.Fatalf("create calls = %d, want 2", createCalls)
	}
}

func registerRenderSegmentMediaTestActivity(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(_ context.Context, input ProcessRenderSegmentMediaInput) (ProcessRenderSegmentMediaOutput, error) {
		return ProcessRenderSegmentMediaOutput{
			ExecutionPlanID:           input.ExecutionPlanID,
			RenderSegmentID:           input.RenderSegmentID,
			RawArtifactID:             input.RawArtifactID,
			RawMediaFileID:            input.RawMediaFileID,
			RawStorageKey:             input.RawStorageKey,
			MezzanineArtifactID:       "mezzanine-" + input.RenderSegmentID,
			MezzanineMediaFileID:      "mezzanine-media-" + input.RenderSegmentID,
			MezzanineStorageKey:       "mezzanine/" + input.RenderSegmentID + ".mp4",
			ExtractedAudioArtifactID:  "audio-" + input.RenderSegmentID,
			ExtractedAudioMediaFileID: "audio-media-" + input.RenderSegmentID,
			ExtractedAudioStorageKey:  "audio/" + input.RenderSegmentID + ".m4a",
		}, nil
	}, activity.RegisterOptions{Name: "ProcessRenderSegmentMedia"})
	env.RegisterActivityWithOptions(func(_ context.Context, input ComposeShotRenderPlanMediaInput) (ComposeShotRenderPlanMediaOutput, error) {
		return ComposeShotRenderPlanMediaOutput{
			ExecutionPlanID: input.ExecutionPlanID, ShotID: input.ShotID,
			ArtifactID: "shot-video-" + input.ShotID, MediaFileID: "shot-video-media-" + input.ShotID,
			StorageKey: "shot-video/" + input.ShotID + ".mp4", MimeType: "video/mp4",
			NativeAudioStatus: "audio_unverified", ProductionReadiness: "preview_only",
		}, nil
	}, activity.RegisterOptions{Name: "ComposeShotRenderPlanMedia"})
}

func registerShotVideoExecutionGroupsTestActivity(env *testsuite.TestWorkflowEnvironment) {
	var finalOutput BatchShotProductionOutput
	env.RegisterWorkflow(EpisodeBatchGenerateShotVideosWorkflow)
	env.RegisterWorkflow(EpisodeVideoProductionWorkflow)
	env.RegisterWorkflow(SceneOrShotBatchWorkflow)
	env.RegisterActivityWithOptions(func(_ context.Context, input PrepareEpisodeVideoProductionsInput) ([]EpisodeVideoProductionPlan, error) {
		return []EpisodeVideoProductionPlan{{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
			WorkflowRunID: input.WorkflowRunID, CreatedBy: input.CreatedBy,
			ScriptEpisodeID: "test-episode", EpisodeIndex: 1,
			TemporalWorkflowID: input.WorkflowRunID + ":test-episode",
			CheckpointID:       "test-checkpoint", TargetShotIDs: append([]string(nil), input.Options.ShotIDs...),
		}}, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductions"})
	env.RegisterActivityWithOptions(func(_ context.Context, input EpisodeVideoProductionInput) (EpisodeVideoProductionBatch, error) {
		shots := make([]ShotVideoExecutionShot, 0, len(input.Plan.TargetShotIDs))
		for index, shotID := range input.Plan.TargetShotIDs {
			shots = append(shots, ShotVideoExecutionShot{ShotID: shotID, ShotIndex: index, ShotNo: index + 1})
		}
		return EpisodeVideoProductionBatch{BatchID: "test-batch", CheckpointID: input.Plan.CheckpointID, Ordinal: 0, Shots: shots}, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductionBatchV2"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CommitEpisodeVideoProductionBatchInput) (CommitEpisodeVideoProductionBatchOutput, error) {
		finalOutput = input.Output
		return CommitEpisodeVideoProductionBatchOutput{HasMore: false, Status: input.Output.Status}, nil
	}, activity.RegisterOptions{Name: "CommitEpisodeVideoProductionBatchV2"})
	env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionPlan) error {
		return nil
	}, activity.RegisterOptions{Name: "ReconcileEpisodeVideoProductionCheckpointV2"})
	env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionPlan) (BatchShotProductionOutput, error) {
		return finalOutput, nil
	}, activity.RegisterOptions{Name: "LoadEpisodeVideoProductionOutputV2"})
	env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionPlan) error { return nil }, activity.RegisterOptions{Name: "CancelEpisodeVideoProductionCheckpoint"})
	env.RegisterActivityWithOptions(func(context.Context, FailEpisodeVideoProductionCheckpointInput) error { return nil }, activity.RegisterOptions{Name: "FailEpisodeVideoProductionCheckpoint"})
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, BatchShotProductionOutput) error { return nil }, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})
	env.RegisterActivityWithOptions(func(context.Context, TextToStoryboardInput, BatchShotProductionOutput, string) error { return nil }, activity.RegisterOptions{Name: "FinalizeBatchShotProductionCancellation"})
}
