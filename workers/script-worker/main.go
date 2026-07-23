package main

import (
	"context"
	"log"
	"time"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/Einzieg/cineweave/workers/workerkit"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	ctx := context.Background()
	pool, err := db.Open(ctx, config.Get("DATABASE_URL", "postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if settled, err := workflows.ReconcileTerminalWorkflowNodes(ctx, pool); err != nil {
		log.Fatal(err)
	} else if settled > 0 {
		log.Printf("reconciled %d unfinished nodes from terminal workflows", settled)
	}

	storageClient, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}

	temporalClient, err := client.Dial(client.Options{
		HostPort:  config.Get("TEMPORAL_ADDRESS", "localhost:7233"),
		Namespace: config.Get("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer temporalClient.Close()

	workerOptions, err := workerkit.TemporalWorkerOptions("script-worker")
	if err != nil {
		log.Fatal(err)
	}
	temporalWorker := worker.New(temporalClient, workflows.ScriptTaskQueue, workerOptions)
	gatewayClient := provider.NewGatewayClientFromEnv()
	activities := workflows.NewActivities(pool, storageClient, gatewayClient)
	commerceSetupActivities := workflows.NewCommerceSetupActivities(activities, workflows.NewCommerceSetupRuntime(pool, gatewayClient))
	workflows.RegisterCommerceSetupWorkflow(temporalWorker)
	workflows.RegisterCommerceSetupActivity(temporalWorker, commerceSetupActivities)
	commerceGenerationActivities := workflows.NewCommerceActivities(activities, workflows.NewCommerceGenerationRuntime(pool))
	workflows.RegisterCommerceGenerationWorkflows(temporalWorker)
	workflows.RegisterCommerceGenerationActivities(temporalWorker, commerceGenerationActivities)
	temporalWorker.RegisterWorkflow(workflows.TextToStoryboardWorkflow)
	temporalWorker.RegisterWorkflow(workflows.TemporalReleaseCanaryWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ExtractNovelEventsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateAdaptationPlanWorkflow)
	temporalWorker.RegisterWorkflow(workflows.AdaptationPlanToScriptWorkflow)
	temporalWorker.RegisterWorkflow(workflows.SourceToScriptWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateSourceScriptEpisodeWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ParseScriptScenesWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ScriptToAssetsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ScriptToStoryboardWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ScriptEpisodeToStoryboardWorkflow)
	temporalWorker.RegisterWorkflow(workflows.StoryboardScenePlanWorkflow)
	temporalWorker.RegisterWorkflow(workflows.AnalyzeScriptEpisodeTimingWorkflow)
	temporalWorker.RegisterWorkflow(workflows.StoryboardToImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.StoryboardToVideoWorkflow)
	temporalWorker.RegisterWorkflow(workflows.VideoComposeWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ComposeTimelineWorkflow)
	temporalWorker.RegisterWorkflow(workflows.VideoProductionWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateCanonicalAssetImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateDerivedAssetImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateShotImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateShotVideoWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateFinalVideoWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateScriptSceneWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateSceneStoryboardWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateShotImagePromptsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateShotImagesWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateShotVideoPromptsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateShotVideosWorkflow)
	temporalWorker.RegisterWorkflow(workflows.EpisodeBatchGenerateShotVideosWorkflow)
	temporalWorker.RegisterWorkflow(workflows.EpisodeVideoProductionWorkflow)
	temporalWorker.RegisterWorkflow(workflows.SceneOrShotBatchWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchCancelShotVideosWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateAssetCardsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateCanonicalAssetImagesWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateDerivedAssetImagesWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateAssetCardItemWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateCanonicalAssetImageItemWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ProjectVideoProductionRebuildWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardText, workflowActivityOptions("GenerateStoryboardText"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardImage, workflowActivityOptions("GenerateStoryboardImage"))
	temporalWorker.RegisterActivityWithOptions(activities.ExtractNovelEvents, workflowActivityOptions("ExtractNovelEvents"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateAdaptationPlan, workflowActivityOptions("GenerateAdaptationPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateScriptFromAdaptationPlan, workflowActivityOptions("GenerateScriptFromAdaptationPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareScriptFromSource, workflowActivityOptions("PrepareScriptFromSource"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateSourceScriptEpisode, workflowActivityOptions("GenerateSourceScriptEpisode"))
	temporalWorker.RegisterActivityWithOptions(activities.FailSourceScriptEpisode, workflowActivityOptions("FailSourceScriptEpisode"))
	temporalWorker.RegisterActivityWithOptions(activities.FinalizeScriptFromSource, workflowActivityOptions("FinalizeScriptFromSource"))
	temporalWorker.RegisterActivityWithOptions(activities.ParseScriptScenes, workflowActivityOptions("ParseScriptScenes"))
	temporalWorker.RegisterActivityWithOptions(activities.RegenerateScriptScene, workflowActivityOptions("RegenerateScriptScene"))
	temporalWorker.RegisterActivityWithOptions(activities.AnalyzeScriptAssets, workflowActivityOptions("AnalyzeScriptAssets"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareScriptStoryboard, workflowActivityOptions("PrepareScriptStoryboard"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardFromScript, workflowActivityOptions("GenerateStoryboardFromScript"))
	temporalWorker.RegisterActivityWithOptions(activities.AnalyzeEpisodeTiming, workflowActivityOptions("AnalyzeEpisodeTiming"))
	temporalWorker.RegisterActivityWithOptions(activities.BuildEpisodeContinuityBlueprint, workflowActivityOptions("BuildEpisodeContinuityBlueprint"))
	temporalWorker.RegisterActivityWithOptions(activities.PlanStoryboardScene, workflowActivityOptions("PlanStoryboardScene"))
	temporalWorker.RegisterActivityWithOptions(activities.ReviewStoryboardPlan, workflowActivityOptions("ReviewStoryboardPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.ActivateStoryboardPlan, workflowActivityOptions("ActivateStoryboardPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteEpisodeTimingWorkflow, workflowActivityOptions("CompleteEpisodeTimingWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateCanonicalAssetImage, workflowActivityOptions("GenerateCanonicalAssetImage"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateDerivedAssetImage, workflowActivityOptions("GenerateDerivedAssetImage"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteScriptAssetsWorkflow, workflowActivityOptions("CompleteScriptAssetsWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteScriptStoryboardWorkflow, workflowActivityOptions("CompleteScriptStoryboardWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.FailScriptStoryboardWorkflow, workflowActivityOptions("FailScriptStoryboardWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteNovelEventExtractionWorkflow, workflowActivityOptions("CompleteNovelEventExtractionWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteAdaptationPlanWorkflow, workflowActivityOptions("CompleteAdaptationPlanWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteAdaptationPlanToScriptWorkflow, workflowActivityOptions("CompleteAdaptationPlanToScriptWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteSourceToScriptWorkflow, workflowActivityOptions("CompleteSourceToScriptWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteScriptScenesWorkflow, workflowActivityOptions("CompleteScriptScenesWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteTextToStoryboardWorkflow, workflowActivityOptions("CompleteTextToStoryboardWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteRegenerationWorkflow, workflowActivityOptions("CompleteRegenerationWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteBatchShotProductionWorkflow, workflowActivityOptions("CompleteBatchShotProductionWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.FinalizeBatchShotProductionCancellation, workflowActivityOptions("FinalizeBatchShotProductionCancellation"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareEpisodeVideoProductions, workflowActivityOptions("PrepareEpisodeVideoProductions"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareEpisodeVideoProductionBatch, workflowActivityOptions("PrepareEpisodeVideoProductionBatch"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareEpisodeVideoProductionBatchV2, workflowActivityOptions("PrepareEpisodeVideoProductionBatchV2"))
	temporalWorker.RegisterActivityWithOptions(activities.CommitEpisodeVideoProductionBatch, workflowActivityOptions("CommitEpisodeVideoProductionBatch"))
	temporalWorker.RegisterActivityWithOptions(activities.CommitEpisodeVideoProductionBatchV2, workflowActivityOptions("CommitEpisodeVideoProductionBatchV2"))
	temporalWorker.RegisterActivityWithOptions(activities.ReconcileEpisodeVideoProductionCheckpointV2, workflowActivityOptions("ReconcileEpisodeVideoProductionCheckpointV2"))
	temporalWorker.RegisterActivityWithOptions(activities.LoadEpisodeVideoProductionOutputV2, workflowActivityOptions("LoadEpisodeVideoProductionOutputV2"))
	temporalWorker.RegisterActivityWithOptions(activities.CancelEpisodeVideoProductionCheckpoint, workflowActivityOptions("CancelEpisodeVideoProductionCheckpoint"))
	temporalWorker.RegisterActivityWithOptions(activities.FailEpisodeVideoProductionCheckpoint, workflowActivityOptions("FailEpisodeVideoProductionCheckpoint"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateAssetCardBatchItem, workflowActivityOptions("GenerateAssetCardBatchItem"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateCanonicalAssetImageBatchItem, workflowActivityOptions("GenerateCanonicalAssetImageBatchItem"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteAssetBatchWorkflow, workflowActivityOptions("CompleteAssetBatchWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.LoadDerivedAssetExecutionItems, workflowActivityOptions("LoadDerivedAssetExecutionItems"))
	temporalWorker.RegisterActivityWithOptions(activities.ClaimDerivedAssetExecution, workflowActivityOptions("ClaimDerivedAssetExecution"))
	temporalWorker.RegisterActivityWithOptions(activities.RunDerivedAssetProvider, workflowActivityOptions("RunDerivedAssetProvider"))
	temporalWorker.RegisterActivityWithOptions(activities.VerifyDerivedAssetMedia, workflowActivityOptions("VerifyDerivedAssetMedia"))
	temporalWorker.RegisterActivityWithOptions(activities.CommitDerivedAssetExecution, workflowActivityOptions("CommitDerivedAssetExecution"))
	temporalWorker.RegisterActivityWithOptions(activities.FailDerivedAssetExecution, workflowActivityOptions("FailDerivedAssetExecution"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteDerivedAssetBatchWorkflowV2, workflowActivityOptions("CompleteDerivedAssetBatchWorkflowV2"))
	temporalWorker.RegisterActivityWithOptions(activities.ReconcileExpiredDerivedAssetExecutions, workflowActivityOptions("ReconcileExpiredDerivedAssetExecutions"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareProjectVideoProductionRebuild, workflowActivityOptions("PrepareProjectVideoProductionRebuild"))
	temporalWorker.RegisterActivityWithOptions(activities.CheckProjectVideoProductionDrain, workflowActivityOptions("CheckProjectVideoProductionDrain"))
	temporalWorker.RegisterActivityWithOptions(activities.SwitchProjectVideoProductionGeneration, workflowActivityOptions("SwitchProjectVideoProductionGeneration"))
	temporalWorker.RegisterActivityWithOptions(activities.ListProjectVideoProductionRebuildItems, workflowActivityOptions("ListProjectVideoProductionRebuildItems"))
	temporalWorker.RegisterActivityWithOptions(activities.StartProjectVideoProductionRebuildItem, workflowActivityOptions("StartProjectVideoProductionRebuildItem"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteProjectVideoProductionRebuildItem, workflowActivityOptions("CompleteProjectVideoProductionRebuildItem"))
	temporalWorker.RegisterActivityWithOptions(activities.FailProjectVideoProductionRebuildItem, workflowActivityOptions("FailProjectVideoProductionRebuildItem"))
	temporalWorker.RegisterActivityWithOptions(activities.FinalizeProjectVideoProductionRebuild, workflowActivityOptions("FinalizeProjectVideoProductionRebuild"))
	temporalWorker.RegisterActivityWithOptions(activities.FailProjectVideoProductionRebuild, workflowActivityOptions("FailProjectVideoProductionRebuild"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteComposeTimelineWorkflow, workflowActivityOptions("CompleteComposeTimelineWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.ListStoryboardShots, workflowActivityOptions("ListStoryboardShots"))
	temporalWorker.RegisterActivityWithOptions(activities.ListRunningShotVideoTasks, workflowActivityOptions("ListRunningShotVideoTasks"))
	registerShotAnchorActivities(temporalWorker, activities)
	temporalWorker.RegisterActivityWithOptions(activities.PrepareShotImagePrompt, workflowActivityOptions("PrepareShotImagePrompt"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateShotImage, workflowActivityOptions("GenerateShotImage"))
	temporalWorker.RegisterActivityWithOptions(activities.ReviewStoryboardSheetOutput, workflowActivityOptions("ReviewStoryboardSheetOutput"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareShotVideoPrompt, workflowActivityOptions("PrepareShotVideoPrompt"))
	temporalWorker.RegisterActivityWithOptions(activities.ReconcileStoryboardDialogueAssignments, workflowActivityOptions("ReconcileStoryboardDialogueAssignments"))
	temporalWorker.RegisterActivityWithOptions(activities.PlanShotVideo, workflowActivityOptions("PlanShotVideo"))
	temporalWorker.RegisterActivityWithOptions(activities.FinalizeShotVideoPromptPlan, workflowActivityOptions("FinalizeShotVideoPromptPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.LoadPreparedShotVideoPlan, workflowActivityOptions("LoadPreparedShotVideoPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.EnsurePreparedShotVideoPlan, workflowActivityOptions("EnsurePreparedShotVideoPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.EnsurePreparedShotVideoPlanV2, workflowActivityOptions("EnsurePreparedShotVideoPlanV2"))
	temporalWorker.RegisterActivityWithOptions(activities.LoadApprovedShotVideoPromptPlanV2, workflowActivityOptions("LoadApprovedShotVideoPromptPlanV2"))
	temporalWorker.RegisterActivityWithOptions(activities.MaterializeAndBindExecutableShotVideoPlanV2, workflowActivityOptions("MaterializeAndBindExecutableShotVideoPlanV2"))
	temporalWorker.RegisterActivityWithOptions(activities.LoadExecutableShotVideoPlanV2, workflowActivityOptions("LoadExecutableShotVideoPlanV2"))
	temporalWorker.RegisterActivityWithOptions(activities.RetryShotVideoRenderSegment, workflowActivityOptions("RetryShotVideoRenderSegment"))
	temporalWorker.RegisterActivityWithOptions(activities.CreateShotVideoTask, workflowActivityOptions("CreateShotVideoTask"))
	temporalWorker.RegisterActivityWithOptions(activities.PollShotVideoTask, workflowActivityOptions("PollShotVideoTask"))
	temporalWorker.RegisterActivityWithOptions(activities.CancelShotVideoTask, workflowActivityOptions("CancelShotVideoTask"))
	temporalWorker.RegisterActivityWithOptions(activities.CreateStoryboardVideoTask, workflowActivityOptions("CreateStoryboardVideoTask"))
	temporalWorker.RegisterActivityWithOptions(activities.PollStoryboardVideoTask, workflowActivityOptions("PollStoryboardVideoTask"))
	temporalWorker.RegisterActivityWithOptions(activities.CancelStoryboardVideoTask, workflowActivityOptions("CancelStoryboardVideoTask"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteVideoProductionWorkflow, workflowActivityOptions("CompleteVideoProductionWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.FailVideoProductionWorkflow, workflowActivityOptions("FailVideoProductionWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CancelVideoProductionWorkflow, workflowActivityOptions("CancelVideoProductionWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateScriptStoryboard, workflowActivityOptions("GenerateScriptStoryboard"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardImages, workflowActivityOptions("GenerateStoryboardImages"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardVideos, workflowActivityOptions("GenerateStoryboardVideos"))
	temporalWorker.RegisterActivityWithOptions(activities.ComposeTimeline, workflowActivityOptions("ComposeTimeline"))
	temporalWorker.RegisterActivityWithOptions(activities.QualityCheck, workflowActivityOptions("QualityCheck"))
	reconcilerCtx, cancelReconciler := context.WithCancel(ctx)
	defer cancelReconciler()
	go runEpisodeVideoCheckpointReconciler(reconcilerCtx, pool)
	go runSourceToScriptPayloadCleanup(reconcilerCtx, pool)
	go runDerivedAssetExecutionReconciler(reconcilerCtx, activities)
	if err := workerkit.RunTemporalWorker(temporalWorker, worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}

func runDerivedAssetExecutionReconciler(ctx context.Context, activities workflows.Activities) {
	interval := config.Duration("CINEWEAVE_DERIVED_ASSET_RECONCILE_INTERVAL", time.Minute)
	batchSize := config.Int("CINEWEAVE_DERIVED_ASSET_RECONCILE_BATCH_SIZE", 32)
	if interval <= 0 {
		interval = time.Minute
	}
	reconcile := func() {
		count, err := workflows.ReconcileExpiredDerivedAssetExecutions(ctx, activities, batchSize)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("reconcile derived asset executions: %v", err)
			}
			return
		}
		if count > 0 {
			log.Printf("reconciled %d derived asset executions", count)
		}
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

func runSourceToScriptPayloadCleanup(ctx context.Context, pool *pgxpool.Pool) {
	interval := config.Duration("CINEWEAVE_SOURCE_TO_SCRIPT_PAYLOAD_CLEANUP_INTERVAL", 6*time.Hour)
	batchSize := config.Int("CINEWEAVE_SOURCE_TO_SCRIPT_PAYLOAD_CLEANUP_BATCH_SIZE", 100)
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	cleanup := func() {
		result, err := workflows.PurgeExpiredSourceToScriptPayloads(ctx, pool, batchSize)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("purge expired source-to-script payloads: %v", err)
			}
			return
		}
		if result.Generations > 0 {
			log.Printf(
				"purged source-to-script payloads for %d generations (%d source items, %d generated results)",
				result.Generations,
				result.Items,
				result.Results,
			)
		}
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func runEpisodeVideoCheckpointReconciler(ctx context.Context, pool *pgxpool.Pool) {
	interval := config.Duration("CINEWEAVE_EPISODE_VIDEO_RECONCILE_INTERVAL", time.Minute)
	staleAfter := config.Duration("CINEWEAVE_EPISODE_VIDEO_STALE_AFTER", 10*time.Minute)
	batchSize := config.Int("CINEWEAVE_EPISODE_VIDEO_RECONCILE_BATCH_SIZE", 32)
	if interval <= 0 {
		interval = time.Minute
	}
	reconcile := func() {
		count, err := workflows.ReconcileStuckEpisodeVideoProductionCheckpoints(ctx, pool, staleAfter, batchSize)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("reconcile stuck episode video checkpoints: %v", err)
			}
			return
		}
		if count > 0 {
			log.Printf("reconciled %d stuck episode video checkpoints", count)
		}
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

func workflowActivityOptions(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

type scriptActivityRegistrar interface {
	RegisterActivityWithOptions(a interface{}, options activity.RegisterOptions)
}

func registerShotAnchorActivities(registrar scriptActivityRegistrar, activities workflows.Activities) {
	registrar.RegisterActivityWithOptions(activities.ResolveShotAnchorWorkItems, workflowActivityOptions("ResolveShotAnchorWorkItems"))
}
