package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (c openAICompatibleClient) createVideoTask(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage, references []GatewayVideoReference) (manifestRunResult, error) {
	for _, reference := range references {
		if strings.TrimSpace(reference.URL) == "" {
			continue
		}
		if err := validateOutboundProviderReferenceURL(reference.URL, "video"); err != nil {
			return manifestRunResult{}, err
		}
	}
	endpoint, err := buildProviderURL(account.BaseURL, cfg.VideoCreateEndpoint, !cfg.DisableV1Prefix)
	if err != nil {
		return manifestRunResult{}, err
	}
	requestBody, err := buildOpenAICompatibleVideoRequest(model.ModelKey, input, references, cfg.VideoProtocol)
	if err != nil {
		return manifestRunResult{}, err
	}
	result, err := c.callVideoEndpoint(ctx, account, apiKey, http.MethodPost, endpoint, requestBody, true, false)
	if err != nil {
		return result, err
	}
	if layoutErr := validateOpenAICompatibleVideoCreateLayout(requestBody, result.NormalizedOutput); layoutErr != nil {
		standard, _ := StandardErrorFromError(layoutErr)
		result.NormalizedOutput = videoLayoutFailureOutput(
			result.NormalizedOutput,
			standard,
			videoMapString(requestBody, "size"),
			videoStringField(result.NormalizedOutput, "size"),
		)
		return result, layoutErr
	}
	return result, nil
}

func (c openAICompatibleClient) pollVideoTask(ctx context.Context, account Account, apiKey string, cfg openAICompatibleConfig, externalTaskID string) (manifestRunResult, error) {
	endpointTemplate := videoTaskEndpoint(cfg.VideoPollEndpoint, externalTaskID)
	endpoint, err := buildProviderURL(account.BaseURL, endpointTemplate, !cfg.DisableV1Prefix)
	if err != nil {
		return manifestRunResult{}, err
	}
	return c.callVideoEndpoint(ctx, account, apiKey, http.MethodGet, endpoint, nil, false, false)
}

func (c openAICompatibleClient) cancelVideoTask(ctx context.Context, account Account, apiKey string, cfg openAICompatibleConfig, externalTaskID string) (manifestRunResult, error) {
	if strings.TrimSpace(cfg.VideoCancelEndpoint) == "" {
		return manifestRunResult{}, fmt.Errorf("%w: provider video cancel endpoint is not configured", ErrValidation)
	}
	endpointTemplate := videoTaskEndpoint(cfg.VideoCancelEndpoint, externalTaskID)
	endpoint, err := buildProviderURL(account.BaseURL, endpointTemplate, !cfg.DisableV1Prefix)
	if err != nil {
		return manifestRunResult{}, err
	}
	return c.callVideoEndpoint(ctx, account, apiKey, http.MethodDelete, endpoint, nil, false, true)
}

func (c openAICompatibleClient) callVideoEndpoint(ctx context.Context, account Account, apiKey, method, endpoint string, requestBody map[string]any, requireTaskID, cancelled bool) (manifestRunResult, error) {
	var requestBytes []byte
	var bodyReader io.Reader
	var err error
	if requestBody != nil {
		requestBytes, err = json.Marshal(requestBody)
		if err != nil {
			return manifestRunResult{}, err
		}
		bodyReader = bytes.NewReader(requestBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return manifestRunResult{}, err
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	applyAuth(req, account.AuthType, apiKey)

	requestSnapshot := requestBytes
	if len(requestSnapshot) == 0 {
		requestSnapshot = mustJSON(map[string]any{"method": method, "url": endpoint})
	}
	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return manifestRunResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return manifestRunResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot}, err
	}
	if resp.StatusCode >= 400 {
		return manifestRunResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot, ResponseSnapshot: body}, upstreamError(resp.StatusCode, body)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte(`{}`)
	}
	normalized, status, err := normalizeOpenAICompatibleVideoResponse(body, requireTaskID, cancelled)
	if err != nil {
		return manifestRunResult{LatencyMS: latencyMS, RequestSnapshot: requestSnapshot, ResponseSnapshot: body}, err
	}
	return manifestRunResult{
		Status:           status,
		LatencyMS:        latencyMS,
		RequestSnapshot:  requestSnapshot,
		ResponseSnapshot: body,
		NormalizedOutput: normalized,
	}, nil
}

func buildOpenAICompatibleVideoRequest(modelKey string, input json.RawMessage, references []GatewayVideoReference, protocol string) (map[string]any, error) {
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return nil, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
	}
	prompt := strings.TrimSpace(videoStringOption(decoded, "prompt"))
	if prompt == "" {
		return nil, fmt.Errorf("%w: input.prompt is required", ErrValidation)
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	body := map[string]any{"model": strings.TrimSpace(modelKey), "prompt": prompt}
	if protocol != "openrouter" {
		body["n"] = 1
	}
	if duration := floatField(decoded["duration"], "duration"); duration > 0 {
		if protocol == "openrouter" {
			body["duration"] = int(math.Round(duration))
		} else {
			body["duration"] = duration
		}
	}
	if protocol == "openrouter" {
		appendOpenRouterVideoReferences(body, references)
		copyVideoRequestOption(body, decoded, "aspectRatio", "aspect_ratio")
		copyVideoRequestOption(body, decoded, "resolution", "resolution")
		copyVideoRequestOption(body, decoded, "generateAudio", "generate_audio")
	} else {
		for _, reference := range references {
			if strings.TrimSpace(reference.URL) != "" {
				body["image"] = strings.TrimSpace(reference.URL)
				break
			}
		}
	}

	if protocol == "" || protocol == "new_api" || protocol == "newapi" {
		if width, height := newAPIVideoDimensions(videoStringOption(decoded, "resolution"), videoStringOption(decoded, "aspectRatio")); width > 0 && height > 0 {
			// New API's /v1/video/generations contract reads size and ignores
			// width/height, which otherwise falls back to its portrait default.
			body["size"] = fmt.Sprintf("%dx%d", width, height)
		}
	} else if protocol != "openrouter" {
		copyVideoRequestOption(body, decoded, "aspectRatio", "aspect_ratio")
		copyVideoRequestOption(body, decoded, "resolution", "resolution")
		copyVideoRequestOption(body, decoded, "mode", "mode")
	}
	if negativePrompt := videoStringOption(decoded, "negativePrompt"); negativePrompt != "" {
		body["negative_prompt"] = negativePrompt
	}
	mergeVideoRequestOverrides(body, decoded["extraBody"])
	mergeVideoRequestOverrides(body, decoded["providerOptions"])
	return body, nil
}

func appendOpenRouterVideoReferences(body map[string]any, references []GatewayVideoReference) {
	frameImages := make([]map[string]any, 0, 2)
	inputReferences := make([]map[string]any, 0, len(references))
	for _, reference := range references {
		url := strings.TrimSpace(reference.URL)
		if url == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(reference.Type)) {
		case "first_frame", "last_frame":
			frameImages = append(frameImages, map[string]any{
				"frame_type": strings.ToLower(strings.TrimSpace(reference.Type)),
				"image_url":  url,
			})
		default:
			referenceType := "image_url"
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(reference.MimeType)), "video/") || strings.Contains(strings.ToLower(reference.Type), "video") {
				referenceType = "video_url"
			}
			inputReferences = append(inputReferences, map[string]any{
				"type": referenceType,
				"url":  url,
			})
		}
	}
	if len(frameImages) > 0 {
		body["frame_images"] = frameImages
	}
	if len(inputReferences) > 0 {
		body["input_references"] = inputReferences
	}
}

func newAPIVideoDimensions(resolution, aspectRatio string) (int, int) {
	normalizedResolution := strings.ToLower(strings.TrimSpace(resolution))
	if parts := strings.FieldsFunc(normalizedResolution, func(r rune) bool { return r == 'x' || r == '*' }); len(parts) == 2 {
		width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
			return width, height
		}
	}
	shortEdge := 0
	switch normalizedResolution {
	case "720p", "hd":
		shortEdge = 720
	case "1080p", "full_hd", "fhd":
		shortEdge = 1080
	}
	if shortEdge == 0 {
		return 0, 0
	}
	parts := strings.FieldsFunc(strings.TrimSpace(aspectRatio), func(r rune) bool { return r == ':' || r == '/' })
	if len(parts) != 2 {
		return 0, 0
	}
	widthRatio, widthErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	heightRatio, heightErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if widthErr != nil || heightErr != nil || widthRatio <= 0 || heightRatio <= 0 {
		return 0, 0
	}
	ratio := widthRatio / heightRatio
	if ratio >= 1 {
		return evenDimension(float64(shortEdge) * ratio), shortEdge
	}
	return shortEdge, evenDimension(float64(shortEdge) / ratio)
}

func normalizeOpenAICompatibleVideoResponse(body []byte, requireTaskID, cancelled bool) (json.RawMessage, string, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, "", fmt.Errorf("%w: provider video response is invalid", ErrValidation)
	}
	externalTaskID := firstVideoResponseString(decoded,
		"task_id", "taskId", "id", "data.task_id", "data.taskId", "data.id", "data.0.task_id", "data.0.taskId", "data.0.id", "output.task_id", "output.id", "result.task_id", "result.id")
	rawStatus := firstVideoResponseString(decoded, "status", "data.status", "data.data.status", "output.status", "result.status")
	videoURL := firstVideoResponseString(decoded,
		"url", "video_url", "videoUrl", "output_url", "unsigned_urls.0", "data.url", "data.video_url", "data.videoUrl", "data.0.url", "data.0.video_url", "output.url", "output.video_url", "output.videoUrl", "result.url", "result.video_url", "result.videoUrl")
	resultURL := firstVideoResponseString(decoded, "result_url", "resultUrl", "data.result_url", "data.resultUrl", "data.data.result_url", "data.data.resultUrl", "output.result_url", "output.resultUrl")
	mimeType := firstVideoResponseString(decoded, "mime_type", "mimeType", "data.mime_type", "output.mime_type", "result.mime_type")
	errorCode := firstVideoResponseString(decoded, "error.code", "code", "data.error.code", "data.data.error.code", "output.error.code", "result.error.code")
	errorMessage := firstVideoResponseString(decoded,
		"error.message", "fail_reason", "failReason", "message", "detail", "data.error.message", "data.data.error.message", "data.message", "data.data.message", "output.error.message", "result.error.message")
	duration := firstVideoResponseFloat(decoded, "duration", "duration_seconds", "durationSeconds", "data.duration", "output.duration", "result.duration", "metadata.duration")
	size := firstVideoResponseString(decoded, "size", "data.size", "data.0.size", "output.size", "result.size", "metadata.size")

	status := normalizeGatewayVideoStatus(rawStatus)
	switch {
	case cancelled:
		status = "cancelled"
	case status == "" && videoURL != "":
		status = "succeeded"
	case status == "" && errorMessage != "":
		status = "failed"
	case status == "":
		status = "running"
	}
	if status == "succeeded" && videoURL == "" {
		videoURL = resultURL
	}
	if status == "failed" && errorMessage == "" {
		errorMessage = resultURL
	}
	if requireTaskID && externalTaskID == "" && (status == "queued" || status == "running") {
		return nil, "", fmt.Errorf("%w: provider video create response did not include task id", ErrValidation)
	}
	normalized := map[string]any{
		"externalTaskId": externalTaskID,
		"status":         status,
		"videoUrl":       videoURL,
		"mimeType":       mimeType,
		"errorCode":      errorCode,
		"errorMessage":   errorMessage,
	}
	if size != "" {
		normalized["size"] = size
	}
	if duration > 0 {
		normalized["durationSeconds"] = duration
	}
	return mustJSON(normalized), status, nil
}

func validateOpenAICompatibleVideoCreateLayout(requestBody map[string]any, normalized json.RawMessage) error {
	requestedSize := videoMapString(requestBody, "size")
	providerSize := videoStringField(normalized, "size")
	requestedWidth, requestedHeight, requestedOK := parseVideoDimensions(requestedSize)
	providerWidth, providerHeight, providerOK := parseVideoDimensions(providerSize)
	if !requestedOK || !providerOK {
		return nil
	}
	return validateVideoOutputLayout(
		fmt.Sprintf("%d:%d", requestedWidth, requestedHeight),
		providerWidth,
		providerHeight,
	)
}

func videoMapString(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func videoTaskEndpoint(template, taskID string) string {
	template = strings.TrimSpace(template)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return template
	}
	replaced := strings.NewReplacer("{taskId}", taskID, "{task_id}", taskID, "{id}", taskID).Replace(template)
	if replaced == template {
		replaced = strings.TrimRight(template, "/") + "/" + taskID
	}
	return replaced
}

func firstVideoResponseString(value any, paths ...string) string {
	for _, path := range paths {
		if resolved, ok := videoResponsePath(value, path); ok {
			if text, ok := resolved.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func firstVideoResponseFloat(value any, paths ...string) float64 {
	for _, path := range paths {
		if resolved, ok := videoResponsePath(value, path); ok {
			if number := floatField(resolved, path); number > 0 {
				return number
			}
		}
	}
	return 0
}

func videoResponsePath(value any, path string) (any, bool) {
	current := value
	for _, part := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]any:
			current, _ = typed[part]
			if current == nil {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func copyVideoRequestOption(target, source map[string]any, sourceKey, targetKey string) {
	if value, ok := source[sourceKey]; ok {
		target[targetKey] = value
	}
}

func mergeVideoRequestOverrides(target map[string]any, value any) {
	overrides, ok := value.(map[string]any)
	if !ok {
		return
	}
	for key, item := range overrides {
		if key == "model" || key == "prompt" {
			continue
		}
		target[key] = item
	}
}

func evenDimension(value float64) int {
	rounded := int(math.Round(value))
	if rounded%2 != 0 {
		rounded++
	}
	return rounded
}
