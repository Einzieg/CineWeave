package workflows

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

const (
	providerTextGatewayTimeoutMS = 10 * 60 * 1000
	providerTextHeartbeatTimeout = 45 * time.Second
	providerTextHeartbeatEvery   = 10 * time.Second
)

func providerTextGatewayOptions() provider.GatewayTextOptions {
	return provider.GatewayTextOptions{TimeoutMS: providerTextGatewayTimeoutMS}
}

func providerTextActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 15 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	return options
}

func providerImageActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	// The Gateway client may wait up to eleven minutes for image providers.
	// Keep Temporal's activity deadline outside that window so the activity can
	// persist the terminal provider result before Temporal schedules a retry.
	options.StartToCloseTimeout = 15 * time.Minute
	return options
}

func longRunningProviderTextActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 6 * time.Hour
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	return options
}

func scriptEpisodeGenerationActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 90 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	if options.RetryPolicy != nil {
		options.RetryPolicy.MaximumAttempts = 3
	}
	return options
}

func storyboardEpisodeGenerationActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 90 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	if options.RetryPolicy != nil {
		options.RetryPolicy.MaximumAttempts = 3
	}
	return options
}

func (a Activities) generateProviderText(ctx context.Context, execution NodeExecution, req provider.GatewayTextRequest) (provider.GatewayTextResponse, error) {
	if !execution.valid() || strings.TrimSpace(req.NodeRunID) != execution.NodeRunID {
		return provider.GatewayTextResponse{}, ErrWorkflowWriteFenced
	}
	if req.Options.TimeoutMS <= 0 {
		req.Options = providerTextGatewayOptions()
	}
	var text strings.Builder
	receivedDelta := false
	stopHeartbeat := startProviderTextHeartbeat(ctx, execution.NodeRunID)
	defer stopHeartbeat()
	lastProgress := time.Now().Add(-providerTextProgressInterval)
	response, err := a.gateway.StreamText(ctx, req, func(delta provider.GatewayTextDelta) error {
		if strings.TrimSpace(delta.Text) == "" {
			return nil
		}
		receivedDelta = true
		text.WriteString(delta.Text)
		if time.Since(lastProgress) < providerTextProgressInterval {
			return nil
		}
		lastProgress = time.Now()
		_ = progressProviderText(ctx, a, execution, text.String(), false)
		return nil
	})
	if err == nil {
		_ = progressProviderText(ctx, a, execution, text.String(), true)
		return response, nil
	}
	if !receivedDelta && shouldFallbackToProviderGenerateText(err) {
		return a.gateway.GenerateText(ctx, req)
	}
	return provider.GatewayTextResponse{}, err
}

func startProviderTextHeartbeat(ctx context.Context, nodeRunID string) func() {
	done := make(chan struct{})
	recordProviderTextHeartbeat(ctx, nodeRunID)
	go func() {
		ticker := time.NewTicker(providerTextHeartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				recordProviderTextHeartbeat(ctx, nodeRunID)
			}
		}
	}()
	return func() { close(done) }
}

func recordProviderTextHeartbeat(ctx context.Context, nodeRunID string) {
	defer func() {
		_ = recover()
	}()
	activity.RecordHeartbeat(ctx, map[string]any{
		"nodeRunId": nodeRunID,
		"phase":     "provider_text_stream",
	})
}

const (
	providerTextProgressInterval = 2 * time.Second
	providerTextProgressMaxRunes = 12000
)

func progressProviderText(ctx context.Context, a Activities, execution NodeExecution, text string, completed bool) error {
	if a.db == nil || !execution.valid() || strings.TrimSpace(text) == "" {
		return nil
	}
	return ProgressNodeRun(ctx, a.db, execution, mustJSON(map[string]any{
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
