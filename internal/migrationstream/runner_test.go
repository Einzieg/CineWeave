package migrationstream

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidateLoadsConsecutiveImmutableMigrations(t *testing.T) {
	files := fstest.MapFS{
		"000001_first.sql":  &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n")},
		"000002_second.sql": &fstest.MapFile{Data: []byte("-- +goose Up\r\nSELECT 2;\r\n-- +goose Down\r\nSELECT 2;\r\n")},
	}
	validated := make([]string, 0, 2)
	migrations, err := Validate(testDefinition(files, func(name string, _ []byte) error {
		validated = append(validated, name)
		return nil
	}))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(migrations) != 2 || migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Fatalf("migrations = %#v", migrations)
	}
	if strings.Join(validated, ",") != "000001_first.sql,000002_second.sql" {
		t.Fatalf("validator order = %v", validated)
	}
	if migrations[1].Hash != ContentHash([]byte("-- +goose Up\nSELECT 2;\n-- +goose Down\nSELECT 2;\n")) {
		t.Fatal("migration hash was not canonicalized across line endings")
	}
}

func TestValidateRejectsInvalidStreamDefinitionsAndSources(t *testing.T) {
	valid := fstest.MapFS{
		"000001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n")},
	}
	tests := map[string]Definition{
		"missing filesystem": {
			ID: "test", ControlSchema: "test_migrations", LedgerTable: "schema_versions",
			AuditTable: "migration_audit", AuditIndex: "migration_audit_idx", AdvisoryLockKey: 1,
		},
		"invalid schema": {
			ID: "test", Files: valid, ControlSchema: "public;drop", LedgerTable: "schema_versions",
			AuditTable: "migration_audit", AuditIndex: "migration_audit_idx", AdvisoryLockKey: 1,
		},
		"zero lock": {
			ID: "test", Files: valid, ControlSchema: "test_migrations", LedgerTable: "schema_versions",
			AuditTable: "migration_audit", AuditIndex: "migration_audit_idx",
		},
		"gap": testDefinition(fstest.MapFS{
			"000002_second.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 2;\n-- +goose Down\nSELECT 2;\n")},
		}, nil),
		"missing down": testDefinition(fstest.MapFS{
			"000001_first.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n")},
		}, nil),
	}
	for name, definition := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate(definition); err == nil {
				t.Fatal("Validate() accepted an invalid stream")
			}
		})
	}
}

func TestMigrationCommandPolicyIsSharedAcrossStreams(t *testing.T) {
	for _, command := range []string{"down", "down-to", "reset"} {
		if err := ValidateCommandPolicy("production", command); err == nil {
			t.Fatalf("production accepted %q", command)
		}
	}
	for _, command := range []string{"up", "verify", "status", "version"} {
		if err := ValidateCommandPolicy("production", command); err != nil {
			t.Fatalf("production rejected %q: %v", command, err)
		}
	}
}

func testDefinition(files fs.FS, validate ValidateMigrationFunc) Definition {
	return Definition{
		ID:                "test",
		Files:             files,
		Directory:         ".",
		ControlSchema:     "test_migrations",
		LedgerTable:       "schema_versions",
		AuditTable:        "migration_audit",
		AuditIndex:        "migration_audit_idx",
		AdvisoryLockKey:   1,
		ValidateMigration: validate,
	}
}
