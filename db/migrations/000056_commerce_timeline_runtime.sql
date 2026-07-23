-- +goose Up

SET search_path TO public;

ALTER TABLE project_timelines
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT project_timelines_revision_positive CHECK (revision > 0);

CREATE TABLE commerce_timeline_overlays (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    production_generation_id UUID NOT NULL,
    timeline_id UUID NOT NULL,
    timeline_clip_id UUID REFERENCES timeline_clips(id) ON DELETE CASCADE,
    commerce_script_unit_id UUID NOT NULL,
    commerce_script_unit_generation_id UUID NOT NULL,
    storyboard_shot_id UUID,
    role TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    text_content TEXT NOT NULL,
    start_tick BIGINT NOT NULL,
    end_tick BIGINT NOT NULL,
    style JSONB NOT NULL DEFAULT '{}'::jsonb,
    content_hash TEXT NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT commerce_timeline_overlays_timeline_fk
        FOREIGN KEY (timeline_id, project_id, production_generation_id,
                     commerce_script_unit_id, commerce_script_unit_generation_id)
        REFERENCES project_timelines(id, project_id, production_generation_id,
                                     commerce_script_unit_id, commerce_script_unit_generation_id)
        ON DELETE CASCADE,
    CONSTRAINT commerce_timeline_overlays_generation_fk
        FOREIGN KEY (commerce_script_unit_generation_id, commerce_script_unit_id, organization_id, project_id)
        REFERENCES commerce_script_unit_generations(id, script_unit_id, organization_id, project_id)
        ON DELETE RESTRICT,
    CONSTRAINT commerce_timeline_overlays_shot_fk
        FOREIGN KEY (storyboard_shot_id, project_id, production_generation_id)
        REFERENCES storyboard_shots(id, project_id, production_generation_id)
        ON DELETE CASCADE,
    CONSTRAINT commerce_timeline_overlays_role_check
        CHECK (role IN ('onscreen_text', 'cta_end_card')),
    CONSTRAINT commerce_timeline_overlays_ordinal_check CHECK (ordinal >= 0),
    CONSTRAINT commerce_timeline_overlays_text_check CHECK (btrim(text_content) <> ''),
    CONSTRAINT commerce_timeline_overlays_tick_check CHECK (start_tick >= 0 AND end_tick > start_tick),
    CONSTRAINT commerce_timeline_overlays_style_check CHECK (jsonb_typeof(style) = 'object'),
    CONSTRAINT commerce_timeline_overlays_hash_check CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT commerce_timeline_overlays_subject_check CHECK (
        (role = 'onscreen_text' AND timeline_clip_id IS NOT NULL AND storyboard_shot_id IS NOT NULL)
        OR (role = 'cta_end_card' AND timeline_clip_id IS NULL)
    ),
    UNIQUE(timeline_id, role, ordinal)
);

CREATE INDEX commerce_timeline_overlays_unit_idx
    ON commerce_timeline_overlays(commerce_script_unit_id, commerce_script_unit_generation_id, timeline_id, ordinal);

-- +goose StatementBegin
CREATE FUNCTION validate_commerce_timeline_overlay_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    clip_shot UUID;
    clip_project UUID;
    clip_generation UUID;
    shot_unit UUID;
    shot_generation UUID;
BEGIN
    IF NEW.timeline_clip_id IS NOT NULL THEN
        SELECT clip.storyboard_shot_id, clip.project_id, clip.production_generation_id
          INTO clip_shot, clip_project, clip_generation
        FROM timeline_clips clip
        WHERE clip.id = NEW.timeline_clip_id
          AND clip.timeline_id = NEW.timeline_id;

        IF clip_project IS NULL
           OR clip_project IS DISTINCT FROM NEW.project_id
           OR clip_generation IS DISTINCT FROM NEW.production_generation_id
           OR clip_shot IS DISTINCT FROM NEW.storyboard_shot_id THEN
            RAISE EXCEPTION 'commerce timeline overlay clip identity mismatch' USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.storyboard_shot_id IS NOT NULL THEN
        SELECT shot.commerce_script_unit_id, shot.commerce_script_unit_generation_id
          INTO shot_unit, shot_generation
        FROM storyboard_shots shot
        WHERE shot.id = NEW.storyboard_shot_id
          AND shot.project_id = NEW.project_id
          AND shot.production_generation_id = NEW.production_generation_id
          AND shot.deleted_at IS NULL;

        IF shot_unit IS DISTINCT FROM NEW.commerce_script_unit_id
           OR shot_generation IS DISTINCT FROM NEW.commerce_script_unit_generation_id THEN
            RAISE EXCEPTION 'commerce timeline overlay shot identity mismatch' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_timeline_overlays_identity
BEFORE INSERT OR UPDATE OF organization_id, project_id, production_generation_id,
    timeline_id, timeline_clip_id, commerce_script_unit_id,
    commerce_script_unit_generation_id, storyboard_shot_id
ON commerce_timeline_overlays
FOR EACH ROW EXECUTE FUNCTION validate_commerce_timeline_overlay_identity();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS commerce_timeline_overlays_identity ON commerce_timeline_overlays;
DROP FUNCTION IF EXISTS validate_commerce_timeline_overlay_identity();
DROP TABLE IF EXISTS commerce_timeline_overlays;

ALTER TABLE project_timelines
    DROP CONSTRAINT IF EXISTS project_timelines_revision_positive,
    DROP COLUMN IF EXISTS revision;
