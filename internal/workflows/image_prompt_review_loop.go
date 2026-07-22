package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

const (
	maxShotImagePromptReviewAttempts  = 3
	structuredImagePromptAttempts     = 2
	structuredImagePromptOutputTokens = 2400

	codeImagePromptOutputInvalid   = "IMAGE_PROMPT_OUTPUT_INVALID"
	codeImagePromptReviewExhausted = "IMAGE_PROMPT_REVIEW_EXHAUSTED"
	codeImagePromptContextConflict = "IMAGE_PROMPT_CONTEXT_CONFLICT"
	codeImagePromptDialogueLeak    = "IMAGE_PROMPT_DIALOGUE_LEAK"
	codeImagePromptSafetyRisk      = "IMAGE_PROMPT_PROVIDER_SAFETY_RISK"
)

// agentJSONList accepts both strings and structured objects. Provider models
// frequently return richer review records than the prompt contract's compact
// string examples, and those details are valuable correction input.
type agentJSONList []json.RawMessage

func (values *agentJSONList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*values = nil
		return nil
	}
	if data[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		*values = append((*values)[:0], items...)
		return nil
	}
	*values = agentJSONList{append(json.RawMessage(nil), data...)}
	return nil
}

func agentJSONListFromStrings(values ...string) agentJSONList {
	result := make(agentJSONList, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		raw, _ := json.Marshal(value)
		result = append(result, raw)
	}
	return result
}

func appendAgentJSONMessages(values agentJSONList, messages ...string) agentJSONList {
	return append(values, agentJSONListFromStrings(messages...)...)
}

func agentJSONListMessages(values agentJSONList) []string {
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if message := agentJSONValueMessage(raw); message != "" {
			result = append(result, message)
		}
	}
	return result
}

func agentJSONValueMessage(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err == nil {
		parts := make([]string, 0, 3)
		for _, key := range []string{"type", "field", "severity"} {
			if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
				parts = append(parts, strings.TrimSpace(value))
			}
		}
		for _, key := range []string{"message", "detail", "reason", "description", "issue", "change", "suggestion"} {
			if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
				parts = append(parts, strings.TrimSpace(value))
				break
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "：")
		}
	}
	return strings.TrimSpace(string(raw))
}

type ShotImagePromptAttemptTrace struct {
	ActivityAttempt            int           `json:"activityAttempt"`
	Attempt                    int           `json:"attempt"`
	Status                     string        `json:"status"`
	GenerationProviderCallID   string        `json:"generationProviderCallId,omitempty"`
	GenerationModelID          string        `json:"generationModelId,omitempty"`
	ReviewProviderCallID       string        `json:"reviewProviderCallId,omitempty"`
	ReviewModelID              string        `json:"reviewModelId,omitempty"`
	Approved                   bool          `json:"approved"`
	AcceptedReviewerCorrection bool          `json:"acceptedReviewerCorrection,omitempty"`
	ReviewSummary              string        `json:"reviewSummary,omitempty"`
	Issues                     agentJSONList `json:"issues,omitempty"`
	Changes                    agentJSONList `json:"changes,omitempty"`
	ValidationCode             string        `json:"validationCode,omitempty"`
}

type shotImagePromptReviewFeedback struct {
	Attempt                         int                                       `json:"attempt"`
	ReviewProviderCallID            string                                    `json:"reviewProviderCallId,omitempty"`
	ReviewModelID                   string                                    `json:"reviewModelId,omitempty"`
	ReviewTemplateKey               string                                    `json:"reviewTemplateKey,omitempty"`
	ReviewPromptVersion             string                                    `json:"reviewPromptVersionId,omitempty"`
	ReviewContract                  *videoproduction.PromptContractProvenance `json:"reviewContract,omitempty"`
	PreviousCandidate               generatedImagePrompt                      `json:"previousCandidate"`
	ReviewerSuggestedPrompt         string                                    `json:"reviewerSuggestedPrompt,omitempty"`
	ReviewerSuggestedNegativePrompt string                                    `json:"reviewerSuggestedNegativePrompt,omitempty"`
	Issues                          agentJSONList                             `json:"issues,omitempty"`
	Changes                         agentJSONList                             `json:"changes,omitempty"`
	Summary                         string                                    `json:"summary"`
	ValidationCode                  string                                    `json:"validationCode,omitempty"`
}

type shotImagePromptReviewFailure struct {
	Cause    workflowError
	Attempts []ShotImagePromptAttemptTrace
	Feedback shotImagePromptReviewFeedback
}

func (failure *shotImagePromptReviewFailure) Error() string { return failure.Cause.Error() }
func (failure *shotImagePromptReviewFailure) Unwrap() error { return failure.Cause }

func (failure *shotImagePromptReviewFailure) details() map[string]any {
	return map[string]any{
		"attempts":           failure.Attempts,
		"lastReviewFeedback": failure.Feedback,
	}
}

func (a Activities) runShotImagePromptReviewLoop(
	ctx context.Context,
	input PrepareShotImagePromptInput,
	project ProjectProductionSettings,
	shot StoryboardShotRecord,
	agentContext shotImagePromptAgentContext,
	productionContract shotProductionContractContext,
	referencePack videoproduction.ReferencePack,
	promptContextPlan videoproduction.PromptContextPlan,
	panelManifest *videoproduction.PanelManifest,
	imageProviderModelID string,
	imageCandidates []provider.GatewayModelConstraintCandidate,
	fail func(NodeExecution, error) error,
) (PrepareShotImagePromptOutput, error) {
	input.ShotID = shot.ID
	activityAttempt := int(currentActivityAttempt(ctx))
	referencePackID := ""
	promptContextPlanID := ""
	var feedback *shotImagePromptReviewFeedback
	attempts := make([]ShotImagePromptAttemptTrace, 0, maxShotImagePromptReviewAttempts)

	for attempt := 1; attempt <= maxShotImagePromptReviewAttempts; attempt++ {
		extraContext := map[string]any{
			"agentContext":  agentContext,
			"anchorRole":    input.AnchorRole,
			"panelManifest": panelManifest,
			"attempt":       attempt,
		}
		if feedback != nil {
			extraContext["reviewFeedback"] = feedback
			extraContext["previousCandidate"] = feedback.PreviousCandidate
		}
		compiled, err := a.renderVideoProductionPromptContract(
			ctx, input.OrganizationID, input.ProjectID, project,
			videoproduction.PromptRoleAnchorGenerate, promptContextPlan,
			productionContract, referencePack, extraContext,
		)
		if err != nil {
			return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
		}
		generationRendered := applyShotImagePromptReviewFeedback(compiled.Rendered, feedback, attempt)
		generationContract := &compiled.Contract.Provenance
		draft, generationResponse, corrected := reviewerCorrectedImagePromptDraft(feedback)
		if corrected {
			generationRendered.TemplateKey = firstNonEmptyString(feedback.ReviewTemplateKey, generationRendered.TemplateKey)
			generationRendered.PromptVersionID = firstNonEmptyString(feedback.ReviewPromptVersion, generationRendered.PromptVersionID)
			if feedback.ReviewContract != nil {
				generationContract = feedback.ReviewContract
			}
		}
		generationExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			NodeKey: shotImagePromptAttemptNodeKey(
				nodeGenerateShotImagePromptPrefix, shot.ShotIndex, input.AnchorRole, activityAttempt, attempt,
			),
			NodeType: "agent.image_prompt.generate",
			Input: mustJSON(map[string]any{
				"shotId":             shot.ID,
				"shotNo":             shot.ShotNo,
				"anchorRole":         input.AnchorRole,
				"activityAttempt":    activityAttempt,
				"attempt":            attempt,
				"correctionFeedback": feedback,
				"modelProfileKey":    project.ScriptModelProfileKey,
				"imageModelProfile":  project.ImageModelProfileKey,
				"imageModels":        agentContext.ImageModels,
				"promptTemplateKey":  generationRendered.TemplateKey,
				"promptVersionId":    generationRendered.PromptVersionID,
				"promptHash":         generationRendered.RenderedHash,
				"correctionSource":   imagePromptCorrectionSource(feedback),
			}),
		})
		if err != nil {
			return PrepareShotImagePromptOutput{}, err
		}
		if attempt == 1 {
			persistedContext, persistErr := a.persistPromptContextPlan(
				ctx, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.CreatedBy,
				generationExecution, project, shot, promptContextPlan,
			)
			if persistErr != nil {
				return PrepareShotImagePromptOutput{}, fail(generationExecution, persistErr)
			}
			promptContextPlanID = persistedContext.ID
			referencePackID, err = a.persistShotReferencePack(ctx, input, generationExecution, project, productionContract, referencePack)
			if err != nil {
				return PrepareShotImagePromptOutput{}, fail(generationExecution, err)
			}
			if err := a.markShotImagePromptRunning(ctx, input, shot, generationExecution); err != nil {
				return PrepareShotImagePromptOutput{}, fail(generationExecution, err)
			}
		}

		if !corrected {
			generationRequest := provider.GatewayTextRequest{
				OrganizationID:    input.OrganizationID,
				ProjectID:         input.ProjectID,
				WorkflowRunID:     input.WorkflowRunID,
				NodeRunID:         generationExecution.NodeRunID,
				ModelProfileKey:   project.ScriptModelProfileKey,
				PromptTemplateKey: generationRendered.TemplateKey,
				PromptVersionID:   generationRendered.PromptVersionID,
				PromptHash:        generationRendered.RenderedHash,
				PromptSource:      generationRendered.Source,
				Input: mustJSON(map[string]any{
					"prompt":          generationRendered.RenderedText,
					"responseFormat":  "json",
					"maxOutputTokens": structuredImagePromptOutputTokens,
					"temperature":     0.2,
				}),
				Options: providerTextGatewayOptions(),
			}
			draft, generationResponse, err = requestStructuredImagePrompt(
				ctx, generationExecution, generationRequest, a.generateProviderText,
				parseGeneratedImagePrompt, "图片提示词生成 Agent",
			)
			if err != nil {
				return PrepareShotImagePromptOutput{}, fail(generationExecution, normalizeImagePromptRequestError(err))
			}
		}
		if err := CompleteNodeRun(ctx, a.db, generationExecution, mustJSON(map[string]any{
			"activityAttempt":   activityAttempt,
			"attempt":           attempt,
			"providerCallId":    generationResponse.ProviderCallID,
			"modelId":           generationResponse.ModelID,
			"prompt":            draft.Prompt,
			"negativePrompt":    draft.NegativePrompt,
			"sourceAnchors":     draft.SourceAnchors,
			"assetAnchors":      draft.AssetAnchors,
			"conflictsResolved": draft.ConflictsResolved,
			"correctionSource":  imagePromptCorrectionSource(feedback),
		})); err != nil {
			return PrepareShotImagePromptOutput{}, err
		}

		reviewExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			NodeKey: shotImagePromptAttemptNodeKey(
				nodeReviewShotImagePromptPrefix, shot.ShotIndex, input.AnchorRole, activityAttempt, attempt,
			),
			NodeType: "agent.image_prompt.review",
			Input: mustJSON(map[string]any{
				"shotId":           shot.ID,
				"shotNo":           shot.ShotNo,
				"anchorRole":       input.AnchorRole,
				"activityAttempt":  activityAttempt,
				"attempt":          attempt,
				"generationCallId": generationResponse.ProviderCallID,
				"modelProfileKey":  project.ScriptModelProfileKey,
				"imageModels":      agentContext.ImageModels,
			}),
		})
		if err != nil {
			return PrepareShotImagePromptOutput{}, err
		}
		reviewCompiled, err := a.renderVideoProductionPromptContract(
			ctx, input.OrganizationID, input.ProjectID, project,
			videoproduction.PromptRoleAnchorReview, promptContextPlan,
			productionContract, referencePack,
			map[string]any{
				"agentContext":  agentContext,
				"candidate":     draft,
				"anchorRole":    input.AnchorRole,
				"panelManifest": panelManifest,
				"attempt":       attempt,
			},
		)
		if err != nil {
			return PrepareShotImagePromptOutput{}, fail(reviewExecution, err)
		}
		reviewRendered := reviewCompiled.Rendered
		reviewContract := &reviewCompiled.Contract.Provenance
		reviewRequest := provider.GatewayTextRequest{
			OrganizationID:    input.OrganizationID,
			ProjectID:         input.ProjectID,
			WorkflowRunID:     input.WorkflowRunID,
			NodeRunID:         reviewExecution.NodeRunID,
			ModelProfileKey:   project.ScriptModelProfileKey,
			PromptTemplateKey: reviewRendered.TemplateKey,
			PromptVersionID:   reviewRendered.PromptVersionID,
			PromptHash:        reviewRendered.RenderedHash,
			PromptSource:      reviewRendered.Source,
			Input: mustJSON(map[string]any{
				"prompt":          reviewRendered.RenderedText,
				"responseFormat":  "json",
				"maxOutputTokens": structuredImagePromptOutputTokens,
				"temperature":     0.1,
			}),
			Options: providerTextGatewayOptions(),
		}
		reviewed, reviewResponse, err := requestStructuredImagePrompt(
			ctx, reviewExecution, reviewRequest, a.generateProviderText,
			func(text string) (reviewedImagePrompt, error) { return parseReviewedImagePrompt(text, draft) },
			"图片提示词审核 Agent",
		)
		if err != nil {
			return PrepareShotImagePromptOutput{}, fail(reviewExecution, normalizeImagePromptRequestError(err))
		}

		dialogueLines := NormalizeStoryboardDialogue(agentContext.Shot.ScriptDialogue)
		negativePrompt := compactShotImageNegativePrompt(stripScriptDialogueFromImagePrompt(firstNonEmptyString(reviewed.NegativePrompt, draft.NegativePrompt), dialogueLines))
		reviewedPrompt := stripScriptDialogueFromImagePrompt(firstNonEmptyString(reviewed.FinalPrompt, reviewed.Prompt), dialogueLines)
		acceptedReviewerCorrection := shouldAcceptFinalReviewerCorrection(attempt, reviewed)
		if acceptedReviewerCorrection {
			reviewed.Approved = true
		}
		finalPrompt := ""
		measurements := map[string]int(nil)
		validationCode := ""
		if reviewed.Approved {
			finalPrompt = buildReviewedShotImagePrompt(reviewedPrompt, negativePrompt, agentContext)
			if validationErr := validateShotImagePromptContainsNoDialogue(finalPrompt, dialogueLines); validationErr != nil {
				validationCode, _ = workflowErrorFields(validationErr, codeImagePromptDialogueLeak)
				reviewed = rejectReviewedImagePrompt(reviewed, validationErr.Error(), "删除全部台词正文与台词元数据，只保留可见画面和表演状态")
			} else if validationErr := validateShotImagePromptProviderSafety(finalPrompt); validationErr != nil {
				validationCode, _ = workflowErrorFields(validationErr, codeImagePromptSafetyRisk)
				reviewed = rejectReviewedImagePrompt(
					reviewed,
					validationErr.Error(),
					"保留人物、动作、构图和剧情结果，但改写为非图形化的战损、污痕与氛围表达；删除血泊、滴血、残肢、伤口等直接细节",
				)
			} else {
				deterministicReview := videoproduction.ReviewImagePrompt(project.VideoProductionProfileKey, finalPrompt, promptContextPlan.VerbatimDialogueCues)
				if !deterministicReview.Approved {
					validationCode = videoproduction.CodePromptContractIncomplete
					reviewed = rejectReviewedImagePrompt(
						reviewed,
						fmt.Sprintf("当前生产方案的图片提示词确定性审核失败：%v", deterministicReview.Issues),
						"按生产方案契约修正提示词，同时保持镜头状态和参考图约束",
					)
				} else {
					measurements, err = validateShotImagePromptForCandidates(finalPrompt, imageCandidates)
					if err != nil {
						validationCode, _ = workflowErrorFields(err, provider.CodeInvalidRequest)
						reviewed = rejectReviewedImagePrompt(reviewed, err.Error(), "压缩提示词并移除原始手册或资产 JSON，保持所有必要视觉事实")
					}
				}
			}
		}

		summary := reviewedImagePromptSummary(reviewed)
		trace := ShotImagePromptAttemptTrace{
			ActivityAttempt:            activityAttempt,
			Attempt:                    attempt,
			GenerationProviderCallID:   generationResponse.ProviderCallID,
			GenerationModelID:          generationResponse.ModelID,
			ReviewProviderCallID:       reviewResponse.ProviderCallID,
			ReviewModelID:              reviewResponse.ModelID,
			Approved:                   reviewed.Approved,
			AcceptedReviewerCorrection: acceptedReviewerCorrection,
			ReviewSummary:              summary,
			Issues:                     reviewed.Issues,
			Changes:                    reviewed.Changes,
			ValidationCode:             validationCode,
		}
		if reviewed.Approved {
			trace.Status = "approved"
			if acceptedReviewerCorrection {
				trace.Status = "reviewer_corrected"
			}
			attempts = append(attempts, trace)
			output := PrepareShotImagePromptOutput{
				ShotID:                   shot.ID,
				AnchorRole:               input.AnchorRole,
				Prompt:                   finalPrompt,
				NegativePrompt:           negativePrompt,
				PromptHash:               promptsvc.HashText(finalPrompt),
				GenerationProviderCallID: generationResponse.ProviderCallID,
				GenerationModelID:        generationResponse.ModelID,
				GenerationTemplateKey:    generationRendered.TemplateKey,
				GenerationPromptVersion:  generationRendered.PromptVersionID,
				ReviewProviderCallID:     reviewResponse.ProviderCallID,
				ReviewModelID:            reviewResponse.ModelID,
				ReviewTemplateKey:        reviewRendered.TemplateKey,
				ReviewPromptVersion:      reviewRendered.PromptVersionID,
				ImageProviderModelID:     imageProviderModelID,
				ModelCandidates:          imageCandidates,
				PromptMeasurements:       measurements,
				DialogueLines:            dialogueLines,
				ReferencePackID:          referencePackID,
				ReferencePackHash:        referencePack.ManifestHash,
				CapabilitySnapshotHash:   referencePack.CapabilitySnapshotHash,
				PromptContextPlanID:      promptContextPlanID,
				PromptContextPlanHash:    promptContextPlan.PlanHash,
				GenerationContract:       generationContract,
				ReviewContract:           reviewContract,
				Attempts:                 attempts,
			}
			if err := a.persistReviewedShotImagePrompt(ctx, input, project, shot, reviewExecution, output, reviewed, draft); err != nil {
				return PrepareShotImagePromptOutput{}, fail(reviewExecution, workflowError{Code: codeActivityFailed, Message: err.Error()})
			}
			return output, nil
		}

		trace.Status = "changes_requested"
		attempts = append(attempts, trace)
		feedback = &shotImagePromptReviewFeedback{
			Attempt:                         attempt,
			ReviewProviderCallID:            reviewResponse.ProviderCallID,
			ReviewModelID:                   reviewResponse.ModelID,
			ReviewTemplateKey:               reviewRendered.TemplateKey,
			ReviewPromptVersion:             reviewRendered.PromptVersionID,
			ReviewContract:                  reviewContract,
			PreviousCandidate:               draft,
			ReviewerSuggestedPrompt:         firstNonEmptyString(reviewed.FinalPrompt, reviewed.Prompt),
			ReviewerSuggestedNegativePrompt: reviewed.NegativePrompt,
			Issues:                          reviewed.Issues,
			Changes:                         reviewed.Changes,
			Summary:                         summary,
			ValidationCode:                  validationCode,
		}
		if attempt == maxShotImagePromptReviewAttempts {
			code := codeImagePromptReviewExhausted
			message := fmt.Sprintf("图片提示词连续 %d 轮审核未通过：%s", maxShotImagePromptReviewAttempts, summary)
			if reviewFeedbackHasUnresolvableConflict(*feedback) {
				code = codeImagePromptContextConflict
				message = "镜头状态、锁定事实或参考资产之间存在无法自动调和的冲突：" + summary
			}
			return PrepareShotImagePromptOutput{}, fail(reviewExecution, &shotImagePromptReviewFailure{
				Cause:    workflowError{Code: code, Message: message, RetryabilityKnown: true},
				Attempts: attempts,
				Feedback: *feedback,
			})
		}
		if err := CompleteNodeRun(ctx, a.db, reviewExecution, mustJSON(map[string]any{
			"activityAttempt": activityAttempt,
			"attempt":         attempt,
			"status":          "changes_requested",
			"approved":        false,
			"providerCallId":  reviewResponse.ProviderCallID,
			"modelId":         reviewResponse.ModelID,
			"prompt":          reviewed.Prompt,
			"finalPrompt":     reviewed.FinalPrompt,
			"negativePrompt":  reviewed.NegativePrompt,
			"sourceAnchors":   reviewed.SourceAnchors,
			"issues":          reviewed.Issues,
			"changes":         reviewed.Changes,
			"reviewSummary":   summary,
			"validationCode":  validationCode,
		})); err != nil {
			return PrepareShotImagePromptOutput{}, err
		}
	}
	return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{
		Code:              codeImagePromptReviewExhausted,
		Message:           "图片提示词审核循环异常结束",
		RetryabilityKnown: true,
	})
}

func shotImagePromptAttemptNodeKey(prefix string, shotIndex int, anchorRole string, activityAttempt, attempt int) string {
	return fmt.Sprintf("%s_%s_activity_%d_attempt_%d", nodeKeyForShot(prefix, shotIndex), anchorRole, activityAttempt, attempt)
}

func reviewerCorrectedImagePromptDraft(feedback *shotImagePromptReviewFeedback) (generatedImagePrompt, provider.GatewayTextResponse, bool) {
	if feedback == nil || strings.TrimSpace(feedback.ReviewerSuggestedPrompt) == "" {
		return generatedImagePrompt{}, provider.GatewayTextResponse{}, false
	}
	draft := feedback.PreviousCandidate
	draft.Prompt = strings.TrimSpace(feedback.ReviewerSuggestedPrompt)
	if negative := strings.TrimSpace(feedback.ReviewerSuggestedNegativePrompt); negative != "" {
		draft.NegativePrompt = negative
	}
	draft.ConflictsResolved = append(draft.ConflictsResolved, feedback.Changes...)
	return draft, provider.GatewayTextResponse{
		ProviderCallID: feedback.ReviewProviderCallID,
		ModelID:        feedback.ReviewModelID,
		Status:         "succeeded",
	}, true
}

func shouldAcceptFinalReviewerCorrection(attempt int, reviewed reviewedImagePrompt) bool {
	if attempt != maxShotImagePromptReviewAttempts || reviewed.Approved {
		return false
	}
	if strings.TrimSpace(firstNonEmptyString(reviewed.FinalPrompt, reviewed.Prompt)) == "" {
		return false
	}
	return !reviewFeedbackHasUnresolvableConflict(shotImagePromptReviewFeedback{
		Issues:  reviewed.Issues,
		Changes: reviewed.Changes,
		Summary: reviewedImagePromptSummary(reviewed),
	})
}

func imagePromptCorrectionSource(feedback *shotImagePromptReviewFeedback) string {
	if feedback != nil && strings.TrimSpace(feedback.ReviewerSuggestedPrompt) != "" {
		return "reviewer_correction"
	}
	return "generator"
}

func applyShotImagePromptReviewFeedback(rendered promptsvc.RenderedPrompt, feedback *shotImagePromptReviewFeedback, attempt int) promptsvc.RenderedPrompt {
	if feedback == nil {
		return rendered
	}
	rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) + fmt.Sprintf(`

<review_correction priority="highest" attempt="%d">
上一轮审核未通过。下面的审核信息是本轮必须落实的修正约束，不是可选建议：
%s

修正规则：
1. 逐项解决 issues，并落实 changes；reviewerSuggestedPrompt 只能作为修正参考，仍须服从 ShotState、ReferencePack 和锁定事实。
2. 输出完整的新候选，不要只输出差异、解释、推理过程或 Markdown。
3. 严禁写入剧本台词正文、对白字段或屏幕文字；只描述单帧可见画面、构图、人物状态和环境。
4. 严格返回 JSON：{"prompt":"...","negativePrompt":"...","dialogueLines":[],"sourceAnchors":[],"assetAnchors":[],"conflictsResolved":[]}。
</review_correction>`, attempt, string(mustJSON(feedback)))
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+review_feedback_v1"
	return rendered
}

func rejectReviewedImagePrompt(reviewed reviewedImagePrompt, issue, change string) reviewedImagePrompt {
	reviewed.Approved = false
	reviewed.Issues = appendAgentJSONMessages(reviewed.Issues, issue)
	reviewed.Changes = appendAgentJSONMessages(reviewed.Changes, change)
	return reviewed
}

func reviewedImagePromptSummary(reviewed reviewedImagePrompt) string {
	parts := append(agentJSONListMessages(reviewed.Issues), agentJSONListMessages(reviewed.Changes)...)
	if len(parts) == 0 {
		return "审核 Agent 未提供可执行的修正说明"
	}
	return strings.Join(parts, "；")
}

func reviewFeedbackHasUnresolvableConflict(feedback shotImagePromptReviewFeedback) bool {
	raw := strings.ToLower(string(mustJSON(map[string]any{
		"issues":  feedback.Issues,
		"changes": feedback.Changes,
		"summary": feedback.Summary,
	})))
	for _, marker := range []string{
		`"resolvable":false`, `"canresolve":false`, "无法自动调和", "无法调和", "不可调和", "无法同时满足",
		"unresolvable", "irreconcilable", "cannot satisfy both", "conflicting locked facts",
	} {
		if strings.Contains(raw, marker) {
			return true
		}
	}
	return false
}

func normalizeImagePromptRequestError(err error) error {
	var workflowErr workflowError
	if errors.As(err, &workflowErr) {
		return err
	}
	return workflowErrorFromProvider(err, codeActivityFailed)
}

func requestStructuredImagePrompt[T any](
	ctx context.Context,
	nodeExecution NodeExecution,
	request provider.GatewayTextRequest,
	call func(context.Context, NodeExecution, provider.GatewayTextRequest) (provider.GatewayTextResponse, error),
	parse func(string) (T, error),
	agentLabel string,
) (T, provider.GatewayTextResponse, error) {
	var zero T
	var response provider.GatewayTextResponse
	var parseErr error
	for attempt := 1; attempt <= structuredImagePromptAttempts; attempt++ {
		var err error
		response, err = call(ctx, nodeExecution, request)
		if err != nil {
			return zero, response, err
		}
		parsed, err := parse(response.Output.Text)
		if err == nil {
			return parsed, response, nil
		}
		parseErr = err
		if attempt < structuredImagePromptAttempts {
			request.Input = structuredImagePromptRetryInput(request.Input, attempt+1, err)
		}
	}
	return zero, response, workflowError{
		Code:              codeImagePromptOutputInvalid,
		Message:           fmt.Sprintf("%s连续 %d 次返回无法解析的结构化结果：%v", agentLabel, structuredImagePromptAttempts, parseErr),
		RetryabilityKnown: true,
	}
}

func structuredImagePromptRetryInput(raw json.RawMessage, attempt int, cause error) json.RawMessage {
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return raw
	}
	prompt, _ := input["prompt"].(string)
	input["prompt"] = strings.TrimSpace(prompt) + fmt.Sprintf(`

结构化输出纠错（第 %d 次尝试）：上一次输出未通过 JSON 校验：%s
重新返回一个完整、紧凑且可解析的 JSON 对象。数组字段允许使用字符串或带 message/detail/reason 的对象；不要输出 Markdown、解释、推理过程或未闭合字符串。`, attempt, cause.Error())
	input["maxOutputTokens"] = structuredImagePromptOutputTokens
	input["temperature"] = 0
	return mustJSON(input)
}

func containsAffirmativeDialogueMetadata(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, token := range []string{"台词", "对白", "dialogue", "spoken words", "speech text", "speaks in chinese"} {
		searchFrom := 0
		for searchFrom < len(lower) {
			relative := strings.Index(lower[searchFrom:], token)
			if relative < 0 {
				break
			}
			index := searchFrom + relative
			before := strings.TrimSpace(lower[:index])
			after := strings.TrimLeft(lower[index+len(token):], " \t")
			negative := false
			for _, marker := range []string{"无", "不含", "禁止", "不要", "no", "without", "exclude", "avoid"} {
				if strings.HasSuffix(before, marker) {
					negative = true
					break
				}
			}
			if !negative && (strings.HasPrefix(after, ":") || strings.HasPrefix(after, "：")) {
				content := strings.TrimSpace(strings.TrimLeft(after, ":："))
				if content != "" && !strings.HasPrefix(content, "无") && !strings.HasPrefix(content, "none") && !strings.HasPrefix(content, "no ") {
					return true
				}
			}
			searchFrom = index + len(token)
		}
	}
	return false
}
