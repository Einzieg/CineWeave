ALTER TABLE projects
  DROP CONSTRAINT IF EXISTS projects_active_final_video_version_fk;

ALTER TABLE projects
  DROP COLUMN IF EXISTS active_final_video_version_id;

DROP TRIGGER IF EXISTS timeline_clips_set_updated_at ON timeline_clips;
DROP TRIGGER IF EXISTS project_timelines_set_updated_at ON project_timelines;

DROP TABLE IF EXISTS final_video_versions;
DROP TABLE IF EXISTS timeline_clips;
DROP TABLE IF EXISTS project_timelines;

DELETE FROM schema_migrations WHERE version = '000020_timeline_final_video';
