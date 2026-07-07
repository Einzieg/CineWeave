ALTER TABLE provider_call_logs
  DROP CONSTRAINT IF EXISTS provider_call_logs_model_profile_binding_id_fkey;

ALTER TABLE provider_call_logs
  ADD CONSTRAINT provider_call_logs_model_profile_binding_id_fkey
  FOREIGN KEY (model_profile_binding_id) REFERENCES model_profile_bindings(id) ON DELETE SET NULL;

ALTER TABLE provider_async_tasks
  DROP CONSTRAINT IF EXISTS provider_async_tasks_model_profile_binding_id_fkey;

ALTER TABLE provider_async_tasks
  ADD CONSTRAINT provider_async_tasks_model_profile_binding_id_fkey
  FOREIGN KEY (model_profile_binding_id) REFERENCES model_profile_bindings(id) ON DELETE SET NULL;

WITH stale_bindings AS (
  SELECT b.id
  FROM model_profile_bindings b
  JOIN provider_models m ON m.id = b.provider_model_id
  JOIN provider_accounts a ON a.id = m.provider_account_id
  WHERE b.enabled = false
     OR m.status <> 'active'
     OR a.status <> 'active'
)
UPDATE provider_call_logs c
SET model_profile_binding_id = NULL
WHERE c.model_profile_binding_id IN (SELECT id FROM stale_bindings);

WITH stale_bindings AS (
  SELECT b.id
  FROM model_profile_bindings b
  JOIN provider_models m ON m.id = b.provider_model_id
  JOIN provider_accounts a ON a.id = m.provider_account_id
  WHERE b.enabled = false
     OR m.status <> 'active'
     OR a.status <> 'active'
)
UPDATE provider_async_tasks t
SET model_profile_binding_id = NULL
WHERE t.model_profile_binding_id IN (SELECT id FROM stale_bindings);

WITH stale_bindings AS (
  SELECT b.id
  FROM model_profile_bindings b
  JOIN provider_models m ON m.id = b.provider_model_id
  JOIN provider_accounts a ON a.id = m.provider_account_id
  WHERE b.enabled = false
     OR m.status <> 'active'
     OR a.status <> 'active'
)
DELETE FROM model_profile_bindings b
WHERE b.id IN (SELECT id FROM stale_bindings);

INSERT INTO schema_migrations(version)
VALUES ('000029_provider_binding_cleanup')
ON CONFLICT (version) DO NOTHING;
