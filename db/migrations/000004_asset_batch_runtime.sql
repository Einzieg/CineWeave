-- +goose Up

SET search_path TO public;

ALTER TABLE projects
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT projects_revision_check CHECK (revision > 0);

ALTER TABLE workflow_node_runs
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD CONSTRAINT workflow_node_runs_revision_check CHECK (revision > 0);

CREATE INDEX workflow_node_runs_activity_idx
    ON workflow_node_runs (workflow_run_id, status, updated_at DESC);

ALTER TABLE asset_references
    ADD COLUMN workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    ADD COLUMN node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
    ADD COLUMN source_asset_revision BIGINT,
    ADD COLUMN source_prompt_revision BIGINT,
    ADD COLUMN result_state TEXT NOT NULL DEFAULT 'applied',
    ADD CONSTRAINT asset_references_source_asset_revision_check CHECK (
        source_asset_revision IS NULL OR source_asset_revision > 0
    ),
    ADD CONSTRAINT asset_references_source_prompt_revision_check CHECK (
        source_prompt_revision IS NULL OR source_prompt_revision > 0
    ),
    ADD CONSTRAINT asset_references_result_state_check CHECK (
        result_state IN ('applied', 'conflict_skipped', 'historical')
    );

CREATE UNIQUE INDEX asset_references_node_run_uidx
    ON asset_references (node_run_id)
    WHERE node_run_id IS NOT NULL;

ALTER TABLE asset_versions
    ADD COLUMN workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    ADD COLUMN node_run_id UUID REFERENCES workflow_node_runs(id) ON DELETE SET NULL,
    ADD COLUMN source_asset_revision BIGINT,
    ADD COLUMN source_prompt_revision BIGINT,
    ADD COLUMN result_state TEXT NOT NULL DEFAULT 'applied',
    ADD CONSTRAINT asset_versions_source_asset_revision_check CHECK (
        source_asset_revision IS NULL OR source_asset_revision > 0
    ),
    ADD CONSTRAINT asset_versions_source_prompt_revision_check CHECK (
        source_prompt_revision IS NULL OR source_prompt_revision > 0
    ),
    ADD CONSTRAINT asset_versions_result_state_check CHECK (
        result_state IN ('applied', 'conflict_skipped', 'historical')
    );

CREATE UNIQUE INDEX asset_versions_node_run_uidx
    ON asset_versions (node_run_id)
    WHERE node_run_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS asset_versions_node_run_uidx;
ALTER TABLE asset_versions DROP CONSTRAINT IF EXISTS asset_versions_result_state_check;
ALTER TABLE asset_versions DROP CONSTRAINT IF EXISTS asset_versions_source_prompt_revision_check;
ALTER TABLE asset_versions DROP CONSTRAINT IF EXISTS asset_versions_source_asset_revision_check;
ALTER TABLE asset_versions DROP COLUMN IF EXISTS result_state;
ALTER TABLE asset_versions DROP COLUMN IF EXISTS source_prompt_revision;
ALTER TABLE asset_versions DROP COLUMN IF EXISTS source_asset_revision;
ALTER TABLE asset_versions DROP COLUMN IF EXISTS node_run_id;
ALTER TABLE asset_versions DROP COLUMN IF EXISTS workflow_run_id;

DROP INDEX IF EXISTS asset_references_node_run_uidx;
ALTER TABLE asset_references DROP CONSTRAINT IF EXISTS asset_references_result_state_check;
ALTER TABLE asset_references DROP CONSTRAINT IF EXISTS asset_references_source_prompt_revision_check;
ALTER TABLE asset_references DROP CONSTRAINT IF EXISTS asset_references_source_asset_revision_check;
ALTER TABLE asset_references DROP COLUMN IF EXISTS result_state;
ALTER TABLE asset_references DROP COLUMN IF EXISTS source_prompt_revision;
ALTER TABLE asset_references DROP COLUMN IF EXISTS source_asset_revision;
ALTER TABLE asset_references DROP COLUMN IF EXISTS node_run_id;
ALTER TABLE asset_references DROP COLUMN IF EXISTS workflow_run_id;

DROP INDEX IF EXISTS workflow_node_runs_activity_idx;
ALTER TABLE workflow_node_runs DROP CONSTRAINT IF EXISTS workflow_node_runs_revision_check;
ALTER TABLE workflow_node_runs DROP COLUMN IF EXISTS updated_at;
ALTER TABLE workflow_node_runs DROP COLUMN IF EXISTS revision;

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_revision_check;
ALTER TABLE projects DROP COLUMN IF EXISTS revision;
