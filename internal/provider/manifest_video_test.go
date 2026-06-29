package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManifestVideoCreateRendersModelInputAndReferences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video/create" {
			t.Fatalf("path = %s, want /video/create", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "video-model" || body["prompt"] != "sunrise train" || body["image_url"] != "https://cdn.example/still.png" || body["duration"] != float64(5) || body["aspect_ratio"] != "16:9" {
			t.Fatalf("request body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"taskId": "task-1", "status": "processing"})
	}))
	defer server.Close()

	manifest := ProviderManifest{
		BaseURL: server.URL,
		Auth:    ManifestAuth{Type: "none"},
		Endpoints: map[string]ManifestEndpoint{
			"video_generate": {
				EndpointType: "async_create",
				Method:       http.MethodPost,
				PathTemplate: "/video/create",
				RequestTemplate: mustJSON(map[string]any{
					"model":        "{{ model.id }}",
					"prompt":       "{{ input.prompt }}",
					"image_url":    "{{ references[0].url }}",
					"duration":     "{{ input.duration }}",
					"aspect_ratio": "{{ input.aspectRatio }}",
				}),
				ResponseMapping: json.RawMessage(`{"externalTaskId":"$.taskId","status":"$.status"}`),
			},
		},
	}
	result, err := callManifestEndpointWithContext(context.Background(), manifest, Account{}, nil, "video_generate", manifest.Endpoints["video_generate"], json.RawMessage(`{"prompt":"sunrise train","duration":5,"aspectRatio":"16:9"}`), manifestCallContext{
		References: []map[string]any{{"url": "https://cdn.example/still.png"}},
		Model:      map[string]any{"id": "video-model", "displayName": "Video Model", "modality": "video"},
		Account:    map[string]any{"baseUrl": server.URL, "authType": "none"},
	})
	if err != nil {
		t.Fatalf("callManifestEndpointWithContext() error = %v", err)
	}
	if videoStringField(result.NormalizedOutput, "externalTaskId") != "task-1" || normalizeGatewayVideoStatus(videoStringField(result.NormalizedOutput, "status")) != "running" {
		t.Fatalf("normalized output = %s", string(result.NormalizedOutput))
	}
}

func TestManifestVideoPollRendersTaskExternalTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video/poll/task-1" {
			t.Fatalf("path = %s, want /video/poll/task-1", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "videoUrl": "https://cdn.example/video.mp4"})
	}))
	defer server.Close()

	manifest := ProviderManifest{
		BaseURL: server.URL,
		Auth:    ManifestAuth{Type: "none"},
		Endpoints: map[string]ManifestEndpoint{
			"video_poll": {
				EndpointType:    "async_poll",
				Method:          http.MethodGet,
				PathTemplate:    "/video/poll/{{ task.externalTaskId }}",
				ResponseMapping: json.RawMessage(`{"status":"$.status","videoUrl":"$.videoUrl"}`),
			},
		},
	}
	result, err := callManifestEndpointWithContext(context.Background(), manifest, Account{}, nil, "video_poll", manifest.Endpoints["video_poll"], json.RawMessage(`{"prompt":"sunrise train"}`), manifestCallContext{
		Task: map[string]any{"externalTaskId": "task-1", "providerAsyncTaskId": "async-1"},
	})
	if err != nil {
		t.Fatalf("callManifestEndpointWithContext() error = %v", err)
	}
	if normalizeGatewayVideoStatus(videoStringField(result.NormalizedOutput, "status")) != "succeeded" || videoStringField(result.NormalizedOutput, "videoUrl") == "" {
		t.Fatalf("normalized output = %s", string(result.NormalizedOutput))
	}
}
