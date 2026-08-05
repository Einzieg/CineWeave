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

func TestModelCapabilityPresetMatchesGPTImage2Snapshots(t *testing.T) {
	preset := modelCapabilityPreset{
		PresetKey:     "gpt-image-2",
		MatchPatterns: json.RawMessage(`["gpt-image-2","gpt-image-2-*","openai/gpt-image-2*"]`),
	}
	for _, modelKey := range []string{"gpt-image-2", "gpt-image-2-2026-04-21", "openai/gpt-image-2"} {
		if !modelCapabilityPresetMatches(modelKey, preset) {
			t.Fatalf("modelCapabilityPresetMatches(%q) = false, want true", modelKey)
		}
	}
	if modelCapabilityPresetMatches("gpt-image-1.5", preset) {
		t.Fatal("gpt-image-1.5 unexpectedly matched the GPT Image 2 preset")
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
	var schema struct {
		XCapabilities struct {
			SupportsAsyncTask bool `json:"supportsAsyncTask"`
		} `json:"xCapabilities"`
	}
	if err := json.Unmarshal(got, &schema); err != nil {
		t.Fatalf("decode normalized provider options: %v", err)
	}
	if !schema.XCapabilities.SupportsAsyncTask {
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

func TestNormalizeCapabilityInputAddsTextRuntimeCapabilities(t *testing.T) {
	capability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:   json.RawMessage(`["text.generate","text.stream"]`),
		InputLimits: json.RawMessage(`{"inputTypes":["text","image"]}`),
	})
	if err != nil {
		t.Fatalf("normalize capability: %v", err)
	}
	var schema struct {
		XCapabilities struct {
			SupportsStreaming       bool     `json:"supportsStreaming"`
			StreamTerminalMode      string   `json:"streamTerminalMode"`
			SupportsReasoning       bool     `json:"supportsReasoning"`
			SupportsReasoningLevels bool     `json:"supportsReasoningLevels"`
			SupportsMultimodalInput bool     `json:"supportsMultimodalInput"`
			RequestModes            []string `json:"requestModes"`
			SupportedInputTypes     []string `json:"supportedInputTypes"`
		} `json:"xCapabilities"`
	}
	if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
		t.Fatalf("decode provider options: %v", err)
	}
	if !schema.XCapabilities.SupportsStreaming {
		t.Fatalf("supportsStreaming = false, want true in %s", capability.ProviderOptionsSchema)
	}
	if schema.XCapabilities.StreamTerminalMode != "done_or_finish_reason" {
		t.Fatalf("streamTerminalMode = %q, want done_or_finish_reason", schema.XCapabilities.StreamTerminalMode)
	}
	if schema.XCapabilities.SupportsReasoning {
		t.Fatalf("supportsReasoning = true, want default false in %s", capability.ProviderOptionsSchema)
	}
	if schema.XCapabilities.SupportsReasoningLevels {
		t.Fatalf("supportsReasoningLevels = true, want default false in %s", capability.ProviderOptionsSchema)
	}
	if !schema.XCapabilities.SupportsMultimodalInput {
		t.Fatalf("supportsMultimodalInput = false, want true in %s", capability.ProviderOptionsSchema)
	}
	if !containsString(schema.XCapabilities.RequestModes, "chat_completions") {
		t.Fatalf("requestModes = %#v, want chat_completions", schema.XCapabilities.RequestModes)
	}
	if !containsString(schema.XCapabilities.SupportedInputTypes, "image") {
		t.Fatalf("supportedInputTypes = %#v, want image", schema.XCapabilities.SupportedInputTypes)
	}
}

func TestNormalizeCapabilityInputAddsSelectableCapabilityDefaults(t *testing.T) {
	capability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:    json.RawMessage(`["text.generate","text.stream"]`),
		InputLimits:  json.RawMessage(`{"inputTypes":["text","image","file"]}`),
		QualityTiers: json.RawMessage(`[]`),
		ProviderOptionsSchema: json.RawMessage(`{
			"xCapabilities": {
				"supportsReasoning": true,
				"supportsReasoningLevels": true,
				"reasoningLevels": ["low", "medium", "high"]
			}
		}`),
	})
	if err != nil {
		t.Fatalf("normalize capability: %v", err)
	}
	var schema map[string]map[string]any
	if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
		t.Fatalf("decode provider options: %v", err)
	}
	caps := schema["xCapabilities"]
	if caps["defaultReasoningLevel"] != "medium" {
		t.Fatalf("defaultReasoningLevel = %#v, want medium", caps["defaultReasoningLevel"])
	}
	if caps["supportsMultimodalInput"] != true {
		t.Fatalf("supportsMultimodalInput = %#v, want true", caps["supportsMultimodalInput"])
	}
	if !containsString(stringsFromAny(caps["supportedInputTypes"]), "file") {
		t.Fatalf("supportedInputTypes = %#v, want file", caps["supportedInputTypes"])
	}
}

func TestNormalizeCapabilityInputAddsImageAndVideoLimits(t *testing.T) {
	imageCapability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:    json.RawMessage(`["image.generate"]`),
		InputLimits:  json.RawMessage(`{"inputTypes":["text","image"]}`),
		OutputLimits: json.RawMessage(`{"responseFormats":["url","b64_json"],"outputFormats":["png","webp"]}`),
		QualityTiers: json.RawMessage(`["1024x1024","1792x1024"]`),
	})
	if err != nil {
		t.Fatalf("normalize image capability: %v", err)
	}
	var imageSchema map[string]map[string]any
	if err := json.Unmarshal(imageCapability.ProviderOptionsSchema, &imageSchema); err != nil {
		t.Fatalf("decode image provider options: %v", err)
	}
	imageCaps := imageSchema["xCapabilities"]
	if imageCaps["supportsReferenceImages"] != true || imageCaps["supportsReferences"] != true {
		t.Fatalf("image references not inferred in %s", imageCapability.ProviderOptionsSchema)
	}
	if !containsString(stringsFromAny(imageCaps["responseFormats"]), "b64_json") {
		t.Fatalf("responseFormats = %#v, want b64_json", imageCaps["responseFormats"])
	}
	if !containsString(stringsFromAny(imageCaps["outputFormats"]), "webp") {
		t.Fatalf("outputFormats = %#v, want webp", imageCaps["outputFormats"])
	}
	if !containsString(stringsFromAny(imageCaps["supportedResolutions"]), "1792x1024") {
		t.Fatalf("supportedResolutions = %#v, want 1792x1024", imageCaps["supportedResolutions"])
	}
	if _, exists := imageCaps["defaultQuality"]; exists {
		t.Fatalf("resolution-only tiers must not produce defaultQuality: %#v", imageCaps["defaultQuality"])
	}

	qualityCapability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:    json.RawMessage(`["image.generate"]`),
		QualityTiers: json.RawMessage(`["low","medium","high"]`),
	})
	if err != nil {
		t.Fatalf("normalize image quality capability: %v", err)
	}
	var qualitySchema map[string]map[string]any
	if err := json.Unmarshal(qualityCapability.ProviderOptionsSchema, &qualitySchema); err != nil {
		t.Fatalf("decode image quality provider options: %v", err)
	}
	if qualitySchema["xCapabilities"]["defaultQuality"] != "medium" {
		t.Fatalf("defaultQuality = %#v, want medium", qualitySchema["xCapabilities"]["defaultQuality"])
	}

	videoCapability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:             json.RawMessage(`["video.create_task","video.poll_task","video.cancel_task"]`),
		InputLimits:           json.RawMessage(`{"inputTypes":["text","image","video"]}`),
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"referenceTypes":["first_frame","video"]}}`),
	})
	if err != nil {
		t.Fatalf("normalize video capability: %v", err)
	}
	var videoSchema map[string]map[string]any
	if err := json.Unmarshal(videoCapability.ProviderOptionsSchema, &videoSchema); err != nil {
		t.Fatalf("decode video provider options: %v", err)
	}
	videoCaps := videoSchema["xCapabilities"]
	if videoCaps["supportsAsyncTask"] != true {
		t.Fatalf("supportsAsyncTask = %#v, want true in %s", videoCaps["supportsAsyncTask"], videoCapability.ProviderOptionsSchema)
	}
	if videoCaps["supportsFirstFrame"] != true {
		t.Fatalf("supportsFirstFrame = %#v, want true in %s", videoCaps["supportsFirstFrame"], videoCapability.ProviderOptionsSchema)
	}
	if videoCaps["supportsVideoReference"] != true {
		t.Fatalf("supportsVideoReference = %#v, want true in %s", videoCaps["supportsVideoReference"], videoCapability.ProviderOptionsSchema)
	}
	if !containsString(stringsFromAny(videoCaps["requestModes"]), "cancel") {
		t.Fatalf("requestModes = %#v, want cancel", videoCaps["requestModes"])
	}
}

func TestNormalizeCapabilityInputPreservesVideoGenerateAlias(t *testing.T) {
	capability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:    json.RawMessage(`["video.generate"]`),
		InputLimits:  json.RawMessage(`{"maxReferenceImages":2,"maxReferenceVideos":1}`),
		QualityTiers: json.RawMessage(`["720p"]`),
		ProviderOptionsSchema: json.RawMessage(`{
			"xCapabilities": {
				"requestModes": ["async_create"],
				"supportsFirstFrame": true,
				"videoGenerationVariants": [{"variantKey":"legacy"}]
			}
		}`),
	})
	if err != nil {
		t.Fatalf("normalize video.generate capability: %v", err)
	}

	var inputLimits map[string]any
	if err := json.Unmarshal(capability.InputLimits, &inputLimits); err != nil {
		t.Fatalf("decode input limits: %v", err)
	}
	if inputLimits["maxReferenceImages"] != float64(2) || inputLimits["maxReferenceVideos"] != float64(1) {
		t.Fatalf("video reference limits were removed: %s", capability.InputLimits)
	}
	if !containsString(stringsFromRawJSON(capability.QualityTiers), "720p") {
		t.Fatalf("video quality tiers were removed: %s", capability.QualityTiers)
	}

	var schema map[string]map[string]any
	if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
		t.Fatalf("decode provider options: %v", err)
	}
	videoCaps := schema["xCapabilities"]
	if !containsString(stringsFromAny(videoCaps["requestModes"]), "async_create") {
		t.Fatalf("video request modes were removed: %s", capability.ProviderOptionsSchema)
	}
	if videoCaps["supportsFirstFrame"] != true {
		t.Fatalf("supportsFirstFrame was removed: %s", capability.ProviderOptionsSchema)
	}
	if variants, ok := videoCaps["videoGenerationVariants"].([]any); !ok || len(variants) != 1 {
		t.Fatalf("video variants were removed: %s", capability.ProviderOptionsSchema)
	}
}

func TestNormalizeCapabilityInputRemovesInactiveTaskFamilyFields(t *testing.T) {
	capability, err := normalizeCapabilityInput(CapabilityInput{
		TaskTypes:    json.RawMessage(`["text.generate"]`),
		InputLimits:  json.RawMessage(`{"maxTokens":4096,"maxReferenceImages":4,"maxReferenceVideos":1}`),
		OutputLimits: json.RawMessage(`{"maxTokens":1024,"maxImages":1,"responseFormats":["url"],"outputFormats":["png"]}`),
		QualityTiers: json.RawMessage(`["high"]`),
		ProviderOptionsSchema: json.RawMessage(`{
			"xCapabilities": {
				"quality": ["high"],
				"defaultQuality": "high",
				"supportsReferences": true,
				"supportsStreaming": true,
				"streamTerminalMode": "done_marker",
				"requestModes": ["images.generate", "async_create", "audio.speech", "chat_completions"],
				"videoGenerationVariants": [{"variantKey":"legacy"}],
				"supportsTTS": true
			}
		}`),
	})
	if err != nil {
		t.Fatalf("normalize capability: %v", err)
	}
	var schema map[string]map[string]any
	if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
		t.Fatalf("decode provider options: %v", err)
	}
	for _, key := range []string{"quality", "defaultQuality", "supportsReferences", "videoGenerationVariants", "supportsTTS"} {
		if _, exists := schema["xCapabilities"][key]; exists {
			t.Fatalf("inactive capability field %s was retained in %s", key, capability.ProviderOptionsSchema)
		}
	}
	requestModes := stringsFromAny(schema["xCapabilities"]["requestModes"])
	if len(requestModes) != 1 || requestModes[0] != "chat_completions" {
		t.Fatalf("requestModes = %#v, want only chat_completions", requestModes)
	}
	if schema["xCapabilities"]["supportsStreaming"] != false {
		t.Fatalf("supportsStreaming = %#v, want false without text.stream task", schema["xCapabilities"]["supportsStreaming"])
	}
	if _, exists := schema["xCapabilities"]["streamTerminalMode"]; exists {
		t.Fatalf("streamTerminalMode was retained without text.stream task in %s", capability.ProviderOptionsSchema)
	}
	var inputLimits map[string]any
	if err := json.Unmarshal(capability.InputLimits, &inputLimits); err != nil {
		t.Fatalf("decode input limits: %v", err)
	}
	if _, exists := inputLimits["maxReferenceImages"]; exists {
		t.Fatalf("inactive image limits were retained in %s", capability.InputLimits)
	}
	var qualityTiers []string
	if err := json.Unmarshal(capability.QualityTiers, &qualityTiers); err != nil {
		t.Fatalf("decode quality tiers: %v", err)
	}
	if len(qualityTiers) != 0 {
		t.Fatalf("inactive quality tiers were retained: %#v", qualityTiers)
	}
}
