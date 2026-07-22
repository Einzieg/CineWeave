package workflows

import (
	"errors"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/provider"
	"go.temporal.io/sdk/temporal"
)

func TestProviderImageActivityTimeoutOutlivesGatewayClient(t *testing.T) {
	options := providerImageActivityOptions()
	if options.StartToCloseTimeout != 25*time.Minute {
		t.Fatalf("StartToCloseTimeout = %s, want 25m", options.StartToCloseTimeout)
	}
	if options.HeartbeatTimeout != providerTextHeartbeatTimeout {
		t.Fatalf("HeartbeatTimeout = %s, want %s", options.HeartbeatTimeout, providerTextHeartbeatTimeout)
	}
}

func TestMediaProcessingActivityOptionsHaveHeartbeat(t *testing.T) {
	options := mediaProcessingActivityOptions()
	if options.TaskQueue != MediaTaskQueue || options.StartToCloseTimeout != 30*time.Minute || options.HeartbeatTimeout != providerTextHeartbeatTimeout {
		t.Fatalf("media activity options = %+v", options)
	}
}

func TestProviderContentRejectionIsNonRetryable(t *testing.T) {
	cause := workflowError{
		Code:              provider.CodeContentRejected,
		Message:           "content rejected",
		Retryable:         false,
		RetryabilityKnown: true,
	}
	err := newWorkflowApplicationError(cause, cause.Code, cause.Message)
	var applicationErr *temporal.ApplicationError
	if !errors.As(err, &applicationErr) {
		t.Fatalf("error type = %T, want *temporal.ApplicationError", err)
	}
	if !applicationErr.NonRetryable() {
		t.Fatal("content rejection must not be retried")
	}
}

func TestProviderTimeoutRemainsRetryable(t *testing.T) {
	cause := workflowError{
		Code:              provider.CodeUpstreamTimeout,
		Message:           "provider request timed out",
		Retryable:         true,
		RetryabilityKnown: true,
	}
	err := newWorkflowApplicationError(cause, cause.Code, cause.Message)
	var applicationErr *temporal.ApplicationError
	if !errors.As(err, &applicationErr) {
		t.Fatalf("error type = %T, want *temporal.ApplicationError", err)
	}
	if applicationErr.NonRetryable() {
		t.Fatal("provider timeout should remain retryable")
	}
}
