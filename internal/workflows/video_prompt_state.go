package workflows

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// restoreApprovedVideoPromptStateTx keeps a failed or cancelled regeneration
// attempt from invalidating the last approved prompt contract for the shot.
func restoreApprovedVideoPromptStateTx(ctx context.Context, tx pgx.Tx, shotID, workflowRunID string) (bool, error) {
	shotID = strings.TrimSpace(shotID)
	workflowRunID = strings.TrimSpace(workflowRunID)
	if shotID == "" || workflowRunID == "" {
		return false, nil
	}

	var restoredID string
	err := tx.QueryRow(ctx, `
		WITH approved AS (
			SELECT plan.storyboard_shot_id, plan.rendered_prompt,
			       plan.workflow_run_id, plan.approved_at
			FROM video_prompt_plans plan
			JOIN storyboard_shots shot
			  ON shot.id = plan.storyboard_shot_id
			 AND shot.production_generation_id = plan.production_generation_id
			WHERE plan.storyboard_shot_id = $1
			  AND plan.status = 'approved'
			  AND plan.stale_at IS NULL
			  AND plan.archived_at IS NULL
			ORDER BY plan.revision DESC
			LIMIT 1
		)
		UPDATE storyboard_shots shot
		SET video_prompt = approved.rendered_prompt,
		    video_prompt_status = 'succeeded',
		    video_prompt_error_code = NULL,
		    video_prompt_error_message = NULL,
		    video_prompt_workflow_run_id = approved.workflow_run_id,
		    video_prompt_updated_at = COALESCE(approved.approved_at, now()),
		    updated_at = now()
		FROM approved
		WHERE shot.id = approved.storyboard_shot_id
		  AND shot.id = $1
		  AND shot.deleted_at IS NULL
		  AND shot.video_prompt_workflow_run_id = $2
		  AND shot.video_prompt_status = 'failed'
		RETURNING shot.id::text
	`, shotID, workflowRunID).Scan(&restoredID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return restoredID != "", nil
}

func restoreApprovedVideoPromptStatesForWorkflowTx(ctx context.Context, tx pgx.Tx, projectID, workflowRunID string) error {
	projectID = strings.TrimSpace(projectID)
	workflowRunID = strings.TrimSpace(workflowRunID)
	if projectID == "" || workflowRunID == "" {
		return nil
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM storyboard_shots
		WHERE project_id = $1
		  AND video_prompt_workflow_run_id = $2
		  AND video_prompt_status = 'failed'
		  AND deleted_at IS NULL
	`, projectID, workflowRunID)
	if err != nil {
		return err
	}
	shotIDs := make([]string, 0)
	for rows.Next() {
		var shotID string
		if err := rows.Scan(&shotID); err != nil {
			rows.Close()
			return err
		}
		shotIDs = append(shotIDs, shotID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, shotID := range shotIDs {
		if _, err := restoreApprovedVideoPromptStateTx(ctx, tx, shotID, workflowRunID); err != nil {
			return err
		}
	}
	return nil
}
