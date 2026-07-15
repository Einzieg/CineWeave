package dbmigrate_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/dbmigrate"
	"github.com/Einzieg/cineweave/internal/dbseed"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestEmptyDatabaseUpDownUpProducesStableSchema(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run migration roundtrip tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for migration roundtrip tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	baseConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	databaseName := "cineweave_roundtrip_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminConfig := baseConfig.Copy()
	adminConfig.Database = "postgres"
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	defer admin.Close(context.Background())
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
		_, _ = admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+identifier)
	})

	testConfig := baseConfig.Copy()
	testConfig.Database = databaseName
	testURL := testConfig.ConnString()
	runner, err := dbmigrate.Open(ctx, dbmigrate.Config{
		DatabaseURL: testURL,
		Environment: "test",
		ReleaseID:   "migration-roundtrip-test",
	})
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	defer runner.Close()

	if err := runner.Run(ctx, "up", 0); err != nil {
		t.Fatalf("first migrate up: %v", err)
	}
	applySeed(t, ctx, testURL)
	first := normalizedSchemaSnapshot(t, ctx, testConfig)

	if err := runner.Run(ctx, "reset", 0); err != nil {
		t.Fatalf("migrate down to zero: %v", err)
	}
	if err := runner.Run(ctx, "up", 0); err != nil {
		t.Fatalf("second migrate up: %v", err)
	}
	applySeed(t, ctx, testURL)
	second := normalizedSchemaSnapshot(t, ctx, testConfig)
	if first != second {
		t.Fatalf("normalized schema changed after up/down/up\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func applySeed(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()
	runner, err := dbseed.Open(ctx, dbseed.Config{
		DatabaseURL: databaseURL,
		Environment: "test",
		ReleaseID:   "migration-roundtrip-test",
	})
	if err != nil {
		t.Fatalf("open seed runner: %v", err)
	}
	defer runner.Close()
	if err := runner.Apply(ctx); err != nil {
		t.Fatalf("apply seed: %v", err)
	}
	if err := runner.Verify(ctx); err != nil {
		t.Fatalf("verify seed: %v", err)
	}
}

func normalizedSchemaSnapshot(t *testing.T, ctx context.Context, config *pgx.ConnConfig) string {
	t.Helper()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect snapshot database: %v", err)
	}
	defer conn.Close(context.Background())
	queries := []string{
		`SELECT format('table|%I', c.relname)
		 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') ORDER BY c.relname`,
		`SELECT format('column|%I|%s|%I|%s|%s|%s', c.relname, a.attnum, a.attname,
		                     pg_catalog.format_type(a.atttypid, a.atttypmod), a.attnotnull,
		                     COALESCE(pg_get_expr(d.adbin, d.adrelid), ''))
		 FROM pg_attribute a
		 JOIN pg_class c ON c.oid = a.attrelid
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped
		 ORDER BY c.relname, a.attnum`,
		`SELECT format('constraint|%I|%I|%s', c.relname, con.conname, pg_get_constraintdef(con.oid, true))
		 FROM pg_constraint con
		 JOIN pg_class c ON c.oid = con.conrelid
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' ORDER BY c.relname, con.conname`,
		`SELECT format('index|%I|%I|%s', tablename, indexname, indexdef)
		 FROM pg_indexes WHERE schemaname = 'public' ORDER BY tablename, indexname`,
		`SELECT format('function|%I|%s', p.proname, pg_get_functiondef(p.oid))
		 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' ORDER BY p.proname, p.oid`,
		`SELECT format('trigger|%I|%I|%s', c.relname, t.tgname, pg_get_triggerdef(t.oid, true))
		 FROM pg_trigger t
		 JOIN pg_class c ON c.oid = t.tgrelid
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND NOT t.tgisinternal ORDER BY c.relname, t.tgname`,
	}
	lines := make([]string, 0, 4096)
	for _, query := range queries {
		rows, err := conn.Query(ctx, query)
		if err != nil {
			t.Fatalf("query schema snapshot: %v", err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatalf("scan schema snapshot: %v", err)
			}
			lines = append(lines, line)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate schema snapshot: %v", err)
		}
		rows.Close()
	}
	sort.Strings(lines)
	return fmt.Sprintf("%s\n", strings.Join(lines, "\n"))
}
