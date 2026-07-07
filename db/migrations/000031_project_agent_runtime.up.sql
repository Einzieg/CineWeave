ALTER TABLE agent_sessions
  DROP CONSTRAINT IF EXISTS agent_sessions_agent_type_check;

ALTER TABLE agent_sessions
  ADD CONSTRAINT agent_sessions_agent_type_check
  CHECK (agent_type IN ('script_agent', 'asset_agent', 'storyboard_agent', 'shot_asset_agent', 'project_agent'));

CREATE TABLE IF NOT EXISTS agent_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id UUID REFERENCES agent_sessions(id) ON DELETE SET NULL,
  agent_type TEXT NOT NULL DEFAULT 'project_agent',
  user_goal TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'supervised' CHECK (mode IN ('plan_only', 'supervised', 'auto_low_risk')),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (
    status IN ('queued', 'planning', 'waiting_approval', 'running', 'succeeded', 'failed', 'blocked', 'cancelled')
  ),
  temporal_workflow_id TEXT,
  constraints JSONB NOT NULL DEFAULT '{}',
  plan JSONB NOT NULL DEFAULT '{}',
  summary JSONB NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS agent_steps (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
  step_index INTEGER NOT NULL,
  tool_name TEXT NOT NULL,
  risk TEXT NOT NULL CHECK (risk IN ('read', 'draft', 'write', 'workflow', 'costed', 'destructive', 'admin')),
  permission TEXT,
  status TEXT NOT NULL DEFAULT 'planned' CHECK (
    status IN ('planned', 'waiting_approval', 'approved', 'running', 'succeeded', 'failed', 'blocked', 'skipped', 'cancelled')
  ),
  requires_approval BOOLEAN NOT NULL DEFAULT false,
  input JSONB NOT NULL DEFAULT '{}',
  dry_run_output JSONB NOT NULL DEFAULT '{}',
  supervisor_decision JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  verifier_output JSONB NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  UNIQUE(task_id, step_index)
);

CREATE TABLE IF NOT EXISTS agent_approvals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
  step_id UUID REFERENCES agent_steps(id) ON DELETE CASCADE,
  approval_type TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
  requested_payload JSONB NOT NULL DEFAULT '{}',
  decision_payload JSONB NOT NULL DEFAULT '{}',
  decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
  decided_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_runs
  ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS step_id UUID REFERENCES agent_steps(id) ON DELETE SET NULL;

DROP TRIGGER IF EXISTS agent_tasks_set_updated_at ON agent_tasks;
CREATE TRIGGER agent_tasks_set_updated_at
BEFORE UPDATE ON agent_tasks
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS agent_steps_set_updated_at ON agent_steps;
CREATE TRIGGER agent_steps_set_updated_at
BEFORE UPDATE ON agent_steps
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS agent_approvals_set_updated_at ON agent_approvals;
CREATE TRIGGER agent_approvals_set_updated_at
BEFORE UPDATE ON agent_approvals
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS agent_tasks_project_status_idx
  ON agent_tasks(project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS agent_tasks_org_status_idx
  ON agent_tasks(organization_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS agent_tasks_session_idx
  ON agent_tasks(session_id, created_at DESC)
  WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS agent_steps_task_status_idx
  ON agent_steps(task_id, status, step_index);
CREATE INDEX IF NOT EXISTS agent_steps_tool_idx
  ON agent_steps(tool_name, status);
CREATE INDEX IF NOT EXISTS agent_approvals_task_status_idx
  ON agent_approvals(task_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS agent_approvals_step_idx
  ON agent_approvals(step_id)
  WHERE step_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS agent_runs_task_idx
  ON agent_runs(task_id)
  WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS agent_runs_step_idx
  ON agent_runs(step_id)
  WHERE step_id IS NOT NULL;

INSERT INTO schema_migrations(version)
VALUES ('000031_project_agent_runtime')
ON CONFLICT (version) DO NOTHING;
