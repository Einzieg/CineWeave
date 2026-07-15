package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleVideoCreateAndPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/video/generations":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body["model"] != "grok-imagine-video-1.5-preview" || body["prompt"] != "slow camera push" || body["image"] != "https://cdn.example/frame.png" {
				t.Fatalf("create body = %#v", body)
			}
			if body["size"] != "1280x720" || body["duration"] != float64(5) || body["n"] != float64(1) {
				t.Fatalf("create layout = %#v", body)
			}
			if _, ok := body["width"]; ok {
				t.Fatalf("New API request must use size, got width in %#v", body)
			}
			if _, ok := body["height"]; ok {
				t.Fatalf("New API request must use size, got height in %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"task_id": "task-1", "status": "queued", "size": "1280x720"}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/video/generations/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   "completed",
				"output":   map[string]any{"video_url": "https://cdn.example/video.mp4"},
				"metadata": map[string]any{"duration": 5},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "grok-imagine-video-1.5-preview"}
	cfg := parseOpenAICompatibleConfig(json.RawMessage(`{"runtime":"openai_compatible"}`))
	client := newOpenAICompatibleClient(2 * time.Second)
	createResult, err := client.createVideoTask(context.Background(), account, model, "test-key", cfg, json.RawMessage(`{"prompt":"slow camera push","duration":5,"aspectRatio":"16:9","resolution":"720p"}`), []GatewayVideoReference{{URL: "https://cdn.example/frame.png"}})
	if err != nil {
		t.Fatalf("createVideoTask() error = %v", err)
	}
	if createResult.Status != "queued" || videoStringField(createResult.NormalizedOutput, "externalTaskId") != "task-1" {
		t.Fatalf("create result = %+v %s", createResult, createResult.NormalizedOutput)
	}

	pollResult, err := client.pollVideoTask(context.Background(), account, "test-key", cfg, "task-1")
	if err != nil {
		t.Fatalf("pollVideoTask() error = %v", err)
	}
	if pollResult.Status != "succeeded" || videoStringField(pollResult.NormalizedOutput, "videoUrl") != "https://cdn.example/video.mp4" || videoFloatField(pollResult.NormalizedOutput, "durationSeconds") != 5 {
		t.Fatalf("poll result = %+v %s", pollResult, pollResult.NormalizedOutput)
	}
}

func TestOpenRouterVideoCreateAndPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/videos":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body["model"] != "google/veo-3.1-lite" || body["duration"] != float64(4) || body["aspect_ratio"] != "16:9" || body["resolution"] != "720p" {
				t.Fatalf("create body = %#v", body)
			}
			if _, exists := body["n"]; exists {
				t.Fatalf("OpenRouter request must omit n: %#v", body)
			}
			frames, _ := body["frame_images"].([]any)
			if len(frames) != 1 {
				t.Fatalf("frame_images = %#v", body["frame_images"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "job-1", "polling_url": "/api/v1/videos/job-1", "status": "pending"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/videos/job-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "job-1", "status": "completed", "unsigned_urls": []string{"https://cdn.example/video.mp4"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL + "/api/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "google/veo-3.1-lite"}
	cfg := parseOpenAICompatibleConfig(json.RawMessage(`{"runtime":"openai_compatible","videoProtocol":"openrouter","videoCreateEndpoint":"/videos","videoPollEndpoint":"/videos/{taskId}"}`))
	client := newOpenAICompatibleClient(2 * time.Second)
	createResult, err := client.createVideoTask(context.Background(), account, model, "test-key", cfg, json.RawMessage(`{"prompt":"sunrise","duration":4,"aspectRatio":"16:9","resolution":"720p"}`), []GatewayVideoReference{{Type: "first_frame", URL: "https://cdn.example/frame.png"}})
	if err != nil {
		t.Fatalf("createVideoTask() error = %v", err)
	}
	if createResult.Status != "queued" || videoStringField(createResult.NormalizedOutput, "externalTaskId") != "job-1" {
		t.Fatalf("create result = %+v %s", createResult, createResult.NormalizedOutput)
	}
	pollResult, err := client.pollVideoTask(context.Background(), account, "test-key", cfg, "job-1")
	if err != nil {
		t.Fatalf("pollVideoTask() error = %v", err)
	}
	if pollResult.Status != "succeeded" || videoStringField(pollResult.NormalizedOutput, "videoUrl") != "https://cdn.example/video.mp4" {
		t.Fatalf("poll result = %+v %s", pollResult, pollResult.NormalizedOutput)
	}
}

func TestOpenAICompatibleVideoCreateRejectsAcknowledgedLayoutMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "task-portrait",
			"status": "queued",
			"size":   "720x1280",
		})
	}))
	defer server.Close()

	baseURL := server.URL
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.createVideoTask(
		context.Background(),
		Account{BaseURL: &baseURL, AuthType: "bearer"},
		Model{ModelKey: "grok-imagine-video-1.5-preview"},
		"test-key",
		parseOpenAICompatibleConfig(json.RawMessage(`{"runtime":"openai_compatible"}`)),
		json.RawMessage(`{"prompt":"slow camera push","aspectRatio":"16:9","resolution":"720p"}`),
		nil,
	)
	standard, ok := StandardErrorFromError(err)
	if !ok || standard.Code != CodeUpstreamOutputMismatch || !standard.Retryable {
		t.Fatalf("layout mismatch error = %#v, %v", standard, err)
	}
	if got := videoStringField(result.NormalizedOutput, "status"); got != "failed" {
		t.Fatalf("normalized status = %q, output = %s", got, result.NormalizedOutput)
	}
	if got := videoStringField(result.NormalizedOutput, "requestedSize"); got != "1280x720" {
		t.Fatalf("requestedSize = %q, output = %s", got, result.NormalizedOutput)
	}
}

func TestNormalizeOpenAICompatibleVideoCreateRequiresTaskID(t *testing.T) {
	if _, _, err := normalizeOpenAICompatibleVideoResponse([]byte(`{"status":"processing"}`), true, false); err == nil {
		t.Fatal("expected missing task id error")
	}
	if normalized, status, err := normalizeOpenAICompatibleVideoResponse([]byte(`{"status":"FAILURE","fail_reason":"invalid reference"}`), true, false); err != nil || status != "failed" || videoStringField(normalized, "errorMessage") != "invalid reference" {
		t.Fatalf("immediate failure normalization = status %q error %v output %s", status, err, normalized)
	}
	if normalized, status, err := normalizeOpenAICompatibleVideoResponse([]byte(`{"status":"processing"}`), false, false); err != nil || status != "running" || videoStringField(normalized, "status") != "running" {
		t.Fatalf("poll normalization = status %q error %v output %s", status, err, normalized)
	}
}

func TestNormalizeOpenAICompatibleVideoFailureReason(t *testing.T) {
	normalized, status, err := normalizeOpenAICompatibleVideoResponse([]byte(`{
		"status":"FAILURE",
		"fail_reason":"参考图下载失败：图片地址下载失败或超时",
		"result_url":"参考图下载失败：图片地址下载失败或超时",
		"data":{"data":{"error":{"code":"video_generation_failed","message":"参考图下载失败：图片地址下载失败或超时"}}}
	}`), false, false)
	if err != nil {
		t.Fatalf("normalize failure: %v", err)
	}
	if status != "failed" {
		t.Fatalf("status = %q", status)
	}
	if got := videoStringField(normalized, "errorCode"); got != "video_generation_failed" {
		t.Fatalf("errorCode = %q", got)
	}
	if got := videoStringField(normalized, "errorMessage"); got != "参考图下载失败：图片地址下载失败或超时" {
		t.Fatalf("errorMessage = %q", got)
	}
	if got := videoStringField(normalized, "videoUrl"); got != "" {
		t.Fatalf("videoUrl = %q", got)
	}

	code, message, upstreamCode, standard := normalizedVideoTerminalFailure(normalized)
	if code != CodeUpstreamInternalError || upstreamCode != "video_generation_failed" || message != "参考图下载失败：图片地址下载失败或超时" {
		t.Fatalf("failure = code %q message %q upstream %q", code, message, upstreamCode)
	}
	if standard == nil || standard.Message != message || !standard.Retryable {
		t.Fatalf("standard = %+v", standard)
	}
}

func TestGatewayVideoPromptLimitAndInvalidRequestClassification(t *testing.T) {
	model := Model{Capabilities: []Capability{{
		InputLimits:           json.RawMessage(`{"promptMaxLength":4096}`),
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"promptMaxLength":8192}}`),
	}}}
	if got := ModelPromptLengthConstraint(model.Capabilities); got.MaxLength != 4096 || got.Unit != PromptLengthUnitCharacters {
		t.Fatalf("prompt constraint = %+v", got)
	}
	if err := validateGatewayVideoPromptForModel(strings.Repeat("x", 4097), model); err == nil {
		t.Fatal("expected prompt limit validation error")
	}
	if !isVideoInvalidRequestFailure("video_generation_failed", "Prompt length exceeds the maximum allowed length of 4096") {
		t.Fatal("prompt length failure should be classified as invalid request")
	}
}

func TestNewAPIVideoDimensions(t *testing.T) {
	tests := []struct {
		resolution  string
		aspectRatio string
		width       int
		height      int
	}{
		{resolution: "720p", aspectRatio: "16:9", width: 1280, height: 720},
		{resolution: "720p", aspectRatio: "9:16", width: 720, height: 1280},
		{resolution: "1080p", aspectRatio: "1:1", width: 1080, height: 1080},
		{resolution: "1920x1080", aspectRatio: "9:16", width: 1920, height: 1080},
	}
	for _, test := range tests {
		width, height := newAPIVideoDimensions(test.resolution, test.aspectRatio)
		if width != test.width || height != test.height {
			t.Fatalf("dimensions(%q, %q) = %dx%d, want %dx%d", test.resolution, test.aspectRatio, width, height, test.width, test.height)
		}
	}
}
