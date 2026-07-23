"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  Download,
  Film,
  Loader2,
  Play,
  RefreshCcw,
  Save,
  Sparkles,
} from "lucide-react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { cn } from "@/lib/utils";
import type {
  CommerceFinalVideoVersion,
  CommerceProductionRun,
  CommerceTimeline,
  CommerceTimelineOverlay,
  CommerceUnitProductionStatus,
} from "@/lib/types";

const activeRunStatuses = new Set(["queued", "running", "cancelling"]);

export function CommerceFinalPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const [selectedUnitId, setSelectedUnitId] = useState("");
  const [selectedTimelineId, setSelectedTimelineId] = useState("");
  const [overlayDrafts, setOverlayDrafts] = useState<CommerceTimelineOverlay[]>([]);
  const hydratedTimelineRevision = useRef("");
  const notifiedRuns = useRef(new Set<string>());
  const notificationScope = useRef("");

  const unitsQuery = useApiQuery({
    key: qk.commerceScriptUnits(projectId, "active", ""),
    queryFn: (session) => studioApi.listCommerceScriptUnits(session, projectId, {
      status: "active",
      limit: 100,
      includeProductionSummary: true,
    }),
  });
  const units = useMemo(() => unitsQuery.data?.items ?? [], [unitsQuery.data?.items]);
  const activeUnitId = units.some((item) => item.id === selectedUnitId) ? selectedUnitId : units[0]?.id ?? "";
  const activeUnit = units.find((item) => item.id === activeUnitId);

  const statusQuery = useApiQuery({
    key: qk.commerceUnitProductionStatus(projectId, activeUnitId),
    queryFn: (session) => studioApi.getCommerceUnitProductionStatus(session, projectId, activeUnitId),
    enabled: Boolean(activeUnitId),
  });
  const status = statusQuery.data;

  const plansQuery = useApiQuery({
    key: qk.commerceStoryboardPlans(projectId, activeUnitId),
    queryFn: (session) => studioApi.listCommerceStoryboardPlans(session, projectId, activeUnitId),
    enabled: Boolean(activeUnitId),
  });
  const activePlan = plansQuery.data?.items.find((item) => item.active) ?? plansQuery.data?.items[0];

  const timelinesQuery = useApiQuery({
    key: qk.commerceTimelines(projectId, activeUnitId),
    queryFn: (session) => studioApi.listCommerceTimelines(session, projectId, activeUnitId),
    enabled: Boolean(activeUnitId),
  });
  const timelines = useMemo(() => timelinesQuery.data?.items ?? [], [timelinesQuery.data?.items]);
  const preferredTimeline = timelines.find((item) => item.status === "active") ?? timelines[0];
  const activeTimelineId = timelines.some((item) => item.id === selectedTimelineId)
    ? selectedTimelineId
    : preferredTimeline?.id ?? "";
  const activeTimeline = timelines.find((item) => item.id === activeTimelineId);

  const timelineQuery = useApiQuery({
    key: qk.commerceTimeline(projectId, activeUnitId, activeTimelineId),
    queryFn: (session) => studioApi.getCommerceTimeline(session, projectId, activeUnitId, activeTimelineId),
    enabled: Boolean(activeUnitId && activeTimelineId),
  });

  const finalVideosQuery = useApiQuery({
    key: qk.commerceFinalVideos(projectId, activeUnitId),
    queryFn: (session) => studioApi.listCommerceFinalVideos(session, projectId, activeUnitId),
    enabled: Boolean(activeUnitId),
  });
  const finalVideos = useMemo(() => finalVideosQuery.data?.items ?? [], [finalVideosQuery.data?.items]);

  const runsQuery = useApiQuery({
    key: qk.commerceProductionRuns(projectId, activeUnitId, "final_compose"),
    queryFn: (session) => studioApi.listCommerceProductionRuns(session, projectId, {
      scriptUnitId: activeUnitId,
      runType: "final_compose",
      limit: 20,
    }),
    enabled: Boolean(activeUnitId),
    refetchInterval: (query) => query.state.data?.items.some((run) => activeRunStatuses.has(run.status)) ? 2000 : false,
  });
  const runs = useMemo(() => runsQuery.data?.items ?? [], [runsQuery.data?.items]);
  const activeRun = runs.find((run) => activeRunStatuses.has(run.status));
  const latestRun = activeRun ?? runs[0];

  useEffect(() => {
    const detail = timelineQuery.data;
    if (!detail) return;
    const identity = `${detail.timeline.id}:${detail.timeline.revision}`;
    if (hydratedTimelineRevision.current === identity) return;
    hydratedTimelineRevision.current = identity;
    setOverlayDrafts(detail.overlays.map((overlay) => ({ ...overlay, style: { ...overlay.style } })));
  }, [timelineQuery.data]);

  useEffect(() => {
    if (!activeUnitId || !runsQuery.isFetched) return;
    if (notificationScope.current !== activeUnitId) {
      notificationScope.current = activeUnitId;
      notifiedRuns.current = new Set(
        runs
          .filter((run) => !activeRunStatuses.has(run.status))
          .map((run) => run.id),
      );
      return;
    }
    for (const run of runs) {
      if (activeRunStatuses.has(run.status) || notifiedRuns.current.has(run.id)) continue;
      notifiedRuns.current.add(run.id);
      invalidate([
        qk.commerceProductionRun(projectId, run.id),
        qk.commerceProductionRuns(projectId, activeUnitId, "final_compose"),
        qk.commerceTimelines(projectId, activeUnitId),
        qk.commerceFinalVideos(projectId, activeUnitId),
        qk.commerceUnitProductionStatus(projectId, activeUnitId),
        qk.commerceScriptUnitsRoot(projectId),
        qk.workflowRuns(projectId),
      ]);
      if (run.status === "succeeded") toast.success("成片合成已完成");
      else if (run.status === "failed") toast.error(run.errorMessage || "成片合成失败");
      else if (run.status === "cancelled") toast.info("成片合成已取消");
    }
  }, [activeUnitId, invalidate, projectId, runs, runsQuery.isFetched]);

  const prepareTimeline = useApiMutation<CommerceTimeline, void>({
    requiredPermission: "project.write",
    mutationFn: (session) => {
      if (!activeUnit?.activeUnitGenerationId || !activePlan) throw new Error("当前脚本单元尚未准备好分镜方案");
      return studioApi.prepareCommerceTimeline(session, projectId, activeUnitId, {
        storyboardPlanId: activePlan.id,
        expectedPlanRevision: activePlan.planRevision,
        expectedUnitGenerationId: activeUnit.activeUnitGenerationId,
        title: `${formatUnitNo(activeUnit.unitNo)} ${activeUnit.title}`,
        resolution: "1080p",
      }, `commerce-timeline-${activeUnitId}-${activePlan.id}-${crypto.randomUUID()}`);
    },
    onSuccess: (timeline) => {
      setSelectedTimelineId(timeline.id);
      hydratedTimelineRevision.current = "";
      invalidate([
        qk.commerceTimelines(projectId, activeUnitId),
        qk.commerceUnitProductionStatus(projectId, activeUnitId),
      ]);
      toast.success("成片时间线已准备");
    },
    onError: (error) => toast.error(error.message),
  });

  const saveTimeline = useApiMutation<CommerceTimeline, void>({
    requiredPermission: "project.write",
    mutationFn: (session) => {
      const timeline = timelineQuery.data?.timeline;
      if (!timeline) throw new Error("当前没有可保存的时间线");
      return studioApi.updateCommerceTimeline(session, projectId, activeUnitId, timeline.id, {
        expectedRevision: timeline.revision,
        overlays: overlayDrafts.map((overlay) => ({
          timelineClipId: overlay.timelineClipId,
          storyboardShotId: overlay.storyboardShotId,
          role: overlay.role,
          ordinal: overlay.ordinal,
          text: overlay.text,
          startTick: overlay.startTick,
          endTick: overlay.endTick,
          style: overlay.style,
        })),
      });
    },
    onSuccess: (timeline) => {
      hydratedTimelineRevision.current = "";
      invalidate([
        qk.commerceTimeline(projectId, activeUnitId, timeline.id),
        qk.commerceTimelines(projectId, activeUnitId),
        qk.commerceFinalVideos(projectId, activeUnitId),
        qk.commerceUnitProductionStatus(projectId, activeUnitId),
      ]);
      toast.success("屏幕文字已保存");
    },
    onError: (error) => toast.error(error.message),
  });

  const composeFinal = useApiMutation<CommerceProductionRun, void>({
    requiredPermission: "workflow.run",
    mutationFn: (session) => {
      const timeline = timelineQuery.data?.timeline;
      if (!timeline || !activeUnit?.activeUnitGenerationId) throw new Error("当前没有可合成的时间线");
      return studioApi.composeCommerceFinalVideo(session, projectId, activeUnitId, {
        timelineId: timeline.id,
        expectedTimelineRevision: timeline.revision,
        expectedUnitGenerationId: activeUnit.activeUnitGenerationId,
        title: `${formatUnitNo(activeUnit.unitNo)} ${activeUnit.title}`,
        resolution: timeline.resolution,
      }, `commerce-final-${activeUnitId}-${timeline.id}-${crypto.randomUUID()}`);
    },
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "final_compose"),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("成片合成任务已提交");
    },
    onError: (error) => toast.error(error.message),
  });

  const retryFinal = useApiMutation<CommerceProductionRun, CommerceProductionRun>({
    requiredPermission: "workflow.run",
    mutationFn: (session, run) => studioApi.retryFailedCommerceProductionRun(
      session,
      projectId,
      run.id,
      {},
      `commerce-final-retry-${run.id}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "final_compose"),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("成片合成已重新提交");
    },
    onError: (error) => toast.error(error.message),
  });

  const activateFinal = useApiMutation<{ activated: boolean }, CommerceFinalVideoVersion>({
    requiredPermission: "project.write",
    mutationFn: (session, version) => studioApi.activateCommerceFinalVideo(session, projectId, activeUnitId, version.id),
    onSuccess: () => {
      invalidate([
        qk.commerceFinalVideos(projectId, activeUnitId),
        qk.commerceUnitProductionStatus(projectId, activeUnitId),
        qk.commerceScriptUnitsRoot(projectId),
      ]);
      toast.success("当前成片已切换");
    },
    onError: (error) => toast.error(error.message),
  });

  const allVideosReady = Boolean(activePlan && status && status.stages.shotVideos.total > 0 &&
    status.stages.shotVideos.succeeded === status.stages.shotVideos.total);
  const isLoading = unitsQuery.isLoading || (activeUnitId && (statusQuery.isLoading || plansQuery.isLoading || timelinesQuery.isLoading));

  return (
    <div className="space-y-5 pb-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">成片</h1>
          {activeUnit ? <p className="mt-1 text-sm text-muted-foreground">{formatUnitNo(activeUnit.unitNo)} · {activeUnit.title}</p> : null}
        </div>
        {status ? <UnitStatus status={status} /> : null}
      </div>

      <div className="max-w-xl space-y-1.5">
        <Label>脚本单元</Label>
        <Select value={activeUnitId} onValueChange={(value) => {
          setSelectedUnitId(value);
          setSelectedTimelineId("");
          hydratedTimelineRevision.current = "";
          setOverlayDrafts([]);
        }}>
          <SelectTrigger><SelectValue placeholder="选择脚本" /></SelectTrigger>
          <SelectContent>
            {units.map((unit) => (
              <SelectItem key={unit.id} value={unit.id}>
                {formatUnitNo(unit.unitNo)} · {unit.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isLoading ? <FinalPageSkeleton /> : !units.length ? (
        <Surface>
          <SectionTitle title="暂无广告脚本" />
          <div className="px-4 py-10 text-center text-sm text-muted-foreground">请先创建广告脚本。</div>
        </Surface>
      ) : (
        <>
          <Surface>
            <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
              <div>
                <h2 className="text-sm font-semibold">成片时间线</h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  {activeTimeline ? `版本 ${activeTimeline.revision} · ${activeTimeline.aspectRatio} · ${activeTimeline.resolution}` : "尚未准备"}
                </p>
              </div>
              <div className="flex items-center gap-2">
                {timelines.length > 1 ? (
                  <Select value={activeTimelineId} onValueChange={(value) => {
                    setSelectedTimelineId(value);
                    hydratedTimelineRevision.current = "";
                  }}>
                    <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {timelines.map((timeline) => (
                        <SelectItem key={timeline.id} value={timeline.id}>版本 {timeline.revision} · {statusLabel(timeline.status)}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : null}
                <Button
                  variant={activeTimeline ? "outline" : "default"}
                  disabled={!allVideosReady || prepareTimeline.isPending || Boolean(activeRun)}
                  onClick={() => prepareTimeline.mutate()}
                >
                  {prepareTimeline.isPending ? <Loader2 className="animate-spin" /> : <RefreshCcw />}
                  {activeTimeline ? "重建时间线" : "准备时间线"}
                </Button>
              </div>
            </div>

            {timelineQuery.isLoading ? <div className="space-y-3 p-4"><Skeleton className="h-32" /><Skeleton className="h-32" /></div> : timelineQuery.data ? (
              <div className="divide-y">
                {timelineQuery.data.clips.map((clip, index) => {
                  const overlayIndex = overlayDrafts.findIndex((overlay) => overlay.role === "onscreen_text" && overlay.timelineClipId === clip.id);
                  const overlay = overlayIndex >= 0 ? overlayDrafts[overlayIndex] : undefined;
                  return (
                    <div className="grid gap-4 p-4 lg:grid-cols-[220px_minmax(0,1fr)]" key={clip.id}>
                      <div className="aspect-video overflow-hidden rounded-md border bg-black">
                        {clip.previewUrl ? <video className="h-full w-full object-contain" controls preload="metadata" src={clip.previewUrl} /> : (
                          <div className="flex h-full items-center justify-center text-muted-foreground"><Film /></div>
                        )}
                      </div>
                      <div className="min-w-0 space-y-3">
                        <div className="flex items-center justify-between gap-3">
                          <div className="min-w-0">
                            <p className="truncate text-sm font-medium">镜头 {index + 1} · {clip.title}</p>
                            <p className="mt-1 text-xs text-muted-foreground">{clip.durationSeconds.toFixed(0)} 秒</p>
                          </div>
                          <Badge variant="outline">已入轨</Badge>
                        </div>
                        {overlay ? (
                          <div className="space-y-1.5">
                            <Label htmlFor={`overlay-${overlay.id}`}>屏幕文字</Label>
                            <Textarea
                              id={`overlay-${overlay.id}`}
                              value={overlay.text}
                              rows={2}
                              onChange={(event) => setOverlayDrafts((current) => current.map((item, itemIndex) =>
                                itemIndex === overlayIndex ? { ...item, text: event.target.value } : item,
                              ))}
                            />
                          </div>
                        ) : null}
                      </div>
                    </div>
                  );
                })}

                {overlayDrafts.filter((overlay) => overlay.role === "cta_end_card").map((overlay, index) => (
                  <div className="grid gap-4 p-4 lg:grid-cols-[220px_minmax(0,1fr)]" key={overlay.id}>
                    <div className="flex aspect-video items-center justify-center rounded-md border bg-muted text-sm font-medium">CTA 尾卡</div>
                    <div className="space-y-1.5">
                      <Label htmlFor={`cta-${overlay.id}`}>尾卡文字</Label>
                      <Textarea
                        id={`cta-${overlay.id}`}
                        value={overlay.text}
                        rows={3}
                        onChange={(event) => setOverlayDrafts((current) => {
                          const targetIndex = current.findIndex((item) => item.id === overlay.id);
                          return current.map((item, itemIndex) => itemIndex === targetIndex ? { ...item, text: event.target.value } : item);
                        })}
                      />
                      <p className="text-xs text-muted-foreground">第 {index + 1} 个尾卡 · {ticksToSeconds(overlay.endTick - overlay.startTick, timelineQuery.data.timeline.timelineTimebase)} 秒</p>
                    </div>
                  </div>
                ))}

                <div className="flex flex-wrap justify-end gap-2 px-4 py-3">
                  <Button variant="outline" disabled={saveTimeline.isPending || Boolean(activeRun)} onClick={() => saveTimeline.mutate()}>
                    {saveTimeline.isPending ? <Loader2 className="animate-spin" /> : <Save />}
                    保存文字
                  </Button>
                  <Button disabled={composeFinal.isPending || Boolean(activeRun)} onClick={() => composeFinal.mutate()}>
                    {composeFinal.isPending || activeRun ? <Loader2 className="animate-spin" /> : <Sparkles />}
                    {activeRun ? "正在合成" : "合成成片"}
                  </Button>
                </div>
              </div>
            ) : (
              <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                {allVideosReady ? "准备时间线后可合成成片。" : "当前脚本单元仍有镜头视频未完成。"}
              </div>
            )}
          </Surface>

          {latestRun ? (
            <Surface>
              <SectionTitle title="合成任务" />
              <div className="space-y-3 p-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2">
                      {activeRun ? <Loader2 className="size-4 animate-spin text-primary" /> : latestRun.status === "succeeded" ? <Check className="size-4 text-emerald-600" /> : <Film className="size-4" />}
                      <span className="text-sm font-medium">{statusLabel(latestRun.status)}</span>
                    </div>
                    {latestRun.errorMessage ? <p className="mt-1 text-sm text-destructive">{latestRun.errorMessage}</p> : null}
                  </div>
                  {latestRun.status === "failed" ? (
                    <Button variant="outline" disabled={retryFinal.isPending} onClick={() => retryFinal.mutate(latestRun)}>
                      {retryFinal.isPending ? <Loader2 className="animate-spin" /> : <RefreshCcw />}
                      重试
                    </Button>
                  ) : null}
                </div>
                <Progress value={latestRun.totalItems ? ((latestRun.completedItems + latestRun.failedItems) / latestRun.totalItems) * 100 : 0} />
              </div>
            </Surface>
          ) : null}

          <Surface>
            <SectionTitle title="成片版本" />
            {finalVideosQuery.isLoading ? <div className="grid gap-4 p-4 lg:grid-cols-2"><Skeleton className="h-72" /><Skeleton className="h-72" /></div> : finalVideos.length ? (
              <div className="grid gap-4 p-4 lg:grid-cols-2">
                {finalVideos.map((version) => (
                  <article className={cn("overflow-hidden rounded-lg border", version.status === "active" && "border-emerald-500/60")} key={version.id}>
                    <div className="aspect-video bg-black">
                      {version.previewUrl ? <video className="h-full w-full object-contain" controls preload="metadata" src={version.previewUrl} /> : (
                        <div className="flex h-full items-center justify-center text-muted-foreground"><Film /></div>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center justify-between gap-3 p-3">
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium">版本 {version.version}</span>
                          <Badge variant={version.status === "active" ? "default" : "outline"}>{version.status === "active" ? "当前成片" : statusLabel(version.status)}</Badge>
                        </div>
                        <p className="mt-1 text-xs text-muted-foreground">{version.resolution} · {version.aspectRatio}</p>
                      </div>
                      <div className="flex items-center gap-2">
                        {version.previewUrl ? <Button size="icon" variant="ghost" asChild title="打开成片"><a href={version.previewUrl} target="_blank" rel="noreferrer"><Play /></a></Button> : null}
                        {version.previewUrl ? <Button size="icon" variant="ghost" asChild title="下载成片"><a href={version.previewUrl} download><Download /></a></Button> : null}
                        {version.status !== "active" && version.productionReadiness === "ready" ? (
                          <Button variant="outline" disabled={activateFinal.isPending} onClick={() => activateFinal.mutate(version)}>设为当前</Button>
                        ) : null}
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            ) : <div className="px-4 py-10 text-center text-sm text-muted-foreground">暂无成片版本。</div>}
          </Surface>
        </>
      )}
    </div>
  );
}

function UnitStatus({ status }: { status: CommerceUnitProductionStatus }) {
  return (
    <div className="flex items-center gap-3">
      <Badge variant={status.status === "completed" ? "default" : "outline"}>{statusLabel(status.status)}</Badge>
      <span className="text-sm tabular-nums text-muted-foreground">{status.progress}%</span>
    </div>
  );
}

function formatUnitNo(value: number) {
  return `脚本 ${String(value).padStart(2, "0")}`;
}

function ticksToSeconds(ticks: number, timebase: number) {
  if (timebase <= 0) return "0";
  return (ticks / timebase).toFixed(0);
}

function FinalPageSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-12 max-w-xl" />
      <Skeleton className="h-80" />
      <Skeleton className="h-64" />
    </div>
  );
}
