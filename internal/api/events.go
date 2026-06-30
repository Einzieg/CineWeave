package api

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func insertAPIEvent(ctx context.Context, tx pgx.Tx, organizationID, projectID, eventType, aggregateType, aggregateID string, payload json.RawMessage) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, organizationID, projectID, eventType, aggregateType, aggregateID, payload)
	return err
}
