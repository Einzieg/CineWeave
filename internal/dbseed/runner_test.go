package dbseed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
)

func TestValidateEmbedded(t *testing.T) {
	if err := ValidateEmbedded(); err != nil {
		t.Fatalf("ValidateEmbedded() error = %v", err)
	}
}

func TestEmbeddedResourceCoverage(t *testing.T) {
	resources, err := loadResources()
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 6 {
		t.Fatalf("resource count = %d, want 6", len(resources))
	}
	totals := map[string]int{}
	for _, resource := range resources {
		for table, count := range resource.Counts {
			totals[table] += count
		}
	}
	want := map[string]int{
		"permissions":                       53,
		"roles":                             10,
		"role_permissions":                  154,
		"provider_connectors":               2,
		"provider_catalog_entries":          15,
		"provider_model_capability_presets": 49,
		"prompt_templates":                  236,
		"prompt_versions":                   248,
	}
	for table, expected := range want {
		if totals[table] != expected {
			t.Fatalf("%s count = %d, want %d", table, totals[table], expected)
		}
	}
}

func TestModelCapabilityPresetsExposeStructuredEditorDefaults(t *testing.T) {
	resources, err := loadResources()
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, resource := range resources {
		if resource.Data.ResourceKey != "model-capabilities" {
			continue
		}
		found = true
		if resource.Data.ResourceVersion != 4 {
			t.Fatalf("model capabilities version = %d, want 4", resource.Data.ResourceVersion)
		}
		for _, table := range resource.Data.Tables {
			if table.Name != "provider_model_capability_presets" {
				continue
			}
			var rows []struct {
				PresetKey             string          `json:"preset_key"`
				TaskTypes             []string        `json:"task_types"`
				InputLimits           json.RawMessage `json:"input_limits"`
				QualityTiers          []string        `json:"quality_tiers"`
				ProviderOptionsSchema json.RawMessage `json:"provider_options_schema"`
			}
			if err := json.Unmarshal(table.Rows, &rows); err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				var inputLimits struct {
					InputTypes []string `json:"inputTypes"`
				}
				if err := json.Unmarshal(row.InputLimits, &inputLimits); err != nil {
					t.Fatalf("%s input limits: %v", row.PresetKey, err)
				}
				var schema struct {
					XCapabilities struct {
						SupportedInputTypes     []string `json:"supportedInputTypes"`
						SupportsMultimodalInput *bool    `json:"supportsMultimodalInput"`
						SupportsReasoningLevels bool     `json:"supportsReasoningLevels"`
						ReasoningLevels         []string `json:"reasoningLevels"`
						DefaultReasoningLevel   string   `json:"defaultReasoningLevel"`
						Quality                 []string `json:"quality"`
						DefaultQuality          string   `json:"defaultQuality"`
						SupportedResolutions    []string `json:"supportedResolutions"`
					} `json:"xCapabilities"`
				}
				if err := json.Unmarshal(row.ProviderOptionsSchema, &schema); err != nil {
					t.Fatalf("%s provider options: %v", row.PresetKey, err)
				}
				supportsText := containsSeedValue(row.TaskTypes, "text.generate") || containsSeedValue(row.TaskTypes, "text.stream")
				if supportsText {
					if len(inputLimits.InputTypes) == 0 || !sameSeedValues(inputLimits.InputTypes, schema.XCapabilities.SupportedInputTypes) {
						t.Fatalf("%s text input types are not normalized", row.PresetKey)
					}
					wantMultimodal := false
					for _, inputType := range inputLimits.InputTypes {
						if !strings.EqualFold(inputType, "text") {
							wantMultimodal = true
						}
					}
					if schema.XCapabilities.SupportsMultimodalInput == nil || *schema.XCapabilities.SupportsMultimodalInput != wantMultimodal {
						t.Fatalf("%s multimodal flag does not match input types", row.PresetKey)
					}
					if schema.XCapabilities.SupportsReasoningLevels &&
						(!containsSeedValue(schema.XCapabilities.ReasoningLevels, schema.XCapabilities.DefaultReasoningLevel) || schema.XCapabilities.DefaultReasoningLevel == "") {
						t.Fatalf("%s selectable reasoning levels have no valid default", row.PresetKey)
					}
				}
				if containsSeedValue(row.TaskTypes, "image.generate") {
					if len(schema.XCapabilities.Quality) > 0 &&
						!containsSeedValue(schema.XCapabilities.Quality, schema.XCapabilities.DefaultQuality) {
						t.Fatalf("%s declared image quality tiers have no valid default", row.PresetKey)
					}
					if len(schema.XCapabilities.Quality) == 0 && schema.XCapabilities.DefaultQuality != "" {
						t.Fatalf("%s image default quality has no declared quality tiers", row.PresetKey)
					}
					for _, quality := range schema.XCapabilities.Quality {
						if looksLikeSeedResolution(quality) {
							t.Fatalf("%s image resolution %q is stored as a quality tier", row.PresetKey, quality)
						}
					}
					for _, tier := range row.QualityTiers {
						if looksLikeSeedResolution(tier) && !containsSeedValue(schema.XCapabilities.SupportedResolutions, tier) {
							t.Fatalf("%s image resolution %q is missing from supportedResolutions", row.PresetKey, tier)
						}
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("model-capabilities seed resource is missing")
	}
}

func looksLikeSeedResolution(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "AUTO" {
		return false
	}
	if parts := strings.Split(value, "X"); len(parts) == 2 {
		return seedDigits(parts[0]) && seedDigits(parts[1])
	}
	if seedDigits(value) {
		return true
	}
	if len(value) > 1 && (strings.HasSuffix(value, "K") || strings.HasSuffix(value, "P")) {
		return seedDigits(value[:len(value)-1])
	}
	return false
}

func seedDigits(value string) bool {
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

func containsSeedValue(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(expected)) {
			return true
		}
	}
	return false
}

func sameSeedValues(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for _, value := range left {
		if !containsSeedValue(right, value) {
			return false
		}
	}
	return true
}

func TestActiveStoryboardScenePlannerSeedUsesCurrentContract(t *testing.T) {
	resources, err := loadResources()
	if err != nil {
		t.Fatal(err)
	}

	var templateID string
	var activeVersions []struct {
		Version int
		Content string
	}
	for _, resource := range resources {
		if resource.Data.ResourceKey != "prompt-registry" {
			continue
		}
		if resource.Data.ResourceVersion < 3 {
			t.Fatalf("prompt registry version = %d, want at least 3", resource.Data.ResourceVersion)
		}
		for _, table := range resource.Data.Tables {
			switch table.Name {
			case "prompt_templates":
				var rows []struct {
					ID          string `json:"id"`
					TemplateKey string `json:"template_key"`
				}
				if err := json.Unmarshal(table.Rows, &rows); err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row.TemplateKey == "storyboard_scene_planner" {
						templateID = row.ID
						break
					}
				}
			case "prompt_versions":
				var rows []struct {
					TemplateID string `json:"template_id"`
					Status     string `json:"status"`
					Version    int    `json:"version"`
					Content    string `json:"content"`
				}
				if err := json.Unmarshal(table.Rows, &rows); err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row.TemplateID == templateID && row.Status == "active" {
						activeVersions = append(activeVersions, struct {
							Version int
							Content string
						}{Version: row.Version, Content: row.Content})
					}
				}
			}
		}
	}
	if templateID == "" {
		t.Fatal("storyboard_scene_planner template is missing")
	}
	if len(activeVersions) != 1 {
		t.Fatalf("active storyboard scene planner versions = %d, want 1", len(activeVersions))
	}
	active := activeVersions[0]
	if active.Version < 3 {
		t.Fatalf("active storyboard scene planner version = %d, want at least 3", active.Version)
	}
	if strings.Contains(active.Content, "continuityGroupKey") {
		t.Fatal("active storyboard scene planner still requests removed continuityGroupKey")
	}
	if !strings.Contains(active.Content, "禁止自行新增字段") {
		t.Fatal("active storyboard scene planner does not forbid undeclared output fields")
	}
	assertStoryboardPlannerExampleMatchesContract(t, active.Content)
}

func TestCommerceScriptDerivationPromptsAreActiveAndStructured(t *testing.T) {
	resources, err := loadResources()
	if err != nil {
		t.Fatal(err)
	}
	requiredKeys := map[string]bool{
		"commerce_script_derivation_candidate_planner": false,
		"commerce_script_derivation_generator":         false,
		"commerce_script_derivation_reviewer":          false,
		"commerce_script_derivation_reviser":           false,
	}
	templateKeysByID := make(map[string]string)
	activeVersionCount := make(map[string]int)
	for _, resource := range resources {
		if resource.Data.ResourceKey != "prompt-registry" {
			continue
		}
		if resource.Data.ResourceVersion < 5 {
			t.Fatalf("prompt registry version = %d, want at least 5", resource.Data.ResourceVersion)
		}
		for _, table := range resource.Data.Tables {
			switch table.Name {
			case "prompt_templates":
				var rows []struct {
					ID          string `json:"id"`
					TemplateKey string `json:"template_key"`
					TaskType    string `json:"task_type"`
					Status      string `json:"status"`
				}
				if err := json.Unmarshal(table.Rows, &rows); err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if _, ok := requiredKeys[row.TemplateKey]; !ok {
						continue
					}
					if row.TaskType != "text.generate" || row.Status != "active" {
						t.Fatalf("template %s task/status = %s/%s", row.TemplateKey, row.TaskType, row.Status)
					}
					requiredKeys[row.TemplateKey] = true
					templateKeysByID[row.ID] = row.TemplateKey
				}
			case "prompt_versions":
				var rows []struct {
					TemplateID      string          `json:"template_id"`
					Status          string          `json:"status"`
					Content         string          `json:"content"`
					ContentHash     string          `json:"content_hash"`
					Metadata        json.RawMessage `json:"metadata"`
					VariablesSchema json.RawMessage `json:"variables_schema"`
				}
				if err := json.Unmarshal(table.Rows, &rows); err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					key := templateKeysByID[row.TemplateID]
					if key == "" || row.Status != "active" {
						continue
					}
					activeVersionCount[key]++
					if strings.TrimSpace(row.Content) == "" || !strings.HasPrefix(row.ContentHash, "sha256:") {
						t.Fatalf("active prompt %s is missing content or content hash", key)
					}
					var metadata struct {
						OutputContract json.RawMessage `json:"outputContract"`
					}
					if err := json.Unmarshal(row.Metadata, &metadata); err != nil || len(metadata.OutputContract) == 0 {
						t.Fatalf("active prompt %s is missing output contract", key)
					}
					var schema map[string]any
					if err := json.Unmarshal(row.VariablesSchema, &schema); err != nil || schema["type"] != "object" {
						t.Fatalf("active prompt %s is missing variables schema", key)
					}
				}
			}
		}
	}
	for key, found := range requiredKeys {
		if !found {
			t.Fatalf("prompt template %s is missing", key)
		}
		if activeVersionCount[key] != 1 {
			t.Fatalf("active prompt versions for %s = %d, want 1", key, activeVersionCount[key])
		}
	}
}

func assertStoryboardPlannerExampleMatchesContract(t *testing.T, content string) {
	t.Helper()
	const prefix = "只返回合法 JSON：\n"
	start := strings.Index(content, prefix)
	if start < 0 {
		t.Fatal("active storyboard scene planner is missing its JSON example")
	}
	example := content[start+len(prefix):]
	if end := strings.Index(example, "\n\n硬性规则："); end >= 0 {
		example = example[:end]
	}
	var decoded struct {
		SceneKey string           `json:"sceneKey"`
		Shots    []map[string]any `json:"shots"`
	}
	if err := json.Unmarshal([]byte(example), &decoded); err != nil {
		t.Fatalf("decode storyboard scene planner JSON example: %v", err)
	}
	if len(decoded.Shots) != 1 {
		t.Fatalf("storyboard scene planner example shots = %d, want 1", len(decoded.Shots))
	}
	allowed := jsonFieldSet(reflect.TypeOf(storyboardpkg.ShotPlannerSuggestion{}))
	for field := range decoded.Shots[0] {
		if !allowed[field] {
			t.Fatalf("storyboard scene planner example contains undeclared shot field %q", field)
		}
	}
}

func jsonFieldSet(valueType reflect.Type) map[string]bool {
	fields := make(map[string]bool, valueType.NumField())
	for index := 0; index < valueType.NumField(); index++ {
		name := strings.Split(valueType.Field(index).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

func TestSeedContentHashIgnoresPlatformLineEndings(t *testing.T) {
	lf := []byte(`{"resourceKey":"test","resourceVersion":1}` + "\n")
	crlf := []byte(`{"resourceKey":"test","resourceVersion":1}` + "\r\n")
	if seedContentHash(lf) != seedContentHash(crlf) {
		t.Fatalf("seed hash differs by line ending: lf=%s crlf=%s", seedContentHash(lf), seedContentHash(crlf))
	}
}
