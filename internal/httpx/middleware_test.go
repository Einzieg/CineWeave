package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithRecoveryReturnsStructuredError(t *testing.T) {
	handler := WithRequestID(WithRecovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("snapshot context is missing")
	})))
	request := httptest.NewRequest(http.MethodPost, "/api/projects/project-1/asset-batches", nil)
	request.Header.Set(requestIDHeader, "req_test_recovery")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if got := response.Header().Get(requestIDHeader); got != "req_test_recovery" {
		t.Fatalf("request id header = %q", got)
	}
	var envelope Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.RequestID != "req_test_recovery" || envelope.Error == nil || envelope.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("response envelope = %+v", envelope)
	}
}

func TestWithCORSAllowsCurrentWebOriginAndRealtimeHeaders(t *testing.T) {
	t.Setenv("CINEWEAVE_CORS_ORIGINS", "")
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodOptions, "/api/realtime/events", nil)
	request.Header.Set("Origin", "http://localhost:19285")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Cache-Control, Last-Event-ID")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:19285" {
		t.Fatalf("allow origin = %q", got)
	}
	for _, required := range []string{"Authorization", "Cache-Control", "Last-Event-ID"} {
		if !headerListContains(response.Header().Get("Access-Control-Allow-Headers"), required) {
			t.Fatalf("allow headers %q does not include %q", response.Header().Get("Access-Control-Allow-Headers"), required)
		}
	}
	if !headerListContains(response.Header().Get("Access-Control-Expose-Headers"), "X-CineWeave-Stream-High-Watermark") {
		t.Fatalf("expose headers = %q", response.Header().Get("Access-Control-Expose-Headers"))
	}
}

func TestWithCORSDoesNotAuthorizeUnknownOrigin(t *testing.T) {
	t.Setenv("CINEWEAVE_CORS_ORIGINS", "http://localhost:19285")
	handler := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events", nil)
	request.Header.Set("Origin", "https://untrusted.example")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow origin %q", got)
	}
}

func headerListContains(raw, expected string) bool {
	for _, value := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}
