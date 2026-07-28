package dbmigrate

import (
	"strings"
	"testing"
)

func TestCommerceScriptDerivationPromptHashMigrationAcceptsRegistryHashes(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000069_commerce_script_derivation_prompt_hash.sql")
	for _, fragment := range []string{
		"commerce_script_derivation_attempt_calls_hash_check",
		"prompt_hash ~ '^(sha256:)?[0-9a-f]{64}$'",
		"output_content_hash IS NULL OR output_content_hash ~ '^[0-9a-f]{64}$'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000069 migration is missing %q", fragment)
		}
	}
	if err := validateProtectedProviderMigration("000069_commerce_script_derivation_prompt_hash.sql", sql); err != nil {
		t.Fatalf("000069 writes protected Provider configuration: %v", err)
	}
}
