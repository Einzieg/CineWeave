package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

func projectControlAgentDescriptors() ([]projectcontrol.Descriptor, map[string]agent.AgentTool, error) {
	descriptors := make([]projectcontrol.Descriptor, 0)
	tools := make(map[string]agent.AgentTool)
	for _, tool := range agent.ProjectControlTools() {
		descriptor := tool.Descriptor()
		if descriptor.ExportToMCP {
			inputSchema, err := projectControlExternalInputSchema(descriptor.InputSchema, !descriptor.ReadOnly)
			if err != nil {
				return nil, nil, fmt.Errorf("extend project control schema for %s: %w", descriptor.Name, err)
			}
			descriptor.InputSchema = inputSchema
		}
		// Bounded actions with a shared transaction implementation remain sync so
		// the command and domain mutation commit atomically. Other writes are
		// dispatched only through an explicitly registered shared async runtime.
		if !descriptor.ReadOnly && descriptor.ExecutionMode == projectcontrol.ExecutionModeSync && !projectControlHasSharedSyncAction(descriptor.Name) {
			descriptor.ExecutionMode = projectcontrol.ExecutionModeAsyncCommand
		}
		if err := descriptor.Validate(); err != nil {
			return nil, nil, err
		}
		descriptors = append(descriptors, descriptor)
		tools[descriptor.Name] = tool
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Name < descriptors[j].Name })
	return descriptors, tools, nil
}

func projectControlExternalInputSchema(raw json.RawMessage, requireIdempotency bool) (json.RawMessage, error) {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
		schema["properties"] = properties
	}
	properties["projectId"] = map[string]any{
		"type": "string", "format": "uuid", "description": "要操作的项目 ID。",
	}
	required := projectControlSchemaRequired(schema)
	required = appendUniqueControlSchemaField(required, "projectId")
	if requireIdempotency {
		properties["idempotencyKey"] = map[string]any{
			"type": "string", "minLength": 1, "maxLength": 200,
			"description": "调用方为本次业务意图生成的稳定幂等键。",
		}
		required = appendUniqueControlSchemaField(required, "idempotencyKey")
	}
	schema["required"] = required
	return json.Marshal(schema)
}

func projectControlSchemaRequired(schema map[string]any) []string {
	raw, _ := schema["required"].([]any)
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	// Schemas constructed in Go and decoded through interface values always use
	// []any, but retain this branch for hand-authored schemas used by tests.
	if typed, ok := schema["required"].([]string); ok {
		result = append(result[:0], typed...)
	}
	return result
}

func appendUniqueControlSchemaField(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (e *projectControlExecutor) executeAgentAction(
	ctx context.Context,
	identity controlmcp.Identity,
	descriptor projectcontrol.Descriptor,
	tool agent.AgentTool,
	raw json.RawMessage,
) (projectcontrol.Result, error) {
	projectID, idempotencyKey, actionInput, err := decodeProjectControlAgentInput(raw, !descriptor.ReadOnly)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if err := agent.ValidateToolInput(tool, actionInput); err != nil {
		return projectcontrol.Result{}, controlValidationError(err.Error())
	}
	project, principal, err := e.authorizedProjectID(ctx, identity.Principal, projectID, descriptorPermissions(descriptor)...)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	if !e.server.agentToolAllowedForProjectKind(string(project.ProjectKind), tool.Name) {
		return projectControlFailure("PROJECT_KIND_MISMATCH", "当前项目类型不支持此操作", false, map[string]any{
			"projectKind": project.ProjectKind, "action": tool.Name,
		}), nil
	}

	if descriptor.ReadOnly {
		result := e.server.executeProjectAction(
			requestWithContext(ctx), principal, project,
			AgentTask{OrganizationID: project.OrganizationID, ProjectID: project.ID, AgentType: "project_agent", Mode: string(agent.TaskModeAutoLowRisk), Constraints: json.RawMessage(`{}`)},
			AgentStep{ToolName: tool.Name, Risk: string(tool.Risk), Input: actionInput}, tool,
		)
		return projectControlResultFromAgentTool(result), nil
	}

	controlKeyID := ""
	if identity.ControllerType == projectcontrol.ControllerCodexMCP {
		controlKeyID = identity.Key.ID
	}
	if descriptor.ExecutionMode == projectcontrol.ExecutionModeSync {
		command, actionResult, replayed, err := e.executeBoundedSyncAction(
			ctx, principal, project, descriptor, identity.ControllerType, controlKeyID,
			"", "", actionInput, idempotencyKey,
		)
		if err != nil {
			return projectcontrol.Result{}, err
		}
		result := projectControlResultFromAgentTool(actionResult)
		result.CommandID = command.ID
		result.Status = string(command.Status)
		result.Data = mustProjectControlJSON(map[string]any{
			"result": actionResult, "command": command, "idempotentReplay": replayed,
		})
		return result, nil
	}
	command, replayed, err := e.repository.Create(ctx, projectcontrol.CreateCommand{
		OrganizationID: project.OrganizationID,
		WorkspaceID:    project.WorkspaceID,
		ProjectID:      project.ID,
		ActorUserID:    principal.UserID,
		ControllerType: identity.ControllerType,
		ControlKeyID:   controlKeyID,
		Descriptor:     descriptor,
		Input:          actionInput,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return projectcontrol.Result{}, err
	}
	snapshot, err := e.commandSnapshot(ctx, command)
	if err != nil {
		return projectcontrol.Result{}, err
	}
	result := projectControlSuccess("命令已创建，Worker 将在后台执行", map[string]any{
		"command": snapshot, "idempotentReplay": replayed,
	})
	result.CommandID = command.ID
	result.Status = string(command.Status)
	return result, nil
}

func decodeProjectControlAgentInput(raw json.RawMessage, requireIdempotency bool) (string, string, json.RawMessage, error) {
	var input map[string]any
	if err := decodeControlInput(raw, &input); err != nil {
		return "", "", nil, err
	}
	projectID, _ := input["projectId"].(string)
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", "", nil, controlValidationError("projectId 不能为空")
	}
	idempotencyKey, _ := input["idempotencyKey"].(string)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if requireIdempotency && idempotencyKey == "" {
		return "", "", nil, controlValidationError("idempotencyKey 不能为空")
	}
	delete(input, "projectId")
	delete(input, "idempotencyKey")
	actionInput, err := json.Marshal(input)
	if err != nil {
		return "", "", nil, err
	}
	return projectID, idempotencyKey, actionInput, nil
}

func projectControlResultFromAgentTool(result agentToolResult) projectcontrol.Result {
	if result.Status != "succeeded" {
		code := firstNonEmpty(result.ErrorCode, "ACTION_FAILED")
		message := firstNonEmpty(result.ErrorMessage, result.Summary, "操作失败")
		return projectControlFailure(code, message, result.Retryable, map[string]any{
			"action": result.Name, "result": result,
		})
	}
	response := projectControlSuccess(result.Summary, map[string]any{"result": result})
	response.WorkflowRunIDs = append([]string(nil), result.ChildWorkflowRunIDs...)
	return response
}

func (e *projectControlExecutor) workflowLinks(
	ctx context.Context,
	command projectcontrol.Command,
	projectID string,
	workflowRunIDs []string,
) ([]projectcontrol.WorkflowLink, error) {
	return e.workflowLinksWithConsistencyWait(
		ctx,
		command,
		projectID,
		workflowRunIDs,
		5*time.Second,
		100*time.Millisecond,
	)
}

func (e *projectControlExecutor) workflowLinksWithConsistencyWait(
	ctx context.Context,
	command projectcontrol.Command,
	projectID string,
	workflowRunIDs []string,
	maxWait time.Duration,
	pollInterval time.Duration,
) ([]projectcontrol.WorkflowLink, error) {
	if len(workflowRunIDs) == 0 {
		return nil, nil
	}
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	deadline := time.Now().Add(maxWait)
	expectedTemporalID, err := projectcontrol.TemporalWorkflowIdentity(command.ID, "", command.ActionVersion)
	if err != nil {
		return nil, err
	}
	links := make([]projectcontrol.WorkflowLink, 0, len(workflowRunIDs))
	seen := make(map[string]struct{}, len(workflowRunIDs))
	for _, workflowRunID := range workflowRunIDs {
		workflowRunID = strings.TrimSpace(workflowRunID)
		if workflowRunID == "" {
			continue
		}
		if _, exists := seen[workflowRunID]; exists {
			continue
		}
		seen[workflowRunID] = struct{}{}
		var temporalWorkflowID string
		for {
			err := e.server.db.QueryRow(ctx, `
				SELECT temporal_workflow_id
				FROM workflow_runs
				WHERE id = $1 AND project_id = $2
			`, workflowRunID, projectID).Scan(&temporalWorkflowID)
			if err == nil {
				break
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			remaining := time.Until(deadline)
			if remaining <= 0 || maxWait <= 0 {
				return nil, fmt.Errorf("workflow run %s is not owned by project", workflowRunID)
			}
			wait := min(pollInterval, remaining)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		relationType := projectcontrol.WorkflowRelationDomainIdempotentChild
		if temporalWorkflowID == expectedTemporalID {
			relationType = projectcontrol.WorkflowRelationDeterministicChild
		}
		links = append(links, projectcontrol.WorkflowLink{
			WorkflowRunID: workflowRunID, TemporalWorkflowID: temporalWorkflowID,
			RelationType: relationType,
		})
	}
	return links, nil
}
