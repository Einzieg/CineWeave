package projectcontrol

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBWorkflowTracker struct {
	db *pgxpool.Pool
}

func NewDBWorkflowTracker(db *pgxpool.Pool) *DBWorkflowTracker {
	return &DBWorkflowTracker{db: db}
}

func (t *DBWorkflowTracker) Inspect(ctx context.Context, command Command, links []WorkflowLink) ([]WorkflowExecutionState, error) {
	if t == nil || t.db == nil {
		return nil, fmt.Errorf("project control workflow tracker is unavailable")
	}
	if len(links) == 0 {
		return []WorkflowExecutionState{}, nil
	}
	runIDs := make([]string, 0, len(links))
	temporalIDs := make([]string, 0, len(links))
	for _, link := range links {
		if link.WorkflowRunID != "" {
			if _, err := uuid.Parse(link.WorkflowRunID); err != nil {
				return nil, fmt.Errorf("workflow run ID %q is invalid: %w", link.WorkflowRunID, err)
			}
			runIDs = append(runIDs, link.WorkflowRunID)
		}
		temporalIDs = append(temporalIDs, link.TemporalWorkflowID)
	}
	rows, err := t.db.Query(ctx, `
		SELECT w.id::text, w.temporal_workflow_id, w.status,
		       COALESCE(w.error_code, ''), COALESCE(w.error_message, ''), w.output,
		       (
			   SELECT count(*) FROM workflow_node_runs node
			   WHERE node.workflow_run_id = w.id AND node.status IN ('pending', 'queued', 'running', 'waiting_review')
		       ),
		       (
			   SELECT count(*) FROM provider_async_tasks task
			   WHERE task.workflow_run_id = w.id AND task.status IN ('queued', 'running', 'cancelling')
		       ),
		       (
			   SELECT count(*) FROM (
			       SELECT checkpoint.id FROM episode_video_production_checkpoints checkpoint
			       WHERE checkpoint.workflow_run_id = w.id AND checkpoint.status IN ('queued', 'running', 'cancelling')
			       UNION ALL
			       SELECT batch.id FROM episode_video_production_batches batch
			       WHERE batch.workflow_run_id = w.id AND batch.status IN ('queued', 'running', 'cancelling')
			       UNION ALL
			       SELECT batch.id FROM derived_asset_batches batch
			       WHERE batch.workflow_run_id = w.id AND batch.status IN ('prepared', 'queued', 'running')
			       UNION ALL
			       SELECT coordinator.id FROM commerce_script_unit_batch_coordinators coordinator
			       WHERE coordinator.workflow_run_id = w.id AND coordinator.status IN ('queued', 'running', 'cancelling')
			       UNION ALL
			       SELECT production.id FROM commerce_production_runs production
			       WHERE production.workflow_run_id = w.id AND production.status IN ('queued', 'running', 'cancelling')
			       UNION ALL
			       SELECT derivation.id FROM commerce_script_derivation_batches derivation
			       WHERE derivation.workflow_run_id = w.id AND derivation.status IN ('queued', 'running', 'cancelling')
			   ) active_checkpoints
		       )
		FROM workflow_runs w
		WHERE w.organization_id = $1
		  AND ($2 = '' OR w.project_id = $2::uuid)
		  AND (w.id = ANY($3::uuid[]) OR w.temporal_workflow_id = ANY($4::text[]))
	`, command.OrganizationID, command.ProjectID, runIDs, temporalIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type trackedRow struct {
		runID, temporalID, status, errorCode, errorMessage string
		output                                             []byte
		activeNodes, activeProviders, activeCheckpoints    int
	}
	byRunID := make(map[string]trackedRow, len(links))
	byTemporalID := make(map[string]trackedRow, len(links))
	for rows.Next() {
		var row trackedRow
		if err := rows.Scan(&row.runID, &row.temporalID, &row.status, &row.errorCode,
			&row.errorMessage, &row.output, &row.activeNodes, &row.activeProviders,
			&row.activeCheckpoints); err != nil {
			return nil, err
		}
		byRunID[row.runID] = row
		byTemporalID[row.temporalID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	states := make([]WorkflowExecutionState, 0, len(links))
	for _, link := range links {
		row, found := trackedRow{}, false
		if link.WorkflowRunID != "" {
			row, found = byRunID[link.WorkflowRunID]
		}
		if !found {
			row, found = byTemporalID[link.TemporalWorkflowID]
		}
		if !found {
			return nil, fmt.Errorf("workflow %s has not been persisted", link.TemporalWorkflowID)
		}
		if row.temporalID != link.TemporalWorkflowID {
			return nil, fmt.Errorf("workflow run %s belongs to temporal workflow %s, expected %s",
				row.runID, row.temporalID, link.TemporalWorkflowID)
		}
		state := WorkflowExecutionState{
			Link: link, Status: strings.TrimSpace(row.status),
			ActiveNodeRuns: row.activeNodes, ActiveProviderTasks: row.activeProviders,
			ActiveCheckpoints: row.activeCheckpoints, ErrorCode: row.errorCode,
			ErrorMessage: row.errorMessage, Output: cloneRawMessage(row.output),
		}
		state.Active = !terminalWorkflowStatus(state.Status) || state.ActiveNodeRuns > 0 ||
			state.ActiveProviderTasks > 0 || state.ActiveCheckpoints > 0
		states = append(states, state)
	}
	return states, nil
}
