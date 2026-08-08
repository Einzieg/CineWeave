package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type workflowStartActionInput struct {
	WorkflowType string         `json:"workflowType"`
	Input        map[string]any `json:"input"`
}

type timelineComposeActionInput struct {
	TimelineID  string `json:"timelineId"`
	Title       string `json:"title"`
	Resolution  string `json:"resolution"`
	AspectRatio string `json:"aspectRatio"`
}

type workflowCancelActionInput struct {
	WorkflowRunID string `json:"workflowRunId"`
	Reason        string `json:"reason"`
}

func (s *Server) executeWorkflowStartAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input workflowStartActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.WorkflowType = strings.TrimSpace(input.WorkflowType)
	input.Input = cleanAgentReferenceOptions(input.Input)
	if input.WorkflowType == "" {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowType is required")
	}
	if input.WorkflowType == derivedAssetBatchWorkflowType {
		return s.executeDerivedAssetWorkflowStart(ctx, principal, project, command, raw, input.Input)
	}
	existing, found, err := s.workflowRunForProjectControlCommand(ctx, project.ID, input.WorkflowType, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	if found {
		return workflowStartedAgentResult("workflow.start", workflowActionArguments(raw), existing, input.Input, true), nil
	}
	spec, err := s.agentWorkflowStartSpec(requestWithContext(ctx), project, input.WorkflowType, input.Input)
	if err != nil {
		return agentToolResult{}, err
	}
	specInput := cloneMap(spec.Input)
	specInput["projectControlCommandId"] = command.ID
	specInput["idempotencyKey"] = "project-control-command:" + command.ID
	run, err := s.startProjectWorkflowCore(ctx, principal, project, spec.WorkflowType, specInput, spec.WorkflowFunc)
	if err != nil {
		return agentToolResult{}, err
	}
	result := workflowStartedAgentResult("workflow.start", workflowActionArguments(raw), run, specInput, false)
	if spec.Note != "" {
		result.Data["note"] = spec.Note
	}
	return result, nil
}

func (s *Server) executeDerivedAssetWorkflowStart(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
	input map[string]any,
) (agentToolResult, error) {
	requirementIDs := agentReferenceStringSliceArg(input, "requirementIds")
	mode := derivedAssetBatchModeSelectAll
	if len(requirementIDs) > 0 {
		mode = derivedAssetBatchModeExplicit
	}
	result, err := s.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode: mode, RequirementIDs: requirementIDs,
		Filters: DerivedAssetBatchFilters{
			ScriptEpisodeID: agentReferenceStringArg(input, "scriptEpisodeId"),
			ShotIDs:         agentReferenceStringSliceArg(input, "shotIds"),
		},
		MaxConcurrency:          agentIntArg(input, "maxConcurrency", workflows.DefaultDerivedAssetImageConcurrency, 1, workflows.MaxDerivedAssetImageConcurrency),
		Force:                   boolValue(input["force"]),
		ExpectedProjectRevision: project.Revision,
		IdempotencyKey:          "project-control-command:" + command.ID,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	response := workflowStartedAgentResult("workflow.start", workflowActionArguments(raw), result.WorkflowRun, input, false)
	response.Data["operationId"] = result.OperationID
	response.Data["derivedAssets"] = result.Batch
	return response, nil
}

func (s *Server) executeTimelineComposeAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input timelineComposeActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	run, timeline, replayed, err := s.composeTimelineAction(ctx, principal, project, input, command.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	result := workflowStartedAgentResult("timeline.compose", workflowActionArguments(raw), run, map[string]any{"timelineId": timeline.ID}, replayed)
	result.Data["timelineId"] = timeline.ID
	return result, nil
}

func (s *Server) executeWorkflowCancelAsyncAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	_ projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input workflowCancelActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.WorkflowRunID = strings.TrimSpace(input.WorkflowRunID)
	if input.WorkflowRunID == "" {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowRunId is required")
	}
	item, err := scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE id = $1 AND project_id = $2
	`), input.WorkflowRunID, project.ID))
	if err != nil {
		return agentToolResult{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "项目控制请求取消工作流"
	}
	updated, err := s.cancelWorkflowRunItem(ctx, item, reason)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := fmt.Sprintf("已请求取消工作流 %s。", updated.ID)
	if isTerminalWorkflowStatus(item.Status) {
		summary = fmt.Sprintf("工作流 %s 已是终态 %s。", updated.ID, updated.Status)
	}
	return agentToolOK("workflow.cancel", workflowActionArguments(raw), summary, map[string]any{
		"workflowRun": updated,
	}), nil
}

func (s *Server) composeTimelineAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	input timelineComposeActionInput,
	projectControlCommandID string,
) (WorkflowRun, ProjectTimeline, bool, error) {
	input.TimelineID = strings.TrimSpace(input.TimelineID)
	if input.TimelineID == "" {
		return WorkflowRun{}, ProjectTimeline{}, false, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "timelineId is required")
	}
	timeline, err := s.timelineByID(requestWithContext(ctx), project.ID, input.TimelineID)
	if err != nil {
		return WorkflowRun{}, ProjectTimeline{}, false, err
	}
	ready, err := s.projectShotVideosReadyContext(ctx, project.ID)
	if err != nil {
		return WorkflowRun{}, ProjectTimeline{}, false, err
	}
	if !ready {
		return WorkflowRun{}, ProjectTimeline{}, false, newAPIError(http.StatusUnprocessableEntity, "SHOT_VIDEOS_REQUIRED", "all storyboard shots must have completed video before composing final video")
	}
	commandID := strings.TrimSpace(projectControlCommandID)
	if commandID != "" {
		existing, found, err := s.workflowRunForProjectControlCommand(ctx, project.ID, "compose_timeline", commandID)
		if err != nil {
			return WorkflowRun{}, ProjectTimeline{}, false, err
		}
		if found {
			return existing, timeline, true, nil
		}
	}
	workflowInput := map[string]any{
		"timelineId": timeline.ID, "title": defaultAPIString(input.Title, timeline.Title),
		"resolution":  defaultAPIString(input.Resolution, timeline.Resolution, "720p"),
		"aspectRatio": defaultAPIString(input.AspectRatio, timeline.AspectRatio, "16:9"),
	}
	if commandID != "" {
		workflowInput["projectControlCommandId"] = commandID
		workflowInput["idempotencyKey"] = "project-control-command:" + commandID
	}
	run, err := s.startProjectWorkflowCoreWithHook(
		ctx, principal, project, "compose_timeline", workflowInput, workflows.ComposeTimelineWorkflow,
		func(ctx context.Context, tx pgx.Tx, run WorkflowRun) error {
			_, err := tx.Exec(ctx, `
				UPDATE project_timelines
				SET workflow_run_id = $2, status = 'active', edited_by = $3, edited_at = now()
				WHERE id = $1 AND project_id = $4
			`, timeline.ID, run.ID, principal.UserID, project.ID)
			return err
		},
	)
	if err != nil {
		return WorkflowRun{}, ProjectTimeline{}, false, err
	}
	return run, timeline, false, nil
}

func (s *Server) workflowRunForProjectControlCommand(ctx context.Context, projectID, workflowType, commandID string) (WorkflowRun, bool, error) {
	if strings.TrimSpace(commandID) == "" {
		return WorkflowRun{}, false, nil
	}
	run, err := scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE project_id = $1
		  AND workflow_type = $2
		  AND input->'input'->>'projectControlCommandId' = $3
		ORDER BY created_at DESC
		LIMIT 1
	`), projectID, workflowType, commandID))
	if err == nil {
		return run, true, nil
	}
	if err == pgx.ErrNoRows {
		return WorkflowRun{}, false, nil
	}
	return WorkflowRun{}, false, err
}

func (s *Server) projectShotVideosReadyContext(ctx context.Context, projectID string) (bool, error) {
	var missing int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM storyboard_shots
		WHERE project_id = $1
		  AND deleted_at IS NULL
		  AND NOT (
		    (COALESCE(video_status, '') = 'succeeded' OR COALESCE(status, '') = 'video_succeeded')
		    AND (video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '')
		  )
	`, projectID).Scan(&missing)
	return missing == 0, err
}

func workflowStartedAgentResult(actionName string, arguments map[string]any, run WorkflowRun, input map[string]any, idempotent bool) agentToolResult {
	summary := fmt.Sprintf("已启动 %s，工作流 %s 当前状态 %s。", run.WorkflowType, run.ID, run.Status)
	if idempotent {
		summary = fmt.Sprintf("已存在 %s 工作流 %s，未重复启动。", run.WorkflowType, run.ID)
	}
	return agentToolOK(actionName, arguments, summary, map[string]any{
		"workflowRunId": run.ID, "workflowType": run.WorkflowType, "status": run.Status,
		"input": input, "idempotent": idempotent,
	})
}

func decodeWorkflowActionInput(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflow action input is invalid")
	}
	return nil
}

func workflowActionArguments(raw json.RawMessage) map[string]any {
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil || arguments == nil {
		return map[string]any{}
	}
	return arguments
}
