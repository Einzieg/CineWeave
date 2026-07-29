package migrationstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const defaultMigrationDirectory = "."

var (
	identifierPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	migrationNamePattern = regexp.MustCompile(`^(\d+)_[a-z0-9_]+\.sql$`)
)

// ValidateMigrationFunc validates one migration before a stream can be opened.
// It is used by stream owners to enforce additional DDL and data-protection
// rules without coupling the generic runner to either Core or Commercial SQL.
type ValidateMigrationFunc func(name string, content []byte) error

// BeforeDownFunc is invoked after the current stream version is known and
// before a destructive migration command changes the ledger.
type BeforeDownFunc func(ctx context.Context, db *sql.DB, current, target int64) error

// Definition is the immutable identity and policy of one migration stream.
// Two streams are independent only when their control schema, ledger, audit
// table, advisory lock and embedded filesystem are all distinct.
type Definition struct {
	ID                string
	Files             fs.FS
	Directory         string
	ControlSchema     string
	LedgerTable       string
	AuditTable        string
	AuditIndex        string
	AdvisoryLockKey   int64
	ValidateMigration ValidateMigrationFunc
	BeforeDown        BeforeDownFunc
}

// Config contains deployment-specific values shared by all migration streams.
type Config struct {
	DatabaseURL string
	Environment string
	ReleaseID   string
	Output      io.Writer
}

// File is one validated, immutable migration source.
type File struct {
	Version int64
	Name    string
	Hash    string
}

// Identity is the database identity of a migration stream and is suitable for
// release-manifest comparison.
type Identity struct {
	StreamID        string
	ControlSchema   string
	LedgerTable     string
	AuditTable      string
	AdvisoryLockKey int64
}

type Runner struct {
	db          *sql.DB
	provider    *goose.Provider
	definition  Definition
	environment string
	releaseID   string
	output      io.Writer
	migrations  []File
	ledger      string
	audit       string
}

// Validate verifies the stream identity, source ordering, source hashes and
// owner-specific migration policy without connecting to a database.
func Validate(definition Definition) ([]File, error) {
	if err := validateDefinition(definition); err != nil {
		return nil, err
	}
	root, err := migrationRoot(definition)
	if err != nil {
		return nil, err
	}
	entries, err := fs.Glob(root, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list %s embedded migrations: %w", definition.ID, err)
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s migration stream has no embedded migrations", definition.ID)
	}

	result := make([]File, 0, len(entries))
	var previous int64
	for _, name := range entries {
		base := path.Base(name)
		matches := migrationNamePattern.FindStringSubmatch(base)
		if matches == nil {
			return nil, fmt.Errorf("%s migration stream has invalid filename %q", definition.ID, base)
		}
		version, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse %s migration version %q: %w", definition.ID, base, err)
		}
		if version != previous+1 {
			return nil, fmt.Errorf(
				"%s migration versions must be consecutive: expected %d, found %d",
				definition.ID,
				previous+1,
				version,
			)
		}
		content, err := fs.ReadFile(root, name)
		if err != nil {
			return nil, fmt.Errorf("read %s migration %q: %w", definition.ID, base, err)
		}
		text := string(content)
		if !strings.Contains(text, "-- +goose Up") || !strings.Contains(text, "-- +goose Down") {
			return nil, fmt.Errorf("%s migration %q must contain Goose Up and Down sections", definition.ID, base)
		}
		if definition.ValidateMigration != nil {
			if err := definition.ValidateMigration(base, content); err != nil {
				return nil, fmt.Errorf("%s migration policy: %w", definition.ID, err)
			}
		}
		result = append(result, File{
			Version: version,
			Name:    base,
			Hash:    ContentHash(content),
		})
		previous = version
	}
	return result, nil
}

func Open(ctx context.Context, cfg Config, definition Definition) (*Runner, error) {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	migrations, err := Validate(definition)
	if err != nil {
		return nil, err
	}
	root, err := migrationRoot(definition)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open %s migration database: %w", definition.ID, err)
	}
	// Session advisory locks are safe only if Goose cannot borrow another
	// connection while applying a migration.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect %s migration database: %w", definition.ID, err)
	}

	ledger := qualifiedName(definition.ControlSchema, definition.LedgerTable)
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		root,
		goose.WithTableName(ledger),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize %s Goose provider: %w", definition.ID, err)
	}
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}
	return &Runner{
		db:          db,
		provider:    provider,
		definition:  definition,
		environment: firstNonEmpty(cfg.Environment, "development"),
		releaseID:   firstNonEmpty(cfg.ReleaseID, "local-dev"),
		output:      output,
		migrations:  migrations,
		ledger:      quoteQualifiedName(definition.ControlSchema, definition.LedgerTable),
		audit:       quoteQualifiedName(definition.ControlSchema, definition.AuditTable),
	}, nil
}

func (r *Runner) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Runner) Identity() Identity {
	return Identity{
		StreamID:        r.definition.ID,
		ControlSchema:   r.definition.ControlSchema,
		LedgerTable:     r.definition.LedgerTable,
		AuditTable:      r.definition.AuditTable,
		AdvisoryLockKey: r.definition.AdvisoryLockKey,
	}
}

func (r *Runner) Head() int64 {
	if len(r.migrations) == 0 {
		return 0
	}
	return r.migrations[len(r.migrations)-1].Version
}

func (r *Runner) Run(ctx context.Context, command string, target int64) error {
	command = strings.ToLower(strings.TrimSpace(command))
	if err := ValidateCommandPolicy(r.environment, command); err != nil {
		return err
	}
	if err := r.acquireLock(ctx); err != nil {
		return err
	}
	defer r.releaseLock()

	if err := r.ensureControlSchema(ctx); err != nil {
		return err
	}
	current, err := r.provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("initialize %s Goose ledger: %w", r.definition.ID, err)
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
		return r.status(ctx)
	case "version":
		_, err := fmt.Fprintf(r.output, "%s\t%d\n", r.definition.ID, current)
		return err
	case "down":
		if err := r.beforeDown(ctx, current, max(current-1, 0)); err != nil {
			return err
		}
		return r.downOne(ctx)
	case "down-to":
		if target < 0 {
			return errors.New("down-to target must be zero or greater")
		}
		if err := r.beforeDown(ctx, current, target); err != nil {
			return err
		}
		return r.downTo(ctx, target)
	case "reset":
		if err := r.beforeDown(ctx, current, 0); err != nil {
			return err
		}
		return r.downTo(ctx, 0)
	default:
		return fmt.Errorf("unsupported migration command %q", command)
	}
}

func (r *Runner) beforeDown(ctx context.Context, current, target int64) error {
	if r.definition.BeforeDown == nil {
		return nil
	}
	return r.definition.BeforeDown(ctx, r.db, current, target)
}

func (r *Runner) up(ctx context.Context) error {
	current, err := r.provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	for _, migration := range r.migrations {
		if migration.Version <= current {
			continue
		}
		if err := r.runAudited(ctx, migration, "up", func() error {
			_, err := r.provider.ApplyVersion(ctx, migration.Version, true)
			return err
		}); err != nil {
			return err
		}
		current = migration.Version
	}
	return nil
}

func (r *Runner) downOne(ctx context.Context) error {
	current, err := r.provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	if current == 0 {
		return nil
	}
	migration, ok := r.migrationByVersion(current)
	if !ok {
		return fmt.Errorf("%s database has migration %d applied, but this release does not embed it", r.definition.ID, current)
	}
	return r.runAudited(ctx, migration, "down", func() error {
		_, err := r.provider.ApplyVersion(ctx, migration.Version, false)
		return err
	})
}

func (r *Runner) downTo(ctx context.Context, target int64) error {
	for {
		current, err := r.provider.GetDBVersion(ctx)
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

func (r *Runner) status(ctx context.Context) error {
	statuses, err := r.provider.Status(ctx)
	if err != nil {
		return err
	}
	for _, status := range statuses {
		if _, err := fmt.Fprintf(
			r.output,
			"%s\t%06d\t%s\t%s\n",
			r.definition.ID,
			status.Source.Version,
			status.State,
			status.Source.Path,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runAudited(ctx context.Context, migration File, direction string, run func() error) error {
	auditID, err := r.startAudit(ctx, migration, direction)
	if err != nil {
		return err
	}
	started := time.Now()
	runErr := run()
	duration := time.Since(started)
	if finishErr := r.finishAudit(ctx, auditID, duration, runErr); finishErr != nil {
		slog.ErrorContext(ctx, "database migration audit failed",
			"streamId", r.definition.ID,
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
			"streamId", r.definition.ID,
			"version", migration.Version,
			"direction", direction,
			"contentHash", migration.Hash,
			"durationMs", duration.Milliseconds(),
			"releaseId", r.releaseID,
			"failureStatementSummary", truncate(runErr.Error(), 2048),
		)
		return fmt.Errorf("%s migration %06d %s failed: %w", r.definition.ID, migration.Version, direction, runErr)
	}
	slog.InfoContext(ctx, "database migration completed",
		"streamId", r.definition.ID,
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
	if _, err := r.db.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, r.definition.AdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire %s migration advisory lock: %w", r.definition.ID, err)
	}
	return nil
}

func (r *Runner) releaseLock() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = r.db.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, r.definition.AdvisoryLockKey)
}

func (r *Runner) ensureControlSchema(ctx context.Context) error {
	schema := quoteIdentifier(r.definition.ControlSchema)
	audit := quoteQualifiedName(r.definition.ControlSchema, r.definition.AuditTable)
	index := quoteIdentifier(r.definition.AuditIndex)
	statement := fmt.Sprintf(`
CREATE SCHEMA IF NOT EXISTS %s;
CREATE TABLE IF NOT EXISTS %s (
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
CREATE INDEX IF NOT EXISTS %s ON %s (version_id, id DESC);`,
		schema,
		audit,
		index,
		audit,
	)
	if _, err := r.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("initialize %s migration control schema: %w", r.definition.ID, err)
	}
	return nil
}

func (r *Runner) validateAppliedHashes(ctx context.Context) error {
	query := fmt.Sprintf(`
WITH latest AS (
    SELECT DISTINCT ON (version_id) version_id, is_applied
    FROM %s
    ORDER BY version_id, id DESC
)
SELECT version_id FROM latest WHERE is_applied AND version_id > 0 ORDER BY version_id`, r.ledger)
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read %s applied migration versions: %w", r.definition.ID, err)
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
			return fmt.Errorf("%s database has migration %d applied, but this release does not embed it", r.definition.ID, version)
		}
		var auditHash string
		var auditStatus string
		auditQuery := fmt.Sprintf(`
SELECT content_hash, status
FROM %s
WHERE version_id = $1 AND direction = 'up'
ORDER BY id DESC
LIMIT 1`, r.audit)
		err := r.db.QueryRowContext(ctx, auditQuery, version).Scan(&auditHash, &auditStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s migration %d is applied without a hash audit record", r.definition.ID, version)
		}
		if err != nil {
			return fmt.Errorf("read %s migration %d hash audit: %w", r.definition.ID, version, err)
		}
		if auditHash != migration.Hash {
			return fmt.Errorf(
				"%s migration %d hash mismatch: database=%s release=%s",
				r.definition.ID,
				version,
				auditHash,
				migration.Hash,
			)
		}
		if auditStatus == "running" {
			recoverQuery := fmt.Sprintf(`
UPDATE %s
SET status = 'succeeded', duration_ms = COALESCE(duration_ms, 0),
    error_summary = 'recovered after runner interruption', completed_at = now()
WHERE id = (
    SELECT id FROM %s
    WHERE version_id = $1 AND direction = 'up'
    ORDER BY id DESC LIMIT 1
)`, r.audit, r.audit)
			if _, err := r.db.ExecContext(ctx, recoverQuery, version); err != nil {
				return fmt.Errorf("recover %s migration %d audit: %w", r.definition.ID, version, err)
			}
		}
	}
	return nil
}

func (r *Runner) startAudit(ctx context.Context, migration File, direction string) (int64, error) {
	query := fmt.Sprintf(`
INSERT INTO %s (version_id, direction, content_hash, status, release_id)
VALUES ($1, $2, $3, 'running', $4)
RETURNING id`, r.audit)
	var id int64
	if err := r.db.QueryRowContext(
		ctx,
		query,
		migration.Version,
		direction,
		migration.Hash,
		r.releaseID,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("start %s migration audit: %w", r.definition.ID, err)
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
	query := fmt.Sprintf(`
UPDATE %s
SET status = $2, duration_ms = $3, error_summary = NULLIF($4, ''), completed_at = now()
WHERE id = $1`, r.audit)
	if _, err := r.db.ExecContext(ctx, query, id, status, duration.Milliseconds(), errorSummary); err != nil {
		return fmt.Errorf("finish %s migration audit: %w", r.definition.ID, err)
	}
	return nil
}

func (r *Runner) migrationByVersion(version int64) (File, bool) {
	index := sort.Search(len(r.migrations), func(i int) bool {
		return r.migrations[i].Version >= version
	})
	if index >= len(r.migrations) || r.migrations[index].Version != version {
		return File{}, false
	}
	return r.migrations[index], true
}

func ContentHash(content []byte) string {
	canonical := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	canonical = bytes.ReplaceAll(canonical, []byte("\r"), []byte("\n"))
	hash := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", hash)
}

func ValidateCommandPolicy(environment, command string) error {
	if IsDestructive(command) && IsProduction(environment) {
		return fmt.Errorf("migration command %q is disabled in production; use a forward repair migration", command)
	}
	return nil
}

func IsProduction(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "prod", "production":
		return true
	default:
		return false
	}
}

func IsDestructive(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "down", "down-to", "reset":
		return true
	default:
		return false
	}
}

func validateDefinition(definition Definition) error {
	if !identifierPattern.MatchString(definition.ID) {
		return fmt.Errorf("migration stream ID %q must be a lowercase identifier", definition.ID)
	}
	if definition.Files == nil {
		return fmt.Errorf("%s migration filesystem is required", definition.ID)
	}
	for label, value := range map[string]string{
		"control schema": definition.ControlSchema,
		"ledger table":   definition.LedgerTable,
		"audit table":    definition.AuditTable,
		"audit index":    definition.AuditIndex,
	} {
		if !identifierPattern.MatchString(value) {
			return fmt.Errorf("%s %s %q must be a lowercase SQL identifier", definition.ID, label, value)
		}
	}
	if definition.AdvisoryLockKey == 0 {
		return fmt.Errorf("%s migration advisory lock key must be non-zero", definition.ID)
	}
	directory := firstNonEmpty(definition.Directory, defaultMigrationDirectory)
	if directory != defaultMigrationDirectory && !fs.ValidPath(directory) {
		return fmt.Errorf("%s migration directory %q is invalid", definition.ID, directory)
	}
	return nil
}

func migrationRoot(definition Definition) (fs.FS, error) {
	directory := firstNonEmpty(definition.Directory, defaultMigrationDirectory)
	if directory == defaultMigrationDirectory {
		return definition.Files, nil
	}
	root, err := fs.Sub(definition.Files, directory)
	if err != nil {
		return nil, fmt.Errorf("open %s migration directory %q: %w", definition.ID, directory, err)
	}
	return root, nil
}

func qualifiedName(schema, object string) string {
	return schema + "." + object
}

func quoteQualifiedName(schema, object string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(object)
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
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
