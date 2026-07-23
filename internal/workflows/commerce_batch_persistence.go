package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (r *CommerceGenerationRuntime) StartCommerceScriptUnitBatchCoordinator(ctx context.Context, input CommerceScriptUnitBatchCoordinatorInput) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := validateCommerceBatchCoordinatorWorkflowRunTx(ctx, tx, input); err != nil {
		return err
	}
	var status, organizationID, projectID, generationID, workflowRunID, targetStage string
	if err := tx.QueryRow(ctx, `
		SELECT status, organization_id::text, project_id::text,
		       project_production_generation_id::text, workflow_run_id::text, target_stage
		FROM commerce_script_unit_batch_coordinators
		WHERE id = $1
		FOR UPDATE
	`, input.CoordinatorID).Scan(&status, &organizationID, &projectID, &generationID, &workflowRunID, &targetStage); err != nil {
		return err
	}
	if organizationID != input.OrganizationID || projectID != input.ProjectID || generationID != input.ProjectGenerationID ||
		workflowRunID != input.WorkflowRunID || targetStage != input.TargetStage {
		return generationMismatch("跨脚本批量任务身份不一致", nil)
	}
	if status != "queued" && status != "running" {
		return generationMismatch("跨脚本批量任务已不再可写", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_coordinators
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    error_code = NULL, error_message = NULL, revision = revision + 1
		WHERE id = $1 AND status = 'queued'
	`, input.CoordinatorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateCommerceBatchCoordinatorWorkflowRunTx(ctx context.Context, tx pgx.Tx, input CommerceScriptUnitBatchCoordinatorInput) error {
	var organizationID, projectID, generationID, workflowType, status, createdBy, outboxHash string
	var runInput, outboxInput json.RawMessage
	if err := tx.QueryRow(ctx, `
		SELECT run.organization_id::text, run.project_id::text, run.production_generation_id::text,
		       run.workflow_type, run.status, run.created_by::text, run.input,
		       outbox.input, outbox.input_hash
		FROM workflow_runs run
		JOIN workflow_start_outbox outbox ON outbox.workflow_run_id = run.id
		WHERE run.id = $1
		FOR UPDATE OF run
	`, input.WorkflowRunID).Scan(
		&organizationID, &projectID, &generationID, &workflowType, &status, &createdBy,
		&runInput, &outboxInput, &outboxHash,
	); err != nil {
		return err
	}
	if organizationID != input.OrganizationID || projectID != input.ProjectID || generationID != input.ProjectGenerationID ||
		workflowType != "commerce_script_unit_batch_coordinator" || createdBy != input.RequestedBy ||
		(status != "queued" && status != "running") {
		return generationMismatch("跨脚本批量 Workflow Run 身份不一致", nil)
	}
	if err := assertWorkflowInputMatches(runInput, input, input.WorkflowRunID); err != nil {
		return err
	}
	runHash, err := canonicalCommerceWorkflowInputHash(runInput)
	if err != nil {
		return err
	}
	startHash, err := canonicalCommerceWorkflowInputHash(outboxInput)
	if err != nil {
		return err
	}
	if runHash != outboxHash || startHash != outboxHash || runHash != startHash {
		return generationMismatch("跨脚本批量 Workflow Run 输入 hash 与启动快照不一致", nil)
	}
	return nil
}

func (r *CommerceGenerationRuntime) StartCommerceScriptUnitBatchItem(ctx context.Context, input CommerceScriptUnitBatchItemStart) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var itemStatus, coordinatorWorkflowRunID, childWorkflowRunID, organizationID, projectID, generationID string
	if err := tx.QueryRow(ctx, `
		SELECT item.status, coordinator.workflow_run_id::text, item.child_workflow_run_id::text,
		       coordinator.organization_id::text, coordinator.project_id::text,
		       coordinator.project_production_generation_id::text
		FROM commerce_script_unit_batch_items item
		JOIN commerce_script_unit_batch_coordinators coordinator ON coordinator.id = item.coordinator_id
		WHERE item.id = $1 AND coordinator.id = $2
		FOR UPDATE OF item, coordinator
	`, input.CoordinatorItemID, input.CoordinatorID).Scan(
		&itemStatus, &coordinatorWorkflowRunID, &childWorkflowRunID,
		&organizationID, &projectID, &generationID,
	); err != nil {
		return err
	}
	if coordinatorWorkflowRunID != input.WorkflowRunID || childWorkflowRunID != input.ChildWorkflowRunID ||
		organizationID != input.OrganizationID || projectID != input.ProjectID || generationID != input.ProjectGenerationID {
		return generationMismatch("跨脚本批量子任务身份不一致", nil)
	}
	if itemStatus != "queued" && itemStatus != "running" {
		return generationMismatch("跨脚本批量子任务已不再可启动", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_items
		SET status = 'running', error_code = NULL, error_message = NULL
		WHERE id = $1 AND status = 'queued'
	`, input.CoordinatorItemID); err != nil {
		return err
	}
	var workflowType string
	var revision int64
	err = tx.QueryRow(ctx, `
		UPDATE workflow_runs
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status = 'queued'
		RETURNING workflow_type, revision
	`, input.ChildWorkflowRunID).Scan(&workflowType, &revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := insertEvent(ctx, tx, organizationID, projectID, "workflow.run.started", "workflow_run", input.ChildWorkflowRunID, mustJSON(map[string]any{
			"workflowRunId": input.ChildWorkflowRunID,
			"workflowType":  workflowType,
			"status":        "running",
			"revision":      revision,
		})); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *CommerceGenerationRuntime) CompleteCommerceScriptUnitBatchItem(ctx context.Context, input CommerceScriptUnitBatchItemCompletion) error {
	if input.Status != "succeeded" && input.Status != "failed" && input.Status != "cancelled" {
		return fmt.Errorf("unsupported commerce batch item status %q", input.Status)
	}
	if len(input.Output) == 0 {
		input.Output = json.RawMessage(`{}`)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, coordinatorWorkflowRunID, childWorkflowRunID, childRunID, organizationID, projectID string
	if err := tx.QueryRow(ctx, `
		SELECT item.status, coordinator.workflow_run_id::text, item.child_workflow_run_id::text,
		       COALESCE(item.child_run_id::text, ''), item.organization_id::text, item.project_id::text
		FROM commerce_script_unit_batch_items item
		JOIN commerce_script_unit_batch_coordinators coordinator ON coordinator.id = item.coordinator_id
		WHERE item.id = $1 AND coordinator.id = $2
		FOR UPDATE OF item, coordinator
	`, input.CoordinatorItemID, input.CoordinatorID).Scan(
		&status, &coordinatorWorkflowRunID, &childWorkflowRunID, &childRunID, &organizationID, &projectID,
	); err != nil {
		return err
	}
	if coordinatorWorkflowRunID != input.WorkflowRunID || childWorkflowRunID != input.ChildWorkflowRunID {
		return generationMismatch("跨脚本批量子任务完成身份不一致", nil)
	}
	if status == "succeeded" || status == "failed" || status == "cancelled" || status == "skipped" {
		if status != input.Status {
			return generationMismatch("跨脚本批量子任务终态冲突", nil)
		}
		return tx.Commit(ctx)
	}
	if status != "running" {
		return generationMismatch("跨脚本批量子任务尚未启动", nil)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_items
		SET status = $2, error_code = NULLIF($3, ''), error_message = NULLIF($4, ''), completed_at = now()
		WHERE id = $1 AND status = 'running'
	`, input.CoordinatorItemID, input.Status, input.ErrorCode, input.ErrorMessage); err != nil {
		return err
	}
	switch input.Status {
	case "succeeded":
		if _, _, err := transitionWorkflowRunTx(ctx, tx, input.ChildWorkflowRunID, "succeeded", "", "", input.Output); err != nil {
			return err
		}
	case "failed":
		if _, _, err := transitionWorkflowRunTx(ctx, tx, input.ChildWorkflowRunID, "failed", defaultWorkflowString(input.ErrorCode, "COMMERCE_BATCH_CHILD_FAILED"), input.ErrorMessage, input.Output); err != nil {
			return err
		}
		if childRunID != "" {
			if _, err := r.catalog.FailActiveProductionRunItems(ctx, tx, organizationID, projectID, childRunID,
				defaultWorkflowString(input.ErrorCode, "COMMERCE_BATCH_CHILD_FAILED"), input.ErrorMessage, true); err != nil {
				return err
			}
		}
	case "cancelled":
		if _, _, err := cancelWorkflowRunTx(ctx, tx, input.ChildWorkflowRunID, input.Output, input.ErrorMessage, defaultWorkflowString(input.ErrorCode, "USER_CANCELLED")); err != nil {
			return err
		}
		if childRunID != "" {
			if _, err := r.catalog.CancelProductionRun(ctx, tx, organizationID, projectID, childRunID, input.ErrorMessage); err != nil {
				return err
			}
		}
	}
	if err := refreshCommerceBatchProgressTx(ctx, tx, input.CoordinatorID, input.WorkflowRunID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *CommerceGenerationRuntime) FinalizeCommerceScriptUnitBatchCoordinator(ctx context.Context, input CommerceScriptUnitBatchCoordinatorInput) (CommerceScriptUnitBatchCoordinatorOutput, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return CommerceScriptUnitBatchCoordinatorOutput{}, err
	}
	defer tx.Rollback(ctx)
	output, err := commerceBatchCoordinatorOutputTx(ctx, tx, input.CoordinatorID, input.WorkflowRunID, input.TargetStage)
	if err != nil {
		return output, err
	}
	if output.Succeeded+output.Failed+output.Cancelled != output.Total {
		return output, generationMismatch("跨脚本批量任务仍有未完成单元", nil)
	}
	switch {
	case output.Total > 0 && output.Succeeded == output.Total:
		output.Status = "succeeded"
	case output.Succeeded > 0:
		output.Status = "partially_succeeded"
	default:
		output.Status = "failed"
	}
	raw := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_coordinators
		SET status = $2, completed_at = COALESCE(completed_at, now()),
		    error_code = CASE WHEN $2 = 'succeeded' THEN NULL ELSE 'COMMERCE_BATCH_INCOMPLETE' END,
		    error_message = CASE WHEN $2 = 'succeeded' THEN NULL ELSE '部分脚本单元未完成' END,
		    revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running')
	`, input.CoordinatorID, output.Status); err != nil {
		return output, err
	}
	workflowStatus := output.Status
	if workflowStatus == "partially_succeeded" {
		workflowStatus = "partial_succeeded"
	}
	code, message := "", ""
	if workflowStatus == "failed" {
		code, message = "COMMERCE_BATCH_ALL_FAILED", "所有脚本单元任务均未完成"
	}
	if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, workflowStatus, code, message, raw); err != nil {
		return output, err
	}
	if err := tx.Commit(ctx); err != nil {
		return output, err
	}
	return output, nil
}

func (r *CommerceGenerationRuntime) AbortCommerceScriptUnitBatchCoordinator(ctx context.Context, input CommerceScriptUnitBatchAbort) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT item.id::text, item.child_workflow_run_id::text, COALESCE(item.child_run_id::text, ''),
		       item.organization_id::text, item.project_id::text
		FROM commerce_script_unit_batch_items item
		JOIN commerce_script_unit_batch_coordinators coordinator ON coordinator.id = item.coordinator_id
		WHERE coordinator.id = $1 AND coordinator.workflow_run_id = $2
		  AND item.status IN ('queued', 'running')
		FOR UPDATE OF item
	`, input.CoordinatorID, input.WorkflowRunID)
	if err != nil {
		return err
	}
	type activeChild struct{ itemID, workflowRunID, productionRunID, organizationID, projectID string }
	children := make([]activeChild, 0)
	for rows.Next() {
		var child activeChild
		if err := rows.Scan(&child.itemID, &child.workflowRunID, &child.productionRunID, &child.organizationID, &child.projectID); err != nil {
			rows.Close()
			return err
		}
		children = append(children, child)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	itemStatus := "failed"
	if input.Cancelled {
		itemStatus = "cancelled"
	}
	for _, child := range children {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_batch_items
			SET status = $2, error_code = NULLIF($3, ''), error_message = NULLIF($4, ''), completed_at = now()
			WHERE id = $1 AND status IN ('queued', 'running')
		`, child.itemID, itemStatus, input.ErrorCode, input.ErrorMessage); err != nil {
			return err
		}
		if input.Cancelled {
			if _, _, err := cancelWorkflowRunTx(ctx, tx, child.workflowRunID, json.RawMessage(`{}`), input.ErrorMessage, defaultWorkflowString(input.ErrorCode, "USER_CANCELLED")); err != nil {
				return err
			}
			if child.productionRunID != "" {
				if _, err := r.catalog.CancelProductionRun(ctx, tx, child.organizationID, child.projectID, child.productionRunID, input.ErrorMessage); err != nil {
					return err
				}
			}
		} else if _, _, err := transitionWorkflowRunTx(ctx, tx, child.workflowRunID, "failed", defaultWorkflowString(input.ErrorCode, "COMMERCE_BATCH_COORDINATOR_FAILED"), input.ErrorMessage, json.RawMessage(`{}`)); err != nil {
			return err
		} else if child.productionRunID != "" {
			if _, err := r.catalog.FailActiveProductionRunItems(ctx, tx, child.organizationID, child.projectID, child.productionRunID,
				defaultWorkflowString(input.ErrorCode, "COMMERCE_BATCH_COORDINATOR_FAILED"), input.ErrorMessage, true); err != nil {
				return err
			}
		}
	}
	output, err := commerceBatchCoordinatorOutputTx(ctx, tx, input.CoordinatorID, input.WorkflowRunID, "")
	if err != nil {
		return err
	}
	output.Status = "failed"
	if input.Cancelled {
		output.Status = "cancelled"
	} else if output.Succeeded > 0 {
		output.Status = "partially_succeeded"
	}
	raw := mustJSON(output)
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_coordinators
		SET status = $2, completed_at = COALESCE(completed_at, now()),
		    cancelled_at = CASE WHEN $2 = 'cancelled' THEN COALESCE(cancelled_at, now()) ELSE NULL END,
		    error_code = NULLIF($3, ''), error_message = NULLIF($4, ''), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'cancelling')
	`, input.CoordinatorID, output.Status, input.ErrorCode, input.ErrorMessage); err != nil {
		return err
	}
	if input.Cancelled {
		if _, _, err := cancelWorkflowRunTx(ctx, tx, input.WorkflowRunID, raw, input.ErrorMessage, defaultWorkflowString(input.ErrorCode, "USER_CANCELLED")); err != nil {
			return err
		}
	} else {
		workflowStatus := "failed"
		if output.Status == "partially_succeeded" {
			workflowStatus = "partial_succeeded"
		}
		if _, _, err := transitionWorkflowRunTx(ctx, tx, input.WorkflowRunID, workflowStatus, defaultWorkflowString(input.ErrorCode, "COMMERCE_BATCH_COORDINATOR_FAILED"), input.ErrorMessage, raw); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func refreshCommerceBatchProgressTx(ctx context.Context, tx pgx.Tx, coordinatorID, workflowRunID string) error {
	var total, succeeded, failed, cancelled int
	if err := tx.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status = 'succeeded'),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE status IN ('cancelled', 'skipped'))
		FROM commerce_script_unit_batch_items
		WHERE coordinator_id = $1
	`, coordinatorID).Scan(&total, &succeeded, &failed, &cancelled); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_batch_coordinators
		SET total_items = $2, completed_items = $3, failed_items = $4,
		    cancelled_items = $5, revision = revision + 1
		WHERE id = $1
	`, coordinatorID, total, succeeded, failed, cancelled); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE workflow_runs
		SET total_items = $2, completed_items = $3, failed_items = $4 + $5,
		    updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('queued', 'running', 'waiting_review')
	`, workflowRunID, total, succeeded, failed, cancelled)
	return err
}

func commerceBatchCoordinatorOutputTx(ctx context.Context, tx pgx.Tx, coordinatorID, workflowRunID, expectedTargetStage string) (CommerceScriptUnitBatchCoordinatorOutput, error) {
	var output CommerceScriptUnitBatchCoordinatorOutput
	if err := tx.QueryRow(ctx, `
		SELECT id::text, workflow_run_id::text, target_stage
		FROM commerce_script_unit_batch_coordinators
		WHERE id = $1 AND workflow_run_id = $2
		FOR UPDATE
	`, coordinatorID, workflowRunID).Scan(&output.CoordinatorID, &output.WorkflowRunID, &output.TargetStage); err != nil {
		return output, err
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status = 'succeeded'),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE status IN ('cancelled', 'skipped'))
		FROM commerce_script_unit_batch_items
		WHERE coordinator_id = $1
	`, coordinatorID).Scan(
		&output.Total, &output.Succeeded, &output.Failed, &output.Cancelled,
	); err != nil {
		return output, err
	}
	if expectedTargetStage != "" && output.TargetStage != expectedTargetStage {
		return output, generationMismatch("跨脚本批量任务阶段不一致", nil)
	}
	return output, nil
}

func defaultWorkflowString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
