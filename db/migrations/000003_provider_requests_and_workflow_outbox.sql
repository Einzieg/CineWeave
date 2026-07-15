-- +goose Up

SET search_path TO public;

ALTER TABLE provider_call_logs
    DROP CONSTRAINT provider_call_logs_status_check;
ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped', 'blocked', 'unknown_outcome')
    );

ALTER TABLE workflow_start_outbox
    ALTER COLUMN workflow_run_id DROP NOT NULL,
    ADD COLUMN agent_task_id UUID REFERENCES agent_tasks(id) ON DELETE CASCADE,
    ADD COLUMN workflow_handler TEXT;

UPDATE workflow_start_outbox
SET workflow_handler = workflow_type
WHERE workflow_handler IS NULL;

ALTER TABLE workflow_start_outbox
    ALTER COLUMN workflow_handler SET NOT NULL,
    ADD CONSTRAINT workflow_start_outbox_target_check CHECK (
        num_nonnulls(workflow_run_id, agent_task_id) = 1
    ),
    ADD CONSTRAINT workflow_start_outbox_handler_check CHECK (btrim(workflow_handler) <> '');

CREATE INDEX workflow_start_outbox_agent_task_idx
    ON workflow_start_outbox (agent_task_id)
    WHERE agent_task_id IS NOT NULL;

CREATE UNIQUE INDEX cost_records_provider_call_uidx
    ON cost_records (provider_call_id)
    WHERE provider_call_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS cost_records_provider_call_uidx;
DROP INDEX IF EXISTS workflow_start_outbox_agent_task_idx;
ALTER TABLE workflow_start_outbox DROP CONSTRAINT IF EXISTS workflow_start_outbox_handler_check;
ALTER TABLE workflow_start_outbox DROP CONSTRAINT IF EXISTS workflow_start_outbox_target_check;
DELETE FROM workflow_start_outbox WHERE workflow_run_id IS NULL;
ALTER TABLE workflow_start_outbox DROP COLUMN IF EXISTS workflow_handler;
ALTER TABLE workflow_start_outbox DROP COLUMN IF EXISTS agent_task_id;
ALTER TABLE workflow_start_outbox ALTER COLUMN workflow_run_id SET NOT NULL;

UPDATE provider_call_logs
SET status = 'failed',
    error_code = COALESCE(error_code, 'UNKNOWN_OUTCOME_DOWNGRADE'),
    error_message = COALESCE(error_message, 'unknown outcome downgraded during development rollback')
WHERE status = 'unknown_outcome';
ALTER TABLE provider_call_logs DROP CONSTRAINT provider_call_logs_status_check;
ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped', 'blocked')
    );
