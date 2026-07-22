-- +goose Up

SET search_path TO public;

CREATE TABLE provider_credential_models (
    provider_credential_id UUID NOT NULL REFERENCES provider_credentials(id) ON DELETE CASCADE,
    provider_model_id UUID NOT NULL REFERENCES provider_models(id) ON DELETE CASCADE,
    is_available BOOLEAN NOT NULL DEFAULT true,
    last_discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_credential_id, provider_model_id)
);

CREATE INDEX provider_credential_models_available_model_idx
    ON provider_credential_models(provider_model_id, provider_credential_id)
    WHERE is_available = true;

CREATE INDEX provider_credential_models_credential_idx
    ON provider_credential_models(provider_credential_id, is_available, updated_at DESC);

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS provider_credential_models;
