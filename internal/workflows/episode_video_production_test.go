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
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestEpisodeBatchGenerateShotVideosWorkflowBoundsTenEpisodeConcurrency(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var active, maxActive int
	env.RegisterActivityWithOptions(func(context.Context, PrepareEpisodeVideoProductionsInput) ([]EpisodeVideoProductionPlan, error) {
		plans := make([]EpisodeVideoProductionPlan, 10)
		for index := range plans {
			plans[index] = episodeVideoTestPlan(index, fmt.Sprintf("shot-%d", index+1))
		}
		return plans, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductions"})
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, input EpisodeVideoProductionInput) (BatchShotProductionOutput, error) {
		active++
		if active > maxActive {
			maxActive = active
		}
		if err := workflow.Sleep(ctx, time.Minute); err != nil {
			return BatchShotProductionOutput{}, err
		}
		active--
		output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID}, input.Plan.TargetShotIDs)
		output.SucceededShotIDs = append(output.SucceededShotIDs, input.Plan.TargetShotIDs...)
		output.Status = "succeeded"
		return output, nil
	}, workflow.RegisterOptions{Name: "EpisodeVideoProductionWorkflow"})
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(EpisodeBatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1","shot-2","shot-3","shot-4","shot-5","shot-6","shot-7","shot-8","shot-9","shot-10"]}`),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if maxActive != defaultEpisodeWorkflowParallel {
		t.Fatalf("max active episodes = %d, want %d", maxActive, defaultEpisodeWorkflowParallel)
	}
	if completed.Status != "succeeded" || len(completed.SucceededShotIDs) != 10 {
		t.Fatalf("completed = %+v", completed)
	}
}

func TestEpisodeBatchStopsUnstartedPlansOnInsufficientBalance(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	plans := make([]EpisodeVideoProductionPlan, 11)
	shotIDs := make([]string, 11)
	for index := range plans {
		shotIDs[index] = fmt.Sprintf("shot-%d", index+1)
		plans[index] = episodeVideoTestPlan(index, shotIDs[index])
	}
	env.RegisterActivityWithOptions(func(
		context.Context,
		PrepareEpisodeVideoProductionsInput,
	) ([]EpisodeVideoProductionPlan, error) {
		return plans, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductions"})
	startedPlans := make([]int, 0, defaultEpisodeWorkflowParallel)
	env.RegisterWorkflowWithOptions(func(
		_ workflow.Context,
		input EpisodeVideoProductionInput,
	) (BatchShotProductionOutput, error) {
		startedPlans = append(startedPlans, input.Plan.EpisodeIndex)
		output := newBatchShotVideoOutput(
			TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID},
			input.Plan.TargetShotIDs,
		)
		if input.Plan.EpisodeIndex == 0 {
			shotID := input.Plan.TargetShotIDs[0]
			output.FailedShotIDs = []string{shotID}
			output.ErrorCodes[shotID] = billingInsufficientBalanceCode
			output.Errors[shotID] = "New API 账户余额不足"
			output.Status = "failed"
			return output, nil
		}
		output.SucceededShotIDs = append(output.SucceededShotIDs, input.Plan.TargetShotIDs...)
		output.Status = "succeeded"
		return output, nil
	}, workflow.RegisterOptions{Name: "EpisodeVideoProductionWorkflow"})
	failedCheckpoints := make([]string, 0, 1)
	env.RegisterActivityWithOptions(func(
		_ context.Context,
		input FailEpisodeVideoProductionCheckpointInput,
	) error {
		failedCheckpoints = append(failedCheckpoints, input.Plan.CheckpointID)
		if input.FailureCode != billingInsufficientBalanceCode {
			t.Fatalf("checkpoint failure = %+v", input)
		}
		return nil
	}, activity.RegisterOptions{Name: "FailEpisodeVideoProductionCheckpoint"})
	var completed BatchShotProductionOutput
	env.RegisterActivityWithOptions(func(
		_ context.Context,
		_ TextToStoryboardInput,
		output BatchShotProductionOutput,
	) error {
		completed = output
		return nil
	}, activity.RegisterOptions{Name: "CompleteBatchShotProductionWorkflow"})

	env.ExecuteWorkflow(EpisodeBatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: mustJSON(BatchShotProductionOptions{
			ShotIDs: shotIDs, MaxConcurrency: DefaultShotVideoConcurrency,
		}),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if len(startedPlans) != defaultEpisodeWorkflowParallel {
		t.Fatalf("started plans = %v, want only initial episode window", startedPlans)
	}
	expectedFailedCheckpoints := make([]string, 0, len(plans)-defaultEpisodeWorkflowParallel)
	for _, plan := range plans[defaultEpisodeWorkflowParallel:] {
		expectedFailedCheckpoints = append(expectedFailedCheckpoints, plan.CheckpointID)
	}
	if fmt.Sprint(failedCheckpoints) != fmt.Sprint(expectedFailedCheckpoints) {
		t.Fatalf("failed checkpoints = %v, want %v", failedCheckpoints, expectedFailedCheckpoints)
	}
	if completed.ErrorCodes[shotIDs[10]] != billingInsufficientBalanceCode ||
		!strings.Contains(completed.Errors[shotIDs[10]], "未发起") {
		t.Fatalf("unstarted shot output = %+v", completed)
	}
}

func TestEpisodeBatchGenerateShotVideosWorkflowWaitsForChildCancellationCleanup(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	plan := episodeVideoTestPlan(0, "shot-1")
	env.RegisterActivityWithOptions(func(context.Context, PrepareEpisodeVideoProductionsInput) ([]EpisodeVideoProductionPlan, error) {
		return []EpisodeVideoProductionPlan{plan}, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductions"})
	childCleaned := false
	env.RegisterActivityWithOptions(func(context.Context) error {
		childCleaned = true
		return nil
	}, activity.RegisterOptions{Name: "MarkEpisodeChildCancellationCleanup"})
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, _ EpisodeVideoProductionInput) (result BatchShotProductionOutput, resultErr error) {
		defer func() {
			if !isWorkflowCancellationError(resultErr) {
				return
			}
			cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
			cleanupCtx = workflow.WithActivityOptions(cleanupCtx, defaultActivityOptions())
			_ = workflow.ExecuteActivity(cleanupCtx, "MarkEpisodeChildCancellationCleanup").Get(cleanupCtx, nil)
		}()
		resultErr = workflow.Sleep(ctx, time.Hour)
		return result, resultErr
	}, workflow.RegisterOptions{Name: "EpisodeVideoProductionWorkflow"})
	parentFinalized := false
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output BatchShotProductionOutput) error {
		if !childCleaned {
			t.Fatal("parent cancellation finalized before child cleanup completed")
		}
		if output.Status != "cancelled" {
			t.Fatalf("parent cancellation output = %+v", output)
		}
		parentFinalized = true
		return nil
	}, activity.RegisterOptions{Name: "FinalizeBatchShotProductionCancellation"})
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)

	env.ExecuteWorkflow(EpisodeBatchGenerateShotVideosWorkflow, TextToStoryboardInput{
		OrganizationID: "org", ProjectID: "project", WorkflowRunID: "workflow", CreatedBy: "user",
		Input: json.RawMessage(`{"shotIds":["shot-1"]}`),
	})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() == nil {
		t.Fatalf("workflow completed=%v error=%v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if !childCleaned || !parentFinalized {
		t.Fatalf("childCleaned=%v parentFinalized=%v", childCleaned, parentFinalized)
	}
}

func TestEpisodeVideoProductionWorkflowDrainsAndCommitsBeforeCompactContinueAsNew(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	prepareCalls := 0
	commitCalls := 0
	childCompleted := map[int]bool{}
	env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionInput) (EpisodeVideoProductionBatch, error) {
		ordinal := prepareCalls
		prepareCalls++
		shotID := fmt.Sprintf("shot-%d", ordinal+1)
		return EpisodeVideoProductionBatch{
			BatchID: fmt.Sprintf("batch-%d", ordinal), CheckpointID: "checkpoint", Ordinal: ordinal,
			DependencyHash: fmt.Sprintf("%064d", ordinal+1), Shots: []ShotVideoExecutionShot{{ShotID: shotID, ShotIndex: ordinal, ShotNo: ordinal + 1}},
		}, nil
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductionBatchV2"})
	env.RegisterWorkflowWithOptions(func(ctx workflow.Context, input SceneOrShotBatchInput) (BatchShotProductionOutput, error) {
		if err := workflow.Sleep(ctx, time.Minute); err != nil {
			return BatchShotProductionOutput{}, err
		}
		childCompleted[input.Batch.Ordinal] = true
		output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID}, shotExecutionIDs(input.Batch.Shots))
		output.SucceededShotIDs = append(output.SucceededShotIDs, output.TargetShotIDs...)
		output.Status = "succeeded"
		return output, nil
	}, workflow.RegisterOptions{Name: "SceneOrShotBatchWorkflow"})
	env.RegisterActivityWithOptions(func(_ context.Context, input CommitEpisodeVideoProductionBatchInput) (CommitEpisodeVideoProductionBatchOutput, error) {
		if !childCompleted[input.Batch.Ordinal] {
			t.Fatalf("batch %d committed before child drain", input.Batch.Ordinal)
		}
		commitCalls++
		return CommitEpisodeVideoProductionBatchOutput{HasMore: true, Status: "running"}, nil
	}, activity.RegisterOptions{Name: "CommitEpisodeVideoProductionBatchV2"})

	input := EpisodeVideoProductionInput{Plan: episodeVideoTestPlan(0, "shot-1", "shot-2", "shot-3", "shot-4", "shot-5")}
	env.ExecuteWorkflow(EpisodeVideoProductionWorkflow, input)

	if !env.IsWorkflowCompleted() || !workflow.IsContinueAsNewError(env.GetWorkflowError()) {
		t.Fatalf("workflow error = %v, want ContinueAsNew", env.GetWorkflowError())
	}
	if prepareCalls != maximumEpisodeBatchesPerRun || commitCalls != maximumEpisodeBatchesPerRun {
		t.Fatalf("prepare=%d commit=%d, want %d", prepareCalls, commitCalls, maximumEpisodeBatchesPerRun)
	}
	var continueErr *workflow.ContinueAsNewError
	if !errors.As(env.GetWorkflowError(), &continueErr) {
		t.Fatalf("workflow error type = %T", env.GetWorkflowError())
	}
	var continued EpisodeVideoProductionInput
	if err := converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &continued); err != nil {
		t.Fatalf("decode ContinueAsNew input: %v", err)
	}
	if continued.Plan.CheckpointID != input.Plan.CheckpointID || len(continued.Plan.TargetShotIDs) != 0 {
		t.Fatalf("continued input is not compact: %+v", continued.Plan)
	}
}

func TestEpisodeVideoProductionWorkflowFinalizesCheckpointAfterNonCancellationFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	plan := episodeVideoTestPlan(0, "shot-1")
	env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionInput) (EpisodeVideoProductionBatch, error) {
		return EpisodeVideoProductionBatch{}, temporal.NewNonRetryableApplicationError("render plan is stale", provider.CodeRenderPlanReplanRequired, nil)
	}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductionBatchV2"})
	var failure FailEpisodeVideoProductionCheckpointInput
	env.RegisterActivityWithOptions(func(_ context.Context, input FailEpisodeVideoProductionCheckpointInput) error {
		failure = input
		return nil
	}, activity.RegisterOptions{Name: "FailEpisodeVideoProductionCheckpoint"})

	env.ExecuteWorkflow(EpisodeVideoProductionWorkflow, EpisodeVideoProductionInput{Plan: plan})

	if env.GetWorkflowError() == nil {
		t.Fatal("workflow unexpectedly succeeded")
	}
	if failure.Plan.CheckpointID != plan.CheckpointID || failure.FailureCode != provider.CodeRenderPlanReplanRequired || failure.FailureMessage == "" {
		t.Fatalf("failure finalizer input = %+v", failure)
	}
}

func TestEpisodeVideoProductionWorkflowSeventyMinuteLoadUsesBoundedContinueAsNew(t *testing.T) {
	const (
		totalShots = 70 * 60 / 5
		batchSize  = maximumEpisodeVideoBatchSize
	)
	nextShot := 0
	batchOrdinal := 0
	runCount := 0
	input := EpisodeVideoProductionInput{Plan: episodeVideoTestPlan(0, "initial-shot-selection")}

	for {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		batchesThisRun := 0
		env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionInput) (EpisodeVideoProductionBatch, error) {
			if nextShot >= totalShots {
				return EpisodeVideoProductionBatch{Done: true, FinalOutput: BatchShotProductionOutput{Status: "succeeded"}}, nil
			}
			end := nextShot + batchSize
			if end > totalShots {
				end = totalShots
			}
			shots := make([]ShotVideoExecutionShot, 0, end-nextShot)
			for index := nextShot; index < end; index++ {
				shots = append(shots, ShotVideoExecutionShot{ShotID: fmt.Sprintf("shot-%04d", index+1), ShotIndex: index, ShotNo: index + 1})
			}
			batch := EpisodeVideoProductionBatch{
				BatchID: fmt.Sprintf("batch-%04d", batchOrdinal), CheckpointID: input.Plan.CheckpointID,
				Ordinal: batchOrdinal, DependencyHash: fmt.Sprintf("%064d", batchOrdinal+1), Shots: shots,
			}
			nextShot = end
			batchOrdinal++
			batchesThisRun++
			return batch, nil
		}, activity.RegisterOptions{Name: "PrepareEpisodeVideoProductionBatchV2"})
		env.RegisterWorkflowWithOptions(func(_ workflow.Context, batchInput SceneOrShotBatchInput) (BatchShotProductionOutput, error) {
			output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: batchInput.Plan.WorkflowRunID}, shotExecutionIDs(batchInput.Batch.Shots))
			output.SucceededShotIDs = append(output.SucceededShotIDs, output.TargetShotIDs...)
			output.Status = "succeeded"
			return output, nil
		}, workflow.RegisterOptions{Name: "SceneOrShotBatchWorkflow"})
		env.RegisterActivityWithOptions(func(_ context.Context, commitInput CommitEpisodeVideoProductionBatchInput) (CommitEpisodeVideoProductionBatchOutput, error) {
			if len(commitInput.Output.SucceededShotIDs) != len(commitInput.Batch.Shots) {
				t.Fatalf("batch %d succeeded shots = %d, want %d", commitInput.Batch.Ordinal, len(commitInput.Output.SucceededShotIDs), len(commitInput.Batch.Shots))
			}
			hasMore := nextShot < totalShots
			return CommitEpisodeVideoProductionBatchOutput{HasMore: hasMore, Status: "running", FinalOutput: BatchShotProductionOutput{Status: "succeeded"}}, nil
		}, activity.RegisterOptions{Name: "CommitEpisodeVideoProductionBatchV2"})
		env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionPlan) error {
			return nil
		}, activity.RegisterOptions{Name: "ReconcileEpisodeVideoProductionCheckpointV2"})
		env.RegisterActivityWithOptions(func(context.Context, EpisodeVideoProductionPlan) (BatchShotProductionOutput, error) {
			output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID}, nil)
			for index := 0; index < totalShots; index++ {
				shotID := fmt.Sprintf("shot-%04d", index+1)
				output.TargetShotIDs = append(output.TargetShotIDs, shotID)
				output.SucceededShotIDs = append(output.SucceededShotIDs, shotID)
			}
			output.Status = "succeeded"
			return output, nil
		}, activity.RegisterOptions{Name: "LoadEpisodeVideoProductionOutputV2"})

		env.ExecuteWorkflow(EpisodeVideoProductionWorkflow, input)
		runCount++
		if batchesThisRun > maximumEpisodeBatchesPerRun {
			t.Fatalf("run %d processed %d batches, maximum %d", runCount, batchesThisRun, maximumEpisodeBatchesPerRun)
		}
		workflowErr := env.GetWorkflowError()
		if nextShot >= totalShots {
			if workflowErr != nil {
				t.Fatalf("final run failed: %v", workflowErr)
			}
			break
		}
		if !workflow.IsContinueAsNewError(workflowErr) {
			t.Fatalf("run %d error = %v, want ContinueAsNew", runCount, workflowErr)
		}
		var continueErr *workflow.ContinueAsNewError
		if !errors.As(workflowErr, &continueErr) {
			t.Fatalf("run %d error type = %T", runCount, workflowErr)
		}
		var continued EpisodeVideoProductionInput
		if err := converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &continued); err != nil {
			t.Fatalf("decode run %d ContinueAsNew input: %v", runCount, err)
		}
		input = continued
		if len(input.Plan.TargetShotIDs) != 0 {
			t.Fatalf("run %d carried %d target shot IDs", runCount, len(input.Plan.TargetShotIDs))
		}
	}

	wantBatches := (totalShots + batchSize - 1) / batchSize
	wantRuns := (wantBatches + maximumEpisodeBatchesPerRun - 1) / maximumEpisodeBatchesPerRun
	if nextShot != totalShots || batchOrdinal != wantBatches || runCount != wantRuns {
		t.Fatalf("load result shots=%d/%d batches=%d/%d runs=%d/%d", nextShot, totalShots, batchOrdinal, wantBatches, runCount, wantRuns)
	}
}

func TestSceneOrShotBatchWorkflowUsesBoundedConcurrentActivitiesWithoutPromptAgents(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
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
		return LoadPreparedShotVideoPlanOutput{}, temporal.NewNonRetryableApplicationError(
			"prepared plan unavailable", provider.CodeRenderPlanReplanRequired, nil,
		)
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	env.RegisterActivityWithOptions(func(context.Context, PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
		t.Fatal("video execution must not call prompt agents")
		return PrepareShotVideoPromptOutput{}, nil
	}, activity.RegisterOptions{Name: "PrepareShotVideoPrompt"})
	shots := make([]ShotVideoExecutionShot, defaultEpisodeVideoBatchSize)
	for index := range shots {
		shots[index] = ShotVideoExecutionShot{ShotID: fmt.Sprintf("shot-%d", index+1), ShotIndex: index, ShotNo: index + 1}
	}
	env.ExecuteWorkflow(SceneOrShotBatchWorkflow, SceneOrShotBatchInput{
		Plan:    episodeVideoTestPlan(0, shotExecutionIDs(shots)...),
		Batch:   EpisodeVideoProductionBatch{BatchID: "batch", CheckpointID: "checkpoint", Shots: shots},
		Options: BatchShotProductionOptions{MaxPolls: 1},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	var output BatchShotProductionOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if maxActive != defaultEpisodeVideoBatchSize {
		t.Fatalf("max active shot activities = %d, want %d", maxActive, defaultEpisodeVideoBatchSize)
	}
	if output.Status != "failed" || len(output.FailedShotIDs) != defaultEpisodeVideoBatchSize {
		t.Fatalf("output = %+v", output)
	}
}

func TestSceneOrShotBatchWorkflowUsesV2MaterializeActivityForOperationItems(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var calledMu sync.Mutex
	called := false
	env.RegisterActivityWithOptions(func(_ context.Context, input EnsurePreparedShotVideoPlanV2Input) (LoadPreparedShotVideoPlanOutput, error) {
		calledMu.Lock()
		called = true
		calledMu.Unlock()
		if input.OperationID != "checkpoint-1" || input.OperationItemID != "item-1" || input.OperationItemAttempt != 2 {
			t.Fatalf("v2 materialize input = %+v", input)
		}
		return LoadPreparedShotVideoPlanOutput{}, temporal.NewNonRetryableApplicationError(
			"prepared plan unavailable", provider.CodeRenderPlanReplanRequired, nil,
		)
	}, activity.RegisterOptions{Name: "MaterializeAndBindExecutableShotVideoPlanV2"})
	env.RegisterActivityWithOptions(func(context.Context, EnsurePreparedShotVideoPlanInput) (LoadPreparedShotVideoPlanOutput, error) {
		t.Fatal("v2 operation item must not use the legacy EnsurePreparedShotVideoPlan activity")
		return LoadPreparedShotVideoPlanOutput{}, nil
	}, activity.RegisterOptions{Name: "EnsurePreparedShotVideoPlan"})
	shot := ShotVideoExecutionShot{
		ShotID: "shot-1", ShotIndex: 0, ShotNo: 1,
		OperationItemID: "item-1", OperationItemAttempt: 2,
	}
	plan := episodeVideoTestPlan(1, shot.ShotID)
	plan.CheckpointID = "checkpoint-1"
	env.ExecuteWorkflow(SceneOrShotBatchWorkflow, SceneOrShotBatchInput{
		Plan: plan,
		Batch: EpisodeVideoProductionBatch{
			BatchID: "batch-1", CheckpointID: plan.CheckpointID, Shots: []ShotVideoExecutionShot{shot},
		},
		Options: BatchShotProductionOptions{MaxPolls: 1},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	calledMu.Lock()
	wasCalled := called
	calledMu.Unlock()
	if !wasCalled {
		t.Fatal("v2 materialize activity was not called")
	}
	var output BatchShotProductionOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.Status != "failed" || len(output.FailedShotIDs) != 1 {
		t.Fatalf("output = %+v", output)
	}
}

func episodeVideoTestPlan(index int, shotIDs ...string) EpisodeVideoProductionPlan {
	return EpisodeVideoProductionPlan{
		CheckpointID: fmt.Sprintf("checkpoint-%d", index), OrganizationID: "org", ProjectID: "project",
		WorkflowRunID: "workflow", CreatedBy: "user", ScriptEpisodeID: fmt.Sprintf("episode-%d", index),
		EpisodeIndex: index, ProductionGenerationID: "generation", VideoProductionBindingID: "binding",
		VideoProductionBindingRevision: 1, ProductionProfileVersionID: "profile-version",
		ProductionProfileSnapshotHash: fmt.Sprintf("%064d", index+1),
		TemporalWorkflowID:            fmt.Sprintf("episode-workflow-%d", index), TargetShotIDs: append([]string(nil), shotIDs...),
	}
}
