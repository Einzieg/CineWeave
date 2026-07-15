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
	if options.StartToCloseTimeout != 15*time.Minute {
		t.Fatalf("StartToCloseTimeout = %s, want 15m", options.StartToCloseTimeout)
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
