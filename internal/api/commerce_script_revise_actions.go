package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

type commerceScriptReviseActionInput struct {
	commerceScriptSelectionActionInput
	ExpectedRevision int64    `json:"expectedRevision"`
	Instruction      string   `json:"instruction"`
	TargetMaxLength  int      `json:"targetMaxLength"`
	TargetLengthUnit string   `json:"targetLengthUnit"`
	Preserve         []string `json:"preserve"`
}

func (s *Server) executeCommerceScriptReviseAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, scriptUnitID, err := s.resolveCommerceScriptSelectionForAction(ctx, project, raw, "scriptUnitId")
	if err != nil {
		return agentToolResult{}, err
	}
	input, err := decodeCommerceScriptActionInput[commerceScriptReviseActionInput](raw, "广告脚本改写参数无效")
	if err != nil {
		return agentToolResult{}, err
	}
	input.ScriptUnitID = scriptUnitID
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.ExpectedRevision <= 0 || input.Instruction == "" {
		return agentToolResult{}, controlValidationError("请选择广告脚本，并提供当前 revision 和改写要求")
	}
	if replay, ok, err := s.commerceScriptRevisionCommandReplay(ctx, project.ID, command.ID); err != nil {
		return agentToolResult{}, err
	} else if ok {
		return agentToolOK("commerce.script.revise", arguments, "广告脚本已按相同命令完成改写", replay), nil
	}

	current, err := s.commerceCatalog.GetScriptUnit(ctx, s.db, project.OrganizationID, project.ID, input.ScriptUnitID)
	if err != nil {
		return agentToolResult{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return agentToolResult{}, commercepkg.Error{
			Code: commercepkg.CodeScriptUnitRevision, Message: "脚本已被其他操作修改，请刷新后重试",
		}
	}
	sourceContent := strings.TrimSpace(current.CurrentContent)
	if sourceContent == "" {
		return agentToolResult{}, commercepkg.Error{
			Code: commercepkg.CodeScriptDerivationSourceEmpty, Message: "广告脚本当前正文为空",
		}
	}
	constraint, err := s.resolveCommerceScriptRevisionConstraint(ctx, project, arguments)
	if err != nil {
		return agentToolResult{}, err
	}
	product, err := s.commerceCatalog.GetProduct(ctx, s.db, project.OrganizationID, project.ID)
	if err != nil {
		return agentToolResult{}, err
	}
	preserve := input.Preserve
	if len(preserve) == 0 {
		preserve = []string{"product_facts", "selling_points", "prohibited_claims", "language", "cta"}
	}

	request := requestWithContext(ctx)
	previousLength := 0
	var (
		content           string
		runID             string
		rendered          promptsvc.RenderedPrompt
		gatewayResp       provider.GatewayTextResponse
		successfulAttempt int
	)
	for attempt := 1; attempt <= commerceScriptReviseMaxAttempts; attempt++ {
		options := s.scriptRewritePromptOptions(
			ctx, project, command, "commerce.script.revise", current.Title, authz.PermissionScriptWrite,
		)
		options.Stream = true
		options.IdempotencyKey += fmt.Sprintf(":attempt:%d", attempt)
		if command.AgentTaskID != "" && command.AgentStepID != "" {
			options.OnProgress = s.agentCommerceScriptReviseProgressCallback(
				ctx, project, AgentTask{ID: command.AgentTaskID}, AgentStep{ID: command.AgentStepID},
				input.StableOrdinal, current.Title, attempt,
			)
		}
		content, runID, rendered, gatewayResp, err = s.runScriptAgentPromptWithOptions(
			request, principal, project, nil, "commerce_script_revise", "script_agent_rewrite",
			commerceScriptRevisionPromptVariables(
				project, current, sourceContent,
				commerceScriptRevisionInstruction(
					input.Instruction, preserve, product, constraint, attempt, previousLength,
				),
			),
			options,
		)
		if err != nil {
			return agentToolResult{}, err
		}
		content = trimCommerceScriptRewriteOutput(content)
		outputLength := commercepkg.MeasureDirectVideoPromptLength(content, constraint.Unit)
		if content != "" && (constraint.MaxLength <= 0 || outputLength <= constraint.MaxLength) {
			successfulAttempt = attempt
			break
		}
		previousLength = outputLength
		code := "COMMERCE_SCRIPT_REVISION_OUTPUT_EMPTY"
		message := "脚本改写模型返回了空正文"
		if content != "" {
			code = "COMMERCE_SCRIPT_REVISION_OUTPUT_TOO_LONG"
			message = fmt.Sprintf(
				"脚本改写结果长度为 %d，仍超过目标上限 %d（%s）",
				outputLength, constraint.MaxLength, commerceScriptLengthUnitLabel(constraint.Unit),
			)
		}
		s.failCommerceScriptRevisionRun(ctx, runID, code, message, map[string]any{
			"attempt": attempt, "outputLength": outputLength,
			"maxLength": constraint.MaxLength, "lengthUnit": constraint.Unit,
		})
		content = ""
	}
	if content == "" {
		return agentToolResult{}, apiError{
			Status: http.StatusBadGateway, Code: "COMMERCE_SCRIPT_REVISION_OUTPUT_INVALID",
			Message: "脚本改写连续 3 次未满足当前视频模型长度要求，请缩小改写范围后重试",
			Details: map[string]any{
				"maxAttempts": commerceScriptReviseMaxAttempts,
				"maxLength":   constraint.MaxLength, "lengthUnit": constraint.Unit,
			},
		}
	}
	if err := s.validateCommerceScriptContentForCurrentVideoModel(ctx, project, content); err != nil {
		s.failCommerceScriptRevisionRun(ctx, runID, "COMMERCE_SCRIPT_REVISION_OUTPUT_INVALID", err.Error(), nil)
		return agentToolResult{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		s.failCommerceScriptRevisionRun(ctx, runID, "DATABASE_ERROR", err.Error(), nil)
		return agentToolResult{}, err
	}
	defer tx.Rollback(ctx)
	updated, err := s.updateCommerceScriptUnitActionTx(ctx, tx, project, principal.UserID, commerceScriptUpdateActionInput{
		commerceScriptSelectionActionInput: commerceScriptSelectionActionInput{ScriptUnitID: current.ID},
		ExpectedRevision:                   input.ExpectedRevision,
		DraftContent:                       &content,
	})
	if err != nil {
		s.failCommerceScriptRevisionRun(ctx, runID, "COMMERCE_SCRIPT_REVISION_UPDATE_FAILED", err.Error(), nil)
		return agentToolResult{}, err
	}
	outputLength := commercepkg.MeasureDirectVideoPromptLength(content, constraint.Unit)
	output := map[string]any{
		"scriptUnitId": updated.ID, "stableOrdinal": input.StableOrdinal,
		"title": updated.Title, "revision": updated.Revision,
		"contentHash":  updated.CurrentContentHash,
		"sourceLength": commercepkg.MeasureDirectVideoPromptLength(sourceContent, constraint.Unit),
		"outputLength": outputLength, "maxLength": constraint.MaxLength,
		"lengthUnit": constraint.Unit, "constraintSource": constraint.Source,
		"attempts": successfulAttempt, "agentRunId": runID,
		"providerRequestId": gatewayResp.ProviderRequestID,
		"providerCallId":    gatewayResp.ProviderCallID, "modelId": gatewayResp.ModelID,
		"promptVersionId": rendered.PromptVersionID, "promptHash": rendered.RenderedHash,
		"projectControlCommandId": command.ID,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agent_runs
		SET status = 'succeeded', output = $2, provider_call_id = NULLIF($3, '')::uuid,
		    prompt_version_id = $4, prompt_hash = $5, completed_at = now()
		WHERE id = $1
	`, runID, mustMarshal(output), gatewayResp.ProviderCallID, rendered.PromptVersionID, rendered.RenderedHash); err != nil {
		s.failCommerceScriptRevisionRun(ctx, runID, "AGENT_RUN_FINALIZE_FAILED", err.Error(), nil)
		return agentToolResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		s.failCommerceScriptRevisionRun(ctx, runID, "DATABASE_COMMIT_FAILED", err.Error(), nil)
		return agentToolResult{}, err
	}
	targetLabel := "广告脚本“" + updated.Title + "”"
	if input.StableOrdinal > 0 {
		targetLabel = fmt.Sprintf("第 %d 条广告脚本", input.StableOrdinal)
	}
	return agentToolOK(
		"commerce.script.revise", arguments,
		fmt.Sprintf("已按要求改写%s，当前长度 %d/%d（%s）。",
			targetLabel, outputLength, constraint.MaxLength, commerceScriptLengthUnitLabel(constraint.Unit)),
		output,
	), nil
}

func (s *Server) commerceScriptRevisionCommandReplay(
	ctx context.Context,
	projectID string,
	commandID string,
) (map[string]any, bool, error) {
	if strings.TrimSpace(commandID) == "" {
		return nil, false, nil
	}
	var raw json.RawMessage
	err := s.db.QueryRow(ctx, `
		SELECT output
		FROM agent_runs
		WHERE project_id = $1
		  AND project_control_command_id = $2
		  AND task_type = 'commerce_script_revise'
		  AND status = 'succeeded'
		LIMIT 1
	`, projectID, commandID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil {
		return nil, false, err
	}
	return output, true, nil
}
