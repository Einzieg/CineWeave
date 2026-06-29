package provider

import (
	"context"
	"errors"
	"testing"
)

func TestProviderGatewayRequiredByDefault(t *testing.T) {
	t.Setenv("CINEWEAVE_ENV", "development")
	t.Setenv("CINEWEAVE_ALLOW_PROVIDER_DIRECT_FALLBACK", "")
	service := NewService(nil, nil)

	if _, err := service.DiscoverModels(context.Background(), "org", "account"); !errors.Is(err, ErrProviderGatewayRequired) {
		t.Fatalf("DiscoverModels error = %v, want ErrProviderGatewayRequired", err)
	}
	if _, err := service.RecordProviderModelTest(context.Background(), "org", "user", "model", TestProviderModelRequest{TestType: "text_generation_test"}); !errors.Is(err, ErrProviderGatewayRequired) {
		t.Fatalf("RecordProviderModelTest error = %v, want ErrProviderGatewayRequired", err)
	}
	if _, err := service.RecordProviderModelTest(context.Background(), "org", "user", "model", TestProviderModelRequest{TestType: "image_generation_test"}); !errors.Is(err, ErrProviderGatewayRequired) {
		t.Fatalf("RecordProviderModelTest image error = %v, want ErrProviderGatewayRequired", err)
	}
	if _, err := service.RecordProviderModelTest(context.Background(), "org", "user", "model", TestProviderModelRequest{TestType: "video_generation_test"}); !errors.Is(err, ErrProviderGatewayRequired) {
		t.Fatalf("RecordProviderModelTest video error = %v, want ErrProviderGatewayRequired", err)
	}
	if _, err := service.RunManifestTest(context.Background(), "org", "user", ManifestTestRunRequest{}); !errors.Is(err, ErrProviderGatewayRequired) {
		t.Fatalf("RunManifestTest error = %v, want ErrProviderGatewayRequired", err)
	}
}

func TestProviderDirectFallbackRequiresDevelopmentOrTest(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "development", env: "development", want: true},
		{name: "test", env: "test", want: true},
		{name: "production", env: "production", want: false},
		{name: "empty env", env: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerDirectFallbackAllowed("true", tt.env); got != tt.want {
				t.Fatalf("providerDirectFallbackAllowed(true, %q) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
	if providerDirectFallbackAllowed("false", "development") {
		t.Fatal("fallback was allowed when flag is false")
	}
}
