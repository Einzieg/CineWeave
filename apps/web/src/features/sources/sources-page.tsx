"use client";

import { useMemo, useState } from "react";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { StatusBadge } from "@/components/shared/status-badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import {
  Edit2,
  FileText,
  Upload,
  Wand2,
  Clock,
  AlertCircle,
  Trash2,
  ListChecks,
  Save,
  CheckCircle2,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { localizePlatformError } from "@/lib/error-localization";
import { contentFormatLabel, sourceTypeLabel, statusLabel, targetFormatLabel } from "@/lib/labels";
import { currentProjectScript } from "@/lib/scripts";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";
import { useUiStore } from "@/lib/stores/ui-store";
import { isActiveWorkflowStatus } from "@/lib/workflow-status";
import { useProjectPollingFallback } from "@/lib/realtime/use-project-polling-fallback";
import type { AdaptationPlan, JsonRecord, NovelChapterSummary, ProjectSource, ScriptEpisode, WorkflowRun } from "@/lib/types";
import { EpisodeAudioPanel } from "@/features/sources/episode-audio-panel";

type ImportSourceType = "novel" | "script" | "brief";
type SourcesTab = "sources" | "scripts";
type SourcesPageMode = "combined" | "content" | "scripts";
type SourcesInitialTab = SourcesTab | "events" | "plans";

type SourceEditForm = {
  title: string;
  sourceType: ImportSourceType;
  contentFormat: string;
  content: string;
  splitChapters: boolean;
};

type AdaptationPlanEditForm = {
  title: string;
  status: string;
  targetFormat: string;
  targetDurationSeconds: string;
  maxShots: string;
  content: string;
};

type ScriptEpisodeEditForm = {
  episodeTitle: string;
  contentFormat: string;
  content: string;
  reviewStatus: string;
};

export function SourcesPage({
  projectId,
  initialTab = "sources",
  mode = "combined",
}: {
  projectId: string;
  initialTab?: SourcesInitialTab;
  initialSceneId?: string;
  mode?: SourcesPageMode;
}) {
  const [activeTab, setActiveTab] = useState<SourcesTab>(initialTab === "scripts" ? "scripts" : "sources");
  const fixedTab = mode === "content" ? "sources" : mode === "scripts" ? "scripts" : null;
  const visibleTab = fixedTab ?? activeTab;
  const [selectedSourceId, setSelectedSourceId] = useState<string | null>(null);
  const [selectedChapterId, setSelectedChapterId] = useState<string | null>(null);
  const [selectedChapterIds, setSelectedChapterIds] = useState<string[]>([]);
  const [selectedPlanId, setSelectedPlanId] = useState<string | null>(null);
  const [selectedScriptId, setSelectedScriptId] = useState<string | null>(null);
  const [selectedScriptVersionId, setSelectedScriptVersionId] = useState<string | null>(null);
  const [selectedScriptEpisodeId, setSelectedScriptEpisodeId] = useState<string | null>(null);
  const [scriptEpisodeDialogOpen, setScriptEpisodeDialogOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importTitle, setImportTitle] = useState("");
  const [importContent, setImportContent] = useState("");
  const [importSourceType, setImportSourceType] = useState<ImportSourceType>("novel");
  const [splitChapters, setSplitChapters] = useState(true);
  const [createScript, setCreateScript] = useState(false);
  const [sourceEditOpen, setSourceEditOpen] = useState(false);
  const [editingSource, setEditingSource] = useState<ProjectSource | null>(null);
  const [sourceEditForm, setSourceEditForm] = useState<SourceEditForm>(emptySourceEditForm());
  const [sourceToDelete, setSourceToDelete] = useState<ProjectSource | null>(null);
  const [planEditDraft, setPlanEditDraft] = useState<{ planId: string; form: AdaptationPlanEditForm } | null>(null);
  const [scriptEpisodeEditDraft, setScriptEpisodeEditDraft] = useState<{ episodeId: string; form: ScriptEpisodeEditForm } | null>(null);
  const invalidate = useInvalidateKeys();
  const pollingFallback = useProjectPollingFallback(projectId);
  const { open: openAgent, setContext } = useAgentDrawerStore();
  const setActivityOpen = useUiStore((state) => state.setActivityOpen);
  const generatedScriptFocus = useUiStore((state) => state.latestGeneratedScripts[projectId]);

  // 获取原文列表
  const { data: sources = [], isLoading: sourcesLoading } = useApiQuery({
    key: qk.sources(projectId),
    queryFn: (session) => studioApi.listSources(session, projectId).then(r => r.items || []),
  });

  const selectedSource = useMemo(
    () => sources.find((source) => source.id === selectedSourceId) ?? sources.find((source) => source.sourceType === "novel") ?? sources[0] ?? null,
    [selectedSourceId, sources],
  );

  const effectiveSourceId = selectedSource?.id ?? "";
  const sourceToDeleteId = sourceToDelete?.id ?? "";

  const { data: sourceDeleteImpact, isLoading: sourceDeleteImpactLoading } = useApiQuery({
    key: qk.sourceImpact(projectId, sourceToDeleteId),
    queryFn: (session) => studioApi.getSourceImpact(session, projectId, sourceToDeleteId),
    enabled: !!sourceToDeleteId,
  });

  // 获取剧本列表
  const { data: scripts = [], isLoading: scriptsLoading } = useApiQuery({
    key: qk.scripts(projectId),
    queryFn: (session) => studioApi.listScripts(session, projectId).then(r => r.items || []),
  });
  const { data: workflowRuns = [] } = useApiQuery({
    key: qk.workflowRuns(projectId, { status: "active", limit: 100 }),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId, { status: "active", limit: 100 }).then((response) => response.items || []),
    enabled: visibleTab === "scripts",
    refetchInterval: (query) =>
      pollingFallback && query.state.data?.some((run) => isActiveWorkflowStatus(run.status)) ? 5000 : false,
  });

  const { data: chapters = [], isLoading: chaptersLoading } = useApiQuery({
    key: qk.sourceChapters(projectId, effectiveSourceId),
    queryFn: (session) => studioApi.listSourceChapters(session, projectId, effectiveSourceId).then((response) => response.items || []),
    enabled: !!effectiveSourceId && selectedSource?.sourceType === "novel",
  });

  const selectedChapter = useMemo(
    () => chapters.find((chapter) => chapter.id === selectedChapterId) ?? chapters[0] ?? null,
    [chapters, selectedChapterId],
  );

  const effectiveChapterId = selectedChapter?.id ?? "";
  const chapterIdSet = useMemo(() => new Set(chapters.map((chapter) => chapter.id)), [chapters]);
  const selectedValidChapterIds = useMemo(
    () => selectedChapterIds.filter((chapterId) => chapterIdSet.has(chapterId)),
    [chapterIdSet, selectedChapterIds],
  );
  const selectedValidChapterIdSet = useMemo(() => new Set(selectedValidChapterIds), [selectedValidChapterIds]);

  const { data: selectedChapterDetail } = useApiQuery({
    key: qk.sourceChapter(projectId, effectiveSourceId, effectiveChapterId),
    queryFn: (session) => studioApi.getSourceChapter(session, projectId, effectiveSourceId, effectiveChapterId),
    enabled: !!effectiveSourceId && !!effectiveChapterId,
  });

  // 获取选中分集的事件
  const { data: eventsData } = useApiQuery({
    key: qk.sourceEvents(projectId, effectiveSourceId, effectiveChapterId),
    queryFn: (session) =>
      studioApi.listSourceNovelEvents(session, projectId, effectiveSourceId, { chapterId: effectiveChapterId }),
    enabled: !!effectiveSourceId && !!effectiveChapterId,
  });

  const events = eventsData?.items || [];

  // 获取改编计划
  const { data: plans = [] } = useApiQuery({
    key: qk.adaptationPlans(projectId),
    queryFn: (session) =>
      studioApi.listAdaptationPlans(session, projectId).then(r => r.items || []),
  });

  const selectedPlanSummary = useMemo(
    () => plans.find((plan) => plan.id === selectedPlanId) ?? plans[0] ?? null,
    [plans, selectedPlanId],
  );
  const effectivePlanId = selectedPlanSummary?.id ?? "";
  const { data: selectedPlanDetail } = useApiQuery({
    key: qk.adaptationPlan(projectId, effectivePlanId),
    queryFn: (session) => studioApi.getAdaptationPlan(session, projectId, effectivePlanId),
    enabled: !!effectivePlanId,
  });
  const selectedPlan = selectedPlanDetail ?? selectedPlanSummary;

  const selectedScriptSummary = useMemo(
    () =>
      scripts.find((script) => script.id === selectedScriptId) ??
      scripts.find((script) => script.id === generatedScriptFocus?.scriptId) ??
      currentProjectScript(scripts) ??
      null,
    [generatedScriptFocus?.scriptId, scripts, selectedScriptId],
  );
  const effectiveScriptId = selectedScriptSummary?.id ?? "";
  const { data: selectedScriptDetail } = useApiQuery({
    key: qk.script(projectId, effectiveScriptId),
    queryFn: (session) => studioApi.getScript(session, projectId, effectiveScriptId),
    enabled: !!effectiveScriptId,
  });
  const selectedScript = selectedScriptDetail ?? selectedScriptSummary;
  const { data: scriptVersions = [] } = useApiQuery({
    key: qk.scriptVersions(projectId, effectiveScriptId),
    queryFn: (session) => studioApi.listScriptVersions(session, projectId, effectiveScriptId).then((response) => response.items || []),
    enabled: !!effectiveScriptId,
  });
  const selectedScriptVersion = useMemo(
    () =>
      scriptVersions.find((version) => version.id === generatedScriptFocus?.versionId) ??
      scriptVersions.find((version) => version.id === selectedScriptVersionId) ??
      scriptVersions.find((version) => version.id === selectedScript?.currentVersionId) ??
      selectedScript?.currentVersion ??
      scriptVersions[0] ??
      null,
    [generatedScriptFocus?.versionId, scriptVersions, selectedScript?.currentVersion, selectedScript?.currentVersionId, selectedScriptVersionId],
  );
  const effectiveScriptVersionId = selectedScriptVersion?.id ?? "";
  const { data: scriptEpisodes = [], isLoading: scriptEpisodesLoading } = useApiQuery({
    key: qk.scriptEpisodes(projectId, effectiveScriptId, effectiveScriptVersionId),
    queryFn: (session) =>
      studioApi.listScriptEpisodes(session, projectId, effectiveScriptId, effectiveScriptVersionId).then((response) => response.items || []),
    enabled: !!effectiveScriptId && !!effectiveScriptVersionId,
  });
  const selectedScriptEpisode = useMemo(
    () => scriptEpisodes.find((episode) => episode.id === selectedScriptEpisodeId) ?? scriptEpisodes[0] ?? null,
    [scriptEpisodes, selectedScriptEpisodeId],
  );
  const latestAssetExtractionError = useMemo(
    () => workflowRuns.find((run) => workflowRunType(run) === "script_to_assets" && run.status === "failed" && (run.errorCode || run.errorMessage)) ?? null,
    [workflowRuns],
  );

  const planEditForm = useMemo(() => {
    if (!selectedPlan) {
      return emptyAdaptationPlanEditForm();
    }
    if (planEditDraft?.planId === selectedPlan.id) {
      return planEditDraft.form;
    }
    return adaptationPlanToForm(selectedPlan);
  }, [planEditDraft, selectedPlan]);

  const setPlanEditForm = (form: AdaptationPlanEditForm) => {
    if (!selectedPlan) {
      setPlanEditDraft(null);
      return;
    }
    setPlanEditDraft({ planId: selectedPlan.id, form });
  };

  const scriptEpisodeEditForm = useMemo(() => {
    if (!selectedScriptEpisode) {
      return emptyScriptEpisodeEditForm();
    }
    if (scriptEpisodeEditDraft?.episodeId === selectedScriptEpisode.id) {
      return scriptEpisodeEditDraft.form;
    }
    return scriptEpisodeToForm(selectedScriptEpisode);
  }, [scriptEpisodeEditDraft, selectedScriptEpisode]);

  const setScriptEpisodeEditForm = (form: ScriptEpisodeEditForm) => {
    if (!selectedScriptEpisode) {
      setScriptEpisodeEditDraft(null);
      return;
    }
    setScriptEpisodeEditDraft({ episodeId: selectedScriptEpisode.id, form });
  };

  // 提取事件
  const extractEventsMutation = useApiMutation({
    mutationFn: (session, payload: { sourceId: string; chapterIds: string[] }) =>
      studioApi.extractNovelEvents(session, projectId, payload.sourceId, { chapterIds: payload.chapterIds }),
    onSuccess: (run, payload) => {
      toast.success("事件提取已启动");
      setActivityOpen(true);
      setSelectedSourceId(payload.sourceId);
      if (payload.chapterIds[0]) {
        setSelectedChapterId(payload.chapterIds[0]);
      }
      setActiveTab("sources");
      invalidate([
        qk.sources(projectId),
        qk.sourceChapters(projectId, payload.sourceId),
        ...payload.chapterIds.map((chapterId) => qk.sourceEvents(projectId, payload.sourceId, chapterId)),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
      if (run.id) {
        toast.message("任务已启动");
      }
    },
    onError: (error) => {
      toast.error("提取失败：" + error.message);
    },
  });

  const loadSourceForEditMutation = useApiMutation({
    mutationFn: (session, sourceId: string) => studioApi.getSource(session, projectId, sourceId),
    onSuccess: (source) => {
      setEditingSource(source);
        setSourceEditForm({
          title: source.title,
          sourceType: source.sourceType === "script" ? "script" : source.sourceType === "brief" ? "brief" : "novel",
          contentFormat: source.contentFormat || "plain_text",
          content: source.content || "",
          splitChapters: source.sourceType === "novel",
      });
      setSourceEditOpen(true);
    },
    onError: (error) => toast.error("读取原文失败：" + error.message),
  });

  // 生成改编计划
  const createPlanMutation = useApiMutation({
    mutationFn: (session, data: { sourceId: string; targetFormat: string }) =>
      studioApi.generateAdaptationPlan(session, projectId, data.sourceId, {
        targetFormat: data.targetFormat,
      }),
    onSuccess: (plan) => {
      toast.success("改编计划已创建");
      setSelectedPlanId(plan.id);
      setPlanEditDraft({ planId: plan.id, form: adaptationPlanToForm(plan) });
      invalidate([qk.adaptationPlans(projectId)]);
      setActiveTab("sources");
    },
    onError: (error) => {
      toast.error("创建失败：" + error.message);
    },
  });

  const updatePlanMutation = useApiMutation({
    mutationFn: (session, data: { planId: string; body: JsonRecord }) =>
      studioApi.updateAdaptationPlan(session, projectId, data.planId, data.body),
    onSuccess: (plan) => {
      toast.success("改编计划已保存");
      setSelectedPlanId(plan.id);
      setPlanEditDraft({ planId: plan.id, form: adaptationPlanToForm(plan) });
      invalidate([
        qk.adaptationPlans(projectId),
        qk.adaptationPlan(projectId, plan.id),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("保存失败：" + error.message);
    },
  });

  const activatePlanMutation = useApiMutation({
    mutationFn: (session, planId: string) => studioApi.activateAdaptationPlan(session, projectId, planId),
    onSuccess: (plan) => {
      toast.success("改编计划已激活");
      setSelectedPlanId(plan.id);
      setPlanEditDraft({ planId: plan.id, form: adaptationPlanToForm(plan) });
      invalidate([
        qk.adaptationPlans(projectId),
        qk.adaptationPlan(projectId, plan.id),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("激活失败：" + error.message);
    },
  });

  // 从计划生成剧本
  const generateScriptMutation = useApiMutation({
    mutationFn: (session, planId: string) =>
      studioApi.generateScriptFromAdaptationPlan(session, projectId, planId, {}),
    onSuccess: (result) => {
      toast.success("剧本生成已启动");
      setSelectedScriptId(result.scriptId);
      setSelectedScriptVersionId(result.versionId);
      invalidate([
        qk.scripts(projectId),
        qk.script(projectId, result.scriptId),
        qk.scriptVersions(projectId, result.scriptId),
        qk.scriptEpisodes(projectId, result.scriptId, result.versionId),
        qk.adaptationPlans(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
      setActiveTab("scripts");
    },
    onError: (error) => {
      toast.error("生成失败：" + error.message);
    },
  });

  const analyzeScriptAssetsMutation = useApiMutation({
    mutationFn: (session, scriptId: string) =>
      studioApi.analyzeScriptAssets(session, projectId, scriptId, { mergeExisting: true, generateImages: false }),
    onSuccess: (run) => {
      toast.success(`资产提取工作流已创建：${run.id.slice(0, 8)}`);
      setActivityOpen(true);
      invalidate([
        qk.assetsRoot(projectId),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("提取失败：" + error.message);
    },
  });

  const updateScriptEpisodeMutation = useApiMutation({
    mutationFn: (session, data: { episodeId: string; body: JsonRecord }) =>
      studioApi.updateScriptEpisode(session, projectId, data.episodeId, data.body),
    onSuccess: (episode) => {
      toast.success("分集已保存");
      setSelectedScriptEpisodeId(episode.id);
      setScriptEpisodeEditDraft({ episodeId: episode.id, form: scriptEpisodeToForm(episode) });
      invalidate([
        qk.scriptEpisodes(projectId, episode.scriptId, episode.scriptVersionId),
        qk.scriptVersions(projectId, episode.scriptId),
        qk.script(projectId, episode.scriptId),
        qk.scriptScenes(projectId, episode.scriptId, episode.scriptVersionId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("保存分集失败：" + error.message);
    },
  });

  const importFileMutation = useApiMutation({
    mutationFn: (session) => {
      const title = importTitle.trim() || importFile?.name.replace(/\.[^.]+$/, "") || "";
      if (!title) {
        throw new Error("请填写标题");
      }
      if (importFile) {
        const form = new FormData();
        form.append("file", importFile);
        form.append("sourceType", importSourceType);
        form.append("title", title);
        form.append("splitChapters", splitChapters ? "true" : "false");
        form.append("createScript", createScript ? "true" : "false");
        return studioApi.importSourceFile(session, projectId, form);
      }
      if (!importContent.trim()) {
        throw new Error("请填写正文或选择文件");
      }
      return studioApi.createSource(session, projectId, {
        sourceType: importSourceType,
        title,
        content: importContent,
        contentFormat: "plain_text",
        splitChapters,
        createScript,
      });
    },
    onSuccess: (response) => {
      toast.success("内容已添加");
      setSelectedSourceId(response.source.id);
      setSelectedChapterId(response.chapters[0]?.id ?? null);
      setSelectedChapterIds([]);
      if (response.script?.id) {
        setSelectedScriptId(response.script.id);
        setActiveTab("scripts");
      } else {
        setActiveTab("sources");
      }
      setImportOpen(false);
      setImportFile(null);
      setImportTitle("");
      setImportContent("");
      invalidate([
        qk.sources(projectId),
        qk.sourceChapters(projectId, response.source.id),
        qk.scripts(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("导入失败：" + error.message);
    },
  });

  const updateSourceMutation = useApiMutation({
    mutationFn: (session, data: { sourceId: string; body: JsonRecord }) =>
      studioApi.updateSource(session, projectId, data.sourceId, data.body),
    onSuccess: (source) => {
      toast.success("原文已保存");
      setSelectedSourceId(source.id);
      setSourceEditOpen(false);
      setEditingSource(null);
      invalidate([
        qk.sources(projectId),
        qk.sourceChapters(projectId, source.id),
        qk.sourceEvents(projectId, source.id),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("保存失败：" + error.message);
    },
  });

  const deleteSourceMutation = useApiMutation({
    mutationFn: (session, sourceId: string) => studioApi.deleteSource(session, projectId, sourceId),
    onSuccess: (_result, sourceId) => {
      toast.success("原文已归档");
      if (selectedSourceId === sourceId) {
        setSelectedSourceId(null);
        setSelectedChapterId(null);
        setSelectedChapterIds([]);
      }
      setSourceToDelete(null);
      invalidate([
        qk.sources(projectId),
        qk.sourceImpact(projectId, sourceId),
        qk.sourceEvents(projectId, sourceId),
        qk.scripts(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("删除失败：" + error.message);
    },
  });

  const updateImportSourceType = (value: string) => {
    const next: ImportSourceType = value === "script" ? "script" : value === "brief" ? "brief" : "novel";
    setImportSourceType(next);
    setSplitChapters(next === "novel");
    setCreateScript(next === "script");
  };

  const openEditSourceDialog = (source: ProjectSource) => {
    loadSourceForEditMutation.mutate(source.id);
  };

  const handleSaveSource = () => {
    if (!editingSource) {
      return;
    }
    if (!sourceEditForm.title.trim() || !sourceEditForm.content.trim()) {
      toast.error("请填写标题和正文");
      return;
    }
    updateSourceMutation.mutate({
      sourceId: editingSource.id,
      body: {
        title: sourceEditForm.title.trim(),
        sourceType: sourceEditForm.sourceType,
        contentFormat: sourceEditForm.contentFormat,
        content: sourceEditForm.content,
        splitChapters: sourceEditForm.sourceType === "novel" && sourceEditForm.splitChapters,
      },
    });
  };

  const handleSelectSource = (source: ProjectSource) => {
    setSelectedSourceId(source.id);
    setSelectedChapterId(null);
    setSelectedChapterIds([]);
  };

  const toggleChapterSelection = (chapterId: string) => {
    setSelectedChapterIds((current) =>
      current.includes(chapterId) ? current.filter((id) => id !== chapterId) : [...current, chapterId],
    );
  };

  const selectAllChapters = () => {
    setSelectedChapterIds(chapters.map((chapter) => chapter.id));
  };

  const clearSelectedChapters = () => {
    setSelectedChapterIds([]);
  };

  const extractCurrentChapter = () => {
    if (!selectedSource || !selectedChapter) {
      toast.error("请先选择分集");
      return;
    }
    extractEventsMutation.mutate({ sourceId: selectedSource.id, chapterIds: [selectedChapter.id] });
  };

  const extractSelectedChapters = () => {
    if (!selectedSource) {
      toast.error("请先选择原文");
      return;
    }
    if (selectedValidChapterIds.length === 0) {
      toast.error("请选择要提取的分集");
      return;
    }
    extractEventsMutation.mutate({ sourceId: selectedSource.id, chapterIds: selectedValidChapterIds });
  };

  const handleSelectPlan = (plan: AdaptationPlan) => {
    setSelectedPlanId(plan.id);
  };

  const handleSavePlan = () => {
    if (!selectedPlan) {
      toast.error("请先选择改编计划");
      return;
    }
    const title = planEditForm.title.trim();
    if (!title) {
      toast.error("请填写计划名称");
      return;
    }
    updatePlanMutation.mutate({
      planId: selectedPlan.id,
      body: {
        title,
        status: planEditForm.status || "draft",
        targetFormat: planEditForm.targetFormat || "short_video",
        targetDurationSeconds: positiveIntegerFromText(planEditForm.targetDurationSeconds),
        maxShots: positiveIntegerFromText(planEditForm.maxShots),
        content: planEditForm.content,
      },
    });
  };

  const handleActivatePlan = () => {
    if (!selectedPlan) {
      toast.error("请先选择改编计划");
      return;
    }
    activatePlanMutation.mutate(selectedPlan.id);
  };

  const handleGenerateScriptFromSelectedPlan = () => {
    if (!selectedPlan) {
      toast.error("请先选择改编计划");
      return;
    }
    generateScriptMutation.mutate(selectedPlan.id);
  };

  const handleSelectScript = (scriptId: string) => {
    setSelectedScriptId(scriptId);
    setSelectedScriptVersionId(null);
    setSelectedScriptEpisodeId(null);
    setScriptEpisodeEditDraft(null);
  };

  const handleAnalyzeAssetsFromSelectedScript = () => {
    if (!selectedScript) {
      toast.error("请先选择剧本");
      return;
    }
    analyzeScriptAssetsMutation.mutate(selectedScript.id);
  };

  const handleSelectScriptEpisode = (episode: ScriptEpisode) => {
    setSelectedScriptEpisodeId(episode.id);
    setScriptEpisodeEditDraft({ episodeId: episode.id, form: scriptEpisodeToForm(episode) });
    setScriptEpisodeDialogOpen(true);
  };

  const handleSaveScriptEpisode = () => {
    if (!selectedScriptEpisode) {
      toast.error("请先选择剧本分集");
      return;
    }
    const title = scriptEpisodeEditForm.episodeTitle.trim();
    if (!title) {
      toast.error("请填写分集标题");
      return;
    }
    if (!scriptEpisodeEditForm.content.trim()) {
      toast.error("请填写分集内容");
      return;
    }
    updateScriptEpisodeMutation.mutate({
      episodeId: selectedScriptEpisode.id,
      body: {
        episodeTitle: title,
        content: scriptEpisodeEditForm.content,
        contentFormat: scriptEpisodeEditForm.contentFormat || "markdown",
        reviewStatus: scriptEpisodeEditForm.reviewStatus || "pending",
      },
    });
  };

  const handleUseAgent = () => {
    setContext({
      projectId,
      sourceId: effectiveSourceId || selectedSourceId,
      scriptId: selectedScriptId,
    });
    openAgent();
  };

  return (
    <Surface>
      <SectionTitle
        title={visibleTab === "scripts" ? "剧本" : "内容"}
        description={visibleTab === "scripts" ? "查看、编辑、激活剧本版本" : "添加小说原文、剧本或创意文案并管理分集"}
      />

      <Tabs
        value={visibleTab}
        onValueChange={(value) => {
          if (mode === "combined") {
            setActiveTab(value as SourcesTab);
          }
        }}
        className="p-4"
      >
        {mode === "combined" ? (
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="sources">
              内容
              <Badge variant="secondary" className="ml-2">{sources.length}</Badge>
            </TabsTrigger>
            <TabsTrigger value="scripts">
              剧本
              <Badge variant="secondary" className="ml-2">{scripts.length}</Badge>
            </TabsTrigger>
          </TabsList>
        ) : null}

        {/* 原文管理 */}
        <TabsContent value="sources" className="space-y-4">
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" onClick={() => setImportOpen(true)}>
              <Upload className="h-4 w-4 mr-2" />
              添加内容
            </Button>
            <Button size="sm" onClick={handleUseAgent}>
              <Wand2 className="h-4 w-4 mr-2" />
              使用助手
            </Button>
          </div>

          <Dialog open={importOpen} onOpenChange={setImportOpen}>
            <DialogContent className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-md">
              <DialogHeader>
                <DialogTitle>添加内容</DialogTitle>
              </DialogHeader>
              <div className="grid min-h-0 gap-4 overflow-y-auto pr-1">
                <div className="grid gap-2">
                  <Label htmlFor="source-file">文件</Label>
                  <Input
                    id="source-file"
                    accept=".txt,.md,.markdown,text/plain,text/markdown"
                    type="file"
                    onChange={(event) => {
                      const file = event.currentTarget.files?.[0] ?? null;
                      setImportFile(file);
                      if (file && !importTitle.trim()) {
                        setImportTitle(file.name.replace(/\.[^.]+$/, ""));
                      }
                    }}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="source-title">标题</Label>
                  <Input id="source-title" value={importTitle} onChange={(event) => setImportTitle(event.target.value)} />
                </div>
                <div className="grid gap-2">
                  <Label>类型</Label>
                  <Select value={importSourceType} onValueChange={updateImportSourceType}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="novel">小说</SelectItem>
                        <SelectItem value="script">剧本</SelectItem>
                        <SelectItem value="brief">创意文案</SelectItem>
                      </SelectContent>
                  </Select>
                </div>
                {!importFile && (
                  <div className="grid gap-2">
                    <Label htmlFor="source-content">正文</Label>
                    <Textarea
                      id="source-content"
                      className="field-sizing-fixed min-h-48 resize-y"
                      value={importContent}
                      onChange={(event) => setImportContent(event.target.value)}
                    />
                  </div>
                )}
                {importSourceType === "novel" && (
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox checked={splitChapters} onCheckedChange={(checked) => setSplitChapters(checked === true)} />
                    自动分集/章节
                  </label>
                )}
                {importSourceType === "script" && (
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox checked={createScript} onCheckedChange={(checked) => setCreateScript(checked === true)} />
                    创建剧本
                  </label>
                )}
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setImportOpen(false)} type="button">
                  取消
                </Button>
                <Button disabled={(!importFile && !importContent.trim()) || importFileMutation.isPending} onClick={() => importFileMutation.mutate()} type="button">
                  添加
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          <Dialog open={sourceEditOpen} onOpenChange={setSourceEditOpen}>
            <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-3xl">
              <DialogHeader>
                <DialogTitle>编辑原文</DialogTitle>
              </DialogHeader>
              <div className="grid gap-4">
                <div className="grid gap-2">
                  <Label htmlFor="edit-source-title">标题</Label>
                  <Input
                    id="edit-source-title"
                    value={sourceEditForm.title}
                    onChange={(event) => setSourceEditForm({ ...sourceEditForm, title: event.target.value })}
                  />
                </div>
                <div className="grid gap-3 md:grid-cols-2">
                  <div className="grid gap-2">
                    <Label>类型</Label>
                    <Select
                      value={sourceEditForm.sourceType}
                      onValueChange={(value) =>
                        setSourceEditForm({
                          ...sourceEditForm,
                          sourceType: value === "script" ? "script" : value === "brief" ? "brief" : "novel",
                          splitChapters: value === "novel",
                        })
                      }
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="novel">小说</SelectItem>
                        <SelectItem value="script">剧本</SelectItem>
                        <SelectItem value="brief">创意文案</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label>格式</Label>
                    <Select
                      value={sourceEditForm.contentFormat}
                      onValueChange={(value) => setSourceEditForm({ ...sourceEditForm, contentFormat: value })}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="plain_text">纯文本</SelectItem>
                        <SelectItem value="markdown">Markdown</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                {sourceEditForm.sourceType === "novel" && (
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={sourceEditForm.splitChapters}
                      onCheckedChange={(checked) => setSourceEditForm({ ...sourceEditForm, splitChapters: checked === true })}
                    />
                    保存时重新切分分集/章节
                  </label>
                )}
                <div className="grid gap-2">
                  <Label htmlFor="edit-source-content">正文</Label>
                  <Textarea
                    id="edit-source-content"
                    className="min-h-80 font-mono text-sm"
                    value={sourceEditForm.content}
                    onChange={(event) => setSourceEditForm({ ...sourceEditForm, content: event.target.value })}
                  />
                </div>
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setSourceEditOpen(false)} type="button">
                  取消
                </Button>
                <Button disabled={updateSourceMutation.isPending} onClick={handleSaveSource} type="button">
                  保存
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          <Dialog open={!!sourceToDelete} onOpenChange={(open) => !open && setSourceToDelete(null)}>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>归档原文</DialogTitle>
              </DialogHeader>
              <div className="grid gap-4 text-sm">
                <div>
                  <div className="font-medium">{sourceToDelete?.title}</div>
                  <div className="mt-1 text-muted-foreground">归档后不会出现在默认列表，也不会再作为后续生产入口。</div>
                </div>
                {sourceDeleteImpactLoading ? (
                  <Skeleton className="h-20" />
                ) : sourceDeleteImpact ? (
                  <div className="grid gap-3 rounded-lg border bg-muted/30 p-3">
                    <div className="font-medium">影响范围</div>
                    {sourceDeleteImpact.affected.length > 0 ? (
                      <div className="grid gap-2">
                        {sourceDeleteImpact.affected.map((item) => (
                          <div key={item.entityType} className="flex items-center justify-between gap-3">
                            <span className="text-muted-foreground">{sourceImpactEntityLabel(item.entityType)}</span>
                            <Badge variant="outline">{item.count}</Badge>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="text-muted-foreground">没有关联产物。</div>
                    )}
                    {sourceDeleteImpact.warnings.length > 0 ? (
                      <div className="grid gap-1 text-xs text-muted-foreground">
                        {sourceDeleteImpact.warnings.map((warning) => (
                          <div key={warning}>{warning}</div>
                        ))}
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setSourceToDelete(null)} type="button">
                  取消
                </Button>
                <Button
                  variant="destructive"
                  disabled={deleteSourceMutation.isPending || !sourceToDelete}
                  onClick={() => sourceToDelete && deleteSourceMutation.mutate(sourceToDelete.id)}
                  type="button"
                >
                  归档
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>

          {sourcesLoading && <Skeleton className="h-32" />}

          {!sourcesLoading && sources.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <FileText className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无原文</p>
              <p className="mt-1 text-xs text-muted-foreground">点击“导入文件”上传小说原文</p>
            </div>
          )}

          {sources.length > 0 && (
            <div className="grid gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
              <div className="grid content-start gap-3">
                {sources.map((source) => (
                  <div
                    key={source.id}
                    onClick={() => handleSelectSource(source)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        handleSelectSource(source);
                      }
                    }}
                    className={cn(
                      "rounded-lg border p-4 text-left transition hover:bg-muted/50",
                      selectedSource?.id === source.id && "bg-muted/50 ring-2 ring-primary"
                    )}
                    role="button"
                    tabIndex={0}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="truncate font-medium">{source.title}</div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {sourceTypeLabel(source.sourceType)} · {contentFormatLabel(source.contentFormat)}
                        </div>
                      </div>
                      <StatusBadge status={source.status} />
                    </div>
                    <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
                      {source.sourceType === "novel" ? (
                        <Badge variant="outline">分集/章节 {sourceChapterCount(source)}</Badge>
                      ) : null}
                      {source.originalFileName ? <Badge variant="outline">{source.originalFileName}</Badge> : null}
                    </div>
                    <div className="mt-3 flex gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={(event) => {
                          event.stopPropagation();
                          openEditSourceDialog(source);
                        }}
                        disabled={loadSourceForEditMutation.isPending}
                      >
                        <Edit2 className="h-4 w-4" />
                        编辑
                      </Button>
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={(event) => {
                          event.stopPropagation();
                          setSourceToDelete(source);
                        }}
                        disabled={deleteSourceMutation.isPending}
                      >
                        <Trash2 className="h-4 w-4" />
                        删除
                      </Button>
                    </div>
                  </div>
                ))}
              </div>

              <div className="grid content-start gap-4">
                {!selectedSource ? (
                  <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
                    请选择原文
                  </div>
                ) : selectedSource.sourceType !== "novel" ? (
                  <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
                    当前内容不需要分集提取事件。
                  </div>
                ) : (
                  <>
                    <div className="rounded-lg border p-4">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div>
                          <div className="text-sm text-muted-foreground">当前原文</div>
                          <h3 className="mt-1 text-lg font-semibold">{selectedSource.title}</h3>
                          <div className="mt-2 flex flex-wrap gap-2">
                            <Badge variant="outline">分集/章节 {chapters.length || sourceChapterCount(selectedSource)}</Badge>
                            <Badge variant="outline">已提取事件 {chapters.reduce((sum, chapter) => sum + chapter.eventCount, 0)}</Badge>
                            <Badge variant="outline">已确认 {chapters.reduce((sum, chapter) => sum + chapter.approvedEventCount, 0)}</Badge>
                          </div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          <Button size="sm" variant="outline" onClick={selectAllChapters} disabled={chapters.length === 0}>
                            全选分集
                          </Button>
                          <Button size="sm" variant="outline" onClick={clearSelectedChapters} disabled={selectedValidChapterIds.length === 0}>
                            清空选择
                          </Button>
                          <Button size="sm" onClick={extractSelectedChapters} disabled={extractEventsMutation.isPending || selectedValidChapterIds.length === 0}>
                            <ListChecks className="mr-1 h-3.5 w-3.5" />
                            提取所选 {selectedValidChapterIds.length || ""}
                          </Button>
                        </div>
                      </div>
                    </div>

                    {chaptersLoading ? <Skeleton className="h-64" /> : null}
                    {!chaptersLoading && chapters.length === 0 ? (
                      <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
                        当前原文没有分集/章节，请编辑原文并开启重新切分，或重新导入。
                      </div>
                    ) : null}

                    <div className="grid gap-4 xl:grid-cols-[minmax(360px,1fr)_minmax(320px,420px)]">
                      <div className="grid max-h-[680px] content-start gap-2 overflow-y-auto pr-1">
                        {chapters.map((chapter) => (
                          <div
                            key={chapter.id}
                            className={cn(
                              "grid gap-2 rounded-lg border p-3 text-left transition hover:bg-muted/50",
                              selectedChapter?.id === chapter.id && "bg-muted/50 ring-2 ring-primary"
                            )}
                            onClick={() => setSelectedChapterId(chapter.id)}
                            onKeyDown={(event) => {
                              if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                setSelectedChapterId(chapter.id);
                              }
                            }}
                            role="button"
                            tabIndex={0}
                          >
                            <div className="flex items-start gap-3">
                              <Checkbox
                                checked={selectedValidChapterIdSet.has(chapter.id)}
                                onClick={(event) => event.stopPropagation()}
                                onCheckedChange={() => toggleChapterSelection(chapter.id)}
                              />
                              <div className="min-w-0 flex-1">
                                <div className="flex flex-wrap items-center gap-2">
                                  <span className="font-medium">{chapterDisplayTitle(chapter)}</span>
                                  {chapterOrdinalLabel(chapter) ? <Badge variant="outline">{chapterOrdinalLabel(chapter)}</Badge> : null}
                                  <StatusBadge status={chapter.eventState} />
                                </div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                  {chapter.volumeTitle ? `${chapter.volumeTitle} · ` : ""}
                                  {formatContentLength(chapter.contentLength)} · 事件 {chapter.eventCount} · 已确认 {chapter.approvedEventCount}
                                </div>
                              </div>
                            </div>
                            {chapter.errorMessage ? <div className="text-xs text-destructive">{localizePlatformError(chapter.errorMessage)}</div> : null}
                          </div>
                        ))}
                      </div>

                      <div className="grid max-h-[680px] content-start gap-3 overflow-y-auto rounded-lg border p-4">
                        {selectedChapter ? (
                          <>
                            <div>
                              <div className="text-sm text-muted-foreground">选中分集</div>
                              <h4 className="mt-1 font-semibold">{chapterDisplayTitle(selectedChapter)}</h4>
                              <div className="mt-2 flex flex-wrap gap-2">
                                <StatusBadge status={selectedChapter.eventState} />
                                {chapterOrdinalLabel(selectedChapter) ? <Badge variant="outline">{chapterOrdinalLabel(selectedChapter)}</Badge> : null}
                                <Badge variant="outline">事件 {selectedChapter.eventCount}</Badge>
                                <Badge variant="outline">{formatContentLength(selectedChapter.contentLength)}</Badge>
                              </div>
                            </div>
                            <div className="flex flex-wrap gap-2">
                              <Button size="sm" onClick={extractCurrentChapter} disabled={extractEventsMutation.isPending}>
                                提取当前分集
                              </Button>
                              <Button size="sm" variant="outline" onClick={() => toggleChapterSelection(selectedChapter.id)}>
                                {selectedValidChapterIdSet.has(selectedChapter.id) ? "取消选择" : "加入选择"}
                              </Button>
                            </div>
                            <div className="rounded-md bg-muted/50 p-3 text-sm leading-6 text-muted-foreground">
                              <div className="mb-2 text-xs text-muted-foreground">正文预览</div>
                              <p className="whitespace-pre-wrap">{chapterPreview(selectedChapterDetail?.content || "")}</p>
                            </div>
                          </>
                        ) : (
                          <div className="py-8 text-center text-sm text-muted-foreground">请选择分集</div>
                        )}
                      </div>
                    </div>
                    <SourceAdvancedDetails
                      selectedSource={selectedSource}
                      selectedChapter={selectedChapter}
                      events={events}
                      plans={plans}
                      selectedPlan={selectedPlan}
                      planEditForm={planEditForm}
                      onPlanFormChange={setPlanEditForm}
                      onSelectPlan={handleSelectPlan}
                      onExtractCurrentChapter={extractCurrentChapter}
                      onCreatePlan={() => createPlanMutation.mutate({ sourceId: effectiveSourceId, targetFormat: "short_video" })}
                      onGenerateScript={handleGenerateScriptFromSelectedPlan}
                      onActivatePlan={handleActivatePlan}
                      onSavePlan={handleSavePlan}
                      isExtracting={extractEventsMutation.isPending}
                      isCreatingPlan={createPlanMutation.isPending}
                      isGeneratingScript={generateScriptMutation.isPending}
                      isActivatingPlan={activatePlanMutation.isPending}
                      isSavingPlan={updatePlanMutation.isPending}
                    />
                  </>
                )}
              </div>
            </div>
          )}
        </TabsContent>

        {/* 小说事件 */}
        <TabsContent value="events" className="space-y-4">
          {!effectiveSourceId && (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <AlertCircle className="mx-auto h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-sm text-muted-foreground">请先选择一个原文</p>
            </div>
          )}

          {effectiveSourceId && !effectiveChapterId && (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <Clock className="mx-auto h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-sm text-muted-foreground">请先选择一个分集</p>
            </div>
          )}

          {effectiveSourceId && effectiveChapterId && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4">
              <div>
                <div className="text-sm text-muted-foreground">当前分集</div>
                <div className="font-medium">{selectedChapter ? chapterDisplayTitle(selectedChapter) : "未选择"}</div>
              </div>
              <Button size="sm" onClick={extractCurrentChapter} disabled={extractEventsMutation.isPending || !selectedChapter}>
                提取当前分集
              </Button>
            </div>
          )}

          {effectiveSourceId && effectiveChapterId && events.length === 0 && (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <Clock className="mx-auto h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-sm text-muted-foreground">当前分集暂无事件</p>
            </div>
          )}

          <div className="space-y-2">
            {events.map((event) => (
              <div
                key={event.id}
                className="flex items-start gap-3 rounded-lg border p-3 text-sm"
              >
                <div className="font-mono text-xs text-muted-foreground">
                  {event.eventIndex}
                </div>
                <div className="flex-1">
                  <div className="font-medium">{event.title}</div>
                  <div className="text-muted-foreground line-clamp-2">{event.summary}</div>
                </div>
                <Badge variant={event.reviewStatus === "approved" ? "default" : "secondary"}>
                  {statusLabel(event.reviewStatus)}
                </Badge>
              </div>
            ))}
          </div>
        </TabsContent>

        {/* 改编计划 */}
        <TabsContent value="plans" className="space-y-4">
          <div className="flex justify-end">
            <Button
              size="sm"
              onClick={() => {
                if (!effectiveSourceId) {
                  toast.error("请先选择原文");
                  return;
                }
                createPlanMutation.mutate({
                  sourceId: effectiveSourceId,
                  targetFormat: "short_video",
                });
              }}
              disabled={createPlanMutation.isPending}
            >
              创建改编计划
            </Button>
          </div>

          {plans.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <p className="text-sm text-muted-foreground">暂无改编计划</p>
            </div>
          )}

          {plans.length > 0 && (
            <div className="grid gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
              <div className="grid content-start gap-3">
                {plans.map((plan) => (
                  <button
                    key={plan.id}
                    type="button"
                    onClick={() => handleSelectPlan(plan)}
                    className={cn(
                      "rounded-lg border p-4 text-left transition hover:bg-muted/50",
                      selectedPlan?.id === plan.id && "bg-muted/50 ring-2 ring-primary",
                    )}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="truncate font-medium">{plan.title}</div>
                        <div className="mt-1 text-xs text-muted-foreground">{targetFormatLabel(plan.targetFormat)}</div>
                      </div>
                      <StatusBadge status={plan.status} />
                    </div>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Badge variant="outline">事件 {selectedEventCount(plan)}</Badge>
                      {plan.targetDurationSeconds ? <Badge variant="outline">{plan.targetDurationSeconds} 秒</Badge> : null}
                      {plan.maxShots ? <Badge variant="outline">镜头 {plan.maxShots}</Badge> : null}
                    </div>
                    <div className="mt-3 text-xs text-muted-foreground">
                      {plan.updatedAt ? `更新于 ${formatDateTime(plan.updatedAt)}` : "未记录更新时间"}
                    </div>
                  </button>
                ))}
              </div>

              <div className="rounded-lg border p-4">
                {selectedPlan ? (
                  <div className="grid gap-4">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <div className="text-sm text-muted-foreground">计划详情</div>
                        <h3 className="mt-1 text-lg font-semibold">{selectedPlan.title}</h3>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <StatusBadge status={selectedPlan.status} />
                        <Badge variant="outline">{statusLabel(selectedPlan.reviewStatus)}</Badge>
                      </div>
                    </div>

                    <div className="grid gap-3 md:grid-cols-2">
                      <div className="grid gap-2 md:col-span-2">
                        <Label htmlFor="plan-title">计划名称</Label>
                        <Input
                          id="plan-title"
                          value={planEditForm.title}
                          onChange={(event) => setPlanEditForm({ ...planEditForm, title: event.target.value })}
                        />
                      </div>
                      <div className="grid gap-2">
                        <Label>状态</Label>
                        <Select
                          value={planEditForm.status}
                          onValueChange={(value) => setPlanEditForm({ ...planEditForm, status: value })}
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="draft">草稿</SelectItem>
                            <SelectItem value="active">启用</SelectItem>
                            <SelectItem value="archived">归档</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                      <div className="grid gap-2">
                        <Label>目标格式</Label>
                        <Select
                          value={planEditForm.targetFormat}
                          onValueChange={(value) => setPlanEditForm({ ...planEditForm, targetFormat: value })}
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="short_video">短视频</SelectItem>
                            <SelectItem value="episode">剧集</SelectItem>
                            <SelectItem value="feature">长片</SelectItem>
                            <SelectItem value="outline">大纲</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                      <div className="grid gap-2">
                        <Label htmlFor="plan-duration">目标时长（秒）</Label>
                        <Input
                          id="plan-duration"
                          type="number"
                          min={0}
                          inputMode="numeric"
                          value={planEditForm.targetDurationSeconds}
                          onChange={(event) => setPlanEditForm({ ...planEditForm, targetDurationSeconds: event.target.value })}
                        />
                      </div>
                      <div className="grid gap-2">
                        <Label htmlFor="plan-max-shots">最大镜头数</Label>
                        <Input
                          id="plan-max-shots"
                          type="number"
                          min={0}
                          inputMode="numeric"
                          value={planEditForm.maxShots}
                          onChange={(event) => setPlanEditForm({ ...planEditForm, maxShots: event.target.value })}
                        />
                      </div>
                    </div>

                    <div className="grid gap-2">
                      <Label htmlFor="plan-content">计划内容</Label>
                      <Textarea
                        id="plan-content"
                        className="min-h-[420px] font-mono text-sm leading-6"
                        value={planEditForm.content}
                        onChange={(event) => setPlanEditForm({ ...planEditForm, content: event.target.value })}
                      />
                    </div>

                    <div className="flex flex-wrap justify-end gap-2">
                      <Button variant="outline" onClick={handleGenerateScriptFromSelectedPlan} disabled={generateScriptMutation.isPending}>
                        <Wand2 className="h-4 w-4" />
                        生成剧本
                      </Button>
                      <Button variant="outline" onClick={handleActivatePlan} disabled={activatePlanMutation.isPending || selectedPlan.status === "active"}>
                        <CheckCircle2 className="h-4 w-4" />
                        设为启用
                      </Button>
                      <Button onClick={handleSavePlan} disabled={updatePlanMutation.isPending}>
                        <Save className="h-4 w-4" />
                        保存计划
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="py-16 text-center text-sm text-muted-foreground">请选择改编计划</div>
                )}
              </div>
            </div>
          )}
        </TabsContent>

        {/* 剧本列表 */}
        <TabsContent value="scripts" className="space-y-4">
          {scriptsLoading && <Skeleton className="h-32" />}

          {!scriptsLoading && scripts.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <FileText className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无剧本</p>
              <p className="mt-1 text-xs text-muted-foreground">从改编计划生成或使用助手创建</p>
            </div>
          )}

          {scripts.length > 0 && (
            <div className="grid gap-4">
              <div className="flex flex-wrap items-end gap-3 rounded-lg border bg-background p-4">
                <div className="min-w-64 flex-1">
                  <Label className="mb-2 block">当前剧本</Label>
                  <Select value={selectedScript?.id ?? ""} onValueChange={handleSelectScript}>
                    <SelectTrigger>
                      <SelectValue placeholder="选择剧本" />
                    </SelectTrigger>
                    <SelectContent>
                      {scripts.map((script) => (
                        <SelectItem key={script.id} value={script.id}>
                          {script.title}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <div className="mt-2 text-xs text-muted-foreground">
                    {selectedScriptVersion ? `版本 ${selectedScriptVersion.version}` : "未选择版本"}
                    {scriptEpisodes.length ? ` · ${scriptEpisodes.length} 集` : ""}
                  </div>
                </div>
                <Button variant="outline" onClick={handleUseAgent}>
                  <Wand2 className="h-4 w-4" />
                  使用助手
                </Button>
                <Button onClick={handleAnalyzeAssetsFromSelectedScript} disabled={!selectedScript || analyzeScriptAssetsMutation.isPending}>
                  <Wand2 className="h-4 w-4" />
                  提取资产
                </Button>
              </div>

              {latestAssetExtractionError ? (
                <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="destructive">提取失败</Badge>
                  </div>
                  <p className="mt-2 text-sm text-muted-foreground">
                    {localizePlatformError(
                      latestAssetExtractionError.errorMessage,
                      latestAssetExtractionError.errorCode,
                      "资产提取未完成，请重新提取或检查供应商调用日志。",
                    )}
                  </p>
                </div>
              ) : null}

                {selectedScript ? (
                  <>
                    <div className="grid gap-3 rounded-lg border p-4">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div>
                          <div className="text-sm font-medium">剧本分集</div>
                          <div className="text-xs text-muted-foreground">
                            {selectedScriptVersion ? `版本 ${selectedScriptVersion.version}` : "未选择版本"}
                          </div>
                        </div>
                        <Badge variant="outline">{scriptEpisodes.length} 集</Badge>
                      </div>

                      {scriptEpisodesLoading ? <Skeleton className="h-24" /> : null}
                      {!scriptEpisodesLoading && scriptEpisodes.length === 0 ? (
                        <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">暂无分集</div>
                      ) : null}

                      {scriptEpisodes.length > 0 ? (
                        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                          {scriptEpisodes.map((episode) => (
                            <button
                              key={episode.id}
                              type="button"
                              onClick={() => handleSelectScriptEpisode(episode)}
                              className={cn(
                                "grid min-h-44 content-between gap-3 rounded-lg border p-4 text-left transition hover:bg-muted/50",
                                selectedScriptEpisode?.id === episode.id && "bg-muted/50 ring-1 ring-primary",
                              )}
                            >
                              <div className="grid gap-2">
                                <div className="flex items-start justify-between gap-3">
                                  <div className="min-w-0">
                                    <div className="text-sm font-medium">{scriptEpisodeOrdinalLabel(episode)}</div>
                                    <div className="mt-1 line-clamp-2 text-base font-semibold">{episode.episodeTitle}</div>
                                  </div>
                                  <StatusBadge status={episode.reviewStatus} />
                                </div>
                                <p className="line-clamp-4 text-sm leading-6 text-muted-foreground">{scriptEpisodePreview(episode.content)}</p>
                              </div>
                              <div className="flex flex-wrap gap-2">
                                {episode.staleState ? <Badge variant="outline">{statusLabel(episode.staleState)}</Badge> : null}
                                {episode.updatedAt ? <Badge variant="outline">{formatDateTime(episode.updatedAt)}</Badge> : null}
                              </div>
                            </button>
                          ))}
                        </div>
                      ) : null}
                    </div>

                    <Dialog
                      open={scriptEpisodeDialogOpen && !!selectedScriptEpisode}
                      onOpenChange={(open) => {
                        setScriptEpisodeDialogOpen(open);
                        if (!open) {
                          setScriptEpisodeEditDraft(null);
                        }
                      }}
                    >
                      <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-4xl">
                        <DialogHeader>
                          <DialogTitle>剧本分集</DialogTitle>
                        </DialogHeader>
                        {selectedScriptEpisode ? (
                          <div className="grid gap-4">
                            <div className="flex flex-wrap items-start justify-between gap-3">
                              <div>
                                <div className="text-sm text-muted-foreground">{scriptEpisodeOrdinalLabel(selectedScriptEpisode)}</div>
                                <div className="mt-1 text-lg font-semibold">{selectedScriptEpisode.episodeTitle}</div>
                              </div>
                              <div className="flex flex-wrap gap-2">
                                <StatusBadge status={selectedScriptEpisode.reviewStatus} />
                                {selectedScriptEpisode.staleState ? <Badge variant="outline">{statusLabel(selectedScriptEpisode.staleState)}</Badge> : null}
                              </div>
                            </div>

                            <div className="grid gap-3 md:grid-cols-2">
                              <div className="grid gap-2 md:col-span-2">
                                <Label>分集标题</Label>
                                <Input
                                  value={scriptEpisodeEditForm.episodeTitle}
                                  onChange={(event) => setScriptEpisodeEditForm({ ...scriptEpisodeEditForm, episodeTitle: event.target.value })}
                                />
                              </div>
                              <div className="grid gap-2">
                                <Label>正文格式</Label>
                                <Select
                                  value={scriptEpisodeEditForm.contentFormat}
                                  onValueChange={(value) => setScriptEpisodeEditForm({ ...scriptEpisodeEditForm, contentFormat: value })}
                                >
                                  <SelectTrigger className="w-full">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectItem value="plain_text">纯文本</SelectItem>
                                    <SelectItem value="markdown">Markdown</SelectItem>
                                  </SelectContent>
                                </Select>
                              </div>
                              <div className="grid gap-2">
                                <Label>审核状态</Label>
                                <Select
                                  value={scriptEpisodeEditForm.reviewStatus}
                                  onValueChange={(value) => setScriptEpisodeEditForm({ ...scriptEpisodeEditForm, reviewStatus: value })}
                                >
                                  <SelectTrigger className="w-full">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectItem value="pending">待审核</SelectItem>
                                    <SelectItem value="approved">已确认</SelectItem>
                                    <SelectItem value="needs_edit">需修改</SelectItem>
                                    <SelectItem value="rejected">已驳回</SelectItem>
                                  </SelectContent>
                                </Select>
                              </div>
                              <div className="grid gap-2 md:col-span-2">
                                <Label>分集正文</Label>
                                <Textarea
                                  className="min-h-[52vh] font-mono text-sm leading-6"
                                  value={scriptEpisodeEditForm.content}
                                  onChange={(event) => setScriptEpisodeEditForm({ ...scriptEpisodeEditForm, content: event.target.value })}
                                />
                              </div>
                            </div>
                            <EpisodeAudioPanel projectId={projectId} episodeId={selectedScriptEpisode.id} />
                          </div>
                        ) : null}
                        <DialogFooter>
                          <Button variant="outline" onClick={() => setScriptEpisodeDialogOpen(false)} type="button">
                            关闭
                          </Button>
                          <Button onClick={handleSaveScriptEpisode} disabled={!selectedScriptEpisode || updateScriptEpisodeMutation.isPending} type="button">
                            <Save className="h-4 w-4" />
                            保存分集
                          </Button>
                        </DialogFooter>
                      </DialogContent>
                    </Dialog>

                  </>
                ) : (
                  <div className="py-16 text-center text-sm text-muted-foreground">请选择剧本</div>
                )}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </Surface>
  );
}

function sourceChapterCount(source: ProjectSource) {
  if (typeof source.chapterCount === "number") {
    return source.chapterCount;
  }
  if (source.chapters?.length) {
    return source.chapters.length;
  }
  const value = source.metadata?.import;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const chapterCount = value.chapterCount;
    if (typeof chapterCount === "number") {
      return chapterCount;
    }
  }
  return 0;
}

function chapterDisplayTitle(chapter: NovelChapterSummary) {
  const title = chapter.chapterTitle || `第 ${chapter.chapterIndex} 集`;
  return chapter.volumeTitle ? `${chapter.volumeTitle} · ${title}` : title;
}

function chapterOrdinalLabel(chapter: NovelChapterSummary) {
  const parts: string[] = [];
  if (typeof chapter.volumeIndex === "number" && chapter.volumeIndex > 0) {
    parts.push(`第 ${chapter.volumeIndex} 卷`);
  }
  if (typeof chapter.sectionIndex === "number" && chapter.sectionIndex > 0) {
    parts.push(`第 ${chapter.sectionIndex} 节`);
  }
  return parts.join(" / ");
}

function scriptEpisodeOrdinalLabel(episode: ScriptEpisode) {
  const parts: string[] = [];
  if (typeof episode.volumeIndex === "number" && episode.volumeIndex > 0) {
    parts.push(`第 ${episode.volumeIndex} 卷`);
  }
  if (typeof episode.sectionIndex === "number" && episode.sectionIndex > 0) {
    parts.push(`第 ${episode.sectionIndex} 节`);
  }
  if (parts.length === 0) {
    parts.push(`第 ${episode.episodeIndex} 集`);
  }
  return parts.join(" / ");
}

function formatContentLength(value: number) {
  if (value >= 10000) {
    return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)} 万字`;
  }
  return `${value} 字`;
}

function chapterPreview(content: string) {
  const trimmed = content.trim();
  if (!trimmed) {
    return "暂无正文";
  }
  const maxLength = 1200;
  if (trimmed.length <= maxLength) {
    return trimmed;
  }
  return `${trimmed.slice(0, maxLength)}...`;
}

function scriptEpisodePreview(content: string) {
  const trimmed = content.replace(/\s+/g, " ").trim();
  if (!trimmed) {
    return "暂无正文";
  }
  const maxLength = 180;
  if (trimmed.length <= maxLength) {
    return trimmed;
  }
  return `${trimmed.slice(0, maxLength)}...`;
}

function workflowRunType(run: WorkflowRun) {
  const value = run.input?.workflowType;
  return typeof value === "string" ? value : "";
}

function SourceAdvancedDetails({
  selectedSource,
  selectedChapter,
  events,
  plans,
  selectedPlan,
  planEditForm,
  onPlanFormChange,
  onSelectPlan,
  onExtractCurrentChapter,
  onCreatePlan,
  onGenerateScript,
  onActivatePlan,
  onSavePlan,
  isExtracting,
  isCreatingPlan,
  isGeneratingScript,
  isActivatingPlan,
  isSavingPlan,
}: {
  selectedSource: ProjectSource;
  selectedChapter: NovelChapterSummary | null;
  events: Array<{ id: string; eventIndex: number; title: string; summary: string; reviewStatus: string }>;
  plans: AdaptationPlan[];
  selectedPlan: AdaptationPlan | null | undefined;
  planEditForm: AdaptationPlanEditForm;
  onPlanFormChange: (form: AdaptationPlanEditForm) => void;
  onSelectPlan: (plan: AdaptationPlan) => void;
  onExtractCurrentChapter: () => void;
  onCreatePlan: () => void;
  onGenerateScript: () => void;
  onActivatePlan: () => void;
  onSavePlan: () => void;
  isExtracting: boolean;
  isCreatingPlan: boolean;
  isGeneratingScript: boolean;
  isActivatingPlan: boolean;
  isSavingPlan: boolean;
}) {
  return (
    <details className="rounded-lg border bg-background p-4">
      <summary className="cursor-pointer text-sm font-medium">生成依据</summary>
      <div className="mt-4 grid gap-4 xl:grid-cols-2">
        <div className="grid content-start gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <div className="text-sm font-medium">小说事件</div>
              <div className="text-xs text-muted-foreground">{selectedChapter ? chapterDisplayTitle(selectedChapter) : selectedSource.title}</div>
            </div>
            <Button size="sm" variant="outline" onClick={onExtractCurrentChapter} disabled={isExtracting || !selectedChapter}>
              提取当前分集
            </Button>
          </div>
          {events.length === 0 ? (
            <div className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">当前分集暂无事件</div>
          ) : (
            <div className="grid max-h-80 gap-2 overflow-y-auto pr-1">
              {events.map((event) => (
                <div key={event.id} className="grid gap-1 rounded-md border p-3 text-sm">
                  <div className="flex items-start justify-between gap-3">
                    <div className="font-medium">{event.eventIndex}. {event.title}</div>
                    <Badge variant={event.reviewStatus === "approved" ? "default" : "secondary"}>{statusLabel(event.reviewStatus)}</Badge>
                  </div>
                  <div className="line-clamp-2 text-muted-foreground">{event.summary}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="grid content-start gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <div className="text-sm font-medium">改编计划</div>
              <div className="text-xs text-muted-foreground">计划数量 {plans.length}</div>
            </div>
            <Button size="sm" onClick={onCreatePlan} disabled={isCreatingPlan}>
              创建改编计划
            </Button>
          </div>
          {plans.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {plans.map((plan) => (
                <Button key={plan.id} size="sm" variant={selectedPlan?.id === plan.id ? "default" : "outline"} onClick={() => onSelectPlan(plan)}>
                  {plan.title}
                </Button>
              ))}
            </div>
          ) : (
            <div className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">暂无改编计划</div>
          )}

          {selectedPlan ? (
            <div className="grid gap-3 rounded-md border p-3">
              <div className="grid gap-2">
                <Label htmlFor="advanced-plan-title">计划名称</Label>
                <Input id="advanced-plan-title" value={planEditForm.title} onChange={(event) => onPlanFormChange({ ...planEditForm, title: event.target.value })} />
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <div className="grid gap-2">
                  <Label>状态</Label>
                  <Select value={planEditForm.status} onValueChange={(value) => onPlanFormChange({ ...planEditForm, status: value })}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="draft">草稿</SelectItem>
                      <SelectItem value="active">启用</SelectItem>
                      <SelectItem value="archived">归档</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-2">
                  <Label>目标格式</Label>
                  <Select value={planEditForm.targetFormat} onValueChange={(value) => onPlanFormChange({ ...planEditForm, targetFormat: value })}>
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="short_video">短视频</SelectItem>
                      <SelectItem value="episode">剧集</SelectItem>
                      <SelectItem value="feature">长片</SelectItem>
                      <SelectItem value="outline">大纲</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="advanced-plan-duration">目标时长（秒）</Label>
                  <Input
                    id="advanced-plan-duration"
                    type="number"
                    min={0}
                    inputMode="numeric"
                    value={planEditForm.targetDurationSeconds}
                    onChange={(event) => onPlanFormChange({ ...planEditForm, targetDurationSeconds: event.target.value })}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="advanced-plan-max-shots">最大镜头数</Label>
                  <Input
                    id="advanced-plan-max-shots"
                    type="number"
                    min={0}
                    inputMode="numeric"
                    value={planEditForm.maxShots}
                    onChange={(event) => onPlanFormChange({ ...planEditForm, maxShots: event.target.value })}
                  />
                </div>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="advanced-plan-content">计划内容</Label>
                <Textarea
                  id="advanced-plan-content"
                  className="min-h-64 font-mono text-sm leading-6"
                  value={planEditForm.content}
                  onChange={(event) => onPlanFormChange({ ...planEditForm, content: event.target.value })}
                />
              </div>
              <div className="flex flex-wrap justify-end gap-2">
                <Button variant="outline" onClick={onGenerateScript} disabled={isGeneratingScript}>
                  <Wand2 className="h-4 w-4" />
                  生成剧本
                </Button>
                <Button variant="outline" onClick={onActivatePlan} disabled={isActivatingPlan || selectedPlan.status === "active"}>
                  <CheckCircle2 className="h-4 w-4" />
                  设为启用
                </Button>
                <Button onClick={onSavePlan} disabled={isSavingPlan}>
                  <Save className="h-4 w-4" />
                  保存计划
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </details>
  );
}

function emptySourceEditForm(): SourceEditForm {
  return {
    title: "",
    sourceType: "novel",
    contentFormat: "plain_text",
    content: "",
    splitChapters: true,
  };
}

function emptyAdaptationPlanEditForm(): AdaptationPlanEditForm {
  return {
    title: "",
    status: "draft",
    targetFormat: "short_video",
    targetDurationSeconds: "",
    maxShots: "",
    content: "",
  };
}

function adaptationPlanToForm(plan: AdaptationPlan): AdaptationPlanEditForm {
  return {
    title: plan.title || "",
    status: plan.status || "draft",
    targetFormat: plan.targetFormat || "short_video",
    targetDurationSeconds: plan.targetDurationSeconds ? String(plan.targetDurationSeconds) : "",
    maxShots: plan.maxShots ? String(plan.maxShots) : "",
    content: plan.content || "",
  };
}

function emptyScriptEpisodeEditForm(): ScriptEpisodeEditForm {
  return {
    episodeTitle: "",
    contentFormat: "markdown",
    content: "",
    reviewStatus: "pending",
  };
}

function scriptEpisodeToForm(episode: ScriptEpisode): ScriptEpisodeEditForm {
  return {
    episodeTitle: episode.episodeTitle || "",
    contentFormat: episode.contentFormat || "markdown",
    content: episode.content || "",
    reviewStatus: episode.reviewStatus || "pending",
  };
}

function positiveIntegerFromText(value: string) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0;
  }
  return Math.floor(parsed);
}

function selectedEventCount(plan: AdaptationPlan) {
  return Array.isArray(plan.selectedEventIds) ? plan.selectedEventIds.length : 0;
}

function sourceImpactEntityLabel(entityType: string) {
  const labels: Record<string, string> = {
    novel_chapters: "分集/章节",
    novel_events: "事件",
    adaptation_plans: "改编计划",
    scripts: "剧本",
  };
  return labels[entityType] ?? entityType;
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
