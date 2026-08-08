package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	reviewpkg "github.com/Einzieg/cineweave/internal/review"
)

type storyboardShotUpdateCommandInput struct {
	ShotID string         `json:"shotId"`
	Patch  map[string]any `json:"patch"`
}

func (s *Server) executeStoryboardUpdateShotAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input storyboardShotUpdateCommandInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.ShotID = strings.TrimSpace(input.ShotID)
	if input.ShotID == "" || len(input.Patch) == 0 {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotId 和 patch 不能为空")
	}
	if err := reviewpkg.ValidateReviewPatch("storyboard_shot", input.Patch); err != nil {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
	}
	patch, err := json.Marshal(input.Patch)
	if err != nil {
		return agentToolResult{}, err
	}
	var update updateStoryboardShotActionInput
	if err := json.Unmarshal(patch, &update); err != nil {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "分镜修改字段格式无效")
	}
	update.ShotID = input.ShotID
	update.Provenance = mustRawJSON(map[string]any{
		"projectControlCommandId": command.ID,
		"controllerType":          command.ControllerType,
		"agentTaskId":             command.AgentTaskID,
		"agentStepId":             command.AgentStepID,
	})
	item, err := s.updateStoryboardShotAction(ctx, project, principal.UserID, update)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("storyboard.update_shot", workflowActionArguments(raw), "已更新分镜镜头，并统一处理提示词、引用包和下游失效状态。", map[string]any{
		"shotId":                  item.ID,
		"shot":                    item,
		"projectControlCommandId": command.ID,
		"agentTaskId":             command.AgentTaskID,
		"agentStepId":             command.AgentStepID,
	}), nil
}
