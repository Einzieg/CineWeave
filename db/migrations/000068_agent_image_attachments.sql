-- +goose Up

SET search_path TO public;

CREATE TABLE agent_image_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    original_file_name TEXT NOT NULL,
    requested_mime_type TEXT NOT NULL,
    byte_size BIGINT,
    width INTEGER,
    height INTEGER,
    content_hash TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    idempotency_key TEXT NOT NULL,
    artifact_id UUID REFERENCES artifacts(id) ON DELETE CASCADE,
    media_file_id UUID REFERENCES media_files(id) ON DELETE CASCADE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    abandoned_at TIMESTAMPTZ,
    CONSTRAINT agent_image_attachments_file_name_check
        CHECK (btrim(original_file_name) <> ''),
    CONSTRAINT agent_image_attachments_mime_check
        CHECK (requested_mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT agent_image_attachments_status_check
        CHECK (status IN ('pending', 'completed', 'abandoned')),
    CONSTRAINT agent_image_attachments_content_hash_check
        CHECK (content_hash IS NULL OR content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT agent_image_attachments_dimensions_check
        CHECK (
            (width IS NULL AND height IS NULL)
            OR (width > 0 AND height > 0)
        ),
    CONSTRAINT agent_image_attachments_lifecycle_check
        CHECK (
            (status = 'pending'
                AND artifact_id IS NULL
                AND media_file_id IS NULL
                AND completed_at IS NULL
                AND abandoned_at IS NULL)
            OR (status = 'completed'
                AND artifact_id IS NOT NULL
                AND media_file_id IS NOT NULL
                AND byte_size > 0
                AND width > 0
                AND height > 0
                AND content_hash IS NOT NULL
                AND completed_at IS NOT NULL
                AND abandoned_at IS NULL)
            OR (status = 'abandoned'
                AND artifact_id IS NULL
                AND media_file_id IS NULL
                AND completed_at IS NULL
                AND abandoned_at IS NOT NULL)
        ),
    CONSTRAINT agent_image_attachments_org_idempotency_unique
        UNIQUE (organization_id, idempotency_key),
    CONSTRAINT agent_image_attachments_project_storage_unique
        UNIQUE (project_id, storage_key)
);

CREATE INDEX agent_image_attachments_project_status_idx
    ON agent_image_attachments(project_id, status, created_at DESC);

CREATE INDEX agent_image_attachments_cleanup_idx
    ON agent_image_attachments(status, expires_at)
    WHERE status = 'pending';

CREATE TABLE agent_task_image_attachments (
    task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES agent_image_attachments(id) ON DELETE CASCADE,
    usage TEXT NOT NULL DEFAULT 'unspecified',
    ordinal INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, attachment_id),
    CONSTRAINT agent_task_image_attachments_usage_check
        CHECK (usage IN ('unspecified', 'product_common', 'script_custom', 'visual_reference')),
    CONSTRAINT agent_task_image_attachments_ordinal_check
        CHECK (ordinal >= 0),
    CONSTRAINT agent_task_image_attachments_task_ordinal_unique
        UNIQUE (task_id, ordinal)
);

CREATE INDEX agent_task_image_attachments_attachment_idx
    ON agent_task_image_attachments(attachment_id, task_id);

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS agent_task_image_attachments;
DROP TABLE IF EXISTS agent_image_attachments;
