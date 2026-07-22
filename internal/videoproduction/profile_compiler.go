package videoproduction

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/videocontracts"
)

const (
	AnchorRolePlannedFirstFrame = "planned_first_frame"
	AnchorRolePlannedLastFrame  = "planned_last_frame"
	AnchorRoleStoryboardSheet   = "storyboard_sheet"
	AnchorRoleStoryboardPanel   = "storyboard_panel"

	InputContractFirstFrame               = string(videocontracts.InputContractFirstFrame)
	InputContractFirstLastFrames          = string(videocontracts.InputContractFirstLastFrames)
	InputContractFirstFramePlusReferences = string(videocontracts.InputContractFirstFramePlusReferences)
	InputContractStoryboardSheetReference = string(videocontracts.InputContractStoryboardSheetReference)
	InputContractVideoExtension           = string(videocontracts.InputContractVideoExtension)
)

const shotStatePlannerSchemaContract = `
每个 shots[] 元素必须额外输出 plannedEntryState、plannedExitState、transitionFromPrevious。
plannedEntryState/plannedExitState 必须严格符合 ShotStateContract：
- JSON 层级必须是 {"scene":{},"characters":[],"props":[],"camera":{},"action":{"entry":"","exit":""},"screenDirection":""}；screenDirection 是 ShotState 顶层字段，与 action 同级，禁止放入 action 内部。
- scene.assetId 必须引用输入 assets 中的场景 UUID；timeOfDay 仅 dawn/morning/day/afternoon/dusk/night/unknown；weather 仅 clear/cloudy/overcast/fog/light_rain/rain/storm/snow/indoor/unknown。
- characters 每项使用输入 assets 中的角色 UUID；blocking.horizontal 仅 left/center/right/offscreen，depth 仅 foreground/midground/background，facing 仅 screen_left/screen_right/camera/away/profile。
- props 每项使用输入 assets 中的道具 UUID；state 仅 present/held/worn/placed/moving/damaged/hidden/consumed/unknown；held 必须给出 holderAssetId 且该角色在 characters 中。
- assetRequirements 是本镜头可见资产的权威集合；仅为动作、视线或连续性保留但不入画的角色必须设 blocking.horizontal=offscreen，不入画的道具必须设 state=hidden。
- camera.shotSize 仅 extreme_wide/wide/full/medium_wide/medium/medium_close_up/close_up/extreme_close_up/insert；angle 仅 eye_level/low/high/overhead/dutch/over_shoulder/point_of_view；axisSide 仅 A/B/NEUTRAL；lensIntent 仅 wide/normal/telephoto/macro；movement 仅 static/pan/tilt/dolly_in/dolly_out/tracking/crane/handheld/orbit/zoom_in/zoom_out。
- action.entry/action.exit 描述本镜头可达的动作起止状态；顶层 screenDirection 仅 left_to_right/right_to_left/static/toward_camera/away_from_camera/unknown。
- 同一个镜头不得改变场景或当前人物身份集合。镜头切换必须拆成两个 shots。
transitionFromPrevious 只提出 transitionType、confidence；后端会重新分类并覆盖 carry/reset/tailPolicy。不得输出 hard tail policy。`

const singleFramePlannerContract = `<single_frame_i2v_shot_contract priority="highest">
` + shotStatePlannerSchemaContract + `
imagePromptDirection 只描述 plannedEntryState 的单张干净首帧，禁止台词、字幕、引号和可见文字。
</single_frame_i2v_shot_contract>`

const firstLastFramePlannerContract = `<first_last_frame_shot_contract priority="highest">
` + shotStatePlannerSchemaContract + `
plannedEntryState 是当前镜头权威首帧状态，plannedExitState 是同一镜头动作完成后的权威尾帧状态。
两帧必须保持人物身份、appearanceVersionId、costumeVariantId、场景、道具身份和空间轴一致；角色位移、朝向、道具状态和构图变化必须能在本镜头时长内完成。
不得把下一镜头的人物、场景、动作或构图写入 plannedExitState。
imagePromptDirection 只描述两帧共享的视觉风格与硬事实；首帧和尾帧的具体构图由后端分别结合 plannedEntryState/plannedExitState 生成。两帧都禁止台词、字幕、引号和可见文字。
</first_last_frame_shot_contract>`

const multimodalPlannerContract = `<multimodal_reference_shot_contract priority="highest">
` + shotStatePlannerSchemaContract + `
imagePromptDirection 描述当前镜头主构图锚点，禁止台词、字幕、引号和可见文字；角色、场景和道具引用必须使用输入 assets 的 UUID。
</multimodal_reference_shot_contract>`

const storyboardSheetPlannerContract = `<storyboard_sheet_shot_contract priority="highest">
` + shotStatePlannerSchemaContract + `
plannedEntryState 与 plannedExitState 必须描述同一镜头有序动作阶段；不得混入下一镜头。分镜板 panel 的数量和时间点由后续 PanelManifest 编译器决定。
</storyboard_sheet_shot_contract>`

type AnchorRequirement struct {
	Role      string `json:"role"`
	StateRole string `json:"stateRole,omitempty"`
	Required  bool   `json:"required"`
	Minimum   int    `json:"minimum"`
	Maximum   int    `json:"maximum"`
}

type AnchorStrategy interface {
	Requirements() []AnchorRequirement
	ValidateReadyAnchors(map[string]int) error
	PlannerContract() string
}

type ReferenceStrategy interface {
	Allows(purpose, role string) bool
	Validate(purpose string, items []ReferencePackItem) error
}

type InputContractAdapter interface {
	InitialContract() string
	ContinuationContracts() []string
	ValidateReferenceRoles([]ReferencePackItem) error
}

type PromptContractStrategy interface {
	TemplateKey(role string) (string, error)
	ReviewImage(prompt string, cues []DialogueCue) PromptContractReview
	ReviewVideo(prompt string, cues []DialogueCue, nativeAudioRequired, modelSupportsNativeAudio bool) PromptContractReview
}

type ProfileStrategy interface {
	Key() string
	Anchors() AnchorStrategy
	References() ReferenceStrategy
	InputAdapter() InputContractAdapter
	Prompts() PromptContractStrategy
}

type CompiledProfile struct {
	Version                ProfileVersion
	Strategy               ProfileStrategy
	AnchorRequirements     []AnchorRequirement
	InitialInputContract   string
	ContinuationContracts  []string
	PromptTemplates        map[string]string
	Configuration          map[string]any
	CapabilityRequirements map[string]any
}

type ProfileCompiler struct {
	strategies map[string]ProfileStrategy
}

func NewProfileCompiler(strategies ...ProfileStrategy) ProfileCompiler {
	if len(strategies) == 0 {
		strategies = BuiltInProfileStrategies()
	}
	registry := make(map[string]ProfileStrategy, len(strategies))
	for _, strategy := range strategies {
		if strategy == nil || strings.TrimSpace(strategy.Key()) == "" {
			continue
		}
		registry[enumValue(strategy.Key())] = strategy
	}
	return ProfileCompiler{strategies: registry}
}

func BuiltInProfileStrategies() []ProfileStrategy {
	return []ProfileStrategy{
		newBuiltInProfileStrategy(ProfileSingleFrameI2V),
		newBuiltInProfileStrategy(ProfileFirstLastFrame),
		newBuiltInProfileStrategy(ProfileMultimodalReference),
		newBuiltInProfileStrategy(ProfileStoryboardSheet),
	}
}

func ProfileStrategyFor(profileKey string) (ProfileStrategy, error) {
	strategy, ok := NewProfileCompiler().strategies[enumValue(profileKey)]
	if !ok {
		return nil, Error{Code: CodeProfileNotFound, Message: "未注册的视频生产方案：" + strings.TrimSpace(profileKey)}
	}
	return strategy, nil
}

func (compiler ProfileCompiler) Compile(version ProfileVersion, requireAvailable bool) (CompiledProfile, error) {
	profileKey := enumValue(version.ProfileKey)
	strategy, ok := compiler.strategies[profileKey]
	if !ok {
		return CompiledProfile{}, Error{Code: CodeProfileNotFound, Message: "视频生产方案没有运行时策略：" + version.ProfileKey}
	}
	if version.LifecycleState != LifecyclePublished {
		return CompiledProfile{}, Error{Code: CodeProfileUnavailable, Message: "视频生产方案版本尚未发布"}
	}
	if requireAvailable && !version.Available() {
		return CompiledProfile{}, Error{Code: CodeProfileUnavailable, Message: "视频生产方案版本暂不可执行"}
	}
	configuration, err := decodeProfileObject(version.Configuration, "configuration")
	if err != nil {
		return CompiledProfile{}, err
	}
	capabilities, err := decodeProfileObject(version.CapabilityRequirements, "capabilityRequirements")
	if err != nil {
		return CompiledProfile{}, err
	}
	prompts, err := decodeProfileStringMap(version.PromptContract, "promptContract")
	if err != nil {
		return CompiledProfile{}, err
	}
	compiled := CompiledProfile{
		Version: version, Strategy: strategy,
		AnchorRequirements:    strategy.Anchors().Requirements(),
		InitialInputContract:  strategy.InputAdapter().InitialContract(),
		ContinuationContracts: strategy.InputAdapter().ContinuationContracts(),
		PromptTemplates:       prompts, Configuration: configuration,
		CapabilityRequirements: capabilities,
	}
	if err := validateCompiledProfile(compiled); err != nil {
		return CompiledProfile{}, err
	}
	return compiled, nil
}

func validateCompiledProfile(compiled CompiledProfile) error {
	configuredAnchors := stringSliceValue(compiled.Configuration["anchorRoles"])
	requiredAnchors := make([]string, 0, len(compiled.AnchorRequirements))
	for _, item := range compiled.AnchorRequirements {
		if item.Required {
			requiredAnchors = append(requiredAnchors, item.Role)
		}
	}
	for _, role := range requiredAnchors {
		if !containsString(configuredAnchors, role) {
			return Error{Code: CodeProfileIncompatible, Message: "Profile configuration 缺少必需锚点：" + role}
		}
	}
	declaredInitial := strings.TrimSpace(stringValueAny(compiled.CapabilityRequirements["initialInputContract"]))
	if declaredInitial == "" {
		declaredInitial = strings.TrimSpace(stringValueAny(compiled.CapabilityRequirements["inputContract"]))
	}
	if declaredInitial != compiled.InitialInputContract {
		return Error{Code: CodeProfileIncompatible, Message: fmt.Sprintf("Profile 输入契约为 %s，运行时策略要求 %s", declaredInitial, compiled.InitialInputContract)}
	}
	for _, role := range []string{PromptRoleAnchorPlan, PromptRoleAnchorGenerate, PromptRoleAnchorReview, PromptRoleVideoGenerate, PromptRoleVideoReview} {
		key, err := compiled.Strategy.Prompts().TemplateKey(role)
		if err != nil {
			return err
		}
		contractKey := promptContractField(role)
		if strings.TrimSpace(compiled.PromptTemplates[contractKey]) != key {
			return Error{Code: CodePromptContractIncomplete, Message: "Profile Prompt Contract 缺少或不匹配：" + contractKey}
		}
	}
	return nil
}

type builtInProfileStrategy struct {
	key        string
	anchors    staticAnchorStrategy
	references staticReferenceStrategy
	input      staticInputContractAdapter
	prompt     profilePromptStrategy
}

func newBuiltInProfileStrategy(profileKey string) ProfileStrategy {
	strategy := &builtInProfileStrategy{key: profileKey}
	strategy.prompt = profilePromptStrategy{profileKey: profileKey}
	semanticRoles := []string{
		ReferenceRoleCharacterIdentity, ReferenceRoleCharacterCostume,
		ReferenceRoleSceneIdentity, ReferenceRoleSceneSpatial, ReferenceRolePropIdentity,
		ReferenceRoleContinuityHint, ReferenceRoleMotion, ReferenceRoleVideo,
		ReferenceRoleAudio, ReferenceRoleStyle,
	}
	strategy.references.anchorRoles = append([]string(nil), semanticRoles...)
	switch profileKey {
	case ProfileSingleFrameI2V:
		strategy.anchors.requirements = []AnchorRequirement{{Role: AnchorRolePlannedFirstFrame, StateRole: StateRolePlannedEntry, Required: true, Minimum: 1, Maximum: 1}}
		strategy.anchors.plannerContract = singleFramePlannerContract
		strategy.references.videoRoles = []string{ReferenceRoleFirstFrame}
		strategy.references.requiredVideoRoles = map[string]int{ReferenceRoleFirstFrame: 1}
		strategy.input = staticInputContractAdapter{initial: InputContractFirstFrame, continuation: []string{InputContractVideoExtension, InputContractFirstFrame}, requiredRoles: map[string]int{ReferenceRoleFirstFrame: 1}, maximumRoles: map[string]int{ReferenceRoleFirstFrame: 1}}
	case ProfileFirstLastFrame:
		strategy.anchors.requirements = []AnchorRequirement{
			{Role: AnchorRolePlannedFirstFrame, StateRole: StateRolePlannedEntry, Required: true, Minimum: 1, Maximum: 1},
			{Role: AnchorRolePlannedLastFrame, StateRole: StateRolePlannedExit, Required: true, Minimum: 1, Maximum: 1},
		}
		strategy.anchors.plannerContract = firstLastFramePlannerContract
		strategy.references.videoRoles = []string{ReferenceRoleFirstFrame, ReferenceRoleLastFrame}
		strategy.references.requiredVideoRoles = map[string]int{ReferenceRoleFirstFrame: 1, ReferenceRoleLastFrame: 1}
		strategy.input = staticInputContractAdapter{initial: InputContractFirstLastFrames, requiredRoles: map[string]int{ReferenceRoleFirstFrame: 1, ReferenceRoleLastFrame: 1}, maximumRoles: map[string]int{ReferenceRoleFirstFrame: 1, ReferenceRoleLastFrame: 1}}
	case ProfileMultimodalReference:
		strategy.anchors.requirements = []AnchorRequirement{{Role: AnchorRolePlannedFirstFrame, StateRole: StateRolePlannedEntry, Required: true, Minimum: 1, Maximum: 1}}
		strategy.anchors.plannerContract = multimodalPlannerContract
		strategy.references.videoRoles = append([]string{ReferenceRoleFirstFrame}, semanticRoles...)
		strategy.references.requiredVideoRoles = map[string]int{ReferenceRoleFirstFrame: 1}
		strategy.input = staticInputContractAdapter{
			initial: InputContractFirstFramePlusReferences, continuation: []string{InputContractVideoExtension, InputContractFirstFramePlusReferences},
			requiredRoles: map[string]int{ReferenceRoleFirstFrame: 1}, maximumRoles: map[string]int{ReferenceRoleFirstFrame: 1},
			semanticImageMaximum: 8, videoReferenceMaximum: 2, audioReferenceMaximum: 2,
		}
	case ProfileStoryboardSheet:
		strategy.anchors.requirements = []AnchorRequirement{
			{Role: AnchorRoleStoryboardSheet, StateRole: StateRolePlannedEntry, Required: true, Minimum: 1, Maximum: 1},
			{Role: AnchorRoleStoryboardPanel, Required: true, Minimum: 3, Maximum: 6},
		}
		strategy.anchors.plannerContract = storyboardSheetPlannerContract
		strategy.references.videoRoles = []string{ReferenceRoleStoryboardSheet}
		strategy.references.requiredVideoRoles = map[string]int{ReferenceRoleStoryboardSheet: 1}
		strategy.input = staticInputContractAdapter{
			initial: InputContractStoryboardSheetReference, continuation: []string{InputContractVideoExtension},
			requiredRoles: map[string]int{ReferenceRoleStoryboardSheet: 1}, maximumRoles: map[string]int{ReferenceRoleStoryboardSheet: 1},
		}
	}
	return strategy
}

func (strategy *builtInProfileStrategy) Key() string                        { return strategy.key }
func (strategy *builtInProfileStrategy) Anchors() AnchorStrategy            { return strategy.anchors }
func (strategy *builtInProfileStrategy) References() ReferenceStrategy      { return strategy.references }
func (strategy *builtInProfileStrategy) InputAdapter() InputContractAdapter { return strategy.input }
func (strategy *builtInProfileStrategy) Prompts() PromptContractStrategy    { return strategy.prompt }

type staticAnchorStrategy struct {
	requirements    []AnchorRequirement
	plannerContract string
}

func (strategy staticAnchorStrategy) Requirements() []AnchorRequirement {
	return append([]AnchorRequirement(nil), strategy.requirements...)
}

func (strategy staticAnchorStrategy) PlannerContract() string {
	return strings.TrimSpace(strategy.plannerContract)
}

func (strategy staticAnchorStrategy) ValidateReadyAnchors(counts map[string]int) error {
	for _, requirement := range strategy.requirements {
		count := counts[requirement.Role]
		if count < requirement.Minimum || (requirement.Maximum > 0 && count > requirement.Maximum) {
			return Error{Code: CodeReferencePackIncomplete, Message: fmt.Sprintf("锚点 %s 数量为 %d，要求 %d-%d", requirement.Role, count, requirement.Minimum, requirement.Maximum)}
		}
	}
	return nil
}

type staticReferenceStrategy struct {
	anchorRoles        []string
	videoRoles         []string
	requiredVideoRoles map[string]int
}

func (strategy staticReferenceStrategy) Allows(purpose, role string) bool {
	roles := strategy.anchorRoles
	if enumValue(purpose) == ReferencePurposeVideo {
		roles = strategy.videoRoles
	}
	return containsString(roles, enumValue(role))
}

func (strategy staticReferenceStrategy) Validate(purpose string, items []ReferencePackItem) error {
	if enumValue(purpose) != ReferencePurposeVideo {
		return nil
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[enumValue(item.Role)]++
	}
	for role, required := range strategy.requiredVideoRoles {
		if counts[role] != required {
			return Error{Code: CodeReferencePackIncomplete, Message: fmt.Sprintf("视频参考包要求 %s x%d，当前为 %d", role, required, counts[role])}
		}
	}
	return nil
}

type staticInputContractAdapter struct {
	initial               string
	continuation          []string
	requiredRoles         map[string]int
	maximumRoles          map[string]int
	semanticImageMaximum  int
	videoReferenceMaximum int
	audioReferenceMaximum int
}

func (adapter staticInputContractAdapter) InitialContract() string { return adapter.initial }
func (adapter staticInputContractAdapter) ContinuationContracts() []string {
	return append([]string(nil), adapter.continuation...)
}
func (adapter staticInputContractAdapter) ValidateReferenceRoles(items []ReferencePackItem) error {
	counts := map[string]int{}
	for _, item := range items {
		counts[enumValue(item.Role)]++
	}
	for role, minimum := range adapter.requiredRoles {
		if counts[role] < minimum {
			return Error{Code: CodeReferencePackIncomplete, Message: "输入契约缺少必需引用：" + role}
		}
	}
	for role, maximum := range adapter.maximumRoles {
		if maximum > 0 && counts[role] > maximum {
			return Error{Code: CodeReferencePackIncomplete, Message: "输入契约引用超过上限：" + role}
		}
	}
	semanticImages, videoReferences, audioReferences := 0, 0, 0
	for _, item := range items {
		mediaType := enumValue(item.MediaType)
		if mediaType == "" {
			mediaType = defaultReferenceMediaType(item.Role)
		}
		switch {
		case isSemanticImageReferenceRole(item.Role) && mediaType == "image":
			semanticImages++
		case enumValue(item.Role) == ReferenceRoleVideo || (enumValue(item.Role) == ReferenceRoleMotion && mediaType == "video"):
			videoReferences++
		case enumValue(item.Role) == ReferenceRoleAudio:
			audioReferences++
		}
	}
	for _, limit := range []struct {
		label   string
		count   int
		maximum int
	}{
		{label: "语义图片", count: semanticImages, maximum: adapter.semanticImageMaximum},
		{label: "参考视频", count: videoReferences, maximum: adapter.videoReferenceMaximum},
		{label: "参考音频", count: audioReferences, maximum: adapter.audioReferenceMaximum},
	} {
		if limit.maximum > 0 && limit.count > limit.maximum {
			return Error{Code: CodeReferencePackIncomplete, Message: fmt.Sprintf("输入契约%s数量 %d 超过上限 %d", limit.label, limit.count, limit.maximum)}
		}
	}
	return nil
}

func isSemanticImageReferenceRole(role string) bool {
	switch enumValue(role) {
	case ReferenceRoleCharacterIdentity, ReferenceRoleCharacterCostume,
		ReferenceRoleSceneIdentity, ReferenceRoleSceneSpatial, ReferenceRolePropIdentity,
		ReferenceRoleContinuityHint, ReferenceRoleStyle:
		return true
	default:
		return false
	}
}

type profilePromptStrategy struct{ profileKey string }

func (strategy profilePromptStrategy) TemplateKey(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if !validPromptRole(role) {
		return "", Error{Code: CodePromptContractIncomplete, Message: "不支持的 Prompt Contract role：" + role}
	}
	return fmt.Sprintf("video_profile.%s.%s", strategy.profileKey, role), nil
}
func (strategy profilePromptStrategy) ReviewImage(prompt string, cues []DialogueCue) PromptContractReview {
	return reviewImagePrompt(prompt, cues)
}
func (strategy profilePromptStrategy) ReviewVideo(prompt string, cues []DialogueCue, nativeAudioRequired, modelSupportsNativeAudio bool) PromptContractReview {
	return reviewVideoPrompt(prompt, cues, nativeAudioRequired, modelSupportsNativeAudio)
}

func ReviewImagePrompt(profileKey, prompt string, cues []DialogueCue) PromptContractReview {
	strategy, err := ProfileStrategyFor(profileKey)
	if err != nil {
		return failedPromptContractReview("PROFILE_STRATEGY_MISSING", err.Error())
	}
	return strategy.Prompts().ReviewImage(prompt, cues)
}

func ReviewVideoPrompt(profileKey, prompt string, cues []DialogueCue, nativeAudioRequired, modelSupportsNativeAudio bool) PromptContractReview {
	strategy, err := ProfileStrategyFor(profileKey)
	if err != nil {
		return failedPromptContractReview("PROFILE_STRATEGY_MISSING", err.Error())
	}
	return strategy.Prompts().ReviewVideo(prompt, cues, nativeAudioRequired, modelSupportsNativeAudio)
}

func failedPromptContractReview(code, message string) PromptContractReview {
	review := PromptContractReview{Approved: false, Checks: map[string]string{"profile": "failed"}, Issues: []ContractIssue{}}
	review.Issues = append(review.Issues, ContractIssue{Code: code, Field: "profile", Message: message})
	return review
}

func reviewImagePrompt(prompt string, cues []DialogueCue) PromptContractReview {
	return ReviewSingleFrameImagePrompt(prompt, cues)
}

func reviewVideoPrompt(prompt string, cues []DialogueCue, nativeAudioRequired, modelSupportsNativeAudio bool) PromptContractReview {
	return ReviewSingleFrameVideoPrompt(prompt, cues, nativeAudioRequired, modelSupportsNativeAudio)
}

func promptContractField(role string) string {
	switch role {
	case PromptRoleAnchorPlan:
		return "anchorPlan"
	case PromptRoleAnchorGenerate:
		return "anchorGenerate"
	case PromptRoleAnchorReview:
		return "anchorReview"
	case PromptRoleVideoGenerate:
		return "videoGenerate"
	case PromptRoleVideoReview:
		return "videoReview"
	default:
		return ""
	}
}

func decodeProfileObject(raw json.RawMessage, field string) (map[string]any, error) {
	result := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil {
		return nil, Error{Code: CodeProfileIncompatible, Message: "Profile " + field + " 必须是对象"}
	}
	return result, nil
}

func decodeProfileStringMap(raw json.RawMessage, field string) (map[string]string, error) {
	result := map[string]string{}
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil {
		return nil, Error{Code: CodePromptContractIncomplete, Message: "Profile " + field + " 必须是字符串对象"}
	}
	return result, nil
}

func stringSliceValue(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if typed, typedOK := value.([]string); typedOK {
			return append([]string(nil), typed...)
		}
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text := strings.TrimSpace(stringValueAny(item)); text != "" {
			result = append(result, text)
		}
	}
	sort.Strings(result)
	return result
}

func stringValueAny(value any) string {
	text, _ := value.(string)
	return text
}
