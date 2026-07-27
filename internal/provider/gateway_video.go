package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const defaultGatewayVideoMaxBytes int64 = 512 << 20

type gatewayVideoInput struct {
	Prompt          string
	DurationSeconds float64
	AspectRatio     string
	Resolution      string
	Mode            string
}

type gatewayVideoMedia struct {
	Body        []byte
	TempPath    string
	MimeType    string
	ByteSize    int64
	ContentHash string
}

type gatewayStoredVideo struct {
	ArtifactID  string
	MediaFileID string
	Output      GatewayVideoOutput
	Media       gatewayVideoMedia
}

type gatewayVideoTask struct {
	ID                             string
	ProviderCallID                 string
	OrganizationID                 string
	ProjectID                      string
	OperationID                    string
	OperationItemID                string
	OperationItemAttempt           int
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
	WorkflowRunID                  string
	NodeRunID                      string
	NodeExecutionToken             string
	NodeAttemptGeneration          int
	ProviderAccountID              string
	ProviderModelID                string
	CredentialID                   string
	ModelProfileID                 string
	ModelProfileBindingID          string
	ModelProfileKey                string
	PromptTemplateKey              string
	PromptVersionID                string
	PromptHash                     string
	PromptSource                   string
	ExternalTaskID                 string
	Status                         string
	Input                          json.RawMessage
	NormalizedOutput               json.RawMessage
	PollCount                      int
	ExecutionPlanID                string
	RenderSegmentID                string
	VideoVariantKey                string
	CapabilitySnapshotHash         string
}

type gatewayVideoExecutionProvenance struct {
	OperationID          string
	OperationItemID      string
	OperationItemAttempt int
	ExecutionPlanID      string
	RenderSegmentID      string
}

func (s *Service) CreateVideoTask(ctx context.Context, req GatewayVideoCreateTaskRequest) (GatewayVideoCreateTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	if err := s.assertProviderProjectWritable(ctx, req.OrganizationID, req.ProjectID); err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input
	if err := s.validateGatewayVideoProductionIdentity(
		ctx, req.OrganizationID, req.ProjectID, req.ProductionGenerationID, req.VideoProductionBindingID,
		req.VideoProductionBindingRevision, req.WorkflowRunID, req.NodeRunID,
	); err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	if err := s.validateGatewayVideoNodeExecution(ctx, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, req.NodeExecutionToken, req.NodeAttemptGeneration); err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	start, err := s.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID:                 req.OrganizationID,
		ProjectID:                      req.ProjectID,
		ProductionGenerationID:         req.ProductionGenerationID,
		VideoProductionBindingID:       req.VideoProductionBindingID,
		VideoProductionBindingRevision: req.VideoProductionBindingRevision,
		WorkflowRunID:                  req.WorkflowRunID,
		NodeRunID:                      req.NodeRunID,
		OperationID:                    req.OperationID,
		OperationItemID:                req.OperationItemID,
		OperationItemAttempt:           req.OperationItemAttempt,
		ExecutionPlanID:                req.ExecutionPlanID,
		RenderSegmentID:                req.RenderSegmentID,
		TaskType:                       TaskTypeVideoCreateTask,
		IdempotencyKey:                 gatewayVideoIdempotencyKey(req.IdempotencyKey, req.Options),
		RequestHash:                    requestHash,
		Retry:                          req.Options.Retry,
	})
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayVideoCreateTaskResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerVideoCreateStatusResponse(start.Request)
		}
		response.ProviderRequestID = start.Request.ID
		response.AttemptGeneration = start.Request.AttemptGeneration
		return response, nil
	}
	if start.Disposition == providerRequestInProgress {
		return providerVideoCreateStatusResponse(start.Request), nil
	}
	response, runErr := s.executeGatewayVideoCreate(ctx, req, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		_, standardError := StandardErrorFromError(runErr)
		if standardError || errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayVideoCreateTaskResponse{ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration, Status: "failed", Error: standard}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, "failed", response, nil, nil, standard); completeErr != nil {
				return GatewayVideoCreateTaskResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayVideoCreateTaskResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.AttemptGeneration = start.Request.AttemptGeneration
	logicalStatus := "succeeded"
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		logicalStatus = "failed"
	}
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, logicalStatus, response, nil, nil, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayVideoCreateTaskResponse{}, err
	}
	return response, nil
}

func providerVideoCreateStatusResponse(request ProviderRequest) GatewayVideoCreateTaskResponse {
	return GatewayVideoCreateTaskResponse{ProviderRequestID: request.ID, AttemptGeneration: request.AttemptGeneration, Status: request.Status, Error: providerRequestStatusError(request)}
}

func (s *Service) executeGatewayVideoCreate(ctx context.Context, req GatewayVideoCreateTaskRequest, providerRequestID string, attemptGeneration int) (GatewayVideoCreateTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	videoInput, err := parseGatewayVideoInput(input)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	req.References, err = s.hydrateGatewayVideoReferences(ctx, req.OrganizationID, req.References)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	req.Input = input
	executionSegment, err := s.validateVideoExecutionRequest(ctx, &req, videoInput)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	if executionSegment != nil && executionSegment.ProviderAsyncTaskID != "" && executionSegment.ProviderTaskStatus != "failed" && executionSegment.ProviderTaskStatus != "cancelled" {
		existingTask, taskErr := s.getGatewayVideoTask(ctx, GatewayVideoPollTaskRequest{
			OrganizationID: req.OrganizationID, ProviderAsyncTaskID: executionSegment.ProviderAsyncTaskID,
		})
		if taskErr != nil {
			return GatewayVideoCreateTaskResponse{}, &StandardErrorError{Standard: StandardError{
				Code: CodeRenderPlanReplanRequired, Message: "视频渲染片段关联的供应商任务已失效，请重新生成视频提示词计划", Retryable: false,
			}}
		}
		if err := validateGatewayVideoCreateTaskIdentity(existingTask, req); err != nil {
			return GatewayVideoCreateTaskResponse{}, err
		}
		return GatewayVideoCreateTaskResponse{
			ProviderRequestID: providerRequestID, AttemptGeneration: attemptGeneration,
			ProviderCallID: executionSegment.ProviderCallID, ProviderAsyncTaskID: executionSegment.ProviderAsyncTaskID,
			ExternalTaskID: executionSegment.ExternalTaskID, ModelID: executionSegment.ProviderModelID,
			Status: executionSegment.ProviderTaskStatus, ExecutionPlanID: executionSegment.ExecutionPlanID, RenderSegmentID: executionSegment.RenderSegmentID,
		}, nil
	}
	if executionSegment != nil {
		recovered, found, recoveryErr := s.tryBindGatewayVideoMediaRecovery(
			ctx, req, videoInput, *executionSegment, providerRequestID, attemptGeneration,
		)
		if recoveryErr != nil {
			return GatewayVideoCreateTaskResponse{}, recoveryErr
		}
		if found {
			return recovered, nil
		}
	}

	if strings.TrimSpace(req.ProviderModelID) != "" {
		selection, err := s.selectGatewayVideoModel(ctx, req.OrganizationID, req.ProviderModelID, req.ModelProfileKey)
		if err != nil {
			return GatewayVideoCreateTaskResponse{}, err
		}
		response, _, err := s.executeGatewayVideoCreateAttempt(ctx, req, videoInput, selection, 1, 1, string(RoutingPriority), providerRequestID, attemptGeneration)
		response.ExecutionPlanID = req.ExecutionPlanID
		response.RenderSegmentID = req.RenderSegmentID
		return response, err
	}

	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:       req.OrganizationID,
		ModelProfileKey:      req.ModelProfileKey,
		TaskType:             TaskTypeVideoCreateTask,
		Modality:             "video",
		VideoDurationSeconds: videoInput.DurationSeconds,
		VideoResolution:      videoInput.Resolution,
	})
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	strategy := candidates[0].FallbackStrategy
	maxAttempts := fallbackMaxAttempts(strategy, len(candidates))
	attempts := make([]GatewayAttempt, 0, maxAttempts)
	var final GatewayVideoCreateTaskResponse
	for i := 0; i < maxAttempts; i++ {
		candidate := candidates[i]
		selection, err := s.completeGatewaySelectionFromCandidate(ctx, req.OrganizationID, candidate)
		if err != nil {
			return GatewayVideoCreateTaskResponse{}, err
		}
		response, attempt, err := s.executeGatewayVideoCreateAttempt(ctx, req, videoInput, selection, i+1, maxAttempts, candidate.RoutingStrategy, providerRequestID, attemptGeneration)
		if err != nil {
			return GatewayVideoCreateTaskResponse{}, err
		}
		attempts = append(attempts, attempt)
		response.Attempts = append([]GatewayAttempt(nil), attempts...)
		final = response
		if !isProviderFailureStatus(response.Status) {
			return response, nil
		}
		if i+1 >= maxAttempts || !shouldFallback(gatewayErrorCode(response.Error), strategy) {
			return response, nil
		}
	}
	return final, nil
}

func validateGatewayVideoCreateTaskIdentity(task gatewayVideoTask, req GatewayVideoCreateTaskRequest) error {
	err := validateGatewayVideoTaskRequestIdentity(task, GatewayVideoPollTaskRequest{
		NodeRunID: req.NodeRunID, NodeExecutionToken: req.NodeExecutionToken,
		NodeAttemptGeneration: req.NodeAttemptGeneration,
	})
	if err == nil {
		return nil
	}
	return &StandardErrorError{Standard: StandardError{
		Code: CodeRenderPlanReplanRequired, Message: "视频渲染片段已绑定到另一工作流执行，请重新生成视频提示词计划", Retryable: false,
	}}
}

func (s *Service) executeGatewayVideoCreateAttempt(ctx context.Context, req GatewayVideoCreateTaskRequest, videoInput gatewayVideoInput, selection gatewayModelSelection, attemptIndex, maxAttempts int, selectedBy, providerRequestID string, attemptGeneration int) (GatewayVideoCreateTaskResponse, GatewayAttempt, error) {
	openAICompatibleRuntime := usesNativeOpenAICompatibleRuntime(selection.Account)
	var manifest ProviderManifest
	var endpointKey string
	var endpoint ManifestEndpoint
	var openAIConfig openAICompatibleConfig
	endpointTimeoutMS := 0
	if openAICompatibleRuntime {
		openAIConfig = parseOpenAICompatibleConfig(selection.Account.Config)
		endpointTimeoutMS = openAIConfig.TimeoutMS
	} else {
		var err error
		manifest, err = s.manifestForAccount(ctx, selection.Account)
		if err != nil {
			return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, err
		}
		endpointKey, endpoint, err = selectVideoCreateEndpoint(selection, manifest)
		if err != nil {
			return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, err
		}
		endpointTimeoutMS = endpoint.TimeoutMS
	}
	timeout := gatewayVideoTimeout(req.Options.TimeoutMS, endpointTimeoutMS)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	callID := uuid.NewString()
	baseCall := RecordCallRequest{
		ID:                     callID,
		ProviderRequestID:      providerRequestID,
		AttemptGeneration:      attemptGeneration,
		AttemptSequence:        attemptIndex,
		OrganizationID:         req.OrganizationID,
		ProjectID:              req.ProjectID,
		ProductionGenerationID: req.ProductionGenerationID,
		OperationID:            req.OperationID,
		OperationItemID:        req.OperationItemID,
		OperationItemAttempt:   req.OperationItemAttempt,
		ExecutionPlanID:        req.ExecutionPlanID,
		RenderSegmentID:        req.RenderSegmentID,
		WorkflowRunID:          req.WorkflowRunID,
		NodeRunID:              req.NodeRunID,
		ProviderAccountID:      selection.Account.ID,
		ProviderModelID:        selection.Model.ID,
		CredentialID:           selection.CredentialID,
		ModelProfileID:         selection.ModelProfileID,
		ModelProfileBindingID:  selection.ModelProfileBindingID,
		ModelProfileKey:        selection.ModelProfileKey,
		PromptVersionID:        req.PromptVersionID,
		PromptHash:             req.PromptHash,
		IdempotencyKey:         gatewayVideoIdempotencyKey(req.IdempotencyKey, req.Options),
		TaskType:               TaskTypeVideoCreateTask,
		ExecutionMode:          "async_create",
		Status:                 "running",
		RequestSnapshot:        req.Input,
	}
	if _, err := recordCall(ctx, s.db, baseCall); err != nil {
		return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, err
	}
	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: req.OrganizationID,
		Selection:      selection,
		TaskType:       TaskTypeVideoCreateTask,
		EstimatedCost:  "0.00000000",
		Currency:       "USD",
		LeaseTTL:       2 * time.Minute,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		standard, ok := blockedGatewayStandard(guardErr)
		if !ok {
			return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, guardErr
		}
		blockedCall := baseCall
		blockedCall.Status = "blocked"
		blockedCall.ErrorCode = standard.Code
		blockedCall.ErrorMessage = standard.Message
		blockedCall.ResponseSnapshot = blockedResponseSnapshot(standard)
		blockedCall.NormalizedOutput = withRoutingNormalizedOutput(blockedNormalizedOutput(standard), selection, attemptIndex, maxAttempts, selectedBy)
		call, err := recordCall(ctx, s.db, blockedCall)
		if err != nil {
			return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, err
		}
		attempt := gatewayAttemptFromCall(call, selection, standard, 0)
		return GatewayVideoCreateTaskResponse{
			ProviderRequestID: providerRequestID,
			AttemptGeneration: attemptGeneration,
			ProviderCallID:    call.ID,
			ModelID:           selection.Model.ID,
			Status:            "blocked",
			Error:             standard,
			Attempts:          []GatewayAttempt{attempt},
		}, attempt, nil
	}
	providerCallID := ""
	defer func() {
		s.releaseGatewayLease(lease, providerCallID)
	}()

	manifestInput := mergeJSONObjects(mustJSON(map[string]any{
		"prompt":         videoInput.Prompt,
		"duration":       videoInput.DurationSeconds,
		"aspectRatio":    videoInput.AspectRatio,
		"resolution":     videoInput.Resolution,
		"mode":           videoInput.Mode,
		"negativePrompt": "",
	}), req.Input)
	started := time.Now()
	var result manifestRunResult
	var runErr error
	if validationErr := validateGatewayVideoPromptForModel(videoInput.Prompt, selection.Model); validationErr != nil {
		result.RequestSnapshot = req.Input
		runErr = validationErr
	} else if openAICompatibleRuntime {
		client := newOpenAICompatibleClient(timeout)
		result, runErr = client.createVideoTask(callCtx, selection.Account, selection.Model, selection.APIKey, openAIConfig, req.Input, req.References)
	} else {
		result, runErr = callManifestEndpointWithContext(callCtx, manifest, selection.Account, selection.Credential, endpointKey, endpoint, manifestInput, videoManifestContext(selection, req.References, nil))
	}
	latencyMS := int(time.Since(started).Milliseconds())
	if result.LatencyMS > latencyMS {
		latencyMS = result.LatencyMS
	}

	status := normalizeGatewayVideoStatus(result.Status)
	if status == "" {
		status = normalizeGatewayVideoStatus(videoStringField(result.NormalizedOutput, "status"))
	}
	if status == "" {
		status = "running"
	}
	externalTaskID := videoStringField(result.NormalizedOutput, "externalTaskId", "taskId", "id")
	if externalTaskID == "" {
		externalTaskID = videoStringField(req.Input, "externalTaskId", "taskId")
	}
	normalizedOutput := result.NormalizedOutput
	responseSnapshot := result.ResponseSnapshot
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	var stored *gatewayStoredVideo
	usage := GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"}

	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
		if len(normalizedOutput) == 0 {
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		}
	} else if status == "failed" {
		errorCode, errorMessage, upstreamErrorCode, standardError = normalizedVideoTerminalFailure(normalizedOutput)
	} else if videoURL := videoStringField(normalizedOutput, "videoUrl", "url", "outputUrl"); status == "succeeded" && strings.TrimSpace(videoURL) != "" {
		if s.objectStorage == nil {
			return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, fmt.Errorf("%w: object storage is not configured", ErrValidation)
		}
		var mediaErr error
		stored, mediaErr = s.downloadAndStoreGatewayVideoMedia(ctx, callID, req.OrganizationID, req.ProjectID, selection, externalTaskID, result, videoInput)
		if mediaErr != nil {
			status = "failed"
			errorCode, errorMessage, standardError = gatewayVideoMediaFailure(mediaErr)
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode, "errorMessage": errorMessage})
		} else {
			usage = estimateVideoCost(videoInput, stored.Output.DurationSeconds, selection.Model.Capabilities)
			normalizedOutput = mustJSON(stored.Output)
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = json.RawMessage(`{}`)
	}
	normalizedOutput = withRoutingNormalizedOutput(normalizedOutput, selection, attemptIndex, maxAttempts, selectedBy)

	call, taskID, err := s.recordVideoCreateTask(ctx, selection, req, providerRequestID, attemptGeneration, attemptIndex, callID, lease.LeaseID, externalTaskID, status, latencyMS, errorCode, errorMessage, upstreamStatus, upstreamErrorCode, result.RequestSnapshot, responseSnapshot, normalizedOutput, usage, stored, videoInput)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, GatewayAttempt{}, err
	}
	providerCallID = call.ID
	if isProviderFailureStatus(status) {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}
	attempt := gatewayAttemptFromCall(call, selection, standardError, latencyMS)
	return GatewayVideoCreateTaskResponse{
		ProviderRequestID:   providerRequestID,
		AttemptGeneration:   attemptGeneration,
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: taskID,
		ExternalTaskID:      externalTaskID,
		ModelID:             selection.Model.ID,
		Status:              status,
		Error:               standardError,
		LatencyMS:           latencyMS,
		Attempts:            []GatewayAttempt{attempt},
	}, attempt, nil
}

func (s *Service) PollVideoTask(ctx context.Context, req GatewayVideoPollTaskRequest) (GatewayVideoPollTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	task, err := s.getGatewayVideoTask(ctx, req)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if err := validateGatewayVideoTaskRequestIdentity(task, req); err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if err := s.validateGatewayVideoProductionIdentity(
		ctx, task.OrganizationID, task.ProjectID, task.ProductionGenerationID, task.VideoProductionBindingID,
		task.VideoProductionBindingRevision, task.WorkflowRunID, task.NodeRunID,
	); err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if err := s.validateGatewayVideoNodeExecution(ctx, task.OrganizationID, task.ProjectID, task.WorkflowRunID, task.NodeRunID, task.NodeExecutionToken, task.NodeAttemptGeneration); err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	startInput := providerRequestStartInput{
		OrganizationID:                 req.OrganizationID,
		ProjectID:                      task.ProjectID,
		ProductionGenerationID:         task.ProductionGenerationID,
		VideoProductionBindingID:       task.VideoProductionBindingID,
		VideoProductionBindingRevision: task.VideoProductionBindingRevision,
		WorkflowRunID:                  task.WorkflowRunID,
		NodeRunID:                      task.NodeRunID,
		OperationID:                    task.OperationID,
		OperationItemID:                task.OperationItemID,
		OperationItemAttempt:           task.OperationItemAttempt,
		ExecutionPlanID:                task.ExecutionPlanID,
		RenderSegmentID:                task.RenderSegmentID,
		TaskType:                       TaskTypeVideoPollTask,
		IdempotencyKey:                 gatewayVideoIdempotencyKey("", req.Options),
		RequestHash:                    requestHash,
		Retry:                          req.Options.Retry,
	}
	start, err := s.beginProviderRequest(ctx, startInput)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayVideoPollTaskResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerVideoPollStatusResponse(start.Request)
		}
		persisted, persistenceErr := s.gatewayVideoReplayOutputPersisted(ctx, task, response)
		if persistenceErr != nil {
			return GatewayVideoPollTaskResponse{}, persistenceErr
		}
		if persisted || !gatewayVideoReplayNeedsMaterialization(response) {
			response.ProviderRequestID = start.Request.ID
			response.AttemptGeneration = start.Request.AttemptGeneration
			return response, nil
		}
		if err := s.markProviderRequestResultMissing(ctx, start.Request.ID, start.Request.AttemptGeneration); err != nil {
			return GatewayVideoPollTaskResponse{}, err
		}
		startInput.Retry = true
		start, err = s.beginProviderRequest(ctx, startInput)
		if err != nil {
			return GatewayVideoPollTaskResponse{}, err
		}
		if start.Disposition == providerRequestReplay {
			repaired, decodeErr := decodeProviderRequestResult[GatewayVideoPollTaskResponse](start.Request)
			if decodeErr == nil {
				repairedPersisted, persistenceErr := s.gatewayVideoReplayOutputPersisted(ctx, task, repaired)
				if persistenceErr != nil {
					return GatewayVideoPollTaskResponse{}, persistenceErr
				}
				if repairedPersisted || !gatewayVideoReplayNeedsMaterialization(repaired) {
					repaired.ProviderRequestID = start.Request.ID
					repaired.AttemptGeneration = start.Request.AttemptGeneration
					return repaired, nil
				}
			}
			return GatewayVideoPollTaskResponse{
				ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration,
				Status: "running",
			}, nil
		}
		if start.Disposition == providerRequestInProgress {
			return providerVideoPollStatusResponse(start.Request), nil
		}
	}
	if start.Disposition == providerRequestInProgress {
		return providerVideoPollStatusResponse(start.Request), nil
	}
	response, runErr := s.executeGatewayVideoPoll(ctx, req, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		if errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayVideoPollTaskResponse{ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration, Status: "failed", Error: standard}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, "failed", response, nil, nil, standard); completeErr != nil {
				return GatewayVideoPollTaskResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayVideoPollTaskResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.AttemptGeneration = start.Request.AttemptGeneration
	logicalStatus := "succeeded"
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		logicalStatus = "failed"
	}
	artifactIDs := []string{}
	mediaFileIDs := []string{}
	if response.Output.ArtifactID != "" {
		artifactIDs = append(artifactIDs, response.Output.ArtifactID)
	}
	if response.Output.MediaFileID != "" {
		mediaFileIDs = append(mediaFileIDs, response.Output.MediaFileID)
	}
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, logicalStatus, response, artifactIDs, mediaFileIDs, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayVideoPollTaskResponse{}, err
	}
	return response, nil
}

func providerVideoPollStatusResponse(request ProviderRequest) GatewayVideoPollTaskResponse {
	return GatewayVideoPollTaskResponse{ProviderRequestID: request.ID, AttemptGeneration: request.AttemptGeneration, Status: request.Status, Error: providerRequestStatusError(request)}
}

func gatewayVideoReplayNeedsMaterialization(response GatewayVideoPollTaskResponse) bool {
	return response.Status == "succeeded" && (strings.TrimSpace(response.Output.ArtifactID) != "" || strings.TrimSpace(response.Output.MediaFileID) != "")
}

func (s *Service) gatewayVideoReplayOutputPersisted(ctx context.Context, task gatewayVideoTask, response GatewayVideoPollTaskResponse) (bool, error) {
	if !gatewayVideoReplayNeedsMaterialization(response) {
		return false, nil
	}
	var persisted bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM artifacts artifact
			JOIN media_files media ON media.id = $2 AND media.artifact_id = artifact.id
			WHERE artifact.id = $1
			  AND artifact.organization_id = $3
			  AND artifact.project_id = NULLIF($4, '')::uuid
			  AND artifact.production_generation_id = NULLIF($5, '')::uuid
			  AND artifact.storage_key = $6
			  AND media.organization_id = artifact.organization_id
			  AND media.project_id = artifact.project_id
			  AND media.production_generation_id = artifact.production_generation_id
			  AND media.storage_key = artifact.storage_key
		)
	`, response.Output.ArtifactID, response.Output.MediaFileID, task.OrganizationID, task.ProjectID, task.ProductionGenerationID, response.Output.StorageKey).Scan(&persisted)
	return persisted, err
}

func (s *Service) markProviderRequestResultMissing(ctx context.Context, requestID string, attemptGeneration int) error {
	_, err := s.db.Exec(ctx, `
		UPDATE provider_requests
		SET status = 'unknown_outcome',
		    error_code = $3,
		    error_message = 'provider video result references missing persisted artifacts',
		    completed_at = now(),
		    updated_at = now()
		WHERE id = $1 AND attempt_generation = $2 AND status = 'succeeded'
	`, requestID, attemptGeneration, CodeProviderUnknownOutcome)
	return err
}

func (s *Service) executeGatewayVideoPoll(ctx context.Context, req GatewayVideoPollTaskRequest, providerRequestID string, attemptGeneration int) (GatewayVideoPollTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	task, err := s.getGatewayVideoTask(ctx, req)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	pollSourceTask, err := s.gatewayVideoPollSourceTask(ctx, task)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if task.OrganizationID != req.OrganizationID {
		return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: provider async task belongs to a different organization", ErrValidation)
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, task.ProviderAccountID)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	model, err := s.GetModel(ctx, req.OrganizationID, task.ProviderModelID)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if strings.TrimSpace(task.CredentialID) == "" {
		return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: provider async task has no credential identity", ErrValidation)
	}
	credential, credentialID, err := s.credentialPayloadByID(ctx, req.OrganizationID, account.ID, task.CredentialID)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	selection := gatewayModelSelection{
		Account:               account,
		Model:                 model,
		CredentialID:          credentialID,
		Credential:            credential,
		APIKey:                apiKey,
		ModelProfileID:        task.ModelProfileID,
		ModelProfileBindingID: task.ModelProfileBindingID,
		ModelProfileKey:       task.ModelProfileKey,
	}
	openAICompatibleRuntime := usesNativeOpenAICompatibleRuntime(account)
	var manifest ProviderManifest
	var endpointKey string
	var endpoint ManifestEndpoint
	var openAIConfig openAICompatibleConfig
	endpointTimeoutMS := 0
	if openAICompatibleRuntime {
		openAIConfig = parseOpenAICompatibleConfig(account.Config)
		endpointTimeoutMS = openAIConfig.VideoPollTimeoutMS
	} else {
		manifest, err = s.manifestForAccount(ctx, account)
		if err != nil {
			return GatewayVideoPollTaskResponse{}, err
		}
		_, createEndpoint, _ := selectVideoCreateEndpoint(selection, manifest)
		endpointKey, endpoint, err = selectVideoPollEndpoint(selection, manifest, createEndpoint)
		if err != nil {
			return GatewayVideoPollTaskResponse{}, err
		}
		endpointTimeoutMS = endpoint.TimeoutMS
	}
	timeout := gatewayVideoTimeout(req.Options.TimeoutMS, endpointTimeoutMS)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	callID := uuid.NewString()
	baseCall := RecordCallRequest{
		ID:                     callID,
		ProviderRequestID:      providerRequestID,
		AttemptGeneration:      attemptGeneration,
		AttemptSequence:        1,
		OrganizationID:         task.OrganizationID,
		ProjectID:              firstNonEmpty(req.ProjectID, task.ProjectID),
		ProductionGenerationID: task.ProductionGenerationID,
		OperationID:            task.OperationID,
		OperationItemID:        task.OperationItemID,
		OperationItemAttempt:   task.OperationItemAttempt,
		ExecutionPlanID:        task.ExecutionPlanID,
		RenderSegmentID:        task.RenderSegmentID,
		WorkflowRunID:          firstNonEmpty(req.WorkflowRunID, task.WorkflowRunID),
		NodeRunID:              firstNonEmpty(req.NodeRunID, task.NodeRunID),
		ProviderAccountID:      selection.Account.ID,
		ProviderModelID:        selection.Model.ID,
		CredentialID:           selection.CredentialID,
		ModelProfileID:         selection.ModelProfileID,
		ModelProfileBindingID:  selection.ModelProfileBindingID,
		ModelProfileKey:        selection.ModelProfileKey,
		PromptVersionID:        task.PromptVersionID,
		PromptHash:             task.PromptHash,
		IdempotencyKey:         gatewayVideoIdempotencyKey("", req.Options),
		TaskType:               TaskTypeVideoPollTask,
		ExecutionMode:          "async_poll",
		Status:                 "running",
		RequestSnapshot:        task.Input,
	}
	if _, err := recordCall(ctx, s.db, baseCall); err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: task.OrganizationID,
		Selection:      selection,
		TaskType:       TaskTypeVideoPollTask,
		EstimatedCost:  "0.00000000",
		Currency:       "USD",
		LeaseTTL:       time.Minute,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		standard, ok := blockedGatewayStandard(guardErr)
		if !ok {
			return GatewayVideoPollTaskResponse{}, guardErr
		}
		blockedCall := baseCall
		blockedCall.Status = "blocked"
		blockedCall.ErrorCode = standard.Code
		blockedCall.ErrorMessage = standard.Message
		blockedCall.ResponseSnapshot = blockedResponseSnapshot(standard)
		blockedCall.NormalizedOutput = blockedNormalizedOutput(standard)
		call, err := recordCall(ctx, s.db, blockedCall)
		if err != nil {
			return GatewayVideoPollTaskResponse{}, err
		}
		return GatewayVideoPollTaskResponse{
			ProviderRequestID:   providerRequestID,
			AttemptGeneration:   attemptGeneration,
			ProviderCallID:      call.ID,
			ProviderAsyncTaskID: task.ID,
			ExternalTaskID:      task.ExternalTaskID,
			ModelID:             task.ProviderModelID,
			Status:              "blocked",
			Usage:               GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			Error:               standard,
		}, nil
	}
	providerCallID := ""
	defer func() {
		s.releaseGatewayLease(lease, providerCallID)
	}()

	started := time.Now()
	var result manifestRunResult
	var runErr error
	if openAICompatibleRuntime {
		client := newOpenAICompatibleClient(timeout)
		result, runErr = client.pollVideoTask(callCtx, account, selection.APIKey, openAIConfig, pollSourceTask.ExternalTaskID)
	} else {
		result, runErr = callManifestEndpointWithContext(callCtx, manifest, account, credential, endpointKey, endpoint, task.Input, videoManifestContext(selection, nil, &pollSourceTask))
	}
	latencyMS := int(time.Since(started).Milliseconds())
	if result.LatencyMS > latencyMS {
		latencyMS = result.LatencyMS
	}

	status := normalizeGatewayVideoStatus(result.Status)
	if status == "" {
		status = normalizeGatewayVideoStatus(videoStringField(result.NormalizedOutput, "status"))
	}
	if status == "" {
		status = "running"
	}
	normalizedOutput := result.NormalizedOutput
	responseSnapshot := result.ResponseSnapshot
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	var output GatewayVideoOutput
	usage := GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"}
	var stored *gatewayStoredVideo
	videoInput, _ := parseGatewayVideoInput(task.Input)
	callStatus := status
	mediaTransferPending := false

	if runErr != nil {
		outcome := normalizeGatewayVideoPollFailure(runErr)
		status = outcome.TaskStatus
		callStatus = outcome.CallStatus
		errorCode = outcome.ErrorCode
		errorMessage = outcome.ErrorMessage
		upstreamStatus = outcome.UpstreamStatus
		upstreamErrorCode = outcome.UpstreamErrorCode
		standardError = outcome.ResponseError
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
		if len(normalizedOutput) == 0 {
			normalizedOutput = mustJSON(map[string]any{
				"status":           status,
				"pollErrorCode":    errorCode,
				"pollErrorMessage": errorMessage,
			})
		}
	} else if status == "failed" {
		errorCode, errorMessage, upstreamErrorCode, standardError = normalizedVideoTerminalFailure(normalizedOutput)
	} else if status == "succeeded" {
		videoURL := videoStringField(normalizedOutput, "videoUrl", "url", "outputUrl")
		if strings.TrimSpace(videoURL) != "" {
			if s.objectStorage == nil {
				return GatewayVideoPollTaskResponse{}, fmt.Errorf("%w: object storage is not configured", ErrValidation)
			}
			var mediaErr error
			stored, mediaErr = s.downloadAndStoreGatewayVideoMedia(ctx, callID, task.OrganizationID, firstNonEmpty(req.ProjectID, task.ProjectID), selection, pollSourceTask.ExternalTaskID, result, videoInput)
			if mediaErr != nil {
				outcome := normalizeGatewayVideoMediaFailure(result.NormalizedOutput, mediaErr)
				errorCode = outcome.ErrorCode
				errorMessage = outcome.ErrorMessage
				standardError = outcome.ResponseError
				status = outcome.TaskStatus
				callStatus = outcome.CallStatus
				mediaTransferPending = outcome.TransferPending
				normalizedOutput = outcome.NormalizedOutput
			} else {
				output = stored.Output
				usage = estimateVideoCost(videoInput, output.DurationSeconds, model.Capabilities)
				normalizedOutput = mustJSON(output)
			}
		}
	}
	if runErr == nil {
		if !mediaTransferPending {
			callStatus = status
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = json.RawMessage(`{}`)
	}

	call, err := s.recordVideoPollTask(ctx, selection, req, task, providerRequestID, attemptGeneration, 1, callID, lease.LeaseID, status, callStatus, latencyMS, errorCode, errorMessage, upstreamStatus, upstreamErrorCode, result.RequestSnapshot, responseSnapshot, normalizedOutput, usage, stored, videoInput)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	providerCallID = call.ID
	if isProviderFailureStatus(callStatus) && !mediaTransferPending {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}
	return GatewayVideoPollTaskResponse{
		ProviderRequestID:   providerRequestID,
		AttemptGeneration:   attemptGeneration,
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: task.ID,
		ExternalTaskID:      task.ExternalTaskID,
		ModelID:             task.ProviderModelID,
		Status:              status,
		ExecutionPlanID:     task.ExecutionPlanID,
		RenderSegmentID:     task.RenderSegmentID,
		Output:              output,
		Usage:               usage,
		Error:               standardError,
		LatencyMS:           latencyMS,
	}, nil
}

type gatewayVideoPollFailureOutcome struct {
	TaskStatus        string
	CallStatus        string
	ErrorCode         string
	ErrorMessage      string
	UpstreamStatus    *int
	UpstreamErrorCode string
	ResponseError     *StandardError
}

func normalizeGatewayVideoPollFailure(err error) gatewayVideoPollFailureOutcome {
	status, code, message, upstreamStatus, upstreamCode := normalizedProviderFailure(err)
	outcome := gatewayVideoPollFailureOutcome{
		TaskStatus:        status,
		CallStatus:        status,
		ErrorCode:         code,
		ErrorMessage:      message,
		UpstreamStatus:    upstreamStatus,
		UpstreamErrorCode: upstreamCode,
		ResponseError:     standardErrorFromRunError(err, code, message),
	}
	if videoSegmentFailureRetryable(code) {
		// Polling only observes an existing upstream task. A transient observation
		// failure must not terminally fail it and trigger a duplicate submission.
		outcome.TaskStatus = "running"
		outcome.ResponseError = nil
	}
	return outcome
}

type gatewayVideoMediaFailureOutcome struct {
	TaskStatus       string
	CallStatus       string
	TransferPending  bool
	ErrorCode        string
	ErrorMessage     string
	ResponseError    *StandardError
	NormalizedOutput json.RawMessage
}

func normalizeGatewayVideoMediaFailure(normalizedOutput json.RawMessage, err error) gatewayVideoMediaFailureOutcome {
	code, message, standard := gatewayVideoMediaFailure(err)
	if gatewayVideoMediaTransferCanRetry(code, standard) {
		return gatewayVideoMediaFailureOutcome{
			TaskStatus:       "running",
			CallStatus:       "failed",
			TransferPending:  true,
			ErrorCode:        code,
			ErrorMessage:     message,
			ResponseError:    standard,
			NormalizedOutput: gatewayVideoMediaPendingOutput(normalizedOutput, code, message),
		}
	}
	if standard != nil {
		terminal := *standard
		terminal.Retryable = false
		standard = &terminal
	}
	return gatewayVideoMediaFailureOutcome{
		TaskStatus:       "failed",
		CallStatus:       "failed",
		ErrorCode:        code,
		ErrorMessage:     message,
		ResponseError:    standard,
		NormalizedOutput: gatewayVideoMediaTerminalOutput(normalizedOutput, code, message),
	}
}

func gatewayVideoMediaTransferCanRetry(code string, standard *StandardError) bool {
	return strings.EqualFold(strings.TrimSpace(code), CodeMediaDownloadFailed) &&
		standard != nil &&
		standard.Retryable
}

func (s *Service) CancelVideoTask(ctx context.Context, req GatewayVideoCancelTaskRequest) (GatewayVideoCancelTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoCancelTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	task, err := s.getGatewayVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:      req.OrganizationID,
		ProviderAsyncTaskID: req.ProviderAsyncTaskID,
		ExternalTaskID:      req.ExternalTaskID,
		ProviderModelID:     req.ProviderModelID,
		ProviderAccountID:   req.ProviderAccountID,
	})
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	start, err := s.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID:                 task.OrganizationID,
		ProjectID:                      task.ProjectID,
		ProductionGenerationID:         task.ProductionGenerationID,
		VideoProductionBindingID:       task.VideoProductionBindingID,
		VideoProductionBindingRevision: task.VideoProductionBindingRevision,
		WorkflowRunID:                  task.WorkflowRunID,
		NodeRunID:                      task.NodeRunID,
		OperationID:                    task.OperationID,
		OperationItemID:                task.OperationItemID,
		OperationItemAttempt:           task.OperationItemAttempt,
		ExecutionPlanID:                task.ExecutionPlanID,
		RenderSegmentID:                task.RenderSegmentID,
		TaskType:                       TaskTypeVideoCancelTask,
		IdempotencyKey:                 gatewayVideoIdempotencyKey(req.IdempotencyKey, req.Options),
		RequestHash:                    requestHash,
		Retry:                          req.Options.Retry,
	})
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayVideoCancelTaskResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerVideoCancelStatusResponse(start.Request)
		}
		response.ProviderRequestID = start.Request.ID
		response.AttemptGeneration = start.Request.AttemptGeneration
		return response, nil
	}
	if start.Disposition == providerRequestInProgress {
		return providerVideoCancelStatusResponse(start.Request), nil
	}
	response, runErr := s.executeGatewayVideoCancel(ctx, req, task, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		if errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayVideoCancelTaskResponse{ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration, Status: "failed", Error: standard}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, "failed", response, nil, nil, standard); completeErr != nil {
				return GatewayVideoCancelTaskResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayVideoCancelTaskResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.AttemptGeneration = start.Request.AttemptGeneration
	logicalStatus := "succeeded"
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		logicalStatus = "failed"
	}
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, logicalStatus, response, nil, nil, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayVideoCancelTaskResponse{}, err
	}
	return response, nil
}

func providerVideoCancelStatusResponse(request ProviderRequest) GatewayVideoCancelTaskResponse {
	return GatewayVideoCancelTaskResponse{ProviderRequestID: request.ID, AttemptGeneration: request.AttemptGeneration, Status: request.Status, Error: providerRequestStatusError(request)}
}

func (s *Service) executeGatewayVideoCancel(ctx context.Context, req GatewayVideoCancelTaskRequest, task gatewayVideoTask, providerRequestID string, attemptGeneration int) (GatewayVideoCancelTaskResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayVideoCancelTaskResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	if task.Status == "cancelled" {
		return GatewayVideoCancelTaskResponse{
			ProviderAsyncTaskID: task.ID,
			ExternalTaskID:      task.ExternalTaskID,
			Status:              "cancelled",
			ExecutionPlanID:     task.ExecutionPlanID,
			RenderSegmentID:     task.RenderSegmentID,
		}, nil
	}
	if task.Status == "succeeded" {
		return GatewayVideoCancelTaskResponse{
			ProviderAsyncTaskID: task.ID,
			ExternalTaskID:      task.ExternalTaskID,
			Status:              "succeeded",
			ExecutionPlanID:     task.ExecutionPlanID,
			RenderSegmentID:     task.RenderSegmentID,
		}, nil
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, task.ProviderAccountID)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	model, err := s.GetModel(ctx, req.OrganizationID, task.ProviderModelID)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	if strings.TrimSpace(task.CredentialID) == "" {
		return GatewayVideoCancelTaskResponse{}, fmt.Errorf("%w: provider async task has no credential identity", ErrValidation)
	}
	credential, credentialID, err := s.credentialPayloadByID(ctx, req.OrganizationID, account.ID, task.CredentialID)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	selection := gatewayModelSelection{Account: account, Model: model, CredentialID: credentialID, Credential: credential, APIKey: apiKey, ModelProfileID: task.ModelProfileID, ModelProfileBindingID: task.ModelProfileBindingID, ModelProfileKey: task.ModelProfileKey}
	openAICompatibleRuntime := usesNativeOpenAICompatibleRuntime(account)
	var manifest ProviderManifest
	var openAIConfig openAICompatibleConfig
	if openAICompatibleRuntime {
		openAIConfig = parseOpenAICompatibleConfig(account.Config)
	} else {
		manifest, err = s.manifestForAccount(ctx, account)
		if err != nil {
			return GatewayVideoCancelTaskResponse{}, err
		}
	}

	callID := uuid.NewString()
	baseCall := RecordCallRequest{
		ID:                     callID,
		ProviderRequestID:      providerRequestID,
		AttemptGeneration:      attemptGeneration,
		AttemptSequence:        1,
		OrganizationID:         task.OrganizationID,
		ProjectID:              task.ProjectID,
		ProductionGenerationID: task.ProductionGenerationID,
		OperationID:            task.OperationID,
		OperationItemID:        task.OperationItemID,
		OperationItemAttempt:   task.OperationItemAttempt,
		ExecutionPlanID:        task.ExecutionPlanID,
		RenderSegmentID:        task.RenderSegmentID,
		WorkflowRunID:          task.WorkflowRunID,
		NodeRunID:              task.NodeRunID,
		ProviderAccountID:      selection.Account.ID,
		ProviderModelID:        selection.Model.ID,
		CredentialID:           selection.CredentialID,
		ModelProfileID:         selection.ModelProfileID,
		ModelProfileBindingID:  selection.ModelProfileBindingID,
		ModelProfileKey:        selection.ModelProfileKey,
		IdempotencyKey:         gatewayVideoIdempotencyKey(req.IdempotencyKey, req.Options),
		TaskType:               TaskTypeVideoCancelTask,
		ExecutionMode:          "sync",
		Status:                 "running",
		RequestSnapshot:        task.Input,
	}
	if _, err := recordCall(ctx, s.db, baseCall); err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: task.OrganizationID,
		Selection:      selection,
		TaskType:       TaskTypeVideoCancelTask,
		EstimatedCost:  "0.00000000",
		Currency:       "USD",
		LeaseTTL:       time.Minute,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		standard, ok := blockedGatewayStandard(guardErr)
		if !ok {
			return GatewayVideoCancelTaskResponse{}, guardErr
		}
		blockedCall := baseCall
		blockedCall.Status = "blocked"
		blockedCall.ErrorCode = standard.Code
		blockedCall.ErrorMessage = standard.Message
		blockedCall.ResponseSnapshot = blockedResponseSnapshot(standard)
		blockedCall.NormalizedOutput = blockedNormalizedOutput(standard)
		call, err := recordCall(ctx, s.db, blockedCall)
		if err != nil {
			return GatewayVideoCancelTaskResponse{}, err
		}
		return GatewayVideoCancelTaskResponse{
			ProviderRequestID:   providerRequestID,
			AttemptGeneration:   attemptGeneration,
			ProviderCallID:      call.ID,
			ProviderAsyncTaskID: task.ID,
			ExternalTaskID:      task.ExternalTaskID,
			Status:              "blocked",
			ExecutionPlanID:     task.ExecutionPlanID,
			RenderSegmentID:     task.RenderSegmentID,
			Error:               standard,
		}, nil
	}
	providerCallID := ""
	defer func() {
		s.releaseGatewayLease(lease, providerCallID)
	}()

	status := "cancelled"
	latencyMS := 0
	requestSnapshot := mustJSON(map[string]any{"providerAsyncTaskId": task.ID, "externalTaskId": task.ExternalTaskID})
	responseSnapshot := json.RawMessage(`null`)
	normalizedOutput := mustJSON(map[string]any{"status": status})
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	var cancelRunErr error
	var cancelResult manifestRunResult
	cancelAttempted := false
	if openAICompatibleRuntime && strings.TrimSpace(openAIConfig.VideoCancelEndpoint) != "" {
		cancelAttempted = true
		timeout := gatewayVideoTimeout(0, openAIConfig.TimeoutMS)
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		client := newOpenAICompatibleClient(timeout)
		cancelResult, cancelRunErr = client.cancelVideoTask(callCtx, account, selection.APIKey, openAIConfig, task.ExternalTaskID)
	} else if !openAICompatibleRuntime {
		if endpointKey, endpoint, ok := selectVideoCancelEndpoint(selection, manifest); ok {
			cancelAttempted = true
			timeout := gatewayVideoTimeout(0, endpoint.TimeoutMS)
			callCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cancelResult, cancelRunErr = callManifestEndpointWithContext(callCtx, manifest, account, credential, endpointKey, endpoint, task.Input, videoManifestContext(selection, nil, &task))
		}
	}
	if cancelAttempted {
		latencyMS = cancelResult.LatencyMS
		requestSnapshot = cancelResult.RequestSnapshot
		responseSnapshot = cancelResult.ResponseSnapshot
		if len(cancelResult.NormalizedOutput) > 0 {
			normalizedOutput = cancelResult.NormalizedOutput
		}
		if cancelRunErr != nil {
			status = "failed"
			_, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(cancelRunErr)
			if errorCode == "" {
				errorCode = CodeProviderCancelFailed
			}
			standardError = &StandardError{Code: errorCode, Message: "provider video task cancel failed", Retryable: true}
			if len(responseSnapshot) == 0 {
				responseSnapshot = upstreamBody(cancelRunErr)
			}
		} else {
			status = "cancelled"
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	call, err := s.recordVideoCancelTask(ctx, selection, task, providerRequestID, attemptGeneration, 1, callID, lease.LeaseID, status, latencyMS, errorCode, errorMessage, upstreamStatus, upstreamErrorCode, requestSnapshot, responseSnapshot, normalizedOutput)
	if err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	providerCallID = call.ID
	if cancelRunErr != nil {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}
	return GatewayVideoCancelTaskResponse{
		ProviderRequestID:   providerRequestID,
		AttemptGeneration:   attemptGeneration,
		ProviderCallID:      call.ID,
		ProviderAsyncTaskID: task.ID,
		ExternalTaskID:      task.ExternalTaskID,
		Status:              status,
		ExecutionPlanID:     task.ExecutionPlanID,
		RenderSegmentID:     task.RenderSegmentID,
		Error:               standardError,
	}, nil
}

func (s *Service) selectGatewayVideoModel(ctx context.Context, organizationID, providerModelID, modelProfileKey string) (gatewayModelSelection, error) {
	if strings.TrimSpace(providerModelID) != "" {
		model, err := s.GetModel(ctx, organizationID, providerModelID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		if model.Status != "active" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
		}
		if model.Modality != "video" && model.Modality != "multimodal" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model does not support video generation", ErrValidation)
		}
		if !modelSupportsTaskType(model, TaskTypeVideoCreateTask) {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model does not support %s", ErrValidation, TaskTypeVideoCreateTask)
		}
		account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		return s.completeGatewaySelection(ctx, organizationID, account, model, "", "", "")
	}
	profileKey := strings.TrimSpace(modelProfileKey)
	if profileKey == "" {
		return gatewayModelSelection{}, fmt.Errorf("%w: modelProfileKey or providerModelId is required", ErrValidation)
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:  organizationID,
		ModelProfileKey: profileKey,
		TaskType:        TaskTypeVideoCreateTask,
		Modality:        "video",
	})
	if err != nil {
		return gatewayModelSelection{}, err
	}
	return s.completeGatewaySelectionFromCandidate(ctx, organizationID, candidates[0])
}

func parseGatewayVideoInput(input json.RawMessage) (gatewayVideoInput, error) {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return gatewayVideoInput{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	prompt, _ := decoded["prompt"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return gatewayVideoInput{}, fmt.Errorf("%w: input.prompt is required", ErrValidation)
	}
	return gatewayVideoInput{
		Prompt:          prompt,
		DurationSeconds: floatField(decoded["duration"], "duration"),
		AspectRatio:     videoStringOption(decoded, "aspectRatio"),
		Resolution:      videoStringOption(decoded, "resolution"),
		Mode:            videoStringOption(decoded, "mode"),
	}, nil
}

func videoTaskInputWithPromptTrace(input json.RawMessage, promptTemplateKey, promptSource string) json.RawMessage {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil || decoded == nil {
		return input
	}
	if strings.TrimSpace(promptTemplateKey) != "" {
		decoded["promptTemplateKey"] = strings.TrimSpace(promptTemplateKey)
	}
	if strings.TrimSpace(promptSource) != "" {
		decoded["promptSource"] = strings.TrimSpace(promptSource)
	}
	raw, err := json.Marshal(decoded)
	if err != nil {
		return input
	}
	return raw
}

func (s *Service) manifestForAccount(ctx context.Context, account Account) (ProviderManifest, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, `
		SELECT c.manifest
		FROM provider_connectors c
		WHERE c.id = $1
	`, account.ConnectorID).Scan(&raw)
	if err != nil {
		return ProviderManifest{}, err
	}
	manifest, _, err := ParseManifest(raw, "")
	return manifest, err
}

func selectVideoCreateEndpoint(selection gatewayModelSelection, manifest ProviderManifest) (string, ManifestEndpoint, error) {
	return firstManifestEndpoint(manifest, "async_create", []string{
		accountConfigString(selection.Account.Config, "videoCreateEndpointKey"),
		modelProviderOptionString(selection.Model, "videoCreateEndpointKey"),
		"video_generate",
		"video_create",
		"createVideo",
	})
}

func selectVideoPollEndpoint(selection gatewayModelSelection, manifest ProviderManifest, createEndpoint ManifestEndpoint) (string, ManifestEndpoint, error) {
	return firstManifestEndpoint(manifest, "async_poll", []string{
		strings.TrimSpace(createEndpoint.PollEndpointKey),
		accountConfigString(selection.Account.Config, "videoPollEndpointKey"),
		modelProviderOptionString(selection.Model, "videoPollEndpointKey"),
		"video_poll",
		"pollVideo",
		"poll",
	})
}

func selectVideoCancelEndpoint(selection gatewayModelSelection, manifest ProviderManifest) (string, ManifestEndpoint, bool) {
	key, endpoint, err := firstManifestEndpoint(manifest, "", []string{
		accountConfigString(selection.Account.Config, "videoCancelEndpointKey"),
		modelProviderOptionString(selection.Model, "videoCancelEndpointKey"),
		"video_cancel",
		"cancelVideo",
		"cancel",
	})
	return key, endpoint, err == nil
}

func firstManifestEndpoint(manifest ProviderManifest, wantType string, keys []string) (string, ManifestEndpoint, error) {
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		endpoint, ok := manifest.Endpoints[key]
		if !ok {
			continue
		}
		if wantType != "" && endpointType(endpoint.EndpointType) != wantType {
			continue
		}
		return key, endpoint, nil
	}
	return "", ManifestEndpoint{}, fmt.Errorf("%w: video manifest endpoint was not found", ErrValidation)
}

func videoManifestContext(selection gatewayModelSelection, references []GatewayVideoReference, task *gatewayVideoTask) manifestCallContext {
	refValues := make([]map[string]any, 0, len(references))
	for _, ref := range references {
		refValues = append(refValues, map[string]any{
			"role":        gatewayVideoReferenceRole(ref),
			"type":        ref.Type,
			"assetId":     ref.AssetID,
			"artifactId":  ref.ArtifactID,
			"mediaFileId": ref.MediaFileID,
			"url":         ref.URL,
			"storageKey":  ref.StorageKey,
			"mimeType":    ref.MimeType,
			"metadata":    rawJSONValue(ref.Metadata),
		})
	}
	if len(refValues) == 0 {
		refValues = append(refValues, map[string]any{
			"role":        "",
			"type":        "",
			"assetId":     "",
			"artifactId":  "",
			"mediaFileId": "",
			"url":         "",
			"storageKey":  "",
			"mimeType":    "",
			"metadata":    map[string]any{},
		})
	}
	taskValue := map[string]any{}
	if task != nil {
		taskValue = map[string]any{
			"externalTaskId":      task.ExternalTaskID,
			"providerAsyncTaskId": task.ID,
		}
	}
	baseURL := ""
	if selection.Account.BaseURL != nil {
		baseURL = *selection.Account.BaseURL
	}
	return manifestCallContext{
		References: refValues,
		Model: map[string]any{
			"id":          selection.Model.ModelKey,
			"displayName": selection.Model.DisplayName,
			"modality":    selection.Model.Modality,
		},
		Account: map[string]any{
			"baseUrl":  baseURL,
			"authType": selection.Account.AuthType,
		},
		Task: taskValue,
	}
}

func (s *Service) storeGatewayVideoMedia(ctx context.Context, callID, organizationID, projectID string, selection gatewayModelSelection, externalTaskID string, result manifestRunResult, media gatewayVideoMedia, input gatewayVideoInput) (*gatewayStoredVideo, error) {
	storageKey := gatewayVideoStorageKey(organizationID, projectID, media.MimeType, videoStringField(result.NormalizedOutput, "videoUrl", "url", "outputUrl"))
	providerDuration := videoFloatField(result.NormalizedOutput, "durationSeconds", "duration")
	probe := GatewayVideoMediaProbe{Status: "unavailable", AudioCodecs: []string{}}
	if s.videoMediaProbe != nil || s.videoMediaFileProbe != nil {
		var observed GatewayVideoMediaProbe
		var probeErr error
		if strings.TrimSpace(media.TempPath) != "" && s.videoMediaFileProbe != nil {
			observed, probeErr = s.videoMediaFileProbe(ctx, media.TempPath)
		} else {
			body := media.Body
			if len(body) == 0 && strings.TrimSpace(media.TempPath) != "" {
				body, probeErr = readGatewayMediaFile(media.TempPath, gatewayVideoMaxBytes())
			}
			if probeErr == nil {
				observed, probeErr = s.videoMediaProbe(ctx, body, media.MimeType)
			}
		}
		probe = observed
		probe.AudioCodecs = append([]string(nil), observed.AudioCodecs...)
		if probeErr != nil {
			probe.Status = "failed"
			probe.Error = strings.TrimSpace(probeErr.Error())
		} else {
			probe.Status = "succeeded"
		}
	}
	if probe.Status == "succeeded" {
		expectedAspectRatio := strings.TrimSpace(input.AspectRatio)
		if expectedAspectRatio == "" {
			if width, height, ok := parseVideoDimensions(input.Resolution); ok {
				expectedAspectRatio = fmt.Sprintf("%d:%d", width, height)
			}
		}
		if err := validateVideoOutputLayout(expectedAspectRatio, probe.Width, probe.Height); err != nil {
			return nil, err
		}
	}
	if err := s.assertProviderProjectWritable(ctx, organizationID, projectID); err != nil {
		return nil, err
	}
	var put storage.PutResult
	var err error
	if strings.TrimSpace(media.TempPath) != "" {
		put, err = s.objectStorage.PutFile(ctx, storageKey, media.TempPath, media.MimeType)
	} else {
		put, err = s.objectStorage.PutBytes(ctx, storageKey, media.Body, media.MimeType)
	}
	if err != nil {
		return nil, err
	}
	media.ContentHash = put.ContentHash
	if media.ContentHash == "" {
		if len(media.Body) > 0 {
			media.ContentHash = sha256ContentHash(media.Body)
		}
	}
	if media.ByteSize == 0 {
		media.ByteSize = put.ByteSize
	}
	actualDuration := probe.DurationSeconds
	duration := firstPositiveFloat(actualDuration, providerDuration, input.DurationSeconds)
	durationSource := ""
	switch {
	case actualDuration > 0:
		durationSource = "media_probe"
	case providerDuration > 0:
		durationSource = "provider"
	case input.DurationSeconds > 0:
		durationSource = "request"
	}
	durationPtr := float64PtrIfPositive(duration)
	requestedDurationPtr := float64PtrIfPositive(input.DurationSeconds)
	providerDurationPtr := float64PtrIfPositive(providerDuration)
	actualDurationPtr := float64PtrIfPositive(actualDuration)
	artifactID := uuid.NewString()
	mediaFileID := uuid.NewString()
	output := GatewayVideoOutput{
		ArtifactID:               artifactID,
		MediaFileID:              mediaFileID,
		StorageKey:               put.StorageKey,
		URL:                      videoStringField(result.NormalizedOutput, "videoUrl", "url", "outputUrl"),
		MimeType:                 media.MimeType,
		ByteSize:                 &media.ByteSize,
		DurationSeconds:          durationPtr,
		RequestedDurationSeconds: requestedDurationPtr,
		ProviderDurationSeconds:  providerDurationPtr,
		ActualDurationSeconds:    actualDurationPtr,
		DurationSource:           durationSource,
		Width:                    intPtrIfPositive(probe.Width),
		Height:                   intPtrIfPositive(probe.Height),
		MediaProbe:               &probe,
		Raw:                      result.NormalizedOutput,
	}
	return &gatewayStoredVideo{ArtifactID: artifactID, MediaFileID: mediaFileID, Output: output, Media: media}, nil
}

func (s *Service) downloadAndStoreGatewayVideoMedia(ctx context.Context, callID, organizationID, projectID string, selection gatewayModelSelection, externalTaskID string, result manifestRunResult, input gatewayVideoInput) (*gatewayStoredVideo, error) {
	transferTimeout := gatewayVideoMediaTimeoutFromEnv()
	transferCtx, cancel := context.WithTimeout(ctx, transferTimeout)
	defer cancel()
	videoURL := videoStringField(result.NormalizedOutput, "videoUrl", "url", "outputUrl")
	media, err := s.downloadGatewayVideoURL(transferCtx, selection, externalTaskID, videoURL, videoStringField(result.NormalizedOutput, "mimeType"), transferTimeout)
	if err != nil {
		return nil, gatewayMediaStageFailure("download", err)
	}
	defer media.close()
	stored, err := s.storeGatewayVideoMedia(transferCtx, callID, organizationID, projectID, selection, externalTaskID, result, media, input)
	if err != nil {
		return nil, gatewayMediaStageFailure("storage", err)
	}
	return stored, nil
}

func gatewayVideoMediaPendingOutput(normalizedOutput json.RawMessage, errorCode, errorMessage string) json.RawMessage {
	var output map[string]any
	if err := json.Unmarshal(normalizedOutput, &output); err != nil || output == nil {
		output = map[string]any{}
	}
	output["status"] = "running"
	output["mediaTransferStatus"] = "pending"
	output["mediaTransferErrorCode"] = strings.TrimSpace(errorCode)
	output["mediaTransferErrorMessage"] = strings.TrimSpace(errorMessage)
	return mustJSON(output)
}

func gatewayVideoMediaTerminalOutput(normalizedOutput json.RawMessage, errorCode, errorMessage string) json.RawMessage {
	var output map[string]any
	if err := json.Unmarshal(normalizedOutput, &output); err != nil || output == nil {
		output = map[string]any{}
	}
	output["status"] = "failed"
	output["mediaTransferStatus"] = "failed"
	output["errorCode"] = strings.TrimSpace(errorCode)
	output["errorMessage"] = strings.TrimSpace(errorMessage)
	return mustJSON(output)
}

func gatewayVideoMediaFailure(err error) (string, string, *StandardError) {
	return normalizedGatewayMediaFailure(err, "video")
}

func (s *Service) recordVideoCreateTask(ctx context.Context, selection gatewayModelSelection, req GatewayVideoCreateTaskRequest, providerRequestID string, attemptGeneration, attemptSequence int, callID, leaseID, externalTaskID, status string, latencyMS int, errorCode, errorMessage string, upstreamStatus *int, upstreamErrorCode string, requestSnapshot, responseSnapshot, normalizedOutput json.RawMessage, usage GatewayUsage, stored *gatewayStoredVideo, input gatewayVideoInput) (CallLog, string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, "", err
	}
	defer tx.Rollback(ctx)
	if err := assertGatewayVideoProductionIdentityTx(ctx, tx, req.OrganizationID, req.ProjectID, videoProductionIdentity(
		req.ProductionGenerationID, req.VideoProductionBindingID, req.VideoProductionBindingRevision,
	)); err != nil {
		return CallLog{}, "", err
	}
	if err := lockGatewayVideoNodeExecutionTx(ctx, tx, req.NodeRunID, req.NodeExecutionToken, req.NodeAttemptGeneration); err != nil {
		return CallLog{}, "", err
	}
	taskID := uuid.NewString()
	if stored != nil {
		if err := insertGatewayVideoArtifact(ctx, tx, selection, req.OrganizationID, req.ProjectID, req.ProductionGenerationID, req.WorkflowRunID, req.NodeRunID, req.PromptTemplateKey, req.PromptVersionID, req.PromptHash, req.PromptSource, callID, taskID, externalTaskID, stored, input); err != nil {
			return CallLog{}, "", err
		}
		if err := insertGatewayVideoMediaFile(ctx, tx, req.OrganizationID, req.ProjectID, req.ProductionGenerationID, callID, taskID, externalTaskID, selection.Model.ID, stored); err != nil {
			return CallLog{}, "", err
		}
	}
	requestedDuration := float64PtrIfPositive(input.DurationSeconds)
	actualDuration, mediaProbe := videoMediaObservation(stored)
	callTaskType, callExecutionMode, taskExecutionMode, recoveryCallStatus := gatewayVideoTaskRecordingMode(req.Input)
	callStatus := status
	if recoveryCallStatus != "" {
		callStatus = recoveryCallStatus
	}
	callReq := RecordCallRequest{
		ID:                       callID,
		ProviderRequestID:        providerRequestID,
		AttemptGeneration:        attemptGeneration,
		AttemptSequence:          attemptSequence,
		OrganizationID:           req.OrganizationID,
		ProjectID:                req.ProjectID,
		ProductionGenerationID:   req.ProductionGenerationID,
		OperationID:              req.OperationID,
		OperationItemID:          req.OperationItemID,
		OperationItemAttempt:     req.OperationItemAttempt,
		ExecutionPlanID:          req.ExecutionPlanID,
		RenderSegmentID:          req.RenderSegmentID,
		WorkflowRunID:            req.WorkflowRunID,
		NodeRunID:                req.NodeRunID,
		ProviderAccountID:        selection.Account.ID,
		ProviderModelID:          selection.Model.ID,
		CredentialID:             selection.CredentialID,
		ModelProfileID:           selection.ModelProfileID,
		ModelProfileBindingID:    selection.ModelProfileBindingID,
		ModelProfileKey:          selection.ModelProfileKey,
		PromptVersionID:          req.PromptVersionID,
		PromptHash:               req.PromptHash,
		LeaseID:                  leaseID,
		IdempotencyKey:           gatewayVideoIdempotencyKey(req.IdempotencyKey, req.Options),
		TaskType:                 callTaskType,
		ExecutionMode:            callExecutionMode,
		Status:                   callStatus,
		LatencyMS:                &latencyMS,
		RequestedDurationSeconds: requestedDuration,
		ActualDurationSeconds:    actualDuration,
		MediaProbe:               mediaProbe,
		EstimatedCost:            usage.EstimatedCost,
		Currency:                 usage.Currency,
		ErrorCode:                errorCode,
		ErrorMessage:             errorMessage,
		UpstreamStatus:           upstreamStatus,
		UpstreamErrorCode:        upstreamErrorCode,
		RequestSnapshot:          requestSnapshot,
		ResponseSnapshot:         responseSnapshot,
		NormalizedOutput:         normalizedOutput,
	}
	if stored != nil {
		callReq.ArtifactIDs = mustJSON([]string{stored.ArtifactID})
		callReq.MediaFileIDs = mustJSON([]string{stored.MediaFileID})
	}
	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, "", err
	}
	if isProviderFailureStatus(status) {
		if err := updateVideoRenderSegmentCreateTx(ctx, tx, req, call.ID, "", externalTaskID, status, errorCode, errorMessage, stored); err != nil {
			return CallLog{}, "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return CallLog{}, "", err
		}
		return call, "", nil
	}
	taskInput := videoTaskInputWithPromptTrace(req.Input, req.PromptTemplateKey, req.PromptSource)
	if err := tx.QueryRow(ctx, `
		INSERT INTO provider_async_tasks(
			id,
			provider_call_id, provider_request_id, organization_id, project_id, workflow_run_id, node_run_id,
			production_generation_id, video_production_binding_id, video_production_binding_revision,
			operation_id, operation_item_id, operation_item_attempt,
			provider_account_id, provider_model_id, credential_id, model_profile_id, model_profile_binding_id, model_profile_key,
			external_task_id, task_type, status, execution_mode, input, normalized_output, last_response_snapshot,
			error_code, error_message, poll_count, next_poll_at, started_at, completed_at, cancelled_at, raw_status,
			request_hash,
			requested_duration_seconds, actual_duration_seconds, media_probe,
			video_render_plan_id, video_render_segment_id, video_variant_key, capability_snapshot_hash,
			node_execution_token, node_attempt_generation
		)
		VALUES (
			$1,
			$2, NULLIF($3, '')::uuid, $4, $5, $6, $7,
			NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, NULLIF($10, 0),
			NULLIF($11, '')::uuid, NULLIF($12, '')::uuid, NULLIF($13, 0),
			$14, $15, $16, $17, $18, $19,
			$20, 'video.generate', $21, $35, $22, $23, $24,
			$25, $26, 0, NULL, now(),
			CASE WHEN $21 IN ('succeeded', 'failed') THEN now() ELSE NULL END,
			CASE WHEN $21 = 'cancelled' THEN now() ELSE NULL END,
			$23,
			(SELECT request_hash FROM provider_requests WHERE id = NULLIF($3, '')::uuid),
			$27, $28, $29,
			NULLIF($30, '')::uuid, NULLIF($31, '')::uuid,
			(SELECT variant_key FROM video_render_plans WHERE id = NULLIF($30, '')::uuid), NULLIF($32, ''),
			NULLIF($33, '')::uuid, NULLIF($34, 0)
		)
		RETURNING id
	`, taskID, call.ID, providerRequestID, req.OrganizationID, nullString(req.ProjectID), nullString(req.WorkflowRunID), nullString(req.NodeRunID),
		req.ProductionGenerationID, req.VideoProductionBindingID, req.VideoProductionBindingRevision,
		req.OperationID, req.OperationItemID, req.OperationItemAttempt,
		selection.Account.ID, selection.Model.ID, selection.CredentialID, nullString(selection.ModelProfileID), nullString(selection.ModelProfileBindingID), nullString(selection.ModelProfileKey), nullString(externalTaskID), status, taskInput, nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot), nullString(errorCode), nullString(errorMessage), nullFloat(requestedDuration), nullFloat(actualDuration), mediaProbe, req.ExecutionPlanID, req.RenderSegmentID, req.CapabilitySnapshotHash, req.NodeExecutionToken, req.NodeAttemptGeneration, taskExecutionMode).Scan(&taskID); err != nil {
		return CallLog{}, "", err
	}
	if err := updateVideoRenderSegmentCreateTx(ctx, tx, req, call.ID, taskID, externalTaskID, status, errorCode, errorMessage, stored); err != nil {
		return CallLog{}, "", err
	}
	if stored != nil {
		if err := insertVideoCostRecord(ctx, tx, call.ID, selection, req.OrganizationID, req.ProjectID, req.ProductionGenerationID, req.WorkflowRunID, req.NodeRunID, taskID, externalTaskID, gatewayVideoExecutionProvenance{
			OperationID: req.OperationID, OperationItemID: req.OperationItemID, OperationItemAttempt: req.OperationItemAttempt,
			ExecutionPlanID: req.ExecutionPlanID, RenderSegmentID: req.RenderSegmentID,
		}, usage, input, stored.Output.DurationSeconds); err != nil {
			return CallLog{}, "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, "", err
	}
	return call, taskID, nil
}

func (s *Service) recordVideoPollTask(ctx context.Context, selection gatewayModelSelection, req GatewayVideoPollTaskRequest, task gatewayVideoTask, providerRequestID string, attemptGeneration, attemptSequence int, callID, leaseID, taskStatus, callStatus string, latencyMS int, errorCode, errorMessage string, upstreamStatus *int, upstreamErrorCode string, requestSnapshot, responseSnapshot, normalizedOutput json.RawMessage, usage GatewayUsage, stored *gatewayStoredVideo, input gatewayVideoInput) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)
	if err := assertGatewayVideoProductionIdentityTx(ctx, tx, task.OrganizationID, task.ProjectID, videoProductionIdentity(
		task.ProductionGenerationID, task.VideoProductionBindingID, task.VideoProductionBindingRevision,
	)); err != nil {
		return CallLog{}, err
	}
	if err := lockGatewayVideoNodeExecutionTx(ctx, tx, task.NodeRunID, task.NodeExecutionToken, task.NodeAttemptGeneration); err != nil {
		return CallLog{}, err
	}
	projectID := firstNonEmpty(req.ProjectID, task.ProjectID)
	workflowRunID := firstNonEmpty(req.WorkflowRunID, task.WorkflowRunID)
	nodeRunID := firstNonEmpty(req.NodeRunID, task.NodeRunID)
	if stored != nil {
		if err := insertGatewayVideoArtifact(ctx, tx, selection, task.OrganizationID, projectID, task.ProductionGenerationID, workflowRunID, nodeRunID, task.PromptTemplateKey, task.PromptVersionID, task.PromptHash, task.PromptSource, callID, task.ID, task.ExternalTaskID, stored, input); err != nil {
			return CallLog{}, err
		}
		if err := insertGatewayVideoMediaFile(ctx, tx, task.OrganizationID, projectID, task.ProductionGenerationID, callID, task.ID, task.ExternalTaskID, selection.Model.ID, stored); err != nil {
			return CallLog{}, err
		}
	}
	requestedDuration := float64PtrIfPositive(input.DurationSeconds)
	actualDuration, mediaProbe := videoMediaObservation(stored)
	callReq := RecordCallRequest{
		ID:                       callID,
		ProviderRequestID:        providerRequestID,
		AttemptGeneration:        attemptGeneration,
		AttemptSequence:          attemptSequence,
		OrganizationID:           task.OrganizationID,
		ProjectID:                projectID,
		ProductionGenerationID:   task.ProductionGenerationID,
		OperationID:              task.OperationID,
		OperationItemID:          task.OperationItemID,
		OperationItemAttempt:     task.OperationItemAttempt,
		ExecutionPlanID:          task.ExecutionPlanID,
		RenderSegmentID:          task.RenderSegmentID,
		WorkflowRunID:            workflowRunID,
		NodeRunID:                nodeRunID,
		ProviderAccountID:        selection.Account.ID,
		ProviderModelID:          selection.Model.ID,
		CredentialID:             selection.CredentialID,
		ModelProfileID:           selection.ModelProfileID,
		ModelProfileBindingID:    selection.ModelProfileBindingID,
		ModelProfileKey:          selection.ModelProfileKey,
		PromptVersionID:          task.PromptVersionID,
		PromptHash:               task.PromptHash,
		LeaseID:                  leaseID,
		TaskType:                 TaskTypeVideoPollTask,
		ExecutionMode:            "async_poll",
		Status:                   callStatus,
		LatencyMS:                &latencyMS,
		RequestedDurationSeconds: requestedDuration,
		ActualDurationSeconds:    actualDuration,
		MediaProbe:               mediaProbe,
		EstimatedCost:            usage.EstimatedCost,
		Currency:                 usage.Currency,
		ErrorCode:                errorCode,
		ErrorMessage:             errorMessage,
		UpstreamStatus:           upstreamStatus,
		UpstreamErrorCode:        upstreamErrorCode,
		RequestSnapshot:          requestSnapshot,
		ResponseSnapshot:         responseSnapshot,
		NormalizedOutput:         normalizedOutput,
	}
	if stored != nil {
		callReq.ArtifactIDs = mustJSON([]string{stored.ArtifactID})
		callReq.MediaFileIDs = mustJSON([]string{stored.MediaFileID})
	}
	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, err
	}
	taskErrorCode := errorCode
	taskErrorMessage := errorMessage
	if !isProviderFailureStatus(taskStatus) {
		taskErrorCode = ""
		taskErrorMessage = ""
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_async_tasks
		SET status = $2,
		    normalized_output = $3,
		    last_response_snapshot = $4,
		    raw_status = $3,
		    error_code = $5,
		    error_message = $6,
		    poll_count = poll_count + 1,
		    last_poll_at = now(),
		    completed_at = CASE WHEN $2 IN ('succeeded', 'failed') THEN COALESCE(completed_at, now()) ELSE completed_at END,
		    cancelled_at = CASE WHEN $2 = 'cancelled' THEN COALESCE(cancelled_at, now()) ELSE cancelled_at END,
		    finalized_at = CASE WHEN $2 IN ('succeeded', 'failed', 'cancelled') THEN COALESCE(finalized_at, now()) ELSE finalized_at END
		    , requested_duration_seconds = COALESCE(requested_duration_seconds, $7)
		    , actual_duration_seconds = COALESCE($8, actual_duration_seconds)
		    , media_probe = CASE WHEN $9::jsonb = '{}'::jsonb THEN media_probe ELSE $9::jsonb END
		WHERE id = $1
		  AND (
		    node_run_id IS NULL
		    OR (node_execution_token = NULLIF($10, '')::uuid AND node_attempt_generation = NULLIF($11, 0))
		  )
	`, task.ID, taskStatus, nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot), nullString(taskErrorCode), nullString(taskErrorMessage), nullFloat(requestedDuration), nullFloat(actualDuration), mediaProbe, task.NodeExecutionToken, task.NodeAttemptGeneration); err != nil {
		return CallLog{}, err
	}
	if err := updateVideoRenderSegmentPollTx(ctx, tx, task, call.ID, taskStatus, taskErrorCode, taskErrorMessage, stored); err != nil {
		return CallLog{}, err
	}
	if stored != nil {
		if err := insertVideoCostRecord(ctx, tx, call.ID, selection, task.OrganizationID, projectID, task.ProductionGenerationID, workflowRunID, nodeRunID, task.ID, task.ExternalTaskID, gatewayVideoExecutionProvenance{
			OperationID: task.OperationID, OperationItemID: task.OperationItemID, OperationItemAttempt: task.OperationItemAttempt,
			ExecutionPlanID: task.ExecutionPlanID, RenderSegmentID: task.RenderSegmentID,
		}, usage, input, stored.Output.DurationSeconds); err != nil {
			return CallLog{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func (s *Service) recordVideoCancelTask(ctx context.Context, selection gatewayModelSelection, task gatewayVideoTask, providerRequestID string, attemptGeneration, attemptSequence int, callID, leaseID, status string, latencyMS int, errorCode, errorMessage string, upstreamStatus *int, upstreamErrorCode string, requestSnapshot, responseSnapshot, normalizedOutput json.RawMessage) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)
	call, err := recordCall(ctx, tx, RecordCallRequest{
		ID:                     callID,
		ProviderRequestID:      providerRequestID,
		AttemptGeneration:      attemptGeneration,
		AttemptSequence:        attemptSequence,
		OrganizationID:         task.OrganizationID,
		ProjectID:              task.ProjectID,
		ProductionGenerationID: task.ProductionGenerationID,
		OperationID:            task.OperationID,
		OperationItemID:        task.OperationItemID,
		OperationItemAttempt:   task.OperationItemAttempt,
		ExecutionPlanID:        task.ExecutionPlanID,
		RenderSegmentID:        task.RenderSegmentID,
		WorkflowRunID:          task.WorkflowRunID,
		NodeRunID:              task.NodeRunID,
		ProviderAccountID:      selection.Account.ID,
		ProviderModelID:        selection.Model.ID,
		CredentialID:           selection.CredentialID,
		ModelProfileID:         selection.ModelProfileID,
		ModelProfileBindingID:  selection.ModelProfileBindingID,
		ModelProfileKey:        selection.ModelProfileKey,
		LeaseID:                leaseID,
		TaskType:               TaskTypeVideoCancelTask,
		ExecutionMode:          "sync",
		Status:                 status,
		LatencyMS:              &latencyMS,
		ErrorCode:              errorCode,
		ErrorMessage:           errorMessage,
		UpstreamStatus:         upstreamStatus,
		UpstreamErrorCode:      upstreamErrorCode,
		RequestSnapshot:        requestSnapshot,
		ResponseSnapshot:       responseSnapshot,
		NormalizedOutput:       normalizedOutput,
	})
	if err != nil {
		return CallLog{}, err
	}
	if status == "cancelled" {
		if _, err := tx.Exec(ctx, `
			UPDATE provider_async_tasks
			SET status = 'cancelled',
			    cancelled_at = COALESCE(cancelled_at, now()),
			    finalized_at = COALESCE(finalized_at, now()),
			    error_code = $2,
			    error_message = $3,
			    normalized_output = $4,
			    last_response_snapshot = $5
			WHERE id = $1
		`, task.ID, nullString(errorCode), nullString(errorMessage), nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot)); err != nil {
			return CallLog{}, err
		}
		if err := insertProviderVideoCancelEvent(ctx, tx, task, call.ID, "provider.video.task.cancelled", "cancelled", ""); err != nil {
			return CallLog{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE provider_async_tasks
			SET error_code = $2,
			    error_message = $3,
			    normalized_output = $4,
			    last_response_snapshot = $5
			WHERE id = $1
		`, task.ID, nullString(errorCode), nullString(errorMessage), nullIfJSONNull(normalizedOutput), nullIfJSONNull(responseSnapshot)); err != nil {
			return CallLog{}, err
		}
		if err := insertProviderVideoCancelEvent(ctx, tx, task, call.ID, "provider.video.task.cancel_failed", status, errorMessage); err != nil {
			return CallLog{}, err
		}
	}
	if err := updateVideoRenderSegmentCancelTx(ctx, tx, task, call.ID, status, errorMessage); err != nil {
		return CallLog{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func insertProviderVideoCancelEvent(ctx context.Context, tx pgx.Tx, task gatewayVideoTask, providerCallID, eventType, status, errorMessage string) error {
	return events.AppendTx(ctx, tx, task.OrganizationID, task.ProjectID, eventType, "provider_async_task", task.ID, mustJSON(map[string]any{
		"workflowRunId":       task.WorkflowRunID,
		"nodeRunId":           task.NodeRunID,
		"providerAsyncTaskId": task.ID,
		"externalTaskId":      task.ExternalTaskID,
		"providerCallId":      providerCallID,
		"status":              status,
		"errorMessage":        errorMessage,
	}))
}

func insertGatewayVideoArtifact(ctx context.Context, tx pgx.Tx, selection gatewayModelSelection, organizationID, projectID, productionGenerationID, workflowRunID, nodeRunID, promptTemplateKey, promptVersionID, promptHash, promptSource, callID, providerAsyncTaskID, externalTaskID string, stored *gatewayStoredVideo, input gatewayVideoInput) error {
	metadata := mustJSON(map[string]any{
		"source":                   "provider_gateway",
		"providerCallId":           callID,
		"providerAsyncTaskId":      providerAsyncTaskID,
		"externalTaskId":           externalTaskID,
		"providerModelId":          selection.Model.ID,
		"mediaFileId":              stored.MediaFileID,
		"prompt":                   input.Prompt,
		"promptTemplateKey":        promptTemplateKey,
		"promptVersionId":          promptVersionID,
		"promptHash":               promptHash,
		"promptSource":             promptSource,
		"requestedDurationSeconds": stored.Output.RequestedDurationSeconds,
		"providerDurationSeconds":  stored.Output.ProviderDurationSeconds,
		"actualDurationSeconds":    stored.Output.ActualDurationSeconds,
		"durationSeconds":          stored.Output.DurationSeconds,
		"durationSource":           stored.Output.DurationSource,
		"mediaProbe":               stored.Output.MediaProbe,
		"aspectRatio":              input.AspectRatio,
		"resolution":               input.Resolution,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO artifacts(
			id, organization_id, project_id, production_generation_id, workflow_run_id, node_run_id, type,
			storage_key, mime_type, content_hash, prompt_hash, model_id, metadata, created_by
		)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, 'generated_video', $7, $8, $9, $10, $11, $12, NULL)
	`, stored.ArtifactID, organizationID, nullString(projectID), productionGenerationID, nullString(workflowRunID), nullString(nodeRunID), stored.Output.StorageKey, stored.Output.MimeType, stored.Media.ContentHash, nullString(promptHash), selection.Model.ID, metadata)
	return err
}

func insertGatewayVideoMediaFile(ctx context.Context, tx pgx.Tx, organizationID, projectID, productionGenerationID, callID, providerAsyncTaskID, externalTaskID, providerModelID string, stored *gatewayStoredVideo) error {
	metadata := mustJSON(map[string]any{
		"source":                   "provider_gateway",
		"providerCallId":           callID,
		"providerAsyncTaskId":      providerAsyncTaskID,
		"externalTaskId":           externalTaskID,
		"providerModelId":          providerModelID,
		"requestedDurationSeconds": stored.Output.RequestedDurationSeconds,
		"providerDurationSeconds":  stored.Output.ProviderDurationSeconds,
		"actualDurationSeconds":    stored.Output.ActualDurationSeconds,
		"durationSource":           stored.Output.DurationSource,
		"mediaProbe":               stored.Output.MediaProbe,
		"upstream":                 map[string]any{"responseType": "url"},
	})
	var probe GatewayVideoMediaProbe
	if stored.Output.MediaProbe != nil {
		probe = *stored.Output.MediaProbe
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO media_files(
			id, organization_id, project_id, production_generation_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, duration_seconds, checksum, created_by, metadata,
			frame_rate_numerator, frame_rate_denominator, frame_count,
			video_stream_count, audio_stream_count, media_probe
		)
		VALUES (
			$1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7,
			$8, $9, $10, $11, $12, NULL, $13,
			$14, $15, $16, $17, $18, $19
		)
	`, stored.MediaFileID, organizationID, nullString(projectID), productionGenerationID, stored.ArtifactID, stored.Output.StorageKey, stored.Output.MimeType,
		stored.Media.ByteSize, stored.Output.Width, stored.Output.Height, nullFloat(stored.Output.DurationSeconds), stored.Media.ContentHash, metadata,
		nullInt64Value(probe.FrameRateNumerator), nullInt64Value(probe.FrameRateDenominator), nullInt64Value(probe.FrameCount),
		nullIntValue(probe.VideoStreamCount), nullIntValue(probe.AudioStreamCount), mustJSON(probe))
	return err
}

func insertVideoCostRecord(ctx context.Context, tx pgx.Tx, providerCallID string, selection gatewayModelSelection, organizationID, projectID, productionGenerationID, workflowRunID, nodeRunID, providerAsyncTaskID, externalTaskID string, provenance gatewayVideoExecutionProvenance, usage GatewayUsage, input gatewayVideoInput, durationSeconds *float64) error {
	quantity := input.DurationSeconds
	if durationSeconds != nil && *durationSeconds > 0 {
		quantity = *durationSeconds
	}
	metadata := mustJSON(map[string]any{
		"durationSeconds":     quantity,
		"resolution":          input.Resolution,
		"aspectRatio":         input.AspectRatio,
		"providerAsyncTaskId": providerAsyncTaskID,
		"externalTaskId":      externalTaskID,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO cost_records(
			organization_id, project_id, production_generation_id, workflow_run_id, node_run_id,
			operation_id, operation_item_id, operation_item_attempt, video_render_plan_id, video_render_segment_id,
			provider_call_id, provider_model_id, credential_id, model_profile_id,
			cost_type, amount, currency, unit, quantity, metadata
		)
		VALUES (
			$1, $2, NULLIF($3, '')::uuid, $4, $5,
			NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, NULLIF($8, 0), NULLIF($9, '')::uuid, NULLIF($10, '')::uuid,
			$11, $12, $13, $14, 'video.generate', $15::numeric, $16, 'second', $17, $18
		)
		ON CONFLICT (provider_call_id) WHERE provider_call_id IS NOT NULL DO NOTHING
	`, organizationID, nullString(projectID), productionGenerationID, nullString(workflowRunID), nullString(nodeRunID),
		provenance.OperationID, provenance.OperationItemID, provenance.OperationItemAttempt, provenance.ExecutionPlanID, provenance.RenderSegmentID,
		providerCallID, selection.Model.ID, selection.CredentialID, nullString(selection.ModelProfileID), costOrZero(usage.EstimatedCost), currencyOrDefault(usage.Currency), quantity, metadata)
	return err
}

func (s *Service) getGatewayVideoTask(ctx context.Context, req GatewayVideoPollTaskRequest) (gatewayVideoTask, error) {
	var task gatewayVideoTask
	var projectID, operationID, productionGenerationID, videoProductionBindingID, workflowRunID, nodeRunID, nodeExecutionToken, providerModelID, credentialID, modelProfileID, modelProfileBindingID, modelProfileKey, promptVersionID, promptHash, externalTaskID sql.NullString
	var videoProductionBindingRevision sql.NullInt64
	var nodeAttemptGeneration sql.NullInt32
	var operationItemID, executionPlanID, renderSegmentID, videoVariantKey, capabilitySnapshotHash sql.NullString
	var operationItemAttempt sql.NullInt32
	var normalizedOutput []byte
	if strings.TrimSpace(req.ProviderAsyncTaskID) != "" {
		err := s.db.QueryRow(ctx, gatewayVideoTaskSelect(`WHERE t.id = $1 AND t.organization_id = $2`), req.ProviderAsyncTaskID, req.OrganizationID).Scan(
			&task.ID, &task.ProviderCallID, &task.OrganizationID, &projectID, &operationID, &operationItemID, &operationItemAttempt, &productionGenerationID, &videoProductionBindingID, &videoProductionBindingRevision, &workflowRunID, &nodeRunID, &nodeExecutionToken, &nodeAttemptGeneration,
			&task.ProviderAccountID, &providerModelID, &credentialID, &modelProfileID, &modelProfileBindingID, &modelProfileKey,
			&promptVersionID, &promptHash,
			&externalTaskID, &task.Status, &task.Input, &normalizedOutput, &task.PollCount,
			&executionPlanID, &renderSegmentID, &videoVariantKey, &capabilitySnapshotHash,
		)
		if err != nil {
			return gatewayVideoTask{}, err
		}
	} else {
		if strings.TrimSpace(req.ProviderAccountID) == "" || strings.TrimSpace(req.ExternalTaskID) == "" {
			return gatewayVideoTask{}, fmt.Errorf("%w: providerAsyncTaskId or providerAccountId/externalTaskId is required", ErrValidation)
		}
		err := s.db.QueryRow(ctx, gatewayVideoTaskSelect(`WHERE t.organization_id = $1 AND t.provider_account_id = $2 AND t.external_task_id = $3`), req.OrganizationID, req.ProviderAccountID, req.ExternalTaskID).Scan(
			&task.ID, &task.ProviderCallID, &task.OrganizationID, &projectID, &operationID, &operationItemID, &operationItemAttempt, &productionGenerationID, &videoProductionBindingID, &videoProductionBindingRevision, &workflowRunID, &nodeRunID, &nodeExecutionToken, &nodeAttemptGeneration,
			&task.ProviderAccountID, &providerModelID, &credentialID, &modelProfileID, &modelProfileBindingID, &modelProfileKey,
			&promptVersionID, &promptHash,
			&externalTaskID, &task.Status, &task.Input, &normalizedOutput, &task.PollCount,
			&executionPlanID, &renderSegmentID, &videoVariantKey, &capabilitySnapshotHash,
		)
		if err != nil {
			return gatewayVideoTask{}, err
		}
	}
	task.ProjectID = nullStringText(projectID)
	task.OperationID = nullStringText(operationID)
	task.OperationItemID = nullStringText(operationItemID)
	if operationItemAttempt.Valid {
		task.OperationItemAttempt = int(operationItemAttempt.Int32)
	}
	task.ProductionGenerationID = nullStringText(productionGenerationID)
	task.VideoProductionBindingID = nullStringText(videoProductionBindingID)
	if videoProductionBindingRevision.Valid {
		task.VideoProductionBindingRevision = videoProductionBindingRevision.Int64
	}
	task.WorkflowRunID = nullStringText(workflowRunID)
	task.NodeRunID = nullStringText(nodeRunID)
	task.NodeExecutionToken = nullStringText(nodeExecutionToken)
	if nodeAttemptGeneration.Valid {
		task.NodeAttemptGeneration = int(nodeAttemptGeneration.Int32)
	}
	task.ProviderModelID = nullStringText(providerModelID)
	task.CredentialID = nullStringText(credentialID)
	task.ModelProfileID = nullStringText(modelProfileID)
	task.ModelProfileBindingID = nullStringText(modelProfileBindingID)
	task.ModelProfileKey = nullStringText(modelProfileKey)
	task.PromptVersionID = nullStringText(promptVersionID)
	task.PromptHash = nullStringText(promptHash)
	task.ExternalTaskID = nullStringText(externalTaskID)
	task.NormalizedOutput = rawOrDefault(normalizedOutput, "{}")
	task.ExecutionPlanID = nullStringText(executionPlanID)
	task.RenderSegmentID = nullStringText(renderSegmentID)
	task.VideoVariantKey = nullStringText(videoVariantKey)
	task.CapabilitySnapshotHash = nullStringText(capabilitySnapshotHash)
	task.PromptTemplateKey = videoStringField(task.Input, "promptTemplateKey")
	task.PromptSource = videoStringField(task.Input, "promptSource")
	if task.ProviderModelID == "" {
		return gatewayVideoTask{}, fmt.Errorf("%w: provider async task has no provider model", ErrValidation)
	}
	return task, nil
}

func (s *Service) validateGatewayVideoNodeExecution(
	ctx context.Context,
	organizationID, projectID, workflowRunID, nodeRunID, executionToken string,
	attemptGeneration int,
) error {
	nodeRunID = strings.TrimSpace(nodeRunID)
	executionToken = strings.TrimSpace(executionToken)
	if nodeRunID == "" {
		if executionToken != "" || attemptGeneration != 0 {
			return fmt.Errorf("%w: node execution identity requires nodeRunId", ErrValidation)
		}
		return nil
	}
	if _, err := uuid.Parse(nodeRunID); err != nil {
		return fmt.Errorf("%w: nodeRunId is invalid", ErrValidation)
	}
	if _, err := uuid.Parse(executionToken); err != nil || attemptGeneration <= 0 {
		return fmt.Errorf("%w: nodeExecutionToken and nodeAttemptGeneration are required", ErrValidation)
	}
	var writable bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM workflow_node_runs node
			JOIN workflow_runs run ON run.id = node.workflow_run_id
			WHERE node.id = $1
			  AND node.execution_token = $2
			  AND node.attempt_generation = $3
			  AND node.status = 'running'
			  AND run.status = 'running'
			  AND ($4 = '' OR node.organization_id = NULLIF($4, '')::uuid)
			  AND ($5 = '' OR node.project_id = NULLIF($5, '')::uuid)
			  AND ($6 = '' OR node.workflow_run_id = NULLIF($6, '')::uuid)
		)
	`, nodeRunID, executionToken, attemptGeneration, strings.TrimSpace(organizationID), strings.TrimSpace(projectID), strings.TrimSpace(workflowRunID)).Scan(&writable); err != nil {
		return err
	}
	if !writable {
		return fmt.Errorf("%w: workflow node execution is no longer writable", ErrConflict)
	}
	return nil
}

func validateGatewayVideoTaskRequestIdentity(task gatewayVideoTask, req GatewayVideoPollTaskRequest) error {
	if task.NodeRunID == "" {
		if strings.TrimSpace(req.NodeRunID) != "" || strings.TrimSpace(req.NodeExecutionToken) != "" || req.NodeAttemptGeneration != 0 {
			return fmt.Errorf("%w: provider task is not bound to a workflow node", ErrConflict)
		}
		return nil
	}
	if strings.TrimSpace(req.NodeRunID) != task.NodeRunID ||
		strings.TrimSpace(req.NodeExecutionToken) != task.NodeExecutionToken ||
		req.NodeAttemptGeneration != task.NodeAttemptGeneration {
		return fmt.Errorf("%w: provider task belongs to a different node execution", ErrConflict)
	}
	return nil
}

func lockGatewayVideoNodeExecutionTx(ctx context.Context, tx pgx.Tx, nodeRunID, executionToken string, attemptGeneration int) error {
	if strings.TrimSpace(nodeRunID) == "" {
		return nil
	}
	var locked int
	if err := tx.QueryRow(ctx, `
		SELECT 1
		FROM workflow_node_runs node
		JOIN workflow_runs run ON run.id = node.workflow_run_id
		WHERE node.id = $1
		  AND node.execution_token = $2
		  AND node.attempt_generation = $3
		  AND node.status = 'running'
		  AND run.status = 'running'
		FOR UPDATE OF run, node
	`, nodeRunID, executionToken, attemptGeneration).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: workflow node execution is no longer writable", ErrConflict)
		}
		return err
	}
	return nil
}

func gatewayVideoTaskSelect(where string) string {
	return `
		SELECT t.id, t.provider_call_id, t.organization_id, t.project_id,
		       t.operation_id::text, t.operation_item_id::text, t.operation_item_attempt,
		       t.production_generation_id, t.video_production_binding_id, t.video_production_binding_revision,
		       t.workflow_run_id, t.node_run_id,
		       t.node_execution_token::text, t.node_attempt_generation,
		       t.provider_account_id, t.provider_model_id, t.credential_id, t.model_profile_id, t.model_profile_binding_id, t.model_profile_key,
		       l.prompt_version_id::text, l.prompt_hash,
		       t.external_task_id, t.status, t.input, t.normalized_output, t.poll_count,
		       t.video_render_plan_id::text, t.video_render_segment_id::text, t.video_variant_key, t.capability_snapshot_hash
		FROM provider_async_tasks t
		JOIN provider_call_logs l ON l.id = t.provider_call_id
	` + where
}

func normalizedVideoTerminalFailure(normalizedOutput json.RawMessage) (code, message, upstreamCode string, standard *StandardError) {
	upstreamCode = videoStringField(normalizedOutput, "errorCode", "code")
	message = normalizeUpstreamMessage(videoStringField(normalizedOutput, "errorMessage", "message", "failReason"))
	if message == "" {
		message = "provider video task failed"
	}
	code = CodeUpstreamInternalError
	retryable := true
	if containsUpstreamContentRejectionSignal(upstreamCode, message) {
		code = CodeContentRejected
		retryable = false
	} else if isVideoTimeoutFailure(upstreamCode, message) {
		code = CodeUpstreamTimeout
	} else if isVideoInvalidRequestFailure(upstreamCode, message) {
		code = CodeInvalidRequest
		retryable = false
	}
	standard = &StandardError{
		Code:         code,
		Message:      message,
		Retryable:    retryable,
		UpstreamCode: upstreamCode,
	}
	return code, message, upstreamCode, standard
}

func isVideoTimeoutFailure(code, message string) bool {
	value := strings.ToLower(strings.TrimSpace(code + " " + message))
	for _, signal := range []string{
		"task timeout",
		"task timed out",
		"task exceeded total timeout",
		"generation timeout",
		"generation timed out",
		"request timeout",
		"request timed out",
		"deadline exceeded",
		"任务超时",
		"生成超时",
	} {
		if strings.Contains(value, signal) {
			return true
		}
	}
	return false
}

func validateGatewayVideoPromptForModel(prompt string, model Model) error {
	constraint := ModelPromptLengthConstraint(model.Capabilities)
	length := MeasurePromptLength(prompt, constraint.Unit)
	if constraint.MaxLength > 0 && length > constraint.MaxLength {
		unit := "characters"
		if constraint.Unit == PromptLengthUnitUTF8Bytes {
			unit = "UTF-8 bytes"
		}
		return fmt.Errorf("%w: video prompt length %d exceeds model limit of %d %s", ErrValidation, length, constraint.MaxLength, unit)
	}
	return nil
}

func isVideoInvalidRequestFailure(code, message string) bool {
	value := strings.ToLower(strings.TrimSpace(code + " " + message))
	for _, signal := range []string{"prompt length", "maximum allowed length", "invalid parameter", "invalid argument"} {
		if strings.Contains(value, signal) {
			return true
		}
	}
	return false
}

func gatewayVideoStorageKey(organizationID, projectID, mimeType, sourceURL string) string {
	now := time.Now().UTC()
	ext := videoFileExtension(mimeType, sourceURL)
	if strings.TrimSpace(projectID) != "" {
		return fmt.Sprintf("org/%s/project/%s/provider-videos/%04d/%02d/%s%s", organizationID, projectID, now.Year(), int(now.Month()), uuid.NewString(), ext)
	}
	return fmt.Sprintf("org/%s/provider-videos/%04d/%02d/%s%s", organizationID, now.Year(), int(now.Month()), uuid.NewString(), ext)
}

func videoFileExtension(mimeType, sourceURL string) string {
	switch normalizeMediaType(mimeType) {
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}
	if sourceURL != "" {
		parsed, err := url.Parse(sourceURL)
		if err == nil {
			ext := strings.ToLower(path.Ext(parsed.Path))
			switch ext {
			case ".mp4", ".webm", ".mov":
				return ext
			}
		}
	}
	if ext, _ := mime.ExtensionsByType(normalizeMediaType(mimeType)); len(ext) > 0 {
		return ext[0]
	}
	return ".mp4"
}

func estimateVideoCost(input gatewayVideoInput, durationSeconds *float64, capabilities []Capability) GatewayUsage {
	currency := "USD"
	seconds := input.DurationSeconds
	if durationSeconds != nil && *durationSeconds > 0 {
		seconds = *durationSeconds
	}
	amount := 0.0
	for _, capability := range capabilities {
		var policy map[string]any
		if err := json.Unmarshal(capability.PricingPolicy, &policy); err != nil || len(policy) == 0 {
			continue
		}
		if value := stringPolicyField(policy, "currency"); value != "" {
			currency = strings.ToUpper(value)
		}
		if value, ok := videoCostByResolution(policy, input.Resolution); ok {
			amount = value * seconds
			break
		}
		if value, ok := imageCostValue(policy["videoCostPerSecond"]); ok {
			amount = value * seconds
			break
		}
		if value, ok := imageCostValue(policy["videoCostFlat"]); ok {
			amount = value
			break
		}
		break
	}
	return GatewayUsage{EstimatedCost: strconv.FormatFloat(math.Round(amount*1e8)/1e8, 'f', 8, 64), Currency: currency}
}

func videoCostByResolution(policy map[string]any, resolution string) (float64, bool) {
	if strings.TrimSpace(resolution) == "" {
		return 0, false
	}
	values, ok := policy["videoCostByResolution"].(map[string]any)
	if !ok {
		return 0, false
	}
	return imageCostValue(values[resolution])
}

func normalizeGatewayVideoStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "queued", "pending":
		return "queued"
	case "running", "processing", "in_progress", "in-progress":
		return "running"
	case "succeeded", "success", "completed", "done":
		return "succeeded"
	case "failed", "failure", "error", "timed_out", "timeout":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return ""
	}
}

func videoStringField(raw json.RawMessage, keys ...string) string {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := decoded[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func videoFloatField(raw json.RawMessage, keys ...string) float64 {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0
	}
	for _, key := range keys {
		if value := floatField(decoded[key], key); value > 0 {
			return value
		}
	}
	return 0
}

func videoStringOption(decoded map[string]any, key string) string {
	value, _ := decoded[key].(string)
	return strings.TrimSpace(value)
}

func floatField(value any, key string) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		return 0
	}
}

func accountConfigString(raw json.RawMessage, key string) string {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	value, _ := decoded[key].(string)
	return strings.TrimSpace(value)
}

func modelProviderOptionString(model Model, key string) string {
	for _, capability := range model.Capabilities {
		var decoded map[string]any
		if err := json.Unmarshal(capability.ProviderOptionsSchema, &decoded); err != nil {
			continue
		}
		if value, ok := decoded[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if nested, ok := decoded["providerOptions"].(map[string]any); ok {
			if value, ok := nested[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func rawJSONValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}
	return value
}

func gatewayVideoTimeout(optionMS, endpointMS int) time.Duration {
	ms := firstPositive(optionMS, endpointMS, 120000)
	return time.Duration(ms) * time.Millisecond
}

func gatewayVideoMaxBytes() int64 {
	value := strings.TrimSpace(os.Getenv("CINEWEAVE_PROVIDER_VIDEO_MAX_BYTES"))
	if value == "" {
		return defaultGatewayVideoMaxBytes
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return defaultGatewayVideoMaxBytes
	}
	return parsed
}

func gatewayVideoIdempotencyKey(value string, options GatewayVideoOptions) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(options.IdempotencyKey)
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func float64PtrIfPositive(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func videoMediaObservation(stored *gatewayStoredVideo) (*float64, json.RawMessage) {
	if stored == nil || stored.Output.MediaProbe == nil {
		return nil, json.RawMessage(`{}`)
	}
	return stored.Output.ActualDurationSeconds, mustJSON(stored.Output.MediaProbe)
}

func nullInt64Value(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullIntValue(value int) any {
	if value < 0 {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nullFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullStringText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
