-- +goose Up

SET search_path TO public;

ALTER TABLE workflow_runs
    ADD COLUMN settled_at TIMESTAMPTZ;

UPDATE workflow_runs
SET settled_at = COALESCE(terminalized_at, completed_at, cancelled_at, updated_at)
WHERE status IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled', 'skipped');

ALTER TABLE provider_async_tasks
    ADD COLUMN node_execution_token UUID,
    ADD COLUMN node_attempt_generation INTEGER;

UPDATE provider_async_tasks task
SET node_execution_token = node.execution_token,
    node_attempt_generation = node.attempt_generation
FROM workflow_node_runs node
WHERE task.node_run_id = node.id;

ALTER TABLE provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_node_execution_identity_check CHECK (
        node_run_id IS NULL
        OR
        (node_run_id IS NOT NULL AND node_execution_token IS NOT NULL AND node_attempt_generation > 0)
    );

CREATE INDEX provider_async_tasks_node_execution_idx
    ON provider_async_tasks(node_run_id, node_execution_token, node_attempt_generation)
    WHERE node_run_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS provider_async_tasks_node_execution_idx;
ALTER TABLE provider_async_tasks
    DROP CONSTRAINT IF EXISTS provider_async_tasks_node_execution_identity_check,
    DROP COLUMN IF EXISTS node_attempt_generation,
    DROP COLUMN IF EXISTS node_execution_token;

ALTER TABLE workflow_runs DROP COLUMN IF EXISTS settled_at;
