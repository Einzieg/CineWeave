package main

import (
	"context"
	"log"
	"time"

	"github.com/Einzieg/cineweave/internal/api"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
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

	providerService := provider.NewService(pool, nil)
	providerService.SetGateway(
		config.Get("PROVIDER_GATEWAY_URL", "http://localhost:8082"),
		config.Get("CINEWEAVE_SERVICE_TOKEN", config.DefaultServiceToken),
	)
	authService := auth.NewService(
		pool,
		config.Get("CINEWEAVE_JWT_SECRET", config.DefaultJWTSecret),
		config.Duration("CINEWEAVE_ACCESS_TOKEN_TTL", 2*time.Hour),
		config.Duration("CINEWEAVE_REFRESH_TOKEN_TTL", 30*24*time.Hour),
	)
	apiServer := api.New(pool, authService, providerService, storageClient, temporalClient, authz.New(pool))
	activities := api.NewProjectAgentActivities(apiServer)

	workerOptions, err := workerkit.TemporalWorkerOptions("agent-worker")
	if err != nil {
		log.Fatal(err)
	}
	temporalWorker := worker.New(temporalClient, workflows.AgentTaskQueue, workerOptions)
	temporalWorker.RegisterWorkflow(workflows.ProjectAgentWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.PlanTask, activity.RegisterOptions{Name: "ProjectAgentPlanTask"})
	temporalWorker.RegisterActivityWithOptions(activities.ExecuteReadySteps, activity.RegisterOptions{Name: "ProjectAgentExecuteReadySteps"})
	temporalWorker.RegisterActivityWithOptions(activities.ApproveStep, activity.RegisterOptions{Name: "ProjectAgentApproveStep"})
	temporalWorker.RegisterActivityWithOptions(activities.RejectStep, activity.RegisterOptions{Name: "ProjectAgentRejectStep"})
	temporalWorker.RegisterActivityWithOptions(activities.CancelTask, activity.RegisterOptions{Name: "ProjectAgentCancelTask"})
	temporalWorker.RegisterActivityWithOptions(activities.ModifyConstraints, activity.RegisterOptions{Name: "ProjectAgentModifyConstraints"})

	if err := workerkit.RunTemporalWorker(temporalWorker, worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
