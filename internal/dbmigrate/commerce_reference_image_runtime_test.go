package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceReferenceImageRuntimeMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000054_commerce_reference_image_runtime.sql")
	if err != nil {
		t.Fatalf("read commerce reference image runtime migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE commerce_image_prompt_plans",
		"CREATE TABLE commerce_shot_image_versions",
		"CREATE TABLE commerce_product_fidelity_reviews",
		"active_commerce_image_prompt_plan_id",
		"active_commerce_image_version_id",
		"commerce_image_prompt_plans_one_active_idx",
		"commerce_shot_image_versions_one_active_idx",
		"protect_commerce_image_prompt_plan",
		"protect_commerce_shot_image_version",
		"DROP TABLE IF EXISTS commerce_product_fidelity_reviews",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000054 migration is missing %q", required)
		}
	}
	if err := validateProtectedProviderMigration("000054_commerce_reference_image_runtime.sql", sql); err != nil {
		t.Fatalf("commerce reference image migration writes protected Provider configuration: %v", err)
	}
}
