package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEstimateTextCostUsesPricingPolicy(t *testing.T) {
	usage := estimateTextCost(GatewayUsage{InputTokens: 1000, OutputTokens: 500}, []Capability{{
		PricingPolicy: json.RawMessage(`{"currency":"USD","inputTokenPer1K":"0.0100","outputTokenPer1K":"0.0200"}`),
	}})
	if usage.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", usage.Currency)
	}
	if usage.TotalTokens != 1500 {
		t.Fatalf("total tokens = %d, want 1500", usage.TotalTokens)
	}
	if usage.EstimatedCost != "0.02000000" {
		t.Fatalf("estimated cost = %q, want 0.02000000", usage.EstimatedCost)
	}
}

func TestShouldRetryGatewayTextAttempt(t *testing.T) {
	for _, code := range []string{CodeUpstreamTimeout, CodeUpstreamStreamTruncated} {
		if !shouldRetryGatewayTextAttempt(&StandardError{Code: code, Retryable: true}) {
			t.Fatalf("code %s should be retried", code)
		}
	}
	for _, standard := range []*StandardError{
		nil,
		{Code: CodeUpstreamTimeout, Retryable: false},
		{Code: CodeUpstreamInternalError, Retryable: true},
		{Code: CodeInvalidRequest, Retryable: false},
	} {
		if shouldRetryGatewayTextAttempt(standard) {
			t.Fatalf("standard %#v should not be retried", standard)
		}
	}
}

func TestGatewayTextRetryBudgetAndBackoff(t *testing.T) {
	if gatewayTextMaxRetries != 3 || gatewayTextMaxAttemptsPerSelection != 4 {
		t.Fatalf("retry budget = %d retries/%d attempts, want 3/4", gatewayTextMaxRetries, gatewayTextMaxAttemptsPerSelection)
	}
	want := []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, time.Second}
	for retry, expected := range want {
		if got := gatewayTextRetryDelay(retry); got != expected {
			t.Fatalf("retry delay %d = %s, want %s", retry, got, expected)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitGatewayTextRetry(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled retry wait = %v, want context.Canceled", err)
	}

	interrupted := &gatewayTextRetryInterruptedError{cause: context.Canceled}
	if !errors.Is(interrupted, context.Canceled) {
		t.Fatalf("interrupted retry error = %v, want context.Canceled", interrupted)
	}
}
