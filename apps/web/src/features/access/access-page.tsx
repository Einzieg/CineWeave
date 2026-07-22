"use client";

import { AppShell } from "@/components/layout/app-shell";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { InvitationsPanel } from "@/features/access/invitations-panel";
import { MembersPanel } from "@/features/access/members-panel";
import { AuditLogsPanel } from "@/features/access/audit-logs-panel";
import { OrganizationSettingsPanel } from "@/features/access/organization-settings-panel";
import { RolesBindingsPanel } from "@/features/access/roles-bindings-panel";
import { TeamsPanel } from "@/features/access/teams-panel";
import { sessionHasPermission, useStudioSession } from "@/lib/session";

export function AccessPage() {
  const { session } = useStudioSession();
  const canReadMembers = sessionHasPermission(session, "member.read");
  const canManageMembers = sessionHasPermission(session, "member.manage");
  const canReadTeams = sessionHasPermission(session, "team.read");
  const canManageTeams = sessionHasPermission(session, "team.manage");
  const canReadRoles = sessionHasPermission(session, "role.read");
  const canManageRoles = sessionHasPermission(session, "role.manage");
  const canUpdateOrganization = sessionHasPermission(session, "organization.update");
  const canReadAudit = sessionHasPermission(session, "audit.read");
  const defaultTab = canReadMembers ? "members" : canReadTeams ? "teams" : canReadRoles ? "roles" : "organization";
  const permissionKey = session.permissions?.join(",") ?? "pending";
  return (
    <AppShell active="access" title="组织与权限" description="管理成员、邀请、团队以及角色授权。">
      <Tabs key={permissionKey} defaultValue={defaultTab} className="gap-4">
        <TabsList variant="line" className="h-auto w-full justify-start overflow-x-auto border-b bg-transparent px-0">
          {canReadMembers ? <TabsTrigger value="members">成员</TabsTrigger> : null}
          {canReadMembers ? <TabsTrigger value="invitations">邀请</TabsTrigger> : null}
          {canReadTeams ? <TabsTrigger value="teams">团队</TabsTrigger> : null}
          {canReadRoles ? <TabsTrigger value="roles">角色与权限</TabsTrigger> : null}
          <TabsTrigger value="organization">组织设置</TabsTrigger>
          {canReadAudit ? <TabsTrigger value="audit">审计记录</TabsTrigger> : null}
        </TabsList>
        {canReadMembers ? <TabsContent value="members"><MembersPanel canManage={canManageMembers} /></TabsContent> : null}
        {canReadMembers ? <TabsContent value="invitations"><InvitationsPanel canManage={canManageMembers} /></TabsContent> : null}
        {canReadTeams ? <TabsContent value="teams"><TeamsPanel canManage={canManageTeams} /></TabsContent> : null}
        {canReadRoles ? <TabsContent value="roles"><RolesBindingsPanel canManage={canManageRoles} /></TabsContent> : null}
        <TabsContent value="organization"><OrganizationSettingsPanel canUpdate={canUpdateOrganization} /></TabsContent>
        {canReadAudit ? <TabsContent value="audit"><AuditLogsPanel /></TabsContent> : null}
      </Tabs>
    </AppShell>
  );
}
