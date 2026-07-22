-- +goose Up

SET search_path TO public;

CREATE TABLE provider_model_deletion_tombstones (
    provider_model_id UUID PRIMARY KEY,
    provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    model_key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    modality TEXT NOT NULL,
    original_status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_model_deletion_render_plan_references (
    video_render_plan_id UUID PRIMARY KEY REFERENCES video_render_plans(id) ON DELETE CASCADE,
    provider_model_id UUID NOT NULL REFERENCES provider_model_deletion_tombstones(provider_model_id) ON DELETE CASCADE,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Migration 000036 may have been active before deletion snapshots existed.
-- Preserve already-null Render Plans with deterministic disabled placeholders.
INSERT INTO provider_model_deletion_tombstones(
    provider_model_id, provider_account_id, model_key, display_name,
    modality, original_status, created_at, updated_at, deleted_at
)
SELECT gen_random_uuid(), plan.provider_account_id,
       '__deleted_render_plan_' || replace(plan.id::text, '-', ''),
       'Deleted Render Plan Model', 'video', 'disabled',
       plan.created_at, plan.updated_at, plan.updated_at
FROM video_render_plans plan
WHERE plan.provider_model_id IS NULL;

INSERT INTO provider_model_deletion_render_plan_references(video_render_plan_id, provider_model_id)
SELECT plan.id, tombstone.provider_model_id
FROM video_render_plans plan
JOIN provider_model_deletion_tombstones tombstone
  ON tombstone.provider_account_id = plan.provider_account_id
 AND tombstone.model_key = '__deleted_render_plan_' || replace(plan.id::text, '-', '')
WHERE plan.provider_model_id IS NULL;

-- +goose StatementBegin
CREATE FUNCTION cineweave_capture_provider_model_deletion()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = public
AS $$
BEGIN
    INSERT INTO provider_model_deletion_tombstones(
        provider_model_id, provider_account_id, model_key, display_name,
        modality, original_status, created_at, updated_at, deleted_at
    )
    VALUES (
        OLD.id, OLD.provider_account_id, OLD.model_key, OLD.display_name,
        OLD.modality, OLD.status, OLD.created_at, OLD.updated_at, now()
    )
    ON CONFLICT (provider_model_id) DO UPDATE
    SET provider_account_id = EXCLUDED.provider_account_id,
        model_key = EXCLUDED.model_key,
        display_name = EXCLUDED.display_name,
        modality = EXCLUDED.modality,
        original_status = EXCLUDED.original_status,
        created_at = EXCLUDED.created_at,
        updated_at = EXCLUDED.updated_at,
        deleted_at = EXCLUDED.deleted_at;

    INSERT INTO provider_model_deletion_render_plan_references(video_render_plan_id, provider_model_id)
    SELECT plan.id, OLD.id
    FROM video_render_plans plan
    WHERE plan.provider_model_id = OLD.id
    ON CONFLICT (video_render_plan_id) DO UPDATE
    SET provider_model_id = EXCLUDED.provider_model_id,
        captured_at = now();

    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER provider_models_capture_deletion
    BEFORE DELETE ON provider_models
    FOR EACH ROW EXECUTE FUNCTION cineweave_capture_provider_model_deletion();

-- Provider model deletion is an administrative provenance update. It must be
-- allowed for archived generations while all production fields stay immutable.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cineweave_enforce_active_production_generation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    active_generation UUID;
BEGIN
    IF TG_OP = 'UPDATE'
       AND TG_TABLE_NAME = 'video_render_plans'
       AND (to_jsonb(NEW) -> 'provider_model_id') IS DISTINCT FROM (to_jsonb(OLD) -> 'provider_model_id')
       AND (to_jsonb(NEW) - 'provider_model_id') = (to_jsonb(OLD) - 'provider_model_id') THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id THEN
        RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: production_generation_id is immutable'
            USING ERRCODE = '40001';
    END IF;
    SELECT active_video_production_generation_id
    INTO active_generation
    FROM projects
    WHERE id = NEW.project_id;
    IF active_generation IS NULL OR NEW.production_generation_id IS DISTINCT FROM active_generation THEN
        RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: production generation is no longer active'
            USING ERRCODE = '40001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS provider_models_capture_deletion ON provider_models;

CREATE TEMP TABLE cineweave_provider_model_restore_map (
    deleted_provider_model_id UUID PRIMARY KEY,
    restored_provider_model_id UUID NOT NULL
) ON COMMIT DROP;

-- Restoring deleted configuration is limited to this development-only Down
-- path; production migration rollback is disabled by the migration runner.
INSERT INTO provider_models(
    id, provider_account_id, model_key, display_name, modality,
    status, created_at, updated_at
)
SELECT tombstone.provider_model_id,
       tombstone.provider_account_id,
       tombstone.model_key,
       tombstone.display_name,
       tombstone.modality,
       'disabled',
       tombstone.created_at,
       now()
FROM provider_model_deletion_tombstones tombstone
WHERE EXISTS (
        SELECT 1
        FROM provider_model_deletion_render_plan_references reference
        WHERE reference.provider_model_id = tombstone.provider_model_id
    )
  AND NOT EXISTS (
        SELECT 1
        FROM provider_models current_model
        WHERE current_model.provider_account_id = tombstone.provider_account_id
          AND current_model.model_key = tombstone.model_key
    )
  AND NOT EXISTS (
        SELECT 1
        FROM provider_model_deletion_tombstones newer
        WHERE newer.provider_account_id = tombstone.provider_account_id
          AND newer.model_key = tombstone.model_key
          AND EXISTS (
              SELECT 1
              FROM provider_model_deletion_render_plan_references newer_reference
              WHERE newer_reference.provider_model_id = newer.provider_model_id
          )
          AND (newer.deleted_at, newer.provider_model_id::text) > (tombstone.deleted_at, tombstone.provider_model_id::text)
    )
ON CONFLICT DO NOTHING;

INSERT INTO cineweave_provider_model_restore_map(deleted_provider_model_id, restored_provider_model_id)
SELECT tombstone.provider_model_id, current_model.id
FROM provider_model_deletion_tombstones tombstone
JOIN provider_models current_model
  ON current_model.provider_account_id = tombstone.provider_account_id
 AND current_model.model_key = tombstone.model_key
WHERE EXISTS (
    SELECT 1
    FROM provider_model_deletion_render_plan_references reference
    WHERE reference.provider_model_id = tombstone.provider_model_id
);

UPDATE video_render_plans plan
SET provider_model_id = restore.restored_provider_model_id
FROM provider_model_deletion_render_plan_references reference
JOIN cineweave_provider_model_restore_map restore
  ON restore.deleted_provider_model_id = reference.provider_model_id
WHERE plan.id = reference.video_render_plan_id
  AND plan.provider_model_id IS NULL;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM video_render_plans WHERE provider_model_id IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back provider model deletion safeguards: a Render Plan has no recoverable provider model snapshot';
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cineweave_enforce_active_production_generation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    active_generation UUID;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id THEN
        RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: production_generation_id is immutable'
            USING ERRCODE = '40001';
    END IF;
    SELECT active_video_production_generation_id
    INTO active_generation
    FROM projects
    WHERE id = NEW.project_id;
    IF active_generation IS NULL OR NEW.production_generation_id IS DISTINCT FROM active_generation THEN
        RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: production generation is no longer active'
            USING ERRCODE = '40001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS cineweave_capture_provider_model_deletion();
DROP TABLE provider_model_deletion_render_plan_references;
DROP TABLE provider_model_deletion_tombstones;
