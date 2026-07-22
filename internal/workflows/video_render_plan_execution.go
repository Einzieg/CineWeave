package workflows

import (
	"context"
	"errors"
	"strings"

	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

// recoverableWorkflowShotVideoExecutionPlan finds a plan created by the same
// workflow attempt before the activity result was durably acknowledged. Only a
// pristine plan is recoverable; provider execution state is never replayed.
func (a Activities) recoverableWorkflowShotVideoExecutionPlan(ctx context.Context, input EnsurePreparedShotVideoPlanInput) (PlanShotVideoOutput, bool, error) {
	var executionPlanID, videoPromptPlanID, promptContextPlanID, referencePackID string
	err := a.db.QueryRow(ctx, `
		SELECT plan.id::text, plan.video_prompt_plan_id::text,
		       plan.prompt_context_plan_id::text, plan.reference_pack_id::text
		FROM video_render_plans plan
		JOIN workflow_runs run
		  ON run.id = plan.workflow_run_id
		 AND run.organization_id = plan.organization_id
		 AND run.project_id = plan.project_id
		 AND run.production_generation_id = plan.production_generation_id
		 AND run.video_production_binding_id = plan.video_production_binding_id
		 AND run.video_production_binding_revision = plan.video_production_binding_revision
		JOIN workflow_node_runs node
		  ON node.id = plan.node_run_id
		 AND node.workflow_run_id = run.id
		 AND node.attempt_generation = run.attempt_generation
		WHERE plan.organization_id = $1
		  AND plan.project_id = $2
		  AND plan.workflow_run_id = $3
		  AND plan.storyboard_shot_id = $4
		  AND plan.active = true
		  AND plan.status = 'planned'
		  AND NOT EXISTS (
		    SELECT 1
		    FROM video_render_segments segment
		    WHERE segment.video_render_plan_id = plan.id
		      AND (
		        segment.status <> 'planned'
		        OR segment.retry_generation > 0
		        OR segment.provider_async_task_id IS NOT NULL
		        OR segment.provider_call_id IS NOT NULL
		        OR segment.external_task_id IS NOT NULL
		        OR segment.artifact_id IS NOT NULL
		        OR segment.media_file_id IS NOT NULL
		        OR COALESCE(segment.storage_key, '') <> ''
		      )
		  )
		ORDER BY plan.created_at DESC
		LIMIT 1
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.ShotID).Scan(
		&executionPlanID, &videoPromptPlanID, &promptContextPlanID, &referencePackID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanShotVideoOutput{}, false, nil
	}
	if err != nil {
		return PlanShotVideoOutput{}, false, err
	}
	return PlanShotVideoOutput{GatewayVideoPlanResponse: provider.GatewayVideoPlanResponse{
		ExecutionPlanID: executionPlanID, VideoPromptPlanID: videoPromptPlanID,
		PromptContextPlanID: promptContextPlanID, ReferencePackID: referencePackID,
	}}, true, nil
}

// activeShotVideoExecutionPlanID returns the current immutable plan only as
// provenance for the next execution plan. Provider identity must be resolved
// again by Provider Gateway for every new execution workflow.
func (a Activities) activeShotVideoExecutionPlanID(ctx context.Context, input EnsurePreparedShotVideoPlanInput) (string, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" || strings.TrimSpace(input.ShotID) == "" {
		return "", errors.New("organizationId, projectId, workflowRunId, and shotId are required")
	}

	var planID string
	err := a.db.QueryRow(ctx, `
		SELECT plan.id::text
		FROM storyboard_shots shot
		JOIN video_render_plans plan ON plan.id = shot.active_video_render_plan_id
		JOIN workflow_runs run
		  ON run.id = $4
		 AND run.organization_id = shot.organization_id
		 AND run.project_id = shot.project_id
		 AND run.production_generation_id = shot.production_generation_id
		 AND run.video_production_binding_id = plan.video_production_binding_id
		 AND run.video_production_binding_revision = plan.video_production_binding_revision
		WHERE shot.id = $1
		  AND shot.project_id = $2
		  AND shot.organization_id = $3
		  AND shot.deleted_at IS NULL
		  AND plan.active = true
		  AND plan.production_generation_id = shot.production_generation_id
		  AND plan.status NOT IN ('stale', 'archived', 'cancelled', 'replan_required')
	`, input.ShotID, input.ProjectID, input.OrganizationID, input.WorkflowRunID).Scan(&planID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return planID, err
}
