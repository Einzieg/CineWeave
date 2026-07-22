package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSelectGatewayImageReferencesRespectsModelLimitAndOrder(t *testing.T) {
	capabilities := []Capability{{
		InputLimits: json.RawMessage(`{"maxReferenceImages":1}`),
		ProviderOptionsSchema: json.RawMessage(`{
			"xCapabilities": {
				"supportsReferenceImages": true,
				"maxReferenceImages": 3,
				"requestModes": ["images.generate", "images.edit"]
			}
		}`),
	}}
	references := []GatewayImageReference{
		{AssetID: "leader", Metadata: json.RawMessage(`{"referenceKey":"asset_primary:leader"}`)},
		{AssetID: "crowd", Metadata: json.RawMessage(`{"referenceKey":"asset_primary:crowd"}`)},
	}

	selected, err := selectGatewayImageReferences(capabilities, references, true)
	if err != nil {
		t.Fatalf("selectGatewayImageReferences() error = %v", err)
	}
	if len(selected) != 1 || selected[0].AssetID != "leader" {
		t.Fatalf("selected references = %+v, want leader", selected)
	}
}

func TestSelectGatewayImageReferencesRejectsUnsupportedModel(t *testing.T) {
	_, err := selectGatewayImageReferences([]Capability{{
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"supportsReferenceImages":false,"requestModes":["images.generate"]}}`),
	}}, []GatewayImageReference{{AssetID: "leader"}}, true)
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeModelCapabilityUnavailable {
		t.Fatalf("error = %v, want %s", err, CodeModelCapabilityUnavailable)
	}
}

func TestResolveGatewayImageQualityMapsProjectQualityTiers(t *testing.T) {
	capabilities := []Capability{{QualityTiers: json.RawMessage(`["low","medium","high"]`)}}
	tests := []struct {
		requested string
		want      string
	}{
		{requested: "standard", want: "medium"},
		{requested: "hd", want: "high"},
		{requested: "low", want: "low"},
	}
	for _, test := range tests {
		got, err := resolveGatewayImageQuality(test.requested, capabilities)
		if err != nil {
			t.Fatalf("resolveGatewayImageQuality(%q) error = %v", test.requested, err)
		}
		if got != test.want {
			t.Fatalf("resolveGatewayImageQuality(%q) = %q, want %q", test.requested, got, test.want)
		}
	}
}

func TestResolveGatewayImageQualityRejectsUnsupportedTier(t *testing.T) {
	_, err := resolveGatewayImageQuality("ultra", []Capability{{QualityTiers: json.RawMessage(`["low","medium","high"]`)}})
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeModelCapabilityUnavailable {
		t.Fatalf("error = %v, want %s", err, CodeModelCapabilityUnavailable)
	}
}

func TestGatewayImageRequestSnapshotDoesNotPersistSignedReferenceURL(t *testing.T) {
	reference := GatewayImageReference{
		AssetID:    "leader",
		ArtifactID: "artifact-id",
		StorageKey: "references/leader.png",
		URL:        "https://storage.example/private.png?X-Amz-Signature=secret",
		Metadata:   json.RawMessage(`{"referenceKey":"asset_primary:leader"}`),
	}
	snapshot := gatewayImageRequestSnapshot(
		"gpt-image-2",
		json.RawMessage(`{"prompt":"leader close-up"}`),
		[]GatewayImageReference{reference},
		[]GatewayImageReference{reference},
	)
	if string(snapshot) == "" || json.Valid(snapshot) == false {
		t.Fatalf("snapshot is invalid: %s", snapshot)
	}
	if strings.Contains(string(snapshot), "X-Amz-Signature") || strings.Contains(string(snapshot), "secret") {
		t.Fatalf("snapshot leaked signed URL: %s", snapshot)
	}
	var decoded map[string]any
	if err := json.Unmarshal(snapshot, &decoded); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if decoded["requestMode"] != "images.edit" || decoded["referenceCountUsed"] != float64(1) {
		t.Fatalf("snapshot = %#v", decoded)
	}
}
