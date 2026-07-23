package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Einzieg/cineweave/internal/commerce"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func enqueueCommerceScriptOrganizationTx(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.UnitGenerationIdentity,
	createdBy string,
) (string, error) {
	if err := ValidateCommerceUnitGenerationIdentity(identity); err != nil {
		return "", err
	}
	var existingID string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM workflow_runs
		WHERE organization_id = $1 AND project_id = $2
		  AND production_generation_id = $3
		  AND workflow_type = $4
		  AND input->'identity'->>'scriptUnitGenerationId' = $5
		  AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE
	`, identity.OrganizationID, identity.ProjectID, identity.ProjectGenerationID,
		commerceScriptOrganizationWorkflowType, identity.UnitGenerationID).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	runID := uuid.NewString()
	input := CommerceScriptOrganizationInput{
		Identity: identity, WorkflowRunID: runID, CreatedBy: createdBy, AttemptGeneration: 1,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	inputHash, err := commerceContractHash(input)
	if err != nil {
		return "", err
	}
	temporalWorkflowID := fmt.Sprintf("commerce-script-organization-%s-%s", identity.UnitGenerationID, runID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', $7, $8, $9, $10)
	`, runID, identity.OrganizationID, identity.ProjectID, temporalWorkflowID,
		commerceScriptOrganizationWorkflowType, raw, createdBy, identity.ProjectGenerationID,
		identity.VideoProductionBindingID, identity.VideoProductionBindingRevision); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id, organization_id, project_id, production_generation_id,
			workflow_type, workflow_handler, temporal_workflow_id, task_queue,
			input, input_hash, max_attempts
		)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9, 12)
	`, runID, identity.OrganizationID, identity.ProjectID, identity.ProjectGenerationID,
		commerceScriptOrganizationWorkflowType, temporalWorkflowID, ScriptTaskQueue, raw, inputHash); err != nil {
		return "", err
	}
	return runID, nil
}

// EnqueueCommerceScriptOrganizationTx creates or reuses the active organizer
// workflow for one immutable ScriptUnitGeneration.
func EnqueueCommerceScriptOrganizationTx(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.UnitGenerationIdentity,
	createdBy string,
) (string, error) {
	return enqueueCommerceScriptOrganizationTx(ctx, tx, identity, createdBy)
}

func enqueueCommerceStoryboardPlanningTx(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.UnitGenerationIdentity,
	createdBy string,
	rootWorkflowRunID string,
) (string, error) {
	if err := ValidateCommerceUnitGenerationIdentity(identity); err != nil {
		return "", err
	}
	var existingID string
	err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM workflow_runs
		WHERE organization_id = $1 AND project_id = $2
		  AND production_generation_id = $3
		  AND workflow_type = $4
		  AND input->'identity'->>'scriptUnitGenerationId' = $5
		  AND (
		    status IN ('queued', 'running', 'waiting_review')
		    OR ($6 <> '' AND status = 'succeeded' AND root_workflow_run_id = $6::uuid)
		  )
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE
	`, identity.OrganizationID, identity.ProjectID, identity.ProjectGenerationID,
		commerceStoryboardWorkflowType, identity.UnitGenerationID, rootWorkflowRunID).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	runID := uuid.NewString()
	input := CommerceStoryboardPlanningInput{
		Identity: identity, WorkflowRunID: runID, CreatedBy: createdBy, AttemptGeneration: 1,
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	inputHash, err := commerceContractHash(input)
	if err != nil {
		return "", err
	}
	temporalWorkflowID := fmt.Sprintf("commerce-storyboard-%s-%s", identity.UnitGenerationID, runID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_runs(
			id, organization_id, project_id, temporal_workflow_id, workflow_type,
			status, input, output, created_by, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			root_workflow_run_id
		)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, '{}', $7, $8, $9, $10,
		        NULLIF($11, '')::uuid)
	`, runID, identity.OrganizationID, identity.ProjectID, temporalWorkflowID,
		commerceStoryboardWorkflowType, raw, createdBy, identity.ProjectGenerationID,
		identity.VideoProductionBindingID, identity.VideoProductionBindingRevision,
		rootWorkflowRunID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_start_outbox(
			workflow_run_id, organization_id, project_id, production_generation_id,
			workflow_type, workflow_handler, temporal_workflow_id, task_queue,
			input, input_hash, max_attempts
		)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9, 12)
	`, runID, identity.OrganizationID, identity.ProjectID, identity.ProjectGenerationID,
		commerceStoryboardWorkflowType, temporalWorkflowID, ScriptTaskQueue, raw, inputHash); err != nil {
		return "", err
	}
	return runID, nil
}

// EnqueueCommerceStoryboardPlanningTx creates a durable storyboard workflow
// start record. An empty rootWorkflowRunID starts an explicit plan revision;
// setup callers pass their root run to make replay idempotent.
func EnqueueCommerceStoryboardPlanningTx(
	ctx context.Context,
	tx pgx.Tx,
	identity commerce.UnitGenerationIdentity,
	createdBy string,
	rootWorkflowRunID string,
) (string, error) {
	return enqueueCommerceStoryboardPlanningTx(ctx, tx, identity, createdBy, rootWorkflowRunID)
}
