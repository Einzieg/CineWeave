package api

import (
	"os"
	"strings"
	"testing"
)

func TestAgentSupervisionDoesNotTreatCostRecordsAsSpendAuthority(t *testing.T) {
	payload, err := os.ReadFile("agent_control.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	if strings.Contains(source, "FROM cost_records") ||
		strings.Contains(source, "agentProjectCostSpentCents") {
		t.Fatal("Agent supervision still derives spend authority from cost_records")
	}
	if !strings.Contains(source, `"authoritative":`) ||
		!strings.Contains(source, `"estimatedCostCents":`) {
		t.Fatal("Agent technical cost estimate is not explicitly non-authoritative")
	}
}
