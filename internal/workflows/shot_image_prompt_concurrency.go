package workflows

import "go.temporal.io/sdk/workflow"

type shotImagePromptResult struct {
	Output PrepareShotImagePromptOutput
	Err    error
}

func generateShotImagePromptsConcurrently(
	ctx workflow.Context,
	promptCtx workflow.Context,
	input TextToStoryboardInput,
	options BatchShotProductionOptions,
) ([]shotImagePromptResult, error) {
	results := make([]shotImagePromptResult, len(options.ShotIDs))
	if len(options.ShotIDs) == 0 {
		return results, nil
	}

	limit := clampConcurrency(options.MaxConcurrency, DefaultShotImagePromptConcurrency, MaxShotImagePromptConcurrency)
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
	schedule := func(index int) {
		shotID := options.ShotIDs[index]
		future := workflow.ExecuteActivity(promptCtx, "PrepareShotImagePrompt", PrepareShotImagePromptInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shotID,
			WorkflowPrompt: "batch_generate_shot_image_prompts",
			AspectRatio:    options.AspectRatio,
			Size:           options.Resolution,
			Force:          options.Force,
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var output PrepareShotImagePromptOutput
			err := completed.Get(ctx, &output)
			results[index] = shotImagePromptResult{Output: output, Err: err}
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
