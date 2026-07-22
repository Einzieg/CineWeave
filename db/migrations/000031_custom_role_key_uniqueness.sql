-- +goose Up

SET search_path TO public;

CREATE UNIQUE INDEX roles_organization_role_key_unique
    ON roles(organization_id, role_key)
    WHERE organization_id IS NOT NULL;

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS roles_organization_role_key_unique;
