package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/jackc/pgx/v5"
)

func (r *CommerceGenerationRuntime) ClaimSalesScriptContract(
	ctx context.Context,
	input CommerceSalesScriptContractClaimInput,
) (CommerceSalesScriptContractClaimResult, error) {
	phase, identity, workflowRunID, _, workflowInput, err := commerceSalesScriptRuntimeOwner(input.Owner)
	if err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockGenerationState(ctx, tx, identity)
	if err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	if _, err := r.lockWorkflowRun(ctx, tx, workflowRunID, phase, workflowInput); err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	snapshot, err := r.buildStoryboardSnapshot(ctx, tx, state)
	if err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	contractState, found, err := loadCommerceSalesScriptContractState(ctx, tx, identity.UnitGenerationID)
	if err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	if !found {
		contractState, err = insertCommerceSalesScriptContract(
			ctx, tx, state, snapshot, workflowRunID, commerceSalesScriptOwnerCreatedBy(input.Owner),
		)
		if err != nil {
			return CommerceSalesScriptContractClaimResult{}, err
		}
		contractState.Owner = true
	} else {
		if contractState.InputHash != snapshot.InputHash {
			return CommerceSalesScriptContractClaimResult{}, generationMismatch("销售脚本契约与当前脚本单元生产输入不一致", nil)
		}
		switch contractState.Status {
		case "ready":
			if err := validatePersistedCommerceSalesScript(contractState, snapshot); err != nil {
				return CommerceSalesScriptContractClaimResult{}, err
			}
		case "running":
			if contractState.OwnerWorkflowRunID == workflowRunID {
				contractState.Owner = true
			} else {
				ownerStatus, ownerErr := loadCommerceWorkflowStatus(ctx, tx, contractState.OwnerWorkflowRunID)
				if ownerErr != nil {
					return CommerceSalesScriptContractClaimResult{}, ownerErr
				}
				if isTerminalCommerceWorkflowStatus(ownerStatus) {
					contractState, err = reclaimCommerceSalesScriptContract(ctx, tx, contractState, workflowRunID)
					if err != nil {
						return CommerceSalesScriptContractClaimResult{}, err
					}
					contractState.Owner = true
				}
			}
		case "failed", "cancelled":
			contractState, err = reclaimCommerceSalesScriptContract(ctx, tx, contractState, workflowRunID)
			if err != nil {
				return CommerceSalesScriptContractClaimResult{}, err
			}
			contractState.Owner = true
		default:
			return CommerceSalesScriptContractClaimResult{}, generationMismatch("销售脚本契约状态无效", nil)
		}
	}
	result := CommerceSalesScriptContractClaimResult{Snapshot: snapshot, State: contractState}
	if phase == CommercePhaseScriptOrganization && contractState.Status == "ready" {
		output := commerceScriptOrganizationOutput(identity, contractState, nil)
		if err := finalizeCommerceWorkflowSuccessTx(ctx, tx, workflowRunID, output); err != nil {
			return CommerceSalesScriptContractClaimResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceSalesScriptContractClaimResult{}, err
	}
	return result, nil
}

func (r *CommerceGenerationRuntime) CommitSalesScriptContract(
	ctx context.Context,
	input CommerceSalesScriptContractCommitInput,
) (CommerceSalesScriptContractState, error) {
	phase, identity, workflowRunID, attemptGeneration, workflowInput, err := commerceSalesScriptRuntimeOwner(input.Owner)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if err := ValidateCommerceSalesScript(input.Contract, input.Snapshot); err != nil {
		return CommerceSalesScriptContractState{}, commerce.Error{
			Code: CommerceCodeStoryboardContractInvalid, Message: "销售脚本契约无效", Cause: err,
		}
	}
	contractHash, err := commerceContractHash(input.Contract)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	defer tx.Rollback(ctx)
	state, err := r.lockGenerationState(ctx, tx, identity)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if err := assertCommerceWorkflowRunIdentity(ctx, tx, workflowRunID, phase, workflowInput); err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	fresh, err := r.buildStoryboardSnapshot(ctx, tx, state)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if err := assertCommerceSnapshotEqual(input.Snapshot, fresh, "销售脚本契约提交"); err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	current, found, err := loadCommerceSalesScriptContractState(ctx, tx, identity.UnitGenerationID)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if found && current.Status == "ready" && current.OwnerWorkflowRunID == workflowRunID &&
		current.InputHash == fresh.InputHash && current.ContractHash == contractHash {
		if err := validatePersistedCommerceSalesScript(current, fresh); err != nil {
			return CommerceSalesScriptContractState{}, err
		}
		if err := assertCommerceSnapshotEqual(current.Contract, input.Contract, "销售脚本契约重放"); err != nil {
			return CommerceSalesScriptContractState{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CommerceSalesScriptContractState{}, err
		}
		current.Owner = true
		return current, nil
	}
	if _, err := r.lockWorkflowRun(ctx, tx, workflowRunID, phase, workflowInput); err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if !found || current.Status != "running" || current.OwnerWorkflowRunID != workflowRunID || current.InputHash != fresh.InputHash {
		return CommerceSalesScriptContractState{}, commerce.Error{Code: commerce.CodeScriptOrganizationBusy, Message: "销售脚本契约已由其他工作流接管"}
	}
	callInput, err := commerceSalesScriptAgentCall(input.Owner, fresh, input.Provenance.Round, nil)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	callInput.AttemptGeneration = attemptGeneration
	if err := r.assertAgentProvenance(ctx, tx, callInput, input.Provenance); err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	contractRaw, err := json.Marshal(input.Contract)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_sales_script_contracts
		SET status = 'ready', contract_version = $2, contract = $3,
		    contract_hash = $4, prompt_version_id = NULLIF($5, '')::uuid,
		    provider_request_id = NULLIF($6, '')::uuid,
		    provider_call_id = NULLIF($7, '')::uuid,
		    provider_model_id = NULLIF($8, '')::uuid,
		    accepted_round = $9, completed_at = now(), updated_at = now(),
		    error_code = NULL, error_message = NULL
		WHERE id = $1 AND status = 'running'
		  AND current_workflow_run_id = $10 AND input_hash = $11
	`, current.ContractID, input.Contract.ContractVersion, contractRaw, contractHash,
		input.Provenance.PromptVersionID, input.Provenance.ProviderRequestID,
		input.Provenance.ProviderCallID, input.Provenance.ProviderModelID,
		input.Provenance.Round, workflowRunID, fresh.InputHash)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if tag.RowsAffected() != 1 {
		return CommerceSalesScriptContractState{}, commerce.Error{Code: commerce.CodeScriptOrganizationBusy, Message: "销售脚本契约提交所有权已变化"}
	}
	current.Status = "ready"
	current.Owner = true
	current.Contract = input.Contract
	current.ContractHash = contractHash
	if phase == CommercePhaseScriptOrganization {
		output := commerceScriptOrganizationOutput(identity, current, []CommerceAgentProvenance{input.Provenance})
		if err := finalizeCommerceWorkflowSuccessTx(ctx, tx, workflowRunID, output); err != nil {
			return CommerceSalesScriptContractState{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	return current, nil
}

func commerceSalesScriptRuntimeOwner(
	owner CommerceSalesScriptOwner,
) (CommerceWorkflowPhase, commerce.UnitGenerationIdentity, string, int, any, error) {
	phase, identity, workflowRunID, attemptGeneration, err := commerceSalesScriptOwnerIdentity(owner)
	if err != nil {
		return "", commerce.UnitGenerationIdentity{}, "", 0, nil, err
	}
	if owner.OrganizationInput != nil {
		return phase, identity, workflowRunID, attemptGeneration, *owner.OrganizationInput, nil
	}
	return phase, identity, workflowRunID, attemptGeneration, *owner.StoryboardInput, nil
}

func commerceSalesScriptOwnerCreatedBy(owner CommerceSalesScriptOwner) string {
	if owner.OrganizationInput != nil {
		return owner.OrganizationInput.CreatedBy
	}
	if owner.StoryboardInput != nil {
		return owner.StoryboardInput.CreatedBy
	}
	return ""
}

func loadCommerceSalesScriptContractState(
	ctx context.Context,
	tx pgx.Tx,
	unitGenerationID string,
) (CommerceSalesScriptContractState, bool, error) {
	var item CommerceSalesScriptContractState
	var contractRaw []byte
	err := tx.QueryRow(ctx, `
		SELECT id::text, status, attempt_generation, current_workflow_run_id::text,
		       input_hash, contract, COALESCE(contract_hash, '')
		FROM commerce_sales_script_contracts
		WHERE script_unit_generation_id = $1
		FOR UPDATE
	`, unitGenerationID).Scan(
		&item.ContractID, &item.Status, &item.AttemptGeneration,
		&item.OwnerWorkflowRunID, &item.InputHash, &contractRaw, &item.ContractHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommerceSalesScriptContractState{}, false, nil
	}
	if err != nil {
		return CommerceSalesScriptContractState{}, false, err
	}
	if len(contractRaw) > 0 {
		if err := json.Unmarshal(contractRaw, &item.Contract); err != nil {
			return CommerceSalesScriptContractState{}, false, err
		}
	}
	return item, true, nil
}

func insertCommerceSalesScriptContract(
	ctx context.Context,
	tx pgx.Tx,
	state commerceGenerationFrozenState,
	snapshot CommerceStoryboardPlanningSnapshot,
	workflowRunID string,
	createdBy string,
) (CommerceSalesScriptContractState, error) {
	var item CommerceSalesScriptContractState
	err := tx.QueryRow(ctx, `
		INSERT INTO commerce_sales_script_contracts(
			organization_id, project_id, product_id, script_unit_id,
			script_unit_generation_id, project_production_generation_id,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, status, attempt_generation,
			current_workflow_run_id, input_hash, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
		        'running', 1, $13, $14, NULLIF($15, '')::uuid)
		RETURNING id::text, status, attempt_generation,
		          current_workflow_run_id::text, input_hash
	`, state.Generation.Identity.OrganizationID, state.Generation.Identity.ProjectID,
		state.Generation.Identity.ProductID, state.Generation.Identity.ScriptUnitID,
		state.Generation.Identity.UnitGenerationID, state.Generation.Identity.ProjectGenerationID,
		state.Generation.Identity.CommerceWorkflowBindingID,
		state.Generation.Identity.CommerceWorkflowBindingRevision,
		state.Generation.ProductVersionID, state.Generation.SourceScriptVersionID,
		state.Generation.LocalizationID, state.Generation.ReferencePackID,
		workflowRunID, snapshot.InputHash, createdBy).Scan(
		&item.ContractID, &item.Status, &item.AttemptGeneration,
		&item.OwnerWorkflowRunID, &item.InputHash,
	)
	return item, err
}

func reclaimCommerceSalesScriptContract(
	ctx context.Context,
	tx pgx.Tx,
	current CommerceSalesScriptContractState,
	workflowRunID string,
) (CommerceSalesScriptContractState, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_sales_script_contracts
		SET status = 'running', attempt_generation = attempt_generation + 1,
		    current_workflow_run_id = $2, started_at = now(), completed_at = NULL,
		    error_code = NULL, error_message = NULL, updated_at = now()
		WHERE id = $1 AND status = $3 AND current_workflow_run_id = $4
	`, current.ContractID, workflowRunID, current.Status, current.OwnerWorkflowRunID)
	if err != nil {
		return CommerceSalesScriptContractState{}, err
	}
	if tag.RowsAffected() != 1 {
		return CommerceSalesScriptContractState{}, commerce.Error{Code: commerce.CodeScriptOrganizationBusy, Message: "销售脚本契约已由其他工作流接管"}
	}
	current.Status = "running"
	current.AttemptGeneration++
	current.OwnerWorkflowRunID = workflowRunID
	current.Contract = CommerceSalesScriptContract{}
	current.ContractHash = ""
	return current, nil
}

func validatePersistedCommerceSalesScript(
	state CommerceSalesScriptContractState,
	snapshot CommerceStoryboardPlanningSnapshot,
) error {
	if state.Status != "ready" || strings.TrimSpace(state.ContractHash) == "" {
		return generationMismatch("销售脚本契约尚未就绪", nil)
	}
	if err := ValidateCommerceSalesScript(state.Contract, snapshot); err != nil {
		return commerce.Error{Code: CommerceCodeStoryboardContractInvalid, Message: "已冻结销售脚本契约无效", Cause: err}
	}
	hash, err := commerceContractHash(state.Contract)
	if err != nil {
		return err
	}
	if hash != state.ContractHash {
		return generationMismatch("销售脚本契约内容 hash 不一致", nil)
	}
	return nil
}

func (r *CommerceGenerationRuntime) assertSalesScriptContract(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceStoryboardPlanCommit,
) error {
	state, found, err := loadCommerceSalesScriptContractState(
		ctx,
		tx,
		input.WorkflowInput.Identity.UnitGenerationID,
	)
	if err != nil {
		return err
	}
	if !found || state.ContractID != input.SalesScriptContractID || state.Status != "ready" ||
		state.InputHash != input.Snapshot.InputHash || state.ContractHash != input.SalesScriptContractHash {
		return commerce.Error{Code: commerce.CodeScriptOrganizationNeed, Message: "已冻结销售脚本契约不存在或已失效"}
	}
	if err := validatePersistedCommerceSalesScript(state, input.Snapshot); err != nil {
		return err
	}
	if err := assertCommerceSnapshotEqual(state.Contract, input.SalesScript, "销售脚本契约"); err != nil {
		return err
	}
	return nil
}

func loadCommerceWorkflowStatus(ctx context.Context, tx pgx.Tx, workflowRunID string) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", generationMismatch("销售脚本契约所有者工作流不存在", err)
	}
	return status, err
}

func isTerminalCommerceWorkflowStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", "partial_succeeded":
		return true
	default:
		return false
	}
}

func (r *CommerceGenerationRuntime) lockCommerceSalesScriptAgentWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceAgentCallInput,
) error {
	if input.GenerationIdentity == nil {
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "销售脚本 Agent 缺少脚本单元生产身份"}
	}
	record, err := loadCommerceWorkflowRunRecord(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return err
	}
	switch input.Phase {
	case CommercePhaseScriptOrganization:
		var workflowInput CommerceScriptOrganizationInput
		if err := json.Unmarshal(record.Input, &workflowInput); err != nil {
			return generationMismatch("销售脚本整理 Workflow 输入无法解析", err)
		}
		if workflowInput.Identity != *input.GenerationIdentity || workflowInput.AttemptGeneration != input.AttemptGeneration {
			return generationMismatch("销售脚本 Agent 与工作流生产身份不一致", nil)
		}
		_, err = r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, input.Phase, workflowInput)
		return err
	case CommercePhaseStoryboard:
		var workflowInput CommerceStoryboardPlanningInput
		if err := json.Unmarshal(record.Input, &workflowInput); err != nil {
			return generationMismatch("分镜 Workflow 输入无法解析", err)
		}
		if workflowInput.Identity != *input.GenerationIdentity || workflowInput.AttemptGeneration != input.AttemptGeneration {
			return generationMismatch("分镜 Agent 与工作流生产身份不一致", nil)
		}
		_, err = r.lockWorkflowRun(ctx, tx, input.WorkflowRunID, input.Phase, workflowInput)
		return err
	default:
		return commerce.Error{Code: CommerceCodeWorkflowInputInvalid, Message: "销售脚本 Agent 阶段无效"}
	}
}
