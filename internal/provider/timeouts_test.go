package provider

import (
	"testing"
	"time"
)

func TestGatewayImageTimeoutMSFromEnv(t *testing.T) {
	t.Setenv("CINEWEAVE_PROVIDER_IMAGE_TIMEOUT_MS", "90s")
	if got := gatewayImageTimeoutMSFromEnv(); got != 90000 {
		t.Fatalf("gatewayImageTimeoutMSFromEnv() = %d, want 90000", got)
	}

	t.Setenv("CINEWEAVE_PROVIDER_IMAGE_TIMEOUT_MS", "120000")
	if got := gatewayImageTimeoutMSFromEnv(); got != 120000 {
		t.Fatalf("gatewayImageTimeoutMSFromEnv() = %d, want 120000", got)
	}
}

func TestGatewayImageTimeoutMSFromEnvFallback(t *testing.T) {
	t.Setenv("CINEWEAVE_PROVIDER_IMAGE_TIMEOUT_MS", "invalid")
	if got := gatewayImageTimeoutMSFromEnv(); got != defaultGatewayImageTimeoutMS {
		t.Fatalf("gatewayImageTimeoutMSFromEnv() = %d, want default %d", got, defaultGatewayImageTimeoutMS)
	}
}

func TestGatewayImageRequestTimeoutMSFromEnv(t *testing.T) {
	t.Setenv("CINEWEAVE_PROVIDER_IMAGE_REQUEST_TIMEOUT_MS", "20m")
	if got := gatewayImageRequestTimeoutMSFromEnv(); got != 20*60*1000 {
		t.Fatalf("gatewayImageRequestTimeoutMSFromEnv() = %d, want 1200000", got)
	}
}

func TestGatewayImageAttemptTimeoutUsesRemainingRequestBudget(t *testing.T) {
	deadline := time.Now().Add(2 * time.Second)
	got := gatewayImageAttemptTimeout(10*time.Second, deadline)
	if got <= 0 || got > 2*time.Second {
		t.Fatalf("gatewayImageAttemptTimeout() = %s, want remaining request budget", got)
	}
}
