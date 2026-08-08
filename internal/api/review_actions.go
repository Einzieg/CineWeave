package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/jackc/pgx/v5"
)

type reviewRunActionInput struct {
	ReviewType                 string `json:"reviewType"`
	IncludeAgent               bool   `json:"includeAgent"`
	IncludeDeterministicChecks *bool  `json:"includeDeterministicChecks"`
}

type reviewGenerateFixActionInput struct {
	ItemID      string `json:"itemId"`
	Mode        string `json:"mode"`
	Instruction string `json:"instruction"`
}

type reviewApplyFixActionInput struct {
	FixID               string `json:"fixId"`
	ResolveReviewItem   *bool  `json:"resolveReviewItem"`
	TriggerRegeneration bool   `json:"triggerRegeneration"`
}

type reviewDismissFixActionInput struct {
	FixID string `json:"fixId"`
}

type reviewItemStatusActionInput struct {
	ItemID string `json:"itemId"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (s *Server) executeReviewRunAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input reviewRunActionInput
	if err := decodeReviewActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	response, err := s.runProjectReviewCore(ctx, principal, project, runProjectReviewRequest{
		ReviewType:                 input.ReviewType,
		UseAgent:                   input.IncludeAgent,
		IncludeDeterministicChecks: input.IncludeDeterministicChecks,
		ProjectControlCommandID:    command.ID,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("review.run", reviewActionArguments(raw), fmt.Sprintf("审阅已完成，生成 %d 个问题。", response.ItemCount), map[string]any{
		"reviewRunId": response.ReviewRunID,
		"status":      response.Status,
		"summary":     rawObject(response.Summary),
		"itemCount":   response.ItemCount,
		"useAgent":    input.IncludeAgent,
	}), nil
}

func (s *Server) executeReviewGenerateFixAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	var input reviewGenerateFixActionInput
	if err := decodeReviewActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.ItemID = strings.TrimSpace(input.ItemID)
	if input.ItemID == "" {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "itemId is required")
	}
	fix, err := s.generateReviewFixCore(ctx, principal, project, input.ItemID, generateReviewFixRequest{
		Mode: input.Mode, Instruction: input.Instruction, ProjectControlCommandID: command.ID,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("review.generate_fix", reviewActionArguments(raw), "已生成修复草稿，等待用户确认后才能应用。", reviewFixActionData(fix)), nil
}

func (s *Server) executeReviewApplyFixSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateReviewActionCommand(command, "review.apply_fix"); err != nil {
		return agentToolResult{}, err
	}
	var input reviewApplyFixActionInput
	if err := decodeReviewActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.FixID = strings.TrimSpace(input.FixID)
	if input.FixID == "" {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "fixId is required")
	}
	resolve := true
	if input.ResolveReviewItem != nil {
		resolve = *input.ResolveReviewItem
	}
	response, regenerateRequest, err := s.applyReviewFixActionTx(ctx, tx, principal, project, input.FixID, applyReviewFixOptions{
		ResolveReviewItem: resolve, TriggerRegeneration: input.TriggerRegeneration,
	})
	if err != nil {
		return agentToolResult{}, err
	}
	data := map[string]any{
		"fixId": response.FixID, "status": response.Status,
		"reviewItemStatus":  stringPtrValue(response.ReviewItemStatus),
		"resolveReviewItem": resolve, "triggerRegeneration": input.TriggerRegeneration,
		"regenerateRequest": rawObject(regenerateRequest),
	}
	if input.TriggerRegeneration && len(regenerateRequest) > 0 && string(regenerateRequest) != "null" {
		data["note"] = "修复已应用；再生请求需要通过生产工作流工具单独确认后执行。"
	}
	return agentToolOK("review.apply_fix", reviewActionArguments(raw), "已应用审阅修复。", data), nil
}

func (s *Server) executeReviewDismissFixSyncAction(
	ctx context.Context,
	tx pgx.Tx,
	_ auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	if err := validateReviewActionCommand(command, "review.dismiss_fix"); err != nil {
		return agentToolResult{}, err
	}
	var input reviewDismissFixActionInput
	if err := decodeReviewActionInput(raw, &input); err != nil {
		return agentToolResult{}, err
	}
	input.FixID = strings.TrimSpace(input.FixID)
	if input.FixID == "" {
		return agentToolResult{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "fixId is required")
	}
	response, err := s.dismissReviewFixActionTx(ctx, tx, project, input.FixID)
	if err != nil {
		return agentToolResult{}, err
	}
	return agentToolOK("review.dismiss_fix", reviewActionArguments(raw), "已忽略审阅修复。", map[string]any{
		"fixId": response.FixID, "status": response.Status,
	}), nil
}

func (s *Server) executeReviewItemStatusSyncAction(status string) projectControlSyncAction {
	return func(
		ctx context.Context,
		tx pgx.Tx,
		principal auth.Principal,
		project Project,
		command projectcontrol.Command,
		raw json.RawMessage,
	) (agentToolResult, error) {
		actionName := map[string]string{
			"resolved": "review.resolve_item",
			"ignored":  "review.ignore_item",
			"open":     "review.reopen_item",
		}[status]
		if err := validateReviewActionCommand(command, actionName); err != nil {
			return agentToolResult{}, err
		}
		var input reviewItemStatusActionInput
		if err := decodeReviewActionInput(raw, &input); err != nil {
			return agentToolResult{}, err
		}
		input.Status = status
		item, err := s.updateReviewItemStatusActionTx(ctx, tx, principal, project, input)
		if err != nil {
			return agentToolResult{}, err
		}
		return agentToolOK(actionName, reviewActionArguments(raw), "审阅项状态已更新。", map[string]any{"item": item}), nil
	}
}

func (s *Server) updateReviewItemStatusActionTx(ctx context.Context, tx pgx.Tx, principal auth.Principal, project Project, input reviewItemStatusActionInput) (ReviewItem, error) {
	input.ItemID = strings.TrimSpace(input.ItemID)
	input.Status = strings.TrimSpace(input.Status)
	if input.ItemID == "" || (input.Status != "open" && input.Status != "resolved" && input.Status != "ignored") {
		return ReviewItem{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "review item status request is invalid")
	}
	item, err := scanReviewItem(tx.QueryRow(ctx, reviewItemSelectSQL(`
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`), project.ID, input.ItemID))
	if err != nil {
		return ReviewItem{}, err
	}
	if item.Status != input.Status {
		if input.Status == "open" {
			_, err = tx.Exec(ctx, `
				UPDATE review_items
				SET status = 'open', resolved_by = NULL, resolved_at = NULL, resolution_note = NULL
				WHERE project_id = $1 AND id = $2
			`, project.ID, item.ID)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE review_items
				SET status = $3, resolved_by = $4, resolved_at = now(), resolution_note = NULLIF($5, '')
				WHERE project_id = $1 AND id = $2
			`, project.ID, item.ID, input.Status, principal.UserID, strings.TrimSpace(input.Note))
		}
		if err != nil {
			return ReviewItem{}, err
		}
		if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "review.item.status_changed", "review_item", item.ID, mustRawJSON(map[string]any{
			"reviewItemId": item.ID, "previousStatus": item.Status, "status": input.Status,
		})); err != nil {
			return ReviewItem{}, err
		}
	}
	return scanReviewItem(tx.QueryRow(ctx, reviewItemSelectSQL(`WHERE project_id = $1 AND id = $2`), project.ID, item.ID))
}

func reviewFixActionData(fix ReviewFix) map[string]any {
	return map[string]any{
		"fix": fix, "reviewFixId": fix.ID, "reviewItemId": fix.ReviewItemID,
		"status": fix.Status, "fixType": fix.FixType,
		"targetEntityType": fix.TargetEntityType, "targetEntityId": stringPtrValue(fix.TargetEntityID),
		"beforeSnapshot": rawObject(fix.BeforeSnapshot), "patch": rawObject(fix.Patch),
		"afterPreview": rawObject(fix.AfterPreview), "regenerateRequest": rawObject(fix.RegenerateRequest),
		"providerCallId":  stringPtrValue(fix.ProviderCallID),
		"promptVersionId": stringPtrValue(fix.PromptVersionID), "promptHash": stringPtrValue(fix.PromptHash),
	}
}

func decodeReviewActionInput(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "review action input is invalid")
	}
	return nil
}

func reviewActionArguments(raw json.RawMessage) map[string]any {
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil || arguments == nil {
		return map[string]any{}
	}
	return arguments
}

func validateReviewActionCommand(command projectcontrol.Command, actionName string) error {
	if strings.TrimSpace(command.ProjectID) == "" || strings.TrimSpace(command.ActorUserID) == "" || command.ActionName != actionName {
		return fmt.Errorf("project control command is invalid for %s", actionName)
	}
	return nil
}
