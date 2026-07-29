package workflows

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type shotVideoPromptResult struct {
	Outputs []PrepareShotVideoPromptOutput
	Err     error
}

func generateShotVideoPromptsConcurrently(
	ctx workflow.Context,
	promptCtx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
) ([]shotVideoPromptResult, error) {
	results := make([]shotVideoPromptResult, len(options.ShotIDs))
	if len(options.ShotIDs) == 0 {
		return results, nil
	}

	limit := clampConcurrency(options.MaxConcurrency, DefaultShotVideoPromptConcurrency, MaxShotVideoPromptConcurrency)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	schedule := func(index int) {
		shotID := options.ShotIDs[index]
		future := workflow.ExecuteActivity(promptCtx, "PrepareShotVideoPrompt", PrepareShotVideoPromptInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shotID,
			WorkflowPrompt: "batch_generate_shot_video_prompts",
			Duration:       options.Duration,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
			Force:          options.Force,
			PromptOnly:     true,
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var output PrepareShotVideoPromptOutput
			err := completed.Get(ctx, &output)
			results[index] = shotVideoPromptResult{Outputs: []PrepareShotVideoPromptOutput{output}, Err: err}
			if stopOnBalance && !stopScheduling {
				if code, message, ok := billingInsufficientBalanceFailure(err); ok {
					stopScheduling = true
					stopCode = code
					stopMessage = message
				}
			}
			inFlight--
		})
	}

	for nextIndex < len(options.ShotIDs) && inFlight < limit {
		schedule(nextIndex)
		nextIndex++
	}
	for inFlight > 0 {
		selector.Select(ctx)
		if err := ctx.Err(); isWorkflowCancellationError(err) {
			return nil, err
		}
		for !stopScheduling && nextIndex < len(options.ShotIDs) && inFlight < limit {
			schedule(nextIndex)
			nextIndex++
		}
	}
	if stopScheduling {
		code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
		for nextIndex < len(options.ShotIDs) {
			results[nextIndex].Err = billingInsufficientBalanceError(code, message)
			nextIndex++
		}
	}
	return results, nil
}

func generateShotVideoPromptPlansConcurrently(
	ctx workflow.Context,
	promptCtx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
) ([]shotVideoPromptResult, error) {
	if workflow.GetVersion(ctx, "batch-video-prompt-resume-approved-plan-v2", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		return generateResumableShotVideoPromptPlansConcurrently(ctx, input, options)
	}
	results, err := generateShotVideoPromptsConcurrently(ctx, promptCtx, input, options)
	if err != nil {
		return nil, err
	}
	if workflow.GetVersion(ctx, "batch-video-prompt-segment-agents-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		return generateShotVideoSegmentPromptPlansConcurrently(ctx, input, options, results)
	}
	planResults, err := materializeApprovedShotVideoPlansConcurrently(ctx, input, options, results)
	if err != nil {
		return nil, err
	}
	for index := range results {
		if results[index].Err == nil && planResults[index] != nil {
			results[index].Err = planResults[index]
		}
	}
	return results, nil
}

func generateResumableShotVideoPromptPlansConcurrently(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
) ([]shotVideoPromptResult, error) {
	results := make([]shotVideoPromptResult, len(options.ShotIDs))
	limit := clampConcurrency(options.MaxConcurrency, DefaultShotVideoPromptConcurrency, MaxShotVideoPromptConcurrency)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for start := 0; start < len(options.ShotIDs); start += limit {
		end := start + limit
		if end > len(options.ShotIDs) {
			end = len(options.ShotIDs)
		}
		completed := workflow.NewBufferedChannel(ctx, end-start)
		for index := start; index < end; index++ {
			currentIndex := index
			workflow.Go(ctx, func(shotCtx workflow.Context) {
				outputs, prepareErr := prepareResumableShotVideoPromptPlan(
					shotCtx, input, options, options.ShotIDs[currentIndex],
				)
				completed.SendAsync(shotVideoPromptPlanCompletion{Index: currentIndex, Outputs: outputs, Err: prepareErr})
			})
		}
		drainCtx, _ := workflow.NewDisconnectedContext(ctx)
		cancelled := false
		for index := start; index < end; index++ {
			var completion shotVideoPromptPlanCompletion
			completed.Receive(drainCtx, &completion)
			results[completion.Index] = shotVideoPromptResult{Outputs: completion.Outputs, Err: completion.Err}
			if isWorkflowCancellationError(completion.Err) {
				cancelled = true
			}
			if stopOnBalance && !stopScheduling {
				if code, message, ok := billingInsufficientBalanceFailure(completion.Err); ok {
					stopScheduling = true
					stopCode = code
					stopMessage = message
				}
			}
		}
		if cancelled || isWorkflowCancellationError(ctx.Err()) {
			return nil, temporal.NewCanceledError("batch video prompt planning was cancelled")
		}
		if stopScheduling {
			code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
			for index := end; index < len(options.ShotIDs); index++ {
				results[index].Err = billingInsufficientBalanceError(code, message)
			}
			break
		}
	}
	return results, nil
}

func generateShotVideoSegmentPromptPlansConcurrently(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	results []shotVideoPromptResult,
) ([]shotVideoPromptResult, error) {
	limit := clampConcurrency(options.MaxConcurrency, DefaultShotVideoPromptConcurrency, MaxShotVideoPromptConcurrency)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for start := 0; start < len(options.ShotIDs); start += limit {
		end := start + limit
		if end > len(options.ShotIDs) {
			end = len(options.ShotIDs)
		}
		completed := workflow.NewBufferedChannel(ctx, end-start)
		started := 0
		for index := start; index < end; index++ {
			if results[index].Err != nil {
				continue
			}
			started++
			currentIndex := index
			workflow.Go(ctx, func(shotCtx workflow.Context) {
				outputs, prepareErr := prepareShotVideoSegmentPromptPlan(
					shotCtx, input, options, options.ShotIDs[currentIndex],
				)
				completed.SendAsync(shotVideoPromptPlanCompletion{Index: currentIndex, Outputs: outputs, Err: prepareErr})
			})
		}
		drainCtx, _ := workflow.NewDisconnectedContext(ctx)
		cancelled := false
		for index := 0; index < started; index++ {
			var completion shotVideoPromptPlanCompletion
			completed.Receive(drainCtx, &completion)
			if isWorkflowCancellationError(completion.Err) {
				cancelled = true
			}
			if completion.Err != nil {
				results[completion.Index].Err = completion.Err
				if stopOnBalance && !stopScheduling {
					if code, message, ok := billingInsufficientBalanceFailure(completion.Err); ok {
						stopScheduling = true
						stopCode = code
						stopMessage = message
					}
				}
				continue
			}
			results[completion.Index].Outputs = append(results[completion.Index].Outputs, completion.Outputs...)
		}
		if cancelled || isWorkflowCancellationError(ctx.Err()) {
			return nil, temporal.NewCanceledError("batch video prompt segment planning was cancelled")
		}
		if stopScheduling {
			code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
			for index := end; index < len(options.ShotIDs); index++ {
				if results[index].Err == nil {
					results[index].Err = billingInsufficientBalanceError(code, message)
				}
			}
			break
		}
	}
	return results, nil
}

type shotVideoPromptPlanCompletion struct {
	Index   int
	Outputs []PrepareShotVideoPromptOutput
	Err     error
}

func prepareShotVideoSegmentPromptPlan(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	shotID string,
) ([]PrepareShotVideoPromptOutput, error) {
	var plan PlanShotVideoOutput
	if err := workflow.ExecuteActivity(ctx, "PlanShotVideo", PlanShotVideoInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, WorkflowPrompt: "batch_generate_shot_video_prompts",
		FailureScope: workflowFailureScopeBatchItem, ShotID: shotID,
		AspectRatio: options.AspectRatio, Resolution: options.Resolution,
		AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement,
		Force: true, PromptOnly: true,
	}).Get(ctx, &plan); err != nil {
		return nil, err
	}
	return prepareShotVideoSegmentPromptsFromPlan(ctx, input, options, shotID, plan, nil)
}

func prepareResumableShotVideoPromptPlan(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	shotID string,
) ([]PrepareShotVideoPromptOutput, error) {
	var plan PlanShotVideoOutput
	planInput := PlanShotVideoInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, WorkflowPrompt: "batch_generate_shot_video_prompts",
		FailureScope: workflowFailureScopeBatchItem, ShotID: shotID,
		AspectRatio: options.AspectRatio, Resolution: options.Resolution,
		AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement,
		Force: true, PromptOnly: true,
	}
	planErr := workflow.ExecuteActivity(ctx, "PlanShotVideo", planInput).Get(ctx, &plan)
	outputs := make([]PrepareShotVideoPromptOutput, 0, 2)
	if planErr != nil {
		if !isPreparedVideoPromptPlanError(planErr) {
			return nil, planErr
		}
		promptCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
		var coarse PrepareShotVideoPromptOutput
		if err := workflow.ExecuteActivity(promptCtx, "PrepareShotVideoPrompt", PrepareShotVideoPromptInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shotID,
			WorkflowPrompt: "batch_generate_shot_video_prompts",
			Duration:       options.Duration,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
			Force:          options.Force,
			PromptOnly:     true,
		}).Get(promptCtx, &coarse); err != nil {
			return outputs, err
		}
		outputs = append(outputs, coarse)
		if err := workflow.ExecuteActivity(ctx, "PlanShotVideo", planInput).Get(ctx, &plan); err != nil {
			return outputs, err
		}
	}
	return prepareShotVideoSegmentPromptsFromPlan(ctx, input, options, shotID, plan, outputs)
}

func prepareShotVideoSegmentPromptsFromPlan(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	shotID string,
	plan PlanShotVideoOutput,
	outputs []PrepareShotVideoPromptOutput,
) ([]PrepareShotVideoPromptOutput, error) {
	promptCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	if len(plan.Segments) == 0 {
		return outputs, preparedVideoPromptError("视频执行计划没有可生成提示词的片段")
	}
	if outputs == nil {
		outputs = make([]PrepareShotVideoPromptOutput, 0, len(plan.Segments))
	}
	for _, segment := range plan.Segments {
		var output PrepareShotVideoPromptOutput
		err := workflow.ExecuteActivity(promptCtx, "PrepareShotVideoPrompt", PrepareShotVideoPromptInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			CreatedBy: input.CreatedBy, ShotID: shotID,
			WorkflowPrompt: "batch_generate_shot_video_prompts", PromptOnly: true, Force: options.Force,
			Duration: segment.RequestedDurationSeconds, RequestedDuration: segment.RequestedDurationSeconds,
			AspectRatio: options.AspectRatio, Resolution: options.Resolution,
			ExecutionPlanID: plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
			SegmentIndex: segment.SegmentIndex, SegmentCount: len(plan.Segments),
			SegmentStartTick: segment.PlannedStartTick, SegmentEndTick: segment.PlannedEndTick,
			AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement,
			DialogueLines: renderSegmentDialogueLines(segment.DialogueSpans),
		}).Get(promptCtx, &output)
		if err != nil {
			return outputs, err
		}
		outputs = append(outputs, output)
	}
	if err := workflow.ExecuteActivity(ctx, "FinalizeShotVideoPromptPlan", FinalizeShotVideoPromptPlanInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		ShotID: shotID, ExecutionPlanID: plan.ExecutionPlanID, PromptSource: "segment_video_prompt_agents",
	}).Get(ctx, nil); err != nil {
		return outputs, err
	}
	return outputs, nil
}

func materializeApprovedShotVideoPlansConcurrently(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	promptResults []shotVideoPromptResult,
) ([]error, error) {
	results := make([]error, len(options.ShotIDs))
	limit := clampConcurrency(options.MaxConcurrency, DefaultShotVideoPromptConcurrency, MaxShotVideoPromptConcurrency)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	scheduleNext := func() bool {
		for nextIndex < len(options.ShotIDs) && promptResults[nextIndex].Err != nil {
			nextIndex++
		}
		if nextIndex >= len(options.ShotIDs) {
			return false
		}
		index := nextIndex
		nextIndex++
		future := workflow.ExecuteActivity(ctx, "EnsurePreparedShotVideoPlan", EnsurePreparedShotVideoPlanInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			CreatedBy: input.CreatedBy, WorkflowPrompt: "batch_generate_shot_video_prompts",
			ShotID: options.ShotIDs[index], AspectRatio: options.AspectRatio, Resolution: options.Resolution,
			AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement,
			Force: true, PromptOnly: true,
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var prepared LoadPreparedShotVideoPlanOutput
			results[index] = completed.Get(ctx, &prepared)
			if stopOnBalance && !stopScheduling {
				if code, message, ok := billingInsufficientBalanceFailure(results[index]); ok {
					stopScheduling = true
					stopCode = code
					stopMessage = message
				}
			}
			inFlight--
		})
		return true
	}
	for inFlight < limit && scheduleNext() {
	}
	for inFlight > 0 {
		selector.Select(ctx)
		if err := ctx.Err(); isWorkflowCancellationError(err) {
			return nil, err
		}
		for !stopScheduling && inFlight < limit && scheduleNext() {
		}
	}
	if stopScheduling {
		code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
		for nextIndex < len(options.ShotIDs) {
			if promptResults[nextIndex].Err == nil {
				results[nextIndex] = billingInsufficientBalanceError(code, message)
			}
			nextIndex++
		}
	}
	return results, nil
}
