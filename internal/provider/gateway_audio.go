package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type gatewayObjectReader interface {
	GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error)
}

type gatewayAudioInput struct {
	Text           string
	Voice          string
	ResponseFormat string
}

type gatewayStoredAudio struct {
	ArtifactID  string
	MediaFileID string
	Output      GatewayAudioOutput
}

type gatewayAudioSourceMaterial struct {
	StorageKey string
	MimeType   string
	FileName   string
	Body       []byte
	Duration   float64
}

func (s *Service) GenerateSpeech(ctx context.Context, req GatewayTTSRequest) (GatewayTTSResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayTTSResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayTTSResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayTTSResponse{}, err
	}
	start, err := s.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: req.OrganizationID,
		ProjectID:      req.ProjectID,
		WorkflowRunID:  req.WorkflowRunID,
		NodeRunID:      req.NodeRunID,
		TaskType:       TaskTypeAudioTTS,
		IdempotencyKey: gatewayTTSIdempotencyKey(req),
		RequestHash:    requestHash,
		Retry:          req.Options.Retry,
	})
	if err != nil {
		return GatewayTTSResponse{}, err
	}
	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayTTSResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerTTSStatusResponse(start.Request)
		}
		response.ProviderRequestID = start.Request.ID
		response.AttemptGeneration = start.Request.AttemptGeneration
		return response, nil
	}
	if start.Disposition == providerRequestInProgress {
		return providerTTSStatusResponse(start.Request), nil
	}
	response, runErr := s.executeGatewayTTS(ctx, req, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		if errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayTTSResponse{ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration, Status: "failed", Error: standard}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, nil, nil, standard); completeErr != nil {
				return GatewayTTSResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayTTSResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.AttemptGeneration = start.Request.AttemptGeneration
	artifactIDs, mediaFileIDs := audioResponseResourceIDs(response.Output)
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, artifactIDs, mediaFileIDs, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayTTSResponse{}, err
	}
	return response, nil
}

func providerTTSStatusResponse(request ProviderRequest) GatewayTTSResponse {
	return GatewayTTSResponse{ProviderRequestID: request.ID, AttemptGeneration: request.AttemptGeneration, Status: request.Status, Error: providerRequestStatusError(request)}
}

func (s *Service) executeGatewayTTS(ctx context.Context, req GatewayTTSRequest, providerRequestID string, attemptGeneration int) (GatewayTTSResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayTTSResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayTTSResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	audioInput, err := parseGatewayAudioInput(input)
	if err != nil {
		return GatewayTTSResponse{}, err
	}
	if s.objectStorage == nil {
		return GatewayTTSResponse{}, fmt.Errorf("%w: object storage is not configured", ErrValidation)
	}
	req.Input = input
	if req.TimelineTimebase <= 0 {
		req.TimelineTimebase = 90_000
	}

	selections, strategy, err := s.gatewayAudioSelections(ctx, req.OrganizationID, req.ProviderModelID, req.ModelProfileKey, TaskTypeAudioTTS)
	if err != nil {
		return GatewayTTSResponse{}, err
	}
	maxAttempts := fallbackMaxAttempts(strategy, len(selections))
	attempts := make([]GatewayAttempt, 0, maxAttempts)
	var final GatewayTTSResponse
	for index := 0; index < maxAttempts; index++ {
		selection := selections[index]
		selectedBy := string(RoutingPriority)
		if selection.ModelProfileKey != "" {
			selectedBy = selectionRoutingStrategy(strategy)
		}
		response, attempt, runErr := s.executeGatewayTTSAttempt(ctx, req, audioInput, selection, index+1, maxAttempts, selectedBy, providerRequestID, attemptGeneration)
		if runErr != nil {
			return GatewayTTSResponse{}, runErr
		}
		attempts = append(attempts, attempt)
		response.Attempts = append([]GatewayAttempt(nil), attempts...)
		final = response
		if response.Status == "succeeded" || index+1 >= maxAttempts || !shouldFallback(gatewayErrorCode(response.Error), strategy) {
			return response, nil
		}
	}
	return final, nil
}

func (s *Service) TranscribeAudio(ctx context.Context, req GatewayASRRequest) (GatewayASRResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayASRResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayASRResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input
	requestHash, err := gatewayRequestHash(req)
	if err != nil {
		return GatewayASRResponse{}, err
	}
	start, err := s.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: req.OrganizationID,
		ProjectID:      req.ProjectID,
		WorkflowRunID:  req.WorkflowRunID,
		NodeRunID:      req.NodeRunID,
		TaskType:       TaskTypeAudioTranscribe,
		IdempotencyKey: gatewayASRIdempotencyKey(req),
		RequestHash:    requestHash,
		Retry:          req.Options.Retry,
	})
	if err != nil {
		return GatewayASRResponse{}, err
	}
	if start.Disposition == providerRequestReplay {
		response, decodeErr := decodeProviderRequestResult[GatewayASRResponse](start.Request)
		if decodeErr != nil || strings.TrimSpace(response.Status) == "" {
			response = providerASRStatusResponse(start.Request)
		}
		response.ProviderRequestID = start.Request.ID
		response.AttemptGeneration = start.Request.AttemptGeneration
		return response, nil
	}
	if start.Disposition == providerRequestInProgress {
		return providerASRStatusResponse(start.Request), nil
	}
	response, runErr := s.executeGatewayASR(ctx, req, start.Request.ID, start.Request.AttemptGeneration)
	if runErr != nil {
		if errors.Is(runErr, ErrValidation) || errors.Is(runErr, pgx.ErrNoRows) {
			_, code, message, _, _ := normalizedProviderFailure(runErr)
			standard := standardErrorFromRunError(runErr, code, message)
			response = GatewayASRResponse{ProviderRequestID: start.Request.ID, AttemptGeneration: start.Request.AttemptGeneration, Status: "failed", Error: standard}
			if completeErr := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, nil, nil, standard); completeErr != nil {
				return GatewayASRResponse{}, completeErr
			}
			return response, nil
		}
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, runErr)
		return GatewayASRResponse{}, runErr
	}
	response.ProviderRequestID = start.Request.ID
	response.AttemptGeneration = start.Request.AttemptGeneration
	if err := s.completeProviderRequest(ctx, start.Request.ID, start.Request.AttemptGeneration, response.Status, response, nil, nil, response.Error); err != nil {
		_ = s.markProviderRequestUnknown(context.WithoutCancel(ctx), start.Request.ID, start.Request.AttemptGeneration, err)
		return GatewayASRResponse{}, err
	}
	return response, nil
}

func providerASRStatusResponse(request ProviderRequest) GatewayASRResponse {
	return GatewayASRResponse{ProviderRequestID: request.ID, AttemptGeneration: request.AttemptGeneration, Status: request.Status, Error: providerRequestStatusError(request)}
}

func (s *Service) executeGatewayASR(ctx context.Context, req GatewayASRRequest, providerRequestID string, attemptGeneration int) (GatewayASRResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayASRResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayASRResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input
	source, err := s.loadGatewayAudioSource(ctx, req.OrganizationID, req.Source)
	if err != nil {
		return GatewayASRResponse{}, err
	}
	selections, strategy, err := s.gatewayAudioSelections(ctx, req.OrganizationID, req.ProviderModelID, req.ModelProfileKey, TaskTypeAudioTranscribe)
	if err != nil {
		return GatewayASRResponse{}, err
	}
	maxAttempts := fallbackMaxAttempts(strategy, len(selections))
	attempts := make([]GatewayAttempt, 0, maxAttempts)
	var final GatewayASRResponse
	for index := 0; index < maxAttempts; index++ {
		selection := selections[index]
		selectedBy := string(RoutingPriority)
		if selection.ModelProfileKey != "" {
			selectedBy = selectionRoutingStrategy(strategy)
		}
		response, attempt, runErr := s.executeGatewayASRAttempt(ctx, req, source, selection, index+1, maxAttempts, selectedBy, providerRequestID, attemptGeneration)
		if runErr != nil {
			return GatewayASRResponse{}, runErr
		}
		attempts = append(attempts, attempt)
		response.Attempts = append([]GatewayAttempt(nil), attempts...)
		final = response
		if response.Status == "succeeded" || index+1 >= maxAttempts || !shouldFallback(gatewayErrorCode(response.Error), strategy) {
			return response, nil
		}
	}
	return final, nil
}

func parseGatewayAudioInput(raw json.RawMessage) (gatewayAudioInput, error) {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return gatewayAudioInput{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	input := gatewayAudioInput{
		Text:           firstAudioString(decoded, "input", "text"),
		Voice:          firstAudioString(decoded, "voice", "voiceKey"),
		ResponseFormat: strings.ToLower(firstAudioString(decoded, "response_format", "responseFormat", "format")),
	}
	if input.ResponseFormat == "" {
		input.ResponseFormat = "mp3"
	}
	if input.Text == "" || input.Voice == "" {
		return gatewayAudioInput{}, fmt.Errorf("%w: audio TTS input and voice are required", ErrValidation)
	}
	if !supportedAudioResponseFormat(input.ResponseFormat) {
		return gatewayAudioInput{}, fmt.Errorf("%w: audio response format is not supported", ErrValidation)
	}
	return input, nil
}

func (s *Service) gatewayAudioSelections(ctx context.Context, organizationID, providerModelID, modelProfileKey, taskType string) ([]gatewayModelSelection, FallbackStrategy, error) {
	if strings.TrimSpace(providerModelID) != "" {
		model, err := s.GetModel(ctx, organizationID, providerModelID)
		if err != nil {
			return nil, FallbackStrategy{}, err
		}
		if model.Status != "active" || (model.Modality != "audio" && model.Modality != "multimodal") || !modelSupportsTaskType(model, taskType) {
			return nil, FallbackStrategy{}, fmt.Errorf("%w: provider model does not support %s", ErrValidation, taskType)
		}
		account, err := s.GetAccount(ctx, organizationID, model.ProviderAccountID)
		if err != nil {
			return nil, FallbackStrategy{}, err
		}
		selection, err := s.completeGatewaySelection(ctx, organizationID, account, model, "", "", "")
		return []gatewayModelSelection{selection}, FallbackStrategy{Enabled: false, MaxAttempts: 1}, err
	}
	if strings.TrimSpace(modelProfileKey) == "" {
		return nil, FallbackStrategy{}, fmt.Errorf("%w: modelProfileKey or providerModelId is required", ErrValidation)
	}
	candidates, err := s.ResolveRoutingCandidates(ctx, RoutingRequest{
		OrganizationID: organizationID, ModelProfileKey: modelProfileKey, TaskType: taskType, Modality: "audio",
	})
	if err != nil {
		return nil, FallbackStrategy{}, err
	}
	selections := make([]gatewayModelSelection, 0, len(candidates))
	for _, candidate := range candidates {
		selection, err := s.completeGatewaySelectionFromCandidate(ctx, organizationID, candidate)
		if err != nil {
			return nil, FallbackStrategy{}, err
		}
		selections = append(selections, selection)
	}
	return selections, candidates[0].FallbackStrategy, nil
}

func selectionRoutingStrategy(strategy FallbackStrategy) string {
	if strategy.Enabled {
		return string(RoutingPriorityWithFallback)
	}
	return string(RoutingPriority)
}

func (s *Service) executeGatewayTTSAttempt(ctx context.Context, req GatewayTTSRequest, input gatewayAudioInput, selection gatewayModelSelection, attemptIndex, maxAttempts int, selectedBy, providerRequestID string, attemptGeneration int) (GatewayTTSResponse, GatewayAttempt, error) {
	cfg := parseOpenAICompatibleConfig(selection.Account.Config)
	if req.Options.TimeoutMS > 0 {
		cfg.TimeoutMS = req.Options.TimeoutMS
	} else if !openAICompatibleConfigHasTimeout(selection.Account.Config) {
		cfg.TimeoutMS = gatewayAudioTimeoutMSFromEnv()
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	usage := estimateTTSCost(input, selection.Model.Capabilities)
	callID := uuid.NewString()
	baseRecord := audioCallRecordInput{
		ProviderRequestID: providerRequestID, AttemptGeneration: attemptGeneration, AttemptSequence: attemptIndex,
		OrganizationID: req.OrganizationID, ProjectID: req.ProjectID, WorkflowRunID: req.WorkflowRunID, NodeRunID: req.NodeRunID,
		CallID: callID, IdempotencyKey: gatewayTTSIdempotencyKey(req), TaskType: TaskTypeAudioTTS,
		Status: "running", RequestSnapshot: req.Input, Usage: usage,
		Quantity: float64(len([]rune(input.Text))), Unit: "character", SourceText: input.Text, Voice: input.Voice, SkipCost: true,
	}
	if _, err := s.recordGatewayAudioCall(ctx, selection, baseRecord); err != nil {
		return GatewayTTSResponse{}, GatewayAttempt{}, err
	}
	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: req.OrganizationID, Selection: selection, TaskType: TaskTypeAudioTTS,
		EstimatedCost: usage.EstimatedCost, Currency: usage.Currency, LeaseTTL: timeout + 30*time.Second,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		return s.blockedGatewayTTSAttempt(ctx, selection, baseRecord, usage, guardErr, attemptIndex, maxAttempts, selectedBy)
	}
	providerCallID := ""
	defer func() { s.releaseGatewayLease(lease, providerCallID) }()

	client := newOpenAICompatibleClient(timeout)
	client.mediaFetcher = s.mediaFetcher
	result, runErr := client.audioSpeech(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, req.Input)
	defer result.close()
	status := "succeeded"
	var standardError *StandardError
	var errorCode, errorMessage, upstreamErrorCode string
	var upstreamStatus *int
	responseSnapshot := result.ResponseSnapshot
	normalizedOutput := result.NormalizedOutput
	var stored *gatewayStoredAudio
	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
	} else {
		stored, runErr = s.storeGatewayTTSMedia(callCtx, callID, req, selection, result)
		if runErr != nil {
			status = "failed"
			errorCode = CodeMediaDownloadFailed
			errorMessage = runErr.Error()
			standardError = &StandardError{Code: CodeMediaDownloadFailed, Message: "provider audio media could not be stored", Retryable: true}
		}
	}
	if stored != nil {
		normalizedOutput = mustJSON(stored.Output)
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
	}
	normalizedOutput = withRoutingNormalizedOutput(normalizedOutput, selection, attemptIndex, maxAttempts, selectedBy)
	finalRecord := baseRecord
	finalRecord.LeaseID = lease.LeaseID
	finalRecord.Status = status
	finalRecord.LatencyMS = result.LatencyMS
	finalRecord.ErrorCode = errorCode
	finalRecord.ErrorMessage = errorMessage
	finalRecord.UpstreamStatus = upstreamStatus
	finalRecord.UpstreamErrorCode = upstreamErrorCode
	finalRecord.RequestSnapshot = result.RequestSnapshot
	finalRecord.ResponseSnapshot = responseSnapshot
	finalRecord.NormalizedOutput = normalizedOutput
	finalRecord.Stored = stored
	finalRecord.SkipCost = false
	call, err := s.recordGatewayAudioCall(ctx, selection, finalRecord)
	if err != nil {
		return GatewayTTSResponse{}, GatewayAttempt{}, err
	}
	providerCallID = call.ID
	if runErr != nil {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}
	attempt := gatewayAttemptFromCall(call, selection, standardError, result.LatencyMS)
	response := GatewayTTSResponse{
		ProviderRequestID: providerRequestID, AttemptGeneration: attemptGeneration,
		ProviderCallID: call.ID, ModelID: selection.Model.ID, Status: status, Usage: usage,
		Error: standardError, LatencyMS: result.LatencyMS, Attempts: []GatewayAttempt{attempt},
	}
	if stored != nil {
		response.Output = stored.Output
	}
	return response, attempt, nil
}

func (s *Service) blockedGatewayTTSAttempt(ctx context.Context, selection gatewayModelSelection, record audioCallRecordInput, usage GatewayUsage, guardErr error, attemptIndex, maxAttempts int, selectedBy string) (GatewayTTSResponse, GatewayAttempt, error) {
	standard, ok := blockedGatewayStandard(guardErr)
	if !ok {
		return GatewayTTSResponse{}, GatewayAttempt{}, guardErr
	}
	record.Status = "blocked"
	record.ErrorCode = standard.Code
	record.ErrorMessage = standard.Message
	record.ResponseSnapshot = blockedResponseSnapshot(standard)
	record.NormalizedOutput = withRoutingNormalizedOutput(blockedNormalizedOutput(standard), selection, attemptIndex, maxAttempts, selectedBy)
	record.Usage = GatewayUsage{EstimatedCost: "0.00000000", Currency: usage.Currency}
	record.SkipCost = true
	call, err := s.recordGatewayAudioCall(ctx, selection, record)
	if err != nil {
		return GatewayTTSResponse{}, GatewayAttempt{}, err
	}
	attempt := gatewayAttemptFromCall(call, selection, standard, 0)
	return GatewayTTSResponse{
		ProviderRequestID: record.ProviderRequestID, AttemptGeneration: record.AttemptGeneration,
		ProviderCallID: call.ID, ModelID: selection.Model.ID, Status: "blocked", Error: standard,
		Usage: GatewayUsage{EstimatedCost: "0.00000000", Currency: usage.Currency}, Attempts: []GatewayAttempt{attempt},
	}, attempt, nil
}

func (s *Service) executeGatewayASRAttempt(ctx context.Context, req GatewayASRRequest, source gatewayAudioSourceMaterial, selection gatewayModelSelection, attemptIndex, maxAttempts int, selectedBy, providerRequestID string, attemptGeneration int) (GatewayASRResponse, GatewayAttempt, error) {
	cfg := parseOpenAICompatibleConfig(selection.Account.Config)
	if req.Options.TimeoutMS > 0 {
		cfg.TimeoutMS = req.Options.TimeoutMS
	} else if !openAICompatibleConfigHasTimeout(selection.Account.Config) {
		cfg.TimeoutMS = gatewayAudioTimeoutMSFromEnv()
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	usage := estimateASRCost(source.Duration, selection.Model.Capabilities)
	callID := uuid.NewString()
	baseRecord := audioCallRecordInput{
		ProviderRequestID: providerRequestID, AttemptGeneration: attemptGeneration, AttemptSequence: attemptIndex,
		OrganizationID: req.OrganizationID, ProjectID: req.ProjectID, WorkflowRunID: req.WorkflowRunID, NodeRunID: req.NodeRunID,
		CallID: callID, IdempotencyKey: gatewayASRIdempotencyKey(req), TaskType: TaskTypeAudioTranscribe,
		Status: "running", RequestSnapshot: mustJSON(map[string]any{"storageKey": source.StorageKey, "input": req.Input}),
		Usage: usage, Quantity: source.Duration / 60, Unit: "minute", SkipCost: true,
	}
	if _, err := s.recordGatewayAudioCall(ctx, selection, baseRecord); err != nil {
		return GatewayASRResponse{}, GatewayAttempt{}, err
	}
	guardReq := s.gatewayGuardRequest(gatewayGuardRequestInput{
		OrganizationID: req.OrganizationID, Selection: selection, TaskType: TaskTypeAudioTranscribe,
		EstimatedCost: usage.EstimatedCost, Currency: usage.Currency, LeaseTTL: timeout + 30*time.Second,
	})
	lease, guardErr := s.guard.Acquire(ctx, guardReq)
	if guardErr != nil {
		return s.blockedGatewayASRAttempt(ctx, selection, baseRecord, usage, guardErr, attemptIndex, maxAttempts, selectedBy)
	}
	providerCallID := ""
	defer func() { s.releaseGatewayLease(lease, providerCallID) }()

	client := newOpenAICompatibleClient(timeout)
	result, runErr := client.audioTranscription(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, req.Input, source.Body, source.MimeType, source.FileName)
	status := "succeeded"
	var standardError *StandardError
	var errorCode, errorMessage, upstreamErrorCode string
	var upstreamStatus *int
	responseSnapshot := result.ResponseSnapshot
	normalizedOutput := result.NormalizedOutput
	if runErr != nil {
		status, errorCode, errorMessage, upstreamStatus, upstreamErrorCode = normalizedProviderFailure(runErr)
		standardError = standardErrorFromRunError(runErr, errorCode, errorMessage)
		if len(responseSnapshot) == 0 {
			responseSnapshot = upstreamBody(runErr)
		}
	}
	if len(responseSnapshot) == 0 {
		responseSnapshot = json.RawMessage(`null`)
	}
	if len(normalizedOutput) == 0 {
		normalizedOutput = mustJSON(map[string]any{"status": status, "errorCode": errorCode})
	}
	normalizedOutput = withRoutingNormalizedOutput(normalizedOutput, selection, attemptIndex, maxAttempts, selectedBy)
	finalRecord := baseRecord
	finalRecord.LeaseID = lease.LeaseID
	finalRecord.Status = status
	finalRecord.LatencyMS = result.LatencyMS
	finalRecord.ErrorCode = errorCode
	finalRecord.ErrorMessage = errorMessage
	finalRecord.UpstreamStatus = upstreamStatus
	finalRecord.UpstreamErrorCode = upstreamErrorCode
	finalRecord.RequestSnapshot = result.RequestSnapshot
	finalRecord.ResponseSnapshot = responseSnapshot
	finalRecord.NormalizedOutput = normalizedOutput
	finalRecord.SkipCost = false
	call, err := s.recordGatewayAudioCall(ctx, selection, finalRecord)
	if err != nil {
		return GatewayASRResponse{}, GatewayAttempt{}, err
	}
	providerCallID = call.ID
	if runErr != nil {
		s.recordGatewayGuardFailure(ctx, guardReq, errorCode, errorMessage)
	} else {
		s.recordGatewayGuardSuccess(ctx, guardReq)
	}
	attempt := gatewayAttemptFromCall(call, selection, standardError, result.LatencyMS)
	return GatewayASRResponse{
		ProviderRequestID: providerRequestID, AttemptGeneration: attemptGeneration,
		ProviderCallID: call.ID, ModelID: selection.Model.ID, Status: status, Output: result.Output,
		Usage: usage, Error: standardError, LatencyMS: result.LatencyMS, Attempts: []GatewayAttempt{attempt},
	}, attempt, nil
}

func (s *Service) blockedGatewayASRAttempt(ctx context.Context, selection gatewayModelSelection, record audioCallRecordInput, usage GatewayUsage, guardErr error, attemptIndex, maxAttempts int, selectedBy string) (GatewayASRResponse, GatewayAttempt, error) {
	standard, ok := blockedGatewayStandard(guardErr)
	if !ok {
		return GatewayASRResponse{}, GatewayAttempt{}, guardErr
	}
	record.Status = "blocked"
	record.ErrorCode = standard.Code
	record.ErrorMessage = standard.Message
	record.ResponseSnapshot = blockedResponseSnapshot(standard)
	record.NormalizedOutput = withRoutingNormalizedOutput(blockedNormalizedOutput(standard), selection, attemptIndex, maxAttempts, selectedBy)
	record.Usage = GatewayUsage{EstimatedCost: "0.00000000", Currency: usage.Currency}
	record.SkipCost = true
	call, err := s.recordGatewayAudioCall(ctx, selection, record)
	if err != nil {
		return GatewayASRResponse{}, GatewayAttempt{}, err
	}
	attempt := gatewayAttemptFromCall(call, selection, standard, 0)
	return GatewayASRResponse{
		ProviderRequestID: record.ProviderRequestID, AttemptGeneration: record.AttemptGeneration,
		ProviderCallID: call.ID, ModelID: selection.Model.ID, Status: "blocked", Error: standard,
		Usage: GatewayUsage{EstimatedCost: "0.00000000", Currency: usage.Currency}, Attempts: []GatewayAttempt{attempt},
	}, attempt, nil
}

func (s *Service) storeGatewayTTSMedia(ctx context.Context, callID string, req GatewayTTSRequest, selection gatewayModelSelection, result audioSpeechResult) (*gatewayStoredAudio, error) {
	storageKey := path.Join("org", req.OrganizationID, "project", fallbackPathSegment(req.ProjectID, "unscoped"), "provider-audio", callID+audioExtension(result.MimeType))
	put, err := s.objectStorage.PutFile(ctx, storageKey, result.TempPath, result.MimeType)
	if err != nil {
		return nil, err
	}
	contentHash := put.ContentHash
	if contentHash == "" {
		contentHash = result.ContentHash
	}
	return &gatewayStoredAudio{
		ArtifactID: uuid.NewString(), MediaFileID: uuid.NewString(),
		Output: GatewayAudioOutput{
			StorageKey: put.StorageKey, MimeType: result.MimeType, ByteSize: result.ByteSize, ContentHash: contentHash,
			Raw: result.NormalizedOutput,
		},
	}, nil
}

type audioCallRecordInput struct {
	ProviderRequestID                                   string
	AttemptGeneration, AttemptSequence                  int
	OrganizationID, ProjectID, WorkflowRunID, NodeRunID string
	CallID, LeaseID, IdempotencyKey, TaskType, Status   string
	LatencyMS                                           int
	ErrorCode, ErrorMessage, UpstreamErrorCode          string
	UpstreamStatus                                      *int
	RequestSnapshot, ResponseSnapshot, NormalizedOutput json.RawMessage
	Stored                                              *gatewayStoredAudio
	Usage                                               GatewayUsage
	Quantity                                            float64
	Unit, SourceText, Voice                             string
	SkipCost                                            bool
}

func (s *Service) recordGatewayAudioCall(ctx context.Context, selection gatewayModelSelection, input audioCallRecordInput) (CallLog, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CallLog{}, err
	}
	defer tx.Rollback(ctx)
	if input.Stored != nil {
		input.Stored.Output.ArtifactID = input.Stored.ArtifactID
		input.Stored.Output.MediaFileID = input.Stored.MediaFileID
		if err := insertGatewayAudioArtifact(ctx, tx, selection, input); err != nil {
			return CallLog{}, err
		}
		if err := insertGatewayAudioMediaFile(ctx, tx, selection, input); err != nil {
			return CallLog{}, err
		}
	}
	callReq := RecordCallRequest{
		ID: input.CallID, ProviderRequestID: input.ProviderRequestID, AttemptGeneration: input.AttemptGeneration, AttemptSequence: input.AttemptSequence,
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		WorkflowRunID: input.WorkflowRunID, NodeRunID: input.NodeRunID,
		ProviderAccountID: selection.Account.ID, ProviderModelID: selection.Model.ID, CredentialID: selection.CredentialID,
		ModelProfileID: selection.ModelProfileID, ModelProfileBindingID: selection.ModelProfileBindingID, ModelProfileKey: selection.ModelProfileKey,
		LeaseID: input.LeaseID, IdempotencyKey: input.IdempotencyKey, TaskType: input.TaskType, ExecutionMode: "sync", Status: input.Status,
		LatencyMS: &input.LatencyMS, EstimatedCost: input.Usage.EstimatedCost, Currency: input.Usage.Currency,
		ErrorCode: input.ErrorCode, ErrorMessage: input.ErrorMessage, UpstreamStatus: input.UpstreamStatus, UpstreamErrorCode: input.UpstreamErrorCode,
		RequestSnapshot: input.RequestSnapshot, ResponseSnapshot: input.ResponseSnapshot, NormalizedOutput: input.NormalizedOutput,
	}
	if input.Stored != nil {
		callReq.ArtifactIDs = mustJSON([]string{input.Stored.ArtifactID})
		callReq.MediaFileIDs = mustJSON([]string{input.Stored.MediaFileID})
	}
	call, err := recordCall(ctx, tx, callReq)
	if err != nil {
		return CallLog{}, err
	}
	if !input.SkipCost {
		if err := insertAudioCostRecord(ctx, tx, call.ID, selection, input); err != nil {
			return CallLog{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CallLog{}, err
	}
	return call, nil
}

func insertGatewayAudioArtifact(ctx context.Context, tx pgx.Tx, selection gatewayModelSelection, input audioCallRecordInput) error {
	metadata := mustJSON(map[string]any{
		"source": "provider_gateway", "providerCallId": input.CallID, "providerModelId": selection.Model.ID,
		"mediaFileId": input.Stored.MediaFileID, "voice": input.Voice, "sourceText": input.SourceText,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO artifacts(id, organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, model_id, metadata)
		VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, 'generated_audio', $6, $7, $8, $9, $10)
	`, input.Stored.ArtifactID, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.NodeRunID,
		input.Stored.Output.StorageKey, input.Stored.Output.MimeType, input.Stored.Output.ContentHash, selection.Model.ID, metadata)
	return err
}

func insertGatewayAudioMediaFile(ctx context.Context, tx pgx.Tx, selection gatewayModelSelection, input audioCallRecordInput) error {
	metadata := mustJSON(map[string]any{
		"source": "provider_gateway", "providerCallId": input.CallID, "providerModelId": selection.Model.ID,
		"voice": input.Voice,
	})
	_, err := tx.Exec(ctx, `
		INSERT INTO media_files(id, organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, metadata)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9)
	`, input.Stored.MediaFileID, input.OrganizationID, input.ProjectID, input.Stored.ArtifactID,
		input.Stored.Output.StorageKey, input.Stored.Output.MimeType, input.Stored.Output.ByteSize, input.Stored.Output.ContentHash, metadata)
	return err
}

func insertAudioCostRecord(ctx context.Context, tx pgx.Tx, providerCallID string, selection gatewayModelSelection, input audioCallRecordInput) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO cost_records(
			organization_id, project_id, workflow_run_id, node_run_id, provider_call_id,
			provider_model_id, credential_id, model_profile_id, cost_type, amount, currency, unit, quantity, metadata
		)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6, $7,
		        NULLIF($8, '')::uuid, $9, $10::numeric, $11, $12, $13, $14)
		ON CONFLICT (provider_call_id) WHERE provider_call_id IS NOT NULL DO NOTHING
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.NodeRunID, providerCallID,
		selection.Model.ID, selection.CredentialID, selection.ModelProfileID, input.TaskType,
		costOrZero(input.Usage.EstimatedCost), currencyOrDefault(input.Usage.Currency), input.Unit, input.Quantity,
		mustJSON(map[string]any{"quantity": input.Quantity, "unit": input.Unit}))
	return err
}

func audioResponseResourceIDs(output GatewayAudioOutput) ([]string, []string) {
	artifactIDs := []string{}
	mediaFileIDs := []string{}
	if output.ArtifactID != "" {
		artifactIDs = append(artifactIDs, output.ArtifactID)
	}
	if output.MediaFileID != "" {
		mediaFileIDs = append(mediaFileIDs, output.MediaFileID)
	}
	return artifactIDs, mediaFileIDs
}

func (s *Service) loadGatewayAudioSource(ctx context.Context, organizationID string, source GatewayAudioSource) (gatewayAudioSourceMaterial, error) {
	reader, ok := s.objectStorage.(gatewayObjectReader)
	if !ok {
		return gatewayAudioSourceMaterial{}, fmt.Errorf("%w: object storage does not support audio reads", ErrValidation)
	}
	var storageKey, mimeType string
	var duration sql.NullFloat64
	switch {
	case strings.TrimSpace(source.MediaFileID) != "":
		err := s.db.QueryRow(ctx, `
			SELECT storage_key, mime_type, duration_seconds::float8
			FROM media_files WHERE organization_id = $1 AND id = $2
		`, organizationID, source.MediaFileID).Scan(&storageKey, &mimeType, &duration)
		if err != nil {
			return gatewayAudioSourceMaterial{}, err
		}
	case strings.TrimSpace(source.ArtifactID) != "":
		err := s.db.QueryRow(ctx, `
			SELECT storage_key, COALESCE(mime_type, 'application/octet-stream')
			FROM artifacts WHERE organization_id = $1 AND id = $2 AND storage_key IS NOT NULL
		`, organizationID, source.ArtifactID).Scan(&storageKey, &mimeType)
		if err != nil {
			return gatewayAudioSourceMaterial{}, err
		}
	case strings.TrimSpace(source.StorageKey) != "":
		err := s.db.QueryRow(ctx, `
			SELECT storage_key, mime_type, duration_seconds::float8
			FROM media_files WHERE organization_id = $1 AND storage_key = $2
			ORDER BY created_at DESC LIMIT 1
		`, organizationID, source.StorageKey).Scan(&storageKey, &mimeType, &duration)
		if errors.Is(err, pgx.ErrNoRows) {
			err = s.db.QueryRow(ctx, `
				SELECT storage_key, COALESCE(mime_type, 'application/octet-stream')
				FROM artifacts WHERE organization_id = $1 AND storage_key = $2
				ORDER BY created_at DESC LIMIT 1
			`, organizationID, source.StorageKey).Scan(&storageKey, &mimeType)
		}
		if err != nil {
			return gatewayAudioSourceMaterial{}, err
		}
	default:
		return gatewayAudioSourceMaterial{}, fmt.Errorf("%w: audio source artifactId, mediaFileId, or storageKey is required", ErrValidation)
	}
	body, storedMime, err := reader.GetObject(ctx, storageKey, maxGatewayTTSBytes)
	if err != nil {
		return gatewayAudioSourceMaterial{}, err
	}
	if strings.TrimSpace(source.MimeType) != "" {
		mimeType = source.MimeType
	} else if strings.TrimSpace(storedMime) != "" {
		mimeType = storedMime
	}
	return gatewayAudioSourceMaterial{
		StorageKey: storageKey, MimeType: mimeType, FileName: firstNonEmptyAudio(source.FileName, path.Base(storageKey)),
		Body: body, Duration: duration.Float64,
	}, nil
}

func gatewayTTSIdempotencyKey(req GatewayTTSRequest) string {
	return firstNonEmptyAudio(req.IdempotencyKey, req.Options.IdempotencyKey)
}

func gatewayASRIdempotencyKey(req GatewayASRRequest) string {
	return firstNonEmptyAudio(req.IdempotencyKey, req.Options.IdempotencyKey)
}

func firstNonEmptyAudio(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fallbackPathSegment(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func estimateTTSCost(input gatewayAudioInput, capabilities []Capability) GatewayUsage {
	currency := "USD"
	rate := 0.0
	for _, capability := range capabilities {
		var policy map[string]any
		if json.Unmarshal(capability.PricingPolicy, &policy) != nil {
			continue
		}
		if value := stringPolicyField(policy, "currency"); value != "" {
			currency = strings.ToUpper(value)
		}
		rate = firstFloatPolicyField(policy, "characterPer1K", "ttsCharacterPer1K", "audioCharacterCostPer1K")
		if rate > 0 {
			break
		}
	}
	cost := float64(len([]rune(input.Text))) / 1000 * rate
	return GatewayUsage{EstimatedCost: fmt.Sprintf("%.8f", math.Max(cost, 0)), Currency: currency}
}

func estimateASRCost(durationSeconds float64, capabilities []Capability) GatewayUsage {
	currency := "USD"
	rate := 0.0
	for _, capability := range capabilities {
		var policy map[string]any
		if json.Unmarshal(capability.PricingPolicy, &policy) != nil {
			continue
		}
		if value := stringPolicyField(policy, "currency"); value != "" {
			currency = strings.ToUpper(value)
		}
		rate = firstFloatPolicyField(policy, "audioMinute", "transcriptionMinute", "audioMinuteCost")
		if rate > 0 {
			break
		}
	}
	cost := durationSeconds / 60 * rate
	return GatewayUsage{EstimatedCost: fmt.Sprintf("%.8f", math.Max(cost, 0)), Currency: currency}
}
