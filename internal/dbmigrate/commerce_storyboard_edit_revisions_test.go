package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceStoryboardEditRevisionsMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000053_commerce_storyboard_edit_revisions.sql")
	if err != nil {
		t.Fatalf("read commerce storyboard edit revisions migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN edit_revision BIGINT NOT NULL DEFAULT 1",
		"ADD COLUMN projection_hash TEXT",
		"ADD COLUMN allowed_shot_durations INTEGER[]",
		"commerce_storyboard_plans_allowed_durations_check",
		"protect_commerce_storyboard_plan_allowed_durations",
		"ADD COLUMN revision BIGINT NOT NULL DEFAULT 1",
		"ADD COLUMN manual_override BOOLEAN NOT NULL DEFAULT false",
		"commerce_storyboard_plans_unit_active_edit_idx",
		"DROP COLUMN IF EXISTS allowed_shot_durations",
		"DROP COLUMN IF EXISTS edit_revision",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000053 migration is missing %q", required)
		}
	}
}

func TestCommerceStoryboardEditRevisionsMigrationDoesNotWriteProviderConfiguration(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000053_commerce_storyboard_edit_revisions.sql")
	if err != nil {
		t.Fatalf("read commerce storyboard edit revisions migration: %v", err)
	}
	if err := validateProtectedProviderMigration("000053_commerce_storyboard_edit_revisions.sql", string(content)); err != nil {
		t.Fatalf("commerce storyboard edit revisions migration writes protected Provider configuration: %v", err)
	}
}
