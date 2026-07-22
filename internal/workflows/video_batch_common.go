package workflows

import (
	"fmt"
	"time"

	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ShotVideoExecutionShot struct {
	ShotID                     string `json:"shotId"`
	ShotIndex                  int    `json:"shotIndex"`
	ShotNo                     int    `json:"shotNo"`
	OperationItemID            string `json:"operationItemId,omitempty"`
	OperationItemAttempt       int    `json:"operationItemAttempt,omitempty"`
	PredecessorExecutionPlanID string `json:"predecessorExecutionPlanId,omitempty"`
}

func newBatchShotVideoOutput(input TextToStoryboardInput, shotIDs []string) BatchShotProductionOutput {
	return BatchShotProductionOutput{
		Action: "batch_generate_shot_videos", WorkflowRunID: input.WorkflowRunID,
		TargetShotIDs: append([]string(nil), shotIDs...), ProviderAsyncTaskIDs: map[string]string{},
		Errors: map[string]string{}, Status: "running",
		ErrorCodes: map[string]string{},
	}
}

func appendRenderedShotVideoOutput(output *BatchShotProductionOutput, shotID string, rendered ShotRenderExecutionResult) {
	output.SucceededShotIDs = append(output.SucceededShotIDs, shotID)
	output.VideoOutputs = append(output.VideoOutputs, rendered.Segments...)
	output.ShotVideoOutputs = append(output.ShotVideoOutputs, rendered.Output)
	for _, created := range rendered.Creates {
		if created.ProviderCallID != "" {
			output.VideoCreateProviderCallIDs = append(output.VideoCreateProviderCallIDs, created.ProviderCallID)
		}
		if created.ProviderAsyncTaskID == "" {
			continue
		}
		output.ProviderAsyncTaskIDs[shotID] = created.ProviderAsyncTaskID
		output.ProviderAsyncTaskIDs[fmt.Sprintf("%s:%d", shotID, created.SegmentIndex)] = created.ProviderAsyncTaskID
	}
	for _, polled := range rendered.Polls {
		if polled.ProviderCallID != "" {
			output.VideoPollProviderCallIDs = append(output.VideoPollProviderCallIDs, polled.ProviderCallID)
		}
	}
}

func mergeBatchShotVideoOutput(target *BatchShotProductionOutput, source BatchShotProductionOutput) {
	target.SucceededShotIDs = append(target.SucceededShotIDs, source.SucceededShotIDs...)
	target.FailedShotIDs = append(target.FailedShotIDs, source.FailedShotIDs...)
	target.CancelledShotIDs = append(target.CancelledShotIDs, source.CancelledShotIDs...)
	target.VideoOutputs = append(target.VideoOutputs, source.VideoOutputs...)
	target.ShotVideoOutputs = append(target.ShotVideoOutputs, source.ShotVideoOutputs...)
	target.VideoCreateProviderCallIDs = append(target.VideoCreateProviderCallIDs, source.VideoCreateProviderCallIDs...)
	target.VideoPollProviderCallIDs = append(target.VideoPollProviderCallIDs, source.VideoPollProviderCallIDs...)
	if target.Errors == nil {
		target.Errors = map[string]string{}
	}
	for key, value := range source.Errors {
		target.Errors[key] = value
	}
	if target.ErrorCodes == nil {
		target.ErrorCodes = map[string]string{}
	}
	for key, value := range source.ErrorCodes {
		target.ErrorCodes[key] = value
	}
	if target.ProviderAsyncTaskIDs == nil {
		target.ProviderAsyncTaskIDs = map[string]string{}
	}
	for key, value := range source.ProviderAsyncTaskIDs {
		target.ProviderAsyncTaskIDs[key] = value
	}
}

func batchShotOutputStatus(output BatchShotProductionOutput) string {
	if len(output.FailedShotIDs) > 0 && len(output.SucceededShotIDs) > 0 {
		return "partial_succeeded"
	}
	if len(output.FailedShotIDs) > 0 {
		return "failed"
	}
	if len(output.CancelledShotIDs) > 0 && len(output.SucceededShotIDs) == 0 {
		return "cancelled"
	}
	return "succeeded"
}

func runShotVideoBatchChild(ctx workflow.Context, input TextToStoryboardInput, shots []StoryboardShotRecord, aspectRatio, resolution, audioStrategy, audioRequirement string, maxPolls int, pollInterval time.Duration) (BatchShotProductionOutput, error) {
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	childInput := input
	childInput.Input = mustJSON(BatchShotProductionOptions{
		ShotIDs: shotIDs, Force: true, MaxConcurrency: DefaultShotVideoConcurrency,
		AspectRatio: aspectRatio, Resolution: resolution, AudioStrategy: audioStrategy,
		AudioRequirement: audioRequirement, PollIntervalSeconds: int(pollInterval / time.Second),
		MaxPolls: maxPolls, SkipCompletion: true,
	})
	childOptions := workflow.ChildWorkflowOptions{
		WorkflowID:               workflow.GetInfo(ctx).WorkflowExecution.ID + ":episode-video-production",
		WorkflowExecutionTimeout: 7 * 24 * time.Hour, WorkflowRunTimeout: 24 * time.Hour,
		WaitForCancellation: true, ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	var output BatchShotProductionOutput
	err := workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, childOptions), EpisodeBatchGenerateShotVideosWorkflow, childInput,
	).Get(ctx, &output)
	return output, err
}

func videoProductionShotOutputs(shots []StoryboardShotRecord, images map[string]GenerateShotImageOutput, batch BatchShotProductionOutput, defaultDuration float64) []VideoProductionShotOutput {
	videos := make(map[string]ComposeShotRenderPlanMediaOutput, len(batch.ShotVideoOutputs))
	for _, video := range batch.ShotVideoOutputs {
		videos[video.ShotID] = video
	}
	outputs := make([]VideoProductionShotOutput, 0, len(batch.SucceededShotIDs))
	for _, shot := range shots {
		video, ok := videos[shot.ID]
		if !ok {
			continue
		}
		image := images[shot.ID]
		duration := shot.Duration
		if duration <= 0 {
			duration = defaultDuration
		}
		outputs = append(outputs, VideoProductionShotOutput{
			ShotID: shot.ID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo, Duration: duration,
			ImageArtifactID: image.ImageArtifactID, ImageMediaFileID: image.ImageMediaFileID,
			ImageStorageKey: image.ImageStorageKey, VideoArtifactID: video.ArtifactID,
			VideoMediaFileID: video.MediaFileID, VideoStorageKey: video.StorageKey,
			ProviderAsyncTaskID: batch.ProviderAsyncTaskIDs[shot.ID],
		})
	}
	return outputs
}
