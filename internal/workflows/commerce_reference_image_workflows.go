package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	LoadCommerceReferenceImageShotActivityName        = "LoadCommerceReferenceImageShot"
	GenerateCommerceImagePromptItemActivityName       = "GenerateCommerceImagePromptItem"
	GenerateCommerceReferenceImageItemActivityName    = "GenerateCommerceReferenceImageItem"
	FinalizeCommerceReferenceImageBatchActivityName   = "FinalizeCommerceReferenceImageBatch"
	FinalizeCommerceReferenceImageFailureActivityName = "FinalizeCommerceReferenceImageFailure"
)

type CommerceReferenceImageItemAttempt struct {
	ItemID        string `json:"itemId"`
	AttemptID     string `json:"attemptId"`
	AttemptNumber int    `json:"attemptNumber"`
	InputHash     string `json:"inputHash"`
}

type CommerceImagePromptPlanState struct {
	ID                   string                            `json:"id"`
	Revision             int                               `json:"revision"`
	Status               string                            `json:"status"`
	Prompt               string                            `json:"prompt"`
	NegativePrompt       string                            `json:"negativePrompt"`
	PromptHash           string                            `json:"promptHash"`
	InputHash            string                            `json:"inputHash"`
	ReferenceHash        string                            `json:"referenceHash"`
	References           []CommerceReferenceImageReference `json:"references"`
	ImageProviderModelID string                            `json:"imageProviderModelId"`
}

type CommerceShotImageVersionState struct {
	ID                  string `json:"id"`
	Revision            int    `json:"revision"`
	Status              string `json:"status"`
	ArtifactID          string `json:"artifactId,omitempty"`
	MediaFileID         string `json:"mediaFileId,omitempty"`
	StorageKey          string `json:"storageKey,omitempty"`
	ProviderRequestID   string `json:"providerRequestId,omitempty"`
	ProviderCallID      string `json:"providerCallId,omitempty"`
	ProviderModelID     string `json:"providerModelId,omitempty"`
	FidelityStatus      string `json:"fidelityStatus"`
	ReusedFromVersionID string `json:"reusedFromVersionId,omitempty"`
}

type CommerceReferenceImageItemOutput struct {
	ShotID       string                         `json:"shotId"`
	Status       commerce.ProductionItemStatus  `json:"status"`
	PromptPlan   *CommerceImagePromptPlanState  `json:"promptPlan,omitempty"`
	ImageVersion *CommerceShotImageVersionState `json:"imageVersion,omitempty"`
	ErrorCode    string                         `json:"errorCode,omitempty"`
	ErrorMessage string                         `json:"errorMessage,omitempty"`
	Retryable    bool                           `json:"retryable"`
}

type CommerceReferenceImageBatchOutput struct {
	Identity        commerce.UnitGenerationIdentity    `json:"identity"`
	ProductionRunID string                             `json:"productionRunId"`
	Operation       string                             `json:"operation"`
	Status          commerce.ProductionRunStatus       `json:"status"`
	Total           int                                `json:"total"`
	Succeeded       int                                `json:"succeeded"`
	Failed          int                                `json:"failed"`
	Items           []CommerceReferenceImageItemOutput `json:"items"`
}

type CommitCommerceImagePromptPlanInput struct {
	WorkflowInput CommerceReferenceImageBatchInput   `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt  `json:"attempt"`
	Snapshot      CommerceReferenceImageShotSnapshot `json:"snapshot"`
	Contract      CommerceImagePromptPlanContract    `json:"contract"`
	Provenance    CommerceAgentProvenance            `json:"provenance"`
}

type BeginCommerceShotImageVersionInput struct {
	WorkflowInput CommerceReferenceImageBatchInput   `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt  `json:"attempt"`
	Snapshot      CommerceReferenceImageShotSnapshot `json:"snapshot"`
	PromptPlan    CommerceImagePromptPlanState       `json:"promptPlan"`
}

type CompleteCommerceShotImageVersionInput struct {
	WorkflowInput CommerceReferenceImageBatchInput    `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt   `json:"attempt"`
	Snapshot      CommerceReferenceImageShotSnapshot  `json:"snapshot"`
	PromptPlan    CommerceImagePromptPlanState        `json:"promptPlan"`
	ImageVersion  CommerceShotImageVersionState       `json:"imageVersion"`
	Gateway       provider.GatewayImageResponse       `json:"gateway"`
	Fidelity      CommerceImageFidelityReviewContract `json:"fidelity"`
	Provenance    CommerceAgentProvenance             `json:"provenance"`
}

type RecordCommerceShotImageGeneratedInput struct {
	WorkflowInput CommerceReferenceImageBatchInput   `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt  `json:"attempt"`
	Snapshot      CommerceReferenceImageShotSnapshot `json:"snapshot"`
	PromptPlan    CommerceImagePromptPlanState       `json:"promptPlan"`
	ImageVersion  CommerceShotImageVersionState      `json:"imageVersion"`
	Gateway       provider.GatewayImageResponse      `json:"gateway"`
}

type FailCommerceReferenceImageItemInput struct {
	WorkflowInput  CommerceReferenceImageBatchInput  `json:"workflowInput"`
	Attempt        CommerceReferenceImageItemAttempt `json:"attempt"`
	ShotID         string                            `json:"shotId"`
	ImageVersionID string                            `json:"imageVersionId,omitempty"`
	ErrorCode      string                            `json:"errorCode"`
	ErrorMessage   string                            `json:"errorMessage"`
	Retryable      bool                              `json:"retryable"`
}

type FinalizeCommerceReferenceImageFailureInput struct {
	WorkflowInput CommerceReferenceImageBatchInput  `json:"workflowInput"`
	Output        CommerceReferenceImageBatchOutput `json:"output"`
	Cancelled     bool                              `json:"cancelled"`
	ErrorCode     string                            `json:"errorCode"`
	ErrorMessage  string                            `json:"errorMessage"`
}

type CommerceReferenceImagePort interface {
	LoadCommerceReferenceImageShot(context.Context, CommerceReferenceImageBatchInput, string) (CommerceReferenceImageShotSnapshot, error)
	BeginCommerceReferenceImageItem(context.Context, CommerceReferenceImageBatchInput, string) (CommerceReferenceImageItemAttempt, error)
	CommitCommerceImagePromptPlan(context.Context, CommitCommerceImagePromptPlanInput) (CommerceImagePromptPlanState, error)
	LoadApprovedCommerceImagePromptPlan(context.Context, CommerceReferenceImageBatchInput, CommerceReferenceImageShotSnapshot) (CommerceImagePromptPlanState, error)
	BeginCommerceShotImageVersion(context.Context, BeginCommerceShotImageVersionInput) (CommerceShotImageVersionState, error)
	RecordCommerceShotImageGenerated(context.Context, RecordCommerceShotImageGeneratedInput) (CommerceShotImageVersionState, error)
	CompleteCommerceShotImageVersion(context.Context, CompleteCommerceShotImageVersionInput) (CommerceShotImageVersionState, error)
	FailCommerceReferenceImageItem(context.Context, FailCommerceReferenceImageItemInput) error
	FinalizeCommerceReferenceImageBatch(context.Context, CommerceReferenceImageBatchInput, CommerceReferenceImageBatchOutput) (CommerceReferenceImageBatchOutput, error)
	FinalizeCommerceReferenceImageFailure(context.Context, FinalizeCommerceReferenceImageFailureInput) error
}

func RegisterCommerceReferenceImageWorkflow(registrar CommerceWorkflowRegistrar) {
	registrar.RegisterWorkflowWithOptions(CommerceReferenceImageBatchWorkflow, workflow.RegisterOptions{Name: CommerceReferenceImageBatchWorkflowName})
}

func RegisterCommerceReferenceImageActivities(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	registrar.RegisterActivityWithOptions(
		func(ctx context.Context, input CommerceReferenceImageBatchInput, shotID string) (CommerceReferenceImageItemOutput, error) {
			return activities.GenerateCommerceImagePromptItem(ctx, input, shotID), nil
		},
		activity.RegisterOptions{Name: GenerateCommerceImagePromptItemActivityName},
	)
	registrar.RegisterActivityWithOptions(
		func(ctx context.Context, input CommerceReferenceImageBatchInput, shotID string) (CommerceReferenceImageItemOutput, error) {
			return activities.GenerateCommerceReferenceImageItem(ctx, input, shotID), nil
		},
		activity.RegisterOptions{Name: GenerateCommerceReferenceImageItemActivityName},
	)
	registrar.RegisterActivityWithOptions(activities.FinalizeCommerceReferenceImageBatch, activity.RegisterOptions{Name: FinalizeCommerceReferenceImageBatchActivityName})
	registrar.RegisterActivityWithOptions(activities.FinalizeCommerceReferenceImageFailure, activity.RegisterOptions{Name: FinalizeCommerceReferenceImageFailureActivityName})
}

func CommerceReferenceImageBatchWorkflow(ctx workflow.Context, input CommerceReferenceImageBatchInput) (result CommerceReferenceImageBatchOutput, resultErr error) {
	if err := ValidateCommerceUnitGenerationIdentity(input.Identity); err != nil {
		return CommerceReferenceImageBatchOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	if strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ProductionRunID) == "" || len(input.ShotIDs) == 0 {
		return CommerceReferenceImageBatchOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, errors.New("reference image batch identity is incomplete"))
	}
	if strings.TrimSpace(input.StoryboardPlanID) == "" || input.PlanEditRevision < 1 {
		return CommerceReferenceImageBatchOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, errors.New("reference image batch storyboard identity is incomplete"))
	}
	if _, err := commerceReferenceImagePhase(input.Operation); err != nil {
		return CommerceReferenceImageBatchOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	completionPersisted := false
	defer finalizeFailedCommerceReferenceImageBatch(ctx, input, &result, &resultErr, &completionPersisted)
	concurrency := input.Concurrency
	if concurrency < 1 {
		concurrency = 5
	}
	if concurrency > 16 {
		concurrency = 16
	}
	activityOptions := providerImageActivityOptions()
	if activityOptions.RetryPolicy == nil {
		activityOptions.RetryPolicy = &temporal.RetryPolicy{}
	}
	activityOptions.RetryPolicy.MaximumAttempts = 1
	activityCtx := workflow.WithActivityOptions(ctx, activityOptions)
	result = CommerceReferenceImageBatchOutput{
		Identity: input.Identity, ProductionRunID: input.ProductionRunID,
		Operation: input.Operation, Total: len(input.ShotIDs),
		Items: make([]CommerceReferenceImageItemOutput, 0, len(input.ShotIDs)),
	}
	activityName := GenerateCommerceImagePromptItemActivityName
	if input.Operation == "generate_images" {
		activityName = GenerateCommerceReferenceImageItemActivityName
	}
	stopOnBalance := batchStopsOnInsufficientBalance(ctx)
	stopScheduling := false
	stopCode := ""
	stopMessage := ""
	for offset := 0; offset < len(input.ShotIDs); offset += concurrency {
		end := offset + concurrency
		if end > len(input.ShotIDs) {
			end = len(input.ShotIDs)
		}
		futures := make([]workflow.Future, 0, end-offset)
		for _, shotID := range input.ShotIDs[offset:end] {
			futures = append(futures, workflow.ExecuteActivity(activityCtx, activityName, input, shotID))
		}
		for index, future := range futures {
			var item CommerceReferenceImageItemOutput
			if err := future.Get(activityCtx, &item); err != nil {
				code, message := workflowExecutionError(err)
				item = CommerceReferenceImageItemOutput{
					ShotID: input.ShotIDs[offset+index], Status: commerce.ItemFailedRetryable,
					ErrorCode: code, ErrorMessage: message, Retryable: true,
				}
				if isBillingInsufficientBalanceCode(code) {
					item.Status = commerce.ItemFailedTerminal
					item.Retryable = false
				}
			}
			result.Items = append(result.Items, item)
			if item.Status == commerce.ItemSucceeded {
				result.Succeeded++
			} else {
				result.Failed++
			}
			if stopOnBalance && !stopScheduling && isBillingInsufficientBalanceCode(item.ErrorCode) {
				stopScheduling = true
				stopCode = item.ErrorCode
				stopMessage = item.ErrorMessage
			}
		}
		if stopScheduling {
			code, message := unstartedBillingInsufficientBalanceFailure(stopCode, stopMessage)
			for index := end; index < len(input.ShotIDs); index++ {
				result.Items = append(result.Items, CommerceReferenceImageItemOutput{
					ShotID: input.ShotIDs[index], Status: commerce.ItemFailedTerminal,
					ErrorCode: code, ErrorMessage: message, Retryable: false,
				})
				result.Failed++
			}
			break
		}
	}
	switch {
	case result.Succeeded == result.Total:
		result.Status = commerce.RunSucceeded
	case result.Succeeded > 0:
		result.Status = commerce.RunPartiallySucceeded
	default:
		result.Status = commerce.RunFailed
	}
	finalCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var finalized CommerceReferenceImageBatchOutput
	if err := workflow.ExecuteActivity(finalCtx, FinalizeCommerceReferenceImageBatchActivityName, input, result).Get(finalCtx, &finalized); err != nil {
		return result, err
	}
	completionPersisted = true
	return finalized, nil
}

func finalizeFailedCommerceReferenceImageBatch(
	ctx workflow.Context,
	input CommerceReferenceImageBatchInput,
	output *CommerceReferenceImageBatchOutput,
	resultErr *error,
	completionPersisted *bool,
) {
	if output == nil || resultErr == nil || *resultErr == nil || (completionPersisted != nil && *completionPersisted) {
		return
	}
	code, message := workflowExecutionError(*resultErr)
	cancelled := temporal.IsCanceledError(*resultErr)
	if cancelled {
		code = "WORKFLOW_CANCELLED"
		message = "用户取消参考图生产批次"
	}
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	disconnected = workflow.WithActivityOptions(disconnected, defaultActivityOptions())
	if err := workflow.ExecuteActivity(disconnected, FinalizeCommerceReferenceImageFailureActivityName, FinalizeCommerceReferenceImageFailureInput{
		WorkflowInput: input, Output: *output, Cancelled: cancelled,
		ErrorCode: code, ErrorMessage: message,
	}).Get(disconnected, nil); err != nil {
		workflow.GetLogger(ctx).Error("failed to finalize commerce reference image workflow", "error", err)
	}
}

func (a CommerceActivities) GenerateCommerceImagePromptItem(ctx context.Context, input CommerceReferenceImageBatchInput, shotID string) CommerceReferenceImageItemOutput {
	attempt, err := a.beginCommerceReferenceImageItem(ctx, input, shotID)
	if err != nil {
		return commerceReferenceImageFailureOutput(shotID, err)
	}
	snapshot, err := a.loadCommerceReferenceImageShot(ctx, input, shotID)
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", err)
	}
	agent, err := a.runCommerceTextAgent(ctx, CommerceAgentCallInput{
		GenerationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID,
		AttemptGeneration: input.AttemptGeneration, Phase: CommercePhaseImagePrompt,
		SubjectKey: shotID, Round: 1, Binding: snapshot.Bindings.ImagePromptAgent,
		InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.TargetLocale,
		Context: commerceImagePromptAgentContext(snapshot),
	})
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", err)
	}
	contract, err := ParseCommerceImagePromptPlan(agent.RawOutput)
	if err == nil {
		contract = BindCommerceImagePromptPlanIdentity(contract, snapshot)
		err = ValidateCommerceImagePromptPlan(contract, snapshot)
	}
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", temporal.NewNonRetryableApplicationError(err.Error(), CommerceCodeImagePromptContractInvalid, err))
	}
	port, ok := a.referenceImagePort()
	if !ok {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", commerceActivityPortError())
	}
	plan, err := port.CommitCommerceImagePromptPlan(ctx, CommitCommerceImagePromptPlanInput{
		WorkflowInput: input, Attempt: attempt, Snapshot: snapshot,
		Contract: contract, Provenance: agent.Provenance,
	})
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", err)
	}
	return CommerceReferenceImageItemOutput{ShotID: shotID, Status: commerce.ItemSucceeded, PromptPlan: &plan}
}

func commerceImagePromptAgentContext(snapshot CommerceReferenceImageShotSnapshot) json.RawMessage {
	return mustJSON(map[string]any{
		"snapshot":       snapshot,
		"reviewerIssues": snapshot.PreviousFidelityIssues,
		"renderPolicy": map[string]any{
			"imageCount": 1, "frameMoment": "start_state_only",
			"forbidTemporalSequence": true, "forbidStoryboardPanels": true,
			"forbidSplitScreen": true, "forbidCollage": true,
			"instructions": "Describe and render only one still image at the first visible moment. " +
				"Do not include later actions, multiple time points, transitions, or sequential verbs in visualPrompt.",
		},
	})
}

func (a CommerceActivities) GenerateCommerceReferenceImageItem(ctx context.Context, input CommerceReferenceImageBatchInput, shotID string) CommerceReferenceImageItemOutput {
	attempt, err := a.beginCommerceReferenceImageItem(ctx, input, shotID)
	if err != nil {
		return commerceReferenceImageFailureOutput(shotID, err)
	}
	snapshot, err := a.loadCommerceReferenceImageShot(ctx, input, shotID)
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", err)
	}
	port, ok := a.referenceImagePort()
	if !ok {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", commerceActivityPortError())
	}
	plan, err := port.LoadApprovedCommerceImagePromptPlan(ctx, input, snapshot)
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", err)
	}
	version, err := port.BeginCommerceShotImageVersion(ctx, BeginCommerceShotImageVersionInput{
		WorkflowInput: input, Attempt: attempt, Snapshot: snapshot, PromptPlan: plan,
	})
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, "", err)
	}
	if version.Status == "succeeded" {
		return CommerceReferenceImageItemOutput{ShotID: shotID, Status: commerce.ItemSucceeded, ImageVersion: &version}
	}
	if a.Core.gateway == nil || a.Core.db == nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, workflowError{Code: provider.CodeProviderGatewayRequired, Message: "Provider Gateway 未配置"})
	}
	reusedResponse, reusedGeneratedMedia := commerceGatewayImageResponseFromVersion(version)
	node, err := StartNodeRun(ctx, a.Core.db, NodeRunInput{
		OrganizationID: input.Identity.OrganizationID, ProjectID: input.Identity.ProjectID,
		WorkflowRunID: input.WorkflowRunID, NodeKey: "commerce_reference_image_" + shotID,
		NodeType: "image.generate", AttemptGeneration: input.AttemptGeneration,
		Input: mustJSON(map[string]any{
			"shotId": shotID, "promptPlanId": plan.ID, "promptHash": plan.PromptHash,
			"references": plan.References, "reusedGeneratedMedia": reusedGeneratedMedia,
			"reusedFromImageVersionId": version.ReusedFromVersionID,
		}),
	})
	if err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, err)
	}
	response := reusedResponse
	if !reusedGeneratedMedia {
		request := commerceProviderImageRequest(input, snapshot, plan, node)
		response, err = a.Core.generateProviderImage(ctx, node, request)
		if err != nil || response.Status != "succeeded" {
			if err == nil && response.Error != nil {
				err = &provider.StandardErrorError{Standard: *response.Error}
			}
			if err == nil {
				err = errors.New("图片供应商未返回成功状态")
			}
			_ = FailNodeRun(ctx, a.Core.db, node, provider.CodeUpstreamInternalError, err.Error())
			return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, err)
		}
		version, err = port.RecordCommerceShotImageGenerated(ctx, RecordCommerceShotImageGeneratedInput{
			WorkflowInput: input, Attempt: attempt, Snapshot: snapshot,
			PromptPlan: plan, ImageVersion: version, Gateway: response,
		})
		if err != nil {
			_ = FailNodeRun(ctx, a.Core.db, node, codeActivityFailed, err.Error())
			return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, err)
		}
	}
	fidelityReferences := commerceImageFidelityReviewReferences(snapshot.References, response.Output, version.ID)
	reviewAgent, err := a.runCommerceTextAgent(ctx, CommerceAgentCallInput{
		GenerationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID,
		AttemptGeneration: input.AttemptGeneration, Phase: CommercePhaseImageFidelity,
		SubjectKey: shotID, Round: 1, Binding: snapshot.Bindings.ImageFidelityReviewer,
		InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.TargetLocale,
		Context:    commerceImageFidelityAgentContext(snapshot, plan, response.Output, version.ID),
		References: fidelityReferences,
	})
	if err != nil {
		reviewErr := commerceImageFidelityReviewFailure(err)
		_ = FailNodeRun(ctx, a.Core.db, node, CommerceCodeImageFidelityReviewFailed, reviewErr.Error())
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, reviewErr)
	}
	review, err := ParseCommerceImageFidelityReview(reviewAgent.RawOutput)
	if err == nil {
		err = ValidateCommerceImageFidelityReview(review)
	}
	if err == nil {
		review = ReconcileCommerceImageFidelityReview(review)
		err = ValidateCommerceImageFidelityReview(review)
	}
	if err != nil {
		reviewErr := commerceImageFidelityReviewFailure(err)
		_ = FailNodeRun(ctx, a.Core.db, node, CommerceCodeImageFidelityReviewFailed, reviewErr.Error())
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, reviewErr)
	}
	completed, err := port.CompleteCommerceShotImageVersion(ctx, CompleteCommerceShotImageVersionInput{
		WorkflowInput: input, Attempt: attempt, Snapshot: snapshot, PromptPlan: plan,
		ImageVersion: version, Gateway: response, Fidelity: review, Provenance: reviewAgent.Provenance,
	})
	if err != nil {
		_ = FailNodeRun(ctx, a.Core.db, node, codeActivityFailed, err.Error())
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, err)
	}
	if completed.Status != "succeeded" {
		_ = FailNodeRun(ctx, a.Core.db, node, CommerceCodeImageFidelityRejected, "商品保真审核未通过")
		return CommerceReferenceImageItemOutput{
			ShotID: shotID, Status: commerce.ItemFailedTerminal, ImageVersion: &completed,
			ErrorCode: CommerceCodeImageFidelityRejected, ErrorMessage: "商品保真审核未通过",
		}
	}
	if err := CompleteNodeRun(ctx, a.Core.db, node, mustJSON(completed)); err != nil {
		return a.failCommerceReferenceImageItem(ctx, input, attempt, shotID, version.ID, err)
	}
	return CommerceReferenceImageItemOutput{ShotID: shotID, Status: commerce.ItemSucceeded, ImageVersion: &completed}
}

func commerceGatewayImageResponseFromVersion(version CommerceShotImageVersionState) (provider.GatewayImageResponse, bool) {
	if strings.TrimSpace(version.ArtifactID) == "" ||
		strings.TrimSpace(version.MediaFileID) == "" ||
		strings.TrimSpace(version.StorageKey) == "" {
		return provider.GatewayImageResponse{}, false
	}
	return provider.GatewayImageResponse{
		ProviderRequestID: version.ProviderRequestID,
		ProviderCallID:    version.ProviderCallID,
		ModelID:           version.ProviderModelID,
		Status:            "succeeded",
		Output: provider.GatewayImageOutput{
			ArtifactID: version.ArtifactID, MediaFileID: version.MediaFileID,
			StorageKey: version.StorageKey,
		},
	}, true
}

func commerceImageFidelityReviewFailure(cause error) error {
	return temporal.NewApplicationError(
		"参考图已生成并入库，但商品保真审核未完成；重试时将复用已生成图片",
		CommerceCodeImageFidelityReviewFailed,
		cause,
	)
}

func (a CommerceActivities) FinalizeCommerceReferenceImageBatch(ctx context.Context, input CommerceReferenceImageBatchInput, output CommerceReferenceImageBatchOutput) (CommerceReferenceImageBatchOutput, error) {
	port, ok := a.referenceImagePort()
	if !ok {
		return output, commerceActivityPortError()
	}
	return port.FinalizeCommerceReferenceImageBatch(ctx, input, output)
}

func (a CommerceActivities) FinalizeCommerceReferenceImageFailure(ctx context.Context, input FinalizeCommerceReferenceImageFailureInput) error {
	port, ok := a.referenceImagePort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.FinalizeCommerceReferenceImageFailure(ctx, input)
}

func (a CommerceActivities) referenceImagePort() (CommerceReferenceImagePort, bool) {
	port, ok := a.Ports.(CommerceReferenceImagePort)
	return port, ok
}

func (a CommerceActivities) beginCommerceReferenceImageItem(ctx context.Context, input CommerceReferenceImageBatchInput, shotID string) (CommerceReferenceImageItemAttempt, error) {
	port, ok := a.referenceImagePort()
	if !ok {
		return CommerceReferenceImageItemAttempt{}, commerceActivityPortError()
	}
	return port.BeginCommerceReferenceImageItem(ctx, input, shotID)
}

func (a CommerceActivities) loadCommerceReferenceImageShot(ctx context.Context, input CommerceReferenceImageBatchInput, shotID string) (CommerceReferenceImageShotSnapshot, error) {
	port, ok := a.referenceImagePort()
	if !ok {
		return CommerceReferenceImageShotSnapshot{}, commerceActivityPortError()
	}
	return port.LoadCommerceReferenceImageShot(ctx, input, shotID)
}

func (a CommerceActivities) failCommerceReferenceImageItem(ctx context.Context, input CommerceReferenceImageBatchInput, attempt CommerceReferenceImageItemAttempt, shotID, versionID string, cause error) CommerceReferenceImageItemOutput {
	code, message := workflowErrorFields(cause, codeActivityFailed)
	retryable := commerceReferenceImageErrorRetryable(cause)
	if port, ok := a.referenceImagePort(); ok {
		_ = port.FailCommerceReferenceImageItem(context.WithoutCancel(ctx), FailCommerceReferenceImageItemInput{
			WorkflowInput: input, Attempt: attempt, ShotID: shotID, ImageVersionID: versionID,
			ErrorCode: code, ErrorMessage: message, Retryable: retryable,
		})
	}
	status := commerce.ItemFailedTerminal
	if retryable {
		status = commerce.ItemFailedRetryable
	}
	return CommerceReferenceImageItemOutput{
		ShotID: shotID, Status: status,
		ErrorCode: code, ErrorMessage: message, Retryable: retryable,
	}
}

func commerceReferenceImageFailureOutput(shotID string, err error) CommerceReferenceImageItemOutput {
	code, message := workflowErrorFields(err, codeActivityFailed)
	retryable := commerceReferenceImageErrorRetryable(err)
	status := commerce.ItemFailedTerminal
	if retryable {
		status = commerce.ItemFailedRetryable
	}
	return CommerceReferenceImageItemOutput{
		ShotID: shotID, Status: status,
		ErrorCode: code, ErrorMessage: message, Retryable: retryable,
	}
}

func commerceReferenceImageErrorRetryable(err error) bool {
	var standard *provider.StandardErrorError
	if errors.As(err, &standard) {
		return standard.Standard.Retryable
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return !appErr.NonRetryable()
	}
	return false
}

func commerceGatewayImageReferences(items []CommerceReferenceImageReference) []provider.GatewayImageReference {
	references := make([]provider.GatewayImageReference, 0, len(items))
	for _, item := range items {
		references = append(references, provider.GatewayImageReference{
			Type: "commerce_product_reference", ArtifactID: item.ArtifactID,
			StorageKey: item.StorageKey,
			Metadata: mustJSON(map[string]any{
				"referenceKey":       "commerce-product:" + item.ReferenceID,
				"productReferenceId": item.ReferenceID, "packItemId": item.PackItemID,
				"role": item.Role, "contentHash": item.ContentHash,
			}),
		})
	}
	return references
}

func commerceImageFidelityReviewReferences(
	items []CommerceReferenceImageReference,
	generated provider.GatewayImageOutput,
	imageVersionID string,
) []provider.GatewayImageReference {
	references := make([]provider.GatewayImageReference, 0, len(items)+1)
	for index, item := range items {
		references = append(references, provider.GatewayImageReference{
			Type: "commerce_product_reference", ArtifactID: item.ArtifactID,
			StorageKey: item.StorageKey,
			Metadata: mustJSON(map[string]any{
				"referenceKey":       "commerce-product:" + item.ReferenceID,
				"productReferenceId": item.ReferenceID, "packItemId": item.PackItemID,
				"role": item.Role, "contentHash": item.ContentHash,
				"attachmentIndex": index + 1, "reviewRole": "product_reference",
			}),
		})
	}
	references = append(references, provider.GatewayImageReference{
		Type: "commerce_generated_reference", ArtifactID: generated.ArtifactID,
		StorageKey: generated.StorageKey,
		Metadata: mustJSON(map[string]any{
			"referenceKey":    "generated:" + imageVersionID,
			"attachmentIndex": len(items) + 1, "reviewRole": "generated_candidate",
		}),
	})
	return references
}

func commerceImageFidelityAgentContext(
	snapshot CommerceReferenceImageShotSnapshot,
	plan CommerceImagePromptPlanState,
	generated provider.GatewayImageOutput,
	imageVersionID string,
) json.RawMessage {
	return mustJSON(map[string]any{
		"snapshot": snapshot, "imagePromptPlan": plan,
		"generatedImage": map[string]any{
			"artifactId": generated.ArtifactID, "mediaFileId": generated.MediaFileID,
			"storageKey": generated.StorageKey, "imageVersionId": imageVersionID,
			"attachmentIndex": len(snapshot.References) + 1,
		},
		"attachmentGuide": map[string]any{
			"productReferenceAttachmentIndexes": integerRange(1, len(snapshot.References)),
			"generatedCandidateAttachmentIndex": len(snapshot.References) + 1,
			"instructions": "Only the generated candidate attachment is the image under review. " +
				"Product reference attachments are comparison inputs and must never be treated as panels, split screens, or parts of the generated candidate.",
		},
	})
}

func integerRange(start, count int) []int {
	values := make([]int, 0, count)
	for value := start; value < start+count; value++ {
		values = append(values, value)
	}
	return values
}

func commerceProviderImageRequest(
	input CommerceReferenceImageBatchInput,
	snapshot CommerceReferenceImageShotSnapshot,
	plan CommerceImagePromptPlanState,
	node NodeExecution,
) provider.GatewayImageRequest {
	idempotencyKey := fmt.Sprintf("commerce-reference-image:%s:%s", input.WorkflowRunID, snapshot.StoryboardShotID)
	return provider.GatewayImageRequest{
		OrganizationID:    input.Identity.OrganizationID,
		ProjectID:         input.Identity.ProjectID,
		WorkflowRunID:     input.WorkflowRunID,
		NodeRunID:         node.NodeRunID,
		ModelProfileKey:   snapshot.ImageModel.ModelProfileKey,
		ProviderModelID:   snapshot.ImageModel.ProviderModelID,
		PromptTemplateKey: snapshot.Bindings.ImagePromptAgent.TemplateKey,
		PromptVersionID:   snapshot.Bindings.ImagePromptAgent.PromptVersionID,
		PromptHash:        plan.PromptHash,
		PromptSource:      "commerce_image_prompt_plan",
		IdempotencyKey:    idempotencyKey,
		Input: mustJSON(map[string]any{
			"prompt":         plan.Prompt,
			"negativePrompt": plan.NegativePrompt,
			"aspectRatio":    snapshot.AspectRatio,
			"quality":        snapshot.ImageQuality,
			"n":              1,
		}),
		References: commerceGatewayImageReferences(plan.References),
		Options:    provider.GatewayImageOptions{IdempotencyKey: idempotencyKey},
	}
}

func CommerceReferenceImageSubjectHash(input CommerceReferenceImageBatchInput, shotID string) (string, error) {
	return commerceContractHash(map[string]any{
		"identity": input.Identity, "operation": input.Operation,
		"storyboardPlanId": input.StoryboardPlanID, "planEditRevision": input.PlanEditRevision,
		"shotId": shotID, "force": input.Force,
		"reuseGeneratedMedia": commerceReferenceImageMayReuseGeneratedMedia(input, shotID),
	})
}

func commerceReferenceImageMayReuseGeneratedMedia(input CommerceReferenceImageBatchInput, shotID string) bool {
	if !input.Force || input.ReuseGeneratedMedia {
		return true
	}
	for _, reusableShotID := range input.ReuseGeneratedMediaShotIDs {
		if reusableShotID == shotID {
			return true
		}
	}
	return false
}
