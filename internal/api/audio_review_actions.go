package api

import (
	"context"
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func (s *Server) executeShotRenderPlanReviewAudioAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input nativeAudioReviewRequest
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	run, replayed, err := s.startNativeAudioReviewCore(ctx, principal, project, input, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	return workflowStartedAgentResult("shot.render_plan.review_audio", workflowActionArguments(raw), run, map[string]any{
		"shotId": input.ShotID, "videoRenderPlanId": input.VideoRenderPlanID,
	}, replayed), nil
}
