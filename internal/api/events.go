package api

import (
	"context"
	"encoding/json"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/jackc/pgx/v5"
)

func insertAPIEvent(ctx context.Context, exec events.Execer, organizationID, projectID, eventType, aggregateType, aggregateID string, payload json.RawMessage) error {
	return events.AppendTx(ctx, exec, organizationID, projectID, eventType, aggregateType, aggregateID, payload)
}

func insertWorkflowQueuedEventTx(ctx context.Context, tx pgx.Tx, run WorkflowRun, workflowType string) error {
	return insertAPIEvent(ctx, tx, run.OrganizationID, run.ProjectID, "workflow.run.queued", "workflow_run", run.ID, mustMarshal(map[string]any{
		"workflowRunId": run.ID,
		"workflowType":  workflowType,
		"status":        "queued",
		"createdAt":     run.CreatedAt,
	}))
}
