package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	"github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
)

func (s *Server) executeCommerceScriptDerivationPreviewAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	sourceScriptUnitID, err := s.resolveCommerceActionScriptUnitID(
		ctx, project, arguments, "sourceScriptUnitId", true,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	input := commercepkg.ScriptDerivationPreviewInput{
		SourceScriptUnitID: sourceScriptUnitID,
		Count:              agentIntArg(arguments, "count", 0, 1, commercepkg.ScriptDerivationMaxVariations),
		Dimension:          agentStringArg(arguments, "dimension"),
		Instruction:        agentStringArg(arguments, "instruction"),
		CandidateValues:    agentStringSliceArg(arguments, "candidateValues"),
		Preserve:           agentStringSliceArg(arguments, "preserve"),
	}
	prepared, err := s.commerceDerivations.PreparePreview(
		ctx, s.db, project.OrganizationID, project.ID, input,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	variables, err := prepared.PromptVariables()
	if err != nil {
		return agentToolResult{}, err
	}
	rendered, err := prompts.Render(prepared.Prompt, variables)
	if err != nil {
		return agentToolResult{}, err
	}
	rendered = prompts.WithOutputContract(rendered)
	idempotencyKey := "project-control-command:" + command.ID + ":commerce.script.derive.preview"
	response, err := provider.NewGatewayClientFromEnv().GenerateText(
		ctx,
		provider.GatewayTextRequest{
			GatewayBillingIdentity: gatewayBillingIdentityFromContext(
				ctx, authz.PermissionScriptRead, provider.BillingContextReasonAgentAction,
			),
			OrganizationID:    project.OrganizationID,
			WorkspaceID:       project.WorkspaceID,
			ProjectID:         project.ID,
			ModelProfileKey:   prepared.Routing.ModelProfileKey,
			ProviderModelID:   prepared.Routing.ProviderModelID,
			PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID:   rendered.PromptVersionID,
			PromptHash:        rendered.RenderedHash,
			PromptSource:      rendered.Source,
			IdempotencyKey:    idempotencyKey,
			Input: mustRawJSON(map[string]any{
				"prompt": rendered.RenderedText, "responseFormat": "json",
				"maxOutputTokens": 8000,
			}),
			Options: provider.GatewayTextOptions{IdempotencyKey: idempotencyKey},
		},
	)
	if err != nil {
		return agentToolResult{}, err
	}
	preview, err := commercepkg.DecodeScriptDerivationPreview(
		response.Output.Text, prepared.Input, prepared.Source,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	preview.ProviderRequestID = response.ProviderRequestID
	preview.ProviderCallID = response.ProviderCallID
	preview.ProviderModelID = response.ModelID
	preview.PromptTemplateKey = rendered.TemplateKey
	preview.PromptVersionID = rendered.PromptVersionID
	preview.PromptHash = rendered.RenderedHash
	data, err := agentCommerceValueData(preview)
	if err != nil {
		return agentToolResult{}, err
	}
	data["confirmation"] = map[string]any{
		"sourceScriptUnitId": preview.SourceScriptUnitID,
		"sourceScriptTitle":  preview.SourceScriptTitle,
		"dimension":          preview.Dimension,
		"count":              len(preview.Variations),
		"preserve":           preview.Preserve,
		"variations":         preview.Variations,
		"maySpendProvider":   true,
	}
	return agentToolOK(
		"commerce.script.derive.preview", arguments,
		"已生成脚本裂变候选", data,
	), nil
}

func (s *Server) executeCommerceScriptDerivationBatchAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	scriptUnitID, err := s.resolveCommerceActionScriptUnitID(
		ctx, project, arguments, "sourceScriptUnitId", true,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	input := commercepkg.CreateScriptDerivationInput{
		Dimension:   agentStringArg(arguments, "dimension"),
		Instruction: agentStringArg(arguments, "instruction"),
		Preserve:    agentStringSliceArg(arguments, "preserve"),
	}
	variationsRaw, err := json.Marshal(arguments["variations"])
	if err != nil {
		return agentToolResult{}, err
	}
	if err := json.Unmarshal(variationsRaw, &input.Variations); err != nil {
		return agentToolResult{}, controlValidationError("脚本裂变变体参数无效")
	}
	batch, replayed, err := s.createCommerceScriptDerivationCore(
		ctx, principal, project, scriptUnitID, input, command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	data, err := agentCommerceValueData(batch)
	if err != nil {
		return agentToolResult{}, err
	}
	data["idempotentReplay"] = replayed
	return agentToolOK("commerce.script.derive.batch", arguments, "脚本裂变任务已创建", data), nil
}

func (s *Server) createCommerceScriptDerivationCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	scriptUnitID string,
	input commercepkg.CreateScriptDerivationInput,
	idempotencyKey string,
) (commercepkg.ScriptDerivationBatch, bool, error) {
	if err := commercepkg.NormalizeScriptDerivationInput(&input); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	defer tx.Rollback(ctx)
	repository := commercepkg.NewRepository()
	source, err := repository.LoadScriptUnit(
		ctx, tx, project.OrganizationID, project.ID, scriptUnitID, true,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "sourceScriptUnitId": scriptUnitID,
		"sourceContentHash": source.CurrentContentHash, "input": input,
	})
	claim, err := claimIdempotencyTx(
		ctx, tx, project.OrganizationID,
		"commerce_script_derivation:create:"+scriptUnitID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.ScriptDerivationBatch
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		return replay, true, nil
	}
	workflowRunID := uuid.NewString()
	prepared, err := s.commerceDerivations.PrepareBatch(
		ctx, tx, commercepkg.PrepareScriptDerivationParams{
			BatchID: uuid.NewString(), WorkflowRunID: workflowRunID,
			OrganizationID: project.OrganizationID, ProjectID: project.ID,
			ScriptUnitID: scriptUnitID, CreatedBy: principal.UserID,
			IdempotencyKey: idempotencyKey, RequestHash: requestHash, Input: input,
		},
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	workflowInput := workflows.CommerceScriptDerivationBatchInput{
		OrganizationID: project.OrganizationID, ProjectID: project.ID,
		BatchID: prepared.Batch.ID, WorkflowRunID: workflowRunID,
		MaxConcurrency: 5, ProjectControlCommandID: idempotencyKey,
	}
	if err := workflows.EnqueueCommerceScriptDerivationBatchTx(
		ctx, tx, workflowInput, prepared.Production, principal.UserID,
	); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	run, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), workflowRunID))
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, run, run.WorkflowType); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := insertAPIEvent(
		ctx, tx, project.OrganizationID, project.ID,
		"commerce.script_derivation.batch.created",
		"commerce_script_derivation_batch", prepared.Batch.ID,
		commerceScriptDerivationBatchEventPayload(prepared.Batch),
	); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := completeIdempotencyTxWithStatus(
		ctx, tx, claim.state, http.StatusAccepted, prepared.Batch,
	); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	return prepared.Batch, false, nil
}

func (s *Server) executeCommerceScriptDerivationRetryAsyncAction(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	batchID := agentReferenceStringArg(arguments, "batchId")
	batch, replayed, err := s.retryCommerceScriptDerivationCore(
		ctx, principal, project, batchID, command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	data, err := agentCommerceValueData(batch)
	if err != nil {
		return agentToolResult{}, err
	}
	data["idempotentReplay"] = replayed
	return agentToolOK("commerce.script.derive.retry_failed", arguments, "失败变体重试任务已创建", data), nil
}

func (s *Server) retryCommerceScriptDerivationCore(
	ctx context.Context,
	principal auth.Principal,
	project Project,
	sourceBatchID string,
	idempotencyKey string,
) (commercepkg.ScriptDerivationBatch, bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	defer tx.Rollback(ctx)
	source, err := s.commerceDerivations.GetBatch(
		ctx, tx, project.OrganizationID, project.ID, sourceBatchID, false,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	failedInputHashes := make([]string, 0)
	for _, item := range source.Items {
		if item.Status == "failed_retryable" {
			failedInputHashes = append(failedInputHashes, item.InputHash)
		}
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "sourceBatchId": sourceBatchID,
		"failedInputHashes": failedInputHashes,
	})
	claim, err := claimIdempotencyTx(
		ctx, tx, project.OrganizationID,
		"commerce_script_derivation:retry_failed:"+sourceBatchID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if len(claim.replaySnapshot) > 0 {
		var replay commercepkg.ScriptDerivationBatch
		if err := json.Unmarshal(claim.replaySnapshot, &replay); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		return replay, true, nil
	}
	workflowRunID := uuid.NewString()
	prepared, err := s.commerceDerivations.PrepareRetryBatch(
		ctx, tx, sourceBatchID, workflowRunID,
		project.OrganizationID, project.ID, principal.UserID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	workflowInput := workflows.CommerceScriptDerivationBatchInput{
		OrganizationID: project.OrganizationID, ProjectID: project.ID,
		BatchID: prepared.Batch.ID, WorkflowRunID: workflowRunID,
		MaxConcurrency: 5, ProjectControlCommandID: idempotencyKey,
	}
	if err := workflows.EnqueueCommerceScriptDerivationBatchTx(
		ctx, tx, workflowInput, prepared.Production, principal.UserID,
	); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	run, err := scanWorkflowRun(tx.QueryRow(ctx, workflowRunSelectSQL(`WHERE id = $1`), workflowRunID))
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := insertWorkflowQueuedEventTx(ctx, tx, run, run.WorkflowType); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := insertAPIEvent(
		ctx, tx, project.OrganizationID, project.ID,
		"commerce.script_derivation.batch.created",
		"commerce_script_derivation_batch", prepared.Batch.ID,
		commerceScriptDerivationBatchEventPayload(prepared.Batch),
	); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := completeIdempotencyTxWithStatus(
		ctx, tx, claim.state, http.StatusAccepted, prepared.Batch,
	); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	return prepared.Batch, false, nil
}

func (s *Server) executeCommerceScriptDerivationCancelAsyncAction(
	ctx context.Context,
	_ auth.Principal,
	project Project,
	command projectcontrol.Command,
	raw json.RawMessage,
) (agentToolResult, error) {
	arguments, err := decodeCommerceActionMap(raw)
	if err != nil {
		return agentToolResult{}, err
	}
	batch, replayed, err := s.cancelCommerceScriptDerivationCore(
		ctx, project, agentReferenceStringArg(arguments, "batchId"),
		agentStringArg(arguments, "reason"), command.ID,
	)
	if err != nil {
		return agentToolResult{}, err
	}
	data, err := agentCommerceValueData(batch)
	if err != nil {
		return agentToolResult{}, err
	}
	data["idempotentReplay"] = replayed
	return agentToolOK("commerce.script.derive.cancel", arguments, "脚本裂变取消请求已提交", data), nil
}

func (s *Server) cancelCommerceScriptDerivationCore(
	ctx context.Context,
	project Project,
	batchID string,
	reason string,
	idempotencyKey string,
) (commercepkg.ScriptDerivationBatch, bool, error) {
	batch, err := s.commerceDerivations.GetBatch(
		ctx, s.db, project.OrganizationID, project.ID, batchID, false,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if commerceScriptDerivationTerminal(batch.Status) {
		return batch, false, nil
	}
	if batch.WorkflowRunID == nil || strings.TrimSpace(*batch.WorkflowRunID) == "" {
		return commercepkg.ScriptDerivationBatch{}, false, commercepkg.Error{
			Code:    commercepkg.CodeScriptDerivationState,
			Message: "脚本裂变任务缺少可取消的工作流",
		}
	}
	requestHash := idempotencyRequestHash(map[string]any{
		"projectId": project.ID, "batchId": batchID, "reason": strings.TrimSpace(reason),
	})
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	defer tx.Rollback(ctx)
	claim, err := claimIdempotencyTx(
		ctx, tx, project.OrganizationID,
		"commerce_script_derivation:cancel:"+batchID,
		idempotencyKey, requestHash,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	replayed := len(claim.replaySnapshot) > 0
	if !replayed {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_script_derivation_batches
			SET status = 'cancelling', revision = revision + 1, updated_at = now()
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
			  AND status IN ('queued', 'running')
		`, batch.ID, project.OrganizationID, project.ID); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		batch.Status = "cancelling"
		if err := insertAPIEvent(
			ctx, tx, project.OrganizationID, project.ID,
			"commerce.script_derivation.batch.cancelling",
			"commerce_script_derivation_batch", batch.ID,
			commerceScriptDerivationBatchEventPayload(batch),
		); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		if err := completeIdempotencyTxWithStatus(
			ctx, tx, claim.state, http.StatusAccepted, batch,
		); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	current, err := s.commerceDerivations.GetBatch(
		ctx, s.db, project.OrganizationID, project.ID, batchID, false,
	)
	if err != nil {
		return commercepkg.ScriptDerivationBatch{}, false, err
	}
	if !commerceScriptDerivationTerminal(current.Status) {
		if err := s.requestCommerceScriptDerivationCancellationContext(
			ctx, project, current, strings.TrimSpace(reason),
		); err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
		current, err = s.commerceDerivations.GetBatch(
			ctx, s.db, project.OrganizationID, project.ID, batchID, false,
		)
		if err != nil {
			return commercepkg.ScriptDerivationBatch{}, false, err
		}
	}
	return current, replayed, nil
}

func (s *Server) requestCommerceScriptDerivationCancellationContext(
	ctx context.Context,
	project Project,
	batch commercepkg.ScriptDerivationBatch,
	reason string,
) error {
	if batch.WorkflowRunID == nil || strings.TrimSpace(*batch.WorkflowRunID) == "" {
		return commercepkg.Error{
			Code:    commercepkg.CodeScriptDerivationState,
			Message: "脚本裂变任务缺少可取消的工作流",
		}
	}
	run, err := scanWorkflowRun(s.db.QueryRow(
		ctx, workflowRunSelectSQL(`
			WHERE id = $1 AND organization_id = $2 AND project_id = $3
		`), *batch.WorkflowRunID, project.OrganizationID, project.ID,
	))
	if err != nil {
		return err
	}
	if reason == "" {
		reason = "用户取消脚本裂变任务"
	}
	_, err = s.cancelWorkflowRunItem(ctx, run, reason)
	return err
}
