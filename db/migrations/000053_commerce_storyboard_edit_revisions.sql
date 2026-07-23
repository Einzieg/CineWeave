-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_storyboard_plans
    ADD COLUMN edit_revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN projection_hash TEXT,
    ADD COLUMN allowed_shot_durations INTEGER[];

UPDATE commerce_storyboard_plans
SET projection_hash = plan_hash
WHERE projection_hash IS NULL;

UPDATE commerce_storyboard_plans plan
SET allowed_shot_durations = COALESCE(
    (
        SELECT array_agg(DISTINCT ((shot.end_tick - shot.start_tick) / plan.timeline_timebase)::INTEGER ORDER BY ((shot.end_tick - shot.start_tick) / plan.timeline_timebase)::INTEGER)
        FROM storyboard_shots shot
        WHERE shot.commerce_storyboard_plan_id = plan.id
          AND shot.deleted_at IS NULL
          AND (shot.end_tick - shot.start_tick) % plan.timeline_timebase = 0
    ),
    ARRAY[plan.target_duration_seconds]
)
WHERE allowed_shot_durations IS NULL;

ALTER TABLE commerce_storyboard_plans
    ALTER COLUMN projection_hash SET NOT NULL,
    ALTER COLUMN allowed_shot_durations SET NOT NULL,
    ADD CONSTRAINT commerce_storyboard_plans_edit_revision_check CHECK (edit_revision > 0),
    ADD CONSTRAINT commerce_storyboard_plans_projection_hash_check CHECK (projection_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT commerce_storyboard_plans_allowed_durations_check CHECK (
        cardinality(allowed_shot_durations) > 0
        AND 0 < ALL(allowed_shot_durations)
    );

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_storyboard_plan_allowed_durations()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('ready', 'stale', 'archived')
       AND NEW.allowed_shot_durations IS DISTINCT FROM OLD.allowed_shot_durations THEN
        RAISE EXCEPTION 'reviewed commerce storyboard plan duration capabilities are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_storyboard_plans_allowed_durations_immutable
BEFORE UPDATE ON commerce_storyboard_plans
FOR EACH ROW EXECUTE FUNCTION protect_commerce_storyboard_plan_allowed_durations();

ALTER TABLE commerce_shot_contracts
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN manual_override BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN edited_by UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN edited_at TIMESTAMPTZ,
    ADD CONSTRAINT commerce_shot_contracts_revision_check CHECK (revision > 0);

CREATE INDEX commerce_storyboard_plans_unit_active_edit_idx
    ON commerce_storyboard_plans(script_unit_id, active, edit_revision DESC)
    WHERE status <> 'archived';

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS commerce_storyboard_plans_unit_active_edit_idx;

DROP TRIGGER IF EXISTS commerce_storyboard_plans_allowed_durations_immutable ON commerce_storyboard_plans;
DROP FUNCTION IF EXISTS protect_commerce_storyboard_plan_allowed_durations();

ALTER TABLE commerce_shot_contracts
    DROP CONSTRAINT IF EXISTS commerce_shot_contracts_revision_check,
    DROP COLUMN IF EXISTS edited_at,
    DROP COLUMN IF EXISTS edited_by,
    DROP COLUMN IF EXISTS manual_override,
    DROP COLUMN IF EXISTS revision;

ALTER TABLE commerce_storyboard_plans
    DROP CONSTRAINT IF EXISTS commerce_storyboard_plans_allowed_durations_check,
    DROP CONSTRAINT IF EXISTS commerce_storyboard_plans_projection_hash_check,
    DROP CONSTRAINT IF EXISTS commerce_storyboard_plans_edit_revision_check,
    DROP COLUMN IF EXISTS allowed_shot_durations,
    DROP COLUMN IF EXISTS projection_hash,
    DROP COLUMN IF EXISTS edit_revision;
