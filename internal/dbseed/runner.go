package dbseed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	seedfiles "github.com/Einzieg/cineweave/db/seeds"
	"github.com/Einzieg/cineweave/internal/dbmigrate"
)

const seedLockKey = int64(0x43494e4557454157)

type Config struct {
	DatabaseURL string
	Environment string
	ReleaseID   string
}

type Runner struct {
	db        *sql.DB
	releaseID string
	resources []resource
}

type manifest struct {
	ResourceKey     string          `json:"resourceKey"`
	ResourceVersion int             `json:"resourceVersion"`
	Tables          []manifestTable `json:"tables"`
}

type manifestTable struct {
	Name string          `json:"name"`
	Rows json.RawMessage `json:"rows"`
}

type resource struct {
	Path   string
	Hash   string
	Data   manifest
	Counts map[string]int
}

type tableDefinition struct {
	ConflictColumns []string
	UpdateColumns   []string
}

var tableDefinitions = map[string]tableDefinition{
	"permissions": {
		ConflictColumns: []string{"permission_key"},
		UpdateColumns:   []string{"description", "name", "id", "managed_by"},
	},
	"roles": {
		ConflictColumns: []string{"id"},
		UpdateColumns:   []string{"organization_id", "role_key", "name", "scope", "is_system", "description", "updated_at", "managed_by"},
	},
	"role_permissions": {
		ConflictColumns: []string{"role_id", "permission_key"},
		UpdateColumns:   []string{"managed_by"},
	},
	"provider_connectors": {
		ConflictColumns: []string{"id"},
		UpdateColumns:   []string{"connector_key", "name", "type", "is_official", "manifest", "version", "managed_by"},
	},
	"provider_catalog_entries": {
		ConflictColumns: []string{"id"},
		UpdateColumns: []string{
			"provider_key", "name", "display_name", "description", "provider_type", "category",
			"logo_key", "docs_url", "default_base_url", "default_auth_type", "connector_manifest",
			"model_templates", "supported_task_types", "setup_schema", "enabled", "is_official",
			"updated_at", "managed_by",
		},
	},
	"provider_model_capability_presets": {
		ConflictColumns: []string{"id"},
		UpdateColumns: []string{
			"preset_key", "display_name", "modality", "match_patterns", "task_types", "input_limits",
			"output_limits", "quality_tiers", "provider_options_schema", "pricing_policy", "priority",
			"enabled", "updated_at", "managed_by",
		},
	},
	"prompt_templates": {
		ConflictColumns: []string{"id"},
		UpdateColumns: []string{
			"organization_id", "template_key", "name", "purpose", "created_by", "description", "modality",
			"task_type", "scope", "status", "is_system", "updated_at", "managed_by",
		},
	},
	"prompt_versions": {
		ConflictColumns: []string{"id"},
		UpdateColumns: []string{
			"prompt_template_id", "version_no", "content", "variables_schema", "content_hash", "created_by",
			"template_id", "version", "status", "title", "content_format", "metadata", "activated_at", "managed_by",
		},
	},
}

func ConfigFromEnv() (Config, error) {
	environment := firstNonEmpty(os.Getenv("CINEWEAVE_ENV"), os.Getenv("APP_ENV"), "development")
	releaseID := strings.TrimSpace(os.Getenv("CINEWEAVE_RELEASE_ID"))
	if releaseID == "" {
		if dbmigrate.IsProduction(environment) {
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
	resources, err := loadResources()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open seed database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect seed database: %w", err)
	}
	return &Runner{db: db, releaseID: firstNonEmpty(cfg.ReleaseID, "local-dev"), resources: resources}, nil
}

func (r *Runner) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func ValidateEmbedded() error {
	_, err := loadResources()
	return err
}

func (r *Runner) Apply(ctx context.Context) error {
	return r.withLock(ctx, func() error {
		for _, resource := range r.resources {
			if err := r.applyResource(ctx, resource); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Runner) Verify(ctx context.Context) error {
	return r.withLock(ctx, func() error {
		for _, resource := range r.resources {
			if err := r.verifyResource(ctx, resource); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Runner) applyResource(ctx context.Context, resource resource) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentVersion int
	var currentHash string
	err = tx.QueryRowContext(ctx, `
SELECT resource_version, content_hash
FROM system_seed_versions
WHERE resource_key = $1
FOR UPDATE`, resource.Data.ResourceKey).Scan(&currentVersion, &currentHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read seed state %q: %w", resource.Data.ResourceKey, err)
	}
	if err == nil {
		if currentVersion > resource.Data.ResourceVersion {
			return fmt.Errorf("seed %q database version %d is newer than release version %d", resource.Data.ResourceKey, currentVersion, resource.Data.ResourceVersion)
		}
		if currentVersion == resource.Data.ResourceVersion {
			if currentHash != resource.Hash {
				return fmt.Errorf("seed %q version %d hash mismatch: database=%s release=%s", resource.Data.ResourceKey, currentVersion, currentHash, resource.Hash)
			}
		}
	}

	for _, table := range resource.Data.Tables {
		definition := tableDefinitions[table.Name]
		if err := assertNoUserOwnershipCollision(ctx, tx, table, definition); err != nil {
			return fmt.Errorf("seed %q table %s: %w", resource.Data.ResourceKey, table.Name, err)
		}
		if err := upsertTable(ctx, tx, table, definition); err != nil {
			return fmt.Errorf("seed %q table %s: %w", resource.Data.ResourceKey, table.Name, err)
		}
	}
	countsJSON, err := json.Marshal(resource.Counts)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO system_seed_versions
    (resource_key, resource_version, content_hash, record_counts, release_id, applied_at)
VALUES ($1, $2, $3, $4::jsonb, $5, now())
ON CONFLICT (resource_key) DO UPDATE SET
    resource_version = EXCLUDED.resource_version,
    content_hash = EXCLUDED.content_hash,
    record_counts = EXCLUDED.record_counts,
    release_id = EXCLUDED.release_id,
    applied_at = EXCLUDED.applied_at`,
		resource.Data.ResourceKey, resource.Data.ResourceVersion, resource.Hash, string(countsJSON), r.releaseID); err != nil {
		return fmt.Errorf("record seed state %q: %w", resource.Data.ResourceKey, err)
	}
	return tx.Commit()
}

func (r *Runner) verifyResource(ctx context.Context, resource resource) error {
	var version int
	var hash string
	err := r.db.QueryRowContext(ctx, `
SELECT resource_version, content_hash
FROM system_seed_versions
WHERE resource_key = $1`, resource.Data.ResourceKey).Scan(&version, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("seed %q has not been applied", resource.Data.ResourceKey)
	}
	if err != nil {
		return err
	}
	if version != resource.Data.ResourceVersion || hash != resource.Hash {
		return fmt.Errorf("seed %q state mismatch: database=%d/%s release=%d/%s", resource.Data.ResourceKey, version, hash, resource.Data.ResourceVersion, resource.Hash)
	}
	for _, table := range resource.Data.Tables {
		definition := tableDefinitions[table.Name]
		matched, err := countMatchedSystemRows(ctx, r.db, table, definition)
		if err != nil {
			return fmt.Errorf("verify seed %q table %s: %w", resource.Data.ResourceKey, table.Name, err)
		}
		if matched != resource.Counts[table.Name] {
			return fmt.Errorf("verify seed %q table %s: expected %d exact system-managed rows, found %d", resource.Data.ResourceKey, table.Name, resource.Counts[table.Name], matched)
		}
	}
	return nil
}

func (r *Runner) withLock(ctx context.Context, run func() error) error {
	if _, err := r.db.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, seedLockKey); err != nil {
		return fmt.Errorf("acquire seed advisory lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = r.db.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, seedLockKey)
	}()
	return run()
}

func assertNoUserOwnershipCollision(ctx context.Context, tx *sql.Tx, table manifestTable, definition tableDefinition) error {
	join := joinPredicate("current", "incoming", definition.ConflictColumns)
	query := fmt.Sprintf(`
WITH incoming AS (
    SELECT * FROM jsonb_populate_recordset(NULL::public.%s, $1::jsonb)
)
SELECT count(*)
FROM public.%s current
JOIN incoming ON %s
WHERE current.managed_by <> 'system'`, table.Name, table.Name, join)
	var conflicts int
	if err := tx.QueryRowContext(ctx, query, string(table.Rows)).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts > 0 {
		return fmt.Errorf("%d stable keys are owned by users", conflicts)
	}
	return nil
}

func upsertTable(ctx context.Context, tx *sql.Tx, table manifestTable, definition tableDefinition) error {
	assignments := make([]string, 0, len(definition.UpdateColumns))
	for _, column := range definition.UpdateColumns {
		assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", column, column))
	}
	query := fmt.Sprintf(`
INSERT INTO public.%s
SELECT * FROM jsonb_populate_recordset(NULL::public.%s, $1::jsonb)
ON CONFLICT (%s) DO UPDATE SET %s
WHERE %s.managed_by = 'system'
  AND (to_jsonb(%s) - 'updated_at') IS DISTINCT FROM (to_jsonb(EXCLUDED) - 'updated_at')`,
		table.Name,
		table.Name,
		strings.Join(definition.ConflictColumns, ", "),
		strings.Join(assignments, ", "),
		table.Name,
		table.Name,
	)
	_, err := tx.ExecContext(ctx, query, string(table.Rows))
	return err
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func countMatchedSystemRows(ctx context.Context, queryer rowQuerier, table manifestTable, definition tableDefinition) (int, error) {
	join := joinPredicate("current", "incoming", definition.ConflictColumns)
	query := fmt.Sprintf(`
WITH incoming AS (
    SELECT * FROM jsonb_populate_recordset(NULL::public.%s, $1::jsonb)
)
SELECT count(*)
FROM public.%s current
JOIN incoming ON %s
WHERE current.managed_by = 'system'
  AND (to_jsonb(current) - 'updated_at') = (to_jsonb(incoming) - 'updated_at')`, table.Name, table.Name, join)
	var count int
	err := queryer.QueryRowContext(ctx, query, string(table.Rows)).Scan(&count)
	return count, err
}

func joinPredicate(left, right string, columns []string) string {
	predicates := make([]string, 0, len(columns))
	for _, column := range columns {
		predicates = append(predicates, fmt.Sprintf("%s.%s = %s.%s", left, column, right, column))
	}
	return strings.Join(predicates, " AND ")
}

func loadResources() ([]resource, error) {
	paths, err := fs.Glob(seedfiles.FS, "*/*.json")
	if err != nil {
		return nil, fmt.Errorf("list embedded seeds: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, errors.New("no embedded seed resources found")
	}
	resources := make([]resource, 0, len(paths))
	keys := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		content, err := seedfiles.FS.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var data manifest
		if err := json.Unmarshal(content, &data); err != nil {
			return nil, fmt.Errorf("parse seed %q: %w", path, err)
		}
		data.ResourceKey = strings.TrimSpace(data.ResourceKey)
		if data.ResourceKey == "" || data.ResourceVersion <= 0 {
			return nil, fmt.Errorf("seed %q has invalid key or version", path)
		}
		if _, exists := keys[data.ResourceKey]; exists {
			return nil, fmt.Errorf("duplicate seed resource key %q", data.ResourceKey)
		}
		keys[data.ResourceKey] = struct{}{}
		counts := make(map[string]int, len(data.Tables))
		seenTables := make(map[string]struct{}, len(data.Tables))
		for _, table := range data.Tables {
			definition, allowed := tableDefinitions[table.Name]
			if !allowed || len(definition.ConflictColumns) == 0 {
				return nil, fmt.Errorf("seed %q uses unsupported table %q", path, table.Name)
			}
			if _, exists := seenTables[table.Name]; exists {
				return nil, fmt.Errorf("seed %q repeats table %q", path, table.Name)
			}
			seenTables[table.Name] = struct{}{}
			var rows []map[string]any
			if err := json.Unmarshal(table.Rows, &rows); err != nil {
				return nil, fmt.Errorf("seed %q table %s rows: %w", path, table.Name, err)
			}
			if len(rows) == 0 {
				return nil, fmt.Errorf("seed %q table %s has no rows", path, table.Name)
			}
			stableKeys := make(map[string]struct{}, len(rows))
			for _, row := range rows {
				if row["managed_by"] != "system" {
					return nil, fmt.Errorf("seed %q table %s contains a non-system row", path, table.Name)
				}
				parts := make([]string, 0, len(definition.ConflictColumns))
				for _, column := range definition.ConflictColumns {
					value, exists := row[column]
					if !exists || value == nil || fmt.Sprint(value) == "" {
						return nil, fmt.Errorf("seed %q table %s row misses stable key %s", path, table.Name, column)
					}
					parts = append(parts, fmt.Sprint(value))
				}
				stableKey := strings.Join(parts, "\x00")
				if _, exists := stableKeys[stableKey]; exists {
					return nil, fmt.Errorf("seed %q table %s contains duplicate stable key", path, table.Name)
				}
				stableKeys[stableKey] = struct{}{}
			}
			counts[table.Name] = len(rows)
		}
		resources = append(resources, resource{
			Path:   path,
			Hash:   seedContentHash(content),
			Data:   data,
			Counts: counts,
		})
	}
	return resources, nil
}

func seedContentHash(content []byte) string {
	canonical := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	canonical = bytes.ReplaceAll(canonical, []byte("\r"), []byte("\n"))
	hash := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", hash)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
