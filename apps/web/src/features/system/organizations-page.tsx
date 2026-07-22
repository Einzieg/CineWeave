"use client";

import { useState } from "react";
import { Building2, ChevronLeft, ChevronRight, Loader2, Plus, Search, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { AppShell } from "@/components/layout/app-shell";
import { ErrorPanel } from "@/components/shared/error-panel";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useStudioSession } from "@/lib/session";
import type { CreateSystemOrganizationRequest } from "@/lib/types";

const pageSize = 25;

export function SystemOrganizationsPage() {
  const { session } = useStudioSession();
  const systemAdministrator = Boolean(session.user?.systemAdministrator);
  const [page, setPage] = useState(1);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [workspaceName, setWorkspaceName] = useState("默认工作区");
  const [ownerIdentifier, setOwnerIdentifier] = useState("");
  const invalidateKeys = useInvalidateKeys();

  const organizations = useApiQuery({
    key: qk.systemOrganizations(search, page),
    enabled: systemAdministrator,
    queryFn: (activeSession) => studioApi.listSystemOrganizations(activeSession, { search, page, pageSize }),
  });
  const createMutation = useApiMutation({
    mutationFn: (activeSession, request: CreateSystemOrganizationRequest) =>
      studioApi.createSystemOrganization(activeSession, request),
    onSuccess: (created) => {
      setCreateOpen(false);
      setName("");
      setWorkspaceName("默认工作区");
      setOwnerIdentifier("");
      setPage(1);
      toast.success(`组织“${created.organization.name}”已创建`, {
        description: `初始所有者：${created.initialOwner.username || created.initialOwner.email}`,
      });
      invalidateKeys([qk.systemOrganizationsRoot()]);
    },
  });

  const totalPages = Math.max(1, Math.ceil((organizations.data?.total ?? 0) / pageSize));

  function submitSearch() {
    setPage(1);
    setSearch(searchInput.trim());
  }

  function createOrganization() {
    const request = {
      name: name.trim(),
      workspaceName: workspaceName.trim() || "默认工作区",
      ownerIdentifier: ownerIdentifier.trim(),
    };
    if (!request.name || !request.ownerIdentifier) return;
    createMutation.mutate(request);
  }

  return (
    <AppShell active="system-organizations" title="系统组织" description="管理平台中的全部组织及其初始所有权。">
      {!systemAdministrator ? (
        <div className="grid min-h-80 place-items-center rounded-xl border bg-card px-6 text-center">
          <div>
            <ShieldAlert className="mx-auto h-9 w-9 text-muted-foreground" />
            <h2 className="mt-4 text-base font-semibold">仅系统管理员可访问</h2>
            <p className="mt-2 text-sm text-muted-foreground">当前账号没有平台级组织管理权限。</p>
          </div>
        </div>
      ) : (
        <section className="overflow-hidden rounded-xl border bg-card">
          <div className="flex flex-col gap-4 border-b px-4 py-4 md:flex-row md:items-center md:justify-between">
            <div>
              <div className="flex items-center gap-2">
                <Building2 className="h-4 w-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">组织目录</h2>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">创建组织时指定一个现有有效用户作为初始所有者。</p>
            </div>
            <Dialog
              open={createOpen}
              onOpenChange={(open) => {
                setCreateOpen(open);
                if (open) createMutation.reset();
              }}
            >
              <DialogTrigger asChild>
                <Button size="sm"><Plus className="mr-2 h-4 w-4" />创建组织</Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>创建组织</DialogTitle>
                  <DialogDescription>系统会同时创建默认工作区，并授予指定用户直接所有者角色。</DialogDescription>
                </DialogHeader>
                <div className="grid gap-4">
                  <div className="grid gap-2">
                    <Label htmlFor="system-organization-name">组织名称</Label>
                    <Input
                      id="system-organization-name"
                      value={name}
                      maxLength={100}
                      onChange={(event) => setName(event.target.value)}
                      placeholder="例如：星河动画工作室"
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="system-workspace-name">默认工作区</Label>
                    <Input
                      id="system-workspace-name"
                      value={workspaceName}
                      maxLength={100}
                      onChange={(event) => setWorkspaceName(event.target.value)}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="system-owner-identifier">初始所有者</Label>
                    <Input
                      id="system-owner-identifier"
                      value={ownerIdentifier}
                      maxLength={320}
                      onChange={(event) => setOwnerIdentifier(event.target.value)}
                      placeholder="输入现有用户的用户名或邮箱"
                    />
                    <p className="text-xs text-muted-foreground">仅匹配已存在且状态正常的账号。</p>
                  </div>
                  {createMutation.error ? <ErrorPanel message={errorMessage(createMutation.error)} /> : null}
                </div>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
                  <Button
                    disabled={createMutation.isPending || !name.trim() || !ownerIdentifier.trim()}
                    onClick={createOrganization}
                  >
                    {createMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                    创建组织
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>

          <form
            className="flex gap-2 border-b bg-muted/20 px-4 py-3"
            onSubmit={(event) => {
              event.preventDefault();
              submitSearch();
            }}
          >
            <div className="relative max-w-md flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={searchInput}
                maxLength={100}
                onChange={(event) => setSearchInput(event.target.value)}
                className="pl-9"
                aria-label="搜索系统组织"
                placeholder="搜索组织名称或标识"
              />
            </div>
            <Button type="submit" variant="outline">搜索</Button>
          </form>

          {organizations.isLoading ? (
            <div className="grid gap-2 p-4">
              {Array.from({ length: 6 }).map((_, index) => <Skeleton key={index} className="h-14" />)}
            </div>
          ) : organizations.error ? (
            <div className="p-4"><ErrorPanel message={errorMessage(organizations.error)} /></div>
          ) : organizations.data?.items.length ? (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="min-w-64">组织</TableHead>
                      <TableHead className="text-right">有效成员</TableHead>
                      <TableHead className="text-right">所有者</TableHead>
                      <TableHead className="text-right">工作区</TableHead>
                      <TableHead className="text-right">项目</TableHead>
                      <TableHead className="min-w-36 text-right">创建时间</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {organizations.data.items.map((organization) => (
                      <TableRow key={organization.id}>
                        <TableCell>
                          <div className="font-medium">{organization.name}</div>
                          <div className="mt-1 font-mono text-xs text-muted-foreground">{organization.slug}</div>
                        </TableCell>
                        <TableCell className="text-right tabular-nums">{organization.activeMemberCount}</TableCell>
                        <TableCell className="text-right tabular-nums">{organization.ownerCount}</TableCell>
                        <TableCell className="text-right tabular-nums">{organization.workspaceCount}</TableCell>
                        <TableCell className="text-right tabular-nums">{organization.projectCount}</TableCell>
                        <TableCell className="text-right text-xs text-muted-foreground">{formatDate(organization.createdAt)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
              <div className="flex items-center justify-between border-t px-4 py-3 text-xs text-muted-foreground">
                <span>共 {organizations.data.total} 个组织</span>
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
            <div className="grid place-items-center px-4 py-16 text-center">
              <Building2 className="h-8 w-8 text-muted-foreground/60" />
              <p className="mt-3 text-sm font-medium">没有匹配的组织</p>
              <p className="mt-1 text-xs text-muted-foreground">调整搜索条件，或创建新的组织。</p>
            </div>
          )}
        </section>
      )}
    </AppShell>
  );
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function errorMessage(cause: unknown) {
  return cause instanceof StudioApiError ? cause.message : "系统组织操作失败，请稍后重试。";
}
