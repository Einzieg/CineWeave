ALTER TABLE workflow_runs
  DROP CONSTRAINT IF EXISTS workflow_runs_status_check;

ALTER TABLE workflow_runs
  ADD CONSTRAINT workflow_runs_status_check
  CHECK (status IN ('pending', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped', 'waiting_review'));

DELETE FROM schema_migrations WHERE version = '000006_workflow_cancellation';
