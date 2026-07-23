package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type gatewayModelSelection struct {
	Account               Account
	Model                 Model
	CredentialID          string
	Credential            map[string]any
	APIKey                string
	ModelProfileID        string
	ModelProfileBindingID string
	ModelProfileKey       string
	RuntimeOptions        ModelProfileBindingRuntimeOptions
}

func (s *Service) GenerateText(ctx context.Context, req GatewayTextRequest) (GatewayTextResponse, error) {
	return s.executeProviderTextRequest(ctx, req, false, nil)
}

func (s *Service) StreamText(ctx context.Context, req GatewayTextRequest, onDelta func(GatewayTextDelta) error) (GatewayTextResponse, error) {
	return s.StreamTextEvents(ctx, req, func(event GatewayTextStreamEvent) error {
		if event.Type != GatewayTextEventDelta || event.Delta == nil || onDelta == nil {
			return nil
		}
		return onDelta(*event.Delta)
	})
}

func (s *Service) StreamTextEvents(ctx context.Context, req GatewayTextRequest, emit func(GatewayTextStreamEvent) error) (GatewayTextResponse, error) {
	response, err := s.executeProviderTextRequest(ctx, req, true, emit)
	if err != nil {
		standard := standardErrorForStream(err, response.Error)
		_ = emitGatewayTextEvent(emit, GatewayTextStreamEvent{
			Type: GatewayTextEventFailed,
			Failure: &GatewayTextFailureEvent{
				SchemaVersion:     GatewayTextStreamSchemaVersion,
				ProviderRequestID: response.ProviderRequestID,
				ProviderCallID:    response.ProviderCallID,
				AttemptGeneration: response.AttemptGeneration,
				AttemptSequence:   response.AttemptSequence,
				Error:             standard,
			},
		})
		return response, err
	}
	if response.requestDisposition == string(providerRequestReplay) {
		if err := emitGatewayTextEvent(emit, GatewayTextStreamEvent{
			Type: GatewayTextEventReplayed,
			Replay: &GatewayTextReplayEvent{
				SchemaVersion:     GatewayTextStreamSchemaVersion,
				ProviderRequestID: response.ProviderRequestID,
				ProviderCallID:    response.ProviderCallID,
				AttemptGeneration: response.AttemptGeneration,
				AttemptSequence:   response.AttemptSequence,
				ModelID:           response.ModelID,
				Status:            response.Status,
				Output:            response.Output,
				Usage:             response.Usage,
				LatencyMS:         response.LatencyMS,
			},
		}); err != nil {
			return GatewayTextResponse{}, err
		}
		return response, nil
	}
	if response.Status != "succeeded" {
		standard := response.Error
		if standard == nil {
			standard = &StandardError{Code: CodeUnknownError, Message: "provider stream failed", Retryable: false}
		}
		if err := emitGatewayTextEvent(emit, GatewayTextStreamEvent{
			Type: GatewayTextEventFailed,
			Failure: &GatewayTextFailureEvent{
				SchemaVersion:     GatewayTextStreamSchemaVersion,
				ProviderRequestID: response.ProviderRequestID,
				ProviderCallID:    response.ProviderCallID,
				AttemptGeneration: response.AttemptGeneration,
				AttemptSequence:   response.AttemptSequence,
				Error:             standard,
			},
		}); err != nil {
			return GatewayTextResponse{}, err
		}
		return response, &StandardErrorError{Standard: *standard}
	}
	if err := emitGatewayTextEvent(emit, GatewayTextStreamEvent{Type: GatewayTextEventCompleted, Response: &response}); err != nil {
		return GatewayTextResponse{}, err
	}
	return response, nil
}

func emitGatewayTextEvent(emit func(GatewayTextStreamEvent) error, event GatewayTextStreamEvent) error {
	if emit == nil {
		return nil
	}
	return emit(event)
}

func standardErrorForStream(err error, fallback *StandardError) *StandardError {
	if fallback != nil {
		return fallback
	}
	var standardErr *StandardErrorError
	if errors.As(err, &standardErr) {
		standard := standardErr.Standard
		return &standard
	}
	_, code, message, _, _ := normalizedProviderFailure(err)
	return standardErrorFromRunError(err, code, message)
}

func (s *Service) executeProviderTextRequest(ctx context.Context, req GatewayTextRequest, stream bool, emit func(GatewayTextStreamEvent) error) (GatewayTextResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayTextResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input
	taskType := TaskTypeTextGenerate
	if stream {
		taskType = TaskTypeTextStream
	}
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	start, err := s.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: req.OrganizationID,
		ProjectID:      req.ProjectID,
		WorkflowRunID:  req.WorkflowRunID,
		NodeRunID:      req.NodeRunID,
		TaskType:       taskType,
		IdempotencyKey: gatewayIdempotencyKey(req),
		RequestHash:    requestHash,
		Retry:          req.Options.Retry,
	})
	if err != nil {
		return GatewayTextResponse{}, err
	}

	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayTextResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerTextStatusResponse(start.Request)
		}
		response.ProviderRequestID = start.Request.ID
		response.SchemaVersion = GatewayTextStreamSchemaVersion
		response.AttemptGeneration = start.Request.AttemptGeneration
		response.requestDisposition = string(providerRequestReplay)
		return response, nil
	}
	if start.Disposition == providerRequestInProgress {
		response := providerTextStatusResponse(start.Request)
		response.requestDisposition = string(providerRequestInProgress)
		return response, nil
	}

	response, runErr := s.executeGatewayText(ctx, req, stream, emit, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		if errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayTextResponse{
				SchemaVersion:     GatewayTextStreamSchemaVersion,
				ProviderRequestID: start.Request.ID,
				AttemptGeneration: start.Request.AttemptGeneration,
				Status:            "failed",
				Error:             standard,
			}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, nil, nil, standard); completeErr != nil {
				return GatewayTextResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayTextResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.SchemaVersion = GatewayTextStreamSchemaVersion
	response.AttemptGeneration = start.Request.AttemptGeneration
	response.requestDisposition = string(providerRequestExecute)
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, nil, nil, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayTextResponse{}, err
	}
	return response, nil
}

func providerTextStatusResponse(request ProviderRequest) GatewayTextResponse {
	return GatewayTextResponse{
		SchemaVersion:     GatewayTextStreamSchemaVersion,
		ProviderRequestID: request.ID,
		AttemptGeneration: request.AttemptGeneration,
		Status:            request.Status,
		Error:             providerRequestStatusError(request),
	}
}

func (s *Service) DiscoverModelsViaGateway(ctx context.Context, req GatewayDiscoverModelsRequest) (GatewayDiscoverModelsResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.AccountID) == "" {
		return GatewayDiscoverModelsResponse{}, fmt.Errorf("%w: organizationId and accountId are required", ErrValidation)
	}
	taskType := strings.TrimSpace(req.TestType)
	if taskType == "" {
		taskType = "model_discovery"
	}
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}
	start, err := s.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: req.OrganizationID,
		TaskType:       taskType,
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    requestHash,
		Retry:          req.Retry,
	})
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}
	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayDiscoverModelsResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerDiscoveryStatusResponse(start.Request)
		}
		response.ProviderRequestID = start.Request.ID
		response.AttemptGeneration = start.Request.AttemptGeneration
		return response, nil
	}
	if start.Disposition == providerRequestInProgress {
		return providerDiscoveryStatusResponse(start.Request), nil
	}
	response, runErr := s.executeGatewayDiscovery(ctx, req, taskType, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		if errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayDiscoverModelsResponse{ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration, Status: "failed", Error: standard}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, "failed", response, nil, nil, standard); completeErr != nil {
				return GatewayDiscoverModelsResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayDiscoverModelsResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.AttemptGeneration = start.Request.AttemptGeneration
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, nil, nil, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayDiscoverModelsResponse{}, err
	}
	return response, nil
}

func providerDiscoveryStatusResponse(request ProviderRequest) GatewayDiscoverModelsResponse {
	return GatewayDiscoverModelsResponse{ProviderRequestID: request.ID, AttemptGeneration: request.AttemptGeneration, Status: request.Status, Models: []DiscoveredModel{}, Unsupported: []any{}, Error: providerRequestStatusError(request)}
}

func (s *Service) executeGatewayDiscovery(ctx context.Context, req GatewayDiscoverModelsRequest, taskType, providerRequestID string, attemptGeneration int) (GatewayDiscoverModelsResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" || strings.TrimSpace(req.AccountID) == "" {
		return GatewayDiscoverModelsResponse{}, fmt.Errorf("%w: organizationId and accountId are required", ErrValidation)
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, req.AccountID)
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}
	if account.Status != "active" {
		return GatewayDiscoverModelsResponse{}, fmt.Errorf("%w: provider account is not active", ErrValidation)
	}
	var credential map[string]any
	var credentialID string
	if strings.TrimSpace(req.CredentialID) != "" {
		credential, credentialID, err = s.activeCredentialPayloadByID(ctx, req.OrganizationID, account.ID, req.CredentialID)
	} else {
		credential, credentialID, err = s.activeCredentialPayload(ctx, req.OrganizationID, account.ID)
	}
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}

	cfg := parseOpenAICompatibleConfig(account.Config)
	client := newOpenAICompatibleClient(time.Duration(cfg.TimeoutMS) * time.Millisecond)
	callID := uuid.NewString()
	baseCall := RecordCallRequest{
		ID:                callID,
		ProviderRequestID: providerRequestID,
		AttemptGeneration: attemptGeneration,
		AttemptSequence:   1,
		OrganizationID:    req.OrganizationID,
		ProviderAccountID: account.ID,
		CredentialID:      credentialID,
		IdempotencyKey:    req.IdempotencyKey,
		TaskType:          taskType,
		ExecutionMode:     "sync",
		Status:            "running",
		RequestSnapshot:   mustJSON(map[string]any{"method": "GET", "endpoint": cfg.ModelsEndpoint}),
	}
	if _, err := recordCall(ctx, s.db, baseCall); err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}
	started := time.Now()
	discovery, runErr := client.discoverModels(ctx, account, apiKey, cfg)
	latencyMS := int(time.Since(started).Milliseconds())

	status := "succeeded"
	normalizedOutput := mustJSON(map[string]any{"models": discovery.Models, "unsupported": discovery.Unsupported})
	responseSnapshot := normalizedOutput
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		responseSnapshot = upstreamBody(runErr)
		normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
	}

	finalCall := baseCall
	finalCall.Status = status
	finalCall.LatencyMS = &latencyMS
	finalCall.ErrorCode = errorCode
	finalCall.ErrorMessage = errorMessage
	finalCall.UpstreamStatus = upstreamStatus
	finalCall.UpstreamErrorCode = upstreamErrorCode
	finalCall.ResponseSnapshot = responseSnapshot
	finalCall.NormalizedOutput = normalizedOutput
	call, err := recordCall(ctx, s.db, finalCall)
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}

	return GatewayDiscoverModelsResponse{
		ProviderRequestID: providerRequestID,
		AttemptGeneration: attemptGeneration,
		ProviderCallID:    call.ID,
		Status:            status,
		Models:            discovery.Models,
		Unsupported:       discovery.Unsupported,
		Error:             standardError,
		LatencyMS:         latencyMS,
	}, nil
}

func (s *Service) executeGatewayText(ctx context.Context, req GatewayTextRequest, stream bool, emit func(GatewayTextStreamEvent) error, providerRequestID string, attemptGeneration int) (GatewayTextResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayTextResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input

	taskType := TaskTypeTextGenerate
	executionMode := "sync"
	if stream {
		taskType = TaskTypeTextStream
		executionMode = "stream"
	}

	if strings.TrimSpace(req.ProviderModelID) != "" {
		selection, err := s.selectGatewayTextModel(ctx, req, taskType)
		if err != nil {
			return GatewayTextResponse{}, err
		}
		response, _, err := s.executeGatewayTextAttempt(ctx, req, selection, stream, emit, taskType, executionMode, 1, 1, string(RoutingPriority), providerRequestID, attemptGeneration)
		return response, err
	}

	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:                      req.OrganizationID,
		ModelProfileKey:                     req.ModelProfileKey,
		TaskType:                            taskType,
		Modality:                            "text",
		MaxOutputTokens:                     firstPositive(intFieldFromJSON(req.Input, "maxOutputTokens"), intFieldFromJSON(req.Input, "max_tokens")),
		InputLanguage:                       req.InputLanguage,
		OutputLanguage:                      req.OutputLanguage,
		RequireApprovedLanguageCapabilities: req.RequireApprovedLanguageCapabilities,
	})
	if err != nil {
		return GatewayTextResponse{}, err
	}
	strategy := candidates[0].FallbackStrategy
	maxAttempts := fallbackMaxAttempts(strategy, len(candidates))
	attempts := make([]GatewayAttempt, 0, maxAttempts)
	var final GatewayTextResponse
	for i := 0; i < maxAttempts; i++ {
		candidate := candidates[i]
		selection, err := s.completeGatewaySelectionFromCandidate(ctx, req.OrganizationID, candidate)
		if err != nil {
			return GatewayTextResponse{}, err
		}
		sentDelta := false
		attemptEmit := emit
		if stream {
			attemptEmit = func(event GatewayTextStreamEvent) error {
				if event.Type == GatewayTextEventDelta && event.Delta != nil && strings.TrimSpace(event.Delta.Text) != "" {
					sentDelta = true
				}
				return emitGatewayTextEvent(emit, event)
			}
		}
		response, attempt, err := s.executeGatewayTextAttempt(ctx, req, selection, stream, attemptEmit, taskType, executionMode, i+1, maxAttempts, candidate.RoutingStrategy, providerRequestID, attemptGeneration)
		if err != nil {
			return GatewayTextResponse{}, err
		}
		attempts = append(attempts, attempt)
		response.Attempts = append([]GatewayAttempt(nil), attempts...)
		final = response
		if response.Status == "succeeded" {
			return response, nil
		}
		if stream && sentDelta {
			return response, nil
		}
		if i+1 >= maxAttempts || !shouldFallback(gatewayErrorCode(response.Error), strategy) {
			return response, nil
		}
	}
	return final, nil
}

func (s *Service) executeGatewayTextAttempt(ctx context.Context, req GatewayTextRequest, selection gatewayModelSelection, stream bool, emit func(GatewayTextStreamEvent) error, taskType, executionMode string, attemptIndex, maxAttempts int, selectedBy, providerRequestID string, attemptGeneration int) (GatewayTextResponse, GatewayAttempt, error) {
	effectiveInput, err := applyTextRuntimeOptions(req.Input, selection.Model, selection.RuntimeOptions)
	if err != nil {
		return GatewayTextResponse{}, GatewayAttempt{}, err
	}
	req.Input = effectiveInput
	cfg := parseOpenAICompatibleConfig(selection.Account.Config)
	if req.Options.TimeoutMS > 0 {
		cfg.TimeoutMS = req.Options.TimeoutMS
	} else if !openAICompatibleConfigHasTimeout(selection.Account.Config) {
		cfg.TimeoutMS = gatewayTextTimeoutMSFromEnv()
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	upstreamInput := req.Input
	requestSnapshot := gatewayTextRequestSnapshot(req.Input, req.References)
	if len(req.References) > 0 {
		if !modelSupportsTextImageInput(selection.Model) {
			return GatewayTextResponse{}, GatewayAttempt{}, &StandardErrorError{Standard: StandardError{
				Code: CodeModelCapabilityUnavailable, Message: "selected text model does not support image input", Retryable: false,
			}}
		}
		materials, materializeErr := s.materializeOpenAICompatibleImageReferences(ctx, selection.Account, req.References, timeout)
		if materializeErr != nil {
			return GatewayTextResponse{}, GatewayAttempt{}, materializeErr
		}
		upstreamInput, err = injectGatewayTextImageReferences(req.Input, materials)
		if err != nil {
			return GatewayTextResponse{}, GatewayAttempt{}, err
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	callID := uuid.NewString()
	baseCall := RecordCallRequest{
		ID:                    callID,
		ProviderRequestID:     providerRequestID,
		AttemptGeneration:     attemptGeneration,
		AttemptSequence:       attemptIndex,
		OrganizationID:        req.OrganizationID,
		ProjectID:             req.ProjectID,
		WorkflowRunID:         req.WorkflowRunID,
		NodeRunID:             req.NodeRunID,
		ProviderAccountID:     selection.Account.ID,
		ProviderModelID:       selection.Model.ID,
		CredentialID:          selection.CredentialID,
		ModelProfileID:        selection.ModelProfileID,
		ModelProfileBindingID: selection.ModelProfileBindingID,
		ModelProfileKey:       selection.ModelProfileKey,
		PromptVersionID:       req.PromptVersionID,
		PromptHash:            req.PromptHash,
		IdempotencyKey:        gatewayIdempotencyKey(req),
		TaskType:              taskType,
		ExecutionMode:         executionMode,
		Status:                "running",
		RequestSnapshot:       requestSnapshot,
	}
	if _, err := recordCall(ctx, s.db, baseCall); err != nil {
		return GatewayTextResponse{}, GatewayAttempt{}, err
	}
	if stream {
		if err := emitGatewayTextEvent(emit, GatewayTextStreamEvent{
			Type: GatewayTextEventAttemptStarted,
			Attempt: &GatewayTextAttemptEvent{
				SchemaVersion:     GatewayTextStreamSchemaVersion,
				ProviderRequestID: providerRequestID,
				ProviderCallID:    callID,
				AttemptGeneration: attemptGeneration,
				AttemptSequence:   attemptIndex,
				ProviderModelID:   selection.Model.ID,
				Status:            "running",
			},
		}); err != nil {
			return GatewayTextResponse{}, GatewayAttempt{}, err
		}
	}

	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: req.OrganizationID,
		Selection:      selection,
		TaskType:       taskType,
		EstimatedCost:  "0.00000000",
		Currency:       "USD",
		LeaseTTL:       timeout + 30*time.Second,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		standard, ok := blockedGatewayStandard(guardErr)
		if !ok {
			return GatewayTextResponse{}, GatewayAttempt{}, guardErr
		}
		blockedCall := baseCall
		blockedCall.Status = "blocked"
		blockedCall.ErrorCode = standard.Code
		blockedCall.ErrorMessage = standard.Message
		blockedCall.ResponseSnapshot = blockedResponseSnapshot(standard)
		blockedCall.NormalizedOutput = withRoutingNormalizedOutput(blockedNormalizedOutput(standard), selection, attemptIndex, maxAttempts, selectedBy)
		call, err := s.recordGatewayTextCall(ctx, selection, req, blockedCall, GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"})
		if err != nil {
			return GatewayTextResponse{}, GatewayAttempt{}, err
		}
		attempt := gatewayAttemptFromCall(call, selection, standard, 0)
		response := GatewayTextResponse{
			SchemaVersion:     GatewayTextStreamSchemaVersion,
			ProviderRequestID: providerRequestID,
			AttemptGeneration: attemptGeneration,
			AttemptSequence:   attemptIndex,
			ProviderCallID:    call.ID,
			ModelID:           selection.Model.ID,
			Status:            "blocked",
			Output:            GatewayTextOutput{Raw: blockedResponseSnapshot(standard)},
			Usage:             GatewayUsage{EstimatedCost: "0.00000000", Currency: "USD"},
			Error:             standard,
			Attempts:          []GatewayAttempt{attempt},
		}
		if stream {
			if err := emitGatewayTextAttemptFailed(emit, response, selection.Model.ID, standard); err != nil {
				return GatewayTextResponse{}, GatewayAttempt{}, err
			}
		}
		return response, attempt, nil
	}
	recordedProviderCallID := ""
	defer func() {
		s.releaseGatewayLease(lease, recordedProviderCallID)
	}()

	client := newOpenAICompatibleClient(timeout)
	var result chatCompletionResult
	if stream {
		var sequence int64
		result, err = client.streamChatCompletion(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, upstreamInput, func(text string) error {
			if emit == nil {
				return nil
			}
			sequence++
			delta := &GatewayTextDelta{
				SchemaVersion:     GatewayTextStreamSchemaVersion,
				ProviderRequestID: providerRequestID,
				ProviderCallID:    callID,
				AttemptGeneration: attemptGeneration,
				AttemptSequence:   attemptIndex,
				Sequence:          sequence,
				Text:              text,
			}
			return emitGatewayTextEvent(emit, GatewayTextStreamEvent{Type: GatewayTextEventDelta, Delta: delta})
		})
	} else {
		result, err = client.chatCompletion(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, upstreamInput)
	}

	status := "succeeded"
	var errorCode, errorMessage string
	var upstreamStatus *int
	var upstreamErrorCode string
	var standardError *StandardError
	responseSnapshot := result.ResponseSnapshot
	normalizedOutput := result.NormalizedOutput
	if err != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(err)
		standardError = standardErrorFromRunError(err, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(err)
		}
		if len(normalizedOutput) == 0 {
			normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = mustJSON(map[string]any{"text": result.Text})
	}
	normalizedOutput = withRoutingNormalizedOutput(normalizedOutput, selection, attemptIndex, maxAttempts, selectedBy)

	runErr := err
	usage := estimateTextCost(result.Usage, selection.Model.Capabilities)
	finalCall := baseCall
	finalCall.LeaseID = lease.LeaseID
	finalCall.Status = status
	finalCall.LatencyMS = &result.LatencyMS
	finalCall.InputTokens = intPtrIfPositive(usage.InputTokens)
	finalCall.OutputTokens = intPtrIfPositive(usage.OutputTokens)
	finalCall.EstimatedCost = usage.EstimatedCost
	finalCall.Currency = usage.Currency
	finalCall.ErrorCode = errorCode
	finalCall.ErrorMessage = errorMessage
	finalCall.UpstreamStatus = upstreamStatus
	finalCall.UpstreamErrorCode = upstreamErrorCode
	finalCall.RequestSnapshot = requestSnapshot
	finalCall.ResponseSnapshot = responseSnapshot
	finalCall.NormalizedOutput = normalizedOutput
	call, err := s.recordGatewayTextCall(ctx, selection, req, finalCall, usage)
	if err != nil {
		return GatewayTextResponse{}, GatewayAttempt{}, err
	}
	recordedProviderCallID = call.ID
	if runErr != nil {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}

	attempt := gatewayAttemptFromCall(call, selection, standardError, result.LatencyMS)
	response := GatewayTextResponse{
		SchemaVersion:     GatewayTextStreamSchemaVersion,
		ProviderRequestID: providerRequestID,
		AttemptGeneration: attemptGeneration,
		AttemptSequence:   attemptIndex,
		ProviderCallID:    call.ID,
		ModelID:           selection.Model.ID,
		Status:            status,
		Output: GatewayTextOutput{
			Text: result.Text,
			Raw:  responseSnapshot,
		},
		Usage:     usage,
		Error:     standardError,
		LatencyMS: result.LatencyMS,
		Attempts:  []GatewayAttempt{attempt},
	}
	if stream && standardError != nil {
		if err := emitGatewayTextAttemptFailed(emit, response, selection.Model.ID, standardError); err != nil {
			return GatewayTextResponse{}, GatewayAttempt{}, err
		}
	}
	return response, attempt, nil
}

func emitGatewayTextAttemptFailed(emit func(GatewayTextStreamEvent) error, response GatewayTextResponse, providerModelID string, standard *StandardError) error {
	return emitGatewayTextEvent(emit, GatewayTextStreamEvent{
		Type: GatewayTextEventAttemptFailed,
		Attempt: &GatewayTextAttemptEvent{
			SchemaVersion:     GatewayTextStreamSchemaVersion,
			ProviderRequestID: response.ProviderRequestID,
			ProviderCallID:    response.ProviderCallID,
			AttemptGeneration: response.AttemptGeneration,
			AttemptSequence:   response.AttemptSequence,
			ProviderModelID:   providerModelID,
			Status:            response.Status,
			Error:             standard,
		},
	})
}

func (s *Service) selectGatewayTextModel(ctx context.Context, req GatewayTextRequest, taskType string) (gatewayModelSelection, error) {
	if strings.TrimSpace(req.ProviderModelID) != "" {
		model, err := s.GetModel(ctx, req.OrganizationID, req.ProviderModelID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		if model.Status != "active" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
		}
		if model.Modality != "text" && model.Modality != "multimodal" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model does not support text generation", ErrValidation)
		}
		if !modelSupportsTaskType(model, taskType) {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model does not support %s", ErrValidation, taskType)
		}
		if err := ValidateModelLanguageCapabilities(model, taskType, LanguageCapabilityRequirement{
			InputLanguage: req.InputLanguage, OutputLanguage: req.OutputLanguage,
			RequireApproved: req.RequireApprovedLanguageCapabilities,
		}); err != nil {
			return gatewayModelSelection{}, err
		}
		account, err := s.GetAccount(ctx, req.OrganizationID, model.ProviderAccountID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		return s.completeGatewaySelection(ctx, req.OrganizationID, account, model, "", "", "")
	}

	profileKey := strings.TrimSpace(req.ModelProfileKey)
	if profileKey == "" {
		return gatewayModelSelection{}, fmt.Errorf("%w: modelProfileKey or providerModelId is required", ErrValidation)
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID:                      req.OrganizationID,
		ModelProfileKey:                     profileKey,
		TaskType:                            taskType,
		Modality:                            "text",
		InputLanguage:                       req.InputLanguage,
		OutputLanguage:                      req.OutputLanguage,
		RequireApprovedLanguageCapabilities: req.RequireApprovedLanguageCapabilities,
	})
	if err != nil {
		return gatewayModelSelection{}, err
	}
	return s.completeGatewaySelectionFromCandidate(ctx, req.OrganizationID, candidates[0])
}

func (s *Service) completeGatewaySelectionFromCandidate(ctx context.Context, organizationID string, candidate RoutingCandidate) (gatewayModelSelection, error) {
	model, err := s.GetModel(ctx, organizationID, candidate.ProviderModelID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	selection, err := s.completeGatewaySelection(ctx, organizationID, account, model, candidate.ModelProfileID, candidate.ModelProfileBindingID, candidate.ModelProfileKey)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	selection.RuntimeOptions = candidate.RuntimeOptions
	return selection, nil
}

func (s *Service) completeGatewaySelection(ctx context.Context, organizationID string, account Account, model Model, profileID, bindingID, profileKey string) (gatewayModelSelection, error) {
	if account.Status != "active" {
		return gatewayModelSelection{}, fmt.Errorf("%w: provider account is not active", ErrValidation)
	}
	credential, credentialID, err := s.credentialPayloadForModel(ctx, organizationID, account.ID, model.ID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	return gatewayModelSelection{
		Account:               account,
		Model:                 model,
		CredentialID:          credentialID,
		Credential:            credential,
		APIKey:                apiKey,
		ModelProfileID:        profileID,
		ModelProfileBindingID: bindingID,
		ModelProfileKey:       profileKey,
	}, nil
}

func (s *Service) recordGatewayTextCall(ctx context.Context, selection gatewayModelSelection, req GatewayTextRequest, callReq RecordCallRequest, usage GatewayUsage) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)

	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, err
	}
	if callReq.Status != "blocked" {
		if err := insertTextCostRecord(ctx, tx, call.ID, selection, req, callReq.TaskType, usage); err != nil {
			return CallLog{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func insertTextCostRecord(ctx context.Context, tx pgx.Tx, providerCallID string, selection gatewayModelSelection, req GatewayTextRequest, costType string, usage GatewayUsage) error {
	totalTokens := usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = usage.InputTokens + usage.OutputTokens
	}
	metadata := mustJSON(map[string]any{
		"inputTokens":  usage.InputTokens,
		"outputTokens": usage.OutputTokens,
		"totalTokens":  totalTokens,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO cost_records(
			organization_id, project_id, workflow_run_id, node_run_id,
			provider_call_id, provider_model_id, credential_id, model_profile_id,
			cost_type, amount, currency, unit, quantity, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11, 'token', $12, $13)
		ON CONFLICT (provider_call_id) WHERE provider_call_id IS NOT NULL DO NOTHING
	`,
		req.OrganizationID,
		nullString(req.ProjectID),
		nullString(req.WorkflowRunID),
		nullString(req.NodeRunID),
		providerCallID,
		selection.Model.ID,
		selection.CredentialID,
		nullString(selection.ModelProfileID),
		costType,
		costOrZero(usage.EstimatedCost),
		currencyOrDefault(usage.Currency),
		totalTokens,
		metadata,
	)
	return err
}

func estimateTextCost(usage GatewayUsage, capabilities []Capability) GatewayUsage {
	currency := "USD"
	var inputRate, outputRate float64
	for _, capability := range capabilities {
		var policy map[string]any
		if err := json.Unmarshal(capability.PricingPolicy, &policy); err != nil || len(policy) == 0 {
			continue
		}
		if value := stringPolicyField(policy, "currency"); value != "" {
			currency = strings.ToUpper(value)
		}
		inputRate = firstFloatPolicyField(policy, "inputTokenPer1K", "inputTokenCostPer1K", "promptTokenPer1K", "promptTokenCostPer1K", "inputPer1K")
		outputRate = firstFloatPolicyField(policy, "outputTokenPer1K", "outputTokenCostPer1K", "completionTokenPer1K", "completionTokenCostPer1K", "outputPer1K")
		break
	}
	total := usage.TotalTokens
	if total == 0 {
		total = usage.InputTokens + usage.OutputTokens
	}
	estimated := (float64(usage.InputTokens)/1000.0)*inputRate + (float64(usage.OutputTokens)/1000.0)*outputRate
	usage.TotalTokens = total
	usage.Currency = currency
	usage.EstimatedCost = strconv.FormatFloat(math.Round(estimated*1e8)/1e8, 'f', 8, 64)
	return usage
}

func standardErrorFromRunError(err error, code, message string) *StandardError {
	if standard, ok := StandardErrorFromError(err); ok {
		return standard
	}
	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		standard := NormalizeUpstreamError(upstreamErr)
		return &standard
	}
	retryable := errors.Is(err, context.DeadlineExceeded) || isTransientProviderTransportError(err) || code == CodeUpstreamInternalError || code == CodeUpstreamStreamTruncated
	return &StandardError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}
}

func gatewayIdempotencyKey(req GatewayTextRequest) string {
	if value := strings.TrimSpace(req.IdempotencyKey); value != "" {
		return value
	}
	return strings.TrimSpace(req.Options.IdempotencyKey)
}

func intPtrIfPositive(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func costOrZero(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0.00000000"
	}
	return value
}

func stringPolicyField(policy map[string]any, key string) string {
	value, _ := policy[key].(string)
	return strings.TrimSpace(value)
}

func firstFloatPolicyField(policy map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := policy[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err == nil {
				return parsed
			}
		case json.Number:
			parsed, err := typed.Float64()
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}
