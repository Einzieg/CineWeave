package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	reviewpkg "github.com/Einzieg/cineweave/internal/review"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/jackc/pgx/v5"
)

func (s *Server) executeAgentTaskReadySteps(r *http.Request, principal auth.Principal, project Project, taskID string) (AgentTask, error) {
	task, err := s.agentTask(r, project.ID, taskID)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Mode == string(agent.TaskModePlanOnly) || isTerminalAgentTaskStatus(task.Status) {
		return s.agentTaskWithDetails(r, project.ID, task.ID)
	}
	registry, err := s.projectAgentRegistry()
	if err != nil {
		return AgentTask{}, err
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE agent_tasks
		SET status = CASE WHEN status IN ('queued', 'planning') THEN 'running' ELSE status END,
		    started_at = COALESCE(started_at, now())
		WHERE id = $1 AND project_id = $2
	`, task.ID, project.ID); err != nil {
		return AgentTask{}, err
	}

	for {
		steps, err := s.listAgentTaskSteps(r, task.ID)
		if err != nil {
			return AgentTask{}, err
		}
		next, state := nextExecutableAgentStep(steps)
		switch state {
		case "waiting_approval":
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "waiting_approval", "", "")
		case "blocked":
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "blocked", "AGENT_STEP_BLOCKED", "agent step is blocked")
		case "failed":
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "failed", "AGENT_STEP_FAILED", "agent step failed")
		case "done":
			pendingRuns, err := s.agentTaskPendingWorkflowRuns(r.Context(), project.ID, task.ID)
			if err != nil {
				return AgentTask{}, err
			}
			if len(pendingRuns) > 0 {
				return s.finishAgentTaskWaitingForWorkflows(r.Context(), project, task.ID, pendingRuns)
			}
			appended, stopped, err := s.appendAgentAutoContinuationPlan(r, principal, project, task.ID)
			if err != nil {
				return AgentTask{}, err
			}
			if stopped != nil {
				return *stopped, nil
			}
			if appended {
				continue
			}
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "succeeded", "", "")
		}
		if next == nil {
			return s.agentTaskWithDetails(r, project.ID, task.ID)
		}
		pendingRuns, err := s.agentTaskPendingWorkflowRuns(r.Context(), project.ID, task.ID)
		if err != nil {
			return AgentTask{}, err
		}
		if len(pendingRuns) > 0 {
			return s.finishAgentTaskWaitingForWorkflows(r.Context(), project, task.ID, pendingRuns)
		}
		result, execErr := s.executeAgentStep(r, principal, project, task, *next, registry)
		if execErr != nil {
			return AgentTask{}, execErr
		}
		if result.Status == "failed" {
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "failed", firstNonEmpty(result.ErrorCode, "AGENT_STEP_FAILED"), result.ErrorMessage)
		}
		pendingRuns, err = s.agentTaskPendingWorkflowRuns(r.Context(), project.ID, task.ID)
		if err != nil {
			return AgentTask{}, err
		}
		if len(pendingRuns) > 0 {
			return s.finishAgentTaskWaitingForWorkflows(r.Context(), project, task.ID, pendingRuns)
		}
	}
}

func nextExecutableAgentStep(steps []AgentStep) (*AgentStep, string) {
	for i := range steps {
		switch steps[i].Status {
		case "planned", "approved":
			return &steps[i], "execute"
		case "waiting_approval":
			return nil, "waiting_approval"
		case "blocked":
			return nil, "blocked"
		case "failed":
			return nil, "failed"
		case "running":
			return &steps[i], "execute"
		}
	}
	return nil, "done"
}

func (s *Server) executeAgentStep(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, registry *agent.Registry) (agentToolResult, error) {
	tool, ok := registry.Get(step.ToolName)
	if !ok {
		result := agentToolResult{
			Name:         step.ToolName,
			Label:        step.ToolName,
			Status:       "failed",
			Summary:      "工具不在后端白名单中，未执行。",
			ErrorCode:    "UNKNOWN_TOOL",
			ErrorMessage: "unknown tool",
		}
		return result, s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result)
	}
	if strings.TrimSpace(tool.Permission) != "" {
		resource := agentToolPermissionResource(project, tool.Permission)
		if err := s.authorizer.Authorize(r.Context(), principal, tool.Permission, resource); err != nil {
			result := agentToolError(step.ToolName, map[string]any{}, err)
			result.Label = tool.Label
			return result, s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result)
		}
	}
	stateGate := s.superviseAgentStepState(r, project, task, step.ToolName, step.Input)
	if !stateGate.Allowed {
		message := firstNonEmpty(stateGate.Message, stateGate.Reason, "agent step is blocked")
		result := agentToolResult{
			Name:         step.ToolName,
			Label:        tool.Label,
			Status:       "blocked",
			Summary:      message,
			ErrorCode:    "AGENT_STEP_BLOCKED",
			ErrorMessage: message,
			Retryable:    agentStateGateRetryable(stateGate),
			NextActions:  agentStateGateNextActions(stateGate),
			Data:         map[string]any{"stateGate": stateGate},
		}
		return result, s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result)
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE agent_steps
		SET status = 'running', started_at = COALESCE(started_at, now()), error_code = NULL, error_message = NULL
		WHERE id = $1 AND task_id = $2 AND status IN ('planned', 'approved', 'running')
	`, step.ID, task.ID); err != nil {
		return agentToolResult{}, err
	}
	s.insertAgentStepEvent(r.Context(), project, task.ID, step.ID, "agent.step.started", map[string]any{
		"tool":      step.ToolName,
		"stepIndex": step.StepIndex,
		"risk":      step.Risk,
	})

	if tool.Execute == nil {
		result := agentToolResult{
			Name:         step.ToolName,
			Label:        tool.Label,
			Status:       "failed",
			Summary:      "该 Project Agent 工具尚未接入执行器。",
			ErrorCode:    "AGENT_TOOL_NOT_IMPLEMENTED",
			ErrorMessage: "agent tool executor is not implemented",
		}
		return result, s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result)
	}
	registryResult, err := tool.Execute(r.Context(), agentToolContext(project, principal, task, step), step.Input)
	result := agentToolResultFromRegistry(registryResult)
	if err != nil {
		args, _ := agentStepArgs(step.Input)
		result = agentToolError(step.ToolName, args, err)
	}
	if result.Name == "" {
		result.Name = step.ToolName
	}
	if result.Label == "" {
		result.Label = tool.Label
	}
	if result.Status == "" {
		result.Status = "succeeded"
	}
	if result.Status != "succeeded" && result.Status != "blocked" && result.ErrorCode == "" {
		result.ErrorCode = "AGENT_TOOL_FAILED"
	}
	if err := s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result); err != nil {
		return agentToolResult{}, err
	}
	return result, nil
}

func (s *Server) executeProjectAgentTool(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, tool agent.AgentTool) agentToolResult {
	args, err := agentStepArgs(step.Input)
	if err != nil {
		return agentToolError(step.ToolName, nil, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "step input must be a JSON object"))
	}
	switch step.ToolName {
	case "project.read_summary":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolProjectStatus(r, principal, project, args))
	case "source.list":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolListSources(r, principal, project, args))
	case "source.list_chapters":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolListSourceChapters(r, principal, project, args))
	case "script.list":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolListScripts(r, principal, project, args))
	case "script.get":
		return s.agentToolGetScript(r, project, args)
	case "asset.list":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolListAssets(r, principal, project, args))
	case "storyboard.list":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolListStoryboardShots(r, principal, project, args))
	case "workflow.read_runs":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolListWorkflowRuns(r, principal, project, args))
	case "workflow.read_nodes":
		return s.agentToolListWorkflowNodes(r, project, args)
	case "workflow.read_shots":
		return s.agentToolListWorkflowShots(r, project, args)
	case "review.list_items":
		return s.agentToolListReviewItems(r, project, args)
	case "review.run":
		return s.agentToolRunReview(r, principal, project, args)
	case "review.generate_fix":
		return s.agentToolGenerateReviewFix(r, principal, project, args)
	case "review.apply_fix":
		return s.agentToolApplyReviewFix(r, principal, project, task, step, args)
	case "review.dismiss_fix":
		return s.agentToolDismissReviewFix(r, project, task, step, args)
	case "prompt.render_test":
		return s.agentToolRenderPromptTest(r, project, args)
	case "script.rewrite_preview":
		return s.agentToolRewriteScriptPreview(r, principal, project, task, step, args)
	case "script.generate_from_source":
		return s.agentToolGenerateScriptFromSource(r, principal, project, task, step, args)
	case "script.rewrite":
		return s.agentToolRewriteScript(r, principal, project, task, step, args)
	case "script.create_version":
		return s.agentToolCreateScriptVersion(r, principal, project, task, step, args)
	case "script.activate_version":
		return s.agentToolActivateScriptVersion(r, project, task, step, args)
	case "asset.update":
		return s.agentToolUpdateReviewPatchTarget(r, principal, project, task, step, args, "asset.update", "canonical_asset", "assetId")
	case "storyboard.update_shot":
		return s.agentToolUpdateReviewPatchTarget(r, principal, project, task, step, args, "storyboard.update_shot", "storyboard_shot", "shotId")
	case "storyboard.reorder":
		return s.agentToolReorderStoryboard(r, project, task, step, args)
	case "timeline.update_clip":
		return s.agentToolUpdateReviewPatchTarget(r, principal, project, task, step, args, "timeline.update_clip", "timeline_clip", "clipId")
	case "final_video.activate":
		return s.agentToolActivateFinalVideo(r, project, task, step, args)
	case "prompt.create_version":
		return s.agentToolCreatePromptVersion(r, principal, project, task, step, args)
	case "prompt.activate_version":
		return s.agentToolActivatePromptVersion(r, project, task, step, args)
	case "provider.test_model":
		return s.agentToolTestProviderModel(r, principal, project, task, step, args)
	case "provider.update_account":
		return s.agentToolUpdateProviderAccount(r, project, args)
	case "provider.update_model":
		return s.agentToolUpdateProviderModel(r, project, args)
	case "provider.install_catalog_preset":
		return s.agentToolInstallProviderCatalogPreset(r, principal, project, args)
	case "artifact.list":
		return s.agentToolListArtifacts(r, project, args)
	case "artifact.preview_url":
		return s.agentToolArtifactPreviewURL(r, project, args)
	case "provider.list_status":
		return s.agentToolProviderStatus(r, project, args)
	case "workflow.start":
		return s.agentToolStartWorkflow(r, principal, project, task, step, args)
	case "workflow.cancel":
		return normalizeProjectAgentResult(step.ToolName, tool.Label, s.agentToolCancelWorkflow(r, principal, project, args))
	case "shot.status":
		return s.agentToolShotStatus(r, project, args)
	case "shot.generate_missing_images":
		return s.agentToolRunShotProduction(r, principal, project, task, step, "generate_missing_images", args)
	case "shot.generate_missing_videos":
		return s.agentToolRunShotProduction(r, principal, project, task, step, "generate_missing_videos", args)
	case "shot.cancel_running_videos":
		return s.agentToolRunShotProduction(r, principal, project, task, step, "cancel_running_videos", args)
	case "timeline.compose":
		args["workflowType"] = "compose_timeline"
		args["input"] = agentMapArg(args, "input")
		if timelineID := agentReferenceStringArg(args, "timelineId"); timelineID != "" {
			agentMapArg(args, "input")["timelineId"] = timelineID
		}
		return s.agentToolStartWorkflow(r, principal, project, task, step, args)
	default:
		return agentToolResult{
			Name:         step.ToolName,
			Label:        tool.Label,
			Status:       "failed",
			Summary:      "该 Project Agent 工具尚未接入执行器。",
			Arguments:    args,
			ErrorCode:    "AGENT_TOOL_NOT_IMPLEMENTED",
			ErrorMessage: "agent tool executor is not implemented",
		}
	}
}

func (s *Server) storeAgentStepResult(ctx context.Context, project Project, taskID, stepID string, result agentToolResult) error {
	verifier := s.verifyAgentToolResult(ctx, project, result)
	if result.Status == "succeeded" && stringValueFromAny(verifier["status"]) == "failed" {
		result.Status = "failed"
		result.ErrorCode = firstNonEmpty(stringValueFromAny(verifier["errorCode"]), "AGENT_VERIFIER_FAILED")
		result.ErrorMessage = firstNonEmpty(stringValueFromAny(verifier["errorMessage"]), "agent verifier failed")
		result.Summary = result.ErrorMessage
	}
	status := "succeeded"
	eventType := "agent.step.completed"
	completedAt := "now()"
	if result.Status == "failed" {
		status = "failed"
		eventType = "agent.step.failed"
	} else if result.Status == "skipped" {
		status = "skipped"
	} else if result.Status == "blocked" {
		status = "blocked"
		eventType = "agent.step.blocked"
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_steps
		SET status = $3,
		    output = $4,
		    error_code = NULLIF($5, ''),
		    error_message = NULLIF($6, ''),
		    verifier_output = $7,
		    completed_at = `+completedAt+`
		WHERE id = $1 AND task_id = $2
	`, stepID, taskID, status, mustMarshal(result), result.ErrorCode, result.ErrorMessage, mustMarshal(verifier)); err != nil {
		return err
	}
	trace := newAgentTaskTrace()
	trace.AddResult(result)
	s.insertAgentStepEvent(ctx, project, taskID, stepID, eventType, map[string]any{
		"tool":         result.Name,
		"status":       result.Status,
		"summary":      result.Summary,
		"errorCode":    result.ErrorCode,
		"errorMessage": result.ErrorMessage,
		"retryable":    result.Retryable,
		"nextActions":  result.NextActions,
		"data":         result.Data,
		"verifier":     verifier,
		"trace":        trace.Patch(),
	})
	return nil
}

func (s *Server) verifyAgentToolResult(ctx context.Context, project Project, result agentToolResult) map[string]any {
	if result.Status != "succeeded" {
		return map[string]any{"status": "skipped", "reason": "tool did not succeed"}
	}
	ok := func(summary string) map[string]any {
		return map[string]any{"status": "succeeded", "summary": summary}
	}
	fail := func(code, message string) map[string]any {
		return map[string]any{"status": "failed", "errorCode": code, "errorMessage": message}
	}
	switch result.Name {
	case "workflow.start", "timeline.compose", "shot.cancel_running_videos":
		runID := stringValueFromAny(result.Data["workflowRunId"])
		if runID == "" {
			return fail("VERIFIER_MISSING_WORKFLOW_RUN", "workflowRunId is missing from tool output")
		}
		var status string
		if err := s.db.QueryRow(ctx, `SELECT status FROM workflow_runs WHERE project_id = $1 AND id = $2`, project.ID, runID).Scan(&status); err != nil {
			return fail("VERIFIER_WORKFLOW_NOT_FOUND", err.Error())
		}
		switch status {
		case "queued", "running", "succeeded", "cancelling", "cancelled":
			return ok("workflow run exists with status " + status)
		default:
			return fail("VERIFIER_WORKFLOW_BAD_STATUS", "workflow run status is "+status)
		}
	case "shot.generate_missing_images", "shot.generate_missing_videos":
		runID := stringValueFromAny(result.Data["workflowRunId"])
		if runID == "" {
			return fail("VERIFIER_MISSING_WORKFLOW_RUN", "workflowRunId is missing from tool output")
		}
		var workflowStatus string
		if err := s.db.QueryRow(ctx, `SELECT status FROM workflow_runs WHERE project_id = $1 AND id = $2`, project.ID, runID).Scan(&workflowStatus); err != nil {
			return fail("VERIFIER_WORKFLOW_NOT_FOUND", err.Error())
		}
		switch workflowStatus {
		case "queued", "running", "succeeded":
		default:
			return fail("VERIFIER_WORKFLOW_BAD_STATUS", "workflow run status is "+workflowStatus)
		}
		targets := stringSliceFromAny(result.Data["targetShotIds"])
		if len(targets) == 0 {
			return fail("VERIFIER_MISSING_TARGET_SHOTS", "targetShotIds is missing from tool output")
		}
		column := "image_status"
		if result.Name == "shot.generate_missing_videos" {
			column = "video_status"
		}
		var invalidCount int
		if err := s.db.QueryRow(ctx, `
			SELECT count(*)
			FROM storyboard_shots
			WHERE project_id = $1
			  AND id = ANY($2::uuid[])
			  AND COALESCE(`+column+`, '') NOT IN ('queued', 'running', 'succeeded')
		`, project.ID, targets).Scan(&invalidCount); err != nil {
			return fail("VERIFIER_SHOT_STATUS_CHECK_FAILED", err.Error())
		}
		if invalidCount > 0 {
			return fail("VERIFIER_SHOT_STATUS_NOT_QUEUED", fmt.Sprintf("%d target shots did not enter queued/running/succeeded", invalidCount))
		}
		return ok(fmt.Sprintf("%d target shots entered %s queued/running/succeeded", len(targets), strings.TrimSuffix(column, "_status")))
	case "workflow.cancel":
		runID := workflowRunIDFromValue(result.Data["workflowRun"])
		if runID == "" {
			return fail("VERIFIER_MISSING_WORKFLOW_RUN", "workflowRun.id is missing from tool output")
		}
		var status string
		if err := s.db.QueryRow(ctx, `SELECT status FROM workflow_runs WHERE project_id = $1 AND id = $2`, project.ID, runID).Scan(&status); err != nil {
			return fail("VERIFIER_WORKFLOW_NOT_FOUND", err.Error())
		}
		switch status {
		case "cancelling", "cancelled", "succeeded", "failed":
			return ok("workflow run cancellation state is " + status)
		default:
			return fail("VERIFIER_WORKFLOW_CANCEL_BAD_STATUS", "workflow run status is "+status)
		}
	case "review.apply_fix":
		fixID := stringValueFromAny(result.Data["fixId"])
		if fixID == "" {
			return fail("VERIFIER_MISSING_FIX", "fixId is missing from tool output")
		}
		var status, entityType string
		var entityID *string
		var afterPreview json.RawMessage
		if err := s.db.QueryRow(ctx, `
			SELECT status, target_entity_type, target_entity_id, after_preview
			FROM review_fixes
			WHERE project_id = $1 AND id = $2
		`, project.ID, fixID).Scan(&status, &entityType, &entityID, &afterPreview); err != nil {
			return fail("VERIFIER_FIX_NOT_FOUND", err.Error())
		}
		if status != "applied" {
			return fail("VERIFIER_FIX_NOT_APPLIED", "review fix status is "+status)
		}
		if boolValue(result.Data["resolveReviewItem"]) {
			var itemStatus string
			if err := s.db.QueryRow(ctx, `SELECT ri.status FROM review_items ri JOIN review_fixes rf ON rf.review_item_id = ri.id WHERE rf.project_id = $1 AND rf.id = $2`, project.ID, fixID).Scan(&itemStatus); err != nil {
				return fail("VERIFIER_REVIEW_ITEM_CHECK_FAILED", err.Error())
			}
			if itemStatus != "resolved" {
				return fail("VERIFIER_REVIEW_ITEM_NOT_RESOLVED", "review item status is "+itemStatus)
			}
		}
		if entityID == nil || strings.TrimSpace(*entityID) == "" || len(afterPreview) == 0 || string(afterPreview) == "null" {
			return ok("review fix is applied")
		}
		target, err := reviewpkg.LoadReviewFixTarget(ctx, s.db, project.ID, entityType, *entityID)
		if err != nil {
			return fail("VERIFIER_FIX_TARGET_NOT_FOUND", err.Error())
		}
		expected := rawObject(afterPreview)
		for key, want := range expected {
			got, exists := target.Snapshot[key]
			if !exists || !agentJSONEqual(got, want) {
				return fail("VERIFIER_FIX_TARGET_MISMATCH", "review fix target field "+key+" does not match afterPreview")
			}
		}
		return ok("review fix is applied and target snapshot matches")
	case "script.activate_version":
		scriptID := stringValueFromAny(result.Data["scriptId"])
		versionID := stringValueFromAny(result.Data["versionId"])
		var current string
		if err := s.db.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE project_id = $1 AND id = $2`, project.ID, scriptID).Scan(&current); err != nil {
			return fail("VERIFIER_SCRIPT_NOT_FOUND", err.Error())
		}
		if current != versionID {
			return fail("VERIFIER_SCRIPT_VERSION_NOT_ACTIVE", "script current version does not match tool output")
		}
		return ok("script version is active")
	case "script.create_version", "script.generate_from_source", "script.rewrite":
		versionID := stringValueFromAny(result.Data["versionId"])
		var exists bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM script_versions WHERE project_id = $1 AND id = $2)`, project.ID, versionID).Scan(&exists); err != nil {
			return fail("VERIFIER_SCRIPT_VERSION_CHECK_FAILED", err.Error())
		}
		if !exists {
			return fail("VERIFIER_SCRIPT_VERSION_NOT_FOUND", "script version was not found")
		}
		return ok("script version exists")
	case "prompt.activate_version":
		versionID := stringValueFromAny(result.Data["versionId"])
		var status string
		if err := s.db.QueryRow(ctx, `SELECT status FROM prompt_versions WHERE id = $1`, versionID).Scan(&status); err != nil {
			return fail("VERIFIER_PROMPT_VERSION_NOT_FOUND", err.Error())
		}
		if status != "active" {
			return fail("VERIFIER_PROMPT_VERSION_NOT_ACTIVE", "prompt version status is "+status)
		}
		return ok("prompt version is active")
	case "final_video.activate":
		versionID := stringValueFromAny(result.Data["versionId"])
		var active string
		if err := s.db.QueryRow(ctx, `SELECT active_final_video_version_id::text FROM projects WHERE id = $1`, project.ID).Scan(&active); err != nil {
			return fail("VERIFIER_FINAL_VIDEO_CHECK_FAILED", err.Error())
		}
		if active != versionID {
			return fail("VERIFIER_FINAL_VIDEO_NOT_ACTIVE", "project active final video does not match tool output")
		}
		return ok("final video is active")
	case "artifact.preview_url":
		if stringValueFromAny(result.Data["url"]) == "" || stringValueFromAny(result.Data["expiresAt"]) == "" {
			return fail("VERIFIER_PREVIEW_URL_INVALID", "preview url or expiry is missing")
		}
		return ok("preview url exists")
	case "asset.update", "storyboard.update_shot", "timeline.update_clip", "storyboard.reorder":
		return ok("tool returned explicit before/after update output")
	default:
		return map[string]any{"status": "skipped", "reason": "no verifier for tool"}
	}
}

func (s *Server) finishAgentTaskState(ctx context.Context, projectID, taskID, status, code, message string) (AgentTask, error) {
	completedAtExpr := "completed_at"
	if isTerminalAgentTaskStatus(status) {
		completedAtExpr = "COALESCE(completed_at, now())"
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET status = $3,
		    error_code = CASE WHEN $4 = '' THEN NULL ELSE $4 END,
		    error_message = CASE WHEN $5 = '' THEN NULL ELSE $5 END,
		    completed_at = `+completedAtExpr+`
		WHERE id = $1 AND project_id = $2
	`, taskID, projectID, status, code, message); err != nil {
		return AgentTask{}, err
	}
	if status == "succeeded" {
		if err := s.mergeAgentTaskCompletionSummary(ctx, projectID, taskID); err != nil {
			return AgentTask{}, err
		}
	}
	return s.agentTaskWithDetails(requestWithContext(ctx), projectID, taskID)
}

type agentPendingWorkflowRun struct {
	ID                  string `json:"id"`
	WorkflowType        string `json:"workflowType,omitempty"`
	Status              string `json:"status"`
	ActiveNodeRuns      int    `json:"activeNodeRuns,omitempty"`
	ActiveProviderTasks int    `json:"activeProviderTasks,omitempty"`
}

func (s *Server) finishAgentTaskWaitingForWorkflows(ctx context.Context, project Project, taskID string, runs []agentPendingWorkflowRun) (AgentTask, error) {
	summary := map[string]any{
		"summary":                fmt.Sprintf("已启动生产任务，正在等待 %d 个工作流完成。", len(runs)),
		"waitingForWorkflowRuns": runs,
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET status = 'running',
		    summary = COALESCE(summary, '{}'::jsonb) || $3::jsonb,
		    error_code = NULL,
		    error_message = NULL,
		    completed_at = NULL
		WHERE id = $1 AND project_id = $2
	`, taskID, project.ID, mustMarshal(summary)); err != nil {
		return AgentTask{}, err
	}
	s.insertAgentTaskEvent(ctx, project, taskID, "agent.task.waiting_workflow", summary)
	return s.agentTaskWithDetails(requestWithContext(ctx), project.ID, taskID)
}

func (s *Server) agentTaskPendingWorkflowRuns(ctx context.Context, projectID, taskID string) ([]agentPendingWorkflowRun, error) {
	ids, err := s.agentTaskWorkflowRunIDs(ctx, taskID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT
			w.id::text,
			COALESCE(w.input->>'workflowType', w.template_id::text, ''),
			w.status,
			(
				SELECT count(*)
				FROM workflow_node_runs n
				WHERE n.workflow_run_id = w.id
				  AND n.status IN ('queued', 'running')
			),
			(
				SELECT count(*)
				FROM provider_async_tasks p
				WHERE p.workflow_run_id = w.id
				  AND p.status IN ('queued', 'running', 'cancelling')
			)
		FROM workflow_runs w
		WHERE w.project_id = $1
		  AND w.id = ANY($2::uuid[])
	`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pending := make([]agentPendingWorkflowRun, 0)
	for rows.Next() {
		var run agentPendingWorkflowRun
		if err := rows.Scan(&run.ID, &run.WorkflowType, &run.Status, &run.ActiveNodeRuns, &run.ActiveProviderTasks); err != nil {
			return nil, err
		}
		if !isTerminalWorkflowStatus(run.Status) || run.ActiveNodeRuns > 0 || run.ActiveProviderTasks > 0 {
			pending = append(pending, run)
		}
	}
	return pending, rows.Err()
}

func (s *Server) agentTaskWorkflowRunIDs(ctx context.Context, taskID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT tool_name, output
		FROM agent_steps
		WHERE task_id = $1
		  AND status IN ('running', 'succeeded')
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	ids := make([]string, 0)
	for rows.Next() {
		var toolName string
		var raw json.RawMessage
		if err := rows.Scan(&toolName, &raw); err != nil {
			return nil, err
		}
		if !agentToolWaitsForWorkflow(toolName) {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		collectAgentWorkflowRunIDs(value, func(id string) {
			if id == "" || seen[id] {
				return
			}
			seen[id] = true
			ids = append(ids, id)
		})
	}
	return ids, rows.Err()
}

func agentToolWaitsForWorkflow(toolName string) bool {
	switch toolName {
	case "workflow.start",
		"timeline.compose",
		"shot.generate_missing_images",
		"shot.generate_missing_videos",
		"shot.cancel_running_videos":
		return true
	default:
		return false
	}
}

func collectAgentWorkflowRunIDs(value any, add func(string)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch key {
			case "workflowRunId":
				add(stringValueFromAny(child))
			case "workflowRunIds":
				for _, id := range stringSliceFromAny(child) {
					add(id)
				}
			case "workflowRun":
				if item, ok := mapFromAny(child); ok {
					add(stringValueFromAny(item["id"]))
				}
			}
			collectAgentWorkflowRunIDs(child, add)
		}
	case []any:
		for _, child := range typed {
			collectAgentWorkflowRunIDs(child, add)
		}
	}
}

func (s *Server) appendAgentAutoContinuationPlan(r *http.Request, principal auth.Principal, project Project, taskID string) (bool, *AgentTask, error) {
	task, err := s.agentTask(r, project.ID, taskID)
	if err != nil {
		return false, nil, err
	}
	if task.Mode == string(agent.TaskModePlanOnly) || !agentGoalRequestsAutoProduction(task.UserGoal) {
		return false, nil, nil
	}
	status, err := s.productionStatus(r, project)
	if err != nil {
		return false, nil, err
	}
	gapSummary, err := s.agentProjectGapSummary(r.Context(), project, status)
	if err != nil {
		return false, nil, err
	}
	plan, ok := agentAutoProductionPlanFromSummary(task.UserGoal, gapSummary)
	if !ok || !agentPlanHasContinuationStep(plan) {
		return false, nil, s.mergeAgentTaskSummaryPatch(r.Context(), project.ID, task.ID, map[string]any{
			"summary":           gapSummary.Summary,
			"projectGapSummary": gapSummary,
		})
	}
	registry, err := s.projectAgentRegistry()
	if err != nil {
		return false, nil, err
	}
	validated, err := agent.ValidatePlan(plan, registry, 20)
	if err != nil {
		return false, nil, err
	}
	if duplicate, fingerprints, err := s.agentTaskHasDuplicateContinuationStep(r.Context(), task.ID, validated); err != nil {
		return false, nil, err
	} else if duplicate {
		stopped, err := s.blockAgentAutoContinuation(r.Context(), project, task.ID, fingerprints, gapSummary)
		return false, &stopped, err
	}
	planInput := map[string]any{
		"goal":            task.UserGoal,
		"mode":            task.Mode,
		"constraints":     json.RawMessage(task.Constraints),
		"plannerMode":     "deterministic_continuation",
		"projectGapHash":  promptsvc.HashText(string(mustMarshal(gapSummary))),
		"modelProfileKey": project.ScriptModelProfileKey,
	}
	promptHash := promptsvc.HashText("agent-auto-continuation:" + task.ID + ":" + string(mustMarshal(planInput)))
	var runID string
	if err := s.db.QueryRow(r.Context(), `
		INSERT INTO agent_runs(
			organization_id, project_id, session_id, agent_type, task_type, status,
			input, prompt_hash, task_id, created_by, started_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, 'project_agent', 'auto_continue', 'running', $4, $5, $6, $7, now())
		RETURNING id
	`, project.OrganizationID, project.ID, stringValue(task.SessionID), mustMarshal(planInput), promptHash, task.ID, principal.UserID).Scan(&runID); err != nil {
		return false, nil, err
	}
	if err := s.persistAgentPlan(r, principal, project, task, registry, validated, runID, provider.GatewayTextResponse{}); err != nil {
		return false, nil, err
	}
	s.insertAgentTaskEvent(r.Context(), project, task.ID, "agent.task.continued", map[string]any{
		"summary":      validated.Summary,
		"appendedPlan": validated,
	})
	return true, nil, nil
}

func (s *Server) mergeAgentTaskSummaryPatch(ctx context.Context, projectID, taskID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET summary = COALESCE(summary, '{}'::jsonb) || $3::jsonb
		WHERE id = $1 AND project_id = $2
	`, taskID, projectID, mustMarshal(patch))
	return err
}

func agentPlanHasContinuationStep(plan agent.Plan) bool {
	for _, step := range plan.Steps {
		if strings.TrimSpace(step.Tool) != "" && step.Tool != "project.read_summary" {
			return true
		}
	}
	return false
}

func (s *Server) agentTaskHasDuplicateContinuationStep(ctx context.Context, taskID string, plan agent.Plan) (bool, []string, error) {
	fingerprints := agentPlanContinuationFingerprints(plan)
	if len(fingerprints) == 0 {
		return false, nil, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT tool_name, input
		FROM agent_steps
		WHERE task_id = $1
		  AND status NOT IN ('failed', 'skipped', 'cancelled')
	`, taskID)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var tool string
		var input json.RawMessage
		if err := rows.Scan(&tool, &input); err != nil {
			return false, nil, err
		}
		existing[agentStepFingerprint(tool, input)] = true
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	for _, fingerprint := range fingerprints {
		if existing[fingerprint] {
			return true, fingerprints, nil
		}
	}
	return false, fingerprints, nil
}

func agentPlanContinuationFingerprints(plan agent.Plan) []string {
	out := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		tool := strings.TrimSpace(step.Tool)
		if tool == "" || tool == "project.read_summary" || tool == "review.run" {
			continue
		}
		out = append(out, agentStepFingerprint(tool, step.Args))
	}
	return out
}

func agentStepFingerprint(tool string, raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(tool) + ":" + strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(tool) + ":" + string(mustMarshal(value))
}

func (s *Server) blockAgentAutoContinuation(ctx context.Context, project Project, taskID string, fingerprints []string, gapSummary agentProjectGapSummary) (AgentTask, error) {
	message := "自动推进未产生新的可执行步骤，已暂停等待人工处理。"
	patch := map[string]any{
		"summary":                          message,
		"projectGapSummary":                gapSummary,
		"autoContinuationStopped":          true,
		"autoContinuationStepFingerprints": fingerprints,
		"nextActions":                      gapSummary.NextActions,
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET status = 'blocked',
		    error_code = 'AGENT_AUTO_CONTINUATION_STALLED',
		    error_message = $3,
		    summary = COALESCE(summary, '{}'::jsonb) || $4::jsonb
		WHERE id = $1 AND project_id = $2
	`, taskID, project.ID, message, mustMarshal(patch)); err != nil {
		return AgentTask{}, err
	}
	s.insertAgentTaskEvent(ctx, project, taskID, "agent.task.blocked", patch)
	return s.agentTaskWithDetails(requestWithContext(ctx), project.ID, taskID)
}

func (s *Server) mergeAgentTaskCompletionSummary(ctx context.Context, projectID, taskID string) error {
	rows, err := s.db.Query(ctx, `
		SELECT output
		FROM agent_steps
		WHERE task_id = $1 AND status = 'succeeded'
		ORDER BY step_index ASC
	`, taskID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var patch map[string]any
	completedTools := make([]map[string]any, 0)
	trace := newAgentTaskTrace()
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var result agentToolResult
		if err := json.Unmarshal(raw, &result); err == nil && result.Name != "" {
			completedTools = append(completedTools, map[string]any{
				"name":    result.Name,
				"label":   result.Label,
				"status":  result.Status,
				"summary": result.Summary,
			})
			trace.AddResult(result)
		}
		if stepPatch, ok := agentTaskSummaryPatchFromStepOutput(raw); ok {
			patch = stepPatch
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if patch == nil {
		if len(completedTools) == 0 {
			return nil
		}
		patch = map[string]any{}
	}
	if len(completedTools) > 0 {
		patch["completedTools"] = completedTools
	}
	if tracePatch := trace.Patch(); len(tracePatch) > 0 {
		patch["trace"] = tracePatch
	}
	if len(patch) == 0 {
		return nil
	}
	_, err = s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET summary = COALESCE(summary, '{}'::jsonb) || $3::jsonb
		WHERE id = $1 AND project_id = $2
	`, taskID, projectID, mustMarshal(patch))
	return err
}

type agentTaskTrace struct {
	providerCallIDs  map[string]bool
	promptHashes     map[string]bool
	promptVersionIDs map[string]bool
	artifactIDs      map[string]bool
	workflowRunIDs   map[string]bool
	agentRunIDs      map[string]bool
	testRunIDs       map[string]bool
	modelIDs         map[string]bool
}

func newAgentTaskTrace() *agentTaskTrace {
	return &agentTaskTrace{
		providerCallIDs:  map[string]bool{},
		promptHashes:     map[string]bool{},
		promptVersionIDs: map[string]bool{},
		artifactIDs:      map[string]bool{},
		workflowRunIDs:   map[string]bool{},
		agentRunIDs:      map[string]bool{},
		testRunIDs:       map[string]bool{},
		modelIDs:         map[string]bool{},
	}
}

func (t *agentTaskTrace) AddResult(result agentToolResult) {
	if t == nil || result.Data == nil {
		return
	}
	t.addData(result.Data)
}

func (t *agentTaskTrace) Patch() map[string]any {
	if t == nil {
		return nil
	}
	patch := map[string]any{}
	if values := sortedTraceValues(t.providerCallIDs); len(values) > 0 {
		patch["providerCallIds"] = values
	}
	if values := sortedTraceValues(t.promptHashes); len(values) > 0 {
		patch["promptHashes"] = values
	}
	if values := sortedTraceValues(t.promptVersionIDs); len(values) > 0 {
		patch["promptVersionIds"] = values
	}
	if values := sortedTraceValues(t.artifactIDs); len(values) > 0 {
		patch["artifactIds"] = values
	}
	if values := sortedTraceValues(t.workflowRunIDs); len(values) > 0 {
		patch["workflowRunIds"] = values
	}
	if values := sortedTraceValues(t.agentRunIDs); len(values) > 0 {
		patch["agentRunIds"] = values
	}
	if values := sortedTraceValues(t.testRunIDs); len(values) > 0 {
		patch["testRunIds"] = values
	}
	if values := sortedTraceValues(t.modelIDs); len(values) > 0 {
		patch["modelIds"] = values
	}
	if len(patch) > 0 {
		patch["source"] = "agent_step_outputs"
	}
	return patch
}

func (t *agentTaskTrace) addData(data map[string]any) {
	t.addValue(t.providerCallIDs, data["providerCallId"])
	t.addValue(t.providerCallIDs, data["providerCallIds"])
	t.addValue(t.promptHashes, data["promptHash"])
	t.addValue(t.promptHashes, data["promptHashes"])
	t.addValue(t.promptVersionIDs, data["promptVersionId"])
	t.addValue(t.promptVersionIDs, data["promptVersionIds"])
	t.addValue(t.artifactIDs, data["artifactId"])
	t.addValue(t.artifactIDs, data["artifactIds"])
	t.addValue(t.workflowRunIDs, data["workflowRunId"])
	t.addValue(t.workflowRunIDs, data["workflowRunIds"])
	t.addValue(t.agentRunIDs, data["agentRunId"])
	t.addValue(t.agentRunIDs, data["agentRunIds"])
	t.addValue(t.testRunIDs, data["testRunId"])
	t.addValue(t.testRunIDs, data["testRunIds"])
	t.addValue(t.modelIDs, data["modelId"])
	t.addValue(t.modelIDs, data["modelIds"])
	if nested, ok := mapFromAny(data["result"]); ok {
		t.addData(nested)
	}
	if nested, ok := mapFromAny(data["artifact"]); ok {
		t.addValue(t.artifactIDs, nested["id"])
		t.addValue(t.promptHashes, nested["promptHash"])
		t.addValue(t.modelIDs, nested["modelId"])
	}
	if nested, ok := mapFromAny(data["workflowRun"]); ok {
		t.addValue(t.workflowRunIDs, nested["id"])
	}
	if items, ok := sliceFromAny(data["artifacts"]); ok {
		for _, item := range items {
			if nested, ok := mapFromAny(item); ok {
				t.addValue(t.artifactIDs, nested["id"])
				t.addValue(t.promptHashes, nested["promptHash"])
				t.addValue(t.modelIDs, nested["modelId"])
			}
		}
	}
}

func (t *agentTaskTrace) addValue(set map[string]bool, value any) {
	for _, item := range traceStringsFromAny(value) {
		set[item] = true
	}
}

func traceStringsFromAny(value any) []string {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, traceStringsFromAny(item)...)
		}
		return out
	case map[string]any:
		return traceStringsFromAny(typed["id"])
	default:
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			return []string{text}
		}
	}
	return nil
}

func sliceFromAny(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	switch typed := value.(type) {
	case []any:
		return typed, true
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		out := []any{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}

func sortedTraceValues(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func (s *Server) insertAgentStepEvent(ctx context.Context, project Project, taskID, stepID, eventType string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["agentTaskId"] = taskID
	payload["agentStepId"] = stepID
	if sessionID := s.agentTaskSessionID(ctx, taskID); sessionID != "" {
		payload["sessionId"] = sessionID
	}
	_, _ = s.db.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, $3, 'agent_step', $4, $5)
	`, project.OrganizationID, project.ID, eventType, stepID, mustMarshal(payload))
}

func (s *Server) insertAgentTaskEvent(ctx context.Context, project Project, taskID, eventType string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["agentTaskId"] = taskID
	if sessionID := s.agentTaskSessionID(ctx, taskID); sessionID != "" {
		payload["sessionId"] = sessionID
	}
	_, _ = s.db.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, $3, 'agent_task', $4, $5)
	`, project.OrganizationID, project.ID, eventType, taskID, mustMarshal(payload))
}

func (s *Server) agentTaskSessionID(ctx context.Context, taskID string) string {
	var sessionID string
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(session_id::text, '') FROM agent_tasks WHERE id = $1`, taskID).Scan(&sessionID)
	return strings.TrimSpace(sessionID)
}

func agentStepArgs(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func normalizeProjectAgentResult(name, label string, result agentToolResult) agentToolResult {
	result.Name = name
	result.Label = label
	return result
}

func isTerminalAgentTaskStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled"
}

func requestWithContext(ctx context.Context) *http.Request {
	return (&http.Request{}).WithContext(ctx)
}

func (s *Server) agentToolListWorkflowNodes(r *http.Request, project Project, args map[string]any) agentToolResult {
	runID := agentReferenceStringArg(args, "workflowRunId")
	if runID == "" {
		return agentToolError("workflow.read_nodes", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowRunId is required"))
	}
	if _, err := s.workflowRunForProject(r, project.ID, runID); err != nil {
		return agentToolError("workflow.read_nodes", args, err)
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id, node_key, node_type, status, input, output, retry_count, error_code, error_message, started_at, completed_at, created_at
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		ORDER BY created_at ASC
	`, runID)
	if err != nil {
		return agentToolError("workflow.read_nodes", args, err)
	}
	defer rows.Close()
	items := make([]WorkflowNodeRun, 0)
	for rows.Next() {
		var item WorkflowNodeRun
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.WorkflowRunID, &item.NodeKey, &item.NodeType, &item.Status, &item.Input, &item.Output, &item.RetryCount, &item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.CompletedAt, &item.CreatedAt); err != nil {
			return agentToolError("workflow.read_nodes", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("workflow.read_nodes", args, err)
	}
	return agentToolOK("workflow.read_nodes", args, fmt.Sprintf("读取到 %d 个工作流节点。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolListWorkflowShots(r *http.Request, project Project, args map[string]any) agentToolResult {
	runID := agentReferenceStringArg(args, "workflowRunId")
	if runID == "" {
		return agentToolError("workflow.read_shots", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowRunId is required"))
	}
	if _, err := s.workflowRunForProject(r, project.ID, runID); err != nil {
		return agentToolError("workflow.read_shots", args, err)
	}
	rows, err := s.db.Query(r.Context(), storyboardShotSelectSQL(`
		WHERE s.workflow_run_id = $1
		  AND s.deleted_at IS NULL
		ORDER BY s.shot_index ASC
	`), runID)
	if err != nil {
		return agentToolError("workflow.read_shots", args, err)
	}
	defer rows.Close()
	items := make([]StoryboardShot, 0)
	for rows.Next() {
		item, err := scanStoryboardShot(rows)
		if err != nil {
			return agentToolError("workflow.read_shots", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("workflow.read_shots", args, err)
	}
	return agentToolOK("workflow.read_shots", args, fmt.Sprintf("读取到 %d 个分镜镜头。", len(items)), map[string]any{"items": items})
}

func (s *Server) workflowRunForProject(r *http.Request, projectID, runID string) (WorkflowRun, error) {
	return scanWorkflowRun(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE id = $1 AND project_id = $2
	`, runID, projectID))
}

func (s *Server) agentToolGetScript(r *http.Request, project Project, args map[string]any) agentToolResult {
	scriptID := agentReferenceStringArg(args, "scriptId")
	if scriptID == "" {
		return agentToolError("script.get", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId is required"))
	}
	item, err := s.script(r, project.ID, scriptID)
	if err != nil {
		return agentToolError("script.get", args, err)
	}
	versionID := firstNonEmpty(agentReferenceStringArg(args, "versionId"), stringValue(item.CurrentVersionID))
	if versionID != "" {
		version, err := s.scriptVersion(r, project.ID, item.ID, versionID)
		if err != nil {
			return agentToolError("script.get", args, err)
		}
		item.CurrentVersion = &version
	}
	return agentToolOK("script.get", args, "已读取剧本《"+item.Title+"》。", map[string]any{"script": item})
}

func (s *Server) agentToolListReviewItems(r *http.Request, project Project, args map[string]any) agentToolResult {
	status := firstNonEmpty(agentStringArg(args, "status"), "open")
	limit := agentIntArg(args, "limit", 50, 1, 200)
	rows, err := s.db.Query(r.Context(), reviewItemSelectSQL(`
		WHERE project_id = $1
		  AND ($2 = 'all' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3
	`), project.ID, status, limit)
	if err != nil {
		return agentToolError("review.list_items", args, err)
	}
	defer rows.Close()
	items := make([]ReviewItem, 0)
	for rows.Next() {
		item, err := scanReviewItem(rows)
		if err != nil {
			return agentToolError("review.list_items", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("review.list_items", args, err)
	}
	return agentToolOK("review.list_items", args, fmt.Sprintf("读取到 %d 个审阅问题。", len(items)), map[string]any{"items": items, "status": status})
}

func (s *Server) agentToolListArtifacts(r *http.Request, project Project, args map[string]any) agentToolResult {
	limit := agentIntArg(args, "limit", 50, 1, 100)
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_at
		FROM artifacts
		WHERE organization_id = $1 AND project_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, project.OrganizationID, project.ID, limit)
	if err != nil {
		return agentToolError("artifact.list", args, err)
	}
	defer rows.Close()
	items := make([]Artifact, 0)
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return agentToolError("artifact.list", args, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return agentToolError("artifact.list", args, err)
	}
	return agentToolOK("artifact.list", args, fmt.Sprintf("读取到 %d 个成果。", len(items)), map[string]any{"items": items})
}

func (s *Server) agentToolArtifactPreviewURL(r *http.Request, project Project, args map[string]any) agentToolResult {
	artifactID := agentReferenceStringArg(args, "artifactId")
	if artifactID == "" {
		return agentToolError("artifact.preview_url", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "artifactId is required"))
	}
	if s.storage == nil {
		return agentToolError("artifact.preview_url", args, apiError{Status: http.StatusServiceUnavailable, Code: "STORAGE_UNAVAILABLE", Message: "object storage is not configured", Retryable: true})
	}
	artifact, err := s.artifact(r, artifactID)
	if err != nil {
		return agentToolError("artifact.preview_url", args, err)
	}
	if artifact.ProjectID == nil || *artifact.ProjectID != project.ID {
		return agentToolError("artifact.preview_url", args, auth.ErrForbidden)
	}
	if artifact.StorageKey == nil || strings.TrimSpace(*artifact.StorageKey) == "" || !artifactCanPreview(artifact) {
		return agentToolError("artifact.preview_url", args, newAPIError(http.StatusUnprocessableEntity, "UNSUPPORTED_PREVIEW_TYPE", "artifact cannot be previewed"))
	}
	expires := agentIntArg(args, "expiresSeconds", 900, 60, 86400)
	presigned, err := s.storage.PresignGetObject(r.Context(), *artifact.StorageKey, previewURLExpiry(expires))
	if err != nil {
		return agentToolError("artifact.preview_url", args, err)
	}
	return agentToolOK("artifact.preview_url", args, "已生成成果预览链接。", map[string]any{
		"artifactId": artifact.ID,
		"storageKey": presigned.StorageKey,
		"url":        presigned.URL,
		"method":     presigned.Method,
		"expiresAt":  presigned.ExpiresAt,
	})
}

func (s *Server) agentToolProviderStatus(r *http.Request, project Project, args map[string]any) agentToolResult {
	var accounts, activeAccounts, disabledAccounts, models, activeModels, disabledModels, recentCalls, failedRecentCalls int
	err := s.db.QueryRow(r.Context(), `
		SELECT
		  (SELECT count(*) FROM provider_accounts WHERE organization_id = $1),
		  (SELECT count(*) FROM provider_accounts WHERE organization_id = $1 AND status = 'active'),
		  (SELECT count(*) FROM provider_accounts WHERE organization_id = $1 AND status = 'disabled'),
		  (SELECT count(*) FROM provider_models m JOIN provider_accounts a ON a.id = m.provider_account_id WHERE a.organization_id = $1),
		  (SELECT count(*) FROM provider_models m JOIN provider_accounts a ON a.id = m.provider_account_id WHERE a.organization_id = $1 AND m.status = 'active' AND a.status = 'active'),
		  (SELECT count(*) FROM provider_models m JOIN provider_accounts a ON a.id = m.provider_account_id WHERE a.organization_id = $1 AND m.status = 'disabled'),
		  (SELECT count(*) FROM provider_call_logs WHERE organization_id = $1 AND created_at >= now() - interval '24 hours'),
		  (SELECT count(*) FROM provider_call_logs WHERE organization_id = $1 AND created_at >= now() - interval '24 hours' AND status = 'failed')
	`, project.OrganizationID).Scan(&accounts, &activeAccounts, &disabledAccounts, &models, &activeModels, &disabledModels, &recentCalls, &failedRecentCalls)
	if err != nil {
		return agentToolError("provider.list_status", args, err)
	}
	return agentToolOK("provider.list_status", args, fmt.Sprintf("当前有 %d 个启用供应商、%d 个启用模型。", activeAccounts, activeModels), map[string]any{
		"accounts":          accounts,
		"activeAccounts":    activeAccounts,
		"disabledAccounts":  disabledAccounts,
		"models":            models,
		"activeModels":      activeModels,
		"disabledModels":    disabledModels,
		"recentCalls24h":    recentCalls,
		"failedCalls24h":    failedRecentCalls,
		"scriptProfileKey":  project.ScriptModelProfileKey,
		"imageProfileKey":   project.ImageModelProfileKey,
		"videoProfileKey":   project.VideoModelProfileKey,
		"productionProfile": project.ProductionMode,
	})
}

func (s *Server) agentToolStartWorkflow(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	workflowType := agentStringArg(args, "workflowType")
	if workflowType == "" {
		return agentToolError("workflow.start", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowType is required"))
	}
	input := cleanAgentReferenceOptions(agentMapArg(args, "input"))
	args["input"] = input
	spec, err := s.agentWorkflowStartSpec(r, project, workflowType, input)
	if err != nil {
		return agentToolError("workflow.start", args, err)
	}
	if existing, ok, err := s.agentWorkflowRunForStep(r.Context(), project.ID, task.ID, step.ID); err != nil {
		return agentToolError("workflow.start", args, err)
	} else if ok {
		return agentToolOK("workflow.start", args, fmt.Sprintf("已存在 %s 工作流 %s，未重复启动。", spec.WorkflowType, existing.ID), map[string]any{
			"workflowRunId": existing.ID,
			"workflowType":  spec.WorkflowType,
			"status":        existing.Status,
			"input":         rawObject(existing.Input),
			"agentTaskId":   task.ID,
			"agentStepId":   step.ID,
			"idempotent":    true,
		})
	}
	specInput := cloneMap(spec.Input)
	specInput["agentTaskId"] = task.ID
	specInput["agentStepId"] = step.ID
	specInput["idempotencyKey"] = agentStepIdempotencyKey(task, step)
	run, err := s.startProjectWorkflowCore(r.Context(), principal, project, spec.WorkflowType, specInput, spec.WorkflowFunc)
	if err != nil {
		return agentToolError("workflow.start", args, err)
	}
	data := map[string]any{
		"workflowRunId": run.ID,
		"workflowType":  spec.WorkflowType,
		"status":        run.Status,
		"input":         specInput,
		"agentTaskId":   task.ID,
		"agentStepId":   step.ID,
	}
	if spec.Note != "" {
		data["note"] = spec.Note
	}
	return agentToolOK("workflow.start", args, fmt.Sprintf("已启动 %s，工作流 %s 当前状态 %s。", spec.WorkflowType, run.ID, run.Status), data)
}

func (s *Server) agentWorkflowStartSpec(r *http.Request, project Project, workflowType string, input map[string]any) (productionWorkflowSpec, error) {
	if input == nil {
		input = map[string]any{}
	}
	options := cleanAgentReferenceOptions(input)
	req := ProductionActionRequest{Options: options}
	if sourceID := agentReferenceStringFromAny(input["sourceId"]); sourceID != "" {
		req.SourceID = sourceID
	}
	if scriptID := agentReferenceStringFromAny(input["scriptId"]); scriptID != "" {
		req.ScriptID = scriptID
	}
	switch workflowType {
	case "extract_novel_events":
		return s.productionActionWorkflowCore(r, project, "extract_events", req)
	case "generate_adaptation_plan":
		return s.productionActionWorkflowCore(r, project, "generate_adaptation_plan", req)
	case "adaptation_plan_to_script":
		return s.productionActionWorkflowCore(r, project, "generate_script_from_plan", req)
	case "source_to_script":
		return s.productionActionWorkflowCore(r, project, "generate_script", req)
	case "parse_script_scenes":
		return s.productionActionWorkflowCore(r, project, "parse_script_scenes", req)
	case "script_to_assets":
		action := "analyze_assets"
		if boolValue(input["generateImages"]) {
			action = "generate_asset_images"
		}
		return s.productionActionWorkflowCore(r, project, action, req)
	case "script_to_storyboard":
		action := "generate_storyboard"
		if boolValue(input["generateDerivedAssets"]) {
			action = "analyze_shot_assets"
		}
		return s.productionActionWorkflowCore(r, project, action, req)
	case "script_to_video":
		action := "generate_shot_videos"
		if boolValue(input["generateImages"]) && boolValue(input["skipCompose"]) {
			action = "generate_shot_images"
		}
		return s.productionActionWorkflowCore(r, project, action, req)
	case "full_production":
		return s.productionActionWorkflowCore(r, project, "run_full_production", req)
	case "compose_timeline":
		return s.productionActionWorkflowCore(r, project, "compose_final_video", req)
	case "video_production", "text_to_storyboard":
		normalized, err := normalizeWorkflowRequestInput(workflowType, mustMarshal(input), projectDefaultAspectRatio(project))
		if err != nil {
			return productionWorkflowSpec{}, err
		}
		normalizedMap := map[string]any{}
		_ = json.Unmarshal(normalized, &normalizedMap)
		return productionWorkflowSpec{WorkflowType: workflowType, Input: normalizedMap, WorkflowFunc: agentWorkflowFuncForType(workflowType)}, nil
	default:
		return productionWorkflowSpec{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "workflowType is not supported")
	}
}

func agentWorkflowFuncForType(workflowType string) any {
	switch workflowType {
	case "extract_novel_events":
		return workflows.ExtractNovelEventsWorkflow
	case "generate_adaptation_plan":
		return workflows.GenerateAdaptationPlanWorkflow
	case "adaptation_plan_to_script":
		return workflows.AdaptationPlanToScriptWorkflow
	case "source_to_script":
		return workflows.SourceToScriptWorkflow
	case "parse_script_scenes":
		return workflows.ParseScriptScenesWorkflow
	case "script_to_assets":
		return workflows.ScriptToAssetsWorkflow
	case "script_to_storyboard":
		return workflows.ScriptToStoryboardWorkflow
	case "script_to_video", "full_production", "video_production":
		return workflows.VideoProductionWorkflow
	case "compose_timeline":
		return workflows.ComposeTimelineWorkflow
	default:
		return workflows.TextToStoryboardWorkflow
	}
}

func (s *Server) agentWorkflowRunForStep(ctx context.Context, projectID, taskID, stepID string) (WorkflowRun, bool, error) {
	run, err := scanWorkflowRun(s.db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, template_id, temporal_workflow_id, status, input, output, error_code, error_message, created_by, created_at, started_at, completed_at, cancelled_at
		FROM workflow_runs
		WHERE project_id = $1
		  AND input->'input'->>'agentTaskId' = $2
		  AND input->'input'->>'agentStepId' = $3
		  AND status <> 'failed'
		ORDER BY created_at DESC
		LIMIT 1
	`, projectID, taskID, stepID))
	if err == pgx.ErrNoRows {
		return WorkflowRun{}, false, nil
	}
	if err != nil {
		return WorkflowRun{}, false, err
	}
	return run, true, nil
}

func (s *Server) agentToolShotStatus(r *http.Request, project Project, args map[string]any) agentToolResult {
	status, err := s.loadShotProductionStatus(r, project.ID, agentReferenceStringArg(args, "scriptSceneId"), agentReferenceStringArg(args, "workflowRunId"), false)
	if err != nil {
		return agentToolError("shot.status", args, err)
	}
	return agentToolOK("shot.status", args, fmt.Sprintf("读取到 %d 个镜头生产状态。", len(status.Shots)), map[string]any{"status": status})
}

func (s *Server) agentToolRunShotProduction(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, action string, args map[string]any) agentToolResult {
	status, err := s.loadShotProductionStatus(r, project.ID, agentReferenceStringArg(args, "scriptSceneId"), agentReferenceStringArg(args, "workflowRunId"), false)
	if err != nil {
		return agentToolError(action, args, err)
	}
	req := ShotProductionActionRequest{
		Action:        action,
		ScriptSceneID: agentReferenceStringArg(args, "scriptSceneId"),
		WorkflowRunID: agentReferenceStringArg(args, "workflowRunId"),
		ShotIDs:       agentReferenceStringSliceArg(args, "shotIds"),
		Options:       cleanAgentReferenceOptions(agentMapArg(args, "options")),
	}
	targets, errorCode := selectShotProductionTargets(req, status.Shots)
	if errorCode != "" {
		return agentToolError(action, args, newAPIError(http.StatusUnprocessableEntity, errorCode, shotProductionActionErrorMessage(errorCode)))
	}
	workflowType, workflowFunc, ok := shotProductionWorkflowForAction(action)
	if !ok {
		return agentToolError(action, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shot production action is not supported"))
	}
	input := map[string]any{
		"action":      action,
		"shotIds":     targets,
		"force":       shotProductionOptionBool(req.Options, "force", true),
		"aspectRatio": firstNonEmptyString(shotProductionOptionString(req.Options, "aspectRatio"), project.VideoRatio, stringValue(project.AspectRatio), "16:9"),
		"resolution":  firstNonEmptyString(shotProductionOptionString(req.Options, "resolution"), "720p"),
	}
	if value := shotProductionOptionFloat(req.Options, "duration", 0); value > 0 {
		input["duration"] = value
	}
	if value := shotProductionOptionInt(req.Options, "maxConcurrency", 1); value > 0 {
		input["maxConcurrency"] = value
	}
	if value := shotProductionOptionInt(req.Options, "pollIntervalSeconds", 0); value > 0 {
		input["pollIntervalSeconds"] = value
	}
	if value := shotProductionOptionInt(req.Options, "maxPolls", 0); value > 0 {
		input["maxPolls"] = value
	}
	if existing, ok, err := s.agentWorkflowRunForStep(r.Context(), project.ID, task.ID, step.ID); err != nil {
		return agentToolError(action, args, err)
	} else if ok {
		return agentToolOK(action, args, fmt.Sprintf("已存在 %s 工作流 %s，未重复启动。", workflowType, existing.ID), map[string]any{
			"action":        action,
			"workflowRunId": existing.ID,
			"workflowType":  workflowType,
			"status":        existing.Status,
			"targetShotIds": targets,
			"idempotent":    true,
		})
	}
	input["agentTaskId"] = task.ID
	input["agentStepId"] = step.ID
	input["idempotencyKey"] = agentStepIdempotencyKey(task, step)
	run, err := s.startProjectWorkflowCore(r.Context(), principal, project, workflowType, input, workflowFunc)
	if err != nil {
		return agentToolError(action, args, err)
	}
	if err := s.markShotProductionQueued(r, action, run.ID, targets); err != nil {
		return agentToolError(action, args, err)
	}
	return agentToolOK(action, args, fmt.Sprintf("已启动 %s，目标镜头 %d 个。", workflowType, len(targets)), map[string]any{
		"action":        action,
		"workflowRunId": run.ID,
		"workflowType":  workflowType,
		"status":        run.Status,
		"targetShotIds": targets,
	})
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func workflowRunIDFromValue(value any) string {
	switch typed := value.(type) {
	case WorkflowRun:
		return typed.ID
	case *WorkflowRun:
		if typed == nil {
			return ""
		}
		return typed.ID
	case map[string]any:
		return stringValueFromAny(typed["id"])
	case map[string]string:
		return strings.TrimSpace(typed["id"])
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(encoded, &payload); err != nil {
			return ""
		}
		return strings.TrimSpace(payload.ID)
	}
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := stringValueFromAny(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out []string
		if err := json.Unmarshal(encoded, &out); err != nil {
			return nil
		}
		return stringSliceFromAny(out)
	}
}

func agentToolPromptOptions(task AgentTask, step AgentStep) scriptAgentPromptOptions {
	return scriptAgentPromptOptions{
		AgentType:      "project_agent",
		TaskID:         task.ID,
		StepID:         step.ID,
		IdempotencyKey: agentStepIdempotencyKey(task, step),
	}
}

func agentStepMetadata(task AgentTask, step AgentStep, toolName string) map[string]any {
	return map[string]any{
		"agentTool":      toolName,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"agentToolName":  step.ToolName,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	}
}

func mergeAgentStepMetadata(input map[string]any, task AgentTask, step AgentStep, toolName string) map[string]any {
	out := cloneMap(input)
	for key, value := range agentStepMetadata(task, step, toolName) {
		out[key] = value
	}
	if len(input) > 0 {
		out["inputMetadata"] = cloneMap(input)
	}
	return out
}

func agentStepIdempotencyKey(task AgentTask, step AgentStep) string {
	return "agent-step:" + task.ID + ":" + step.ID + ":" + step.ToolName
}

func agentJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return fmt.Sprint(left) == fmt.Sprint(right)
	}
	return string(leftJSON) == string(rightJSON)
}

func (s *Server) agentToolRunReview(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	useAgent := false
	if value, exists := agentBoolArg(args, "includeAgent"); exists {
		useAgent = value
	}
	if value, exists := agentBoolArg(args, "useAgent"); exists {
		useAgent = value
	}
	var includeDeterministic *bool
	if value, exists := agentBoolArg(args, "includeDeterministicChecks"); exists {
		includeDeterministic = &value
	}
	response, err := s.runProjectReviewCore(r.Context(), principal, project, runProjectReviewRequest{
		ReviewType:                 agentStringArg(args, "reviewType"),
		UseAgent:                   useAgent,
		IncludeDeterministicChecks: includeDeterministic,
	})
	if err != nil {
		return agentToolError("review.run", args, err)
	}
	return agentToolOK("review.run", args, fmt.Sprintf("审阅已完成，生成 %d 个问题。", response.ItemCount), map[string]any{
		"reviewRunId": response.ReviewRunID,
		"status":      response.Status,
		"summary":     rawObject(response.Summary),
		"itemCount":   response.ItemCount,
		"useAgent":    useAgent,
	})
}

func (s *Server) agentToolGenerateReviewFix(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	itemID := agentReferenceStringArg(args, "itemId")
	if itemID == "" {
		return agentToolError("review.generate_fix", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "itemId is required"))
	}
	fix, err := s.generateReviewFixCore(r.Context(), principal, project, itemID, generateReviewFixRequest{
		Mode:        firstNonEmpty(agentStringArg(args, "mode"), "deterministic"),
		Instruction: agentStringArg(args, "instruction"),
	})
	if err != nil {
		return agentToolError("review.generate_fix", args, err)
	}
	return agentToolOK("review.generate_fix", args, "已生成修复草稿，等待用户确认后才能应用。", map[string]any{
		"fix":               fix,
		"reviewFixId":       fix.ID,
		"reviewItemId":      fix.ReviewItemID,
		"status":            fix.Status,
		"fixType":           fix.FixType,
		"targetEntityType":  fix.TargetEntityType,
		"targetEntityId":    stringPtrValue(fix.TargetEntityID),
		"beforeSnapshot":    rawObject(fix.BeforeSnapshot),
		"patch":             rawObject(fix.Patch),
		"afterPreview":      rawObject(fix.AfterPreview),
		"regenerateRequest": rawObject(fix.RegenerateRequest),
		"providerCallId":    stringPtrValue(fix.ProviderCallID),
		"promptVersionId":   stringPtrValue(fix.PromptVersionID),
		"promptHash":        stringPtrValue(fix.PromptHash),
	})
}

func (s *Server) agentToolApplyReviewFix(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	fixID := agentReferenceStringArg(args, "fixId")
	if fixID == "" {
		return agentToolError("review.apply_fix", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "fixId is required"))
	}
	resolveReviewItem := true
	if value, exists := agentBoolArg(args, "resolveReviewItem"); exists {
		resolveReviewItem = value
	}
	triggerRegeneration := false
	if value, exists := agentBoolArg(args, "triggerRegeneration"); exists {
		triggerRegeneration = value
	}
	var existingStatus string
	var existingRegenerateRequest json.RawMessage
	if err := s.db.QueryRow(r.Context(), `
		SELECT status, COALESCE(regenerate_request, 'null'::jsonb)
		FROM review_fixes
		WHERE project_id = $1 AND id = $2
	`, project.ID, fixID).Scan(&existingStatus, &existingRegenerateRequest); err != nil {
		return agentToolError("review.apply_fix", args, err)
	}
	if existingStatus == "applied" {
		return agentToolOK("review.apply_fix", args, "审阅修复已应用，未重复写入。", map[string]any{
			"fixId":               fixID,
			"status":              existingStatus,
			"resolveReviewItem":   resolveReviewItem,
			"regenerateRequest":   rawObject(existingRegenerateRequest),
			"triggerRegeneration": triggerRegeneration,
			"idempotent":          true,
			"idempotencyKey":      agentStepIdempotencyKey(task, step),
		})
	}
	response, regenerateRequest, err := s.applyReviewFixCore(r.Context(), principal, project, fixID, applyReviewFixRequest{
		ResolveReviewItem:   resolveReviewItem,
		TriggerRegeneration: triggerRegeneration,
	})
	if err != nil {
		return agentToolError("review.apply_fix", args, err)
	}
	data := map[string]any{
		"fixId":               response.FixID,
		"status":              response.Status,
		"reviewItemStatus":    stringPtrValue(response.ReviewItemStatus),
		"resolveReviewItem":   resolveReviewItem,
		"regenerateRequest":   rawObject(regenerateRequest),
		"triggerRegeneration": triggerRegeneration,
		"idempotencyKey":      agentStepIdempotencyKey(task, step),
	}
	if triggerRegeneration && len(regenerateRequest) > 0 && string(regenerateRequest) != "null" {
		data["note"] = "修复已应用；再生请求需要通过生产工作流工具单独确认后执行。"
	}
	return agentToolOK("review.apply_fix", args, "已应用审阅修复。", data)
}

func (s *Server) agentToolDismissReviewFix(r *http.Request, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	fixID := agentReferenceStringArg(args, "fixId")
	if fixID == "" {
		return agentToolError("review.dismiss_fix", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "fixId is required"))
	}
	var existingStatus string
	if err := s.db.QueryRow(r.Context(), `SELECT status FROM review_fixes WHERE project_id = $1 AND id = $2`, project.ID, fixID).Scan(&existingStatus); err != nil {
		return agentToolError("review.dismiss_fix", args, err)
	}
	if existingStatus == "dismissed" {
		return agentToolOK("review.dismiss_fix", args, "审阅修复已忽略，未重复写入。", map[string]any{
			"fixId":          fixID,
			"status":         existingStatus,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	response, err := s.dismissReviewFixCore(r.Context(), project, fixID)
	if err != nil {
		return agentToolError("review.dismiss_fix", args, err)
	}
	return agentToolOK("review.dismiss_fix", args, "已忽略审阅修复。", map[string]any{
		"fixId":          response.FixID,
		"status":         response.Status,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentToolRenderPromptTest(r *http.Request, project Project, args map[string]any) agentToolResult {
	templateKey := agentStringArg(args, "templateKey")
	if templateKey == "" {
		return agentToolError("prompt.render_test", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "templateKey is required"))
	}
	variables := map[string]any{
		"project": projectPromptVariables(project),
	}
	if input := agentMapArg(args, "input"); len(input) > 0 {
		variables["input"] = input
	}
	for key, value := range agentMapArg(args, "variables") {
		variables[key] = value
	}
	resolved, err := promptsvc.NewService(s.db).Resolve(r.Context(), promptsvc.ResolveRequest{
		OrganizationID: project.OrganizationID,
		ProjectID:      project.ID,
		TemplateKey:    templateKey,
	})
	if err != nil {
		return agentToolError("prompt.render_test", args, err)
	}
	rendered, err := promptsvc.Render(resolved, variables)
	if err != nil {
		return agentToolError("prompt.render_test", args, err)
	}
	return agentToolOK("prompt.render_test", args, "提示词渲染测试已完成。", map[string]any{
		"templateKey":     rendered.TemplateKey,
		"promptVersionId": rendered.PromptVersionID,
		"renderedHash":    rendered.RenderedHash,
		"contentHash":     rendered.ContentHash,
		"promptSource":    rendered.Source,
		"text":            rendered.RenderedText,
	})
}

func (s *Server) agentToolRewriteScriptPreview(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	scriptID := agentReferenceStringArg(args, "scriptId")
	instruction := agentStringArg(args, "instruction")
	if scriptID == "" || instruction == "" {
		return agentToolError("script.rewrite_preview", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId and instruction are required"))
	}
	script, err := s.script(r, project.ID, scriptID)
	if err != nil {
		return agentToolError("script.rewrite_preview", args, err)
	}
	versionID := agentReferenceStringArg(args, "versionId")
	if versionID == "" && script.CurrentVersionID != nil {
		versionID = *script.CurrentVersionID
	}
	current, err := s.scriptVersion(r, project.ID, script.ID, versionID)
	if err != nil {
		return agentToolError("script.rewrite_preview", args, err)
	}
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPromptWithOptions(r, principal, project, nil, "rewrite_preview", "script_agent_rewrite", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": current.ID, "content": current.Content},
		"input":   map[string]any{"instruction": instruction},
	}, agentToolPromptOptions(task, step))
	if err != nil {
		return agentToolError("script.rewrite_preview", args, err)
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = $4, prompt_hash = $5, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(map[string]any{"scriptId": script.ID, "versionId": current.ID, "content": content, "previewOnly": true}), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return agentToolError("script.rewrite_preview", args, err)
	}
	return agentToolOK("script.rewrite_preview", args, "已生成剧本改写预览，未创建新版本。", map[string]any{
		"scriptId":        script.ID,
		"versionId":       current.ID,
		"content":         content,
		"contentFormat":   current.ContentFormat,
		"previewOnly":     true,
		"agentRunId":      runID,
		"providerCallId":  gatewayResp.ProviderCallID,
		"promptVersionId": rendered.PromptVersionID,
		"promptHash":      rendered.RenderedHash,
	})
}

func (s *Server) agentToolGenerateScriptFromSource(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	sourceID := agentReferenceStringArg(args, "sourceId")
	if sourceID == "" {
		if err := s.db.QueryRow(r.Context(), `
			SELECT id::text
			FROM project_sources
			WHERE project_id = $1
			ORDER BY created_at DESC
			LIMIT 1
		`, project.ID).Scan(&sourceID); err != nil {
			return agentToolError("script.generate_from_source", args, err)
		}
	}
	source, err := s.projectSource(r, project.ID, sourceID)
	if err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	title := firstNonEmpty(agentStringArg(args, "title"), source.Title+" Script")
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPromptWithOptions(r, principal, project, nil, "generate_script", "script_agent_generate", map[string]any{
		"project": projectPromptVariables(project),
		"source": map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"sourceType": source.SourceType,
			"content":    source.Content,
		},
		"input": map[string]any{"instruction": agentStringArg(args, "instruction")},
	}, agentToolPromptOptions(task, step))
	if err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	defer tx.Rollback(r.Context())
	script, err := scanScript(tx.QueryRow(r.Context(), scriptInsertSQL(), project.OrganizationID, project.ID, &source.ID, title, "active", principal.UserID))
	if err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, 1, content, "markdown", stringPtrFromValue("agent_generated"), rendered.PromptVersionID, rendered.RenderedHash, json.RawMessage(`{}`), principal.UserID)
	if err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2 WHERE id = $1`, script.ID, version.ID); err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = $4, prompt_hash = $5, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(map[string]any{"scriptId": script.ID, "versionId": version.ID, "content": content}), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	return agentToolOK("script.generate_from_source", args, "已从原文生成剧本。", map[string]any{
		"scriptId":       script.ID,
		"versionId":      version.ID,
		"content":        content,
		"agentRunId":     runID,
		"providerCallId": gatewayResp.ProviderCallID,
		"promptHash":     rendered.RenderedHash,
	})
}

func (s *Server) agentToolRewriteScript(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	scriptID := agentReferenceStringArg(args, "scriptId")
	instruction := agentStringArg(args, "instruction")
	if scriptID == "" || instruction == "" {
		return agentToolError("script.rewrite", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId and instruction are required"))
	}
	script, err := s.script(r, project.ID, scriptID)
	if err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	versionID := agentReferenceStringArg(args, "versionId")
	if versionID == "" && script.CurrentVersionID != nil {
		versionID = *script.CurrentVersionID
	}
	current, err := s.scriptVersion(r, project.ID, script.ID, versionID)
	if err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	content, runID, rendered, gatewayResp, err := s.runScriptAgentPromptWithOptions(r, principal, project, nil, "rewrite_script", "script_agent_rewrite", map[string]any{
		"project": projectPromptVariables(project),
		"script":  map[string]any{"id": script.ID, "versionId": current.ID, "content": current.Content},
		"input":   map[string]any{"instruction": instruction},
	}, agentToolPromptOptions(task, step))
	if err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	defer tx.Rollback(r.Context())
	nextVersion, err := nextScriptVersion(r, tx, script.ID)
	if err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	newVersion, err := insertScriptVersionTx(r, tx, project, script.ID, nextVersion, content, current.ContentFormat, stringPtrFromValue("agent_rewrite"), rendered.PromptVersionID, rendered.RenderedHash, json.RawMessage(`{}`), principal.UserID)
	if err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	activate := false
	if value, exists := agentBoolArg(args, "activate"); exists {
		activate = value
	}
	if activate {
		if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, newVersion.ID); err != nil {
			return agentToolError("script.rewrite", args, err)
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = $4, prompt_hash = $5, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(map[string]any{"scriptId": script.ID, "versionId": newVersion.ID, "content": content}), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("script.rewrite", args, err)
	}
	return agentToolOK("script.rewrite", args, "已改写剧本并创建新版本。", map[string]any{
		"scriptId":       script.ID,
		"versionId":      newVersion.ID,
		"content":        content,
		"activated":      activate,
		"agentRunId":     runID,
		"providerCallId": gatewayResp.ProviderCallID,
		"promptHash":     rendered.RenderedHash,
	})
}

func (s *Server) agentToolCreateScriptVersion(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	scriptID := agentReferenceStringArg(args, "scriptId")
	content := strings.TrimSpace(agentStringArg(args, "content"))
	if scriptID == "" || content == "" {
		return agentToolError("script.create_version", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId and content are required"))
	}
	script, err := s.script(r, project.ID, scriptID)
	if err != nil {
		return agentToolError("script.create_version", args, err)
	}
	contentFormat := firstNonEmpty(agentStringArg(args, "contentFormat"), "markdown")
	if !validScriptContentFormat(contentFormat) {
		return agentToolError("script.create_version", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "contentFormat is invalid"))
	}
	sourceType := stringPtrFromValue(agentStringArg(args, "sourceType"))
	if strings.TrimSpace(stringValue(sourceType)) == "" {
		sourceType = stringPtrFromValue("agent_created")
	}
	var existingVersionID string
	if err := s.db.QueryRow(r.Context(), `
		SELECT id::text
		FROM script_versions
		WHERE project_id = $1 AND script_id = $2 AND metadata->>'agentStepId' = $3
		ORDER BY created_at DESC
		LIMIT 1
	`, project.ID, script.ID, step.ID).Scan(&existingVersionID); err == nil {
		version, err := s.scriptVersion(r, project.ID, script.ID, existingVersionID)
		if err != nil {
			return agentToolError("script.create_version", args, err)
		}
		activate := false
		if value, exists := agentBoolArg(args, "activate"); exists {
			activate = value
		}
		if activate && stringValue(script.CurrentVersionID) != version.ID {
			if _, err := s.db.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
				return agentToolError("script.create_version", args, err)
			}
		}
		return agentToolOK("script.create_version", args, fmt.Sprintf("已存在剧本版本 v%d，未重复创建。", version.Version), map[string]any{
			"scriptId":       script.ID,
			"version":        version,
			"versionId":      version.ID,
			"activated":      activate,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	} else if err != pgx.ErrNoRows {
		return agentToolError("script.create_version", args, err)
	}
	metadata := mustRawJSON(mergeAgentStepMetadata(agentMapArg(args, "metadata"), task, step, "script.create_version"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("script.create_version", args, err)
	}
	defer tx.Rollback(r.Context())
	nextVersion, err := nextScriptVersion(r, tx, script.ID)
	if err != nil {
		return agentToolError("script.create_version", args, err)
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, nextVersion, content, contentFormat, sourceType, "", "", metadata, principal.UserID)
	if err != nil {
		return agentToolError("script.create_version", args, err)
	}
	activate := false
	if value, exists := agentBoolArg(args, "activate"); exists {
		activate = value
	}
	if activate {
		if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
			return agentToolError("script.create_version", args, err)
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("script.create_version", args, err)
	}
	return agentToolOK("script.create_version", args, fmt.Sprintf("已创建剧本版本 v%d。", version.Version), map[string]any{
		"scriptId":       script.ID,
		"version":        version,
		"versionId":      version.ID,
		"activated":      activate,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentToolActivateScriptVersion(r *http.Request, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	scriptID := agentReferenceStringArg(args, "scriptId")
	versionID := agentReferenceStringArg(args, "versionId")
	if scriptID == "" || versionID == "" {
		return agentToolError("script.activate_version", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "scriptId and versionId are required"))
	}
	script, err := s.script(r, project.ID, scriptID)
	if err != nil {
		return agentToolError("script.activate_version", args, err)
	}
	version, err := s.scriptVersion(r, project.ID, script.ID, versionID)
	if err != nil {
		return agentToolError("script.activate_version", args, err)
	}
	if stringValue(script.CurrentVersionID) == version.ID {
		return agentToolOK("script.activate_version", args, fmt.Sprintf("剧本版本 v%d 已是当前激活版本。", version.Version), map[string]any{
			"scriptId":       script.ID,
			"versionId":      version.ID,
			"version":        version.Version,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	if _, err := s.db.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
		return agentToolError("script.activate_version", args, err)
	}
	return agentToolOK("script.activate_version", args, fmt.Sprintf("已激活剧本版本 v%d。", version.Version), map[string]any{
		"scriptId":       script.ID,
		"versionId":      version.ID,
		"version":        version.Version,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentToolUpdateReviewPatchTarget(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any, toolName, entityType, idKey string) agentToolResult {
	entityID := agentReferenceStringArg(args, idKey)
	if entityID == "" {
		return agentToolError(toolName, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", idKey+" is required"))
	}
	patch := agentMapArg(args, "patch")
	if len(patch) == 0 {
		return agentToolError(toolName, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "patch is required"))
	}
	target, err := reviewpkg.LoadReviewFixTarget(r.Context(), s.db, project.ID, entityType, entityID)
	if err != nil {
		return agentToolError(toolName, args, err)
	}
	if err := reviewpkg.ValidateReviewPatch(entityType, patch); err != nil {
		return agentToolError(toolName, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error()))
	}
	before := cloneMap(target.Snapshot)
	if already, err := s.agentPatchTargetHasStep(r.Context(), project.ID, entityType, entityID, step.ID); err != nil {
		return agentToolError(toolName, args, err)
	} else if already {
		return agentToolOK(toolName, args, "目标字段已由当前 Agent 步骤更新，未重复写入。", map[string]any{
			idKey:            entityID,
			"entityType":     entityType,
			"before":         before,
			"patch":          patch,
			"after":          before,
			"idempotent":     true,
			"agentTaskId":    task.ID,
			"agentStepId":    step.ID,
			"agentTool":      toolName,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	after := reviewpkg.ApplyReviewPatchPreview(target.Snapshot, patch)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError(toolName, args, err)
	}
	defer tx.Rollback(r.Context())
	if err := s.applyReviewPatchToTarget(r.Context(), tx, project, entityType, entityID, after, principal.UserID); err != nil {
		return agentToolError(toolName, args, err)
	}
	metadata := agentStepMetadata(task, step, toolName)
	if err := s.mergeAgentPatchTargetMetadata(r.Context(), tx, project.ID, entityType, entityID, metadata); err != nil {
		return agentToolError(toolName, args, err)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "agent.target.updated", entityType, entityID, mustRawJSON(map[string]any{
		"tool":           toolName,
		"entityType":     entityType,
		"entityId":       entityID,
		"patch":          patch,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})); err != nil {
		return agentToolError(toolName, args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError(toolName, args, err)
	}
	return agentToolOK(toolName, args, "已更新目标字段，并标记相关下游内容需要重新检查或生成。", map[string]any{
		idKey:            entityID,
		"entityType":     entityType,
		"before":         before,
		"patch":          patch,
		"after":          after,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentPatchTargetHasStep(ctx context.Context, projectID, entityType, entityID, stepID string) (bool, error) {
	table, err := agentPatchTargetMetadataTable(entityType)
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM `+table+`
			WHERE project_id = $1 AND id = $2 AND metadata->>'agentStepId' = $3
		)
	`, projectID, entityID, stepID).Scan(&ok)
	return ok, err
}

func (s *Server) mergeAgentPatchTargetMetadata(ctx context.Context, tx pgx.Tx, projectID, entityType, entityID string, metadata map[string]any) error {
	table, err := agentPatchTargetMetadataTable(entityType)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE `+table+`
		SET metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, projectID, entityID, mustMarshal(metadata))
	return err
}

func agentPatchTargetMetadataTable(entityType string) (string, error) {
	switch entityType {
	case "script_scene":
		return "script_scenes", nil
	case "canonical_asset":
		return "canonical_assets", nil
	case "storyboard_shot":
		return "storyboard_shots", nil
	case "shot_asset_requirement":
		return "shot_asset_requirements", nil
	case "timeline_clip":
		return "timeline_clips", nil
	case "project_timeline":
		return "project_timelines", nil
	default:
		return "", reviewpkg.ErrUnsupportedFixTarget
	}
}

func (s *Server) agentToolReorderStoryboard(r *http.Request, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	shotIDs := agentReferenceStringSliceArg(args, "shotIds")
	if len(shotIDs) == 0 {
		return agentToolError("storyboard.reorder", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotIds is required"))
	}
	seen := map[string]bool{}
	before := make([]map[string]any, 0, len(shotIDs))
	workflowRunID := ""
	for _, shotID := range shotIDs {
		if seen[shotID] {
			return agentToolError("storyboard.reorder", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "shotIds contains duplicate values"))
		}
		seen[shotID] = true
		var row struct {
			ID            string
			WorkflowRunID string
			ShotIndex     int
			ShotNo        int
			Visual        string
		}
		if err := s.db.QueryRow(r.Context(), `
			SELECT id::text, COALESCE(workflow_run_id::text, ''), shot_index, shot_no, COALESCE(visual, '')
			FROM storyboard_shots
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, shotID).Scan(&row.ID, &row.WorkflowRunID, &row.ShotIndex, &row.ShotNo, &row.Visual); err != nil {
			return agentToolError("storyboard.reorder", args, err)
		}
		if workflowRunID == "" {
			workflowRunID = row.WorkflowRunID
		} else if workflowRunID != row.WorkflowRunID {
			return agentToolError("storyboard.reorder", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "all shots must belong to the same workflow"))
		}
		before = append(before, map[string]any{
			"shotId":        row.ID,
			"workflowRunId": row.WorkflowRunID,
			"shotIndex":     row.ShotIndex,
			"shotNo":        row.ShotNo,
			"visual":        row.Visual,
		})
	}
	var alreadyProcessed int
	if err := s.db.QueryRow(r.Context(), `
		SELECT count(*)
		FROM storyboard_shots
		WHERE project_id = $1 AND id = ANY($2::uuid[]) AND metadata->>'agentStepId' = $3
	`, project.ID, shotIDs, step.ID).Scan(&alreadyProcessed); err != nil {
		return agentToolError("storyboard.reorder", args, err)
	}
	if alreadyProcessed == len(shotIDs) {
		return agentToolOK("storyboard.reorder", args, fmt.Sprintf("已由当前 Agent 步骤重排 %d 个分镜镜头，未重复写入。", len(shotIDs)), map[string]any{
			"workflowRunId":  workflowRunID,
			"shotIds":        shotIDs,
			"before":         before,
			"after":          before,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("storyboard.reorder", args, err)
	}
	defer tx.Rollback(r.Context())
	metadata := agentStepMetadata(task, step, "storyboard.reorder")
	for index, shotID := range shotIDs {
		if _, err := tx.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET shot_index = $3, shot_no = $3, updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, shotID, -1000000-index); err != nil {
			return agentToolError("storyboard.reorder", args, err)
		}
	}
	after := make([]map[string]any, 0, len(shotIDs))
	for index, shotID := range shotIDs {
		shotNo := index + 1
		if _, err := tx.Exec(r.Context(), `
			UPDATE storyboard_shots
			SET shot_index = $3,
			    shot_no = $4,
			    manual_override = true,
			    metadata = COALESCE(metadata, '{}'::jsonb) || $5::jsonb,
			    updated_at = now()
			WHERE project_id = $1 AND id = $2 AND deleted_at IS NULL
		`, project.ID, shotID, index, shotNo, mustMarshal(metadata)); err != nil {
			return agentToolError("storyboard.reorder", args, err)
		}
		after = append(after, map[string]any{
			"shotId":    shotID,
			"shotIndex": index,
			"shotNo":    shotNo,
		})
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "agent.storyboard.reordered", "storyboard_shot", shotIDs[0], mustRawJSON(map[string]any{
		"shotIds":        shotIDs,
		"workflowRunId":  workflowRunID,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})); err != nil {
		return agentToolError("storyboard.reorder", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("storyboard.reorder", args, err)
	}
	return agentToolOK("storyboard.reorder", args, fmt.Sprintf("已重排 %d 个分镜镜头。", len(shotIDs)), map[string]any{
		"workflowRunId":  workflowRunID,
		"shotIds":        shotIDs,
		"before":         before,
		"after":          after,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentToolActivateFinalVideo(r *http.Request, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	versionID := firstNonEmpty(agentReferenceStringArg(args, "finalVideoId"), agentReferenceStringArg(args, "versionId"))
	if versionID == "" {
		return agentToolError("final_video.activate", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "finalVideoId is required"))
	}
	var activeVersionID string
	if err := s.db.QueryRow(r.Context(), `SELECT COALESCE(active_final_video_version_id::text, '') FROM projects WHERE id = $1`, project.ID).Scan(&activeVersionID); err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	if activeVersionID == versionID {
		item, err := s.finalVideoVersionByID(r, project.ID, versionID)
		if err != nil {
			return agentToolError("final_video.activate", args, err)
		}
		return agentToolOK("final_video.activate", args, "成片版本已是当前激活版本。", map[string]any{
			"finalVideo":     item,
			"versionId":      item.ID,
			"status":         item.Status,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'ready' WHERE project_id = $1 AND status = 'active' AND id <> $2`, project.ID, versionID); err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	tag, err := tx.Exec(r.Context(), `UPDATE final_video_versions SET status = 'active' WHERE project_id = $1 AND id = $2`, project.ID, versionID)
	if err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	if tag.RowsAffected() == 0 {
		return agentToolError("final_video.activate", args, pgx.ErrNoRows)
	}
	if _, err := tx.Exec(r.Context(), `UPDATE projects SET active_final_video_version_id = $2 WHERE id = $1`, project.ID, versionID); err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE final_video_versions
		SET metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb
		WHERE project_id = $1 AND id = $2
	`, project.ID, versionID, mustMarshal(agentStepMetadata(task, step, "final_video.activate"))); err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	if err := insertAPIEvent(r.Context(), tx, project.OrganizationID, project.ID, "final_video.activated", "final_video_version", versionID, mustRawJSON(map[string]any{
		"finalVideoVersionId": versionID,
		"agentTaskId":         task.ID,
		"agentStepId":         step.ID,
		"idempotencyKey":      agentStepIdempotencyKey(task, step),
	})); err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	item, err := s.finalVideoVersionByID(r, project.ID, versionID)
	if err != nil {
		return agentToolError("final_video.activate", args, err)
	}
	return agentToolOK("final_video.activate", args, "已激活成片版本。", map[string]any{
		"finalVideo":     item,
		"versionId":      item.ID,
		"status":         item.Status,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentToolCreatePromptVersion(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	templateID := agentReferenceStringArg(args, "templateId")
	content := strings.TrimSpace(agentStringArg(args, "content"))
	if templateID == "" || content == "" {
		return agentToolError("prompt.create_version", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "templateId and content are required"))
	}
	template, err := s.promptTemplate(r.Context(), templateID)
	if err != nil {
		return agentToolError("prompt.create_version", args, err)
	}
	if !promptTemplateBelongsToProjectOrg(template, project) {
		return agentToolError("prompt.create_version", args, auth.ErrForbidden)
	}
	contentFormat := firstNonEmpty(agentStringArg(args, "contentFormat"), "text")
	if contentFormat != "text" && contentFormat != "markdown" {
		return agentToolError("prompt.create_version", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "contentFormat is invalid"))
	}
	title := agentStringArg(args, "title")
	var titlePtr *string
	if title != "" {
		titlePtr = &title
	}
	variablesSchema := mustRawJSON(agentMapArg(args, "variablesSchema"))
	metadata := mustRawJSON(mergeAgentStepMetadata(agentMapArg(args, "metadata"), task, step, "prompt.create_version"))
	activate := false
	if value, exists := agentBoolArg(args, "activate"); exists {
		activate = value
	}
	var existingVersionID string
	if err := s.db.QueryRow(r.Context(), `
		SELECT id::text
		FROM prompt_versions
		WHERE COALESCE(template_id, prompt_template_id) = $1
		  AND metadata->>'agentStepId' = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, template.ID, step.ID).Scan(&existingVersionID); err == nil {
		if activate {
			tx, err := s.db.Begin(r.Context())
			if err != nil {
				return agentToolError("prompt.create_version", args, err)
			}
			defer tx.Rollback(r.Context())
			if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'archived' WHERE template_id = $1 AND status = 'active' AND id <> $2`, template.ID, existingVersionID); err != nil {
				return agentToolError("prompt.create_version", args, err)
			}
			if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'active', activated_at = COALESCE(activated_at, now()) WHERE id = $1`, existingVersionID); err != nil {
				return agentToolError("prompt.create_version", args, err)
			}
			if err := tx.Commit(r.Context()); err != nil {
				return agentToolError("prompt.create_version", args, err)
			}
		}
		version, err := s.promptVersion(r.Context(), existingVersionID)
		if err != nil {
			return agentToolError("prompt.create_version", args, err)
		}
		return agentToolOK("prompt.create_version", args, fmt.Sprintf("已存在提示词版本 v%d，未重复创建。", version.Version), map[string]any{
			"templateId":     template.ID,
			"version":        version,
			"versionId":      version.ID,
			"activated":      activate,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	} else if err != pgx.ErrNoRows {
		return agentToolError("prompt.create_version", args, err)
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("prompt.create_version", args, err)
	}
	defer tx.Rollback(r.Context())
	var versionNo int
	if err := tx.QueryRow(r.Context(), `
		SELECT COALESCE(MAX(COALESCE(version, version_no)), 0) + 1
		FROM prompt_versions
		WHERE COALESCE(template_id, prompt_template_id) = $1
	`, template.ID).Scan(&versionNo); err != nil {
		return agentToolError("prompt.create_version", args, err)
	}
	status := "draft"
	activatedAtExpr := "NULL"
	if activate {
		status = "active"
		activatedAtExpr = "now()"
		if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'archived' WHERE template_id = $1 AND status = 'active'`, template.ID); err != nil {
			return agentToolError("prompt.create_version", args, err)
		}
	}
	var versionID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO prompt_versions(
			prompt_template_id, template_id, version_no, version, status, title, content, content_format,
			variables_schema, metadata, content_hash, created_by, activated_at
		)
		VALUES ($1, $1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, `+activatedAtExpr+`)
		RETURNING id::text
	`, template.ID, versionNo, status, titlePtr, content, contentFormat, variablesSchema, metadata, promptsvc.HashText(content), principal.UserID).Scan(&versionID); err != nil {
		return agentToolError("prompt.create_version", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("prompt.create_version", args, err)
	}
	version, err := s.promptVersion(r.Context(), versionID)
	if err != nil {
		return agentToolError("prompt.create_version", args, err)
	}
	return agentToolOK("prompt.create_version", args, fmt.Sprintf("已创建提示词版本 v%d。", version.Version), map[string]any{
		"templateId":     template.ID,
		"version":        version,
		"versionId":      version.ID,
		"activated":      activate,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func (s *Server) agentToolActivatePromptVersion(r *http.Request, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	versionID := agentReferenceStringArg(args, "versionId")
	if versionID == "" {
		return agentToolError("prompt.activate_version", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "versionId is required"))
	}
	version, err := s.promptVersion(r.Context(), versionID)
	if err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	template, err := s.promptTemplate(r.Context(), version.TemplateID)
	if err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	if !promptTemplateBelongsToProjectOrg(template, project) {
		return agentToolError("prompt.activate_version", args, auth.ErrForbidden)
	}
	if version.Status == "active" {
		return agentToolOK("prompt.activate_version", args, fmt.Sprintf("提示词版本 v%d 已是激活版本。", version.Version), map[string]any{
			"templateId":     template.ID,
			"version":        version,
			"versionId":      version.ID,
			"status":         version.Status,
			"idempotent":     true,
			"idempotencyKey": agentStepIdempotencyKey(task, step),
		})
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE prompt_versions SET status = 'archived' WHERE template_id = $1 AND status = 'active'`, template.ID); err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE prompt_versions
		SET status = 'active',
		    activated_at = now(),
		    metadata = COALESCE(metadata, '{}'::jsonb) || $2::jsonb
		WHERE id = $1
	`, version.ID, mustMarshal(agentStepMetadata(task, step, "prompt.activate_version"))); err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	updated, err := s.promptVersion(r.Context(), version.ID)
	if err != nil {
		return agentToolError("prompt.activate_version", args, err)
	}
	return agentToolOK("prompt.activate_version", args, fmt.Sprintf("已激活提示词版本 v%d。", updated.Version), map[string]any{
		"templateId":     template.ID,
		"version":        updated,
		"versionId":      updated.ID,
		"status":         updated.Status,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
	})
}

func promptTemplateBelongsToProjectOrg(template PromptTemplate, project Project) bool {
	return template.OrganizationID == nil || strings.TrimSpace(*template.OrganizationID) == project.OrganizationID
}

func (s *Server) agentToolTestProviderModel(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	if s.providers == nil {
		return agentToolError("provider.test_model", args, apiError{Status: http.StatusServiceUnavailable, Code: "PROVIDER_SERVICE_UNAVAILABLE", Message: "provider service is not configured", Retryable: true})
	}
	modelID := agentReferenceStringArg(args, "modelId")
	if modelID == "" {
		return agentToolError("provider.test_model", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "modelId is required"))
	}
	testType := firstNonEmpty(agentStringArg(args, "testType"), "text_generation_test")
	input := agentMapArg(args, "input")
	if len(input) == 0 {
		prompt := firstNonEmpty(agentStringArg(args, "prompt"), "Say ok.")
		input = map[string]any{
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		}
	}
	result, err := s.providers.RecordProviderModelTest(r.Context(), project.OrganizationID, principal.UserID, modelID, provider.TestProviderModelRequest{
		TestType:       testType,
		Input:          mustRawJSON(input),
		IdempotencyKey: agentStepIdempotencyKey(task, step),
	})
	if err != nil {
		return agentToolError("provider.test_model", args, err)
	}
	return agentToolOK("provider.test_model", args, "供应商模型测试已完成。", map[string]any{
		"result":         result,
		"testRunId":      result.TestRunID,
		"providerCallId": result.ProviderCallID,
		"status":         result.Status,
		"latencyMs":      result.LatencyMS,
	})
}

func (s *Server) agentToolUpdateProviderAccount(r *http.Request, project Project, args map[string]any) agentToolResult {
	if s.providers == nil {
		return agentToolError("provider.update_account", args, apiError{Status: http.StatusServiceUnavailable, Code: "PROVIDER_SERVICE_UNAVAILABLE", Message: "provider service is not configured", Retryable: true})
	}
	accountID := agentReferenceStringArg(args, "accountId")
	if accountID == "" {
		return agentToolError("provider.update_account", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "accountId is required"))
	}
	patch := agentMapArg(args, "patch")
	req := provider.UpdateAccountRequest{}
	if value, ok := optionalStringPatch(patch, "name"); ok {
		req.Name = &value
	}
	if value, ok := optionalStringPatch(patch, "baseUrl"); ok {
		req.BaseURL = &value
	}
	if value, ok := optionalStringPatch(patch, "authType"); ok {
		req.AuthType = &value
	}
	if value, ok := optionalStringPatch(patch, "status"); ok {
		req.Status = &value
	}
	if config := mapPatch(patch, "config"); len(config) > 0 {
		req.Config = mustRawJSON(config)
	}
	item, err := s.providers.UpdateAccount(r.Context(), project.OrganizationID, accountID, req)
	if err != nil {
		return agentToolError("provider.update_account", args, err)
	}
	return agentToolOK("provider.update_account", args, "已更新供应商账号。", map[string]any{"account": item, "accountId": item.ID})
}

func (s *Server) agentToolUpdateProviderModel(r *http.Request, project Project, args map[string]any) agentToolResult {
	if s.providers == nil {
		return agentToolError("provider.update_model", args, apiError{Status: http.StatusServiceUnavailable, Code: "PROVIDER_SERVICE_UNAVAILABLE", Message: "provider service is not configured", Retryable: true})
	}
	modelID := agentReferenceStringArg(args, "modelId")
	if modelID == "" {
		return agentToolError("provider.update_model", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "modelId is required"))
	}
	patch := agentMapArg(args, "patch")
	req := provider.UpdateModelRequest{}
	if value, ok := optionalStringPatch(patch, "modelKey"); ok {
		req.ModelKey = &value
	}
	if value, ok := optionalStringPatch(patch, "displayName"); ok {
		req.DisplayName = &value
	}
	if value, ok := optionalStringPatch(patch, "modality"); ok {
		req.Modality = &value
	}
	if value, ok := optionalStringPatch(patch, "status"); ok {
		req.Status = &value
	}
	if value, ok := patch["capabilities"]; ok && value != nil {
		var capabilities provider.CapabilityInput
		if err := json.Unmarshal(mustMarshal(value), &capabilities); err != nil {
			return agentToolError("provider.update_model", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "capabilities is invalid"))
		}
		req.Capabilities = &capabilities
	}
	item, err := s.providers.UpdateModel(r.Context(), project.OrganizationID, modelID, req)
	if err != nil {
		return agentToolError("provider.update_model", args, err)
	}
	return agentToolOK("provider.update_model", args, "已更新供应商模型。", map[string]any{"model": item, "modelId": item.ID})
}

func (s *Server) agentToolInstallProviderCatalogPreset(r *http.Request, principal auth.Principal, project Project, args map[string]any) agentToolResult {
	if s.providers == nil {
		return agentToolError("provider.install_catalog_preset", args, apiError{Status: http.StatusServiceUnavailable, Code: "PROVIDER_SERVICE_UNAVAILABLE", Message: "provider service is not configured", Retryable: true})
	}
	providerKey := agentStringArg(args, "providerKey")
	if providerKey == "" {
		return agentToolError("provider.install_catalog_preset", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "providerKey is required"))
	}
	var req provider.InstallCatalogRequest
	if err := json.Unmarshal(mustMarshal(args), &req); err != nil {
		return agentToolError("provider.install_catalog_preset", args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "install request is invalid"))
	}
	req.OrganizationID = project.OrganizationID
	item, err := s.providers.InstallCatalogEntry(r.Context(), project.OrganizationID, principal.UserID, providerKey, req)
	if err != nil {
		return agentToolError("provider.install_catalog_preset", args, err)
	}
	return agentToolOK("provider.install_catalog_preset", args, "已安装供应商预设。", map[string]any{
		"result":    item,
		"accountId": item.Account.ID,
	})
}

func optionalStringPatch(patch map[string]any, key string) (string, bool) {
	value, ok := patch[key]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(fmt.Sprint(value)), true
}

func mapPatch(patch map[string]any, key string) map[string]any {
	value, ok := patch[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
