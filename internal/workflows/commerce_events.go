package workflows

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func appendCommerceWorkflowEvent(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	eventName string,
	aggregateType string,
	aggregateID string,
	payload map[string]any,
) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return insertEvent(ctx, tx, organizationID, projectID, eventName, aggregateType, aggregateID, raw)
}
