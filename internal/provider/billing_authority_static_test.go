package provider

import (
	"os"
	"strings"
	"testing"
)

func TestProviderGuardDoesNotUseLocalMonetaryRecordsAsAuthority(t *testing.T) {
	payload, err := os.ReadFile("limits.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	for _, forbidden := range []string{
		"checkBudgetTx",
		"costSpentTx",
		"FROM cost_records",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Provider guard still contains local monetary gate %q", forbidden)
		}
	}
	for _, required := range []string{
		"checkCircuitTx",
		"checkConcurrencyTx",
		"checkRequestRateTx",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Provider guard lost technical protection %q", required)
		}
	}
}
