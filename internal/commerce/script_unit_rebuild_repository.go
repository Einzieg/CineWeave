package commerce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

type persistedScriptUnitRebuild struct {
	ID                          string
	OrganizationID              string
	ProjectID                   string
	ProductID                   string
	ScriptUnitID                string
	ProjectGenerationID         string
	SourceUnitGenerationID      string
	SourceUnitConfigurationHash string
	SourceScriptVersionID       string
	SourceLocalizationID        string
	TargetSourceScriptVersionID string
	TargetLanguageMode          string
	TargetLanguage              *string
	TargetDurationSeconds       int
	TargetPlatform              string
	TargetConfiguration         json.RawMessage
	TargetConfigurationHash     string
	ImpactSnapshot              json.RawMessage
	ImpactToken                 string
	ExpectedRevision            int64
	Status                      string
	IdempotencyKey              string
	WorkflowRunID               string
	TargetLocalizationID        string
	TargetUnitGenerationID      string
}

func (r *Repository) LoadScriptUnitRebuildAffectedCounts(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	unitGenerationID string,
) (ScriptUnitRebuildAffectedCounts, error) {
	var item ScriptUnitRebuildAffectedCounts
	err := db.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM commerce_storyboard_plans
		   WHERE organization_id = $1 AND project_id = $2 AND script_unit_generation_id = $3),
		  (SELECT count(*)
		   FROM storyboard_shots shot
		   JOIN commerce_storyboard_plans plan
		     ON plan.id = shot.commerce_storyboard_plan_id
		    AND plan.organization_id = shot.organization_id
		    AND plan.project_id = shot.project_id
		   WHERE shot.organization_id = $1 AND shot.project_id = $2
		     AND plan.script_unit_generation_id = $3 AND shot.deleted_at IS NULL),
		  (SELECT count(*) FROM commerce_shot_image_versions
		   WHERE organization_id = $1 AND project_id = $2 AND script_unit_generation_id = $3),
		  (SELECT count(*) FROM video_prompt_plans
		   WHERE organization_id = $1 AND project_id = $2 AND commerce_script_unit_generation_id = $3),
		  (SELECT count(*) FROM video_render_plans
		   WHERE organization_id = $1 AND project_id = $2 AND commerce_script_unit_generation_id = $3),
		  (SELECT count(*) FROM project_timelines
		   WHERE organization_id = $1 AND project_id = $2 AND commerce_script_unit_generation_id = $3),
		  (SELECT count(*) FROM final_video_versions
		   WHERE organization_id = $1 AND project_id = $2 AND commerce_script_unit_generation_id = $3)
	`, organizationID, projectID, unitGenerationID).Scan(
		&item.StoryboardPlans, &item.StoryboardShots, &item.ReferenceImages,
		&item.VideoPrompts, &item.ShotVideos, &item.Timelines, &item.FinalVideos,
	)
	return item, err
}

func (r *Repository) LoadScriptUnitRebuildBlockers(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	unitGenerationID string,
) ([]string, error) {
	var workflowCount, runCount, providerTaskCount int
	err := db.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM workflow_runs
		   WHERE organization_id = $1 AND project_id = $2
		     AND status IN ('queued', 'running', 'waiting_review', 'cancelling')
		     AND input->'identity'->>'scriptUnitGenerationId' = $3::text),
		  (SELECT count(*) FROM commerce_production_runs
		   WHERE organization_id = $1 AND project_id = $2
		     AND script_unit_generation_id = $3::uuid
		     AND status IN ('queued', 'running', 'cancelling')),
		  (SELECT count(*)
		   FROM provider_async_tasks task
		   JOIN workflow_runs run ON run.id = task.workflow_run_id
		   WHERE run.organization_id = $1 AND run.project_id = $2
		     AND run.input->'identity'->>'scriptUnitGenerationId' = $3::text
		     AND task.status IN ('queued', 'running', 'cancelling'))
	`, organizationID, projectID, unitGenerationID).Scan(&workflowCount, &runCount, &providerTaskCount)
	if err != nil {
		return nil, err
	}
	blockers := make([]string, 0, 3)
	if workflowCount > 0 {
		blockers = append(blockers, "该脚本仍有进行中的工作流")
	}
	if runCount > 0 {
		blockers = append(blockers, "该脚本仍有进行中的生产任务")
	}
	if providerTaskCount > 0 {
		blockers = append(blockers, "该脚本仍有进行中的供应商异步任务")
	}
	return blockers, nil
}

func (r *Repository) SupersedePlannedScriptUnitRebuilds(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_rebuilds
		SET status = 'cancelled', completed_at = now(),
		    error_code = 'IMPACT_SUPERSEDED', error_message = '已生成新的影响分析'
		WHERE organization_id = $1 AND project_id = $2 AND script_unit_id = $3
		  AND status = 'planned'
	`, organizationID, projectID, unitID)
	return err
}

func (r *Repository) InsertPlannedScriptUnitRebuild(
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
	unit ScriptUnit,
	generation UnitGenerationContext,
	target ScriptUnitRebuildTarget,
	targetConfiguration json.RawMessage,
	targetConfigurationHash string,
	impactSnapshot json.RawMessage,
	impactToken string,
	requestedBy string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_unit_rebuilds(
			organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, source_unit_generation_id,
			source_unit_configuration_hash, source_script_version_id,
			source_localization_id, target_source_script_version_id,
			target_language_mode, target_explicit_language,
			target_duration_seconds, target_platform,
			target_configuration_snapshot, target_configuration_hash,
			impact_snapshot, impact_token, impact_expires_at,
			expected_script_unit_revision, status, idempotency_key, requested_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		        $14, $15, $16, $17, $18, (($17::jsonb)->>'expiresAt')::timestamptz,
		        $19, 'planned', $20, $21)
	`, production.OrganizationID, production.ProjectID, unit.ProductID, unit.ID,
		production.Generation.ID, generation.Identity.UnitGenerationID,
		generation.Identity.UnitConfigurationHash, generation.SourceScriptVersionID,
		generation.LocalizationID, target.TargetSourceScriptVersionID,
		target.TargetLanguageMode, target.TargetLanguage, target.TargetDurationSeconds,
		target.TargetPlatform, targetConfiguration, targetConfigurationHash,
		impactSnapshot, impactToken, unit.Revision, "impact:"+impactToken, requestedBy)
	return err
}

func (r *Repository) LockScriptUnitRebuildByToken(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	token string,
) (persistedScriptUnitRebuild, error) {
	return scanPersistedScriptUnitRebuild(tx.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       script_unit_id::text, project_production_generation_id::text,
		       source_unit_generation_id::text, source_unit_configuration_hash,
		       source_script_version_id::text, source_localization_id::text,
		       target_source_script_version_id::text, target_language_mode,
		       target_explicit_language, target_duration_seconds, target_platform,
		       target_configuration_snapshot, target_configuration_hash,
		       impact_snapshot, impact_token, expected_script_unit_revision,
		       status, idempotency_key, COALESCE(workflow_run_id::text, ''),
		       COALESCE(target_localization_id::text, ''),
		       COALESCE(target_unit_generation_id::text, '')
		FROM commerce_script_unit_rebuilds
		WHERE organization_id = $1 AND project_id = $2
		  AND script_unit_id = $3 AND impact_token = $4
		FOR UPDATE
	`, organizationID, projectID, unitID, token))
}

func (r *Repository) LoadScriptUnitRebuildByID(
	ctx context.Context,
	tx pgx.Tx,
	rebuildID string,
) (persistedScriptUnitRebuild, error) {
	return scanPersistedScriptUnitRebuild(tx.QueryRow(ctx, `
		SELECT id::text, organization_id::text, project_id::text, product_id::text,
		       script_unit_id::text, project_production_generation_id::text,
		       source_unit_generation_id::text, source_unit_configuration_hash,
		       source_script_version_id::text, source_localization_id::text,
		       target_source_script_version_id::text, target_language_mode,
		       target_explicit_language, target_duration_seconds, target_platform,
		       target_configuration_snapshot, target_configuration_hash,
		       impact_snapshot, impact_token, expected_script_unit_revision,
		       status, idempotency_key, COALESCE(workflow_run_id::text, ''),
		       COALESCE(target_localization_id::text, ''),
		       COALESCE(target_unit_generation_id::text, '')
		FROM commerce_script_unit_rebuilds
		WHERE id = $1
		FOR UPDATE
	`, rebuildID))
}

func scanPersistedScriptUnitRebuild(row pgx.Row) (persistedScriptUnitRebuild, error) {
	var item persistedScriptUnitRebuild
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.ScriptUnitID, &item.ProjectGenerationID, &item.SourceUnitGenerationID,
		&item.SourceUnitConfigurationHash, &item.SourceScriptVersionID,
		&item.SourceLocalizationID, &item.TargetSourceScriptVersionID,
		&item.TargetLanguageMode, &item.TargetLanguage, &item.TargetDurationSeconds,
		&item.TargetPlatform, &item.TargetConfiguration, &item.TargetConfigurationHash,
		&item.ImpactSnapshot, &item.ImpactToken, &item.ExpectedRevision, &item.Status,
		&item.IdempotencyKey, &item.WorkflowRunID, &item.TargetLocalizationID,
		&item.TargetUnitGenerationID,
	)
	return item, err
}

func (r *Repository) MarkScriptUnitRebuildRunning(
	ctx context.Context,
	tx pgx.Tx,
	rebuild persistedScriptUnitRebuild,
	idempotencyKey string,
) error {
	var conflictingID string
	err := tx.QueryRow(ctx, `
		SELECT id::text FROM commerce_script_unit_rebuilds
		WHERE organization_id = $1 AND idempotency_key = $2 AND id <> $3
	`, rebuild.OrganizationID, idempotencyKey, rebuild.ID).Scan(&conflictingID)
	if err == nil {
		return Error{Code: CodeIdempotencyKeyReused, Message: "脚本换代请求标识已被其他任务使用"}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_rebuilds
		SET status = 'running', idempotency_key = $2, started_at = now()
		WHERE id = $1 AND status = 'planned'
	`, rebuild.ID, idempotencyKey)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Error{Code: CodeScriptRebuildStale, Message: "脚本换代状态已变化，请刷新后重试"}
	}
	return nil
}

func (r *Repository) AttachScriptUnitRebuildWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	rebuildID string,
	workflowRunID string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_rebuilds
		SET workflow_run_id = $2
		WHERE id = $1 AND status = 'running' AND workflow_run_id IS NULL
	`, rebuildID, workflowRunID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Error{Code: CodeScriptRebuildStale, Message: "脚本换代工作流已被其他执行绑定"}
	}
	return nil
}
