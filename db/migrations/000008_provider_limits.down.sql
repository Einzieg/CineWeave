DROP INDEX IF EXISTS idx_cost_records_budget_window;
DROP INDEX IF EXISTS idx_provider_call_logs_limit_window;
DROP INDEX IF EXISTS idx_provider_leases_expiry;
DROP INDEX IF EXISTS idx_provider_leases_active;

DROP TABLE IF EXISTS provider_circuit_states;

DROP TRIGGER IF EXISTS provider_limit_policies_set_updated_at ON provider_limit_policies;
DROP TABLE IF EXISTS provider_limit_policies;

ALTER TABLE provider_leases
  DROP CONSTRAINT IF EXISTS provider_leases_lease_token_key;

ALTER TABLE provider_leases
  DROP COLUMN IF EXISTS lease_token;

DELETE FROM provider_leases
WHERE provider_model_id IS NULL;

ALTER TABLE provider_leases
  ALTER COLUMN provider_model_id SET NOT NULL;

ALTER TABLE provider_call_logs
  DROP CONSTRAINT IF EXISTS provider_call_logs_status_check;

ALTER TABLE provider_call_logs
  ADD CONSTRAINT provider_call_logs_status_check
  CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped'));

DELETE FROM schema_migrations WHERE version = '000008_provider_limits';
