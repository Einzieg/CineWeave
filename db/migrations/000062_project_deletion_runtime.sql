-- +goose Up

SET search_path TO public;

ALTER TABLE projects
    ADD COLUMN lifecycle_status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN deletion_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN deletion_requested_at TIMESTAMPTZ,
    ADD CONSTRAINT projects_lifecycle_status_check CHECK (
        lifecycle_status IN ('active', 'deleting')
    ),
    ADD CONSTRAINT projects_deletion_revision_check CHECK (deletion_revision >= 0),
    ADD CONSTRAINT projects_deletion_state_check CHECK (
        (lifecycle_status = 'active' AND deletion_requested_at IS NULL)
        OR
        (lifecycle_status = 'deleting' AND deletion_revision > 0 AND deletion_requested_at IS NOT NULL)
    );

CREATE INDEX projects_active_workspace_created_idx
    ON projects(workspace_id, created_at DESC)
    WHERE lifecycle_status = 'active';

CREATE TABLE project_deletion_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id UUID NOT NULL,
    project_name TEXT NOT NULL,
    project_revision BIGINT NOT NULL,
    deletion_revision BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'requested',
    impact_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    impact_hash TEXT NOT NULL,
    manifest_cursor BIGINT NOT NULL DEFAULT 0,
    storage_object_count INTEGER NOT NULL DEFAULT 0,
    storage_deleted_count INTEGER NOT NULL DEFAULT 0,
    storage_failed_count INTEGER NOT NULL DEFAULT 0,
    storage_skipped_shared_count INTEGER NOT NULL DEFAULT 0,
    temporal_workflow_id TEXT NOT NULL UNIQUE,
    idempotency_key TEXT NOT NULL,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    drain_deadline_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT project_deletion_requests_project_name_check CHECK (btrim(project_name) <> ''),
    CONSTRAINT project_deletion_requests_project_revision_check CHECK (project_revision > 0),
    CONSTRAINT project_deletion_requests_deletion_revision_check CHECK (deletion_revision > 0),
    CONSTRAINT project_deletion_requests_status_check CHECK (
        status IN (
            'requested',
            'cancelling_tasks',
            'waiting_for_terminal',
            'deleting_storage',
            'deleting_business_data',
            'completed',
            'failed_retryable',
            'failed_terminal'
        )
    ),
    CONSTRAINT project_deletion_requests_impact_snapshot_check CHECK (
        jsonb_typeof(impact_snapshot) = 'object'
    ),
    CONSTRAINT project_deletion_requests_impact_hash_check CHECK (
        impact_hash ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT project_deletion_requests_manifest_cursor_check CHECK (manifest_cursor >= 0),
    CONSTRAINT project_deletion_requests_storage_counts_check CHECK (
        storage_object_count >= 0
        AND storage_deleted_count >= 0
        AND storage_failed_count >= 0
        AND storage_skipped_shared_count >= 0
        AND storage_deleted_count + storage_failed_count + storage_skipped_shared_count <= storage_object_count
    ),
    CONSTRAINT project_deletion_requests_idempotency_key_check CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT project_deletion_requests_retry_count_check CHECK (retry_count >= 0),
    CONSTRAINT project_deletion_requests_terminal_check CHECK (
        (
            status IN ('completed', 'failed_terminal')
            AND completed_at IS NOT NULL
            AND expires_at IS NOT NULL
        )
        OR
        (
            status NOT IN ('completed', 'failed_terminal')
            AND completed_at IS NULL
        )
    ),
    UNIQUE(organization_id, project_id, idempotency_key),
    UNIQUE(project_id, deletion_revision)
);

CREATE UNIQUE INDEX project_deletion_requests_open_project_idx
    ON project_deletion_requests(project_id)
    WHERE status NOT IN ('completed', 'failed_terminal');

CREATE INDEX project_deletion_requests_requested_by_idx
    ON project_deletion_requests(requested_by, requested_at DESC);

CREATE INDEX project_deletion_requests_expiry_idx
    ON project_deletion_requests(expires_at)
    WHERE expires_at IS NOT NULL;

CREATE TABLE project_deletion_objects (
    id BIGSERIAL PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES project_deletion_requests(id) ON DELETE CASCADE,
    project_id UUID NOT NULL,
    source_kind TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    byte_size BIGINT,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    claim_token UUID,
    claim_expires_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    deleted_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_deletion_objects_source_kind_check CHECK (
        source_kind IN ('artifact', 'media_file', 'media_variant', 'business_reference')
    ),
    CONSTRAINT project_deletion_objects_storage_key_check CHECK (btrim(storage_key) <> ''),
    CONSTRAINT project_deletion_objects_byte_size_check CHECK (byte_size IS NULL OR byte_size >= 0),
    CONSTRAINT project_deletion_objects_status_check CHECK (
        status IN ('pending', 'deleting', 'deleted', 'failed', 'skipped_shared')
    ),
    CONSTRAINT project_deletion_objects_attempt_count_check CHECK (attempt_count >= 0),
    CONSTRAINT project_deletion_objects_claim_check CHECK (
        (
            status = 'deleting'
            AND claim_token IS NOT NULL
            AND claim_expires_at IS NOT NULL
        )
        OR
        (
            status <> 'deleting'
            AND claim_token IS NULL
            AND claim_expires_at IS NULL
        )
    ),
    CONSTRAINT project_deletion_objects_terminal_check CHECK (
        (status IN ('deleted', 'skipped_shared') AND deleted_at IS NOT NULL)
        OR
        (status NOT IN ('deleted', 'skipped_shared') AND deleted_at IS NULL)
    ),
    UNIQUE(request_id, storage_key)
);

CREATE INDEX project_deletion_objects_work_idx
    ON project_deletion_objects(request_id, status, id);

CREATE INDEX project_deletion_objects_expired_claim_idx
    ON project_deletion_objects(request_id, claim_expires_at, id)
    WHERE status = 'deleting';

-- +goose StatementBegin
CREATE FUNCTION protect_project_deletion_request_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.workspace_id IS DISTINCT FROM OLD.workspace_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.project_name IS DISTINCT FROM OLD.project_name
       OR NEW.project_revision IS DISTINCT FROM OLD.project_revision
       OR NEW.deletion_revision IS DISTINCT FROM OLD.deletion_revision
       OR NEW.impact_snapshot IS DISTINCT FROM OLD.impact_snapshot
       OR NEW.impact_hash IS DISTINCT FROM OLD.impact_hash
       OR NEW.temporal_workflow_id IS DISTINCT FROM OLD.temporal_workflow_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.requested_by IS DISTINCT FROM OLD.requested_by
       OR NEW.requested_at IS DISTINCT FROM OLD.requested_at THEN
        RAISE EXCEPTION 'project deletion request identity is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_deletion_request_identity_immutable
BEFORE UPDATE ON project_deletion_requests
FOR EACH ROW EXECUTE FUNCTION protect_project_deletion_request_identity();

ALTER TABLE workflow_start_outbox
    DROP CONSTRAINT workflow_start_outbox_production_identity_check,
    DROP CONSTRAINT workflow_start_outbox_target_check,
    ADD COLUMN project_deletion_request_id UUID UNIQUE
        REFERENCES project_deletion_requests(id) ON DELETE CASCADE,
    ADD CONSTRAINT workflow_start_outbox_target_check CHECK (
        num_nonnulls(
            workflow_run_id,
            agent_task_id,
            commerce_setup_run_id,
            project_deletion_request_id
        ) = 1
    ),
    ADD CONSTRAINT workflow_start_outbox_production_identity_check CHECK (
        (
            commerce_setup_run_id IS NULL
            AND project_deletion_request_id IS NULL
            AND production_generation_id IS NOT NULL
        )
        OR
        (
            commerce_setup_run_id IS NOT NULL
            AND workflow_run_id IS NULL
            AND agent_task_id IS NULL
            AND project_deletion_request_id IS NULL
            AND production_generation_id IS NULL
            AND workflow_type = 'commerce_project_setup'
        )
        OR
        (
            project_deletion_request_id IS NOT NULL
            AND workflow_run_id IS NULL
            AND agent_task_id IS NULL
            AND commerce_setup_run_id IS NULL
            AND production_generation_id IS NULL
            AND workflow_type = 'project_deletion'
        )
    );

-- +goose Down

SET search_path TO public;

DELETE FROM workflow_start_outbox
WHERE project_deletion_request_id IS NOT NULL;

ALTER TABLE workflow_start_outbox
    DROP CONSTRAINT workflow_start_outbox_production_identity_check,
    DROP CONSTRAINT workflow_start_outbox_target_check,
    DROP COLUMN project_deletion_request_id,
    ADD CONSTRAINT workflow_start_outbox_target_check CHECK (
        num_nonnulls(workflow_run_id, agent_task_id, commerce_setup_run_id) = 1
    ),
    ADD CONSTRAINT workflow_start_outbox_production_identity_check CHECK (
        (
            commerce_setup_run_id IS NULL
            AND production_generation_id IS NOT NULL
        )
        OR
        (
            commerce_setup_run_id IS NOT NULL
            AND workflow_run_id IS NULL
            AND agent_task_id IS NULL
            AND production_generation_id IS NULL
            AND workflow_type = 'commerce_project_setup'
        )
    );

DROP TRIGGER IF EXISTS project_deletion_request_identity_immutable
    ON project_deletion_requests;
DROP FUNCTION IF EXISTS protect_project_deletion_request_identity();

DROP TABLE IF EXISTS project_deletion_objects;
DROP TABLE IF EXISTS project_deletion_requests;

DROP INDEX IF EXISTS projects_active_workspace_created_idx;

ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_deletion_state_check,
    DROP CONSTRAINT IF EXISTS projects_deletion_revision_check,
    DROP CONSTRAINT IF EXISTS projects_lifecycle_status_check,
    DROP COLUMN IF EXISTS deletion_requested_at,
    DROP COLUMN IF EXISTS deletion_revision,
    DROP COLUMN IF EXISTS lifecycle_status;
