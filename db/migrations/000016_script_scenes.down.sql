DELETE FROM prompt_bindings
WHERE template_key IN ('script_scene_parser', 'script_scene_rewrite');

DELETE FROM prompt_versions
WHERE template_id IN (
  SELECT id FROM prompt_templates
  WHERE organization_id IS NULL
    AND template_key IN ('script_scene_parser', 'script_scene_rewrite')
)
AND metadata->>'seed' = 'system';

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key IN ('script_scene_parser', 'script_scene_rewrite')
  AND is_system = true;

DROP INDEX IF EXISTS idx_storyboard_shots_script_scene;
DROP INDEX IF EXISTS idx_scene_asset_links_asset;
DROP INDEX IF EXISTS idx_scene_asset_links_scene;
DROP INDEX IF EXISTS idx_script_scenes_review;
DROP INDEX IF EXISTS idx_script_scenes_version;
DROP INDEX IF EXISTS idx_script_scenes_script;
DROP INDEX IF EXISTS idx_script_scenes_project;

ALTER TABLE storyboard_shots
  DROP COLUMN IF EXISTS script_scene_id;

DROP TABLE IF EXISTS scene_asset_links;

DROP TRIGGER IF EXISTS script_scenes_set_updated_at ON script_scenes;
DROP TABLE IF EXISTS script_scenes;

DELETE FROM schema_migrations WHERE version = '000016_script_scenes';
