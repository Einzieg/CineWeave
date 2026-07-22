package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrValidation              = errors.New("provider validation failed")
	ErrConflict                = errors.New("provider conflict")
	ErrModelAlreadyExists      = fmt.Errorf("%w: provider model already exists", ErrConflict)
	ErrModelInUse              = fmt.Errorf("%w: provider model has active runtime work", ErrConflict)
	ErrProviderGatewayRequired = errors.New("provider gateway required")
)

type CatalogError struct {
	Code    string
	Message string
}

func (e CatalogError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return e.Code
}

const (
	CodeAuthFailed                      = "AUTH_FAILED"
	CodeQuotaExceeded                   = "QUOTA_EXCEEDED"
	CodeRateLimited                     = "RATE_LIMITED"
	CodeModelNotFound                   = "MODEL_NOT_FOUND"
	CodeInvalidRequest                  = "INVALID_REQUEST"
	CodeUnsupportedCapability           = "UNSUPPORTED_CAPABILITY"
	CodeUpstreamTimeout                 = "UPSTREAM_TIMEOUT"
	CodeUpstreamStreamTruncated         = "UPSTREAM_STREAM_TRUNCATED"
	CodeUpstreamInternalError           = "UPSTREAM_INTERNAL_ERROR"
	CodePollingTimeout                  = "POLLING_TIMEOUT"
	CodeResultExpired                   = "RESULT_EXPIRED"
	CodeMediaDownloadFailed             = "MEDIA_DOWNLOAD_FAILED"
	CodeUpstreamOutputMismatch          = "UPSTREAM_OUTPUT_MISMATCH"
	CodeContentRejected                 = "CONTENT_REJECTED"
	CodeProviderGatewayRequired         = "PROVIDER_GATEWAY_REQUIRED"
	CodeCannotCancelCompletedTask       = "CANNOT_CANCEL_COMPLETED_TASK"
	CodeProviderTaskNotFound            = "PROVIDER_TASK_NOT_FOUND"
	CodeProviderCancelFailed            = "PROVIDER_CANCEL_FAILED"
	CodeProviderRateLimited             = "PROVIDER_RATE_LIMITED"
	CodeProviderConcurrencyLimited      = "PROVIDER_CONCURRENCY_LIMITED"
	CodeProviderDailyQuotaExceeded      = "PROVIDER_DAILY_QUOTA_EXCEEDED"
	CodeProviderMonthlyBudgetExceeded   = "PROVIDER_MONTHLY_BUDGET_EXCEEDED"
	CodeProviderCircuitOpen             = "PROVIDER_CIRCUIT_OPEN"
	CodeProviderLeaseExpired            = "PROVIDER_LEASE_EXPIRED"
	CodeModelProfileNotConfigured       = "MODEL_PROFILE_NOT_CONFIGURED"
	CodeProviderPresetNotFound          = "PROVIDER_PRESET_NOT_FOUND"
	CodeProviderInstallFailed           = "PROVIDER_INSTALL_FAILED"
	CodeProviderManifestInvalid         = "PROVIDER_MANIFEST_INVALID"
	CodeProviderModelTemplateInvalid    = "PROVIDER_MODEL_TEMPLATE_INVALID"
	CodeProviderSetupFieldMissing       = "PROVIDER_SETUP_FIELD_MISSING"
	CodeProviderIdempotencyConflict     = "PROVIDER_IDEMPOTENCY_CONFLICT"
	CodeProviderRequestInProgress       = "PROVIDER_REQUEST_IN_PROGRESS"
	CodeProviderUnknownOutcome          = "PROVIDER_UNKNOWN_OUTCOME"
	CodeModelCapabilityUnavailable      = "MODEL_CAPABILITY_UNAVAILABLE"
	CodeRenderPlanReplanRequired        = "RENDER_PLAN_REPLAN_REQUIRED"
	CodeStoryboardReplanRequired        = "STORYBOARD_REPLAN_REQUIRED"
	CodeProductionGenerationMismatch    = "PRODUCTION_GENERATION_MISMATCH"
	CodeProductionProfileIncompatible   = "PRODUCTION_PROFILE_INCOMPATIBLE"
	CodeModelInputContractUnsupported   = "MODEL_INPUT_CONTRACT_UNSUPPORTED"
	CodeModelCapabilityApprovalRequired = "MODEL_CAPABILITY_APPROVAL_REQUIRED"
	CodeVideoDialogueContractViolation  = "VIDEO_DIALOGUE_CONTRACT_VIOLATION"
	CodeUnknownError                    = "UNKNOWN_ERROR"
)

type StandardError struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Retryable      bool   `json:"retryable"`
	RetryAfterMs   int    `json:"retryAfterMs,omitempty"`
	UpstreamStatus int    `json:"upstreamStatus,omitempty"`
	UpstreamCode   string `json:"upstreamCode,omitempty"`
}

type StandardErrorError struct {
	Standard StandardError
}

func (e *StandardErrorError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Standard.Message) != "" {
		return e.Standard.Message
	}
	if strings.TrimSpace(e.Standard.Code) != "" {
		return e.Standard.Code
	}
	return "provider request failed"
}

func StandardErrorFromError(err error) (*StandardError, bool) {
	var standardErr *StandardErrorError
	if errors.As(err, &standardErr) && standardErr != nil {
		standard := standardErr.Standard
		return &standard, true
	}
	return nil, false
}

func HTTPStatusForStandardError(standard *StandardError) int {
	if standard == nil {
		return http.StatusBadGateway
	}
	switch standard.Code {
	case CodeRateLimited, CodeProviderRateLimited, CodeProviderConcurrencyLimited, CodeProviderCircuitOpen:
		return http.StatusTooManyRequests
	case CodeQuotaExceeded, CodeProviderDailyQuotaExceeded, CodeProviderMonthlyBudgetExceeded:
		return http.StatusPaymentRequired
	case CodeUpstreamTimeout, CodePollingTimeout:
		return http.StatusGatewayTimeout
	case CodeUpstreamStreamTruncated:
		return http.StatusBadGateway
	case CodeInvalidRequest, CodeUnsupportedCapability, CodeModelNotFound, CodeContentRejected, CodeProviderTaskNotFound, CodeCannotCancelCompletedTask, CodeModelCapabilityUnavailable, CodeProductionProfileIncompatible, CodeModelInputContractUnsupported, CodeModelCapabilityApprovalRequired, CodeVideoDialogueContractViolation:
		return http.StatusUnprocessableEntity
	case CodeRenderPlanReplanRequired, CodeStoryboardReplanRequired, CodeProductionGenerationMismatch, CodeProviderIdempotencyConflict, CodeProviderRequestInProgress, CodeProviderUnknownOutcome:
		return http.StatusConflict
	case CodeProviderGatewayRequired:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

type UpstreamError struct {
	Status  int
	Code    string
	Message string
	Body    string
}

func (e *UpstreamError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("provider upstream error: status=%d code=%s", e.Status, e.Code)
	}
	return fmt.Sprintf("provider upstream error: status=%d", e.Status)
}

func NormalizeHTTPError(status int, upstreamCode string) StandardError {
	return NormalizeUpstreamError(&UpstreamError{Status: status, Code: upstreamCode})
}

func NormalizeUpstreamError(upstream *UpstreamError) StandardError {
	if upstream == nil {
		return StandardError{Code: CodeUnknownError, Message: "provider request failed", Retryable: false}
	}
	status := upstream.Status
	upstreamCode := strings.TrimSpace(upstream.Code)
	upstreamMessage := normalizeUpstreamMessage(upstream.Message)
	normalizedUpstreamCode := strings.ToLower(strings.TrimSpace(upstreamCode))
	err := StandardError{
		Code:           CodeUnknownError,
		Message:        "provider request failed",
		Retryable:      false,
		UpstreamStatus: status,
		UpstreamCode:   upstreamCode,
	}

	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		err.Code = CodeAuthFailed
		err.Message = "provider authentication failed"
	case status == http.StatusNotFound:
		err.Code = CodeModelNotFound
		err.Message = "provider model was not found"
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		err.Code = CodeInvalidRequest
		err.Message = "provider rejected the request"
	case status == http.StatusTooManyRequests:
		err.Code = CodeRateLimited
		err.Message = "provider rate limit was exceeded"
		err.Retryable = true
		err.RetryAfterMs = 30000
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		err.Code = CodeUpstreamTimeout
		err.Message = "provider request timed out"
		err.Retryable = true
	case status >= 500 && status <= 599:
		err.Code = CodeUpstreamInternalError
		err.Message = "provider returned an internal error"
		err.Retryable = true
	}

	if strings.Contains(normalizedUpstreamCode, "quota") || strings.Contains(normalizedUpstreamCode, "insufficient") {
		err.Code = CodeQuotaExceeded
		err.Message = "provider quota was exceeded"
		err.Retryable = false
	}
	if containsUpstreamContentRejectionSignal(upstreamCode, upstreamMessage) {
		err.Code = CodeContentRejected
		if upstreamMessage != "" {
			err.Message = upstreamMessage
		} else {
			err.Message = "provider rejected the content"
		}
		err.Retryable = false
	} else if (status == http.StatusBadRequest || status == http.StatusUnprocessableEntity) && upstreamMessage != "" {
		err.Message = upstreamMessage
	}

	return err
}

func containsUpstreamContentRejectionSignal(code, message string) bool {
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	for _, signal := range []string{"content", "moderation", "safety"} {
		if strings.Contains(normalizedCode, signal) {
			return true
		}
	}
	normalizedMessage := strings.ToLower(strings.TrimSpace(message))
	for _, signal := range []string{
		"moderation", "safety", "guardrail", "violate", "violence",
		"content policy", "policy violation", "blocked prompt", "prohibited",
	} {
		if strings.Contains(normalizedMessage, signal) {
			return true
		}
	}
	return false
}

func normalizeUpstreamMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	const maxRunes = 1200
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return value
}
