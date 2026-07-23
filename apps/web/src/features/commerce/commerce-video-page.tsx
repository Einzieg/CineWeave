"use client";

import NextImage from "next/image";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  Captions,
  Check,
  Film,
  Image as ImageIcon,
  Loader2,
  MessageSquareText,
  Music2,
  Play,
  RefreshCcw,
  Sparkles,
  Volume2,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { studioApi } from "@/lib/api-client";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { cn } from "@/lib/utils";
import type {
  CommerceProductionRun,
  CommerceProductionRunDetail,
  CommerceProductionRunType,
  CommerceStoryboardPlanDetail,
  CommerceStoryboardShot,
  JsonValue,
} from "@/lib/types";

const activeRunStatuses = new Set(["queued", "running", "cancelling"]);

type GenerateRequest = {
  shotIds: string[];
  force: boolean;
};

export function CommerceVideoPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const [selectedUnitId, setSelectedUnitId] = useState("");
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [selection, setSelection] = useState<{ planId: string; ids: Set<string> }>({ planId: "", ids: new Set() });
  const [detailShot, setDetailShot] = useState<CommerceStoryboardShot | null>(null);
  const [submittingPromptShotIds, setSubmittingPromptShotIds] = useState<Set<string>>(new Set());
  const [submittingVideoShotIds, setSubmittingVideoShotIds] = useState<Set<string>>(new Set());
  const notifiedRuns = useRef(new Set<string>());
  const notificationScope = useRef("");

  const unitsQuery = useApiQuery({
    key: qk.commerceScriptUnits(projectId, "active", ""),
    queryFn: (session) => studioApi.listCommerceScriptUnits(session, projectId, { status: "active", limit: 100 }),
  });
  const units = useMemo(() => unitsQuery.data?.items ?? [], [unitsQuery.data?.items]);
  const activeUnitId = units.some((unit) => unit.id === selectedUnitId) ? selectedUnitId : units[0]?.id ?? "";
  const activeUnit = units.find((unit) => unit.id === activeUnitId);

  const plansQuery = useApiQuery({
    key: qk.commerceStoryboardPlans(projectId, activeUnitId),
    queryFn: (session) => studioApi.listCommerceStoryboardPlans(session, projectId, activeUnitId),
    enabled: Boolean(activeUnitId),
  });
  const plans = useMemo(() => plansQuery.data?.items ?? [], [plansQuery.data?.items]);
  const preferredPlan = plans.find((plan) => plan.active) ?? plans[0];
  const activePlanId = plans.some((plan) => plan.id === selectedPlanId) ? selectedPlanId : preferredPlan?.id ?? "";

  const detailQuery = useApiQuery({
    key: qk.commerceStoryboardPlan(projectId, activeUnitId, activePlanId),
    queryFn: (session) => studioApi.getCommerceStoryboardPlan(session, projectId, activeUnitId, activePlanId),
    enabled: Boolean(activeUnitId && activePlanId),
  });
  const detail = detailQuery.data;
  const selectedShotIds = useMemo(() => {
    if (!detail) return new Set<string>();
    if (selection.planId !== detail.plan.id) return new Set(detail.shots.map((shot) => shot.id));
    const available = new Set(detail.shots.map((shot) => shot.id));
    return new Set([...selection.ids].filter((shotId) => available.has(shotId)));
  }, [detail, selection]);
  const selectedShots = useMemo(
    () => detail?.shots.filter((shot) => selectedShotIds.has(shot.id)) ?? [],
    [detail?.shots, selectedShotIds],
  );

  const promptRunsQuery = useCommerceRunList(projectId, activeUnitId, "video_prompts");
  const videoRunsQuery = useCommerceRunList(projectId, activeUnitId, "shot_videos");
  const promptRuns = useMemo(() => promptRunsQuery.data?.items ?? [], [promptRunsQuery.data?.items]);
  const videoRuns = useMemo(() => videoRunsQuery.data?.items ?? [], [videoRunsQuery.data?.items]);
  const latestPromptRun = promptRuns.find((run) => activeRunStatuses.has(run.status)) ?? promptRuns[0];
  const latestVideoRun = videoRuns.find((run) => activeRunStatuses.has(run.status)) ?? videoRuns[0];
  const activePromptRun = promptRuns.find((run) => activeRunStatuses.has(run.status));
  const activeVideoRun = videoRuns.find((run) => activeRunStatuses.has(run.status));
  const promptRunDetailQuery = useCommerceRunDetail(projectId, latestPromptRun);
  const videoRunDetailQuery = useCommerceRunDetail(projectId, latestVideoRun);

  useEffect(() => {
    if (!activeUnitId || !promptRunsQuery.isFetched || !videoRunsQuery.isFetched) return;
    if (notificationScope.current !== activeUnitId) {
      notificationScope.current = activeUnitId;
      notifiedRuns.current = new Set(
        [...promptRuns, ...videoRuns]
          .filter((run) => !activeRunStatuses.has(run.status))
          .map((run) => run.id),
      );
      return;
    }
    for (const run of [...promptRuns, ...videoRuns]) {
      if (activeRunStatuses.has(run.status) || notifiedRuns.current.has(run.id)) continue;
      notifiedRuns.current.add(run.id);
      invalidate([
        qk.commerceStoryboardPlan(projectId, activeUnitId, activePlanId),
        qk.commerceProductionRun(projectId, run.id),
        qk.commerceProductionRuns(projectId, activeUnitId, run.runType),
        qk.workflowRuns(projectId),
      ]);
      const label = run.runType === "video_prompts" ? "视频提示词" : "镜头视频";
      if (run.status === "succeeded") toast.success(`${label}批次已完成`);
      else if (run.status === "partially_succeeded") toast.warning(`${label}批次部分完成，可重试失败镜头`);
      else if (run.status === "failed") toast.error(run.errorMessage || `${label}批次执行失败`);
    }
  }, [
    activePlanId,
    activeUnitId,
    invalidate,
    projectId,
    promptRuns,
    promptRunsQuery.isFetched,
    videoRuns,
    videoRunsQuery.isFetched,
  ]);

  const generatePrompts = useApiMutation<CommerceProductionRun, GenerateRequest>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, request) => studioApi.generateCommerceVideoPrompts(
      session,
      projectId,
      activeUnitId,
      videoBatchBody(detail, request),
      `commerce-video-prompts-${activeUnitId}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "video_prompts"),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("视频提示词任务已提交");
    },
    onError: (error) => toast.error(error.message),
    onMutate: (request) => setSubmittingPromptShotIds((current) => new Set([...current, ...request.shotIds])),
    onSettled: (_data, _error, request) => setSubmittingPromptShotIds((current) => withoutShotIds(current, request.shotIds)),
  });

  const generateVideos = useApiMutation<CommerceProductionRun, GenerateRequest>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, request) => studioApi.generateCommerceShotVideos(
      session,
      projectId,
      activeUnitId,
      videoBatchBody(detail, request),
      `commerce-shot-videos-${activeUnitId}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "shot_videos"),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("镜头视频任务已提交");
    },
    onError: (error) => toast.error(error.message),
    onMutate: (request) => setSubmittingVideoShotIds((current) => new Set([...current, ...request.shotIds])),
    onSettled: (_data, _error, request) => setSubmittingVideoShotIds((current) => withoutShotIds(current, request.shotIds)),
  });

  const retryRun = useApiMutation<CommerceProductionRun, CommerceProductionRun>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, run) => studioApi.retryFailedCommerceProductionRun(
      session,
      projectId,
      run.id,
      {},
      `commerce-video-retry-${run.id}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, run.runType),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("失败镜头已重新提交");
    },
    onError: (error) => toast.error(error.message),
  });

  const cancelRun = useApiMutation<CommerceProductionRunDetail, CommerceProductionRun>({
    requiredPermission: "workflow.cancel",
    mutationFn: (session, run) => studioApi.cancelCommerceProductionRun(session, projectId, run.id),
    onSuccess: ({ run }) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, run.runType),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("已请求取消生产批次");
    },
    onError: (error) => toast.error(error.message),
  });

  function submitMissingPrompts() {
    const shotIds = selectedShots
      .filter((shot) => shot.imageStatus === "succeeded" && shot.videoPromptStatus !== "succeeded")
      .map((shot) => shot.id);
    if (!shotIds.length) {
      toast.info("所选镜头没有待生成的视频提示词");
      return;
    }
    generatePrompts.mutate({ shotIds, force: false });
  }

  function submitExecutableVideos() {
    const shotIds = selectedShots
      .filter((shot) => shot.imageStatus === "succeeded" && shot.videoPromptStatus === "succeeded" && shot.videoStatus !== "succeeded")
      .map((shot) => shot.id);
    if (!shotIds.length) {
      toast.info("所选镜头没有待生成的可执行视频");
      return;
    }
    generateVideos.mutate({ shotIds, force: false });
  }

  const completedVideos = detail?.shots.filter((shot) => shot.videoStatus === "succeeded").length ?? 0;
  const approvedPrompts = detail?.shots.filter((shot) => shot.videoPromptStatus === "succeeded").length ?? 0;

  return (
    <div className="space-y-5 pb-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold">视频制作</h1>
          {activeUnit ? <p className="mt-1 truncate text-sm text-muted-foreground">脚本 {formatUnitNo(activeUnit.unitNo)} · {activeUnit.title}</p> : null}
        </div>
        {detail ? (
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <span>提示词 {approvedPrompts}/{detail.shots.length}</span>
            <span className="h-4 w-px bg-border" />
            <span>视频 {completedVideos}/{detail.shots.length}</span>
          </div>
        ) : null}
      </div>

      <div className="grid gap-3 md:grid-cols-[minmax(240px,1fr)_minmax(220px,320px)]">
        <div className="space-y-1.5">
          <Label>脚本单元</Label>
          <Select value={activeUnitId} onValueChange={(value) => { setSelectedUnitId(value); setSelectedPlanId(""); setDetailShot(null); }}>
            <SelectTrigger><SelectValue placeholder="选择脚本" /></SelectTrigger>
            <SelectContent>
              {units.map((unit) => <SelectItem key={unit.id} value={unit.id}>{formatUnitNo(unit.unitNo)} · {unit.title}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>分镜方案</Label>
          <Select value={activePlanId} onValueChange={setSelectedPlanId} disabled={!plans.length}>
            <SelectTrigger><SelectValue placeholder="暂无可用方案" /></SelectTrigger>
            <SelectContent>
              {plans.map((plan) => <SelectItem key={plan.id} value={plan.id}>版本 {plan.planRevision} · {plan.active ? "当前启用" : statusLabel(plan.status)}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
      </div>

      {unitsQuery.isLoading || plansQuery.isLoading || detailQuery.isLoading ? <VideoPageSkeleton /> : !units.length ? (
        <Surface><SectionTitle title="暂无广告脚本" /><div className="px-4 py-10 text-center text-sm text-muted-foreground">请先在商品与脚本页面创建广告脚本。</div></Surface>
      ) : !detail ? (
        <Surface><SectionTitle title="暂无分镜方案" /><div className="px-4 py-10 text-center text-sm text-muted-foreground">请先为当前脚本单元生成并启用分镜方案。</div></Surface>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3 border-y py-3 text-sm">
            <label className="inline-flex cursor-pointer items-center gap-2">
              <Checkbox
                checked={selectedShotIds.size === detail.shots.length && detail.shots.length > 0}
                onCheckedChange={(checked) => setSelection({
                  planId: detail.plan.id,
                  ids: checked ? new Set(detail.shots.map((shot) => shot.id)) : new Set(),
                })}
              />
              选择全部镜头
            </label>
            <span className="text-muted-foreground">已选 {selectedShotIds.size}/{detail.shots.length}</span>
            <div className="ml-auto flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={!detail.plan.active || selectedShotIds.size === 0 || Boolean(activePromptRun) || generatePrompts.isPending}
                onClick={submitMissingPrompts}
              >
                {generatePrompts.isPending ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
                生成缺失视频提示词
              </Button>
              <Button
                size="sm"
                disabled={!detail.plan.active || selectedShotIds.size === 0 || Boolean(activeVideoRun) || generateVideos.isPending}
                onClick={submitExecutableVideos}
              >
                {generateVideos.isPending ? <Loader2 className="size-4 animate-spin" /> : <Film className="size-4" />}
                生成可执行视频
              </Button>
            </div>
          </div>

          <div className="grid gap-3 xl:grid-cols-2">
            <CommerceRunStrip
              title="视频提示词任务"
              run={latestPromptRun}
              detail={promptRunDetailQuery.data}
              retrying={retryRun.isPending && retryRun.variables?.runType === "video_prompts"}
              cancelling={cancelRun.isPending && cancelRun.variables?.runType === "video_prompts"}
              onRetry={() => latestPromptRun && retryRun.mutate(latestPromptRun)}
              onCancel={() => latestPromptRun && cancelRun.mutate(latestPromptRun)}
            />
            <CommerceRunStrip
              title="镜头视频任务"
              run={latestVideoRun}
              detail={videoRunDetailQuery.data}
              retrying={retryRun.isPending && retryRun.variables?.runType === "shot_videos"}
              cancelling={cancelRun.isPending && cancelRun.variables?.runType === "shot_videos"}
              onRetry={() => latestVideoRun && retryRun.mutate(latestVideoRun)}
              onCancel={() => latestVideoRun && cancelRun.mutate(latestVideoRun)}
            />
          </div>

          <div className="divide-y border-y">
            {detail.shots.map((shot) => {
              const promptBusy = submittingPromptShotIds.has(shot.id);
              const videoBusy = submittingVideoShotIds.has(shot.id);
              const promptItem = promptRunDetailQuery.data?.items.find((item) => item.subject.storyboardShotId === shot.id);
              const videoItem = videoRunDetailQuery.data?.items.find((item) => item.subject.storyboardShotId === shot.id);
              return (
                <article key={shot.id} className="grid gap-4 py-4 lg:grid-cols-[24px_170px_170px_minmax(0,1fr)_auto]">
                  <Checkbox
                    className="mt-1"
                    checked={selectedShotIds.has(shot.id)}
                    aria-label={`选择镜头 ${shot.shotOrdinal}`}
                    onCheckedChange={(checked) => {
                      const next = new Set(selectedShotIds);
                      if (checked) next.add(shot.id); else next.delete(shot.id);
                      setSelection({ planId: detail.plan.id, ids: next });
                    }}
                  />
                  <ShotFirstFrame shot={shot} aspectRatio={detail.plan.aspectRatio} />
                  <ShotVideoPreview shot={shot} aspectRatio={detail.plan.aspectRatio} />
                  <div className="min-w-0 space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium">{formatUnitNo(activeUnit?.unitNo ?? 0)}-{String(shot.shotOrdinal).padStart(2, "0")}</span>
                      <span className="text-xs text-muted-foreground">{shot.durationSeconds} 秒</span>
                      <Badge variant="outline">提示词：{statusLabel(shot.videoPromptStatus)}</Badge>
                      <Badge variant="outline">执行计划：{shot.videoRenderPlanStatus ? statusLabel(shot.videoRenderPlanStatus) : "未创建"}</Badge>
                      <Badge variant="outline">视频：{statusLabel(shot.videoStatus)}</Badge>
                    </div>
                    <p className="line-clamp-2 text-sm font-medium">{shot.visualAction}</p>
                    {shot.voiceoverText ? <p className="line-clamp-2 text-sm text-muted-foreground">旁白：{shot.voiceoverText}</p> : null}
                    {shot.videoPrompt ? <p className="line-clamp-2 text-xs text-muted-foreground">提示词：{shot.videoPrompt}</p> : null}
                    {videoItem?.errorMessage || promptItem?.errorMessage || shot.videoErrorMessage ? (
                      <p className="text-xs text-destructive">{videoItem?.errorMessage || promptItem?.errorMessage || shot.videoErrorMessage}</p>
                    ) : null}
                  </div>
                  <div className="flex min-w-28 flex-col items-stretch gap-2">
                    <Button size="sm" variant="outline" onClick={() => setDetailShot(shot)}>查看详情</Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={shot.imageStatus !== "succeeded" || Boolean(activePromptRun) || promptBusy}
                      onClick={() => generatePrompts.mutate({ shotIds: [shot.id], force: shot.videoPromptStatus === "succeeded" })}
                    >
                      {promptBusy ? <Loader2 className="size-4 animate-spin" /> : <MessageSquareText className="size-4" />}
                      {shot.videoPromptStatus === "succeeded" ? "重生成提示词" : "生成提示词"}
                    </Button>
                    <Button
                      size="sm"
                      disabled={shot.videoPromptStatus !== "succeeded" || Boolean(activeVideoRun) || videoBusy}
                      onClick={() => generateVideos.mutate({ shotIds: [shot.id], force: shot.videoStatus === "succeeded" })}
                    >
                      {videoBusy ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
                      {shot.videoStatus === "succeeded" ? "重新生成视频" : "生成视频"}
                    </Button>
                  </div>
                </article>
              );
            })}
          </div>
        </>
      )}

      <Dialog open={Boolean(detailShot)} onOpenChange={(open) => { if (!open) setDetailShot(null); }}>
        <DialogContent className="h-[90vh] grid-rows-[auto_minmax(0,1fr)] overflow-hidden p-0 sm:max-w-7xl">
          <DialogHeader className="border-b px-5 py-4"><DialogTitle>镜头 {detailShot?.shotOrdinal} 生产详情</DialogTitle></DialogHeader>
          <ScrollArea className="min-h-0">
            {detailShot ? <ShotDetail shot={detailShot} aspectRatio={detail?.plan.aspectRatio ?? "9:16"} /> : null}
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function useCommerceRunList(projectId: string, scriptUnitId: string, runType: CommerceProductionRunType) {
  return useApiQuery({
    key: qk.commerceProductionRuns(projectId, scriptUnitId, runType),
    queryFn: (session) => studioApi.listCommerceProductionRuns(session, projectId, { scriptUnitId, runType, limit: 20 }),
    enabled: Boolean(scriptUnitId),
    refetchInterval: (query) => query.state.data?.items.some((run) => activeRunStatuses.has(run.status)) ? 2000 : false,
  });
}

function useCommerceRunDetail(projectId: string, run?: CommerceProductionRun) {
  return useApiQuery({
    key: qk.commerceProductionRun(projectId, run?.id ?? ""),
    queryFn: (session) => studioApi.getCommerceProductionRun(session, projectId, run?.id ?? ""),
    enabled: Boolean(run?.id),
    refetchInterval: run && activeRunStatuses.has(run.status) ? 2000 : false,
  });
}

function videoBatchBody(detail: CommerceStoryboardPlanDetail | undefined, request: GenerateRequest) {
  return {
    planId: detail?.plan.id ?? "",
    expectedPlanRevision: detail?.plan.revision ?? 0,
    expectedUnitGenerationId: detail?.plan.scriptUnitGenerationId ?? "",
    shotIds: request.shotIds,
    force: request.force,
    concurrency: 5,
    resolution: "1080p",
  };
}

function CommerceRunStrip({
  title,
  run,
  detail,
  retrying,
  cancelling,
  onRetry,
  onCancel,
}: {
  title: string;
  run?: CommerceProductionRun;
  detail?: CommerceProductionRunDetail;
  retrying: boolean;
  cancelling: boolean;
  onRetry: () => void;
  onCancel: () => void;
}) {
  if (!run) return <div className="border-y px-3 py-4 text-sm text-muted-foreground">{title}：暂无任务</div>;
  const settled = run.completedItems + run.failedItems + run.cancelledItems;
  const failed = detail?.items.filter((item) => item.status === "failed_retryable" || item.status === "failed_terminal") ?? [];
  return (
    <div className="space-y-2 border-y px-3 py-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        {activeRunStatuses.has(run.status) ? <Loader2 className="size-4 animate-spin text-primary" /> : run.status === "succeeded" ? <Check className="size-4 text-emerald-600" /> : <XCircle className="size-4 text-destructive" />}
        <span className="font-medium">{title}</span>
        <Badge variant="outline">{statusLabel(run.status)}</Badge>
        <span className="text-muted-foreground">{settled}/{run.totalItems}</span>
        <div className="ml-auto flex gap-1">
          {(run.status === "failed" || run.status === "partially_succeeded") ? (
            <Button size="sm" variant="outline" disabled={retrying} onClick={onRetry}>
              {retrying ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
              重试失败项
            </Button>
          ) : null}
          {activeRunStatuses.has(run.status) ? (
            <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={cancelling} onClick={onCancel}>
              {cancelling ? <Loader2 className="size-4 animate-spin" /> : <XCircle className="size-4" />}
              取消
            </Button>
          ) : null}
        </div>
      </div>
      {failed.length ? (
        <details className="text-xs text-destructive">
          <summary className="cursor-pointer">查看 {failed.length} 个失败项</summary>
          <div className="mt-2 space-y-1">
            {failed.map((item) => <p key={item.id}>{item.errorMessage || item.errorCode || "任务步骤执行失败"}</p>)}
          </div>
        </details>
      ) : null}
    </div>
  );
}

function ShotFirstFrame({ shot, aspectRatio }: { shot: CommerceStoryboardShot; aspectRatio: string }) {
  return (
    <div className="relative overflow-hidden border bg-muted/40" style={{ aspectRatio: cssAspectRatio(aspectRatio) }}>
      {shot.imagePreviewUrl ? <NextImage src={shot.imagePreviewUrl} alt={`镜头 ${shot.shotOrdinal} 首帧`} fill unoptimized className="object-cover" sizes="150px" /> : <ImageIcon className="absolute inset-0 m-auto size-6 text-muted-foreground" />}
      <span className="absolute bottom-1 left-1 bg-background/85 px-1.5 py-0.5 text-[11px]">首帧</span>
    </div>
  );
}

function ShotVideoPreview({ shot, aspectRatio }: { shot: CommerceStoryboardShot; aspectRatio: string }) {
  return (
    <div className="relative overflow-hidden border bg-black" style={{ aspectRatio: cssAspectRatio(aspectRatio) }}>
      {shot.videoPreviewUrl ? (
        <video key={shot.videoPreviewUrl} src={shot.videoPreviewUrl} poster={shot.imagePreviewUrl} controls preload="metadata" className="h-full w-full object-contain" />
      ) : (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-1 text-xs text-white/65"><Film className="size-6" />{statusLabel(shot.videoStatus)}</div>
      )}
    </div>
  );
}

function ShotDetail({ shot, aspectRatio }: { shot: CommerceStoryboardShot; aspectRatio: string }) {
  return (
    <div className="grid gap-6 p-5 lg:grid-cols-[minmax(280px,0.9fr)_minmax(0,1.1fr)]">
      <div className="space-y-3">
        <ShotVideoPreview shot={shot} aspectRatio={aspectRatio} />
        <ShotFirstFrame shot={shot} aspectRatio={aspectRatio} />
      </div>
      <div className="min-w-0 space-y-5">
        <div className="flex flex-wrap gap-2"><Badge variant="outline">提示词：{statusLabel(shot.videoPromptStatus)}</Badge><Badge variant="outline">执行计划：{shot.videoRenderPlanStatus ? statusLabel(shot.videoRenderPlanStatus) : "未创建"}</Badge><Badge variant="outline">视频：{statusLabel(shot.videoStatus)}</Badge><Badge variant="secondary">{shot.durationSeconds} 秒</Badge></div>
        <DetailField icon={<MessageSquareText className="size-4" />} label="审核后视频提示词" value={shot.videoPrompt || "尚未生成"} />
        <DetailField icon={<Volume2 className="size-4" />} label="逐字旁白" value={shot.voiceoverText || "无旁白"} />
        <DetailField icon={<Captions className="size-4" />} label="后期屏幕文字" value={shot.onscreenText || "无屏幕文字"} />
        <DetailField icon={<Music2 className="size-4" />} label="音效与音乐" value={[formatJsonText(shot.soundEffects), shot.musicCue].filter(Boolean).join("\n") || "无音效或音乐要求"} />
        <div className="space-y-1.5"><p className="text-sm font-medium">画面动作</p><p className="whitespace-pre-wrap text-sm text-muted-foreground">{shot.visualAction}</p></div>
        {shot.videoErrorMessage ? <p className="text-sm text-destructive">{shot.videoErrorMessage}</p> : null}
      </div>
    </div>
  );
}

function DetailField({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return <div className="space-y-1.5"><p className="flex items-center gap-2 text-sm font-medium">{icon}{label}</p><p className="whitespace-pre-wrap text-sm text-muted-foreground">{value}</p></div>;
}

function formatJsonText(value: JsonValue): string {
  if (value === null || value === undefined || value === "") return "";
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map((item) => formatJsonText(item)).filter(Boolean).join("、");
  if (typeof value === "object") return Object.values(value).map((item) => formatJsonText(item)).filter(Boolean).join("、");
  return String(value);
}

function formatUnitNo(value: number) {
  return String(value || 0).padStart(2, "0");
}

function cssAspectRatio(value: string) {
  const [width, height] = value.split(":").map(Number);
  return width > 0 && height > 0 ? `${width} / ${height}` : "9 / 16";
}

function withoutShotIds(current: Set<string>, shotIds: string[]) {
  const next = new Set(current);
  for (const shotId of shotIds) next.delete(shotId);
  return next;
}

function VideoPageSkeleton() {
  return <div className="space-y-3">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className={cn("h-44 w-full", index === 0 && "h-20")} />)}</div>;
}
