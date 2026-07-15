package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/assetprompts"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeAnalyzeScriptAssetsKey       = "analyze_script_assets"
	nodePrepareStoryboardEpisodesKey = "prepare_storyboard_episodes"
	nodeGenerateStoryboardFromScript = "generate_storyboard_from_script"
	nodeGenerateCanonicalAssetPrefix = "generate_canonical_asset"
	nodeGenerateDerivedAssetPrefix   = "generate_derived_asset"
	promptKeyScriptAssetExtraction   = "script_asset_extraction"
	promptKeyCanonicalAssetImage     = "canonical_asset_image_prompt"
	promptKeyStoryboardFromScript    = "storyboard_from_script"
	promptKeyDerivedAssetImage       = "derived_asset_image_prompt"
	promptKeyShotImage               = "shot_image_prompt"
)

type ScriptProductionOptions struct {
	ScriptID              string   `json:"scriptId"`
	ScriptSceneID         string   `json:"scriptSceneId,omitempty"`
	ScriptEpisodeIDs      []string `json:"scriptEpisodeIds,omitempty"`
	MergeExisting         bool     `json:"mergeExisting"`
	GenerateImages        bool     `json:"generateImages"`
	GenerateDerivedAssets bool     `json:"generateDerivedAssets"`
	PacingProfile         string   `json:"pacingProfile,omitempty"`
	TargetDurationSeconds *float64 `json:"targetDurationSeconds,omitempty"`
	AudioStrategy         string   `json:"audioStrategy,omitempty"`
	AudioRequirement      string   `json:"audioRequirement,omitempty"`
	PlannerBatchMaxShots  int      `json:"plannerBatchMaxShots,omitempty"`
	MaxSceneConcurrency   int      `json:"maxSceneConcurrency,omitempty"`
	ShotBudget            int      `json:"shotBudget,omitempty"`
	Force                 bool     `json:"force,omitempty"`
	MaxShots              int      `json:"maxShots,omitempty"`
}

type ScriptRecord struct {
	ID            string `json:"scriptId"`
	VersionID     string `json:"versionId"`
	Version       int    `json:"version"`
	Content       string `json:"content"`
	ContentFormat string `json:"contentFormat"`
	Title         string `json:"title"`
}

type ProjectProductionSettings struct {
	ID                    string `json:"id"`
	ProjectType           string `json:"projectType"`
	ContentType           string `json:"contentType"`
	AspectRatio           string `json:"aspectRatio"`
	VideoRatio            string `json:"videoRatio"`
	ArtStyle              string `json:"artStyle"`
	DirectorManual        string `json:"directorManual"`
	VisualManual          string `json:"visualManual"`
	ImageModelProfileKey  string `json:"imageModelProfileKey"`
	VideoModelProfileKey  string `json:"videoModelProfileKey"`
	ScriptModelProfileKey string `json:"scriptModelProfileKey"`
	TTSModelProfileKey    string `json:"ttsModelProfileKey"`
	ASRModelProfileKey    string `json:"asrModelProfileKey"`
	AudioStrategy         string `json:"audioStrategy"`
	AudioRequirement      string `json:"audioRequirement"`
	ImageQuality          string `json:"imageQuality"`
	ProductionMode        string `json:"productionMode"`
	TimelineTimebase      int64  `json:"timelineTimebase"`
	FPSNumerator          int    `json:"fpsNumerator"`
	FPSDenominator        int    `json:"fpsDenominator"`
}

type ScriptAssetCandidate struct {
	AssetType    string          `json:"assetType"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	BasePrompt   string          `json:"basePrompt,omitempty"`
	VisualTraits json.RawMessage `json:"visualTraits,omitempty"`
}

type CanonicalAssetRecord struct {
	ID                          string          `json:"id"`
	AssetType                   string          `json:"assetType"`
	Name                        string          `json:"name"`
	Description                 string          `json:"description"`
	BasePrompt                  string          `json:"basePrompt,omitempty"`
	Profile                     json.RawMessage `json:"profile,omitempty"`
	ConsistencyPrompt           string          `json:"consistencyPrompt,omitempty"`
	NegativePrompt              string          `json:"negativePrompt,omitempty"`
	VisualTraits                json.RawMessage `json:"visualTraits,omitempty"`
	PrimaryReferenceArtifactID  string          `json:"primaryReferenceArtifactId,omitempty"`
	PrimaryReferenceMediaFileID string          `json:"primaryReferenceMediaFileId,omitempty"`
	PrimaryReferenceStorageKey  string          `json:"primaryReferenceStorageKey,omitempty"`
	LockReference               bool            `json:"lockReference,omitempty"`
	ReferenceArtifactID         string          `json:"referenceArtifactId,omitempty"`
	ReferenceMediaFileID        string          `json:"referenceMediaFileId,omitempty"`
	ReferenceStorageKey         string          `json:"referenceStorageKey,omitempty"`
	Status                      string          `json:"status"`
	ManualOverride              bool            `json:"manualOverride,omitempty"`
	StaleState                  string          `json:"staleState,omitempty"`
	Revision                    int64           `json:"revision"`
	PromptRevision              int64           `json:"promptRevision"`
}

type AnalyzeScriptAssetsInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`
	ScriptID       string `json:"scriptId"`
	MergeExisting  bool   `json:"mergeExisting"`
}

type ScriptAssetsOutput struct {
	ScriptID        string                 `json:"scriptId"`
	ScriptVersionID string                 `json:"scriptVersionId"`
	Assets          []CanonicalAssetRecord `json:"assets"`
	ProviderCallID  string                 `json:"providerCallId,omitempty"`
	ModelID         string                 `json:"modelId,omitempty"`
}

type GenerateStoryboardFromScriptInput struct {
	OrganizationID  string `json:"organizationId"`
	ProjectID       string `json:"projectId"`
	WorkflowRunID   string `json:"workflowRunId"`
	CreatedBy       string `json:"createdBy"`
	ScriptID        string `json:"scriptId"`
	ScriptSceneID   string `json:"scriptSceneId,omitempty"`
	ScriptEpisodeID string `json:"scriptEpisodeId,omitempty"`
	EpisodeIndex    int    `json:"episodeIndex,omitempty"`
	EpisodeTotal    int    `json:"episodeTotal,omitempty"`
	EpisodeTitle    string `json:"episodeTitle,omitempty"`
	MaxShots        int    `json:"maxShots,omitempty"`
}

type PrepareScriptStoryboardInput struct {
	OrganizationID   string   `json:"organizationId"`
	ProjectID        string   `json:"projectId"`
	WorkflowRunID    string   `json:"workflowRunId"`
	CreatedBy        string   `json:"createdBy"`
	ScriptID         string   `json:"scriptId"`
	ScriptEpisodeIDs []string `json:"scriptEpisodeIds,omitempty"`
}

type ScriptStoryboardEpisodeRef struct {
	ID           string `json:"id"`
	EpisodeIndex int    `json:"episodeIndex"`
	EpisodeTitle string `json:"episodeTitle"`
}

type ScriptStoryboardEpisodeRecord struct {
	ID           string `json:"id"`
	EpisodeIndex int    `json:"episodeIndex"`
	EpisodeTitle string `json:"episodeTitle"`
	Content      string `json:"content"`
}

type ScriptStoryboardPlan struct {
	ScriptID         string                       `json:"scriptId"`
	ScriptVersionID  string                       `json:"scriptVersionId"`
	EpisodeTotal     int                          `json:"episodeTotal"`
	TimelineTimebase int64                        `json:"timelineTimebase"`
	FPSNumerator     int                          `json:"fpsNumerator"`
	FPSDenominator   int                          `json:"fpsDenominator"`
	Episodes         []ScriptStoryboardEpisodeRef `json:"episodes"`
}

type ScriptStoryboardEpisodeResult struct {
	ScriptEpisodeID      string                    `json:"scriptEpisodeId"`
	EpisodeIndex         int                       `json:"episodeIndex"`
	EpisodeTitle         string                    `json:"episodeTitle"`
	StoryboardArtifactID string                    `json:"storyboardArtifactId"`
	StorageKey           string                    `json:"storageKey"`
	ProviderCallID       string                    `json:"providerCallId,omitempty"`
	ModelID              string                    `json:"modelId,omitempty"`
	Shots                []StoryboardShotRecord    `json:"shots"`
	DurationMetrics      StoryboardDurationMetrics `json:"durationMetrics"`
}

type StoryboardDurationMetrics struct {
	RawShotCount           int     `json:"rawShotCount"`
	PlannedShotCount       int     `json:"plannedShotCount"`
	StoredShotCount        int     `json:"storedShotCount,omitempty"`
	RawDurationSeconds     float64 `json:"rawDurationSeconds"`
	PlannedDurationSeconds float64 `json:"plannedDurationSeconds"`
	StoredDurationSeconds  float64 `json:"storedDurationSeconds,omitempty"`
	DurationLossSeconds    float64 `json:"durationLossSeconds,omitempty"`
}

type ScriptStoryboardOutput struct {
	ScriptID             string                          `json:"scriptId"`
	ScriptVersionID      string                          `json:"scriptVersionId"`
	ScriptEpisodeID      string                          `json:"scriptEpisodeId,omitempty"`
	EpisodeIndex         int                             `json:"episodeIndex,omitempty"`
	EpisodeTotal         int                             `json:"episodeTotal,omitempty"`
	EpisodeTitle         string                          `json:"episodeTitle,omitempty"`
	EpisodeCount         int                             `json:"episodeCount,omitempty"`
	Episodes             []ScriptStoryboardEpisodeResult `json:"episodes,omitempty"`
	StoryboardArtifactID string                          `json:"storyboardArtifactId"`
	StorageKey           string                          `json:"storageKey"`
	ProviderCallID       string                          `json:"providerCallId,omitempty"`
	ProviderCallIDs      []string                        `json:"providerCallIds,omitempty"`
	ModelID              string                          `json:"modelId,omitempty"`
	Storyboard           json.RawMessage                 `json:"storyboard"`
	Shots                []StoryboardShotRecord          `json:"shots"`
	Requirements         []ShotAssetRequirementRecord    `json:"requirements"`
	RawText              string                          `json:"rawText,omitempty"`
	ParseError           string                          `json:"parseError,omitempty"`
	DurationMetrics      StoryboardDurationMetrics       `json:"durationMetrics"`
}

type ShotAssetRequirementRecord struct {
	ID                 string `json:"id,omitempty"`
	ShotNo             int    `json:"shotNo,omitempty"`
	StoryboardShotID   string `json:"storyboardShotId,omitempty"`
	AssetID            string `json:"assetId,omitempty"`
	AssetType          string `json:"assetType,omitempty"`
	AssetName          string `json:"assetName,omitempty"`
	RequirementType    string `json:"requirementType"`
	RoleInShot         string `json:"roleInShot,omitempty"`
	Costume            string `json:"costume,omitempty"`
	Pose               string `json:"pose,omitempty"`
	Expression         string `json:"expression,omitempty"`
	Action             string `json:"action,omitempty"`
	CameraRelation     string `json:"cameraRelation,omitempty"`
	SceneState         string `json:"sceneState,omitempty"`
	PropState          string `json:"propState,omitempty"`
	Prompt             string `json:"prompt,omitempty"`
	DerivedArtifactID  string `json:"derivedArtifactId,omitempty"`
	DerivedMediaFileID string `json:"derivedMediaFileId,omitempty"`
	DerivedStorageKey  string `json:"derivedStorageKey,omitempty"`
	Status             string `json:"status,omitempty"`
	ManualOverride     bool   `json:"manualOverride,omitempty"`
	StaleState         string `json:"staleState,omitempty"`
}

func ScriptToAssetsWorkflow(ctx workflow.Context, input TextToStoryboardInput) (ScriptAssetsOutput, error) {
	options := resolveScriptProductionOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	var output ScriptAssetsOutput
	if err := workflow.ExecuteActivity(ctx, "AnalyzeScriptAssets", AnalyzeScriptAssetsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ScriptID:       options.ScriptID,
		MergeExisting:  options.MergeExisting,
	}).Get(ctx, &output); err != nil {
		return ScriptAssetsOutput{}, err
	}
	if options.GenerateImages {
		for _, asset := range output.Assets {
			if asset.ReferenceArtifactID != "" {
				continue
			}
			var imageOutput GenerateCanonicalAssetImageOutput
			if err := workflow.ExecuteActivity(ctx, "GenerateCanonicalAssetImage", GenerateCanonicalAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				AssetID:        asset.ID,
			}).Get(ctx, &imageOutput); err != nil {
				return ScriptAssetsOutput{}, err
			}
		}
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteScriptAssetsWorkflow", input, output).Get(ctx, nil); err != nil {
		return ScriptAssetsOutput{}, err
	}
	return output, nil
}

func ScriptToStoryboardWorkflow(ctx workflow.Context, input TextToStoryboardInput) (ScriptStoryboardOutput, error) {
	options := resolveScriptProductionOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	imageCtx := workflow.WithActivityOptions(ctx, providerImageActivityOptions())
	output, err := generateScriptStoryboardEpisodes(ctx, input, options)
	if err != nil {
		recordScriptStoryboardWorkflowFailure(ctx, input, err)
		return ScriptStoryboardOutput{}, err
	}
	if options.GenerateDerivedAssets {
		for _, requirement := range output.Requirements {
			var derived GenerateDerivedAssetImageOutput
			if err := workflow.ExecuteActivity(imageCtx, "GenerateDerivedAssetImage", GenerateDerivedAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				RequirementID:  requirement.ID,
			}).Get(imageCtx, &derived); err != nil {
				recordScriptStoryboardWorkflowFailure(ctx, input, err)
				return ScriptStoryboardOutput{}, err
			}
		}
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteScriptStoryboardWorkflow", input, output).Get(ctx, nil); err != nil {
		recordScriptStoryboardWorkflowFailure(ctx, input, err)
		return ScriptStoryboardOutput{}, err
	}
	return output, nil
}

func generateScriptStoryboardEpisodes(ctx workflow.Context, input TextToStoryboardInput, options ScriptProductionOptions) (ScriptStoryboardOutput, error) {
	episodeCtx := workflow.WithActivityOptions(ctx, storyboardEpisodeGenerationActivityOptions())
	if strings.TrimSpace(options.ScriptSceneID) != "" {
		var output ScriptStoryboardOutput
		if err := workflow.ExecuteActivity(episodeCtx, "GenerateStoryboardFromScript", GenerateStoryboardFromScriptInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ScriptID:       options.ScriptID,
			ScriptSceneID:  options.ScriptSceneID,
			MaxShots:       options.MaxShots,
		}).Get(ctx, &output); err != nil {
			return ScriptStoryboardOutput{}, err
		}
		return output, nil
	}

	prepareCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var plan ScriptStoryboardPlan
	if err := workflow.ExecuteActivity(prepareCtx, "PrepareScriptStoryboard", PrepareScriptStoryboardInput{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		WorkflowRunID:    input.WorkflowRunID,
		CreatedBy:        input.CreatedBy,
		ScriptID:         options.ScriptID,
		ScriptEpisodeIDs: options.ScriptEpisodeIDs,
	}).Get(ctx, &plan); err != nil {
		return ScriptStoryboardOutput{}, err
	}

	output := ScriptStoryboardOutput{
		ScriptID:        plan.ScriptID,
		ScriptVersionID: plan.ScriptVersionID,
		EpisodeCount:    len(plan.Episodes),
		EpisodeTotal:    len(plan.Episodes),
		Episodes:        make([]ScriptStoryboardEpisodeResult, 0, len(plan.Episodes)),
	}
	providerCallIDs := make([]string, 0, len(plan.Episodes))
	for _, episode := range plan.Episodes {
		var episodeOutput ScriptStoryboardOutput
		episodeCtx := workflow.WithChildOptions(ctx, storyboardEpisodeChildWorkflowOptions(ctx, episode))
		if err := workflow.ExecuteChildWorkflow(episodeCtx, ScriptEpisodeToStoryboardWorkflow, ScriptEpisodeToStoryboardInput{
			OrganizationID:    input.OrganizationID,
			ProjectID:         input.ProjectID,
			WorkflowRunID:     input.WorkflowRunID,
			CreatedBy:         input.CreatedBy,
			ScriptID:          plan.ScriptID,
			ScriptVersionID:   plan.ScriptVersionID,
			ScriptEpisodeID:   episode.ID,
			EpisodeIndex:      episode.EpisodeIndex,
			EpisodeTotal:      len(plan.Episodes),
			EpisodeTitle:      episode.EpisodeTitle,
			TimelineTimebase:  plan.TimelineTimebase,
			FPSNumerator:      plan.FPSNumerator,
			FPSDenominator:    plan.FPSDenominator,
			ProductionOptions: options,
		}).Get(ctx, &episodeOutput); err != nil {
			return ScriptStoryboardOutput{}, err
		}
		if output.StoryboardArtifactID == "" {
			output.StoryboardArtifactID = episodeOutput.StoryboardArtifactID
			output.StorageKey = episodeOutput.StorageKey
			output.ProviderCallID = episodeOutput.ProviderCallID
			output.ModelID = episodeOutput.ModelID
		}
		for _, providerCallID := range episodeOutput.ProviderCallIDs {
			providerCallIDs = appendProviderCallID(providerCallIDs, providerCallID)
		}
		if len(episodeOutput.ProviderCallIDs) == 0 {
			providerCallIDs = appendProviderCallID(providerCallIDs, episodeOutput.ProviderCallID)
		}
		output.Shots = append(output.Shots, episodeOutput.Shots...)
		output.Requirements = append(output.Requirements, episodeOutput.Requirements...)
		mergeStoryboardDurationMetrics(&output.DurationMetrics, episodeOutput.DurationMetrics)
		output.Episodes = append(output.Episodes, ScriptStoryboardEpisodeResult{
			ScriptEpisodeID:      episode.ID,
			EpisodeIndex:         episode.EpisodeIndex,
			EpisodeTitle:         episode.EpisodeTitle,
			StoryboardArtifactID: episodeOutput.StoryboardArtifactID,
			StorageKey:           episodeOutput.StorageKey,
			ProviderCallID:       episodeOutput.ProviderCallID,
			ModelID:              episodeOutput.ModelID,
			Shots:                episodeOutput.Shots,
			DurationMetrics:      episodeOutput.DurationMetrics,
		})
	}
	output.ProviderCallIDs = providerCallIDs
	return output, nil
}

func ScriptDrivenVideoProduction(ctx workflow.Context, input TextToStoryboardInput, options videoProductionOptions, scriptOptions ScriptProductionOptions) (VideoProductionOutput, error) {
	if scriptOptions.MaxShots <= 0 {
		scriptOptions.MaxShots = options.MaxShots
	}
	ctx = workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	imageCtx := workflow.WithActivityOptions(ctx, providerImageActivityOptions())
	var assets ScriptAssetsOutput
	if err := workflow.ExecuteActivity(ctx, "AnalyzeScriptAssets", AnalyzeScriptAssetsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ScriptID:       scriptOptions.ScriptID,
		MergeExisting:  true,
	}).Get(ctx, &assets); err != nil {
		return VideoProductionOutput{}, err
	}
	if scriptOptions.GenerateImages {
		for _, asset := range assets.Assets {
			if asset.ReferenceArtifactID != "" {
				continue
			}
			var imageOutput GenerateCanonicalAssetImageOutput
			if err := workflow.ExecuteActivity(imageCtx, "GenerateCanonicalAssetImage", GenerateCanonicalAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				AssetID:        asset.ID,
			}).Get(imageCtx, &imageOutput); err != nil {
				return VideoProductionOutput{}, err
			}
		}
	}
	storyboardOptions := scriptOptions
	storyboardOptions.MaxShots = options.MaxShots
	storyboard, err := generateScriptStoryboardEpisodes(ctx, input, storyboardOptions)
	if err != nil {
		return VideoProductionOutput{}, err
	}
	if scriptOptions.GenerateDerivedAssets {
		for _, requirement := range storyboard.Requirements {
			var derived GenerateDerivedAssetImageOutput
			if err := workflow.ExecuteActivity(imageCtx, "GenerateDerivedAssetImage", GenerateDerivedAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				RequirementID:  requirement.ID,
			}).Get(imageCtx, &derived); err != nil {
				return VideoProductionOutput{}, err
			}
		}
	}
	var shots []StoryboardShotRecord
	if err := workflow.ExecuteActivity(ctx, "ListStoryboardShots", ListStoryboardShotsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
	}).Get(ctx, &shots); err != nil {
		return VideoProductionOutput{}, err
	}
	if workflow.GetVersion(ctx, "script-driven-video-shot-image-prompts-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		if err := prepareShotImagePromptsForProduction(ctx, input, shots, options.AspectRatio, options.MaxImageConcurrency); err != nil {
			return VideoProductionOutput{}, err
		}
	}
	imageResults := make([]shotImageGenerationResult, len(shots))
	imageConcurrencyVersion := workflow.GetVersion(ctx, "script-driven-video-shot-image-concurrency-v1", workflow.DefaultVersion, 1)
	if imageConcurrencyVersion != workflow.DefaultVersion {
		imageRequests := make([]shotImageGenerationRequest, 0, len(shots))
		for _, shot := range shots {
			imageRequests = append(imageRequests, shotImageGenerationRequest{
				ShotID:         shot.ID,
				ShotIndex:      shot.ShotIndex,
				ShotNo:         shot.ShotNo,
				WorkflowPrompt: firstNonEmptyString(input.Prompt, "script_to_video"),
				AspectRatio:    options.AspectRatio,
			})
		}
		var err error
		imageResults, err = generateShotImagesConcurrently(ctx, imageCtx, input, imageRequests, options.MaxImageConcurrency)
		if err != nil {
			return VideoProductionOutput{}, err
		}
		for _, imageResult := range imageResults {
			if imageResult.Err != nil {
				return VideoProductionOutput{}, imageResult.Err
			}
		}
	}
	providerCalls := VideoProductionProviderCalls{
		Storyboard: storyboard.ProviderCallID,
	}
	imagesByShotID := make(map[string]GenerateShotImageOutput, len(shots))
	for index, shot := range shots {
		var image GenerateShotImageOutput
		if imageConcurrencyVersion == workflow.DefaultVersion {
			if err := workflow.ExecuteActivity(imageCtx, "GenerateShotImage", GenerateShotImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				ShotID:         shot.ID,
				ShotIndex:      shot.ShotIndex,
				ShotNo:         shot.ShotNo,
				WorkflowPrompt: firstNonEmptyString(input.Prompt, "script_to_video"),
				AspectRatio:    options.AspectRatio,
			}).Get(imageCtx, &image); err != nil {
				return VideoProductionOutput{}, err
			}
		} else {
			image = imageResults[index].Output
		}
		if image.ProviderCallID != "" {
			providerCalls.Images = append(providerCalls.Images, image.ProviderCallID)
		}
		imagesByShotID[shot.ID] = image
	}
	videoBatch, err := runShotVideoBatchChild(ctx, input, shots, options.AspectRatio, options.Resolution, scriptOptions.AudioStrategy, scriptOptions.AudioRequirement, options.MaxPolls, options.PollInterval)
	if err != nil {
		return VideoProductionOutput{}, err
	}
	providerCalls.VideoCreates = append(providerCalls.VideoCreates, videoBatch.VideoCreateProviderCallIDs...)
	providerCalls.VideoPolls = append(providerCalls.VideoPolls, videoBatch.VideoPollProviderCallIDs...)
	shotOutputs := videoProductionShotOutputs(shots, imagesByShotID, videoBatch, options.Duration)
	output := VideoProductionOutput{
		Status:               videoBatch.Status,
		SucceededShotIDs:     videoBatch.SucceededShotIDs,
		FailedShotIDs:        videoBatch.FailedShotIDs,
		Errors:               videoBatch.Errors,
		StoryboardArtifactID: storyboard.StoryboardArtifactID,
		Shots:                shotOutputs,
		ProviderCalls:        providerCalls,
	}
	if len(shotOutputs) > 0 {
		first := shotOutputs[0]
		output.ImageArtifactID = first.ImageArtifactID
		output.ImageMediaFileID = first.ImageMediaFileID
		output.ImageStorageKey = first.ImageStorageKey
		output.VideoArtifactID = first.VideoArtifactID
		output.VideoMediaFileID = first.VideoMediaFileID
		output.VideoStorageKey = first.VideoStorageKey
		output.ProviderAsyncTaskID = first.ProviderAsyncTaskID
		output.ExternalTaskID = first.ExternalTaskID
	}
	if !options.SkipCompose {
		composeOptions := defaultActivityOptions()
		composeOptions.TaskQueue = MediaTaskQueue
		composeOptions.StartToCloseTimeout = 30 * time.Minute
		composeCtx := workflow.WithActivityOptions(ctx, composeOptions)
		var composeOutput ComposeFinalVideoOutput
		if err := workflow.ExecuteActivity(composeCtx, "ComposeFinalVideo", ComposeFinalVideoInput{
			OrganizationID:    input.OrganizationID,
			ProjectID:         input.ProjectID,
			WorkflowRunID:     input.WorkflowRunID,
			CreatedBy:         input.CreatedBy,
			AspectRatio:       options.AspectRatio,
			Resolution:        options.Resolution,
			ProductionPartial: videoBatch.Status == "partial_succeeded",
		}).Get(composeCtx, &composeOutput); err != nil {
			return VideoProductionOutput{}, err
		}
		output.FinalVideoArtifactID = composeOutput.ArtifactID
		output.FinalVideoMediaFileID = composeOutput.MediaFileID
		output.FinalVideoStorageKey = composeOutput.StorageKey
		output.TimelineArtifactID = composeOutput.TimelineArtifactID
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteVideoProductionWorkflow", input, output).Get(ctx, nil); err != nil {
		return VideoProductionOutput{}, err
	}
	return output, nil
}

func (a Activities) AnalyzeScriptAssets(ctx context.Context, input AnalyzeScriptAssetsInput) (ScriptAssetsOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "script_to_assets", CreatedBy: input.CreatedBy}
	if err := validateScriptWorkflowInput(input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.ScriptID); err != nil {
		return ScriptAssetsOutput{}, err
	}
	script, err := a.activeScript(ctx, input.ProjectID, input.ScriptID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	existing, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	scriptScenes, err := a.scriptScenesForVersion(ctx, input.ProjectID, script.VersionID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	scriptContent := script.Content
	if len(scriptScenes) > 0 {
		scriptContent = FormatScriptScenesForPrompt(scriptScenes)
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyScriptAssetExtraction, map[string]any{
		"script": map[string]any{"id": script.ID, "versionId": script.VersionID, "content": scriptContent, "scenes": string(mustJSON(scriptScenes))},
		"assets": map[string]any{"existing": string(mustJSON(existing))},
	})
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeAnalyzeScriptAssetsKey,
		NodeType:       "agent.asset_analyze",
		Input: mustJSON(map[string]any{
			"scriptId":          input.ScriptID,
			"scriptVersionId":   script.VersionID,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return ScriptAssetsOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayResp, err := a.generateProviderText(ctx, nodeRunID, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID.NodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json"}),
		Options:           providerTextGatewayOptions(),
	})
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	candidates, parseErr := NormalizeScriptAssetExtraction(gatewayResp.Output.Text)
	if parseErr != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: parseErr.Error()})
	}
	output, err := a.upsertCanonicalAssets(ctx, input, script, nodeRunID, scriptScenes, candidates, rendered, gatewayResp)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	return output, nil
}

func (a Activities) PrepareScriptStoryboard(ctx context.Context, input PrepareScriptStoryboardInput) (ScriptStoryboardPlan, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "script_to_storyboard", CreatedBy: input.CreatedBy}
	if err := validateScriptWorkflowInput(input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.ScriptID); err != nil {
		return ScriptStoryboardPlan{}, err
	}
	script, err := a.activeScript(ctx, input.ProjectID, input.ScriptID)
	if err != nil {
		return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodePrepareStoryboardEpisodesKey,
		NodeType:       "workflow.storyboard_prepare_episodes",
		Input: mustJSON(map[string]any{
			"scriptId":         script.ID,
			"scriptVersionId":  script.VersionID,
			"scriptEpisodeIds": normalizeStringSlice(input.ScriptEpisodeIDs),
		}),
	})
	if err != nil {
		return ScriptStoryboardPlan{}, err
	}
	episodeIDs := normalizeStringSlice(input.ScriptEpisodeIDs)
	rows, err := a.db.Query(ctx, `
		SELECT id::text, episode_index, episode_title
		FROM script_episodes
		WHERE project_id = $1
		  AND script_id = $2
		  AND script_version_id = $3
		  AND (cardinality($4::uuid[]) = 0 OR id = ANY($4::uuid[]))
		ORDER BY episode_index ASC
	`, input.ProjectID, script.ID, script.VersionID, episodeIDs)
	if err != nil {
		return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	defer rows.Close()
	episodes := make([]ScriptStoryboardEpisodeRef, 0)
	for rows.Next() {
		var episode ScriptStoryboardEpisodeRef
		if err := rows.Scan(&episode.ID, &episode.EpisodeIndex, &episode.EpisodeTitle); err != nil {
			return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		episodes = append(episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if len(episodes) == 0 {
		return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "active script version has no episodes"})
	}
	if len(episodeIDs) > 0 && len(episodes) != len(episodeIDs) {
		return ScriptStoryboardPlan{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "scriptEpisodeIds do not match active script episodes"})
	}
	plan := ScriptStoryboardPlan{
		ScriptID:         script.ID,
		ScriptVersionID:  script.VersionID,
		EpisodeTotal:     len(episodes),
		TimelineTimebase: project.TimelineTimebase,
		FPSNumerator:     project.FPSNumerator,
		FPSDenominator:   project.FPSDenominator,
		Episodes:         episodes,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(plan)); err != nil {
		return ScriptStoryboardPlan{}, err
	}
	return plan, nil
}

func (a Activities) scriptStoryboardEpisode(ctx context.Context, projectID, scriptID, scriptVersionID, episodeID string) (ScriptStoryboardEpisodeRecord, error) {
	var episode ScriptStoryboardEpisodeRecord
	err := a.db.QueryRow(ctx, `
		SELECT id::text, episode_index, episode_title, content
		FROM script_episodes
		WHERE project_id = $1
		  AND script_id = $2
		  AND script_version_id = $3
		  AND id = $4
	`, projectID, scriptID, scriptVersionID, episodeID).Scan(
		&episode.ID,
		&episode.EpisodeIndex,
		&episode.EpisodeTitle,
		&episode.Content,
	)
	return episode, err
}

func (a Activities) GenerateStoryboardFromScript(ctx context.Context, input GenerateStoryboardFromScriptInput) (ScriptStoryboardOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "script_to_storyboard", CreatedBy: input.CreatedBy}
	if err := validateScriptWorkflowInput(input.OrganizationID, input.ProjectID, input.WorkflowRunID, firstNonEmptyString(input.ScriptID, input.ScriptSceneID, input.ScriptEpisodeID)); err != nil {
		return ScriptStoryboardOutput{}, err
	}
	if input.ScriptID == "" && input.ScriptSceneID != "" {
		scene, err := a.scriptSceneByID(ctx, input.ProjectID, input.ScriptSceneID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		input.ScriptID = scene.ScriptID
	}
	script, err := a.activeScript(ctx, input.ProjectID, input.ScriptID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if input.ScriptSceneID != "" {
		scene, err := a.scriptSceneByID(ctx, input.ProjectID, input.ScriptSceneID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		script, err = a.scriptForSceneParse(ctx, input.ProjectID, scene.ScriptID, scene.ScriptVersionID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
	}
	var episode ScriptStoryboardEpisodeRecord
	if input.ScriptEpisodeID != "" {
		episode, err = a.scriptStoryboardEpisode(ctx, input.ProjectID, script.ID, script.VersionID, input.ScriptEpisodeID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		input.EpisodeIndex = episode.EpisodeIndex
		input.EpisodeTitle = episode.EpisodeTitle
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	assets, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	maxShots := input.MaxShots
	maxOutputTokens := storyboardMaxOutputTokens(firstPositiveInt(maxShots, plannerBatchMaxShots))
	var scriptScenes []ScriptSceneRecord
	if input.ScriptEpisodeID != "" {
		scriptScenes, err = a.storyboardScenesForEpisode(ctx, input.ProjectID, script.VersionID, input.ScriptEpisodeID)
	} else {
		scriptScenes, err = a.storyboardScenesForScript(ctx, input.ProjectID, script.VersionID, input.ScriptSceneID)
	}
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	scriptContent := script.Content
	if input.ScriptEpisodeID != "" {
		scriptContent = episode.Content
	}
	dialogueSourceContent := scriptContent
	requiredDialogue := ExtractScriptDialogueLines(dialogueSourceContent)
	if len(scriptScenes) > 0 {
		scriptContent = FormatScriptScenesForPrompt(scriptScenes)
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardFromScript, map[string]any{
		"project": project.asPromptVariables(),
		"script":  map[string]any{"id": script.ID, "versionId": script.VersionID, "content": scriptContent, "scenes": string(mustJSON(scriptScenes)), "dialogueLines": string(mustJSON(requiredDialogue))},
		"episode": map[string]any{"id": input.ScriptEpisodeID, "index": input.EpisodeIndex, "total": input.EpisodeTotal, "title": input.EpisodeTitle},
		"assets":  map[string]any{"items": string(mustJSON(assets))},
		"input":   map[string]any{"maxShots": maxShots},
	})
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	dialogueConstraint := ""
	if len(requiredDialogue) > 0 {
		dialogueConstraint = "\n- Copy every entry from this required dialogue JSON into shots[].dialogue exactly once without translation or rewriting: " + string(mustJSON(requiredDialogue))
	}
	shotBudgetConstraint := "There is no fixed episode shot cap. Create every semantically required shot and preserve total narrative duration."
	if maxShots > 0 {
		shotBudgetConstraint = fmt.Sprintf("The user explicitly locked a maximum of %d shots. If complete dialogue and action cannot fit, return DURATION_CONSTRAINT_CONFLICT instead of deleting content.", maxShots)
	}
	rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) + fmt.Sprintf(`

<runtime_constraints priority="highest">
- Process only script episode %d of %d: %s.
- %s
- Keep every field concise and return all required shots; never truncate at 24 shots or clamp a shot to 15 seconds.
- Spoken dialogue timing takes priority over compact shot count; multiple exact dialogue lines may share a shot only when they fit its duration.%s
- Return one complete JSON object and stop immediately after its closing brace.
</runtime_constraints>`, input.EpisodeIndex, input.EpisodeTotal, input.EpisodeTitle, shotBudgetConstraint, dialogueConstraint)
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	rendered.Source = firstNonEmptyString(rendered.Source, "system_active") + "+episode_runtime_constraints"
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID(nodeGenerateStoryboardFromScript, firstNonEmptyString(input.ScriptEpisodeID, input.ScriptSceneID, script.VersionID)),
		NodeType:       "agent.storyboard_generate",
		Input: mustJSON(map[string]any{
			"scriptId":          input.ScriptID,
			"scriptVersionId":   script.VersionID,
			"scriptEpisodeId":   input.ScriptEpisodeID,
			"episodeIndex":      input.EpisodeIndex,
			"episodeTotal":      input.EpisodeTotal,
			"episodeTitle":      input.EpisodeTitle,
			"maxShots":          maxShots,
			"maxOutputTokens":   maxOutputTokens,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return ScriptStoryboardOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayResp, err := a.generateProviderText(ctx, nodeRunID, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID.NodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": maxOutputTokens}),
		Options:           providerTextGatewayOptions(),
	})
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	storyboard, parseError := parseStoryboardText(gatewayResp.Output.Text)
	parsedShots, parseShotsErr := ParseStoryboardShots(storyboard)
	if parseShotsErr != nil && parseError == "" {
		parseError = parseShotsErr.Error()
	}
	normalizedShots := NormalizeStoryboardShots(parsedShots, scriptContent)
	if maxShots > 0 && len(normalizedShots) > maxShots {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: "DURATION_CONSTRAINT_CONFLICT", Message: fmt.Sprintf("complete storyboard requires %d shots but the user budget is %d", len(normalizedShots), maxShots)})
	}
	normalizedShots, err = QuantizeStoryboardShotCandidates(normalizedShots, project)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	durationMetrics := newStoryboardDurationMetrics(parsedShots, normalizedShots)
	normalizedShots = assignScriptScenesToShots(normalizedShots, scriptScenes)
	if input.ScriptEpisodeID != "" {
		if err := ValidateStoryboardDialogueCoverage(normalizedShots, dialogueSourceContent, requiredDialogue); err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
		}
	}
	parsedRequirements, err := ParseShotAssetRequirements(storyboard)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	requirements, err := ResolveShotAssetRequirements(parsedRequirements, normalizedShots, assets)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	storyboardValue := map[string]any{
		"storyboard":      storyboard,
		"rawText":         gatewayResp.Output.Text,
		"shots":           normalizedShots,
		"requirements":    requirements,
		"scriptId":        script.ID,
		"scriptVersion":   script.VersionID,
		"scriptScenes":    scriptScenes,
		"scriptEpisodeId": input.ScriptEpisodeID,
		"episodeIndex":    input.EpisodeIndex,
		"episodeTotal":    input.EpisodeTotal,
		"episodeTitle":    input.EpisodeTitle,
		"durationMetrics": durationMetrics,
	}
	if parseError != "" {
		storyboardValue["parseError"] = parseError
	}
	storageSuffix := "script-storyboard.json"
	if input.ScriptEpisodeID != "" {
		storageSuffix = fmt.Sprintf("episode-%04d-%s.json", input.EpisodeIndex, input.ScriptEpisodeID)
	}
	storageKey := fmt.Sprintf("org/%s/project/%s/workflow/%s/storyboard/%s", input.OrganizationID, input.ProjectID, input.WorkflowRunID, storageSuffix)
	put, err := a.storage.PutJSON(ctx, storageKey, storyboardValue)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	artifactID, shotRecords, requirementRecords, err := a.insertScriptStoryboardArtifactShotsAndRequirements(ctx, input, script, project, nodeRunID, put, gatewayResp, rendered.RenderedHash, normalizedShots, requirements, storyboard, parseError, &durationMetrics)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	output := ScriptStoryboardOutput{
		ScriptID:             script.ID,
		ScriptVersionID:      script.VersionID,
		ScriptEpisodeID:      input.ScriptEpisodeID,
		EpisodeIndex:         input.EpisodeIndex,
		EpisodeTotal:         input.EpisodeTotal,
		EpisodeTitle:         input.EpisodeTitle,
		StoryboardArtifactID: artifactID,
		StorageKey:           put.StorageKey,
		ProviderCallID:       gatewayResp.ProviderCallID,
		ModelID:              gatewayResp.ModelID,
		Storyboard:           storyboard,
		Shots:                shotRecords,
		Requirements:         requirementRecords,
		RawText:              gatewayResp.Output.Text,
		ParseError:           parseError,
		DurationMetrics:      durationMetrics,
	}
	return output, nil
}

func storyboardMaxOutputTokens(maxShots int) int {
	if maxShots <= 0 {
		maxShots = plannerBatchMaxShots
	}
	const (
		baseTokens    = 1200
		perShotTokens = 700
		maxTokens     = 18000
	)
	tokens := baseTokens + maxShots*perShotTokens
	if tokens > maxTokens {
		return maxTokens
	}
	return tokens
}

func newStoryboardDurationMetrics(raw, planned []StoryboardShot) StoryboardDurationMetrics {
	return StoryboardDurationMetrics{
		RawShotCount:           len(raw),
		PlannedShotCount:       len(planned),
		RawDurationSeconds:     storyboardShotDurationTotal(raw),
		PlannedDurationSeconds: storyboardShotDurationTotal(planned),
	}
}

func (metrics *StoryboardDurationMetrics) recordStored(shots []StoryboardShotRecord) {
	metrics.StoredShotCount = len(shots)
	for _, shot := range shots {
		metrics.StoredDurationSeconds += shot.Duration
	}
	metrics.DurationLossSeconds = metrics.RawDurationSeconds - metrics.StoredDurationSeconds
}

func mergeStoryboardDurationMetrics(target *StoryboardDurationMetrics, source StoryboardDurationMetrics) {
	target.RawShotCount += source.RawShotCount
	target.PlannedShotCount += source.PlannedShotCount
	target.StoredShotCount += source.StoredShotCount
	target.RawDurationSeconds += source.RawDurationSeconds
	target.PlannedDurationSeconds += source.PlannedDurationSeconds
	target.StoredDurationSeconds += source.StoredDurationSeconds
	target.DurationLossSeconds += source.DurationLossSeconds
}

func storyboardShotDurationTotal(shots []StoryboardShot) float64 {
	total := 0.0
	for _, shot := range shots {
		total += shot.Duration
	}
	return total
}

func (a Activities) CompleteScriptAssetsWorkflow(ctx context.Context, input TextToStoryboardInput, output ScriptAssetsOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) CompleteScriptStoryboardWorkflow(ctx context.Context, input TextToStoryboardInput, output ScriptStoryboardOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) FailScriptStoryboardWorkflow(ctx context.Context, input TextToStoryboardInput, code, message string) error {
	return a.markWorkflowFailed(ctx, input, code, message)
}

func recordScriptStoryboardWorkflowFailure(ctx workflow.Context, input TextToStoryboardInput, cause error) {
	code, message := storyboardWorkflowFailureDetails(cause)
	failureCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	_ = workflow.ExecuteActivity(failureCtx, "FailScriptStoryboardWorkflow", input, code, message).Get(failureCtx, nil)
}

func storyboardWorkflowFailureDetails(cause error) (string, string) {
	var applicationErr *temporal.ApplicationError
	if errors.As(cause, &applicationErr) {
		code := strings.TrimSpace(applicationErr.Type())
		if code == "" {
			code = codeActivityFailed
		}
		return code, applicationErr.Error()
	}
	return codeActivityFailed, cause.Error()
}

func (a Activities) completeSimpleWorkflow(ctx context.Context, input TextToStoryboardInput, output any) error {
	return TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, "succeeded", "", "", mustJSON(output))
}

func resolveScriptProductionOptions(raw json.RawMessage) ScriptProductionOptions {
	options := ScriptProductionOptions{
		MergeExisting:        true,
		PacingProfile:        "standard",
		AudioStrategy:        "native_av",
		AudioRequirement:     "preferred",
		PlannerBatchMaxShots: plannerBatchMaxShots,
		MaxSceneConcurrency:  3,
	}
	if len(raw) == 0 {
		return options
	}
	_ = json.Unmarshal(raw, &options)
	options.ScriptEpisodeIDs = normalizeStringSlice(options.ScriptEpisodeIDs)
	if options.MaxShots < 0 {
		options.MaxShots = 0
	}
	options.PacingProfile = defaultStoryboardPacingProfile(options.PacingProfile)
	if options.PlannerBatchMaxShots <= 0 {
		options.PlannerBatchMaxShots = plannerBatchMaxShots
	}
	if options.MaxSceneConcurrency <= 0 {
		options.MaxSceneConcurrency = 3
	}
	if options.MaxSceneConcurrency > 8 {
		options.MaxSceneConcurrency = 8
	}
	if options.ShotBudget < 0 {
		options.ShotBudget = 0
	}
	return options
}

func validateScriptWorkflowInput(organizationID, projectID, workflowRunID, scriptID string) error {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(workflowRunID) == "" || strings.TrimSpace(scriptID) == "" {
		return fmt.Errorf("organizationId, projectId, workflowRunId, and scriptId are required")
	}
	return nil
}

func NormalizeScriptAssetExtraction(text string) ([]ScriptAssetCandidate, error) {
	candidate := stripJSONFence(text)
	var decoded struct {
		Assets []ScriptAssetCandidate `json:"assets"`
	}
	if err := json.Unmarshal([]byte(candidate), &decoded); err != nil {
		return nil, err
	}
	out := make([]ScriptAssetCandidate, 0, len(decoded.Assets))
	seen := map[string]bool{}
	for _, asset := range decoded.Assets {
		asset.AssetType = normalizeAssetType(asset.AssetType)
		asset.Name = strings.TrimSpace(asset.Name)
		asset.Description = strings.TrimSpace(asset.Description)
		asset.BasePrompt = strings.TrimSpace(asset.BasePrompt)
		if len(asset.VisualTraits) == 0 {
			asset.VisualTraits = json.RawMessage(`{}`)
		}
		if asset.AssetType == "" || asset.Name == "" || asset.Description == "" {
			continue
		}
		if asset.AssetType == "character" {
			name, ok := normalizeCharacterAssetName(asset.Name)
			if !ok {
				continue
			}
			asset.Name = name
		}
		key := asset.AssetType + "\x00" + strings.ToLower(asset.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, asset)
	}
	return out, nil
}

func normalizeCharacterAssetName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, pair := range [][2]string{{"（", "）"}, {"(", ")"}, {"【", "】"}, {"[", "]"}} {
		name = trimVariantBracket(name, pair[0], pair[1])
	}
	for _, separator := range []string{" - ", "-", "—", "：", ":", "/", "／"} {
		parts := strings.Split(name, separator)
		if len(parts) != 2 {
			continue
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		switch {
		case left != "" && looksLikeCharacterVariant(right):
			name = left
		case right != "" && looksLikeCharacterVariant(left):
			name = right
		}
	}
	name = trimCharacterVariantAffixes(strings.TrimSpace(name))
	if name == "" || looksLikeCharacterVariant(name) {
		return "", false
	}
	return name, true
}

func trimVariantBracket(value, open, close string) string {
	start := strings.LastIndex(value, open)
	end := strings.LastIndex(value, close)
	if start < 0 || end <= start {
		return value
	}
	inside := strings.TrimSpace(value[start+len(open) : end])
	if !looksLikeCharacterVariant(inside) {
		return value
	}
	return strings.TrimSpace(value[:start] + value[end+len(close):])
}

func trimCharacterVariantAffixes(value string) string {
	for {
		next := value
		for _, prefix := range characterVariantAffixes() {
			next = strings.TrimPrefix(next, prefix)
		}
		for _, suffix := range characterVariantAffixes() {
			next = strings.TrimSuffix(next, suffix)
		}
		next = strings.TrimSpace(next)
		if next == value {
			return value
		}
		value = next
	}
}

func looksLikeCharacterVariant(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, marker := range characterVariantMarkers() {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func characterVariantMarkers() []string {
	return []string{
		"服装", "衣", "袍", "披风", "盔甲", "铠甲", "战损", "受伤", "伤痕", "血迹",
		"姿态", "站姿", "坐姿", "侧身", "背影", "特写", "表情", "微笑", "愤怒",
		"阶段", "时期", "版本", "形态", "造型", "状态", "幼年", "少年", "青年", "成年", "老年",
		"costume", "outfit", "pose", "expression", "stage", "phase", "variant", "version", "wounded", "injured",
	}
}

func characterVariantAffixes() []string {
	return []string{
		"幼年", "少年", "青年", "成年", "老年", "红衣", "黑衣", "白衣", "便装", "战损", "受伤",
		"幼年版", "少年版", "青年版", "成年版", "老年版", "战损版",
	}
}

func ParseShotAssetRequirements(storyboard json.RawMessage) ([]ShotAssetRequirementRecord, error) {
	var decoded struct {
		Shots []struct {
			ShotNo            int               `json:"shotNo"`
			AssetRequirements []json.RawMessage `json:"assetRequirements"`
		} `json:"shots"`
	}
	if err := json.Unmarshal(storyboard, &decoded); err != nil {
		return nil, fmt.Errorf("decode storyboard asset requirements: %w", err)
	}
	out := make([]ShotAssetRequirementRecord, 0)
	for shotIndex, shot := range decoded.Shots {
		shotNo := shot.ShotNo
		if shotNo <= 0 {
			shotNo = shotIndex + 1
		}
		for requirementIndex, raw := range shot.AssetRequirements {
			req, err := parseShotAssetRequirement(raw)
			if err != nil {
				return nil, fmt.Errorf("decode shot %d asset requirement %d: %w", shotNo, requirementIndex+1, err)
			}
			req.ShotNo = shotNo
			req.AssetType = normalizeAssetType(req.AssetType)
			req.AssetID = strings.TrimSpace(req.AssetID)
			req.AssetName = strings.TrimSpace(req.AssetName)
			req.RequirementType = strings.TrimSpace(req.RequirementType)
			if req.RequirementType == "" && req.AssetType != "" {
				req.RequirementType = defaultRequirementType(req.AssetType)
			}
			req.RoleInShot = strings.TrimSpace(req.RoleInShot)
			req.Costume = strings.TrimSpace(req.Costume)
			req.Pose = strings.TrimSpace(req.Pose)
			req.Expression = strings.TrimSpace(req.Expression)
			req.Action = strings.TrimSpace(req.Action)
			req.CameraRelation = strings.TrimSpace(req.CameraRelation)
			req.SceneState = strings.TrimSpace(req.SceneState)
			req.PropState = strings.TrimSpace(req.PropState)
			req.Prompt = strings.TrimSpace(req.Prompt)
			if req.AssetID == "" && req.AssetName == "" {
				continue
			}
			out = append(out, req)
		}
	}
	return out, nil
}

func NormalizeShotAssetRequirements(storyboard json.RawMessage) []ShotAssetRequirementRecord {
	requirements, _ := ParseShotAssetRequirements(storyboard)
	return requirements
}

func parseShotAssetRequirement(raw json.RawMessage) (ShotAssetRequirementRecord, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ShotAssetRequirementRecord{}, nil
	}
	if raw[0] == '"' {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return ShotAssetRequirementRecord{}, err
		}
		return ShotAssetRequirementRecord{AssetName: strings.TrimSpace(name)}, nil
	}
	if raw[0] != '{' {
		return ShotAssetRequirementRecord{}, fmt.Errorf("expected an asset name string or object")
	}
	var value struct {
		ShotAssetRequirementRecord
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ShotAssetRequirementRecord{}, err
	}
	result := value.ShotAssetRequirementRecord
	if strings.TrimSpace(result.AssetName) == "" {
		result.AssetName = value.Name
	}
	if strings.TrimSpace(result.AssetType) == "" {
		result.AssetType = value.Type
	}
	return result, nil
}

func ResolveShotAssetRequirements(requirements []ShotAssetRequirementRecord, shots []StoryboardShot, assets []CanonicalAssetRecord) ([]ShotAssetRequirementRecord, error) {
	assetsByID := make(map[string]CanonicalAssetRecord, len(assets))
	assetsByKey := make(map[string]CanonicalAssetRecord, len(assets))
	assetsByName := make(map[string][]CanonicalAssetRecord, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
		assetsByKey[assetKey(asset.AssetType, asset.Name)] = asset
		nameKey := strings.ToLower(strings.TrimSpace(asset.Name))
		assetsByName[nameKey] = append(assetsByName[nameKey], asset)
	}

	rawCounts := map[int]int{}
	matchedCounts := map[int]int{}
	seen := map[string]struct{}{}
	resolved := make([]ShotAssetRequirementRecord, 0, len(requirements))
	appendResolved := func(req ShotAssetRequirementRecord, asset CanonicalAssetRecord) {
		req.AssetID = asset.ID
		req.AssetName = asset.Name
		req.AssetType = asset.AssetType
		if req.RequirementType == "" || req.RequirementType == "shot_context" {
			req.RequirementType = defaultRequirementType(asset.AssetType)
		}
		key := fmt.Sprintf("%d\x00%s\x00%s", req.ShotNo, req.AssetID, req.RequirementType)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		resolved = append(resolved, req)
		matchedCounts[req.ShotNo]++
	}

	for _, req := range requirements {
		rawCounts[req.ShotNo]++
		asset, ok := assetsByID[strings.TrimSpace(req.AssetID)]
		if !ok && req.AssetType != "" {
			asset, ok = assetsByKey[assetKey(req.AssetType, req.AssetName)]
		}
		if !ok {
			matches := assetsByName[strings.ToLower(strings.TrimSpace(req.AssetName))]
			if len(matches) == 1 {
				asset, ok = matches[0], true
			}
		}
		if ok {
			appendResolved(req, asset)
		}
	}

	// Model output is advisory. Visible asset names and dialogue speakers provide a
	// deterministic fallback when the model omits a structured requirement.
	for _, shot := range shots {
		visibleText := strings.Join(compactStrings([]string{shot.Title, shot.Visual, shot.Motion, shot.ImagePrompt}), "\n")
		for _, asset := range assets {
			mentioned := strings.Contains(strings.ToLower(visibleText), strings.ToLower(strings.TrimSpace(asset.Name)))
			if !mentioned && asset.AssetType == "character" {
				for _, line := range NormalizeStoryboardDialogue(shot.Dialogue) {
					speaker := strings.TrimSpace(line.Speaker)
					if speaker != "" && (strings.Contains(speaker, asset.Name) || strings.Contains(asset.Name, speaker)) {
						mentioned = true
						break
					}
				}
			}
			if mentioned {
				appendResolved(ShotAssetRequirementRecord{ShotNo: shot.ShotNo}, asset)
			}
		}
	}

	failedShots := make([]string, 0)
	for shotNo, count := range rawCounts {
		if count > 0 && matchedCounts[shotNo] == 0 {
			failedShots = append(failedShots, fmt.Sprintf("%d", shotNo))
		}
	}
	if len(failedShots) > 0 {
		return nil, fmt.Errorf("storyboard asset requirements could not be matched for shots %s", strings.Join(failedShots, ", "))
	}
	return resolved, nil
}

func normalizeAssetType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "role", "character", "角色":
		return "character"
	case "scene", "场景":
		return "scene"
	case "tool", "prop", "道具":
		return "prop"
	default:
		return ""
	}
}

func defaultRequirementType(assetType string) string {
	switch assetType {
	case "character":
		return "character_appearance"
	case "scene":
		return "scene_variant"
	case "prop":
		return "prop_state"
	default:
		return "shot_context"
	}
}

func (p ProjectProductionSettings) asPromptVariables() map[string]any {
	return map[string]any{
		"id":               p.ID,
		"projectType":      p.ProjectType,
		"contentType":      p.ContentType,
		"aspectRatio":      p.AspectRatio,
		"videoRatio":       p.VideoRatio,
		"artStyle":         p.ArtStyle,
		"directorManual":   p.DirectorManual,
		"visualManual":     p.VisualManual,
		"imageQuality":     p.ImageQuality,
		"productionMode":   p.ProductionMode,
		"timelineTimebase": p.TimelineTimebase,
		"fpsNumerator":     p.FPSNumerator,
		"fpsDenominator":   p.FPSDenominator,
	}
}

type GenerateCanonicalAssetImageInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`
	AssetID        string `json:"assetId"`
}

type GenerateCanonicalAssetImageOutput struct {
	AssetID          string `json:"assetId"`
	ProviderCallID   string `json:"providerCallId,omitempty"`
	ImageArtifactID  string `json:"imageArtifactId,omitempty"`
	ImageMediaFileID string `json:"imageMediaFileId,omitempty"`
	ImageStorageKey  string `json:"imageStorageKey,omitempty"`
}

type GenerateDerivedAssetImageInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`
	RequirementID  string `json:"requirementId"`
}

type GenerateDerivedAssetImageOutput struct {
	RequirementID    string `json:"requirementId"`
	ProviderCallID   string `json:"providerCallId,omitempty"`
	ImageArtifactID  string `json:"imageArtifactId,omitempty"`
	ImageMediaFileID string `json:"imageMediaFileId,omitempty"`
	ImageStorageKey  string `json:"imageStorageKey,omitempty"`
}

func (a Activities) GenerateCanonicalAssetImage(ctx context.Context, input GenerateCanonicalAssetImageInput) (_ GenerateCanonicalAssetImageOutput, err error) {
	var nodeRunID NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, nodeRunID, err)
	}()
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "canonical_asset_image", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.AssetID) == "" {
		return GenerateCanonicalAssetImageOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and assetId are required")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	asset, err := a.canonicalAssetByID(ctx, input.ProjectID, input.AssetID)
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyCanonicalAssetImage, map[string]any{
		"project": project.asPromptVariables(),
		"asset": map[string]any{
			"assetType":         asset.AssetType,
			"type":              asset.AssetType,
			"name":              asset.Name,
			"description":       asset.Description,
			"profile":           string(asset.Profile),
			"basePrompt":        asset.BasePrompt,
			"consistencyPrompt": asset.ConsistencyPrompt,
			"negativePrompt":    asset.NegativePrompt,
			"visualTraits":      string(asset.VisualTraits),
		},
	})
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	rendered, err = a.withToonflowVisualPrompt(ctx, project, rendered, asset.AssetType, false)
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	rendered = withCanonicalAssetImageRequirements(rendered, asset.AssetType)
	nodeRunID, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID(nodeGenerateCanonicalAssetPrefix, input.AssetID),
		NodeType:       "image.generate",
		Input: mustJSON(map[string]any{
			"assetId":           input.AssetID,
			"modelProfileKey":   project.ImageModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, err
	}
	if err := a.updateCanonicalAssetImageStatus(ctx, input.WorkflowRunID, nodeRunID, input.AssetID, "image_running"); err != nil {
		return GenerateCanonicalAssetImageOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ImageModelProfileKey, []string{"image", "multimodal"}); err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayResp, err := a.gateway.GenerateImage(ctx, provider.GatewayImageRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID.NodeRunID,
		ModelProfileKey:   project.ImageModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(assetprompts.CanonicalImageInput(rendered.RenderedText, asset.AssetType, project.ImageQuality)),
		References:        lockedCanonicalAssetRecordImageReferences(asset),
	})
	if err != nil {
		_ = a.updateCanonicalAssetImageStatus(context.WithoutCancel(ctx), input.WorkflowRunID, nodeRunID, input.AssetID, "image_failed")
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateCanonicalAssetImageOutput{
		AssetID:          input.AssetID,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ImageArtifactID:  gatewayResp.Output.ArtifactID,
		ImageMediaFileID: gatewayResp.Output.MediaFileID,
		ImageStorageKey:  gatewayResp.Output.StorageKey,
	}
	if err := a.completeCanonicalAssetImage(ctx, input, nodeRunID, asset, rendered, output); err != nil {
		return GenerateCanonicalAssetImageOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateDerivedAssetImage(ctx context.Context, input GenerateDerivedAssetImageInput) (_ GenerateDerivedAssetImageOutput, err error) {
	var nodeRunID NodeExecution
	defer func() {
		err = finalizeWorkflowActivityError(ctx, a.db, nodeRunID, err)
	}()
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "derived_asset_image", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.RequirementID) == "" {
		return GenerateDerivedAssetImageOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and requirementId are required")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	requirement, err := a.shotAssetRequirementByID(ctx, input.ProjectID, input.RequirementID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	asset, err := a.canonicalAssetByID(ctx, input.ProjectID, requirement.AssetID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	shot, err := a.storyboardShotByID(ctx, input.ProjectID, requirement.StoryboardShotID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyDerivedAssetImage, map[string]any{
		"project": project.asPromptVariables(),
		"baseAsset": map[string]any{
			"name":        asset.Name,
			"description": asset.Description,
		},
		"shot":        map[string]any{"summary": storyboardShotSummary(shot)},
		"requirement": map[string]any{"summary": shotRequirementSummary(requirement)},
	})
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, err)
	}
	nodeRunID, err = StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKeyForID(nodeGenerateDerivedAssetPrefix, input.RequirementID),
		NodeType:       "image.generate",
		Input: mustJSON(map[string]any{
			"requirementId":     input.RequirementID,
			"assetId":           asset.ID,
			"modelProfileKey":   project.ImageModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, err
	}
	if err := a.updateDerivedAssetImageStatus(ctx, input.WorkflowRunID, nodeRunID, input.RequirementID, "image_running"); err != nil {
		return GenerateDerivedAssetImageOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ImageModelProfileKey, []string{"image", "multimodal"}); err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	refs := make([]provider.GatewayImageReference, 0, 1)
	if asset.ReferenceArtifactID != "" || asset.ReferenceStorageKey != "" {
		refs = append(refs, provider.GatewayImageReference{
			Type:       "image",
			AssetID:    asset.ID,
			ArtifactID: asset.ReferenceArtifactID,
			StorageKey: asset.ReferenceStorageKey,
		})
	}
	gatewayResp, err := a.gateway.GenerateImage(ctx, provider.GatewayImageRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID.NodeRunID,
		ModelProfileKey:   project.ImageModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustJSON(map[string]any{
			"prompt":  rendered.RenderedText,
			"size":    "1024x1024",
			"n":       1,
			"quality": project.ImageQuality,
		}),
		References: refs,
	})
	if err != nil {
		_ = a.updateDerivedAssetImageStatus(context.WithoutCancel(ctx), input.WorkflowRunID, nodeRunID, input.RequirementID, "image_failed")
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateDerivedAssetImageOutput{
		RequirementID:    input.RequirementID,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ImageArtifactID:  gatewayResp.Output.ArtifactID,
		ImageMediaFileID: gatewayResp.Output.MediaFileID,
		ImageStorageKey:  gatewayResp.Output.StorageKey,
	}
	if err := a.completeDerivedAssetImage(ctx, input, nodeRunID, output); err != nil {
		return GenerateDerivedAssetImageOutput{}, err
	}
	return output, nil
}

func (a Activities) updateCanonicalAssetImageStatus(ctx context.Context, workflowRunID string, execution NodeExecution, assetID, status string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, workflowRunID, execution); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET status = $2, revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status <> 'archived'
	`, assetID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) updateDerivedAssetImageStatus(ctx context.Context, workflowRunID string, execution NodeExecution, requirementID, status string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, workflowRunID, execution); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET status = $2, updated_at = now()
		WHERE id = $1
	`, requirementID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}
