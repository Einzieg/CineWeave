ALTER TABLE storyboard_shots
  ALTER COLUMN storyboard_id DROP NOT NULL;

ALTER TABLE storyboard_shots
  ADD COLUMN IF NOT EXISTS workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS storyboard_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS shot_no INTEGER,
  ADD COLUMN IF NOT EXISTS title TEXT,
  ADD COLUMN IF NOT EXISTS visual TEXT,
  ADD COLUMN IF NOT EXISTS camera TEXT,
  ADD COLUMN IF NOT EXISTS motion TEXT,
  ADD COLUMN IF NOT EXISTS mood TEXT,
  ADD COLUMN IF NOT EXISTS image_prompt TEXT,
  ADD COLUMN IF NOT EXISTS video_prompt TEXT,
  ADD COLUMN IF NOT EXISTS image_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS image_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS image_storage_key TEXT,
  ADD COLUMN IF NOT EXISTS video_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS video_media_file_id UUID REFERENCES media_files(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS video_storage_key TEXT,
  ADD COLUMN IF NOT EXISTS video_provider_async_task_id UUID REFERENCES provider_async_tasks(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS video_external_task_id TEXT,
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';

UPDATE storyboard_shots
SET shot_no = shot_index + 1
WHERE shot_no IS NULL;

UPDATE storyboard_shots
SET visual = COALESCE(visual, action),
    camera = COALESCE(camera, camera_move),
    image_prompt = COALESCE(image_prompt, action),
    video_prompt = COALESCE(video_prompt, action)
WHERE action IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_storyboard_shots_workflow_unique
  ON storyboard_shots(workflow_run_id, shot_index);

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_project
  ON storyboard_shots(project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_workflow
  ON storyboard_shots(workflow_run_id, shot_index);

CREATE INDEX IF NOT EXISTS idx_storyboard_shots_status
  ON storyboard_shots(workflow_run_id, status);

INSERT INTO schema_migrations(version) VALUES ('000011_storyboard_shots')
ON CONFLICT (version) DO NOTHING;
