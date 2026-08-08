package projectcontrol

import (
	"context"
	"fmt"
)

type RuntimeCommandCount struct {
	Status     string `json:"status"`
	Controller string `json:"controller"`
	Count      int64  `json:"count"`
}

type RuntimeSnapshot struct {
	CommandCounts                  []RuntimeCommandCount `json:"commandCounts"`
	ActiveCommands                 int64                 `json:"activeCommands"`
	WaitingCommands                int64                 `json:"waitingCommands"`
	ExpiredLeases                  int64                 `json:"expiredLeases"`
	OverdueReconciliations         int64                 `json:"overdueReconciliations"`
	OldestReconcileLagSeconds      float64               `json:"oldestReconcileLagSeconds"`
	UnlinkedDeterministicWorkflows int64                 `json:"unlinkedDeterministicWorkflows"`
}

func (r *Repository) RuntimeSnapshot(ctx context.Context) (RuntimeSnapshot, error) {
	if r == nil || r.db == nil {
		return RuntimeSnapshot{}, fmt.Errorf("project control repository is unavailable")
	}
	result := RuntimeSnapshot{CommandCounts: []RuntimeCommandCount{}}
	rows, err := r.db.Query(ctx, `
		SELECT status, controller_type, count(*)
		FROM project_control_commands
		GROUP BY status, controller_type
		ORDER BY status, controller_type
	`)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	for rows.Next() {
		var count RuntimeCommandCount
		if err := rows.Scan(&count.Status, &count.Controller, &count.Count); err != nil {
			rows.Close()
			return RuntimeSnapshot{}, err
		}
		result.CommandCounts = append(result.CommandCounts, count)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RuntimeSnapshot{}, err
	}
	rows.Close()

	err = r.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (
				WHERE status IN ('queued', 'running', 'waiting_workflow', 'waiting_input')
			),
			count(*) FILTER (
				WHERE status IN ('waiting_workflow', 'waiting_input')
			),
			count(*) FILTER (
				WHERE status IN ('queued', 'running', 'waiting_workflow', 'waiting_input')
				  AND lease_expires_at IS NOT NULL
				  AND lease_expires_at <= now()
			),
			count(*) FILTER (
				WHERE (
					status = 'waiting_workflow'
					OR (cancellation_requested_at IS NOT NULL
						AND status IN ('running', 'waiting_workflow', 'waiting_input'))
				)
				  AND COALESCE(next_reconcile_at, updated_at) <= now()
			),
			COALESCE(EXTRACT(EPOCH FROM (
				now() - min(COALESCE(next_reconcile_at, updated_at)) FILTER (
					WHERE (
						status = 'waiting_workflow'
						OR (cancellation_requested_at IS NOT NULL
							AND status IN ('running', 'waiting_workflow', 'waiting_input'))
					)
					  AND COALESCE(next_reconcile_at, updated_at) <= now()
				)
			)), 0),
			(
				SELECT count(*)
				FROM workflow_runs AS workflow
				WHERE COALESCE(workflow.input->'input'->>'projectControlCommandId', '') <> ''
				  AND NOT EXISTS (
					SELECT 1
					FROM project_control_command_workflows AS link
					WHERE link.workflow_run_id = workflow.id
				  )
			)
		FROM project_control_commands
	`).Scan(
		&result.ActiveCommands,
		&result.WaitingCommands,
		&result.ExpiredLeases,
		&result.OverdueReconciliations,
		&result.OldestReconcileLagSeconds,
		&result.UnlinkedDeterministicWorkflows,
	)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	return result, nil
}
