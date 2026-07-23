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
		Source:                CapabilitySourcePreset,
		ApprovalStatus:        CapabilityApprovalApproved,
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
		Source:                CapabilitySourceInferred,
		ApprovalStatus:        CapabilityApprovalInferred,
	}
}

func normalizeProviderOptionsSchemaAsyncTask(providerOptionsSchema, taskTypes json.RawMessage) json.RawMessage {
	return normalizeProviderOptionsSchema(providerOptionsSchema, taskTypes, nil, nil, nil)
}

func normalizeProviderOptionsSchema(providerOptionsSchema, taskTypes, inputLimits, outputLimits, qualityTiers json.RawMessage) json.RawMessage {
	var schema map[string]any
	if err := json.Unmarshal(providerOptionsSchema, &schema); err != nil {
		return providerOptionsSchema
	}
	if schema == nil {
		schema = map[string]any{}
	}
	xCapabilities, ok := schema["xCapabilities"].(map[string]any)
	if !ok {
		xCapabilities = map[string]any{}
		schema["xCapabilities"] = xCapabilities
	}
	taskTypeValues := stringsFromRawJSON(taskTypes)
	inputTypes := uniqueStrings(append(stringsFromJSONField(inputLimits, "inputTypes"), stringsFromAny(xCapabilities["supportedInputTypes"])...))
	outputTypes := uniqueStrings(append(stringsFromJSONField(outputLimits, "outputTypes"), stringsFromAny(xCapabilities["supportedOutputTypes"])...))
	referenceTypes := uniqueStrings(stringsFromAny(xCapabilities["referenceTypes"]))

	if _, ok := xCapabilities["supportsAsyncTask"].(bool); !ok {
		xCapabilities["supportsAsyncTask"] = inferSupportsAsyncTask(taskTypes, xCapabilities)
	}
	if _, ok := xCapabilities["supportsStreaming"].(bool); !ok {
		xCapabilities["supportsStreaming"] = containsNormalizedString(taskTypeValues, TaskTypeTextStream)
	}
	if truthyBool(xCapabilities["supportsStreaming"]) {
		mode, _ := xCapabilities["streamTerminalMode"].(string)
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "done_marker", "finish_reason", "done_or_finish_reason":
		default:
			xCapabilities["streamTerminalMode"] = "done_or_finish_reason"
		}
	}
	if _, ok := xCapabilities["supportsReasoning"].(bool); !ok {
		xCapabilities["supportsReasoning"] = false
	}
	if _, ok := xCapabilities["supportsReasoningLevels"].(bool); !ok {
		xCapabilities["supportsReasoningLevels"] = len(stringsFromAny(xCapabilities["reasoningLevels"])) > 0
	}
	if _, ok := xCapabilities["supportedInputTypes"]; !ok && len(inputTypes) > 0 {
		xCapabilities["supportedInputTypes"] = inputTypes
	}
	if _, ok := xCapabilities["supportedOutputTypes"]; !ok && len(outputTypes) > 0 {
		xCapabilities["supportedOutputTypes"] = outputTypes
	}
	if _, ok := xCapabilities["requestModes"]; !ok {
		xCapabilities["requestModes"] = defaultRequestModes(taskTypeValues)
	}
	supportsText := containsNormalizedString(taskTypeValues, TaskTypeTextGenerate) || containsNormalizedString(taskTypeValues, TaskTypeTextStream)
	supportsImage := containsNormalizedString(taskTypeValues, TaskTypeImageGenerate)
	supportsAudio := containsNormalizedString(taskTypeValues, TaskTypeAudioTTS) || containsNormalizedString(taskTypeValues, TaskTypeAudioTranscribe)
	supportsVideo := containsNormalizedString(taskTypeValues, TaskTypeVideoCreateTask) || containsNormalizedString(taskTypeValues, "video.text_to_video") || containsNormalizedString(taskTypeValues, "video.image_to_video")
	if _, ok := xCapabilities["supportsMultimodalInput"].(bool); !ok {
		xCapabilities["supportsMultimodalInput"] = supportsText && hasNonTextInput(inputTypes)
	}
	if supportsImage {
		if _, ok := xCapabilities["supportsReferences"].(bool); !ok {
			xCapabilities["supportsReferences"] = containsNormalizedString(inputTypes, "image")
		}
		if _, ok := xCapabilities["supportsReferenceImages"].(bool); !ok {
			xCapabilities["supportsReferenceImages"] = containsNormalizedString(inputTypes, "image")
		}
		if _, ok := xCapabilities["maxReferenceImages"]; !ok && truthyBool(xCapabilities["supportsReferenceImages"]) {
			xCapabilities["maxReferenceImages"] = 1
		}
	}
	if supportsVideo {
		if _, ok := xCapabilities["supportsReferenceImages"].(bool); !ok {
			xCapabilities["supportsReferenceImages"] = containsNormalizedString(inputTypes, "image") || containsNormalizedString(referenceTypes, "image")
		}
		if _, ok := xCapabilities["supportsFirstFrame"].(bool); !ok {
			xCapabilities["supportsFirstFrame"] = containsNormalizedString(referenceTypes, "first_frame")
		}
		if _, ok := xCapabilities["supportsLastFrame"].(bool); !ok {
			xCapabilities["supportsLastFrame"] = containsNormalizedString(referenceTypes, "last_frame")
		}
		if _, ok := xCapabilities["supportsVideoReference"].(bool); !ok {
			xCapabilities["supportsVideoReference"] = containsNormalizedString(inputTypes, "video") || containsNormalizedString(referenceTypes, "video")
		}
		if _, ok := xCapabilities["maxReferenceImages"]; !ok && truthyBool(xCapabilities["supportsReferenceImages"]) {
			xCapabilities["maxReferenceImages"] = 1
		}
	}
	if supportsAudio {
		if _, ok := xCapabilities["supportsTTS"].(bool); !ok {
			xCapabilities["supportsTTS"] = containsNormalizedString(taskTypeValues, TaskTypeAudioTTS)
		}
		if _, ok := xCapabilities["supportsTranscription"].(bool); !ok {
			xCapabilities["supportsTranscription"] = containsNormalizedString(taskTypeValues, TaskTypeAudioTranscribe)
		}
		if _, ok := xCapabilities["audioResponseFormats"]; !ok {
			xCapabilities["audioResponseFormats"] = []string{"mp3", "wav", "aac", "flac", "opus"}
		}
	}
	if _, ok := xCapabilities["responseFormats"]; !ok {
		if values := stringsFromJSONField(outputLimits, "responseFormats"); len(values) > 0 {
			xCapabilities["responseFormats"] = values
		}
	}
	if _, ok := xCapabilities["supportedResolutions"]; !ok {
		if values := stringsFromRawJSON(qualityTiers); len(values) > 0 && (supportsImage || supportsVideo) {
			xCapabilities["supportedResolutions"] = values
		}
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

func stringsFromJSONField(raw json.RawMessage, key string) []string {
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return stringsFromAny(decoded[key])
}

func containsNormalizedString(values []string, expected string) bool {
	expected = strings.TrimSpace(strings.ToLower(expected))
	for _, value := range values {
		if strings.TrimSpace(strings.ToLower(value)) == expected {
			return true
		}
	}
	return false
}

func hasNonTextInput(values []string) bool {
	for _, value := range values {
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "", "text":
		default:
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func truthyBool(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func defaultRequestModes(taskTypes []string) []string {
	modes := make([]string, 0, 4)
	add := func(mode string) {
		if mode == "" || containsNormalizedString(modes, mode) {
			return
		}
		modes = append(modes, mode)
	}
	if containsNormalizedString(taskTypes, TaskTypeTextGenerate) || containsNormalizedString(taskTypes, TaskTypeTextStream) {
		add("chat_completions")
	}
	if containsNormalizedString(taskTypes, TaskTypeImageGenerate) {
		add("images.generate")
	}
	if containsNormalizedString(taskTypes, TaskTypeAudioTTS) {
		add("audio.speech")
	}
	if containsNormalizedString(taskTypes, TaskTypeAudioTranscribe) {
		add("audio.transcriptions")
	}
	if containsNormalizedString(taskTypes, TaskTypeVideoCreateTask) {
		add("async_create")
	}
	if containsNormalizedString(taskTypes, TaskTypeVideoPollTask) {
		add("poll")
	}
	if containsNormalizedString(taskTypes, TaskTypeVideoCancelTask) {
		add("cancel")
	}
	return modes
}

func modelSupportsTaskType(model Model, taskType string) bool {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return true
	}
	for _, capability := range model.Capabilities {
		for _, value := range stringsFromRawJSON(capability.TaskTypes) {
			value = strings.TrimSpace(value)
			if value == taskType || (taskType == TaskTypeVideoCreateTask && value == "video.generate") {
				return true
			}
		}
	}
	return false
}

func modelSupportsTextImageInput(model Model) bool {
	if strings.EqualFold(strings.TrimSpace(model.Modality), "multimodal") {
		return true
	}
	for _, capability := range model.Capabilities {
		if capability.ApprovalStatus != CapabilityApprovalApproved {
			continue
		}
		taskTypes := stringsFromRawJSON(capability.TaskTypes)
		if !containsNormalizedString(taskTypes, TaskTypeTextGenerate) &&
			!containsNormalizedString(taskTypes, TaskTypeTextStream) {
			continue
		}
		var schema struct {
			XCapabilities struct {
				SupportsMultimodalInput bool `json:"supportsMultimodalInput"`
			} `json:"xCapabilities"`
		}
		if json.Unmarshal(capability.ProviderOptionsSchema, &schema) == nil &&
			schema.XCapabilities.SupportsMultimodalInput {
			return true
		}
		if containsNormalizedString(stringsFromJSONField(capability.InputLimits, "inputTypes"), "image") {
			return true
		}
	}
	return false
}
