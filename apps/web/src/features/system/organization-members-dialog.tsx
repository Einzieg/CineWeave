"use client";

import { useDeferredValue, useState } from "react";
import { ChevronLeft, ChevronRight, Loader2, Pencil, Plus, Search, ShieldCheck, UserRoundX } from "lucide-react";
import { toast } from "sonner";
import { ErrorPanel } from "@/components/shared/error-panel";
import { StatusBadge } from "@/components/shared/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { CodexControlKeySecretDialog } from "@/features/settings/codex-control-key-secret-dialog";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { roleKeyLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type {
  CreateSystemOrganizationMemberRequest,
  OrganizationMember,
  SystemOrganization,
  UpdateSystemOrganizationMemberRequest,
} from "@/lib/types";

const pageSize = 25;

export function SystemOrganizationMembersDialog({
  organization,
  onOpenChange,
}: {
  organization: SystemOrganization | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const [status, setStatus] = useState("all");
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<OrganizationMember | null>(null);
  const [createdMember, setCreatedMember] = useState<OrganizationMember | null>(null);
  const invalidateKeys = useInvalidateKeys();
  const organizationId = organization?.id ?? "";

  const members = useApiQuery({
    key: qk.systemOrganizationMembers(organizationId, deferredSearch, status, page),
    enabled: Boolean(organizationId),
    queryFn: (session) => studioApi.listSystemOrganizationMembers(session, organizationId, {
      search: deferredSearch || undefined,
      status: status === "all" ? undefined : status,
      page,
      pageSize,
    }),
  });
  const createMutation = useApiMutation<OrganizationMember, CreateSystemOrganizationMemberRequest>({
    mutationFn: (session, request) => studioApi.createSystemOrganizationMember(session, organizationId, request),
    onSuccess: (member) => {
      setCreateOpen(false);
      setCreatedMember(member.codexControlKey ? member : null);
      setPage(1);
      toast.success(member.codexControlKey ? "新账号已创建并加入组织" : "已有账号已加入组织");
      invalidateKeys([
        qk.systemOrganizationMembers(organizationId),
        qk.systemOrganizationsRoot(),
      ]);
    },
  });
  const updateMutation = useApiMutation<
    OrganizationMember,
    { userId: string; request: UpdateSystemOrganizationMemberRequest }
  >({
    mutationFn: (session, variables) =>
      studioApi.updateSystemOrganizationMember(session, organizationId, variables.userId, variables.request),
    onSuccess: () => {
      setEditing(null);
      toast.success("成员已更新");
      invalidateKeys([
        qk.systemOrganizationMembers(organizationId),
        qk.systemOrganizationsRoot(),
      ]);
    },
  });

  const totalPages = Math.max(1, Math.ceil((members.data?.total ?? 0) / pageSize));

  return (
    <>
      <Dialog open={Boolean(organization)} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>{organization?.name || "组织"} · 成员管理</DialogTitle>
            <DialogDescription>系统管理员可以直接创建账号或添加已有账号，无需发送邀请。</DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                aria-label="搜索系统组织成员"
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
              <SelectTrigger className="w-full sm:w-32" aria-label="系统组织成员状态">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="active">启用</SelectItem>
                <SelectItem value="disabled">已停用</SelectItem>
                <SelectItem value="removed">已移除</SelectItem>
              </SelectContent>
            </Select>
            <Button size="sm" onClick={() => { createMutation.reset(); setCreateOpen(true); }}>
              <Plus className="mr-2 h-4 w-4" />
              新增成员
            </Button>
          </div>

          <div className="overflow-hidden rounded-lg border">
            {members.isLoading ? (
              <div className="grid gap-2 p-4">
                {Array.from({ length: 5 }).map((_, index) => <Skeleton className="h-14" key={index} />)}
              </div>
            ) : members.error ? (
              <div className="p-4"><ErrorPanel message={errorMessage(members.error)} /></div>
            ) : members.data?.items.length ? (
              <>
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="min-w-64">成员</TableHead>
                        <TableHead>状态</TableHead>
                        <TableHead className="hidden md:table-cell">角色</TableHead>
                        <TableHead className="hidden lg:table-cell">加入时间</TableHead>
                        <TableHead className="w-24 text-right">操作</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {members.data.items.map((member) => (
                        <TableRow key={member.user.id}>
                          <TableCell>
                            <div className="flex min-w-0 items-center gap-2">
                              <div className="min-w-0">
                                <div className="truncate text-sm font-medium">
                                  {member.user.displayName || member.user.username || member.user.email}
                                </div>
                                <div className="truncate text-xs text-muted-foreground">
                                  {member.user.username ? `@${member.user.username} · ` : ""}{member.user.email}
                                </div>
                              </div>
                              {member.user.systemAdministrator ? <Badge variant="outline">系统管理员</Badge> : null}
                            </div>
                          </TableCell>
                          <TableCell><StatusBadge status={member.status} /></TableCell>
                          <TableCell className="hidden text-xs text-muted-foreground md:table-cell">
                            {member.roles.length ? member.roles.slice(0, 2).map((role) => roleKeyLabel(role.roleKey)).join("、") : "无有效角色"}
                          </TableCell>
                          <TableCell className="hidden text-xs text-muted-foreground lg:table-cell">
                            {formatDate(member.createdAt)}
                          </TableCell>
                          <TableCell className="text-right">
                            {member.status === "removed" ? (
                              <span className="text-xs text-muted-foreground">可重新添加</span>
                            ) : member.user.systemAdministrator ? (
                              <span className="text-xs text-muted-foreground">受保护</span>
                            ) : (
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => {
                                  updateMutation.reset();
                                  setEditing(member);
                                }}
                              >
                                <Pencil className="mr-1.5 h-3.5 w-3.5" />
                                编辑
                              </Button>
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
                <div className="flex items-center justify-between border-t px-4 py-3 text-xs text-muted-foreground">
                  <span>共 {members.data.total} 名成员</span>
                  <div className="flex items-center gap-2">
                    <Button
                      size="icon-sm"
                      variant="outline"
                      aria-label="上一页"
                      disabled={page <= 1}
                      onClick={() => setPage((value) => value - 1)}
                    >
                      <ChevronLeft className="h-4 w-4" />
                    </Button>
                    <span>{page} / {totalPages}</span>
                    <Button
                      size="icon-sm"
                      variant="outline"
                      aria-label="下一页"
                      disabled={page >= totalPages}
                      onClick={() => setPage((value) => value + 1)}
                    >
                      <ChevronRight className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </>
            ) : (
              <div className="grid place-items-center px-4 py-14 text-center">
                <UserRoundX className="h-8 w-8 text-muted-foreground/60" />
                <p className="mt-3 text-sm font-medium">没有匹配的成员</p>
                <p className="mt-1 text-xs text-muted-foreground">可以直接新增账号，或添加已有账号。</p>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>

      <CreateMemberDialog
        key={createOpen ? "create-open" : "create-closed"}
        open={createOpen}
        pending={createMutation.isPending}
        error={createMutation.error}
        onOpenChange={setCreateOpen}
        onSubmit={(request) => createMutation.mutate(request)}
      />
      <EditMemberDialog
        key={editing?.user.id ?? "no-member"}
        member={editing}
        pending={updateMutation.isPending}
        error={updateMutation.error}
        onOpenChange={(open) => { if (!open) setEditing(null); }}
        onSubmit={(request) => {
          if (editing) {
            updateMutation.mutate({ userId: editing.user.id, request });
          }
        }}
      />
      <CodexControlKeySecretDialog
        secret={createdMember?.codexControlKey}
        title="保存新成员的 Codex 项目控制密钥"
        onClose={() => setCreatedMember(null)}
      />
    </>
  );
}

function CreateMemberDialog({
  open,
  pending,
  error,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  pending: boolean;
  error: Error | null;
  onOpenChange: (open: boolean) => void;
  onSubmit: (request: CreateSystemOrganizationMemberRequest) => void;
}) {
  const [mode, setMode] = useState<"new" | "existing">("new");
  const [existingIdentifier, setExistingIdentifier] = useState("");
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [avatarUrl, setAvatarUrl] = useState("");
  const canSubmit = mode === "existing"
    ? Boolean(existingIdentifier.trim())
    : Boolean(email.trim() && username.trim() && password.length >= 8);

  function submit() {
    if (!canSubmit) return;
    if (mode === "existing") {
      onSubmit({ existingUserIdentifier: existingIdentifier.trim() });
      return;
    }
    onSubmit({
      email: email.trim(),
      username: username.trim(),
      password,
      displayName: displayName.trim() || undefined,
      avatarUrl: avatarUrl.trim() || undefined,
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>新增成员</DialogTitle>
          <DialogDescription>新账号会立即加入组织并获得基础成员角色，无需邀请确认。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="system-member-create-mode">新增方式</Label>
            <Select value={mode} onValueChange={(value: "new" | "existing") => setMode(value)}>
              <SelectTrigger id="system-member-create-mode"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="new">创建新账号</SelectItem>
                <SelectItem value="existing">添加已有账号</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {mode === "existing" ? (
            <div className="grid gap-2">
              <Label htmlFor="system-member-existing-identifier">用户名或邮箱</Label>
              <Input
                id="system-member-existing-identifier"
                value={existingIdentifier}
                maxLength={320}
                onChange={(event) => setExistingIdentifier(event.target.value)}
                placeholder="输入精确用户名或邮箱"
              />
            </div>
          ) : (
            <>
              <div className="grid gap-2 sm:grid-cols-2">
                <div className="grid gap-2">
                  <Label htmlFor="system-member-email">邮箱</Label>
                  <Input id="system-member-email" type="email" maxLength={320} value={email} onChange={(event) => setEmail(event.target.value)} />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="system-member-username">用户名</Label>
                  <Input id="system-member-username" maxLength={32} value={username} onChange={(event) => setUsername(event.target.value)} />
                </div>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="system-member-password">初始密码</Label>
                <Input id="system-member-password" type="password" minLength={8} maxLength={72} value={password} onChange={(event) => setPassword(event.target.value)} />
                <p className="text-xs text-muted-foreground">使用 8 至 72 个字符，并通过安全渠道交给成员。</p>
              </div>
              <div className="grid gap-2 sm:grid-cols-2">
                <div className="grid gap-2">
                  <Label htmlFor="system-member-display-name">显示名称</Label>
                  <Input id="system-member-display-name" maxLength={100} value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="system-member-avatar-url">头像地址</Label>
                  <Input id="system-member-avatar-url" type="url" maxLength={2048} value={avatarUrl} onChange={(event) => setAvatarUrl(event.target.value)} />
                </div>
              </div>
            </>
          )}
          {error ? <ErrorPanel message={errorMessage(error)} /> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button disabled={pending || !canSubmit} onClick={submit}>
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
            直接新增
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function EditMemberDialog({
  member,
  pending,
  error,
  onOpenChange,
  onSubmit,
}: {
  member: OrganizationMember | null;
  pending: boolean;
  error: Error | null;
  onOpenChange: (open: boolean) => void;
  onSubmit: (request: UpdateSystemOrganizationMemberRequest) => void;
}) {
  const [email, setEmail] = useState(member?.user.email ?? "");
  const [username, setUsername] = useState(member?.user.username ?? "");
  const [displayName, setDisplayName] = useState(member?.user.displayName ?? "");
  const [avatarUrl, setAvatarUrl] = useState(member?.user.avatarUrl ?? "");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState<"active" | "disabled">(
    member?.status === "disabled" ? "disabled" : "active",
  );

  function submit() {
    if (!member || !email.trim() || !username.trim()) return;
    onSubmit({
      email: email.trim(),
      username: username.trim(),
      displayName: displayName.trim(),
      avatarUrl: avatarUrl.trim(),
      password: password || undefined,
      status,
    });
  }

  return (
    <Dialog open={Boolean(member)} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>编辑成员</DialogTitle>
          <DialogDescription>用户名和邮箱是全局登录身份，修改会影响该账号加入的所有组织。</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="system-member-edit-email">邮箱</Label>
              <Input id="system-member-edit-email" type="email" maxLength={320} value={email} onChange={(event) => setEmail(event.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="system-member-edit-username">用户名</Label>
              <Input id="system-member-edit-username" maxLength={32} value={username} onChange={(event) => setUsername(event.target.value)} />
            </div>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="system-member-edit-display-name">显示名称</Label>
              <Input id="system-member-edit-display-name" maxLength={100} value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="system-member-edit-status">成员状态</Label>
              <Select value={status} onValueChange={(value: "active" | "disabled") => setStatus(value)}>
                <SelectTrigger id="system-member-edit-status"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="active">启用</SelectItem>
                  <SelectItem value="disabled">停用</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="system-member-edit-avatar-url">头像地址</Label>
            <Input id="system-member-edit-avatar-url" type="url" maxLength={2048} value={avatarUrl} onChange={(event) => setAvatarUrl(event.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="system-member-edit-password">新密码</Label>
            <Input id="system-member-edit-password" type="password" minLength={8} maxLength={72} value={password} onChange={(event) => setPassword(event.target.value)} placeholder="留空则不修改" />
            <p className="text-xs text-muted-foreground">设置新密码会立即撤销该账号的全部登录会话。</p>
          </div>
          <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
            <ShieldCheck className="h-4 w-4 shrink-0" />
            系统管理员账号不能在这里修改；停用最后一个有效组织所有者也会被拒绝。
          </div>
          {error ? <ErrorPanel message={errorMessage(error)} /> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
          <Button disabled={pending || !email.trim() || !username.trim() || Boolean(password && password.length < 8)} onClick={submit}>
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            保存修改
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function errorMessage(cause: unknown) {
  return cause instanceof StudioApiError ? cause.message : "系统成员操作失败，请稍后重试。";
}
