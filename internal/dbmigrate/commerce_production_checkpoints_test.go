package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceProductionCheckpointsMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000048_commerce_production_checkpoints.sql")
	if err != nil {
		t.Fatalf("read commerce production checkpoints migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN source_commerce_workflow_binding_id UUID",
		"ADD COLUMN target_commerce_workflow_binding_id UUID",
		"commerce rebuild source binding identity mismatch",
		"commerce rebuild target binding identity mismatch",
		"CREATE TABLE commerce_script_unit_batch_coordinators",
		"CREATE TABLE commerce_script_unit_batch_items",
		"input_snapshot JSONB NOT NULL",
		"child_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL",
		"retry_of_coordinator_id UUID REFERENCES commerce_script_unit_batch_coordinators(id) ON DELETE SET NULL",
		"commerce_batch_items_status_idx",
		"CREATE TABLE commerce_production_runs",
		"CREATE TABLE commerce_production_run_items",
		"CREATE TABLE commerce_production_run_item_attempts",
		"UNIQUE(run_id, subject_type, subject_key)",
		"UNIQUE(item_id, attempt_number)",
		"shot production run item requires a storyboard shot subject",
		"terminal commerce production attempts are immutable",
		"CREATE TABLE commerce_product_rebuilds",
		"CREATE TABLE commerce_product_rebuild_items",
		"CREATE TABLE commerce_project_rebuild_items",
		"source_script_unit_revision BIGINT NOT NULL",
		"target_unit_configuration_hash TEXT",
		"commerce project rebuild item does not match source generation",
		"commerce project rebuild item does not match target generation",
		"ADD COLUMN commerce_script_unit_id UUID",
		"project_timelines_one_active_commerce_unit",
		"final_video_versions_narrative_project_version_unique",
		"final_video_versions_commerce_unit_version_unique",
		"final_video_versions_one_active_commerce_unit",
		"commerce timeline generation identity mismatch",
		"commerce final video requires script unit generation identity",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000048 migration is missing %q", required)
		}
	}
}

func TestCommerceProductionCheckpointsMigrationDoesNotWriteProviderConfiguration(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000048_commerce_production_checkpoints.sql")
	if err != nil {
		t.Fatalf("read commerce production checkpoints migration: %v", err)
	}
	if err := validateProtectedProviderMigration("000048_commerce_production_checkpoints.sql", string(content)); err != nil {
		t.Fatalf("commerce production checkpoints migration writes protected Provider configuration: %v", err)
	}
}
