-- +goose Up

SET search_path TO public;

CREATE TABLE workflow_activity_views (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cleared_terminal_through TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX workflow_activity_views_user_idx
    ON workflow_activity_views(organization_id, user_id, project_id);

CREATE INDEX workflow_runs_project_created_idx
    ON workflow_runs(project_id, created_at DESC, id DESC);

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS workflow_runs_project_created_idx;
DROP TABLE IF EXISTS workflow_activity_views;
