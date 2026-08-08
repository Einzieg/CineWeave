"use client";

import NextImage from "next/image";
import { useMemo, useState } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { ArrowRight, ChevronLeft, ChevronRight, Clock3, Combine, Film, Image as ImageIcon, Loader2, RefreshCw, Save, Scissors, SkipForward, Trash2, Video, WandSparkles } from "lucide-react";
import { toast } from "sonner";
import { Surface } from "@/components/layout/app-shell";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { studioApi } from "@/lib/api-client";
import { cssAspectRatio } from "@/lib/aspect-ratio";
import { localizePlatformError } from "@/lib/error-localization";
import { assetTypeLabel, requirementTypeLabel, statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import { currentProjectScript } from "@/lib/scripts";
import { secondsToFrameTicks, wholeSecondDuration } from "@/lib/timing";
import { cn } from "@/lib/utils";
import type { ScriptTimingAnalysis, ShotProductionShot, StoryboardPlan, StoryboardShot, StoryboardShotDetail, StoryboardShotRequirementDetail, WorkflowRun } from "@/lib/types";
import { ShotImageDetailDialog } from "./shot-image-detail-dialog";

type ShotRow = {
  id: string;
  workflowRunId: string;
  storyboardPlanId?: string;
  scriptSceneId?: string;
  scriptEpisodeId?: string;
  episodeIndex?: number;
  episodeShotIndex?: number;
  episodeTitle?: string;
  shotNo: number;
  shotIndex: number;
  title?: string;
	startTick?: number;
	endTick?: number;
	plannedDurationTicks?: number;
	timelineTimebase?: number;
	fpsNumerator?: number;
	fpsDenominator?: number;
  durationSeconds?: number;
  visual?: string;
  camera?: string;
  motion?: string;
  mood?: string;
  imagePrompt?: string;
  imagePromptStatus?: string;
  imagePromptErrorCode?: string;
  imagePromptErrorMessage?: string;
  videoPrompt?: string;
  imageArtifactId?: string;
  videoArtifactId?: string;
  imagePreviewUrl?: string;
  videoPreviewUrl?: string;
  imageStatus?: string;
  videoStatus?: string;
  imageErrorCode?: string;
  imageErrorMessage?: string;
  videoErrorCode?: string;
  videoErrorMessage?: string;
  staleState?: string;
  canGenerateImage?: boolean;
  canGenerateImagePrompt?: boolean;
  canGenerateVideo?: boolean;
  canRetryImage?: boolean;
  canRetryVideo?: boolean;
};

type ShotDraft = {
  durationSeconds: string;
  visual: string;
  camera: string;
  motion: string;
  mood: string;
  imagePrompt: string;
  videoPrompt: string;
};

type RequirementDraft = {
  costume: string;
  pose: string;
  expression: string;
  action: string;
  cameraRelation: string;
  sceneState: string;
  propState: string;
  prompt: string;
};

type DraftEntry<TDraft> = { key: string; draft: TDraft };
type ShotDraftEntry = DraftEntry<ShotDraft> & { durationEdited: boolean };
type ShotFilter = "all" | "missing" | "running" | "ready" | "failed";

const EMPTY_SHOT_DRAFT: ShotDraft = {
  durationSeconds: "",
  visual: "",
  camera: "",
  motion: "",
  mood: "",
  imagePrompt: "",
  videoPrompt: "",
};

const ACTIVE_WORKFLOW_STATUSES = new Set(["pending", "queued", "running", "cancelling"]);

export function StoryboardPage({
  projectId,
  initialShotId = "",
  initialRequirementId = "",
}: {
  projectId: string;
  initialShotId?: string;
  initialRequirementId?: string;
}) {
  const invalidate = useInvalidateKeys();
  const pollingFallback = useProjectPollingFallback(projectId);
  const [selectedEpisodeId, setSelectedEpisodeId] = useState("");
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [selectedShotId, setSelectedShotId] = useState(initialShotId);
  const [shotFilter, setShotFilter] = useState<ShotFilter>("all");
  const [inspectorTab, setInspectorTab] = useState(initialRequirementId ? "assets" : "shot");
  const [shotDraftEntry, setShotDraftEntry] = useState<ShotDraftEntry | null>(null);
  const [requirementDrafts, setRequirementDrafts] = useState<Record<string, DraftEntry<RequirementDraft>>>({});
  const [shotToDelete, setShotToDelete] = useState<ShotRow | null>(null);
  const [imageDetailShotId, setImageDetailShotId] = useState("");

  const { data: scripts = [], isLoading: scriptsLoading } = useApiQuery({
    key: qk.scripts(projectId),
    queryFn: (session) => studioApi.listScripts(session, projectId).then((response) => response.items || []),
  });
  const activeScript = useMemo(() => currentProjectScript(scripts), [scripts]);
  const activeVersionId = activeScript?.currentVersionId ?? activeScript?.currentVersion?.id ?? "";
  const { data: episodes = [], isLoading: episodesLoading } = useApiQuery({
    key: qk.scriptEpisodes(projectId, activeScript?.id ?? "none", activeVersionId),
    queryFn: (session) => studioApi.listScriptEpisodes(session, projectId, activeScript!.id, activeVersionId).then((response) => response.items || []),
    enabled: !!activeScript?.id && !!activeVersionId,
  });
  const effectiveEpisodeId = episodes.some((episode) => episode.id === selectedEpisodeId) ? selectedEpisodeId : episodes[0]?.id ?? "";
  const selectedEpisode = episodes.find((episode) => episode.id === effectiveEpisodeId) ?? null;
  const selectedEpisodePosition = selectedEpisode ? episodes.findIndex((episode) => episode.id === selectedEpisode.id) : -1;

  const { data: timingAnalysis, isFetching: timingFetching } = useApiQuery({
    key: qk.scriptEpisodeTiming(projectId, effectiveEpisodeId || "none"),
    queryFn: (session) => studioApi.getScriptEpisodeTiming(session, projectId, effectiveEpisodeId),
    enabled: !!effectiveEpisodeId,
    retry: false,
  });
  const { data: storyboardPlans = [] } = useApiQuery({
    key: qk.storyboardPlans(projectId, effectiveEpisodeId || "none"),
    queryFn: (session) => studioApi.listStoryboardPlans(session, projectId, effectiveEpisodeId).then((response) => response.items || []),
    enabled: !!effectiveEpisodeId,
    refetchInterval: (query) =>
      pollingFallback && query.state.data?.some((plan) => ["planning", "reviewing"].includes(plan.status)) ? 5000 : false,
  });
  const activePlan = storyboardPlans.find((plan) => plan.active) ?? null;
  const effectivePlanId = storyboardPlans.some((plan) => plan.id === selectedPlanId)
    ? selectedPlanId
    : activePlan?.id ?? storyboardPlans[0]?.id ?? "";
  const selectedPlanSummary = storyboardPlans.find((plan) => plan.id === effectivePlanId) ?? null;
  const { data: selectedPlanDetail } = useApiQuery({
    key: qk.storyboardPlan(projectId, effectivePlanId || "none"),
    queryFn: (session) => studioApi.getStoryboardPlan(session, projectId, effectivePlanId),
    enabled: !!effectivePlanId,
    refetchInterval: pollingFallback && selectedPlanSummary && ["planning", "reviewing"].includes(selectedPlanSummary.status) ? 5000 : false,
  });

  const { data: workflowRunPage } = useApiQuery({
    key: qk.workflowRuns(projectId, { status: "active", limit: 100 }),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId, { status: "active", limit: 100 }),
    refetchInterval: (query) =>
      pollingFallback && query.state.data?.items.some((run) => isActiveStoryboardRun(run, effectiveEpisodeId)) ? 5000 : false,
  });
  const workflowRuns = workflowRunPage?.items ?? [];
  const activeStoryboardRun = workflowRuns.find((run) => isActiveStoryboardRun(run, effectiveEpisodeId));
  const { data: shotProduction, isLoading: productionLoading } = useApiQuery({
    key: qk.shotProduction(projectId, effectiveEpisodeId, effectivePlanId),
    queryFn: (session) =>
      studioApi.getShotProductionStatus(session, projectId, {
        scriptEpisodeId: effectiveEpisodeId,
        storyboardPlanId: effectivePlanId,
        includePreviewUrl: true,
        previewExpiresSeconds: 900,
      }),
    enabled: !!effectiveEpisodeId && !!effectivePlanId,
    refetchInterval: (query) => pollingFallback && (activeStoryboardRun || (query.state.data?.summary.running ?? 0) > 0) ? 5000 : false,
  });

  const productionByShotId = useMemo(
    () => new Map((shotProduction?.shots ?? []).map((shot) => [shot.id, shot] as const)),
    [shotProduction?.shots],
  );
  const rows = useMemo(
    () => (selectedPlanDetail?.shots ?? [])
      .map((shot) => rowFromPlanShot(shot, productionByShotId.get(shot.id)))
      .sort(compareEpisodeShots),
    [productionByShotId, selectedPlanDetail?.shots],
  );
  const storyboardAspectRatio = shotProduction?.aspectRatio || "16:9";
  const filteredRows = useMemo(() => rows.filter((shot) => shotMatchesFilter(shot, shotFilter)), [rows, shotFilter]);
  const selectedRow = rows.find((shot) => shot.id === selectedShotId) ?? filteredRows[0] ?? rows[0] ?? null;
  const selectedId = selectedRow?.id ?? "";
  const { data: shotDetail, isLoading: detailLoading } = useApiQuery({
    key: qk.shotDetail(projectId, selectedId || "none"),
    queryFn: (session) => studioApi.getStoryboardShotDetail(session, projectId, selectedId),
    enabled: !!selectedId,
  });

  const selectedShot = shotDetail?.shot ?? selectedRow;
  const inspectingActivePlan = !selectedPlanSummary || selectedPlanSummary.active;
  const imageDetailRow = rows.find((shot) => shot.id === imageDetailShotId) ?? null;
  const selectedIndex = selectedRow ? rows.findIndex((shot) => shot.id === selectedRow.id) : -1;
  const selectedShotDraftKey = selectedShot ? shotDraftKey(selectedShot) : "";
  const shotDraft = selectedShot && shotDraftEntry?.key === selectedShotDraftKey
    ? shotDraftEntry.draft
    : selectedShot
      ? draftFromShot(selectedShot)
      : EMPTY_SHOT_DRAFT;

  const generateEpisodeMutation = useApiMutation({
    mutationFn: (session) => studioApi.generateStoryboard(session, projectId, activeScript!.id, {
      scriptEpisodeIds: [effectiveEpisodeId],
      pacingProfile: "standard",
      plannerBatchMaxShots: 12,
      maxSceneConcurrency: 3,
      generateDerivedAssets: false,
    }),
    onSuccess: () => {
      toast.success(rows.length > 0 ? "本集分镜重新生成中" : "本集分镜生成中");
      setSelectedShotId("");
      invalidate([
        qk.workflowRuns(projectId),
        qk.shotProductionPrefix(projectId),
        qk.productionStatus(projectId),
        qk.scriptEpisodeTiming(projectId, effectiveEpisodeId),
        qk.storyboardPlans(projectId, effectiveEpisodeId),
      ]);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const analyzeTimingMutation = useApiMutation({
    mutationFn: (session) => studioApi.analyzeScriptEpisodeTiming(session, projectId, effectiveEpisodeId),
    onSuccess: () => {
      toast.success("时长分析已启动");
      invalidate([qk.workflowRuns(projectId), qk.scriptEpisodeTiming(projectId, effectiveEpisodeId)]);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const activatePlanMutation = useApiMutation({
    mutationFn: (session, planId: string) => studioApi.activateStoryboardPlan(session, projectId, planId),
    onSuccess: (plan) => {
      toast.success(`已激活分镜版本 ${plan.revision}`);
      setSelectedPlanId(plan.id);
      refreshStoryboard(projectId, "", invalidate);
      invalidate([qk.storyboardPlans(projectId, effectiveEpisodeId), qk.storyboardPlan(projectId, plan.id)]);
    },
    onError: (error) => toast.error("激活失败：" + error.message),
  });

  const productionMutation = useApiMutation({
    mutationFn: (session, payload: { action: string; shotIds: string[] }) =>
      studioApi.runShotProductionAction(session, projectId, {
        action: payload.action,
        shotIds: payload.shotIds,
        options: { maxConcurrency: payload.shotIds.length === 1 ? 1 : 5 },
      }),
    onSuccess: () => {
      toast.success("任务已启动");
      refreshStoryboard(projectId, selectedId, invalidate);
    },
    onError: (error) => toast.error("启动失败：" + error.message),
  });

  const updateShotMutation = useApiMutation({
    mutationFn: async (session, payload: { shotId: string; draft: ShotDraft; mode: "shot" | "prompts"; durationEdited?: boolean }) => {
      const current = rows.find((shot) => shot.id === payload.shotId);
      if (payload.mode === "prompts") {
        return studioApi.updateStoryboardShot(session, projectId, payload.shotId, shotPromptUpdateBody(payload.draft));
      }
      await studioApi.updateStoryboardShot(session, projectId, payload.shotId, shotVisualUpdateBody(payload.draft));
      if (payload.durationEdited) {
        const durationSeconds = wholeSecondDuration(Number.parseFloat(payload.draft.durationSeconds.trim()));
        const durationTicks = secondsToFrameTicks(durationSeconds, current);
        if (current?.startTick != null && durationTicks > 0 && durationTicks !== current.plannedDurationTicks) {
          return studioApi.updateStoryboardShotTiming(session, projectId, payload.shotId, { endTick: current.startTick + durationTicks });
        }
      }
      return null;
    },
    onSuccess: (data, payload) => {
      if (data && "plan" in data) {
        setSelectedPlanId(data.plan.id);
        setSelectedShotId("");
        toast.success(`已创建待激活分镜版本 ${data.plan.revision}`);
        invalidate([qk.storyboardPlans(projectId, effectiveEpisodeId), qk.storyboardPlan(projectId, data.plan.id)]);
      } else {
        toast.success("镜头已保存");
      }
      setShotDraftEntry(null);
      refreshStoryboard(projectId, payload.shotId, invalidate);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const splitShotMutation = useApiMutation({
    mutationFn: (session, shot: ShotRow) => studioApi.splitStoryboardShot(session, projectId, shot.id, { splitTick: midpointFrameTick(shot) }),
    onSuccess: (data) => {
      setSelectedPlanId(data.plan.id);
      setSelectedShotId("");
      toast.success(`已创建拆镜版本 ${data.plan.revision}`);
      invalidate([qk.storyboardPlans(projectId, effectiveEpisodeId), qk.storyboardPlan(projectId, data.plan.id)]);
    },
    onError: (error) => toast.error("拆分失败：" + error.message),
  });

  const mergeShotMutation = useApiMutation({
    mutationFn: (session, payload: { shotId: string; nextShotId: string }) => studioApi.mergeStoryboardShots(session, projectId, { shotIds: [payload.shotId, payload.nextShotId] }),
    onSuccess: (data) => {
      setSelectedPlanId(data.plan.id);
      setSelectedShotId("");
      toast.success(`已创建合镜版本 ${data.plan.revision}`);
      invalidate([qk.storyboardPlans(projectId, effectiveEpisodeId), qk.storyboardPlan(projectId, data.plan.id)]);
    },
    onError: (error) => toast.error("合并失败：" + error.message),
  });

  const deleteShotMutation = useApiMutation({
    mutationFn: (session, shotId: string) => studioApi.deleteStoryboardShot(session, projectId, shotId),
    onSuccess: () => {
      toast.success("镜头已删除");
      setShotToDelete(null);
      setSelectedShotId("");
      refreshStoryboard(projectId, "", invalidate);
    },
    onError: (error) => toast.error("删除失败：" + error.message),
  });

  const updateRequirementMutation = useApiMutation({
    mutationFn: (session, payload: { requirementId: string; draft: RequirementDraft }) =>
      studioApi.updateShotAssetRequirement(session, projectId, payload.requirementId, requirementUpdateBody(payload.draft)),
    onSuccess: (_data, payload) => {
      toast.success("资产需求已保存");
      setRequirementDrafts((drafts) => {
        const next = { ...drafts };
        delete next[payload.requirementId];
        return next;
      });
      refreshStoryboard(projectId, selectedId, invalidate);
      invalidate([qk.requirements(projectId)]);
    },
    onError: (error) => toast.error("保存失败：" + error.message),
  });

  const skipRequirementMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.skipShotAssetRequirement(session, projectId, requirementId),
    onSuccess: () => {
      toast.success("资产需求已跳过");
      refreshStoryboard(projectId, selectedId, invalidate);
      invalidate([qk.requirements(projectId)]);
    },
    onError: (error) => toast.error("跳过失败：" + error.message),
  });

  const generateDerivedMutation = useApiMutation({
    mutationFn: (session, requirementId: string) => studioApi.generateDerivedAssetImage(session, projectId, requirementId),
    onSuccess: (result) => {
      toast.success("衍生资产图任务已创建");
      refreshStoryboard(projectId, selectedId, invalidate);
      invalidate([
        qk.workflowRuns(projectId),
        qk.workflowDerivedAssetBatch(result.workflowRun.id),
        qk.requirements(projectId),
        qk.assetsRoot(projectId),
        qk.artifacts(projectId),
      ]);
    },
    onError: (error) => toast.error("生成失败：" + error.message),
  });

  const loading = scriptsLoading || episodesLoading || (!!effectiveEpisodeId && productionLoading);
  const deleteImpactItems = shotToDelete ? buildDeleteImpactItems(shotToDelete, shotDetail?.shot.id === shotToDelete.id ? shotDetail : undefined) : [];

  function selectEpisode(episodeId: string) {
    setSelectedEpisodeId(episodeId);
    setSelectedPlanId("");
    setSelectedShotId("");
    setShotDraftEntry(null);
    setRequirementDrafts({});
    setInspectorTab("shot");
    setImageDetailShotId("");
  }

  function moveEpisode(offset: number) {
    const next = episodes[selectedEpisodePosition + offset];
    if (next) {
      selectEpisode(next.id);
    }
  }

  function setShotDraftField(field: keyof ShotDraft, value: string) {
    if (!selectedShot) return;
    const key = shotDraftKey(selectedShot);
    const base = shotDraftEntry?.key === key ? shotDraftEntry.draft : draftFromShot(selectedShot);
    setShotDraftEntry({
      key,
      draft: { ...base, [field]: value },
      durationEdited: (shotDraftEntry?.key === key && shotDraftEntry.durationEdited) || field === "durationSeconds",
    });
  }

  function requirementDraft(requirement: StoryboardShotRequirementDetail) {
    const key = requirementDraftKey(requirement);
    return requirementDrafts[requirement.id]?.key === key ? requirementDrafts[requirement.id].draft : draftFromRequirement(requirement);
  }

  function setRequirementDraftField(requirement: StoryboardShotRequirementDetail, field: keyof RequirementDraft, value: string) {
    const key = requirementDraftKey(requirement);
    const base = requirementDrafts[requirement.id]?.key === key ? requirementDrafts[requirement.id].draft : draftFromRequirement(requirement);
    setRequirementDrafts((drafts) => ({ ...drafts, [requirement.id]: { key, draft: { ...base, [field]: value } } }));
  }

  return (
    <Surface className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-4 border-b px-4 py-3">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold">分镜工作台</h2>
          <p className="mt-1 text-sm text-muted-foreground">按分集组织镜头、媒体与衍生资产</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => productionMutation.mutate({ action: "generate_selected_image_prompts", shotIds: rows.filter((shot) => shot.canGenerateImagePrompt && shot.imagePromptStatus !== "succeeded").map((shot) => shot.id) })}
            disabled={!inspectingActivePlan || productionMutation.isPending || rows.filter((shot) => shot.canGenerateImagePrompt && shot.imagePromptStatus !== "succeeded").length === 0}
          >
            {productionMutation.isPending && productionMutation.variables?.action === "generate_selected_image_prompts" ? <Loader2 className="animate-spin" /> : <WandSparkles />}
            一键生成图片提示词
          </Button>
          <Button
            size="sm"
            onClick={() => generateEpisodeMutation.mutate()}
            disabled={!activeScript || !effectiveEpisodeId || !!activeStoryboardRun || generateEpisodeMutation.isPending}
          >
            {generateEpisodeMutation.isPending || activeStoryboardRun ? <Loader2 className="animate-spin" data-icon="inline-start" /> : <WandSparkles data-icon="inline-start" />}
            {activeStoryboardRun ? "本集生成中" : rows.length > 0 ? "重新生成本集" : "生成本集分镜"}
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 border-b bg-muted/20 px-4 py-3">
        <Button variant="ghost" size="icon-sm" title="上一集" aria-label="上一集" onClick={() => moveEpisode(-1)} disabled={selectedEpisodePosition <= 0}>
          <ChevronLeft />
        </Button>
        <Select value={effectiveEpisodeId || undefined} onValueChange={selectEpisode} disabled={episodes.length === 0}>
          <SelectTrigger className="w-full min-w-0 sm:w-[360px]">
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
        {storyboardPlans.length > 0 ? (
          <Select value={effectivePlanId} onValueChange={(value) => {
            setSelectedPlanId(value);
            setSelectedShotId("");
          }}>
            <SelectTrigger className="w-[190px]">
              <SelectValue placeholder="分镜版本" />
            </SelectTrigger>
            <SelectContent>
              {storyboardPlans.map((plan) => (
                <SelectItem key={plan.id} value={plan.id}>
                  版本 {plan.revision} · {plan.active ? "当前" : statusLabel(plan.status)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        {selectedPlanSummary && !selectedPlanSummary.active && selectedPlanSummary.status === "ready" ? (
          <Button size="sm" variant="outline" onClick={() => activatePlanMutation.mutate(selectedPlanSummary.id)} disabled={activatePlanMutation.isPending}>
            {activatePlanMutation.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            激活此版本
          </Button>
        ) : null}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <SummaryBadge label="镜头" value={shotProduction?.summary.total ?? 0} />
          <SummaryBadge label="图片完成" value={shotProduction?.summary.imageSucceeded ?? 0} />
          <SummaryBadge label="视频完成" value={shotProduction?.summary.videoSucceeded ?? 0} />
          <Select value={shotFilter} onValueChange={(value) => setShotFilter(value as ShotFilter)}>
            <SelectTrigger className="w-[128px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部镜头</SelectItem>
              <SelectItem value="missing">待生成</SelectItem>
              <SelectItem value="running">生成中</SelectItem>
              <SelectItem value="ready">已完成</SelectItem>
              <SelectItem value="failed">失败</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <EpisodeTimingSummary
        analysis={timingAnalysis}
        plan={selectedPlanDetail ?? selectedPlanSummary}
        loading={timingFetching}
        analyzing={analyzeTimingMutation.isPending}
        onAnalyze={() => analyzeTimingMutation.mutate()}
      />

      <div className="grid min-h-[620px] xl:grid-cols-[minmax(0,1fr)_430px]">
        <main className="min-w-0 p-4 xl:border-r">
          {loading ? <ShotGridSkeleton aspectRatio={storyboardAspectRatio} /> : null}
          {!loading && episodes.length === 0 ? <EmptyState title="当前剧本没有分集" /> : null}
          {!loading && episodes.length > 0 && rows.length === 0 ? <EmptyState title="本集尚未生成分镜" /> : null}
          {!loading && rows.length > 0 && filteredRows.length === 0 ? <EmptyState title="本集没有符合筛选条件的镜头" /> : null}
          {filteredRows.length > 0 ? (
            <div className="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3">
              {filteredRows.map((shot, index) => (
                <ShotCard
                  key={shot.id}
                  shot={shot}
                  index={index}
                  aspectRatio={storyboardAspectRatio}
                  selected={selectedRow?.id === shot.id}
                  onSelect={() => {
                    setSelectedShotId(shot.id);
                    setShotDraftEntry(null);
                  }}
                  onOpenImageDetail={() => {
                    setSelectedShotId(shot.id);
                    setShotDraftEntry(null);
                    setImageDetailShotId(shot.id);
                  }}
                />
              ))}
            </div>
          ) : null}
        </main>

        <aside className="min-w-0 bg-muted/10">
          {!selectedShot && !loading ? <EmptyState title="选择一个镜头查看详情" compact /> : null}
          {selectedShot ? (
            <div className="xl:sticky xl:top-0 xl:max-h-[calc(100vh-9rem)] xl:overflow-y-auto">
              <div className="border-b px-4 py-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-xs text-muted-foreground">镜头 {localShotNumber(selectedRow ?? selectedShot, selectedIndex)}</p>
                    <h3 className="mt-1 truncate text-base font-semibold">{selectedShot.title || selectedShot.visual || "未命名镜头"}</h3>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    {selectedRow?.storyboardPlanId ? (
                      <>
                        <Button variant="ghost" size="icon-sm" title="从中点拆分" aria-label="从中点拆分" onClick={() => splitShotMutation.mutate(selectedRow)} disabled={!inspectingActivePlan || splitShotMutation.isPending || midpointFrameTick(selectedRow) <= (selectedRow.startTick ?? 0)}>
                          {splitShotMutation.isPending ? <Loader2 className="animate-spin" /> : <Scissors />}
                        </Button>
                        <Button variant="ghost" size="icon-sm" title="与下一镜合并" aria-label="与下一镜合并" onClick={() => {
                          const next = rows[selectedIndex + 1];
                          if (next) mergeShotMutation.mutate({ shotId: selectedRow.id, nextShotId: next.id });
                        }} disabled={!inspectingActivePlan || mergeShotMutation.isPending || !canMergeWithNext(rows, selectedIndex)}>
                          {mergeShotMutation.isPending ? <Loader2 className="animate-spin" /> : <Combine />}
                        </Button>
                      </>
                    ) : (
                      <Button variant="ghost" size="icon-sm" title="删除镜头" aria-label="删除镜头" onClick={() => setShotToDelete(selectedRow ?? rowFromShot(selectedShot))}>
                        <Trash2 />
                      </Button>
                    )}
                  </div>
                </div>
                <div className="mt-2 flex flex-wrap gap-2">
                  <Badge variant="outline">图片 {statusLabel(selectedShot.imageStatus || selectedRow?.imageStatus || "not_started")}</Badge>
                  <Badge variant="outline">图片提示词 {statusLabel(selectedShot.imagePromptStatus || selectedRow?.imagePromptStatus || "not_started")}</Badge>
                  <Badge variant="outline">视频 {statusLabel(selectedShot.videoStatus || selectedRow?.videoStatus || "not_started")}</Badge>
                  {selectedShot.staleState && selectedShot.staleState !== "fresh" ? <Badge variant="secondary">{statusLabel(selectedShot.staleState)}</Badge> : null}
                </div>
              </div>

              <Tabs value={inspectorTab} onValueChange={setInspectorTab} className="p-4">
                <TabsList className="grid w-full grid-cols-3">
                  <TabsTrigger value="shot">镜头</TabsTrigger>
                  <TabsTrigger value="prompts">提示词</TabsTrigger>
                  <TabsTrigger value="assets">衍生资产</TabsTrigger>
                </TabsList>

                <TabsContent value="shot" className="mt-4 grid gap-4">
                  <div className="grid grid-cols-2 gap-2">
                    <ShotMediaPreview kind="image" previewUrl={shotDetail?.imagePreviewUrl || selectedRow?.imagePreviewUrl} aspectRatio={storyboardAspectRatio} />
                    <ShotMediaPreview kind="video" previewUrl={shotDetail?.videoPreviewUrl || selectedRow?.videoPreviewUrl} aspectRatio={storyboardAspectRatio} />
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => productionMutation.mutate({ action: selectedRow?.canRetryImage ? "regenerate_failed_images" : "generate_selected_images", shotIds: [selectedShot.id] })}
                      disabled={!inspectingActivePlan || productionMutation.isPending || (!selectedRow?.canGenerateImage && !selectedRow?.canRetryImage)}
                    >
                      {productionMutation.isPending ? <Loader2 className="animate-spin" data-icon="inline-start" /> : <ImageIcon data-icon="inline-start" />}
                      生成图片
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => productionMutation.mutate({ action: selectedRow?.canRetryVideo ? "regenerate_failed_videos" : "generate_selected_videos", shotIds: [selectedShot.id] })}
                      disabled={!inspectingActivePlan || productionMutation.isPending || (!selectedRow?.canGenerateVideo && !selectedRow?.canRetryVideo)}
                    >
                      {productionMutation.isPending ? <Loader2 className="animate-spin" data-icon="inline-start" /> : <Video data-icon="inline-start" />}
                      生成视频
                    </Button>
                  </div>
                  {selectedRow?.imageErrorMessage ? <p className="text-xs text-destructive">图片错误：{localizePlatformError(selectedRow.imageErrorMessage, selectedRow.imageErrorCode)}</p> : null}
                  {selectedRow?.imagePromptErrorMessage ? <p className="text-xs text-destructive">图片提示词错误：{localizePlatformError(selectedRow.imagePromptErrorMessage, selectedRow.imagePromptErrorCode)}</p> : null}
                  {selectedRow?.videoErrorMessage ? <p className="text-xs text-destructive">视频错误：{localizePlatformError(selectedRow.videoErrorMessage, selectedRow.videoErrorCode)}</p> : null}
                  <div className="grid grid-cols-2 gap-3">
                    <FieldText label="时长（秒）" value={shotDraft.durationSeconds} onChange={(value) => setShotDraftField("durationSeconds", value)} disabled={!inspectingActivePlan} type="number" min={1} step={1} />
                    <FieldText label="情绪" value={shotDraft.mood} onChange={(value) => setShotDraftField("mood", value)} disabled={!inspectingActivePlan} />
                  </div>
                  <FieldText label="景别与机位" value={shotDraft.camera} onChange={(value) => setShotDraftField("camera", value)} disabled={!inspectingActivePlan} />
                  <FieldTextarea label="画面描述" value={shotDraft.visual} onChange={(value) => setShotDraftField("visual", value)} rows={5} disabled={!inspectingActivePlan} />
                  <FieldTextarea label="动作与运镜" value={shotDraft.motion} onChange={(value) => setShotDraftField("motion", value)} rows={3} disabled={!inspectingActivePlan} />
                  <Button size="sm" onClick={() => updateShotMutation.mutate({ shotId: selectedShot.id, draft: shotDraft, mode: "shot", durationEdited: shotDraftEntry?.key === selectedShotDraftKey && shotDraftEntry.durationEdited })} disabled={updateShotMutation.isPending || !inspectingActivePlan}>
                    {updateShotMutation.isPending ? <Loader2 className="animate-spin" data-icon="inline-start" /> : <Save data-icon="inline-start" />}
                    保存镜头
                  </Button>
                </TabsContent>

                <TabsContent value="prompts" className="mt-4 grid gap-4">
                  <FieldTextarea label="镜头图提示词" value={shotDraft.imagePrompt} onChange={(value) => setShotDraftField("imagePrompt", value)} rows={10} disabled={!inspectingActivePlan} />
                  <Button size="sm" variant="outline" onClick={() => productionMutation.mutate({ action: "generate_selected_image_prompts", shotIds: [selectedShot.id] })} disabled={!inspectingActivePlan || productionMutation.isPending || !selectedRow?.canGenerateImagePrompt}>
                    {productionMutation.isPending && productionMutation.variables?.action === "generate_selected_image_prompts" ? <Loader2 className="animate-spin" /> : <WandSparkles />}
                    {selectedRow?.imagePromptStatus === "succeeded" ? "重新生成图片提示词" : "生成图片提示词"}
                  </Button>
                  <FieldTextarea label="镜头视频提示词" value={shotDraft.videoPrompt} onChange={(value) => setShotDraftField("videoPrompt", value)} rows={10} disabled={!inspectingActivePlan} />
                  <Button size="sm" onClick={() => updateShotMutation.mutate({ shotId: selectedShot.id, draft: shotDraft, mode: "prompts" })} disabled={!inspectingActivePlan || updateShotMutation.isPending}>
                    {updateShotMutation.isPending ? <Loader2 className="animate-spin" data-icon="inline-start" /> : <Save data-icon="inline-start" />}
                    保存提示词
                  </Button>
                </TabsContent>

                <TabsContent value="assets" className="mt-4">
                  {detailLoading ? <Skeleton className="h-48" /> : null}
                  {!detailLoading && (!shotDetail?.requirements || shotDetail.requirements.length === 0) ? <EmptyState title="当前镜头没有衍生资产需求" compact /> : null}
                  <div className="divide-y">
                    {shotDetail?.requirements.map((requirement) => {
                      const draft = requirementDraft(requirement);
                      const skipped = requirement.status === "skipped";
                      return (
                        <section key={requirement.id} className="grid gap-3 py-4 first:pt-0">
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="truncate text-sm font-medium">{requirement.assetName || requirement.asset?.name || "未命名资产"}</div>
                              <div className="mt-1 flex flex-wrap gap-1.5">
                                <Badge variant="outline">{assetTypeLabel(requirement.assetType || requirement.asset?.assetType)}</Badge>
                                <Badge variant="outline">{requirementTypeLabel(requirement.requirementType)}</Badge>
                                <Badge variant={skipped ? "secondary" : "outline"}>{statusLabel(requirement.status)}</Badge>
                              </div>
                            </div>
                            <Button size="sm" variant="outline" onClick={() => generateDerivedMutation.mutate(requirement.id)} disabled={!inspectingActivePlan || generateDerivedMutation.isPending || skipped}>
                              {generateDerivedMutation.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
                              <span className="sr-only">生成衍生图</span>
                            </Button>
                          </div>
                          <AssetLineage requirement={requirement} />
                          {requirement.roleInShot ? <p className="text-xs leading-5 text-muted-foreground">镜头关系：{requirement.roleInShot}</p> : null}
                          <details className="group">
                            <summary className="cursor-pointer text-sm font-medium text-muted-foreground group-open:text-foreground">编辑衍生要求</summary>
                            <div className="mt-3 grid gap-3">
                              <div className="grid grid-cols-2 gap-3">
                                <FieldText label="服装" value={draft.costume} onChange={(value) => setRequirementDraftField(requirement, "costume", value)} disabled={!inspectingActivePlan || skipped} />
                                <FieldText label="姿态" value={draft.pose} onChange={(value) => setRequirementDraftField(requirement, "pose", value)} disabled={!inspectingActivePlan || skipped} />
                                <FieldText label="表情" value={draft.expression} onChange={(value) => setRequirementDraftField(requirement, "expression", value)} disabled={!inspectingActivePlan || skipped} />
                                <FieldText label="动作" value={draft.action} onChange={(value) => setRequirementDraftField(requirement, "action", value)} disabled={!inspectingActivePlan || skipped} />
                              </div>
                              <FieldText label="镜头关系" value={draft.cameraRelation} onChange={(value) => setRequirementDraftField(requirement, "cameraRelation", value)} disabled={!inspectingActivePlan || skipped} />
                              <FieldText label="场景状态" value={draft.sceneState} onChange={(value) => setRequirementDraftField(requirement, "sceneState", value)} disabled={!inspectingActivePlan || skipped} />
                              <FieldText label="道具状态" value={draft.propState} onChange={(value) => setRequirementDraftField(requirement, "propState", value)} disabled={!inspectingActivePlan || skipped} />
                              <FieldTextarea label="衍生图提示词" value={draft.prompt} onChange={(value) => setRequirementDraftField(requirement, "prompt", value)} rows={5} disabled={!inspectingActivePlan || skipped} />
                              <div className="flex gap-2">
                                <Button size="sm" onClick={() => updateRequirementMutation.mutate({ requirementId: requirement.id, draft })} disabled={!inspectingActivePlan || updateRequirementMutation.isPending || skipped}>
                                  {updateRequirementMutation.isPending ? <Loader2 className="animate-spin" data-icon="inline-start" /> : <Save data-icon="inline-start" />}
                                  保存
                                </Button>
                                <Button size="sm" variant="outline" onClick={() => skipRequirementMutation.mutate(requirement.id)} disabled={!inspectingActivePlan || skipRequirementMutation.isPending || skipped}>
                                  <SkipForward data-icon="inline-start" />
                                  跳过
                                </Button>
                              </div>
                            </div>
                          </details>
                        </section>
                      );
                    })}
                  </div>
                </TabsContent>
              </Tabs>
            </div>
          ) : null}
        </aside>
      </div>

      <AlertDialog open={!!shotToDelete} onOpenChange={(open) => !open && setShotToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除镜头</AlertDialogTitle>
            <AlertDialogDescription>删除后该镜头会从当前分集移除，并影响关联媒体和成片合成。</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="border-y py-3 text-sm">
            <div className="font-medium">影响范围</div>
            <ul className="mt-2 grid gap-1 text-muted-foreground">
              {deleteImpactItems.map((item) => <li key={item}>{item}</li>)}
            </ul>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => shotToDelete && deleteShotMutation.mutate(shotToDelete.id)}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {imageDetailRow ? (
        <ShotImageDetailDialog
          projectId={projectId}
          shotId={imageDetailRow.id}
          shotNumber={localShotNumber(imageDetailRow, rows.findIndex((shot) => shot.id === imageDetailRow.id))}
          open
          onOpenChange={(open) => !open && setImageDetailShotId("")}
          onChanged={() => refreshStoryboard(projectId, imageDetailRow.id, invalidate)}
        />
      ) : null}
    </Surface>
  );
}

function EpisodeTimingSummary({
  analysis,
  plan,
  loading,
  analyzing,
  onAnalyze,
}: {
  analysis?: ScriptTimingAnalysis;
  plan?: StoryboardPlan | null;
  loading: boolean;
  analyzing: boolean;
  onAnalyze: () => void;
}) {
  const scenePlans = plan?.scenePlans ?? [];
  return (
    <section className="border-b px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
        <div className="min-w-[150px]">
          <div className="text-xs text-muted-foreground">分集时长</div>
          <div className="mt-1 text-lg font-semibold tabular-nums">
            {analysis ? formatDuration(analysis.estimatedDurationSeconds) : loading ? "--" : "未分析"}
          </div>
        </div>
        <TimingMetric label="硬下限" value={analysis ? formatDuration(analysis.minimumDurationSeconds) : "--"} />
        <TimingMetric label="目标" value={analysis?.targetDurationSeconds ? formatDuration(analysis.targetDurationSeconds) : "自动"} />
        <TimingMetric label="对白" value={analysis ? formatTicks(analysis.dialogueDurationTicks, analysis.timelineTimebase) : "--"} />
        <TimingMetric label="动作" value={analysis ? formatTicks(analysis.actionDurationTicks, analysis.timelineTimebase) : "--"} />
        <TimingMetric label="停顿" value={analysis ? formatTicks(analysis.pauseDurationTicks, analysis.timelineTimebase) : "--"} />
        <TimingMetric
          label="时间基准"
          value={analysis ? `${analysis.fpsNumerator / analysis.fpsDenominator} FPS · ${analysis.estimatedDurationFrames} 帧` : "24 FPS"}
        />
        <Button size="sm" variant="ghost" className="ml-auto" onClick={onAnalyze} disabled={analyzing || loading}>
          {analyzing ? <Loader2 className="animate-spin" /> : <RefreshCw />}
          重新分析时长
        </Button>
      </div>
      {plan ? (
        <div className="mt-3 flex flex-wrap items-center gap-2 border-t pt-3">
          <Badge variant={plan.active ? "default" : "outline"}>版本 {plan.revision}</Badge>
          <Badge variant="outline">{statusLabel(plan.status)}</Badge>
          <span className="text-xs text-muted-foreground">
            场景 {plan.completedSceneCount}/{plan.sceneCount} · 镜头 {plan.actualShotCount || plan.estimatedShotCount}
          </span>
          {scenePlans.map((scene) => (
            <span key={scene.id} className="inline-flex items-center gap-1.5 border-l pl-2 text-xs text-muted-foreground">
              {scene.status === "planning" || scene.status === "reviewing" ? <Loader2 className="size-3 animate-spin" /> : <span className={cn("size-1.5 rounded-full bg-muted-foreground/40", scene.status === "ready" && "bg-emerald-500", scene.status === "failed" && "bg-destructive")} />}
              场景 {scene.sceneOrdinal + 1} {statusLabel(scene.status)}
            </span>
          ))}
        </div>
      ) : null}
    </section>
  );
}

function TimingMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-[88px]">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}

function formatDuration(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return "--";
  const rounded = Math.round(seconds);
  const minutes = Math.floor(rounded / 60);
  const remainder = rounded % 60;
  return minutes > 0 ? `${minutes}分${String(remainder).padStart(2, "0")}秒` : `${remainder}秒`;
}

function formatTicks(ticks: number, timebase: number) {
  return timebase > 0 ? formatDuration(ticks / timebase) : "--";
}

function SummaryBadge({ label, value }: { label: string; value: number }) {
  return <span className="whitespace-nowrap text-xs text-muted-foreground">{label} <strong className="font-semibold text-foreground">{value}</strong></span>;
}

function ShotCard({ shot, index, aspectRatio, selected, onSelect, onOpenImageDetail }: { shot: ShotRow; index: number; aspectRatio: string; selected: boolean; onSelect: () => void; onOpenImageDetail: () => void }) {
  return (
    <article
      className={cn(
        "group min-w-0 overflow-hidden rounded-md border bg-background text-left transition-colors hover:border-foreground/25 hover:bg-muted/30",
        selected && "border-primary ring-1 ring-primary",
      )}
    >
      <button type="button" onClick={onOpenImageDetail} className="relative block w-full overflow-hidden bg-muted text-left" style={{ aspectRatio: cssAspectRatio(aspectRatio) }} aria-label={`查看镜头 ${localShotNumber(shot, index)} 图片详情`}>
        {shot.imagePreviewUrl ? (
          <NextImage src={shot.imagePreviewUrl} alt={shot.title || `镜头 ${index + 1}`} fill unoptimized sizes="(max-width: 768px) 100vw, 280px" className="object-cover transition-transform duration-200 group-hover:scale-[1.02]" />
        ) : (
          <div className="grid h-full place-items-center"><Film className="text-muted-foreground/60" /></div>
        )}
        <span className="absolute left-2 top-2 rounded bg-black/70 px-2 py-1 text-xs font-medium text-white">镜头 {localShotNumber(shot, index)}</span>
        {shot.durationSeconds ? <span className="absolute bottom-2 right-2 flex items-center gap-1 rounded bg-black/70 px-2 py-1 text-xs text-white"><Clock3 className="size-3" />{wholeSecondDuration(shot.durationSeconds)} 秒</span> : null}
        <span className="absolute bottom-2 left-2 rounded bg-black/70 px-2 py-1 text-[11px] text-white opacity-0 transition-opacity group-hover:opacity-100">图片详情</span>
      </button>
      <button type="button" onClick={onSelect} className="grid min-h-24 w-full gap-2 p-3 text-left">
        <div className="truncate text-sm font-medium">{shot.title || shot.visual || `镜头 ${index + 1}`}</div>
        <p className="line-clamp-2 text-xs leading-5 text-muted-foreground">{shot.visual || "未填写画面描述"}</p>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <StatusDot status={shot.imageStatus} label="图片" />
          <StatusDot status={shot.imagePromptStatus} label="图词" />
          <StatusDot status={shot.videoStatus} label="视频" />
        </div>
      </button>
    </article>
  );
}

function StatusDot({ status, label }: { status?: string; label: string }) {
  const normalized = status || "not_started";
  return (
    <span className="flex items-center gap-1.5">
      <span className={cn("size-1.5 rounded-full bg-muted-foreground/40", ["queued", "running"].includes(normalized) && "animate-pulse bg-primary", normalized === "succeeded" && "bg-emerald-500", normalized === "failed" && "bg-destructive")} />
      {label} {statusLabel(normalized)}
    </span>
  );
}

function AssetLineage({ requirement }: { requirement: StoryboardShotRequirementDetail }) {
  const originalPreview = primaryAssetPreview(requirement);
  return (
    <div className="grid grid-cols-[1fr_28px_1fr] items-center gap-2">
      <LineageImage label="基础资产" previewUrl={originalPreview} name={requirement.asset?.name || requirement.assetName} />
      <ArrowRight className="mx-auto size-4 text-muted-foreground" />
      <LineageImage label="镜头衍生" previewUrl={requirement.derivedPreviewUrl} name={requirement.asset?.name || requirement.assetName} />
    </div>
  );
}

function LineageImage({ label, previewUrl, name }: { label: string; previewUrl?: string; name?: string }) {
  return (
    <div className="min-w-0">
      <div className="relative aspect-video overflow-hidden rounded-md bg-muted">
        {previewUrl ? <NextImage src={previewUrl} alt={`${name || "资产"}${label}`} fill unoptimized sizes="180px" className="object-cover" /> : <div className="grid h-full place-items-center"><ImageIcon className="size-5 text-muted-foreground/60" /></div>}
      </div>
      <div className="mt-1.5 truncate text-center text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

function primaryAssetPreview(requirement: StoryboardShotRequirementDetail) {
  const references = requirement.asset?.references ?? [];
  return references.find((reference) => reference.isPrimary && reference.previewUrl)?.previewUrl ?? references.find((reference) => reference.previewUrl)?.previewUrl;
}

function ShotMediaPreview({ kind, previewUrl, aspectRatio }: { kind: "image" | "video"; previewUrl?: string; aspectRatio: string }) {
  const style = { aspectRatio: cssAspectRatio(aspectRatio) };
  if (previewUrl && kind === "video") return <video className="w-full rounded-md bg-black object-cover" style={style} controls src={previewUrl} />;
  if (previewUrl) return <div className="relative overflow-hidden rounded-md bg-muted" style={style}><NextImage src={previewUrl} alt="镜头图片" fill unoptimized sizes="220px" className="object-cover" /></div>;
  return <div className="grid place-items-center rounded-md bg-muted" style={style}>{kind === "image" ? <ImageIcon className="text-muted-foreground/60" /> : <Video className="text-muted-foreground/60" />}</div>;
}

function ShotGridSkeleton({ aspectRatio }: { aspectRatio: string }) {
  return <div className="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3">{Array.from({ length: 6 }, (_, index) => <Skeleton key={index} className="w-full rounded-md" style={{ aspectRatio: cssAspectRatio(aspectRatio) }} />)}</div>;
}

function EmptyState({ title, compact = false }: { title: string; compact?: boolean }) {
  return <div className={cn("grid place-items-center text-center text-sm text-muted-foreground", compact ? "min-h-48" : "min-h-80")}><div><Film className="mx-auto mb-3" />{title}</div></div>;
}

function isActiveStoryboardRun(run: WorkflowRun, episodeId: string) {
  if (!ACTIVE_WORKFLOW_STATUSES.has(run.status)) return false;
  const workflowType = typeof run.input?.workflowType === "string" ? run.input.workflowType : "";
  if (!["script_to_storyboard", "script_to_video", "full_production"].includes(workflowType)) return false;
  const input = asRecord(run.input?.input);
  const episodeIds = Array.isArray(input.scriptEpisodeIds) ? input.scriptEpisodeIds.filter((value): value is string => typeof value === "string") : [];
  return episodeIds.length === 0 || episodeIds.includes(episodeId);
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function shotMatchesFilter(shot: ShotRow, filter: ShotFilter) {
  const statuses = [shot.imagePromptStatus, shot.imageStatus, shot.videoStatus];
  if (filter === "all") return true;
  if (filter === "running") return statuses.some((status) => status === "queued" || status === "running");
  if (filter === "failed") return statuses.includes("failed");
  if (filter === "ready") return shot.imageStatus === "succeeded" && shot.videoStatus === "succeeded";
  return statuses.some((status) => !status || status === "not_started" || status === "stale");
}

function compareEpisodeShots(left: ShotRow, right: ShotRow) {
  if (left.startTick != null && right.startTick != null && left.startTick !== right.startTick) {
    return left.startTick - right.startTick;
  }
  return (left.episodeShotIndex ?? left.shotIndex) - (right.episodeShotIndex ?? right.shotIndex) || left.shotNo - right.shotNo;
}

function rowFromShot(shot: StoryboardShot | ShotRow): ShotRow {
  return {
    id: shot.id,
    workflowRunId: shot.workflowRunId,
    storyboardPlanId: "storyboardPlanId" in shot ? shot.storyboardPlanId : undefined,
    scriptSceneId: "scriptSceneId" in shot ? shot.scriptSceneId : undefined,
    scriptEpisodeId: shot.scriptEpisodeId,
    episodeIndex: shot.episodeIndex,
    episodeShotIndex: shot.episodeShotIndex,
    episodeTitle: shot.episodeTitle,
    shotNo: shot.shotNo,
    shotIndex: shot.shotIndex,
    title: shot.title,
	startTick: "startTick" in shot ? shot.startTick : undefined,
	endTick: "endTick" in shot ? shot.endTick : undefined,
	plannedDurationTicks: "plannedDurationTicks" in shot ? shot.plannedDurationTicks : undefined,
	timelineTimebase: "timelineTimebase" in shot ? shot.timelineTimebase : undefined,
	fpsNumerator: "fpsNumerator" in shot ? shot.fpsNumerator : undefined,
	fpsDenominator: "fpsDenominator" in shot ? shot.fpsDenominator : undefined,
    durationSeconds: shot.durationSeconds,
    visual: shot.visual,
    camera: "camera" in shot ? shot.camera : undefined,
    motion: "motion" in shot ? shot.motion : undefined,
    mood: "mood" in shot ? shot.mood : undefined,
    imagePrompt: "imagePrompt" in shot ? shot.imagePrompt : undefined,
    imagePromptStatus: "imagePromptStatus" in shot ? shot.imagePromptStatus : undefined,
    videoPrompt: "videoPrompt" in shot ? shot.videoPrompt : undefined,
    imageArtifactId: shot.imageArtifactId,
    videoArtifactId: shot.videoArtifactId,
    imagePreviewUrl: shot.imagePreviewUrl,
    videoPreviewUrl: shot.videoPreviewUrl,
    imageStatus: shot.imageStatus,
    videoStatus: shot.videoStatus,
    staleState: shot.staleState,
  };
}

function rowFromPlanShot(shot: StoryboardShot, production?: ShotProductionShot): ShotRow {
  const row = rowFromShot(shot);
  if (!production) return row;
  return {
    ...row,
    imagePrompt: production.imagePrompt || row.imagePrompt,
    imagePromptStatus: production.imagePromptStatus,
    imagePromptErrorCode: production.imagePromptErrorCode,
    imagePromptErrorMessage: production.imagePromptErrorMessage,
    videoPrompt: production.videoPrompt || row.videoPrompt,
    imageArtifactId: production.imageArtifactId,
    videoArtifactId: production.videoArtifactId,
    imagePreviewUrl: production.imagePreviewUrl,
    videoPreviewUrl: production.videoPreviewUrl,
    imageStatus: production.imageStatus,
    videoStatus: production.videoStatus,
    imageErrorCode: production.imageErrorCode,
    imageErrorMessage: production.imageErrorMessage,
    videoErrorCode: production.videoErrorCode,
    videoErrorMessage: production.videoErrorMessage,
    staleState: production.staleState,
    canGenerateImage: production.canGenerateImage,
    canGenerateImagePrompt: production.canGenerateImagePrompt,
    canGenerateVideo: production.canGenerateVideo,
    canRetryImage: production.canRetryImage,
    canRetryVideo: production.canRetryVideo,
  };
}

function midpointFrameTick(shot: Pick<ShotRow, "startTick" | "endTick" | "timelineTimebase" | "fpsNumerator" | "fpsDenominator">) {
  const start = shot.startTick ?? 0;
  const end = shot.endTick ?? start;
  const timebase = shot.timelineTimebase ?? 90_000;
  const numerator = shot.fpsNumerator ?? 24;
  const denominator = shot.fpsDenominator ?? 1;
  const frameTick = numerator > 0 ? (timebase * denominator) / numerator : 0;
  if (!Number.isInteger(frameTick) || frameTick <= 0) return start;
  const frameCount = Math.floor((end - start) / frameTick);
  if (frameCount < 2) return start;
  return start + Math.floor(frameCount / 2) * frameTick;
}

function canMergeWithNext(rows: ShotRow[], selectedIndex: number) {
  const current = rows[selectedIndex];
  const next = rows[selectedIndex + 1];
  if (!current || !next || !current.storyboardPlanId || current.storyboardPlanId !== next.storyboardPlanId) return false;
  if (current.endTick == null || next.startTick == null || current.endTick !== next.startTick) return false;
  return (current.scriptSceneId ?? "") === (next.scriptSceneId ?? "");
}

function localShotNumber(shot: Pick<ShotRow, "episodeShotIndex" | "shotNo"> | Pick<StoryboardShot, "episodeShotIndex" | "shotNo">, fallbackIndex: number) {
  return shot.episodeShotIndex != null ? shot.episodeShotIndex + 1 : shot.shotNo || fallbackIndex + 1;
}

function refreshStoryboard(projectId: string, shotId: string, invalidate: (keys: QueryKey[]) => void) {
  const keys: QueryKey[] = [qk.shotProductionPrefix(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId)];
  if (shotId) keys.push(qk.shotDetail(projectId, shotId));
  invalidate(keys);
}

function shotDraftKey(shot: StoryboardShot | ShotRow) {
  return [shot.id, shot.durationSeconds ?? "", shot.visual ?? "", "camera" in shot ? shot.camera ?? "" : "", "motion" in shot ? shot.motion ?? "" : "", "mood" in shot ? shot.mood ?? "" : "", "imagePrompt" in shot ? shot.imagePrompt ?? "" : "", "videoPrompt" in shot ? shot.videoPrompt ?? "" : ""].join("\u0001");
}

function draftFromShot(shot: StoryboardShot | ShotRow): ShotDraft {
  return {
    durationSeconds: shot.durationSeconds ? String(wholeSecondDuration(shot.durationSeconds)) : "",
    visual: shot.visual ?? "",
    camera: "camera" in shot ? shot.camera ?? "" : "",
    motion: "motion" in shot ? shot.motion ?? "" : "",
    mood: "mood" in shot ? shot.mood ?? "" : "",
    imagePrompt: "imagePrompt" in shot ? shot.imagePrompt ?? "" : "",
    videoPrompt: "videoPrompt" in shot ? shot.videoPrompt ?? "" : "",
  };
}

function shotVisualUpdateBody(draft: ShotDraft) {
  return { visual: draft.visual, camera: draft.camera, motion: draft.motion, mood: draft.mood };
}

function shotPromptUpdateBody(draft: ShotDraft) {
  return { imagePrompt: draft.imagePrompt, videoPrompt: draft.videoPrompt };
}

function requirementDraftKey(requirement: StoryboardShotRequirementDetail) {
  return [requirement.id, requirement.costume ?? "", requirement.pose ?? "", requirement.expression ?? "", requirement.action ?? "", requirement.cameraRelation ?? "", requirement.sceneState ?? "", requirement.propState ?? "", requirement.prompt ?? ""].join("\u0001");
}

function draftFromRequirement(requirement: StoryboardShotRequirementDetail): RequirementDraft {
  return { costume: requirement.costume ?? "", pose: requirement.pose ?? "", expression: requirement.expression ?? "", action: requirement.action ?? "", cameraRelation: requirement.cameraRelation ?? "", sceneState: requirement.sceneState ?? "", propState: requirement.propState ?? "", prompt: requirement.prompt ?? "" };
}

function requirementUpdateBody(draft: RequirementDraft) {
  return { ...draft };
}

function buildDeleteImpactItems(shot: ShotRow, detail?: StoryboardShotDetail) {
  const items = [`镜头 ${localShotNumber(shot, shot.shotIndex)} 将从当前分集移除`];
  const requirementCount = detail?.requirements.length ?? 0;
  if (requirementCount > 0) items.push(`${requirementCount} 个镜头资产需求会随镜头失效`);
  if (shot.imagePreviewUrl || shot.imageArtifactId) items.push("已绑定的镜头图片会解除关联");
  if (shot.videoPreviewUrl || shot.videoArtifactId) items.push("已绑定的镜头视频会解除关联");
  items.push("当前成片版本需要重新合成");
  return items;
}

function FieldText({ label, value, onChange, disabled, type = "text", min, step }: { label: string; value: string; onChange: (value: string) => void; disabled?: boolean; type?: "text" | "number"; min?: number; step?: number }) {
  return <div className="grid gap-1.5"><Label>{label}</Label><Input type={type} min={min} step={step} inputMode={type === "number" ? "numeric" : undefined} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} /></div>;
}

function FieldTextarea({ label, value, onChange, rows = 3, disabled }: { label: string; value: string; onChange: (value: string) => void; rows?: number; disabled?: boolean }) {
  return <div className="grid gap-1.5"><Label>{label}</Label><Textarea value={value} onChange={(event) => onChange(event.target.value)} rows={rows} disabled={disabled} /></div>;
}
