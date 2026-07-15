package workflows

import "go.temporal.io/sdk/workflow"

const (
	DefaultShotImageConcurrency       = 5
	MaxShotImageConcurrency           = 16
	DefaultShotImagePromptConcurrency = 5
	MaxShotImagePromptConcurrency     = 12
	DefaultShotVideoConcurrency       = 5
	MaxShotVideoConcurrency           = 8
	DefaultShotVideoPromptConcurrency = 5
	MaxShotVideoPromptConcurrency     = 12
)

type shotImageGenerationRequest struct {
	ShotID         string
	ShotIndex      int
	ShotNo         int
	WorkflowPrompt string
	AspectRatio    string
	Force          bool
}

type shotImageGenerationResult struct {
	Output GenerateShotImageOutput
	Err    error
}

// generateShotImagesConcurrently keeps the activity pipeline full while
// preserving input order in the returned results.
func generateShotImagesConcurrently(
	ctx workflow.Context,
	imageCtx workflow.Context,
	input TextToStoryboardInput,
	requests []shotImageGenerationRequest,
	maxConcurrency int,
) ([]shotImageGenerationResult, error) {
	results := make([]shotImageGenerationResult, len(requests))
	if len(requests) == 0 {
		return results, nil
	}

	limit := clampConcurrency(maxConcurrency, DefaultShotImageConcurrency, MaxShotImageConcurrency)
	selector := workflow.NewSelector(ctx)
	nextIndex := 0
	inFlight := 0

	schedule := func(index int) {
		request := requests[index]
		future := workflow.ExecuteActivity(imageCtx, "GenerateShotImage", GenerateShotImageInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         request.ShotID,
			ShotIndex:      request.ShotIndex,
			ShotNo:         request.ShotNo,
			WorkflowPrompt: request.WorkflowPrompt,
			AspectRatio:    request.AspectRatio,
			Force:          request.Force,
		})
		inFlight++
		selector.AddFuture(future, func(completed workflow.Future) {
			var output GenerateShotImageOutput
			err := completed.Get(ctx, &output)
			results[index] = shotImageGenerationResult{
				Output: output,
				Err:    err,
			}
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

func clampConcurrency(value, fallback, maximum int) int {
	if fallback < 1 {
		fallback = 1
	}
	if maximum < 1 {
		maximum = fallback
	}
	if value < 1 {
		value = fallback
	}
	if value > maximum {
		value = maximum
	}
	return value
}
