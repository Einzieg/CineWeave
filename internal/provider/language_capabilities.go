package provider

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const (
	CapabilitySourceOfficial   = "official"
	CapabilitySourceProvider   = "provider"
	CapabilitySourcePreset     = "preset"
	CapabilitySourceDiscovered = "discovered"
	CapabilitySourceInferred   = "inferred"
	CapabilitySourceManual     = "manual"
	CapabilitySourceUnknown    = "unknown"

	CapabilityApprovalApproved = "approved"
	CapabilityApprovalInferred = "inferred"
	CapabilityApprovalRejected = "rejected"
	CapabilityApprovalUnknown  = "unknown"
)

type LanguageCapabilityRequirement struct {
	InputLanguage       string
	OutputLanguage      string
	PromptLanguage      string
	NativeAudioLanguage string
	RequireApproved     bool
}

type capabilityLanguageMetadata struct {
	SupportedInputLanguages       []string
	SupportedOutputLanguages      []string
	SupportedPromptLanguages      []string
	SupportedNativeAudioLanguages []string
	Source                        string
	ApprovalStatus                string
}

func normalizeCapabilityLanguageMetadata(input CapabilityInput, providerOptionsSchema json.RawMessage) (CapabilityInput, json.RawMessage, error) {
	schema := map[string]any{}
	if err := json.Unmarshal(providerOptionsSchema, &schema); err != nil || schema == nil {
		return CapabilityInput{}, nil, fmt.Errorf("%w: providerOptionsSchema must be a JSON object", ErrValidation)
	}
	xCapabilities, _ := schema["xCapabilities"].(map[string]any)
	if xCapabilities == nil {
		xCapabilities = map[string]any{}
		schema["xCapabilities"] = xCapabilities
	}

	existing := capabilityLanguageMetadataFromMap(xCapabilities)
	metadata := capabilityLanguageMetadata{
		SupportedInputLanguages:       chooseCapabilityLanguages(input.SupportedInputLanguages, existing.SupportedInputLanguages),
		SupportedOutputLanguages:      chooseCapabilityLanguages(input.SupportedOutputLanguages, existing.SupportedOutputLanguages),
		SupportedPromptLanguages:      chooseCapabilityLanguages(input.SupportedPromptLanguages, existing.SupportedPromptLanguages),
		SupportedNativeAudioLanguages: chooseCapabilityLanguages(input.SupportedNativeAudioLanguages, existing.SupportedNativeAudioLanguages),
		Source:                        firstNonEmptyString(input.Source, existing.Source),
		ApprovalStatus:                firstNonEmptyString(input.ApprovalStatus, existing.ApprovalStatus),
	}
	var err error
	metadata.SupportedInputLanguages, err = normalizeLanguageTags(metadata.SupportedInputLanguages)
	if err != nil {
		return CapabilityInput{}, nil, fmt.Errorf("%w: supportedInputLanguages %v", ErrValidation, err)
	}
	metadata.SupportedOutputLanguages, err = normalizeLanguageTags(metadata.SupportedOutputLanguages)
	if err != nil {
		return CapabilityInput{}, nil, fmt.Errorf("%w: supportedOutputLanguages %v", ErrValidation, err)
	}
	metadata.SupportedPromptLanguages, err = normalizeLanguageTags(metadata.SupportedPromptLanguages)
	if err != nil {
		return CapabilityInput{}, nil, fmt.Errorf("%w: supportedPromptLanguages %v", ErrValidation, err)
	}
	metadata.SupportedNativeAudioLanguages, err = normalizeLanguageTags(metadata.SupportedNativeAudioLanguages)
	if err != nil {
		return CapabilityInput{}, nil, fmt.Errorf("%w: supportedNativeAudioLanguages %v", ErrValidation, err)
	}
	metadata.Source, err = normalizeCapabilitySource(metadata.Source)
	if err != nil {
		return CapabilityInput{}, nil, err
	}
	metadata.ApprovalStatus, err = normalizeCapabilityApproval(metadata.ApprovalStatus, metadata.Source)
	if err != nil {
		return CapabilityInput{}, nil, err
	}

	xCapabilities["supportedInputLanguages"] = metadata.SupportedInputLanguages
	xCapabilities["supportedOutputLanguages"] = metadata.SupportedOutputLanguages
	xCapabilities["supportedPromptLanguages"] = metadata.SupportedPromptLanguages
	xCapabilities["supportedNativeAudioLanguages"] = metadata.SupportedNativeAudioLanguages
	xCapabilities["capabilitySource"] = metadata.Source
	xCapabilities["capabilityApprovalStatus"] = metadata.ApprovalStatus

	normalizedSchema, err := json.Marshal(schema)
	if err != nil {
		return CapabilityInput{}, nil, err
	}
	input.SupportedInputLanguages = metadata.SupportedInputLanguages
	input.SupportedOutputLanguages = metadata.SupportedOutputLanguages
	input.SupportedPromptLanguages = metadata.SupportedPromptLanguages
	input.SupportedNativeAudioLanguages = metadata.SupportedNativeAudioLanguages
	input.Source = metadata.Source
	input.ApprovalStatus = metadata.ApprovalStatus
	return input, normalizedSchema, nil
}

func capabilityLanguageMetadataFromSchema(raw json.RawMessage) capabilityLanguageMetadata {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
		return unknownCapabilityLanguageMetadata()
	}
	xCapabilities, _ := schema["xCapabilities"].(map[string]any)
	if xCapabilities == nil {
		return unknownCapabilityLanguageMetadata()
	}
	metadata := capabilityLanguageMetadataFromMap(xCapabilities)
	deriveCapabilityLanguagesFromVideoVariants(&metadata, xCapabilities)
	metadata.Source, _ = normalizeCapabilitySource(metadata.Source)
	metadata.ApprovalStatus, _ = normalizeCapabilityApproval(metadata.ApprovalStatus, metadata.Source)
	metadata.SupportedInputLanguages, _ = normalizeLanguageTags(metadata.SupportedInputLanguages)
	metadata.SupportedOutputLanguages, _ = normalizeLanguageTags(metadata.SupportedOutputLanguages)
	metadata.SupportedPromptLanguages, _ = normalizeLanguageTags(metadata.SupportedPromptLanguages)
	metadata.SupportedNativeAudioLanguages, _ = normalizeLanguageTags(metadata.SupportedNativeAudioLanguages)
	return metadata
}

func capabilityLanguageMetadataFromMap(values map[string]any) capabilityLanguageMetadata {
	return capabilityLanguageMetadata{
		SupportedInputLanguages:       stringsFromAny(values["supportedInputLanguages"]),
		SupportedOutputLanguages:      stringsFromAny(values["supportedOutputLanguages"]),
		SupportedPromptLanguages:      stringsFromAny(values["supportedPromptLanguages"]),
		SupportedNativeAudioLanguages: stringsFromAny(values["supportedNativeAudioLanguages"]),
		Source:                        stringFromAny(values["capabilitySource"]),
		ApprovalStatus:                stringFromAny(values["capabilityApprovalStatus"]),
	}
}

func deriveCapabilityLanguagesFromVideoVariants(metadata *capabilityLanguageMetadata, xCapabilities map[string]any) {
	variants, _ := xCapabilities["videoGenerationVariants"].([]any)
	if len(variants) == 0 {
		return
	}
	verificationStatuses := make([]string, 0, len(variants))
	sources := make([]string, 0, len(variants))
	for _, item := range variants {
		variant, ok := item.(map[string]any)
		if !ok {
			continue
		}
		metadata.SupportedPromptLanguages = append(metadata.SupportedPromptLanguages, stringsFromAny(variant["supportedPromptLanguages"])...)
		if nativeAudio, ok := variant["nativeAudio"].(map[string]any); ok {
			metadata.SupportedNativeAudioLanguages = append(metadata.SupportedNativeAudioLanguages, stringsFromAny(nativeAudio["supportedDialogueLanguages"])...)
		}
		if source := stringFromAny(variant["source"]); source != "" {
			sources = append(sources, source)
		}
		if status := stringFromAny(variant["verificationStatus"]); status != "" {
			verificationStatuses = append(verificationStatuses, status)
		}
	}
	if metadata.Source == "" {
		metadata.Source = deriveCapabilitySource(sources)
	}
	if metadata.ApprovalStatus == "" {
		metadata.ApprovalStatus = deriveCapabilityApproval(verificationStatuses)
	}
}

func unknownCapabilityLanguageMetadata() capabilityLanguageMetadata {
	return capabilityLanguageMetadata{
		SupportedInputLanguages:       []string{},
		SupportedOutputLanguages:      []string{},
		SupportedPromptLanguages:      []string{},
		SupportedNativeAudioLanguages: []string{},
		Source:                        CapabilitySourceUnknown,
		ApprovalStatus:                CapabilityApprovalUnknown,
	}
}

func normalizeCapabilitySource(value string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "", CapabilitySourceUnknown:
		return CapabilitySourceUnknown, nil
	case "user":
		return CapabilitySourceManual, nil
	case CapabilitySourceOfficial, CapabilitySourceProvider, CapabilitySourcePreset, CapabilitySourceDiscovered, CapabilitySourceInferred, CapabilitySourceManual:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: capability source is invalid", ErrValidation)
	}
}

func normalizeCapabilityApproval(value, source string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case CapabilityApprovalApproved, CapabilityApprovalInferred, CapabilityApprovalRejected, CapabilityApprovalUnknown:
		return normalized, nil
	case "":
		switch source {
		case CapabilitySourceOfficial, CapabilitySourceProvider, CapabilitySourcePreset, CapabilitySourceManual:
			return CapabilityApprovalApproved, nil
		case CapabilitySourceDiscovered, CapabilitySourceInferred:
			return CapabilityApprovalInferred, nil
		default:
			return CapabilityApprovalUnknown, nil
		}
	default:
		return "", fmt.Errorf("%w: capability approvalStatus is invalid", ErrValidation)
	}
}

func normalizeLanguageTags(values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		tag := canonicalLanguageTag(value)
		if tag == "" {
			return nil, fmt.Errorf("contains invalid BCP 47 tag %q", strings.TrimSpace(value))
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, tag)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result, nil
}

func canonicalLanguageTag(value string) string {
	value = strings.TrimSpace(value)
	if value == "*" {
		return value
	}
	parts := strings.Split(value, "-")
	if len(parts) == 0 || len(parts[0]) < 2 || len(parts[0]) > 3 || !allLetters(parts[0]) {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	for index := 1; index < len(parts); index++ {
		part := parts[index]
		if len(part) < 2 || len(part) > 8 || !allLettersOrDigits(part) {
			return ""
		}
		switch {
		case len(part) == 2 && allLetters(part):
			parts[index] = strings.ToUpper(part)
		case len(part) == 4 && allLetters(part):
			runes := []rune(strings.ToLower(part))
			runes[0] = unicode.ToUpper(runes[0])
			parts[index] = string(runes)
		default:
			parts[index] = strings.ToLower(part)
		}
	}
	return strings.Join(parts, "-")
}

func allLetters(value string) bool {
	for _, char := range value {
		if !unicode.IsLetter(char) || char > unicode.MaxASCII {
			return false
		}
	}
	return value != ""
}

func allLettersOrDigits(value string) bool {
	for _, char := range value {
		if char > unicode.MaxASCII || (!unicode.IsLetter(char) && !unicode.IsDigit(char)) {
			return false
		}
	}
	return value != ""
}

func chooseCapabilityLanguages(explicit, existing []string) []string {
	if explicit != nil {
		return explicit
	}
	return existing
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func deriveCapabilitySource(values []string) string {
	if len(values) == 0 {
		return CapabilitySourceUnknown
	}
	best := CapabilitySourceUnknown
	for _, value := range values {
		source, err := normalizeCapabilitySource(value)
		if err != nil {
			return CapabilitySourceUnknown
		}
		if source == CapabilitySourceInferred || source == CapabilitySourceDiscovered {
			return source
		}
		if source != CapabilitySourceUnknown {
			best = source
		}
	}
	return best
}

func deriveCapabilityApproval(values []string) string {
	if len(values) == 0 {
		return CapabilityApprovalUnknown
	}
	result := CapabilityApprovalApproved
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case VideoCapabilityVerificationOfficial, VideoCapabilityVerificationTested:
		case VideoCapabilityVerificationInferred:
			if result != CapabilityApprovalRejected {
				result = CapabilityApprovalInferred
			}
		default:
			return CapabilityApprovalUnknown
		}
	}
	return result
}

func ValidateModelLanguageCapabilities(model Model, taskType string, requirement LanguageCapabilityRequirement) error {
	relevant := make([]Capability, 0, len(model.Capabilities))
	for _, capability := range model.Capabilities {
		if capabilitySupportsTaskType(capability, taskType) {
			relevant = append(relevant, capability)
		}
	}
	if len(relevant) == 0 {
		return unsupportedLanguageCapability("模型没有匹配任务的语言能力")
	}
	checks := []struct {
		name     string
		language string
		values   func(Capability) []string
	}{
		{name: "输入", language: requirement.InputLanguage, values: func(value Capability) []string { return value.SupportedInputLanguages }},
		{name: "输出", language: requirement.OutputLanguage, values: func(value Capability) []string { return value.SupportedOutputLanguages }},
		{name: "提示词", language: requirement.PromptLanguage, values: func(value Capability) []string { return value.SupportedPromptLanguages }},
		{name: "原生音频", language: requirement.NativeAudioLanguage, values: func(value Capability) []string { return value.SupportedNativeAudioLanguages }},
	}
	for _, check := range checks {
		language := strings.TrimSpace(check.language)
		if language == "" {
			continue
		}
		if canonicalLanguageTag(language) == "" {
			return unsupportedLanguageCapability(fmt.Sprintf("请求的%s语言标签 %s 无效", check.name, language))
		}
		matched := false
		unapproved := false
		declared := false
		for _, capability := range relevant {
			values := check.values(capability)
			if len(values) == 0 {
				continue
			}
			declared = true
			if !matchesLanguage(values, language) {
				continue
			}
			matched = true
			if !requirement.RequireApproved || capability.ApprovalStatus == CapabilityApprovalApproved {
				unapproved = false
				break
			}
			unapproved = true
		}
		if matched && !unapproved {
			continue
		}
		if requirement.RequireApproved && (unapproved || !declared) {
			return &StandardErrorError{Standard: StandardError{
				Code: CodeModelCapabilityApprovalRequired, Message: fmt.Sprintf("模型的%s语言能力尚未批准", check.name), Retryable: false,
			}}
		}
		return unsupportedLanguageCapability(fmt.Sprintf("模型不支持%s语言 %s", check.name, language))
	}
	return nil
}

func capabilitySupportsTaskType(capability Capability, taskType string) bool {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return true
	}
	for _, value := range stringsFromRawJSON(capability.TaskTypes) {
		if value == taskType || (taskType == TaskTypeVideoCreateTask && value == "video.generate") {
			return true
		}
	}
	return false
}

func unsupportedLanguageCapability(message string) error {
	return &StandardErrorError{Standard: StandardError{Code: CodeUnsupportedCapability, Message: message, Retryable: false}}
}
