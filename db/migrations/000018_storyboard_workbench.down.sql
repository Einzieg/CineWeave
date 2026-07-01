DROP INDEX IF EXISTS idx_storyboard_shots_project_scene;

DROP INDEX IF EXISTS idx_storyboard_shots_workflow_unique;

CREATE UNIQUE INDEX IF NOT EXISTS idx_storyboard_shots_workflow_unique
  ON storyboard_shots(workflow_run_id, shot_index);

ALTER TABLE storyboard_shots
  DROP COLUMN IF EXISTS deleted_at;

DELETE FROM schema_migrations WHERE version = '000018_storyboard_workbench';
