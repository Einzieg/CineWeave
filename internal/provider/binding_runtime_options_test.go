package provider

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestApplyTextRuntimeOptionsUsesModelDefaultReasoningLevel(t *testing.T) {
	model := reasoningLevelTestModel("medium", "low", "medium", "high")
	input, err := applyTextRuntimeOptions(
		json.RawMessage(`{"prompt":"hello"}`),
		model,
		ModelProfileBindingRuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("applyTextRuntimeOptions() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatalf("decode effective input: %v", err)
	}
	if decoded["reasoningLevel"] != "medium" {
		t.Fatalf("reasoningLevel = %#v, want medium", decoded["reasoningLevel"])
	}
}

func TestApplyTextRuntimeOptionsBindingOverridesModelDefault(t *testing.T) {
	model := reasoningLevelTestModel("medium", "low", "medium", "high")
	input, err := applyTextRuntimeOptions(
		json.RawMessage(`{"prompt":"hello"}`),
		model,
		ModelProfileBindingRuntimeOptions{ReasoningLevel: "low"},
	)
	if err != nil {
		t.Fatalf("applyTextRuntimeOptions() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatalf("decode effective input: %v", err)
	}
	if decoded["reasoningLevel"] != "low" {
		t.Fatalf("reasoningLevel = %#v, want low", decoded["reasoningLevel"])
	}
}

func TestApplyTextRuntimeOptionsRequestOverridesBindingAndModel(t *testing.T) {
	model := reasoningLevelTestModel("medium", "low", "medium", "high")
	input, err := applyTextRuntimeOptions(
		json.RawMessage(`{"prompt":"hello","reasoningEffort":"HIGH"}`),
		model,
		ModelProfileBindingRuntimeOptions{ReasoningLevel: "low"},
	)
	if err != nil {
		t.Fatalf("applyTextRuntimeOptions() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatalf("decode effective input: %v", err)
	}
	if decoded["reasoningLevel"] != "high" {
		t.Fatalf("reasoningLevel = %#v, want high", decoded["reasoningLevel"])
	}
	if _, ok := decoded["reasoningEffort"]; ok {
		t.Fatalf("reasoningEffort alias should be normalized: %#v", decoded)
	}
}

func TestApplyTextRuntimeOptionsRejectsUnsupportedReasoningLevel(t *testing.T) {
	model := reasoningLevelTestModel("", "low", "medium", "high")
	_, err := applyTextRuntimeOptions(
		json.RawMessage(`{"prompt":"hello"}`),
		model,
		ModelProfileBindingRuntimeOptions{ReasoningLevel: "max"},
	)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestNormalizeModelProfileBindingRuntimeOptionsRequiresDeclaredLevels(t *testing.T) {
	_, err := normalizeModelProfileBindingRuntimeOptions(
		Model{Modality: "text", Capabilities: []Capability{{ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"supportsReasoning":true,"supportsReasoningLevels":false,"reasoningLevels":["high"]}}`)}}},
		&ModelProfileBindingRuntimeOptions{ReasoningLevel: "high"},
	)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestNormalizeReasoningCapabilityDefaultsCanonicalizesDeclaredLevel(t *testing.T) {
	normalized, err := normalizeReasoningCapabilityDefaults(json.RawMessage(`{
		"xCapabilities": {
			"supportsReasoning": true,
			"supportsReasoningLevels": true,
			"reasoningLevels": ["low", "medium", "high"],
			"defaultReasoningLevel": "HIGH"
		}
	}`))
	if err != nil {
		t.Fatalf("normalizeReasoningCapabilityDefaults() error = %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(normalized, &schema); err != nil {
		t.Fatalf("decode normalized schema: %v", err)
	}
	xCapabilities := schema["xCapabilities"].(map[string]any)
	if xCapabilities["defaultReasoningLevel"] != "high" {
		t.Fatalf("defaultReasoningLevel = %#v, want high", xCapabilities["defaultReasoningLevel"])
	}
}

func TestNormalizeReasoningCapabilityDefaultsRejectsUndeclaredLevel(t *testing.T) {
	_, err := normalizeReasoningCapabilityDefaults(json.RawMessage(`{
		"xCapabilities": {
			"supportsReasoning": true,
			"supportsReasoningLevels": true,
			"reasoningLevels": ["low", "medium", "high"],
			"defaultReasoningLevel": "max"
		}
	}`))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func reasoningLevelTestModel(defaultLevel string, levels ...string) Model {
	xCapabilities := map[string]any{
		"supportsReasoning":       true,
		"supportsReasoningLevels": true,
		"reasoningLevels":         levels,
	}
	if defaultLevel != "" {
		xCapabilities["defaultReasoningLevel"] = defaultLevel
	}
	return Model{
		Modality: "text",
		Capabilities: []Capability{{
			ProviderOptionsSchema: mustJSON(map[string]any{
				"xCapabilities": xCapabilities,
			}),
		}},
	}
}
