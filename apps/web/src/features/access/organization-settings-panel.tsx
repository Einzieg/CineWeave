"use client";

import { useState } from "react";
import type { Route } from "next";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { Building2, Loader2, LogOut, RefreshCw } from "lucide-react";
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
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";
import type { AuthResponse, Organization } from "@/lib/types";

export function OrganizationSettingsPanel({ canUpdate }: { canUpdate: boolean }) {
  const { session } = useStudioSession();
  const organizations = useApiQuery({
    key: qk.organizations(),
    queryFn: (apiSession) => studioApi.listOrganizations(apiSession).then((response) => response.items),
  });
  const current = organizations.data?.find((item) => item.id === session.organizationId);

  if (organizations.isLoading) {
    return <div className="grid gap-3 rounded-xl border bg-card p-4"><Skeleton className="h-32" /><Skeleton className="h-32" /></div>;
  }
  if (organizations.error) {
    return <div className="rounded-xl border bg-card p-4"><ErrorPanel message={errorMessage(organizations.error)} /></div>;
  }
  if (!current) {
    return <div className="rounded-xl border bg-card p-8 text-center text-sm text-muted-foreground">当前组织不可用，请重新登录。</div>;
  }
  return <OrganizationSettingsForm key={current.id} current={current} organizations={organizations.data ?? []} canUpdate={canUpdate} />;
}

function OrganizationSettingsForm({ current, organizations, canUpdate }: { current: Organization; organizations: Organization[]; canUpdate: boolean }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { setSession, clearSession } = useStudioSession();
  const invalidateKeys = useInvalidateKeys();
  const [name, setName] = useState(current.name);
  const [targetOrganizationId, setTargetOrganizationId] = useState(current.id);

  const updateMutation = useApiMutation({
    mutationFn: (session, nextName: string) => studioApi.updateOrganization(session, current.id, nextName),
    onSuccess: (updated) => {
      setName(updated.name);
      toast.success("组织名称已更新");
      invalidateKeys([qk.organizations()]);
    },
  });
  const switchMutation = useApiMutation<AuthResponse, string>({
    mutationFn: (session, organizationId) => studioApi.switchOrganization(session, organizationId),
    onSuccess: (response) => {
      queryClient.clear();
      setSession(sessionFromAuthResponse(response));
      toast.success("已切换组织");
      router.replace("/projects" as Route);
    },
  });
  const leaveMutation = useApiMutation({
    mutationFn: (session) => studioApi.leaveOrganization(session, current.id),
    onSuccess: () => {
      queryClient.clear();
      clearSession();
      toast.success("已退出组织");
      router.replace("/login" as Route);
    },
  });
  const error = updateMutation.error || switchMutation.error || leaveMutation.error;

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(20rem,0.72fr)]">
      <section className="overflow-hidden rounded-xl border bg-card">
        <div className="border-b px-4 py-4">
          <div className="flex items-center gap-2"><Building2 className="h-4 w-4 text-muted-foreground" /><h2 className="text-sm font-semibold">组织资料</h2></div>
          <p className="mt-1 text-xs text-muted-foreground">名称会显示在团队工作区中；组织标识首期保持不变。</p>
        </div>
        <div className="grid gap-5 p-4">
          <div className="grid gap-2">
            <Label htmlFor="organization-name">组织名称</Label>
            <Input id="organization-name" maxLength={100} value={name} readOnly={!canUpdate} className={!canUpdate ? "bg-muted/40 text-muted-foreground" : undefined} onChange={(event) => setName(event.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="organization-slug">组织标识</Label>
            <Input id="organization-slug" value={current.slug ?? ""} readOnly className="bg-muted/40 text-muted-foreground" />
          </div>
          {canUpdate ? <div>
            <Button disabled={!name.trim() || name.trim() === current.name || updateMutation.isPending} onClick={() => updateMutation.mutate(name.trim())}>
              {updateMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}保存组织资料
            </Button>
          </div> : null}
        </div>
      </section>

      <div className="grid gap-4">
        <section className="overflow-hidden rounded-xl border bg-card">
          <div className="border-b px-4 py-4"><h2 className="text-sm font-semibold">切换组织</h2><p className="mt-1 text-xs text-muted-foreground">切换后将重新签发会话并清空上一组织的页面缓存。</p></div>
          <div className="grid gap-3 p-4">
            <Select value={targetOrganizationId} onValueChange={setTargetOrganizationId}>
              <SelectTrigger aria-label="选择组织"><SelectValue /></SelectTrigger>
              <SelectContent>{organizations.map((organization) => <SelectItem key={organization.id} value={organization.id}>{organization.name}</SelectItem>)}</SelectContent>
            </Select>
            <Button variant="outline" disabled={targetOrganizationId === current.id || switchMutation.isPending} onClick={() => switchMutation.mutate(targetOrganizationId)}>
              {switchMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RefreshCw className="mr-2 h-4 w-4" />}切换到所选组织
            </Button>
          </div>
        </section>

        <section className="overflow-hidden rounded-xl border border-destructive/30 bg-card">
          <div className="border-b border-destructive/20 px-4 py-4"><h2 className="text-sm font-semibold text-destructive">退出组织</h2><p className="mt-1 text-xs text-muted-foreground">退出会清除你在该组织中的团队、项目成员关系和直接角色，不能直接恢复。</p></div>
          <div className="p-4">
            <AlertDialog>
              <AlertDialogTrigger asChild><Button variant="destructive"><LogOut className="mr-2 h-4 w-4" />退出当前组织</Button></AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader><AlertDialogTitle>确认退出“{current.name}”</AlertDialogTitle><AlertDialogDescription>该操作会立即撤销当前组织会话。若你是最后一名直接所有者，系统会拒绝退出。</AlertDialogDescription></AlertDialogHeader>
                <AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction variant="destructive" disabled={leaveMutation.isPending} onClick={() => leaveMutation.mutate()}>确认退出</AlertDialogAction></AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        </section>
      </div>
      {error ? <div className="lg:col-span-2"><ErrorPanel message={errorMessage(error)} /></div> : null}
    </div>
  );
}

function errorMessage(error: unknown) {
  return error instanceof StudioApiError ? error.message : error instanceof Error ? error.message : "操作失败，请稍后重试";
}
