-- +goose Up

SET search_path TO public;

ALTER TABLE commerce_script_unit_generations
    ADD COLUMN script_unit_revision BIGINT;

WITH observed_revisions AS MATERIALIZED (
    SELECT workflow.input #>> '{identity,scriptUnitGenerationId}' AS generation_id,
           (workflow.input #>> '{identity,scriptUnitRevision}')::BIGINT AS script_unit_revision
    FROM workflow_runs workflow
    WHERE workflow.input #>> '{identity,scriptUnitGenerationId}' IS NOT NULL
      AND workflow.input #>> '{identity,scriptUnitRevision}' ~ '^[1-9][0-9]{0,17}$'
),
frozen_revisions AS (
    SELECT generation_id, min(script_unit_revision) AS script_unit_revision
    FROM observed_revisions
    GROUP BY generation_id
)
UPDATE commerce_script_unit_generations generation
SET script_unit_revision = COALESCE(
    (
        SELECT frozen.script_unit_revision
        FROM frozen_revisions frozen
        WHERE frozen.generation_id = generation.id::text
    ),
    unit.revision
)
FROM commerce_script_units unit
WHERE unit.id = generation.script_unit_id
  AND unit.product_id = generation.product_id
  AND unit.organization_id = generation.organization_id
  AND unit.project_id = generation.project_id;

ALTER TABLE commerce_script_unit_generations
    ALTER COLUMN script_unit_revision SET NOT NULL,
    ADD CONSTRAINT commerce_unit_generations_script_unit_revision_check
        CHECK (script_unit_revision > 0);

-- +goose StatementBegin
CREATE FUNCTION protect_commerce_unit_generation_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.script_unit_revision IS DISTINCT FROM OLD.script_unit_revision THEN
        RAISE EXCEPTION 'commerce script unit generation revision is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER commerce_unit_generations_revision_immutable
BEFORE UPDATE OF script_unit_revision ON commerce_script_unit_generations
FOR EACH ROW EXECUTE FUNCTION protect_commerce_unit_generation_revision();

-- +goose Down

SET search_path TO public;

DROP TRIGGER IF EXISTS commerce_unit_generations_revision_immutable
    ON commerce_script_unit_generations;
DROP FUNCTION IF EXISTS protect_commerce_unit_generation_revision();

ALTER TABLE commerce_script_unit_generations
    DROP CONSTRAINT IF EXISTS commerce_unit_generations_script_unit_revision_check,
    DROP COLUMN IF EXISTS script_unit_revision;
