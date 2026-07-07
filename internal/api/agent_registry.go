package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
)

func (s *Server) projectAgentRegistry() (*agent.Registry, error) {
	tools := agent.DefaultTools()
	for i := range tools {
		tool := tools[i]
		tools[i].Execute = s.projectAgentExecuteFunc(tool)
	}
	return agent.NewRegistry(tools...)
}

func (s *Server) projectAgentExecuteFunc(tool agent.AgentTool) agent.ToolFunc {
	return func(ctx context.Context, toolCtx agent.ToolContext, rawInput json.RawMessage) (agent.ToolResult, error) {
		r := requestWithContext(ctx)
		project, err := s.project(r, toolCtx.ProjectID)
		if err != nil {
			return agent.ToolResult{}, err
		}
		task, err := s.agentTask(r, project.ID, toolCtx.TaskID)
		if err != nil {
			return agent.ToolResult{}, err
		}
		step, err := s.agentStep(r, task.ID, toolCtx.StepID)
		if err != nil {
			return agent.ToolResult{}, err
		}
		if len(rawInput) > 0 && strings.TrimSpace(string(rawInput)) != "" && strings.TrimSpace(string(rawInput)) != "null" {
			step.Input = append(json.RawMessage(nil), rawInput...)
		}
		principal := auth.Principal{
			UserID:         toolCtx.UserID,
			OrganizationID: toolCtx.OrganizationID,
		}
		result := s.executeProjectAgentTool(r, principal, project, task, step, tool)
		return agentToolResultToRegistry(result), nil
	}
}

func (s *Server) agentStep(r *http.Request, taskID, stepID string) (AgentStep, error) {
	return scanAgentStep(s.db.QueryRow(r.Context(), `
		SELECT id, task_id, step_index, tool_name, risk, permission, status, requires_approval,
		       input, dry_run_output, supervisor_decision, output, verifier_output,
		       error_code, error_message, created_at, updated_at, started_at, completed_at
		FROM agent_steps
		WHERE task_id = $1 AND id = $2
	`, taskID, stepID))
}

func agentToolContext(project Project, principal auth.Principal, task AgentTask, step AgentStep) agent.ToolContext {
	return agent.ToolContext{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		SessionID:      stringValue(task.SessionID),
		TaskID:         task.ID,
		StepID:         step.ID,
		UserID:         principal.UserID,
		IdempotencyKey: agentStepIdempotencyKey(task, step),
		Constraints:    rawObject(task.Constraints),
		Metadata: map[string]any{
			"agentTaskId":    task.ID,
			"agentStepId":    step.ID,
			"agentToolName":  step.ToolName,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		},
	}
}

func agentToolResultToRegistry(result agentToolResult) agent.ToolResult {
	return agent.ToolResult{
		Name:         result.Name,
		Label:        result.Label,
		Status:       result.Status,
		Summary:      result.Summary,
		Arguments:    result.Arguments,
		Data:         result.Data,
		Retryable:    result.Retryable,
		NextActions:  agentToolNextActionsToRegistry(result.NextActions),
		ErrorCode:    result.ErrorCode,
		ErrorMessage: result.ErrorMessage,
	}
}

func agentToolResultFromRegistry(result agent.ToolResult) agentToolResult {
	return agentToolResult{
		Name:         result.Name,
		Label:        result.Label,
		Status:       result.Status,
		Summary:      result.Summary,
		Arguments:    result.Arguments,
		Data:         result.Data,
		Retryable:    result.Retryable,
		NextActions:  agentToolNextActionsFromRegistry(result.NextActions),
		ErrorCode:    result.ErrorCode,
		ErrorMessage: result.ErrorMessage,
	}
}

func agentToolNextActionsToRegistry(items []agentToolNextAction) []agent.ToolNextAction {
	if len(items) == 0 {
		return nil
	}
	out := make([]agent.ToolNextAction, 0, len(items))
	for _, item := range items {
		out = append(out, agent.ToolNextAction{
			Label:     item.Label,
			Reason:    item.Reason,
			Tool:      item.Tool,
			Arguments: item.Arguments,
		})
	}
	return out
}

func agentToolNextActionsFromRegistry(items []agent.ToolNextAction) []agentToolNextAction {
	if len(items) == 0 {
		return nil
	}
	out := make([]agentToolNextAction, 0, len(items))
	for _, item := range items {
		out = append(out, agentToolNextAction{
			Label:     item.Label,
			Reason:    item.Reason,
			Tool:      item.Tool,
			Arguments: item.Arguments,
		})
	}
	return out
}
