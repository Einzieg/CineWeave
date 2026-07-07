ALTER TABLE provider_call_logs
  DROP CONSTRAINT IF EXISTS provider_call_logs_model_profile_binding_id_fkey;

ALTER TABLE provider_call_logs
  ADD CONSTRAINT provider_call_logs_model_profile_binding_id_fkey
  FOREIGN KEY (model_profile_binding_id) REFERENCES model_profile_bindings(id);

ALTER TABLE provider_async_tasks
  DROP CONSTRAINT IF EXISTS provider_async_tasks_model_profile_binding_id_fkey;

ALTER TABLE provider_async_tasks
  ADD CONSTRAINT provider_async_tasks_model_profile_binding_id_fkey
  FOREIGN KEY (model_profile_binding_id) REFERENCES model_profile_bindings(id);

DELETE FROM schema_migrations
WHERE version = '000029_provider_binding_cleanup';
