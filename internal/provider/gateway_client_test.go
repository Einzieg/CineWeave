package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayClientStreamTextV2CollectsAndDeduplicatesDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/stream" {
			t.Fatalf("path = %s, want /internal/provider/text/stream", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("Accept = %q, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSE(w, GatewayTextEventAttemptStarted, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":2,"attemptSequence":1,"providerModelId":"model-1","status":"running"}`)
		writeTestSSE(w, GatewayTextEventDelta, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":2,"attemptSequence":1,"sequence":1,"text":"hello","finishReason":null}`)
		writeTestSSE(w, GatewayTextEventDelta, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":2,"attemptSequence":1,"sequence":1,"text":"hello","finishReason":null}`)
		writeTestSSE(w, GatewayTextEventDelta, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":2,"attemptSequence":1,"sequence":2,"text":" world","finishReason":null}`)
		writeTestSSE(w, GatewayTextEventCompleted, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":2,"attemptSequence":1,"modelId":"model-1","status":"succeeded","output":{"text":"hello world"},"usage":{"estimatedCost":"0.00000000"}}`)
	}))
	defer server.Close()

	client := &GatewayClient{BaseURL: server.URL, Client: server.Client()}
	var streamed strings.Builder
	var lastSequence int64
	response, err := client.StreamText(context.Background(), GatewayTextRequest{OrganizationID: "org-1"}, func(delta GatewayTextDelta) error {
		if delta.SchemaVersion != 2 || delta.ProviderRequestID != "request-1" || delta.ProviderCallID != "call-1" || delta.AttemptGeneration != 2 || delta.AttemptSequence != 1 || delta.Sequence != lastSequence+1 {
			t.Fatalf("delta metadata = %+v, last sequence = %d", delta, lastSequence)
		}
		lastSequence = delta.Sequence
		streamed.WriteString(delta.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if streamed.String() != "hello world" || lastSequence != 2 {
		t.Fatalf("streamed text = %q sequence=%d, want deduplicated hello world/2", streamed.String(), lastSequence)
	}
	if response.ProviderRequestID != "request-1" || response.ProviderCallID != "call-1" || response.AttemptGeneration != 2 || response.AttemptSequence != 1 || response.Output.Text != "hello world" {
		t.Fatalf("response = %+v, want completed v2 response", response)
	}
}

func TestGatewayClientStreamTextV2ReplayedSnapshotDoesNotEmitDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSE(w, GatewayTextEventReplayed, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":1,"attemptSequence":2,"modelId":"model-1","status":"succeeded","output":{"text":"snapshot"},"usage":{"estimatedCost":"0.00000000"}}`)
	}))
	defer server.Close()

	client := &GatewayClient{BaseURL: server.URL, Client: server.Client()}
	deltaCount := 0
	response, err := client.StreamText(context.Background(), GatewayTextRequest{OrganizationID: "org-1"}, func(GatewayTextDelta) error {
		deltaCount++
		return nil
	})
	if err != nil {
		t.Fatalf("StreamText replay: %v", err)
	}
	if deltaCount != 0 || response.Output.Text != "snapshot" || response.AttemptSequence != 2 {
		t.Fatalf("replay deltaCount=%d response=%+v", deltaCount, response)
	}
}

func TestGatewayClientStreamTextV2RejectsFallbackAfterDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSE(w, GatewayTextEventDelta, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":1,"attemptSequence":1,"sequence":1,"text":"partial","finishReason":null}`)
		writeTestSSE(w, GatewayTextEventDelta, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-2","attemptGeneration":1,"attemptSequence":2,"sequence":1,"text":"replacement","finishReason":null}`)
	}))
	defer server.Close()

	client := &GatewayClient{BaseURL: server.URL, Client: server.Client()}
	_, err := client.StreamText(context.Background(), GatewayTextRequest{OrganizationID: "org-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "changed attempts") {
		t.Fatalf("StreamText() error = %v, want attempt change rejection", err)
	}
}

func TestGatewayClientStreamTextV2ReturnsProviderFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSE(w, GatewayTextEventDelta, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":1,"attemptSequence":1,"sequence":1,"text":"partial","finishReason":null}`)
		writeTestSSE(w, GatewayTextEventFailed, `{"schemaVersion":2,"providerRequestId":"request-1","providerCallId":"call-1","attemptGeneration":1,"attemptSequence":1,"error":{"code":"UPSTREAM_STREAM_TRUNCATED","message":"provider stream ended before a completion marker","retryable":true}}`)
	}))
	defer server.Close()

	client := &GatewayClient{BaseURL: server.URL, Client: server.Client()}
	_, err := client.StreamText(context.Background(), GatewayTextRequest{OrganizationID: "org-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "completion marker") {
		t.Fatalf("StreamText() error = %v", err)
	}
}

func writeTestSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}
