package workflows

import (
	"fmt"

	"github.com/Einzieg/cineweave/internal/provider"
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
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
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
			inFlight--
		})
	}

	for nextIndex < len(options.ShotIDs) && inFlight < limit {
		schedule(nextIndex)
		nextIndex++
	}
	for inFlight > 0 {
		selector.Select(ctx)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for nextIndex < len(options.ShotIDs) && inFlight < limit {
			schedule(nextIndex)
			nextIndex++
		}
	}
	return results, nil
}

type shotVideoPlanResult struct {
	Plan PlanShotVideoOutput
	Err  error
}

type shotVideoSegmentPromptRequest struct {
	ShotPosition int
	ShotID       string
	Plan         PlanShotVideoOutput
	Segment      provider.GatewayVideoPlanSegment
}

type shotVideoSegmentPromptResult struct {
	Output PrepareShotVideoPromptOutput
	Err    error
}

func generateShotVideoPromptPlansConcurrently(
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
	plans, err := planShotVideoPromptsConcurrently(ctx, input, options, limit)
	if err != nil {
		return nil, err
	}
	requests := make([]shotVideoSegmentPromptRequest, 0)
	for shotPosition, planned := range plans {
		if planned.Err != nil {
			results[shotPosition].Err = planned.Err
			continue
		}
		if len(planned.Plan.Segments) == 0 {
			results[shotPosition].Err = fmt.Errorf("video render plan has no segments")
			continue
		}
		for _, segment := range planned.Plan.Segments {
			requests = append(requests, shotVideoSegmentPromptRequest{
				ShotPosition: shotPosition,
				ShotID:       options.ShotIDs[shotPosition],
				Plan:         planned.Plan,
				Segment:      segment,
			})
		}
	}
	segmentResults, err := prepareVideoSegmentPromptsConcurrently(ctx, promptCtx, input, options, requests, limit)
	if err != nil {
		return nil, err
	}
	for index, segmentResult := range segmentResults {
		request := requests[index]
		if segmentResult.Err != nil {
			if results[request.ShotPosition].Err == nil {
				results[request.ShotPosition].Err = segmentResult.Err
			}
			continue
		}
		results[request.ShotPosition].Outputs = append(results[request.ShotPosition].Outputs, segmentResult.Output)
	}
	for shotPosition, planned := range plans {
		if planned.Err != nil || results[shotPosition].Err != nil {
			continue
		}
		if err := workflow.ExecuteActivity(ctx, "FinalizeShotVideoPromptPlan", FinalizeShotVideoPromptPlanInput{
			OrganizationID:  input.OrganizationID,
			ProjectID:       input.ProjectID,
			WorkflowRunID:   input.WorkflowRunID,
			ShotID:          options.ShotIDs[shotPosition],
			ExecutionPlanID: planned.Plan.ExecutionPlanID,
		}).Get(ctx, nil); err != nil {
			results[shotPosition].Err = err
		}
	}
	return results, nil
}

func planShotVideoPromptsConcurrently(
	ctx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	limit int,
) ([]shotVideoPlanResult, error) {
	results := make([]shotVideoPlanResult, len(options.ShotIDs))
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
	schedule := func(index int) {
		future := workflow.ExecuteActivity(ctx, "PlanShotVideo", PlanShotVideoInput{
			OrganizationID:   input.OrganizationID,
			ProjectID:        input.ProjectID,
			WorkflowRunID:    input.WorkflowRunID,
			CreatedBy:        input.CreatedBy,
			WorkflowPrompt:   "batch_generate_shot_video_prompts",
			ShotID:           options.ShotIDs[index],
			AspectRatio:      options.AspectRatio,
			Resolution:       options.Resolution,
			AudioStrategy:    options.AudioStrategy,
			AudioRequirement: options.AudioRequirement,
			Force:            options.Force,
			PromptOnly:       true,
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var plan PlanShotVideoOutput
			results[index] = shotVideoPlanResult{Plan: plan, Err: completed.Get(ctx, &plan)}
			results[index].Plan = plan
			inFlight--
		})
	}
	for nextIndex < len(options.ShotIDs) && inFlight < limit {
		schedule(nextIndex)
		nextIndex++
	}
	for inFlight > 0 {
		selector.Select(ctx)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for nextIndex < len(options.ShotIDs) && inFlight < limit {
			schedule(nextIndex)
			nextIndex++
		}
	}
	return results, nil
}

func prepareVideoSegmentPromptsConcurrently(
	ctx workflow.Context,
	promptCtx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
	requests []shotVideoSegmentPromptRequest,
	limit int,
) ([]shotVideoSegmentPromptResult, error) {
	results := make([]shotVideoSegmentPromptResult, len(requests))
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
	schedule := func(index int) {
		request := requests[index]
		segment := request.Segment
		future := workflow.ExecuteActivity(promptCtx, "PrepareShotVideoPrompt", PrepareShotVideoPromptInput{
			OrganizationID:    input.OrganizationID,
			ProjectID:         input.ProjectID,
			WorkflowRunID:     input.WorkflowRunID,
			CreatedBy:         input.CreatedBy,
			ShotID:            request.ShotID,
			WorkflowPrompt:    "batch_generate_shot_video_prompts",
			Duration:          segment.PlannedDurationSeconds,
			RequestedDuration: segment.RequestedDurationSeconds,
			AspectRatio:       options.AspectRatio,
			Resolution:        options.Resolution,
			Force:             options.Force,
			PromptOnly:        true,
			ExecutionPlanID:   request.Plan.ExecutionPlanID,
			RenderSegmentID:   segment.SegmentID,
			SegmentIndex:      segment.SegmentIndex,
			SegmentCount:      len(request.Plan.Segments),
			SegmentStartTick:  segment.PlannedStartTick,
			SegmentEndTick:    segment.PlannedEndTick,
			AudioStrategy:     request.Plan.AudioStrategy,
			AudioRequirement:  request.Plan.AudioRequirement,
			DialogueLines:     renderSegmentDialogueLines(segment.DialogueSpans),
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var output PrepareShotVideoPromptOutput
			results[index] = shotVideoSegmentPromptResult{Output: output, Err: completed.Get(ctx, &output)}
			results[index].Output = output
			inFlight--
		})
	}
	for nextIndex < len(requests) && inFlight < limit {
		schedule(nextIndex)
		nextIndex++
	}
	for inFlight > 0 {
		selector.Select(ctx)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for nextIndex < len(requests) && inFlight < limit {
			schedule(nextIndex)
			nextIndex++
		}
	}
	return results, nil
}
