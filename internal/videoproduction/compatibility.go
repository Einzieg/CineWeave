package videoproduction

import (
	"encoding/json"
	"fmt"
	"strings"
)

type capabilityRequirements struct {
	TaskType                   string      `json:"taskType"`
	InputContract              string      `json:"inputContract"`
	SupportsFirstFrame         bool        `json:"supportsFirstFrame"`
	SupportsLastFrame          bool        `json:"supportsLastFrame"`
	SupportsSemanticReferences bool        `json:"supportsSemanticReferenceImages"`
	SupportsStoryboardSheet    bool        `json:"supportsStoryboardSheetReference"`
	MaxFirstFrames             minimumSpec `json:"maxFirstFrames"`
	MaxReferenceImages         minimumSpec `json:"maxReferenceImages"`
}

type minimumSpec struct {
	Minimum int `json:"minimum"`
}

type providerOptions struct {
	XCapabilities normalizedCapabilities `json:"xCapabilities"`
}

type normalizedCapabilities struct {
	SupportsFirstFrame      bool                     `json:"supportsFirstFrame"`
	SupportsLastFrame       bool                     `json:"supportsLastFrame"`
	SupportsReferenceImages bool                     `json:"supportsReferenceImages"`
	SupportsMultimodalInput bool                     `json:"supportsMultimodalInput"`
	MaxReferenceImages      int                      `json:"maxReferenceImages"`
	ReferenceTypes          []string                 `json:"referenceTypes"`
	VideoGenerationVariants []videoGenerationVariant `json:"videoGenerationVariants"`
}

type videoGenerationVariant struct {
	When struct {
		TaskTypes            []string `json:"taskTypes"`
		ReferenceModes       []string `json:"referenceModes"`
		NativeAudioRequested *bool    `json:"nativeAudioRequested"`
	} `json:"when"`
	NativeAudio struct {
		Support                    any      `json:"support"`
		SupportedDialogueLanguages []string `json:"supportedDialogueLanguages"`
	} `json:"nativeAudio"`
	Continuation struct {
		SupportsFirstFrame     bool `json:"supportsFirstFrame"`
		SupportsLastFrame      bool `json:"supportsLastFrame"`
		SupportsVideoReference bool `json:"supportsVideoReference"`
	} `json:"continuation"`
}

// EvaluateModelCompatibility evaluates the normalized capability contract only.
// Provider-specific field mapping remains owned by Provider Gateway adapters.
func EvaluateModelCompatibility(profile ProfileVersion, capability ModelCapability, nativeAudioRequired bool) ModelCompatibility {
	result := ModelCompatibility{Compatible: true, Issues: make([]CompatibilityIssue, 0)}
	addIssue := func(code, message string) {
		result.Compatible = false
		result.Issues = append(result.Issues, CompatibilityIssue{Code: code, Message: message})
	}

	var requirements capabilityRequirements
	if err := json.Unmarshal(profile.CapabilityRequirements, &requirements); err != nil {
		addIssue("PROFILE_CAPABILITY_CONTRACT_INVALID", "视频生产方案的模型能力合同无效")
		return result
	}
	var taskTypes []string
	if err := json.Unmarshal(capability.TaskTypes, &taskTypes); err != nil {
		addIssue("MODEL_TASK_TYPES_INVALID", "模型任务类型配置无效")
		return result
	}
	var options providerOptions
	if err := json.Unmarshal(capability.ProviderOptionsSchema, &options); err != nil {
		addIssue("MODEL_CAPABILITIES_INVALID", "模型能力配置无效")
		return result
	}

	if requirements.TaskType != "" && !containsString(taskTypes, requirements.TaskType) {
		addIssue("TASK_TYPE_UNSUPPORTED", fmt.Sprintf("模型不支持任务类型 %s", requirements.TaskType))
	}

	caps := options.XCapabilities
	firstFrame := caps.SupportsFirstFrame || containsString(caps.ReferenceTypes, "first_frame") || variantSupports(caps.VideoGenerationVariants, requirements.TaskType, "first_frame")
	lastFrame := caps.SupportsLastFrame || containsString(caps.ReferenceTypes, "last_frame") || variantSupports(caps.VideoGenerationVariants, requirements.TaskType, "last_frame")
	referenceImages := caps.SupportsReferenceImages || caps.SupportsMultimodalInput || containsString(caps.ReferenceTypes, "image")

	if requirements.SupportsFirstFrame && !firstFrame {
		addIssue("FIRST_FRAME_UNSUPPORTED", "模型不支持首帧输入")
	}
	if requirements.SupportsLastFrame && !lastFrame {
		addIssue("LAST_FRAME_UNSUPPORTED", "模型不支持尾帧输入")
	}
	if requirements.MaxFirstFrames.Minimum > 0 {
		if !firstFrame || caps.MaxReferenceImages < requirements.MaxFirstFrames.Minimum {
			addIssue("FIRST_FRAME_LIMIT_TOO_LOW", "模型可接收的首帧数量不足")
		}
	}
	if requirements.SupportsSemanticReferences && (!referenceImages || !caps.SupportsMultimodalInput) {
		addIssue("SEMANTIC_REFERENCE_UNSUPPORTED", "模型不支持带语义的多参考图输入")
	}
	if requirements.MaxReferenceImages.Minimum > 0 && caps.MaxReferenceImages < requirements.MaxReferenceImages.Minimum {
		addIssue("REFERENCE_IMAGE_LIMIT_TOO_LOW", "模型可接收的参考图数量不足")
	}
	if requirements.SupportsStoryboardSheet && (!referenceImages || !caps.SupportsMultimodalInput) {
		addIssue("STORYBOARD_SHEET_UNSUPPORTED", "模型不支持分镜板参考输入")
	}
	if nativeAudioRequired && !hasNativeAudioVariant(caps.VideoGenerationVariants, requirements.TaskType) {
		addIssue("NATIVE_AUDIO_UNSUPPORTED", "项目要求模型原生音频，但当前模型没有兼容的原生音频变体")
	}
	return result
}

func variantSupports(variants []videoGenerationVariant, taskType, referenceMode string) bool {
	for _, variant := range variants {
		if taskType != "" && len(variant.When.TaskTypes) > 0 && !containsString(variant.When.TaskTypes, taskType) {
			continue
		}
		if containsString(variant.When.ReferenceModes, referenceMode) {
			return true
		}
		if referenceMode == "first_frame" && variant.Continuation.SupportsFirstFrame {
			return true
		}
		if referenceMode == "last_frame" && variant.Continuation.SupportsLastFrame {
			return true
		}
	}
	return false
}

func hasNativeAudioVariant(variants []videoGenerationVariant, taskType string) bool {
	for _, variant := range variants {
		if taskType != "" && len(variant.When.TaskTypes) > 0 && !containsString(variant.When.TaskTypes, taskType) {
			continue
		}
		if !truthyCapability(variant.NativeAudio.Support) {
			continue
		}
		if len(variant.NativeAudio.SupportedDialogueLanguages) == 0 || containsString(variant.NativeAudio.SupportedDialogueLanguages, "zh-CN") {
			return true
		}
	}
	return false
}

func truthyCapability(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.EqualFold(strings.TrimSpace(typed), "supported")
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
