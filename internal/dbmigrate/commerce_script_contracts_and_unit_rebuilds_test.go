package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceScriptContractsAndUnitRebuildsMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000057_commerce_script_contracts_and_unit_rebuilds.sql")
	if err != nil {
		t.Fatalf("read commerce script contracts migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE commerce_sales_script_contracts",
		"UNIQUE(script_unit_generation_id)",
		"organization_id, project_id, contract_hash) ON DELETE RESTRICT",
		"protect_commerce_sales_script_contract",
		"ready commerce sales script contracts are immutable",
		"ADD COLUMN sales_script_contract_id UUID NOT NULL",
		"ADD COLUMN sales_script_contract_hash TEXT NOT NULL",
		"commerce_storyboard_plans_sales_script_contract_fk",
		"CREATE TABLE commerce_script_unit_rebuilds",
		"CREATE UNIQUE INDEX commerce_script_unit_rebuilds_one_open",
		"protect_commerce_script_unit_rebuild_snapshot",
		"terminal commerce script unit rebuilds are immutable",
		"DROP TABLE IF EXISTS commerce_script_unit_rebuilds",
		"DROP TABLE IF EXISTS commerce_sales_script_contracts",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000057 migration is missing %q", required)
		}
	}
	if err := validateProtectedProviderMigration("000057_commerce_script_contracts_and_unit_rebuilds.sql", sql); err != nil {
		t.Fatalf("commerce script contract migration writes protected Provider configuration: %v", err)
	}
}
