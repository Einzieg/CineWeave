package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/assetprompts"
	"github.com/Einzieg/cineweave/internal/production"
	promptsvc "github.com/Einzieg/cineweave/internal/prompts"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	AssetBatchOperationGeneratePrompts   = "generate_prompts"
	AssetBatchOperationGenerateImages    = "generate_images"
	DefaultAssetBatchConcurrency         = 5
	MaxAssetBatchConcurrency             = 16
	assetBatchItemsPerRun                = 50
	failUnstartedAssetBatchItemsActivity = "FailUnstartedAssetBatchItems"
	assetBatchCancellationDrainDeadline  = 2 * time.Minute
)

type AssetBatchVisualSnapshot struct {
	ManualTemplateKey        string `json:"manualTemplateKey,omitempty"`
	ManualPromptVersionID    string `json:"manualPromptVersionId,omitempty"`
	ManualContentHash        string `json:"manualContentHash,omitempty"`
	StyleSlug                string `json:"styleSlug,omitempty"`
	AssetType                string `json:"assetType"`
	PrefixTemplateKey        string `json:"prefixTemplateKey,omitempty"`
	PrefixPromptVersionID    string `json:"prefixPromptVersionId,omitempty"`
	AssetTypeTemplateKey     string `json:"assetTypeTemplateKey,omitempty"`
	AssetTypePromptVersionID string `json:"assetTypePromptVersionId,omitempty"`
	StylePrefix              string `json:"stylePrefix,omitempty"`
	AssetTypeRules           string `json:"assetTypeRules,omitempty"`
	ManualFallback           string `json:"manualFallback,omitempty"`
}

func (v AssetBatchVisualSnapshot) promptVariables() map[string]any {
	return map[string]any{
		"manualTemplateKey": v.ManualTemplateKey, "manualPromptVersionId": v.ManualPromptVersionID,
		"manualContentHash": v.ManualContentHash, "styleSlug": v.StyleSlug,
		"styleFamily": assetprompts.VisualStyleFamily(v.StyleSlug), "assetType": v.AssetType,
		"prefixTemplateKey": v.PrefixTemplateKey, "prefixPromptVersionId": v.PrefixPromptVersionID,
		"assetTypeTemplateKey": v.AssetTypeTemplateKey, "assetTypePromptVersionId": v.AssetTypePromptVersionID,
		"stylePrefix": v.StylePrefix, "assetTypeRules": v.AssetTypeRules, "manualFallback": v.ManualFallback,
	}
}

type AssetBatchProjectSnapshot struct {
	Revision                  int64                             `json:"revision"`
	AspectRatio               string                            `json:"aspectRatio"`
	VideoRatio                string                            `json:"videoRatio"`
	ArtStyle                  string                            `json:"artStyle"`
	ImageQuality              string                            `json:"imageQuality"`
	VideoProductionProfileKey string                            `json:"videoProductionProfileKey"`
	ScriptModelProfileKey     string                            `json:"scriptModelProfileKey"`
	ImageModelProfileKey      string                            `json:"imageModelProfileKey"`
	PromptTemplateKey         string                            `json:"promptTemplateKey"`
	PromptVersionID           string                            `json:"promptVersionId"`
	ManualBindings            []AssetBatchManualBindingSnapshot `json:"manualBindings"`
	ModelBindings             []AssetBatchModelBindingSnapshot  `json:"modelBindings"`
}

type AssetBatchManualBindingSnapshot struct {
	BindingID       string `json:"bindingId"`
	ManualKind      string `json:"manualKind"`
	TemplateKey     string `json:"templateKey"`
	PromptVersionID string `json:"promptVersionId"`
	ContentHash     string `json:"contentHash"`
}

type AssetBatchModelBindingSnapshot struct {
	BindingID       string `json:"bindingId"`
	ProfileID       string `json:"profileId"`
	ProfileKey      string `json:"profileKey"`
	ProviderModelID string `json:"providerModelId"`
	ModelKey        string `json:"modelKey"`
	Modality        string `json:"modality"`
	Priority        int    `json:"priority"`
	Weight          int    `json:"weight"`
	ModelUpdatedAt  string `json:"modelUpdatedAt"`
}

// ProviderModelID returns the first candidate in the immutable routing order
// captured by the API transaction.
func (p AssetBatchProjectSnapshot) ProviderModelID(profileKey string) string {
	profileKey = strings.TrimSpace(profileKey)
	for _, binding := range p.ModelBindings {
		if binding.ProfileKey == profileKey {
			return binding.ProviderModelID
		}
	}
	return ""
}

func (p AssetBatchProjectSnapshot) promptVariables(projectID string) map[string]any {
	return map[string]any{
		"id": projectID, "aspectRatio": p.AspectRatio, "videoRatio": p.VideoRatio,
		"artStyle": p.ArtStyle, "imageQuality": p.ImageQuality, "videoProductionProfileKey": p.VideoProductionProfileKey,
	}
}

type AssetBatchItemSnapshot struct {
	AssetID           string                            `json:"assetId"`
	AssetType         string                            `json:"assetType"`
	Name              string                            `json:"name"`
	Description       string                            `json:"description"`
	Profile           json.RawMessage                   `json:"profile"`
	BasePrompt        string                            `json:"basePrompt,omitempty"`
	ConsistencyPrompt string                            `json:"consistencyPrompt,omitempty"`
	NegativePrompt    string                            `json:"negativePrompt,omitempty"`
	VisualTraits      json.RawMessage                   `json:"visualTraits"`
	ManualOverride    bool                              `json:"manualOverride"`
	Revision          int64                             `json:"revision"`
	PromptRevision    int64                             `json:"promptRevision"`
	SceneContext      string                            `json:"sceneContext,omitempty"`
	Visual            AssetBatchVisualSnapshot          `json:"visual"`
	References        []provider.GatewayImageReference  `json:"references,omitempty"`
	RecoveredImage    *AssetBatchRecoveredImageSnapshot `json:"recoveredImage,omitempty"`
}

type AssetBatchRecoveredImageSnapshot struct {
	SourceWorkflowRunID string `json:"sourceWorkflowRunId"`
	SourceNodeRunID     string `json:"sourceNodeRunId"`
	ProviderCallID      string `json:"providerCallId"`
	ProviderModelID     string `json:"providerModelId"`
	PromptHash          string `json:"promptHash"`
	ArtifactID          string `json:"artifactId"`
	MediaFileID         string `json:"mediaFileId"`
	StorageKey          string `json:"storageKey"`
}

type AssetBatchWorkflowInput struct {
	OrganizationID    string                    `json:"organizationId"`
	ProjectID         string                    `json:"projectId"`
	WorkflowRunID     string                    `json:"workflowRunId"`
	CreatedBy         string                    `json:"createdBy"`
	Operation         string                    `json:"operation"`
	MaxConcurrency    int                       `json:"maxConcurrency"`
	Force             bool                      `json:"force"`
	AttemptGeneration int                       `json:"attemptGeneration"`
	Project           AssetBatchProjectSnapshot `json:"project"`
	Items             []AssetBatchItemSnapshot  `json:"items"`
	NextIndex         int                       `json:"nextIndex,omitempty"`
	Results           []AssetBatchItemOutput    `json:"results,omitempty"`
}

type AssetBatchItemActivityInput struct {
	Batch AssetBatchWorkflowInput `json:"batch"`
	Item  AssetBatchItemSnapshot  `json:"item"`
}

type AssetBatchItemOutput struct {
	AssetID        string `json:"assetId"`
	NodeRunID      string `json:"nodeRunId,omitempty"`
	Status         string `json:"status"`
	Applied        bool   `json:"applied"`
	Conflict       bool   `json:"conflict,omitempty"`
	ProviderCallID string `json:"providerCallId,omitempty"`
	ModelID        string `json:"modelId,omitempty"`
	ArtifactID     string `json:"artifactId,omitempty"`
	MediaFileID    string `json:"mediaFileId,omitempty"`
	StorageKey     string `json:"storageKey,omitempty"`
	ReferenceID    string `json:"referenceId,omitempty"`
	ErrorCode      string `json:"errorCode,omitempty"`
	ErrorMessage   string `json:"errorMessage,omitempty"`
}

type AssetBatchWorkflowOutput struct {
	Operation      string                 `json:"operation"`
	Status         string                 `json:"status"`
	WorkflowRunID  string                 `json:"workflowRunId"`
	TotalItems     int                    `json:"totalItems"`
	CompletedItems int                    `json:"completedItems"`
	FailedItems    int                    `json:"failedItems"`
	CancelledItems int                    `json:"cancelledItems"`
	ActiveItems    int                    `json:"activeItems,omitempty"`
	Items          []AssetBatchItemOutput `json:"items"`
}

type FailUnstartedAssetBatchItemsInput struct {
	Batch        AssetBatchWorkflowInput  `json:"batch"`
	Items        []AssetBatchItemSnapshot `json:"items"`
	ErrorCode    string                   `json:"errorCode"`
	ErrorMessage string                   `json:"errorMessage"`
}

func AssetBatchNodeKey(operation, assetID string) string {
	prefix := "asset_prompt"
	if operation == AssetBatchOperationGenerateImages {
		prefix = "asset_image"
	}
	return nodeKeyForID(prefix, assetID)
}

func BatchGenerateAssetCardsWorkflow(ctx workflow.Context, input AssetBatchWorkflowInput) (AssetBatchWorkflowOutput, error) {
	input.Operation = AssetBatchOperationGeneratePrompts
	return runAssetBatchWorkflow(ctx, input)
}

func BatchGenerateCanonicalAssetImagesWorkflow(ctx workflow.Context, input AssetBatchWorkflowInput) (AssetBatchWorkflowOutput, error) {
	input.Operation = AssetBatchOperationGenerateImages
	return runAssetBatchWorkflow(ctx, input)
}

func GenerateAssetCardItemWorkflow(ctx workflow.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
	ctx = workflow.WithActivityOptions(ctx, providerTextActivityOptions())
	var output AssetBatchItemOutput
	err := workflow.ExecuteActivity(ctx, "GenerateAssetCardBatchItem", input).Get(ctx, &output)
	return output, err
}

func GenerateCanonicalAssetImageItemWorkflow(ctx workflow.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
	ctx = workflow.WithActivityOptions(ctx, providerImageActivityOptions())
	var output AssetBatchItemOutput
	err := workflow.ExecuteActivity(ctx, "GenerateCanonicalAssetImageBatchItem", input).Get(ctx, &output)
	return output, err
}

func runAssetBatchWorkflow(ctx workflow.Context, input AssetBatchWorkflowInput) (AssetBatchWorkflowOutput, error) {
	if err := validateAssetBatchInput(input); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	input.MaxConcurrency = clampConcurrency(input.MaxConcurrency, DefaultAssetBatchConcurrency, MaxAssetBatchConcurrency)
	if input.AttemptGeneration <= 0 {
		input.AttemptGeneration = 1
	}
	output := AssetBatchWorkflowOutput{
		Operation: input.Operation, WorkflowRunID: input.WorkflowRunID,
		TotalItems: len(input.Items), Items: append([]AssetBatchItemOutput(nil), input.Results...),
	}
	start := input.NextIndex
	if start < 0 || start > len(input.Items) {
		start = 0
	}
	end := start + assetBatchItemsPerRun
	if end > len(input.Items) {
		end = len(input.Items)
	}
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for batchStart := start; batchStart < end; batchStart += input.MaxConcurrency {
		batchEnd := batchStart + input.MaxConcurrency
		if batchEnd > end {
			batchEnd = end
		}
		futures := make([]workflow.ChildWorkflowFuture, 0, batchEnd-batchStart)
		for index := batchStart; index < batchEnd; index++ {
			item := input.Items[index]
			childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID:          fmt.Sprintf("%s/%s/%s/g%d", input.WorkflowRunID, input.Operation, item.AssetID, input.AttemptGeneration),
				ParentClosePolicy:   enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
				WaitForCancellation: true,
			})
			activityInput := AssetBatchItemActivityInput{Batch: input, Item: item}
			if input.Operation == AssetBatchOperationGeneratePrompts {
				futures = append(futures, workflow.ExecuteChildWorkflow(childCtx, GenerateAssetCardItemWorkflow, activityInput))
			} else {
				futures = append(futures, workflow.ExecuteChildWorkflow(childCtx, GenerateCanonicalAssetImageItemWorkflow, activityInput))
			}
		}
		for offset, future := range futures {
			item := input.Items[batchStart+offset]
			var itemOutput AssetBatchItemOutput
			if err := future.Get(ctx, &itemOutput); err != nil {
				if isWorkflowCancellationError(err) {
					output = drainAssetBatchChildrenAfterCancellation(ctx, input, batchStart, futures, output)
					output.Status = "cancelled"
					appendUnfinishedAssetBatchItems(&output, input.Items)
					_ = finalizeAssetBatchAfterCancellation(ctx, input, output)
					return output, err
				}
				code, message := workflowExecutionError(err)
				itemOutput = AssetBatchItemOutput{AssetID: item.AssetID, Status: "failed", ErrorCode: code, ErrorMessage: message}
			}
			output.Items = append(output.Items, itemOutput)
			if stopOnBalance && !stopScheduling && isBillingInsufficientBalanceCode(itemOutput.ErrorCode) {
				stopScheduling = true
				stopCode = itemOutput.ErrorCode
				stopMessage = itemOutput.ErrorMessage
			}
		}
		if stopScheduling {
			code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
			var unstarted []AssetBatchItemOutput
			if err := workflow.ExecuteActivity(
				workflow.WithActivityOptions(ctx, defaultActivityOptions()),
				failUnstartedAssetBatchItemsActivity,
				FailUnstartedAssetBatchItemsInput{
					Batch: input, Items: append([]AssetBatchItemSnapshot(nil), input.Items[batchEnd:]...),
					ErrorCode: code, ErrorMessage: message,
				},
			).Get(ctx, &unstarted); err != nil {
				return AssetBatchWorkflowOutput{}, err
			}
			output.Items = append(output.Items, unstarted...)
			end = len(input.Items)
			break
		}
	}
	if end < len(input.Items) {
		input.NextIndex = end
		input.Results = output.Items
		return AssetBatchWorkflowOutput{}, workflow.NewContinueAsNewError(ctx, assetBatchParentWorkflow(input.Operation), input)
	}
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions())
	if err := workflow.ExecuteActivity(ctx, "CompleteAssetBatchWorkflow", input, output).Get(ctx, &output); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	return output, nil
}

func (a Activities) FailUnstartedAssetBatchItems(
	ctx context.Context,
	input FailUnstartedAssetBatchItemsInput,
) ([]AssetBatchItemOutput, error) {
	if !isBillingInsufficientBalanceCode(input.ErrorCode) {
		return nil, temporal.NewNonRetryableApplicationError(
			"unstarted asset batch failures require a billing balance denial",
			"INVALID_ASSET_BATCH_BALANCE_STOP",
			nil,
		)
	}
	code, message := unstartedBillingInsufficientBalanceFailure(input.ErrorCode, input.ErrorMessage)
	outputs := make([]AssetBatchItemOutput, 0, len(input.Items))
	nodeType := "asset.prompt.generate"
	if input.Batch.Operation == AssetBatchOperationGenerateImages {
		nodeType = "asset.image.generate"
	}
	for _, item := range input.Items {
		output := AssetBatchItemOutput{
			AssetID: item.AssetID, Status: "failed",
			ErrorCode: code, ErrorMessage: message,
		}
		execution, err := StartNodeRun(ctx, a.db, NodeRunInput{
			OrganizationID:    input.Batch.OrganizationID,
			ProjectID:         input.Batch.ProjectID,
			WorkflowRunID:     input.Batch.WorkflowRunID,
			NodeKey:           AssetBatchNodeKey(input.Batch.Operation, item.AssetID),
			NodeType:          nodeType,
			Input:             mustJSON(item),
			AttemptGeneration: input.Batch.AttemptGeneration,
		})
		if err != nil {
			return nil, err
		}
		output.NodeRunID = execution.NodeRunID
		if err := FailNodeRunWithOutput(
			ctx,
			a.db,
			execution,
			code,
			message,
			mustJSON(output),
		); err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

func drainAssetBatchChildrenAfterCancellation(
	ctx workflow.Context,
	input AssetBatchWorkflowInput,
	batchStart int,
	futures []workflow.ChildWorkflowFuture,
	output AssetBatchWorkflowOutput,
) AssetBatchWorkflowOutput {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	selector := workflow.NewSelector(disconnected)
	seen := make(map[string]bool, len(output.Items))
	for _, item := range output.Items {
		seen[item.AssetID] = true
	}
	pending := len(futures)
	timedOut := false
	for offset, childFuture := range futures {
		offset := offset
		childFuture := childFuture
		selector.AddFuture(childFuture, func(f workflow.Future) {
			defer func() { pending-- }()
			item := input.Items[batchStart+offset]
			if seen[item.AssetID] {
				return
			}
			var itemOutput AssetBatchItemOutput
			if err := f.Get(disconnected, &itemOutput); err != nil {
				itemOutput = AssetBatchItemOutput{
					AssetID: item.AssetID, Status: "cancelled", ErrorCode: "USER_CANCELLED",
					ErrorMessage: "batch was cancelled",
				}
			}
			seen[item.AssetID] = true
			output.Items = append(output.Items, itemOutput)
		})
	}
	deadline := workflow.NewTimer(disconnected, assetBatchCancellationDrainDeadline)
	selector.AddFuture(deadline, func(workflow.Future) { timedOut = true })
	for pending > 0 && !timedOut {
		selector.Select(disconnected)
	}
	return output
}

func validateAssetBatchInput(input AssetBatchWorkflowInput) error {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return temporal.NewNonRetryableApplicationError("organizationId, projectId, and workflowRunId are required", "INVALID_ASSET_BATCH", nil)
	}
	if input.Operation != AssetBatchOperationGeneratePrompts && input.Operation != AssetBatchOperationGenerateImages {
		return temporal.NewNonRetryableApplicationError("asset batch operation is invalid", "INVALID_ASSET_BATCH", nil)
	}
	if len(input.Items) == 0 {
		return temporal.NewNonRetryableApplicationError("asset batch requires at least one item", "INVALID_ASSET_BATCH", nil)
	}
	return nil
}

func assetBatchParentWorkflow(operation string) any {
	if operation == AssetBatchOperationGenerateImages {
		return BatchGenerateCanonicalAssetImagesWorkflow
	}
	return BatchGenerateAssetCardsWorkflow
}

func appendUnfinishedAssetBatchItems(output *AssetBatchWorkflowOutput, items []AssetBatchItemSnapshot) {
	seen := make(map[string]bool, len(output.Items))
	for _, item := range output.Items {
		seen[item.AssetID] = true
	}
	for _, item := range items {
		if !seen[item.AssetID] {
			output.Items = append(output.Items, AssetBatchItemOutput{AssetID: item.AssetID, Status: "cancelled", ErrorCode: "USER_CANCELLED", ErrorMessage: "batch was cancelled"})
		}
	}
}

func finalizeAssetBatchAfterCancellation(ctx workflow.Context, input AssetBatchWorkflowInput, output AssetBatchWorkflowOutput) error {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	disconnected = workflow.WithActivityOptions(disconnected, defaultActivityOptions())
	return workflow.ExecuteActivity(disconnected, "CompleteAssetBatchWorkflow", input, output).Get(disconnected, nil)
}

func workflowExecutionError(err error) (string, string) {
	if err == nil {
		return codeActivityFailed, "任务步骤执行失败"
	}
	var workflowErr workflowError
	if errors.As(err, &workflowErr) {
		code := strings.TrimSpace(workflowErr.Code)
		if code == "" {
			code = codeActivityFailed
		}
		message := strings.TrimSpace(workflowErr.Message)
		if message == "" {
			message = "任务步骤执行失败"
		}
		return code, message
	}
	if standard, ok := provider.StandardErrorFromError(err); ok {
		code := strings.TrimSpace(standard.Code)
		if code == "" {
			code = codeActivityFailed
		}
		message := strings.TrimSpace(standard.Message)
		if message == "" {
			message = strings.TrimSpace(err.Error())
		}
		return code, message
	}
	var applicationError *temporal.ApplicationError
	if errors.As(err, &applicationError) {
		code := strings.TrimSpace(applicationError.Type())
		if code == "" {
			code = codeActivityFailed
		}
		message := strings.TrimSpace(applicationError.Message())
		if message == "" {
			message = strings.TrimSpace(applicationError.Error())
		}
		return code, message
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "任务步骤执行失败"
	}
	return codeActivityFailed, message
}

func (a Activities) GenerateAssetCardBatchItem(ctx context.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
	item, batch := input.Item, input.Batch
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: batch.OrganizationID, ProjectID: batch.ProjectID, WorkflowRunID: batch.WorkflowRunID,
		NodeKey: AssetBatchNodeKey(batch.Operation, item.AssetID), NodeType: "asset.prompt.generate", Input: mustJSON(item),
		AttemptGeneration: batch.AttemptGeneration,
	})
	if err != nil {
		return AssetBatchItemOutput{}, err
	}
	if replay, ok, err := a.assetBatchItemReplay(ctx, nodeExecution.NodeRunID); err != nil {
		return AssetBatchItemOutput{}, err
	} else if ok {
		if err := CompleteNodeRun(ctx, a.db, nodeExecution, mustJSON(replay)); err != nil {
			return AssetBatchItemOutput{}, err
		}
		return replay, nil
	}
	if err := a.ensureAssetSnapshotCurrent(ctx, batch.ProjectID, item); err != nil {
		output := assetBatchConflictOutput(item.AssetID, nodeExecution.NodeRunID, err.Error())
		_ = FailNodeRunWithOutput(ctx, a.db, nodeExecution, output.ErrorCode, output.ErrorMessage, mustJSON(output))
		return output, nil
	}
	providerModelID := batch.Project.ProviderModelID(batch.Project.ScriptModelProfileKey)
	if providerModelID == "" {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowError{Code: "MODEL_PROFILE_NOT_CONFIGURED", Message: "script model snapshot has no active provider model"})
	}
	canonicalSource := assetprompts.BuildCanonicalCardSource(item.AssetType, item.Description, item.VisualTraits, item.SceneContext)
	variables := map[string]any{
		"project": batch.Project.promptVariables(batch.ProjectID), "visualContext": item.Visual.promptVariables(),
		"asset": map[string]any{
			"id": item.AssetID, "assetType": item.AssetType, "name": item.Name,
			"description": canonicalSource.Description, "visualTraits": canonicalSource.VisualTraits,
			"canonicalBaselinePolicy": map[string]any{
				"stableIdentityOnly": true, "transientStateTarget": "shot_derived_asset",
			},
		},
		"scenes":             canonicalSource.SceneContext,
		"validationFeedback": "", "previousRejectedDraft": map[string]any{},
	}
	var rendered promptsvc.RenderedPrompt
	var response provider.GatewayTextResponse
	var draft assetprompts.CardDraft
	for attempt := 1; attempt <= 3; attempt++ {
		rendered, err = a.renderWorkflowPromptVersion(ctx, batch.OrganizationID, batch.ProjectID, batch.Project.PromptTemplateKey, batch.Project.PromptVersionID, variables)
		if err != nil {
			return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, err)
		}
		idempotencyKey := assetBatchProviderKey(batch, item, attempt)
		response, err = a.generateProviderText(ctx, nodeExecution, provider.GatewayTextRequest{
			OrganizationID: batch.OrganizationID, ProjectID: batch.ProjectID, WorkflowRunID: batch.WorkflowRunID, NodeRunID: nodeExecution.NodeRunID,
			ModelProfileKey: batch.Project.ScriptModelProfileKey, ProviderModelID: providerModelID, PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash, PromptSource: rendered.Source,
			IdempotencyKey: idempotencyKey,
			Options:        provider.GatewayTextOptions{TimeoutMS: providerTextGatewayTimeoutMS, IdempotencyKey: idempotencyKey},
			Input:          mustJSON(map[string]any{"prompt": rendered.RenderedText, "responseFormat": "json"}),
		})
		if err != nil {
			return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowErrorFromProvider(err, codeActivityFailed))
		}
		draft, err = assetprompts.NormalizeCardDraft(response.Output.Text)
		if err == nil {
			err = assetprompts.ValidateGeneratedCardStyle(item.Visual.StyleSlug, draft.BasePrompt, draft.ConsistencyPrompt)
		}
		if err == nil {
			err = assetprompts.ValidateCanonicalAssetBaseline(item.AssetType, draft.BasePrompt, draft.ConsistencyPrompt)
		}
		if err == nil {
			break
		}
		if attempt == 3 {
			return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowError{Code: "ASSET_CARD_VISUAL_CONTRACT_FAILED", Message: err.Error()})
		}
		variables["validationFeedback"] = err.Error()
		variables["previousRejectedDraft"] = draft
	}
	output, err := a.applyAssetCardBatchResult(ctx, batch, item, nodeExecution, rendered, response, draft)
	if err != nil {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, err)
	}
	return output, nil
}

func (a Activities) GenerateCanonicalAssetImageBatchItem(ctx context.Context, input AssetBatchItemActivityInput) (AssetBatchItemOutput, error) {
	item, batch := input.Item, input.Batch
	nodeExecution, err := StartNodeRun(ctx, a.db, NodeRunInput{
		OrganizationID: batch.OrganizationID, ProjectID: batch.ProjectID, WorkflowRunID: batch.WorkflowRunID,
		NodeKey: AssetBatchNodeKey(batch.Operation, item.AssetID), NodeType: "asset.image.generate", Input: mustJSON(item),
		AttemptGeneration: batch.AttemptGeneration,
	})
	if err != nil {
		return AssetBatchItemOutput{}, err
	}
	if replay, ok, err := a.assetBatchItemReplay(ctx, nodeExecution.NodeRunID); err != nil {
		return AssetBatchItemOutput{}, err
	} else if ok {
		if err := CompleteNodeRun(ctx, a.db, nodeExecution, mustJSON(replay)); err != nil {
			return AssetBatchItemOutput{}, err
		}
		return replay, nil
	}
	if strings.TrimSpace(item.BasePrompt) == "" || strings.TrimSpace(item.ConsistencyPrompt) == "" {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowError{Code: "ASSET_PROMPT_NOT_READY", Message: "canonical asset prompt card is not ready"})
	}
	if err := a.ensureAssetSnapshotCurrent(ctx, batch.ProjectID, item); err != nil {
		output := assetBatchConflictOutput(item.AssetID, nodeExecution.NodeRunID, err.Error())
		_ = FailNodeRunWithOutput(ctx, a.db, nodeExecution, output.ErrorCode, output.ErrorMessage, mustJSON(output))
		return output, nil
	}
	if a.gateway == nil {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowError{Code: "PROVIDER_GATEWAY_UNAVAILABLE", Message: "provider gateway is not configured"})
	}
	providerModelID := batch.Project.ProviderModelID(batch.Project.ImageModelProfileKey)
	if providerModelID == "" {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowError{Code: "MODEL_PROFILE_NOT_CONFIGURED", Message: "image model snapshot has no active provider model"})
	}
	rendered, err := a.renderWorkflowPromptVersion(ctx, batch.OrganizationID, batch.ProjectID, batch.Project.PromptTemplateKey, batch.Project.PromptVersionID, map[string]any{
		"project": batch.Project.promptVariables(batch.ProjectID),
		"asset": map[string]any{
			"assetType": item.AssetType, "type": item.AssetType, "name": item.Name, "description": item.Description,
			"profile": string(item.Profile), "basePrompt": item.BasePrompt, "consistencyPrompt": item.ConsistencyPrompt,
			"negativePrompt": item.NegativePrompt, "visualTraits": string(item.VisualTraits),
		},
	})
	if err != nil {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, err)
	}
	rendered, err = a.withAssetBatchVisualSnapshot(ctx, batch, item, rendered)
	if err != nil {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, err)
	}
	rendered = withCanonicalAssetImageRequirements(rendered, item.AssetType)
	rendered.RenderedText = assetprompts.RuntimeImagePrompt(rendered.RenderedText)
	rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
	idempotencyKey := assetBatchProviderKey(batch, item, 1)
	response, recovered := recoveredAssetBatchImage(item, providerModelID, rendered.RenderedHash)
	if !recovered {
		response, err = a.generateProviderImage(ctx, nodeExecution, provider.GatewayImageRequest{
			OrganizationID: batch.OrganizationID, ProjectID: batch.ProjectID, WorkflowRunID: batch.WorkflowRunID, NodeRunID: nodeExecution.NodeRunID,
			ModelProfileKey: batch.Project.ImageModelProfileKey, ProviderModelID: providerModelID, PromptTemplateKey: rendered.TemplateKey,
			PromptVersionID: rendered.PromptVersionID, PromptHash: rendered.RenderedHash, PromptSource: rendered.Source,
			IdempotencyKey: idempotencyKey, Options: provider.GatewayImageOptions{IdempotencyKey: idempotencyKey},
			Input:      mustJSON(assetprompts.CanonicalImageInput(rendered.RenderedText, item.AssetType, batch.Project.ImageQuality)),
			References: item.References,
		})
		if err != nil {
			a.markAssetBatchImageFailed(ctx, batch.ProjectID, nodeExecution, item, err)
			return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, workflowErrorFromProvider(err, codeActivityFailed))
		}
	}
	output, err := a.applyAssetImageBatchResult(ctx, batch, item, nodeExecution, rendered, response)
	if err != nil {
		return AssetBatchItemOutput{}, a.failAssetBatchActivity(ctx, nodeExecution, err)
	}
	return output, nil
}

func recoveredAssetBatchImage(item AssetBatchItemSnapshot, providerModelID, promptHash string) (provider.GatewayImageResponse, bool) {
	recovered := item.RecoveredImage
	if recovered == nil || recovered.ProviderModelID != providerModelID || recovered.PromptHash != promptHash ||
		strings.TrimSpace(recovered.ProviderCallID) == "" || strings.TrimSpace(recovered.ArtifactID) == "" ||
		strings.TrimSpace(recovered.MediaFileID) == "" || strings.TrimSpace(recovered.StorageKey) == "" {
		return provider.GatewayImageResponse{}, false
	}
	return provider.GatewayImageResponse{
		Status:         "succeeded",
		ProviderCallID: recovered.ProviderCallID,
		ModelID:        recovered.ProviderModelID,
		Output: provider.GatewayImageOutput{
			ArtifactID:  recovered.ArtifactID,
			MediaFileID: recovered.MediaFileID,
			StorageKey:  recovered.StorageKey,
		},
	}, true
}

func (a Activities) renderWorkflowPromptVersion(ctx context.Context, organizationID, projectID, templateKey, versionID string, variables map[string]any) (promptsvc.RenderedPrompt, error) {
	resolved, err := promptsvc.NewService(a.db).ResolveVersion(ctx, promptsvc.ResolveRequest{
		OrganizationID: organizationID, ProjectID: projectID, TemplateKey: templateKey,
	}, versionID)
	if err != nil {
		return promptsvc.RenderedPrompt{}, workflowErrorFromPrompt(err)
	}
	rendered, err := promptsvc.Render(resolved, variables)
	if err != nil {
		return promptsvc.RenderedPrompt{}, workflowErrorFromPrompt(err)
	}
	return rendered, nil
}

func (a Activities) withAssetBatchVisualSnapshot(ctx context.Context, batch AssetBatchWorkflowInput, item AssetBatchItemSnapshot, rendered promptsvc.RenderedPrompt) (promptsvc.RenderedPrompt, error) {
	visual := item.Visual
	if visual.PrefixTemplateKey == "" || visual.PrefixPromptVersionID == "" || visual.AssetTypeTemplateKey == "" || visual.AssetTypePromptVersionID == "" {
		return rendered, nil
	}
	prefix, err := a.renderWorkflowPromptVersion(ctx, batch.OrganizationID, batch.ProjectID, visual.PrefixTemplateKey, visual.PrefixPromptVersionID, map[string]any{})
	if err != nil {
		return promptsvc.RenderedPrompt{}, err
	}
	target, err := a.renderWorkflowPromptVersion(ctx, batch.OrganizationID, batch.ProjectID, visual.AssetTypeTemplateKey, visual.AssetTypePromptVersionID, map[string]any{})
	if err != nil {
		return promptsvc.RenderedPrompt{}, err
	}
	rules := strings.TrimSpace(strings.Join(compactStrings([]string{prefix.RenderedText, target.RenderedText}), "\n\n"))
	if rules != "" {
		rendered.RenderedText = rules + "\n\n" + strings.TrimSpace(rendered.RenderedText)
		rendered.RenderedHash = promptsvc.HashText(rendered.RenderedText)
		rendered.Source += "+visual_snapshot"
	}
	return rendered, nil
}

func (a Activities) ensureAssetSnapshotCurrent(ctx context.Context, projectID string, item AssetBatchItemSnapshot) error {
	var revision, promptRevision int64
	var status string
	err := a.db.QueryRow(ctx, `SELECT revision, prompt_revision, status FROM canonical_assets WHERE project_id = $1 AND id = $2`, projectID, item.AssetID).Scan(&revision, &promptRevision, &status)
	if err != nil {
		return err
	}
	if status == "archived" {
		return fmt.Errorf("canonical asset is archived")
	}
	if revision != item.Revision || promptRevision != item.PromptRevision {
		return fmt.Errorf("asset revision changed from %d/%d to %d/%d", item.Revision, item.PromptRevision, revision, promptRevision)
	}
	return nil
}

func assetBatchConflictOutput(assetID, nodeRunID, message string) AssetBatchItemOutput {
	return AssetBatchItemOutput{
		AssetID: assetID, NodeRunID: nodeRunID, Status: "conflict_skipped", Conflict: true,
		ErrorCode: "ASSET_REVISION_CONFLICT", ErrorMessage: message,
	}
}

func assetBatchProviderKey(batch AssetBatchWorkflowInput, item AssetBatchItemSnapshot, validationAttempt int) string {
	return fmt.Sprintf("asset-batch:%s:%s:g%d:v%d", batch.WorkflowRunID, AssetBatchNodeKey(batch.Operation, item.AssetID), batch.AttemptGeneration, validationAttempt)
}

func assetBatchEventContext(batch AssetBatchWorkflowInput, item AssetBatchItemSnapshot, nodeRunID string) map[string]any {
	return map[string]any{
		"workflowRunId":     batch.WorkflowRunID,
		"nodeRunId":         nodeRunID,
		"attemptGeneration": batch.AttemptGeneration,
		"operation":         batch.Operation,
		"assetId":           item.AssetID,
	}
}

func (a Activities) failAssetBatchActivity(ctx context.Context, execution NodeExecution, cause error) error {
	if isWorkflowWriteFenced(cause) {
		return discardWorkflowResult(ctx, a.db, execution, cause.Error())
	}
	code, message := workflowErrorFields(cause, codeActivityFailed)
	_ = FailNodeRun(ctx, a.db, execution, code, message)
	return newWorkflowApplicationError(cause, code, message)
}

func (a Activities) assetBatchItemReplay(ctx context.Context, nodeRunID string) (AssetBatchItemOutput, bool, error) {
	var raw []byte
	err := a.db.QueryRow(ctx, `
		SELECT metadata->'batchOutput'
		FROM asset_versions
		WHERE node_run_id = $1 AND metadata ? 'batchOutput'
	`, nodeRunID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return AssetBatchItemOutput{}, false, nil
	}
	if err != nil {
		return AssetBatchItemOutput{}, false, err
	}
	var output AssetBatchItemOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return AssetBatchItemOutput{}, false, err
	}
	return output, true, nil
}

func (a Activities) applyAssetCardBatchResult(
	ctx context.Context,
	batch AssetBatchWorkflowInput,
	item AssetBatchItemSnapshot,
	execution NodeExecution,
	rendered promptsvc.RenderedPrompt,
	response provider.GatewayTextResponse,
	draft assetprompts.CardDraft,
) (AssetBatchItemOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return AssetBatchItemOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, batch.WorkflowRunID, execution); err != nil {
		return AssetBatchItemOutput{}, err
	}
	var revision, promptRevision int64
	var manualOverride bool
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT revision, prompt_revision, manual_override, status
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, batch.ProjectID, item.AssetID).Scan(&revision, &promptRevision, &manualOverride, &status); err != nil {
		return AssetBatchItemOutput{}, err
	}
	conflict := status == "archived" || revision != item.Revision || promptRevision != item.PromptRevision
	applied := !conflict && (!manualOverride || batch.Force)
	resultState, outputStatus := "applied", "succeeded"
	if conflict {
		resultState, outputStatus = "conflict_skipped", "conflict_skipped"
	} else if !applied {
		resultState, outputStatus = "historical", "suggestion_only"
	}
	output := AssetBatchItemOutput{
		AssetID: item.AssetID, NodeRunID: execution.NodeRunID, Status: outputStatus, Applied: applied,
		Conflict: conflict, ProviderCallID: response.ProviderCallID, ModelID: response.ModelID,
	}
	if conflict {
		output.ErrorCode = "ASSET_REVISION_CONFLICT"
		output.ErrorMessage = fmt.Sprintf("asset revision changed from %d/%d to %d/%d", item.Revision, item.PromptRevision, revision, promptRevision)
	}
	provenance := assetBatchEventContext(batch, item, execution.NodeRunID)
	provenance["source"] = "asset_batch_prompt"
	provenance["providerCallId"] = response.ProviderCallID
	provenance["modelId"] = response.ModelID
	provenance["promptTemplateKey"] = rendered.TemplateKey
	provenance["promptVersionId"] = rendered.PromptVersionID
	provenance["promptHash"] = rendered.RenderedHash
	provenance["visualManualPromptVersionId"] = item.Visual.ManualPromptVersionID
	provenance["visualStyleSlug"] = item.Visual.StyleSlug
	provenance["profile"] = draft.Profile
	provenance["consistencyPrompt"] = draft.ConsistencyPrompt
	provenance["negativePrompt"] = draft.NegativePrompt
	provenance["batchOutput"] = output
	if applied {
		if _, err := tx.Exec(ctx, `
			UPDATE canonical_assets
			SET profile = $3,
			    base_prompt = NULLIF($4, ''),
			    consistency_prompt = NULLIF($5, ''),
			    negative_prompt = NULLIF($6, ''),
			    manual_override = false,
			    status = 'prompt_ready',
			    stale_state = 'fresh',
			    metadata = COALESCE(metadata, '{}'::jsonb) || $7,
			    revision = revision + 1,
			    prompt_revision = prompt_revision + 1,
			    updated_at = now()
			WHERE id = $1 AND project_id = $2
		`, item.AssetID, batch.ProjectID, draft.Profile, draft.BasePrompt, draft.ConsistencyPrompt, draft.NegativePrompt, mustJSON(provenance)); err != nil {
			return AssetBatchItemOutput{}, err
		}
		if err := production.MarkAssetProductionMaterialStale(ctx, tx, batch.ProjectID, item.AssetID); err != nil {
			return AssetBatchItemOutput{}, err
		}
		if err := production.MarkFinalVideoStale(ctx, tx, batch.ProjectID, ""); err != nil {
			return AssetBatchItemOutput{}, err
		}
	}
	if err := insertAssetBatchVersion(
		ctx, tx, batch, item, execution.NodeRunID, draft.BasePrompt,
		"", "", "", rendered.PromptVersionID, rendered.RenderedHash,
		resultState, provenance,
	); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if err := insertEvent(ctx, tx, batch.OrganizationID, batch.ProjectID, "asset.batch.prompt.completed", "canonical_asset", item.AssetID, mustJSON(provenance)); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if output.Conflict {
		if _, err := failNodeRunTx(ctx, tx, execution, output.ErrorCode, output.ErrorMessage, mustJSON(output)); err != nil {
			return AssetBatchItemOutput{}, err
		}
	} else if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AssetBatchItemOutput{}, err
	}
	return output, nil
}

func (a Activities) applyAssetImageBatchResult(
	ctx context.Context,
	batch AssetBatchWorkflowInput,
	item AssetBatchItemSnapshot,
	execution NodeExecution,
	rendered promptsvc.RenderedPrompt,
	response provider.GatewayImageResponse,
) (AssetBatchItemOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return AssetBatchItemOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockNodeBusinessWrite(ctx, tx, batch.WorkflowRunID, execution); err != nil {
		return AssetBatchItemOutput{}, err
	}
	var revision, promptRevision int64
	var status string
	var primaryArtifactID, primaryMediaFileID, primaryStorageKey *string
	if err := tx.QueryRow(ctx, `
		SELECT revision, prompt_revision, status,
		       primary_reference_artifact_id::text, primary_reference_media_file_id::text, primary_reference_storage_key
		FROM canonical_assets
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, batch.ProjectID, item.AssetID).Scan(
		&revision, &promptRevision, &status, &primaryArtifactID, &primaryMediaFileID, &primaryStorageKey,
	); err != nil {
		return AssetBatchItemOutput{}, err
	}
	conflict := status == "archived" || revision != item.Revision || promptRevision != item.PromptRevision
	resultState, outputStatus := "applied", "succeeded"
	if conflict {
		resultState, outputStatus = "conflict_skipped", "conflict_skipped"
	}
	output := AssetBatchItemOutput{
		AssetID: item.AssetID, NodeRunID: execution.NodeRunID, Status: outputStatus, Applied: !conflict, Conflict: conflict,
		ProviderCallID: response.ProviderCallID, ModelID: response.ModelID,
		ArtifactID: response.Output.ArtifactID, MediaFileID: response.Output.MediaFileID,
		StorageKey: response.Output.StorageKey,
	}
	if conflict {
		output.ErrorCode = "ASSET_REVISION_CONFLICT"
		output.ErrorMessage = fmt.Sprintf("asset revision changed from %d/%d to %d/%d", item.Revision, item.PromptRevision, revision, promptRevision)
	}
	shouldPrimary := !conflict && primaryArtifactID == nil && primaryMediaFileID == nil && primaryStorageKey == nil
	metadata := assetBatchEventContext(batch, item, execution.NodeRunID)
	metadata["source"] = "asset_batch_image"
	metadata["providerCallId"] = response.ProviderCallID
	metadata["modelId"] = response.ModelID
	metadata["promptTemplateKey"] = rendered.TemplateKey
	metadata["promptVersionId"] = rendered.PromptVersionID
	metadata["promptHash"] = rendered.RenderedHash
	metadata["batchOutput"] = output
	metadata["resultState"] = resultState
	if recovered := item.RecoveredImage; recovered != nil && recovered.ProviderCallID == response.ProviderCallID {
		metadata["reusedProviderResult"] = true
		metadata["sourceWorkflowRunId"] = recovered.SourceWorkflowRunID
		metadata["sourceNodeRunId"] = recovered.SourceNodeRunID
	}
	var referenceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO asset_references(
			organization_id, project_id, asset_id, reference_type, title, description,
			artifact_id, media_file_id, storage_key, prompt, prompt_version_id, prompt_hash,
			is_primary, metadata, created_by, workflow_run_id, node_run_id,
			source_asset_revision, source_prompt_revision, result_state
		)
		VALUES (
			$1, $2, $3, 'generated', $4, $5,
			NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, NULLIF($8, ''),
			$9, NULLIF($10, '')::uuid, NULLIF($11, ''),
			$12, $13, $14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (node_run_id) WHERE node_run_id IS NOT NULL
		DO UPDATE SET metadata = EXCLUDED.metadata
		RETURNING id::text
	`, batch.OrganizationID, batch.ProjectID, item.AssetID, "Generated reference", item.Description,
		response.Output.ArtifactID, response.Output.MediaFileID, response.Output.StorageKey,
		rendered.RenderedText, rendered.PromptVersionID, rendered.RenderedHash,
		shouldPrimary, mustJSON(metadata), batch.CreatedBy, batch.WorkflowRunID, execution.NodeRunID,
		item.Revision, item.PromptRevision, resultState).Scan(&referenceID); err != nil {
		return AssetBatchItemOutput{}, err
	}
	output.ReferenceID = referenceID
	metadata["batchOutput"] = output
	if !conflict {
		if _, err := tx.Exec(ctx, `
			UPDATE canonical_assets
			SET reference_artifact_id = NULLIF($3, '')::uuid,
			    reference_media_file_id = NULLIF($4, '')::uuid,
			    reference_storage_key = NULLIF($5, ''),
			    primary_reference_artifact_id = CASE WHEN $6 THEN NULLIF($3, '')::uuid ELSE primary_reference_artifact_id END,
			    primary_reference_media_file_id = CASE WHEN $6 THEN NULLIF($4, '')::uuid ELSE primary_reference_media_file_id END,
			    primary_reference_storage_key = CASE WHEN $6 THEN NULLIF($5, '') ELSE primary_reference_storage_key END,
			    status = 'image_succeeded',
			    stale_state = 'fresh',
			    revision = revision + 1,
			    updated_at = now()
			WHERE id = $1 AND project_id = $2
		`, item.AssetID, batch.ProjectID, response.Output.ArtifactID, response.Output.MediaFileID, response.Output.StorageKey, shouldPrimary); err != nil {
			return AssetBatchItemOutput{}, err
		}
		if err := production.MarkAssetProductionMaterialStale(ctx, tx, batch.ProjectID, item.AssetID); err != nil {
			return AssetBatchItemOutput{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE asset_references SET metadata = $2 WHERE id = $1`, referenceID, mustJSON(metadata)); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if err := insertAssetBatchVersion(
		ctx, tx, batch, item, execution.NodeRunID, item.BasePrompt,
		response.Output.ArtifactID, response.Output.MediaFileID, response.Output.StorageKey,
		rendered.PromptVersionID, rendered.RenderedHash, resultState, metadata,
	); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if err := insertEvent(ctx, tx, batch.OrganizationID, batch.ProjectID, "asset.batch.image.completed", "canonical_asset", item.AssetID, mustJSON(metadata)); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if output.Conflict {
		if _, err := failNodeRunTx(ctx, tx, execution, output.ErrorCode, output.ErrorMessage, mustJSON(output)); err != nil {
			return AssetBatchItemOutput{}, err
		}
	} else if _, err := completeNodeRunTx(ctx, tx, execution, mustJSON(output)); err != nil {
		return AssetBatchItemOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AssetBatchItemOutput{}, err
	}
	return output, nil
}

func insertAssetBatchVersion(
	ctx context.Context,
	tx pgx.Tx,
	batch AssetBatchWorkflowInput,
	item AssetBatchItemSnapshot,
	nodeRunID, basePrompt, artifactID, mediaFileID, storageKey, promptVersionID, promptHash, resultState string,
	metadata map[string]any,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO asset_versions(
			organization_id, project_id, asset_id, version, description, base_prompt, visual_traits,
			reference_artifact_id, reference_media_file_id, reference_storage_key, prompt_version_id,
			prompt_hash, metadata, created_by, workflow_run_id, node_run_id,
			source_asset_revision, source_prompt_revision, result_state
		)
		SELECT $1, $2, $3, COALESCE(MAX(version), 0) + 1, $4, NULLIF($5, ''), $6,
		       NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, ''),
		       NULLIF($10, '')::uuid, NULLIF($11, ''), $12, $13, $14, $15, $16, $17, $18
		FROM asset_versions
		WHERE asset_id = $3
		ON CONFLICT (node_run_id) WHERE node_run_id IS NOT NULL
		DO UPDATE SET metadata = EXCLUDED.metadata
	`, batch.OrganizationID, batch.ProjectID, item.AssetID, item.Description, basePrompt,
		jsonOrDefault(item.VisualTraits, `{}`), artifactID, mediaFileID, storageKey,
		promptVersionID, promptHash, mustJSON(metadata), batch.CreatedBy,
		batch.WorkflowRunID, nodeRunID, item.Revision, item.PromptRevision, resultState)
	return err
}

func (a Activities) markAssetBatchImageFailed(ctx context.Context, projectID string, execution NodeExecution, item AssetBatchItemSnapshot, cause error) {
	_, _ = a.db.Exec(ctx, `
		UPDATE canonical_assets
		SET status = 'image_failed',
		    metadata = COALESCE(metadata, '{}'::jsonb)
		               || jsonb_build_object('imageFailedReason', $5::text, 'imageFailedAt', now()),
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $3 AND prompt_revision = $4
		  AND EXISTS (
			SELECT 1
			FROM workflow_node_runs node
			JOIN workflow_runs run ON run.id = node.workflow_run_id
			WHERE node.id = $6
			  AND node.execution_token = $7
			  AND node.attempt_generation = $8
			  AND node.status = 'running'
			  AND run.status = 'running'
		  )
	`, projectID, item.AssetID, item.Revision, item.PromptRevision, cause.Error(), execution.NodeRunID, execution.ExecutionToken, execution.AttemptGeneration)
}

func (a Activities) CompleteAssetBatchWorkflow(
	ctx context.Context,
	input AssetBatchWorkflowInput,
	requested AssetBatchWorkflowOutput,
) (AssetBatchWorkflowOutput, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockWorkflowRunContext(ctx, tx, input.WorkflowRunID); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	if err := reconcileAssetBatchRequestedTerminals(ctx, tx, input, requested); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	output, err := loadAssetBatchOutput(ctx, tx, input)
	if err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	if output.ActiveItems > 0 {
		return output, temporal.NewApplicationError(
			fmt.Sprintf("asset batch still has %d active item(s)", output.ActiveItems),
			"ASSET_BATCH_ITEMS_ACTIVE",
		)
	}
	if requested.Status == "cancelled" {
		output.Status = "cancelled"
	}
	outputJSON := mustJSON(output)
	errorCode, errorMessage := "", ""
	if output.Status == "failed" {
		errorCode, errorMessage = "BATCH_ALL_FAILED", "all asset batch items failed"
	}
	if output.Status == "cancelled" {
		errorCode, errorMessage = "USER_CANCELLED", "batch was cancelled"
	}
	var applied bool
	if output.Status == "cancelled" {
		_, applied, err = cancelWorkflowRunTx(ctx, tx, input.WorkflowRunID, outputJSON, errorMessage, errorCode)
	} else {
		_, applied, err = transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, output.Status, errorCode, errorMessage, outputJSON)
	}
	if err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	if !applied {
		if err := tx.Commit(ctx); err != nil {
			return AssetBatchWorkflowOutput{}, err
		}
		return output, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET total_items = $2,
		    completed_items = $3,
		    failed_items = $4,
		    updated_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, output.TotalItems, output.CompletedItems, output.FailedItems); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	return output, nil
}

func reconcileAssetBatchRequestedTerminals(
	ctx context.Context,
	tx pgx.Tx,
	input AssetBatchWorkflowInput,
	requested AssetBatchWorkflowOutput,
) error {
	for _, item := range requested.Items {
		status, eventType, code, message := "", "", strings.TrimSpace(item.ErrorCode), strings.TrimSpace(item.ErrorMessage)
		switch item.Status {
		case "failed", "conflict_skipped":
			status, eventType = "failed", "workflow.node.failed"
			if code == "" {
				code = codeActivityFailed
			}
			if message == "" {
				message = "asset batch item failed"
			}
		case "cancelled":
			status, eventType = "cancelled", "workflow.node.cancelled"
			if code == "" {
				code = "USER_CANCELLED"
			}
			if message == "" {
				message = "asset batch item was cancelled"
			}
		default:
			continue
		}
		if strings.TrimSpace(item.AssetID) == "" {
			continue
		}

		var nodeRunID, nodeKey string
		var nodeRevision int64
		err := tx.QueryRow(ctx, `
			UPDATE workflow_node_runs
			SET status = $3,
			    output = $4,
			    error_code = NULLIF($5, ''),
			    error_message = NULLIF($6, ''),
			    completed_at = COALESCE(completed_at, now()),
			    revision = revision + 1,
			    updated_at = now()
			WHERE workflow_run_id = $1
			  AND node_key = $2
			  AND status IN ('pending', 'queued', 'running', 'waiting_review', 'cancelling')
			RETURNING id::text, node_key, revision
		`, input.WorkflowRunID, AssetBatchNodeKey(input.Operation, item.AssetID), status, mustJSON(item), code, message).Scan(
			&nodeRunID, &nodeKey, &nodeRevision,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, eventType, "workflow_node_run", nodeRunID, mustJSON(map[string]any{
			"workflowRunId": input.WorkflowRunID,
			"nodeRunId":     nodeRunID,
			"nodeKey":       nodeKey,
			"assetId":       item.AssetID,
			"status":        status,
			"code":          code,
			"message":       message,
			"revision":      nodeRevision,
			"reconciledBy":  "asset_batch_parent",
		})); err != nil {
			return err
		}
	}
	return nil
}

func loadAssetBatchOutput(ctx context.Context, tx pgx.Tx, input AssetBatchWorkflowInput) (AssetBatchWorkflowOutput, error) {
	rows, err := tx.Query(ctx, `
		SELECT status, input, output, COALESCE(error_code, ''), COALESCE(error_message, '')
		FROM workflow_node_runs
		WHERE workflow_run_id = $1
		ORDER BY created_at, node_key
	`, input.WorkflowRunID)
	if err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	defer rows.Close()
	output := AssetBatchWorkflowOutput{
		Operation: input.Operation, WorkflowRunID: input.WorkflowRunID,
		TotalItems: len(input.Items), Items: make([]AssetBatchItemOutput, 0, len(input.Items)),
	}
	for rows.Next() {
		var status, code, message string
		var inputJSON, outputJSON []byte
		if err := rows.Scan(&status, &inputJSON, &outputJSON, &code, &message); err != nil {
			return AssetBatchWorkflowOutput{}, err
		}
		var itemOutput AssetBatchItemOutput
		_ = json.Unmarshal(outputJSON, &itemOutput)
		if itemOutput.AssetID == "" {
			var item AssetBatchItemSnapshot
			_ = json.Unmarshal(inputJSON, &item)
			itemOutput.AssetID = item.AssetID
		}
		itemOutput.Status = firstNonEmptyString(itemOutput.Status, status)
		itemOutput.ErrorCode = firstNonEmptyString(itemOutput.ErrorCode, code)
		itemOutput.ErrorMessage = firstNonEmptyString(itemOutput.ErrorMessage, message)
		output.Items = append(output.Items, itemOutput)
		switch status {
		case "succeeded", "skipped":
			output.CompletedItems++
		case "failed":
			output.FailedItems++
		case "cancelled":
			output.CancelledItems++
		default:
			output.ActiveItems++
		}
	}
	if err := rows.Err(); err != nil {
		return AssetBatchWorkflowOutput{}, err
	}
	if missingItems := output.TotalItems - len(output.Items); missingItems > 0 {
		output.ActiveItems += missingItems
	}
	classifyAssetBatchOutput(&output)
	return output, nil
}

func classifyAssetBatchOutput(output *AssetBatchWorkflowOutput) {
	switch {
	case output.ActiveItems > 0:
		output.Status = "running"
	case output.CancelledItems > 0 && output.CompletedItems == 0 && output.FailedItems == 0:
		output.Status = "cancelled"
	case output.FailedItems == 0 && output.CancelledItems == 0:
		output.Status = "succeeded"
	case output.CompletedItems == 0:
		output.Status = "failed"
	default:
		output.Status = "partial_succeeded"
	}
}
