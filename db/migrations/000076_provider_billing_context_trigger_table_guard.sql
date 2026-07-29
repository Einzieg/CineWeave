-- +goose Up
SET search_path TO public;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_provider_billing_context_identity_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_TABLE_NAME = 'workflow_runs' THEN
        IF OLD.billing_context_id IS NULL
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
        IF NEW.billing_context_revision IS DISTINCT FROM OLD.billing_context_revision
           OR NEW.billing_context_snapshot_hash IS DISTINCT FROM OLD.billing_context_snapshot_hash THEN
            RAISE EXCEPTION 'billing context revision and snapshot hash are immutable'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    ELSIF TG_TABLE_NAME = 'provider_requests' THEN
        IF NEW.billing_context_id IS DISTINCT FROM OLD.billing_context_id THEN
            RAISE EXCEPTION 'provider billing_context_id is immutable'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.billing_context_revision IS DISTINCT FROM OLD.billing_context_revision
           OR NEW.billing_context_snapshot_hash IS DISTINCT FROM OLD.billing_context_snapshot_hash THEN
            RAISE EXCEPTION 'billing context revision and snapshot hash are immutable'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    ELSIF TG_TABLE_NAME IN ('provider_call_logs', 'provider_async_tasks') THEN
        IF NEW.billing_context_id IS DISTINCT FROM OLD.billing_context_id THEN
            RAISE EXCEPTION 'provider billing_context_id is immutable'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'provider billing context trigger attached to unsupported table %',
        TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;
-- +goose StatementEnd

-- +goose Down
SET search_path TO public;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_provider_billing_context_identity_change()
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
