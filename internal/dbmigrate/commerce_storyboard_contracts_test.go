package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceStoryboardContractsMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000047_commerce_storyboard_contracts.sql")
	if err != nil {
		t.Fatalf("read commerce storyboard contracts migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE commerce_storyboard_plans",
		"commerce_storyboard_plans_one_active",
		"commerce storyboard plan does not match the frozen unit generation",
		"ADD COLUMN commerce_storyboard_plan_id UUID",
		"storyboard_shots_plan_kind_check",
		"storyboard_source = 'commerce_script'",
		"CREATE TABLE commerce_shot_contracts",
		"CREATE TABLE commerce_shot_segment_links",
		"shot segment verbatim range exceeds localized voiceover",
		"CREATE TABLE commerce_shot_product_references",
		"shot product reference does not belong to the frozen reference pack",
		"ADD COLUMN commerce_script_unit_generation_id UUID",
		"video prompt commerce provenance is immutable",
		"commerce render plan identity does not match approved prompt plan",
		"render plan commerce provenance is immutable",
		"DROP TABLE IF EXISTS commerce_storyboard_plans",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000047 migration is missing %q", required)
		}
	}
}

func TestCommerceStoryboardContractsMigrationDoesNotWriteProviderConfiguration(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000047_commerce_storyboard_contracts.sql")
	if err != nil {
		t.Fatalf("read commerce storyboard contracts migration: %v", err)
	}
	if err := validateProtectedProviderMigration("000047_commerce_storyboard_contracts.sql", string(content)); err != nil {
		t.Fatalf("commerce storyboard contracts migration writes protected Provider configuration: %v", err)
	}
}
