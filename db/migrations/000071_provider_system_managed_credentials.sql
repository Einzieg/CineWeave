-- +goose Up

SET search_path TO public;

CREATE TABLE provider_managed_accounts (
    provider_account_id UUID PRIMARY KEY
        REFERENCES provider_accounts(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL
        REFERENCES organizations(id) ON DELETE CASCADE,
    management_scope TEXT NOT NULL DEFAULT 'system_managed',
    management_reference TEXT NOT NULL,
    ensure_request_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_managed_accounts_scope_check
        CHECK (management_scope = 'system_managed'),
    CONSTRAINT provider_managed_accounts_reference_check
        CHECK (length(btrim(management_reference)) BETWEEN 1 AND 240),
    CONSTRAINT provider_managed_accounts_hash_check
        CHECK (ensure_request_hash ~ '^[0-9a-f]{64}$'),
    UNIQUE (organization_id, management_reference)
);

CREATE INDEX provider_managed_accounts_organization_idx
    ON provider_managed_accounts(organization_id, provider_account_id);

CREATE TABLE provider_managed_credentials (
    provider_credential_id UUID PRIMARY KEY
        REFERENCES provider_credentials(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL
        REFERENCES organizations(id) ON DELETE CASCADE,
    provider_account_id UUID NOT NULL
        REFERENCES provider_accounts(id) ON DELETE CASCADE,
    management_scope TEXT NOT NULL DEFAULT 'system_managed',
    management_reference TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_managed_credentials_scope_check
        CHECK (management_scope = 'system_managed'),
    CONSTRAINT provider_managed_credentials_reference_check
        CHECK (length(btrim(management_reference)) BETWEEN 1 AND 240),
    UNIQUE (provider_account_id, management_reference)
);

CREATE INDEX provider_managed_credentials_organization_idx
    ON provider_managed_credentials(
        organization_id,
        provider_account_id,
        provider_credential_id
    );

CREATE TABLE provider_credential_imports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL
        REFERENCES organizations(id) ON DELETE CASCADE,
    attempt_id TEXT NOT NULL,
    import_idempotency_key TEXT NOT NULL,
    local_request_hash TEXT NOT NULL,
    upstream_request_hash TEXT NOT NULL,
    provider_account_id UUID
        REFERENCES provider_accounts(id) ON DELETE SET NULL,
    provider_credential_id UUID
        REFERENCES provider_credentials(id) ON DELETE SET NULL,
    provider_credential_reference TEXT NOT NULL,
    credential_key TEXT NOT NULL,
    credential_type TEXT NOT NULL,
    secret_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'imported_inactive',
    activation_request_hash TEXT,
    revocation_request_hash TEXT,
    activated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_credential_imports_status_check
        CHECK (status IN (
            'imported_inactive',
            'active',
            'revoked',
            'quarantined'
        )),
    CONSTRAINT provider_credential_imports_local_hash_check
        CHECK (local_request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT provider_credential_imports_upstream_hash_check
        CHECK (upstream_request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT provider_credential_imports_secret_hash_check
        CHECK (secret_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT provider_credential_imports_activation_hash_check
        CHECK (
            activation_request_hash IS NULL
            OR activation_request_hash ~ '^[0-9a-f]{64}$'
        ),
    CONSTRAINT provider_credential_imports_revocation_hash_check
        CHECK (
            revocation_request_hash IS NULL
            OR revocation_request_hash ~ '^[0-9a-f]{64}$'
        ),
    UNIQUE (attempt_id),
    UNIQUE (import_idempotency_key)
);

CREATE INDEX provider_credential_imports_scope_idx
    ON provider_credential_imports(
        organization_id,
        provider_account_id,
        status,
        created_at DESC
    );

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS provider_credential_imports;
DROP TABLE IF EXISTS provider_managed_credentials;
DROP TABLE IF EXISTS provider_managed_accounts;
