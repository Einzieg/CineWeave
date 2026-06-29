DROP TRIGGER IF EXISTS projects_set_updated_at ON projects;
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS role_bindings;
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS organization_members;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;

DELETE FROM schema_migrations WHERE version = '000001_auth_tenants_projects_rbac';

