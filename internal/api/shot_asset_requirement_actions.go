package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

type shotAssetRequirementReviewActionInput struct {
	RequirementIDs  []string `json:"requirementIds,omitempty"`
	ScriptEpisodeID string   `json:"scriptEpisodeId,omitempty"`
	ReviewStatus    string   `json:"reviewStatus"`
	Note            string   `json:"note,omitempty"`
}

type shotAssetRequirementUpdateActionInput struct {
	RequirementID string         `json:"requirementId"`
	Patch         map[string]any `json:"patch"`
}

type shotAssetRequirementSkipActionInput struct {
	RequirementID string `json:"requirementId"`
	Reason        string `json:"reason"`
}

type shotAssetDerivedImageActionInput struct {
	RequirementID string `json:"requirementId"`
}

func (s *Server) executeShotAssetDerivedImageAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input shotAssetDerivedImageActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.createDerivedAssetImageAction(
		ctx,
		principal,
		project,
		input.RequirementID,
		"project-control-command:"+command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := shotAssetRequirementActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	response := workflowStartedAgentResult(
		"shot_asset.generate_derived_image",
		arguments,
		result.WorkflowRun,
		map[string]any{"requirementId": strings.TrimSpace(input.RequirementID)},
		result.IdempotentReplay,
	)
	response.Data["operationId"] = result.OperationID
	response.Data["derivedAssets"] = result.Batch
	return response, nil
}

func (s *Server) createDerivedAssetImageAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	requirementID string,
	idempotencyKey string,
) (DerivedAssetBatchCommandResult, error) {
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return DerivedAssetBatchCommandResult{}, controlValidationError("requirementId 不能为空")
	}
	return s.createDerivedAssetBatchRun(ctx, principal, project, DerivedAssetBatchCreateOptions{
		Mode:                    derivedAssetBatchModeExplicit,
		RequirementIDs:          []string{requirementID},
		MaxConcurrency:          1,
		ExpectedProjectRevision: project.Revision,
		IdempotencyKey:          strings.TrimSpace(idempotencyKey),
	})
}

func (s *Server) executeShotAssetRequirementReviewSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateShotAssetRequirementActionCommand(command, "shot_asset.review_requirements"); err != nil {
		return agentToolResult{}, err
	}
	var input shotAssetRequirementReviewActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	result, err := s.batchReviewShotAssetRequirementsActionTx(ctx, tx, project, principal.UserID, string(command.ControllerType), BatchReviewShotAssetRequirementsRequest{
		RequirementIDs: input.RequirementIDs, ScriptEpisodeID: input.ScriptEpisodeID,
		ReviewStatus: input.ReviewStatus, Note: input.Note,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := shotAssetRequirementActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	summary := fmt.Sprintf("已审核 %d 个镜头资产需求：批准 %d 个，需修改 %d 个。", result.TotalItems, result.ApprovedCount, result.NeedsEditCount)
	if result.BlockedCount > 0 {
		summary += fmt.Sprintf(" %d 个需求未通过结构化校验，未被批准。", result.BlockedCount)
	}
	return agentToolOK("shot_asset.review_requirements", arguments, summary, map[string]any{"report": result}), nil
}

func (s *Server) executeShotAssetRequirementUpdateSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateShotAssetRequirementActionCommand(command, "shot_asset.update_requirement"); err != nil {
		return agentToolResult{}, err
	}
	var input shotAssetRequirementUpdateActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	if input.RequirementID == "" {
		return agentToolResult{}, controlValidationError("requirementId 不能为空")
	}
	item, err := s.updateShotAssetRequirementActionTx(
		ctx, tx, project, principal.UserID, string(command.ControllerType),
		input.RequirementID, updateShotAssetRequirementRequestFromPatch(input.Patch),
	)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := shotAssetRequirementActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("shot_asset.update_requirement", arguments, "已修正镜头资产需求；该需求需要重新审核后才能生成衍生图。", map[string]any{
		"requirement": item,
		"nextAction": map[string]any{
			"tool": "shot_asset.review_requirements",
			"args": map[string]any{
				"requirementIds": []string{item.ID}, "reviewStatus": "approved",
				"note": "修正后重新执行结构化校验",
			},
		},
	}), nil
}

func (s *Server) executeShotAssetRequirementSkipSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateShotAssetRequirementActionCommand(command, "shot_asset.skip_requirement"); err != nil {
		return agentToolResult{}, err
	}
	var input shotAssetRequirementSkipActionInput
	if err := decodeControlInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.RequirementID == "" || input.Reason == "" {
		return agentToolResult{}, controlValidationError("requirementId 和 reason 不能为空")
	}
	item, err := s.skipShotAssetRequirementActionTx(
		ctx, tx, project, principal.UserID, string(command.ControllerType), input.Reason, input.RequirementID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	arguments, err := shotAssetRequirementActionArguments(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("shot_asset.skip_requirement", arguments, "已跳过不适用于当前镜头的资产需求，并保留审计记录。", map[string]any{
		"requirement": item,
	}), nil
}

func shotAssetRequirementActionArguments(raw json.RawMessage) (map[string]any, error) {
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, fmt.Errorf("decode shot asset requirement arguments: %w", err)
	}
	return arguments, nil
}

func validateShotAssetRequirementActionCommand(command projectcontrol.Command, action string) error {
	if strings.TrimSpace(command.ProjectID) == "" || strings.TrimSpace(command.ActorUserID) == "" {
		return fmt.Errorf("%s command identity is incomplete", action)
	}
	return nil
}
