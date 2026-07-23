package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceTimelineRuntimeMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000056_commerce_timeline_runtime.sql")
	if err != nil {
		t.Fatalf("read commerce timeline runtime migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN revision BIGINT NOT NULL DEFAULT 1",
		"CREATE TABLE commerce_timeline_overlays",
		"commerce_timeline_overlays_timeline_fk",
		"commerce_timeline_overlays_generation_fk",
		"validate_commerce_timeline_overlay_identity",
		"role IN ('onscreen_text', 'cta_end_card')",
		"UNIQUE(timeline_id, role, ordinal)",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000056 migration is missing %q", required)
		}
	}
	if err := validateProtectedProviderMigration("000056_commerce_timeline_runtime.sql", sql); err != nil {
		t.Fatalf("commerce timeline migration writes protected Provider configuration: %v", err)
	}
}
