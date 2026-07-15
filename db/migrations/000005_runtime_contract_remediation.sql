-- +goose Up

SET search_path TO public;

CREATE TABLE project_event_streams (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    next_position BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_event_streams_next_position_check CHECK (next_position > 0)
);

CREATE TABLE project_event_log (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    stream_position BIGINT NOT NULL,
    event_id UUID NOT NULL,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '7 days'),
    PRIMARY KEY (project_id, stream_position),
    UNIQUE (event_id),
    CONSTRAINT project_event_log_position_check CHECK (stream_position > 0),
    CONSTRAINT project_event_log_payload_check CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX project_event_log_retention_idx
    ON project_event_log (expires_at, project_id, stream_position);

WITH ranked AS (
    SELECT
        id AS event_id,
        organization_id,
        project_id,
        event_type,
        aggregate_type,
        aggregate_id,
        payload,
        created_at,
        row_number() OVER (PARTITION BY project_id ORDER BY created_at, id) AS stream_position
    FROM event_outbox
    WHERE project_id IS NOT NULL
), inserted_streams AS (
    INSERT INTO project_event_streams(project_id, organization_id, next_position, created_at, updated_at)
    SELECT project_id, min(organization_id::text)::uuid, count(*) + 1, min(created_at), now()
    FROM ranked
    GROUP BY project_id
    ON CONFLICT (project_id) DO NOTHING
)
INSERT INTO project_event_log(
    project_id, stream_position, event_id, organization_id, event_type,
    aggregate_type, aggregate_id, payload, created_at, expires_at
)
SELECT
    project_id, stream_position, event_id, organization_id, event_type,
    aggregate_type, aggregate_id, payload, created_at, created_at + interval '7 days'
FROM ranked
ORDER BY project_id, stream_position;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION append_project_event_log()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    assigned_position BIGINT;
BEGIN
    IF NEW.project_id IS NULL THEN
        RETURN NEW;
    END IF;

    INSERT INTO project_event_streams(project_id, organization_id)
    VALUES (NEW.project_id, NEW.organization_id)
    ON CONFLICT (project_id) DO NOTHING;

    UPDATE project_event_streams
    SET next_position = next_position + 1,
        updated_at = now()
    WHERE project_id = NEW.project_id
    RETURNING next_position - 1 INTO assigned_position;

    INSERT INTO project_event_log(
        project_id, stream_position, event_id, organization_id, event_type,
        aggregate_type, aggregate_id, payload, created_at, expires_at
    )
    VALUES (
        NEW.project_id, assigned_position, NEW.id, NEW.organization_id, NEW.event_type,
        NEW.aggregate_type, NEW.aggregate_id, NEW.payload, NEW.created_at,
        NEW.created_at + interval '7 days'
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER event_outbox_project_event_log_trigger
AFTER INSERT ON event_outbox
FOR EACH ROW
EXECUTE FUNCTION append_project_event_log();

ALTER TABLE workflow_runs
    ADD COLUMN execution_token UUID NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN attempt_generation INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN cancellation_requested_at TIMESTAMPTZ,
    ADD COLUMN cancellation_deadline_at TIMESTAMPTZ,
    ADD COLUMN terminalized_at TIMESTAMPTZ,
    ADD CONSTRAINT workflow_runs_attempt_generation_check CHECK (attempt_generation > 0),
    ADD CONSTRAINT workflow_runs_cancellation_deadline_check CHECK (
        cancellation_deadline_at IS NULL
        OR cancellation_requested_at IS NULL
        OR cancellation_deadline_at >= cancellation_requested_at
    );

CREATE UNIQUE INDEX workflow_runs_execution_token_uidx ON workflow_runs(execution_token);
CREATE INDEX workflow_runs_cancellation_reconcile_idx
    ON workflow_runs(cancellation_deadline_at)
    WHERE status = 'cancelling';

ALTER TABLE workflow_node_runs
    ADD COLUMN execution_token UUID NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN attempt_generation INTEGER NOT NULL DEFAULT 1,
    ADD CONSTRAINT workflow_node_runs_attempt_generation_check CHECK (attempt_generation > 0);

CREATE UNIQUE INDEX workflow_node_runs_execution_token_uidx ON workflow_node_runs(execution_token);

CREATE TABLE runtime_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    operation_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'processing',
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    request_hash TEXT NOT NULL,
    hash_schema_version INTEGER NOT NULL DEFAULT 1,
    result_snapshot JSONB,
    error_code TEXT,
    error_message TEXT,
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT runtime_operations_type_check CHECK (btrim(operation_type) <> ''),
    CONSTRAINT runtime_operations_status_check CHECK (
        status IN ('processing', 'succeeded', 'failed_retryable', 'failed_terminal', 'unknown_outcome')
    ),
    CONSTRAINT runtime_operations_hash_schema_check CHECK (hash_schema_version > 0),
    CONSTRAINT runtime_operations_result_check CHECK (
        result_snapshot IS NULL OR jsonb_typeof(result_snapshot) IN ('object', 'array')
    )
);

CREATE INDEX runtime_operations_reconcile_idx
    ON runtime_operations(status, lease_expires_at, updated_at)
    WHERE status IN ('processing', 'unknown_outcome');

ALTER TABLE idempotency_keys
    DROP CONSTRAINT idempotency_keys_status_check,
    ADD COLUMN operation_id UUID REFERENCES runtime_operations(id) ON DELETE SET NULL,
    ADD COLUMN hash_schema_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN response_status INTEGER,
    ADD COLUMN error_code TEXT,
    ADD COLUMN error_message TEXT,
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD CONSTRAINT idempotency_keys_hash_schema_check CHECK (hash_schema_version > 0),
    ADD CONSTRAINT idempotency_keys_retry_count_check CHECK (retry_count >= 0);

UPDATE idempotency_keys
SET status = 'failed_retryable'
WHERE status = 'failed';

ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_keys_status_check CHECK (
        status IN ('processing', 'succeeded', 'failed_retryable', 'failed_terminal', 'unknown_outcome')
    );

CREATE INDEX idempotency_keys_reconcile_idx
    ON idempotency_keys(status, lease_expires_at, updated_at)
    WHERE status IN ('processing', 'unknown_outcome');

ALTER TABLE provider_requests
    ADD COLUMN hash_schema_version INTEGER NOT NULL DEFAULT 2,
    ADD CONSTRAINT provider_requests_hash_schema_check CHECK (hash_schema_version > 0);

ALTER TABLE provider_async_tasks
    ADD COLUMN cancellation_requested_at TIMESTAMPTZ,
    ADD COLUMN cancellation_deadline_at TIMESTAMPTZ,
    ADD COLUMN cancellation_error_code TEXT,
    ADD COLUMN cancellation_error_message TEXT;

CREATE INDEX provider_async_tasks_cancellation_reconcile_idx
    ON provider_async_tasks(cancellation_deadline_at)
    WHERE status = 'cancelling';

CREATE TABLE workflow_input_snapshots (
    workflow_run_id UUID PRIMARY KEY REFERENCES workflow_runs(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_revision BIGINT NOT NULL,
    snapshot JSONB NOT NULL,
    snapshot_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workflow_input_snapshots_revision_check CHECK (project_revision > 0),
    CONSTRAINT workflow_input_snapshots_snapshot_check CHECK (jsonb_typeof(snapshot) = 'object'),
    CONSTRAINT workflow_input_snapshots_hash_check CHECK (snapshot_hash ~ '^[0-9a-f]{64}$')
);

-- +goose Down

DROP TABLE IF EXISTS workflow_input_snapshots;

DROP INDEX IF EXISTS provider_async_tasks_cancellation_reconcile_idx;
ALTER TABLE provider_async_tasks
    DROP COLUMN IF EXISTS cancellation_error_message,
    DROP COLUMN IF EXISTS cancellation_error_code,
    DROP COLUMN IF EXISTS cancellation_deadline_at,
    DROP COLUMN IF EXISTS cancellation_requested_at;

ALTER TABLE provider_requests DROP CONSTRAINT IF EXISTS provider_requests_hash_schema_check;
ALTER TABLE provider_requests DROP COLUMN IF EXISTS hash_schema_version;

DROP INDEX IF EXISTS idempotency_keys_reconcile_idx;
ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_keys_status_check;
UPDATE idempotency_keys
SET status = 'failed'
WHERE status IN ('failed_retryable', 'failed_terminal', 'unknown_outcome');
ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_keys_retry_count_check;
ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_keys_hash_schema_check;
ALTER TABLE idempotency_keys
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS retry_count,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS error_code,
    DROP COLUMN IF EXISTS response_status,
    DROP COLUMN IF EXISTS hash_schema_version,
    DROP COLUMN IF EXISTS operation_id;
ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_keys_status_check CHECK (
        status IN ('processing', 'succeeded', 'failed')
    );

DROP TABLE IF EXISTS runtime_operations;

DROP INDEX IF EXISTS workflow_node_runs_execution_token_uidx;
ALTER TABLE workflow_node_runs DROP CONSTRAINT IF EXISTS workflow_node_runs_attempt_generation_check;
ALTER TABLE workflow_node_runs
    DROP COLUMN IF EXISTS attempt_generation,
    DROP COLUMN IF EXISTS execution_token;

DROP INDEX IF EXISTS workflow_runs_cancellation_reconcile_idx;
DROP INDEX IF EXISTS workflow_runs_execution_token_uidx;
ALTER TABLE workflow_runs DROP CONSTRAINT IF EXISTS workflow_runs_cancellation_deadline_check;
ALTER TABLE workflow_runs DROP CONSTRAINT IF EXISTS workflow_runs_attempt_generation_check;
ALTER TABLE workflow_runs
    DROP COLUMN IF EXISTS terminalized_at,
    DROP COLUMN IF EXISTS cancellation_deadline_at,
    DROP COLUMN IF EXISTS cancellation_requested_at,
    DROP COLUMN IF EXISTS attempt_generation,
    DROP COLUMN IF EXISTS execution_token;

DROP TRIGGER IF EXISTS event_outbox_project_event_log_trigger ON event_outbox;
DROP FUNCTION IF EXISTS append_project_event_log();
DROP TABLE IF EXISTS project_event_log;
DROP TABLE IF EXISTS project_event_streams;
