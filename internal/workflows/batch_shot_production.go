package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"
)

type BatchShotProductionOptions struct {
	ShotIDs             []string `json:"shotIds"`
	Force               bool     `json:"force"`
	MaxConcurrency      int      `json:"maxConcurrency"`
	Duration            float64  `json:"duration"`
	AspectRatio         string   `json:"aspectRatio"`
	Resolution          string   `json:"resolution"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds"`
	MaxPolls            int      `json:"maxPolls"`
}

type BatchShotProductionOutput struct {
	Action                   string                      `json:"action"`
	WorkflowRunID            string                      `json:"workflowRunId"`
	TargetShotIDs            []string                    `json:"targetShotIds"`
	SucceededShotIDs         []string                    `json:"succeededShotIds"`
	FailedShotIDs            []string                    `json:"failedShotIds"`
	CancelledShotIDs         []string                    `json:"cancelledShotIds,omitempty"`
	ProviderAsyncTaskIDs     map[string]string           `json:"providerAsyncTaskIds,omitempty"`
	Errors                   map[string]string           `json:"errors,omitempty"`
	ImageOutputs             []GenerateShotImageOutput   `json:"imageOutputs,omitempty"`
	VideoOutputs             []PollShotVideoTaskOutput   `json:"videoOutputs,omitempty"`
	CancelledProviderOutputs []CancelShotVideoTaskOutput `json:"cancelledProviderOutputs,omitempty"`
}

type BatchShotVideoCancelTask struct {
	ShotID              string `json:"shotId"`
	ShotIndex           int    `json:"shotIndex"`
	ShotNo              int    `json:"shotNo"`
	NodeRunID           string `json:"nodeRunId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
}

type ListRunningShotVideoTasksInput struct {
	ProjectID string   `json:"projectId"`
	ShotIDs   []string `json:"shotIds"`
}

func BatchGenerateShotImagesWorkflow(ctx workflow.Context, input TextToStoryboardInput) (BatchShotProductionOutput, error) {
	options := resolveBatchShotProductionOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	output := BatchShotProductionOutput{
		Action:        "batch_generate_shot_images",
		WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: options.ShotIDs,
		Errors:        map[string]string{},
	}
	for _, shotID := range options.ShotIDs {
		var image GenerateShotImageOutput
		err := workflow.ExecuteActivity(ctx, "GenerateShotImage", GenerateShotImageInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shotID,
			WorkflowPrompt: "batch_generate_shot_images",
			AspectRatio:    options.AspectRatio,
			Force:          options.Force,
		}).Get(ctx, &image)
		if err != nil {
			output.FailedShotIDs = append(output.FailedShotIDs, shotID)
			output.Errors[shotID] = err.Error()
			continue
		}
		output.SucceededShotIDs = append(output.SucceededShotIDs, shotID)
		output.ImageOutputs = append(output.ImageOutputs, image)
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteBatchShotProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return BatchShotProductionOutput{}, err
	}
	return output, nil
}

func BatchGenerateShotVideosWorkflow(ctx workflow.Context, input TextToStoryboardInput) (result BatchShotProductionOutput, err error) {
	options := resolveBatchShotProductionOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	createOptions := defaultActivityOptions()
	createOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createOptions)
	result = BatchShotProductionOutput{
		Action:               "batch_generate_shot_videos",
		WorkflowRunID:        input.WorkflowRunID,
		TargetShotIDs:        options.ShotIDs,
		ProviderAsyncTaskIDs: map[string]string{},
		Errors:               map[string]string{},
	}
	var currentCreate CreateShotVideoTaskOutput
	var currentShotID string
	defer func() {
		if ctx.Err() == nil || currentCreate.ProviderAsyncTaskID == "" || currentCreate.NodeRunID == "" {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		var cancelOutput CancelShotVideoTaskOutput
		_ = workflow.ExecuteActivity(cleanupCtx, "CancelShotVideoTask", CancelShotVideoTaskInput{
			OrganizationID:      input.OrganizationID,
			ProjectID:           input.ProjectID,
			WorkflowRunID:       input.WorkflowRunID,
			ShotID:              currentShotID,
			NodeRunID:           currentCreate.NodeRunID,
			ProviderAsyncTaskID: currentCreate.ProviderAsyncTaskID,
			ExternalTaskID:      currentCreate.ExternalTaskID,
			Reason:              "Batch workflow cancellation requested",
		}).Get(cleanupCtx, &cancelOutput)
	}()
	for _, shotID := range options.ShotIDs {
		currentShotID = shotID
		currentCreate = CreateShotVideoTaskOutput{}
		var created CreateShotVideoTaskOutput
		if err := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", CreateShotVideoTaskInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shotID,
			WorkflowPrompt: "batch_generate_shot_videos",
			Duration:       options.Duration,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
			Force:          options.Force,
		}).Get(createCtx, &created); err != nil {
			result.FailedShotIDs = append(result.FailedShotIDs, shotID)
			result.Errors[shotID] = err.Error()
			continue
		}
		currentCreate = created
		result.ProviderAsyncTaskIDs[shotID] = created.ProviderAsyncTaskID
		var terminal PollShotVideoTaskOutput
		shotSucceeded := false
		for pollCount := 1; pollCount <= options.MaxPolls; pollCount++ {
			var poll PollShotVideoTaskOutput
			if err := workflow.ExecuteActivity(ctx, "PollShotVideoTask", PollShotVideoTaskInput{
				OrganizationID:      input.OrganizationID,
				ProjectID:           input.ProjectID,
				WorkflowRunID:       input.WorkflowRunID,
				ShotID:              shotID,
				NodeRunID:           created.NodeRunID,
				ProviderAsyncTaskID: created.ProviderAsyncTaskID,
				ExternalTaskID:      created.ExternalTaskID,
				PollCount:           pollCount,
			}).Get(ctx, &poll); err != nil {
				result.FailedShotIDs = append(result.FailedShotIDs, shotID)
				result.Errors[shotID] = err.Error()
				break
			}
			if poll.Status == "succeeded" {
				terminal = poll
				shotSucceeded = true
				break
			}
			if poll.Status == "failed" || poll.Status == "cancelled" {
				result.FailedShotIDs = append(result.FailedShotIDs, shotID)
				result.Errors[shotID] = "provider video task " + poll.Status
				break
			}
			if err := workflow.Sleep(ctx, time.Duration(options.PollIntervalSeconds)*time.Second); err != nil {
				return BatchShotProductionOutput{}, err
			}
		}
		if shotSucceeded {
			result.SucceededShotIDs = append(result.SucceededShotIDs, shotID)
			result.VideoOutputs = append(result.VideoOutputs, terminal)
		} else if _, ok := result.Errors[shotID]; !ok {
			result.FailedShotIDs = append(result.FailedShotIDs, shotID)
			result.Errors[shotID] = "provider video task polling timed out"
		}
		currentCreate = CreateShotVideoTaskOutput{}
		currentShotID = ""
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteBatchShotProductionWorkflow", input, result).Get(ctx, nil); err != nil {
		return BatchShotProductionOutput{}, err
	}
	return result, nil
}

func BatchCancelShotVideosWorkflow(ctx workflow.Context, input TextToStoryboardInput) (BatchShotProductionOutput, error) {
	options := resolveBatchShotProductionOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	output := BatchShotProductionOutput{
		Action:        "batch_cancel_shot_videos",
		WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: options.ShotIDs,
		Errors:        map[string]string{},
	}
	var tasks []BatchShotVideoCancelTask
	if err := workflow.ExecuteActivity(ctx, "ListRunningShotVideoTasks", ListRunningShotVideoTasksInput{
		ProjectID: input.ProjectID,
		ShotIDs:   options.ShotIDs,
	}).Get(ctx, &tasks); err != nil {
		return BatchShotProductionOutput{}, err
	}
	for _, task := range tasks {
		var cancelled CancelShotVideoTaskOutput
		err := workflow.ExecuteActivity(ctx, "CancelShotVideoTask", CancelShotVideoTaskInput{
			OrganizationID:      input.OrganizationID,
			ProjectID:           input.ProjectID,
			WorkflowRunID:       input.WorkflowRunID,
			ShotID:              task.ShotID,
			ShotIndex:           task.ShotIndex,
			ShotNo:              task.ShotNo,
			NodeRunID:           task.NodeRunID,
			ProviderAsyncTaskID: task.ProviderAsyncTaskID,
			ExternalTaskID:      task.ExternalTaskID,
			Reason:              "Batch cancel requested",
		}).Get(ctx, &cancelled)
		if err != nil {
			output.FailedShotIDs = append(output.FailedShotIDs, task.ShotID)
			output.Errors[task.ShotID] = err.Error()
			continue
		}
		output.CancelledShotIDs = append(output.CancelledShotIDs, task.ShotID)
		output.CancelledProviderOutputs = append(output.CancelledProviderOutputs, cancelled)
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteBatchShotProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return BatchShotProductionOutput{}, err
	}
	return output, nil
}

func (a Activities) CompleteBatchShotProductionWorkflow(ctx context.Context, input TextToStoryboardInput, output BatchShotProductionOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) ListRunningShotVideoTasks(ctx context.Context, input ListRunningShotVideoTasksInput) ([]BatchShotVideoCancelTask, error) {
	rows, err := a.db.Query(ctx, `
		SELECT
			s.id::text,
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			COALESCE(n.id::text, ''),
			COALESCE(s.video_provider_async_task_id::text, ''),
			COALESCE(s.video_external_task_id, '')
		FROM storyboard_shots s
		LEFT JOIN LATERAL (
			SELECT id
			FROM workflow_node_runs
			WHERE project_id = s.project_id
			  AND node_key = 'create_shot_video_' || s.shot_index::text
			  AND status = 'running'
			ORDER BY started_at DESC
			LIMIT 1
		) n ON true
		WHERE s.project_id = $1
		  AND s.id = ANY($2::uuid[])
		  AND s.deleted_at IS NULL
		  AND COALESCE(s.video_provider_async_task_id::text, '') <> ''
		  AND COALESCE(s.video_status, '') IN ('queued', 'running')
		ORDER BY s.shot_index ASC
	`, input.ProjectID, input.ShotIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BatchShotVideoCancelTask, 0)
	for rows.Next() {
		var item BatchShotVideoCancelTask
		if err := rows.Scan(&item.ShotID, &item.ShotIndex, &item.ShotNo, &item.NodeRunID, &item.ProviderAsyncTaskID, &item.ExternalTaskID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(item.NodeRunID) == "" {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func resolveBatchShotProductionOptions(raw json.RawMessage) BatchShotProductionOptions {
	options := BatchShotProductionOptions{
		Force:               true,
		MaxConcurrency:      1,
		AspectRatio:         "16:9",
		Resolution:          "720p",
		PollIntervalSeconds: 5,
		MaxPolls:            120,
	}
	if len(raw) == 0 {
		return options
	}
	var decoded struct {
		ShotIDs             []string `json:"shotIds"`
		Force               *bool    `json:"force"`
		MaxConcurrency      int      `json:"maxConcurrency"`
		Duration            float64  `json:"duration"`
		AspectRatio         string   `json:"aspectRatio"`
		Resolution          string   `json:"resolution"`
		PollIntervalSeconds int      `json:"pollIntervalSeconds"`
		MaxPolls            int      `json:"maxPolls"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return options
	}
	options.ShotIDs = cleanBatchStringList(decoded.ShotIDs)
	if decoded.MaxConcurrency > 0 {
		options.MaxConcurrency = decoded.MaxConcurrency
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

func cleanBatchStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
