package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestOpenAICompatibleVideoPollTimeoutIsIndependent(t *testing.T) {
	cfg := parseOpenAICompatibleConfig(json.RawMessage(`{"timeoutMs":120000}`))
	if cfg.TimeoutMS != 120000 {
		t.Fatalf("request timeout = %d, want 120000", cfg.TimeoutMS)
	}
	if cfg.VideoPollTimeoutMS != defaultOpenAIVideoPollTimeoutMS {
		t.Fatalf("poll timeout = %d, want %d", cfg.VideoPollTimeoutMS, defaultOpenAIVideoPollTimeoutMS)
	}

	cfg = parseOpenAICompatibleConfig(json.RawMessage(`{"timeoutMs":120000,"videoPollTimeoutMs":45000}`))
	if cfg.VideoPollTimeoutMS != 45000 {
		t.Fatalf("explicit poll timeout = %d, want 45000", cfg.VideoPollTimeoutMS)
	}
}

func TestNormalizeGatewayVideoPollFailureKeepsRetryableTaskRunning(t *testing.T) {
	outcome := normalizeGatewayVideoPollFailure(context.DeadlineExceeded)
	if outcome.TaskStatus != "running" || outcome.CallStatus != "failed" {
		t.Fatalf("outcome status = task:%s call:%s", outcome.TaskStatus, outcome.CallStatus)
	}
	if outcome.ErrorCode != CodeUpstreamTimeout || outcome.ResponseError != nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestNormalizeGatewayVideoPollFailurePreservesTerminalFailure(t *testing.T) {
	outcome := normalizeGatewayVideoPollFailure(fmt.Errorf("%w: invalid poll contract", ErrValidation))
	if outcome.TaskStatus != "failed" || outcome.CallStatus != "failed" {
		t.Fatalf("outcome status = task:%s call:%s", outcome.TaskStatus, outcome.CallStatus)
	}
	if outcome.ErrorCode != CodeInvalidRequest || outcome.ResponseError == nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestGatewayVideoObservedCallStatusSeparatesTaskProgress(t *testing.T) {
	for _, status := range []string{"queued", "running"} {
		if got := gatewayVideoObservedCallStatus(status); got != "succeeded" {
			t.Fatalf("call status for task %q = %q", status, got)
		}
	}
	for _, status := range []string{"succeeded", "failed", "cancelled"} {
		if got := gatewayVideoObservedCallStatus(status); got != status {
			t.Fatalf("call status for task %q = %q", status, got)
		}
	}
}
