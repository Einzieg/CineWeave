-- +goose Up

SET search_path TO public;

ALTER TABLE scripts
    ADD CONSTRAINT scripts_id_project_unique UNIQUE (id, project_id);

ALTER TABLE projects
    ADD COLUMN active_script_id UUID;

UPDATE projects project
SET active_script_id = (
    SELECT script.id
    FROM scripts script
    WHERE script.project_id = project.id
      AND script.current_version_id IS NOT NULL
      AND COALESCE(script.status, 'active') <> 'archived'
    ORDER BY CASE WHEN script.status = 'active' THEN 0 ELSE 1 END,
             script.updated_at DESC,
             script.created_at DESC,
             script.id
    LIMIT 1
);

ALTER TABLE projects
    ADD CONSTRAINT projects_active_script_project_fk
    FOREIGN KEY (active_script_id, id)
    REFERENCES scripts(id, project_id)
    ON DELETE SET NULL (active_script_id);

CREATE INDEX projects_active_script_idx
    ON projects(active_script_id)
    WHERE active_script_id IS NOT NULL;

-- +goose Down

SET search_path TO public;

ALTER TABLE projects
    DROP CONSTRAINT projects_active_script_project_fk;

DROP INDEX projects_active_script_idx;

ALTER TABLE projects
    DROP COLUMN active_script_id;

ALTER TABLE scripts
    DROP CONSTRAINT scripts_id_project_unique;
