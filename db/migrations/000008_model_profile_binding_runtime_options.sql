-- +goose Up

SET search_path TO public;

ALTER TABLE model_profile_bindings
    ADD COLUMN runtime_options JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT model_profile_bindings_runtime_options_object_check
        CHECK (jsonb_typeof(runtime_options) = 'object');

-- +goose Down

ALTER TABLE model_profile_bindings
    DROP CONSTRAINT IF EXISTS model_profile_bindings_runtime_options_object_check,
    DROP COLUMN IF EXISTS runtime_options;
