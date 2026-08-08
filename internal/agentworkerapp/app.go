package agentworkerapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Einzieg/cineweave/internal/api"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/Einzieg/cineweave/workers/workerkit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

type RuntimeDependencies struct {
	DB         *pgxpool.Pool
	Auth       *auth.Service
	Authorizer *authz.Authorizer
}

type EditionRuntimeFactory func(context.Context, RuntimeDependencies) (*edition.Runtime, error)

func Run(ctx context.Context, runtimeFactory EditionRuntimeFactory) error {
	if runtimeFactory == nil {
		return fmt.Errorf("agent worker edition runtime factory is required")
	}
	logger := observability.Logger("agent-worker", config.Get("CINEWEAVE_ENV", "development"))
	ctx = observability.WithLogger(ctx, logger)
	pool, err := db.Open(ctx, config.Get(
		"DATABASE_URL",
		"postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable",
	))
	if err != nil {
		return err
	}
	defer pool.Close()

	storageClient, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		return err
	}
	temporalClient, err := client.Dial(client.Options{
		HostPort:  config.Get("TEMPORAL_ADDRESS", "localhost:7233"),
		Namespace: config.Get("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		return err
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
	authorizer := authz.New(pool)
	editionRuntime, err := runtimeFactory(ctx, RuntimeDependencies{
		DB: pool, Auth: authService, Authorizer: authorizer,
	})
	if err != nil {
		return err
	}
	apiServer := api.New(pool, authService, providerService, storageClient, temporalClient, authorizer)
	if err := apiServer.SetEditionRuntime(editionRuntime); err != nil {
		return err
	}
	activities := api.NewProjectAgentActivities(apiServer)

	workerOptions, err := workerkit.TemporalWorkerOptions("agent-worker")
	if err != nil {
		return err
	}
	temporalWorker := worker.New(temporalClient, workflows.AgentTaskQueue, workerOptions)
	temporalWorker.RegisterWorkflow(workflows.ProjectAgentWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.PlanTask, activity.RegisterOptions{Name: "ProjectAgentPlanTask"})
	temporalWorker.RegisterActivityWithOptions(activities.ExecuteReadySteps, activity.RegisterOptions{Name: "ProjectAgentExecuteReadySteps"})
	temporalWorker.RegisterActivityWithOptions(activities.ApproveStep, activity.RegisterOptions{Name: "ProjectAgentApproveStep"})
	temporalWorker.RegisterActivityWithOptions(activities.RejectStep, activity.RegisterOptions{Name: "ProjectAgentRejectStep"})
	temporalWorker.RegisterActivityWithOptions(activities.CancelTask, activity.RegisterOptions{Name: "ProjectAgentCancelTask"})
	temporalWorker.RegisterActivityWithOptions(activities.ModifyConstraints, activity.RegisterOptions{Name: "ProjectAgentModifyConstraints"})

	projectControlRegistry := apiServer.ProjectControlRuntimeRegistry()
	if projectControlRegistry == nil {
		return fmt.Errorf("project control runtime registry is unavailable")
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	releaseID := config.Get("CINEWEAVE_RELEASE_ID", config.Get("CINEWEAVE_BUILD_ID", "local-dev"))
	projectControlRepository := projectcontrol.NewRepository(pool)
	dispatcher := &projectcontrol.Dispatcher{
		Repository: projectControlRepository, Registry: projectControlRegistry,
		Owner: "agent-worker/" + hostname + "/" + uuid.NewString(), ReleaseID: releaseID,
		LeaseDuration: 30 * time.Second, MaximumAttempts: projectcontrol.DefaultMaximumDispatchAttempts,
		ReconcileDelay: 3 * time.Second,
	}
	reconciler := &projectcontrol.Reconciler{
		Repository: projectControlRepository, Tracker: projectcontrol.NewDBWorkflowTracker(pool),
		Canceller: projectcontrol.NewTemporalWorkflowCanceller(temporalClient),
		Owner:     "agent-worker/" + hostname + "/" + uuid.NewString(), ReleaseID: releaseID,
		LeaseDuration: 30 * time.Second, ReconcileDelay: 3 * time.Second,
	}
	go runProjectControlRuntime(ctx, dispatcher, reconciler)
	go runProjectControlMetrics(ctx, projectControlRepository)
	go runProjectControlContentGC(ctx, apiServer)

	return workerkit.RunTemporalWorker(temporalWorker, worker.InterruptCh())
}

func runProjectControlContentGC(ctx context.Context, apiServer *api.Server) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if cleaned, err := apiServer.CleanupExpiredProjectControlContentUploads(ctx, 20); err != nil {
			observability.Log(ctx, slog.LevelError, "project control content cleanup failed",
				"component", "project_control", "operation", "content_gc", "error", err)
		} else if cleaned > 0 {
			observability.Log(ctx, slog.LevelInfo, "project control content cleanup completed",
				"component", "project_control", "operation", "content_gc", "uploads", cleaned)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runProjectControlRuntime(
	ctx context.Context,
	dispatcher *projectcontrol.Dispatcher,
	reconciler *projectcontrol.Reconciler,
) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		processed := false
		if ok, err := dispatcher.RunOnce(ctx); err != nil {
			observability.Log(ctx, slog.LevelError, "project control dispatcher cycle failed",
				"component", "project_control", "operation", "dispatch", "releaseId", dispatcher.ReleaseID, "error", err)
		} else {
			processed = processed || ok
		}
		if ok, err := reconciler.RunOnce(ctx); err != nil {
			observability.Log(ctx, slog.LevelError, "project control reconciler cycle failed",
				"component", "project_control", "operation", "reconcile", "releaseId", reconciler.ReleaseID, "error", err)
		} else {
			processed = processed || ok
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runProjectControlMetrics(ctx context.Context, repository *projectcontrol.Repository) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		snapshot, err := repository.RuntimeSnapshot(ctx)
		if err != nil {
			observability.Log(ctx, slog.LevelError, "project control runtime snapshot failed",
				"component", "project_control", "operation", "runtime_snapshot", "error", err)
		} else {
			counts := make([]observability.ProjectControlCommandCount, 0, len(snapshot.CommandCounts))
			for _, count := range snapshot.CommandCounts {
				counts = append(counts, observability.ProjectControlCommandCount{
					Status: count.Status, Controller: count.Controller, Count: count.Count,
				})
			}
			observability.SetProjectControlRuntime(observability.ProjectControlRuntimeSnapshot{
				CommandCounts:                  counts,
				ExpiredLeases:                  snapshot.ExpiredLeases,
				OverdueReconciliations:         snapshot.OverdueReconciliations,
				OldestReconcileLagSeconds:      snapshot.OldestReconcileLagSeconds,
				UnlinkedDeterministicWorkflows: snapshot.UnlinkedDeterministicWorkflows,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
