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

	"github.com/jackc/pgx/v5"
)

type gatewayModelSelection struct {
	Account               Account
	Model                 Model
	CredentialID          string
	APIKey                string
	ModelProfileID        string
	ModelProfileBindingID string
	ModelProfileKey       string
}

func (s *Service) GenerateText(ctx context.Context, req GatewayTextRequest) (GatewayTextResponse, error) {
	return s.executeGatewayText(ctx, req, false, nil)
}

func (s *Service) StreamText(ctx context.Context, req GatewayTextRequest, onDelta func(GatewayTextDelta) error) (GatewayTextResponse, error) {
	return s.executeGatewayText(ctx, req, true, onDelta)
}

func (s *Service) DiscoverModelsViaGateway(ctx context.Context, req GatewayDiscoverModelsRequest) (GatewayDiscoverModelsResponse, error) {
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
	credential, credentialID, err := s.activeCredentialPayload(ctx, req.OrganizationID, account.ID)
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}
	apiKey, err := apiKeyFromCredential(credential)
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}

	cfg := parseOpenAICompatibleConfig(account.Config)
	client := newOpenAICompatibleClient(time.Duration(cfg.TimeoutMS) * time.Millisecond)
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

	taskType := strings.TrimSpace(req.TestType)
	if taskType == "" {
		taskType = "model_discovery"
	}
	call, err := recordCall(ctx, s.db, RecordCallRequest{
		OrganizationID:    req.OrganizationID,
		ProviderAccountID: account.ID,
		CredentialID:      credentialID,
		IdempotencyKey:    req.IdempotencyKey,
		TaskType:          taskType,
		ExecutionMode:     "sync",
		Status:            status,
		LatencyMS:         &latencyMS,
		ErrorCode:         errorCode,
		ErrorMessage:      errorMessage,
		UpstreamStatus:    upstreamStatus,
		UpstreamErrorCode: upstreamErrorCode,
		RequestSnapshot:   mustJSON(map[string]any{"method": "GET", "endpoint": cfg.ModelsEndpoint}),
		ResponseSnapshot:  responseSnapshot,
		NormalizedOutput:  normalizedOutput,
	})
	if err != nil {
		return GatewayDiscoverModelsResponse{}, err
	}

	return GatewayDiscoverModelsResponse{
		ProviderCallID: call.ID,
		Status:         status,
		Models:         discovery.Models,
		Unsupported:    discovery.Unsupported,
		Error:          standardError,
		LatencyMS:      latencyMS,
	}, nil
}

func (s *Service) executeGatewayText(ctx context.Context, req GatewayTextRequest, stream bool, onDelta func(GatewayTextDelta) error) (GatewayTextResponse, error) {
	if strings.TrimSpace(req.OrganizationID) == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: organizationId is required", ErrValidation)
	}
	input, err := normalizeJSON(req.Input, "{}")
	if err != nil {
		return GatewayTextResponse{}, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	req.Input = input

	selection, err := s.selectGatewayTextModel(ctx, req)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	cfg := parseOpenAICompatibleConfig(selection.Account.Config)
	if req.Options.TimeoutMS > 0 {
		cfg.TimeoutMS = req.Options.TimeoutMS
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := newOpenAICompatibleClient(timeout)
	var result chatCompletionResult
	if stream {
		result, err = client.streamChatCompletion(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, input, func(text string) error {
			if onDelta == nil {
				return nil
			}
			return onDelta(GatewayTextDelta{Text: text})
		})
	} else {
		result, err = client.chatCompletion(callCtx, selection.Account, selection.Model, selection.APIKey, cfg, input)
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

	usage := estimateTextCost(result.Usage, selection.Model.Capabilities)
	taskType := "text.generate"
	executionMode := "sync"
	if stream {
		taskType = "text.stream"
		executionMode = "stream"
	}
	call, err := s.recordGatewayTextCall(ctx, selection, req, RecordCallRequest{
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
		Status:                status,
		LatencyMS:             &result.LatencyMS,
		InputTokens:           intPtrIfPositive(usage.InputTokens),
		OutputTokens:          intPtrIfPositive(usage.OutputTokens),
		EstimatedCost:         usage.EstimatedCost,
		Currency:              usage.Currency,
		ErrorCode:             errorCode,
		ErrorMessage:          errorMessage,
		UpstreamStatus:        upstreamStatus,
		UpstreamErrorCode:     upstreamErrorCode,
		RequestSnapshot:       result.RequestSnapshot,
		ResponseSnapshot:      responseSnapshot,
		NormalizedOutput:      normalizedOutput,
	}, usage)
	if err != nil {
		return GatewayTextResponse{}, err
	}

	return GatewayTextResponse{
		ProviderCallID: call.ID,
		ModelID:        selection.Model.ID,
		Status:         status,
		Output: GatewayTextOutput{
			Text: result.Text,
			Raw:  responseSnapshot,
		},
		Usage:     usage,
		Error:     standardError,
		LatencyMS: result.LatencyMS,
	}, nil
}

func (s *Service) selectGatewayTextModel(ctx context.Context, req GatewayTextRequest) (gatewayModelSelection, error) {
	if strings.TrimSpace(req.ProviderModelID) != "" {
		model, err := s.GetModel(ctx, req.OrganizationID, req.ProviderModelID)
		if err != nil {
			return gatewayModelSelection{}, err
		}
		if model.Status != "active" {
			return gatewayModelSelection{}, fmt.Errorf("%w: provider model is not active", ErrValidation)
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
	var profileID, bindingID, modelID string
	err := s.db.QueryRow(ctx, `
		SELECT p.id, b.id, m.id
		FROM model_profiles p
		JOIN model_profile_bindings b ON b.model_profile_id = p.id
		JOIN provider_models m ON m.id = b.provider_model_id
		JOIN provider_accounts a ON a.id = m.provider_account_id
		WHERE p.organization_id = $1
		  AND p.profile_key = $2
		  AND b.enabled = true
		  AND m.status = 'active'
		  AND a.status = 'active'
		  AND m.modality IN ('text', 'multimodal')
		ORDER BY b.priority ASC, b.weight DESC, b.created_at ASC
		LIMIT 1
	`, req.OrganizationID, profileKey).Scan(&profileID, &bindingID, &modelID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gatewayModelSelection{}, fmt.Errorf("%w: no active provider model is bound to modelProfileKey", ErrValidation)
		}
		return gatewayModelSelection{}, err
	}
	model, err := s.GetModel(ctx, req.OrganizationID, modelID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	account, err := s.GetAccount(ctx, req.OrganizationID, model.ProviderAccountID)
	if err != nil {
		return gatewayModelSelection{}, err
	}
	return s.completeGatewaySelection(ctx, req.OrganizationID, account, model, profileID, bindingID, profileKey)
}

func (s *Service) completeGatewaySelection(ctx context.Context, organizationID string, account Account, model Model, profileID, bindingID, profileKey string) (gatewayModelSelection, error) {
	if account.Status != "active" {
		return gatewayModelSelection{}, fmt.Errorf("%w: provider account is not active", ErrValidation)
	}
	credential, credentialID, err := s.activeCredentialPayload(ctx, organizationID, account.ID)
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
	if err := insertTextCostRecord(ctx, tx, call.ID, selection, req, callReq.TaskType, usage); err != nil {
		return CallLog{}, err
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
	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		standard := NormalizeHTTPError(upstreamErr.Status, upstreamErr.Code)
		return &standard
	}
	retryable := errors.Is(err, context.DeadlineExceeded)
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
