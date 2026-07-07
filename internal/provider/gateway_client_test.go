package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayClientStreamTextCollectsDeltasAndCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/text/stream" {
			t.Fatalf("path = %s, want /internal/provider/text/stream", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("Accept = %q, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: provider.delta\n")
		fmt.Fprint(w, "data: {\"text\":\"hello\"}\n\n")
		fmt.Fprint(w, "event: provider.delta\n")
		fmt.Fprint(w, "data: {\"text\":\" world\"}\n\n")
		fmt.Fprint(w, "event: provider.completed\n")
		fmt.Fprint(w, "data: {\"providerCallId\":\"call-1\",\"modelId\":\"model-1\",\"status\":\"succeeded\",\"output\":{\"text\":\"hello world\"},\"usage\":{\"estimatedCost\":\"0.00000000\"}}\n\n")
	}))
	defer server.Close()

	client := &GatewayClient{BaseURL: server.URL, Client: server.Client()}
	var streamed strings.Builder
	response, err := client.StreamText(context.Background(), GatewayTextRequest{OrganizationID: "org-1"}, func(delta GatewayTextDelta) error {
		streamed.WriteString(delta.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if streamed.String() != "hello world" {
		t.Fatalf("streamed text = %q, want hello world", streamed.String())
	}
	if response.ProviderCallID != "call-1" || response.Output.Text != "hello world" {
		t.Fatalf("response = %+v, want completed response", response)
	}
}
