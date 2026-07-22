package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func TestProviderConfigurationWriteGateRejectsWritesWhileFrozen(t *testing.T) {
	t.Setenv("CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN", "true")
	server := &Server{}
	called := false
	handler := server.withProviderConfigurationWriteGate(func(http.ResponseWriter, *http.Request, auth.Principal) {
		called = true
	})

	request := httptest.NewRequest(http.MethodPatch, "/api/providers/accounts/provider-1", nil)
	response := httptest.NewRecorder()
	handler(response, request, auth.Principal{})

	if called {
		t.Fatal("provider configuration handler was called while writes were frozen")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != providerConfigurationFrozenCode || !envelope.Error.Retryable {
		t.Fatalf("error envelope = %+v", envelope.Error)
	}
}

func TestProviderConfigurationWriteGateAllowsWritesByDefault(t *testing.T) {
	t.Setenv("CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN", "false")
	server := &Server{}
	called := false
	handler := server.withProviderConfigurationWriteGate(func(http.ResponseWriter, *http.Request, auth.Principal) {
		called = true
	})

	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/providers/accounts", nil), auth.Principal{})

	if !called {
		t.Fatal("provider configuration handler was not called while writes were enabled")
	}
}
