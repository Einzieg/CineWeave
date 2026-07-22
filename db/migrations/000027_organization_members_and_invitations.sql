-- +goose Up

SET search_path TO public;

UPDATE organization_members
SET status = 'disabled'
WHERE status = 'invited';

ALTER TABLE organization_members
    DROP CONSTRAINT organization_members_status_check,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN disabled_at TIMESTAMPTZ,
    ADD COLUMN disabled_by UUID REFERENCES users(id),
    ADD COLUMN removed_at TIMESTAMPTZ,
    ADD COLUMN removed_by UUID REFERENCES users(id),
    ADD CONSTRAINT organization_members_status_check CHECK (status IN ('active', 'disabled', 'removed'));

UPDATE organization_members
SET disabled_at = COALESCE(disabled_at, updated_at)
WHERE status = 'disabled';

ALTER TABLE organization_members
    ADD CONSTRAINT organization_members_lifecycle_check CHECK (
        (status = 'active' AND disabled_at IS NULL AND removed_at IS NULL)
        OR (status = 'disabled' AND disabled_at IS NOT NULL AND removed_at IS NULL)
        OR (status = 'removed' AND removed_at IS NOT NULL)
    );

CREATE TABLE organization_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    base_role_id UUID NOT NULL REFERENCES roles(id),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    accepted_by UUID REFERENCES users(id),
    invited_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT organization_invitations_email_check CHECK (email = lower(btrim(email)) AND position('@' IN email) > 1),
    CONSTRAINT organization_invitations_token_hash_check CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT organization_invitations_status_check CHECK (status IN ('pending', 'accepted', 'revoked')),
    CONSTRAINT organization_invitations_acceptance_check CHECK (
        (status = 'accepted' AND accepted_at IS NOT NULL AND accepted_by IS NOT NULL)
        OR (status <> 'accepted' AND accepted_at IS NULL AND accepted_by IS NULL)
    ),
    CONSTRAINT organization_invitations_expiry_check CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX organization_invitations_pending_email_unique
    ON organization_invitations(organization_id, email)
    WHERE status = 'pending';

CREATE INDEX organization_invitations_org_created_idx
    ON organization_invitations(organization_id, created_at DESC);

CREATE TABLE organization_invitation_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invitation_id UUID NOT NULL REFERENCES organization_invitations(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id),
    resource_type TEXT NOT NULL CHECK (resource_type IN ('organization', 'workspace', 'project')),
    resource_organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    resource_workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    resource_project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT organization_invitation_bindings_resource_check CHECK (
        (resource_type = 'organization' AND resource_organization_id IS NOT NULL AND resource_workspace_id IS NULL AND resource_project_id IS NULL)
        OR (resource_type = 'workspace' AND resource_organization_id IS NULL AND resource_workspace_id IS NOT NULL AND resource_project_id IS NULL)
        OR (resource_type = 'project' AND resource_organization_id IS NULL AND resource_workspace_id IS NULL AND resource_project_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX organization_invitation_bindings_unique
    ON organization_invitation_bindings(
        invitation_id,
        role_id,
        resource_type,
        COALESCE(resource_organization_id, resource_workspace_id, resource_project_id)
    );

-- +goose Down

SET search_path TO public;

DROP TABLE IF EXISTS organization_invitation_bindings;
DROP TABLE IF EXISTS organization_invitations;

UPDATE organization_members
SET status = 'disabled', disabled_at = COALESCE(disabled_at, now())
WHERE status = 'removed';

ALTER TABLE organization_members
    DROP CONSTRAINT IF EXISTS organization_members_lifecycle_check,
    DROP CONSTRAINT IF EXISTS organization_members_status_check,
    DROP COLUMN IF EXISTS removed_by,
    DROP COLUMN IF EXISTS removed_at,
    DROP COLUMN IF EXISTS disabled_by,
    DROP COLUMN IF EXISTS disabled_at,
    DROP COLUMN IF EXISTS updated_at,
    ADD CONSTRAINT organization_members_status_check CHECK (status IN ('active', 'disabled', 'invited'));
