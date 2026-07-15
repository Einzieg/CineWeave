package workflows

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const defaultVideoGroupsPerRun = 20

type PrepareShotVideoExecutionGroupsInput struct {
	OrganizationID string   `json:"organizationId"`
	ProjectID      string   `json:"projectId"`
	WorkflowRunID  string   `json:"workflowRunId"`
	ShotIDs        []string `json:"shotIds"`
}

type ShotVideoExecutionShot struct {
	ShotID    string `json:"shotId"`
	ShotIndex int    `json:"shotIndex"`
	ShotNo    int    `json:"shotNo"`
}

type ShotVideoContinuitySource struct {
	ShotID      string `json:"shotId"`
	ShotIndex   int    `json:"shotIndex"`
	ShotNo      int    `json:"shotNo"`
	ArtifactID  string `json:"artifactId"`
	MediaFileID string `json:"mediaFileId"`
	StorageKey  string `json:"storageKey"`
}

type shotVideoPreparationRecord struct {
	Shot                   ShotVideoExecutionShot
	ContinuityGroupID      string
	PredecessorShotID      string
	PredecessorShotIndex   int
	PredecessorShotNo      int
	PredecessorArtifactID  string
	PredecessorMediaFileID string
	PredecessorStorageKey  string
	PredecessorVideoStatus string
	PredecessorStaleState  string
}

type ShotVideoExecutionGroup struct {
	GroupKey               string                     `json:"groupKey"`
	ContinuityGroupID      string                     `json:"continuityGroupId,omitempty"`
	Shots                  []ShotVideoExecutionShot   `json:"shots"`
	InitialPredecessor     *ShotVideoContinuitySource `json:"initialPredecessor,omitempty"`
	InitialDependencyError string                     `json:"initialDependencyError,omitempty"`
}

type BatchShotVideoCheckpoint struct {
	Groups         []ShotVideoExecutionGroup `json:"groups"`
	NextGroupIndex int                       `json:"nextGroupIndex"`
	Output         BatchShotProductionOutput `json:"output"`
}

type ShotVideoContinuityGroupInput struct {
	TextInput TextToStoryboardInput      `json:"textInput"`
	Options   BatchShotProductionOptions `json:"options"`
	Group     ShotVideoExecutionGroup    `json:"group"`
}

func (a Activities) PrepareShotVideoExecutionGroups(ctx context.Context, input PrepareShotVideoExecutionGroupsInput) ([]ShotVideoExecutionGroup, error) {
	shotIDs := normalizeStringSlice(input.ShotIDs)
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || len(shotIDs) == 0 {
		return nil, fmt.Errorf("organizationId, projectId, and shotIds are required")
	}
	rows, err := a.db.Query(ctx, `
		WITH active_shots AS (
			SELECT shot.id, shot.shot_index, COALESCE(shot.shot_no, shot.shot_index + 1) AS shot_no,
			       shot.continuity_group_id,
			       LAG(shot.id) OVER continuity_order AS predecessor_id,
			       LAG(shot.shot_index) OVER continuity_order AS predecessor_index,
			       LAG(COALESCE(shot.shot_no, shot.shot_index + 1)) OVER continuity_order AS predecessor_no,
			       LAG(shot.video_artifact_id) OVER continuity_order AS predecessor_artifact_id,
			       LAG(shot.video_media_file_id) OVER continuity_order AS predecessor_media_file_id,
			       LAG(shot.video_storage_key) OVER continuity_order AS predecessor_storage_key,
			       LAG(shot.video_status) OVER continuity_order AS predecessor_video_status,
			       LAG(COALESCE(shot.stale_state, 'fresh')) OVER continuity_order AS predecessor_stale_state
			FROM storyboard_shots shot
			LEFT JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
			WHERE shot.organization_id = $1 AND shot.project_id = $2 AND shot.deleted_at IS NULL
			  AND (shot.storyboard_plan_id IS NULL OR (plan.active = true AND plan.status = 'ready'))
			WINDOW continuity_order AS (
				PARTITION BY shot.continuity_group_id ORDER BY shot.shot_index, shot.id
			)
		)
		SELECT id::text, shot_index, shot_no, COALESCE(continuity_group_id::text, ''),
		       COALESCE(predecessor_id::text, ''), COALESCE(predecessor_index, 0), COALESCE(predecessor_no, 0),
		       COALESCE(predecessor_artifact_id::text, ''), COALESCE(predecessor_media_file_id::text, ''),
		       COALESCE(predecessor_storage_key, ''), COALESCE(predecessor_video_status, ''),
		       COALESCE(predecessor_stale_state, '')
		FROM active_shots
		WHERE id::text = ANY($3::text[])
		ORDER BY shot_index, id
	`, input.OrganizationID, input.ProjectID, shotIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]shotVideoPreparationRecord, 0, len(shotIDs))
	found := map[string]bool{}
	for rows.Next() {
		var record shotVideoPreparationRecord
		if err := rows.Scan(
			&record.Shot.ShotID, &record.Shot.ShotIndex, &record.Shot.ShotNo, &record.ContinuityGroupID,
			&record.PredecessorShotID, &record.PredecessorShotIndex, &record.PredecessorShotNo,
			&record.PredecessorArtifactID, &record.PredecessorMediaFileID, &record.PredecessorStorageKey,
			&record.PredecessorVideoStatus, &record.PredecessorStaleState,
		); err != nil {
			return nil, err
		}
		found[record.Shot.ShotID] = true
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, shotID := range shotIDs {
		if !found[shotID] {
			return nil, fmt.Errorf("storyboard shot %s is missing or not in the active plan", shotID)
		}
	}
	groups := buildShotVideoExecutionGroups(records)
	if strings.TrimSpace(input.WorkflowRunID) != "" {
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "video.production.blueprint.created", "workflow_run", input.WorkflowRunID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "groupCount": len(groups), "shotCount": len(shotIDs), "groups": groups,
		})); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

func buildShotVideoExecutionGroups(records []shotVideoPreparationRecord) []ShotVideoExecutionGroup {
	groups := make([]ShotVideoExecutionGroup, 0, len(records))
	continuityRecords := map[string][]shotVideoPreparationRecord{}
	for _, record := range records {
		continuityID := strings.TrimSpace(record.ContinuityGroupID)
		if continuityID != "" {
			continuityRecords[continuityID] = append(continuityRecords[continuityID], record)
		}
	}
	emitted := map[string]bool{}
	for _, record := range records {
		continuityID := strings.TrimSpace(record.ContinuityGroupID)
		if continuityID == "" {
			groups = append(groups, ShotVideoExecutionGroup{GroupKey: "shot-" + record.Shot.ShotID, Shots: []ShotVideoExecutionShot{record.Shot}})
			continue
		}
		if emitted[continuityID] {
			continue
		}
		emitted[continuityID] = true
		items := continuityRecords[continuityID]
		sort.SliceStable(items, func(i, j int) bool { return items[i].Shot.ShotIndex < items[j].Shot.ShotIndex })
		var current *ShotVideoExecutionGroup
		previousSelectedShotID := ""
		for _, item := range items {
			if current == nil || item.PredecessorShotID != previousSelectedShotID {
				if current != nil {
					groups = append(groups, *current)
				}
				current = &ShotVideoExecutionGroup{
					GroupKey:          fmt.Sprintf("continuity-%s-from-%s", continuityID, item.Shot.ShotID),
					ContinuityGroupID: continuityID,
				}
				if item.PredecessorShotID != "" {
					if shotVideoPredecessorAvailable(item) {
						current.InitialPredecessor = &ShotVideoContinuitySource{
							ShotID: item.PredecessorShotID, ShotIndex: item.PredecessorShotIndex, ShotNo: item.PredecessorShotNo,
							ArtifactID: item.PredecessorArtifactID, MediaFileID: item.PredecessorMediaFileID, StorageKey: item.PredecessorStorageKey,
						}
					} else {
						current.InitialDependencyError = fmt.Sprintf("镜头 %d 的前序连续镜头 %d 没有可用的最新成片", item.Shot.ShotNo, item.PredecessorShotNo)
					}
				}
			}
			current.Shots = append(current.Shots, item.Shot)
			previousSelectedShotID = item.Shot.ShotID
		}
		if current != nil {
			groups = append(groups, *current)
		}
	}
	return groups
}

func shotVideoPredecessorAvailable(record shotVideoPreparationRecord) bool {
	return record.PredecessorVideoStatus == "succeeded" && record.PredecessorStaleState == "fresh" &&
		strings.TrimSpace(record.PredecessorArtifactID) != "" && strings.TrimSpace(record.PredecessorMediaFileID) != "" &&
		strings.TrimSpace(record.PredecessorStorageKey) != ""
}

func ShotVideoContinuityGroupWorkflow(ctx workflow.Context, input ShotVideoContinuityGroupInput) (BatchShotProductionOutput, error) {
	version := workflow.GetVersion(ctx, "video-cross-shot-tail-frame-v1", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		return legacyShotVideoContinuityGroupWorkflow(ctx, input)
	}
	return crossShotVideoContinuityGroupWorkflow(ctx, input)
}

func shotVideoBatchFailureScope(ctx workflow.Context) string {
	if workflow.GetVersion(ctx, "video-explicit-batch-failure-scope-v1", workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return ""
	}
	return workflowFailureScopeBatchItem
}

func legacyShotVideoContinuityGroupWorkflow(ctx workflow.Context, input ShotVideoContinuityGroupInput) (BatchShotProductionOutput, error) {
	options := input.Options
	textInput := input.TextInput
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	createOptions := defaultActivityOptions()
	createOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createOptions)
	output := newBatchShotVideoOutput(textInput, groupShotIDs(input.Group))
	failureScope := shotVideoBatchFailureScope(ctx)
	for index, shot := range input.Group.Shots {
		rendered, err := executeShotRenderPlan(ctx, createCtx, ShotRenderExecutionInput{
			OrganizationID: textInput.OrganizationID, ProjectID: textInput.ProjectID, WorkflowRunID: textInput.WorkflowRunID,
			CreatedBy: textInput.CreatedBy, ShotID: shot.ShotID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
			WorkflowPrompt: "batch_generate_shot_videos", FailureScope: failureScope,
			AspectRatio: options.AspectRatio, Resolution: options.Resolution,
			AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement, Force: options.Force,
			MaxPolls: options.MaxPolls, PollInterval: time.Duration(options.PollIntervalSeconds) * time.Second,
		})
		if err != nil {
			output.FailedShotIDs = append(output.FailedShotIDs, shot.ShotID)
			output.Errors[shot.ShotID] = err.Error()
			for _, blocked := range input.Group.Shots[index+1:] {
				output.FailedShotIDs = append(output.FailedShotIDs, blocked.ShotID)
				output.Errors[blocked.ShotID] = "CONTINUITY_DEPENDENCY_FAILED: 前序连续镜头生成失败"
			}
			break
		}
		appendRenderedShotVideoOutput(&output, shot.ShotID, rendered)
	}
	output.Status = batchShotOutputStatus(output)
	return output, nil
}

func crossShotVideoContinuityGroupWorkflow(ctx workflow.Context, input ShotVideoContinuityGroupInput) (BatchShotProductionOutput, error) {
	options := input.Options
	textInput := input.TextInput
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	createOptions := defaultActivityOptions()
	createOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createOptions)
	output := newBatchShotVideoOutput(textInput, groupShotIDs(input.Group))

	if input.Group.ContinuityGroupID == "" {
		return executeIndependentShotVideoGroup(ctx, createCtx, input, output)
	}
	failureScope := shotVideoBatchFailureScope(ctx)
	if strings.TrimSpace(input.Group.InitialDependencyError) != "" {
		markContinuityReferenceUnavailable(&output, input.Group.Shots, 0, input.Group.InitialDependencyError)
		output.Status = batchShotOutputStatus(output)
		return output, nil
	}

	var continuityFrame *ShotContinuityFrameReference
	if predecessor := input.Group.InitialPredecessor; predecessor != nil {
		frame, err := extractShotContinuityFrame(ctx, ExtractShotContinuityFrameInput{
			OrganizationID: textInput.OrganizationID, ProjectID: textInput.ProjectID, WorkflowRunID: textInput.WorkflowRunID,
			CreatedBy: textInput.CreatedBy, ShotID: predecessor.ShotID, SourceVideoArtifactID: predecessor.ArtifactID,
			SourceVideoMediaFileID: predecessor.MediaFileID, SourceVideoStorageKey: predecessor.StorageKey,
		})
		if err != nil {
			markContinuityReferenceUnavailable(&output, input.Group.Shots, 0, "前序镜头尾帧提取失败: "+err.Error())
			output.Status = batchShotOutputStatus(output)
			return output, nil
		}
		continuityFrame = continuityReferenceFromOutput(frame)
	}

	for index, shot := range input.Group.Shots {
		rendered, err := executeShotRenderPlan(ctx, createCtx, ShotRenderExecutionInput{
			OrganizationID: textInput.OrganizationID, ProjectID: textInput.ProjectID, WorkflowRunID: textInput.WorkflowRunID,
			CreatedBy: textInput.CreatedBy, ShotID: shot.ShotID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
			WorkflowPrompt: "batch_generate_shot_videos", FailureScope: failureScope,
			AspectRatio: options.AspectRatio, Resolution: options.Resolution,
			AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement, Force: options.Force,
			MaxPolls: options.MaxPolls, PollInterval: time.Duration(options.PollIntervalSeconds) * time.Second,
			ContinuityFirstFrame: continuityFrame,
		})
		if err != nil {
			output.FailedShotIDs = append(output.FailedShotIDs, shot.ShotID)
			output.Errors[shot.ShotID] = err.Error()
			markContinuityDependentsBlocked(&output, input.Group.Shots[index+1:])
			break
		}
		appendRenderedShotVideoOutput(&output, shot.ShotID, rendered)
		if index == len(input.Group.Shots)-1 {
			continue
		}
		frame, err := extractShotContinuityFrame(ctx, ExtractShotContinuityFrameInput{
			OrganizationID: textInput.OrganizationID, ProjectID: textInput.ProjectID, WorkflowRunID: textInput.WorkflowRunID,
			CreatedBy: textInput.CreatedBy, ShotID: shot.ShotID, SourceVideoArtifactID: rendered.Output.ArtifactID,
			SourceVideoMediaFileID: rendered.Output.MediaFileID, SourceVideoStorageKey: rendered.Output.StorageKey,
		})
		if err != nil {
			markContinuityReferenceUnavailable(&output, input.Group.Shots, index+1, "前序镜头尾帧提取失败: "+err.Error())
			break
		}
		continuityFrame = continuityReferenceFromOutput(frame)
	}
	output.Status = batchShotOutputStatus(output)
	return output, nil
}

func executeIndependentShotVideoGroup(ctx, createCtx workflow.Context, input ShotVideoContinuityGroupInput, output BatchShotProductionOutput) (BatchShotProductionOutput, error) {
	options := input.Options
	textInput := input.TextInput
	failureScope := shotVideoBatchFailureScope(ctx)
	for index, shot := range input.Group.Shots {
		rendered, err := executeShotRenderPlan(ctx, createCtx, ShotRenderExecutionInput{
			OrganizationID: textInput.OrganizationID, ProjectID: textInput.ProjectID, WorkflowRunID: textInput.WorkflowRunID,
			CreatedBy: textInput.CreatedBy, ShotID: shot.ShotID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
			WorkflowPrompt: "batch_generate_shot_videos", FailureScope: failureScope,
			AspectRatio: options.AspectRatio, Resolution: options.Resolution,
			AudioStrategy: options.AudioStrategy, AudioRequirement: options.AudioRequirement, Force: options.Force,
			MaxPolls: options.MaxPolls, PollInterval: time.Duration(options.PollIntervalSeconds) * time.Second,
		})
		if err != nil {
			output.FailedShotIDs = append(output.FailedShotIDs, shot.ShotID)
			output.Errors[shot.ShotID] = err.Error()
			markContinuityDependentsBlocked(&output, input.Group.Shots[index+1:])
			break
		}
		appendRenderedShotVideoOutput(&output, shot.ShotID, rendered)
	}
	output.Status = batchShotOutputStatus(output)
	return output, nil
}

func extractShotContinuityFrame(ctx workflow.Context, input ExtractShotContinuityFrameInput) (ExtractShotContinuityFrameOutput, error) {
	options := defaultActivityOptions()
	options.TaskQueue = MediaTaskQueue
	options.StartToCloseTimeout = 30 * time.Minute
	mediaCtx := workflow.WithActivityOptions(ctx, options)
	var output ExtractShotContinuityFrameOutput
	err := workflow.ExecuteActivity(mediaCtx, "ExtractShotContinuityFrame", input).Get(mediaCtx, &output)
	return output, err
}

func continuityReferenceFromOutput(output ExtractShotContinuityFrameOutput) *ShotContinuityFrameReference {
	return &ShotContinuityFrameReference{
		SourceShotID: output.SourceShotID, SourceVideoArtifactID: output.SourceVideoArtifactID,
		ArtifactID: output.ArtifactID, MediaFileID: output.MediaFileID, StorageKey: output.StorageKey,
	}
}

func markContinuityReferenceUnavailable(output *BatchShotProductionOutput, shots []ShotVideoExecutionShot, index int, detail string) {
	if index < 0 || index >= len(shots) {
		return
	}
	shot := shots[index]
	output.FailedShotIDs = append(output.FailedShotIDs, shot.ShotID)
	output.Errors[shot.ShotID] = "CONTINUITY_REFERENCE_UNAVAILABLE: " + strings.TrimSpace(detail)
	markContinuityDependentsBlocked(output, shots[index+1:])
}

func markContinuityDependentsBlocked(output *BatchShotProductionOutput, shots []ShotVideoExecutionShot) {
	for _, shot := range shots {
		output.FailedShotIDs = append(output.FailedShotIDs, shot.ShotID)
		output.Errors[shot.ShotID] = "CONTINUITY_DEPENDENCY_FAILED: 前序连续镜头未完成"
	}
}

func shotVideoGroupChildOptions(ctx workflow.Context, group ShotVideoExecutionGroup) workflow.ChildWorkflowOptions {
	return workflow.ChildWorkflowOptions{
		WorkflowID:               fmt.Sprintf("%s:video-group:%s", workflow.GetInfo(ctx).WorkflowExecution.ID, group.GroupKey),
		WorkflowExecutionTimeout: 24 * time.Hour, WorkflowRunTimeout: 12 * time.Hour,
		WaitForCancellation: true, ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
		Memo:        map[string]any{"groupKey": group.GroupKey, "continuityGroupId": group.ContinuityGroupID, "shotIds": groupShotIDs(group)},
	}
}

func groupShotIDs(group ShotVideoExecutionGroup) []string {
	result := make([]string, 0, len(group.Shots))
	for _, shot := range group.Shots {
		result = append(result, shot.ShotID)
	}
	return result
}

func newBatchShotVideoOutput(input TextToStoryboardInput, shotIDs []string) BatchShotProductionOutput {
	return BatchShotProductionOutput{
		Action: "batch_generate_shot_videos", WorkflowRunID: input.WorkflowRunID, TargetShotIDs: append([]string(nil), shotIDs...),
		ProviderAsyncTaskIDs: map[string]string{}, Errors: map[string]string{}, Status: "running",
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
		AspectRatio: aspectRatio, Resolution: resolution, AudioStrategy: audioStrategy, AudioRequirement: audioRequirement,
		PollIntervalSeconds: int(pollInterval / time.Second), MaxPolls: maxPolls, SkipCompletion: true,
	})
	childOptions := workflow.ChildWorkflowOptions{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID + ":video-batches", WorkflowExecutionTimeout: 7 * 24 * time.Hour,
		WorkflowRunTimeout: 24 * time.Hour, WaitForCancellation: true,
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	var output BatchShotProductionOutput
	err := workflow.ExecuteChildWorkflow(workflow.WithChildOptions(ctx, childOptions), BatchGenerateShotVideosWorkflow, childInput).Get(ctx, &output)
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
			ImageArtifactID: image.ImageArtifactID, ImageMediaFileID: image.ImageMediaFileID, ImageStorageKey: image.ImageStorageKey,
			VideoArtifactID: video.ArtifactID, VideoMediaFileID: video.MediaFileID, VideoStorageKey: video.StorageKey,
			ProviderAsyncTaskID: batch.ProviderAsyncTaskIDs[shot.ID],
		})
	}
	return outputs
}
