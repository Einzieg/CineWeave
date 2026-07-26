package dbmigrate

import (
	"strings"
	"testing"
)

func TestCommerceMultilingualWorkflowV2MigrationContract(t *testing.T) {
	sql := readCommerceSeedMigration(t, "000060_commerce_multilingual_workflow_v2.sql")
	required := []string{
		"commerce_language_resolver:2",
		"commerce-workflow-template:commerce_video_v1:2",
		`"confirmationMode": "disabled"`,
		`"mixedLanguagePolicy": "spoken_content_priority"`,
		`"unsupportedLocalePolicy": "fail"`,
		`"locale":"ms-MY"`,
		`"label":"马来语"`,
		"VOICEOVER/NARRATION/旁白/口播语言标注",
		"needsUserConfirmation 必须为 false",
		"'capabilityApprovalRequired', false",
		"'nativeAudioLanguageApprovalRequired', false",
		"INSERT INTO commerce_workflow_template_versions",
		"DELETE FROM commerce_workflow_template_versions",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("000060 migration is missing %q", fragment)
		}
	}
	for _, locale := range []string{
		"zh-CN", "zh-TW", "en-US", "en-GB", "ms-MY", "id-ID", "ja-JP", "ko-KR",
		"th-TH", "vi-VN", "es-ES", "es-MX", "pt-BR", "fr-FR", "de-DE", "it-IT",
		"ru-RU", "ar-SA", "hi-IN", "tr-TR",
	} {
		if !strings.Contains(sql, `"locale":"`+locale+`"`) {
			t.Errorf("000060 migration is missing locale %s", locale)
		}
	}
	if err := validateProtectedProviderMigration("000060_commerce_multilingual_workflow_v2.sql", sql); err != nil {
		t.Fatalf("000060 writes protected Provider configuration: %v", err)
	}
}
