ALTER TABLE roles
  ADD COLUMN IF NOT EXISTS description TEXT,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE permissions
  ADD COLUMN IF NOT EXISTS id UUID DEFAULT gen_random_uuid(),
  ADD COLUMN IF NOT EXISTS name TEXT;

UPDATE permissions
SET name = COALESCE(name, permission_key)
WHERE name IS NULL;

ALTER TABLE permissions
  ALTER COLUMN name SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS permissions_id_unique ON permissions(id);

ALTER TABLE role_permissions
  ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE teams
  ADD COLUMN IF NOT EXISTS description TEXT,
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE team_members
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
  ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id);

DROP TRIGGER IF EXISTS roles_set_updated_at ON roles;
CREATE TRIGGER roles_set_updated_at
BEFORE UPDATE ON roles
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS teams_set_updated_at ON teams;
CREATE TRIGGER teams_set_updated_at
BEFORE UPDATE ON teams
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

INSERT INTO permissions(permission_key, name, description) VALUES
  ('organization.read', 'Organization Read', 'Read organization'),
  ('organization.manage', 'Organization Manage', 'Manage organization'),
  ('workspace.read', 'Workspace Read', 'Read workspace'),
  ('workspace.manage', 'Workspace Manage', 'Manage workspaces'),
  ('project.read', 'Project Read', 'Read project'),
  ('project.write', 'Project Write', 'Create or update project'),
  ('project.delete', 'Project Delete', 'Delete project'),
  ('asset.read', 'Asset Read', 'Read assets'),
  ('asset.write', 'Asset Write', 'Create or update assets'),
  ('asset.delete', 'Asset Delete', 'Delete assets'),
  ('artifact.read', 'Artifact Read', 'Read artifacts'),
  ('media.read', 'Media Read', 'Read media files'),
  ('provider.read', 'Provider Read', 'Read provider settings'),
  ('provider.manage', 'Provider Manage', 'Manage provider settings'),
  ('workflow.read', 'Workflow Read', 'Read workflows'),
  ('workflow.run', 'Workflow Run', 'Run workflows'),
  ('workflow.cancel', 'Workflow Cancel', 'Cancel workflows'),
  ('team.read', 'Team Read', 'Read teams'),
  ('team.manage', 'Team Manage', 'Manage teams'),
  ('role.read', 'Role Read', 'Read roles and bindings'),
  ('role.manage', 'Role Manage', 'Manage role bindings'),
  ('audit.read', 'Audit Read', 'Read audit data'),
  ('admin.manage', 'Admin Manage', 'Full administrative access')
ON CONFLICT (permission_key) DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description;

INSERT INTO roles(role_key, name, description, scope, is_system) VALUES
  ('org_owner', 'Organization Owner', 'Full organization owner access', 'organization', true),
  ('org_admin', 'Organization Admin', 'Administrative organization access', 'organization', true),
  ('org_member', 'Organization Member', 'Basic organization member access', 'organization', true),
  ('provider_admin', 'Provider Admin', 'Provider administration access', 'organization', true),
  ('project_owner', 'Project Owner', 'Full project access', 'project', true),
  ('project_editor', 'Project Editor', 'Edit project content and workflows', 'project', true),
  ('project_viewer', 'Project Viewer', 'Read project content and workflows', 'project', true)
ON CONFLICT (role_key, scope) WHERE organization_id IS NULL DO UPDATE SET
  name = EXCLUDED.name,
  description = EXCLUDED.description,
  is_system = EXCLUDED.is_system;

WITH role_grants(role_key, permission_key) AS (
  VALUES
    ('org_owner', 'admin.manage'),
    ('organization_owner', 'admin.manage'),
    ('org_admin', 'organization.read'),
    ('org_admin', 'workspace.read'),
    ('org_admin', 'workspace.manage'),
    ('org_admin', 'project.read'),
    ('org_admin', 'project.write'),
    ('org_admin', 'project.delete'),
    ('org_admin', 'asset.read'),
    ('org_admin', 'asset.write'),
    ('org_admin', 'asset.delete'),
    ('org_admin', 'artifact.read'),
    ('org_admin', 'media.read'),
    ('org_admin', 'provider.read'),
    ('org_admin', 'provider.manage'),
    ('org_admin', 'workflow.read'),
    ('org_admin', 'workflow.run'),
    ('org_admin', 'workflow.cancel'),
    ('org_admin', 'team.read'),
    ('org_admin', 'team.manage'),
    ('org_admin', 'role.read'),
    ('org_admin', 'role.manage'),
    ('org_admin', 'audit.read'),
    ('organization_admin', 'organization.read'),
    ('organization_admin', 'workspace.read'),
    ('organization_admin', 'workspace.manage'),
    ('organization_admin', 'project.read'),
    ('organization_admin', 'project.write'),
    ('organization_admin', 'project.delete'),
    ('organization_admin', 'asset.read'),
    ('organization_admin', 'asset.write'),
    ('organization_admin', 'asset.delete'),
    ('organization_admin', 'artifact.read'),
    ('organization_admin', 'media.read'),
    ('organization_admin', 'provider.read'),
    ('organization_admin', 'provider.manage'),
    ('organization_admin', 'workflow.read'),
    ('organization_admin', 'workflow.run'),
    ('organization_admin', 'workflow.cancel'),
    ('organization_admin', 'team.read'),
    ('organization_admin', 'team.manage'),
    ('organization_admin', 'role.read'),
    ('organization_admin', 'role.manage'),
    ('organization_admin', 'audit.read'),
    ('org_member', 'organization.read'),
    ('org_member', 'workspace.read'),
    ('org_member', 'project.read'),
    ('organization_member', 'organization.read'),
    ('organization_member', 'workspace.read'),
    ('organization_member', 'project.read'),
    ('project_owner', 'project.read'),
    ('project_owner', 'project.write'),
    ('project_owner', 'project.delete'),
    ('project_owner', 'asset.read'),
    ('project_owner', 'asset.write'),
    ('project_owner', 'asset.delete'),
    ('project_owner', 'artifact.read'),
    ('project_owner', 'media.read'),
    ('project_owner', 'workflow.read'),
    ('project_owner', 'workflow.run'),
    ('project_owner', 'workflow.cancel'),
    ('project_editor', 'project.read'),
    ('project_editor', 'project.write'),
    ('project_editor', 'asset.read'),
    ('project_editor', 'asset.write'),
    ('project_editor', 'artifact.read'),
    ('project_editor', 'media.read'),
    ('project_editor', 'workflow.read'),
    ('project_editor', 'workflow.run'),
    ('project_editor', 'workflow.cancel'),
    ('project_viewer', 'project.read'),
    ('project_viewer', 'asset.read'),
    ('project_viewer', 'artifact.read'),
    ('project_viewer', 'media.read'),
    ('project_viewer', 'workflow.read'),
    ('provider_admin', 'provider.read'),
    ('provider_admin', 'provider.manage')
)
INSERT INTO role_permissions(role_id, permission_key)
SELECT r.id, g.permission_key
FROM role_grants g
JOIN roles r ON r.organization_id IS NULL AND r.role_key = g.role_key
JOIN permissions p ON p.permission_key = g.permission_key
ON CONFLICT DO NOTHING;

INSERT INTO schema_migrations(version) VALUES ('000007_fine_grained_rbac')
ON CONFLICT (version) DO NOTHING;
