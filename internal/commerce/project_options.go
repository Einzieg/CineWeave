package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type workflowTemplateConfiguration struct {
	Durations         []int    `json:"durations"`
	AspectRatios      []string `json:"aspectRatios"`
	ImageQualities    []string `json:"imageQualities"`
	LanguageModes     []string `json:"languageModes"`
	AudioStrategies   []string `json:"audioStrategies"`
	AudioRequirements []string `json:"audioRequirements"`
}

type workflowTemplateLanguageContract struct {
	Locales []struct {
		Locale string `json:"locale"`
		Label  string `json:"label"`
	} `json:"locales"`
}

type workflowTemplateModelContract struct {
	ProfileKey         string `json:"profileKey"`
	Label              string `json:"label"`
	TaskType           string `json:"taskType"`
	Modality           string `json:"modality"`
	UsesInputLanguage  bool   `json:"usesInputLanguage"`
	UsesOutputLanguage bool   `json:"usesOutputLanguage"`
	UsesPromptLanguage bool   `json:"usesPromptLanguage"`
	UsesNativeAudio    bool   `json:"usesNativeAudio"`
}

func (s *CatalogService) ResolveProjectOptions(ctx context.Context, tx pgx.Tx, organizationID string) (ProjectOptions, error) {
	template, err := s.repository.ResolvePublishedWorkflowTemplate(ctx, tx, organizationID, DefaultWorkflowTemplateKey)
	if err != nil {
		if errors.Is(err, ErrWorkflowTemplateUnavailable) {
			return ProjectOptions{
				Available: false, Blockers: []string{"当前没有已发布的带货视频工作流模板"},
				Durations: []int{15, 30, 60}, AspectRatios: []string{"9:16", "16:9", "1:1"},
				ImageQualities: []string{"standard", "hd"}, LanguageModes: []string{"auto", "explicit"},
				AudioStrategies: []string{"native_av", "external_audio"}, AudioRequirements: []string{"preferred", "required", "disabled"},
				Languages: []ProjectLanguageOption{}, ModelRequirements: []ProjectModelRequirement{},
			}, nil
		}
		return ProjectOptions{}, err
	}
	options, err := projectOptionsFromTemplate(template)
	if err != nil {
		return ProjectOptions{}, err
	}
	return options, nil
}

func projectOptionsFromTemplate(template WorkflowTemplateVersion) (ProjectOptions, error) {
	configuration := workflowTemplateConfiguration{
		Durations: []int{15, 30, 60}, AspectRatios: []string{"9:16", "16:9", "1:1"},
		ImageQualities: []string{"standard", "hd"}, LanguageModes: []string{"auto", "explicit"},
		AudioStrategies: []string{"native_av", "external_audio"}, AudioRequirements: []string{"preferred", "required", "disabled"},
	}
	if len(template.ConfigurationSnapshot) > 0 {
		if err := json.Unmarshal(template.ConfigurationSnapshot, &configuration); err != nil {
			return ProjectOptions{}, Error{Code: CodeSetupIncomplete, Message: "带货视频工作流模板配置无效", Cause: err}
		}
	}
	languageContract := workflowTemplateLanguageContract{}
	if err := json.Unmarshal(template.LanguageContract, &languageContract); err != nil {
		return ProjectOptions{}, Error{Code: CodeSetupIncomplete, Message: "带货视频语言契约无效", Cause: err}
	}
	modelContracts := map[string]workflowTemplateModelContract{}
	if err := json.Unmarshal(template.AgentModelContracts, &modelContracts); err != nil {
		return ProjectOptions{}, Error{Code: CodeSetupIncomplete, Message: "带货视频 Agent 模型契约无效", Cause: err}
	}
	appendContract := func(role string, contract workflowTemplateModelContract) {
		if strings.TrimSpace(contract.ProfileKey) == "" {
			return
		}
		modelContracts[role] = contract
	}
	var imageContract workflowTemplateModelContract
	if err := json.Unmarshal(template.ImageCapabilityContract, &imageContract); err != nil {
		return ProjectOptions{}, Error{Code: CodeSetupIncomplete, Message: "带货视频图片模型契约无效", Cause: err}
	}
	appendContract("imageGenerator", imageContract)
	var videoContract workflowTemplateModelContract
	if err := json.Unmarshal(template.VideoCapabilityContract, &videoContract); err != nil {
		return ProjectOptions{}, Error{Code: CodeSetupIncomplete, Message: "带货视频视频模型契约无效", Cause: err}
	}
	appendContract("videoGenerator", videoContract)

	requirements := make([]ProjectModelRequirement, 0, len(modelContracts))
	roles := make([]string, 0, len(modelContracts))
	for role := range modelContracts {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		contract := modelContracts[role]
		if strings.TrimSpace(contract.ProfileKey) == "" {
			continue
		}
		label := strings.TrimSpace(contract.Label)
		if label == "" {
			label = role
		}
		requirements = append(requirements, ProjectModelRequirement{
			Role: role, Label: label, ProfileKey: strings.TrimSpace(contract.ProfileKey),
			TaskType: strings.TrimSpace(contract.TaskType), Modality: strings.TrimSpace(contract.Modality),
			UsesInputLanguage: contract.UsesInputLanguage, UsesOutputLanguage: contract.UsesOutputLanguage,
			UsesPromptLanguage: contract.UsesPromptLanguage, UsesNativeAudio: contract.UsesNativeAudio,
		})
	}
	languages := make([]ProjectLanguageOption, 0, len(languageContract.Locales))
	seenLocales := map[string]bool{}
	for _, configured := range languageContract.Locales {
		locale, err := normalizeLocale(configured.Locale)
		if err != nil || locale == "" || seenLocales[locale] {
			continue
		}
		seenLocales[locale] = true
		label := strings.TrimSpace(configured.Label)
		if label == "" {
			label = locale
		}
		languages = append(languages, ProjectLanguageOption{Locale: locale, Label: label, Blockers: []string{}})
	}
	blockers := []string{}
	if len(requirements) == 0 {
		blockers = append(blockers, "工作流模板没有声明业务模型契约")
	}
	if len(languages) == 0 {
		blockers = append(blockers, "工作流模板没有声明可用语言")
	}
	return ProjectOptions{
		WorkflowTemplateVersionID: template.ID, WorkflowTemplateVersion: template.Version,
		TemplateContentHash: template.ContentHash, VideoProductionProfileKey: template.VideoProfileKey,
		VideoProductionProfileVersion: template.VideoProfileVersion, Available: len(blockers) == 0, Blockers: blockers,
		Durations:         normalizedPositiveInts(configuration.Durations, []int{15, 30, 60}),
		AspectRatios:      normalizedStrings(configuration.AspectRatios, []string{"9:16", "16:9", "1:1"}),
		ImageQualities:    normalizedStrings(configuration.ImageQualities, []string{"standard", "hd"}),
		LanguageModes:     normalizedStrings(configuration.LanguageModes, []string{"auto", "explicit"}),
		AudioStrategies:   normalizedStrings(configuration.AudioStrategies, []string{"native_av", "external_audio"}),
		AudioRequirements: normalizedStrings(configuration.AudioRequirements, []string{"preferred", "required", "disabled"}),
		Languages:         languages, ModelRequirements: requirements,
	}, nil
}

func normalizedPositiveInts(values, fallback []int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value > 0 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return append([]int(nil), fallback...)
	}
	return result
}

func normalizedStrings(values, fallback []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return append([]string(nil), fallback...)
	}
	return result
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (options ProjectOptions) ValidateDraftSelection(duration int, aspectRatio, imageQuality, languageMode string, targetLanguage *string) error {
	if !containsInt(options.Durations, duration) || !containsString(options.AspectRatios, aspectRatio) ||
		!containsString(options.ImageQualities, imageQuality) || !containsString(options.LanguageModes, languageMode) {
		return Error{Code: CodeSetupIncomplete, Message: "带货视频创建参数不在当前业务模板允许范围内"}
	}
	if languageMode == "explicit" {
		if targetLanguage == nil {
			return Error{Code: CodeLanguageRequired, Message: "明确指定语言时必须选择目标语言"}
		}
		target, err := normalizeLocale(*targetLanguage)
		if err != nil {
			return Error{Code: CodeLanguageUnsupported, Message: "目标语言标签无效", Cause: err}
		}
		for _, language := range options.Languages {
			if language.Locale == target {
				return nil
			}
		}
		return Error{Code: CodeLanguageUnsupported, Message: "目标语言不在当前带货视频模板支持范围内"}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
