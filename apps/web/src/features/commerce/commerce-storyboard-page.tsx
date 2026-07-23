"use client";

import NextImage from "next/image";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  Archive,
  ArrowDown,
  ArrowUp,
  Check,
  Clock3,
  Image as ImageIcon,
  Loader2,
  Maximize2,
  Pencil,
  RefreshCcw,
  Sparkles,
  X,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";

import { SectionTitle, Surface } from "@/components/layout/app-shell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { commerceReferenceRoleLabel, commerceSalesBeatLabel, localeLabel, statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { cn } from "@/lib/utils";
import type {
  CommerceProductionRun,
  CommerceProductionRunDetail,
  CommerceProductReferencePack,
  CommerceStoryboardPlanDetail,
  CommerceStoryboardShot,
  JsonRecord,
  WorkflowRun,
} from "@/lib/types";

type ShotDraft = {
  visualAction: string;
  shotPurpose: string;
  composition: string;
  voiceoverText: string;
  onscreenText: string;
  durationSeconds: number;
  cameraShotSize: string;
  cameraAngle: string;
  cameraMovement: string;
  productReferenceIds: string[];
};

const activeWorkflowStatuses = new Set(["queued", "running", "waiting_review", "cancelling"]);
const activeCommerceRunStatuses = new Set(["queued", "running", "cancelling"]);

export function CommerceStoryboardPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const [selectedUnitId, setSelectedUnitId] = useState("");
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [editingShot, setEditingShot] = useState<CommerceStoryboardShot | null>(null);
  const [shotDraft, setShotDraft] = useState<ShotDraft | null>(null);
  const [largeImageUrl, setLargeImageUrl] = useState("");
  const [planningRunId, setPlanningRunId] = useState("");
  const notifiedWorkflowRunId = useRef("");
  const notifiedProductionRunIds = useRef(new Set<string>());
  const productionNotificationScope = useRef("");
  const [shotSelection, setShotSelection] = useState<{ planId: string; ids: Set<string> }>({ planId: "", ids: new Set() });
  const [busyShotIds, setBusyShotIds] = useState<Set<string>>(new Set());
  const [orderBusy, setOrderBusy] = useState<{ shotId: string; direction: -1 | 1 } | null>(null);

  const unitsQuery = useApiQuery({
    key: qk.commerceScriptUnits(projectId, "active", ""),
    queryFn: (session) => studioApi.listCommerceScriptUnits(session, projectId, { status: "active", limit: 100 }),
  });
  const units = useMemo(() => unitsQuery.data?.items ?? [], [unitsQuery.data?.items]);
  const activeUnitId = units.some((item) => item.id === selectedUnitId) ? selectedUnitId : units[0]?.id ?? "";
  const selectedUnit = units.find((item) => item.id === activeUnitId);

  const workflowRunsQuery = useApiQuery({
    key: qk.workflowRuns(projectId),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId).then((response) => response.items),
    enabled: Boolean(planningRunId),
    refetchInterval: (query) => {
      const run = query.state.data?.find((item) => item.id === planningRunId);
      return !run || activeWorkflowStatuses.has(run.status) ? 2000 : false;
    },
  });
  const planningRun = workflowRunsQuery.data?.find((item) => item.id === planningRunId);
  const planningActive = Boolean(planningRunId && (!planningRun || activeWorkflowStatuses.has(planningRun.status)));

  const plansQuery = useApiQuery({
    key: qk.commerceStoryboardPlans(projectId, activeUnitId),
    queryFn: (session) => studioApi.listCommerceStoryboardPlans(session, projectId, activeUnitId),
    enabled: Boolean(activeUnitId),
    refetchInterval: planningActive ? 2500 : false,
  });
  const plans = useMemo(() => plansQuery.data?.items ?? [], [plansQuery.data?.items]);
  const preferredPlan = plans.find((item) => item.active) ?? plans[0];
  const activePlanId = plans.some((item) => item.id === selectedPlanId) ? selectedPlanId : preferredPlan?.id ?? "";

  const detailQuery = useApiQuery({
    key: qk.commerceStoryboardPlan(projectId, activeUnitId, activePlanId),
    queryFn: (session) => studioApi.getCommerceStoryboardPlan(session, projectId, activeUnitId, activePlanId),
    enabled: Boolean(activeUnitId && activePlanId),
    refetchInterval: planningActive ? 2500 : false,
  });
  const detail = detailQuery.data;
  const selectedShotIds = useMemo(() => {
    if (!detail) return new Set<string>();
    if (shotSelection.planId !== detail.plan.id) return new Set(detail.shots.map((shot) => shot.id));
    const available = new Set(detail.shots.map((shot) => shot.id));
    return new Set([...shotSelection.ids].filter((shotId) => available.has(shotId)));
  }, [detail, shotSelection]);

  const productionRunsQuery = useApiQuery({
    key: qk.commerceProductionRuns(projectId, activeUnitId, "reference_images"),
    queryFn: (session) => studioApi.listCommerceProductionRuns(session, projectId, {
      scriptUnitId: activeUnitId,
      runType: "reference_images",
      limit: 20,
    }),
    enabled: Boolean(activeUnitId),
    refetchInterval: (query) => query.state.data?.items.some((run) => activeCommerceRunStatuses.has(run.status)) ? 2000 : false,
  });
  const referenceRuns = useMemo(() => productionRunsQuery.data?.items ?? [], [productionRunsQuery.data?.items]);
  const activeReferenceRun = referenceRuns.find((run) => activeCommerceRunStatuses.has(run.status));
  const latestReferenceRun = activeReferenceRun ?? referenceRuns[0];

  useEffect(() => {
    if (!activeUnitId || !productionRunsQuery.isFetched) return;
    if (productionNotificationScope.current !== activeUnitId) {
      productionNotificationScope.current = activeUnitId;
      notifiedProductionRunIds.current = new Set(
        referenceRuns
          .filter((run) => !activeCommerceRunStatuses.has(run.status))
          .map((run) => run.id),
      );
      return;
    }
    for (const run of referenceRuns) {
      if (activeCommerceRunStatuses.has(run.status) || notifiedProductionRunIds.current.has(run.id)) continue;
      notifiedProductionRunIds.current.add(run.id);
      invalidate([
        qk.commerceStoryboardPlan(projectId, activeUnitId, activePlanId),
        qk.commerceProductionRun(projectId, run.id),
        qk.commerceProductionRuns(projectId, activeUnitId, "reference_images"),
        qk.workflowRuns(projectId),
      ]);
      if (run.status === "succeeded") toast.success("参考图批次已完成");
      else if (run.status === "partially_succeeded") toast.warning("参考图批次部分完成，可重试失败镜头");
      else if (run.status === "failed") toast.error(run.errorMessage || "参考图批次执行失败");
    }
  }, [activePlanId, activeUnitId, invalidate, productionRunsQuery.isFetched, projectId, referenceRuns]);

  useEffect(() => {
    if (!planningRun || activeWorkflowStatuses.has(planningRun.status) || notifiedWorkflowRunId.current === planningRun.id) return;
    notifiedWorkflowRunId.current = planningRun.id;
    invalidate([
      qk.commerceStoryboardPlans(projectId, activeUnitId),
      qk.workflowRuns(projectId),
    ]);
    if (planningRun.status === "succeeded") toast.success("分镜方案已生成");
    else toast.error(planningRun.errorMessage || "分镜方案生成失败");
  }, [activeUnitId, invalidate, planningRun, projectId]);

  const referencePackQuery = useApiQuery({
    key: qk.commerceProductReferencePack(projectId, detail?.plan.referencePackId ?? ""),
    queryFn: (session) => studioApi.getCommerceProductReferencePack(session, projectId, detail?.plan.referencePackId ?? ""),
    enabled: Boolean(detail?.plan.referencePackId && editingShot),
  });

  const createPlan = useApiMutation<WorkflowRun, void>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session) => studioApi.createCommerceStoryboardPlan(
      session,
      projectId,
      activeUnitId,
      selectedUnit?.activeUnitGenerationId ?? "",
      `commerce-storyboard-${activeUnitId}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      setPlanningRunId(run.id);
      invalidate([qk.workflowRuns(projectId)]);
      toast.success("分镜任务已提交");
    },
    onError: (error) => toast.error(error.message),
  });

  const updateShot = useApiMutation<CommerceStoryboardPlanDetail, { shot: CommerceStoryboardShot; draft: ShotDraft }>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, { shot, draft }) => studioApi.updateCommerceStoryboardShot(
      session,
      projectId,
      activeUnitId,
      shot.id,
      {
        expectedPlanRevision: detail?.plan.revision ?? 0,
        expectedShotRevision: shot.revision,
        visualAction: draft.visualAction,
        shotPurpose: draft.shotPurpose,
        composition: draft.composition,
        voiceoverText: draft.voiceoverText,
        onscreenText: draft.onscreenText,
        durationSeconds: draft.durationSeconds,
        camera: {
          ...asRecord(shot.camera),
          shotSize: draft.cameraShotSize,
          angle: draft.cameraAngle,
          movement: draft.cameraMovement,
        },
        productReferenceIds: draft.productReferenceIds,
      },
    ),
    onSuccess: (next) => {
      refreshStoryboardQueries(next);
      setEditingShot(null);
      setShotDraft(null);
      toast.success("镜头已保存");
    },
    onError: (error) => toast.error(error.message),
  });

  const archiveShot = useApiMutation<CommerceStoryboardPlanDetail, CommerceStoryboardShot>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, shot) => studioApi.archiveCommerceStoryboardShot(
      session,
      projectId,
      activeUnitId,
      shot.id,
      detail?.plan.id ?? "",
      detail?.plan.revision ?? 0,
    ),
    onSuccess: (next) => {
      refreshStoryboardQueries(next);
      setEditingShot(null);
      setShotDraft(null);
      toast.success("镜头已移出当前方案");
    },
    onError: (error) => toast.error(error.message),
  });

  const reorderShots = useApiMutation<CommerceStoryboardPlanDetail, CommerceStoryboardShot[]>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, ordered) => studioApi.reorderCommerceStoryboardShots(session, projectId, activeUnitId, {
      planId: detail?.plan.id ?? "",
      expectedPlanRevision: detail?.plan.revision ?? 0,
      items: ordered.map((shot) => ({ shotId: shot.id, durationSeconds: shot.durationSeconds })),
    }),
    onSuccess: (next) => {
      refreshStoryboardQueries(next);
      toast.success("镜头顺序已更新");
    },
    onError: (error) => toast.error(error.message),
    onSettled: () => setOrderBusy(null),
  });

  const activatePlan = useApiMutation<CommerceStoryboardPlanDetail, void>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session) => studioApi.activateCommerceStoryboardPlan(
      session,
      projectId,
      activeUnitId,
      detail?.plan.id ?? "",
      detail?.plan.revision ?? 0,
    ),
    onSuccess: (next) => {
      refreshStoryboardQueries(next);
      toast.success("分镜方案已启用");
    },
    onError: (error) => toast.error(error.message),
  });

  const generateReferenceImages = useApiMutation<CommerceProductionRun, { operation: "generate_prompts" | "generate_images"; force: boolean }>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, payload) => studioApi.generateCommerceReferenceImageBatch(
      session,
      projectId,
      activeUnitId,
      {
        operation: payload.operation,
        planId: detail?.plan.id ?? "",
        expectedPlanRevision: detail?.plan.revision ?? 0,
        expectedUnitGenerationId: detail?.plan.scriptUnitGenerationId ?? "",
        shotIds: [...selectedShotIds],
        force: payload.force,
        concurrency: 5,
      },
      `commerce-${payload.operation}-${activeUnitId}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "reference_images"),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success(run.status === "queued" ? "生产任务已提交" : "已返回现有生产任务");
    },
    onError: (error) => toast.error(error.message),
  });

  const retryReferenceRun = useApiMutation<CommerceProductionRun, string>({
    requiredPermission: "storyboard.generate",
    mutationFn: (session, runId) => studioApi.retryFailedCommerceProductionRun(
      session,
      projectId,
      runId,
      {},
      `commerce-retry-${runId}-${crypto.randomUUID()}`,
    ),
    onSuccess: (run) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "reference_images"),
        qk.commerceProductionRun(projectId, run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("失败镜头已重新提交");
    },
    onError: (error) => toast.error(error.message),
  });

  const cancelReferenceRun = useApiMutation<CommerceProductionRunDetail, string>({
    requiredPermission: "workflow.cancel",
    mutationFn: (session, runId) => studioApi.cancelCommerceProductionRun(session, projectId, runId),
    onSuccess: (detail) => {
      invalidate([
        qk.commerceProductionRuns(projectId, activeUnitId, "reference_images"),
        qk.commerceProductionRun(projectId, detail.run.id),
        qk.workflowRuns(projectId),
      ]);
      toast.success("已请求取消生产批次");
    },
    onError: (error) => toast.error(error.message),
  });

  function refreshStoryboardQueries(next: CommerceStoryboardPlanDetail) {
    invalidate([
      qk.commerceStoryboardPlans(projectId, activeUnitId),
      qk.commerceStoryboardPlan(projectId, activeUnitId, next.plan.id),
      qk.workflowRuns(projectId),
    ]);
  }

  function openShotEditor(shot: CommerceStoryboardShot) {
    const camera = asRecord(shot.camera);
    setEditingShot(shot);
    setShotDraft({
      visualAction: shot.visualAction,
      shotPurpose: shot.shotPurpose,
      composition: shot.composition,
      voiceoverText: shot.voiceoverText,
      onscreenText: shot.onscreenText,
      durationSeconds: shot.durationSeconds,
      cameraShotSize: textValue(camera.shotSize),
      cameraAngle: textValue(camera.angle),
      cameraMovement: textValue(camera.movement),
      productReferenceIds: shot.productReferences.map((item) => item.productReferenceId),
    });
  }

  async function moveShot(index: number, offset: -1 | 1) {
    if (!detail || orderBusy) return;
    const nextIndex = index + offset;
    if (nextIndex < 0 || nextIndex >= detail.shots.length) return;
    const ordered = [...detail.shots];
    [ordered[index], ordered[nextIndex]] = [ordered[nextIndex], ordered[index]];
    setOrderBusy({ shotId: detail.shots[index].id, direction: offset });
    await reorderShots.mutateAsync(ordered).catch(() => undefined);
  }

  async function saveShot() {
    if (!editingShot || !shotDraft || busyShotIds.has(editingShot.id)) return;
    const shotId = editingShot.id;
    setBusyShotIds((current) => new Set(current).add(shotId));
    await updateShot.mutateAsync({ shot: editingShot, draft: shotDraft }).catch(() => undefined);
    setBusyShotIds((current) => {
      const next = new Set(current);
      next.delete(shotId);
      return next;
    });
  }

  const totalDuration = useMemo(
    () => detail?.shots.reduce((sum, shot) => sum + shot.durationSeconds, 0) ?? 0,
    [detail?.shots],
  );

  return (
    <div className="space-y-5 pb-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold">分镜方案</h1>
          {selectedUnit ? (
            <p className="mt-1 truncate text-sm text-muted-foreground">
              脚本 {formatUnitNo(selectedUnit.unitNo)} · {selectedUnit.title}
            </p>
          ) : null}
        </div>
        <Button
          onClick={() => createPlan.mutate()}
          disabled={!selectedUnit?.activeUnitGenerationId || createPlan.isPending || planningActive}
        >
          {createPlan.isPending || planningActive ? <Loader2 className="size-4 animate-spin" /> : plans.length ? <RefreshCcw className="size-4" /> : <Sparkles className="size-4" />}
          {plans.length ? "重新生成方案" : "生成分镜方案"}
        </Button>
      </div>

      <div className="grid gap-3 md:grid-cols-[minmax(240px,1fr)_minmax(220px,320px)]">
        <div className="space-y-1.5">
          <Label>脚本单元</Label>
          <Select value={activeUnitId} onValueChange={(value) => { setSelectedUnitId(value); setSelectedPlanId(""); }}>
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
        <div className="space-y-1.5">
          <Label>方案版本</Label>
          <Select value={activePlanId} onValueChange={setSelectedPlanId} disabled={!plans.length}>
            <SelectTrigger><SelectValue placeholder="暂无方案" /></SelectTrigger>
            <SelectContent>
              {plans.map((plan) => (
                <SelectItem key={plan.id} value={plan.id}>
                  版本 {plan.planRevision} · {plan.active ? "当前启用" : statusLabel(plan.status)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {unitsQuery.isLoading ? <StoryboardSkeleton /> : !units.length ? (
        <Surface><SectionTitle title="暂无广告脚本" /><div className="px-4 py-10 text-center text-sm text-muted-foreground">请先在商品与脚本页面创建广告脚本。</div></Surface>
      ) : plansQuery.isLoading || detailQuery.isLoading ? <StoryboardSkeleton /> : !detail ? (
        <Surface><SectionTitle title="尚未生成分镜" /><div className="px-4 py-10 text-center text-sm text-muted-foreground">完成脚本准备后即可生成当前单元的分镜方案。</div></Surface>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-y py-3 text-sm">
            <span className="inline-flex items-center gap-1.5"><Clock3 className="size-4 text-muted-foreground" />{totalDuration}/{detail.plan.targetDurationSeconds} 秒</span>
            <span>{detail.shots.length} 个镜头</span>
            <span>{localeLabel(detail.plan.targetLanguage)}</span>
            <span>{detail.plan.aspectRatio}</span>
            <Badge variant={detail.plan.active ? "default" : "outline"}>{detail.plan.active ? "当前启用" : statusLabel(detail.plan.status)}</Badge>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={!detail.plan.active || selectedShotIds.size === 0 || Boolean(activeReferenceRun) || generateReferenceImages.isPending}
                onClick={() => generateReferenceImages.mutate({ operation: "generate_prompts", force: false })}
              >
                {generateReferenceImages.isPending && generateReferenceImages.variables?.operation === "generate_prompts" ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
                生成图片提示词
              </Button>
              <Button
                size="sm"
                disabled={!detail.plan.active || selectedShotIds.size === 0 || Boolean(activeReferenceRun) || generateReferenceImages.isPending}
                onClick={() => generateReferenceImages.mutate({ operation: "generate_images", force: false })}
              >
                {generateReferenceImages.isPending && generateReferenceImages.variables?.operation === "generate_images" ? <Loader2 className="size-4 animate-spin" /> : <ImageIcon className="size-4" />}
                生成参考图
              </Button>
            </div>
            {!detail.plan.active && detail.plan.status === "reviewing" ? (
              <Button size="sm" variant="outline" onClick={() => activatePlan.mutate()} disabled={activatePlan.isPending}>
                {activatePlan.isPending ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
                校验并启用
              </Button>
            ) : null}
          </div>

          <div className="flex flex-wrap items-center gap-3 border-b pb-3 text-sm">
            <label className="inline-flex cursor-pointer items-center gap-2">
              <Checkbox
                checked={selectedShotIds.size === detail.shots.length && detail.shots.length > 0}
                onCheckedChange={(checked) => setShotSelection({
                  planId: detail.plan.id,
                  ids: checked ? new Set(detail.shots.map((shot) => shot.id)) : new Set(),
                })}
              />
              选择全部镜头
            </label>
            <span className="text-muted-foreground">已选 {selectedShotIds.size}/{detail.shots.length}</span>
            {latestReferenceRun ? <CommerceRunSummary run={latestReferenceRun} /> : null}
            {latestReferenceRun && (latestReferenceRun.status === "failed" || latestReferenceRun.status === "partially_succeeded") ? (
              <Button
                size="sm"
                variant="outline"
                disabled={retryReferenceRun.isPending || Boolean(activeReferenceRun)}
                onClick={() => retryReferenceRun.mutate(latestReferenceRun.id)}
              >
                {retryReferenceRun.isPending ? <Loader2 className="size-4 animate-spin" /> : <RefreshCcw className="size-4" />}
                重试失败镜头
              </Button>
            ) : null}
            {activeReferenceRun ? (
              <Button
                size="sm"
                variant="ghost"
                className="text-destructive hover:text-destructive"
                disabled={cancelReferenceRun.isPending}
                onClick={() => cancelReferenceRun.mutate(activeReferenceRun.id)}
              >
                {cancelReferenceRun.isPending ? <Loader2 className="size-4 animate-spin" /> : <XCircle className="size-4" />}
                取消任务
              </Button>
            ) : null}
          </div>

          <div className="divide-y border-y">
            {detail.shots.map((shot, index) => (
              <article key={shot.id} className="grid gap-4 py-4 lg:grid-cols-[24px_150px_minmax(0,1fr)_auto]">
                <Checkbox
                  className="mt-1"
                  checked={selectedShotIds.has(shot.id)}
                  aria-label={`选择镜头 ${shot.shotOrdinal}`}
                  onCheckedChange={(checked) => {
                    const next = new Set(selectedShotIds);
                    if (checked) next.add(shot.id); else next.delete(shot.id);
                    setShotSelection({ planId: detail.plan.id, ids: next });
                  }}
                />
                <button
                  type="button"
                  className="group relative overflow-hidden border bg-muted/40"
                  style={{ aspectRatio: cssAspectRatio(detail.plan.aspectRatio) }}
                  onClick={() => shot.imagePreviewUrl && setLargeImageUrl(shot.imagePreviewUrl)}
                  disabled={!shot.imagePreviewUrl}
                  aria-label={shot.imagePreviewUrl ? "查看分镜参考图" : "暂无分镜参考图"}
                >
                  {shot.imagePreviewUrl ? (
                    <NextImage src={shot.imagePreviewUrl} alt={`镜头 ${shot.shotOrdinal} 参考图`} fill unoptimized className="object-cover" sizes="150px" />
                  ) : (
                    <span className="absolute inset-0 flex items-center justify-center text-muted-foreground"><ImageIcon className="size-6" /></span>
                  )}
                  {shot.imagePreviewUrl ? <Maximize2 className="absolute bottom-2 right-2 size-4 rounded bg-background/85 p-0.5 opacity-0 transition-opacity group-hover:opacity-100" /> : null}
                </button>

                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{formatUnitNo(selectedUnit?.unitNo ?? 0)}-{String(shot.shotOrdinal).padStart(2, "0")}</span>
                    <Badge variant="secondary">{commerceSalesBeatLabel(shot.salesBeat)}</Badge>
                    <span className="text-xs text-muted-foreground">{shot.durationSeconds} 秒</span>
                    <Badge variant="outline">提示词：{statusLabel(shot.imagePromptStatus)}</Badge>
                    <Badge variant="outline">参考图：{statusLabel(shot.imageStatus)}</Badge>
                  </div>
                  <p className="line-clamp-2 text-sm font-medium">{shot.visualAction}</p>
                  {shot.voiceoverText ? <p className="line-clamp-2 text-sm text-muted-foreground">旁白：{shot.voiceoverText}</p> : null}
                  {shot.onscreenText ? <p className="line-clamp-1 text-xs text-muted-foreground">屏幕文字：{shot.onscreenText}</p> : null}
                  {shot.imagePrompt ? <p className="line-clamp-2 text-xs text-muted-foreground">图片提示词：{shot.imagePrompt}</p> : null}
                  {shot.imageErrorMessage ? <p className="text-xs text-destructive">{shot.imageErrorMessage}</p> : null}
                  <div className="flex flex-wrap gap-1.5">
                    {shot.productReferences.map((reference) => (
                      <span key={reference.id} className="text-xs text-muted-foreground">{commerceReferenceRoleLabel(reference.role)}</span>
                    ))}
                  </div>
                </div>

                <div className="flex items-start gap-1">
                  <Button variant="ghost" size="icon" title="上移镜头" disabled={index === 0 || Boolean(orderBusy)} onClick={() => void moveShot(index, -1)}>
                    {orderBusy?.shotId === shot.id && orderBusy.direction === -1 ? <Loader2 className="size-4 animate-spin" /> : <ArrowUp className="size-4" />}
                  </Button>
                  <Button variant="ghost" size="icon" title="下移镜头" disabled={index === detail.shots.length - 1 || Boolean(orderBusy)} onClick={() => void moveShot(index, 1)}>
                    {orderBusy?.shotId === shot.id && orderBusy.direction === 1 ? <Loader2 className="size-4 animate-spin" /> : <ArrowDown className="size-4" />}
                  </Button>
                  <Button variant="ghost" size="icon" title="编辑镜头" onClick={() => openShotEditor(shot)}>
                    <Pencil className="size-4" />
                  </Button>
                </div>
              </article>
            ))}
          </div>
        </>
      )}

      <Dialog open={Boolean(editingShot)} onOpenChange={(open) => { if (!open) { setEditingShot(null); setShotDraft(null); } }}>
        <DialogContent className="max-h-[90vh] overflow-hidden p-0 sm:max-w-4xl">
          <DialogHeader className="border-b px-5 py-4"><DialogTitle>编辑镜头 {editingShot?.shotOrdinal}</DialogTitle></DialogHeader>
          <ScrollArea className="max-h-[68vh] px-5 py-4">
            {editingShot && shotDraft ? (
              <div className="space-y-5 pb-2">
                <div className="grid gap-4 md:grid-cols-2">
                  <Field label="镜头目的"><Input value={shotDraft.shotPurpose} onChange={(event) => setShotDraft({ ...shotDraft, shotPurpose: event.target.value })} /></Field>
                  <Field label="构图"><Input value={shotDraft.composition} onChange={(event) => setShotDraft({ ...shotDraft, composition: event.target.value })} /></Field>
                </div>
                <Field label="画面动作"><Textarea rows={4} value={shotDraft.visualAction} onChange={(event) => setShotDraft({ ...shotDraft, visualAction: event.target.value })} /></Field>
                <div className="grid gap-4 md:grid-cols-3">
                  <Field label="景别"><Input value={shotDraft.cameraShotSize} onChange={(event) => setShotDraft({ ...shotDraft, cameraShotSize: event.target.value })} placeholder="近景" /></Field>
                  <Field label="机位角度"><Input value={shotDraft.cameraAngle} onChange={(event) => setShotDraft({ ...shotDraft, cameraAngle: event.target.value })} placeholder="平视" /></Field>
                  <Field label="运镜"><Input value={shotDraft.cameraMovement} onChange={(event) => setShotDraft({ ...shotDraft, cameraMovement: event.target.value })} placeholder="缓慢推进" /></Field>
                </div>
                <Field label="逐字旁白"><Textarea rows={3} value={shotDraft.voiceoverText} onChange={(event) => setShotDraft({ ...shotDraft, voiceoverText: event.target.value })} /></Field>
                <Field label="屏幕文字"><Textarea rows={2} value={shotDraft.onscreenText} onChange={(event) => setShotDraft({ ...shotDraft, onscreenText: event.target.value })} /></Field>
                <Field label="镜头时长">
                  <Select value={String(shotDraft.durationSeconds)} onValueChange={(value) => setShotDraft({ ...shotDraft, durationSeconds: Number(value) })}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>{detail?.plan.allowedShotDurations.map((duration) => <SelectItem key={duration} value={String(duration)}>{duration} 秒</SelectItem>)}</SelectContent>
                  </Select>
                </Field>
                <ProductReferenceSelector pack={referencePackQuery.data} selected={shotDraft.productReferenceIds} onChange={(productReferenceIds) => setShotDraft({ ...shotDraft, productReferenceIds })} onPreview={setLargeImageUrl} />
              </div>
            ) : null}
          </ScrollArea>
          <DialogFooter className="flex-row justify-between border-t px-5 py-4 sm:justify-between">
            <Button variant="ghost" className="text-destructive hover:text-destructive" disabled={!editingShot || archiveShot.isPending} onClick={() => editingShot && archiveShot.mutate(editingShot)}>
              {archiveShot.isPending ? <Loader2 className="size-4 animate-spin" /> : <Archive className="size-4" />}
              移出方案
            </Button>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => { setEditingShot(null); setShotDraft(null); }}>取消</Button>
              <Button onClick={() => void saveShot()} disabled={!editingShot || !shotDraft || busyShotIds.has(editingShot.id)}>
                {editingShot && busyShotIds.has(editingShot.id) ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
                保存镜头
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(largeImageUrl)} onOpenChange={(open) => { if (!open) setLargeImageUrl(""); }}>
        <DialogContent className="h-[88vh] border-0 bg-black/95 p-8 sm:max-w-6xl" showCloseButton={false}>
          <DialogTitle className="sr-only">图片预览</DialogTitle>
          {largeImageUrl ? <div className="relative h-full w-full"><NextImage src={largeImageUrl} alt="商品或分镜大图" fill unoptimized className="object-contain" sizes="90vw" /></div> : null}
          <Button
            aria-label="关闭图片预览"
            className="absolute right-3 top-3 z-10 bg-black/60 text-white hover:bg-black/80 hover:text-white"
            onClick={() => setLargeImageUrl("")}
            size="icon"
            type="button"
            variant="ghost"
          >
            <X className="size-5" />
          </Button>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ProductReferenceSelector({
  pack,
  selected,
  onChange,
  onPreview,
}: {
  pack?: CommerceProductReferencePack;
  selected: string[];
  onChange: (ids: string[]) => void;
  onPreview: (url: string) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>商品参考图</Label>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {(pack?.items ?? []).map((item) => {
          const checked = selected.includes(item.productReferenceId);
          return (
            <div key={item.id} className={cn("relative border p-2", checked && "border-primary bg-primary/5")}>
              <button type="button" className="relative mb-2 block aspect-square w-full overflow-hidden bg-muted" onClick={() => item.previewUrl && onPreview(item.previewUrl)}>
                {item.previewUrl ? <NextImage src={item.previewUrl} alt={commerceReferenceRoleLabel(item.referenceRole)} fill unoptimized className="object-cover" sizes="160px" /> : <ImageIcon className="absolute inset-0 m-auto size-5 text-muted-foreground" />}
              </button>
              <label className="flex cursor-pointer items-center gap-2 text-xs">
                <Checkbox
                  checked={checked}
                  onCheckedChange={(value) => onChange(value ? [...selected, item.productReferenceId] : selected.filter((id) => id !== item.productReferenceId))}
                />
                <span className="truncate">{commerceReferenceRoleLabel(item.referenceRole)}</span>
              </label>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function CommerceRunSummary({ run }: { run: CommerceProductionRun }) {
  const settled = run.completedItems + run.failedItems + run.cancelledItems;
  return (
    <div className="ml-auto inline-flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
      {activeCommerceRunStatuses.has(run.status) ? <Loader2 className="size-3.5 shrink-0 animate-spin text-primary" /> : run.status === "succeeded" ? <Check className="size-3.5 shrink-0 text-emerald-600" /> : null}
      <span className="truncate">最近任务：{statusLabel(run.status)}</span>
      <span>{settled}/{run.totalItems}</span>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="space-y-1.5"><Label>{label}</Label>{children}</div>;
}

function StoryboardSkeleton() {
  return <div className="space-y-3">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-36 w-full" />)}</div>;
}

function formatUnitNo(value: number) {
  return String(value || 0).padStart(2, "0");
}

function cssAspectRatio(value: string) {
  const [width, height] = value.split(":").map(Number);
  return width > 0 && height > 0 ? `${width} / ${height}` : "16 / 9";
}

function asRecord(value: unknown): JsonRecord {
  return value && typeof value === "object" && !Array.isArray(value) ? value as JsonRecord : {};
}

function textValue(value: unknown) {
  return typeof value === "string" ? value : "";
}
