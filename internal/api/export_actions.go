package api

import (
	"context"
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func (s *Server) executeExportCreateAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input createProjectExportRequest
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	response, run, replayed, err := s.createProjectExportCore(ctx, principal, project, input, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	result := workflowStartedAgentResult("export.create", workflowActionArguments(raw), run, map[string]any{
		"exportId": response.ExportID,
	}, replayed)
	result.Data["exportId"] = response.ExportID
	result.Data["status"] = response.Status
	return result, nil
}
