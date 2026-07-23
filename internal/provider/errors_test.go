package provider

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type providerTimeoutError struct{}

func (providerTimeoutError) Error() string   { return "TLS handshake timeout" }
func (providerTimeoutError) Timeout() bool   { return true }
func (providerTimeoutError) Temporary() bool { return true }

func TestNormalizeHTTPError(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		upstream  string
		wantCode  string
		retryable bool
	}{
		{name: "auth", status: http.StatusUnauthorized, wantCode: CodeAuthFailed},
		{name: "rate limited", status: http.StatusTooManyRequests, wantCode: CodeRateLimited, retryable: true},
		{name: "quota override", status: http.StatusTooManyRequests, upstream: "quota_exceeded", wantCode: CodeQuotaExceeded},
		{name: "not found", status: http.StatusNotFound, wantCode: CodeModelNotFound},
		{name: "server", status: http.StatusBadGateway, wantCode: CodeUpstreamInternalError, retryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeHTTPError(tt.status, tt.upstream)
			if got.Code != tt.wantCode {
				t.Fatalf("Code = %s, want %s", got.Code, tt.wantCode)
			}
			if got.Retryable != tt.retryable {
				t.Fatalf("Retryable = %v, want %v", got.Retryable, tt.retryable)
			}
		})
	}
}

func TestNormalizeUpstreamErrorPreservesSafeClientMessage(t *testing.T) {
	standard := NormalizeUpstreamError(&UpstreamError{
		Status:  http.StatusUnprocessableEntity,
		Message: "The requested image size is unsupported.",
	})
	if standard.Code != CodeInvalidRequest || standard.Message != "The requested image size is unsupported." {
		t.Fatalf("standard = %#v, want upstream client message", standard)
	}
}

func TestNormalizeUpstreamErrorDoesNotMisclassifyContentLength(t *testing.T) {
	standard := NormalizeUpstreamError(&UpstreamError{
		Status:  http.StatusBadRequest,
		Message: "request content length exceeds the model limit",
	})
	if standard.Code != CodeInvalidRequest || standard.Message != "request content length exceeds the model limit" {
		t.Fatalf("standard = %#v, want invalid request", standard)
	}
}

func TestNormalizeUpstreamErrorDoesNotExposeAuthenticationMessage(t *testing.T) {
	standard := NormalizeUpstreamError(&UpstreamError{
		Status:  http.StatusUnauthorized,
		Message: "API key sk-secret-value is invalid",
	})
	if standard.Code != CodeAuthFailed || standard.Message != "provider authentication failed" {
		t.Fatalf("standard = %#v, want generic authentication error", standard)
	}
}

func TestNormalizedProviderFailureClassifiesTransportEOF(t *testing.T) {
	status, code, message, upstreamStatus, upstreamCode := normalizedProviderFailure(errors.New(`Post "https://example.test/v1/images/generations": EOF`))
	if status != "failed" || code != CodeUpstreamInternalError || message != "provider connection was interrupted" {
		t.Fatalf("normalized EOF = status=%s code=%s message=%q", status, code, message)
	}
	if upstreamStatus != nil || upstreamCode != "" {
		t.Fatalf("upstream status/code = %v/%q, want empty", upstreamStatus, upstreamCode)
	}
	status, code, message, _, _ = normalizedProviderFailure(io.ErrUnexpectedEOF)
	if status != "failed" || code != CodeUpstreamStreamTruncated || message != "provider stream ended before a completion marker" {
		t.Fatalf("normalized unexpected EOF = status=%s code=%s message=%q", status, code, message)
	}
	if !standardErrorFromRunError(io.ErrUnexpectedEOF, CodeUpstreamStreamTruncated, "provider stream ended before a completion marker").Retryable {
		t.Fatalf("unexpected EOF standard error should be retryable")
	}
}

func TestNormalizedProviderFailureClassifiesTransportTimeout(t *testing.T) {
	raw := fmt.Errorf(`Post "https://example.test/v1/chat/completions": %w`, providerTimeoutError{})
	status, code, message, upstreamStatus, upstreamCode := normalizedProviderFailure(raw)
	if status != "failed" || code != CodeUpstreamTimeout || message != "provider request timed out" {
		t.Fatalf("normalized timeout = status=%s code=%s message=%q", status, code, message)
	}
	if upstreamStatus != nil || upstreamCode != "" {
		t.Fatalf("upstream status/code = %v/%q, want empty", upstreamStatus, upstreamCode)
	}
	standard := standardErrorFromRunError(raw, code, message)
	if standard == nil || !standard.Retryable {
		t.Fatalf("transport timeout standard error = %#v, want retryable", standard)
	}
}

func TestNormalizedProviderFailureClassifiesValidation(t *testing.T) {
	status, code, message, upstreamStatus, upstreamCode := normalizedProviderFailure(fmt.Errorf("%w: public reference URL is required", ErrValidation))
	if status != "failed" || code != CodeInvalidRequest || message != "provider validation failed: public reference URL is required" {
		t.Fatalf("normalized validation = status=%s code=%s message=%q", status, code, message)
	}
	if upstreamStatus != nil || upstreamCode != "" {
		t.Fatalf("upstream status/code = %v/%q, want empty", upstreamStatus, upstreamCode)
	}
}
