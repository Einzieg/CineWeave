package provider

import (
	"encoding/json"
	"testing"
)

func TestMissingRequiredSetupFields(t *testing.T) {
	raw := json.RawMessage(`{
		"fields": [
			{"key": "imageGenerationPath", "required": true},
			{"key": "cancelTaskPath", "required": false}
		]
	}`)
	missing := missingRequiredSetupFields(raw, map[string]any{"cancelTaskPath": "/cancel"})
	if len(missing) != 1 || missing[0] != "imageGenerationPath" {
		t.Fatalf("missing = %#v, want imageGenerationPath", missing)
	}
	if got := missingRequiredSetupFields(raw, map[string]any{"imageGenerationPath": "/images"}); len(got) != 0 {
		t.Fatalf("missing = %#v, want none", got)
	}
}

func TestCatalogInstallModelsFromTemplates(t *testing.T) {
	entry := CatalogEntry{
		ModelTemplates: json.RawMessage(`[
			{
				"modelKey": "deepseek-chat",
				"displayName": "DeepSeek Chat",
				"modality": "text",
				"taskTypes": ["text.generate", "text.stream"],
				"inputLimits": {"maxTokens": 128000},
				"qualityTiers": ["standard"],
				"providerOptionsSchema": {"type": "object"}
			}
		]`),
	}
	models, err := catalogInstallModels(entry, nil)
	if err != nil {
		t.Fatalf("catalogInstallModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if models[0].ModelKey != "deepseek-chat" || models[0].Modality != "text" {
		t.Fatalf("model = %+v", models[0])
	}
	var inputLimits map[string]any
	if err := json.Unmarshal(models[0].InputLimits, &inputLimits); err != nil {
		t.Fatalf("decode inputLimits: %v", err)
	}
	if inputLimits["maxTokens"] != float64(128000) {
		t.Fatalf("inputLimits = %#v", inputLimits)
	}
}

func TestNormalizeCatalogInstallModelMergesDocumentedCapabilityHints(t *testing.T) {
	supportsJSON := true
	supportsTools := false
	model, err := normalizeCatalogInstallModel(CatalogInstallModel{
		ModelKey:           "example/model",
		DisplayName:        "Example Model",
		Modality:           "text",
		TaskTypes:          []string{"text.generate", "text.stream"},
		ExecutionMode:      "sync",
		SupportsJsonOutput: &supportsJSON,
		SupportsToolCalls:  &supportsTools,
		ProviderOptionsSchema: json.RawMessage(`{
			"type":"object",
			"xCapabilities":{"supportsStreaming":true}
		}`),
	})
	if err != nil {
		t.Fatalf("normalizeCatalogInstallModel() error = %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(model.ProviderOptionsSchema, &schema); err != nil {
		t.Fatalf("decode provider options: %v", err)
	}
	xCapabilities, _ := schema["xCapabilities"].(map[string]any)
	if xCapabilities["executionMode"] != "sync" || xCapabilities["supportsJsonOutput"] != true || xCapabilities["supportsToolCalls"] != false || xCapabilities["supportsStreaming"] != true {
		t.Fatalf("xCapabilities = %#v", xCapabilities)
	}
}
