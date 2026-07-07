"use client";

import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Download, ExternalLink, FileArchive, FileText, Package, Video } from "lucide-react";
import { toast } from "sonner";
import { statusLabel } from "@/lib/labels";
import type { FinalVideoVersion, ProjectExport } from "@/lib/types";

export function ExportPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();

  const { data: project } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });
  const { data: finalVideos = [], isLoading: finalVideosLoading } = useApiQuery({
    key: qk.finalVideos(projectId),
    queryFn: (session) => studioApi.listFinalVideos(session, projectId).then((response) => response.items || []),
  });
  const { data: exports = [], isLoading: exportsLoading } = useApiQuery({
    key: qk.exports(projectId),
    queryFn: (session) => studioApi.listProjectExports(session, projectId).then((response) => response.items || []),
  });

  const createExportMutation = useApiMutation({
    mutationFn: (session, payload: { exportType: string; format: string }) =>
      studioApi.createProjectExport(session, projectId, {
        exportType: payload.exportType,
        format: payload.format,
        title: exportTitle(project?.name ?? "", payload.exportType),
      }),
    onSuccess: () => {
      toast.success("导出任务已创建");
      invalidate([qk.exports(projectId), qk.workflowRuns(projectId)]);
    },
    onError: (error) => toast.error("创建失败：" + error.message),
  });

  const exportDownloadMutation = useApiMutation({
    mutationFn: (session, item: ProjectExport) => studioApi.createProjectExportDownloadUrl(session, projectId, item.id, { expiresSeconds: 900 }),
    onSuccess: (response) => {
      window.open(response.url, "_blank", "noreferrer");
    },
    onError: (error) => toast.error("下载失败：" + error.message),
  });

  const finalVideoDownloadMutation = useApiMutation({
    mutationFn: (session, version: FinalVideoVersion) => studioApi.createFinalVideoDownloadUrl(session, projectId, version.id, { expiresSeconds: 900 }),
    onSuccess: (response) => {
      window.open(response.url, "_blank", "noreferrer");
    },
    onError: (error) => toast.error("下载失败：" + error.message),
  });

  return (
    <Surface>
      <SectionTitle title="导出中心" description="下载最终成片和项目导出包" />
      <div className="grid gap-5 p-4">
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => createExportMutation.mutate({ exportType: "final_video", format: "mp4" })} disabled={createExportMutation.isPending}>
            <Video className="mr-2 h-4 w-4" />
            导出成片
          </Button>
          <Button variant="outline" onClick={() => createExportMutation.mutate({ exportType: "documents", format: "markdown" })} disabled={createExportMutation.isPending}>
            <FileText className="mr-2 h-4 w-4" />
            导出文档
          </Button>
          <Button variant="outline" onClick={() => createExportMutation.mutate({ exportType: "asset_package", format: "zip" })} disabled={createExportMutation.isPending}>
            <FileArchive className="mr-2 h-4 w-4" />
            导出资产包
          </Button>
        </div>

        <section className="grid gap-3">
          <h3 className="text-lg font-semibold">最终成片</h3>
          {finalVideosLoading && <Skeleton className="h-48" />}
          {!finalVideosLoading && finalVideos.length === 0 && (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">暂无最终成片</div>
          )}
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {finalVideos.map((version) => (
              <div key={version.id} className="overflow-hidden rounded-lg border">
                {version.previewUrl ? (
                  <video className="aspect-video w-full bg-black object-cover" controls src={version.previewUrl} />
                ) : (
                  <div className="grid aspect-video place-items-center bg-muted">
                    <Video className="h-6 w-6 text-muted-foreground" />
                  </div>
                )}
                <div className="grid gap-3 p-3">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <div className="font-medium">{version.title}</div>
                      <div className="text-xs text-muted-foreground">v{version.version} · {version.resolution}</div>
                    </div>
                    <Badge variant="outline">{statusLabel(version.status)}</Badge>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button size="sm" onClick={() => finalVideoDownloadMutation.mutate(version)} disabled={finalVideoDownloadMutation.isPending || !version.storageKey}>
                      <Download className="mr-1 h-3.5 w-3.5" />
                      下载
                    </Button>
                    {version.previewUrl ? (
                      <a className="inline-flex h-8 items-center gap-1 rounded-lg border px-2.5 text-sm" href={version.previewUrl} rel="noreferrer" target="_blank">
                        打开
                        <ExternalLink className="h-3.5 w-3.5" />
                      </a>
                    ) : null}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </section>

        <section className="grid gap-3">
          <h3 className="text-lg font-semibold">导出记录</h3>
          {exportsLoading && <Skeleton className="h-48" />}
          {!exportsLoading && exports.length === 0 && (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">暂无导出记录</div>
          )}
          <div className="grid gap-3">
            {exports.map((item) => (
              <div key={item.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4">
                <div className="flex min-w-0 items-start gap-3">
                  <Package className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <div className="font-medium">{item.title}</div>
                    <div className="truncate text-sm text-muted-foreground">
                      {exportTypeLabel(item.exportType)} · {item.format} · {item.storageKey || item.id}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant="outline">{statusLabel(item.status)}</Badge>
                  <Button size="sm" onClick={() => exportDownloadMutation.mutate(item)} disabled={exportDownloadMutation.isPending || item.status !== "succeeded"}>
                    <Download className="mr-1 h-3.5 w-3.5" />
                    下载
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </section>
      </div>
    </Surface>
  );
}

function exportTitle(projectName: string, exportType: string) {
  const name = projectName.trim() || "CineWeave Project";
  switch (exportType) {
    case "final_video":
      return `${name} 最终成片`;
    case "documents":
      return `${name} 文档`;
    case "asset_package":
      return `${name} 资产包`;
    default:
      return `${name} 导出`;
  }
}

function exportTypeLabel(value: string) {
  switch (value) {
    case "final_video":
      return "最终成片";
    case "documents":
      return "文档";
    case "asset_package":
      return "资产包";
    case "project_archive":
      return "项目归档";
    default:
      return value;
  }
}
