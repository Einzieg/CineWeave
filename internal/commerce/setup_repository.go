package commerce

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) LoadSetupSession(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	setupSessionID string,
	lock bool,
) (SetupSession, error) {
	query := `
		SELECT id::text, organization_id::text, workspace_id::text, project_id::text,
		       workflow_template_version_id::text, client_request_id, scope_type,
		       state, step, revision, input_snapshot, setup_attempt,
		       setup_workflow_run_id::text, production_workflow_run_id::text,
		       product_id::text, script_unit_id::text, source_script_version_id::text,
		       localization_id::text, last_error_code, last_error_message,
		       created_at, updated_at, expires_at, completed_at
		FROM commerce_setup_sessions
		WHERE id = $1 AND project_id = $2 AND organization_id = $3`
	if lock {
		query += " FOR UPDATE"
	}
	var item SetupSession
	var setupWorkflowRunID, productionWorkflowRunID pgtype.Text
	var productID, scriptUnitID, sourceVersionID, localizationID pgtype.Text
	var errorCode, errorMessage pgtype.Text
	err := db.QueryRow(ctx, query, setupSessionID, projectID, organizationID).Scan(
		&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.ProjectID,
		&item.WorkflowTemplateVersionID, &item.ClientRequestID, &item.ScopeType,
		&item.State, &item.Step, &item.Revision, &item.InputSnapshot, &item.SetupAttempt,
		&setupWorkflowRunID, &productionWorkflowRunID,
		&productID, &scriptUnitID, &sourceVersionID, &localizationID,
		&errorCode, &errorMessage, &item.CreatedAt, &item.UpdatedAt, &item.ExpiresAt, &item.CompletedAt,
	)
	if err != nil {
		return SetupSession{}, err
	}
	item.SetupWorkflowRunID = textPointer(setupWorkflowRunID)
	item.ProductionWorkflowRunID = textPointer(productionWorkflowRunID)
	item.ProductID = textPointer(productID)
	item.ScriptUnitID = textPointer(scriptUnitID)
	item.SourceScriptVersionID = textPointer(sourceVersionID)
	item.LocalizationID = textPointer(localizationID)
	item.LastErrorCode = textPointer(errorCode)
	item.LastErrorMessage = textPointer(errorMessage)
	return item, nil
}

func (r *Repository) UpdateSetupUploadKeys(
	ctx context.Context,
	tx pgx.Tx,
	item SetupSession,
	storageKey string,
	add bool,
) (SetupSession, error) {
	var snapshot map[string]any
	if err := json.Unmarshal(item.InputSnapshot, &snapshot); err != nil {
		return SetupSession{}, err
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	keys := setupUploadKeys(item.InputSnapshot)
	seen := make(map[string]bool, len(keys)+1)
	next := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		if key == storageKey && !add {
			continue
		}
		if !seen[key] {
			seen[key] = true
			next = append(next, key)
		}
	}
	if add && !seen[storageKey] {
		next = append(next, storageKey)
	}
	snapshot["pendingUploadKeys"] = next
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return SetupSession{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET input_snapshot = $2,
		    input_hash = encode(digest($2::jsonb::text, 'sha256'), 'hex'),
		    state = CASE WHEN state = 'draft' THEN 'uploading' ELSE state END,
		    step = CASE WHEN state = 'draft' THEN 'product_images' ELSE step END,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
	`, item.ID, raw); err != nil {
		return SetupSession{}, err
	}
	return r.LoadSetupSession(ctx, tx, item.OrganizationID, item.ProjectID, item.ID, false)
}

func (r *Repository) AbandonSetupSession(ctx context.Context, tx pgx.Tx, item SetupSession) (SetupSession, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = 'abandoned', step = 'abandoned', revision = revision + 1,
		    updated_at = now(), completed_at = NULL,
		    input_snapshot = input_snapshot - 'pendingUploadKeys',
		    input_hash = encode(digest((input_snapshot - 'pendingUploadKeys')::text, 'sha256'), 'hex')
		WHERE id = $1 AND revision = $2
	`, item.ID, item.Revision); err != nil {
		return SetupSession{}, err
	}
	return r.LoadSetupSession(ctx, tx, item.OrganizationID, item.ProjectID, item.ID, false)
}

func (r *Repository) AttachSetupRun(
	ctx context.Context,
	tx pgx.Tx,
	item SetupSession,
	runID string,
	preparation SetupPreparation,
) (SetupSession, error) {
	var previousRunID *string
	if item.SetupWorkflowRunID != nil {
		value := *item.SetupWorkflowRunID
		previousRunID = &value
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET setup_workflow_run_id = $3,
		    product_id = $4,
		    script_unit_id = $5,
		    source_script_version_id = $6,
		    state = 'starting',
		    step = 'workflow_queued',
		    setup_attempt = setup_attempt + 1,
		    last_error_code = NULL,
		    last_error_message = NULL,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND revision = $2
		  AND setup_workflow_run_id IS NOT DISTINCT FROM $7::uuid
	`, item.ID, item.Revision, runID, preparation.Product.ID, preparation.ScriptUnit.ID, preparation.SourceScriptVersion.ID, previousRunID)
	if err != nil {
		return SetupSession{}, err
	}
	if tag.RowsAffected() != 1 {
		return SetupSession{}, Error{Code: CodeSetupRevisionConflict, Message: "创建会话已更新，请刷新后重试"}
	}
	return r.LoadSetupSession(ctx, tx, item.OrganizationID, item.ProjectID, item.ID, false)
}

func (r *Repository) LoadSetupRun(ctx context.Context, db rowQuerier, organizationID, projectID, runID string) (SetupRun, error) {
	var item SetupRun
	var errorCode, errorMessage sql.NullString
	err := db.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, setup_session_id::text,
		       attempt_no, temporal_workflow_id, status, input, output, error_code, error_message,
		       created_at, updated_at, started_at, completed_at, revision
		FROM commerce_setup_runs
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
	`, runID, organizationID, projectID).Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.SetupSessionID, &item.AttemptNo,
		&item.TemporalWorkflowID, &item.Status, &item.Input, &item.Output,
		&errorCode, &errorMessage, &item.CreatedAt, &item.UpdatedAt,
		&item.StartedAt, &item.CompletedAt, &item.Revision,
	)
	if errorCode.Valid {
		item.ErrorCode = &errorCode.String
	}
	if errorMessage.Valid {
		item.ErrorMessage = &errorMessage.String
	}
	return item, err
}

func (r *Repository) InsertInitialUnitGeneration(
	ctx context.Context,
	tx pgx.Tx,
	params InitialSetupCommitParams,
	bindings InitialBindingResult,
	referencePackID string,
	unit ScriptUnit,
) (string, int64, string, error) {
	unitGenerationNo := unit.UnitGenerationNo + 1
	snapshot := map[string]any{
		"schemaVersion":                   2,
		"projectGenerationId":             bindings.ProjectGenerationID,
		"commerceWorkflowBindingId":       bindings.CommerceBindingID,
		"commerceWorkflowBindingRevision": bindings.CommerceBindingRevision,
		"videoProductionBindingId":        bindings.VideoBindingID,
		"videoProductionBindingRevision":  bindings.VideoBindingRevision,
		"workflowTemplateVersionId":       params.WorkflowTemplateVersionID,
		"productVersionId":                params.ProductVersionID,
		"sourceScriptVersionId":           params.SourceScriptVersionID,
		"localizationId":                  params.LocalizationID,
		"referencePackId":                 referencePackID,
		"targetDurationSeconds":           unit.TargetDurationSeconds,
		"targetPlatform":                  unit.TargetPlatform,
		"preparationWorkflowRunId":        params.SetupRunID,
		"preparationInputHash":            params.PreparationInputHash,
		"preparationAgentCalls":           json.RawMessage(params.PreparationAgentCalls),
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", 0, "", err
	}
	hash, err := hashJSON(raw)
	if err != nil {
		return "", 0, "", err
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_unit_generations(
			organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'preparing', $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id::text
	`, params.OrganizationID, params.ProjectID, params.ProductID, params.ScriptUnitID,
		bindings.ProjectGenerationID, unitGenerationNo, bindings.CommerceBindingID,
		bindings.CommerceBindingRevision, params.ProductVersionID, params.SourceScriptVersionID,
		params.LocalizationID, referencePackID, raw, hash, params.CreatedBy).Scan(&id); err != nil {
		return "", 0, "", err
	}
	return id, unitGenerationNo, hash, nil
}

func (r *Repository) ActivateInitialUnitGeneration(
	ctx context.Context,
	tx pgx.Tx,
	params InitialSetupCommitParams,
	unit ScriptUnit,
	unitGenerationID string,
	unitGenerationNo int64,
	output json.RawMessage,
) (SetupSession, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND script_unit_id = $2 AND status = 'preparing'
	`, unitGenerationID, unit.ID)
	if err != nil {
		return SetupSession{}, err
	}
	if tag.RowsAffected() != 1 {
		return SetupSession{}, Error{Code: CodeGenerationMismatch, Message: "首个脚本生产代已被其他操作修改"}
	}
	tag, err = tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET current_localization_id = $2, active_unit_generation_id = $3,
		    unit_generation_no = $4, status = 'ready', revision = revision + 1,
		    updated_at = now()
		WHERE id = $1 AND revision = $5
		  AND current_source_version_id = $6
		  AND active_unit_generation_id IS NULL
	`, unit.ID, params.LocalizationID, unitGenerationID, unitGenerationNo, unit.Revision, params.SourceScriptVersionID)
	if err != nil {
		return SetupSession{}, err
	}
	if tag.RowsAffected() != 1 {
		return SetupSession{}, Error{Code: CodeScriptUnitRevision, Message: "广告脚本在创建期间已变化，请重新启动"}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET status = 'succeeded', output = $2, error_code = NULL, error_message = NULL,
		    completed_at = now(), updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status IN ('running', 'waiting_user_confirmation', 'needs_user_review')
	`, params.SetupRunID, output); err != nil {
		return SetupSession{}, err
	}
	tag, err = tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = 'completed', step = 'completed', localization_id = $2,
		    completed_at = now(), last_error_code = NULL, last_error_message = NULL,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1 AND setup_workflow_run_id = $3
		  AND state NOT IN ('completed', 'abandoned')
	`, params.SetupSessionID, params.LocalizationID, params.SetupRunID)
	if err != nil {
		return SetupSession{}, err
	}
	if tag.RowsAffected() != 1 {
		return SetupSession{}, Error{Code: CodeSetupRevisionConflict, Message: "创建会话已变化，不能提交生产代"}
	}
	return r.LoadSetupSession(ctx, tx, params.OrganizationID, params.ProjectID, params.SetupSessionID, false)
}

func (r *Repository) MarkSetupWaitingForLanguage(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	setupRunID string,
	resolutionID string,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET status = 'waiting_user_confirmation', updated_at = now(), revision = revision + 1
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND status IN ('running', 'waiting_user_confirmation')
	`, setupRunID, organizationID, projectID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = 'waiting_user_confirmation', step = 'language_confirmation',
		    updated_at = now(), revision = revision + 1,
		    input_snapshot = jsonb_set(input_snapshot, '{languageResolutionId}', to_jsonb($5::text), true),
		    input_hash = encode(digest(jsonb_set(input_snapshot, '{languageResolutionId}', to_jsonb($5::text), true)::text, 'sha256'), 'hex')
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND setup_workflow_run_id = $4 AND state NOT IN ('completed', 'abandoned')
	`, setupSessionID, organizationID, projectID, setupRunID, resolutionID)
	return err
}

func (r *Repository) MarkSetupLanguageConfirmed(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	setupSessionID string,
	setupRunID string,
	expectedRevision int64,
) (SetupSession, error) {
	current, err := r.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, true)
	if err != nil {
		return SetupSession{}, err
	}
	if current.SetupWorkflowRunID == nil || *current.SetupWorkflowRunID != setupRunID {
		return SetupSession{}, Error{Code: CodeSetupRevisionConflict, Message: "创建任务身份已变化，请刷新后重试"}
	}
	if current.State == "started" && current.Step == "language_confirmed" {
		return current, nil
	}
	if current.Revision != expectedRevision {
		return SetupSession{}, Error{Code: CodeSetupRevisionConflict, Message: "语言确认状态已变化，请刷新后重试"}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_setup_sessions
		SET state = 'started', step = 'language_confirmed', updated_at = now(), revision = revision + 1
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND setup_workflow_run_id = $4 AND revision = $5
		  AND state = 'waiting_user_confirmation'
	`, setupSessionID, organizationID, projectID, setupRunID, current.Revision)
	if err != nil {
		return SetupSession{}, err
	}
	if tag.RowsAffected() != 1 {
		return SetupSession{}, Error{Code: CodeSetupRevisionConflict, Message: "语言确认状态已变化，请刷新后重试"}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_setup_runs
		SET status = 'running', updated_at = now(), revision = revision + 1
		WHERE id = $1 AND status = 'waiting_user_confirmation'
	`, setupRunID); err != nil {
		return SetupSession{}, err
	}
	return r.LoadSetupSession(ctx, tx, organizationID, projectID, setupSessionID, false)
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	copy := value.String
	return &copy
}
