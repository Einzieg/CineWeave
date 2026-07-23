package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceTimelineIdentityRepairMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000058_commerce_timeline_identity_repair.sql")
	if err != nil {
		t.Fatalf("read commerce timeline identity repair migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION validate_commerce_timeline_overlay_identity()",
		"JOIN commerce_shot_contracts contract",
		"contract.script_unit_id",
		"contract.script_unit_generation_id",
		"shot.organization_id = NEW.organization_id",
		"shot_unit IS NULL",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000058 migration is missing %q", required)
		}
	}
	upSQL := strings.Split(sql, "-- +goose Down")[0]
	if strings.Contains(upSQL, "shot.commerce_script_unit_id") ||
		strings.Contains(upSQL, "shot.commerce_script_unit_generation_id") {
		t.Fatal("000058 up migration still reads Commerce identity from storyboard_shots")
	}
	if err := validateProtectedProviderMigration("000058_commerce_timeline_identity_repair.sql", sql); err != nil {
		t.Fatalf("commerce timeline identity repair migration writes protected Provider configuration: %v", err)
	}
}
