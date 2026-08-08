package workflows

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	CommerceScriptDerivationBatchWorkflowName = "CommerceScriptDerivationBatchWorkflow"
	CommerceScriptDerivationItemWorkflowName  = "CommerceScriptDerivationItemWorkflow"

	commerceScriptDerivationWorkflowType = "commerce_script_derivation"

	StartCommerceScriptDerivationBatchActivity    = "StartCommerceScriptDerivationBatch"
	LoadCommerceScriptDerivationItemActivity      = "LoadCommerceScriptDerivationItem"
	CallCommerceScriptDerivationAgentActivity     = "CallCommerceScriptDerivationAgent"
	CommitCommerceScriptDerivationItemActivity    = "CommitCommerceScriptDerivationItem"
	FailCommerceScriptDerivationItemActivity      = "FailCommerceScriptDerivationItem"
	FinalizeCommerceScriptDerivationBatchActivity = "FinalizeCommerceScriptDerivationBatch"
	CancelCommerceScriptDerivationBatchActivity   = "CancelCommerceScriptDerivationBatch"

	commerceScriptDerivationMaxReviewRounds = 3
	commerceScriptDerivationDefaultWindow   = 5
	commerceScriptDerivationMaxWindow       = 20
)

type CommerceScriptDerivationBatchInput struct {
	OrganizationID          string `json:"organizationId"`
	ProjectID               string `json:"projectId"`
	BatchID                 string `json:"batchId"`
	WorkflowRunID           string `json:"workflowRunId"`
	MaxConcurrency          int    `json:"maxConcurrency"`
	ProjectControlCommandID string `json:"projectControlCommandId,omitempty"`
}

type CommerceScriptDerivationItemInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	BatchID        string `json:"batchId"`
	ItemID         string `json:"itemId"`
	WorkflowRunID  string `json:"workflowRunId"`
}

type CommerceScriptDerivationBatchSnapshot struct {
	Batch   commerce.ScriptDerivationBatch `json:"batch"`
	ItemIDs []string                       `json:"itemIds"`
}

type CommerceScriptDerivationItemSnapshot struct {
	Batch          commerce.ScriptDerivationBatch   `json:"batch"`
	Item           commerce.ScriptDerivationItem    `json:"item"`
	Attempt        commerce.ScriptDerivationAttempt `json:"attempt"`
	ProductVersion commerce.ProductVersion          `json:"productVersion"`
}

type CommerceScriptCandidate struct {
	ContractVersion string `json:"contractVersion"`
	Title           string `json:"title"`
	Content         string `json:"content"`
}

type CommerceScriptReviewIssue struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type CommerceScriptReview struct {
	ContractVersion string                      `json:"contractVersion"`
	Decision        string                      `json:"decision"`
	Issues          []CommerceScriptReviewIssue `json:"issues"`
	Feedback        string                      `json:"feedback,omitempty"`
}

type CommerceScriptDerivationAgentInput struct {
	WorkflowInput CommerceScriptDerivationItemInput    `json:"workflowInput"`
	Snapshot      CommerceScriptDerivationItemSnapshot `json:"snapshot"`
	Phase         string                               `json:"phase"`
	Round         int                                  `json:"round"`
	Candidate     *CommerceScriptCandidate             `json:"candidate,omitempty"`
	Review        *CommerceScriptReview                `json:"review,omitempty"`
}

type CommerceScriptDerivationAgentOutput struct {
	RawOutput  string                     `json:"rawOutput"`
	Provenance CommerceAgentProvenance    `json:"provenance"`
	Call       ScriptDerivationCallResult `json:"call"`
}

type ScriptDerivationCallResult struct {
	AttemptCallID     string `json:"attemptCallId"`
	ProviderRequestID string `json:"providerRequestId,omitempty"`
	ProviderCallID    string `json:"providerCallId,omitempty"`
}

type CommerceScriptDerivationCommitInput struct {
	WorkflowInput CommerceScriptDerivationItemInput    `json:"workflowInput"`
	Snapshot      CommerceScriptDerivationItemSnapshot `json:"snapshot"`
	Candidate     CommerceScriptCandidate              `json:"candidate"`
	Review        CommerceScriptReview                 `json:"review"`
}

type CommerceScriptDerivationItemOutput struct {
	ItemID                string `json:"itemId"`
	Status                string `json:"status"`
	OutputScriptUnitID    string `json:"outputScriptUnitId,omitempty"`
	OutputScriptVersionID string `json:"outputScriptVersionId,omitempty"`
	ErrorCode             string `json:"errorCode,omitempty"`
	ErrorMessage          string `json:"errorMessage,omitempty"`
}

type CommerceScriptDerivationFailureInput struct {
	WorkflowInput CommerceScriptDerivationItemInput `json:"workflowInput"`
	Retryable     bool                              `json:"retryable"`
	ErrorCode     string                            `json:"errorCode"`
	ErrorMessage  string                            `json:"errorMessage"`
}

type CommerceScriptDerivationBatchOutput struct {
	BatchID              string `json:"batchId"`
	Status               string `json:"status"`
	RequestedCount       int    `json:"requestedCount"`
	SucceededCount       int    `json:"succeededCount"`
	FailedRetryableCount int    `json:"failedRetryableCount"`
	FailedTerminalCount  int    `json:"failedTerminalCount"`
	CancelledCount       int    `json:"cancelledCount"`
}

func EnqueueCommerceScriptDerivationBatchTx(
	ctx context.Context,
	tx pgx.Tx,
	input CommerceScriptDerivationBatchInput,
	production commerce.ProductionContext,
	createdBy string,
) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.BatchID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return errors.New("commerce script derivation workflow identity is incomplete")
	}
	input.MaxConcurrency = normalizeCommerceScriptDerivationWindow(input.MaxConcurrency)
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	inputHash, err := commerceContractHash(input)
	if err != nil {
		return err
	}
	temporalWorkflowID := fmt.Sprintf("commerce-script-derivation-%s", input.BatchID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', NULLIF($7, '')::uuid, $8, $9, $10)
	`, input.WorkflowRunID, input.OrganizationID, input.ProjectID, temporalWorkflowID,
		commerceScriptDerivationWorkflowType, raw, createdBy,
		production.Generation.ID, production.VideoBinding.ID, production.VideoBinding.Revision); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id, organization_id, project_id, production_generation_id,
			workflow_type, workflow_handler, temporal_workflow_id, task_queue,
			input, input_hash, max_attempts
		)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9, 12)
	`, input.WorkflowRunID, input.OrganizationID, input.ProjectID, production.Generation.ID,
		commerceScriptDerivationWorkflowType, temporalWorkflowID, ScriptTaskQueue, raw, inputHash)
	return err
}

func CommerceScriptDerivationBatchWorkflow(
	ctx workflow.Context,
	input CommerceScriptDerivationBatchInput,
) (output CommerceScriptDerivationBatchOutput, workflowErr error) {
	if err := validateCommerceScriptDerivationBatchInput(input); err != nil {
		return output, temporal.NewNonRetryableApplicationError(
			err.Error(), commerce.CodeScriptDerivationInvalid, err,
		)
	}
	input.MaxConcurrency = normalizeCommerceScriptDerivationWindow(input.MaxConcurrency)
	defer func() {
		if workflowErr == nil || !temporal.IsCanceledError(workflowErr) {
			return
		}
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		cancelCtx := workflow.WithActivityOptions(disconnected, defaultActivityOptions())
		_ = workflow.ExecuteActivity(
			cancelCtx, CancelCommerceScriptDerivationBatchActivity, input,
		).Get(cancelCtx, nil)
	}()

	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var snapshot CommerceScriptDerivationBatchSnapshot
	if workflowErr = workflow.ExecuteActivity(
		activityCtx, StartCommerceScriptDerivationBatchActivity, input,
	).Get(activityCtx, &snapshot); workflowErr != nil {
		return output, workflowErr
	}

	type childResult struct {
		itemID string
		output CommerceScriptDerivationItemOutput
		err    error
	}
	results := workflow.NewBufferedChannel(ctx, input.MaxConcurrency)
	active := 0
	next := 0
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for (!stopScheduling && next < len(snapshot.ItemIDs)) || active > 0 {
		for !stopScheduling && next < len(snapshot.ItemIDs) && active < input.MaxConcurrency {
			itemID := snapshot.ItemIDs[next]
			next++
			active++
			childInput := CommerceScriptDerivationItemInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
				BatchID: input.BatchID, ItemID: itemID, WorkflowRunID: input.WorkflowRunID,
			}
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:        fmt.Sprintf("commerce-script-derivation-item-%s", itemID),
				ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
			})
			future := workflow.ExecuteChildWorkflow(
				childCtx, CommerceScriptDerivationItemWorkflowName, childInput,
			)
			workflow.Go(ctx, func(ctx workflow.Context) {
				var itemOutput CommerceScriptDerivationItemOutput
				err := future.Get(ctx, &itemOutput)
				results.Send(ctx, childResult{itemID: itemID, output: itemOutput, err: err})
			})
		}
		var result childResult
		results.Receive(ctx, &result)
		active--
		if stopOnBalance && !stopScheduling {
			if code, message, ok := billingInsufficientBalanceFailure(result.err); ok {
				stopScheduling = true
				stopCode = code
				stopMessage = message
			}
		}
	}
	if stopScheduling {
		code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
		for ; next < len(snapshot.ItemIDs); next++ {
			itemInput := CommerceScriptDerivationItemInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
				BatchID: input.BatchID, ItemID: snapshot.ItemIDs[next],
				WorkflowRunID: input.WorkflowRunID,
			}
			if workflowErr = workflow.ExecuteActivity(
				activityCtx,
				FailCommerceScriptDerivationItemActivity,
				CommerceScriptDerivationFailureInput{
					WorkflowInput: itemInput,
					Retryable:     false,
					ErrorCode:     code,
					ErrorMessage:  message,
				},
			).Get(activityCtx, nil); workflowErr != nil {
				return CommerceScriptDerivationBatchOutput{}, workflowErr
			}
		}
	}

	if workflowErr = workflow.ExecuteActivity(
		activityCtx, FinalizeCommerceScriptDerivationBatchActivity, input,
	).Get(activityCtx, &output); workflowErr != nil {
		return CommerceScriptDerivationBatchOutput{}, workflowErr
	}
	return output, nil
}

func CommerceScriptDerivationItemWorkflow(
	ctx workflow.Context,
	input CommerceScriptDerivationItemInput,
) (output CommerceScriptDerivationItemOutput, workflowErr error) {
	if err := validateCommerceScriptDerivationItemInput(input); err != nil {
		return output, temporal.NewNonRetryableApplicationError(
			err.Error(), commerce.CodeScriptDerivationInvalid, err,
		)
	}
	defer func() {
		if workflowErr == nil {
			return
		}
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		failureCtx := workflow.WithActivityOptions(disconnected, defaultActivityOptions())
		if temporal.IsCanceledError(workflowErr) {
			_ = workflow.ExecuteActivity(
				failureCtx, FailCommerceScriptDerivationItemActivity,
				CommerceScriptDerivationFailureInput{
					WorkflowInput: input, Retryable: false,
					ErrorCode: "CANCELLED", ErrorMessage: "脚本裂变条目已取消",
				},
			).Get(failureCtx, nil)
			return
		}
		code, message := workflowErrorFields(workflowErr, commerce.CodeScriptDerivationInvalid)
		_ = workflow.ExecuteActivity(
			failureCtx, FailCommerceScriptDerivationItemActivity,
			CommerceScriptDerivationFailureInput{
				WorkflowInput: input, Retryable: scriptDerivationWorkflowErrorRetryable(workflowErr),
				ErrorCode: code, ErrorMessage: message,
			},
		).Get(failureCtx, nil)
	}()

	activityCtx := workflow.WithActivityOptions(ctx, commerceScriptDerivationActivityOptions())
	var snapshot CommerceScriptDerivationItemSnapshot
	if workflowErr = workflow.ExecuteActivity(
		activityCtx, LoadCommerceScriptDerivationItemActivity, input,
	).Get(activityCtx, &snapshot); workflowErr != nil {
		return output, workflowErr
	}
	if snapshot.Item.Status == "succeeded" && snapshot.Item.OutputScriptUnitID != nil &&
		snapshot.Item.OutputScriptVersionID != nil {
		return CommerceScriptDerivationItemOutput{
			ItemID: snapshot.Item.ID, Status: "succeeded",
			OutputScriptUnitID:    *snapshot.Item.OutputScriptUnitID,
			OutputScriptVersionID: *snapshot.Item.OutputScriptVersionID,
		}, nil
	}

	var candidate CommerceScriptCandidate
	var generated CommerceScriptDerivationAgentOutput
	if workflowErr = workflow.ExecuteActivity(
		activityCtx, CallCommerceScriptDerivationAgentActivity,
		CommerceScriptDerivationAgentInput{
			WorkflowInput: input, Snapshot: snapshot, Phase: "generate", Round: 1,
		},
	).Get(activityCtx, &generated); workflowErr != nil {
		return output, workflowErr
	}
	if err := decodeCommerceScriptCandidate(generated.RawOutput, &candidate); err != nil {
		workflowErr = temporal.NewNonRetryableApplicationError(
			err.Error(), commerce.CodeScriptDerivationInvalid, err,
		)
		return output, workflowErr
	}

	var review CommerceScriptReview
	for round := 1; round <= commerceScriptDerivationMaxReviewRounds; round++ {
		var reviewed CommerceScriptDerivationAgentOutput
		if workflowErr = workflow.ExecuteActivity(
			activityCtx, CallCommerceScriptDerivationAgentActivity,
			CommerceScriptDerivationAgentInput{
				WorkflowInput: input, Snapshot: snapshot, Phase: "review",
				Round: round, Candidate: &candidate,
			},
		).Get(activityCtx, &reviewed); workflowErr != nil {
			return output, workflowErr
		}
		if err := decodeCommerceScriptReview(reviewed.RawOutput, &review); err != nil {
			workflowErr = temporal.NewNonRetryableApplicationError(
				err.Error(), commerce.CodeScriptDerivationInvalid, err,
			)
			return output, workflowErr
		}
		if review.Decision == "approve" {
			break
		}
		if round == commerceScriptDerivationMaxReviewRounds {
			workflowErr = temporal.NewNonRetryableApplicationError(
				"脚本裂变结果在三轮修正后仍未通过商品事实审核",
				commerce.CodeScriptDerivationInvalid, nil,
			)
			return output, workflowErr
		}
		var revised CommerceScriptDerivationAgentOutput
		if workflowErr = workflow.ExecuteActivity(
			activityCtx, CallCommerceScriptDerivationAgentActivity,
			CommerceScriptDerivationAgentInput{
				WorkflowInput: input, Snapshot: snapshot, Phase: "revise",
				Round: round + 1, Candidate: &candidate, Review: &review,
			},
		).Get(activityCtx, &revised); workflowErr != nil {
			return output, workflowErr
		}
		if err := decodeCommerceScriptCandidate(revised.RawOutput, &candidate); err != nil {
			workflowErr = temporal.NewNonRetryableApplicationError(
				err.Error(), commerce.CodeScriptDerivationInvalid, err,
			)
			return output, workflowErr
		}
	}

	if workflowErr = workflow.ExecuteActivity(
		activityCtx, CommitCommerceScriptDerivationItemActivity,
		CommerceScriptDerivationCommitInput{
			WorkflowInput: input, Snapshot: snapshot, Candidate: candidate, Review: review,
		},
	).Get(activityCtx, &output); workflowErr != nil {
		return CommerceScriptDerivationItemOutput{}, workflowErr
	}
	return output, nil
}

func commerceScriptDerivationActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 10 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	if options.RetryPolicy != nil {
		options.RetryPolicy.MaximumAttempts = 3
	}
	return options
}

func (a CommerceActivities) StartCommerceScriptDerivationBatch(
	ctx context.Context,
	input CommerceScriptDerivationBatchInput,
) (CommerceScriptDerivationBatchSnapshot, error) {
	if err := validateCommerceScriptDerivationBatchInput(input); err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, temporal.NewNonRetryableApplicationError(
			err.Error(), commerce.CodeScriptDerivationInvalid, err,
		)
	}
	if a.Core.db == nil {
		return CommerceScriptDerivationBatchSnapshot{}, commerceActivityPortError()
	}
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	batch, err := repository.LoadScriptDerivationBatch(
		ctx, tx, input.OrganizationID, input.ProjectID, input.BatchID, true,
	)
	if err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, err
	}
	if batch.WorkflowRunID == nil || *batch.WorkflowRunID != input.WorkflowRunID {
		return CommerceScriptDerivationBatchSnapshot{}, temporal.NewNonRetryableApplicationError(
			"脚本裂变批次与工作流身份不一致", commerce.CodeScriptDerivationState, nil,
		)
	}
	batch, err = repository.StartScriptDerivationBatch(ctx, tx, batch)
	if err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, err
	}
	items, err := repository.ListScriptDerivationItems(ctx, tx, batch.ID)
	if err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, err
	}
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	if err := insertEvent(
		ctx, tx, batch.OrganizationID, batch.ProjectID,
		"commerce.script_derivation.batch.started", "commerce_script_derivation_batch",
		batch.ID, scriptDerivationBatchEventPayload(batch),
	); err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceScriptDerivationBatchSnapshot{}, err
	}
	return CommerceScriptDerivationBatchSnapshot{Batch: batch, ItemIDs: itemIDs}, nil
}

func (a CommerceActivities) LoadCommerceScriptDerivationItem(
	ctx context.Context,
	input CommerceScriptDerivationItemInput,
) (CommerceScriptDerivationItemSnapshot, error) {
	if err := validateCommerceScriptDerivationItemInput(input); err != nil {
		return CommerceScriptDerivationItemSnapshot{}, temporal.NewNonRetryableApplicationError(
			err.Error(), commerce.CodeScriptDerivationInvalid, err,
		)
	}
	if a.Core.db == nil {
		return CommerceScriptDerivationItemSnapshot{}, commerceActivityPortError()
	}
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return CommerceScriptDerivationItemSnapshot{}, err
	}
	defer tx.Rollback(ctx)
	batch, err := repository.LoadScriptDerivationBatch(
		ctx, tx, input.OrganizationID, input.ProjectID, input.BatchID, true,
	)
	if err != nil {
		return CommerceScriptDerivationItemSnapshot{}, err
	}
	if batch.WorkflowRunID == nil || *batch.WorkflowRunID != input.WorkflowRunID {
		return CommerceScriptDerivationItemSnapshot{}, temporal.NewNonRetryableApplicationError(
			"脚本裂变条目与工作流身份不一致", commerce.CodeScriptDerivationState, nil,
		)
	}
	item, err := repository.LoadScriptDerivationItem(
		ctx, tx, batch.OrganizationID, batch.ProjectID, input.ItemID, true,
	)
	if err != nil {
		return CommerceScriptDerivationItemSnapshot{}, err
	}
	if item.Status != "succeeded" {
		if _, err = repository.StartScriptDerivationItem(ctx, tx, item); err != nil {
			return CommerceScriptDerivationItemSnapshot{}, err
		}
		item, err = repository.LoadScriptDerivationItem(
			ctx, tx, batch.OrganizationID, batch.ProjectID, input.ItemID, true,
		)
		if err != nil {
			return CommerceScriptDerivationItemSnapshot{}, err
		}
		if err := insertEvent(
			ctx, tx, batch.OrganizationID, batch.ProjectID,
			"commerce.script_derivation.item.started", "commerce_script_derivation_item",
			item.ID, scriptDerivationItemEventPayload(batch, item),
		); err != nil {
			return CommerceScriptDerivationItemSnapshot{}, err
		}
		if _, err := reconcileScriptDerivationBatchProgress(
			ctx, tx, repository, batch,
		); err != nil {
			return CommerceScriptDerivationItemSnapshot{}, err
		}
	}
	attempt, err := repository.LoadScriptDerivationAttempt(ctx, tx, item.ID)
	if err != nil {
		return CommerceScriptDerivationItemSnapshot{}, err
	}
	productVersion, err := repository.LoadProductVersion(
		ctx, tx, batch.OrganizationID, batch.ProjectID, batch.ProductVersionID,
	)
	if err != nil {
		return CommerceScriptDerivationItemSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceScriptDerivationItemSnapshot{}, err
	}
	return CommerceScriptDerivationItemSnapshot{
		Batch: batch, Item: item, Attempt: attempt, ProductVersion: productVersion,
	}, nil
}

func (a CommerceActivities) CallCommerceScriptDerivationAgent(
	ctx context.Context,
	input CommerceScriptDerivationAgentInput,
) (CommerceScriptDerivationAgentOutput, error) {
	if err := validateCommerceScriptDerivationAgentInput(input); err != nil {
		return CommerceScriptDerivationAgentOutput{}, temporal.NewNonRetryableApplicationError(
			err.Error(), commerce.CodeScriptDerivationInvalid, err,
		)
	}
	if a.Core.db == nil || a.Core.gateway == nil {
		return CommerceScriptDerivationAgentOutput{}, temporal.NewNonRetryableApplicationError(
			"Provider Gateway 或工作流数据库未配置", provider.CodeProviderGatewayRequired, nil,
		)
	}
	binding := scriptDerivationPromptBinding(input.Snapshot.Batch.PromptContract, input.Phase)
	variables := scriptDerivationPromptVariables(input)
	rendered, err := a.Core.renderWorkflowPromptVersion(
		ctx, input.Snapshot.Batch.OrganizationID, input.Snapshot.Batch.ProjectID,
		binding.TemplateKey, binding.PromptVersionID, variables,
	)
	if err != nil {
		return CommerceScriptDerivationAgentOutput{}, err
	}
	rendered = withCommerceOutputContract(rendered)
	nodeExecution, err := StartNodeRun(
		ctx, a.Core.db, scriptDerivationNodeRunInput(input, rendered),
	)
	if err != nil {
		return CommerceScriptDerivationAgentOutput{}, err
	}
	if nodeExecution.ProductionGenerationID != input.Snapshot.Batch.ProductionGenerationID ||
		nodeExecution.VideoProductionBindingID != input.Snapshot.Batch.VideoProductionBindingID ||
		nodeExecution.VideoProductionBindingRevision != input.Snapshot.Batch.VideoProductionBindingRevision {
		_ = FailNodeRun(
			ctx, a.Core.db, nodeExecution, commerce.CodeScriptDerivationState,
			"脚本裂变节点身份与冻结生产配置不一致",
		)
		return CommerceScriptDerivationAgentOutput{}, temporal.NewNonRetryableApplicationError(
			"脚本裂变节点身份与冻结生产配置不一致", commerce.CodeScriptDerivationState, nil,
		)
	}

	callID := uuid.NewString()
	call, err := a.startCommerceScriptDerivationAttemptCall(
		ctx, input, callID, rendered,
	)
	if err != nil {
		_ = FailNodeRun(ctx, a.Core.db, nodeExecution, commerce.CodeScriptDerivationState, err.Error())
		return CommerceScriptDerivationAgentOutput{}, err
	}
	idempotencyKey := fmt.Sprintf(
		"commerce-script-derivation:%s:%s:%d:%s",
		input.Snapshot.Batch.ID, input.Snapshot.Item.ID, input.Round, input.Phase,
	)
	response, err := a.Core.generateProviderText(ctx, nodeExecution, provider.GatewayTextRequest{
		OrganizationID:    input.Snapshot.Batch.OrganizationID,
		ProjectID:         input.Snapshot.Batch.ProjectID,
		WorkflowRunID:     input.WorkflowInput.WorkflowRunID,
		NodeRunID:         nodeExecution.NodeRunID,
		ModelProfileKey:   input.Snapshot.Batch.ScriptModelProfileKey,
		ProviderModelID:   pointerText(input.Snapshot.Batch.ProviderModelID),
		PromptTemplateKey: rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
		PromptHash: rendered.RenderedHash, PromptSource: rendered.Source,
		IdempotencyKey: idempotencyKey,
		Input: mustJSON(map[string]any{
			"prompt": rendered.RenderedText, "responseFormat": "json", "maxOutputTokens": 16000,
		}),
		Options: provider.GatewayTextOptions{
			TimeoutMS: providerTextGatewayTimeoutMS, IdempotencyKey: idempotencyKey,
		},
	})
	if err != nil {
		workflowErr := workflowErrorFromProvider(err, codeActivityFailed)
		code, message := workflowErrorFields(workflowErr, codeActivityFailed)
		_ = a.failCommerceScriptDerivationAttemptCall(ctx, call.ID, code, message)
		_ = FailNodeRun(ctx, a.Core.db, nodeExecution, code, message)
		return CommerceScriptDerivationAgentOutput{}, temporal.NewApplicationError(message, code, err)
	}
	outputHash := scriptDerivationTextHash(response.Output.Text)
	if err := a.completeCommerceScriptDerivationAttemptCall(
		ctx, call.ID, response.ProviderRequestID, response.ProviderCallID,
		response.ModelID, outputHash,
	); err != nil {
		_ = FailNodeRun(ctx, a.Core.db, nodeExecution, commerce.CodeScriptDerivationState, err.Error())
		return CommerceScriptDerivationAgentOutput{}, err
	}
	if input.Phase == "review" {
		var review CommerceScriptReview
		if decodeCommerceScriptReview(response.Output.Text, &review) == nil {
			if err := a.recordCommerceScriptDerivationReview(
				ctx, input.Snapshot.Item.ID, input.Snapshot.Attempt.ID,
				input.Round, review,
			); err != nil {
				_ = FailNodeRun(
					ctx, a.Core.db, nodeExecution,
					commerce.CodeScriptDerivationState, err.Error(),
				)
				return CommerceScriptDerivationAgentOutput{}, err
			}
		}
	}
	output := CommerceScriptDerivationAgentOutput{
		RawOutput: response.Output.Text,
		Provenance: CommerceAgentProvenance{
			Role: input.Phase, Round: input.Round, NodeRunID: nodeExecution.NodeRunID,
			ProviderRequestID: response.ProviderRequestID, ProviderCallID: response.ProviderCallID,
			ProviderModelID: response.ModelID, PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash,
		},
		Call: ScriptDerivationCallResult{
			AttemptCallID: call.ID, ProviderRequestID: response.ProviderRequestID,
			ProviderCallID: response.ProviderCallID,
		},
	}
	if err := CompleteNodeRun(ctx, a.Core.db, nodeExecution, mustJSON(output)); err != nil {
		return CommerceScriptDerivationAgentOutput{}, err
	}
	return output, nil
}

func (a CommerceActivities) CommitCommerceScriptDerivationItem(
	ctx context.Context,
	input CommerceScriptDerivationCommitInput,
) (CommerceScriptDerivationItemOutput, error) {
	if a.Core.db == nil {
		return CommerceScriptDerivationItemOutput{}, commerceActivityPortError()
	}
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	defer tx.Rollback(ctx)
	batch, err := repository.LoadScriptDerivationBatch(
		ctx, tx, input.WorkflowInput.OrganizationID, input.WorkflowInput.ProjectID,
		input.WorkflowInput.BatchID, true,
	)
	if err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	item, err := repository.LoadScriptDerivationItem(
		ctx, tx, batch.OrganizationID, batch.ProjectID, input.WorkflowInput.ItemID, true,
	)
	if err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	unit, version, err := repository.MaterializeScriptDerivationItem(
		ctx, tx, batch, item, input.Candidate.Title, input.Candidate.Content,
	)
	if err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	item, err = repository.LoadScriptDerivationItem(
		ctx, tx, batch.OrganizationID, batch.ProjectID, item.ID, true,
	)
	if err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	if err := insertEvent(
		ctx, tx, batch.OrganizationID, batch.ProjectID,
		"commerce.script_derivation.item.succeeded", "commerce_script_derivation_item",
		item.ID, scriptDerivationItemEventPayload(batch, item),
	); err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	if _, err := reconcileScriptDerivationBatchProgress(
		ctx, tx, repository, batch,
	); err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommerceScriptDerivationItemOutput{}, err
	}
	return CommerceScriptDerivationItemOutput{
		ItemID: item.ID, Status: "succeeded",
		OutputScriptUnitID: unit.ID, OutputScriptVersionID: version.ID,
	}, nil
}

func (a CommerceActivities) FailCommerceScriptDerivationItem(
	ctx context.Context,
	input CommerceScriptDerivationFailureInput,
) error {
	if a.Core.db == nil {
		return commerceActivityPortError()
	}
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch, err := repository.LoadScriptDerivationBatch(
		ctx, tx, input.WorkflowInput.OrganizationID, input.WorkflowInput.ProjectID,
		input.WorkflowInput.BatchID, true,
	)
	if err != nil {
		return err
	}
	item, err := repository.LoadScriptDerivationItem(
		ctx, tx, batch.OrganizationID, batch.ProjectID, input.WorkflowInput.ItemID, true,
	)
	if err != nil {
		return err
	}
	if item.Status == "succeeded" || item.Status == "failed_retryable" ||
		item.Status == "failed_terminal" || item.Status == "cancelled" {
		return tx.Commit(ctx)
	}
	if input.ErrorCode == "CANCELLED" {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_script_derivation_items
			SET status = 'cancelled', completed_at = now(), revision = revision + 1, updated_at = now()
			WHERE id = $1 AND status IN ('queued', 'running', 'reviewing')
		`, item.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_script_derivation_attempts
			SET status = 'cancelled', completed_at = now(), updated_at = now()
			WHERE id = NULLIF($1, '')::uuid
			  AND status IN ('queued', 'generating', 'reviewing', 'revising')
		`, pointerText(item.CurrentAttemptID)); err != nil {
			return err
		}
		item.Status = "cancelled"
		if err := insertEvent(
			ctx, tx, batch.OrganizationID, batch.ProjectID,
			"commerce.script_derivation.item.cancelled", "commerce_script_derivation_item",
			item.ID, scriptDerivationItemEventPayload(batch, item),
		); err != nil {
			return err
		}
		if _, err := reconcileScriptDerivationBatchProgress(
			ctx, tx, repository, batch,
		); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err := repository.FailScriptDerivationItem(
		ctx, tx, item, input.Retryable, input.ErrorCode, input.ErrorMessage,
	); err != nil {
		return err
	}
	item, err = repository.LoadScriptDerivationItem(
		ctx, tx, batch.OrganizationID, batch.ProjectID, item.ID, true,
	)
	if err != nil {
		return err
	}
	if err := insertEvent(
		ctx, tx, batch.OrganizationID, batch.ProjectID,
		"commerce.script_derivation.item.failed", "commerce_script_derivation_item",
		item.ID, scriptDerivationItemEventPayload(batch, item),
	); err != nil {
		return err
	}
	if _, err := reconcileScriptDerivationBatchProgress(
		ctx, tx, repository, batch,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func reconcileScriptDerivationBatchProgress(
	ctx context.Context,
	tx pgx.Tx,
	repository *commerce.Repository,
	batch commerce.ScriptDerivationBatch,
) (commerce.ScriptDerivationBatch, error) {
	reconciled, err := repository.ReconcileScriptDerivationBatch(ctx, tx, batch)
	if err != nil {
		return commerce.ScriptDerivationBatch{}, err
	}
	if err := insertEvent(
		ctx, tx, reconciled.OrganizationID, reconciled.ProjectID,
		"commerce.script_derivation.batch.progressed",
		"commerce_script_derivation_batch", reconciled.ID,
		scriptDerivationBatchEventPayload(reconciled),
	); err != nil {
		return commerce.ScriptDerivationBatch{}, err
	}
	return reconciled, nil
}

func (a CommerceActivities) FinalizeCommerceScriptDerivationBatch(
	ctx context.Context,
	input CommerceScriptDerivationBatchInput,
) (CommerceScriptDerivationBatchOutput, error) {
	if a.Core.db == nil {
		return CommerceScriptDerivationBatchOutput{}, commerceActivityPortError()
	}
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return CommerceScriptDerivationBatchOutput{}, err
	}
	defer tx.Rollback(ctx)
	batch, err := repository.LoadScriptDerivationBatch(
		ctx, tx, input.OrganizationID, input.ProjectID, input.BatchID, true,
	)
	if err != nil {
		return CommerceScriptDerivationBatchOutput{}, err
	}
	batch, err = repository.ReconcileScriptDerivationBatch(ctx, tx, batch)
	if err != nil {
		return CommerceScriptDerivationBatchOutput{}, err
	}
	eventName := "commerce.script_derivation.batch.progressed"
	switch batch.Status {
	case "succeeded":
		eventName = "commerce.script_derivation.batch.succeeded"
	case "partial_succeeded":
		eventName = "commerce.script_derivation.batch.partial_succeeded"
	case "failed":
		eventName = "commerce.script_derivation.batch.failed"
	case "cancelled":
		eventName = "commerce.script_derivation.batch.cancelled"
	}
	if err := insertEvent(
		ctx, tx, batch.OrganizationID, batch.ProjectID, eventName,
		"commerce_script_derivation_batch", batch.ID, scriptDerivationBatchEventPayload(batch),
	); err != nil {
		return CommerceScriptDerivationBatchOutput{}, err
	}
	output := scriptDerivationBatchOutput(batch)
	if err := tx.Commit(ctx); err != nil {
		return CommerceScriptDerivationBatchOutput{}, err
	}
	workflowStatus := "succeeded"
	errorCode := ""
	errorMessage := ""
	if batch.Status == "failed" {
		workflowStatus = "failed"
		errorCode = commerce.CodeScriptDerivationInvalid
		errorMessage = "脚本裂变批次全部失败"
	}
	if err := TransitionWorkflowRun(
		ctx, a.Core.db, input.WorkflowRunID, workflowStatus,
		errorCode, errorMessage, mustJSON(output),
	); err != nil {
		return CommerceScriptDerivationBatchOutput{}, err
	}
	return output, nil
}

func (a CommerceActivities) CancelCommerceScriptDerivationBatch(
	ctx context.Context,
	input CommerceScriptDerivationBatchInput,
) error {
	if a.Core.db == nil {
		return commerceActivityPortError()
	}
	service := commerce.NewScriptDerivationService(commerce.NewRepository())
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch, err := service.CancelBatch(
		ctx, tx, input.OrganizationID, input.ProjectID, input.BatchID,
	)
	if err != nil {
		return err
	}
	if err := insertEvent(
		ctx, tx, batch.OrganizationID, batch.ProjectID,
		"commerce.script_derivation.batch.cancelled", "commerce_script_derivation_batch",
		batch.ID, scriptDerivationBatchEventPayload(batch),
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return CancelWorkflowRun(
		ctx, a.Core.db, input.WorkflowRunID,
		mustJSON(scriptDerivationBatchOutput(batch)), "脚本裂变任务已取消",
	)
}

func (a CommerceActivities) startCommerceScriptDerivationAttemptCall(
	ctx context.Context,
	input CommerceScriptDerivationAgentInput,
	callID string,
	rendered promptsvc.RenderedPrompt,
) (commerce.ScriptDerivationAttemptCall, error) {
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return commerce.ScriptDerivationAttemptCall{}, err
	}
	defer tx.Rollback(ctx)
	if input.Phase == "review" {
		if err := repository.SetScriptDerivationReviewState(
			ctx, tx, input.Snapshot.Item.ID, input.Snapshot.Attempt.ID,
			input.Round, "reviewing", json.RawMessage(`{}`), "",
		); err != nil {
			return commerce.ScriptDerivationAttemptCall{}, err
		}
		item := input.Snapshot.Item
		item.Status = "reviewing"
		if err := insertEvent(
			ctx, tx, input.Snapshot.Batch.OrganizationID, input.Snapshot.Batch.ProjectID,
			"commerce.script_derivation.item.reviewing", "commerce_script_derivation_item",
			item.ID, scriptDerivationItemEventPayload(input.Snapshot.Batch, item),
		); err != nil {
			return commerce.ScriptDerivationAttemptCall{}, err
		}
	} else if input.Phase == "revise" {
		reviewRaw := mustJSON(input.Review)
		if err := repository.SetScriptDerivationReviewState(
			ctx, tx, input.Snapshot.Item.ID, input.Snapshot.Attempt.ID,
			input.Round, "revising", reviewRaw, input.Review.Feedback,
		); err != nil {
			return commerce.ScriptDerivationAttemptCall{}, err
		}
	}
	call, err := repository.InsertScriptDerivationAttemptCall(
		ctx, tx, commerce.ScriptDerivationAttemptCall{
			ID: callID, BatchID: input.Snapshot.Batch.ID, ItemID: input.Snapshot.Item.ID,
			AttemptID:      input.Snapshot.Attempt.ID,
			OrganizationID: input.Snapshot.Batch.OrganizationID,
			ProjectID:      input.Snapshot.Batch.ProjectID, ProductID: input.Snapshot.Batch.ProductID,
			RoundNo: input.Round, Phase: input.Phase,
			ModelProfileKey:       input.Snapshot.Batch.ScriptModelProfileKey,
			ModelProfileBindingID: input.Snapshot.Batch.ModelProfileBindingID,
			ProviderModelID:       input.Snapshot.Batch.ProviderModelID,
			PromptTemplateKey:     rendered.TemplateKey, PromptVersionID: rendered.PromptVersionID,
			PromptHash: rendered.RenderedHash, StartedAt: time.Now().UTC(),
		},
	)
	if err != nil {
		return commerce.ScriptDerivationAttemptCall{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return commerce.ScriptDerivationAttemptCall{}, err
	}
	return call, nil
}

func (a CommerceActivities) completeCommerceScriptDerivationAttemptCall(
	ctx context.Context,
	callID string,
	providerRequestID string,
	providerCallID string,
	providerModelID string,
	outputHash string,
) error {
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := repository.CompleteScriptDerivationAttemptCall(
		ctx, tx, callID, providerRequestID, providerCallID, providerModelID, outputHash,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a CommerceActivities) failCommerceScriptDerivationAttemptCall(
	ctx context.Context,
	callID string,
	errorCode string,
	errorMessage string,
) error {
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := repository.FailScriptDerivationAttemptCall(
		ctx, tx, callID, errorCode, errorMessage,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a CommerceActivities) recordCommerceScriptDerivationReview(
	ctx context.Context,
	itemID string,
	attemptID string,
	round int,
	review CommerceScriptReview,
) error {
	repository := commerce.NewRepository()
	tx, err := a.Core.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := repository.SetScriptDerivationReviewState(
		ctx, tx, itemID, attemptID, round, "reviewing", mustJSON(review), review.Feedback,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateCommerceScriptDerivationBatchInput(input CommerceScriptDerivationBatchInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.BatchID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return errors.New("脚本裂变批次身份不完整")
	}
	return nil
}

func validateCommerceScriptDerivationItemInput(input CommerceScriptDerivationItemInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" ||
		strings.TrimSpace(input.BatchID) == "" || strings.TrimSpace(input.ItemID) == "" ||
		strings.TrimSpace(input.WorkflowRunID) == "" {
		return errors.New("脚本裂变条目身份不完整")
	}
	return nil
}

func validateCommerceScriptDerivationAgentInput(input CommerceScriptDerivationAgentInput) error {
	if err := validateCommerceScriptDerivationItemInput(input.WorkflowInput); err != nil {
		return err
	}
	if input.Round < 1 || input.Round > commerceScriptDerivationMaxReviewRounds {
		return errors.New("脚本裂变审核轮次无效")
	}
	switch input.Phase {
	case "generate":
		if input.Round != 1 {
			return errors.New("脚本裂变首次生成轮次无效")
		}
	case "review":
		if input.Candidate == nil {
			return errors.New("脚本裂变审核缺少候选脚本")
		}
	case "revise":
		if input.Candidate == nil || input.Review == nil {
			return errors.New("脚本裂变修正缺少候选脚本或审核反馈")
		}
	default:
		return errors.New("脚本裂变 Agent 阶段无效")
	}
	return nil
}

func normalizeCommerceScriptDerivationWindow(value int) int {
	if value <= 0 {
		return commerceScriptDerivationDefaultWindow
	}
	if value > commerceScriptDerivationMaxWindow {
		return commerceScriptDerivationMaxWindow
	}
	return value
}

func scriptDerivationPromptBinding(
	contract commerce.ScriptDerivationPromptContract,
	phase string,
) commerce.ScriptDerivationPromptBinding {
	switch phase {
	case "review":
		return contract.Reviewer
	case "revise":
		return contract.Reviser
	default:
		return contract.Generator
	}
}

func scriptDerivationPromptVariables(input CommerceScriptDerivationAgentInput) map[string]any {
	contextValue := map[string]any{
		"batchId":     input.Snapshot.Batch.ID,
		"itemId":      input.Snapshot.Item.ID,
		"dimension":   input.Snapshot.Batch.Dimension,
		"instruction": input.Snapshot.Batch.Instruction,
		"preserve":    input.Snapshot.Batch.Preserve,
		"variation": map[string]any{
			"key":   input.Snapshot.Item.VariationKey,
			"label": input.Snapshot.Item.VariationLabel,
			"brief": input.Snapshot.Item.VariationBrief,
		},
		"sourceScript": map[string]any{
			"content":     input.Snapshot.Batch.SourceContentSnapshot,
			"contentHash": input.Snapshot.Batch.SourceContentHash,
		},
		"product":   input.Snapshot.ProductVersion,
		"candidate": input.Candidate,
		"review":    input.Review,
		"round":     input.Round,
	}
	raw := mustJSON(contextValue)
	return map[string]any{"input": map[string]any{"context": string(raw)}}
}

func decodeCommerceScriptCandidate(raw string, output *CommerceScriptCandidate) error {
	if err := decodeStrictCommerceJSON(raw, output); err != nil {
		return fmt.Errorf("脚本裂变模型输出不是有效的单脚本契约: %w", err)
	}
	output.ContractVersion = strings.TrimSpace(output.ContractVersion)
	output.Title = strings.TrimSpace(output.Title)
	output.Content = strings.TrimSpace(output.Content)
	if output.ContractVersion != "commerce-script-derivation/v1" {
		return errors.New("脚本裂变输出契约版本无效")
	}
	if output.Title == "" || output.Content == "" {
		return errors.New("脚本裂变输出标题或正文为空")
	}
	return nil
}

func decodeCommerceScriptReview(raw string, output *CommerceScriptReview) error {
	if err := decodeStrictCommerceJSON(raw, output); err != nil {
		return fmt.Errorf("脚本裂变审核输出不是有效契约: %w", err)
	}
	output.ContractVersion = strings.TrimSpace(output.ContractVersion)
	output.Decision = strings.TrimSpace(output.Decision)
	output.Feedback = strings.TrimSpace(output.Feedback)
	if output.ContractVersion != "commerce-script-derivation-review/v1" {
		return errors.New("脚本裂变审核契约版本无效")
	}
	switch output.Decision {
	case "approve":
	case "revise", "reject":
		if output.Feedback == "" && len(output.Issues) == 0 {
			return errors.New("脚本裂变审核拒绝时必须提供反馈")
		}
	default:
		return errors.New("脚本裂变审核决定无效")
	}
	return nil
}

func decodeStrictCommerceJSON(raw string, output any) error {
	raw = strings.TrimSpace(stripJSONFence(raw))
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 后存在额外内容")
		}
		return err
	}
	return nil
}

func scriptDerivationWorkflowErrorRetryable(err error) bool {
	var applicationErr *temporal.ApplicationError
	if errors.As(err, &applicationErr) {
		return !applicationErr.NonRetryable()
	}
	return !temporal.IsCanceledError(err)
}

func scriptDerivationTextHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func scriptDerivationNodeRunInput(
	input CommerceScriptDerivationAgentInput,
	rendered promptsvc.RenderedPrompt,
) NodeRunInput {
	return NodeRunInput{
		OrganizationID: input.Snapshot.Batch.OrganizationID,
		ProjectID:      input.Snapshot.Batch.ProjectID,
		WorkflowRunID:  input.WorkflowInput.WorkflowRunID,
		NodeKey: fmt.Sprintf(
			"commerce-script-derivation-%s-%02d-%s",
			input.Snapshot.Item.ID, input.Round, input.Phase,
		),
		NodeType: "agent.commerce.script_derivation." + input.Phase,
		Input: mustJSON(map[string]any{
			"batchId": input.Snapshot.Batch.ID, "itemId": input.Snapshot.Item.ID,
			"round": input.Round, "phase": input.Phase,
			"promptVersionId": rendered.PromptVersionID, "promptHash": rendered.RenderedHash,
		}),
	}
}

func scriptDerivationBatchOutput(batch commerce.ScriptDerivationBatch) CommerceScriptDerivationBatchOutput {
	return CommerceScriptDerivationBatchOutput{
		BatchID: batch.ID, Status: batch.Status, RequestedCount: batch.RequestedCount,
		SucceededCount:       batch.SucceededCount,
		FailedRetryableCount: batch.FailedRetryableCount,
		FailedTerminalCount:  batch.FailedTerminalCount,
		CancelledCount:       batch.CancelledCount,
	}
}

func scriptDerivationBatchEventPayload(batch commerce.ScriptDerivationBatch) json.RawMessage {
	return mustJSON(map[string]any{
		"batchId": batch.ID, "sourceScriptUnitId": batch.SourceScriptUnitID,
		"workflowRunId": pointerText(batch.WorkflowRunID), "status": batch.Status,
		"requestedCount": batch.RequestedCount, "queuedCount": batch.QueuedCount,
		"runningCount": batch.RunningCount, "succeededCount": batch.SucceededCount,
		"failedRetryableCount": batch.FailedRetryableCount,
		"failedTerminalCount":  batch.FailedTerminalCount,
		"cancelledCount":       batch.CancelledCount,
		"rootBatchId":          pointerText(batch.RootBatchID),
		"retryOfBatchId":       pointerText(batch.RetryOfBatchID),
	})
}

func scriptDerivationItemEventPayload(
	batch commerce.ScriptDerivationBatch,
	item commerce.ScriptDerivationItem,
) json.RawMessage {
	return mustJSON(map[string]any{
		"batchId": batch.ID, "itemId": item.ID,
		"variationKey": item.VariationKey, "inputOrdinal": item.InputOrdinal,
		"workflowRunId": pointerText(batch.WorkflowRunID), "status": item.Status,
		"outputScriptUnitId": pointerText(item.OutputScriptUnitID),
		"errorCode":          pointerText(item.ErrorCode), "errorMessage": pointerText(item.ErrorMessage),
	})
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
