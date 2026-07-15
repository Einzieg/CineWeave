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
	if _, err := db.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET stale_state = 'upstream_changed',
		    updated_at = now()
		WHERE project_id = $1 AND asset_id = $2
	`, projectID, assetID); err != nil {
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
		  AND EXISTS (
			SELECT 1
			FROM shot_asset_requirements r
			WHERE r.storyboard_shot_id = s.id
			  AND r.asset_id = $2
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
	`, projectID, shotID)
	return err
}

func MarkRequirementDownstreamStale(ctx context.Context, db Execer, projectID, requirementID string) error {
	if _, err := db.Exec(ctx, `
		UPDATE shot_asset_requirements
		SET stale_state = 'needs_regeneration',
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
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
	`, projectID, requirementID)
	return err
}

func MarkFinalVideoStale(ctx context.Context, db Execer, projectID, workflowRunID string) error {
	_, err := db.Exec(ctx, `
		UPDATE artifacts
		SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('staleState', 'needs_regeneration')
		WHERE project_id = $1
		  AND type = 'final_video'
		  AND ($2 = '' OR workflow_run_id = $2::uuid)
	`, projectID, workflowRunID)
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
		WHERE project_id = $1 AND deleted_at IS NULL
	`, projectID, timelineTimebase, fpsNumerator, fpsDenominator); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE project_timelines
		SET stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1
	`, projectID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		UPDATE timeline_clips
		SET stale_state = 'needs_regeneration', updated_at = now()
		WHERE project_id = $1
	`, projectID); err != nil {
		return err
	}
	return MarkFinalVideoStale(ctx, db, projectID, "")
}
