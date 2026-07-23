package dbmigrate_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/dbmigrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCommerceSetupRunsMigrationUpDownUp(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run commerce setup migration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce setup migration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	baseConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	databaseName := "cineweave_commerce_setup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	testURL, err := databaseURLForTestDatabase(databaseURL, databaseName)
	if err != nil {
		t.Fatalf("build temporary database URL: %v", err)
	}
	runner, err := dbmigrate.Open(ctx, dbmigrate.Config{
		DatabaseURL: testURL,
		Environment: "test",
		ReleaseID:   "commerce-setup-migration-test",
	})
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	defer runner.Close()

	if err := runner.Run(ctx, "up", 0); err != nil {
		t.Fatalf("first migrate up: %v", err)
	}
	testConfig := baseConfig.Copy()
	testConfig.Database = databaseName
	conn, err := pgx.ConnectConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("connect temporary database: %v", err)
	}
	defer conn.Close(context.Background())

	assertCommerceSetupMigrationVersion(t, ctx, conn, 53)
	assertCommerceSetupMigrationUpSchema(t, ctx, conn)
	fixture := insertCommerceSetupMigrationFixture(t, ctx, conn)
	firstOutboxID := exerciseCommerceSetupMigrationUp(t, ctx, conn, fixture)

	if err := runner.Run(ctx, "down-to", 50); err != nil {
		t.Fatalf("migrate down to 50: %v", err)
	}
	assertCommerceSetupMigrationVersion(t, ctx, conn, 50)
	assertCommerceSetupMigrationDownSchema(t, ctx, conn, fixture.setupSessionID, firstOutboxID)

	if err := runner.Run(ctx, "up", 0); err != nil {
		t.Fatalf("second migrate up: %v", err)
	}
	assertCommerceSetupMigrationVersion(t, ctx, conn, 53)
	assertCommerceSetupMigrationUpSchema(t, ctx, conn)
	exerciseCommerceSetupMigrationUp(t, ctx, conn, fixture)
}

type commerceSetupMigrationFixture struct {
	organizationID string
	projectID      string
	setupSessionID string
	userID         string
}

func insertCommerceSetupMigrationFixture(t *testing.T, ctx context.Context, conn *pgx.Conn) commerceSetupMigrationFixture {
	t.Helper()
	fixture := commerceSetupMigrationFixture{
		organizationID: uuid.NewString(),
		projectID:      uuid.NewString(),
		setupSessionID: uuid.NewString(),
		userID:         uuid.NewString(),
	}
	workspaceID := uuid.NewString()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")

	if _, err := conn.Exec(ctx, `
		INSERT INTO users(id, email, display_name, status)
		VALUES ($1, $2, 'Commerce Setup Migration User', 'active')
	`, fixture.userID, "commerce-setup-"+suffix+"@example.test"); err != nil {
		t.Fatalf("insert migration user: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO organizations(id, name, slug)
		VALUES ($1, 'Commerce Setup Migration Org', $2)
	`, fixture.organizationID, "commerce-setup-"+suffix); err != nil {
		t.Fatalf("insert migration organization: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO workspaces(id, organization_id, name)
		VALUES ($1, $2, 'Commerce Setup Migration Workspace')
	`, workspaceID, fixture.organizationID); err != nil {
		t.Fatalf("insert migration workspace: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO projects(
			id, organization_id, workspace_id, name, project_kind,
			project_type, content_type, created_by, video_production_state
		)
		VALUES ($1, $2, $3, 'Commerce Setup Migration Project', 'commerce_video',
		        'commerce_video', NULL, $4, 'unconfigured')
	`, fixture.projectID, fixture.organizationID, workspaceID, fixture.userID); err != nil {
		t.Fatalf("insert migration project: %v", err)
	}

	var workflowTemplateVersionID string
	if err := conn.QueryRow(ctx, `
		SELECT version.id::text
		FROM commerce_workflow_template_versions version
		JOIN commerce_workflow_templates template ON template.id = version.template_id
		WHERE template.template_key = 'commerce_video_v1'
		  AND version.version = 1
		  AND version.status = 'published'
	`).Scan(&workflowTemplateVersionID); err != nil {
		t.Fatalf("load commerce workflow template version: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO commerce_setup_sessions(
			id, organization_id, workspace_id, project_id, workflow_template_version_id,
			idempotency_scope, client_request_id, request_hash, scope_type,
			input_snapshot, input_hash, created_by, expires_at
		)
		VALUES ($1, $2, $3, $4, $5,
		        'commerce-setup-migration', $6, $7, 'project',
		        '{}'::jsonb, $8, $9, now() + interval '1 hour')
	`, fixture.setupSessionID, fixture.organizationID, workspaceID, fixture.projectID,
		workflowTemplateVersionID, "request-"+suffix, strings.Repeat("a", 64),
		strings.Repeat("b", 64), fixture.userID); err != nil {
		t.Fatalf("insert commerce setup session: %v", err)
	}

	return fixture
}

func exerciseCommerceSetupMigrationUp(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	fixture commerceSetupMigrationFixture,
) string {
	t.Helper()
	setupRunID := uuid.NewString()
	if _, err := conn.Exec(ctx, `
		INSERT INTO commerce_setup_runs(
			id, organization_id, project_id, setup_session_id,
			temporal_workflow_id, input_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, setupRunID, fixture.organizationID, fixture.projectID, fixture.setupSessionID,
		"commerce-setup-"+setupRunID, strings.Repeat("c", 64), fixture.userID); err != nil {
		t.Fatalf("insert commerce setup run: %v", err)
	}
	var firstAttempt int
	if err := conn.QueryRow(ctx, `SELECT attempt_no FROM commerce_setup_runs WHERE id = $1`, setupRunID).Scan(&firstAttempt); err != nil {
		t.Fatalf("load first setup attempt: %v", err)
	}
	if firstAttempt != 1 {
		t.Fatalf("first setup attempt = %d, want 1", firstAttempt)
	}
	secondRunID := uuid.NewString()
	if _, err := conn.Exec(ctx, `
		INSERT INTO commerce_setup_runs(
			id, organization_id, project_id, setup_session_id, attempt_no,
			temporal_workflow_id, input_hash, created_by
		)
		VALUES ($1, $2, $3, $4, 2, $5, $6, $7)
	`, secondRunID, fixture.organizationID, fixture.projectID, fixture.setupSessionID,
		"commerce-setup-"+secondRunID, strings.Repeat("e", 64), fixture.userID); err != nil {
		t.Fatalf("insert second commerce setup attempt: %v", err)
	}
	assertPostgresError(t, "session rejects a duplicate setup attempt", "23505", "commerce_setup_runs_session_attempt_unique", func() error {
		duplicateRunID := uuid.NewString()
		_, err := conn.Exec(ctx, `
			INSERT INTO commerce_setup_runs(
				id, organization_id, project_id, setup_session_id, attempt_no,
				temporal_workflow_id, input_hash, created_by
			)
			VALUES ($1, $2, $3, $4, 2, $5, $6, $7)
		`, duplicateRunID, fixture.organizationID, fixture.projectID, fixture.setupSessionID,
			"commerce-setup-"+duplicateRunID, strings.Repeat("f", 64), fixture.userID)
		return err
	})

	assertPostgresError(t, "session rejects a non-setup-run target", "23503", "commerce_setup_sessions_setup_workflow_run_fk", func() error {
		_, err := conn.Exec(ctx, `
			UPDATE commerce_setup_sessions
			SET setup_workflow_run_id = $2
			WHERE id = $1
		`, fixture.setupSessionID, uuid.NewString())
		return err
	})
	if _, err := conn.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET setup_workflow_run_id = $2
		WHERE id = $1
	`, fixture.setupSessionID, setupRunID); err != nil {
		t.Fatalf("attach setup run through the new session foreign key: %v", err)
	}

	assertPostgresError(t, "outbox requires one target", "23514", "workflow_start_outbox_target_check", func() error {
		productionGenerationID := uuid.NewString()
		return insertCommerceSetupOutbox(ctx, conn, uuid.NewString(), fixture, nil, nil, nil, &productionGenerationID, "project_workflow")
	})
	workflowRunID := uuid.NewString()
	agentTaskID := uuid.NewString()
	assertPostgresError(t, "outbox rejects multiple targets", "23514", "workflow_start_outbox_target_check", func() error {
		productionGenerationID := uuid.NewString()
		return insertCommerceSetupOutbox(ctx, conn, uuid.NewString(), fixture, &workflowRunID, &agentTaskID, nil, &productionGenerationID, "project_workflow")
	})
	assertPostgresError(t, "setup target requires setup workflow type", "23514", "workflow_start_outbox_production_identity_check", func() error {
		return insertCommerceSetupOutbox(ctx, conn, uuid.NewString(), fixture, nil, nil, &setupRunID, nil, "project_workflow")
	})
	assertPostgresError(t, "non-setup target requires a production generation", "23514", "workflow_start_outbox_production_identity_check", func() error {
		return insertCommerceSetupOutbox(ctx, conn, uuid.NewString(), fixture, &workflowRunID, nil, nil, nil, "project_workflow")
	})

	outboxID := uuid.NewString()
	if err := insertCommerceSetupOutbox(ctx, conn, outboxID, fixture, nil, nil, &setupRunID, nil, "commerce_project_setup"); err != nil {
		t.Fatalf("insert setup outbox with nullable production generation: %v", err)
	}
	var storedRunID string
	var productionGenerationID *string
	if err := conn.QueryRow(ctx, `
		SELECT commerce_setup_run_id::text, production_generation_id::text
		FROM workflow_start_outbox
		WHERE id = $1
	`, outboxID).Scan(&storedRunID, &productionGenerationID); err != nil {
		t.Fatalf("load inserted setup outbox: %v", err)
	}
	if storedRunID != setupRunID || productionGenerationID != nil {
		t.Fatalf("setup outbox identity = run:%q generation:%v, want run:%q generation:<nil>", storedRunID, productionGenerationID, setupRunID)
	}
	return outboxID
}

func insertCommerceSetupOutbox(
	ctx context.Context,
	conn *pgx.Conn,
	outboxID string,
	fixture commerceSetupMigrationFixture,
	workflowRunID *string,
	agentTaskID *string,
	setupRunID *string,
	productionGenerationID *string,
	workflowType string,
) error {
	_, err := conn.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			id, workflow_run_id, agent_task_id, commerce_setup_run_id,
			organization_id, project_id, workflow_type, temporal_workflow_id,
			task_queue, input, input_hash, workflow_handler, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		        'script-generation', '{}'::jsonb, $9, $7, $10)
	`, outboxID, workflowRunID, agentTaskID, setupRunID, fixture.organizationID, fixture.projectID,
		workflowType, "commerce-setup-outbox-"+outboxID, strings.Repeat("d", 64), productionGenerationID)
	return err
}

func assertCommerceSetupMigrationUpSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	assertRelationExists(t, ctx, conn, "commerce_setup_runs", true)
	assertColumnExists(t, ctx, conn, "commerce_setup_runs", "attempt_no", true)
	assertColumnNullable(t, ctx, conn, "workflow_start_outbox", "production_generation_id", true)
	assertColumnExists(t, ctx, conn, "workflow_start_outbox", "commerce_setup_run_id", true)
	assertConstraintContains(t, ctx, conn, "commerce_setup_sessions", "commerce_setup_sessions_setup_workflow_run_fk",
		"REFERENCES commerce_setup_runs(id) ON DELETE SET NULL")
	assertConstraintContains(t, ctx, conn, "workflow_start_outbox", "workflow_start_outbox_target_check",
		"num_nonnulls(workflow_run_id, agent_task_id, commerce_setup_run_id) = 1")
	assertConstraintContains(t, ctx, conn, "workflow_start_outbox", "workflow_start_outbox_production_identity_check",
		"commerce_setup_run_id IS NOT NULL", "production_generation_id IS NULL", "workflow_type = 'commerce_project_setup'::text")
	assertConstraintContains(t, ctx, conn, "workflow_start_outbox", "workflow_start_outbox_commerce_setup_run_id_fkey",
		"FOREIGN KEY (commerce_setup_run_id) REFERENCES commerce_setup_runs(id) ON DELETE CASCADE")
}

func assertCommerceSetupMigrationDownSchema(
	t *testing.T,
	ctx context.Context,
	conn *pgx.Conn,
	setupSessionID string,
	outboxID string,
) {
	t.Helper()
	assertRelationExists(t, ctx, conn, "commerce_setup_runs", false)
	assertColumnExists(t, ctx, conn, "workflow_start_outbox", "commerce_setup_run_id", false)
	assertColumnNullable(t, ctx, conn, "workflow_start_outbox", "production_generation_id", false)
	assertConstraintContains(t, ctx, conn, "commerce_setup_sessions", "commerce_setup_sessions_setup_workflow_run_id_fkey",
		"REFERENCES workflow_runs(id) ON DELETE SET NULL")
	assertConstraintContains(t, ctx, conn, "workflow_start_outbox", "workflow_start_outbox_target_check",
		"num_nonnulls(workflow_run_id, agent_task_id) = 1")
	assertConstraintMissing(t, ctx, conn, "workflow_start_outbox", "workflow_start_outbox_production_identity_check")

	var setupWorkflowRunID *string
	if err := conn.QueryRow(ctx, `
		SELECT setup_workflow_run_id::text
		FROM commerce_setup_sessions
		WHERE id = $1
	`, setupSessionID).Scan(&setupWorkflowRunID); err != nil {
		t.Fatalf("load setup session after rollback: %v", err)
	}
	if setupWorkflowRunID != nil {
		t.Fatalf("setup workflow run after rollback = %q, want NULL", *setupWorkflowRunID)
	}
	var outboxCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workflow_start_outbox WHERE id = $1`, outboxID).Scan(&outboxCount); err != nil {
		t.Fatalf("count setup outbox after rollback: %v", err)
	}
	if outboxCount != 0 {
		t.Fatalf("setup outbox rows after rollback = %d, want 0", outboxCount)
	}
}

func assertCommerceSetupMigrationVersion(t *testing.T, ctx context.Context, conn *pgx.Conn, expected int64) {
	t.Helper()
	var actual int64
	if err := conn.QueryRow(ctx, `
		SELECT COALESCE(max(version_id), 0)
		FROM cineweave_migrations.cineweave_schema_versions
		WHERE is_applied
	`).Scan(&actual); err != nil {
		t.Fatalf("load migration version: %v", err)
	}
	if actual != expected {
		t.Fatalf("migration version = %d, want %d", actual, expected)
	}
}

func assertRelationExists(t *testing.T, ctx context.Context, conn *pgx.Conn, relation string, expected bool) {
	t.Helper()
	var name *string
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.' || $1)::text`, relation).Scan(&name); err != nil {
		t.Fatalf("resolve relation %s: %v", relation, err)
	}
	if (name != nil) != expected {
		t.Fatalf("relation %s existence = %t, want %t", relation, name != nil, expected)
	}
}

func assertColumnExists(t *testing.T, ctx context.Context, conn *pgx.Conn, table, column string, expected bool) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&count); err != nil {
		t.Fatalf("inspect column %s.%s: %v", table, column, err)
	}
	if (count == 1) != expected {
		t.Fatalf("column %s.%s existence = %t, want %t", table, column, count == 1, expected)
	}
}

func assertColumnNullable(t *testing.T, ctx context.Context, conn *pgx.Conn, table, column string, expected bool) {
	t.Helper()
	var nullable string
	if err := conn.QueryRow(ctx, `
		SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&nullable); err != nil {
		t.Fatalf("inspect column nullability %s.%s: %v", table, column, err)
	}
	actual := nullable == "YES"
	if actual != expected {
		t.Fatalf("column %s.%s nullable = %t, want %t", table, column, actual, expected)
	}
}

func assertConstraintContains(t *testing.T, ctx context.Context, conn *pgx.Conn, table, constraint string, fragments ...string) {
	t.Helper()
	var definition string
	if err := conn.QueryRow(ctx, `
		SELECT pg_get_constraintdef(constraint_row.oid)
		FROM pg_constraint constraint_row
		JOIN pg_class relation ON relation.oid = constraint_row.conrelid
		JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'public'
		  AND relation.relname = $1
		  AND constraint_row.conname = $2
	`, table, constraint).Scan(&definition); err != nil {
		t.Fatalf("load constraint %s.%s: %v", table, constraint, err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(definition, fragment) {
			t.Errorf("constraint %s.%s = %q, missing %q", table, constraint, definition, fragment)
		}
	}
}

func assertConstraintMissing(t *testing.T, ctx context.Context, conn *pgx.Conn, table, constraint string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint constraint_row
		JOIN pg_class relation ON relation.oid = constraint_row.conrelid
		JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'public'
		  AND relation.relname = $1
		  AND constraint_row.conname = $2
	`, table, constraint).Scan(&count); err != nil {
		t.Fatalf("inspect constraint %s.%s: %v", table, constraint, err)
	}
	if count != 0 {
		t.Fatalf("constraint %s.%s exists after rollback", table, constraint)
	}
}

func assertPostgresError(t *testing.T, name, code, constraint string, run func() error) {
	t.Helper()
	err := run()
	if err == nil {
		t.Fatalf("%s: expected PostgreSQL error %s", name, code)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("%s: error type = %T, want *pgconn.PgError: %v", name, err, err)
	}
	if pgErr.Code != code || pgErr.ConstraintName != constraint {
		t.Fatalf("%s: PostgreSQL error = %s constraint %q, want %s constraint %q: %v",
			name, pgErr.Code, pgErr.ConstraintName, code, constraint, err)
	}
}
