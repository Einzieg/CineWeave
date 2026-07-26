package dbmigrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func assertProjectDeletionHardDeletePath(
	t *testing.T,
	ctx context.Context,
	config *pgx.ConnConfig,
) {
	t.Helper()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect project deletion migration database: %v", err)
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx, `
		SELECT conrelid::regclass::text, conname, confdeltype
		FROM pg_constraint
		WHERE contype = 'f'
		  AND confrelid = 'projects'::regclass
		ORDER BY conrelid::regclass::text, conname
	`)
	if err != nil {
		t.Fatalf("list project foreign keys: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tableName string
		var constraintName string
		var deleteAction string
		if err := rows.Scan(&tableName, &constraintName, &deleteAction); err != nil {
			t.Fatalf("scan project foreign key: %v", err)
		}
		if deleteAction != "c" && deleteAction != "n" {
			t.Fatalf(
				"project foreign key %s.%s uses blocking delete action %q",
				tableName,
				constraintName,
				deleteAction,
			)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate project foreign keys: %v", err)
	}

	var blockingCrossReferences int
	if err := conn.QueryRow(ctx, `
		WITH RECURSIVE project_owned(oid) AS (
			SELECT 'projects'::regclass::oid
			UNION
			SELECT source_table.oid
			FROM project_owned parent
			JOIN pg_constraint foreign_key
			  ON foreign_key.confrelid = parent.oid
			 AND foreign_key.contype = 'f'
			 AND foreign_key.confdeltype = 'c'
			JOIN pg_class source_table
			  ON source_table.oid = foreign_key.conrelid
			JOIN pg_namespace source_namespace
			  ON source_namespace.oid = source_table.relnamespace
			 AND source_namespace.nspname = 'public'
		),
		project_mutated(oid) AS (
			SELECT oid
			FROM project_owned
			UNION
			SELECT source_table.oid
			FROM project_owned parent
			JOIN pg_constraint foreign_key
			  ON foreign_key.confrelid = parent.oid
			 AND foreign_key.contype = 'f'
			 AND foreign_key.confdeltype IN ('n', 'd')
			JOIN pg_class source_table
			  ON source_table.oid = foreign_key.conrelid
			JOIN pg_namespace source_namespace
			  ON source_namespace.oid = source_table.relnamespace
			 AND source_namespace.nspname = 'public'
		)
		SELECT count(*)
		FROM pg_constraint foreign_key
		WHERE foreign_key.contype = 'f'
		  AND foreign_key.confdeltype = 'r'
		  AND foreign_key.conrelid IN (SELECT oid FROM project_owned)
		  AND foreign_key.confrelid IN (SELECT oid FROM project_mutated)
	`).Scan(&blockingCrossReferences); err != nil {
		t.Fatalf("count project-owned blocking foreign keys: %v", err)
	}
	if blockingCrossReferences != 0 {
		t.Fatalf(
			"project deletion still has %d ordering-sensitive ON DELETE RESTRICT constraints",
			blockingCrossReferences,
		)
	}

	var projectOwnedTablesOutsideCascade int
	if err := conn.QueryRow(ctx, `
		WITH RECURSIVE project_owned(oid) AS (
			SELECT 'projects'::regclass::oid
			UNION
			SELECT source_table.oid
			FROM project_owned parent
			JOIN pg_constraint foreign_key
			  ON foreign_key.confrelid = parent.oid
			 AND foreign_key.contype = 'f'
			 AND foreign_key.confdeltype = 'c'
			JOIN pg_class source_table
			  ON source_table.oid = foreign_key.conrelid
			JOIN pg_namespace source_namespace
			  ON source_namespace.oid = source_table.relnamespace
			 AND source_namespace.nspname = 'public'
		)
		SELECT count(*)
		FROM pg_class project_table
		JOIN pg_namespace project_namespace
		  ON project_namespace.oid = project_table.relnamespace
		 AND project_namespace.nspname = 'public'
		JOIN pg_attribute project_column
		  ON project_column.attrelid = project_table.oid
		 AND project_column.attname = 'project_id'
		 AND project_column.attnotnull
		 AND NOT project_column.attisdropped
		WHERE project_table.relkind = 'r'
		  AND project_table.oid NOT IN (SELECT oid FROM project_owned)
		  AND project_table.relname NOT IN (
		      'project_deletion_requests',
		      'project_deletion_objects'
		  )
	`).Scan(&projectOwnedTablesOutsideCascade); err != nil {
		t.Fatalf("count project-owned tables outside cascade: %v", err)
	}
	if projectOwnedTablesOutsideCascade != 0 {
		t.Fatalf(
			"project schema has %d required project_id tables outside the cascade root",
			projectOwnedTablesOutsideCascade,
		)
	}

	var externalBlockingReferences int
	if err := conn.QueryRow(ctx, `
		WITH RECURSIVE project_owned(oid) AS (
			SELECT 'projects'::regclass::oid
			UNION
			SELECT source_table.oid
			FROM project_owned parent
			JOIN pg_constraint foreign_key
			  ON foreign_key.confrelid = parent.oid
			 AND foreign_key.contype = 'f'
			 AND foreign_key.confdeltype = 'c'
			JOIN pg_class source_table
			  ON source_table.oid = foreign_key.conrelid
			JOIN pg_namespace source_namespace
			  ON source_namespace.oid = source_table.relnamespace
			 AND source_namespace.nspname = 'public'
		)
		SELECT count(*)
		FROM pg_constraint foreign_key
		WHERE foreign_key.contype = 'f'
		  AND foreign_key.confdeltype = 'r'
		  AND foreign_key.conrelid NOT IN (SELECT oid FROM project_owned)
		  AND foreign_key.confrelid IN (SELECT oid FROM project_owned)
	`).Scan(&externalBlockingReferences); err != nil {
		t.Fatalf("count external blockers into project cascade: %v", err)
	}
	if externalBlockingReferences != 0 {
		t.Fatalf(
			"project cascade still has %d external ON DELETE RESTRICT references",
			externalBlockingReferences,
		)
	}

	var detachedRowBlockers int
	if err := conn.QueryRow(ctx, `
		WITH RECURSIVE project_owned(oid) AS (
			SELECT 'projects'::regclass::oid
			UNION
			SELECT source_table.oid
			FROM project_owned parent
			JOIN pg_constraint foreign_key
			  ON foreign_key.confrelid = parent.oid
			 AND foreign_key.contype = 'f'
			 AND foreign_key.confdeltype = 'c'
			JOIN pg_class source_table
			  ON source_table.oid = foreign_key.conrelid
			JOIN pg_namespace source_namespace
			  ON source_namespace.oid = source_table.relnamespace
			 AND source_namespace.nspname = 'public'
		),
		project_detached_columns(table_oid, column_number) AS (
			SELECT
				detach_foreign_key.conrelid,
				unnest(detach_foreign_key.conkey)
			FROM pg_constraint detach_foreign_key
			WHERE detach_foreign_key.contype = 'f'
			  AND detach_foreign_key.confdeltype IN ('n', 'd')
			  AND detach_foreign_key.confrelid IN (SELECT oid FROM project_owned)
		)
		SELECT count(*)
		FROM pg_constraint blocker
		WHERE blocker.contype = 'f'
		  AND blocker.confdeltype = 'r'
		  AND blocker.conrelid NOT IN (SELECT oid FROM project_owned)
		  AND EXISTS (
		      SELECT 1
		      FROM project_detached_columns detached
		      WHERE detached.table_oid = blocker.confrelid
		        AND detached.column_number = ANY(blocker.confkey)
		  )
	`).Scan(&detachedRowBlockers); err != nil {
		t.Fatalf("count blockers into project-detached columns: %v", err)
	}
	if detachedRowBlockers != 0 {
		t.Fatalf(
			"project deletion still has %d persistent RESTRICT references into detached columns",
			detachedRowBlockers,
		)
	}

	var deferredConstraintCount int
	var allDeferredCapable bool
	if err := conn.QueryRow(ctx, `
		SELECT
			count(*),
			COALESCE(bool_and(
				confdeltype = 'a'
				AND condeferrable
			), false)
		FROM pg_constraint
		WHERE obj_description(oid, 'pg_constraint')
		      LIKE 'cineweave:project-deletion-deferred:v1:%'
	`).Scan(&deferredConstraintCount, &allDeferredCapable); err != nil {
		t.Fatalf("inspect project deletion deferred constraints: %v", err)
	}
	if deferredConstraintCount == 0 || !allDeferredCapable {
		t.Fatalf(
			"project deletion deferred constraints = count:%d deferredCapable:%t",
			deferredConstraintCount,
			allDeferredCapable,
		)
	}

	userID := uuid.NewString()
	organizationID := uuid.NewString()
	workspaceID := uuid.NewString()
	projectID := uuid.NewString()
	requestID := uuid.NewString()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")

	if _, err := conn.Exec(ctx, `
		INSERT INTO users(id, email, display_name, status)
		VALUES ($1, $2, 'Project Deletion Migration User', 'active')
	`, userID, "project-deletion-"+suffix+"@example.test"); err != nil {
		t.Fatalf("insert project deletion user: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO organizations(id, name, slug)
		VALUES ($1, 'Project Deletion Migration Org', $2)
	`, organizationID, "project-deletion-"+suffix); err != nil {
		t.Fatalf("insert project deletion organization: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO workspaces(id, organization_id, name)
		VALUES ($1, $2, 'Project Deletion Migration Workspace')
	`, workspaceID, organizationID); err != nil {
		t.Fatalf("insert project deletion workspace: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO projects(
			id, organization_id, workspace_id, name, project_kind,
			project_type, content_type, created_by, video_production_state,
			lifecycle_status, deletion_revision, deletion_requested_at
		)
		VALUES ($1, $2, $3, 'Project Deletion Migration Project', 'commerce_video',
		        'commerce_video', NULL, $4, 'unconfigured',
		        'deleting', 1, now())
	`, projectID, organizationID, workspaceID, userID); err != nil {
		t.Fatalf("insert deleting project: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO project_deletion_requests(
			id, organization_id, workspace_id, project_id, project_name,
			project_revision, deletion_revision, status, impact_snapshot, impact_hash,
			temporal_workflow_id, idempotency_key, requested_by, drain_deadline_at
		)
		VALUES (
			$1, $2, $3, $4, 'Project Deletion Migration Project',
			1, 1, 'deleting_storage', '{}'::jsonb, $5,
			$6, $7, $8, now() + interval '15 minutes'
		)
	`, requestID, organizationID, workspaceID, projectID, strings.Repeat("a", 64),
		"project-deletion-"+requestID, "request-"+requestID, userID,
	); err != nil {
		t.Fatalf("insert project deletion request: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO project_deletion_objects(
			request_id, project_id, source_kind, storage_key
		)
		VALUES ($1, $2, 'artifact', $3)
	`, requestID, projectID, "projects/"+projectID+"/fixture.bin"); err != nil {
		t.Fatalf("insert project deletion manifest object: %v", err)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Fatalf("hard delete project with deletion runtime state: %v", err)
	}
	var projectCount int
	var requestCount int
	var objectCount int
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM projects WHERE id = $1),
			(SELECT count(*) FROM project_deletion_requests WHERE id = $2),
			(SELECT count(*) FROM project_deletion_objects WHERE request_id = $2)
	`, projectID, requestID).Scan(&projectCount, &requestCount, &objectCount); err != nil {
		t.Fatalf("verify project hard delete: %v", err)
	}
	if projectCount != 0 || requestCount != 1 || objectCount != 1 {
		t.Fatalf(
			"hard delete state = project:%d request:%d object:%d, want 0/1/1",
			projectCount,
			requestCount,
			objectCount,
		)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID); err != nil {
		t.Fatalf("clean up project deletion fixture: %v", err)
	}
}
