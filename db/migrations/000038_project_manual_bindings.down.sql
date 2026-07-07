DELETE FROM project_manual_bindings
WHERE prompt_version_id IN (
  SELECT pv.id
  FROM prompt_versions pv
  JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
  WHERE pt.organization_id IS NULL
    AND pt.template_key IN ('default_director_manual', 'default_visual_manual')
);

DELETE FROM prompt_versions
WHERE template_id IN (
  SELECT id FROM prompt_templates
  WHERE organization_id IS NULL
    AND template_key IN ('default_director_manual', 'default_visual_manual')
)
AND metadata->>'seed' = 'system';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key IN ('default_director_manual', 'default_visual_manual')
  AND is_system = true;

DROP TRIGGER IF EXISTS project_manual_bindings_set_updated_at ON project_manual_bindings;
DROP TABLE IF EXISTS project_manual_bindings;

DELETE FROM schema_migrations WHERE version = '000038_project_manual_bindings';
