package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestDerivedAssetExecutionV2MigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000043_derived_asset_execution_v2.sql")
	if err != nil {
		t.Fatalf("read derived asset execution v2 migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE derived_asset_batches",
		"CREATE TABLE derived_asset_request_items",
		"CREATE TABLE derived_asset_execution_items",
		"input_ordinal INTEGER NOT NULL",
		"original_id TEXT NOT NULL",
		"requirement_id UUID",
		"duplicate_of_request_item_id UUID",
		"error_code TEXT",
		"retryable BOOLEAN NOT NULL",
		"'executable', 'review_required', 'not_found', 'generation_mismatch'",
		"'already_running', 'duplicate', 'skipped'",
		"retry_of_batch_id UUID",
		"retry_of_request_item_id UUID",
		"retry_of_attempt_id UUID",
		"production_generation_id UUID NOT NULL",
		"video_production_binding_revision BIGINT NOT NULL",
		"node_key TEXT NOT NULL",
		"requirement_snapshot JSONB NOT NULL",
		"storyboard_shot_snapshot JSONB NOT NULL",
		"canonical_asset_snapshot JSONB NOT NULL",
		"prompt_snapshot JSONB NOT NULL",
		"reference_snapshot JSONB NOT NULL",
		"model_snapshot JSONB NOT NULL",
		"capability_snapshot JSONB NOT NULL",
		"request_hash TEXT NOT NULL",
		"lease_expires_at TIMESTAMPTZ",
		"diagnostic JSONB NOT NULL",
		"late_result_diagnostics JSONB NOT NULL",
		"derived_asset_execution_items_one_active_per_item",
		"derived_asset_execution_items_stuck_lease_idx",
		"derived_asset_execution_items_stuck_queue_idx",
		"derived_asset_batches_disposition_aggregate_check",
		"derived_asset_batches_execution_aggregate_check",
		"derived_asset_request_items_failure_status_check",
		"derived_asset_execution_items_terminal_time_check",
		"CREATE FUNCTION sync_derived_asset_request_item_from_attempt()",
		"error_code = CASE",
		"THEN NEW.error_code",
		"OLD.status IN ('pending', 'queued', 'running')",
		"NEW.status IN ('failed_retryable', 'failed_terminal', 'cancelled', 'discarded')",
		"derived asset request item error outcome is immutable",
		"protect_derived_asset_execution_item_snapshot",
		"DROP TABLE IF EXISTS derived_asset_execution_items",
		"DROP TABLE IF EXISTS derived_asset_request_items",
		"DROP TABLE IF EXISTS derived_asset_batches",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000043 migration is missing %q", required)
		}
	}
}

func TestDerivedAssetExecutionV2MigrationDoesNotWriteProviderConfiguration(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000043_derived_asset_execution_v2.sql")
	if err != nil {
		t.Fatalf("read derived asset execution v2 migration: %v", err)
	}
	if err := validateProtectedProviderMigration("000043_derived_asset_execution_v2.sql", string(content)); err != nil {
		t.Fatalf("derived asset execution v2 migration writes protected Provider configuration: %v", err)
	}
}
