package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"go.temporal.io/sdk/workflow"
)

const (
	CommerceScriptOrganizationWorkflowName = "CommerceScriptOrganizationWorkflow"

	ClaimCommerceSalesScriptContractActivityName  = "ClaimCommerceSalesScriptContract"
	CommitCommerceSalesScriptContractActivityName = "CommitCommerceSalesScriptContract"
)

type CommerceScriptOrganizationInput struct {
	Identity          commerce.UnitGenerationIdentity `json:"identity"`
	WorkflowRunID     string                          `json:"workflowRunId"`
	CreatedBy         string                          `json:"createdBy"`
	AttemptGeneration int                             `json:"attemptGeneration"`
}

type CommerceSalesScriptOwner struct {
	OrganizationInput *CommerceScriptOrganizationInput `json:"organizationInput,omitempty"`
	StoryboardInput   *CommerceStoryboardPlanningInput `json:"storyboardInput,omitempty"`
}

type CommerceSalesScriptContractState struct {
	ContractID         string                      `json:"contractId"`
	Status             string                      `json:"status"`
	AttemptGeneration  int                         `json:"attemptGeneration"`
	OwnerWorkflowRunID string                      `json:"ownerWorkflowRunId"`
	Owner              bool                        `json:"owner"`
	InputHash          string                      `json:"inputHash"`
	Contract           CommerceSalesScriptContract `json:"contract"`
	ContractHash       string                      `json:"contractHash,omitempty"`
}

type CommerceSalesScriptContractClaimInput struct {
	Owner CommerceSalesScriptOwner `json:"owner"`
}

type CommerceSalesScriptContractClaimResult struct {
	Snapshot CommerceStoryboardPlanningSnapshot `json:"snapshot"`
	State    CommerceSalesScriptContractState   `json:"state"`
}

type CommerceSalesScriptContractCommitInput struct {
	Owner      CommerceSalesScriptOwner           `json:"owner"`
	Snapshot   CommerceStoryboardPlanningSnapshot `json:"snapshot"`
	Contract   CommerceSalesScriptContract        `json:"contract"`
	Provenance CommerceAgentProvenance            `json:"provenance"`
}

type CommerceScriptOrganizationOutput struct {
	Identity     commerce.UnitGenerationIdentity `json:"identity"`
	ContractID   string                          `json:"contractId"`
	Contract     CommerceSalesScriptContract     `json:"contract"`
	ContractHash string                          `json:"contractHash"`
	Status       string                          `json:"status"`
	AgentCalls   []CommerceAgentProvenance       `json:"agentCalls,omitempty"`
}

func CommerceScriptOrganizationWorkflow(ctx workflow.Context, input CommerceScriptOrganizationInput) (output CommerceScriptOrganizationOutput, resultErr error) {
	if err := validateCommerceScriptOrganizationInput(input); err != nil {
		return output, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	defer finalizeCommerceGenerationWorkflowFailure(ctx, CommerceGenerationWorkflowFailureInput{OrganizationInput: &input}, &resultErr)
	owner := CommerceSalesScriptOwner{OrganizationInput: &input}
	state, _, calls, err := ensureCommerceSalesScriptContract(ctx, owner)
	if err != nil {
		return output, err
	}
	acceptedCalls := calls
	if len(acceptedCalls) > 1 {
		acceptedCalls = acceptedCalls[len(acceptedCalls)-1:]
	}
	output = commerceScriptOrganizationOutput(input.Identity, state, acceptedCalls)
	if output.Status != "ready" {
		return CommerceScriptOrganizationOutput{}, commerceWorkflowError(CommerceCodeGenerationMismatch, errors.New("销售脚本契约未进入就绪状态"))
	}
	return output, nil
}

func ensureCommerceSalesScriptContract(
	ctx workflow.Context,
	owner CommerceSalesScriptOwner,
) (CommerceSalesScriptContractState, CommerceStoryboardPlanningSnapshot, []CommerceAgentProvenance, error) {
	defaultCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	agentCtx := workflow.WithActivityOptions(ctx, commerceAgentActivityOptions())
	for {
		var claimed CommerceSalesScriptContractClaimResult
		if err := workflow.ExecuteActivity(
			defaultCtx,
			ClaimCommerceSalesScriptContractActivityName,
			CommerceSalesScriptContractClaimInput{Owner: owner},
		).Get(defaultCtx, &claimed); err != nil {
			return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, nil, err
		}
		if err := validateCommerceSalesScriptClaim(owner, claimed); err != nil {
			return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, nil,
				commerceWorkflowError(CommerceCodeGenerationMismatch, err)
		}
		if claimed.State.Status == "ready" {
			return claimed.State, claimed.Snapshot, nil, nil
		}
		if !claimed.State.Owner {
			if err := workflow.Sleep(ctx, 2*time.Second); err != nil {
				return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, nil, err
			}
			continue
		}

		contract, calls, err := organizeCommerceScriptInWorkflow(agentCtx, owner, claimed.Snapshot)
		if err != nil {
			return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, calls, err
		}
		if len(calls) == 0 {
			return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, nil,
				commerceWorkflowError(CommerceCodeStoryboardContractInvalid, errors.New("销售脚本契约缺少 Agent provenance"))
		}
		var committed CommerceSalesScriptContractState
		if err := workflow.ExecuteActivity(
			defaultCtx,
			CommitCommerceSalesScriptContractActivityName,
			CommerceSalesScriptContractCommitInput{
				Owner: owner, Snapshot: claimed.Snapshot, Contract: contract,
				Provenance: calls[len(calls)-1],
			},
		).Get(defaultCtx, &committed); err != nil {
			return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, calls, err
		}
		if committed.ContractID != claimed.State.ContractID || committed.Status != "ready" {
			return CommerceSalesScriptContractState{}, CommerceStoryboardPlanningSnapshot{}, calls,
				commerceWorkflowError(CommerceCodeGenerationMismatch, errors.New("销售脚本契约提交身份不一致"))
		}
		return committed, claimed.Snapshot, calls, nil
	}
}

func organizeCommerceScriptInWorkflow(
	ctx workflow.Context,
	owner CommerceSalesScriptOwner,
	snapshot CommerceStoryboardPlanningSnapshot,
) (CommerceSalesScriptContract, []CommerceAgentProvenance, error) {
	limit := commerceReviewRounds(snapshot.Bindings.ScriptOrganizer)
	feedback := []CommerceReviewIssue{}
	calls := make([]CommerceAgentProvenance, 0, limit)
	var lastErr error
	for round := 1; round <= limit; round++ {
		callInput, err := commerceSalesScriptAgentCall(owner, snapshot, round, feedback)
		if err != nil {
			return CommerceSalesScriptContract{}, calls, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
		}
		var call CommerceAgentCallOutput
		if err := workflow.ExecuteActivity(ctx, OrganizeCommerceScriptActivityName, callInput).Get(ctx, &call); err != nil {
			return CommerceSalesScriptContract{}, calls, err
		}
		calls = append(calls, call.Provenance)
		contract, err := ParseCommerceSalesScript(call.RawOutput)
		if err == nil {
			contract = normalizeCommerceSalesScript(contract, snapshot)
			err = ValidateCommerceSalesScript(contract, snapshot)
		}
		if err == nil {
			return contract, calls, nil
		}
		lastErr = err
		feedback = commerceValidationFeedback(CommerceCodeStoryboardContractInvalid, "salesScript", err)
	}
	return CommerceSalesScriptContract{}, calls, commerceWorkflowError(CommerceCodeStoryboardContractInvalid, lastErr)
}

func commerceSalesScriptAgentCall(
	owner CommerceSalesScriptOwner,
	snapshot CommerceStoryboardPlanningSnapshot,
	round int,
	feedback []CommerceReviewIssue,
) (CommerceAgentCallInput, error) {
	phase, identity, workflowRunID, attemptGeneration, err := commerceSalesScriptOwnerIdentity(owner)
	if err != nil {
		return CommerceAgentCallInput{}, err
	}
	return CommerceAgentCallInput{
		GenerationIdentity: &identity, WorkflowRunID: workflowRunID, AttemptGeneration: attemptGeneration,
		Phase: phase, Round: round, Binding: snapshot.Bindings.ScriptOrganizer,
		InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.TargetLocale,
		Context: mustJSON(map[string]any{"snapshot": snapshot, "reviewerIssues": feedback}), ReviewerIssues: feedback,
	}, nil
}

func commerceSalesScriptOwnerIdentity(owner CommerceSalesScriptOwner) (CommerceWorkflowPhase, commerce.UnitGenerationIdentity, string, int, error) {
	if owner.OrganizationInput != nil && owner.StoryboardInput == nil {
		return CommercePhaseScriptOrganization, owner.OrganizationInput.Identity,
			owner.OrganizationInput.WorkflowRunID, owner.OrganizationInput.AttemptGeneration, nil
	}
	if owner.StoryboardInput != nil && owner.OrganizationInput == nil {
		return CommercePhaseStoryboard, owner.StoryboardInput.Identity,
			owner.StoryboardInput.WorkflowRunID, owner.StoryboardInput.AttemptGeneration, nil
	}
	return "", commerce.UnitGenerationIdentity{}, "", 0, errors.New("销售脚本契约必须且只能有一个工作流所有者")
}

func validateCommerceScriptOrganizationInput(input CommerceScriptOrganizationInput) error {
	if err := ValidateCommerceUnitGenerationIdentity(input.Identity); err != nil {
		return err
	}
	if strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.CreatedBy) == "" || input.AttemptGeneration <= 0 {
		return errors.New("workflowRunId, createdBy, and attemptGeneration are required")
	}
	return nil
}

func validateCommerceSalesScriptClaim(owner CommerceSalesScriptOwner, claimed CommerceSalesScriptContractClaimResult) error {
	_, identity, workflowRunID, _, err := commerceSalesScriptOwnerIdentity(owner)
	if err != nil {
		return err
	}
	if err := ValidateCommerceStoryboardSnapshot(identity, claimed.Snapshot); err != nil {
		return err
	}
	if claimed.State.ContractID == "" || claimed.State.InputHash != claimed.Snapshot.InputHash || claimed.State.AttemptGeneration <= 0 {
		return errors.New("销售脚本契约持久化身份不完整")
	}
	if claimed.State.Status != "running" && claimed.State.Status != "ready" {
		return fmt.Errorf("销售脚本契约状态无效: %s", claimed.State.Status)
	}
	if claimed.State.Owner && claimed.State.OwnerWorkflowRunID != workflowRunID {
		return errors.New("销售脚本契约所有权与当前工作流不一致")
	}
	if claimed.State.Status == "ready" {
		if err := ValidateCommerceSalesScript(claimed.State.Contract, claimed.Snapshot); err != nil {
			return err
		}
		hash, err := commerceContractHash(claimed.State.Contract)
		if err != nil {
			return err
		}
		if hash != claimed.State.ContractHash {
			return errors.New("销售脚本契约 hash 不一致")
		}
	}
	return nil
}

func commerceScriptOrganizationOutput(
	identity commerce.UnitGenerationIdentity,
	state CommerceSalesScriptContractState,
	calls []CommerceAgentProvenance,
) CommerceScriptOrganizationOutput {
	return CommerceScriptOrganizationOutput{
		Identity: identity, ContractID: state.ContractID, Contract: state.Contract,
		ContractHash: state.ContractHash, Status: state.Status,
		AgentCalls: append([]CommerceAgentProvenance(nil), calls...),
	}
}

func (a CommerceActivities) ClaimCommerceSalesScriptContract(
	ctx context.Context,
	input CommerceSalesScriptContractClaimInput,
) (CommerceSalesScriptContractClaimResult, error) {
	if a.Ports == nil {
		return CommerceSalesScriptContractClaimResult{}, commerceActivityPortError()
	}
	result, err := a.Ports.ClaimSalesScriptContract(ctx, input)
	if err != nil {
		return CommerceSalesScriptContractClaimResult{}, commercePortError(err)
	}
	return result, nil
}

func (a CommerceActivities) CommitCommerceSalesScriptContract(
	ctx context.Context,
	input CommerceSalesScriptContractCommitInput,
) (CommerceSalesScriptContractState, error) {
	if a.Ports == nil {
		return CommerceSalesScriptContractState{}, commerceActivityPortError()
	}
	result, err := a.Ports.CommitSalesScriptContract(ctx, input)
	if err != nil {
		return CommerceSalesScriptContractState{}, commercePortError(err)
	}
	return result, nil
}
