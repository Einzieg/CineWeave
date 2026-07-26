package dbmigrate

import (
	"strings"
	"testing"
)

func TestCommerceDirectVideoRuntimeMigrationContract(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000066_commerce_direct_video_runtime.sql")
	for _, fragment := range []string{
		"CREATE TABLE commerce_script_reference_images",
		"CREATE TABLE commerce_script_reference_uploads",
		"CREATE TABLE commerce_direct_video_jobs",
		"CREATE TABLE commerce_direct_video_job_references",
		"commerce_direct_video_jobs_validate_identity",
		"commerce_direct_video_jobs_protect_snapshot",
		"commerce_direct_video_jobs_output_artifact_fk",
		"commerce_direct_video_jobs_output_media_fk",
		"ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE",
		"DROP TABLE IF EXISTS commerce_direct_video_jobs",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000066 migration is missing %q", fragment)
		}
	}
	if count := strings.Count(sql, "-- +goose StatementBegin"); count != 2 {
		t.Fatalf("000066 has %d PL/pgSQL statement boundaries, want 2", count)
	}
	if count := strings.Count(sql, "-- +goose StatementEnd"); count != 2 {
		t.Fatalf("000066 has %d PL/pgSQL statement terminators, want 2", count)
	}
	if err := validateProtectedProviderMigration("000066_commerce_direct_video_runtime.sql", sql); err != nil {
		t.Fatalf("000066 writes protected Provider configuration: %v", err)
	}
}
