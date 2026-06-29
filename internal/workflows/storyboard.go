package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ScriptTaskQueue                 = "cineweave-script"
	scriptModelProfileKey           = "script_agent_default"
	imageGenerationModelProfileKey  = "image_generation_default"
	videoGenerationModelProfileKey  = "video_generation_default"
	codeActivityFailed              = "ACTIVITY_FAILED"
	codeModelProfileNotConfigured   = "MODEL_PROFILE_NOT_CONFIGURED"
	codeProviderVideoPollingTimeout = "PROVIDER_VIDEO_POLLING_TIMEOUT"
	codeUserCancelRequested         = "USER_CANCEL_REQUESTED"
	codeUserCancelled               = "USER_CANCELLED"
	nodeGenerateStoryboardTextKey   = "generate_storyboard_text"
	nodeGenerateStoryboardImageKey  = "generate_storyboard_image"
	nodeGenerateStoryboardVideoKey  = "generate_storyboard_video"
	promptKeyStoryboardPlanner      = "storyboard_planner"
	promptKeyStoryboardImage        = "storyboard_image_prompt"
	promptKeyStoryboardVideo        = "storyboard_video_prompt"
)

type TextToStoryboardInput struct {
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	WorkflowRunID  string          `json:"workflowRunId"`
	Prompt         string          `json:"prompt"`
	CreatedBy      string          `json:"createdBy"`
	Input          json.RawMessage `json:"input,omitempty"`
}

type TextToStoryboardOutput struct {
	StoryboardArtifactID string            `json:"storyboardArtifactId"`
	ImageArtifactID      string            `json:"imageArtifactId"`
	ImageMediaFileID     string            `json:"imageMediaFileId"`
	ImageStorageKey      string            `json:"imageStorageKey"`
	ProviderCalls        map[string]string `json:"providerCalls"`
}

type GenerateStoryboardTextInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	Prompt         string `json:"prompt"`
	CreatedBy      string `json:"createdBy"`
}

type GenerateStoryboardTextOutput struct {
	StoryboardArtifactID string          `json:"storyboardArtifactId"`
	StorageKey           string          `json:"storageKey"`
	ProviderCallID       string          `json:"providerCallId"`
	ModelID              string          `json:"modelId"`
	Storyboard           json.RawMessage `json:"storyboard"`
	RawText              string          `json:"rawText,omitempty"`
	ParseError           string          `json:"parseError,omitempty"`
}

type GenerateStoryboardImageInput struct {
	OrganizationID         string          `json:"organizationId"`
	ProjectID              string          `json:"projectId"`
	WorkflowRunID          string          `json:"workflowRunId"`
	Prompt                 string          `json:"prompt"`
	CreatedBy              string          `json:"createdBy"`
	StoryboardArtifactID   string          `json:"storyboardArtifactId"`
	Storyboard             json.RawMessage `json:"storyboard"`
	StoryboardProviderCall string          `json:"storyboardProviderCall,omitempty"`
}

type GenerateStoryboardImageOutput struct {
	ImageArtifactID  string `json:"imageArtifactId"`
	ImageMediaFileID string `json:"imageMediaFileId"`
	ImageStorageKey  string `json:"imageStorageKey"`
	ProviderCallID   string `json:"providerCallId"`
	ModelID          string `json:"modelId"`
	ImagePrompt      string `json:"imagePrompt"`
}

type workflowStorage interface {
	PutJSON(ctx context.Context, key string, value any) (storage.PutResult, error)
}

type Activities struct {
	db      *pgxpool.Pool
	storage workflowStorage
	gateway *provider.GatewayClient
}

func NewActivities(db *pgxpool.Pool, storageClient workflowStorage, gatewayClient *provider.GatewayClient) Activities {
	return Activities{db: db, storage: storageClient, gateway: gatewayClient}
}

func TextToStoryboardWorkflow(ctx workflow.Context, input TextToStoryboardInput) (TextToStoryboardOutput, error) {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())

	var storyboard GenerateStoryboardTextOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardText", generateStoryboardTextInput(input)).Get(ctx, &storyboard); err != nil {
		return TextToStoryboardOutput{}, err
	}

	var image GenerateStoryboardImageOutput
	imageInput := GenerateStoryboardImageInput{
		OrganizationID:         input.OrganizationID,
		ProjectID:              input.ProjectID,
		WorkflowRunID:          input.WorkflowRunID,
		Prompt:                 input.Prompt,
		CreatedBy:              input.CreatedBy,
		StoryboardArtifactID:   storyboard.StoryboardArtifactID,
		Storyboard:             storyboard.Storyboard,
		StoryboardProviderCall: storyboard.ProviderCallID,
	}
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardImage", imageInput).Get(ctx, &image); err != nil {
		return TextToStoryboardOutput{}, err
	}

	output := BuildTextToStoryboardOutput(storyboard, image)
	if err := workflow.ExecuteActivity(ctx, "CompleteTextToStoryboardWorkflow", input, output).Get(ctx, nil); err != nil {
		return TextToStoryboardOutput{}, err
	}
	return output, nil
}

func generateStoryboardTextInput(input TextToStoryboardInput) GenerateStoryboardTextInput {
	return GenerateStoryboardTextInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.Prompt,
		CreatedBy:      input.CreatedBy,
	}
}

func (a Activities) GenerateStoryboardText(ctx context.Context, input GenerateStoryboardTextInput) (GenerateStoryboardTextOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.Prompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return GenerateStoryboardTextOutput{}, err
	}
	aspectRatio, err := a.projectAspectRatio(ctx, input.ProjectID)
	if err != nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardPlanner, map[string]any{
		"input": map[string]any{
			"prompt": input.Prompt,
		},
		"project": map[string]any{
			"id":          input.ProjectID,
			"aspectRatio": aspectRatio,
		},
		"workflow": map[string]any{
			"id": input.WorkflowRunID,
		},
	})
	if err != nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateStoryboardTextKey,
		NodeType:       "provider_text",
		Input: mustJSON(map[string]any{
			"prompt":            input.Prompt,
			"modelProfileKey":   scriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return GenerateStoryboardTextOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, scriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.GenerateText(ctx, provider.GatewayTextRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   scriptModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustJSON(map[string]any{
			"prompt":         rendered.RenderedText,
			"responseFormat": "json",
		}),
	})
	if err != nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	storyboard, parseError := parseStoryboardText(gatewayResp.Output.Text)
	storyboardValue := map[string]any{
		"storyboard": storyboard,
		"rawText":    gatewayResp.Output.Text,
	}
	if parseError != "" {
		storyboardValue["parseError"] = parseError
	}
	storageKey := fmt.Sprintf("org/%s/project/%s/workflow/%s/storyboard/storyboard.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID)
	put, err := a.storage.PutJSON(ctx, storageKey, storyboardValue)
	if err != nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	artifactID, err := a.insertStoryboardArtifact(ctx, input, nodeRunID, put, gatewayResp, rendered, parseError)
	if err != nil {
		return GenerateStoryboardTextOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}

	output := GenerateStoryboardTextOutput{
		StoryboardArtifactID: artifactID,
		StorageKey:           put.StorageKey,
		ProviderCallID:       gatewayResp.ProviderCallID,
		ModelID:              gatewayResp.ModelID,
		Storyboard:           storyboard,
		RawText:              gatewayResp.Output.Text,
		ParseError:           parseError,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return GenerateStoryboardTextOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateStoryboardImage(ctx context.Context, input GenerateStoryboardImageInput) (GenerateStoryboardImageOutput, error) {
	baseInput := TextToStoryboardInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		Prompt:         input.Prompt,
		CreatedBy:      input.CreatedBy,
	}
	if err := validateStoryboardInput(baseInput); err != nil {
		return GenerateStoryboardImageOutput{}, err
	}
	imagePrompt := selectImagePrompt(input.Storyboard, input.Prompt)
	shot := firstStoryboardShot(input.Storyboard)
	if strings.TrimSpace(shot.ImagePrompt) == "" {
		shot.ImagePrompt = imagePrompt
	}
	if strings.TrimSpace(shot.Visual) == "" {
		shot.Visual = imagePrompt
	}
	aspectRatio, err := a.projectAspectRatio(ctx, input.ProjectID)
	if err != nil {
		return GenerateStoryboardImageOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyStoryboardImage, map[string]any{
		"input": map[string]any{
			"prompt": input.Prompt,
		},
		"project": map[string]any{
			"id":          input.ProjectID,
			"aspectRatio": aspectRatio,
		},
		"shot": map[string]any{
			"visual":      shot.Visual,
			"camera":      shot.Camera,
			"mood":        shot.Mood,
			"imagePrompt": shot.ImagePrompt,
		},
	})
	if err != nil {
		return GenerateStoryboardImageOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateStoryboardImageKey,
		NodeType:       "provider_image",
		Input: mustJSON(map[string]any{
			"storyboardArtifactId": input.StoryboardArtifactID,
			"imagePrompt":          imagePrompt,
			"modelProfileKey":      imageGenerationModelProfileKey,
			"promptTemplateKey":    rendered.TemplateKey,
			"promptVersionId":      rendered.PromptVersionID,
			"promptHash":           rendered.RenderedHash,
			"promptSource":         rendered.Source,
		}),
	})
	if err != nil {
		return GenerateStoryboardImageOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, imageGenerationModelProfileKey, []string{"image", "multimodal"}); err != nil {
		return GenerateStoryboardImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return GenerateStoryboardImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	gatewayResp, err := a.gateway.GenerateImage(ctx, provider.GatewayImageRequest{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         nodeRunID,
		ModelProfileKey:   imageGenerationModelProfileKey,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		PromptSource:      rendered.Source,
		Input: mustJSON(map[string]any{
			"prompt":  rendered.RenderedText,
			"size":    "1024x1024",
			"n":       1,
			"quality": "standard",
		}),
	})
	if err != nil {
		return GenerateStoryboardImageOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	output := GenerateStoryboardImageOutput{
		ImageArtifactID:  gatewayResp.Output.ArtifactID,
		ImageMediaFileID: gatewayResp.Output.MediaFileID,
		ImageStorageKey:  gatewayResp.Output.StorageKey,
		ProviderCallID:   gatewayResp.ProviderCallID,
		ModelID:          gatewayResp.ModelID,
		ImagePrompt:      rendered.RenderedText,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return GenerateStoryboardImageOutput{}, err
	}
	return output, nil
}

func (a Activities) CompleteTextToStoryboardWorkflow(ctx context.Context, input TextToStoryboardInput, output TextToStoryboardOutput) error {
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

func BuildTextToStoryboardOutput(storyboard GenerateStoryboardTextOutput, image GenerateStoryboardImageOutput) TextToStoryboardOutput {
	return TextToStoryboardOutput{
		StoryboardArtifactID: storyboard.StoryboardArtifactID,
		ImageArtifactID:      image.ImageArtifactID,
		ImageMediaFileID:     image.ImageMediaFileID,
		ImageStorageKey:      image.ImageStorageKey,
		ProviderCalls: map[string]string{
			"storyboard": storyboard.ProviderCallID,
			"image":      image.ProviderCallID,
		},
	}
}

func (a Activities) insertStoryboardArtifact(ctx context.Context, input GenerateStoryboardTextInput, nodeRunID string, put storage.PutResult, gatewayResp provider.GatewayTextResponse, rendered promptsvc.RenderedPrompt, parseError string) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	metadata := map[string]any{
		"source":            "workflow",
		"nodeKey":           nodeGenerateStoryboardTextKey,
		"providerCallId":    gatewayResp.ProviderCallID,
		"modelId":           gatewayResp.ModelID,
		"nodeRunId":         nodeRunID,
		"prompt":            input.Prompt,
		"promptTemplateKey": rendered.TemplateKey,
		"promptVersionId":   rendered.PromptVersionID,
		"promptHash":        rendered.RenderedHash,
		"promptSource":      rendered.Source,
		"byteSize":          put.ByteSize,
	}
	if parseError == "" {
		metadata["parseError"] = nil
	} else {
		metadata["parseError"] = parseError
	}
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'storyboard_json', $5, 'application/json', $6, $7, $8, $9)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, put.StorageKey, put.ContentHash, rendered.RenderedHash, mustJSON(metadata), input.CreatedBy).Scan(&artifactID); err != nil {
		return "", err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "artifact.created", "artifact", artifactID, mustJSON(map[string]any{
		"artifactId":    artifactID,
		"workflowRunId": input.WorkflowRunID,
		"nodeRunId":     nodeRunID,
		"storageKey":    put.StorageKey,
		"type":          "storyboard_json",
	})); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return artifactID, nil
}

func (a Activities) renderWorkflowPrompt(ctx context.Context, organizationID, projectID, templateKey string, variables map[string]any) (promptsvc.RenderedPrompt, error) {
	resolved, err := promptsvc.NewService(a.db).Resolve(ctx, promptsvc.ResolveRequest{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return promptsvc.RenderedPrompt{}, workflowErrorFromPrompt(err)
	}
	rendered, err := promptsvc.Render(resolved, variables)
	if err != nil {
		return promptsvc.RenderedPrompt{}, workflowErrorFromPrompt(err)
	}
	return rendered, nil
}

func workflowErrorFromPrompt(err error) error {
	var promptErr promptsvc.Error
	if errors.As(err, &promptErr) {
		return workflowError{Code: promptErr.Code, Message: promptErr.Message}
	}
	return workflowError{Code: codeActivityFailed, Message: err.Error()}
}

func (a Activities) projectAspectRatio(ctx context.Context, projectID string) (string, error) {
	var aspectRatio sqlNullString
	err := a.db.QueryRow(ctx, `SELECT aspect_ratio FROM projects WHERE id = $1`, projectID).Scan(&aspectRatio)
	if errors.Is(err, pgx.ErrNoRows) {
		return "16:9", nil
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(aspectRatio.String) == "" {
		return "16:9", nil
	}
	return strings.TrimSpace(aspectRatio.String), nil
}

func (a Activities) ensureModelProfileConfigured(ctx context.Context, organizationID, profileKey string, modalities []string) error {
	rows, err := a.db.Query(ctx, `
		SELECT 1
		FROM model_profiles p
		JOIN model_profile_bindings b ON b.model_profile_id = p.id
		JOIN provider_models m ON m.id = b.provider_model_id
		JOIN provider_accounts acc ON acc.id = m.provider_account_id
		WHERE p.organization_id = $1
		  AND p.profile_key = $2
		  AND b.enabled = true
		  AND m.status = 'active'
		  AND acc.status = 'active'
		  AND m.modality = ANY($3::text[])
		LIMIT 1
	`, organizationID, profileKey, modalities)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return rows.Err()
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return workflowError{
		Code:    codeModelProfileNotConfigured,
		Message: fmt.Sprintf("model profile %s has no active provider model binding", profileKey),
	}
}

func (a Activities) failActivity(ctx context.Context, input TextToStoryboardInput, nodeRunID string, cause error) error {
	code, message := workflowErrorFields(cause, codeActivityFailed)
	if strings.TrimSpace(nodeRunID) != "" {
		_ = FailNodeRun(ctx, a.db, nodeRunID, code, message)
	}
	_ = a.markWorkflowFailed(ctx, input, code, message)
	return temporal.NewApplicationError(message, code)
}

func (a Activities) markWorkflowFailed(ctx context.Context, input TextToStoryboardInput, code, message string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, code, message); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.run.failed", "workflow_run", input.WorkflowRunID, mustJSON(map[string]any{
		"code":    code,
		"message": message,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateStoryboardInput(input TextToStoryboardInput) error {
	if input.OrganizationID == "" || input.ProjectID == "" || input.WorkflowRunID == "" {
		return fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	return nil
}

func storyboardPlannerPrompt(userPrompt string) string {
	return `You are CineWeave's storyboard planner.
Convert the user's idea into a short storyboard JSON.

Return only JSON:
{
  "title": "...",
  "summary": "...",
  "shots": [
    {
      "shotNo": 1,
      "duration": 3,
      "visual": "...",
      "camera": "...",
      "motion": "...",
      "mood": "...",
      "imagePrompt": "..."
    }
  ]
}

User idea:
` + strings.TrimSpace(userPrompt)
}

func parseStoryboardText(text string) (json.RawMessage, string) {
	candidate := stripJSONFence(text)
	var decoded any
	if err := json.Unmarshal([]byte(candidate), &decoded); err != nil {
		return mustJSON(map[string]any{"rawText": text}), err.Error()
	}
	return mustJSON(decoded), ""
}

func stripJSONFence(text string) string {
	value := strings.TrimSpace(text)
	if strings.HasPrefix(value, "```") {
		value = strings.TrimPrefix(value, "```json")
		value = strings.TrimPrefix(value, "```JSON")
		value = strings.TrimPrefix(value, "```")
		value = strings.TrimSpace(value)
		value = strings.TrimSuffix(value, "```")
		value = strings.TrimSpace(value)
	}
	return value
}

func selectImagePrompt(storyboard json.RawMessage, fallback string) string {
	var decoded struct {
		Shots []struct {
			ImagePrompt string `json:"imagePrompt"`
			Visual      string `json:"visual"`
		} `json:"shots"`
	}
	if err := json.Unmarshal(storyboard, &decoded); err == nil && len(decoded.Shots) > 0 {
		if value := strings.TrimSpace(decoded.Shots[0].ImagePrompt); value != "" {
			return value
		}
		if value := strings.TrimSpace(decoded.Shots[0].Visual); value != "" {
			return value
		}
	}
	return strings.TrimSpace(fallback)
}

func workflowErrorFromProvider(err error, fallbackCode string) error {
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		standard := provider.NormalizeHTTPError(upstreamErr.Status, upstreamErr.Code)
		return workflowError{Code: standard.Code, Message: standard.Message}
	}
	if errors.Is(err, provider.ErrProviderGatewayRequired) {
		return workflowError{Code: provider.CodeProviderGatewayRequired, Message: err.Error()}
	}
	if errors.Is(err, provider.ErrValidation) {
		return workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()}
	}
	return workflowError{Code: fallbackCode, Message: err.Error()}
}

func workflowErrorFields(err error, fallbackCode string) (string, string) {
	var workflowErr workflowError
	if errors.As(err, &workflowErr) {
		return workflowErr.Code, workflowErr.Message
	}
	return fallbackCode, err.Error()
}

type workflowError struct {
	Code    string
	Message string
}

func (e workflowError) Error() string {
	return e.Message
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

type sqlNullString struct {
	String string
	Valid  bool
}

func (s *sqlNullString) Scan(value any) error {
	if value == nil {
		s.String = ""
		s.Valid = false
		return nil
	}
	s.Valid = true
	switch typed := value.(type) {
	case string:
		s.String = typed
	case []byte:
		s.String = string(typed)
	default:
		s.String = fmt.Sprint(typed)
	}
	return nil
}
