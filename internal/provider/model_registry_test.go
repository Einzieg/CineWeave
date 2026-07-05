package provider

import (
	"encoding/json"
	"testing"
)

func TestModelCapabilityPresetMatchesAliasesAndGlobs(t *testing.T) {
	preset := modelCapabilityPreset{
		PresetKey:     "doubao-seedream-4",
		MatchPatterns: json.RawMessage(`["doubao-seedream-4-*", "volcengine/doubao-seedream-4-*"]`),
	}
	for _, modelKey := range []string{
		"doubao-seedream-4-0-250828",
		"volcengine/doubao-seedream-4-5-251128",
		"VOLCENGINE/DOUBAO-SEEDREAM-4-5-251128",
	} {
		if !modelCapabilityPresetMatches(modelKey, preset) {
			t.Fatalf("modelCapabilityPresetMatches(%q) = false, want true", modelKey)
		}
	}
	if modelCapabilityPresetMatches("doubao-seedream-3-0-250415", preset) {
		t.Fatal("seedream 3 matched seedream 4 preset")
	}
}

func TestModelCapabilityPresetMatchesStructuredPatterns(t *testing.T) {
	preset := modelCapabilityPreset{
		PresetKey: "kling-video",
		MatchPatterns: json.RawMessage(`[
			{"type":"prefix","pattern":"kling-"},
			{"type":"contains","pattern":"video-pro"}
		]`),
	}
	if !modelCapabilityPresetMatches("kuaishou/kling-v2-master", preset) {
		t.Fatal("provider-prefixed kling model did not match prefix pattern")
	}
	if !modelCapabilityPresetMatches("acme-video-pro-1", preset) {
		t.Fatal("video-pro model did not match contains pattern")
	}
	if modelCapabilityPresetMatches("seedance-2", preset) {
		t.Fatal("seedance model unexpectedly matched kling preset")
	}
}

func TestModelSupportsTaskType(t *testing.T) {
	model := Model{
		Capabilities: []Capability{{
			TaskTypes: json.RawMessage(`["text.generate"]`),
		}},
	}
	if !modelSupportsTaskType(model, TaskTypeTextGenerate) {
		t.Fatal("model should support text.generate")
	}
	if modelSupportsTaskType(model, TaskTypeTextStream) {
		t.Fatal("model should not support text.stream")
	}
}

func TestNormalizeProviderOptionsSchemaAsyncTaskInferredFromTaskTypes(t *testing.T) {
	got := normalizeProviderOptionsSchemaAsyncTask(
		json.RawMessage(`{}`),
		json.RawMessage(`["video.create_task","video.poll_task"]`),
	)
	var schema map[string]map[string]bool
	if err := json.Unmarshal(got, &schema); err != nil {
		t.Fatalf("decode normalized provider options: %v", err)
	}
	if !schema["xCapabilities"]["supportsAsyncTask"] {
		t.Fatalf("supportsAsyncTask = false, want true in %s", got)
	}
}

func TestNormalizeProviderOptionsSchemaAsyncTaskPreservesExplicitValue(t *testing.T) {
	got := normalizeProviderOptionsSchemaAsyncTask(
		json.RawMessage(`{"xCapabilities":{"supportsAsyncTask":false,"requestModes":["async_create","poll"]}}`),
		json.RawMessage(`["video.create_task"]`),
	)
	var schema struct {
		XCapabilities struct {
			SupportsAsyncTask bool `json:"supportsAsyncTask"`
		} `json:"xCapabilities"`
	}
	if err := json.Unmarshal(got, &schema); err != nil {
		t.Fatalf("decode normalized provider options: %v", err)
	}
	if schema.XCapabilities.SupportsAsyncTask {
		t.Fatalf("supportsAsyncTask = true, want explicit false preserved in %s", got)
	}
}
