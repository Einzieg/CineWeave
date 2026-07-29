-- +goose Up
SET search_path TO public;

ALTER TABLE storyboard_shots
    DROP CONSTRAINT storyboard_shots_plan_kind_check,
    ADD CONSTRAINT storyboard_shots_plan_kind_check CHECK (
        storyboard_plan_id IS NULL
        OR commerce_storyboard_plan_id IS NULL
    );

COMMENT ON CONSTRAINT storyboard_shots_plan_kind_check ON storyboard_shots IS
    'A shot may use the legacy workflow identity with neither plan, or belong to exactly one narrative/Commerce plan; both plan identities are forbidden.';

-- +goose Down
SET search_path TO public;

COMMENT ON CONSTRAINT storyboard_shots_plan_kind_check ON storyboard_shots IS NULL;

ALTER TABLE storyboard_shots
    DROP CONSTRAINT storyboard_shots_plan_kind_check,
    ADD CONSTRAINT storyboard_shots_plan_kind_check CHECK (
        (storyboard_plan_id IS NOT NULL AND commerce_storyboard_plan_id IS NULL)
        OR (storyboard_plan_id IS NULL AND commerce_storyboard_plan_id IS NOT NULL)
    );
