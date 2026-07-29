package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/providergatewayserver"
	"github.com/Einzieg/cineweave/internal/service"
	"github.com/Einzieg/cineweave/internal/storage"
)

func main() {
	cfg := config.ServerFromEnv(
		"provider-gateway",
		"CINEWEAVE_PROVIDER_GATEWAY_ADDR",
		":8082",
	)
	logger := observability.Logger(cfg.Name, cfg.Env)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Open(
		ctx,
		config.Get(
			"DATABASE_URL",
			"postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable",
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	credentialVault, err := provider.NewVaultFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	providerService := provider.NewService(pool, credentialVault)
	providerService.EnableGatewayRuntime()
	storageClient, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	providerService.SetStorage(storageClient)
	go providergatewayserver.RunProviderRequestReconciler(
		ctx,
		providerService,
		logger,
		providerReconcileDuration(
			"CINEWEAVE_PROVIDER_REQUEST_RECONCILE_INTERVAL",
			time.Minute,
		),
		providerReconcileDuration(
			"CINEWEAVE_PROVIDER_REQUEST_STALE_AFTER",
			30*time.Minute,
		),
	)
	serviceToken := config.Get(
		"CINEWEAVE_SERVICE_TOKEN",
		config.DefaultServiceToken,
	)
	if err := config.ValidateProductionSecret(
		cfg.Env,
		"CINEWEAVE_SERVICE_TOKEN",
		serviceToken,
		config.DefaultServiceToken,
	); err != nil {
		log.Fatal(err)
	}
	handler, err := providergatewayserver.NewHandler(
		providergatewayserver.Options{
			Providers:    providerService,
			ServiceToken: serviceToken,
			ReadinessChecks: map[string]httpx.ReadinessCheck{
				"database": func(checkCtx context.Context) error {
					pingCtx, cancel := context.WithTimeout(
						checkCtx,
						2*time.Second,
					)
					defer cancel()
					return pool.Ping(pingCtx)
				},
				"storage": func(checkCtx context.Context) error {
					pingCtx, cancel := context.WithTimeout(
						checkCtx,
						2*time.Second,
					)
					defer cancel()
					return storageClient.Ping(pingCtx)
				},
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := service.Serve(ctx, cfg, handler, logger); err != nil {
		log.Fatal(err)
	}
}

func providerReconcileDuration(
	key string,
	fallback time.Duration,
) time.Duration {
	value := strings.TrimSpace(config.Get(key, ""))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
