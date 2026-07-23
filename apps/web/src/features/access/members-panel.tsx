"use client";

import { useDeferredValue, useState } from "react";
import { Check, ChevronLeft, ChevronRight, Copy, KeyRound, Loader2, Save, Search, ShieldCheck, UserRoundX } from "lucide-react";
import { toast } from "sonner";
import { ErrorPanel } from "@/components/shared/error-panel";
import { StatusBadge } from "@/components/shared/status-badge";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { roleKeyLabel, statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";
import type { OrganizationMember } from "@/lib/types";

const pageSize = 25;

export function MembersPanel({ canManage }: { canManage: boolean }) {
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<OrganizationMember | null>(null);
  const invalidateKeys = useInvalidateKeys();
  const query = useApiQuery({
    key: qk.organizationMembers(deferredSearch, status, page),
    queryFn: (session) => studioApi.listOrganizationMembers(session, session.organizationId, {
      search: deferredSearch || undefined,
      status: status === "all" ? undefined : status,
      page,
      pageSize,
    }),
  });
  const mutation = useApiMutation<OrganizationMember | { removed: boolean }, { userId: string; action: "disable" | "restore" | "remove" }>({
    mutationFn: (session, variables: { userId: string; action: "disable" | "restore" | "remove" }) => {
      if (variables.action === "remove") {
        return studioApi.removeOrganizationMember(session, session.organizationId, variables.userId);
      }
      return studioApi.updateOrganizationMemberStatus(
        session,
        session.organizationId,
        variables.userId,
        variables.action === "disable" ? "disabled" : "active",
      );
    },
    onSuccess: (_, variables) => {
      toast.success(variables.action === "remove" ? "成员已移除" : variables.action === "disable" ? "成员已停用" : "成员已恢复");
      setSelected(null);
      invalidateKeys([qk.organizationMembers(deferredSearch, status, page)]);
    },
  });

  const totalPages = Math.max(1, Math.ceil((query.data?.total ?? 0) / pageSize));

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="flex flex-col gap-3 border-b px-4 py-4 md:flex-row md:items-center md:justify-between">
        <div>
          <h2 className="text-sm font-semibold">组织成员</h2>
          <p className="mt-1 text-xs text-muted-foreground">查看成员的团队、直接角色和继承权限来源。</p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative min-w-64">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              aria-label="搜索成员"
              className="pl-9"
              placeholder="搜索姓名、用户名或邮箱"
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setPage(1);
              }}
            />
          </div>
          <Select value={status} onValueChange={(value) => { setStatus(value); setPage(1); }}>
            <SelectTrigger className="w-full sm:w-32" aria-label="成员状态">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="active">启用</SelectItem>
              <SelectItem value="disabled">已停用</SelectItem>
              <SelectItem value="removed">已移除</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {query.isLoading ? (
        <div className="grid gap-2 p-4">
          {Array.from({ length: 5 }).map((_, index) => <Skeleton key={index} className="h-14" />)}
        </div>
      ) : query.error ? (
        <div className="p-4"><ErrorPanel message={errorMessage(query.error)} /></div>
      ) : query.data?.items.length ? (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>成员</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="hidden lg:table-cell">团队</TableHead>
                <TableHead className="hidden xl:table-cell">角色</TableHead>
                <TableHead className="hidden md:table-cell">加入时间</TableHead>
                <TableHead className="w-36 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.items.map((member) => (
                <TableRow key={member.user.id}>
                  <TableCell>
                    <button type="button" className="min-w-0 text-left" onClick={() => setSelected(member)}>
                      <span className="block truncate text-sm font-medium">{member.user.displayName || member.user.username || member.user.email}</span>
                      <span className="block truncate text-xs text-muted-foreground">{member.user.username ? `@${member.user.username} · ` : ""}{member.user.email}</span>
                    </button>
                  </TableCell>
                  <TableCell><StatusBadge status={member.status} /></TableCell>
                  <TableCell className="hidden max-w-56 lg:table-cell"><Summary values={member.teams.map((team) => team.name)} empty="未加入团队" /></TableCell>
                  <TableCell className="hidden max-w-64 xl:table-cell"><Summary values={member.roles.map((role) => roleKeyLabel(role.roleKey))} empty="无有效角色" /></TableCell>
                  <TableCell className="hidden text-xs text-muted-foreground md:table-cell">{formatDate(member.createdAt)}</TableCell>
                  <TableCell className="text-right">
                    {!canManage ? (
                      <span className="text-xs text-muted-foreground">只读</span>
                    ) : member.user.systemAdministrator ? (
                      <span className="text-xs text-muted-foreground">系统账号受保护</span>
                    ) : member.status === "removed" ? (
                      <span className="text-xs text-muted-foreground">需重新邀请</span>
                    ) : (
                      <div className="flex justify-end gap-1">
                        <ConfirmAction
                          title={member.status === "active" ? "停用成员" : "恢复成员"}
                          description={member.status === "active" ? "该成员的组织会话会立即失效，现有团队和角色关系会保留。" : "恢复后，原团队与角色关系将重新生效。"}
                          confirmLabel={member.status === "active" ? "确认停用" : "确认恢复"}
                          disabled={mutation.isPending}
                          onConfirm={() => mutation.mutate({ userId: member.user.id, action: member.status === "active" ? "disable" : "restore" })}
                        >
                          <Button size="sm" variant="ghost">{member.status === "active" ? "停用" : "恢复"}</Button>
                        </ConfirmAction>
                        <ConfirmAction
                          title="移除成员"
                          description="成员的团队、项目成员关系和直接角色将被清除。再次加入必须重新发送邀请。"
                          confirmLabel="确认移除"
                          destructive
                          disabled={mutation.isPending}
                          onConfirm={() => mutation.mutate({ userId: member.user.id, action: "remove" })}
                        >
                          <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive">移除</Button>
                        </ConfirmAction>
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <div className="flex items-center justify-between border-t px-4 py-3 text-xs text-muted-foreground">
            <span>共 {query.data.total} 名成员</span>
            <div className="flex items-center gap-2">
              <Button size="icon-sm" variant="outline" aria-label="上一页" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}><ChevronLeft className="h-4 w-4" /></Button>
              <span>{page} / {totalPages}</span>
              <Button size="icon-sm" variant="outline" aria-label="下一页" disabled={page >= totalPages} onClick={() => setPage((value) => value + 1)}><ChevronRight className="h-4 w-4" /></Button>
            </div>
          </div>
        </>
      ) : (
        <div className="grid place-items-center px-4 py-16 text-center">
          <UserRoundX className="h-8 w-8 text-muted-foreground/60" />
          <p className="mt-3 text-sm font-medium">没有匹配的成员</p>
          <p className="mt-1 text-xs text-muted-foreground">调整搜索词或状态筛选后重试。</p>
        </div>
      )}

      <MemberDetail
        key={selected?.user.id ?? "no-member"}
        member={selected}
        canManage={canManage}
        onMemberUpdated={(member) => {
          setSelected(member);
          invalidateKeys([qk.organizationMembers(deferredSearch, status, page)]);
        }}
        onOpenChange={(open) => { if (!open) setSelected(null); }}
      />
      {mutation.error ? <div className="border-t p-4"><ErrorPanel message={errorMessage(mutation.error)} /></div> : null}
    </div>
  );
}

function MemberDetail({ member, canManage, onMemberUpdated, onOpenChange }: {
  member: OrganizationMember | null;
  canManage: boolean;
  onMemberUpdated: (member: OrganizationMember) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const { session } = useStudioSession();
  const [displayName, setDisplayName] = useState(member?.user.displayName ?? "");
  const [avatarUrl, setAvatarUrl] = useState(member?.user.avatarUrl ?? "");
  const [resetToken, setResetToken] = useState("");
  const [resetExpiresAt, setResetExpiresAt] = useState("");
  const [copied, setCopied] = useState(false);
  const profileMutation = useApiMutation<OrganizationMember, { userId: string; displayName: string; avatarUrl: string }>({
    mutationFn: (apiSession, values) => studioApi.updateOrganizationMemberProfile(
      apiSession,
      apiSession.organizationId,
      values.userId,
      { displayName: values.displayName, avatarUrl: values.avatarUrl },
    ),
    onSuccess: (updated) => {
      setDisplayName(updated.user.displayName ?? "");
      setAvatarUrl(updated.user.avatarUrl ?? "");
      onMemberUpdated(updated);
      toast.success("成员资料已更新");
    },
  });
  const resetMutation = useApiMutation<{ resetToken: string; expiresAt: string }, string>({
    mutationFn: (apiSession, userId) => studioApi.issueOrganizationMemberPasswordReset(apiSession, apiSession.organizationId, userId),
    onSuccess: (reset) => {
      setResetToken(reset.resetToken);
      setResetExpiresAt(reset.expiresAt);
      setCopied(false);
      toast.success("密码重置链接已生成，旧密码和全部会话已失效");
    },
  });

  const actorIsDirectOwner = session.membership?.roles.some((role) => !role.viaTeam && isOwnerRole(role.roleKey)) ?? false;
  const targetIsDirectOwner = member?.roles.some((role) => !role.viaTeam && isOwnerRole(role.roleKey)) ?? false;
  const targetIsSelf = member?.user.id === session.user?.id;
  const ownerProtected = targetIsDirectOwner && !targetIsSelf && !actorIsDirectOwner;
  const accountManageable = Boolean(member && member.status !== "removed" && !member.user.systemAdministrator && member.accountManagementAllowed && !ownerProtected);
  const canEditAccount = canManage && accountManageable;
  const profileDirty = Boolean(member) && (
    displayName.trim() !== (member?.user.displayName ?? "") || avatarUrl.trim() !== (member?.user.avatarUrl ?? "")
  );
  const resetLink = resetToken && typeof window !== "undefined"
    ? `${window.location.origin}/reset-password#token=${encodeURIComponent(resetToken)}`
    : "";
  const managementNotice = member?.user.systemAdministrator
    ? "系统管理员账号受到平台级保护，不能通过组织成员操作修改。"
    : member?.status === "removed"
    ? "已移除成员不能继续管理账号，需重新邀请后操作。"
    : member && !member.accountManagementAllowed
      ? "该账号同时属于多个组织。为避免影响其他组织，当前组织不能修改其全局资料或密码。"
      : ownerProtected
        ? "普通组织管理员不能修改直接所有者的账号资料或密码。"
        : "";

  async function copyResetLink() {
    if (!resetLink) return;
    await navigator.clipboard.writeText(resetLink);
    setCopied(true);
    toast.success("密码重置链接已复制");
  }

  return (
    <Dialog open={Boolean(member)} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[88svh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{member?.user.displayName || member?.user.username || "成员详情"}</DialogTitle>
          <DialogDescription>维护成员资料、安全状态与权限来源。</DialogDescription>
        </DialogHeader>
        {member ? (
          <div className="grid gap-5">
            <div className="grid gap-3 border-b pb-5 text-sm sm:grid-cols-3">
              <div><p className="text-xs text-muted-foreground">用户名</p><p className="mt-1">{member.user.username ? `@${member.user.username}` : "未设置"}</p></div>
              <div className="min-w-0"><p className="text-xs text-muted-foreground">邮箱</p><p className="mt-1 truncate">{member.user.email}</p></div>
              <div><p className="text-xs text-muted-foreground">成员状态</p><p className="mt-1">{statusLabel(member.status)}</p></div>
            </div>

            <section className="grid gap-3">
              <div><h3 className="text-sm font-medium">成员资料</h3><p className="mt-1 text-xs text-muted-foreground">用户名和邮箱属于登录身份，组织管理员不能修改。</p></div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="grid gap-2"><Label htmlFor="member-display-name">显示名称</Label><Input id="member-display-name" maxLength={100} readOnly={!canEditAccount} value={displayName} onChange={(event) => setDisplayName(event.target.value)} /></div>
                <div className="grid gap-2"><Label htmlFor="member-avatar-url">头像地址</Label><Input id="member-avatar-url" type="url" maxLength={2048} readOnly={!canEditAccount} value={avatarUrl} onChange={(event) => setAvatarUrl(event.target.value)} placeholder="https://example.com/avatar.jpg" /></div>
              </div>
              {managementNotice ? <p className="rounded-md border bg-muted/30 px-3 py-2 text-xs leading-5 text-muted-foreground">{managementNotice}</p> : null}
              {canEditAccount ? <div><Button size="sm" disabled={!profileDirty || profileMutation.isPending} onClick={() => profileMutation.mutate({ userId: member.user.id, displayName: displayName.trim(), avatarUrl: avatarUrl.trim() })}>{profileMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}保存成员资料</Button></div> : null}
            </section>

            <section className="grid gap-3 border-y py-5">
              <div><h3 className="text-sm font-medium">账号安全</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">发起重置会立即清除旧密码并撤销该账号的全部会话。链接 30 分钟内有效且只能使用一次。</p></div>
              {resetLink ? (
                <div className="grid gap-2">
                  <Label htmlFor="member-password-reset-link">一次性密码重置链接</Label>
                  <div className="flex gap-2"><Input id="member-password-reset-link" readOnly value={resetLink} /><Button size="icon" variant="outline" aria-label="复制密码重置链接" onClick={copyResetLink}>{copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}</Button></div>
                  <p className="text-xs text-muted-foreground">有效期至 {formatDateTime(resetExpiresAt)}。关闭此窗口后不会再次显示。</p>
                </div>
              ) : canEditAccount ? (
                <div>
                  <ConfirmAction
                    title="重置成员密码"
                    description="确认后旧密码和全部登录会话立即失效。请安全地将一次性链接发送给成员；系统不会保存明文链接。"
                    confirmLabel="确认重置"
                    destructive
                    disabled={resetMutation.isPending}
                    onConfirm={() => resetMutation.mutate(member.user.id)}
                  >
                    <Button size="sm" variant="outline" className="text-destructive hover:text-destructive"><KeyRound className="mr-2 h-4 w-4" />重置密码</Button>
                  </ConfirmAction>
                </div>
              ) : null}
            </section>

            <AccessGroup title="直接角色" roles={member.roles.filter((role) => !role.viaTeam)} />
            <AccessGroup title="通过团队继承" roles={member.roles.filter((role) => role.viaTeam)} />
            {profileMutation.error || resetMutation.error ? <ErrorPanel message={errorMessage(profileMutation.error || resetMutation.error)} /> : null}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function isOwnerRole(value: string) {
  return value === "org_owner" || value === "organization_owner";
}

function AccessGroup({ title, roles }: { title: string; roles: OrganizationMember["roles"] }) {
  return (
    <div>
      <p className="mb-2 flex items-center gap-2 text-xs font-medium text-muted-foreground"><ShieldCheck className="h-3.5 w-3.5" />{title}</p>
      <div className="divide-y rounded-lg border">
        {roles.length ? roles.map((role) => (
          <div key={`${role.bindingId}-${role.teamId ?? "direct"}`} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
            <div className="min-w-0"><p className="truncate font-medium">{roleKeyLabel(role.roleKey)}</p><p className="truncate text-xs text-muted-foreground">{role.teamName || scopeLabel(role.resourceType)}</p></div>
            <span className="text-xs text-muted-foreground">{scopeLabel(role.resourceType)}</span>
          </div>
        )) : <p className="px-3 py-4 text-xs text-muted-foreground">无</p>}
      </div>
    </div>
  );
}

function ConfirmAction({ children, title, description, confirmLabel, destructive = false, disabled, onConfirm }: {
  children: React.ReactNode;
  title: string;
  description: string;
  confirmLabel: string;
  destructive?: boolean;
  disabled?: boolean;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>{children}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle><AlertDialogDescription>{description}</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant={destructive ? "destructive" : "default"} disabled={disabled} onClick={onConfirm}>{disabled ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}{confirmLabel}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function Summary({ values, empty }: { values: string[]; empty: string }) {
  const unique = Array.from(new Set(values));
  return <p className="truncate text-xs text-muted-foreground">{unique.length ? unique.slice(0, 2).join("、") + (unique.length > 2 ? ` 等 ${unique.length} 项` : "") : empty}</p>;
}

function scopeLabel(value: string) {
  if (value === "organization") return "组织";
  if (value === "workspace") return "工作区";
  if (value === "project") return "项目";
  return value;
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "short", day: "numeric" }).format(new Date(value));
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function errorMessage(cause: unknown) {
  return cause instanceof StudioApiError ? cause.message : "成员数据加载失败，请稍后重试。";
}
