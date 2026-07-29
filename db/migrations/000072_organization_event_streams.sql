-- +goose Up

SET search_path TO public;

CREATE TABLE organization_event_streams (
    organization_id UUID PRIMARY KEY
        REFERENCES organizations(id) ON DELETE CASCADE,
    next_position BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT organization_event_streams_next_position_check
        CHECK (next_position > 0)
);

CREATE TABLE organization_event_log (
    organization_id UUID NOT NULL
        REFERENCES organizations(id) ON DELETE CASCADE,
    stream_position BIGINT NOT NULL,
    event_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1,
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID,
    aggregate_revision BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '7 days'),
    PRIMARY KEY (organization_id, stream_position),
    UNIQUE (event_id),
    CONSTRAINT organization_event_log_position_check
        CHECK (stream_position > 0),
    CONSTRAINT organization_event_log_schema_version_check
        CHECK (schema_version > 0),
    CONSTRAINT organization_event_log_aggregate_revision_check
        CHECK (aggregate_revision IS NULL OR aggregate_revision >= 0),
    CONSTRAINT organization_event_log_payload_check
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX organization_event_log_retention_idx
    ON organization_event_log (
        expires_at,
        organization_id,
        stream_position
    );

WITH ranked AS (
    SELECT
        id AS event_id,
        organization_id,
        event_type,
        schema_version,
        aggregate_type,
        aggregate_id,
        aggregate_revision,
        payload,
        created_at,
        row_number() OVER (
            PARTITION BY organization_id
            ORDER BY created_at, id
        ) AS stream_position
    FROM event_outbox
    WHERE project_id IS NULL
), inserted_streams AS (
    INSERT INTO organization_event_streams (
        organization_id,
        next_position,
        created_at,
        updated_at
    )
    SELECT
        organization_id,
        count(*) + 1,
        min(created_at),
        now()
    FROM ranked
    GROUP BY organization_id
    ON CONFLICT (organization_id) DO NOTHING
)
INSERT INTO organization_event_log (
    organization_id,
    stream_position,
    event_id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    aggregate_revision,
    payload,
    created_at,
    expires_at
)
SELECT
    organization_id,
    stream_position,
    event_id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    aggregate_revision,
    payload,
    created_at,
    created_at + interval '7 days'
FROM ranked
ORDER BY organization_id, stream_position;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION append_project_event_log()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    assigned_position BIGINT;
BEGIN
    IF NEW.project_id IS NULL THEN
        INSERT INTO organization_event_streams(organization_id)
        VALUES (NEW.organization_id)
        ON CONFLICT (organization_id) DO NOTHING;

        UPDATE organization_event_streams
        SET next_position = next_position + 1,
            updated_at = now()
        WHERE organization_id = NEW.organization_id
        RETURNING next_position - 1 INTO assigned_position;

        INSERT INTO organization_event_log(
            organization_id, stream_position, event_id, event_type,
            schema_version, aggregate_type, aggregate_id, aggregate_revision,
            payload, created_at, expires_at
        )
        VALUES (
            NEW.organization_id, assigned_position, NEW.id, NEW.event_type,
            NEW.schema_version, NEW.aggregate_type, NEW.aggregate_id,
            NEW.aggregate_revision, NEW.payload, NEW.created_at,
            NEW.created_at + interval '7 days'
        );
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
        NEW.project_id, assigned_position, NEW.id, NEW.organization_id,
        NEW.event_type, NEW.schema_version, NEW.aggregate_type,
        NEW.aggregate_id, NEW.aggregate_revision, NEW.payload,
        NEW.created_at, NEW.created_at + interval '7 days'
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
        schema_version, aggregate_type, aggregate_id, aggregate_revision,
        payload, created_at, expires_at
    )
    VALUES (
        NEW.project_id, assigned_position, NEW.id, NEW.organization_id,
        NEW.event_type, NEW.schema_version, NEW.aggregate_type,
        NEW.aggregate_id, NEW.aggregate_revision, NEW.payload,
        NEW.created_at, NEW.created_at + interval '7 days'
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TABLE organization_event_log;
DROP TABLE organization_event_streams;
