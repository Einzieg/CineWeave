CREATE TABLE IF NOT EXISTS provider_connectors (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  connector_key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  is_official BOOLEAN NOT NULL DEFAULT false,
  manifest JSONB NOT NULL DEFAULT '{}',
  version TEXT NOT NULL DEFAULT 'v1',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_accounts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  connector_id UUID NOT NULL REFERENCES provider_connectors(id),
  name TEXT NOT NULL,
  base_url TEXT,
  auth_type TEXT NOT NULL DEFAULT 'bearer' CHECK (auth_type IN ('none', 'bearer', 'api_key', 'basic')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'error')),
  config JSONB NOT NULL DEFAULT '{}',
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  credential_key TEXT NOT NULL DEFAULT 'default',
  credential_type TEXT NOT NULL DEFAULT 'api_key',
  secret_ref TEXT,
  encrypted_payload BYTEA,
  masked_preview TEXT,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'rotated', 'revoked', 'expired')),
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ,
  rotated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS provider_credentials_active_unique
  ON provider_credentials(provider_account_id, credential_key)
  WHERE is_active = true;

CREATE TABLE IF NOT EXISTS provider_models (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  model_key TEXT NOT NULL,
  display_name TEXT NOT NULL,
  modality TEXT NOT NULL CHECK (modality IN ('text', 'image', 'video', 'audio', 'embedding', 'multimodal')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'deprecated', 'error')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider_account_id, model_key)
);

CREATE TABLE IF NOT EXISTS provider_model_capabilities (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_model_id UUID NOT NULL REFERENCES provider_models(id) ON DELETE CASCADE,
  task_types JSONB NOT NULL DEFAULT '[]',
  input_limits JSONB NOT NULL DEFAULT '{}',
  output_limits JSONB NOT NULL DEFAULT '{}',
  quality_tiers JSONB NOT NULL DEFAULT '[]',
  provider_options_schema JSONB NOT NULL DEFAULT '{}',
  pricing_policy JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_endpoints (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  endpoint_key TEXT NOT NULL,
  endpoint_type TEXT NOT NULL,
  method TEXT NOT NULL,
  path_template TEXT NOT NULL,
  headers_template JSONB NOT NULL DEFAULT '{}',
  request_template JSONB NOT NULL DEFAULT '{}',
  response_mapping JSONB NOT NULL DEFAULT '{}',
  timeout_ms INT NOT NULL DEFAULT 120000,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider_account_id, endpoint_key)
);

CREATE TABLE IF NOT EXISTS provider_test_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  provider_model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
  test_type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'skipped')),
  request_snapshot JSONB NOT NULL DEFAULT '{}',
  response_snapshot JSONB,
  normalized_output JSONB,
  error_code TEXT,
  error_message TEXT,
  latency_ms INT,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS model_profiles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  profile_key TEXT NOT NULL,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL,
  routing_strategy TEXT NOT NULL DEFAULT 'priority' CHECK (routing_strategy IN ('priority', 'weighted', 'priority_with_fallback')),
  fallback_strategy JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, profile_key),
  UNIQUE(organization_id, purpose)
);

CREATE TABLE IF NOT EXISTS model_profile_bindings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  model_profile_id UUID NOT NULL REFERENCES model_profiles(id) ON DELETE CASCADE,
  provider_model_id UUID NOT NULL REFERENCES provider_models(id) ON DELETE CASCADE,
  priority INT NOT NULL DEFAULT 100,
  weight INT NOT NULL DEFAULT 100,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(model_profile_id, provider_model_id)
);

CREATE TABLE IF NOT EXISTS workflow_templates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  template_key TEXT NOT NULL,
  name TEXT NOT NULL,
  version TEXT NOT NULL DEFAULT 'v1',
  definition JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'draft')),
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, template_key, version)
);

CREATE TABLE IF NOT EXISTS workflow_template_nodes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id UUID NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
  node_key TEXT NOT NULL,
  node_type TEXT NOT NULL,
  config JSONB NOT NULL DEFAULT '{}',
  depends_on JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(template_id, node_key)
);

CREATE TABLE IF NOT EXISTS workflow_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  template_id UUID REFERENCES workflow_templates(id) ON DELETE SET NULL,
  temporal_workflow_id TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK (status IN ('pending', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped', 'waiting_review')),
  input JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  cancelled_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS workflow_node_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  node_key TEXT NOT NULL,
  node_type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped', 'waiting_review')),
  input JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  retry_count INT NOT NULL DEFAULT 0,
  error_code TEXT,
  error_message TEXT,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(workflow_run_id, node_key)
);

CREATE TABLE IF NOT EXISTS artifacts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
  type TEXT NOT NULL,
  storage_key TEXT,
  mime_type TEXT,
  content_hash TEXT,
  prompt_hash TEXT,
  model_id UUID REFERENCES provider_models(id) ON DELETE SET NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS prompt_templates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  template_key TEXT NOT NULL,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, template_key)
);

CREATE TABLE IF NOT EXISTS prompt_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  prompt_template_id UUID NOT NULL REFERENCES prompt_templates(id) ON DELETE CASCADE,
  version_no INT NOT NULL,
  content TEXT NOT NULL,
  variables_schema JSONB NOT NULL DEFAULT '{}',
  content_hash TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(prompt_template_id, version_no)
);

CREATE TABLE IF NOT EXISTS provider_call_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID REFERENCES provider_models(id),
  credential_id UUID REFERENCES provider_credentials(id),
  model_profile_id UUID REFERENCES model_profiles(id),
  model_profile_binding_id UUID REFERENCES model_profile_bindings(id),
  model_profile_key TEXT,
  prompt_version_id UUID REFERENCES prompt_versions(id),
  prompt_hash TEXT,
  input_hash TEXT,
  output_hash TEXT,
  task_type TEXT NOT NULL,
  execution_mode TEXT NOT NULL DEFAULT 'sync' CHECK (execution_mode IN ('sync', 'async', 'stream')),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped')),
  upstream_request_id TEXT,
  external_task_id TEXT,
  lease_id UUID,
  idempotency_key TEXT,
  latency_ms INT,
  input_tokens INT,
  output_tokens INT,
  media_count INT,
  duration_seconds NUMERIC,
  estimated_cost NUMERIC(18, 8),
  currency TEXT DEFAULT 'USD',
  error_code TEXT,
  error_message TEXT,
  upstream_status INT,
  upstream_error_code TEXT,
  request_hash TEXT,
  request_snapshot JSONB NOT NULL DEFAULT '{}',
  response_snapshot JSONB,
  normalized_output JSONB,
  artifact_ids JSONB NOT NULL DEFAULT '[]',
  media_file_ids JSONB NOT NULL DEFAULT '[]',
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_async_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_call_id UUID NOT NULL REFERENCES provider_call_logs(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID REFERENCES provider_models(id),
  external_task_id TEXT NOT NULL,
  status TEXT NOT NULL,
  poll_after TIMESTAMPTZ,
  result_expires_at TIMESTAMPTZ,
  raw_status JSONB NOT NULL DEFAULT '{}',
  last_poll_at TIMESTAMPTZ,
  finalized_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider_account_id, external_task_id)
);

CREATE TABLE IF NOT EXISTS cost_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
  provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
  provider_model_id UUID REFERENCES provider_models(id),
  credential_id UUID REFERENCES provider_credentials(id),
  model_profile_id UUID REFERENCES model_profiles(id),
  cost_type TEXT NOT NULL,
  amount NUMERIC(18, 8) NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  unit TEXT,
  quantity NUMERIC(18, 6),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_leases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID NOT NULL REFERENCES provider_models(id),
  task_type TEXT NOT NULL,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
  provider_call_id UUID REFERENCES provider_call_logs(id) ON DELETE SET NULL,
  acquired_by_service TEXT NOT NULL DEFAULT 'provider-gateway',
  status TEXT NOT NULL CHECK (status IN ('active', 'released', 'expired', 'cancelled')),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  released_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS event_outbox (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID,
  payload JSONB NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'publishing', 'published', 'failed')),
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  scope TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_snapshot JSONB,
  status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'succeeded', 'failed')),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, scope, key)
);

CREATE TABLE IF NOT EXISTS review_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
  node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
  review_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}',
  assigned_to UUID REFERENCES users(id),
  resolved_by UUID REFERENCES users(id),
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  actor_user_id UUID REFERENCES users(id),
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id UUID,
  metadata JSONB NOT NULL DEFAULT '{}',
  ip_address TEXT,
  user_agent TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS provider_accounts_set_updated_at ON provider_accounts;
CREATE TRIGGER provider_accounts_set_updated_at
BEFORE UPDATE ON provider_accounts
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS provider_models_set_updated_at ON provider_models;
CREATE TRIGGER provider_models_set_updated_at
BEFORE UPDATE ON provider_models
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS provider_endpoints_set_updated_at ON provider_endpoints;
CREATE TRIGGER provider_endpoints_set_updated_at
BEFORE UPDATE ON provider_endpoints
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS model_profiles_set_updated_at ON model_profiles;
CREATE TRIGGER model_profiles_set_updated_at
BEFORE UPDATE ON model_profiles
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS provider_async_tasks_set_updated_at ON provider_async_tasks;
CREATE TRIGGER provider_async_tasks_set_updated_at
BEFORE UPDATE ON provider_async_tasks
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS provider_accounts_organization_status_idx ON provider_accounts(organization_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS provider_accounts_connector_id_idx ON provider_accounts(connector_id);
CREATE INDEX IF NOT EXISTS provider_credentials_organization_id_idx ON provider_credentials(organization_id);
CREATE INDEX IF NOT EXISTS provider_credentials_account_id_idx ON provider_credentials(provider_account_id);
CREATE INDEX IF NOT EXISTS provider_models_account_status_idx ON provider_models(provider_account_id, status);
CREATE INDEX IF NOT EXISTS provider_model_capabilities_model_id_idx ON provider_model_capabilities(provider_model_id);
CREATE INDEX IF NOT EXISTS provider_endpoints_account_id_idx ON provider_endpoints(provider_account_id);
CREATE INDEX IF NOT EXISTS provider_test_runs_org_created_idx ON provider_test_runs(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS provider_test_runs_account_id_idx ON provider_test_runs(provider_account_id);
CREATE INDEX IF NOT EXISTS provider_test_runs_model_id_idx ON provider_test_runs(provider_model_id);
CREATE INDEX IF NOT EXISTS model_profiles_org_key_idx ON model_profiles(organization_id, profile_key);
CREATE INDEX IF NOT EXISTS model_profile_bindings_profile_id_idx ON model_profile_bindings(model_profile_id);
CREATE INDEX IF NOT EXISTS model_profile_bindings_model_id_idx ON model_profile_bindings(provider_model_id);
CREATE INDEX IF NOT EXISTS workflow_templates_org_idx ON workflow_templates(organization_id);
CREATE INDEX IF NOT EXISTS workflow_template_nodes_template_id_idx ON workflow_template_nodes(template_id);
CREATE INDEX IF NOT EXISTS workflow_runs_org_project_status_idx ON workflow_runs(organization_id, project_id, status);
CREATE INDEX IF NOT EXISTS workflow_runs_template_id_idx ON workflow_runs(template_id);
CREATE INDEX IF NOT EXISTS workflow_node_runs_workflow_status_idx ON workflow_node_runs(workflow_run_id, status);
CREATE INDEX IF NOT EXISTS artifacts_org_project_idx ON artifacts(organization_id, project_id);
CREATE INDEX IF NOT EXISTS artifacts_workflow_id_idx ON artifacts(workflow_run_id);
CREATE INDEX IF NOT EXISTS artifacts_node_run_id_idx ON artifacts(node_run_id);
CREATE INDEX IF NOT EXISTS artifacts_model_id_idx ON artifacts(model_id);
CREATE INDEX IF NOT EXISTS prompt_templates_org_key_idx ON prompt_templates(organization_id, template_key);
CREATE INDEX IF NOT EXISTS prompt_versions_template_id_idx ON prompt_versions(prompt_template_id);
CREATE INDEX IF NOT EXISTS provider_call_logs_org_created_idx ON provider_call_logs(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS provider_call_logs_model_created_idx ON provider_call_logs(provider_model_id, created_at DESC);
CREATE INDEX IF NOT EXISTS provider_call_logs_account_created_idx ON provider_call_logs(provider_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS provider_call_logs_project_id_idx ON provider_call_logs(project_id);
CREATE INDEX IF NOT EXISTS provider_call_logs_status_idx ON provider_call_logs(status);
CREATE INDEX IF NOT EXISTS provider_call_logs_credential_id_idx ON provider_call_logs(credential_id);
CREATE INDEX IF NOT EXISTS provider_call_logs_profile_id_idx ON provider_call_logs(model_profile_id);
CREATE INDEX IF NOT EXISTS provider_async_tasks_call_id_idx ON provider_async_tasks(provider_call_id);
CREATE INDEX IF NOT EXISTS provider_async_tasks_org_status_idx ON provider_async_tasks(organization_id, status);
CREATE INDEX IF NOT EXISTS cost_records_org_created_idx ON cost_records(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS cost_records_call_id_idx ON cost_records(provider_call_id);
CREATE INDEX IF NOT EXISTS provider_leases_status_expires_idx ON provider_leases(status, expires_at);
CREATE INDEX IF NOT EXISTS provider_leases_org_model_idx ON provider_leases(organization_id, provider_model_id);
CREATE INDEX IF NOT EXISTS event_outbox_pending_idx ON event_outbox(next_attempt_at, created_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idempotency_keys_org_scope_idx ON idempotency_keys(organization_id, scope);
CREATE INDEX IF NOT EXISTS review_tasks_org_status_idx ON review_tasks(organization_id, status);
CREATE INDEX IF NOT EXISTS audit_logs_org_created_idx ON audit_logs(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_actor_user_id_idx ON audit_logs(actor_user_id);

INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
VALUES
  (
    'openai_compatible',
    'OpenAI Compatible',
    'http',
    true,
    '{"kind":"ProviderConnector","version":"v1","id":"openai_compatible","name":"OpenAI Compatible","transport":"http","auth":{"type":"bearer"},"models":[],"endpoints":{"models":{"method":"GET","path":"/models"},"chatCompletions":{"method":"POST","path":"/chat/completions"}}}',
    'v1'
  ),
  (
    'declarative_http',
    'Declarative HTTP Provider',
    'http',
    false,
    '{"kind":"ProviderConnector","version":"v1","id":"declarative_http","name":"Declarative HTTP Provider","transport":"http","auth":{"type":"bearer"},"models":[],"endpoints":{}}',
    'v1'
  )
ON CONFLICT (connector_key) DO UPDATE SET
  name = EXCLUDED.name,
  type = EXCLUDED.type,
  is_official = EXCLUDED.is_official,
  manifest = EXCLUDED.manifest,
  version = EXCLUDED.version;

INSERT INTO schema_migrations(version) VALUES ('000002_provider_core')
ON CONFLICT (version) DO NOTHING;
