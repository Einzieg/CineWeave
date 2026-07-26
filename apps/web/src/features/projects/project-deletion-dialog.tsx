"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import { AlertTriangle, CheckCircle2, Loader2, RefreshCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { ErrorPanel } from "@/components/shared/error-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { studioApi } from "@/lib/api-client";
import { projectDeletionStatusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { Project, ProjectDeletionRequest, ProjectDeletionRequestStatus } from "@/lib/types";

type DeleteProjectTarget = Pick<Project, "id" | "name" | "revision">;

export function ProjectDeletionDialog({
  project,
  open,
  onOpenChange,
}: {
  project: DeleteProjectTarget;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const router = useRouter();
  const [confirmation, setConfirmation] = useState("");
  const impactQuery = useApiQuery({
    key: qk.projectDeletionImpact(project.id),
    queryFn: (session) => studioApi.getProjectDeletionImpact(session, project.id),
    enabled: open,
    staleTime: 0,
  });
  const createMutation = useApiMutation({
    requiredPermission: "project.delete",
    mutationFn: (session) => {
      if (!impactQuery.data) throw new Error("删除影响尚未加载完成");
      return studioApi.createProjectDeletionRequest(
        session,
        project.id,
        {
          projectName: confirmation,
          expectedProjectRevision: impactQuery.data.projectRevision,
          impactHash: impactQuery.data.impactHash,
        },
        `project-delete-${project.id}-${crypto.randomUUID()}`,
      );
    },
    onSuccess: (request) => {
      toast.success("项目删除任务已提交");
      changeOpen(false);
      router.push(projectDeletionStatusHref(request) as Route);
    },
  });

  function changeOpen(nextOpen: boolean) {
    if (!nextOpen) {
      setConfirmation("");
      createMutation.reset();
    }
    onOpenChange(nextOpen);
  }

  const impact = impactQuery.data;
  const activeTasks = impact
    ? impact.activeWorkflowCount + impact.activeAgentTaskCount + impact.activeProviderTaskCount
    : 0;
  const confirmed = confirmation === project.name;

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-destructive">
            <Trash2 className="size-5" />
            删除项目
          </DialogTitle>
        </DialogHeader>

        {impactQuery.isLoading ? (
          <div className="space-y-3 py-2">
            <Skeleton className="h-20" />
            <Skeleton className="h-32" />
          </div>
        ) : null}

        {impact ? (
          <div className="space-y-4">
            <div className="flex items-start gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-4">
              <AlertTriangle className="mt-0.5 size-5 shrink-0 text-destructive" />
              <div className="space-y-1 text-sm">
                <p className="font-medium">删除后无法恢复</p>
                <p className="text-muted-foreground">
                  原文、脚本、商品资料、分镜、媒体文件、成片和项目内任务记录都会被删除。供应商账单不受此操作影响。
                </p>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <ImpactValue label="广告脚本" value={impact.scriptUnitCount} />
              <ImpactValue label="分镜" value={impact.storyboardShotCount} />
              <ImpactValue label="媒体文件" value={impact.mediaFileCount} />
              <ImpactValue label="成片" value={impact.finalVideoCount} />
              <ImpactValue label="运行中任务" value={activeTasks} emphasis={activeTasks > 0} />
              <ImpactValue label="存储对象" value={impact.storageObjectCount} />
              <ImpactValue label="存储空间" value={formatBytes(impact.storageByteSize)} />
              <ImpactValue label="项目修订" value={impact.projectRevision} />
            </div>

            <div className="space-y-2">
              <Label htmlFor="project-delete-confirmation">
                输入项目名称 <span className="font-semibold">{project.name}</span> 以确认
              </Label>
              <Input
                id="project-delete-confirmation"
                autoComplete="off"
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
              />
            </div>
          </div>
        ) : null}

        <ErrorPanel message={impactQuery.error?.message || createMutation.error?.message || ""} />
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => changeOpen(false)} disabled={createMutation.isPending}>
            取消
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={!impact || !confirmed || createMutation.isPending}
            onClick={() => createMutation.mutate()}
          >
            {createMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
            永久删除
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectDeletionStatusDialog({
  projectId,
  requestId,
  open,
  onOpenChange,
}: {
  projectId: string;
  requestId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const invalidate = useInvalidateKeys();
  const statusQuery = useApiQuery({
    key: qk.projectDeletionRequest(projectId, requestId),
    queryFn: (session) => studioApi.getProjectDeletionRequest(session, projectId, requestId),
    enabled: open && Boolean(projectId && requestId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && isProjectDeletionTerminal(status) ? false : 1500;
    },
  });
  const retryMutation = useApiMutation({
    requiredPermission: "project.delete",
    mutationFn: (session) => studioApi.retryProjectDeletionRequest(session, projectId, requestId),
    onSuccess: () => {
      void statusQuery.refetch();
      toast.success("已重新启动删除任务");
    },
  });
  const request = statusQuery.data;
  const terminal = request ? isProjectDeletionTerminal(request.status) : false;
  const progress = request ? deletionProgress(request) : 0;

  useEffect(() => {
    if (!request || !terminal) return;
    invalidate([qk.projects()]);
  }, [invalidate, request, terminal]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {request?.status === "completed"
              ? <CheckCircle2 className="size-5 text-emerald-600" />
              : <Loader2 className={`size-5 ${terminal ? "text-destructive" : "animate-spin text-primary"}`} />}
            项目删除任务
          </DialogTitle>
        </DialogHeader>
        {statusQuery.isLoading ? <Skeleton className="h-32" /> : null}
        {request ? (
          <div className="space-y-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="font-medium">{request.projectName}</p>
                <p className="mt-1 text-sm text-muted-foreground">任务 {request.id.slice(0, 8)}</p>
              </div>
              <Badge variant={request.status === "completed" ? "default" : request.status.startsWith("failed") ? "destructive" : "secondary"}>
                {projectDeletionStatusLabel(request.status)}
              </Badge>
            </div>
            <Progress value={progress} />
            <div className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
              <ImpactValue label="存储对象" value={request.storageObjectCount} />
              <ImpactValue label="已删除" value={request.storageDeletedCount} />
              <ImpactValue label="共享跳过" value={request.storageSkippedSharedCount} />
              <ImpactValue label="失败" value={request.storageFailedCount} emphasis={request.storageFailedCount > 0} />
            </div>
            {request.errorMessage ? <ErrorPanel message={request.errorMessage} /> : null}
          </div>
        ) : null}
        <ErrorPanel message={statusQuery.error?.message || retryMutation.error?.message || ""} />
        <DialogFooter>
          {request?.status === "failed_retryable" ? (
            <Button type="button" onClick={() => retryMutation.mutate()} disabled={retryMutation.isPending}>
              {retryMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
              重试删除
            </Button>
          ) : null}
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {terminal ? "关闭" : "后台继续"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ImpactValue({
  label,
  value,
  emphasis = false,
}: {
  label: string;
  value: string | number;
  emphasis?: boolean;
}) {
  return (
    <div className="rounded-md border px-3 py-2">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`mt-1 font-semibold ${emphasis ? "text-destructive" : ""}`}>{value}</p>
    </div>
  );
}

function projectDeletionStatusHref(request: ProjectDeletionRequest) {
  const query = new URLSearchParams({
    deletionProjectId: request.projectId,
    deletionRequestId: request.id,
  });
  return `/projects?${query.toString()}`;
}

function isProjectDeletionTerminal(status: ProjectDeletionRequestStatus) {
  return status === "completed" || status === "failed_retryable" || status === "failed_terminal";
}

function deletionProgress(request: ProjectDeletionRequest) {
  if (request.status === "completed") return 100;
  if (request.status === "deleting_business_data") return 92;
  if (request.status === "deleting_storage") {
    if (request.storageObjectCount <= 0) return 75;
    return 55 + Math.round((request.storageDeletedCount / request.storageObjectCount) * 30);
  }
  if (request.status === "waiting_for_terminal") return 45;
  if (request.status === "cancelling_tasks") return 25;
  if (request.status.startsWith("failed")) return 100;
  return 10;
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const scaled = value / 1024 ** index;
  return `${scaled >= 10 || index === 0 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[index]}`;
}
