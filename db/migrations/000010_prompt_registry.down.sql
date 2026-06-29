DELETE FROM prompt_bindings
WHERE template_key IN ('storyboard_planner', 'storyboard_image_prompt', 'storyboard_video_prompt');

DELETE FROM prompt_versions
WHERE template_id IN (
  SELECT id FROM prompt_templates
  WHERE organization_id IS NULL
    AND template_key IN ('storyboard_planner', 'storyboard_image_prompt', 'storyboard_video_prompt')
)
AND metadata->>'seed' = 'system';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key IN ('storyboard_planner', 'storyboard_image_prompt', 'storyboard_video_prompt')
  AND is_system = true;

DROP TRIGGER IF EXISTS prompt_bindings_set_updated_at ON prompt_bindings;
DROP TABLE IF EXISTS prompt_bindings;

DROP INDEX IF EXISTS idx_prompt_versions_one_active;
DROP INDEX IF EXISTS idx_prompt_versions_template_version;
DROP INDEX IF EXISTS idx_prompt_templates_system_key;

ALTER TABLE prompt_versions
  DROP CONSTRAINT IF EXISTS prompt_versions_template_id_fkey,
  DROP CONSTRAINT IF EXISTS prompt_versions_status_check,
  DROP CONSTRAINT IF EXISTS prompt_versions_content_format_check;

ALTER TABLE prompt_versions
  DROP COLUMN IF EXISTS template_id,
  DROP COLUMN IF EXISTS version,
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS title,
  DROP COLUMN IF EXISTS content_format,
  DROP COLUMN IF EXISTS metadata,
  DROP COLUMN IF EXISTS activated_at;

DROP TRIGGER IF EXISTS prompt_templates_set_updated_at ON prompt_templates;

ALTER TABLE prompt_templates
  DROP CONSTRAINT IF EXISTS prompt_templates_scope_check,
  DROP CONSTRAINT IF EXISTS prompt_templates_status_check;

ALTER TABLE prompt_templates
  DROP COLUMN IF EXISTS description,
  DROP COLUMN IF EXISTS modality,
  DROP COLUMN IF EXISTS task_type,
  DROP COLUMN IF EXISTS scope,
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS is_system,
  DROP COLUMN IF EXISTS updated_at;

DELETE FROM role_permissions
WHERE permission_key IN ('prompt.read', 'prompt.manage');

DELETE FROM permissions
WHERE permission_key IN ('prompt.read', 'prompt.manage');

DELETE FROM schema_migrations WHERE version = '000010_prompt_registry';
