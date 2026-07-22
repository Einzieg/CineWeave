-- +goose Up

SET search_path TO public;

ALTER TABLE organization_invitations
    ADD COLUMN binding_count INTEGER NOT NULL DEFAULT 0;

UPDATE organization_invitations invitation
SET binding_count = (
    SELECT count(*)
    FROM organization_invitation_bindings binding
    WHERE binding.invitation_id = invitation.id
);

ALTER TABLE organization_invitations
    ADD CONSTRAINT organization_invitations_binding_count_nonnegative
    CHECK (binding_count >= 0);

-- +goose Down

SET search_path TO public;

ALTER TABLE organization_invitations
    DROP CONSTRAINT IF EXISTS organization_invitations_binding_count_nonnegative,
    DROP COLUMN IF EXISTS binding_count;
