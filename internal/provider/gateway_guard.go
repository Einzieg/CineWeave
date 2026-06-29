package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

func (s *Service) gatewayGuardRequest(req gatewayGuardRequestInput) GuardRequest {
	return GuardRequest{
		OrganizationID:    req.OrganizationID,
		ProviderAccountID: req.Selection.Account.ID,
		ProviderModelID:   req.Selection.Model.ID,
		TaskType:          req.TaskType,
		EstimatedCost:     req.EstimatedCost,
		Currency:          req.Currency,
		LeaseTTL:          req.LeaseTTL,
		AcquiredByService: providerGuardServiceName(),
	}
}

type gatewayGuardRequestInput struct {
	OrganizationID string
	Selection      gatewayModelSelection
	TaskType       string
	EstimatedCost  string
	Currency       string
	LeaseTTL       time.Duration
}

func (s *Service) releaseGatewayLease(lease GuardLease, providerCallID string) {
	if s.guard == nil || lease.LeaseToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.guard.Release(ctx, lease, providerCallID)
}

func (s *Service) recordGatewayGuardSuccess(ctx context.Context, req GuardRequest) {
	if s.guard == nil {
		return
	}
	_ = s.guard.RecordSuccess(ctx, req)
}

func (s *Service) recordGatewayGuardFailure(ctx context.Context, req GuardRequest, code, message string) {
	if s.guard == nil {
		return
	}
	_ = s.guard.RecordFailure(ctx, req, code, message)
}

func blockedGatewayStandard(err error) (*StandardError, bool) {
	if standard, ok := standardErrorFromGuard(err); ok {
		return standard, true
	}
	return nil, false
}

func StandardErrorFromGuard(err error) (*StandardError, bool) {
	return standardErrorFromGuard(err)
}

func blockedNormalizedOutput(standard *StandardError) json.RawMessage {
	code := CodeUnknownError
	if standard != nil && standard.Code != "" {
		code = standard.Code
	}
	return mustJSON(map[string]any{"status": "blocked", "errorCode": code})
}

func blockedResponseSnapshot(standard *StandardError) json.RawMessage {
	if standard == nil {
		return json.RawMessage(`null`)
	}
	return mustJSON(standard)
}

func isProviderFailureStatus(status string) bool {
	return status == "failed" || status == "blocked"
}

func providerGuardServiceName() string {
	value := strings.TrimSpace(os.Getenv("CINEWEAVE_PROVIDER_GUARD_SERVICE_NAME"))
	if value == "" {
		return "provider-gateway"
	}
	return value
}
