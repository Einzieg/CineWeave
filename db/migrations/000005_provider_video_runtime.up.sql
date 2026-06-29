ALTER TABLE provider_call_logs
  DROP CONSTRAINT IF EXISTS provider_call_logs_execution_mode_check;

ALTER TABLE provider_call_logs
  ADD CONSTRAINT provider_call_logs_execution_mode_check
  CHECK (execution_mode IN ('sync', 'async', 'stream', 'async_create', 'async_poll'));

ALTER TABLE provider_async_tasks
  ALTER COLUMN external_task_id DROP NOT NULL;

ALTER TABLE provider_async_tasks
  ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS credential_id UUID REFERENCES provider_credentials(id),
  ADD COLUMN IF NOT EXISTS model_profile_id UUID REFERENCES model_profiles(id),
  ADD COLUMN IF NOT EXISTS model_profile_binding_id UUID REFERENCES model_profile_bindings(id),
  ADD COLUMN IF NOT EXISTS model_profile_key TEXT,
  ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'video.generate',
  ADD COLUMN IF NOT EXISTS execution_mode TEXT NOT NULL DEFAULT 'async_polling',
  ADD COLUMN IF NOT EXISTS input JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS normalized_output JSONB,
  ADD COLUMN IF NOT EXISTS last_response_snapshot JSONB,
  ADD COLUMN IF NOT EXISTS error_code TEXT,
  ADD COLUMN IF NOT EXISTS error_message TEXT,
  ADD COLUMN IF NOT EXISTS poll_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS next_poll_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;

UPDATE provider_async_tasks
SET task_type = 'video.generate'
WHERE task_type IS NULL OR task_type = '';

UPDATE provider_async_tasks
SET execution_mode = 'async_polling'
WHERE execution_mode IS NULL OR execution_mode = '';

CREATE INDEX IF NOT EXISTS idx_provider_async_tasks_org_status
  ON provider_async_tasks(organization_id, status);

CREATE INDEX IF NOT EXISTS idx_provider_async_tasks_external
  ON provider_async_tasks(provider_account_id, external_task_id);

CREATE INDEX IF NOT EXISTS idx_provider_async_tasks_next_poll
  ON provider_async_tasks(status, next_poll_at);

INSERT INTO schema_migrations(version) VALUES ('000005_provider_video_runtime')
ON CONFLICT (version) DO NOTHING;
