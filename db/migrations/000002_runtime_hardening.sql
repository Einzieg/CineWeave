-- +goose Up

SET search_path TO public;

CREATE TABLE provider_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
    task_type TEXT NOT NULL,
    idempotency_key TEXT,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_generation INTEGER NOT NULL DEFAULT 1,
    result_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    artifact_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    media_file_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_code TEXT,
    error_message TEXT,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_requests_status_check CHECK (
        status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'unknown_outcome')
    ),
    CONSTRAINT provider_requests_attempt_generation_check CHECK (attempt_generation > 0),
    CONSTRAINT provider_requests_idempotency_key_check CHECK (
        idempotency_key IS NULL OR btrim(idempotency_key) <> ''
    ),
    CONSTRAINT provider_requests_result_snapshot_check CHECK (jsonb_typeof(result_snapshot) = 'object'),
    CONSTRAINT provider_requests_artifact_ids_check CHECK (jsonb_typeof(artifact_ids) = 'array'),
    CONSTRAINT provider_requests_media_file_ids_check CHECK (jsonb_typeof(media_file_ids) = 'array')
);

CREATE UNIQUE INDEX provider_requests_idempotency_uidx
    ON provider_requests (organization_id, task_type, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX provider_requests_workflow_idx
    ON provider_requests (workflow_run_id, node_run_id, created_at DESC);

CREATE INDEX provider_requests_status_idx
    ON provider_requests (status, updated_at)
    WHERE status IN ('pending', 'running', 'unknown_outcome');

ALTER TABLE provider_call_logs
    ADD COLUMN provider_request_id UUID REFERENCES provider_requests(id) ON DELETE SET NULL,
    ADD COLUMN attempt_generation INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN attempt_sequence INTEGER NOT NULL DEFAULT 1;

ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_attempt_generation_check CHECK (attempt_generation > 0),
    ADD CONSTRAINT provider_call_logs_attempt_sequence_check CHECK (attempt_sequence > 0);

CREATE UNIQUE INDEX provider_call_logs_request_attempt_uidx
    ON provider_call_logs (provider_request_id, attempt_generation, attempt_sequence)
    WHERE provider_request_id IS NOT NULL;

ALTER TABLE provider_async_tasks
    ADD COLUMN provider_request_id UUID REFERENCES provider_requests(id) ON DELETE SET NULL;

CREATE INDEX provider_async_tasks_provider_request_idx
    ON provider_async_tasks (provider_request_id)
    WHERE provider_request_id IS NOT NULL;

ALTER TABLE workflow_runs
    ADD COLUMN workflow_type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN total_items INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN completed_items INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN failed_items INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN root_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    ADD COLUMN retry_of_workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE workflow_runs
    ADD CONSTRAINT workflow_runs_workflow_type_check CHECK (btrim(workflow_type) <> ''),
    ADD CONSTRAINT workflow_runs_progress_check CHECK (
        total_items >= 0
        AND completed_items >= 0
        AND failed_items >= 0
        AND completed_items + failed_items <= total_items
    ),
    ADD CONSTRAINT workflow_runs_revision_check CHECK (revision > 0);

CREATE INDEX workflow_runs_activity_idx
    ON workflow_runs (organization_id, status, updated_at DESC);

CREATE INDEX workflow_runs_root_idx
    ON workflow_runs (root_workflow_run_id, created_at)
    WHERE root_workflow_run_id IS NOT NULL;

CREATE INDEX workflow_runs_retry_idx
    ON workflow_runs (retry_of_workflow_run_id)
    WHERE retry_of_workflow_run_id IS NOT NULL;

CREATE TABLE workflow_start_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL UNIQUE REFERENCES workflow_runs(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_type TEXT NOT NULL,
    temporal_workflow_id TEXT NOT NULL UNIQUE,
    task_queue TEXT NOT NULL,
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 12,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workflow_start_outbox_status_check CHECK (
        status IN ('pending', 'processing', 'started', 'failed', 'cancelled')
    ),
    CONSTRAINT workflow_start_outbox_attempts_check CHECK (
        attempt_count >= 0 AND max_attempts > 0 AND attempt_count <= max_attempts
    ),
    CONSTRAINT workflow_start_outbox_workflow_type_check CHECK (btrim(workflow_type) <> ''),
    CONSTRAINT workflow_start_outbox_task_queue_check CHECK (btrim(task_queue) <> ''),
    CONSTRAINT workflow_start_outbox_input_check CHECK (jsonb_typeof(input) = 'object')
);

CREATE INDEX workflow_start_outbox_dispatch_idx
    ON workflow_start_outbox (status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing');

CREATE INDEX workflow_start_outbox_queued_reconcile_idx
    ON workflow_start_outbox (created_at)
    WHERE status = 'pending';

ALTER TABLE canonical_assets
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN prompt_revision BIGINT NOT NULL DEFAULT 1;

ALTER TABLE canonical_assets
    ADD CONSTRAINT canonical_assets_revision_check CHECK (revision > 0),
    ADD CONSTRAINT canonical_assets_prompt_revision_check CHECK (prompt_revision > 0);

CREATE TABLE system_seed_versions (
    resource_key TEXT PRIMARY KEY,
    resource_version INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    record_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    release_id TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT system_seed_versions_resource_key_check CHECK (btrim(resource_key) <> ''),
    CONSTRAINT system_seed_versions_resource_version_check CHECK (resource_version > 0),
    CONSTRAINT system_seed_versions_content_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT system_seed_versions_record_counts_check CHECK (jsonb_typeof(record_counts) = 'object')
);

ALTER TABLE permissions
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE permissions
    ADD CONSTRAINT permissions_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE roles
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE roles
    ADD CONSTRAINT roles_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE role_permissions
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE role_permissions
    ADD CONSTRAINT role_permissions_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE provider_connectors
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE provider_connectors
    ADD CONSTRAINT provider_connectors_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE provider_catalog_entries
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE provider_catalog_entries
    ADD CONSTRAINT provider_catalog_entries_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE provider_model_capability_presets
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE provider_model_capability_presets
    ADD CONSTRAINT provider_model_capability_presets_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE prompt_templates
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE prompt_templates
    ADD CONSTRAINT prompt_templates_managed_by_check CHECK (managed_by IN ('system', 'user'));

ALTER TABLE prompt_versions
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'user';
ALTER TABLE prompt_versions
    ADD CONSTRAINT prompt_versions_managed_by_check CHECK (managed_by IN ('system', 'user'));

-- +goose Down

ALTER TABLE prompt_versions DROP CONSTRAINT IF EXISTS prompt_versions_managed_by_check;
ALTER TABLE prompt_versions DROP COLUMN IF EXISTS managed_by;
ALTER TABLE prompt_templates DROP CONSTRAINT IF EXISTS prompt_templates_managed_by_check;
ALTER TABLE prompt_templates DROP COLUMN IF EXISTS managed_by;
ALTER TABLE provider_model_capability_presets DROP CONSTRAINT IF EXISTS provider_model_capability_presets_managed_by_check;
ALTER TABLE provider_model_capability_presets DROP COLUMN IF EXISTS managed_by;
ALTER TABLE provider_catalog_entries DROP CONSTRAINT IF EXISTS provider_catalog_entries_managed_by_check;
ALTER TABLE provider_catalog_entries DROP COLUMN IF EXISTS managed_by;
ALTER TABLE provider_connectors DROP CONSTRAINT IF EXISTS provider_connectors_managed_by_check;
ALTER TABLE provider_connectors DROP COLUMN IF EXISTS managed_by;
ALTER TABLE role_permissions DROP CONSTRAINT IF EXISTS role_permissions_managed_by_check;
ALTER TABLE role_permissions DROP COLUMN IF EXISTS managed_by;
ALTER TABLE roles DROP CONSTRAINT IF EXISTS roles_managed_by_check;
ALTER TABLE roles DROP COLUMN IF EXISTS managed_by;
ALTER TABLE permissions DROP CONSTRAINT IF EXISTS permissions_managed_by_check;
ALTER TABLE permissions DROP COLUMN IF EXISTS managed_by;

DROP TABLE IF EXISTS system_seed_versions;

ALTER TABLE canonical_assets DROP CONSTRAINT IF EXISTS canonical_assets_prompt_revision_check;
ALTER TABLE canonical_assets DROP CONSTRAINT IF EXISTS canonical_assets_revision_check;
ALTER TABLE canonical_assets DROP COLUMN IF EXISTS prompt_revision;
ALTER TABLE canonical_assets DROP COLUMN IF EXISTS revision;

DROP TABLE IF EXISTS workflow_start_outbox;

ALTER TABLE workflow_runs DROP CONSTRAINT IF EXISTS workflow_runs_revision_check;
ALTER TABLE workflow_runs DROP CONSTRAINT IF EXISTS workflow_runs_progress_check;
ALTER TABLE workflow_runs DROP CONSTRAINT IF EXISTS workflow_runs_workflow_type_check;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS updated_at;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS retry_of_workflow_run_id;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS root_workflow_run_id;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS revision;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS failed_items;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS completed_items;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS total_items;
ALTER TABLE workflow_runs DROP COLUMN IF EXISTS workflow_type;

DROP INDEX IF EXISTS provider_async_tasks_provider_request_idx;
ALTER TABLE provider_async_tasks DROP COLUMN IF EXISTS provider_request_id;

DROP INDEX IF EXISTS provider_call_logs_request_attempt_uidx;
ALTER TABLE provider_call_logs DROP CONSTRAINT IF EXISTS provider_call_logs_attempt_sequence_check;
ALTER TABLE provider_call_logs DROP CONSTRAINT IF EXISTS provider_call_logs_attempt_generation_check;
ALTER TABLE provider_call_logs DROP COLUMN IF EXISTS attempt_sequence;
ALTER TABLE provider_call_logs DROP COLUMN IF EXISTS attempt_generation;
ALTER TABLE provider_call_logs DROP COLUMN IF EXISTS provider_request_id;

DROP TABLE IF EXISTS provider_requests;
