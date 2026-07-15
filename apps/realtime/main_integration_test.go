package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/google/uuid"
)

func TestProjectEventLogSharesOutboxTransactionAndSupportsDurableCatchup(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run realtime integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for realtime integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	var organizationID, userID, workspaceID, projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO organizations(name, slug)
		VALUES ('Realtime Integration', $1)
		RETURNING id::text
	`, "realtime-"+suffix).Scan(&organizationID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO users(email, display_name)
		VALUES ($1, 'Realtime Integration')
		RETURNING id::text
	`, "realtime-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspaces(organization_id, name)
		VALUES ($1, 'Realtime Integration')
		RETURNING id::text
	`, organizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Realtime Integration', $3)
		RETURNING id::text
	`, organizationID, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO event_outbox(
			organization_id, project_id, event_type, schema_version,
			aggregate_type, aggregate_id, aggregate_revision, payload
		)
		SELECT $1, $2, 'workflow.node.progress', 1, 'workflow_node_run', gen_random_uuid(), item,
		       jsonb_build_object('progress', item, 'nodeRunId', gen_random_uuid())
		FROM generate_series(1, 500) AS item
	`, organizationID, projectID); err != nil {
		t.Fatalf("insert durable events: %v", err)
	}

	repository := &pgEventRepository{pool: pool}
	bounds, err := repository.Bounds(ctx, projectID)
	if err != nil {
		t.Fatalf("load stream bounds: %v", err)
	}
	if bounds.HighWatermark != 500 || bounds.RetainedFrom != 1 {
		t.Fatalf("bounds = %+v, want high watermark 500 and retained from 1", bounds)
	}

	cursor := int64(0)
	seen := 0
	restarted := false
	for {
		events, loadErr := repository.EventsAfter(ctx, projectID, cursor, 200)
		if loadErr != nil {
			t.Fatalf("load events after %d: %v", cursor, loadErr)
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			if event.Position != cursor+1 {
				t.Fatalf("event position = %d, want %d", event.Position, cursor+1)
			}
			if event.SchemaVersion != 1 || event.AggregateRevision == nil || *event.AggregateRevision != event.Position {
				t.Fatalf("event contract at position %d = schema %d revision %v", event.Position, event.SchemaVersion, event.AggregateRevision)
			}
			cursor = event.Position
			seen++
		}
		if !restarted && cursor == 200 {
			restartedPool, restartErr := db.Open(ctx, databaseURL)
			if restartErr != nil {
				t.Fatalf("reopen database after simulated realtime restart: %v", restartErr)
			}
			t.Cleanup(restartedPool.Close)
			repository = &pgEventRepository{pool: restartedPool}
			restarted = true
		}
	}
	if !restarted || seen != 500 || cursor != 500 {
		t.Fatalf("seen = %d cursor = %d, want 500", seen, cursor)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback transaction: %v", err)
	}
	rolledBackEventID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO event_outbox(
			id, organization_id, project_id, event_type, aggregate_type, aggregate_id, payload
		)
		VALUES ($1, $2, $3, 'workflow.node.progress', 'workflow_node_run', gen_random_uuid(), '{"progress":501}')
	`, rolledBackEventID, organizationID, projectID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert rollback event: %v", err)
	}
	var insideCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_event_log WHERE event_id = $1`, rolledBackEventID).Scan(&insideCount); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("query event inside transaction: %v", err)
	}
	if insideCount != 1 {
		_ = tx.Rollback(ctx)
		t.Fatalf("project event count inside transaction = %d, want 1", insideCount)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback event transaction: %v", err)
	}

	var outsideCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM project_event_log WHERE event_id = $1`, rolledBackEventID).Scan(&outsideCount); err != nil {
		t.Fatalf("query rolled back event: %v", err)
	}
	if outsideCount != 0 {
		t.Fatalf("rolled back project event count = %d, want 0", outsideCount)
	}
	afterRollback, err := repository.Bounds(ctx, projectID)
	if err != nil {
		t.Fatalf("load bounds after rollback: %v", err)
	}
	if afterRollback.HighWatermark != 500 {
		t.Fatalf("high watermark after rollback = %d, want 500", afterRollback.HighWatermark)
	}

	if organization, err := repository.ProjectOrganization(ctx, projectID); err != nil || organization != organizationID {
		t.Fatalf("project organization = %q err = %v, want %q", organization, err, organizationID)
	}
	t.Logf("verified %s project events through durable stream position %d", fmt.Sprint(seen), cursor)
}
