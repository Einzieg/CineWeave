"use client";

import { useMemo, useState } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { Download, Film, Image as ImageIcon, Link2Off, RefreshCcw, Trash2, Video, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { studioApi } from "@/lib/api-client";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { FinalVideoVersion, ProjectTimeline, ShotProductionShot } from "@/lib/types";

type ShotAction = "generate_missing_images" | "generate_missing_videos" | "regenerate_failed_images" | "regenerate_failed_videos" | "cancel_running_videos";
type MediaKind = "image" | "video";

export function VideoPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const [finalVideoToDelete, setFinalVideoToDelete] = useState<FinalVideoVersion | null>(null);

  const { data: status, isLoading } = useApiQuery({
    key: qk.shotProduction(projectId),
    queryFn: (session) =>
      studioApi.getShotProductionStatus(session, projectId, {
        includePreviewUrl: true,
        previewExpiresSeconds: 900,
      }),
  });
  const { data: timelines = [], isLoading: timelinesLoading } = useApiQuery({
    key: qk.timelines(projectId),
    queryFn: (session) => studioApi.listTimelines(session, projectId).then((response) => response.items || []),
  });
  const { data: finalVideos = [], isLoading: finalVideosLoading } = useApiQuery({
    key: qk.finalVideos(projectId),
    queryFn: (session) => studioApi.listFinalVideos(session, projectId).then((response) => response.items || []),
  });

  const shots = status?.shots ?? [];
  const summary = status?.summary;
  const runningCount = summary?.running ?? 0;
  const activeTimeline = timelines.find((timeline) => timeline.status === "active") ?? timelines[0] ?? null;
  const canCompose = !!summary && summary.total > 0 && summary.videoSucceeded === summary.total && runningCount === 0;

  const actionMutation = useApiMutation({
    mutationFn: (session, action: ShotAction) =>
      studioApi.runShotProductionAction(session, projectId, {
        action,
        options: { maxConcurrency: 1 },
      }),
    onSuccess: (response) => {
      toast.success(`${response.targetShotIds.length} 个镜头任务已启动`);
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const unlinkMediaMutation = useApiMutation({
    mutationFn: (session, payload: { shotId: string; kind: MediaKind }) =>
      studioApi.unlinkStoryboardShotMedia(session, projectId, payload.shotId, payload.kind),
    onSuccess: () => {
      toast.success("媒体绑定已解除");
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("解绑失败：" + error.message),
  });

  const composeMutation = useApiMutation({
    mutationFn: async (session) => {
      let timeline: ProjectTimeline | null = activeTimeline;
      if (!timeline) {
        timeline = await studioApi.createTimeline(session, projectId, {
          title: "主时间线",
          fromStoryboardShots: true,
        });
      }
      return studioApi.composeTimeline(session, projectId, timeline.id, {
        title: timeline.title,
        resolution: timeline.resolution,
        aspectRatio: timeline.aspectRatio,
      });
    },
    onSuccess: () => {
      toast.success("成片合成任务已启动");
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("合成失败：" + error.message),
  });

  const activateFinalMutation = useApiMutation({
    mutationFn: (session, versionId: string) => studioApi.activateFinalVideo(session, projectId, versionId),
    onSuccess: () => {
      toast.success("当前成片已切换");
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("切换失败：" + error.message),
  });

  const downloadFinalMutation = useApiMutation({
    mutationFn: (session, versionId: string) => studioApi.createFinalVideoDownloadUrl(session, projectId, versionId, { expiresSeconds: 900 }),
    onSuccess: (response) => {
      window.open(response.url, "_blank", "noopener,noreferrer");
    },
    onError: (error) => toast.error("下载地址创建失败：" + error.message),
  });

  const deleteFinalMutation = useApiMutation({
    mutationFn: (session, version: FinalVideoVersion) => studioApi.deleteFinalVideo(session, projectId, version.id, version.status === "active"),
    onSuccess: () => {
      toast.success("成片版本已删除");
      setFinalVideoToDelete(null);
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("删除失败：" + error.message),
  });

  const cards = useMemo(
    () => [
      { label: "镜头", value: summary?.total ?? 0 },
      { label: "缺失图片", value: summary?.imageMissing ?? 0 },
      { label: "失败图片", value: summary?.imageFailed ?? 0 },
      { label: "缺失视频", value: summary?.videoMissing ?? 0 },
      { label: "失败视频", value: summary?.videoFailed ?? 0 },
      { label: "运行中", value: runningCount },
    ],
    [runningCount, summary],
  );

  return (
    <Surface>
      <SectionTitle title="视频" description="生成、查看和管理镜头图片、镜头视频与最终成片" />
      <div className="grid gap-5 p-4">
        <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-6">
          {cards.map((card) => (
            <div key={card.label} className="rounded-lg border p-3">
              <div className="text-xs text-muted-foreground">{card.label}</div>
              <div className="mt-1 text-2xl font-semibold">{card.value}</div>
            </div>
          ))}
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            onClick={() => actionMutation.mutate("generate_missing_images")}
            disabled={actionMutation.isPending || !summary || summary.imageMissing + summary.imageStale === 0}
          >
            <ImageIcon data-icon="inline-start" />
            生成缺失图片
          </Button>
          <Button
            onClick={() => actionMutation.mutate("generate_missing_videos")}
            disabled={actionMutation.isPending || !summary || summary.videoMissing + summary.videoStale === 0}
          >
            <Video data-icon="inline-start" />
            生成缺失视频
          </Button>
          <Button
            variant="outline"
            onClick={() => actionMutation.mutate("regenerate_failed_images")}
            disabled={actionMutation.isPending || !summary || summary.imageFailed === 0}
          >
            <RefreshCcw data-icon="inline-start" />
            重试失败图片
          </Button>
          <Button
            variant="outline"
            onClick={() => actionMutation.mutate("regenerate_failed_videos")}
            disabled={actionMutation.isPending || !summary || summary.videoFailed === 0}
          >
            <RefreshCcw data-icon="inline-start" />
            重试失败视频
          </Button>
          <Button
            variant="outline"
            onClick={() => actionMutation.mutate("cancel_running_videos")}
            disabled={actionMutation.isPending || runningCount === 0}
          >
            <XCircle data-icon="inline-start" />
            取消运行中
          </Button>
          <Button variant="outline" onClick={() => composeMutation.mutate()} disabled={composeMutation.isPending || !canCompose}>
            <Film data-icon="inline-start" />
            合成成片
          </Button>
        </div>

        {isLoading ? <Skeleton className="h-80" /> : null}
        {!isLoading && shots.length === 0 ? (
          <div className="rounded-lg border border-dashed p-12 text-center text-sm text-muted-foreground">暂无镜头视频任务</div>
        ) : null}

        <div className="grid gap-4">
          {shots.map((shot) => (
            <ShotVideoRow
              key={shot.id}
              shot={shot}
              unlinkPending={unlinkMediaMutation.isPending}
              onUnlink={(kind) => unlinkMediaMutation.mutate({ shotId: shot.id, kind })}
            />
          ))}
        </div>

        <div className="rounded-lg border bg-background">
          <div className="flex flex-wrap items-start justify-between gap-3 border-b p-4">
            <div>
              <h3 className="font-semibold">成片版本</h3>
              <p className="mt-1 text-sm text-muted-foreground">
                {activeTimeline ? `时间线：${activeTimeline.title} · ${statusLabel(activeTimeline.status)}` : "尚未创建时间线"}
              </p>
            </div>
            <Badge variant="outline">{finalVideos.length} 个版本</Badge>
          </div>
          {timelinesLoading || finalVideosLoading ? <Skeleton className="m-4 h-32" /> : null}
          {!timelinesLoading && !finalVideosLoading && finalVideos.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">暂无成片版本</p>
          ) : null}
          <div className="grid gap-4 p-4">
            {finalVideos.map((version) => (
              <FinalVideoCard
                key={version.id}
                version={version}
                activatePending={activateFinalMutation.isPending}
                downloadPending={downloadFinalMutation.isPending}
                deletePending={deleteFinalMutation.isPending}
                onActivate={() => activateFinalMutation.mutate(version.id)}
                onDownload={() => downloadFinalMutation.mutate(version.id)}
                onDelete={() => setFinalVideoToDelete(version)}
              />
            ))}
          </div>
        </div>
      </div>

      <AlertDialog open={!!finalVideoToDelete} onOpenChange={(open) => !open && setFinalVideoToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除成片版本</AlertDialogTitle>
            <AlertDialogDescription>
              {finalVideoToDelete?.status === "active" ? "当前版本正在作为项目成片使用，删除会清空当前成片绑定。" : "删除后该成片版本会从版本列表移除。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => finalVideoToDelete && deleteFinalMutation.mutate(finalVideoToDelete)}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Surface>
  );
}

function refreshVideoQueries(projectId: string, invalidate: (keys: QueryKey[]) => void) {
  const keys: QueryKey[] = [qk.shotProduction(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId), qk.timelines(projectId), qk.finalVideos(projectId)];
  invalidate(keys);
}

function ShotVideoRow({
  shot,
  unlinkPending,
  onUnlink,
}: {
  shot: ShotProductionShot;
  unlinkPending: boolean;
  onUnlink: (kind: MediaKind) => void;
}) {
  const hasImage = !!(shot.imagePreviewUrl || shot.imageArtifactId || shot.imageMediaFileId || shot.imageStorageKey);
  const hasVideo = !!(shot.videoPreviewUrl || shot.videoArtifactId || shot.videoMediaFileId || shot.videoStorageKey);
  return (
    <div className="grid gap-4 rounded-lg border p-4 lg:grid-cols-[280px_280px_minmax(0,1fr)]">
      <ShotPreview kind="image" url={shot.imagePreviewUrl} />
      <ShotPreview kind="video" url={shot.videoPreviewUrl} />
      <div className="grid content-start gap-3">
        <div>
          <div className="text-lg font-semibold">镜头 {shot.shotNo || shot.shotIndex + 1}</div>
          <div className="mt-2 flex flex-wrap gap-2">
            <Badge variant="outline">图片 {statusLabel(shot.imageStatus)}</Badge>
            <Badge variant="outline">视频 {statusLabel(shot.videoStatus)}</Badge>
            {shot.staleState && shot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(shot.staleState)}</Badge> : null}
          </div>
        </div>
        <p className="text-sm leading-6 text-muted-foreground">{shot.visual || "未填写画面描述"}</p>
        {shot.imageErrorMessage ? <p className="text-sm text-destructive">图片错误：{shot.imageErrorMessage}</p> : null}
        {shot.videoErrorMessage ? <p className="text-sm text-destructive">视频错误：{shot.videoErrorMessage}</p> : null}
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={() => onUnlink("image")} disabled={unlinkPending || !hasImage}>
            <Link2Off data-icon="inline-start" />
            解绑图片
          </Button>
          <Button size="sm" variant="outline" onClick={() => onUnlink("video")} disabled={unlinkPending || !hasVideo}>
            <Link2Off data-icon="inline-start" />
            解绑视频
          </Button>
        </div>
      </div>
    </div>
  );
}

function FinalVideoCard({
  version,
  activatePending,
  downloadPending,
  deletePending,
  onActivate,
  onDownload,
  onDelete,
}: {
  version: FinalVideoVersion;
  activatePending: boolean;
  downloadPending: boolean;
  deletePending: boolean;
  onActivate: () => void;
  onDownload: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="grid gap-4 rounded-lg border p-4 lg:grid-cols-[320px_minmax(0,1fr)]">
      <ShotPreview kind="video" url={version.previewUrl || undefined} />
      <div className="grid content-start gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div className="text-lg font-semibold">{version.title || `版本 ${version.version}`}</div>
            <div className="mt-1 text-sm text-muted-foreground">
              {version.resolution} · {version.aspectRatio}
            </div>
          </div>
          <Badge variant={version.status === "active" ? "default" : "outline"}>{statusLabel(version.status)}</Badge>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={onActivate} disabled={activatePending || version.status === "active"}>
            设为当前
          </Button>
          <Button size="sm" variant="outline" onClick={onDownload} disabled={downloadPending || !version.storageKey}>
            <Download data-icon="inline-start" />
            下载
          </Button>
          <Button size="sm" variant="outline" onClick={onDelete} disabled={deletePending}>
            <Trash2 data-icon="inline-start" />
            删除
          </Button>
        </div>
      </div>
    </div>
  );
}

function ShotPreview({ kind, url }: { kind: "image" | "video"; url?: string }) {
  if (url && kind === "video") {
    return <video className="aspect-video w-full rounded-md bg-black object-cover" controls src={url} />;
  }
  if (url) {
    return <img alt="镜头预览" className="aspect-video w-full rounded-md bg-muted object-cover" src={url} />;
  }
  return (
    <div className="grid aspect-video place-items-center rounded-md bg-muted">
      {kind === "image" ? <ImageIcon className="text-muted-foreground" /> : <Video className="text-muted-foreground" />}
    </div>
  );
}
