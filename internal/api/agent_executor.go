package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/agent"
	"github.com/Einzieg/cineweave/internal/auth"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	projectevents "github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	reviewpkg "github.com/Einzieg/cineweave/internal/review"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
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
	registry, err := s.projectAgentRegistry(project)
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
			failedRuns, err := s.agentTaskFailedWorkflowRuns(r.Context(), project.ID, task.ID)
			if err != nil {
				return AgentTask{}, err
			}
			if len(failedRuns) > 0 {
				appendedRecovery, err := s.appendAgentWorkflowRecoveryPlan(r, principal, project, task.ID, failedRuns)
				if err != nil {
					return AgentTask{}, err
				}
				if appendedRecovery {
					continue
				}
				return s.finishAgentTaskFailedWorkflows(r.Context(), project, task.ID, failedRuns)
			}
			appendedQuestionContinuation, err := s.appendAgentQuestionContinuationPlan(r, principal, project, task.ID)
			if err != nil {
				return AgentTask{}, err
			}
			if appendedQuestionContinuation {
				continue
			}
			derivationGoalSatisfied, err := s.agentTaskCommerceScriptDerivationGoalSatisfied(
				r.Context(), project.ID, task,
			)
			if err != nil {
				return AgentTask{}, err
			}
			if derivationGoalSatisfied {
				return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "succeeded", "", "")
			}
			appended, complete, stopped, err := s.appendAgentRuntimeNextAction(r, principal, project, task.ID)
			if err != nil {
				return AgentTask{}, err
			}
			if stopped != nil {
				return *stopped, nil
			}
			if appended {
				continue
			}
			if !complete {
				return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "failed", "AGENT_RUNTIME_INCOMPLETE", "助手未能确定下一步动作。")
			}
			currentTask, err := s.agentTask(r, project.ID, task.ID)
			if err != nil {
				return AgentTask{}, err
			}
			appendedGoalEffect, stopped, err := s.appendAgentGoalEffectPlan(r, principal, project, currentTask)
			if err != nil {
				return AgentTask{}, err
			}
			if stopped != nil {
				return *stopped, nil
			}
			if appendedGoalEffect {
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
		failedRuns, err := s.agentTaskFailedWorkflowRuns(r.Context(), project.ID, task.ID)
		if err != nil {
			return AgentTask{}, err
		}
		if len(failedRuns) > 0 {
			appendedRecovery, err := s.appendAgentWorkflowRecoveryPlan(r, principal, project, task.ID, failedRuns)
			if err != nil {
				return AgentTask{}, err
			}
			if appendedRecovery {
				continue
			}
			return s.finishAgentTaskFailedWorkflows(r.Context(), project, task.ID, failedRuns)
		}
		result, execErr := s.executeAgentStep(r, principal, project, task, *next, registry)
		if execErr != nil {
			return AgentTask{}, execErr
		}
		if result.Status == "failed" {
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "failed", firstNonEmpty(result.ErrorCode, "AGENT_STEP_FAILED"), result.ErrorMessage)
		}
		if result.Status == "running" {
			if _, err := s.persistAgentRuntimeSnapshot(r.Context(), project, task.ID); err != nil {
				return AgentTask{}, err
			}
			return s.finishAgentTaskState(r.Context(), project.ID, task.ID, "running", "", "")
		}
		if _, err := s.persistAgentRuntimeSnapshot(r.Context(), project, task.ID); err != nil {
			return AgentTask{}, err
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

type agentQuestionContinuation struct {
	StepIndex int
	Answer    string
	NextGoal  string
}

func (s *Server) appendAgentGoalEffectPlan(r *http.Request, principal auth.Principal, project Project, task AgentTask) (bool, *AgentTask, error) {
	if !agentGoalRequiresWriteEffect(task.UserGoal) {
		return false, nil, nil
	}
	var succeededEffects int
	if err := s.db.QueryRow(r.Context(), `
		SELECT count(*)
		FROM agent_steps
		WHERE task_id = $1
		  AND status = 'succeeded'
		  AND risk IN ('write', 'destructive', 'workflow', 'cost', 'admin')
	`, task.ID).Scan(&succeededEffects); err != nil {
		return false, nil, err
	}
	if succeededEffects > 0 {
		return false, nil, nil
	}
	if !agentGoalRequestsProductionContentClear(task.UserGoal) {
		stopped, err := s.blockAgentGoalWithoutEffect(r.Context(), project, task.ID)
		return false, &stopped, err
	}
	var existingClearSteps int
	if err := s.db.QueryRow(r.Context(), `
		SELECT count(*)
		FROM agent_steps
		WHERE task_id = $1 AND tool_name = 'project.clear_production_content'
	`, task.ID).Scan(&existingClearSteps); err != nil {
		return false, nil, err
	}
	if existingClearSteps > 0 {
		stopped, err := s.blockAgentGoalWithoutEffect(r.Context(), project, task.ID)
		return false, &stopped, err
	}
	registry, err := s.projectAgentRegistry(project)
	if err != nil {
		return false, nil, err
	}
	plan := agent.Plan{
		Summary: "已完成清理范围核查，开始保留小说原文并清空其余生产内容。",
		Steps: []agent.PlanStep{{
			Tool: "project.clear_production_content",
			Args: mustRawJSON(map[string]any{
				"confirmation": "preserve_novel_sources",
				"reason":       task.UserGoal,
			}),
			ExpectedResult: "保留小说原文及分卷分集，切换到新的空白生产代并清空其余生产内容",
		}},
	}
	validated, err := agent.ValidatePlan(plan, registry, agentRuntimeMaxPlanSteps(task))
	if err != nil {
		return false, nil, err
	}
	planInput := map[string]any{
		"goal":        task.UserGoal,
		"plannerMode": "deterministic_goal_effect",
		"reason":      "read_only_plan_did_not_satisfy_mutation_goal",
	}
	promptHash := promptsvc.HashText("agent-goal-effect:" + task.ID + ":" + string(mustMarshal(planInput)))
	var runID string
	if err := s.db.QueryRow(r.Context(), `
		INSERT INTO agent_runs(
			organization_id, project_id, session_id, agent_type, task_type, status,
			input, prompt_hash, task_id, created_by, started_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, 'project_agent', 'goal_effect', 'running', $4, $5, $6, $7, now())
		RETURNING id
	`, project.OrganizationID, project.ID, stringValue(task.SessionID), mustMarshal(planInput), promptHash, task.ID, principal.UserID).Scan(&runID); err != nil {
		return false, nil, err
	}
	if err := s.persistAgentPlanWithSummaryPatch(r, principal, project, task, registry, validated, runID, provider.GatewayTextResponse{}, map[string]any{
		"goalEffectContinuation": true,
	}); err != nil {
		return false, nil, err
	}
	s.insertAgentTaskEvent(r.Context(), project, task.ID, "agent.task.continued", map[string]any{
		"summary":      validated.Summary,
		"appendedPlan": validated,
		"reason":       "goal_effect_required",
	})
	return true, nil, nil
}

func agentGoalRequiresWriteEffect(goal string) bool {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return false
	}
	for _, clause := range agentGoalIntentClauses(goal) {
		if !containsAnyFold(clause, agentWriteIntentVerbs...) || agentGoalClauseNegatesWriteEffect(clause) {
			continue
		}
		interrogative := containsAnyFold(clause, "为什么", "什么原因", "是否", "能否", "能不能", "查看", "检查", "分析", "说明", "吗")
		if !interrogative || agentGoalClauseHasExplicitWriteDirective(clause) {
			return true
		}
	}
	return false
}

var agentWriteIntentVerbs = []string{
	"删除", "清空", "重置", "归档", "覆盖", "替换", "修改", "更新",
	"创建", "生成", "启动", "执行", "取消", "修复",
}

func agentGoalIntentClauses(goal string) []string {
	normalized := strings.NewReplacer(
		"但是", "，",
		"而是", "，",
		"然后", "，",
		"随后", "，",
		"接着", "，",
		"并且", "，",
	).Replace(goal)
	for _, verb := range agentWriteIntentVerbs {
		for _, connector := range []string{"并", "再", "后", "且"} {
			normalized = strings.ReplaceAll(normalized, connector+verb, "，"+verb)
		}
	}
	return strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '，', ',', '。', '.', '；', ';', '！', '!', '？', '?', '\n', '\r':
			return true
		default:
			return false
		}
	})
}

func agentGoalClauseNegatesWriteEffect(clause string) bool {
	firstWrite := len(clause)
	for _, verb := range agentWriteIntentVerbs {
		if index := strings.Index(clause, verb); index >= 0 && index < firstWrite {
			firstWrite = index
		}
	}
	if firstWrite == len(clause) {
		return false
	}
	prefix := clause[:firstWrite]
	return containsAnyFold(prefix, "不要", "无需", "无须", "不需要", "不必", "不得", "禁止", "请勿", "勿", "不允许", "避免", "不能")
}

func agentGoalClauseHasExplicitWriteDirective(clause string) bool {
	for _, verb := range agentWriteIntentVerbs {
		for _, prefix := range []string{"请", "帮我", "立即", "直接", "务必", "马上", "现在"} {
			if containsAnyFold(clause, prefix+verb) {
				return true
			}
		}
	}
	return false
}

func agentGoalRequestsProductionContentClear(goal string) bool {
	return containsAnyFold(goal, "清空", "彻底删除", "移除全部", "删除全部", "重置") &&
		containsAnyFold(goal, "除小说原文外", "保留小说原文", "生产内容", "当前项目")
}

func (s *Server) blockAgentGoalWithoutEffect(ctx context.Context, project Project, taskID string) (AgentTask, error) {
	message := "任务只完成了读取或分析，没有执行用户要求的写入操作，已停止并等待重新规划。"
	patch := map[string]any{
		"summary":                 message,
		"goalEffectMissing":       true,
		"goalEffectRequired":      true,
		"autoContinuationStopped": true,
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET status = 'blocked',
		    error_code = 'AGENT_GOAL_NOT_EFFECTED',
		    error_message = $3,
		    completed_at = NULL,
		    summary = COALESCE(summary, '{}'::jsonb) || $4::jsonb
		WHERE id = $1 AND project_id = $2
	`, taskID, project.ID, message, mustMarshal(patch)); err != nil {
		return AgentTask{}, err
	}
	s.insertAgentTaskEvent(ctx, project, taskID, "agent.task.blocked", patch)
	return s.agentTaskWithDetails(requestWithContext(ctx), project.ID, taskID)
}

func (s *Server) pendingAgentQuestionContinuation(ctx context.Context, taskID string) (agentQuestionContinuation, bool, error) {
	var continuation agentQuestionContinuation
	err := s.db.QueryRow(ctx, `
		SELECT question.step_index,
		       COALESCE(question.output #>> '{data,answer}', ''),
		       COALESCE(question.output #>> '{data,nextGoal}', '')
		FROM agent_steps question
		WHERE question.task_id = $1
		  AND question.tool_name = 'agent.ask_user'
		  AND question.status = 'succeeded'
		  AND COALESCE(question.output #>> '{data,nextGoal}', '') <> ''
		  AND NOT EXISTS (
		    SELECT 1
		    FROM agent_steps later
		    WHERE later.task_id = question.task_id
		      AND later.step_index > question.step_index
		  )
		ORDER BY question.step_index DESC
		LIMIT 1
	`, taskID).Scan(&continuation.StepIndex, &continuation.Answer, &continuation.NextGoal)
	if err == pgx.ErrNoRows {
		return agentQuestionContinuation{}, false, nil
	}
	if err != nil {
		return agentQuestionContinuation{}, false, err
	}
	continuation.Answer = strings.TrimSpace(continuation.Answer)
	continuation.NextGoal = strings.TrimSpace(continuation.NextGoal)
	return continuation, continuation.NextGoal != "", nil
}

func (s *Server) appendAgentQuestionContinuationPlan(r *http.Request, principal auth.Principal, project Project, taskID string) (bool, error) {
	continuation, ok, err := s.pendingAgentQuestionContinuation(r.Context(), taskID)
	if err != nil || !ok {
		return false, err
	}
	task, err := s.agentTask(r, project.ID, taskID)
	if err != nil {
		return false, err
	}
	continuationTask := task
	continuationTask.UserGoal = strings.TrimSpace(fmt.Sprintf(
		"用户原始目标：%s\n用户已经回答：%s\n已确认的下一步：%s\n请直接规划并执行这个已确认的下一步，不要再次询问同一个问题。",
		task.UserGoal,
		firstNonEmpty(continuation.Answer, "已确认"),
		continuation.NextGoal,
	))
	if _, err := s.planAgentTask(r, principal, project, continuationTask); err != nil {
		return false, err
	}
	if err := s.mergeAgentTaskSummaryPatch(r.Context(), project.ID, task.ID, map[string]any{
		"questionContinuation": map[string]any{
			"stepIndex": continuation.StepIndex,
			"answer":    continuation.Answer,
			"nextGoal":  continuation.NextGoal,
		},
	}); err != nil {
		return false, err
	}
	s.insertAgentTaskEvent(r.Context(), project, task.ID, "agent.task.question_continued", map[string]any{
		"stepIndex": continuation.StepIndex,
		"answer":    continuation.Answer,
		"nextGoal":  continuation.NextGoal,
	})
	return true, nil
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
		result := unavailableAgentToolResult(project, step.ToolName)
		return result, s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result)
	}
	if step.ProjectControlCommandID != nil && strings.TrimSpace(*step.ProjectControlCommandID) != "" {
		return s.observeAgentStepProjectControlCommand(r.Context(), project, task, step, tool)
	}
	if err := s.authorizeAgentToolPermissions(r.Context(), principal, project, tool); err != nil {
		result := agentToolError(step.ToolName, map[string]any{}, err)
		result.Label = tool.Label
		return result, s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result)
	}
	stateGate := s.superviseAgentStepState(r, project, task, tool, step.Input)
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
	if descriptor, exists := s.projectControlDescriptorForAgentTool(tool); exists && !descriptor.ReadOnly {
		return s.enqueueAgentStepProjectControlCommand(r.Context(), principal, project, task, step, tool, descriptor)
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
	if result.Status == "succeeded" && tool.EffectiveEffects().StartsWorkflow {
		workflowRunIDs, workflowErr := agentWorkflowRunIDsFromValue(result.Data)
		if workflowErr != nil || len(workflowRunIDs) == 0 {
			result.Status = "failed"
			result.ErrorCode = "AGENT_CHILD_WORKFLOW_CONTRACT_INVALID"
			result.ErrorMessage = "启动型工具没有返回有效的子工作流标识"
			if workflowErr != nil {
				result.ErrorMessage = workflowErr.Error()
			}
			result.Summary = result.ErrorMessage
		} else {
			result.ChildWorkflowRunIDs = workflowRunIDs
		}
	}
	if result.Status != "succeeded" && result.Status != "blocked" && result.ErrorCode == "" {
		result.ErrorCode = "AGENT_TOOL_FAILED"
	}
	if err := s.storeAgentStepResult(r.Context(), project, task.ID, step.ID, result); err != nil {
		return agentToolResult{}, err
	}
	return result, nil
}

func (s *Server) projectControlDescriptorForAgentTool(tool agent.AgentTool) (projectcontrol.Descriptor, bool) {
	if s == nil || s.projectControl == nil || s.projectControl.registry == nil {
		return projectcontrol.Descriptor{}, false
	}
	descriptor, exists := s.projectControl.registry.Get(tool.Name)
	return descriptor, exists
}

func (s *Server) enqueueAgentStepProjectControlCommand(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	task AgentTask,
	step AgentStep,
	tool agent.AgentTool,
	descriptor projectcontrol.Descriptor,
) (agentToolResult, error) {
	if descriptor.ExecutionMode == projectcontrol.ExecutionModeSync {
		command, result, replayed, err := s.projectControl.executeBoundedSyncAction(
			ctx, principal, project, descriptor, projectcontrol.ControllerEmbeddedAgent,
			"", task.ID, step.ID, step.Input, agentStepIdempotencyKey(task, step),
		)
		if err != nil {
			return agentToolResult{}, err
		}
		if result.Data == nil {
			result.Data = map[string]any{}
		}
		result.Data["projectControlCommandId"] = command.ID
		result.Data["commandStatus"] = command.Status
		result.Data["idempotentReplay"] = replayed
		if err := s.storeAgentStepResult(ctx, project, task.ID, step.ID, result); err != nil {
			return agentToolResult{}, err
		}
		return result, nil
	}
	command, replayed, err := s.projectControl.repository.Create(ctx, projectcontrol.CreateCommand{
		OrganizationID: project.OrganizationID,
		WorkspaceID:    project.WorkspaceID,
		ProjectID:      project.ID,
		ActorUserID:    principal.UserID,
		ControllerType: projectcontrol.ControllerEmbeddedAgent,
		AgentTaskID:    task.ID,
		AgentStepID:    step.ID,
		Descriptor:     descriptor,
		Input:          step.Input,
		IdempotencyKey: agentStepIdempotencyKey(task, step),
	})
	if err != nil {
		return agentToolResult{}, err
	}
	step.ProjectControlCommandID = &command.ID
	if command.Terminal() {
		return s.observeAgentStepProjectControlCommand(ctx, project, task, step, tool)
	}
	result := agentToolResult{
		Name:    tool.Name,
		Label:   tool.Label,
		Status:  "running",
		Summary: "已提交到项目控制运行时，正在等待后台执行。",
		Data: map[string]any{
			"projectControlCommandId": command.ID,
			"commandStatus":           command.Status,
			"idempotentReplay":        replayed,
		},
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_steps
		SET status = 'running', output = $3, error_code = NULL, error_message = NULL,
		    started_at = COALESCE(started_at, now()), completed_at = NULL
		WHERE id = $1 AND task_id = $2 AND project_control_command_id = $4
	`, step.ID, task.ID, mustMarshal(result), command.ID); err != nil {
		return agentToolResult{}, err
	}
	s.insertAgentStepEvent(ctx, project, task.ID, step.ID, "agent.step.started", map[string]any{
		"tool": tool.Name, "stepIndex": step.StepIndex, "risk": step.Risk,
		"projectControlCommandId": command.ID, "idempotentReplay": replayed,
	})
	return result, nil
}

func (s *Server) observeAgentStepProjectControlCommand(
	ctx context.Context,
	project Project,
	task AgentTask,
	step AgentStep,
	tool agent.AgentTool,
) (agentToolResult, error) {
	commandID := strings.TrimSpace(stringValue(step.ProjectControlCommandID))
	if commandID == "" {
		return agentToolResult{}, fmt.Errorf("Agent step project-control command ID is missing")
	}
	command, err := s.projectControl.repository.Get(ctx, commandID)
	if err != nil {
		return agentToolResult{}, err
	}
	if command.AgentTaskID != task.ID || command.AgentStepID != step.ID || command.ProjectID != project.ID {
		return agentToolResult{}, fmt.Errorf("project-control command %s does not belong to Agent step", command.ID)
	}
	if !command.Terminal() {
		return agentToolResult{
			Name: tool.Name, Label: tool.Label, Status: "running",
			Summary: "项目控制命令仍在执行。",
			Data: map[string]any{
				"projectControlCommandId": command.ID,
				"commandStatus":           command.Status,
			},
		}, nil
	}
	if command.Status == projectcontrol.CommandSucceeded {
		var result agentToolResult
		if len(command.Output) > 0 && json.Unmarshal(command.Output, &result) == nil && result.Name != "" {
			if err := s.storeAgentStepResult(ctx, project, task.ID, step.ID, result); err != nil {
				return agentToolResult{}, err
			}
			return result, nil
		}
		result = agentToolResult{
			Name: tool.Name, Label: tool.Label, Status: "succeeded",
			Summary: "项目控制命令已完成。",
			Data:    map[string]any{"projectControlCommandId": command.ID, "commandOutput": rawObject(command.Output)},
		}
		if err := s.storeAgentStepResult(ctx, project, task.ID, step.ID, result); err != nil {
			return agentToolResult{}, err
		}
		return result, nil
	}
	if command.Status == projectcontrol.CommandPartialSucceeded {
		result := agentToolResult{
			Name: tool.Name, Label: tool.Label, Status: "succeeded",
			Summary: "项目控制命令部分完成，可在任务活动中重试失败单元。",
			Data: map[string]any{
				"projectControlCommandId": command.ID, "commandStatus": command.Status,
				"commandOutput": rawObject(command.Output),
			},
		}
		if err := s.storeAgentStepResult(ctx, project, task.ID, step.ID, result); err != nil {
			return agentToolResult{}, err
		}
		return result, nil
	}
	code := firstNonEmpty(command.ErrorCode, "PROJECT_CONTROL_COMMAND_FAILED")
	message := firstNonEmpty(command.ErrorMessage, "项目控制命令执行失败")
	if command.Status == projectcontrol.CommandCancelled {
		code = "PROJECT_CONTROL_COMMAND_CANCELLED"
		message = "项目控制命令已取消"
	}
	result := agentToolResult{
		Name: tool.Name, Label: tool.Label, Status: "failed", Summary: message,
		ErrorCode: code, ErrorMessage: message,
		Data: map[string]any{"projectControlCommandId": command.ID, "commandStatus": command.Status},
	}
	if err := s.storeAgentStepResult(ctx, project, task.ID, step.ID, result); err != nil {
		return agentToolResult{}, err
	}
	return result, nil
}

func unavailableAgentToolResult(project Project, toolName string) agentToolResult {
	result := agentToolResult{
		Name:         toolName,
		Label:        toolName,
		Status:       "failed",
		Summary:      "工具不在后端白名单中，未执行。",
		ErrorCode:    "UNKNOWN_TOOL",
		ErrorMessage: "unknown tool",
	}
	if agent.ToolBelongsToDifferentProjectKind(string(project.ProjectKind), toolName) {
		result.Summary = "当前项目类型不支持此工具，未执行任何写入或供应商调用。"
		result.ErrorCode = "PROJECT_KIND_MISMATCH"
		result.ErrorMessage = "project kind does not allow agent tool"
	}
	return result
}

// executeProjectAction is the single action invoker shared by the embedded
// assistant and project-control reads. Writes are deliberately rejected here:
// executeAgentStep must first persist a command and the shared runtime owns the
// mutation. Agent task/step values are execution provenance only.
func (s *Server) executeProjectAction(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, tool agent.AgentTool) agentToolResult {
	args, err := agentStepArgs(step.Input)
	if err != nil {
		return agentToolError(step.ToolName, nil, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "step input must be a JSON object"))
	}
	if err := validateAgentRuntimeArguments(args); err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	if step.ToolName == agentAskUserToolName {
		return s.agentToolAskUser(r, project, task, step, args)
	}
	descriptor, exists := s.projectControlDescriptorForAgentTool(tool)
	if !exists {
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
	if !descriptor.ReadOnly {
		return agentToolResult{
			Name: step.ToolName, Label: tool.Label, Status: "failed",
			Summary:      "写操作必须通过持久项目控制命令执行。",
			ErrorCode:    "PROJECT_CONTROL_COMMAND_REQUIRED",
			ErrorMessage: "write action must execute through a durable project-control command",
		}
	}
	externalInput := make(map[string]any, len(args)+1)
	for key, value := range args {
		externalInput[key] = value
	}
	externalInput["projectId"] = project.ID
	result, err := s.projectControl.Execute(r.Context(), controlmcp.Identity{
		Principal: principal, ControllerType: projectcontrol.ControllerEmbeddedAgent,
	}, step.ToolName, mustRawJSON(externalInput))
	if err != nil {
		return agentToolError(step.ToolName, args, err)
	}
	if result.Error != nil {
		return agentToolResult{
			Name: step.ToolName, Label: tool.Label, Status: "failed",
			Summary: result.Error.UserMessage, ErrorCode: result.Error.Code,
			ErrorMessage: result.Error.UserMessage, Retryable: result.Error.Retryable,
			Data: rawObject(result.Data),
		}
	}
	return agentToolResult{
		Name: step.ToolName, Label: tool.Label, Status: "succeeded",
		Summary: result.Summary, Data: rawObject(result.Data),
		ChildWorkflowRunIDs: append([]string(nil), result.WorkflowRunIDs...),
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
	s.insertAgentStepEvent(ctx, project, taskID, stepID, eventType, map[string]any{
		"tool":         result.Name,
		"status":       result.Status,
		"summary":      result.Summary,
		"errorCode":    result.ErrorCode,
		"errorMessage": result.ErrorMessage,
		"retryable":    result.Retryable,
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
	case "project.clear_production_content":
		generationID := stringValueFromAny(result.Data["activeGenerationId"])
		if generationID == "" {
			return fail("VERIFIER_MISSING_PRODUCTION_GENERATION", "activeGenerationId is missing from tool output")
		}
		var activeGenerationID string
		var remainingScripts, remainingAssets, remainingEvents, remainingPlans, remainingShots, remainingNonNovelSources int
		if err := s.db.QueryRow(ctx, `
			SELECT project.active_video_production_generation_id::text,
			       (SELECT count(*) FROM scripts WHERE project_id = project.id),
			       (SELECT count(*) FROM canonical_assets WHERE project_id = project.id),
			       (SELECT count(*) FROM novel_events WHERE project_id = project.id),
			       (SELECT count(*) FROM adaptation_plans WHERE project_id = project.id),
			       (SELECT count(*) FROM storyboard_shots WHERE project_id = project.id AND deleted_at IS NULL),
			       (SELECT count(*) FROM project_sources WHERE project_id = project.id AND source_type <> 'novel')
			FROM projects project
			WHERE project.id = $1
		`, project.ID).Scan(&activeGenerationID, &remainingScripts, &remainingAssets, &remainingEvents, &remainingPlans, &remainingShots, &remainingNonNovelSources); err != nil {
			return fail("VERIFIER_PROJECT_CLEAR_CHECK_FAILED", err.Error())
		}
		if activeGenerationID != generationID {
			return fail("VERIFIER_PRODUCTION_GENERATION_MISMATCH", "project active generation does not match tool output")
		}
		remaining := remainingScripts + remainingAssets + remainingEvents + remainingPlans + remainingShots + remainingNonNovelSources
		if remaining != 0 {
			return fail("VERIFIER_PROJECT_CLEAR_INCOMPLETE", fmt.Sprintf("%d production records remain after clear", remaining))
		}
		return ok("project production content is empty and the novel source was preserved")
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
	case "shot.generate_image_prompts", "shot.generate_video_prompts", "shot.generate_missing_images", "shot.generate_missing_videos":
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
		if result.Name == "shot.generate_image_prompts" {
			column = "image_prompt_status"
		} else if result.Name == "shot.generate_missing_videos" {
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
		if scriptID == "" || versionID == "" {
			return fail("VERIFIER_MISSING_SCRIPT_VERSION", "scriptId and versionId are required in tool output")
		}
		var current string
		if err := s.db.QueryRow(ctx, `SELECT current_version_id::text FROM scripts WHERE project_id = $1 AND id = $2`, project.ID, scriptID).Scan(&current); err != nil {
			return fail("VERIFIER_SCRIPT_NOT_FOUND", err.Error())
		}
		if current != versionID {
			return fail("VERIFIER_SCRIPT_VERSION_NOT_ACTIVE", "script current version does not match tool output")
		}
		return ok("script version is active")
	case "script.generate_from_source":
		versionID := firstNonEmpty(stringValueFromAny(result.Data["versionId"]), stringValueFromAny(result.Data["scriptVersionId"]))
		if versionID == "" {
			runID := stringValueFromAny(result.Data["workflowRunId"])
			if runID == "" {
				return fail("VERIFIER_MISSING_WORKFLOW_RUN", "workflowRunId is required for async script generation verification")
			}
			var workflowStatus string
			var workflowOutput json.RawMessage
			var workflowErrorMessage *string
			if err := s.db.QueryRow(ctx, `
				SELECT status, output, error_message
				FROM workflow_runs
				WHERE project_id = $1 AND id = $2
			`, project.ID, runID).Scan(&workflowStatus, &workflowOutput, &workflowErrorMessage); err != nil {
				return fail("VERIFIER_WORKFLOW_NOT_FOUND", err.Error())
			}
			switch workflowStatus {
			case "queued", "running", "cancelling":
				return ok("source_to_script workflow exists with status " + workflowStatus)
			case "failed", "cancelled":
				return fail("VERIFIER_SCRIPT_WORKFLOW_FAILED", firstNonEmpty(stringPtrValue(workflowErrorMessage), "source_to_script workflow "+workflowStatus))
			case "succeeded":
				versionID = firstNonEmpty(scriptVersionIDFromWorkflowOutput(workflowOutput), workflowOutputString(workflowOutput, "versionId"))
				if versionID == "" {
					return fail("VERIFIER_MISSING_SCRIPT_VERSION", "source_to_script workflow succeeded without scriptVersionId")
				}
			default:
				return fail("VERIFIER_WORKFLOW_BAD_STATUS", "workflow run status is "+workflowStatus)
			}
		}
		var exists bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM script_versions WHERE project_id = $1 AND id = $2 AND COALESCE(status, 'active') <> 'archived')`, project.ID, versionID).Scan(&exists); err != nil {
			return fail("VERIFIER_SCRIPT_VERSION_CHECK_FAILED", err.Error())
		}
		if !exists {
			return fail("VERIFIER_SCRIPT_VERSION_NOT_FOUND", "script version was not found")
		}
		return ok("script version exists")
	case "script.create_version", "script.rewrite":
		versionID := firstNonEmpty(stringValueFromAny(result.Data["versionId"]), stringValueFromAny(result.Data["scriptVersionId"]))
		if versionID == "" {
			return fail("VERIFIER_MISSING_SCRIPT_VERSION", "versionId is required in tool output")
		}
		var exists bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM script_versions WHERE project_id = $1 AND id = $2 AND COALESCE(status, 'active') <> 'archived')`, project.ID, versionID).Scan(&exists); err != nil {
			return fail("VERIFIER_SCRIPT_VERSION_CHECK_FAILED", err.Error())
		}
		if !exists {
			return fail("VERIFIER_SCRIPT_VERSION_NOT_FOUND", "script version was not found")
		}
		return ok("script version exists")
	case "commerce.script.revise":
		scriptUnitID := stringValueFromAny(result.Data["scriptUnitId"])
		expectedHash := stringValueFromAny(result.Data["contentHash"])
		expectedRevision := agentInt64Value(result.Data["revision"])
		if scriptUnitID == "" || expectedHash == "" || expectedRevision <= 0 {
			return fail(
				"VERIFIER_MISSING_COMMERCE_SCRIPT_REVISION",
				"scriptUnitId, revision and contentHash are required in tool output",
			)
		}
		scriptUnit, err := s.commerceCatalog.GetScriptUnit(
			ctx,
			s.db,
			project.OrganizationID,
			project.ID,
			scriptUnitID,
		)
		if err != nil {
			return fail("VERIFIER_COMMERCE_SCRIPT_NOT_FOUND", err.Error())
		}
		if !commerceScriptRevisionSnapshotMatches(scriptUnit, expectedRevision, expectedHash) {
			return fail(
				"VERIFIER_COMMERCE_SCRIPT_REVISION_MISMATCH",
				"advertising script revision or content hash does not match tool output",
			)
		}
		return ok("advertising script revision and content hash match the committed update")
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
	case "asset.update", "asset.revise_prompt", "storyboard.update_shot", "timeline.update_clip", "storyboard.reorder":
		return ok("tool returned explicit before/after update output")
	default:
		return map[string]any{"status": "skipped", "reason": "no verifier for tool"}
	}
}

func commerceScriptRevisionSnapshotMatches(
	scriptUnit commercepkg.ScriptUnit,
	expectedRevision int64,
	expectedHash string,
) bool {
	return scriptUnit.Revision == expectedRevision &&
		scriptUnit.CurrentContentHash == strings.TrimSpace(expectedHash)
}

func scriptVersionIDFromWorkflowOutput(raw json.RawMessage) string {
	return firstNonEmpty(
		workflowOutputString(raw, "scriptVersionId"),
		workflowOutputString(raw, "versionId"),
		workflowOutputNestedString(raw, "data", "scriptVersionId"),
		workflowOutputNestedString(raw, "data", "versionId"),
	)
}

func workflowOutputString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	payload := rawObject(raw)
	return stringValueFromAny(payload[key])
}

func workflowOutputNestedString(raw json.RawMessage, objectKey, valueKey string) string {
	if len(raw) == 0 {
		return ""
	}
	payload := rawObject(raw)
	nested, ok := payload[objectKey].(map[string]any)
	if !ok {
		return ""
	}
	return stringValueFromAny(nested[valueKey])
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
	ID                  string          `json:"id"`
	WorkflowType        string          `json:"workflowType,omitempty"`
	Status              string          `json:"status"`
	ActiveNodeRuns      int             `json:"activeNodeRuns,omitempty"`
	ActiveProviderTasks int             `json:"activeProviderTasks,omitempty"`
	ErrorCode           string          `json:"errorCode,omitempty"`
	ErrorMessage        string          `json:"errorMessage,omitempty"`
	Input               json.RawMessage `json:"input,omitempty"`
}

type agentCompletedWorkflowRun struct {
	ID             string          `json:"id"`
	WorkflowType   string          `json:"workflowType,omitempty"`
	Status         string          `json:"status"`
	TotalItems     int             `json:"totalItems,omitempty"`
	CompletedItems int             `json:"completedItems,omitempty"`
	FailedItems    int             `json:"failedItems,omitempty"`
	ErrorCode      string          `json:"errorCode,omitempty"`
	ErrorMessage   string          `json:"errorMessage,omitempty"`
	Input          json.RawMessage `json:"-"`
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

func (s *Server) finishAgentTaskFailedWorkflows(ctx context.Context, project Project, taskID string, runs []agentPendingWorkflowRun) (AgentTask, error) {
	errorCode, errorMessage := agentFailedWorkflowError(runs)
	summary := map[string]any{
		"summary":            fmt.Sprintf("子工作流失败，已停止后续执行。失败工作流 %d 个。", len(runs)),
		"failedWorkflowRuns": runs,
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE agent_tasks
		SET status = 'failed',
		    summary = COALESCE(summary, '{}'::jsonb) || $3::jsonb,
		    error_code = $4,
		    error_message = $5,
		    completed_at = now()
		WHERE id = $1 AND project_id = $2
	`, taskID, project.ID, mustMarshal(summary), errorCode, errorMessage); err != nil {
		return AgentTask{}, err
	}
	s.insertAgentTaskEvent(ctx, project, taskID, "agent.task.child_workflow_failed", summary)
	return s.agentTaskWithDetails(requestWithContext(ctx), project.ID, taskID)
}

func agentFailedWorkflowError(runs []agentPendingWorkflowRun) (string, string) {
	if len(runs) == 1 {
		return firstNonEmpty(runs[0].ErrorCode, "CHILD_WORKFLOW_FAILED"),
			firstNonEmpty(runs[0].ErrorMessage, "子工作流执行失败")
	}
	return "CHILD_WORKFLOW_FAILED", fmt.Sprintf("%d 个子工作流执行失败", len(runs))
}

func (s *Server) agentTaskPendingWorkflowRuns(ctx context.Context, projectID, taskID string) ([]agentPendingWorkflowRun, error) {
	ids, err := s.agentTaskWorkflowRunIDs(ctx, taskID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT
			w.id::text,
			COALESCE(NULLIF(w.input->>'workflowType', ''), NULLIF(w.workflow_type, ''), w.template_id::text, ''),
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
			),
			COALESCE(w.error_code, ''),
			COALESCE(w.error_message, ''),
			w.input
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
		if err := rows.Scan(&run.ID, &run.WorkflowType, &run.Status, &run.ActiveNodeRuns, &run.ActiveProviderTasks, &run.ErrorCode, &run.ErrorMessage, &run.Input); err != nil {
			return nil, err
		}
		if !isTerminalWorkflowStatus(run.Status) || run.ActiveNodeRuns > 0 || run.ActiveProviderTasks > 0 {
			pending = append(pending, run)
		}
	}
	return pending, rows.Err()
}

func (s *Server) agentTaskFailedWorkflowRuns(ctx context.Context, projectID, taskID string) ([]agentPendingWorkflowRun, error) {
	ids, err := s.agentTaskWorkflowRunIDs(ctx, taskID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT
			w.id::text,
			COALESCE(NULLIF(w.input->>'workflowType', ''), NULLIF(w.workflow_type, ''), w.template_id::text, ''),
			w.status,
			0,
			0,
			COALESCE(w.error_code, ''),
			COALESCE(w.error_message, ''),
			w.input
		FROM workflow_runs w
		WHERE w.project_id = $1
		  AND w.id = ANY($2::uuid[])
		  AND w.status IN ('failed', 'cancelled')
		ORDER BY w.created_at DESC, w.id DESC
	`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	handled, err := s.agentTaskHandledFailedWorkflowRuns(ctx, taskID)
	if err != nil {
		return nil, err
	}
	failed := make([]agentPendingWorkflowRun, 0)
	for rows.Next() {
		var run agentPendingWorkflowRun
		if err := rows.Scan(&run.ID, &run.WorkflowType, &run.Status, &run.ActiveNodeRuns, &run.ActiveProviderTasks, &run.ErrorCode, &run.ErrorMessage, &run.Input); err != nil {
			return nil, err
		}
		if run.WorkflowType == agentCommerceScriptDerivationWorkflowType {
			state, err := s.agentScriptDerivationLineageState(ctx, projectID, run)
			if err != nil {
				return nil, err
			}
			if state.RootBatchID != "" {
				if handled[state.LatestWorkflowRunID] || handled[run.ID] {
					state.Status = "succeeded"
				}
				if state.Status == "succeeded" || state.Status == "running" {
					continue
				}
				if handled["commerce-script-derivation-root:"+state.RootBatchID] {
					continue
				}
				handled["commerce-script-derivation-root:"+state.RootBatchID] = true
			}
		}
		if handled[run.ID] {
			continue
		}
		failed = append(failed, run)
	}
	return failed, rows.Err()
}

func (s *Server) agentTaskHandledFailedWorkflowRuns(ctx context.Context, taskID string) (map[string]bool, error) {
	var summary json.RawMessage
	if err := s.db.QueryRow(ctx, `SELECT summary FROM agent_tasks WHERE id = $1`, taskID).Scan(&summary); err != nil {
		return nil, err
	}
	values, _ := rawObject(summary)["handledFailedWorkflowRunIds"].([]any)
	handled := make(map[string]bool, len(values))
	for _, value := range values {
		if id := stringValueFromAny(value); id != "" {
			handled[id] = true
		}
	}
	return handled, nil
}

const (
	agentWorkflowRecoveryMaxAttempts          = 3
	agentCommerceScriptDerivationWorkflowType = "commerce_script_derivation"
)

func (s *Server) appendAgentWorkflowRecoveryPlan(
	r *http.Request,
	principal auth.Principal,
	project Project,
	taskID string,
	failedRuns []agentPendingWorkflowRun,
) (bool, error) {
	task, err := s.agentTask(r, project.ID, taskID)
	if err != nil {
		return false, err
	}
	if task.Mode == string(agent.TaskModePlanOnly) {
		return false, nil
	}
	summary := rawObject(task.Summary)
	attempts := int(float64Value(summary["workflowRecoveryAttempts"]))
	if attempts >= agentWorkflowRecoveryMaxAttempts {
		return false, nil
	}
	plan, handledRunIDs, ok, err := s.agentWorkflowRecoveryPlan(r.Context(), project, task.UserGoal, failedRuns)
	if err != nil || !ok {
		return false, err
	}
	registry, err := s.projectAgentRegistry(project)
	if err != nil {
		return false, err
	}
	validated, err := agent.ValidatePlan(plan, registry, agentRuntimeMaxPlanSteps(task))
	if err != nil {
		return false, err
	}
	previousHandled := agentStringValues(summary["handledFailedWorkflowRunIds"])
	handledRunIDs = uniqueNonEmptyStrings(append(previousHandled, handledRunIDs...))
	recoveryInput := map[string]any{
		"goal": task.UserGoal, "failedWorkflowRuns": failedRuns,
		"attempt": attempts + 1, "maxAttempts": agentWorkflowRecoveryMaxAttempts,
	}
	promptHash := promptsvc.HashText("agent-workflow-recovery:" + task.ID + ":" + string(mustMarshal(recoveryInput)))
	var runID string
	if err := s.db.QueryRow(r.Context(), `
		INSERT INTO agent_runs(
			organization_id, project_id, session_id, agent_type, task_type, status,
			input, prompt_hash, task_id, created_by, started_at
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, 'project_agent', 'workflow_recovery', 'running', $4, $5, $6, $7, now())
		RETURNING id
	`, project.OrganizationID, project.ID, stringValue(task.SessionID), mustMarshal(recoveryInput), promptHash, task.ID, principal.UserID).Scan(&runID); err != nil {
		return false, err
	}
	summaryPatch := map[string]any{
		"workflowRecoveryAttempts":    attempts + 1,
		"workflowRecoveryMaxAttempts": agentWorkflowRecoveryMaxAttempts,
		"handledFailedWorkflowRunIds": handledRunIDs,
		"lastWorkflowRecovery":        recoveryInput,
	}
	if err := s.persistAgentPlanWithSummaryPatch(
		r, principal, project, task, registry, validated, runID, provider.GatewayTextResponse{}, summaryPatch,
	); err != nil {
		return false, err
	}
	s.insertAgentTaskEvent(r.Context(), project, task.ID, "agent.task.workflow_recovery_planned", map[string]any{
		"summary": validated.Summary, "attempt": attempts + 1,
		"maxAttempts": agentWorkflowRecoveryMaxAttempts, "handledWorkflowRunIds": handledRunIDs,
	})
	return true, nil
}

func (s *Server) agentWorkflowRecoveryPlan(
	ctx context.Context,
	project Project,
	userGoal string,
	failedRuns []agentPendingWorkflowRun,
) (agent.Plan, []string, bool, error) {
	policy := agentWorkflowRecoveryPolicyForGoal(userGoal)
	needsCapabilityRepair := false
	needsRenderPlanRepair := false
	retryPromptRequested := false
	retryVideoRequested := false
	retryPromptShotIDs := make([]string, 0)
	retryVideoShotIDs := make([]string, 0)
	handledRunIDs := make([]string, 0)
	for _, run := range failedRuns {
		code := agentWorkflowFailureCode(run)
		shotIDs := agentFailedWorkflowShotIDs(run)
		switch code {
		case provider.CodeModelCapabilityApprovalRequired:
			needsCapabilityRepair = true
			handledRunIDs = append(handledRunIDs, run.ID)
			if run.WorkflowType == "batch_generate_shot_video_prompts" {
				retryPromptRequested = true
				retryPromptShotIDs = append(retryPromptShotIDs, shotIDs...)
			} else {
				retryVideoRequested = true
				retryVideoShotIDs = append(retryVideoShotIDs, shotIDs...)
			}
		case "RENDER_PLAN_REPLAN_REQUIRED":
			if !policy.allows("shot.generate_video_prompts") {
				continue
			}
			needsRenderPlanRepair = true
			retryPromptRequested = true
			retryVideoRequested = true
			handledRunIDs = append(handledRunIDs, run.ID)
			retryPromptShotIDs = append(retryPromptShotIDs, shotIDs...)
			retryVideoShotIDs = append(retryVideoShotIDs, shotIDs...)
		}
	}
	if !needsCapabilityRepair && !needsRenderPlanRepair {
		return agent.Plan{}, nil, false, nil
	}
	retryPromptShotIDs = uniqueNonEmptyStrings(retryPromptShotIDs)
	retryVideoShotIDs = uniqueNonEmptyStrings(retryVideoShotIDs)
	steps := make([]agent.PlanStep, 0)
	if needsCapabilityRepair {
		steps = append(steps, agentRecoveryStep("provider.list_status", map[string]any{}, "读取当前视频模型能力快照和审批状态"))
		variants, variantErrors, err := s.agentVideoCapabilityVariantStatus(ctx, project)
		if err != nil {
			return agent.Plan{}, nil, false, err
		}
		if len(variantErrors) > 0 && len(variants) == 0 {
			return agent.Plan{}, nil, false, fmt.Errorf("读取视频模型能力失败: %s", variantErrors[0]["message"])
		}
		needsUserDecision := false
		actionable := 0
		for _, variant := range variants {
			approvalState := stringValueFromAny(variant["approvalState"])
			if approvalState == "approved" || approvalState == "system_verified" {
				continue
			}
			verificationStatus := stringValueFromAny(variant["verificationStatus"])
			args := map[string]any{
				"modelId":                stringValueFromAny(variant["modelId"]),
				"variantKey":             stringValueFromAny(variant["variantKey"]),
				"capabilitySnapshotHash": stringValueFromAny(variant["capabilitySnapshotHash"]),
			}
			switch {
			case approvalState == "rejected":
				needsUserDecision = true
			case verificationStatus == provider.VideoCapabilityVerificationUnknown:
				args["verificationMode"] = "adapter_contract_test"
				args["reason"] = "Project Agent 在恢复视频工作流前验证当前能力快照"
				steps = append(steps, agentRecoveryStep("provider.verify_video_capability", args, "完成视频模型 Adapter 能力验证"))
				actionable++
			default:
				args["decision"] = "approved"
				args["reason"] = "Project Agent 恢复任务：批准当前推断能力快照"
				args["evidence"] = map[string]any{"source": "project_agent_workflow_recovery"}
				steps = append(steps, agentRecoveryStep("provider.attest_video_capability", args, "批准当前视频模型能力快照"))
				actionable++
			}
		}
		if needsUserDecision || actionable == 0 {
			question := "当前视频模型能力快照无法由助手直接恢复，请确认下一步。"
			if needsUserDecision {
				question = "当前视频模型能力快照曾被管理员拒绝，是否重新检查并调整模型能力配置？"
			}
			steps = append(steps, agentRecoveryStep("agent.ask_user", map[string]any{
				"question": question,
				"options": []any{
					map[string]any{"id": "review_capability", "label": "检查模型能力", "description": "打开供应商模型配置并重新验证当前能力。", "nextGoal": "检查并修正视频模型能力后重试失败任务"},
					map[string]any{"id": "change_model", "label": "更换视频模型", "description": "改用已有可靠能力配置的模型。", "nextGoal": "调整视频业务模型绑定后重试"},
				},
				"allowCustom": true, "defaultOptionId": "review_capability",
			}, "等待用户确认视频模型能力处理方式"))
			return agent.Plan{Summary: "视频模型能力需要管理员确认，助手已整理当前能力状态并请求下一步。", Steps: steps}, handledRunIDs, true, nil
		}
	}
	if retryPromptRequested {
		args := map[string]any{"maxConcurrency": 5}
		if len(retryPromptShotIDs) > 0 {
			args["shotIds"] = retryPromptShotIDs
		}
		steps = append(steps, agentRecoveryStep("shot.generate_video_prompts", args, "重新生成并审核缺失或失效的视频提示词契约"))
	}
	if retryVideoRequested {
		args := map[string]any{"maxConcurrency": 5}
		if len(retryVideoShotIDs) > 0 {
			args["shotIds"] = retryVideoShotIDs
		}
		steps = append(steps, agentRecoveryStep("shot.generate_missing_videos", args, "在提示词和能力恢复后重试失败镜头视频"))
	}
	for _, step := range steps {
		if !policy.allows(step.Tool) {
			return agent.Plan{}, nil, false, nil
		}
	}
	return agent.Plan{Summary: "助手已根据子工作流错误生成受控恢复计划。", Steps: steps}, handledRunIDs, len(steps) > 0, nil
}

type agentWorkflowRecoveryPolicy struct {
	restrictedTools                bool
	allowedTools                   map[string]bool
	forbidsVideoPromptRegeneration bool
}

var agentWorkflowRecoveryTools = []string{
	"agent.ask_user",
	"provider.list_status",
	"provider.verify_video_capability",
	"provider.attest_video_capability",
	"shot.generate_video_prompts",
	"shot.generate_missing_videos",
}

func agentWorkflowRecoveryPolicyForGoal(goal string) agentWorkflowRecoveryPolicy {
	normalized := strings.ToLower(strings.Join(strings.Fields(goal), ""))
	policy := agentWorkflowRecoveryPolicy{
		allowedTools:                   map[string]bool{},
		forbidsVideoPromptRegeneration: agentGoalForbidsVideoPromptRegeneration(goal),
	}
	for _, marker := range []string{"仅调用", "只调用", "仅使用", "只使用", "onlycall", "onlyuse", "onlyexecute"} {
		if strings.Contains(normalized, marker) {
			policy.restrictedTools = true
			break
		}
	}
	if policy.restrictedTools {
		for _, tool := range agentWorkflowRecoveryTools {
			if strings.Contains(normalized, tool) {
				policy.allowedTools[tool] = true
			}
		}
	}
	return policy
}

func (policy agentWorkflowRecoveryPolicy) allows(tool string) bool {
	if tool == "shot.generate_video_prompts" && policy.forbidsVideoPromptRegeneration {
		return false
	}
	return !policy.restrictedTools || policy.allowedTools[tool]
}

func agentGoalForbidsVideoPromptRegeneration(goal string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(goal), ""))
	for _, marker := range []string{
		"不得重新生成或审核视频提示词",
		"不得重新生成视频提示词",
		"不要重新生成视频提示词",
		"禁止重新生成视频提示词",
		"不得生成视频提示词",
		"不要生成视频提示词",
		"donotregeneratevideoprompt",
		"mustnotregeneratevideoprompt",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	for _, negation := range []string{"不得", "不要", "禁止", "不可", "不允许"} {
		remaining := normalized
		for {
			index := strings.Index(remaining, negation)
			if index < 0 {
				break
			}
			tail := remaining[index+len(negation):]
			promptIndex := strings.Index(tail, "提示词")
			if promptIndex >= 0 && promptIndex <= 120 {
				restriction := tail[:promptIndex+len("提示词")]
				for _, action := range []string{"重新生成", "生成", "修改", "审核"} {
					if strings.Contains(restriction, action) {
						return true
					}
				}
			}
			remaining = tail
		}
	}
	return false
}

func agentRecoveryStep(tool string, args map[string]any, expected string) agent.PlanStep {
	return agent.PlanStep{Tool: tool, Args: mustMarshal(args), ExpectedResult: expected}
}

func agentWorkflowFailureCode(run agentPendingWorkflowRun) string {
	code := strings.ToUpper(strings.TrimSpace(run.ErrorCode))
	message := strings.ToUpper(run.ErrorMessage)
	for _, candidate := range []string{
		provider.CodeModelCapabilityApprovalRequired,
		"RENDER_PLAN_REPLAN_REQUIRED",
	} {
		if code == candidate || strings.Contains(message, candidate) {
			return candidate
		}
	}
	return code
}

func agentFailedWorkflowShotIDs(run agentPendingWorkflowRun) []string {
	envelope := rawObject(run.Input)
	input := agentMapArg(envelope, "input")
	if len(input) == 0 {
		input = envelope
	}
	return agentReferenceStringSliceArg(input, "shotIds")
}

func agentStringValues(value any) []string {
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text := stringValueFromAny(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func (s *Server) agentTaskWorkflowRunIDs(ctx context.Context, taskID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT tool_name, output
		FROM agent_steps
		WHERE task_id = $1
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
		for _, id := range agentStepChildWorkflowRunIDs(toolName, raw) {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return ids, nil
	}
	lineageRows, err := s.db.Query(ctx, `
		WITH RECURSIVE derivation_lineage AS (
			SELECT batch.id, batch.workflow_run_id
			FROM commerce_script_derivation_batches batch
			WHERE batch.workflow_run_id = ANY($1::uuid[])
			UNION
			SELECT child.id, child.workflow_run_id
			FROM commerce_script_derivation_batches child
			JOIN derivation_lineage parent ON child.retry_of_batch_id = parent.id
		)
		SELECT DISTINCT workflow_run_id::text
		FROM derivation_lineage
		WHERE workflow_run_id IS NOT NULL
	`, ids)
	if err != nil {
		return nil, err
	}
	defer lineageRows.Close()
	for lineageRows.Next() {
		var workflowRunID string
		if err := lineageRows.Scan(&workflowRunID); err != nil {
			return nil, err
		}
		if seen[workflowRunID] {
			continue
		}
		seen[workflowRunID] = true
		ids = append(ids, workflowRunID)
	}
	return ids, lineageRows.Err()
}

type agentScriptDerivationLineageProjection struct {
	RootBatchID         string
	LatestWorkflowRunID string
	Status              string
	TotalItems          int
	CompletedItems      int
	FailedItems         int
}

func (s *Server) agentScriptDerivationLineageState(
	ctx context.Context,
	projectID string,
	run agentPendingWorkflowRun,
) (agentScriptDerivationLineageProjection, error) {
	input := rawObject(run.Input)
	batchID := strings.TrimSpace(stringValueFromAny(input["batchId"]))
	if batchID == "" {
		return agentScriptDerivationLineageProjection{}, nil
	}
	var (
		organizationID      string
		rootBatchID         string
		latestWorkflowRunID string
	)
	err := s.db.QueryRow(ctx, `
		WITH selected AS (
			SELECT organization_id, COALESCE(root_batch_id, id) AS root_batch_id
			FROM commerce_script_derivation_batches
			WHERE id = $1 AND project_id = $2
		),
		latest AS (
			SELECT batch.workflow_run_id
			FROM commerce_script_derivation_batches batch
			JOIN selected ON batch.id = selected.root_batch_id
			                  OR batch.root_batch_id = selected.root_batch_id
			WHERE batch.workflow_run_id IS NOT NULL
			ORDER BY batch.retry_depth DESC, batch.created_at DESC, batch.id DESC
			LIMIT 1
		)
		SELECT selected.organization_id::text,
		       selected.root_batch_id::text,
		       COALESCE((SELECT workflow_run_id::text FROM latest), '')
		FROM selected
	`, batchID, projectID).Scan(&organizationID, &rootBatchID, &latestWorkflowRunID)
	if err == pgx.ErrNoRows {
		return agentScriptDerivationLineageProjection{}, nil
	}
	if err != nil {
		return agentScriptDerivationLineageProjection{}, err
	}
	batch, err := s.commerceDerivations.GetBatch(
		ctx, s.db, organizationID, projectID, rootBatchID, true,
	)
	if err != nil {
		return agentScriptDerivationLineageProjection{}, err
	}
	return agentScriptDerivationLineageProjectionFromBatch(rootBatchID, latestWorkflowRunID, batch), nil
}

func agentScriptDerivationLineageProjectionFromBatch(
	rootBatchID string,
	latestWorkflowRunID string,
	batch commercepkg.ScriptDerivationBatch,
) agentScriptDerivationLineageProjection {
	projection := agentScriptDerivationLineageProjection{
		RootBatchID:         rootBatchID,
		LatestWorkflowRunID: latestWorkflowRunID,
		TotalItems:          len(batch.LineageResults),
	}
	activeItems := 0
	cancelledItems := 0
	for _, result := range batch.LineageResults {
		switch result.LatestResult.Status {
		case "succeeded":
			projection.CompletedItems++
		case "queued", "running", "reviewing":
			activeItems++
		case "cancelled":
			cancelledItems++
			projection.FailedItems++
		case "failed_retryable", "failed_terminal":
			projection.FailedItems++
		}
	}
	switch {
	case activeItems > 0:
		projection.Status = "running"
	case projection.TotalItems > 0 && projection.CompletedItems == projection.TotalItems:
		projection.Status = "succeeded"
	case projection.CompletedItems > 0:
		projection.Status = "partial_succeeded"
	case cancelledItems > 0 && cancelledItems == projection.TotalItems:
		projection.Status = "cancelled"
	case projection.FailedItems > 0:
		projection.Status = "failed"
	default:
		projection.Status = batch.Status
		projection.TotalItems = batch.RequestedCount
		projection.CompletedItems = batch.SucceededCount
		projection.FailedItems = batch.FailedRetryableCount + batch.FailedTerminalCount + batch.CancelledCount
	}
	return projection
}

func (s *Server) agentTaskCommerceScriptDerivationGoalSatisfied(
	ctx context.Context,
	projectID string,
	task AgentTask,
) (bool, error) {
	if !agentGoalIsCommerceScriptDerivationOnly(task.UserGoal) {
		return false, nil
	}
	var hasSucceededDerivationStep bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_steps
			WHERE task_id = $1
			  AND tool_name IN ('commerce.script.derive.batch', 'commerce.script.derive.retry_failed')
			  AND status = 'succeeded'
		)
	`, task.ID).Scan(&hasSucceededDerivationStep); err != nil {
		return false, err
	}
	if !hasSucceededDerivationStep {
		return false, nil
	}
	workflows, err := s.agentTaskCompletedWorkflowRuns(ctx, projectID, task.ID)
	if err != nil {
		return false, err
	}
	for _, result := range workflows {
		if result.WorkflowType == agentCommerceScriptDerivationWorkflowType &&
			result.Status == "succeeded" &&
			result.TotalItems > 0 &&
			result.CompletedItems == result.TotalItems {
			return true, nil
		}
	}
	return false, nil
}

func agentGoalIsCommerceScriptDerivationOnly(goal string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(goal), ""))
	requestsDerivation := false
	for _, marker := range []string{
		"裂变", "场景变体", "脚本变体", "不同场景", "多个场景版本", "场景替换",
		"scriptvariant", "scenevariant", "derivescript", "derivescripts",
	} {
		if strings.Contains(normalized, marker) {
			requestsDerivation = true
			break
		}
	}
	if !requestsDerivation {
		return false
	}
	for _, marker := range []string{
		"生成视频", "制作视频", "创建视频", "开始生成视频", "批量生成视频",
		"修改商品", "更新商品", "删除商品", "更换商品", "上传商品",
		"归档脚本", "删除脚本", "取消任务", "生成图片", "制作图片",
		"generatevideo", "createvideo", "rendervideo", "producevideo",
		"updateproduct", "deleteproduct", "archivescript", "deletescript",
	} {
		if strings.Contains(normalized, marker) {
			return false
		}
	}
	return true
}

// legacyAgentToolWaitsForWorkflow is only used for step results written before
// childWorkflowRunIds became part of the tool result contract.
func legacyAgentToolWaitsForWorkflow(toolName string) bool {
	switch toolName {
	case "workflow.start",
		"script.generate_from_source",
		"asset.batch_generate_prompts",
		"asset.batch_generate_images",
		"timeline.compose",
		"shot.generate_image_prompts",
		"shot.generate_video_prompts",
		"shot.generate_missing_images",
		"shot.generate_missing_videos",
		"shot.cancel_running_videos":
		return true
	default:
		return false
	}
}

func agentStepChildWorkflowRunIDs(toolName string, raw json.RawMessage) []string {
	var result agentToolResult
	if err := json.Unmarshal(raw, &result); err == nil && len(result.ChildWorkflowRunIDs) > 0 {
		ids, err := normalizeAgentWorkflowRunIDs(result.ChildWorkflowRunIDs)
		if err == nil {
			return ids
		}
		return nil
	}
	if !legacyAgentToolWaitsForWorkflow(toolName) {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	ids, err := agentWorkflowRunIDsFromValue(value)
	if err != nil {
		return nil
	}
	return ids
}

func agentWorkflowRunIDsFromValue(value any) ([]string, error) {
	values := make([]string, 0, 1)
	collectAgentWorkflowRunIDs(value, func(id string) {
		values = append(values, id)
	})
	return normalizeAgentWorkflowRunIDs(values)
}

func normalizeAgentWorkflowRunIDs(values []string) ([]string, error) {
	seen := make(map[string]bool, len(values))
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" || seen[id] {
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("启动型工具返回了无效的子工作流标识")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
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
	registry, err := s.projectAgentRegistry(project)
	if err != nil {
		return false, nil, err
	}
	validated, err := agent.ValidatePlan(plan, registry, agentRuntimeMaxPlanSteps(task))
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
	rows.Close()
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
	workflowResults, err := s.agentTaskCompletedWorkflowRuns(ctx, projectID, taskID)
	if err != nil {
		return err
	}
	if len(workflowResults) > 0 {
		patch["summary"] = agentCompletedWorkflowSummary(workflowResults)
		patch["waitingForWorkflowRuns"] = []agentPendingWorkflowRun{}
		patch["failedWorkflowRuns"] = []agentPendingWorkflowRun{}
		patch["completedWorkflowRuns"] = workflowResults
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

func (s *Server) agentTaskCompletedWorkflowRuns(ctx context.Context, projectID, taskID string) ([]agentCompletedWorkflowRun, error) {
	ids, err := s.agentTaskWorkflowRunIDs(ctx, taskID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT w.id::text,
		       COALESCE(NULLIF(w.input->>'workflowType', ''), NULLIF(w.workflow_type, ''), w.template_id::text, ''),
		       w.status, w.total_items, w.completed_items, w.failed_items,
		       COALESCE(w.error_code, ''), COALESCE(w.error_message, ''),
		       w.input
		FROM workflow_runs w
		WHERE w.project_id = $1
		  AND w.id = ANY($2::uuid[])
		  AND w.status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled', 'skipped')
		ORDER BY w.created_at DESC, w.id DESC
	`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rawResults := make([]agentCompletedWorkflowRun, 0, len(ids))
	for rows.Next() {
		var result agentCompletedWorkflowRun
		if err := rows.Scan(
			&result.ID, &result.WorkflowType, &result.Status,
			&result.TotalItems, &result.CompletedItems, &result.FailedItems,
			&result.ErrorCode, &result.ErrorMessage, &result.Input,
		); err != nil {
			return nil, err
		}
		rawResults = append(rawResults, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	results := make([]agentCompletedWorkflowRun, 0, len(rawResults))
	projectedDerivations := make(map[string]bool)
	for _, result := range rawResults {
		if result.WorkflowType != agentCommerceScriptDerivationWorkflowType {
			results = append(results, result)
			continue
		}
		state, err := s.agentScriptDerivationLineageState(ctx, projectID, agentPendingWorkflowRun{
			ID: result.ID, WorkflowType: result.WorkflowType, Status: result.Status, Input: result.Input,
		})
		if err != nil {
			return nil, err
		}
		if state.RootBatchID == "" {
			results = append(results, result)
			continue
		}
		if projectedDerivations[state.RootBatchID] {
			continue
		}
		projectedDerivations[state.RootBatchID] = true
		results = append(results, agentCompletedWorkflowRun{
			ID:             firstNonEmpty(state.LatestWorkflowRunID, result.ID),
			WorkflowType:   agentCommerceScriptDerivationWorkflowType,
			Status:         state.Status,
			TotalItems:     state.TotalItems,
			CompletedItems: state.CompletedItems,
			FailedItems:    state.FailedItems,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].ID < results[j].ID
	})
	return results, nil
}

func agentCompletedWorkflowSummary(results []agentCompletedWorkflowRun) string {
	partialRuns := 0
	completedItems := 0
	failedItems := 0
	for _, result := range results {
		completedItems += result.CompletedItems
		failedItems += result.FailedItems
		if result.Status == "partial_succeeded" || result.Status == "failed" || result.Status == "cancelled" || result.FailedItems > 0 {
			partialRuns++
		}
	}
	if partialRuns > 0 || failedItems > 0 {
		return fmt.Sprintf("生产任务部分完成：%d 个工作流，成功 %d 项，失败 %d 项。", len(results), completedItems, failedItems)
	}
	if completedItems > 0 {
		return fmt.Sprintf("生产任务已完成：%d 个工作流，共完成 %d 项。", len(results), completedItems)
	}
	return fmt.Sprintf("生产任务已完成：%d 个工作流。", len(results))
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
	raw, _ := projectevents.ProjectRealtimePayload(eventType, mustMarshal(payload))
	_ = insertAPIEvent(ctx, s.db, project.OrganizationID, project.ID, eventType, "agent_step", stepID, raw)
}

func (s *Server) insertAgentTaskEvent(ctx context.Context, project Project, taskID, eventType string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["agentTaskId"] = taskID
	if sessionID := s.agentTaskSessionID(ctx, taskID); sessionID != "" {
		payload["sessionId"] = sessionID
	}
	raw, _ := projectevents.ProjectRealtimePayload(eventType, mustMarshal(payload))
	_ = insertAPIEvent(ctx, s.db, project.OrganizationID, project.ID, eventType, "agent_task", taskID, raw)
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://project-control.internal/", nil)
	if err != nil {
		panic(err)
	}
	return request
}

func (s *Server) agentToolListWorkflowNodes(r *http.Request, project Project, args map[string]any) agentToolResult {
	page, err := s.listWorkflowNodesAction(r.Context(), project, workflowRunChildrenActionInput{
		WorkflowRunID: agentReferenceStringArg(args, "workflowRunId"),
		Limit:         agentIntArg(args, "limit", 100, 1, projectOperationalReadMaximumPageSize),
		Cursor:        agentStringArg(args, "cursor"),
	})
	if err != nil {
		return agentToolError("workflow.read_nodes", args, err)
	}
	return workflowNodeListAgentResult(args, page)
}

func (s *Server) agentToolListWorkflowShots(r *http.Request, project Project, args map[string]any) agentToolResult {
	page, err := s.listWorkflowShotsAction(r.Context(), project, workflowRunChildrenActionInput{
		WorkflowRunID:     agentReferenceStringArg(args, "workflowRunId"),
		Limit:             agentIntArg(args, "limit", 100, 1, projectOperationalReadMaximumPageSize),
		Cursor:            agentStringArg(args, "cursor"),
		IncludePreviewURL: agentBoolArgValue(args, "includePreviewUrl"),
		PreviewExpires:    agentIntArg(args, "previewExpiresSeconds", 900, 60, 3600),
	})
	if err != nil {
		return agentToolError("workflow.read_shots", args, err)
	}
	return workflowShotListAgentResult(args, page)
}

func (s *Server) workflowRunForProject(r *http.Request, projectID, runID string) (WorkflowRun, error) {
	return scanWorkflowRun(s.db.QueryRow(r.Context(), workflowRunSelectSQL(`
		WHERE id = $1 AND project_id = $2
	`), runID, projectID))
}

func (s *Server) agentToolGetScript(r *http.Request, project Project, args map[string]any) agentToolResult {
	page, err := s.getScriptAction(r.Context(), project, scriptGetActionInput{
		ScriptID:      agentReferenceStringArg(args, "scriptId"),
		VersionID:     agentReferenceStringArg(args, "versionId"),
		EpisodeLimit:  agentIntArg(args, "episodeLimit", 20, 1, scriptActionMaximumPageSize),
		EpisodeCursor: agentStringArg(args, "episodeCursor"),
	})
	if err != nil {
		return agentToolError("script.get", args, err)
	}
	return scriptGetAgentResult(args, page)
}

func (s *Server) agentToolListReviewItems(r *http.Request, project Project, args map[string]any) agentToolResult {
	status := firstNonEmpty(agentStringArg(args, "status"), "open")
	page, err := s.listReviewItemsAction(r.Context(), project, reviewItemListActionInput{
		Status:     status,
		Severity:   agentStringArg(args, "severity"),
		Category:   agentStringArg(args, "category"),
		EntityType: agentStringArg(args, "entityType"),
		Limit:      agentIntArg(args, "limit", 50, 1, projectOperationalReadMaximumPageSize),
		Cursor:     agentStringArg(args, "cursor"),
	})
	if err != nil {
		return agentToolError("review.list_items", args, err)
	}
	return reviewItemListAgentResult(args, page, status)
}

func (s *Server) agentToolProviderStatus(r *http.Request, project Project, args map[string]any) agentToolResult {
	status, err := s.readProviderStatusAction(r.Context(), project)
	if err != nil {
		return agentToolError("provider.list_status", args, err)
	}
	return providerStatusAgentResult(args, status)
}

func (s *Server) agentVideoCapabilityVariantStatus(ctx context.Context, project Project) ([]map[string]any, []map[string]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT model.id::text, model.model_key, model.display_name
		FROM model_profiles profile
		JOIN model_profile_bindings binding ON binding.model_profile_id = profile.id AND binding.enabled
		JOIN provider_models model ON model.id = binding.provider_model_id AND model.status = 'active'
		JOIN provider_accounts account ON account.id = model.provider_account_id AND account.status = 'active'
		WHERE profile.organization_id = $1
		  AND profile.profile_key = $2
		ORDER BY model.display_name, model.model_key
	`, project.OrganizationID, project.VideoModelProfileKey)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type boundModel struct{ ID, Key, Name string }
	models := make([]boundModel, 0)
	for rows.Next() {
		var model boundModel
		if err := rows.Scan(&model.ID, &model.Key, &model.Name); err != nil {
			return nil, nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	variants := make([]map[string]any, 0)
	variantErrors := make([]map[string]string, 0)
	for _, model := range models {
		status, err := s.providers.ListVideoCapabilityAttestations(ctx, project.OrganizationID, model.ID)
		if err != nil {
			variantErrors = append(variantErrors, map[string]string{"modelId": model.ID, "modelKey": model.Key, "message": err.Error()})
			continue
		}
		for _, variant := range status.Variants {
			approvalState := "pending"
			if variant.CurrentAttestation != nil {
				approvalState = variant.CurrentAttestation.Decision
			} else if variant.VerificationStatus == provider.VideoCapabilityVerificationOfficial || variant.VerificationStatus == provider.VideoCapabilityVerificationTested {
				approvalState = "system_verified"
			}
			variants = append(variants, map[string]any{
				"modelId": model.ID, "modelKey": model.Key, "modelName": model.Name,
				"variantKey": variant.VariantKey, "capabilitySnapshotHash": variant.CapabilitySnapshotHash,
				"verificationStatus": variant.VerificationStatus, "source": variant.Source,
				"approvalState": approvalState,
			})
		}
	}
	return variants, variantErrors, nil
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
	case "batch_generate_derived_asset_images":
		return s.productionActionWorkflowCore(r, project, "generate_derived_asset_images", req)
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
		normalized, err := normalizeWorkflowRequestInput(workflowType, mustMarshal(input), project)
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
	case "batch_generate_derived_asset_images":
		return workflows.BatchGenerateDerivedAssetImagesWorkflow
	case "script_to_video", "full_production", "video_production":
		return workflows.VideoProductionWorkflow
	case "compose_timeline":
		return workflows.ComposeTimelineWorkflow
	default:
		return workflows.TextToStoryboardWorkflow
	}
}

func (s *Server) agentWorkflowRunForStep(ctx context.Context, projectID, taskID, stepID string) (WorkflowRun, bool, error) {
	run, err := scanWorkflowRun(s.db.QueryRow(ctx, workflowRunSelectSQL(`
		WHERE project_id = $1
		  AND input->'input'->>'agentTaskId' = $2
		  AND input->'input'->>'agentStepId' = $3
		  AND status <> 'failed'
		ORDER BY created_at DESC
		LIMIT 1
	`), projectID, taskID, stepID))
	if err == pgx.ErrNoRows {
		return WorkflowRun{}, false, nil
	}
	if err != nil {
		return WorkflowRun{}, false, err
	}
	return run, true, nil
}

func (s *Server) agentToolShotStatus(r *http.Request, project Project, args map[string]any) agentToolResult {
	status, err := s.readShotStatusAction(r.Context(), project, shotStatusActionInput{
		ScriptSceneID:     agentReferenceStringArg(args, "scriptSceneId"),
		WorkflowRunID:     agentReferenceStringArg(args, "workflowRunId"),
		ScriptEpisodeID:   agentReferenceStringArg(args, "scriptEpisodeId"),
		StoryboardPlanID:  agentReferenceStringArg(args, "storyboardPlanId"),
		IncludePreviewURL: agentBoolArgValue(args, "includePreviewUrl"),
		Limit:             agentIntArg(args, "limit", 100, 1, projectOperationalReadMaximumPageSize),
	})
	if err != nil {
		return agentToolError("shot.status", args, err)
	}
	return shotStatusAgentResult(args, status)
}

func agentShotProductionAction(action string, args map[string]any) string {
	if len(agentReferenceStringSliceArg(args, "shotIds")) == 0 {
		return action
	}
	switch action {
	case "generate_image_prompts":
		return "generate_selected_image_prompts"
	case "generate_video_prompts":
		return "generate_selected_video_prompts"
	case "generate_missing_videos":
		return "generate_selected_videos"
	case "generate_missing_images":
		return "generate_selected_images"
	default:
		return action
	}
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

func (s *Server) agentStepStreamProgressCallback(ctx context.Context, project Project, task AgentTask, step AgentStep, toolName string, episodeIndex, episodeTotal int, chapterTitle string) func(scriptAgentStreamProgress) error {
	if episodeTotal <= 0 {
		episodeTotal = 1
	}
	if episodeIndex <= 0 {
		episodeIndex = 1
	}
	lastFlush := time.Time{}
	lastLength := 0
	return func(update scriptAgentStreamProgress) error {
		textLength := len([]rune(update.Text))
		now := time.Now()
		if !update.Done && now.Sub(lastFlush) < 1200*time.Millisecond && textLength-lastLength < 160 {
			return nil
		}
		lastFlush = now
		lastLength = textLength
		summary := fmt.Sprintf("正在生成剧本：第 %d/%d 集", episodeIndex, episodeTotal)
		if strings.TrimSpace(chapterTitle) != "" {
			summary += "「" + strings.TrimSpace(chapterTitle) + "」"
		}
		if update.Done {
			summary = fmt.Sprintf("已完成剧本分集：第 %d/%d 集", episodeIndex, episodeTotal)
		}
		progress := map[string]any{
			"kind":           "stream_text",
			"toolName":       toolName,
			"agentRunId":     update.RunID,
			"episodeIndex":   episodeIndex,
			"episodeTotal":   episodeTotal,
			"chapterTitle":   strings.TrimSpace(chapterTitle),
			"text":           tailRunes(strings.TrimSpace(update.Text), 6000),
			"textLength":     textLength,
			"done":           update.Done,
			"updatedAt":      now.UTC().Format(time.RFC3339Nano),
			"providerCallId": strings.TrimSpace(update.ProviderCallID),
			"modelId":        strings.TrimSpace(update.ModelID),
		}
		return s.updateAgentStepProgress(ctx, project.ID, task.ID, step.ID, map[string]any{
			"status":   "running",
			"summary":  summary,
			"progress": progress,
		})
	}
}

func (s *Server) updateAgentStepProgress(ctx context.Context, projectID, taskID, stepID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		UPDATE agent_steps
		SET output = COALESCE(output, '{}'::jsonb) || $4::jsonb,
		    updated_at = now()
		WHERE id = $1
		  AND task_id = $2
		  AND status = 'running'
		  AND EXISTS (
		    SELECT 1 FROM agent_tasks t
		    WHERE t.id = agent_steps.task_id AND t.project_id = $3
		  )
	`, stepID, taskID, projectID, mustMarshal(patch))
	return err
}

func tailRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
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

func (s *Server) agentToolRenderPromptTest(r *http.Request, project Project, args map[string]any) agentToolResult {
	rendered, err := s.renderPromptAction(r.Context(), project.OrganizationID, project.ID, projectPromptVariables(project), promptRenderActionInput{
		TemplateKey: agentStringArg(args, "templateKey"),
		Variables:   agentMapArg(args, "variables"),
		Input:       agentMapArg(args, "input"),
	})
	if err != nil {
		return agentToolError("prompt.render_test", args, err)
	}
	return agentToolOK("prompt.render_test", args, "提示词渲染测试已完成。", map[string]any{
		"templateKey":     rendered.TemplateKey,
		"promptVersionId": rendered.PromptVersionID,
		"renderedHash":    rendered.RenderedHash,
		"contentHash":     rendered.ContentHash,
		"promptSource":    rendered.PromptSource,
		"text":            rendered.Text,
	})
}

func (s *Server) agentToolGenerateScriptFromSource(r *http.Request, principal auth.Principal, project Project, task AgentTask, step AgentStep, args map[string]any) agentToolResult {
	if existing, ok, err := s.agentWorkflowRunForStep(r.Context(), project.ID, task.ID, step.ID); err != nil {
		return agentToolError("script.generate_from_source", args, err)
	} else if ok {
		return agentToolOK("script.generate_from_source", args, fmt.Sprintf("已存在原文转剧本工作流 %s，未重复启动。", existing.ID), map[string]any{
			"workflowRunId": existing.ID,
			"workflowType":  "source_to_script",
			"status":        existing.Status,
			"input":         rawObject(existing.Input),
			"agentTaskId":   task.ID,
			"agentStepId":   step.ID,
			"idempotent":    true,
		})
	}
	raw := json.RawMessage(mustMarshal(args))
	var input scriptGenerateFromSourceActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return agentToolError("script.generate_from_source", args, controlValidationError("生成剧本参数无效"))
	}
	result, err := s.generateScriptFromSourceCore(r.Context(), principal, project, input, task.UserGoal, "", task.ID, step.ID)
	if err != nil {
		return agentToolError("script.generate_from_source", args, err)
	}
	response := scriptGenerateFromSourceAgentResult("script.generate_from_source", raw, result)
	response.Data["agentTaskId"] = task.ID
	response.Data["agentStepId"] = step.ID
	response.Data["idempotencyKey"] = agentStepIdempotencyKey(task, step)
	return response
}

func scriptEpisodeGenerationInstruction(base string, index, total int, chapter scriptNovelChapterContext) string {
	parts := []string{}
	if strings.TrimSpace(base) != "" {
		parts = append(parts, strings.TrimSpace(base))
	}
	parts = append(parts,
		"本次只改编当前单个分集，禁止合并其它章节，禁止跳到后续分集。",
		"保持原文事件顺序和重要台词，删除旁白时必须保留剧情因果。",
		"输出该分集可直接进入分镜解析的完整剧本正文。",
		"分集序号："+intToString(index)+"/"+intToString(total),
		"原文分集："+chapter.Title,
	)
	return strings.Join(parts, "\n")
}

func (s *Server) markAgentRunsAborted(r *http.Request, runIDs []string, code, message string) {
	for _, runID := range runIDs {
		runID = strings.TrimSpace(runID)
		if runID == "" {
			continue
		}
		_, _ = s.db.Exec(r.Context(), `
			UPDATE agent_runs
			SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
			WHERE id = $1 AND status = 'running'
		`, runID, code, message)
	}
}

func firstString(values []string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
		WHERE project_id = $1 AND script_id = $2 AND metadata->>'agentStepId' = $3 AND COALESCE(status, 'active') <> 'archived'
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
			tx, err := s.db.Begin(r.Context())
			if err != nil {
				return agentToolError("script.create_version", args, err)
			}
			defer tx.Rollback(r.Context())
			if _, err := activateScriptVersionTx(r, tx, project, script, version); err != nil {
				return agentToolError("script.create_version", args, err)
			}
			if err := tx.Commit(r.Context()); err != nil {
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
	if _, err := insertScriptEpisodesTx(r, tx, project, script.ID, version.ID, principal.UserID, []scriptEpisodeDraft{
		defaultScriptEpisodeDraft(script.SourceID, "第 1 集", content, contentFormat, "", "", "", metadata),
	}); err != nil {
		return agentToolError("script.create_version", args, err)
	}
	activate := false
	if value, exists := agentBoolArg(args, "activate"); exists {
		activate = value
	}
	if activate {
		if _, err := activateScriptVersionTx(r, tx, project, script, version); err != nil {
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return agentToolError("script.activate_version", args, err)
	}
	defer tx.Rollback(r.Context())
	if _, err := activateScriptVersionTx(r, tx, project, script, version); err != nil {
		return agentToolError("script.activate_version", args, err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return agentToolError("script.activate_version", args, err)
	}
	return agentToolOK("script.activate_version", args, fmt.Sprintf("已激活剧本版本 v%d。", version.Version), map[string]any{
		"scriptId":       script.ID,
		"versionId":      version.ID,
		"version":        version.Version,
		"idempotencyKey": agentStepIdempotencyKey(task, step),
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
