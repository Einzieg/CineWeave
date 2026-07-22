package videoproduction

import "strings"

const (
	PromptRoleAnchorPlan     = "anchor.plan"
	PromptRoleAnchorGenerate = "anchor.generate"
	PromptRoleAnchorReview   = "anchor.review"
	PromptRoleVideoGenerate  = "video.generate"
	PromptRoleVideoReview    = "video.review"
)

type PromptContractLayer struct {
	LayerKey    string `json:"layerKey"`
	VersionID   string `json:"versionId,omitempty"`
	ContentHash string `json:"contentHash"`
	Source      string `json:"source"`
}

type PromptContractProvenance struct {
	ProfileKey             string                `json:"profileKey"`
	ProfileVersionID       string                `json:"profileVersionId"`
	ProfileSnapshotHash    string                `json:"profileSnapshotHash"`
	PromptTemplateKey      string                `json:"promptTemplateKey"`
	PromptContextPlanHash  string                `json:"promptContextPlanHash"`
	ShotStateHash          string                `json:"shotStateHash"`
	TransitionHash         string                `json:"transitionHash,omitempty"`
	ReferencePackHash      string                `json:"referencePackHash"`
	CapabilitySnapshotHash string                `json:"capabilitySnapshotHash"`
	InputContractVersion   string                `json:"inputContractVersion"`
	Layers                 []PromptContractLayer `json:"layers"`
	ContractHash           string                `json:"contractHash"`
}

type PromptContractInput struct {
	ProfileKey             string
	ProfileVersionID       string
	ProfileSnapshotHash    string
	Role                   string
	InputContractVersion   string
	ContextPlan            PromptContextPlan
	ShotState              ShotState
	Transition             *ShotTransition
	ReferencePack          ReferencePack
	CapabilitySnapshotHash string
	Layers                 []PromptContractLayer
}

type CompiledPromptContract struct {
	TemplateKey string                   `json:"templateKey"`
	Context     map[string]any           `json:"context"`
	Provenance  PromptContractProvenance `json:"provenance"`
}

type PromptContractReview struct {
	Approved bool              `json:"approved"`
	Checks   map[string]string `json:"checks"`
	Issues   []ContractIssue   `json:"issues"`
}

func CompilePromptContract(input PromptContractInput) (CompiledPromptContract, error) {
	profileKey := enumValue(input.ProfileKey)
	role := strings.ToLower(strings.TrimSpace(input.Role))
	if profileKey == "" || strings.TrimSpace(input.ProfileVersionID) == "" || !validPromptRole(role) {
		return CompiledPromptContract{}, Error{Code: CodePromptContractIncomplete, Message: "Prompt Contract 缺少 Profile version 或 role"}
	}
	strategy, err := ProfileStrategyFor(profileKey)
	if err != nil {
		return CompiledPromptContract{}, err
	}
	if !validSHA256(input.ProfileSnapshotHash) || !validSHA256(input.ContextPlan.PlanHash) || !validSHA256(input.ReferencePack.ManifestHash) || !validSHA256(input.CapabilitySnapshotHash) {
		return CompiledPromptContract{}, Error{Code: CodePromptContractIncomplete, Message: "Prompt Contract provenance hash 不完整"}
	}
	shotStateHash, err := HashShotState(input.ShotState)
	if err != nil {
		return CompiledPromptContract{}, err
	}
	if shotStateHash != normalizeSHA256(input.ReferencePack.ShotStateHash) {
		return CompiledPromptContract{}, Error{Code: CodePromptContractIncomplete, Message: "Reference Pack 与 ShotState hash 不一致"}
	}
	transitionHash := ""
	if input.Transition != nil {
		transitionHash, err = HashTransition(*input.Transition)
		if err != nil {
			return CompiledPromptContract{}, err
		}
	}
	layers := make([]PromptContractLayer, 0, len(input.Layers))
	for _, layer := range input.Layers {
		layer.LayerKey = strings.TrimSpace(layer.LayerKey)
		layer.VersionID = strings.TrimSpace(layer.VersionID)
		layer.ContentHash = normalizeSHA256(layer.ContentHash)
		layer.Source = strings.TrimSpace(layer.Source)
		if layer.LayerKey == "" || !validSHA256(layer.ContentHash) || layer.Source == "" {
			return CompiledPromptContract{}, Error{Code: CodePromptContractIncomplete, Message: "Prompt Contract layer 缺少 key、hash 或 source"}
		}
		layers = append(layers, layer)
	}
	templateKey, err := strategy.Prompts().TemplateKey(role)
	if err != nil {
		return CompiledPromptContract{}, err
	}
	provenance := PromptContractProvenance{
		ProfileKey:             profileKey,
		ProfileVersionID:       strings.TrimSpace(input.ProfileVersionID),
		ProfileSnapshotHash:    normalizeSHA256(input.ProfileSnapshotHash),
		PromptTemplateKey:      templateKey,
		PromptContextPlanHash:  normalizeSHA256(input.ContextPlan.PlanHash),
		ShotStateHash:          shotStateHash,
		TransitionHash:         transitionHash,
		ReferencePackHash:      normalizeSHA256(input.ReferencePack.ManifestHash),
		CapabilitySnapshotHash: normalizeSHA256(input.CapabilitySnapshotHash),
		InputContractVersion:   strings.TrimSpace(input.InputContractVersion),
		Layers:                 layers,
	}
	if provenance.InputContractVersion == "" {
		return CompiledPromptContract{}, Error{Code: CodePromptContractIncomplete, Message: "Input Contract version 不能为空"}
	}
	hashInput := provenance
	hashInput.ContractHash = ""
	provenance.ContractHash, err = canonicalHash(hashInput)
	if err != nil {
		return CompiledPromptContract{}, err
	}
	context := map[string]any{
		"profile": map[string]any{
			"key":                  profileKey,
			"versionId":            provenance.ProfileVersionID,
			"snapshotHash":         provenance.ProfileSnapshotHash,
			"inputContractVersion": provenance.InputContractVersion,
		},
		"promptContextPlan": input.ContextPlan,
		"shotState":         NormalizeShotState(input.ShotState),
		"transition":        input.Transition,
		"referencePack":     input.ReferencePack.Manifest,
		"capability": map[string]any{
			"snapshotHash": provenance.CapabilitySnapshotHash,
		},
		"layers": layers,
	}
	return CompiledPromptContract{TemplateKey: templateKey, Context: context, Provenance: provenance}, nil
}

func ReviewSingleFrameImagePrompt(prompt string, cues []DialogueCue) PromptContractReview {
	review := PromptContractReview{Approved: true, Checks: map[string]string{}, Issues: []ContractIssue{}}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		review.addIssue("EMPTY_IMAGE_PROMPT", "prompt", "图片 Prompt 不能为空")
	} else {
		review.Checks["prompt"] = "passed"
	}
	for _, cue := range normalizeDialogueCues(cues) {
		if strings.Contains(normalizePromptText(prompt), normalizePromptText(cue.Text)) {
			review.addIssue("DIALOGUE_LEAKAGE", "prompt", "图片 Prompt 包含剧本台词："+cue.Text)
		}
	}
	if containsVisibleTextInstruction(prompt) {
		review.addIssue("VISIBLE_TEXT_REQUEST", "prompt", "图片 Prompt 要求生成字幕、台词或可见文字")
	}
	if review.Checks["dialogueIsolation"] == "" {
		review.Checks["dialogueIsolation"] = "passed"
	}
	return review
}

func ReviewSingleFrameVideoPrompt(prompt string, cues []DialogueCue, nativeAudioRequired, modelSupportsNativeAudio bool) PromptContractReview {
	review := PromptContractReview{Approved: true, Checks: map[string]string{}, Issues: []ContractIssue{}}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		review.addIssue("EMPTY_VIDEO_PROMPT", "prompt", "视频 Prompt 不能为空")
	} else {
		review.Checks["prompt"] = "passed"
	}
	for _, cue := range normalizeDialogueCues(cues) {
		if !strings.Contains(normalizePromptText(prompt), normalizePromptText(cue.Text)) {
			review.addIssue("DIALOGUE_NOT_VERBATIM", "verbatimDialogueCues", "视频 Prompt 未逐字保留中文台词："+cue.Text)
		}
	}
	if nativeAudioRequired && !modelSupportsNativeAudio {
		review.addIssue("NATIVE_AUDIO_UNSUPPORTED", "capability", "项目要求原生音频，但目标模型不支持原生音视频输出")
	}
	if review.Checks["verbatimDialogue"] == "" {
		review.Checks["verbatimDialogue"] = "passed"
	}
	if review.Checks["nativeAudio"] == "" {
		review.Checks["nativeAudio"] = "passed"
	}
	return review
}

func (review *PromptContractReview) addIssue(code, field, message string) {
	review.Approved = false
	review.Checks[field] = "failed"
	if code == "DIALOGUE_LEAKAGE" || code == "VISIBLE_TEXT_REQUEST" {
		review.Checks["dialogueIsolation"] = "failed"
	}
	if code == "DIALOGUE_NOT_VERBATIM" {
		review.Checks["verbatimDialogue"] = "failed"
	}
	if code == "NATIVE_AUDIO_UNSUPPORTED" {
		review.Checks["nativeAudio"] = "failed"
	}
	review.Issues = append(review.Issues, ContractIssue{Code: code, Field: field, Message: message})
}

func validPromptRole(value string) bool {
	return oneOf(value, PromptRoleAnchorPlan, PromptRoleAnchorGenerate, PromptRoleAnchorReview, PromptRoleVideoGenerate, PromptRoleVideoReview)
}

func normalizePromptText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func containsVisibleTextInstruction(prompt string) bool {
	normalized := strings.ToLower(prompt)
	for _, marker := range []string{"画面字幕：", "显示字幕：", "可见文字：", "对话框：", "speech bubble:", "visible text:", "subtitle:"} {
		if strings.Contains(normalized, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}
