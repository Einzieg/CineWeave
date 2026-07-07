"use client";

import { useMemo } from "react";
import { Image as ImageIcon, RefreshCcw, Video, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { studioApi } from "@/lib/api-client";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { ShotProductionShot } from "@/lib/types";

type ShotAction = "generate_missing_images" | "generate_missing_videos" | "regenerate_failed_images" | "regenerate_failed_videos" | "cancel_running_videos";

export function VideoPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const { data: status, isLoading } = useApiQuery({
    key: qk.shotProduction(projectId),
    queryFn: (session) =>
      studioApi.getShotProductionStatus(session, projectId, {
        includePreviewUrl: true,
        previewExpiresSeconds: 900,
      }),
  });

  const shots = status?.shots ?? [];
  const summary = status?.summary;
  const runningCount = summary?.running ?? 0;

  const actionMutation = useApiMutation({
    mutationFn: (session, action: ShotAction) =>
      studioApi.runShotProductionAction(session, projectId, {
        action,
        options: { maxConcurrency: 1 },
      }),
    onSuccess: (response) => {
      toast.success(`${response.targetShotIds.length} 个镜头任务已启动`);
      invalidate([qk.shotProduction(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)]);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
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
      <SectionTitle title="视频" description="生成、查看和重试镜头图片与镜头视频" />
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
            <ImageIcon className="mr-2 h-4 w-4" />
            生成缺失图片
          </Button>
          <Button
            onClick={() => actionMutation.mutate("generate_missing_videos")}
            disabled={actionMutation.isPending || !summary || summary.videoMissing + summary.videoStale === 0}
          >
            <Video className="mr-2 h-4 w-4" />
            生成缺失视频
          </Button>
          <Button
            variant="outline"
            onClick={() => actionMutation.mutate("regenerate_failed_images")}
            disabled={actionMutation.isPending || !summary || summary.imageFailed === 0}
          >
            <RefreshCcw className="mr-2 h-4 w-4" />
            重试失败图片
          </Button>
          <Button
            variant="outline"
            onClick={() => actionMutation.mutate("regenerate_failed_videos")}
            disabled={actionMutation.isPending || !summary || summary.videoFailed === 0}
          >
            <RefreshCcw className="mr-2 h-4 w-4" />
            重试失败视频
          </Button>
          <Button
            variant="outline"
            onClick={() => actionMutation.mutate("cancel_running_videos")}
            disabled={actionMutation.isPending || runningCount === 0}
          >
            <XCircle className="mr-2 h-4 w-4" />
            取消运行中
          </Button>
        </div>

        {isLoading ? <Skeleton className="h-80" /> : null}
        {!isLoading && shots.length === 0 ? (
          <div className="rounded-lg border border-dashed p-12 text-center text-sm text-muted-foreground">暂无镜头视频任务</div>
        ) : null}

        <div className="grid gap-4">
          {shots.map((shot) => (
            <ShotVideoRow key={shot.id} shot={shot} />
          ))}
        </div>
      </div>
    </Surface>
  );
}

function ShotVideoRow({ shot }: { shot: ShotProductionShot }) {
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
      {kind === "image" ? <ImageIcon className="h-6 w-6 text-muted-foreground" /> : <Video className="h-6 w-6 text-muted-foreground" />}
    </div>
  );
}
