-- +goose Up

SET search_path TO public;

UPDATE artifacts artifact
SET production_generation_id = run.production_generation_id
FROM workflow_runs run
WHERE artifact.production_generation_id IS NULL
  AND artifact.workflow_run_id = run.id;

UPDATE media_files media
SET production_generation_id = artifact.production_generation_id
FROM artifacts artifact
WHERE media.production_generation_id IS NULL
  AND media.artifact_id = artifact.id
  AND artifact.production_generation_id IS NOT NULL;

ALTER TABLE project_timelines
    ADD CONSTRAINT project_timelines_generation_identity UNIQUE (id, project_id, production_generation_id);

ALTER TABLE shot_asset_requirements
    ADD CONSTRAINT shot_asset_requirements_shot_generation_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE storyboard_shots
    ADD CONSTRAINT storyboard_shots_plan_generation_fk
        FOREIGN KEY (storyboard_plan_id, project_id, production_generation_id)
        REFERENCES storyboard_plans(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE video_render_plans
    ADD CONSTRAINT video_render_plans_plan_generation_fk
        FOREIGN KEY (storyboard_plan_id, project_id, production_generation_id)
        REFERENCES storyboard_plans(id, project_id, production_generation_id) ON DELETE CASCADE,
    ADD CONSTRAINT video_render_plans_shot_generation_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE video_render_segments
    ADD CONSTRAINT video_render_segments_plan_generation_fk
        FOREIGN KEY (video_render_plan_id, project_id, production_generation_id)
        REFERENCES video_render_plans(id, project_id, production_generation_id) ON DELETE CASCADE,
    ADD CONSTRAINT video_render_segments_shot_generation_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE timeline_clips
    ADD CONSTRAINT timeline_clips_timeline_generation_fk
        FOREIGN KEY (timeline_id, project_id, production_generation_id)
        REFERENCES project_timelines(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE final_video_versions
    ADD CONSTRAINT final_video_versions_timeline_generation_fk
        FOREIGN KEY (timeline_id, project_id, production_generation_id)
        REFERENCES project_timelines(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE native_audio_reviews
    ADD CONSTRAINT native_audio_reviews_plan_generation_fk
        FOREIGN KEY (video_render_plan_id, project_id, production_generation_id)
        REFERENCES video_render_plans(id, project_id, production_generation_id) ON DELETE CASCADE,
    ADD CONSTRAINT native_audio_reviews_segment_generation_fk
        FOREIGN KEY (video_render_segment_id, project_id, production_generation_id)
        REFERENCES video_render_segments(id, project_id, production_generation_id) ON DELETE CASCADE;

ALTER TABLE provider_async_tasks
	ADD CONSTRAINT provider_async_tasks_render_plan_generation_fk
		FOREIGN KEY (video_render_plan_id, project_id, production_generation_id)
		REFERENCES video_render_plans(id, project_id, production_generation_id) ON DELETE RESTRICT,
    ADD CONSTRAINT provider_async_tasks_render_segment_generation_fk
        FOREIGN KEY (video_render_segment_id, project_id, production_generation_id)
		REFERENCES video_render_segments(id, project_id, production_generation_id) ON DELETE RESTRICT;

ALTER TABLE provider_call_logs
    ADD CONSTRAINT provider_call_logs_video_generation_required CHECK (
        project_id IS NULL OR task_type NOT LIKE 'video.%' OR production_generation_id IS NOT NULL
    );

ALTER TABLE cost_records
    ADD CONSTRAINT cost_records_video_generation_required CHECK (
        project_id IS NULL OR cost_type NOT LIKE 'video.%' OR production_generation_id IS NOT NULL
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cineweave_enforce_active_production_generation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    active_generation UUID;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id THEN
        RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: production_generation_id is immutable'
            USING ERRCODE = '40001';
    END IF;
    SELECT active_video_production_generation_id
    INTO active_generation
    FROM projects
    WHERE id = NEW.project_id;
    IF active_generation IS NULL OR NEW.production_generation_id IS DISTINCT FROM active_generation THEN
        RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: production generation is no longer active'
            USING ERRCODE = '40001';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER storyboard_plans_active_generation_guard
    BEFORE INSERT OR UPDATE ON storyboard_plans
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER storyboard_shots_active_generation_guard
    BEFORE INSERT OR UPDATE ON storyboard_shots
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER shot_asset_requirements_active_generation_guard
    BEFORE INSERT OR UPDATE ON shot_asset_requirements
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER video_render_plans_active_generation_guard
    BEFORE INSERT OR UPDATE ON video_render_plans
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER video_render_segments_active_generation_guard
    BEFORE INSERT OR UPDATE ON video_render_segments
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER project_timelines_active_generation_guard
    BEFORE INSERT OR UPDATE ON project_timelines
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER timeline_clips_active_generation_guard
    BEFORE INSERT OR UPDATE ON timeline_clips
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER final_video_versions_active_generation_guard
    BEFORE INSERT OR UPDATE ON final_video_versions
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();
CREATE TRIGGER native_audio_reviews_active_generation_guard
    BEFORE INSERT OR UPDATE ON native_audio_reviews
    FOR EACH ROW EXECUTE FUNCTION cineweave_enforce_active_production_generation();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cineweave_assign_artifact_production_generation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    workflow_generation UUID;
BEGIN
    IF NEW.workflow_run_id IS NOT NULL THEN
        SELECT production_generation_id INTO workflow_generation
        FROM workflow_runs WHERE id = NEW.workflow_run_id;
        IF workflow_generation IS NOT NULL THEN
            IF NEW.production_generation_id IS NULL THEN
                NEW.production_generation_id := workflow_generation;
            ELSIF NEW.production_generation_id IS DISTINCT FROM workflow_generation THEN
                RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: artifact workflow generation differs'
                    USING ERRCODE = '40001';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER artifacts_assign_production_generation
    BEFORE INSERT OR UPDATE OF workflow_run_id, production_generation_id ON artifacts
    FOR EACH ROW EXECUTE FUNCTION cineweave_assign_artifact_production_generation();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cineweave_assign_media_production_generation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    artifact_generation UUID;
BEGIN
    IF NEW.artifact_id IS NOT NULL THEN
        SELECT production_generation_id INTO artifact_generation
        FROM artifacts WHERE id = NEW.artifact_id;
        IF artifact_generation IS NOT NULL THEN
            IF NEW.production_generation_id IS NULL THEN
                NEW.production_generation_id := artifact_generation;
            ELSIF NEW.production_generation_id IS DISTINCT FROM artifact_generation THEN
                RAISE EXCEPTION 'PRODUCTION_GENERATION_MISMATCH: media artifact generation differs'
                    USING ERRCODE = '40001';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER media_files_assign_production_generation
    BEFORE INSERT OR UPDATE OF artifact_id, production_generation_id ON media_files
    FOR EACH ROW EXECUTE FUNCTION cineweave_assign_media_production_generation();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS media_files_assign_production_generation ON media_files;
DROP FUNCTION IF EXISTS cineweave_assign_media_production_generation();
DROP TRIGGER IF EXISTS artifacts_assign_production_generation ON artifacts;
DROP FUNCTION IF EXISTS cineweave_assign_artifact_production_generation();

DROP TRIGGER IF EXISTS native_audio_reviews_active_generation_guard ON native_audio_reviews;
DROP TRIGGER IF EXISTS final_video_versions_active_generation_guard ON final_video_versions;
DROP TRIGGER IF EXISTS timeline_clips_active_generation_guard ON timeline_clips;
DROP TRIGGER IF EXISTS project_timelines_active_generation_guard ON project_timelines;
DROP TRIGGER IF EXISTS video_render_segments_active_generation_guard ON video_render_segments;
DROP TRIGGER IF EXISTS video_render_plans_active_generation_guard ON video_render_plans;
DROP TRIGGER IF EXISTS shot_asset_requirements_active_generation_guard ON shot_asset_requirements;
DROP TRIGGER IF EXISTS storyboard_shots_active_generation_guard ON storyboard_shots;
DROP TRIGGER IF EXISTS storyboard_plans_active_generation_guard ON storyboard_plans;
DROP FUNCTION IF EXISTS cineweave_enforce_active_production_generation();

ALTER TABLE cost_records DROP CONSTRAINT IF EXISTS cost_records_video_generation_required;
ALTER TABLE provider_call_logs DROP CONSTRAINT IF EXISTS provider_call_logs_video_generation_required;
ALTER TABLE provider_async_tasks
    DROP CONSTRAINT IF EXISTS provider_async_tasks_render_segment_generation_fk,
    DROP CONSTRAINT IF EXISTS provider_async_tasks_render_plan_generation_fk;
ALTER TABLE native_audio_reviews
    DROP CONSTRAINT IF EXISTS native_audio_reviews_segment_generation_fk,
    DROP CONSTRAINT IF EXISTS native_audio_reviews_plan_generation_fk;
ALTER TABLE final_video_versions DROP CONSTRAINT IF EXISTS final_video_versions_timeline_generation_fk;
ALTER TABLE timeline_clips DROP CONSTRAINT IF EXISTS timeline_clips_timeline_generation_fk;
ALTER TABLE video_render_segments
    DROP CONSTRAINT IF EXISTS video_render_segments_shot_generation_fk,
    DROP CONSTRAINT IF EXISTS video_render_segments_plan_generation_fk;
ALTER TABLE video_render_plans
    DROP CONSTRAINT IF EXISTS video_render_plans_shot_generation_fk,
    DROP CONSTRAINT IF EXISTS video_render_plans_plan_generation_fk;
ALTER TABLE storyboard_shots DROP CONSTRAINT IF EXISTS storyboard_shots_plan_generation_fk;
ALTER TABLE shot_asset_requirements DROP CONSTRAINT IF EXISTS shot_asset_requirements_shot_generation_fk;
ALTER TABLE project_timelines DROP CONSTRAINT IF EXISTS project_timelines_generation_identity;
