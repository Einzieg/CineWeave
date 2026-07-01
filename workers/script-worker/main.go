package main

import (
	"context"
	"log"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/Einzieg/cineweave/internal/workflows"
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

	storageClient, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}

	temporalClient, err := client.Dial(client.Options{
		HostPort: config.Get("TEMPORAL_ADDRESS", "localhost:7233"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer temporalClient.Close()

	temporalWorker := worker.New(temporalClient, workflows.ScriptTaskQueue, worker.Options{})
	gatewayClient := provider.NewGatewayClientFromEnv()
	activities := workflows.NewActivities(pool, storageClient, gatewayClient)
	temporalWorker.RegisterWorkflow(workflows.TextToStoryboardWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ExtractNovelEventsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.GenerateAdaptationPlanWorkflow)
	temporalWorker.RegisterWorkflow(workflows.AdaptationPlanToScriptWorkflow)
	temporalWorker.RegisterWorkflow(workflows.SourceToScriptWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ScriptToAssetsWorkflow)
	temporalWorker.RegisterWorkflow(workflows.ScriptToStoryboardWorkflow)
	temporalWorker.RegisterWorkflow(workflows.StoryboardToImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.StoryboardToVideoWorkflow)
	temporalWorker.RegisterWorkflow(workflows.VideoComposeWorkflow)
	temporalWorker.RegisterWorkflow(workflows.VideoProductionWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateCanonicalAssetImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateDerivedAssetImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateShotImageWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateShotVideoWorkflow)
	temporalWorker.RegisterWorkflow(workflows.RegenerateFinalVideoWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardText, workflowActivityOptions("GenerateStoryboardText"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardImage, workflowActivityOptions("GenerateStoryboardImage"))
	temporalWorker.RegisterActivityWithOptions(activities.ExtractNovelEvents, workflowActivityOptions("ExtractNovelEvents"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateAdaptationPlan, workflowActivityOptions("GenerateAdaptationPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateScriptFromAdaptationPlan, workflowActivityOptions("GenerateScriptFromAdaptationPlan"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateScriptFromSource, workflowActivityOptions("GenerateScriptFromSource"))
	temporalWorker.RegisterActivityWithOptions(activities.AnalyzeScriptAssets, workflowActivityOptions("AnalyzeScriptAssets"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateStoryboardFromScript, workflowActivityOptions("GenerateStoryboardFromScript"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateCanonicalAssetImage, workflowActivityOptions("GenerateCanonicalAssetImage"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateDerivedAssetImage, workflowActivityOptions("GenerateDerivedAssetImage"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteScriptAssetsWorkflow, workflowActivityOptions("CompleteScriptAssetsWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteScriptStoryboardWorkflow, workflowActivityOptions("CompleteScriptStoryboardWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteNovelEventExtractionWorkflow, workflowActivityOptions("CompleteNovelEventExtractionWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteAdaptationPlanWorkflow, workflowActivityOptions("CompleteAdaptationPlanWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteAdaptationPlanToScriptWorkflow, workflowActivityOptions("CompleteAdaptationPlanToScriptWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteSourceToScriptWorkflow, workflowActivityOptions("CompleteSourceToScriptWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteTextToStoryboardWorkflow, workflowActivityOptions("CompleteTextToStoryboardWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.CompleteRegenerationWorkflow, workflowActivityOptions("CompleteRegenerationWorkflow"))
	temporalWorker.RegisterActivityWithOptions(activities.ListStoryboardShots, workflowActivityOptions("ListStoryboardShots"))
	temporalWorker.RegisterActivityWithOptions(activities.GenerateShotImage, workflowActivityOptions("GenerateShotImage"))
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

	if err := temporalWorker.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}

func workflowActivityOptions(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}
