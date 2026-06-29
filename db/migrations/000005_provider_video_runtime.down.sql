DROP INDEX IF EXISTS idx_provider_async_tasks_next_poll;
DROP INDEX IF EXISTS idx_provider_async_tasks_external;
DROP INDEX IF EXISTS idx_provider_async_tasks_org_status;

ALTER TABLE provider_async_tasks
  DROP COLUMN IF EXISTS cancelled_at,
  DROP COLUMN IF EXISTS completed_at,
  DROP COLUMN IF EXISTS started_at,
  DROP COLUMN IF EXISTS next_poll_at,
  DROP COLUMN IF EXISTS poll_count,
  DROP COLUMN IF EXISTS error_message,
  DROP COLUMN IF EXISTS error_code,
  DROP COLUMN IF EXISTS last_response_snapshot,
  DROP COLUMN IF EXISTS normalized_output,
  DROP COLUMN IF EXISTS input,
  DROP COLUMN IF EXISTS execution_mode,
  DROP COLUMN IF EXISTS task_type,
  DROP COLUMN IF EXISTS model_profile_key,
  DROP COLUMN IF EXISTS model_profile_binding_id,
  DROP COLUMN IF EXISTS model_profile_id,
  DROP COLUMN IF EXISTS credential_id,
  DROP COLUMN IF EXISTS node_run_id,
  DROP COLUMN IF EXISTS workflow_run_id,
  DROP COLUMN IF EXISTS project_id;

ALTER TABLE provider_call_logs
  DROP CONSTRAINT IF EXISTS provider_call_logs_execution_mode_check;

ALTER TABLE provider_call_logs
  ADD CONSTRAINT provider_call_logs_execution_mode_check
  CHECK (execution_mode IN ('sync', 'async', 'stream'));

DELETE FROM schema_migrations WHERE version = '000005_provider_video_runtime';
