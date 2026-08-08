package api

import (
	"context"
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type createStoryboardShotRenderPlanActionInput struct {
	ShotID  string                                `json:"shotId"`
	Request createStoryboardShotRenderPlanRequest `json:"request"`
}

func (s *Server) executeShotRenderPlanCreateAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	_ projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input createStoryboardShotRenderPlanActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.Request.ShotID = input.ShotID
	detail, err := s.createStoryboardShotRenderPlanCore(ctx, principal, project, input.Request)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("shot.render_plan.create", workflowActionArguments(raw), "已创建镜头视频执行计划。", map[string]any{
		"shotId": input.ShotID, "videoRenderPlan": detail,
	}), nil
}
