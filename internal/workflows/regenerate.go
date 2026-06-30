package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type RegenerationOptions struct {
	TargetID            string  `json:"targetId"`
	Force               bool    `json:"force"`
	Duration            float64 `json:"duration"`
	AspectRatio         string  `json:"aspectRatio"`
	Resolution          string  `json:"resolution"`
	PollIntervalSeconds int     `json:"pollIntervalSeconds"`
	MaxPolls            int     `json:"maxPolls"`
}

type RegenerationOutput struct {
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	Status     string          `json:"status"`
	Output     json.RawMessage `json:"output,omitempty"`
}

func RegenerateCanonicalAssetImageWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var image GenerateCanonicalAssetImageOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateCanonicalAssetImage", GenerateCanonicalAssetImageInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		AssetID:        options.TargetID,
	}).Get(ctx, &image); err != nil {
		return RegenerationOutput{}, err
	}
	output := RegenerationOutput{TargetType: "canonical_asset_image", TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(image)}
	if err := workflow.ExecuteActivity(ctx, "CompleteRegenerationWorkflow", input, output).Get(ctx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func RegenerateDerivedAssetImageWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var image GenerateDerivedAssetImageOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateDerivedAssetImage", GenerateDerivedAssetImageInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		RequirementID:  options.TargetID,
	}).Get(ctx, &image); err != nil {
		return RegenerationOutput{}, err
	}
	output := RegenerationOutput{TargetType: "derived_asset_image", TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(image)}
	if err := workflow.ExecuteActivity(ctx, "CompleteRegenerationWorkflow", input, output).Get(ctx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func RegenerateShotImageWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var image GenerateShotImageOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateShotImage", GenerateShotImageInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         options.TargetID,
		WorkflowPrompt: "regenerate_shot_image",
		AspectRatio:    options.AspectRatio,
		Force:          options.Force,
	}).Get(ctx, &image); err != nil {
		return RegenerationOutput{}, err
	}
	output := RegenerationOutput{TargetType: "shot_image", TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(image)}
	if err := workflow.ExecuteActivity(ctx, "CompleteRegenerationWorkflow", input, output).Get(ctx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func RegenerateShotVideoWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	createOptions := defaultActivityOptions()
	createOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createOptions)
	var created CreateShotVideoTaskOutput
	if err := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", CreateShotVideoTaskInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         options.TargetID,
		WorkflowPrompt: "regenerate_shot_video",
		Duration:       options.Duration,
		AspectRatio:    options.AspectRatio,
		Resolution:     options.Resolution,
		Force:          options.Force,
	}).Get(createCtx, &created); err != nil {
		return RegenerationOutput{}, err
	}
	var terminal PollShotVideoTaskOutput
	for pollCount := 1; pollCount <= options.MaxPolls; pollCount++ {
		var poll PollShotVideoTaskOutput
		if err := workflow.ExecuteActivity(ctx, "PollShotVideoTask", PollShotVideoTaskInput{
			OrganizationID:      input.OrganizationID,
			ProjectID:           input.ProjectID,
			WorkflowRunID:       input.WorkflowRunID,
			ShotID:              options.TargetID,
			NodeRunID:           created.NodeRunID,
			ProviderAsyncTaskID: created.ProviderAsyncTaskID,
			ExternalTaskID:      created.ExternalTaskID,
			PollCount:           pollCount,
		}).Get(ctx, &poll); err != nil {
			return RegenerationOutput{}, err
		}
		if poll.Status == "succeeded" {
			terminal = poll
			break
		}
		if poll.Status == "failed" || poll.Status == "cancelled" {
			return RegenerationOutput{}, temporal.NewApplicationError("provider video task "+poll.Status, codeActivityFailed)
		}
		if err := workflow.Sleep(ctx, time.Duration(options.PollIntervalSeconds)*time.Second); err != nil {
			return RegenerationOutput{}, err
		}
	}
	if terminal.Status != "succeeded" {
		return RegenerationOutput{}, temporal.NewApplicationError("provider video task polling timed out", codeProviderVideoPollingTimeout)
	}
	output := RegenerationOutput{TargetType: "shot_video", TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(terminal)}
	if err := workflow.ExecuteActivity(ctx, "CompleteRegenerationWorkflow", input, output).Get(ctx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func RegenerateFinalVideoWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	activityOptions := defaultActivityOptions()
	activityOptions.TaskQueue = MediaTaskQueue
	activityOptions.StartToCloseTimeout = 30 * time.Minute
	ctx = workflow.WithActivityOptions(ctx, activityOptions)
	var composed ComposeFinalVideoOutput
	if err := workflow.ExecuteActivity(ctx, "ComposeFinalVideo", ComposeFinalVideoInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		CreatedBy:           input.CreatedBy,
		SourceWorkflowRunID: options.TargetID,
		AspectRatio:         options.AspectRatio,
		Resolution:          options.Resolution,
	}).Get(ctx, &composed); err != nil {
		return RegenerationOutput{}, err
	}
	defaultCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	output := RegenerationOutput{TargetType: "final_video", TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(composed)}
	if err := workflow.ExecuteActivity(defaultCtx, "CompleteRegenerationWorkflow", input, output).Get(defaultCtx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func (a Activities) CompleteRegenerationWorkflow(ctx context.Context, input TextToStoryboardInput, output RegenerationOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func resolveRegenerationOptions(raw json.RawMessage) RegenerationOptions {
	options := RegenerationOptions{
		Force:               true,
		AspectRatio:         "16:9",
		Resolution:          "720p",
		PollIntervalSeconds: 5,
		MaxPolls:            120,
	}
	if len(raw) == 0 {
		return options
	}
	var decoded struct {
		TargetID            string  `json:"targetId"`
		Force               *bool   `json:"force"`
		Duration            float64 `json:"duration"`
		AspectRatio         string  `json:"aspectRatio"`
		Resolution          string  `json:"resolution"`
		PollIntervalSeconds int     `json:"pollIntervalSeconds"`
		MaxPolls            int     `json:"maxPolls"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return options
	}
	decoded.TargetID = strings.TrimSpace(decoded.TargetID)
	if decoded.TargetID != "" {
		options.TargetID = decoded.TargetID
	}
	if decoded.Force != nil {
		options.Force = *decoded.Force
	}
	if decoded.Duration > 0 {
		options.Duration = decoded.Duration
	}
	if strings.TrimSpace(decoded.AspectRatio) != "" {
		options.AspectRatio = strings.TrimSpace(decoded.AspectRatio)
	}
	if strings.TrimSpace(decoded.Resolution) != "" {
		options.Resolution = strings.TrimSpace(decoded.Resolution)
	}
	if decoded.PollIntervalSeconds > 0 {
		options.PollIntervalSeconds = decoded.PollIntervalSeconds
	}
	if decoded.MaxPolls > 0 {
		options.MaxPolls = decoded.MaxPolls
	}
	return options
}
