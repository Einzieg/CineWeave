-- +goose Up

SET search_path TO public;

ALTER TABLE project_video_production_rebuilds
    ADD COLUMN source_video_production_state TEXT NOT NULL DEFAULT 'storyboard_required',
    ADD CONSTRAINT project_video_production_rebuilds_source_state_check
        CHECK (source_video_production_state IN (
            'unconfigured',
            'storyboard_required',
            'ready',
            'rebuilding',
            'blocked',
            'reconfiguration_required'
        ));

ALTER TABLE projects
    ADD COLUMN active_video_production_rebuild_id UUID,
    ADD CONSTRAINT projects_active_video_production_rebuild_fk
        FOREIGN KEY (active_video_production_rebuild_id)
        REFERENCES project_video_production_rebuilds(id) ON DELETE SET NULL,
    ADD CONSTRAINT projects_video_production_rebuild_lock_check
        CHECK (active_video_production_rebuild_id IS NULL OR video_production_locked);

CREATE INDEX projects_active_video_production_rebuild_idx
    ON projects(active_video_production_rebuild_id)
    WHERE active_video_production_rebuild_id IS NOT NULL;

-- +goose Down

SET search_path TO public;

DROP INDEX IF EXISTS projects_active_video_production_rebuild_idx;

ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_video_production_rebuild_lock_check,
    DROP CONSTRAINT IF EXISTS projects_active_video_production_rebuild_fk,
    DROP COLUMN IF EXISTS active_video_production_rebuild_id;

ALTER TABLE project_video_production_rebuilds
    DROP CONSTRAINT IF EXISTS project_video_production_rebuilds_source_state_check,
    DROP COLUMN IF EXISTS source_video_production_state;
