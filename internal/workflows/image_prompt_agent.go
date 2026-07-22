package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

const (
	nodeGenerateShotImagePromptPrefix = "generate_shot_image_prompt"
	nodeReviewShotImagePromptPrefix   = "review_shot_image_prompt"
	promptKeyShotImageAgent           = "shot_image_prompt_agent"
	promptKeyShotImageReviewAgent     = "shot_image_prompt_review_agent"
	maxReviewedShotImagePromptBytes   = 12000
	maxShotImageNegativePromptRunes   = 800
)

type PrepareShotImagePromptInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	ShotID     string `json:"shotId"`
	ShotIndex  int    `json:"shotIndex"`
	ShotNo     int    `json:"shotNo"`
	AnchorRole string `json:"anchorRole,omitempty"`

	WorkflowPrompt string `json:"workflowPrompt"`
	AspectRatio    string `json:"aspectRatio"`
	Size           string `json:"size"`
	Force          bool   `json:"force,omitempty"`
}

type PrepareShotImagePromptOutput struct {
	ShotID                   string                                     `json:"shotId"`
	AnchorRole               string                                     `json:"anchorRole"`
	Prompt                   string                                     `json:"prompt"`
	NegativePrompt           string                                     `json:"negativePrompt,omitempty"`
	PromptHash               string                                     `json:"promptHash"`
	GenerationProviderCallID string                                     `json:"generationProviderCallId"`
	GenerationModelID        string                                     `json:"generationModelId"`
	GenerationTemplateKey    string                                     `json:"generationTemplateKey"`
	GenerationPromptVersion  string                                     `json:"generationPromptVersionId"`
	ReviewProviderCallID     string                                     `json:"reviewProviderCallId"`
	ReviewModelID            string                                     `json:"reviewModelId"`
	ReviewTemplateKey        string                                     `json:"reviewTemplateKey"`
	ReviewPromptVersion      string                                     `json:"reviewPromptVersionId"`
	ImageProviderModelID     string                                     `json:"imageProviderModelId,omitempty"`
	ModelCandidates          []provider.GatewayModelConstraintCandidate `json:"modelCandidates"`
	PromptMeasurements       map[string]int                             `json:"promptMeasurements"`
	DialogueLines            []StoryboardDialogueLine                   `json:"dialogueLines,omitempty"`
	ReferencePackID          string                                     `json:"referencePackId,omitempty"`
	ReferencePackHash        string                                     `json:"referencePackHash,omitempty"`
	CapabilitySnapshotHash   string                                     `json:"capabilitySnapshotHash,omitempty"`
	PromptContextPlanID      string                                     `json:"promptContextPlanId,omitempty"`
	PromptContextPlanHash    string                                     `json:"promptContextPlanHash,omitempty"`
	GenerationContract       *videoproduction.PromptContractProvenance  `json:"generationContract,omitempty"`
	ReviewContract           *videoproduction.PromptContractProvenance  `json:"reviewContract,omitempty"`
	Attempts                 []ShotImagePromptAttemptTrace              `json:"attempts,omitempty"`
}

type shotImagePromptAgentContext struct {
	AnchorRole        string                                 `json:"anchorRole"`
	Project           shotVideoPromptProject                 `json:"project"`
	Source            shotVideoPromptSource                  `json:"source"`
	Script            shotVideoPromptScript                  `json:"script"`
	Scene             shotVideoPromptScene                   `json:"scene"`
	Shot              shotImagePromptShot                    `json:"shot"`
	Assets            []ShotVideoPromptAsset                 `json:"assets"`
	ImageModels       []imagePromptModelContext              `json:"imageModels"`
	ReferenceMode     string                                 `json:"referenceMode"`
	ReferenceKeys     []string                               `json:"referenceKeys"`
	ShotState         *videoproduction.ShotState             `json:"shotState,omitempty"`
	Transition        *videoproduction.ShotTransition        `json:"transition,omitempty"`
	ReferencePack     *videoproduction.ReferencePackManifest `json:"referencePack,omitempty"`
	PromptContextPlan *videoproduction.PromptContextPlan     `json:"promptContextPlan,omitempty"`
	PanelManifest     *videoproduction.PanelManifest         `json:"panelManifest,omitempty"`
}

type shotImagePromptShot struct {
	ShotID         string                   `json:"shotId"`
	ShotNo         int                      `json:"shotNo"`
	AnchorRole     string                   `json:"anchorRole"`
	Title          string                   `json:"title,omitempty"`
	Visual         string                   `json:"visual,omitempty"`
	Camera         string                   `json:"camera,omitempty"`
	Motion         string                   `json:"motion,omitempty"`
	Mood           string                   `json:"mood,omitempty"`
	AspectRatio    string                   `json:"aspectRatio"`
	Size           string                   `json:"size"`
	ExistingPrompt string                   `json:"existingPrompt,omitempty"`
	ScriptDialogue []StoryboardDialogueLine `json:"scriptDialogue"`
	LockedFacts    shotImageLockedFacts     `json:"lockedFacts"`
}

type shotImageLockedFacts struct {
	Title       string                   `json:"title,omitempty"`
	Visual      string                   `json:"visual,omitempty"`
	Camera      string                   `json:"camera,omitempty"`
	Motion      string                   `json:"motion,omitempty"`
	Mood        string                   `json:"mood,omitempty"`
	Dialogue    []StoryboardDialogueLine `json:"dialogue"`
	AspectRatio string                   `json:"aspectRatio"`
}

type imagePromptModelContext struct {
	ProviderModelID string `json:"providerModelId"`
	ModelKey        string `json:"modelKey"`
	MaxLength       int    `json:"maxLength,omitempty"`
	LengthUnit      string `json:"lengthUnit,omitempty"`
	TargetLength    int    `json:"targetLength,omitempty"`
}

type generatedImagePrompt struct {
	Prompt            string                   `json:"prompt"`
	NegativePrompt    string                   `json:"negativePrompt"`
	DialogueLines     []StoryboardDialogueLine `json:"dialogueLines"`
	SourceAnchors     agentJSONList            `json:"sourceAnchors"`
	AssetAnchors      agentJSONList            `json:"assetAnchors"`
	ConflictsResolved agentJSONList            `json:"conflictsResolved"`
}

type reviewedImagePrompt struct {
	Approved       bool                     `json:"approved"`
	Prompt         string                   `json:"prompt"`
	FinalPrompt    string                   `json:"finalPrompt"`
	NegativePrompt string                   `json:"negativePrompt"`
	DialogueLines  []StoryboardDialogueLine `json:"dialogueLines"`
	SourceAnchors  agentJSONList            `json:"sourceAnchors"`
	Issues         agentJSONList            `json:"issues"`
	Changes        agentJSONList            `json:"changes"`
}

func resolveShotAnchorRole(profileKey, requested string) (string, error) {
	role := strings.TrimSpace(requested)
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return "", err
	}
	if role == "" {
		for _, requirement := range strategy.Anchors().Requirements() {
			if requirement.Required && requirement.Role != videoproduction.AnchorRoleStoryboardPanel {
				role = requirement.Role
				break
			}
		}
	}
	for _, requirement := range strategy.Anchors().Requirements() {
		if requirement.Role == role {
			if role == videoproduction.AnchorRoleStoryboardPanel {
				return "", videoproduction.Error{
					Code: videoproduction.CodeProfileIncompatible, Message: "分镜板画格由已审核分镜板确定性裁切生成，不能独立调用图片模型",
				}
			}
			return role, nil
		}
	}
	return "", videoproduction.Error{
		Code:    videoproduction.CodeProfileIncompatible,
		Message: "当前视频生产方案不支持锚点角色：" + role,
	}
}

func (a Activities) PrepareShotImagePrompt(ctx context.Context, input PrepareShotImagePromptInput) (PrepareShotImagePromptOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.WorkflowPrompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return PrepareShotImagePromptOutput{}, err
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return PrepareShotImagePromptOutput{}, err
	}
	fail := func(execution NodeExecution, cause error) error {
		return a.failShotImagePromptActivity(ctx, input, shot, execution, cause)
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	anchorRole, err := resolveShotAnchorRole(project.VideoProductionProfileKey, input.AnchorRole)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
	}
	input.AnchorRole = anchorRole
	if a.gateway == nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	constraints, err := a.gateway.ResolveModelConstraints(ctx, provider.GatewayModelConstraintsRequest{
		OrganizationID:  input.OrganizationID,
		ModelProfileKey: project.ImageModelProfileKey,
		TaskType:        provider.TaskTypeImageGenerate,
		Modality:        "image",
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowErrorFromProvider(err, codeActivityFailed))
	}
	imageProviderModelID := ""
	if project.VideoProductionProfileKey == videoproduction.ProfileStoryboardSheet {
		candidate, ok := selectStoryboardSheetImageModel(constraints.Candidates)
		if !ok {
			return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{
				Code: provider.CodeModelCapabilityUnavailable, Message: "分镜板模式要求图片业务模型绑定可用的 gpt-image-2",
			})
		}
		imageProviderModelID = candidate.ProviderModelID
		constraints.Candidates = []provider.GatewayModelConstraintCandidate{candidate}
	}
	existingPrompt, err := a.reviewedShotImagePrompt(ctx, shot.ID, anchorRole)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if project.VideoProductionProfileKey != videoproduction.ProfileStoryboardSheet && !input.Force && existingPrompt.Status == "succeeded" && strings.TrimSpace(existingPrompt.Prompt) != "" {
		return PrepareShotImagePromptOutput{
			ShotID:     shot.ID,
			AnchorRole: anchorRole,
			Prompt:     existingPrompt.Prompt,
			PromptHash: firstNonEmptyString(existingPrompt.PromptHash, promptsvc.HashText(existingPrompt.Prompt)),
		}, nil
	}
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	var productionContract shotProductionContractContext
	var referencePack videoproduction.ReferencePack
	var promptContextPlan videoproduction.PromptContextPlan
	productionContract, err = a.loadShotProductionContract(ctx, input.ProjectID, shot.ID, anchorRole)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: videoproduction.CodePromptContractIncomplete, Message: err.Error()})
	}
	var panelManifest *videoproduction.PanelManifest
	if project.VideoProductionProfileKey == videoproduction.ProfileStoryboardSheet {
		manifestRuntime, manifestErr := a.ensureStoryboardSheetManifest(ctx, input, project, shot, productionContract)
		if manifestErr != nil {
			return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, manifestErr)
		}
		panelManifest = &manifestRuntime.Manifest
		input.AspectRatio = panelManifest.SheetAspectRatio
		input.Size = storyboardImageSizeForAspectRatio(panelManifest.SheetAspectRatio)
	}
	referenceCandidates, loadErr := a.loadShotReferenceCandidates(ctx, input.ProjectID, shot.ID, productionContract.AnchorState)
	if loadErr != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, loadErr)
	}
	referencePack, err = resolveAnchorReferencePack(project, productionContract, referenceCandidates, constraints.Candidates)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
	}
	textConstraints, constraintsErr := a.gateway.ResolveModelConstraints(ctx, provider.GatewayModelConstraintsRequest{
		OrganizationID:  input.OrganizationID,
		ModelProfileKey: project.ScriptModelProfileKey,
		TaskType:        provider.TaskTypeTextGenerate,
		Modality:        "text",
	})
	if constraintsErr != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowErrorFromProvider(constraintsErr, codeActivityFailed))
	}
	promptContextPlan, err = a.compileShotPromptContextPlan(ctx, project, shot, productionContract.AnchorState, textConstraints.Candidates)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
	}
	agentContext, err := a.loadShotImagePromptAgentContext(ctx, project, shot, assetContext, constraints.Candidates, input)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	agentContext.AnchorRole = anchorRole
	agentContext.Shot.AnchorRole = anchorRole
	agentContext.ShotState = &productionContract.AnchorState
	agentContext.Transition = &productionContract.Transition
	agentContext.ReferencePack = &referencePack.Manifest
	agentContext.PromptContextPlan = &promptContextPlan
	agentContext.PanelManifest = panelManifest
	agentContext.Source.Content = ""
	agentContext.Script.Content = ""
	agentContext.Scene.Content = ""
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
	}

	return a.runShotImagePromptReviewLoop(
		ctx, input, project, shot, agentContext, productionContract, referencePack,
		promptContextPlan, panelManifest, imageProviderModelID, constraints.Candidates, fail,
	)
}

func (a Activities) loadShotImagePromptAgentContext(ctx context.Context, project ProjectProductionSettings, shot StoryboardShotRecord, assets ShotAssetContext, candidates []provider.GatewayModelConstraintCandidate, input PrepareShotImagePromptInput) (shotImagePromptAgentContext, error) {
	aspectRatio := firstNonEmptyString(input.AspectRatio, project.VideoRatio, project.AspectRatio, "16:9")
	existingPrompt := ""
	if shot.ImagePromptStatus == "succeeded" {
		existingPrompt = compactImageContextText(shot.ImagePrompt, 1600)
	}
	contextValue := shotImagePromptAgentContext{
		Project: shotVideoPromptProject{
			ProjectType:               project.ProjectType,
			ContentType:               project.ContentType,
			VideoProductionProfileKey: project.VideoProductionProfileKey,
			ArtStyle:                  project.ArtStyle,
			AspectRatio:               aspectRatio,
			DirectorManual:            compactImageContextText(project.DirectorManual, 8000),
			VisualManual:              compactImageContextText(project.VisualManual, 8000),
		},
		Shot: shotImagePromptShot{
			ShotID:         shot.ID,
			ShotNo:         shot.ShotNo,
			Title:          shot.Title,
			Visual:         shot.Visual,
			Camera:         shot.Camera,
			Motion:         shot.Motion,
			Mood:           shot.Mood,
			AspectRatio:    aspectRatio,
			Size:           firstNonEmptyString(input.Size, storyboardImageSizeForAspectRatio(aspectRatio)),
			ExistingPrompt: existingPrompt,
			ScriptDialogue: append([]StoryboardDialogueLine(nil), shot.Dialogue...),
			LockedFacts: shotImageLockedFacts{
				Title:       shot.Title,
				Visual:      shot.Visual,
				Camera:      shot.Camera,
				Motion:      shot.Motion,
				Mood:        shot.Mood,
				Dialogue:    append([]StoryboardDialogueLine(nil), shot.Dialogue...),
				AspectRatio: aspectRatio,
			},
		},
		Assets:        compactShotImagePromptAssets(assets.PromptAssets),
		ImageModels:   imagePromptModelContexts(candidates),
		ReferenceMode: assets.ImageReferenceMode,
		ReferenceKeys: assets.ResolvedReferenceKeys,
	}
	narrative, err := a.loadShotPromptNarrativeContext(ctx, project.ID, shot.ID)
	if err != nil {
		return shotImagePromptAgentContext{}, err
	}
	contextValue.Source = narrative.Source
	contextValue.Script = narrative.Script
	contextValue.Scene = narrative.Scene
	return contextValue, nil
}

func compactShotImagePromptAssets(values []ShotVideoPromptAsset) []ShotVideoPromptAsset {
	result := make([]ShotVideoPromptAsset, 0, len(values))
	for _, value := range values {
		requirement := map[string]any{}
		for _, key := range []string{"type", "role", "costume", "pose", "expression", "action", "cameraRelation", "sceneState", "propState", "prompt"} {
			text := compactImageContextText(fmt.Sprint(value.Requirement[key]), 500)
			if text != "" && text != "<nil>" {
				requirement[key] = text
			}
		}
		result = append(result, ShotVideoPromptAsset{
			AssetID:           value.AssetID,
			AssetType:         value.AssetType,
			Name:              value.Name,
			Description:       compactImageContextText(value.Description, 500),
			ConsistencyPrompt: compactImageContextText(value.ConsistencyPrompt, 1000),
			NegativePrompt:    compactImageContextText(value.NegativePrompt, 300),
			Requirement:       requirement,
		})
	}
	return result
}

func compactImageContextText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func imagePromptModelContexts(candidates []provider.GatewayModelConstraintCandidate) []imagePromptModelContext {
	result := make([]imagePromptModelContext, 0, len(candidates))
	for _, candidate := range candidates {
		target := 0
		if candidate.Prompt.MaxLength > 0 {
			target = candidate.Prompt.MaxLength * 80 / 100
		}
		result = append(result, imagePromptModelContext{
			ProviderModelID: candidate.ProviderModelID,
			ModelKey:        candidate.ModelKey,
			MaxLength:       candidate.Prompt.MaxLength,
			LengthUnit:      candidate.Prompt.Unit,
			TargetLength:    target,
		})
	}
	return result
}

func parseGeneratedImagePrompt(text string) (generatedImagePrompt, error) {
	var output generatedImagePrompt
	if err := decodeAgentJSONObject(text, &output); err != nil {
		return generatedImagePrompt{}, fmt.Errorf("image prompt agent returned invalid JSON: %w", err)
	}
	output.Prompt = strings.TrimSpace(output.Prompt)
	output.NegativePrompt = strings.TrimSpace(output.NegativePrompt)
	output.DialogueLines = NormalizeStoryboardDialogue(output.DialogueLines)
	if output.Prompt == "" {
		return generatedImagePrompt{}, fmt.Errorf("image prompt agent returned an empty prompt")
	}
	return output, nil
}

func parseReviewedImagePrompt(text string, draft generatedImagePrompt) (reviewedImagePrompt, error) {
	var output reviewedImagePrompt
	if err := decodeAgentJSONObject(text, &output); err != nil {
		return reviewedImagePrompt{}, fmt.Errorf("image prompt review agent returned invalid JSON: %w", err)
	}
	output.Prompt = strings.TrimSpace(output.Prompt)
	output.FinalPrompt = strings.TrimSpace(output.FinalPrompt)
	output.NegativePrompt = strings.TrimSpace(output.NegativePrompt)
	output.DialogueLines = NormalizeStoryboardDialogue(output.DialogueLines)
	if output.Approved && firstNonEmptyString(output.FinalPrompt, output.Prompt) == "" {
		return reviewedImagePrompt{}, fmt.Errorf("image prompt review agent returned an empty prompt")
	}
	if !output.Approved && firstNonEmptyString(output.FinalPrompt, output.Prompt) == "" && len(output.Issues) == 0 && len(output.Changes) == 0 {
		return reviewedImagePrompt{}, fmt.Errorf("image prompt review agent rejected the candidate without correction details")
	}
	if output.NegativePrompt == "" {
		output.NegativePrompt = draft.NegativePrompt
	}
	if len(output.DialogueLines) == 0 {
		output.DialogueLines = append([]StoryboardDialogueLine(nil), draft.DialogueLines...)
	}
	return output, nil
}

func validateShotImagePromptContainsNoDialogue(prompt string, dialogue []StoryboardDialogueLine) error {
	for _, line := range NormalizeStoryboardDialogue(dialogue) {
		if text := strings.TrimSpace(line.Text); text != "" && strings.Contains(prompt, text) {
			return workflowError{Code: codeImagePromptDialogueLeak, Message: "图片提示词包含了剧本中的原始台词，必须只保留可见画面和表演状态", RetryabilityKnown: true}
		}
	}
	if containsAffirmativeDialogueMetadata(prompt) {
		return workflowError{Code: codeImagePromptDialogueLeak, Message: "图片提示词包含了明确的台词或对白字段，必须改写为纯视觉描述", RetryabilityKnown: true}
	}
	return nil
}

func validateShotImagePromptProviderSafety(prompt string) error {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	for _, term := range []string{
		"黏稠暗红血", "血泊", "滴血", "血珠", "浸透暗红血", "浸透血迹",
		"干涸暗红血痕", "暗红血痕", "血液飞溅", "大面积血污", "喷溅血腥",
		"流血", "血袍", "尸体", "骸骨", "肢体残留", "残肢", "断肢", "内脏", "开膛",
		"肢解", "血肉模糊", "开放性伤口", "开放伤口", "喷溅的血", "喷血", "人体残留",
		"自爆", "自毁", "同归于尽", "毁灭", "爆炸", "爆发", "吞没", "吞卷", "巨响",
		"gore", "dismember", "exposed wound", "self-destruct", "detonation",
	} {
		if strings.Contains(normalized, term) {
			return workflowError{
				Code:              codeImagePromptSafetyRisk,
				Message:           fmt.Sprintf("图片提示词包含上游模型容易拒绝的图形化伤害细节 %q", term),
				RetryabilityKnown: true,
			}
		}
	}
	return nil
}

func providerSafeShotImageContext(value string) string {
	return strings.NewReplacer(
		"同归于尽", "决绝告别",
		"自我毁灭", "完成时空转场",
		"自毁", "时空转场",
		"自爆", "光场转场",
		"一次完整爆发", "一次完整绽放",
		"向外爆发", "向外绽放",
		"爆发起始", "绽放起始",
		"爆发前", "绽放前",
		"爆发光", "扩散光场",
		"爆炸", "光场扩散",
		"爆发", "绽放",
		"吞卷", "铺展",
		"吞没", "逐渐覆盖",
		"毁灭", "告别",
		"巨响", "无声过渡",
		"冲击光", "扩散光",
		"冲击", "扩散",
		"人体残留", "令人不适的人体细节",
		"黏稠暗红血泊", "战后岩地上的暗色污迹",
		"中央暗红血泊", "中央战后暗色岩地",
		"暗红血泊", "战后暗色岩地",
		"血泊", "战后暗色岩地",
		"滴血碧绿衣摆", "破损污损的碧绿衣摆",
		"滴血衣摆", "破损污损的衣摆",
		"血珠持续滴落", "衣摆污痕随风轻颤",
		"血珠将从衣摆滴落", "衣摆污痕随风轻颤",
		"血珠", "衣摆暗色污痕",
		"破损血袍", "严重战损的长袍",
		"染血长袍", "严重战损的长袍",
		"血袍", "战损长袍",
		"浸透暗红血迹", "严重战损并带暗色污痕",
		"浸透血迹", "严重战损并带暗色污痕",
		"干涸暗红血痕", "克制的暗色战损污痕",
		"暗红血痕", "克制的暗色战损污痕",
		"血液飞溅", "过度写实的伤害细节",
		"大面积血污", "过度写实的伤害细节",
		"喷溅血腥", "过度写实的伤害细节",
		"开放性伤口", "过度写实的伤害细节",
		"开放伤口", "过度写实的伤害细节",
		"肢体残留", "过度写实的伤害细节",
		"尸体", "远景倒地轮廓",
		"骸骨", "战后远景残留",
		"流血", "明显伤害细节",
		"染血的", "严重战损的",
		"染血", "战损污痕",
		"尸体、残肢、血腥人体细节", "任何令人不适或过度写实的伤害细节",
		"血腥人体细节", "过度写实的伤害细节",
		"残肢", "过度写实的伤害细节",
		"断肢", "过度写实的伤害细节",
		"内脏", "过度写实的伤害细节",
		"血腥", "过度写实的伤害",
	).Replace(strings.TrimSpace(value))
}

func stripScriptDialogueFromImagePrompt(prompt string, dialogue []StoryboardDialogueLine) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	filteredLines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Performance context only -") || strings.Contains(trimmed, "speaks in Chinese:") {
			continue
		}
		for _, dialogueLine := range NormalizeStoryboardDialogue(dialogue) {
			text := strings.TrimSpace(dialogueLine.Text)
			if text == "" {
				continue
			}
			for _, quoted := range []string{"“" + text + "”", "\"" + text + "\"", "‘" + text + "’", "'" + text + "'", text} {
				line = strings.ReplaceAll(line, quoted, "")
			}
		}
		line = strings.NewReplacer("“”", "", "‘’", "", "\"\"", "", "''", "").Replace(line)
		if strings.TrimSpace(line) != "" {
			filteredLines = append(filteredLines, strings.TrimSpace(line))
		}
	}
	return stripDialogueMetadataSentences(strings.TrimSpace(strings.Join(filteredLines, "\n")))
}

func stripDialogueMetadataSentences(value string) string {
	var result strings.Builder
	var sentence strings.Builder
	flush := func() {
		text := sentence.String()
		sentence.Reset()
		lower := strings.ToLower(text)
		for _, token := range []string{"台词", "对白", "dialogue", "spoken words", "speech text"} {
			if strings.Contains(lower, token) {
				return
			}
		}
		result.WriteString(text)
	}
	for _, char := range value {
		sentence.WriteRune(char)
		if strings.ContainsRune("。！？.!?\n", char) {
			flush()
		}
	}
	flush()
	return strings.TrimSpace(result.String())
}

func buildReviewedShotImagePrompt(reviewedPrompt, negativePrompt string, contextValue shotImagePromptAgentContext) string {
	sections := []string{strings.TrimSpace(reviewedPrompt)}
	locked := []string{"SOURCE-LOCKED SHOT FACTS - do not alter:"}
	dialogue := contextValue.Shot.ScriptDialogue
	if value := stripScriptDialogueFromImagePrompt(contextValue.Shot.Visual, dialogue); value != "" {
		locked = append(locked, "Visual: "+providerSafeShotImageContext(value))
	}
	if value := stripScriptDialogueFromImagePrompt(contextValue.Shot.Camera, dialogue); value != "" {
		locked = append(locked, "Camera/composition: "+providerSafeShotImageContext(value))
	}
	if value := stripScriptDialogueFromImagePrompt(contextValue.Shot.Motion, dialogue); value != "" {
		locked = append(locked, "Single-frame motion implication: "+providerSafeShotImageContext(value))
	}
	if value := stripScriptDialogueFromImagePrompt(contextValue.Shot.Mood, dialogue); value != "" {
		locked = append(locked, "Mood: "+providerSafeShotImageContext(value))
	}
	locked = append(locked, "Output aspect ratio: "+contextValue.Shot.AspectRatio+". No on-screen text, subtitles, captions, speech bubbles, watermarks, logos, UI, contact sheet, or collage.")
	sections = append(sections, strings.Join(locked, "\n"))
	if negativePrompt != "" {
		sections = append(sections, "Scene-specific negative constraints: "+providerSafeShotImageContext(negativePrompt))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func compactShotImageNegativePrompt(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxShotImageNegativePromptRunes {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:maxShotImageNegativePromptRunes]))
}

func validateShotImagePromptForCandidates(prompt string, candidates []provider.GatewayModelConstraintCandidate) (map[string]int, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "reviewed image prompt is empty"}
	}
	if strings.Contains(prompt, `"forbiddenChanges"`) || strings.Contains(prompt, `"baseClothing"`) || strings.Contains(prompt, "## 一、") {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "reviewed image prompt copied raw asset or manual content"}
	}
	byteLength := len([]byte(prompt))
	if byteLength > maxReviewedShotImagePromptBytes {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: fmt.Sprintf("reviewed image prompt length %d exceeds compact runtime limit of %d utf8 bytes", byteLength, maxReviewedShotImagePromptBytes)}
	}
	measurements := map[string]int{"runtime:utf8_bytes": byteLength}
	for _, candidate := range candidates {
		unit := candidate.Prompt.Unit
		if unit == "" {
			unit = provider.PromptLengthUnitCharacters
		}
		length := provider.MeasurePromptLength(prompt, unit)
		measurements[candidate.ModelKey+":"+unit] = length
		if candidate.Prompt.MaxLength > 0 && length > candidate.Prompt.MaxLength {
			return nil, workflowError{Code: provider.CodeInvalidRequest, Message: fmt.Sprintf("reviewed image prompt length %d exceeds %s limit of %d %s", length, candidate.ModelKey, candidate.Prompt.MaxLength, unit)}
		}
	}
	return measurements, nil
}

func (a Activities) persistReviewedShotImagePrompt(ctx context.Context, input PrepareShotImagePromptInput, project ProjectProductionSettings, shot StoryboardShotRecord, execution NodeExecution, output PrepareShotImagePromptOutput, review reviewedImagePrompt, draft generatedImagePrompt) error {
	dialogueBackfilled := len(shot.Dialogue) == 0 && len(output.DialogueLines) > 0
	metadata := mustJSON(map[string]any{
		"generationContract":     output.GenerationContract,
		"reviewContract":         output.ReviewContract,
		"referencePackHash":      output.ReferencePackHash,
		"capabilitySnapshotHash": output.CapabilitySnapshotHash,
		"promptContextPlanHash":  output.PromptContextPlanHash,
		"imagePromptAgent": map[string]any{
			"status":                    "approved",
			"generationProviderCallId":  output.GenerationProviderCallID,
			"generationModelId":         output.GenerationModelID,
			"generationTemplateKey":     output.GenerationTemplateKey,
			"generationPromptVersionId": output.GenerationPromptVersion,
			"reviewProviderCallId":      output.ReviewProviderCallID,
			"reviewModelId":             output.ReviewModelID,
			"reviewTemplateKey":         output.ReviewTemplateKey,
			"reviewPromptVersionId":     output.ReviewPromptVersion,
			"imageProviderModelId":      output.ImageProviderModelID,
			"promptHash":                output.PromptHash,
			"negativePrompt":            output.NegativePrompt,
			"modelCandidates":           output.ModelCandidates,
			"promptMeasurements":        output.PromptMeasurements,
			"dialogueLines":             output.DialogueLines,
			"sourceAnchors":             review.SourceAnchors,
			"assetAnchors":              draft.AssetAnchors,
			"conflictsResolved":         draft.ConflictsResolved,
			"issues":                    review.Issues,
			"changes":                   review.Changes,
			"dialogueBackfilled":        dialogueBackfilled,
			"referencePackId":           output.ReferencePackID,
			"referencePackHash":         output.ReferencePackHash,
			"capabilitySnapshotHash":    output.CapabilitySnapshotHash,
			"promptContextPlanId":       output.PromptContextPlanID,
			"promptContextPlanHash":     output.PromptContextPlanHash,
			"generationContract":        output.GenerationContract,
			"reviewContract":            output.ReviewContract,
			"attempts":                  output.Attempts,
		},
	})
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	type anchorPromptTarget struct {
		ID         string
		Status     string
		ArtifactID string
	}
	var target anchorPromptTarget
	err = tx.QueryRow(ctx, `
		SELECT id::text, status, COALESCE(artifact_id::text, '')
		FROM shot_visual_anchors
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND anchor_role = $3
		ORDER BY revision DESC
		LIMIT 1
		FOR UPDATE
	`, input.ProjectID, shot.ID, input.AnchorRole).Scan(&target.ID, &target.Status, &target.ArtifactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return workflowError{Code: videoproduction.CodePromptContractIncomplete, Message: "当前镜头缺少待生成的视觉锚点，请先重新生成分镜"}
	}
	if err != nil {
		return err
	}
	if target.ArtifactID != "" || target.Status == "ready" || target.Status == "stale" || target.Status == "archived" {
		previousID := target.ID
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'stale', review_status = 'needs_edit', reference_pack_id = NULL,
			    metadata = metadata || jsonb_build_object('supersededAt', now(), 'supersededReason', 'anchor_prompt_regenerated')
			WHERE id = $1
		`, previousID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status,
				reference_pack_id, metadata
			)
			SELECT organization_id, project_id, production_generation_id, storyboard_shot_id,
			       shot_state_version_id, anchor_role, revision + 1, 'draft', 'pending',
			       NULLIF($3, '')::uuid,
			       jsonb_build_object(
			         'workflowRunId', $2::text,
			         'source', 'anchor_prompt_regeneration',
			         'previousAnchorId', id::text
			       )
			FROM shot_visual_anchors
			WHERE id = $1
			RETURNING id::text, status, COALESCE(artifact_id::text, '')
		`, previousID, input.WorkflowRunID, output.ReferencePackID).Scan(&target.ID, &target.Status, &target.ArtifactID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET prompt = $2,
		    prompt_version_id = NULLIF($3, '')::uuid,
		    prompt_hash = NULLIF(regexp_replace($4, '^sha256:', ''), ''),
		    reference_pack_id = NULLIF($5, '')::uuid,
		    status = CASE WHEN artifact_id IS NULL THEN 'draft' ELSE status END,
		    review_status = CASE WHEN artifact_id IS NULL THEN 'pending' ELSE review_status END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || $6::jsonb
		WHERE id = $1
	`, target.ID, output.Prompt, output.GenerationPromptVersion, output.PromptHash,
		output.ReferencePackID, metadata); err != nil {
		return err
	}
	requiredRoles, err := requiredProfileAnchorRoles(project.VideoProductionProfileKey)
	if err != nil {
		return err
	}
	var readyPromptCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT DISTINCT ON (anchor_role) anchor_role, prompt, prompt_hash
			FROM shot_visual_anchors
			WHERE storyboard_shot_id = $1
			  AND anchor_role = ANY($2::text[])
			  AND status <> 'archived'
			ORDER BY anchor_role, revision DESC
		) latest
		WHERE COALESCE(prompt, '') <> '' AND prompt_hash IS NOT NULL
	`, shot.ID, requiredRoles).Scan(&readyPromptCount); err != nil {
		return err
	}
	promptStatus := "running"
	if readyPromptCount == len(requiredRoles) {
		promptStatus = "succeeded"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_prompt = CASE WHEN $7 = 'planned_first_frame' THEN $2 ELSE image_prompt END,
		    script_dialogue = CASE
		      WHEN jsonb_array_length(script_dialogue) = 0 AND jsonb_array_length($6::jsonb) > 0 THEN $6::jsonb
		      ELSE script_dialogue
		    END,
		    metadata = jsonb_set(
		      COALESCE(metadata, '{}'::jsonb),
		      '{anchorPromptAgents}',
		      COALESCE(metadata->'anchorPromptAgents', '{}'::jsonb)
		        || jsonb_build_object($7::text, $3::jsonb->'imagePromptAgent'),
		      true
		    ) || CASE WHEN $7 = 'planned_first_frame' THEN $3::jsonb ELSE '{}'::jsonb END,
		    image_prompt_status = $8,
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULLIF($5, '')::uuid,
		    image_prompt_updated_at = now(),
		    image_error_code = NULL,
		    image_error_message = NULL,
		    updated_at = now()
		WHERE project_id = $1 AND id = $4 AND deleted_at IS NULL
	`, input.ProjectID, output.Prompt, metadata, shot.ID, input.WorkflowRunID,
		mustJSON(output.DialogueLines), input.AnchorRole, promptStatus); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image_prompt.reviewed", "storyboard_shot", shot.ID, mustJSON(map[string]any{
		"workflowRunId":            input.WorkflowRunID,
		"shotId":                   shot.ID,
		"shotNo":                   shot.ShotNo,
		"anchorRole":               input.AnchorRole,
		"anchorId":                 target.ID,
		"aggregateStatus":          promptStatus,
		"generationProviderCallId": output.GenerationProviderCallID,
		"reviewProviderCallId":     output.ReviewProviderCallID,
		"promptHash":               output.PromptHash,
		"promptMeasurements":       output.PromptMeasurements,
		"dialogueLines":            output.DialogueLines,
		"dialogueBackfilled":       dialogueBackfilled,
	})); err != nil {
		return err
	}
	if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) markShotImagePromptRunning(ctx context.Context, input PrepareShotImagePromptInput, shot StoryboardShotRecord, execution NodeExecution) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, execution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'imagePromptAgent', COALESCE(metadata->'imagePromptAgent', '{}'::jsonb)
		        || jsonb_build_object('status', 'running', 'workflowRunId', $3::text, 'startedAt', now())
		    )
		WHERE id = (
			SELECT id FROM shot_visual_anchors
			WHERE storyboard_shot_id = $1 AND anchor_role = $2 AND status <> 'archived'
			ORDER BY revision DESC LIMIT 1
		)
	`, shot.ID, input.AnchorRole, input.WorkflowRunID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_prompt_status = 'running',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULLIF($2, '')::uuid,
		    image_prompt_updated_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $3 AND deleted_at IS NULL
	`, input.ProjectID, input.WorkflowRunID, shot.ID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image_prompt.running", "storyboard_shot", shot.ID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "shotNo": shot.ShotNo, "anchorRole": input.AnchorRole, "status": "image_prompt_running",
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) failShotImagePromptActivity(ctx context.Context, input PrepareShotImagePromptInput, shot StoryboardShotRecord, execution NodeExecution, cause error) error {
	if isWorkflowWriteFenced(cause) {
		return discardWorkflowResult(ctx, a.db, execution, cause.Error())
	}
	code, message := workflowErrorFields(cause, codeActivityFailed)
	failureDetails := map[string]any{}
	var reviewFailure *shotImagePromptReviewFailure
	if errors.As(cause, &reviewFailure) {
		failureDetails = reviewFailure.details()
	}
	failureDetailsJSON := mustJSON(failureDetails)
	persistCtx, cancel := workflowFailurePersistenceContext(ctx)
	defer cancel()
	if !execution.valid() {
		return newWorkflowApplicationError(cause, code, message)
	}
	tx, err := a.db.Begin(persistCtx)
	if err != nil {
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	defer tx.Rollback(persistCtx)
	if _, err := lockNodeBusinessWrite(persistCtx, tx, input.WorkflowRunID, execution); err != nil {
		if errors.Is(err, ErrWorkflowWriteFenced) || errors.Is(err, pgx.ErrNoRows) {
			return discardWorkflowResult(ctx, a.db, execution, err.Error())
		}
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	if _, err := tx.Exec(persistCtx, `
		UPDATE shot_visual_anchors
		SET status = CASE WHEN artifact_id IS NULL THEN 'failed' ELSE status END,
		    review_status = CASE WHEN artifact_id IS NULL THEN 'needs_edit' ELSE review_status END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
			  'imagePromptAgent', COALESCE(metadata->'imagePromptAgent', '{}'::jsonb)
			    || jsonb_build_object(
			      'status', 'failed', 'workflowRunId', $3::text,
			      'errorCode', $4::text, 'errorMessage', $5::text, 'failedAt', now()
			    ) || $6::jsonb
			)
		WHERE id = (
			SELECT id FROM shot_visual_anchors
			WHERE storyboard_shot_id = $1 AND anchor_role = $2 AND status <> 'archived'
			ORDER BY revision DESC LIMIT 1
		)
	`, shot.ID, input.AnchorRole, input.WorkflowRunID, code, message, failureDetailsJSON); err != nil {
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	if _, err := tx.Exec(persistCtx, `
		UPDATE storyboard_shots
		SET image_prompt_status = 'failed',
		    image_prompt_error_code = $2,
		    image_prompt_error_message = $3,
		    image_prompt_workflow_run_id = NULLIF($4, '')::uuid,
		    image_prompt_updated_at = now(),
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`, shot.ID, code, message, input.WorkflowRunID); err != nil {
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	outputValue := map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "shotNo": shot.ShotNo, "anchorRole": input.AnchorRole, "status": "image_prompt_failed", "code": code, "message": message,
	}
	if len(failureDetails) > 0 {
		outputValue["details"] = failureDetails
	}
	output := mustJSON(outputValue)
	if err := insertEvent(persistCtx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image_prompt.failed", "storyboard_shot", shot.ID, output); err != nil {
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	if _, err := failNodeRunTx(persistCtx, tx, execution, code, message, output); err != nil {
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	if err := tx.Commit(persistCtx); err != nil {
		return newWorkflowApplicationError(err, codeActivityFailed, err.Error())
	}
	return newWorkflowApplicationError(cause, code, message)
}

type reviewedShotImagePromptTrace struct {
	Status               string
	Prompt               string
	PromptHash           string
	NegativePrompt       string
	TemplateKey          string
	PromptVersionID      string
	PromptSource         string
	ImageProviderModelID string
}

func (a Activities) reviewedShotImagePrompt(ctx context.Context, shotID, anchorRole string) (reviewedShotImagePromptTrace, error) {
	var trace reviewedShotImagePromptTrace
	err := a.db.QueryRow(ctx, `
		SELECT
			CASE
			  WHEN COALESCE(anchor.prompt, '') <> '' AND COALESCE(anchor.prompt_hash, '') <> '' THEN 'succeeded'
			  WHEN $2 = 'planned_first_frame' THEN COALESCE(shot.image_prompt_status, 'not_started')
			  ELSE 'not_started'
			END,
			COALESCE(anchor.prompt, CASE WHEN $2 = 'planned_first_frame' THEN shot.image_prompt ELSE '' END, ''),
			COALESCE(anchor.prompt_hash, shot.metadata->'imagePromptAgent'->>'promptHash', ''),
			COALESCE(anchor.metadata->'imagePromptAgent'->>'negativePrompt', shot.metadata->'imagePromptAgent'->>'negativePrompt', ''),
			COALESCE(anchor.metadata->'imagePromptAgent'->>'reviewTemplateKey', shot.metadata->'imagePromptAgent'->>'reviewTemplateKey', ''),
			COALESCE(anchor.metadata->'imagePromptAgent'->>'reviewPromptVersionId', shot.metadata->'imagePromptAgent'->>'reviewPromptVersionId', ''),
			CASE
			  WHEN anchor.metadata->'imagePromptAgent'->>'status' = 'approved' THEN 'agent_reviewed'
			  WHEN anchor.metadata->'imagePromptAgent'->>'status' = 'manual' THEN 'manual'
			  WHEN shot.metadata->'imagePromptAgent'->>'status' = 'approved' THEN 'agent_reviewed'
			  WHEN shot.metadata->'imagePromptAgent'->>'status' = 'manual' THEN 'manual'
			  ELSE ''
			END,
			COALESCE(anchor.metadata->'imagePromptAgent'->>'imageProviderModelId', shot.metadata->'imagePromptAgent'->>'imageProviderModelId', '')
		FROM storyboard_shots shot
		LEFT JOIN LATERAL (
			SELECT candidate.*
			FROM shot_visual_anchors candidate
			WHERE candidate.storyboard_shot_id = shot.id AND candidate.anchor_role = $2
			  AND candidate.status <> 'archived'
			ORDER BY candidate.revision DESC
			LIMIT 1
		) anchor ON true
		WHERE shot.id = $1 AND shot.deleted_at IS NULL
	`, shotID, anchorRole).Scan(
		&trace.Status, &trace.Prompt, &trace.PromptHash, &trace.NegativePrompt,
		&trace.TemplateKey, &trace.PromptVersionID, &trace.PromptSource, &trace.ImageProviderModelID,
	)
	return trace, err
}

func selectStoryboardSheetImageModel(candidates []provider.GatewayModelConstraintCandidate) (provider.GatewayModelConstraintCandidate, bool) {
	for _, candidate := range candidates {
		modelKey := strings.ToLower(strings.TrimSpace(candidate.ModelKey))
		if slash := strings.LastIndex(modelKey, "/"); slash >= 0 {
			modelKey = modelKey[slash+1:]
		}
		if modelKey == "gpt-image-2" || strings.HasPrefix(modelKey, "gpt-image-2-") {
			return candidate, true
		}
	}
	return provider.GatewayModelConstraintCandidate{}, false
}
