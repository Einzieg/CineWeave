package dbmigrate_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		admin.Close(context.Background())
		t.Fatalf("create temporary database: %v", err)
	}
	t.Cleanup(func() {
		defer admin.Close(context.Background())
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()
		`, databaseName); err != nil {
			t.Errorf("terminate temporary database sessions: %v", err)
			return
		}
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+identifier); err != nil {
			t.Errorf("drop temporary database: %v", err)
		}
	})

	testConfig := baseConfig.Copy()
	testConfig.Database = databaseName
	testURL, err := databaseURLForTestDatabase(databaseURL, databaseName)
	if err != nil {
		t.Fatalf("build temporary database URL: %v", err)
	}
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
	assertProviderModelHardDeleteRollbackPreflight(t, ctx, runner, testConfig)
	assertVersion36NullRenderPlanRollbackPreflight(t, ctx, runner, testConfig)
	assertProjectDeletionHardDeletePath(t, ctx, testConfig)
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

func assertProviderModelHardDeleteRollbackPreflight(
	t *testing.T,
	ctx context.Context,
	runner *dbmigrate.Runner,
	config *pgx.ConnConfig,
) {
	t.Helper()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect provider model migration fixture database: %v", err)
	}
	defer conn.Close(context.Background())

	accountID := uuid.NewString()
	modelID := uuid.NewString()
	replacementModelID := uuid.NewString()
	attestationID := uuid.NewString()
	planID := uuid.NewString()
	organizationID := uuid.NewString()
	projectID := uuid.NewString()

	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatalf("disable fixture foreign key triggers: %v", err)
	}
	replicationRoleReset := false
	defer func() {
		if !replicationRoleReset {
			_, _ = conn.Exec(context.Background(), `SET session_replication_role = origin`)
		}
	}()
	if _, err := conn.Exec(ctx, `
		INSERT INTO provider_accounts(
			id, organization_id, connector_id, name, base_url,
			auth_type, status, config, created_by
		)
		VALUES ($1, $2, $3, 'Migration rollback fixture', 'https://example.invalid/v1',
		        'bearer', 'active', '{}', $4)
	`, accountID, organizationID, uuid.NewString(), uuid.NewString()); err != nil {
		t.Fatalf("insert provider account migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO provider_models(id, provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, 'migration-video-model', 'Migration Video Model', 'video', 'active')
	`, modelID, accountID); err != nil {
		t.Fatalf("insert provider model migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO provider_model_capability_attestations(
			id, organization_id, provider_model_id, variant_key, capability_snapshot_hash,
			verification_status, evidence_type, evidence, decision, reason
		)
		VALUES ($1, $2, $3, 'migration-fixture', $4, 'tested',
		        'adapter_contract_test', '{"fixture":true}', 'approved', 'migration fixture')
	`, attestationID, organizationID, modelID, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("insert provider model capability attestation migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO video_render_plans(
			id, organization_id, project_id, storyboard_shot_id,
			provider_account_id, provider_model_id, model_family, variant_key,
			capability_snapshot, capability_snapshot_hash, plan_key, status, active,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			task_type, reference_mode, aspect_ratio, resolution,
			audio_strategy, audio_requirement, native_audio_status, production_readiness,
			expires_at, production_generation_id, video_production_binding_id,
			video_production_binding_revision, profile_version_id,
			production_profile_snapshot, production_profile_snapshot_hash,
			capability_attestation_id
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, 'migration-video-model', 'fixture',
			'{"fixture":true}', $7, $8, 'archived', false,
			450000, 90000, 24, 1, 'video.image_to_video', 'first_frame', '16:9', '720p',
			'native_av', 'preferred', 'not_requested', 'preview_only', now() + interval '1 hour',
			$9, $10, 1, $11, '{"fixture":true}', $12, $13
		)
	`, planID, organizationID, projectID, uuid.NewString(), accountID, modelID,
		strings.Repeat("a", 64), "migration-plan-"+planID,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), strings.Repeat("b", 64), attestationID); err != nil {
		t.Fatalf("insert Render Plan migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `SET session_replication_role = origin`); err != nil {
		t.Fatalf("restore fixture foreign key triggers: %v", err)
	}
	replicationRoleReset = true

	if _, err := conn.Exec(ctx, `DELETE FROM provider_models WHERE id = $1`, modelID); err != nil {
		t.Fatalf("hard delete provider model fixture: %v", err)
	}
	var deletedModelID *string
	if err := conn.QueryRow(ctx, `SELECT provider_model_id::text FROM video_render_plans WHERE id = $1`, planID).Scan(&deletedModelID); err != nil {
		t.Fatalf("load Render Plan after provider model deletion: %v", err)
	}
	if deletedModelID != nil {
		t.Fatalf("Render Plan provider model = %q, want NULL after hard delete", *deletedModelID)
	}
	var preservedAttestationModelID *string
	if err := conn.QueryRow(ctx, `
		SELECT provider_model_id::text
		FROM provider_model_capability_attestations
		WHERE id = $1
	`, attestationID).Scan(&preservedAttestationModelID); err != nil {
		t.Fatalf("load preserved capability attestation after provider model deletion: %v", err)
	}
	if preservedAttestationModelID != nil {
		t.Fatalf("capability attestation provider model = %q, want NULL after hard delete", *preservedAttestationModelID)
	}
	var planAttestationID string
	if err := conn.QueryRow(ctx, `SELECT capability_attestation_id::text FROM video_render_plans WHERE id = $1`, planID).Scan(&planAttestationID); err != nil {
		t.Fatalf("load Render Plan capability attestation after provider model deletion: %v", err)
	}
	if planAttestationID != attestationID {
		t.Fatalf("Render Plan capability attestation = %q, want %q", planAttestationID, attestationID)
	}
	var tombstoneCount, referenceCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM provider_model_deletion_tombstones WHERE provider_model_id = $1`, modelID).Scan(&tombstoneCount); err != nil {
		t.Fatalf("load provider model deletion tombstone: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM provider_model_deletion_render_plan_references WHERE video_render_plan_id = $1 AND provider_model_id = $2`, planID, modelID).Scan(&referenceCount); err != nil {
		t.Fatalf("load provider model Render Plan reference: %v", err)
	}
	if tombstoneCount != 1 || referenceCount != 1 {
		t.Fatalf("provider model deletion provenance = tombstones:%d references:%d, want 1/1", tombstoneCount, referenceCount)
	}

	if _, err := conn.Exec(ctx, `
		INSERT INTO provider_models(id, provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, 'migration-video-model', 'Replacement Migration Video Model', 'video', 'active')
	`, replacementModelID, accountID); err != nil {
		t.Fatalf("insert conflicting replacement provider model: %v", err)
	}

	var expectedVersion int64
	if err := conn.QueryRow(ctx, `
		SELECT COALESCE(max(version_id), 0)
		FROM cineweave_migrations.cineweave_schema_versions
		WHERE is_applied
	`).Scan(&expectedVersion); err != nil {
		t.Fatalf("load schema version before rejected rollback: %v", err)
	}
	rollbackErr := runner.Run(ctx, "down-to", 36)
	if !errors.Is(rollbackErr, dbmigrate.ErrProviderModelRollbackUnsafe) {
		t.Fatalf("rollback error = %v, want ErrProviderModelRollbackUnsafe", rollbackErr)
	}
	var preflightErr *dbmigrate.ProviderModelRollbackPreflightError
	if !errors.As(rollbackErr, &preflightErr) {
		t.Fatalf("rollback error type = %T, want *ProviderModelRollbackPreflightError", rollbackErr)
	}
	if preflightErr.TombstoneCount != 1 || preflightErr.ConflictingModelCount != 1 || preflightErr.NullRenderPlanCount != 1 {
		t.Fatalf(
			"rollback preflight counts = tombstones:%d conflicts:%d nullPlans:%d, want 1/1/1",
			preflightErr.TombstoneCount,
			preflightErr.ConflictingModelCount,
			preflightErr.NullRenderPlanCount,
		)
	}

	var currentVersion int64
	if err := conn.QueryRow(ctx, `
		SELECT COALESCE(max(version_id), 0)
		FROM cineweave_migrations.cineweave_schema_versions
		WHERE is_applied
	`).Scan(&currentVersion); err != nil {
		t.Fatalf("load schema version after rejected rollback: %v", err)
	}
	if currentVersion != expectedVersion {
		t.Fatalf("schema version after rejected rollback = %d, want %d", currentVersion, expectedVersion)
	}
	if err := conn.QueryRow(ctx, `SELECT provider_model_id::text FROM video_render_plans WHERE id = $1`, planID).Scan(&deletedModelID); err != nil {
		t.Fatalf("load Render Plan after rejected rollback: %v", err)
	}
	if deletedModelID != nil {
		t.Fatalf("Render Plan provider model after rejected rollback = %q, want NULL", *deletedModelID)
	}

	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatalf("disable cleanup foreign key triggers: %v", err)
	}
	replicationRoleReset = false
	if _, err := conn.Exec(ctx, `DELETE FROM provider_model_deletion_render_plan_references WHERE video_render_plan_id = $1`, planID); err != nil {
		t.Fatalf("delete provider model Render Plan reference: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM video_render_plans WHERE id = $1`, planID); err != nil {
		t.Fatalf("delete Render Plan migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM provider_model_capability_attestations WHERE id = $1`, attestationID); err != nil {
		t.Fatalf("delete provider model capability attestation migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM provider_models WHERE id = $1`, replacementModelID); err != nil {
		t.Fatalf("delete replacement provider model migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM provider_model_deletion_tombstones WHERE provider_model_id = $1`, modelID); err != nil {
		t.Fatalf("delete provider model deletion tombstone: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM provider_accounts WHERE id = $1`, accountID); err != nil {
		t.Fatalf("delete provider account migration fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `SET session_replication_role = origin`); err != nil {
		t.Fatalf("restore cleanup foreign key triggers: %v", err)
	}
	replicationRoleReset = true
}

func assertVersion36NullRenderPlanRollbackPreflight(
	t *testing.T,
	ctx context.Context,
	runner *dbmigrate.Runner,
	config *pgx.ConnConfig,
) {
	t.Helper()
	if err := runner.Run(ctx, "down-to", 36); err != nil {
		t.Fatalf("prepare version 36 rollback preflight fixture: %v", err)
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect version 36 rollback fixture database: %v", err)
	}
	defer conn.Close(context.Background())

	accountID := uuid.NewString()
	planID := uuid.NewString()
	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatalf("disable version 36 fixture foreign key triggers: %v", err)
	}
	replicationRoleReset := false
	defer func() {
		if !replicationRoleReset {
			_, _ = conn.Exec(context.Background(), `SET session_replication_role = origin`)
		}
	}()
	if _, err := conn.Exec(ctx, `
		INSERT INTO provider_accounts(
			id, organization_id, connector_id, name, base_url,
			auth_type, status, config, created_by
		)
		VALUES ($1, $2, $3, 'Version 36 rollback fixture', 'https://example.invalid/v1',
		        'bearer', 'active', '{}', $4)
	`, accountID, uuid.NewString(), uuid.NewString(), uuid.NewString()); err != nil {
		t.Fatalf("insert version 36 provider account fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO video_render_plans(
			id, organization_id, project_id, storyboard_shot_id,
			provider_account_id, provider_model_id, model_family, variant_key,
			capability_snapshot, capability_snapshot_hash, plan_key, status, active,
			target_duration_ticks, timeline_timebase, fps_numerator, fps_denominator,
			task_type, reference_mode, aspect_ratio, resolution,
			audio_strategy, audio_requirement, native_audio_status, production_readiness,
			expires_at, production_generation_id, video_production_binding_id,
			video_production_binding_revision, profile_version_id,
			production_profile_snapshot, production_profile_snapshot_hash
		)
		VALUES (
			$1, $2, $3, $4, $5, NULL, 'deleted-model', 'fixture',
			'{"fixture":true}', $6, $7, 'archived', false,
			450000, 90000, 24, 1, 'video.image_to_video', 'first_frame', '16:9', '720p',
			'native_av', 'preferred', 'not_requested', 'preview_only', now() + interval '1 hour',
			$8, $9, 1, $10, '{"fixture":true}', $11
		)
	`, planID, uuid.NewString(), uuid.NewString(), uuid.NewString(), accountID,
		strings.Repeat("c", 64), "version-36-plan-"+planID,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), strings.Repeat("d", 64)); err != nil {
		t.Fatalf("insert version 36 NULL Render Plan fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `SET session_replication_role = origin`); err != nil {
		t.Fatalf("restore version 36 fixture foreign key triggers: %v", err)
	}
	replicationRoleReset = true

	rollbackErr := runner.Run(ctx, "down-to", 35)
	if !errors.Is(rollbackErr, dbmigrate.ErrProviderModelRollbackUnsafe) {
		t.Fatalf("version 36 rollback error = %v, want ErrProviderModelRollbackUnsafe", rollbackErr)
	}
	var preflightErr *dbmigrate.ProviderModelRollbackPreflightError
	if !errors.As(rollbackErr, &preflightErr) || preflightErr.NullRenderPlanCount != 1 {
		t.Fatalf("version 36 rollback preflight = %#v, want one NULL Render Plan", preflightErr)
	}
	var currentVersion int64
	if err := conn.QueryRow(ctx, `
		SELECT COALESCE(max(version_id), 0)
		FROM cineweave_migrations.cineweave_schema_versions
		WHERE is_applied
	`).Scan(&currentVersion); err != nil {
		t.Fatalf("load version after rejected version 36 rollback: %v", err)
	}
	if currentVersion != 36 {
		t.Fatalf("schema version after rejected version 36 rollback = %d, want 36", currentVersion)
	}

	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatalf("disable version 36 cleanup triggers: %v", err)
	}
	replicationRoleReset = false
	if _, err := conn.Exec(ctx, `DELETE FROM video_render_plans WHERE id = $1`, planID); err != nil {
		t.Fatalf("delete version 36 Render Plan fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM provider_accounts WHERE id = $1`, accountID); err != nil {
		t.Fatalf("delete version 36 provider account fixture: %v", err)
	}
	if _, err := conn.Exec(ctx, `SET session_replication_role = origin`); err != nil {
		t.Fatalf("restore version 36 cleanup triggers: %v", err)
	}
	replicationRoleReset = true

	if err := runner.Run(ctx, "up", 0); err != nil {
		t.Fatalf("reapply migrations after version 36 rollback preflight: %v", err)
	}
}

func databaseURLForTestDatabase(databaseURL, databaseName string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("DATABASE_URL must use postgres:// or postgresql:// for isolated migration tests")
	}
	parsed.Path = "/" + databaseName
	query := parsed.Query()
	query.Del("database")
	query.Del("dbname")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
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
	assertSeedPreservesCustomRole(t, ctx, databaseURL, runner)
}

func assertSeedPreservesCustomRole(t *testing.T, ctx context.Context, databaseURL string, runner *dbseed.Runner) {
	t.Helper()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse custom role test database URL: %v", err)
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect custom role seed test database: %v", err)
	}
	defer conn.Close(context.Background())

	organizationID := uuid.NewString()
	roleID := uuid.NewString()
	if _, err := conn.Exec(ctx, `
		INSERT INTO organizations(id, name, slug)
		VALUES ($1, 'Seed Isolation Org', $2)
	`, organizationID, "seed-isolation-"+strings.ReplaceAll(uuid.NewString(), "-", "")); err != nil {
		t.Fatalf("insert custom role test organization: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO roles(id, organization_id, role_key, name, scope, is_system, managed_by)
		VALUES ($1, $2, 'seed_isolation_editor', 'Seed Isolation Editor', 'project', false, 'user')
	`, roleID, organizationID); err != nil {
		t.Fatalf("insert custom role for seed isolation: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO role_permissions(role_id, permission_key, managed_by)
		VALUES ($1, 'project.read', 'user')
	`, roleID); err != nil {
		t.Fatalf("insert custom role permission for seed isolation: %v", err)
	}

	if err := runner.Apply(ctx); err != nil {
		t.Fatalf("reapply seed with existing custom role: %v", err)
	}
	var roleName, roleScope, roleManagedBy string
	var isSystem bool
	if err := conn.QueryRow(ctx, `
		SELECT name, scope, is_system, managed_by
		FROM roles
		WHERE id = $1
	`, roleID).Scan(&roleName, &roleScope, &isSystem, &roleManagedBy); err != nil {
		t.Fatalf("read custom role after seed reapply: %v", err)
	}
	if roleName != "Seed Isolation Editor" || roleScope != "project" || isSystem || roleManagedBy != "user" {
		t.Fatalf("custom role changed after seed reapply: name=%q scope=%q isSystem=%t managedBy=%q", roleName, roleScope, isSystem, roleManagedBy)
	}
	var permissionCount int
	var permissionKey, permissionManagedBy string
	if err := conn.QueryRow(ctx, `
		SELECT count(*), min(permission_key), min(managed_by)
		FROM role_permissions
		WHERE role_id = $1
	`, roleID).Scan(&permissionCount, &permissionKey, &permissionManagedBy); err != nil {
		t.Fatalf("read custom role permissions after seed reapply: %v", err)
	}
	if permissionCount != 1 || permissionKey != "project.read" || permissionManagedBy != "user" {
		t.Fatalf("custom role permissions changed after seed reapply: count=%d key=%q managedBy=%q", permissionCount, permissionKey, permissionManagedBy)
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
		`SELECT format('column|%I|%I|%s|%s|%s', c.relname, a.attname,
		                     pg_catalog.format_type(a.atttypid, a.atttypmod), a.attnotnull,
		                     COALESCE(pg_get_expr(d.adbin, d.adrelid), ''))
		 FROM pg_attribute a
		 JOIN pg_class c ON c.oid = a.attrelid
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped
		 ORDER BY c.relname, a.attname`,
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
