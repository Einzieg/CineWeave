package dbmigrate

import (
	"strings"
	"testing"
)

func TestAgentImageAttachmentsMigrationDefinesDurableMediaLinks(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000068_agent_image_attachments.sql")
	for _, fragment := range []string{
		"agent_image_attachments",
		"agent_task_image_attachments",
		"artifact_id UUID REFERENCES artifacts(id) ON DELETE CASCADE",
		"media_file_id UUID REFERENCES media_files(id) ON DELETE CASCADE",
		"UNIQUE (organization_id, idempotency_key)",
		"UNIQUE (project_id, storage_key)",
		"UNIQUE (task_id, ordinal)",
		"'unspecified', 'product_common', 'script_custom', 'visual_reference'",
		"status = 'completed'",
		"content_hash ~ '^[0-9a-f]{64}$'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000068 migration is missing %q", fragment)
		}
	}
	if err := validateProtectedProviderMigration("000068_agent_image_attachments.sql", sql); err != nil {
		t.Fatalf("000068 writes protected Provider configuration: %v", err)
	}
}
