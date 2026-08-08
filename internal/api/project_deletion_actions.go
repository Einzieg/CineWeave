package api

import (
	"context"
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func (s *Server) executeProjectDeleteAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input createProjectDeletionRequestBody
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	request, replayed, err := s.createProjectDeletionRequestCore(
		ctx, principal, project, input, "project-control-command:"+command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := "已创建项目删除任务。"
	if replayed {
		summary = "项目删除任务已存在，未重复创建。"
	}
	return agentToolOK("project.delete", workflowActionArguments(raw), summary, map[string]any{
		"projectDeletionRequest": request,
		"requestId":              request.ID,
		"status":                 request.Status,
		"idempotent":             replayed,
	}), nil
}

type retryProjectDeletionActionInput struct {
	RequestID string `json:"requestId"`
}

func (s *Server) executeProjectDeleteRetryAsyncAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	_ projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input retryProjectDeletionActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	request, err := s.retryProjectDeletionRequestCore(ctx, project.ID, input.RequestID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("project.delete.retry", workflowActionArguments(raw), "已重新提交项目删除任务。", map[string]any{
		"projectDeletionRequest": request, "requestId": request.ID, "status": request.Status,
	}), nil
}
