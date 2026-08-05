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

func (s *Service) reconcileModelCapabilityPreset(ctx context.Context, model Model) (Model, error) {
	if model.Status != "active" || strings.TrimSpace(model.ModelKey) == "" {
		return model, nil
	}
	preset, matched, err := s.lookupModelCapabilityPreset(ctx, s.db, model.ModelKey)
	if err != nil || !matched {
		return model, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Model{}, err
	}
	defer tx.Rollback(ctx)

	var currentModelKey, currentDisplayName, currentModality, currentStatus string
	if err := tx.QueryRow(ctx, `
		SELECT model_key, display_name, modality, status
		FROM provider_models
		WHERE id = $1
		FOR UPDATE
	`, model.ID).Scan(&currentModelKey, &currentDisplayName, &currentModality, &currentStatus); err != nil {
		return Model{}, err
	}
	if currentStatus != "active" || !strings.EqualFold(strings.TrimSpace(currentModelKey), strings.TrimSpace(model.ModelKey)) {
		model.ModelKey = currentModelKey
		model.DisplayName = currentDisplayName
		model.Modality = currentModality
		model.Status = currentStatus
		return model, nil
	}
	model.DisplayName = currentDisplayName
	model.Modality = currentModality
	model.Status = currentStatus

	rows, err := tx.Query(ctx, `
		SELECT provider_options_schema
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
		FOR UPDATE
	`, model.ID)
	if err != nil {
		return Model{}, err
	}
	hasCapabilities := false
	replaceable := true
	for rows.Next() {
		hasCapabilities = true
		var providerOptionsSchema []byte
		if err := rows.Scan(&providerOptionsSchema); err != nil {
			rows.Close()
			return Model{}, err
		}
		if !capabilitySourceAllowsPreset(providerOptionsSchema) {
			replaceable = false
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Model{}, err
	}
	rows.Close()
	if !replaceable {
		return model, nil
	}

	capability, err := normalizeCapabilityInput(preset.capabilityInput())
	if err != nil {
		return Model{}, err
	}
	displayName := model.DisplayName
	if strings.TrimSpace(displayName) == "" || strings.EqualFold(strings.TrimSpace(displayName), strings.TrimSpace(model.ModelKey)) {
		displayName = preset.DisplayName
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_models
		SET display_name = $2,
		    modality = $3,
		    updated_at = CASE
		        WHEN display_name IS DISTINCT FROM $2 OR modality IS DISTINCT FROM $3 THEN now()
		        ELSE updated_at
		    END
		WHERE id = $1
		  AND status = 'active'
	`, model.ID, displayName, preset.Modality); err != nil {
		return Model{}, err
	}
	if !hasCapabilities {
		if _, err := insertCapability(ctx, tx, model.ID, capability); err != nil {
			return Model{}, err
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE provider_model_capabilities
		SET task_types = $2,
		    input_limits = $3,
		    output_limits = $4,
		    quality_tiers = $5,
		    provider_options_schema = $6,
		    pricing_policy = $7
		WHERE provider_model_id = $1
	`, model.ID, capability.TaskTypes, capability.InputLimits, capability.OutputLimits, capability.QualityTiers, capability.ProviderOptionsSchema, capability.PricingPolicy); err != nil {
		return Model{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, err
	}
	model.DisplayName = displayName
	model.Modality = preset.Modality
	return model, nil
}

func capabilitySourceAllowsPreset(providerOptionsSchema json.RawMessage) bool {
	switch capabilityLanguageMetadataFromSchema(providerOptionsSchema).Source {
	case CapabilitySourceOfficial, CapabilitySourceProvider, CapabilitySourceManual:
		return false
	default:
		return true
	}
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
	supportsText := supportsCapabilityTaskFamily(taskTypeValues, "text")
	supportsImage := supportsCapabilityTaskFamily(taskTypeValues, "image")
	supportsAudio := supportsCapabilityTaskFamily(taskTypeValues, "audio")
	supportsVideo := supportsCapabilityTaskFamily(taskTypeValues, "video")
	requestModes := normalizeRequestModesForTaskTypes(stringsFromAny(xCapabilities["requestModes"]), supportsText, supportsImage, supportsVideo, supportsAudio)
	if len(requestModes) == 0 {
		requestModes = defaultRequestModes(taskTypeValues)
	}
	if len(requestModes) > 0 {
		xCapabilities["requestModes"] = requestModes
	} else {
		delete(xCapabilities, "requestModes")
	}

	if _, ok := xCapabilities["supportsAsyncTask"].(bool); !ok {
		xCapabilities["supportsAsyncTask"] = inferSupportsAsyncTask(taskTypes, xCapabilities)
	}
	if supportsText {
		supportsTextStream := containsNormalizedString(taskTypeValues, TaskTypeTextStream)
		if !supportsTextStream {
			xCapabilities["supportsStreaming"] = false
			delete(xCapabilities, "streamTerminalMode")
		} else if _, ok := xCapabilities["supportsStreaming"].(bool); !ok {
			xCapabilities["supportsStreaming"] = true
		}
		if truthyBool(xCapabilities["supportsStreaming"]) {
			mode, _ := xCapabilities["streamTerminalMode"].(string)
			switch strings.ToLower(strings.TrimSpace(mode)) {
			case "done_marker", "finish_reason", "done_or_finish_reason":
			default:
				xCapabilities["streamTerminalMode"] = "done_or_finish_reason"
			}
		}
	} else {
		delete(xCapabilities, "supportsStreaming")
		delete(xCapabilities, "streamTerminalMode")
	}
	if _, ok := xCapabilities["supportsReasoning"].(bool); !ok {
		xCapabilities["supportsReasoning"] = false
	}
	if _, ok := xCapabilities["supportsReasoningLevels"].(bool); !ok {
		xCapabilities["supportsReasoningLevels"] = len(stringsFromAny(xCapabilities["reasoningLevels"])) > 0
	}
	reasoningLevels := uniqueStrings(stringsFromAny(xCapabilities["reasoningLevels"]))
	if truthyBool(xCapabilities["supportsReasoningLevels"]) && len(reasoningLevels) > 0 {
		xCapabilities["reasoningLevels"] = reasoningLevels
		defaultLevel, _ := xCapabilities["defaultReasoningLevel"].(string)
		if !containsNormalizedString(reasoningLevels, defaultLevel) {
			xCapabilities["defaultReasoningLevel"] = preferredCapabilityOption(reasoningLevels)
		}
	}
	if _, ok := xCapabilities["supportedInputTypes"]; !ok && len(inputTypes) > 0 {
		xCapabilities["supportedInputTypes"] = inputTypes
	}
	if _, ok := xCapabilities["supportedOutputTypes"]; !ok && len(outputTypes) > 0 {
		xCapabilities["supportedOutputTypes"] = outputTypes
	}
	if _, ok := xCapabilities["supportsMultimodalInput"].(bool); !ok {
		xCapabilities["supportsMultimodalInput"] = supportsText && hasNonTextInput(inputTypes)
	}
	if supportsImage {
		imageQualities := uniqueStrings(stringsFromAny(xCapabilities["quality"]))
		if len(imageQualities) == 0 {
			imageQualities = semanticImageQualityOptions(stringsFromRawJSON(qualityTiers))
		}
		if len(imageQualities) > 0 {
			xCapabilities["quality"] = imageQualities
			defaultQuality, _ := xCapabilities["defaultQuality"].(string)
			if !containsNormalizedString(imageQualities, defaultQuality) {
				xCapabilities["defaultQuality"] = preferredCapabilityOption(imageQualities)
			}
		} else {
			delete(xCapabilities, "quality")
			delete(xCapabilities, "defaultQuality")
		}
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
	if supportsImage {
		if _, ok := xCapabilities["outputFormats"]; !ok {
			if values := stringsFromJSONField(outputLimits, "outputFormats"); len(values) > 0 {
				xCapabilities["outputFormats"] = values
			}
		}
	}
	if _, ok := xCapabilities["supportedResolutions"]; !ok {
		values := stringsFromRawJSON(qualityTiers)
		if supportsImage {
			values = imageResolutionOptions(values)
		}
		if len(values) > 0 && (supportsImage || supportsVideo) {
			xCapabilities["supportedResolutions"] = values
		}
	}
	removeInactiveCapabilityFamilyFields(xCapabilities, supportsText, supportsImage, supportsVideo, supportsAudio)
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

func supportsCapabilityTaskFamily(taskTypes []string, family string) bool {
	prefix := strings.ToLower(strings.TrimSpace(family)) + "."
	if prefix == "." {
		return false
	}
	for _, taskType := range taskTypes {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(taskType)), prefix) {
			return true
		}
	}
	return false
}

func preferredCapabilityOption(values []string) string {
	for _, preferred := range []string{"auto", "medium", "standard", "low"} {
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value), preferred) {
				return value
			}
		}
	}
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func semanticImageQualityOptions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "auto", "low", "medium", "high", "standard", "hd":
			result = append(result, value)
		}
	}
	return uniqueStrings(result)
}

func imageResolutionOptions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToUpper(strings.TrimSpace(value))
		if normalized == "AUTO" || isPixelDimensions(normalized) || isResolutionTier(normalized) {
			result = append(result, value)
		}
	}
	return uniqueStrings(result)
}

func isPixelDimensions(value string) bool {
	parts := strings.Split(value, "X")
	return len(parts) == 2 && isDigits(parts[0]) && isDigits(parts[1])
}

func isResolutionTier(value string) bool {
	if isDigits(value) {
		return true
	}
	if len(value) < 2 {
		return false
	}
	suffix := value[len(value)-1]
	return (suffix == 'K' || suffix == 'P') && isDigits(value[:len(value)-1])
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func removeInactiveCapabilityFamilyFields(xCapabilities map[string]any, supportsText, supportsImage, supportsVideo, supportsAudio bool) {
	if !supportsText {
		deleteCapabilityFields(xCapabilities,
			"supportsReasoning",
			"supportsReasoningLevels",
			"reasoningLevels",
			"defaultReasoningLevel",
			"supportsMultimodalInput",
		)
	}
	if !supportsImage {
		deleteCapabilityFields(xCapabilities,
			"supportsReferences",
			"quality",
			"defaultQuality",
			"responseFormats",
			"outputFormats",
		)
	}
	if !supportsVideo {
		deleteCapabilityFields(xCapabilities,
			"videoGenerationVariants",
			"supportsFirstFrame",
			"supportsLastFrame",
			"supportsVideoReference",
			"maxReferenceVideos",
		)
	}
	if !supportsAudio {
		deleteCapabilityFields(xCapabilities,
			"supportsTTS",
			"supportsTranscription",
			"audioVoices",
			"audioLanguages",
			"audioInputFormats",
			"audioResponseFormats",
			"audioRequestModes",
			"maxTTSCharacters",
			"maxAudioDurationSeconds",
		)
	}
	if !supportsImage && !supportsVideo {
		deleteCapabilityFields(xCapabilities,
			"supportsReferenceImages",
			"maxReferenceImages",
			"referenceTypes",
			"supportedAspectRatios",
			"supportedResolutions",
		)
	}
}

func normalizeCapabilityLimitsForTaskTypes(taskTypes, inputLimits, outputLimits, qualityTiers json.RawMessage) (json.RawMessage, json.RawMessage, json.RawMessage) {
	taskTypeValues := stringsFromRawJSON(taskTypes)
	supportsText := supportsCapabilityTaskFamily(taskTypeValues, "text")
	supportsImage := supportsCapabilityTaskFamily(taskTypeValues, "image")
	supportsAudio := supportsCapabilityTaskFamily(taskTypeValues, "audio")
	supportsVideo := supportsCapabilityTaskFamily(taskTypeValues, "video")

	input := jsonObjectFromRaw(inputLimits)
	output := jsonObjectFromRaw(outputLimits)
	if !supportsText {
		delete(input, "maxTokens")
		delete(output, "maxTokens")
	}
	if !supportsImage {
		delete(output, "maxImages")
		delete(output, "responseFormats")
		delete(output, "outputFormats")
	}
	if !supportsVideo {
		delete(input, "maxReferenceVideos")
	}
	if !supportsAudio {
		delete(input, "maxTTSCharacters")
		delete(input, "maxAudioDurationSeconds")
		delete(input, "audioFormats")
		delete(output, "audioFormats")
	}
	if !supportsImage && !supportsVideo {
		delete(input, "maxReferenceImages")
		qualityTiers = json.RawMessage(`[]`)
	}
	return mustJSON(input), mustJSON(output), qualityTiers
}

func jsonObjectFromRaw(raw json.RawMessage) map[string]any {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return map[string]any{}
	}
	return values
}

func deleteCapabilityFields(values map[string]any, keys ...string) {
	for _, key := range keys {
		delete(values, key)
	}
}

func normalizeRequestModesForTaskTypes(modes []string, supportsText, supportsImage, supportsVideo, supportsAudio bool) []string {
	result := make([]string, 0, len(modes))
	for _, mode := range uniqueStrings(modes) {
		supported := true
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "images.generate", "images.edit":
			supported = supportsImage
		case "async_create", "async_poll", "poll", "cancel":
			supported = supportsVideo
		case "audio.speech", "audio.transcriptions":
			supported = supportsAudio
		default:
			supported = supportsText || supportsImage || supportsVideo || supportsAudio
		}
		if supported {
			result = append(result, mode)
		}
	}
	return result
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
