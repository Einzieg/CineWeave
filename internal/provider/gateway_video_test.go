package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeGatewayVideoStatus(t *testing.T) {
	tests := map[string]string{
		"pending":     "queued",
		"processing":  "running",
		"in_progress": "running",
		"completed":   "succeeded",
		"done":        "succeeded",
		"error":       "failed",
		"canceled":    "cancelled",
	}
	for input, want := range tests {
		if got := normalizeGatewayVideoStatus(input); got != want {
			t.Fatalf("normalizeGatewayVideoStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSelectVideoEndpointKeys(t *testing.T) {
	manifest := ProviderManifest{Endpoints: map[string]ManifestEndpoint{
		"custom_create": {EndpointType: "async_create"},
		"video_poll":    {EndpointType: "async_poll"},
	}}
	selection := gatewayModelSelection{
		Account: Account{Config: json.RawMessage(`{"videoCreateEndpointKey":"custom_create"}`)},
		Model: Model{Capabilities: []Capability{{
			ProviderOptionsSchema: json.RawMessage(`{"providerOptions":{"videoPollEndpointKey":"video_poll"}}`),
		}}},
	}
	createKey, _, err := selectVideoCreateEndpoint(selection, manifest)
	if err != nil {
		t.Fatalf("selectVideoCreateEndpoint() error = %v", err)
	}
	pollKey, _, err := selectVideoPollEndpoint(selection, manifest, ManifestEndpoint{})
	if err != nil {
		t.Fatalf("selectVideoPollEndpoint() error = %v", err)
	}
	if createKey != "custom_create" || pollKey != "video_poll" {
		t.Fatalf("keys = %s/%s, want custom_create/video_poll", createKey, pollKey)
	}
}

func TestEstimateVideoCostUsesPricingPolicy(t *testing.T) {
	usage := estimateVideoCost(gatewayVideoInput{DurationSeconds: 5, Resolution: "720p"}, nil, []Capability{{
		PricingPolicy: json.RawMessage(`{
			"currency": "USD",
			"videoCostPerSecond": "0.0300",
			"videoCostByResolution": {"720p": "0.0500"},
			"videoCostFlat": "0.2000"
		}`),
	}})
	if usage.Currency != "USD" || usage.EstimatedCost != "0.25000000" {
		t.Fatalf("usage = %+v, want 0.25000000 USD", usage)
	}
}

func TestGatewayVideoStorageKey(t *testing.T) {
	key := gatewayVideoStorageKey("org-1", "project-1", "video/mp4", "")
	if !strings.HasPrefix(key, "org/org-1/project/project-1/provider-videos/") || !strings.HasSuffix(key, ".mp4") {
		t.Fatalf("storage key = %q", key)
	}
	key = gatewayVideoStorageKey("org-1", "", "video/webm", "")
	if !strings.HasPrefix(key, "org/org-1/provider-videos/") || !strings.HasSuffix(key, ".webm") {
		t.Fatalf("storage key without project = %q", key)
	}
}

func TestValidateGatewayVideoURLBlocksPrivateByDefault(t *testing.T) {
	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS", "false")
	if err := validateGatewayVideoURL("http://127.0.0.1/video.mp4"); err == nil {
		t.Fatal("validateGatewayVideoURL() allowed private loopback URL")
	}
}
