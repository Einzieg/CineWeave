package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/videoproduction"
)

type projectVideoProductionRebuildImpactActionResult struct {
	Impact        videoproduction.RebuildImpact        `json:"impact"`
	Compatibility videoProductionCompatibilityResponse `json:"compatibility"`
}

func (s *Server) projectVideoProductionRebuildImpactAction(
	ctx context.Context,
	db videoProductionQueryer,
	project Project,
	req projectVideoProductionRebuildTargetRequest,
) (projectVideoProductionRebuildImpactActionResult, error) {
	req.TargetProfileKey = strings.TrimSpace(req.TargetProfileKey)
	if req.TargetProfileKey == "" {
		return projectVideoProductionRebuildImpactActionResult{}, videoproduction.NewError(videoproduction.CodeProfileNotFound, "targetProfileKey 不能为空", false)
	}
	target, err := videoproduction.ResolveProfileVersion(ctx, db, req.TargetProfileKey, req.TargetProfileVersion, true)
	if err != nil {
		return projectVideoProductionRebuildImpactActionResult{}, err
	}
	targetConfiguration, err := s.resolveTargetProductionConfiguration(ctx, db, project.OrganizationID, project.ID, req.TargetConfiguration)
	if err != nil {
		return projectVideoProductionRebuildImpactActionResult{}, err
	}
	compatibility, err := s.loadVideoProductionCompatibility(ctx, db, projectWithProductionConfiguration(project, targetConfiguration), target)
	if err != nil {
		return projectVideoProductionRebuildImpactActionResult{}, err
	}
	impact, err := videoproduction.BuildRebuildImpact(ctx, db, project.ID, target, targetConfiguration)
	if err != nil {
		return projectVideoProductionRebuildImpactActionResult{}, err
	}
	return projectVideoProductionRebuildImpactActionResult{Impact: impact, Compatibility: compatibility}, nil
}

func projectVideoProductionRebuildImpactAgentResult(args map[string]any, result projectVideoProductionRebuildImpactActionResult) agentToolResult {
	return agentToolOK(
		"project.production_rebuild_impact",
		args,
		fmt.Sprintf("已计算生产配置换代影响：%d 个分集需要重建，保留 %d 个核心资产。", result.Impact.Counts.Episodes, result.Impact.Counts.RetainedAssets),
		projectOperationalReadData(result),
	)
}

func decodeProjectVideoProductionRebuildImpactActionInput(raw json.RawMessage) (projectVideoProductionRebuildTargetRequest, error) {
	var input projectVideoProductionRebuildTargetRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return projectVideoProductionRebuildTargetRequest{}, controlValidationError("project.production_rebuild_impact 输入格式无效")
	}
	return input, nil
}

type retryProjectVideoProductionRebuildActionInput struct {
	RebuildID string `json:"rebuildId"`
}

func (s *Server) executeProjectProductionRebuildAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input createProjectVideoProductionRebuildRequest
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.createProjectVideoProductionRebuildCore(
		ctx,
		principal,
		project,
		input,
		"project-control-command:"+command.ID+":project.production_rebuild",
	)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := fmt.Sprintf("已创建生产配置换代任务，正在按 %d 个分集重建分镜。", result.Rebuild.EpisodeCount)
	if result.IdempotentReplay {
		summary = "生产配置换代任务已存在，未重复创建。"
	}
	return projectVideoProductionRebuildAgentResult(
		"project.production_rebuild", raw, summary, result,
	), nil
}

func (s *Server) executeProjectProductionRebuildRetryAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input retryProjectVideoProductionRebuildActionInput
	if err := decodeWorkflowActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.retryProjectVideoProductionRebuildCore(
		ctx,
		principal,
		project,
		input.RebuildID,
		"project-control-command:"+command.ID+":project.production_rebuild.retry_failed",
	)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := "已提交生产配置换代失败分集重试。"
	if result.IdempotentReplay {
		summary = "生产配置换代失败分集重试已存在，未重复创建。"
	}
	return projectVideoProductionRebuildAgentResult(
		"project.production_rebuild.retry_failed", raw, summary, result,
	), nil
}

func projectVideoProductionRebuildAgentResult(
	name string,
	raw json.RawMessage,
	summary string,
	result projectVideoProductionRebuildActionResult,
) agentToolResult {
	return agentToolOK(name, workflowActionArguments(raw), summary, map[string]any{
		"rebuild":          result.Rebuild,
		"rebuildId":        result.Rebuild.ID,
		"workflowRunId":    result.WorkflowRunID,
		"workflowRunIds":   []string{result.WorkflowRunID},
		"idempotentReplay": result.IdempotentReplay,
	})
}
