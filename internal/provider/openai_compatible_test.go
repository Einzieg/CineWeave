package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseOpenAIModels(t *testing.T) {
	models, err := parseOpenAIModels([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4.1-mini"}]}`))
	if err != nil {
		t.Fatalf("parseOpenAIModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ModelKey != "gpt-4o-mini" || models[0].Modality != "text" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
}

func TestParseChatCompletionText(t *testing.T) {
	text, err := parseChatCompletionText([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	if err != nil {
		t.Fatalf("parseChatCompletionText() error = %v", err)
	}
	if text != "pong" {
		t.Fatalf("text = %q, want pong", text)
	}
}

func TestBuildChatCompletionRequestMapsTextOptions(t *testing.T) {
	request, err := buildChatCompletionRequest("gpt-test", json.RawMessage(`{
		"prompt": "hello",
		"maxOutputTokens": 42,
		"responseFormat": "json"
	}`), true)
	if err != nil {
		t.Fatalf("buildChatCompletionRequest() error = %v", err)
	}
	if request["model"] != "gpt-test" {
		t.Fatalf("model = %v, want gpt-test", request["model"])
	}
	if request["stream"] != true {
		t.Fatalf("stream = %v, want true", request["stream"])
	}
	if request["max_tokens"] != float64(42) {
		t.Fatalf("max_tokens = %v, want 42", request["max_tokens"])
	}
	responseFormat, ok := request["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", request["response_format"])
	}
}

func TestBuildProviderURLNormalizesOpenAICompatibleV1(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "base without v1",
			baseURL:  "https://newapi.example.com",
			endpoint: "/models",
			want:     "https://newapi.example.com/v1/models",
		},
		{
			name:     "base with v1",
			baseURL:  "https://newapi.example.com/v1",
			endpoint: "/models",
			want:     "https://newapi.example.com/v1/models",
		},
		{
			name:     "endpoint with v1",
			baseURL:  "https://newapi.example.com/v1",
			endpoint: "/v1/chat/completions",
			want:     "https://newapi.example.com/v1/chat/completions",
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

func TestBuildProviderURLCanDisableV1Prefix(t *testing.T) {
	baseURL := "https://api.deepseek.com"
	got, err := buildProviderURL(&baseURL, "/chat/completions", false)
	if err != nil {
		t.Fatalf("buildProviderURL() error = %v", err)
	}
	if got != "https://api.deepseek.com/chat/completions" {
		t.Fatalf("url = %q, want DeepSeek chat completions URL without v1", got)
	}
}

func TestBuildChatCompletionRequestMergesDeepSeekOptions(t *testing.T) {
	request, err := buildChatCompletionRequest("deepseek-chat", json.RawMessage(`{
		"prompt": "hello",
		"extraBody": {
			"model": "ignored",
			"stream": false,
			"temperature": 0.2
		},
		"providerOptions": {
			"deepseek": {
				"model": "ignored",
				"messages": [],
				"thinking": { "type": "enabled" },
				"reasoning_effort": "high"
			}
		}
	}`), true)
	if err != nil {
		t.Fatalf("buildChatCompletionRequest() error = %v", err)
	}
	if request["model"] != "deepseek-chat" {
		t.Fatalf("model = %v, want deepseek-chat", request["model"])
	}
	if request["stream"] != true {
		t.Fatalf("stream = %v, want true", request["stream"])
	}
	if request["temperature"] != float64(0.2) {
		t.Fatalf("temperature = %v, want 0.2", request["temperature"])
	}
	thinking, ok := request["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v, want enabled", request["thinking"])
	}
	if request["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v, want high", request["reasoning_effort"])
	}
}

func TestParseChatCompletionUsage(t *testing.T) {
	usage := parseChatCompletionUsage([]byte(`{"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`))
	if usage.InputTokens != 12 || usage.OutputTokens != 8 || usage.TotalTokens != 20 {
		t.Fatalf("usage = %+v, want 12/8/20", usage)
	}
}

func TestOpenAICompatibleStreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["stream"] != true {
			t.Fatalf("stream = %v, want true", request["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-test"}
	client := newOpenAICompatibleClient(2 * time.Second)
	var chunks []string
	result, err := client.streamChatCompletion(context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil), json.RawMessage(`{"prompt":"hi"}`), func(text string) error {
		chunks = append(chunks, text)
		return nil
	})
	if err != nil {
		t.Fatalf("streamChatCompletion() error = %v", err)
	}
	if result.Text != "hello" {
		t.Fatalf("text = %q, want hello", result.Text)
	}
	if len(chunks) != 2 || chunks[0] != "hel" || chunks[1] != "lo" {
		t.Fatalf("chunks = %#v, want hel/lo", chunks)
	}
	if result.Usage.InputTokens != 3 || result.Usage.OutputTokens != 2 || result.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want 3/2/5", result.Usage)
	}
}
