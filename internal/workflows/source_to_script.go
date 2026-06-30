package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/workflow"
)

const (
	nodeGenerateScriptFromSourceKey = "generate_script_from_source"
	promptKeyScriptAgentGenerate    = "script_agent_generate"
)

type SourceToScriptOptions struct {
	SourceID    string `json:"sourceId"`
	Instruction string `json:"instruction,omitempty"`
	Title       string `json:"title,omitempty"`
}

type ProjectSourceRecord struct {
	ID            string `json:"id"`
	SourceType    string `json:"sourceType"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	ContentFormat string `json:"contentFormat"`
}

type GenerateScriptFromSourceInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	CreatedBy      string `json:"createdBy"`
	SourceID       string `json:"sourceId"`
	Instruction    string `json:"instruction,omitempty"`
	Title          string `json:"title,omitempty"`
}

type SourceToScriptOutput struct {
	SourceID        string `json:"sourceId"`
	ScriptID        string `json:"scriptId"`
	ScriptVersionID string `json:"scriptVersionId"`
	AgentRunID      string `json:"agentRunId,omitempty"`
	ProviderCallID  string `json:"providerCallId,omitempty"`
	ModelID         string `json:"modelId,omitempty"`
	Content         string `json:"content"`
}

func SourceToScriptWorkflow(ctx workflow.Context, input TextToStoryboardInput) (SourceToScriptOutput, error) {
	options := resolveSourceToScriptOptions(input.Input)
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var output SourceToScriptOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateScriptFromSource", GenerateScriptFromSourceInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		SourceID:       options.SourceID,
		Instruction:    options.Instruction,
		Title:          options.Title,
	}).Get(ctx, &output); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteSourceToScriptWorkflow", input, output).Get(ctx, nil); err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateScriptFromSource(ctx context.Context, input GenerateScriptFromSourceInput) (SourceToScriptOutput, error) {
	baseInput := TextToStoryboardInput{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID, Prompt: "source_to_script", CreatedBy: input.CreatedBy}
	if err := validateSourceToScriptInput(input); err != nil {
		return SourceToScriptOutput{}, err
	}
	project, err := a.projectProductionSettings(ctx, input.ProjectID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	source, err := a.projectSourceRecord(ctx, input.ProjectID, input.SourceID)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, "", workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	rendered, err := a.renderWorkflowPrompt(ctx, input.OrganizationID, input.ProjectID, promptKeyScriptAgentGenerate, map[string]any{
		"project": project.asPromptVariables(),
		"source": map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"sourceType": source.SourceType,
			"content":    source.Content,
		},
		"input": map[string]any{"instruction": strings.TrimSpace(input.Instruction)},
	})
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, "", err)
	}
	nodeRunID, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        nodeGenerateScriptFromSourceKey,
		NodeType:       "agent.script_generate",
		Input: mustJSON(map[string]any{
			"sourceId":          input.SourceID,
			"modelProfileKey":   project.ScriptModelProfileKey,
			"promptTemplateKey": rendered.TemplateKey,
			"promptVersionId":   rendered.PromptVersionID,
			"promptHash":        rendered.RenderedHash,
			"promptSource":      rendered.Source,
		}),
	})
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	agentRunID, err := a.startScriptAgentRun(ctx, input, source, rendered)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := a.ensureModelProfileConfigured(ctx, input.OrganizationID, project.ScriptModelProfileKey, []string{"text", "multimodal"}); err != nil {
		code, message := workflowErrorFields(err, codeActivityFailed)
		_ = a.failAgentRun(ctx, agentRunID, code, message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	if a.gateway == nil {
		err := workflowError{Code: provider.CodeProviderGatewayRequired, Message: "provider gateway client is not configured"}
		_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
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
		cause := workflowErrorFromProvider(err, codeActivityFailed)
		code, message := workflowErrorFields(cause, codeActivityFailed)
		_ = a.failAgentRun(ctx, agentRunID, code, message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, cause)
	}
	content := strings.TrimSpace(gatewayResp.Output.Text)
	if content == "" {
		content = strings.TrimSpace(string(gatewayResp.Output.Raw))
	}
	if content == "" {
		err := workflowError{Code: provider.CodeInvalidRequest, Message: "provider gateway returned empty script content"}
		_ = a.failAgentRun(ctx, agentRunID, err.Code, err.Message)
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, err)
	}
	output, err := a.createGeneratedScriptFromSource(ctx, input, source, rendered, gatewayResp, agentRunID, content)
	if err != nil {
		return SourceToScriptOutput{}, a.failActivity(ctx, baseInput, nodeRunID, workflowError{Code: codeActivityFailed, Message: err.Error()})
	}
	if err := CompleteNodeRun(ctx, a.db, nodeRunID, mustJSON(output)); err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}

func (a Activities) CompleteSourceToScriptWorkflow(ctx context.Context, input TextToStoryboardInput, output SourceToScriptOutput) error {
	return a.completeSimpleWorkflow(ctx, input, output)
}

func resolveSourceToScriptOptions(raw json.RawMessage) SourceToScriptOptions {
	var options SourceToScriptOptions
	if len(raw) == 0 {
		return options
	}
	_ = json.Unmarshal(raw, &options)
	options.SourceID = strings.TrimSpace(options.SourceID)
	options.Instruction = strings.TrimSpace(options.Instruction)
	options.Title = strings.TrimSpace(options.Title)
	return options
}

func validateSourceToScriptInput(input GenerateScriptFromSourceInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.SourceID) == "" {
		return fmt.Errorf("organizationId, projectId, workflowRunId, and sourceId are required")
	}
	return nil
}

func (a Activities) projectSourceRecord(ctx context.Context, projectID, sourceID string) (ProjectSourceRecord, error) {
	var item ProjectSourceRecord
	err := a.db.QueryRow(ctx, `
		SELECT id::text, source_type, title, content, content_format
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, projectID, sourceID).Scan(&item.ID, &item.SourceType, &item.Title, &item.Content, &item.ContentFormat)
	return item, err
}

func (a Activities) startScriptAgentRun(ctx context.Context, input GenerateScriptFromSourceInput, source ProjectSourceRecord, rendered promptsvc.RenderedPrompt) (string, error) {
	var runID string
	err := a.db.QueryRow(ctx, `
		INSERT INTO agent_runs(
			organization_id, project_id, agent_type, task_type, status,
			input, prompt_version_id, prompt_hash, created_by, started_at
		)
		VALUES ($1, $2, 'script_agent', 'generate_script', 'running', $3, NULLIF($4, '')::uuid, NULLIF($5, ''), $6, now())
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, mustJSON(map[string]any{
		"source": map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"sourceType": source.SourceType,
		},
		"input": map[string]any{"instruction": strings.TrimSpace(input.Instruction)},
	}), rendered.PromptVersionID, rendered.RenderedHash, input.CreatedBy).Scan(&runID)
	return runID, err
}

func (a Activities) failAgentRun(ctx context.Context, runID string, codeAndMessage ...string) error {
	if strings.TrimSpace(runID) == "" {
		return nil
	}
	code := codeActivityFailed
	message := "script agent run failed"
	if len(codeAndMessage) > 0 && strings.TrimSpace(codeAndMessage[0]) != "" {
		code = strings.TrimSpace(codeAndMessage[0])
	}
	if len(codeAndMessage) > 1 && strings.TrimSpace(codeAndMessage[1]) != "" {
		message = strings.TrimSpace(codeAndMessage[1])
	}
	_, err := a.db.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
		WHERE id = $1
	`, runID, code, message)
	return err
}

func (a Activities) createGeneratedScriptFromSource(ctx context.Context, input GenerateScriptFromSourceInput, source ProjectSourceRecord, rendered promptsvc.RenderedPrompt, gatewayResp provider.GatewayTextResponse, agentRunID, content string) (SourceToScriptOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return SourceToScriptOutput{}, err
	}
	defer tx.Rollback(ctx)
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(source.Title) + " Script"
	}
	var scriptID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO scripts(organization_id, project_id, source_id, title, status, created_by)
		VALUES ($1, $2, $3, $4, 'active', $5)
		RETURNING id::text
	`, input.OrganizationID, input.ProjectID, source.ID, title, input.CreatedBy).Scan(&scriptID); err != nil {
		return SourceToScriptOutput{}, err
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
		"source":          "source_to_script",
		"sourceId":        source.ID,
		"providerCallId":  gatewayResp.ProviderCallID,
		"modelId":         gatewayResp.ModelID,
		"promptTemplate":  rendered.TemplateKey,
		"promptVersionId": rendered.PromptVersionID,
		"promptHash":      rendered.RenderedHash,
	}), input.CreatedBy).Scan(&versionID); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, scriptID, versionID); err != nil {
		return SourceToScriptOutput{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE project_sources SET status = 'processed' WHERE id = $1`, source.ID); err != nil {
		return SourceToScriptOutput{}, err
	}
	output := SourceToScriptOutput{
		SourceID:        source.ID,
		ScriptID:        scriptID,
		ScriptVersionID: versionID,
		AgentRunID:      agentRunID,
		ProviderCallID:  gatewayResp.ProviderCallID,
		ModelID:         gatewayResp.ModelID,
		Content:         content,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = NULLIF($4, '')::uuid, prompt_hash = NULLIF($5, ''), completed_at = now()
		WHERE id = $1
	`, agentRunID, mustJSON(output), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "script.generated", "script", scriptID, mustJSON(map[string]any{
		"scriptId":        scriptID,
		"scriptVersionId": versionID,
		"sourceId":        source.ID,
		"workflowRunId":   input.WorkflowRunID,
		"agentRunId":      agentRunID,
	})); err != nil {
		return SourceToScriptOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceToScriptOutput{}, err
	}
	return output, nil
}
