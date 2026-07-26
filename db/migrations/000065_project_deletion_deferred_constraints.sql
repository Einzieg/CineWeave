-- +goose Up

SET search_path TO public;

-- These tables carry immutable project identity but were only connected to the
-- project graph through RESTRICT references. Give every project-owned Commerce
-- row an explicit cascade root so hard deletion cannot leave orphaned rows.
ALTER TABLE commerce_ad_script_localizations
    ADD CONSTRAINT commerce_ad_script_localizations_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_localization_segments
    ADD CONSTRAINT commerce_localization_segments_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_product_reference_pack_items
    ADD CONSTRAINT commerce_reference_pack_items_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_product_reference_packs
    ADD CONSTRAINT commerce_reference_packs_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_production_run_item_attempts
    ADD CONSTRAINT commerce_run_attempts_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_production_run_items
    ADD CONSTRAINT commerce_run_items_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_production_runs
    ADD CONSTRAINT commerce_runs_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_sales_script_contracts
    ADD CONSTRAINT commerce_sales_script_contracts_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_script_unit_batch_coordinators
    ADD CONSTRAINT commerce_batch_coordinators_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_script_unit_batch_items
    ADD CONSTRAINT commerce_batch_items_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_script_unit_rebuilds
    ADD CONSTRAINT commerce_script_unit_rebuilds_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

ALTER TABLE commerce_storyboard_plans
    ADD CONSTRAINT commerce_storyboard_plans_project_owner_fk
    FOREIGN KEY (project_id, organization_id)
    REFERENCES projects(id, organization_id) ON DELETE CASCADE;

-- Provider and media history deliberately outlives a project through nullable
-- project identity. Detach its remaining project-runtime references instead of
-- letting them block deletion of the owned production graph.
ALTER TABLE cost_records
    DROP CONSTRAINT cost_records_production_generation_fk,
    ADD CONSTRAINT cost_records_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE SET NULL;

ALTER TABLE media_files
    DROP CONSTRAINT media_files_production_generation_fk,
    ADD CONSTRAINT media_files_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE SET NULL;

ALTER TABLE provider_async_tasks
    DROP CONSTRAINT provider_async_tasks_production_generation_fk,
    ADD CONSTRAINT provider_async_tasks_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE SET NULL,
    DROP CONSTRAINT provider_async_tasks_render_plan_generation_fk,
    ADD CONSTRAINT provider_async_tasks_render_plan_generation_fk
    FOREIGN KEY (video_render_plan_id, project_id, production_generation_id)
    REFERENCES video_render_plans(id, project_id, production_generation_id)
    ON DELETE SET NULL,
    DROP CONSTRAINT provider_async_tasks_render_segment_generation_fk,
    ADD CONSTRAINT provider_async_tasks_render_segment_generation_fk
    FOREIGN KEY (video_render_segment_id, project_id, production_generation_id)
    REFERENCES video_render_segments(id, project_id, production_generation_id)
    ON DELETE SET NULL,
    DROP CONSTRAINT provider_async_tasks_video_production_binding_fk,
    ADD CONSTRAINT provider_async_tasks_video_production_binding_fk
    FOREIGN KEY (video_production_binding_id, project_id)
    REFERENCES project_video_production_bindings(id, project_id)
    ON DELETE SET NULL;

ALTER TABLE provider_call_logs
    DROP CONSTRAINT provider_call_logs_production_generation_fk,
    ADD CONSTRAINT provider_call_logs_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE SET NULL;

ALTER TABLE provider_requests
    DROP CONSTRAINT provider_requests_production_generation_fk,
    ADD CONSTRAINT provider_requests_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE SET NULL,
    DROP CONSTRAINT provider_requests_video_production_binding_fk,
    ADD CONSTRAINT provider_requests_video_production_binding_fk
    FOREIGN KEY (video_production_binding_id, project_id)
    REFERENCES project_video_production_bindings(id, project_id)
    ON DELETE SET NULL;

-- A project hard delete cascades through several independently owned branches
-- and detaches nullable history/media rows. RESTRICT triggers can fire before a
-- sibling row is deleted or before the referencing branch disappears, so valid
-- project graphs can fail depending on trigger order. Convert only constraints
-- from project-owned rows into rows deleted or detached by the same project
-- operation to deferred-capable NO ACTION constraints. Existing initial modes
-- are preserved; newly deferrable constraints remain initially immediate. The
-- deletion activity explicitly defers the complete set.
-- +goose StatementBegin
DO $$
DECLARE
    blocker RECORD;
    constraint_definition TEXT;
    converted_count INTEGER := 0;
    marker_prefix CONSTANT TEXT := 'cineweave:project-deletion-deferred:v1:';
    marker TEXT;
    target_deferrability TEXT;
BEGIN
    FOR blocker IN
        WITH RECURSIVE project_owned(oid) AS (
            SELECT 'projects'::regclass::oid
            UNION
            SELECT source_table.oid
            FROM project_owned parent
            JOIN pg_constraint foreign_key
              ON foreign_key.confrelid = parent.oid
             AND foreign_key.contype = 'f'
             AND foreign_key.confdeltype = 'c'
            JOIN pg_class source_table
              ON source_table.oid = foreign_key.conrelid
            JOIN pg_namespace source_namespace
              ON source_namespace.oid = source_table.relnamespace
             AND source_namespace.nspname = 'public'
        ),
        project_mutated(oid) AS (
            SELECT oid
            FROM project_owned
            UNION
            SELECT source_table.oid
            FROM project_owned parent
            JOIN pg_constraint foreign_key
              ON foreign_key.confrelid = parent.oid
             AND foreign_key.contype = 'f'
             AND foreign_key.confdeltype IN ('n', 'd')
            JOIN pg_class source_table
              ON source_table.oid = foreign_key.conrelid
            JOIN pg_namespace source_namespace
              ON source_namespace.oid = source_table.relnamespace
             AND source_namespace.nspname = 'public'
        )
        SELECT
            source_table.relname AS table_name,
            foreign_key.conname AS constraint_name,
            pg_get_constraintdef(foreign_key.oid) AS definition,
            foreign_key.condeferrable AS was_deferrable,
            foreign_key.condeferred AS was_initially_deferred,
            obj_description(foreign_key.oid, 'pg_constraint') AS existing_comment
        FROM pg_constraint foreign_key
        JOIN pg_class source_table
          ON source_table.oid = foreign_key.conrelid
        WHERE foreign_key.contype = 'f'
          AND foreign_key.confdeltype = 'r'
          AND foreign_key.conrelid IN (SELECT oid FROM project_owned)
          AND foreign_key.confrelid IN (SELECT oid FROM project_mutated)
        ORDER BY source_table.relname, foreign_key.conname
    LOOP
        IF blocker.existing_comment IS NOT NULL THEN
            RAISE EXCEPTION
                'project deletion migration refuses to overwrite comment on %.%',
                blocker.table_name,
                blocker.constraint_name;
        END IF;

        constraint_definition := replace(
            blocker.definition,
            ' ON DELETE RESTRICT',
            ' ON DELETE NO ACTION'
        );
        constraint_definition := regexp_replace(
            constraint_definition,
            ' DEFERRABLE( INITIALLY (IMMEDIATE|DEFERRED))?$',
            ''
        );
        IF constraint_definition = blocker.definition THEN
            RAISE EXCEPTION
                'project deletion migration cannot rewrite %.%: %',
                blocker.table_name,
                blocker.constraint_name,
                blocker.definition;
        END IF;

        marker := marker_prefix || CASE
            WHEN NOT blocker.was_deferrable THEN 'not-deferrable'
            WHEN blocker.was_initially_deferred THEN 'deferrable-deferred'
            ELSE 'deferrable-immediate'
        END;
        target_deferrability := CASE
            WHEN blocker.was_initially_deferred
                THEN 'DEFERRABLE INITIALLY DEFERRED'
            ELSE 'DEFERRABLE INITIALLY IMMEDIATE'
        END;

        EXECUTE format(
            'ALTER TABLE public.%I DROP CONSTRAINT %I',
            blocker.table_name,
            blocker.constraint_name
        );
        EXECUTE format(
            'ALTER TABLE public.%I ADD CONSTRAINT %I %s %s',
            blocker.table_name,
            blocker.constraint_name,
            constraint_definition,
            target_deferrability
        );
        EXECUTE format(
            'COMMENT ON CONSTRAINT %I ON public.%I IS %L',
            blocker.constraint_name,
            blocker.table_name,
            marker
        );
        converted_count := converted_count + 1;
    END LOOP;

    IF converted_count = 0 THEN
        RAISE EXCEPTION 'project deletion migration found no blocking constraints';
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down

SET search_path TO public;

-- +goose StatementBegin
DO $$
DECLARE
    changed RECORD;
    constraint_definition TEXT;
    deferrability_clause TEXT;
    restored_count INTEGER := 0;
    marker_prefix CONSTANT TEXT := 'cineweave:project-deletion-deferred:v1:';
BEGIN
    FOR changed IN
        SELECT
            source_table.relname AS table_name,
            foreign_key.conname AS constraint_name,
            pg_get_constraintdef(foreign_key.oid) AS definition,
            obj_description(foreign_key.oid, 'pg_constraint') AS marker
        FROM pg_constraint foreign_key
        JOIN pg_class source_table
          ON source_table.oid = foreign_key.conrelid
        JOIN pg_namespace source_namespace
          ON source_namespace.oid = source_table.relnamespace
         AND source_namespace.nspname = 'public'
        WHERE foreign_key.contype = 'f'
          AND obj_description(foreign_key.oid, 'pg_constraint')
              LIKE marker_prefix || '%'
        ORDER BY source_table.relname, foreign_key.conname
    LOOP
        constraint_definition := replace(
            changed.definition,
            ' ON DELETE NO ACTION',
            ''
        );
        constraint_definition := regexp_replace(
            constraint_definition,
            ' DEFERRABLE( INITIALLY (IMMEDIATE|DEFERRED))?$',
            ''
        );
        IF constraint_definition ~ ' ON DELETE ' THEN
            RAISE EXCEPTION
                'project deletion rollback cannot restore %.%: %',
                changed.table_name,
                changed.constraint_name,
                changed.definition;
        END IF;
        constraint_definition := constraint_definition || ' ON DELETE RESTRICT';
        deferrability_clause := CASE changed.marker
            WHEN marker_prefix || 'not-deferrable'
                THEN 'NOT DEFERRABLE'
            WHEN marker_prefix || 'deferrable-immediate'
                THEN 'DEFERRABLE INITIALLY IMMEDIATE'
            WHEN marker_prefix || 'deferrable-deferred'
                THEN 'DEFERRABLE INITIALLY DEFERRED'
            ELSE NULL
        END;
        IF deferrability_clause IS NULL THEN
            RAISE EXCEPTION
                'project deletion rollback found unknown marker on %.%: %',
                changed.table_name,
                changed.constraint_name,
                changed.marker;
        END IF;

        EXECUTE format(
            'ALTER TABLE public.%I DROP CONSTRAINT %I',
            changed.table_name,
            changed.constraint_name
        );
        EXECUTE format(
            'ALTER TABLE public.%I ADD CONSTRAINT %I %s %s',
            changed.table_name,
            changed.constraint_name,
            constraint_definition,
            deferrability_clause
        );
        restored_count := restored_count + 1;
    END LOOP;

    IF restored_count = 0 THEN
        RAISE EXCEPTION 'project deletion rollback found no converted constraints';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE provider_requests
    DROP CONSTRAINT provider_requests_video_production_binding_fk,
    ADD CONSTRAINT provider_requests_video_production_binding_fk
    FOREIGN KEY (video_production_binding_id, project_id)
    REFERENCES project_video_production_bindings(id, project_id)
    ON DELETE RESTRICT,
    DROP CONSTRAINT provider_requests_production_generation_fk,
    ADD CONSTRAINT provider_requests_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE RESTRICT;

ALTER TABLE provider_call_logs
    DROP CONSTRAINT provider_call_logs_production_generation_fk,
    ADD CONSTRAINT provider_call_logs_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE RESTRICT;

ALTER TABLE provider_async_tasks
    DROP CONSTRAINT provider_async_tasks_video_production_binding_fk,
    ADD CONSTRAINT provider_async_tasks_video_production_binding_fk
    FOREIGN KEY (video_production_binding_id, project_id)
    REFERENCES project_video_production_bindings(id, project_id)
    ON DELETE RESTRICT,
    DROP CONSTRAINT provider_async_tasks_render_segment_generation_fk,
    ADD CONSTRAINT provider_async_tasks_render_segment_generation_fk
    FOREIGN KEY (video_render_segment_id, project_id, production_generation_id)
    REFERENCES video_render_segments(id, project_id, production_generation_id)
    ON DELETE RESTRICT,
    DROP CONSTRAINT provider_async_tasks_render_plan_generation_fk,
    ADD CONSTRAINT provider_async_tasks_render_plan_generation_fk
    FOREIGN KEY (video_render_plan_id, project_id, production_generation_id)
    REFERENCES video_render_plans(id, project_id, production_generation_id)
    ON DELETE RESTRICT,
    DROP CONSTRAINT provider_async_tasks_production_generation_fk,
    ADD CONSTRAINT provider_async_tasks_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE RESTRICT;

ALTER TABLE media_files
    DROP CONSTRAINT media_files_production_generation_fk,
    ADD CONSTRAINT media_files_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE RESTRICT;

ALTER TABLE cost_records
    DROP CONSTRAINT cost_records_production_generation_fk,
    ADD CONSTRAINT cost_records_production_generation_fk
    FOREIGN KEY (production_generation_id, project_id)
    REFERENCES project_video_production_generations(id, project_id)
    ON DELETE RESTRICT;

ALTER TABLE commerce_storyboard_plans
    DROP CONSTRAINT commerce_storyboard_plans_project_owner_fk;
ALTER TABLE commerce_script_unit_rebuilds
    DROP CONSTRAINT commerce_script_unit_rebuilds_project_owner_fk;
ALTER TABLE commerce_script_unit_batch_items
    DROP CONSTRAINT commerce_batch_items_project_owner_fk;
ALTER TABLE commerce_script_unit_batch_coordinators
    DROP CONSTRAINT commerce_batch_coordinators_project_owner_fk;
ALTER TABLE commerce_sales_script_contracts
    DROP CONSTRAINT commerce_sales_script_contracts_project_owner_fk;
ALTER TABLE commerce_production_runs
    DROP CONSTRAINT commerce_runs_project_owner_fk;
ALTER TABLE commerce_production_run_items
    DROP CONSTRAINT commerce_run_items_project_owner_fk;
ALTER TABLE commerce_production_run_item_attempts
    DROP CONSTRAINT commerce_run_attempts_project_owner_fk;
ALTER TABLE commerce_product_reference_packs
    DROP CONSTRAINT commerce_reference_packs_project_owner_fk;
ALTER TABLE commerce_product_reference_pack_items
    DROP CONSTRAINT commerce_reference_pack_items_project_owner_fk;
ALTER TABLE commerce_localization_segments
    DROP CONSTRAINT commerce_localization_segments_project_owner_fk;
ALTER TABLE commerce_ad_script_localizations
    DROP CONSTRAINT commerce_ad_script_localizations_project_owner_fk;
