package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceVideoRuntimeMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000055_commerce_video_runtime.sql")
	if err != nil {
		t.Fatalf("read commerce video runtime migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ALTER COLUMN storyboard_plan_id DROP NOT NULL",
		"ALTER COLUMN script_episode_id DROP NOT NULL",
		"ADD COLUMN commerce_storyboard_plan_id UUID",
		"ADD COLUMN commerce_product_id UUID",
		"ADD COLUMN commerce_script_unit_id UUID",
		"ADD COLUMN commerce_script_unit_generation_id UUID",
		"ADD COLUMN commerce_localization_id UUID",
		"prompt_context_plans_subject_kind_check",
		"prompt_context_plans_commerce_plan_fk",
		"prompt_context_plans_commerce_localization_fk",
		"prompt_context_plans_commerce_unit_idx",
		"NEW.commerce_script_unit_generation_id IS DISTINCT FROM OLD.commerce_script_unit_generation_id",
		"NEW.commerce_product_id IS DISTINCT FROM OLD.commerce_product_id",
		"cannot roll back commerce video runtime while commerce prompt context plans exist",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000055 migration is missing %q", required)
		}
	}
	if err := validateProtectedProviderMigration("000055_commerce_video_runtime.sql", sql); err != nil {
		t.Fatalf("commerce video runtime migration writes protected Provider configuration: %v", err)
	}
}
