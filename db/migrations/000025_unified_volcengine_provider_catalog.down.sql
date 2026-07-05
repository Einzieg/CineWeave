DELETE FROM provider_catalog_entries
WHERE provider_key = 'volcengine_ark';

UPDATE provider_catalog_entries
SET enabled = true
WHERE provider_key IN (
  'volcengine_ark_text',
  'volcengine_seedream_image',
  'volcengine_seedance_video'
);

DELETE FROM schema_migrations
WHERE version = '000025_unified_volcengine_provider_catalog';
