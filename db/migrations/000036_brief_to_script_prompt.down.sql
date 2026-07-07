UPDATE prompt_versions pv
SET status = 'archived'
FROM prompt_templates pt
WHERE pv.template_id = pt.id
  AND pt.organization_id IS NULL
  AND pt.template_key = 'brief_to_script'
  AND pv.metadata->>'seed' = 'brief_to_script_v1';

INSERT INTO schema_migrations(version) VALUES ('000036_brief_to_script_prompt_down')
ON CONFLICT (version) DO NOTHING;
