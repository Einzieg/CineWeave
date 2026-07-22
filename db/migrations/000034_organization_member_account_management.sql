-- +goose Up

SET search_path TO public;

ALTER TABLE users
    ADD COLUMN credential_version INTEGER NOT NULL DEFAULT 0,
    ADD CONSTRAINT users_credential_version_nonnegative CHECK (credential_version >= 0);

CREATE TABLE auth_password_reset_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT auth_password_reset_tokens_hash_check CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auth_password_reset_tokens_expiry_check CHECK (expires_at > created_at),
    CONSTRAINT auth_password_reset_tokens_terminal_check CHECK (consumed_at IS NULL OR revoked_at IS NULL)
);

CREATE INDEX auth_password_reset_tokens_user_created_idx
    ON auth_password_reset_tokens(user_id, created_at DESC);

CREATE INDEX auth_password_reset_tokens_pending_expiry_idx
    ON auth_password_reset_tokens(expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS auth_password_reset_tokens;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_credential_version_nonnegative,
    DROP COLUMN IF EXISTS credential_version;
