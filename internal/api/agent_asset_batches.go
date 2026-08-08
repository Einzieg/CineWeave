package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/workflows"
)

type assetBatchActionInput struct {
	AssetIDs       []string `json:"assetIds"`
	MaxConcurrency int      `json:"maxConcurrency,omitempty"`
	Force          bool     `json:"force,omitempty"`
}

func (s *Server) executeAssetBatchPromptsAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	return s.executeAssetBatchAsyncAction(ctx, principal, project, command, raw, workflows.AssetBatchOperationGeneratePrompts)
}

func (s *Server) executeAssetBatchImagesAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	return s.executeAssetBatchAsyncAction(ctx, principal, project, command, raw, workflows.AssetBatchOperationGenerateImages)
}

func (s *Server) executeAssetBatchAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
	operation string,
) (agentToolResult, error) {
	toolName := "asset.batch_generate_prompts"
	if operation == workflows.AssetBatchOperationGenerateImages {
		toolName = "asset.batch_generate_images"
	}
	var input assetBatchActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	assetIDs := uniqueNonEmptyStrings(input.AssetIDs)
	if len(assetIDs) == 0 || len(assetIDs) > maxAssetBatchItems {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "assetIds must contain between 1 and 500 unique values")
	}
	maxConcurrency := input.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = workflows.DefaultAssetBatchConcurrency
	}
	if maxConcurrency > workflows.MaxAssetBatchConcurrency {
		maxConcurrency = workflows.MaxAssetBatchConcurrency
	}
	run, started, err := s.createAssetBatchRun(nil, requestWithContext(ctx), principal, project, createAssetBatchRequest{
		Operation:               operation,
		AssetIDs:                assetIDs,
		MaxConcurrency:          maxConcurrency,
		Force:                   input.Force,
		ExpectedProjectRevision: project.Revision,
		IdempotencyKey:          "project-control-command:" + command.ID,
	}, "", "", 1, "asset-batches:create")
	if err != nil {
		return agentToolResult{}, err
	}
	summary := fmt.Sprintf("已启动 %d 项资产批处理，工作流 %s 当前状态 %s。", len(assetIDs), run.ID, run.Status)
	if !started {
		summary = fmt.Sprintf("已存在相同资产批处理工作流 %s，未重复启动。", run.ID)
	}
	result := agentToolOK(toolName, workflowActionArguments(raw), summary, map[string]any{
		"workflowRunId":  run.ID,
		"workflowType":   run.WorkflowType,
		"status":         run.Status,
		"assetIds":       assetIDs,
		"operation":      operation,
		"maxConcurrency": maxConcurrency,
		"force":          input.Force,
		"idempotent":     !started,
	})
	result.ChildWorkflowRunIDs = []string{run.ID}
	return result, nil
}
