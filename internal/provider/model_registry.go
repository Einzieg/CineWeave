package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
)

type modelCapabilityPresetQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type modelCapabilityPreset struct {
	PresetKey             string
	DisplayName           string
	Modality              string
	MatchPatterns         json.RawMessage
	TaskTypes             json.RawMessage
	InputLimits           json.RawMessage
	OutputLimits          json.RawMessage
	QualityTiers          json.RawMessage
	ProviderOptionsSchema json.RawMessage
	PricingPolicy         json.RawMessage
}

func (s *Service) lookupModelCapabilityPreset(ctx context.Context, q modelCapabilityPresetQuerier, modelKey string) (modelCapabilityPreset, bool, error) {
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		return modelCapabilityPreset{}, false, nil
	}
	rows, err := q.Query(ctx, `
		SELECT preset_key, display_name, modality, match_patterns, task_types,
		       input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		FROM provider_model_capability_presets
		WHERE enabled = true
		ORDER BY priority ASC, preset_key ASC
	`)
	if err != nil {
		return modelCapabilityPreset{}, false, err
	}
	defer rows.Close()

	for rows.Next() {
		var preset modelCapabilityPreset
		var matchPatterns, taskTypes, inputLimits, outputLimits, qualityTiers, providerOptionsSchema, pricingPolicy []byte
		if err := rows.Scan(
			&preset.PresetKey,
			&preset.DisplayName,
			&preset.Modality,
			&matchPatterns,
			&taskTypes,
			&inputLimits,
			&outputLimits,
			&qualityTiers,
			&providerOptionsSchema,
			&pricingPolicy,
		); err != nil {
			return modelCapabilityPreset{}, false, err
		}
		preset.MatchPatterns = rawOrDefault(matchPatterns, "[]")
		preset.TaskTypes = rawOrDefault(taskTypes, "[]")
		preset.InputLimits = rawOrDefault(inputLimits, "{}")
		preset.OutputLimits = rawOrDefault(outputLimits, "{}")
		preset.QualityTiers = rawOrDefault(qualityTiers, "[]")
		preset.ProviderOptionsSchema = rawOrDefault(providerOptionsSchema, "{}")
		preset.PricingPolicy = rawOrDefault(pricingPolicy, "{}")
		if modelCapabilityPresetMatches(modelKey, preset) {
			return preset, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return modelCapabilityPreset{}, false, err
	}
	return modelCapabilityPreset{}, false, nil
}

func (preset modelCapabilityPreset) capabilityInput() CapabilityInput {
	return CapabilityInput{
		TaskTypes:             preset.TaskTypes,
		InputLimits:           preset.InputLimits,
		OutputLimits:          preset.OutputLimits,
		QualityTiers:          preset.QualityTiers,
		ProviderOptionsSchema: preset.ProviderOptionsSchema,
		PricingPolicy:         preset.PricingPolicy,
	}
}

func defaultCapabilityInput(modality string) CapabilityInput {
	return CapabilityInput{
		TaskTypes:             mustJSON(discoveredTaskTypes(modality)),
		InputLimits:           json.RawMessage(`{}`),
		OutputLimits:          json.RawMessage(`{}`),
		QualityTiers:          json.RawMessage(`[]`),
		ProviderOptionsSchema: json.RawMessage(`{}`),
		PricingPolicy:         json.RawMessage(`{}`),
	}
}

func normalizeProviderOptionsSchemaAsyncTask(providerOptionsSchema, taskTypes json.RawMessage) json.RawMessage {
	var schema map[string]any
	if err := json.Unmarshal(providerOptionsSchema, &schema); err != nil {
		return providerOptionsSchema
	}
	xCapabilities, ok := schema["xCapabilities"].(map[string]any)
	if !ok {
		xCapabilities = map[string]any{}
		schema["xCapabilities"] = xCapabilities
	}
	if _, ok := xCapabilities["supportsAsyncTask"].(bool); !ok {
		xCapabilities["supportsAsyncTask"] = inferSupportsAsyncTask(taskTypes, xCapabilities)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return providerOptionsSchema
	}
	return raw
}

func inferSupportsAsyncTask(taskTypes json.RawMessage, xCapabilities map[string]any) bool {
	for _, taskType := range stringsFromRawJSON(taskTypes) {
		taskType = strings.TrimSpace(taskType)
		if strings.HasSuffix(taskType, ".create_task") || strings.HasSuffix(taskType, ".poll_task") || strings.HasSuffix(taskType, ".cancel_task") {
			return true
		}
	}
	for _, mode := range stringsFromAny(xCapabilities["requestModes"]) {
		mode = strings.TrimSpace(strings.ToLower(mode))
		if strings.Contains(mode, "async") || mode == "poll" || mode == "async_poll" {
			return true
		}
	}
	return false
}

func applyPresetToCatalogModel(model CatalogInstallModel, preset modelCapabilityPreset) CatalogInstallModel {
	if strings.TrimSpace(model.DisplayName) == "" || strings.EqualFold(strings.TrimSpace(model.DisplayName), strings.TrimSpace(model.ModelKey)) {
		model.DisplayName = preset.DisplayName
	}
	model.Modality = preset.Modality
	model.TaskTypes = stringsFromRawJSON(preset.TaskTypes)
	model.InputLimits = preset.InputLimits
	model.OutputLimits = preset.OutputLimits
	model.QualityTiers = preset.QualityTiers
	model.ProviderOptionsSchema = preset.ProviderOptionsSchema
	model.PricingPolicy = preset.PricingPolicy
	return model
}

func modelCapabilityPresetMatches(modelKey string, preset modelCapabilityPreset) bool {
	candidates := modelMatchCandidates(modelKey)
	presetKey := normalizeModelMatchText(preset.PresetKey)
	for _, candidate := range candidates {
		if candidate == presetKey {
			return true
		}
	}

	var rawPatterns []any
	if err := json.Unmarshal(preset.MatchPatterns, &rawPatterns); err != nil {
		return false
	}
	for _, item := range rawPatterns {
		switch typed := item.(type) {
		case string:
			if modelPatternMatches(typed, candidates, "glob") {
				return true
			}
		case map[string]any:
			pattern, _ := typed["pattern"].(string)
			matchType, _ := typed["type"].(string)
			if modelPatternMatches(pattern, candidates, matchType) {
				return true
			}
		}
	}
	return false
}

func modelPatternMatches(pattern string, candidates []string, matchType string) bool {
	pattern = normalizeModelMatchText(pattern)
	if pattern == "" {
		return false
	}
	matchType = strings.TrimSpace(strings.ToLower(matchType))
	if matchType == "" {
		matchType = "exact"
	}
	for _, candidate := range candidates {
		switch matchType {
		case "prefix":
			if strings.HasPrefix(candidate, pattern) {
				return true
			}
		case "contains":
			if strings.Contains(candidate, pattern) {
				return true
			}
		case "glob":
			if wildcardMatch(pattern, candidate) {
				return true
			}
		default:
			if candidate == pattern {
				return true
			}
		}
	}
	return false
}

func modelMatchCandidates(modelKey string) []string {
	normalized := normalizeModelMatchText(modelKey)
	if normalized == "" {
		return nil
	}
	seen := map[string]struct{}{normalized: {}}
	candidates := []string{normalized}
	add := func(value string) {
		value = normalizeModelMatchText(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 && idx+1 < len(normalized) {
		add(normalized[idx+1:])
	}
	if idx := strings.LastIndex(normalized, ":"); idx >= 0 && idx+1 < len(normalized) {
		add(normalized[idx+1:])
	}
	if idx := strings.LastIndex(normalized, "@"); idx > 0 {
		add(normalized[:idx])
	}
	return candidates
}

func normalizeModelMatchText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func wildcardMatch(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}
	parts := strings.Split(pattern, "*")
	position := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		index := strings.Index(value[position:], part)
		if index < 0 {
			return false
		}
		if i == 0 && !strings.HasPrefix(pattern, "*") && index != 0 {
			return false
		}
		position += index + len(part)
	}
	last := parts[len(parts)-1]
	if last != "" && !strings.HasSuffix(pattern, "*") && !strings.HasSuffix(value, last) {
		return false
	}
	return true
}

func stringsFromRawJSON(raw json.RawMessage) []string {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func stringsFromAny(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func modelSupportsTaskType(model Model, taskType string) bool {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return true
	}
	for _, capability := range model.Capabilities {
		for _, value := range stringsFromRawJSON(capability.TaskTypes) {
			if strings.TrimSpace(value) == taskType {
				return true
			}
		}
	}
	return false
}
