-- +goose Up

SET search_path TO public;

ALTER TABLE event_outbox
    ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN aggregate_revision BIGINT,
    ADD CONSTRAINT event_outbox_schema_version_check CHECK (schema_version > 0),
    ADD CONSTRAINT event_outbox_aggregate_revision_check CHECK (aggregate_revision IS NULL OR aggregate_revision >= 0);

ALTER TABLE project_event_log
    ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN aggregate_revision BIGINT,
    ADD CONSTRAINT project_event_log_schema_version_check CHECK (schema_version > 0),
    ADD CONSTRAINT project_event_log_aggregate_revision_check CHECK (aggregate_revision IS NULL OR aggregate_revision >= 0);

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
        schema_version, aggregate_type, aggregate_id, aggregate_revision,
        payload, created_at, expires_at
    )
    VALUES (
        NEW.project_id, assigned_position, NEW.id, NEW.organization_id, NEW.event_type,
        NEW.schema_version, NEW.aggregate_type, NEW.aggregate_id, NEW.aggregate_revision,
        NEW.payload, NEW.created_at, NEW.created_at + interval '7 days'
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

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

ALTER TABLE project_event_log
    DROP CONSTRAINT IF EXISTS project_event_log_aggregate_revision_check,
    DROP CONSTRAINT IF EXISTS project_event_log_schema_version_check,
    DROP COLUMN IF EXISTS aggregate_revision,
    DROP COLUMN IF EXISTS schema_version;

ALTER TABLE event_outbox
    DROP CONSTRAINT IF EXISTS event_outbox_aggregate_revision_check,
    DROP CONSTRAINT IF EXISTS event_outbox_schema_version_check,
    DROP COLUMN IF EXISTS aggregate_revision,
    DROP COLUMN IF EXISTS schema_version;
