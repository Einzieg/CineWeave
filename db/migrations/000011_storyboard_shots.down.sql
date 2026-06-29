DROP INDEX IF EXISTS idx_storyboard_shots_status;
DROP INDEX IF EXISTS idx_storyboard_shots_workflow;
DROP INDEX IF EXISTS idx_storyboard_shots_project;
DROP INDEX IF EXISTS idx_storyboard_shots_workflow_unique;

ALTER TABLE storyboard_shots
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS video_external_task_id,
  DROP COLUMN IF EXISTS video_provider_async_task_id,
  DROP COLUMN IF EXISTS video_storage_key,
  DROP COLUMN IF EXISTS video_media_file_id,
  DROP COLUMN IF EXISTS video_artifact_id,
  DROP COLUMN IF EXISTS image_storage_key,
  DROP COLUMN IF EXISTS image_media_file_id,
  DROP COLUMN IF EXISTS image_artifact_id,
  DROP COLUMN IF EXISTS video_prompt,
  DROP COLUMN IF EXISTS image_prompt,
  DROP COLUMN IF EXISTS mood,
  DROP COLUMN IF EXISTS motion,
  DROP COLUMN IF EXISTS camera,
  DROP COLUMN IF EXISTS visual,
  DROP COLUMN IF EXISTS title,
  DROP COLUMN IF EXISTS shot_no,
  DROP COLUMN IF EXISTS storyboard_artifact_id,
  DROP COLUMN IF EXISTS workflow_run_id;

DELETE FROM schema_migrations WHERE version = '000011_storyboard_shots';
