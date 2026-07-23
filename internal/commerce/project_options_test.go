package commerce

import (
	"encoding/json"
	"testing"
)

func TestProjectOptionsFromTemplateNormalizesPublishedContract(t *testing.T) {
	template := WorkflowTemplateVersion{
		ID: "template-version", Version: 3, ContentHash: stringOf("a", 64),
		VideoProfileKey: "single_frame_i2v", VideoProfileVersion: 2,
		ConfigurationSnapshot: json.RawMessage(`{
			"durations":[30,15,30],
			"aspectRatios":["9:16","1:1"],
			"imageQualities":["standard","hd"],
			"languageModes":["auto","explicit"],
			"audioStrategies":["native_av","external_audio"],
			"audioRequirements":["preferred","required"]
		}`),
		LanguageContract: json.RawMessage(`{"locales":[
			{"locale":"zh-cn","label":"简体中文"},
			{"locale":"en-us","label":"English"},
			{"locale":"zh-CN","label":"重复项"},
			{"locale":"invalid_locale","label":"无效项"}
		]}`),
		AgentModelContracts: json.RawMessage(`{
			"languageResolver":{"profileKey":"commerce_language_resolver","label":"语言判断","taskType":"text.generate","modality":"text","usesInputLanguage":true},
			"storyboardPlanner":{"profileKey":"commerce_storyboard_planner","label":"分镜规划","taskType":"text.generate","modality":"text","usesOutputLanguage":true}
		}`),
		ImageCapabilityContract: json.RawMessage(`{"profileKey":"image_generation_default","label":"商品参考图","taskType":"image.generate","modality":"image","usesPromptLanguage":true}`),
		VideoCapabilityContract: json.RawMessage(`{"profileKey":"video_generation_default","label":"镜头视频","taskType":"video.generate","modality":"video","usesPromptLanguage":true,"usesNativeAudio":true}`),
	}

	options, err := projectOptionsFromTemplate(template)
	if err != nil {
		t.Fatalf("projectOptionsFromTemplate() error = %v", err)
	}
	if !options.Available || options.WorkflowTemplateVersionID != template.ID || options.VideoProductionProfileVersion != 2 {
		t.Fatalf("unexpected template identity: %+v", options)
	}
	if len(options.Durations) != 2 || options.Durations[0] != 30 || options.Durations[1] != 15 {
		t.Fatalf("durations were not normalized: %#v", options.Durations)
	}
	if len(options.Languages) != 2 || options.Languages[0].Locale != "zh-CN" || options.Languages[1].Locale != "en-US" {
		t.Fatalf("languages were not normalized: %#v", options.Languages)
	}
	if len(options.ModelRequirements) != 4 {
		t.Fatalf("model requirement count = %d, want 4", len(options.ModelRequirements))
	}
	roles := map[string]ProjectModelRequirement{}
	for _, requirement := range options.ModelRequirements {
		roles[requirement.Role] = requirement
	}
	if !roles["videoGenerator"].UsesNativeAudio || roles["imageGenerator"].ProfileKey != "image_generation_default" {
		t.Fatalf("capability contracts were not merged: %#v", roles)
	}
}

func TestProjectOptionsValidateDraftSelection(t *testing.T) {
	options := ProjectOptions{
		Durations: []int{15, 30}, AspectRatios: []string{"9:16"}, ImageQualities: []string{"standard"},
		LanguageModes: []string{"auto", "explicit"}, Languages: []ProjectLanguageOption{{Locale: "zh-CN"}},
	}
	zh := "zh-cn"
	if err := options.ValidateDraftSelection(30, "9:16", "standard", "explicit", &zh); err != nil {
		t.Fatalf("valid selection rejected: %v", err)
	}
	unsupported := "fr-FR"
	if err := options.ValidateDraftSelection(30, "9:16", "standard", "explicit", &unsupported); errorCode(err) != CodeLanguageUnsupported {
		t.Fatalf("unsupported locale error = %v", err)
	}
	if err := options.ValidateDraftSelection(60, "9:16", "standard", "auto", nil); errorCode(err) != CodeSetupIncomplete {
		t.Fatalf("unsupported duration error = %v", err)
	}
}

func errorCode(err error) string {
	if typed, ok := AsError(err); ok {
		return typed.Code
	}
	return ""
}

func stringOf(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
