package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider/outbound"
)

const (
	maxGatewayTTSBytes = 64 << 20
	maxGatewayASRBytes = 32 << 20
)

type audioSpeechResult struct {
	RequestSnapshot  json.RawMessage
	ResponseSnapshot json.RawMessage
	NormalizedOutput json.RawMessage
	TempPath         string
	MimeType         string
	ByteSize         int64
	ContentHash      string
	ResponseFormat   string
	LatencyMS        int
}

type audioTranscriptionResult struct {
	RequestSnapshot  json.RawMessage
	ResponseSnapshot json.RawMessage
	NormalizedOutput json.RawMessage
	Output           GatewayASROutput
	LatencyMS        int
}

func (c openAICompatibleClient) audioSpeech(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage) (audioSpeechResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.AudioSpeechEndpoint, !cfg.DisableV1Prefix)
	if err != nil {
		return audioSpeechResult{}, err
	}
	requestBody, responseFormat, err := buildAudioSpeechRequest(model.ModelKey, input)
	if err != nil {
		return audioSpeechResult{}, err
	}
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return audioSpeechResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBytes))
	if err != nil {
		return audioSpeechResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", audioMimeType(responseFormat))
	applyAuth(req, account.AuthType, apiKey)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return audioSpeechResult{RequestSnapshot: requestBytes, LatencyMS: latencyMS}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if readErr != nil {
			return audioSpeechResult{RequestSnapshot: requestBytes, LatencyMS: latencyMS}, readErr
		}
		return audioSpeechResult{RequestSnapshot: requestBytes, ResponseSnapshot: body, LatencyMS: latencyMS}, upstreamError(resp.StatusCode, body)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mediaType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
		contentType = mediaType
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = audioMimeType(responseFormat)
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		return audioSpeechResult{RequestSnapshot: requestBytes, LatencyMS: latencyMS}, fmt.Errorf("%w: provider audio response did not contain binary media", ErrValidation)
	}
	fetcher := c.mediaFetcher
	if fetcher == nil {
		fetcher = outbound.NewMediaFetcher(outbound.Config{})
	}
	spoolTimeout := 2 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		spoolTimeout = time.Until(deadline)
		if spoolTimeout <= 0 {
			return audioSpeechResult{RequestSnapshot: requestBytes, LatencyMS: latencyMS}, ctx.Err()
		}
	}
	media, err := fetcher.SpoolToTempFile(ctx, resp.Body, outbound.FetchOptions{
		Kind:                "audio",
		MaxBytes:            maxGatewayTTSBytes,
		Timeout:             spoolTimeout,
		UpstreamMIMEType:    contentType,
		AllowedMIMEPrefixes: []string{"audio/"},
	})
	if err != nil {
		return audioSpeechResult{RequestSnapshot: requestBytes, LatencyMS: latencyMS}, err
	}
	responseSnapshot := mustJSON(map[string]any{
		"contentType": media.MIMEType,
		"byteSize":    media.ByteSize,
		"format":      responseFormat,
	})
	normalized := mustJSON(map[string]any{
		"mimeType": media.MIMEType,
		"byteSize": media.ByteSize,
		"format":   responseFormat,
	})
	return audioSpeechResult{
		RequestSnapshot: requestBytes, ResponseSnapshot: responseSnapshot, NormalizedOutput: normalized,
		TempPath: media.Path, MimeType: media.MIMEType, ByteSize: media.ByteSize, ContentHash: media.ContentHash,
		ResponseFormat: responseFormat, LatencyMS: latencyMS,
	}, nil
}

func buildAudioSpeechRequest(modelKey string, input json.RawMessage) (map[string]any, string, error) {
	var decoded map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return nil, "", fmt.Errorf("%w: input must be valid JSON", ErrValidation)
		}
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	text := firstAudioString(decoded, "input", "text")
	voice := firstAudioString(decoded, "voice", "voiceKey")
	if text == "" || voice == "" {
		return nil, "", fmt.Errorf("%w: audio TTS input and voice are required", ErrValidation)
	}
	responseFormat := strings.ToLower(firstAudioString(decoded, "response_format", "responseFormat", "format"))
	if responseFormat == "" {
		responseFormat = "mp3"
	}
	if !supportedAudioResponseFormat(responseFormat) {
		return nil, "", fmt.Errorf("%w: audio response format is not supported", ErrValidation)
	}
	requestBody := map[string]any{
		"model":           modelKey,
		"input":           text,
		"voice":           voice,
		"response_format": responseFormat,
	}
	for _, key := range []string{"instructions", "speed"} {
		if value, ok := decoded[key]; ok {
			requestBody[key] = value
		}
	}
	if extra, ok := decoded["extraBody"].(map[string]any); ok {
		for key, value := range extra {
			if key == "model" || key == "input" || key == "voice" || key == "response_format" {
				continue
			}
			requestBody[key] = value
		}
	}
	return requestBody, responseFormat, nil
}

func (c openAICompatibleClient) audioTranscription(ctx context.Context, account Account, model Model, apiKey string, cfg openAICompatibleConfig, input json.RawMessage, audioBody []byte, mimeType, fileName string) (audioTranscriptionResult, error) {
	endpoint, err := buildProviderURL(account.BaseURL, cfg.AudioTranscriptionsEndpoint, !cfg.DisableV1Prefix)
	if err != nil {
		return audioTranscriptionResult{}, err
	}
	requestBody, contentType, requestSnapshot, err := buildAudioTranscriptionBody(model.ModelKey, input, audioBody, mimeType, fileName)
	if err != nil {
		return audioTranscriptionResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return audioTranscriptionResult{}, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	applyAuth(req, account.AuthType, apiKey)

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		return audioTranscriptionResult{RequestSnapshot: requestSnapshot, LatencyMS: latencyMS}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayASRBytes+1))
	if err != nil {
		return audioTranscriptionResult{RequestSnapshot: requestSnapshot, LatencyMS: latencyMS}, err
	}
	if len(body) > maxGatewayASRBytes {
		return audioTranscriptionResult{RequestSnapshot: requestSnapshot, LatencyMS: latencyMS}, fmt.Errorf("%w: provider transcription response exceeds size limit", ErrValidation)
	}
	if resp.StatusCode >= 400 {
		return audioTranscriptionResult{RequestSnapshot: requestSnapshot, ResponseSnapshot: body, LatencyMS: latencyMS}, upstreamError(resp.StatusCode, body)
	}
	output, err := parseAudioTranscriptionResponse(body)
	if err != nil {
		return audioTranscriptionResult{RequestSnapshot: requestSnapshot, ResponseSnapshot: body, LatencyMS: latencyMS}, err
	}
	output.Raw = append(json.RawMessage(nil), body...)
	normalized := mustJSON(output)
	return audioTranscriptionResult{
		RequestSnapshot: requestSnapshot, ResponseSnapshot: body, NormalizedOutput: normalized,
		Output: output, LatencyMS: latencyMS,
	}, nil
}

func buildAudioTranscriptionBody(modelKey string, input json.RawMessage, audioBody []byte, mimeType, fileName string) (io.Reader, string, json.RawMessage, error) {
	var decoded map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return nil, "", nil, fmt.Errorf("%w: input must be valid JSON", ErrValidation)
		}
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	if len(audioBody) == 0 {
		return nil, "", nil, fmt.Errorf("%w: transcription audio is required", ErrValidation)
	}
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	name := filepath.Base(strings.TrimSpace(fileName))
	if name == "" || name == "." {
		name = "audio" + audioExtension(mimeType)
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return nil, "", nil, err
	}
	if _, err := part.Write(audioBody); err != nil {
		return nil, "", nil, err
	}
	fields := map[string]string{"model": modelKey}
	for _, key := range []string{"language", "prompt", "response_format", "temperature"} {
		if value := fmt.Sprint(decoded[key]); value != "<nil>" && strings.TrimSpace(value) != "" {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if fields["response_format"] == "" {
		fields["response_format"] = "verbose_json"
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", nil, err
		}
	}
	granularities := stringsFromAny(decoded["timestamp_granularities"])
	if len(granularities) == 0 {
		granularities = []string{"segment", "word"}
	}
	for _, value := range granularities {
		if err := writer.WriteField("timestamp_granularities[]", value); err != nil {
			return nil, "", nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", nil, err
	}
	snapshot := mustJSON(map[string]any{
		"model": modelKey, "fileName": name, "mimeType": mimeType, "byteSize": len(audioBody),
		"language": fields["language"], "responseFormat": fields["response_format"], "timestampGranularities": granularities,
	})
	return bytes.NewReader(buffer.Bytes()), writer.FormDataContentType(), snapshot, nil
}

func parseAudioTranscriptionResponse(body []byte) (GatewayASROutput, error) {
	var output GatewayASROutput
	if err := json.Unmarshal(body, &output); err == nil && strings.TrimSpace(output.Text) != "" {
		return output, nil
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return GatewayASROutput{}, fmt.Errorf("%w: provider transcription response is empty", ErrValidation)
	}
	return GatewayASROutput{Text: text}, nil
}

func firstAudioString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func supportedAudioResponseFormat(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mp3", "opus", "aac", "flac", "wav", "pcm":
		return true
	default:
		return false
	}
}

func audioMimeType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "pcm":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}

func audioExtension(mimeType string) string {
	mediaType, _, _ := mime.ParseMediaType(strings.TrimSpace(mimeType))
	switch mediaType {
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/aac":
		return ".aac"
	case "audio/flac":
		return ".flac"
	default:
		return ".mp3"
	}
}
