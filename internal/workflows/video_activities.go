package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videocontracts"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
)

const (
	nodeGenerateShotImagePrefix = "generate_shot_image"
	nodeCreateShotVideoPrefix   = "create_shot_video"
)

func storyboardDialogueToGatewaySpans(lines []StoryboardDialogueLine) []provider.GatewayVideoDialogueSpan {
	lines = SpokenStoryboardDialogue(lines)
	result := make([]provider.GatewayVideoDialogueSpan, 0, len(lines))
	for _, line := range lines {
		result = append(result, provider.GatewayVideoDialogueSpan{
			TimingUnitID: line.TimingUnitID, Speaker: line.Speaker, Text: line.Text,
			Delivery: line.Delivery, Kind: line.Kind,
			StartTick: line.SpanStartTick, EndTick: line.SpanEndTick,
			ContinuesFromPrevious: line.ContinuesFromPrevious,
			ContinuesToNext:       line.ContinuesToNext,
		})
	}
	return result
}

var storyboardImageSizesByAspectRatio = map[string]string{
	"1:1":  "1024x1024",
	"2:3":  "1024x1536",
	"3:2":  "1536x1024",
	"4:3":  "1024x768",
	"3:4":  "768x1024",
	"5:4":  "1280x1024",
	"4:5":  "1024x1280",
	"16:9": "1536x864",
	"9:16": "864x1536",
	"2:1":  "2048x1024",
	"1:2":  "1024x2048",
	"21:9": "2016x864",
	"9:21": "864x2016",
	"7:4":  "1792x1024",
	"4:7":  "1024x1792",
}

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
	FailureScope   string `json:"failureScope,omitempty"`

	ShotID     string `json:"shotId"`
	ShotIndex  int    `json:"shotIndex"`
	ShotNo     int    `json:"shotNo"`
	AnchorRole string `json:"anchorRole,omitempty"`
	ProfileKey string `json:"profileKey,omitempty"`

	WorkflowPrompt string `json:"workflowPrompt"`
	AspectRatio    string `json:"aspectRatio"`
	Force          bool   `json:"force,omitempty"`
}

type GenerateShotImageOutput struct {
	NodeRunID         string `json:"nodeRunId"`
	ExecutionToken    string `json:"executionToken"`
	AttemptGeneration int    `json:"attemptGeneration"`
	ShotID            string `json:"shotId"`
	AnchorRole        string `json:"anchorRole"`
	VisualAnchorID    string `json:"visualAnchorId,omitempty"`
	ProviderCallID    string `json:"providerCallId"`
	ImageArtifactID   string `json:"imageArtifactId"`
	ImageMediaFileID  string `json:"imageMediaFileId"`
	ImageStorageKey   string `json:"imageStorageKey"`
}

type CreateShotVideoTaskInput struct {
	OrganizationID       string `json:"organizationId"`
	ProjectID            string `json:"projectId"`
	WorkflowRunID        string `json:"workflowRunId"`
	CreatedBy            string `json:"createdBy"`
	OperationID          string `json:"operationId,omitempty"`
	OperationItemID      string `json:"operationItemId,omitempty"`
	OperationItemAttempt int    `json:"operationItemAttempt,omitempty"`

	ShotID    string `json:"shotId"`
	ShotIndex int    `json:"shotIndex"`
	ShotNo    int    `json:"shotNo"`

	WorkflowPrompt                 string                          `json:"workflowPrompt"`
	FailureScope                   string                          `json:"failureScope,omitempty"`
	Duration                       float64                         `json:"duration"`
	PlannedDuration                float64                         `json:"plannedDuration,omitempty"`
	AspectRatio                    string                          `json:"aspectRatio"`
	Resolution                     string                          `json:"resolution"`
	Force                          bool                            `json:"force,omitempty"`
	ExecutionPlanID                string                          `json:"executionPlanId,omitempty"`
	RenderSegmentID                string                          `json:"renderSegmentId,omitempty"`
	CapabilitySnapshotHash         string                          `json:"capabilitySnapshotHash,omitempty"`
	InputContractKey               string                          `json:"inputContractKey,omitempty"`
	InputContractHash              string                          `json:"inputContractHash,omitempty"`
	SegmentIndex                   int                             `json:"segmentIndex,omitempty"`
	SegmentCount                   int                             `json:"segmentCount,omitempty"`
	RetryGeneration                int                             `json:"retryGeneration,omitempty"`
	SegmentStartTick               int64                           `json:"segmentStartTick,omitempty"`
	SegmentEndTick                 int64                           `json:"segmentEndTick,omitempty"`
	DialogueLines                  []StoryboardDialogueLine        `json:"dialogueLines,omitempty"`
	AudioStrategy                  string                          `json:"audioStrategy,omitempty"`
	AudioRequirement               string                          `json:"audioRequirement,omitempty"`
	ContinuityMode                 string                          `json:"continuityMode,omitempty"`
	PreviousSegmentArtifactID      string                          `json:"previousSegmentArtifactId,omitempty"`
	PreviousSegmentRenderSegmentID string                          `json:"previousSegmentRenderSegmentId,omitempty"`
	PreviousSegmentMediaFileID     string                          `json:"previousSegmentMediaFileId,omitempty"`
	PreviousSegmentStorageKey      string                          `json:"previousSegmentStorageKey,omitempty"`
	PreviousSegmentTailFrame       *ShotSegmentTailAnchorReference `json:"previousSegmentTailFrame,omitempty"`

	Prompt                   string `json:"prompt"`
	NegativePrompt           string `json:"negativePrompt,omitempty"`
	PromptHash               string `json:"promptHash,omitempty"`
	GenerationProviderCallID string `json:"generationProviderCallId,omitempty"`
	ReviewProviderCallID     string `json:"reviewProviderCallId,omitempty"`
	ReviewTemplateKey        string `json:"reviewTemplateKey,omitempty"`
	ReviewPromptVersionID    string `json:"reviewPromptVersionId,omitempty"`
}

type CreateShotVideoTaskOutput struct {
	NodeRunID           string `json:"nodeRunId"`
	ExecutionToken      string `json:"executionToken"`
	AttemptGeneration   int    `json:"attemptGeneration"`
	ShotID              string `json:"shotId"`
	ProviderCallID      string `json:"providerCallId"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	Status              string `json:"status"`
	ModelID             string `json:"modelId"`
	ExecutionPlanID     string `json:"executionPlanId,omitempty"`
	RenderSegmentID     string `json:"renderSegmentId,omitempty"`
	SegmentIndex        int    `json:"segmentIndex,omitempty"`
	SegmentCount        int    `json:"segmentCount,omitempty"`
	RetryGeneration     int    `json:"retryGeneration,omitempty"`
	ErrorCode           string `json:"errorCode,omitempty"`
	ErrorMessage        string `json:"errorMessage,omitempty"`
}

type PollShotVideoTaskInput struct {
	OrganizationID      string `json:"organizationId"`
	ProjectID           string `json:"projectId"`
	WorkflowRunID       string `json:"workflowRunId"`
	FailureScope        string `json:"failureScope,omitempty"`
	ShotID              string `json:"shotId"`
	ShotIndex           int    `json:"shotIndex"`
	ShotNo              int    `json:"shotNo"`
	NodeRunID           string `json:"nodeRunId"`
	ExecutionToken      string `json:"executionToken"`
	AttemptGeneration   int    `json:"attemptGeneration"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	PollCount           int    `json:"pollCount,omitempty"`
	ExecutionPlanID     string `json:"executionPlanId,omitempty"`
	RenderSegmentID     string `json:"renderSegmentId,omitempty"`
	SegmentIndex        int    `json:"segmentIndex,omitempty"`
	SegmentCount        int    `json:"segmentCount,omitempty"`
}

type PollShotVideoTaskOutput struct {
	ProviderCallID            string                           `json:"providerCallId"`
	ProviderAsyncTaskID       string                           `json:"providerAsyncTaskId"`
	ExternalTaskID            string                           `json:"externalTaskId,omitempty"`
	Status                    string                           `json:"status"`
	ArtifactID                string                           `json:"artifactId,omitempty"`
	MediaFileID               string                           `json:"mediaFileId,omitempty"`
	StorageKey                string                           `json:"storageKey,omitempty"`
	MimeType                  string                           `json:"mimeType,omitempty"`
	DurationSeconds           *float64                         `json:"durationSeconds,omitempty"`
	RequestedDurationSeconds  *float64                         `json:"requestedDurationSeconds,omitempty"`
	ProviderDurationSeconds   *float64                         `json:"providerDurationSeconds,omitempty"`
	ActualDurationSeconds     *float64                         `json:"actualDurationSeconds,omitempty"`
	DurationSource            string                           `json:"durationSource,omitempty"`
	MediaProbe                *provider.GatewayVideoMediaProbe `json:"mediaProbe,omitempty"`
	PollCount                 int                              `json:"pollCount,omitempty"`
	ExecutionPlanID           string                           `json:"executionPlanId,omitempty"`
	RenderSegmentID           string                           `json:"renderSegmentId,omitempty"`
	SegmentIndex              int                              `json:"segmentIndex,omitempty"`
	SegmentCount              int                              `json:"segmentCount,omitempty"`
	ErrorCode                 string                           `json:"errorCode,omitempty"`
	ErrorMessage              string                           `json:"errorMessage,omitempty"`
	MezzanineArtifactID       string                           `json:"mezzanineArtifactId,omitempty"`
	MezzanineMediaFileID      string                           `json:"mezzanineMediaFileId,omitempty"`
	MezzanineStorageKey       string                           `json:"mezzanineStorageKey,omitempty"`
	ExtractedAudioArtifactID  string                           `json:"extractedAudioArtifactId,omitempty"`
	ExtractedAudioMediaFileID string                           `json:"extractedAudioMediaFileId,omitempty"`
	ExtractedAudioStorageKey  string                           `json:"extractedAudioStorageKey,omitempty"`
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
	ExecutionPlanID     string `json:"executionPlanId,omitempty"`
	RenderSegmentID     string `json:"renderSegmentId,omitempty"`
	SegmentIndex        int    `json:"segmentIndex,omitempty"`
	SegmentCount        int    `json:"segmentCount,omitempty"`
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
	ExecutionPlanID     string `json:"executionPlanId,omitempty"`
	RenderSegmentID     string `json:"renderSegmentId,omitempty"`
	SegmentIndex        int    `json:"segmentIndex,omitempty"`
	SegmentCount        int    `json:"segmentCount,omitempty"`
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
	ExecutionToken      string `json:"executionToken"`
	AttemptGeneration   int    `json:"attemptGeneration"`
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
	ExecutionToken      string `json:"executionToken"`
	AttemptGeneration   int    `json:"attemptGeneration"`
	ProviderAsyncTaskID string `json:"providerAsyncTaskId"`
	ExternalTaskID      string `json:"externalTaskId,omitempty"`
	PollCount           int    `json:"pollCount,omitempty"`
}

type PollStoryboardVideoTaskOutput struct {
	ProviderCallID           string                           `json:"providerCallId"`
	ProviderAsyncTaskID      string                           `json:"providerAsyncTaskId"`
	ExternalTaskID           string                           `json:"externalTaskId,omitempty"`
	Status                   string                           `json:"status"`
	ArtifactID               string                           `json:"artifactId,omitempty"`
	MediaFileID              string                           `json:"mediaFileId,omitempty"`
	StorageKey               string                           `json:"storageKey,omitempty"`
	MimeType                 string                           `json:"mimeType,omitempty"`
	DurationSeconds          *float64                         `json:"durationSeconds,omitempty"`
	RequestedDurationSeconds *float64                         `json:"requestedDurationSeconds,omitempty"`
	ProviderDurationSeconds  *float64                         `json:"providerDurationSeconds,omitempty"`
	ActualDurationSeconds    *float64                         `json:"actualDurationSeconds,omitempty"`
	DurationSource           string                           `json:"durationSource,omitempty"`
	MediaProbe               *provider.GatewayVideoMediaProbe `json:"mediaProbe,omitempty"`
	PollCount                int                              `json:"pollCount,omitempty"`
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

func (output GenerateShotImageOutput) nodeExecution() NodeExecution {
	return NodeExecution{
		NodeRunID: output.NodeRunID, ExecutionToken: output.ExecutionToken,
		AttemptGeneration: output.AttemptGeneration,
	}
}

func (output CreateShotVideoTaskOutput) nodeExecution() NodeExecution {
	return NodeExecution{
		NodeRunID: output.NodeRunID, ExecutionToken: output.ExecutionToken,
		AttemptGeneration: output.AttemptGeneration,
	}
}

func (input PollShotVideoTaskInput) nodeExecution() NodeExecution {
	return NodeExecution{
		NodeRunID: input.NodeRunID, ExecutionToken: input.ExecutionToken,
		AttemptGeneration: input.AttemptGeneration,
	}
}

func (output CreateStoryboardVideoTaskOutput) nodeExecution() NodeExecution {
	return NodeExecution{
		NodeRunID: output.NodeRunID, ExecutionToken: output.ExecutionToken,
		AttemptGeneration: output.AttemptGeneration,
	}
}

func (input PollStoryboardVideoTaskInput) nodeExecution() NodeExecution {
	return NodeExecution{
		NodeRunID: input.NodeRunID, ExecutionToken: input.ExecutionToken,
		AttemptGeneration: input.AttemptGeneration,
	}
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
		FailureScope:   input.FailureScope,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return GenerateShotImageOutput{}, err
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return GenerateShotImageOutput{}, err
	}
	projectSettings, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	anchorRole, err := resolveShotAnchorRole(projectSettings.VideoProductionProfileKey, input.AnchorRole)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", err)
	}
	input.AnchorRole = anchorRole
	input.ProfileKey = projectSettings.VideoProductionProfileKey
	anchor, err := a.latestShotVisualAnchor(ctx, input.ProjectID, shot.ID, anchorRole)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", err)
	}
	if !input.Force && anchor.Status == "ready" && anchor.ReviewStatus == "approved" && anchor.ArtifactID != "" && anchor.MediaFileID != "" && anchor.StorageKey != "" {
		return GenerateShotImageOutput{
			ShotID:           shot.ID,
			AnchorRole:       anchorRole,
			VisualAnchorID:   anchor.ID,
			ImageArtifactID:  anchor.ArtifactID,
			ImageMediaFileID: anchor.MediaFileID,
			ImageStorageKey:  anchor.StorageKey,
		}, nil
	}
	aspectRatio := firstNonEmptyString(projectSettings.VideoRatio, projectSettings.AspectRatio, input.AspectRatio, "16:9")
	pinnedImageModelID := ""
	if projectSettings.VideoProductionProfileKey == videoproduction.ProfileStoryboardSheet {
		manifest, ok, manifestErr := a.findStoryboardSheetManifest(ctx, shot.ID, "", false)
		if manifestErr != nil {
			return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", manifestErr)
		}
		if !ok {
			return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{Code: videoproduction.CodePanelManifestInvalid, Message: "分镜板模式缺少 PanelManifest，请先生成图片提示词"})
		}
		aspectRatio = manifest.Manifest.SheetAspectRatio
	}
	imageSize := storyboardImageSizeForAspectRatio(aspectRatio)
	assetContext, err := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	promptTrace, err := a.reviewedShotImagePrompt(ctx, shot.ID, anchorRole)
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	finalPrompt := strings.TrimSpace(promptTrace.Prompt)
	if promptTrace.Status != "succeeded" || finalPrompt == "" {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{
			Code:    provider.CodeInvalidRequest,
			Message: "an agent-reviewed or manually saved image prompt is required",
		})
	}
	pinnedImageModelID = promptTrace.ImageProviderModelID
	if projectSettings.VideoProductionProfileKey == videoproduction.ProfileStoryboardSheet && pinnedImageModelID == "" {
		if a.gateway == nil {
			return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
		}
		constraints, constraintErr := a.gateway.ResolveModelConstraints(ctx, provider.GatewayModelConstraintsRequest{
			OrganizationID: input.OrganizationID, ModelProfileKey: projectSettings.ImageModelProfileKey,
			TaskType: provider.TaskTypeImageGenerate, Modality: "image",
		})
		if constraintErr != nil {
			return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowErrorFromProvider(constraintErr, codeActivityFailed))
		}
		candidate, ok := selectStoryboardSheetImageModel(constraints.Candidates)
		if !ok {
			return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "image_failed", "storyboard.shot.image.failed", workflowError{Code: provider.CodeModelCapabilityUnavailable, Message: "分镜板模式要求图片业务模型绑定可用的 gpt-image-2"})
		}
		pinnedImageModelID = candidate.ProviderModelID
	}
	modelProfileKey := projectSettings.ImageModelProfileKey
	promptHash := strings.TrimSpace(promptTrace.PromptHash)
	if promptHash == "" {
		promptHash = promptsvc.HashText(finalPrompt)
	}
	promptSource := firstNonEmptyString(promptTrace.PromptSource, "shot_image_prompt")
	nodeKey := nodeKeyForShot(nodeGenerateShotImagePrefix, shot.ShotIndex) + "_" + anchorRole
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "image.generate",
		Input: mustJSON(map[string]any{
			"shotId":                       shot.ID,
			"shotIndex":                    shot.ShotIndex,
			"shotNo":                       shot.ShotNo,
			"anchorRole":                   anchorRole,
			"visualAnchorId":               anchor.ID,
			"aspectRatio":                  aspectRatio,
			"size":                         imageSize,
			"modelProfileKey":              modelProfileKey,
			"providerModelId":              pinnedImageModelID,
			"promptTemplateKey":            promptTrace.TemplateKey,
			"promptVersionId":              promptTrace.PromptVersionID,
			"promptHash":                   promptHash,
			"promptSource":                 promptSource,
			"negativePrompt":               promptTrace.NegativePrompt,
			"imageReferenceMode":           assetContext.ImageReferenceMode,
			"imageReferenceKeys":           assetContext.ResolvedReferenceKeys,
			"configuredImageReferenceKeys": assetContext.ImageReferenceKeys,
			"imageReferenceCount":          len(assetContext.ImageReferences),
		}),
	})
	if err != nil {
		return GenerateShotImageOutput{}, err
	}
	if err := a.markShotImageStarted(ctx, input, shot, nodeExecution); err != nil {
		return GenerateShotImageOutput{}, err
	}
	if recovered, ok, err := a.recoverCompletedShotImage(ctx, input, nodeExecution); err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "image_failed", "storyboard.shot.image.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	} else if ok {
		if err := a.validateShotImageAspectRatio(ctx, input.ProjectID, recovered.ImageMediaFileID, aspectRatio); err != nil {
			return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "image_failed", "storyboard.shot.image.failed", err)
		}
		if err := a.completeShotImage(ctx, input, shot, recovered); err != nil {
			return GenerateShotImageOutput{}, err
		}
		return recovered, nil
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, modelProfileKey, []string{"image", "multimodal"}); err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "image_failed", "storyboard.shot.image.failed", err)
	}
	if a.gateway == nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "image_failed", "storyboard.shot.image.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	idempotencyKey := "shot-image:" + nodeExecution.NodeRunID
	gatewayResp, err := a.generateProviderImage(ctx, nodeExecution, provider.GatewayImageRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeExecution.NodeRunID,
		ModelProfileKey:   modelProfileKey,
		ProviderModelID:   pinnedImageModelID,
		PromptTemplateKey: promptTrace.TemplateKey,
		PromptVersionID:   promptTrace.PromptVersionID,
		PromptHash:        promptHash,
		PromptSource:      promptSource,
		IdempotencyKey:    idempotencyKey,
		Input: mustJSON(map[string]any{
			"prompt":         finalPrompt,
			"negativePrompt": promptTrace.NegativePrompt,
			"size":           imageSize,
			"aspectRatio":    aspectRatio,
			"n":              1,
			"quality":        projectSettings.ImageQuality,
		}),
		References: assetContext.ImageReferences,
		Options: provider.GatewayImageOptions{
			IdempotencyKey: idempotencyKey,
			Retry:          currentActivityAttempt(ctx) > 1,
		},
	})
	if err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "image_failed", "storyboard.shot.image.failed", workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateShotImageOutput{
		NodeRunID:         nodeExecution.NodeRunID,
		ExecutionToken:    nodeExecution.ExecutionToken,
		AttemptGeneration: nodeExecution.AttemptGeneration,
		ShotID:            shot.ID,
		AnchorRole:        anchorRole,
		VisualAnchorID:    anchor.ID,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ImageArtifactID:   gatewayResp.Output.ArtifactID,
		ImageMediaFileID:  gatewayResp.Output.MediaFileID,
		ImageStorageKey:   gatewayResp.Output.StorageKey,
	}
	if err := a.validateShotImageAspectRatio(ctx, input.ProjectID, output.ImageMediaFileID, aspectRatio); err != nil {
		return GenerateShotImageOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "image_failed", "storyboard.shot.image.failed", err)
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
		FailureScope:   input.FailureScope,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	if strings.TrimSpace(input.ExecutionPlanID) == "" && !input.Force && shot.VideoProviderAsyncTaskID != "" {
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
	if strings.TrimSpace(input.ExecutionPlanID) == "" {
		duration = wholeSecondVideoRequestDuration(duration)
	}
	resolution := strings.TrimSpace(input.Resolution)
	if resolution == "" {
		resolution = "720p"
	}
	projectSettings, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	aspectRatio := firstNonEmptyString(projectSettings.VideoRatio, projectSettings.AspectRatio, input.AspectRatio, "16:9")
	var approvedContract approvedShotVideoExecutionContract
	var videoReferences ShotVideoReferenceContext
	usesApprovedContract := strings.TrimSpace(input.ExecutionPlanID) != ""
	if usesApprovedContract {
		approvedContract, err = a.loadApprovedShotVideoExecutionContract(ctx, input.OrganizationID, projectSettings, shot)
		if err != nil {
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", err)
		}
		videoReferences = ShotVideoReferenceContext{
			ReferenceMode: videoReferenceModeForProfile(projectSettings.VideoProductionProfileKey),
			References:    append([]provider.GatewayVideoReference(nil), approvedContract.References...),
		}
	} else {
		assetContext, loadErr := a.shotAssetContext(ctx, input.ProjectID, shot.ID)
		if loadErr != nil {
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: codeActivityFailed, Message: loadErr.Error()})
		}
		videoReferences, err = a.shotVideoReferenceContext(ctx, input.ProjectID, shot, assetContext)
		if err != nil {
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", err)
		}
	}
	if input.SegmentIndex > 0 {
		if strings.TrimSpace(input.PreviousSegmentArtifactID) == "" {
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "continuation render segment requires the previous segment artifact"})
		}
		videoReferences.ReferenceMode = "custom"
		videoReferences.ConfiguredReferenceKeys = []string{"render_segment_previous"}
		videoReferences.ResolvedReferenceKeys = []string{"render_segment_previous"}
		switch input.InputContractKey {
		case provider.VideoInputContractVideoExtension:
			videoReferences.References = []provider.GatewayVideoReference{{
				ReferenceKey: "video-extension:" + input.RenderSegmentID,
				Role:         "video_extension_source", Required: true, Type: "video_reference",
				SourceType: "video_render_segment", SourceID: input.PreviousSegmentRenderSegmentID,
				SourceVersion: input.PreviousSegmentArtifactID,
				ArtifactID:    input.PreviousSegmentArtifactID, MediaFileID: input.PreviousSegmentMediaFileID,
				StorageKey: input.PreviousSegmentStorageKey,
			}}
		case provider.VideoInputContractFirstFrame:
			frame := input.PreviousSegmentTailFrame
			if frame == nil || frame.SourceVideoArtifactID != input.PreviousSegmentArtifactID {
				return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "首帧续接缺少前一片段的 fresh 尾帧"})
			}
			videoReferences.References = []provider.GatewayVideoReference{{
				ReferenceKey: "tail-frame:" + frame.SourceRenderSegmentID,
				Role:         "first_frame", Required: true, Type: "image_reference",
				SourceType: "video_render_segment_tail_anchor", SourceID: frame.AnchorID,
				SourceVersion: frame.SourceRenderSegmentID,
				ArtifactID:    frame.ArtifactID, MediaFileID: frame.MediaFileID, StorageKey: frame.StorageKey,
				ContentHash: frame.ContentHash, GeneratedAt: frame.GeneratedAt.UTC().Format(time.RFC3339Nano),
			}}
		case provider.VideoInputContractFirstFramePlusReferences:
			frame := input.PreviousSegmentTailFrame
			if frame == nil || frame.SourceVideoArtifactID != input.PreviousSegmentArtifactID {
				return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "多模态续接缺少前一片段的 fresh 尾帧"})
			}
			continued := []provider.GatewayVideoReference{{
				ReferenceKey: "tail-frame:" + frame.SourceRenderSegmentID,
				Role:         "first_frame", Required: true, Type: "image",
				SourceType: "video_render_segment_tail_anchor", SourceID: frame.AnchorID,
				SourceVersion: frame.SourceRenderSegmentID,
				ArtifactID:    frame.ArtifactID, MediaFileID: frame.MediaFileID,
				StorageKey: frame.StorageKey, MimeType: "image/png",
				ContentHash: frame.ContentHash, GeneratedAt: frame.GeneratedAt.UTC().Format(time.RFC3339Nano),
			}}
			for _, reference := range approvedContract.References {
				if reference.Role == videoproduction.ReferenceRoleFirstFrame || reference.Role == videoproduction.ReferenceRoleLastFrame {
					continue
				}
				continued = append(continued, reference)
			}
			videoReferences.References = continued
		default:
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeModelInputContractUnsupported, Message: "当前视频片段没有可执行的续接输入契约"})
		}
	}
	if usesApprovedContract && input.SegmentIndex == 0 {
		for _, reference := range videoReferences.References {
			if strings.TrimSpace(reference.MediaFileID) == "" || strings.Contains(strings.ToLower(reference.Type), "video") {
				continue
			}
			if err := a.validateShotImageAspectRatio(ctx, input.ProjectID, reference.MediaFileID, aspectRatio); err != nil {
				return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", err)
			}
		}
	} else if shotVideoReferencesStoryboardImage(videoReferences, shot.ID) {
		if err := a.validateShotImageAspectRatio(ctx, input.ProjectID, shot.ImageMediaFileID, aspectRatio); err != nil {
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", err)
		}
	}
	if videoReferences.ReferenceMode != "none" && len(videoReferences.References) == 0 {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "shot video generation requires an available reference image or reference mode none"})
	}
	var referenceManifest *videocontracts.ReferenceManifestV2
	var referenceManifestHash string
	if usesApprovedContract {
		videoReferences.References, referenceManifest, referenceManifestHash, err = a.prepareVideoReferenceManifest(
			ctx, input.OrganizationID, input.InputContractKey, input.CapabilitySnapshotHash, videoReferences.References,
		)
		if err != nil {
			return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", err)
		}
	}
	generationMode := "image_to_video"
	if len(videoReferences.References) == 0 {
		generationMode = "text_to_video"
	}
	modelProfileKey := projectSettings.VideoModelProfileKey
	finalPrompt := firstNonEmptyString(input.Prompt, shot.VideoPrompt)
	if finalPrompt == "" {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, NodeExecution{}, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "an agent-reviewed video prompt is required"})
	}
	promptHash := resolvedVideoPromptHash(finalPrompt, input.PromptHash)
	promptSource := "shot_video_prompt"
	if strings.TrimSpace(input.ReviewProviderCallID) != "" {
		promptSource = "agent_reviewed"
	}
	nodeKey := nodeKeyForShot(nodeCreateShotVideoPrefix, shot.ShotIndex)
	if strings.TrimSpace(input.RenderSegmentID) != "" {
		nodeKey = fmt.Sprintf("%s_segment_%d", nodeKey, input.SegmentIndex)
	}
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeKey,
		NodeType:       "video.create_task",
		Input: mustJSON(map[string]any{
			"shotId":                       shot.ID,
			"shotIndex":                    shot.ShotIndex,
			"shotNo":                       shot.ShotNo,
			"imageArtifactId":              shot.ImageArtifactID,
			"imageMediaFileId":             shot.ImageMediaFileID,
			"imageStorageKey":              shot.ImageStorageKey,
			"duration":                     duration,
			"aspectRatio":                  aspectRatio,
			"resolution":                   resolution,
			"mode":                         generationMode,
			"modelProfileKey":              modelProfileKey,
			"videoReferenceMode":           videoReferences.ReferenceMode,
			"videoReferenceKeys":           videoReferences.ResolvedReferenceKeys,
			"configuredVideoReferenceKeys": videoReferences.ConfiguredReferenceKeys,
			"videoReferenceCount":          len(videoReferences.References),
			"promptTemplateKey":            input.ReviewTemplateKey,
			"promptVersionId":              input.ReviewPromptVersionID,
			"promptHash":                   promptHash,
			"promptSource":                 promptSource,
			"generationProviderCallId":     input.GenerationProviderCallID,
			"reviewProviderCallId":         input.ReviewProviderCallID,
			"executionPlanId":              input.ExecutionPlanID,
			"renderSegmentId":              input.RenderSegmentID,
			"capabilitySnapshotHash":       input.CapabilitySnapshotHash,
			"segmentIndex":                 input.SegmentIndex,
			"segmentCount":                 input.SegmentCount,
			"retryGeneration":              input.RetryGeneration,
			"continuityMode":               input.ContinuityMode,
			"previousSegmentTailAnchor":    input.PreviousSegmentTailFrame,
			"referenceManifestHash":        referenceManifestHash,
		}),
	})
	if err != nil {
		return CreateShotVideoTaskOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, modelProfileKey, []string{"video", "multimodal"}); err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", err)
	}
	if a.gateway == nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayRequest := provider.GatewayVideoCreateTaskRequest{
		OrganizationID:                 input.OrganizationID,
		ProjectID:                      input.ProjectID,
		OperationID:                    input.OperationID,
		OperationItemID:                input.OperationItemID,
		OperationItemAttempt:           input.OperationItemAttempt,
		ProductionGenerationID:         nodeExecution.ProductionGenerationID,
		VideoProductionBindingID:       nodeExecution.VideoProductionBindingID,
		VideoProductionBindingRevision: nodeExecution.VideoProductionBindingRevision,
		StoryboardShotID:               shot.ID,
		ProductionProfileVersionID:     approvedContract.ProductionProfileVersionID,
		ProductionProfileSnapshotHash:  approvedContract.ProductionProfileSnapshotHash,
		InputContractKey:               input.InputContractKey,
		InputContractHash:              input.InputContractHash,
		InputContractVersion:           approvedContract.InputContractVersion,
		ShotStateRevision:              approvedContract.ShotStateRevision,
		ShotStateHash:                  approvedContract.ShotStateHash,
		TransitionHash:                 approvedContract.TransitionHash,
		ReferencePackID:                approvedContract.ReferencePackID,
		ReferencePackHash:              approvedContract.ReferencePackHash,
		PromptContextPlanID:            approvedContract.PromptContextPlanID,
		PromptContextPlanHash:          approvedContract.PromptContextPlanHash,
		VideoPromptPlanID:              approvedContract.VideoPromptPlanID,
		NativeAudioRequired:            approvedContract.NativeAudioRequired,
		DialogueCues:                   storyboardDialogueToGatewaySpans(input.DialogueLines),
		WorkflowRunID:                  input.WorkflowRunID,
		NodeRunID:                      nodeExecution.NodeRunID,
		NodeExecutionToken:             nodeExecution.ExecutionToken,
		NodeAttemptGeneration:          nodeExecution.AttemptGeneration,
		ModelProfileKey:                modelProfileKey,
		PromptTemplateKey:              input.ReviewTemplateKey,
		PromptVersionID:                input.ReviewPromptVersionID,
		PromptHash:                     promptHash,
		PromptSource:                   promptSource,
		IdempotencyKey:                 shotVideoSegmentIdempotencyKey(input.WorkflowRunID, shot.ShotIndex, input.ExecutionPlanID, input.SegmentIndex, input.RetryGeneration),
		ExecutionPlanID:                input.ExecutionPlanID,
		RenderSegmentID:                input.RenderSegmentID,
		CapabilitySnapshotHash:         input.CapabilitySnapshotHash,
		ReferenceManifest:              referenceManifest,
		ReferenceManifestHash:          referenceManifestHash,
		Input: mustJSON(map[string]any{
			"prompt":         finalPrompt,
			"negativePrompt": input.NegativePrompt,
			"duration":       duration,
			"aspectRatio":    aspectRatio,
			"resolution":     resolution,
			"mode":           generationMode,
		}),
		References: videoReferences.References,
		Options:    provider.GatewayVideoOptions{IdempotencyKey: shotVideoSegmentIdempotencyKey(input.WorkflowRunID, shot.ShotIndex, input.ExecutionPlanID, input.SegmentIndex, input.RetryGeneration)},
	}
	var gatewayResp provider.GatewayVideoCreateTaskResponse
	if strings.TrimSpace(input.ExecutionPlanID) != "" {
		gatewayResp, err = a.gateway.CreateVideoTaskResult(ctx, gatewayRequest)
	} else {
		gatewayResp, err = a.gateway.CreateVideoTask(ctx, gatewayRequest)
	}
	if err != nil {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := CreateShotVideoTaskOutput{
		NodeRunID:           nodeExecution.NodeRunID,
		ExecutionToken:      nodeExecution.ExecutionToken,
		AttemptGeneration:   nodeExecution.AttemptGeneration,
		ShotID:              shot.ID,
		ProviderCallID:      gatewayResp.ProviderCallID,
		ProviderAsyncTaskID: gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:      gatewayResp.ExternalTaskID,
		Status:              gatewayResp.Status,
		ModelID:             gatewayResp.ModelID,
		ExecutionPlanID:     gatewayResp.ExecutionPlanID,
		RenderSegmentID:     gatewayResp.RenderSegmentID,
		SegmentIndex:        input.SegmentIndex,
		SegmentCount:        input.SegmentCount,
		RetryGeneration:     input.RetryGeneration,
	}
	if gatewayResp.Error != nil {
		output.ErrorCode = gatewayResp.Error.Code
		output.ErrorMessage = gatewayResp.Error.Message
	}
	if strings.TrimSpace(input.ExecutionPlanID) != "" && isTerminalVideoAttemptFailure(output.Status) {
		if err := a.recordPlannedVideoSegmentFailure(ctx, baseInput, shot, nodeExecution, output.ErrorCode, output.ErrorMessage); err != nil {
			return CreateShotVideoTaskOutput{}, err
		}
		return output, nil
	}
	if strings.TrimSpace(output.ProviderAsyncTaskID) == "" {
		return CreateShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway did not return providerAsyncTaskId"})
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
		FailureScope:   input.FailureScope,
	}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.NodeRunID) == "" {
		return PollShotVideoTaskOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and nodeRunId are required")
	}
	if strings.TrimSpace(input.ProviderAsyncTaskID) == "" {
		return PollShotVideoTaskOutput{}, fmt.Errorf("providerAsyncTaskId is required")
	}
	nodeExecution := input.nodeExecution()
	if !nodeExecution.valid() {
		return PollShotVideoTaskOutput{}, fmt.Errorf("node execution identity is required")
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
	if err != nil {
		return PollShotVideoTaskOutput{}, err
	}
	if a.gateway == nil {
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}
	gatewayRequest := provider.GatewayVideoPollTaskRequest{
		OrganizationID:        input.OrganizationID,
		ProjectID:             input.ProjectID,
		WorkflowRunID:         input.WorkflowRunID,
		NodeRunID:             input.NodeRunID,
		NodeExecutionToken:    nodeExecution.ExecutionToken,
		NodeAttemptGeneration: nodeExecution.AttemptGeneration,
		ProviderAsyncTaskID:   input.ProviderAsyncTaskID,
		ExternalTaskID:        input.ExternalTaskID,
	}
	var gatewayResp provider.GatewayVideoPollTaskResponse
	if strings.TrimSpace(input.ExecutionPlanID) != "" {
		gatewayResp, err = a.gateway.PollVideoTaskResult(ctx, gatewayRequest)
	} else {
		gatewayResp, err = a.gateway.PollVideoTask(ctx, gatewayRequest)
	}
	if err != nil {
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := PollShotVideoTaskOutput{
		ProviderCallID:           gatewayResp.ProviderCallID,
		ProviderAsyncTaskID:      gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:           gatewayResp.ExternalTaskID,
		Status:                   gatewayResp.Status,
		ArtifactID:               gatewayResp.Output.ArtifactID,
		MediaFileID:              gatewayResp.Output.MediaFileID,
		StorageKey:               gatewayResp.Output.StorageKey,
		MimeType:                 gatewayResp.Output.MimeType,
		DurationSeconds:          gatewayResp.Output.DurationSeconds,
		RequestedDurationSeconds: gatewayResp.Output.RequestedDurationSeconds,
		ProviderDurationSeconds:  gatewayResp.Output.ProviderDurationSeconds,
		ActualDurationSeconds:    gatewayResp.Output.ActualDurationSeconds,
		DurationSource:           gatewayResp.Output.DurationSource,
		MediaProbe:               gatewayResp.Output.MediaProbe,
		PollCount:                input.PollCount,
		ExecutionPlanID:          gatewayResp.ExecutionPlanID,
		RenderSegmentID:          gatewayResp.RenderSegmentID,
		SegmentIndex:             input.SegmentIndex,
		SegmentCount:             input.SegmentCount,
	}
	if gatewayResp.Error != nil {
		output.ErrorCode = gatewayResp.Error.Code
		output.ErrorMessage = gatewayResp.Error.Message
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
			return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway video output is missing artifact/media/storage"})
		}
		if err := a.completeShotVideo(ctx, input, shot, output); err != nil {
			return PollShotVideoTaskOutput{}, err
		}
		return output, nil
	case "failed", "cancelled":
		if strings.TrimSpace(input.ExecutionPlanID) != "" {
			if err := a.recordPlannedVideoSegmentFailure(ctx, baseInput, shot, nodeExecution, output.ErrorCode, output.ErrorMessage); err != nil {
				return PollShotVideoTaskOutput{}, err
			}
			return output, nil
		}
		status := "video_failed"
		if output.Status == "cancelled" {
			status = "cancelled"
		}
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, status, "storyboard.shot.video.failed", workflowError{Code: codeActivityFailed, Message: "provider video task " + output.Status})
	default:
		return PollShotVideoTaskOutput{}, a.failShotActivity(ctx, baseInput, shot, nodeExecution, "video_failed", "storyboard.shot.video.failed", workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned unsupported video status: " + output.Status})
	}
}

func (a Activities) CancelShotVideoTask(ctx context.Context, input CancelShotVideoTaskInput) (CancelShotVideoTaskOutput, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.NodeRunID) == "" {
		return CancelShotVideoTaskOutput{}, fmt.Errorf("organizationId, projectId, workflowRunId, and nodeRunId are required")
	}
	if strings.TrimSpace(input.ProviderAsyncTaskID) == "" {
		return CancelShotVideoTaskOutput{}, fmt.Errorf("providerAsyncTaskId is required")
	}
	shot, err := a.storyboardShot(ctx, input.ProjectID, input.WorkflowRunID, input.ShotID, input.ShotIndex)
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
		ExecutionPlanID:     input.ExecutionPlanID,
		RenderSegmentID:     input.RenderSegmentID,
		SegmentIndex:        input.SegmentIndex,
		SegmentCount:        input.SegmentCount,
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
			output.ExecutionPlanID = firstNonEmptyString(gatewayResp.ExecutionPlanID, input.ExecutionPlanID)
			output.RenderSegmentID = firstNonEmptyString(gatewayResp.RenderSegmentID, input.RenderSegmentID)
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
	duration = wholeSecondVideoRequestDuration(duration)
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
	if videoPrompt == "" {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, NodeExecution{}, workflowError{Code: provider.CodeInvalidRequest, Message: "video prompt is required"})
	}
	promptHash := resolvedVideoPromptHash(videoPrompt, "")

	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
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
			"promptHash":           promptHash,
			"promptSource":         "direct_video_prompt",
		}),
	})
	if err != nil {
		return CreateStoryboardVideoTaskOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, videoGenerationModelProfileKey, []string{"video", "multimodal"}); err != nil {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, err)
	}
	if a.gateway == nil {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.CreateVideoTask(ctx, provider.GatewayVideoCreateTaskRequest{
		OrganizationID:                 input.OrganizationID,
		ProjectID:                      input.ProjectID,
		ProductionGenerationID:         nodeExecution.ProductionGenerationID,
		VideoProductionBindingID:       nodeExecution.VideoProductionBindingID,
		VideoProductionBindingRevision: nodeExecution.VideoProductionBindingRevision,
		WorkflowRunID:                  input.WorkflowRunID,
		NodeRunID:                      nodeExecution.NodeRunID,
		NodeExecutionToken:             nodeExecution.ExecutionToken,
		NodeAttemptGeneration:          nodeExecution.AttemptGeneration,
		ModelProfileKey:                videoGenerationModelProfileKey,
		PromptHash:                     promptHash,
		PromptSource:                   "direct_video_prompt",
		IdempotencyKey:                 videoTaskIdempotencyKey(input.WorkflowRunID),
		Input: mustJSON(map[string]any{
			"prompt":      videoPrompt,
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
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := CreateStoryboardVideoTaskOutput{
		NodeRunID:           nodeExecution.NodeRunID,
		ExecutionToken:      nodeExecution.ExecutionToken,
		AttemptGeneration:   nodeExecution.AttemptGeneration,
		ProviderCallID:      gatewayResp.ProviderCallID,
		ProviderAsyncTaskID: gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:      gatewayResp.ExternalTaskID,
		Status:              gatewayResp.Status,
		ModelID:             gatewayResp.ModelID,
	}
	if strings.TrimSpace(output.ProviderAsyncTaskID) == "" {
		return CreateStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway did not return providerAsyncTaskId"})
	}
	if err := ProgressNodeRun(ctx, a.db, nodeExecution, mustJSON(output)); err != nil {
		return CreateStoryboardVideoTaskOutput{}, err
	}
	return output, nil
}

func wholeSecondVideoRequestDuration(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	return math.Ceil(value - 1e-9)
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
	nodeExecution := input.nodeExecution()
	if !nodeExecution.valid() {
		return PollStoryboardVideoTaskOutput{}, fmt.Errorf("node execution identity is required")
	}
	if a.gateway == nil {
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.PollVideoTask(ctx, provider.GatewayVideoPollTaskRequest{
		OrganizationID:        input.OrganizationID,
		ProjectID:             input.ProjectID,
		WorkflowRunID:         input.WorkflowRunID,
		NodeRunID:             input.NodeRunID,
		NodeExecutionToken:    nodeExecution.ExecutionToken,
		NodeAttemptGeneration: nodeExecution.AttemptGeneration,
		ProviderAsyncTaskID:   input.ProviderAsyncTaskID,
		ExternalTaskID:        input.ExternalTaskID,
	})
	if err != nil {
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := PollStoryboardVideoTaskOutput{
		ProviderCallID:           gatewayResp.ProviderCallID,
		ProviderAsyncTaskID:      gatewayResp.ProviderAsyncTaskID,
		ExternalTaskID:           gatewayResp.ExternalTaskID,
		Status:                   gatewayResp.Status,
		ArtifactID:               gatewayResp.Output.ArtifactID,
		MediaFileID:              gatewayResp.Output.MediaFileID,
		StorageKey:               gatewayResp.Output.StorageKey,
		MimeType:                 gatewayResp.Output.MimeType,
		DurationSeconds:          gatewayResp.Output.DurationSeconds,
		RequestedDurationSeconds: gatewayResp.Output.RequestedDurationSeconds,
		ProviderDurationSeconds:  gatewayResp.Output.ProviderDurationSeconds,
		ActualDurationSeconds:    gatewayResp.Output.ActualDurationSeconds,
		DurationSource:           gatewayResp.Output.DurationSource,
		MediaProbe:               gatewayResp.Output.MediaProbe,
		PollCount:                input.PollCount,
	}

	switch output.Status {
	case "queued", "running", "":
		if output.Status == "" {
			output.Status = "running"
		}
		if err := ProgressNodeRun(ctx, a.db, nodeExecution, mustJSON(output)); err != nil {
			return PollStoryboardVideoTaskOutput{}, err
		}
		return output, nil
	case "succeeded":
		if output.ArtifactID == "" || output.MediaFileID == "" || output.StorageKey == "" {
			return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway video output is missing artifact/media/storage"})
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
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: code, Message: "provider video task " + output.Status})
	default:
		return PollStoryboardVideoTaskOutput{}, a.failActivity(ctx, baseInput, nodeExecution, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned unsupported video status: " + output.Status})
	}
}

func (a Activities) CompleteVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, output VideoProductionOutput) error {
	status := strings.TrimSpace(output.Status)
	if status == "" {
		status = "succeeded"
	}
	if status != "succeeded" && status != "partial_succeeded" && status != "failed" {
		status = "succeeded"
	}
	code, message := "", ""
	if status == "failed" {
		code, message = "VIDEO_PRODUCTION_FAILED", "所有镜头视频均生成失败"
	}
	return TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, status, code, message, mustJSON(output))
}

func (a Activities) FailVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, nodeExecution NodeExecution, code, message string) error {
	output := mustJSON(map[string]any{"code": code, "message": message})
	if !nodeExecution.valid() {
		return TransitionWorkflowRun(ctx, a.db, input.WorkflowRunID, "failed", code, message, output)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if _, err := failNodeRunTx(ctx, tx, nodeExecution, code, message, output); err != nil {
		return err
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, "failed", code, message, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) CancelVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, output CancelShotVideoTaskOutput, reason string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
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
	_, applied, err := cancelWorkflowRunTx(ctx, tx, input.WorkflowRunID, runOutput, reason, "USER_CANCELLED")
	if err != nil {
		return err
	}
	if !applied {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'cancelled',
		    video_status = 'cancelled',
		    video_completed_at = now(),
		    updated_at = now()
		WHERE workflow_run_id = $1
		  AND status NOT IN ('video_succeeded', 'video_failed', 'cancelled')
	`, input.WorkflowRunID); err != nil {
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

func storyboardShotRecordSelectSQL(where string) string {
	return `
		SELECT
			s.id::text,
			COALESCE(s.workflow_run_id::text, ''),
			COALESCE(s.script_scene_id::text, ''),
			COALESCE(s.script_episode_id::text, ''),
			COALESCE(s.episode_index, 0),
			COALESCE(s.episode_shot_index, s.shot_index),
			s.shot_index,
			COALESCE(s.shot_no, s.shot_index + 1),
			COALESCE(s.title, ''),
			COALESCE(s.storyboard_plan_id::text, ''),
			s.start_tick,
			s.end_tick,
			s.planned_duration_ticks,
			s.planned_duration_ticks::float8 / p.timeline_timebase,
			p.timeline_timebase,
			p.fps_numerator,
			p.fps_denominator,
			s.duration_source,
			COALESCE(s.timing_confidence, 0)::float8,
			COALESCE(s.duration_locked, false),
			COALESCE(s.one_take, false),
			COALESCE(s.timing_revision, 1),
			COALESCE(s.visual, ''),
			COALESCE(s.camera, ''),
			COALESCE(s.motion, ''),
			COALESCE(s.mood, ''),
			COALESCE(s.image_prompt, ''),
			COALESCE(s.image_prompt_status, 'not_started'),
			COALESCE(s.image_prompt_error_code, ''),
			COALESCE(s.image_prompt_error_message, ''),
			COALESCE(s.image_prompt_workflow_run_id::text, ''),
			COALESCE(s.video_prompt, ''),
			COALESCE(s.script_dialogue, '[]'::jsonb),
			COALESCE(s.video_prompt_status, 'not_started'),
			COALESCE(s.video_prompt_error_code, ''),
			COALESCE(s.video_prompt_error_message, ''),
			COALESCE(s.video_prompt_workflow_run_id::text, ''),
			COALESCE(s.video_reference_mode, 'auto'),
			COALESCE(s.video_reference_keys, ARRAY[]::text[]),
			COALESCE(s.image_artifact_id::text, ''),
			COALESCE(s.image_media_file_id::text, ''),
			COALESCE(s.image_storage_key, ''),
			COALESCE(s.image_status, 'not_started'),
			COALESCE(s.video_artifact_id::text, ''),
			COALESCE(s.video_media_file_id::text, ''),
			COALESCE(s.video_storage_key, ''),
			COALESCE(s.video_provider_async_task_id::text, ''),
			COALESCE(s.video_external_task_id, ''),
			COALESCE(s.status, 'pending'),
			COALESCE(s.manual_override, false),
			COALESCE(s.stale_state, 'fresh')
		FROM storyboard_shots s
		JOIN project_video_production_generations generation
		  ON generation.id = s.production_generation_id
		 AND generation.project_id = s.project_id
		JOIN project_video_production_bindings binding
		  ON binding.id = generation.binding_id
		 AND binding.project_id = s.project_id
		JOIN LATERAL (
		  SELECT
		    NULLIF(binding.profile_snapshot #>> '{productionConfiguration,timelineTimebase}', '')::bigint AS timeline_timebase,
		    NULLIF(binding.profile_snapshot #>> '{productionConfiguration,fpsNumerator}', '')::integer AS fps_numerator,
		    NULLIF(binding.profile_snapshot #>> '{productionConfiguration,fpsDenominator}', '')::integer AS fps_denominator
		) p ON p.timeline_timebase > 0 AND p.fps_numerator > 0 AND p.fps_denominator > 0
		WHERE ` + where
}

func (a Activities) listStoryboardShots(ctx context.Context, workflowRunID string) ([]StoryboardShotRecord, error) {
	rows, err := a.db.Query(ctx, storyboardShotRecordSelectSQL(`
		s.workflow_run_id = $1
		  AND s.deleted_at IS NULL
		ORDER BY s.episode_index ASC NULLS LAST, s.episode_shot_index ASC NULLS LAST, s.start_tick ASC
	`), workflowRunID)
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

func (a Activities) storyboardShot(ctx context.Context, projectID, workflowRunID, shotID string, shotIndex int) (StoryboardShotRecord, error) {
	args := []any{workflowRunID}
	where := `s.workflow_run_id = $1`
	if strings.TrimSpace(shotID) != "" {
		where = `s.project_id = $1 AND s.id = $2`
		args[0] = projectID
		args = append(args, shotID)
	} else {
		where += ` AND s.shot_index = $2`
		args = append(args, shotIndex)
	}
	return scanStoryboardShotRecord(a.db.QueryRow(ctx, storyboardShotRecordSelectSQL(where+` AND s.deleted_at IS NULL`), args...))
}

func scanStoryboardShotRecord(row pgx.Row) (StoryboardShotRecord, error) {
	var shot StoryboardShotRecord
	var dialogue []byte
	err := row.Scan(
		&shot.ID,
		&shot.WorkflowRunID,
		&shot.ScriptSceneID,
		&shot.ScriptEpisodeID,
		&shot.EpisodeIndex,
		&shot.EpisodeShotIndex,
		&shot.ShotIndex,
		&shot.ShotNo,
		&shot.Title,
		&shot.StoryboardPlanID,
		&shot.StartTick,
		&shot.EndTick,
		&shot.PlannedDurationTicks,
		&shot.Duration,
		&shot.TimelineTimebase,
		&shot.FPSNumerator,
		&shot.FPSDenominator,
		&shot.DurationSource,
		&shot.TimingConfidence,
		&shot.DurationLocked,
		&shot.OneTake,
		&shot.TimingRevision,
		&shot.Visual,
		&shot.Camera,
		&shot.Motion,
		&shot.Mood,
		&shot.ImagePrompt,
		&shot.ImagePromptStatus,
		&shot.ImagePromptErrorCode,
		&shot.ImagePromptErrorMessage,
		&shot.ImagePromptWorkflowRunID,
		&shot.VideoPrompt,
		&dialogue,
		&shot.VideoPromptStatus,
		&shot.VideoPromptErrorCode,
		&shot.VideoPromptErrorMessage,
		&shot.VideoPromptWorkflowRunID,
		&shot.VideoReferenceMode,
		&shot.VideoReferenceKeys,
		&shot.ImageArtifactID,
		&shot.ImageMediaFileID,
		&shot.ImageStorageKey,
		&shot.ImageStatus,
		&shot.VideoArtifactID,
		&shot.VideoMediaFileID,
		&shot.VideoStorageKey,
		&shot.VideoProviderAsyncTaskID,
		&shot.VideoExternalTaskID,
		&shot.Status,
		&shot.ManualOverride,
		&shot.StaleState,
	)
	if err == nil {
		if decodeErr := json.Unmarshal(dialogue, &shot.Dialogue); decodeErr != nil {
			return StoryboardShotRecord{}, decodeErr
		}
		shot.Dialogue = NormalizeStoryboardDialogue(shot.Dialogue)
	}
	return shot, err
}

func (a Activities) updateStoryboardShotStatus(ctx context.Context, shotID, status string) error {
	_, err := a.db.Exec(ctx, `UPDATE storyboard_shots SET status = $2, updated_at = now() WHERE id = $1`, shotID, status)
	return err
}

func (a Activities) markShotImageStarted(ctx context.Context, input GenerateShotImageInput, shot StoryboardShotRecord, nodeExecution NodeExecution) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET status = 'generating', review_status = 'pending',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'imageGeneration', jsonb_build_object(
		        'status', 'running', 'workflowRunId', $3::text,
		        'nodeRunId', $4::text, 'startedAt', now()
		      )
		    )
		WHERE id = (
			SELECT id FROM shot_visual_anchors
			WHERE storyboard_shot_id = $1 AND anchor_role = $2 AND status <> 'archived'
			ORDER BY revision DESC LIMIT 1
		)
	`, shot.ID, input.AnchorRole, input.WorkflowRunID, nodeExecution.NodeRunID); err != nil {
		return err
	}
	if err := updateShotProductionStatusTx(ctx, tx, shot.ID, input.WorkflowRunID, "image_running", "", ""); err != nil {
		return err
	}
	payload := storyboardShotEventPayload(input.WorkflowRunID, shot, "image_running")
	var payloadValue map[string]any
	_ = json.Unmarshal(payload, &payloadValue)
	payloadValue["anchorRole"] = input.AnchorRole
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image.started", "storyboard_shot", shot.ID, mustJSON(payloadValue)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func updateShotProductionStatusTx(ctx context.Context, tx pgx.Tx, shotID, workflowRunID, status, code, message string) error {
	switch status {
	case "image_running":
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = $2,
			    image_status = 'running',
			    image_error_code = NULL,
			    image_error_message = NULL,
			    image_started_at = COALESCE(image_started_at, now()),
			    image_completed_at = NULL,
			    image_workflow_run_id = NULLIF($3, '')::uuid,
			    updated_at = now()
			WHERE id = $1
		`, shotID, status, workflowRunID)
		return err
	case "image_failed":
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = $2,
			    image_status = 'failed',
			    image_error_code = NULLIF($3, ''),
			    image_error_message = NULLIF($4, ''),
			    image_completed_at = now(),
			    updated_at = now()
			WHERE id = $1
		`, shotID, status, code, message)
		return err
	case "video_running":
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = $2,
			    video_status = 'running',
			    video_error_code = NULL,
			    video_error_message = NULL,
			    video_started_at = COALESCE(video_started_at, now()),
			    video_completed_at = NULL,
			    video_workflow_run_id = NULLIF($3, '')::uuid,
			    updated_at = now()
			WHERE id = $1
		`, shotID, status, workflowRunID)
		return err
	case "video_failed":
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = $2,
			    video_status = 'failed',
			    video_error_code = NULLIF($3, ''),
			    video_error_message = NULLIF($4, ''),
			    video_completed_at = now(),
			    updated_at = now()
			WHERE id = $1
		`, shotID, status, code, message)
		return err
	case "cancelled":
		_, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET status = $2,
			    video_status = 'cancelled',
			    video_error_code = NULLIF($3, ''),
			    video_error_message = NULLIF($4, ''),
			    video_completed_at = now(),
			    updated_at = now()
			WHERE id = $1
		`, shotID, status, code, message)
		return err
	default:
		_, err := tx.Exec(ctx, `UPDATE storyboard_shots SET status = $2, updated_at = now() WHERE id = $1`, shotID, status)
		return err
	}
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

func (a Activities) failShotActivity(ctx context.Context, input TextToStoryboardInput, shot StoryboardShotRecord, nodeExecution NodeExecution, status, eventType string, cause error) error {
	if isWorkflowWriteFenced(cause) {
		return discardWorkflowResult(ctx, a.db, nodeExecution, cause.Error())
	}
	code, message := workflowErrorFields(cause, codeActivityFailed)
	if !nodeExecution.valid() {
		return newWorkflowApplicationError(cause, code, message)
	}
	persistCtx, cancel := workflowFailurePersistenceContext(ctx)
	defer cancel()
	tx, err := a.db.Begin(persistCtx)
	if err == nil {
		defer tx.Rollback(persistCtx)
		_, err = lockNodeBusinessWrite(persistCtx, tx, input.WorkflowRunID, nodeExecution)
	}
	if err == nil && strings.TrimSpace(shot.ID) != "" {
		err = updateShotProductionStatusTx(persistCtx, tx, shot.ID, input.WorkflowRunID, status, code, message)
	}
	if err == nil && strings.TrimSpace(shot.ID) != "" {
		err = insertEvent(persistCtx, tx, input.OrganizationID, input.ProjectID, eventType, "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, status))
	}
	if err == nil {
		_, err = failNodeRunTx(persistCtx, tx, nodeExecution, code, message, json.RawMessage(`{}`))
	}
	if err == nil && shouldTransitionWorkflowOnActivityFailure(input) {
		_, _, err = transitionWorkflowRunTx(persistCtx, tx, input.WorkflowRunID, "failed", code, message, mustJSON(map[string]any{
			"code": code, "message": message,
		}))
	}
	if err == nil {
		err = tx.Commit(persistCtx)
	}
	if err != nil {
		return newWorkflowApplicationError(cause, code, message)
	}
	return newWorkflowApplicationError(cause, code, message)
}

func (a Activities) recordPlannedVideoSegmentFailure(ctx context.Context, input TextToStoryboardInput, shot StoryboardShotRecord, nodeExecution NodeExecution, code, message string) error {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" {
		code = codeActivityFailed
	}
	if message == "" {
		message = "provider video render segment failed"
	}
	persistCtx, cancel := workflowFailurePersistenceContext(ctx)
	defer cancel()
	tx, err := a.db.Begin(persistCtx)
	if err != nil {
		return err
	}
	defer tx.Rollback(persistCtx)
	if _, err := lockNodeBusinessWrite(persistCtx, tx, input.WorkflowRunID, nodeExecution); err != nil {
		return err
	}
	if err := updateShotProductionStatusTx(persistCtx, tx, shot.ID, input.WorkflowRunID, "video_failed", code, message); err != nil {
		return err
	}
	if err := insertEvent(persistCtx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.segment_failed", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, "video_failed")); err != nil {
		return err
	}
	if applied, err := failNodeRunTx(persistCtx, tx, nodeExecution, code, message, json.RawMessage(`{}`)); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(persistCtx)
}

func isTerminalVideoAttemptFailure(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "cancelled", "blocked":
		return true
	default:
		return false
	}
}

func (a Activities) completeShotImage(ctx context.Context, input GenerateShotImageInput, shot StoryboardShotRecord, output GenerateShotImageOutput) error {
	project, err := a.projectProductionSettings(ctx, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, output.nodeExecution()); err != nil {
		return err
	}
	anchorID, err := completePlannedAnchorTx(
		ctx, tx, input, shot, output,
		inputProfileKey(input), input.AnchorRole, project.TimelineTimebase,
	)
	if err != nil {
		return err
	}
	output.VisualAnchorID = anchorID
	profileKey := inputProfileKey(input)
	allAnchorsReady, err := profileRequiredAnchorsReadyTx(ctx, tx, shot.ID, profileKey)
	if err != nil {
		return err
	}
	isPrimaryVisual := input.AnchorRole == videoproduction.AnchorRolePlannedFirstFrame || input.AnchorRole == videoproduction.AnchorRoleStoryboardSheet
	aggregateStatus := "image_running"
	aggregateImageStatus := "running"
	if allAnchorsReady {
		aggregateStatus = "image_succeeded"
		aggregateImageStatus = "succeeded"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_artifact_id = CASE WHEN $6 THEN NULLIF($2, '')::uuid ELSE image_artifact_id END,
		    image_media_file_id = CASE WHEN $6 THEN NULLIF($3, '')::uuid ELSE image_media_file_id END,
		    image_storage_key = CASE WHEN $6 THEN NULLIF($4, '') ELSE image_storage_key END,
		    status = $7,
		    image_status = $8,
		    image_error_code = NULL,
		    image_error_message = NULL,
		    image_started_at = COALESCE(image_started_at, now()),
		    image_completed_at = CASE WHEN $9 THEN now() ELSE NULL END,
		    image_workflow_run_id = NULLIF($5, '')::uuid,
		    metadata = CASE WHEN $6 THEN
		      (COALESCE(metadata, '{}'::jsonb)
		        - 'imageAspectRatioMismatch'
		        - 'imageAspectRatioMismatchDetectedAt')
		        || jsonb_build_object('imageAspectRatioValidatedAt', now())
		      ELSE COALESCE(metadata, '{}'::jsonb)
		    END,
		    stale_state = CASE
		      WHEN $9
		       AND active_video_render_plan_id IS NULL
		       AND video_artifact_id IS NULL
		       AND video_media_file_id IS NULL
		       AND COALESCE(video_storage_key, '') = ''
		       AND video_provider_async_task_id IS NULL
		       AND COALESCE(video_external_task_id, '') = ''
		       AND COALESCE(video_status, 'not_started') IN ('not_started', 'failed', 'cancelled')
		       AND COALESCE(video_prompt_status, 'not_started') IN ('not_started', 'failed')
		       AND EXISTS (
		         SELECT 1
		         FROM storyboard_plans plan
		         WHERE plan.id = storyboard_shots.storyboard_plan_id
		           AND plan.active = true
		           AND COALESCE(plan.stale_state, 'fresh') = 'fresh'
		       )
		      THEN 'fresh'
		      ELSE stale_state
		    END,
		    updated_at = now()
		WHERE id = $1
	`, shot.ID, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey,
		input.WorkflowRunID, isPrimaryVisual, aggregateStatus, aggregateImageStatus, allAnchorsReady); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.image.completed", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, StoryboardShotRecord{
		ID:        shot.ID,
		ShotIndex: shot.ShotIndex,
		ShotNo:    shot.ShotNo,
		Status:    aggregateStatus,
	}, aggregateStatus)); err != nil {
		return err
	}
	if strings.TrimSpace(output.ImageArtifactID) != "" {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", output.ImageArtifactID, mustJSON(map[string]any{
			"artifactId":     output.ImageArtifactID,
			"workflowRunId":  input.WorkflowRunID,
			"nodeRunId":      output.NodeRunID,
			"shotId":         shot.ID,
			"shotIndex":      shot.ShotIndex,
			"storageKey":     output.ImageStorageKey,
			"type":           "generated_image",
			"mediaFileId":    output.ImageMediaFileID,
			"anchorRole":     input.AnchorRole,
			"visualAnchorId": anchorID,
		})); err != nil {
			return err
		}
	}
	outputJSON := mustJSON(output)
	if applied, err := completeNodeRunTx(ctx, tx, output.nodeExecution(), outputJSON); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func completePlannedAnchorTx(
	ctx context.Context,
	tx pgx.Tx,
	input GenerateShotImageInput,
	shot StoryboardShotRecord,
	output GenerateShotImageOutput,
	profileKey string,
	anchorRole string,
	timelineTimebase int64,
) (string, error) {
	anchorRole = strings.TrimSpace(anchorRole)
	if anchorRole == "" {
		anchorRole = videoproduction.AnchorRolePlannedFirstFrame
	}
	stateRole, err := profileAnchorStateRole(profileKey, anchorRole)
	if err != nil {
		return "", err
	}
	type anchorRecord struct {
		ID                     string
		Revision               int
		Status                 string
		ReviewStatus           string
		ArtifactID             string
		StateVersionID         string
		ProductionGenerationID string
	}
	var current anchorRecord
	err = tx.QueryRow(ctx, `
		SELECT id::text, revision, status, review_status,
		       COALESCE(artifact_id::text, ''), COALESCE(shot_state_version_id::text, ''),
		       production_generation_id::text
		FROM shot_visual_anchors
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND anchor_role = $3
		ORDER BY revision DESC
		LIMIT 1
		FOR UPDATE
	`, input.ProjectID, shot.ID, anchorRole).Scan(
		&current.ID, &current.Revision, &current.Status, &current.ReviewStatus,
		&current.ArtifactID, &current.StateVersionID, &current.ProductionGenerationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `
			SELECT state.id::text, shot.production_generation_id::text
			FROM storyboard_shots shot
			JOIN storyboard_shot_state_versions state
			  ON state.storyboard_shot_id = shot.id
			 AND state.production_generation_id = shot.production_generation_id
			 AND state.state_role = $3
			 AND state.status = 'approved'
			WHERE shot.project_id = $1 AND shot.id = $2
		`, input.ProjectID, shot.ID, stateRole).Scan(&current.StateVersionID, &current.ProductionGenerationID); err != nil {
			return "", err
		}
		current.Revision = 1
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status, metadata
			)
			VALUES ($1, $2, $3, $4, $5, $6, 1, 'generating', 'pending',
			        jsonb_build_object('workflowRunId', $7::text, 'source', 'shot_image_generation'))
			RETURNING id::text
		`, input.OrganizationID, input.ProjectID, current.ProductionGenerationID, shot.ID,
			current.StateVersionID, anchorRole, input.WorkflowRunID).Scan(&current.ID); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

	if current.ArtifactID == output.ImageArtifactID && current.Status == "ready" && current.ReviewStatus == "approved" {
		return current.ID, nil
	}
	if current.Status == "ready" || current.Status == "stale" || current.Status == "archived" || current.ArtifactID != "" {
		previousID := current.ID
		if _, err := tx.Exec(ctx, `
			UPDATE shot_visual_anchors
			SET status = 'stale', review_status = 'needs_edit', reference_pack_id = NULL,
			    metadata = metadata || jsonb_build_object('supersededAt', now())
			WHERE id = $1
		`, previousID); err != nil {
			return "", err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO shot_visual_anchors(
				organization_id, project_id, production_generation_id, storyboard_shot_id,
				shot_state_version_id, anchor_role, revision, status, review_status, metadata
			)
			SELECT organization_id, project_id, production_generation_id, storyboard_shot_id,
			       shot_state_version_id, anchor_role, revision + 1, 'generating', 'pending',
			       metadata || jsonb_build_object(
			         'workflowRunId', $2::text, 'source', 'shot_image_regeneration',
			         'previousAnchorId', id::text
			       )
			FROM shot_visual_anchors WHERE id = $1
			RETURNING id::text, revision, production_generation_id::text
		`, previousID, input.WorkflowRunID).Scan(&current.ID, &current.Revision, &current.ProductionGenerationID); err != nil {
			return "", err
		}
	}

	finalReviewStatus := "approved"
	approvalSource := "successful_shot_image_generation"
	if profileKey == videoproduction.ProfileFirstLastFrame || profileKey == videoproduction.ProfileStoryboardSheet {
		finalReviewStatus = "pending"
		approvalSource = "awaiting_profile_output_review"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors anchor
		SET status = 'ready',
		    review_status = $7,
		    artifact_id = NULLIF($2, '')::uuid,
		    media_file_id = NULLIF($3, '')::uuid,
		    storage_key = NULLIF($4, ''),
		    prompt = COALESCE(NULLIF(anchor.prompt, ''), CASE WHEN $8 = 'planned_first_frame' THEN shot.image_prompt ELSE '' END),
		    prompt_version_id = COALESCE(
		      anchor.prompt_version_id,
		      CASE WHEN $8 = 'planned_first_frame' THEN NULLIF(shot.metadata #>> '{imagePromptAgent,generationPromptVersionId}', '')::uuid ELSE NULL END
		    ),
		    prompt_hash = COALESCE(
		      anchor.prompt_hash,
		      CASE WHEN $8 = 'planned_first_frame' THEN NULLIF(regexp_replace(shot.metadata #>> '{imagePromptAgent,promptHash}', '^sha256:', ''), '') ELSE NULL END
		    ),
		    provider_call_id = NULLIF($5, '')::uuid,
		    model_id = COALESCE(call.provider_model_id, anchor.model_id),
		    reference_pack_id = NULL,
		    metadata = anchor.metadata || jsonb_build_object(
		      'completedAt', now(),
		      'reviewUpdatedAt', now(),
		      'approvalSource', $9::text,
		      'workflowRunId', $6::text,
		      'providerCallId', $5::text,
		      'artifactId', $2::text,
		      'mediaFileId', $3::text
		    )
		FROM storyboard_shots shot
		LEFT JOIN provider_call_logs call ON call.id = NULLIF($5, '')::uuid
		WHERE anchor.id = $1 AND shot.id = anchor.storyboard_shot_id
	`, current.ID, output.ImageArtifactID, output.ImageMediaFileID, output.ImageStorageKey,
		output.ProviderCallID, input.WorkflowRunID, finalReviewStatus, anchorRole, approvalSource); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_visual_anchors
		SET status = 'stale', review_status = 'needs_edit', reference_pack_id = NULL,
		    metadata = metadata || jsonb_build_object('supersededAt', now(), 'supersededByAnchorId', $3::text)
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND anchor_role = $4
		  AND id <> $3 AND status = 'ready' AND review_status = 'approved'
	`, input.ProjectID, shot.ID, current.ID, anchorRole); err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE shot_reference_packs SET status = 'stale'
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND status = 'active'
	`, input.ProjectID, shot.ID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE prompt_context_plans SET status = 'stale', stale_at = now()
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND status = 'active'
	`, input.ProjectID, shot.ID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE video_prompt_plans
		SET status = 'stale', stale_at = now(),
		    metadata = metadata || jsonb_build_object('staleReason', 'planned_anchor_changed', 'anchorRole', $3::text, 'staleAt', now())
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND status NOT IN ('stale', 'archived')
	`, input.ProjectID, shot.ID, anchorRole); err != nil {
		return "", err
	}
	var staleRenderPlanCount int64
	if result, err := tx.Exec(ctx, `
		UPDATE video_render_plans
		SET active = false,
		    status = CASE WHEN status IN ('planned', 'running', 'succeeded') THEN 'stale' ELSE status END,
		    metadata = metadata || jsonb_build_object('staleReason', 'planned_anchor_changed', 'anchorRole', $3::text, 'staleAt', now()),
		    updated_at = now()
		WHERE project_id = $1 AND storyboard_shot_id = $2 AND active = true
	`, input.ProjectID, shot.ID, anchorRole); err != nil {
		return "", err
	} else {
		staleRenderPlanCount = result.RowsAffected()
	}
	if staleRenderPlanCount > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET active_video_render_plan_id = NULL,
			    video_status = CASE WHEN video_status IN ('queued', 'running', 'succeeded') THEN 'stale' ELSE video_status END,
			    production_readiness = 'preview_only', stale_state = 'needs_regeneration', updated_at = now()
			WHERE project_id = $1 AND id = $2
		`, input.ProjectID, shot.ID); err != nil {
			return "", err
		}
		if err := production.MarkFinalVideoStale(ctx, tx, input.ProjectID, ""); err != nil {
			return "", err
		}
	}
	pairReviewed := false
	if profileKey == videoproduction.ProfileFirstLastFrame {
		pairReviewed, err = reviewFirstLastAnchorPairTx(ctx, tx, input.ProjectID, shot.ID, timelineTimebase)
		if err != nil {
			return "", err
		}
		if pairReviewed {
			finalReviewStatus = "approved"
			approvalSource = "deterministic_first_last_pair_review"
		}
	}

	var generationID, bindingID, episodeID, sourceWorkflowRunID string
	var bindingRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT shot.production_generation_id::text, generation.binding_id::text,
		       binding.revision, plan.script_episode_id::text,
		       COALESCE(shot.workflow_run_id::text, $3::text)
		FROM storyboard_shots shot
		JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
		JOIN project_video_production_generations generation ON generation.id = shot.production_generation_id
		JOIN project_video_production_bindings binding ON binding.id = generation.binding_id
		WHERE shot.project_id = $1 AND shot.id = $2
	`, input.ProjectID, shot.ID, input.WorkflowRunID).Scan(
		&generationID, &bindingID, &bindingRevision, &episodeID, &sourceWorkflowRunID,
	); err != nil {
		return "", err
	}
	payload := map[string]any{
		"bindingId": bindingID, "bindingRevision": bindingRevision,
		"productionGenerationId": generationID, "episodeId": episodeID,
		"storyboardShotId": shot.ID, "workflowRunId": input.WorkflowRunID,
		"sourceWorkflowRunId": sourceWorkflowRunID, "anchorId": current.ID,
		"revision": current.Revision, "artifactId": output.ImageArtifactID,
		"mediaFileId": output.ImageMediaFileID, "providerCallId": output.ProviderCallID,
		"anchorRole": anchorRole,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.anchor.completed", "shot_visual_anchor", current.ID, mustJSON(payload)); err != nil {
		return "", err
	}
	if finalReviewStatus == "approved" {
		reviewedAnchors := []struct{ ID, Role string }{{ID: current.ID, Role: anchorRole}}
		if pairReviewed {
			reviewedAnchors = reviewedAnchors[:0]
			rows, queryErr := tx.Query(ctx, `
				SELECT DISTINCT ON (anchor_role) id::text, anchor_role
				FROM shot_visual_anchors
				WHERE storyboard_shot_id = $1
				  AND anchor_role IN ('planned_first_frame', 'planned_last_frame')
				  AND status = 'ready' AND review_status = 'approved'
				ORDER BY anchor_role, revision DESC
			`, shot.ID)
			if queryErr != nil {
				return "", queryErr
			}
			for rows.Next() {
				var reviewed struct{ ID, Role string }
				if scanErr := rows.Scan(&reviewed.ID, &reviewed.Role); scanErr != nil {
					rows.Close()
					return "", scanErr
				}
				reviewedAnchors = append(reviewedAnchors, reviewed)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				rows.Close()
				return "", rowsErr
			}
			rows.Close()
		}
		for _, reviewed := range reviewedAnchors {
			reviewPayload := make(map[string]any, len(payload)+3)
			for key, value := range payload {
				reviewPayload[key] = value
			}
			reviewPayload["anchorId"] = reviewed.ID
			reviewPayload["anchorRole"] = reviewed.Role
			reviewPayload["decision"] = "approved"
			reviewPayload["reviewSource"] = approvalSource
			if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.anchor.reviewed", "shot_visual_anchor", reviewed.ID, mustJSON(reviewPayload)); err != nil {
				return "", err
			}
		}
	}
	return current.ID, nil
}

func completePlannedFirstFrameAnchorTx(
	ctx context.Context,
	tx pgx.Tx,
	input GenerateShotImageInput,
	shot StoryboardShotRecord,
	output GenerateShotImageOutput,
) (string, error) {
	return completePlannedAnchorTx(
		ctx, tx, input, shot, output,
		videoproduction.ProfileSingleFrameI2V,
		videoproduction.AnchorRolePlannedFirstFrame,
		90_000,
	)
}

func (a Activities) recoverCompletedShotImage(ctx context.Context, input GenerateShotImageInput, nodeExecution NodeExecution) (GenerateShotImageOutput, bool, error) {
	var providerCallID string
	var normalizedOutput json.RawMessage
	err := a.db.QueryRow(ctx, `
		SELECT id::text, normalized_output
		FROM provider_call_logs
		WHERE organization_id = $1
		  AND project_id = $2
		  AND workflow_run_id = $3
		  AND node_run_id = $4
		  AND task_type = 'image.generate'
		  AND status = 'succeeded'
		  AND jsonb_array_length(COALESCE(artifact_ids, '[]'::jsonb)) > 0
		  AND jsonb_array_length(COALESCE(media_file_ids, '[]'::jsonb)) > 0
		ORDER BY completed_at DESC NULLS LAST, created_at DESC
		LIMIT 1
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeExecution.NodeRunID).Scan(&providerCallID, &normalizedOutput)
	if errors.Is(err, pgx.ErrNoRows) {
		return GenerateShotImageOutput{}, false, nil
	}
	if err != nil {
		return GenerateShotImageOutput{}, false, err
	}
	var gatewayOutput provider.GatewayImageOutput
	if err := json.Unmarshal(normalizedOutput, &gatewayOutput); err != nil {
		return GenerateShotImageOutput{}, false, fmt.Errorf("decode completed image gateway output: %w", err)
	}
	if strings.TrimSpace(gatewayOutput.ArtifactID) == "" || strings.TrimSpace(gatewayOutput.MediaFileID) == "" || strings.TrimSpace(gatewayOutput.StorageKey) == "" {
		return GenerateShotImageOutput{}, false, nil
	}
	return GenerateShotImageOutput{
		NodeRunID:         nodeExecution.NodeRunID,
		ExecutionToken:    nodeExecution.ExecutionToken,
		AttemptGeneration: nodeExecution.AttemptGeneration,
		ShotID:            input.ShotID,
		AnchorRole:        input.AnchorRole,
		ProviderCallID:    providerCallID,
		ImageArtifactID:   gatewayOutput.ArtifactID,
		ImageMediaFileID:  gatewayOutput.MediaFileID,
		ImageStorageKey:   gatewayOutput.StorageKey,
	}, true, nil
}

func (a Activities) markShotVideoCreated(ctx context.Context, input CreateShotVideoTaskInput, shot StoryboardShotRecord, output CreateShotVideoTaskOutput) error {
	if strings.TrimSpace(output.RenderSegmentID) != "" {
		return a.markShotVideoSegmentCreated(ctx, input, shot, output)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, output.nodeExecution()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_provider_async_task_id = $2,
		    video_external_task_id = NULLIF($3, ''),
		    status = 'video_running',
		    video_status = 'running',
		    video_error_code = NULL,
		    video_error_message = NULL,
		    video_started_at = COALESCE(video_started_at, now()),
		    video_completed_at = NULL,
		    video_workflow_run_id = NULLIF($4, '')::uuid,
		    updated_at = now()
		WHERE id = $1
	`, shot.ID, nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID, input.WorkflowRunID); err != nil {
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
	if applied, err := progressNodeRunTx(ctx, tx, output.nodeExecution(), outputJSON); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) markShotVideoPolled(ctx context.Context, input PollShotVideoTaskInput, shot StoryboardShotRecord, output PollShotVideoTaskOutput) error {
	if strings.TrimSpace(output.RenderSegmentID) != "" {
		return a.markShotVideoSegmentPolled(ctx, input, shot, output)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, input.nodeExecution()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'video_running',
		    video_status = 'running',
		    video_started_at = COALESCE(video_started_at, now()),
		    updated_at = now()
		WHERE id = $1
	`, shot.ID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "storyboard.shot.video.polled", "storyboard_shot", shot.ID, storyboardShotEventPayload(input.WorkflowRunID, shot, "video_running")); err != nil {
		return err
	}
	if applied, err := progressNodeRunTx(ctx, tx, input.nodeExecution(), outputJSON); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) completeShotVideo(ctx context.Context, input PollShotVideoTaskInput, shot StoryboardShotRecord, output PollShotVideoTaskOutput) error {
	if strings.TrimSpace(output.RenderSegmentID) != "" {
		return a.completeShotVideoSegment(ctx, input, shot, output)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	outputJSON := mustJSON(output)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, input.nodeExecution()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET video_artifact_id = $2,
		    video_media_file_id = $3,
		    video_storage_key = NULLIF($4, ''),
		    video_provider_async_task_id = $5,
		    video_external_task_id = NULLIF($6, ''),
		    status = 'video_succeeded',
		    video_status = 'succeeded',
		    video_error_code = NULL,
		    video_error_message = NULL,
		    video_started_at = COALESCE(video_started_at, now()),
		    video_completed_at = now(),
		    video_workflow_run_id = NULLIF($7, '')::uuid,
		    stale_state = 'fresh',
		    updated_at = now()
		WHERE id = $1
	`, shot.ID, nullIfEmpty(output.ArtifactID), nullIfEmpty(output.MediaFileID), output.StorageKey, nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID, input.WorkflowRunID); err != nil {
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
	if applied, err := completeNodeRunTx(ctx, tx, input.nodeExecution(), outputJSON); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) markShotVideoSegmentCreated(ctx context.Context, input CreateShotVideoTaskInput, shot StoryboardShotRecord, output CreateShotVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, output.nodeExecution()); err != nil {
		return err
	}
	shotTag, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'video_running', video_status = 'running',
		    video_error_code = NULL, video_error_message = NULL,
		    video_started_at = COALESCE(video_started_at, now()), video_completed_at = NULL,
		    video_workflow_run_id = NULLIF($3, '')::uuid,
		    updated_at = now()
		WHERE id = $1 AND active_video_render_plan_id = NULLIF($2, '')::uuid
	`, shot.ID, output.ExecutionPlanID, input.WorkflowRunID)
	if err != nil {
		return err
	}
	if shotTag.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if applied, err := progressNodeRunTx(ctx, tx, output.nodeExecution(), mustJSON(output)); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) markShotVideoSegmentPolled(ctx context.Context, input PollShotVideoTaskInput, shot StoryboardShotRecord, output PollShotVideoTaskOutput) error {
	return ProgressNodeRun(ctx, a.db, input.nodeExecution(), mustJSON(output))
}

func (a Activities) completeShotVideoSegment(ctx context.Context, input PollShotVideoTaskInput, shot StoryboardShotRecord, output PollShotVideoTaskOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, input.nodeExecution()); err != nil {
		return err
	}
	if output.SegmentCount <= 1 {
		if _, err := tx.Exec(ctx, `
			UPDATE storyboard_shots
			SET video_artifact_id = $2, video_media_file_id = $3, video_storage_key = NULLIF($4, ''),
			    video_provider_async_task_id = $5, video_external_task_id = NULLIF($6, ''),
			    video_workflow_run_id = NULLIF($7, '')::uuid, video_completed_at = now(), updated_at = now()
			WHERE id = $1 AND active_video_render_plan_id = NULLIF($8, '')::uuid
		`, shot.ID, nullIfEmpty(output.ArtifactID), nullIfEmpty(output.MediaFileID), output.StorageKey,
			nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID, input.WorkflowRunID, output.ExecutionPlanID); err != nil {
			return err
		}
	}
	outputJSON := mustJSON(output)
	if strings.TrimSpace(output.ArtifactID) != "" {
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", output.ArtifactID, mustJSON(map[string]any{
			"artifactId": output.ArtifactID, "workflowRunId": input.WorkflowRunID, "nodeRunId": input.NodeRunID,
			"shotId": shot.ID, "shotIndex": shot.ShotIndex, "executionPlanId": output.ExecutionPlanID,
			"renderSegmentId": output.RenderSegmentID, "segmentIndex": output.SegmentIndex,
			"storageKey": output.StorageKey, "type": "generated_video", "mediaFileId": output.MediaFileID,
			"providerAsyncTaskId": output.ProviderAsyncTaskID, "externalTaskId": output.ExternalTaskID,
		})); err != nil {
			return err
		}
	}
	if applied, err := completeNodeRunTx(ctx, tx, input.nodeExecution(), outputJSON); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) cancelShotVideoSegment(ctx context.Context, input CancelShotVideoTaskInput, output CancelShotVideoTaskOutput) error {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "Video render segment cancellation requested"
	}
	return CancelNodeRun(ctx, a.db, input.NodeRunID, mustJSON(output), reason)
}

func (a Activities) cancelStoryboardShot(ctx context.Context, input CancelShotVideoTaskInput, output CancelShotVideoTaskOutput) error {
	if strings.TrimSpace(output.RenderSegmentID) != "" {
		return a.cancelShotVideoSegment(ctx, input, output)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'cancelled',
		    video_status = 'cancelled',
		    video_provider_async_task_id = COALESCE($2, video_provider_async_task_id),
		    video_external_task_id = COALESCE(NULLIF($3, ''), video_external_task_id),
		    video_error_code = CASE WHEN $4 = 'cancel_failed' THEN 'CANCEL_FAILED' ELSE NULL END,
		    video_error_message = NULLIF($5, ''),
		    video_completed_at = now(),
		    updated_at = now()
		WHERE id = $1
	`, input.ShotID, nullIfEmpty(output.ProviderAsyncTaskID), output.ExternalTaskID, output.Status, output.ErrorMessage); err != nil {
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
		SELECT id::text, execution_token::text, attempt_generation, COALESCE(output, '{}'::jsonb)
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		  AND node_key = $2
		  AND status IN ('running', 'succeeded')
	`, workflowRunID, nodeGenerateStoryboardVideoKey).Scan(&output.NodeRunID, &output.ExecutionToken, &output.AttemptGeneration, &raw)
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
	if _, err := lockNodeBusinessWrite(ctx, tx, input.WorkflowRunID, input.nodeExecution()); err != nil {
		return err
	}
	outputJSON := mustJSON(output)
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
	if applied, err := completeNodeRunTx(ctx, tx, input.nodeExecution(), outputJSON); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func videoTaskIdempotencyKey(workflowRunID string) string {
	return workflowRunID + ":" + nodeGenerateStoryboardVideoKey
}

func shotVideoTaskIdempotencyKey(workflowRunID string, shotIndex int) string {
	return fmt.Sprintf("%s:%s:%d", workflowRunID, nodeCreateShotVideoPrefix, shotIndex)
}

func shotVideoSegmentIdempotencyKey(workflowRunID string, shotIndex int, executionPlanID string, segmentIndex, retryGeneration int) string {
	if strings.TrimSpace(executionPlanID) == "" {
		return shotVideoTaskIdempotencyKey(workflowRunID, shotIndex)
	}
	return fmt.Sprintf("render-segment:%s:%d:%s:%d:%d", workflowRunID, shotIndex, executionPlanID, segmentIndex, retryGeneration)
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

func storyboardImageSizeForAspectRatio(value string) string {
	normalized := strings.NewReplacer(" ", "", "/", ":").Replace(strings.TrimSpace(value))
	if size, ok := storyboardImageSizesByAspectRatio[normalized]; ok {
		return size
	}
	return "1536x864"
}

func shotVideoReferencesStoryboardImage(references ShotVideoReferenceContext, shotID string) bool {
	expectedKey := "shot_image:" + strings.TrimSpace(shotID)
	for _, key := range references.ResolvedReferenceKeys {
		if key == expectedKey {
			return true
		}
	}
	return false
}

func (a Activities) validateShotImageAspectRatio(ctx context.Context, projectID, mediaFileID, expectedAspectRatio string) error {
	var width, height int
	if err := a.db.QueryRow(ctx, `
		SELECT COALESCE(width, 0), COALESCE(height, 0)
		FROM media_files
		WHERE id = NULLIF($1, '')::uuid AND project_id = $2
	`, mediaFileID, projectID).Scan(&width, &height); err != nil {
		return workflowError{Code: provider.CodeInvalidRequest, Message: "storyboard first-frame media is unavailable", Retryable: false, RetryabilityKnown: true}
	}
	parts := strings.FieldsFunc(strings.TrimSpace(expectedAspectRatio), func(r rune) bool {
		return r == ':' || r == '/'
	})
	if len(parts) != 2 {
		return workflowError{Code: provider.CodeInvalidRequest, Message: "project aspect ratio is invalid", Retryable: false, RetryabilityKnown: true}
	}
	ratioWidth, widthErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	ratioHeight, heightErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if widthErr != nil || heightErr != nil || ratioWidth <= 0 || ratioHeight <= 0 || width <= 0 || height <= 0 {
		return workflowError{Code: provider.CodeInvalidRequest, Message: "storyboard first-frame dimensions or project aspect ratio are invalid", Retryable: false, RetryabilityKnown: true}
	}
	expected := ratioWidth / ratioHeight
	actual := float64(width) / float64(height)
	if math.Abs(actual-expected)/expected > 0.001 {
		return workflowError{
			Code:              provider.CodeUpstreamOutputMismatch,
			Retryable:         false,
			RetryabilityKnown: true,
			Message: fmt.Sprintf(
				"storyboard first-frame layout %dx%d does not match project aspect ratio %s; regenerate the image before generating video",
				width,
				height,
				strings.TrimSpace(expectedAspectRatio),
			),
		}
	}
	return nil
}
