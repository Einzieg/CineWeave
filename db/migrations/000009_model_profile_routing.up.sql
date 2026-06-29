ALTER TABLE model_profiles
  ALTER COLUMN routing_strategy SET DEFAULT 'priority_with_fallback';

ALTER TABLE model_profiles
  DROP CONSTRAINT IF EXISTS model_profiles_routing_strategy_check;

ALTER TABLE model_profiles
  ADD CONSTRAINT model_profiles_routing_strategy_check
  CHECK (routing_strategy IN ('priority', 'priority_with_fallback', 'weighted', 'cost_optimized', 'latency_optimized'));

INSERT INTO schema_migrations(version) VALUES ('000009_model_profile_routing')
ON CONFLICT (version) DO NOTHING;
