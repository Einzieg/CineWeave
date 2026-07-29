package editionmigration_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Einzieg/cineweave/internal/editionmigration"
	"github.com/Einzieg/cineweave/internal/migrationstream"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestCoreAndCommercialStreamsUseIndependentLedgersUpDownUp(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run migration stream integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for migration stream integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	databaseName := "cineweave_streams_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatalf("create temporary database: %v", err)
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
			t.Errorf("drop temporary database: %v", err)
		}
	})

	testURL, err := databaseURLForDatabase(databaseURL, databaseName)
	if err != nil {
		t.Fatalf("build temporary database URL: %v", err)
	}
	coreFiles := fstest.MapFS{
		"000001_core_fixture.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE public.core_stream_fixture (id bigint PRIMARY KEY);
-- +goose Down
DROP TABLE public.core_stream_fixture;
`)},
	}
	commercialFiles := fstest.MapFS{
		"000001_commercial_fixture.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE SCHEMA IF NOT EXISTS cineweave_commercial;
CREATE TABLE cineweave_commercial.stream_fixture (
    id bigint PRIMARY KEY,
    core_id bigint REFERENCES public.core_stream_fixture(id) ON DELETE SET NULL
);
-- +goose Down
DROP TABLE cineweave_commercial.stream_fixture;
DROP SCHEMA cineweave_commercial;
`)},
	}

	core, err := migrationstream.Open(ctx, migrationstream.Config{
		DatabaseURL: testURL,
		Environment: "test",
		ReleaseID:   "migration-stream-integration",
	}, editionmigration.CoreDefinition(coreFiles, nil, nil))
	if err != nil {
		t.Fatalf("open Core fixture stream: %v", err)
	}
	defer core.Close()
	commercial, err := migrationstream.Open(ctx, migrationstream.Config{
		DatabaseURL: testURL,
		Environment: "test",
		ReleaseID:   "migration-stream-integration",
	}, editionmigration.CommercialDefinition(commercialFiles))
	if err != nil {
		t.Fatalf("open Commercial fixture stream: %v", err)
	}
	defer commercial.Close()

	if err := core.Run(ctx, "up", 0); err != nil {
		t.Fatalf("Core up: %v", err)
	}
	if err := commercial.Run(ctx, "up", 0); err != nil {
		t.Fatalf("Commercial first up: %v", err)
	}

	testConfig := baseConfig.Copy()
	testConfig.Database = databaseName
	conn, err := pgx.ConnectConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("connect fixture database: %v", err)
	}
	defer conn.Close(context.Background())
	assertStreamVersion(t, ctx, conn, "cineweave_migrations.cineweave_schema_versions", 1)
	assertStreamVersion(t, ctx, conn, "cineweave_commercial_migrations.schema_versions", 1)

	if err := commercial.Run(ctx, "reset", 0); err != nil {
		t.Fatalf("Commercial reset: %v", err)
	}
	assertStreamVersion(t, ctx, conn, "cineweave_migrations.cineweave_schema_versions", 1)
	assertStreamVersion(t, ctx, conn, "cineweave_commercial_migrations.schema_versions", 0)
	assertRelationPresence(t, ctx, conn, "public.core_stream_fixture", true)
	assertRelationPresence(t, ctx, conn, "cineweave_commercial.stream_fixture", false)

	if err := commercial.Run(ctx, "up", 0); err != nil {
		t.Fatalf("Commercial second up: %v", err)
	}
	assertStreamVersion(t, ctx, conn, "cineweave_migrations.cineweave_schema_versions", 1)
	assertStreamVersion(t, ctx, conn, "cineweave_commercial_migrations.schema_versions", 1)
	assertRelationPresence(t, ctx, conn, "public.core_stream_fixture", true)
	assertRelationPresence(t, ctx, conn, "cineweave_commercial.stream_fixture", true)
}

func assertStreamVersion(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	ledger string,
	expected int64,
) {
	t.Helper()
	var version int64
	query := fmt.Sprintf(`SELECT COALESCE(max(version_id) FILTER (WHERE is_applied), 0) FROM %s`, ledger)
	if err := conn.QueryRow(ctx, query).Scan(&version); err != nil {
		t.Fatalf("read %s: %v", ledger, err)
	}
	if version != expected {
		t.Fatalf("%s version = %d, want %d", ledger, version, expected)
	}
}

func assertRelationPresence(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	relation string,
	expected bool,
) {
	t.Helper()
	var present bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, relation).Scan(&present); err != nil {
		t.Fatalf("inspect relation %s: %v", relation, err)
	}
	if present != expected {
		t.Fatalf("relation %s presence = %t, want %t", relation, present, expected)
	}
}

func databaseURLForDatabase(base, database string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + database
	return parsed.String(), nil
}
