-- +goose Up

SET search_path TO public;

UPDATE prompt_versions
SET status = 'archived',
    activated_at = NULL
WHERE prompt_template_id IN (
    SELECT id
    FROM prompt_templates
    WHERE organization_id IS NULL
      AND template_key IN ('shot_video_prompt_agent', 'shot_video_prompt_review_agent')
);

UPDATE prompt_templates
SET status = 'archived',
    updated_at = now()
WHERE organization_id IS NULL
  AND template_key IN ('shot_video_prompt_agent', 'shot_video_prompt_review_agent');

ALTER TABLE projects DROP COLUMN production_mode;

-- +goose Down

SET search_path TO public;

ALTER TABLE projects
    ADD COLUMN production_mode TEXT NOT NULL DEFAULT 'single_frame_i2v';

UPDATE prompt_templates
SET status = 'active',
    updated_at = now()
WHERE organization_id IS NULL
  AND template_key IN ('shot_video_prompt_agent', 'shot_video_prompt_review_agent');

WITH latest_versions AS (
    SELECT DISTINCT ON (prompt_template_id) id
    FROM prompt_versions
    WHERE prompt_template_id IN (
        SELECT id
        FROM prompt_templates
        WHERE organization_id IS NULL
          AND template_key IN ('shot_video_prompt_agent', 'shot_video_prompt_review_agent')
    )
    ORDER BY prompt_template_id, version_no DESC
)
UPDATE prompt_versions
SET status = CASE WHEN id IN (SELECT id FROM latest_versions) THEN 'active' ELSE 'archived' END,
    activated_at = CASE WHEN id IN (SELECT id FROM latest_versions) THEN now() ELSE NULL END
WHERE prompt_template_id IN (
    SELECT id
    FROM prompt_templates
    WHERE organization_id IS NULL
      AND template_key IN ('shot_video_prompt_agent', 'shot_video_prompt_review_agent')
);
