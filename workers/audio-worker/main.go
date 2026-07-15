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
	workerOptions, err := workerkit.TemporalWorkerOptions("audio-worker")
	if err != nil {
		log.Fatal(err)
	}
	activities := workflows.NewActivities(pool, storageClient, provider.NewGatewayClientFromEnv())
	temporalWorker := worker.New(temporalClient, workflows.AudioTaskQueue, workerOptions)
	temporalWorker.RegisterWorkflow(workflows.EpisodeAudioProductionWorkflow)
	temporalWorker.RegisterWorkflow(workflows.NativeAudioReviewWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.ResolveEpisodeAudioTiming, activity.RegisterOptions{Name: "ResolveEpisodeAudioTiming"})
	temporalWorker.RegisterActivityWithOptions(activities.AnalyzeEpisodeTiming, activity.RegisterOptions{Name: "AnalyzeEpisodeTiming"})
	temporalWorker.RegisterActivityWithOptions(activities.PrepareEpisodeTTS, activity.RegisterOptions{Name: "PrepareEpisodeTTS"})
	temporalWorker.RegisterActivityWithOptions(activities.GenerateTTSAudio, activity.RegisterOptions{Name: "GenerateTTSAudio"})
	temporalWorker.RegisterActivityWithOptions(activities.CreateTTSTimingRevision, activity.RegisterOptions{Name: "CreateTTSTimingRevision"})
	temporalWorker.RegisterActivityWithOptions(activities.ComposeEpisodeAudioMix, activity.RegisterOptions{Name: "ComposeEpisodeAudioMix"})
	temporalWorker.RegisterActivityWithOptions(activities.RefreshTimingCalibrationProfile, activity.RegisterOptions{Name: "RefreshTimingCalibrationProfile"})
	temporalWorker.RegisterActivityWithOptions(activities.CompleteEpisodeAudioProductionWorkflow, activity.RegisterOptions{Name: "CompleteEpisodeAudioProductionWorkflow"})
	temporalWorker.RegisterActivityWithOptions(activities.PrepareNativeAudioReview, activity.RegisterOptions{Name: "PrepareNativeAudioReview"})
	temporalWorker.RegisterActivityWithOptions(activities.ReviewNativeAudioSegment, activity.RegisterOptions{Name: "ReviewNativeAudioSegment"})
	temporalWorker.RegisterActivityWithOptions(activities.CompleteNativeAudioReviewWorkflow, activity.RegisterOptions{Name: "CompleteNativeAudioReviewWorkflow"})
	if err := workerkit.RunTemporalWorker(temporalWorker, worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
