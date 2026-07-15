package provider

import "testing"

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
