package workflows

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
)

func TestProviderTextGatewayOptionsRetryOnlyAfterFirstActivityAttempt(t *testing.T) {
	initial := provider.GatewayTextOptions{IdempotencyKey: "request-1"}
	first := providerTextGatewayOptionsForAttempt(initial, 1)
	if first.TimeoutMS != providerTextGatewayTimeoutMS || first.Retry {
		t.Fatalf("first attempt options = %+v", first)
	}
	if first.IdempotencyKey != initial.IdempotencyKey {
		t.Fatalf("first attempt lost idempotency key: %+v", first)
	}

	retry := providerTextGatewayOptionsForAttempt(first, 2)
	if !retry.Retry || retry.TimeoutMS != providerTextGatewayTimeoutMS || retry.IdempotencyKey != initial.IdempotencyKey {
		t.Fatalf("retry attempt options = %+v", retry)
	}
}

func TestCommerceAgentActivityOptionsRetryTransientFailures(t *testing.T) {
	options := commerceAgentActivityOptions()
	if options.RetryPolicy == nil || options.RetryPolicy.MaximumAttempts != 3 {
		t.Fatalf("commerce agent retry policy = %#v, want 3 attempts", options.RetryPolicy)
	}
}

func TestCommerceVideoPromptItemActivityOptionsCoverSupervisedRounds(t *testing.T) {
	options := commerceVideoPromptItemActivityOptions()
	if options.StartToCloseTimeout != 75*time.Minute {
		t.Fatalf("commerce video prompt timeout = %s, want 75m", options.StartToCloseTimeout)
	}
	if options.HeartbeatTimeout != providerTextHeartbeatTimeout {
		t.Fatalf("commerce video prompt heartbeat = %s, want %s", options.HeartbeatTimeout, providerTextHeartbeatTimeout)
	}
	if options.RetryPolicy == nil || options.RetryPolicy.MaximumAttempts != 1 {
		t.Fatalf("commerce video prompt retry policy = %#v, want one explicit attempt", options.RetryPolicy)
	}
}

func TestCommerceProjectSetupActivityOptionsCoverSupervisedSetup(t *testing.T) {
	options := commerceProjectSetupActivityOptions()
	if options.StartToCloseTimeout != 2*time.Hour {
		t.Fatalf("commerce project setup timeout = %s, want 2h", options.StartToCloseTimeout)
	}
	if options.HeartbeatTimeout != providerTextHeartbeatTimeout {
		t.Fatalf("commerce project setup heartbeat = %s, want %s", options.HeartbeatTimeout, providerTextHeartbeatTimeout)
	}
	if options.RetryPolicy == nil || options.RetryPolicy.MaximumAttempts != 3 {
		t.Fatalf("commerce project setup retry policy = %#v, want three idempotent attempts", options.RetryPolicy)
	}
}

func TestCommerceProjectSetupTimeoutIsNormalized(t *testing.T) {
	cause := temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)
	code, message := commerceProjectSetupErrorFields(cause)
	if code != provider.CodeUpstreamTimeout {
		t.Fatalf("code = %q, want %q", code, provider.CodeUpstreamTimeout)
	}
	if message != "项目准备等待模型响应超时，请重试；已完成的供应商请求会自动复用" {
		t.Fatalf("message = %q", message)
	}
}

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

func TestGenerateProviderImageWaitsForInProgressRequestReplay(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/provider/image/generate" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"data":{"status":"running","providerRequestId":"request-1","error":{"code":"PROVIDER_REQUEST_IN_PROGRESS","message":"request is running","retryable":true,"retryAfterMs":1}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"status":"succeeded","providerRequestId":"request-1","providerCallId":"call-1","modelId":"model-1","output":{"artifactId":"artifact-1","mediaFileId":"media-1","storageKey":"images/result.png"}}}`)
	}))
	defer server.Close()

	activities := Activities{gateway: &provider.GatewayClient{BaseURL: server.URL, Client: server.Client()}}
	execution := NodeExecution{NodeRunID: "node-1", ExecutionToken: "token-1", AttemptGeneration: 1}
	response, err := activities.generateProviderImage(context.Background(), execution, provider.GatewayImageRequest{
		OrganizationID: "org", NodeRunID: execution.NodeRunID, IdempotencyKey: "image-1",
	})
	if err != nil {
		t.Fatalf("generate provider image: %v", err)
	}
	if calls.Load() != 2 || response.ProviderCallID != "call-1" || response.Output.ArtifactID != "artifact-1" {
		t.Fatalf("calls=%d response=%+v", calls.Load(), response)
	}
}
