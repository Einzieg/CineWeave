DELETE FROM schema_migrations WHERE version = '000037_script_version_scene_archive';

DROP INDEX IF EXISTS idx_script_scenes_project_deleted;
DROP INDEX IF EXISTS idx_script_versions_project_status;

ALTER TABLE script_scenes
  DROP COLUMN IF EXISTS deleted_at;

ALTER TABLE script_versions
  DROP CONSTRAINT IF EXISTS script_versions_status_check,
  DROP COLUMN IF EXISTS status;
