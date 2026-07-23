package dbmigrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommercePromptRegistryMigrationContract(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000049_commerce_prompt_registry.sql")
	required := []string{
		"commerce_language_resolver",
		"commerce_script_localizer",
		"commerce_localization_reviewer",
		"commerce_script_organizer",
		"commerce_storyboard_planner",
		"commerce_storyboard_reviewer",
		"commerce_image_prompt_agent",
		"commerce_image_fidelity_reviewer",
		"commerce_video_prompt_agent",
		"commerce_video_prompt_reviewer",
		"INSERT INTO prompt_templates",
		"INSERT INTO prompt_versions",
		"'maxReviewRounds', 3",
		"'reviewFeedbackMode', 'structured_issues'",
		"'seedMigration', '000049_commerce_prompt_registry'",
		"'sha256:' || encode(public.digest",
		"CREATE FUNCTION protect_commerce_prompt_version()",
		"published commerce prompt versions are immutable",
		"CREATE TRIGGER commerce_prompt_versions_immutable",
		"不得输出 Markdown、解释文字或代码围栏",
		"最多 3 轮",
		"音效或音乐词不得进入旁白",
		"DELETE FROM prompt_versions",
		"DELETE FROM prompt_templates",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000049 migration is missing %q", fragment)
		}
	}
	if count := strings.Count(sql, "md5('cineweave:commerce-prompt-version:' || seed.template_key || ':1')"); count != 1 {
		t.Fatalf("000049 deterministic prompt version identity expression count = %d, want 1", count)
	}
	if err := validateProtectedProviderMigration("000049_commerce_prompt_registry.sql", sql); err != nil {
		t.Fatalf("000049 writes protected Provider configuration: %v", err)
	}
}

func TestCommerceWorkflowV1SeedContract(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000050_commerce_workflow_v1_seed.sql")
	required := []string{
		"commerce_video_v1 requires exactly 10 active prompt contracts",
		"commerce_video_v1 requires published single_frame_i2v profile version 1",
		"'commerce_video_v1'",
		"'10000000-0000-4000-9000-000000000001'::uuid",
		"version.metadata->>'seedMigration' = '000049_commerce_prompt_registry'",
		"'promptVersionId', id",
		"'contentHash', content_hash",
		"INSERT INTO commerce_workflow_template_versions",
		"'published'",
		"public.digest",
		"DELETE FROM commerce_workflow_template_versions",
		"DELETE FROM commerce_workflow_templates",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000050 migration is missing %q", fragment)
		}
	}

	configuration := extractCommerceSeedJSON(t, sql, "configuration_snapshot")
	assertJSONNumber(t, configuration, "reviewPolicy", "maxReviewRounds", 3)
	assertJSONNumber(t, configuration, "reviewPolicy", "maxAutomaticImageRegenerations", 0)
	assertJSONString(t, configuration, "defaults", "aspectRatio", "9:16")
	assertJSONNumber(t, configuration, "defaults", "fpsNumerator", 24)
	assertJSONNumber(t, configuration, "defaults", "timelineTimebase", 90000)

	agentContracts := extractCommerceSeedJSON(t, sql, "agent_model_contracts")
	for _, role := range []string{
		"languageResolver", "scriptLocalizer", "localizationReviewer", "scriptOrganizer",
		"storyboardPlanner", "storyboardReviewer", "imagePromptAgent", "imageFidelityReviewer",
		"videoPromptAgent", "videoPromptReviewer",
	} {
		contract, ok := agentContracts[role].(map[string]any)
		if !ok {
			t.Errorf("agent_model_contracts.%s is missing", role)
			continue
		}
		if contract["profileKey"] != "script_agent_default" || contract["taskType"] != "text.generate" {
			t.Errorf("agent_model_contracts.%s has unexpected routing contract: %#v", role, contract)
		}
	}

	languageContract := extractCommerceSeedJSON(t, sql, "language_contract")
	locales, ok := languageContract["locales"].([]any)
	if !ok || len(locales) != 2 {
		t.Fatalf("language contract locales = %#v, want exactly zh-CN and en-US", languageContract["locales"])
	}
	localeSet := map[string]bool{}
	for _, item := range locales {
		locale, _ := item.(map[string]any)
		localeSet[locale["locale"].(string)] = true
	}
	if !localeSet["zh-CN"] || !localeSet["en-US"] {
		t.Fatalf("language contract locales = %#v, want zh-CN and en-US", localeSet)
	}

	imageContract := extractCommerceSeedJSON(t, sql, "image_capability_contract")
	assertJSONString(t, imageContract, "", "profileKey", "image_generation_default")
	referenceInput, _ := imageContract["referenceInput"].(map[string]any)
	if referenceInput["required"] != true || referenceInput["minimum"] != float64(1) || referenceInput["maximum"] != float64(8) {
		t.Fatalf("image reference contract = %#v", referenceInput)
	}

	videoContract := extractCommerceSeedJSON(t, sql, "video_capability_contract")
	assertJSONString(t, videoContract, "", "profileKey", "video_generation_default")
	profile, _ := videoContract["videoProductionProfile"].(map[string]any)
	if profile["profileKey"] != "single_frame_i2v" || profile["profileVersion"] != float64(1) || profile["profileVersionId"] != "10000000-0000-4000-9000-000000000001" {
		t.Fatalf("video production profile contract = %#v", profile)
	}
	request, _ := videoContract["request"].(map[string]any)
	if request["asyncTaskRequired"] != true || request["firstFrameRequired"] != true || request["maximumReferenceImages"] != float64(1) {
		t.Fatalf("video request contract = %#v", request)
	}

	if err := validateProtectedProviderMigration("000050_commerce_workflow_v1_seed.sql", sql); err != nil {
		t.Fatalf("000050 writes protected Provider configuration: %v", err)
	}
}

func readCommerceSeedMigration(t *testing.T, name string) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve commerce seed test source path")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "..", "..", "db", "migrations", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func extractCommerceSeedJSON(t *testing.T, sql, alias string) map[string]any {
	t.Helper()
	marker := "$json$::jsonb AS " + alias
	end := strings.Index(sql, marker)
	if end < 0 {
		t.Fatalf("JSON seed alias %s not found", alias)
	}
	start := strings.LastIndex(sql[:end], "$json$")
	if start < 0 {
		t.Fatalf("JSON seed start for %s not found", alias)
	}
	raw := strings.TrimSpace(sql[start+len("$json$") : end])
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("parse JSON seed %s: %v", alias, err)
	}
	return value
}

func assertJSONString(t *testing.T, value map[string]any, parent, key, expected string) {
	t.Helper()
	target := value
	if parent != "" {
		var ok bool
		target, ok = value[parent].(map[string]any)
		if !ok {
			t.Fatalf("JSON object %s is missing", parent)
		}
	}
	if target[key] != expected {
		t.Fatalf("JSON %s.%s = %#v, want %q", parent, key, target[key], expected)
	}
}

func assertJSONNumber(t *testing.T, value map[string]any, parent, key string, expected float64) {
	t.Helper()
	target, ok := value[parent].(map[string]any)
	if !ok {
		t.Fatalf("JSON object %s is missing", parent)
	}
	if target[key] != expected {
		t.Fatalf("JSON %s.%s = %#v, want %v", parent, key, target[key], expected)
	}
}
