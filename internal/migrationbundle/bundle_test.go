package migrationbundle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildProducesCompleteDeterministicBundle(t *testing.T) {
	bundle, manifestJSON, err := Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.MigrationCount == 0 || manifest.HeadVersion != int64(manifest.MigrationCount) {
		t.Fatalf("manifest head/count = %d/%d, want a complete sequence", manifest.HeadVersion, manifest.MigrationCount)
	}
	if got := strings.Count(string(bundle), "\n-- migration 00"); got != manifest.MigrationCount {
		t.Fatalf("bundle migration sections = %d, want %d", got, manifest.MigrationCount)
	}
	if strings.Contains(string(bundle), "-- +goose Down") || strings.Contains(string(bundle), "-- +goose StatementBegin") {
		t.Fatal("bundle contains Goose control directives")
	}
	secondBundle, secondManifest, err := Build()
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if string(bundle) != string(secondBundle) || string(manifestJSON) != string(secondManifest) {
		t.Fatal("Build() output is not deterministic")
	}
}
