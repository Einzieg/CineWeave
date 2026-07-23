package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	GenerateCommerceVideoPromptItemActivityName = "GenerateCommerceVideoPromptItem"
	LoadCommerceVideoExecutionShotActivityName  = "LoadCommerceVideoExecutionShot"
	BeginCommerceVideoItemActivityName          = "BeginCommerceVideoItem"
	CompleteCommerceShotVideoItemActivityName   = "CompleteCommerceShotVideoItem"
	FailCommerceVideoItemActivityName           = "FailCommerceVideoItem"
	FinalizeCommerceVideoBatchActivityName      = "FinalizeCommerceVideoBatch"
	FinalizeCommerceVideoFailureActivityName    = "FinalizeCommerceVideoFailure"
)

type CommerceVideoPromptPlanState struct {
	ID                  string `json:"id"`
	Revision            int    `json:"revision"`
	Status              string `json:"status"`
	Prompt              string `json:"prompt"`
	PromptHash          string `json:"promptHash"`
	PromptContextPlanID string `json:"promptContextPlanId"`
	ReferencePackID     string `json:"referencePackId"`
	ShotStateHash       string `json:"shotStateHash"`
}

type CommerceVideoItemOutput struct {
	ShotID            string                        `json:"shotId"`
	Status            commerce.ProductionItemStatus `json:"status"`
	PromptPlan        *CommerceVideoPromptPlanState `json:"promptPlan,omitempty"`
	VideoRenderPlanID string                        `json:"videoRenderPlanId,omitempty"`
	ArtifactID        string                        `json:"artifactId,omitempty"`
	MediaFileID       string                        `json:"mediaFileId,omitempty"`
	StorageKey        string                        `json:"storageKey,omitempty"`
	ErrorCode         string                        `json:"errorCode,omitempty"`
	ErrorMessage      string                        `json:"errorMessage,omitempty"`
	Retryable         bool                          `json:"retryable"`
}

type CommerceVideoBatchOutput struct {
	Identity        commerce.UnitGenerationIdentity `json:"identity"`
	ProductionRunID string                          `json:"productionRunId"`
	Operation       string                          `json:"operation"`
	Status          commerce.ProductionRunStatus    `json:"status"`
	Total           int                             `json:"total"`
	Succeeded       int                             `json:"succeeded"`
	Failed          int                             `json:"failed"`
	Items           []CommerceVideoItemOutput       `json:"items"`
}

type CommitCommerceVideoPromptPlanInput struct {
	WorkflowInput CommerceVideoBatchInput           `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt `json:"attempt"`
	Snapshot      CommerceVideoPromptShotSnapshot   `json:"snapshot"`
	Contract      CommerceVideoPromptPlanContract   `json:"contract"`
	Review        CommerceVideoPromptReviewContract `json:"review"`
	Generation    CommerceAgentProvenance           `json:"generation"`
	Reviewer      CommerceAgentProvenance           `json:"reviewer"`
}

type CommerceVideoExecutionShot struct {
	ShotID           string `json:"shotId"`
	ShotIndex        int    `json:"shotIndex"`
	ShotNo           int    `json:"shotNo"`
	AspectRatio      string `json:"aspectRatio"`
	Resolution       string `json:"resolution"`
	AudioStrategy    string `json:"audioStrategy"`
	AudioRequirement string `json:"audioRequirement"`
}

type CompleteCommerceShotVideoItemInput struct {
	WorkflowInput CommerceVideoBatchInput           `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt `json:"attempt"`
	Shot          CommerceVideoExecutionShot        `json:"shot"`
	Result        ShotRenderExecutionResult         `json:"result"`
}

type FailCommerceVideoItemInput struct {
	WorkflowInput CommerceVideoBatchInput           `json:"workflowInput"`
	Attempt       CommerceReferenceImageItemAttempt `json:"attempt"`
	ShotID        string                            `json:"shotId"`
	ErrorCode     string                            `json:"errorCode"`
	ErrorMessage  string                            `json:"errorMessage"`
	Retryable     bool                              `json:"retryable"`
}

type FinalizeCommerceVideoFailureInput struct {
	WorkflowInput CommerceVideoBatchInput  `json:"workflowInput"`
	Output        CommerceVideoBatchOutput `json:"output"`
	Cancelled     bool                     `json:"cancelled"`
	ErrorCode     string                   `json:"errorCode"`
	ErrorMessage  string                   `json:"errorMessage"`
}

type CommerceVideoPort interface {
	LoadCommerceVideoPromptShot(context.Context, CommerceVideoBatchInput, string) (CommerceVideoPromptShotSnapshot, error)
	LoadCommerceVideoExecutionShot(context.Context, CommerceVideoBatchInput, string) (CommerceVideoExecutionShot, error)
	BeginCommerceVideoItem(context.Context, CommerceVideoBatchInput, string) (CommerceReferenceImageItemAttempt, error)
	CommitCommerceVideoPromptPlan(context.Context, CommitCommerceVideoPromptPlanInput) (CommerceVideoPromptPlanState, error)
	CompleteCommerceShotVideoItem(context.Context, CompleteCommerceShotVideoItemInput) (CommerceVideoItemOutput, error)
	FailCommerceVideoItem(context.Context, FailCommerceVideoItemInput) error
	FinalizeCommerceVideoBatch(context.Context, CommerceVideoBatchInput, CommerceVideoBatchOutput) (CommerceVideoBatchOutput, error)
	FinalizeCommerceVideoFailure(context.Context, FinalizeCommerceVideoFailureInput) error
}

func RegisterCommerceVideoWorkflows(registrar CommerceWorkflowRegistrar) {
	registrar.RegisterWorkflowWithOptions(CommerceVideoPromptBatchWorkflow, workflow.RegisterOptions{Name: CommerceVideoPromptBatchWorkflowName})
	registrar.RegisterWorkflowWithOptions(CommerceShotVideoBatchWorkflow, workflow.RegisterOptions{Name: CommerceShotVideoBatchWorkflowName})
	registrar.RegisterWorkflowWithOptions(CommerceShotVideoWorkflow, workflow.RegisterOptions{Name: CommerceShotVideoWorkflowName})
}

func RegisterCommerceVideoActivities(registrar CommerceActivityRegistrar, activities CommerceActivities) {
	registrar.RegisterActivityWithOptions(
		func(ctx context.Context, input CommerceVideoBatchInput, shotID string) (CommerceVideoItemOutput, error) {
			return activities.GenerateCommerceVideoPromptItem(ctx, input, shotID), nil
		},
		activity.RegisterOptions{Name: GenerateCommerceVideoPromptItemActivityName},
	)
	registrar.RegisterActivityWithOptions(activities.LoadCommerceVideoExecutionShot, activity.RegisterOptions{Name: LoadCommerceVideoExecutionShotActivityName})
	registrar.RegisterActivityWithOptions(activities.BeginCommerceVideoItem, activity.RegisterOptions{Name: BeginCommerceVideoItemActivityName})
	registrar.RegisterActivityWithOptions(activities.CompleteCommerceShotVideoItem, activity.RegisterOptions{Name: CompleteCommerceShotVideoItemActivityName})
	registrar.RegisterActivityWithOptions(activities.FailCommerceVideoItem, activity.RegisterOptions{Name: FailCommerceVideoItemActivityName})
	registrar.RegisterActivityWithOptions(activities.FinalizeCommerceVideoBatch, activity.RegisterOptions{Name: FinalizeCommerceVideoBatchActivityName})
	registrar.RegisterActivityWithOptions(activities.FinalizeCommerceVideoFailure, activity.RegisterOptions{Name: FinalizeCommerceVideoFailureActivityName})
}

func CommerceVideoPromptBatchWorkflow(ctx workflow.Context, input CommerceVideoBatchInput) (CommerceVideoBatchOutput, error) {
	return commerceVideoBatchWorkflow(ctx, input, GenerateCommerceVideoPromptItemActivityName, false)
}

func CommerceShotVideoBatchWorkflow(ctx workflow.Context, input CommerceVideoBatchInput) (CommerceVideoBatchOutput, error) {
	return commerceVideoBatchWorkflow(ctx, input, CommerceShotVideoWorkflowName, true)
}

func commerceVideoBatchWorkflow(
	ctx workflow.Context,
	input CommerceVideoBatchInput,
	executableName string,
	child bool,
) (result CommerceVideoBatchOutput, resultErr error) {
	if err := validateCommerceVideoBatchInput(input); err != nil {
		return CommerceVideoBatchOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	completionPersisted := false
	defer finalizeFailedCommerceVideoBatch(ctx, input, &result, &resultErr, &completionPersisted)
	concurrency := normalizedCommerceVideoConcurrency(input.Concurrency)
	result = CommerceVideoBatchOutput{
		Identity: input.Identity, ProductionRunID: input.ProductionRunID,
		Operation: input.Operation, Total: len(input.ShotIDs),
		Items: make([]CommerceVideoItemOutput, 0, len(input.ShotIDs)),
	}
	activityOptions := defaultActivityOptions()
	if !child && executableName == GenerateCommerceVideoPromptItemActivityName {
		activityOptions = commerceVideoPromptItemActivityOptions()
	}
	if activityOptions.RetryPolicy == nil {
		activityOptions.RetryPolicy = &temporal.RetryPolicy{}
	}
	activityOptions.RetryPolicy.MaximumAttempts = 1
	activityCtx := workflow.WithActivityOptions(ctx, activityOptions)
	for offset := 0; offset < len(input.ShotIDs); offset += concurrency {
		end := offset + concurrency
		if end > len(input.ShotIDs) {
			end = len(input.ShotIDs)
		}
		futures := make([]workflow.Future, 0, end-offset)
		for _, shotID := range input.ShotIDs[offset:end] {
			if child {
				childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
					WorkflowID:  fmt.Sprintf("commerce-shot-video-%s-%s", input.ProductionRunID, shotID),
					RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
				})
				futures = append(futures, workflow.ExecuteChildWorkflow(childCtx, executableName, input, shotID))
			} else {
				futures = append(futures, workflow.ExecuteActivity(activityCtx, executableName, input, shotID))
			}
		}
		for index, future := range futures {
			var item CommerceVideoItemOutput
			if err := future.Get(ctx, &item); err != nil {
				item = CommerceVideoItemOutput{
					ShotID: input.ShotIDs[offset+index], Status: commerce.ItemFailedRetryable,
					ErrorCode: "COMMERCE_VIDEO_ACTIVITY_FAILED", ErrorMessage: err.Error(), Retryable: true,
				}
			}
			result.Items = append(result.Items, item)
			if item.Status == commerce.ItemSucceeded {
				result.Succeeded++
			} else {
				result.Failed++
			}
		}
	}
	result.Status = aggregateCommerceVideoBatchStatus(result)
	finalCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var finalized CommerceVideoBatchOutput
	if err := workflow.ExecuteActivity(finalCtx, FinalizeCommerceVideoBatchActivityName, input, result).Get(finalCtx, &finalized); err != nil {
		return result, err
	}
	completionPersisted = true
	return finalized, nil
}

func CommerceShotVideoWorkflow(ctx workflow.Context, input CommerceVideoBatchInput, shotID string) (result CommerceVideoItemOutput, resultErr error) {
	if err := validateCommerceVideoBatchInput(input); err != nil || input.Operation != "generate_videos" || !commerceStringSliceContains(input.ShotIDs, shotID) {
		if err == nil {
			err = errors.New("shot video child workflow identity is invalid")
		}
		return CommerceVideoItemOutput{}, commerceWorkflowError(CommerceCodeWorkflowInputInvalid, err)
	}
	activityCtx := workflow.WithActivityOptions(ctx, defaultActivityOptions())
	var attempt CommerceReferenceImageItemAttempt
	if err := workflow.ExecuteActivity(activityCtx, BeginCommerceVideoItemActivityName, input, shotID).Get(activityCtx, &attempt); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		code, message := workflowExecutionError(resultErr)
		retryable := !isCommerceTerminalVideoError(code)
		disconnected, _ := workflow.NewDisconnectedContext(ctx)
		disconnected = workflow.WithActivityOptions(disconnected, defaultActivityOptions())
		if err := workflow.ExecuteActivity(disconnected, FailCommerceVideoItemActivityName, FailCommerceVideoItemInput{
			WorkflowInput: input, Attempt: attempt, ShotID: shotID,
			ErrorCode: code, ErrorMessage: message, Retryable: retryable,
		}).Get(disconnected, nil); err != nil {
			workflow.GetLogger(ctx).Error("failed to finalize commerce shot video item", "shotId", shotID, "error", err)
		}
	}()
	var shot CommerceVideoExecutionShot
	if err := workflow.ExecuteActivity(activityCtx, LoadCommerceVideoExecutionShotActivityName, input, shotID).Get(activityCtx, &shot); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	createOptions := defaultActivityOptions()
	if createOptions.RetryPolicy != nil {
		// Provider task creation is externally visible. Let the durable item retry
		// path decide whether to create a new task instead of replaying it here.
		createOptions.RetryPolicy.MaximumAttempts = 1
	}
	createCtx := workflow.WithActivityOptions(ctx, createOptions)
	renderResult, err := executePreparedShotRenderPlan(activityCtx, createCtx, ShotRenderExecutionInput{
		OrganizationID: input.Identity.OrganizationID, ProjectID: input.Identity.ProjectID,
		WorkflowRunID: input.WorkflowRunID, CreatedBy: input.CreatedBy,
		ShotID: shot.ShotID, ShotIndex: shot.ShotIndex, ShotNo: shot.ShotNo,
		WorkflowPrompt: "commerce_approved_video_prompt", FailureScope: workflowFailureScopeBatchItem,
		AspectRatio: shot.AspectRatio, Resolution: shot.Resolution,
		AudioStrategy: shot.AudioStrategy, AudioRequirement: shot.AudioRequirement,
		Force: input.Force, MaxPolls: 240, PollInterval: 5 * time.Second,
	})
	if err != nil {
		return CommerceVideoItemOutput{}, err
	}
	if err := workflow.ExecuteActivity(activityCtx, CompleteCommerceShotVideoItemActivityName, CompleteCommerceShotVideoItemInput{
		WorkflowInput: input, Attempt: attempt, Shot: shot, Result: renderResult,
	}).Get(activityCtx, &result); err != nil {
		return CommerceVideoItemOutput{}, err
	}
	return result, nil
}

func (a CommerceActivities) GenerateCommerceVideoPromptItem(ctx context.Context, input CommerceVideoBatchInput, shotID string) CommerceVideoItemOutput {
	port, ok := a.videoPort()
	if !ok {
		return commerceVideoFailureOutput(shotID, commerceActivityPortError())
	}
	attempt, err := port.BeginCommerceVideoItem(ctx, input, shotID)
	if err != nil {
		return commerceVideoFailureOutput(shotID, err)
	}
	snapshot, err := port.LoadCommerceVideoPromptShot(ctx, input, shotID)
	if err != nil {
		return a.failCommerceVideoItem(ctx, input, attempt, shotID, err)
	}
	if err := validateCommerceVideoSnapshot(snapshot); err != nil {
		return a.failCommerceVideoItem(ctx, input, attempt, shotID, err)
	}
	references := []provider.GatewayImageReference{{
		Type: "image", ArtifactID: snapshot.FirstFrame.ArtifactID, StorageKey: snapshot.FirstFrame.StorageKey,
	}}
	maxRounds := commerceVideoReviewRoundLimit(snapshot.Bindings)
	issues := []CommerceReviewIssue(nil)
	for round := 1; round <= maxRounds; round++ {
		generated, err := a.runCommerceTextAgent(ctx, CommerceAgentCallInput{
			GenerationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID,
			AttemptGeneration: input.AttemptGeneration, Phase: CommercePhaseVideoPrompt,
			SubjectKey: shotID, Round: round, Binding: snapshot.Bindings.VideoPromptAgent,
			InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.InstructionLanguage,
			Context:        mustJSON(map[string]any{"snapshot": snapshot, "reviewerIssues": issues}),
			ReviewerIssues: issues, References: references,
		})
		if err != nil {
			return a.failCommerceVideoItem(ctx, input, attempt, shotID, err)
		}
		contract, parseErr := ParseCommerceVideoPromptPlan(generated.RawOutput)
		if parseErr == nil {
			parseErr = ValidateCommerceVideoPromptPlan(contract, snapshot)
		}
		if parseErr != nil {
			issues = []CommerceReviewIssue{{
				Code: "DETERMINISTIC_CONTRACT_INVALID", Field: "videoPromptPlan",
				Message: parseErr.Error(), Suggestion: "严格按冻结镜头、逐字旁白、首帧和模型能力契约修正",
			}}
			if round == maxRounds {
				return a.failCommerceVideoItem(ctx, input, attempt, shotID, temporal.NewNonRetryableApplicationError(parseErr.Error(), CommerceCodeVideoPromptReviewExhausted, parseErr))
			}
			continue
		}
		reviewed, err := a.runCommerceTextAgent(ctx, CommerceAgentCallInput{
			GenerationIdentity: &input.Identity, WorkflowRunID: input.WorkflowRunID,
			AttemptGeneration: input.AttemptGeneration, Phase: CommercePhaseVideoPrompt,
			SubjectKey: shotID, Round: round, Binding: snapshot.Bindings.VideoPromptReviewer,
			InputLanguage: snapshot.TargetLocale, OutputLanguage: snapshot.InstructionLanguage,
			Context:    mustJSON(map[string]any{"snapshot": snapshot, "draft": contract}),
			References: references,
		})
		if err != nil {
			return a.failCommerceVideoItem(ctx, input, attempt, shotID, err)
		}
		review, err := ParseCommerceVideoPromptReview(reviewed.RawOutput)
		if err == nil {
			err = ValidateCommerceVideoPromptReview(review)
		}
		if err != nil {
			return a.failCommerceVideoItem(ctx, input, attempt, shotID, temporal.NewNonRetryableApplicationError(err.Error(), CommerceCodeVideoPromptContractInvalid, err))
		}
		switch review.Decision {
		case "approve":
			plan, err := port.CommitCommerceVideoPromptPlan(ctx, CommitCommerceVideoPromptPlanInput{
				WorkflowInput: input, Attempt: attempt, Snapshot: snapshot,
				Contract: contract, Review: review, Generation: generated.Provenance, Reviewer: reviewed.Provenance,
			})
			if err != nil {
				return a.failCommerceVideoItem(ctx, input, attempt, shotID, err)
			}
			return CommerceVideoItemOutput{ShotID: shotID, Status: commerce.ItemSucceeded, PromptPlan: &plan}
		case "reject":
			return a.failCommerceVideoItem(ctx, input, attempt, shotID, temporal.NewNonRetryableApplicationError("视频提示词审核拒绝", CommerceCodeVideoPromptContractInvalid, nil))
		case "revise":
			issues = append([]CommerceReviewIssue(nil), review.Issues...)
			if round == maxRounds {
				return a.failCommerceVideoItem(ctx, input, attempt, shotID, temporal.NewNonRetryableApplicationError("视频提示词审核在 3 轮内未通过", CommerceCodeVideoPromptReviewExhausted, nil))
			}
		}
	}
	return a.failCommerceVideoItem(ctx, input, attempt, shotID, temporal.NewNonRetryableApplicationError("视频提示词审核未产生终态", CommerceCodeVideoPromptReviewExhausted, nil))
}

func commerceVideoReviewRoundLimit(bindings CommerceVideoPromptAgentBindings) int {
	normalize := func(value int) int {
		if value < 1 || value > CommerceMaxAgentReviewRounds {
			return CommerceMaxAgentReviewRounds
		}
		return value
	}
	generationRounds := normalize(bindings.VideoPromptAgent.MaxReviewRounds)
	reviewerRounds := normalize(bindings.VideoPromptReviewer.MaxReviewRounds)
	if reviewerRounds < generationRounds {
		return reviewerRounds
	}
	return generationRounds
}

func (a CommerceActivities) LoadCommerceVideoExecutionShot(ctx context.Context, input CommerceVideoBatchInput, shotID string) (CommerceVideoExecutionShot, error) {
	port, ok := a.videoPort()
	if !ok {
		return CommerceVideoExecutionShot{}, commerceActivityPortError()
	}
	return port.LoadCommerceVideoExecutionShot(ctx, input, shotID)
}

func (a CommerceActivities) BeginCommerceVideoItem(ctx context.Context, input CommerceVideoBatchInput, shotID string) (CommerceReferenceImageItemAttempt, error) {
	port, ok := a.videoPort()
	if !ok {
		return CommerceReferenceImageItemAttempt{}, commerceActivityPortError()
	}
	return port.BeginCommerceVideoItem(ctx, input, shotID)
}

func (a CommerceActivities) CompleteCommerceShotVideoItem(ctx context.Context, input CompleteCommerceShotVideoItemInput) (CommerceVideoItemOutput, error) {
	port, ok := a.videoPort()
	if !ok {
		return CommerceVideoItemOutput{}, commerceActivityPortError()
	}
	return port.CompleteCommerceShotVideoItem(ctx, input)
}

func (a CommerceActivities) FailCommerceVideoItem(ctx context.Context, input FailCommerceVideoItemInput) error {
	port, ok := a.videoPort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.FailCommerceVideoItem(ctx, input)
}

func (a CommerceActivities) FinalizeCommerceVideoBatch(ctx context.Context, input CommerceVideoBatchInput, output CommerceVideoBatchOutput) (CommerceVideoBatchOutput, error) {
	port, ok := a.videoPort()
	if !ok {
		return output, commerceActivityPortError()
	}
	return port.FinalizeCommerceVideoBatch(ctx, input, output)
}

func (a CommerceActivities) FinalizeCommerceVideoFailure(ctx context.Context, input FinalizeCommerceVideoFailureInput) error {
	port, ok := a.videoPort()
	if !ok {
		return commerceActivityPortError()
	}
	return port.FinalizeCommerceVideoFailure(ctx, input)
}

func (a CommerceActivities) videoPort() (CommerceVideoPort, bool) {
	port, ok := a.Ports.(CommerceVideoPort)
	return port, ok && port != nil
}

func (a CommerceActivities) failCommerceVideoItem(ctx context.Context, input CommerceVideoBatchInput, attempt CommerceReferenceImageItemAttempt, shotID string, cause error) CommerceVideoItemOutput {
	code, message := workflowErrorFields(cause, "COMMERCE_VIDEO_ITEM_FAILED")
	retryable := provider.VideoSegmentFailureRetryable(code)
	var applicationErr *temporal.ApplicationError
	if errors.As(cause, &applicationErr) && applicationErr.NonRetryable() {
		retryable = false
	}
	port, ok := a.videoPort()
	if ok {
		_ = port.FailCommerceVideoItem(ctx, FailCommerceVideoItemInput{
			WorkflowInput: input, Attempt: attempt, ShotID: shotID,
			ErrorCode: code, ErrorMessage: message, Retryable: retryable,
		})
	}
	return CommerceVideoItemOutput{
		ShotID: shotID, Status: commerceVideoFailureStatus(retryable),
		ErrorCode: code, ErrorMessage: message, Retryable: retryable,
	}
}

func finalizeFailedCommerceVideoBatch(ctx workflow.Context, input CommerceVideoBatchInput, output *CommerceVideoBatchOutput, resultErr *error, persisted *bool) {
	if output == nil || resultErr == nil || *resultErr == nil || (persisted != nil && *persisted) {
		return
	}
	code, message := workflowExecutionError(*resultErr)
	cancelled := temporal.IsCanceledError(*resultErr)
	if cancelled {
		code, message = "WORKFLOW_CANCELLED", "用户取消视频生产批次"
	}
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	disconnected = workflow.WithActivityOptions(disconnected, defaultActivityOptions())
	if err := workflow.ExecuteActivity(disconnected, FinalizeCommerceVideoFailureActivityName, FinalizeCommerceVideoFailureInput{
		WorkflowInput: input, Output: *output, Cancelled: cancelled,
		ErrorCode: code, ErrorMessage: message,
	}).Get(disconnected, nil); err != nil {
		workflow.GetLogger(ctx).Error("failed to finalize commerce video workflow", "error", err)
	}
}

func validateCommerceVideoBatchInput(input CommerceVideoBatchInput) error {
	if err := ValidateCommerceUnitGenerationIdentity(input.Identity); err != nil {
		return err
	}
	if strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ProductionRunID) == "" ||
		strings.TrimSpace(input.StoryboardPlanID) == "" || input.PlanEditRevision < 1 || len(input.ShotIDs) == 0 {
		return errors.New("commerce video batch identity is incomplete")
	}
	if _, err := commerceVideoPhase(input.Operation); err != nil {
		return err
	}
	if input.Concurrency < 0 || input.Concurrency > 16 {
		return errors.New("commerce video concurrency must be between 1 and 16")
	}
	return nil
}

func normalizedCommerceVideoConcurrency(value int) int {
	if value < 1 {
		return 5
	}
	if value > 16 {
		return 16
	}
	return value
}

func aggregateCommerceVideoBatchStatus(output CommerceVideoBatchOutput) commerce.ProductionRunStatus {
	switch {
	case output.Total > 0 && output.Succeeded == output.Total:
		return commerce.RunSucceeded
	case output.Succeeded > 0:
		return commerce.RunPartiallySucceeded
	default:
		return commerce.RunFailed
	}
}

func commerceVideoFailureStatus(retryable bool) commerce.ProductionItemStatus {
	if retryable {
		return commerce.ItemFailedRetryable
	}
	return commerce.ItemFailedTerminal
}

func commerceVideoFailureOutput(shotID string, err error) CommerceVideoItemOutput {
	code, message := workflowErrorFields(err, "COMMERCE_VIDEO_ITEM_FAILED")
	return CommerceVideoItemOutput{ShotID: shotID, Status: commerce.ItemFailedRetryable, ErrorCode: code, ErrorMessage: message, Retryable: true}
}

func commerceStringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func isCommerceTerminalVideoError(code string) bool {
	switch strings.TrimSpace(code) {
	case CommerceCodeVideoPromptContractInvalid, CommerceCodeVideoPromptReviewExhausted,
		CommerceCodeGenerationMismatch, provider.CodeRenderPlanReplanRequired,
		provider.CodeModelInputContractUnsupported, provider.CodeModelCapabilityUnavailable:
		return true
	default:
		return false
	}
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
