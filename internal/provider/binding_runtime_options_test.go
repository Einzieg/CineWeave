package provider

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestApplyTextRuntimeOptionsUsesBindingReasoningLevel(t *testing.T) {
	model := reasoningLevelTestModel("low", "medium", "high")
	input, err := applyTextRuntimeOptions(
		json.RawMessage(`{"prompt":"hello"}`),
		model,
		ModelProfileBindingRuntimeOptions{ReasoningLevel: "medium"},
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

func TestApplyTextRuntimeOptionsRequestOverridesBinding(t *testing.T) {
	model := reasoningLevelTestModel("low", "medium", "high")
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
	model := reasoningLevelTestModel("low", "medium", "high")
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

func reasoningLevelTestModel(levels ...string) Model {
	return Model{
		Modality: "text",
		Capabilities: []Capability{{
			ProviderOptionsSchema: mustJSON(map[string]any{
				"xCapabilities": map[string]any{
					"supportsReasoning":       true,
					"supportsReasoningLevels": true,
					"reasoningLevels":         levels,
				},
			}),
		}},
	}
}
