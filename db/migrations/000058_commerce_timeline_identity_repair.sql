-- +goose Up

SET search_path TO public;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_commerce_timeline_overlay_identity()
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
        SELECT contract.script_unit_id, contract.script_unit_generation_id
          INTO shot_unit, shot_generation
        FROM storyboard_shots shot
        JOIN commerce_shot_contracts contract
          ON contract.storyboard_shot_id = shot.id
         AND contract.commerce_storyboard_plan_id = shot.commerce_storyboard_plan_id
         AND contract.organization_id = shot.organization_id
         AND contract.project_id = shot.project_id
        WHERE shot.id = NEW.storyboard_shot_id
          AND shot.organization_id = NEW.organization_id
          AND shot.project_id = NEW.project_id
          AND shot.production_generation_id = NEW.production_generation_id
          AND shot.deleted_at IS NULL;

        IF shot_unit IS NULL
           OR shot_unit IS DISTINCT FROM NEW.commerce_script_unit_id
           OR shot_generation IS DISTINCT FROM NEW.commerce_script_unit_generation_id THEN
            RAISE EXCEPTION 'commerce timeline overlay shot identity mismatch' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_commerce_timeline_overlay_identity()
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
