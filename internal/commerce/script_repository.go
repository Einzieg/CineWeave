package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) LoadScriptUnit(ctx context.Context, db rowQuerier, organizationID, projectID, unitID string, lock bool) (ScriptUnit, error) {
	query := scriptUnitSelectSQL + ` WHERE unit.id = $1 AND unit.organization_id = $2 AND unit.project_id = $3`
	if lock {
		query += ` FOR UPDATE OF unit`
	}
	return scanScriptUnit(db.QueryRow(ctx, query, unitID, organizationID, projectID))
}

func (r *Repository) ListScriptUnits(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	status string,
	cursorSort int64,
	cursorID string,
	limit int,
) ([]ScriptUnit, error) {
	query := scriptUnitSelectSQL + ` WHERE unit.organization_id = $1 AND unit.project_id = $2`
	args := []any{organizationID, projectID}
	if status == "active" {
		query += ` AND unit.status <> 'archived'`
	} else if status != "all" {
		query += fmt.Sprintf(` AND unit.status = $%d`, len(args)+1)
		args = append(args, status)
	}
	if cursorID != "" {
		query += fmt.Sprintf(` AND (unit.sort_order, unit.id) > ($%d, $%d::uuid)`, len(args)+1, len(args)+2)
		args = append(args, cursorSort, cursorID)
	}
	query += fmt.Sprintf(` ORDER BY unit.sort_order, unit.id LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptUnit, 0, limit)
	for rows.Next() {
		item, err := scanScriptUnit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) InsertScriptUnit(
	ctx context.Context,
	tx pgx.Tx,
	product Product,
	input CreateScriptUnitInput,
	createdBy string,
) (ScriptUnit, error) {
	var unitNo, sortOrder int64
	if err := tx.QueryRow(ctx, `
		SELECT product.next_script_unit_no,
		       COALESCE((SELECT max(sort_order) FROM commerce_script_units WHERE product_id = product.id AND status <> 'archived'), 0) + 10
		FROM commerce_products product
		WHERE product.id = $1
		FOR UPDATE
	`, product.ID).Scan(&unitNo, &sortOrder); err != nil {
		return ScriptUnit{}, err
	}
	draftHash := nullableTextHash(input.Content)
	var unitID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO commerce_script_units(
			organization_id, project_id, product_id, unit_no, title, sort_order,
			language_mode, explicit_target_language, target_duration_seconds,
			target_platform, draft_content, draft_content_hash, draft_updated_at,
			derived_from_script_unit_id, derivation_kind, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11,
		        NULLIF($12, ''), CASE WHEN $11 = '' THEN NULL ELSE now() END,
		        NULLIF($13, '')::uuid, NULLIF($14, ''), $15)
		RETURNING id::text
	`, product.OrganizationID, product.ProjectID, product.ID, unitNo, input.Title, sortOrder,
		input.LanguageMode, optionalString(input.ExplicitTargetLanguage), input.TargetDurationSeconds,
		input.TargetPlatform, input.Content, draftHash, optionalString(input.DerivedFromScriptUnitID),
		optionalString(input.DerivationKind), createdBy).Scan(&unitID); err != nil {
		return ScriptUnit{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET next_script_unit_no = next_script_unit_no + 1,
		    script_units_revision = script_units_revision + 1,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, product.ID); err != nil {
		return ScriptUnit{}, err
	}
	return r.LoadScriptUnit(ctx, tx, product.OrganizationID, product.ProjectID, unitID, true)
}

func (r *Repository) UpdateScriptUnit(
	ctx context.Context,
	tx pgx.Tx,
	current ScriptUnit,
	input UpdateScriptUnitInput,
) (ScriptUnit, error) {
	title := current.Title
	if input.Title != nil {
		title = *input.Title
	}
	draft := current.DraftContent
	if input.DraftContent != nil {
		draft = *input.DraftContent
	}
	languageMode := current.LanguageMode
	if input.LanguageMode != nil {
		languageMode = *input.LanguageMode
	}
	targetLanguage := current.ExplicitTargetLanguage
	if input.LanguageMode != nil || input.ExplicitTargetLanguage != nil {
		targetLanguage = input.ExplicitTargetLanguage
	}
	duration := current.TargetDurationSeconds
	if input.TargetDurationSeconds != nil {
		duration = *input.TargetDurationSeconds
	}
	platform := current.TargetPlatform
	if input.TargetPlatform != nil {
		platform = *input.TargetPlatform
	}
	return scanScriptUnit(tx.QueryRow(ctx, `
		UPDATE commerce_script_units unit
		SET title = $4, draft_content = $5, draft_content_hash = NULLIF($6, ''),
		    draft_updated_at = CASE WHEN $5 = '' THEN NULL ELSE now() END,
		    language_mode = $7, explicit_target_language = NULLIF($8, ''),
		    target_duration_seconds = $9, target_platform = $10,
		    revision = revision + 1, updated_at = now()
		WHERE unit.id = $1 AND unit.organization_id = $2 AND unit.project_id = $3 AND unit.revision = $11
		RETURNING `+scriptUnitReturningColumns+`
	`, current.ID, current.OrganizationID, current.ProjectID, title, draft, nullableTextHash(draft),
		languageMode, optionalString(targetLanguage), duration, platform, current.Revision))
}

func (r *Repository) ArchiveScriptUnit(ctx context.Context, tx pgx.Tx, current ScriptUnit) (ScriptUnit, error) {
	item, err := scanScriptUnit(tx.QueryRow(ctx, `
		UPDATE commerce_script_units unit
		SET status = 'archived', archived_at = now(), revision = revision + 1, updated_at = now()
		WHERE unit.id = $1 AND unit.revision = $2
		RETURNING `+scriptUnitReturningColumns+`
	`, current.ID, current.Revision))
	if err != nil {
		return ScriptUnit{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_products
		SET script_units_revision = script_units_revision + 1,
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
	`, current.ProductID); err != nil {
		return ScriptUnit{}, err
	}
	return item, nil
}

func (r *Repository) ReorderScriptUnits(ctx context.Context, tx pgx.Tx, product Product, expectedRevision int64, items []ReorderScriptUnitItem) (int64, error) {
	if product.ScriptUnitsRevision != expectedRevision {
		return 0, Error{Code: CodeScriptUnitRevision, Message: "脚本列表已变化，请刷新后重试"}
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text FROM commerce_script_units
		WHERE product_id = $1 AND status <> 'archived'
		ORDER BY id FOR UPDATE
	`, product.ID)
	if err != nil {
		return 0, err
	}
	active := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		active[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(active) != len(items) {
		return 0, Error{Code: CodeScriptUnitRevision, Message: "排序请求必须包含全部活动脚本"}
	}
	seenIDs := make(map[string]bool, len(items))
	seenOrders := make(map[int64]bool, len(items))
	for _, item := range items {
		if !active[item.ScriptUnitID] || seenIDs[item.ScriptUnitID] || item.SortOrder <= 0 || seenOrders[item.SortOrder] {
			return 0, errors.New("script unit reorder set is invalid")
		}
		seenIDs[item.ScriptUnitID] = true
		seenOrders[item.SortOrder] = true
	}
	var temporaryBase int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sort_order), 0) + 1000000 FROM commerce_script_units WHERE product_id = $1`, product.ID).Scan(&temporaryBase); err != nil {
		return 0, err
	}
	for index, item := range items {
		if _, err := tx.Exec(ctx, `UPDATE commerce_script_units SET sort_order = $2 WHERE id = $1`, item.ScriptUnitID, temporaryBase+int64(index)); err != nil {
			return 0, err
		}
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `UPDATE commerce_script_units SET sort_order = $2, revision = revision + 1, updated_at = now() WHERE id = $1`, item.ScriptUnitID, item.SortOrder); err != nil {
			return 0, err
		}
	}
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE commerce_products
		SET script_units_revision = script_units_revision + 1, revision = revision + 1, updated_at = now()
		WHERE id = $1 AND script_units_revision = $2
		RETURNING script_units_revision
	`, product.ID, expectedRevision).Scan(&revision); err != nil {
		return 0, err
	}
	return revision, nil
}

func (r *Repository) InsertScriptVersion(ctx context.Context, tx pgx.Tx, unit ScriptUnit, content string, sourceLanguageHint, sourceVersionID *string, manualOverride bool, createdBy string) (ScriptVersion, error) {
	var nextVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version), 0) + 1 FROM commerce_ad_script_versions WHERE script_unit_id = $1`, unit.ID).Scan(&nextVersion); err != nil {
		return ScriptVersion{}, err
	}
	hash := hashText(content)
	item, err := scanScriptVersion(tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_versions(
			organization_id, project_id, product_id, script_unit_id, version,
			content, content_hash, source_language_hint, manual_override,
			source_version_id, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, NULLIF($10, '')::uuid, $11)
		RETURNING `+scriptVersionColumns+`
	`, unit.OrganizationID, unit.ProjectID, unit.ProductID, unit.ID, nextVersion,
		content, hash, optionalString(sourceLanguageHint), manualOverride,
		optionalString(sourceVersionID), createdBy))
	if err != nil {
		return ScriptVersion{}, err
	}
	segments := splitScriptSegments(content)
	for index, segment := range segments {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_ad_script_segments(
				organization_id, project_id, product_id, script_unit_id,
				script_version_id, segment_no, segment_kind, source_text,
				content_hash, required
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'script', $7, $8, true)
		`, unit.OrganizationID, unit.ProjectID, unit.ProductID, unit.ID,
			item.ID, index+1, segment, hashText(segment)); err != nil {
			return ScriptVersion{}, err
		}
	}
	return item, nil
}

func (r *Repository) ActivateScriptVersion(ctx context.Context, tx pgx.Tx, unit ScriptUnit, version ScriptVersion) (ScriptUnit, error) {
	if unit.ActiveUnitGenerationID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE commerce_storyboard_plans
			SET status = 'stale', active = false, stale_state = 'upstream_changed',
			    stale_at = COALESCE(stale_at, now())
			WHERE organization_id = $1 AND project_id = $2
			  AND script_unit_id = $3 AND script_unit_generation_id = $4
			  AND status <> 'archived'
		`, unit.OrganizationID, unit.ProjectID, unit.ID, *unit.ActiveUnitGenerationID); err != nil {
			return ScriptUnit{}, err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE commerce_script_unit_generations
			SET status = 'archived', archived_at = now()
			WHERE id = $1 AND script_unit_id = $2 AND status = 'active'
		`, *unit.ActiveUnitGenerationID, unit.ID)
		if err != nil {
			return ScriptUnit{}, err
		}
		if tag.RowsAffected() != 1 {
			return ScriptUnit{}, Error{
				Code: CodeGenerationMismatch, Message: "脚本旧生产代已变化，请刷新后重试",
			}
		}
	}
	return scanScriptUnit(tx.QueryRow(ctx, `
		UPDATE commerce_script_units unit
		SET current_source_version_id = $4, current_localization_id = NULL,
		    active_unit_generation_id = NULL,
		    status = 'draft',
		    draft_content = $5, draft_content_hash = $6,
		    draft_updated_at = now(), revision = revision + 1, updated_at = now()
		WHERE unit.id = $1 AND unit.organization_id = $2 AND unit.project_id = $3 AND unit.revision = $7
		RETURNING `+scriptUnitReturningColumns+`
	`, unit.ID, unit.OrganizationID, unit.ProjectID, version.ID, version.Content, version.ContentHash, unit.Revision))
}

func (r *Repository) ListScriptVersions(ctx context.Context, db rowsQuerier, organizationID, projectID, unitID string) ([]ScriptVersion, error) {
	rows, err := db.Query(ctx, `SELECT `+scriptVersionColumns+` FROM commerce_ad_script_versions
		WHERE organization_id = $1 AND project_id = $2 AND script_unit_id = $3 ORDER BY version DESC`, organizationID, projectID, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptVersion, 0)
	for rows.Next() {
		item, err := scanScriptVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadScriptVersion(ctx context.Context, db rowQuerier, organizationID, projectID, unitID, versionID string) (ScriptVersion, error) {
	return scanScriptVersion(db.QueryRow(ctx, `SELECT `+scriptVersionColumns+` FROM commerce_ad_script_versions
		WHERE id = $1 AND script_unit_id = $2 AND organization_id = $3 AND project_id = $4`, versionID, unitID, organizationID, projectID))
}

func (r *Repository) LoadLatestLanguageResolution(ctx context.Context, db rowQuerier, organizationID, projectID, unitID string) (LanguageResolution, error) {
	return scanLanguageResolution(db.QueryRow(ctx, `
		SELECT id::text, script_unit_id::text, source_script_version_id::text,
		       language_mode, source_language, target_language, confidence,
		       reasoning, needs_user_confirmation, status, input_hash,
		       xmin::text::bigint, confirmed_at, created_at, updated_at
		FROM commerce_language_resolutions
		WHERE organization_id = $1 AND project_id = $2 AND script_unit_id = $3
		ORDER BY created_at DESC LIMIT 1
	`, organizationID, projectID, unitID))
}

func (r *Repository) InsertLanguageResolution(
	ctx context.Context,
	tx pgx.Tx,
	unit ScriptUnit,
	sourceVersionID string,
	sourceLanguage *string,
	targetLanguage *string,
	confidence *float64,
	reasoning string,
	needsConfirmation bool,
	status string,
	confirmedBy *string,
	inputHash string,
) (LanguageResolution, error) {
	return scanLanguageResolution(tx.QueryRow(ctx, `
		INSERT INTO commerce_language_resolutions(
			organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_mode, source_language,
			target_language, confidence, reasoning, needs_user_confirmation,
			status, confirmed_by, confirmed_at, input_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9,
		        $10, $11, $12, NULLIF($13, '')::uuid,
		        CASE WHEN $12 = 'confirmed' THEN now() ELSE NULL END, $14)
		RETURNING id::text, script_unit_id::text, source_script_version_id::text,
		          language_mode, source_language, target_language, confidence,
		          reasoning, needs_user_confirmation, status, input_hash,
		          xmin::text::bigint, confirmed_at, created_at, updated_at
	`, unit.OrganizationID, unit.ProjectID, unit.ProductID, unit.ID,
		sourceVersionID, unit.LanguageMode, optionalString(sourceLanguage), optionalString(targetLanguage),
		confidence, reasoning, needsConfirmation, status, optionalString(confirmedBy), inputHash))
}

func (r *Repository) ConfirmLanguageResolution(ctx context.Context, tx pgx.Tx, organizationID, projectID, unitID, resolutionID, locale, confirmedBy string) (LanguageResolution, error) {
	return scanLanguageResolution(tx.QueryRow(ctx, `
		UPDATE commerce_language_resolutions
		SET target_language = $5, needs_user_confirmation = false,
		    status = 'confirmed', confirmed_by = $6, confirmed_at = now(), updated_at = now()
		WHERE id = $1 AND script_unit_id = $2 AND organization_id = $3 AND project_id = $4
		  AND status IN ('pending', 'needs_confirmation')
		RETURNING id::text, script_unit_id::text, source_script_version_id::text,
		          language_mode, source_language, target_language, confidence,
		          reasoning, needs_user_confirmation, status, input_hash,
		          xmin::text::bigint, confirmed_at, created_at, updated_at
	`, resolutionID, unitID, organizationID, projectID, locale, confirmedBy))
}

func (r *Repository) ListScriptSegments(ctx context.Context, db rowsQuerier, organizationID, projectID, unitID, versionID string) ([]ScriptSegment, error) {
	rows, err := db.Query(ctx, `
		SELECT id::text, script_version_id::text, segment_no, segment_kind,
		       source_text, content_hash, required, created_at
		FROM commerce_ad_script_segments
		WHERE organization_id = $1 AND project_id = $2 AND script_unit_id = $3 AND script_version_id = $4
		ORDER BY segment_no
	`, organizationID, projectID, unitID, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptSegment, 0)
	for rows.Next() {
		var item ScriptSegment
		if err := rows.Scan(&item.ID, &item.ScriptVersionID, &item.SegmentNo, &item.SegmentKind, &item.SourceText, &item.ContentHash, &item.Required, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) InsertLocalization(
	ctx context.Context,
	tx pgx.Tx,
	unit ScriptUnit,
	resolution LanguageResolution,
	input LocalizationInput,
	timing TimingEstimate,
	createdBy string,
) (ScriptLocalization, error) {
	var nextVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version), 0) + 1 FROM commerce_ad_script_localizations WHERE script_unit_id = $1`, unit.ID).Scan(&nextVersion); err != nil {
		return ScriptLocalization{}, err
	}
	status := "draft"
	reviewStatus := "pending"
	if input.Approve {
		status = "approved"
		reviewStatus = "approved"
	}
	timingRaw := mustJSON(timing)
	item, err := scanLocalization(tx.QueryRow(ctx, `
		INSERT INTO commerce_ad_script_localizations(
			organization_id, project_id, product_id, script_unit_id,
			source_script_version_id, language_resolution_id, version,
			source_language, target_language, localized_content,
			localized_content_hash, structured_contract,
			estimated_voiceover_seconds, timing_analysis, timing_policy_version,
			review_status, reviewer_output, status, revision, created_by,
			approved_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
		        $13, $14, $15, $16, $17, $18, 1, $19,
		        CASE WHEN $18 = 'approved' THEN now() ELSE NULL END)
		RETURNING id::text, script_unit_id::text, source_script_version_id::text,
		          language_resolution_id::text, version, source_language, target_language,
		          localized_content, localized_content_hash, structured_contract,
		          estimated_voiceover_seconds::float8, timing_analysis, timing_policy_version,
		          review_status, reviewer_output, status, revision, created_at, approved_at, archived_at
	`, unit.OrganizationID, unit.ProjectID, unit.ProductID, unit.ID,
		input.SourceScriptVersionID, resolution.ID, nextVersion, input.SourceLanguage,
		input.TargetLanguage, input.LocalizedContent, hashText(input.LocalizedContent),
		input.StructuredContract, timing.EstimatedVoiceoverSeconds, timingRaw,
		timing.PolicyVersion, reviewStatus, input.ReviewerOutput, status, createdBy))
	if err != nil {
		return ScriptLocalization{}, err
	}
	return item, nil
}

func (r *Repository) InsertLocalizationSegments(ctx context.Context, tx pgx.Tx, unit ScriptUnit, localization ScriptLocalization, segments []localizationSegmentInput) error {
	for _, segment := range segments {
		claims, err := json.Marshal(segment.ProductClaims)
		if err != nil {
			return err
		}
		features, err := json.Marshal(segment.RequiredProductFeatures)
		if err != nil {
			return err
		}
		contentHash, err := hashJSONValue(segment)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_localization_segments(
				organization_id, project_id, product_id, script_unit_id,
				source_script_version_id, localization_id, source_segment_id,
				segment_no, sales_beat, localized_text, voiceover_text,
				onscreen_text, product_claims, required_product_features, content_hash
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`, unit.OrganizationID, unit.ProjectID, unit.ProductID, unit.ID,
			localization.SourceScriptVersionID, localization.ID, segment.SourceSegmentID,
			segment.SegmentNo, segment.SalesBeat, segment.LocalizedText, segment.VoiceoverText,
			segment.OnscreenText, claims, features, contentHash); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ActivateLocalization(ctx context.Context, tx pgx.Tx, unit ScriptUnit, localization ScriptLocalization) (ScriptUnit, error) {
	return scanScriptUnit(tx.QueryRow(ctx, `
		UPDATE commerce_script_units unit
		SET current_localization_id = $4, revision = revision + 1, updated_at = now()
		WHERE unit.id = $1 AND unit.organization_id = $2 AND unit.project_id = $3 AND unit.revision = $5
		RETURNING `+scriptUnitReturningColumns+`
	`, unit.ID, unit.OrganizationID, unit.ProjectID, localization.ID, unit.Revision))
}

func (r *Repository) ListLocalizations(ctx context.Context, db rowsQuerier, organizationID, projectID, unitID string) ([]ScriptLocalization, error) {
	rows, err := db.Query(ctx, localizationSelectSQL+`
		WHERE organization_id = $1 AND project_id = $2 AND script_unit_id = $3
		ORDER BY version DESC`, organizationID, projectID, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ScriptLocalization, 0)
	for rows.Next() {
		item, err := scanLocalization(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadLocalization(ctx context.Context, db rowQuerier, organizationID, projectID, unitID, localizationID string) (ScriptLocalization, error) {
	return scanLocalization(db.QueryRow(ctx, localizationSelectSQL+`
		WHERE id = $1 AND script_unit_id = $2 AND organization_id = $3 AND project_id = $4`, localizationID, unitID, organizationID, projectID))
}

const scriptUnitReturningColumns = `
	unit.id::text, unit.organization_id::text, unit.project_id::text, unit.product_id::text,
	unit.unit_no, unit.title, unit.sort_order, unit.status,
	unit.current_source_version_id::text, unit.current_localization_id::text,
	unit.language_mode, unit.explicit_target_language, unit.target_duration_seconds,
	unit.target_platform, unit.draft_content, unit.draft_content_hash, unit.draft_updated_at,
	unit.active_unit_generation_id::text, unit.unit_generation_no,
	COALESCE((
		SELECT generation.storyboard_strategy
		FROM commerce_script_unit_generations generation
		WHERE generation.id = unit.active_unit_generation_id
	), ''),
	unit.derived_from_script_unit_id::text, unit.derivation_kind,
	unit.revision, unit.metadata, unit.created_at, unit.updated_at, unit.archived_at`

const scriptUnitSelectSQL = `SELECT ` + scriptUnitReturningColumns + ` FROM commerce_script_units unit`

const scriptVersionColumns = `
	id::text, organization_id::text, project_id::text, product_id::text,
	script_unit_id::text, version, content, content_hash, source_language_hint,
	detected_source_language, manual_override, source_version_id::text, created_at`

const localizationSelectSQL = `
	SELECT id::text, script_unit_id::text, source_script_version_id::text,
	       language_resolution_id::text, version, source_language, target_language,
	       localized_content, localized_content_hash, structured_contract,
	       estimated_voiceover_seconds::float8, timing_analysis, timing_policy_version,
	       review_status, reviewer_output, status, revision, created_at, approved_at, archived_at
	FROM commerce_ad_script_localizations `

func scanScriptUnit(row scanRow) (ScriptUnit, error) {
	var item ScriptUnit
	var currentSource, currentLocalization, targetLanguage, draftHash pgtype.Text
	var activeGeneration, derivedFrom, derivationKind pgtype.Text
	var draftUpdatedAt, archivedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.UnitNo, &item.Title, &item.SortOrder, &item.Status,
		&currentSource, &currentLocalization, &item.LanguageMode, &targetLanguage,
		&item.TargetDurationSeconds, &item.TargetPlatform, &item.DraftContent,
		&draftHash, &draftUpdatedAt, &activeGeneration, &item.UnitGenerationNo,
		&item.StoryboardStrategy,
		&derivedFrom, &derivationKind, &item.Revision, &item.Metadata,
		&item.CreatedAt, &item.UpdatedAt, &archivedAt,
	)
	item.CurrentSourceVersionID = textPointer(currentSource)
	item.CurrentLocalizationID = textPointer(currentLocalization)
	item.ExplicitTargetLanguage = textPointer(targetLanguage)
	item.DraftContentHash = textPointer(draftHash)
	item.ActiveUnitGenerationID = textPointer(activeGeneration)
	item.DerivedFromScriptUnitID = textPointer(derivedFrom)
	item.DerivationKind = textPointer(derivationKind)
	item.DraftUpdatedAt = timestampPointer(draftUpdatedAt)
	item.ArchivedAt = timestampPointer(archivedAt)
	return item, err
}

func scanScriptVersion(row scanRow) (ScriptVersion, error) {
	var item ScriptVersion
	var hint, detected, sourceVersion pgtype.Text
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.ScriptUnitID, &item.Version, &item.Content, &item.ContentHash,
		&hint, &detected, &item.ManualOverride, &sourceVersion, &item.CreatedAt,
	)
	item.SourceLanguageHint = textPointer(hint)
	item.DetectedSourceLanguage = textPointer(detected)
	item.SourceVersionID = textPointer(sourceVersion)
	return item, err
}

func scanLanguageResolution(row scanRow) (LanguageResolution, error) {
	var item LanguageResolution
	var source, target pgtype.Text
	var confidence pgtype.Numeric
	var confirmedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.ScriptUnitID, &item.SourceScriptVersionID,
		&item.LanguageMode, &source, &target, &confidence, &item.Reasoning,
		&item.NeedsUserConfirmation, &item.Status, &item.InputHash, &item.Revision,
		&confirmedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	item.SourceLanguage = textPointer(source)
	item.TargetLanguage = textPointer(target)
	if confidence.Valid {
		value, err := confidence.Float64Value()
		if err == nil && value.Valid {
			item.Confidence = &value.Float64
		}
	}
	item.ConfirmedAt = timestampPointer(confirmedAt)
	return item, err
}

func scanLocalization(row scanRow) (ScriptLocalization, error) {
	var item ScriptLocalization
	var approvedAt, archivedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID, &item.ScriptUnitID, &item.SourceScriptVersionID,
		&item.LanguageResolutionID, &item.Version, &item.SourceLanguage,
		&item.TargetLanguage, &item.LocalizedContent, &item.LocalizedContentHash,
		&item.StructuredContract, &item.EstimatedVoiceoverSeconds,
		&item.TimingAnalysis, &item.TimingPolicyVersion, &item.ReviewStatus,
		&item.ReviewerOutput, &item.Status, &item.Revision, &item.CreatedAt,
		&approvedAt, &archivedAt,
	)
	item.ApprovedAt = timestampPointer(approvedAt)
	item.ArchivedAt = timestampPointer(archivedAt)
	return item, err
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timestampPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	copy := value.Time
	return &copy
}

func nullableTextHash(value string) string {
	if value == "" {
		return ""
	}
	return hashText(value)
}
