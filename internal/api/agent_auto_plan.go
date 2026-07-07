package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/agent"
)

func (s *Server) agentAutoProductionPlan(r *http.Request, project Project, task AgentTask) (agent.Plan, bool, error) {
	if !agentGoalRequestsAutoProduction(task.UserGoal) {
		return agent.Plan{}, false, nil
	}
	status, err := s.productionStatus(r, project)
	if err != nil {
		return agent.Plan{}, false, err
	}
	gapSummary, err := s.agentProjectGapSummary(r.Context(), project, status)
	if err != nil {
		return agent.Plan{}, false, err
	}
	plan, ok := agentAutoProductionPlanFromSummary(task.UserGoal, gapSummary)
	return plan, ok, nil
}

func agentGoalRequestsAutoProduction(goal string) bool {
	text := strings.ToLower(strings.TrimSpace(goal))
	if text == "" {
		return false
	}
	keywords := []string{
		"生成成片",
		"生成最终预览",
		"自动推进",
		"继续生产",
		"完整生产",
		"一键生产",
		"一键制作",
		"推进到成片",
		"做到成片",
		"制作成片",
		"可预览成片",
		"run full production",
		"full production",
		"auto production",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func agentAutoProductionPlanFromSummary(goal string, summary agentProjectGapSummary) (agent.Plan, bool) {
	if !agentGoalRequestsAutoProduction(goal) {
		return agent.Plan{}, false
	}
	steps := []agent.PlanStep{
		agentPlanStep("project.read_summary", map[string]any{}, "读取当前项目状态和离成片缺口"),
	}
	if summary.Reviews.OpenCritical > 0 || summary.Reviews.OpenHigh > 0 {
		steps = append(steps, agentPlanStep("review.list_items", map[string]any{"limit": 50}, "列出阻塞生产的高危审阅问题"))
		return agent.Plan{Summary: "项目存在 high/critical 审阅问题，自动生产先暂停并列出阻塞项。", Steps: steps}, true
	}
	if missing := missingAgentProviderProfiles(summary.ProviderProfiles); len(missing) > 0 {
		steps = append(steps, agentPlanStep("provider.list_status", map[string]any{}, "检查业务模型绑定和供应商状态"))
		return agent.Plan{Summary: "项目业务模型绑定未全部可用，自动生产先暂停在供应商配置检查。", Steps: steps}, true
	}
	if summary.Workflows.Failed > 0 {
		steps = append(steps, agentPlanStep("workflow.read_runs", map[string]any{"limit": 20}, "列出失败工作流并等待人工处理"))
		return agent.Plan{Summary: "项目存在失败工作流，自动生产已暂停，避免重复启动同一生产动作。", Steps: steps}, true
	}
	next, ok := firstExecutableAutoProductionAction(summary.NextActions)
	if !ok {
		return agent.Plan{Summary: firstNonEmpty(summary.Summary, "当前没有可自动执行的下一步生产动作。"), Steps: steps}, true
	}
	if agentAutoActionShouldRunReview(next) {
		steps = append(steps, agentPlanStep("review.run", map[string]any{
			"reviewType":                 "project",
			"includeDeterministicChecks": true,
			"includeAgent":               false,
		}, "在生产动作前运行确定性项目审阅"))
	}
	steps = append(steps, agentPlanStep(next.Tool, next.Arguments, next.Reason))
	return agent.Plan{
		Summary: "按当前项目状态自动选择下一步：" + next.Label + "。",
		Steps:   steps,
	}, true
}

func firstExecutableAutoProductionAction(actions []agentProjectNextAction) (agentProjectNextAction, bool) {
	for _, action := range actions {
		if strings.TrimSpace(action.Tool) == "" {
			continue
		}
		switch action.Tool {
		case "workflow.start", "shot.generate_missing_images", "shot.generate_missing_videos", "timeline.compose", "provider.list_status", "review.list_items":
			if action.Arguments == nil {
				action.Arguments = map[string]any{}
			}
			return action, true
		}
	}
	return agentProjectNextAction{}, false
}

func agentAutoActionShouldRunReview(action agentProjectNextAction) bool {
	switch action.Tool {
	case "workflow.start", "shot.generate_missing_images", "shot.generate_missing_videos", "timeline.compose":
		return true
	default:
		return false
	}
}

func agentPlanStep(tool string, args map[string]any, expected string) agent.PlanStep {
	if args == nil {
		args = map[string]any{}
	}
	return agent.PlanStep{
		Tool:           tool,
		Args:           json.RawMessage(mustMarshal(args)),
		ExpectedResult: strings.TrimSpace(expected),
	}
}
