package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	defaultEpisodeVideoBatchSize   = 5
	maximumEpisodeVideoBatchSize   = 10
	defaultEpisodeWorkflowParallel = 2
	maximumEpisodeBatchesPerRun    = 4
)

type PrepareEpisodeVideoProductionsInput struct {
	OrganizationID string                     `json:"organizationId"`
	ProjectID      string                     `json:"projectId"`
	WorkflowRunID  string                     `json:"workflowRunId"`
	CreatedBy      string                     `json:"createdBy,omitempty"`
	Options        BatchShotProductionOptions `json:"options"`
}

type EpisodeVideoProductionPlan struct {
	CheckpointID                   string   `json:"checkpointId"`
	OrganizationID                 string   `json:"organizationId"`
	ProjectID                      string   `json:"projectId"`
	WorkflowRunID                  string   `json:"workflowRunId"`
	CreatedBy                      string   `json:"createdBy,omitempty"`
	ScriptEpisodeID                string   `json:"scriptEpisodeId"`
	EpisodeIndex                   int      `json:"episodeIndex"`
	EpisodeTitle                   string   `json:"episodeTitle"`
	ProductionGenerationID         string   `json:"productionGenerationId"`
	VideoProductionBindingID       string   `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64    `json:"videoProductionBindingRevision"`
	ProductionProfileVersionID     string   `json:"productionProfileVersionId"`
	ProductionProfileSnapshotHash  string   `json:"productionProfileSnapshotHash"`
	TemporalWorkflowID             string   `json:"temporalWorkflowId"`
	TargetShotIDs                  []string `json:"targetShotIds,omitempty"`
}

type EpisodeVideoProductionInput struct {
	Plan    EpisodeVideoProductionPlan `json:"plan"`
	Options BatchShotProductionOptions `json:"options"`
}

type EpisodeVideoProductionBatch struct {
	BatchID        string                    `json:"batchId"`
	CheckpointID   string                    `json:"checkpointId"`
	Ordinal        int                       `json:"ordinal"`
	DependencyHash string                    `json:"dependencyHash"`
	Shots          []ShotVideoExecutionShot  `json:"shots"`
	Done           bool                      `json:"done"`
	NeedsReconcile bool                      `json:"needsReconcile,omitempty"`
	FinalOutput    BatchShotProductionOutput `json:"finalOutput,omitempty"`
}

type SceneOrShotBatchInput struct {
	Plan    EpisodeVideoProductionPlan  `json:"plan"`
	Batch   EpisodeVideoProductionBatch `json:"batch"`
	Options BatchShotProductionOptions  `json:"options"`
}

type episodeVideoShotResult struct {
	Output    BatchShotProductionOutput
	ShotID    string
	ErrorCode string
	Error     string
	Cancelled bool
}

type CommitEpisodeVideoProductionBatchInput struct {
	Plan   EpisodeVideoProductionPlan  `json:"plan"`
	Batch  EpisodeVideoProductionBatch `json:"batch"`
	Output BatchShotProductionOutput   `json:"output"`
}

type CommitEpisodeVideoProductionBatchOutput struct {
	HasMore     bool                      `json:"hasMore"`
	Status      string                    `json:"status"`
	FinalOutput BatchShotProductionOutput `json:"finalOutput,omitempty"`
}

type FailEpisodeVideoProductionCheckpointInput struct {
	Plan           EpisodeVideoProductionPlan `json:"plan"`
	FailureCode    string                     `json:"failureCode"`
	FailureMessage string                     `json:"failureMessage"`
}

func EpisodeBatchGenerateShotVideosWorkflow(ctx workflow.Context, input TextToStoryboardInput) (result BatchShotProductionOutput, resultErr error) {
	completionStarted := false
	defer finalizeFailedBatchShotProduction(ctx, input, &result, &resultErr, &completionStarted)
	options := resolveBatchShotProductionOptions(input.Input, DefaultShotVideoConcurrency, MaxShotVideoConcurrency)
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var plans []EpisodeVideoProductionPlan
	if err := workflow.ExecuteActivity(activityCtx, "PrepareEpisodeVideoProductions", PrepareEpisodeVideoProductionsInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, Options: options,
	}).Get(activityCtx, &plans); err != nil {
		if isWorkflowCancellationError(err) {
			return result, err
		}
		result = failedBatchShotVideoOutput(input, options.ShotIDs, err)
		completionStarted = true
		if completeErr := workflow.ExecuteActivity(activityCtx, "CompleteBatchShotProductionWorkflow", input, result).Get(activityCtx, nil); completeErr != nil {
			return result, completeErr
		}
		return result, nil
	}
	result = newBatchShotVideoOutput(input, options.ShotIDs)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for start := 0; start < len(plans); start += defaultEpisodeWorkflowParallel {
		end := start + defaultEpisodeWorkflowParallel
		if end > len(plans) {
			end = len(plans)
		}
		futures := make([]workflow.ChildWorkflowFuture, 0, end-start)
		for _, plan := range plans[start:end] {
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:          plan.TemporalWorkflowID,
				ParentClosePolicy:   enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
				WaitForCancellation: true,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
			})
			futures = append(futures, workflow.ExecuteChildWorkflow(childCtx, EpisodeVideoProductionWorkflow, EpisodeVideoProductionInput{Plan: plan, Options: options}))
		}
		for offset, future := range futures {
			var episodeOutput BatchShotProductionOutput
			if err := future.Get(ctx, &episodeOutput); err != nil {
				if isWorkflowCancellationError(err) {
					return BatchShotProductionOutput{}, err
				}
				code, message := workflowExecutionError(err)
				for _, shotID := range plans[start+offset].TargetShotIDs {
					result.FailedShotIDs = appendUniqueWorkflowString(result.FailedShotIDs, shotID)
					result.ErrorCodes[shotID] = code
					result.Errors[shotID] = message
				}
				if stopOnBalance && !stopScheduling {
					if billingCode, billingMessage, ok := billingInsufficientBalanceFailure(err); ok {
						stopScheduling = true
						stopCode = billingCode
						stopMessage = billingMessage
					}
				}
				continue
			}
			mergeBatchShotVideoOutput(&result, episodeOutput)
			if stopOnBalance && !stopScheduling {
				if code, message, ok := batchShotBillingInsufficientBalance(episodeOutput); ok {
					stopScheduling = true
					stopCode = code
					stopMessage = message
				}
			}
		}
		if stopScheduling {
			code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
			for index := end; index < len(plans); index++ {
				if err := workflow.ExecuteActivity(
					activityCtx,
					"FailEpisodeVideoProductionCheckpoint",
					FailEpisodeVideoProductionCheckpointInput{
						Plan: plans[index], FailureCode: code, FailureMessage: message,
					},
				).Get(activityCtx, nil); err != nil {
					return result, err
				}
				markBatchShotTargetsUnstartedForBalance(
					&result,
					plans[index].TargetShotIDs,
					code,
					message,
				)
			}
			break
		}
	}
	result.Status = batchShotOutputStatus(result)
	completionStarted = true
	if err := workflow.ExecuteActivity(activityCtx, "CompleteBatchShotProductionWorkflow", input, result).Get(activityCtx, nil); err != nil {
		return result, err
	}
	return result, nil
}

func EpisodeVideoProductionWorkflow(ctx workflow.Context, input EpisodeVideoProductionInput) (BatchShotProductionOutput, error) {
	version := workflow.GetVersion(ctx, "episode-video-runtime-v2", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		return episodeVideoProductionWorkflowV1(ctx, input)
	}
	return episodeVideoProductionWorkflowV2(ctx, input)
}

func episodeVideoProductionWorkflowV1(ctx workflow.Context, input EpisodeVideoProductionInput) (result BatchShotProductionOutput, resultErr error) {
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	defer func() {
		if resultErr == nil || workflow.IsContinueAsNewError(resultErr) {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		cleanupCtx = workflow.WithActivityOptions(cleanupCtx, defaultActivityOptions())
		if isWorkflowCancellationError(resultErr) {
			_ = workflow.ExecuteActivity(cleanupCtx, "CancelEpisodeVideoProductionCheckpoint", input.Plan).Get(cleanupCtx, nil)
			return
		}
		failureCode, failureMessage := workflowExecutionError(resultErr)
		_ = workflow.ExecuteActivity(cleanupCtx, "FailEpisodeVideoProductionCheckpoint", FailEpisodeVideoProductionCheckpointInput{
			Plan: input.Plan, FailureCode: failureCode, FailureMessage: failureMessage,
		}).Get(cleanupCtx, nil)
	}()
	for batchCount := 0; ; batchCount++ {
		var batch EpisodeVideoProductionBatch
		if err := workflow.ExecuteActivity(activityCtx, "PrepareEpisodeVideoProductionBatch", input).Get(activityCtx, &batch); err != nil {
			return result, err
		}
		if batch.Done {
			return batch.FinalOutput, nil
		}
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:          fmt.Sprintf("%s:batch:%04d", input.Plan.TemporalWorkflowID, batch.Ordinal),
			ParentClosePolicy:   enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
			WaitForCancellation: true,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		var batchOutput BatchShotProductionOutput
		if err := workflow.ExecuteChildWorkflow(childCtx, SceneOrShotBatchWorkflow, SceneOrShotBatchInput{
			Plan: input.Plan, Batch: batch, Options: input.Options,
		}).Get(ctx, &batchOutput); err != nil {
			if isWorkflowCancellationError(err) {
				return result, err
			}
			batchOutput = newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID}, shotExecutionIDs(batch.Shots))
			code, message := workflowExecutionError(err)
			for _, shot := range batch.Shots {
				batchOutput.FailedShotIDs = append(batchOutput.FailedShotIDs, shot.ShotID)
				batchOutput.ErrorCodes[shot.ShotID] = code
				batchOutput.Errors[shot.ShotID] = message
			}
			batchOutput.Status = "failed"
		}
		var committed CommitEpisodeVideoProductionBatchOutput
		if err := workflow.ExecuteActivity(activityCtx, "CommitEpisodeVideoProductionBatch", CommitEpisodeVideoProductionBatchInput{
			Plan: input.Plan, Batch: batch, Output: batchOutput,
		}).Get(activityCtx, &committed); err != nil {
			return result, err
		}
		if !committed.HasMore {
			return committed.FinalOutput, nil
		}
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() || batchCount+1 >= maximumEpisodeBatchesPerRun {
			nextInput := input
			nextInput.Plan.TargetShotIDs = nil
			return BatchShotProductionOutput{}, workflow.NewContinueAsNewError(ctx, EpisodeVideoProductionWorkflow, nextInput)
		}
	}
}

func episodeVideoProductionWorkflowV2(ctx workflow.Context, input EpisodeVideoProductionInput) (result BatchShotProductionOutput, resultErr error) {
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	defer func() {
		if resultErr == nil || workflow.IsContinueAsNewError(resultErr) {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		cleanupCtx = workflow.WithActivityOptions(cleanupCtx, defaultActivityOptions())
		if isWorkflowCancellationError(resultErr) {
			_ = workflow.ExecuteActivity(cleanupCtx, "CancelEpisodeVideoProductionCheckpoint", input.Plan).Get(cleanupCtx, nil)
			return
		}
		failureCode, failureMessage := workflowExecutionError(resultErr)
		_ = workflow.ExecuteActivity(cleanupCtx, "FailEpisodeVideoProductionCheckpoint", FailEpisodeVideoProductionCheckpointInput{
			Plan: input.Plan, FailureCode: failureCode, FailureMessage: failureMessage,
		}).Get(cleanupCtx, nil)
	}()
	loadFinalOutput := func() (BatchShotProductionOutput, error) {
		var output BatchShotProductionOutput
		err := workflow.ExecuteActivity(activityCtx, "LoadEpisodeVideoProductionOutputV2", input.Plan).Get(activityCtx, &output)
		return output, err
	}
	for batchCount := 0; ; batchCount++ {
		var batch EpisodeVideoProductionBatch
		if err := workflow.ExecuteActivity(activityCtx, "PrepareEpisodeVideoProductionBatchV2", input).Get(activityCtx, &batch); err != nil {
			return result, err
		}
		if batch.Done {
			if err := workflow.ExecuteActivity(activityCtx, "ReconcileEpisodeVideoProductionCheckpointV2", input.Plan).Get(activityCtx, nil); err != nil {
				return result, err
			}
			return loadFinalOutput()
		}
		if batch.NeedsReconcile {
			if err := workflow.ExecuteActivity(activityCtx, "ReconcileEpisodeVideoProductionCheckpointV2", input.Plan).Get(activityCtx, nil); err != nil {
				return result, err
			}
			return loadFinalOutput()
		}
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:          fmt.Sprintf("%s:batch:%04d", input.Plan.TemporalWorkflowID, batch.Ordinal),
			ParentClosePolicy:   enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
			WaitForCancellation: true,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		var batchOutput BatchShotProductionOutput
		if err := workflow.ExecuteChildWorkflow(childCtx, SceneOrShotBatchWorkflow, SceneOrShotBatchInput{
			Plan: input.Plan, Batch: batch, Options: input.Options,
		}).Get(ctx, &batchOutput); err != nil {
			if isWorkflowCancellationError(err) {
				return result, err
			}
			batchOutput = newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID}, shotExecutionIDs(batch.Shots))
			code, message := workflowExecutionError(err)
			for _, shot := range batch.Shots {
				batchOutput.FailedShotIDs = append(batchOutput.FailedShotIDs, shot.ShotID)
				batchOutput.ErrorCodes[shot.ShotID] = code
				batchOutput.Errors[shot.ShotID] = message
			}
			batchOutput.Status = "failed"
		}
		var committed CommitEpisodeVideoProductionBatchOutput
		if err := workflow.ExecuteActivity(activityCtx, "CommitEpisodeVideoProductionBatchV2", CommitEpisodeVideoProductionBatchInput{
			Plan: input.Plan, Batch: batch, Output: batchOutput,
		}).Get(activityCtx, &committed); err != nil {
			return result, err
		}
		if !committed.HasMore {
			if err := workflow.ExecuteActivity(activityCtx, "ReconcileEpisodeVideoProductionCheckpointV2", input.Plan).Get(activityCtx, nil); err != nil {
				return result, err
			}
			return loadFinalOutput()
		}
		if stopOnBalance {
			if code, message, ok := batchShotBillingInsufficientBalance(batchOutput); ok {
				code, message = unstartedBillingInsufficientBalanceFailure(code, message)
				if err := workflow.ExecuteActivity(
					activityCtx,
					"FailEpisodeVideoProductionCheckpoint",
					FailEpisodeVideoProductionCheckpointInput{
						Plan: input.Plan, FailureCode: code, FailureMessage: message,
					},
				).Get(activityCtx, nil); err != nil {
					return result, err
				}
				output, err := loadFinalOutput()
				if err != nil {
					return result, err
				}
				markBatchShotTargetsUnstartedForBalance(
					&output,
					input.Plan.TargetShotIDs,
					code,
					message,
				)
				output.Status = batchShotOutputStatus(output)
				return output, nil
			}
		}
		if workflow.GetInfo(ctx).GetContinueAsNewSuggested() || batchCount+1 >= maximumEpisodeBatchesPerRun {
			nextInput := input
			nextInput.Plan.TargetShotIDs = nil
			return BatchShotProductionOutput{}, workflow.NewContinueAsNewError(ctx, EpisodeVideoProductionWorkflow, nextInput)
		}
	}
}

func SceneOrShotBatchWorkflow(ctx workflow.Context, input SceneOrShotBatchInput) (BatchShotProductionOutput, error) {
	result := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: input.Plan.WorkflowRunID}, shotExecutionIDs(input.Batch.Shots))
	results := workflow.NewBufferedChannel(ctx, len(input.Batch.Shots))
	for _, shot := range input.Batch.Shots {
		currentShot := shot
		workflow.Go(ctx, func(shotCtx workflow.Context) {
			output, err := executeEpisodeVideoShot(shotCtx, input.Plan, currentShot, input.Options)
			item := episodeVideoShotResult{Output: output, ShotID: currentShot.ShotID}
			if err != nil {
				item.ErrorCode, item.Error = workflowExecutionError(err)
				item.Cancelled = isWorkflowCancellationError(err)
			}
			results.SendAsync(item)
		})
	}
	drainCtx, _ := workflow.NewDisconnectedContext(ctx)
	cancelled := false
	for range input.Batch.Shots {
		var item episodeVideoShotResult
		results.Receive(drainCtx, &item)
		if item.Error != "" {
			if item.Cancelled {
				cancelled = true
				result.CancelledShotIDs = append(result.CancelledShotIDs, item.ShotID)
			} else {
				result.FailedShotIDs = append(result.FailedShotIDs, item.ShotID)
				result.ErrorCodes[item.ShotID] = item.ErrorCode
				result.Errors[item.ShotID] = item.Error
			}
			continue
		}
		mergeBatchShotVideoOutput(&result, item.Output)
	}
	if cancelled {
		return BatchShotProductionOutput{}, temporal.NewCanceledError("episode video batch was cancelled")
	}
	result.Status = batchShotOutputStatus(result)
	return result, nil
}

func executeEpisodeVideoShot(
	ctx workflow.Context,
	plan EpisodeVideoProductionPlan,
	shot ShotVideoExecutionShot,
	options BatchShotProductionOptions,
) (BatchShotProductionOutput, error) {
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	createOptions := defaultActivityOptions()
	createOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createOptions)
	textInput := TextToStoryboardInput{
		OrganizationID: plan.OrganizationID,
		ProjectID:      plan.ProjectID,
		WorkflowRunID:  plan.WorkflowRunID,
		CreatedBy:      plan.CreatedBy,
		Prompt:         "batch_generate_shot_videos",
	}
	output := newBatchShotVideoOutput(textInput, []string{shot.ShotID})
	rendered, err := executeShotRenderPlan(activityCtx, createCtx, ShotRenderExecutionInput{
		OrganizationID:   plan.OrganizationID,
		ProjectID:        plan.ProjectID,
		WorkflowRunID:    plan.WorkflowRunID,
		OperationID:      plan.CheckpointID,
		OperationItemID:  shot.OperationItemID,
		OperationAttempt: shot.OperationItemAttempt,
		CreatedBy:        plan.CreatedBy,
		ShotID:           shot.ShotID,
		ShotIndex:        shot.ShotIndex,
		ShotNo:           shot.ShotNo,
		WorkflowPrompt:   "batch_generate_shot_videos",
		FailureScope:     workflowFailureScopeBatchItem,
		AspectRatio:      options.AspectRatio,
		Resolution:       options.Resolution,
		AudioStrategy:    options.AudioStrategy,
		AudioRequirement: options.AudioRequirement,
		Force:            options.Force,
		MaxPolls:         options.MaxPolls,
		PollInterval:     time.Duration(options.PollIntervalSeconds) * time.Second,
	})
	if err != nil {
		return BatchShotProductionOutput{}, err
	}
	appendRenderedShotVideoOutput(&output, shot.ShotID, rendered)
	output.Status = batchShotOutputStatus(output)
	return output, nil
}

func (a Activities) PrepareEpisodeVideoProductions(ctx context.Context, input PrepareEpisodeVideoProductionsInput) ([]EpisodeVideoProductionPlan, error) {
	shotIDs := normalizeStringSlice(input.Options.ShotIDs)
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || len(shotIDs) == 0 {
		return nil, fmt.Errorf("organizationId, projectId, workflowRunId, and shotIds are required")
	}
	var generationID, bindingID, profileVersionID, profileHash string
	var bindingRevision int64
	if err := a.db.QueryRow(ctx, `
		SELECT run.production_generation_id::text, run.video_production_binding_id::text,
		       run.video_production_binding_revision, binding.profile_version_id::text,
		       binding.profile_snapshot_hash
		FROM workflow_runs run
		JOIN projects project ON project.id = run.project_id
		JOIN project_video_production_generations generation
		  ON generation.id = run.production_generation_id AND generation.status = 'active'
		JOIN project_video_production_bindings binding
		  ON binding.id = run.video_production_binding_id AND binding.status = 'active'
		WHERE run.id = $1 AND run.project_id = $2 AND run.organization_id = $3
		  AND project.active_video_production_generation_id = generation.id
		  AND binding.revision = run.video_production_binding_revision
	`, input.WorkflowRunID, input.ProjectID, input.OrganizationID).Scan(
		&generationID, &bindingID, &bindingRevision, &profileVersionID, &profileHash,
	); err != nil {
		return nil, err
	}
	rows, err := a.db.Query(ctx, `
		SELECT shot.id::text, shot.shot_index, COALESCE(shot.shot_no, shot.shot_index + 1),
		       shot.script_episode_id::text, episode.episode_index, episode.episode_title
		FROM storyboard_shots shot
		JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id AND plan.active = true AND plan.status = 'ready'
		JOIN script_episodes episode ON episode.id = shot.script_episode_id
		WHERE shot.organization_id = $1 AND shot.project_id = $2
		  AND shot.id::text = ANY($3::text[]) AND shot.deleted_at IS NULL
		  AND shot.production_generation_id = $4
		ORDER BY episode.episode_index, shot.shot_index, shot.id
	`, input.OrganizationID, input.ProjectID, shotIDs, generationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type episodeGroup struct {
		ID    string
		Index int
		Title string
		Shots []ShotVideoExecutionShot
	}
	groups := make([]episodeGroup, 0)
	groupIndex := make(map[string]int)
	found := make(map[string]bool, len(shotIDs))
	for rows.Next() {
		var shot ShotVideoExecutionShot
		var episodeID, episodeTitle string
		var episodeIndex int
		if err := rows.Scan(&shot.ShotID, &shot.ShotIndex, &shot.ShotNo, &episodeID, &episodeIndex, &episodeTitle); err != nil {
			return nil, err
		}
		found[shot.ShotID] = true
		index, ok := groupIndex[episodeID]
		if !ok {
			index = len(groups)
			groupIndex[episodeID] = index
			groups = append(groups, episodeGroup{ID: episodeID, Index: episodeIndex, Title: episodeTitle})
		}
		groups[index].Shots = append(groups[index].Shots, shot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(found) != len(shotIDs) {
		return nil, fmt.Errorf("one or more storyboard shots are missing from the active production generation")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	plans := make([]EpisodeVideoProductionPlan, 0, len(groups))
	for _, group := range groups {
		targetIDs := shotExecutionIDs(group.Shots)
		targetHash := hashEpisodeVideoValue(targetIDs)
		temporalWorkflowID := fmt.Sprintf("episode-video:%s:%s:%s:%s", input.ProjectID, generationID, group.ID, input.WorkflowRunID)
		var checkpointID string
		var existingWorkflowRunID, existingTargetHash, existingWorkflowStatus string
		err := tx.QueryRow(ctx, `
			SELECT checkpoint.id::text, COALESCE(checkpoint.workflow_run_id::text, ''),
			       COALESCE(checkpoint.metadata->>'targetHash', ''), COALESCE(run.status, '')
			FROM episode_video_production_checkpoints checkpoint
			LEFT JOIN workflow_runs run ON run.id = checkpoint.workflow_run_id
			WHERE checkpoint.project_id = $1 AND checkpoint.production_generation_id = $2
			  AND checkpoint.script_episode_id = $3
			  AND checkpoint.status IN ('queued', 'running', 'cancelling')
			FOR UPDATE OF checkpoint
		`, input.ProjectID, generationID, group.ID).Scan(
			&checkpointID, &existingWorkflowRunID, &existingTargetHash, &existingWorkflowStatus,
		)
		if err == nil {
			if existingWorkflowRunID != input.WorkflowRunID && isTerminalEpisodeParentWorkflowStatus(existingWorkflowStatus) {
				abandonedPlan := EpisodeVideoProductionPlan{
					CheckpointID: checkpointID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
					WorkflowRunID: existingWorkflowRunID, ScriptEpisodeID: group.ID,
					ProductionGenerationID: generationID, VideoProductionBindingID: bindingID,
					VideoProductionBindingRevision: bindingRevision, ProductionProfileVersionID: profileVersionID,
					ProductionProfileSnapshotHash: profileHash,
				}
				if err := cancelEpisodeVideoProductionCheckpointTx(ctx, tx, abandonedPlan, "父工作流已结束，自动回收遗留的视频生产检查点"); err != nil {
					return nil, err
				}
				err = pgx.ErrNoRows
			} else if existingWorkflowRunID != input.WorkflowRunID || existingTargetHash != targetHash {
				return nil, fmt.Errorf("episode %s already has an active video production checkpoint", group.ID)
			}
		}
		if errors.Is(err, pgx.ErrNoRows) {
			metadata := mustJSON(map[string]any{
				"targetShotIds": targetIDs, "targetHash": targetHash,
				"episodeIndex": group.Index, "episodeTitle": group.Title,
			})
			if err := tx.QueryRow(ctx, `
				INSERT INTO episode_video_production_checkpoints(
					organization_id, project_id, production_generation_id,
					video_production_binding_id, video_production_binding_revision,
					script_episode_id, profile_version_id, profile_snapshot_hash,
					workflow_run_id, temporal_workflow_id, status, metadata
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'queued', $11::jsonb)
				RETURNING id::text
			`, input.OrganizationID, input.ProjectID, generationID, bindingID, bindingRevision,
				group.ID, profileVersionID, profileHash, input.WorkflowRunID, temporalWorkflowID, metadata,
			).Scan(&checkpointID); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		plans = append(plans, EpisodeVideoProductionPlan{
			CheckpointID: checkpointID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
			WorkflowRunID: input.WorkflowRunID, CreatedBy: input.CreatedBy,
			ScriptEpisodeID: group.ID, EpisodeIndex: group.Index, EpisodeTitle: group.Title,
			ProductionGenerationID: generationID, VideoProductionBindingID: bindingID,
			VideoProductionBindingRevision: bindingRevision, ProductionProfileVersionID: profileVersionID,
			ProductionProfileSnapshotHash: profileHash, TemporalWorkflowID: temporalWorkflowID, TargetShotIDs: targetIDs,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	sortedEpisodeVideoPlans(plans)
	return plans, nil
}

func (a Activities) PrepareEpisodeVideoProductionBatch(ctx context.Context, input EpisodeVideoProductionInput) (EpisodeVideoProductionBatch, error) {
	return a.prepareEpisodeVideoProductionBatch(ctx, input, false)
}

func (a Activities) PrepareEpisodeVideoProductionBatchV2(ctx context.Context, input EpisodeVideoProductionInput) (EpisodeVideoProductionBatch, error) {
	return a.prepareEpisodeVideoProductionBatch(ctx, input, true)
}

func (a Activities) prepareEpisodeVideoProductionBatch(ctx context.Context, input EpisodeVideoProductionInput, runtimeV2 bool) (EpisodeVideoProductionBatch, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	defer tx.Rollback(ctx)
	var status string
	var nextOrdinal int
	var metadata json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT checkpoint.status, checkpoint.next_batch_ordinal, checkpoint.metadata
		FROM episode_video_production_checkpoints checkpoint
		JOIN projects project ON project.id = checkpoint.project_id
		WHERE checkpoint.id = $1 AND checkpoint.project_id = $2
		  AND checkpoint.production_generation_id = $3
		  AND checkpoint.video_production_binding_id = $4
		  AND checkpoint.video_production_binding_revision = $5
		  AND project.active_video_production_generation_id = checkpoint.production_generation_id
		FOR UPDATE OF checkpoint
	`, input.Plan.CheckpointID, input.Plan.ProjectID, input.Plan.ProductionGenerationID,
		input.Plan.VideoProductionBindingID, input.Plan.VideoProductionBindingRevision).Scan(&status, &nextOrdinal, &metadata); err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	if isTerminalEpisodeVideoCheckpoint(status) {
		if runtimeV2 {
			return EpisodeVideoProductionBatch{CheckpointID: input.Plan.CheckpointID, Done: true}, tx.Commit(ctx)
		}
		output, err := loadEpisodeVideoProductionOutputTx(ctx, tx, input.Plan)
		return EpisodeVideoProductionBatch{CheckpointID: input.Plan.CheckpointID, Done: true, FinalOutput: output}, err
	}
	var values struct {
		TargetShotIDs []string `json:"targetShotIds"`
	}
	if err := json.Unmarshal(metadata, &values); err != nil || len(values.TargetShotIDs) == 0 {
		return EpisodeVideoProductionBatch{}, fmt.Errorf("episode video checkpoint target list is invalid")
	}
	if existing, found, err := loadRunningEpisodeVideoBatchTx(ctx, tx, input.Plan, nextOrdinal, runtimeV2); err != nil {
		return EpisodeVideoProductionBatch{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return EpisodeVideoProductionBatch{}, err
		}
		return existing, nil
	}
	processed := make(map[string]bool)
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT item.storyboard_shot_id::text
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		WHERE batch.checkpoint_id = $1
	`, input.Plan.CheckpointID)
	if err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	for rows.Next() {
		var shotID string
		if err := rows.Scan(&shotID); err != nil {
			rows.Close()
			return EpisodeVideoProductionBatch{}, err
		}
		processed[shotID] = true
	}
	rows.Close()
	remaining := make([]string, 0)
	for _, shotID := range values.TargetShotIDs {
		if !processed[shotID] {
			remaining = append(remaining, shotID)
		}
	}
	if len(remaining) == 0 {
		if runtimeV2 {
			if err := tx.Commit(ctx); err != nil {
				return EpisodeVideoProductionBatch{}, err
			}
			return EpisodeVideoProductionBatch{CheckpointID: input.Plan.CheckpointID, NeedsReconcile: true}, nil
		}
		checkpointStatus, err := reconcileEpisodeVideoCheckpointTx(ctx, tx, input.Plan, len(values.TargetShotIDs))
		if err != nil {
			return EpisodeVideoProductionBatch{}, err
		}
		output, err := loadEpisodeVideoProductionOutputTx(ctx, tx, input.Plan)
		if err != nil {
			return EpisodeVideoProductionBatch{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return EpisodeVideoProductionBatch{}, err
		}
		output.Status = checkpointStatus
		return EpisodeVideoProductionBatch{CheckpointID: input.Plan.CheckpointID, Done: true, FinalOutput: output}, nil
	}
	batchSize := input.Options.MaxConcurrency
	if batchSize <= 0 {
		batchSize = defaultEpisodeVideoBatchSize
	}
	if batchSize > maximumEpisodeVideoBatchSize {
		batchSize = maximumEpisodeVideoBatchSize
	}
	remaining, err = selectEpisodeVideoBatchShotIDsTx(ctx, tx, input.Plan, remaining, batchSize)
	if err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	prepared, dependencyHash, err := loadPreparedEpisodeVideoShotsTx(ctx, tx, input.Plan, remaining)
	if err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	var batchID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO episode_video_production_batches(
			checkpoint_id, ordinal, dependency_snapshot_hash, workflow_run_id,
			temporal_workflow_id, status, total_items, started_at, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 'running', $6, now(), '{}'::jsonb)
		RETURNING id::text
	`, input.Plan.CheckpointID, nextOrdinal, dependencyHash, input.Plan.WorkflowRunID,
		fmt.Sprintf("%s:batch:%04d", input.Plan.TemporalWorkflowID, nextOrdinal), len(prepared)).Scan(&batchID); err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	for index := range prepared {
		shot := &prepared[index]
		var itemID string
		if runtimeV2 {
			attempt, err := nextEpisodeVideoItemAttemptTx(ctx, tx, input.Plan, shot.Shot.ShotID)
			if err != nil {
				return EpisodeVideoProductionBatch{}, err
			}
			if err := tx.QueryRow(ctx, `
			INSERT INTO episode_video_production_items(
				batch_id, storyboard_shot_id, shot_state_hash, reference_pack_id,
				video_prompt_plan_id, video_render_plan_id, predecessor_video_render_plan_id,
				execution_identity_version, attempt, status, started_at
			)
			VALUES ($1, $2, $3, $4, $5, NULL, $6, 2, $7, 'running', now())
			RETURNING id::text
			`, batchID, shot.Shot.ShotID, shot.ShotStateHash, shot.ReferencePackID,
				shot.VideoPromptPlanID, shot.VideoRenderPlanID, attempt).Scan(&itemID); err != nil {
				return EpisodeVideoProductionBatch{}, err
			}
			shot.Shot.OperationItemID = itemID
			shot.Shot.OperationItemAttempt = attempt
			shot.Shot.PredecessorExecutionPlanID = shot.VideoRenderPlanID
		} else if err := tx.QueryRow(ctx, `
			INSERT INTO episode_video_production_items(
				batch_id, storyboard_shot_id, shot_state_hash, reference_pack_id,
				video_prompt_plan_id, video_render_plan_id, status, started_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'running', now())
			RETURNING id::text
		`, batchID, shot.Shot.ShotID, shot.ShotStateHash, shot.ReferencePackID,
			shot.VideoPromptPlanID, shot.VideoRenderPlanID).Scan(&itemID); err != nil {
			return EpisodeVideoProductionBatch{}, err
		}
		if err := insertEvent(ctx, tx, input.Plan.OrganizationID, input.Plan.ProjectID,
			"video.production.item.started", "episode_video_item", itemID,
			mustJSON(episodeVideoEventPayload(input.Plan, batchID, itemID, shot.Shot.ShotID, "running")),
		); err != nil {
			return EpisodeVideoProductionBatch{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_checkpoints
		SET status = 'running', updated_at = now()
		WHERE id = $1
	`, input.Plan.CheckpointID); err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	if err := insertEvent(ctx, tx, input.Plan.OrganizationID, input.Plan.ProjectID,
		"video.production.batch.started", "episode_video_batch", batchID,
		mustJSON(episodeVideoEventPayload(input.Plan, batchID, "", "", "running")),
	); err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EpisodeVideoProductionBatch{}, err
	}
	shots := make([]ShotVideoExecutionShot, 0, len(prepared))
	for _, item := range prepared {
		shots = append(shots, item.Shot)
	}
	return EpisodeVideoProductionBatch{
		BatchID: batchID, CheckpointID: input.Plan.CheckpointID, Ordinal: nextOrdinal,
		DependencyHash: dependencyHash, Shots: shots,
	}, nil
}

func nextEpisodeVideoItemAttemptTx(ctx context.Context, tx pgx.Tx, plan EpisodeVideoProductionPlan, shotID string) (int, error) {
	var attempt int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(item.attempt), 0) + 1
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		WHERE checkpoint.project_id = $1
		  AND checkpoint.production_generation_id = $2
		  AND item.storyboard_shot_id = $3
	`, plan.ProjectID, plan.ProductionGenerationID, shotID).Scan(&attempt); err != nil {
		return 0, err
	}
	return attempt, nil
}

func selectEpisodeVideoBatchShotIDsTx(
	ctx context.Context,
	tx pgx.Tx,
	plan EpisodeVideoProductionPlan,
	remaining []string,
	limit int,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT shot.id::text
		FROM storyboard_shots shot
		WHERE shot.organization_id = $1 AND shot.project_id = $2
		  AND shot.id::text = ANY($3::text[])
		  AND shot.production_generation_id = $4 AND shot.deleted_at IS NULL
		ORDER BY shot.shot_index, shot.id
	`, plan.OrganizationID, plan.ProjectID, remaining, plan.ProductionGenerationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	available := make(map[string]bool, len(remaining))
	for rows.Next() {
		var shotID string
		if err := rows.Scan(&shotID); err != nil {
			return nil, err
		}
		available[shotID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return selectIndependentEpisodeVideoShots(remaining, available, limit)
}

func selectIndependentEpisodeVideoShots(remaining []string, available map[string]bool, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("episode video batch concurrency must be positive")
	}
	selected := make([]string, 0, min(limit, len(remaining)))
	for _, shotID := range remaining {
		if !available[shotID] {
			return nil, fmt.Errorf("storyboard shot %s is missing from the active production generation", shotID)
		}
		selected = append(selected, shotID)
		if len(selected) >= limit {
			break
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("episode video batch has no schedulable shots")
	}
	return selected, nil
}

func loadRunningEpisodeVideoBatchTx(
	ctx context.Context,
	tx pgx.Tx,
	plan EpisodeVideoProductionPlan,
	ordinal int,
	runtimeV2 bool,
) (EpisodeVideoProductionBatch, bool, error) {
	var batch EpisodeVideoProductionBatch
	var status string
	err := tx.QueryRow(ctx, `
		SELECT id::text, dependency_snapshot_hash, status
		FROM episode_video_production_batches
		WHERE checkpoint_id = $1 AND ordinal = $2
		ORDER BY attempt DESC
		LIMIT 1
	`, plan.CheckpointID, ordinal).Scan(&batch.BatchID, &batch.DependencyHash, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return EpisodeVideoProductionBatch{}, false, nil
	}
	if err != nil {
		return EpisodeVideoProductionBatch{}, false, err
	}
	if status != "queued" && status != "running" {
		return EpisodeVideoProductionBatch{}, false, fmt.Errorf(
			"episode video checkpoint %s points to terminal batch ordinal %d",
			plan.CheckpointID,
			ordinal,
		)
	}
	rows, err := tx.Query(ctx, `
		SELECT shot.id::text, shot.shot_index, COALESCE(shot.shot_no, shot.shot_index + 1),
		       CASE WHEN item.execution_identity_version = 2 THEN item.id::text ELSE '' END,
		       CASE WHEN item.execution_identity_version = 2 THEN item.attempt ELSE 0 END,
		       COALESCE(item.predecessor_video_render_plan_id::text, '')
		FROM episode_video_production_items item
		JOIN storyboard_shots shot ON shot.id = item.storyboard_shot_id
		WHERE item.batch_id = $1
		ORDER BY shot.shot_index, shot.id
	`, batch.BatchID)
	if err != nil {
		return EpisodeVideoProductionBatch{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var shot ShotVideoExecutionShot
		if err := rows.Scan(&shot.ShotID, &shot.ShotIndex, &shot.ShotNo,
			&shot.OperationItemID, &shot.OperationItemAttempt, &shot.PredecessorExecutionPlanID); err != nil {
			return EpisodeVideoProductionBatch{}, false, err
		}
		if runtimeV2 && shot.OperationItemID == "" {
			return EpisodeVideoProductionBatch{}, false, fmt.Errorf("episode video batch %s contains a legacy item in v2 execution", batch.BatchID)
		}
		batch.Shots = append(batch.Shots, shot)
	}
	if err := rows.Err(); err != nil {
		return EpisodeVideoProductionBatch{}, false, err
	}
	if len(batch.Shots) == 0 {
		return EpisodeVideoProductionBatch{}, false, fmt.Errorf("episode video batch %s has no durable items", batch.BatchID)
	}
	batch.CheckpointID = plan.CheckpointID
	batch.Ordinal = ordinal
	return batch, true, nil
}

func reconcileEpisodeVideoCheckpointTx(
	ctx context.Context,
	tx pgx.Tx,
	plan EpisodeVideoProductionPlan,
	targetCount int,
) (string, error) {
	var processedCount, succeededCount, failedCount, cancelledCount, activeCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT item.storyboard_shot_id),
		       count(*) FILTER (WHERE item.status = 'succeeded'),
		       count(*) FILTER (WHERE item.status = 'failed'),
		       count(*) FILTER (WHERE item.status = 'cancelled'),
		       count(*) FILTER (WHERE item.status IN ('queued', 'running', 'cancelling'))
		FROM episode_video_production_batches batch
		JOIN episode_video_production_items item ON item.batch_id = batch.id
		WHERE batch.checkpoint_id = $1
	`, plan.CheckpointID).Scan(
		&processedCount,
		&succeededCount,
		&failedCount,
		&cancelledCount,
		&activeCount,
	); err != nil {
		return "", err
	}
	if processedCount != targetCount || activeCount > 0 {
		return "", fmt.Errorf("episode video checkpoint %s cannot be finalized from incomplete durable items", plan.CheckpointID)
	}
	status := "succeeded"
	if failedCount > 0 || cancelledCount > 0 {
		status = "partial_succeeded"
		if succeededCount == 0 {
			status = "failed"
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_checkpoints
		SET status = $2, completed_at = COALESCE(completed_at, now()),
		    updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running')
	`, plan.CheckpointID, status); err != nil {
		return "", err
	}
	return status, nil
}

type preparedEpisodeVideoShot struct {
	Shot              ShotVideoExecutionShot
	ShotStateHash     string
	ReferencePackID   string
	VideoPromptPlanID string
	VideoRenderPlanID string
}

func loadPreparedEpisodeVideoShotsTx(ctx context.Context, tx pgx.Tx, plan EpisodeVideoProductionPlan, shotIDs []string) ([]preparedEpisodeVideoShot, string, error) {
	// The active render plan is predecessor provenance, not the plan that will be
	// executed by this workflow. EnsurePreparedShotVideoPlan creates a fresh plan
	// from the approved prompt and reference contracts, so predecessor expiry must
	// not invalidate an otherwise executable shot.
	rows, err := tx.Query(ctx, `
		SELECT shot.id::text, shot.shot_index, COALESCE(shot.shot_no, shot.shot_index + 1),
		       render_plan.shot_state_hash, render_plan.reference_pack_id::text,
		       render_plan.video_prompt_plan_id::text, render_plan.id::text
		FROM storyboard_shots shot
		JOIN storyboard_plans storyboard_plan ON storyboard_plan.id = shot.storyboard_plan_id
		  AND storyboard_plan.active = true AND storyboard_plan.status = 'ready'
		JOIN video_render_plans render_plan ON render_plan.id = shot.active_video_render_plan_id
		  AND render_plan.active = true
		  AND render_plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
		  AND render_plan.production_generation_id = $4
		  AND render_plan.video_production_binding_id = $6
		  AND render_plan.video_production_binding_revision = $7
		  AND render_plan.profile_version_id = $8
		  AND render_plan.production_profile_snapshot_hash = $9
		JOIN storyboard_shot_state_versions state
		  ON state.storyboard_shot_id = shot.id AND state.state_role = 'planned_entry'
		 AND state.status = 'approved' AND state.production_generation_id = $4
		 AND state.state_hash = render_plan.shot_state_hash
		JOIN shot_reference_packs reference_pack
		  ON reference_pack.id = render_plan.reference_pack_id
		 AND reference_pack.status = 'active' AND reference_pack.production_generation_id = $4
		 AND reference_pack.manifest_hash = render_plan.reference_pack_hash
		 AND reference_pack.manifest->>'purpose' = 'video'
		JOIN video_prompt_plans prompt
		  ON prompt.id = render_plan.video_prompt_plan_id
		 AND prompt.status = 'approved' AND prompt.production_generation_id = $4
		 AND prompt.shot_state_hash = render_plan.shot_state_hash
		 AND prompt.reference_pack_hash = render_plan.reference_pack_hash
		WHERE shot.organization_id = $1 AND shot.project_id = $2
		  AND shot.id::text = ANY($3::text[]) AND shot.script_episode_id = $5
		  AND shot.production_generation_id = $4 AND shot.deleted_at IS NULL
		  AND COALESCE(shot.video_prompt_status, 'not_started') = 'succeeded'
		ORDER BY shot.shot_index, shot.id
	`, plan.OrganizationID, plan.ProjectID, shotIDs, plan.ProductionGenerationID, plan.ScriptEpisodeID,
		plan.VideoProductionBindingID, plan.VideoProductionBindingRevision,
		plan.ProductionProfileVersionID, plan.ProductionProfileSnapshotHash)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]preparedEpisodeVideoShot, 0, len(shotIDs))
	for rows.Next() {
		var item preparedEpisodeVideoShot
		if err := rows.Scan(&item.Shot.ShotID, &item.Shot.ShotIndex, &item.Shot.ShotNo,
			&item.ShotStateHash, &item.ReferencePackID, &item.VideoPromptPlanID, &item.VideoRenderPlanID); err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(items) != len(shotIDs) {
		return nil, "", preparedVideoPromptError("部分镜头缺少已审核提示词、引用包或有效 Render Plan，请先重新生成视频提示词")
	}
	dependency := make([]map[string]string, 0, len(items))
	for _, item := range items {
		dependency = append(dependency, map[string]string{
			"shotId": item.Shot.ShotID, "shotStateHash": item.ShotStateHash,
			"referencePackId": item.ReferencePackID, "videoPromptPlanId": item.VideoPromptPlanID,
			"videoRenderPlanId": item.VideoRenderPlanID,
		})
	}
	return items, hashEpisodeVideoValue(dependency), nil
}

func (a Activities) CommitEpisodeVideoProductionBatch(ctx context.Context, input CommitEpisodeVideoProductionBatchInput) (CommitEpisodeVideoProductionBatchOutput, error) {
	return a.commitEpisodeVideoProductionBatch(ctx, input, false)
}

func (a Activities) CommitEpisodeVideoProductionBatchV2(ctx context.Context, input CommitEpisodeVideoProductionBatchInput) (CommitEpisodeVideoProductionBatchOutput, error) {
	return a.commitEpisodeVideoProductionBatch(ctx, input, true)
}

func (a Activities) commitEpisodeVideoProductionBatch(ctx context.Context, input CommitEpisodeVideoProductionBatchInput, runtimeV2 bool) (CommitEpisodeVideoProductionBatchOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	defer tx.Rollback(ctx)
	var batchStatus string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM episode_video_production_batches
		WHERE id = $1 AND checkpoint_id = $2 FOR UPDATE
	`, input.Batch.BatchID, input.Plan.CheckpointID).Scan(&batchStatus); err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	if batchStatus != "running" {
		var checkpointStatus string
		if err := tx.QueryRow(ctx, `
			SELECT status FROM episode_video_production_checkpoints WHERE id = $1
		`, input.Plan.CheckpointID).Scan(&checkpointStatus); err != nil {
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
		hasMore := !isTerminalEpisodeVideoCheckpoint(checkpointStatus)
		if runtimeV2 {
			return CommitEpisodeVideoProductionBatchOutput{HasMore: hasMore, Status: checkpointStatus}, nil
		}
		output, err := loadEpisodeVideoProductionOutputTx(ctx, tx, input.Plan)
		if err != nil {
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
		return CommitEpisodeVideoProductionBatchOutput{
			HasMore: hasMore, Status: checkpointStatus, FinalOutput: output,
		}, nil
	}
	succeeded := stringSet(input.Output.SucceededShotIDs)
	failed := stringSet(input.Output.FailedShotIDs)
	cancelled := stringSet(input.Output.CancelledShotIDs)
	rows, err := tx.Query(ctx, `
		SELECT id::text, storyboard_shot_id::text,
		       execution_identity_version, COALESCE(video_render_plan_id::text, '')
		FROM episode_video_production_items
		WHERE batch_id = $1 FOR UPDATE
	`, input.Batch.BatchID)
	if err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	type itemRef struct {
		ID, ShotID, RenderPlanID string
		IdentityVersion          int
	}
	items := make([]itemRef, 0)
	for rows.Next() {
		var item itemRef
		if err := rows.Scan(&item.ID, &item.ShotID, &item.IdentityVersion, &item.RenderPlanID); err != nil {
			rows.Close()
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
		items = append(items, item)
	}
	rows.Close()
	succeededCount, failedCount, cancelledCount := 0, 0, 0
	for _, item := range items {
		itemStatus := "failed"
		errorCode := "VIDEO_PRODUCTION_ITEM_FAILED"
		errorMessage := input.Output.Errors[item.ShotID]
		providerAsyncTaskID := input.Output.ProviderAsyncTaskIDs[item.ShotID]
		eventType := "video.production.item.failed"
		switch {
		case succeeded[item.ShotID]:
			if runtimeV2 {
				if item.IdentityVersion != 2 || item.RenderPlanID == "" {
					return CommitEpisodeVideoProductionBatchOutput{}, fmt.Errorf("episode video item %s has no bound v2 render plan", item.ID)
				}
				if err := validateSucceededEpisodeVideoItemTx(ctx, tx, item.ID, item.RenderPlanID); err != nil {
					return CommitEpisodeVideoProductionBatchOutput{}, err
				}
			}
			itemStatus, errorCode, errorMessage = "succeeded", "", ""
			eventType = "video.production.item.completed"
			succeededCount++
		case cancelled[item.ShotID]:
			itemStatus, errorCode = "cancelled", "VIDEO_PRODUCTION_ITEM_CANCELLED"
			eventType = "video.production.item.cancelled"
			cancelledCount++
		default:
			if !failed[item.ShotID] && errorMessage == "" {
				errorMessage = "镜头视频批次未返回终态结果"
			}
			failedCount++
		}
		providerTaskValue := providerAsyncTaskID
		if runtimeV2 {
			providerTaskValue = ""
		}
		command, err := tx.Exec(ctx, `
			UPDATE episode_video_production_items
			SET status = $2, error_code = NULLIF($3, ''),
			    error_detail = CASE WHEN $4 = '' THEN '{}'::jsonb ELSE jsonb_build_object('message', $4::text) END,
			    provider_async_task_id = NULLIF($5, '')::uuid,
			    completed_at = now(), updated_at = now(), revision = revision + 1
			WHERE id = $1 AND status IN ('queued', 'running', 'cancelling')
		`, item.ID, itemStatus, errorCode, errorMessage, providerTaskValue)
		if err != nil {
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
		if command.RowsAffected() != 1 {
			return CommitEpisodeVideoProductionBatchOutput{}, fmt.Errorf("episode video item %s is no longer writable", item.ID)
		}
		if err := insertEvent(ctx, tx, input.Plan.OrganizationID, input.Plan.ProjectID,
			eventType, "episode_video_item", item.ID,
			mustJSON(episodeVideoEventPayload(input.Plan, input.Batch.BatchID, item.ID, item.ShotID, itemStatus)),
		); err != nil {
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
	}
	terminalBatchStatus := "succeeded"
	if failedCount > 0 || cancelledCount > 0 {
		terminalBatchStatus = "partial_succeeded"
		if succeededCount == 0 {
			terminalBatchStatus = "failed"
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_batches
		SET status = $2, succeeded_items = $3, failed_items = $4, cancelled_items = $5,
		    metadata = metadata || jsonb_build_object('batchOutput', $6::jsonb),
		    completed_at = now(), updated_at = now(), revision = revision + 1
		WHERE id = $1
	`, input.Batch.BatchID, terminalBatchStatus, succeededCount, failedCount, cancelledCount, mustJSON(input.Output)); err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	var targetCount, processedCount, totalSucceeded, totalFailed, totalCancelled int
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_array_length(checkpoint.metadata->'targetShotIds'),
		       count(DISTINCT item.storyboard_shot_id),
		       count(*) FILTER (WHERE item.status = 'succeeded'),
		       count(*) FILTER (WHERE item.status = 'failed'),
		       count(*) FILTER (WHERE item.status = 'cancelled')
		FROM episode_video_production_checkpoints checkpoint
		LEFT JOIN episode_video_production_batches batch ON batch.checkpoint_id = checkpoint.id
		LEFT JOIN episode_video_production_items item ON item.batch_id = batch.id
		WHERE checkpoint.id = $1
		GROUP BY checkpoint.id
	`, input.Plan.CheckpointID).Scan(&targetCount, &processedCount, &totalSucceeded, &totalFailed, &totalCancelled); err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	hasMore := processedCount < targetCount
	checkpointStatus := "running"
	if !hasMore && !runtimeV2 {
		checkpointStatus = "succeeded"
		if totalFailed > 0 || totalCancelled > 0 {
			checkpointStatus = "partial_succeeded"
			if totalSucceeded == 0 {
				checkpointStatus = "failed"
			}
		}
	}
	completed := !hasMore && !runtimeV2
	command, err := tx.Exec(ctx, `
		UPDATE episode_video_production_checkpoints
		SET status = $2, next_batch_ordinal = next_batch_ordinal + 1,
		    completed_at = CASE WHEN $3 THEN now() ELSE NULL END,
		    updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running')
	`, input.Plan.CheckpointID, checkpointStatus, completed)
	if err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return CommitEpisodeVideoProductionBatchOutput{}, fmt.Errorf("episode video checkpoint %s is no longer writable", input.Plan.CheckpointID)
	}
	if terminalBatchStatus == "partial_succeeded" || terminalBatchStatus == "failed" {
		eventType := "video.production.batch.partial_succeeded"
		if terminalBatchStatus == "failed" {
			eventType = "video.production.batch.failed"
		}
		if err := insertEvent(ctx, tx, input.Plan.OrganizationID, input.Plan.ProjectID,
			eventType, "episode_video_batch", input.Batch.BatchID,
			mustJSON(episodeVideoEventPayload(input.Plan, input.Batch.BatchID, "", "", terminalBatchStatus)),
		); err != nil {
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
	}
	if err := insertEvent(ctx, tx, input.Plan.OrganizationID, input.Plan.ProjectID,
		"video.production.checkpoint.committed", "episode_video_checkpoint", input.Plan.CheckpointID,
		mustJSON(episodeVideoEventPayload(input.Plan, input.Batch.BatchID, "", "", checkpointStatus)),
	); err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	output := CommitEpisodeVideoProductionBatchOutput{HasMore: hasMore, Status: checkpointStatus}
	if !hasMore && !runtimeV2 {
		output.FinalOutput, err = loadEpisodeVideoProductionOutputTx(ctx, tx, input.Plan)
		if err != nil {
			return CommitEpisodeVideoProductionBatchOutput{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CommitEpisodeVideoProductionBatchOutput{}, err
	}
	return output, nil
}

func validateSucceededEpisodeVideoItemTx(ctx context.Context, tx pgx.Tx, itemID, planID string) error {
	var planStatus string
	var outputArtifactID, outputMediaFileID string
	var segmentCount, succeededSegmentCount, succeededTaskCount int
	if err := tx.QueryRow(ctx, `
		SELECT plan.status,
		       COALESCE(plan.output_artifact_id::text, ''), COALESCE(plan.output_media_file_id::text, ''),
		       count(segment.id)::integer,
		       count(segment.id) FILTER (WHERE segment.status = 'succeeded')::integer,
		       count(task.id) FILTER (WHERE task.status = 'succeeded')::integer
		FROM episode_video_production_items item
		JOIN video_render_plans plan
		  ON plan.id = item.video_render_plan_id
		 AND plan.operation_item_id = item.id
		 AND plan.operation_item_attempt = item.attempt
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		LEFT JOIN provider_async_tasks task
		  ON task.id = segment.provider_async_task_id
		 AND task.video_render_plan_id = plan.id
		 AND task.video_render_segment_id = segment.id
		 AND task.operation_item_id = item.id
		 AND task.operation_item_attempt = item.attempt
		WHERE item.id = $1 AND plan.id = $2 AND item.execution_identity_version = 2
		GROUP BY plan.id
	`, itemID, planID).Scan(
		&planStatus, &outputArtifactID, &outputMediaFileID,
		&segmentCount, &succeededSegmentCount, &succeededTaskCount,
	); err != nil {
		return err
	}
	if planStatus != "succeeded" || outputArtifactID == "" || outputMediaFileID == "" ||
		segmentCount == 0 || succeededSegmentCount != segmentCount || succeededTaskCount != segmentCount {
		return fmt.Errorf("episode video item %s cannot succeed before its exact render plan, segments, tasks, and media are durable", itemID)
	}
	return nil
}

func (a Activities) CancelEpisodeVideoProductionCheckpoint(ctx context.Context, plan EpisodeVideoProductionPlan) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := cancelEpisodeVideoProductionCheckpointTx(ctx, tx, plan, "用户取消分集视频生产"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func cancelEpisodeVideoProductionCheckpointTx(
	ctx context.Context,
	tx pgx.Tx,
	plan EpisodeVideoProductionPlan,
	reason string,
) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "分集视频生产已取消"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_items item
		SET status = 'cancelled', completed_at = now(), updated_at = now(), revision = item.revision + 1,
		    error_code = 'VIDEO_PRODUCTION_ITEM_CANCELLED',
		    error_detail = jsonb_build_object('message', $2::text)
		FROM episode_video_production_batches batch
		WHERE batch.id = item.batch_id AND batch.checkpoint_id = $1
		  AND item.status IN ('queued', 'running', 'cancelling')
	`, plan.CheckpointID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_batches batch
		SET status = 'cancelled', completed_at = now(), updated_at = now(), revision = batch.revision + 1,
		    succeeded_items = counts.succeeded_items,
		    failed_items = counts.failed_items,
		    cancelled_items = counts.cancelled_items,
		    metadata = batch.metadata || jsonb_build_object('cancelReason', $2::text, 'cancelledAt', now())
		FROM (
			SELECT source.id,
			       count(item.id) FILTER (WHERE item.status = 'succeeded')::integer AS succeeded_items,
			       count(item.id) FILTER (WHERE item.status = 'failed')::integer AS failed_items,
			       count(item.id) FILTER (WHERE item.status = 'cancelled')::integer AS cancelled_items
			FROM episode_video_production_batches source
			LEFT JOIN episode_video_production_items item ON item.batch_id = source.id
			WHERE source.checkpoint_id = $1 AND source.status IN ('queued', 'running', 'cancelling')
			GROUP BY source.id
		) counts
		WHERE batch.id = counts.id
	`, plan.CheckpointID, reason); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		UPDATE episode_video_production_checkpoints
		SET status = 'cancelled', completed_at = now(), updated_at = now(), revision = revision + 1,
		    metadata = metadata || jsonb_build_object('cancelReason', $2::text, 'cancelledAt', now())
		WHERE id = $1 AND status IN ('queued', 'running', 'cancelling')
	`, plan.CheckpointID, reason)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return nil
	}
	payload := episodeVideoEventPayload(plan, "", "", "", "cancelled")
	payload["reason"] = reason
	if err := insertEvent(ctx, tx, plan.OrganizationID, plan.ProjectID,
		"video.production.checkpoint.committed", "episode_video_checkpoint", plan.CheckpointID,
		mustJSON(payload),
	); err != nil {
		return err
	}
	return nil
}

func cancelEpisodeVideoProductionCheckpointsForWorkflowTx(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID string,
	reason string,
) error {
	rows, err := tx.Query(ctx, `
		SELECT checkpoint.id::text,
		       checkpoint.organization_id::text,
		       checkpoint.project_id::text,
		       COALESCE(checkpoint.workflow_run_id::text, ''),
		       checkpoint.script_episode_id::text,
		       checkpoint.production_generation_id::text,
		       checkpoint.video_production_binding_id::text,
		       checkpoint.video_production_binding_revision,
		       checkpoint.profile_version_id::text,
		       checkpoint.profile_snapshot_hash,
		       checkpoint.temporal_workflow_id
		FROM episode_video_production_checkpoints checkpoint
		WHERE checkpoint.workflow_run_id = $1
		  AND checkpoint.status IN ('queued', 'running', 'cancelling')
		ORDER BY checkpoint.created_at, checkpoint.id
		FOR UPDATE OF checkpoint
	`, workflowRunID)
	if err != nil {
		return err
	}
	plans := make([]EpisodeVideoProductionPlan, 0)
	for rows.Next() {
		var plan EpisodeVideoProductionPlan
		if err := rows.Scan(
			&plan.CheckpointID,
			&plan.OrganizationID,
			&plan.ProjectID,
			&plan.WorkflowRunID,
			&plan.ScriptEpisodeID,
			&plan.ProductionGenerationID,
			&plan.VideoProductionBindingID,
			&plan.VideoProductionBindingRevision,
			&plan.ProductionProfileVersionID,
			&plan.ProductionProfileSnapshotHash,
			&plan.TemporalWorkflowID,
		); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, plan)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := cancelEpisodeVideoProductionCheckpointTx(ctx, tx, plan, reason); err != nil {
			return err
		}
	}
	return nil
}

func (a Activities) FailEpisodeVideoProductionCheckpoint(ctx context.Context, input FailEpisodeVideoProductionCheckpointInput) error {
	input.FailureCode = strings.TrimSpace(input.FailureCode)
	input.FailureMessage = strings.TrimSpace(input.FailureMessage)
	if input.FailureCode == "" {
		input.FailureCode = codeActivityFailed
	}
	if input.FailureMessage == "" {
		input.FailureMessage = "分集视频生产失败"
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_items item
		SET status = 'failed', completed_at = now(), updated_at = now(), revision = item.revision + 1,
		    error_code = $2,
		    error_detail = jsonb_build_object('message', $3::text)
		FROM episode_video_production_batches batch
		WHERE batch.id = item.batch_id AND batch.checkpoint_id = $1
		  AND item.status IN ('queued', 'running', 'cancelling')
	`, input.Plan.CheckpointID, input.FailureCode, input.FailureMessage); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE episode_video_production_batches batch
		SET status = 'failed', completed_at = now(), updated_at = now(), revision = batch.revision + 1,
		    succeeded_items = counts.succeeded_items,
		    failed_items = counts.failed_items,
		    cancelled_items = counts.cancelled_items,
		    metadata = batch.metadata || jsonb_build_object(
		        'failureCode', $2::text, 'failureMessage', $3::text, 'failedAt', now()
		    )
		FROM (
			SELECT source.id,
			       count(item.id) FILTER (WHERE item.status = 'succeeded')::integer AS succeeded_items,
			       count(item.id) FILTER (WHERE item.status = 'failed')::integer AS failed_items,
			       count(item.id) FILTER (WHERE item.status = 'cancelled')::integer AS cancelled_items
			FROM episode_video_production_batches source
			LEFT JOIN episode_video_production_items item ON item.batch_id = source.id
			WHERE source.checkpoint_id = $1 AND source.status IN ('queued', 'running', 'cancelling')
			GROUP BY source.id
		) counts
		WHERE batch.id = counts.id
	`, input.Plan.CheckpointID, input.FailureCode, input.FailureMessage); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		UPDATE episode_video_production_checkpoints
		SET status = 'failed', completed_at = now(), updated_at = now(), revision = revision + 1,
		    metadata = metadata || jsonb_build_object(
		        'failureCode', $6::text, 'failureMessage', $7::text, 'failedAt', now()
		    )
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND production_generation_id = $4 AND video_production_binding_id = $5
		  AND video_production_binding_revision = $8
		  AND status IN ('queued', 'running', 'cancelling')
	`, input.Plan.CheckpointID, input.Plan.OrganizationID, input.Plan.ProjectID,
		input.Plan.ProductionGenerationID, input.Plan.VideoProductionBindingID,
		input.FailureCode, input.FailureMessage, input.Plan.VideoProductionBindingRevision)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	payload := episodeVideoEventPayload(input.Plan, "", "", "", "failed")
	payload["failureCode"] = input.FailureCode
	payload["failureMessage"] = input.FailureMessage
	if err := insertEvent(ctx, tx, input.Plan.OrganizationID, input.Plan.ProjectID,
		"video.production.checkpoint.failed", "episode_video_checkpoint", input.Plan.CheckpointID,
		mustJSON(payload),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) ReconcileEpisodeVideoProductionCheckpointV2(ctx context.Context, plan EpisodeVideoProductionPlan) error {
	return a.reconcileEpisodeVideoProductionCheckpointV2(ctx, plan)
}

func (a Activities) LoadEpisodeVideoProductionOutputV2(ctx context.Context, plan EpisodeVideoProductionPlan) (BatchShotProductionOutput, error) {
	return loadEpisodeVideoProductionOutputV2(ctx, a.db, plan)
}

type episodeVideoOutputQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadEpisodeVideoProductionOutputV2(ctx context.Context, query episodeVideoOutputQuerier, plan EpisodeVideoProductionPlan) (BatchShotProductionOutput, error) {
	return loadEpisodeVideoCheckpointOutputV2(ctx, query, plan)
}

func stringMetadataValue(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func loadEpisodeVideoProductionOutputTx(ctx context.Context, tx pgx.Tx, plan EpisodeVideoProductionPlan) (BatchShotProductionOutput, error) {
	var targetRaw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT metadata->'targetShotIds'
		FROM episode_video_production_checkpoints
		WHERE id = $1
	`, plan.CheckpointID).Scan(&targetRaw); err != nil {
		return BatchShotProductionOutput{}, err
	}
	var targetShotIDs []string
	if err := json.Unmarshal(targetRaw, &targetShotIDs); err != nil || len(targetShotIDs) == 0 {
		return BatchShotProductionOutput{}, fmt.Errorf("episode video checkpoint target list is invalid")
	}
	output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: plan.WorkflowRunID}, targetShotIDs)
	rows, err := tx.Query(ctx, `
		SELECT metadata->'batchOutput'
		FROM episode_video_production_batches
		WHERE checkpoint_id = $1 AND metadata ? 'batchOutput'
		ORDER BY ordinal, attempt
	`, plan.CheckpointID)
	if err != nil {
		return BatchShotProductionOutput{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return BatchShotProductionOutput{}, err
		}
		var batch BatchShotProductionOutput
		if err := json.Unmarshal(raw, &batch); err != nil {
			return BatchShotProductionOutput{}, err
		}
		mergeBatchShotVideoOutput(&output, batch)
	}
	if err := rows.Err(); err != nil {
		return BatchShotProductionOutput{}, err
	}
	output.Status = batchShotOutputStatus(output)
	return output, nil
}

func episodeVideoEventPayload(plan EpisodeVideoProductionPlan, batchID, itemID, shotID, status string) map[string]any {
	payload := map[string]any{
		"bindingId": plan.VideoProductionBindingID, "bindingRevision": plan.VideoProductionBindingRevision,
		"productionGenerationId": plan.ProductionGenerationID, "episodeId": plan.ScriptEpisodeID,
		"workflowRunId": plan.WorkflowRunID, "checkpointId": plan.CheckpointID, "status": status,
	}
	if batchID != "" {
		payload["batchId"] = batchID
	}
	if itemID != "" {
		payload["itemId"] = itemID
	}
	if shotID != "" {
		payload["storyboardShotId"] = shotID
	}
	return payload
}

func hashEpisodeVideoValue(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func shotExecutionIDs(shots []ShotVideoExecutionShot) []string {
	ids := make([]string, 0, len(shots))
	for _, shot := range shots {
		ids = append(ids, shot.ShotID)
	}
	return ids
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func isTerminalEpisodeVideoCheckpoint(status string) bool {
	switch status {
	case "succeeded", "partial_succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func isTerminalEpisodeParentWorkflowStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "succeeded", "partial_succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func sortedEpisodeVideoPlans(plans []EpisodeVideoProductionPlan) {
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].EpisodeIndex != plans[j].EpisodeIndex {
			return plans[i].EpisodeIndex < plans[j].EpisodeIndex
		}
		return plans[i].ScriptEpisodeID < plans[j].ScriptEpisodeID
	})
}
