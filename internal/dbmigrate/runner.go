package dbmigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"unicode"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
	"github.com/Einzieg/cineweave/internal/editionmigration"
	"github.com/Einzieg/cineweave/internal/migrationstream"
)

const (
	providerProtectionBaselineVersion = int64(8)
	providerModelHardDeleteVersion    = int64(36)
	providerModelRollbackVersion      = int64(37)
)

var ErrProviderModelRollbackUnsafe = errors.New("provider model deletion history makes this rollback unsafe")

var (
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
	stream *migrationstream.Runner
}

type migrationFile = migrationstream.File

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
	releaseID := firstNonEmpty(cfg.ReleaseID, "local-dev")
	definition := editionmigration.CoreDefinition(
		migrationfiles.FS,
		validateCoreMigration,
		func(ctx context.Context, db *sql.DB, current, target int64) error {
			return preflightProviderModelRollback(ctx, db, releaseID, current, target)
		},
	)
	stream, err := migrationstream.Open(ctx, migrationstream.Config{
		DatabaseURL: cfg.DatabaseURL,
		Environment: cfg.Environment,
		ReleaseID:   releaseID,
	}, definition)
	if err != nil {
		return nil, err
	}
	return &Runner{stream: stream}, nil
}

func (r *Runner) Close() error {
	if r == nil || r.stream == nil {
		return nil
	}
	return r.stream.Close()
}

func ValidateEmbedded() error {
	_, err := loadMigrationFiles()
	return err
}

func (r *Runner) Run(ctx context.Context, command string, target int64) error {
	if r == nil || r.stream == nil {
		return errors.New("migration runner is not initialized")
	}
	return r.stream.Run(ctx, command, target)
}

func validateMigrationCommandPolicy(environment, command string) error {
	return migrationstream.ValidateCommandPolicy(environment, command)
}

func preflightProviderModelRollback(
	ctx context.Context,
	db *sql.DB,
	releaseID string,
	current,
	target int64,
) error {
	checkDeletionHistory, checkNullRenderPlans := providerModelRollbackPreflightScope(current, target)
	if !checkDeletionHistory && !checkNullRenderPlans {
		return nil
	}

	var tombstones int64
	var conflicts int64
	var nullRenderPlans int64
	if checkDeletionHistory {
		if err := db.QueryRowContext(ctx, `
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
	} else if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM video_render_plans WHERE provider_model_id IS NULL
`).Scan(&nullRenderPlans); err != nil {
		return fmt.Errorf("inspect nullable Render Plan provider models for migration range %d -> %d: %w", current, target, err)
	}

	if tombstones == 0 && conflicts == 0 && nullRenderPlans == 0 {
		slog.InfoContext(ctx, "provider model rollback preflight passed",
			"currentVersion", current,
			"targetVersion", target,
			"releaseId", releaseID,
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
		"releaseId", releaseID,
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

func loadMigrationFiles() ([]migrationFile, error) {
	return migrationstream.Validate(editionmigration.CoreDefinition(
		migrationfiles.FS,
		validateCoreMigration,
		nil,
	))
}

func validateCoreMigration(name string, content []byte) error {
	version := int64(0)
	if separator := strings.IndexByte(name, '_'); separator > 0 {
		_, _ = fmt.Sscan(name[:separator], &version)
	}
	if version <= providerProtectionBaselineVersion {
		return nil
	}
	return validateProtectedProviderMigration(name, string(content))
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
	return migrationstream.ContentHash(content)
}

func IsProduction(environment string) bool {
	return migrationstream.IsProduction(environment)
}

func isDestructive(command string) bool {
	return migrationstream.IsDestructive(command)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
