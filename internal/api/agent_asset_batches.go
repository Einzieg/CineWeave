package api

import (
	"fmt"
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/workflows"
)

func (s *Server) agentToolStartAssetBatch(
	r *http.Request,
	principal auth.Principal,
	project Project,
	task AgentTask,
	step AgentStep,
	args map[string]any,
	operation string,
) agentToolResult {
	toolName := "asset.batch_generate_prompts"
	if operation == workflows.AssetBatchOperationGenerateImages {
		toolName = "asset.batch_generate_images"
	}
	assetIDs := uniqueNonEmptyStrings(agentReferenceStringSliceArg(args, "assetIds"))
	if len(assetIDs) == 0 || len(assetIDs) > maxAssetBatchItems {
		return agentToolError(toolName, args, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetIds must contain between 1 and 500 unique values"))
	}
	maxConcurrency := agentIntArg(args, "maxConcurrency", workflows.DefaultAssetBatchConcurrency, 1, workflows.MaxAssetBatchConcurrency)
	force, _ := agentBoolArg(args, "force")
	run, started, err := s.createAssetBatchRun(nil, r, principal, project, createAssetBatchRequest{
		Operation:               operation,
		AssetIDs:                assetIDs,
		MaxConcurrency:          maxConcurrency,
		Force:                   force,
		ExpectedProjectRevision: project.Revision,
		IdempotencyKey:          agentStepIdempotencyKey(task, step),
	}, "", "", 1, "agent-asset-batches:create")
	if err != nil {
		return agentToolError(toolName, args, err)
	}
	summary := fmt.Sprintf("已启动 %d 项资产批处理，工作流 %s 当前状态 %s。", len(assetIDs), run.ID, run.Status)
	if !started {
		summary = fmt.Sprintf("已存在相同资产批处理工作流 %s，未重复启动。", run.ID)
	}
	return agentToolOK(toolName, args, summary, map[string]any{
		"workflowRunId":  run.ID,
		"workflowType":   run.WorkflowType,
		"status":         run.Status,
		"assetIds":       assetIDs,
		"operation":      operation,
		"maxConcurrency": maxConcurrency,
		"force":          force,
		"idempotent":     !started,
		"agentTaskId":    task.ID,
		"agentStepId":    step.ID,
	})
}
