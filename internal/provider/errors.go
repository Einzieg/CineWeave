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
	ErrProviderGatewayRequired = errors.New("provider gateway required")
)

const (
	CodeAuthFailed                    = "AUTH_FAILED"
	CodeQuotaExceeded                 = "QUOTA_EXCEEDED"
	CodeRateLimited                   = "RATE_LIMITED"
	CodeModelNotFound                 = "MODEL_NOT_FOUND"
	CodeInvalidRequest                = "INVALID_REQUEST"
	CodeUnsupportedCapability         = "UNSUPPORTED_CAPABILITY"
	CodeUpstreamTimeout               = "UPSTREAM_TIMEOUT"
	CodeUpstreamInternalError         = "UPSTREAM_INTERNAL_ERROR"
	CodePollingTimeout                = "POLLING_TIMEOUT"
	CodeResultExpired                 = "RESULT_EXPIRED"
	CodeMediaDownloadFailed           = "MEDIA_DOWNLOAD_FAILED"
	CodeContentRejected               = "CONTENT_REJECTED"
	CodeProviderGatewayRequired       = "PROVIDER_GATEWAY_REQUIRED"
	CodeCannotCancelCompletedTask     = "CANNOT_CANCEL_COMPLETED_TASK"
	CodeProviderTaskNotFound          = "PROVIDER_TASK_NOT_FOUND"
	CodeProviderCancelFailed          = "PROVIDER_CANCEL_FAILED"
	CodeProviderRateLimited           = "PROVIDER_RATE_LIMITED"
	CodeProviderConcurrencyLimited    = "PROVIDER_CONCURRENCY_LIMITED"
	CodeProviderDailyQuotaExceeded    = "PROVIDER_DAILY_QUOTA_EXCEEDED"
	CodeProviderMonthlyBudgetExceeded = "PROVIDER_MONTHLY_BUDGET_EXCEEDED"
	CodeProviderCircuitOpen           = "PROVIDER_CIRCUIT_OPEN"
	CodeProviderLeaseExpired          = "PROVIDER_LEASE_EXPIRED"
	CodeUnknownError                  = "UNKNOWN_ERROR"
)

type StandardError struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Retryable      bool   `json:"retryable"`
	RetryAfterMs   int    `json:"retryAfterMs,omitempty"`
	UpstreamStatus int    `json:"upstreamStatus,omitempty"`
	UpstreamCode   string `json:"upstreamCode,omitempty"`
}

type UpstreamError struct {
	Status int
	Code   string
	Body   string
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
	if strings.Contains(normalizedUpstreamCode, "content") || strings.Contains(normalizedUpstreamCode, "moderation") || strings.Contains(normalizedUpstreamCode, "safety") {
		err.Code = CodeContentRejected
		err.Message = "provider rejected the content"
		err.Retryable = false
	}

	return err
}
