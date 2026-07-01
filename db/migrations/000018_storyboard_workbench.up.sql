ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL;

DROP INDEX IF EXISTS idx_storyboard_shots_workflow_unique;

CREATE UNIQUE INDEX IF NOT EXISTS idx_storyboard_shots_workflow_unique
  ON storyboard_shots(workflow_run_id, shot_index)
  WHERE workflow_run_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_project_scene
  ON storyboard_shots(project_id, script_scene_id, shot_index)
  WHERE deleted_at IS NULL;

INSERT INTO schema_migrations(version) VALUES ('000018_storyboard_workbench')
ON CONFLICT (version) DO NOTHING;
