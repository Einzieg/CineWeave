package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type GatewayClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewGatewayClientFromEnv() *GatewayClient {
	return &GatewayClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(envValue("PROVIDER_GATEWAY_URL", "http://localhost:8082")), "/"),
		Token:   strings.TrimSpace(envValue("CINEWEAVE_SERVICE_TOKEN", "dev-service-token")),
		Client:  &http.Client{Timeout: gatewayClientTimeoutFromEnv()},
	}
}

func (c *GatewayClient) GenerateText(ctx context.Context, req GatewayTextRequest) (GatewayTextResponse, error) {
	var response GatewayTextResponse
	if err := c.postJSON(ctx, "/internal/provider/text/generate", req, &response); err != nil {
		return GatewayTextResponse{}, err
	}
	if isProviderFailureStatus(response.Status) {
		return GatewayTextResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) StreamText(ctx context.Context, req GatewayTextRequest, onDelta func(GatewayTextDelta) error) (GatewayTextResponse, error) {
	response, err := c.postStream(ctx, req, onDelta)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	if isProviderFailureStatus(response.Status) {
		return GatewayTextResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) GenerateImage(ctx context.Context, req GatewayImageRequest) (GatewayImageResponse, error) {
	var response GatewayImageResponse
	if err := c.postJSON(ctx, "/internal/provider/image/generate", req, &response); err != nil {
		return GatewayImageResponse{}, err
	}
	if isProviderFailureStatus(response.Status) {
		return GatewayImageResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) CreateVideoTask(ctx context.Context, req GatewayVideoCreateTaskRequest) (GatewayVideoCreateTaskResponse, error) {
	var response GatewayVideoCreateTaskResponse
	if err := c.postJSON(ctx, "/internal/provider/video/create-task", req, &response); err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	if isProviderFailureStatus(response.Status) {
		return GatewayVideoCreateTaskResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) PollVideoTask(ctx context.Context, req GatewayVideoPollTaskRequest) (GatewayVideoPollTaskResponse, error) {
	var response GatewayVideoPollTaskResponse
	if err := c.postJSON(ctx, "/internal/provider/video/poll-task", req, &response); err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if isProviderFailureStatus(response.Status) {
		return GatewayVideoPollTaskResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) CancelVideoTask(ctx context.Context, req GatewayVideoCancelTaskRequest) (GatewayVideoCancelTaskResponse, error) {
	var response GatewayVideoCancelTaskResponse
	if err := c.postJSON(ctx, "/internal/provider/video/cancel-task", req, &response); err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	if isProviderFailureStatus(response.Status) {
		return GatewayVideoCancelTaskResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) postJSON(ctx context.Context, path string, payload any, target any) error {
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("%w: PROVIDER_GATEWAY_URL is required", ErrProviderGatewayRequired)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	httpClient := c.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: gatewayClientTimeoutFromEnv()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return gatewayHTTPError(resp.StatusCode, responseBody)
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *StandardError  `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("%w: provider gateway response is invalid", ErrValidation)
	}
	if envelope.Error != nil {
		return errorFromGatewayStandard(envelope.Error)
	}
	if target == nil || len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("%w: provider gateway data is invalid", ErrValidation)
	}
	return nil
}

func (c *GatewayClient) postStream(ctx context.Context, payload GatewayTextRequest, onDelta func(GatewayTextDelta) error) (GatewayTextResponse, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: PROVIDER_GATEWAY_URL is required", ErrProviderGatewayRequired)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	httpClient := c.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: gatewayClientTimeoutFromEnv()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/internal/provider/text/stream", bytes.NewReader(body))
	if err != nil {
		return GatewayTextResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(c.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if readErr != nil {
			return GatewayTextResponse{}, readErr
		}
		return GatewayTextResponse{}, gatewayHTTPError(resp.StatusCode, responseBody)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	event := ""
	var completed GatewayTextResponse
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			event = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		switch event {
		case "provider.delta":
			var delta GatewayTextDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream delta is invalid", ErrValidation)
			}
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					return GatewayTextResponse{}, err
				}
			}
		case "provider.completed":
			if err := json.Unmarshal([]byte(data), &completed); err != nil {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream completion is invalid", ErrValidation)
			}
		case "provider.error":
			var standard StandardError
			if err := json.Unmarshal([]byte(data), &standard); err != nil {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream error is invalid", ErrValidation)
			}
			return GatewayTextResponse{}, errorFromGatewayStandard(&standard)
		}
	}
	if err := scanner.Err(); err != nil {
		return GatewayTextResponse{}, err
	}
	if completed.Status == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream did not complete", ErrValidation)
	}
	return completed, nil
}

func envValue(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
