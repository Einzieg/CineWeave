package workflows

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeExtractNovelEventsKey         = "extract_novel_events"
	nodeGenerateAdaptationPlanKey     = "generate_adaptation_plan"
	nodeGenerateScriptFromPlanKey     = "generate_script_from_adaptation_plan"
	promptKeyNovelEventExtraction     = "novel_event_extraction"
	promptKeyAdaptationPlanGeneration = "adaptation_plan_generation"
	promptKeyScriptFromAdaptationPlan = "script_from_adaptation_plan"
	defaultAdaptationTargetFormat     = "short_video"
)

type ExtractNovelEventsOptions struct {
	SourceID   string   `json:"sourceId"`
	ChapterIDs []string `json:"chapterIds,omitempty"`
	Force      bool     `json:"force,omitempty"`
}

type GenerateAdaptationPlanOptions struct {
	SourceID              string   `json:"sourceId"`
	EventIDs              []string `json:"eventIds,omitempty"`
	TargetFormat          string   `json:"targetFormat,omitempty"`
	TargetDurationSeconds int      `json:"targetDurationSeconds,omitempty"`
	MaxShots              int      `json:"maxShots,omitempty"`
	Instruction           string   `json:"instruction,omitempty"`
}

type AdaptationPlanToScriptOptions struct {
	PlanID      string `json:"planId"`
	Title       string `json:"title,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

type ExtractNovelEventsInput struct {
	OrganizationID string   `json:"organizationId"`
	ProjectID      string   `json:"projectId"`
	WorkflowRunID  string   `json:"workflowRunId"`
	CreatedBy      string   `json:"createdBy"`
	SourceID       string   `json:"sourceId"`
	ChapterIDs     []string `json:"chapterIds,omitempty"`
	Force          bool     `json:"force,omitempty"`
}

type ExtractNovelEventsOutput struct {
	SourceID        string   `json:"sourceId"`
	ChapterCount    int      `json:"chapterCount"`
	EventCount      int      `json:"eventCount"`
	LinkCount       int      `json:"linkCount"`
	ProviderCallIDs []string `json:"providerCallIds,omitempty"`
	ModelIDs        []string `json:"modelIds,omitempty"`
}

type GenerateAdaptationPlanInput struct {
	OrganizationID        string   `json:"organizationId"`
	ProjectID             string   `json:"projectId"`
	WorkflowRunID         string   `json:"workflowRunId,omitempty"`
	CreatedBy             string   `json:"createdBy"`
	SourceID              string   `json:"sourceId"`
	EventIDs              []string `json:"eventIds,omitempty"`
	TargetFormat          string   `json:"targetFormat,omitempty"`
	TargetDurationSeconds int      `json:"targetDurationSeconds,omitempty"`
	MaxShots              int      `json:"maxShots,omitempty"`
	Instruction           string   `json:"instruction,omitempty"`
}

type AdaptationPlanOutput struct {
	PlanID         string          `json:"planId"`
	SourceID       string          `json:"sourceId,omitempty"`
	Title          string          `json:"title"`
	SelectedEvents []string        `json:"selectedEventIds"`
	Content        string          `json:"content"`
	Structure      json.RawMessage `json:"structure"`
	ProviderCallID string          `json:"providerCallId,omitempty"`
	ModelID        string          `json:"modelId,omitempty"`
	Warning        string          `json:"warning,omitempty"`
}

type GenerateScriptFromPlanInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId,omitempty"`
	CreatedBy      string `json:"createdBy"`
	PlanID         string `json:"planId"`
	Title          string `json:"title,omitempty"`
	Instruction    string `json:"instruction,omitempty"`
}

type AdaptationScriptOutput struct {
	PlanID          string `json:"planId"`
	SourceID        string `json:"sourceId,omitempty"`
	ScriptID        string `json:"scriptId"`
	ScriptVersionID string `json:"scriptVersionId"`
	ProviderCallID  string `json:"providerCallId,omitempty"`
	ModelID         string `json:"modelId,omitempty"`
	Content         string `json:"content"`
}

type NovelEventCandidate struct {
	EventIndex     int      `json:"eventIndex,omitempty"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	EventType      string   `json:"eventType,omitempty"`
	Importance     int      `json:"importance"`
	TimelineHint   string   `json:"timelineHint,omitempty"`
	LocationHint   string   `json:"locationHint,omitempty"`
	EmotionalTone  string   `json:"emotionalTone,omitempty"`
	Conflict       string   `json:"conflict,omitempty"`
	Outcome        string   `json:"outcome,omitempty"`
	AdaptationHint string   `json:"adaptationHint,omitempty"`
	Characters     []string `json:"characters"`
	Scenes         []string `json:"scenes"`
	Props          []string `json:"props"`
	Keywords       []string `json:"keywords"`
	RawExcerpt     string   `json:"rawExcerpt,omitempty"`
}

type NovelEventLinkCandidate struct {
	SourceEventIndex int    `json:"sourceEventIndex"`
	TargetEventIndex int    `json:"targetEventIndex"`
	LinkType         string `json:"linkType"`
	Description      string `json:"description,omitempty"`
}

type NovelEventExtraction struct {
	Events []NovelEventCandidate     `json:"events"`
	Links  []NovelEventLinkCandidate `json:"links"`
}

type NovelEventRecord struct {
	ID             string          `json:"id"`
	SourceID       string          `json:"sourceId"`
	ChapterID      string          `json:"chapterId,omitempty"`
	ChapterIndex   int             `json:"chapterIndex,omitempty"`
	EventIndex     int             `json:"eventIndex"`
	SequenceNo     int             `json:"sequenceNo"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary"`
	EventType      string          `json:"eventType,omitempty"`
	Importance     int             `json:"importance"`
	TimelineHint   string          `json:"timelineHint,omitempty"`
	LocationHint   string          `json:"locationHint,omitempty"`
	EmotionalTone  string          `json:"emotionalTone,omitempty"`
	Conflict       string          `json:"conflict,omitempty"`
	Outcome        string          `json:"outcome,omitempty"`
	AdaptationHint string          `json:"adaptationHint,omitempty"`
	Characters     json.RawMessage `json:"characters"`
	Scenes         json.RawMessage `json:"scenes"`
	Props          json.RawMessage `json:"props"`
	Keywords       json.RawMessage `json:"keywords"`
	RawExcerpt     string          `json:"rawExcerpt,omitempty"`
	ReviewStatus   string          `json:"reviewStatus"`
}

type AdaptationPlanDraft struct {
	Title             string          `json:"title"`
	Logline           string          `json:"logline,omitempty"`
	Theme             string          `json:"theme,omitempty"`
	Structure         json.RawMessage `json:"structure"`
	SelectedEvents    []string        `json:"selectedEvents"`
	OmittedEvents     json.RawMessage `json:"omittedEvents"`
	VisualStrategy    string          `json:"visualStrategy,omitempty"`
	CharacterStrategy string          `json:"characterStrategy,omitempty"`
	ShotStrategy      string          `json:"shotStrategy,omitempty"`
	EstimatedShots    int             `json:"estimatedShots,omitempty"`
	Notes             string          `json:"notes,omitempty"`
	Raw               json.RawMessage `json:"raw"`
}

type novelChapterRecord struct {
	ID           string
	ChapterIndex int
	VolumeTitle  string
	ChapterTitle string
	Content      string
}

type adaptationPlanRecord struct {
	ID               string
	SourceID         string
	Title            string
	Content          string
	Structure        json.RawMessage
	SelectedEventIDs []string
}

func ExtractNovelEventsWorkflow(ctx workflow.Context, input TextToStoryboardInput) (ExtractNovelEventsOutput, error) {
	options := resolveExtractNovelEventsOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output ExtractNovelEventsOutput
	if err := workflow.ExecuteActivity(ctx, "ExtractNovelEvents", ExtractNovelEventsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		SourceID:       options.SourceID,
		ChapterIDs:     options.ChapterIDs,
		Force:          options.Force,
	}).Get(ctx, &output); err != nil {
		return ExtractNovelEventsOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteNovelEventExtractionWorkflow", input, output).Get(ctx, nil); err != nil {
		return ExtractNovelEventsOutput{}, err
	}
	return output, nil
}

func GenerateAdaptationPlanWorkflow(ctx workflow.Context, input TextToStoryboardInput) (AdaptationPlanOutput, error) {
	options := resolveGenerateAdaptationPlanOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output AdaptationPlanOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateAdaptationPlan", GenerateAdaptationPlanInput{
		OrganizationID:        input.OrganizationID,
		ProjectID:             input.ProjectID,
		WorkflowRunID:         input.WorkflowRunID,
		CreatedBy:             input.CreatedBy,
		SourceID:              options.SourceID,
		EventIDs:              options.EventIDs,
		TargetFormat:          options.TargetFormat,
		TargetDurationSeconds: options.TargetDurationSeconds,
		MaxShots:              options.MaxShots,
		Instruction:           options.Instruction,
	}).Get(ctx, &output); err != nil {
		return AdaptationPlanOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteAdaptationPlanWorkflow", input, output).Get(ctx, nil); err != nil {
		return AdaptationPlanOutput{}, err
	}
	return output, nil
}

func AdaptationPlanToScriptWorkflow(ctx workflow.Context, input TextToStoryboardInput) (AdaptationScriptOutput, error) {
	options := resolveAdaptationPlanToScriptOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output AdaptationScriptOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateScriptFromAdaptationPlan", GenerateScriptFromPlanInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		PlanID:         options.PlanID,
		Title:          options.Title,
		Instruction:    options.Instruction,
	}).Get(ctx, &output); err != nil {
		return AdaptationScriptOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteAdaptationPlanToScriptWorkflow", input, output).Get(ctx, nil); err != nil {
		return AdaptationScriptOutput{}, err
	}
	return output, nil
}

func (a Activities) ExtractNovelEvents(ctx context.Context, input ExtractNovelEventsInput) (ExtractNovelEventsOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "extract_novel_events", CreatedBy: input.CreatedBy}
	if err := validateExtractNovelEventsInput(input); err != nil {
		return ExtractNovelEventsOutput{}, err
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if source.SourceType != "novel" {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: provider.CodeInvalidRequest, Message: "sourceType must be novel"})
	}
	chapters, err := a.loadNovelChapters(ctx, input.ProjectID, input.SourceID, input.ChapterIDs)
	if err != nil {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if len(chapters) == 0 {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: provider.CodeInvalidRequest, Message: "source has no chapters to extract"})
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	if a.gateway == nil {
		return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
	}

	output := ExtractNovelEventsOutput{SourceID: input.SourceID, ChapterCount: len(chapters)}
	for _, chapter := range chapters {
		rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyNovelEventExtraction, map[string]any{
			"project": project.asPromptVariables(),
			"source":  map[string]any{"id": source.ID, "title": source.Title, "sourceType": source.SourceType},
			"chapter": map[string]any{"id": chapter.ID, "index": chapter.ChapterIndex, "title": chapterTitle(chapter), "content": chapter.Content},
		})
		if err != nil {
			return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, "", err)
		}
		nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID: input.OrganizationID,
			ProjectID:      input.ProjectID,
			WorkflowRunID:  input.WorkflowRunID,
			NodeKey:        nodeKeyForID(nodeExtractNovelEventsKey, chapter.ID),
			NodeType:       "agent.novel_event_extract",
			Input: mustJSON(map[string]any{
				"sourceId":          input.SourceID,
				"chapterId":         chapter.ID,
				"chapterIndex":      chapter.ChapterIndex,
				"force":             input.Force,
				"modelProfileKey":   project.ScriptModelProfileKey,
				"promptTemplateKey": rendered.TemplateKey,
				"promptVersionId":   rendered.PromptVersionID,
				"promptHash":        rendered.RenderedHash,
				"promptSource":      rendered.Source,
			}),
		})
		if err != nil {
			return ExtractNovelEventsOutput{}, err
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
			return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
		}
		extraction, err := NormalizeNovelEventExtraction(gatewayResp.Output.Text)
		if err != nil {
			return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
		}
		eventIDs, linkCount, err := a.storeNovelEventExtraction(ctx, input, source, chapter, rendered, gatewayResp, extraction)
		if err != nil {
			return ExtractNovelEventsOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
		}
		output.EventCount += len(eventIDs)
		output.LinkCount += linkCount
		output.ProviderCallIDs = append(output.ProviderCallIDs, gatewayResp.ProviderCallID)
		output.ModelIDs = appendUniqueString(output.ModelIDs, gatewayResp.ModelID)
		if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(map[string]any{"eventIds": eventIDs, "linkCount": linkCount})); err != nil {
			return ExtractNovelEventsOutput{}, err
		}
	}
	return output, nil
}

func (a Activities) GenerateAdaptationPlan(ctx context.Context, input GenerateAdaptationPlanInput) (AdaptationPlanOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "generate_adaptation_plan", CreatedBy: input.CreatedBy}
	if err := validateGenerateAdaptationPlanInput(input); err != nil {
		return AdaptationPlanOutput{}, err
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if source.SourceType != "novel" {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: provider.CodeInvalidRequest, Message: "sourceType must be novel"})
	}
	events, warning, err := a.selectEventsForAdaptationPlan(ctx, input.ProjectID, input.SourceID, input.EventIDs)
	if err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if len(events) == 0 {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: provider.CodeInvalidRequest, Message: "no novel events are available for adaptation plan"})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyAdaptationPlanGeneration, map[string]any{
		"project": project.asPromptVariables(),
		"input": map[string]any{
			"targetFormat":          firstNonEmptyString(input.TargetFormat, defaultAdaptationTargetFormat),
			"targetDurationSeconds": input.TargetDurationSeconds,
			"maxShots":              input.MaxShots,
			"instruction":           strings.TrimSpace(input.Instruction),
		},
		"events": map[string]any{"items": string(mustJSON(events))},
	})
	if err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateAdaptationPlanKey,
		NodeType:       "agent.adaptation_plan",
		Input: mustJSON(map[string]any{
			"sourceId":          input.SourceID,
			"eventIds":          input.EventIDs,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return AdaptationPlanOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
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
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	draft, err := NormalizeAdaptationPlan(gatewayResp.Output.Text, events)
	if err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: err.Error()})
	}
	output, err := a.insertAdaptationPlan(ctx, input, rendered, gatewayResp, draft, warning)
	if err != nil {
		return AdaptationPlanOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return AdaptationPlanOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateScriptFromAdaptationPlan(ctx context.Context, input GenerateScriptFromPlanInput) (AdaptationScriptOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "adaptation_plan_to_script", CreatedBy: input.CreatedBy}
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.PlanID) == "" {
		return AdaptationScriptOutput{}, fmt.Errorf("organizationId, projectId, and planId are required")
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	plan, err := a.adaptationPlan(ctx, input.ProjectID, input.PlanID)
	if err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	events, err := a.eventsByIDs(ctx, input.ProjectID, plan.SourceID, plan.SelectedEventIDs)
	if err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyScriptFromAdaptationPlan, map[string]any{
		"project": project.asPromptVariables(),
		"input":   map[string]any{"instruction": strings.TrimSpace(input.Instruction)},
		"plan":    map[string]any{"id": plan.ID, "title": plan.Title, "content": plan.Content, "structure": string(plan.Structure)},
		"events":  map[string]any{"items": string(mustJSON(events))},
	})
	if err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateScriptFromPlanKey,
		NodeType:       "agent.script_generate",
		Input: mustJSON(map[string]any{
			"planId":            input.PlanID,
			"sourceId":          plan.SourceID,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return AdaptationScriptOutput{}, err
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"})
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
		Input:             mustJSON(map[string]any{"prompt": rendered.RenderedText}),
	})
	if err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowErrorFromProvider(err, codeActivityFailed))
	}
	content := strings.TrimSpace(gatewayResp.Output.Text)
	if content == "" {
		content = strings.TrimSpace(string(gatewayResp.Output.Raw))
	}
	if content == "" {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned empty script content"})
	}
	output, err := a.createGeneratedScriptFromPlan(ctx, input, plan, rendered, gatewayResp, content)
	if err != nil {
		return AdaptationScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return AdaptationScriptOutput{}, err
	}
	return output, nil
}

func (a Activities) CompleteNovelEventExtractionWorkflow(ctx context.Context, input TextToStoryboardInput, output ExtractNovelEventsOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) CompleteAdaptationPlanWorkflow(ctx context.Context, input TextToStoryboardInput, output AdaptationPlanOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func (a Activities) CompleteAdaptationPlanToScriptWorkflow(ctx context.Context, input TextToStoryboardInput, output AdaptationScriptOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func NormalizeNovelEventExtraction(text string) (NovelEventExtraction, error) {
	candidate := stripJSONFence(text)
	var decoded NovelEventExtraction
	if err := json.Unmarshal([]byte(candidate), &decoded); err != nil {
		return NovelEventExtraction{}, err
	}
	out := NovelEventExtraction{Events: make([]NovelEventCandidate, 0, len(decoded.Events)), Links: make([]NovelEventLinkCandidate, 0, len(decoded.Links))}
	for i, event := range decoded.Events {
		event.EventIndex = i + 1
		event.Title = strings.TrimSpace(event.Title)
		event.Summary = strings.TrimSpace(event.Summary)
		if event.Title == "" && event.Summary == "" {
			continue
		}
		if event.Title == "" {
			event.Title = "事件 " + strconv.Itoa(event.EventIndex)
		}
		if event.Summary == "" {
			event.Summary = event.Title
		}
		event.EventType = strings.TrimSpace(event.EventType)
		if event.Importance <= 0 {
			event.Importance = 3
		}
		if event.Importance < 1 {
			event.Importance = 1
		}
		if event.Importance > 5 {
			event.Importance = 5
		}
		event.TimelineHint = strings.TrimSpace(event.TimelineHint)
		event.LocationHint = strings.TrimSpace(event.LocationHint)
		event.EmotionalTone = strings.TrimSpace(event.EmotionalTone)
		event.Conflict = strings.TrimSpace(event.Conflict)
		event.Outcome = strings.TrimSpace(event.Outcome)
		event.AdaptationHint = strings.TrimSpace(event.AdaptationHint)
		event.Characters = normalizeStringSlice(event.Characters)
		event.Scenes = normalizeStringSlice(event.Scenes)
		event.Props = normalizeStringSlice(event.Props)
		event.Keywords = normalizeStringSlice(event.Keywords)
		event.RawExcerpt = strings.TrimSpace(event.RawExcerpt)
		out.Events = append(out.Events, event)
	}
	validIndexes := map[int]bool{}
	for _, event := range out.Events {
		validIndexes[event.EventIndex] = true
	}
	for _, link := range decoded.Links {
		link.LinkType = normalizeNovelEventLinkType(link.LinkType)
		link.Description = strings.TrimSpace(link.Description)
		if link.LinkType == "" || !validIndexes[link.SourceEventIndex] || !validIndexes[link.TargetEventIndex] || link.SourceEventIndex == link.TargetEventIndex {
			continue
		}
		out.Links = append(out.Links, link)
	}
	return out, nil
}

func NormalizeAdaptationPlan(text string, events []NovelEventRecord) (AdaptationPlanDraft, error) {
	candidate := stripJSONFence(text)
	var decoded struct {
		Title             string          `json:"title"`
		Logline           string          `json:"logline"`
		Theme             string          `json:"theme"`
		Structure         json.RawMessage `json:"structure"`
		SelectedEvents    []string        `json:"selectedEvents"`
		OmittedEvents     json.RawMessage `json:"omittedEvents"`
		VisualStrategy    string          `json:"visualStrategy"`
		CharacterStrategy string          `json:"characterStrategy"`
		ShotStrategy      string          `json:"shotStrategy"`
		EstimatedShots    int             `json:"estimatedShots"`
		Notes             string          `json:"notes"`
	}
	if err := json.Unmarshal([]byte(candidate), &decoded); err != nil {
		return AdaptationPlanDraft{}, err
	}
	raw := mustJSON(decoded)
	title := strings.TrimSpace(decoded.Title)
	if title == "" {
		title = "改编计划"
	}
	structure := jsonOrDefault(decoded.Structure, `{}`)
	omitted := jsonOrDefault(decoded.OmittedEvents, `[]`)
	selected := eventIDsFromReferences(decoded.SelectedEvents, events)
	if len(selected) == 0 {
		for _, event := range events {
			selected = append(selected, event.ID)
		}
	}
	return AdaptationPlanDraft{
		Title:             title,
		Logline:           strings.TrimSpace(decoded.Logline),
		Theme:             strings.TrimSpace(decoded.Theme),
		Structure:         structure,
		SelectedEvents:    selected,
		OmittedEvents:     omitted,
		VisualStrategy:    strings.TrimSpace(decoded.VisualStrategy),
		CharacterStrategy: strings.TrimSpace(decoded.CharacterStrategy),
		ShotStrategy:      strings.TrimSpace(decoded.ShotStrategy),
		EstimatedShots:    decoded.EstimatedShots,
		Notes:             strings.TrimSpace(decoded.Notes),
		Raw:               raw,
	}, nil
}

func resolveExtractNovelEventsOptions(raw json.RawMessage) ExtractNovelEventsOptions {
	var options ExtractNovelEventsOptions
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &options)
	}
	options.SourceID = strings.TrimSpace(options.SourceID)
	options.ChapterIDs = normalizeStringSlice(options.ChapterIDs)
	return options
}

func resolveGenerateAdaptationPlanOptions(raw json.RawMessage) GenerateAdaptationPlanOptions {
	var options GenerateAdaptationPlanOptions
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &options)
	}
	options.SourceID = strings.TrimSpace(options.SourceID)
	options.EventIDs = normalizeStringSlice(options.EventIDs)
	options.TargetFormat = firstNonEmptyString(options.TargetFormat, defaultAdaptationTargetFormat)
	options.Instruction = strings.TrimSpace(options.Instruction)
	return options
}

func resolveAdaptationPlanToScriptOptions(raw json.RawMessage) AdaptationPlanToScriptOptions {
	var options AdaptationPlanToScriptOptions
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &options)
	}
	options.PlanID = strings.TrimSpace(options.PlanID)
	options.Title = strings.TrimSpace(options.Title)
	options.Instruction = strings.TrimSpace(options.Instruction)
	return options
}

func validateExtractNovelEventsInput(input ExtractNovelEventsInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.SourceID) == "" {
		return fmt.Errorf("organizationId, projectId, workflowRunId, and sourceId are required")
	}
	return nil
}

func validateGenerateAdaptationPlanInput(input GenerateAdaptationPlanInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SourceID) == "" {
		return fmt.Errorf("organizationId, projectId, and sourceId are required")
	}
	return nil
}

func (a Activities) loadNovelChapters(ctx context.Context, projectID, sourceID string, chapterIDs []string) ([]novelChapterRecord, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id::text, chapter_index, COALESCE(volume_title, ''), COALESCE(chapter_title, ''), content
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2
		ORDER BY chapter_index ASC
	`, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	filter := map[string]bool{}
	for _, id := range chapterIDs {
		filter[id] = true
	}
	items := make([]novelChapterRecord, 0)
	for rows.Next() {
		var item novelChapterRecord
		if err := rows.Scan(&item.ID, &item.ChapterIndex, &item.VolumeTitle, &item.ChapterTitle, &item.Content); err != nil {
			return nil, err
		}
		if len(filter) > 0 && !filter[item.ID] {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a Activities) storeNovelEventExtraction(ctx context.Context, input ExtractNovelEventsInput, source ProjectSourceRecord, chapter novelChapterRecord, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, extraction NovelEventExtraction) ([]string, int, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	idsByIndex := map[int]string{}
	eventIDs := make([]string, 0, len(extraction.Events))
	for _, event := range extraction.Events {
		id, err := upsertNovelEventTx(ctx, tx, input, source, chapter, event, rendered, gatewayResp)
		if err != nil {
			return nil, 0, err
		}
		idsByIndex[event.EventIndex] = id
		eventIDs = append(eventIDs, id)
	}
	linkCount := 0
	for _, link := range extraction.Links {
		sourceID := idsByIndex[link.SourceEventIndex]
		targetID := idsByIndex[link.TargetEventIndex]
		if sourceID == "" || targetID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO novel_event_links(organization_id, project_id, source_event_id, target_event_id, link_type, description, metadata)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)
			ON CONFLICT (source_event_id, target_event_id, link_type) DO UPDATE SET
			  description = EXCLUDED.description,
			  metadata = COALESCE(novel_event_links.metadata, '{}'::jsonb) || EXCLUDED.metadata
		`, input.OrganizationID, input.ProjectID, sourceID, targetID, link.LinkType, link.Description, mustJSON(map[string]any{"source": "novel_event_extraction", "providerCallId": gatewayResp.ProviderCallID})); err != nil {
			return nil, 0, err
		}
		linkCount++
	}
	if _, err := tx.Exec(ctx, `
		UPDATE novel_chapters
		SET event_state = 'extracted',
		    event_summary = $3,
		    error_message = NULL,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, input.ProjectID, chapter.ID, mustJSON(map[string]any{"eventCount": len(eventIDs), "eventIds": eventIDs, "linkCount": linkCount})); err != nil {
		return nil, 0, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "novel.events.extracted", "project_source", source.ID, mustJSON(map[string]any{
		"sourceId":      source.ID,
		"chapterId":     chapter.ID,
		"eventCount":    len(eventIDs),
		"linkCount":     linkCount,
		"workflowRunId": input.WorkflowRunID,
	})); err != nil {
		return nil, 0, err
	}
	return eventIDs, linkCount, tx.Commit(ctx)
}

func upsertNovelEventTx(ctx context.Context, tx pgx.Tx, input ExtractNovelEventsInput, source ProjectSourceRecord, chapter novelChapterRecord, event NovelEventCandidate, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse) (string, error) {
	var existingID string
	var manualOverride bool
	err := tx.QueryRow(ctx, `
		SELECT id::text, manual_override
		FROM novel_events
		WHERE chapter_id = $1 AND event_index = $2
	`, chapter.ID, event.EventIndex).Scan(&existingID, &manualOverride)
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}
	sequenceNo := chapter.ChapterIndex*1000 + event.EventIndex
	metadata := mustJSON(map[string]any{
		"source":             "novel_event_extraction",
		"workflowRunId":      input.WorkflowRunID,
		"providerCallId":     gatewayResp.ProviderCallID,
		"modelId":            gatewayResp.ModelID,
		"promptTemplateKey":  rendered.TemplateKey,
		"promptVersionId":    rendered.PromptVersionID,
		"promptHash":         rendered.RenderedHash,
		"overwrittenByAgent": input.Force,
	})
	if err == pgx.ErrNoRows {
		var id string
		err = tx.QueryRow(ctx, novelEventInsertSQL(), input.OrganizationID, input.ProjectID, source.ID, chapter.ID, event.EventIndex, sequenceNo,
			event.Title, event.Summary, nullableText(event.EventType), event.Importance, nullableText(event.TimelineHint), nullableText(event.LocationHint),
			nullableText(event.EmotionalTone), nullableText(event.Conflict), nullableText(event.Outcome), nullableText(event.AdaptationHint),
			mustJSON(event.Characters), mustJSON(event.Scenes), mustJSON(event.Props), mustJSON(event.Keywords), nullableText(event.RawExcerpt),
			metadata, input.CreatedBy).Scan(&id)
		return id, err
	}
	if manualOverride && !input.Force {
		_, err := tx.Exec(ctx, `
			UPDATE novel_events
			SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('agentLastSuggestion', $2::jsonb),
			    updated_at = now()
			WHERE id = $1
		`, existingID, metadata)
		return existingID, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE novel_events
		SET source_id = $3,
		    sequence_no = $4,
		    title = $5,
		    summary = $6,
		    event_type = $7,
		    importance = $8,
		    timeline_hint = $9,
		    location_hint = $10,
		    emotional_tone = $11,
		    conflict = $12,
		    outcome = $13,
		    adaptation_hint = $14,
		    characters = $15,
		    scenes = $16,
		    props = $17,
		    keywords = $18,
		    raw_excerpt = $19,
		    review_status = 'pending',
		    stale_state = 'fresh',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $20::jsonb,
		    updated_at = now()
		WHERE id = $1 AND project_id = $2
	`, existingID, input.ProjectID, source.ID, sequenceNo, event.Title, event.Summary, nullableText(event.EventType), event.Importance,
		nullableText(event.TimelineHint), nullableText(event.LocationHint), nullableText(event.EmotionalTone), nullableText(event.Conflict),
		nullableText(event.Outcome), nullableText(event.AdaptationHint), mustJSON(event.Characters), mustJSON(event.Scenes), mustJSON(event.Props),
		mustJSON(event.Keywords), nullableText(event.RawExcerpt), metadata)
	return existingID, err
}

func novelEventInsertSQL() string {
	return `
		INSERT INTO novel_events(
			organization_id, project_id, source_id, chapter_id, event_index, sequence_no,
			title, summary, event_type, importance, timeline_hint, location_hint,
			emotional_tone, conflict, outcome, adaptation_hint,
			characters, scenes, props, keywords, raw_excerpt, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		RETURNING id::text
	`
}

func (a Activities) selectEventsForAdaptationPlan(ctx context.Context, projectID, sourceID string, eventIDs []string) ([]NovelEventRecord, string, error) {
	if len(eventIDs) > 0 {
		events, err := a.eventsByIDs(ctx, projectID, sourceID, eventIDs)
		return events, "", err
	}
	approved, err := a.eventsByReviewStatus(ctx, projectID, sourceID, "approved")
	if err != nil {
		return nil, "", err
	}
	if len(approved) > 0 {
		return approved, "", nil
	}
	pending, err := a.eventsByReviewStatus(ctx, projectID, sourceID, "pending")
	if err != nil {
		return nil, "", err
	}
	if len(pending) > 0 {
		return pending, "No approved events were available, so pending events were used.", nil
	}
	return nil, "", nil
}

func (a Activities) countNovelEventsForSource(ctx context.Context, projectID, sourceID string) (int, error) {
	var count int
	err := a.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM novel_events
		WHERE project_id = $1 AND source_id = $2
	`, projectID, sourceID).Scan(&count)
	return count, err
}

func (a Activities) eventsByReviewStatus(ctx context.Context, projectID, sourceID, status string) ([]NovelEventRecord, error) {
	rows, err := a.db.Query(ctx, novelEventsSelectSQL(`
		WHERE e.project_id = $1 AND e.source_id = $2 AND e.review_status = $3
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNovelEventRecords(rows)
}

func (a Activities) eventsByIDs(ctx context.Context, projectID, sourceID string, eventIDs []string) ([]NovelEventRecord, error) {
	if len(eventIDs) == 0 {
		return a.eventsBySource(ctx, projectID, sourceID)
	}
	rows, err := a.db.Query(ctx, novelEventsSelectSQL(`
		WHERE e.project_id = $1
		  AND ($2 = '' OR e.source_id = $2::uuid)
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanNovelEventRecords(rows)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range eventIDs {
		wanted[strings.TrimSpace(id)] = true
	}
	out := make([]NovelEventRecord, 0, len(eventIDs))
	for _, event := range all {
		if wanted[event.ID] {
			out = append(out, event)
		}
	}
	return out, nil
}

func (a Activities) eventsBySource(ctx context.Context, projectID, sourceID string) ([]NovelEventRecord, error) {
	rows, err := a.db.Query(ctx, novelEventsSelectSQL(`
		WHERE e.project_id = $1 AND e.source_id = $2
		ORDER BY e.sequence_no ASC
	`), projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNovelEventRecords(rows)
}

func novelEventsSelectSQL(where string) string {
	return `
		SELECT e.id::text, e.source_id::text, COALESCE(e.chapter_id::text, ''), COALESCE(c.chapter_index, 0),
		       e.event_index, e.sequence_no, e.title, e.summary, COALESCE(e.event_type, ''),
		       e.importance, COALESCE(e.timeline_hint, ''), COALESCE(e.location_hint, ''),
		       COALESCE(e.emotional_tone, ''), COALESCE(e.conflict, ''), COALESCE(e.outcome, ''),
		       COALESCE(e.adaptation_hint, ''), e.characters, e.scenes, e.props, e.keywords,
		       COALESCE(e.raw_excerpt, ''), e.review_status
		FROM novel_events e
		LEFT JOIN novel_chapters c ON c.id = e.chapter_id
	` + where
}

type novelEventRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanNovelEventRecords(rows novelEventRows) ([]NovelEventRecord, error) {
	items := make([]NovelEventRecord, 0)
	for rows.Next() {
		var item NovelEventRecord
		var characters, scenes, props, keywords []byte
		if err := rows.Scan(&item.ID, &item.SourceID, &item.ChapterID, &item.ChapterIndex, &item.EventIndex, &item.SequenceNo, &item.Title, &item.Summary, &item.EventType, &item.Importance, &item.TimelineHint, &item.LocationHint, &item.EmotionalTone, &item.Conflict, &item.Outcome, &item.AdaptationHint, &characters, &scenes, &props, &keywords, &item.RawExcerpt, &item.ReviewStatus); err != nil {
			return nil, err
		}
		item.Characters = jsonOrDefault(characters, `[]`)
		item.Scenes = jsonOrDefault(scenes, `[]`)
		item.Props = jsonOrDefault(props, `[]`)
		item.Keywords = jsonOrDefault(keywords, `[]`)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a Activities) insertAdaptationPlan(ctx context.Context, input GenerateAdaptationPlanInput, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, draft AdaptationPlanDraft, warning string) (AdaptationPlanOutput, error) {
	metadata := map[string]any{
		"source":            "adaptation_plan_generation",
		"providerCallId":    gatewayResp.ProviderCallID,
		"modelId":           gatewayResp.ModelID,
		"promptTemplateKey": rendered.TemplateKey,
		"promptVersionId":   rendered.PromptVersionID,
		"promptHash":        rendered.RenderedHash,
		"logline":           draft.Logline,
		"theme":             draft.Theme,
		"omittedEvents":     json.RawMessage(draft.OmittedEvents),
		"visualStrategy":    draft.VisualStrategy,
		"characterStrategy": draft.CharacterStrategy,
		"shotStrategy":      draft.ShotStrategy,
		"estimatedShots":    draft.EstimatedShots,
		"notes":             draft.Notes,
	}
	if warning != "" {
		metadata["warning"] = warning
	}
	var planID string
	if err := a.db.QueryRow(ctx, `
		INSERT INTO adaptation_plans(
			organization_id, project_id, source_id, title, target_format, target_duration_seconds,
			max_shots, selected_event_ids, structure, content, prompt_version_id, prompt_hash,
			metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, 0), NULLIF($7, 0), $8, $9, $10, NULLIF($11, '')::uuid, NULLIF($12, ''), $13, $14)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, input.SourceID, draft.Title, firstNonEmptyString(input.TargetFormat, defaultAdaptationTargetFormat),
		input.TargetDurationSeconds, input.MaxShots, mustJSON(draft.SelectedEvents), jsonOrDefault(draft.Structure, `{}`), string(draft.Raw),
		rendered.PromptVersionID, rendered.RenderedHash, mustJSON(metadata), input.CreatedBy).Scan(&planID); err != nil {
		return AdaptationPlanOutput{}, err
	}
	return AdaptationPlanOutput{
		PlanID:         planID,
		SourceID:       input.SourceID,
		Title:          draft.Title,
		SelectedEvents: draft.SelectedEvents,
		Content:        string(draft.Raw),
		Structure:      jsonOrDefault(draft.Structure, `{}`),
		ProviderCallID: gatewayResp.ProviderCallID,
		ModelID:        gatewayResp.ModelID,
		Warning:        warning,
	}, nil
}

func (a Activities) adaptationPlan(ctx context.Context, projectID, planID string) (adaptationPlanRecord, error) {
	var item adaptationPlanRecord
	var sourceID sql.NullString
	var selected []byte
	err := a.db.QueryRow(ctx, `
		SELECT id::text, COALESCE(source_id::text, ''), title, content, structure, selected_event_ids
		FROM adaptation_plans
		WHERE project_id = $1 AND id = $2
	`, projectID, planID).Scan(&item.ID, &sourceID, &item.Title, &item.Content, &item.Structure, &selected)
	item.SourceID = sourceID.String
	item.Structure = jsonOrDefault(item.Structure, `{}`)
	_ = json.Unmarshal(jsonOrDefault(selected, `[]`), &item.SelectedEventIDs)
	return item, err
}

func (a Activities) createGeneratedScriptFromPlan(ctx context.Context, input GenerateScriptFromPlanInput, plan adaptationPlanRecord, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, content string) (AdaptationScriptOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return AdaptationScriptOutput{}, err
	}
	defer tx.Rollback(ctx)
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = plan.Title + " Script"
	}
	title, err = uniqueScriptTitle(ctx, tx, input.ProjectID, title)
	if err != nil {
		return AdaptationScriptOutput{}, err
	}
	var scriptID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, 'active', $5)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, plan.SourceID, title, input.CreatedBy).Scan(&scriptID); err != nil {
		return AdaptationScriptOutput{}, err
	}
	var versionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO script_versions(
			organization_id, project_id, script_id, version_no, version, content,
			content_format, source_type, prompt_version_id, prompt_hash, metadata, created_by
		)
		VALUES ($1, $2, $3, 1, 1, $4, 'markdown', 'agent_generated', NULLIF($5, '')::uuid, NULLIF($6, ''), $7, $8)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, scriptID, content, rendered.PromptVersionID, rendered.RenderedHash, mustJSON(map[string]any{
		"source":           "adaptation_plan_to_script",
		"adaptationPlanId": plan.ID,
		"sourceId":         plan.SourceID,
		"providerCallId":   gatewayResp.ProviderCallID,
		"modelId":          gatewayResp.ModelID,
	}), input.CreatedBy).Scan(&versionID); err != nil {
		return AdaptationScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		return AdaptationScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE adaptation_plans SET script_id = $2, updated_at = now() WHERE id = $1`, plan.ID, scriptID); err != nil {
		return AdaptationScriptOutput{}, err
	}
	if plan.SourceID != "" {
		if _, err := tx.Exec(ctx, `UPDATE project_sources SET status = 'processed' WHERE id = $1`, plan.SourceID); err != nil {
			return AdaptationScriptOutput{}, err
		}
	}
	output := AdaptationScriptOutput{
		PlanID:          plan.ID,
		SourceID:        plan.SourceID,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
		Content:         content,
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.generated", "script", scriptID, mustJSON(map[string]any{
		"scriptId":         scriptID,
		"scriptVersionId":  versionID,
		"adaptationPlanId": plan.ID,
		"sourceId":         plan.SourceID,
		"workflowRunId":    input.WorkflowRunID,
	})); err != nil {
		return AdaptationScriptOutput{}, err
	}
	return output, tx.Commit(ctx)
}

func uniqueScriptTitle(ctx context.Context, tx pgx.Tx, projectID, baseTitle string) (string, error) {
	baseTitle = strings.TrimSpace(baseTitle)
	if baseTitle == "" {
		baseTitle = "Adapted Script"
	}
	for suffix := 1; suffix < 1000; suffix++ {
		candidate := baseTitle
		if suffix > 1 {
			candidate = fmt.Sprintf("%s (%d)", baseTitle, suffix)
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scripts WHERE project_id = $1 AND title = $2)`, projectID, candidate).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("script title conflict: %s", baseTitle)
}

func normalizeStringSlice(values []string) []string {
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

func normalizeNovelEventLinkType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "next", "causes", "foreshadows", "resolves", "parallels":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func eventIDsFromReferences(refs []string, events []NovelEventRecord) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		id := eventIDFromReference(ref, events)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func eventIDFromReference(ref string, events []NovelEventRecord) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	for _, event := range events {
		if ref == event.ID || ref == strconv.Itoa(event.EventIndex) || ref == strconv.Itoa(event.SequenceNo) || strings.EqualFold(ref, event.Title) {
			return event.ID
		}
	}
	return ""
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func chapterTitle(chapter novelChapterRecord) string {
	return firstNonEmptyString(chapter.ChapterTitle, chapter.VolumeTitle, "第 "+strconv.Itoa(chapter.ChapterIndex)+" 章")
}

func nullableText(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
