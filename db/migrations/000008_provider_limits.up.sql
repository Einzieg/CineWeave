ALTER TABLE provider_call_logs
  DROP CONSTRAINT IF EXISTS provider_call_logs_status_check;

ALTER TABLE provider_call_logs
  ADD CONSTRAINT provider_call_logs_status_check
  CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped', 'blocked'));

ALTER TABLE provider_leases
  ALTER COLUMN provider_model_id DROP NOT NULL,
  ADD COLUMN IF NOT EXISTS lease_token TEXT;

UPDATE provider_leases
SET lease_token = 'legacy-' || id::text
WHERE lease_token IS NULL OR lease_token = '';

ALTER TABLE provider_leases
  ALTER COLUMN lease_token SET NOT NULL;

ALTER TABLE provider_leases
  DROP CONSTRAINT IF EXISTS provider_leases_lease_token_key;

ALTER TABLE provider_leases
  ADD CONSTRAINT provider_leases_lease_token_key UNIQUE (lease_token);

CREATE TABLE IF NOT EXISTS provider_limit_policies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  provider_model_id UUID NULL REFERENCES provider_models(id) ON DELETE CASCADE,

  task_type TEXT NOT NULL,

  max_concurrency INTEGER NULL,
  requests_per_minute INTEGER NULL,
  requests_per_day INTEGER NULL,

  daily_budget NUMERIC(18, 8) NULL,
  monthly_budget NUMERIC(18, 8) NULL,
  currency TEXT NOT NULL DEFAULT 'USD',

  failure_threshold INTEGER NULL,
  failure_window_seconds INTEGER NULL,
  circuit_cooldown_seconds INTEGER NULL,

  enabled BOOLEAN NOT NULL DEFAULT true,

  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS provider_limit_policies_set_updated_at ON provider_limit_policies;
CREATE TRIGGER provider_limit_policies_set_updated_at
BEFORE UPDATE ON provider_limit_policies
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS idx_provider_limit_policies_org
  ON provider_limit_policies(organization_id);

CREATE INDEX IF NOT EXISTS idx_provider_limit_policies_account
  ON provider_limit_policies(provider_account_id);

CREATE INDEX IF NOT EXISTS idx_provider_limit_policies_model
  ON provider_limit_policies(provider_model_id);

CREATE INDEX IF NOT EXISTS idx_provider_limit_policies_task
  ON provider_limit_policies(organization_id, task_type, enabled);

CREATE TABLE IF NOT EXISTS provider_circuit_states (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  provider_model_id UUID NULL REFERENCES provider_models(id) ON DELETE SET NULL,
  task_type TEXT NOT NULL,

  state TEXT NOT NULL DEFAULT 'closed' CHECK (state IN ('closed', 'open', 'half_open')),
  failure_count INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  opened_at TIMESTAMPTZ NULL,
  half_open_at TIMESTAMPTZ NULL,
  next_attempt_at TIMESTAMPTZ NULL,
  last_error_code TEXT NULL,
  last_error_message TEXT NULL,

  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE NULLS NOT DISTINCT (organization_id, provider_account_id, provider_model_id, task_type)
);

CREATE INDEX IF NOT EXISTS idx_provider_circuit_states_org
  ON provider_circuit_states(organization_id);

CREATE INDEX IF NOT EXISTS idx_provider_circuit_states_account
  ON provider_circuit_states(provider_account_id);

CREATE INDEX IF NOT EXISTS idx_provider_circuit_states_task
  ON provider_circuit_states(organization_id, task_type, state);

CREATE INDEX IF NOT EXISTS idx_provider_leases_active
  ON provider_leases(
    organization_id,
    provider_account_id,
    provider_model_id,
    task_type,
    status,
    expires_at
  );

CREATE INDEX IF NOT EXISTS idx_provider_leases_expiry
  ON provider_leases(status, expires_at);

CREATE INDEX IF NOT EXISTS idx_provider_call_logs_limit_window
  ON provider_call_logs(
    organization_id,
    provider_account_id,
    provider_model_id,
    task_type,
    created_at
  );

CREATE INDEX IF NOT EXISTS idx_cost_records_budget_window
  ON cost_records(
    organization_id,
    provider_model_id,
    currency,
    created_at
  );

INSERT INTO schema_migrations(version) VALUES ('000008_provider_limits')
ON CONFLICT (version) DO NOTHING;
