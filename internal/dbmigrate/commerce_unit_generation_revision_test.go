package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceUnitGenerationRevisionMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000059_commerce_unit_generation_revision.sql")
	if err != nil {
		t.Fatalf("read commerce unit generation revision migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN script_unit_revision BIGINT",
		"input #>> '{identity,scriptUnitGenerationId}'",
		"input #>> '{identity,scriptUnitRevision}'",
		"min(script_unit_revision)",
		"COALESCE(",
		"unit.revision",
		"ALTER COLUMN script_unit_revision SET NOT NULL",
		"CHECK (script_unit_revision > 0)",
		"CREATE FUNCTION protect_commerce_unit_generation_revision()",
		"commerce script unit generation revision is immutable",
		"DROP COLUMN IF EXISTS script_unit_revision",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000059 migration is missing %q", required)
		}
	}
	if err := validateProtectedProviderMigration("000059_commerce_unit_generation_revision.sql", sql); err != nil {
		t.Fatalf("commerce unit generation revision migration writes protected Provider configuration: %v", err)
	}
}
