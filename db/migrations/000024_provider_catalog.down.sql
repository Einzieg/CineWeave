DELETE FROM provider_catalog_entries
WHERE provider_key IN (
  'deepseek',
  'volcengine_ark_text',
  'volcengine_seedream_image',
  'volcengine_seedance_video',
  'kling_video',
  'openai_compatible_custom'
);

DROP TRIGGER IF EXISTS provider_catalog_entries_set_updated_at ON provider_catalog_entries;
DROP TABLE IF EXISTS provider_catalog_entries;

DELETE FROM schema_migrations WHERE version = '000024_provider_catalog';
