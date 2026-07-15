package workflows

import "go.temporal.io/sdk/workflow"

func prepareShotImagePromptsForProduction(
	ctx workflow.Context,
	input TextToStoryboardInput,
	shots []StoryboardShotRecord,
	aspectRatio string,
	maxConcurrency int,
) error {
	if len(shots) == 0 {
		return nil
	}
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	promptCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	results, err := generateShotImagePromptsConcurrently(ctx, promptCtx, input, BatchShotProductionOptions{
		ShotIDs:        shotIDs,
		MaxConcurrency: clampConcurrency(maxConcurrency, DefaultShotImagePromptConcurrency, MaxShotImagePromptConcurrency),
		AspectRatio:    aspectRatio,
		Force:          false,
	})
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func prepareSingleShotImagePrompt(ctx workflow.Context, input TextToStoryboardInput, shotID, workflowPrompt, aspectRatio string) error {
	promptCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	return workflow.ExecuteActivity(promptCtx, "PrepareShotImagePrompt", PrepareShotImagePromptInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         shotID,
		WorkflowPrompt: workflowPrompt,
		AspectRatio:    aspectRatio,
		Force:          false,
	}).Get(promptCtx, nil)
}
