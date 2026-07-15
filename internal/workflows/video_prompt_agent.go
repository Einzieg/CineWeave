package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeGenerateShotVideoPromptPrefix = "generate_shot_video_prompt"
	nodeReviewShotVideoPromptPrefix   = "review_shot_video_prompt"
	promptKeyShotVideoAgent           = "shot_video_prompt_agent"
	promptKeyShotVideoReviewAgent     = "shot_video_prompt_review_agent"
	structuredVideoPromptAttempts     = 3
	structuredVideoPromptOutputTokens = 6000
	authoritativeAudioStartMarker     = "<cineweave_authoritative_audio_timeline>"
	authoritativeAudioEndMarker       = "</cineweave_authoritative_audio_timeline>"
)

type PrepareShotVideoPromptInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	ShotID    string `json:"shotId"`
	ShotIndex int    `json:"shotIndex"`
	ShotNo    int    `json:"shotNo"`

	WorkflowPrompt    string                   `json:"workflowPrompt"`
	Duration          float64                  `json:"duration"`
	RequestedDuration float64                  `json:"requestedDuration,omitempty"`
	AspectRatio       string                   `json:"aspectRatio"`
	Resolution        string                   `json:"resolution"`
	Force             bool                     `json:"force,omitempty"`
	PromptOnly        bool                     `json:"promptOnly,omitempty"`
	ExecutionPlanID   string                   `json:"executionPlanId,omitempty"`
	RenderSegmentID   string                   `json:"renderSegmentId,omitempty"`
	SegmentIndex      int                      `json:"segmentIndex,omitempty"`
	SegmentCount      int                      `json:"segmentCount,omitempty"`
	SegmentStartTick  int64                    `json:"segmentStartTick,omitempty"`
	SegmentEndTick    int64                    `json:"segmentEndTick,omitempty"`
	RetryGeneration   int                      `json:"retryGeneration,omitempty"`
	AudioStrategy     string                   `json:"audioStrategy,omitempty"`
	AudioRequirement  string                   `json:"audioRequirement,omitempty"`
	DialogueLines     []StoryboardDialogueLine `json:"dialogueLines,omitempty"`
}

type PrepareShotVideoPromptOutput struct {
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

type shotVideoPromptAgentContext struct {
	Project       shotVideoPromptProject    `json:"project"`
	Source        shotVideoPromptSource     `json:"source"`
	Script        shotVideoPromptScript     `json:"script"`
	Scene         shotVideoPromptScene      `json:"scene"`
	Shot          shotVideoPromptShot       `json:"shot"`
	Assets        []ShotVideoPromptAsset    `json:"assets"`
	VideoModels   []videoPromptModelContext `json:"videoModels"`
	ReferenceMode string                    `json:"referenceMode"`
	ReferenceKeys []string                  `json:"referenceKeys"`
}

type shotVideoPromptProject struct {
	ProjectType    string `json:"projectType,omitempty"`
	ContentType    string `json:"contentType,omitempty"`
	ProductionMode string `json:"productionMode,omitempty"`
	ArtStyle       string `json:"artStyle,omitempty"`
	AspectRatio    string `json:"aspectRatio"`
	DirectorManual string `json:"directorManual,omitempty"`
	VisualManual   string `json:"visualManual,omitempty"`
}

type shotVideoPromptSource struct {
	SourceID     string `json:"sourceId,omitempty"`
	SourceTitle  string `json:"sourceTitle,omitempty"`
	ChapterID    string `json:"chapterId,omitempty"`
	ChapterTitle string `json:"chapterTitle,omitempty"`
	VolumeIndex  int    `json:"volumeIndex,omitempty"`
	SectionIndex int    `json:"sectionIndex,omitempty"`
	Content      string `json:"content,omitempty"`
}

type shotVideoPromptScript struct {
	ScriptID     string `json:"scriptId,omitempty"`
	EpisodeID    string `json:"episodeId,omitempty"`
	Episode      int    `json:"episodeIndex,omitempty"`
	EpisodeTitle string `json:"episodeTitle,omitempty"`
	Content      string `json:"content,omitempty"`
}

type shotVideoPromptScene struct {
	SceneID       string          `json:"sceneId,omitempty"`
	SceneNo       int             `json:"sceneNo,omitempty"`
	Title         string          `json:"title,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Location      string          `json:"location,omitempty"`
	TimeOfDay     string          `json:"timeOfDay,omitempty"`
	Atmosphere    string          `json:"atmosphere,omitempty"`
	Characters    json.RawMessage `json:"characters,omitempty"`
	Action        string          `json:"action,omitempty"`
	Dialogue      string          `json:"dialogue,omitempty"`
	VisualGoal    string          `json:"visualGoal,omitempty"`
	EmotionalTone string          `json:"emotionalTone,omitempty"`
	Content       string          `json:"content,omitempty"`
}

type shotVideoPromptShot struct {
	ShotID            string                   `json:"shotId"`
	ShotNo            int                      `json:"shotNo"`
	Duration          float64                  `json:"duration"`
	AspectRatio       string                   `json:"aspectRatio"`
	Resolution        string                   `json:"resolution"`
	Title             string                   `json:"title,omitempty"`
	Visual            string                   `json:"visual,omitempty"`
	Camera            string                   `json:"camera,omitempty"`
	Motion            string                   `json:"motion,omitempty"`
	Mood              string                   `json:"mood,omitempty"`
	ExistingPrompt    string                   `json:"existingPrompt,omitempty"`
	ScriptDialogue    []StoryboardDialogueLine `json:"scriptDialogue"`
	ExecutionPlanID   string                   `json:"executionPlanId,omitempty"`
	RenderSegmentID   string                   `json:"renderSegmentId,omitempty"`
	SegmentIndex      int                      `json:"segmentIndex,omitempty"`
	SegmentCount      int                      `json:"segmentCount,omitempty"`
	SegmentStartTick  int64                    `json:"segmentStartTick,omitempty"`
	SegmentEndTick    int64                    `json:"segmentEndTick,omitempty"`
	RequestedDuration float64                  `json:"requestedDuration,omitempty"`
	AudioStrategy     string                   `json:"audioStrategy,omitempty"`
	AudioRequirement  string                   `json:"audioRequirement,omitempty"`
}

type videoPromptModelContext struct {
	ProviderModelID string `json:"providerModelId"`
	ModelKey        string `json:"modelKey"`
	MaxLength       int    `json:"maxLength,omitempty"`
	LengthUnit      string `json:"lengthUnit,omitempty"`
	TargetLength    int    `json:"targetLength,omitempty"`
}

type generatedVideoPrompt struct {
	Prompt         string                   `json:"prompt"`
	NegativePrompt string                   `json:"negativePrompt"`
	DialogueLines  []StoryboardDialogueLine `json:"dialogueLines"`
	SourceAnchors  []string                 `json:"sourceAnchors"`
	Notes          []string                 `json:"notes"`
}

type reviewedVideoPrompt struct {
	Approved       bool                     `json:"approved"`
	Prompt         string                   `json:"prompt"`
	FinalPrompt    string                   `json:"finalPrompt"`
	NegativePrompt string                   `json:"negativePrompt"`
	DialogueLines  []StoryboardDialogueLine `json:"dialogueLines"`
	Issues         []string                 `json:"issues"`
	Changes        []string                 `json:"changes"`
}

func (a Activities) PrepareShotVideoPrompt(ctx context.Context, input PrepareShotVideoPromptInput) (PrepareShotVideoPromptOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.WorkflowPrompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return PrepareShotVideoPromptOutput{}, err
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, err
	}
	fail := func(nodeExecution NodeExecution, cause error) error {
		return a.failShotVideoPromptActivity(ctx, input, baseInput, shot, nodeExecution, cause)
	}
	if !input.Force && shot.VideoProviderAsyncTaskID != "" && strings.TrimSpace(shot.VideoPrompt) != "" {
		return PrepareShotVideoPromptOutput{ShotID: shot.ID, Prompt: shot.VideoPrompt, PromptHash: promptsvc.HashText(shot.VideoPrompt)}, nil
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if a.gateway == nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	constraints, err := a.gateway.ResolveModelConstraints(ctx, provider.GatewayModelConstraintsRequest{
		OrganizationID:  input.OrganizationID,
		ModelProfileKey: project.VideoModelProfileKey,
		TaskType:        provider.TaskTypeVideoCreateTask,
		Modality:        "video",
	})
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowErrorFromProvider(err, codeActivityFailed))
	}
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	agentContext, err := a.loadShotVideoPromptAgentContext(ctx, project, shot, assetContext, constraints.Candidates, input)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	contextJSON, err := json.Marshal(agentContext)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}

	generationRendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyShotVideoAgent, map[string]any{
		"context": map[string]any{"json": string(contextJSON)},
	})
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}
	generationRendered = applyVideoPromptAudioRuntimeContract(generationRendered)
	generationExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        shotVideoPromptNodeKey(nodeGenerateShotVideoPromptPrefix, shot.ShotIndex, input),
		NodeType:       "agent.video_prompt.generate",
		Input: mustJSON(map[string]any{
			"shotId":            shot.ID,
			"shotNo":            shot.ShotNo,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"videoModelProfile": project.VideoModelProfileKey,
			"videoModels":       agentContext.VideoModels,
			"promptTemplateKey": generationRendered.TemplateKey,
			"promptVersionId":   generationRendered.PromptVersionID,
			"promptHash":        generationRendered.RenderedHash,
		}),
	})
	if err != nil {
		return PrepareShotVideoPromptOutput{}, err
	}
	if err := a.markShotVideoPromptRunning(ctx, input, shot, generationExecution); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(generationExecution, err)
	}
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
			"maxOutputTokens": structuredVideoPromptOutputTokens,
			"temperature":     0.25,
		}),
		Options: providerTextGatewayOptions(),
	}
	draft, generationResponse, err := requestStructuredVideoPrompt(
		ctx,
		generationExecution,
		generationRequest,
		a.generateProviderText,
		parseGeneratedVideoPrompt,
	)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(generationExecution, workflowErrorFromProvider(err, codeActivityFailed))
	}
	if err := CompleteNodeRun(ctx, a.db, generationExecution, mustJSON(map[string]any{
		"providerCallId": generationResponse.ProviderCallID,
		"modelId":        generationResponse.ModelID,
		"prompt":         draft.Prompt,
		"negativePrompt": draft.NegativePrompt,
		"sourceAnchors":  draft.SourceAnchors,
	})); err != nil {
		return PrepareShotVideoPromptOutput{}, err
	}

	draftJSON := mustJSON(draft)
	reviewRendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyShotVideoReviewAgent, map[string]any{
		"context":   map[string]any{"json": string(contextJSON)},
		"candidate": map[string]any{"json": string(draftJSON)},
	})
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}
	reviewRendered = applyVideoPromptAudioRuntimeContract(reviewRendered)
	reviewExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        shotVideoPromptNodeKey(nodeReviewShotVideoPromptPrefix, shot.ShotIndex, input),
		NodeType:       "agent.video_prompt.review",
		Input: mustJSON(map[string]any{
			"shotId":            shot.ID,
			"shotNo":            shot.ShotNo,
			"generationCallId":  generationResponse.ProviderCallID,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"videoModels":       agentContext.VideoModels,
			"promptTemplateKey": reviewRendered.TemplateKey,
			"promptVersionId":   reviewRendered.PromptVersionID,
			"promptHash":        reviewRendered.RenderedHash,
		}),
	})
	if err != nil {
		return PrepareShotVideoPromptOutput{}, err
	}
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
			"maxOutputTokens": structuredVideoPromptOutputTokens,
			"temperature":     0.1,
		}),
		Options: providerTextGatewayOptions(),
	}
	reviewed, reviewResponse, err := requestStructuredVideoPrompt(
		ctx,
		reviewExecution,
		reviewRequest,
		a.generateProviderText,
		func(text string) (reviewedVideoPrompt, error) {
			reviewed, err := parseReviewedVideoPrompt(text, draft)
			if err != nil {
				return reviewedVideoPrompt{}, err
			}
			if !reviewed.Approved {
				return reviewedVideoPrompt{}, fmt.Errorf("video prompt review did not approve the candidate")
			}
			return reviewed, nil
		},
	)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, workflowErrorFromProvider(err, codeActivityFailed))
	}
	dialogueLines := NormalizeStoryboardDialogue(agentContext.Shot.ScriptDialogue)
	reviewedPrompt := firstNonEmptyString(reviewed.FinalPrompt, reviewed.Prompt)
	if err := validateVideoPromptDialogueScope(reviewedPrompt, agentContext); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	finalPrompt := composeAuthoritativeVideoPrompt(reviewedPrompt, dialogueLines)
	if err := validateVideoPromptDialogue(finalPrompt, agentContext); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	measurements, err := validateVideoPromptForCandidates(finalPrompt, constraints.Candidates)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	negativePrompt := firstNonEmptyString(reviewed.NegativePrompt, draft.NegativePrompt)
	output := PrepareShotVideoPromptOutput{
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
	if err := a.persistReviewedShotVideoPrompt(ctx, input, shot, reviewExecution, output, reviewed); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	return output, nil
}

func (a Activities) loadShotVideoPromptAgentContext(ctx context.Context, project ProjectProductionSettings, shot StoryboardShotRecord, assets ShotAssetContext, candidates []provider.GatewayModelConstraintCandidate, input PrepareShotVideoPromptInput) (shotVideoPromptAgentContext, error) {
	contextValue := shotVideoPromptAgentContext{
		Project: shotVideoPromptProject{
			ProjectType:    project.ProjectType,
			ContentType:    project.ContentType,
			ProductionMode: project.ProductionMode,
			ArtStyle:       project.ArtStyle,
			AspectRatio:    firstNonEmptyString(input.AspectRatio, project.VideoRatio, project.AspectRatio, "16:9"),
			DirectorManual: project.DirectorManual,
			VisualManual:   project.VisualManual,
		},
		Shot: shotVideoPromptShot{
			ShotID:            shot.ID,
			ShotNo:            shot.ShotNo,
			Duration:          firstPositiveFloat(input.Duration, shot.Duration, defaultShotDuration),
			AspectRatio:       firstNonEmptyString(input.AspectRatio, project.VideoRatio, project.AspectRatio, "16:9"),
			Resolution:        firstNonEmptyString(input.Resolution, "720p"),
			Title:             shot.Title,
			Visual:            shot.Visual,
			Camera:            shot.Camera,
			Motion:            shot.Motion,
			Mood:              shot.Mood,
			ExistingPrompt:    shot.VideoPrompt,
			ScriptDialogue:    append([]StoryboardDialogueLine(nil), shot.Dialogue...),
			ExecutionPlanID:   input.ExecutionPlanID,
			RenderSegmentID:   input.RenderSegmentID,
			SegmentIndex:      input.SegmentIndex,
			SegmentCount:      input.SegmentCount,
			SegmentStartTick:  input.SegmentStartTick,
			SegmentEndTick:    input.SegmentEndTick,
			RequestedDuration: input.RequestedDuration,
			AudioStrategy:     input.AudioStrategy,
			AudioRequirement:  input.AudioRequirement,
		},
		Assets:        assets.PromptAssets,
		ReferenceMode: shot.VideoReferenceMode,
		ReferenceKeys: shot.VideoReferenceKeys,
		VideoModels:   videoPromptModelContexts(candidates),
	}
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		contextValue.Shot.ScriptDialogue = append([]StoryboardDialogueLine(nil), input.DialogueLines...)
	}
	narrative, err := a.loadShotPromptNarrativeContext(ctx, project.ID, shot.ID)
	if err != nil {
		return shotVideoPromptAgentContext{}, err
	}
	contextValue.Source = narrative.Source
	contextValue.Script = narrative.Script
	contextValue.Scene = narrative.Scene
	return contextValue, nil
}

func videoPromptModelContexts(candidates []provider.GatewayModelConstraintCandidate) []videoPromptModelContext {
	result := make([]videoPromptModelContext, 0, len(candidates))
	for _, candidate := range candidates {
		target := 0
		if candidate.Prompt.MaxLength > 0 {
			target = candidate.Prompt.MaxLength * 85 / 100
		}
		result = append(result, videoPromptModelContext{
			ProviderModelID: candidate.ProviderModelID,
			ModelKey:        candidate.ModelKey,
			MaxLength:       candidate.Prompt.MaxLength,
			LengthUnit:      candidate.Prompt.Unit,
			TargetLength:    target,
		})
	}
	return result
}

func applyVideoPromptAudioRuntimeContract(rendered promptsvc.RenderedPrompt) promptsvc.RenderedPrompt {
	rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) + `

<runtime_audio_contract priority="highest">
- The complete episode script remains required narrative and continuity context. Use it to understand causal order, character state, performance, and transitions.
- Only shot.scriptDialogue defines this shot's audio. Never select audio from any other episode or scene text.
- CineWeave appends the authoritative structured audio timeline after independent review. The JSON prompt field must describe visuals, movement, performance, timing, and continuity without quoting, translating, paraphrasing, or duplicating any dialogue or audio text.
- When assigned audio affects performance, refer to the assigned authoritative audio timeline and the required lip-sync or off-screen delivery mode without reproducing its text.
- Do not emit the cineweave_authoritative_audio_timeline marker yourself.
</runtime_audio_contract>`
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+deterministic_audio_v2"
	return rendered
}

func requestStructuredVideoPrompt[T any](
	ctx context.Context,
	nodeExecution NodeExecution,
	request provider.GatewayTextRequest,
	call func(context.Context, NodeExecution, provider.GatewayTextRequest) (provider.GatewayTextResponse, error),
	parse func(string) (T, error),
) (T, provider.GatewayTextResponse, error) {
	var zero T
	var response provider.GatewayTextResponse
	var parseErr error
	for attempt := 1; attempt <= structuredVideoPromptAttempts; attempt++ {
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
		var deterministic workflowError
		if errors.As(err, &deterministic) {
			return zero, response, err
		}
		if attempt < structuredVideoPromptAttempts {
			request.Input = structuredVideoPromptRetryInput(request.Input, attempt+1, err)
		}
	}
	return zero, response, fmt.Errorf("%w: structured video prompt output remained invalid after %d attempts: %v", provider.ErrValidation, structuredVideoPromptAttempts, parseErr)
}

func structuredVideoPromptRetryInput(raw json.RawMessage, attempt int, cause error) json.RawMessage {
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return raw
	}
	prompt, _ := input["prompt"].(string)
	input["prompt"] = strings.TrimSpace(prompt) + fmt.Sprintf(`

结构化输出纠错（第 %d 次尝试）：上一次输出未通过结构化格式校验：%s
只返回一个完整、紧凑且可解析的 JSON 对象。不要输出推理过程、Markdown、解释或未闭合字符串；严格修复指出的问题。`, attempt, cause.Error())
	input["maxOutputTokens"] = structuredVideoPromptOutputTokens
	input["temperature"] = 0
	return mustJSON(input)
}

func parseGeneratedVideoPrompt(text string) (generatedVideoPrompt, error) {
	var output generatedVideoPrompt
	if err := decodeAgentJSONObject(text, &output); err != nil {
		return generatedVideoPrompt{}, fmt.Errorf("video prompt agent returned invalid JSON: %w", err)
	}
	output.Prompt = strings.TrimSpace(output.Prompt)
	output.NegativePrompt = strings.TrimSpace(output.NegativePrompt)
	output.DialogueLines = NormalizeStoryboardDialogue(output.DialogueLines)
	if output.Prompt == "" {
		return generatedVideoPrompt{}, fmt.Errorf("video prompt agent returned an empty prompt")
	}
	return output, nil
}

func parseReviewedVideoPrompt(text string, draft generatedVideoPrompt) (reviewedVideoPrompt, error) {
	var output reviewedVideoPrompt
	if err := decodeAgentJSONObject(text, &output); err != nil {
		return reviewedVideoPrompt{}, fmt.Errorf("video prompt review agent returned invalid JSON: %w", err)
	}
	output.Prompt = strings.TrimSpace(output.Prompt)
	output.FinalPrompt = strings.TrimSpace(output.FinalPrompt)
	output.NegativePrompt = strings.TrimSpace(output.NegativePrompt)
	output.DialogueLines = NormalizeStoryboardDialogue(output.DialogueLines)
	if firstNonEmptyString(output.FinalPrompt, output.Prompt) == "" {
		return reviewedVideoPrompt{}, fmt.Errorf("video prompt review agent returned an empty prompt")
	}
	if output.NegativePrompt == "" {
		output.NegativePrompt = draft.NegativePrompt
	}
	if len(output.DialogueLines) == 0 {
		output.DialogueLines = append([]StoryboardDialogueLine(nil), draft.DialogueLines...)
	}
	return output, nil
}

func validateVideoPromptDialogue(prompt string, contextValue shotVideoPromptAgentContext) error {
	prompt = strings.TrimSpace(prompt)
	required := NormalizeStoryboardDialogue(contextValue.Shot.ScriptDialogue)
	if injected, found, err := extractAuthoritativeVideoPromptAudio(prompt); err != nil {
		return workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()}
	} else if found {
		if !storyboardDialogueEquivalent(injected, required) {
			return workflowError{Code: provider.CodeInvalidRequest, Message: "video prompt authoritative audio timeline does not match the shot timing assignment"}
		}
	} else {
		for _, line := range required {
			if !videoPromptContainsRequiredDialogueLine(prompt, line) {
				return workflowError{Code: provider.CodeInvalidRequest, Message: fmt.Sprintf("video prompt did not preserve required Chinese dialogue verbatim: %s: %s", line.Speaker, line.Text)}
			}
		}
	}
	return validateVideoPromptDialogueScope(prompt, contextValue)
}

func validateVideoPromptDialogueScope(prompt string, contextValue shotVideoPromptAgentContext) error {
	required := NormalizeStoryboardDialogue(contextValue.Shot.ScriptDialogue)
	// Timing-assigned shot dialogue is authoritative. Titles, visual descriptions,
	// and an existing prompt may be stale after timing boundaries are corrected.
	scriptDialogue := ExtractScriptDialogueLines(strings.Join(compactStrings([]string{
		contextValue.Script.Content,
		contextValue.Scene.Dialogue,
		contextValue.Scene.Content,
	}), "\n"))
	for _, line := range NormalizeStoryboardDialogue(scriptDialogue) {
		if storyboardDialogueLineAssigned(line, required) {
			continue
		}
		if videoPromptContainsDialogueLine(prompt, line) {
			return workflowError{Code: provider.CodeInvalidRequest, Message: fmt.Sprintf("video prompt added dialogue outside the authoritative shot timing: %s: %s", line.Speaker, line.Text)}
		}
	}
	return nil
}

func composeAuthoritativeVideoPrompt(prompt string, dialogue []StoryboardDialogueLine) string {
	prompt = stripAuthoritativeVideoPromptAudio(strings.TrimSpace(prompt))
	dialogue = NormalizeStoryboardDialogue(dialogue)
	payload, _ := json.Marshal(dialogue)
	rules := "Earlier audio or dialogue instructions are non-authoritative and must be ignored. Execute only the ordered JSON audio timeline below. Preserve every Chinese text value verbatim. kind=dialogue uses synchronized character speech; kind=voiceover or narration is off-screen speech; kind=system is non-character audio without lip sync. Audio is never rendered as subtitles or visible text."
	if len(dialogue) == 0 {
		rules = "Earlier audio or dialogue instructions are non-authoritative and must be ignored. This shot has no dialogue, narration, voiceover, system audio, music, ambience, or sound effects. Do not animate lip sync and do not render subtitles or visible text."
	}
	return strings.TrimSpace(prompt + "\n\n" + rules + "\n" + authoritativeAudioStartMarker + "\n" + string(payload) + "\n" + authoritativeAudioEndMarker)
}

func stripAuthoritativeVideoPromptAudio(prompt string) string {
	for {
		start := strings.Index(prompt, authoritativeAudioStartMarker)
		if start < 0 {
			return strings.TrimSpace(prompt)
		}
		endRelative := strings.Index(prompt[start+len(authoritativeAudioStartMarker):], authoritativeAudioEndMarker)
		if endRelative < 0 {
			return strings.TrimSpace(prompt[:start])
		}
		end := start + len(authoritativeAudioStartMarker) + endRelative + len(authoritativeAudioEndMarker)
		prompt = strings.TrimSpace(prompt[:start] + "\n" + prompt[end:])
	}
}

func extractAuthoritativeVideoPromptAudio(prompt string) ([]StoryboardDialogueLine, bool, error) {
	start := strings.LastIndex(prompt, authoritativeAudioStartMarker)
	if start < 0 {
		return nil, false, nil
	}
	bodyStart := start + len(authoritativeAudioStartMarker)
	endRelative := strings.Index(prompt[bodyStart:], authoritativeAudioEndMarker)
	if endRelative < 0 {
		return nil, true, fmt.Errorf("video prompt authoritative audio timeline is not closed")
	}
	var lines []StoryboardDialogueLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(prompt[bodyStart:bodyStart+endRelative])), &lines); err != nil {
		return nil, true, fmt.Errorf("video prompt authoritative audio timeline is invalid: %w", err)
	}
	return NormalizeStoryboardDialogue(lines), true, nil
}

func storyboardDialogueEquivalent(left, right []StoryboardDialogueLine) bool {
	left = NormalizeStoryboardDialogue(left)
	right = NormalizeStoryboardDialogue(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if dialogueLineKey(left[index]) != dialogueLineKey(right[index]) ||
			strings.TrimSpace(left[index].Delivery) != strings.TrimSpace(right[index].Delivery) ||
			left[index].Kind != right[index].Kind {
			return false
		}
	}
	return true
}

func storyboardDialogueLineAssigned(line StoryboardDialogueLine, assigned []StoryboardDialogueLine) bool {
	normalized := NormalizeStoryboardDialogue([]StoryboardDialogueLine{line})
	if len(normalized) == 0 {
		return false
	}
	line = normalized[0]
	lineText := normalizeDialogueTextForComparison(line.Text)
	for _, candidate := range NormalizeStoryboardDialogue(assigned) {
		if strings.TrimSpace(candidate.Speaker) != strings.TrimSpace(line.Speaker) {
			continue
		}
		candidateText := normalizeDialogueTextForComparison(candidate.Text)
		if candidateText != "" && lineText != "" && (candidateText == lineText || strings.Contains(candidateText, lineText) || strings.Contains(lineText, candidateText)) {
			return true
		}
	}
	return false
}

func videoPromptContainsRequiredDialogueLine(prompt string, line StoryboardDialogueLine) bool {
	normalizedPrompt := normalizeDialogueTextForComparison(prompt)
	text := normalizeDialogueTextForComparison(line.Text)
	if text == "" || !strings.Contains(normalizedPrompt, text) {
		return false
	}
	speaker := strings.TrimSpace(line.Speaker)
	return speaker == "" || strings.Contains(prompt, speaker)
}

func videoPromptContainsDialogueLine(prompt string, line StoryboardDialogueLine) bool {
	text := normalizeDialogueTextForComparison(line.Text)
	if text == "" || !strings.Contains(normalizeDialogueTextForComparison(prompt), text) {
		return false
	}
	if utf8.RuneCountInString(text) >= 4 {
		return true
	}
	return strings.Contains(prompt, strings.TrimSpace(line.Speaker))
}

func decodeAgentJSONObject(text string, target any) error {
	candidate := stripJSONFence(text)
	if err := json.Unmarshal([]byte(candidate), target); err == nil {
		return nil
	}
	start := strings.Index(candidate, "{")
	end := strings.LastIndex(candidate, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("JSON object was not found")
	}
	return json.Unmarshal([]byte(candidate[start:end+1]), target)
}

func validateVideoPromptForCandidates(prompt string, candidates []provider.GatewayModelConstraintCandidate) (map[string]int, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, workflowError{Code: provider.CodeInvalidRequest, Message: "reviewed video prompt is empty"}
	}
	measurements := map[string]int{}
	for _, candidate := range candidates {
		unit := candidate.Prompt.Unit
		if unit == "" {
			unit = provider.PromptLengthUnitCharacters
		}
		length := provider.MeasurePromptLength(prompt, unit)
		measurements[candidate.ModelKey+":"+unit] = length
		if candidate.Prompt.MaxLength > 0 && length > candidate.Prompt.MaxLength {
			return nil, workflowError{
				Code:    provider.CodeInvalidRequest,
				Message: fmt.Sprintf("reviewed video prompt length %d exceeds %s limit of %d %s", length, candidate.ModelKey, candidate.Prompt.MaxLength, unit),
			}
		}
	}
	return measurements, nil
}

func (a Activities) persistReviewedShotVideoPrompt(ctx context.Context, input PrepareShotVideoPromptInput, shot StoryboardShotRecord, nodeExecution NodeExecution, output PrepareShotVideoPromptOutput, review reviewedVideoPrompt) error {
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		return a.persistReviewedRenderSegmentPrompt(ctx, input, shot, nodeExecution, output, review)
	}
	dialogueBackfilled := len(shot.Dialogue) == 0 && len(output.DialogueLines) > 0
	metadata := mustJSON(map[string]any{
		"videoPromptAgent": map[string]any{
			"status":                    "approved",
			"generationProviderCallId":  output.GenerationProviderCallID,
			"generationModelId":         output.GenerationModelID,
			"generationPromptVersionId": output.GenerationPromptVersion,
			"reviewProviderCallId":      output.ReviewProviderCallID,
			"reviewModelId":             output.ReviewModelID,
			"reviewPromptVersionId":     output.ReviewPromptVersion,
			"promptHash":                output.PromptHash,
			"negativePrompt":            output.NegativePrompt,
			"modelCandidates":           output.ModelCandidates,
			"promptMeasurements":        output.PromptMeasurements,
			"dialogueLines":             output.DialogueLines,
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
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt = $2,
		    script_dialogue = CASE
		      WHEN jsonb_array_length(script_dialogue) = 0 AND jsonb_array_length($6::jsonb) > 0 THEN $6::jsonb
		      ELSE script_dialogue
		    END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    video_prompt_status = 'succeeded',
		    video_prompt_error_code = NULL,
		    video_prompt_error_message = NULL,
		    video_prompt_workflow_run_id = NULLIF($5, '')::uuid,
		    video_prompt_updated_at = now(),
		    video_error_code = NULL,
		    video_error_message = NULL,
		    updated_at = now()
		WHERE project_id = $1 AND id = $4 AND deleted_at IS NULL
	`, input.ProjectID, output.Prompt, metadata, shot.ID, input.WorkflowRunID, mustJSON(output.DialogueLines)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video_prompt.reviewed", "storyboard_shot", shot.ID, mustJSON(map[string]any{
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
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) persistReviewedRenderSegmentPrompt(ctx context.Context, input PrepareShotVideoPromptInput, shot StoryboardShotRecord, nodeExecution NodeExecution, output PrepareShotVideoPromptOutput, review reviewedVideoPrompt) error {
	metadata := mustJSON(map[string]any{
		"videoPromptAgent": map[string]any{
			"status": "approved", "promptHash": output.PromptHash, "negativePrompt": output.NegativePrompt,
			"generationProviderCallId": output.GenerationProviderCallID, "generationModelId": output.GenerationModelID,
			"generationTemplateKey": output.GenerationTemplateKey, "generationPromptVersionId": output.GenerationPromptVersion,
			"reviewProviderCallId": output.ReviewProviderCallID, "reviewModelId": output.ReviewModelID,
			"reviewTemplateKey": output.ReviewTemplateKey, "reviewPromptVersionId": output.ReviewPromptVersion,
			"promptMeasurements": output.PromptMeasurements, "issues": review.Issues, "changes": review.Changes,
		},
	})
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		UPDATE video_render_segments
		SET prompt = $4, dialogue = $5::jsonb, error_code = NULL, error_message = NULL,
		    metadata = (metadata || $6::jsonb) || jsonb_build_object('promptStatus', 'succeeded', 'promptCompletedAt', now()),
		    updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2 AND project_id = $3
	`, input.RenderSegmentID, input.ExecutionPlanID, input.ProjectID, output.Prompt, mustJSON(output.DialogueLines), metadata)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return workflowError{Code: provider.CodeRenderPlanReplanRequired, Message: "video render segment is no longer active for prompt generation"}
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.segment.prompt.reviewed", "video_render_segment", input.RenderSegmentID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "executionPlanId": input.ExecutionPlanID,
		"renderSegmentId": input.RenderSegmentID, "segmentIndex": input.SegmentIndex,
		"promptHash": output.PromptHash, "dialogueLines": output.DialogueLines,
		"generationProviderCallId": output.GenerationProviderCallID, "reviewProviderCallId": output.ReviewProviderCallID,
	})); err != nil {
		return err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func resolvedVideoPromptHash(prompt, provided string) string {
	if value := strings.TrimSpace(provided); value != "" {
		return value
	}
	return promptsvc.HashText(strings.TrimSpace(prompt))
}

func shotVideoPromptNodeKey(prefix string, shotIndex int, input PrepareShotVideoPromptInput) string {
	base := nodeKeyForShot(prefix, shotIndex)
	if strings.TrimSpace(input.RenderSegmentID) == "" {
		return base
	}
	return fmt.Sprintf("%s_segment_%d_retry_%d", base, input.SegmentIndex, input.RetryGeneration)
}

func (a Activities) markShotVideoPromptRunning(ctx context.Context, input PrepareShotVideoPromptInput, shot StoryboardShotRecord, nodeExecution NodeExecution) error {
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE video_render_segments
			SET error_code = NULL, error_message = NULL,
			    metadata = metadata || jsonb_build_object('promptStatus', 'running', 'promptStartedAt', now()), updated_at = now()
			WHERE id = $1 AND video_render_plan_id = $2 AND project_id = $3
		`, input.RenderSegmentID, input.ExecutionPlanID, input.ProjectID); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.segment.prompt.running", "video_render_segment", input.RenderSegmentID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "executionPlanId": input.ExecutionPlanID,
			"renderSegmentId": input.RenderSegmentID, "segmentIndex": input.SegmentIndex,
		})); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt_status = 'running',
		    video_prompt_error_code = NULL,
		    video_prompt_error_message = NULL,
		    video_prompt_workflow_run_id = NULLIF($2, '')::uuid,
		    video_prompt_updated_at = now(),
		    updated_at = now()
		WHERE project_id = $1 AND id = $3 AND deleted_at IS NULL
	`, input.ProjectID, input.WorkflowRunID, shot.ID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video_prompt.running", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, "video_prompt_running")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) failShotVideoPromptActivity(ctx context.Context, input PrepareShotVideoPromptInput, baseInput TextToStoryboardInput, shot StoryboardShotRecord, nodeExecution NodeExecution, cause error) error {
	if isWorkflowWriteFenced(cause) {
		return discardWorkflowResult(ctx, a.db, nodeExecution, cause.Error())
	}
	code, message := workflowErrorFields(cause, codeActivityFailed)
	if !nodeExecution.valid() {
		return newWorkflowApplicationError(cause, code, message)
	}
	persistCtx, cancel := workflowFailurePersistenceContext(ctx)
	defer cancel()
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		_ = a.failRenderSegmentPrompt(persistCtx, input, shot, nodeExecution, code, message)
		return newWorkflowApplicationError(cause, code, message)
	}
	tx, err := a.db.Begin(persistCtx)
	if err == nil {
		defer tx.Rollback(persistCtx)
		_, err = lockNodeBusinessWrite(persistCtx, tx, input.WorkflowRunID, nodeExecution)
	}
	if err == nil {
		_, err = tx.Exec(persistCtx, `
		UPDATE storyboard_shots
		SET video_prompt_status = 'failed',
		    video_prompt_error_code = $2,
		    video_prompt_error_message = $3,
		    video_prompt_workflow_run_id = NULLIF($4, '')::uuid,
		    video_prompt_updated_at = now(),
		    status = CASE WHEN $5 THEN status ELSE 'video_failed' END,
		    video_status = CASE WHEN $5 THEN video_status ELSE 'failed' END,
		    video_error_code = CASE WHEN $5 THEN video_error_code ELSE $2 END,
		    video_error_message = CASE WHEN $5 THEN video_error_message ELSE $3 END,
		    video_completed_at = CASE WHEN $5 THEN video_completed_at ELSE now() END,
		    updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`, shot.ID, code, message, input.WorkflowRunID, input.PromptOnly)
	}
	if err == nil {
		err = insertEvent(persistCtx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video_prompt.failed", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, "video_prompt_failed"))
	}
	if err == nil && !input.PromptOnly {
		err = insertEvent(persistCtx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.failed", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, "video_failed"))
	}
	if err == nil {
		_, err = failNodeRunTx(persistCtx, tx, nodeExecution, code, message, json.RawMessage(`{}`))
	}
	if err == nil && !input.PromptOnly && shouldTransitionWorkflowOnActivityFailure(baseInput) {
		_, _, err = transitionWorkflowRunTx(persistCtx, tx, input.WorkflowRunID, "failed", code, message, mustJSON(map[string]any{
			"code": code, "message": message,
		}))
	}
	if err == nil {
		err = tx.Commit(persistCtx)
	}
	return newWorkflowApplicationError(cause, code, message)
}

func (a Activities) failRenderSegmentPrompt(ctx context.Context, input PrepareShotVideoPromptInput, shot StoryboardShotRecord, nodeExecution NodeExecution, code, message string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_segments
		SET error_code = $4, error_message = $5,
		    metadata = metadata || jsonb_build_object('promptStatus', 'failed', 'promptFailedAt', now()), updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2 AND project_id = $3
	`, input.RenderSegmentID, input.ExecutionPlanID, input.ProjectID, code, message); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET metadata = metadata || jsonb_build_object('promptStatus', 'failed', 'promptFailedAt', now()), updated_at = now()
		WHERE id = $1 AND project_id = $2
	`, input.ExecutionPlanID, input.ProjectID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_prompt_status = 'failed', video_prompt_error_code = $2, video_prompt_error_message = $3,
		    video_prompt_workflow_run_id = NULLIF($5, '')::uuid,
		    video_prompt_updated_at = now(), updated_at = now()
		WHERE id = $1 AND project_id = $4
	`, shot.ID, code, message, input.ProjectID, input.WorkflowRunID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.segment.prompt.failed", "video_render_segment", input.RenderSegmentID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID, "shotId": shot.ID, "executionPlanId": input.ExecutionPlanID,
		"renderSegmentId": input.RenderSegmentID, "segmentIndex": input.SegmentIndex, "errorCode": code, "errorMessage": message,
	})); err != nil {
		return err
	}
	if applied, err := failNodeRunTx(ctx, tx, nodeExecution, code, message, json.RawMessage(`{}`)); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func executeAgentReviewedShotVideoCreate(ctx, createCtx workflow.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
	promptCtx := workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	var prepared PrepareShotVideoPromptOutput
	if err := workflow.ExecuteActivity(promptCtx, "PrepareShotVideoPrompt", PrepareShotVideoPromptInput{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		CreatedBy:         input.CreatedBy,
		ShotID:            input.ShotID,
		ShotIndex:         input.ShotIndex,
		ShotNo:            input.ShotNo,
		WorkflowPrompt:    input.WorkflowPrompt,
		Duration:          firstPositiveFloat(input.PlannedDuration, input.Duration),
		RequestedDuration: input.Duration,
		AspectRatio:       input.AspectRatio,
		Resolution:        input.Resolution,
		Force:             input.Force,
		ExecutionPlanID:   input.ExecutionPlanID,
		RenderSegmentID:   input.RenderSegmentID,
		SegmentIndex:      input.SegmentIndex,
		SegmentCount:      input.SegmentCount,
		SegmentStartTick:  input.SegmentStartTick,
		SegmentEndTick:    input.SegmentEndTick,
		RetryGeneration:   input.RetryGeneration,
		AudioStrategy:     input.AudioStrategy,
		AudioRequirement:  input.AudioRequirement,
		DialogueLines:     input.DialogueLines,
	}).Get(promptCtx, &prepared); err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	input.Prompt = prepared.Prompt
	input.NegativePrompt = prepared.NegativePrompt
	input.PromptHash = prepared.PromptHash
	input.GenerationProviderCallID = prepared.GenerationProviderCallID
	input.ReviewProviderCallID = prepared.ReviewProviderCallID
	input.ReviewTemplateKey = prepared.ReviewTemplateKey
	input.ReviewPromptVersionID = prepared.ReviewPromptVersion
	var created CreateShotVideoTaskOutput
	if err := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", input).Get(createCtx, &created); err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	return created, nil
}
