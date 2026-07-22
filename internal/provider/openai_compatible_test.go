package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestUsesNativeOpenAICompatibleRuntime(t *testing.T) {
	if !usesNativeOpenAICompatibleRuntime(Account{ConnectorKey: "openai_compatible_custom", Config: json.RawMessage(`{}`)}) {
		t.Fatal("openai-compatible connector without an explicit runtime should use the native runtime")
	}
	if usesNativeOpenAICompatibleRuntime(Account{ConnectorKey: "openai_compatible_custom", Config: json.RawMessage(`{"runtime":"declarative_manifest"}`)}) {
		t.Fatal("explicit declarative runtime should take precedence over the connector default")
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

func TestUpstreamErrorPreservesNestedProviderMessage(t *testing.T) {
	err := upstreamError(http.StatusBadRequest, []byte(`{
		"error": {
			"message": "The generated image may violate our guardrails around violence."
		}
	}`))
	var upstream *UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Message != "The generated image may violate our guardrails around violence." {
		t.Fatalf("message = %q", upstream.Message)
	}
	standard := NormalizeUpstreamError(upstream)
	if standard.Code != CodeContentRejected || standard.Message != upstream.Message || standard.Retryable {
		t.Fatalf("standard = %#v, want non-retryable content rejection with provider message", standard)
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

func TestBuildChatCompletionRequestUsesCompletionTokenLimitForGPT5(t *testing.T) {
	request, err := buildChatCompletionRequest("openai/gpt-5.5", json.RawMessage(`{"prompt":"hello","maxOutputTokens":3400}`), true)
	if err != nil {
		t.Fatalf("buildChatCompletionRequest() error = %v", err)
	}
	if request["max_completion_tokens"] != float64(3400) {
		t.Fatalf("max_completion_tokens = %v, want 3400", request["max_completion_tokens"])
	}
	if _, exists := request["max_tokens"]; exists {
		t.Fatalf("max_tokens should not be sent for GPT-5 models")
	}
}

func TestBuildChatCompletionRequestMapsReasoningLevel(t *testing.T) {
	request, err := buildChatCompletionRequest("openai/o3", json.RawMessage(`{
		"prompt": "hello",
		"reasoningLevel": "high"
	}`), false)
	if err != nil {
		t.Fatalf("buildChatCompletionRequest() error = %v", err)
	}
	if request["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", request["reasoning_effort"])
	}
	if _, ok := request["reasoningLevel"]; ok {
		t.Fatalf("provider request must not contain the canonical input alias: %#v", request)
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

func TestOpenAICompatibleStreamChatCompletionSucceedsWithFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1,\"total_tokens\":5}}\n\n"))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-test"}
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.streamChatCompletion(context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil), json.RawMessage(`{"prompt":"hi"}`), nil)
	if err != nil {
		t.Fatalf("streamChatCompletion() error = %v", err)
	}
	if result.Text != "done" {
		t.Fatalf("text = %q, want done", result.Text)
	}
	if result.Usage.InputTokens != 4 || result.Usage.OutputTokens != 1 || result.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want 4/1/5", result.Usage)
	}
}

func TestOpenAIStreamTerminalModeUsesModelCapability(t *testing.T) {
	model := Model{Capabilities: []Capability{{
		ProviderOptionsSchema: json.RawMessage(`{"xCapabilities":{"streamTerminalMode":"done_marker"}}`),
	}}}
	mode := openAIStreamTerminalMode(model)
	if mode != "done_marker" {
		t.Fatalf("terminal mode = %q, want done_marker", mode)
	}
	if openAIStreamTerminalSatisfied(mode, false, true) {
		t.Fatal("finish_reason must not satisfy done_marker mode")
	}
	if !openAIStreamTerminalSatisfied(mode, true, false) {
		t.Fatal("[DONE] must satisfy done_marker mode")
	}
}

func TestOpenAICompatibleStreamChatCompletionRejectsCleanEOFWithoutTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-test"}
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.streamChatCompletion(context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil), json.RawMessage(`{"prompt":"hi"}`), nil)
	if err == nil {
		t.Fatal("streamChatCompletion() error = nil, want missing terminal error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("streamChatCompletion() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if result.Text != "partial" {
		t.Fatalf("partial text = %q, want partial", result.Text)
	}
	if len(result.ResponseSnapshot) == 0 {
		t.Fatal("response snapshot is empty, want received chunks preserved")
	}
}

func TestOpenAICompatibleStreamChatCompletionRejectsIncompleteStructuredOutputAfterDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"shots\\\":[{\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-test"}
	client := newOpenAICompatibleClient(2 * time.Second)
	result, err := client.streamChatCompletion(
		context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil),
		json.RawMessage(`{"prompt":"plan shots","responseFormat":"json"}`), nil,
	)
	if err == nil {
		t.Fatal("streamChatCompletion() error = nil, want incomplete structured output error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("streamChatCompletion() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if result.Text != `{"shots":[{` {
		t.Fatalf("partial text = %q, want truncated JSON preserved", result.Text)
	}
	if len(result.ResponseSnapshot) == 0 {
		t.Fatal("response snapshot is empty, want received chunks preserved")
	}
}

func TestOpenAICompatibleStreamChatCompletionReturnsUnexpectedEOF(t *testing.T) {
	baseURL := "https://provider.example/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-test"}
	client := openAICompatibleClient{httpClient: &http.Client{Transport: openAIStreamRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: &unexpectedEOFReadCloser{
				payload: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"),
			},
		}, nil
	})}}
	_, err := client.streamChatCompletion(context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil), json.RawMessage(`{"prompt":"hi"}`), nil)
	if err == nil {
		t.Fatal("streamChatCompletion() error = nil, want unexpected EOF")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("streamChatCompletion() error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestOpenAICompatibleStreamChatCompletionReturnsStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte("data: {\"error\":{\"code\":\"upstream_overloaded\",\"message\":\"try later\"}}\n\n"))
	}))
	defer server.Close()

	baseURL := server.URL + "/v1"
	account := Account{BaseURL: &baseURL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-test"}
	client := newOpenAICompatibleClient(2 * time.Second)
	_, err := client.streamChatCompletion(context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil), json.RawMessage(`{"prompt":"hi"}`), nil)
	if err == nil {
		t.Fatal("streamChatCompletion() error = nil, want upstream error")
	}
	var upstream *UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Code != "upstream_overloaded" {
		t.Fatalf("upstream code = %q, want upstream_overloaded", upstream.Code)
	}
}

type openAIStreamRoundTripper func(*http.Request) (*http.Response, error)

func (f openAIStreamRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type unexpectedEOFReadCloser struct {
	payload []byte
	sent    bool
}

func (r *unexpectedEOFReadCloser) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.ErrUnexpectedEOF
	}
	r.sent = true
	return copy(p, r.payload), io.ErrUnexpectedEOF
}

func (r *unexpectedEOFReadCloser) Close() error {
	return nil
}
