"use client";

import NextImage from "next/image";
import { useMemo, useRef, useState } from "react";
import { CheckCircle2, Film, Link2Off, Loader2, Maximize2, RefreshCw, Save, ShieldAlert, Sparkles, Square, Video, X } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
  formatGenerationTime,
  normalizeReferenceMode,
  ReferenceModeSelector,
  ReferenceOptionCard,
  type ShotReferenceMode,
} from "@/features/storyboard/shot-reference-controls";
import { studioApi } from "@/lib/api-client";
import { cssAspectRatio } from "@/lib/aspect-ratio";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { retainUsableSignedMediaUrl } from "@/lib/signed-media-url";
import type { NativeAudioReview, StoryboardShotDetail, StoryboardShotVideoReferenceOption, UpdateStoryboardShotRequest, VideoRenderPlan } from "@/lib/types";

type VideoDraft = {
  shotId: string;
  promptRevision: string;
  videoPrompt: string;
  referenceMode: ShotReferenceMode;
  referenceKeys: string[];
};

const EMPTY_DRAFT: VideoDraft = {
  shotId: "",
  promptRevision: "",
  videoPrompt: "",
  referenceMode: "auto",
  referenceKeys: [],
};

export function ShotVideoDetailDialog({
  projectId,
  shotId,
  shotNumber,
  initialPosterUrl,
  open,
  onOpenChange,
  onChanged,
}: {
  projectId: string;
  shotId: string;
  shotNumber: number;
  initialPosterUrl?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onChanged: () => void;
}) {
  const invalidate = useInvalidateKeys();
  const [draft, setDraft] = useState<VideoDraft>(EMPTY_DRAFT);
  const [largePreview, setLargePreview] = useState<{ url: string; title: string; kind: "image" | "video" } | null>(null);
  const [readyVideoKey, setReadyVideoKey] = useState("");
  const { data: detail, isLoading } = useApiQuery({
    key: qk.shotDetail(projectId, shotId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotDetail(session, projectId, shotId),
    enabled: open && !!shotId,
    refetchInterval: (query) => {
      const videoStatus = query.state.data?.shot.videoStatus;
      const promptStatus = query.state.data?.shot.videoPromptStatus;
      return open && (
        videoStatus === "queued"
        || videoStatus === "running"
        || promptStatus === "queued"
        || promptStatus === "running"
      ) ? 3000 : false;
    },
    structuralSharing: (previous, next) => preserveVideoPreviewUrls(
      previous as StoryboardShotDetail | undefined,
      next as StoryboardShotDetail,
    ),
  });
  const { data: renderPlan, isLoading: renderPlanLoading } = useApiQuery({
    key: qk.shotRenderPlan(projectId, shotId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotRenderPlan(session, projectId, shotId),
    enabled: open && !!shotId && !!detail?.shot.activeVideoRenderPlanId,
    refetchInterval: (query) => open && ["planned", "running"].includes(query.state.data?.status ?? "") ? 3000 : false,
  });
  const { data: audioReviews = [], isLoading: audioReviewsLoading } = useApiQuery({
    key: qk.nativeAudioReviews(projectId, shotId || "none"),
    queryFn: (session) => studioApi.listNativeAudioReviews(session, projectId, shotId).then((response) => response.items),
    enabled: open && !!shotId && !!detail?.shot.activeVideoRenderPlanId,
    refetchInterval: (query) => open && query.state.data?.some((review) => review.status === "queued" || review.status === "running") ? 3000 : false,
  });

  const detailPromptRevision = detail ? promptRevision(detail) : "";
  if (detail && (draft.shotId !== detail.shot.id || draft.promptRevision !== detailPromptRevision)) {
    setDraft(draftFromDetail(detail));
  }

  const referenceOptions = useMemo(() => detail?.videoReferenceOptions ?? [], [detail?.videoReferenceOptions]);
  const selectedReferenceKeys = useMemo(() => {
    if (draft.referenceMode === "custom") return new Set(draft.referenceKeys);
    if (draft.referenceMode === "auto") return new Set(referenceOptions.filter((option) => option.autoSelected).map((option) => option.key));
    return new Set<string>();
  }, [draft.referenceKeys, draft.referenceMode, referenceOptions]);
  const videoRunning = detail?.shot.videoStatus === "queued" || detail?.shot.videoStatus === "running";
  const promptRunning = detail?.shot.videoPromptStatus === "queued" || detail?.shot.videoPromptStatus === "running";
  const currentVideoKey = detail?.shot.videoArtifactId || detail?.videoPreviewUrl || detail?.shot.videoPreviewUrl || "";
  const videoPreviewReady = !currentVideoKey || readyVideoKey === currentVideoKey;
  const autoHasReference = referenceOptions.some((option) => option.autoSelected);
  const referencesValid = draft.referenceMode === "none" || (draft.referenceMode === "auto" ? autoHasReference : draft.referenceKeys.length > 0);
  const canSubmit = referencesValid && draft.videoPrompt.trim().length > 0;

  const refresh = () => {
    invalidate([
      qk.shotDetail(projectId, shotId),
      qk.shotRenderPlan(projectId, shotId),
      qk.nativeAudioReviews(projectId, shotId),
      qk.shotProductionPrefix(projectId),
      qk.workflowRuns(projectId),
      qk.productionStatus(projectId),
      qk.artifacts(projectId),
    ]);
    onChanged();
  };

  const renderPlanMutation = useApiMutation({
    mutationFn: (session) => studioApi.createStoryboardShotRenderPlan(session, projectId, shotId, {
      aspectRatio: detail?.aspectRatio || "16:9",
      resolution: "720p",
    }),
    onSuccess: () => {
      toast.success("视频执行计划已生成");
      refresh();
    },
    onError: (error) => toast.error("执行计划生成失败：" + error.message),
  });

  const audioVerificationMutation = useApiMutation({
    mutationFn: (session, decision: "approve" | "reject") => studioApi.verifyStoryboardShotRenderPlanAudio(session, projectId, shotId, { decision }),
    onSuccess: (_, decision) => {
      toast.success(decision === "approve" ? "音轨审核已通过" : "音轨已退回重试");
      refresh();
    },
    onError: (error) => toast.error("音轨审核失败：" + error.message),
  });

  const nativeAudioReviewMutation = useApiMutation({
    mutationFn: (session) => studioApi.startNativeAudioReview(session, projectId, shotId, {
      videoRenderPlanId: renderPlan?.id || "",
      maxConcurrency: 5,
    }),
    onSuccess: (run) => {
      toast.success(`音轨自动审核已启动：${run.id.slice(0, 8)}`);
      refresh();
    },
    onError: (error) => toast.error("音轨自动审核启动失败：" + error.message),
  });

  const saveMutation = useApiMutation({
    mutationFn: (session) => studioApi.updateStoryboardShot(session, projectId, shotId, videoUpdateBody(draft)),
    onSuccess: (shot) => {
      setDraft((current) => ({
        ...current,
        promptRevision: shot.videoPromptUpdatedAt ?? shot.videoPrompt ?? "",
        videoPrompt: shot.videoPrompt ?? "",
        referenceMode: normalizeReferenceMode(shot.videoReferenceMode),
        referenceKeys: shot.videoReferenceKeys ?? [],
      }));
      toast.success("分镜视频设置已保存");
      refresh();
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const promptMutation = useApiMutation({
    mutationFn: (session) => studioApi.runShotProductionAction(session, projectId, {
      action: "generate_selected_video_prompts",
      shotIds: [shotId],
      options: { force: true, maxConcurrency: 1 },
    }),
    onSuccess: () => {
      toast.success(detail?.shot.videoPrompt ? "视频提示词重新生成中" : "视频提示词生成中");
      refresh();
    },
    onError: (error) => toast.error("提示词生成失败：" + error.message),
  });

  const generateMutation = useApiMutation({
    mutationFn: async (session) => {
      await studioApi.updateStoryboardShot(session, projectId, shotId, videoUpdateBody(draft));
      return studioApi.runShotProductionAction(session, projectId, {
        action: "generate_selected_videos",
        shotIds: [shotId],
        options: { maxConcurrency: 1 },
      });
    },
    onSuccess: () => {
      toast.success(detail?.shot.videoArtifactId ? "分镜视频重新生成中" : "分镜视频生成中");
      refresh();
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const cancelMutation = useApiMutation({
    mutationFn: (session) => studioApi.runShotProductionAction(session, projectId, {
      action: "cancel_running_videos",
      shotIds: [shotId],
    }),
    onSuccess: () => {
      toast.success("已提交取消请求");
      refresh();
    },
    onError: (error) => toast.error("取消失败：" + error.message),
  });

  const unlinkMutation = useApiMutation({
    mutationFn: (session) => studioApi.unlinkStoryboardShotMedia(session, projectId, shotId, "video"),
    onSuccess: () => {
      toast.success("当前视频已解绑");
      refresh();
    },
    onError: (error) => toast.error("解绑失败：" + error.message),
  });

  function setReferenceMode(mode: ShotReferenceMode) {
    setDraft((current) => ({
      ...current,
      referenceMode: mode,
      referenceKeys: mode === "custom"
        ? current.referenceKeys.length > 0
          ? current.referenceKeys
          : referenceOptions.filter((option) => option.selected || option.autoSelected).map((option) => option.key)
        : [],
    }));
  }

  function toggleReference(key: string, checked: boolean) {
    setDraft((current) => {
      const keys = new Set(current.referenceKeys);
      if (checked) keys.add(key);
      else keys.delete(key);
      return { ...current, referenceMode: "custom", referenceKeys: [...keys] };
    });
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!nextOpen && largePreview) return;
      onOpenChange(nextOpen);
    }}>
      <DialogContent className="h-[min(90vh,920px)] max-w-[min(96vw,1320px)] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-[min(96vw,1320px)]" showCloseButton={!largePreview}>
        <DialogHeader className="border-b px-5 py-4 pr-14">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle>第 {shotNumber} 镜 · 视频详情</DialogTitle>
            {detail ? <Badge variant="outline">{statusLabel(detail.shot.videoStatus)}</Badge> : null}
            {detail?.shot.staleState && detail.shot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(detail.shot.staleState)}</Badge> : null}
          </div>
          <DialogDescription>{detail?.shot.title || detail?.shot.visual || "查看并调整镜头视频生成设置"}</DialogDescription>
        </DialogHeader>

        {isLoading || !detail ? (
          <div className="grid min-h-0 flex-1 gap-4 overflow-hidden p-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(360px,.85fr)]">
            <Skeleton className="h-full min-h-80" />
            <Skeleton className="h-full min-h-80" />
          </div>
        ) : (
          <div className="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[minmax(0,1.15fr)_minmax(360px,.85fr)] lg:overflow-hidden">
            <div className="p-5 lg:min-h-0 lg:overflow-y-auto">
              <CurrentVideo
                detail={detail}
                posterUrl={initialPosterUrl}
                onReady={() => setReadyVideoKey(currentVideoKey)}
                onOpen={(url, title) => setLargePreview({ url, title, kind: "video" })}
              />

              {detail.shot.videoErrorMessage ? (
                <div role="alert" className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  {detail.shot.videoErrorMessage}
                </div>
              ) : null}

              {detail.shot.videoPromptErrorMessage ? (
                <div role="alert" className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                  视频提示词生成失败：{detail.shot.videoPromptErrorMessage}
                </div>
              ) : null}

              <div className="mt-4 flex flex-wrap gap-2">
                <Button onClick={() => generateMutation.mutate()} disabled={!canSubmit || videoRunning || promptRunning || generateMutation.isPending || saveMutation.isPending}>
                  {videoRunning || generateMutation.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                  {videoRunning ? "生成中" : detail.shot.videoArtifactId ? "重新生成" : "生成视频"}
                </Button>
                <Button variant="outline" onClick={() => saveMutation.mutate()} disabled={!canSubmit || videoRunning || promptRunning || saveMutation.isPending || generateMutation.isPending}>
                  {saveMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
                  保存设置
                </Button>
                {videoRunning ? (
                  <Button variant="outline" onClick={() => cancelMutation.mutate()} disabled={cancelMutation.isPending}>
                    {cancelMutation.isPending ? <Loader2 className="animate-spin" /> : <Square />}
                    取消任务
                  </Button>
                ) : null}
                <Button variant="outline" onClick={() => unlinkMutation.mutate()} disabled={!detail.shot.videoArtifactId || unlinkMutation.isPending || videoRunning}>
                  {unlinkMutation.isPending ? <Loader2 className="animate-spin" /> : <Link2Off />}
                  解绑视频
                </Button>
              </div>

              <RenderPlanPanel
                plan={renderPlan}
                loading={renderPlanLoading}
                creating={renderPlanMutation.isPending}
                onCreate={() => renderPlanMutation.mutate()}
                verifying={audioVerificationMutation.isPending}
                onVerify={(decision) => audioVerificationMutation.mutate(decision)}
                reviews={audioReviews}
                reviewsLoading={audioReviewsLoading}
                reviewing={nativeAudioReviewMutation.isPending || audioReviews.some((review) => review.status === "queued" || review.status === "running")}
                onReview={() => nativeAudioReviewMutation.mutate()}
                onOpen={(url, title) => setLargePreview({ url, title, kind: "video" })}
              />

              <VideoGenerationHistory detail={detail} onOpen={(url, title) => setLargePreview({ url, title, kind: "video" })} />
            </div>

            <div className="border-t bg-muted/10 p-5 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:border-t-0">
              <section className="space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <h3 className="text-sm font-semibold">剧本原始台词</h3>
                    <p className="mt-1 text-xs text-muted-foreground">视频提示词必须逐字保留以下中文台词。</p>
                  </div>
                  <Badge variant="outline">{detail.shot.scriptDialogue.length} 条</Badge>
                </div>
                {detail.shot.scriptDialogue.length > 0 ? (
                  <div className="grid gap-2">
                    {detail.shot.scriptDialogue.map((line, index) => (
                      <div key={`${line.speaker}-${index}`} className="rounded-md border bg-background px-3 py-2 text-sm leading-6">
                        <span className="font-medium">{line.speaker || "角色"}：</span>
                        <span>{line.text}</span>
                        {line.delivery ? <span className="ml-2 text-xs text-muted-foreground">（{line.delivery}）</span> : null}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="rounded-md border border-dashed px-3 py-2 text-sm text-muted-foreground">该镜头尚未绑定剧本台词</div>
                )}
              </section>

              <section className="space-y-2">
                <div className="mt-5 flex flex-wrap items-center justify-between gap-2 border-t pt-5">
                  <div className="flex items-center gap-2">
                    <Label htmlFor="shot-video-prompt">镜头视频提示词</Label>
                    <Badge variant="outline">{statusLabel(detail.shot.videoPromptStatus)}</Badge>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => promptMutation.mutate()}
                    disabled={promptRunning || videoRunning || promptMutation.isPending}
                  >
                    {promptRunning || promptMutation.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}
                    {promptRunning ? "生成中" : detail.shot.videoPrompt ? "重新生成提示词" : "生成提示词"}
                  </Button>
                </div>
                <Textarea
                  id="shot-video-prompt"
                  className="min-h-52 resize-y leading-6"
                  value={draft.videoPrompt}
                  disabled={videoRunning || promptRunning}
                  onChange={(event) => setDraft((current) => ({ ...current, videoPrompt: event.target.value }))}
                />
                {!draft.videoPrompt.trim() ? <p className="text-xs text-destructive">请先生成或填写视频提示词，再生成视频。</p> : null}
              </section>

              <section className="mt-5 flex items-center justify-between gap-3 border-t pt-5">
                <Label>镜头规划时长</Label>
                <Badge variant="outline">{formatShotDuration(detail.shot.durationSeconds)}</Badge>
              </section>

              <section className="mt-6 space-y-3 border-t pt-5">
                <div>
                  <h3 className="text-sm font-semibold">参考图策略</h3>
                  <p className="mt-1 text-xs text-muted-foreground">自动模式优先使用当前镜头图；手动模式可改用资产参考图。</p>
                </div>
                <ReferenceModeSelector value={draft.referenceMode} customDisabled={referenceOptions.length === 0} disabled={videoRunning} onChange={setReferenceMode} />

                {!referencesValid ? (
                  <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700">
                    {draft.referenceMode === "auto" ? "当前没有可自动采用的参考图，请先生成镜头图、手动选择参考图或切换为不使用。" : "至少选择一张参考图。"}
                  </div>
                ) : null}
                {referenceOptions.length === 0 ? (
                  <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">当前镜头没有可用参考图</div>
                ) : (
                  <div className="grid gap-2 sm:grid-cols-2">
                    {referenceOptions.map((option) => (
                      <ReferenceOptionCard
                        key={option.key}
                        option={option}
                        checked={selectedReferenceKeys.has(option.key)}
                        disabled={draft.referenceMode !== "custom" || videoRunning}
                        loadPreview={videoPreviewReady}
                        sourceLabel={videoReferenceSourceLabel(option)}
                        onCheckedChange={(checked) => toggleReference(option.key, checked)}
                        onOpen={() => option.previewUrl && setLargePreview({ url: option.previewUrl, title: option.title, kind: "image" })}
                      />
                    ))}
                  </div>
                )}
              </section>

              <section className="mt-6 border-t pt-5">
                <details className="rounded-md border bg-background px-3 py-2 text-xs">
                  <summary className="cursor-pointer font-medium text-muted-foreground">镜头信息</summary>
                  <div className="mt-3 grid gap-2 leading-5">
                    <DetailRow label="画面" value={detail.shot.visual} />
                    <DetailRow label="机位" value={detail.shot.camera} />
                    <DetailRow label="运镜" value={detail.shot.motion} />
                    <DetailRow label="情绪" value={detail.shot.mood} />
                  </div>
                </details>
              </section>
            </div>
          </div>
        )}

        {largePreview ? (
          <div className="absolute inset-0 z-20 grid place-items-center bg-black/90 p-6" onClick={() => setLargePreview(null)}>
            <button type="button" className="absolute right-4 top-4 z-10 grid size-10 place-items-center rounded-full bg-white/10 text-white hover:bg-white/20" onClick={(event) => { event.stopPropagation(); setLargePreview(null); }} aria-label="关闭视频预览">
              <X />
            </button>
            <div className="relative flex h-full w-full items-center justify-center" onClick={(event) => event.stopPropagation()}>
              {largePreview.kind === "video" ? (
                <video className="max-h-full max-w-full" controls autoPlay src={largePreview.url} />
              ) : (
                <NextImage src={largePreview.url} alt={largePreview.title} fill unoptimized sizes="100vw" className="object-contain" />
              )}
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function CurrentVideo({ detail, posterUrl, onReady, onOpen }: { detail: StoryboardShotDetail; posterUrl?: string; onReady: () => void; onOpen: (url: string, title: string) => void }) {
  const previewUrl = detail.videoPreviewUrl || detail.shot.videoPreviewUrl;
  if (!previewUrl) {
    return (
      <div className="group relative overflow-hidden rounded-md bg-black" style={{ aspectRatio: cssAspectRatio(detail.aspectRatio) }}>
        <div className="grid h-full place-items-center"><Video className="size-10 text-white/30" /></div>
      </div>
    );
  }
  return (
    <VideoPlayer
      key={previewUrl}
      url={previewUrl}
      posterUrl={posterUrl || detail.imagePreviewUrl || detail.shot.imagePreviewUrl}
      title={detail.shot.title || "分镜视频"}
      aspectRatio={detail.aspectRatio}
      onReady={onReady}
      onOpen={onOpen}
    />
  );
}

function VideoPlayer({ url, posterUrl, title, aspectRatio, onReady, onOpen }: { url: string; posterUrl?: string; title: string; aspectRatio: string; onReady: () => void; onOpen: (url: string, title: string) => void }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");

  const retry = () => {
    setLoadState("loading");
    videoRef.current?.load();
  };
  const markReady = () => {
    setLoadState("ready");
    onReady();
  };

  return (
    <div className="group relative overflow-hidden rounded-md bg-black" style={{ aspectRatio: cssAspectRatio(aspectRatio) }}>
      <video
        ref={videoRef}
        className="h-full w-full object-contain"
        controls
        playsInline
        preload="metadata"
        poster={posterUrl}
        src={url}
        onLoadedMetadata={markReady}
        onCanPlay={markReady}
        onError={() => {
          setLoadState("error");
          onReady();
        }}
      />
      {loadState === "loading" ? (
        <div className="pointer-events-none absolute inset-0 grid place-items-center bg-black/35 text-sm text-white">
          <span className="flex items-center gap-2"><Loader2 className="size-5 animate-spin" />正在加载视频</span>
        </div>
      ) : null}
      {loadState === "error" ? (
        <div className="absolute inset-0 grid place-items-center bg-black/75 text-white">
          <div className="grid justify-items-center gap-3 text-sm">
            <span>视频加载失败</span>
            <Button type="button" size="sm" variant="secondary" onClick={retry}><RefreshCw />重新加载</Button>
          </div>
        </div>
      ) : null}
      <button type="button" className="absolute right-3 top-3 grid size-9 place-items-center rounded-full bg-black/65 text-white opacity-0 transition-opacity group-hover:opacity-100" onClick={() => onOpen(url, title)} aria-label="全屏查看视频">
        <Maximize2 className="size-4" />
      </button>
    </div>
  );
}

function preserveVideoPreviewUrls(previous: StoryboardShotDetail | undefined, next: StoryboardShotDetail) {
  if (!previous || previous.shot.id !== next.shot.id) return next;
  const sameArtifact = previous.shot.videoArtifactId === next.shot.videoArtifactId;
  const previousRuns = new Map(previous.videoGenerationRuns.map((run) => [run.providerCallId, run]));
  return {
    ...next,
    videoPreviewUrl: sameArtifact
      ? retainUsableSignedMediaUrl(previous.videoPreviewUrl, next.videoPreviewUrl)
      : next.videoPreviewUrl,
    shot: {
      ...next.shot,
      videoPreviewUrl: sameArtifact
        ? retainUsableSignedMediaUrl(previous.shot.videoPreviewUrl, next.shot.videoPreviewUrl)
        : next.shot.videoPreviewUrl,
    },
    videoGenerationRuns: next.videoGenerationRuns.map((run) => {
      const previousRun = previousRuns.get(run.providerCallId);
      return previousRun
        ? { ...run, previewUrl: retainUsableSignedMediaUrl(previousRun.previewUrl, run.previewUrl) }
        : run;
    }),
  };
}

function RenderPlanPanel({
  plan,
  loading,
  creating,
  onCreate,
  verifying,
  onVerify,
  reviews,
  reviewsLoading,
  reviewing,
  onReview,
  onOpen,
}: {
  plan?: VideoRenderPlan;
  loading: boolean;
  creating: boolean;
  onCreate: () => void;
  verifying: boolean;
  onVerify: (decision: "approve" | "reject") => void;
  reviews: NativeAudioReview[];
  reviewsLoading: boolean;
  reviewing: boolean;
  onReview: () => void;
  onOpen: (url: string, title: string) => void;
}) {
  return (
    <section className="mt-6 border-t pt-5">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold">视频执行计划</h3>
          {plan ? <p className="mt-1 text-xs text-muted-foreground">{plan.modelFamily} · {plan.variantKey} · {plan.resolution}</p> : null}
        </div>
        <Button type="button" size="sm" variant="outline" onClick={onCreate} disabled={creating || plan?.status === "running"}>
          {creating ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          {plan ? "重新规划" : "生成计划"}
        </Button>
        {plan ? (
          <Button type="button" size="sm" variant="outline" onClick={onReview} disabled={reviewing || !plan.segments.some((segment) => segment.nativeAudioDetected)}>
            {reviewing ? <Loader2 className="animate-spin" /> : <ShieldAlert />}
            自动审核音轨
          </Button>
        ) : null}
      </div>
      {loading ? <Skeleton className="h-28" /> : plan ? (
        <div className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Badge variant="outline">{statusLabel(plan.status)}</Badge>
            <Badge variant="outline">{plan.targetDurationSeconds.toFixed(2)} 秒 / {plan.targetDurationFrames} 帧</Badge>
            <Badge variant={plan.productionReadiness === "ready" ? "default" : "secondary"}>{statusLabel(plan.productionReadiness)}</Badge>
            <Badge variant={plan.nativeAudioStatus === "audio_verified" ? "default" : "outline"}>{statusLabel(plan.nativeAudioStatus)}</Badge>
            {plan.nativeAudioStatus === "audio_unverified" ? (
              <>
                <Button type="button" size="sm" onClick={() => onVerify("approve")} disabled={verifying}>
                  {verifying ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
                  通过音轨
                </Button>
                <Button type="button" size="sm" variant="outline" onClick={() => onVerify("reject")} disabled={verifying}>
                  <ShieldAlert />
                  退回重试
                </Button>
              </>
            ) : null}
          </div>
          <div className="grid gap-2">
            {plan.segments.map((segment) => (
              <div key={segment.id} className="rounded-md border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium">片段 {segment.segmentIndex + 1}</span>
                    <Badge variant={segment.status === "succeeded" ? "default" : segment.status === "failed" ? "destructive" : "outline"}>{statusLabel(segment.status)}</Badge>
                    <Badge variant="outline">叙事 {segment.plannedDurationSeconds.toFixed(2)} 秒</Badge>
                    <Badge variant="outline">请求 {segment.requestedDurationSeconds} 秒</Badge>
                  </div>
                  {segment.previewUrl ? (
                    <Button type="button" size="sm" variant="ghost" onClick={() => onOpen(segment.previewUrl!, `执行片段 ${segment.segmentIndex + 1}`)}>
                      <Video className="h-3.5 w-3.5" />
                      预览
                    </Button>
                  ) : null}
                </div>
                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>续接：{statusLabel(segment.continuityMode)}</span>
                  <span>音频：{statusLabel(segment.audioVerificationStatus)}</span>
                  <span>就绪：{statusLabel(segment.productionReadiness)}</span>
                  {segment.trimEndTick ? <span>尾部安全裁剪</span> : null}
                  {segment.retryGeneration > 0 ? <span>重试 {segment.retryGeneration} 次</span> : null}
                </div>
                {segment.errorMessage ? <p className="mt-2 text-xs text-destructive">{segment.errorMessage}</p> : null}
              </div>
            ))}
          </div>
          {reviewsLoading ? <Skeleton className="h-20" /> : reviews.length > 0 ? <NativeAudioReviewList reviews={reviews} /> : null}
        </div>
      ) : (
        <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">尚未生成视频执行计划</div>
      )}
    </section>
  );
}

function NativeAudioReviewList({ reviews }: { reviews: NativeAudioReview[] }) {
  const latestBySegment = new Map<string, NativeAudioReview>();
  for (const review of reviews) {
    const current = latestBySegment.get(review.videoRenderSegmentId);
    if (!current || review.revision > current.revision) latestBySegment.set(review.videoRenderSegmentId, review);
  }
  return (
    <div className="space-y-2 border-t pt-3">
      <div className="text-sm font-medium">音轨自动审核</div>
      {[...latestBySegment.values()].map((review, index) => (
        <div key={review.id} className="rounded-md border p-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">片段 {index + 1}</span>
            <Badge variant={review.status === "passed" ? "default" : review.status === "failed" ? "destructive" : "outline"}>{statusLabel(review.status)}</Badge>
            {review.dialogueCoverage !== undefined ? <Badge variant="outline">对白覆盖 {(review.dialogueCoverage * 100).toFixed(0)}%</Badge> : null}
            {review.textAccuracy !== undefined ? <Badge variant="outline">文本准确 {(review.textAccuracy * 100).toFixed(0)}%</Badge> : null}
            {review.timingAccuracy !== undefined ? <Badge variant="outline">时长准确 {(review.timingAccuracy * 100).toFixed(0)}%</Badge> : null}
            {review.speakerTurnAccuracy !== undefined ? <Badge variant="outline">说话人 {(review.speakerTurnAccuracy * 100).toFixed(0)}%</Badge> : null}
          </div>
          {review.transcript ? <div className="mt-2 text-muted-foreground">{review.transcript}</div> : null}
          {review.errorMessage ? <div className="mt-2 text-destructive">{review.errorMessage}</div> : null}
        </div>
      ))}
    </div>
  );
}

function VideoGenerationHistory({ detail, onOpen }: { detail: StoryboardShotDetail; onOpen: (url: string, title: string) => void }) {
  return (
    <section className="mt-6 border-t pt-5">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">生成记录</h3>
        <Badge variant="secondary">{detail.videoGenerationRuns.length}</Badge>
      </div>
      {detail.videoGenerationRuns.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">暂无生成记录</div>
      ) : (
        <div className="grid gap-2">
          {detail.videoGenerationRuns.map((run, index) => (
            <div key={run.providerCallId} className="grid grid-cols-[112px_1fr] gap-3 rounded-md border p-2">
              <button type="button" className="relative h-20 overflow-hidden rounded bg-black" disabled={!run.previewUrl} onClick={() => run.previewUrl && onOpen(run.previewUrl, `视频版本 ${detail.videoGenerationRuns.length - index}`)}>
                <span className="grid h-full place-items-center"><Film className="size-5 text-white/35" /></span>
                {run.previewUrl ? <span className="absolute inset-0 grid place-items-center"><Video className="size-5 text-white drop-shadow" /></span> : null}
              </button>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={run.status === "succeeded" ? "default" : run.status === "failed" ? "destructive" : "outline"}>{statusLabel(run.status)}</Badge>
                  <span className="truncate text-xs text-muted-foreground">{run.modelName || run.modelId || "未记录模型"}</span>
                </div>
                <div className="mt-2 text-xs text-muted-foreground">{formatGenerationTime(run.completedAt || run.startedAt)}</div>
                {run.errorMessage ? <p className="mt-2 line-clamp-2 text-xs text-destructive">{run.errorMessage}</p> : null}
                <details className="mt-2 text-xs">
                  <summary className="cursor-pointer text-muted-foreground">查看提示词与任务信息</summary>
                  {run.prompt ? <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded bg-muted/50 p-2 text-foreground">{run.prompt}{run.promptTruncated ? "\n\n[内容过长，详情中已截断]" : ""}</pre> : null}
                  <div className="mt-2 whitespace-pre-wrap break-all text-muted-foreground">
                    {`调用：${run.providerCallId}${run.providerAsyncTaskId ? `\n任务：${run.providerAsyncTaskId}` : ""}${run.externalTaskId ? `\n上游任务：${run.externalTaskId}` : ""}${run.promptHash ? `\n哈希：${run.promptHash}` : ""}`}
                  </div>
                </details>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function draftFromDetail(detail: StoryboardShotDetail): VideoDraft {
  return {
    shotId: detail.shot.id,
    promptRevision: promptRevision(detail),
    videoPrompt: detail.shot.videoPrompt ?? "",
    referenceMode: normalizeReferenceMode(detail.shot.videoReferenceMode),
    referenceKeys: detail.shot.videoReferenceKeys ?? [],
  };
}

function promptRevision(detail: StoryboardShotDetail) {
  return detail.shot.videoPromptUpdatedAt ?? detail.shot.videoPrompt ?? "";
}

function videoUpdateBody(draft: VideoDraft): UpdateStoryboardShotRequest {
  return {
    videoPrompt: draft.videoPrompt,
    videoReferenceMode: draft.referenceMode,
    videoReferenceKeys: draft.referenceMode === "custom" ? draft.referenceKeys : [],
  };
}

function formatShotDuration(durationSeconds?: number) {
  if (!Number.isFinite(durationSeconds) || !durationSeconds || durationSeconds <= 0) return "未规划";
  return `${Number.isInteger(durationSeconds) ? durationSeconds : durationSeconds.toFixed(2)} 秒`;
}

function videoReferenceSourceLabel(option: StoryboardShotVideoReferenceOption) {
  if (option.sourceType === "shot_image") return "当前镜头首帧";
  if (option.sourceType === "derived_asset") return "镜头衍生图";
  if (option.sourceType === "asset_primary") return "资产当前主图";
  return "资产参考图";
}

function DetailRow({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return <div className="grid gap-1 sm:grid-cols-[56px_1fr]"><span className="text-muted-foreground">{label}</span><span>{value}</span></div>;
}
