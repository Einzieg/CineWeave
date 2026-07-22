-- +goose Up

SET search_path TO public;

ALTER TABLE shot_reference_packs
    ADD COLUMN purpose TEXT;

UPDATE shot_reference_packs
SET purpose = COALESCE(NULLIF(manifest->>'purpose', ''), 'anchor');

ALTER TABLE shot_reference_packs
    ALTER COLUMN purpose SET DEFAULT 'anchor',
    ALTER COLUMN purpose SET NOT NULL,
    ADD CONSTRAINT shot_reference_packs_purpose_check CHECK (purpose IN ('anchor', 'video'));

DROP INDEX shot_reference_packs_one_active;
CREATE UNIQUE INDEX shot_reference_packs_one_active_per_purpose
    ON shot_reference_packs(storyboard_shot_id, purpose)
    WHERE status = 'active';

ALTER TABLE shot_visual_anchors
    ADD COLUMN source_render_segment_id UUID REFERENCES video_render_segments(id) ON DELETE CASCADE,
    ADD COLUMN source_video_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    ADD COLUMN source_role TEXT,
    ADD CONSTRAINT shot_visual_anchors_source_role_check CHECK (
        source_role IS NULL OR source_role IN ('previous_segment_tail', 'provider_observed_tail')
    ),
    ADD CONSTRAINT shot_visual_anchors_observed_source_check CHECK (
        anchor_role <> 'observed_tail_frame'
        OR (source_render_segment_id IS NOT NULL AND source_video_artifact_id IS NOT NULL AND source_role IS NOT NULL)
    );

CREATE INDEX shot_visual_anchors_segment_source_idx
    ON shot_visual_anchors(source_render_segment_id, source_role, revision DESC)
    WHERE source_render_segment_id IS NOT NULL;

DROP TABLE storyboard_shot_continuity_frames;
ALTER TABLE storyboard_shots DROP COLUMN continuity_group_id;

-- +goose Down

SET search_path TO public;

ALTER TABLE storyboard_shots ADD COLUMN continuity_group_id UUID;

CREATE TABLE storyboard_shot_continuity_frames (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    storyboard_shot_id UUID NOT NULL REFERENCES storyboard_shots(id) ON DELETE CASCADE,
    source_video_artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    source_video_media_file_id UUID,
    frame_artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    frame_media_file_id UUID NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL,
    frame_role TEXT DEFAULT 'tail' NOT NULL,
    status TEXT DEFAULT 'active' NOT NULL,
    frame_time_seconds NUMERIC DEFAULT 0 NOT NULL,
    workflow_run_id UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    metadata JSONB DEFAULT '{}'::jsonb NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT storyboard_shot_continuity_frames_frame_role_check CHECK (frame_role = 'tail'),
    CONSTRAINT storyboard_shot_continuity_frames_status_check CHECK (status IN ('active', 'superseded'))
);

CREATE UNIQUE INDEX storyboard_shot_continuity_frames_active_role_idx
    ON storyboard_shot_continuity_frames(storyboard_shot_id, frame_role)
    WHERE status = 'active';
CREATE INDEX storyboard_shot_continuity_frames_project_shot_idx
    ON storyboard_shot_continuity_frames(project_id, storyboard_shot_id, created_at DESC);
CREATE INDEX storyboard_shot_continuity_frames_source_video_idx
    ON storyboard_shot_continuity_frames(source_video_artifact_id);
CREATE TRIGGER storyboard_shot_continuity_frames_set_updated_at
BEFORE UPDATE ON storyboard_shot_continuity_frames
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP INDEX shot_visual_anchors_segment_source_idx;
ALTER TABLE shot_visual_anchors
    DROP CONSTRAINT shot_visual_anchors_observed_source_check,
    DROP CONSTRAINT shot_visual_anchors_source_role_check,
    DROP COLUMN source_role,
    DROP COLUMN source_video_artifact_id,
    DROP COLUMN source_render_segment_id;

DROP INDEX shot_reference_packs_one_active_per_purpose;
ALTER TABLE shot_reference_packs
    DROP CONSTRAINT shot_reference_packs_purpose_check,
    DROP COLUMN purpose;
CREATE UNIQUE INDEX shot_reference_packs_one_active
    ON shot_reference_packs(storyboard_shot_id)
    WHERE status = 'active';
