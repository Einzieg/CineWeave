CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS organizations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT,
  display_name TEXT,
  avatar_url TEXT,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'pending')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  refresh_token_hash TEXT NOT NULL UNIQUE,
  user_agent TEXT,
  ip_address TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS roles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  role_key TEXT NOT NULL,
  name TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('organization', 'workspace', 'project')),
  is_system BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, role_key, scope)
);

CREATE UNIQUE INDEX IF NOT EXISTS roles_system_unique
  ON roles(role_key, scope)
  WHERE organization_id IS NULL;

CREATE TABLE IF NOT EXISTS permissions (
  permission_key TEXT PRIMARY KEY,
  description TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS role_permissions (
  role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission_key TEXT NOT NULL REFERENCES permissions(permission_key) ON DELETE CASCADE,
  PRIMARY KEY (role_id, permission_key)
);

CREATE TABLE IF NOT EXISTS organization_members (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'invited')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE IF NOT EXISTS teams (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, slug)
);

CREATE TABLE IF NOT EXISTS team_members (
  team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

CREATE TABLE IF NOT EXISTS workspaces (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  project_type TEXT,
  aspect_ratio TEXT,
  settings JSONB NOT NULL DEFAULT '{}',
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_members (
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'invited')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, user_id)
);

CREATE TABLE IF NOT EXISTS role_bindings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'team')),
  subject_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  subject_team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('organization', 'workspace', 'project')),
  resource_organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  resource_workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
  resource_project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
  created_by UUID REFERENCES users(id),
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    (subject_type = 'user' AND subject_user_id IS NOT NULL AND subject_team_id IS NULL)
    OR
    (subject_type = 'team' AND subject_team_id IS NOT NULL AND subject_user_id IS NULL)
  ),
  CHECK (
    (resource_type = 'organization' AND resource_organization_id IS NOT NULL AND resource_workspace_id IS NULL AND resource_project_id IS NULL)
    OR
    (resource_type = 'workspace' AND resource_workspace_id IS NOT NULL AND resource_organization_id IS NULL AND resource_project_id IS NULL)
    OR
    (resource_type = 'project' AND resource_project_id IS NOT NULL AND resource_organization_id IS NULL AND resource_workspace_id IS NULL)
  )
);

CREATE UNIQUE INDEX IF NOT EXISTS role_bindings_user_unique
  ON role_bindings(role_id, subject_user_id, resource_type, COALESCE(resource_organization_id, resource_workspace_id, resource_project_id))
  WHERE subject_type = 'user';

CREATE UNIQUE INDEX IF NOT EXISTS role_bindings_team_unique
  ON role_bindings(role_id, subject_team_id, resource_type, COALESCE(resource_organization_id, resource_workspace_id, resource_project_id))
  WHERE subject_type = 'team';

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS projects_set_updated_at ON projects;
CREATE TRIGGER projects_set_updated_at
BEFORE UPDATE ON projects
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS auth_sessions_user_id_idx ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS auth_sessions_organization_id_idx ON auth_sessions(organization_id);
CREATE INDEX IF NOT EXISTS organization_members_user_id_idx ON organization_members(user_id);
CREATE INDEX IF NOT EXISTS teams_organization_id_idx ON teams(organization_id);
CREATE INDEX IF NOT EXISTS team_members_user_id_idx ON team_members(user_id);
CREATE INDEX IF NOT EXISTS workspaces_organization_id_idx ON workspaces(organization_id);
CREATE INDEX IF NOT EXISTS projects_organization_id_idx ON projects(organization_id);
CREATE INDEX IF NOT EXISTS projects_workspace_id_idx ON projects(workspace_id);
CREATE INDEX IF NOT EXISTS project_members_user_id_idx ON project_members(user_id);
CREATE INDEX IF NOT EXISTS role_bindings_organization_id_idx ON role_bindings(organization_id);
CREATE INDEX IF NOT EXISTS role_bindings_subject_user_id_idx ON role_bindings(subject_user_id);
CREATE INDEX IF NOT EXISTS role_bindings_subject_team_id_idx ON role_bindings(subject_team_id);
CREATE INDEX IF NOT EXISTS role_bindings_resource_project_id_idx ON role_bindings(resource_project_id);

ALTER TABLE permissions
  ADD COLUMN IF NOT EXISTS name TEXT;

INSERT INTO permissions(permission_key, name, description) VALUES
  ('organization.read', 'Organization Read', 'Read organization'),
  ('organization.update', 'Organization Update', 'Update organization'),
  ('organization.members.manage', 'Organization Members Manage', 'Manage organization members'),
  ('workspace.read', 'Workspace Read', 'Read workspace'),
  ('workspace.create', 'Workspace Create', 'Create workspace'),
  ('project.read', 'Project Read', 'Read project'),
  ('project.create', 'Project Create', 'Create project'),
  ('project.update', 'Project Update', 'Update project'),
  ('project.delete', 'Project Delete', 'Delete project'),
  ('project.members.manage', 'Project Members Manage', 'Manage project members'),
  ('provider.read', 'Provider Read', 'Read providers'),
  ('provider.create', 'Provider Create', 'Create providers'),
  ('provider.update', 'Provider Update', 'Update providers'),
  ('provider.delete', 'Provider Delete', 'Delete providers'),
  ('provider.test', 'Provider Test', 'Test providers'),
  ('provider.credentials.rotate', 'Provider Credentials Rotate', 'Rotate provider credentials'),
  ('provider.models.manage', 'Provider Models Manage', 'Manage provider models'),
  ('model_profiles.manage', 'Model Profiles Manage', 'Manage model profiles'),
  ('workflow.run', 'Workflow Run', 'Run workflows'),
  ('workflow.cancel', 'Workflow Cancel', 'Cancel workflows'),
  ('workflow.retry', 'Workflow Retry', 'Retry workflows'),
  ('workflow.read', 'Workflow Read', 'Read workflows'),
  ('workflow.audit', 'Workflow Audit', 'Audit workflows')
ON CONFLICT (permission_key) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description;

INSERT INTO roles(role_key, name, scope, is_system) VALUES
  ('organization_owner', 'Organization Owner', 'organization', true),
  ('organization_admin', 'Organization Admin', 'organization', true),
  ('organization_member', 'Organization Member', 'organization', true),
  ('project_owner', 'Project Owner', 'project', true),
  ('project_editor', 'Project Editor', 'project', true),
  ('project_viewer', 'Project Viewer', 'project', true)
ON CONFLICT (role_key, scope) WHERE organization_id IS NULL DO UPDATE SET
  name = EXCLUDED.name,
  is_system = EXCLUDED.is_system;

INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, p.permission_key
FROM roles r
JOIN permissions p ON p.permission_key IN (
  'organization.read',
  'organization.update',
  'organization.members.manage',
  'workspace.read',
  'workspace.create',
  'project.read',
  'project.create',
  'project.update',
  'project.delete',
  'project.members.manage',
  'provider.read',
  'provider.create',
  'provider.update',
  'provider.delete',
  'provider.test',
  'provider.credentials.rotate',
  'provider.models.manage',
  'model_profiles.manage',
  'workflow.run',
  'workflow.cancel',
  'workflow.retry',
  'workflow.read',
  'workflow.audit'
)
WHERE r.organization_id IS NULL AND r.role_key = 'organization_owner'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, p.permission_key
FROM roles r
JOIN permissions p ON p.permission_key IN (
  'organization.read',
  'workspace.read',
  'workspace.create',
  'project.read',
  'project.create',
  'provider.read',
  'provider.test',
  'workflow.run',
  'workflow.cancel',
  'workflow.retry',
  'workflow.read'
)
WHERE r.organization_id IS NULL AND r.role_key = 'organization_admin'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, p.permission_key
FROM roles r
JOIN permissions p ON p.permission_key IN (
  'organization.read',
  'workspace.read',
  'project.read',
  'provider.read',
  'workflow.read'
)
WHERE r.organization_id IS NULL AND r.role_key = 'organization_member'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, p.permission_key
FROM roles r
JOIN permissions p ON p.permission_key IN (
  'project.read',
  'project.update',
  'project.delete',
  'project.members.manage',
  'workflow.run',
  'workflow.cancel',
  'workflow.retry',
  'workflow.read',
  'workflow.audit'
)
WHERE r.organization_id IS NULL AND r.role_key = 'project_owner'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, p.permission_key
FROM roles r
JOIN permissions p ON p.permission_key IN (
  'project.read',
  'project.update',
  'workflow.run',
  'workflow.cancel',
  'workflow.retry',
  'workflow.read'
)
WHERE r.organization_id IS NULL AND r.role_key = 'project_editor'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, p.permission_key
FROM roles r
JOIN permissions p ON p.permission_key IN (
  'project.read',
  'workflow.read'
)
WHERE r.organization_id IS NULL AND r.role_key = 'project_viewer'
ON CONFLICT DO NOTHING;

INSERT INTO schema_migrations(version) VALUES ('000001_auth_tenants_projects_rbac')
ON CONFLICT (version) DO NOTHING;
