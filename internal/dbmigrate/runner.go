package dbmigrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

const (
	gooseTable                        = "cineweave_migrations.cineweave_schema_versions"
	migrationDir                      = "."
	migrationLockKey                  = int64(0x43494e4557454156)
	providerProtectionBaselineVersion = int64(8)
	providerModelHardDeleteVersion    = int64(36)
	providerModelRollbackVersion      = int64(37)
)

var ErrProviderModelRollbackUnsafe = errors.New("provider model deletion history makes this rollback unsafe")

var (
	migrationNamePattern                  = regexp.MustCompile(`^(\d+)_[a-z0-9_]+\.sql$`)
	migrationExecutableDollarQuotePattern = regexp.MustCompile(
		`(?s)(?:\bdo(?:\s+language\s+[a-z_][a-z0-9_$]*)?|\bas|\bexecute)\s*$`,
	)
)

var protectedProviderConfigurationTables = []string{
	"provider_accounts",
	"provider_connectors",
	"provider_credentials",
	"provider_endpoints",
	"provider_models",
	"provider_model_capabilities",
	"provider_limit_policies",
	"model_profiles",
	"model_profile_bindings",
}

type Config struct {
	DatabaseURL string
	Environment string
	ReleaseID   string
}

type Runner struct {
	db          *sql.DB
	environment string
	releaseID   string
	migrations  []migrationFile
}

type migrationFile struct {
	Version int64
	Name    string
	Hash    string
}

type ProviderModelRollbackPreflightError struct {
	CurrentVersion        int64
	TargetVersion         int64
	TombstoneCount        int64
	ConflictingModelCount int64
	NullRenderPlanCount   int64
}

func (e *ProviderModelRollbackPreflightError) Error() string {
	return fmt.Sprintf(
		"%v: migration range %d -> %d has tombstones=%d, conflictingModelKeys=%d, nullRenderPlans=%d; rebuild the development database or use a forward migration",
		ErrProviderModelRollbackUnsafe,
		e.CurrentVersion,
		e.TargetVersion,
		e.TombstoneCount,
		e.ConflictingModelCount,
		e.NullRenderPlanCount,
	)
}

func (e *ProviderModelRollbackPreflightError) Unwrap() error {
	return ErrProviderModelRollbackUnsafe
}

func ConfigFromEnv() (Config, error) {
	environment := firstNonEmpty(os.Getenv("CINEWEAVE_ENV"), os.Getenv("APP_ENV"), "development")
	releaseID := strings.TrimSpace(os.Getenv("CINEWEAVE_RELEASE_ID"))
	if releaseID == "" {
		if IsProduction(environment) {
			return Config{}, errors.New("CINEWEAVE_RELEASE_ID is required in production")
		}
		releaseID = "local-dev"
	}
	return Config{
		DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
		Environment: environment,
		ReleaseID:   releaseID,
	}, nil
}

func Open(ctx context.Context, cfg Config) (*Runner, error) {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	migrations, err := loadMigrationFiles()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open migration database: %w", err)
	}
	// A session advisory lock is only correct when Goose cannot borrow a
	// different connection while a migration is running.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect migration database: %w", err)
	}
	configureGoose()
	return &Runner{
		db:          db,
		environment: firstNonEmpty(cfg.Environment, "development"),
		releaseID:   firstNonEmpty(cfg.ReleaseID, "local-dev"),
		migrations:  migrations,
	}, nil
}

func (r *Runner) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func ValidateEmbedded() error {
	configureGoose()
	_, err := loadMigrationFiles()
	if err != nil {
		return err
	}
	if _, err := goose.CollectMigrations(migrationDir, 0, math.MaxInt64); err != nil {
		return fmt.Errorf("collect embedded migrations: %w", err)
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, command string, target int64) error {
	command = strings.ToLower(strings.TrimSpace(command))
	if err := validateMigrationCommandPolicy(r.environment, command); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()

	if err := r.ensureControlSchema(ctx); err != nil {
		return err
	}
	current, err := goose.GetDBVersionContext(ctx, r.db)
	if err != nil {
		return fmt.Errorf("initialize Goose ledger: %w", err)
	}
	if err := r.validateAppliedHashes(ctx); err != nil {
		return err
	}

	switch command {
	case "up":
		return r.up(ctx)
	case "verify":
		return nil
	case "status":
		return goose.StatusContext(ctx, r.db, migrationDir)
	case "version":
		return goose.VersionContext(ctx, r.db, migrationDir)
	case "down":
		if err := r.preflightProviderModelRollback(ctx, current, max(current-1, 0)); err != nil {
			return err
		}
		return r.downOne(ctx)
	case "down-to":
		if target < 0 {
			return errors.New("down-to target must be zero or greater")
		}
		if err := r.preflightProviderModelRollback(ctx, current, target); err != nil {
			return err
		}
		return r.downTo(ctx, target)
	case "reset":
		if err := r.preflightProviderModelRollback(ctx, current, 0); err != nil {
			return err
		}
		return r.downTo(ctx, 0)
	default:
		return fmt.Errorf("unsupported migration command %q", command)
	}
}

func validateMigrationCommandPolicy(environment, command string) error {
	if isDestructive(command) && IsProduction(environment) {
		return fmt.Errorf("migration command %q is disabled in production; use a forward repair migration", command)
	}
	return nil
}

func (r *Runner) preflightProviderModelRollback(ctx context.Context, current, target int64) error {
	checkDeletionHistory, checkNullRenderPlans := providerModelRollbackPreflightScope(current, target)
	if !checkDeletionHistory && !checkNullRenderPlans {
		return nil
	}

	var tombstones int64
	var conflicts int64
	var nullRenderPlans int64
	if checkDeletionHistory {
		if err := r.db.QueryRowContext(ctx, `
SELECT
    (SELECT count(*) FROM provider_model_deletion_tombstones),
    (
        SELECT count(*)
        FROM provider_model_deletion_tombstones tombstone
        JOIN provider_models current_model
          ON current_model.provider_account_id = tombstone.provider_account_id
         AND current_model.model_key = tombstone.model_key
         AND current_model.id <> tombstone.provider_model_id
    ),
    (SELECT count(*) FROM video_render_plans WHERE provider_model_id IS NULL)
`).Scan(&tombstones, &conflicts, &nullRenderPlans); err != nil {
			return fmt.Errorf("inspect provider model rollback safety for migration range %d -> %d: %w", current, target, err)
		}
	} else if err := r.db.QueryRowContext(ctx, `
SELECT count(*) FROM video_render_plans WHERE provider_model_id IS NULL
`).Scan(&nullRenderPlans); err != nil {
		return fmt.Errorf("inspect nullable Render Plan provider models for migration range %d -> %d: %w", current, target, err)
	}

	if tombstones == 0 && conflicts == 0 && nullRenderPlans == 0 {
		slog.InfoContext(ctx, "provider model rollback preflight passed",
			"currentVersion", current,
			"targetVersion", target,
			"releaseId", r.releaseID,
		)
		return nil
	}

	preflightErr := &ProviderModelRollbackPreflightError{
		CurrentVersion:        current,
		TargetVersion:         target,
		TombstoneCount:        tombstones,
		ConflictingModelCount: conflicts,
		NullRenderPlanCount:   nullRenderPlans,
	}
	slog.ErrorContext(ctx, "provider model rollback preflight rejected",
		"currentVersion", current,
		"targetVersion", target,
		"tombstoneCount", tombstones,
		"conflictingModelCount", conflicts,
		"nullRenderPlanCount", nullRenderPlans,
		"releaseId", r.releaseID,
	)
	return preflightErr
}

func providerModelRollbackPreflightScope(current, target int64) (checkDeletionHistory, checkNullRenderPlans bool) {
	if current <= target {
		return false, false
	}
	checkDeletionHistory = current >= providerModelRollbackVersion && target < providerModelRollbackVersion
	checkNullRenderPlans = current >= providerModelHardDeleteVersion && target < providerModelHardDeleteVersion
	if checkDeletionHistory {
		checkNullRenderPlans = true
	}
	return checkDeletionHistory, checkNullRenderPlans
}

func (r *Runner) up(ctx context.Context) error {
	current, err := goose.GetDBVersionContext(ctx, r.db)
	if err != nil {
		return err
	}
	for _, migration := range r.migrations {
		if migration.Version <= current {
			continue
		}
		if err := r.runAudited(ctx, migration, "up", func() error {
			return goose.UpToContext(ctx, r.db, migrationDir, migration.Version)
		}); err != nil {
			return err
		}
		current = migration.Version
	}
	return nil
}

func (r *Runner) downOne(ctx context.Context) error {
	current, err := goose.GetDBVersionContext(ctx, r.db)
	if err != nil {
		return err
	}
	if current == 0 {
		return nil
	}
	migration, ok := r.migrationByVersion(current)
	if !ok {
		return fmt.Errorf("applied migration version %d is not embedded", current)
	}
	return r.runAudited(ctx, migration, "down", func() error {
		return goose.DownContext(ctx, r.db, migrationDir)
	})
}

func (r *Runner) downTo(ctx context.Context, target int64) error {
	for {
		current, err := goose.GetDBVersionContext(ctx, r.db)
		if err != nil {
			return err
		}
		if current <= target {
			return nil
		}
		if err := r.downOne(ctx); err != nil {
			return err
		}
	}
}

func (r *Runner) runAudited(ctx context.Context, migration migrationFile, direction string, run func() error) error {
	auditID, err := r.startAudit(ctx, migration, direction)
	if err != nil {
		return err
	}
	started := time.Now()
	runErr := run()
	duration := time.Since(started)
	if finishErr := r.finishAudit(ctx, auditID, duration, runErr); finishErr != nil {
		slog.ErrorContext(ctx, "database migration audit failed",
			"version", migration.Version,
			"direction", direction,
			"contentHash", migration.Hash,
			"durationMs", duration.Milliseconds(),
			"releaseId", r.releaseID,
			"failureStatementSummary", truncate(errors.Join(runErr, finishErr).Error(), 2048),
		)
		if runErr != nil {
			return errors.Join(runErr, finishErr)
		}
		return finishErr
	}
	if runErr != nil {
		slog.ErrorContext(ctx, "database migration failed",
			"version", migration.Version,
			"direction", direction,
			"contentHash", migration.Hash,
			"durationMs", duration.Milliseconds(),
			"releaseId", r.releaseID,
			"failureStatementSummary", truncate(runErr.Error(), 2048),
		)
		return fmt.Errorf("migration %06d %s failed: %w", migration.Version, direction, runErr)
	}
	slog.InfoContext(ctx, "database migration completed",
		"version", migration.Version,
		"direction", direction,
		"contentHash", migration.Hash,
		"durationMs", duration.Milliseconds(),
		"releaseId", r.releaseID,
		"status", "succeeded",
	)
	return nil
}

func (r *Runner) acquireLock(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	return nil
}

func (r *Runner) releaseLock() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = r.db.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockKey)
}

func (r *Runner) ensureControlSchema(ctx context.Context) error {
	const statement = `
CREATE SCHEMA IF NOT EXISTS cineweave_migrations;
CREATE TABLE IF NOT EXISTS cineweave_migrations.cineweave_migration_audit (
    id BIGSERIAL PRIMARY KEY,
    version_id BIGINT NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('up', 'down')),
    content_hash TEXT NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    duration_ms BIGINT,
    release_id TEXT NOT NULL,
    error_summary TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS cineweave_migration_audit_version_idx
    ON cineweave_migrations.cineweave_migration_audit (version_id, id DESC);`
	if _, err := r.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("initialize migration control schema: %w", err)
	}
	return nil
}

func (r *Runner) validateAppliedHashes(ctx context.Context) error {
	rows, err := r.db.QueryContext(ctx, `
WITH latest AS (
    SELECT DISTINCT ON (version_id) version_id, is_applied
    FROM cineweave_migrations.cineweave_schema_versions
    ORDER BY version_id, id DESC
)
SELECT version_id FROM latest WHERE is_applied AND version_id > 0 ORDER BY version_id`)
	if err != nil {
		return fmt.Errorf("read applied migration versions: %w", err)
	}
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, version := range versions {
		migration, ok := r.migrationByVersion(version)
		if !ok {
			return fmt.Errorf("database has migration %d applied, but this release does not embed it", version)
		}
		var auditHash string
		var auditStatus string
		err := r.db.QueryRowContext(ctx, `
SELECT content_hash, status
FROM cineweave_migrations.cineweave_migration_audit
WHERE version_id = $1 AND direction = 'up'
ORDER BY id DESC
LIMIT 1`, version).Scan(&auditHash, &auditStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("migration %d is applied without a hash audit record", version)
		}
		if err != nil {
			return fmt.Errorf("read migration %d hash audit: %w", version, err)
		}
		if auditHash != migration.Hash {
			return fmt.Errorf("migration %d hash mismatch: database=%s release=%s", version, auditHash, migration.Hash)
		}
		if auditStatus == "running" {
			if _, err := r.db.ExecContext(ctx, `
UPDATE cineweave_migrations.cineweave_migration_audit
SET status = 'succeeded', duration_ms = COALESCE(duration_ms, 0),
    error_summary = 'recovered after runner interruption', completed_at = now()
WHERE id = (
    SELECT id FROM cineweave_migrations.cineweave_migration_audit
    WHERE version_id = $1 AND direction = 'up'
    ORDER BY id DESC LIMIT 1
)`, version); err != nil {
				return fmt.Errorf("recover migration %d audit: %w", version, err)
			}
		}
	}
	return nil
}

func (r *Runner) startAudit(ctx context.Context, migration migrationFile, direction string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO cineweave_migrations.cineweave_migration_audit
    (version_id, direction, content_hash, status, release_id)
VALUES ($1, $2, $3, 'running', $4)
RETURNING id`, migration.Version, direction, migration.Hash, r.releaseID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("start migration audit: %w", err)
	}
	return id, nil
}

func (r *Runner) finishAudit(ctx context.Context, id int64, duration time.Duration, runErr error) error {
	status := "succeeded"
	errorSummary := ""
	if runErr != nil {
		status = "failed"
		errorSummary = truncate(runErr.Error(), 2048)
	}
	_, err := r.db.ExecContext(ctx, `
UPDATE cineweave_migrations.cineweave_migration_audit
SET status = $2, duration_ms = $3, error_summary = NULLIF($4, ''), completed_at = now()
WHERE id = $1`, id, status, duration.Milliseconds(), errorSummary)
	if err != nil {
		return fmt.Errorf("finish migration audit: %w", err)
	}
	return nil
}

func (r *Runner) migrationByVersion(version int64) (migrationFile, bool) {
	index := sort.Search(len(r.migrations), func(i int) bool {
		return r.migrations[i].Version >= version
	})
	if index >= len(r.migrations) || r.migrations[index].Version != version {
		return migrationFile{}, false
	}
	return r.migrations[index], true
}

func loadMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.Glob(migrationfiles.FS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return nil, errors.New("no embedded migrations found")
	}
	result := make([]migrationFile, 0, len(entries))
	var previous int64
	for _, name := range entries {
		base := filepath.Base(name)
		matches := migrationNamePattern.FindStringSubmatch(base)
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", base)
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", base, err)
		}
		if version != previous+1 {
			return nil, fmt.Errorf("migration versions must be consecutive: expected %d, found %d", previous+1, version)
		}
		content, err := migrationfiles.FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", base, err)
		}
		text := string(content)
		if !strings.Contains(text, "-- +goose Up") || !strings.Contains(text, "-- +goose Down") {
			return nil, fmt.Errorf("migration %q must contain Goose Up and Down sections", base)
		}
		if version > providerProtectionBaselineVersion {
			if err := validateProtectedProviderMigration(base, text); err != nil {
				return nil, err
			}
		}
		result = append(result, migrationFile{
			Version: version,
			Name:    base,
			Hash:    migrationContentHash(content),
		})
		previous = version
	}
	return result, nil
}

func validateProtectedProviderMigration(name, content string) error {
	sql := migrationGuardSQL(content)
	sql = removeAllowedProviderRestoreStatements(name, sql)
	for _, table := range protectedProviderConfigurationTables {
		tablePattern := `(?:public\.)?` + regexp.QuoteMeta(table) + `\b`
		patterns := []string{
			`\btruncate\b[^;]*` + tablePattern,
			`\bdelete\s+from\s+(?:only\s+)?` + tablePattern,
			`\binsert\s+into\s+` + tablePattern,
			`\bupdate\s+(?:only\s+)?` + tablePattern,
			`\bdrop\s+table\b[^;]*` + tablePattern,
			`\balter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?` + tablePattern + `[^;]*\b(?:drop|rename)\b`,
		}
		for _, pattern := range patterns {
			if regexp.MustCompile(pattern).FindStringIndex(sql) != nil {
				return fmt.Errorf("migration %q contains a forbidden write to protected Provider configuration table %q", name, table)
			}
		}
	}
	return nil
}

func removeAllowedProviderRestoreStatements(name, sql string) string {
	if name != "000037_provider_model_deletion_rollback.sql" {
		return sql
	}
	statements := strings.Split(sql, ";")
	for index, statement := range statements {
		normalized := strings.ToLower(strings.TrimSpace(statement))
		if strings.HasPrefix(normalized, "insert into provider_models") &&
			strings.Contains(normalized, "from provider_model_deletion_tombstones") {
			statements[index] = ""
		}
	}
	return strings.Join(statements, ";")
}

// migrationGuardSQL removes comments and quoted data so prompt text cannot be
// mistaken for an executable Provider configuration statement. Dollar-quoted
// procedural bodies remain executable SQL and are recursively inspected.
func migrationGuardSQL(content string) string {
	var output strings.Builder
	for index := 0; index < len(content); {
		switch {
		case strings.HasPrefix(content[index:], "--"):
			if newline := strings.IndexByte(content[index:], '\n'); newline >= 0 {
				index += newline
				output.WriteByte('\n')
				index++
				continue
			}
			index = len(content)
		case strings.HasPrefix(content[index:], "/*"):
			if end := strings.Index(content[index+2:], "*/"); end >= 0 {
				index += end + 4
				output.WriteByte(' ')
				continue
			}
			index = len(content)
		case content[index] == '\'':
			index++
			for index < len(content) {
				if content[index] != '\'' {
					index++
					continue
				}
				if index+1 < len(content) && content[index+1] == '\'' {
					index += 2
					continue
				}
				index++
				break
			}
			output.WriteByte(' ')
		case content[index] == '$':
			tag, ok := migrationDollarQuoteTag(content[index:])
			if !ok {
				output.WriteByte(content[index])
				index++
				continue
			}
			bodyStart := index + len(tag)
			bodyEnd := strings.Index(content[bodyStart:], tag)
			executable := migrationDollarQuoteIsExecutable(output.String())
			if bodyEnd >= 0 {
				if executable {
					output.WriteByte(' ')
					output.WriteString(migrationGuardSQL(content[bodyStart : bodyStart+bodyEnd]))
				}
				index = bodyStart + bodyEnd + len(tag)
			} else {
				if executable {
					output.WriteByte(' ')
					output.WriteString(migrationGuardSQL(content[bodyStart:]))
				}
				index = len(content)
			}
			output.WriteByte(' ')
		default:
			character := content[index]
			if character != '"' {
				output.WriteByte(character)
			}
			index++
		}
	}
	return strings.ToLower(output.String())
}

func migrationDollarQuoteIsExecutable(prefix string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	return migrationExecutableDollarQuotePattern.MatchString(prefix)
}

func migrationDollarQuoteTag(content string) (string, bool) {
	if len(content) < 2 || content[0] != '$' {
		return "", false
	}
	end := strings.IndexByte(content[1:], '$')
	if end < 0 {
		return "", false
	}
	end++
	for _, character := range content[1:end] {
		if character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return "", false
		}
	}
	return content[:end+1], true
}

func migrationContentHash(content []byte) string {
	canonical := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	canonical = bytes.ReplaceAll(canonical, []byte("\r"), []byte("\n"))
	hash := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", hash)
}

func configureGoose() {
	goose.SetBaseFS(migrationfiles.FS)
	goose.SetTableName(gooseTable)
	_ = goose.SetDialect("postgres")
}

func IsProduction(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production":
		return true
	default:
		return false
	}
}

func isDestructive(command string) bool {
	switch command {
	case "down", "down-to", "reset":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
