package workflows

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videocontracts"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ShotRenderExecutionInput struct {
	OrganizationID            string
	ProjectID                 string
	WorkflowRunID             string
	OperationID               string
	OperationItemID           string
	OperationAttempt          int
	CreatedBy                 string
	ShotID                    string
	ShotIndex                 int
	ShotNo                    int
	WorkflowPrompt            string
	FailureScope              string
	AspectRatio               string
	Resolution                string
	ExpectedRequestedDuration float64
	AudioStrategy             string
	AudioRequirement          string
	Force                     bool
	MaxPolls                  int
	PollInterval              time.Duration
}

type ShotRenderExecutionResult struct {
	Plan        PlanShotVideoOutput
	Creates     []CreateShotVideoTaskOutput
	Polls       []PollShotVideoTaskOutput
	Media       []ProcessRenderSegmentMediaOutput
	Output      ComposeShotRenderPlanMediaOutput
	Segments    []PollShotVideoTaskOutput
	LastSegment PollShotVideoTaskOutput
}

const maxVideoRenderSegmentAttempts = 3

func executeShotRenderPlan(ctx, createCtx workflow.Context, input ShotRenderExecutionInput) (result ShotRenderExecutionResult, err error) {
	return executePreparedShotRenderPlan(ctx, createCtx, input)
}

func executePreparedShotRenderPlan(ctx, createCtx workflow.Context, input ShotRenderExecutionInput) (result ShotRenderExecutionResult, err error) {
	if input.MaxPolls <= 0 {
		input.MaxPolls = 120
	}
	if input.PollInterval <= 0 {
		input.PollInterval = 5 * time.Second
	}
	if strings.TrimSpace(input.AudioStrategy) == "" {
		input.AudioStrategy = "native_av"
	}
	if strings.TrimSpace(input.AudioRequirement) == "" {
		input.AudioRequirement = "preferred"
	}
	var prepared LoadPreparedShotVideoPlanOutput
	prepareInput := EnsurePreparedShotVideoPlanInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope,
		ShotID: input.ShotID, ShotIndex: input.ShotIndex,
		AspectRatio: input.AspectRatio, Resolution: input.Resolution, AudioStrategy: input.AudioStrategy,
		AudioRequirement: input.AudioRequirement, Force: input.Force,
	}
	var preparePlan workflow.Future
	if strings.TrimSpace(input.OperationItemID) != "" {
		preparePlan = workflow.ExecuteActivity(ctx, "MaterializeAndBindExecutableShotVideoPlanV2", EnsurePreparedShotVideoPlanV2Input{
			EnsurePreparedShotVideoPlanInput: prepareInput,
			OperationID:                      input.OperationID, OperationItemID: input.OperationItemID,
			OperationItemAttempt: input.OperationAttempt,
		})
	} else {
		preparePlan = workflow.ExecuteActivity(ctx, "EnsurePreparedShotVideoPlan", prepareInput)
	}
	if err := preparePlan.Get(ctx, &prepared); err != nil {
		return ShotRenderExecutionResult{}, err
	}
	if len(prepared.Segments) == 0 {
		return ShotRenderExecutionResult{}, temporal.NewApplicationError("视频执行计划没有已审核片段提示词，请重新生成视频提示词", provider.CodeRenderPlanReplanRequired)
	}
	if err := validateExpectedShotRenderDuration(input.ExpectedRequestedDuration, prepared.Segments); err != nil {
		return ShotRenderExecutionResult{}, err
	}
	result.Plan = prepared.Plan
	var current CreateShotVideoTaskOutput
	defer func() {
		if (ctx.Err() == nil && err == nil) || current.ProviderAsyncTaskID == "" || current.NodeRunID == "" {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		reason := "Video render workflow exited before the provider task reached a terminal state"
		if ctx.Err() != nil {
			reason = "Video render workflow cancellation requested"
		}
		_, _ = cancelShotVideoAttempt(cleanupCtx, input, current, reason)
	}()
	planSegments := make([]PollShotVideoTaskOutput, 0, len(prepared.Segments))
	var previous PollShotVideoTaskOutput
	for _, segment := range prepared.Segments {
		var previousTailFrame *ShotSegmentTailAnchorReference
		if segment.SegmentIndex > 0 && videocontracts.RequiresPreviousTailFrame(segment.InputContractKey) {
			frame, frameErr := extractPreviousRenderSegmentTail(ctx, input, previous)
			if frameErr != nil {
				return ShotRenderExecutionResult{}, frameErr
			}
			previousTailFrame = frame
		}
		segmentSucceeded := false
		retryGeneration := 0
		lastFailureCode := provider.CodeUpstreamInternalError
		lastFailureMessage := "视频片段生成失败"
		for segmentAttempt := 0; segmentAttempt < maxVideoRenderSegmentAttempts; segmentAttempt++ {
			created := CreateShotVideoTaskOutput{}
			createErr := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", CreateShotVideoTaskInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
				OperationID: input.OperationID, OperationItemID: input.OperationItemID, OperationItemAttempt: input.OperationAttempt,
				CreatedBy: input.CreatedBy, ShotID: input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
				WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope, Duration: segment.RequestedDurationSeconds,
				PlannedDuration: segment.PlannedDurationSeconds,
				AspectRatio:     input.AspectRatio, Resolution: input.Resolution, Force: input.Force,
				ExecutionPlanID: prepared.Plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
				CapabilitySnapshotHash: prepared.Plan.CapabilitySnapshotHash,
				InputContractKey:       segment.InputContractKey, InputContractHash: segment.InputContractHash,
				SegmentIndex: segment.SegmentIndex,
				SegmentCount: len(prepared.Segments), RetryGeneration: retryGeneration, ContinuityMode: segment.ContinuityMode,
				SegmentStartTick: segment.PlannedStartTick, SegmentEndTick: segment.PlannedEndTick,
				DialogueLines: renderSegmentDialogueLines(segment.DialogueSpans),
				AudioStrategy: input.AudioStrategy, AudioRequirement: input.AudioRequirement,
				PreviousSegmentArtifactID: previous.ArtifactID, PreviousSegmentMediaFileID: previous.MediaFileID,
				PreviousSegmentRenderSegmentID: previous.RenderSegmentID,
				PreviousSegmentStorageKey:      previous.StorageKey,
				PreviousSegmentTailFrame:       previousTailFrame,
				Prompt:                         segment.Prompt, NegativePrompt: segment.NegativePrompt, PromptHash: segment.PromptHash,
				GenerationProviderCallID: segment.GenerationProviderCallID, ReviewProviderCallID: segment.ReviewProviderCallID,
				ReviewTemplateKey: segment.ReviewTemplateKey, ReviewPromptVersionID: segment.ReviewPromptVersionID,
			}).Get(createCtx, &created)
			if createErr != nil {
				return ShotRenderExecutionResult{}, createErr
			}
			result.Creates = append(result.Creates, created)
			if isTerminalVideoAttemptFailure(created.Status) || strings.TrimSpace(created.ErrorCode) != "" {
				lastFailureCode = firstNonEmptyString(created.ErrorCode, provider.CodeUpstreamInternalError)
				lastFailureMessage = firstNonEmptyString(created.ErrorMessage, "视频片段创建失败")
				if !provider.VideoSegmentFailureRetryable(lastFailureCode) {
					return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
				}
				if segmentAttempt+1 >= maxVideoRenderSegmentAttempts {
					return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
				}
				decision, retryErr := prepareVideoSegmentRetry(ctx, input, prepared.Plan.ExecutionPlanID, segment.SegmentID, retryGeneration, lastFailureCode, lastFailureMessage)
				if retryErr != nil {
					return ShotRenderExecutionResult{}, retryErr
				}
				if decision.Replan {
					return ShotRenderExecutionResult{}, preparedVideoPromptError("视频模型执行计划已失效，请重新批量生成视频提示词后再生成视频")
				}
				if decision.Exhausted {
					return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
				}
				retryGeneration = decision.RetryGeneration
				if err := waitForVideoSegmentRetry(ctx, lastFailureCode, retryGeneration); err != nil {
					return ShotRenderExecutionResult{}, err
				}
				continue
			}

			current = created
			pollFailed := false
			for pollCount := 1; pollCount <= input.MaxPolls; pollCount++ {
				var poll PollShotVideoTaskOutput
				if err := workflow.ExecuteActivity(ctx, "PollShotVideoTask", PollShotVideoTaskInput{
					OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
					FailureScope: input.FailureScope,
					ShotID:       input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
					NodeRunID: created.NodeRunID, ExecutionToken: created.ExecutionToken,
					AttemptGeneration: created.AttemptGeneration, ProviderAsyncTaskID: created.ProviderAsyncTaskID,
					ExternalTaskID: created.ExternalTaskID, PollCount: pollCount,
					ExecutionPlanID: prepared.Plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
					SegmentIndex: segment.SegmentIndex, SegmentCount: len(prepared.Segments),
				}).Get(ctx, &poll); err != nil {
					return ShotRenderExecutionResult{}, err
				}
				result.Polls = append(result.Polls, poll)
				if poll.Status == "succeeded" {
					current = CreateShotVideoTaskOutput{}
					processed, processErr := processVideoRenderSegment(ctx, input, prepared.Plan, segment.GatewayVideoPlanSegment, poll)
					if processErr != nil {
						return ShotRenderExecutionResult{}, processErr
					}
					poll.MezzanineArtifactID = processed.MezzanineArtifactID
					poll.MezzanineMediaFileID = processed.MezzanineMediaFileID
					poll.MezzanineStorageKey = processed.MezzanineStorageKey
					poll.ExtractedAudioArtifactID = processed.ExtractedAudioArtifactID
					poll.ExtractedAudioMediaFileID = processed.ExtractedAudioMediaFileID
					poll.ExtractedAudioStorageKey = processed.ExtractedAudioStorageKey
					result.Media = append(result.Media, processed)
					previous = poll
					planSegments = append(planSegments, poll)
					segmentSucceeded = true
					break
				}
				if isTerminalVideoAttemptFailure(poll.Status) {
					current = CreateShotVideoTaskOutput{}
					lastFailureCode = firstNonEmptyString(poll.ErrorCode, provider.CodeUpstreamInternalError)
					lastFailureMessage = firstNonEmptyString(poll.ErrorMessage, "视频片段生成失败")
					if !provider.VideoSegmentFailureRetryable(lastFailureCode) {
						return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
					}
					if segmentAttempt+1 >= maxVideoRenderSegmentAttempts {
						return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
					}
					decision, retryErr := prepareVideoSegmentRetry(ctx, input, prepared.Plan.ExecutionPlanID, segment.SegmentID, retryGeneration, lastFailureCode, lastFailureMessage)
					if retryErr != nil {
						return ShotRenderExecutionResult{}, retryErr
					}
					if decision.Replan {
						return ShotRenderExecutionResult{}, preparedVideoPromptError("视频模型执行计划已失效，请重新批量生成视频提示词后再生成视频")
					}
					if decision.Exhausted {
						return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
					}
					retryGeneration = decision.RetryGeneration
					if err := waitForVideoSegmentRetry(ctx, lastFailureCode, retryGeneration); err != nil {
						return ShotRenderExecutionResult{}, err
					}
					pollFailed = true
					break
				}
				if pollCount < input.MaxPolls {
					if err := workflow.Sleep(ctx, input.PollInterval); err != nil {
						return ShotRenderExecutionResult{}, err
					}
				}
			}
			if segmentSucceeded {
				break
			}
			if pollFailed {
				continue
			}
			lastFailureCode = codeProviderVideoPollingTimeout
			lastFailureMessage = fmt.Sprintf("视频片段 %d 轮询超时", segment.SegmentIndex+1)
			cancelled, cancelErr := cancelShotVideoAttempt(ctx, input, current, "Video render polling budget exhausted before retry")
			if cancelErr != nil {
				return ShotRenderExecutionResult{}, temporal.NewNonRetryableApplicationError("视频任务轮询超时，但终结当前供应商任务失败", provider.CodeProviderCancelFailed, cancelErr)
			}
			if strings.TrimSpace(cancelled.Status) != "cancelled" {
				message := firstNonEmptyString(cancelled.ErrorMessage, "供应商任务未能进入已取消状态")
				return ShotRenderExecutionResult{}, temporal.NewNonRetryableApplicationError("视频任务轮询超时，但终结当前供应商任务失败："+message, provider.CodeProviderCancelFailed, nil)
			}
			current = CreateShotVideoTaskOutput{}
			if segmentAttempt+1 >= maxVideoRenderSegmentAttempts {
				return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
			}
			decision, retryErr := prepareVideoSegmentRetry(ctx, input, prepared.Plan.ExecutionPlanID, segment.SegmentID, retryGeneration, lastFailureCode, lastFailureMessage)
			if retryErr != nil {
				return ShotRenderExecutionResult{}, retryErr
			}
			if decision.Replan {
				return ShotRenderExecutionResult{}, preparedVideoPromptError("视频模型执行计划已失效，请重新批量生成视频提示词后再生成视频")
			}
			if decision.Exhausted {
				return ShotRenderExecutionResult{}, terminalVideoSegmentFailure(lastFailureCode, lastFailureMessage)
			}
			retryGeneration = decision.RetryGeneration
			if err := waitForVideoSegmentRetry(ctx, lastFailureCode, retryGeneration); err != nil {
				return ShotRenderExecutionResult{}, err
			}
		}
		if !segmentSucceeded {
			return ShotRenderExecutionResult{}, temporal.NewApplicationError(lastFailureMessage, lastFailureCode)
		}
	}
	result.Segments = planSegments
	if len(planSegments) > 0 {
		result.LastSegment = planSegments[len(planSegments)-1]
	}
	composed, composeErr := composeShotRenderPlan(ctx, input, prepared.Plan)
	if composeErr != nil {
		return ShotRenderExecutionResult{}, composeErr
	}
	result.Output = composed
	return result, nil
}

func validateExpectedShotRenderDuration(expected float64, segments []PreparedShotVideoSegment) error {
	if expected <= 0 {
		return nil
	}
	if len(segments) != 1 || math.Abs(segments[0].RequestedDurationSeconds-expected) > 0.001 {
		return temporal.NewNonRetryableApplicationError(
			"视频执行计划与分镜冻结的模型请求时长不一致，请重新生成分镜方案",
			provider.CodeRenderPlanReplanRequired,
			nil,
		)
	}
	return nil
}

func waitForVideoSegmentRetry(ctx workflow.Context, code string, retryGeneration int) error {
	base := 10 * time.Second
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case provider.CodeProviderRateLimited, provider.CodeProviderConcurrencyLimited, provider.CodeProviderCircuitOpen:
		base = 30 * time.Second
	case provider.CodeUpstreamTimeout, provider.CodePollingTimeout, codeProviderVideoPollingTimeout:
		base = 15 * time.Second
	}
	multiplier := retryGeneration
	if multiplier < 1 {
		multiplier = 1
	}
	if multiplier > 2 {
		multiplier = 2
	}
	return workflow.Sleep(ctx, base*time.Duration(multiplier))
}

func cancelShotVideoAttempt(ctx workflow.Context, input ShotRenderExecutionInput, current CreateShotVideoTaskOutput, reason string) (CancelShotVideoTaskOutput, error) {
	var output CancelShotVideoTaskOutput
	err := workflow.ExecuteActivity(ctx, "CancelShotVideoTask", CancelShotVideoTaskInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		ShotID: input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
		NodeRunID: current.NodeRunID, ProviderAsyncTaskID: current.ProviderAsyncTaskID, ExternalTaskID: current.ExternalTaskID,
		ExecutionPlanID: current.ExecutionPlanID, RenderSegmentID: current.RenderSegmentID,
		SegmentIndex: current.SegmentIndex, SegmentCount: current.SegmentCount,
		Reason: reason,
	}).Get(ctx, &output)
	return output, err
}

func extractPreviousRenderSegmentTail(
	ctx workflow.Context,
	input ShotRenderExecutionInput,
	previous PollShotVideoTaskOutput,
) (*ShotSegmentTailAnchorReference, error) {
	if strings.TrimSpace(previous.ArtifactID) == "" || strings.TrimSpace(previous.StorageKey) == "" || strings.TrimSpace(previous.RenderSegmentID) == "" {
		return nil, temporal.NewApplicationError("首帧续接缺少已完成的前一视频片段", provider.CodeModelInputContractUnsupported)
	}
	frame, err := extractRenderSegmentTailAnchor(ctx, ExtractRenderSegmentTailAnchorInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, ShotID: input.ShotID,
		SourceVideoArtifactID: previous.ArtifactID, SourceVideoMediaFileID: previous.MediaFileID,
		SourceVideoStorageKey: previous.StorageKey, SourceRenderSegmentID: previous.RenderSegmentID,
	})
	if err != nil {
		return nil, err
	}
	return &ShotSegmentTailAnchorReference{
		AnchorID:     frame.AnchorID,
		SourceShotID: frame.SourceShotID, SourceRenderSegmentID: frame.SourceRenderSegmentID,
		SourceVideoArtifactID: frame.SourceVideoArtifactID, ArtifactID: frame.ArtifactID,
		MediaFileID: frame.MediaFileID, StorageKey: frame.StorageKey,
		ContentHash: frame.ContentHash, GeneratedAt: frame.GeneratedAt,
	}, nil
}

func extractRenderSegmentTailAnchor(ctx workflow.Context, input ExtractRenderSegmentTailAnchorInput) (ExtractRenderSegmentTailAnchorOutput, error) {
	options := defaultActivityOptions()
	options.TaskQueue = MediaTaskQueue
	options.StartToCloseTimeout = 30 * time.Minute
	mediaCtx := workflow.WithActivityOptions(ctx, options)
	var output ExtractRenderSegmentTailAnchorOutput
	err := workflow.ExecuteActivity(mediaCtx, "ExtractRenderSegmentTailAnchor", input).Get(mediaCtx, &output)
	return output, err
}

func composeShotRenderPlan(ctx workflow.Context, input ShotRenderExecutionInput, plan PlanShotVideoOutput) (ComposeShotRenderPlanMediaOutput, error) {
	options := mediaProcessingActivityOptions()
	mediaCtx := workflow.WithActivityOptions(ctx, options)
	var output ComposeShotRenderPlanMediaOutput
	err := workflow.ExecuteActivity(mediaCtx, "ComposeShotRenderPlanMedia", ComposeShotRenderPlanMediaInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, ExecutionPlanID: plan.ExecutionPlanID, ShotID: input.ShotID,
	}).Get(mediaCtx, &output)
	return output, err
}

func processVideoRenderSegment(ctx workflow.Context, input ShotRenderExecutionInput, plan PlanShotVideoOutput, segment provider.GatewayVideoPlanSegment, poll PollShotVideoTaskOutput) (ProcessRenderSegmentMediaOutput, error) {
	options := mediaProcessingActivityOptions()
	mediaCtx := workflow.WithActivityOptions(ctx, options)
	var output ProcessRenderSegmentMediaOutput
	err := workflow.ExecuteActivity(mediaCtx, "ProcessRenderSegmentMedia", ProcessRenderSegmentMediaInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		CreatedBy: input.CreatedBy, ExecutionPlanID: plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
		SegmentIndex: segment.SegmentIndex, RawArtifactID: poll.ArtifactID, RawMediaFileID: poll.MediaFileID,
		RawStorageKey: poll.StorageKey, RawMimeType: poll.MimeType, AspectRatio: input.AspectRatio,
		Resolution: input.Resolution, FPSNumerator: int(plan.FPSNumerator), FPSDenominator: int(plan.FPSDenominator),
		PlannedDurationSeconds: segment.PlannedDurationSeconds,
	}).Get(mediaCtx, &output)
	return output, err
}

func renderSegmentDialogueLines(spans []provider.GatewayVideoDialogueSpan) []StoryboardDialogueLine {
	result := make([]StoryboardDialogueLine, 0, len(spans))
	for _, span := range spans {
		result = append(result, StoryboardDialogueLine{
			TimingUnitID: span.TimingUnitID, Speaker: span.Speaker, Text: span.Text, Delivery: span.Delivery, Kind: span.Kind,
			SpanStartTick: span.StartTick, SpanEndTick: span.EndTick,
			ContinuesFromPrevious: span.ContinuesFromPrevious, ContinuesToNext: span.ContinuesToNext,
		})
	}
	return result
}

type videoSegmentRetryDecision struct {
	RetryShotVideoRenderSegmentOutput
	Replan    bool
	Exhausted bool
}

func prepareVideoSegmentRetry(ctx workflow.Context, input ShotRenderExecutionInput, executionPlanID, segmentID string, currentRetryGeneration int, code, message string) (videoSegmentRetryDecision, error) {
	var output RetryShotVideoRenderSegmentOutput
	err := workflow.ExecuteActivity(ctx, "RetryShotVideoRenderSegment", RetryShotVideoRenderSegmentInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		ExecutionPlanID: executionPlanID, RenderSegmentID: segmentID, CurrentRetryGeneration: currentRetryGeneration,
		FailureCode: code, FailureMessage: message,
	}).Get(ctx, &output)
	if err == nil {
		return videoSegmentRetryDecision{RetryShotVideoRenderSegmentOutput: output}, nil
	}
	var applicationErr *temporal.ApplicationError
	if errors.As(err, &applicationErr) {
		switch applicationErr.Type() {
		case provider.CodeRenderPlanReplanRequired:
			return videoSegmentRetryDecision{Replan: true}, nil
		case provider.CodeModelCapabilityUnavailable:
			return videoSegmentRetryDecision{Exhausted: true}, nil
		}
	}
	return videoSegmentRetryDecision{}, err
}

func terminalVideoSegmentFailure(code, message string) error {
	code = firstNonEmptyString(code, provider.CodeUpstreamInternalError)
	message = firstNonEmptyString(message, "视频片段生成失败")
	if code == provider.CodeRenderPlanReplanRequired || code == provider.CodeStoryboardReplanRequired || code == provider.CodeProductionGenerationMismatch {
		return preparedVideoPromptError(message)
	}
	return temporal.NewNonRetryableApplicationError(message, code, nil)
}

func appendUniqueWorkflowString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
