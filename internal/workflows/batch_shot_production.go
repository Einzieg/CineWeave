package workflows

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type BatchShotProductionOptions struct {
	ShotIDs             []string `json:"shotIds"`
	Force               bool     `json:"force"`
	MaxConcurrency      int      `json:"maxConcurrency"`
	Duration            float64  `json:"duration"`
	AspectRatio         string   `json:"aspectRatio"`
	Resolution          string   `json:"resolution"`
	AudioStrategy       string   `json:"audioStrategy"`
	AudioRequirement    string   `json:"audioRequirement"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds"`
	MaxPolls            int      `json:"maxPolls"`
	SkipCompletion      bool     `json:"skipCompletion,omitempty"`
}

type BatchShotProductionOutput struct {
	Action                     string                             `json:"action"`
	Status                     string                             `json:"status"`
	WorkflowRunID              string                             `json:"workflowRunId"`
	TargetShotIDs              []string                           `json:"targetShotIds"`
	SucceededShotIDs           []string                           `json:"succeededShotIds"`
	FailedShotIDs              []string                           `json:"failedShotIds"`
	CancelledShotIDs           []string                           `json:"cancelledShotIds,omitempty"`
	ProviderAsyncTaskIDs       map[string]string                  `json:"providerAsyncTaskIds,omitempty"`
	Errors                     map[string]string                  `json:"errors,omitempty"`
	ErrorCodes                 map[string]string                  `json:"errorCodes,omitempty"`
	ImageOutputs               []GenerateShotImageOutput          `json:"imageOutputs,omitempty"`
	ImagePromptOutputs         []PrepareShotImagePromptOutput     `json:"imagePromptOutputs,omitempty"`
	VideoOutputs               []PollShotVideoTaskOutput          `json:"videoOutputs,omitempty"`
	ShotVideoOutputs           []ComposeShotRenderPlanMediaOutput `json:"shotVideoOutputs,omitempty"`
	VideoCreateProviderCallIDs []string                           `json:"videoCreateProviderCallIds,omitempty"`
	VideoPollProviderCallIDs   []string                           `json:"videoPollProviderCallIds,omitempty"`
	VideoPromptOutputs         []PrepareShotVideoPromptOutput     `json:"videoPromptOutputs,omitempty"`
	CancelledProviderOutputs   []CancelShotVideoTaskOutput        `json:"cancelledProviderOutputs,omitempty"`
}

func BatchGenerateShotImagePromptsWorkflow(ctx workflow.Context, input TextToStoryboardInput) (output BatchShotProductionOutput, resultErr error) {
	options := resolveBatchShotProductionOptions(input.Input, DefaultShotImagePromptConcurrency, MaxShotImagePromptConcurrency)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	promptCtx := workflow.WithActivityOptions(ctx, shotImagePromptReviewActivityOptions())
	output = BatchShotProductionOutput{
		Action:        "batch_generate_shot_image_prompts",
		WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: options.ShotIDs,
		Errors:        map[string]string{},
		ErrorCodes:    map[string]string{},
	}
	completionStarted := false
	defer finalizeFailedBatchShotProduction(ctx, input, &output, &resultErr, &completionStarted)
	workItems, err := resolveShotAnchorWorkItemsForWorkflow(ctx, input, options.ShotIDs)
	if err != nil {
		return output, err
	}
	results, err := generateShotImagePromptsConcurrently(
		ctx, promptCtx, input, workItems, options.MaxConcurrency, options.AspectRatio, options.Resolution, options.Force,
	)
	if err != nil {
		return output, err
	}
	outcomes := make([]shotAnchorWorkItemOutcome, 0, len(results))
	for index, result := range results {
		item := workItems[index]
		outcomes = append(outcomes, shotAnchorWorkItemOutcome{Item: item, Err: result.Err})
		if result.Err != nil {
			continue
		}
		output.ImagePromptOutputs = append(output.ImagePromptOutputs, result.Output)
	}
	output.SucceededShotIDs, output.FailedShotIDs, output.Errors, output.ErrorCodes = summarizeShotAnchorWorkItemOutcomes(options.ShotIDs, outcomes)
	completionStarted = true
	if err := workflow.ExecuteActivity(ctx, "CompleteBatchShotProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return output, err
	}
	return output, nil
}

func BatchGenerateShotVideoPromptsWorkflow(ctx workflow.Context, input TextToStoryboardInput) (output BatchShotProductionOutput, resultErr error) {
	options := resolveBatchShotProductionOptions(input.Input, DefaultShotVideoPromptConcurrency, MaxShotVideoPromptConcurrency)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	promptCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	output = BatchShotProductionOutput{
		Action:        "batch_generate_shot_video_prompts",
		WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: options.ShotIDs,
		Errors:        map[string]string{},
		ErrorCodes:    map[string]string{},
	}
	completionStarted := false
	defer finalizeFailedBatchShotProduction(ctx, input, &output, &resultErr, &completionStarted)
	if workflow.GetVersion(ctx, "batch-video-prompt-dialogue-reconciliation-v3", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		var reconciled ReconcileStoryboardDialogueAssignmentsOutput
		if err := workflow.ExecuteActivity(ctx, "ReconcileStoryboardDialogueAssignments", ReconcileStoryboardDialogueAssignmentsInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			ShotIDs:        options.ShotIDs,
		}).Get(ctx, &reconciled); err != nil {
			return output, err
		}
		options.ShotIDs = reconciledStoryboardDialogueTargetIDs(options.ShotIDs, reconciled.ChangedShotIDs)
		output.TargetShotIDs = append([]string(nil), options.ShotIDs...)
	}
	var results []shotVideoPromptResult
	var err error
	version := workflow.GetVersion(ctx, "batch-video-prompt-render-plan-v1", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		results, err = generateShotVideoPromptsConcurrently(ctx, promptCtx, input, options)
	} else {
		results, err = generateShotVideoPromptPlansConcurrently(ctx, promptCtx, input, options)
	}
	if err != nil {
		return output, err
	}
	for index, result := range results {
		shotID := options.ShotIDs[index]
		if result.Err != nil {
			code, message := workflowExecutionError(result.Err)
			output.FailedShotIDs = append(output.FailedShotIDs, shotID)
			output.ErrorCodes[shotID] = code
			output.Errors[shotID] = message
			continue
		}
		output.SucceededShotIDs = append(output.SucceededShotIDs, shotID)
		output.VideoPromptOutputs = append(output.VideoPromptOutputs, result.Outputs...)
	}
	completionStarted = true
	if err := workflow.ExecuteActivity(ctx, "CompleteBatchShotProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return output, err
	}
	return output, nil
}

type BatchShotVideoCancelTask struct {
	ShotID              string `json:"shotId"`
	ShotIndex           int    `json:"shotIndex"`
	ShotNo              int    `json:"shotNo"`
	NodeRunID           string `json:"nodeRunId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	ExecutionPlanID     string `json:"executionPlanId,omitempty"`
	RenderSegmentID     string `json:"renderSegmentId,omitempty"`
	SegmentIndex        int    `json:"segmentIndex,omitempty"`
}

type ListRunningShotVideoTasksInput struct {
	ProjectID string   `json:"projectId"`
	ShotIDs   []string `json:"shotIds"`
}

func BatchGenerateShotImagesWorkflow(ctx workflow.Context, input TextToStoryboardInput) (output BatchShotProductionOutput, resultErr error) {
	options := resolveBatchShotProductionOptions(input.Input, DefaultShotImageConcurrency, MaxShotImageConcurrency)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	imageCtx := workflow.WithActivityOptions(ctx, providerImageActivityOptions())
	output = BatchShotProductionOutput{
		Action:        "batch_generate_shot_images",
		WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: options.ShotIDs,
		Errors:        map[string]string{},
		ErrorCodes:    map[string]string{},
	}
	completionStarted := false
	defer finalizeFailedBatchShotProduction(ctx, input, &output, &resultErr, &completionStarted)
	workItems, err := resolveShotAnchorWorkItemsForWorkflow(ctx, input, options.ShotIDs)
	if err != nil {
		return output, err
	}
	results := make([]shotImageGenerationResult, len(workItems))
	version := workflow.GetVersion(ctx, "batch-shot-image-concurrency-v1", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		for index, item := range workItems {
			var image GenerateShotImageOutput
			err := workflow.ExecuteActivity(imageCtx, "GenerateShotImage", GenerateShotImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				FailureScope:   workflowFailureScopeBatchItem,
				ShotID:         item.ShotID,
				ShotIndex:      item.ShotIndex,
				ShotNo:         item.ShotNo,
				AnchorRole:     item.AnchorRole,
				WorkflowPrompt: "batch_generate_shot_images",
				AspectRatio:    options.AspectRatio,
				Force:          options.Force,
			}).Get(imageCtx, &image)
			results[index] = shotImageGenerationResult{Output: image, Err: err}
		}
	} else {
		requests := make([]shotImageGenerationRequest, 0, len(workItems))
		for _, item := range workItems {
			requests = append(requests, shotImageGenerationRequest{
				ShotID:         item.ShotID,
				ShotIndex:      item.ShotIndex,
				ShotNo:         item.ShotNo,
				AnchorRole:     item.AnchorRole,
				WorkflowPrompt: "batch_generate_shot_images",
				AspectRatio:    options.AspectRatio,
				Force:          options.Force,
				FailureScope:   workflowFailureScopeBatchItem,
			})
		}
		var err error
		results, err = generateShotImagesConcurrently(ctx, imageCtx, input, requests, options.MaxConcurrency)
		if err != nil {
			return output, err
		}
	}
	for index := range results {
		if results[index].Err != nil {
			continue
		}
		if err := finalizeStoryboardSheetImage(ctx, input, results[index].Output); err != nil {
			results[index].Err = err
		}
	}
	outcomes := make([]shotAnchorWorkItemOutcome, 0, len(results))
	for index, result := range results {
		item := workItems[index]
		outcomes = append(outcomes, shotAnchorWorkItemOutcome{Item: item, Err: result.Err})
		if result.Err != nil {
			continue
		}
		output.ImageOutputs = append(output.ImageOutputs, result.Output)
	}
	output.SucceededShotIDs, output.FailedShotIDs, output.Errors, output.ErrorCodes = summarizeShotAnchorWorkItemOutcomes(options.ShotIDs, outcomes)
	completionStarted = true
	if err := workflow.ExecuteActivity(ctx, "CompleteBatchShotProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return output, err
	}
	return output, nil
}

func finalizeFailedBatchShotProduction(
	ctx workflow.Context,
	input TextToStoryboardInput,
	output *BatchShotProductionOutput,
	resultErr *error,
	completionStarted *bool,
) {
	if output == nil || resultErr == nil || *resultErr == nil || (completionStarted != nil && *completionStarted) {
		return
	}
	if temporal.IsCanceledError(*resultErr) {
		if workflow.GetVersion(ctx, "batch-shot-cancellation-finalizer-v1", workflow.DefaultVersion, 1) == workflow.DefaultVersion {
			return
		}
		output.Status = "cancelled"
		output.CancelledShotIDs = append([]string(nil), output.TargetShotIDs...)
		cancellationCtx, _ := workflow.NewDisconnectedContext(ctx)
		cancellationCtx = workflow.WithActivityOptions(cancellationCtx, defaultActivityOptions())
		if err := workflow.ExecuteActivity(cancellationCtx, "FinalizeBatchShotProductionCancellation", input, *output).Get(cancellationCtx, nil); err != nil {
			workflow.GetLogger(ctx).Error("failed to persist batch shot workflow cancellation", "error", err)
		}
		return
	}
	if workflow.GetVersion(ctx, "batch-shot-terminal-reconciliation-v1", workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return
	}
	code, message := workflowExecutionError(*resultErr)
	if strings.Contains(message, "ActivityNotRegisteredError") || strings.Contains(message, "unable to find activityType") {
		message = "Worker 缺少批量镜头任务所需组件，请更新 Worker 后重试"
	}
	output.Status = "failed"
	output.SucceededShotIDs = nil
	output.FailedShotIDs = append([]string(nil), output.TargetShotIDs...)
	if output.Errors == nil {
		output.Errors = make(map[string]string, len(output.TargetShotIDs))
	}
	if output.ErrorCodes == nil {
		output.ErrorCodes = make(map[string]string, len(output.TargetShotIDs))
	}
	for _, shotID := range output.TargetShotIDs {
		output.Errors[shotID] = message
		output.ErrorCodes[shotID] = code
	}
	failureCtx, _ := workflow.NewDisconnectedContext(ctx)
	failureCtx = workflow.WithActivityOptions(failureCtx, defaultActivityOptions())
	if err := workflow.ExecuteActivity(failureCtx, "CompleteBatchShotProductionWorkflow", input, *output).Get(failureCtx, nil); err != nil {
		workflow.GetLogger(ctx).Error("failed to persist batch shot workflow terminal state", "error", err)
	}
}

func (a Activities) FinalizeBatchShotProductionCancellation(ctx context.Context, input TextToStoryboardInput, output BatchShotProductionOutput) error {
	output.Status = "cancelled"
	if len(output.CancelledShotIDs) == 0 {
		output.CancelledShotIDs = append([]string(nil), output.TargetShotIDs...)
	}
	return CancelWorkflowRun(ctx, a.db, input.WorkflowRunID, mustJSON(output), "用户已取消批量镜头任务")
}

func BatchGenerateShotVideosWorkflow(ctx workflow.Context, input TextToStoryboardInput) (result BatchShotProductionOutput, err error) {
	return EpisodeBatchGenerateShotVideosWorkflow(ctx, input)
}

func BatchCancelShotVideosWorkflow(ctx workflow.Context, input TextToStoryboardInput) (BatchShotProductionOutput, error) {
	options := resolveBatchShotProductionOptions(input.Input, DefaultShotVideoConcurrency, MaxShotVideoConcurrency)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	output := BatchShotProductionOutput{
		Action:        "batch_cancel_shot_videos",
		WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: options.ShotIDs,
		Errors:        map[string]string{},
		ErrorCodes:    map[string]string{},
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
			ExecutionPlanID:     task.ExecutionPlanID,
			RenderSegmentID:     task.RenderSegmentID,
			SegmentIndex:        task.SegmentIndex,
			Reason:              "Batch cancel requested",
		}).Get(ctx, &cancelled)
		if err != nil {
			output.FailedShotIDs = append(output.FailedShotIDs, task.ShotID)
			code, message := workflowExecutionError(err)
			output.ErrorCodes[task.ShotID] = code
			output.Errors[task.ShotID] = message
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
	if strings.TrimSpace(output.Status) == "" {
		output.Status = batchShotOutputStatus(output)
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        "complete_batch_shot_production",
		NodeType:       "batch.complete",
		Input:          mustJSON(output),
	})
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if output.Action == "batch_generate_shot_image_prompts" {
		if err := a.reconcileFailedBatchShotImagePromptsTx(ctx, tx, input, output); err != nil {
			return err
		}
	}
	if output.Action == "batch_generate_shot_images" {
		if err := a.reconcileFailedBatchShotImagesTx(ctx, tx, input, output); err != nil {
			return err
		}
	}
	if output.Action == "batch_generate_shot_video_prompts" {
		if err := a.reconcileFailedBatchShotVideoPromptsTx(ctx, tx, input, output); err != nil {
			return err
		}
	}
	if output.Action == "batch_generate_shot_videos" {
		if err := a.reconcileFailedBatchShotVideosTx(ctx, tx, input, output); err != nil {
			return err
		}
	}
	outputJSON := mustJSON(output)
	if _, err := completeNodeRunTx(ctx, tx, execution, outputJSON); err != nil {
		return err
	}
	failureCode, failureMessage := batchShotWorkflowFailure(output)
	if _, applied, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, output.Status, failureCode, failureMessage, outputJSON); err != nil {
		return err
	} else if applied {
		totalItems, completedItems, failedItems := batchShotProgressCounts(output)
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			SET total_items = $2,
			    completed_items = $3,
			    failed_items = $4,
			    updated_at = now()
			WHERE id = $1
		`, input.WorkflowRunID, totalItems, completedItems, failedItems); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func batchShotProgressCounts(output BatchShotProductionOutput) (totalItems, completedItems, failedItems int) {
	targets := make(map[string]struct{}, len(output.TargetShotIDs))
	for _, shotID := range output.TargetShotIDs {
		if shotID = strings.TrimSpace(shotID); shotID != "" {
			targets[shotID] = struct{}{}
		}
	}
	succeeded := make(map[string]struct{}, len(output.SucceededShotIDs))
	for _, shotID := range output.SucceededShotIDs {
		succeeded[strings.TrimSpace(shotID)] = struct{}{}
	}
	failed := make(map[string]struct{}, len(output.FailedShotIDs))
	for _, shotID := range output.FailedShotIDs {
		failed[strings.TrimSpace(shotID)] = struct{}{}
	}
	for shotID := range targets {
		totalItems++
		if _, ok := failed[shotID]; ok {
			failedItems++
			continue
		}
		if _, ok := succeeded[shotID]; ok {
			completedItems++
		}
	}
	return totalItems, completedItems, failedItems
}

func batchShotWorkflowFailure(output BatchShotProductionOutput) (string, string) {
	if output.Status != "failed" {
		return "", ""
	}
	message := ""
	for _, shotID := range output.TargetShotIDs {
		if value := strings.TrimSpace(output.Errors[shotID]); value != "" {
			message = value
			break
		}
	}
	if message == "" {
		message = "所有目标均生成失败"
	}
	failedCodes := make([]string, 0, len(output.FailedShotIDs))
	for _, shotID := range output.FailedShotIDs {
		if code := strings.TrimSpace(output.ErrorCodes[shotID]); code != "" {
			failedCodes = append(failedCodes, code)
		}
	}
	if len(failedCodes) == len(output.FailedShotIDs) && len(failedCodes) > 0 {
		if code := commonWorkflowErrorCode(failedCodes); code != codeActivityFailed {
			return code, message
		}
	}
	if len(output.FailedShotIDs) == 1 {
		if code := strings.TrimSpace(output.ErrorCodes[output.FailedShotIDs[0]]); code != "" {
			return code, message
		}
		switch output.Action {
		case "batch_generate_shot_images":
			return batchShotImageErrorCode(message), message
		case "batch_generate_shot_videos":
			return batchShotVideoErrorCode(message), message
		}
	}
	return "BATCH_ALL_FAILED", message
}

func (a Activities) reconcileFailedBatchShotImagePromptsTx(ctx context.Context, tx pgx.Tx, input TextToStoryboardInput, output BatchShotProductionOutput) error {
	if len(output.FailedShotIDs) == 0 {
		return nil
	}
	for _, shotID := range output.FailedShotIDs {
		message := strings.TrimSpace(output.Errors[shotID])
		if message == "" {
			message = "image prompt generation failed"
		}
		code := strings.TrimSpace(output.ErrorCodes[shotID])
		if code == "" {
			code = codeActivityFailed
		}
		normalizedMessage := message
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET image_prompt_status = 'failed',
			    image_prompt_error_code = $4,
			    image_prompt_error_message = $5,
			    image_prompt_updated_at = now(),
			    updated_at = now()
			WHERE id = $1
			  AND project_id = $2
			  AND image_prompt_workflow_run_id = $3
			  AND image_prompt_status IN ('queued', 'running')
		`, shotID, input.ProjectID, input.WorkflowRunID, code, normalizedMessage); err != nil {
			return err
		}
	}
	return nil
}

func (a Activities) reconcileFailedBatchShotVideoPromptsTx(ctx context.Context, tx pgx.Tx, input TextToStoryboardInput, output BatchShotProductionOutput) error {
	if len(output.FailedShotIDs) == 0 {
		return nil
	}
	for _, shotID := range output.FailedShotIDs {
		message := strings.TrimSpace(output.Errors[shotID])
		if message == "" {
			message = "video prompt generation failed"
		}
		code := strings.TrimSpace(output.ErrorCodes[shotID])
		if code == "" {
			code = codeActivityFailed
		}
		normalizedMessage := message
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET video_prompt_status = 'failed',
			    video_prompt_error_code = $4,
			    video_prompt_error_message = $5,
			    video_prompt_updated_at = now(),
			    updated_at = now()
			WHERE id = $1
			  AND project_id = $2
			  AND video_prompt_workflow_run_id = $3
			  AND video_prompt_status IN ('queued', 'running')
		`, shotID, input.ProjectID, input.WorkflowRunID, code, normalizedMessage); err != nil {
			return err
		}
		if _, err := restoreApprovedVideoPromptStateTx(ctx, tx, shotID, input.WorkflowRunID); err != nil {
			return err
		}
	}
	return nil
}

func (a Activities) reconcileFailedBatchShotImagesTx(ctx context.Context, tx pgx.Tx, input TextToStoryboardInput, output BatchShotProductionOutput) error {
	if len(output.FailedShotIDs) == 0 {
		return nil
	}
	for _, shotID := range output.FailedShotIDs {
		message := strings.TrimSpace(output.Errors[shotID])
		if message == "" {
			message = "image generation failed"
		}
		code := strings.TrimSpace(output.ErrorCodes[shotID])
		if code == "" {
			code = batchShotImageErrorCode(message)
		}
		shotTag, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = 'image_failed',
			    image_status = 'failed',
			    image_error_code = $4,
			    image_error_message = $5,
			    image_completed_at = now(),
			    updated_at = now()
			WHERE id = $1
			  AND project_id = $2
			  AND image_workflow_run_id = $3
			  AND image_status IN ('queued', 'running')
		`, shotID, input.ProjectID, input.WorkflowRunID, code, message)
		if err != nil {
			return err
		}
		if shotTag.RowsAffected() > 0 {
			if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image.failed", "storyboard_shot", shotID, mustJSON(map[string]any{
				"workflowRunId": input.WorkflowRunID,
				"shotId":        shotID,
				"status":        "image_failed",
				"code":          code,
				"message":       message,
			})); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a Activities) reconcileFailedBatchShotVideosTx(ctx context.Context, tx pgx.Tx, input TextToStoryboardInput, output BatchShotProductionOutput) error {
	if len(output.FailedShotIDs) == 0 {
		return nil
	}
	for _, shotID := range output.FailedShotIDs {
		message := strings.TrimSpace(output.Errors[shotID])
		if message == "" {
			message = "video generation failed"
		}
		code := strings.TrimSpace(output.ErrorCodes[shotID])
		if code == "" {
			code = batchShotVideoErrorCode(message)
		}
		shotTag, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = 'video_failed',
			    video_status = 'failed',
			    video_error_code = $4,
			    video_error_message = $5,
			    video_completed_at = now(),
			    updated_at = now()
			WHERE id = $1
			  AND project_id = $2
			  AND video_workflow_run_id = $3
			  AND video_status IN ('queued', 'running')
		`, shotID, input.ProjectID, input.WorkflowRunID, code, message)
		if err != nil {
			return err
		}
		if shotTag.RowsAffected() > 0 {
			if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.failed", "storyboard_shot", shotID, mustJSON(map[string]any{
				"workflowRunId": input.WorkflowRunID,
				"shotId":        shotID,
				"status":        "video_failed",
				"code":          code,
				"message":       message,
			})); err != nil {
				return err
			}
		}
	}
	return nil
}

func batchShotImageErrorCode(message string) string {
	normalized := strings.ToUpper(strings.TrimSpace(message))
	switch {
	case strings.Contains(normalized, provider.CodeContentRejected),
		strings.Contains(normalized, "GUARDRAIL"),
		strings.Contains(normalized, "VIOLATE"):
		return provider.CodeContentRejected
	case strings.Contains(normalized, provider.CodeUpstreamTimeout),
		strings.Contains(normalized, "DEADLINE EXCEEDED"),
		strings.Contains(normalized, "STARTTOCLOSE TIMEOUT"):
		return provider.CodeUpstreamTimeout
	case strings.Contains(normalized, provider.CodeRateLimited):
		return provider.CodeRateLimited
	case strings.Contains(normalized, provider.CodeInvalidRequest):
		return provider.CodeInvalidRequest
	default:
		return codeActivityFailed
	}
}

func batchShotVideoErrorCode(message string) string {
	code := batchShotImageErrorCode(message)
	if code != codeActivityFailed {
		return code
	}
	normalized := strings.ToUpper(strings.TrimSpace(message))
	if strings.Contains(normalized, "MISSING OR NOT IN THE ACTIVE PLAN") {
		return provider.CodeInvalidRequest
	}
	return codeActivityFailed
}

func failedBatchShotVideoOutput(input TextToStoryboardInput, shotIDs []string, cause error) BatchShotProductionOutput {
	output := newBatchShotVideoOutput(input, shotIDs)
	code, message := workflowExecutionError(cause)
	for _, shotID := range shotIDs {
		output.FailedShotIDs = append(output.FailedShotIDs, shotID)
		output.ErrorCodes[shotID] = code
		output.Errors[shotID] = message
	}
	output.Status = "failed"
	return output
}

func isWorkflowCancellationError(err error) bool {
	return temporal.IsCanceledError(err)
}

func (a Activities) ListRunningShotVideoTasks(ctx context.Context, input ListRunningShotVideoTasksInput) ([]BatchShotVideoCancelTask, error) {
	rows, err := a.db.Query(ctx, `
		WITH segment_tasks AS (
		  SELECT shot.id::text AS shot_id, shot.shot_index, COALESCE(shot.shot_no, shot.shot_index + 1) AS shot_no,
		         COALESCE(task.node_run_id::text, '') AS node_run_id, task.id::text AS provider_task_id,
		         COALESCE(task.external_task_id, '') AS external_task_id,
		         COALESCE(task.video_render_plan_id::text, '') AS execution_plan_id,
		         COALESCE(task.video_render_segment_id::text, '') AS render_segment_id,
		         segment.segment_index
		  FROM provider_async_tasks task
		  JOIN video_render_segments segment ON segment.id = task.video_render_segment_id
		  JOIN storyboard_shots shot ON shot.id = segment.storyboard_shot_id
		  WHERE shot.project_id = $1 AND shot.id::text = ANY($2::text[])
		    AND shot.deleted_at IS NULL AND task.status IN ('queued', 'running', 'cancelling')
		)
		SELECT shot_id, shot_index, shot_no, node_run_id, provider_task_id, external_task_id,
		       execution_plan_id, render_segment_id, segment_index
		FROM segment_tasks
		ORDER BY shot_index, segment_index
	`, input.ProjectID, input.ShotIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BatchShotVideoCancelTask, 0)
	for rows.Next() {
		var item BatchShotVideoCancelTask
		if err := rows.Scan(&item.ShotID, &item.ShotIndex, &item.ShotNo, &item.NodeRunID, &item.ProviderAsyncTaskID, &item.ExternalTaskID, &item.ExecutionPlanID, &item.RenderSegmentID, &item.SegmentIndex); err != nil {
			return nil, err
		}
		if strings.TrimSpace(item.NodeRunID) == "" {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func resolveBatchShotProductionOptions(raw json.RawMessage, defaultConcurrency, maxConcurrency int) BatchShotProductionOptions {
	options := BatchShotProductionOptions{
		Force:               true,
		MaxConcurrency:      clampConcurrency(0, defaultConcurrency, maxConcurrency),
		AspectRatio:         "16:9",
		Resolution:          "720p",
		AudioStrategy:       "native_av",
		AudioRequirement:    "preferred",
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
		AudioStrategy       string   `json:"audioStrategy"`
		AudioRequirement    string   `json:"audioRequirement"`
		PollIntervalSeconds int      `json:"pollIntervalSeconds"`
		MaxPolls            int      `json:"maxPolls"`
		SkipCompletion      bool     `json:"skipCompletion"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return options
	}
	options.ShotIDs = cleanBatchStringList(decoded.ShotIDs)
	if decoded.MaxConcurrency > 0 {
		options.MaxConcurrency = clampConcurrency(decoded.MaxConcurrency, defaultConcurrency, maxConcurrency)
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
	if strings.TrimSpace(decoded.AudioStrategy) != "" {
		options.AudioStrategy = strings.TrimSpace(decoded.AudioStrategy)
	}
	if strings.TrimSpace(decoded.AudioRequirement) != "" {
		options.AudioRequirement = strings.TrimSpace(decoded.AudioRequirement)
	}
	if decoded.PollIntervalSeconds > 0 {
		options.PollIntervalSeconds = decoded.PollIntervalSeconds
	}
	if decoded.MaxPolls > 0 {
		options.MaxPolls = decoded.MaxPolls
	}
	options.SkipCompletion = decoded.SkipCompletion
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
