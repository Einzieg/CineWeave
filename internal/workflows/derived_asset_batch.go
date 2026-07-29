package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	DefaultDerivedAssetImageConcurrency = 5
	MaxDerivedAssetImageConcurrency     = 16
	derivedAssetExecutionLeaseDuration  = 45 * time.Minute
)

type DerivedAssetBatchOptions struct {
	BatchID         string   `json:"batchId,omitempty"`
	ScriptEpisodeID string   `json:"scriptEpisodeId,omitempty"`
	RequirementIDs  []string `json:"requirementIds,omitempty"`
	ShotIDs         []string `json:"shotIds,omitempty"`
	MaxConcurrency  int      `json:"maxConcurrency,omitempty"`
	Force           bool     `json:"force,omitempty"`
	AgentTaskID     string   `json:"agentTaskId,omitempty"`
	AgentStepID     string   `json:"agentStepId,omitempty"`
}

// DerivedAssetBatchItem remains the compact display shape used by callers that
// have not loaded the durable request-item projection yet.
type DerivedAssetBatchItem struct {
	RequirementID      string `json:"requirementId"`
	StoryboardShotID   string `json:"storyboardShotId"`
	ShotNo             int    `json:"shotNo"`
	AssetID            string `json:"assetId"`
	AssetType          string `json:"assetType"`
	AssetName          string `json:"assetName"`
	RequirementType    string `json:"requirementType"`
	BaseReferenceReady bool   `json:"baseReferenceReady"`
}

type DerivedAssetRequirementSnapshot struct {
	ID                     string `json:"id"`
	ProjectID              string `json:"projectId"`
	ProductionGenerationID string `json:"productionGenerationId"`
	StoryboardShotID       string `json:"storyboardShotId"`
	CanonicalAssetID       string `json:"canonicalAssetId"`
	ReviewStatus           string `json:"reviewStatus"`
	Status                 string `json:"status"`
	Prompt                 string `json:"prompt,omitempty"`
	UpdatedAt              string `json:"updatedAt"`
}

type DerivedAssetStoryboardShotSnapshot struct {
	ID                     string `json:"id"`
	ProjectID              string `json:"projectId"`
	ProductionGenerationID string `json:"productionGenerationId"`
	ShotNo                 int    `json:"shotNo"`
	DeletedAt              string `json:"deletedAt,omitempty"`
	UpdatedAt              string `json:"updatedAt"`
}

type DerivedAssetCanonicalAssetSnapshot struct {
	ID                           string `json:"id"`
	ProjectID                    string `json:"projectId"`
	Status                       string `json:"status"`
	Revision                     int64  `json:"revision"`
	PromptRevision               int64  `json:"promptRevision"`
	UpdatedAt                    string `json:"updatedAt"`
	PrimaryReferenceArtifactID   string `json:"primaryReferenceArtifactId,omitempty"`
	PrimaryReferenceMediaFileID  string `json:"primaryReferenceMediaFileId,omitempty"`
	PrimaryReferenceStorageKey   string `json:"primaryReferenceStorageKey,omitempty"`
	FallbackReferenceArtifactID  string `json:"referenceArtifactId,omitempty"`
	FallbackReferenceMediaFileID string `json:"referenceMediaFileId,omitempty"`
	FallbackReferenceStorageKey  string `json:"referenceStorageKey,omitempty"`
}

type DerivedAssetPromptSnapshot struct {
	TemplateKey string `json:"templateKey"`
	VersionID   string `json:"versionId,omitempty"`
	Hash        string `json:"hash"`
	Source      string `json:"source"`
	Text        string `json:"text"`
}

type DerivedAssetReferenceSnapshot struct {
	Items []provider.GatewayImageReference `json:"items"`
}

type DerivedAssetModelSnapshot struct {
	ProviderAccountID string `json:"providerAccountId"`
	ProviderModelID   string `json:"providerModelId"`
	ModelProfileKey   string `json:"modelProfileKey"`
	ModelKey          string `json:"modelKey"`
	Modality          string `json:"modality"`
	Status            string `json:"status"`
	UpdatedAt         string `json:"updatedAt"`
}

type DerivedAssetCapabilitySnapshot struct {
	TaskTypes             json.RawMessage `json:"taskTypes"`
	InputLimits           json.RawMessage `json:"inputLimits"`
	OutputLimits          json.RawMessage `json:"outputLimits"`
	QualityTiers          json.RawMessage `json:"qualityTiers"`
	ProviderOptionsSchema json.RawMessage `json:"providerOptionsSchema"`
	PricingPolicy         json.RawMessage `json:"pricingPolicy"`
}

func HashDerivedAssetSnapshot(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type DerivedAssetBatchWorkItem struct {
	ExecutionItemID  string `json:"executionItemId"`
	RequestItemID    string `json:"requestItemId"`
	BatchID          string `json:"batchId"`
	InputOrdinal     int    `json:"inputOrdinal"`
	RequirementID    string `json:"requirementId"`
	StoryboardShotID string `json:"storyboardShotId"`
	CanonicalAssetID string `json:"canonicalAssetId"`
	NodeRunID        string `json:"nodeRunId"`
	NodeKey          string `json:"nodeKey"`
	AttemptNo        int    `json:"attemptNo"`
	Status           string `json:"status"`
}

type DerivedAssetExecutionLease struct {
	DerivedAssetBatchWorkItem
	OrganizationID                 string        `json:"organizationId"`
	ProjectID                      string        `json:"projectId"`
	WorkflowRunID                  string        `json:"workflowRunId"`
	ProductionGenerationID         string        `json:"productionGenerationId"`
	VideoProductionBindingID       string        `json:"videoProductionBindingId"`
	VideoProductionBindingRevision int64         `json:"videoProductionBindingRevision"`
	LeaseOwner                     string        `json:"leaseOwner"`
	LeaseToken                     string        `json:"leaseToken"`
	Execution                      NodeExecution `json:"execution"`
	Terminal                       bool          `json:"terminal,omitempty"`
	TerminalStatus                 string        `json:"terminalStatus,omitempty"`
}

type DerivedAssetProviderExecutionInput struct {
	Lease DerivedAssetExecutionLease `json:"lease"`
}

type DerivedAssetProviderExecutionOutput struct {
	Lease    DerivedAssetExecutionLease    `json:"lease"`
	Response provider.GatewayImageResponse `json:"response"`
}

type DerivedAssetMediaVerification struct {
	Lease          DerivedAssetExecutionLease `json:"lease"`
	ProviderCallID string                     `json:"providerCallId"`
	ModelID        string                     `json:"modelId"`
	ArtifactID     string                     `json:"artifactId"`
	MediaFileID    string                     `json:"mediaFileId"`
	StorageKey     string                     `json:"storageKey"`
}

type DerivedAssetExecutionFailure struct {
	Lease        DerivedAssetExecutionLease `json:"lease"`
	ErrorCode    string                     `json:"errorCode"`
	ErrorMessage string                     `json:"errorMessage"`
	Retryable    bool                       `json:"retryable"`
	Cancelled    bool                       `json:"cancelled,omitempty"`
	Discarded    bool                       `json:"discarded,omitempty"`
}

type DerivedAssetBatchItemOutput struct {
	RequestItemID   string `json:"requestItemId"`
	ExecutionItemID string `json:"executionItemId,omitempty"`
	InputOrdinal    int    `json:"inputOrdinal"`
	OriginalID      string `json:"originalId"`
	RequirementID   string `json:"requirementId,omitempty"`
	Disposition     string `json:"disposition"`
	Status          string `json:"status"`
	Retryable       bool   `json:"retryable"`
	ErrorCode       string `json:"errorCode,omitempty"`
	ErrorMessage    string `json:"errorMessage,omitempty"`
	ProviderCallID  string `json:"providerCallId,omitempty"`
	ModelID         string `json:"modelId,omitempty"`
	ArtifactID      string `json:"artifactId,omitempty"`
	MediaFileID     string `json:"mediaFileId,omitempty"`
	StorageKey      string `json:"storageKey,omitempty"`
}

type DerivedAssetBatchOutput struct {
	BatchID                 string                        `json:"batchId"`
	Status                  string                        `json:"status"`
	WorkflowRunID           string                        `json:"workflowRunId"`
	TotalItems              int                           `json:"totalItems"`
	ExecutableItems         int                           `json:"executableItems"`
	ReviewRequiredItems     int                           `json:"reviewRequiredItems"`
	NotFoundItems           int                           `json:"notFoundItems"`
	GenerationMismatchItems int                           `json:"generationMismatchItems"`
	AlreadyRunningItems     int                           `json:"alreadyRunningItems"`
	DuplicateItems          int                           `json:"duplicateItems"`
	SkippedItems            int                           `json:"skippedItems"`
	PendingItems            int                           `json:"pendingItems"`
	QueuedItems             int                           `json:"queuedItems"`
	RunningItems            int                           `json:"runningItems"`
	SucceededItems          int                           `json:"succeededItems"`
	FailedRetryableItems    int                           `json:"failedRetryableItems"`
	FailedTerminalItems     int                           `json:"failedTerminalItems"`
	CancelledItems          int                           `json:"cancelledItems"`
	DiscardedItems          int                           `json:"discardedItems"`
	CompletedItems          int                           `json:"completedItems"`
	FailedItems             int                           `json:"failedItems"`
	Items                   []DerivedAssetBatchItemOutput `json:"items"`
}

func BatchGenerateDerivedAssetImagesWorkflow(
	ctx workflow.Context,
	input TextToStoryboardInput,
) (output DerivedAssetBatchOutput, resultErr error) {
	options := resolveDerivedAssetBatchOptions(input.Input)
	if options.BatchID == "" {
		return output, temporal.NewNonRetryableApplicationError("batchId is required", "INVALID_DERIVED_ASSET_BATCH", nil)
	}
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	output.BatchID = options.BatchID
	output.WorkflowRunID = input.WorkflowRunID

	var items []DerivedAssetBatchWorkItem
	if err := workflow.ExecuteActivity(ctx, "LoadDerivedAssetExecutionItems", input, options.BatchID).Get(ctx, &items); err != nil {
		return output, err
	}
	limit := clampConcurrency(options.MaxConcurrency, DefaultDerivedAssetImageConcurrency, MaxDerivedAssetImageConcurrency)
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for start := 0; start < len(items); start += limit {
		end := start + limit
		if end > len(items) {
			end = len(items)
		}
		resultCh := workflow.NewBufferedChannel(ctx, end-start)
		for index := start; index < end; index++ {
			item := items[index]
			workflow.Go(ctx, func(itemCtx workflow.Context) {
				err := executeDerivedAssetWorkItem(itemCtx, input, item)
				resultCh.Send(itemCtx, err)
			})
		}
		for index := start; index < end; index++ {
			var itemErr error
			resultCh.Receive(ctx, &itemErr)
			if temporal.IsCanceledError(itemErr) {
				return output, itemErr
			}
			if stopOnBalance && !stopScheduling {
				if code, message, ok := billingInsufficientBalanceFailure(itemErr); ok {
					stopScheduling = true
					stopCode = code
					stopMessage = message
				}
			}
		}
		if stopScheduling {
			code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
			for index := end; index < len(items); index++ {
				if err := failUnstartedDerivedAssetWorkItem(ctx, input, items[index], code, message); err != nil {
					return output, err
				}
			}
			break
		}
	}
	if err := workflow.ExecuteActivity(ctx, "CompleteDerivedAssetBatchWorkflowV2", input, options.BatchID).Get(ctx, &output); err != nil {
		return output, err
	}
	return output, nil
}

func failUnstartedDerivedAssetWorkItem(
	ctx workflow.Context,
	input TextToStoryboardInput,
	item DerivedAssetBatchWorkItem,
	code string,
	message string,
) error {
	owner := workflow.GetInfo(ctx).WorkflowExecution.ID + ":balance-stop:" + item.ExecutionItemID
	var lease DerivedAssetExecutionLease
	if err := workflow.ExecuteActivity(
		ctx,
		"ClaimDerivedAssetExecution",
		input,
		item,
		owner,
	).Get(ctx, &lease); err != nil {
		return err
	}
	if lease.Terminal {
		return nil
	}
	var ignored bool
	return workflow.ExecuteActivity(
		ctx,
		"FailDerivedAssetExecution",
		DerivedAssetExecutionFailure{
			Lease: lease, ErrorCode: code, ErrorMessage: message, Retryable: false,
		},
	).Get(ctx, &ignored)
}

func executeDerivedAssetWorkItem(ctx workflow.Context, input TextToStoryboardInput, item DerivedAssetBatchWorkItem) error {
	owner := workflow.GetInfo(ctx).WorkflowExecution.ID + ":" + item.ExecutionItemID
	var lease DerivedAssetExecutionLease
	if err := workflow.ExecuteActivity(ctx, "ClaimDerivedAssetExecution", input, item, owner).Get(ctx, &lease); err != nil {
		return err
	}
	if lease.Terminal {
		return nil
	}
	providerCtx := workflow.WithActivityOptions(ctx, providerImageActivityOptions())
	var generated DerivedAssetProviderExecutionOutput
	if err := workflow.ExecuteActivity(providerCtx, "RunDerivedAssetProvider", DerivedAssetProviderExecutionInput{Lease: lease}).Get(providerCtx, &generated); err != nil {
		return failDerivedAssetWorkItem(ctx, lease, err)
	}
	var verified DerivedAssetMediaVerification
	if err := workflow.ExecuteActivity(ctx, "VerifyDerivedAssetMedia", generated).Get(ctx, &verified); err != nil {
		return failDerivedAssetWorkItem(ctx, lease, err)
	}
	if err := workflow.ExecuteActivity(ctx, "CommitDerivedAssetExecution", verified).Get(ctx, nil); err != nil {
		return failDerivedAssetWorkItem(ctx, lease, err)
	}
	return nil
}

func failDerivedAssetWorkItem(ctx workflow.Context, lease DerivedAssetExecutionLease, cause error) error {
	code, message := workflowExecutionError(cause)
	failure := DerivedAssetExecutionFailure{
		Lease: lease, ErrorCode: code, ErrorMessage: message,
		Retryable: derivedAssetFailureRetryable(code),
		Cancelled: temporal.IsCanceledError(cause),
		Discarded: code == CodeWorkflowResultDiscarded || code == "PRODUCTION_GENERATION_MISMATCH",
	}
	failureCtx := ctx
	if failure.Cancelled {
		failureCtx, _ = workflow.NewDisconnectedContext(ctx)
		failureCtx = workflow.WithActivityOptions(failureCtx, defaultActivityOptions())
	}
	var ignored bool
	if err := workflow.ExecuteActivity(failureCtx, "FailDerivedAssetExecution", failure).Get(failureCtx, &ignored); err != nil && !isWorkflowWriteFenced(err) {
		workflow.GetLogger(ctx).Error("failed to persist derived asset item failure", "executionItemId", lease.ExecutionItemID, "error", err)
	}
	return cause
}

func resolveDerivedAssetBatchOptions(raw json.RawMessage) DerivedAssetBatchOptions {
	options := DerivedAssetBatchOptions{MaxConcurrency: DefaultDerivedAssetImageConcurrency}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &options)
	}
	options.BatchID = strings.TrimSpace(options.BatchID)
	options.ScriptEpisodeID = strings.TrimSpace(options.ScriptEpisodeID)
	for index := range options.RequirementIDs {
		options.RequirementIDs[index] = strings.TrimSpace(options.RequirementIDs[index])
	}
	for index := range options.ShotIDs {
		options.ShotIDs[index] = strings.TrimSpace(options.ShotIDs[index])
	}
	options.MaxConcurrency = clampConcurrency(options.MaxConcurrency, DefaultDerivedAssetImageConcurrency, MaxDerivedAssetImageConcurrency)
	return options
}

func (a Activities) LoadDerivedAssetExecutionItems(
	ctx context.Context,
	input TextToStoryboardInput,
	batchID string,
) ([]DerivedAssetBatchWorkItem, error) {
	rows, err := a.db.Query(ctx, `
		SELECT execution.id::text, execution.request_item_id::text, execution.batch_id::text,
		       request.input_ordinal, execution.requirement_id::text,
		       execution.storyboard_shot_id::text, execution.canonical_asset_id::text,
		       execution.node_run_id::text, execution.node_key, execution.attempt_no, execution.status
		FROM derived_asset_batches batch
		JOIN derived_asset_request_items request ON request.batch_id = batch.id
		JOIN derived_asset_execution_items execution ON execution.request_item_id = request.id
		WHERE batch.id = $1
		  AND batch.organization_id = $2
		  AND batch.project_id = $3
		  AND batch.workflow_run_id = $4
		  AND execution.attempt_no = request.current_attempt_no
		ORDER BY request.input_ordinal
	`, batchID, input.OrganizationID, input.ProjectID, input.WorkflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DerivedAssetBatchWorkItem, 0)
	for rows.Next() {
		var item DerivedAssetBatchWorkItem
		if err := rows.Scan(
			&item.ExecutionItemID, &item.RequestItemID, &item.BatchID, &item.InputOrdinal,
			&item.RequirementID, &item.StoryboardShotID, &item.CanonicalAssetID,
			&item.NodeRunID, &item.NodeKey, &item.AttemptNo, &item.Status,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a Activities) ClaimDerivedAssetExecution(
	ctx context.Context,
	input TextToStoryboardInput,
	item DerivedAssetBatchWorkItem,
	leaseOwner string,
) (DerivedAssetExecutionLease, error) {
	if strings.TrimSpace(item.ExecutionItemID) == "" || strings.TrimSpace(leaseOwner) == "" {
		return DerivedAssetExecutionLease{}, temporal.NewNonRetryableApplicationError("executionItemId and leaseOwner are required", "INVALID_DERIVED_ASSET_EXECUTION", nil)
	}
	lease, reusable, err := a.loadReusableDerivedAssetLease(ctx, input, item, leaseOwner)
	if err != nil || reusable {
		return lease, err
	}
	execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		NodeKey:        item.NodeKey,
		NodeType:       "shot_asset_requirement.derived_image.generate",
		Input:          mustJSON(item),
	})
	if err != nil {
		if isWorkflowWriteFenced(err) {
			loaded, loadErr := a.loadDerivedAssetExecution(ctx, item.ExecutionItemID)
			if loadErr == nil {
				_, _ = a.FailDerivedAssetExecution(context.WithoutCancel(ctx), DerivedAssetExecutionFailure{
					Lease: loaded, ErrorCode: "PRODUCTION_GENERATION_MISMATCH",
					ErrorMessage: "项目生产代已切换，旧镜头衍生资产执行已废弃", Discarded: true,
				})
			}
		}
		return DerivedAssetExecutionLease{}, err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return DerivedAssetExecutionLease{}, err
	}
	defer tx.Rollback(ctx)
	runCtx, err := lockWorkflowBusinessWrite(ctx, tx, input.WorkflowRunID)
	if err != nil {
		return DerivedAssetExecutionLease{}, err
	}
	if runCtx.OrganizationID != input.OrganizationID || runCtx.ProjectID != input.ProjectID {
		return DerivedAssetExecutionLease{}, ErrWorkflowWriteFenced
	}
	lease, err = a.lockDerivedAssetExecutionTx(ctx, tx, item.ExecutionItemID)
	if err != nil {
		return DerivedAssetExecutionLease{}, err
	}
	if lease.WorkflowRunID != input.WorkflowRunID || lease.BatchID != item.BatchID ||
		lease.NodeRunID != execution.NodeRunID ||
		lease.ProductionGenerationID != execution.ProductionGenerationID ||
		lease.VideoProductionBindingID != execution.VideoProductionBindingID ||
		lease.VideoProductionBindingRevision != execution.VideoProductionBindingRevision {
		if err := a.markDerivedAssetExecutionDiscardedTx(ctx, tx, lease, "PRODUCTION_GENERATION_MISMATCH", "镜头衍生资产执行身份与当前工作流不一致"); err != nil {
			return DerivedAssetExecutionLease{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return DerivedAssetExecutionLease{}, err
		}
		return DerivedAssetExecutionLease{}, temporal.NewNonRetryableApplicationError("镜头衍生资产执行身份与当前工作流不一致", "PRODUCTION_GENERATION_MISMATCH", ErrWorkflowWriteFenced)
	}
	if isDerivedAssetExecutionTerminal(lease.Status) {
		lease.Terminal = true
		lease.TerminalStatus = lease.Status
		if err := tx.Commit(ctx); err != nil {
			return DerivedAssetExecutionLease{}, err
		}
		return lease, nil
	}
	if err := a.validateDerivedAssetFrozenTargetTx(ctx, tx, lease); err != nil {
		code, message := workflowErrorFields(err, "DERIVED_ASSET_SNAPSHOT_CHANGED")
		if discardErr := a.markDerivedAssetExecutionDiscardedTx(ctx, tx, lease, code, message); discardErr != nil {
			return DerivedAssetExecutionLease{}, discardErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return DerivedAssetExecutionLease{}, commitErr
		}
		return DerivedAssetExecutionLease{}, temporal.NewNonRetryableApplicationError(message, code, ErrWorkflowWriteFenced)
	}
	var leaseToken string
	err = tx.QueryRow(ctx, `
		UPDATE derived_asset_execution_items
		SET status = CASE
		      WHEN provider_result_snapshot IS NOT NULL THEN 'transferring'
		      ELSE 'leased'
		    END,
		    revision = revision + 1,
		    lease_owner = $2, lease_token = gen_random_uuid(),
		    lease_expires_at = now() + $3::interval, heartbeat_at = now(),
		    queued_at = COALESCE(queued_at, now()), started_at = COALESCE(started_at, now()),
		    error_code = NULL, error_message = NULL
		WHERE id = $1
		  AND status IN ('prepared', 'queued', 'leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
		  AND (
		    status IN ('prepared', 'queued')
		    OR lease_expires_at IS NULL
		    OR lease_expires_at <= now()
		    OR lease_owner = $2
		  )
		RETURNING lease_token::text
	`, item.ExecutionItemID, leaseOwner, derivedAssetExecutionLeaseDuration.String()).Scan(&leaseToken)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DerivedAssetExecutionLease{}, temporal.NewApplicationError("镜头衍生资产执行已被其他 Worker 认领", "DERIVED_ASSET_ALREADY_RUNNING")
		}
		return DerivedAssetExecutionLease{}, err
	}
	lease.LeaseOwner = leaseOwner
	lease.LeaseToken = leaseToken
	lease.Execution = execution
	if _, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET status = 'image_running',
		    metadata = (COALESCE(metadata, '{}'::jsonb)
		      - 'lastErrorCode' - 'lastErrorMessage' - 'lastFailedAt')
		      || jsonb_build_object(
		        'derivedAssetBatchId', $2::text,
		        'derivedAssetRequestItemId', $3::text,
		        'derivedAssetExecutionItemId', $4::text,
		        'derivedImageWorkflowRunId', $5::text,
		        'derivedImageQueuedAt', now()
		      ),
		    updated_at = now()
		WHERE id = $1
		  AND project_id = $6
		  AND production_generation_id = $7
		  AND storyboard_shot_id = $8
		  AND asset_id = $9
		  AND review_status = 'approved'
		  AND status <> 'skipped'
	`, lease.RequirementID, lease.BatchID, lease.RequestItemID, lease.ExecutionItemID,
		lease.WorkflowRunID, lease.ProjectID, lease.ProductionGenerationID,
		lease.StoryboardShotID, lease.CanonicalAssetID); err != nil {
		return DerivedAssetExecutionLease{}, err
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.started", "derived_asset_execution_item", lease.ExecutionItemID, mustJSON(map[string]any{
		"batchId": lease.BatchID, "requestItemId": lease.RequestItemID,
		"workflowRunId": lease.WorkflowRunID, "requirementId": lease.RequirementID,
		"attemptNo": lease.AttemptNo,
	})); err != nil {
		return DerivedAssetExecutionLease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DerivedAssetExecutionLease{}, err
	}
	return lease, nil
}

func (a Activities) loadReusableDerivedAssetLease(
	ctx context.Context,
	input TextToStoryboardInput,
	item DerivedAssetBatchWorkItem,
	leaseOwner string,
) (DerivedAssetExecutionLease, bool, error) {
	lease, err := a.loadDerivedAssetExecution(ctx, item.ExecutionItemID)
	if err != nil {
		return DerivedAssetExecutionLease{}, false, err
	}
	if lease.OrganizationID != input.OrganizationID || lease.ProjectID != input.ProjectID ||
		lease.WorkflowRunID != input.WorkflowRunID || lease.BatchID != item.BatchID {
		return DerivedAssetExecutionLease{}, false, ErrWorkflowWriteFenced
	}
	if isDerivedAssetExecutionTerminal(lease.Status) {
		lease.Terminal = true
		lease.TerminalStatus = lease.Status
		return lease, true, nil
	}
	var currentOwner string
	var expiresAt *time.Time
	var nodeToken string
	var nodeAttempt int
	err = a.db.QueryRow(ctx, `
		SELECT COALESCE(execution.lease_owner, ''), execution.lease_expires_at,
		       COALESCE(execution.lease_token::text, ''),
		       node.execution_token::text, node.attempt_generation
		FROM derived_asset_execution_items execution
		JOIN workflow_node_runs node ON node.id = execution.node_run_id
		WHERE execution.id = $1
	`, item.ExecutionItemID).Scan(&currentOwner, &expiresAt, &lease.LeaseToken, &nodeToken, &nodeAttempt)
	if err == nil && expiresAt != nil && expiresAt.After(time.Now()) {
		if currentOwner != leaseOwner {
			return DerivedAssetExecutionLease{}, false, temporal.NewApplicationError(
				"镜头衍生资产执行已被其他 Worker 认领",
				"DERIVED_ASSET_ALREADY_RUNNING",
			)
		}
		lease.LeaseOwner = leaseOwner
		lease.Execution = NodeExecution{
			NodeRunID: lease.NodeRunID, ExecutionToken: nodeToken, AttemptGeneration: nodeAttempt,
			ProductionGenerationID:         lease.ProductionGenerationID,
			VideoProductionBindingID:       lease.VideoProductionBindingID,
			VideoProductionBindingRevision: lease.VideoProductionBindingRevision,
		}
		return lease, true, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return DerivedAssetExecutionLease{}, false, err
	}
	return lease, false, nil
}

func (a Activities) RunDerivedAssetProvider(
	ctx context.Context,
	input DerivedAssetProviderExecutionInput,
) (DerivedAssetProviderExecutionOutput, error) {
	lease := input.Lease
	if lease.Terminal || !lease.Execution.valid() {
		return DerivedAssetProviderExecutionOutput{}, ErrWorkflowWriteFenced
	}
	if a.gateway == nil {
		return DerivedAssetProviderExecutionOutput{}, temporal.NewApplicationError("Provider Gateway 未配置", "PROVIDER_GATEWAY_UNAVAILABLE")
	}
	request, err := a.prepareDerivedAssetProviderRequest(ctx, lease)
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	response, err := a.generateProviderImage(ctx, lease.Execution, request)
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, workflowErrorFromProvider(err, codeActivityFailed)
	}
	if strings.TrimSpace(response.ProviderCallID) == "" || strings.TrimSpace(response.Output.ArtifactID) == "" ||
		strings.TrimSpace(response.Output.MediaFileID) == "" || strings.TrimSpace(response.Output.StorageKey) == "" {
		return DerivedAssetProviderExecutionOutput{}, workflowError{Code: "PROVIDER_IMAGE_MEDIA_MISSING", Message: "供应商返回成功，但媒体入库结果不完整"}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	defer tx.Rollback(ctx)
	current, err := a.lockDerivedAssetExecutionTx(ctx, tx, lease.ExecutionItemID)
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	if !derivedAssetLeaseMatches(current, lease) {
		if err := a.recordDerivedAssetLateResultTx(ctx, tx, current, response, "lease_changed_after_provider_completion"); err != nil {
			return DerivedAssetProviderExecutionOutput{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return DerivedAssetProviderExecutionOutput{}, err
		}
		return DerivedAssetProviderExecutionOutput{}, ErrWorkflowWriteFenced
	}
	if _, err := lockWorkflowBusinessWrite(ctx, tx, lease.WorkflowRunID); err != nil {
		if lateErr := a.recordDerivedAssetLateResultTx(ctx, tx, current, response, "production_generation_changed"); lateErr != nil {
			return DerivedAssetProviderExecutionOutput{}, lateErr
		}
		if discardErr := a.markDerivedAssetExecutionDiscardedTx(ctx, tx, current, "PRODUCTION_GENERATION_MISMATCH", "供应商返回结果时项目生产代已切换"); discardErr != nil {
			return DerivedAssetProviderExecutionOutput{}, discardErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return DerivedAssetProviderExecutionOutput{}, commitErr
		}
		return DerivedAssetProviderExecutionOutput{}, ErrWorkflowWriteFenced
	}
	providerResult := mustJSON(response)
	providerResultHash := HashDerivedAssetSnapshot(response)
	var selectedCredentialID *string
	_ = tx.QueryRow(ctx, `SELECT credential_id::text FROM provider_call_logs WHERE id = $1`, response.ProviderCallID).Scan(&selectedCredentialID)
	command, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'transferring', revision = revision + 1,
		    provider_request_id = NULLIF($3, '')::uuid,
		    provider_call_id = NULLIF($4, '')::uuid,
		    selected_credential_id = NULLIF($5, '')::uuid,
		    provider_result_snapshot = $6, provider_result_hash = $7,
		    artifact_id = NULLIF($8, '')::uuid,
		    media_file_id = NULLIF($9, '')::uuid,
		    storage_key = NULLIF($10, ''),
		    heartbeat_at = now(), lease_expires_at = now() + $11::interval
		WHERE id = $1 AND lease_token = $2 AND status IN ('leased', 'provider_running', 'unknown_outcome')
	`, lease.ExecutionItemID, lease.LeaseToken, response.ProviderRequestID, response.ProviderCallID,
		derivedNullableString(selectedCredentialID), providerResult, providerResultHash,
		response.Output.ArtifactID, response.Output.MediaFileID, response.Output.StorageKey,
		derivedAssetExecutionLeaseDuration.String())
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return DerivedAssetProviderExecutionOutput{}, ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.provider_succeeded", "derived_asset_execution_item", lease.ExecutionItemID, mustJSON(map[string]any{
		"batchId": lease.BatchID, "requestItemId": lease.RequestItemID,
		"workflowRunId":  lease.WorkflowRunID,
		"providerCallId": response.ProviderCallID, "modelId": response.ModelID,
	})); err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	return DerivedAssetProviderExecutionOutput{Lease: lease, Response: response}, nil
}

func (a Activities) prepareDerivedAssetProviderRequest(ctx context.Context, lease DerivedAssetExecutionLease) (provider.GatewayImageRequest, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return provider.GatewayImageRequest{}, err
	}
	defer tx.Rollback(ctx)
	current, err := a.lockDerivedAssetExecutionTx(ctx, tx, lease.ExecutionItemID)
	if err != nil {
		return provider.GatewayImageRequest{}, err
	}
	if !derivedAssetLeaseMatches(current, lease) {
		return provider.GatewayImageRequest{}, ErrWorkflowWriteFenced
	}
	if _, err := lockWorkflowBusinessWrite(ctx, tx, lease.WorkflowRunID); err != nil {
		return provider.GatewayImageRequest{}, err
	}
	if err := a.validateDerivedAssetFrozenTargetTx(ctx, tx, current); err != nil {
		return provider.GatewayImageRequest{}, err
	}
	var requestRaw json.RawMessage
	var modelRaw, capabilityRaw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT request_snapshot, model_snapshot, capability_snapshot
		FROM derived_asset_execution_items WHERE id = $1
	`, lease.ExecutionItemID).Scan(&requestRaw, &modelRaw, &capabilityRaw); err != nil {
		return provider.GatewayImageRequest{}, err
	}
	if err := validateDerivedAssetModelSnapshotTx(ctx, tx, modelRaw, capabilityRaw); err != nil {
		return provider.GatewayImageRequest{}, err
	}
	var request provider.GatewayImageRequest
	if err := json.Unmarshal(requestRaw, &request); err != nil {
		return provider.GatewayImageRequest{}, temporal.NewNonRetryableApplicationError("冻结的图片请求无法解析", "DERIVED_ASSET_REQUEST_SNAPSHOT_INVALID", err)
	}
	if request.OrganizationID != lease.OrganizationID || request.ProjectID != lease.ProjectID ||
		request.WorkflowRunID != lease.WorkflowRunID || request.ProviderModelID == "" || request.IdempotencyKey == "" {
		return provider.GatewayImageRequest{}, temporal.NewNonRetryableApplicationError("冻结的图片请求身份不完整", "DERIVED_ASSET_REQUEST_SNAPSHOT_INVALID", nil)
	}
	request.NodeRunID = lease.Execution.NodeRunID
	request.Options.IdempotencyKey = request.IdempotencyKey
	command, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'provider_running', revision = revision + 1,
		    heartbeat_at = now(), lease_expires_at = now() + $3::interval
		WHERE id = $1 AND lease_token = $2 AND status IN ('leased', 'provider_running', 'unknown_outcome')
	`, lease.ExecutionItemID, lease.LeaseToken, derivedAssetExecutionLeaseDuration.String())
	if err != nil {
		return provider.GatewayImageRequest{}, err
	}
	if command.RowsAffected() != 1 {
		return provider.GatewayImageRequest{}, ErrWorkflowWriteFenced
	}
	if err := tx.Commit(ctx); err != nil {
		return provider.GatewayImageRequest{}, err
	}
	return request, nil
}

func (a Activities) VerifyDerivedAssetMedia(
	ctx context.Context,
	input DerivedAssetProviderExecutionOutput,
) (DerivedAssetMediaVerification, error) {
	lease, response := input.Lease, input.Response
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return DerivedAssetMediaVerification{}, err
	}
	defer tx.Rollback(ctx)
	current, err := a.lockDerivedAssetExecutionTx(ctx, tx, lease.ExecutionItemID)
	if err != nil {
		return DerivedAssetMediaVerification{}, err
	}
	if !derivedAssetLeaseMatches(current, lease) {
		return DerivedAssetMediaVerification{}, ErrWorkflowWriteFenced
	}
	if _, err := lockWorkflowBusinessWrite(ctx, tx, lease.WorkflowRunID); err != nil {
		return DerivedAssetMediaVerification{}, err
	}
	var artifactStorageKey, mediaStorageKey string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(artifact.storage_key, ''), media.storage_key
		FROM artifacts artifact
		JOIN media_files media ON media.id = $2 AND media.artifact_id = artifact.id
		WHERE artifact.id = $1
		  AND artifact.organization_id = $3
		  AND artifact.project_id = $4
		  AND artifact.production_generation_id = $5
		  AND media.organization_id = $3
		  AND media.project_id = $4
		  AND media.production_generation_id = $5
	`, response.Output.ArtifactID, response.Output.MediaFileID, lease.OrganizationID, lease.ProjectID, lease.ProductionGenerationID).Scan(
		&artifactStorageKey, &mediaStorageKey,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DerivedAssetMediaVerification{}, temporal.NewApplicationError("供应商图片媒体尚未完成入库", "PROVIDER_IMAGE_MEDIA_NOT_STORED")
		}
		return DerivedAssetMediaVerification{}, err
	}
	if mediaStorageKey != response.Output.StorageKey || (artifactStorageKey != "" && artifactStorageKey != response.Output.StorageKey) {
		return DerivedAssetMediaVerification{}, temporal.NewNonRetryableApplicationError("图片媒体存储身份不一致", "PROVIDER_IMAGE_MEDIA_IDENTITY_MISMATCH", nil)
	}
	command, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'committing', revision = revision + 1,
		    heartbeat_at = now(), lease_expires_at = now() + $3::interval
		WHERE id = $1 AND lease_token = $2 AND status IN ('transferring', 'committing')
	`, lease.ExecutionItemID, lease.LeaseToken, derivedAssetExecutionLeaseDuration.String())
	if err != nil {
		return DerivedAssetMediaVerification{}, err
	}
	if command.RowsAffected() != 1 {
		return DerivedAssetMediaVerification{}, ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.media_ready", "derived_asset_execution_item", lease.ExecutionItemID, mustJSON(map[string]any{
		"batchId": lease.BatchID, "workflowRunId": lease.WorkflowRunID, "artifactId": response.Output.ArtifactID,
		"mediaFileId": response.Output.MediaFileID, "storageKey": response.Output.StorageKey,
	})); err != nil {
		return DerivedAssetMediaVerification{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DerivedAssetMediaVerification{}, err
	}
	return DerivedAssetMediaVerification{
		Lease: lease, ProviderCallID: response.ProviderCallID, ModelID: response.ModelID,
		ArtifactID: response.Output.ArtifactID, MediaFileID: response.Output.MediaFileID,
		StorageKey: response.Output.StorageKey,
	}, nil
}

func (a Activities) CommitDerivedAssetExecution(ctx context.Context, input DerivedAssetMediaVerification) error {
	lease := input.Lease
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	current, err := a.lockDerivedAssetExecutionTx(ctx, tx, lease.ExecutionItemID)
	if err != nil {
		return err
	}
	if current.Status == "succeeded" {
		return tx.Commit(ctx)
	}
	if !derivedAssetLeaseMatches(current, lease) {
		return ErrWorkflowWriteFenced
	}
	if _, err := lockNodeBusinessWrite(ctx, tx, lease.WorkflowRunID, lease.Execution); err != nil {
		return err
	}
	var promptText string
	var promptRaw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT prompt_text, prompt_snapshot FROM derived_asset_execution_items WHERE id = $1
	`, lease.ExecutionItemID).Scan(&promptText, &promptRaw); err != nil {
		return err
	}
	var prompt DerivedAssetPromptSnapshot
	if err := json.Unmarshal(promptRaw, &prompt); err != nil {
		return err
	}
	result := map[string]any{
		"batchId": lease.BatchID, "requestItemId": lease.RequestItemID,
		"workflowRunId":   lease.WorkflowRunID,
		"executionItemId": lease.ExecutionItemID, "requirementId": lease.RequirementID,
		"providerCallId": input.ProviderCallID, "modelId": input.ModelID,
		"artifactId": input.ArtifactID, "mediaFileId": input.MediaFileID,
		"storageKey": input.StorageKey, "promptHash": prompt.Hash,
	}
	resultRaw := mustJSON(result)
	resultHash := HashDerivedAssetSnapshot(result)
	command, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET derived_artifact_id = $2,
		    derived_media_file_id = $3,
		    derived_storage_key = $4,
		    prompt = $5,
		    status = 'image_succeeded', stale_state = 'fresh',
		    metadata = (COALESCE(metadata, '{}'::jsonb)
		      - 'derivedImageWorkflowRunId' - 'lastErrorCode' - 'lastErrorMessage' - 'lastFailedAt')
		      || jsonb_build_object(
		        'derivedAssetBatchId', $6::text,
		        'derivedAssetRequestItemId', $7::text,
		        'derivedAssetExecutionItemId', $8::text,
		        'providerCallId', $9::text,
		        'modelId', $10::text,
		        'promptTemplateKey', $11::text,
		        'promptVersionId', $12::text,
		        'promptHash', $13::text,
		        'promptSource', $14::text,
		        'generatedWorkflowRunId', $15::text,
		        'generatedAt', now()
		      ),
		    updated_at = now()
		WHERE id = $1
		  AND project_id = $16
		  AND production_generation_id = $17
		  AND storyboard_shot_id = $18
		  AND asset_id = $19
		  AND metadata->>'derivedAssetExecutionItemId' = $8
	`, lease.RequirementID, input.ArtifactID, input.MediaFileID, input.StorageKey, promptText,
		lease.BatchID, lease.RequestItemID, lease.ExecutionItemID, input.ProviderCallID, input.ModelID,
		prompt.TemplateKey, prompt.VersionID, prompt.Hash, prompt.Source, lease.WorkflowRunID,
		lease.ProjectID, lease.ProductionGenerationID, lease.StoryboardShotID, lease.CanonicalAssetID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		if err := a.markDerivedAssetExecutionDiscardedTx(ctx, tx, current, "DERIVED_ASSET_TARGET_CHANGED", "镜头衍生资产目标已被其他操作修改"); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return temporal.NewNonRetryableApplicationError("镜头衍生资产目标已被其他操作修改", "DERIVED_ASSET_TARGET_CHANGED", ErrWorkflowWriteFenced)
	}
	command, err = tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'succeeded', revision = revision + 1,
		    output_snapshot = $3, output_hash = $4,
		    lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL, heartbeat_at = NULL,
		    error_code = NULL, error_message = NULL, completed_at = now()
		WHERE id = $1 AND lease_token = $2 AND status = 'committing'
	`, lease.ExecutionItemID, lease.LeaseToken, resultRaw, resultHash)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "shot_asset_requirement.derived_image.generated", "shot_asset_requirement", lease.RequirementID, mustJSON(result)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.succeeded", "derived_asset_execution_item", lease.ExecutionItemID, mustJSON(result)); err != nil {
		return err
	}
	if applied, err := completeNodeRunTx(ctx, tx, lease.Execution, resultRaw); err != nil {
		return err
	} else if !applied {
		return ErrWorkflowWriteFenced
	}
	return tx.Commit(ctx)
}

func (a Activities) FailDerivedAssetExecution(ctx context.Context, failure DerivedAssetExecutionFailure) (bool, error) {
	lease := failure.Lease
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	current, err := a.lockDerivedAssetExecutionTx(ctx, tx, lease.ExecutionItemID)
	if err != nil {
		return false, err
	}
	if isDerivedAssetExecutionTerminal(current.Status) {
		return false, tx.Commit(ctx)
	}
	if !derivedAssetLeaseMatches(current, lease) && !failure.Discarded {
		return false, ErrWorkflowWriteFenced
	}
	status := "failed_terminal"
	if failure.Retryable {
		status = "failed_retryable"
	}
	if failure.Cancelled {
		status = "cancelled"
	}
	if failure.Discarded {
		status = "discarded"
	}
	if strings.TrimSpace(failure.ErrorCode) == "" {
		failure.ErrorCode = codeActivityFailed
	}
	if strings.TrimSpace(failure.ErrorMessage) == "" {
		failure.ErrorMessage = "镜头衍生资产生成失败"
	}
	command, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = $2, revision = revision + 1,
		    error_code = $3, error_message = $4,
		    lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL, heartbeat_at = NULL,
		    completed_at = now()
		WHERE id = $1
		  AND status IN ('prepared', 'queued', 'leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
	`, current.ExecutionItemID, status, failure.ErrorCode, failure.ErrorMessage)
	if err != nil {
		return false, err
	}
	if command.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET status = CASE WHEN $4 = 'cancelled' THEN 'pending' ELSE 'image_failed' END,
		    metadata = (COALESCE(metadata, '{}'::jsonb)
		      - 'derivedImageWorkflowRunId' - 'derivedAssetExecutionItemId')
		      || jsonb_build_object(
		        'lastErrorCode', $2::text,
		        'lastErrorMessage', $3::text,
		        'lastFailedWorkflowRunId', $5::text,
		        'lastFailedExecutionItemId', $6::text,
		        'lastFailedAt', now()
		      ),
		    updated_at = now()
		WHERE id = $1
		  AND project_id = $7
		  AND production_generation_id = $8
		  AND metadata->>'derivedAssetExecutionItemId' = $6
	`, current.RequirementID, failure.ErrorCode, failure.ErrorMessage, status,
		current.WorkflowRunID, current.ExecutionItemID, current.ProjectID, current.ProductionGenerationID); err != nil {
		return false, err
	}
	output := mustJSON(map[string]any{
		"batchId": current.BatchID, "requestItemId": current.RequestItemID,
		"workflowRunId":   current.WorkflowRunID,
		"executionItemId": current.ExecutionItemID, "requirementId": current.RequirementID,
		"status": status, "errorCode": failure.ErrorCode, "errorMessage": failure.ErrorMessage,
	})
	if current.Execution.valid() {
		if _, err := failNodeRunTx(ctx, tx, current.Execution, failure.ErrorCode, failure.ErrorMessage, output); err != nil && !isWorkflowWriteFenced(err) {
			return false, err
		}
	}
	eventType := "derived_asset.item.failed"
	if status == "discarded" {
		eventType = "derived_asset.item.discarded"
	}
	if err := insertEvent(ctx, tx, current.OrganizationID, current.ProjectID, eventType, "derived_asset_execution_item", current.ExecutionItemID, output); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (a Activities) CompleteDerivedAssetBatchWorkflowV2(
	ctx context.Context,
	input TextToStoryboardInput,
	batchID string,
) (DerivedAssetBatchOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT refresh_derived_asset_batch_counts($1)`, batchID); err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	output, err := loadDerivedAssetBatchOutputTx(ctx, tx, batchID)
	if err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	if output.WorkflowRunID != input.WorkflowRunID {
		return DerivedAssetBatchOutput{}, ErrWorkflowWriteFenced
	}
	if isDerivedAssetBatchTerminal(output.Status) {
		output.CompletedItems = output.SucceededItems + output.DuplicateItems + output.SkippedItems
		output.FailedItems = output.TotalItems - output.CompletedItems
		if err := tx.Commit(ctx); err != nil {
			return DerivedAssetBatchOutput{}, err
		}
		return output, nil
	}
	status, code, message := derivedAssetBatchTerminalStatus(output)
	if output.PendingItems+output.QueuedItems+output.RunningItems > 0 {
		return DerivedAssetBatchOutput{}, temporal.NewApplicationError("镜头衍生资产批次仍有未完成项目", "DERIVED_ASSET_BATCH_NOT_SETTLED")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE derived_asset_batches
		SET status = $2, error_code = NULLIF($3, ''), error_message = NULLIF($4, ''),
		    completed_at = COALESCE(completed_at, now()), revision = revision + 1
		WHERE id = $1 AND status IN ('prepared', 'queued', 'running')
	`, batchID, status, code, message); err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	output.Status = status
	output.CompletedItems = output.SucceededItems + output.DuplicateItems + output.SkippedItems
	output.FailedItems = output.TotalItems - output.CompletedItems
	outputRaw := mustJSON(output)
	if status == "cancelled" {
		if _, _, err := cancelWorkflowRunTx(ctx, tx, input.WorkflowRunID, outputRaw, message, code); err != nil {
			return DerivedAssetBatchOutput{}, err
		}
	} else {
		workflowStatus := status
		if workflowStatus == "discarded" {
			workflowStatus = "failed"
		}
		if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, workflowStatus, code, message, outputRaw); err != nil {
			return DerivedAssetBatchOutput{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET total_items = $2, completed_items = $3, failed_items = $4, updated_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, output.TotalItems, output.CompletedItems, output.FailedItems); err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "derived_asset.batch.completed", "derived_asset_batch", batchID, outputRaw); err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	return output, nil
}

func (a Activities) loadDerivedAssetExecution(ctx context.Context, executionItemID string) (DerivedAssetExecutionLease, error) {
	return scanDerivedAssetExecution(a.db.QueryRow(ctx, derivedAssetExecutionSelect+` WHERE execution.id = $1`, executionItemID))
}

func (a Activities) lockDerivedAssetExecutionTx(ctx context.Context, tx pgx.Tx, executionItemID string) (DerivedAssetExecutionLease, error) {
	return scanDerivedAssetExecution(tx.QueryRow(ctx, derivedAssetExecutionSelect+` WHERE execution.id = $1 FOR UPDATE OF execution`, executionItemID))
}

const derivedAssetExecutionSelect = `
	SELECT execution.id::text, execution.request_item_id::text, execution.batch_id::text,
	       request.input_ordinal, execution.requirement_id::text,
	       execution.storyboard_shot_id::text, execution.canonical_asset_id::text,
	       execution.node_run_id::text, execution.node_key, execution.attempt_no, execution.status,
	       execution.organization_id::text, execution.project_id::text, execution.workflow_run_id::text,
	       execution.production_generation_id::text, execution.video_production_binding_id::text,
	       execution.video_production_binding_revision,
	       COALESCE(execution.lease_owner, ''), COALESCE(execution.lease_token::text, ''),
	       node.execution_token::text, node.attempt_generation
	FROM derived_asset_execution_items execution
	JOIN derived_asset_request_items request ON request.id = execution.request_item_id
	JOIN workflow_node_runs node ON node.id = execution.node_run_id
`

func scanDerivedAssetExecution(row pgx.Row) (DerivedAssetExecutionLease, error) {
	var lease DerivedAssetExecutionLease
	var nodeToken string
	var nodeAttempt int
	err := row.Scan(
		&lease.ExecutionItemID, &lease.RequestItemID, &lease.BatchID, &lease.InputOrdinal,
		&lease.RequirementID, &lease.StoryboardShotID, &lease.CanonicalAssetID,
		&lease.NodeRunID, &lease.NodeKey, &lease.AttemptNo, &lease.Status,
		&lease.OrganizationID, &lease.ProjectID, &lease.WorkflowRunID,
		&lease.ProductionGenerationID, &lease.VideoProductionBindingID,
		&lease.VideoProductionBindingRevision, &lease.LeaseOwner, &lease.LeaseToken,
		&nodeToken, &nodeAttempt,
	)
	lease.Execution = NodeExecution{
		NodeRunID: lease.NodeRunID, ExecutionToken: nodeToken, AttemptGeneration: nodeAttempt,
		ProductionGenerationID:         lease.ProductionGenerationID,
		VideoProductionBindingID:       lease.VideoProductionBindingID,
		VideoProductionBindingRevision: lease.VideoProductionBindingRevision,
	}
	return lease, err
}

func (a Activities) validateDerivedAssetFrozenTargetTx(ctx context.Context, tx pgx.Tx, lease DerivedAssetExecutionLease) error {
	var requirementRaw, shotRaw, assetRaw json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT requirement_snapshot, storyboard_shot_snapshot, canonical_asset_snapshot
		FROM derived_asset_execution_items WHERE id = $1
	`, lease.ExecutionItemID).Scan(&requirementRaw, &shotRaw, &assetRaw); err != nil {
		return err
	}
	var frozenRequirement DerivedAssetRequirementSnapshot
	var frozenShot DerivedAssetStoryboardShotSnapshot
	var frozenAsset DerivedAssetCanonicalAssetSnapshot
	if err := json.Unmarshal(requirementRaw, &frozenRequirement); err != nil {
		return temporal.NewNonRetryableApplicationError("镜头资产需求快照无效", "DERIVED_ASSET_SNAPSHOT_INVALID", err)
	}
	if err := json.Unmarshal(shotRaw, &frozenShot); err != nil {
		return temporal.NewNonRetryableApplicationError("分镜快照无效", "DERIVED_ASSET_SNAPSHOT_INVALID", err)
	}
	if err := json.Unmarshal(assetRaw, &frozenAsset); err != nil {
		return temporal.NewNonRetryableApplicationError("基础资产快照无效", "DERIVED_ASSET_SNAPSHOT_INVALID", err)
	}
	var currentRequirement DerivedAssetRequirementSnapshot
	var requirementUpdatedAt time.Time
	var metadata json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, production_generation_id::text,
		       storyboard_shot_id::text, asset_id::text, review_status, status,
		       COALESCE(prompt, ''), updated_at, metadata
		FROM shot_asset_requirements
		WHERE id = $1 AND project_id = $2
	`, lease.RequirementID, lease.ProjectID).Scan(
		&currentRequirement.ID, &currentRequirement.ProjectID, &currentRequirement.ProductionGenerationID,
		&currentRequirement.StoryboardShotID, &currentRequirement.CanonicalAssetID,
		&currentRequirement.ReviewStatus, &currentRequirement.Status, &currentRequirement.Prompt,
		&requirementUpdatedAt, &metadata,
	); err != nil {
		return temporal.NewNonRetryableApplicationError("镜头资产需求不存在", "DERIVED_ASSET_REQUIREMENT_NOT_FOUND", err)
	}
	currentRequirement.UpdatedAt = requirementUpdatedAt.UTC().Format(time.RFC3339Nano)
	ownedRunning := currentRequirement.Status == "image_running" && jsonStringField(metadata, "derivedAssetExecutionItemId") == lease.ExecutionItemID
	if currentRequirement.ID != frozenRequirement.ID ||
		currentRequirement.ProjectID != frozenRequirement.ProjectID ||
		currentRequirement.ProductionGenerationID != frozenRequirement.ProductionGenerationID ||
		currentRequirement.StoryboardShotID != frozenRequirement.StoryboardShotID ||
		currentRequirement.CanonicalAssetID != frozenRequirement.CanonicalAssetID ||
		currentRequirement.ReviewStatus != "approved" || currentRequirement.ReviewStatus != frozenRequirement.ReviewStatus ||
		(!ownedRunning && !sameSnapshotTimestamp(currentRequirement.UpdatedAt, frozenRequirement.UpdatedAt)) ||
		(!ownedRunning && currentRequirement.Status != frozenRequirement.Status) {
		return temporal.NewNonRetryableApplicationError("镜头资产需求在执行前已发生变化", "DERIVED_ASSET_REQUIREMENT_CHANGED", nil)
	}
	var currentShot DerivedAssetStoryboardShotSnapshot
	var shotDeletedAt *time.Time
	var shotUpdatedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, production_generation_id::text,
		       COALESCE(shot_no, shot_index + 1), deleted_at, updated_at
		FROM storyboard_shots WHERE id = $1 AND project_id = $2
	`, lease.StoryboardShotID, lease.ProjectID).Scan(
		&currentShot.ID, &currentShot.ProjectID, &currentShot.ProductionGenerationID,
		&currentShot.ShotNo, &shotDeletedAt, &shotUpdatedAt,
	); err != nil {
		return temporal.NewNonRetryableApplicationError("分镜不存在", "DERIVED_ASSET_SHOT_NOT_FOUND", err)
	}
	if shotDeletedAt != nil {
		currentShot.DeletedAt = shotDeletedAt.UTC().Format(time.RFC3339Nano)
	}
	currentShot.UpdatedAt = shotUpdatedAt.UTC().Format(time.RFC3339Nano)
	if currentShot.ID != frozenShot.ID || currentShot.ProjectID != frozenShot.ProjectID ||
		currentShot.ProductionGenerationID != frozenShot.ProductionGenerationID ||
		currentShot.DeletedAt != "" || !sameSnapshotTimestamp(currentShot.UpdatedAt, frozenShot.UpdatedAt) {
		return temporal.NewNonRetryableApplicationError("分镜在执行前已发生变化", "DERIVED_ASSET_SHOT_CHANGED", nil)
	}
	var currentAsset DerivedAssetCanonicalAssetSnapshot
	var assetUpdatedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, status, revision, prompt_revision, updated_at,
		       COALESCE(primary_reference_artifact_id::text, ''),
		       COALESCE(primary_reference_media_file_id::text, ''),
		       COALESCE(primary_reference_storage_key, ''),
		       COALESCE(reference_artifact_id::text, ''),
		       COALESCE(reference_media_file_id::text, ''),
		       COALESCE(reference_storage_key, '')
		FROM canonical_assets WHERE id = $1 AND project_id = $2
	`, lease.CanonicalAssetID, lease.ProjectID).Scan(
		&currentAsset.ID, &currentAsset.ProjectID, &currentAsset.Status,
		&currentAsset.Revision, &currentAsset.PromptRevision, &assetUpdatedAt,
		&currentAsset.PrimaryReferenceArtifactID, &currentAsset.PrimaryReferenceMediaFileID,
		&currentAsset.PrimaryReferenceStorageKey, &currentAsset.FallbackReferenceArtifactID,
		&currentAsset.FallbackReferenceMediaFileID, &currentAsset.FallbackReferenceStorageKey,
	); err != nil {
		return temporal.NewNonRetryableApplicationError("基础资产不存在", "DERIVED_ASSET_CANONICAL_ASSET_NOT_FOUND", err)
	}
	currentAsset.UpdatedAt = assetUpdatedAt.UTC().Format(time.RFC3339Nano)
	if currentAsset.Status == "archived" || HashDerivedAssetSnapshot(currentAsset) != HashDerivedAssetSnapshot(frozenAsset) {
		return temporal.NewNonRetryableApplicationError("基础资产或参考图在执行前已发生变化", "DERIVED_ASSET_CANONICAL_ASSET_CHANGED", nil)
	}
	return nil
}

func validateDerivedAssetModelSnapshotTx(ctx context.Context, tx pgx.Tx, modelRaw, capabilityRaw json.RawMessage) error {
	var frozenModel DerivedAssetModelSnapshot
	var frozenCapability DerivedAssetCapabilitySnapshot
	if err := json.Unmarshal(modelRaw, &frozenModel); err != nil {
		return temporal.NewNonRetryableApplicationError("模型快照无效", "DERIVED_ASSET_MODEL_SNAPSHOT_INVALID", err)
	}
	if err := json.Unmarshal(capabilityRaw, &frozenCapability); err != nil {
		return temporal.NewNonRetryableApplicationError("模型能力快照无效", "DERIVED_ASSET_MODEL_SNAPSHOT_INVALID", err)
	}
	var currentModel DerivedAssetModelSnapshot
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT model.provider_account_id::text, model.id::text, $2,
		       model.model_key, model.modality, model.status, model.updated_at
		FROM provider_models model WHERE model.id = $1
	`, frozenModel.ProviderModelID, frozenModel.ModelProfileKey).Scan(
		&currentModel.ProviderAccountID, &currentModel.ProviderModelID, &currentModel.ModelProfileKey,
		&currentModel.ModelKey, &currentModel.Modality, &currentModel.Status, &updatedAt,
	); err != nil {
		return temporal.NewNonRetryableApplicationError("冻结的图片模型已不存在", "DERIVED_ASSET_MODEL_UNAVAILABLE", err)
	}
	currentModel.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	if currentModel.Status != "active" || HashDerivedAssetSnapshot(currentModel) != HashDerivedAssetSnapshot(frozenModel) {
		return temporal.NewNonRetryableApplicationError("图片模型配置在执行前已发生变化", "DERIVED_ASSET_MODEL_CHANGED", nil)
	}
	var currentCapability DerivedAssetCapabilitySnapshot
	if err := tx.QueryRow(ctx, `
		SELECT task_types, input_limits, output_limits, quality_tiers,
		       provider_options_schema, pricing_policy
		FROM provider_model_capabilities
		WHERE provider_model_id = $1
		ORDER BY created_at DESC, id DESC LIMIT 1
	`, frozenModel.ProviderModelID).Scan(
		&currentCapability.TaskTypes, &currentCapability.InputLimits, &currentCapability.OutputLimits,
		&currentCapability.QualityTiers, &currentCapability.ProviderOptionsSchema, &currentCapability.PricingPolicy,
	); err != nil {
		return temporal.NewNonRetryableApplicationError("图片模型能力记录不存在", "DERIVED_ASSET_MODEL_CAPABILITY_UNAVAILABLE", err)
	}
	if HashDerivedAssetSnapshot(normalizeDerivedAssetCapability(currentCapability)) != HashDerivedAssetSnapshot(normalizeDerivedAssetCapability(frozenCapability)) {
		return temporal.NewNonRetryableApplicationError("图片模型能力在执行前已发生变化", "DERIVED_ASSET_MODEL_CAPABILITY_CHANGED", nil)
	}
	return nil
}

func normalizeDerivedAssetCapability(snapshot DerivedAssetCapabilitySnapshot) DerivedAssetCapabilitySnapshot {
	snapshot.TaskTypes = jsonOrDefault(snapshot.TaskTypes, `[]`)
	snapshot.InputLimits = jsonOrDefault(snapshot.InputLimits, `{}`)
	snapshot.OutputLimits = jsonOrDefault(snapshot.OutputLimits, `{}`)
	snapshot.QualityTiers = jsonOrDefault(snapshot.QualityTiers, `[]`)
	snapshot.ProviderOptionsSchema = jsonOrDefault(snapshot.ProviderOptionsSchema, `{}`)
	snapshot.PricingPolicy = jsonOrDefault(snapshot.PricingPolicy, `{}`)
	return snapshot
}

func (a Activities) recordDerivedAssetLateResultTx(
	ctx context.Context,
	tx pgx.Tx,
	lease DerivedAssetExecutionLease,
	response provider.GatewayImageResponse,
	reason string,
) error {
	diagnostic := mustJSON(map[string]any{
		"recordedAt": time.Now().UTC().Format(time.RFC3339Nano), "reason": reason,
		"batchId": lease.BatchID, "requestItemId": lease.RequestItemID, "workflowRunId": lease.WorkflowRunID,
		"providerRequestId": response.ProviderRequestID, "providerCallId": response.ProviderCallID,
		"modelId": response.ModelID, "artifactId": response.Output.ArtifactID,
		"mediaFileId": response.Output.MediaFileID, "storageKey": response.Output.StorageKey,
	})
	if _, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET late_result_count = late_result_count + 1,
		    late_result_diagnostics = late_result_diagnostics || jsonb_build_array($2::jsonb),
		    last_late_result_at = now(), revision = revision + 1
		WHERE id = $1
	`, lease.ExecutionItemID, diagnostic); err != nil {
		return err
	}
	return insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.discarded", "derived_asset_execution_item", lease.ExecutionItemID, diagnostic)
}

func (a Activities) markDerivedAssetExecutionDiscardedTx(
	ctx context.Context,
	tx pgx.Tx,
	lease DerivedAssetExecutionLease,
	code string,
	message string,
) error {
	if strings.TrimSpace(code) == "" {
		code = "DERIVED_ASSET_RESULT_DISCARDED"
	}
	if strings.TrimSpace(message) == "" {
		message = "镜头衍生资产执行结果已过期"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'discarded', revision = revision + 1,
		    error_code = $2, error_message = $3,
		    lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL, heartbeat_at = NULL,
		    completed_at = now()
		WHERE id = $1
		  AND status IN ('prepared', 'queued', 'leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
	`, lease.ExecutionItemID, code, message); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET status = 'pending',
		    metadata = COALESCE(metadata, '{}'::jsonb)
		      - 'derivedImageWorkflowRunId' - 'derivedAssetExecutionItemId',
		    updated_at = now()
		WHERE id = $1
		  AND project_id = $3
		  AND production_generation_id = (
		    SELECT active_video_production_generation_id FROM projects WHERE id = $3
		  )
		  AND metadata->>'derivedAssetExecutionItemId' = $2
	`, lease.RequirementID, lease.ExecutionItemID, lease.ProjectID); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.discarded", "derived_asset_execution_item", lease.ExecutionItemID, mustJSON(map[string]any{
		"batchId": lease.BatchID, "requestItemId": lease.RequestItemID,
		"workflowRunId": lease.WorkflowRunID,
		"errorCode":     code, "errorMessage": message,
	})); err != nil {
		return err
	}
	if lease.Execution.valid() {
		if _, err := failNodeRunTx(ctx, tx, lease.Execution, code, message, mustJSON(map[string]any{
			"batchId": lease.BatchID, "requestItemId": lease.RequestItemID,
			"workflowRunId":   lease.WorkflowRunID,
			"executionItemId": lease.ExecutionItemID, "status": "discarded",
		})); err != nil && !isWorkflowWriteFenced(err) {
			return err
		}
	}
	return nil
}

func derivedAssetLeaseMatches(current, expected DerivedAssetExecutionLease) bool {
	return current.ExecutionItemID == expected.ExecutionItemID &&
		current.LeaseOwner == expected.LeaseOwner && current.LeaseToken != "" &&
		current.LeaseToken == expected.LeaseToken &&
		current.WorkflowRunID == expected.WorkflowRunID &&
		current.ProductionGenerationID == expected.ProductionGenerationID &&
		current.VideoProductionBindingID == expected.VideoProductionBindingID &&
		current.VideoProductionBindingRevision == expected.VideoProductionBindingRevision &&
		current.Execution.NodeRunID == expected.Execution.NodeRunID &&
		current.Execution.ExecutionToken == expected.Execution.ExecutionToken &&
		current.Execution.AttemptGeneration == expected.Execution.AttemptGeneration
}

func isDerivedAssetExecutionTerminal(status string) bool {
	switch status {
	case "succeeded", "failed_retryable", "failed_terminal", "cancelled", "discarded":
		return true
	default:
		return false
	}
}

func derivedAssetFailureRetryable(code string) bool {
	switch strings.TrimSpace(code) {
	case provider.CodeUpstreamTimeout, provider.CodeRateLimited, provider.CodeProviderCircuitOpen,
		provider.CodeProviderRequestInProgress, "ACTIVITY_FAILED", "PROVIDER_IMAGE_MEDIA_NOT_STORED",
		"PROVIDER_GATEWAY_UNAVAILABLE", "PROVIDER_REQUEST_TIMEOUT":
		return true
	default:
		return false
	}
}

func sameSnapshotTimestamp(current, frozen string) bool {
	currentTime, currentErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(current))
	frozenTime, frozenErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(frozen))
	if currentErr == nil && frozenErr == nil {
		return currentTime.Equal(frozenTime)
	}
	return strings.TrimSpace(current) == strings.TrimSpace(frozen)
}

func jsonStringField(raw json.RawMessage, key string) string {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func derivedNullableString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func loadDerivedAssetBatchOutputTx(ctx context.Context, tx pgx.Tx, batchID string) (DerivedAssetBatchOutput, error) {
	var output DerivedAssetBatchOutput
	err := tx.QueryRow(ctx, `
		SELECT id::text, status, workflow_run_id::text,
		       total_items, executable_items, review_required_items, not_found_items,
		       generation_mismatch_items, already_running_items, duplicate_items, skipped_items,
		       pending_items, queued_items, running_items, succeeded_items,
		       failed_retryable_items, failed_terminal_items, cancelled_items, discarded_items
		FROM derived_asset_batches WHERE id = $1 FOR UPDATE
	`, batchID).Scan(
		&output.BatchID, &output.Status, &output.WorkflowRunID,
		&output.TotalItems, &output.ExecutableItems, &output.ReviewRequiredItems,
		&output.NotFoundItems, &output.GenerationMismatchItems, &output.AlreadyRunningItems,
		&output.DuplicateItems, &output.SkippedItems, &output.PendingItems,
		&output.QueuedItems, &output.RunningItems, &output.SucceededItems,
		&output.FailedRetryableItems, &output.FailedTerminalItems,
		&output.CancelledItems, &output.DiscardedItems,
	)
	if err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT request.id::text, COALESCE(execution.id::text, ''), request.input_ordinal,
		       request.original_id::text, COALESCE(request.requirement_id::text, ''),
		       request.disposition, request.status, request.retryable,
		       COALESCE(execution.error_code, request.error_code, ''),
		       COALESCE(execution.error_message, request.error_message, ''),
		       COALESCE(execution.provider_call_id::text, ''),
		       COALESCE(execution.model_snapshot->>'providerModelId', ''),
		       COALESCE(execution.artifact_id::text, ''),
		       COALESCE(execution.media_file_id::text, ''), COALESCE(execution.storage_key, '')
		FROM derived_asset_request_items request
		LEFT JOIN derived_asset_execution_items execution ON execution.id = request.current_attempt_id
		WHERE request.batch_id = $1
		ORDER BY request.input_ordinal
	`, batchID)
	if err != nil {
		return DerivedAssetBatchOutput{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item DerivedAssetBatchItemOutput
		if err := rows.Scan(
			&item.RequestItemID, &item.ExecutionItemID, &item.InputOrdinal, &item.OriginalID,
			&item.RequirementID, &item.Disposition, &item.Status, &item.Retryable,
			&item.ErrorCode, &item.ErrorMessage, &item.ProviderCallID, &item.ModelID,
			&item.ArtifactID, &item.MediaFileID, &item.StorageKey,
		); err != nil {
			return DerivedAssetBatchOutput{}, err
		}
		output.Items = append(output.Items, item)
	}
	return output, rows.Err()
}

func derivedAssetBatchTerminalStatus(output DerivedAssetBatchOutput) (string, string, string) {
	if output.TotalItems > 0 && output.SucceededItems == output.TotalItems && output.ExecutableItems == output.TotalItems {
		return "succeeded", "", ""
	}
	if output.SucceededItems > 0 {
		return "partial_succeeded", "DERIVED_ASSET_BATCH_PARTIAL", "部分镜头衍生资产未生成，可在任务活动中重试失败项目"
	}
	if output.CancelledItems > 0 && output.CancelledItems == output.ExecutableItems {
		return "cancelled", "USER_CANCELLED", "镜头衍生资产批次已取消"
	}
	if output.DiscardedItems > 0 && output.DiscardedItems == output.ExecutableItems {
		return "discarded", "PRODUCTION_GENERATION_MISMATCH", "镜头衍生资产批次已因生产代切换而废弃"
	}
	return "failed", "DERIVED_ASSET_BATCH_FAILED", "镜头衍生资产批次没有成功项目"
}

func isDerivedAssetBatchTerminal(status string) bool {
	switch status {
	case "succeeded", "partial_succeeded", "failed", "cancelled", "discarded":
		return true
	default:
		return false
	}
}

func (a Activities) ReconcileExpiredDerivedAssetExecutions(ctx context.Context, limit int) (int, error) {
	return ReconcileExpiredDerivedAssetExecutions(ctx, a, limit)
}

func ReconcileExpiredDerivedAssetExecutions(ctx context.Context, activities Activities, limit int) (int, error) {
	if limit <= 0 {
		limit = 32
	}
	rows, err := activities.db.Query(ctx, `
		SELECT execution.id::text, execution.request_item_id::text, execution.batch_id::text,
		       request.input_ordinal, execution.requirement_id::text,
		       execution.storyboard_shot_id::text, execution.canonical_asset_id::text,
		       execution.node_run_id::text, execution.node_key, execution.attempt_no, execution.status,
		       execution.organization_id::text, execution.project_id::text, execution.workflow_run_id::text,
		       project.active_video_production_generation_id = execution.production_generation_id AS generation_active
		FROM derived_asset_execution_items execution
		JOIN derived_asset_request_items request ON request.id = execution.request_item_id
		JOIN projects project ON project.id = execution.project_id
		WHERE execution.status IN ('prepared', 'queued', 'leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
		  AND (
		    project.active_video_production_generation_id <> execution.production_generation_id
		    OR (
		      execution.status IN ('prepared', 'queued')
		      AND COALESCE(execution.queued_at, execution.created_at) < now() - interval '5 minutes'
		    )
		    OR (execution.lease_expires_at IS NOT NULL AND execution.lease_expires_at <= now())
		  )
		ORDER BY execution.updated_at, execution.id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		Item             DerivedAssetBatchWorkItem
		OrganizationID   string
		ProjectID        string
		WorkflowRunID    string
		GenerationActive bool
	}
	candidates := make([]candidate, 0)
	batchInputs := make(map[string]TextToStoryboardInput)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.Item.ExecutionItemID, &item.Item.RequestItemID, &item.Item.BatchID,
			&item.Item.InputOrdinal, &item.Item.RequirementID, &item.Item.StoryboardShotID,
			&item.Item.CanonicalAssetID, &item.Item.NodeRunID, &item.Item.NodeKey,
			&item.Item.AttemptNo, &item.Item.Status, &item.OrganizationID, &item.ProjectID,
			&item.WorkflowRunID, &item.GenerationActive,
		); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, item)
		batchInputs[item.Item.BatchID] = TextToStoryboardInput{
			OrganizationID: item.OrganizationID, ProjectID: item.ProjectID, WorkflowRunID: item.WorkflowRunID,
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	reconciled := 0
	for _, candidate := range candidates {
		if !candidate.GenerationActive {
			lease, loadErr := activities.loadDerivedAssetExecution(ctx, candidate.Item.ExecutionItemID)
			if loadErr != nil {
				return reconciled, loadErr
			}
			_, failureErr := activities.FailDerivedAssetExecution(ctx, DerivedAssetExecutionFailure{
				Lease: lease, ErrorCode: "PRODUCTION_GENERATION_MISMATCH",
				ErrorMessage: "项目已切换到新的生产代，旧镜头衍生资产执行已废弃", Discarded: true,
			})
			if failureErr != nil && !isWorkflowWriteFenced(failureErr) {
				return reconciled, failureErr
			}
			reconciled++
			continue
		}
		input := TextToStoryboardInput{
			OrganizationID: candidate.OrganizationID, ProjectID: candidate.ProjectID,
			WorkflowRunID: candidate.WorkflowRunID,
		}
		if err := activities.reconcileDerivedAssetExecution(ctx, input, candidate.Item); err != nil {
			if isWorkflowWriteFenced(err) {
				continue
			}
			return reconciled, err
		}
		reconciled++
	}
	for batchID, input := range batchInputs {
		var active int
		if err := activities.db.QueryRow(ctx, `
			SELECT count(*) FROM derived_asset_execution_items
			WHERE batch_id = $1
			  AND status IN ('prepared', 'queued', 'leased', 'provider_running', 'transferring', 'committing', 'unknown_outcome')
		`, batchID).Scan(&active); err != nil {
			return reconciled, err
		}
		if active == 0 {
			if _, err := activities.CompleteDerivedAssetBatchWorkflowV2(ctx, input, batchID); err != nil && !isWorkflowWriteFenced(err) {
				return reconciled, err
			}
			if err := activities.appendDerivedAssetBatchReconciledEvent(ctx, input, batchID); err != nil {
				return reconciled, err
			}
		}
	}
	return reconciled, nil
}

func (a Activities) appendDerivedAssetBatchReconciledEvent(ctx context.Context, input TextToStoryboardInput, batchID string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "derived_asset.batch.reconciled", "derived_asset_batch", batchID, mustJSON(map[string]any{
		"batchId": batchID, "workflowRunId": input.WorkflowRunID,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) reconcileDerivedAssetExecution(ctx context.Context, input TextToStoryboardInput, item DerivedAssetBatchWorkItem) error {
	lease, err := a.ClaimDerivedAssetExecution(ctx, input, item, "derived-asset-reconciler:"+item.ExecutionItemID)
	if err != nil || lease.Terminal {
		return err
	}
	var persisted json.RawMessage
	if err := a.db.QueryRow(ctx, `SELECT COALESCE(provider_result_snapshot, 'null'::jsonb) FROM derived_asset_execution_items WHERE id = $1`, item.ExecutionItemID).Scan(&persisted); err != nil {
		return err
	}
	var response provider.GatewayImageResponse
	if len(persisted) > 0 && string(persisted) != "null" {
		if err := json.Unmarshal(persisted, &response); err != nil {
			return err
		}
	} else {
		request, err := a.prepareDerivedAssetProviderRequest(ctx, lease)
		if err != nil {
			return err
		}
		response, err = a.generateProviderImageWithoutActivityHeartbeat(ctx, request)
		if err != nil {
			_, _ = a.FailDerivedAssetExecution(ctx, DerivedAssetExecutionFailure{
				Lease: lease, ErrorCode: codeActivityFailed, ErrorMessage: err.Error(), Retryable: true,
			})
			return err
		}
		generated, err := a.persistReconciledDerivedAssetProviderResult(ctx, lease, response)
		if err != nil {
			return err
		}
		response = generated.Response
	}
	verified, err := a.VerifyDerivedAssetMedia(ctx, DerivedAssetProviderExecutionOutput{Lease: lease, Response: response})
	if err != nil {
		return err
	}
	return a.CommitDerivedAssetExecution(ctx, verified)
}

func (a Activities) generateProviderImageWithoutActivityHeartbeat(ctx context.Context, request provider.GatewayImageRequest) (provider.GatewayImageResponse, error) {
	if a.gateway == nil {
		return provider.GatewayImageResponse{}, temporal.NewApplicationError("Provider Gateway 未配置", "PROVIDER_GATEWAY_UNAVAILABLE")
	}
	for {
		response, err := a.gateway.GenerateImage(ctx, request)
		if err == nil {
			return response, nil
		}
		standard, ok := provider.StandardErrorFromError(err)
		if !ok || standard.Code != provider.CodeProviderRequestInProgress {
			return provider.GatewayImageResponse{}, err
		}
		delay := providerRequestPollInterval
		if standard.RetryAfterMs > 0 {
			delay = time.Duration(standard.RetryAfterMs) * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return provider.GatewayImageResponse{}, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (a Activities) persistReconciledDerivedAssetProviderResult(
	ctx context.Context,
	lease DerivedAssetExecutionLease,
	response provider.GatewayImageResponse,
) (DerivedAssetProviderExecutionOutput, error) {
	if strings.TrimSpace(response.ProviderCallID) == "" || strings.TrimSpace(response.Output.ArtifactID) == "" ||
		strings.TrimSpace(response.Output.MediaFileID) == "" || strings.TrimSpace(response.Output.StorageKey) == "" {
		return DerivedAssetProviderExecutionOutput{}, fmt.Errorf("reconciled provider image result is incomplete")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	defer tx.Rollback(ctx)
	current, err := a.lockDerivedAssetExecutionTx(ctx, tx, lease.ExecutionItemID)
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	if !derivedAssetLeaseMatches(current, lease) {
		return DerivedAssetProviderExecutionOutput{}, ErrWorkflowWriteFenced
	}
	providerResult := mustJSON(response)
	var selectedCredentialID *string
	_ = tx.QueryRow(ctx, `SELECT credential_id::text FROM provider_call_logs WHERE id = $1`, response.ProviderCallID).Scan(&selectedCredentialID)
	command, err := tx.Exec(ctx, `
		UPDATE derived_asset_execution_items
		SET status = 'transferring', revision = revision + 1,
		    provider_request_id = NULLIF($3, '')::uuid,
		    provider_call_id = NULLIF($4, '')::uuid,
		    selected_credential_id = NULLIF($5, '')::uuid,
		    provider_result_snapshot = $6, provider_result_hash = $7,
		    artifact_id = NULLIF($8, '')::uuid, media_file_id = NULLIF($9, '')::uuid,
		    storage_key = NULLIF($10, ''), heartbeat_at = now(),
		    lease_expires_at = now() + $11::interval
		WHERE id = $1 AND lease_token = $2
	`, lease.ExecutionItemID, lease.LeaseToken, response.ProviderRequestID, response.ProviderCallID,
		derivedNullableString(selectedCredentialID), providerResult, HashDerivedAssetSnapshot(response), response.Output.ArtifactID,
		response.Output.MediaFileID, response.Output.StorageKey, derivedAssetExecutionLeaseDuration.String())
	if err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	if command.RowsAffected() != 1 {
		return DerivedAssetProviderExecutionOutput{}, ErrWorkflowWriteFenced
	}
	if err := insertEvent(ctx, tx, lease.OrganizationID, lease.ProjectID, "derived_asset.item.provider_succeeded", "derived_asset_execution_item", lease.ExecutionItemID, mustJSON(map[string]any{
		"batchId": lease.BatchID, "requestItemId": lease.RequestItemID, "workflowRunId": lease.WorkflowRunID,
		"providerCallId": response.ProviderCallID, "modelId": response.ModelID, "reconciled": true,
	})); err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DerivedAssetProviderExecutionOutput{}, err
	}
	return DerivedAssetProviderExecutionOutput{Lease: lease, Response: response}, nil
}
