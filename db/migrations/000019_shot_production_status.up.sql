ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS image_status TEXT NOT NULL DEFAULT 'not_started',
  ADD COLUMN IF NOT EXISTS video_status TEXT NOT NULL DEFAULT 'not_started',
  ADD COLUMN IF NOT EXISTS image_error_code TEXT NULL,
  ADD COLUMN IF NOT EXISTS image_error_message TEXT NULL,
  ADD COLUMN IF NOT EXISTS video_error_code TEXT NULL,
  ADD COLUMN IF NOT EXISTS video_error_message TEXT NULL,
  ADD COLUMN IF NOT EXISTS image_started_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS image_completed_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS video_started_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS video_completed_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS image_workflow_run_id UUID NULL REFERENCES workflow_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS video_workflow_run_id UUID NULL REFERENCES workflow_runs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_image_status
  ON storyboard_shots(project_id, image_status)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_video_status
  ON storyboard_shots(project_id, video_status)
  WHERE deleted_at IS NULL;

INSERT INTO schema_migrations(version) VALUES ('000019_shot_production_status')
ON CONFLICT (version) DO NOTHING;
