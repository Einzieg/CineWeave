UPDATE model_profiles
SET routing_strategy = 'priority_with_fallback'
WHERE routing_strategy IN ('cost_optimized', 'latency_optimized');

ALTER TABLE model_profiles
  ALTER COLUMN routing_strategy SET DEFAULT 'priority';

ALTER TABLE model_profiles
  DROP CONSTRAINT IF EXISTS model_profiles_routing_strategy_check;

ALTER TABLE model_profiles
  ADD CONSTRAINT model_profiles_routing_strategy_check
  CHECK (routing_strategy IN ('priority', 'weighted', 'priority_with_fallback'));

DELETE FROM schema_migrations WHERE version = '000009_model_profile_routing';
