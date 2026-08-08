package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type storyboardShotIDActionInput struct {
	ShotID string `json:"shotId"`
}

type storyboardGenerateAnchorActionInput struct {
	ShotID     string `json:"shotId"`
	AnchorRole string `json:"anchorRole,omitempty"`
}

type storyboardWorkflowActionResult struct {
	Run        WorkflowRun
	Input      map[string]any
	AnchorID   string
	Idempotent bool
}

func (s *Server) replanStoryboardShotStateCore(ctx context.Context, principal auth.Principal, project Project, shotID, commandID string) (storyboardWorkflowActionResult, error) {
	shotID = strings.TrimSpace(shotID)
	if shotID == "" {
		return storyboardWorkflowActionResult{}, controlValidationError("shotId 不能为空")
	}
	shot, err := s.storyboardShotByIDContext(ctx, project.ID, shotID)
	if err != nil {
		return storyboardWorkflowActionResult{}, err
	}
	commandID = strings.TrimSpace(commandID)
	if commandID != "" {
		existing, found, err := s.workflowRunForProjectControlCommand(ctx, project.ID, "script_to_storyboard", commandID)
		if err != nil {
			return storyboardWorkflowActionResult{}, err
		}
		if found {
			return storyboardWorkflowActionResult{Run: existing, Input: map[string]any{}, Idempotent: true}, nil
		}
	}
	var scriptID, episodeID string
	if err := s.db.QueryRow(ctx, `
		SELECT script.id::text, episode.id::text
		FROM storyboard_shots shot
		JOIN storyboard_plans plan ON plan.id = shot.storyboard_plan_id
		JOIN script_episodes episode ON episode.id = plan.script_episode_id
		JOIN script_versions version ON version.id = episode.script_version_id
		JOIN scripts script ON script.id = version.script_id
		WHERE shot.project_id = $1 AND shot.id = $2 AND shot.deleted_at IS NULL
	`, project.ID, shot.ID).Scan(&scriptID, &episodeID); err != nil {
		return storyboardWorkflowActionResult{}, err
	}
	input := map[string]any{
		"scriptId":              scriptID,
		"scriptEpisodeIds":      []string{episodeID},
		"pacingProfile":         "standard",
		"audioStrategy":         project.AudioStrategy,
		"audioRequirement":      project.AudioRequirement,
		"plannerBatchMaxShots":  12,
		"maxSceneConcurrency":   3,
		"force":                 true,
		"generateDerivedAssets": false,
	}
	if commandID != "" {
		input["projectControlCommandId"] = commandID
		input["idempotencyKey"] = "project-control-command:" + commandID
	}
	run, err := s.startProjectWorkflowCore(ctx, principal, project, "script_to_storyboard", input, workflows.ScriptToStoryboardWorkflow)
	if err != nil {
		return storyboardWorkflowActionResult{}, err
	}
	return storyboardWorkflowActionResult{Run: run, Input: input}, nil
}

func (s *Server) generateStoryboardShotAnchorCore(ctx context.Context, principal auth.Principal, project Project, input storyboardGenerateAnchorActionInput, commandID string) (storyboardWorkflowActionResult, error) {
	input.ShotID = strings.TrimSpace(input.ShotID)
	if input.ShotID == "" {
		return storyboardWorkflowActionResult{}, controlValidationError("shotId 不能为空")
	}
	shot, err := s.storyboardShotByIDContext(ctx, project.ID, input.ShotID)
	if err != nil {
		return storyboardWorkflowActionResult{}, err
	}
	profileKey := videoproduction.ProfileSingleFrameI2V
	if project.VideoProductionBinding != nil && strings.TrimSpace(project.VideoProductionBinding.ProfileKey) != "" {
		profileKey = project.VideoProductionBinding.ProfileKey
	}
	anchorRole := strings.TrimSpace(input.AnchorRole)
	if anchorRole == "" {
		anchorRole = videoproduction.AnchorRolePlannedFirstFrame
	}
	strategy, err := videoproduction.ProfileStrategyFor(profileKey)
	if err != nil {
		return storyboardWorkflowActionResult{}, err
	}
	anchorAllowed := false
	for _, requirement := range strategy.Anchors().Requirements() {
		if requirement.Role == anchorRole {
			anchorAllowed = true
			break
		}
	}
	if !anchorAllowed {
		return storyboardWorkflowActionResult{}, videoproduction.NewError(videoproduction.CodeProfileIncompatible, "当前视频生产方案不支持该视觉锚点", false)
	}
	commandID = strings.TrimSpace(commandID)
	if commandID != "" {
		existing, found, err := s.workflowRunForProjectControlCommand(ctx, project.ID, "regenerate_shot_image", commandID)
		if err != nil {
			return storyboardWorkflowActionResult{}, err
		}
		if found {
			var anchorID string
			if err := s.db.QueryRow(ctx, `
				SELECT id::text FROM shot_visual_anchors
				WHERE project_id = $1 AND storyboard_shot_id = $2
				  AND metadata->>'workflowRunId' = $3
				ORDER BY revision DESC LIMIT 1
			`, project.ID, shot.ID, existing.ID).Scan(&anchorID); err != nil {
				return storyboardWorkflowActionResult{}, err
			}
			return storyboardWorkflowActionResult{Run: existing, Input: map[string]any{}, AnchorID: anchorID, Idempotent: true}, nil
		}
	}
	workflowInput := map[string]any{
		"targetId":    shot.ID,
		"anchorRole":  anchorRole,
		"force":       true,
		"aspectRatio": firstNonEmpty(project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
	}
	if commandID != "" {
		workflowInput["projectControlCommandId"] = commandID
		workflowInput["idempotencyKey"] = "project-control-command:" + commandID
	}
	anchorID := ""
	run, err := s.startProjectWorkflowCoreWithHook(
		ctx, principal, project, "regenerate_shot_image", workflowInput, workflows.RegenerateShotImageWorkflow,
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			var hookErr error
			anchorID, hookErr = s.markShotAnchorGeneratingTx(ctx, tx, project, shot.ID, anchorRole, profileKey, run.ID, principal.UserID)
			return hookErr
		},
	)
	if err != nil {
		return storyboardWorkflowActionResult{}, err
	}
	return storyboardWorkflowActionResult{Run: run, Input: workflowInput, AnchorID: anchorID}, nil
}

func (s *Server) executeStoryboardReplanShotStateAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardShotIDActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.replanStoryboardShotStateCore(ctx, principal, project, input.ShotID, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	return workflowStartedAgentResult(command.ActionName, workflowActionArguments(raw), result.Run, result.Input, result.Idempotent), nil
}

func (s *Server) executeStoryboardGenerateAnchorAsyncAction(ctx context.Context, principal auth.Principal, project Project, command projectcontrol.Command, raw json.RawMessage) (agentToolResult, error) {
	var input storyboardGenerateAnchorActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.generateStoryboardShotAnchorCore(ctx, principal, project, input, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	response := workflowStartedAgentResult(command.ActionName, workflowActionArguments(raw), result.Run, result.Input, result.Idempotent)
	response.Data["anchorId"] = result.AnchorID
	response.Data["anchorRole"] = firstNonEmpty(input.AnchorRole, videoproduction.AnchorRolePlannedFirstFrame)
	return response, nil
}
