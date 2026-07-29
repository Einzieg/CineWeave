package editionmigration

import (
	"io/fs"

	"github.com/Einzieg/cineweave/internal/migrationstream"
)

const (
	DDLContractVersion = "cineweave.ddl-owners.v1"

	CoreStreamID        = "core"
	CoreControlSchema   = "cineweave_migrations"
	CoreLedgerTable     = "cineweave_schema_versions"
	CoreAuditTable      = "cineweave_migration_audit"
	CoreAuditIndex      = "cineweave_migration_audit_version_idx"
	CoreAdvisoryLockKey = int64(0x43494e4557454156)

	CommercialStreamID        = "commercial"
	CommercialControlSchema   = "cineweave_commercial_migrations"
	CommercialLedgerTable     = "schema_versions"
	CommercialAuditTable      = "migration_audit"
	CommercialAuditIndex      = "migration_audit_version_idx"
	CommercialAdvisoryLockKey = int64(0x43494e455745434d)

	CommercialObjectSchema = "cineweave_commercial"
)

func CoreDefinition(
	files fs.FS,
	validate migrationstream.ValidateMigrationFunc,
	beforeDown migrationstream.BeforeDownFunc,
) migrationstream.Definition {
	return migrationstream.Definition{
		ID:                CoreStreamID,
		Files:             files,
		Directory:         ".",
		ControlSchema:     CoreControlSchema,
		LedgerTable:       CoreLedgerTable,
		AuditTable:        CoreAuditTable,
		AuditIndex:        CoreAuditIndex,
		AdvisoryLockKey:   CoreAdvisoryLockKey,
		ValidateMigration: validate,
		BeforeDown:        beforeDown,
	}
}

// CommercialDefinition is the public assembly contract for the private
// Commercial migration binary. The caller must supply its private embedded FS;
// Community binaries never import, enumerate or probe that filesystem.
func CommercialDefinition(files fs.FS) migrationstream.Definition {
	return migrationstream.Definition{
		ID:                CommercialStreamID,
		Files:             files,
		Directory:         ".",
		ControlSchema:     CommercialControlSchema,
		LedgerTable:       CommercialLedgerTable,
		AuditTable:        CommercialAuditTable,
		AuditIndex:        CommercialAuditIndex,
		AdvisoryLockKey:   CommercialAdvisoryLockKey,
		ValidateMigration: ValidateCommercialMigration,
	}
}
