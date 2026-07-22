"use client";

import { useMemo, useState } from "react";
import { Loader2, Pencil, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ErrorPanel } from "@/components/shared/error-panel";
import { CustomRoleDialog } from "@/features/access/custom-role-dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { permissionKeyLabel, roleKeyLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { sessionHasPermission, useStudioSession } from "@/lib/session";
import type { JsonRecord, Role } from "@/lib/types";

export function RolesBindingsPanel({ canManage }: { canManage: boolean }) {
  const { session } = useStudioSession();
  const canReadMembers = sessionHasPermission(session, "member.read");
  const canReadTeams = sessionHasPermission(session, "team.read");
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedRoleId, setSelectedRoleId] = useState("");
  const [customRoleTarget, setCustomRoleTarget] = useState<"new" | Role | null>(null);
  const [bindingRoleId, setBindingRoleId] = useState("");
  const [subjectType, setSubjectType] = useState<"user" | "team">("user");
  const [subjectId, setSubjectId] = useState("");
  const [resourceId, setResourceId] = useState("");
  const [roleFilter, setRoleFilter] = useState("all");
  const [subjectFilter, setSubjectFilter] = useState("all");
  const [resourceFilter, setResourceFilter] = useState("all");
  const [bindingPage, setBindingPage] = useState(1);
  const invalidateKeys = useInvalidateKeys();
  const filters = useMemo(() => ({
    ...(roleFilter === "all" ? {} : { roleId: roleFilter }),
    ...(subjectFilter === "all" ? {} : { subjectType: subjectFilter }),
    ...(resourceFilter === "all" ? {} : { resourceType: resourceFilter }),
    page: String(bindingPage),
    pageSize: "25",
  }), [bindingPage, resourceFilter, roleFilter, subjectFilter]);
  const roles = useApiQuery({ key: qk.roles(), queryFn: (activeSession) => studioApi.listRoles(activeSession).then((response) => response.items) });
  const roleDetail = useApiQuery({ key: qk.role(selectedRoleId || "none"), enabled: Boolean(selectedRoleId), queryFn: (activeSession) => studioApi.getRole(activeSession, selectedRoleId) });
  const bindings = useApiQuery({ key: qk.roleBindings(filters), queryFn: (activeSession) => studioApi.listRoleBindings(activeSession, filters) });
  const members = useApiQuery({ key: qk.organizationMembers("", "active", 1), enabled: canManage && canReadMembers, queryFn: (activeSession) => studioApi.listOrganizationMembers(activeSession, activeSession.organizationId, { status: "active", page: 1, pageSize: 100 }) });
  const teams = useApiQuery({ key: qk.teams(), enabled: canManage && canReadTeams, queryFn: (activeSession) => studioApi.listTeams(activeSession).then((response) => response.items.filter((team) => team.status === "active")) });
  const workspaces = useApiQuery({ key: qk.workspaces(), enabled: canManage, queryFn: (activeSession) => studioApi.listWorkspaces(activeSession).then((response) => response.items) });
  const projects = useApiQuery({ key: qk.projects(), enabled: canManage, queryFn: (activeSession) => studioApi.listProjects(activeSession).then((response) => response.items) });
  const selectedBindingRole = roles.data?.find((role) => role.id === bindingRoleId);
  const createMutation = useApiMutation({
    mutationFn: (activeSession, body: JsonRecord) => studioApi.createRoleBinding(activeSession, body),
    onSuccess: () => {
      toast.success("角色绑定已创建");
      setCreateOpen(false);
      setSubjectId("");
      setResourceId("");
      invalidateKeys([qk.roleBindings(filters), qk.organizationMembers("", "active", 1), qk.teams()]);
    },
  });
  const deleteMutation = useApiMutation({
    mutationFn: (activeSession, bindingId: string) => studioApi.deleteRoleBinding(activeSession, bindingId),
    onSuccess: () => {
      toast.success("角色绑定已撤销");
      invalidateKeys([qk.roleBindings(filters), qk.organizationMembers("", "active", 1), qk.teams()]);
    },
  });

  function createBinding() {
    if (!selectedBindingRole || !subjectId) return;
    const scope = selectedBindingRole.scope;
    const targetResourceId = scope === "organization" ? session.organizationId : resourceId;
    if (!targetResourceId) return;
    const body: JsonRecord = {
      organizationId: session.organizationId,
      roleId: selectedBindingRole.id,
      subjectType,
      ...(subjectType === "user" ? { subjectUserId: subjectId } : { subjectTeamId: subjectId }),
      resourceType: scope ?? "organization",
      ...(scope === "organization" ? { resourceOrganizationId: targetResourceId } : {}),
      ...(scope === "workspace" ? { resourceWorkspaceId: targetResourceId } : {}),
      ...(scope === "project" ? { resourceProjectId: targetResourceId } : {}),
    };
    createMutation.mutate(body);
  }

  return (
    <div className="grid gap-4 2xl:grid-cols-[22rem_minmax(0,1fr)]">
      <div className="overflow-hidden rounded-xl border bg-card">
        <div className="flex items-center justify-between gap-3 border-b px-4 py-4"><div><h2 className="text-sm font-semibold">角色目录</h2><p className="mt-1 text-xs text-muted-foreground">查看系统角色，或维护组织自定义角色。</p></div>{canManage ? <Button size="sm" variant="outline" onClick={() => setCustomRoleTarget("new")}><Plus className="mr-2 h-4 w-4" />新建角色</Button> : null}</div>
        {roles.isLoading ? <div className="grid gap-2 p-4"><Skeleton className="h-14" /><Skeleton className="h-14" /></div> : roles.error ? <div className="p-4"><ErrorPanel message={errorMessage(roles.error)} /></div> : <div className="divide-y">{roles.data?.map((role) => <div key={role.id} className="flex items-center gap-2 pr-2 transition-colors hover:bg-muted/50"><button type="button" className="flex min-w-0 flex-1 items-center justify-between gap-3 px-4 py-3 text-left" onClick={() => setSelectedRoleId(role.id)}><div className="min-w-0"><p className="truncate text-sm font-medium">{roleDisplayName(role)}</p><p className="mt-1 text-xs text-muted-foreground">{scopeLabel(role.scope)} · {role.isSystem ? "系统角色" : "自定义角色"}</p></div><ShieldCheck className="h-4 w-4 shrink-0 text-muted-foreground" /></button>{canManage && !role.isSystem ? <Button size="icon-sm" variant="ghost" aria-label="编辑自定义角色" onClick={() => setCustomRoleTarget(role)}><Pencil className="h-4 w-4" /></Button> : null}</div>)}</div>}
      </div>

      <div className="overflow-hidden rounded-xl border bg-card">
        <div className="flex flex-col gap-3 border-b px-4 py-4 lg:flex-row lg:items-center lg:justify-between">
          <div><h2 className="text-sm font-semibold">角色绑定</h2><p className="mt-1 text-xs text-muted-foreground">为成员或团队分配组织、工作区和项目角色。</p></div>
          <div className="flex flex-wrap gap-2">
            <Select value={subjectFilter} onValueChange={(value) => { setSubjectFilter(value); setBindingPage(1); }}><SelectTrigger className="w-28"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">全部主体</SelectItem><SelectItem value="user">成员</SelectItem><SelectItem value="team">团队</SelectItem></SelectContent></Select>
            <Select value={resourceFilter} onValueChange={(value) => { setResourceFilter(value); setBindingPage(1); }}><SelectTrigger className="w-32"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">全部范围</SelectItem><SelectItem value="organization">组织</SelectItem><SelectItem value="workspace">工作区</SelectItem><SelectItem value="project">项目</SelectItem></SelectContent></Select>
            <Select value={roleFilter} onValueChange={(value) => { setRoleFilter(value); setBindingPage(1); }}><SelectTrigger className="w-36"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">全部角色</SelectItem>{roles.data?.map((role) => <SelectItem key={role.id} value={role.id}>{roleDisplayName(role)}</SelectItem>)}</SelectContent></Select>
            {canManage ? <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); if (open && !bindingRoleId && roles.data?.[0]) setBindingRoleId(roles.data[0].id); }}>
              <DialogTrigger asChild><Button size="sm"><Plus className="mr-2 h-4 w-4" />添加绑定</Button></DialogTrigger>
              <DialogContent>
                <DialogHeader><DialogTitle>添加角色绑定</DialogTitle><DialogDescription>角色范围决定可选择的目标资源。</DialogDescription></DialogHeader>
                <div className="grid gap-4">
                  <Field label="角色"><Select value={bindingRoleId} onValueChange={(value) => { setBindingRoleId(value); setResourceId(""); }}><SelectTrigger><SelectValue placeholder="选择角色" /></SelectTrigger><SelectContent>{roles.data?.map((role) => <SelectItem key={role.id} value={role.id}>{roleDisplayName(role)} · {scopeLabel(role.scope)}</SelectItem>)}</SelectContent></Select></Field>
                  <Field label="授权主体"><div className="grid grid-cols-[7rem_minmax(0,1fr)] gap-2"><Select value={subjectType} onValueChange={(value: "user" | "team") => { setSubjectType(value); setSubjectId(""); }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="user">成员</SelectItem><SelectItem value="team">团队</SelectItem></SelectContent></Select><Select value={subjectId} onValueChange={setSubjectId}><SelectTrigger><SelectValue placeholder={subjectType === "user" ? "选择成员" : "选择团队"} /></SelectTrigger><SelectContent>{subjectType === "user" ? members.data?.items.map((member) => <SelectItem key={member.user.id} value={member.user.id}>{member.user.displayName || member.user.username || member.user.email}</SelectItem>) : teams.data?.map((team) => <SelectItem key={team.id} value={team.id}>{team.name}</SelectItem>)}</SelectContent></Select></div></Field>
                  <Field label="资源范围">{selectedBindingRole?.scope === "organization" ? <div className="rounded-md border bg-muted/30 px-3 py-2 text-sm">当前组织</div> : <Select value={resourceId} onValueChange={setResourceId}><SelectTrigger><SelectValue placeholder={selectedBindingRole?.scope === "workspace" ? "选择工作区" : "选择项目"} /></SelectTrigger><SelectContent>{selectedBindingRole?.scope === "workspace" ? workspaces.data?.map((workspace) => <SelectItem key={workspace.id} value={workspace.id}>{workspace.name}</SelectItem>) : projects.data?.map((project) => <SelectItem key={project.id} value={project.id}>{project.name}</SelectItem>)}</SelectContent></Select>}</Field>
                  {createMutation.error ? <ErrorPanel message={errorMessage(createMutation.error)} /> : null}
                </div>
                <DialogFooter><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button><Button disabled={!bindingRoleId || !subjectId || (selectedBindingRole?.scope !== "organization" && !resourceId) || createMutation.isPending} onClick={createBinding}>{createMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}创建绑定</Button></DialogFooter>
              </DialogContent>
            </Dialog> : null}
          </div>
        </div>
        {bindings.isLoading ? <div className="grid gap-2 p-4"><Skeleton className="h-14" /><Skeleton className="h-14" /></div> : bindings.error ? <div className="p-4"><ErrorPanel message={errorMessage(bindings.error)} /></div> : bindings.data?.items.length ? (
          <Table><TableHeader><TableRow><TableHead>主体</TableHead><TableHead>角色</TableHead><TableHead>资源</TableHead><TableHead className="hidden lg:table-cell">来源</TableHead><TableHead className="w-16 text-right">操作</TableHead></TableRow></TableHeader><TableBody>{bindings.data.items.map((binding) => <TableRow key={binding.id}><TableCell><p className="font-medium">{binding.subjectName || "未命名主体"}</p><p className="text-xs text-muted-foreground">{binding.subjectType === "team" ? "团队" : "直接授权"}</p></TableCell><TableCell>{roleBindingDisplayName(binding.roleKey, binding.roleName)}</TableCell><TableCell><p>{binding.resourceName || "当前组织"}</p><p className="text-xs text-muted-foreground">{scopeLabel(binding.resourceType)}</p></TableCell><TableCell className="hidden text-xs text-muted-foreground lg:table-cell">{binding.subjectType === "team" ? "通过团队继承" : "成员直接角色"}</TableCell><TableCell className="text-right">{canManage ? <RoleBindingDeleteButton pending={deleteMutation.isPending} onDelete={() => deleteMutation.mutate(binding.id)} /> : <span className="text-xs text-muted-foreground">—</span>}</TableCell></TableRow>)}</TableBody></Table>
        ) : <div className="px-4 py-16 text-center"><p className="text-sm font-medium">没有匹配的角色绑定</p><p className="mt-1 text-xs text-muted-foreground">添加绑定或调整筛选条件。</p></div>}
        {bindings.data && bindings.data.total > 0 ? <div className="flex items-center justify-between gap-3 border-t px-4 py-3"><p className="text-xs text-muted-foreground">共 {bindings.data.total} 项，第 {bindings.data.page} / {Math.max(1, Math.ceil(bindings.data.total / bindings.data.pageSize))} 页</p><div className="flex gap-2"><Button size="sm" variant="outline" disabled={bindingPage <= 1} onClick={() => setBindingPage((value) => Math.max(1, value - 1))}>上一页</Button><Button size="sm" variant="outline" disabled={bindingPage * bindings.data.pageSize >= bindings.data.total} onClick={() => setBindingPage((value) => value + 1)}>下一页</Button></div></div> : null}
        {deleteMutation.error ? <div className="border-t p-4"><ErrorPanel message={errorMessage(deleteMutation.error)} /></div> : null}
      </div>

      <Dialog open={Boolean(selectedRoleId)} onOpenChange={(open) => { if (!open) setSelectedRoleId(""); }}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader><DialogTitle>{roleDetail.data ? roleDisplayName(roleDetail.data) : "角色详情"}</DialogTitle><DialogDescription>{roleDetail.data ? `${scopeLabel(roleDetail.data.scope)} · 当前 ${roleDetail.data.bindingCount ?? 0} 项绑定` : "正在读取角色权限"}</DialogDescription></DialogHeader>
          {roleDetail.isLoading ? <Skeleton className="h-48" /> : roleDetail.error ? <ErrorPanel message={errorMessage(roleDetail.error)} /> : <PermissionList role={roleDetail.data} />}
        </DialogContent>
      </Dialog>
      {canManage && customRoleTarget ? <CustomRoleDialog role={customRoleTarget === "new" ? undefined : customRoleTarget} onClose={() => setCustomRoleTarget(null)} /> : null}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="grid gap-2"><Label>{label}</Label>{children}</div>;
}

function PermissionList({ role }: { role?: Role }) {
  return <div className="grid max-h-96 grid-cols-1 gap-px overflow-y-auto rounded-lg border bg-border sm:grid-cols-2">{role?.permissions?.length ? role.permissions.map((permission) => <div key={permission.permissionKey} className="bg-card px-3 py-3"><p className="text-sm font-medium">{permissionKeyLabel(permission.permissionKey)}</p></div>) : <p className="col-span-full bg-card px-3 py-8 text-center text-sm text-muted-foreground">此角色没有权限</p>}</div>;
}

function RoleBindingDeleteButton({ pending, onDelete }: { pending: boolean; onDelete: () => void }) {
  return <AlertDialog><AlertDialogTrigger asChild><Button size="icon-sm" variant="ghost" aria-label="撤销角色绑定" className="text-destructive hover:text-destructive" disabled={pending}><Trash2 className="h-4 w-4" /></Button></AlertDialogTrigger><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>确认撤销角色绑定</AlertDialogTitle><AlertDialogDescription>该主体会立即失去由此绑定授予的资源权限；若这是最后一名直接组织所有者，系统会拒绝撤销。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction variant="destructive" onClick={onDelete}>确认撤销</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>;
}

function roleDisplayName(role: Pick<Role, "name" | "roleKey" | "isSystem">) {
  return role.isSystem ? roleKeyLabel(role.roleKey) : role.name;
}

function roleBindingDisplayName(roleKey?: string, roleName?: string) {
  const translated = roleKeyLabel(roleKey);
  return roleKey && translated !== roleKey ? translated : roleName || translated;
}

function scopeLabel(value?: string) {
  if (value === "organization") return "组织范围";
  if (value === "workspace") return "工作区范围";
  if (value === "project") return "项目范围";
  return "未指定范围";
}

function errorMessage(cause: unknown) {
  return cause instanceof StudioApiError ? cause.message : "角色操作失败，请稍后重试。";
}
