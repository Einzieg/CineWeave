package api

import (
	"context"
	"strings"

	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

type audioConfigurationInvalidationResult struct {
	Revision                int
	StaleClipCount          int64
	StaleMixCount           int64
	StaleReviewCount        int64
	StaleTimingCount        int64
	StaleStoryboardCount    int64
	StaleRenderPlanCount    int64
	ResetRenderSegmentCount int64
}

func invalidateProjectAudioConfigurationTx(ctx context.Context, tx pgx.Tx, project Project, reason, actorID string) (audioConfigurationInvalidationResult, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "audio_configuration_changed"
	}
	var result audioConfigurationInvalidationResult
	if err := tx.QueryRow(ctx, `
		UPDATE projects
		SET audio_configuration_revision = audio_configuration_revision + 1,
		    active_audio_mix_version_id = NULL,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		RETURNING audio_configuration_revision
	`, project.ID).Scan(&result.Revision); err != nil {
		return result, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE script_timing_units unit
		SET source_tts_audio_clip_id = NULL
		FROM tts_audio_clips clip
		WHERE unit.source_tts_audio_clip_id = clip.id
		  AND clip.project_id = $1
		  AND clip.audio_configuration_revision <> $2
	`, project.ID, result.Revision); err != nil {
		return result, err
	}
	clipTag, err := tx.Exec(ctx, `
		UPDATE tts_audio_clips
		SET status = 'stale', active = false,
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $3::text,
		      'replacementAudioConfigurationRevision', $2::integer
		    ),
		    updated_at = now(), completed_at = COALESCE(completed_at, now())
		WHERE project_id = $1
		  AND audio_configuration_revision <> $2
		  AND status IN ('queued', 'running', 'succeeded')
	`, project.ID, result.Revision, reason)
	if err != nil {
		return result, err
	}
	result.StaleClipCount = clipTag.RowsAffected()

	mixTag, err := tx.Exec(ctx, `
		UPDATE audio_mix_versions
		SET status = 'stale', active = false, production_readiness = 'blocked',
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $3::text,
		      'replacementAudioConfigurationRevision', $2::integer
		    ),
		    updated_at = now(), completed_at = COALESCE(completed_at, now())
		WHERE project_id = $1
		  AND audio_configuration_revision <> $2
		  AND status IN ('draft', 'mixing', 'ready')
	`, project.ID, result.Revision, reason)
	if err != nil {
		return result, err
	}
	result.StaleMixCount = mixTag.RowsAffected()

	reviewTag, err := tx.Exec(ctx, `
		UPDATE native_audio_reviews
		SET status = 'stale',
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $3::text,
		      'replacementAudioConfigurationRevision', $2::integer
		    ),
		    updated_at = now(), completed_at = COALESCE(completed_at, now())
		WHERE project_id = $1
		  AND audio_configuration_revision <> $2
		  AND status IN ('pending', 'running', 'passed', 'manual_override')
	`, project.ID, result.Revision, reason)
	if err != nil {
		return result, err
	}
	result.StaleReviewCount = reviewTag.RowsAffected()

	if _, err := tx.Exec(ctx, `
		UPDATE timing_calibration_profiles
		SET status = 'archived',
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $3::text,
		      'replacementAudioConfigurationRevision', $2::integer
		    ),
		    updated_at = now()
		WHERE project_id = $1
		  AND audio_configuration_revision <> $2
		  AND status = 'active'
	`, project.ID, result.Revision, reason); err != nil {
		return result, err
	}

	timingTag, err := tx.Exec(ctx, `
		UPDATE script_timing_analyses
		SET status = 'archived',
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $2::text,
		      'replacementAudioConfigurationRevision', $3::integer
		    )
		WHERE project_id = $1 AND method_version = 'tts-actual-v1' AND status IN ('draft', 'analyzing', 'ready')
	`, project.ID, reason, result.Revision)
	if err != nil {
		return result, err
	}
	result.StaleTimingCount = timingTag.RowsAffected()

	storyboardTag, err := tx.Exec(ctx, `
		UPDATE storyboard_plans plan
		SET active = false,
		    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
		    stale_state = 'upstream_changed',
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $2::text,
		      'replacementAudioConfigurationRevision', $3::integer
		    )
		WHERE plan.project_id = $1
		  AND plan.timing_analysis_id IN (
		    SELECT id FROM script_timing_analyses WHERE project_id = $1 AND method_version = 'tts-actual-v1'
		  )
		  AND plan.stale_state <> 'upstream_changed'
	`, project.ID, reason, result.Revision)
	if err != nil {
		return result, err
	}
	result.StaleStoryboardCount = storyboardTag.RowsAffected()

	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots shot
		SET stale_state = 'needs_regeneration',
		    image_status = CASE WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale' ELSE image_status END,
		    video_status = CASE WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale' ELSE video_status END,
		    active_video_render_plan_id = NULL,
		    production_readiness = 'blocked',
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $2::text,
		      'replacementAudioConfigurationRevision', $3::integer
		    ),
		    updated_at = now()
		WHERE shot.project_id = $1
		  AND shot.storyboard_plan_id IN (
		    SELECT plan.id FROM storyboard_plans plan
		    JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		    WHERE plan.project_id = $1 AND analysis.method_version = 'tts-actual-v1'
		  )
	`, project.ID, reason, result.Revision); err != nil {
		return result, err
	}

	renderPlanTag, err := tx.Exec(ctx, `
		UPDATE video_render_plans render_plan
		SET status = 'stale', active = false, production_readiness = 'blocked', updated_at = now(),
		    metadata = metadata || jsonb_build_object(
		      'audioConfigurationInvalidatedAt', now(),
		      'audioConfigurationInvalidationReason', $2::text,
		      'replacementAudioConfigurationRevision', $3::integer
		    )
		WHERE render_plan.project_id = $1
		  AND render_plan.storyboard_plan_id IN (
		    SELECT plan.id FROM storyboard_plans plan
		    JOIN script_timing_analyses analysis ON analysis.id = plan.timing_analysis_id
		    WHERE plan.project_id = $1 AND analysis.method_version = 'tts-actual-v1'
		  )
		  AND render_plan.status NOT IN ('archived', 'cancelled', 'stale')
	`, project.ID, reason, result.Revision)
	if err != nil {
		return result, err
	}
	result.StaleRenderPlanCount = renderPlanTag.RowsAffected()

	segmentTag, err := tx.Exec(ctx, `
		UPDATE video_render_segments segment
		SET audio_verification_status = 'audio_unverified', production_readiness = 'preview_only',
		    audio_verified_by = NULL, audio_verified_at = NULL,
		    audio_verification_notes = '音频配置已变更，需要重新审核',
		    updated_at = now()
		WHERE segment.project_id = $1
		  AND segment.native_audio_requested = true
		  AND COALESCE(segment.native_audio_detected, false) = true
	`, project.ID)
	if err != nil {
		return result, err
	}
	result.ResetRenderSegmentCount = segmentTag.RowsAffected()
	if _, err := tx.Exec(ctx, `
		UPDATE video_render_plans plan
		SET native_audio_status = 'audio_unverified', production_readiness = 'preview_only', updated_at = now()
		WHERE plan.project_id = $1
		  AND plan.status NOT IN ('archived', 'cancelled', 'stale')
		  AND EXISTS (
		    SELECT 1 FROM video_render_segments segment
		    WHERE segment.video_render_plan_id = plan.id
		      AND segment.native_audio_requested = true
		      AND COALESCE(segment.native_audio_detected, false) = true
		  )
	`, project.ID); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots shot
		SET native_audio_status = 'audio_unverified', production_readiness = 'preview_only', updated_at = now()
		WHERE shot.project_id = $1
		  AND shot.active_video_render_plan_id IN (
		    SELECT plan.id FROM video_render_plans plan
		    WHERE plan.project_id = $1 AND plan.native_audio_status = 'audio_unverified'
		  )
	`, project.ID); err != nil {
		return result, err
	}

	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return result, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "project.audio_configuration.invalidated", "project", project.ID, mustRawJSON(map[string]any{
		"reason": reason, "actorId": actorID, "audioConfigurationRevision": result.Revision,
		"staleClipCount": result.StaleClipCount, "staleMixCount": result.StaleMixCount,
		"staleReviewCount": result.StaleReviewCount, "staleTimingCount": result.StaleTimingCount,
		"staleStoryboardCount": result.StaleStoryboardCount, "staleRenderPlanCount": result.StaleRenderPlanCount,
		"resetRenderSegmentCount": result.ResetRenderSegmentCount,
	})); err != nil {
		return result, err
	}
	return result, nil
}
