-- +goose Up

SET search_path TO public;

ALTER TABLE users
    ADD COLUMN username TEXT,
    ADD COLUMN username_normalized TEXT,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD CONSTRAINT users_username_pair_check CHECK (
        (username IS NULL AND username_normalized IS NULL)
        OR (username IS NOT NULL AND username_normalized IS NOT NULL)
    ),
    ADD CONSTRAINT users_username_format_check CHECK (
        username IS NULL OR username ~ '^[A-Za-z0-9](?:[A-Za-z0-9_-]{1,30}[A-Za-z0-9])?$'
    ),
    ADD CONSTRAINT users_username_normalized_format_check CHECK (
        username_normalized IS NULL OR username_normalized ~ '^[a-z0-9](?:[a-z0-9_-]{1,30}[a-z0-9])?$'
    );

CREATE UNIQUE INDEX users_username_normalized_unique
    ON users(username_normalized)
    WHERE username_normalized IS NOT NULL;

CREATE TABLE auth_organization_selection_nonces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    nonce_hash TEXT NOT NULL UNIQUE,
    organization_ids UUID[] NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT auth_organization_selection_nonces_hash_check CHECK (nonce_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auth_organization_selection_nonces_orgs_check CHECK (cardinality(organization_ids) > 1)
);

CREATE INDEX auth_organization_selection_nonces_user_idx
    ON auth_organization_selection_nonces(user_id, expires_at DESC);

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS auth_organization_selection_nonces;

DROP INDEX IF EXISTS users_username_normalized_unique;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_username_normalized_format_check,
    DROP CONSTRAINT IF EXISTS users_username_format_check,
    DROP CONSTRAINT IF EXISTS users_username_pair_check,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS username_normalized,
    DROP COLUMN IF EXISTS username;
