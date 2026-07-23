-- +goose Up

SET search_path TO public;

CREATE TABLE commerce_setup_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL,
    setup_session_id UUID NOT NULL UNIQUE REFERENCES commerce_setup_sessions(id) ON DELETE CASCADE,
    temporal_workflow_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'queued',
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_hash TEXT NOT NULL,
    output JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    error_message TEXT,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    revision BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT commerce_setup_runs_project_fk
        FOREIGN KEY (project_id, organization_id)
        REFERENCES projects(id, organization_id) ON DELETE CASCADE,
    CONSTRAINT commerce_setup_runs_status_check CHECK (
        status IN ('queued', 'running', 'waiting_user_confirmation', 'needs_user_review', 'succeeded', 'failed', 'cancelled')
    ),
    CONSTRAINT commerce_setup_runs_input_check CHECK (jsonb_typeof(input) = 'object'),
    CONSTRAINT commerce_setup_runs_output_check CHECK (jsonb_typeof(output) = 'object'),
    CONSTRAINT commerce_setup_runs_input_hash_check CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_setup_runs_revision_check CHECK (revision > 0),
    CONSTRAINT commerce_setup_runs_terminal_check CHECK (
        (status IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
        OR (status NOT IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
    )
);

CREATE INDEX commerce_setup_runs_project_status_idx
    ON commerce_setup_runs(project_id, status, updated_at DESC);

ALTER TABLE commerce_setup_sessions
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_setup_workflow_run_id_fkey,
    ADD CONSTRAINT commerce_setup_sessions_setup_workflow_run_fk
        FOREIGN KEY (setup_workflow_run_id) REFERENCES commerce_setup_runs(id) ON DELETE SET NULL;

ALTER TABLE workflow_start_outbox
    DROP CONSTRAINT workflow_start_outbox_target_check,
    DROP CONSTRAINT workflow_start_outbox_production_generation_fk,
    ALTER COLUMN production_generation_id DROP NOT NULL,
    ADD COLUMN commerce_setup_run_id UUID UNIQUE REFERENCES commerce_setup_runs(id) ON DELETE CASCADE,
    ADD CONSTRAINT workflow_start_outbox_target_check CHECK (
        num_nonnulls(workflow_run_id, agent_task_id, commerce_setup_run_id) = 1
    ),
    ADD CONSTRAINT workflow_start_outbox_production_identity_check CHECK (
        (
            commerce_setup_run_id IS NULL
            AND production_generation_id IS NOT NULL
        )
        OR (
            commerce_setup_run_id IS NOT NULL
            AND workflow_run_id IS NULL
            AND agent_task_id IS NULL
            AND production_generation_id IS NULL
            AND workflow_type = 'commerce_project_setup'
        )
    ),
    ADD CONSTRAINT workflow_start_outbox_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

-- +goose Down

SET search_path TO public;

UPDATE commerce_setup_sessions
SET setup_workflow_run_id = NULL
WHERE setup_workflow_run_id IS NOT NULL;

DELETE FROM workflow_start_outbox
WHERE commerce_setup_run_id IS NOT NULL;

ALTER TABLE workflow_start_outbox
    DROP CONSTRAINT workflow_start_outbox_production_generation_fk,
    DROP CONSTRAINT workflow_start_outbox_production_identity_check,
    DROP CONSTRAINT workflow_start_outbox_target_check,
    DROP COLUMN commerce_setup_run_id,
    ALTER COLUMN production_generation_id SET NOT NULL,
    ADD CONSTRAINT workflow_start_outbox_target_check CHECK (
        num_nonnulls(workflow_run_id, agent_task_id) = 1
    ),
    ADD CONSTRAINT workflow_start_outbox_production_generation_fk
        FOREIGN KEY (production_generation_id, project_id)
        REFERENCES project_video_production_generations(id, project_id) ON DELETE RESTRICT;

ALTER TABLE commerce_setup_sessions
    DROP CONSTRAINT IF EXISTS commerce_setup_sessions_setup_workflow_run_fk,
    ADD CONSTRAINT commerce_setup_sessions_setup_workflow_run_id_fkey
        FOREIGN KEY (setup_workflow_run_id) REFERENCES workflow_runs(id) ON DELETE SET NULL;

DROP INDEX IF EXISTS commerce_setup_runs_project_status_idx;
DROP TABLE IF EXISTS commerce_setup_runs;
