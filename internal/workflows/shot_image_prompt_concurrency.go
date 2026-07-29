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
	items []ShotAnchorWorkItem,
	maxConcurrency int,
	aspectRatio string,
	size string,
	force bool,
) ([]shotImagePromptResult, error) {
	results := make([]shotImagePromptResult, len(items))
	if len(items) == 0 {
		return results, nil
	}

	limit := clampConcurrency(maxConcurrency, DefaultShotImagePromptConcurrency, MaxShotImagePromptConcurrency)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	schedule := func(index int) {
		item := items[index]
		future := workflow.ExecuteActivity(promptCtx, "PrepareShotImagePrompt", PrepareShotImagePromptInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         item.ShotID,
			ShotIndex:      item.ShotIndex,
			ShotNo:         item.ShotNo,
			AnchorRole:     item.AnchorRole,
			WorkflowPrompt: "batch_generate_shot_image_prompts",
			AspectRatio:    aspectRatio,
			Size:           size,
			Force:          force,
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var output PrepareShotImagePromptOutput
			err := completed.Get(ctx, &output)
			results[index] = shotImagePromptResult{Output: output, Err: err}
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

	for nextIndex < len(items) && inFlight < limit {
		schedule(nextIndex)
		nextIndex++
	}
	for inFlight > 0 {
		selector.Select(ctx)
		if err := ctx.Err(); isWorkflowCancellationError(err) {
			return nil, err
		}
		for !stopScheduling && nextIndex < len(items) && inFlight < limit {
			schedule(nextIndex)
			nextIndex++
		}
	}
	if stopScheduling {
		code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
		for nextIndex < len(items) {
			results[nextIndex].Err = billingInsufficientBalanceError(code, message)
			nextIndex++
		}
	}
	return results, nil
}
