ALTER TABLE script_versions
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'script_versions_status_check'
  ) THEN
    ALTER TABLE script_versions
      ADD CONSTRAINT script_versions_status_check
      CHECK (status IN ('active', 'archived'));
  END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_script_versions_project_status
  ON script_versions(project_id, script_id, status, version DESC);

ALTER TABLE script_scenes
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_script_scenes_project_deleted
  ON script_scenes(project_id, deleted_at, script_id, scene_index);

INSERT INTO schema_migrations(version) VALUES ('000037_script_version_scene_archive')
ON CONFLICT (version) DO NOTHING;
