package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

type shotProductionControlInput struct {
	ScriptSceneID   string         `json:"scriptSceneId,omitempty"`
	ScriptEpisodeID string         `json:"scriptEpisodeId,omitempty"`
	WorkflowRunID   string         `json:"workflowRunId,omitempty"`
	ShotIDs         []string       `json:"shotIds,omitempty"`
	MaxConcurrency  int            `json:"maxConcurrency,omitempty"`
	Options         map[string]any `json:"options,omitempty"`
}

func (s *Server) executeShotGenerateImagePromptsAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	return s.executeShotProductionAsyncAction(ctx, principal, project, command, raw, "generate_image_prompts")
}

func (s *Server) executeShotGenerateVideoPromptsAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	return s.executeShotProductionAsyncAction(ctx, principal, project, command, raw, "generate_video_prompts")
}

func (s *Server) executeShotGenerateMissingImagesAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	return s.executeShotProductionAsyncAction(ctx, principal, project, command, raw, "generate_missing_images")
}

func (s *Server) executeShotGenerateMissingVideosAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	return s.executeShotProductionAsyncAction(ctx, principal, project, command, raw, "generate_missing_videos")
}

func (s *Server) executeShotCancelRunningVideosAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	return s.executeShotProductionAsyncAction(ctx, principal, project, command, raw, "cancel_running_videos")
}

func (s *Server) executeShotProductionAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
	baseAction string,
) (agentToolResult, error) {
	var input shotProductionControlInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	options := cleanAgentReferenceOptions(input.Options)
	if input.MaxConcurrency > 0 {
		options["maxConcurrency"] = input.MaxConcurrency
	}
	action := baseAction
	if len(uniqueNonEmptyStrings(input.ShotIDs)) > 0 {
		switch baseAction {
		case "generate_image_prompts":
			action = "generate_selected_image_prompts"
		case "generate_video_prompts":
			action = "generate_selected_video_prompts"
		case "generate_missing_images":
			action = "generate_selected_images"
		case "generate_missing_videos":
			action = "generate_selected_videos"
		}
	}
	response, replayed, err := s.runShotProductionActionCore(ctx, principal, project, ShotProductionActionRequest{
		Action: action, ScriptSceneID: input.ScriptSceneID, ScriptEpisodeID: input.ScriptEpisodeID,
		WorkflowRunID: input.WorkflowRunID, ShotIDs: input.ShotIDs, Options: options,
	}, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := fmt.Sprintf("已启动 %s，目标镜头 %d 个。", response.WorkflowType, len(response.TargetShotIDs))
	if replayed {
		summary = fmt.Sprintf("已存在 %s 工作流 %s，未重复启动。", response.WorkflowType, response.WorkflowRunID)
	}
	result := agentToolOK(command.ActionName, workflowActionArguments(raw), summary, map[string]any{
		"action": response.Action, "workflowRunId": response.WorkflowRunID,
		"workflowType": response.WorkflowType, "status": response.Status,
		"targetShotIds": response.TargetShotIDs, "idempotent": replayed,
	})
	result.ChildWorkflowRunIDs = []string{response.WorkflowRunID}
	return result, nil
}
