package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	storyboardpkg "github.com/Einzieg/cineweave/internal/storyboard"
	"github.com/jackc/pgx/v5"
)

type storyboardActivatePlanActionInput struct {
	PlanID string `json:"planId"`
}

type storyboardSplitShotActionInput struct {
	ShotID     string `json:"shotId"`
	SplitTick  *int64 `json:"splitTick,omitempty"`
	SplitFrame *int64 `json:"splitFrame,omitempty"`
	RightTitle string `json:"rightTitle,omitempty"`
}

type storyboardMergeShotsActionInput struct {
	ShotIDs []string       `json:"shotIds"`
	Patch   map[string]any `json:"patch,omitempty"`
	Title   string         `json:"title,omitempty"`
	Visual  string         `json:"visual,omitempty"`
	Camera  string         `json:"camera,omitempty"`
	Motion  string         `json:"motion,omitempty"`
	Mood    string         `json:"mood,omitempty"`
}

func (s *Server) activateStoryboardPlanActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorID string,
	planID string,
) (storyboardpkg.ActivateStoryboardPlanResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return storyboardpkg.ActivateStoryboardPlanResult{}, controlValidationError("planId 不能为空")
	}
	var episodeID string
	if err := tx.QueryRow(ctx, `
		SELECT script_episode_id::text
		FROM storyboard_plans
		WHERE project_id = $1 AND id = $2
	`, project.ID, planID).Scan(&episodeID); err != nil {
		return storyboardpkg.ActivateStoryboardPlanResult{}, err
	}
	result, err := storyboardpkg.ActivateStoryboardPlanTx(ctx, tx, storyboardpkg.ActivateStoryboardPlanRequest{
		ProjectID: project.ID, ScriptEpisodeID: episodeID, StoryboardPlanID: planID, ActorID: actorID,
	})
	if err != nil {
		return storyboardpkg.ActivateStoryboardPlanResult{}, newAPIError(http.StatusUnprocessableEntity, "STORYBOARD_PLAN_INVALID", err.Error())
	}
	return result, nil
}

func (s *Server) splitStoryboardShotActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorID string,
	input storyboardSplitShotActionInput,
) (storyboardpkg.StoryboardPlanEditResult, error) {
	input.ShotID = strings.TrimSpace(input.ShotID)
	if input.ShotID == "" || (input.SplitTick == nil) == (input.SplitFrame == nil) {
		return storyboardpkg.StoryboardPlanEditResult{}, controlValidationError("shotId 且 splitTick/splitFrame 中恰好一项不能为空")
	}
	splitTick := int64(0)
	if input.SplitTick != nil {
		splitTick = *input.SplitTick
	} else {
		_, frameTick, err := s.storyboardShotStartAndFrameTickWithDB(ctx, tx, project.ID, input.ShotID)
		if err != nil {
			return storyboardpkg.StoryboardPlanEditResult{}, err
		}
		splitTick = *input.SplitFrame * frameTick
	}
	result, err := storyboardpkg.SplitStoryboardShotTx(ctx, tx, storyboardpkg.SplitStoryboardShotRequest{
		ProjectID: project.ID, ShotID: input.ShotID, SplitTick: splitTick,
		ActorID: actorID, RightTitle: input.RightTitle,
	})
	if err != nil {
		return storyboardpkg.StoryboardPlanEditResult{}, newAPIError(http.StatusUnprocessableEntity, "STORYBOARD_EDIT_INVALID", err.Error())
	}
	return result, nil
}

func (s *Server) mergeStoryboardShotsActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorID string,
	input storyboardMergeShotsActionInput,
) (storyboardpkg.StoryboardPlanEditResult, error) {
	input.ShotIDs = uniqueNonEmptyStrings(input.ShotIDs)
	if len(input.ShotIDs) < 2 {
		return storyboardpkg.StoryboardPlanEditResult{}, controlValidationError("shotIds 至少需要两个镜头")
	}
	if input.Patch != nil {
		input.Title = firstNonEmpty(strings.TrimSpace(input.Title), agentStringArg(input.Patch, "title"))
		input.Visual = firstNonEmpty(strings.TrimSpace(input.Visual), agentStringArg(input.Patch, "visual"))
		input.Camera = firstNonEmpty(strings.TrimSpace(input.Camera), agentStringArg(input.Patch, "camera"))
		input.Motion = firstNonEmpty(strings.TrimSpace(input.Motion), agentStringArg(input.Patch, "motion"))
		input.Mood = firstNonEmpty(strings.TrimSpace(input.Mood), agentStringArg(input.Patch, "mood"))
	}
	result, err := storyboardpkg.MergeStoryboardShotsTx(ctx, tx, storyboardpkg.MergeStoryboardShotsRequest{
		ProjectID: project.ID, ShotIDs: input.ShotIDs, ActorID: actorID,
		Title: input.Title, Visual: input.Visual, Camera: input.Camera, Motion: input.Motion, Mood: input.Mood,
	})
	if err != nil {
		return storyboardpkg.StoryboardPlanEditResult{}, newAPIError(http.StatusUnprocessableEntity, "STORYBOARD_EDIT_INVALID", err.Error())
	}
	return result, nil
}

func (s *Server) executeStoryboardActivatePlanSyncAction(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardActivatePlanActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.activateStoryboardPlanActionTx(ctx, tx, project, principal.UserID, input.PlanID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已激活分镜方案。", map[string]any{"activation": result}), nil
}

func (s *Server) executeStoryboardSplitShotSyncAction(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardSplitShotActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.splitStoryboardShotActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), "已拆分分镜镜头并创建新方案版本。", map[string]any{"edit": result}), nil
}

func (s *Server) executeStoryboardMergeShotsSyncAction(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardMergeShotsActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.mergeStoryboardShotsActionTx(ctx, tx, project, principal.UserID, input)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK(command.ActionName, workflowActionArguments(raw), fmt.Sprintf("已合并 %d 个分镜镜头并创建新方案版本。", len(input.ShotIDs)), map[string]any{"edit": result}), nil
}
