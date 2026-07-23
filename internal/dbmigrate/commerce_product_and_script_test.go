package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceProductAndScriptMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000046_commerce_product_and_script.sql")
	if err != nil {
		t.Fatalf("read commerce product and script migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE commerce_products",
		"CREATE TABLE commerce_product_versions",
		"commerce product versions are immutable",
		"CREATE TABLE commerce_product_references",
		"commerce_product_references_one_primary",
		"commerce_product_references_active_hash_unique",
		"CREATE TABLE commerce_product_reference_packs",
		"CREATE TABLE commerce_product_reference_pack_items",
		"reference pack item snapshot mismatch",
		"CREATE TABLE commerce_script_units",
		"UNIQUE(project_id, unit_no)",
		"commerce_script_units_active_sort_unique",
		"CREATE TABLE commerce_ad_script_versions",
		"CREATE TABLE commerce_ad_script_segments",
		"CREATE TABLE commerce_language_resolutions",
		"CREATE TABLE commerce_ad_script_localizations",
		"CREATE TABLE commerce_localization_segments",
		"CREATE TABLE commerce_script_unit_generations",
		"commerce_unit_generations_one_active",
		"ON commerce_script_unit_generations(script_unit_id)",
		"script unit generation binding identity mismatch",
		"ADD COLUMN product_id UUID",
		"ADD COLUMN script_unit_id UUID",
		"DROP TABLE IF EXISTS commerce_script_unit_generations",
		"DROP TABLE IF EXISTS commerce_product_versions",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000046 migration is missing %q", required)
		}
	}
}

func TestCommerceProductAndScriptMigrationDoesNotWriteProviderConfiguration(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000046_commerce_product_and_script.sql")
	if err != nil {
		t.Fatalf("read commerce product and script migration: %v", err)
	}
	if err := validateProtectedProviderMigration("000046_commerce_product_and_script.sql", string(content)); err != nil {
		t.Fatalf("commerce product and script migration writes protected Provider configuration: %v", err)
	}
}
