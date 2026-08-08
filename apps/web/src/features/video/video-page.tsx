"use client";

import NextImage from "next/image";
import { useMemo, useState } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Download, Film, Image as ImageIcon, Loader2, RefreshCcw, Sparkles, Trash2, Video, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ShotImageDetailDialog } from "@/features/storyboard/shot-image-detail-dialog";
import { ShotVideoDetailDialog } from "@/features/video/shot-video-detail-dialog";
import { studioApi } from "@/lib/api-client";
import { cssAspectRatio } from "@/lib/aspect-ratio";
import { localizePlatformError } from "@/lib/error-localization";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import { currentProjectScript } from "@/lib/scripts";
import type { FinalVideoVersion, ProjectTimeline, ShotProductionShot } from "@/lib/types";

type ShotAction = "generate_image_prompts" | "generate_video_prompts" | "generate_missing_images" | "generate_missing_videos" | "regenerate_failed_images" | "regenerate_failed_videos" | "cancel_running_videos";
type EpisodeShotAction = { action: ShotAction; scriptEpisodeId: string; force?: boolean };

export function VideoPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const pollingFallback = useProjectPollingFallback(projectId);
  const [finalVideoToDelete, setFinalVideoToDelete] = useState<FinalVideoVersion | null>(null);
  const [imageDetailShot, setImageDetailShot] = useState<ShotProductionShot | null>(null);
  const [videoDetailShot, setVideoDetailShot] = useState<ShotProductionShot | null>(null);
  const [selectedEpisodeId, setSelectedEpisodeId] = useState("");

  const { data: scripts = [], isLoading: scriptsLoading } = useApiQuery({
    key: qk.scripts(projectId),
    queryFn: (session) => studioApi.listScripts(session, projectId).then((response) => response.items || []),
  });
  const activeScript = useMemo(() => currentProjectScript(scripts), [scripts]);
  const activeVersionId = activeScript?.currentVersionId ?? activeScript?.currentVersion?.id ?? "";
  const { data: loadedEpisodes = [], isLoading: episodesLoading } = useApiQuery({
    key: qk.scriptEpisodes(projectId, activeScript?.id ?? "none", activeVersionId || "none"),
    queryFn: (session) => studioApi.listScriptEpisodes(session, projectId, activeScript!.id, activeVersionId).then((response) => response.items || []),
    enabled: !!activeScript?.id && !!activeVersionId,
  });
  const episodes = useMemo(
    () => [...loadedEpisodes].sort((left, right) => left.episodeIndex - right.episodeIndex || left.episodeTitle.localeCompare(right.episodeTitle, "zh-CN")),
    [loadedEpisodes],
  );
  const effectiveEpisodeId = episodes.some((episode) => episode.id === selectedEpisodeId) ? selectedEpisodeId : episodes[0]?.id ?? "";
  const selectedEpisode = episodes.find((episode) => episode.id === effectiveEpisodeId) ?? null;
  const selectedEpisodePosition = selectedEpisode ? episodes.findIndex((episode) => episode.id === selectedEpisode.id) : -1;

  const { data: status, isLoading: statusLoading } = useApiQuery({
    key: qk.shotProduction(projectId, effectiveEpisodeId || "none"),
    queryFn: (session) =>
      studioApi.getShotProductionStatus(session, projectId, {
        scriptEpisodeId: effectiveEpisodeId,
        includePreviewUrl: true,
        previewExpiresSeconds: 900,
      }),
    enabled: !!effectiveEpisodeId,
    refetchInterval: (query) => pollingFallback && (query.state.data?.summary.running ?? 0) > 0 ? 5000 : false,
  });
  const { data: projectStatus } = useApiQuery({
    key: qk.shotProduction(projectId),
    queryFn: (session) => studioApi.getShotProductionStatus(session, projectId),
    refetchInterval: (query) => pollingFallback && (query.state.data?.summary.running ?? 0) > 0 ? 5000 : false,
  });
  const { data: timelines = [], isLoading: timelinesLoading } = useApiQuery({
    key: qk.timelines(projectId),
    queryFn: (session) => studioApi.listTimelines(session, projectId).then((response) => response.items || []),
  });
  const { data: finalVideos = [], isLoading: finalVideosLoading } = useApiQuery({
    key: qk.finalVideos(projectId),
    queryFn: (session) => studioApi.listFinalVideos(session, projectId).then((response) => response.items || []),
  });

  const shots = useMemo(() => [...(status?.shots ?? [])].sort(compareVideoShots), [status?.shots]);
  const shotAspectRatio = status?.aspectRatio || "16:9";
  const summary = status?.summary;
  const loading = scriptsLoading || episodesLoading || (!!effectiveEpisodeId && statusLoading);
  const runningCount = summary?.running ?? 0;
  const pendingImageCount = (summary?.imageMissing ?? 0) + (summary?.imageStale ?? 0);
  const failedImageCount = summary?.imageFailed ?? 0;
  const pendingVideoCount = (summary?.videoMissing ?? 0) + (summary?.videoStale ?? 0);
  const failedVideoCount = summary?.videoFailed ?? 0;
  const pendingImagePromptCount = (summary?.imagePromptMissing ?? 0) + (summary?.imagePromptFailed ?? 0);
  const pendingVideoPromptCount = (summary?.videoPromptMissing ?? 0) + (summary?.videoPromptFailed ?? 0);
  const imageReadyToGenerateCount = shots.filter((shot) => shot.canGenerateImage || shot.canRetryImage).length;
  const videoReadyToGenerateCount = shots.filter((shot) => shot.canGenerateVideo).length;
  const retryableFailedImageCount = shots.filter((shot) => shot.canRetryImage).length;
  const retryableFailedVideoCount = shots.filter((shot) => shot.canRetryVideo).length;
  const cancellableVideoCount = shots.filter((shot) => (shot.videoStatus === "queued" || shot.videoStatus === "running") && !!shot.providerAsyncTaskId).length;
  const activeTimeline = timelines.find((timeline) => timeline.status === "active") ?? timelines[0] ?? null;
  const projectSummary = projectStatus?.summary;
  const canCompose = !!projectSummary && projectSummary.total > 0 && projectSummary.videoSucceeded === projectSummary.total && projectSummary.running === 0;

  const actionMutation = useApiMutation({
    mutationFn: (session, request: EpisodeShotAction) => {
      if (request.action === "generate_video_prompts") {
        return studioApi.generateVideoPromptsBatch(session, projectId, {
          scriptEpisodeId: request.scriptEpisodeId,
          force: request.force,
        });
      }
      if (request.action === "generate_missing_videos") {
        return studioApi.generateShotVideosBatch(session, projectId, {
          scriptEpisodeId: request.scriptEpisodeId,
        });
      }
      if (request.action === "regenerate_failed_videos") {
        return studioApi.generateShotVideosBatch(session, projectId, {
          scriptEpisodeId: request.scriptEpisodeId,
          shotIds: shots.filter((shot) => shot.canRetryVideo).map((shot) => shot.id),
          force: true,
        });
      }
      return studioApi.runShotProductionAction(session, projectId, {
        action: request.action,
        scriptEpisodeId: request.scriptEpisodeId,
      });
    },
    onSuccess: (response) => {
      const actionLabel = response.action === "generate_image_prompts"
        ? "图片提示词"
        : response.action === "generate_video_prompts"
          ? "视频提示词"
          : "镜头";
      toast.success(`当前分集 ${response.targetShotIds.length} 个${actionLabel}任务已启动`);
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const composeMutation = useApiMutation({
    mutationFn: async (session) => {
      let timeline: ProjectTimeline | null = activeTimeline;
      if (!timeline) {
        timeline = await studioApi.createTimeline(session, projectId, {
          title: "主时间线",
          fromStoryboardShots: true,
        }, crypto.randomUUID());
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
    mutationFn: (session, version: FinalVideoVersion) => studioApi.activateFinalVideo(
      session,
      projectId,
      version.id,
      version.revision,
      `final-video-activate-${version.id}-${version.revision}-${crypto.randomUUID()}`,
    ),
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
    mutationFn: (session, version: FinalVideoVersion) => studioApi.deleteFinalVideo(
      session,
      projectId,
      version.id,
      version.revision,
      version.status === "active",
      `final-video-delete-${version.id}-${version.revision}-${crypto.randomUUID()}`,
    ),
    onSuccess: () => {
      toast.success("成片版本已删除");
      setFinalVideoToDelete(null);
      refreshVideoQueries(projectId, invalidate);
    },
    onError: (error) => toast.error("删除失败：" + error.message),
  });

  const cards = useMemo(
    () => [
      { label: "本集镜头", value: summary?.total ?? 0 },
      { label: "待生成图片", value: pendingImageCount },
      { label: "失败图片", value: failedImageCount },
      { label: "待生成视频", value: pendingVideoCount },
      { label: "失败视频", value: failedVideoCount },
      { label: "图片提示词就绪", value: summary?.imagePromptSucceeded ?? 0 },
      { label: "视频提示词就绪", value: summary?.videoPromptSucceeded ?? 0 },
      { label: "运行中", value: runningCount },
    ],
    [failedImageCount, failedVideoCount, pendingImageCount, pendingVideoCount, runningCount, summary?.imagePromptSucceeded, summary?.total, summary?.videoPromptSucceeded],
  );

  function selectEpisode(episodeId: string) {
    setSelectedEpisodeId(episodeId);
    setImageDetailShot(null);
    setVideoDetailShot(null);
  }

  function moveEpisode(offset: number) {
    const nextEpisode = episodes[selectedEpisodePosition + offset];
    if (nextEpisode) selectEpisode(nextEpisode.id);
  }

  function runEpisodeAction(action: ShotAction, force = false) {
    if (!effectiveEpisodeId) return;
    actionMutation.mutate({ action, scriptEpisodeId: effectiveEpisodeId, force });
  }

  return (
    <Surface>
      <SectionTitle title="视频" description="生成、查看和管理镜头图片、镜头视频与最终成片" />
      <div className="flex flex-wrap items-center gap-3 border-b bg-muted/20 px-4 py-3">
        <Button variant="ghost" size="icon-sm" title="上一集" aria-label="上一集" onClick={() => moveEpisode(-1)} disabled={selectedEpisodePosition <= 0}>
          <ChevronLeft />
        </Button>
        <Select value={effectiveEpisodeId} onValueChange={selectEpisode} disabled={episodes.length === 0}>
          <SelectTrigger className="w-full min-w-0 sm:w-[380px]">
            <SelectValue placeholder={episodesLoading ? "正在加载分集" : "选择分集"} />
          </SelectTrigger>
          <SelectContent>
            {episodes.map((episode) => (
              <SelectItem key={episode.id} value={episode.id}>
                第 {episode.episodeIndex} 集 · {episode.episodeTitle}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="ghost" size="icon-sm" title="下一集" aria-label="下一集" onClick={() => moveEpisode(1)} disabled={selectedEpisodePosition < 0 || selectedEpisodePosition >= episodes.length - 1}>
          <ChevronRight />
        </Button>
        <span className="text-xs text-muted-foreground">{selectedEpisodePosition >= 0 ? `${selectedEpisodePosition + 1} / ${episodes.length}` : `0 / ${episodes.length}`}</span>
        <div className="ml-auto flex items-center gap-2">
          {selectedEpisode ? <Badge variant="outline">第 {selectedEpisode.episodeIndex} 集</Badge> : null}
          <Badge variant="outline">{summary?.total ?? 0} 个镜头</Badge>
        </div>
      </div>
      <div className="grid gap-5 p-4">
        <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-8">
          {cards.map((card) => (
            <div key={card.label} className="rounded-lg border p-3">
              <div className="text-xs text-muted-foreground">{card.label}</div>
              <div className="mt-1 text-2xl font-semibold">{card.value}</div>
            </div>
          ))}
        </div>

        <div className="flex flex-wrap gap-2">
          <Button
            onClick={() => runEpisodeAction("generate_image_prompts")}
            disabled={actionMutation.isPending || !summary || pendingImagePromptCount === 0}
          >
            {actionMutation.isPending && actionMutation.variables?.action === "generate_image_prompts" ? <Loader2 className="animate-spin" /> : <Sparkles />}
            {summary?.imagePromptRunning ? `生成图片提示词 (${summary.imagePromptRunning} 运行中)` : pendingImagePromptCount > 0 ? `一键生成图片提示词 (${pendingImagePromptCount})` : "图片提示词已就绪"}
          </Button>
          <Button
            onClick={() => runEpisodeAction("generate_video_prompts")}
            disabled={actionMutation.isPending || !summary || pendingVideoPromptCount === 0}
          >
            {actionMutation.isPending && actionMutation.variables?.action === "generate_video_prompts" && !actionMutation.variables.force ? <Loader2 className="animate-spin" /> : <Sparkles />}
            {summary?.videoPromptRunning ? `生成视频提示词 (${summary.videoPromptRunning} 运行中)` : pendingVideoPromptCount > 0 ? `一键生成视频提示词 (${pendingVideoPromptCount})` : "提示词已全部就绪"}
          </Button>
          <Button
            variant="outline"
            onClick={() => runEpisodeAction("generate_video_prompts", true)}
            disabled={actionMutation.isPending || !summary || summary.total === 0 || summary.videoPromptRunning > 0}
          >
            {actionMutation.isPending && actionMutation.variables?.action === "generate_video_prompts" && actionMutation.variables.force ? <Loader2 className="animate-spin" /> : <RefreshCcw />}
            重新生成本集提示词
          </Button>
          <Button
            onClick={() => runEpisodeAction("generate_missing_images")}
            disabled={actionMutation.isPending || !summary || imageReadyToGenerateCount === 0}
          >
            {actionMutation.isPending && actionMutation.variables?.action === "generate_missing_images" ? <Loader2 className="animate-spin" /> : <ImageIcon />}
            {imageReadyToGenerateCount > 0 ? `生成缺失图片 (${imageReadyToGenerateCount})` : pendingImageCount > 0 ? "请先生成图片提示词" : "暂无缺失图片"}
          </Button>
          <Button
            onClick={() => runEpisodeAction("generate_missing_videos")}
            disabled={actionMutation.isPending || !summary || videoReadyToGenerateCount === 0}
          >
            {actionMutation.isPending && actionMutation.variables?.action === "generate_missing_videos" ? <Loader2 className="animate-spin" /> : <Video data-icon="inline-start" />}
            {videoReadyToGenerateCount > 0 ? `生成缺失视频 (${videoReadyToGenerateCount})` : pendingVideoCount > 0 ? "请先生成视频提示词" : "暂无缺失视频"}
          </Button>
          <Button
            variant="outline"
            onClick={() => runEpisodeAction("regenerate_failed_images")}
            disabled={actionMutation.isPending || !summary || retryableFailedImageCount === 0}
          >
            <RefreshCcw data-icon="inline-start" />
            {retryableFailedImageCount > 0 ? `重试失败图片 (${retryableFailedImageCount})` : failedImageCount > 0 ? "请先生成图片提示词" : "无失败图片"}
          </Button>
          <Button
            variant="outline"
            onClick={() => runEpisodeAction("regenerate_failed_videos")}
            disabled={actionMutation.isPending || !summary || retryableFailedVideoCount === 0}
          >
            {actionMutation.isPending && actionMutation.variables?.action === "regenerate_failed_videos" ? <Loader2 className="animate-spin" /> : <RefreshCcw data-icon="inline-start" />}
            {retryableFailedVideoCount > 0 ? `重试失败视频 (${retryableFailedVideoCount})` : failedVideoCount > 0 ? "请先生成视频提示词" : "无失败视频"}
          </Button>
          <Button
            variant="outline"
            onClick={() => runEpisodeAction("cancel_running_videos")}
            disabled={actionMutation.isPending || cancellableVideoCount === 0}
          >
            <XCircle data-icon="inline-start" />
            {cancellableVideoCount > 0 ? `取消运行视频 (${cancellableVideoCount})` : "无运行中视频"}
          </Button>
          <Button variant="outline" onClick={() => composeMutation.mutate()} disabled={composeMutation.isPending || !canCompose}>
            <Film data-icon="inline-start" />
            合成项目成片
          </Button>
        </div>

        {loading ? <Skeleton className="h-80" /> : null}
        {!loading && episodes.length === 0 ? (
          <div className="rounded-lg border border-dashed p-12 text-center text-sm text-muted-foreground">当前剧本没有分集</div>
        ) : null}
        {!loading && episodes.length > 0 && shots.length === 0 ? (
          <div className="rounded-lg border border-dashed p-12 text-center text-sm text-muted-foreground">本集尚未生成分镜</div>
        ) : null}

        {!loading ? (
          <div className="grid gap-4">
            {shots.map((shot) => (
              <ShotVideoRow
                key={shot.id}
                shot={shot}
                aspectRatio={shotAspectRatio}
                onOpenImage={() => setImageDetailShot(shot)}
                onOpenVideo={() => setVideoDetailShot(shot)}
              />
            ))}
          </div>
        ) : null}

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
                onActivate={() => activateFinalMutation.mutate(version)}
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

      {imageDetailShot ? (
        <ShotImageDetailDialog
          projectId={projectId}
          shotId={imageDetailShot.id}
          shotNumber={videoShotNumber(imageDetailShot)}
          open
          onOpenChange={(open) => !open && setImageDetailShot(null)}
          onChanged={() => refreshVideoQueries(projectId, invalidate)}
        />
      ) : null}
      {videoDetailShot ? (
        <ShotVideoDetailDialog
          projectId={projectId}
          shotId={videoDetailShot.id}
          shotNumber={videoShotNumber(videoDetailShot)}
          initialPosterUrl={videoDetailShot.imagePreviewUrl}
          open
          onOpenChange={(open) => !open && setVideoDetailShot(null)}
          onChanged={() => refreshVideoQueries(projectId, invalidate)}
        />
      ) : null}
    </Surface>
  );
}

function compareVideoShots(left: ShotProductionShot, right: ShotProductionShot) {
  return (left.episodeShotIndex ?? left.shotIndex) - (right.episodeShotIndex ?? right.shotIndex) || left.shotNo - right.shotNo;
}

function videoShotNumber(shot: ShotProductionShot) {
  return shot.episodeShotIndex != null ? shot.episodeShotIndex + 1 : shot.shotNo || shot.shotIndex + 1;
}

function refreshVideoQueries(projectId: string, invalidate: (keys: QueryKey[]) => void) {
  const keys: QueryKey[] = [qk.shotProductionPrefix(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId), qk.timelines(projectId), qk.finalVideos(projectId)];
  invalidate(keys);
}

function ShotVideoRow({
  shot,
  aspectRatio,
  onOpenImage,
  onOpenVideo,
}: {
  shot: ShotProductionShot;
  aspectRatio: string;
  onOpenImage: () => void;
  onOpenVideo: () => void;
}) {
  return (
    <div className="grid gap-4 rounded-lg border p-4 lg:grid-cols-[280px_280px_minmax(0,1fr)]">
      <ShotMediaTile kind="image" url={shot.imagePreviewUrl} status={shot.imageStatus} aspectRatio={aspectRatio} onOpen={onOpenImage} />
      <ShotMediaTile kind="video" url={shot.videoPreviewUrl} posterUrl={shot.imagePreviewUrl} status={shot.videoStatus} aspectRatio={aspectRatio} onOpen={onOpenVideo} />
      <div className="grid content-start gap-3">
        <div>
          <div className="text-lg font-semibold">镜头 {videoShotNumber(shot)}</div>
          <div className="mt-2 flex flex-wrap gap-2">
            <Badge variant="outline">图片提示词 {statusLabel(shot.imagePromptStatus)}</Badge>
            <Badge variant="outline">图片 {statusLabel(shot.imageStatus)}</Badge>
            <Badge variant="outline">视频提示词 {statusLabel(shot.videoPromptStatus)}</Badge>
            <Badge variant="outline">视频 {statusLabel(shot.videoStatus)}</Badge>
            {shot.staleState && shot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(shot.staleState)}</Badge> : null}
          </div>
        </div>
        <p className="text-sm leading-6 text-muted-foreground">{shot.visual || "未填写画面描述"}</p>
        {shot.imageErrorMessage ? <p className="text-sm text-destructive">图片错误：{localizePlatformError(shot.imageErrorMessage, shot.imageErrorCode)}</p> : null}
        {shot.imagePromptErrorMessage ? <p className="text-sm text-destructive">图片提示词错误：{localizePlatformError(shot.imagePromptErrorMessage, shot.imagePromptErrorCode)}</p> : null}
        {shot.videoErrorMessage ? <p className="text-sm text-destructive">视频错误：{localizePlatformError(shot.videoErrorMessage, shot.videoErrorCode)}</p> : null}
        {shot.videoPromptErrorMessage ? <p className="text-sm text-destructive">提示词错误：{localizePlatformError(shot.videoPromptErrorMessage, shot.videoPromptErrorCode)}</p> : null}
      </div>
    </div>
  );
}

function ShotMediaTile({ kind, url, posterUrl, status, aspectRatio, onOpen }: { kind: "image" | "video"; url?: string; posterUrl?: string; status: string; aspectRatio: string; onOpen: () => void }) {
  return (
    <button type="button" className="group relative w-full overflow-hidden rounded-md bg-muted text-left outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2" style={{ aspectRatio: cssAspectRatio(aspectRatio) }} onClick={onOpen}>
      {url && kind === "video" && posterUrl ? <NextImage alt="镜头视频封面" className="object-cover" fill unoptimized sizes="280px" src={posterUrl} /> : null}
      {url && kind === "video" ? <span className="absolute inset-0 grid place-items-center bg-black/10"><Video className="size-8 text-white drop-shadow" /></span> : null}
      {url && kind === "image" ? <NextImage alt="镜头图片预览" className="object-cover" fill unoptimized sizes="280px" src={url} /> : null}
      {!url ? <span className="grid h-full place-items-center">{kind === "image" ? <ImageIcon className="text-muted-foreground" /> : <Video className="text-muted-foreground" />}</span> : null}
      <span className="absolute inset-x-0 bottom-0 flex items-center justify-between bg-black/65 px-3 py-2 text-xs text-white">
        <span>{kind === "image" ? "图片设置" : "视频设置"}</span>
        <span>{statusLabel(status)}</span>
      </span>
    </button>
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
    return <div className="relative aspect-video w-full overflow-hidden rounded-md bg-muted"><NextImage alt="镜头预览" className="object-cover" fill unoptimized sizes="320px" src={url} /></div>;
  }
  return (
    <div className="grid aspect-video place-items-center rounded-md bg-muted">
      {kind === "image" ? <ImageIcon className="text-muted-foreground" /> : <Video className="text-muted-foreground" />}
    </div>
  );
}
