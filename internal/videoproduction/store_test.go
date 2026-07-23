package videoproduction

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestProfileVersionAvailable(t *testing.T) {
	tests := []struct {
		name string
		item ProfileVersion
		want bool
	}{
		{name: "available", item: ProfileVersion{LifecycleState: LifecyclePublished, ImplementationState: ImplementationAvailable}, want: true},
		{name: "reserved", item: ProfileVersion{LifecycleState: LifecyclePublished, ImplementationState: ImplementationReserved}, want: false},
		{name: "draft", item: ProfileVersion{LifecycleState: "draft", ImplementationState: ImplementationAvailable}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.item.Available(); got != test.want {
				t.Fatalf("Available() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSnapshotHashIsStableForMapOrder(t *testing.T) {
	left, err := json.Marshal(map[string]any{"b": 2, "a": map[string]any{"y": true, "x": "same"}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(map[string]any{"a": map[string]any{"x": "same", "y": true}, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(left) != hashBytes(right) {
		t.Fatalf("canonical JSON hashes differ: %s != %s", hashBytes(left), hashBytes(right))
	}
}

func TestCanonicalJSONSurvivesJSONBObjectReordering(t *testing.T) {
	type nestedSnapshot struct {
		SchemaVersion int    `json:"schemaVersion"`
		VideoRatio    string `json:"videoRatio"`
	}
	created, err := canonicalJSON(map[string]any{
		"profileKey":              "single_frame_i2v",
		"productionConfiguration": nestedSnapshot{SchemaVersion: 2, VideoRatio: "9:16"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := canonicalJSON(json.RawMessage(
		`{"productionConfiguration": {"videoRatio": "9:16", "schemaVersion": 2}, "profileKey": "single_frame_i2v"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if string(created) != string(loaded) {
		t.Fatalf("canonical JSON differs after JSONB-style reordering:\ncreated: %s\nloaded:  %s", created, loaded)
	}
	if hashBytes(created) != hashBytes(loaded) {
		t.Fatalf("canonical hashes differ after JSONB-style reordering: %s != %s", hashBytes(created), hashBytes(loaded))
	}
}

func TestCompatibilityPolicy(t *testing.T) {
	for _, value := range []string{CompatibilityStrict, CompatibilityCompatibleFallback} {
		if err := validateCompatibilityPolicy(value); err != nil {
			t.Fatalf("validateCompatibilityPolicy(%q): %v", value, err)
		}
	}
	if err := validateCompatibilityPolicy("silent_fallback"); err == nil {
		t.Fatal("expected invalid policy to fail")
	}
}

func TestDecodeProductionConfigurationRequiresV2Snapshot(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"schemaVersion":1,"productionConfiguration":{"videoRatio":"16:9"}}`),
		json.RawMessage(`{"profile":{"profileKey":"single_frame_i2v"}}`),
	} {
		_, err := DecodeProductionConfiguration(raw)
		var typed Error
		if !errors.As(err, &typed) || typed.Code != CodeConfigurationRebuildRequired {
			t.Fatalf("error = %v, want %s", err, CodeConfigurationRebuildRequired)
		}
	}
}

func TestProductionConfigurationHashCoversManualsModelsAndFrameRate(t *testing.T) {
	base := ProductionConfigurationSnapshot{
		VideoRatio: "16:9", VideoModelProfileKey: "video_generation_default",
		TimelineTimebase: 90_000, FPSNumerator: 24, FPSDenominator: 1,
		ManualBindings: map[string]ManualBindingSnapshot{
			"visual": {PromptVersionID: "visual-v1", TemplateKey: "visual", ContentHash: "hash-v1"},
		},
	}
	_, baseline, err := ProductionConfigurationHash(base)
	if err != nil {
		t.Fatal(err)
	}
	variants := []ProductionConfigurationSnapshot{base, base, base}
	variants[0].VideoModelProfileKey = "video_generation_high_quality"
	variants[1].FPSNumerator = 25
	variants[2].ManualBindings = map[string]ManualBindingSnapshot{
		"visual": {PromptVersionID: "visual-v2", TemplateKey: "visual", ContentHash: "hash-v2"},
	}
	for index, variant := range variants {
		_, hash, err := ProductionConfigurationHash(variant)
		if err != nil {
			t.Fatal(err)
		}
		if hash == baseline {
			t.Fatalf("variant %d did not change production configuration hash", index)
		}
	}
}
