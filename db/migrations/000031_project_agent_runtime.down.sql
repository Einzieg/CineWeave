ALTER TABLE agent_runs
  DROP COLUMN IF EXISTS step_id,
  DROP COLUMN IF EXISTS task_id;

DROP TRIGGER IF EXISTS agent_approvals_set_updated_at ON agent_approvals;
DROP TRIGGER IF EXISTS agent_steps_set_updated_at ON agent_steps;
DROP TRIGGER IF EXISTS agent_tasks_set_updated_at ON agent_tasks;

DROP TABLE IF EXISTS agent_approvals;
DROP TABLE IF EXISTS agent_steps;
DROP TABLE IF EXISTS agent_tasks;

ALTER TABLE agent_sessions
  DROP CONSTRAINT IF EXISTS agent_sessions_agent_type_check;

ALTER TABLE agent_sessions
  ADD CONSTRAINT agent_sessions_agent_type_check
  CHECK (agent_type IN ('script_agent', 'asset_agent', 'storyboard_agent', 'shot_asset_agent'));

DELETE FROM schema_migrations
WHERE version = '000031_project_agent_runtime';
