package main

import (
	"context"
	"strings"
	"testing"
)

type recordingRunner struct {
	name string
	args []string
	env  []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args, environment []string) error {
	r.name = name
	r.args = append([]string(nil), args...)
	r.env = append([]string(nil), environment...)
	return nil
}

func TestSchemaConfigRejectsUnsafeDatabaseIdentifier(t *testing.T) {
	t.Setenv("TEMPORAL_DB_NAME", "temporal;DROP DATABASE cineweave")
	if _, err := schemaConfigFromEnv(); err == nil {
		t.Fatal("unsafe database identifier should fail")
	}
}

func TestPostgresDSNEscapesCredentials(t *testing.T) {
	dsn := postgresDSN(schemaConfig{
		Host: "db.internal", Port: 5432, User: "temporal user", Password: "p@ss:/word", SSLMode: "require",
	}, "temporal")
	if !strings.Contains(dsn, "temporal%20user:p%40ss%3A%2Fword@db.internal:5432/temporal") || !strings.Contains(dsn, "sslmode=require") {
		t.Fatalf("dsn was not escaped: %s", dsn)
	}
}

func TestRunTemporalSQLToolUsesEnvironmentPassword(t *testing.T) {
	runner := &recordingRunner{}
	config := schemaConfig{
		Host: "postgres", Port: 5432, User: "cineweave", Password: "secret", ToolPath: "temporal-sql-tool",
	}
	if err := runTemporalSQLTool(context.Background(), config, runner, "temporal", "update-schema", "-d", "/schema"); err != nil {
		t.Fatalf("run tool: %v", err)
	}
	joined := strings.Join(runner.args, " ")
	if strings.Contains(joined, "secret") {
		t.Fatalf("password leaked into args: %s", joined)
	}
	if !strings.Contains(strings.Join(runner.env, "\n"), "SQL_PASSWORD=secret") {
		t.Fatal("SQL_PASSWORD was not set")
	}
	if joined != "--plugin postgres12 --ep postgres --port 5432 --user cineweave --database temporal update-schema -d /schema" {
		t.Fatalf("unexpected args: %s", joined)
	}
}
