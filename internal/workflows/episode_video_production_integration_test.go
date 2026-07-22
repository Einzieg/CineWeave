package workflows

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/testsuite"
)

func TestRestoreApprovedVideoPromptStateAfterFailedRegeneration(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	_, organizationID, userID, projectID, sourceWorkflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	failedWorkflowRunID := insertEpisodeVideoWorkflowRun(t, ctx, pool, organizationID, projectID, userID, sourceWorkflowRunID)
	shotID := shots[0].ID

	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt_status = 'failed',
		    video_prompt_error_code = 'MODEL_CAPABILITY_UNAVAILABLE',
		    video_prompt_error_message = 'regeneration failed',
		    video_prompt_workflow_run_id = $2
		WHERE id = $1
	`, shotID, failedWorkflowRunID); err != nil {
		t.Fatalf("seed failed regeneration state: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin restore transaction: %v", err)
	}
	restored, err := restoreApprovedVideoPromptStateTx(ctx, tx, shotID, failedWorkflowRunID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("restore approved prompt state: %v", err)
	}
	if !restored {
		_ = tx.Rollback(ctx)
		t.Fatal("approved prompt state was not restored")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit restored prompt state: %v", err)
	}

	var status, prompt, promptWorkflowRunID string
	var errorCode, errorMessage *string
	if err := pool.QueryRow(ctx, `
		SELECT video_prompt_status, COALESCE(video_prompt, ''),
		       COALESCE(video_prompt_workflow_run_id::text, ''),
		       video_prompt_error_code, video_prompt_error_message
		FROM storyboard_shots
		WHERE id = $1
	`, shotID).Scan(&status, &prompt, &promptWorkflowRunID, &errorCode, &errorMessage); err != nil {
		t.Fatalf("load restored prompt state: %v", err)
	}
	if status != "succeeded" || strings.TrimSpace(prompt) == "" || promptWorkflowRunID == failedWorkflowRunID || errorCode != nil || errorMessage != nil {
		t.Fatalf("restored prompt state = status %s workflow %s code %v message %v", status, promptWorkflowRunID, errorCode, errorMessage)
	}
}

func TestPrepareEpisodeVideoProductionBatchAcceptsExpiredPriorRenderPlan(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE video_render_plans plan
		SET expires_at = now() - interval '1 minute'
		FROM storyboard_shots shot
		WHERE shot.active_video_render_plan_id = plan.id
		  AND shot.id::text = ANY($1::text[])
	`, shotIDs); err != nil {
		t.Fatalf("expire prior render plans: %v", err)
	}

	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("prepare episode video production: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("episode plans = %+v", plans)
	}
	batch, err := activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
		Plan: plans[0], Options: BatchShotProductionOptions{MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("prepare batch with expired prior render plans: %v", err)
	}
	if len(batch.Shots) != len(shotIDs) {
		t.Fatalf("prepared shots = %d, want %d", len(batch.Shots), len(shotIDs))
	}
}

func TestEpisodeVideoProductionCheckpointLifecycleAndFailedItemRetry(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, episodeID, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}

	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("PrepareEpisodeVideoProductions: %v", err)
	}
	if len(plans) != 1 || plans[0].ScriptEpisodeID != episodeID || len(plans[0].TargetShotIDs) != len(shotIDs) {
		t.Fatalf("episode plans = %+v", plans)
	}
	plan := plans[0]
	firstBatch, err := activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("prepare first batch: %v", err)
	}
	replayedBatch, err := activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("replay first batch: %v", err)
	}
	if replayedBatch.BatchID != firstBatch.BatchID || replayedBatch.DependencyHash != firstBatch.DependencyHash {
		t.Fatalf("prepare activity was not idempotent: first=%+v replay=%+v", firstBatch, replayedBatch)
	}

	conflictingRunID := insertEpisodeVideoWorkflowRun(t, ctx, pool, organizationID, projectID, userID, workflowRunID)
	if _, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: conflictingRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs},
	}); err == nil {
		t.Fatal("conflicting active episode checkpoint was accepted")
	}

	failedShotID := firstBatch.Shots[0].ShotID
	batch := firstBatch
	batchNumber := 0
	var final CommitEpisodeVideoProductionBatchOutput
	for {
		output := newBatchShotVideoOutput(TextToStoryboardInput{WorkflowRunID: workflowRunID}, shotExecutionIDs(batch.Shots))
		for _, shot := range batch.Shots {
			if batchNumber == 0 && shot.ShotID == failedShotID {
				output.FailedShotIDs = append(output.FailedShotIDs, shot.ShotID)
				output.Errors[shot.ShotID] = "provider rejected test item"
				continue
			}
			output.SucceededShotIDs = append(output.SucceededShotIDs, shot.ShotID)
		}
		output.Status = batchShotOutputStatus(output)
		final, err = activities.CommitEpisodeVideoProductionBatch(ctx, CommitEpisodeVideoProductionBatchInput{
			Plan: plan, Batch: batch, Output: output,
		})
		if err != nil {
			t.Fatalf("commit batch %d: %v", batchNumber, err)
		}
		if !final.HasMore {
			break
		}
		batchNumber++
		batch, err = activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
			Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 5},
		})
		if err != nil {
			t.Fatalf("prepare batch %d: %v", batchNumber, err)
		}
	}
	if final.Status != "partial_succeeded" || final.FinalOutput.Status != "partial_succeeded" || len(final.FinalOutput.FailedShotIDs) != 1 {
		t.Fatalf("final checkpoint output = %+v", final)
	}

	var checkpointStatus string
	var succeededItems, failedItems, activeItems int
	if err := pool.QueryRow(ctx, `
		SELECT checkpoint.status,
		       count(*) FILTER (WHERE item.status = 'succeeded'),
		       count(*) FILTER (WHERE item.status = 'failed'),
		       count(*) FILTER (WHERE item.status IN ('queued', 'running', 'cancelling'))
		FROM episode_video_production_checkpoints checkpoint
		JOIN episode_video_production_batches batch ON batch.checkpoint_id = checkpoint.id
		JOIN episode_video_production_items item ON item.batch_id = batch.id
		WHERE checkpoint.id = $1
		GROUP BY checkpoint.id
	`, plan.CheckpointID).Scan(&checkpointStatus, &succeededItems, &failedItems, &activeItems); err != nil {
		t.Fatalf("load checkpoint aggregate: %v", err)
	}
	if checkpointStatus != "partial_succeeded" || succeededItems != len(shotIDs)-1 || failedItems != 1 || activeItems != 0 {
		t.Fatalf("checkpoint=%s succeeded=%d failed=%d active=%d", checkpointStatus, succeededItems, failedItems, activeItems)
	}

	retryPlans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: conflictingRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: []string{failedShotID}},
	})
	if err != nil {
		t.Fatalf("prepare failed-item retry: %v", err)
	}
	if len(retryPlans) != 1 || retryPlans[0].CheckpointID == plan.CheckpointID || len(retryPlans[0].TargetShotIDs) != 1 || retryPlans[0].TargetShotIDs[0] != failedShotID {
		t.Fatalf("retry plans = %+v", retryPlans)
	}

	var sourcePlanID, sourceSegmentID, sourceProviderModelID string
	if err := pool.QueryRow(ctx, `
		SELECT plan.id::text, segment.id::text, plan.provider_model_id::text
		FROM storyboard_shots shot
		JOIN video_render_plans plan ON plan.id = shot.active_video_render_plan_id
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		WHERE shot.id = $1
		ORDER BY segment.segment_index
		LIMIT 1
	`, failedShotID).Scan(&sourcePlanID, &sourceSegmentID, &sourceProviderModelID); err != nil {
		t.Fatalf("load consumed render plan: %v", err)
	}
	staleExecutionPrompt := "旧执行计划片段提示词，不得复制到新的供应商执行计划。"
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_segments
		SET prompt = $2, execution_prompt_hash = $3,
		    metadata = metadata || jsonb_build_object(
		      'videoPromptAgent', jsonb_build_object('status', 'approved')
		    )
		WHERE id = $1
	`, sourceSegmentID, staleExecutionPrompt, promptsvc.HashText(staleExecutionPrompt)); err != nil {
		t.Fatalf("prepare stale execution prompt: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE video_render_plans SET status = 'failed' WHERE id = $1`, sourcePlanID); err != nil {
		t.Fatalf("mark render plan consumed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_segments
		SET status = 'failed', error_code = 'UPSTREAM_TIMEOUT'
		WHERE id = $1
	`, sourceSegmentID); err != nil {
		t.Fatalf("mark render plan consumed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt_status = 'succeeded',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'videoPromptPlan', jsonb_build_object(
		        'status', 'ready', 'executionPlanId', $2::uuid::text
		      )
		    )
		WHERE id = $1
	`, failedShotID, sourcePlanID); err != nil {
		t.Fatalf("seed stale shot prompt readiness: %v", err)
	}
	replacementModelID := seedStoryboardEpisodeV2VideoModel(t, ctx, pool, sourceProviderModelID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		SELECT profile.id, $1, 1, 100, true
		FROM model_profiles profile
		WHERE profile.organization_id = $2 AND profile.profile_key = 'video_generation_default'
	`, replacementModelID, organizationID); err != nil {
		t.Fatalf("bind replacement video model: %v", err)
	}
	prepared, err := activities.EnsurePreparedShotVideoPlan(ctx, EnsurePreparedShotVideoPlanInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: conflictingRunID,
		CreatedBy: userID, ShotID: failedShotID, AspectRatio: "16:9", Resolution: "720p",
		AudioStrategy: "native_av", AudioRequirement: "required", Force: true,
	})
	if err != nil {
		t.Fatalf("clone consumed render plan: %v", err)
	}
	if prepared.Plan.ExecutionPlanID == sourcePlanID || len(prepared.Segments) == 0 {
		t.Fatalf("prepared execution plan = %+v", prepared)
	}
	if prepared.Plan.ProviderModelID != replacementModelID {
		t.Fatalf("execution provider model = %s, want current priority model %s", prepared.Plan.ProviderModelID, replacementModelID)
	}
	var failedShot StoryboardShotRecord
	for _, shot := range shots {
		if shot.ID == failedShotID {
			failedShot = shot
			break
		}
	}
	project, err := activities.projectProductionSettings(ctx, projectID, conflictingRunID)
	if err != nil {
		t.Fatalf("load project settings: %v", err)
	}
	contract, err := activities.loadApprovedShotVideoExecutionContract(ctx, organizationID, project, failedShot)
	if err != nil {
		t.Fatalf("load approved video prompt contract: %v", err)
	}
	for index, segment := range prepared.Segments {
		expectedPrompt := composeAuthoritativeVideoPrompt(
			stripAuthoritativeVideoPromptAudio(contract.Prompt),
			renderSegmentDialogueLines(prepared.Plan.Segments[index].DialogueSpans),
		)
		if segment.SegmentID == sourceSegmentID || segment.Prompt == staleExecutionPrompt || segment.Prompt != expectedPrompt || segment.PromptHash != promptsvc.HashText(expectedPrompt) {
			t.Fatalf("materialized segment %d = %+v", index, segment)
		}
	}
	var sourceActive, executionActive bool
	var executionStatus, executionWorkflowRunID, previousExecutionPlanID string
	var executionProviderTasks int
	if err := pool.QueryRow(ctx, `
		SELECT source.active, execution.active, execution.status, execution.workflow_run_id::text,
		       COALESCE(execution.metadata->>'previousExecutionPlanId', ''),
		       count(*) FILTER (WHERE segment.provider_async_task_id IS NOT NULL)
		FROM video_render_plans source
		JOIN video_render_plans execution ON execution.id = $2
		JOIN video_render_segments segment ON segment.video_render_plan_id = execution.id
		WHERE source.id = $1
		GROUP BY source.id, execution.id
	`, sourcePlanID, prepared.Plan.ExecutionPlanID).Scan(
		&sourceActive, &executionActive, &executionStatus, &executionWorkflowRunID,
		&previousExecutionPlanID, &executionProviderTasks,
	); err != nil {
		t.Fatalf("load recompiled execution state: %v", err)
	}
	if sourceActive || !executionActive || executionStatus != "planned" || executionWorkflowRunID != conflictingRunID || previousExecutionPlanID != sourcePlanID || executionProviderTasks != 0 {
		t.Fatalf("sourceActive=%t executionActive=%t status=%s workflow=%s previous=%s tasks=%d", sourceActive, executionActive, executionStatus, executionWorkflowRunID, previousExecutionPlanID, executionProviderTasks)
	}
	replayed, err := activities.EnsurePreparedShotVideoPlan(ctx, EnsurePreparedShotVideoPlanInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: conflictingRunID,
		CreatedBy: userID, ShotID: failedShotID, AspectRatio: "16:9", Resolution: "720p",
		AudioStrategy: "native_av", AudioRequirement: "required", Force: true,
	})
	if err != nil || replayed.Plan.ExecutionPlanID != prepared.Plan.ExecutionPlanID {
		t.Fatalf("replayed execution plan = %+v err=%v", replayed, err)
	}

	for _, eventName := range []string{
		"video.production.batch.started",
		"video.production.item.started",
		"video.production.item.completed",
		"video.production.item.failed",
		"video.production.batch.partial_succeeded",
		"video.production.checkpoint.committed",
	} {
		var count int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM event_outbox
			WHERE project_id = $1 AND event_type = $2 AND payload->>'workflowRunId' = $3
		`, projectID, eventName, workflowRunID).Scan(&count); err != nil {
			t.Fatalf("count event %s: %v", eventName, err)
		}
		if count == 0 {
			t.Fatalf("event %s was not persisted", eventName)
		}
	}
}

func TestFailEpisodeVideoProductionCheckpointReleasesEpisodeRetry(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	if _, err := activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 5},
	}); err != nil {
		t.Fatalf("prepare active batch: %v", err)
	}
	failure := FailEpisodeVideoProductionCheckpointInput{
		Plan: plan, FailureCode: provider.CodeRenderPlanReplanRequired, FailureMessage: "render plan identity changed",
	}
	if err := activities.FailEpisodeVideoProductionCheckpoint(ctx, failure); err != nil {
		t.Fatalf("fail checkpoint: %v", err)
	}
	if err := activities.FailEpisodeVideoProductionCheckpoint(ctx, failure); err != nil {
		t.Fatalf("replay fail checkpoint: %v", err)
	}
	var checkpointStatus string
	var activeItems, activeBatches, eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT checkpoint.status,
		       (SELECT count(*) FROM episode_video_production_items item
		        JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		        WHERE batch.checkpoint_id = checkpoint.id AND item.status IN ('queued', 'running', 'cancelling')),
		       (SELECT count(*) FROM episode_video_production_batches batch
		        WHERE batch.checkpoint_id = checkpoint.id AND batch.status IN ('queued', 'running', 'cancelling')),
		       (SELECT count(*) FROM event_outbox event
		        WHERE event.project_id = checkpoint.project_id
		          AND event.event_type = 'video.production.checkpoint.failed'
		          AND event.aggregate_id = checkpoint.id)
		FROM episode_video_production_checkpoints checkpoint
		WHERE checkpoint.id = $1
	`, plan.CheckpointID).Scan(&checkpointStatus, &activeItems, &activeBatches, &eventCount); err != nil {
		t.Fatalf("load failed checkpoint: %v", err)
	}
	if checkpointStatus != "failed" || activeItems != 0 || activeBatches != 0 || eventCount != 1 {
		t.Fatalf("checkpoint=%s activeItems=%d activeBatches=%d events=%d", checkpointStatus, activeItems, activeBatches, eventCount)
	}
	retryWorkflowRunID := insertEpisodeVideoWorkflowRun(t, ctx, pool, organizationID, projectID, userID, workflowRunID)
	retryPlans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: retryWorkflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("prepare retry after failed checkpoint: %v", err)
	}
	if len(retryPlans) != 1 || retryPlans[0].CheckpointID == plan.CheckpointID {
		t.Fatalf("retry plans = %+v", retryPlans)
	}
}

func TestEpisodeVideoCheckpointV2RecoversFailureBeforeFirstBatch(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := []string{shots[0].ID, shots[1].ID}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	if err := activities.FailEpisodeVideoProductionCheckpoint(ctx, FailEpisodeVideoProductionCheckpointInput{
		Plan: plan, FailureCode: "PREPARE_FAILED", FailureMessage: "prepare failed before first batch",
	}); err != nil {
		t.Fatalf("fail checkpoint: %v", err)
	}
	if err := activities.ReconcileEpisodeVideoProductionCheckpointV2(ctx, plan); err != nil {
		t.Fatalf("reconcile failed checkpoint: %v", err)
	}
	output, err := activities.LoadEpisodeVideoProductionOutputV2(ctx, plan)
	if err != nil {
		t.Fatalf("load failed checkpoint output: %v", err)
	}
	if output.Status != "failed" || len(output.SucceededShotIDs) != 0 || len(output.FailedShotIDs) != len(shotIDs) {
		t.Fatalf("failed checkpoint output = %+v", output)
	}
	for _, shotID := range shotIDs {
		if output.ErrorCodes[shotID] != episodeVideoMissingItemCode {
			t.Fatalf("shot %s error code = %s", shotID, output.ErrorCodes[shotID])
		}
	}
}

func TestEpisodeVideoCheckpointV2RecoversFailureAfterPrepareBeforeCommit(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := []string{shots[0].ID, shots[1].ID}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 2},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	batch, err := activities.PrepareEpisodeVideoProductionBatchV2(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 2},
	})
	if err != nil || len(batch.Shots) != len(shotIDs) {
		t.Fatalf("prepare v2 batch: batch=%+v err=%v", batch, err)
	}
	if err := activities.FailEpisodeVideoProductionCheckpoint(ctx, FailEpisodeVideoProductionCheckpointInput{
		Plan: plan, FailureCode: "WORKER_CRASHED", FailureMessage: "worker stopped before batch commit",
	}); err != nil {
		t.Fatalf("fail prepared checkpoint: %v", err)
	}
	if err := activities.ReconcileEpisodeVideoProductionCheckpointV2(ctx, plan); err != nil {
		t.Fatalf("reconcile prepared checkpoint failure: %v", err)
	}
	output, err := activities.LoadEpisodeVideoProductionOutputV2(ctx, plan)
	if err != nil {
		t.Fatalf("load prepared checkpoint failure: %v", err)
	}
	if output.Status != "failed" || len(output.SucceededShotIDs) != 0 || len(output.FailedShotIDs) != len(shotIDs) {
		t.Fatalf("prepared checkpoint failure output = %+v", output)
	}
	for _, shotID := range shotIDs {
		if output.ErrorCodes[shotID] != "WORKER_CRASHED" {
			t.Fatalf("shot %s error code = %s", shotID, output.ErrorCodes[shotID])
		}
	}
}

func TestEpisodeVideoCheckpointV2RecoversCancelledTargets(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := []string{shots[0].ID, shots[1].ID}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 2},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	if _, err := activities.PrepareEpisodeVideoProductionBatchV2(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 2},
	}); err != nil {
		t.Fatalf("prepare v2 batch: %v", err)
	}
	if err := activities.CancelEpisodeVideoProductionCheckpoint(ctx, plan); err != nil {
		t.Fatalf("cancel checkpoint: %v", err)
	}
	if err := activities.ReconcileEpisodeVideoProductionCheckpointV2(ctx, plan); err != nil {
		t.Fatalf("reconcile cancelled checkpoint: %v", err)
	}
	output, err := activities.LoadEpisodeVideoProductionOutputV2(ctx, plan)
	if err != nil {
		t.Fatalf("load cancelled checkpoint output: %v", err)
	}
	if output.Status != "cancelled" || len(output.CancelledShotIDs) != len(shotIDs) || len(output.SucceededShotIDs) != 0 {
		t.Fatalf("cancelled checkpoint output = %+v", output)
	}
}

func TestEpisodeVideoCheckpointV2RepairsDurableSuccessAndPartialFailure(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := []string{shots[0].ID, shots[1].ID}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 2},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	batch, err := activities.PrepareEpisodeVideoProductionBatchV2(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 2},
	})
	if err != nil || len(batch.Shots) != 2 {
		t.Fatalf("prepare v2 batch: batch=%+v err=%v", batch, err)
	}
	approved, err := activities.LoadApprovedShotVideoPromptPlanV2(ctx, LoadApprovedShotVideoPromptPlanV2Input{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		ShotID: batch.Shots[0].ShotID, ShotIndex: batch.Shots[0].ShotIndex,
	})
	if err != nil || approved.VideoPromptPlanID == "" || approved.Prompt == "" || approved.ReferencePackID == "" {
		t.Fatalf("load approved prompt-only plan: output=%+v err=%v", approved, err)
	}
	materializeInput := EnsurePreparedShotVideoPlanV2Input{
		EnsurePreparedShotVideoPlanInput: EnsurePreparedShotVideoPlanInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ShotID: batch.Shots[0].ShotID, ShotIndex: batch.Shots[0].ShotIndex,
			AspectRatio: "16:9", Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "required", Force: true,
		},
		OperationID: plan.CheckpointID, OperationItemID: batch.Shots[0].OperationItemID,
		OperationItemAttempt: batch.Shots[0].OperationItemAttempt,
	}
	prepared, err := activities.MaterializeAndBindExecutableShotVideoPlanV2(ctx, materializeInput)
	if err != nil {
		t.Fatalf("materialize v2 render plan: %v", err)
	}
	if len(prepared.Segments) == 0 {
		t.Fatal("materialized render plan has no segments")
	}
	replayed, err := activities.MaterializeAndBindExecutableShotVideoPlanV2(ctx, materializeInput)
	if err != nil || replayed.Plan.ExecutionPlanID != prepared.Plan.ExecutionPlanID || len(replayed.Segments) != len(prepared.Segments) {
		t.Fatalf("replayed materialization = %+v err=%v", replayed, err)
	}
	loaded, err := activities.LoadExecutableShotVideoPlanV2(ctx, LoadExecutableShotVideoPlanV2Input{
		LoadPreparedShotVideoPlanInput: LoadPreparedShotVideoPlanInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			ShotID: batch.Shots[0].ShotID, ShotIndex: batch.Shots[0].ShotIndex,
			AspectRatio: "16:9", Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "required",
		},
		OperationID: plan.CheckpointID, OperationItemID: batch.Shots[0].OperationItemID,
		OperationItemAttempt: batch.Shots[0].OperationItemAttempt, ExecutionPlanID: prepared.Plan.ExecutionPlanID,
	})
	if err != nil || loaded.Plan.ExecutionPlanID != prepared.Plan.ExecutionPlanID {
		t.Fatalf("load exact executable plan: output=%+v err=%v", loaded, err)
	}
	seedSucceededEpisodeVideoExecution(t, ctx, pool, organizationID, projectID, workflowRunID,
		batch.Shots[0].OperationItemID, batch.Shots[0].OperationItemAttempt, prepared.Plan.ExecutionPlanID)
	var succeededTaskID string
	if err := pool.QueryRow(ctx, `
		SELECT task.id::text
		FROM provider_async_tasks task
		WHERE task.video_render_plan_id = $1
		ORDER BY task.created_at, task.id
		LIMIT 1
	`, prepared.Plan.ExecutionPlanID).Scan(&succeededTaskID); err != nil {
		t.Fatalf("load succeeded provider task: %v", err)
	}
	requestHash := "sha256:" + strings.Repeat("e", 64)
	if _, err := pool.Exec(ctx, `UPDATE provider_async_tasks SET request_hash = $2 WHERE id = $1`, succeededTaskID, requestHash); err != nil {
		t.Fatalf("seed provider task request hash: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_async_tasks(
		  provider_call_id, organization_id, project_id, workflow_run_id,
		  provider_account_id, provider_model_id, status,
		  production_generation_id, video_production_binding_id, video_production_binding_revision,
		  video_render_plan_id, video_render_segment_id,
		  operation_item_id, operation_item_attempt, request_hash,
		  finalized_at, completed_at
		)
		SELECT provider_call_id, organization_id, project_id, workflow_run_id,
		       provider_account_id, provider_model_id, 'succeeded',
		       production_generation_id, video_production_binding_id, video_production_binding_revision,
		       video_render_plan_id, video_render_segment_id,
		       operation_item_id, operation_item_attempt, request_hash,
		       now(), now()
		FROM provider_async_tasks
		WHERE id = $1
	`, succeededTaskID); err == nil {
		t.Fatal("database allowed duplicate request hash for the same render segment attempt")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE episode_video_production_items
		SET status = 'failed', error_code = 'UPSTREAM_REJECTED',
		    error_detail = '{"message":"provider rejected test item"}'::jsonb,
		    completed_at = now(), updated_at = now()
		WHERE id = $1
	`, batch.Shots[1].OperationItemID); err != nil {
		t.Fatalf("fail second item: %v", err)
	}
	if err := activities.ReconcileEpisodeVideoProductionCheckpointV2(ctx, plan); err != nil {
		t.Fatalf("reconcile partial checkpoint: %v", err)
	}
	if err := activities.ReconcileEpisodeVideoProductionCheckpointV2(ctx, plan); err != nil {
		t.Fatalf("replay partial checkpoint reconcile: %v", err)
	}
	output, err := activities.LoadEpisodeVideoProductionOutputV2(ctx, plan)
	if err != nil {
		t.Fatalf("load partial checkpoint output: %v", err)
	}
	if output.Status != "partial_succeeded" || len(output.SucceededShotIDs) != 1 || len(output.FailedShotIDs) != 1 ||
		output.SucceededShotIDs[0] != batch.Shots[0].ShotID || output.FailedShotIDs[0] != batch.Shots[1].ShotID {
		t.Fatalf("partial checkpoint output = %+v", output)
	}
	var itemStatus, checkpointStatus string
	var reconcileEvents int
	if err := pool.QueryRow(ctx, `
		SELECT item.status, checkpoint.status,
		       (SELECT count(*) FROM event_outbox event
		        WHERE event.aggregate_id = checkpoint.id
		          AND event.event_type = 'video.production.checkpoint.reconciled')
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		WHERE item.id = $1
	`, batch.Shots[0].OperationItemID).Scan(&itemStatus, &checkpointStatus, &reconcileEvents); err != nil {
		t.Fatalf("load reconciled projection: %v", err)
	}
	if itemStatus != "succeeded" || checkpointStatus != "partial_succeeded" || reconcileEvents != 1 {
		t.Fatalf("item=%s checkpoint=%s reconcileEvents=%d", itemStatus, checkpointStatus, reconcileEvents)
	}
	var operationPlanCount, operationSegmentCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT plan.id), count(segment.id)
		FROM video_render_plans plan
		LEFT JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		WHERE plan.operation_item_id = $1 AND plan.operation_item_attempt = $2
	`, batch.Shots[0].OperationItemID, batch.Shots[0].OperationItemAttempt).Scan(&operationPlanCount, &operationSegmentCount); err != nil {
		t.Fatalf("count exact operation plans: %v", err)
	}
	if operationPlanCount != 1 || operationSegmentCount != len(prepared.Segments) {
		t.Fatalf("operation plans=%d segments=%d", operationPlanCount, operationSegmentCount)
	}

	competingWorkflowRunID := insertEpisodeVideoWorkflowRun(t, ctx, pool, organizationID, projectID, userID, workflowRunID)
	competing, err := activities.EnsurePreparedShotVideoPlan(ctx, EnsurePreparedShotVideoPlanInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: competingWorkflowRunID,
		CreatedBy: userID, ShotID: batch.Shots[0].ShotID, ShotIndex: batch.Shots[0].ShotIndex,
		AspectRatio: "16:9", Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "required", Force: true,
	})
	if err != nil {
		t.Fatalf("materialize competing workflow plan: %v", err)
	}
	if competing.Plan.ExecutionPlanID == prepared.Plan.ExecutionPlanID {
		t.Fatal("competing workflow reused the operation-bound render plan")
	}
	if _, err := activities.LoadExecutableShotVideoPlanV2(ctx, LoadExecutableShotVideoPlanV2Input{
		LoadPreparedShotVideoPlanInput: LoadPreparedShotVideoPlanInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			ShotID: batch.Shots[0].ShotID, ShotIndex: batch.Shots[0].ShotIndex,
		},
		OperationID: plan.CheckpointID, OperationItemID: batch.Shots[0].OperationItemID,
		OperationItemAttempt: batch.Shots[0].OperationItemAttempt, ExecutionPlanID: competing.Plan.ExecutionPlanID,
	}); err == nil {
		t.Fatal("workflow A loaded workflow B's render plan")
	}

	retryPlans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: competingWorkflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: []string{batch.Shots[1].ShotID}, MaxConcurrency: 1},
	})
	if err != nil || len(retryPlans) != 1 {
		t.Fatalf("prepare v2 retry: plans=%+v err=%v", retryPlans, err)
	}
	retryBatch, err := activities.PrepareEpisodeVideoProductionBatchV2(ctx, EpisodeVideoProductionInput{
		Plan: retryPlans[0], Options: BatchShotProductionOptions{MaxConcurrency: 1},
	})
	if err != nil || len(retryBatch.Shots) != 1 {
		t.Fatalf("prepare v2 retry batch: batch=%+v err=%v", retryBatch, err)
	}
	if retryBatch.Shots[0].OperationItemAttempt <= batch.Shots[1].OperationItemAttempt ||
		retryBatch.Shots[0].OperationItemID == batch.Shots[1].OperationItemID {
		t.Fatalf("retry operation identity = %+v, previous=%+v", retryBatch.Shots[0], batch.Shots[1])
	}
	retryPrepared, err := activities.MaterializeAndBindExecutableShotVideoPlanV2(ctx, EnsurePreparedShotVideoPlanV2Input{
		EnsurePreparedShotVideoPlanInput: EnsurePreparedShotVideoPlanInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: competingWorkflowRunID,
			CreatedBy: userID, ShotID: retryBatch.Shots[0].ShotID, ShotIndex: retryBatch.Shots[0].ShotIndex,
			AspectRatio: "16:9", Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "required", Force: true,
		},
		OperationID: retryPlans[0].CheckpointID, OperationItemID: retryBatch.Shots[0].OperationItemID,
		OperationItemAttempt: retryBatch.Shots[0].OperationItemAttempt,
	})
	if err != nil {
		t.Fatalf("materialize retry render plan: %v", err)
	}
	var oldPlanID, retryPlanID string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(old_item.video_render_plan_id::text, ''), retry_item.video_render_plan_id::text
		FROM episode_video_production_items old_item
		JOIN episode_video_production_items retry_item ON retry_item.id = $2
		WHERE old_item.id = $1
	`, batch.Shots[1].OperationItemID, retryBatch.Shots[0].OperationItemID).Scan(&oldPlanID, &retryPlanID); err != nil {
		t.Fatalf("load retry plan provenance: %v", err)
	}
	if oldPlanID != "" || retryPlanID != retryPrepared.Plan.ExecutionPlanID {
		t.Fatalf("oldPlan=%s retryPlan=%s prepared=%s", oldPlanID, retryPlanID, retryPrepared.Plan.ExecutionPlanID)
	}
}

func TestMaterializeAndBindExecutableShotVideoPlanV2RollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: []string{shots[0].ID, shots[1].ID}, MaxConcurrency: 2},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	batch, err := activities.PrepareEpisodeVideoProductionBatchV2(ctx, EpisodeVideoProductionInput{
		Plan: plans[0], Options: BatchShotProductionOptions{MaxConcurrency: 2},
	})
	if err != nil || len(batch.Shots) != 2 {
		t.Fatalf("prepare v2 batch: batch=%+v err=%v", batch, err)
	}
	materializeInput := EnsurePreparedShotVideoPlanV2Input{
		EnsurePreparedShotVideoPlanInput: EnsurePreparedShotVideoPlanInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ShotID: batch.Shots[0].ShotID, ShotIndex: batch.Shots[0].ShotIndex,
			AspectRatio: "16:9", Resolution: "720p", AudioStrategy: "native_av", AudioRequirement: "required", Force: true,
		},
		OperationID: plans[0].CheckpointID, OperationItemID: batch.Shots[0].OperationItemID,
		OperationItemAttempt: batch.Shots[0].OperationItemAttempt,
	}
	successfulInput := materializeInput
	successfulInput.ShotID = batch.Shots[1].ShotID
	successfulInput.ShotIndex = batch.Shots[1].ShotIndex
	successfulInput.OperationItemID = batch.Shots[1].OperationItemID
	successfulInput.OperationItemAttempt = batch.Shots[1].OperationItemAttempt
	prepared, err := activities.MaterializeAndBindExecutableShotVideoPlanV2(ctx, successfulInput)
	if err != nil {
		t.Fatalf("materialize committed control plan: %v", err)
	}
	replayed, err := activities.MaterializeAndBindExecutableShotVideoPlanV2(ctx, successfulInput)
	if err != nil || replayed.Plan.ExecutionPlanID != prepared.Plan.ExecutionPlanID || len(replayed.Segments) != len(prepared.Segments) {
		t.Fatalf("replayed materialization = %+v err=%v", replayed, err)
	}
	var operationPlans, operationSegments int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT plan.id), count(segment.id)
		FROM video_render_plans plan
		LEFT JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		WHERE plan.operation_item_id = $1
	`, batch.Shots[1].OperationItemID).Scan(&operationPlans, &operationSegments); err != nil {
		t.Fatalf("count committed operation plan: %v", err)
	}
	if operationPlans != 1 || operationSegments != len(prepared.Segments) {
		t.Fatalf("committed state = plans:%d segments:%d", operationPlans, operationSegments)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE episode_video_production_items
		SET video_render_plan_id = $2, execution_plan_bound_at = now()
		WHERE id = $1
	`, batch.Shots[0].OperationItemID, prepared.Plan.ExecutionPlanID); err == nil {
		t.Fatal("database allowed two v2 items to bind the same render plan")
	}
	var boundPlanID string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(video_render_plan_id::text, '')
		FROM episode_video_production_items WHERE id = $1
	`, batch.Shots[0].OperationItemID).Scan(&boundPlanID); err != nil {
		t.Fatalf("load rejected second item binding: %v", err)
	}
	if boundPlanID != "" {
		t.Fatalf("second item retained rejected render plan binding %s", boundPlanID)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "test_fail_render_plan_commit_" + suffix
	triggerName := "test_fail_render_plan_commit_" + suffix
	createFunctionSQL := fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger AS $body$
		BEGIN
		  IF NEW.event_type = 'storyboard.shot.render_plan.created'
		     AND NEW.project_id = '%s'::uuid
		     AND NEW.aggregate_id = '%s'::uuid THEN
		    RAISE EXCEPTION 'injected render plan transaction failure';
		  END IF;
		  RETURN NEW;
		END;
		$body$ LANGUAGE plpgsql
	`, functionName, projectID, batch.Shots[0].ShotID)
	if _, err := pool.Exec(ctx, createFunctionSQL); err != nil {
		t.Fatalf("create render plan failure function: %v", err)
	}
	createTriggerSQL := fmt.Sprintf(`
		CREATE TRIGGER %s
		BEFORE INSERT ON event_outbox
		FOR EACH ROW EXECUTE FUNCTION %s()
	`, triggerName, functionName)
	if _, err := pool.Exec(ctx, createTriggerSQL); err != nil {
		t.Fatalf("create render plan failure trigger: %v", err)
	}
	dropFailureInjection := func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON event_outbox", triggerName))
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	}
	t.Cleanup(dropFailureInjection)

	if _, err := activities.MaterializeAndBindExecutableShotVideoPlanV2(ctx, materializeInput); err == nil {
		t.Fatal("materialization succeeded despite injected pre-commit failure")
	}
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(item.video_render_plan_id::text, ''),
		       (SELECT count(*) FROM video_render_plans plan WHERE plan.operation_item_id = item.id),
		       (SELECT count(*) FROM video_render_segments segment
		        JOIN video_render_plans plan ON plan.id = segment.video_render_plan_id
		        WHERE plan.operation_item_id = item.id)
		FROM episode_video_production_items item
		WHERE item.id = $1
	`, batch.Shots[0].OperationItemID).Scan(&boundPlanID, &operationPlans, &operationSegments); err != nil {
		t.Fatalf("load rolled back materialization state: %v", err)
	}
	if boundPlanID != "" || operationPlans != 0 || operationSegments != 0 {
		t.Fatalf("rolled back state = plan:%s plans:%d segments:%d", boundPlanID, operationPlans, operationSegments)
	}
}

func TestStuckEpisodeVideoReconcilerDoesNotStealLiveUnmaterializedItem(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: []string{shots[0].ID}},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	if _, err := activities.PrepareEpisodeVideoProductionBatchV2(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 1},
	}); err != nil {
		t.Fatalf("prepare live batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE episode_video_production_checkpoints SET updated_at = now() - interval '1 hour' WHERE id = $1
	`, plan.CheckpointID); err != nil {
		t.Fatalf("age checkpoint: %v", err)
	}
	count, err := ReconcileStuckEpisodeVideoProductionCheckpoints(ctx, pool, time.Minute, 10)
	if err != nil {
		t.Fatalf("run stuck reconciler: %v", err)
	}
	if count != 0 {
		t.Fatalf("reconciled count = %d, want 0", count)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM episode_video_production_checkpoints WHERE id = $1`, plan.CheckpointID).Scan(&status); err != nil {
		t.Fatalf("load live checkpoint: %v", err)
	}
	if status != "running" {
		t.Fatalf("live checkpoint status = %s", status)
	}
}

func seedSucceededEpisodeVideoExecution(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	organizationID, projectID, workflowRunID, itemID string,
	itemAttempt int,
	planID string,
) {
	t.Helper()
	var operationID, productionGenerationID string
	if err := pool.QueryRow(ctx, `
		SELECT checkpoint.id::text, plan.production_generation_id::text
		FROM episode_video_production_items item
		JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
		JOIN video_render_plans plan ON plan.id = $2
		WHERE item.id = $1 AND item.attempt = $3
	`, itemID, planID, itemAttempt).Scan(&operationID, &productionGenerationID); err != nil {
		t.Fatalf("load execution provenance fixture: %v", err)
	}
	var artifactID, mediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
		  organization_id, project_id, production_generation_id, workflow_run_id, type, storage_key,
		  mime_type, content_hash, metadata
		)
		VALUES ($1, $2, $3, $4, 'shot_video', $5, 'video/mp4', $6, '{"fixture":"b09_reconciliation"}'::jsonb)
		RETURNING id::text
	`, organizationID, projectID, productionGenerationID, workflowRunID,
		"tests/b09/"+uuid.NewString()+".mp4", strings.Repeat("9", 64)).Scan(&artifactID); err != nil {
		t.Fatalf("insert durable artifact fixture: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(
		  organization_id, project_id, production_generation_id, artifact_id, storage_key, mime_type,
		  duration_seconds, video_stream_count, audio_stream_count, metadata
		)
		SELECT $1, $2, $3, $4, storage_key, 'video/mp4', 5, 1, 1, '{"fixture":"b09_reconciliation"}'::jsonb
		FROM artifacts WHERE id = $4
		RETURNING id::text
	`, organizationID, projectID, productionGenerationID, artifactID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert durable media fixture: %v", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT segment.id::text, plan.provider_account_id::text, plan.provider_model_id::text,
		       plan.production_generation_id::text, plan.video_production_binding_id::text,
		       plan.video_production_binding_revision
		FROM video_render_plans plan
		JOIN video_render_segments segment ON segment.video_render_plan_id = plan.id
		WHERE plan.id = $1
		ORDER BY segment.segment_index
	`, planID)
	if err != nil {
		t.Fatalf("load render segments: %v", err)
	}
	type segmentRef struct {
		ID, AccountID, ModelID, GenerationID, BindingID string
		BindingRevision                                 int64
	}
	segments := make([]segmentRef, 0)
	for rows.Next() {
		var segment segmentRef
		if err := rows.Scan(
			&segment.ID, &segment.AccountID, &segment.ModelID,
			&segment.GenerationID, &segment.BindingID, &segment.BindingRevision,
		); err != nil {
			rows.Close()
			t.Fatalf("scan render segment: %v", err)
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("read render segments: %v", err)
	}
	rows.Close()
	for _, segment := range segments {
		requestHash := "sha256:" + strings.Repeat(strings.TrimPrefix(segment.ID, "-"), 8)
		if len(requestHash) > 71 {
			requestHash = requestHash[:71]
		}
		var providerRequestID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO provider_requests(
			  organization_id, project_id, workflow_run_id,
			  production_generation_id, video_production_binding_id, video_production_binding_revision,
			  operation_id, operation_item_id, operation_item_attempt,
			  video_render_plan_id, video_render_segment_id,
			  task_type, idempotency_key, request_hash, status,
			  result_snapshot, started_at, completed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        'video.create_task', $12, $13, 'succeeded', '{}', now(), now())
			RETURNING id::text
		`, organizationID, projectID, workflowRunID, segment.GenerationID, segment.BindingID,
			segment.BindingRevision, operationID, itemID, itemAttempt, planID, segment.ID,
			"b03:"+segment.ID, requestHash).Scan(&providerRequestID); err != nil {
			t.Fatalf("insert provider request provenance: %v", err)
		}
		var providerCallID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO provider_call_logs(
			  provider_request_id, organization_id, project_id, production_generation_id, workflow_run_id,
			  operation_id, operation_item_id, operation_item_attempt, video_render_plan_id, video_render_segment_id,
			  provider_account_id, provider_model_id, task_type, execution_mode, status,
			  request_snapshot, response_snapshot, normalized_output, artifact_ids, media_file_ids,
			  started_at, completed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			        $11, $12, 'video.create_task', 'async_create', 'succeeded',
			        '{}', '{}', '{}', jsonb_build_array($13::text), jsonb_build_array($14::text), now(), now())
			RETURNING id::text
		`, providerRequestID, organizationID, projectID, segment.GenerationID, workflowRunID,
			operationID, itemID, itemAttempt, planID, segment.ID, segment.AccountID, segment.ModelID,
			artifactID, mediaFileID).Scan(&providerCallID); err != nil {
			t.Fatalf("insert provider call provenance: %v", err)
		}
		var taskID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO provider_async_tasks(
			  provider_call_id, provider_request_id, organization_id, project_id, workflow_run_id,
			  provider_account_id, provider_model_id, status,
			  production_generation_id, video_production_binding_id, video_production_binding_revision,
			  video_render_plan_id, video_render_segment_id,
			  operation_id, operation_item_id, operation_item_attempt, request_hash,
			  finalized_at, completed_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'succeeded', $8, $9, $10, $11, $12, $13, $14, $15, $16, now(), now())
			RETURNING id::text
		`, providerCallID, providerRequestID, organizationID, projectID, workflowRunID, segment.AccountID, segment.ModelID,
			segment.GenerationID, segment.BindingID, segment.BindingRevision,
			planID, segment.ID, operationID, itemID, itemAttempt, requestHash).Scan(&taskID); err != nil {
			t.Fatalf("insert succeeded provider task: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO cost_records(
			  organization_id, project_id, production_generation_id, workflow_run_id,
			  operation_id, operation_item_id, operation_item_attempt, video_render_plan_id, video_render_segment_id,
			  provider_call_id, provider_model_id, cost_type, amount, currency, unit, quantity, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        'video.generate', 0.1, 'USD', 'second', 5, jsonb_build_object('providerAsyncTaskId', $12::text))
		`, organizationID, projectID, segment.GenerationID, workflowRunID, operationID, itemID, itemAttempt,
			planID, segment.ID, providerCallID, segment.ModelID, taskID); err != nil {
			t.Fatalf("insert provider cost provenance: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE video_render_segments
			SET status = 'succeeded', provider_async_task_id = $2,
			    artifact_id = $3, media_file_id = $4, completed_at = now(), updated_at = now()
			WHERE id = $1
		`, segment.ID, taskID, artifactID, mediaFileID); err != nil {
			t.Fatalf("complete render segment: %v", err)
		}
		var identityRows int
		if err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM provider_requests request
			JOIN provider_call_logs call ON call.provider_request_id = request.id
			JOIN provider_async_tasks task ON task.provider_request_id = request.id AND task.provider_call_id = call.id
			JOIN cost_records cost ON cost.provider_call_id = call.id
			WHERE request.id = $1
			  AND request.operation_id = $2 AND request.operation_item_id = $3 AND request.operation_item_attempt = $4
			  AND request.video_render_plan_id = $5 AND request.video_render_segment_id = $6
			  AND call.operation_id = request.operation_id AND call.operation_item_id = request.operation_item_id
			  AND call.operation_item_attempt = request.operation_item_attempt
			  AND call.video_render_plan_id = request.video_render_plan_id AND call.video_render_segment_id = request.video_render_segment_id
			  AND task.operation_id = request.operation_id AND task.operation_item_id = request.operation_item_id
			  AND task.operation_item_attempt = request.operation_item_attempt
			  AND task.video_render_plan_id = request.video_render_plan_id AND task.video_render_segment_id = request.video_render_segment_id
			  AND cost.operation_id = request.operation_id AND cost.operation_item_id = request.operation_item_id
			  AND cost.operation_item_attempt = request.operation_item_attempt
			  AND cost.video_render_plan_id = request.video_render_plan_id AND cost.video_render_segment_id = request.video_render_segment_id
		`, providerRequestID, operationID, itemID, itemAttempt, planID, segment.ID).Scan(&identityRows); err != nil {
			t.Fatalf("verify provider execution provenance: %v", err)
		}
		if identityRows != 1 {
			t.Fatalf("provider execution provenance rows = %d, want 1", identityRows)
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_plans
		SET status = 'succeeded', output_artifact_id = $2, output_media_file_id = $3,
		    output_storage_key = 'tests/reconciled-video.mp4', completed_at = now(), updated_at = now()
		WHERE id = $1
	`, planID, artifactID, mediaFileID); err != nil {
		t.Fatalf("complete render plan: %v", err)
	}
}

func TestPrepareEpisodeVideoProductionsReclaimsTerminalWorkflowCheckpoint(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	stalePlan := plans[0]
	if _, err := activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
		Plan: stalePlan, Options: BatchShotProductionOptions{MaxConcurrency: 5},
	}); err != nil {
		t.Fatalf("prepare active batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelled', completed_at = now(), cancelled_at = now(), terminalized_at = now(), settled_at = now()
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("terminalize source workflow: %v", err)
	}

	retryWorkflowRunID := insertEpisodeVideoWorkflowRun(t, ctx, pool, organizationID, projectID, userID, workflowRunID)
	retryPlans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: retryWorkflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil {
		t.Fatalf("prepare after terminal workflow: %v", err)
	}
	if len(retryPlans) != 1 || retryPlans[0].CheckpointID == stalePlan.CheckpointID {
		t.Fatalf("retry plans = %+v", retryPlans)
	}
	var checkpointStatus string
	var activeBatches, activeItems, eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT checkpoint.status,
		       (SELECT count(*) FROM episode_video_production_batches batch
		        WHERE batch.checkpoint_id = checkpoint.id AND batch.status IN ('queued', 'running', 'cancelling')),
		       (SELECT count(*) FROM episode_video_production_items item
		        JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		        WHERE batch.checkpoint_id = checkpoint.id AND item.status IN ('queued', 'running', 'cancelling')),
		       (SELECT count(*) FROM event_outbox event
		        WHERE event.project_id = checkpoint.project_id
		          AND event.event_type = 'video.production.checkpoint.committed'
		          AND event.aggregate_id = checkpoint.id
		          AND event.payload->>'status' = 'cancelled')
		FROM episode_video_production_checkpoints checkpoint
		WHERE checkpoint.id = $1
	`, stalePlan.CheckpointID).Scan(&checkpointStatus, &activeBatches, &activeItems, &eventCount); err != nil {
		t.Fatalf("load reclaimed checkpoint: %v", err)
	}
	if checkpointStatus != "cancelled" || activeBatches != 0 || activeItems != 0 || eventCount != 1 {
		t.Fatalf("checkpoint=%s activeBatches=%d activeItems=%d events=%d", checkpointStatus, activeBatches, activeItems, eventCount)
	}
}

func TestCancellationReconcilerReleasesEpisodeVideoCheckpoint(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shotIDs := make([]string, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	plans, err := activities.PrepareEpisodeVideoProductions(ctx, PrepareEpisodeVideoProductionsInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, Options: BatchShotProductionOptions{ShotIDs: shotIDs, MaxConcurrency: 5},
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare checkpoint: plans=%+v err=%v", plans, err)
	}
	plan := plans[0]
	if _, err := activities.PrepareEpisodeVideoProductionBatch(ctx, EpisodeVideoProductionInput{
		Plan: plan, Options: BatchShotProductionOptions{MaxConcurrency: 5},
	}); err != nil {
		t.Fatalf("prepare active batch: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelling', cancellation_requested_at = now() - interval '3 minutes',
		    cancellation_deadline_at = now() - interval '1 minute', error_message = '用户取消视频生产'
		WHERE id = $1
	`, workflowRunID); err != nil {
		t.Fatalf("mark workflow cancelling: %v", err)
	}

	count, err := ReconcileExpiredWorkflowCancellations(ctx, pool, 10)
	if err != nil {
		t.Fatalf("reconcile expired cancellation: %v", err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d, want 1", count)
	}
	var workflowStatus, checkpointStatus string
	var activeBatches, activeItems int
	if err := pool.QueryRow(ctx, `
		SELECT run.status, checkpoint.status,
		       (SELECT count(*) FROM episode_video_production_batches batch
		        WHERE batch.checkpoint_id = checkpoint.id AND batch.status IN ('queued', 'running', 'cancelling')),
		       (SELECT count(*) FROM episode_video_production_items item
		        JOIN episode_video_production_batches batch ON batch.id = item.batch_id
		        WHERE batch.checkpoint_id = checkpoint.id AND item.status IN ('queued', 'running', 'cancelling'))
		FROM workflow_runs run
		JOIN episode_video_production_checkpoints checkpoint ON checkpoint.workflow_run_id = run.id
		WHERE run.id = $1 AND checkpoint.id = $2
	`, workflowRunID, plan.CheckpointID).Scan(&workflowStatus, &checkpointStatus, &activeBatches, &activeItems); err != nil {
		t.Fatalf("load reconciled checkpoint: %v", err)
	}
	if workflowStatus != "cancelled" || checkpointStatus != "cancelled" || activeBatches != 0 || activeItems != 0 {
		t.Fatalf("workflow=%s checkpoint=%s activeBatches=%d activeItems=%d", workflowStatus, checkpointStatus, activeBatches, activeItems)
	}
}

func TestCompletePlannedFirstFrameAnchorCreatesVersionedApprovedAnchor(t *testing.T) {
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	shot := mustLoadStoryboardShot(t, ctx, activities, projectID, shots[0].ID)
	storageKey := "tests/storyboard-v2/replacement-" + uuid.NewString() + ".png"
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key,
			mime_type, content_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, 'storyboard_image', $4, 'image/png', $5,
		        '{"fixture":"replacement_planned_first_frame"}', $6)
		RETURNING id::text
	`, organizationID, projectID, workflowRunID, storageKey, strings.Repeat("8", 64), userID).Scan(&artifactID); err != nil {
		t.Fatalf("insert replacement first-frame artifact: %v", err)
	}
	input := GenerateShotImageInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		CreatedBy:      userID,
		ShotID:         shot.ID,
		ShotIndex:      shot.ShotIndex,
		ShotNo:         shot.ShotNo,
	}
	output := GenerateShotImageOutput{
		ShotID:          shot.ID,
		ImageArtifactID: artifactID,
		ImageStorageKey: storageKey,
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin anchor completion: %v", err)
	}
	anchorID, err := completePlannedFirstFrameAnchorTx(ctx, tx, input, shot, output)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("complete planned first-frame anchor: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit anchor completion: %v", err)
	}
	var revision int
	var status, reviewStatus, persistedArtifactID string
	if err := pool.QueryRow(ctx, `
		SELECT revision, status, review_status, artifact_id::text
		FROM shot_visual_anchors WHERE id = $1
	`, anchorID).Scan(&revision, &status, &reviewStatus, &persistedArtifactID); err != nil {
		t.Fatalf("load completed anchor: %v", err)
	}
	if revision != 2 || status != "ready" || reviewStatus != "approved" || persistedArtifactID != artifactID {
		t.Fatalf("completed anchor = revision %d status %s review %s artifact %s", revision, status, reviewStatus, persistedArtifactID)
	}
	var staleAnchors int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1 AND anchor_role = 'planned_first_frame'
		  AND id <> $2 AND status = 'stale' AND review_status = 'needs_edit'
	`, shot.ID, anchorID).Scan(&staleAnchors); err != nil {
		t.Fatalf("count stale anchors: %v", err)
	}
	if staleAnchors == 0 {
		t.Fatal("previous planned first-frame anchor was not superseded")
	}
	for tableName := range map[string]struct{}{
		"shot_reference_packs": {},
		"prompt_context_plans": {},
		"video_prompt_plans":   {},
	} {
		var activeCount int
		query := "SELECT count(*) FROM " + tableName + " WHERE project_id = $1 AND storyboard_shot_id = $2 AND status = 'active'"
		if tableName == "video_prompt_plans" {
			query = "SELECT count(*) FROM video_prompt_plans WHERE project_id = $1 AND storyboard_shot_id = $2 AND status NOT IN ('stale', 'archived')"
		}
		if err := pool.QueryRow(ctx, query, projectID, shot.ID).Scan(&activeCount); err != nil {
			t.Fatalf("count active %s: %v", tableName, err)
		}
		if activeCount != 0 {
			t.Fatalf("%s retained %d active records after anchor replacement", tableName, activeCount)
		}
	}
	var activeRenderPlans int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM video_render_plans
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND active = true
	`, projectID, shot.ID).Scan(&activeRenderPlans); err != nil {
		t.Fatalf("count active render plans: %v", err)
	}
	if activeRenderPlans != 0 {
		t.Fatalf("active render plans = %d, want 0", activeRenderPlans)
	}
	for _, eventType := range []string{"storyboard.shot.anchor.completed", "storyboard.shot.anchor.reviewed"} {
		var count int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM event_outbox
			WHERE project_id = $1 AND event_type = $2 AND aggregate_id = $3
		`, projectID, eventType, anchorID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", eventType, err)
		}
		if count != 1 {
			t.Fatalf("%s count = %d, want 1", eventType, count)
		}
	}

	replayTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin anchor replay: %v", err)
	}
	replayedAnchorID, err := completePlannedFirstFrameAnchorTx(ctx, replayTx, input, shot, output)
	if err != nil {
		_ = replayTx.Rollback(ctx)
		t.Fatalf("replay planned first-frame completion: %v", err)
	}
	if err := replayTx.Commit(ctx); err != nil {
		t.Fatalf("commit anchor replay: %v", err)
	}
	if replayedAnchorID != anchorID {
		t.Fatalf("replayed anchor = %s, want %s", replayedAnchorID, anchorID)
	}
	var anchorCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1 AND anchor_role = 'planned_first_frame'
	`, shot.ID).Scan(&anchorCount); err != nil {
		t.Fatalf("count replayed anchors: %v", err)
	}
	if anchorCount != staleAnchors+1 {
		t.Fatalf("anchor count after replay = %d, want %d", anchorCount, staleAnchors+1)
	}
}

func TestExtractRenderSegmentTailAnchorPersistsApprovedSameShotAnchor(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	ctx := context.Background()
	pool := openWorkflowGatewayIntegrationDB(t, ctx)
	t.Cleanup(pool.Close)
	activities, organizationID, userID, projectID, workflowRunID, _, shots := prepareEpisodeVideoCheckpointFixture(t, ctx, pool)
	if len(shots) == 0 {
		t.Fatal("fixture did not produce storyboard shots")
	}
	storageClient, ok := activities.storage.(*workflowMemoryStorage)
	if !ok {
		t.Fatalf("fixture storage = %T, want *workflowMemoryStorage", activities.storage)
	}

	var renderSegmentID string
	if err := pool.QueryRow(ctx, `
		SELECT segment.id::text
		FROM video_render_segments segment
		JOIN video_render_plans plan ON plan.id = segment.video_render_plan_id
		WHERE segment.storyboard_shot_id = $1 AND plan.active = true
		ORDER BY segment.segment_index
		LIMIT 1
	`, shots[0].ID).Scan(&renderSegmentID); err != nil {
		t.Fatalf("load render segment: %v", err)
	}

	videoPath := filepath.Join(t.TempDir(), "segment.mp4")
	writeComposeIntegrationClip(t, videoPath, "testsrc=size=160x90:rate=24")
	videoStorageKey := "tests/render-segment-tail/" + renderSegmentID + "/source.mp4"
	put, err := storageClient.PutFile(ctx, videoStorageKey, videoPath, "video/mp4")
	if err != nil {
		t.Fatalf("store source video: %v", err)
	}
	var videoArtifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key,
			mime_type, content_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, 'generated_video', $4, 'video/mp4', $5,
		        '{"fixture":"render_segment_tail_anchor"}', $6)
		RETURNING id::text
	`, organizationID, projectID, workflowRunID, videoStorageKey, put.ContentHash, userID).Scan(&videoArtifactID); err != nil {
		t.Fatalf("insert source artifact: %v", err)
	}
	var videoMediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, duration_seconds, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'video/mp4', $5, 0.25, 160, 90, $6,
		        '{"fixture":"render_segment_tail_anchor"}', $7)
		RETURNING id::text
	`, organizationID, projectID, videoArtifactID, videoStorageKey, put.ByteSize, put.ContentHash, userID).Scan(&videoMediaFileID); err != nil {
		t.Fatalf("insert source media file: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_render_segments
		SET status = 'succeeded', artifact_id = $2, media_file_id = $3, storage_key = $4,
		    production_readiness = 'ready', completed_at = now(), updated_at = now()
		WHERE id = $1
	`, renderSegmentID, videoArtifactID, videoMediaFileID, videoStorageKey); err != nil {
		t.Fatalf("complete render segment: %v", err)
	}

	input := ExtractRenderSegmentTailAnchorInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shots[0].ID, SourceRenderSegmentID: renderSegmentID,
		SourceVideoArtifactID: videoArtifactID, SourceVideoMediaFileID: videoMediaFileID,
		SourceVideoStorageKey: videoStorageKey,
	}
	output, err := activities.ExtractRenderSegmentTailAnchor(ctx, input)
	if err != nil {
		t.Fatalf("ExtractRenderSegmentTailAnchor: %v", err)
	}
	if output.AnchorID == "" || output.ArtifactID == "" || output.MediaFileID == "" ||
		output.SourceShotID != shots[0].ID || output.SourceRenderSegmentID != renderSegmentID ||
		output.SourceVideoArtifactID != videoArtifactID || output.MimeType != "image/png" ||
		output.Width != 160 || output.Height != 90 {
		t.Fatalf("tail anchor output = %+v", output)
	}
	if _, _, err := storageClient.GetObject(ctx, output.StorageKey, 4<<20); err != nil {
		t.Fatalf("load extracted tail anchor: %v", err)
	}

	var role, sourceRole, status, reviewStatus, persistedArtifactID string
	var revision int
	if err := pool.QueryRow(ctx, `
		SELECT anchor_role, source_role, status, review_status, revision, artifact_id::text
		FROM shot_visual_anchors
		WHERE id = $1 AND storyboard_shot_id = $2 AND source_render_segment_id = $3
	`, output.AnchorID, shots[0].ID, renderSegmentID).Scan(
		&role, &sourceRole, &status, &reviewStatus, &revision, &persistedArtifactID,
	); err != nil {
		t.Fatalf("load persisted tail anchor: %v", err)
	}
	if role != "observed_tail_frame" || sourceRole != "previous_segment_tail" ||
		status != "ready" || reviewStatus != "approved" || revision != 1 ||
		persistedArtifactID != output.ArtifactID {
		t.Fatalf("persisted tail anchor = role %s source %s status %s review %s revision %d artifact %s",
			role, sourceRole, status, reviewStatus, revision, persistedArtifactID)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM event_outbox
		WHERE project_id = $1 AND event_type = 'storyboard.shot.segment_tail_anchor.extracted'
		  AND aggregate_id = $2 AND payload->>'sourceRenderSegmentId' = $3
	`, projectID, output.AnchorID, renderSegmentID).Scan(&eventCount); err != nil {
		t.Fatalf("count tail anchor event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("tail anchor event count = %d, want 1", eventCount)
	}

	replayed, err := activities.ExtractRenderSegmentTailAnchor(ctx, input)
	if err != nil {
		t.Fatalf("replay ExtractRenderSegmentTailAnchor: %v", err)
	}
	if replayed.AnchorID != output.AnchorID || replayed.ArtifactID != output.ArtifactID {
		t.Fatalf("replayed output = %+v, want anchor %s artifact %s", replayed, output.AnchorID, output.ArtifactID)
	}
	var anchorCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1 AND source_render_segment_id = $2
		  AND anchor_role = 'observed_tail_frame'
	`, shots[0].ID, renderSegmentID).Scan(&anchorCount); err != nil {
		t.Fatalf("count replayed tail anchors: %v", err)
	}
	if anchorCount != 1 {
		t.Fatalf("tail anchor count after replay = %d, want 1", anchorCount)
	}

	secondVideoStorageKey := "tests/render-segment-tail/" + renderSegmentID + "/source-2.mp4"
	secondPut, err := storageClient.PutFile(ctx, secondVideoStorageKey, videoPath, "video/mp4")
	if err != nil {
		t.Fatalf("store second source video: %v", err)
	}
	var secondVideoArtifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, workflow_run_id, type, storage_key,
			mime_type, content_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, 'generated_video', $4, 'video/mp4', $5,
		        '{"fixture":"render_segment_tail_anchor_replacement"}', $6)
		RETURNING id::text
	`, organizationID, projectID, workflowRunID, secondVideoStorageKey, secondPut.ContentHash, userID).Scan(&secondVideoArtifactID); err != nil {
		t.Fatalf("insert second source artifact: %v", err)
	}
	var secondVideoMediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, duration_seconds, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'video/mp4', $5, 0.25, 160, 90, $6,
		        '{"fixture":"render_segment_tail_anchor_replacement"}', $7)
		RETURNING id::text
	`, organizationID, projectID, secondVideoArtifactID, secondVideoStorageKey,
		secondPut.ByteSize, secondPut.ContentHash, userID).Scan(&secondVideoMediaFileID); err != nil {
		t.Fatalf("insert second source media file: %v", err)
	}
	var secondRenderSegmentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO video_render_segments(
			organization_id, project_id, production_generation_id, video_render_plan_id,
			storyboard_shot_id, segment_index, planned_start_tick, planned_end_tick,
			requested_duration_seconds, continuity_mode, status, input_contract_key,
			input_contract_hash, source_video_prompt_plan_id, source_prompt_hash,
			artifact_id, media_file_id, storage_key, production_readiness, prompt,
			execution_prompt_hash, completed_at
		)
		SELECT source.organization_id, source.project_id, source.production_generation_id,
		       source.video_render_plan_id, source.storyboard_shot_id, bounds.next_index,
		       bounds.next_start_tick,
		       bounds.next_start_tick + (source.planned_end_tick - source.planned_start_tick),
		       source.requested_duration_seconds, source.continuity_mode, 'succeeded',
		       source.input_contract_key, source.input_contract_hash,
		       source.source_video_prompt_plan_id, source.source_prompt_hash,
		       $2, $3, $4, 'ready', source.prompt, source.execution_prompt_hash, now()
		FROM video_render_segments source
		CROSS JOIN LATERAL (
			SELECT COALESCE(max(candidate.segment_index), -1) + 1 AS next_index,
			       COALESCE(max(candidate.planned_end_tick), 0) AS next_start_tick
			FROM video_render_segments candidate
			WHERE candidate.video_render_plan_id = source.video_render_plan_id
		) bounds
		WHERE source.id = $1
		RETURNING id::text
	`, renderSegmentID, secondVideoArtifactID, secondVideoMediaFileID, secondVideoStorageKey).Scan(&secondRenderSegmentID); err != nil {
		t.Fatalf("insert second render segment: %v", err)
	}

	secondInput := ExtractRenderSegmentTailAnchorInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ShotID: shots[0].ID, SourceRenderSegmentID: secondRenderSegmentID,
		SourceVideoArtifactID: secondVideoArtifactID, SourceVideoMediaFileID: secondVideoMediaFileID,
		SourceVideoStorageKey: secondVideoStorageKey,
	}
	secondOutput, err := activities.ExtractRenderSegmentTailAnchor(ctx, secondInput)
	if err != nil {
		t.Fatalf("ExtractRenderSegmentTailAnchor replacement: %v", err)
	}
	if secondOutput.AnchorID == output.AnchorID {
		t.Fatalf("replacement anchor reused prior anchor %s", output.AnchorID)
	}

	var priorStatus, priorReviewStatus, supersededBy string
	if err := pool.QueryRow(ctx, `
		SELECT status, review_status,
		       COALESCE(metadata->>'supersededBySourceRenderSegmentId', '')
		FROM shot_visual_anchors
		WHERE id = $1
	`, output.AnchorID).Scan(&priorStatus, &priorReviewStatus, &supersededBy); err != nil {
		t.Fatalf("load superseded tail anchor: %v", err)
	}
	if priorStatus != "stale" || priorReviewStatus != "needs_edit" || supersededBy != secondRenderSegmentID {
		t.Fatalf("prior anchor = status %s review %s supersededBy %s", priorStatus, priorReviewStatus, supersededBy)
	}
	var secondRevision, approvedCount int
	if err := pool.QueryRow(ctx, `
		SELECT revision FROM shot_visual_anchors WHERE id = $1
	`, secondOutput.AnchorID).Scan(&secondRevision); err != nil {
		t.Fatalf("load replacement revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM shot_visual_anchors
		WHERE storyboard_shot_id = $1 AND anchor_role = 'observed_tail_frame'
		  AND status = 'ready' AND review_status = 'approved'
	`, shots[0].ID).Scan(&approvedCount); err != nil {
		t.Fatalf("count approved replacement anchors: %v", err)
	}
	if secondRevision != 2 || approvedCount != 1 {
		t.Fatalf("replacement anchor revision=%d approved=%d, want revision=2 approved=1", secondRevision, approvedCount)
	}
	replayedSecond, err := activities.ExtractRenderSegmentTailAnchor(ctx, secondInput)
	if err != nil {
		t.Fatalf("replay replacement tail anchor: %v", err)
	}
	if replayedSecond.AnchorID != secondOutput.AnchorID {
		t.Fatalf("replayed replacement anchor = %s, want %s", replayedSecond.AnchorID, secondOutput.AnchorID)
	}
}

func prepareEpisodeVideoCheckpointFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) (Activities, string, string, string, string, string, []StoryboardShotRecord) {
	t.Helper()
	organizationID, userID, projectID, workflowRunID, textModelID, _ := seedWorkflowGatewayIntegrationDataForProfileAndConfiguration(
		t,
		ctx,
		pool,
		videoproduction.ProfileSingleFrameI2V,
		videoproduction.ProductionConfigurationSnapshot{
			VideoRatio:       "16:9",
			AudioStrategy:    "native_av",
			AudioRequirement: "required",
			ImageQuality:     "standard",
		},
	)
	scriptID, versionID, episodeID, sceneIDs := seedStoryboardEpisodeV2Script(t, ctx, pool, organizationID, projectID, userID)
	assets := seedStoryboardEpisodeV2Assets(t, ctx, pool, organizationID, projectID, userID)
	callIDs := seedStoryboardEpisodeV2ProviderCalls(t, ctx, pool, organizationID, projectID, workflowRunID, textModelID, 13)
	var timingOutput TimingAnalysisActivityOutput
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("create provider vault: %v", err)
	}
	videoPlanner := provider.NewService(pool, vault)
	videoPlanner.EnableGatewayRuntime()
	gateway := httptest.NewServer(mockStoryboardEpisodeV2GatewayWithConfig(
		t, textModelID, callIDs, assets, sceneIDs, &timingOutput,
		storyboardEpisodeV2GatewayConfig{VideoPlanner: videoPlanner.PlanVideo},
	))
	t.Cleanup(gateway.Close)
	activities := NewActivities(pool, newWorkflowMemoryStorage(), &provider.GatewayClient{
		BaseURL: gateway.URL, Token: "workflow-service-token", Client: gateway.Client(),
	})

	var activitySuite testsuite.WorkflowTestSuite
	activityEnv := activitySuite.NewTestActivityEnvironment()
	activityEnv.RegisterActivity(activities.AnalyzeEpisodeTiming)
	encodedTimingOutput, err := activityEnv.ExecuteActivity(activities.AnalyzeEpisodeTiming, AnalyzeEpisodeTimingInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID, ScriptEpisodeID: episodeID,
	})
	if err != nil {
		t.Fatalf("AnalyzeEpisodeTiming: %v", err)
	}
	if err := encodedTimingOutput.Get(&timingOutput); err != nil {
		t.Fatalf("decode AnalyzeEpisodeTiming output: %v", err)
	}
	blueprint, err := activities.BuildEpisodeContinuityBlueprint(ctx, BuildEpisodeContinuityBlueprintInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, PacingProfile: "standard", Timing: timingOutput,
	})
	if err != nil {
		t.Fatalf("BuildEpisodeContinuityBlueprint: %v", err)
	}
	for _, scene := range blueprint.ScenePlans {
		output, err := activities.PlanStoryboardScene(ctx, PlanStoryboardSceneInput{
			OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
			CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
			ScriptEpisodeID: episodeID, StoryboardPlanID: blueprint.StoryboardPlanID,
			BlueprintID: blueprint.BlueprintID, ScenePlanID: scene.ID,
			SceneKey: scene.SceneKey, SceneOrdinal: scene.SceneOrdinal,
		})
		if err != nil {
			t.Fatalf("PlanStoryboardScene: %v", err)
		}
		persistStoryboardEpisodeV2ReferencePacks(t, ctx, pool, activities, organizationID, projectID, workflowRunID, userID, output.Shots)
		for _, shot := range output.Shots {
			if _, err := activities.PrepareShotImagePrompt(ctx, PrepareShotImagePromptInput{
				OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
				CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotOrdinal, ShotNo: shot.ShotOrdinal + 1,
				WorkflowPrompt: "生成当前镜头首帧提示词", AspectRatio: "16:9", Size: "1536x1024",
			}); err != nil {
				t.Fatalf("PrepareShotImagePrompt %s: %v", shot.ID, err)
			}
			approveStoryboardEpisodeV2Anchor(t, ctx, pool, organizationID, projectID, userID, shot.ID, shot.ShotOrdinal)
			if _, err := activities.PrepareShotVideoPrompt(ctx, PrepareShotVideoPromptInput{
				OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
				CreatedBy: userID, ShotID: shot.ID, ShotIndex: shot.ShotOrdinal, ShotNo: shot.ShotOrdinal + 1,
				WorkflowPrompt: "生成当前镜头视频提示词", AspectRatio: "16:9", Resolution: "720p",
			}); err != nil {
				t.Fatalf("PrepareShotVideoPrompt %s: %v", shot.ID, err)
			}
		}
	}
	review, err := activities.ReviewStoryboardPlan(ctx, ReviewStoryboardPlanInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptEpisodeID: episodeID, StoryboardPlanID: blueprint.StoryboardPlanID, ReviewAttempt: 1,
	})
	if err != nil || !review.Approved {
		t.Fatalf("ReviewStoryboardPlan: output=%+v err=%v", review, err)
	}
	activated, err := activities.ActivateStoryboardPlan(ctx, ActivateStoryboardPlanActivityInput{
		OrganizationID: organizationID, ProjectID: projectID, WorkflowRunID: workflowRunID,
		CreatedBy: userID, ScriptID: scriptID, ScriptVersionID: versionID,
		ScriptEpisodeID: episodeID, EpisodeIndex: 1, EpisodeTotal: 1,
		EpisodeTitle: "第一集", StoryboardPlanID: blueprint.StoryboardPlanID,
	})
	if err != nil {
		t.Fatalf("ActivateStoryboardPlan: %v", err)
	}
	videoModelID := seedStoryboardEpisodeV2VideoModel(t, ctx, pool, textModelID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
		SELECT profile.id, $1, 100, 100, true
		FROM model_profiles profile
		WHERE profile.organization_id = $2 AND profile.profile_key = 'video_generation_default'
	`, videoModelID, organizationID); err != nil {
		t.Fatalf("bind video model: %v", err)
	}
	assertStoryboardEpisodeV2VideoPlans(t, ctx, pool, activities, organizationID, projectID, workflowRunID, videoModelID, activated.Shots)
	return activities, organizationID, userID, projectID, workflowRunID, episodeID, activated.Shots
}

func insertEpisodeVideoWorkflowRun(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	organizationID string,
	projectID string,
	createdBy string,
	sourceWorkflowRunID string,
) string {
	t.Helper()
	var workflowRunID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workflow_runs(
			organization_id, project_id, temporal_workflow_id, status, input, output, created_by,
			production_generation_id, video_production_binding_id, video_production_binding_revision
		)
		SELECT $1, $2, $3, 'queued', '{}'::jsonb, '{}'::jsonb, $4,
		       production_generation_id, video_production_binding_id, video_production_binding_revision
		FROM workflow_runs WHERE id = $5
		RETURNING id::text
	`, organizationID, projectID, "episode-video-retry-"+uuid.NewString(), createdBy, sourceWorkflowRunID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert retry workflow run: %v", err)
	}
	return workflowRunID
}

func TestSelectIndependentEpisodeVideoShotsUsesConfiguredConcurrency(t *testing.T) {
	remaining := []string{"shot-1", "shot-2", "shot-3", "shot-4", "shot-5", "shot-6"}
	available := map[string]bool{}
	for _, shotID := range remaining {
		available[shotID] = true
	}
	selected, err := selectIndependentEpisodeVideoShots(remaining, available, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(selected, ","), "shot-1,shot-2,shot-3,shot-4,shot-5"; got != want {
		t.Fatalf("selected = %s, want %s", got, want)
	}
}
