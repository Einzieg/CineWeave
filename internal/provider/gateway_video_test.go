package provider

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestNormalizeGatewayVideoStatus(t *testing.T) {
	tests := map[string]string{
		"pending":     "queued",
		"processing":  "running",
		"in_progress": "running",
		"completed":   "succeeded",
		"done":        "succeeded",
		"error":       "failed",
		"canceled":    "cancelled",
	}
	for input, want := range tests {
		if got := normalizeGatewayVideoStatus(input); got != want {
			t.Fatalf("normalizeGatewayVideoStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSelectVideoEndpointKeys(t *testing.T) {
	manifest := ProviderManifest{Endpoints: map[string]ManifestEndpoint{
		"custom_create": {EndpointType: "async_create"},
		"video_poll":    {EndpointType: "async_poll"},
	}}
	selection := gatewayModelSelection{
		Account: Account{Config: json.RawMessage(`{"videoCreateEndpointKey":"custom_create"}`)},
		Model: Model{Capabilities: []Capability{{
			ProviderOptionsSchema: json.RawMessage(`{"providerOptions":{"videoPollEndpointKey":"video_poll"}}`),
		}}},
	}
	createKey, _, err := selectVideoCreateEndpoint(selection, manifest)
	if err != nil {
		t.Fatalf("selectVideoCreateEndpoint() error = %v", err)
	}
	pollKey, _, err := selectVideoPollEndpoint(selection, manifest, ManifestEndpoint{})
	if err != nil {
		t.Fatalf("selectVideoPollEndpoint() error = %v", err)
	}
	if createKey != "custom_create" || pollKey != "video_poll" {
		t.Fatalf("keys = %s/%s, want custom_create/video_poll", createKey, pollKey)
	}
}

func TestEstimateVideoCostUsesPricingPolicy(t *testing.T) {
	usage := estimateVideoCost(gatewayVideoInput{DurationSeconds: 5, Resolution: "720p"}, nil, []Capability{{
		PricingPolicy: json.RawMessage(`{
			"currency": "USD",
			"videoCostPerSecond": "0.0300",
			"videoCostByResolution": {"720p": "0.0500"},
			"videoCostFlat": "0.2000"
		}`),
	}})
	if usage.Currency != "USD" || usage.EstimatedCost != "0.25000000" {
		t.Fatalf("usage = %+v, want 0.25000000 USD", usage)
	}
}

func TestGatewayVideoStorageKey(t *testing.T) {
	key := gatewayVideoStorageKey("org-1", "project-1", "video/mp4", "")
	if !strings.HasPrefix(key, "org/org-1/project/project-1/provider-videos/") || !strings.HasSuffix(key, ".mp4") {
		t.Fatalf("storage key = %q", key)
	}
	key = gatewayVideoStorageKey("org-1", "", "video/webm", "")
	if !strings.HasPrefix(key, "org/org-1/provider-videos/") || !strings.HasSuffix(key, ".webm") {
		t.Fatalf("storage key without project = %q", key)
	}
}

func TestStoreGatewayVideoMediaRecordsRequestedProviderAndActualTiming(t *testing.T) {
	service := NewService(nil, nil)
	service.SetStorage(newMemoryObjectStorage())
	service.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{
			DurationSeconds:      4.96,
			Width:                1280,
			Height:               720,
			FrameRateNumerator:   24,
			FrameRateDenominator: 1,
			FrameRate:            24,
			FrameCount:           119,
			FrameCountEstimated:  true,
			VideoStreamCount:     1,
			AudioStreamCount:     1,
			HasAudio:             true,
			VideoCodec:           "h264",
			AudioCodecs:          []string{"aac"},
		}, nil
	})
	stored, err := service.storeGatewayVideoMedia(
		context.Background(),
		"call-1",
		"org-1",
		"project-1",
		gatewayModelSelection{Model: Model{ID: "model-1"}},
		"task-1",
		manifestRunResult{NormalizedOutput: json.RawMessage(`{"videoUrl":"https://example.test/video.mp4","durationSeconds":5}`)},
		gatewayVideoMedia{Body: []byte("video bytes"), MimeType: "video/mp4", ByteSize: 11},
		gatewayVideoInput{DurationSeconds: 6, Resolution: "720p", AspectRatio: "16:9"},
	)
	if err != nil {
		t.Fatalf("storeGatewayVideoMedia: %v", err)
	}
	output := stored.Output
	if output.RequestedDurationSeconds == nil || *output.RequestedDurationSeconds != 6 {
		t.Fatalf("requested duration = %#v", output.RequestedDurationSeconds)
	}
	if output.ProviderDurationSeconds == nil || *output.ProviderDurationSeconds != 5 {
		t.Fatalf("provider duration = %#v", output.ProviderDurationSeconds)
	}
	if output.ActualDurationSeconds == nil || math.Abs(*output.ActualDurationSeconds-4.96) > 0.000001 {
		t.Fatalf("actual duration = %#v", output.ActualDurationSeconds)
	}
	if output.DurationSeconds == nil || *output.DurationSeconds != 4.96 || output.DurationSource != "media_probe" {
		t.Fatalf("effective duration = %#v source=%q", output.DurationSeconds, output.DurationSource)
	}
	if output.Width == nil || *output.Width != 1280 || output.Height == nil || *output.Height != 720 {
		t.Fatalf("dimensions = %#v/%#v", output.Width, output.Height)
	}
	if output.MediaProbe == nil || output.MediaProbe.Status != "succeeded" || output.MediaProbe.FrameCount != 119 || !output.MediaProbe.HasAudio {
		t.Fatalf("media probe = %+v", output.MediaProbe)
	}
}

func TestStoreGatewayVideoMediaRejectsWrongAspectBeforeStorage(t *testing.T) {
	service := NewService(nil, nil)
	objectStorage := newMemoryObjectStorage()
	service.SetStorage(objectStorage)
	service.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{Width: 720, Height: 1280}, nil
	})

	_, err := service.storeGatewayVideoMedia(
		context.Background(),
		"call-portrait",
		"org-1",
		"project-1",
		gatewayModelSelection{Model: Model{ID: "model-1"}},
		"task-portrait",
		manifestRunResult{NormalizedOutput: json.RawMessage(`{"videoUrl":"https://example.test/video.mp4"}`)},
		gatewayVideoMedia{Body: []byte("portrait video"), MimeType: "video/mp4"},
		gatewayVideoInput{Resolution: "720p", AspectRatio: "16:9"},
	)
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeUpstreamOutputMismatch || !standard.Retryable {
		t.Fatalf("layout mismatch error = %#v, %v", standard, err)
	}
	objectStorage.mu.Lock()
	defer objectStorage.mu.Unlock()
	if len(objectStorage.objects) != 0 {
		t.Fatalf("mismatched video must not be stored: %#v", objectStorage.objects)
	}
}
