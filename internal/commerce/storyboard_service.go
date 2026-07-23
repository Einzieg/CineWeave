package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const storyboardPlanSelectSQL = `
	SELECT plan.id::text, plan.organization_id::text, plan.project_id::text,
	       plan.product_id::text, plan.product_version_id::text,
	       plan.script_unit_id::text, plan.source_script_version_id::text,
	       plan.localization_id::text, plan.reference_pack_id::text,
	       plan.project_production_generation_id::text,
	       plan.script_unit_generation_id::text,
	       plan.commerce_workflow_binding_id::text,
	       plan.commerce_workflow_binding_revision,
	       plan.sales_script_contract_id::text,
	       plan.sales_script_contract_hash,
	       plan.workflow_run_id::text, plan.revision, plan.edit_revision,
	       plan.status, plan.active, plan.stale_state, plan.target_language,
	       plan.target_duration_seconds, plan.aspect_ratio,
	       plan.timeline_timebase, plan.fps_numerator, plan.fps_denominator,
	       plan.allowed_shot_durations, plan.actual_shot_count,
	       plan.review_status, plan.plan_hash, plan.projection_hash,
	       plan.created_at, plan.activated_at
	FROM commerce_storyboard_plans plan`

func (s *CatalogService) ListStoryboardPlans(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	unitID string,
	status string,
) ([]StoryboardPlan, error) {
	if status == "" || status == "active" {
		status = "active"
	}
	if status != "active" && status != "archived" && status != "all" {
		return nil, Error{Code: CodeStoryboardInvalid, Message: "分镜方案状态筛选无效"}
	}
	return s.repository.ListStoryboardPlans(ctx, db, organizationID, projectID, unitID, status)
}

func (s *CatalogService) LockActiveStoryboardGeneration(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
) (UnitGenerationIdentity, error) {
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return UnitGenerationIdentity{}, err
	}
	unit, err := s.repository.LoadScriptUnit(ctx, tx, organizationID, projectID, unitID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return UnitGenerationIdentity{}, Error{Code: CodeScriptUnitRequired, Message: "脚本单元不存在", Cause: err}
	}
	if err != nil {
		return UnitGenerationIdentity{}, err
	}
	if unit.Status == "archived" || unit.ActiveUnitGenerationID == nil {
		return UnitGenerationIdentity{}, Error{Code: CodeGenerationMismatch, Message: "脚本单元没有可用的活动生产代"}
	}
	generation, err := s.repository.LockUnitGenerationContext(ctx, tx, production, UnitGenerationIdentity{
		ExecutionIdentity: production.ExecutionIdentity(), ProductID: unit.ProductID,
		ScriptUnitID: unit.ID, ScriptUnitRevision: unit.Revision,
		UnitGenerationID: *unit.ActiveUnitGenerationID,
	})
	if err != nil {
		return UnitGenerationIdentity{}, err
	}
	return generation.Identity, nil
}

func (s *CatalogService) GetStoryboardPlan(
	ctx context.Context,
	db catalogQuerier,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
) (StoryboardPlanDetail, error) {
	plan, err := s.repository.LoadStoryboardPlan(ctx, db, organizationID, projectID, unitID, planID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoryboardPlanDetail{}, Error{Code: CodeStoryboardPlanRequired, Message: "分镜方案不存在", Cause: err}
	}
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	shots, err := s.repository.ListStoryboardShots(ctx, db, organizationID, projectID, unitID, planID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	return StoryboardPlanDetail{Plan: plan, Shots: shots}, nil
}

func (s *CatalogService) ListStoryboardShots(
	ctx context.Context,
	db catalogQuerier,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
) ([]StoryboardShot, error) {
	if _, err := s.repository.LoadStoryboardPlan(ctx, db, organizationID, projectID, unitID, planID, false); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, Error{Code: CodeStoryboardPlanRequired, Message: "分镜方案不存在", Cause: err}
		}
		return nil, err
	}
	return s.repository.ListStoryboardShots(ctx, db, organizationID, projectID, unitID, planID)
}

func (s *CatalogService) UpdateStoryboardShot(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	shotID string,
	actorID string,
	input UpdateStoryboardShotInput,
) (StoryboardPlanDetail, error) {
	plan, _, err := s.lockWritableStoryboardPlanByShot(ctx, tx, organizationID, projectID, unitID, shotID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if plan.EditRevision != input.ExpectedPlanRevision {
		return StoryboardPlanDetail{}, storyboardRevisionConflict(plan.EditRevision)
	}
	shot, err := s.repository.LoadStoryboardShot(ctx, tx, organizationID, projectID, unitID, plan.ID, shotID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoryboardPlanDetail{}, Error{Code: CodeStoryboardShotRequired, Message: "分镜镜头不存在", Cause: err}
	}
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if shot.Revision != input.ExpectedShotRevision {
		return StoryboardPlanDetail{}, storyboardRevisionConflict(plan.EditRevision)
	}
	if err := normalizeStoryboardShotUpdate(&input); err != nil {
		return StoryboardPlanDetail{}, err
	}

	visualChanged := changedString(input.VisualAction, shot.VisualAction) ||
		changedString(input.ShotPurpose, shot.ShotPurpose) ||
		changedString(input.Composition, shot.Composition) || input.Camera != nil || input.ProductReferenceIDs != nil
	voiceoverChanged := changedString(input.VoiceoverText, shot.VoiceoverText)
	onscreenChanged := changedString(input.OnscreenText, shot.OnscreenText)
	durationChanged := input.DurationSeconds != nil && *input.DurationSeconds != shot.DurationSeconds

	if input.ProductReferenceIDs != nil {
		if err := s.repository.ReplaceStoryboardShotProductReferences(ctx, tx, plan, shot.ID, *input.ProductReferenceIDs); err != nil {
			return StoryboardPlanDetail{}, err
		}
	}

	shots, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	for index := range shots {
		if shots[index].ID != shot.ID {
			continue
		}
		applyStoryboardShotUpdate(&shots[index], input)
		shot = shots[index]
		break
	}
	if voiceoverChanged {
		if err := s.repository.ReplaceStoryboardVoiceoverLinks(ctx, tx, plan, shots); err != nil {
			return StoryboardPlanDetail{}, err
		}
	}

	contractHash, err := storyboardShotContractHash(shot)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	productPresentation, err := storyboardProductPresentation(shot)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	status := plan.Status
	active := plan.Active
	activatedAtSQL := "activated_at"
	if durationChanged {
		status = "reviewing"
		active = false
		activatedAtSQL = "NULL"
	}
	if err := s.repository.UpdateStoryboardShot(ctx, tx, plan, shot, actorID, productPresentation, contractHash,
		visualChanged, voiceoverChanged, onscreenChanged, durationChanged); err != nil {
		return StoryboardPlanDetail{}, err
	}
	if durationChanged {
		if err := s.repository.ReflowStoryboardShots(ctx, tx, plan, shots, actorID, storyboardReflowInvalidation{
			VideoTimingChanged: true,
			DurationChangedShotIDs: map[string]struct{}{
				shot.ID: {},
			},
		}); err != nil {
			return StoryboardPlanDetail{}, err
		}
	}
	if visualChanged || voiceoverChanged || onscreenChanged || durationChanged {
		if err := s.repository.MarkStoryboardUnitDownstreamStale(ctx, tx, plan.UnitGenerationID, "storyboard_shot_updated"); err != nil {
			return StoryboardPlanDetail{}, err
		}
	}
	updatedShots, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	projectionHash, err := storyboardProjectionHash(updatedShots)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	updatedPlan, err := s.repository.AdvanceStoryboardPlanRevision(ctx, tx, plan, projectionHash, len(updatedShots), status, active, activatedAtSQL)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	return StoryboardPlanDetail{Plan: updatedPlan, Shots: updatedShots}, nil
}

func (s *CatalogService) ReorderStoryboardShots(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
	actorID string,
	input ReorderStoryboardShotsInput,
) (StoryboardPlanDetail, error) {
	plan, err := s.lockWritableStoryboardPlan(ctx, tx, organizationID, projectID, unitID, planID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if plan.EditRevision != input.ExpectedPlanRevision {
		return StoryboardPlanDetail{}, storyboardRevisionConflict(plan.EditRevision)
	}
	shots, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if len(input.Items) == 0 || len(input.Items) != len(shots) {
		return StoryboardPlanDetail{}, Error{Code: CodeStoryboardInvalid, Message: "排序必须提交当前方案的完整镜头列表"}
	}
	byID := make(map[string]StoryboardShot, len(shots))
	for _, shot := range shots {
		byID[shot.ID] = shot
	}
	ordered := make([]StoryboardShot, 0, len(input.Items))
	seen := make(map[string]struct{}, len(input.Items))
	durationChanged := false
	durationChangedShotIDs := make(map[string]struct{})
	sequenceChanged := false
	for index, item := range input.Items {
		shot, ok := byID[item.ShotID]
		if !ok {
			return StoryboardPlanDetail{}, Error{Code: CodeStoryboardInvalid, Message: "排序包含不属于当前方案的镜头"}
		}
		if _, duplicate := seen[item.ShotID]; duplicate {
			return StoryboardPlanDetail{}, Error{Code: CodeStoryboardInvalid, Message: "排序列表包含重复镜头"}
		}
		seen[item.ShotID] = struct{}{}
		if item.DurationSeconds > 0 && item.DurationSeconds != shot.DurationSeconds {
			if !storyboardDurationAllowed(plan.AllowedShotDurations, item.DurationSeconds) {
				return StoryboardPlanDetail{}, Error{Code: CodeStoryboardInvalid, Message: fmt.Sprintf("镜头 %d 秒不受当前视频模型支持", item.DurationSeconds)}
			}
			shot.DurationSeconds = item.DurationSeconds
			durationChanged = true
			durationChangedShotIDs[shot.ID] = struct{}{}
		}
		if shots[index].ID != shot.ID {
			sequenceChanged = true
		}
		ordered = append(ordered, shot)
	}
	if storyboardTotalDuration(ordered) != plan.TargetDurationSeconds {
		return StoryboardPlanDetail{}, Error{Code: CodeStoryboardInvalid, Message: "全部镜头时长之和必须等于脚本目标时长"}
	}
	if !sequenceChanged && !durationChanged {
		return StoryboardPlanDetail{Plan: plan, Shots: shots}, nil
	}
	if err := s.repository.ReorderStoryboardShots(ctx, tx, plan, ordered, actorID, storyboardReflowInvalidation{
		VideoTimingChanged:     true,
		DurationChangedShotIDs: durationChangedShotIDs,
	}); err != nil {
		return StoryboardPlanDetail{}, err
	}
	if err := s.repository.MarkStoryboardUnitDownstreamStale(ctx, tx, plan.UnitGenerationID, "storyboard_sequence_updated"); err != nil {
		return StoryboardPlanDetail{}, err
	}
	updatedShots, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	projectionHash, err := storyboardProjectionHash(updatedShots)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	updatedPlan, err := s.repository.AdvanceStoryboardPlanRevision(ctx, tx, plan, projectionHash, len(updatedShots), plan.Status, plan.Active, "activated_at")
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	return StoryboardPlanDetail{Plan: updatedPlan, Shots: updatedShots}, nil
}

func (s *CatalogService) ArchiveStoryboardShot(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	shotID string,
	actorID string,
	expectedPlanRevision int64,
	expectedShotRevision int64,
) (StoryboardPlanDetail, error) {
	plan, _, err := s.lockWritableStoryboardPlanByShot(ctx, tx, organizationID, projectID, unitID, shotID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if plan.EditRevision != expectedPlanRevision {
		return StoryboardPlanDetail{}, storyboardRevisionConflict(plan.EditRevision)
	}
	shot, err := s.repository.LoadStoryboardShot(ctx, tx, organizationID, projectID, unitID, plan.ID, shotID, true)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if expectedShotRevision > 0 && shot.Revision != expectedShotRevision {
		return StoryboardPlanDetail{}, storyboardRevisionConflict(plan.EditRevision)
	}
	shots, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if len(shots) <= 1 {
		return StoryboardPlanDetail{}, Error{Code: CodeStoryboardInvalid, Message: "分镜方案至少需要保留一个镜头"}
	}
	if err := s.repository.ArchiveStoryboardShot(ctx, tx, plan, shot, actorID); err != nil {
		return StoryboardPlanDetail{}, err
	}
	remaining, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if err := s.repository.ReflowStoryboardShots(ctx, tx, plan, remaining, actorID, storyboardReflowInvalidation{
		VideoTimingChanged: true,
	}); err != nil {
		return StoryboardPlanDetail{}, err
	}
	if err := s.repository.MarkStoryboardUnitDownstreamStale(ctx, tx, plan.UnitGenerationID, "storyboard_shot_archived"); err != nil {
		return StoryboardPlanDetail{}, err
	}
	remaining, err = s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	projectionHash, err := storyboardProjectionHash(remaining)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	updatedPlan, err := s.repository.AdvanceStoryboardPlanRevision(ctx, tx, plan, projectionHash, len(remaining), "reviewing", false, "NULL")
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	return StoryboardPlanDetail{Plan: updatedPlan, Shots: remaining}, nil
}

func (s *CatalogService) ActivateStoryboardPlan(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
	expectedPlanRevision int64,
) (StoryboardPlanDetail, error) {
	plan, err := s.lockWritableStoryboardPlan(ctx, tx, organizationID, projectID, unitID, planID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if plan.EditRevision != expectedPlanRevision {
		return StoryboardPlanDetail{}, storyboardRevisionConflict(plan.EditRevision)
	}
	shots, err := s.repository.ListStoryboardShots(ctx, tx, organizationID, projectID, unitID, plan.ID)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	if err := s.repository.ValidateStoryboardPlanForActivation(ctx, tx, plan, shots); err != nil {
		return StoryboardPlanDetail{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE commerce_storyboard_plans
		SET active = false
		WHERE script_unit_generation_id = $1 AND id <> $2 AND active
	`, plan.UnitGenerationID, plan.ID); err != nil {
		return StoryboardPlanDetail{}, err
	}
	projectionHash, err := storyboardProjectionHash(shots)
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	updatedPlan, err := s.repository.AdvanceStoryboardPlanRevision(ctx, tx, plan, projectionHash, len(shots), "ready", true, "now()")
	if err != nil {
		return StoryboardPlanDetail{}, err
	}
	return StoryboardPlanDetail{Plan: updatedPlan, Shots: shots}, nil
}

func (s *CatalogService) lockWritableStoryboardPlan(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
) (StoryboardPlan, error) {
	plan, err := s.repository.LoadStoryboardPlan(ctx, tx, organizationID, projectID, unitID, planID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoryboardPlan{}, Error{Code: CodeStoryboardPlanRequired, Message: "分镜方案不存在", Cause: err}
	}
	if err != nil {
		return StoryboardPlan{}, err
	}
	production, err := s.repository.LockActiveProductionContext(ctx, tx, organizationID, projectID)
	if err != nil {
		return StoryboardPlan{}, err
	}
	if plan.ProjectGenerationID != production.Generation.ID ||
		plan.CommerceWorkflowBindingID != production.CommerceBinding.ID ||
		plan.CommerceBindingRevision != production.CommerceBinding.Revision {
		return StoryboardPlan{}, Error{Code: CodeStoryboardPlanStale, Message: "分镜方案使用的项目生产配置已失效"}
	}
	if _, err := s.repository.LockUnitGenerationContext(ctx, tx, production, UnitGenerationIdentity{
		ExecutionIdentity: production.ExecutionIdentity(), ProductID: plan.ProductID,
		ScriptUnitID: plan.ScriptUnitID, UnitGenerationID: plan.UnitGenerationID,
	}); err != nil {
		return StoryboardPlan{}, err
	}
	plan, err = s.repository.LoadStoryboardPlan(ctx, tx, organizationID, projectID, unitID, planID, true)
	if err != nil {
		return StoryboardPlan{}, err
	}
	if plan.Status == "archived" || plan.Status == "stale" || plan.Status == "failed" || plan.StaleState != "fresh" {
		return StoryboardPlan{}, Error{Code: CodeStoryboardPlanStale, Message: "分镜方案已过期或不可编辑"}
	}
	if plan.Status != "ready" && plan.Status != "reviewing" {
		return StoryboardPlan{}, Error{Code: CodeStoryboardInvalid, Message: "分镜方案尚未进入可编辑状态"}
	}
	return plan, nil
}

func (s *CatalogService) lockWritableStoryboardPlanByShot(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	unitID string,
	shotID string,
) (StoryboardPlan, StoryboardShot, error) {
	var planID string
	if err := tx.QueryRow(ctx, `
		SELECT commerce_storyboard_plan_id::text
		FROM storyboard_shots
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND commerce_storyboard_plan_id IS NOT NULL AND deleted_at IS NULL
	`, shotID, organizationID, projectID).Scan(&planID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoryboardPlan{}, StoryboardShot{}, Error{Code: CodeStoryboardShotRequired, Message: "分镜镜头不存在", Cause: err}
		}
		return StoryboardPlan{}, StoryboardShot{}, err
	}
	plan, err := s.lockWritableStoryboardPlan(ctx, tx, organizationID, projectID, unitID, planID)
	if err != nil {
		return StoryboardPlan{}, StoryboardShot{}, err
	}
	shot, err := s.repository.LoadStoryboardShot(ctx, tx, organizationID, projectID, unitID, plan.ID, shotID, false)
	return plan, shot, err
}

func (r *Repository) ListStoryboardPlans(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	unitID string,
	status string,
) ([]StoryboardPlan, error) {
	query := storyboardPlanSelectSQL + `
		WHERE plan.organization_id = $1 AND plan.project_id = $2 AND plan.script_unit_id = $3`
	if status == "active" {
		query += ` AND plan.status <> 'archived'`
	} else if status == "archived" {
		query += ` AND plan.status = 'archived'`
	}
	query += ` ORDER BY plan.revision DESC, plan.id DESC`
	rows, err := db.Query(ctx, query, organizationID, projectID, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoryboardPlan, 0)
	for rows.Next() {
		item, err := scanStoryboardPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) LoadStoryboardPlan(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
	lock bool,
) (StoryboardPlan, error) {
	query := storyboardPlanSelectSQL + `
		WHERE plan.id = $1 AND plan.organization_id = $2
		  AND plan.project_id = $3 AND plan.script_unit_id = $4`
	if lock {
		query += ` FOR UPDATE OF plan`
	}
	return scanStoryboardPlan(db.QueryRow(ctx, query, planID, organizationID, projectID, unitID))
}

func (r *Repository) ListStoryboardShots(
	ctx context.Context,
	db catalogQuerier,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
) ([]StoryboardShot, error) {
	rows, err := db.Query(ctx, storyboardShotSelectSQL+`
		WHERE shot.organization_id = $1 AND shot.project_id = $2
		  AND contract.script_unit_id = $3 AND shot.commerce_storyboard_plan_id = $4
		  AND shot.deleted_at IS NULL
		ORDER BY shot.shot_index, shot.id
	`, organizationID, projectID, unitID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoryboardShot, 0)
	for rows.Next() {
		item, err := scanStoryboardShot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		items[index].SegmentLinks, err = r.ListStoryboardShotSegmentLinks(ctx, db, organizationID, projectID, planID, items[index].ID)
		if err != nil {
			return nil, err
		}
		items[index].ProductReferences, err = r.ListStoryboardShotProductReferences(ctx, db, organizationID, projectID, planID, items[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *Repository) LoadStoryboardShot(
	ctx context.Context,
	db rowQuerier,
	organizationID string,
	projectID string,
	unitID string,
	planID string,
	shotID string,
	lock bool,
) (StoryboardShot, error) {
	query := storyboardShotSelectSQL + `
		WHERE shot.id = $1 AND shot.organization_id = $2 AND shot.project_id = $3
		  AND contract.script_unit_id = $4 AND shot.commerce_storyboard_plan_id = $5
		  AND shot.deleted_at IS NULL`
	if lock {
		query += ` FOR UPDATE OF shot, contract`
	}
	return scanStoryboardShot(db.QueryRow(ctx, query, shotID, organizationID, projectID, unitID, planID))
}

const storyboardShotSelectSQL = `
	SELECT shot.id::text, shot.commerce_storyboard_plan_id::text,
	       contract.script_unit_id::text, contract.script_unit_generation_id::text,
	       contract.revision, shot.shot_no, COALESCE(shot.title, ''),
	       ((shot.end_tick - shot.start_tick) / plan.timeline_timebase)::integer,
	       shot.start_tick, shot.end_tick, contract.sales_beat,
	       contract.visual_action,
	       COALESCE(contract.product_presentation->>'shotPurpose', ''),
	       COALESCE(contract.product_presentation->>'composition', ''),
	       COALESCE(contract.product_presentation->'camera', '{}'::jsonb),
	       contract.voiceover_text, contract.onscreen_text, contract.target_language,
	       contract.sound_effects, contract.music_cue,
	       COALESCE(contract.product_presentation->'requiredProductFeatures', '[]'::jsonb),
	       contract.review_status, contract.manual_override, shot.stale_state,
	       COALESCE(shot.image_prompt, ''), shot.image_prompt_status, shot.image_status,
	       shot.image_artifact_id::text,
	       COALESCE(shot.image_storage_key, image_artifact.storage_key, ''),
	       COALESCE(image_artifact.mime_type, ''),
	       COALESCE(shot.video_prompt, ''), shot.video_prompt_status, shot.video_status,
	       shot.active_video_render_plan_id::text, render_plan.status,
	       shot.video_artifact_id::text,
	       COALESCE(shot.video_storage_key, video_artifact.storage_key, ''),
	       COALESCE(video_artifact.mime_type, ''),
	       shot.image_error_code, shot.image_error_message,
	       shot.video_error_code, shot.video_error_message,
	       contract.edited_by::text, contract.edited_at
	FROM storyboard_shots shot
	JOIN commerce_storyboard_plans plan ON plan.id = shot.commerce_storyboard_plan_id
	JOIN commerce_shot_contracts contract ON contract.storyboard_shot_id = shot.id
	LEFT JOIN artifacts image_artifact ON image_artifact.id = shot.image_artifact_id
	LEFT JOIN artifacts video_artifact ON video_artifact.id = shot.video_artifact_id
	LEFT JOIN video_render_plans render_plan ON render_plan.id = shot.active_video_render_plan_id`

func (r *Repository) ListStoryboardShotSegmentLinks(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	planID string,
	shotID string,
) ([]StoryboardShotSegmentLink, error) {
	rows, err := db.Query(ctx, `
		SELECT link.id::text, link.localization_segment_id::text,
		       segment.source_segment_id::text, link.usage, link.ordinal,
		       link.verbatim_start, link.verbatim_end
		FROM commerce_shot_segment_links link
		JOIN commerce_localization_segments segment ON segment.id = link.localization_segment_id
		WHERE link.organization_id = $1 AND link.project_id = $2
		  AND link.commerce_storyboard_plan_id = $3 AND link.storyboard_shot_id = $4
		ORDER BY segment.segment_no, link.usage, link.ordinal, link.id
	`, organizationID, projectID, planID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoryboardShotSegmentLink, 0)
	for rows.Next() {
		var item StoryboardShotSegmentLink
		var from, to pgtype.Int4
		if err := rows.Scan(&item.ID, &item.LocalizationSegmentID, &item.SourceSegmentID,
			&item.Usage, &item.Ordinal, &from, &to); err != nil {
			return nil, err
		}
		if from.Valid {
			value := int(from.Int32)
			item.VerbatimStart = &value
		}
		if to.Valid {
			value := int(to.Int32)
			item.VerbatimEnd = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListStoryboardShotProductReferences(
	ctx context.Context,
	db rowsQuerier,
	organizationID string,
	projectID string,
	planID string,
	shotID string,
) ([]StoryboardShotProductReference, error) {
	rows, err := db.Query(ctx, `
		SELECT reference.id::text, reference.product_reference_id::text,
		       reference.source_pack_id::text, reference.source_pack_item_id::text,
		       reference.role, reference.ordinal, reference.required,
		       item.artifact_id::text, item.media_file_id::text, item.content_hash,
		       COALESCE(artifact.storage_key, media.storage_key, ''),
		       COALESCE(artifact.mime_type, media.mime_type, '')
		FROM commerce_shot_product_references reference
		JOIN commerce_product_reference_pack_items item ON item.id = reference.source_pack_item_id
		LEFT JOIN artifacts artifact ON artifact.id = item.artifact_id
		LEFT JOIN media_files media ON media.id = item.media_file_id
		WHERE reference.organization_id = $1 AND reference.project_id = $2
		  AND reference.commerce_storyboard_plan_id = $3 AND reference.storyboard_shot_id = $4
		ORDER BY reference.ordinal, reference.id
	`, organizationID, projectID, planID, shotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]StoryboardShotProductReference, 0)
	for rows.Next() {
		var item StoryboardShotProductReference
		if err := rows.Scan(&item.ID, &item.ProductReferenceID, &item.SourcePackID,
			&item.SourcePackItemID, &item.Role, &item.Ordinal, &item.Required,
			&item.ArtifactID, &item.MediaFileID, &item.ContentHash,
			&item.StorageKey, &item.MimeType); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ReplaceStoryboardShotProductReferences(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shotID string,
	referenceIDs []string,
) error {
	if len(referenceIDs) == 0 {
		return Error{Code: CodeStoryboardInvalid, Message: "每个镜头至少需要一张商品参考图"}
	}
	seen := make(map[string]struct{}, len(referenceIDs))
	type frozenReference struct {
		PackItemID  string
		ReferenceID string
		Role        string
	}
	items := make([]frozenReference, 0, len(referenceIDs))
	for _, referenceID := range referenceIDs {
		referenceID = strings.TrimSpace(referenceID)
		if referenceID == "" {
			return Error{Code: CodeStoryboardInvalid, Message: "商品参考图 ID 不能为空"}
		}
		if _, duplicate := seen[referenceID]; duplicate {
			return Error{Code: CodeStoryboardInvalid, Message: "商品参考图不能重复选择"}
		}
		seen[referenceID] = struct{}{}
		var item frozenReference
		var referenceRole string
		if err := tx.QueryRow(ctx, `
			SELECT id::text, product_reference_id::text, reference_role
			FROM commerce_product_reference_pack_items
			WHERE reference_pack_id = $1 AND product_reference_id = $2
			  AND organization_id = $3 AND project_id = $4
		`, plan.ReferencePackID, referenceID, plan.OrganizationID, plan.ProjectID).Scan(
			&item.PackItemID, &item.ReferenceID, &referenceRole,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Error{Code: CodeStoryboardInvalid, Message: "选择的商品图不属于当前冻结引用包", Cause: err}
			}
			return err
		}
		item.Role = normalizeStoryboardReferenceRole(referenceRole)
		items = append(items, item)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM commerce_shot_product_references WHERE storyboard_shot_id = $1`, shotID); err != nil {
		return err
	}
	for ordinal, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_shot_product_references(
				organization_id, project_id, storyboard_shot_id,
				commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
				product_reference_id, source_pack_id, source_pack_item_id,
				role, ordinal, required
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, plan.OrganizationID, plan.ProjectID, shotID, plan.ID, plan.ScriptUnitID,
			plan.UnitGenerationID, item.ReferenceID, plan.ReferencePackID,
			item.PackItemID, item.Role, ordinal, item.Role == "primary"); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) UpdateStoryboardShot(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shot StoryboardShot,
	actorID string,
	productPresentation json.RawMessage,
	contractHash string,
	visualChanged bool,
	voiceoverChanged bool,
	onscreenChanged bool,
	durationChanged bool,
) error {
	imageChanged := visualChanged
	videoChanged := visualChanged || voiceoverChanged || durationChanged
	_, err := tx.Exec(ctx, `
		UPDATE commerce_shot_contracts
		SET visual_action = $3, product_presentation = $4,
		    voiceover_text = $5, onscreen_text = $6,
		    contract_hash = $7, manual_override = true,
		    revision = revision + 1, edited_by = NULLIF($8, '')::uuid,
		    edited_at = now(), updated_at = now()
		WHERE storyboard_shot_id = $1 AND commerce_storyboard_plan_id = $2
	`, shot.ID, plan.ID, shot.VisualAction, productPresentation,
		shot.VoiceoverText, shot.OnscreenText, contractHash, actorID)
	if err != nil {
		return err
	}
	cameraText := strings.TrimSpace(string(shot.Camera))
	if cameraText == "" {
		cameraText = "{}"
	}
	_, err = tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET action = $3, visual = $3, dialogue = $4, camera = $5,
		    metadata = COALESCE(metadata, '{}'::jsonb)
		      || jsonb_build_object('shotPurpose', $6::text, 'composition', $7::text),
		    end_tick = CASE WHEN $8::boolean THEN start_tick + ($9::bigint * $10::bigint) ELSE end_tick END,
		    stale_state = CASE WHEN $11::boolean OR $12::boolean THEN 'needs_regeneration' ELSE stale_state END,
		    image_prompt = CASE WHEN $11::boolean THEN NULL ELSE image_prompt END,
		    image_prompt_status = CASE WHEN $11::boolean THEN 'not_started' ELSE image_prompt_status END,
		    image_status = CASE WHEN $11::boolean THEN CASE WHEN image_artifact_id IS NULL THEN 'not_started' ELSE 'stale' END ELSE image_status END,
		    image_error_code = CASE WHEN $11::boolean THEN NULL ELSE image_error_code END,
		    image_error_message = CASE WHEN $11::boolean THEN NULL ELSE image_error_message END,
		    video_prompt = CASE WHEN $12::boolean THEN NULL ELSE video_prompt END,
		    video_prompt_status = CASE WHEN $12::boolean THEN 'not_started' ELSE video_prompt_status END,
		    video_status = CASE WHEN $12::boolean THEN CASE WHEN video_artifact_id IS NULL THEN 'not_started' ELSE 'stale' END ELSE video_status END,
		    video_error_code = CASE WHEN $12::boolean THEN NULL ELSE video_error_code END,
		    video_error_message = CASE WHEN $12::boolean THEN NULL ELSE video_error_message END,
		    active_video_render_plan_id = CASE WHEN $12::boolean THEN NULL ELSE active_video_render_plan_id END,
		    manual_override = true, edited_by = NULLIF($13, '')::uuid, edited_at = now(), updated_at = now()
		WHERE id = $1 AND commerce_storyboard_plan_id = $2 AND deleted_at IS NULL
	`, shot.ID, plan.ID, shot.VisualAction, shot.VoiceoverText, cameraText,
		shot.ShotPurpose, shot.Composition, durationChanged, shot.DurationSeconds,
		plan.TimelineTimebase, imageChanged, videoChanged, actorID)
	if err != nil {
		return err
	}
	return nil
}

type storyboardReflowInvalidation struct {
	VideoTimingChanged     bool
	DurationChangedShotIDs map[string]struct{}
}

func (i storyboardReflowInvalidation) durationChanged(shotID string) bool {
	_, ok := i.DurationChangedShotIDs[shotID]
	return ok
}

func (r *Repository) ReorderStoryboardShots(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shots []StoryboardShot,
	actorID string,
	invalidation storyboardReflowInvalidation,
) error {
	startTick := int64(0)
	for index, shot := range shots {
		endTick := startTick + int64(shot.DurationSeconds)*plan.TimelineTimebase
		changed := shot.ShotOrdinal != index+1 || shot.StartTick != startTick || shot.EndTick != endTick
		if changed {
			durationChanged := invalidation.durationChanged(shot.ID)
			videoChanged := invalidation.VideoTimingChanged || durationChanged
			if _, err := tx.Exec(ctx, `
				UPDATE storyboard_shots
				SET shot_index = $3, shot_no = $4, title = $5,
				    start_tick = $6, end_tick = $7,
				    duration_min_ticks = $7 - $6, duration_max_ticks = $7 - $6,
				    duration_source = CASE WHEN $8::boolean THEN 'manual_locked' ELSE duration_source END,
				    duration_locked = CASE WHEN $8::boolean THEN true ELSE duration_locked END,
				    timing_revision = timing_revision + 1,
				    stale_state = CASE WHEN $9::boolean THEN 'needs_regeneration' ELSE stale_state END,
				    video_prompt = CASE WHEN $9::boolean THEN NULL ELSE video_prompt END,
				    video_prompt_status = CASE WHEN $9::boolean THEN 'not_started' ELSE video_prompt_status END,
				    video_status = CASE WHEN $9::boolean THEN CASE WHEN video_artifact_id IS NULL THEN 'not_started' ELSE 'stale' END ELSE video_status END,
				    video_error_code = CASE WHEN $9::boolean THEN NULL ELSE video_error_code END,
				    video_error_message = CASE WHEN $9::boolean THEN NULL ELSE video_error_message END,
				    active_video_render_plan_id = CASE WHEN $9::boolean THEN NULL ELSE active_video_render_plan_id END,
				    manual_override = true, edited_by = NULLIF($10, '')::uuid,
				    edited_at = now(), updated_at = now()
				WHERE id = $1 AND commerce_storyboard_plan_id = $2 AND deleted_at IS NULL
			`, shot.ID, plan.ID, index, index+1, fmt.Sprintf("镜头 %02d", index+1),
				startTick, endTick, durationChanged, videoChanged, actorID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE commerce_shot_contracts
				SET revision = revision + 1, manual_override = true,
				    edited_by = NULLIF($3, '')::uuid, edited_at = now(), updated_at = now()
				WHERE storyboard_shot_id = $1 AND commerce_storyboard_plan_id = $2
			`, shot.ID, plan.ID, actorID); err != nil {
				return err
			}
		}
		startTick = endTick
	}
	return nil
}

func (r *Repository) ReflowStoryboardShots(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shots []StoryboardShot,
	actorID string,
	invalidation storyboardReflowInvalidation,
) error {
	return r.ReorderStoryboardShots(ctx, tx, plan, shots, actorID, invalidation)
}

func (r *Repository) MarkStoryboardUnitDownstreamStale(
	ctx context.Context,
	tx pgx.Tx,
	unitGenerationID string,
	reason string,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE project_timelines
		SET stale_state = 'needs_regeneration',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'staleReason', $2::text,
		      'staleAt', now()
		    ),
		    updated_at = now()
		WHERE commerce_script_unit_generation_id = $1
		  AND status <> 'archived'
	`, unitGenerationID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE artifacts artifact
		SET metadata = COALESCE(artifact.metadata, '{}'::jsonb) || jsonb_build_object(
		      'staleState', 'needs_regeneration',
		      'staleReason', $2::text,
		      'staleAt', now()
		    )
		FROM final_video_versions version
		WHERE version.commerce_script_unit_generation_id = $1
		  AND version.artifact_id = artifact.id
	`, unitGenerationID, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE final_video_versions
		SET production_readiness = 'blocked',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'staleState', 'needs_regeneration',
		      'staleReason', $2::text,
		      'staleAt', now()
		    )
		WHERE commerce_script_unit_generation_id = $1
		  AND status NOT IN ('archived', 'failed')
	`, unitGenerationID, reason)
	return err
}

func (r *Repository) ArchiveStoryboardShot(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shot StoryboardShot,
	actorID string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET deleted_at = now(), manual_override = true,
		    edited_by = NULLIF($4, '')::uuid, edited_at = now(), updated_at = now()
		WHERE id = $1 AND commerce_storyboard_plan_id = $2
		  AND deleted_at IS NULL AND organization_id = $3
	`, shot.ID, plan.ID, plan.OrganizationID, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Error{Code: CodeStoryboardShotRequired, Message: "分镜镜头不存在"}
	}
	return nil
}

func (r *Repository) AdvanceStoryboardPlanRevision(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	projectionHash string,
	shotCount int,
	status string,
	active bool,
	activatedAtSQL string,
) (StoryboardPlan, error) {
	if activatedAtSQL != "activated_at" && activatedAtSQL != "NULL" && activatedAtSQL != "now()" {
		return StoryboardPlan{}, errors.New("invalid storyboard activation expression")
	}
	query := `
		UPDATE commerce_storyboard_plans
		SET edit_revision = edit_revision + 1, projection_hash = $5,
		    actual_shot_count = $6, status = $7, active = $8,
		    activated_at = ` + activatedAtSQL + `
		WHERE id = $1 AND organization_id = $2 AND project_id = $3
		  AND edit_revision = $4
		RETURNING id`
	var id string
	if err := tx.QueryRow(ctx, query, plan.ID, plan.OrganizationID, plan.ProjectID,
		plan.EditRevision, projectionHash, shotCount, status, active).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoryboardPlan{}, storyboardRevisionConflict(plan.EditRevision)
		}
		return StoryboardPlan{}, err
	}
	return r.LoadStoryboardPlan(ctx, tx, plan.OrganizationID, plan.ProjectID, plan.ScriptUnitID, plan.ID, false)
}

func (r *Repository) ValidateStoryboardPlanForActivation(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shots []StoryboardShot,
) error {
	if len(shots) == 0 {
		return Error{Code: CodeStoryboardInvalid, Message: "分镜方案没有可激活的镜头"}
	}
	if storyboardTotalDuration(shots) != plan.TargetDurationSeconds {
		return Error{Code: CodeStoryboardInvalid, Message: "全部镜头时长之和必须等于脚本目标时长"}
	}
	for index, shot := range shots {
		if shot.ShotOrdinal != index+1 || strings.TrimSpace(shot.VisualAction) == "" ||
			strings.TrimSpace(shot.ShotPurpose) == "" || strings.TrimSpace(shot.Composition) == "" ||
			len(shot.ProductReferences) == 0 || !storyboardDurationAllowed(plan.AllowedShotDurations, shot.DurationSeconds) {
			return Error{Code: CodeStoryboardInvalid, Message: fmt.Sprintf("镜头 %02d 的分镜契约不完整", index+1)}
		}
	}
	var missingRequired int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM commerce_localization_segments segment
		JOIN commerce_ad_script_segments source ON source.id = segment.source_segment_id
		WHERE segment.localization_id = $1 AND source.required
		  AND NOT EXISTS (
		    SELECT 1
		    FROM commerce_shot_segment_links link
		    JOIN storyboard_shots shot ON shot.id = link.storyboard_shot_id
		    WHERE link.localization_segment_id = segment.id
		      AND link.commerce_storyboard_plan_id = $2
		      AND shot.deleted_at IS NULL
		  )
	`, plan.LocalizationID, plan.ID).Scan(&missingRequired); err != nil {
		return err
	}
	if missingRequired > 0 {
		return Error{Code: CodeStoryboardInvalid, Message: "分镜尚未覆盖广告脚本中的全部必需段落"}
	}
	return nil
}

type storyboardLocalizationSegment struct {
	ID              string
	SourceSegmentID string
	SegmentNo       int
	VoiceoverText   string
	Required        bool
}

func (r *Repository) ReplaceStoryboardVoiceoverLinks(
	ctx context.Context,
	tx pgx.Tx,
	plan StoryboardPlan,
	shots []StoryboardShot,
) error {
	rows, err := tx.Query(ctx, `
		SELECT segment.id::text, segment.source_segment_id::text,
		       segment.segment_no, segment.voiceover_text, source.required
		FROM commerce_localization_segments segment
		JOIN commerce_ad_script_segments source ON source.id = segment.source_segment_id
		WHERE segment.localization_id = $1 AND segment.script_unit_id = $2
		ORDER BY segment.segment_no, segment.id
	`, plan.LocalizationID, plan.ScriptUnitID)
	if err != nil {
		return err
	}
	segments := make([]storyboardLocalizationSegment, 0)
	for rows.Next() {
		var item storyboardLocalizationSegment
		if err := rows.Scan(&item.ID, &item.SourceSegmentID, &item.SegmentNo, &item.VoiceoverText, &item.Required); err != nil {
			rows.Close()
			return err
		}
		segments = append(segments, item)
	}
	rows.Close()
	bySource := make(map[string]storyboardLocalizationSegment, len(segments))
	order := make(map[string]int, len(segments))
	cursors := make(map[string]int, len(segments))
	ends := make(map[string]int, len(segments))
	for index, segment := range segments {
		bySource[segment.SourceSegmentID] = segment
		order[segment.SourceSegmentID] = index
		cursors[segment.SourceSegmentID], ends[segment.SourceSegmentID] = storyboardTrimmedRuneRange(segment.VoiceoverText)
	}
	type replacementLink struct {
		ShotID  string
		Segment storyboardLocalizationSegment
		Usage   string
		Ordinal int
		From    *int
		To      *int
	}
	replacements := make([]replacementLink, 0)
	covered := make(map[string]bool, len(segments))
	lastVoiceoverOrder := -1
	for _, shot := range shots {
		sourceSet := make(map[string]struct{}, len(shot.SegmentLinks))
		sourceIDs := make([]string, 0, len(shot.SegmentLinks))
		for _, link := range shot.SegmentLinks {
			if _, exists := sourceSet[link.SourceSegmentID]; exists {
				continue
			}
			if _, exists := bySource[link.SourceSegmentID]; !exists {
				return Error{Code: CodeStoryboardInvalid, Message: "镜头脚本段落不属于当前本地化版本"}
			}
			sourceSet[link.SourceSegmentID] = struct{}{}
			sourceIDs = append(sourceIDs, link.SourceSegmentID)
		}
		sort.Slice(sourceIDs, func(i, j int) bool { return order[sourceIDs[i]] < order[sourceIDs[j]] })
		remaining := []rune(strings.TrimSpace(shot.VoiceoverText))
		voiceOrdinal := 0
		linked := make(map[string]bool, len(sourceIDs))
		for _, sourceID := range sourceIDs {
			segment := bySource[sourceID]
			start, end := cursors[sourceID], ends[sourceID]
			if len(remaining) == 0 || start >= end {
				continue
			}
			available := []rune(segment.VoiceoverText)[start:end]
			consumed := 0
			switch {
			case strings.HasPrefix(string(remaining), string(available)):
				consumed = len(available)
				remaining = storyboardTrimLeadingSpaces(remaining[consumed:])
			case strings.HasPrefix(string(available), string(remaining)):
				consumed = len(remaining)
				remaining = nil
			default:
				continue
			}
			if order[sourceID] < lastVoiceoverOrder {
				return Error{Code: CodeStoryboardInvalid, Message: "旁白不能打乱广告脚本段落顺序"}
			}
			lastVoiceoverOrder = order[sourceID]
			from, to := start, start+consumed
			replacements = append(replacements, replacementLink{
				ShotID: shot.ID, Segment: segment, Usage: "voiceover", Ordinal: voiceOrdinal,
				From: &from, To: &to,
			})
			voiceOrdinal++
			cursors[sourceID] = to
			covered[sourceID] = true
			linked[sourceID] = true
		}
		if len(remaining) != 0 {
			return Error{Code: CodeStoryboardInvalid, Message: "旁白必须逐字来自当前已审核广告脚本"}
		}
		contextOrdinal := 0
		for _, sourceID := range sourceIDs {
			if linked[sourceID] {
				continue
			}
			segment := bySource[sourceID]
			replacements = append(replacements, replacementLink{
				ShotID: shot.ID, Segment: segment, Usage: "context", Ordinal: contextOrdinal,
			})
			contextOrdinal++
			covered[sourceID] = true
		}
	}
	for _, segment := range segments {
		if segment.Required && (!covered[segment.SourceSegmentID] || cursors[segment.SourceSegmentID] != ends[segment.SourceSegmentID]) {
			return Error{Code: CodeStoryboardInvalid, Message: "旁白修改后必须完整覆盖广告脚本中的必需段落"}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM commerce_shot_segment_links WHERE commerce_storyboard_plan_id = $1`, plan.ID); err != nil {
		return err
	}
	for _, link := range replacements {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commerce_shot_segment_links(
				organization_id, project_id, storyboard_shot_id,
				commerce_storyboard_plan_id, script_unit_id, script_unit_generation_id,
				localization_id, localization_segment_id, usage, ordinal,
				verbatim_start, verbatim_end
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, plan.OrganizationID, plan.ProjectID, link.ShotID, plan.ID, plan.ScriptUnitID,
			plan.UnitGenerationID, plan.LocalizationID, link.Segment.ID, link.Usage,
			link.Ordinal, link.From, link.To); err != nil {
			return err
		}
	}
	return nil
}

func scanStoryboardPlan(row scanRow) (StoryboardPlan, error) {
	var item StoryboardPlan
	var workflowRunID pgtype.Text
	var activatedAt pgtype.Timestamptz
	var durations []int32
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.ProjectID, &item.ProductID,
		&item.ProductVersionID, &item.ScriptUnitID, &item.SourceScriptVersionID,
		&item.LocalizationID, &item.ReferencePackID, &item.ProjectGenerationID,
		&item.UnitGenerationID, &item.CommerceWorkflowBindingID,
		&item.CommerceBindingRevision, &item.SalesScriptContractID,
		&item.SalesScriptContractHash, &workflowRunID, &item.PlanRevision,
		&item.EditRevision, &item.Status, &item.Active, &item.StaleState,
		&item.TargetLanguage, &item.TargetDurationSeconds, &item.AspectRatio,
		&item.TimelineTimebase, &item.FPSNumerator, &item.FPSDenominator,
		&durations, &item.ShotCount, &item.ReviewStatus, &item.PlanHash,
		&item.ProjectionHash, &item.CreatedAt, &activatedAt,
	)
	item.WorkflowRunID = textPointer(workflowRunID)
	item.ActivatedAt = timestampPointer(activatedAt)
	item.AllowedShotDurations = make([]int, len(durations))
	for index, value := range durations {
		item.AllowedShotDurations[index] = int(value)
	}
	return item, err
}

func scanStoryboardShot(row scanRow) (StoryboardShot, error) {
	var item StoryboardShot
	var imageArtifactID, videoArtifactID, videoRenderPlanID, videoRenderPlanStatus pgtype.Text
	var imageErrorCode, imageErrorMessage, videoErrorCode, videoErrorMessage pgtype.Text
	var editedBy pgtype.Text
	var editedAt pgtype.Timestamptz
	var requiredFeatures json.RawMessage
	err := row.Scan(
		&item.ID, &item.StoryboardPlanID, &item.ScriptUnitID, &item.UnitGenerationID,
		&item.Revision, &item.ShotOrdinal, &item.Title, &item.DurationSeconds,
		&item.StartTick, &item.EndTick, &item.SalesBeat, &item.VisualAction,
		&item.ShotPurpose, &item.Composition, &item.Camera, &item.VoiceoverText,
		&item.OnscreenText, &item.TargetLanguage, &item.SoundEffects, &item.MusicCue,
		&requiredFeatures, &item.ReviewStatus, &item.ManualOverride, &item.StaleState,
		&item.ImagePrompt, &item.ImagePromptStatus, &item.ImageStatus,
		&imageArtifactID, &item.ImageStorageKey, &item.ImageMimeType,
		&item.VideoPrompt, &item.VideoPromptStatus, &item.VideoStatus,
		&videoRenderPlanID, &videoRenderPlanStatus,
		&videoArtifactID, &item.VideoStorageKey, &item.VideoMimeType,
		&imageErrorCode, &imageErrorMessage, &videoErrorCode, &videoErrorMessage,
		&editedBy, &editedAt,
	)
	if err == nil {
		err = json.Unmarshal(requiredFeatures, &item.RequiredProductFeatures)
	}
	item.ImageArtifactID = textPointer(imageArtifactID)
	item.VideoArtifactID = textPointer(videoArtifactID)
	item.VideoRenderPlanID = textPointer(videoRenderPlanID)
	item.VideoRenderPlanStatus = textPointer(videoRenderPlanStatus)
	item.ImageErrorCode = textPointer(imageErrorCode)
	item.ImageErrorMessage = textPointer(imageErrorMessage)
	item.VideoErrorCode = textPointer(videoErrorCode)
	item.VideoErrorMessage = textPointer(videoErrorMessage)
	item.EditedBy = textPointer(editedBy)
	item.EditedAt = timestampPointer(editedAt)
	return item, err
}

func normalizeStoryboardShotUpdate(input *UpdateStoryboardShotInput) error {
	if input.ExpectedPlanRevision <= 0 || input.ExpectedShotRevision <= 0 {
		return Error{Code: CodeStoryboardInvalid, Message: "分镜 revision 不能为空"}
	}
	for label, value := range map[string]*string{
		"画面动作": input.VisualAction, "镜头目的": input.ShotPurpose, "构图": input.Composition,
	} {
		if value != nil {
			*value = strings.TrimSpace(*value)
			if *value == "" {
				return Error{Code: CodeStoryboardInvalid, Message: label + "不能为空"}
			}
		}
	}
	if input.VoiceoverText != nil {
		*input.VoiceoverText = strings.TrimSpace(*input.VoiceoverText)
	}
	if input.OnscreenText != nil {
		*input.OnscreenText = strings.TrimSpace(*input.OnscreenText)
	}
	if input.Camera != nil {
		var object map[string]any
		if err := json.Unmarshal(*input.Camera, &object); err != nil || object == nil {
			return Error{Code: CodeStoryboardInvalid, Message: "机位设置必须是有效对象"}
		}
	}
	if input.DurationSeconds != nil && *input.DurationSeconds <= 0 {
		return Error{Code: CodeStoryboardInvalid, Message: "镜头时长必须是正整数秒"}
	}
	return nil
}

func applyStoryboardShotUpdate(shot *StoryboardShot, input UpdateStoryboardShotInput) {
	if input.VisualAction != nil {
		shot.VisualAction = *input.VisualAction
	}
	if input.ShotPurpose != nil {
		shot.ShotPurpose = *input.ShotPurpose
	}
	if input.Composition != nil {
		shot.Composition = *input.Composition
	}
	if input.Camera != nil {
		shot.Camera = append(json.RawMessage(nil), (*input.Camera)...)
	}
	if input.VoiceoverText != nil {
		shot.VoiceoverText = *input.VoiceoverText
	}
	if input.OnscreenText != nil {
		shot.OnscreenText = *input.OnscreenText
	}
	if input.DurationSeconds != nil {
		shot.DurationSeconds = *input.DurationSeconds
	}
}

func storyboardProductPresentation(shot StoryboardShot) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"shotPurpose": shot.ShotPurpose, "composition": shot.Composition,
		"camera": shot.Camera, "productReferenceIds": storyboardReferenceIDs(shot.ProductReferences),
		"requiredProductFeatures": shot.RequiredProductFeatures,
	})
}

func storyboardShotContractHash(shot StoryboardShot) (string, error) {
	return hashStoryboardValue(map[string]any{
		"salesBeat": shot.SalesBeat, "visualAction": shot.VisualAction,
		"shotPurpose": shot.ShotPurpose, "composition": shot.Composition,
		"camera": shot.Camera, "voiceoverText": shot.VoiceoverText,
		"onscreenText": shot.OnscreenText, "targetLanguage": shot.TargetLanguage,
		"soundEffects": shot.SoundEffects, "musicCue": shot.MusicCue,
		"requiredProductFeatures": shot.RequiredProductFeatures,
		"productReferenceIds":     storyboardReferenceIDs(shot.ProductReferences),
	})
}

func storyboardProjectionHash(shots []StoryboardShot) (string, error) {
	type projectionShot struct {
		ID                string                           `json:"id"`
		Ordinal           int                              `json:"ordinal"`
		Duration          int                              `json:"duration"`
		SalesBeat         string                           `json:"salesBeat"`
		VisualAction      string                           `json:"visualAction"`
		ShotPurpose       string                           `json:"shotPurpose"`
		Composition       string                           `json:"composition"`
		Camera            json.RawMessage                  `json:"camera"`
		VoiceoverText     string                           `json:"voiceoverText"`
		OnscreenText      string                           `json:"onscreenText"`
		SegmentLinks      []StoryboardShotSegmentLink      `json:"segmentLinks"`
		ProductReferences []StoryboardShotProductReference `json:"productReferences"`
	}
	projection := make([]projectionShot, 0, len(shots))
	for _, shot := range shots {
		projection = append(projection, projectionShot{
			ID: shot.ID, Ordinal: shot.ShotOrdinal, Duration: shot.DurationSeconds,
			SalesBeat: shot.SalesBeat, VisualAction: shot.VisualAction,
			ShotPurpose: shot.ShotPurpose, Composition: shot.Composition,
			Camera: shot.Camera, VoiceoverText: shot.VoiceoverText,
			OnscreenText: shot.OnscreenText, SegmentLinks: shot.SegmentLinks,
			ProductReferences: shot.ProductReferences,
		})
	}
	return hashStoryboardValue(projection)
}

func hashStoryboardValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func storyboardReferenceIDs(items []StoryboardShotProductReference) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductReferenceID)
	}
	return ids
}

func storyboardTotalDuration(shots []StoryboardShot) int {
	total := 0
	for _, shot := range shots {
		total += shot.DurationSeconds
	}
	return total
}

func storyboardDurationAllowed(allowed []int, duration int) bool {
	for _, value := range allowed {
		if value == duration {
			return true
		}
	}
	return false
}

func normalizeStoryboardReferenceRole(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "primary":
		return "primary"
	case "detail", "macro":
		return "detail"
	case "logo", "brand":
		return "logo"
	case "usage", "demo", "demonstration":
		return "usage"
	default:
		return "context"
	}
}

func storyboardRevisionConflict(current int64) Error {
	return Error{
		Code: CodeStoryboardRevision, Message: "分镜方案已被其他操作修改，请刷新后重试",
		Details: map[string]any{"currentRevision": current},
	}
}

func changedString(next *string, current string) bool {
	return next != nil && *next != current
}

func storyboardTrimmedRuneRange(value string) (int, int) {
	runes := []rune(value)
	start := 0
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}
	end := len(runes)
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	return start, end
}

func storyboardTrimLeadingSpaces(value []rune) []rune {
	for len(value) > 0 && unicode.IsSpace(value[0]) {
		value = value[1:]
	}
	return value
}
