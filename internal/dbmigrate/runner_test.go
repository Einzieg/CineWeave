package dbmigrate

import "testing"

func TestValidateEmbedded(t *testing.T) {
	if err := ValidateEmbedded(); err != nil {
		t.Fatalf("ValidateEmbedded() error = %v", err)
	}
}

func TestProductionEnvironment(t *testing.T) {
	for _, value := range []string{"prod", "production", " Production "} {
		if !IsProduction(value) {
			t.Fatalf("IsProduction(%q) = false", value)
		}
	}
	if IsProduction("development") {
		t.Fatal("development must not be treated as production")
	}
}

func TestDestructiveCommands(t *testing.T) {
	for _, command := range []string{"down", "down-to", "reset"} {
		if !isDestructive(command) {
			t.Fatalf("isDestructive(%q) = false", command)
		}
	}
	if isDestructive("up") {
		t.Fatal("up must not be treated as destructive")
	}
}

func TestMigrationContentHashIgnoresPlatformLineEndings(t *testing.T) {
	lf := []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 2;\n")
	crlf := []byte("-- +goose Up\r\nSELECT 1;\r\n-- +goose Down\r\nSELECT 2;\r\n")
	if migrationContentHash(lf) != migrationContentHash(crlf) {
		t.Fatalf("migration hash differs by line ending: lf=%s crlf=%s", migrationContentHash(lf), migrationContentHash(crlf))
	}
}
