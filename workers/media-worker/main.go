package main

import (
	"context"
	"log"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
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

	activities := workflows.NewActivities(pool, storageClient, nil)
	workerOptions, err := workerkit.TemporalWorkerOptions("media-worker")
	if err != nil {
		log.Fatal(err)
	}
	temporalWorker := worker.New(temporalClient, workflows.MediaTaskQueue, workerOptions)
	temporalWorker.RegisterWorkflow(workflows.ExportProjectWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.ComposeFinalVideo, activity.RegisterOptions{Name: "ComposeFinalVideo"})
	temporalWorker.RegisterActivityWithOptions(activities.ProcessRenderSegmentMedia, activity.RegisterOptions{Name: "ProcessRenderSegmentMedia"})
	temporalWorker.RegisterActivityWithOptions(activities.ComposeShotRenderPlanMedia, activity.RegisterOptions{Name: "ComposeShotRenderPlanMedia"})
	temporalWorker.RegisterActivityWithOptions(activities.ExtractShotContinuityFrame, activity.RegisterOptions{Name: "ExtractShotContinuityFrame"})
	temporalWorker.RegisterActivityWithOptions(activities.ExportProject, activity.RegisterOptions{Name: "ExportProject"})

	if err := workerkit.RunTemporalWorker(temporalWorker, worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
