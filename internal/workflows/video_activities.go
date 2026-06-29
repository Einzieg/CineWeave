package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

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
		OrganizationID:  input.OrganizationID,
		ProjectID:       input.ProjectID,
		WorkflowRunID:   input.WorkflowRunID,
		NodeRunID:       nodeRunID,
		ModelProfileKey: videoGenerationModelProfileKey,
		PromptHash:      hashString(videoPrompt),
		IdempotencyKey:  videoTaskIdempotencyKey(input.WorkflowRunID),
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

func (a Activities) FailVideoProductionWorkflow(ctx context.Context, input TextToStoryboardInput, nodeRunID, code, message string) error {
	if strings.TrimSpace(nodeRunID) != "" {
		if err := FailNodeRun(ctx, a.db, nodeRunID, code, message); err != nil {
			return err
		}
	}
	return a.markWorkflowFailed(ctx, input, code, message)
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
		ProviderCalls: map[string]string{
			"storyboard":  storyboard.ProviderCallID,
			"image":       image.ProviderCallID,
			"videoCreate": create.ProviderCallID,
			"videoPoll":   poll.ProviderCallID,
		},
	}
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
