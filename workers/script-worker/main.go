package main

import (
	"context"
	"log"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/Einzieg/cineweave/workers/workerkit"
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
	if settled, err := workflows.ReconcileFailedWorkflowNodes(ctx, pool); err != nil {
		log.Fatal(err)
	} else if settled > 0 {
		log.Printf("reconciled %d unfinished nodes from failed workflows", settled)
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
	temporalWorker.RegisterWorkflow(workflows.ShotVideoContinuityGroupWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchCancelShotVideosWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateAssetCardsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.BatchGenerateCanonicalAssetImagesWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateAssetCardItemWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateCanonicalAssetImageItemWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardText, workflowActivityOptions("GenerateStoryboardText"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardImage, workflowActivityOptions("GenerateStoryboardImage"))
	temporalWorker.RegisterActivityWithOptions(activities.ExtractNovelEvents, workflowActivityOptions("ExtractNovelEvents"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateAdaptationPlan, workflowActivityOptions("GenerateAdaptationPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateScriptFromAdaptationPlan, workflowActivityOptions("GenerateScriptFromAdaptationPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareScriptFromSource, workflowActivityOptions("PrepareScriptFromSource"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateSourceScriptEpisode, workflowActivityOptions("GenerateSourceScriptEpisode"))
	temporalWorker.RegisterActivityWithOptions(activities.FailSourceScriptEpisode, workflowActivityOptions("FailSourceScriptEpisode"))
	temporalWorker.RegisterActivityWithOptions(activities.FinalizeScriptFromSource, workflowActivityOptions("FinalizeScriptFromSource"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateScriptFromSource, workflowActivityOptions("GenerateScriptFromSource"))
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
	temporalWorker.RegisterActivityWithOptions(activities.GenerateAssetCardBatchItem, workflowActivityOptions("GenerateAssetCardBatchItem"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateCanonicalAssetImageBatchItem, workflowActivityOptions("GenerateCanonicalAssetImageBatchItem"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteAssetBatchWorkflow, workflowActivityOptions("CompleteAssetBatchWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteComposeTimelineWorkflow, workflowActivityOptions("CompleteComposeTimelineWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.ListStoryboardShots, workflowActivityOptions("ListStoryboardShots"))
	temporalWorker.RegisterActivityWithOptions(activities.ListRunningShotVideoTasks, workflowActivityOptions("ListRunningShotVideoTasks"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareShotVideoExecutionGroups, workflowActivityOptions("PrepareShotVideoExecutionGroups"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareShotImagePrompt, workflowActivityOptions("PrepareShotImagePrompt"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateShotImage, workflowActivityOptions("GenerateShotImage"))
	temporalWorker.RegisterActivityWithOptions(activities.PrepareShotVideoPrompt, workflowActivityOptions("PrepareShotVideoPrompt"))
	temporalWorker.RegisterActivityWithOptions(activities.PlanShotVideo, workflowActivityOptions("PlanShotVideo"))
	temporalWorker.RegisterActivityWithOptions(activities.FinalizeShotVideoPromptPlan, workflowActivityOptions("FinalizeShotVideoPromptPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.LoadPreparedShotVideoPlan, workflowActivityOptions("LoadPreparedShotVideoPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.EnsurePreparedShotVideoPlan, workflowActivityOptions("EnsurePreparedShotVideoPlan"))
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
	if err := workerkit.RunTemporalWorker(temporalWorker, worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}

func workflowActivityOptions(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}
