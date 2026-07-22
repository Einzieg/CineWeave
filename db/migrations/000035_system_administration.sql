-- +goose Up

SET search_path TO public;

ALTER TABLE users
    ADD COLUMN is_system_admin BOOLEAN NOT NULL DEFAULT false;

WITH first_user AS (
    SELECT id
    FROM users
    ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, created_at, id
    LIMIT 1
)
UPDATE users
SET is_system_admin = true
WHERE id IN (SELECT id FROM first_user);

-- +goose Down

SET search_path TO public;

ALTER TABLE users
    DROP COLUMN IF EXISTS is_system_admin;
