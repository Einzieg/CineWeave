package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type openAICompatibleClient struct {
	httpClient *http.Client
}

type openAICompatibleConfig struct {
	ModelsEndpoint          string `json:"modelsEndpoint"`
	ChatCompletionsEndpoint string `json:"chatCompletionsEndpoint"`
	TimeoutMS               int    `json:"timeoutMs"`
}

type chatCompletionResult struct {
	RequestSnapshot  json.RawMessage
	ResponseSnapshot json.RawMessage
	NormalizedOutput json.RawMessage
	LatencyMS        int
}

func newOpenAICompatibleClient(timeout time.Duration) openAICompatibleClient {
	return openAICompatibleClient{
		httpClient: &http.Client{Timeout: timeout},
	}
}

func parseOpenAICompatibleConfig(raw json.RawMessage) openAICompatibleConfig {
	var cfg openAICompatibleConfig
	_ = json.Unmarshal(raw, &cfg)
	if strings.TrimSpace(cfg.ModelsEndpoint) == "" {
		cfg.ModelsEndpoint = "/models"
	}
	if strings.TrimSpace(cfg.ChatCompletionsEndpoint) == "" {
		cfg.ChatCompletionsEndpoint = "/chat/completions"
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = 30000
	}
	return cfg
}

func (c openAICompatibleClient) discoverModels(ctx context.Context, account Account, apiKey string, cfg openAICompatibleConfig) (ModelDiscoveryResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ModelsEndpoint)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	applyAuth(req, account.AuthType, apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	if resp.StatusCode >= 400 {
		return ModelDiscoveryResult{}, upstreamError(resp.StatusCode, body)
	}
	models, err := parseOpenAIModels(body)
	if err != nil {
		return ModelDiscoveryResult{}, err
	}
	return ModelDiscoveryResult{
		Models:      models,
		Unsupported: []any{},
	}, nil
}

func (c openAICompatibleClient) chatCompletion(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage) (chatCompletionResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ChatCompletionsEndpoint)
	if err != nil {
		return chatCompletionResult{}, err
	}
	requestBody, err := buildChatCompletionRequest(model.ModelKey, input)
	if err != nil {
		return chatCompletionResult{}, err
	}
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return chatCompletionResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBytes))
	if err != nil {
		return chatCompletionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, account.AuthType, apiKey)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return chatCompletionResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return chatCompletionResult{}, err
	}
	if resp.StatusCode >= 400 {
		return chatCompletionResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes, ResponseSnapshot: body}, upstreamError(resp.StatusCode, body)
	}
	text, err := parseChatCompletionText(body)
	if err != nil {
		return chatCompletionResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes, ResponseSnapshot: body}, err
	}
	normalizedOutput, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{
		RequestSnapshot:  requestBytes,
		ResponseSnapshot: body,
		NormalizedOutput: normalizedOutput,
		LatencyMS:        latencyMS,
	}, nil
}

func buildProviderURL(baseURL *string, endpoint string) (string, error) {
	if baseURL == nil || strings.TrimSpace(*baseURL) == "" {
		return "", fmt.Errorf("%w: provider account baseUrl is required", ErrValidation)
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("%w: provider endpoint is required", ErrValidation)
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint, nil
	}
	return strings.TrimRight(*baseURL, "/") + "/" + strings.TrimLeft(endpoint, "/"), nil
}

func applyAuth(req *http.Request, authType, apiKey string) {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case "api_key":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "bearer", "":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func buildChatCompletionRequest(modelKey string, input json.RawMessage) (map[string]any, error) {
	var decoded map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return nil, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
		}
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	messages, ok := decoded["messages"]
	if !ok {
		prompt := "ping"
		if value, ok := decoded["prompt"].(string); ok && strings.TrimSpace(value) != "" {
			prompt = value
		}
		messages = []map[string]string{{"role": "user", "content": prompt}}
	}
	requestBody := map[string]any{
		"model":    modelKey,
		"messages": messages,
		"stream":   false,
	}
	for _, key := range []string{"temperature", "max_tokens", "top_p"} {
		if value, ok := decoded[key]; ok {
			requestBody[key] = value
		}
	}
	return requestBody, nil
}

func parseOpenAIModels(body []byte) ([]DiscoveredModel, error) {
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		items := make([]DiscoveredModel, 0, len(envelope.Data))
		for _, model := range envelope.Data {
			if strings.TrimSpace(model.ID) == "" {
				continue
			}
			items = append(items, DiscoveredModel{
				ModelKey:    model.ID,
				DisplayName: model.ID,
				Modality:    "text",
				Status:      "active",
			})
		}
		return items, nil
	}

	var array []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &array); err != nil {
		return nil, fmt.Errorf("%w: provider models response is invalid", ErrValidation)
	}
	items := make([]DiscoveredModel, 0, len(array))
	for _, model := range array {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		items = append(items, DiscoveredModel{
			ModelKey:    model.ID,
			DisplayName: model.ID,
			Modality:    "text",
			Status:      "active",
		})
	}
	return items, nil
}

func parseChatCompletionText(body []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%w: provider chat response is invalid", ErrValidation)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("%w: provider chat response has no choices", ErrValidation)
	}
	choice := response.Choices[0]
	switch {
	case strings.TrimSpace(choice.Message.Content) != "":
		return choice.Message.Content, nil
	case strings.TrimSpace(choice.Delta.Content) != "":
		return choice.Delta.Content, nil
	case strings.TrimSpace(choice.Text) != "":
		return choice.Text, nil
	default:
		return "", fmt.Errorf("%w: provider chat response has no text", ErrValidation)
	}
}

func upstreamError(status int, body []byte) error {
	code := ""
	var decoded struct {
		Error any `json:"error"`
		Code  any `json:"code"`
	}
	if err := json.Unmarshal(body, &decoded); err == nil {
		switch errValue := decoded.Error.(type) {
		case map[string]any:
			if value, ok := errValue["code"].(string); ok {
				code = value
			}
			if code == "" {
				if value, ok := errValue["type"].(string); ok {
					code = value
				}
			}
		case string:
			code = errValue
		}
		if code == "" {
			if value, ok := decoded.Code.(string); ok {
				code = value
			}
		}
	}
	return &UpstreamError{
		Status: status,
		Code:   code,
		Body:   string(body),
	}
}

func apiKeyFromCredential(payload map[string]any) (string, error) {
	for _, key := range []string{"apiKey", "api_key", "token", "accessToken"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", fmt.Errorf("%w: credential apiKey is required", ErrValidation)
}
