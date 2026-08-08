package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

type scriptRewriteActionInput struct {
	ScriptID         string  `json:"scriptId"`
	VersionID        string  `json:"versionId"`
	ExpectedRevision *int64  `json:"expectedRevision,omitempty"`
	Instruction      string  `json:"instruction"`
	SessionID        *string `json:"sessionId,omitempty"`
	Activate         bool    `json:"activate"`
}

type scriptRewritePreviewActionResult struct {
	ScriptID         string `json:"scriptId"`
	VersionID        string `json:"versionId"`
	Content          string `json:"content"`
	ContentFormat    string `json:"contentFormat"`
	AgentRunID       string `json:"agentRunId"`
	ProviderCallID   string `json:"providerCallId,omitempty"`
	ModelID          string `json:"modelId,omitempty"`
	PromptVersionID  string `json:"promptVersionId,omitempty"`
	PromptHash       string `json:"promptHash,omitempty"`
	CommandID        string `json:"projectControlCommandId,omitempty"`
	IdempotentReplay bool   `json:"idempotentReplay"`
}

type scriptRewriteActionResult struct {
	ScriptID          string `json:"scriptId"`
	VersionID         string `json:"versionId"`
	Content           string `json:"content"`
	ContentFormat     string `json:"contentFormat"`
	AgentRunID        string `json:"agentRunId"`
	ProviderCallID    string `json:"providerCallId,omitempty"`
	ModelID           string `json:"modelId,omitempty"`
	PromptVersionID   string `json:"promptVersionId,omitempty"`
	PromptHash        string `json:"promptHash,omitempty"`
	Activated         bool   `json:"activated"`
	PreviousVersionID string `json:"previousVersionId,omitempty"`
	CommandID         string `json:"projectControlCommandId,omitempty"`
	IdempotentReplay  bool   `json:"idempotentReplay"`
}

type scriptAgentRunOutput struct {
	ScriptID        string `json:"scriptId"`
	VersionID       string `json:"versionId"`
	Content         string `json:"content"`
	ContentFormat   string `json:"contentFormat"`
	PreviewOnly     bool   `json:"previewOnly"`
	ProviderCallID  string `json:"providerCallId"`
	ModelID         string `json:"modelId"`
	PromptVersionID string `json:"promptVersionId"`
	PromptHash      string `json:"promptHash"`
}

type scriptAgentRunRecord struct {
	ID              string
	Status          string
	Output          scriptAgentRunOutput
	ProviderCallID  string
	PromptVersionID string
	PromptHash      string
}

func (s *Server) executeScriptRewritePreviewAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input scriptRewriteActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.rewriteScriptPreviewCore(ctx, principal, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("script.rewrite_preview", workflowActionArguments(raw), "已生成剧本改写预览，未创建新版本。", map[string]any{
		"scriptId": result.ScriptID, "versionId": result.VersionID,
		"content": result.Content, "contentFormat": result.ContentFormat, "previewOnly": true,
		"agentRunId": result.AgentRunID, "providerCallId": result.ProviderCallID, "modelId": result.ModelID,
		"promptVersionId": result.PromptVersionID, "promptHash": result.PromptHash,
		"projectControlCommandId": result.CommandID, "idempotentReplay": result.IdempotentReplay,
	}), nil
}

func (s *Server) executeScriptRewriteAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input scriptRewriteActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.rewriteScriptCore(ctx, principal, project, command, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("script.rewrite", workflowActionArguments(raw), "已改写剧本并创建新版本。", map[string]any{
		"scriptId": result.ScriptID, "versionId": result.VersionID,
		"content": result.Content, "contentFormat": result.ContentFormat,
		"activated": result.Activated, "previousVersionId": nullableMetadataValue(result.PreviousVersionID),
		"agentRunId": result.AgentRunID, "providerCallId": result.ProviderCallID, "modelId": result.ModelID,
		"promptVersionId": result.PromptVersionID, "promptHash": result.PromptHash,
		"projectControlCommandId": result.CommandID, "idempotentReplay": result.IdempotentReplay,
	}), nil
}

func (s *Server) rewriteScriptPreviewCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	input scriptRewriteActionInput,
) (scriptRewritePreviewActionResult, error) {
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.ScriptID == "" || input.Instruction == "" {
		return scriptRewritePreviewActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId 和 instruction 不能为空")
	}
	if replay, ok, err := s.scriptRewritePreviewReplay(ctx, project.ID, command.ID); err != nil {
		return scriptRewritePreviewActionResult{}, err
	} else if ok {
		return replay, nil
	}
	r := requestWithContext(ctx)
	script, current, err := s.resolveScriptRewriteTarget(r, project.ID, input)
	if err != nil {
		return scriptRewritePreviewActionResult{}, err
	}
	options := s.scriptRewritePromptOptions(ctx, project, command, "script.rewrite_preview", script.Title, authz.PermissionScriptRead)
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPromptWithOptions(r, principal, project, input.SessionID, "rewrite_preview", "script_agent_rewrite", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": current.ID, "content": current.Content},
		"input":   map[string]any{"instruction": input.Instruction},
	}, options)
	if err != nil {
		return scriptRewritePreviewActionResult{}, err
	}
	output := scriptAgentRunOutput{
		ScriptID: script.ID, VersionID: current.ID, Content: content, ContentFormat: current.ContentFormat,
		PreviewOnly: true, ProviderCallID: gatewayResp.ProviderCallID, ModelID: gatewayResp.ModelID,
		PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash,
	}
	if err := s.completeScriptAgentRun(ctx, runID, output); err != nil {
		return scriptRewritePreviewActionResult{}, err
	}
	return scriptRewritePreviewActionResult{
		ScriptID: script.ID, VersionID: current.ID, Content: content, ContentFormat: current.ContentFormat,
		AgentRunID: runID, ProviderCallID: gatewayResp.ProviderCallID, ModelID: gatewayResp.ModelID,
		PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash, CommandID: command.ID,
	}, nil
}

func (s *Server) rewriteScriptCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	input scriptRewriteActionInput,
) (scriptRewriteActionResult, error) {
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.ScriptID == "" || input.Instruction == "" {
		return scriptRewriteActionResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId 和 instruction 不能为空")
	}
	if replay, ok, err := s.scriptRewriteVersionReplay(ctx, project.ID, command.ID); err != nil {
		return scriptRewriteActionResult{}, err
	} else if ok {
		return replay, nil
	}
	r := requestWithContext(ctx)
	script, current, err := s.resolveScriptRewriteTarget(r, project.ID, input)
	if err != nil {
		return scriptRewriteActionResult{}, err
	}
	if input.ExpectedRevision != nil && *input.ExpectedRevision != script.Revision {
		return scriptRewriteActionResult{}, revisionConflictError("REVISION_CONFLICT", "剧本已被其它操作修改，请重新读取后重试", *input.ExpectedRevision, script.Revision)
	}
	options := s.scriptRewritePromptOptions(ctx, project, command, "script.rewrite", script.Title, authz.PermissionScriptWrite)
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPromptWithOptions(r, principal, project, input.SessionID, "rewrite_script", "script_agent_rewrite", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": current.ID, "content": current.Content},
		"input":   map[string]any{"instruction": input.Instruction},
	}, options)
	if err != nil {
		return scriptRewriteActionResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return scriptRewriteActionResult{}, err
	}
	defer tx.Rollback(ctx)
	var lockedRevision int64
	var lockedCurrentVersionID sql.NullString
	if err := tx.QueryRow(ctx, `
		SELECT revision, current_version_id::text
		FROM scripts
		WHERE project_id = $1 AND id = $2 AND COALESCE(status, 'active') <> 'archived'
		FOR UPDATE
	`, project.ID, script.ID).Scan(&lockedRevision, &lockedCurrentVersionID); err != nil {
		return scriptRewriteActionResult{}, err
	}
	if lockedRevision != script.Revision {
		return scriptRewriteActionResult{}, revisionConflictError("REVISION_CONFLICT", "剧本已被其它操作修改，请重新读取后重试", script.Revision, lockedRevision)
	}
	if replay, ok, err := scriptRewriteVersionReplayTx(ctx, tx, project.ID, command.ID); err != nil {
		return scriptRewriteActionResult{}, err
	} else if ok {
		return replay, nil
	}
	nextVersion, err := nextScriptVersion(r, tx, script.ID)
	if err != nil {
		return scriptRewriteActionResult{}, err
	}
	metadata := map[string]any{
		"agentRunId": runID, "source": "script_agent_rewrite",
		"activated": input.Activate, "previousVersionId": nullableMetadataValue(lockedCurrentVersionID.String),
		"providerCallId": gatewayResp.ProviderCallID, "modelId": gatewayResp.ModelID,
		"promptVersionId": rendered.PromptVersionID, "promptHash": rendered.RenderedHash,
	}
	if command.ID != "" {
		metadata["projectControlCommandId"] = command.ID
		metadata["controllerType"] = command.ControllerType
		metadata["agentTaskId"] = command.AgentTaskID
		metadata["agentStepId"] = command.AgentStepID
	}
	newVersion, err := insertScriptVersionTx(
		r, tx, project, script.ID, nextVersion, content, current.ContentFormat,
		stringPtrFromValue("agent_rewrite"), rendered.PromptVersionID, rendered.RenderedHash,
		mustRawJSON(metadata), principal.UserID,
	)
	if err != nil {
		return scriptRewriteActionResult{}, err
	}
	if _, err := insertScriptEpisodesTx(r, tx, project, script.ID, newVersion.ID, principal.UserID, []scriptEpisodeDraft{
		defaultScriptEpisodeDraft(script.SourceID, "第 1 集", content, current.ContentFormat, rendered.PromptVersionID, rendered.RenderedHash, gatewayResp.ProviderCallID, mustRawJSON(metadata)),
	}); err != nil {
		return scriptRewriteActionResult{}, err
	}
	previousVersionID := ""
	if input.Activate {
		previousVersionID, err = activateScriptVersionTx(r, tx, project, script, newVersion)
		if err != nil {
			return scriptRewriteActionResult{}, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "script.version.activated", "script_version", newVersion.ID, mustRawJSON(map[string]any{
			"scriptId": script.ID, "versionId": newVersion.ID,
			"previousVersionId": nullableMetadataValue(previousVersionID),
			"source":            firstNonEmpty(string(command.ControllerType), "script_agent"),
			"agentRunId":        runID, "projectControlCommandId": nullableMetadataValue(command.ID),
		})); err != nil {
			return scriptRewriteActionResult{}, err
		}
	}
	output := scriptAgentRunOutput{
		ScriptID: script.ID, VersionID: newVersion.ID, Content: content, ContentFormat: current.ContentFormat,
		ProviderCallID: gatewayResp.ProviderCallID, ModelID: gatewayResp.ModelID,
		PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash,
	}
	if err := completeScriptAgentRunTx(ctx, tx, runID, output); err != nil {
		return scriptRewriteActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return scriptRewriteActionResult{}, err
	}
	return scriptRewriteActionResult{
		ScriptID: script.ID, VersionID: newVersion.ID, Content: content, ContentFormat: current.ContentFormat,
		AgentRunID: runID, ProviderCallID: gatewayResp.ProviderCallID, ModelID: gatewayResp.ModelID,
		PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash,
		Activated: input.Activate, PreviousVersionID: previousVersionID, CommandID: command.ID,
	}, nil
}

func (s *Server) resolveScriptRewriteTarget(r *http.Request, projectID string, input scriptRewriteActionInput) (Script, ScriptVersion, error) {
	script, err := s.script(r, projectID, input.ScriptID)
	if err != nil {
		return Script{}, ScriptVersion{}, err
	}
	versionID := input.VersionID
	if versionID == "" && script.CurrentVersionID != nil {
		versionID = *script.CurrentVersionID
	}
	if versionID == "" {
		return Script{}, ScriptVersion{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "剧本没有可改写的当前版本")
	}
	version, err := s.scriptVersion(r, projectID, script.ID, versionID)
	return script, version, err
}

func (s *Server) scriptRewritePromptOptions(
	ctx context.Context,
	project Project,
	command projectcontrol.Command,
	toolName, title, billingPermission string,
) scriptAgentPromptOptions {
	options := scriptAgentPromptOptions{
		AgentType: "script_agent", Stream: true, BillingPermission: billingPermission,
		ProjectControlCommandID: command.ID,
	}
	if command.ID != "" {
		options.AgentType = "project_agent"
		options.TaskID = command.AgentTaskID
		options.StepID = command.AgentStepID
		options.IdempotencyKey = "project-control-command:" + command.ID + ":" + toolName
	}
	if command.AgentTaskID != "" && command.AgentStepID != "" {
		options.OnProgress = s.agentStepStreamProgressCallback(
			ctx, project, AgentTask{ID: command.AgentTaskID}, AgentStep{ID: command.AgentStepID}, toolName, 1, 1, title,
		)
	}
	return options
}

func (s *Server) projectControlScriptAgentRun(ctx context.Context, projectID, commandID, taskType string) (scriptAgentRunRecord, bool, error) {
	if strings.TrimSpace(commandID) == "" {
		return scriptAgentRunRecord{}, false, nil
	}
	var record scriptAgentRunRecord
	var output []byte
	err := s.db.QueryRow(ctx, `
		SELECT id::text, status, output, COALESCE(provider_call_id::text, ''),
		       COALESCE(prompt_version_id::text, ''), COALESCE(prompt_hash, '')
		FROM agent_runs
		WHERE project_id = $1 AND project_control_command_id = $2 AND task_type = $3
	`, projectID, commandID, taskType).Scan(
		&record.ID, &record.Status, &output, &record.ProviderCallID, &record.PromptVersionID, &record.PromptHash,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return scriptAgentRunRecord{}, false, nil
		}
		return scriptAgentRunRecord{}, false, err
	}
	if len(output) > 0 {
		_ = json.Unmarshal(output, &record.Output)
	}
	if record.Output.ProviderCallID == "" {
		record.Output.ProviderCallID = record.ProviderCallID
	}
	if record.Output.PromptVersionID == "" {
		record.Output.PromptVersionID = record.PromptVersionID
	}
	if record.Output.PromptHash == "" {
		record.Output.PromptHash = record.PromptHash
	}
	return record, true, nil
}

func (s *Server) scriptRewritePreviewReplay(ctx context.Context, projectID, commandID string) (scriptRewritePreviewActionResult, bool, error) {
	record, ok, err := s.projectControlScriptAgentRun(ctx, projectID, commandID, "rewrite_preview")
	if err != nil || !ok || record.Status != "succeeded" || strings.TrimSpace(record.Output.Content) == "" {
		return scriptRewritePreviewActionResult{}, false, err
	}
	return scriptRewritePreviewActionResult{
		ScriptID: record.Output.ScriptID, VersionID: record.Output.VersionID,
		Content: record.Output.Content, ContentFormat: record.Output.ContentFormat,
		AgentRunID: record.ID, ProviderCallID: record.Output.ProviderCallID, ModelID: record.Output.ModelID,
		PromptVersionID: record.Output.PromptVersionID, PromptHash: record.Output.PromptHash,
		CommandID: commandID, IdempotentReplay: true,
	}, true, nil
}

func (s *Server) scriptRewriteVersionReplay(ctx context.Context, projectID, commandID string) (scriptRewriteActionResult, bool, error) {
	if strings.TrimSpace(commandID) == "" {
		return scriptRewriteActionResult{}, false, nil
	}
	return scriptRewriteVersionReplayRow(ctx, s.db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, script_id, version, content, content_format, COALESCE(status, 'active'),
		       source_type, prompt_version_id, prompt_hash, metadata, created_by, created_at
		FROM script_versions
		WHERE project_id = $1 AND metadata->>'projectControlCommandId' = $2
	`, projectID, commandID), commandID)
}

func scriptRewriteVersionReplayTx(ctx context.Context, tx pgx.Tx, projectID, commandID string) (scriptRewriteActionResult, bool, error) {
	if strings.TrimSpace(commandID) == "" {
		return scriptRewriteActionResult{}, false, nil
	}
	return scriptRewriteVersionReplayRow(ctx, tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, script_id, version, content, content_format, COALESCE(status, 'active'),
		       source_type, prompt_version_id, prompt_hash, metadata, created_by, created_at
		FROM script_versions
		WHERE project_id = $1 AND metadata->>'projectControlCommandId' = $2
	`, projectID, commandID), commandID)
}

func scriptRewriteVersionReplayRow(_ context.Context, row rowScan, commandID string) (scriptRewriteActionResult, bool, error) {
	version, err := scanScriptVersion(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return scriptRewriteActionResult{}, false, nil
		}
		return scriptRewriteActionResult{}, false, err
	}
	metadata := rawObject(version.Metadata)
	return scriptRewriteActionResult{
		ScriptID: version.ScriptID, VersionID: version.ID, Content: version.Content, ContentFormat: version.ContentFormat,
		AgentRunID:      stringValueFromAny(metadata["agentRunId"]),
		ProviderCallID:  stringValueFromAny(metadata["providerCallId"]),
		ModelID:         stringValueFromAny(metadata["modelId"]),
		PromptVersionID: stringValue(version.PromptVersionID), PromptHash: stringValue(version.PromptHash),
		Activated:         scriptRewriteBoolValue(metadata["activated"]),
		PreviousVersionID: stringValueFromAny(metadata["previousVersionId"]),
		CommandID:         commandID, IdempotentReplay: true,
	}, true, nil
}

func scriptRewriteBoolValue(value any) bool {
	valueBool, _ := value.(bool)
	return valueBool
}

func (s *Server) completeScriptAgentRun(ctx context.Context, runID string, output scriptAgentRunOutput) error {
	_, err := s.db.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = NULLIF($4, '')::uuid, prompt_hash = NULLIF($5, ''),
		    error_code = NULL, error_message = NULL, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(output), output.ProviderCallID, output.PromptVersionID, output.PromptHash)
	return err
}

func completeScriptAgentRunTx(ctx context.Context, tx pgx.Tx, runID string, output scriptAgentRunOutput) error {
	_, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = NULLIF($4, '')::uuid, prompt_hash = NULLIF($5, ''),
		    error_code = NULL, error_message = NULL, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(output), output.ProviderCallID, output.PromptVersionID, output.PromptHash)
	return err
}

func gatewayTextResponseFromScriptAgentRun(record scriptAgentRunRecord) provider.GatewayTextResponse {
	return provider.GatewayTextResponse{
		ProviderCallID: record.Output.ProviderCallID,
		ModelID:        record.Output.ModelID,
		Status:         "succeeded",
		Output:         provider.GatewayTextOutput{Text: record.Output.Content},
	}
}
