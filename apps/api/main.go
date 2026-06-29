package main

import (
	"context"
	"log"
	"time"

	"github.com/Einzieg/cineweave/internal/api"
	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/service"
	"github.com/Einzieg/cineweave/internal/storage"
	"go.temporal.io/sdk/client"
)

func main() {
	cfg := config.ServerFromEnv("api", "CINEWEAVE_API_ADDR", ":8080")
	logger := observability.Logger(cfg.Name, cfg.Env)
	ctx := context.Background()

	pool, err := db.Open(ctx, config.Get("DATABASE_URL", "postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	authService := auth.NewService(
		pool,
		config.Get("CINEWEAVE_JWT_SECRET", "dev-insecure-cineweave-secret"),
		config.Duration("CINEWEAVE_ACCESS_TOKEN_TTL", 2*time.Hour),
		config.Duration("CINEWEAVE_REFRESH_TOKEN_TTL", 30*24*time.Hour),
	)
	credentialVault, err := provider.NewVaultFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	providerService := provider.NewService(pool, credentialVault)
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
	server := api.New(pool, authService, providerService, storageClient, temporalClient)

	if err := service.Serve(ctx, cfg, server.Handler(), logger); err != nil {
		log.Fatal(err)
	}
}
