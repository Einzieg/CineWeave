package provider

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRoutingWeightedOrder(t *testing.T) {
	candidates := []RoutingCandidate{
		{ProviderModelID: "zero", Priority: 1, Weight: 0, createdAt: time.Unix(1, 0)},
		{ProviderModelID: "high", Priority: 2, Weight: 90, createdAt: time.Unix(2, 0)},
		{ProviderModelID: "low", Priority: 3, Weight: 10, createdAt: time.Unix(3, 0)},
	}
	ordered := orderWeightedCandidates(candidates, func() float64 { return 0.95 })
	if ordered[0].ProviderModelID != "low" {
		t.Fatalf("first weighted candidate = %s, want low", ordered[0].ProviderModelID)
	}
	if ordered[len(ordered)-1].ProviderModelID != "zero" {
		t.Fatalf("zero weight candidate = %s, want last", ordered[len(ordered)-1].ProviderModelID)
	}
}

func TestRoutingCostOptimizedEstimate(t *testing.T) {
	cheap := []Capability{capabilityWithPricing(t, map[string]any{
		"inputTokenPer1K":  "0.001",
		"outputTokenPer1K": "0.001",
	})}
	expensive := []Capability{capabilityWithPricing(t, map[string]any{
		"inputTokenPer1K":  "0.100",
		"outputTokenPer1K": "0.100",
	})}
	req := RoutingRequest{TaskType: TaskTypeTextGenerate, Modality: "text", EstimatedInputTokens: 1000, MaxOutputTokens: 1000}
	if estimateRoutingCost(req, cheap) >= estimateRoutingCost(req, expensive) {
		t.Fatal("cheap model did not rank below expensive model")
	}
}

func TestRoutingModality(t *testing.T) {
	cases := map[string]string{
		TaskTypeTextGenerate:    "text",
		TaskTypeTextStream:      "text",
		TaskTypeImageGenerate:   "image",
		TaskTypeVideoCreateTask: "video",
	}
	for taskType, want := range cases {
		if got := routingModality(RoutingRequest{TaskType: taskType}); got != want {
			t.Fatalf("routingModality(%s) = %s, want %s", taskType, got, want)
		}
	}
}

func TestValidateRoutingStrategy(t *testing.T) {
	if got, err := validateRoutingStrategy(""); err != nil || got != string(RoutingPriorityWithFallback) {
		t.Fatalf("default routing strategy = %s/%v", got, err)
	}
	if _, err := validateRoutingStrategy("not-a-strategy"); err == nil {
		t.Fatal("validateRoutingStrategy accepted invalid value")
	}
}

func capabilityWithPricing(t *testing.T, policy map[string]any) Capability {
	t.Helper()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	return Capability{PricingPolicy: raw}
}
