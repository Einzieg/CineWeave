package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
)

const (
	nodeGenerateShotImagePrefix = "generate_shot_image"
	nodeCreateShotVideoPrefix   = "create_shot_video"
)

type ListStoryboardShotsInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
}

type GenerateShotImageInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	ShotID    string `json:"shotId"`
	ShotIndex int    `json:"shotIndex"`
	ShotNo    int    `json:"shotNo"`

	WorkflowPrompt string `json:"workflowPrompt"`
	AspectRatio    string `json:"aspectRatio"`
}

type GenerateShotImageOutput struct {
	NodeRunID        string `json:"nodeRunId"`
	ShotID           string `json:"shotId"`
	ProviderCallID   string `json:"providerCallId"`
	ImageArtifactID  string `json:"imageArtifactId"`
	ImageMediaFileID string `json:"imageMediaFileId"`
	ImageStorageKey  string `json:"imageStorageKey"`
}

type CreateShotVideoTaskInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	ShotID    string `json:"shotId"`
	ShotIndex int    `json:"shotIndex"`
	ShotNo    int    `json:"shotNo"`

	WorkflowPrompt string  `json:"workflowPrompt"`
	Duration       float64 `json:"duration"`
	AspectRatio    string  `json:"aspectRatio"`
	Resolution     string  `json:"resolution"`
}

type CreateShotVideoTaskOutput struct {
	NodeRunID           string `json:"nodeRunId"`
	ShotID              string `json:"shotId"`
	ProviderCallID      string `json:"providerCallId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	Status              string `json:"status"`
	ModelID             string `json:"modelId"`
}

type PollShotVideoTaskInput struct {
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	WorkflowRunID       string `json:"workflowRunId"`
	ShotID              string `json:"shotId"`
	ShotIndex           int    `json:"shotIndex"`
	ShotNo              int    `json:"shotNo"`
	NodeRunID           string `json:"nodeRunId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	PollCount           int    `json:"pollCount,omitempty"`
}

type PollShotVideoTaskOutput struct {
	ProviderCallID      string   `json:"providerCallId"`
	ProviderAsyncTaskID string   `json:"providerAsyncTaskId"`
	ExternalTaskID      string   `json:"externalTaskId,omitempty"`
	Status              string   `json:"status"`
	ArtifactID          string   `json:"artifactId,omitempty"`
	MediaFileID         string   `json:"mediaFileId,omitempty"`
	StorageKey          string   `json:"storageKey,omitempty"`
	MimeType            string   `json:"mimeType,omitempty"`
	DurationSeconds     *float64 `json:"durationSeconds,omitempty"`
	PollCount           int      `json:"pollCount,omitempty"`
}

type CancelShotVideoTaskInput struct {
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	WorkflowRunID       string `json:"workflowRunId"`
	ShotID              string `json:"shotId"`
	ShotIndex           int    `json:"shotIndex"`
	ShotNo              int    `json:"shotNo"`
	NodeRunID           string `json:"nodeRunId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

type CancelShotVideoTaskOutput struct {
	ProviderCallID      string `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	ShotID              string `json:"shotId,omitempty"`
	ShotIndex           int    `json:"shotIndex,omitempty"`
	ShotNo              int    `json:"shotNo,omitempty"`
	Status              string `json:"status"`
	ErrorMessage        string `json:"errorMessage,omitempty"`
}

type CreateStoryboardVideoTaskInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`

	StoryboardArtifactID string `json:"storyboardArtifactId"`
	ImageArtifactID      string `json:"imageArtifactId"`
	ImageMediaFileID     string `json:"imageMediaFileId"`
	ImageStorageKey      string `json:"imageStorageKey"`

	Prompt      string          `json:"prompt"`
	VideoPrompt string          `json:"videoPrompt"`
	Duration    float64         `json:"duration"`
	AspectRatio string          `json:"aspectRatio"`
	Resolution  string          `json:"resolution"`
	Storyboard  json.RawMessage `json:"storyboard"`
}

type CreateStoryboardVideoTaskOutput struct {
	NodeRunID           string `json:"nodeRunId"`
	ProviderCallID      string `json:"providerCallId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	Status              string `json:"status"`
	ModelID             string `json:"modelId"`
}

type PollStoryboardVideoTaskInput struct {
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	WorkflowRunID       string `json:"workflowRunId"`
	NodeRunID           string `json:"nodeRunId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	PollCount           int    `json:"pollCount,omitempty"`
}

type PollStoryboardVideoTaskOutput struct {
	ProviderCallID      string   `json:"providerCallId"`
	ProviderAsyncTaskID string   `json:"providerAsyncTaskId"`
	ExternalTaskID      string   `json:"externalTaskId,omitempty"`
	Status              string   `json:"status"`
	ArtifactID          string   `json:"artifactId,omitempty"`
	MediaFileID         string   `json:"mediaFileId,omitempty"`
	StorageKey          string   `json:"storageKey,omitempty"`
	MimeType            string   `json:"mimeType,omitempty"`
	DurationSeconds     *float64 `json:"durationSeconds,omitempty"`
	PollCount           int      `json:"pollCount,omitempty"`
}

type CancelStoryboardVideoTaskInput struct {
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	WorkflowRunID       string `json:"workflowRunId"`
	NodeRunID           string `json:"nodeRunId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

type CancelStoryboardVideoTaskOutput struct {
	ProviderCallID      string `json:"providerCallId,omitempty"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId,omitempty"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	Status              string `json:"status"`
	ErrorMessage        string `json:"errorMessage,omitempty"`
}

func (a Activities) ListStoryboardShots(ctx context.Context, input ListStoryboardShotsInput) ([]StoryboardShotRecord, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return nil, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	return a.listStoryboardShots(ctx, input.WorkflowRunID)
}

func (a Activities) GenerateShotImage(ctx context.Context, input GenerateShotImageInput) (GenerateShotImageOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.WorkflowPrompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return GenerateShotImageOutput{}, err
	}
	shot, err := a.storyboardShot(ctx, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return GenerateShotImageOutput{}, err
	}
	if shot.ImageArtifactID != "" && shot.ImageMediaFileID != "" && shot.ImageStorageKey != "" {
		return GenerateShotImageOutput{
			ShotID:           shot.ID,
			ImageArtifactID:  shot.ImageArtifactID,
			ImageMediaFileID: shot.ImageMediaFileID,
			ImageStorageKey:  shot.ImageStorageKey,
		}, nil
	}
	aspectRatio := strings.TrimSpace(input.AspectRatio)
	if aspectRatio == "" {
		var aspectErr error
		aspectRatio, aspectErr = a.projectAspectRatio(ctx, input.ProjectID)
		if aspectErr != nil {
			return GenerateShotImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: aspectErr.Error()})
		}
	}
	projectSettings, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "image_failed", "storyboard.shot.image.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "image_failed", "storyboard.shot.image.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	promptKey := promptKeyStoryboardImage
	modelProfileKey := imageGenerationModelProfileKey
	projectVariables := map[string]any{
		"id":             input.ProjectID,
		"aspectRatio":    aspectRatio,
		"videoRatio":     firstNonEmptyString(projectSettings.VideoRatio, aspectRatio),
		"artStyle":       projectSettings.ArtStyle,
		"directorManual": projectSettings.DirectorManual,
		"visualManual":   projectSettings.VisualManual,
	}
	if strings.TrimSpace(assetContext.RequirementsSummary) != "" {
		promptKey = promptKeyShotImage
		modelProfileKey = projectSettings.ImageModelProfileKey
		projectVariables = projectSettings.asPromptVariables()
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKey, map[string]any{
		"input":   map[string]any{"prompt": input.WorkflowPrompt},
		"project": projectVariables,
		"shot": map[string]any{
			"visual":      shot.Visual,
			"camera":      shot.Camera,
			"motion":      shot.Motion,
			"mood":        shot.Mood,
			"imagePrompt": shot.ImagePrompt,
		},
		"assets":       map[string]any{"summary": assetContext.AssetsSummary},
		"requirements": map[string]any{"summary": assetContext.RequirementsSummary},
	})
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "image_failed", "storyboard.shot.image.failed", err)
	}
	nodeKey := nodeKeyForShot(nodeGenerateShotImagePrefix, shot.ShotIndex)
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "image.generate",
		Input: mustJSON(map[string]any{
			"shotId":            shot.ID,
			"shotIndex":         shot.ShotIndex,
			"shotNo":            shot.ShotNo,
			"modelProfileKey":   modelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return GenerateShotImageOutput{}, err
	}
	if err := a.recordShotEvent(ctx, input.OrganizationID, input.ProjectID, "storyboard.shot.image.started", shot, "image_running"); err != nil {
		return GenerateShotImageOutput{}, err
	}
	if err := a.updateStoryboardShotStatus(ctx, shot.ID, "image_running"); err != nil {
		return GenerateShotImageOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, modelProfileKey, []string{"image", "multimodal"}); err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "image_failed", "storyboard.shot.image.failed", err)
	}
	if a.gateway == nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "image_failed", "storyboard.shot.image.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.GenerateImage(ctx, provider.GatewayImageRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   modelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustJSON(map[string]any{
			"prompt":  rendered.RenderedText,
			"size":    "1024x1024",
			"n":       1,
			"quality": projectSettings.ImageQuality,
		}),
		References: assetContext.ImageReferences,
	})
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "image_failed", "storyboard.shot.image.failed", workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateShotImageOutput{
		NodeRunID:        nodeRunID,
		ShotID:           shot.ID,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ImageArtifactID:  gatewayResp.Output.ArtifactID,
		ImageMediaFileID: gatewayResp.Output.MediaFileID,
		ImageStorageKey:  gatewayResp.Output.StorageKey,
	}
	if err := a.completeShotImage(ctx, input, shot, output); err != nil {
		return GenerateShotImageOutput{}, err
	}
	return output, nil
}

func (a Activities) CreateShotVideoTask(ctx context.Context, input CreateShotVideoTaskInput) (CreateShotVideoTaskOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.WorkflowPrompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	shot, err := a.storyboardShot(ctx, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	if shot.ImageArtifactID == "" || shot.ImageMediaFileID == "" || shot.ImageStorageKey == "" {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "shot image artifact/media/storage is required before video generation"})
	}
	if shot.VideoProviderAsyncTaskID != "" {
		return CreateShotVideoTaskOutput{
			ShotID:              shot.ID,
			ProviderAsyncTaskID: shot.VideoProviderAsyncTaskID,
			ExternalTaskID:      shot.VideoExternalTaskID,
			Status:              "running",
		}, nil
	}
	duration := input.Duration
	if duration <= 0 {
		duration = shot.Duration
	}
	if duration <= 0 {
		duration = defaultShotDuration
	}
	if duration > maxShotDuration {
		duration = maxShotDuration
	}
	aspectRatio := strings.TrimSpace(input.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	resolution := strings.TrimSpace(input.Resolution)
	if resolution == "" {
		resolution = "720p"
	}
	projectSettings, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "video_failed", "storyboard.shot.video.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "video_failed", "storyboard.shot.video.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	promptKey := promptKeyStoryboardVideo
	modelProfileKey := videoGenerationModelProfileKey
	if strings.TrimSpace(assetContext.RequirementsSummary) != "" {
		promptKey = promptKeyShotVideo
		modelProfileKey = projectSettings.VideoModelProfileKey
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKey, map[string]any{
		"input": map[string]any{"prompt": input.WorkflowPrompt},
		"shot": map[string]any{
			"visual":      shot.Visual,
			"camera":      shot.Camera,
			"motion":      shot.Motion,
			"mood":        shot.Mood,
			"videoPrompt": shot.VideoPrompt,
		},
		"video": map[string]any{
			"duration":    duration,
			"aspectRatio": aspectRatio,
			"resolution":  resolution,
		},
		"requirements": map[string]any{"summary": assetContext.RequirementsSummary},
	})
	if err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, "", "video_failed", "storyboard.shot.video.failed", err)
	}
	nodeKey := nodeKeyForShot(nodeCreateShotVideoPrefix, shot.ShotIndex)
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "video.create_task",
		Input: mustJSON(map[string]any{
			"shotId":            shot.ID,
			"shotIndex":         shot.ShotIndex,
			"shotNo":            shot.ShotNo,
			"imageArtifactId":   shot.ImageArtifactID,
			"imageMediaFileId":  shot.ImageMediaFileID,
			"imageStorageKey":   shot.ImageStorageKey,
			"duration":          duration,
			"aspectRatio":       aspectRatio,
			"resolution":        resolution,
			"modelProfileKey":   modelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, modelProfileKey, []string{"video", "multimodal"}); err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "video_failed", "storyboard.shot.video.failed", err)
	}
	if a.gateway == nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayResp, err := a.gateway.CreateVideoTask(ctx, provider.GatewayVideoCreateTaskRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   modelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		IdempotencyKey:    shotVideoTaskIdempotencyKey(input.WorkflowRunID, shot.ShotIndex),
		Input: mustJSON(map[string]any{
			"prompt":      rendered.RenderedText,
			"duration":    duration,
			"aspectRatio": aspectRatio,
			"resolution":  resolution,
			"mode":        "image_to_video",
		}),
		References: []provider.GatewayVideoReference{{
			Type:        "image",
			ArtifactID:  shot.ImageArtifactID,
			MediaFileID: shot.ImageMediaFileID,
			StorageKey:  shot.ImageStorageKey,
		}},
		Options: provider.GatewayVideoOptions{IdempotencyKey: shotVideoTaskIdempotencyKey(input.WorkflowRunID, shot.ShotIndex)},
	})
	if err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "video_failed", "storyboard.shot.video.failed", workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := CreateShotVideoTaskOutput{
		NodeRunID:           nodeRunID,
		ShotID:              shot.ID,
		ProviderCallID:      gatewayResp.ProviderCallID,
		ProviderAsyncTaskID: gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:      gatewayResp.ExternalTaskID,
		Status:              gatewayResp.Status,
		ModelID:             gatewayResp.ModelID,
	}
	if strings.TrimSpace(output.ProviderAsyncTaskID) == "" {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeRunID, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway did not return providerAsyncTaskId"})
	}
	if err := a.markShotVideoCreated(ctx, input, shot, output); err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	return output, nil
}

func (a Activities) PollShotVideoTask(ctx context.Context, input PollShotVideoTaskInput) (PollShotVideoTaskOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         "video polling",
	}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.NodeRunID) == "" {
		return PollShotVideoTaskOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and nodeRunId are required")
	}
	if strings.TrimSpace(input.ProviderAsyncTaskID) == "" {
		return PollShotVideoTaskOutput{}, fmt.Errorf("providerAsyncTaskId is required")
	}
	shot, err := a.storyboardShot(ctx, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return PollShotVideoTaskOutput{}, err
	}
	if a.gateway == nil {
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, input.NodeRunID, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayResp, err := a.gateway.PollVideoTask(ctx, provider.GatewayVideoPollTaskRequest{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		NodeRunID:           input.NodeRunID,
		ProviderAsyncTaskID: input.ProviderAsyncTaskID,
		ExternalTaskID:      input.ExternalTaskID,
	})
	if err != nil {
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, input.NodeRunID, "video_failed", "storyboard.shot.video.failed", workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := PollShotVideoTaskOutput{
		ProviderCallID:      gatewayResp.ProviderCallID,
		ProviderAsyncTaskID: gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:      gatewayResp.ExternalTaskID,
		Status:              gatewayResp.Status,
		ArtifactID:          gatewayResp.Output.ArtifactID,
		MediaFileID:         gatewayResp.Output.MediaFileID,
		StorageKey:          gatewayResp.Output.StorageKey,
		MimeType:            gatewayResp.Output.MimeType,
		DurationSeconds:     gatewayResp.Output.DurationSeconds,
		PollCount:           input.PollCount,
	}
	switch output.Status {
	case "queued", "running", "":
		if output.Status == "" {
			output.Status = "running"
		}
		if err := a.markShotVideoPolled(ctx, input, shot, output); err != nil {
			return PollShotVideoTaskOutput{}, err
		}
		return output, nil
	case "succeeded":
		if output.ArtifactID == "" || output.MediaFileID == "" || output.StorageKey == "" {
			return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, input.NodeRunID, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway video output is missing artifact/media/storage"})
		}
		if err := a.completeShotVideo(ctx, input, shot, output); err != nil {
			return PollShotVideoTaskOutput{}, err
		}
		return output, nil
	case "failed", "cancelled":
		status := "video_failed"
		if output.Status == "cancelled" {
			status = "cancelled"
		}
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, input.NodeRunID, status, "storyboard.shot.video.failed", workflowError{Code: codeActivityFailed, Message: "provider video task " + output.Status})
	default:
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, input.NodeRunID, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned unsupported video status: " + output.Status})
	}
}

func (a Activities) CancelShotVideoTask(ctx context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.NodeRunID) == "" {
		return CancelShotVideoTaskOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and nodeRunId are required")
	}
	if strings.TrimSpace(input.ProviderAsyncTaskID) == "" {
		return CancelShotVideoTaskOutput{}, fmt.Errorf("providerAsyncTaskId is required")
	}
	shot, err := a.storyboardShot(ctx, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return CancelShotVideoTaskOutput{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "Workflow cancellation requested"
	}
	output := CancelShotVideoTaskOutput{
		ProviderAsyncTaskID: input.ProviderAsyncTaskID,
		ExternalTaskID:      input.ExternalTaskID,
		ShotID:              shot.ID,
		ShotIndex:           shot.ShotIndex,
		ShotNo:              shot.ShotNo,
		Status:              "cancelled",
	}
	if a.gateway == nil {
		output.Status = "cancel_failed"
		output.ErrorMessage = "provider gateway client is not configured"
	} else {
		gatewayResp, err := a.gateway.CancelVideoTask(ctx, provider.GatewayVideoCancelTaskRequest{
			OrganizationID:      input.OrganizationID,
			ProviderAsyncTaskID: input.ProviderAsyncTaskID,
			ExternalTaskID:      input.ExternalTaskID,
		})
		if err != nil {
			output.Status = "cancel_failed"
			output.ErrorMessage = err.Error()
		} else {
			output.ProviderCallID = gatewayResp.ProviderCallID
			output.ProviderAsyncTaskID = firstNonEmptyString(gatewayResp.ProviderAsyncTaskID, input.ProviderAsyncTaskID)
			output.ExternalTaskID = firstNonEmptyString(gatewayResp.ExternalTaskID, input.ExternalTaskID)
			output.Status = firstNonEmptyString(gatewayResp.Status, "cancelled")
		}
	}
	if err := CancelNodeRun(ctx, a.db, input.NodeRunID, mustJSON(output), reason); err != nil {
		return CancelShotVideoTaskOutput{}, err
	}
	if err := a.cancelStoryboardShot(ctx, input, output); err != nil {
		return CancelShotVideoTaskOutput{}, err
	}
	return output, nil
}

func (a Activities) CreateStoryboardVideoTask(ctx context.Context, input CreateStoryboardVideoTaskInput) (CreateStoryboardVideoTaskOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.Prompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return CreateStoryboardVideoTaskOutput{}, err
	}
	if existing, ok, err := a.existingStoryboardVideoTask(ctx, input.WorkflowRunID); err != nil {
		return CreateStoryboardVideoTaskOutput{}, err
	} else if ok {
		return existing, nil
	}

	duration := input.Duration
	if duration <= 0 {
		duration = 5
	}
	aspectRatio := strings.TrimSpace(input.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	resolution := strings.TrimSpace(input.Resolution)
	if resolution == "" {
		resolution = "720p"
	}
	videoPrompt := strings.TrimSpace(input.VideoPrompt)
	if videoPrompt == "" {
		videoPrompt = selectVideoPrompt(input.Storyboard, input.Prompt, duration)
	}
	shot := firstStoryboardShot(input.Storyboard)
	if strings.TrimSpace(shot.VideoPrompt) == "" {
		shot.VideoPrompt = videoPrompt
	}
	if strings.TrimSpace(shot.Visual) == "" {
		shot.Visual = videoPrompt
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardVideo, map[string]any{
		"input": map[string]any{
			"prompt": input.Prompt,
		},
		"shot": map[string]any{
			"visual":      shot.Visual,
			"camera":      shot.Camera,
			"motion":      shot.Motion,
			"mood":        shot.Mood,
			"videoPrompt": shot.VideoPrompt,
		},
		"video": map[string]any{
			"duration":    duration,
			"aspectRatio": aspectRatio,
			"resolution":  resolution,
		},
	})
	if err != nil {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, "", err)
	}

	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateStoryboardVideoKey,
		NodeType:       "video.generate",
		Input: mustJSON(map[string]any{
			"storyboardArtifactId": input.StoryboardArtifactID,
			"imageArtifactId":      input.ImageArtifactID,
			"imageMediaFileId":     input.ImageMediaFileID,
			"imageStorageKey":      input.ImageStorageKey,
			"videoPrompt":          videoPrompt,
			"duration":             duration,
			"aspectRatio":          aspectRatio,
			"resolution":           resolution,
			"modelProfileKey":      videoGenerationModelProfileKey,
			"promptTemplateKey":    rendered.TemplateKey,
			"promptVersionId":      rendered.PromptVersionID,
			"promptHash":           rendered.RenderedHash,
			"promptSource":         rendered.Source,
		}),
	})
	if err != nil {
		return CreateStoryboardVideoTaskOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, videoGenerationModelProfileKey, []string{"video", "multimodal"}); err != nil {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.CreateVideoTask(ctx, provider.GatewayVideoCreateTaskRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   videoGenerationModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		IdempotencyKey:    videoTaskIdempotencyKey(input.WorkflowRunID),
		Input: mustJSON(map[string]any{
			"prompt":      rendered.RenderedText,
			"duration":    duration,
			"aspectRatio": aspectRatio,
			"resolution":  resolution,
			"mode":        "image_to_video",
		}),
		References: []provider.GatewayVideoReference{
			{
				Type:        "image",
				ArtifactID:  input.ImageArtifactID,
				MediaFileID: input.ImageMediaFileID,
				StorageKey:  input.ImageStorageKey,
			},
		},
		Options: provider.GatewayVideoOptions{IdempotencyKey: videoTaskIdempotencyKey(input.WorkflowRunID)},
	})
	if err != nil {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := CreateStoryboardVideoTaskOutput{
		NodeRunID:           nodeRunID,
		ProviderCallID:      gatewayResp.ProviderCallID,
		ProviderAsyncTaskID: gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:      gatewayResp.ExternalTaskID,
		Status:              gatewayResp.Status,
		ModelID:             gatewayResp.ModelID,
	}
	if strings.TrimSpace(output.ProviderAsyncTaskID) == "" {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway did not return providerAsyncTaskId"})
	}
	if err := ProgressNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return CreateStoryboardVideoTaskOutput{}, err
	}
	return output, nil
}

func (a Activities) CancelStoryboardVideoTask(ctx context.Context, input CancelStoryboardVideoTaskInput) (CancelStoryboardVideoTaskOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.NodeRunID) == "" {
		return CancelStoryboardVideoTaskOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and nodeRunId are required")
	}
	if strings.TrimSpace(input.ProviderAsyncTaskID) == "" {
		return CancelStoryboardVideoTaskOutput{}, fmt.Errorf("providerAsyncTaskId is required")
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "Workflow cancellation requested"
	}
	output := CancelStoryboardVideoTaskOutput{
		ProviderAsyncTaskID: input.ProviderAsyncTaskID,
		ExternalTaskID:      input.ExternalTaskID,
		Status:              "cancelled",
	}
	if a.gateway == nil {
		output.Status = "cancel_failed"
		output.ErrorMessage = "provider gateway client is not configured"
		_ = CancelNodeRun(ctx, a.db, input.NodeRunID, mustJSON(output), reason)
		_ = a.recordProviderVideoCancelEvent(ctx, input, output)
		return output, nil
	}
	gatewayResp, err := a.gateway.CancelVideoTask(ctx, provider.GatewayVideoCancelTaskRequest{
		OrganizationID:      input.OrganizationID,
		ProviderAsyncTaskID: input.ProviderAsyncTaskID,
		ExternalTaskID:      input.ExternalTaskID,
	})
	if err != nil {
		output.Status = "cancel_failed"
		output.ErrorMessage = err.Error()
	} else {
		output.ProviderCallID = gatewayResp.ProviderCallID
		output.ProviderAsyncTaskID = firstNonEmptyString(gatewayResp.ProviderAsyncTaskID, input.ProviderAsyncTaskID)
		output.ExternalTaskID = firstNonEmptyString(gatewayResp.ExternalTaskID, input.ExternalTaskID)
		output.Status = firstNonEmptyString(gatewayResp.Status, "cancelled")
	}
	if err := CancelNodeRun(ctx, a.db, input.NodeRunID, mustJSON(output), reason); err != nil {
		return CancelStoryboardVideoTaskOutput{}, err
	}
	if err := a.recordProviderVideoCancelEvent(ctx, input, output); err != nil {
		return CancelStoryboardVideoTaskOutput{}, err
	}
	return output, nil
}

func (a Activities) PollStoryboardVideoTask(ctx context.Context, input PollStoryboardVideoTaskInput) (PollStoryboardVideoTaskOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         "video polling",
	}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.NodeRunID) == "" {
		return PollStoryboardVideoTaskOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and nodeRunId are required")
	}
	if strings.TrimSpace(input.ProviderAsyncTaskID) == "" {
		return PollStoryboardVideoTaskOutput{}, fmt.Errorf("providerAsyncTaskId is required")
	}
	if a.gateway == nil {
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, input.NodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.PollVideoTask(ctx, provider.GatewayVideoPollTaskRequest{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		NodeRunID:           input.NodeRunID,
		ProviderAsyncTaskID: input.ProviderAsyncTaskID,
		ExternalTaskID:      input.ExternalTaskID,
	})
	if err != nil {
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, input.NodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := PollStoryboardVideoTaskOutput{
		ProviderCallID:      gatewayResp.ProviderCallID,
		ProviderAsyncTaskID: gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:      gatewayResp.ExternalTaskID,
		Status:              gatewayResp.Status,
		ArtifactID:          gatewayResp.Output.ArtifactID,
		MediaFileID:         gatewayResp.Output.MediaFileID,
		StorageKey:          gatewayResp.Output.StorageKey,
		MimeType:            gatewayResp.Output.MimeType,
		DurationSeconds:     gatewayResp.Output.DurationSeconds,
		PollCount:           input.PollCount,
	}

	switch output.Status {
	case "queued", "running", "":
		if output.Status == "" {
			output.Status = "running"
		}
		if err := ProgressNodeRun(ctx, a.db, input.NodeRunID, mustJSON(output)); err != nil {
			return PollStoryboardVideoTaskOutput{}, err
		}
		return output, nil
	case "succeeded":
		if output.ArtifactID == "" || output.MediaFileID == "" || output.StorageKey == "" {
			return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, input.NodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway video output is missing artifact/media/storage"})
		}
		if err := a.completeStoryboardVideoNode(ctx, input, output); err != nil {
			return PollStoryboardVideoTaskOutput{}, err
		}
		return output, nil
	case "failed", "cancelled":
		code := codeActivityFailed
		if output.Status == "cancelled" {
			code = "PROVIDER_VIDEO_CANCELLED"
		}
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, input.NodeRunID, workflowError{Code: code, Message: "provider video task " + output.Status})
	default:
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, input.NodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned unsupported video status: " + output.Status})
	}
}

func (a Activities) CompleteVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, output VideoProductionOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	tag, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
		  AND status NOT IN ('failed', 'cancelled')
	`, input.WorkflowRunID, outputJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.run.completed", "workflow_run", input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) FailVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, nodeRunID, code, message string) error {
	if strings.TrimSpace(nodeRunID) != "" {
		if err := FailNodeRun(ctx, a.db, nodeRunID, code, message); err != nil {
			return err
		}
	}
	return a.markWorkflowFailed(ctx, input, code, message)
}

func (a Activities) CancelVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, output CancelShotVideoTaskOutput, reason string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'cancelled', updated_at = now()
		WHERE workflow_run_id = $1
		  AND status NOT IN ('video_succeeded', 'video_failed', 'cancelled')
	`, input.WorkflowRunID); err != nil {
		return err
	}
	runOutput := mustJSON(map[string]any{
		"providerAsyncTaskId": output.ProviderAsyncTaskID,
		"externalTaskId":      output.ExternalTaskID,
		"providerCallId":      output.ProviderCallID,
		"shotId":              output.ShotID,
		"shotIndex":           output.ShotIndex,
		"shotNo":              output.ShotNo,
		"status":              "cancelled",
		"videoCancelStatus":   output.Status,
		"errorMessage":        output.ErrorMessage,
	})
	runCtx, err := lockWorkflowRunContext(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelled',
		    output = $2,
		    error_code = 'USER_CANCELLED',
		    error_message = $3,
		    completed_at = now(),
		    cancelled_at = COALESCE(cancelled_at, now())
		WHERE id = $1
		  AND status NOT IN ('succeeded', 'failed', 'cancelled')
	`, input.WorkflowRunID, runOutput, nullableCancelReason(reason)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.run.cancelled", "workflow_run", input.WorkflowRunID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"reason":        reason,
		"status":        "cancelled",
		"output":        json.RawMessage(runOutput),
	})); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		UPDATE workflow_node_runs
		SET status = 'cancelled',
		    output = COALESCE(output, '{}'::jsonb) || $2::jsonb,
		    error_code = 'USER_CANCELLED',
		    error_message = $3,
		    completed_at = now()
		WHERE workflow_run_id = $1
		  AND node_key = $4
		  AND status NOT IN ('succeeded', 'failed', 'cancelled')
		RETURNING id::text
	`, input.WorkflowRunID, runOutput, nullableCancelReason(reason), nodeComposeFinalVideoKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeRunID string
		if err := rows.Scan(&nodeRunID); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.cancelled", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     nodeRunID,
			"nodeKey":       nodeComposeFinalVideoKey,
			"reason":        reason,
			"status":        "cancelled",
			"output":        json.RawMessage(runOutput),
		})); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func BuildVideoProductionOutput(storyboard GenerateStoryboardTextOutput, image GenerateStoryboardImageOutput, create CreateStoryboardVideoTaskOutput, poll PollStoryboardVideoTaskOutput) VideoProductionOutput {
	return VideoProductionOutput{
		StoryboardArtifactID: storyboard.StoryboardArtifactID,
		ImageArtifactID:      image.ImageArtifactID,
		ImageMediaFileID:     image.ImageMediaFileID,
		ImageStorageKey:      image.ImageStorageKey,
		VideoArtifactID:      poll.ArtifactID,
		VideoMediaFileID:     poll.MediaFileID,
		VideoStorageKey:      poll.StorageKey,
		ProviderAsyncTaskID:  create.ProviderAsyncTaskID,
		ExternalTaskID:       firstNonEmptyString(poll.ExternalTaskID, create.ExternalTaskID),
		ProviderCalls: VideoProductionProviderCalls{
			Storyboard:   storyboard.ProviderCallID,
			Image:        image.ProviderCallID,
			VideoCreate:  create.ProviderCallID,
			VideoPoll:    poll.ProviderCallID,
			Images:       compactStrings([]string{image.ProviderCallID}),
			VideoCreates: compactStrings([]string{create.ProviderCallID}),
			VideoPolls:   compactStrings([]string{poll.ProviderCallID}),
		},
	}
}

func BuildMultiShotVideoProductionOutput(storyboard GenerateStoryboardTextOutput, shots []VideoProductionShotOutput, providerCalls VideoProductionProviderCalls) VideoProductionOutput {
	output := VideoProductionOutput{
		StoryboardArtifactID: storyboard.StoryboardArtifactID,
		Shots:                shots,
		ProviderCalls:        providerCalls,
	}
	if output.ProviderCalls.Storyboard == "" {
		output.ProviderCalls.Storyboard = storyboard.ProviderCallID
	}
	if len(shots) > 0 {
		first := shots[0]
		output.ImageArtifactID = first.ImageArtifactID
		output.ImageMediaFileID = first.ImageMediaFileID
		output.ImageStorageKey = first.ImageStorageKey
		output.VideoArtifactID = first.VideoArtifactID
		output.VideoMediaFileID = first.VideoMediaFileID
		output.VideoStorageKey = first.VideoStorageKey
		output.ProviderAsyncTaskID = first.ProviderAsyncTaskID
		output.ExternalTaskID = first.ExternalTaskID
	}
	return output
}

func (a Activities) listStoryboardShots(ctx context.Context, workflowRunID string) ([]StoryboardShotRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT
			id::text,
			COALESCE(workflow_run_id::text, ''),
			shot_index,
			COALESCE(shot_no, shot_index + 1),
			COALESCE(title, ''),
			COALESCE(duration_seconds, 0)::float8,
			COALESCE(visual, ''),
			COALESCE(camera, ''),
			COALESCE(motion, ''),
			COALESCE(mood, ''),
			COALESCE(image_prompt, ''),
			COALESCE(video_prompt, ''),
			COALESCE(image_artifact_id::text, ''),
			COALESCE(image_media_file_id::text, ''),
			COALESCE(image_storage_key, ''),
			COALESCE(video_artifact_id::text, ''),
			COALESCE(video_media_file_id::text, ''),
			COALESCE(video_storage_key, ''),
			COALESCE(video_provider_async_task_id::text, ''),
			COALESCE(video_external_task_id, ''),
			COALESCE(status, 'pending')
		FROM storyboard_shots
		WHERE workflow_run_id = $1
		ORDER BY shot_index ASC
	`, workflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shots := make([]StoryboardShotRecord, 0)
	for rows.Next() {
		shot, err := scanStoryboardShotRecord(rows)
		if err != nil {
			return nil, err
		}
		shots = append(shots, shot)
	}
	return shots, rows.Err()
}

func (a Activities) storyboardShot(ctx context.Context, workflowRunID, shotID string, shotIndex int) (StoryboardShotRecord, error) {
	args := []any{workflowRunID}
	where := `workflow_run_id = $1`
	if strings.TrimSpace(shotID) != "" {
		where += ` AND id = $2`
		args = append(args, shotID)
	} else {
		where += ` AND shot_index = $2`
		args = append(args, shotIndex)
	}
	return scanStoryboardShotRecord(a.db.QueryRow(ctx, `
		SELECT
			id::text,
			COALESCE(workflow_run_id::text, ''),
			shot_index,
			COALESCE(shot_no, shot_index + 1),
			COALESCE(title, ''),
			COALESCE(duration_seconds, 0)::float8,
			COALESCE(visual, ''),
			COALESCE(camera, ''),
			COALESCE(motion, ''),
			COALESCE(mood, ''),
			COALESCE(image_prompt, ''),
			COALESCE(video_prompt, ''),
			COALESCE(image_artifact_id::text, ''),
			COALESCE(image_media_file_id::text, ''),
			COALESCE(image_storage_key, ''),
			COALESCE(video_artifact_id::text, ''),
			COALESCE(video_media_file_id::text, ''),
			COALESCE(video_storage_key, ''),
			COALESCE(video_provider_async_task_id::text, ''),
			COALESCE(video_external_task_id, ''),
			COALESCE(status, 'pending')
		FROM storyboard_shots
		WHERE `+where+`
	`, args...))
}

func scanStoryboardShotRecord(row pgx.Row) (StoryboardShotRecord, error) {
	var shot StoryboardShotRecord
	err := row.Scan(
		&shot.ID,
		&shot.WorkflowRunID,
		&shot.ShotIndex,
		&shot.ShotNo,
		&shot.Title,
		&shot.Duration,
		&shot.Visual,
		&shot.Camera,
		&shot.Motion,
		&shot.Mood,
		&shot.ImagePrompt,
		&shot.VideoPrompt,
		&shot.ImageArtifactID,
		&shot.ImageMediaFileID,
		&shot.ImageStorageKey,
		&shot.VideoArtifactID,
		&shot.VideoMediaFileID,
		&shot.VideoStorageKey,
		&shot.VideoProviderAsyncTaskID,
		&shot.VideoExternalTaskID,
		&shot.Status,
	)
	return shot, err
}

func (a Activities) updateStoryboardShotStatus(ctx context.Context, shotID, status string) error {
	_, err := a.db.Exec(ctx, `UPDATE storyboard_shots SET status = $2, updated_at = now() WHERE id = $1`, shotID, status)
	return err
}

func (a Activities) recordShotEvent(ctx context.Context, organizationID, projectID, eventType string, shot StoryboardShotRecord, status string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := insertEvent(ctx, tx, organizationID, projectID, eventType, "storyboard_shot", shot.ID, storyboardShotEventPayload(shot.WorkflowRunID, shot, status)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) failShotActivity(ctx context.Context, input TextToStoryboardInput, shot StoryboardShotRecord, nodeRunID, status, eventType string, cause error) error {
	code, message := workflowErrorFields(cause, codeActivityFailed)
	if strings.TrimSpace(shot.ID) != "" {
		_ = a.updateStoryboardShotStatus(ctx, shot.ID, status)
		_ = a.recordShotEvent(ctx, input.OrganizationID, input.ProjectID, eventType, shot, status)
	}
	if strings.TrimSpace(nodeRunID) != "" {
		_ = FailNodeRun(ctx, a.db, nodeRunID, code, message)
	}
	_ = a.markWorkflowFailed(ctx, input, code, message)
	return temporal.NewApplicationError(message, code)
}

func (a Activities) completeShotImage(ctx context.Context, input GenerateShotImageInput, shot StoryboardShotRecord, output GenerateShotImageOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_artifact_id = $2,
		    image_media_file_id = $3,
		    image_storage_key = NULLIF($4, ''),
		    status = 'image_succeeded',
		    updated_at = now()
		WHERE id = $1
	`, shot.ID, nullIfEmpty(output.ImageArtifactID), nullIfEmpty(output.ImageMediaFileID), output.ImageStorageKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, output.NodeRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.node.completed", "workflow_node_run", output.NodeRunID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"nodeKey":       nodeKeyForShot(nodeGenerateShotImagePrefix, shot.ShotIndex),
		"output":        json.RawMessage(outputJSON),
	})); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image.completed", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, StoryboardShotRecord{
		ID:        shot.ID,
		ShotIndex: shot.ShotIndex,
		ShotNo:    shot.ShotNo,
		Status:    "image_succeeded",
	}, "image_succeeded")); err != nil {
		return err
	}
	if strings.TrimSpace(output.ImageArtifactID) != "" {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", output.ImageArtifactID, mustJSON(map[string]any{
			"artifactId":    output.ImageArtifactID,
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     output.NodeRunID,
			"shotId":        shot.ID,
			"shotIndex":     shot.ShotIndex,
			"storageKey":    output.ImageStorageKey,
			"type":          "generated_image",
			"mediaFileId":   output.ImageMediaFileID,
		})); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a Activities) markShotVideoCreated(ctx context.Context, input CreateShotVideoTaskInput, shot StoryboardShotRecord, output CreateShotVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_provider_async_task_id = $2,
		    video_external_task_id = NULLIF($3, ''),
		    status = 'video_running',
		    updated_at = now()
		WHERE id = $1
	`, shot.ID, nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'running', output = $2
		WHERE id = $1
	`, output.NodeRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.created", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, StoryboardShotRecord{
		ID:        shot.ID,
		ShotIndex: shot.ShotIndex,
		ShotNo:    shot.ShotNo,
		Status:    "video_running",
	}, "video_running")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) markShotVideoPolled(ctx context.Context, input PollShotVideoTaskInput, shot StoryboardShotRecord, output PollShotVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'video_running', updated_at = now()
		WHERE id = $1
	`, shot.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'running', output = $2
		WHERE id = $1
	`, input.NodeRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.node.progress", "workflow_node_run", input.NodeRunID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"nodeKey":       nodeKeyForShot(nodeCreateShotVideoPrefix, shot.ShotIndex),
		"output":        json.RawMessage(outputJSON),
	})); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.polled", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, "video_running")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) completeShotVideo(ctx context.Context, input PollShotVideoTaskInput, shot StoryboardShotRecord, output PollShotVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_artifact_id = $2,
		    video_media_file_id = $3,
		    video_storage_key = NULLIF($4, ''),
		    video_provider_async_task_id = $5,
		    video_external_task_id = NULLIF($6, ''),
		    status = 'video_succeeded',
		    updated_at = now()
		WHERE id = $1
	`, shot.ID, nullIfEmpty(output.ArtifactID), nullIfEmpty(output.MediaFileID), output.StorageKey, nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, input.NodeRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.node.completed", "workflow_node_run", input.NodeRunID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"nodeKey":       nodeKeyForShot(nodeCreateShotVideoPrefix, shot.ShotIndex),
		"output":        json.RawMessage(outputJSON),
	})); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.completed", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, StoryboardShotRecord{
		ID:        shot.ID,
		ShotIndex: shot.ShotIndex,
		ShotNo:    shot.ShotNo,
		Status:    "video_succeeded",
	}, "video_succeeded")); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", output.ArtifactID, mustJSON(map[string]any{
		"artifactId":          output.ArtifactID,
		"workflowRunId":       input.WorkflowRunID,
		"nodeRunId":           input.NodeRunID,
		"shotId":              shot.ID,
		"shotIndex":           shot.ShotIndex,
		"storageKey":          output.StorageKey,
		"type":                "generated_video",
		"mediaFileId":         output.MediaFileID,
		"providerAsyncTaskId": output.ProviderAsyncTaskID,
		"externalTaskId":      output.ExternalTaskID,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) cancelStoryboardShot(ctx context.Context, input CancelShotVideoTaskInput, output CancelShotVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'cancelled',
		    video_provider_async_task_id = COALESCE($2, video_provider_async_task_id),
		    video_external_task_id = COALESCE(NULLIF($3, ''), video_external_task_id),
		    updated_at = now()
		WHERE id = $1
	`, input.ShotID, nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.cancelled", "storyboard_shot", input.ShotID, mustJSON(map[string]any{
		"workflowRunId":       input.WorkflowRunID,
		"shotId":              input.ShotID,
		"shotIndex":           input.ShotIndex,
		"shotNo":              input.ShotNo,
		"status":              "cancelled",
		"providerAsyncTaskId": output.ProviderAsyncTaskID,
		"externalTaskId":      output.ExternalTaskID,
		"providerCallId":      output.ProviderCallID,
		"errorMessage":        output.ErrorMessage,
	})); err != nil {
		return err
	}
	eventType := "provider.video.task.cancelled"
	if output.Status == "cancel_failed" {
		eventType = "provider.video.task.cancel_failed"
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, eventType, "provider_async_task", output.ProviderAsyncTaskID, mustJSON(map[string]any{
		"workflowRunId":       input.WorkflowRunID,
		"nodeRunId":           input.NodeRunID,
		"shotId":              input.ShotID,
		"shotIndex":           input.ShotIndex,
		"shotNo":              input.ShotNo,
		"providerAsyncTaskId": output.ProviderAsyncTaskID,
		"externalTaskId":      output.ExternalTaskID,
		"providerCallId":      output.ProviderCallID,
		"reason":              input.Reason,
		"status":              output.Status,
		"errorMessage":        output.ErrorMessage,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) recordProviderVideoCancelEvent(ctx context.Context, input CancelStoryboardVideoTaskInput, output CancelStoryboardVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	eventType := "provider.video.task.cancelled"
	if output.Status == "cancel_failed" {
		eventType = "provider.video.task.cancel_failed"
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, eventType, "provider_async_task", input.ProviderAsyncTaskID, mustJSON(map[string]any{
		"workflowRunId":       input.WorkflowRunID,
		"nodeRunId":           input.NodeRunID,
		"providerAsyncTaskId": output.ProviderAsyncTaskID,
		"externalTaskId":      output.ExternalTaskID,
		"providerCallId":      output.ProviderCallID,
		"reason":              input.Reason,
		"status":              output.Status,
		"errorMessage":        output.ErrorMessage,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) existingStoryboardVideoTask(ctx context.Context, workflowRunID string) (CreateStoryboardVideoTaskOutput, bool, error) {
	var output CreateStoryboardVideoTaskOutput
	var raw json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT id::text, COALESCE(output, '{}'::jsonb)
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND node_key = $2
		  AND status IN ('running', 'succeeded')
	`, workflowRunID, nodeGenerateStoryboardVideoKey).Scan(&output.NodeRunID, &raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateStoryboardVideoTaskOutput{}, false, nil
		}
		return CreateStoryboardVideoTaskOutput{}, false, err
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		return CreateStoryboardVideoTaskOutput{}, false, err
	}
	if output.NodeRunID == "" {
		return CreateStoryboardVideoTaskOutput{}, false, nil
	}
	return output, strings.TrimSpace(output.ProviderAsyncTaskID) != "", nil
}

func (a Activities) completeStoryboardVideoNode(ctx context.Context, input PollStoryboardVideoTaskInput, output PollStoryboardVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeRunContext(ctx, tx, input.NodeRunID); err != nil {
		return err
	}
	outputJSON := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, input.NodeRunID, outputJSON); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.node.completed", "workflow_node_run", input.NodeRunID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"nodeKey":       nodeGenerateStoryboardVideoKey,
		"output":        json.RawMessage(outputJSON),
	})); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", output.ArtifactID, mustJSON(map[string]any{
		"artifactId":          output.ArtifactID,
		"workflowRunId":       input.WorkflowRunID,
		"nodeRunId":           input.NodeRunID,
		"storageKey":          output.StorageKey,
		"type":                "generated_video",
		"mediaFileId":         output.MediaFileID,
		"providerAsyncTaskId": output.ProviderAsyncTaskID,
		"externalTaskId":      output.ExternalTaskID,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func videoTaskIdempotencyKey(workflowRunID string) string {
	return workflowRunID + ":" + nodeGenerateStoryboardVideoKey
}

func shotVideoTaskIdempotencyKey(workflowRunID string, shotIndex int) string {
	return fmt.Sprintf("%s:%s:%d", workflowRunID, nodeCreateShotVideoPrefix, shotIndex)
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
