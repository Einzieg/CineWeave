package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
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

	ShotID    string `json:"shotId"`
	ShotIndex int    `json:"shotIndex"`
	ShotNo    int    `json:"shotNo"`

	WorkflowPrompt string `json:"workflowPrompt"`
	AspectRatio    string `json:"aspectRatio"`
	Size           string `json:"size"`
	Force          bool   `json:"force,omitempty"`
}

type PrepareShotImagePromptOutput struct {
	ShotID                   string                                     `json:"shotId"`
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
	ModelCandidates          []provider.GatewayModelConstraintCandidate `json:"modelCandidates"`
	PromptMeasurements       map[string]int                             `json:"promptMeasurements"`
	DialogueLines            []StoryboardDialogueLine                   `json:"dialogueLines,omitempty"`
}

type shotImagePromptAgentContext struct {
	Project       shotVideoPromptProject    `json:"project"`
	Source        shotVideoPromptSource     `json:"source"`
	Script        shotVideoPromptScript     `json:"script"`
	Scene         shotVideoPromptScene      `json:"scene"`
	Shot          shotImagePromptShot       `json:"shot"`
	Assets        []ShotVideoPromptAsset    `json:"assets"`
	ImageModels   []imagePromptModelContext `json:"imageModels"`
	ReferenceMode string                    `json:"referenceMode"`
	ReferenceKeys []string                  `json:"referenceKeys"`
}

type shotImagePromptShot struct {
	ShotID         string                   `json:"shotId"`
	ShotNo         int                      `json:"shotNo"`
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
	SourceAnchors     []string                 `json:"sourceAnchors"`
	AssetAnchors      []string                 `json:"assetAnchors"`
	ConflictsResolved []string                 `json:"conflictsResolved"`
}

type reviewedImagePrompt struct {
	Approved       bool                     `json:"approved"`
	Prompt         string                   `json:"prompt"`
	FinalPrompt    string                   `json:"finalPrompt"`
	NegativePrompt string                   `json:"negativePrompt"`
	DialogueLines  []StoryboardDialogueLine `json:"dialogueLines"`
	SourceAnchors  []string                 `json:"sourceAnchors"`
	Issues         []string                 `json:"issues"`
	Changes        []string                 `json:"changes"`
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
	if !input.Force && shot.ImagePromptStatus == "succeeded" && strings.TrimSpace(shot.ImagePrompt) != "" {
		return PrepareShotImagePromptOutput{
			ShotID:     shot.ID,
			Prompt:     shot.ImagePrompt,
			PromptHash: promptsvc.HashText(shot.ImagePrompt),
		}, nil
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
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
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	agentContext, err := a.loadShotImagePromptAgentContext(ctx, project, shot, assetContext, constraints.Candidates, input)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	contextJSON, err := json.Marshal(agentContext)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
	}

	generationRendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyShotImageAgent, map[string]any{
		"context": map[string]any{"json": string(contextJSON)},
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(NodeExecution{}, err)
	}
	generationExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForShot(nodeGenerateShotImagePromptPrefix, shot.ShotIndex),
		NodeType:       "agent.image_prompt.generate",
		Input: mustJSON(map[string]any{
			"shotId":            shot.ID,
			"shotNo":            shot.ShotNo,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"imageModelProfile": project.ImageModelProfileKey,
			"imageModels":       agentContext.ImageModels,
			"promptTemplateKey": generationRendered.TemplateKey,
			"promptVersionId":   generationRendered.PromptVersionID,
			"promptHash":        generationRendered.RenderedHash,
		}),
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, err
	}
	if err := a.markShotImagePromptRunning(ctx, input, shot, generationExecution); err != nil {
		return PrepareShotImagePromptOutput{}, err
	}
	generationResponse, err := a.generateProviderText(ctx, generationExecution, provider.GatewayTextRequest{
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
			"maxOutputTokens": 1800,
			"temperature":     0.2,
		}),
		Options: providerTextGatewayOptions(),
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(generationExecution, workflowErrorFromProvider(err, codeActivityFailed))
	}
	draft, err := parseGeneratedImagePrompt(generationResponse.Output.Text)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(generationExecution, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	if err := CompleteNodeRun(ctx, a.db, generationExecution, mustJSON(map[string]any{
		"providerCallId":    generationResponse.ProviderCallID,
		"modelId":           generationResponse.ModelID,
		"prompt":            draft.Prompt,
		"negativePrompt":    draft.NegativePrompt,
		"sourceAnchors":     draft.SourceAnchors,
		"assetAnchors":      draft.AssetAnchors,
		"conflictsResolved": draft.ConflictsResolved,
	})); err != nil {
		return PrepareShotImagePromptOutput{}, err
	}

	draftJSON := mustJSON(draft)
	reviewExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForShot(nodeReviewShotImagePromptPrefix, shot.ShotIndex),
		NodeType:       "agent.image_prompt.review",
		Input: mustJSON(map[string]any{
			"shotId":           shot.ID,
			"shotNo":           shot.ShotNo,
			"generationCallId": generationResponse.ProviderCallID,
			"modelProfileKey":  project.ScriptModelProfileKey,
			"imageModels":      agentContext.ImageModels,
		}),
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, err
	}
	reviewRendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyShotImageReviewAgent, map[string]any{
		"context":   map[string]any{"json": string(contextJSON)},
		"candidate": map[string]any{"json": string(draftJSON)},
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, err)
	}
	reviewResponse, err := a.generateProviderText(ctx, reviewExecution, provider.GatewayTextRequest{
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
			"maxOutputTokens": 1800,
			"temperature":     0.1,
		}),
		Options: providerTextGatewayOptions(),
	})
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, workflowErrorFromProvider(err, codeActivityFailed))
	}
	reviewed, err := parseReviewedImagePrompt(reviewResponse.Output.Text, draft)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	if !reviewed.Approved {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, workflowError{Code: provider.CodeInvalidRequest, Message: "image prompt review did not approve the candidate"})
	}
	dialogueLines := NormalizeStoryboardDialogue(agentContext.Shot.ScriptDialogue)
	negativePrompt := compactShotImageNegativePrompt(stripScriptDialogueFromImagePrompt(firstNonEmptyString(reviewed.NegativePrompt, draft.NegativePrompt), dialogueLines))
	reviewedPrompt := stripScriptDialogueFromImagePrompt(firstNonEmptyString(reviewed.FinalPrompt, reviewed.Prompt), dialogueLines)
	finalPrompt := buildReviewedShotImagePrompt(reviewedPrompt, negativePrompt, agentContext)
	if err := validateShotImagePromptContainsNoDialogue(finalPrompt, dialogueLines); err != nil {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, err)
	}
	measurements, err := validateShotImagePromptForCandidates(finalPrompt, constraints.Candidates)
	if err != nil {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, err)
	}
	output := PrepareShotImagePromptOutput{
		ShotID:                   shot.ID,
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
		ModelCandidates:          constraints.Candidates,
		PromptMeasurements:       measurements,
		DialogueLines:            dialogueLines,
	}
	if err := a.persistReviewedShotImagePrompt(ctx, input, shot, reviewExecution, output, reviewed, draft); err != nil {
		return PrepareShotImagePromptOutput{}, fail(reviewExecution, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	return output, nil
}

func (a Activities) loadShotImagePromptAgentContext(ctx context.Context, project ProjectProductionSettings, shot StoryboardShotRecord, assets ShotAssetContext, candidates []provider.GatewayModelConstraintCandidate, input PrepareShotImagePromptInput) (shotImagePromptAgentContext, error) {
	aspectRatio := firstNonEmptyString(input.AspectRatio, project.VideoRatio, project.AspectRatio, "16:9")
	existingPrompt := ""
	if shot.ImagePromptStatus == "succeeded" {
		existingPrompt = compactImageContextText(shot.ImagePrompt, 1600)
	}
	contextValue := shotImagePromptAgentContext{
		Project: shotVideoPromptProject{
			ProjectType:    project.ProjectType,
			ContentType:    project.ContentType,
			ProductionMode: project.ProductionMode,
			ArtStyle:       project.ArtStyle,
			AspectRatio:    aspectRatio,
			DirectorManual: compactImageContextText(project.DirectorManual, 8000),
			VisualManual:   compactImageContextText(project.VisualManual, 8000),
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
	if firstNonEmptyString(output.FinalPrompt, output.Prompt) == "" {
		return reviewedImagePrompt{}, fmt.Errorf("image prompt review agent returned an empty prompt")
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
			return workflowError{Code: provider.CodeInvalidRequest, Message: "image prompt must not contain script dialogue text"}
		}
	}
	lowerPrompt := strings.ToLower(prompt)
	for _, token := range []string{"台词", "对白", "dialogue", "spoken words", "speech text"} {
		if strings.Contains(lowerPrompt, token) {
			return workflowError{Code: provider.CodeInvalidRequest, Message: "image prompt must contain visual instructions only, without dialogue metadata"}
		}
	}
	return nil
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
	if value := strings.TrimSpace(contextValue.Shot.Visual); value != "" {
		locked = append(locked, "Visual: "+value)
	}
	if value := strings.TrimSpace(contextValue.Shot.Camera); value != "" {
		locked = append(locked, "Camera/composition: "+value)
	}
	if value := strings.TrimSpace(contextValue.Shot.Motion); value != "" {
		locked = append(locked, "Single-frame motion implication: "+value)
	}
	if value := strings.TrimSpace(contextValue.Shot.Mood); value != "" {
		locked = append(locked, "Mood: "+value)
	}
	locked = append(locked, "Output aspect ratio: "+contextValue.Shot.AspectRatio+". No on-screen text, subtitles, captions, speech bubbles, watermarks, logos, UI, contact sheet, or collage.")
	sections = append(sections, strings.Join(locked, "\n"))
	if negativePrompt != "" {
		sections = append(sections, "Scene-specific negative constraints: "+negativePrompt)
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

func (a Activities) persistReviewedShotImagePrompt(ctx context.Context, input PrepareShotImagePromptInput, shot StoryboardShotRecord, execution NodeExecution, output PrepareShotImagePromptOutput, review reviewedImagePrompt, draft generatedImagePrompt) error {
	dialogueBackfilled := len(shot.Dialogue) == 0 && len(output.DialogueLines) > 0
	metadata := mustJSON(map[string]any{
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
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_prompt = $2,
		    script_dialogue = CASE
		      WHEN jsonb_array_length(script_dialogue) = 0 AND jsonb_array_length($6::jsonb) > 0 THEN $6::jsonb
		      ELSE script_dialogue
		    END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    image_prompt_status = 'succeeded',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULLIF($5, '')::uuid,
		    image_prompt_updated_at = now(),
		    image_error_code = NULL,
		    image_error_message = NULL,
		    updated_at = now()
		WHERE project_id = $1 AND id = $4 AND deleted_at IS NULL
	`, input.ProjectID, output.Prompt, metadata, shot.ID, input.WorkflowRunID, mustJSON(output.DialogueLines)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image_prompt.reviewed", "storyboard_shot", shot.ID, mustJSON(map[string]any{
		"workflowRunId":            input.WorkflowRunID,
		"shotId":                   shot.ID,
		"shotNo":                   shot.ShotNo,
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
		"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "shotNo": shot.ShotNo, "status": "image_prompt_running",
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
	output := mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "shotNo": shot.ShotNo, "status": "image_prompt_failed", "code": code, "message": message,
	})
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
	Status          string
	PromptHash      string
	NegativePrompt  string
	TemplateKey     string
	PromptVersionID string
	PromptSource    string
}

func (a Activities) reviewedShotImagePrompt(ctx context.Context, shotID string) (reviewedShotImagePromptTrace, error) {
	var trace reviewedShotImagePromptTrace
	err := a.db.QueryRow(ctx, `
		SELECT
			COALESCE(image_prompt_status, 'not_started'),
			COALESCE(metadata->'imagePromptAgent'->>'promptHash', ''),
			COALESCE(metadata->'imagePromptAgent'->>'negativePrompt', ''),
			COALESCE(metadata->'imagePromptAgent'->>'reviewTemplateKey', ''),
			COALESCE(metadata->'imagePromptAgent'->>'reviewPromptVersionId', ''),
			CASE
			  WHEN metadata->'imagePromptAgent'->>'status' = 'approved' THEN 'agent_reviewed'
			  WHEN metadata->'imagePromptAgent'->>'status' = 'manual' THEN 'manual'
			  ELSE ''
			END
		FROM storyboard_shots
		WHERE id = $1 AND deleted_at IS NULL
	`, shotID).Scan(&trace.Status, &trace.PromptHash, &trace.NegativePrompt, &trace.TemplateKey, &trace.PromptVersionID, &trace.PromptSource)
	return trace, err
}
