package dbmigrate

import (
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

func TestCommerceProjectIdentityMigrationContract(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000045_commerce_project_identity.sql")
	if err != nil {
		t.Fatalf("read commerce project identity migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN project_kind TEXT",
		"project_kind IN ('narrative', 'commerce_video')",
		"project_type IN ('short_film', 'comic_drama', 'brand_ad', 'character_ip', 'other')",
		"project_type = 'commerce_video'",
		"content_type IS NULL",
		"CREATE FUNCTION protect_project_kind()",
		"CREATE TABLE commerce_workflow_templates",
		"CREATE TABLE commerce_workflow_template_versions",
		"published commerce workflow template versions are immutable",
		"CREATE TABLE project_commerce_workflow_bindings",
		"configuration_snapshot JSONB NOT NULL",
		"model_routing_snapshot JSONB NOT NULL",
		"capability_snapshot JSONB NOT NULL",
		"project_commerce_workflow_bindings_one_active",
		"ADD COLUMN commerce_workflow_binding_id UUID",
		"CREATE FUNCTION validate_project_video_production_generation_commerce_identity()",
		"commerce and video production bindings do not match",
		"CREATE TABLE commerce_setup_sessions",
		"UNIQUE(organization_id, idempotency_scope, client_request_id)",
		"DROP TABLE IF EXISTS commerce_setup_sessions",
		"DROP TABLE IF EXISTS project_commerce_workflow_bindings",
		"DROP TABLE IF EXISTS commerce_workflow_template_versions",
		"DROP TABLE IF EXISTS commerce_workflow_templates",
		"DROP COLUMN IF EXISTS project_kind",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("000045 migration is missing %q", required)
		}
	}
}

func TestCommerceProjectIdentityMigrationDoesNotWriteProviderConfiguration(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000045_commerce_project_identity.sql")
	if err != nil {
		t.Fatalf("read commerce project identity migration: %v", err)
	}
	if err := validateProtectedProviderMigration("000045_commerce_project_identity.sql", string(content)); err != nil {
		t.Fatalf("commerce project identity migration writes protected Provider configuration: %v", err)
	}
}
