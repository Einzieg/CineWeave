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
	providerRequestPollInterval  = 2 * time.Second
)

func providerTextGatewayOptionsForAttempt(options provider.GatewayTextOptions, attempt int32) provider.GatewayTextOptions {
	if options.TimeoutMS <= 0 {
		options.TimeoutMS = providerTextGatewayTimeoutMS
	}
	if attempt > 1 {
		options.Retry = true
	}
	return options
}

func providerTextGatewayOptions() provider.GatewayTextOptions {
	return providerTextGatewayOptionsForAttempt(provider.GatewayTextOptions{}, 1)
}

func providerTextActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.StartToCloseTimeout = 15 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	return options
}

func shotImagePromptReviewActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	// One logical shot may require up to three generate/review rounds. Keep the
	// deadline scoped to this supervised loop instead of widening every text
	// activity in the worker.
	options.StartToCloseTimeout = 45 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	// The activity contains its own bounded three-round correction state machine.
	// Replaying the whole activity would multiply those rounds and duplicate
	// already completed provider work; failed items are retried explicitly by
	// the batch workflow instead.
	if options.RetryPolicy != nil {
		options.RetryPolicy.MaximumAttempts = 1
	}
	return options
}

func commerceVideoPromptItemActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	// One Commerce shot may consume three generate/review rounds. Each text
	// request has its own ten-minute Gateway budget, so the enclosing supervised
	// loop needs a larger deadline without widening unrelated activities.
	options.StartToCloseTimeout = 75 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	// Provider requests and node runs are persisted inside the loop. A failed
	// item is retried through the production-run retry contract instead of
	// replaying the entire multi-call activity.
	if options.RetryPolicy != nil {
		options.RetryPolicy.MaximumAttempts = 1
	}
	return options
}

func commerceProjectSetupActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	// Project setup can run language resolution plus bounded localization and
	// review rounds. Each provider call has its own ten-minute budget, so the
	// enclosing durable activity must cover the whole supervised sequence.
	options.StartToCloseTimeout = 2 * time.Hour
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	return options
}

func providerImageActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	// The Gateway owns a bounded twenty-minute request budget across all image
	// fallback candidates. Leave enough time for response persistence and error
	// normalization before Temporal schedules a retry.
	options.StartToCloseTimeout = 25 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
	return options
}

func mediaProcessingActivityOptions() workflow.ActivityOptions {
	options := defaultActivityOptions()
	options.TaskQueue = MediaTaskQueue
	options.StartToCloseTimeout = 30 * time.Minute
	options.HeartbeatTimeout = providerTextHeartbeatTimeout
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
	req.Options = providerTextGatewayOptionsForAttempt(req.Options, currentActivityAttempt(ctx))
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

func (a Activities) generateProviderImage(ctx context.Context, execution NodeExecution, req provider.GatewayImageRequest) (provider.GatewayImageResponse, error) {
	if !execution.valid() || strings.TrimSpace(req.NodeRunID) != execution.NodeRunID {
		return provider.GatewayImageResponse{}, ErrWorkflowWriteFenced
	}
	stopHeartbeat := startWorkflowActivityHeartbeat(ctx, map[string]any{
		"nodeRunId": execution.NodeRunID,
		"phase":     "provider_image_generate",
	})
	defer stopHeartbeat()

	for {
		response, err := a.gateway.GenerateImage(ctx, req)
		if err == nil {
			return response, nil
		}
		standard, ok := provider.StandardErrorFromError(err)
		if !ok || standard.Code != provider.CodeProviderRequestInProgress {
			return provider.GatewayImageResponse{}, err
		}
		delay := providerRequestPollInterval
		if standard.RetryAfterMs > 0 {
			delay = time.Duration(standard.RetryAfterMs) * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return provider.GatewayImageResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func startProviderTextHeartbeat(ctx context.Context, nodeRunID string) func() {
	return startWorkflowActivityHeartbeat(ctx, map[string]any{
		"nodeRunId": nodeRunID,
		"phase":     "provider_text_stream",
	})
}

func startWorkflowActivityHeartbeat(ctx context.Context, details map[string]any) func() {
	done := make(chan struct{})
	recordWorkflowActivityHeartbeat(ctx, details)
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
				recordWorkflowActivityHeartbeat(ctx, details)
			}
		}
	}()
	return func() { close(done) }
}

func recordWorkflowActivityHeartbeat(ctx context.Context, details map[string]any) {
	defer func() {
		_ = recover()
	}()
	activity.RecordHeartbeat(ctx, details)
}

func currentActivityAttempt(ctx context.Context) (attempt int32) {
	defer func() {
		if recover() != nil {
			attempt = 1
		}
	}()
	attempt = activity.GetInfo(ctx).Attempt
	if attempt <= 0 {
		return 1
	}
	return attempt
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
