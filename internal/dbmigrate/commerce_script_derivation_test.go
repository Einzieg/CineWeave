package dbmigrate

import (
	"strings"
	"testing"
)

func TestCommerceScriptDerivationMigrationDefinesDurableRuntime(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000067_commerce_script_derivation.sql")
	for _, fragment := range []string{
		"next_script_sort_order",
		"commerce_script_derivation_batches",
		"commerce_script_derivation_items",
		"commerce_script_derivation_attempts",
		"commerce_script_derivation_attempt_calls",
		"failed_retryable",
		"retry_of_batch_id",
		"reserved_unit_no",
		"reserved_sort_order",
		"provider_request_id",
		"prompt_version_id",
		"scene_variant",
		"hook_variant",
		"audience_variant",
		"tone_variant",
		"cta_variant",
		"custom_variant",
		"DEFERRABLE INITIALLY DEFERRED",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000067 migration is missing %q", fragment)
		}
	}
	if err := validateProtectedProviderMigration("000067_commerce_script_derivation.sql", sql); err != nil {
		t.Fatalf("000067 writes protected Provider configuration: %v", err)
	}
}
