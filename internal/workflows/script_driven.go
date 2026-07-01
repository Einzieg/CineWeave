package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeAnalyzeScriptAssetsKey       = "analyze_script_assets"
	nodeGenerateStoryboardFromScript = "generate_storyboard_from_script"
	nodeGenerateCanonicalAssetPrefix = "generate_canonical_asset"
	nodeGenerateDerivedAssetPrefix   = "generate_derived_asset"
	promptKeyScriptAssetExtraction   = "script_asset_extraction"
	promptKeyCanonicalAssetImage     = "canonical_asset_image_prompt"
	promptKeyStoryboardFromScript    = "storyboard_from_script"
	promptKeyDerivedAssetImage       = "derived_asset_image_prompt"
	promptKeyShotImage               = "shot_image_prompt"
	promptKeyShotVideo               = "shot_video_prompt"
)

type ScriptProductionOptions struct {
	ScriptID              string `json:"scriptId"`
	ScriptSceneID         string `json:"scriptSceneId,omitempty"`
	MergeExisting         bool   `json:"mergeExisting"`
	GenerateImages        bool   `json:"generateImages"`
	GenerateDerivedAssets bool   `json:"generateDerivedAssets"`
	MaxShots              int    `json:"maxShots"`
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
	ImageQuality          string `json:"imageQuality"`
	ProductionMode        string `json:"productionMode"`
}

type ScriptAssetCandidate struct {
	AssetType    string          `json:"assetType"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	BasePrompt   string          `json:"basePrompt,omitempty"`
	VisualTraits json.RawMessage `json:"visualTraits,omitempty"`
}

type CanonicalAssetRecord struct {
	ID                   string          `json:"id"`
	AssetType            string          `json:"assetType"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	BasePrompt           string          `json:"basePrompt,omitempty"`
	VisualTraits         json.RawMessage `json:"visualTraits,omitempty"`
	ReferenceArtifactID  string          `json:"referenceArtifactId,omitempty"`
	ReferenceMediaFileID string          `json:"referenceMediaFileId,omitempty"`
	ReferenceStorageKey  string          `json:"referenceStorageKey,omitempty"`
	Status               string          `json:"status"`
	ManualOverride       bool            `json:"manualOverride,omitempty"`
	StaleState           string          `json:"staleState,omitempty"`
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
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`
	ScriptID       string `json:"scriptId"`
	ScriptSceneID  string `json:"scriptSceneId,omitempty"`
	MaxShots       int    `json:"maxShots,omitempty"`
}

type ScriptStoryboardOutput struct {
	ScriptID             string                       `json:"scriptId"`
	ScriptVersionID      string                       `json:"scriptVersionId"`
	StoryboardArtifactID string                       `json:"storyboardArtifactId"`
	StorageKey           string                       `json:"storageKey"`
	ProviderCallID       string                       `json:"providerCallId,omitempty"`
	ModelID              string                       `json:"modelId,omitempty"`
	Storyboard           json.RawMessage              `json:"storyboard"`
	Shots                []StoryboardShotRecord       `json:"shots"`
	Requirements         []ShotAssetRequirementRecord `json:"requirements"`
	RawText              string                       `json:"rawText,omitempty"`
	ParseError           string                       `json:"parseError,omitempty"`
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
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
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
	var assets ScriptAssetsOutput
	if err := workflow.ExecuteActivity(ctx, "AnalyzeScriptAssets", AnalyzeScriptAssetsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ScriptID:       options.ScriptID,
		MergeExisting:  true,
	}).Get(ctx, &assets); err != nil {
		return ScriptStoryboardOutput{}, err
	}
	_ = assets
	var output ScriptStoryboardOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardFromScript", GenerateStoryboardFromScriptInput{
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
	if options.GenerateDerivedAssets {
		for _, requirement := range output.Requirements {
			var derived GenerateDerivedAssetImageOutput
			if err := workflow.ExecuteActivity(ctx, "GenerateDerivedAssetImage", GenerateDerivedAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				RequirementID:  requirement.ID,
			}).Get(ctx, &derived); err != nil {
				return ScriptStoryboardOutput{}, err
			}
		}
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteScriptStoryboardWorkflow", input, output).Get(ctx, nil); err != nil {
		return ScriptStoryboardOutput{}, err
	}
	return output, nil
}

func ScriptDrivenVideoProduction(ctx workflow.Context, input TextToStoryboardInput, options videoProductionOptions, scriptOptions ScriptProductionOptions) (VideoProductionOutput, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
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
			if err := workflow.ExecuteActivity(ctx, "GenerateCanonicalAssetImage", GenerateCanonicalAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				AssetID:        asset.ID,
			}).Get(ctx, &imageOutput); err != nil {
				return VideoProductionOutput{}, err
			}
		}
	}
	var storyboard ScriptStoryboardOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardFromScript", GenerateStoryboardFromScriptInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ScriptID:       scriptOptions.ScriptID,
		ScriptSceneID:  scriptOptions.ScriptSceneID,
		MaxShots:       options.MaxShots,
	}).Get(ctx, &storyboard); err != nil {
		return VideoProductionOutput{}, err
	}
	if scriptOptions.GenerateDerivedAssets {
		for _, requirement := range storyboard.Requirements {
			var derived GenerateDerivedAssetImageOutput
			if err := workflow.ExecuteActivity(ctx, "GenerateDerivedAssetImage", GenerateDerivedAssetImageInput{
				OrganizationID: input.OrganizationID,
				ProjectID:      input.ProjectID,
				WorkflowRunID:  input.WorkflowRunID,
				CreatedBy:      input.CreatedBy,
				RequirementID:  requirement.ID,
			}).Get(ctx, &derived); err != nil {
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
	if len(shots) > options.MaxShots {
		shots = shots[:options.MaxShots]
	}

	createActivityOptions := defaultActivityOptions()
	createActivityOptions.RetryPolicy.MaximumAttempts = 1
	createCtx := workflow.WithActivityOptions(ctx, createActivityOptions)
	providerCalls := VideoProductionProviderCalls{
		Storyboard: storyboard.ProviderCallID,
	}
	shotOutputs := make([]VideoProductionShotOutput, 0, len(shots))
	for _, shot := range shots {
		var image GenerateShotImageOutput
		if err := workflow.ExecuteActivity(ctx, "GenerateShotImage", GenerateShotImageInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: firstNonEmptyString(input.Prompt, "script_to_video"),
			AspectRatio:    options.AspectRatio,
		}).Get(ctx, &image); err != nil {
			return VideoProductionOutput{}, err
		}
		if image.ProviderCallID != "" {
			providerCalls.Images = append(providerCalls.Images, image.ProviderCallID)
		}
		duration := shot.Duration
		if duration <= 0 {
			duration = options.Duration
		}
		if duration > maxShotDuration {
			duration = maxShotDuration
		}
		var createOutput CreateShotVideoTaskOutput
		if err := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", CreateShotVideoTaskInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			ShotID:         shot.ID,
			ShotIndex:      shot.ShotIndex,
			ShotNo:         shot.ShotNo,
			WorkflowPrompt: firstNonEmptyString(input.Prompt, "script_to_video"),
			Duration:       duration,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
		}).Get(createCtx, &createOutput); err != nil {
			return VideoProductionOutput{}, err
		}
		if createOutput.ProviderCallID != "" {
			providerCalls.VideoCreates = append(providerCalls.VideoCreates, createOutput.ProviderCallID)
		}
		var terminalPoll PollShotVideoTaskOutput
		shotTerminal := false
		for pollCount := 1; pollCount <= options.MaxPolls; pollCount++ {
			var pollOutput PollShotVideoTaskOutput
			if err := workflow.ExecuteActivity(ctx, "PollShotVideoTask", PollShotVideoTaskInput{
				OrganizationID:      input.OrganizationID,
				ProjectID:           input.ProjectID,
				WorkflowRunID:       input.WorkflowRunID,
				ShotID:              shot.ID,
				ShotIndex:           shot.ShotIndex,
				ShotNo:              shot.ShotNo,
				NodeRunID:           createOutput.NodeRunID,
				ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
				ExternalTaskID:      createOutput.ExternalTaskID,
				PollCount:           pollCount,
			}).Get(ctx, &pollOutput); err != nil {
				return VideoProductionOutput{}, err
			}
			if pollOutput.ProviderCallID != "" {
				providerCalls.VideoPolls = append(providerCalls.VideoPolls, pollOutput.ProviderCallID)
			}
			if pollOutput.Status == "succeeded" {
				terminalPoll = pollOutput
				shotTerminal = true
				break
			}
			if pollOutput.Status == "failed" || pollOutput.Status == "cancelled" {
				return VideoProductionOutput{}, temporal.NewApplicationError("provider video task "+pollOutput.Status, codeActivityFailed)
			}
			if err := workflow.Sleep(ctx, options.PollInterval); err != nil {
				return VideoProductionOutput{}, err
			}
		}
		if !shotTerminal {
			timeoutMessage := "provider video task polling timed out"
			if err := workflow.ExecuteActivity(ctx, "FailVideoProductionWorkflow", input, createOutput.NodeRunID, codeProviderVideoPollingTimeout, timeoutMessage).Get(ctx, nil); err != nil {
				return VideoProductionOutput{}, err
			}
			return VideoProductionOutput{}, temporal.NewApplicationError(timeoutMessage, codeProviderVideoPollingTimeout)
		}
		shotOutputs = append(shotOutputs, VideoProductionShotOutput{
			ShotID:              shot.ID,
			ShotIndex:           shot.ShotIndex,
			ShotNo:              shot.ShotNo,
			Duration:            duration,
			ImageArtifactID:     image.ImageArtifactID,
			ImageMediaFileID:    image.ImageMediaFileID,
			ImageStorageKey:     image.ImageStorageKey,
			VideoArtifactID:     terminalPoll.ArtifactID,
			VideoMediaFileID:    terminalPoll.MediaFileID,
			VideoStorageKey:     terminalPoll.StorageKey,
			ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
			ExternalTaskID:      firstNonEmptyString(terminalPoll.ExternalTaskID, createOutput.ExternalTaskID),
		})
	}
	output := VideoProductionOutput{
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
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			CreatedBy:      input.CreatedBy,
			AspectRatio:    options.AspectRatio,
			Resolution:     options.Resolution,
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
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	existing, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	scriptScenes, err := a.scriptScenesForVersion(ctx, input.ProjectID, script.VersionID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
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
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, "", err)
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
	gatewayResp, err := a.gateway.GenerateText(ctx, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json"}),
	})
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	candidates, parseErr := NormalizeScriptAssetExtraction(gatewayResp.Output.Text)
	if parseErr != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: parseErr.Error()})
	}
	assets, err := a.upsertCanonicalAssets(ctx, input, script, candidates, rendered, gatewayResp.ProviderCallID)
	if err != nil {
		return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if len(scriptScenes) > 0 {
		if err := a.upsertSceneAssetLinks(ctx, input, scriptScenes, assets); err != nil {
			return ScriptAssetsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
	}
	output := ScriptAssetsOutput{
		ScriptID:        script.ID,
		ScriptVersionID: script.VersionID,
		Assets:          assets,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return ScriptAssetsOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateStoryboardFromScript(ctx context.Context, input GenerateStoryboardFromScriptInput) (ScriptStoryboardOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "script_to_storyboard", CreatedBy: input.CreatedBy}
	if err := validateScriptWorkflowInput(input.OrganizationID, input.ProjectID, input.WorkflowRunID, firstNonEmptyString(input.ScriptID, input.ScriptSceneID)); err != nil {
		return ScriptStoryboardOutput{}, err
	}
	if input.ScriptID == "" && input.ScriptSceneID != "" {
		scene, err := a.scriptSceneByID(ctx, input.ProjectID, input.ScriptSceneID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		input.ScriptID = scene.ScriptID
	}
	script, err := a.activeScript(ctx, input.ProjectID, input.ScriptID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if input.ScriptSceneID != "" {
		scene, err := a.scriptSceneByID(ctx, input.ProjectID, input.ScriptSceneID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		script, err = a.scriptForSceneParse(ctx, input.ProjectID, scene.ScriptID, scene.ScriptVersionID)
		if err != nil {
			return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	assets, err := a.listCanonicalAssets(ctx, input.ProjectID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	maxShots := input.MaxShots
	if maxShots <= 0 || maxShots > defaultMaxStoryboardShots {
		maxShots = defaultMaxStoryboardShots
	}
	scriptScenes, err := a.storyboardScenesForScript(ctx, input.ProjectID, script.VersionID, input.ScriptSceneID)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	scriptContent := script.Content
	if len(scriptScenes) > 0 {
		scriptContent = FormatScriptScenesForPrompt(scriptScenes)
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardFromScript, map[string]any{
		"project": project.asPromptVariables(),
		"script":  map[string]any{"id": script.ID, "versionId": script.VersionID, "content": scriptContent, "scenes": string(mustJSON(scriptScenes))},
		"assets":  map[string]any{"items": string(mustJSON(assets))},
		"input":   map[string]any{"maxShots": maxShots},
	})
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateStoryboardFromScript,
		NodeType:       "agent.storyboard_generate",
		Input: mustJSON(map[string]any{
			"scriptId":          input.ScriptID,
			"scriptVersionId":   script.VersionID,
			"maxShots":          maxShots,
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
	gatewayResp, err := a.gateway.GenerateText(ctx, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   project.ScriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json"}),
	})
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	storyboard, parseError := parseStoryboardText(gatewayResp.Output.Text)
	parsedShots, parseShotsErr := ParseStoryboardShots(storyboard)
	if parseShotsErr != nil && parseError == "" {
		parseError = parseShotsErr.Error()
	}
	normalizedShots := NormalizeStoryboardShotsWithLimit(parsedShots, scriptContent, maxShots)
	normalizedShots = assignScriptScenesToShots(normalizedShots, scriptScenes)
	requirements := NormalizeShotAssetRequirements(storyboard)
	storyboardValue := map[string]any{
		"storyboard":    storyboard,
		"rawText":       gatewayResp.Output.Text,
		"shots":         normalizedShots,
		"requirements":  requirements,
		"scriptId":      script.ID,
		"scriptVersion": script.VersionID,
		"scriptScenes":  scriptScenes,
	}
	if parseError != "" {
		storyboardValue["parseError"] = parseError
	}
	storageKey := fmt.Sprintf("org/%s/project/%s/workflow/%s/storyboard/script-storyboard.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID)
	put, err := a.storage.PutJSON(ctx, storageKey, storyboardValue)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	artifactID, shotRecords, requirementRecords, err := a.insertScriptStoryboardArtifactShotsAndRequirements(ctx, input, script, nodeRunID, put, gatewayResp, rendered.RenderedHash, normalizedShots, requirements)
	if err != nil {
		return ScriptStoryboardOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	output := ScriptStoryboardOutput{
		ScriptID:             script.ID,
		ScriptVersionID:      script.VersionID,
		StoryboardArtifactID: artifactID,
		StorageKey:           put.StorageKey,
		ProviderCallID:       gatewayResp.ProviderCallID,
		ModelID:              gatewayResp.ModelID,
		Storyboard:           storyboard,
		Shots:                shotRecords,
		Requirements:         requirementRecords,
		RawText:              gatewayResp.Output.Text,
		ParseError:           parseError,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return ScriptStoryboardOutput{}, err
	}
	return output, nil
}

func (a Activities) CompleteScriptAssetsWorkflow(ctx context.Context, input TextToStoryboardInput, output ScriptAssetsOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) CompleteScriptStoryboardWorkflow(ctx context.Context, input TextToStoryboardInput, output ScriptStoryboardOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) completeSimpleWorkflow(ctx context.Context, input TextToStoryboardInput, output any) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.run.completed", "workflow_run", input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func resolveScriptProductionOptions(raw json.RawMessage) ScriptProductionOptions {
	options := ScriptProductionOptions{MergeExisting: true, MaxShots: defaultMaxStoryboardShots}
	if len(raw) == 0 {
		return options
	}
	_ = json.Unmarshal(raw, &options)
	if options.MaxShots <= 0 || options.MaxShots > defaultMaxStoryboardShots {
		options.MaxShots = defaultMaxStoryboardShots
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
		key := asset.AssetType + "\x00" + strings.ToLower(asset.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, asset)
	}
	return out, nil
}

func NormalizeShotAssetRequirements(storyboard json.RawMessage) []ShotAssetRequirementRecord {
	var decoded struct {
		Shots []struct {
			ShotNo            int                          `json:"shotNo"`
			AssetRequirements []ShotAssetRequirementRecord `json:"assetRequirements"`
		} `json:"shots"`
	}
	if err := json.Unmarshal(storyboard, &decoded); err != nil {
		return nil
	}
	out := make([]ShotAssetRequirementRecord, 0)
	for _, shot := range decoded.Shots {
		for _, req := range shot.AssetRequirements {
			req.ShotNo = shot.ShotNo
			req.AssetType = normalizeAssetType(req.AssetType)
			req.AssetName = strings.TrimSpace(req.AssetName)
			req.RequirementType = strings.TrimSpace(req.RequirementType)
			if req.RequirementType == "" {
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
			if req.AssetType == "" || req.AssetName == "" {
				continue
			}
			out = append(out, req)
		}
	}
	return out
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
		"id":             p.ID,
		"projectType":    p.ProjectType,
		"contentType":    p.ContentType,
		"aspectRatio":    p.AspectRatio,
		"videoRatio":     p.VideoRatio,
		"artStyle":       p.ArtStyle,
		"directorManual": p.DirectorManual,
		"visualManual":   p.VisualManual,
		"imageQuality":   p.ImageQuality,
		"productionMode": p.ProductionMode,
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

func (a Activities) GenerateCanonicalAssetImage(ctx context.Context, input GenerateCanonicalAssetImageInput) (GenerateCanonicalAssetImageOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "canonical_asset_image", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.AssetID) == "" {
		return GenerateCanonicalAssetImageOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and assetId are required")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	asset, err := a.canonicalAssetByID(ctx, input.ProjectID, input.AssetID)
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyCanonicalAssetImage, map[string]any{
		"project": project.asPromptVariables(),
		"asset": map[string]any{
			"type":         asset.AssetType,
			"name":         asset.Name,
			"description":  asset.Description,
			"basePrompt":   asset.BasePrompt,
			"visualTraits": string(asset.VisualTraits),
		},
	})
	if err != nil {
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
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
	if _, err := a.db.Exec(ctx, `UPDATE canonical_assets SET status = 'image_running' WHERE id = $1`, input.AssetID); err != nil {
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
		NodeRunID:         nodeRunID,
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
	})
	if err != nil {
		_, _ = a.db.Exec(ctx, `UPDATE canonical_assets SET status = 'image_failed' WHERE id = $1`, input.AssetID)
		return GenerateCanonicalAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateCanonicalAssetImageOutput{
		AssetID:          input.AssetID,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ImageArtifactID:  gatewayResp.Output.ArtifactID,
		ImageMediaFileID: gatewayResp.Output.MediaFileID,
		ImageStorageKey:  gatewayResp.Output.StorageKey,
	}
	if err := a.completeCanonicalAssetImage(ctx, input, asset, rendered, output); err != nil {
		return GenerateCanonicalAssetImageOutput{}, err
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return GenerateCanonicalAssetImageOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateDerivedAssetImage(ctx context.Context, input GenerateDerivedAssetImageInput) (GenerateDerivedAssetImageOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "derived_asset_image", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.RequirementID) == "" {
		return GenerateDerivedAssetImageOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and requirementId are required")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	requirement, err := a.shotAssetRequirementByID(ctx, input.ProjectID, input.RequirementID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	asset, err := a.canonicalAssetByID(ctx, input.ProjectID, requirement.AssetID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	shot, err := a.storyboardShotByID(ctx, input.ProjectID, requirement.StoryboardShotID)
	if err != nil {
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
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
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
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
	if _, err := a.db.Exec(ctx, `UPDATE shot_asset_requirements SET status = 'image_running' WHERE id = $1`, input.RequirementID); err != nil {
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
		NodeRunID:         nodeRunID,
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
		_, _ = a.db.Exec(ctx, `UPDATE shot_asset_requirements SET status = 'image_failed' WHERE id = $1`, input.RequirementID)
		return GenerateDerivedAssetImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateDerivedAssetImageOutput{
		RequirementID:    input.RequirementID,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ImageArtifactID:  gatewayResp.Output.ArtifactID,
		ImageMediaFileID: gatewayResp.Output.MediaFileID,
		ImageStorageKey:  gatewayResp.Output.StorageKey,
	}
	if err := a.completeDerivedAssetImage(ctx, input, output); err != nil {
		return GenerateDerivedAssetImageOutput{}, err
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return GenerateDerivedAssetImageOutput{}, err
	}
	return output, nil
}
