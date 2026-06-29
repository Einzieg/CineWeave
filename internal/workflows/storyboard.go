package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ScriptTaskQueue = "cineweave-script"

type TextToStoryboardInput struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`
	Prompt         string `json:"prompt"`
	CreatedBy      string `json:"createdBy"`
}

type TextToStoryboardOutput struct {
	ArtifactID string `json:"artifactId"`
	StorageKey string `json:"storageKey"`
}

type Activities struct {
	db      *pgxpool.Pool
	storage *storage.Client
}

func NewActivities(db *pgxpool.Pool, storageClient *storage.Client) Activities {
	return Activities{db: db, storage: storageClient}
}

func TextToStoryboardWorkflow(ctx workflow.Context, input TextToStoryboardInput) (TextToStoryboardOutput, error) {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	var output TextToStoryboardOutput
	if err := workflow.ExecuteActivity(ctx, "GenerateStoryboard", input).Get(ctx, &output); err != nil {
		return TextToStoryboardOutput{}, err
	}
	return output, nil
}

func (a Activities) GenerateStoryboard(ctx context.Context, input TextToStoryboardInput) (TextToStoryboardOutput, error) {
	if input.OrganizationID == "" || input.ProjectID == "" || input.WorkflowRunID == "" {
		return TextToStoryboardOutput{}, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	nodeRunID, err := a.markStarted(ctx, input)
	if err != nil {
		return TextToStoryboardOutput{}, err
	}

	storyboard := map[string]any{
		"kind":          "TextToStoryboard",
		"workflowRunId": input.WorkflowRunID,
		"prompt":        input.Prompt,
		"shots": []map[string]any{
			{
				"shotIndex": 1,
				"duration":  4,
				"action":    "Establish the scene from the prompt.",
				"dialogue":  "",
			},
			{
				"shotIndex": 2,
				"duration":  5,
				"action":    "Show the main subject taking action.",
				"dialogue":  "",
			},
			{
				"shotIndex": 3,
				"duration":  4,
				"action":    "End with a clear visual beat.",
				"dialogue":  "",
			},
		},
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	storageKey := fmt.Sprintf("artifacts/%s/%s/%s/storyboard.json", input.OrganizationID, input.ProjectID, input.WorkflowRunID)
	put, err := a.storage.PutJSON(ctx, storageKey, storyboard)
	if err != nil {
		_ = a.markFailed(ctx, input, nodeRunID, err)
		return TextToStoryboardOutput{}, err
	}
	output := TextToStoryboardOutput{StorageKey: put.StorageKey}
	if err := a.markSucceeded(ctx, input, nodeRunID, put, storyboard, &output); err != nil {
		return TextToStoryboardOutput{}, err
	}
	return output, nil
}

func (a Activities) markStarted(ctx context.Context, input TextToStoryboardInput) (string, error) {
	tx, err := a.db.Begin(ctx)
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
		VALUES ($1, $2, $3, 'text_to_storyboard', 'provider_activity', 'running', $4, now())
		ON CONFLICT (workflow_run_id, node_key) DO UPDATE SET
			status = 'running',
			input = EXCLUDED.input,
			started_at = COALESCE(workflow_node_runs.started_at, now())
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, mustJSON(map[string]any{"prompt": input.Prompt})).Scan(&nodeRunID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($1, $2, 'workflow.node.started', 'workflow_node_run', $3, $4)
	`, input.OrganizationID, input.ProjectID, nodeRunID, mustJSON(map[string]any{"workflowRunId": input.WorkflowRunID, "nodeKey": "text_to_storyboard"})); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return nodeRunID, nil
}

func (a Activities) markSucceeded(ctx context.Context, input TextToStoryboardInput, nodeRunID string, put storage.PutResult, storyboard map[string]any, output *TextToStoryboardOutput) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	storyboardJSON := mustJSON(storyboard)
	var artifactID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, node_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'storyboard', $5, 'application/json', $6, $7, $8)
		RETURNING id
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, nodeRunID, put.StorageKey, put.ContentHash, mustJSON(map[string]any{"byteSize": put.ByteSize}), input.CreatedBy).Scan(&artifactID); err != nil {
		return err
	}
	output.ArtifactID = artifactID
	outputJSON := mustJSON(map[string]any{"artifactId": artifactID, "storageKey": put.StorageKey})
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, nodeRunID, storyboardJSON); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET status = 'succeeded', output = $2, completed_at = now()
		WHERE id = $1
	`, input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES
			($1, $2, 'artifact.created', 'artifact', $3, $4),
			($1, $2, 'workflow.run.completed', 'workflow_run', $5, $6)
	`, input.OrganizationID, input.ProjectID, artifactID, outputJSON, input.WorkflowRunID, outputJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a Activities) markFailed(ctx context.Context, input TextToStoryboardInput, nodeRunID string, cause error) error {
	errorMessage := cause.Error()
	_, err := a.db.Exec(ctx, `
		UPDATE workflow_node_runs
		SET status = 'failed', error_code = 'ACTIVITY_FAILED', error_message = $2, completed_at = now()
		WHERE id = $1;
		UPDATE workflow_runs
		SET status = 'failed', error_code = 'ACTIVITY_FAILED', error_message = $2, completed_at = now()
		WHERE id = $3;
		INSERT INTO event_outbox(organization_id, project_id, event_type, aggregate_type, aggregate_id, payload)
		VALUES ($4, $5, 'workflow.run.failed', 'workflow_run', $3, $6);
	`, nodeRunID, errorMessage, input.WorkflowRunID, input.OrganizationID, input.ProjectID, mustJSON(map[string]any{"message": errorMessage}))
	return err
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
