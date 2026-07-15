package workflows

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Einzieg/cineweave/internal/provider"
)

func TestGenerateProviderTextDoesNotFallbackAfterFirstDelta(t *testing.T) {
	var generateCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/provider/text/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: provider.delta\n")
			fmt.Fprint(w, "data: {\"attemptGeneration\":1,\"attemptSequence\":1,\"sequence\":1,\"text\":\"partial\"}\n\n")
			fmt.Fprint(w, "event: provider.failed\n")
			fmt.Fprint(w, "data: {\"code\":\"INVALID_REQUEST\",\"message\":\"stream endpoint became unavailable\",\"retryable\":true}\n\n")
		case "/internal/provider/text/generate":
			generateCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"status":"succeeded","output":{"text":"duplicated"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	activities := Activities{gateway: &provider.GatewayClient{BaseURL: server.URL, Client: server.Client()}}
	execution := NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1}
	_, err := activities.generateProviderText(context.Background(), execution, provider.GatewayTextRequest{
		OrganizationID: "org", NodeRunID: execution.NodeRunID, Input: []byte(`{"prompt":"hello"}`),
	})
	if err == nil {
		t.Fatal("post-delta stream failure was hidden by a non-streaming fallback")
	}
	if calls := generateCalls.Load(); calls != 0 {
		t.Fatalf("non-streaming fallback calls=%d after a delta, want 0", calls)
	}
}

func TestGenerateProviderTextMayFallbackBeforeFirstDelta(t *testing.T) {
	var generateCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/provider/text/stream":
			http.NotFound(w, r)
		case "/internal/provider/text/generate":
			generateCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"status":"succeeded","output":{"text":"fallback"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	activities := Activities{gateway: &provider.GatewayClient{BaseURL: server.URL, Client: server.Client()}}
	execution := NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1}
	response, err := activities.generateProviderText(context.Background(), execution, provider.GatewayTextRequest{
		OrganizationID: "org", NodeRunID: execution.NodeRunID, Input: []byte(`{"prompt":"hello"}`),
	})
	if err != nil {
		t.Fatalf("pre-delta fallback failed: %v", err)
	}
	if response.Output.Text != "fallback" || generateCalls.Load() != 1 {
		t.Fatalf("response=%+v generateCalls=%d", response, generateCalls.Load())
	}
}
