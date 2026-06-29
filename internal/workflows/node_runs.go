package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type NodeRunInput struct {
	OrganizationID string
	ProjectID      string
	WorkflowRunID  string
	NodeKey        string
	NodeType       string
	Input          json.RawMessage
}

type nodeRunContext struct {
	OrganizationID string
	ProjectID      string
	WorkflowRunID  string
	NodeKey        string
}

func StartNodeRun(ctx context.Context, db txBeginner, input NodeRunInput) (string, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return "", fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if strings.TrimSpace(input.NodeKey) == "" || strings.TrimSpace(input.NodeType) == "" {
		return "", fmt.Errorf("nodeKey and nodeType are required")
	}
	nodeInput := input.Input
	if len(nodeInput) == 0 {
		nodeInput = json.RawMessage(`{}`)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'running', started_at = COALESCE(started_at, now())
		WHERE id = $1
	`, input.WorkflowRunID); err != nil {
		return "", err
	}
	var nodeRunID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_node_runs(organization_id, project_id, workflow_run_id, node_key, node_type, status, input, started_at)
		VALUES ($1, $2, $3, $4, $5, 'running', $6, now())
		ON CONFLICT (workflow_run_id, node_key) DO UPDATE SET
			status = 'running',
			input = EXCLUDED.input,
			retry_count = workflow_node_runs.retry_count + 1,
			error_code = NULL,
			error_message = NULL,
			started_at = now(),
			completed_at = NULL
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.NodeKey, input.NodeType, nodeInput).Scan(&nodeRunID); err != nil {
		return "", err
	}
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.node.started", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
		"workflowRunId": input.WorkflowRunID,
		"nodeKey":       input.NodeKey,
	})); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return nodeRunID, nil
}

func CompleteNodeRun(ctx context.Context, db txBeginner, nodeRunID string, output json.RawMessage) error {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	runCtx, err := lockNodeRunContext(ctx, tx, nodeRunID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, nodeRunID, output); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.completed", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
		"workflowRunId": runCtx.WorkflowRunID,
		"nodeKey":       runCtx.NodeKey,
		"output":        json.RawMessage(output),
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ProgressNodeRun(ctx context.Context, db txBeginner, nodeRunID string, output json.RawMessage) error {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	runCtx, err := lockNodeRunContext(ctx, tx, nodeRunID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'running', output = $2
		WHERE id = $1
	`, nodeRunID, output); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.progress", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
		"workflowRunId": runCtx.WorkflowRunID,
		"nodeKey":       runCtx.NodeKey,
		"output":        json.RawMessage(output),
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func FailNodeRun(ctx context.Context, db txBeginner, nodeRunID, code, message string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	runCtx, err := lockNodeRunContext(ctx, tx, nodeRunID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'failed', error_code = $2, error_message = $3, completed_at = now()
		WHERE id = $1
	`, nodeRunID, code, message); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.failed", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
		"workflowRunId": runCtx.WorkflowRunID,
		"nodeKey":       runCtx.NodeKey,
		"code":          code,
		"message":       message,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertEvent(ctx context.Context, tx pgx.Tx, organizationID, projectID, eventType, aggregateType, aggregateID string, payload json.RawMessage) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, organizationID, projectID, eventType, aggregateType, aggregateID, payload)
	return err
}

func lockNodeRunContext(ctx context.Context, tx pgx.Tx, nodeRunID string) (nodeRunContext, error) {
	var runCtx nodeRunContext
	err := tx.QueryRow(ctx, `
		SELECT organization_id, project_id, workflow_run_id, node_key
		FROM workflow_node_runs
		WHERE id = $1
		FOR UPDATE
	`, nodeRunID).Scan(&runCtx.OrganizationID, &runCtx.ProjectID, &runCtx.WorkflowRunID, &runCtx.NodeKey)
	return runCtx, err
}

type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}
