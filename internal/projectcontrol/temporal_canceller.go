package projectcontrol

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/api/serviceerror"
)

type TemporalCancellationClient interface {
	CancelWorkflow(context.Context, string, string) error
}

type TemporalWorkflowCanceller struct {
	client TemporalCancellationClient
}

func NewTemporalWorkflowCanceller(client TemporalCancellationClient) *TemporalWorkflowCanceller {
	return &TemporalWorkflowCanceller{client: client}
}

func (c *TemporalWorkflowCanceller) Cancel(ctx context.Context, _ Command, links []WorkflowLink) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("Temporal cancellation client is unavailable")
	}
	for _, link := range links {
		if err := c.client.CancelWorkflow(ctx, link.TemporalWorkflowID, link.TemporalRunID); err != nil {
			var notFound *serviceerror.NotFound
			if errors.As(err, &notFound) {
				continue
			}
			return fmt.Errorf("cancel Temporal workflow %s: %w", link.TemporalWorkflowID, err)
		}
	}
	return nil
}
