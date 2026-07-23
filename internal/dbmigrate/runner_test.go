package dbmigrate

import (
	"errors"
	"strings"
	"testing"

	migrationfiles "github.com/Einzieg/cineweave/db/migrations"
)

const currentSchemaMigrationHash = "66803ebb991544fb8fa025e62964820f88fd654ed7171f1bd13815646767fdfc"

func TestValidateEmbedded(t *testing.T) {
	if err := ValidateEmbedded(); err != nil {
		t.Fatalf("ValidateEmbedded() error = %v", err)
	}
}

func TestLatestMigrationIsEmbedded(t *testing.T) {
	migrations, err := loadMigrationFiles()
	if err != nil {
		t.Fatalf("loadMigrationFiles() error = %v", err)
	}
	latest := migrations[len(migrations)-1]
	if latest.Version != 58 || latest.Name != "000058_commerce_timeline_identity_repair.sql" {
		t.Fatalf("latest embedded migration = %d %s", latest.Version, latest.Name)
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

func TestProductionMigrationPolicyRejectsEveryDestructiveCommand(t *testing.T) {
	for _, command := range []string{"down", "down-to", "reset"} {
		if err := validateMigrationCommandPolicy("production", command); err == nil {
			t.Fatalf("validateMigrationCommandPolicy(production, %q) accepted a destructive command", command)
		}
	}
	for _, command := range []string{"up", "verify", "status", "version"} {
		if err := validateMigrationCommandPolicy("production", command); err != nil {
			t.Fatalf("validateMigrationCommandPolicy(production, %q) error = %v", command, err)
		}
	}
	if err := validateMigrationCommandPolicy("development", "reset"); err != nil {
		t.Fatalf("development reset policy error = %v", err)
	}
}

func TestProviderModelRollbackPreflightScope(t *testing.T) {
	tests := []struct {
		name                string
		current             int64
		target              int64
		wantDeletionHistory bool
		wantNullRenderPlans bool
	}{
		{name: "forward or no-op", current: 39, target: 39},
		{name: "above provider migrations", current: 39, target: 37},
		{name: "cross deletion rollback", current: 39, target: 36, wantDeletionHistory: true, wantNullRenderPlans: true},
		{name: "down migration 37", current: 37, target: 36, wantDeletionHistory: true, wantNullRenderPlans: true},
		{name: "cross hard delete migration", current: 36, target: 35, wantNullRenderPlans: true},
		{name: "below provider migrations", current: 35, target: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deletionHistory, nullRenderPlans := providerModelRollbackPreflightScope(test.current, test.target)
			if deletionHistory != test.wantDeletionHistory || nullRenderPlans != test.wantNullRenderPlans {
				t.Fatalf(
					"providerModelRollbackPreflightScope(%d, %d) = (%t, %t), want (%t, %t)",
					test.current,
					test.target,
					deletionHistory,
					nullRenderPlans,
					test.wantDeletionHistory,
					test.wantNullRenderPlans,
				)
			}
		})
	}
}

func TestProviderModelRollbackPreflightErrorIsStableAndNonSensitive(t *testing.T) {
	err := &ProviderModelRollbackPreflightError{
		CurrentVersion:        39,
		TargetVersion:         36,
		TombstoneCount:        2,
		ConflictingModelCount: 1,
		NullRenderPlanCount:   3,
	}
	if !errors.Is(err, ErrProviderModelRollbackUnsafe) {
		t.Fatalf("errors.Is(%v, ErrProviderModelRollbackUnsafe) = false", err)
	}
	message := err.Error()
	for _, expected := range []string{"39 -> 36", "tombstones=2", "conflictingModelKeys=1", "nullRenderPlans=3", "forward migration"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("preflight error %q does not contain %q", message, expected)
		}
	}
}

func TestMigrationContentHashIgnoresPlatformLineEndings(t *testing.T) {
	lf := []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 2;\n")
	crlf := []byte("-- +goose Up\r\nSELECT 1;\r\n-- +goose Down\r\nSELECT 2;\r\n")
	if migrationContentHash(lf) != migrationContentHash(crlf) {
		t.Fatalf("migration hash differs by line ending: lf=%s crlf=%s", migrationContentHash(lf), migrationContentHash(crlf))
	}
}

func TestCurrentSchemaMigrationIsImmutable(t *testing.T) {
	content, err := migrationfiles.FS.ReadFile("000001_current_schema.sql")
	if err != nil {
		t.Fatalf("read current schema migration: %v", err)
	}
	if got := migrationContentHash(content); got != currentSchemaMigrationHash {
		t.Fatalf("000001_current_schema.sql hash = %s, want %s; add a forward migration instead of modifying the applied baseline", got, currentSchemaMigrationHash)
	}
}

func TestProtectedProviderMigrationRejectsConfigurationWrites(t *testing.T) {
	statements := []string{
		"INSERT INTO provider_accounts(id) VALUES ('account-1');",
		"UPDATE provider_models SET status = 'disabled';",
		"DELETE FROM model_profile_bindings;",
		"TRUNCATE TABLE provider_call_logs, provider_credentials;",
		"DROP TABLE IF EXISTS provider_endpoints;",
		"ALTER TABLE provider_model_capabilities DROP COLUMN capabilities;",
		"ALTER TABLE provider_limit_policies RENAME TO discarded_limits;",
	}
	for _, statement := range statements {
		t.Run(strings.Fields(statement)[0]+statement, func(t *testing.T) {
			if err := validateProtectedProviderMigration("000009_test.sql", "-- +goose Up\n"+statement+"\n-- +goose Down\nSELECT 1;"); err == nil {
				t.Fatalf("guard accepted protected Provider write: %s", statement)
			}
		})
	}
}

func TestProtectedProviderMigrationAllowsAdditiveSchemaAndQuotedPromptText(t *testing.T) {
	content := `-- +goose Up
ALTER TABLE provider_model_capabilities ADD COLUMN IF NOT EXISTS native_audio_contract jsonb;
INSERT INTO prompt_versions(content) VALUES ('UPDATE provider_models SET status = ''disabled'';');
DO $body$
BEGIN
  RAISE NOTICE 'DELETE FROM provider_accounts';
END
$body$;
-- UPDATE provider_credentials SET encrypted_payload = '';
-- +goose Down
ALTER TABLE provider_model_capabilities DROP COLUMN IF EXISTS native_audio_contract;
`
	if err := validateProtectedProviderMigration("000009_test.sql", content); err == nil {
		t.Fatal("guard must inspect both migration directions and reject destructive Provider schema changes")
	}

	upOnly := strings.Split(content, "-- +goose Down")[0] + "-- +goose Down\nSELECT 1;"
	if err := validateProtectedProviderMigration("000009_test.sql", upOnly); err != nil {
		t.Fatalf("guard rejected additive Provider schema or quoted prompt text: %v", err)
	}
}

func TestProtectedProviderMigrationRejectsWritesInsideExecutableDollarQuotes(t *testing.T) {
	tests := map[string]string{
		"anonymous block": `DO $$
BEGIN
  UPDATE provider_models SET status = 'disabled';
END
$$;`,
		"function body": `CREATE FUNCTION disable_provider_accounts() RETURNS void AS $fn$
BEGIN
  DELETE FROM provider_accounts;
END
$fn$ LANGUAGE plpgsql;`,
		"dynamic execute": `DO $body$
BEGIN
  EXECUTE $sql$UPDATE provider_credentials SET status = 'disabled'$sql$;
END
$body$;`,
	}
	for name, statement := range tests {
		t.Run(name, func(t *testing.T) {
			content := "-- +goose Up\n" + statement + "\n-- +goose Down\nSELECT 1;"
			if err := validateProtectedProviderMigration("000009_test.sql", content); err == nil {
				t.Fatalf("guard accepted protected Provider write inside executable dollar quote:\n%s", statement)
			}
		})
	}
}

func TestProtectedProviderMigrationAllowsDollarQuotedPromptData(t *testing.T) {
	content := `-- +goose Up
INSERT INTO prompt_versions(content)
VALUES ($prompt$UPDATE provider_models SET status = 'disabled';$prompt$);
-- +goose Down
SELECT 1;
`
	if err := validateProtectedProviderMigration("000009_test.sql", content); err != nil {
		t.Fatalf("guard rejected dollar-quoted prompt data: %v", err)
	}
}

func TestProtectedProviderMigrationAllowsOnlyHardDeleteSnapshotRestore(t *testing.T) {
	allowed := `-- +goose Up
SELECT 1;
-- +goose Down
INSERT INTO provider_models(id, provider_account_id, model_key, display_name, modality, status, created_at, updated_at)
SELECT provider_model_id, provider_account_id, model_key, display_name, modality, 'disabled', created_at, now()
FROM provider_model_deletion_tombstones;`
	if err := validateProtectedProviderMigration("000037_provider_model_deletion_rollback.sql", allowed); err != nil {
		t.Fatalf("guard rejected hard-delete snapshot restore: %v", err)
	}

	tests := map[string]struct {
		name    string
		content string
	}{
		"other migration": {
			name:    "000038_other.sql",
			content: allowed,
		},
		"direct values": {
			name: "000037_provider_model_deletion_rollback.sql",
			content: `-- +goose Up
SELECT 1;
-- +goose Down
INSERT INTO provider_models(id) VALUES (gen_random_uuid());`,
		},
		"additional mutation": {
			name:    "000037_provider_model_deletion_rollback.sql",
			content: allowed + "\nUPDATE provider_models SET status = 'active';",
		},
	}
	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			if err := validateProtectedProviderMigration(test.name, test.content); err == nil {
				t.Fatal("guard accepted an unauthorized Provider configuration write")
			}
		})
	}
}
