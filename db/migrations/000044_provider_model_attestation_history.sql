-- +goose Up

SET search_path TO public;

-- Capability attestations are immutable historical evidence. Deleting a model
-- must detach the live configuration identity without deleting evidence that
-- an existing Render Plan still references.
ALTER TABLE provider_model_capability_attestations
    DROP CONSTRAINT provider_model_capability_attestations_provider_model_id_fkey,
    ALTER COLUMN provider_model_id DROP NOT NULL,
    ADD CONSTRAINT provider_model_capability_attestations_provider_model_id_fkey
        FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE SET NULL;

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM provider_model_capability_attestations
        WHERE provider_model_id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot restore cascading provider model attestations: historical attestations reference deleted models';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE provider_model_capability_attestations
    DROP CONSTRAINT provider_model_capability_attestations_provider_model_id_fkey,
    ALTER COLUMN provider_model_id SET NOT NULL,
    ADD CONSTRAINT provider_model_capability_attestations_provider_model_id_fkey
        FOREIGN KEY (provider_model_id) REFERENCES provider_models(id) ON DELETE CASCADE;
