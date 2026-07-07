package workflows

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/workflow"
)

const providerTextGatewayTimeoutMS = 10 * 60 * 1000

func providerTextGatewayOptions() provider.GatewayTextOptions {
	return provider.GatewayTextOptions{TimeoutMS: providerTextGatewayTimeoutMS}
}

func providerTextActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 15 * time.Minute
	return options
}

func longRunningProviderTextActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 6 * time.Hour
	return options
}

func (a Activities) generateProviderText(ctx context.Context, nodeRunID string, req provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
	if req.Options.TimeoutMS <= 0 {
		req.Options = providerTextGatewayOptions()
	}
	var text strings.Builder
	lastProgress := time.Now().Add(-providerTextProgressInterval)
	response, err := a.gateway.StreamText(ctx, req, func(delta provider.GatewayTextDelta) error {
		if strings.TrimSpace(delta.Text) == "" {
			return nil
		}
		text.WriteString(delta.Text)
		if time.Since(lastProgress) < providerTextProgressInterval {
			return nil
		}
		lastProgress = time.Now()
		_ = progressProviderText(ctx, a, nodeRunID, text.String(), false)
		return nil
	})
	if err == nil {
		_ = progressProviderText(ctx, a, nodeRunID, text.String(), true)
		return response, nil
	}
	if shouldFallbackToProviderGenerateText(err) {
		return a.gateway.GenerateText(ctx, req)
	}
	return provider.GatewayTextResponse{}, err
}

const (
	providerTextProgressInterval = 2 * time.Second
	providerTextProgressMaxRunes = 12000
)

func progressProviderText(ctx context.Context, a Activities, nodeRunID string, text string, completed bool) error {
	if strings.TrimSpace(nodeRunID) == "" || strings.TrimSpace(text) == "" {
		return nil
	}
	return ProgressNodeRun(ctx, a.db, nodeRunID, mustJSON(map[string]any{
		"status":        providerTextProgressStatus(completed),
		"streaming":     !completed,
		"partialText":   tailRunes(text, providerTextProgressMaxRunes),
		"receivedChars": len([]rune(text)),
		"updatedAt":     time.Now().UTC().Format(time.RFC3339),
	}))
}

func providerTextProgressStatus(completed bool) string {
	if completed {
		return "completed"
	}
	return "streaming"
}

func shouldFallbackToProviderGenerateText(err error) bool {
	var upstream *provider.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.Status == http.StatusNotFound || upstream.Status == http.StatusMethodNotAllowed
	}
	standard, ok := provider.StandardErrorFromError(err)
	if !ok {
		return false
	}
	message := strings.ToLower(standard.Message)
	return standard.Code == provider.CodeInvalidRequest && strings.Contains(message, "stream")
}

func tailRunes(value string, limit int) string {
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return "..." + string(runes[len(runes)-limit:])
}
