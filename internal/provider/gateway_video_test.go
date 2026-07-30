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

func TestGatewayVideoMediaPendingOutputPreservesUpstreamResult(t *testing.T) {
	output := gatewayVideoMediaPendingOutput(
		json.RawMessage(`{"status":"succeeded","videoUrl":"https://provider.example/v1/videos/task_1/content","externalTaskId":"task_1"}`),
		CodeMediaDownloadFailed,
		"provider video download failed",
	)
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["status"] != "running" || decoded["mediaTransferStatus"] != "pending" ||
		decoded["mediaTransferErrorCode"] != CodeMediaDownloadFailed ||
		decoded["videoUrl"] != "https://provider.example/v1/videos/task_1/content" ||
		decoded["externalTaskId"] != "task_1" {
		t.Fatalf("pending output = %#v", decoded)
	}
}

func TestNormalizeGatewayVideoMediaFailureRetriesOnlyTransientTransfer(t *testing.T) {
	retryable := normalizeGatewayVideoMediaFailure(
		json.RawMessage(`{"status":"succeeded","videoUrl":"https://provider.example/video.mp4"}`),
		gatewayMediaStageFailure("download", context.DeadlineExceeded),
	)
	if retryable.TaskStatus != "running" || retryable.CallStatus != "failed" || !retryable.TransferPending ||
		retryable.ErrorCode != CodeMediaDownloadFailed || retryable.ResponseError == nil || !retryable.ResponseError.Retryable {
		t.Fatalf("retryable media outcome = %+v", retryable)
	}
	if videoStringField(retryable.NormalizedOutput, "mediaTransferStatus") != "pending" {
		t.Fatalf("retryable normalized output = %s", retryable.NormalizedOutput)
	}

	terminal := normalizeGatewayVideoMediaFailure(
		json.RawMessage(`{"status":"succeeded","videoUrl":"https://provider.example/video.mp4"}`),
		&StandardErrorError{Standard: StandardError{
			Code:      CodeUpstreamOutputMismatch,
			Message:   "provider returned video layout 960x960, expected aspect ratio 9:16",
			Retryable: true,
		}},
	)
	if terminal.TaskStatus != "failed" || terminal.CallStatus != "failed" || terminal.TransferPending ||
		terminal.ErrorCode != CodeUpstreamOutputMismatch || terminal.ResponseError == nil || terminal.ResponseError.Retryable {
		t.Fatalf("terminal media outcome = %+v", terminal)
	}
	if videoStringField(terminal.NormalizedOutput, "status") != "failed" ||
		videoStringField(terminal.NormalizedOutput, "mediaTransferStatus") != "failed" ||
		videoStringField(terminal.NormalizedOutput, "videoUrl") != "https://provider.example/video.mp4" {
		t.Fatalf("terminal normalized output = %s", terminal.NormalizedOutput)
	}
}

func TestValidateGatewayVideoCreateTaskIdentityRejectsCrossExecutionReuse(t *testing.T) {
	task := gatewayVideoTask{
		NodeRunID: "node-old", NodeExecutionToken: "token-old", NodeAttemptGeneration: 1,
	}
	err := validateGatewayVideoCreateTaskIdentity(task, GatewayVideoCreateTaskRequest{
		NodeRunID: "node-new", NodeExecutionToken: "token-new", NodeAttemptGeneration: 1,
	})
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeRenderPlanReplanRequired || standard.Retryable {
		t.Fatalf("cross-execution task error = %#v, %v", standard, err)
	}
}

func TestValidateGatewayVideoCreateTaskIdentityAllowsSameExecutionReplay(t *testing.T) {
	task := gatewayVideoTask{
		NodeRunID: "node-1", NodeExecutionToken: "token-1", NodeAttemptGeneration: 2,
	}
	err := validateGatewayVideoCreateTaskIdentity(task, GatewayVideoCreateTaskRequest{
		NodeRunID: "node-1", NodeExecutionToken: "token-1", NodeAttemptGeneration: 2,
	})
	if err != nil {
		t.Fatalf("same-execution replay error = %v", err)
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

func TestStoreGatewayVideoMediaStoresWrongAspectWithWarning(t *testing.T) {
	service := NewService(nil, nil)
	objectStorage := newMemoryObjectStorage()
	service.SetStorage(objectStorage)
	service.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{Width: 720, Height: 1280}, nil
	})

	stored, err := service.storeGatewayVideoMedia(
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
	if err != nil {
		t.Fatalf("store mismatched video: %v", err)
	}
	if stored == nil || len(stored.Output.Warnings) != 1 {
		t.Fatalf("stored output warnings = %+v", stored)
	}
	warning := stored.Output.Warnings[0]
	if warning.Code != GatewayVideoWarningCodeLayoutMismatch ||
		warning.Category != "provider_capability" ||
		warning.ExpectedAspectRatio != "16:9" ||
		warning.ProviderSize != "720x1280" {
		t.Fatalf("layout warning = %+v", warning)
	}
	if stored.Output.MediaProbe == nil || len(stored.Output.MediaProbe.Warnings) != 1 {
		t.Fatalf("media probe warnings = %+v", stored.Output.MediaProbe)
	}
	objectStorage.mu.Lock()
	defer objectStorage.mu.Unlock()
	if len(objectStorage.objects) != 1 {
		t.Fatalf("mismatched video must be stored: %#v", objectStorage.objects)
	}
}

func TestStoreGatewayVideoMediaUsesProbedLayoutAsAuthority(t *testing.T) {
	service := NewService(nil, nil)
	service.SetStorage(newMemoryObjectStorage())
	service.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{Width: 1280, Height: 720}, nil
	})
	preliminary := videoLayoutWarningOutput(
		json.RawMessage(`{"status":"succeeded","videoUrl":"https://example.test/video.mp4"}`),
		detectVideoOutputLayoutWarning("16:9", 720, 1280),
		"1280x720",
		"720x1280",
	)

	stored, err := service.storeGatewayVideoMedia(
		context.Background(),
		"call-landscape",
		"org-1",
		"project-1",
		gatewayModelSelection{Model: Model{ID: "model-1"}},
		"task-landscape",
		manifestRunResult{NormalizedOutput: preliminary},
		gatewayVideoMedia{Body: []byte("landscape video"), MimeType: "video/mp4"},
		gatewayVideoInput{Resolution: "720p", AspectRatio: "16:9"},
	)
	if err != nil {
		t.Fatalf("store matching video: %v", err)
	}
	if len(stored.Output.Warnings) != 0 || len(stored.Output.MediaProbe.Warnings) != 0 {
		t.Fatalf("actual matching layout must clear preliminary warning: %+v", stored.Output)
	}
}

func TestStoreGatewayVideoMediaKeepsProviderLayoutWarningWithoutProbe(t *testing.T) {
	service := NewService(nil, nil)
	service.SetStorage(newMemoryObjectStorage())
	preliminary := videoLayoutWarningOutput(
		json.RawMessage(`{"status":"succeeded","videoUrl":"https://example.test/video.mp4"}`),
		detectVideoOutputLayoutWarning("9:16", 1280, 720),
		"720x1280",
		"1280x720",
	)

	stored, err := service.storeGatewayVideoMedia(
		context.Background(),
		"call-no-probe",
		"org-1",
		"project-1",
		gatewayModelSelection{Model: Model{ID: "model-1"}},
		"task-no-probe",
		manifestRunResult{NormalizedOutput: preliminary},
		gatewayVideoMedia{Body: []byte("provider-reported landscape video"), MimeType: "video/mp4"},
		gatewayVideoInput{Resolution: "720p", AspectRatio: "9:16"},
	)
	if err != nil {
		t.Fatalf("store provider-reported mismatch: %v", err)
	}
	if len(stored.Output.Warnings) != 1 ||
		stored.Output.Warnings[0].Code != GatewayVideoWarningCodeLayoutMismatch {
		t.Fatalf("provider warning without probe = %+v", stored.Output.Warnings)
	}
}
