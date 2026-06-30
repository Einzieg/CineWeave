DELETE FROM role_permissions
WHERE permission_key IN (
  'source.read',
  'source.write',
  'script.read',
  'script.write',
  'asset.analyze',
  'asset.generate',
  'storyboard.generate'
);

DELETE FROM permissions
WHERE permission_key IN (
  'source.read',
  'source.write',
  'script.read',
  'script.write',
  'asset.analyze',
  'asset.generate',
  'storyboard.generate'
);

DELETE FROM prompt_versions
WHERE template_id IN (
  SELECT id FROM prompt_templates
  WHERE organization_id IS NULL
    AND template_key IN (
      'script_agent_generate',
      'script_agent_rewrite',
      'script_asset_extraction',
      'canonical_asset_image_prompt',
      'storyboard_from_script',
      'shot_asset_requirement_analysis',
      'derived_asset_image_prompt',
      'shot_image_prompt',
      'shot_video_prompt'
    )
);

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key IN (
    'script_agent_generate',
    'script_agent_rewrite',
    'script_asset_extraction',
    'canonical_asset_image_prompt',
    'storyboard_from_script',
    'shot_asset_requirement_analysis',
    'derived_asset_image_prompt',
    'shot_image_prompt',
    'shot_video_prompt'
  );

DROP TABLE IF EXISTS shot_asset_requirements;
DROP TABLE IF EXISTS script_asset_links;
DROP TABLE IF EXISTS asset_versions;
DROP TABLE IF EXISTS canonical_assets;
DROP TABLE IF EXISTS agent_runs;
DROP TABLE IF EXISTS agent_messages;
DROP TABLE IF EXISTS agent_sessions;

ALTER TABLE storyboard_shots
  DROP COLUMN IF EXISTS script_id,
  DROP COLUMN IF EXISTS storyboard_source;

ALTER TABLE script_versions
  DROP COLUMN IF EXISTS organization_id,
  DROP COLUMN IF EXISTS project_id,
  DROP COLUMN IF EXISTS version,
  DROP COLUMN IF EXISTS content,
  DROP COLUMN IF EXISTS content_format,
  DROP COLUMN IF EXISTS source_type,
  DROP COLUMN IF EXISTS prompt_version_id,
  DROP COLUMN IF EXISTS prompt_hash;

ALTER TABLE scripts
  DROP COLUMN IF EXISTS source_id,
  DROP COLUMN IF EXISTS status;

ALTER TABLE novel_chapters
  DROP COLUMN IF EXISTS organization_id,
  DROP COLUMN IF EXISTS project_id,
  DROP COLUMN IF EXISTS source_id,
  DROP COLUMN IF EXISTS volume_title,
  DROP COLUMN IF EXISTS chapter_title,
  DROP COLUMN IF EXISTS content,
  DROP COLUMN IF EXISTS event_state,
  DROP COLUMN IF EXISTS event_summary,
  DROP COLUMN IF EXISTS error_message,
  DROP COLUMN IF EXISTS updated_at;

DROP TABLE IF EXISTS project_sources;

ALTER TABLE projects
  DROP COLUMN IF EXISTS content_type,
  DROP COLUMN IF EXISTS video_ratio,
  DROP COLUMN IF EXISTS art_style,
  DROP COLUMN IF EXISTS director_manual,
  DROP COLUMN IF EXISTS visual_manual,
  DROP COLUMN IF EXISTS image_model_profile_key,
  DROP COLUMN IF EXISTS video_model_profile_key,
  DROP COLUMN IF EXISTS script_model_profile_key,
  DROP COLUMN IF EXISTS image_quality,
  DROP COLUMN IF EXISTS production_mode;

DELETE FROM schema_migrations WHERE version = '000012_script_driven_production';
