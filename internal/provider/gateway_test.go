package provider

import (
	"encoding/json"
	"testing"
)

func TestEstimateTextCostUsesPricingPolicy(t *testing.T) {
	usage := estimateTextCost(GatewayUsage{InputTokens: 1000, OutputTokens: 500}, []Capability{{
		PricingPolicy: json.RawMessage(`{"currency":"USD","inputTokenPer1K":"0.0100","outputTokenPer1K":"0.0200"}`),
	}})
	if usage.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", usage.Currency)
	}
	if usage.TotalTokens != 1500 {
		t.Fatalf("total tokens = %d, want 1500", usage.TotalTokens)
	}
	if usage.EstimatedCost != "0.02000000" {
		t.Fatalf("estimated cost = %q, want 0.02000000", usage.EstimatedCost)
	}
}
