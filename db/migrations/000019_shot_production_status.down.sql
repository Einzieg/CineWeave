DROP INDEX IF EXISTS idx_storyboard_shots_video_status;

DROP INDEX IF EXISTS idx_storyboard_shots_image_status;

ALTER TABLE storyboard_shots
  DROP COLUMN IF EXISTS video_workflow_run_id,
  DROP COLUMN IF EXISTS image_workflow_run_id,
  DROP COLUMN IF EXISTS video_completed_at,
  DROP COLUMN IF EXISTS video_started_at,
  DROP COLUMN IF EXISTS image_completed_at,
  DROP COLUMN IF EXISTS image_started_at,
  DROP COLUMN IF EXISTS video_error_message,
  DROP COLUMN IF EXISTS video_error_code,
  DROP COLUMN IF EXISTS image_error_message,
  DROP COLUMN IF EXISTS image_error_code,
  DROP COLUMN IF EXISTS video_status,
  DROP COLUMN IF EXISTS image_status;

DELETE FROM schema_migrations WHERE version = '000019_shot_production_status';
