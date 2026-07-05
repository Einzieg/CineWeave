DROP TABLE IF EXISTS provider_model_capability_presets;

DELETE FROM schema_migrations
WHERE version = '000026_model_capability_registry';
