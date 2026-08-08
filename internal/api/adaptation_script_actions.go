package api

import (
	"context"
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type adaptationGenerateScriptActionInput struct {
	PlanID      string `json:"planId"`
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
}

func (s *Server) executeAdaptationGenerateScriptAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input adaptationGenerateScriptActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, replayed, err := s.generateScriptFromAdaptationPlanCore(ctx, principal, project, input.PlanID, generateScriptFromAdaptationPlanRequest{
		Title: input.Title, Instruction: input.Instruction,
	}, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("adaptation.generate_script", workflowActionArguments(raw), "已从改编计划生成分集剧本。", map[string]any{
		"scriptId": result.ScriptID, "versionId": result.VersionID,
		"adaptationPlanId": result.AdaptationPlanID,
		"providerCallId":   result.ProviderCallID, "providerCallIds": result.ProviderCallIDs,
		"modelId": result.ModelID, "modelIds": result.ModelIDs,
		"episodeCount": result.EpisodeCount, "content": result.Content,
		"idempotentReplay": replayed,
	}), nil
}
