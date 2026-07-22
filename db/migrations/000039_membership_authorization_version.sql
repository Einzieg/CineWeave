-- +goose Up

SET search_path TO public;

ALTER TABLE organization_members
    ADD COLUMN authorization_version BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT organization_members_authorization_version_positive
        CHECK (authorization_version > 0);

ALTER TABLE auth_sessions
    ADD COLUMN membership_authorization_version BIGINT;

UPDATE auth_sessions
SET revoked_at = COALESCE(revoked_at, now())
WHERE organization_id IS NOT NULL;

UPDATE auth_sessions session
SET membership_authorization_version = COALESCE((
    SELECT membership.authorization_version
    FROM organization_members membership
    WHERE membership.organization_id = session.organization_id
      AND membership.user_id = session.user_id
    LIMIT 1
), 1)
WHERE session.organization_id IS NOT NULL;

UPDATE auth_sessions
SET membership_authorization_version = 1
WHERE organization_id IS NOT NULL
  AND membership_authorization_version IS NULL;

ALTER TABLE auth_sessions
    ADD CONSTRAINT auth_sessions_membership_authorization_version_check CHECK (
        (organization_id IS NULL AND membership_authorization_version IS NULL)
        OR (
            organization_id IS NOT NULL
            AND membership_authorization_version IS NOT NULL
            AND membership_authorization_version > 0
        )
    );

ALTER TABLE auth_organization_selection_nonces
    ADD COLUMN organization_authorization_versions JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT auth_organization_selection_nonces_authorization_versions_check
        CHECK (jsonb_typeof(organization_authorization_versions) = 'object');

UPDATE auth_organization_selection_nonces
SET consumed_at = COALESCE(consumed_at, now());

-- +goose Down

SET search_path TO public;

ALTER TABLE auth_organization_selection_nonces
    DROP CONSTRAINT IF EXISTS auth_organization_selection_nonces_authorization_versions_check,
    DROP COLUMN IF EXISTS organization_authorization_versions;

ALTER TABLE auth_sessions
    DROP CONSTRAINT IF EXISTS auth_sessions_membership_authorization_version_check,
    DROP COLUMN IF EXISTS membership_authorization_version;

ALTER TABLE organization_members
    DROP CONSTRAINT IF EXISTS organization_members_authorization_version_positive,
    DROP COLUMN IF EXISTS authorization_version;
