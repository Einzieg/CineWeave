package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func normalizeModelProfileBindingRuntimeOptions(model Model, input *ModelProfileBindingRuntimeOptions) (ModelProfileBindingRuntimeOptions, error) {
	if input == nil {
		return ModelProfileBindingRuntimeOptions{}, nil
	}
	options := ModelProfileBindingRuntimeOptions{
		ReasoningLevel: strings.TrimSpace(input.ReasoningLevel),
	}
	if options.ReasoningLevel == "" {
		return options, nil
	}
	level, err := validateModelReasoningLevel(model, options.ReasoningLevel)
	if err != nil {
		return ModelProfileBindingRuntimeOptions{}, err
	}
	options.ReasoningLevel = level
	return options, nil
}

func decodeModelProfileBindingRuntimeOptions(raw []byte) (ModelProfileBindingRuntimeOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return ModelProfileBindingRuntimeOptions{}, nil
	}
	var options ModelProfileBindingRuntimeOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return ModelProfileBindingRuntimeOptions{}, fmt.Errorf("decode model profile binding runtime options: %w", err)
	}
	options.ReasoningLevel = strings.TrimSpace(options.ReasoningLevel)
	return options, nil
}

func encodeModelProfileBindingRuntimeOptions(options ModelProfileBindingRuntimeOptions) (json.RawMessage, error) {
	raw, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("encode model profile binding runtime options: %w", err)
	}
	return raw, nil
}

func modelReasoningLevels(model Model) []string {
	levels := make([]string, 0)
	seen := map[string]struct{}{}
	for _, capability := range model.Capabilities {
		var schema map[string]any
		if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
			continue
		}
		values := schema
		if nested, ok := schema["xCapabilities"].(map[string]any); ok {
			values = nested
		}
		if supported, declared := values["supportsReasoningLevels"].(bool); declared && !supported {
			continue
		}
		for _, level := range stringsFromAny(values["reasoningLevels"]) {
			level = strings.TrimSpace(level)
			key := strings.ToLower(level)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			levels = append(levels, level)
		}
	}
	return levels
}

func validateModelReasoningLevel(model Model, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", nil
	}
	levels := modelReasoningLevels(model)
	for _, level := range levels {
		if strings.EqualFold(level, requested) {
			return level, nil
		}
	}
	if len(levels) == 0 {
		return "", fmt.Errorf("%w: provider model does not declare configurable reasoning levels", ErrValidation)
	}
	return "", fmt.Errorf("%w: reasoning level %q is not supported; allowed values: %s", ErrValidation, requested, strings.Join(levels, ", "))
}

func applyTextRuntimeOptions(input json.RawMessage, model Model, options ModelProfileBindingRuntimeOptions) (json.RawMessage, error) {
	var decoded map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return nil, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
		}
	}
	if decoded == nil {
		decoded = map[string]any{}
	}

	requested := ""
	explicit := false
	for _, key := range []string{"reasoningLevel", "reasoningEffort", "reasoning_effort"} {
		value, ok := decoded[key]
		if !ok {
			continue
		}
		explicit = true
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: input.%s must be a string", ErrValidation, key)
		}
		requested = strings.TrimSpace(text)
		break
	}
	if !explicit {
		requested = strings.TrimSpace(options.ReasoningLevel)
	}
	if requested == "" {
		return input, nil
	}

	level, err := validateModelReasoningLevel(model, requested)
	if err != nil {
		return nil, err
	}
	delete(decoded, "reasoningEffort")
	delete(decoded, "reasoning_effort")
	decoded["reasoningLevel"] = level
	raw, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encode text runtime options: %w", err)
	}
	return raw, nil
}

func validateExistingModelBindingRuntimeOptions(ctx context.Context, tx pgx.Tx, modelID, modality string) error {
	model := Model{ID: modelID, Modality: modality, Capabilities: []Capability{}}
	capabilityRows, err := tx.Query(ctx, `
		SELECT provider_options_schema
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
	`, modelID)
	if err != nil {
		return err
	}
	for capabilityRows.Next() {
		var providerOptionsSchema []byte
		if err := capabilityRows.Scan(&providerOptionsSchema); err != nil {
			capabilityRows.Close()
			return err
		}
		model.Capabilities = append(model.Capabilities, Capability{ProviderOptionsSchema: rawOrDefault(providerOptionsSchema, "{}")})
	}
	if err := capabilityRows.Err(); err != nil {
		capabilityRows.Close()
		return err
	}
	capabilityRows.Close()

	bindingRows, err := tx.Query(ctx, `
		SELECT id, runtime_options
		FROM model_profile_bindings
		WHERE provider_model_id = $1
	`, modelID)
	if err != nil {
		return err
	}
	defer bindingRows.Close()
	for bindingRows.Next() {
		var bindingID string
		var runtimeOptionsRaw []byte
		if err := bindingRows.Scan(&bindingID, &runtimeOptionsRaw); err != nil {
			return err
		}
		options, err := decodeModelProfileBindingRuntimeOptions(runtimeOptionsRaw)
		if err != nil {
			return err
		}
		if _, err := normalizeModelProfileBindingRuntimeOptions(model, &options); err != nil {
			return fmt.Errorf("%w: model capability change invalidates binding %s: %v", ErrValidation, bindingID, err)
		}
	}
	return bindingRows.Err()
}
