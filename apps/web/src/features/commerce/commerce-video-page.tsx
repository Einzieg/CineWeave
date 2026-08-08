"use client";

import type { Route } from "next";
import NextImage from "next/image";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Archive,
  Check,
  ChevronRight,
  Clock3,
  Film,
  History,
  ImagePlus,
  Loader2,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Trash2,
  TriangleAlert,
  Upload,
  Volume2,
} from "lucide-react";
import { toast } from "sonner";

import { Surface } from "@/components/layout/app-shell";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import {
  commercePromptLengthUnitLabel,
  commerceScriptExceedsPromptLimit,
  maximumExecutableDuration,
  measureCommerceScriptLength,
} from "@/lib/commerce-direct-video";
import { userFacingErrorMessage } from "@/lib/error-localization";
import { qk } from "@/lib/query/keys";
import { providerVideoWarningMessage } from "@/lib/provider-video-warnings";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { projectHref } from "@/lib/routes";
import { cn } from "@/lib/utils";
import type {
  CommerceDirectVideoJob,
  CommerceDirectVideoOptions,
  CommerceDirectVideoReferenceSelection,
  CommercePromptLengthConstraint,
  CommerceProductReference,
  CommerceScriptDerivationBatch,
  CommerceScriptReferenceImage,
  CommerceScriptUnit,
  CommerceScriptVersionMutation,
} from "@/lib/types";

const activeJobStatuses = new Set(["queued", "running", "cancelling"]);

type ScriptEditorState = {
  mode: "create" | "edit";
  unit?: CommerceScriptUnit;
  title: string;
  content: string;
};

type GenerateState = {
  unit: CommerceScriptUnit;
  durationSeconds: number;
  resolution: string;
  selectedProductReferenceIds: string[];
  selectedCustomReferenceIds: string[];
};

type ScriptDerivationMetadataView = {
  batchId: string;
  rootBatchId: string;
  itemId: string;
  sourceScriptUnitId: string;
  sourceScriptTitle: string;
  dimension: string;
  variationKey: string;
  variationLabel: string;
  variationBrief: string;
};

type ScriptDisplayBlock =
  | { kind: "unit"; key: string; unit: CommerceScriptUnit }
  | {
      kind: "derivation";
      key: string;
      batchId: string;
      metadata: ScriptDerivationMetadataView;
      batch?: CommerceScriptDerivationBatch;
      units: CommerceScriptUnit[];
    };

type ScriptDerivationBlock = Extract<ScriptDisplayBlock, { kind: "derivation" }>;

export function CommerceVideoPage({ projectId }: { projectId: string }) {
  const invalidate = useInvalidateKeys();
  const searchParams = useSearchParams();
  const [editor, setEditor] = useState<ScriptEditorState | null>(null);
  const [generate, setGenerate] = useState<GenerateState | null>(null);
  const [archiveTarget, setArchiveTarget] = useState<CommerceScriptUnit | null>(null);
  const [historyUnit, setHistoryUnit] = useState<CommerceScriptUnit | null>(null);
  const [videoPreview, setVideoPreview] = useState<CommerceDirectVideoJob | null>(null);
  const terminalNotifications = useRef(new Map<string, string>());
  const handledNavigationIntent = useRef("");

  const projectQuery = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
  });
  const productQuery = useApiQuery({
    key: qk.commerceProduct(projectId),
    queryFn: (session) => studioApi.getCommerceProduct(session, projectId),
    retry: false,
  });
  const productReferencesQuery = useApiQuery({
    key: qk.commerceProductReferences(projectId, "active"),
    queryFn: (session) => studioApi.listCommerceProductReferences(session, projectId, "active"),
    enabled: Boolean(productQuery.data?.id),
  });
  const optionsQuery = useApiQuery({
    key: qk.commerceDirectVideoOptions(projectId),
    queryFn: (session) => studioApi.getCommerceDirectVideoOptions(session, projectId),
  });
  const unitsQuery = useApiQuery({
    key: qk.commerceScriptUnits(projectId, "active", ""),
    queryFn: (session) =>
      studioApi.listCommerceScriptUnits(session, projectId, {
        status: "active",
        limit: 100,
      }),
  });
  const derivationsQuery = useApiQuery({
    key: qk.commerceScriptDerivations(projectId, "all", "all"),
    queryFn: (session) =>
      studioApi.listCommerceScriptDerivations(session, projectId, {
        status: "all",
        limit: 100,
      }),
  });
  const jobsQuery = useApiQuery({
    key: qk.commerceDirectVideos(projectId),
    queryFn: (session) => studioApi.listCommerceDirectVideos(session, projectId),
    refetchInterval: (query) =>
      query.state.data?.items.some((item) => activeJobStatuses.has(item.status)) ? 3000 : false,
  });

  const options = optionsQuery.data;
  const units = useMemo(() => unitsQuery.data?.items ?? [], [unitsQuery.data?.items]);
  const jobs = useMemo(() => jobsQuery.data?.items ?? [], [jobsQuery.data?.items]);
  const productReferences = useMemo(
    () => [...(productReferencesQuery.data?.items ?? [])].sort((left, right) => left.ordinal - right.ordinal),
    [productReferencesQuery.data?.items],
  );
  const jobsByUnit = useMemo(() => groupJobsByUnit(jobs), [jobs]);
  const derivations = useMemo(
    () => derivationsQuery.data?.items ?? [],
    [derivationsQuery.data?.items],
  );
  const scriptBlocks = useMemo(
    () => buildScriptDisplayBlocks(units, derivations),
    [derivations, units],
  );

  useEffect(() => {
    for (const job of jobs) {
      if (activeJobStatuses.has(job.status)) {
        terminalNotifications.current.set(job.id, job.status);
        continue;
      }
      const previous = terminalNotifications.current.get(job.id);
      if (!previous || !activeJobStatuses.has(previous)) {
        terminalNotifications.current.set(job.id, job.status);
        continue;
      }
      terminalNotifications.current.set(job.id, job.status);
      if (job.status === "succeeded") {
        const warning = job.outputWarnings?.[0];
        if (warning) {
          toast.warning(providerVideoWarningMessage(warning));
        } else {
          toast.success("广告视频生成完成");
        }
      } else if (job.status === "failed") {
        toast.error(job.errorMessage || "广告视频生成失败");
      } else if (job.status === "cancelled") {
        toast.info("广告视频生成已取消");
      }
    }
  }, [jobs]);

  const saveScript = useApiMutation({
    mutationFn: async (session, input: ScriptEditorState) => {
      const title = input.title.trim();
      const content = input.content.trim();
      if (!title || !content) throw new Error("脚本标题和正文不能为空");
      const promptConstraint = options?.scriptPromptConstraint;
      if (promptConstraint && commerceScriptExceedsPromptLimit(content, promptConstraint)) {
        const length = measureCommerceScriptLength(content, promptConstraint.unit);
        throw new Error(
          `广告脚本长度为 ${length} ${commercePromptLengthUnitLabel(promptConstraint.unit)}，`
          + `超过当前视频模型上限 ${promptConstraint.maxLength}`,
        );
      }
      const maximumDuration =
        options?.defaultDurationSeconds
        ?? maximumExecutableDuration(options?.executableDurationSeconds ?? []);
      if (input.mode === "create") {
        return studioApi.createCommerceScriptUnit(session, projectId, {
          expectedScriptUnitsRevision: unitsQuery.data?.scriptUnitsRevision ?? 0,
          title,
          content,
          languageMode: "auto",
          targetDurationSeconds:
            maximumDuration
            || projectQuery.data?.scriptUnitDefaults?.targetDurationSeconds
            || 6,
          targetPlatform: projectQuery.data?.scriptUnitDefaults?.targetPlatform ?? "other",
        });
      }
      const unit = input.unit;
      if (!unit) throw new Error("脚本不存在");
      let currentUnit = unit;
      if (title !== unit.title) {
        currentUnit = await studioApi.updateCommerceScriptUnit(session, projectId, unit.id, {
          expectedRevision: unit.revision,
          title,
        });
      }
      const currentContent = unit.currentSourceVersion?.content ?? unit.draftContent;
      if (content !== currentContent.trim()) {
        return studioApi.createCommerceScriptVersion(session, projectId, unit.id, {
          expectedRevision: currentUnit.revision,
          content,
          activate: true,
        });
      }
      return {
        scriptUnit: currentUnit,
        version: unit.currentSourceVersion,
        activated: true,
        requiresRebuild: false,
      } as CommerceScriptVersionMutation;
    },
    onSuccess: () => {
      setEditor(null);
      invalidate([
        qk.commerceScriptUnitsRoot(projectId),
        qk.commerceDirectVideosRoot(projectId),
      ]);
      toast.success("广告脚本已保存");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "广告脚本保存失败")),
  });

  const archiveScript = useApiMutation({
    mutationFn: (session, unit: CommerceScriptUnit) =>
      studioApi.archiveCommerceScriptUnit(session, projectId, unit.id, unit.revision),
    onSuccess: () => {
      setArchiveTarget(null);
      invalidate([
        qk.commerceScriptUnitsRoot(projectId),
        qk.commerceDirectVideosRoot(projectId),
      ]);
      toast.success("广告脚本已归档");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "广告脚本归档失败")),
  });

  const createVideo = useApiMutation({
    mutationFn: (session, input: GenerateState) => {
      const references: CommerceDirectVideoReferenceSelection[] = [
        ...input.selectedProductReferenceIds.map((sourceId) => ({
          sourceType: "product" as const,
          sourceId,
        })),
        ...input.selectedCustomReferenceIds.map((sourceId) => ({
          sourceType: "custom" as const,
          sourceId,
        })),
      ];
      return studioApi.createCommerceDirectVideo(
        session,
        projectId,
        input.unit.id,
        {
          durationSeconds: input.durationSeconds,
          resolution: input.resolution,
          aspectRatio: options?.defaultAspectRatio,
          generateAudio: true,
          references,
        },
        `commerce-direct-video-${input.unit.id}-${crypto.randomUUID()}`,
      );
    },
    onSuccess: () => {
      setGenerate(null);
      invalidate([
        qk.commerceDirectVideosRoot(projectId),
        qk.workflowRuns(projectId),
        qk.projectControlCommands(projectId),
      ]);
      toast.success("视频任务已提交");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "视频任务提交失败")),
  });

  const batchCreateVideos = useApiMutation({
    mutationFn: async (session, block: ScriptDerivationBlock) => {
      if (!options) {
        throw new Error("视频模型配置尚未加载完成");
      }
      const eligibleUnits = block.units.filter(
        (unit) => !(jobsByUnit.get(unit.id) ?? []).some((job) => activeJobStatuses.has(job.status)),
      );
      const results = await mapWithConcurrency(eligibleUnits, 5, async (unit) => {
        try {
          const input = defaultGenerateState(unit, options, productReferences);
          if (!input) {
            throw new Error("当前视频模型没有可执行的时长、分辨率或商品参考图组合");
          }
          const references: CommerceDirectVideoReferenceSelection[] = input.selectedProductReferenceIds.map(
            (sourceId) => ({ sourceType: "product", sourceId }),
          );
          const command = await studioApi.createCommerceDirectVideo(
            session,
            projectId,
            unit.id,
            {
              durationSeconds: input.durationSeconds,
              resolution: input.resolution,
              aspectRatio: options.defaultAspectRatio,
              generateAudio: true,
              references,
            },
            `commerce-derivation-video-${block.batchId}-${unit.id}-${crypto.randomUUID()}`,
          );
          return { unit, command, error: null as Error | null };
        } catch (error) {
          return {
            unit,
            command: null,
            error: error instanceof Error ? error : new Error("视频任务提交失败"),
          };
        }
      });
      return { block, results };
    },
    onSuccess: ({ results }) => {
      const succeeded = results.filter((result) => result.command);
      const failed = results.filter((result) => result.error);
      invalidate([
        qk.commerceDirectVideosRoot(projectId),
        qk.workflowRuns(projectId),
        qk.projectControlCommands(projectId),
      ]);
      if (failed.length === 0) {
        toast.success(`已提交 ${succeeded.length} 个视频任务`);
      } else if (succeeded.length > 0) {
        toast.warning(`已提交 ${succeeded.length} 个，${failed.length} 个提交失败`);
      } else {
        toast.error(
          userFacingErrorMessage(failed[0]?.error, `${failed.length} 个视频任务均提交失败`),
        );
      }
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "批量视频任务提交失败")),
  });

  const openGenerate = useCallback((unit: CommerceScriptUnit) => {
    const next = options ? defaultGenerateState(unit, options, productReferences) : null;
    if (!next) {
      toast.error("当前视频模型没有可执行的时长、分辨率或商品参考图组合");
      return;
    }
    setGenerate(next);
  }, [options, productReferences]);

  useEffect(() => {
    const generateScriptUnitId = searchParams.get("generateScriptUnitId")?.trim() ?? "";
    const scriptUnitId = searchParams.get("scriptUnitId")?.trim() ?? "";
    const intentKey = generateScriptUnitId
      ? `generate:${generateScriptUnitId}`
      : scriptUnitId
        ? `view:${scriptUnitId}`
        : "";
    if (!intentKey || handledNavigationIntent.current === intentKey || units.length === 0) return;
    const targetID = generateScriptUnitId || scriptUnitId;
    const unit = units.find((candidate) => candidate.id === targetID);
    if (!unit) return;
    handledNavigationIntent.current = intentKey;
    window.requestAnimationFrame(() => {
      document.getElementById(`commerce-script-${unit.id}`)?.scrollIntoView({
        behavior: "smooth",
        block: "center",
      });
      if (generateScriptUnitId) openGenerate(unit);
    });
  }, [openGenerate, searchParams, units]);

  const loading =
    unitsQuery.isLoading
    || optionsQuery.isLoading
    || jobsQuery.isLoading
    || derivationsQuery.isLoading;

  const renderScriptRow = (unit: CommerceScriptUnit) => {
    const unitJobs = jobsByUnit.get(unit.id) ?? [];
    const latestJob = unitJobs[0];
    const activeJob = unitJobs.find((job) => activeJobStatuses.has(job.status));
    return (
      <ScriptVideoRow
        key={unit.id}
        unit={unit}
        derivation={scriptDerivationMetadata(unit)}
        jobs={unitJobs}
        latestJob={latestJob}
        activeJob={activeJob}
        canGenerate={Boolean(options && productReferences.length)}
        onEdit={() => setEditor({
          mode: "edit",
          unit,
          title: unit.title,
          content: unit.currentSourceVersion?.content ?? unit.draftContent,
        })}
        onGenerate={() => openGenerate(unit)}
        onHistory={() => setHistoryUnit(unit)}
        onArchive={() => setArchiveTarget(unit)}
        onPreview={(job) => setVideoPreview(job)}
      />
    );
  };

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">视频生成</h1>
          <p className="mt-1 text-sm text-muted-foreground">每条广告脚本独立生成一个视频，脚本文本会原样交给视频模型。</p>
        </div>
        <Button
          type="button"
          onClick={() => setEditor({ mode: "create", title: "", content: "" })}
          disabled={!productQuery.data?.currentVersion || !productReferences.length}
        >
          <Plus className="size-4" />
          新增广告脚本
        </Button>
      </div>

      {!productQuery.data?.currentVersion || !productReferences.length ? (
        <ProductConfigurationGate projectId={projectId} hasProduct={Boolean(productQuery.data?.currentVersion)} />
      ) : null}

      {optionsQuery.isError ? (
        <Surface className="border-destructive/40">
          <div className="flex flex-wrap items-center justify-between gap-3 p-4">
            <div>
              <p className="text-sm font-medium text-destructive">视频模型当前不可用</p>
              <p className="mt-1 text-sm text-muted-foreground">
                {userFacingErrorMessage(optionsQuery.error, "请检查视频业务模型配置")}
              </p>
            </div>
            <Button type="button" variant="outline" onClick={() => void optionsQuery.refetch()}>
              <RefreshCw className="size-4" />
              重新检查
            </Button>
          </div>
        </Surface>
      ) : null}

      {loading ? (
        <div className="space-y-3">
          {[1, 2, 3].map((item) => <Skeleton key={item} className="h-52 w-full" />)}
        </div>
      ) : units.length ? (
        <div className="space-y-4">
          {scriptBlocks.map((block) => {
            if (block.kind === "unit") {
              return <div key={block.key} className="border-y">{renderScriptRow(block.unit)}</div>;
            }
            const batchPending =
              batchCreateVideos.isPending
              && batchCreateVideos.variables?.batchId === block.batchId;
            const eligibleCount = block.units.filter(
              (unit) => !(jobsByUnit.get(unit.id) ?? []).some((job) => activeJobStatuses.has(job.status)),
            ).length;
            return (
              <section key={block.key} className="overflow-hidden border-y">
                <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/30 px-3 py-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant="secondary">脚本裂变批次</Badge>
                      <span className="truncate text-sm font-medium">
                        源自：{block.metadata.sourceScriptTitle || "原始广告脚本"}
                      </span>
                      <Badge variant="outline">
                        {commerceDerivationDimensionLabel(block.metadata.dimension)}
                      </Badge>
                      {block.batch ? (
                        <Badge variant="outline">
                          {commerceDerivationBatchStatusLabel(block.batch.status)}
                        </Badge>
                      ) : null}
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      已生成 {block.units.length} 个独立脚本
                    </p>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={
                      batchPending
                      || eligibleCount === 0
                      || !options
                      || productReferences.length === 0
                    }
                    onClick={() => batchCreateVideos.mutate(block)}
                  >
                    {batchPending ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                    {batchPending ? "正在提交" : `批量生成视频 (${eligibleCount})`}
                  </Button>
                </div>
                <div className="divide-y">
                  {block.units.map(renderScriptRow)}
                </div>
              </section>
            );
          })}
        </div>
      ) : (
        <Surface>
          <div className="flex min-h-72 flex-col items-center justify-center gap-3 p-6 text-center">
            <Film className="size-8 text-muted-foreground" />
            <div>
              <p className="font-medium">还没有广告脚本</p>
              <p className="mt-1 text-sm text-muted-foreground">新增脚本后即可直接生成视频。</p>
            </div>
            <Button
              type="button"
              onClick={() => setEditor({ mode: "create", title: "", content: "" })}
              disabled={!productQuery.data?.currentVersion || !productReferences.length}
            >
              <Plus className="size-4" />
              新增广告脚本
            </Button>
          </div>
        </Surface>
      )}

      <ScriptEditorDialog
        state={editor}
        promptConstraint={options?.scriptPromptConstraint}
        saving={saveScript.isPending}
        onChange={setEditor}
        onClose={() => setEditor(null)}
        onSave={() => editor && saveScript.mutate(editor)}
      />

      <GenerateVideoDialog
        projectId={projectId}
        state={generate}
        options={options}
        productReferences={productReferences}
        submitting={createVideo.isPending}
        onChange={setGenerate}
        onClose={() => setGenerate(null)}
        onSubmit={() => generate && createVideo.mutate(generate)}
      />

      <AlertDialog open={Boolean(archiveTarget)} onOpenChange={(open) => !open && setArchiveTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>归档广告脚本</AlertDialogTitle>
            <AlertDialogDescription>
              归档后脚本会从当前列表隐藏，历史视频仍会保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={archiveScript.isPending}
              onClick={(event) => {
                event.preventDefault();
                if (archiveTarget) archiveScript.mutate(archiveTarget);
              }}
            >
              {archiveScript.isPending ? <Loader2 className="size-4 animate-spin" /> : <Archive className="size-4" />}
              归档
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <VideoHistoryDialog
        unit={historyUnit}
        jobs={historyUnit ? jobsByUnit.get(historyUnit.id) ?? [] : []}
        onClose={() => setHistoryUnit(null)}
        onPreview={(job) => setVideoPreview(job)}
      />

      <VideoPreviewDialog job={videoPreview} onClose={() => setVideoPreview(null)} />
    </div>
  );
}

function ScriptVideoRow({
  unit,
  derivation,
  jobs,
  latestJob,
  activeJob,
  canGenerate,
  onEdit,
  onGenerate,
  onHistory,
  onArchive,
  onPreview,
}: {
  unit: CommerceScriptUnit;
  derivation: ScriptDerivationMetadataView | null;
  jobs: CommerceDirectVideoJob[];
  latestJob?: CommerceDirectVideoJob;
  activeJob?: CommerceDirectVideoJob;
  canGenerate: boolean;
  onEdit: () => void;
  onGenerate: () => void;
  onHistory: () => void;
  onArchive: () => void;
  onPreview: (job: CommerceDirectVideoJob) => void;
}) {
  const scriptChanged = Boolean(latestJob && latestJob.scriptUnitRevision !== unit.revision);
  const script = unit.currentSourceVersion?.content ?? unit.draftContent;
  const outputWarnings = latestJob?.outputWarnings ?? [];
  return (
    <article
      id={`commerce-script-${unit.id}`}
      className="grid scroll-mt-28 gap-4 bg-background py-4 lg:grid-cols-[minmax(0,1fr)_280px]"
    >
      <div className="min-w-0 px-1">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline">第 {unit.unitNo} 条</Badge>
          <h2 className="min-w-0 truncate text-base font-semibold">{unit.title}</h2>
          {derivation ? (
            <>
              <Badge variant="secondary">裂变脚本</Badge>
              <Badge variant="outline">{commerceDerivationDimensionLabel(derivation.dimension)}</Badge>
            </>
          ) : (
            <Badge variant="outline">原始脚本</Badge>
          )}
          {scriptChanged ? <Badge variant="secondary">脚本已更新</Badge> : null}
          {latestJob ? <DirectJobStatusBadge status={latestJob.status} /> : <Badge variant="outline">未生成</Badge>}
        </div>
        {derivation ? (
          <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="truncate">源自：{derivation.sourceScriptTitle || "原始广告脚本"}</span>
            {derivation.variationLabel ? <span>变体：{derivation.variationLabel}</span> : null}
          </div>
        ) : null}
        <p className="mt-3 line-clamp-4 whitespace-pre-wrap text-sm leading-6 text-muted-foreground">
          {script || "暂无脚本正文"}
        </p>
        {latestJob?.status === "failed" ? (
          <p className="mt-3 text-sm text-destructive">{latestJob.errorMessage || "视频生成失败"}</p>
        ) : null}
        {outputWarnings.map((warning, index) => (
          <div
            key={`${warning.code}:${index}`}
            role="status"
            className="mt-3 flex items-start gap-2 border border-amber-300/60 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200"
          >
            <TriangleAlert className="mt-0.5 size-4 shrink-0" />
            <span>{providerVideoWarningMessage(warning)}</span>
          </div>
        ))}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Button type="button" size="sm" variant="outline" onClick={onEdit}>
            <Pencil className="size-3.5" />
            编辑脚本
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canGenerate || Boolean(activeJob)}
            onClick={onGenerate}
          >
            {activeJob ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
            {activeJob ? "生成中" : "准备生成"}
          </Button>
          {jobs.length ? (
            <Button type="button" size="sm" variant="ghost" onClick={onHistory}>
              <History className="size-3.5" />
              历史 {jobs.length}
            </Button>
          ) : null}
          <Button
            type="button"
            size="icon"
            variant="ghost"
            className="ml-auto size-8 text-muted-foreground hover:text-destructive"
            onClick={onArchive}
            title="归档脚本"
          >
            <Trash2 className="size-4" />
          </Button>
        </div>
      </div>

      <div className="px-1 lg:border-l lg:pl-4">
        {latestJob?.status === "succeeded" && latestJob.outputPreviewUrl ? (
          <button
            type="button"
            className="group relative block aspect-video w-full overflow-hidden bg-black"
            onClick={() => onPreview(latestJob)}
            title="播放视频"
          >
            <video
              src={latestJob.outputPreviewUrl}
              className="h-full w-full object-contain"
              preload="metadata"
              muted
            />
            <span className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover:bg-black/20">
              <span className="rounded-full bg-background/90 p-3 shadow">
                <Play className="size-5 fill-current" />
              </span>
            </span>
          </button>
        ) : (
          <div className="flex aspect-video items-center justify-center border bg-muted/40">
            {activeJob ? (
              <div className="text-center">
                <Loader2 className="mx-auto size-6 animate-spin text-primary" />
                <p className="mt-2 text-sm">视频生成中</p>
              </div>
            ) : (
              <Film className="size-7 text-muted-foreground" />
            )}
          </div>
        )}
        {latestJob ? (
          <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
            <span>{latestJob.requestedDurationSeconds} 秒 · {latestJob.resolution}</span>
            <span>{formatDateTime(latestJob.createdAt)}</span>
          </div>
        ) : null}
      </div>
    </article>
  );
}

function ScriptEditorDialog({
  state,
  promptConstraint,
  saving,
  onChange,
  onClose,
  onSave,
}: {
  state: ScriptEditorState | null;
  promptConstraint?: CommercePromptLengthConstraint;
  saving: boolean;
  onChange: (state: ScriptEditorState | null) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const contentLength = state
    ? measureCommerceScriptLength(state.content, promptConstraint?.unit ?? "characters")
    : 0;
  const contentTooLong = commerceScriptExceedsPromptLimit(state?.content ?? "", promptConstraint);
  const lengthUnitLabel = commercePromptLengthUnitLabel(promptConstraint?.unit ?? "characters");
  return (
    <Dialog open={Boolean(state)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{state?.mode === "edit" ? "编辑广告脚本" : "新增广告脚本"}</DialogTitle>
          <DialogDescription>脚本将直接作为视频生成内容，不经过分镜或提示词改写。</DialogDescription>
        </DialogHeader>
        {state ? (
          <div className="min-h-0 space-y-4 overflow-y-auto pr-1">
            <div className="space-y-2">
              <Label htmlFor="commerce-script-title">脚本名称</Label>
              <Input
                id="commerce-script-title"
                value={state.title}
                onChange={(event) => onChange({ ...state, title: event.target.value })}
                placeholder="例如：头盔通勤场景广告"
              />
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <Label htmlFor="commerce-script-content">广告脚本</Label>
                {promptConstraint?.maxLength ? (
                  <span className={cn("text-xs tabular-nums", contentTooLong ? "text-destructive" : "text-muted-foreground")}>
                    {contentLength} / {promptConstraint.maxLength} {lengthUnitLabel}
                  </span>
                ) : null}
              </div>
              <Textarea
                id="commerce-script-content"
                className="field-sizing-fixed h-[clamp(14rem,50dvh,32rem)] min-h-56 max-h-[50dvh] resize-y overflow-y-auto overscroll-contain font-mono text-sm leading-6"
                value={state.content}
                aria-invalid={contentTooLong}
                onChange={(event) => onChange({ ...state, content: event.target.value })}
                placeholder="输入完整广告脚本，可使用任意语言。"
              />
              {contentTooLong ? (
                <p className="text-xs text-destructive">
                  已超过当前视频模型允许的脚本长度，请删减后保存。
                </p>
              ) : null}
            </div>
          </div>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>取消</Button>
          <Button
            type="button"
            disabled={saving || contentTooLong || !state?.title.trim() || !state?.content.trim()}
            onClick={onSave}
          >
            {saving ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
            保存脚本
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function GenerateVideoDialog({
  projectId,
  state,
  options,
  productReferences,
  submitting,
  onChange,
  onClose,
  onSubmit,
}: {
  projectId: string;
  state: GenerateState | null;
  options?: CommerceDirectVideoOptions;
  productReferences: CommerceProductReference[];
  submitting: boolean;
  onChange: (state: GenerateState | null) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const invalidate = useInvalidateKeys();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const unitId = state?.unit.id ?? "";
  const customReferencesQuery = useApiQuery({
    key: qk.commerceScriptReferences(projectId, unitId, "active"),
    queryFn: (session) => studioApi.listCommerceScriptReferences(session, projectId, unitId, "active"),
    enabled: Boolean(unitId),
  });
  const customReferences = customReferencesQuery.data?.items ?? [];

  const uploadReference = useApiMutation({
    mutationFn: async (session, file: File) => {
      if (!unitId) throw new Error("广告脚本不存在");
      const ticket = await studioApi.createCommerceScriptReferenceUpload(
        session,
        projectId,
        unitId,
        { fileName: file.name, mimeType: file.type },
        `commerce-script-reference-${unitId}-${crypto.randomUUID()}`,
      );
      await studioApi.uploadCommerceScriptReferenceFile(ticket, file);
      return studioApi.completeCommerceScriptReferenceUpload(session, projectId, unitId, ticket.uploadId);
    },
    onSuccess: (item) => {
      invalidate([qk.commerceScriptReferencesRoot(projectId, unitId)]);
      if (state) {
        const limit = options
          ? directRouteReferenceLimit(options, state.durationSeconds, state.resolution)
          : 0;
        if (
          limit > 0
          && state.selectedProductReferenceIds.length + state.selectedCustomReferenceIds.length < limit
        ) {
          onChange({
            ...state,
            selectedCustomReferenceIds: unique([...state.selectedCustomReferenceIds, item.id]),
          });
        }
      }
      toast.success("自定义参考图已上传");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "自定义参考图上传失败")),
  });

  const archiveReference = useApiMutation({
    mutationFn: (session, item: CommerceScriptReferenceImage) =>
      studioApi.archiveCommerceScriptReference(session, projectId, unitId, item.id, item.revision),
    onSuccess: (item) => {
      invalidate([qk.commerceScriptReferencesRoot(projectId, unitId)]);
      if (state) {
        onChange({
          ...state,
          selectedCustomReferenceIds: state.selectedCustomReferenceIds.filter((id) => id !== item.id),
        });
      }
      toast.success("自定义参考图已移除");
    },
    onError: (error) => toast.error(userFacingErrorMessage(error, "自定义参考图移除失败")),
  });

  const selectedCount =
    (state?.selectedProductReferenceIds.length ?? 0)
    + (state?.selectedCustomReferenceIds.length ?? 0);
  const maxReferenceCount = state && options
    ? directRouteReferenceLimit(options, state.durationSeconds, state.resolution)
    : 0;
  const compatibleResolutions = state && options
    ? directVideoResolutionsForDuration(options, state.durationSeconds)
    : [];

  const changeShape = (durationSeconds: number, requestedResolution: string) => {
    if (!state || !options) return;
    const resolutions = directVideoResolutionsForDuration(options, durationSeconds);
    const resolution = resolutions.includes(requestedResolution)
      ? requestedResolution
      : resolutions[0] ?? "";
    const limit = directRouteReferenceLimit(options, durationSeconds, resolution);
    onChange(trimGenerateReferences({ ...state, durationSeconds, resolution }, limit));
  };

  const toggleReference = (
    source: CommerceDirectVideoReferenceSelection["sourceType"],
    id: string,
    checked: boolean,
  ) => {
    if (!state) return;
    const next = source === "product"
      ? {
          ...state,
          selectedProductReferenceIds: toggleID(state.selectedProductReferenceIds, id, checked),
        }
      : {
          ...state,
          selectedCustomReferenceIds: toggleID(state.selectedCustomReferenceIds, id, checked),
        };
    const nextCount = next.selectedProductReferenceIds.length + next.selectedCustomReferenceIds.length;
    if (checked && (maxReferenceCount <= 0 || nextCount > maxReferenceCount)) {
      toast.info(`当前视频模型最多使用 ${maxReferenceCount} 张参考图`);
      return;
    }
    onChange(next);
  };

  return (
    <Dialog open={Boolean(state)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[92vh] overflow-hidden sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle>生成广告视频</DialogTitle>
          <DialogDescription>{state?.unit.title}</DialogDescription>
        </DialogHeader>
        {state ? (
          <ScrollArea className="max-h-[calc(92vh-170px)] pr-4">
            <div className="space-y-6 pb-2">
              <section className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2">
                  <Label>视频时长</Label>
                  <div className="flex flex-wrap gap-2">
                    {(options?.executableDurationSeconds ?? []).map((duration) => (
                      <Button
                        key={duration}
                        type="button"
                        size="sm"
                        variant={state.durationSeconds === duration ? "default" : "outline"}
                        onClick={() => changeShape(duration, state.resolution)}
                      >
                        <Clock3 className="size-3.5" />
                        {duration} 秒
                      </Button>
                    ))}
                  </div>
                </div>
                <div className="space-y-2">
                  <Label>分辨率</Label>
                  <Select
                    value={state.resolution}
                    onValueChange={(resolution) => changeShape(state.durationSeconds, resolution)}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="选择分辨率" />
                    </SelectTrigger>
                    <SelectContent>
                      {compatibleResolutions.map((resolution) => (
                        <SelectItem key={resolution} value={resolution}>{resolution}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </section>

              <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <h3 className="text-sm font-semibold">参考图</h3>
                    <p className="mt-1 text-sm text-muted-foreground">
                      已选 {selectedCount} 张
                      {maxReferenceCount > 0 ? `，当前模型最多使用 ${maxReferenceCount} 张` : ""}
                    </p>
                  </div>
                  <input
                    ref={fileInputRef}
                    type="file"
                    className="hidden"
                    accept="image/jpeg,image/png,image/webp"
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      event.currentTarget.value = "";
                      if (file) uploadReference.mutate(file);
                    }}
                  />
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={uploadReference.isPending}
                    onClick={() => fileInputRef.current?.click()}
                  >
                    {uploadReference.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Upload className="size-3.5" />}
                    上传自定义图片
                  </Button>
                </div>

                <div className="space-y-4">
                  <ReferenceGroup
                    title="商品图片"
                    items={productReferences.map((item) => ({
                      id: item.id,
                      previewUrl: item.previewUrl,
                      label: item.isPrimary ? "商品主图" : "商品参考图",
                    }))}
                    selectedIds={state.selectedProductReferenceIds}
                    onToggle={(id, checked) => toggleReference("product", id, checked)}
                    onSelectAll={(selected) =>
                      onChange(trimGenerateReferences({
                        ...state,
                        selectedProductReferenceIds: selected ? productReferences.map((item) => item.id) : [],
                      }, maxReferenceCount))
                    }
                  />
                  <ReferenceGroup
                    title="自定义图片"
                    emptyText="未上传自定义参考图"
                    loading={customReferencesQuery.isLoading}
                    items={customReferences.map((item) => ({
                      id: item.id,
                      previewUrl: item.previewUrl,
                      label: item.fileName,
                      removable: true,
                    }))}
                    selectedIds={state.selectedCustomReferenceIds}
                    removingId={
                      archiveReference.isPending ? archiveReference.variables?.id : undefined
                    }
                    onToggle={(id, checked) => toggleReference("custom", id, checked)}
                    onSelectAll={(selected) =>
                      onChange(trimGenerateReferences({
                        ...state,
                        selectedCustomReferenceIds: selected ? customReferences.map((item) => item.id) : [],
                      }, maxReferenceCount))
                    }
                    onRemove={(id) => {
                      const item = customReferences.find((reference) => reference.id === id);
                      if (item) archiveReference.mutate(item);
                    }}
                  />
                </div>
              </section>

              <section className="border-t pt-4">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <Volume2 className="size-4" />
                  使用视频模型原生音频
                </div>
                <p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-muted-foreground">
                  {state.unit.currentSourceVersion?.content ?? state.unit.draftContent}
                </p>
              </section>
            </div>
          </ScrollArea>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>取消</Button>
          <Button
            type="button"
            disabled={
              submitting
              || !state?.durationSeconds
              || !state?.resolution
              || selectedCount === 0
              || maxReferenceCount <= 0
              || selectedCount > maxReferenceCount
            }
            onClick={onSubmit}
          >
            {submitting ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
            开始生成
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ReferenceGroup({
  title,
  items,
  selectedIds,
  emptyText = "暂无图片",
  loading = false,
  removingId,
  onToggle,
  onSelectAll,
  onRemove,
}: {
  title: string;
  items: Array<{
    id: string;
    previewUrl?: string;
    label: string;
    removable?: boolean;
  }>;
  selectedIds: string[];
  emptyText?: string;
  loading?: boolean;
  removingId?: string;
  onToggle: (id: string, checked: boolean) => void;
  onSelectAll: (selected: boolean) => void;
  onRemove?: (id: string) => void;
}) {
  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <Label>{title}</Label>
        {items.length ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => onSelectAll(!items.every((item) => selectedIds.includes(item.id)))}
          >
            {items.every((item) => selectedIds.includes(item.id)) ? "取消全选" : "全选"}
          </Button>
        ) : null}
      </div>
      {loading ? (
        <div className="flex gap-2">
          {[1, 2, 3].map((item) => <Skeleton key={item} className="h-28 w-32" />)}
        </div>
      ) : items.length ? (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-5">
          {items.map((item) => {
            const selected = selectedIds.includes(item.id);
            return (
              <article
                key={item.id}
                className={cn(
                  "relative overflow-hidden border bg-background",
                  selected && "border-primary ring-1 ring-primary",
                )}
              >
                <button
                  type="button"
                  className="relative block aspect-square w-full bg-muted"
                  onClick={() => onToggle(item.id, !selected)}
                >
                  {item.previewUrl ? (
                    <NextImage
                      src={item.previewUrl}
                      alt={item.label}
                      fill
                      unoptimized
                      className="object-cover"
                      sizes="160px"
                    />
                  ) : (
                    <span className="flex h-full items-center justify-center">
                      <ImagePlus className="size-5 text-muted-foreground" />
                    </span>
                  )}
                  <span className={cn(
                    "absolute left-2 top-2 flex size-5 items-center justify-center rounded-sm border bg-background/90",
                    selected && "border-primary bg-primary text-primary-foreground",
                  )}>
                    {selected ? <Check className="size-3.5" /> : null}
                  </span>
                </button>
                <div className="flex h-9 items-center gap-1 px-2">
                  <span className="min-w-0 flex-1 truncate text-xs">{item.label}</span>
                  {item.removable && onRemove ? (
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
                      disabled={removingId === item.id}
                      onClick={() => onRemove(item.id)}
                      title="移除自定义图片"
                    >
                      {removingId === item.id ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
                    </Button>
                  ) : null}
                </div>
              </article>
            );
          })}
        </div>
      ) : (
        <div className="flex h-24 items-center justify-center border border-dashed text-sm text-muted-foreground">
          {emptyText}
        </div>
      )}
    </div>
  );
}

function ProductConfigurationGate({ projectId, hasProduct }: { projectId: string; hasProduct: boolean }) {
  return (
    <Surface className="border-amber-500/40">
      <div className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div>
          <p className="text-sm font-medium">{hasProduct ? "请上传商品参考图" : "请先完成商品配置"}</p>
          <p className="mt-1 text-sm text-muted-foreground">
            视频生成至少需要一张商品图片。
          </p>
        </div>
        <Button asChild variant="outline">
          <Link href={projectHref(projectId, "commerce/materials") as Route}>
            打开商品配置
            <ChevronRight className="size-4" />
          </Link>
        </Button>
      </div>
    </Surface>
  );
}

function VideoHistoryDialog({
  unit,
  jobs,
  onClose,
  onPreview,
}: {
  unit: CommerceScriptUnit | null;
  jobs: CommerceDirectVideoJob[];
  onClose: () => void;
  onPreview: (job: CommerceDirectVideoJob) => void;
}) {
  return (
    <Dialog open={Boolean(unit)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>视频历史</DialogTitle>
          <DialogDescription>{unit?.title}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[68vh]">
          <div className="divide-y pr-4">
            {jobs.map((job) => (
              <div key={job.id} className="grid gap-3 py-3 sm:grid-cols-[160px_minmax(0,1fr)_auto] sm:items-center">
                <div className="flex aspect-video items-center justify-center overflow-hidden bg-muted">
                  {job.status === "succeeded" && job.outputPreviewUrl ? (
                    <video src={job.outputPreviewUrl} className="h-full w-full object-contain" preload="metadata" muted />
                  ) : activeJobStatuses.has(job.status) ? (
                    <Loader2 className="size-5 animate-spin text-primary" />
                  ) : (
                    <Film className="size-5 text-muted-foreground" />
                  )}
                </div>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <DirectJobStatusBadge status={job.status} />
                    <span className="text-sm">{job.requestedDurationSeconds} 秒 · {job.resolution}</span>
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">{formatDateTime(job.createdAt)}</p>
                  {job.errorMessage ? <p className="mt-1 text-sm text-destructive">{job.errorMessage}</p> : null}
                </div>
                {job.outputPreviewUrl ? (
                  <Button type="button" size="sm" variant="outline" onClick={() => onPreview(job)}>
                    <Play className="size-3.5" />
                    播放
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function VideoPreviewDialog({
  job,
  onClose,
}: {
  job: CommerceDirectVideoJob | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={Boolean(job)} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle>广告视频</DialogTitle>
          <DialogDescription>
            {job ? `${job.requestedDurationSeconds} 秒 · ${job.resolution}` : ""}
          </DialogDescription>
        </DialogHeader>
        {job?.outputWarnings?.map((warning, index) => (
          <div
            key={`${warning.code}:${index}`}
            role="status"
            className="flex items-start gap-2 border border-amber-300/60 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200"
          >
            <TriangleAlert className="mt-0.5 size-4 shrink-0" />
            <span>{providerVideoWarningMessage(warning)}</span>
          </div>
        ))}
        {job?.outputPreviewUrl ? (
          <div className="flex max-h-[76vh] min-h-80 items-center justify-center bg-black">
            <video
              src={job.outputPreviewUrl}
              className="max-h-[76vh] max-w-full"
              controls
              autoPlay
              preload="auto"
            />
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function DirectJobStatusBadge({ status }: { status: CommerceDirectVideoJob["status"] }) {
  const style =
    status === "succeeded"
      ? "border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300"
      : status === "failed" || status === "cancelled"
        ? "border-destructive/30 bg-destructive/10 text-destructive"
        : "border-sky-300 bg-sky-50 text-sky-700 dark:border-sky-900 dark:bg-sky-950 dark:text-sky-300";
  return (
    <Badge variant="outline" className={cn("gap-1", style)}>
      {activeJobStatuses.has(status) ? <Loader2 className="size-3 animate-spin" /> : null}
      {directJobStatusLabel(status)}
    </Badge>
  );
}

function groupJobsByUnit(jobs: CommerceDirectVideoJob[]) {
  const result = new Map<string, CommerceDirectVideoJob[]>();
  for (const job of jobs) {
    const current = result.get(job.scriptUnitId) ?? [];
    current.push(job);
    result.set(job.scriptUnitId, current);
  }
  for (const items of result.values()) {
    items.sort((left, right) => Date.parse(right.createdAt) - Date.parse(left.createdAt));
  }
  return result;
}

function buildScriptDisplayBlocks(
  units: CommerceScriptUnit[],
  batches: CommerceScriptDerivationBatch[],
): ScriptDisplayBlock[] {
  const latestBatchByRoot = new Map<string, CommerceScriptDerivationBatch>();
  for (const batch of [...batches].sort(
    (left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt),
  )) {
    const rootID = batch.rootBatchId || batch.id;
    if (!latestBatchByRoot.has(rootID)) latestBatchByRoot.set(rootID, batch);
  }
  const derivationBlocks = new Map<string, ScriptDerivationBlock>();
  const blocks: ScriptDisplayBlock[] = [];
  for (const unit of units) {
    const metadata = scriptDerivationMetadata(unit);
    if (!metadata) {
      blocks.push({ kind: "unit", key: `unit:${unit.id}`, unit });
      continue;
    }
    const batchID = metadata.rootBatchId || metadata.batchId;
    const existing = derivationBlocks.get(batchID);
    if (existing) {
      existing.units.push(unit);
      continue;
    }
    const block: ScriptDerivationBlock = {
      kind: "derivation",
      key: `derivation:${batchID}`,
      batchId: batchID,
      metadata,
      batch: latestBatchByRoot.get(batchID),
      units: [unit],
    };
    derivationBlocks.set(batchID, block);
    blocks.push(block);
  }
  return blocks;
}

function scriptDerivationMetadata(unit: CommerceScriptUnit): ScriptDerivationMetadataView | null {
  const raw = recordValue(unit.metadata.scriptDerivation);
  const batchId = stringRecordValue(raw, "batchId");
  if (!raw || !batchId) return null;
  return {
    batchId,
    rootBatchId: stringRecordValue(raw, "rootBatchId"),
    itemId: stringRecordValue(raw, "itemId"),
    sourceScriptUnitId: stringRecordValue(raw, "sourceScriptUnitId"),
    sourceScriptTitle: stringRecordValue(raw, "sourceScriptTitle"),
    dimension: stringRecordValue(raw, "dimension"),
    variationKey: stringRecordValue(raw, "variationKey"),
    variationLabel: stringRecordValue(raw, "variationLabel"),
    variationBrief: stringRecordValue(raw, "variationBrief"),
  };
}

function recordValue(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function stringRecordValue(value: Record<string, unknown> | null, key: string) {
  const candidate = value?.[key];
  return typeof candidate === "string" ? candidate : "";
}

function commerceDerivationDimensionLabel(value: string) {
  switch (value) {
    case "scene":
      return "场景";
    case "hook":
      return "开场钩子";
    case "audience":
      return "受众";
    case "tone":
      return "表达语气";
    case "language":
      return "语言";
    case "cta":
      return "行动号召";
    case "custom":
      return "自定义";
    default:
      return value || "脚本裂变";
  }
}

function commerceDerivationBatchStatusLabel(status: CommerceScriptDerivationBatch["status"]) {
  switch (status) {
    case "queued":
      return "等待执行";
    case "running":
      return "运行中";
    case "partial_succeeded":
      return "部分完成";
    case "succeeded":
      return "已完成";
    case "failed":
      return "失败";
    case "cancelling":
      return "取消中";
    case "cancelled":
      return "已取消";
  }
}

function defaultGenerateState(
  unit: CommerceScriptUnit,
  options: CommerceDirectVideoOptions,
  productReferences: CommerceProductReference[],
): GenerateState | null {
  const durationSeconds = maximumExecutableDuration(options.executableDurationSeconds);
  if (!durationSeconds) return null;
  const compatibleResolutions = directVideoResolutionsForDuration(options, durationSeconds);
  const resolution = compatibleResolutions.includes(options.defaultResolution)
    ? options.defaultResolution
    : compatibleResolutions[0] ?? "";
  if (!resolution) return null;
  const referenceLimit = directRouteReferenceLimit(options, durationSeconds, resolution);
  if (referenceLimit <= 0 || productReferences.length === 0) return null;
  return {
    unit,
    durationSeconds,
    resolution,
    selectedProductReferenceIds: productReferences.slice(0, referenceLimit).map((item) => item.id),
    selectedCustomReferenceIds: [],
  };
}

async function mapWithConcurrency<TInput, TOutput>(
  values: TInput[],
  concurrency: number,
  run: (value: TInput, index: number) => Promise<TOutput>,
) {
  const results = new Array<TOutput>(values.length);
  let nextIndex = 0;
  const workerCount = Math.min(Math.max(concurrency, 1), values.length);
  await Promise.all(
    Array.from({ length: workerCount }, async () => {
      while (nextIndex < values.length) {
        const index = nextIndex;
        nextIndex += 1;
        results[index] = await run(values[index], index);
      }
    }),
  );
  return results;
}

function directJobStatusLabel(status: CommerceDirectVideoJob["status"]) {
  switch (status) {
    case "queued":
      return "排队中";
    case "running":
      return "生成中";
    case "succeeded":
      return "已完成";
    case "failed":
      return "失败";
    case "cancelling":
      return "取消中";
    case "cancelled":
      return "已取消";
  }
}

function directRouteReferenceLimit(
  options: CommerceDirectVideoOptions,
  durationSeconds: number,
  resolution: string,
) {
  const route = options.routes.find(
    (candidate) =>
      candidate.executableDurationSeconds.includes(durationSeconds)
      && candidate.resolutions.includes(resolution),
  );
  if (!route) return 0;
  return route.inputContract.slots
    .filter((slot) => slot.mediaType === "image")
    .reduce((sum, slot) => sum + Math.max(slot.max, 0), 0);
}

function directVideoResolutionsForDuration(
  options: CommerceDirectVideoOptions,
  durationSeconds: number,
) {
  return unique(
    options.routes
      .filter((route) => route.executableDurationSeconds.includes(durationSeconds))
      .flatMap((route) => route.resolutions),
  ).sort((left, right) => left.localeCompare(right, "zh-CN", { numeric: true }));
}

function trimGenerateReferences(state: GenerateState, maximumCount: number): GenerateState {
  const limit = Math.max(maximumCount, 0);
  const productReferences = state.selectedProductReferenceIds.slice(0, limit);
  const customCapacity = Math.max(limit - productReferences.length, 0);
  return {
    ...state,
    selectedProductReferenceIds: productReferences,
    selectedCustomReferenceIds: state.selectedCustomReferenceIds.slice(0, customCapacity),
  };
}

function toggleID(values: string[], id: string, checked: boolean) {
  return checked ? unique([...values, id]) : values.filter((value) => value !== id);
}

function unique(values: string[]) {
  return [...new Set(values)];
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}
