DELETE FROM prompt_versions
WHERE metadata->>'seed' = 'faithful_novel_script_v2';

UPDATE prompt_versions latest
SET status = 'active',
    activated_at = COALESCE(latest.activated_at, now())
FROM prompt_templates t
WHERE latest.template_id = t.id
  AND t.organization_id IS NULL
  AND t.template_key = 'script_from_adaptation_plan'
  AND latest.id = (
    SELECT pv.id
    FROM prompt_versions pv
    WHERE pv.template_id = t.id
    ORDER BY pv.version_no DESC, pv.created_at DESC
    LIMIT 1
  );

UPDATE prompt_templates
SET name = 'Script From Adaptation Plan',
    description = 'Generate structured script content from an adaptation plan.',
    updated_at = now()
WHERE organization_id IS NULL
  AND template_key = 'script_from_adaptation_plan';

DELETE FROM schema_migrations WHERE version = '000034_faithful_novel_script_prompt';
