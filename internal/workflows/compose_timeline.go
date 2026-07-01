package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ComposeTimelineInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	TimelineID     string `json:"timelineId"`
	Title          string `json:"title"`
	Resolution     string `json:"resolution"`
	AspectRatio    string `json:"aspectRatio"`
	CreatedBy      string `json:"createdBy"`
}

type composeTimelineOptions struct {
	TimelineID  string `json:"timelineId"`
	Title       string `json:"title"`
	Resolution  string `json:"resolution"`
	AspectRatio string `json:"aspectRatio"`
}

type ComposeTimelineOutput struct {
	WorkflowRunID       string `json:"workflowRunId"`
	TimelineID          string `json:"timelineId"`
	FinalVideoVersionID string `json:"finalVideoVersionId"`
	ArtifactID          string `json:"artifactId"`
	MediaFileID         string `json:"mediaFileId"`
	StorageKey          string `json:"storageKey"`
	TimelineArtifactID  string `json:"timelineArtifactId,omitempty"`
	Status              string `json:"status"`
}

func ComposeTimelineWorkflow(ctx workflow.Context, input TextToStoryboardInput) (ComposeTimelineOutput, error) {
	options, err := resolveComposeTimelineOptions(input.Input)
	if err != nil {
		return ComposeTimelineOutput{}, temporal.NewApplicationError(err.Error(), codeActivityFailed)
	}
	if strings.TrimSpace(options.TimelineID) == "" {
		return ComposeTimelineOutput{}, temporal.NewApplicationError("timelineId is required", codeActivityFailed)
	}

	composeOptions := defaultActivityOptions()
	composeOptions.TaskQueue = MediaTaskQueue
	composeOptions.StartToCloseTimeout = 30 * time.Minute
	composeCtx := workflow.WithActivityOptions(ctx, composeOptions)
	var composeOutput ComposeFinalVideoOutput
	if err := workflow.ExecuteActivity(composeCtx, "ComposeFinalVideo", ComposeFinalVideoInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		TimelineID:     options.TimelineID,
		Title:          options.Title,
		AspectRatio:    options.AspectRatio,
		Resolution:     options.Resolution,
	}).Get(composeCtx, &composeOutput); err != nil {
		return ComposeTimelineOutput{}, err
	}

	output := ComposeTimelineOutput{
		WorkflowRunID:       input.WorkflowRunID,
		TimelineID:          options.TimelineID,
		FinalVideoVersionID: composeOutput.FinalVideoVersionID,
		ArtifactID:          composeOutput.ArtifactID,
		MediaFileID:         composeOutput.MediaFileID,
		StorageKey:          composeOutput.StorageKey,
		TimelineArtifactID:  composeOutput.TimelineArtifactID,
		Status:              "succeeded",
	}
	defaultCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	if err := workflow.ExecuteActivity(defaultCtx, "CompleteComposeTimelineWorkflow", input, output).Get(defaultCtx, nil); err != nil {
		return ComposeTimelineOutput{}, err
	}
	return output, nil
}

func resolveComposeTimelineOptions(raw json.RawMessage) (composeTimelineOptions, error) {
	var options composeTimelineOptions
	if len(raw) == 0 {
		return options, nil
	}
	if err := json.Unmarshal(raw, &options); err != nil {
		return composeTimelineOptions{}, fmt.Errorf("decode compose timeline input: %w", err)
	}
	options.TimelineID = strings.TrimSpace(options.TimelineID)
	options.Title = strings.TrimSpace(options.Title)
	options.Resolution = defaultString(options.Resolution, "720p")
	options.AspectRatio = defaultString(options.AspectRatio, "16:9")
	return options, nil
}

func (a Activities) CompleteComposeTimelineWorkflow(ctx context.Context, input TextToStoryboardInput, output ComposeTimelineOutput) error {
	outputJSON := mustJSON(output)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
		  AND status NOT IN ('cancelled', 'failed')
	`, input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.run.completed", "workflow_run", input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
