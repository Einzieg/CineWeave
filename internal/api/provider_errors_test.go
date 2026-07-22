package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
)

func TestProviderModelAlreadyExistsErrorMapping(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/providers/models/model-id", nil)

	var server Server
	server.writeError(recorder, request, provider.ErrModelAlreadyExists)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "PROVIDER_MODEL_ALREADY_EXISTS" {
		t.Fatalf("error = %+v, want PROVIDER_MODEL_ALREADY_EXISTS", envelope.Error)
	}
}

func TestProviderModelInUseErrorMapping(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/providers/models/model-id", nil)

	var server Server
	server.writeError(recorder, request, provider.ErrModelInUse)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "PROVIDER_MODEL_IN_USE" {
		t.Fatalf("error = %+v, want PROVIDER_MODEL_IN_USE", envelope.Error)
	}
}
