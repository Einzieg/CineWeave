-- +goose Up

SET search_path TO public;

ALTER TABLE cost_records
    DROP CONSTRAINT cost_records_provider_model_id_fkey;
ALTER TABLE provider_async_tasks
    DROP CONSTRAINT provider_async_tasks_provider_model_id_fkey;
ALTER TABLE provider_call_logs
    DROP CONSTRAINT provider_call_logs_provider_model_id_fkey;
ALTER TABLE provider_leases
    DROP CONSTRAINT provider_leases_provider_model_id_fkey;
ALTER TABLE video_render_plans
    DROP CONSTRAINT video_render_plans_provider_model_id_fkey;
ALTER TABLE video_render_segments
    DROP CONSTRAINT video_render_segments_provider_model_id_fkey;

ALTER TABLE video_render_plans
    ALTER COLUMN provider_model_id DROP NOT NULL;

ALTER TABLE cost_records
    ADD CONSTRAINT cost_records_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;
ALTER TABLE provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;
ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;
ALTER TABLE provider_leases
    ADD CONSTRAINT provider_leases_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;
ALTER TABLE video_render_plans
    ADD CONSTRAINT video_render_plans_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;
ALTER TABLE video_render_segments
    ADD CONSTRAINT video_render_segments_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;

-- +goose Down

SET search_path TO public;

ALTER TABLE cost_records
    DROP CONSTRAINT cost_records_provider_model_id_fkey;
ALTER TABLE provider_async_tasks
    DROP CONSTRAINT provider_async_tasks_provider_model_id_fkey;
ALTER TABLE provider_call_logs
    DROP CONSTRAINT provider_call_logs_provider_model_id_fkey;
ALTER TABLE provider_leases
    DROP CONSTRAINT provider_leases_provider_model_id_fkey;
ALTER TABLE video_render_plans
    DROP CONSTRAINT video_render_plans_provider_model_id_fkey;
ALTER TABLE video_render_segments
    DROP CONSTRAINT video_render_segments_provider_model_id_fkey;

ALTER TABLE video_render_plans
    ALTER COLUMN provider_model_id SET NOT NULL;

ALTER TABLE cost_records
    ADD CONSTRAINT cost_records_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id);
ALTER TABLE provider_async_tasks
    ADD CONSTRAINT provider_async_tasks_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id);
ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id);
ALTER TABLE provider_leases
    ADD CONSTRAINT provider_leases_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id);
ALTER TABLE video_render_plans
    ADD CONSTRAINT video_render_plans_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id);
ALTER TABLE video_render_segments
    ADD CONSTRAINT video_render_segments_provider_model_id_fkey
    FOREIGN KEY (provider_model_id) REFERENCES provider_models(id);
