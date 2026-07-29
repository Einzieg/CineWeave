-- +goose Up
SET search_path TO public;

ALTER TABLE workflow_runs
    ADD COLUMN billing_context_id UUID,
    ADD COLUMN billing_context_revision BIGINT,
    ADD COLUMN billing_context_snapshot_hash TEXT;

ALTER TABLE workflow_runs
    ADD CONSTRAINT workflow_runs_billing_context_presence_check CHECK (
        (billing_context_id IS NULL
         AND billing_context_revision IS NULL
         AND billing_context_snapshot_hash IS NULL)
        OR
        (billing_context_id IS NOT NULL
         AND billing_context_revision IS NOT NULL
         AND billing_context_snapshot_hash IS NOT NULL)
    ),
    ADD CONSTRAINT workflow_runs_billing_context_revision_check CHECK (
        billing_context_revision IS NULL OR billing_context_revision > 0
    ),
    ADD CONSTRAINT workflow_runs_billing_context_snapshot_hash_check CHECK (
        billing_context_snapshot_hash IS NULL
        OR billing_context_snapshot_hash ~ '^[0-9a-f]{64}$'
    );

CREATE INDEX workflow_runs_billing_context_idx
    ON workflow_runs (billing_context_id, created_at DESC)
    WHERE billing_context_id IS NOT NULL;

ALTER TABLE provider_requests
    ADD COLUMN requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN billing_context_id UUID,
    ADD COLUMN billing_context_revision BIGINT,
    ADD COLUMN billing_context_snapshot_hash TEXT;

ALTER TABLE provider_requests
    ADD CONSTRAINT provider_requests_billing_context_presence_check CHECK (
        (billing_context_id IS NULL
         AND billing_context_revision IS NULL
         AND billing_context_snapshot_hash IS NULL)
        OR
        (billing_context_id IS NOT NULL
         AND billing_context_revision IS NOT NULL
         AND billing_context_snapshot_hash IS NOT NULL)
    ),
    ADD CONSTRAINT provider_requests_billing_context_revision_check CHECK (
        billing_context_revision IS NULL OR billing_context_revision > 0
    ),
    ADD CONSTRAINT provider_requests_billing_context_snapshot_hash_check CHECK (
        billing_context_snapshot_hash IS NULL
        OR billing_context_snapshot_hash ~ '^[0-9a-f]{64}$'
    );

CREATE INDEX provider_requests_billing_context_idx
    ON provider_requests (billing_context_id, created_at DESC)
    WHERE billing_context_id IS NOT NULL;

ALTER TABLE provider_call_logs
    ADD COLUMN billing_context_id UUID,
    ADD COLUMN provider_external_log_id TEXT;

ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_external_log_id_check CHECK (
        provider_external_log_id IS NULL
        OR btrim(provider_external_log_id) <> ''
    );

CREATE INDEX provider_call_logs_billing_context_idx
    ON provider_call_logs (billing_context_id, created_at DESC)
    WHERE billing_context_id IS NOT NULL;

ALTER TABLE provider_async_tasks
    ADD COLUMN billing_context_id UUID;

CREATE INDEX provider_async_tasks_billing_context_idx
    ON provider_async_tasks (billing_context_id, created_at DESC)
    WHERE billing_context_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION reject_provider_billing_context_identity_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_TABLE_NAME = 'workflow_runs'
       AND OLD.billing_context_id IS NULL
       AND OLD.billing_context_revision IS NULL
       AND OLD.billing_context_snapshot_hash IS NULL
       AND NEW.billing_context_id IS NOT NULL
       AND NEW.billing_context_revision IS NOT NULL
       AND NEW.billing_context_snapshot_hash IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.billing_context_id IS DISTINCT FROM OLD.billing_context_id THEN
        RAISE EXCEPTION 'provider billing_context_id is immutable'
            USING ERRCODE = '23514';
    END IF;
    IF TG_TABLE_NAME IN ('workflow_runs', 'provider_requests') THEN
        IF NEW.billing_context_revision IS DISTINCT FROM OLD.billing_context_revision
           OR NEW.billing_context_snapshot_hash IS DISTINCT FROM OLD.billing_context_snapshot_hash THEN
            RAISE EXCEPTION 'billing context revision and snapshot hash are immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workflow_runs_billing_context_immutable
BEFORE UPDATE ON workflow_runs
FOR EACH ROW EXECUTE FUNCTION reject_provider_billing_context_identity_change();

CREATE TRIGGER provider_requests_billing_context_immutable
BEFORE UPDATE ON provider_requests
FOR EACH ROW EXECUTE FUNCTION reject_provider_billing_context_identity_change();

CREATE TRIGGER provider_call_logs_billing_context_immutable
BEFORE UPDATE ON provider_call_logs
FOR EACH ROW EXECUTE FUNCTION reject_provider_billing_context_identity_change();

CREATE TRIGGER provider_async_tasks_billing_context_immutable
BEFORE UPDATE ON provider_async_tasks
FOR EACH ROW EXECUTE FUNCTION reject_provider_billing_context_identity_change();

COMMENT ON COLUMN provider_requests.billing_context_id IS
    'Opaque Edition billing identity; Core does not interpret or authorize it.';
COMMENT ON COLUMN workflow_runs.billing_context_id IS
    'Opaque Edition billing identity resolved before the first paid Provider create.';
COMMENT ON COLUMN provider_requests.billing_context_snapshot_hash IS
    'Immutable audit observation only; never a spend authorization lease.';
COMMENT ON COLUMN provider_call_logs.provider_external_log_id IS
    'Authority-scoped upstream log identifier; uniqueness is enforced by Commercial attribution.';

-- +goose Down
SET search_path TO public;

DROP TRIGGER workflow_runs_billing_context_immutable
    ON workflow_runs;
DROP TRIGGER provider_async_tasks_billing_context_immutable
    ON provider_async_tasks;
DROP TRIGGER provider_call_logs_billing_context_immutable
    ON provider_call_logs;
DROP TRIGGER provider_requests_billing_context_immutable
    ON provider_requests;
DROP FUNCTION reject_provider_billing_context_identity_change();

DROP INDEX provider_async_tasks_billing_context_idx;
ALTER TABLE provider_async_tasks
    DROP COLUMN billing_context_id;

DROP INDEX provider_call_logs_billing_context_idx;
ALTER TABLE provider_call_logs
    DROP CONSTRAINT provider_call_logs_external_log_id_check,
    DROP COLUMN provider_external_log_id,
    DROP COLUMN billing_context_id;

DROP INDEX provider_requests_billing_context_idx;
ALTER TABLE provider_requests
    DROP CONSTRAINT provider_requests_billing_context_snapshot_hash_check,
    DROP CONSTRAINT provider_requests_billing_context_revision_check,
    DROP CONSTRAINT provider_requests_billing_context_presence_check,
    DROP COLUMN billing_context_snapshot_hash,
    DROP COLUMN billing_context_revision,
    DROP COLUMN billing_context_id,
    DROP COLUMN requested_by_user_id;

DROP INDEX workflow_runs_billing_context_idx;
ALTER TABLE workflow_runs
    DROP CONSTRAINT workflow_runs_billing_context_snapshot_hash_check,
    DROP CONSTRAINT workflow_runs_billing_context_revision_check,
    DROP CONSTRAINT workflow_runs_billing_context_presence_check,
    DROP COLUMN billing_context_snapshot_hash,
    DROP COLUMN billing_context_revision,
    DROP COLUMN billing_context_id;
