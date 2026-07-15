package workflows

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ShotRenderExecutionInput struct {
	OrganizationID       string
	ProjectID            string
	WorkflowRunID        string
	CreatedBy            string
	ShotID               string
	ShotIndex            int
	ShotNo               int
	WorkflowPrompt       string
	FailureScope         string
	AspectRatio          string
	Resolution           string
	AudioStrategy        string
	AudioRequirement     string
	Force                bool
	MaxPolls             int
	PollInterval         time.Duration
	ContinuityFirstFrame *ShotContinuityFrameReference
}

type ShotContinuityFrameReference struct {
	SourceShotID          string `json:"sourceShotId"`
	SourceVideoArtifactID string `json:"sourceVideoArtifactId"`
	ArtifactID            string `json:"artifactId"`
	MediaFileID           string `json:"mediaFileId"`
	StorageKey            string `json:"storageKey"`
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

func executeShotRenderPlan(ctx, createCtx workflow.Context, input ShotRenderExecutionInput) (result ShotRenderExecutionResult, err error) {
	version := workflow.GetVersion(ctx, "video-consume-prepared-prompts-v1", workflow.DefaultVersion, 1)
	if version == workflow.DefaultVersion {
		return executeLegacyShotRenderPlan(ctx, createCtx, input)
	}
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
	var preparePlan workflow.Future
	materializeVersion := workflow.GetVersion(ctx, "video-materialize-reviewed-prompt-v1", workflow.DefaultVersion, 1)
	if materializeVersion == workflow.DefaultVersion {
		preparePlan = workflow.ExecuteActivity(ctx, "LoadPreparedShotVideoPlan", LoadPreparedShotVideoPlanInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			ShotID: input.ShotID, ShotIndex: input.ShotIndex, AspectRatio: input.AspectRatio, Resolution: input.Resolution,
			AudioStrategy: input.AudioStrategy, AudioRequirement: input.AudioRequirement,
		})
	} else {
		preparePlan = workflow.ExecuteActivity(ctx, "EnsurePreparedShotVideoPlan", EnsurePreparedShotVideoPlanInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			CreatedBy: input.CreatedBy, WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope,
			ShotID: input.ShotID, ShotIndex: input.ShotIndex,
			AspectRatio: input.AspectRatio, Resolution: input.Resolution, AudioStrategy: input.AudioStrategy,
			AudioRequirement: input.AudioRequirement, Force: input.Force,
		})
	}
	if err := preparePlan.Get(ctx, &prepared); err != nil {
		return ShotRenderExecutionResult{}, err
	}
	if len(prepared.Segments) == 0 {
		return ShotRenderExecutionResult{}, temporal.NewApplicationError("视频执行计划没有已审核片段提示词，请重新生成视频提示词", provider.CodeRenderPlanReplanRequired)
	}
	result.Plan = prepared.Plan
	var current CreateShotVideoTaskOutput
	defer func() {
		if ctx.Err() == nil || current.ProviderAsyncTaskID == "" || current.NodeRunID == "" {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		var cancelled CancelShotVideoTaskOutput
		_ = workflow.ExecuteActivity(cleanupCtx, "CancelShotVideoTask", CancelShotVideoTaskInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			ShotID: input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
			NodeRunID: current.NodeRunID, ProviderAsyncTaskID: current.ProviderAsyncTaskID, ExternalTaskID: current.ExternalTaskID,
			ExecutionPlanID: current.ExecutionPlanID, RenderSegmentID: current.RenderSegmentID,
			SegmentIndex: current.SegmentIndex, SegmentCount: current.SegmentCount,
			Reason: "Video render workflow cancellation requested",
		}).Get(cleanupCtx, &cancelled)
	}()
	const maxSegmentAttempts = 3
	planSegments := make([]PollShotVideoTaskOutput, 0, len(prepared.Segments))
	var previous PollShotVideoTaskOutput
	for _, segment := range prepared.Segments {
		segmentSucceeded := false
		retryGeneration := 0
		lastFailureCode := provider.CodeUpstreamInternalError
		lastFailureMessage := "视频片段生成失败"
		for segmentAttempt := 0; segmentAttempt < maxSegmentAttempts; segmentAttempt++ {
			created := CreateShotVideoTaskOutput{}
			createErr := workflow.ExecuteActivity(createCtx, "CreateShotVideoTask", CreateShotVideoTaskInput{
				OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
				CreatedBy: input.CreatedBy, ShotID: input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
				WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope, Duration: segment.RequestedDurationSeconds,
				PlannedDuration: segment.PlannedDurationSeconds,
				AspectRatio:     input.AspectRatio, Resolution: input.Resolution, Force: input.Force,
				ExecutionPlanID: prepared.Plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
				CapabilitySnapshotHash: prepared.Plan.CapabilitySnapshotHash, SegmentIndex: segment.SegmentIndex,
				SegmentCount: len(prepared.Segments), RetryGeneration: retryGeneration, ContinuityMode: segment.ContinuityMode,
				SegmentStartTick: segment.PlannedStartTick, SegmentEndTick: segment.PlannedEndTick,
				DialogueLines: renderSegmentDialogueLines(segment.DialogueSpans),
				AudioStrategy: input.AudioStrategy, AudioRequirement: input.AudioRequirement,
				ContinuityFirstFrame:      input.ContinuityFirstFrame,
				PreviousSegmentArtifactID: previous.ArtifactID, PreviousSegmentMediaFileID: previous.MediaFileID,
				PreviousSegmentStorageKey: previous.StorageKey,
				Prompt:                    segment.Prompt, NegativePrompt: segment.NegativePrompt, PromptHash: segment.PromptHash,
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
				retry, replan, retryErr := prepareVideoSegmentRetry(ctx, input, prepared.Plan.ExecutionPlanID, segment.SegmentID, retryGeneration, lastFailureCode, lastFailureMessage)
				if retryErr != nil {
					return ShotRenderExecutionResult{}, retryErr
				}
				if replan {
					return ShotRenderExecutionResult{}, preparedVideoPromptError("视频模型执行计划已失效，请重新批量生成视频提示词后再生成视频")
				}
				retryGeneration = retry.RetryGeneration
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
					current = CreateShotVideoTaskOutput{}
					break
				}
				if isTerminalVideoAttemptFailure(poll.Status) {
					current = CreateShotVideoTaskOutput{}
					lastFailureCode = firstNonEmptyString(poll.ErrorCode, provider.CodeUpstreamInternalError)
					lastFailureMessage = firstNonEmptyString(poll.ErrorMessage, "视频片段生成失败")
					retry, replan, retryErr := prepareVideoSegmentRetry(ctx, input, prepared.Plan.ExecutionPlanID, segment.SegmentID, retryGeneration, lastFailureCode, lastFailureMessage)
					if retryErr != nil {
						return ShotRenderExecutionResult{}, retryErr
					}
					if replan {
						return ShotRenderExecutionResult{}, preparedVideoPromptError("视频模型执行计划已失效，请重新批量生成视频提示词后再生成视频")
					}
					retryGeneration = retry.RetryGeneration
					pollFailed = true
					break
				}
				if err := workflow.Sleep(ctx, input.PollInterval); err != nil {
					return ShotRenderExecutionResult{}, err
				}
			}
			if segmentSucceeded {
				break
			}
			if pollFailed {
				continue
			}
			current = CreateShotVideoTaskOutput{}
			lastFailureCode = codeProviderVideoPollingTimeout
			lastFailureMessage = fmt.Sprintf("视频片段 %d 轮询超时", segment.SegmentIndex+1)
			retry, replan, retryErr := prepareVideoSegmentRetry(ctx, input, prepared.Plan.ExecutionPlanID, segment.SegmentID, retryGeneration, lastFailureCode, lastFailureMessage)
			if retryErr != nil {
				return ShotRenderExecutionResult{}, retryErr
			}
			if replan {
				return ShotRenderExecutionResult{}, preparedVideoPromptError("视频模型执行计划已失效，请重新批量生成视频提示词后再生成视频")
			}
			retryGeneration = retry.RetryGeneration
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

func executeLegacyShotRenderPlan(ctx, createCtx workflow.Context, input ShotRenderExecutionInput) (result ShotRenderExecutionResult, err error) {
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
	var current CreateShotVideoTaskOutput
	defer func() {
		if ctx.Err() == nil || current.ProviderAsyncTaskID == "" || current.NodeRunID == "" {
			return
		}
		cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
		var cancelled CancelShotVideoTaskOutput
		_ = workflow.ExecuteActivity(cleanupCtx, "CancelShotVideoTask", CancelShotVideoTaskInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			ShotID: input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
			NodeRunID: current.NodeRunID, ProviderAsyncTaskID: current.ProviderAsyncTaskID, ExternalTaskID: current.ExternalTaskID,
			ExecutionPlanID: current.ExecutionPlanID, RenderSegmentID: current.RenderSegmentID,
			SegmentIndex: current.SegmentIndex, SegmentCount: current.SegmentCount,
			Reason: "Video render workflow cancellation requested",
		}).Get(cleanupCtx, &cancelled)
	}()
	attemptedModels := make([]string, 0, 3)
	previousPlanID := ""
	const maxPlanAttempts = 3
	const maxSegmentAttempts = 3
	for planAttempt := 0; planAttempt < maxPlanAttempts; planAttempt++ {
		var plan PlanShotVideoOutput
		if err := workflow.ExecuteActivity(ctx, "PlanShotVideo", PlanShotVideoInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
			WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope,
			ShotID: input.ShotID, ShotIndex: input.ShotIndex, AspectRatio: input.AspectRatio, Resolution: input.Resolution,
			AudioStrategy: input.AudioStrategy, AudioRequirement: input.AudioRequirement,
			Force:                   input.Force,
			ExcludeProviderModelIDs: attemptedModels, PreviousExecutionPlanID: previousPlanID,
		}).Get(ctx, &plan); err != nil {
			return ShotRenderExecutionResult{}, err
		}
		if len(plan.Segments) == 0 {
			return ShotRenderExecutionResult{}, temporal.NewApplicationError("video render plan has no segments", codeActivityFailed)
		}
		result.Plan = plan
		attemptedModels = appendUniqueWorkflowString(attemptedModels, plan.ProviderModelID)
		planSegments := make([]PollShotVideoTaskOutput, 0, len(plan.Segments))
		var previous PollShotVideoTaskOutput
		wholeShotReplan := false

		for segmentIndex, segment := range plan.Segments {
			segmentSucceeded := false
			retryGeneration := 0
			for segmentAttempt := 0; segmentAttempt < maxSegmentAttempts; segmentAttempt++ {
				created, createErr := executeAgentReviewedShotVideoCreate(ctx, createCtx, CreateShotVideoTaskInput{
					OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
					CreatedBy: input.CreatedBy, ShotID: input.ShotID, ShotIndex: input.ShotIndex, ShotNo: input.ShotNo,
					WorkflowPrompt: input.WorkflowPrompt, FailureScope: input.FailureScope, Duration: segment.RequestedDurationSeconds,
					PlannedDuration: segment.PlannedDurationSeconds,
					AspectRatio:     input.AspectRatio, Resolution: input.Resolution, Force: input.Force,
					ExecutionPlanID: plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
					CapabilitySnapshotHash: plan.CapabilitySnapshotHash, SegmentIndex: segmentIndex,
					SegmentCount: len(plan.Segments), RetryGeneration: retryGeneration, ContinuityMode: segment.ContinuityMode,
					SegmentStartTick: segment.PlannedStartTick, SegmentEndTick: segment.PlannedEndTick,
					DialogueLines: renderSegmentDialogueLines(segment.DialogueSpans),
					AudioStrategy: input.AudioStrategy, AudioRequirement: input.AudioRequirement,
					ContinuityFirstFrame:      input.ContinuityFirstFrame,
					PreviousSegmentArtifactID: previous.ArtifactID, PreviousSegmentMediaFileID: previous.MediaFileID,
					PreviousSegmentStorageKey: previous.StorageKey,
				})
				if createErr != nil {
					return ShotRenderExecutionResult{}, createErr
				}
				result.Creates = append(result.Creates, created)
				attemptedModels = appendUniqueWorkflowString(attemptedModels, created.ModelID)
				if isTerminalVideoAttemptFailure(created.Status) || strings.TrimSpace(created.ErrorCode) != "" {
					retry, replan, retryErr := prepareVideoSegmentRetry(ctx, input, plan.ExecutionPlanID, segment.SegmentID, retryGeneration, created.ErrorCode, created.ErrorMessage)
					if retryErr != nil {
						return ShotRenderExecutionResult{}, retryErr
					}
					if replan {
						wholeShotReplan = true
						break
					}
					retryGeneration = retry.RetryGeneration
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
						ExecutionPlanID: plan.ExecutionPlanID, RenderSegmentID: segment.SegmentID,
						SegmentIndex: segmentIndex, SegmentCount: len(plan.Segments),
					}).Get(ctx, &poll); err != nil {
						return ShotRenderExecutionResult{}, err
					}
					result.Polls = append(result.Polls, poll)
					if poll.Status == "succeeded" {
						processed, processErr := processVideoRenderSegment(ctx, input, plan, segment, poll)
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
						current = CreateShotVideoTaskOutput{}
						break
					}
					if isTerminalVideoAttemptFailure(poll.Status) {
						current = CreateShotVideoTaskOutput{}
						retry, replan, retryErr := prepareVideoSegmentRetry(ctx, input, plan.ExecutionPlanID, segment.SegmentID, retryGeneration, poll.ErrorCode, poll.ErrorMessage)
						if retryErr != nil {
							return ShotRenderExecutionResult{}, retryErr
						}
						if replan {
							wholeShotReplan = true
						} else {
							retryGeneration = retry.RetryGeneration
						}
						pollFailed = true
						break
					}
					if err := workflow.Sleep(ctx, input.PollInterval); err != nil {
						return ShotRenderExecutionResult{}, err
					}
				}
				if wholeShotReplan || segmentSucceeded {
					break
				}
				if pollFailed {
					continue
				}
				current = CreateShotVideoTaskOutput{}
				retry, replan, retryErr := prepareVideoSegmentRetry(ctx, input, plan.ExecutionPlanID, segment.SegmentID, retryGeneration, codeProviderVideoPollingTimeout, fmt.Sprintf("render segment %d polling timed out", segmentIndex+1))
				if retryErr != nil {
					return ShotRenderExecutionResult{}, retryErr
				}
				if replan {
					wholeShotReplan = true
					break
				}
				retryGeneration = retry.RetryGeneration
			}
			if wholeShotReplan {
				break
			}
			if !segmentSucceeded {
				wholeShotReplan = true
				break
			}
		}
		if !wholeShotReplan {
			result.Segments = planSegments
			if len(planSegments) > 0 {
				result.LastSegment = planSegments[len(planSegments)-1]
			}
			composed, composeErr := composeShotRenderPlan(ctx, input, plan)
			if composeErr != nil {
				return ShotRenderExecutionResult{}, composeErr
			}
			result.Output = composed
			return result, nil
		}
		previousPlanID = plan.ExecutionPlanID
	}
	return ShotRenderExecutionResult{}, temporal.NewApplicationError("video render plan fallback attempts were exhausted", provider.CodeModelCapabilityUnavailable)
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

func prepareVideoSegmentRetry(ctx workflow.Context, input ShotRenderExecutionInput, executionPlanID, segmentID string, currentRetryGeneration int, code, message string) (RetryShotVideoRenderSegmentOutput, bool, error) {
	var output RetryShotVideoRenderSegmentOutput
	err := workflow.ExecuteActivity(ctx, "RetryShotVideoRenderSegment", RetryShotVideoRenderSegmentInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, WorkflowRunID: input.WorkflowRunID,
		ExecutionPlanID: executionPlanID, RenderSegmentID: segmentID, CurrentRetryGeneration: currentRetryGeneration,
		FailureCode: code, FailureMessage: message,
	}).Get(ctx, &output)
	if err == nil {
		return output, false, nil
	}
	var applicationErr *temporal.ApplicationError
	if errors.As(err, &applicationErr) && applicationErr.Type() == provider.CodeRenderPlanReplanRequired {
		return RetryShotVideoRenderSegmentOutput{}, true, nil
	}
	return RetryShotVideoRenderSegmentOutput{}, false, err
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
