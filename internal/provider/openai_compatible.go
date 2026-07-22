package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/provider/outbound"
)

type openAICompatibleClient struct {
	httpClient   *http.Client
	mediaFetcher *outbound.MediaFetcher
}

type openAICompatibleConfig struct {
	ModelsEndpoint              string `json:"modelsEndpoint"`
	ChatCompletionsEndpoint     string `json:"chatCompletionsEndpoint"`
	ImageProtocol               string `json:"imageProtocol"`
	ImagesGenerationsEndpoint   string `json:"imagesGenerationsEndpoint"`
	ImagesEditsEndpoint         string `json:"imagesEditsEndpoint"`
	AudioSpeechEndpoint         string `json:"audioSpeechEndpoint"`
	AudioTranscriptionsEndpoint string `json:"audioTranscriptionsEndpoint"`
	VideoProtocol               string `json:"videoProtocol"`
	VideoCreateEndpoint         string `json:"videoCreateEndpoint"`
	VideoPollEndpoint           string `json:"videoPollEndpoint"`
	VideoCancelEndpoint         string `json:"videoCancelEndpoint"`
	VideoExtensionField         string `json:"videoExtensionField"`
	VideoExtensionModeField     string `json:"videoExtensionModeField"`
	VideoExtensionModeValue     string `json:"videoExtensionModeValue"`
	TimeoutMS                   int    `json:"timeoutMs"`
	VideoPollTimeoutMS          int    `json:"videoPollTimeoutMs"`
	DisableV1Prefix             bool   `json:"disableV1Prefix"`
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
		httpClient:   &http.Client{Timeout: timeout},
		mediaFetcher: outbound.NewMediaFetcher(outbound.Config{}),
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
	if strings.TrimSpace(cfg.ImagesEditsEndpoint) == "" {
		cfg.ImagesEditsEndpoint = "/images/edits"
	}
	if strings.TrimSpace(cfg.AudioSpeechEndpoint) == "" {
		cfg.AudioSpeechEndpoint = "/audio/speech"
	}
	if strings.TrimSpace(cfg.AudioTranscriptionsEndpoint) == "" {
		cfg.AudioTranscriptionsEndpoint = "/audio/transcriptions"
	}
	if strings.TrimSpace(cfg.VideoProtocol) == "" {
		cfg.VideoProtocol = accountConfigString(raw, "videoRequestProtocol")
	}
	if strings.TrimSpace(cfg.VideoProtocol) == "" {
		cfg.VideoProtocol = "new_api"
	}
	if strings.TrimSpace(cfg.VideoCreateEndpoint) == "" {
		cfg.VideoCreateEndpoint = accountConfigString(raw, "videoGenerationsEndpoint")
	}
	if strings.TrimSpace(cfg.VideoCreateEndpoint) == "" {
		cfg.VideoCreateEndpoint = "/video/generations"
	}
	if strings.TrimSpace(cfg.VideoPollEndpoint) == "" {
		cfg.VideoPollEndpoint = strings.TrimRight(cfg.VideoCreateEndpoint, "/") + "/{taskId}"
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultOpenAICompatibleTimeoutMS
	}
	if cfg.VideoPollTimeoutMS <= 0 {
		cfg.VideoPollTimeoutMS = defaultOpenAIVideoPollTimeoutMS
	}
	return cfg
}

func openAICompatibleConfigHasTimeout(raw json.RawMessage) bool {
	var cfg struct {
		TimeoutMS *int `json:"timeoutMs"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return false
	}
	return cfg.TimeoutMS != nil && *cfg.TimeoutMS > 0
}

func usesNativeOpenAICompatibleRuntime(account Account) bool {
	runtime := strings.ToLower(strings.TrimSpace(accountConfigString(account.Config, "runtime")))
	if runtime != "" {
		return runtime == "openai_compatible"
	}
	return strings.EqualFold(strings.TrimSpace(account.ConnectorKey), "openai_compatible_custom")
}

func (c openAICompatibleClient) discoverModels(ctx context.Context, account Account, apiKey string, cfg openAICompatibleConfig) (ModelDiscoveryResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ModelsEndpoint, !cfg.DisableV1Prefix)
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
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ChatCompletionsEndpoint, !cfg.DisableV1Prefix)
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
	endpoint, err := buildProviderURL(account.BaseURL, cfg.ChatCompletionsEndpoint, !cfg.DisableV1Prefix)
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
	terminalMode := openAIStreamTerminalMode(model)
	sawDoneMarker := false
	sawFinishReason := false
	decoder := newSSEDecoder(resp.Body, defaultSSEMaxEventBytes)
	for {
		event, ok, err := decoder.Next()
		if err != nil {
			result := "read_error"
			if errors.Is(err, io.ErrUnexpectedEOF) {
				result = "truncated"
			}
			observability.RecordProviderStreamTerminal(terminalMode, result)
			return chatCompletionResult{LatencyMS: int(time.Since(started).Milliseconds()), RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks}), Text: text.String(), Usage: usage}, err
		}
		if !ok {
			break
		}
		payload := strings.TrimSpace(event.Data)
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			sawDoneMarker = true
			break
		}
		payloadBytes := []byte(payload)
		if strings.EqualFold(event.Event, "error") || openAIStreamPayloadHasError(payloadBytes) {
			observability.RecordProviderStreamTerminal(terminalMode, "upstream_error")
			return chatCompletionResult{LatencyMS: int(time.Since(started).Milliseconds()), RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks, "error": rawJSONValue(json.RawMessage(payloadBytes))}), Text: text.String(), Usage: usage}, upstreamError(http.StatusBadGateway, payloadBytes)
		}
		if snapshotBytes+len(payloadBytes) <= 4<<20 {
			chunkCopy := append(json.RawMessage(nil), payloadBytes...)
			chunks = append(chunks, chunkCopy)
			snapshotBytes += len(payloadBytes)
		}
		delta, chunkUsage, chunkTerminal, err := parseChatCompletionStreamChunk(payloadBytes)
		if err != nil {
			observability.RecordProviderStreamTerminal(terminalMode, "invalid_event")
			return chatCompletionResult{LatencyMS: int(time.Since(started).Milliseconds()), RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks}), Text: text.String(), Usage: usage}, err
		}
		if chunkUsage.TotalTokens > 0 || chunkUsage.InputTokens > 0 || chunkUsage.OutputTokens > 0 {
			usage = chunkUsage
		}
		if chunkTerminal {
			sawFinishReason = true
		}
		if delta == "" {
			continue
		}
		text.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				observability.RecordProviderStreamTerminal(terminalMode, "consumer_error")
				return chatCompletionResult{LatencyMS: int(time.Since(started).Milliseconds()), RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks}), Text: text.String(), Usage: usage}, err
			}
		}
	}
	latencyMS = int(time.Since(started).Milliseconds())
	if !openAIStreamTerminalSatisfied(terminalMode, sawDoneMarker, sawFinishReason) {
		observability.RecordProviderStreamTerminal(terminalMode, "truncated")
		return chatCompletionResult{LatencyMS: latencyMS, RequestSnapshot: requestBytes, ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks, "terminalMode": terminalMode, "sawDoneMarker": sawDoneMarker, "sawFinishReason": sawFinishReason}), Text: text.String(), Usage: usage}, fmt.Errorf("%w: provider stream ended without the required %s terminal", io.ErrUnexpectedEOF, terminalMode)
	}
	outputText := text.String()
	if responseFormatRequiresJSON(requestBody["response_format"]) {
		if err := validateStructuredStreamOutput(outputText); err != nil {
			result := "invalid_event"
			if errors.Is(err, io.ErrUnexpectedEOF) {
				result = "truncated"
			}
			observability.RecordProviderStreamTerminal(terminalMode, result)
			return chatCompletionResult{
				LatencyMS:        latencyMS,
				RequestSnapshot:  requestBytes,
				ResponseSnapshot: mustJSON(map[string]any{"chunks": chunks, "structuredOutputError": err.Error()}),
				Text:             outputText,
				Usage:            usage,
			}, err
		}
	}
	observability.RecordProviderStreamTerminal(terminalMode, "succeeded")
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

func responseFormatRequiresJSON(value any) bool {
	format, ok := value.(map[string]any)
	if !ok {
		return false
	}
	typeValue, _ := format["type"].(string)
	switch strings.ToLower(strings.TrimSpace(typeValue)) {
	case "json", "json_object", "json_schema":
		return true
	default:
		return false
	}
}

func validateStructuredStreamOutput(value string) error {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		if newline := strings.IndexByte(value, '\n'); newline >= 0 {
			value = strings.TrimSpace(value[newline+1:])
		}
		value = strings.TrimSpace(strings.TrimSuffix(value, "```"))
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unexpected end of json input") {
			return fmt.Errorf("%w: provider JSON stream ended with incomplete output", io.ErrUnexpectedEOF)
		}
		return fmt.Errorf("%w: provider JSON stream output is invalid: %v", ErrValidation, err)
	}
	return nil
}

func openAIStreamTerminalMode(model Model) string {
	for _, capability := range model.Capabilities {
		var schema map[string]any
		if err := json.Unmarshal(capability.ProviderOptionsSchema, &schema); err != nil {
			continue
		}
		values := schema
		if nested, ok := schema["xCapabilities"].(map[string]any); ok {
			values = nested
		}
		mode, _ := values["streamTerminalMode"].(string)
		mode = strings.ToLower(strings.TrimSpace(mode))
		switch mode {
		case "done_marker", "finish_reason", "done_or_finish_reason":
			return mode
		}
	}
	return "done_or_finish_reason"
}

func openAIStreamTerminalSatisfied(mode string, sawDoneMarker, sawFinishReason bool) bool {
	switch mode {
	case "done_marker":
		return sawDoneMarker
	case "finish_reason":
		return sawFinishReason
	default:
		return sawDoneMarker || sawFinishReason
	}
}

type openAICompatibleImageReference struct {
	Reference GatewayImageReference
	FileName  string
	MimeType  string
	Body      []byte
}

func (c openAICompatibleClient) imageGeneration(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage, references ...openAICompatibleImageReference) (imageGenerationResult, error) {
	requestBody, err := buildImageGenerationRequest(model.ModelKey, input)
	if err != nil {
		return imageGenerationResult{}, err
	}

	endpointPath := cfg.ImagesGenerationsEndpoint
	contentType := "application/json"
	requestSnapshotBody := requestBody
	if strings.EqualFold(strings.TrimSpace(cfg.ImageProtocol), "openrouter") {
		requestBody, requestSnapshotBody, err = buildOpenRouterImageRequest(requestBody, references)
	} else if len(references) > 0 {
		endpointPath = cfg.ImagesEditsEndpoint
	}
	if err != nil {
		return imageGenerationResult{}, err
	}
	requestBytes, err := json.Marshal(requestBody)
	requestSnapshot := mustJSON(requestSnapshotBody)
	if !strings.EqualFold(strings.TrimSpace(cfg.ImageProtocol), "openrouter") && len(references) > 0 {
		requestBytes, contentType, err = buildOpenAICompatibleImageEditBody(requestBody, references)
		requestSnapshot = buildOpenAICompatibleImageEditSnapshot(requestBody, references)
	}
	if err != nil {
		return imageGenerationResult{}, err
	}
	endpoint, err := buildProviderURL(account.BaseURL, endpointPath, !cfg.DisableV1Prefix)
	if err != nil {
		return imageGenerationResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBytes))
	if err != nil {
		return imageGenerationResult{}, err
	}
	req.Header.Set("Content-Type", contentType)
	applyAuth(req, account.AuthType, apiKey)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return imageGenerationResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayImageBytes*2))
	if err != nil {
		return imageGenerationResult{}, err
	}
	if resp.StatusCode >= 400 {
		return imageGenerationResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot, ResponseSnapshot: body}, upstreamError(resp.StatusCode, body)
	}
	result, err := parseImageGenerationResponse(body)
	result.LatencyMS = latencyMS
	result.RequestSnapshot = requestSnapshot
	result.ResponseSnapshot = body
	return result, err
}

func buildOpenRouterImageRequest(requestBody map[string]any, references []openAICompatibleImageReference) (map[string]any, map[string]any, error) {
	body := make(map[string]any, len(requestBody)+1)
	for key, value := range requestBody {
		if key == "response_format" {
			continue
		}
		body[key] = value
	}
	if len(references) == 0 {
		return body, body, nil
	}

	inputReferences := make([]map[string]any, 0, len(references))
	snapshotReferences := make([]map[string]any, 0, len(references))
	for index, reference := range references {
		if len(reference.Body) == 0 {
			return nil, nil, fmt.Errorf("%w: OpenRouter image reference %d is empty", ErrValidation, index+1)
		}
		mimeType := normalizeMediaType(reference.MimeType)
		if mimeType == "" {
			mimeType = http.DetectContentType(reference.Body)
		}
		dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(reference.Body)
		inputReferences = append(inputReferences, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": dataURL},
		})
		snapshotReferences = append(snapshotReferences, map[string]any{
			"index":      index,
			"mimeType":   mimeType,
			"byteSize":   len(reference.Body),
			"artifactId": reference.Reference.ArtifactID,
		})
	}
	body["input_references"] = inputReferences
	snapshot := make(map[string]any, len(body)+2)
	for key, value := range body {
		snapshot[key] = value
	}
	snapshot["input_references"] = snapshotReferences
	snapshot["referenceCountUsed"] = len(references)
	return body, snapshot, nil
}

func buildOpenAICompatibleImageEditBody(requestBody map[string]any, references []openAICompatibleImageReference) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	keys := make([]string, 0, len(requestBody))
	for key := range requestBody {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, err := imageEditFormValue(requestBody[key])
		if err != nil {
			return nil, "", fmt.Errorf("%w: image option %s is invalid", ErrValidation, key)
		}
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	for index, reference := range references {
		fileName := strings.TrimSpace(path.Base(reference.FileName))
		if fileName == "" || fileName == "." || fileName == "/" {
			fileName = fmt.Sprintf("reference-%02d.png", index+1)
		}
		contentType := normalizeMediaType(reference.MimeType)
		if contentType == "" {
			contentType = http.DetectContentType(reference.Body)
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name":     "image[]",
			"filename": fileName,
		}))
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(reference.Body); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func imageEditFormValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case json.Number:
		return typed.String(), nil
	case nil:
		return "", nil
	default:
		raw, err := json.Marshal(value)
		return string(raw), err
	}
}

func buildOpenAICompatibleImageEditSnapshot(requestBody map[string]any, references []openAICompatibleImageReference) json.RawMessage {
	snapshot := make(map[string]any, len(requestBody)+4)
	for key, value := range requestBody {
		snapshot[key] = value
	}
	selected := make([]GatewayImageReference, 0, len(references))
	for _, reference := range references {
		selected = append(selected, reference.Reference)
	}
	snapshot["requestMode"] = "images.edit"
	snapshot["referenceCountUsed"] = len(selected)
	snapshot["referenceKeys"] = gatewayImageReferenceKeys(selected)
	snapshot["references"] = gatewayImageReferenceSnapshots(selected)
	return mustJSON(snapshot)
}

func buildProviderURL(baseURL *string, endpoint string, autoV1Prefix bool) (string, error) {
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
	if autoV1Prefix && openAICompatiblePathNeedsV1(path) && !strings.HasSuffix(base, "/v1") {
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
	path = strings.TrimLeft(path, "/")
	switch path {
	case "models", "chat/completions", "images/generations", "images/edits", "audio/speech", "audio/transcriptions", "video/generations", "videos":
		return true
	default:
		return strings.HasPrefix(path, "video/generations/") || strings.HasPrefix(path, "videos/")
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
		"reasoning_effort",
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
		if usesMaxCompletionTokens(modelKey) {
			requestBody["max_completion_tokens"] = value
		} else {
			requestBody["max_tokens"] = value
		}
	}
	for _, key := range []string{"reasoningLevel", "reasoningEffort"} {
		if value, ok := decoded[key]; ok {
			requestBody["reasoning_effort"] = value
		}
	}
	if value, ok := decoded["responseFormat"]; ok {
		if responseFormat := normalizeResponseFormat(value); responseFormat != nil {
			requestBody["response_format"] = responseFormat
		}
	}
	if extraBody, ok := decoded["extraBody"].(map[string]any); ok {
		for key, value := range extraBody {
			if key == "model" || key == "messages" || key == "stream" {
				continue
			}
			requestBody[key] = value
		}
	}
	if providerOptions, ok := decoded["providerOptions"].(map[string]any); ok {
		if deepseek, ok := providerOptions["deepseek"].(map[string]any); ok {
			for key, value := range deepseek {
				if key == "model" || key == "messages" || key == "stream" {
					continue
				}
				requestBody[key] = value
			}
		}
	}
	return requestBody, nil
}

func usesMaxCompletionTokens(modelKey string) bool {
	model := strings.ToLower(strings.TrimSpace(modelKey))
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	return strings.HasPrefix(model, "gpt-5") ||
		strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		strings.HasPrefix(model, "o4")
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
		"model":  modelKey,
		"prompt": prompt,
		"size":   imageStringOption(decoded, "size", "1024x1024"),
		"n":      n,
	}
	if !isGPTImage2Model(modelKey) {
		requestBody["response_format"] = "url"
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

func isGPTImage2Model(modelKey string) bool {
	model := strings.ToLower(strings.TrimSpace(modelKey))
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	return model == "gpt-image-2" || strings.HasPrefix(model, "gpt-image-2-")
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

func parseChatCompletionStreamChunk(body []byte) (string, GatewayUsage, bool, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content any `json:"content"`
			} `json:"delta"`
			Text         string  `json:"text"`
			FinishReason *string `json:"finish_reason"`
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
		return "", GatewayUsage{}, false, fmt.Errorf("%w: provider stream chunk is invalid", ErrValidation)
	}
	usage := GatewayUsage{
		InputTokens:  firstPositiveInt(response.Usage.InputTokens, response.Usage.PromptTokens),
		OutputTokens: firstPositiveInt(response.Usage.OutputTokens, response.Usage.CompletionTokens),
		TotalTokens:  response.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	terminal := false
	for _, choice := range response.Choices {
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			terminal = true
			break
		}
	}
	if len(response.Choices) == 0 {
		return "", usage, terminal, nil
	}
	choice := response.Choices[0]
	if content, ok := choice.Delta.Content.(string); ok {
		return content, usage, terminal, nil
	}
	switch {
	case choice.Message.Content != "":
		return choice.Message.Content, usage, terminal, nil
	case choice.Text != "":
		return choice.Text, usage, terminal, nil
	default:
		return "", usage, terminal, nil
	}
}

func openAIStreamPayloadHasError(body []byte) bool {
	var decoded struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return false
	}
	return len(decoded.Error) > 0 && strings.TrimSpace(string(decoded.Error)) != "" && strings.TrimSpace(string(decoded.Error)) != "null"
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
	code, message := parseUpstreamErrorBody(body)
	return &UpstreamError{
		Status:  status,
		Code:    code,
		Message: message,
		Body:    string(body),
	}
}

func parseUpstreamErrorBody(body []byte) (code, message string) {
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", ""
	}

	code = firstUpstreamString(decoded, "code", "type")
	message = firstUpstreamString(decoded, "message", "detail")
	switch errorValue := decoded["error"].(type) {
	case map[string]any:
		if code == "" {
			code = firstUpstreamString(errorValue, "code", "type")
		}
		if message == "" {
			message = firstUpstreamString(errorValue, "message", "detail")
		}
	case string:
		if message == "" {
			message = strings.TrimSpace(errorValue)
		}
	}
	return strings.TrimSpace(code), normalizeUpstreamMessage(message)
}

func firstUpstreamString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func apiKeyFromCredential(payload map[string]any) (string, error) {
	for _, key := range []string{"apiKey", "api_key", "token", "accessToken"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", fmt.Errorf("%w: credential apiKey is required", ErrValidation)
}
