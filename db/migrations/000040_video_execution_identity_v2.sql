-- +goose Up

SET search_path TO public;

ALTER TABLE episode_video_production_items
    ADD COLUMN execution_identity_version SMALLINT NOT NULL DEFAULT 1,
    ADD COLUMN predecessor_video_render_plan_id UUID REFERENCES video_render_plans(id) ON DELETE SET NULL,
    ADD COLUMN execution_plan_bound_at TIMESTAMPTZ,
    ADD CONSTRAINT episode_video_production_items_identity_version_check CHECK (
        execution_identity_version IN (1, 2)
    ),
    ADD CONSTRAINT episode_video_production_items_v2_plan_binding_check CHECK (
        execution_identity_version <> 2
        OR video_render_plan_id IS NULL
        OR execution_plan_bound_at IS NOT NULL
    );

CREATE UNIQUE INDEX episode_video_production_items_v2_render_plan_once
    ON episode_video_production_items(video_render_plan_id)
    WHERE execution_identity_version = 2 AND video_render_plan_id IS NOT NULL;

CREATE INDEX episode_video_production_items_v2_attempt_idx
    ON episode_video_production_items(storyboard_shot_id, attempt DESC, created_at DESC)
    WHERE execution_identity_version = 2;

ALTER TABLE video_render_plans
    ADD COLUMN operation_item_id UUID REFERENCES episode_video_production_items(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_attempt INTEGER,
    ADD CONSTRAINT video_render_plans_operation_item_pair_check CHECK (
        (operation_item_id IS NULL) = (operation_item_attempt IS NULL)
    ),
    ADD CONSTRAINT video_render_plans_operation_item_attempt_check CHECK (
        operation_item_attempt IS NULL OR operation_item_attempt > 0
    );

CREATE UNIQUE INDEX video_render_plans_operation_item_once
    ON video_render_plans(operation_item_id)
    WHERE operation_item_id IS NOT NULL;

ALTER TABLE provider_async_tasks
    ADD COLUMN operation_id UUID REFERENCES episode_video_production_checkpoints(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_id UUID REFERENCES episode_video_production_items(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_attempt INTEGER,
    ADD COLUMN request_hash TEXT,
    ADD CONSTRAINT provider_async_tasks_operation_item_pair_check CHECK (
        (operation_item_id IS NULL) = (operation_item_attempt IS NULL)
    ),
    ADD CONSTRAINT provider_async_tasks_operation_item_attempt_check CHECK (
        operation_item_attempt IS NULL OR operation_item_attempt > 0
    ),
    ADD CONSTRAINT provider_async_tasks_request_hash_check CHECK (
        request_hash IS NULL OR btrim(request_hash) <> ''
    );

UPDATE provider_async_tasks task
SET request_hash = request.request_hash
FROM provider_requests request
WHERE request.id = task.provider_request_id
  AND task.request_hash IS NULL;

UPDATE provider_async_tasks task
SET operation_id = batch.checkpoint_id
FROM episode_video_production_items item
JOIN episode_video_production_batches batch ON batch.id = item.batch_id
WHERE item.id = task.operation_item_id
  AND task.operation_id IS NULL;

CREATE INDEX provider_async_tasks_operation_item_idx
    ON provider_async_tasks(operation_id, operation_item_id, status, created_at DESC)
    WHERE operation_item_id IS NOT NULL;

CREATE UNIQUE INDEX provider_async_tasks_segment_request_once
    ON provider_async_tasks(video_render_segment_id, request_hash)
    WHERE video_render_segment_id IS NOT NULL AND request_hash IS NOT NULL;

ALTER TABLE provider_requests
    -- Provider requests are the audit envelope for attempted identities. Keep the
    -- submitted UUIDs even when exact plan validation rejects the request.
    ADD COLUMN operation_id UUID,
    ADD COLUMN operation_item_id UUID,
    ADD COLUMN operation_item_attempt INTEGER,
    ADD COLUMN video_render_plan_id UUID,
    ADD COLUMN video_render_segment_id UUID,
    ADD CONSTRAINT provider_requests_operation_item_pair_check CHECK (
        (operation_item_id IS NULL) = (operation_item_attempt IS NULL)
    ),
    ADD CONSTRAINT provider_requests_operation_item_attempt_check CHECK (
        operation_item_attempt IS NULL OR operation_item_attempt > 0
    );

ALTER TABLE provider_call_logs
    ADD COLUMN operation_id UUID REFERENCES episode_video_production_checkpoints(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_id UUID REFERENCES episode_video_production_items(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_attempt INTEGER,
    ADD COLUMN video_render_plan_id UUID REFERENCES video_render_plans(id) ON DELETE SET NULL,
    ADD COLUMN video_render_segment_id UUID REFERENCES video_render_segments(id) ON DELETE SET NULL,
    ADD CONSTRAINT provider_call_logs_operation_item_pair_check CHECK (
        (operation_item_id IS NULL) = (operation_item_attempt IS NULL)
    ),
    ADD CONSTRAINT provider_call_logs_operation_item_attempt_check CHECK (
        operation_item_attempt IS NULL OR operation_item_attempt > 0
    );

ALTER TABLE cost_records
    ADD COLUMN operation_id UUID REFERENCES episode_video_production_checkpoints(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_id UUID REFERENCES episode_video_production_items(id) ON DELETE SET NULL,
    ADD COLUMN operation_item_attempt INTEGER,
    ADD COLUMN video_render_plan_id UUID REFERENCES video_render_plans(id) ON DELETE SET NULL,
    ADD COLUMN video_render_segment_id UUID REFERENCES video_render_segments(id) ON DELETE SET NULL,
    ADD CONSTRAINT cost_records_operation_item_pair_check CHECK (
        (operation_item_id IS NULL) = (operation_item_attempt IS NULL)
    ),
    ADD CONSTRAINT cost_records_operation_item_attempt_check CHECK (
        operation_item_attempt IS NULL OR operation_item_attempt > 0
    );

UPDATE provider_requests request
SET operation_id = checkpoint.id,
    operation_item_id = task.operation_item_id,
    operation_item_attempt = task.operation_item_attempt,
    video_render_plan_id = task.video_render_plan_id,
    video_render_segment_id = task.video_render_segment_id
FROM provider_async_tasks task
JOIN episode_video_production_items item ON item.id = task.operation_item_id
JOIN episode_video_production_batches batch ON batch.id = item.batch_id
JOIN episode_video_production_checkpoints checkpoint ON checkpoint.id = batch.checkpoint_id
WHERE request.id = task.provider_request_id
  AND task.operation_item_id IS NOT NULL;

UPDATE provider_call_logs call
SET operation_id = request.operation_id,
    operation_item_id = request.operation_item_id,
    operation_item_attempt = request.operation_item_attempt,
    video_render_plan_id = request.video_render_plan_id,
    video_render_segment_id = request.video_render_segment_id
FROM provider_requests request
WHERE request.id = call.provider_request_id
  AND request.operation_item_id IS NOT NULL;

UPDATE cost_records cost
SET operation_id = call.operation_id,
    operation_item_id = call.operation_item_id,
    operation_item_attempt = call.operation_item_attempt,
    video_render_plan_id = call.video_render_plan_id,
    video_render_segment_id = call.video_render_segment_id
FROM provider_call_logs call
WHERE call.id = cost.provider_call_id
  AND call.operation_item_id IS NOT NULL;

CREATE INDEX provider_requests_operation_item_idx
    ON provider_requests(operation_item_id, operation_item_attempt, created_at DESC)
    WHERE operation_item_id IS NOT NULL;

CREATE INDEX provider_call_logs_operation_item_idx
    ON provider_call_logs(operation_item_id, operation_item_attempt, created_at DESC)
    WHERE operation_item_id IS NOT NULL;

CREATE INDEX cost_records_operation_item_idx
    ON cost_records(operation_item_id, operation_item_attempt, created_at DESC)
    WHERE operation_item_id IS NOT NULL;

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM episode_video_production_items
        WHERE execution_identity_version = 2
    ) OR EXISTS (
        SELECT 1
        FROM video_render_plans
        WHERE operation_item_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM provider_async_tasks
        WHERE operation_item_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM provider_requests
        WHERE operation_item_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM provider_call_logs
        WHERE operation_item_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM cost_records
        WHERE operation_item_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back video execution identity v2 after v2 execution data exists';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS cost_records_operation_item_idx;
ALTER TABLE cost_records
    DROP CONSTRAINT IF EXISTS cost_records_operation_item_attempt_check,
    DROP CONSTRAINT IF EXISTS cost_records_operation_item_pair_check,
    DROP COLUMN IF EXISTS video_render_segment_id,
    DROP COLUMN IF EXISTS video_render_plan_id,
    DROP COLUMN IF EXISTS operation_item_attempt,
    DROP COLUMN IF EXISTS operation_item_id,
    DROP COLUMN IF EXISTS operation_id;

DROP INDEX IF EXISTS provider_call_logs_operation_item_idx;
ALTER TABLE provider_call_logs
    DROP CONSTRAINT IF EXISTS provider_call_logs_operation_item_attempt_check,
    DROP CONSTRAINT IF EXISTS provider_call_logs_operation_item_pair_check,
    DROP COLUMN IF EXISTS video_render_segment_id,
    DROP COLUMN IF EXISTS video_render_plan_id,
    DROP COLUMN IF EXISTS operation_item_attempt,
    DROP COLUMN IF EXISTS operation_item_id,
    DROP COLUMN IF EXISTS operation_id;

DROP INDEX IF EXISTS provider_requests_operation_item_idx;
ALTER TABLE provider_requests
    DROP CONSTRAINT IF EXISTS provider_requests_operation_item_attempt_check,
    DROP CONSTRAINT IF EXISTS provider_requests_operation_item_pair_check,
    DROP COLUMN IF EXISTS video_render_segment_id,
    DROP COLUMN IF EXISTS video_render_plan_id,
    DROP COLUMN IF EXISTS operation_item_attempt,
    DROP COLUMN IF EXISTS operation_item_id,
    DROP COLUMN IF EXISTS operation_id;

DROP INDEX IF EXISTS provider_async_tasks_segment_request_once;
DROP INDEX IF EXISTS provider_async_tasks_operation_item_idx;
ALTER TABLE provider_async_tasks
    DROP CONSTRAINT IF EXISTS provider_async_tasks_request_hash_check,
    DROP CONSTRAINT IF EXISTS provider_async_tasks_operation_item_attempt_check,
    DROP CONSTRAINT IF EXISTS provider_async_tasks_operation_item_pair_check,
    DROP COLUMN IF EXISTS request_hash,
    DROP COLUMN IF EXISTS operation_item_attempt,
    DROP COLUMN IF EXISTS operation_item_id,
    DROP COLUMN IF EXISTS operation_id;

DROP INDEX IF EXISTS video_render_plans_operation_item_once;
ALTER TABLE video_render_plans
    DROP CONSTRAINT IF EXISTS video_render_plans_operation_item_attempt_check,
    DROP CONSTRAINT IF EXISTS video_render_plans_operation_item_pair_check,
    DROP COLUMN IF EXISTS operation_item_attempt,
    DROP COLUMN IF EXISTS operation_item_id;

DROP INDEX IF EXISTS episode_video_production_items_v2_attempt_idx;
DROP INDEX IF EXISTS episode_video_production_items_v2_render_plan_once;
ALTER TABLE episode_video_production_items
    DROP CONSTRAINT IF EXISTS episode_video_production_items_v2_plan_binding_check,
    DROP CONSTRAINT IF EXISTS episode_video_production_items_identity_version_check,
    DROP COLUMN IF EXISTS execution_plan_bound_at,
    DROP COLUMN IF EXISTS predecessor_video_render_plan_id,
    DROP COLUMN IF EXISTS execution_identity_version;
