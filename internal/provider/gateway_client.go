package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

func (c *GatewayClient) EnsureManagedProviderAccount(
	ctx context.Context,
	req EnsureManagedProviderAccountRequest,
) (ManagedProviderAccountResult, error) {
	var response ManagedProviderAccountResult
	if err := c.postJSON(
		ctx,
		"/internal/provider/v1/managed-accounts/ensure",
		req,
		&response,
	); err != nil {
		return ManagedProviderAccountResult{}, err
	}
	return response, nil
}

func (c *GatewayClient) ImportManagedCredential(
	ctx context.Context,
	req ImportManagedCredentialRequest,
) (ManagedCredentialResult, error) {
	var response ManagedCredentialResult
	if err := c.postJSON(
		ctx,
		"/internal/provider/v1/credential-imports",
		req,
		&response,
	); err != nil {
		return ManagedCredentialResult{}, err
	}
	return response, nil
}

func (c *GatewayClient) ResolveManagedCredential(
	ctx context.Context,
	req ResolveManagedCredentialRequest,
) (ManagedCredentialResult, error) {
	var response ManagedCredentialResult
	if err := c.postJSON(
		ctx,
		"/internal/provider/v1/credential-imports/resolve",
		req,
		&response,
	); err != nil {
		return ManagedCredentialResult{}, err
	}
	return response, nil
}

func (c *GatewayClient) ActivateManagedCredential(
	ctx context.Context,
	req ActivateManagedCredentialRequest,
) (ManagedCredentialResult, error) {
	var response ManagedCredentialResult
	if err := c.postJSON(
		ctx,
		"/internal/provider/v1/credential-imports/activate",
		req,
		&response,
	); err != nil {
		return ManagedCredentialResult{}, err
	}
	return response, nil
}

func (c *GatewayClient) RevokeManagedCredential(
	ctx context.Context,
	req RevokeManagedCredentialRequest,
) error {
	return c.postJSON(
		ctx,
		"/internal/provider/v1/credential-imports/revoke",
		req,
		nil,
	)
}

func (c *GatewayClient) ResolveModelConstraints(ctx context.Context, req GatewayModelConstraintsRequest) (GatewayModelConstraintsResponse, error) {
	var response GatewayModelConstraintsResponse
	if err := c.postJSON(ctx, "/internal/provider/models/constraints", req, &response); err != nil {
		return GatewayModelConstraintsResponse{}, err
	}
	return response, nil
}

func (c *GatewayClient) GenerateText(ctx context.Context, req GatewayTextRequest) (GatewayTextResponse, error) {
	var response GatewayTextResponse
	if err := c.postJSON(ctx, "/internal/provider/text/generate", req, &response); err != nil {
		return GatewayTextResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayTextResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) StreamText(ctx context.Context, req GatewayTextRequest, onDelta func(GatewayTextDelta) error) (GatewayTextResponse, error) {
	response, err := c.postStream(ctx, req, onDelta)
	if err != nil {
		return GatewayTextResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayTextResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) GenerateImage(ctx context.Context, req GatewayImageRequest) (GatewayImageResponse, error) {
	var response GatewayImageResponse
	if err := c.postJSON(ctx, "/internal/provider/image/generate", req, &response); err != nil {
		return GatewayImageResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayImageResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) GenerateSpeech(ctx context.Context, req GatewayTTSRequest) (GatewayTTSResponse, error) {
	var response GatewayTTSResponse
	if err := c.postJSON(ctx, "/internal/provider/audio/tts", req, &response); err != nil {
		return GatewayTTSResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayTTSResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) TranscribeAudio(ctx context.Context, req GatewayASRRequest) (GatewayASRResponse, error) {
	var response GatewayASRResponse
	if err := c.postJSON(ctx, "/internal/provider/audio/transcribe", req, &response); err != nil {
		return GatewayASRResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayASRResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) PlanVideo(ctx context.Context, req GatewayVideoPlanRequest) (GatewayVideoPlanResponse, error) {
	var response GatewayVideoPlanResponse
	if err := c.postJSON(ctx, "/internal/provider/video/plan", req, &response); err != nil {
		return GatewayVideoPlanResponse{}, err
	}
	return response, nil
}

func (c *GatewayClient) RetryVideoRenderSegment(ctx context.Context, req GatewayVideoRetrySegmentRequest) (GatewayVideoRetrySegmentResponse, error) {
	var response GatewayVideoRetrySegmentResponse
	if err := c.postJSON(ctx, "/internal/provider/video/retry-segment", req, &response); err != nil {
		return GatewayVideoRetrySegmentResponse{}, err
	}
	return response, nil
}

func (c *GatewayClient) CreateVideoTask(ctx context.Context, req GatewayVideoCreateTaskRequest) (GatewayVideoCreateTaskResponse, error) {
	response, err := c.CreateVideoTaskResult(ctx, req)
	if err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayVideoCreateTaskResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) CreateVideoTaskResult(ctx context.Context, req GatewayVideoCreateTaskRequest) (GatewayVideoCreateTaskResponse, error) {
	var response GatewayVideoCreateTaskResponse
	if err := c.postJSON(ctx, "/internal/provider/video/create-task", req, &response); err != nil {
		return GatewayVideoCreateTaskResponse{}, err
	}
	return response, nil
}

func (c *GatewayClient) PollVideoTask(ctx context.Context, req GatewayVideoPollTaskRequest) (GatewayVideoPollTaskResponse, error) {
	response, err := c.PollVideoTaskResult(ctx, req)
	if err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
		return GatewayVideoPollTaskResponse{}, errorFromGatewayStandard(response.Error)
	}
	return response, nil
}

func (c *GatewayClient) PollVideoTaskResult(ctx context.Context, req GatewayVideoPollTaskRequest) (GatewayVideoPollTaskResponse, error) {
	var response GatewayVideoPollTaskResponse
	if err := c.postJSON(ctx, "/internal/provider/video/poll-task", req, &response); err != nil {
		return GatewayVideoPollTaskResponse{}, err
	}
	return response, nil
}

func (c *GatewayClient) CancelVideoTask(ctx context.Context, req GatewayVideoCancelTaskRequest) (GatewayVideoCancelTaskResponse, error) {
	var response GatewayVideoCancelTaskResponse
	if err := c.postJSON(ctx, "/internal/provider/video/cancel-task", req, &response); err != nil {
		return GatewayVideoCancelTaskResponse{}, err
	}
	if response.Error != nil || isProviderFailureStatus(response.Status) {
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
		return normalizeGatewayTransportError(err)
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

func normalizeGatewayTransportError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &StandardErrorError{Standard: StandardError{
			Code:      CodeUpstreamTimeout,
			Message:   "provider request timed out",
			Retryable: true,
		}}
	}
	return err
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
	decoder := newSSEDecoder(resp.Body, defaultSSEMaxEventBytes)
	var completed GatewayTextResponse
	lastSequenceByCall := make(map[string]int64)
	var streamedGeneration int
	var streamedAttemptSequence int
	var streamedCallID string
	for {
		event, ok, err := decoder.Next()
		if err != nil {
			return GatewayTextResponse{}, err
		}
		if !ok {
			break
		}
		data := strings.TrimSpace(event.Data)
		if data == "" {
			continue
		}
		switch event.Event {
		case GatewayTextEventAttemptStarted, GatewayTextEventAttemptFailed:
			var attempt GatewayTextAttemptEvent
			if err := json.Unmarshal([]byte(data), &attempt); err != nil || !validGatewayTextAttemptEvent(attempt) {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream attempt event is invalid", ErrValidation)
			}
		case GatewayTextEventDelta:
			var delta GatewayTextDelta
			if err := json.Unmarshal([]byte(data), &delta); err != nil || !validGatewayTextDelta(delta) {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream delta is invalid", ErrValidation)
			}
			if streamedGeneration != 0 && streamedGeneration != delta.AttemptGeneration {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream generation changed after the first delta", ErrValidation)
			}
			if streamedCallID != "" && (streamedCallID != delta.ProviderCallID || streamedAttemptSequence != delta.AttemptSequence) {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway changed attempts after emitting a delta", ErrValidation)
			}
			lastSequence := lastSequenceByCall[delta.ProviderCallID]
			if delta.Sequence <= lastSequence {
				continue
			}
			if delta.Sequence != lastSequence+1 {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream delta sequence has a gap", ErrValidation)
			}
			streamedGeneration = delta.AttemptGeneration
			streamedAttemptSequence = delta.AttemptSequence
			streamedCallID = delta.ProviderCallID
			lastSequenceByCall[delta.ProviderCallID] = delta.Sequence
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					return GatewayTextResponse{}, err
				}
			}
		case GatewayTextEventCompleted:
			if err := json.Unmarshal([]byte(data), &completed); err != nil || !validGatewayTextCompletion(completed) {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream completion is invalid", ErrValidation)
			}
		case GatewayTextEventReplayed:
			var replay GatewayTextReplayEvent
			if err := json.Unmarshal([]byte(data), &replay); err != nil || !validGatewayTextReplay(replay) {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream replay is invalid", ErrValidation)
			}
			completed = GatewayTextResponse{
				SchemaVersion:     replay.SchemaVersion,
				ProviderRequestID: replay.ProviderRequestID,
				AttemptGeneration: replay.AttemptGeneration,
				AttemptSequence:   replay.AttemptSequence,
				ProviderCallID:    replay.ProviderCallID,
				ModelID:           replay.ModelID,
				Status:            replay.Status,
				Output:            replay.Output,
				Usage:             replay.Usage,
				LatencyMS:         replay.LatencyMS,
			}
		case GatewayTextEventFailed:
			var failure GatewayTextFailureEvent
			if err := json.Unmarshal([]byte(data), &failure); err != nil || failure.SchemaVersion != GatewayTextStreamSchemaVersion || failure.Error == nil {
				return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream error is invalid", ErrValidation)
			}
			return GatewayTextResponse{}, errorFromGatewayStandard(failure.Error)
		default:
			return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream event %q is not supported", ErrValidation, event.Event)
		}
	}
	if completed.Status == "" {
		return GatewayTextResponse{}, fmt.Errorf("%w: provider gateway stream did not complete", ErrValidation)
	}
	return completed, nil
}

func validGatewayTextAttemptEvent(event GatewayTextAttemptEvent) bool {
	return event.SchemaVersion == GatewayTextStreamSchemaVersion &&
		strings.TrimSpace(event.ProviderRequestID) != "" &&
		strings.TrimSpace(event.ProviderCallID) != "" &&
		event.AttemptGeneration > 0 && event.AttemptSequence > 0 &&
		strings.TrimSpace(event.Status) != ""
}

func validGatewayTextDelta(delta GatewayTextDelta) bool {
	return delta.SchemaVersion == GatewayTextStreamSchemaVersion &&
		strings.TrimSpace(delta.ProviderRequestID) != "" &&
		strings.TrimSpace(delta.ProviderCallID) != "" &&
		delta.AttemptGeneration > 0 && delta.AttemptSequence > 0 && delta.Sequence > 0
}

func validGatewayTextCompletion(response GatewayTextResponse) bool {
	return response.SchemaVersion == GatewayTextStreamSchemaVersion &&
		strings.TrimSpace(response.ProviderRequestID) != "" &&
		strings.TrimSpace(response.ProviderCallID) != "" &&
		response.AttemptGeneration > 0 && response.AttemptSequence > 0 &&
		response.Status == "succeeded"
}

func validGatewayTextReplay(replay GatewayTextReplayEvent) bool {
	return replay.SchemaVersion == GatewayTextStreamSchemaVersion &&
		strings.TrimSpace(replay.ProviderRequestID) != "" &&
		strings.TrimSpace(replay.ProviderCallID) != "" &&
		replay.AttemptGeneration > 0 && replay.AttemptSequence > 0 &&
		replay.Status == "succeeded"
}

func envValue(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
