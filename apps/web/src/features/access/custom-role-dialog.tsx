"use client";

import { useMemo, useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ErrorPanel } from "@/components/shared/error-panel";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { permissionKeyLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { JsonRecord, Permission, Role, RoleImpact } from "@/lib/types";

export function CustomRoleDialog({ role, onClose }: { role?: Role; onClose: () => void }) {
  const detail = useApiQuery({
    key: qk.role(role?.id ?? "new"),
    enabled: Boolean(role),
    queryFn: (session) => studioApi.getRole(session, role!.id),
  });
  const impact = useApiQuery({
    key: qk.roleImpact(role?.id ?? "new"),
    enabled: Boolean(role),
    queryFn: (session) => studioApi.getRoleImpact(session, role!.id),
  });
  const permissions = useApiQuery({
    key: qk.permissions(),
    queryFn: (session) => studioApi.listPermissions(session).then((response) => response.items),
  });
  const loading = Boolean(role) && (detail.isLoading || impact.isLoading) || permissions.isLoading;
  const error = detail.error || impact.error || permissions.error;

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{role ? "编辑自定义角色" : "创建自定义角色"}</DialogTitle>
          <DialogDescription>按作用范围选择可分配权限；系统角色与通配管理权限始终保持只读。</DialogDescription>
        </DialogHeader>
        {loading ? <div className="grid gap-3"><Skeleton className="h-10" /><Skeleton className="h-10" /><Skeleton className="h-48" /></div> : error ? <ErrorPanel message={errorMessage(error)} /> : (
          <CustomRoleForm key={role?.id ?? "new"} role={detail.data} impact={impact.data} permissions={permissions.data ?? []} onClose={onClose} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function CustomRoleForm({ role, impact, permissions, onClose }: { role?: Role; impact?: RoleImpact; permissions: Permission[]; onClose: () => void }) {
  const invalidateKeys = useInvalidateKeys();
  const [roleKey, setRoleKey] = useState(role?.roleKey ?? "");
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [scope, setScope] = useState<Role["scope"]>(role?.scope ?? "project");
  const [permissionKeys, setPermissionKeys] = useState(() => role?.permissions?.map((permission) => permission.permissionKey) ?? []);
  const [confirmUpdate, setConfirmUpdate] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const availablePermissions = useMemo(
    () => permissions.filter((permission) => permissionAllowedForScope(scope, permission.permissionKey)),
    [permissions, scope],
  );
  const saveMutation = useApiMutation({
    mutationFn: (session, body: JsonRecord) => role ? studioApi.updateCustomRole(session, role.id, body) : studioApi.createCustomRole(session, body),
    onSuccess: (saved) => {
      toast.success(role ? "自定义角色已更新" : "自定义角色已创建");
      invalidateKeys([qk.roles(), qk.role(saved.id), qk.roleImpact(saved.id), qk.roleBindings(), qk.organizationAuditLogs()]);
      onClose();
    },
  });
  const deleteMutation = useApiMutation({
    mutationFn: (session) => studioApi.deleteCustomRole(session, role!.id),
    onSuccess: () => {
      toast.success("自定义角色已删除");
      invalidateKeys([qk.roles(), qk.roleBindings(), qk.organizationAuditLogs()]);
      onClose();
    },
  });

  function changeScope(value: Role["scope"]) {
    setScope(value);
    setPermissionKeys((current) => current.filter((key) => permissionAllowedForScope(value, key)));
  }

  function togglePermission(key: string, checked: boolean) {
    setPermissionKeys((current) => checked ? [...new Set([...current, key])].sort() : current.filter((value) => value !== key));
  }

  function submit() {
    const body: JsonRecord = role ? {
      name: name.trim(),
      description: description.trim(),
      scope,
      permissionKeys,
    } : {
      roleKey: roleKey.trim().toLowerCase(),
      name: name.trim(),
      description: description.trim(),
      scope,
      permissionKeys,
    };
    saveMutation.mutate(body);
  }

  const roleKeyValid = role ? true : /^[a-z][a-z0-9_]{2,63}$/.test(roleKey.trim().toLowerCase());
  const canSave = roleKeyValid && Boolean(name.trim()) && !saveMutation.isPending;
  const error = saveMutation.error || deleteMutation.error;

  return (
    <>
      <div className="grid max-h-[65vh] gap-5 overflow-y-auto pr-1">
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-2"><Label htmlFor="custom-role-name">角色名称</Label><Input id="custom-role-name" maxLength={100} value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：分镜审核员" /></div>
          <div className="grid gap-2"><Label htmlFor="custom-role-key">角色标识</Label><Input id="custom-role-key" maxLength={64} value={roleKey} readOnly={Boolean(role)} className={role ? "bg-muted/40 text-muted-foreground" : ""} onChange={(event) => setRoleKey(event.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ""))} placeholder="storyboard_reviewer" /><p className="text-[11px] text-muted-foreground">以小写字母开头，仅使用小写字母、数字和下划线。</p></div>
        </div>
        <div className="grid gap-2"><Label htmlFor="custom-role-description">角色说明</Label><Textarea id="custom-role-description" maxLength={500} value={description} onChange={(event) => setDescription(event.target.value)} placeholder="说明该角色的职责边界" /></div>
        <div className="grid gap-2"><Label>作用范围</Label><Select value={scope} onValueChange={(value: Role["scope"]) => changeScope(value)} disabled={Boolean(impact?.bindingCount)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="organization">组织范围</SelectItem><SelectItem value="workspace">工作区范围</SelectItem><SelectItem value="project">项目范围</SelectItem></SelectContent></Select>{impact?.bindingCount ? <p className="text-[11px] text-muted-foreground">角色已有绑定，需先撤销绑定才能修改作用范围。</p> : null}</div>
        {role && impact ? <div className="rounded-lg border bg-muted/30 px-3 py-3 text-xs text-muted-foreground"><p className="font-medium text-foreground">变更影响</p><p className="mt-1">当前 {impact.bindingCount} 项绑定，涉及 {impact.affectedUserCount} 名有效成员；组织 / 工作区 / 项目绑定分别为 {impact.organizationBindings} / {impact.workspaceBindings} / {impact.projectBindings} 项。</p></div> : null}
        <div className="grid gap-2">
          <div className="flex items-center justify-between"><Label>权限</Label><span className="text-xs text-muted-foreground">已选 {permissionKeys.length} 项</span></div>
          <div className="grid max-h-64 grid-cols-1 gap-px overflow-y-auto rounded-lg border bg-border sm:grid-cols-2">
            {availablePermissions.map((permission) => {
              const checked = permissionKeys.includes(permission.permissionKey);
              return <label key={permission.permissionKey} className="flex cursor-pointer items-center gap-3 bg-card px-3 py-3 text-sm hover:bg-muted/40"><Checkbox checked={checked} onCheckedChange={(value) => togglePermission(permission.permissionKey, value === true)} /><span>{permissionKeyLabel(permission.permissionKey)}</span></label>;
            })}
            {!availablePermissions.length ? <p className="col-span-full bg-card px-3 py-8 text-center text-sm text-muted-foreground">当前范围没有可分配权限</p> : null}
          </div>
        </div>
        {error ? <ErrorPanel message={errorMessage(error)} /> : null}
      </div>
      <DialogFooter className="items-center sm:justify-between">
        <div>{role ? <Button variant="ghost" className="text-destructive hover:text-destructive" disabled={Boolean(impact?.bindingCount) || deleteMutation.isPending} onClick={() => setConfirmDelete(true)}><Trash2 className="mr-2 h-4 w-4" />删除角色</Button> : null}</div>
        <div className="flex gap-2"><Button variant="outline" onClick={onClose}>取消</Button><Button disabled={!canSave} onClick={() => impact?.bindingCount ? setConfirmUpdate(true) : submit()}>{saveMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}{role ? "保存角色" : "创建角色"}</Button></div>
      </DialogFooter>

      <AlertDialog open={confirmUpdate} onOpenChange={setConfirmUpdate}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>确认更新角色权限</AlertDialogTitle><AlertDialogDescription>保存后会立即影响 {impact?.affectedUserCount ?? 0} 名成员通过该角色获得的权限。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction onClick={submit}>确认保存</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>确认删除自定义角色</AlertDialogTitle><AlertDialogDescription>角色删除后无法恢复。系统仅允许删除没有任何绑定的自定义角色。</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction variant="destructive" onClick={() => deleteMutation.mutate()}>确认删除</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function permissionAllowedForScope(scope: Role["scope"], key: string) {
  if (key === "admin.manage") return false;
  if (scope === "organization") return true;
  const workspace = ["workspace.read", "workspace.manage", "project.", "source.", "novel_event.", "adaptation_plan.", "script.", "asset.", "storyboard.", "artifact.", "media.", "workflow."];
  const project = ["project.read", "project.write", "project.update", "project.delete", "project.members.manage", "project.video_production.", "source.", "novel_event.", "adaptation_plan.", "script.", "asset.", "storyboard.", "artifact.", "media.", "workflow."];
  return (scope === "workspace" ? workspace : project).some((value) => value.endsWith(".") ? key.startsWith(value) : key === value);
}

function errorMessage(error: unknown) {
  return error instanceof StudioApiError ? error.message : error instanceof Error ? error.message : "自定义角色操作失败";
}
