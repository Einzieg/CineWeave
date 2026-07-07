ALTER TABLE agent_tasks
  ADD COLUMN IF NOT EXISTS temporal_workflow_id TEXT;

CREATE INDEX IF NOT EXISTS agent_tasks_temporal_workflow_idx
  ON agent_tasks(temporal_workflow_id)
  WHERE temporal_workflow_id IS NOT NULL;

INSERT INTO schema_migrations(version)
VALUES ('000032_project_agent_temporal')
ON CONFLICT (version) DO NOTHING;
