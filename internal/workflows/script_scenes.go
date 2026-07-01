package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeParseScriptScenesKey      = "parse_script_scenes"
	promptKeyScriptSceneParser    = "script_scene_parser"
	promptKeyScriptSceneRewrite   = "script_scene_rewrite"
	defaultScriptSceneFormat      = "markdown"
	regenerationTargetScriptScene = "script_scene"
	regenerationTargetSceneBoard  = "scene_storyboard"
)

type ParseScriptScenesOptions struct {
	ScriptID        string `json:"scriptId"`
	ScriptVersionID string `json:"scriptVersionId,omitempty"`
	ScriptSceneID   string `json:"scriptSceneId,omitempty"`
	Force           bool   `json:"force,omitempty"`
}

type ParseScriptScenesInput struct {
	OrganizationID  string `json:"organizationId"`
	ProjectID       string `json:"projectId"`
	WorkflowRunID   string `json:"workflowRunId,omitempty"`
	CreatedBy       string `json:"createdBy"`
	ScriptID        string `json:"scriptId"`
	ScriptVersionID string `json:"scriptVersionId,omitempty"`
	ScriptSceneID   string `json:"scriptSceneId,omitempty"`
	Force           bool   `json:"force,omitempty"`
}

type RegenerateScriptSceneInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId,omitempty"`
	CreatedBy      string `json:"createdBy"`
	ScriptSceneID  string `json:"scriptSceneId"`
	Instruction    string `json:"instruction,omitempty"`
}

type ParseScriptScenesOutput struct {
	ScriptID        string              `json:"scriptId"`
	ScriptVersionID string              `json:"versionId"`
	SceneCount      int                 `json:"sceneCount"`
	Scenes          []ScriptSceneRecord `json:"scenes"`
	ProviderCallID  string              `json:"providerCallId,omitempty"`
	ModelID         string              `json:"modelId,omitempty"`
}

type ScriptSceneCandidate struct {
	SceneIndex     int             `json:"sceneIndex,omitempty"`
	SceneNo        int             `json:"sceneNo"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary,omitempty"`
	Location       string          `json:"location,omitempty"`
	TimeOfDay      string          `json:"timeOfDay,omitempty"`
	Atmosphere     string          `json:"atmosphere,omitempty"`
	Characters     json.RawMessage `json:"characters,omitempty"`
	Scenes         json.RawMessage `json:"scenes,omitempty"`
	Props          json.RawMessage `json:"props,omitempty"`
	Action         string          `json:"action,omitempty"`
	Dialogue       string          `json:"dialogue,omitempty"`
	VisualGoal     string          `json:"visualGoal,omitempty"`
	EmotionalTone  string          `json:"emotionalTone,omitempty"`
	Conflict       string          `json:"conflict,omitempty"`
	Outcome        string          `json:"outcome,omitempty"`
	SourceEventIDs json.RawMessage `json:"sourceEventIds,omitempty"`
	Content        string          `json:"content,omitempty"`
	ContentFormat  string          `json:"contentFormat,omitempty"`
}

type ScriptSceneRecord struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId,omitempty"`
	ProjectID       string          `json:"projectId"`
	ScriptID        string          `json:"scriptId"`
	ScriptVersionID string          `json:"scriptVersionId"`
	SceneIndex      int             `json:"sceneIndex"`
	SceneNo         int             `json:"sceneNo"`
	Title           string          `json:"title"`
	Summary         string          `json:"summary,omitempty"`
	Location        string          `json:"location,omitempty"`
	TimeOfDay       string          `json:"timeOfDay,omitempty"`
	Atmosphere      string          `json:"atmosphere,omitempty"`
	Characters      json.RawMessage `json:"characters"`
	Scenes          json.RawMessage `json:"scenes"`
	Props           json.RawMessage `json:"props"`
	Action          string          `json:"action,omitempty"`
	Dialogue        string          `json:"dialogue,omitempty"`
	VisualGoal      string          `json:"visualGoal,omitempty"`
	EmotionalTone   string          `json:"emotionalTone,omitempty"`
	Conflict        string          `json:"conflict,omitempty"`
	Outcome         string          `json:"outcome,omitempty"`
	SourceEventIDs  json.RawMessage `json:"sourceEventIds"`
	Content         string          `json:"content"`
	ContentFormat   string          `json:"contentFormat"`
	ReviewStatus    string          `json:"reviewStatus"`
	ManualOverride  bool            `json:"manualOverride"`
	StaleState      string          `json:"staleState"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	CreatedBy       string          `json:"createdBy,omitempty"`
	EditedBy        string          `json:"editedBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt,omitempty"`
	UpdatedAt       time.Time       `json:"updatedAt,omitempty"`
	EditedAt        *time.Time      `json:"editedAt,omitempty"`
}

type ScriptSceneStoreInput struct {
	OrganizationID    string
	ProjectID         string
	ScriptID          string
	ScriptVersionID   string
	WorkflowRunID     string
	CreatedBy         string
	Force             bool
	ProviderCallID    string
	ModelID           string
	PromptTemplateKey string
	PromptVersionID   string
	PromptHash        string
	Source            string
}

func ParseScriptScenesWorkflow(ctx workflow.Context, input TextToStoryboardInput) (ParseScriptScenesOutput, error) {
	options := resolveParseScriptScenesOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output ParseScriptScenesOutput
	if err := workflow.ExecuteActivity(ctx, "ParseScriptScenes", ParseScriptScenesInput{
		OrganizationID:  input.OrganizationID,
		ProjectID:       input.ProjectID,
		WorkflowRunID:   input.WorkflowRunID,
		CreatedBy:       input.CreatedBy,
		ScriptID:        options.ScriptID,
		ScriptVersionID: options.ScriptVersionID,
		ScriptSceneID:   options.ScriptSceneID,
		Force:           options.Force,
	}).Get(ctx, &output); err != nil {
		return ParseScriptScenesOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteScriptScenesWorkflow", input, output).Get(ctx, nil); err != nil {
		return ParseScriptScenesOutput{}, err
	}
	return output, nil
}

func RegenerateScriptSceneWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var scene ScriptSceneRecord
	if err := workflow.ExecuteActivity(ctx, "RegenerateScriptScene", RegenerateScriptSceneInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ScriptSceneID:  options.TargetID,
		Instruction:    options.Instruction,
	}).Get(ctx, &scene); err != nil {
		return RegenerationOutput{}, err
	}
	output := RegenerationOutput{TargetType: regenerationTargetScriptScene, TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(scene)}
	if err := workflow.ExecuteActivity(ctx, "CompleteRegenerationWorkflow", input, output).Get(ctx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func RegenerateSceneStoryboardWorkflow(ctx workflow.Context, input TextToStoryboardInput) (RegenerationOutput, error) {
	options := resolveRegenerationOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var storyboard ScriptStoryboardOutput
	maxShots := options.MaxShots
	if maxShots <= 0 {
		maxShots = defaultMaxStoryboardShots
	}
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboardFromScript", GenerateStoryboardFromScriptInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ScriptSceneID:  options.TargetID,
		MaxShots:       maxShots,
	}).Get(ctx, &storyboard); err != nil {
		return RegenerationOutput{}, err
	}
	output := RegenerationOutput{TargetType: regenerationTargetSceneBoard, TargetID: options.TargetID, Status: "succeeded", Output: mustJSON(storyboard)}
	if err := workflow.ExecuteActivity(ctx, "CompleteRegenerationWorkflow", input, output).Get(ctx, nil); err != nil {
		return RegenerationOutput{}, err
	}
	return output, nil
}

func (a Activities) ParseScriptScenes(ctx context.Context, input ParseScriptScenesInput) (ParseScriptScenesOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "parse_script_scenes", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return ParseScriptScenesOutput{}, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if strings.TrimSpace(input.ScriptID) == "" && strings.TrimSpace(input.ScriptSceneID) == "" {
		return ParseScriptScenesOutput{}, fmt.Errorf("scriptId or scriptSceneId is required")
	}
	if input.ScriptSceneID != "" {
		scene, err := a.scriptSceneByID(ctx, input.ProjectID, input.ScriptSceneID)
		if err != nil {
			return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		if input.ScriptID == "" {
			input.ScriptID = scene.ScriptID
		}
		if input.ScriptVersionID == "" {
			input.ScriptVersionID = scene.ScriptVersionID
		}
	}
	script, err := a.scriptForSceneParse(ctx, input.ProjectID, input.ScriptID, input.ScriptVersionID)
	if err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyScriptSceneParser, map[string]any{
		"project": project.asPromptVariables(),
		"script":  map[string]any{"id": script.ID, "versionId": script.VersionID, "title": script.Title, "content": script.Content},
	})
	if err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeParseScriptScenesKey,
		NodeType:       "agent.script_scene_parse",
		Input: mustJSON(map[string]any{
			"scriptId":          script.ID,
			"scriptVersionId":   script.VersionID,
			"force":             input.Force,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return ParseScriptScenesOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
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
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	candidates, err := NormalizeScriptSceneParser(gatewayResp.Output.Text)
	if err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	defer tx.Rollback(ctx)
	scenes, err := StoreScriptScenes(ctx, tx, ScriptSceneStoreInput{
		OrganizationID:    input.OrganizationID,
		ProjectID:         input.ProjectID,
		ScriptID:          script.ID,
		ScriptVersionID:   script.VersionID,
		WorkflowRunID:     input.WorkflowRunID,
		CreatedBy:         input.CreatedBy,
		Force:             input.Force,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ModelID:           gatewayResp.ModelID,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		Source:            promptKeyScriptSceneParser,
	}, candidates)
	if err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.scenes.parsed", "script_version", script.VersionID, mustJSON(map[string]any{
		"scriptId":        script.ID,
		"scriptVersionId": script.VersionID,
		"sceneCount":      len(scenes),
		"force":           input.Force,
	})); err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := tx.Commit(ctx); err != nil {
		return ParseScriptScenesOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	output := ParseScriptScenesOutput{
		ScriptID:        script.ID,
		ScriptVersionID: script.VersionID,
		SceneCount:      len(scenes),
		Scenes:          scenes,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return ParseScriptScenesOutput{}, err
	}
	return output, nil
}

func (a Activities) RegenerateScriptScene(ctx context.Context, input RegenerateScriptSceneInput) (ScriptSceneRecord, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "regenerate_script_scene", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ScriptSceneID) == "" {
		return ScriptSceneRecord{}, fmt.Errorf("organizationId, projectId, workflowRunId, and scriptSceneId are required")
	}
	scene, err := a.scriptSceneByID(ctx, input.ProjectID, input.ScriptSceneID)
	if err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyScriptSceneRewrite, map[string]any{
		"project": project.asPromptVariables(),
		"scene":   scene,
		"input":   map[string]any{"instruction": strings.TrimSpace(input.Instruction)},
		"assets":  map[string]any{"items": []string{}},
		"events":  map[string]any{"items": []string{}},
	})
	if err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        "regenerate_script_scene",
		NodeType:       "agent.script_scene_rewrite",
		Input: mustJSON(map[string]any{
			"scriptSceneId":     input.ScriptSceneID,
			"instruction":       strings.TrimSpace(input.Instruction),
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return ScriptSceneRecord{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
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
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	candidates, err := NormalizeScriptSceneParser(gatewayResp.Output.Text)
	if err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	if len(candidates) == 0 {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "script scene rewrite returned no scene"})
	}
	candidate := candidates[0]
	candidate.SceneNo = scene.SceneNo
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	defer tx.Rollback(ctx)
	updated, err := updateSingleScriptScene(ctx, tx, scene, candidate, ScriptSceneStoreInput{
		ProjectID:         input.ProjectID,
		ProviderCallID:    gatewayResp.ProviderCallID,
		ModelID:           gatewayResp.ModelID,
		PromptTemplateKey: rendered.TemplateKey,
		PromptVersionID:   rendered.PromptVersionID,
		PromptHash:        rendered.RenderedHash,
		Source:            promptKeyScriptSceneRewrite,
	})
	if err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := markScriptSceneDownstreamStale(ctx, tx, input.ProjectID, scene.ID); err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.scene.regenerated", "script_scene", scene.ID, mustJSON(map[string]any{
		"scriptSceneId":  scene.ID,
		"scriptId":       scene.ScriptID,
		"providerCallId": gatewayResp.ProviderCallID,
	})); err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := tx.Commit(ctx); err != nil {
		return ScriptSceneRecord{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(updated)); err != nil {
		return ScriptSceneRecord{}, err
	}
	return updated, nil
}

func (a Activities) CompleteScriptScenesWorkflow(ctx context.Context, input TextToStoryboardInput, output ParseScriptScenesOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func NormalizeScriptSceneParser(text string) ([]ScriptSceneCandidate, error) {
	candidate := stripJSONFence(text)
	var decoded struct {
		Scenes []ScriptSceneCandidate `json:"scenes"`
	}
	if err := json.Unmarshal([]byte(candidate), &decoded); err != nil {
		var single ScriptSceneCandidate
		if singleErr := json.Unmarshal([]byte(candidate), &single); singleErr != nil {
			return nil, err
		}
		decoded.Scenes = []ScriptSceneCandidate{single}
	}
	out := make([]ScriptSceneCandidate, 0, len(decoded.Scenes))
	for i, scene := range decoded.Scenes {
		scene.SceneIndex = i
		if scene.SceneNo <= 0 {
			scene.SceneNo = i + 1
		}
		scene.Title = strings.TrimSpace(scene.Title)
		if scene.Title == "" {
			scene.Title = "Scene " + strconv.Itoa(scene.SceneNo)
		}
		scene.Summary = strings.TrimSpace(scene.Summary)
		scene.Location = strings.TrimSpace(scene.Location)
		scene.TimeOfDay = strings.TrimSpace(scene.TimeOfDay)
		scene.Atmosphere = strings.TrimSpace(scene.Atmosphere)
		scene.Action = strings.TrimSpace(scene.Action)
		scene.Dialogue = strings.TrimSpace(scene.Dialogue)
		scene.VisualGoal = strings.TrimSpace(scene.VisualGoal)
		scene.EmotionalTone = strings.TrimSpace(scene.EmotionalTone)
		scene.Conflict = strings.TrimSpace(scene.Conflict)
		scene.Outcome = strings.TrimSpace(scene.Outcome)
		scene.Content = strings.TrimSpace(scene.Content)
		if scene.Content == "" {
			scene.Content = fallbackSceneContent(scene)
		}
		scene.ContentFormat = strings.TrimSpace(scene.ContentFormat)
		if scene.ContentFormat == "" {
			scene.ContentFormat = defaultScriptSceneFormat
		}
		if scene.ContentFormat != "plain_text" && scene.ContentFormat != "markdown" {
			scene.ContentFormat = defaultScriptSceneFormat
		}
		scene.Characters = normalizedStringArrayRaw(scene.Characters)
		scene.Scenes = normalizedStringArrayRaw(scene.Scenes)
		scene.Props = normalizedStringArrayRaw(scene.Props)
		scene.SourceEventIDs = normalizedStringArrayRaw(scene.SourceEventIDs)
		out = append(out, scene)
	}
	return out, nil
}

func StoreScriptScenes(ctx context.Context, tx pgx.Tx, input ScriptSceneStoreInput, scenes []ScriptSceneCandidate) ([]ScriptSceneRecord, error) {
	records := make([]ScriptSceneRecord, 0, len(scenes))
	for i, scene := range scenes {
		scene.SceneIndex = i
		metadata := mustJSON(map[string]any{
			"source":             firstNonEmptyString(input.Source, promptKeyScriptSceneParser),
			"workflowRunId":      input.WorkflowRunID,
			"providerCallId":     input.ProviderCallID,
			"modelId":            input.ModelID,
			"promptTemplateKey":  input.PromptTemplateKey,
			"promptVersionId":    input.PromptVersionID,
			"promptHash":         input.PromptHash,
			"overwrittenByAgent": input.Force,
		})
		var existingID string
		var manualOverride bool
		err := tx.QueryRow(ctx, `
			SELECT id::text, COALESCE(manual_override, false)
			FROM script_scenes
			WHERE script_version_id = $1 AND scene_index = $2
		`, input.ScriptVersionID, scene.SceneIndex).Scan(&existingID, &manualOverride)
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		if err == pgx.ErrNoRows {
			record, err := ScanScriptSceneRecord(tx.QueryRow(ctx, `
				INSERT INTO script_scenes(
					organization_id, project_id, script_id, script_version_id,
					scene_index, scene_no, title, summary, location, time_of_day, atmosphere,
					characters, scenes, props, action, dialogue, visual_goal, emotional_tone,
					conflict, outcome, source_event_ids, content, content_format,
					review_status, manual_override, stale_state, metadata, created_by
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''),
				        $12, $13, $14, NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''), NULLIF($18, ''),
				        NULLIF($19, ''), NULLIF($20, ''), $21, $22, $23,
				        'pending', false, 'fresh', $24, NULLIF($25, '')::uuid)
				RETURNING `+ScriptSceneColumns()+`
			`, input.OrganizationID, input.ProjectID, input.ScriptID, input.ScriptVersionID,
				scene.SceneIndex, scene.SceneNo, scene.Title, scene.Summary, scene.Location, scene.TimeOfDay, scene.Atmosphere,
				jsonOrDefault(scene.Characters, `[]`), jsonOrDefault(scene.Scenes, `[]`), jsonOrDefault(scene.Props, `[]`),
				scene.Action, scene.Dialogue, scene.VisualGoal, scene.EmotionalTone, scene.Conflict, scene.Outcome,
				jsonOrDefault(scene.SourceEventIDs, `[]`), scene.Content, scene.ContentFormat, metadata, input.CreatedBy))
			if err != nil {
				return nil, err
			}
			records = append(records, record)
			continue
		}
		if manualOverride && !input.Force {
			record, err := ScanScriptSceneRecord(tx.QueryRow(ctx, `
				UPDATE script_scenes
				SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('agentLastSuggestion', $3::jsonb),
				    updated_at = now()
				WHERE project_id = $1 AND id = $2
				RETURNING `+ScriptSceneColumns()+`
			`, input.ProjectID, existingID, metadata))
			if err != nil {
				return nil, err
			}
			records = append(records, record)
			continue
		}
		record, err := ScanScriptSceneRecord(tx.QueryRow(ctx, `
			UPDATE script_scenes
			SET scene_no = $4,
			    title = $5,
			    summary = NULLIF($6, ''),
			    location = NULLIF($7, ''),
			    time_of_day = NULLIF($8, ''),
			    atmosphere = NULLIF($9, ''),
			    characters = $10,
			    scenes = $11,
			    props = $12,
			    action = NULLIF($13, ''),
			    dialogue = NULLIF($14, ''),
			    visual_goal = NULLIF($15, ''),
			    emotional_tone = NULLIF($16, ''),
			    conflict = NULLIF($17, ''),
			    outcome = NULLIF($18, ''),
			    source_event_ids = $19,
			    content = $20,
			    content_format = $21,
			    review_status = 'pending',
			    manual_override = false,
			    stale_state = 'fresh',
			    metadata = COALESCE(metadata, '{}'::jsonb) || $22::jsonb,
			    edited_by = NULL,
			    edited_at = NULL,
			    updated_at = now()
			WHERE project_id = $1 AND id = $2 AND script_version_id = $3
			RETURNING `+ScriptSceneColumns()+`
		`, input.ProjectID, existingID, input.ScriptVersionID,
			scene.SceneNo, scene.Title, scene.Summary, scene.Location, scene.TimeOfDay, scene.Atmosphere,
			jsonOrDefault(scene.Characters, `[]`), jsonOrDefault(scene.Scenes, `[]`), jsonOrDefault(scene.Props, `[]`),
			scene.Action, scene.Dialogue, scene.VisualGoal, scene.EmotionalTone, scene.Conflict, scene.Outcome,
			jsonOrDefault(scene.SourceEventIDs, `[]`), scene.Content, scene.ContentFormat, metadata))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func updateSingleScriptScene(ctx context.Context, tx pgx.Tx, existing ScriptSceneRecord, scene ScriptSceneCandidate, input ScriptSceneStoreInput) (ScriptSceneRecord, error) {
	metadata := mustJSON(map[string]any{
		"source":            firstNonEmptyString(input.Source, promptKeyScriptSceneRewrite),
		"providerCallId":    input.ProviderCallID,
		"modelId":           input.ModelID,
		"promptTemplateKey": input.PromptTemplateKey,
		"promptVersionId":   input.PromptVersionID,
		"promptHash":        input.PromptHash,
	})
	if scene.Title == "" {
		scene.Title = existing.Title
	}
	if scene.Content == "" {
		scene.Content = fallbackSceneContent(scene)
	}
	if scene.ContentFormat == "" {
		scene.ContentFormat = defaultScriptSceneFormat
	}
	scene.Characters = normalizedStringArrayRaw(scene.Characters)
	scene.Scenes = normalizedStringArrayRaw(scene.Scenes)
	scene.Props = normalizedStringArrayRaw(scene.Props)
	scene.SourceEventIDs = normalizedStringArrayRaw(scene.SourceEventIDs)
	return ScanScriptSceneRecord(tx.QueryRow(ctx, `
		UPDATE script_scenes
		SET scene_no = $3,
		    title = $4,
		    summary = NULLIF($5, ''),
		    location = NULLIF($6, ''),
		    time_of_day = NULLIF($7, ''),
		    atmosphere = NULLIF($8, ''),
		    characters = $9,
		    scenes = $10,
		    props = $11,
		    action = NULLIF($12, ''),
		    dialogue = NULLIF($13, ''),
		    visual_goal = NULLIF($14, ''),
		    emotional_tone = NULLIF($15, ''),
		    conflict = NULLIF($16, ''),
		    outcome = NULLIF($17, ''),
		    source_event_ids = $18,
		    content = $19,
		    content_format = $20,
		    review_status = 'pending',
		    manual_override = false,
		    stale_state = 'fresh',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $21::jsonb,
		    edited_by = NULL,
		    edited_at = NULL,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		RETURNING `+ScriptSceneColumns()+`
	`, input.ProjectID, existing.ID,
		scene.SceneNo, scene.Title, scene.Summary, scene.Location, scene.TimeOfDay, scene.Atmosphere,
		jsonOrDefault(scene.Characters, `[]`), jsonOrDefault(scene.Scenes, `[]`), jsonOrDefault(scene.Props, `[]`),
		scene.Action, scene.Dialogue, scene.VisualGoal, scene.EmotionalTone, scene.Conflict, scene.Outcome,
		jsonOrDefault(scene.SourceEventIDs, `[]`), scene.Content, scene.ContentFormat, metadata))
}

func markScriptSceneDownstreamStale(ctx context.Context, tx pgx.Tx, projectID, sceneID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE scene_asset_links
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		  'staleState', 'upstream_changed',
		  'staleReason', 'script_scene_updated'
		)
		WHERE project_id = $1 AND script_scene_id = $2
	`, projectID, sceneID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE canonical_assets
		SET stale_state = 'upstream_changed', updated_at = now()
		WHERE project_id = $1
		  AND id IN (
		    SELECT asset_id
		    FROM scene_asset_links
		    WHERE project_id = $1 AND script_scene_id = $2
		  )
	`, projectID, sceneID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements r
		SET stale_state = 'upstream_changed', updated_at = now()
		FROM storyboard_shots s
		WHERE r.storyboard_shot_id = s.id
		  AND r.project_id = $1
		  AND s.script_scene_id = $2
		  AND s.deleted_at IS NULL
	`, projectID, sceneID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1 AND script_scene_id = $2 AND deleted_at IS NULL
	`, projectID, sceneID)
	return err
}

func ScriptSceneSelectSQL(where string) string {
	return `
		SELECT ` + ScriptSceneColumns() + `
		FROM script_scenes
	` + where
}

func ScriptSceneColumns() string {
	return `
		id::text,
		organization_id::text,
		project_id::text,
		script_id::text,
		script_version_id::text,
		scene_index,
		scene_no,
		title,
		COALESCE(summary, ''),
		COALESCE(location, ''),
		COALESCE(time_of_day, ''),
		COALESCE(atmosphere, ''),
		characters,
		scenes,
		props,
		COALESCE(action, ''),
		COALESCE(dialogue, ''),
		COALESCE(visual_goal, ''),
		COALESCE(emotional_tone, ''),
		COALESCE(conflict, ''),
		COALESCE(outcome, ''),
		source_event_ids,
		content,
		content_format,
		review_status,
		COALESCE(manual_override, false),
		COALESCE(stale_state, 'fresh'),
		metadata,
		COALESCE(created_by::text, ''),
		COALESCE(edited_by::text, ''),
		created_at,
		updated_at,
		edited_at
	`
}

func ScanScriptSceneRecord(row interface{ Scan(...any) error }) (ScriptSceneRecord, error) {
	var item ScriptSceneRecord
	var characters, scenes, props, sourceEventIDs, metadata []byte
	var editedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.ScriptID,
		&item.ScriptVersionID,
		&item.SceneIndex,
		&item.SceneNo,
		&item.Title,
		&item.Summary,
		&item.Location,
		&item.TimeOfDay,
		&item.Atmosphere,
		&characters,
		&scenes,
		&props,
		&item.Action,
		&item.Dialogue,
		&item.VisualGoal,
		&item.EmotionalTone,
		&item.Conflict,
		&item.Outcome,
		&sourceEventIDs,
		&item.Content,
		&item.ContentFormat,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&metadata,
		&item.CreatedBy,
		&item.EditedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&editedAt,
	)
	item.Characters = jsonOrDefault(characters, `[]`)
	item.Scenes = jsonOrDefault(scenes, `[]`)
	item.Props = jsonOrDefault(props, `[]`)
	item.SourceEventIDs = jsonOrDefault(sourceEventIDs, `[]`)
	item.Metadata = jsonOrDefault(metadata, `{}`)
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	return item, err
}

func FormatScriptScenesForPrompt(scenes []ScriptSceneRecord) string {
	blocks := make([]string, 0, len(scenes))
	for _, scene := range scenes {
		parts := []string{
			fmt.Sprintf("===== Scene %d: %s =====", scene.SceneNo, scene.Title),
			"Location: " + scene.Location,
			"Time: " + scene.TimeOfDay,
			"Atmosphere: " + scene.Atmosphere,
			"Characters: " + strings.Join(stringSliceFromRawMessage(scene.Characters), ", "),
			"Scene assets: " + strings.Join(stringSliceFromRawMessage(scene.Scenes), ", "),
			"Props: " + strings.Join(stringSliceFromRawMessage(scene.Props), ", "),
			"Summary: " + scene.Summary,
			"Action: " + scene.Action,
			"Dialogue: " + scene.Dialogue,
			"Visual goal: " + scene.VisualGoal,
			"Emotional tone: " + scene.EmotionalTone,
			"Conflict: " + scene.Conflict,
			"Outcome: " + scene.Outcome,
			"Content:\n" + scene.Content,
		}
		blocks = append(blocks, strings.Join(compactStrings(parts), "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func (a Activities) scriptForSceneParse(ctx context.Context, projectID, scriptID, versionID string) (ScriptRecord, error) {
	if strings.TrimSpace(versionID) == "" {
		return a.activeScript(ctx, projectID, scriptID)
	}
	var script ScriptRecord
	err := a.db.QueryRow(ctx, `
		SELECT
			s.id::text,
			v.id::text,
			COALESCE(v.version, v.version_no),
			COALESCE(v.content, ''),
			COALESCE(v.content_format, 'markdown'),
			s.title
		FROM scripts s
		JOIN script_versions v ON v.script_id = s.id
		WHERE s.project_id = $1 AND s.id = $2 AND v.id = $3
	`, projectID, scriptID, versionID).Scan(&script.ID, &script.VersionID, &script.Version, &script.Content, &script.ContentFormat, &script.Title)
	return script, err
}

func (a Activities) scriptSceneByID(ctx context.Context, projectID, sceneID string) (ScriptSceneRecord, error) {
	return ScanScriptSceneRecord(a.db.QueryRow(ctx, ScriptSceneSelectSQL(`
		WHERE project_id = $1 AND id = $2
	`), projectID, sceneID))
}

func (a Activities) scriptScenesForVersion(ctx context.Context, projectID, versionID string) ([]ScriptSceneRecord, error) {
	return queryScriptScenes(ctx, a.db, `
		WHERE project_id = $1 AND script_version_id = $2
		ORDER BY scene_index ASC
	`, projectID, versionID)
}

func (a Activities) storyboardScenesForScript(ctx context.Context, projectID, versionID, sceneID string) ([]ScriptSceneRecord, error) {
	if strings.TrimSpace(sceneID) != "" {
		scene, err := a.scriptSceneByID(ctx, projectID, sceneID)
		if err != nil {
			return nil, err
		}
		return []ScriptSceneRecord{scene}, nil
	}
	approved, err := queryScriptScenes(ctx, a.db, `
		WHERE project_id = $1 AND script_version_id = $2 AND review_status = 'approved'
		ORDER BY scene_index ASC
	`, projectID, versionID)
	if err != nil {
		return nil, err
	}
	if len(approved) > 0 {
		return approved, nil
	}
	return a.scriptScenesForVersion(ctx, projectID, versionID)
}

type scriptSceneQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func queryScriptScenes(ctx context.Context, db scriptSceneQuerier, where string, args ...any) ([]ScriptSceneRecord, error) {
	rows, err := db.Query(ctx, ScriptSceneSelectSQL(where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptSceneRecord, 0)
	for rows.Next() {
		item, err := ScanScriptSceneRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a Activities) upsertSceneAssetLinks(ctx context.Context, input AnalyzeScriptAssetsInput, scenes []ScriptSceneRecord, assets []CanonicalAssetRecord) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	assetByKey := map[string]CanonicalAssetRecord{}
	for _, asset := range assets {
		assetByKey[assetKey(asset.AssetType, asset.Name)] = asset
	}
	for _, scene := range scenes {
		if _, err := tx.Exec(ctx, `DELETE FROM scene_asset_links WHERE script_scene_id = $1`, scene.ID); err != nil {
			return err
		}
		for _, ref := range sceneAssetReferences(scene) {
			asset, ok := assetByKey[assetKey(ref.AssetType, ref.Name)]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO scene_asset_links(
					organization_id, project_id, script_scene_id, asset_id, asset_role, usage_note, metadata
				)
				VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7)
				ON CONFLICT (script_scene_id, asset_id) DO UPDATE SET
					asset_role = EXCLUDED.asset_role,
					usage_note = EXCLUDED.usage_note,
					metadata = COALESCE(scene_asset_links.metadata, '{}'::jsonb) || EXCLUDED.metadata
			`, input.OrganizationID, input.ProjectID, scene.ID, asset.ID, ref.Role, ref.UsageNote, mustJSON(map[string]any{
				"source":        "script_asset_extraction",
				"scriptId":      input.ScriptID,
				"scriptSceneId": scene.ID,
			})); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

type sceneAssetReference struct {
	AssetType string
	Name      string
	Role      string
	UsageNote string
}

func sceneAssetReferences(scene ScriptSceneRecord) []sceneAssetReference {
	refs := make([]sceneAssetReference, 0)
	for _, name := range stringSliceFromRawMessage(scene.Characters) {
		refs = append(refs, sceneAssetReference{AssetType: "character", Name: name, Role: "main_character", UsageNote: "Appears in scene " + strconv.Itoa(scene.SceneNo)})
	}
	for _, name := range stringSliceFromRawMessage(scene.Scenes) {
		refs = append(refs, sceneAssetReference{AssetType: "scene", Name: name, Role: "location", UsageNote: firstNonEmptyString(scene.Location, "Scene setting")})
	}
	for _, name := range stringSliceFromRawMessage(scene.Props) {
		refs = append(refs, sceneAssetReference{AssetType: "prop", Name: name, Role: "prop", UsageNote: "Used in scene " + strconv.Itoa(scene.SceneNo)})
	}
	return refs
}

func resolveParseScriptScenesOptions(raw json.RawMessage) ParseScriptScenesOptions {
	var options ParseScriptScenesOptions
	if len(raw) == 0 {
		return options
	}
	_ = json.Unmarshal(raw, &options)
	options.ScriptID = strings.TrimSpace(options.ScriptID)
	options.ScriptVersionID = strings.TrimSpace(options.ScriptVersionID)
	options.ScriptSceneID = strings.TrimSpace(options.ScriptSceneID)
	return options
}

func fallbackSceneContent(scene ScriptSceneCandidate) string {
	parts := []string{
		fmt.Sprintf("## Scene %d: %s", scene.SceneNo, scene.Title),
		scene.Summary,
		"Location: " + scene.Location,
		"Time: " + scene.TimeOfDay,
		scene.Action,
		scene.Dialogue,
	}
	return strings.Join(compactStrings(parts), "\n\n")
}

func normalizedStringArrayRaw(raw json.RawMessage) json.RawMessage {
	values := stringSliceFromRawMessage(raw)
	return mustJSON(values)
}

func stringSliceFromRawMessage(raw json.RawMessage) []string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var decoded []any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		var stringsOnly []string
		if stringsErr := json.Unmarshal(raw, &stringsOnly); stringsErr == nil {
			return normalizeStringValues(stringsOnly)
		}
		return nil
	}
	out := make([]string, 0, len(decoded))
	for _, value := range decoded {
		switch typed := value.(type) {
		case string:
			out = append(out, typed)
		case float64:
			if typed == float64(int64(typed)) {
				out = append(out, strconv.FormatInt(int64(typed), 10))
			}
		default:
			rawValue, _ := json.Marshal(typed)
			if len(rawValue) > 0 && string(rawValue) != "null" {
				out = append(out, string(rawValue))
			}
		}
	}
	return normalizeStringValues(out)
}

func normalizeStringValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
