package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestBuildShotVideoExecutionGroupsSplitsContinuityGaps(t *testing.T) {
	records := []shotVideoPreparationRecord{
		{Shot: ShotVideoExecutionShot{ShotID: "shot-1", ShotIndex: 0, ShotNo: 1}, ContinuityGroupID: "continuity-a"},
		{Shot: ShotVideoExecutionShot{ShotID: "shot-2", ShotIndex: 1, ShotNo: 2}, ContinuityGroupID: "continuity-a", PredecessorShotID: "shot-1"},
		{
			Shot: ShotVideoExecutionShot{ShotID: "shot-4", ShotIndex: 3, ShotNo: 4}, ContinuityGroupID: "continuity-a",
			PredecessorShotID: "shot-3", PredecessorShotIndex: 2, PredecessorShotNo: 3,
			PredecessorArtifactID: "video-3", PredecessorMediaFileID: "media-3", PredecessorStorageKey: "videos/3.mp4",
			PredecessorVideoStatus: "succeeded", PredecessorStaleState: "fresh",
		},
	}
	groups := buildShotVideoExecutionGroups(records)
	if len(groups) != 2 || len(groups[0].Shots) != 2 || groups[0].Shots[1].ShotID != "shot-2" || len(groups[1].Shots) != 1 || groups[1].Shots[0].ShotID != "shot-4" {
		t.Fatalf("groups = %+v", groups)
	}
	predecessor := groups[1].InitialPredecessor
	if predecessor == nil || predecessor.ShotID != "shot-3" || predecessor.ArtifactID != "video-3" || predecessor.MediaFileID != "media-3" || predecessor.StorageKey != "videos/3.mp4" {
		t.Fatalf("initial predecessor = %+v", predecessor)
	}
}

func TestBuildShotVideoExecutionGroupsBlocksUnavailableGapPredecessor(t *testing.T) {
	groups := buildShotVideoExecutionGroups([]shotVideoPreparationRecord{{
		Shot: ShotVideoExecutionShot{ShotID: "shot-4", ShotIndex: 3, ShotNo: 4}, ContinuityGroupID: "continuity-a",
		PredecessorShotID: "shot-3", PredecessorShotIndex: 2, PredecessorShotNo: 3,
		PredecessorVideoStatus: "failed", PredecessorStaleState: "fresh",
	}})
	if len(groups) != 1 || groups[0].InitialDependencyError == "" || groups[0].InitialPredecessor != nil {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestCrossShotContinuityUsesPreviousShotTailFrame(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	registerSuccessfulContinuityShotActivities(t, env, nil)
	var extracted []ExtractShotContinuityFrameInput
	env.RegisterActivityWithOptions(func(_ context.Context, input ExtractShotContinuityFrameInput) (ExtractShotContinuityFrameOutput, error) {
		extracted = append(extracted, input)
		return ExtractShotContinuityFrameOutput{
			ContinuityFrameID: "frame-record-1", SourceShotID: input.ShotID, SourceVideoArtifactID: input.SourceVideoArtifactID,
			ArtifactID: "tail-artifact-1", MediaFileID: "tail-media-1", StorageKey: "continuity/tail-1.png", MimeType: "image/png",
		}, nil
	}, activity.RegisterOptions{Name: "ExtractShotContinuityFrame"})

	env.ExecuteWorkflow(ShotVideoContinuityGroupWorkflow, ShotVideoContinuityGroupInput{
		TextInput: TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user"},
		Options:   BatchShotProductionOptions{MaxPolls: 1, PollIntervalSeconds: 1},
		Group: ShotVideoExecutionGroup{GroupKey: "continuity-a", ContinuityGroupID: "continuity-a", Shots: []ShotVideoExecutionShot{
			{ShotID: "shot-1", ShotIndex: 0, ShotNo: 1}, {ShotID: "shot-2", ShotIndex: 1, ShotNo: 2},
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	var output BatchShotProductionOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "succeeded" || len(output.SucceededShotIDs) != 2 || len(extracted) != 1 || extracted[0].ShotID != "shot-1" || extracted[0].SourceVideoArtifactID != "shot-video-shot-1" {
		t.Fatalf("output=%+v extracted=%+v", output, extracted)
	}
}

func TestCrossShotContinuityResumesFromExistingPredecessor(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	var created CreateShotVideoTaskInput
	registerSuccessfulContinuityShotActivities(t, env, func(input CreateShotVideoTaskInput) { created = input })
	var extracted ExtractShotContinuityFrameInput
	env.RegisterActivityWithOptions(func(_ context.Context, input ExtractShotContinuityFrameInput) (ExtractShotContinuityFrameOutput, error) {
		extracted = input
		return ExtractShotContinuityFrameOutput{
			ContinuityFrameID: "frame-record-3", SourceShotID: input.ShotID, SourceVideoArtifactID: input.SourceVideoArtifactID,
			ArtifactID: "tail-artifact-3", MediaFileID: "tail-media-3", StorageKey: "continuity/tail-3.png", MimeType: "image/png",
		}, nil
	}, activity.RegisterOptions{Name: "ExtractShotContinuityFrame"})

	env.ExecuteWorkflow(ShotVideoContinuityGroupWorkflow, ShotVideoContinuityGroupInput{
		TextInput: TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user"},
		Options:   BatchShotProductionOptions{MaxPolls: 1, PollIntervalSeconds: 1},
		Group: ShotVideoExecutionGroup{
			GroupKey: "continuity-a-from-shot-4", ContinuityGroupID: "continuity-a",
			InitialPredecessor: &ShotVideoContinuitySource{
				ShotID: "shot-3", ShotIndex: 2, ShotNo: 3, ArtifactID: "video-3", MediaFileID: "video-media-3", StorageKey: "videos/3.mp4",
			},
			Shots: []ShotVideoExecutionShot{{ShotID: "shot-4", ShotIndex: 3, ShotNo: 4}},
		},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if extracted.ShotID != "shot-3" || extracted.SourceVideoArtifactID != "video-3" {
		t.Fatalf("extracted = %+v", extracted)
	}
	frame := created.ContinuityFirstFrame
	if created.ShotID != "shot-4" || frame == nil || frame.SourceShotID != "shot-3" || frame.ArtifactID != "tail-artifact-3" {
		t.Fatalf("create input = %+v", created)
	}
}

func TestCrossShotContinuityTailFailureBlocksOnlyDownstreamShots(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerRenderSegmentMediaTestActivity(env)
	registerSuccessfulContinuityShotActivities(t, env, nil)
	env.RegisterActivityWithOptions(func(context.Context, ExtractShotContinuityFrameInput) (ExtractShotContinuityFrameOutput, error) {
		return ExtractShotContinuityFrameOutput{}, errors.New("ffmpeg could not decode tail frame")
	}, activity.RegisterOptions{Name: "ExtractShotContinuityFrame"})

	env.ExecuteWorkflow(ShotVideoContinuityGroupWorkflow, ShotVideoContinuityGroupInput{
		TextInput: TextToStoryboardInput{OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user"},
		Options:   BatchShotProductionOptions{MaxPolls: 1, PollIntervalSeconds: 1},
		Group: ShotVideoExecutionGroup{GroupKey: "continuity-a", ContinuityGroupID: "continuity-a", Shots: []ShotVideoExecutionShot{
			{ShotID: "shot-1", ShotIndex: 0, ShotNo: 1}, {ShotID: "shot-2", ShotIndex: 1, ShotNo: 2},
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	var output BatchShotProductionOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "partial_succeeded" || len(output.SucceededShotIDs) != 1 || output.SucceededShotIDs[0] != "shot-1" || len(output.FailedShotIDs) != 1 || output.FailedShotIDs[0] != "shot-2" || !strings.Contains(output.Errors["shot-2"], "CONTINUITY_REFERENCE_UNAVAILABLE") {
		t.Fatalf("output = %+v", output)
	}
}

func registerSuccessfulContinuityShotActivities(t *testing.T, env *testsuite.TestWorkflowEnvironment, onCreate func(CreateShotVideoTaskInput)) {
	t.Helper()
	env.RegisterActivityWithOptions(func(_ context.Context, input EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		return preparedVideoPlanTestOutput(input.ShotID, "plan-"+input.ShotID, "segment-"+input.ShotID, "reviewed "+input.ShotID), nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
		if onCreate != nil {
			onCreate(input)
		}
		if input.ShotID == "shot-2" {
			frame := input.ContinuityFirstFrame
			if frame == nil || frame.SourceShotID != "shot-1" || frame.ArtifactID != "tail-artifact-1" || frame.MediaFileID != "tail-media-1" || frame.StorageKey != "continuity/tail-1.png" {
				t.Fatalf("shot-2 continuity input = %+v", input)
			}
		}
		return CreateShotVideoTaskOutput{
			NodeRunID: "node-" + input.ShotID, ShotID: input.ShotID, ProviderAsyncTaskID: "task-" + input.ShotID,
			Status: "running", ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
		}, nil
	}, activity.RegisterOptions{Name: "CreateShotVideoTask"})
	env.RegisterActivityWithOptions(func(_ context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
		return PollShotVideoTaskOutput{
			ProviderAsyncTaskID: input.ProviderAsyncTaskID, Status: "succeeded", ArtifactID: "raw-" + input.ShotID,
			MediaFileID: "raw-media-" + input.ShotID, StorageKey: "raw/" + input.ShotID + ".mp4",
			ExecutionPlanID: input.ExecutionPlanID, RenderSegmentID: input.RenderSegmentID,
		}, nil
	}, activity.RegisterOptions{Name: "PollShotVideoTask"})
}

func TestBatchGenerateShotVideosWorkflowFinalizesPreparationFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, PrepareShotVideoExecutionGroupsInput) ([]ShotVideoExecutionGroup, error) {
		return nil, errors.New("storyboard shot shot-1 is missing or not in the active plan")
	}, activity.RegisterOptions{Name: "PrepareShotVideoExecutionGroups"})
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "run",
		Input: json.RawMessage(`{"shotIds":["shot-1"],"maxConcurrency":1}`),
	})
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if completed.Status != "failed" || len(completed.FailedShotIDs) != 1 || completed.FailedShotIDs[0] != "shot-1" {
		t.Fatalf("completed = %+v", completed)
	}
	if !strings.Contains(completed.Errors["shot-1"], "missing or not in the active plan") {
		t.Fatalf("failure detail = %q", completed.Errors["shot-1"])
	}
}

func TestBatchGenerateShotVideosWorkflowContinuesAsNewOnlyAfterCompletedGroupCheckpoint(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	groups := []ShotVideoExecutionGroup{
		{GroupKey: "group-1", Shots: []ShotVideoExecutionShot{{ShotID: "shot-1", ShotNo: 1}}},
		{GroupKey: "group-2", Shots: []ShotVideoExecutionShot{{ShotID: "shot-2", ShotNo: 2}}},
	}
	env.RegisterActivityWithOptions(func(context.Context, PrepareShotVideoExecutionGroupsInput) ([]ShotVideoExecutionGroup, error) {
		return groups, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoExecutionGroups"})
	env.OnWorkflow(ShotVideoContinuityGroupWorkflow, mock.Anything, mock.Anything).Return(
		BatchShotProductionOutput{
			Action: "batch_generate_shot_videos", Status: "succeeded", TargetShotIDs: []string{"shot-1"},
			SucceededShotIDs: []string{"shot-1"}, ProviderAsyncTaskIDs: map[string]string{}, Errors: map[string]string{},
		}, nil,
	).Once()
	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "run",
		Input: json.RawMessage(`{"shotIds":["shot-1","shot-2"],"maxConcurrency":2,"groupsPerRun":1}`),
	})
	if !env.IsWorkflowCompleted() || !workflow.IsContinueAsNewError(env.GetWorkflowError()) {
		t.Fatalf("workflow error = %v, want ContinueAsNew", env.GetWorkflowError())
	}
	env.AssertExpectations(t)
}

func TestBatchShotOutputStatusKeepsPartialSuccess(t *testing.T) {
	output := BatchShotProductionOutput{SucceededShotIDs: []string{"shot-1"}, FailedShotIDs: []string{"shot-2"}}
	if status := batchShotOutputStatus(output); status != "partial_succeeded" {
		t.Fatalf("status = %q, want partial_succeeded", status)
	}
}

func TestSeventyMinuteVideoBatchUsesBoundedContinueAsNewCheckpoint(t *testing.T) {
	const shotCount = 420 // 420 ten-second shots model a 70-minute production batch.
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	groups := make([]ShotVideoExecutionGroup, 0, shotCount)
	shotIDs := make([]string, 0, shotCount)
	for index := 0; index < shotCount; index++ {
		shotID := fmt.Sprintf("shot-%03d", index+1)
		shotIDs = append(shotIDs, shotID)
		groups = append(groups, ShotVideoExecutionGroup{GroupKey: shotID, Shots: []ShotVideoExecutionShot{{ShotID: shotID, ShotIndex: index, ShotNo: index + 1}}})
	}
	env.RegisterActivityWithOptions(func(context.Context, PrepareShotVideoExecutionGroupsInput) ([]ShotVideoExecutionGroup, error) {
		return groups, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoExecutionGroups"})
	env.OnWorkflow(ShotVideoContinuityGroupWorkflow, mock.Anything, mock.Anything).Return(
		BatchShotProductionOutput{Action: "batch_generate_shot_videos", Status: "succeeded", ProviderAsyncTaskIDs: map[string]string{}, Errors: map[string]string{}}, nil,
	).Times(defaultVideoGroupsPerRun)
	raw, err := json.Marshal(map[string]any{"shotIds": shotIDs, "maxConcurrency": 5, "groupsPerRun": defaultVideoGroupsPerRun})
	if err != nil {
		t.Fatalf("marshal options: %v", err)
	}
	env.ExecuteWorkflow(BatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "run-70m", Input: raw,
	})
	if !env.IsWorkflowCompleted() || !workflow.IsContinueAsNewError(env.GetWorkflowError()) {
		t.Fatalf("workflow error = %v, want bounded ContinueAsNew after %d groups", env.GetWorkflowError(), defaultVideoGroupsPerRun)
	}
	env.AssertExpectations(t)
}
