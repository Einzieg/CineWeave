package commerce

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) LockProjectRebuild(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	rebuildID string,
) (ProjectRebuildContext, error) {
	var item ProjectRebuildContext
	var activeRebuildID, activeGenerationID pgtype.Text
	var sourceCommerceBindingID, sourceCommerceHash pgtype.Text
	var targetVideoBindingID, targetGenerationID, targetCommerceBindingID, targetCommerceHash pgtype.Text
	var targetVideoRevision, targetCommerceRevision, targetGenerationNo pgtype.Int8
	var targetProfileVersionID, targetProfileHash pgtype.Text
	var targetProfileSnapshot []byte
	err := tx.QueryRow(ctx, `
		SELECT rebuild.id::text, rebuild.organization_id::text, rebuild.project_id::text,
		       rebuild.status, rebuild.expected_project_revision,
		       rebuild.source_binding_id::text, rebuild.source_generation_id::text,
		       rebuild.source_commerce_workflow_binding_id::text,
		       rebuild.source_commerce_configuration_hash,
		       rebuild.target_profile_version_id::text,
		       rebuild.target_configuration, rebuild.target_configuration_hash,
		       rebuild.target_binding_id::text, rebuild.target_generation_id::text,
		       rebuild.target_commerce_workflow_binding_id::text,
		       rebuild.target_commerce_configuration_hash,
		       project.revision, project.video_production_state, project.video_production_locked,
		       project.active_video_production_rebuild_id::text,
		       project.active_video_production_generation_id::text,
		       target_video.revision, target_video.profile_version_id::text,
		       target_video.profile_snapshot_hash, target_video.profile_snapshot,
		       target_commerce.binding_revision,
		       target_generation.generation_no,
		       (SELECT count(*) FROM commerce_project_rebuild_items prepared WHERE prepared.rebuild_id = rebuild.id)
		FROM project_video_production_rebuilds rebuild
		JOIN projects project
		  ON project.id = rebuild.project_id
		 AND project.organization_id = rebuild.organization_id
		LEFT JOIN project_video_production_bindings target_video
		  ON target_video.id = rebuild.target_binding_id
		 AND target_video.project_id = rebuild.project_id
		LEFT JOIN project_commerce_workflow_bindings target_commerce
		  ON target_commerce.id = rebuild.target_commerce_workflow_binding_id
		 AND target_commerce.project_id = rebuild.project_id
		 AND target_commerce.organization_id = rebuild.organization_id
		LEFT JOIN project_video_production_generations target_generation
		  ON target_generation.id = rebuild.target_generation_id
		 AND target_generation.project_id = rebuild.project_id
		 AND target_generation.organization_id = rebuild.organization_id
		WHERE rebuild.id = $1
		  AND rebuild.project_id = $2
		  AND rebuild.organization_id = $3
		  AND project.project_kind = 'commerce_video'
		FOR UPDATE OF rebuild, project
	`, rebuildID, projectID, organizationID).Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.Status,
		&item.ExpectedProjectRevision,
		&item.SourceVideoBindingID,
		&item.SourceProjectGenerationID,
		&sourceCommerceBindingID,
		&sourceCommerceHash,
		&item.TargetProfileVersionID,
		&item.TargetConfiguration,
		&item.TargetConfigurationHash,
		&targetVideoBindingID,
		&targetGenerationID,
		&targetCommerceBindingID,
		&targetCommerceHash,
		&item.ProjectRevision,
		&item.ProjectState,
		&item.ProjectLocked,
		&activeRebuildID,
		&activeGenerationID,
		&targetVideoRevision,
		&targetProfileVersionID,
		&targetProfileHash,
		&targetProfileSnapshot,
		&targetCommerceRevision,
		&targetGenerationNo,
		&item.PreparedUnitCount,
	)
	if err != nil {
		return ProjectRebuildContext{}, projectRebuildNotFound(err)
	}
	if !sourceCommerceBindingID.Valid || !sourceCommerceHash.Valid {
		return ProjectRebuildContext{}, Error{Code: CodeBindingMismatch, Message: "带货视频换代缺少来源 Commerce Binding 身份"}
	}
	item.SourceCommerceBindingID = sourceCommerceBindingID.String
	item.SourceCommerceConfigurationHash = sourceCommerceHash.String
	if activeRebuildID.Valid {
		item.ActiveRebuildID = activeRebuildID.String
	}
	if activeGenerationID.Valid {
		item.ActiveProjectGenerationID = activeGenerationID.String
	}

	targetFields := []bool{
		targetVideoBindingID.Valid,
		targetGenerationID.Valid,
		targetCommerceBindingID.Valid,
		targetCommerceHash.Valid,
		targetVideoRevision.Valid,
		targetCommerceRevision.Valid,
		targetGenerationNo.Valid,
		targetProfileVersionID.Valid,
		targetProfileHash.Valid,
	}
	validTargetFields := 0
	for _, valid := range targetFields {
		if valid {
			validTargetFields++
		}
	}
	if validTargetFields != 0 && validTargetFields != len(targetFields) {
		return ProjectRebuildContext{}, Error{Code: CodeBindingMismatch, Message: "带货视频目标换代身份不完整"}
	}
	if validTargetFields == len(targetFields) {
		item.TargetVideoBindingID = targetVideoBindingID.String
		item.TargetProjectGenerationID = targetGenerationID.String
		item.TargetCommerceBindingID = targetCommerceBindingID.String
		item.TargetCommerceConfigurationHash = targetCommerceHash.String
		item.TargetPrepared = &InitialBindingResult{
			VideoBindingID:            targetVideoBindingID.String,
			VideoBindingRevision:      targetVideoRevision.Int64,
			VideoProfileVersionID:     targetProfileVersionID.String,
			VideoProfileSnapshot:      targetProfileSnapshot,
			VideoProfileSnapshotHash:  targetProfileHash.String,
			CommerceBindingID:         targetCommerceBindingID.String,
			CommerceBindingRevision:   targetCommerceRevision.Int64,
			CommerceConfigurationHash: targetCommerceHash.String,
			ProjectGenerationID:       targetGenerationID.String,
			ProjectGenerationNo:       targetGenerationNo.Int64,
		}
	}
	return item, nil
}

func (r *Repository) ListProjectRebuildUnitSeeds(
	ctx context.Context,
	tx pgx.Tx,
	production ProductionContext,
) ([]ProjectRebuildUnitSeed, error) {
	var blocked int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM commerce_script_units unit
		LEFT JOIN commerce_script_unit_generations generation
		  ON generation.id = unit.active_unit_generation_id
		 AND generation.script_unit_id = unit.id
		 AND generation.product_id = unit.product_id
		 AND generation.organization_id = unit.organization_id
		 AND generation.project_id = unit.project_id
		WHERE unit.project_id = $1
		  AND unit.organization_id = $2
		  AND unit.status <> 'archived'
		  AND (
		    unit.status <> 'ready'
		    OR generation.id IS NULL
		    OR generation.status <> 'active'
		    OR generation.project_production_generation_id <> $3
		    OR generation.commerce_workflow_binding_id <> $4
		    OR generation.commerce_workflow_binding_revision <> $5
		  )
	`, production.ProjectID, production.OrganizationID, production.Generation.ID,
		production.CommerceBinding.ID, production.CommerceBinding.Revision).Scan(&blocked); err != nil {
		return nil, err
	}
	if blocked > 0 {
		return nil, Error{
			Code:    CodeProjectRebuildBlocked,
			Message: "存在尚未完成设置或身份失配的脚本单元，不能切换项目生产配置",
			Details: map[string]any{"blockedUnitCount": blocked},
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT generation.organization_id::text, generation.project_id::text,
		       generation.product_id::text, generation.script_unit_id::text,
		       unit.revision, generation.id::text, generation.unit_generation_no,
		       generation.product_version_id::text, generation.source_script_version_id::text,
		       generation.localization_id::text, generation.reference_pack_id::text,
		       generation.unit_configuration_snapshot, generation.unit_configuration_hash
		FROM commerce_script_units unit
		JOIN commerce_script_unit_generations generation
		  ON generation.id = unit.active_unit_generation_id
		 AND generation.script_unit_id = unit.id
		 AND generation.product_id = unit.product_id
		 AND generation.organization_id = unit.organization_id
		 AND generation.project_id = unit.project_id
		WHERE unit.project_id = $1
		  AND unit.organization_id = $2
		  AND unit.status = 'ready'
		  AND generation.status = 'active'
		  AND generation.project_production_generation_id = $3
		  AND generation.commerce_workflow_binding_id = $4
		  AND generation.commerce_workflow_binding_revision = $5
		ORDER BY unit.sort_order, unit.unit_no
		FOR UPDATE OF unit
	`, production.ProjectID, production.OrganizationID, production.Generation.ID,
		production.CommerceBinding.ID, production.CommerceBinding.Revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProjectRebuildUnitSeed, 0)
	for rows.Next() {
		var item ProjectRebuildUnitSeed
		if err := rows.Scan(
			&item.OrganizationID,
			&item.ProjectID,
			&item.ProductID,
			&item.ScriptUnitID,
			&item.ScriptUnitRevision,
			&item.SourceUnitGenerationID,
			&item.SourceUnitGenerationNo,
			&item.ProductVersionID,
			&item.SourceScriptVersionID,
			&item.LocalizationID,
			&item.ReferencePackID,
			&item.ConfigurationSnapshot,
			&item.ConfigurationHash,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) InsertPreparingProjectRebuildUnit(
	ctx context.Context,
	tx pgx.Tx,
	rebuildID string,
	target InitialBindingResult,
	unit ProjectRebuildUnitTarget,
	createdBy string,
) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO commerce_script_unit_generations(
			id, organization_id, project_id, product_id, script_unit_id,
			project_production_generation_id, unit_generation_no, status,
			commerce_workflow_binding_id, commerce_workflow_binding_revision,
			product_version_id, source_script_version_id, localization_id,
			reference_pack_id, unit_configuration_snapshot, unit_configuration_hash,
			source_unit_generation_id, created_by
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, 'preparing', $8, $9,
			$10, $11, $12, $13, $14, $15, $16, NULLIF($17, '')::uuid
		)
	`, unit.TargetUnitGenerationID, unit.OrganizationID, unit.ProjectID, unit.ProductID,
		unit.ScriptUnitID, target.ProjectGenerationID, unit.TargetUnitGenerationNo,
		target.CommerceBindingID, target.CommerceBindingRevision, unit.ProductVersionID,
		unit.SourceScriptVersionID, unit.LocalizationID, unit.ReferencePackID,
		unit.TargetConfiguration, unit.TargetConfigurationHash, unit.SourceUnitGenerationID, createdBy); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO commerce_project_rebuild_items(
			organization_id, project_id, rebuild_id, script_unit_id,
			source_unit_generation_id, source_script_unit_revision,
			target_unit_generation_id, target_unit_configuration_hash,
			status, checkpoint
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'ready', $9)
	`, unit.OrganizationID, unit.ProjectID, rebuildID, unit.ScriptUnitID,
		unit.SourceUnitGenerationID, unit.ScriptUnitRevision, unit.TargetUnitGenerationID,
		unit.TargetConfigurationHash, mustJSON(map[string]any{
			"sourceUnitConfigurationHash": unit.ConfigurationHash,
			"targetUnitConfigurationHash": unit.TargetConfigurationHash,
		}))
	return err
}

func (r *Repository) AttachPreparedProjectRebuild(
	ctx context.Context,
	tx pgx.Tx,
	rebuild ProjectRebuildContext,
	target InitialBindingResult,
	unitCount int,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuilds
		SET target_binding_id = $2,
		    target_generation_id = $3,
		    target_commerce_workflow_binding_id = $4,
		    target_commerce_configuration_hash = $5,
		    status = 'running',
		    episode_count = $6,
		    started_at = COALESCE(started_at, now()),
		    failure_code = NULL,
		    failure_message = NULL
		WHERE id = $1
		  AND project_id = $7
		  AND organization_id = $8
		  AND status = 'approved'
		  AND target_binding_id IS NULL
		  AND target_generation_id IS NULL
	`, rebuild.ID, target.VideoBindingID, target.ProjectGenerationID,
		target.CommerceBindingID, target.CommerceConfigurationHash, unitCount,
		rebuild.ProjectID, rebuild.OrganizationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Error{Code: CodeBindingMismatch, Message: "带货视频目标换代已被其他执行修改"}
	}
	return nil
}

func (r *Repository) ActivatePreparedProjectRebuild(
	ctx context.Context,
	tx pgx.Tx,
	rebuild ProjectRebuildContext,
	target InitialBindingResult,
	configuration videoproduction.ProductionConfigurationSnapshot,
	expectedUnitCount int,
) (ProjectRebuildActivationResult, error) {
	if err := validatePreparedTarget(rebuild, target); err != nil {
		return ProjectRebuildActivationResult{}, err
	}
	if rebuild.Status == "succeeded" {
		return r.loadActivatedProjectRebuildResult(ctx, tx, rebuild, target)
	}
	if rebuild.ProjectRevision != rebuild.ExpectedProjectRevision ||
		!rebuild.ProjectLocked || rebuild.ProjectState != "rebuilding" ||
		rebuild.ActiveRebuildID != rebuild.ID ||
		rebuild.ActiveProjectGenerationID != rebuild.SourceProjectGenerationID {
		return ProjectRebuildActivationResult{}, Error{Code: CodeRevisionConflict, Message: "项目已变化，请重新确认换代影响"}
	}

	var activeUnits, preparedItems, readyItems, validTargets int
	if err := tx.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM commerce_script_units unit
		   WHERE unit.project_id = $1 AND unit.organization_id = $2 AND unit.status <> 'archived'),
		  count(*),
		  count(*) FILTER (WHERE item.status = 'ready' AND jsonb_array_length(item.blockers) = 0),
		  count(*) FILTER (
		    WHERE target.status = 'preparing'
		      AND target.project_production_generation_id = $4
		      AND target.commerce_workflow_binding_id = $5
		      AND target.commerce_workflow_binding_revision = $6
		      AND target.unit_configuration_hash = item.target_unit_configuration_hash
		      AND source.status = 'active'
		      AND source.project_production_generation_id = $7
		      AND unit.active_unit_generation_id = source.id
		      AND unit.revision = item.source_script_unit_revision
		  )
		FROM commerce_project_rebuild_items item
		JOIN commerce_script_units unit
		  ON unit.id = item.script_unit_id
		 AND unit.project_id = item.project_id
		 AND unit.organization_id = item.organization_id
		JOIN commerce_script_unit_generations source
		  ON source.id = item.source_unit_generation_id
		 AND source.script_unit_id = item.script_unit_id
		 AND source.project_id = item.project_id
		 AND source.organization_id = item.organization_id
		LEFT JOIN commerce_script_unit_generations target
		  ON target.id = item.target_unit_generation_id
		 AND target.script_unit_id = item.script_unit_id
		 AND target.project_id = item.project_id
		 AND target.organization_id = item.organization_id
		WHERE item.rebuild_id = $3
	`, rebuild.ProjectID, rebuild.OrganizationID, rebuild.ID,
		target.ProjectGenerationID, target.CommerceBindingID, target.CommerceBindingRevision,
		rebuild.SourceProjectGenerationID).Scan(&activeUnits, &preparedItems, &readyItems, &validTargets); err != nil {
		return ProjectRebuildActivationResult{}, err
	}
	if activeUnits != expectedUnitCount || preparedItems != expectedUnitCount ||
		readyItems != expectedUnitCount || validTargets != expectedUnitCount {
		return ProjectRebuildActivationResult{}, Error{
			Code:    CodeProjectRebuildBlocked,
			Message: "部分脚本单元未通过项目换代预检，旧生产配置保持不变",
			Details: map[string]any{
				"activeUnitCount":   activeUnits,
				"preparedItemCount": preparedItems,
				"readyItemCount":    readyItems,
				"validTargetCount":  validTargets,
			},
		}
	}

	if err := videoproduction.ArchiveGenerationProductionData(ctx, tx, rebuild.ProjectID, rebuild.SourceProjectGenerationID); err != nil {
		return ProjectRebuildActivationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_storyboard_plans
		SET status = 'stale', active = false, stale_state = 'upstream_changed',
		    stale_at = COALESCE(stale_at, now())
		WHERE project_id = $1
		  AND project_production_generation_id = $2
		  AND status <> 'archived'
	`, rebuild.ProjectID, rebuild.SourceProjectGenerationID); err != nil {
		return ProjectRebuildActivationResult{}, err
	}

	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations source
		SET status = 'archived', archived_at = now()
		FROM commerce_project_rebuild_items item
		WHERE item.rebuild_id = $1
		  AND source.id = item.source_unit_generation_id
		  AND source.status = 'active'
	`, rebuild.ID); err != nil || tag.RowsAffected() != int64(expectedUnitCount) {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "归档旧脚本单元生产代失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_bindings
		SET status = 'superseded', superseded_by_rebuild_id = $2, superseded_at = now()
		WHERE id = $1 AND project_id = $3 AND status = 'active'
	`, rebuild.SourceVideoBindingID, rebuild.ID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "归档旧视频生产绑定失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_commerce_workflow_bindings
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'active'
	`, rebuild.SourceCommerceBindingID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "归档旧带货工作流绑定失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_generations
		SET status = 'superseded', superseded_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'active'
	`, rebuild.SourceProjectGenerationID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "归档旧项目生产代失败")
	}

	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_bindings
		SET status = 'active'
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
	`, target.VideoBindingID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "激活目标视频生产绑定失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_commerce_workflow_bindings
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
	`, target.CommerceBindingID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "激活目标带货工作流绑定失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_generations
		SET status = 'active', activated_at = now()
		WHERE id = $1 AND project_id = $2 AND status = 'preparing'
	`, target.ProjectGenerationID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "激活目标项目生产代失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_unit_generations target_generation
		SET status = 'active', activated_at = now()
		FROM commerce_project_rebuild_items item
		WHERE item.rebuild_id = $1
		  AND target_generation.id = item.target_unit_generation_id
		  AND target_generation.status = 'preparing'
	`, rebuild.ID); err != nil || tag.RowsAffected() != int64(expectedUnitCount) {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "激活目标脚本单元生产代失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_script_units unit
		SET active_unit_generation_id = item.target_unit_generation_id,
		    unit_generation_no = target_generation.unit_generation_no,
		    revision = unit.revision + 1,
		    updated_at = now()
		FROM commerce_project_rebuild_items item
		JOIN commerce_script_unit_generations target_generation
		  ON target_generation.id = item.target_unit_generation_id
		WHERE item.rebuild_id = $1
		  AND unit.id = item.script_unit_id
		  AND unit.active_unit_generation_id = item.source_unit_generation_id
		  AND unit.revision = item.source_script_unit_revision
	`, rebuild.ID); err != nil || tag.RowsAffected() != int64(expectedUnitCount) {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "切换脚本单元生产代失败")
	}

	settings := configuration.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE projects
		SET active_video_production_generation_id = $2,
		    video_production_generation_no = $3,
		    video_production_state = 'storyboard_required',
		    video_production_locked = false,
		    active_video_production_rebuild_id = NULL,
		    project_type = $5,
		    content_type = NULLIF($6, ''),
		    aspect_ratio = $7,
		    video_ratio = $8,
		    art_style = $9,
		    director_manual = $10,
		    visual_manual = $11,
		    image_model_profile_key = $12,
		    video_model_profile_key = $13,
		    script_model_profile_key = $14,
		    tts_model_profile_key = $15,
		    asr_model_profile_key = $16,
		    audio_configuration_revision = audio_configuration_revision + CASE
		      WHEN audio_strategy IS DISTINCT FROM $17
		        OR audio_requirement IS DISTINCT FROM $18
		        OR tts_model_profile_key IS DISTINCT FROM $15
		        OR asr_model_profile_key IS DISTINCT FROM $16 THEN 1 ELSE 0 END,
		    audio_strategy = $17,
		    audio_requirement = $18,
		    image_quality = $19,
		    timeline_timebase = $20,
		    fps_numerator = $21,
		    fps_denominator = $22,
		    settings = $23,
		    active_final_video_version_id = NULL,
		    active_audio_mix_version_id = NULL,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND organization_id = $24
		  AND revision = $25
		  AND active_video_production_generation_id = $4
		  AND active_video_production_rebuild_id = $26
		  AND video_production_locked = true
	`, rebuild.ProjectID, target.ProjectGenerationID, target.ProjectGenerationNo,
		rebuild.SourceProjectGenerationID, configuration.ProjectType, configuration.ContentType,
		configuration.AspectRatio, configuration.VideoRatio, configuration.ArtStyle,
		configuration.DirectorManual, configuration.VisualManual, configuration.ImageModelProfileKey,
		configuration.VideoModelProfileKey, configuration.ScriptModelProfileKey,
		configuration.TTSModelProfileKey, configuration.ASRModelProfileKey,
		configuration.AudioStrategy, configuration.AudioRequirement, configuration.ImageQuality,
		configuration.TimelineTimebase, configuration.FPSNumerator, configuration.FPSDenominator,
		settings, rebuild.OrganizationID, rebuild.ExpectedProjectRevision, rebuild.ID)
	if err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "切换项目生产代失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE commerce_project_rebuild_items
		SET status = 'switched', completed_at = now(), error_code = NULL, error_message = NULL
		WHERE rebuild_id = $1 AND status = 'ready'
	`, rebuild.ID); err != nil || tag.RowsAffected() != int64(expectedUnitCount) {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "提交脚本单元换代结果失败")
	}
	if tag, err := tx.Exec(ctx, `
		UPDATE project_video_production_rebuilds
		SET status = 'succeeded', completed_at = now(),
		    failure_code = NULL, failure_message = NULL
		WHERE id = $1 AND project_id = $2 AND status = 'running'
	`, rebuild.ID, rebuild.ProjectID); err != nil || tag.RowsAffected() != 1 {
		return ProjectRebuildActivationResult{}, affectedRowsError(err, "提交项目换代结果失败")
	}
	return r.loadActivatedProjectRebuildResult(ctx, tx, rebuild, target)
}

func (r *Repository) loadActivatedProjectRebuildResult(
	ctx context.Context,
	tx pgx.Tx,
	rebuild ProjectRebuildContext,
	target InitialBindingResult,
) (ProjectRebuildActivationResult, error) {
	var result ProjectRebuildActivationResult
	result.RebuildID = rebuild.ID
	err := tx.QueryRow(ctx, `
		SELECT project.revision,
		       generation.id::text, generation.generation_no, generation.status,
		       video.id::text, video.revision, video.status, video.profile_version_id::text,
		       video.profile_snapshot_hash, video.profile_snapshot,
		       commerce.id::text, commerce.binding_revision, commerce.status,
		       commerce.template_version_id::text, commerce.video_production_binding_id::text,
		       commerce.video_profile_snapshot_hash, commerce.configuration_hash,
		       commerce.configuration_snapshot, commerce.model_routing_snapshot,
		       commerce.capability_snapshot,
		       (SELECT count(*) FROM commerce_project_rebuild_items item
		        WHERE item.rebuild_id = $2 AND item.status = 'switched')
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id
		JOIN project_video_production_bindings video ON video.id = generation.binding_id
		JOIN project_commerce_workflow_bindings commerce
		  ON commerce.id = generation.commerce_workflow_binding_id
		WHERE project.id = $1
		  AND project.organization_id = $3
		  AND generation.id = $4
		  AND video.id = $5
		  AND commerce.id = $6
	`, rebuild.ProjectID, rebuild.ID, rebuild.OrganizationID, target.ProjectGenerationID,
		target.VideoBindingID, target.CommerceBindingID).Scan(
		&result.ProjectRevision,
		&result.ProjectGeneration.ID,
		&result.ProjectGeneration.GenerationNo,
		&result.ProjectGeneration.Status,
		&result.VideoBinding.ID,
		&result.VideoBinding.Revision,
		&result.VideoBinding.Status,
		&result.VideoBinding.ProfileVersionID,
		&result.VideoBinding.ProfileSnapshotHash,
		&result.VideoBinding.ProfileSnapshot,
		&result.CommerceBinding.ID,
		&result.CommerceBinding.Revision,
		&result.CommerceBinding.Status,
		&result.CommerceBinding.TemplateVersionID,
		&result.CommerceBinding.VideoBindingID,
		&result.CommerceBinding.VideoProfileSnapshotHash,
		&result.CommerceBinding.ConfigurationHash,
		&result.CommerceBinding.ConfigurationSnapshot,
		&result.CommerceBinding.ModelRoutingSnapshot,
		&result.CommerceBinding.CapabilitySnapshot,
		&result.SwitchedUnitCount,
	)
	if err != nil {
		return ProjectRebuildActivationResult{}, err
	}
	result.ProjectGeneration.VideoBindingID = result.VideoBinding.ID
	result.ProjectGeneration.CommerceBindingID = result.CommerceBinding.ID
	return result, nil
}

func affectedRowsError(err error, message string) error {
	if err != nil {
		return err
	}
	return Error{Code: CodeRevisionConflict, Message: message}
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal trusted commerce snapshot: %v", err))
	}
	return raw
}
