"use client";

import { useState } from "react";
import { Loader2, Plus } from "lucide-react";
import { toast } from "sonner";
import { AppShell, Surface, SectionTitle } from "@/components/layout/app-shell";
import { StatusBadge } from "@/components/shared/status-badge";
import { ErrorPanel } from "@/components/shared/error-panel";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi, StudioApiError } from "@/lib/api-client";
import type { JsonRecord, Organization, Workspace, Team, Role, Permission } from "@/lib/types";

export function AccessPage() {
  return (
    <AppShell active="access" title="权限管理" description="查看组织、工作区、团队、角色和权限。">
      <AccessContent />
    </AppShell>
  );
}

function AccessContent() {
  const { data: organizations = [], isLoading: orgsLoading } = useApiQuery({
    key: qk.organizations(),
    queryFn: (session) => studioApi.listOrganizations(session).then((r) => r.items),
  });
  const { data: workspaces = [], isLoading: workspacesLoading } = useApiQuery({
    key: qk.workspaces(),
    queryFn: (session) => studioApi.listWorkspaces(session).then((r) => r.items),
  });
  const { data: teams = [], isLoading: teamsLoading } = useApiQuery({
    key: qk.teams(),
    queryFn: (session) => studioApi.listTeams(session).then((r) => r.items),
  });
  const { data: roles = [], isLoading: rolesLoading } = useApiQuery({
    key: qk.roles(),
    queryFn: (session) => studioApi.listRoles(session).then((r) => r.items),
  });
  const { data: permissions = [], isLoading: permissionsLoading } = useApiQuery({
    key: qk.permissions(),
    queryFn: (session) => studioApi.listPermissions(session).then((r) => r.items),
  });

  const [teamName, setTeamName] = useState("");
  const [teamDescription, setTeamDescription] = useState("");
  const [error, setError] = useState("");
  const invalidateKeys = useInvalidateKeys();

  const createTeamMutation = useApiMutation({
    mutationFn: (session, variables: { name: string; description: string }) =>
      studioApi.createTeam(session, compactRecord({ name: variables.name, description: nullable(variables.description) })),
    onSuccess: () => {
      setTeamName("");
      setTeamDescription("");
      setError("");
      toast.success("团队已创建");
      invalidateKeys([qk.teams()]);
    },
    onError: (err) => {
      setError(errorMessage(err));
    },
  });

  function handleCreateTeam() {
    if (!teamName.trim()) return;
    createTeamMutation.mutate({ name: teamName, description: teamDescription });
  }

  return (
    <div className="grid gap-5 xl:grid-cols-2">
      <Surface>
        <SectionTitle title="创建团队" description="先创建团队，再在后续权限策略中绑定角色。" />
        <div className="grid gap-3 p-4">
          <div className="grid gap-2">
            <Label htmlFor="teamName">团队名称</Label>
            <Input id="teamName" value={teamName} onChange={(e) => setTeamName(e.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="teamDescription">团队说明</Label>
            <Textarea id="teamDescription" value={teamDescription} onChange={(e) => setTeamDescription(e.target.value)} rows={5} />
          </div>
          <Button disabled={createTeamMutation.isPending || !teamName.trim()} onClick={handleCreateTeam}>
            {createTeamMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
            创建团队
          </Button>
          <ErrorPanel message={error} />
        </div>
      </Surface>

      <Surface>
        <SectionTitle title="组织与工作区" />
        <div className="grid gap-3 p-4">
          {orgsLoading || workspacesLoading ? (
            <>
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </>
          ) : (
            <>
              {organizations.map((item) => (
                <SimpleRow key={item.id} title={item.name} detail={item.id} status="active" />
              ))}
              {workspaces.map((item) => (
                <SimpleRow key={item.id} title={item.name} detail={`工作区 · ${item.id}`} status="active" />
              ))}
            </>
          )}
        </div>
      </Surface>

      <Surface>
        <SectionTitle title="团队与角色" />
        <div className="grid gap-3 p-4">
          {teamsLoading || rolesLoading ? (
            <>
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </>
          ) : (
            <>
              {teams.map((item) => (
                <SimpleRow key={item.id} title={item.name} detail="团队" status={item.status} />
              ))}
              {roles.map((item) => (
                <SimpleRow key={item.id} title={item.name || item.roleKey} detail={item.roleKey} status="active" />
              ))}
            </>
          )}
        </div>
      </Surface>

      <Surface className="xl:col-span-2">
        <SectionTitle title="权限" description="细粒度 RBAC 权限列表。" />
        <div className="grid gap-2 p-4 md:grid-cols-2 xl:grid-cols-3">
          {permissionsLoading ? (
            <>
              <Skeleton className="h-20" />
              <Skeleton className="h-20" />
              <Skeleton className="h-20" />
            </>
          ) : (
            permissions.map((item) => (
              <Card key={item.permissionKey} className="p-3">
                <p className="text-sm font-medium">{item.name || item.permissionKey}</p>
                <p className="mt-1 text-xs text-muted-foreground">{item.description || item.permissionKey}</p>
              </Card>
            ))
          )}
        </div>
      </Surface>
    </div>
  );
}

function SimpleRow({ title, detail, status }: { title: string; detail: string; status: string }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-lg border bg-card p-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{title}</p>
        <p className="mt-1 truncate text-xs text-muted-foreground">{detail}</p>
      </div>
      <StatusBadge status={status} />
    </div>
  );
}

function compactRecord(record: Record<string, unknown>): JsonRecord {
  const out: JsonRecord = {};
  for (const [key, value] of Object.entries(record)) {
    if (value === undefined) continue;
    if (value && typeof value === "object" && !Array.isArray(value)) {
      out[key] = compactRecord(value as Record<string, unknown>);
      continue;
    }
    out[key] = value as JsonRecord[string];
  }
  return out;
}

function nullable(value: string | null | undefined) {
  const trimmed = String(value ?? "").trim();
  return trimmed ? trimmed : null;
}

function errorMessage(cause: unknown) {
  if (cause instanceof StudioApiError) {
    return cause.message;
  }
  return cause instanceof Error ? cause.message : "请求失败，请稍后重试。";
}
