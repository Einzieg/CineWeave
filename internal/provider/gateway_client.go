package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
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
		Client:  &http.Client{Timeout: 2 * time.Minute},
	}
}

func (c *GatewayClient) GenerateText(ctx context.Context, req GatewayTextRequest) (GatewayTextResponse, error) {
	var response GatewayTextResponse
	if err := c.postJSON(ctx, "/internal/provider/text/generate", req, &response); err != nil {
		return GatewayTextResponse{}, err
	}
	if response.Status == "failed" {
		return GatewayTextResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) GenerateImage(ctx context.Context, req GatewayImageRequest) (GatewayImageResponse, error) {
	var response GatewayImageResponse
	if err := c.postJSON(ctx, "/internal/provider/image/generate", req, &response); err != nil {
		return GatewayImageResponse{}, err
	}
	if response.Status == "failed" {
		return GatewayImageResponse{}, errorFromGatewayStandard(response.Error)
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
		httpClient = &http.Client{Timeout: 2 * time.Minute}
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

func envValue(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
