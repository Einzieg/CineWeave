package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) ResolveScriptDerivationRoute(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	profileKey string,
) (ScriptDerivationRoutingSnapshot, error) {
	var item ScriptDerivationRoutingSnapshot
	err := db.QueryRow(ctx, `
		SELECT profile.id::text, profile.profile_key,
		       binding.id::text, binding.xmin::text::bigint,
		       model.id::text, model.model_key, binding.priority, binding.weight
		FROM model_profiles profile
		JOIN model_profile_bindings binding
		  ON binding.model_profile_id = profile.id AND binding.enabled
		JOIN provider_models model
		  ON model.id = binding.provider_model_id AND model.status = 'active'
		JOIN provider_accounts account
		  ON account.id = model.provider_account_id AND account.status = 'active'
		WHERE profile.organization_id = $1
		  AND profile.profile_key = $2
		  AND model.modality IN ('text', 'multimodal')
		ORDER BY binding.priority ASC, binding.weight DESC, binding.created_at, binding.id
		LIMIT 1
	`, organizationID, profileKey).Scan(
		&item.ModelProfileID, &item.ModelProfileKey,
		&item.ModelProfileBindingID, &item.BindingRevision,
		&item.ProviderModelID, &item.ProviderModelKey,
		&item.Priority, &item.Weight,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptDerivationRoutingSnapshot{}, Error{
			Code: CodeScriptDerivationModel, Message: "当前项目没有可用的脚本文本模型",
		}
	}
	return item, err
}

func (r *Repository) InsertScriptDerivationBatch(
	ctx context.Context,
	tx pgx.Tx,
	batch ScriptDerivationBatch,
	idempotencyKey string,
	requestHash string,
	positions []ScriptUnitPosition,
	retryItems []ScriptDerivationItem,
) error {
	promptContract, err := json.Marshal(batch.PromptContract)
	if err != nil {
		return err
	}
	preserve, err := json.Marshal(batch.Preserve)
	if err != nil {
		return err
	}
	variations, err := json.Marshal(batch.Variations)
	if err != nil {
		return err
	}
	if len(positions) != len(batch.Variations) {
		return errors.New("script derivation positions do not match variations")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO commerce_script_derivation_batches(
			id, organization_id, project_id, product_id, source_script_unit_id,
			source_content_snapshot, source_content_hash, product_version_id,
			product_snapshot_hash, production_generation_id,
			video_production_binding_id, video_production_binding_revision,
			production_configuration_hash, script_model_profile_key,
			model_profile_binding_id, model_profile_binding_revision, provider_model_id,
			routing_snapshot_hash, prompt_contract_snapshot, dimension, instruction,
			preserve_contract, variation_plan, requested_count, idempotency_key,
			request_hash, root_batch_id, retry_of_batch_id, retry_depth,
			workflow_run_id, status, queued_count, created_by
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
			$20,$21,$22,$23,$24,$25,$26,$27,NULLIF($28,'')::uuid,$29,
			NULLIF($30,'')::uuid,
			'queued',$24,NULLIF($31,'')::uuid
		)
	`, batch.ID, batch.OrganizationID, batch.ProjectID, batch.ProductID,
		batch.SourceScriptUnitID, batch.SourceContentSnapshot, batch.SourceContentHash,
		batch.ProductVersionID, batch.ProductSnapshotHash, batch.ProductionGenerationID,
		batch.VideoProductionBindingID, batch.VideoProductionBindingRevision,
		batch.ProductionConfigurationHash, batch.ScriptModelProfileKey,
		pointerText(batch.ModelProfileBindingID), batch.ModelProfileBindingRevision,
		pointerText(batch.ProviderModelID), batch.RoutingSnapshotHash, promptContract,
		batch.Dimension, batch.Instruction, preserve, variations, batch.RequestedCount,
		idempotencyKey, requestHash, pointerText(batch.RootBatchID),
		pointerText(batch.RetryOfBatchID), batch.RetryDepth,
		pointerText(batch.WorkflowRunID), pointerText(batch.CreatedBy))
	if err != nil {
		return err
	}
	for index, variation := range batch.Variations {
		var rootItemID, retryOfItemID *string
		if len(retryItems) > 0 {
			rootItemID = retryItems[index].RootItemID
			if rootItemID == nil {
				rootItemID = &retryItems[index].ID
			}
			retryOfItemID = &retryItems[index].ID
		}
		if _, err := r.insertScriptDerivationItem(
			ctx, tx, batch, variation, positions[index], rootItemID, retryOfItemID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) insertScriptDerivationItem(
	ctx context.Context,
	tx pgx.Tx,
	batch ScriptDerivationBatch,
	variation ScriptDerivationVariation,
	position ScriptUnitPosition,
	rootItemID *string,
	retryOfItemID *string,
) (ScriptDerivationItem, error) {
	itemID := uuid.NewString()
	if rootItemID == nil {
		rootItemID = &itemID
	}
	inputSnapshot := mustJSON(map[string]any{
		"dimension": batch.Dimension, "instruction": batch.Instruction,
		"preserve": batch.Preserve, "variation": variation,
		"sourceContentHash":   batch.SourceContentHash,
		"productSnapshotHash": batch.ProductSnapshotHash,
		"routingSnapshotHash": batch.RoutingSnapshotHash,
	})
	inputHash, err := DirectVideoHash(inputSnapshot)
	if err != nil {
		return ScriptDerivationItem{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_derivation_items(
			id, batch_id, organization_id, project_id, product_id, input_ordinal,
			root_item_id, retry_of_item_id, variation_key, variation_label,
			variation_brief, input_snapshot, input_hash, reserved_unit_no,
			reserved_sort_order, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11,$12,$13,$14,$15,'queued')
	`, itemID, batch.ID, batch.OrganizationID, batch.ProjectID, batch.ProductID,
		variation.Ordinal, pointerText(rootItemID), pointerText(retryOfItemID),
		variation.Key, variation.Label, variation.Brief, inputSnapshot, inputHash,
		position.UnitNo, position.SortOrder); err != nil {
		return ScriptDerivationItem{}, err
	}
	attemptID := uuid.NewString()
	var rootAttemptID *string
	if retryOfItemID != nil {
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(root_attempt_id, id)::text
			FROM commerce_script_derivation_attempts
			WHERE item_id = $1
			ORDER BY attempt_no DESC
			LIMIT 1
		`, *retryOfItemID).Scan(&rootAttemptID); err != nil {
			return ScriptDerivationItem{}, err
		}
	} else {
		rootAttemptID = &attemptID
	}
	var retryOfAttemptID *string
	if retryOfItemID != nil {
		if err := tx.QueryRow(ctx, `
			SELECT id::text
			FROM commerce_script_derivation_attempts
			WHERE item_id = $1
			ORDER BY attempt_no DESC
			LIMIT 1
		`, *retryOfItemID).Scan(&retryOfAttemptID); err != nil {
			return ScriptDerivationItem{}, err
		}
	}
	attemptNo := batch.RetryDepth + 1
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_derivation_attempts(
			id, batch_id, item_id, organization_id, project_id, product_id,
			attempt_no, root_attempt_id, retry_of_attempt_id, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,'queued')
	`, attemptID, batch.ID, itemID, batch.OrganizationID, batch.ProjectID,
		batch.ProductID, attemptNo, pointerText(rootAttemptID),
		pointerText(retryOfAttemptID)); err != nil {
		return ScriptDerivationItem{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_items
		SET current_attempt_id = $2
		WHERE id = $1
	`, itemID, attemptID); err != nil {
		return ScriptDerivationItem{}, err
	}
	return r.LoadScriptDerivationItem(ctx, tx, batch.OrganizationID, batch.ProjectID, itemID, false)
}

func (r *Repository) LoadScriptDerivationBatch(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	batchID string,
	lock bool,
) (ScriptDerivationBatch, error) {
	query := scriptDerivationBatchSelectSQL + `
		WHERE batch.id = $1 AND batch.organization_id = $2 AND batch.project_id = $3`
	if lock {
		query += ` FOR UPDATE OF batch`
	}
	item, err := scanScriptDerivationBatch(db.QueryRow(ctx, query, batchID, organizationID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptDerivationBatch{}, Error{Code: CodeScriptDerivationNotFound, Message: "脚本裂变任务不存在", Cause: err}
	}
	return item, err
}

func (r *Repository) ListScriptDerivationBatches(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	status string,
	sourceScriptUnitID string,
	cursor time.Time,
	cursorID string,
	limit int,
) ([]ScriptDerivationBatch, error) {
	query := scriptDerivationBatchSelectSQL + `
		WHERE batch.organization_id = $1 AND batch.project_id = $2`
	args := []any{organizationID, projectID}
	if status != "" && status != "all" {
		query += fmt.Sprintf(` AND batch.status = $%d`, len(args)+1)
		args = append(args, status)
	}
	if strings.TrimSpace(sourceScriptUnitID) != "" {
		query += fmt.Sprintf(` AND batch.source_script_unit_id = $%d`, len(args)+1)
		args = append(args, sourceScriptUnitID)
	}
	if !cursor.IsZero() && cursorID != "" {
		query += fmt.Sprintf(` AND (batch.created_at, batch.id) < ($%d, $%d::uuid)`, len(args)+1, len(args)+2)
		args = append(args, cursor, cursorID)
	}
	query += fmt.Sprintf(` ORDER BY batch.created_at DESC, batch.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptDerivationBatch, 0, limit)
	for rows.Next() {
		item, err := scanScriptDerivationBatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadScriptDerivationItem(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	itemID string,
	lock bool,
) (ScriptDerivationItem, error) {
	query := scriptDerivationItemSelectSQL + `
		WHERE item.id = $1 AND item.organization_id = $2 AND item.project_id = $3`
	if lock {
		query += ` FOR UPDATE OF item`
	}
	item, err := scanScriptDerivationItem(db.QueryRow(ctx, query, itemID, organizationID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ScriptDerivationItem{}, Error{Code: CodeScriptDerivationNotFound, Message: "脚本裂变条目不存在", Cause: err}
	}
	return item, err
}

func (r *Repository) ListScriptDerivationItems(
	ctx context.Context,
	db rowsQuerier,
	batchID string,
) ([]ScriptDerivationItem, error) {
	rows, err := db.Query(ctx, scriptDerivationItemSelectSQL+`
		WHERE item.batch_id = $1
		ORDER BY item.input_ordinal, item.id
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptDerivationItem, 0)
	for rows.Next() {
		item, err := scanScriptDerivationItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadScriptDerivationAttempt(
	ctx context.Context,
	db rowQuerier,
	itemID string,
) (ScriptDerivationAttempt, error) {
	return scanScriptDerivationAttempt(db.QueryRow(ctx, scriptDerivationAttemptSelectSQL+`
		WHERE attempt.item_id = $1
		ORDER BY attempt.attempt_no DESC
		LIMIT 1
	`, itemID))
}

func (r *Repository) StartScriptDerivationBatch(
	ctx context.Context,
	tx pgx.Tx,
	batch ScriptDerivationBatch,
) (ScriptDerivationBatch, error) {
	if batch.Status != "queued" && batch.Status != "running" {
		return ScriptDerivationBatch{}, Error{Code: CodeScriptDerivationState, Message: "脚本裂变批次当前不能启动"}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_batches
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    revision = revision + CASE WHEN status = 'queued' THEN 1 ELSE 0 END,
		    updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'running')
	`, batch.ID); err != nil {
		return ScriptDerivationBatch{}, err
	}
	return r.LoadScriptDerivationBatch(ctx, tx, batch.OrganizationID, batch.ProjectID, batch.ID, true)
}

func (r *Repository) StartScriptDerivationItem(
	ctx context.Context,
	tx pgx.Tx,
	item ScriptDerivationItem,
) (ScriptDerivationAttempt, error) {
	if item.Status != "queued" && item.Status != "running" && item.Status != "reviewing" {
		return ScriptDerivationAttempt{}, Error{Code: CodeScriptDerivationState, Message: "脚本裂变条目当前不能启动"}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_items
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    revision = revision + CASE WHEN status = 'queued' THEN 1 ELSE 0 END,
		    updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'running', 'reviewing')
	`, item.ID); err != nil {
		return ScriptDerivationAttempt{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempts
		SET status = CASE WHEN status = 'queued' THEN 'generating' ELSE status END,
		    started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'generating', 'reviewing', 'revising')
	`, pointerText(item.CurrentAttemptID)); err != nil {
		return ScriptDerivationAttempt{}, err
	}
	return r.LoadScriptDerivationAttempt(ctx, tx, item.ID)
}

func (r *Repository) SetScriptDerivationReviewState(
	ctx context.Context,
	tx pgx.Tx,
	itemID string,
	attemptID string,
	round int,
	status string,
	reviewResult json.RawMessage,
	feedback string,
) error {
	itemStatus := "running"
	if status == "reviewing" {
		itemStatus = "reviewing"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_items
		SET status = $2, revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status IN ('running', 'reviewing')
	`, itemID, itemStatus); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempts
		SET status = $3, review_round = greatest(review_round, $4),
		    review_result = CASE WHEN $5::jsonb = '{}'::jsonb THEN review_result ELSE $5::jsonb END,
		    review_feedback = NULLIF($6, ''), updated_at = now()
		WHERE id = $1 AND item_id = $2
		  AND status IN ('generating', 'reviewing', 'revising')
	`, attemptID, itemID, status, round, reviewResult, feedback)
	return err
}

func (r *Repository) InsertScriptDerivationAttemptCall(
	ctx context.Context,
	tx pgx.Tx,
	call ScriptDerivationAttemptCall,
) (ScriptDerivationAttemptCall, error) {
	callID := strings.TrimSpace(call.ID)
	if callID == "" {
		callID = uuid.NewString()
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_derivation_attempt_calls(
			id, batch_id, item_id, attempt_id, organization_id, project_id,
			product_id, round_no, phase, model_profile_key,
			model_profile_binding_id, provider_model_id, prompt_template_key,
			prompt_version_id, prompt_hash, status, started_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')::uuid,
		        NULLIF($12,'')::uuid,$13,$14,$15,'running',$16)
		ON CONFLICT (attempt_id, round_no, phase) DO UPDATE
		SET status = CASE
		        WHEN commerce_script_derivation_attempt_calls.status = 'succeeded' THEN 'succeeded'
		        ELSE 'running'
		    END,
		    error_code = CASE
		        WHEN commerce_script_derivation_attempt_calls.status = 'succeeded'
		            THEN commerce_script_derivation_attempt_calls.error_code
		        ELSE NULL
		    END,
		    error_message = CASE
		        WHEN commerce_script_derivation_attempt_calls.status = 'succeeded'
		            THEN commerce_script_derivation_attempt_calls.error_message
		        ELSE NULL
		    END,
		    completed_at = CASE
		        WHEN commerce_script_derivation_attempt_calls.status = 'succeeded'
		            THEN commerce_script_derivation_attempt_calls.completed_at
		        ELSE NULL
		    END,
		    started_at = CASE
		        WHEN commerce_script_derivation_attempt_calls.status = 'succeeded'
		            THEN commerce_script_derivation_attempt_calls.started_at
		        ELSE EXCLUDED.started_at
		    END
	`, callID, call.BatchID, call.ItemID, call.AttemptID, call.OrganizationID,
		call.ProjectID, call.ProductID, call.RoundNo, call.Phase,
		call.ModelProfileKey, pointerText(call.ModelProfileBindingID),
		pointerText(call.ProviderModelID), call.PromptTemplateKey,
		call.PromptVersionID, call.PromptHash, call.StartedAt)
	if err != nil {
		return ScriptDerivationAttemptCall{}, err
	}
	return scanScriptDerivationAttemptCall(tx.QueryRow(ctx, scriptDerivationAttemptCallSelectSQL+`
		WHERE call.attempt_id = $1 AND call.round_no = $2 AND call.phase = $3
	`, call.AttemptID, call.RoundNo, call.Phase))
}

func (r *Repository) CompleteScriptDerivationAttemptCall(
	ctx context.Context,
	tx pgx.Tx,
	callID string,
	providerRequestID string,
	providerCallID string,
	providerModelID string,
	outputHash string,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempt_calls
		SET status = 'succeeded',
		    provider_request_id = NULLIF($2, '')::uuid,
		    provider_call_id = NULLIF($3, '')::uuid,
		    provider_model_id = COALESCE(NULLIF($4, '')::uuid, provider_model_id),
		    output_content_hash = NULLIF($5, ''),
		    completed_at = now()
		WHERE id = $1 AND status IN ('running', 'succeeded')
	`, callID, providerRequestID, providerCallID, providerModelID, outputHash)
	return err
}

func (r *Repository) FailScriptDerivationAttemptCall(
	ctx context.Context,
	tx pgx.Tx,
	callID string,
	errorCode string,
	errorMessage string,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempt_calls
		SET status = 'failed', error_code = $2, error_message = $3,
		    completed_at = now()
		WHERE id = $1 AND status = 'running'
	`, callID, errorCode, errorMessage)
	return err
}

func (r *Repository) MaterializeScriptDerivationItem(
	ctx context.Context,
	tx pgx.Tx,
	batch ScriptDerivationBatch,
	item ScriptDerivationItem,
	title string,
	content string,
) (ScriptUnit, ScriptVersion, error) {
	if item.Status == "succeeded" && item.OutputScriptUnitID != nil && item.OutputScriptVersionID != nil {
		unit, err := r.LoadScriptUnit(ctx, tx, item.OrganizationID, item.ProjectID, *item.OutputScriptUnitID, false)
		if err != nil {
			return ScriptUnit{}, ScriptVersion{}, err
		}
		version, err := r.LoadScriptVersion(ctx, tx, item.OrganizationID, item.ProjectID, unit.ID, *item.OutputScriptVersionID)
		return unit, version, err
	}
	if item.Status != "running" && item.Status != "reviewing" {
		return ScriptUnit{}, ScriptVersion{}, Error{Code: CodeScriptDerivationState, Message: "脚本裂变条目当前不能写入结果"}
	}
	product, found, err := r.LockProduct(ctx, tx, item.OrganizationID, item.ProjectID)
	if err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	if !found {
		return ScriptUnit{}, ScriptVersion{}, Error{Code: CodeProductRequired, Message: "商品资料不存在"}
	}
	source, err := r.LoadScriptUnit(ctx, tx, item.OrganizationID, item.ProjectID, batch.SourceScriptUnitID, true)
	if err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = source.Title + " · " + item.VariationLabel
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ScriptUnit{}, ScriptVersion{}, Error{Code: CodeScriptDerivationInvalid, Message: "裂变脚本正文为空"}
	}
	derivedFrom := source.ID
	kind := scriptDerivationKind(batch.Dimension)
	input := CreateScriptUnitInput{
		Title: title, Content: content, LanguageMode: source.LanguageMode,
		ExplicitTargetLanguage:  source.ExplicitTargetLanguage,
		TargetDurationSeconds:   source.TargetDurationSeconds,
		TargetPlatform:          source.TargetPlatform,
		DerivedFromScriptUnitID: &derivedFrom, DerivationKind: &kind,
	}
	if batch.Dimension == "language" {
		targetLanguage, err := NormalizeLocale(item.VariationKey)
		if err != nil {
			return ScriptUnit{}, ScriptVersion{}, err
		}
		input.LanguageMode = "explicit"
		input.ExplicitTargetLanguage = &targetLanguage
	}
	if err := normalizeScriptUnitInput(&input); err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	unit, err := r.InsertReservedScriptUnit(ctx, tx, product, ScriptUnitPosition{
		UnitNo: item.ReservedUnitNo, SortOrder: item.ReservedSortOrder,
	}, input, pointerText(batch.CreatedBy))
	if err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	version, err := r.InsertScriptVersion(ctx, tx, unit, content, nil, nil, false, pointerText(batch.CreatedBy))
	if err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	unit, err = r.ActivateScriptVersion(ctx, tx, unit, version)
	if err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	derivationMetadata := mustJSON(map[string]any{
		"batchId":            batch.ID,
		"rootBatchId":        pointerText(batch.RootBatchID),
		"itemId":             item.ID,
		"sourceScriptUnitId": source.ID,
		"sourceScriptTitle":  source.Title,
		"dimension":          batch.Dimension,
		"variationKey":       item.VariationKey,
		"variationLabel":     item.VariationLabel,
		"variationBrief":     item.VariationBrief,
	})
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_units
		SET metadata = COALESCE(metadata, '{}'::jsonb) ||
		    jsonb_build_object('scriptDerivation', $2::jsonb)
		WHERE id = $1
	`, unit.ID, derivationMetadata); err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	outputHash := hashText(content)
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_items
		SET status = 'succeeded', output_script_unit_id = $2,
		    output_script_version_id = $3, error_code = NULL, error_message = NULL,
		    completed_at = now(), revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status IN ('running', 'reviewing')
	`, item.ID, unit.ID, version.ID); err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempts
		SET status = 'succeeded', final_output_content_hash = $2,
		    completed_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('generating', 'reviewing', 'revising')
	`, pointerText(item.CurrentAttemptID), outputHash); err != nil {
		return ScriptUnit{}, ScriptVersion{}, err
	}
	unit, err = r.LoadScriptUnit(
		ctx, tx, item.OrganizationID, item.ProjectID, unit.ID, false,
	)
	return unit, version, err
}

func (r *Repository) FailScriptDerivationItem(
	ctx context.Context,
	tx pgx.Tx,
	item ScriptDerivationItem,
	retryable bool,
	errorCode string,
	errorMessage string,
) error {
	status := "failed_terminal"
	if retryable {
		status = "failed_retryable"
	}
	if strings.TrimSpace(errorCode) == "" {
		errorCode = CodeScriptDerivationInvalid
	}
	if strings.TrimSpace(errorMessage) == "" {
		errorMessage = "脚本裂变条目执行失败"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_items
		SET status = $2, error_code = $3, error_message = $4,
		    completed_at = now(), revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'running', 'reviewing')
	`, item.ID, status, errorCode, errorMessage); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempts
		SET status = 'failed', error_code = $2, error_message = $3,
		    completed_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'generating', 'reviewing', 'revising')
	`, pointerText(item.CurrentAttemptID), errorCode, errorMessage)
	return err
}

func (r *Repository) CancelScriptDerivationBatch(
	ctx context.Context,
	tx pgx.Tx,
	batch ScriptDerivationBatch,
) error {
	if batch.Status == "succeeded" || batch.Status == "partial_succeeded" ||
		batch.Status == "failed" || batch.Status == "cancelled" {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_batches
		SET status = 'cancelling', revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'running')
	`, batch.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_items
		SET status = 'cancelled', completed_at = now(),
		    revision = revision + 1, updated_at = now()
		WHERE batch_id = $1 AND status IN ('queued', 'running', 'reviewing')
	`, batch.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_attempts
		SET status = 'cancelled', completed_at = now(), updated_at = now()
		WHERE batch_id = $1 AND status IN ('queued', 'generating', 'reviewing', 'revising')
	`, batch.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_batches
		SET status = 'cancelled', queued_count = 0, running_count = 0,
		    cancelled_count = (
		        SELECT count(*) FROM commerce_script_derivation_items
		        WHERE batch_id = $1 AND status = 'cancelled'
		    ),
		    completed_at = now(), cancelled_at = now(),
		    revision = revision + 1, updated_at = now()
		WHERE id = $1 AND status IN ('queued', 'running', 'cancelling')
	`, batch.ID)
	return err
}

func (r *Repository) ReconcileScriptDerivationBatch(
	ctx context.Context,
	tx pgx.Tx,
	batch ScriptDerivationBatch,
) (ScriptDerivationBatch, error) {
	var queued, running, succeeded, retryable, terminal, cancelled int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'queued'),
		       count(*) FILTER (WHERE status IN ('running', 'reviewing')),
		       count(*) FILTER (WHERE status = 'succeeded'),
		       count(*) FILTER (WHERE status = 'failed_retryable'),
		       count(*) FILTER (WHERE status = 'failed_terminal'),
		       count(*) FILTER (WHERE status = 'cancelled')
		FROM commerce_script_derivation_items
		WHERE batch_id = $1
	`, batch.ID).Scan(&queued, &running, &succeeded, &retryable, &terminal, &cancelled); err != nil {
		return ScriptDerivationBatch{}, err
	}
	status := "running"
	completed := false
	if queued+running == 0 {
		completed = true
		switch {
		case succeeded == batch.RequestedCount:
			status = "succeeded"
		case succeeded > 0:
			status = "partial_succeeded"
		case cancelled == batch.RequestedCount:
			status = "cancelled"
		default:
			status = "failed"
		}
	} else if batch.Status == "cancelling" {
		status = "cancelling"
	}
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_derivation_batches
		SET status = $2, queued_count = $3, running_count = $4,
		    succeeded_count = $5, failed_retryable_count = $6,
		    failed_terminal_count = $7, cancelled_count = $8,
		    completed_at = CASE WHEN $9 THEN COALESCE(completed_at, now()) ELSE NULL END,
		    cancelled_at = CASE WHEN $2 = 'cancelled' THEN COALESCE(cancelled_at, now()) ELSE cancelled_at END,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, batch.ID, status, queued, running, succeeded, retryable, terminal, cancelled, completed)
	if err != nil {
		return ScriptDerivationBatch{}, err
	}
	return r.LoadScriptDerivationBatch(ctx, tx, batch.OrganizationID, batch.ProjectID, batch.ID, true)
}

func (r *Repository) ListScriptDerivationAttempts(
	ctx context.Context,
	db rowsQuerier,
	itemID string,
) ([]ScriptDerivationAttempt, error) {
	rows, err := db.Query(ctx, scriptDerivationAttemptSelectSQL+`
		WHERE attempt.item_id = $1
		ORDER BY attempt.attempt_no, attempt.id
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptDerivationAttempt, 0)
	for rows.Next() {
		item, err := scanScriptDerivationAttempt(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListScriptDerivationAttemptCalls(
	ctx context.Context,
	db rowsQuerier,
	attemptID string,
) ([]ScriptDerivationAttemptCall, error) {
	rows, err := db.Query(ctx, scriptDerivationAttemptCallSelectSQL+`
		WHERE call.attempt_id = $1
		ORDER BY call.round_no, CASE call.phase WHEN 'generate' THEN 1 WHEN 'review' THEN 2 ELSE 3 END
	`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptDerivationAttemptCall, 0)
	for rows.Next() {
		item, err := scanScriptDerivationAttemptCall(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadScriptDerivationDetail(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	batchID string,
	includeLineage bool,
) (ScriptDerivationBatch, error) {
	item, err := r.LoadScriptDerivationBatch(ctx, db, organizationID, projectID, batchID, false)
	if err != nil {
		return ScriptDerivationBatch{}, err
	}
	item.Items, err = r.ListScriptDerivationItems(ctx, db, item.ID)
	if err != nil {
		return ScriptDerivationBatch{}, err
	}
	for index := range item.Items {
		if err := r.loadScriptDerivationItemHistory(ctx, db, &item.Items[index]); err != nil {
			return ScriptDerivationBatch{}, err
		}
	}
	if includeLineage {
		rootID := item.ID
		if item.RootBatchID != nil {
			rootID = *item.RootBatchID
		}
		rows, queryErr := db.Query(ctx, `
			SELECT id::text, root_batch_id::text, retry_of_batch_id::text, retry_depth,
			       status, succeeded_count, failed_retryable_count,
			       failed_terminal_count, cancelled_count
			FROM commerce_script_derivation_batches
			WHERE id = $1 OR root_batch_id = $1
			ORDER BY retry_depth, created_at, id
		`, rootID)
		if queryErr != nil {
			return ScriptDerivationBatch{}, queryErr
		}
		defer rows.Close()
		for rows.Next() {
			var summary ScriptDerivationBatchSummary
			var root, retry pgtype.Text
			if err := rows.Scan(
				&summary.ID, &root, &retry, &summary.RetryDepth, &summary.Status,
				&summary.SucceededCount, &summary.FailedRetryableCount,
				&summary.FailedTerminalCount, &summary.CancelledCount,
			); err != nil {
				return ScriptDerivationBatch{}, err
			}
			summary.RootBatchID = textPointer(root)
			summary.RetryOfBatchID = textPointer(retry)
			item.Lineage = append(item.Lineage, summary)
		}
		if err := rows.Err(); err != nil {
			return ScriptDerivationBatch{}, err
		}
		item.LineageResults, err = r.loadScriptDerivationLineageResults(
			ctx, db, rootID,
		)
		if err != nil {
			return ScriptDerivationBatch{}, err
		}
	}
	return item, nil
}

func (r *Repository) loadScriptDerivationItemHistory(
	ctx context.Context,
	db rowsQuerier,
	item *ScriptDerivationItem,
) error {
	attempts, err := r.ListScriptDerivationAttempts(ctx, db, item.ID)
	if err != nil {
		return err
	}
	for index := range attempts {
		attempts[index].Calls, err = r.ListScriptDerivationAttemptCalls(
			ctx, db, attempts[index].ID,
		)
		if err != nil {
			return err
		}
	}
	item.Attempts = attempts
	return nil
}

func (r *Repository) loadScriptDerivationLineageResults(
	ctx context.Context,
	db rowsQuerier,
	rootBatchID string,
) ([]ScriptDerivationLineageResult, error) {
	batches, err := db.Query(ctx, `
		SELECT id::text
		FROM commerce_script_derivation_batches
		WHERE id = $1 OR root_batch_id = $1
		ORDER BY retry_depth, created_at, id
	`, rootBatchID)
	if err != nil {
		return nil, err
	}
	batchIDs := make([]string, 0)
	for batches.Next() {
		var batchID string
		if err := batches.Scan(&batchID); err != nil {
			batches.Close()
			return nil, err
		}
		batchIDs = append(batchIDs, batchID)
	}
	batches.Close()
	if err := batches.Err(); err != nil {
		return nil, err
	}

	results := make([]ScriptDerivationLineageResult, 0)
	indexByKey := make(map[string]int)
	for _, batchID := range batchIDs {
		items, err := r.ListScriptDerivationItems(ctx, db, batchID)
		if err != nil {
			return nil, err
		}
		for itemIndex := range items {
			if err := r.loadScriptDerivationItemHistory(ctx, db, &items[itemIndex]); err != nil {
				return nil, err
			}
			current := items[itemIndex]
			resultIndex, exists := indexByKey[current.VariationKey]
			if !exists {
				rootItemID := current.ID
				if current.RootItemID != nil {
					rootItemID = *current.RootItemID
				}
				resultIndex = len(results)
				indexByKey[current.VariationKey] = resultIndex
				results = append(results, ScriptDerivationLineageResult{
					VariationKey: current.VariationKey, VariationLabel: current.VariationLabel,
					RootItemID: rootItemID, LatestResult: current,
					Items: []ScriptDerivationItem{current},
				})
				continue
			}
			result := &results[resultIndex]
			result.Items = append(result.Items, current)
			if current.Status == "succeeded" || result.LatestResult.Status != "succeeded" {
				result.LatestResult = current
			}
		}
	}
	return results, nil
}

const scriptDerivationBatchSelectSQL = `
	SELECT batch.id::text, batch.organization_id::text, batch.project_id::text,
	       batch.product_id::text, batch.source_script_unit_id::text,
	       batch.source_content_snapshot, batch.source_content_hash,
	       batch.product_version_id::text, batch.product_snapshot_hash,
	       batch.production_generation_id::text, batch.video_production_binding_id::text,
	       batch.video_production_binding_revision, batch.production_configuration_hash,
	       batch.script_model_profile_key, batch.model_profile_binding_id::text,
	       batch.model_profile_binding_revision, batch.provider_model_id::text,
	       batch.routing_snapshot_hash, batch.prompt_contract_snapshot,
	       batch.dimension, batch.instruction, batch.preserve_contract,
	       batch.variation_plan, batch.requested_count, batch.root_batch_id::text,
	       batch.retry_of_batch_id::text, batch.retry_depth, batch.workflow_run_id::text,
	       batch.status, batch.queued_count, batch.running_count, batch.succeeded_count,
	       batch.failed_retryable_count, batch.failed_terminal_count, batch.cancelled_count,
	       batch.revision, batch.created_by::text, batch.created_at, batch.started_at,
	       batch.completed_at, batch.cancelled_at, batch.updated_at
	FROM commerce_script_derivation_batches batch`

const scriptDerivationItemSelectSQL = `
	SELECT item.id::text, item.batch_id::text, item.organization_id::text,
	       item.project_id::text, item.product_id::text, item.input_ordinal,
	       item.root_item_id::text, item.retry_of_item_id::text, item.variation_key,
	       item.variation_label, item.variation_brief, item.input_snapshot,
	       item.input_hash, item.reserved_unit_no, item.reserved_sort_order,
	       item.status, item.current_attempt_id::text, item.output_script_unit_id::text,
	       item.output_script_version_id::text, item.error_code, item.error_message,
	       item.revision, item.created_at, item.started_at, item.completed_at, item.updated_at
	FROM commerce_script_derivation_items item`

const scriptDerivationAttemptSelectSQL = `
	SELECT attempt.id::text, attempt.batch_id::text, attempt.item_id::text,
	       attempt.attempt_no, attempt.root_attempt_id::text,
	       attempt.retry_of_attempt_id::text, attempt.status,
	       attempt.final_output_content_hash, attempt.review_round,
	       attempt.review_result, attempt.review_feedback, attempt.error_code,
	       attempt.error_message, attempt.started_at, attempt.completed_at,
	       attempt.created_at, attempt.updated_at
	FROM commerce_script_derivation_attempts attempt`

const scriptDerivationAttemptCallSelectSQL = `
	SELECT call.id::text, call.batch_id::text, call.item_id::text,
	       call.attempt_id::text, call.organization_id::text,
	       call.project_id::text, call.product_id::text, call.round_no, call.phase,
	       call.provider_request_id::text, call.provider_call_id::text,
	       call.model_profile_key, call.model_profile_binding_id::text,
	       call.provider_model_id::text, call.prompt_template_key,
	       call.prompt_version_id::text, call.prompt_hash,
	       call.output_content_hash, call.status, call.error_code,
	       call.error_message, call.started_at, call.completed_at, call.created_at
	FROM commerce_script_derivation_attempt_calls call`

func scanScriptDerivationBatch(row scanRow) (ScriptDerivationBatch, error) {
	var item ScriptDerivationBatch
	var modelBinding, providerModel, rootBatch, retryBatch, workflowRun, createdBy pgtype.Text
	var promptContract, preserve, variations json.RawMessage
	var startedAt, completedAt, cancelledAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.SourceScriptUnitID, &item.SourceContentSnapshot, &item.SourceContentHash,
		&item.ProductVersionID, &item.ProductSnapshotHash, &item.ProductionGenerationID,
		&item.VideoProductionBindingID, &item.VideoProductionBindingRevision,
		&item.ProductionConfigurationHash, &item.ScriptModelProfileKey, &modelBinding,
		&item.ModelProfileBindingRevision, &providerModel, &item.RoutingSnapshotHash,
		&promptContract, &item.Dimension, &item.Instruction, &preserve, &variations,
		&item.RequestedCount, &rootBatch, &retryBatch, &item.RetryDepth, &workflowRun,
		&item.Status, &item.QueuedCount, &item.RunningCount, &item.SucceededCount,
		&item.FailedRetryableCount, &item.FailedTerminalCount, &item.CancelledCount,
		&item.Revision, &createdBy, &item.CreatedAt, &startedAt, &completedAt,
		&cancelledAt, &item.UpdatedAt,
	)
	if err != nil {
		return ScriptDerivationBatch{}, err
	}
	if err := json.Unmarshal(promptContract, &item.PromptContract); err != nil {
		return ScriptDerivationBatch{}, err
	}
	if err := json.Unmarshal(preserve, &item.Preserve); err != nil {
		return ScriptDerivationBatch{}, err
	}
	if err := json.Unmarshal(variations, &item.Variations); err != nil {
		return ScriptDerivationBatch{}, err
	}
	item.ModelProfileBindingID = textPointer(modelBinding)
	item.ProviderModelID = textPointer(providerModel)
	item.RootBatchID = textPointer(rootBatch)
	item.RetryOfBatchID = textPointer(retryBatch)
	item.WorkflowRunID = textPointer(workflowRun)
	item.CreatedBy = textPointer(createdBy)
	item.StartedAt = timestampPointer(startedAt)
	item.CompletedAt = timestampPointer(completedAt)
	item.CancelledAt = timestampPointer(cancelledAt)
	return item, nil
}

func scanScriptDerivationItem(row scanRow) (ScriptDerivationItem, error) {
	var item ScriptDerivationItem
	var rootItem, retryItem, attempt, outputUnit, outputVersion, errorCode, errorMessage pgtype.Text
	var startedAt, completedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.BatchID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.InputOrdinal, &rootItem, &retryItem, &item.VariationKey,
		&item.VariationLabel, &item.VariationBrief, &item.InputSnapshot,
		&item.InputHash, &item.ReservedUnitNo, &item.ReservedSortOrder,
		&item.Status, &attempt, &outputUnit, &outputVersion, &errorCode,
		&errorMessage, &item.Revision, &item.CreatedAt, &startedAt,
		&completedAt, &item.UpdatedAt,
	)
	item.RootItemID = textPointer(rootItem)
	item.RetryOfItemID = textPointer(retryItem)
	item.CurrentAttemptID = textPointer(attempt)
	item.OutputScriptUnitID = textPointer(outputUnit)
	item.OutputScriptVersionID = textPointer(outputVersion)
	item.ErrorCode = textPointer(errorCode)
	item.ErrorMessage = textPointer(errorMessage)
	item.StartedAt = timestampPointer(startedAt)
	item.CompletedAt = timestampPointer(completedAt)
	return item, err
}

func scanScriptDerivationAttempt(row scanRow) (ScriptDerivationAttempt, error) {
	var item ScriptDerivationAttempt
	var root, retry, finalHash, feedback, errorCode, errorMessage pgtype.Text
	var startedAt, completedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.BatchID, &item.ItemID, &item.AttemptNo, &root, &retry,
		&item.Status, &finalHash, &item.ReviewRound, &item.ReviewResult,
		&feedback, &errorCode, &errorMessage, &startedAt, &completedAt,
		&item.CreatedAt, &item.UpdatedAt,
	)
	item.RootAttemptID = textPointer(root)
	item.RetryOfAttemptID = textPointer(retry)
	item.FinalOutputContentHash = textPointer(finalHash)
	item.ReviewFeedback = textPointer(feedback)
	item.ErrorCode = textPointer(errorCode)
	item.ErrorMessage = textPointer(errorMessage)
	item.StartedAt = timestampPointer(startedAt)
	item.CompletedAt = timestampPointer(completedAt)
	return item, err
}

func scanScriptDerivationAttemptCall(row scanRow) (ScriptDerivationAttemptCall, error) {
	var item ScriptDerivationAttemptCall
	var providerRequest, providerCall, modelBinding, providerModel pgtype.Text
	var outputHash, errorCode, errorMessage pgtype.Text
	var completedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.BatchID, &item.ItemID, &item.AttemptID,
		&item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.RoundNo, &item.Phase, &providerRequest, &providerCall,
		&item.ModelProfileKey, &modelBinding, &providerModel,
		&item.PromptTemplateKey, &item.PromptVersionID, &item.PromptHash,
		&outputHash, &item.Status, &errorCode, &errorMessage,
		&item.StartedAt, &completedAt, &item.CreatedAt,
	)
	item.ProviderRequestID = textPointer(providerRequest)
	item.ProviderCallID = textPointer(providerCall)
	item.ModelProfileBindingID = textPointer(modelBinding)
	item.ProviderModelID = textPointer(providerModel)
	item.OutputContentHash = textPointer(outputHash)
	item.ErrorCode = textPointer(errorCode)
	item.ErrorMessage = textPointer(errorMessage)
	item.CompletedAt = timestampPointer(completedAt)
	return item, err
}
