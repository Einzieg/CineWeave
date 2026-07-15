package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildProviderURLNormalizesOpenAICompatibleImageV1(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "base without v1",
			baseURL:  "https://newapi.example.com",
			endpoint: "/images/generations",
			want:     "https://newapi.example.com/v1/images/generations",
		},
		{
			name:     "base with v1",
			baseURL:  "https://newapi.example.com/v1",
			endpoint: "/images/generations",
			want:     "https://newapi.example.com/v1/images/generations",
		},
		{
			name:     "edits base with v1",
			baseURL:  "https://newapi.example.com/v1",
			endpoint: "/images/edits",
			want:     "https://newapi.example.com/v1/images/edits",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildProviderURL(&tt.baseURL, tt.endpoint, true)
			if err != nil {
				t.Fatalf("buildProviderURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildImageGenerationRequestMapsOptions(t *testing.T) {
	request, err := buildImageGenerationRequest("gpt-image-1", json.RawMessage(`{
		"prompt": "paint a train",
		"size": "1024x1792",
		"quality": "hd",
		"style": "vivid",
		"aspectRatio": "9:16",
		"responseFormat": "b64_json",
		"outputFormat": "png",
		"providerOptions": {"background": "transparent", "model": "ignored"}
	}`))
	if err != nil {
		t.Fatalf("buildImageGenerationRequest() error = %v", err)
	}
	if request["model"] != "gpt-image-1" || request["prompt"] != "paint a train" {
		t.Fatalf("model/prompt = %#v", request)
	}
	if request["size"] != "1024x1792" || request["quality"] != "hd" || request["style"] != "vivid" {
		t.Fatalf("image options = %#v", request)
	}
	if request["response_format"] != "b64_json" || request["output_format"] != "png" {
		t.Fatalf("format options = %#v", request)
	}
	if request["aspect_ratio"] != "9:16" {
		t.Fatalf("aspect_ratio = %#v", request)
	}
	if request["background"] != "transparent" {
		t.Fatalf("providerOptions were not merged: %#v", request)
	}
}

func TestParseImageGenerationResponseURL(t *testing.T) {
	result, err := parseImageGenerationResponse([]byte(`{"data":[{"url":"https://cdn.example/image.png","revised_prompt":"train"}]}`))
	if err != nil {
		t.Fatalf("parseImageGenerationResponse() error = %v", err)
	}
	if result.ResponseType != "url" || result.ImageURL != "https://cdn.example/image.png" {
		t.Fatalf("result = %+v, want url response", result)
	}
	var normalized map[string]any
	if err := json.Unmarshal(result.NormalizedOutput, &normalized); err != nil {
		t.Fatalf("normalized output invalid: %v", err)
	}
	if normalized["imageUrl"] != "https://cdn.example/image.png" || normalized["revisedPrompt"] != "train" {
		t.Fatalf("normalized = %#v", normalized)
	}
}

func TestParseImageGenerationResponseB64JSON(t *testing.T) {
	result, err := parseImageGenerationResponse([]byte(`{"data":[{"b64_json":"aW1hZ2U=","mime_type":"image/png"}]}`))
	if err != nil {
		t.Fatalf("parseImageGenerationResponse() error = %v", err)
	}
	if result.ResponseType != "b64_json" || result.B64JSON != "aW1hZ2U=" || result.MimeType != "image/png" {
		t.Fatalf("result = %+v, want b64_json response", result)
	}
}

func TestOpenAICompatibleImageGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %s, want /v1/images/generations", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["model"] != "gpt-image-1" || request["response_format"] != "url" || request["n"] != float64(1) {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"url":"https://cdn.example/generated.png"}]}`))
	}))
	defer server.Close()

	account := Account{BaseURL: &server.URL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-image-1"}
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.imageGeneration(context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil), json.RawMessage(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatalf("imageGeneration() error = %v", err)
	}
	if result.ImageURL != "https://cdn.example/generated.png" {
		t.Fatalf("image url = %q", result.ImageURL)
	}
}

func TestOpenAICompatibleImageGenerationUploadsReferencesToEditsEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s, want /v1/images/edits", r.URL.Path)
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Fatalf("parse multipart request: %v", err)
		}
		if r.FormValue("model") != "gpt-image-2" || r.FormValue("prompt") != "leader close-up" || r.FormValue("size") != "1536x1024" {
			t.Fatalf("multipart fields = %#v", r.MultipartForm.Value)
		}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 1 {
			t.Fatalf("reference files = %d, want 1", len(files))
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatalf("open reference file: %v", err)
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read reference file: %v", err)
		}
		if string(body) != "leader-reference" || files[0].Header.Get("Content-Type") != "image/png" {
			t.Fatalf("reference file = %q %q", body, files[0].Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"url":"https://cdn.example/edited.png"}]}`))
	}))
	defer server.Close()

	account := Account{BaseURL: &server.URL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-image-2"}
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.imageGeneration(
		context.Background(),
		account,
		model,
		"sk-test",
		parseOpenAICompatibleConfig(nil),
		json.RawMessage(`{"prompt":"leader close-up","size":"1536x1024"}`),
		openAICompatibleImageReference{
			Reference: GatewayImageReference{
				AssetID:    "leader",
				ArtifactID: "leader-artifact",
				Metadata:   json.RawMessage(`{"referenceKey":"asset_primary:leader"}`),
			},
			FileName: "leader.png",
			MimeType: "image/png",
			Body:     []byte("leader-reference"),
		},
	)
	if err != nil {
		t.Fatalf("imageGeneration() error = %v", err)
	}
	if result.ImageURL != "https://cdn.example/edited.png" {
		t.Fatalf("image url = %q", result.ImageURL)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(result.RequestSnapshot, &snapshot); err != nil {
		t.Fatalf("request snapshot: %v", err)
	}
	if snapshot["requestMode"] != "images.edit" || snapshot["referenceCountUsed"] != float64(1) {
		t.Fatalf("request snapshot = %#v", snapshot)
	}
	keys, _ := snapshot["referenceKeys"].([]any)
	if len(keys) != 1 || keys[0] != "asset_primary:leader" {
		t.Fatalf("reference keys = %#v", snapshot["referenceKeys"])
	}
}

func TestOpenRouterImageGenerationUsesDedicatedJSONEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images" {
			t.Fatalf("path = %s, want /api/v1/images", r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type = %q", contentType)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, exists := request["response_format"]; exists {
			t.Fatalf("OpenRouter request must omit response_format: %#v", request)
		}
		references, _ := request["input_references"].([]any)
		if len(references) != 1 {
			t.Fatalf("input_references = %#v", request["input_references"])
		}
		item, _ := references[0].(map[string]any)
		imageURL, _ := item["image_url"].(map[string]any)
		if url, _ := imageURL["url"].(string); !strings.HasPrefix(url, "data:image/png;base64,") {
			t.Fatalf("reference url = %q", url)
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aW1hZ2U=","media_type":"image/png"}]}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/api/v1"
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.imageGeneration(
		context.Background(),
		Account{BaseURL: &baseURL, AuthType: "bearer"},
		Model{ModelKey: "openai/gpt-image-1-mini"},
		"sk-test",
		parseOpenAICompatibleConfig(json.RawMessage(`{"imageProtocol":"openrouter","imagesGenerationsEndpoint":"/images"}`)),
		json.RawMessage(`{"prompt":"four-view character sheet","size":"1536x1024","quality":"low"}`),
		openAICompatibleImageReference{
			Reference: GatewayImageReference{ArtifactID: "artifact-1"},
			MimeType:  "image/png",
			Body:      []byte("reference"),
		},
	)
	if err != nil {
		t.Fatalf("imageGeneration() error = %v", err)
	}
	if result.ResponseType != "b64_json" || result.MimeType != "image/png" {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(string(result.RequestSnapshot), "data:image/png;base64,") {
		t.Fatalf("request snapshot leaked reference bytes: %s", result.RequestSnapshot)
	}
}

func TestBuildImageGenerationRequestGPTImage2UsesNativeResponseFormat(t *testing.T) {
	request, err := buildImageGenerationRequest("openai/gpt-image-2-2026-04-21", json.RawMessage(`{
		"prompt": "four-view character sheet",
		"size": "1536x1024",
		"quality": "medium"
	}`))
	if err != nil {
		t.Fatalf("buildImageGenerationRequest() error = %v", err)
	}
	if _, exists := request["response_format"]; exists {
		t.Fatalf("response_format = %#v, want omitted for gpt-image-2", request["response_format"])
	}
	if request["quality"] != "medium" || request["size"] != "1536x1024" {
		t.Fatalf("request = %#v", request)
	}
}

func TestEstimateImageCostUsesPricingPolicy(t *testing.T) {
	usage := estimateImageCost(gatewayImageInput{Size: "1024x1024", Quality: "hd", N: 1}, []Capability{{
		PricingPolicy: json.RawMessage(`{
			"currency": "USD",
			"imageCost": "0.0050",
			"imageCostBySize": {"1024x1024": "0.0100"},
			"imageCostByQuality": {"hd": "0.0200"}
		}`),
	}})
	if usage.Currency != "USD" || usage.EstimatedCost != "0.02000000" {
		t.Fatalf("usage = %+v, want 0.02000000 USD", usage)
	}
}
