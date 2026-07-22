"use client";

import { useMemo, useState } from "react";
import { Check, ChevronLeft, ChevronRight, Copy, Loader2, MailPlus } from "lucide-react";
import { toast } from "sonner";
import { ErrorPanel } from "@/components/shared/error-panel";
import { StatusBadge } from "@/components/shared/status-badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { roleKeyLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { OrganizationInvitation } from "@/lib/types";

export function InvitationsPanel({ canManage }: { canManage: boolean }) {
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("7");
  const [baseRoleId, setBaseRoleId] = useState("");
  const [projectId, setProjectId] = useState("");
  const [projectRoleId, setProjectRoleId] = useState("");
  const [createdInvitation, setCreatedInvitation] = useState<OrganizationInvitation | null>(null);
  const [copied, setCopied] = useState(false);
  const invalidateKeys = useInvalidateKeys();
  const invitations = useApiQuery({
    key: [...qk.organizationInvitations(), page],
    queryFn: (session) => studioApi.listOrganizationInvitations(session, session.organizationId, page, 25),
  });
  const roles = useApiQuery({
    key: qk.roles(),
    queryFn: (session) => studioApi.listRoles(session).then((response) => response.items),
  });
  const projects = useApiQuery({
    key: qk.projects(),
    enabled: canManage,
    queryFn: (session) => studioApi.listProjects(session).then((response) => response.items),
  });
  const allowedRoles = useMemo(() => roles.data?.filter((role) => role.roleKey === "org_member" || role.roleKey === "organization_member") ?? [], [roles.data]);
  const projectRoles = useMemo(() => roles.data?.filter((role) => role.scope === "project") ?? [], [roles.data]);
  const createMutation = useApiMutation({
    mutationFn: (session, variables: { email: string; baseRoleId: string; expiresInDays: number; bindings: Array<{ roleId: string; resourceType: "project"; projectId: string }> }) =>
      studioApi.createOrganizationInvitation(session, session.organizationId, variables),
    onSuccess: (created) => {
      setCreateOpen(false);
      setCreatedInvitation(created);
      setEmail("");
      setProjectId("");
      setProjectRoleId("");
      setCopied(false);
      setPage(1);
      toast.success("邀请已创建");
      invalidateKeys([qk.organizationInvitations()]);
    },
  });
  const revokeMutation = useApiMutation({
    mutationFn: (session, invitationId: string) => studioApi.revokeOrganizationInvitation(session, session.organizationId, invitationId),
    onSuccess: () => {
      toast.success("邀请已撤销");
      invalidateKeys([qk.organizationInvitations()]);
    },
  });

  function createInvitation() {
    const roleId = baseRoleId || allowedRoles[0]?.id || "";
    const initialProjectRoleId = projectRoleId || projectRoles[0]?.id || "";
    if (!email.trim() || !roleId || (projectId && !initialProjectRoleId)) return;
    createMutation.mutate({
      email: email.trim(),
      baseRoleId: roleId,
      expiresInDays: Number(expiresInDays),
      bindings: projectId ? [{ roleId: initialProjectRoleId, resourceType: "project", projectId }] : [],
    });
  }

  const invitationLink = createdInvitation?.invitationToken && typeof window !== "undefined"
    ? `${window.location.origin}/accept-invitation#token=${encodeURIComponent(createdInvitation.invitationToken)}`
    : "";
  const totalPages = Math.max(1, Math.ceil((invitations.data?.total ?? 0) / 25));

  async function copyInvitationLink() {
    if (!invitationLink) return;
    await navigator.clipboard.writeText(invitationLink);
    setCopied(true);
    toast.success("邀请链接已复制");
  }

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="flex items-center justify-between gap-4 border-b px-4 py-4">
        <div>
          <h2 className="text-sm font-semibold">成员邀请</h2>
          <p className="mt-1 text-xs text-muted-foreground">链接只在创建后显示一次，可随时撤销未使用的邀请。</p>
        </div>
        {canManage ? <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); if (open && !baseRoleId && allowedRoles[0]) setBaseRoleId(allowedRoles[0].id); }}>
          <DialogTrigger asChild><Button size="sm"><MailPlus className="mr-2 h-4 w-4" />邀请成员</Button></DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>邀请成员</DialogTitle><DialogDescription>邀请接受后将获得基础组织成员角色。</DialogDescription></DialogHeader>
            <div className="grid gap-4">
              <div className="grid gap-2"><Label htmlFor="invite-email">邮箱</Label><Input id="invite-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="name@example.com" /></div>
              <div className="grid gap-2"><Label htmlFor="invite-role">基础角色</Label><Select value={baseRoleId || allowedRoles[0]?.id || ""} onValueChange={setBaseRoleId}><SelectTrigger id="invite-role"><SelectValue placeholder="选择基础角色" /></SelectTrigger><SelectContent>{allowedRoles.map((role) => <SelectItem key={role.id} value={role.id}>{roleKeyLabel(role.roleKey)}</SelectItem>)}</SelectContent></Select></div>
              <div className="grid gap-2"><Label htmlFor="invite-expiry">有效期</Label><Select value={expiresInDays} onValueChange={setExpiresInDays}><SelectTrigger id="invite-expiry"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="1">1 天</SelectItem><SelectItem value="7">7 天</SelectItem><SelectItem value="14">14 天</SelectItem><SelectItem value="30">30 天</SelectItem></SelectContent></Select></div>
              <div className="grid gap-2"><Label htmlFor="invite-project">初始项目访问</Label><Select value={projectId || "none"} onValueChange={(value) => { setProjectId(value === "none" ? "" : value); if (value === "none") setProjectRoleId(""); }}><SelectTrigger id="invite-project"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="none">不预设项目权限</SelectItem>{projects.data?.map((project) => <SelectItem key={project.id} value={project.id}>{project.name}</SelectItem>)}</SelectContent></Select></div>
              {projectId ? <div className="grid gap-2"><Label htmlFor="invite-project-role">项目角色</Label><Select value={projectRoleId || projectRoles[0]?.id || ""} onValueChange={setProjectRoleId}><SelectTrigger id="invite-project-role"><SelectValue placeholder="选择项目角色" /></SelectTrigger><SelectContent>{projectRoles.map((role) => <SelectItem key={role.id} value={role.id}>{roleKeyLabel(role.roleKey)}</SelectItem>)}</SelectContent></Select></div> : null}
              {createMutation.error ? <ErrorPanel message={errorMessage(createMutation.error)} /> : null}
            </div>
            <DialogFooter><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button><Button disabled={createMutation.isPending || !email.trim() || allowedRoles.length === 0 || (Boolean(projectId) && projectRoles.length === 0)} onClick={createInvitation}>{createMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}创建邀请</Button></DialogFooter>
          </DialogContent>
        </Dialog> : null}
      </div>

      {invitations.isLoading ? (
        <div className="grid gap-2 p-4">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-14" />)}</div>
      ) : invitations.error ? (
        <div className="p-4"><ErrorPanel message={errorMessage(invitations.error)} /></div>
      ) : invitations.data?.items.length ? (
        <>
        <Table>
          <TableHeader><TableRow><TableHead>邮箱</TableHead><TableHead>状态</TableHead><TableHead className="hidden md:table-cell">有效期</TableHead><TableHead className="hidden lg:table-cell">创建时间</TableHead><TableHead className="w-28 text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>
            {invitations.data.items.map((invitation) => (
              <TableRow key={invitation.id}>
                <TableCell className="font-medium">{invitation.email}</TableCell>
                <TableCell><StatusBadge status={invitation.status} /></TableCell>
                <TableCell className="hidden text-xs text-muted-foreground md:table-cell">{formatDate(invitation.expiresAt)}</TableCell>
                <TableCell className="hidden text-xs text-muted-foreground lg:table-cell">{formatDate(invitation.createdAt)}</TableCell>
                <TableCell className="text-right">{canManage && invitation.status === "pending" ? <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={revokeMutation.isPending} onClick={() => revokeMutation.mutate(invitation.id)}>撤销</Button> : <span className="text-xs text-muted-foreground">—</span>}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <div className="flex items-center justify-between border-t px-4 py-3 text-xs text-muted-foreground">
          <span>共 {invitations.data.total} 条邀请</span>
          <div className="flex items-center gap-2">
            <Button size="icon-sm" variant="outline" aria-label="上一页" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}><ChevronLeft className="h-4 w-4" /></Button>
            <span>{page} / {totalPages}</span>
            <Button size="icon-sm" variant="outline" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage((value) => value + 1)}><ChevronRight className="h-4 w-4" /></Button>
          </div>
        </div>
        </>
      ) : (
        <div className="grid place-items-center px-4 py-16 text-center"><MailPlus className="h-8 w-8 text-muted-foreground/60" /><p className="mt-3 text-sm font-medium">尚未创建邀请</p><p className="mt-1 text-xs text-muted-foreground">邀请新成员或已有账号加入当前组织。</p></div>
      )}
      {revokeMutation.error ? <div className="border-t p-4"><ErrorPanel message={errorMessage(revokeMutation.error)} /></div> : null}

      <Dialog open={Boolean(createdInvitation)} onOpenChange={(open) => { if (!open) setCreatedInvitation(null); }}>
        <DialogContent>
          <DialogHeader><DialogTitle>邀请已创建</DialogTitle><DialogDescription>此链接只显示一次。关闭后如需新链接，请撤销并重新邀请。</DialogDescription></DialogHeader>
          <div className="grid gap-2"><Label htmlFor="created-invitation-link">邀请链接</Label><div className="flex gap-2"><Input id="created-invitation-link" readOnly value={invitationLink} /><Button size="icon" variant="outline" aria-label="复制邀请链接" onClick={copyInvitationLink}>{copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}</Button></div></div>
          <DialogFooter><Button onClick={() => setCreatedInvitation(null)}>完成</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function errorMessage(cause: unknown) {
  return cause instanceof StudioApiError ? cause.message : "邀请操作失败，请稍后重试。";
}
