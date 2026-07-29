package editionmigration

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

type ownerManifestFixture struct {
	SchemaVersion string `json:"schemaVersion"`
	Streams       map[string]struct {
		StreamID        string `json:"streamId"`
		ControlSchema   string `json:"controlSchema"`
		LedgerTable     string `json:"ledgerTable"`
		AuditTable      string `json:"auditTable"`
		AuditIndex      string `json:"auditIndex"`
		AdvisoryLockKey string `json:"advisoryLockKey"`
	} `json:"streams"`
	Owners map[string]struct {
		OwnedSchemas             []string `json:"ownedSchemas"`
		MigrationWritableSchemas []string `json:"migrationWritableSchemas"`
	} `json:"owners"`
}

func TestDDLManifestMatchesCompiledStreamIdentity(t *testing.T) {
	content, err := os.ReadFile("../../packages/edition/ddl-owners.v1.json")
	if err != nil {
		t.Fatalf("read DDL owner manifest: %v", err)
	}
	var manifest ownerManifestFixture
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("parse DDL owner manifest: %v", err)
	}
	if manifest.SchemaVersion != DDLContractVersion {
		t.Fatalf("schemaVersion = %q, want %q", manifest.SchemaVersion, DDLContractVersion)
	}
	assertManifestStream(
		t,
		manifest.Streams["core"],
		CoreStreamID,
		CoreControlSchema,
		CoreLedgerTable,
		CoreAuditTable,
		CoreAuditIndex,
		CoreAdvisoryLockKey,
	)
	assertManifestStream(
		t,
		manifest.Streams["commercial"],
		CommercialStreamID,
		CommercialControlSchema,
		CommercialLedgerTable,
		CommercialAuditTable,
		CommercialAuditIndex,
		CommercialAdvisoryLockKey,
	)
	if !containsString(manifest.Owners["core"].OwnedSchemas, "public") {
		t.Fatal("Core owner manifest must own the public schema")
	}
	commercialOwner := manifest.Owners["commercial"]
	if !containsString(commercialOwner.OwnedSchemas, CommercialObjectSchema) ||
		len(commercialOwner.MigrationWritableSchemas) != 1 ||
		commercialOwner.MigrationWritableSchemas[0] != CommercialObjectSchema {
		t.Fatalf("Commercial owner schemas drifted: %#v", commercialOwner)
	}
}

func TestCoreAndCommercialDefinitionsHaveIndependentDatabaseIdentity(t *testing.T) {
	files := fstest.MapFS{
		"000001_fixture.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n")},
	}
	core := CoreDefinition(files, nil, nil)
	commercial := CommercialDefinition(files)

	if core.ID == commercial.ID {
		t.Fatal("Core and Commercial stream IDs must differ")
	}
	if core.ControlSchema == commercial.ControlSchema {
		t.Fatal("Core and Commercial control schemas must differ")
	}
	if core.LedgerTable == commercial.LedgerTable && core.ControlSchema == commercial.ControlSchema {
		t.Fatal("Core and Commercial ledgers must differ")
	}
	if core.AuditTable == commercial.AuditTable && core.ControlSchema == commercial.ControlSchema {
		t.Fatal("Core and Commercial audit tables must differ")
	}
	if core.AdvisoryLockKey == commercial.AdvisoryLockKey {
		t.Fatal("Core and Commercial advisory lock keys must differ")
	}
	if commercial.ValidateMigration == nil {
		t.Fatal("Commercial stream must enforce the DDL owner guard")
	}
}

func assertManifestStream(
	t *testing.T,
	actual struct {
		StreamID        string `json:"streamId"`
		ControlSchema   string `json:"controlSchema"`
		LedgerTable     string `json:"ledgerTable"`
		AuditTable      string `json:"auditTable"`
		AuditIndex      string `json:"auditIndex"`
		AdvisoryLockKey string `json:"advisoryLockKey"`
	},
	streamID,
	controlSchema,
	ledgerTable,
	auditTable,
	auditIndex string,
	advisoryLockKey int64,
) {
	t.Helper()
	if actual.StreamID != streamID ||
		actual.ControlSchema != controlSchema ||
		actual.LedgerTable != ledgerTable ||
		actual.AuditTable != auditTable ||
		actual.AuditIndex != auditIndex ||
		actual.AdvisoryLockKey != strconv.FormatInt(advisoryLockKey, 10) {
		t.Fatalf("manifest stream %#v does not match compiled identity %s", actual, streamID)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestCommercialDDLGuardAllowsCommercialObjectsAndOutboundCoreReferences(t *testing.T) {
	content := []byte(`-- +goose Up
CREATE SCHEMA IF NOT EXISTS cineweave_commercial;
CREATE TABLE cineweave_commercial.billing_accounts (
    id uuid PRIMARY KEY,
    project_id uuid REFERENCES public.projects(id) ON DELETE SET NULL
);
CREATE INDEX billing_accounts_project_idx
    ON cineweave_commercial.billing_accounts(project_id);
CREATE VIEW cineweave_commercial.active_billing_accounts AS
SELECT id FROM cineweave_commercial.billing_accounts;
CREATE FUNCTION cineweave_commercial.touch_account() RETURNS trigger AS $fn$
BEGIN
    NEW.id := NEW.id;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;
CREATE TRIGGER billing_accounts_touch
BEFORE UPDATE ON cineweave_commercial.billing_accounts
FOR EACH ROW EXECUTE FUNCTION cineweave_commercial.touch_account();
-- +goose Down
DROP VIEW IF EXISTS cineweave_commercial.active_billing_accounts;
DROP TABLE IF EXISTS cineweave_commercial.billing_accounts;
DROP FUNCTION IF EXISTS cineweave_commercial.touch_account();
DROP SCHEMA IF EXISTS cineweave_commercial;
`)
	if err := ValidateCommercialMigration("000001_fixture.sql", content); err != nil {
		t.Fatalf("ValidateCommercialMigration() error = %v", err)
	}
}

func TestCommercialDDLGuardRejectsCoreAndAmbiguousTargets(t *testing.T) {
	tests := map[string]string{
		"alter core table": `
ALTER TABLE public.projects ADD COLUMN billing_account_id uuid;`,
		"reverse foreign key": `
ALTER TABLE public.projects
ADD CONSTRAINT projects_billing_fk
FOREIGN KEY (id) REFERENCES cineweave_commercial.billing_accounts(project_id);`,
		"drop Core ledger": `
DROP TABLE cineweave_migrations.cineweave_schema_versions;`,
		"truncate multi-target": `
TRUNCATE TABLE cineweave_commercial.billing_accounts, public.projects;`,
		"trigger on Core": `
CREATE TRIGGER billing_capture AFTER INSERT ON public.provider_requests
FOR EACH ROW EXECUTE FUNCTION cineweave_commercial.capture_request();`,
		"index on Core": `
CREATE INDEX projects_billing_idx ON public.projects(id);`,
		"unqualified target": `
CREATE TABLE billing_accounts(id uuid);`,
		"runner-owned Commercial ledger": `
ALTER TABLE cineweave_commercial_migrations.schema_versions ADD COLUMN forged boolean;`,
		"search path": `
SET search_path = cineweave_commercial;
CREATE TABLE billing_accounts(id uuid);`,
		"dynamic execute": `
DO $body$
BEGIN
    EXECUTE 'ALTER TABLE public.projects ADD COLUMN billing_account_id uuid';
END
$body$;`,
		"DDL inside function body": `
CREATE FUNCTION cineweave_commercial.bad_function() RETURNS void AS $fn$
BEGIN
    ALTER TABLE public.projects ADD COLUMN billing_account_id uuid;
END
$fn$ LANGUAGE plpgsql;`,
		"quoted Core target": `
ALTER TABLE "public"."projects" ADD COLUMN billing_account_id uuid;`,
	}
	for name, statement := range tests {
		t.Run(name, func(t *testing.T) {
			content := []byte("-- +goose Up\n" + strings.TrimSpace(statement) + "\n-- +goose Down\nSELECT 1;\n")
			if err := ValidateCommercialMigration("000001_invalid.sql", content); err == nil {
				t.Fatalf("owner guard accepted:\n%s", statement)
			}
		})
	}
}

func TestCommercialDDLGuardIgnoresQuotedPromptData(t *testing.T) {
	content := []byte(`-- +goose Up
CREATE SCHEMA IF NOT EXISTS cineweave_commercial;
CREATE TABLE cineweave_commercial.prompts (
    body text NOT NULL DEFAULT 'ALTER TABLE public.projects DROP COLUMN id'
);
INSERT INTO cineweave_commercial.prompts(body)
VALUES ($prompt$DROP TABLE public.projects;$prompt$);
-- +goose Down
DROP TABLE cineweave_commercial.prompts;
DROP SCHEMA cineweave_commercial;
`)
	if err := ValidateCommercialMigration("000001_prompt_fixture.sql", content); err != nil {
		t.Fatalf("quoted data was treated as executable DDL: %v", err)
	}
}
