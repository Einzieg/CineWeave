package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildProviderURL(&tt.baseURL, tt.endpoint)
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
