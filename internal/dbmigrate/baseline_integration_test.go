package dbmigrate_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/dbmigrate"
	"github.com/Einzieg/cineweave/internal/migrationbundle"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestConsolidatedBaselineMatchesMigrationChain(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run migration baseline tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for migration baseline tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	baseConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	adminConfig := baseConfig.Copy()
	adminConfig.Database = "postgres"
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	migratedName := "cineweave_chain_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	baselineName := "cineweave_baseline_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, databaseName := range []string{migratedName, baselineName} {
		identifier := pgx.Identifier{databaseName}.Sanitize()
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
			t.Fatalf("create database %s: %v", databaseName, err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cleanupCancel()
			_, _ = admin.Exec(cleanupCtx, `
				SELECT pg_terminate_backend(pid)
				FROM pg_stat_activity
				WHERE datname = $1 AND pid <> pg_backend_pid()
			`, databaseName)
			if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+identifier); err != nil {
				t.Errorf("drop database %s: %v", databaseName, err)
			}
		})
	}

	migratedURL, err := databaseURLForTestDatabase(databaseURL, migratedName)
	if err != nil {
		t.Fatalf("build migrated database URL: %v", err)
	}
	runner, err := dbmigrate.Open(ctx, dbmigrate.Config{
		DatabaseURL: migratedURL,
		Environment: "test",
		ReleaseID:   "baseline-equivalence-test",
	})
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	defer runner.Close()
	if err := runner.Run(ctx, "up", 0); err != nil {
		t.Fatalf("apply migration chain: %v", err)
	}

	baselineConfig := baseConfig.Copy()
	baselineConfig.Database = baselineName
	baseline, err := pgx.ConnectConfig(ctx, baselineConfig)
	if err != nil {
		t.Fatalf("connect baseline database: %v", err)
	}
	defer baseline.Close(context.Background())
	bundle, _, err := migrationbundle.Build()
	if err != nil {
		t.Fatalf("build consolidated baseline: %v", err)
	}
	if _, err := baseline.Exec(ctx, string(bundle)); err != nil {
		t.Fatalf("apply consolidated baseline: %v", err)
	}

	migratedConfig := baseConfig.Copy()
	migratedConfig.Database = migratedName
	migratedSchema := normalizedSchemaSnapshot(t, ctx, migratedConfig)
	baselineSchema := normalizedSchemaSnapshot(t, ctx, baselineConfig)
	if migratedSchema != baselineSchema {
		t.Fatalf("consolidated baseline differs from migration chain\nmigrated bytes=%d baseline bytes=%d", len(migratedSchema), len(baselineSchema))
	}
}
