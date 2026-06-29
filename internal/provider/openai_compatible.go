package provider

import (
	"bufio"
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
	ModelsEndpoint            string `json:"modelsEndpoint"`
	ChatCompletionsEndpoint   string `json:"chatCompletionsEndpoint"`
	ImagesGenerationsEndpoint string `json:"imagesGenerationsEndpoint"`
	TimeoutMS                 int    `json:"timeoutMs"`
}

type chatCompletionResult struct {
	RequestSnapshot  json.RawMessage
	ResponseSnapshot json.RawMessage
	NormalizedOutput json.RawMessage
	Text             string
	Usage            GatewayUsage
	LatencyMS        int
}

type imageGenerationResult struct {
	RequestSnapshot  json.RawMessage
	ResponseSnapshot json.RawMessage
	NormalizedOutput json.RawMessage
	ImageURL         string
	B64JSON          string
	RevisedPrompt    string
	MimeType         string
	ResponseType     string
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
	if strings.TrimSpace(cfg.ImagesGenerationsEndpoint) == "" {
		cfg.ImagesGenerationsEndpoint = "/images/generations"
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
	requestBody, err := buildChatCompletionRequest(model.ModelKey, input, false)
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
	usage := parseChatCompletionUsage(body)
	normalizedOutput, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{
		RequestSnapshot:  requestBytes,
		ResponseSnapshot: body,
		NormalizedOutput: normalizedOutput,
		Text:             text,
		Usage:            usage,
		LatencyMS:        latencyMS,
	}, nil
}

func (c openAICompatibleClient) streamChatCompletion(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage, onDelta func(string) error) (chatCompletionResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ChatCompletionsEndpoint)
	if err != nil {
		return chatCompletionResult{}, err
	}
	requestBody, err := buildChatCompletionRequest(model.ModelKey, input, true)
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
	req.Header.Set("Accept", "text/event-stream")
	applyAuth(req, account.AuthType, apiKey)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return chatCompletionResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if readErr != nil {
			return chatCompletionResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes}, readErr
		}
		return chatCompletionResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes, ResponseSnapshot: body}, upstreamError(resp.StatusCode, body)
	}

	var text strings.Builder
	var usage GatewayUsage
	chunks := make([]json.RawMessage, 0)
	snapshotBytes := 0
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		payloadBytes := []byte(payload)
		if snapshotBytes+len(payloadBytes) <= 4<<20 {
			chunkCopy := append(json.RawMessage(nil), payloadBytes...)
			chunks = append(chunks, chunkCopy)
			snapshotBytes += len(payloadBytes)
		}
		delta, chunkUsage, err := parseChatCompletionStreamChunk(payloadBytes)
		if err != nil {
			return chatCompletionResult{LatencyMS: int(time.Since(started).Milliseconds()), RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks})}, err
		}
		if chunkUsage.TotalTokens > 0 || chunkUsage.InputTokens > 0 || chunkUsage.OutputTokens > 0 {
			usage = chunkUsage
		}
		if delta == "" {
			continue
		}
		text.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return chatCompletionResult{LatencyMS: int(time.Since(started).Milliseconds()), RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks})}, err
			}
		}
	}
	latencyMS = int(time.Since(started).Milliseconds())
	if err := scanner.Err(); err != nil {
		return chatCompletionResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks})}, err
	}
	outputText := text.String()
	normalizedOutput, err := json.Marshal(map[string]any{"text": outputText})
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{
		RequestSnapshot:  requestBytes,
		ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks}),
		NormalizedOutput: normalizedOutput,
		Text:             outputText,
		Usage:            usage,
		LatencyMS:        latencyMS,
	}, nil
}

func (c openAICompatibleClient) imageGeneration(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage) (imageGenerationResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ImagesGenerationsEndpoint)
	if err != nil {
		return imageGenerationResult{}, err
	}
	requestBody, err := buildImageGenerationRequest(model.ModelKey, input)
	if err != nil {
		return imageGenerationResult{}, err
	}
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return imageGenerationResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBytes))
	if err != nil {
		return imageGenerationResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyAuth(req, account.AuthType, apiKey)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return imageGenerationResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayImageBytes*2))
	if err != nil {
		return imageGenerationResult{}, err
	}
	if resp.StatusCode >= 400 {
		return imageGenerationResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes, ResponseSnapshot: body}, upstreamError(resp.StatusCode, body)
	}
	result, err := parseImageGenerationResponse(body)
	result.LatencyMS = latencyMS
	result.RequestSnapshot = requestBytes
	result.ResponseSnapshot = body
	return result, err
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
	base := strings.TrimRight(*baseURL, "/")
	path := strings.TrimLeft(endpoint, "/")
	if strings.HasPrefix(path, "v1/") && strings.HasSuffix(base, "/v1") {
		path = strings.TrimPrefix(path, "v1/")
	}
	if openAICompatiblePathNeedsV1(path) && !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + "/" + path, nil
}

func applyAuth(req *http.Request, authType, apiKey string) {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case "api_key":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "bearer", "":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func openAICompatiblePathNeedsV1(path string) bool {
	switch strings.TrimLeft(path, "/") {
	case "models", "chat/completions", "images/generations":
		return true
	default:
		return false
	}
}

func buildChatCompletionRequest(modelKey string, input json.RawMessage, stream bool) (map[string]any, error) {
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
		"stream":   stream,
	}
	for _, key := range []string{
		"temperature",
		"max_tokens",
		"max_completion_tokens",
		"top_p",
		"stop",
		"presence_penalty",
		"frequency_penalty",
		"response_format",
		"tools",
		"tool_choice",
		"user",
	} {
		if value, ok := decoded[key]; ok {
			requestBody[key] = value
		}
	}
	if value, ok := decoded["maxOutputTokens"]; ok {
		requestBody["max_tokens"] = value
	}
	if value, ok := decoded["responseFormat"]; ok {
		if responseFormat := normalizeResponseFormat(value); responseFormat != nil {
			requestBody["response_format"] = responseFormat
		}
	}
	return requestBody, nil
}

func buildImageGenerationRequest(modelKey string, input json.RawMessage) (map[string]any, error) {
	var decoded map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return nil, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
		}
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	prompt, _ := decoded["prompt"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("%w: input.prompt is required", ErrValidation)
	}
	n := imageRequestCount(decoded["n"])
	if n <= 0 {
		n = 1
	}
	if n > 1 {
		return nil, fmt.Errorf("%w: image.generate only supports n=1 in this version", ErrValidation)
	}
	requestBody := map[string]any{
		"model":           modelKey,
		"prompt":          prompt,
		"size":            imageStringOption(decoded, "size", "1024x1024"),
		"n":               n,
		"response_format": "url",
	}
	for _, key := range []string{"quality", "style", "background", "moderation"} {
		if value, ok := decoded[key]; ok {
			requestBody[key] = value
		}
	}
	for _, pair := range []struct {
		inputKey string
		outKey   string
	}{
		{"response_format", "response_format"},
		{"responseFormat", "response_format"},
		{"output_format", "output_format"},
		{"outputFormat", "output_format"},
		{"aspect_ratio", "aspect_ratio"},
		{"aspectRatio", "aspect_ratio"},
	} {
		if value, ok := decoded[pair.inputKey]; ok {
			requestBody[pair.outKey] = value
		}
	}
	if providerOptions, ok := decoded["providerOptions"].(map[string]any); ok {
		for key, value := range providerOptions {
			if key == "model" || key == "prompt" {
				continue
			}
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

func parseImageGenerationResponse(body []byte) (imageGenerationResult, error) {
	var response struct {
		Data []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
			MimeType      string `json:"mime_type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return imageGenerationResult{}, fmt.Errorf("%w: provider image response is invalid", ErrValidation)
	}
	if len(response.Data) == 0 {
		return imageGenerationResult{}, fmt.Errorf("%w: provider image response has no data", ErrValidation)
	}
	item := response.Data[0]
	result := imageGenerationResult{
		ImageURL:      strings.TrimSpace(item.URL),
		B64JSON:       strings.TrimSpace(item.B64JSON),
		RevisedPrompt: item.RevisedPrompt,
		MimeType:      strings.TrimSpace(item.MimeType),
	}
	switch {
	case result.ImageURL != "":
		result.ResponseType = "url"
	case result.B64JSON != "":
		result.ResponseType = "b64_json"
		if result.MimeType == "" {
			result.MimeType = "image/png"
		}
	default:
		return imageGenerationResult{}, fmt.Errorf("%w: provider image response did not include url or b64_json", ErrValidation)
	}
	result.NormalizedOutput = mustJSON(map[string]any{
		"imageUrl":      result.ImageURL,
		"b64Json":       result.B64JSON,
		"revisedPrompt": result.RevisedPrompt,
		"mimeType":      result.MimeType,
	})
	return result, nil
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

func parseChatCompletionUsage(body []byte) GatewayUsage {
	var response struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return GatewayUsage{}
	}
	usage := GatewayUsage{
		InputTokens:  firstPositiveInt(response.Usage.InputTokens, response.Usage.PromptTokens),
		OutputTokens: firstPositiveInt(response.Usage.OutputTokens, response.Usage.CompletionTokens),
		TotalTokens:  response.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func parseChatCompletionStreamChunk(body []byte) (string, GatewayUsage, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content any `json:"content"`
			} `json:"delta"`
			Text string `json:"text"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", GatewayUsage{}, fmt.Errorf("%w: provider stream chunk is invalid", ErrValidation)
	}
	usage := GatewayUsage{
		InputTokens:  firstPositiveInt(response.Usage.InputTokens, response.Usage.PromptTokens),
		OutputTokens: firstPositiveInt(response.Usage.OutputTokens, response.Usage.CompletionTokens),
		TotalTokens:  response.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if len(response.Choices) == 0 {
		return "", usage, nil
	}
	choice := response.Choices[0]
	if content, ok := choice.Delta.Content.(string); ok {
		return content, usage, nil
	}
	switch {
	case choice.Message.Content != "":
		return choice.Message.Content, usage, nil
	case choice.Text != "":
		return choice.Text, usage, nil
	default:
		return "", usage, nil
	}
}

func normalizeResponseFormat(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "json", "json_object":
			return map[string]any{"type": "json_object"}
		default:
			return nil
		}
	default:
		return nil
	}
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func imageStringOption(decoded map[string]any, key, fallback string) string {
	value, _ := decoded[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func imageRequestCount(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0
		}
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
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
