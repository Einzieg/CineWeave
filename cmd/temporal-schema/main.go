package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultSchemaTimeout = 10 * time.Minute

var validDatabaseIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

type schemaConfig struct {
	Host               string
	Port               int
	User               string
	Password           string
	AdminDatabase      string
	Database           string
	VisibilityDatabase string
	SSLMode            string
	ToolPath           string
	SchemaRoot         string
	Timeout            time.Duration
	ReleaseID          string
}

type commandRunner interface {
	Run(context.Context, string, []string, []string) error
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, name string, args, environment []string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = environment
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s %s: %w", name, strings.Join(redactToolArgs(args), " "), err)
	}
	return nil
}

func main() {
	config, err := schemaConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	if err := applyTemporalSchema(ctx, config, osCommandRunner{}); err != nil {
		log.Fatal(err)
	}
	log.Printf("Temporal schema is current for release %s", config.ReleaseID)
}

func schemaConfigFromEnv() (schemaConfig, error) {
	port, err := strconv.Atoi(environment("TEMPORAL_DB_PORT", "5432"))
	if err != nil || port <= 0 || port > 65535 {
		return schemaConfig{}, fmt.Errorf("TEMPORAL_DB_PORT must be a valid TCP port")
	}
	timeout, err := time.ParseDuration(environment("TEMPORAL_SCHEMA_TIMEOUT", defaultSchemaTimeout.String()))
	if err != nil || timeout <= 0 {
		return schemaConfig{}, fmt.Errorf("TEMPORAL_SCHEMA_TIMEOUT must be a positive duration")
	}
	config := schemaConfig{
		Host:               environment("TEMPORAL_DB_HOST", "postgres"),
		Port:               port,
		User:               environment("TEMPORAL_DB_USER", "cineweave"),
		Password:           os.Getenv("TEMPORAL_DB_PASSWORD"),
		AdminDatabase:      environment("TEMPORAL_DB_ADMIN_DATABASE", "postgres"),
		Database:           environment("TEMPORAL_DB_NAME", "temporal"),
		VisibilityDatabase: environment("TEMPORAL_VISIBILITY_DB_NAME", "temporal_visibility"),
		SSLMode:            environment("TEMPORAL_DB_SSLMODE", "disable"),
		ToolPath:           environment("TEMPORAL_SQL_TOOL", "/usr/local/bin/temporal-sql-tool"),
		SchemaRoot:         environment("TEMPORAL_SCHEMA_ROOT", "/etc/temporal/schema/postgresql/v12"),
		Timeout:            timeout,
		ReleaseID:          environment("CINEWEAVE_RELEASE_ID", "local-dev"),
	}
	if strings.TrimSpace(config.Host) == "" || strings.TrimSpace(config.User) == "" {
		return schemaConfig{}, fmt.Errorf("Temporal database host and user are required")
	}
	for name, value := range map[string]string{
		"TEMPORAL_DB_ADMIN_DATABASE":  config.AdminDatabase,
		"TEMPORAL_DB_NAME":            config.Database,
		"TEMPORAL_VISIBILITY_DB_NAME": config.VisibilityDatabase,
	} {
		if !validDatabaseIdentifier.MatchString(value) {
			return schemaConfig{}, fmt.Errorf("%s must be a PostgreSQL identifier", name)
		}
	}
	if config.Database == config.VisibilityDatabase {
		return schemaConfig{}, fmt.Errorf("Temporal primary and visibility databases must be different")
	}
	return config, nil
}

func applyTemporalSchema(ctx context.Context, config schemaConfig, runner commandRunner) error {
	admin, err := pgx.Connect(ctx, postgresDSN(config, config.AdminDatabase))
	if err != nil {
		return fmt.Errorf("connect to Temporal database administrator: %w", err)
	}
	defer admin.Close(context.Background())

	targets := []struct {
		Database  string
		SchemaDir string
	}{
		{Database: config.Database, SchemaDir: filepath.Join(config.SchemaRoot, "temporal", "versioned")},
		{Database: config.VisibilityDatabase, SchemaDir: filepath.Join(config.SchemaRoot, "visibility", "versioned")},
	}
	for _, target := range targets {
		if err := ensureDatabase(ctx, admin, target.Database); err != nil {
			return err
		}
		initialized, err := schemaInitialized(ctx, config, target.Database)
		if err != nil {
			return err
		}
		if !initialized {
			if err := runTemporalSQLTool(ctx, config, runner, target.Database, "setup-schema", "-v", "0.0"); err != nil {
				return fmt.Errorf("initialize Temporal schema in %s: %w", target.Database, err)
			}
		}
		if err := runTemporalSQLTool(ctx, config, runner, target.Database, "update-schema", "-d", target.SchemaDir); err != nil {
			return fmt.Errorf("update Temporal schema in %s: %w", target.Database, err)
		}
	}
	return nil
}

func ensureDatabase(ctx context.Context, admin *pgx.Conn, database string) error {
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, database).Scan(&exists); err != nil {
		return fmt.Errorf("check Temporal database %s: %w", database, err)
	}
	if exists {
		return nil
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		return fmt.Errorf("create Temporal database %s: %w", database, err)
	}
	return nil
}

func schemaInitialized(ctx context.Context, config schemaConfig, database string) (bool, error) {
	connection, err := pgx.Connect(ctx, postgresDSN(config, database))
	if err != nil {
		return false, fmt.Errorf("connect to Temporal database %s: %w", database, err)
	}
	defer connection.Close(context.Background())
	var initialized bool
	if err := connection.QueryRow(ctx, `SELECT to_regclass('public.schema_version') IS NOT NULL`).Scan(&initialized); err != nil {
		return false, fmt.Errorf("check Temporal schema in %s: %w", database, err)
	}
	return initialized, nil
}

func runTemporalSQLTool(ctx context.Context, config schemaConfig, runner commandRunner, database string, commandAndArgs ...string) error {
	args := []string{
		"--plugin", "postgres12",
		"--ep", config.Host,
		"--port", strconv.Itoa(config.Port),
		"--user", config.User,
		"--database", database,
	}
	args = append(args, commandAndArgs...)
	environment := append(os.Environ(), "SQL_PASSWORD="+config.Password)
	return runner.Run(ctx, config.ToolPath, args, environment)
}

func postgresDSN(config schemaConfig, database string) string {
	user := url.User(config.User)
	if config.Password != "" {
		user = url.UserPassword(config.User, config.Password)
	}
	connection := url.URL{
		Scheme:   "postgres",
		User:     user,
		Host:     net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		Path:     database,
		RawQuery: url.Values{"sslmode": []string{config.SSLMode}}.Encode(),
	}
	return connection.String()
}

func redactToolArgs(args []string) []string {
	copyOfArgs := append([]string(nil), args...)
	for index := range copyOfArgs {
		if strings.EqualFold(copyOfArgs[index], "--password") && index+1 < len(copyOfArgs) {
			copyOfArgs[index+1] = "[redacted]"
		}
	}
	return copyOfArgs
}

func environment(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
