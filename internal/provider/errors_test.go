package provider

import (
	"net/http"
	"testing"
)

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
