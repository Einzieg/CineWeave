-- +goose Up

SET search_path TO public;

ALTER TABLE prompt_context_plans
    ALTER COLUMN storyboard_plan_id DROP NOT NULL,
    ALTER COLUMN script_episode_id DROP NOT NULL,
    ADD COLUMN commerce_storyboard_plan_id UUID,
    ADD COLUMN commerce_product_id UUID,
    ADD COLUMN commerce_script_unit_id UUID,
    ADD COLUMN commerce_script_unit_generation_id UUID,
    ADD COLUMN commerce_localization_id UUID,
    ADD CONSTRAINT prompt_context_plans_commerce_plan_fk
        FOREIGN KEY (commerce_storyboard_plan_id, commerce_script_unit_id, commerce_script_unit_generation_id, organization_id, project_id)
        REFERENCES commerce_storyboard_plans(id, script_unit_id, script_unit_generation_id, organization_id, project_id) ON DELETE CASCADE,
    ADD CONSTRAINT prompt_context_plans_commerce_localization_fk
        FOREIGN KEY (commerce_localization_id, commerce_script_unit_id, commerce_product_id, organization_id, project_id)
        REFERENCES commerce_ad_script_localizations(id, script_unit_id, product_id, organization_id, project_id) ON DELETE RESTRICT,
    ADD CONSTRAINT prompt_context_plans_subject_kind_check CHECK (
        (
            storyboard_plan_id IS NOT NULL
            AND script_episode_id IS NOT NULL
            AND commerce_storyboard_plan_id IS NULL
            AND commerce_product_id IS NULL
            AND commerce_script_unit_id IS NULL
            AND commerce_script_unit_generation_id IS NULL
            AND commerce_localization_id IS NULL
        )
        OR (
            storyboard_plan_id IS NULL
            AND script_episode_id IS NULL
            AND script_scene_id IS NULL
            AND commerce_storyboard_plan_id IS NOT NULL
            AND commerce_product_id IS NOT NULL
            AND commerce_script_unit_id IS NOT NULL
            AND commerce_script_unit_generation_id IS NOT NULL
            AND commerce_localization_id IS NOT NULL
        )
    );

CREATE INDEX prompt_context_plans_commerce_unit_idx
    ON prompt_context_plans(commerce_script_unit_id, commerce_script_unit_generation_id, status, created_at DESC)
    WHERE commerce_script_unit_generation_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_prompt_context_plan()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('stale', 'archived') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'stale or archived prompt context plans are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND (
        NEW.id IS DISTINCT FROM OLD.id
        OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id
        OR NEW.video_production_binding_id IS DISTINCT FROM OLD.video_production_binding_id
        OR NEW.video_production_binding_revision IS DISTINCT FROM OLD.video_production_binding_revision
        OR NEW.storyboard_plan_id IS DISTINCT FROM OLD.storyboard_plan_id
        OR NEW.storyboard_shot_id IS DISTINCT FROM OLD.storyboard_shot_id
        OR NEW.script_episode_id IS DISTINCT FROM OLD.script_episode_id
        OR NEW.script_scene_id IS DISTINCT FROM OLD.script_scene_id
        OR NEW.commerce_storyboard_plan_id IS DISTINCT FROM OLD.commerce_storyboard_plan_id
        OR NEW.commerce_product_id IS DISTINCT FROM OLD.commerce_product_id
        OR NEW.commerce_script_unit_id IS DISTINCT FROM OLD.commerce_script_unit_id
        OR NEW.commerce_script_unit_generation_id IS DISTINCT FROM OLD.commerce_script_unit_generation_id
        OR NEW.commerce_localization_id IS DISTINCT FROM OLD.commerce_localization_id
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.episode_continuity_digest IS DISTINCT FROM OLD.episode_continuity_digest
        OR NEW.current_scene_script IS DISTINCT FROM OLD.current_scene_script
        OR NEW.adjacent_scene_summaries IS DISTINCT FROM OLD.adjacent_scene_summaries
        OR NEW.current_shot_state IS DISTINCT FROM OLD.current_shot_state
        OR NEW.verbatim_dialogue_cues IS DISTINCT FROM OLD.verbatim_dialogue_cues
        OR NEW.model_context_limit IS DISTINCT FROM OLD.model_context_limit
        OR NEW.model_prompt_limit IS DISTINCT FROM OLD.model_prompt_limit
        OR NEW.budget_allocation IS DISTINCT FROM OLD.budget_allocation
        OR NEW.source_hashes IS DISTINCT FROM OLD.source_hashes
        OR NEW.plan_hash IS DISTINCT FROM OLD.plan_hash
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.status NOT IN ('active', 'stale', 'archived')
    ) THEN
        RAISE EXCEPTION 'active prompt context plans are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM prompt_context_plans
        WHERE commerce_storyboard_plan_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back commerce video runtime while commerce prompt context plans exist' USING ERRCODE = '55000';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS prompt_context_plans_commerce_unit_idx;

ALTER TABLE prompt_context_plans
    DROP CONSTRAINT IF EXISTS prompt_context_plans_subject_kind_check,
    DROP CONSTRAINT IF EXISTS prompt_context_plans_commerce_localization_fk,
    DROP CONSTRAINT IF EXISTS prompt_context_plans_commerce_plan_fk,
    DROP COLUMN IF EXISTS commerce_localization_id,
    DROP COLUMN IF EXISTS commerce_script_unit_generation_id,
    DROP COLUMN IF EXISTS commerce_script_unit_id,
    DROP COLUMN IF EXISTS commerce_product_id,
    DROP COLUMN IF EXISTS commerce_storyboard_plan_id,
    ALTER COLUMN storyboard_plan_id SET NOT NULL,
    ALTER COLUMN script_episode_id SET NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_prompt_context_plan()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status IN ('stale', 'archived') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'stale or archived prompt context plans are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'active' AND (
        NEW.id IS DISTINCT FROM OLD.id
        OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.production_generation_id IS DISTINCT FROM OLD.production_generation_id
        OR NEW.video_production_binding_id IS DISTINCT FROM OLD.video_production_binding_id
        OR NEW.video_production_binding_revision IS DISTINCT FROM OLD.video_production_binding_revision
        OR NEW.storyboard_plan_id IS DISTINCT FROM OLD.storyboard_plan_id
        OR NEW.storyboard_shot_id IS DISTINCT FROM OLD.storyboard_shot_id
        OR NEW.script_episode_id IS DISTINCT FROM OLD.script_episode_id
        OR NEW.script_scene_id IS DISTINCT FROM OLD.script_scene_id
        OR NEW.revision IS DISTINCT FROM OLD.revision
        OR NEW.episode_continuity_digest IS DISTINCT FROM OLD.episode_continuity_digest
        OR NEW.current_scene_script IS DISTINCT FROM OLD.current_scene_script
        OR NEW.adjacent_scene_summaries IS DISTINCT FROM OLD.adjacent_scene_summaries
        OR NEW.current_shot_state IS DISTINCT FROM OLD.current_shot_state
        OR NEW.verbatim_dialogue_cues IS DISTINCT FROM OLD.verbatim_dialogue_cues
        OR NEW.model_context_limit IS DISTINCT FROM OLD.model_context_limit
        OR NEW.model_prompt_limit IS DISTINCT FROM OLD.model_prompt_limit
        OR NEW.budget_allocation IS DISTINCT FROM OLD.budget_allocation
        OR NEW.source_hashes IS DISTINCT FROM OLD.source_hashes
        OR NEW.plan_hash IS DISTINCT FROM OLD.plan_hash
        OR NEW.created_by IS DISTINCT FROM OLD.created_by
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
        OR NEW.status NOT IN ('active', 'stale', 'archived')
    ) THEN
        RAISE EXCEPTION 'active prompt context plans are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
