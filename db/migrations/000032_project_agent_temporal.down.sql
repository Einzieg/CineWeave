DROP INDEX IF EXISTS agent_tasks_temporal_workflow_idx;

ALTER TABLE agent_tasks
  DROP COLUMN IF EXISTS temporal_workflow_id;

DELETE FROM schema_migrations
WHERE version = '000032_project_agent_temporal';
