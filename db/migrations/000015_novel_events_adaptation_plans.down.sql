DELETE FROM prompt_versions
WHERE template_id IN (
  SELECT id FROM prompt_templates
  WHERE organization_id IS NULL
    AND template_key IN ('novel_event_extraction', 'adaptation_plan_generation', 'script_from_adaptation_plan')
);

DELETE FROM prompt_templates
WHERE organization_id IS NULL
  AND template_key IN ('novel_event_extraction', 'adaptation_plan_generation', 'script_from_adaptation_plan');

DELETE FROM role_permissions
WHERE permission_key IN ('novel_event.read', 'novel_event.write', 'adaptation_plan.read', 'adaptation_plan.write');

DELETE FROM permissions
WHERE permission_key IN ('novel_event.read', 'novel_event.write', 'adaptation_plan.read', 'adaptation_plan.write');

DROP TRIGGER IF EXISTS adaptation_plans_set_updated_at ON adaptation_plans;
DROP TABLE IF EXISTS adaptation_plans;
DROP TABLE IF EXISTS novel_event_links;
DROP TRIGGER IF EXISTS novel_events_set_updated_at ON novel_events;
DROP TABLE IF EXISTS novel_events;

DELETE FROM schema_migrations WHERE version = '000015_novel_events_adaptation_plans';
