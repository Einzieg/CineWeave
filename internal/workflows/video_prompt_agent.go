package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

const (
	nodeGenerateShotVideoPromptPrefix   = "generate_shot_video_prompt"
	nodeReviewShotVideoPromptPrefix     = "review_shot_video_prompt"
	structuredVideoPromptAttempts       = 3
	structuredVideoPromptOutputTokens   = 6000
	legacyAuthoritativeAudioStartMarker = "<cineweave_authoritative_audio_timeline>"
	legacyAuthoritativeAudioEndMarker   = "</cineweave_authoritative_audio_timeline>"
	authoritativeSpeechStartMarker      = "<cineweave_authoritative_speech_timeline>"
	authoritativeSpeechEndMarker        = "</cineweave_authoritative_speech_timeline>"
	authoritativeSoundStartMarker       = "<cineweave_non_speech_sound_timeline>"
	authoritativeSoundEndMarker         = "</cineweave_non_speech_sound_timeline>"
	videoExecutionStartMarker           = "<cineweave_video_execution_contract>"
	videoExecutionEndMarker             = "</cineweave_video_execution_contract>"
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
	SoundCues                []StoryboardDialogueLine                   `json:"soundCues,omitempty"`
	ReferencePackID          string                                     `json:"referencePackId,omitempty"`
	ReferencePackHash        string                                     `json:"referencePackHash,omitempty"`
	CapabilitySnapshotHash   string                                     `json:"capabilitySnapshotHash,omitempty"`
	PromptContextPlanID      string                                     `json:"promptContextPlanId,omitempty"`
	PromptContextPlanHash    string                                     `json:"promptContextPlanHash,omitempty"`
	VideoPromptPlanID        string                                     `json:"videoPromptPlanId,omitempty"`
	NativeAudioRequired      bool                                       `json:"nativeAudioRequired"`
	ModelSupportsNativeAudio bool                                       `json:"modelSupportsNativeAudio"`
	GenerationContract       *videoproduction.PromptContractProvenance  `json:"generationContract,omitempty"`
	ReviewContract           *videoproduction.PromptContractProvenance  `json:"reviewContract,omitempty"`
	DeterministicReview      *videoproduction.PromptContractReview      `json:"deterministicReview,omitempty"`
}

type shotVideoPromptAgentContext struct {
	Project           shotVideoPromptProject                 `json:"project"`
	Source            shotVideoPromptSource                  `json:"source"`
	Script            shotVideoPromptScript                  `json:"script"`
	Scene             shotVideoPromptScene                   `json:"scene"`
	Shot              shotVideoPromptShot                    `json:"shot"`
	Assets            []ShotVideoPromptAsset                 `json:"assets"`
	VideoModels       []videoPromptModelContext              `json:"videoModels"`
	ReferenceMode     string                                 `json:"referenceMode"`
	ReferenceKeys     []string                               `json:"referenceKeys"`
	ShotState         *videoproduction.ShotState             `json:"shotState,omitempty"`
	Transition        *videoproduction.ShotTransition        `json:"transition,omitempty"`
	ReferencePack     *videoproduction.ReferencePackManifest `json:"referencePack,omitempty"`
	PromptContextPlan *videoproduction.PromptContextPlan     `json:"promptContextPlan,omitempty"`
	PanelManifest     *videoproduction.PanelManifest         `json:"panelManifest,omitempty"`
}

type shotVideoPromptProject struct {
	ProjectType               string `json:"projectType,omitempty"`
	ContentType               string `json:"contentType,omitempty"`
	VideoProductionProfileKey string `json:"videoProductionProfileKey"`
	ArtStyle                  string `json:"artStyle,omitempty"`
	AspectRatio               string `json:"aspectRatio"`
	DirectorManual            string `json:"directorManual,omitempty"`
	VisualManual              string `json:"visualManual,omitempty"`
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
	ShotID            string                    `json:"shotId"`
	ShotNo            int                       `json:"shotNo"`
	Duration          float64                   `json:"duration"`
	AspectRatio       string                    `json:"aspectRatio"`
	Resolution        string                    `json:"resolution"`
	Title             string                    `json:"title,omitempty"`
	Visual            string                    `json:"visual,omitempty"`
	Camera            string                    `json:"camera,omitempty"`
	Motion            string                    `json:"motion,omitempty"`
	Mood              string                    `json:"mood,omitempty"`
	ExistingPrompt    string                    `json:"existingPrompt,omitempty"`
	ScriptDialogue    []StoryboardDialogueLine  `json:"scriptDialogue"`
	SoundCues         []StoryboardDialogueLine  `json:"soundCues,omitempty"`
	ExecutionPlanID   string                    `json:"executionPlanId,omitempty"`
	RenderSegmentID   string                    `json:"renderSegmentId,omitempty"`
	SegmentIndex      int                       `json:"segmentIndex,omitempty"`
	SegmentCount      int                       `json:"segmentCount,omitempty"`
	SegmentStartTick  int64                     `json:"segmentStartTick,omitempty"`
	SegmentEndTick    int64                     `json:"segmentEndTick,omitempty"`
	RequestedDuration float64                   `json:"requestedDuration,omitempty"`
	TimelineTimebase  int64                     `json:"timelineTimebase"`
	DialogueTiming    []shotVideoDialogueTiming `json:"dialogueTiming,omitempty"`
	SoundCueTiming    []shotVideoDialogueTiming `json:"soundCueTiming,omitempty"`
	AudioStrategy     string                    `json:"audioStrategy,omitempty"`
	AudioRequirement  string                    `json:"audioRequirement,omitempty"`
}

type shotVideoDialogueTiming struct {
	TimingUnitID    string  `json:"timingUnitId,omitempty"`
	Speaker         string  `json:"speaker,omitempty"`
	Kind            string  `json:"kind,omitempty"`
	StartSeconds    float64 `json:"startSeconds"`
	EndSeconds      float64 `json:"endSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
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
	Issues         agentJSONList            `json:"issues"`
	Changes        agentJSONList            `json:"changes"`
}

const videoPromptAgentMaxReviewRounds = 3

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
	shot.Dialogue, err = a.localizeStoryboardShotDialogue(ctx, shot)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, err
	}
	fail := func(nodeExecution NodeExecution, cause error) error {
		return a.failShotVideoPromptActivity(ctx, input, baseInput, shot, nodeExecution, cause)
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
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
	var productionContract shotProductionContractContext
	var referencePack videoproduction.ReferencePack
	var promptContextPlan videoproduction.PromptContextPlan
	primaryAnchorRole, err := resolveShotAnchorRole(project.VideoProductionProfileKey, "")
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}
	productionContract, err = a.loadShotProductionContract(ctx, input.ProjectID, shot.ID, primaryAnchorRole)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: videoproduction.CodePromptContractIncomplete, Message: err.Error()})
	}
	anchorCandidates, loadErr := a.loadProfileVideoReferenceCandidates(ctx, input.ProjectID, shot.ID, project.VideoProductionProfileKey, productionContract)
	if loadErr != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, loadErr)
	}
	referencePack, err = resolveVideoReferencePack(project, productionContract, anchorCandidates, constraints.Candidates)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}
	textConstraints, constraintsErr := a.gateway.ResolveModelConstraints(ctx, provider.GatewayModelConstraintsRequest{
		OrganizationID:  input.OrganizationID,
		ModelProfileKey: project.ScriptModelProfileKey,
		TaskType:        provider.TaskTypeTextGenerate,
		Modality:        "text",
	})
	if constraintsErr != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowErrorFromProvider(constraintsErr, codeActivityFailed))
	}
	promptContextPlan, err = a.compileShotPromptContextPlan(ctx, project, shot, productionContract.EntryState, textConstraints.Candidates)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}
	basePromptContextPlanHash := promptContextPlan.PlanHash
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		promptContextPlan, err = scopeVideoPromptContextPlanToSegment(promptContextPlan, input.DialogueLines)
		if err != nil {
			return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
		}
	}
	agentContext, err := a.loadShotVideoPromptAgentContext(ctx, project, shot, assetContext, constraints.Candidates, input)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	agentContext.ShotState = &productionContract.EntryState
	agentContext.Transition = &productionContract.Transition
	agentContext.ReferencePack = &referencePack.Manifest
	agentContext.PromptContextPlan = &promptContextPlan
	if project.VideoProductionProfileKey == videoproduction.ProfileStoryboardSheet {
		manifestRuntime, manifestErr := a.loadApprovedStoryboardSheetManifest(ctx, shot.ID)
		if manifestErr != nil {
			return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, manifestErr)
		}
		agentContext.PanelManifest = &manifestRuntime.Manifest
	}
	agentContext.Source.Content = ""
	agentContext.Script.Content = ""
	agentContext.Scene.Content = ""
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}

	var generationContract *videoproduction.PromptContractProvenance
	var generationRendered promptsvc.RenderedPrompt
	compiled, renderErr := a.renderVideoProductionPromptContract(
		ctx, input.OrganizationID, input.ProjectID, project,
		videoproduction.PromptRoleVideoGenerate, promptContextPlan,
		productionContract, referencePack,
		map[string]any{"agentContext": agentContext},
	)
	if renderErr != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, renderErr)
	}
	generationRendered = compiled.Rendered
	generationContract = &compiled.Contract.Provenance
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, err)
	}
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
	input.ShotID = shot.ID
	referencePackID := ""
	promptContextPlanID := ""
	promptContextPlanHash := basePromptContextPlanHash
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		persistedContext, loadErr := a.loadRenderSegmentPromptContextIdentity(
			ctx, project, shot, input.ExecutionPlanID, input.RenderSegmentID, basePromptContextPlanHash,
		)
		if loadErr != nil {
			return PrepareShotVideoPromptOutput{}, fail(generationExecution, loadErr)
		}
		promptContextPlanID = persistedContext.ID
		promptContextPlanHash = persistedContext.Plan.PlanHash
	} else {
		persistedContext, persistErr := a.persistPromptContextPlan(
			ctx, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.CreatedBy,
			generationExecution, project, shot, promptContextPlan,
		)
		if persistErr != nil {
			return PrepareShotVideoPromptOutput{}, fail(generationExecution, persistErr)
		}
		promptContextPlanID = persistedContext.ID
		promptContextPlanHash = persistedContext.Plan.PlanHash
	}
	referencePackID, err = a.persistShotReferencePackForPurpose(ctx, shotReferencePackPersistenceInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		ShotID:         shot.ID,
		LinkAnchor:     false,
	}, generationExecution, project, productionContract, referencePack)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(generationExecution, err)
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
	draft.DialogueLines = authoritativeVideoPromptDialogueLines(agentContext.Shot.ScriptDialogue)
	draft.Prompt = sanitizeVideoPromptVisualSafety(draft.Prompt, videoPromptShotAudioCues(agentContext.Shot))
	draft.Prompt, err = constrainVideoVisualPrompt(
		draft.Prompt, constraints.Candidates, agentContext.Shot, videoPromptShotAudioCues(agentContext.Shot),
	)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(generationExecution, err)
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

	baseGenerationPrompt := generationRendered.RenderedText
	var reviewContract *videoproduction.PromptContractProvenance
	var reviewRendered promptsvc.RenderedPrompt
	var reviewExecution NodeExecution
	var reviewed reviewedVideoPrompt
	var reviewResponse provider.GatewayTextResponse
	for reviewRound := 1; reviewRound <= videoPromptAgentMaxReviewRounds; reviewRound++ {
		reviewCompiled, renderErr := a.renderVideoProductionPromptContract(
			ctx, input.OrganizationID, input.ProjectID, project,
			videoproduction.PromptRoleVideoReview, promptContextPlan,
			productionContract, referencePack,
			map[string]any{
				"agentContext": agentContext,
				"candidate":    draft,
				"reviewRound":  reviewRound,
			},
		)
		if renderErr != nil {
			return PrepareShotVideoPromptOutput{}, fail(NodeExecution{}, renderErr)
		}
		reviewRendered = reviewCompiled.Rendered
		reviewContract = &reviewCompiled.Contract.Provenance
		reviewExecution, err = StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			NodeKey: videoPromptReviewRoundNodeKey(
				shotVideoPromptNodeKey(nodeReviewShotVideoPromptPrefix, shot.ShotIndex, input),
				reviewRound,
			),
			NodeType: "agent.video_prompt.review",
			Input: mustJSON(map[string]any{
				"shotId":            shot.ID,
				"shotNo":            shot.ShotNo,
				"reviewRound":       reviewRound,
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
		reviewed, reviewResponse, err = requestStructuredVideoPrompt(
			ctx,
			reviewExecution,
			reviewRequest,
			a.generateProviderText,
			func(text string) (reviewedVideoPrompt, error) {
				return parseReviewedVideoPrompt(text, draft)
			},
		)
		if err != nil {
			return PrepareShotVideoPromptOutput{}, fail(reviewExecution, workflowErrorFromProvider(err, codeActivityFailed))
		}
		reviewed.DialogueLines = authoritativeVideoPromptDialogueLines(agentContext.Shot.ScriptDialogue)
		if reviewed.Approved {
			break
		}
		if err := CompleteNodeRun(ctx, a.db, reviewExecution, mustJSON(map[string]any{
			"providerCallId": reviewResponse.ProviderCallID,
			"modelId":        reviewResponse.ModelID,
			"approved":       false,
			"reviewRound":    reviewRound,
			"issues":         reviewed.Issues,
			"changes":        reviewed.Changes,
		})); err != nil {
			return PrepareShotVideoPromptOutput{}, err
		}
		if reviewRound == videoPromptAgentMaxReviewRounds {
			return PrepareShotVideoPromptOutput{}, fail(reviewExecution, workflowError{
				Code:    provider.CodeInvalidRequest,
				Message: fmt.Sprintf("视频提示词审核连续 %d 轮未通过：%s", videoPromptAgentMaxReviewRounds, strings.Join(agentJSONListMessages(reviewed.Issues), "；")),
			})
		}

		correctionRound := reviewRound + 1
		correctionPrompt := composeVideoPromptReviewCorrection(baseGenerationPrompt, draft, reviewed, correctionRound)
		correctionPromptHash := promptsvc.HashText(correctionPrompt)
		generationExecution, err = StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			NodeKey: videoPromptReviewRoundNodeKey(
				shotVideoPromptNodeKey(nodeGenerateShotVideoPromptPrefix, shot.ShotIndex, input),
				correctionRound,
			),
			NodeType: "agent.video_prompt.revise",
			Input: mustJSON(map[string]any{
				"shotId":                 shot.ID,
				"shotNo":                 shot.ShotNo,
				"reviewRound":            correctionRound,
				"previousGenerationCall": generationResponse.ProviderCallID,
				"reviewCallId":           reviewResponse.ProviderCallID,
				"reviewIssues":           reviewed.Issues,
				"modelProfileKey":        project.ScriptModelProfileKey,
				"promptTemplateKey":      generationRendered.TemplateKey,
				"promptVersionId":        generationRendered.PromptVersionID,
				"promptHash":             correctionPromptHash,
			}),
		})
		if err != nil {
			return PrepareShotVideoPromptOutput{}, err
		}
		correctionRequest := generationRequest
		correctionRequest.NodeRunID = generationExecution.NodeRunID
		correctionRequest.PromptHash = correctionPromptHash
		correctionRequest.Input = mustJSON(map[string]any{
			"prompt":          correctionPrompt,
			"responseFormat":  "json",
			"maxOutputTokens": structuredVideoPromptOutputTokens,
			"temperature":     0.2,
		})
		draft, generationResponse, err = requestStructuredVideoPrompt(
			ctx,
			generationExecution,
			correctionRequest,
			a.generateProviderText,
			parseGeneratedVideoPrompt,
		)
		if err != nil {
			return PrepareShotVideoPromptOutput{}, fail(generationExecution, workflowErrorFromProvider(err, codeActivityFailed))
		}
		draft.DialogueLines = authoritativeVideoPromptDialogueLines(agentContext.Shot.ScriptDialogue)
		draft.Prompt = sanitizeVideoPromptVisualSafety(draft.Prompt, videoPromptShotAudioCues(agentContext.Shot))
		draft.Prompt, err = constrainVideoVisualPrompt(
			draft.Prompt, constraints.Candidates, agentContext.Shot, videoPromptShotAudioCues(agentContext.Shot),
		)
		if err != nil {
			return PrepareShotVideoPromptOutput{}, fail(generationExecution, err)
		}
		if err := CompleteNodeRun(ctx, a.db, generationExecution, mustJSON(map[string]any{
			"providerCallId": generationResponse.ProviderCallID,
			"modelId":        generationResponse.ModelID,
			"reviewRound":    correctionRound,
			"prompt":         draft.Prompt,
			"negativePrompt": draft.NegativePrompt,
			"sourceAnchors":  draft.SourceAnchors,
			"reviewIssues":   reviewed.Issues,
		})); err != nil {
			return PrepareShotVideoPromptOutput{}, err
		}
	}
	dialogueLines := SpokenStoryboardDialogue(agentContext.Shot.ScriptDialogue)
	soundCues := NonSpeechStoryboardAudioCues(agentContext.Shot.SoundCues)
	reviewedPrompt := firstNonEmptyString(reviewed.FinalPrompt, reviewed.Prompt)
	if err := validateVideoPromptDialogueScope(reviewedPrompt, agentContext); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	reviewedPrompt = sanitizeVideoPromptVisualSafety(reviewedPrompt, videoPromptShotAudioCues(agentContext.Shot))
	reviewedPrompt, err = constrainVideoVisualPrompt(
		reviewedPrompt, constraints.Candidates, agentContext.Shot, videoPromptShotAudioCues(agentContext.Shot),
	)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	reviewedPrompt, err = composeVideoExecutionContract(reviewedPrompt, agentContext.Shot)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	finalPrompt := composeAuthoritativeVideoPrompt(reviewedPrompt, append(append([]StoryboardDialogueLine(nil), dialogueLines...), soundCues...))
	if err := validateVideoPromptDialogue(finalPrompt, agentContext); err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, err)
	}
	audioStrategy := firstNonEmptyString(strings.ToLower(strings.TrimSpace(input.AudioStrategy)), project.AudioStrategy, "native_av")
	audioRequirement := firstNonEmptyString(strings.ToLower(strings.TrimSpace(input.AudioRequirement)), project.AudioRequirement, "preferred")
	nativeAudioRequired := audioStrategy == "native_av" && audioRequirement == "required"
	modelSupportsNativeAudio := videoCandidatesSupportNativeAudio(
		constraints.Candidates,
		project.VideoProductionRequiredInitialInputContract,
		len(dialogueLines) > 0,
	)
	reviewResult := videoproduction.ReviewVideoPrompt(
		project.VideoProductionProfileKey, finalPrompt, promptContextPlan.VerbatimDialogueCues,
		nativeAudioRequired, modelSupportsNativeAudio,
	)
	deterministicReview := &reviewResult
	if !reviewResult.Approved {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, workflowError{
			Code:    videoproduction.CodePromptContractIncomplete,
			Message: fmt.Sprintf("当前生产方案的视频提示词确定性审核失败：%v", reviewResult.Issues),
		})
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
		SoundCues:                soundCues,
		ReferencePackID:          referencePackID,
		ReferencePackHash:        referencePack.ManifestHash,
		CapabilitySnapshotHash:   referencePack.CapabilitySnapshotHash,
		PromptContextPlanID:      promptContextPlanID,
		PromptContextPlanHash:    promptContextPlanHash,
		NativeAudioRequired:      nativeAudioRequired,
		ModelSupportsNativeAudio: modelSupportsNativeAudio,
		GenerationContract:       generationContract,
		ReviewContract:           reviewContract,
		DeterministicReview:      deterministicReview,
	}
	videoPromptPlanID, err := a.persistReviewedShotVideoPrompt(ctx, input, shot, project, productionContract, reviewExecution, output, reviewed, audioStrategy, audioRequirement)
	if err != nil {
		return PrepareShotVideoPromptOutput{}, fail(reviewExecution, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	output.VideoPromptPlanID = videoPromptPlanID
	return output, nil
}

func authoritativeVideoPromptDialogueLines(lines []StoryboardDialogueLine) []StoryboardDialogueLine {
	return append([]StoryboardDialogueLine(nil), SpokenStoryboardDialogue(lines)...)
}

func videoCandidatesSupportNativeAudio(candidates []provider.GatewayModelConstraintCandidate, inputContract string, hasDialogue bool) bool {
	for _, candidate := range candidates {
		compatible := false
		switch strings.ToLower(strings.TrimSpace(inputContract)) {
		case videoproduction.InputContractFirstFrame:
			compatible = candidate.References.SupportsFirstFrame
		case videoproduction.InputContractFirstLastFrames:
			compatible = candidate.References.SupportsFirstFrame && candidate.References.SupportsLastFrame
		case videoproduction.InputContractFirstFramePlusReferences:
			compatible = candidate.References.SupportsFirstFrame && candidate.References.SupportsSemanticReferenceImages
		case videoproduction.InputContractStoryboardSheetReference:
			compatible = candidate.References.SupportsStoryboardSheetReference
		}
		if len(candidate.References.InputContracts) > 0 && !containsStringFold(candidate.References.InputContracts, inputContract) {
			compatible = false
		}
		if !compatible || candidate.NativeAudio.Support != provider.VideoSupportTrue {
			continue
		}
		if hasDialogue && !candidate.NativeAudio.SupportsDialogue {
			continue
		}
		return true
	}
	return false
}

func (a Activities) loadShotVideoPromptAgentContext(ctx context.Context, project ProjectProductionSettings, shot StoryboardShotRecord, assets ShotAssetContext, candidates []provider.GatewayModelConstraintCandidate, input PrepareShotVideoPromptInput) (shotVideoPromptAgentContext, error) {
	spokenDialogue := SpokenStoryboardDialogue(shot.Dialogue)
	soundCues := NonSpeechStoryboardAudioCues(shot.Dialogue)
	contextValue := shotVideoPromptAgentContext{
		Project: shotVideoPromptProject{
			ProjectType:               project.ProjectType,
			ContentType:               project.ContentType,
			VideoProductionProfileKey: project.VideoProductionProfileKey,
			ArtStyle:                  project.ArtStyle,
			AspectRatio:               firstNonEmptyString(input.AspectRatio, project.VideoRatio, project.AspectRatio, "16:9"),
			DirectorManual:            project.DirectorManual,
			VisualManual:              project.VisualManual,
		},
		Shot: shotVideoPromptShot{
			ShotID:            shot.ID,
			ShotNo:            shot.ShotNo,
			Duration:          videoPromptContextDuration(input, shot),
			AspectRatio:       firstNonEmptyString(input.AspectRatio, project.VideoRatio, project.AspectRatio, "16:9"),
			Resolution:        firstNonEmptyString(input.Resolution, "720p"),
			Title:             shot.Title,
			Visual:            shot.Visual,
			Camera:            shot.Camera,
			Motion:            shot.Motion,
			Mood:              shot.Mood,
			ExistingPrompt:    shot.VideoPrompt,
			ScriptDialogue:    append([]StoryboardDialogueLine(nil), spokenDialogue...),
			SoundCues:         append([]StoryboardDialogueLine(nil), soundCues...),
			ExecutionPlanID:   input.ExecutionPlanID,
			RenderSegmentID:   input.RenderSegmentID,
			SegmentIndex:      input.SegmentIndex,
			SegmentCount:      input.SegmentCount,
			SegmentStartTick:  input.SegmentStartTick,
			SegmentEndTick:    input.SegmentEndTick,
			RequestedDuration: input.RequestedDuration,
			TimelineTimebase:  firstPositiveInt64(project.TimelineTimebase, 90000),
			AudioStrategy:     firstNonEmptyString(strings.ToLower(strings.TrimSpace(input.AudioStrategy)), project.AudioStrategy, "native_av"),
			AudioRequirement:  firstNonEmptyString(strings.ToLower(strings.TrimSpace(input.AudioRequirement)), project.AudioRequirement, "preferred"),
		},
		Assets:        assets.PromptAssets,
		ReferenceMode: shot.VideoReferenceMode,
		ReferenceKeys: shot.VideoReferenceKeys,
		VideoModels:   videoPromptModelContexts(candidates),
	}
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		contextValue.Shot.ScriptDialogue = append([]StoryboardDialogueLine(nil), SpokenStoryboardDialogue(input.DialogueLines)...)
		contextValue.Shot.SoundCues = storyboardAudioCuesForSegment(
			soundCues,
			input.SegmentStartTick,
			input.SegmentEndTick,
		)
	}
	contextValue.Shot.DialogueTiming = videoPromptDialogueTiming(
		contextValue.Shot.ScriptDialogue,
		contextValue.Shot.TimelineTimebase,
	)
	contextValue.Shot.SoundCueTiming = videoPromptDialogueTiming(
		contextValue.Shot.SoundCues,
		contextValue.Shot.TimelineTimebase,
	)
	narrative, err := a.loadShotPromptNarrativeContext(ctx, project.ID, shot.ID)
	if err != nil {
		return shotVideoPromptAgentContext{}, err
	}
	contextValue.Source = narrative.Source
	contextValue.Script = narrative.Script
	contextValue.Scene = narrative.Scene
	return contextValue, nil
}

func videoPromptShotAudioCues(shot shotVideoPromptShot) []StoryboardDialogueLine {
	result := make([]StoryboardDialogueLine, 0, len(shot.ScriptDialogue)+len(shot.SoundCues))
	result = append(result, shot.ScriptDialogue...)
	result = append(result, shot.SoundCues...)
	return result
}

func storyboardAudioCuesForSegment(cues []StoryboardDialogueLine, segmentStart, segmentEnd int64) []StoryboardDialogueLine {
	if segmentEnd <= segmentStart {
		return nil
	}
	result := make([]StoryboardDialogueLine, 0, len(cues))
	for _, cue := range NonSpeechStoryboardAudioCues(cues) {
		originalStart, originalEnd := cue.SpanStartTick, cue.SpanEndTick
		start, end := originalStart, originalEnd
		if start < segmentStart {
			start = segmentStart
		}
		if end > segmentEnd {
			end = segmentEnd
		}
		if end <= start {
			continue
		}
		cue.SpanStartTick = start - segmentStart
		cue.SpanEndTick = end - segmentStart
		cue.ContinuesFromPrevious = cue.ContinuesFromPrevious || start > originalStart
		cue.ContinuesToNext = cue.ContinuesToNext || end < originalEnd
		result = append(result, cue)
	}
	return result
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
- During review, do not reject a candidate merely because its JSON prompt field omits dialogue text or the authoritative timeline marker. Verify the candidate against shot.scriptDialogue and shot.dialogueTiming; CineWeave injects the exact timeline after approval.
- When assigned audio affects performance, refer to the assigned authoritative audio timeline and the required lip-sync or off-screen delivery mode without reproducing its text.
- For a render segment, shot.requestedDuration is the provider request duration and is authoritative. Never replace it with the full-shot duration or invent a decimal duration unsupported by the selected model plan.
- shot.timelineTimebase is ticks per second. Convert ticks with seconds=ticks/timelineTimebase; never interpret raw ticks as milliseconds, microseconds, or decimal seconds. shot.dialogueTiming already contains the authoritative second values.
- The visual prompt must fit the strictest videoModels maxLength after reserving space for CineWeave's execution contract and authoritative audio timeline. Remove repetition before rejecting an otherwise executable candidate.
- Describe conflict and danger with non-graphic cinematic cues. Do not introduce graphic wounds, exposed anatomy, gore, dismemberment, or pools of blood when the same plot beat can be expressed through light, weather, posture, costume color, silhouettes, and reaction.
- Do not emit the cineweave_authoritative_audio_timeline marker yourself.
</runtime_audio_contract>`
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+deterministic_audio_v2"
	return rendered
}

func videoPromptContextDuration(input PrepareShotVideoPromptInput, shot StoryboardShotRecord) float64 {
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		return firstPositiveFloat(input.RequestedDuration, input.Duration, defaultShotDuration)
	}
	return math.Ceil(firstPositiveFloat(input.Duration, shot.Duration, defaultShotDuration))
}

func scopeVideoPromptContextPlanToSegment(
	plan videoproduction.PromptContextPlan,
	lines []StoryboardDialogueLine,
) (videoproduction.PromptContextPlan, error) {
	cues := make([]videoproduction.DialogueCue, 0, len(lines))
	for _, line := range NormalizeStoryboardDialogue(lines) {
		cues = append(cues, videoproduction.DialogueCue{
			TimingUnitID:          line.TimingUnitID,
			Speaker:               line.Speaker,
			Text:                  line.Text,
			Delivery:              line.Delivery,
			Kind:                  line.Kind,
			StartTick:             line.SpanStartTick,
			EndTick:               line.SpanEndTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious,
			ContinuesToNext:       line.ContinuesToNext,
		})
	}
	return videoproduction.CompilePromptContextPlan(videoproduction.PromptContextCompileInput{
		EpisodeScript:           plan.EpisodeContinuityDigest,
		EpisodeContinuityDigest: plan.EpisodeContinuityDigest,
		CurrentSceneScript:      plan.CurrentSceneScript,
		AdjacentSceneSummaries:  plan.AdjacentSceneSummaries,
		CurrentShotState:        plan.CurrentShotState,
		VerbatimDialogueCues:    cues,
		ModelContextLimit:       plan.ModelContextLimit,
		ModelPromptLimit:        plan.ModelPromptLimit,
	})
}

func videoPromptDialogueTiming(lines []StoryboardDialogueLine, timebase int64) []shotVideoDialogueTiming {
	if timebase <= 0 {
		timebase = 90000
	}
	result := make([]shotVideoDialogueTiming, 0, len(lines))
	for _, line := range NormalizeStoryboardDialogue(lines) {
		start := float64(line.SpanStartTick) / float64(timebase)
		end := float64(line.SpanEndTick) / float64(timebase)
		result = append(result, shotVideoDialogueTiming{
			TimingUnitID:    line.TimingUnitID,
			Speaker:         line.Speaker,
			Kind:            line.Kind,
			StartSeconds:    start,
			EndSeconds:      end,
			DurationSeconds: math.Max(0, end-start),
		})
	}
	return result
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func composeVideoExecutionContract(prompt string, shot shotVideoPromptShot) (string, error) {
	prompt = stripVideoExecutionContract(strings.TrimSpace(prompt))
	if strings.TrimSpace(shot.RenderSegmentID) == "" {
		return prompt, nil
	}
	if shot.RequestedDuration <= 0 || shot.SegmentEndTick <= shot.SegmentStartTick || shot.SegmentCount <= 0 || shot.SegmentIndex < 0 || shot.SegmentIndex >= shot.SegmentCount {
		return "", workflowError{Code: provider.CodeRenderPlanReplanRequired, Message: "视频执行片段缺少有效的整数秒时长或时间范围，请重新生成视频提示词"}
	}
	duration := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", shot.RequestedDuration), "0"), ".")
	contract := fmt.Sprintf(`%s
requestedDurationSeconds: %s
segmentIndex: %d
segmentCount: %d
plannedStartTick: %d
plannedEndTick: %d
Execute only this render segment and complete its action within the requested provider duration. Do not continue into another shot.
%s`, videoExecutionStartMarker, duration, shot.SegmentIndex, shot.SegmentCount, shot.SegmentStartTick, shot.SegmentEndTick, videoExecutionEndMarker)
	return strings.TrimSpace(prompt + "\n\n" + contract), nil
}

func stripVideoExecutionContract(prompt string) string {
	for {
		start := strings.Index(prompt, videoExecutionStartMarker)
		if start < 0 {
			return strings.TrimSpace(prompt)
		}
		endOffset := strings.Index(prompt[start+len(videoExecutionStartMarker):], videoExecutionEndMarker)
		if endOffset < 0 {
			return strings.TrimSpace(prompt[:start])
		}
		end := start + len(videoExecutionStartMarker) + endOffset + len(videoExecutionEndMarker)
		prompt = strings.TrimSpace(prompt[:start] + "\n" + prompt[end:])
	}
}

func sanitizeVideoPromptVisualSafety(prompt string, dialogue []StoryboardDialogueLine) string {
	prompt = stripAuthoritativeVideoPromptAudio(strings.TrimSpace(prompt))
	for _, line := range NormalizeStoryboardDialogue(dialogue) {
		if text := strings.TrimSpace(line.Text); text != "" {
			replacement := "按权威语音时间线逐字完成台词表演"
			if !isSpokenStoryboardDialogueKind(line.Kind) {
				replacement = "仅生成与画面同步的非语言环境音效，不得朗读音效描述或生成口型"
			}
			prompt = strings.ReplaceAll(prompt, text, replacement)
			if !isSpokenStoryboardDialogueKind(line.Kind) {
				if description := normalizeNonSpeechSoundDescription(text); description != text {
					prompt = strings.ReplaceAll(prompt, description, replacement)
				}
			}
		}
	}
	replacements := []struct{ old, new string }{
		{"血泊", "深色雨水反光"}, {"血池", "深色雨水反光"}, {"鲜血", "暗红色环境痕迹"},
		{"血液", "暗红色环境痕迹"}, {"血迹", "暗红色环境痕迹"}, {"血痕", "暗红色环境痕迹"},
		{"血衣", "深红色旧衣"}, {"血袍", "深红色旧袍"}, {"尸体", "倒地的人影"}, {"尸骸", "倒地的人影"},
		{"断肢", "散落的衣物与道具"}, {"肢解", "非图形化的冲突结果"}, {"开膛", "非图形化的重创"},
		{"gore", "non-graphic aftermath"}, {"pool of blood", "dark rain reflections"}, {"bloody", "dark red weathered"},
		{"dismembered", "fallen and obscured"}, {"exposed organs", "non-graphic injury"},
	}
	for _, replacement := range replacements {
		prompt = strings.ReplaceAll(prompt, replacement.old, replacement.new)
		prompt = strings.ReplaceAll(prompt, strings.ToUpper(replacement.old), replacement.new)
	}
	return strings.TrimSpace(prompt)
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
			if attempt < structuredVideoPromptAttempts && retryableStructuredVideoPromptProviderError(err) {
				continue
			}
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

func retryableStructuredVideoPromptProviderError(err error) bool {
	standard, ok := provider.StandardErrorFromError(err)
	return ok && standard.Retryable
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
	required := SpokenStoryboardDialogue(contextValue.Shot.ScriptDialogue)
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
	expectedSound := authoritativeVideoSoundCues(contextValue.Shot.SoundCues)
	if injected, found, err := extractAuthoritativeVideoPromptSound(prompt); err != nil {
		return workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()}
	} else if len(expectedSound) > 0 && !found {
		return workflowError{Code: provider.CodeInvalidRequest, Message: "video prompt omitted the non-speech sound timeline"}
	} else if found && !authoritativeVideoSoundCuesEquivalent(injected, expectedSound) {
		return workflowError{Code: provider.CodeInvalidRequest, Message: "video prompt non-speech sound timeline does not match the shot timing assignment"}
	}
	return validateVideoPromptDialogueScope(prompt, contextValue)
}

func validateVideoPromptDialogueScope(prompt string, contextValue shotVideoPromptAgentContext) error {
	required := SpokenStoryboardDialogue(contextValue.Shot.ScriptDialogue)
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
	spoken := SpokenStoryboardDialogue(dialogue)
	sounds := authoritativeVideoSoundCues(dialogue)
	spokenPayload, _ := json.Marshal(spoken)
	parts := []string{prompt}
	if len(spoken) > 0 {
		parts = append(parts,
			"Earlier speech or dialogue instructions are non-authoritative and must be ignored. Execute only the ordered JSON speech timeline below. Preserve every Chinese text value verbatim. kind=dialogue uses synchronized character speech; kind=voiceover or narration is off-screen speech. Never render speech as subtitles or visible text.",
			authoritativeSpeechStartMarker+"\n"+string(spokenPayload)+"\n"+authoritativeSpeechEndMarker,
		)
	} else {
		parts = append(parts, "This shot has no character dialogue, narration, or voiceover. Generate no human voice, no spoken words, and no lip sync.")
	}
	if len(sounds) > 0 {
		soundPayload, _ := json.Marshal(sounds)
		parts = append(parts,
			"The JSON below is non-speech sound-design metadata, never spoken language. Render only environmental, mechanical, Foley, or musical sound described by description. Never vocalize, narrate, quote, translate, or lip-sync any description value.",
			authoritativeSoundStartMarker+"\n"+string(soundPayload)+"\n"+authoritativeSoundEndMarker,
		)
	}
	return strings.TrimSpace(strings.Join(compactStrings(parts), "\n\n"))
}

func stripAuthoritativeVideoPromptAudio(prompt string) string {
	prompt = stripMarkedVideoPromptSection(prompt, legacyAuthoritativeAudioStartMarker, legacyAuthoritativeAudioEndMarker)
	prompt = stripMarkedVideoPromptSection(prompt, authoritativeSpeechStartMarker, authoritativeSpeechEndMarker)
	prompt = stripMarkedVideoPromptSection(prompt, authoritativeSoundStartMarker, authoritativeSoundEndMarker)
	lines := strings.Split(prompt, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Earlier audio or dialogue instructions are non-authoritative") ||
			strings.HasPrefix(trimmed, "Earlier speech or dialogue instructions are non-authoritative") ||
			strings.HasPrefix(trimmed, "The JSON below is non-speech sound-design metadata") ||
			strings.HasPrefix(trimmed, "This shot has no character dialogue, narration, or voiceover") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func extractAuthoritativeVideoPromptAudio(prompt string) ([]StoryboardDialogueLine, bool, error) {
	start := strings.LastIndex(prompt, authoritativeSpeechStartMarker)
	if start < 0 {
		return nil, false, nil
	}
	bodyStart := start + len(authoritativeSpeechStartMarker)
	endRelative := strings.Index(prompt[bodyStart:], authoritativeSpeechEndMarker)
	if endRelative < 0 {
		return nil, true, fmt.Errorf("video prompt authoritative speech timeline is not closed")
	}
	var lines []StoryboardDialogueLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(prompt[bodyStart:bodyStart+endRelative])), &lines); err != nil {
		return nil, true, fmt.Errorf("video prompt authoritative speech timeline is invalid: %w", err)
	}
	return SpokenStoryboardDialogue(lines), true, nil
}

type authoritativeVideoSoundCue struct {
	TimingUnitID          string `json:"timingUnitId,omitempty"`
	Kind                  string `json:"kind"`
	Description           string `json:"description"`
	StartTick             int64  `json:"startTick"`
	EndTick               int64  `json:"endTick"`
	ContinuesFromPrevious bool   `json:"continuesFromPrevious,omitempty"`
	ContinuesToNext       bool   `json:"continuesToNext,omitempty"`
}

func authoritativeVideoSoundCues(lines []StoryboardDialogueLine) []authoritativeVideoSoundCue {
	nonSpeech := NonSpeechStoryboardAudioCues(lines)
	result := make([]authoritativeVideoSoundCue, 0, len(nonSpeech))
	for _, line := range nonSpeech {
		description := normalizeNonSpeechSoundDescription(line.Text)
		if description == "" {
			continue
		}
		result = append(result, authoritativeVideoSoundCue{
			TimingUnitID: line.TimingUnitID, Kind: firstNonEmptyString(strings.ToLower(strings.TrimSpace(line.Kind)), "system"),
			Description: description, StartTick: line.SpanStartTick, EndTick: line.SpanEndTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious, ContinuesToNext: line.ContinuesToNext,
		})
	}
	return result
}

func normalizeNonSpeechSoundDescription(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"【音效：", "【环境音：", "【音乐：", "[音效：", "[环境音：", "[音乐："} {
		if strings.HasPrefix(value, prefix) {
			value = strings.TrimPrefix(value, prefix)
			value = strings.TrimSuffix(value, "】")
			value = strings.TrimSuffix(value, "]")
			break
		}
	}
	return strings.TrimSpace(value)
}

func extractAuthoritativeVideoPromptSound(prompt string) ([]authoritativeVideoSoundCue, bool, error) {
	start := strings.LastIndex(prompt, authoritativeSoundStartMarker)
	if start < 0 {
		return nil, false, nil
	}
	bodyStart := start + len(authoritativeSoundStartMarker)
	endRelative := strings.Index(prompt[bodyStart:], authoritativeSoundEndMarker)
	if endRelative < 0 {
		return nil, true, fmt.Errorf("video prompt non-speech sound timeline is not closed")
	}
	var cues []authoritativeVideoSoundCue
	if err := json.Unmarshal([]byte(strings.TrimSpace(prompt[bodyStart:bodyStart+endRelative])), &cues); err != nil {
		return nil, true, fmt.Errorf("video prompt non-speech sound timeline is invalid: %w", err)
	}
	return cues, true, nil
}

func authoritativeVideoSoundCuesEquivalent(left, right []authoritativeVideoSoundCue) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func stripMarkedVideoPromptSection(prompt, startMarker, endMarker string) string {
	for {
		start := strings.Index(prompt, startMarker)
		if start < 0 {
			return strings.TrimSpace(prompt)
		}
		endRelative := strings.Index(prompt[start+len(startMarker):], endMarker)
		if endRelative < 0 {
			return strings.TrimSpace(prompt[:start])
		}
		end := start + len(startMarker) + endRelative + len(endMarker)
		prompt = strings.TrimSpace(prompt[:start] + "\n" + prompt[end:])
	}
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

func constrainVideoVisualPrompt(
	prompt string,
	candidates []provider.GatewayModelConstraintCandidate,
	shot shotVideoPromptShot,
	dialogue []StoryboardDialogueLine,
) (string, error) {
	prompt = stripAuthoritativeVideoPromptAudio(stripVideoExecutionContract(strings.TrimSpace(prompt)))
	reserved, err := composeVideoExecutionContract("", shot)
	if err != nil {
		return "", err
	}
	reserved = composeAuthoritativeVideoPrompt(reserved, dialogue)
	for _, candidate := range candidates {
		if candidate.Prompt.MaxLength <= 0 {
			continue
		}
		unit := candidate.Prompt.Unit
		if unit == "" {
			unit = provider.PromptLengthUnitCharacters
		}
		budget := candidate.Prompt.MaxLength - provider.MeasurePromptLength(reserved, unit) - 64
		if budget < 256 {
			return "", workflowError{
				Code:    provider.CodeInvalidRequest,
				Message: fmt.Sprintf("视频模型 %s 的提示词上限无法容纳执行契约和权威音频时间线", candidate.ModelKey),
			}
		}
		prompt = truncateVideoPromptToLimit(prompt, budget, unit)
	}
	if strings.TrimSpace(prompt) == "" {
		return "", workflowError{Code: provider.CodeInvalidRequest, Message: "视频视觉提示词在应用模型长度预算后为空"}
	}
	return strings.TrimSpace(prompt), nil
}

func truncateVideoPromptToLimit(prompt string, limit int, unit string) string {
	prompt = strings.TrimSpace(prompt)
	if limit <= 0 || provider.MeasurePromptLength(prompt, unit) <= limit {
		return prompt
	}
	runes := []rune(prompt)
	end := 0
	used := 0
	for index, char := range runes {
		width := 1
		if unit == provider.PromptLengthUnitUTF8Bytes {
			width = utf8.RuneLen(char)
		}
		if used+width > limit-3 {
			break
		}
		used += width
		end = index + 1
	}
	if end <= 0 {
		return ""
	}
	minimumBoundary := end * 2 / 3
	for index := end - 1; index >= minimumBoundary; index-- {
		if strings.ContainsRune("。！？；.!?;\n", runes[index]) {
			end = index + 1
			break
		}
	}
	return strings.TrimSpace(string(runes[:end])) + "。"
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

func (a Activities) persistReviewedShotVideoPrompt(
	ctx context.Context,
	input PrepareShotVideoPromptInput,
	shot StoryboardShotRecord,
	project ProjectProductionSettings,
	contract shotProductionContractContext,
	nodeExecution NodeExecution,
	output PrepareShotVideoPromptOutput,
	review reviewedVideoPrompt,
	audioStrategy string,
	audioRequirement string,
) (string, error) {
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		return "", a.persistReviewedRenderSegmentPrompt(ctx, input, shot, nodeExecution, output, review)
	}
	dialogueBackfilled := len(shot.Dialogue) == 0 && len(output.DialogueLines) > 0
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	runContext, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution)
	if err != nil {
		return "", err
	}
	videoPromptPlanID := ""
	if runContext.ProductionGenerationID != project.ProductionGenerationID || runContext.VideoProductionBindingID != project.VideoProductionBindingID || runContext.VideoProductionBindingRevision != project.VideoProductionBindingRevision {
		return "", ErrWorkflowWriteFenced
	}
	videoPromptPlanID, err = a.persistSingleFrameVideoPromptPlanTx(
		ctx, tx, input, shot, project, contract, nodeExecution, output, review,
		audioStrategy, audioRequirement,
	)
	if err != nil {
		return "", err
	}
	output.VideoPromptPlanID = videoPromptPlanID
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
			"nonSpeechSoundCues":        output.SoundCues,
			"issues":                    review.Issues,
			"changes":                   review.Changes,
			"dialogueBackfilled":        dialogueBackfilled,
			"referencePackId":           output.ReferencePackID,
			"referencePackHash":         output.ReferencePackHash,
			"capabilitySnapshotHash":    output.CapabilitySnapshotHash,
			"promptContextPlanId":       output.PromptContextPlanID,
			"promptContextPlanHash":     output.PromptContextPlanHash,
			"videoPromptPlanId":         output.VideoPromptPlanID,
			"nativeAudioRequired":       output.NativeAudioRequired,
			"modelSupportsNativeAudio":  output.ModelSupportsNativeAudio,
			"generationContract":        output.GenerationContract,
			"reviewContract":            output.ReviewContract,
			"deterministicReview":       output.DeterministicReview,
		},
	})
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
		return "", err
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
		"nonSpeechSoundCues":       output.SoundCues,
		"dialogueBackfilled":       dialogueBackfilled,
		"videoPromptPlanId":        videoPromptPlanID,
		"promptContextPlanId":      output.PromptContextPlanID,
		"referencePackId":          output.ReferencePackID,
		"nativeAudioRequired":      output.NativeAudioRequired,
	})); err != nil {
		return "", err
	}
	if _, err := completeNodeRunTx(ctx, tx, nodeExecution, mustJSON(output)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return videoPromptPlanID, nil
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
		SET prompt = $4, execution_prompt_hash = $5, dialogue = $6::jsonb, error_code = NULL, error_message = NULL,
		    metadata = (metadata || $7::jsonb) || jsonb_build_object('promptStatus', 'succeeded', 'promptCompletedAt', now()),
		    updated_at = now()
		WHERE id = $1 AND video_render_plan_id = $2 AND project_id = $3
	`, input.RenderSegmentID, input.ExecutionPlanID, input.ProjectID, output.Prompt, output.PromptHash,
		mustJSON(storyboardDialogueToGatewaySpans(output.DialogueLines)), metadata)
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

func videoPromptReviewRoundNodeKey(base string, round int) string {
	if round <= 1 {
		return base
	}
	return fmt.Sprintf("%s_review_round_%d", base, round)
}

func composeVideoPromptReviewCorrection(basePrompt string, draft generatedVideoPrompt, review reviewedVideoPrompt, round int) string {
	feedback := mustJSON(map[string]any{
		"round":             round,
		"previousCandidate": draft,
		"reviewIssues":      review.Issues,
		"requiredChanges":   review.Changes,
	})
	return strings.TrimSpace(basePrompt) + `

<cineweave_video_prompt_review_correction>
上一版候选未通过审核。必须逐项修正 reviewIssues 和 requiredChanges，不得原样返回上一版。
保持已给出的结构化镜头状态、逐字中文台词、参考图约束、生产方案和视频模型能力不变。
返回完整的替换版 JSON 对象，不要解释，不要输出 Markdown。
` + string(feedback) + `
</cineweave_video_prompt_review_correction>`
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
		_, err = restoreApprovedVideoPromptStateTx(persistCtx, tx, shot.ID, input.WorkflowRunID)
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
	if _, err := restoreApprovedVideoPromptStateTx(ctx, tx, shot.ID, input.WorkflowRunID); err != nil {
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
