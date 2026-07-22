package production

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func MarkAssetDownstreamStale(ctx context.Context, db Execer, projectID, assetID string) error {
	return markAssetDownstreamStale(ctx, db, projectID, assetID, "upstream_changed")
}

// MarkAssetProductionMaterialStale invalidates derived media after prompt or
// reference production without claiming that the canonical asset identity
// changed and the storyboard must be analyzed again.
func MarkAssetProductionMaterialStale(ctx context.Context, db Execer, projectID, assetID string) error {
	return markAssetDownstreamStale(ctx, db, projectID, assetID, "needs_regeneration")
}

func markAssetDownstreamStale(ctx context.Context, db Execer, projectID, assetID, requirementStaleState string) error {
	if _, err := db.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET stale_state = $3,
		    updated_at = now()
		WHERE project_id = $1 AND asset_id = $2
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, assetID, requirementStaleState); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE storyboard_shots s
		SET stale_state = 'needs_regeneration',
		    image_prompt_status = 'not_started',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULL,
		    image_prompt_updated_at = now(),
		    image_status = CASE
		      WHEN s.image_artifact_id IS NOT NULL OR s.image_media_file_id IS NOT NULL OR COALESCE(s.image_storage_key, '') <> '' THEN 'stale'
		      ELSE s.image_status
		    END,
		    video_status = CASE
		      WHEN s.video_artifact_id IS NOT NULL OR s.video_media_file_id IS NOT NULL OR COALESCE(s.video_storage_key, '') <> '' THEN 'stale'
		      ELSE s.video_status
		    END,
		    updated_at = now()
		WHERE s.project_id = $1
		  AND s.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND EXISTS (
			SELECT 1
			FROM shot_asset_requirements r
			WHERE r.storyboard_shot_id = s.id
			  AND r.asset_id = $2
			  AND r.production_generation_id = s.production_generation_id
		  )
	`, projectID, assetID)
	return err
}

func MarkShotDownstreamStale(ctx context.Context, db Execer, projectID, shotID string) error {
	_, err := db.Exec(ctx, `
		UPDATE storyboard_shots
		SET stale_state = 'needs_regeneration',
		    image_prompt_status = 'not_started',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULL,
		    image_prompt_updated_at = now(),
		    image_status = CASE
		      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
		      ELSE image_status
		    END,
		    video_status = CASE
		      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
		      ELSE video_status
		    END,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, shotID)
	return err
}

func MarkRequirementDownstreamStale(ctx context.Context, db Execer, projectID, requirementID string) error {
	if _, err := db.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET stale_state = 'needs_regeneration',
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, requirementID); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE storyboard_shots s
		SET stale_state = 'needs_regeneration',
		    image_prompt_status = 'not_started',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULL,
		    image_prompt_updated_at = now(),
		    image_status = CASE
		      WHEN s.image_artifact_id IS NOT NULL OR s.image_media_file_id IS NOT NULL OR COALESCE(s.image_storage_key, '') <> '' THEN 'stale'
		      ELSE s.image_status
		    END,
		    video_status = CASE
		      WHEN s.video_artifact_id IS NOT NULL OR s.video_media_file_id IS NOT NULL OR COALESCE(s.video_storage_key, '') <> '' THEN 'stale'
		      ELSE s.video_status
		    END,
		    updated_at = now()
		FROM shot_asset_requirements r
		WHERE r.storyboard_shot_id = s.id
		  AND r.project_id = $1
		  AND r.id = $2
		  AND r.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND s.production_generation_id = r.production_generation_id
	`, projectID, requirementID)
	return err
}

func MarkFinalVideoStale(ctx context.Context, db Execer, projectID, workflowRunID string) error {
	_, err := db.Exec(ctx, `
		UPDATE artifacts
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('staleState', 'needs_regeneration')
		WHERE project_id = $1
		  AND type = 'final_video'
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND ($2 = '' OR workflow_run_id = $2::uuid)
	`, projectID, workflowRunID)
	return err
}

// MarkScriptVersionDownstreamStale invalidates every production object derived
// from a script version. Callers are expected to run it in the same transaction
// that activates the replacement version.
func MarkScriptVersionDownstreamStale(ctx context.Context, db Execer, projectID, versionID string) error {
	if _, err := db.Exec(ctx, `
		UPDATE script_scenes
		SET stale_state = 'needs_regeneration',
		    review_status = 'pending',
		    updated_at = now()
		WHERE project_id = $1
		  AND script_version_id = $2
		  AND deleted_at IS NULL
	`, projectID, versionID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE scene_asset_links l
		SET metadata = COALESCE(l.metadata, '{}'::jsonb) || jsonb_build_object(
		  'staleState', 'upstream_changed',
		  'staleReason', 'script_scene_updated'
		)
		WHERE l.project_id = $1
		  AND l.script_scene_id IN (
		    SELECT id
		    FROM script_scenes
		    WHERE project_id = $1
		      AND script_version_id = $2
		      AND deleted_at IS NULL
		  )
	`, projectID, versionID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE canonical_assets a
		SET stale_state = 'upstream_changed', updated_at = now()
		WHERE a.project_id = $1
		  AND a.id IN (
		    SELECT l.asset_id
		    FROM scene_asset_links l
		    JOIN script_scenes s ON s.id = l.script_scene_id
		    WHERE l.project_id = $1
		      AND s.project_id = $1
		      AND s.script_version_id = $2
		      AND s.deleted_at IS NULL
		  )
	`, projectID, versionID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE shot_asset_requirements r
		SET stale_state = 'upstream_changed', updated_at = now()
		FROM storyboard_shots shot
		JOIN script_scenes scene ON scene.id = shot.script_scene_id
		WHERE r.storyboard_shot_id = shot.id
		  AND r.project_id = $1
		  AND shot.project_id = $1
		  AND scene.project_id = $1
		  AND scene.script_version_id = $2
		  AND scene.deleted_at IS NULL
		  AND shot.deleted_at IS NULL
	`, projectID, versionID); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE storyboard_shots shot
		SET stale_state = 'needs_regeneration',
		    image_prompt_status = 'not_started',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULL,
		    image_prompt_updated_at = now(),
		    image_status = CASE
		      WHEN shot.image_artifact_id IS NOT NULL OR shot.image_media_file_id IS NOT NULL OR COALESCE(shot.image_storage_key, '') <> '' THEN 'stale'
		      ELSE shot.image_status
		    END,
		    video_status = CASE
		      WHEN shot.video_artifact_id IS NOT NULL OR shot.video_media_file_id IS NOT NULL OR COALESCE(shot.video_storage_key, '') <> '' THEN 'stale'
		      ELSE shot.video_status
		    END,
		    updated_at = now()
		FROM script_scenes scene
		WHERE shot.script_scene_id = scene.id
		  AND shot.project_id = $1
		  AND scene.project_id = $1
		  AND scene.script_version_id = $2
		  AND scene.deleted_at IS NULL
		  AND shot.deleted_at IS NULL
	`, projectID, versionID)
	return err
}

func MarkProjectVideoRatioStale(ctx context.Context, db Execer, projectID, aspectRatio string) error {
	aspectRatio = strings.TrimSpace(aspectRatio)
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	if _, err := db.Exec(ctx, `
		UPDATE storyboard_shots
		SET stale_state = 'needs_regeneration',
		    image_prompt_status = 'not_started',
		    image_prompt_error_code = NULL,
		    image_prompt_error_message = NULL,
		    image_prompt_workflow_run_id = NULL,
		    image_prompt_updated_at = now(),
		    image_status = CASE
		      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
		      ELSE image_status
		    END,
		    video_status = CASE
		      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
		      ELSE video_status
		    END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'expectedAspectRatio', $2,
		      'videoRatioChangedAt', now()
		    ),
		    updated_at = now()
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND deleted_at IS NULL
		  AND (
		    image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> ''
		    OR video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> ''
		  )
	`, projectID, aspectRatio); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE project_timelines
		SET aspect_ratio = $2,
		    stale_state = 'needs_regeneration',
		    updated_at = now()
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID, aspectRatio); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE timeline_clips c
		SET stale_state = 'needs_regeneration',
		    updated_at = now()
		FROM project_timelines t
		WHERE t.id = c.timeline_id
		  AND t.project_id = $1
		  AND t.production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND c.production_generation_id = t.production_generation_id
	`, projectID)
	return err
}

func MarkProjectFrameRateStale(ctx context.Context, db Execer, projectID string, timelineTimebase int64, fpsNumerator, fpsDenominator int) error {
	if _, err := db.Exec(ctx, `
		UPDATE storyboard_plans
		SET active = false,
		    status = CASE WHEN status = 'ready' THEN 'archived' ELSE status END,
		    stale_state = 'upstream_changed',
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'frameRateChangedAt', now(),
		      'timelineTimebase', $2,
		      'fpsNumerator', $3,
		      'fpsDenominator', $4
		    )
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND stale_state = 'fresh'
	`, projectID, timelineTimebase, fpsNumerator, fpsDenominator); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE storyboard_shots
		SET stale_state = 'needs_regeneration',
		    image_status = CASE
		      WHEN image_artifact_id IS NOT NULL OR image_media_file_id IS NOT NULL OR COALESCE(image_storage_key, '') <> '' THEN 'stale'
		      ELSE image_status
		    END,
		    video_status = CASE
		      WHEN video_artifact_id IS NOT NULL OR video_media_file_id IS NOT NULL OR COALESCE(video_storage_key, '') <> '' THEN 'stale'
		      ELSE video_status
		    END,
		    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
		      'frameRateChangedAt', now(),
		      'timelineTimebase', $2,
		      'fpsNumerator', $3,
		      'fpsDenominator', $4
		    ),
		    updated_at = now()
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
		  AND deleted_at IS NULL
	`, projectID, timelineTimebase, fpsNumerator, fpsDenominator); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE project_timelines
		SET stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE timeline_clips
		SET stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1
		  AND production_generation_id = (SELECT active_video_production_generation_id FROM projects WHERE id = $1)
	`, projectID); err != nil {
		return err
	}
	return MarkFinalVideoStale(ctx, db, projectID, "")
}
