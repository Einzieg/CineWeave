package commerce

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const productionRunSelectSQL = `
	SELECT run.id::text, run.organization_id::text, run.project_id::text,
	       run.product_id::text, run.script_unit_id::text,
	       unit.revision, run.script_unit_generation_id::text,
	       unit_generation.unit_generation_no, unit_generation.unit_configuration_hash,
	       run.project_production_generation_id::text,
	       project_generation.binding_id::text,
	       video_binding.revision, video_binding.profile_snapshot_hash,
	       project_generation.commerce_workflow_binding_id::text,
	       commerce_binding.binding_revision, commerce_binding.configuration_hash,
	       run.workflow_run_id::text, run.run_type, run.status, run.payload_hash, run.input_snapshot, run.revision,
	       run.total_items, run.completed_items, run.failed_items, run.cancelled_items,
	       run.created_at, run.started_at, run.completed_at,
	       COALESCE(run.error_code, ''), COALESCE(run.error_message, '')
	FROM commerce_production_runs run
	JOIN commerce_script_units unit
	  ON unit.id = run.script_unit_id
	 AND unit.product_id = run.product_id
	 AND unit.organization_id = run.organization_id
	 AND unit.project_id = run.project_id
	JOIN commerce_script_unit_generations unit_generation
	  ON unit_generation.id = run.script_unit_generation_id
	 AND unit_generation.script_unit_id = run.script_unit_id
	 AND unit_generation.product_id = run.product_id
	 AND unit_generation.organization_id = run.organization_id
	 AND unit_generation.project_id = run.project_id
	JOIN project_video_production_generations project_generation
	  ON project_generation.id = run.project_production_generation_id
	 AND project_generation.project_id = run.project_id
	 AND project_generation.organization_id = run.organization_id
	JOIN project_video_production_bindings video_binding
	  ON video_binding.id = project_generation.binding_id
	 AND video_binding.project_id = project_generation.project_id
	JOIN project_commerce_workflow_bindings commerce_binding
	  ON commerce_binding.id = project_generation.commerce_workflow_binding_id
	 AND commerce_binding.project_id = project_generation.project_id
	 AND commerce_binding.organization_id = project_generation.organization_id`

const productionRunItemSelectSQL = `
	SELECT item.id::text, item.run_id::text,
	       item.organization_id::text, item.project_id::text,
	       run.product_id::text, item.script_unit_id::text, unit.revision,
	       item.script_unit_generation_id::text,
	       unit_generation.unit_generation_no, unit_generation.unit_configuration_hash,
	       run.project_production_generation_id::text,
	       project_generation.binding_id::text, video_binding.revision,
	       video_binding.profile_snapshot_hash,
	       project_generation.commerce_workflow_binding_id::text,
	       commerce_binding.binding_revision, commerce_binding.configuration_hash,
	       item.subject_type, item.subject_key, item.storyboard_shot_id::text,
	       item.input_hash, item.status, item.current_attempt,
	       item.output_snapshot, item.output_artifact_id::text,
	       item.output_media_file_id::text, item.output_storyboard_plan_id::text,
	       item.output_video_prompt_plan_id::text, item.output_video_render_plan_id::text,
	       item.output_final_video_version_id::text,
	       latest_attempt.provider_request_id::text, latest_attempt.provider_call_id::text,
	       latest_attempt.provider_async_task_id::text,
	       COALESCE(item.error_code, ''), COALESCE(item.error_message, ''), item.retryable,
	       item.started_at, item.completed_at
	FROM commerce_production_run_items item
	JOIN commerce_production_runs run
	  ON run.id = item.run_id
	 AND run.script_unit_id = item.script_unit_id
	 AND run.script_unit_generation_id = item.script_unit_generation_id
	 AND run.organization_id = item.organization_id
	 AND run.project_id = item.project_id
	JOIN commerce_script_units unit
	  ON unit.id = item.script_unit_id
	 AND unit.product_id = run.product_id
	 AND unit.organization_id = item.organization_id
	 AND unit.project_id = item.project_id
	JOIN commerce_script_unit_generations unit_generation
	  ON unit_generation.id = item.script_unit_generation_id
	 AND unit_generation.script_unit_id = item.script_unit_id
	 AND unit_generation.product_id = run.product_id
	 AND unit_generation.organization_id = item.organization_id
	 AND unit_generation.project_id = item.project_id
	JOIN project_video_production_generations project_generation
	  ON project_generation.id = run.project_production_generation_id
	 AND project_generation.project_id = run.project_id
	 AND project_generation.organization_id = run.organization_id
	JOIN project_video_production_bindings video_binding
	  ON video_binding.id = project_generation.binding_id
	 AND video_binding.project_id = project_generation.project_id
	JOIN project_commerce_workflow_bindings commerce_binding
	  ON commerce_binding.id = project_generation.commerce_workflow_binding_id
	 AND commerce_binding.project_id = project_generation.project_id
	 AND commerce_binding.organization_id = project_generation.organization_id
	LEFT JOIN commerce_production_run_item_attempts latest_attempt
	  ON latest_attempt.run_id = item.run_id
	 AND latest_attempt.item_id = item.id
	 AND latest_attempt.attempt_number = item.current_attempt`

func (r *Repository) FindProductionRunByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	idempotencyScope string,
	idempotencyKey string,
) (ProductionRun, bool, error) {
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1 || chr(31) || $2 || chr(31) || $3, 0))
	`, organizationID, idempotencyScope, idempotencyKey); err != nil {
		return ProductionRun{}, false, err
	}
	run, err := scanProductionRun(tx.QueryRow(ctx, productionRunSelectSQL+`
		WHERE run.organization_id = $1
		  AND run.idempotency_scope = $2
		  AND run.idempotency_key = $3
		FOR UPDATE OF run
	`, organizationID, idempotencyScope, idempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductionRun{}, false, nil
	}
	return run, err == nil, err
}

func (r *Repository) AssertNoActiveProductionSubjectOverlap(
	ctx context.Context,
	tx pgx.Tx,
	identity UnitGenerationIdentity,
	runType ProductionRunType,
	subjects []ProductionSubject,
) error {
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1 || chr(31) || $2 || chr(31) || $3 || chr(31) || $4, 0))
	`, identity.OrganizationID, identity.ProjectID, identity.UnitGenerationID, runType); err != nil {
		return err
	}
	for _, subject := range subjects {
		var conflictingRunID string
		err := tx.QueryRow(ctx, `
			SELECT run.id::text
			FROM commerce_production_runs run
			JOIN commerce_production_run_items item
			  ON item.run_id = run.id
			 AND item.organization_id = run.organization_id
			 AND item.project_id = run.project_id
			WHERE run.organization_id = $1 AND run.project_id = $2
			  AND run.script_unit_generation_id = $3
			  AND run.run_type = $4
			  AND run.status IN ('queued', 'running', 'cancelling')
			  AND item.subject_type = $5 AND item.subject_key = $6
			ORDER BY run.created_at DESC
			LIMIT 1
			FOR UPDATE OF run
		`, identity.OrganizationID, identity.ProjectID, identity.UnitGenerationID,
			runType, subject.Type, subject.Key).Scan(&conflictingRunID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		return Error{
			Code: CodeRunStateConflict, Message: "所选生产对象已有运行中的批次",
			Details: map[string]any{"conflictingRunId": conflictingRunID, "subjectKey": subject.Key},
		}
	}
	return nil
}

func (r *Repository) ListProductionRuns(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	scriptUnitID string,
	runType ProductionRunType,
	limit int,
) ([]ProductionRun, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := db.Query(ctx, productionRunSelectSQL+`
		WHERE run.organization_id = $1 AND run.project_id = $2
		  AND ($3 = '' OR run.script_unit_id = $3::uuid)
		  AND ($4 = '' OR run.run_type = $4)
		ORDER BY run.created_at DESC, run.id DESC
		LIMIT $5
	`, organizationID, projectID, scriptUnitID, runType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProductionRun, 0)
	for rows.Next() {
		item, err := scanProductionRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetProductionRun(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	runID string,
) (ProductionRun, error) {
	return scanProductionRun(db.QueryRow(ctx, productionRunSelectSQL+`
		WHERE run.id = $1 AND run.organization_id = $2 AND run.project_id = $3
	`, runID, organizationID, projectID))
}

func (r *Repository) CancelProductionRun(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	reason string,
) (ProductionRun, error) {
	var status ProductionRunStatus
	if err := tx.QueryRow(ctx, `
		SELECT status
		FROM commerce_production_runs
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		FOR UPDATE
	`, runID, organizationID, projectID).Scan(&status); err != nil {
		return ProductionRun{}, err
	}
	if status == RunSucceeded || status == RunPartiallySucceeded || status == RunFailed || status == RunCancelled {
		return scanProductionRun(tx.QueryRow(ctx, productionRunSelectSQL+`
			WHERE run.id = $1 AND run.organization_id = $2 AND run.project_id = $3
		`, runID, organizationID, projectID))
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_production_runs
		SET status = 'cancelling', error_code = 'USER_CANCELLED', error_message = $4,
		    revision = revision + 1
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('queued', 'running', 'cancelling')
	`, runID, organizationID, projectID, reason); err != nil {
		return ProductionRun{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_item_attempts attempt
		SET status = 'cancelled', error_code = 'USER_CANCELLED', error_message = $4,
		    retryable = false, completed_at = COALESCE(attempt.completed_at, now())
		FROM commerce_production_run_items item
		WHERE attempt.item_id = item.id AND attempt.run_id = item.run_id
		  AND item.run_id = $1 AND item.organization_id = $2 AND item.project_id = $3
		  AND attempt.status IN ('queued', 'running')
	`, runID, organizationID, projectID, reason); err != nil {
		return ProductionRun{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_items
		SET status = 'cancelled', error_code = 'USER_CANCELLED', error_message = $4,
		    retryable = false, completed_at = COALESCE(completed_at, now()), updated_at = now()
		WHERE run_id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('queued', 'running', 'failed_retryable')
	`, runID, organizationID, projectID, reason); err != nil {
		return ProductionRun{}, err
	}
	return r.ReconcileProductionRun(ctx, tx, organizationID, projectID, runID)
}

func (r *Repository) FailActiveProductionRunItems(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	errorCode string,
	errorMessage string,
	retryable bool,
) (ProductionRun, error) {
	var status ProductionRunStatus
	if err := tx.QueryRow(ctx, `
		SELECT status
		FROM commerce_production_runs
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		FOR UPDATE
	`, runID, organizationID, projectID).Scan(&status); err != nil {
		return ProductionRun{}, err
	}
	if status == RunSucceeded || status == RunPartiallySucceeded || status == RunFailed || status == RunCancelled {
		return scanProductionRun(tx.QueryRow(ctx, productionRunSelectSQL+`
			WHERE run.id = $1 AND run.organization_id = $2 AND run.project_id = $3
		`, runID, organizationID, projectID))
	}
	itemStatus := ItemFailedTerminal
	if retryable {
		itemStatus = ItemFailedRetryable
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_item_attempts attempt
		SET status = $4, error_code = $5, error_message = $6,
		    retryable = $7, completed_at = COALESCE(attempt.completed_at, now())
		FROM commerce_production_run_items item
		WHERE attempt.item_id = item.id AND attempt.run_id = item.run_id
		  AND item.run_id = $1 AND item.organization_id = $2 AND item.project_id = $3
		  AND attempt.status IN ('queued', 'running')
	`, runID, organizationID, projectID, itemStatus, errorCode, errorMessage, retryable); err != nil {
		return ProductionRun{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_items
		SET status = $4, error_code = $5, error_message = $6,
		    retryable = $7, completed_at = COALESCE(completed_at, now()), updated_at = now()
		WHERE run_id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('queued', 'running')
	`, runID, organizationID, projectID, itemStatus, errorCode, errorMessage, retryable); err != nil {
		return ProductionRun{}, err
	}
	return r.ReconcileProductionRun(ctx, tx, organizationID, projectID, runID)
}

func (r *Repository) ListProductionRunItems(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	runID string,
) ([]ProductionRunItem, error) {
	rows, err := db.Query(ctx, productionRunItemSelectSQL+`
		WHERE item.run_id = $1 AND item.organization_id = $2 AND item.project_id = $3
		ORDER BY item.subject_key, item.id
	`, runID, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProductionRunItem, 0)
	for rows.Next() {
		item, err := scanProductionRunItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) AttachProductionRunWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	workflowRunID string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_production_runs
		SET workflow_run_id = $4
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND workflow_run_id IS NULL AND status = 'queued'
	`, runID, organizationID, projectID, workflowRunID)
	if err != nil || tag.RowsAffected() != 1 {
		return affectedRowsError(err, "生产批次工作流绑定状态已变化")
	}
	return nil
}

func (r *Repository) InsertProductionRun(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	payloadHash string,
	params CreateProductionRunParams,
) (ProductionRun, error) {
	var createdAt sql.NullTime
	err := tx.QueryRow(ctx, `
		INSERT INTO commerce_production_runs(
			id, organization_id, project_id, product_id, script_unit_id,
			script_unit_generation_id, project_production_generation_id,
			run_type, status, idempotency_scope, idempotency_key, payload_hash,
			input_snapshot, workflow_run_id, coordinator_item_id,
			total_items, created_by
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, 'queued', $9, $10, $11,
			$12, NULLIF($13, '')::uuid, NULLIF($14, '')::uuid,
			$15, NULLIF($16, '')::uuid
		)
		RETURNING created_at
	`, id, params.Identity.OrganizationID, params.Identity.ProjectID, params.Identity.ProductID,
		params.Identity.ScriptUnitID, params.Identity.UnitGenerationID,
		params.Identity.ProjectGenerationID, params.RunType, params.IdempotencyScope,
		params.IdempotencyKey, payloadHash, params.InputSnapshot, params.WorkflowRunID,
		params.CoordinatorItemID, len(params.Subjects), params.CreatedBy).Scan(&createdAt)
	if err != nil {
		return ProductionRun{}, err
	}
	run := ProductionRun{
		ID:             id,
		Identity:       params.Identity,
		RunType:        params.RunType,
		Status:         RunQueued,
		PayloadHash:    payloadHash,
		Revision:       1,
		TotalItems:     len(params.Subjects),
		CompletedItems: 0,
		FailedItems:    0,
		CancelledItems: 0,
	}
	if createdAt.Valid {
		run.CreatedAt = createdAt.Time
	}
	return run, nil
}

func (r *Repository) InsertProductionRunItems(
	ctx context.Context,
	tx pgx.Tx,
	run ProductionRun,
	subjects []ProductionSubject,
) error {
	for _, subject := range subjects {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_production_run_items(
				organization_id, project_id, run_id, script_unit_id,
				script_unit_generation_id, subject_type, subject_key,
				storyboard_shot_id, input_hash
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, $9)
		`, run.Identity.OrganizationID, run.Identity.ProjectID, run.ID,
			run.Identity.ScriptUnitID, run.Identity.UnitGenerationID,
			subject.Type, subject.Key, subject.StoryboardShotID, subject.InputHash); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) LockProductionRunItem(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
	itemID string,
) (ProductionRunItem, error) {
	var item ProductionRunItem
	var storyboardShotID sql.NullString
	var startedAt, completedAt sql.NullTime
	err := tx.QueryRow(ctx, `
		SELECT item.id::text, item.run_id::text,
		       item.organization_id::text, item.project_id::text,
		       run.product_id::text, item.script_unit_id::text, unit.revision,
		       item.script_unit_generation_id::text,
		       unit_generation.unit_generation_no, unit_generation.unit_configuration_hash,
		       run.project_production_generation_id::text,
		       project_generation.binding_id::text, video_binding.revision,
		       video_binding.profile_snapshot_hash,
		       project_generation.commerce_workflow_binding_id::text,
		       commerce_binding.binding_revision, commerce_binding.configuration_hash,
		       item.subject_type, item.subject_key, item.storyboard_shot_id::text,
		       item.input_hash, item.status, item.current_attempt,
		       item.output_snapshot, COALESCE(item.error_code, ''),
		       COALESCE(item.error_message, ''), item.retryable,
		       item.started_at, item.completed_at
		FROM commerce_production_run_items item
		JOIN commerce_production_runs run
		  ON run.id = item.run_id
		 AND run.script_unit_id = item.script_unit_id
		 AND run.script_unit_generation_id = item.script_unit_generation_id
		 AND run.organization_id = item.organization_id
		 AND run.project_id = item.project_id
		JOIN commerce_script_units unit
		  ON unit.id = item.script_unit_id
		 AND unit.product_id = run.product_id
		 AND unit.organization_id = item.organization_id
		 AND unit.project_id = item.project_id
		JOIN commerce_script_unit_generations unit_generation
		  ON unit_generation.id = item.script_unit_generation_id
		 AND unit_generation.script_unit_id = item.script_unit_id
		 AND unit_generation.product_id = run.product_id
		 AND unit_generation.organization_id = item.organization_id
		 AND unit_generation.project_id = item.project_id
		JOIN project_video_production_generations project_generation
		  ON project_generation.id = run.project_production_generation_id
		 AND project_generation.project_id = run.project_id
		 AND project_generation.organization_id = run.organization_id
		JOIN project_video_production_bindings video_binding
		  ON video_binding.id = project_generation.binding_id
		 AND video_binding.project_id = project_generation.project_id
		JOIN project_commerce_workflow_bindings commerce_binding
		  ON commerce_binding.id = project_generation.commerce_workflow_binding_id
		 AND commerce_binding.project_id = project_generation.project_id
		 AND commerce_binding.organization_id = project_generation.organization_id
		WHERE item.id = $1 AND item.run_id = $2
		  AND item.project_id = $3 AND item.organization_id = $4
		FOR UPDATE OF item, run
	`, itemID, runID, projectID, organizationID).Scan(
		&item.ID,
		&item.RunID,
		&item.Identity.OrganizationID,
		&item.Identity.ProjectID,
		&item.Identity.ProductID,
		&item.Identity.ScriptUnitID,
		&item.Identity.ScriptUnitRevision,
		&item.Identity.UnitGenerationID,
		&item.Identity.UnitGenerationNo,
		&item.Identity.UnitConfigurationHash,
		&item.Identity.ProjectGenerationID,
		&item.Identity.VideoProductionBindingID,
		&item.Identity.VideoProductionBindingRevision,
		&item.Identity.VideoProfileSnapshotHash,
		&item.Identity.CommerceWorkflowBindingID,
		&item.Identity.CommerceWorkflowBindingRevision,
		&item.Identity.CommerceConfigurationHash,
		&item.Subject.Type,
		&item.Subject.Key,
		&storyboardShotID,
		&item.Subject.InputHash,
		&item.Status,
		&item.CurrentAttempt,
		&item.OutputSnapshot,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.Retryable,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		return ProductionRunItem{}, err
	}
	if storyboardShotID.Valid {
		item.Subject.StoryboardShotID = storyboardShotID.String
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, nil
}

func (r *Repository) InsertProductionAttempt(
	ctx context.Context,
	tx pgx.Tx,
	item ProductionRunItem,
	params StartProductionAttemptParams,
) (ProductionAttempt, error) {
	attempt := ProductionAttempt{
		ID:            newID(),
		RunID:         item.RunID,
		ItemID:        item.ID,
		AttemptNumber: item.CurrentAttempt + 1,
		InputHash:     params.InputHash,
		Status:        ItemRunning,
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_production_run_item_attempts(
			id, organization_id, project_id, run_id, item_id,
			attempt_number, input_hash, status, workflow_run_id, node_run_id, started_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'running',
		        NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, now())
	`, attempt.ID, item.Identity.OrganizationID, item.Identity.ProjectID, item.RunID,
		item.ID, attempt.AttemptNumber, params.InputHash, params.WorkflowRunID, params.NodeRunID); err != nil {
		return ProductionAttempt{}, err
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_items
		SET status = 'running', current_attempt = $3,
		    started_at = COALESCE(started_at, now()), completed_at = NULL,
		    error_code = NULL, error_message = NULL, retryable = false,
		    updated_at = now()
		WHERE id = $1 AND run_id = $2 AND current_attempt = $4
		  AND status IN ('queued', 'failed_retryable')
	`, item.ID, item.RunID, attempt.AttemptNumber, item.CurrentAttempt); err != nil || tag.RowsAffected() != 1 {
		return ProductionAttempt{}, affectedRowsError(err, "生产项执行状态已变化")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_production_runs
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    completed_at = NULL, cancelled_at = NULL,
		    error_code = NULL, error_message = NULL, revision = revision + 1
		WHERE id = $1 AND project_id = $2 AND organization_id = $3
		  AND status NOT IN ('cancelling', 'cancelled')
	`, item.RunID, item.Identity.ProjectID, item.Identity.OrganizationID); err != nil || tag.RowsAffected() != 1 {
		return ProductionAttempt{}, affectedRowsError(err, "生产批次不能启动新的执行尝试")
	}
	return attempt, nil
}

func (r *Repository) CompleteProductionAttempt(
	ctx context.Context,
	tx pgx.Tx,
	item ProductionRunItem,
	params CompleteProductionAttemptParams,
) error {
	retryable := params.Status == ItemFailedRetryable
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_item_attempts
		SET status = $6,
		    provider_request_id = NULLIF($7, '')::uuid,
		    provider_call_id = NULLIF($8, '')::uuid,
		    provider_async_task_id = NULLIF($9, '')::uuid,
		    output_snapshot = $10,
		    output_artifact_id = NULLIF($11, '')::uuid,
		    output_media_file_id = NULLIF($12, '')::uuid,
		    output_storyboard_plan_id = NULLIF($13, '')::uuid,
		    output_video_prompt_plan_id = NULLIF($14, '')::uuid,
		    output_video_render_plan_id = NULLIF($15, '')::uuid,
		    output_final_video_version_id = NULLIF($16, '')::uuid,
		    error_code = NULLIF($17, ''), error_message = NULLIF($18, ''),
		    retryable = $19, completed_at = now()
		WHERE id = $1 AND item_id = $2 AND run_id = $3
		  AND organization_id = $4 AND project_id = $5
		  AND attempt_number = $20 AND status IN ('queued', 'running')
	`, params.AttemptID, item.ID, item.RunID, item.Identity.OrganizationID,
		item.Identity.ProjectID, params.Status, params.ProviderRequestID,
		params.ProviderCallID, params.ProviderAsyncTaskID, params.OutputSnapshot,
		params.OutputArtifactID, params.OutputMediaFileID, params.OutputStoryboardPlanID,
		params.OutputVideoPromptPlanID, params.OutputVideoRenderPlanID,
		params.OutputFinalVideoVersionID, params.ErrorCode, params.ErrorMessage,
		retryable, item.CurrentAttempt); err != nil || tag.RowsAffected() != 1 {
		return affectedRowsError(err, "生产项执行尝试已结束或身份不匹配")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_production_run_items
		SET status = $4, output_snapshot = $5,
		    output_artifact_id = NULLIF($6, '')::uuid,
		    output_media_file_id = NULLIF($7, '')::uuid,
		    output_storyboard_plan_id = NULLIF($8, '')::uuid,
		    output_video_prompt_plan_id = NULLIF($9, '')::uuid,
		    output_video_render_plan_id = NULLIF($10, '')::uuid,
		    output_final_video_version_id = NULLIF($11, '')::uuid,
		    error_code = NULLIF($12, ''), error_message = NULLIF($13, ''),
		    retryable = $14, completed_at = now(), updated_at = now()
		WHERE id = $1 AND run_id = $2 AND current_attempt = $3 AND status = 'running'
	`, item.ID, item.RunID, item.CurrentAttempt, params.Status, params.OutputSnapshot,
		params.OutputArtifactID, params.OutputMediaFileID, params.OutputStoryboardPlanID,
		params.OutputVideoPromptPlanID, params.OutputVideoRenderPlanID,
		params.OutputFinalVideoVersionID, params.ErrorCode, params.ErrorMessage, retryable); err != nil || tag.RowsAffected() != 1 {
		return affectedRowsError(err, "生产项完成状态已被其他执行修改")
	}
	return nil
}

func (r *Repository) ReconcileProductionRun(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	runID string,
) (ProductionRun, error) {
	var current ProductionRunStatus
	if err := tx.QueryRow(ctx, `
		SELECT status FROM commerce_production_runs
		WHERE id = $1 AND project_id = $2 AND organization_id = $3
		FOR UPDATE
	`, runID, projectID, organizationID).Scan(&current); err != nil {
		return ProductionRun{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT status FROM commerce_production_run_items
		WHERE run_id = $1 AND project_id = $2 AND organization_id = $3
		ORDER BY subject_type, subject_key
	`, runID, projectID, organizationID)
	if err != nil {
		return ProductionRun{}, err
	}
	statuses := make([]ProductionItemStatus, 0)
	for rows.Next() {
		var status ProductionItemStatus
		if err := rows.Scan(&status); err != nil {
			rows.Close()
			return ProductionRun{}, err
		}
		statuses = append(statuses, status)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ProductionRun{}, err
	}
	aggregate := AggregateProductionRun(current, statuses)
	terminal := aggregate.Status == RunPartiallySucceeded || aggregate.Status == RunSucceeded ||
		aggregate.Status == RunFailed || aggregate.Status == RunCancelled
	cancelled := aggregate.Status == RunCancelled
	errorCode := ""
	errorMessage := ""
	if aggregate.Status == RunFailed {
		errorCode = "ALL_ITEMS_FAILED"
		errorMessage = "所有生产项均执行失败"
	} else if aggregate.Status == RunPartiallySucceeded {
		errorCode = "PARTIAL_FAILURE"
		errorMessage = "部分生产项未成功完成"
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_production_runs
		SET status = $4, total_items = $5, completed_items = $6,
		    failed_items = $7, cancelled_items = $8,
		    completed_at = CASE WHEN $9 THEN COALESCE(completed_at, now()) ELSE NULL END,
		    cancelled_at = CASE WHEN $10 THEN COALESCE(cancelled_at, now()) ELSE NULL END,
		    error_code = NULLIF($11, ''), error_message = NULLIF($12, ''),
		    revision = revision + 1
		WHERE id = $1 AND project_id = $2 AND organization_id = $3
	`, runID, projectID, organizationID, aggregate.Status, aggregate.Total,
		aggregate.Completed, aggregate.Failed, aggregate.Cancelled, terminal,
		cancelled, errorCode, errorMessage); err != nil || tag.RowsAffected() != 1 {
		return ProductionRun{}, affectedRowsError(err, "生产批次聚合状态已变化")
	}
	return scanProductionRun(tx.QueryRow(ctx, productionRunSelectSQL+`
		WHERE run.id = $1 AND run.project_id = $2 AND run.organization_id = $3
	`, runID, projectID, organizationID))
}

func scanProductionRun(row pgx.Row) (ProductionRun, error) {
	var run ProductionRun
	var startedAt, completedAt sql.NullTime
	var workflowRunID sql.NullString
	err := row.Scan(
		&run.ID,
		&run.Identity.OrganizationID,
		&run.Identity.ProjectID,
		&run.Identity.ProductID,
		&run.Identity.ScriptUnitID,
		&run.Identity.ScriptUnitRevision,
		&run.Identity.UnitGenerationID,
		&run.Identity.UnitGenerationNo,
		&run.Identity.UnitConfigurationHash,
		&run.Identity.ProjectGenerationID,
		&run.Identity.VideoProductionBindingID,
		&run.Identity.VideoProductionBindingRevision,
		&run.Identity.VideoProfileSnapshotHash,
		&run.Identity.CommerceWorkflowBindingID,
		&run.Identity.CommerceWorkflowBindingRevision,
		&run.Identity.CommerceConfigurationHash,
		&workflowRunID,
		&run.RunType,
		&run.Status,
		&run.PayloadHash,
		&run.InputSnapshot,
		&run.Revision,
		&run.TotalItems,
		&run.CompletedItems,
		&run.FailedItems,
		&run.CancelledItems,
		&run.CreatedAt,
		&startedAt,
		&completedAt,
		&run.ErrorCode,
		&run.ErrorMessage,
	)
	if err != nil {
		return ProductionRun{}, err
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if workflowRunID.Valid {
		run.WorkflowRunID = workflowRunID.String
	}
	return run, nil
}

func scanProductionRunItem(row pgx.Row) (ProductionRunItem, error) {
	var item ProductionRunItem
	var storyboardShotID sql.NullString
	var outputArtifactID, outputMediaFileID sql.NullString
	var outputStoryboardPlanID, outputVideoPromptPlanID sql.NullString
	var outputVideoRenderPlanID, outputFinalVideoVersionID sql.NullString
	var providerRequestID, providerCallID, providerAsyncTaskID sql.NullString
	var startedAt, completedAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.RunID,
		&item.Identity.OrganizationID,
		&item.Identity.ProjectID,
		&item.Identity.ProductID,
		&item.Identity.ScriptUnitID,
		&item.Identity.ScriptUnitRevision,
		&item.Identity.UnitGenerationID,
		&item.Identity.UnitGenerationNo,
		&item.Identity.UnitConfigurationHash,
		&item.Identity.ProjectGenerationID,
		&item.Identity.VideoProductionBindingID,
		&item.Identity.VideoProductionBindingRevision,
		&item.Identity.VideoProfileSnapshotHash,
		&item.Identity.CommerceWorkflowBindingID,
		&item.Identity.CommerceWorkflowBindingRevision,
		&item.Identity.CommerceConfigurationHash,
		&item.Subject.Type,
		&item.Subject.Key,
		&storyboardShotID,
		&item.Subject.InputHash,
		&item.Status,
		&item.CurrentAttempt,
		&item.OutputSnapshot,
		&outputArtifactID,
		&outputMediaFileID,
		&outputStoryboardPlanID,
		&outputVideoPromptPlanID,
		&outputVideoRenderPlanID,
		&outputFinalVideoVersionID,
		&providerRequestID,
		&providerCallID,
		&providerAsyncTaskID,
		&item.ErrorCode,
		&item.ErrorMessage,
		&item.Retryable,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		return ProductionRunItem{}, err
	}
	item.Subject.StoryboardShotID = nullableSQLString(storyboardShotID)
	item.OutputArtifactID = nullableSQLString(outputArtifactID)
	item.OutputMediaFileID = nullableSQLString(outputMediaFileID)
	item.OutputStoryboardPlanID = nullableSQLString(outputStoryboardPlanID)
	item.OutputVideoPromptPlanID = nullableSQLString(outputVideoPromptPlanID)
	item.OutputVideoRenderPlanID = nullableSQLString(outputVideoRenderPlanID)
	item.OutputFinalVideoVersionID = nullableSQLString(outputFinalVideoVersionID)
	item.ProviderRequestID = nullableSQLString(providerRequestID)
	item.ProviderCallID = nullableSQLString(providerCallID)
	item.ProviderAsyncTaskID = nullableSQLString(providerAsyncTaskID)
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, nil
}

func nullableSQLString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func validateProductionRunType(value ProductionRunType) error {
	switch value {
	case RunTypeStoryboardPlan, RunTypeReferenceImages, RunTypeVideoPrompts, RunTypeShotVideos, RunTypeFinalCompose:
		return nil
	default:
		return fmt.Errorf("unsupported commerce production run type %q", value)
	}
}
