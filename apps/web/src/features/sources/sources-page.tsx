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
  RefreshCw,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { contentFormatLabel, sourceTypeLabel, statusLabel, targetFormatLabel } from "@/lib/labels";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";
import { useUiStore } from "@/lib/stores/ui-store";
import type { AdaptationPlan, JsonRecord, NovelChapterSummary, ProjectSource, Script, ScriptScene, ScriptVersion } from "@/lib/types";

type ImportSourceType = "novel" | "script" | "brief";
type SourcesTab = "sources" | "events" | "plans" | "scripts";

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

type ScriptEditForm = {
  title: string;
  status: string;
  contentFormat: string;
  content: string;
};

type ScriptSceneEditForm = {
  title: string;
  summary: string;
  location: string;
  timeOfDay: string;
  atmosphere: string;
  characters: string;
  scenes: string;
  props: string;
  action: string;
  dialogue: string;
  visualGoal: string;
  emotionalTone: string;
  conflict: string;
  outcome: string;
  content: string;
};

export function SourcesPage({
  projectId,
  initialTab = "sources",
}: {
  projectId: string;
  initialTab?: SourcesTab;
  initialSceneId?: string;
}) {
  const [activeTab, setActiveTab] = useState<SourcesTab>(initialTab);
  const [selectedSourceId, setSelectedSourceId] = useState<string | null>(null);
  const [selectedChapterId, setSelectedChapterId] = useState<string | null>(null);
  const [selectedChapterIds, setSelectedChapterIds] = useState<string[]>([]);
  const [selectedPlanId, setSelectedPlanId] = useState<string | null>(null);
  const [selectedScriptId, setSelectedScriptId] = useState<string | null>(null);
  const [selectedScriptVersionId, setSelectedScriptVersionId] = useState<string | null>(null);
  const [selectedScriptSceneId, setSelectedScriptSceneId] = useState<string | null>(null);
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
  const [scriptEditDraft, setScriptEditDraft] = useState<{ key: string; form: ScriptEditForm } | null>(null);
  const [scriptSceneEditDraft, setScriptSceneEditDraft] = useState<{ sceneId: string; form: ScriptSceneEditForm } | null>(null);
  const invalidate = useInvalidateKeys();
  const { open: openAgent, setContext } = useAgentDrawerStore();
  const setActivityOpen = useUiStore((state) => state.setActivityOpen);

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
    () => scripts.find((script) => script.id === selectedScriptId) ?? scripts[0] ?? null,
    [scripts, selectedScriptId],
  );
  const effectiveScriptId = selectedScriptSummary?.id ?? "";
  const { data: selectedScriptDetail } = useApiQuery({
    key: qk.script(projectId, effectiveScriptId),
    queryFn: (session) => studioApi.getScript(session, projectId, effectiveScriptId),
    enabled: !!effectiveScriptId,
  });
  const selectedScript = selectedScriptDetail ?? selectedScriptSummary;
  const { data: scriptVersions = [], isLoading: scriptVersionsLoading } = useApiQuery({
    key: qk.scriptVersions(projectId, effectiveScriptId),
    queryFn: (session) => studioApi.listScriptVersions(session, projectId, effectiveScriptId).then((response) => response.items || []),
    enabled: !!effectiveScriptId,
  });
  const selectedScriptVersion = useMemo(
    () =>
      scriptVersions.find((version) => version.id === selectedScriptVersionId) ??
      scriptVersions.find((version) => version.id === selectedScript?.currentVersionId) ??
      selectedScript?.currentVersion ??
      scriptVersions[0] ??
      null,
    [scriptVersions, selectedScript?.currentVersion, selectedScript?.currentVersionId, selectedScriptVersionId],
  );
  const effectiveScriptVersionId = selectedScriptVersion?.id ?? "";
  const { data: scriptScenes = [], isLoading: scriptScenesLoading } = useApiQuery({
    key: qk.scriptScenes(projectId, effectiveScriptId, effectiveScriptVersionId),
    queryFn: (session) =>
      studioApi
        .listScriptScenes(session, projectId, effectiveScriptId, effectiveScriptVersionId ? { scriptVersionId: effectiveScriptVersionId } : undefined)
        .then((response) => response.items || []),
    enabled: !!effectiveScriptId && !!effectiveScriptVersionId,
  });
  const selectedScriptScene = useMemo(
    () => scriptScenes.find((scene) => scene.id === selectedScriptSceneId) ?? scriptScenes[0] ?? null,
    [scriptScenes, selectedScriptSceneId],
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

  const scriptEditKey = selectedScript ? `${selectedScript.id}:${selectedScriptVersion?.id ?? "draft"}` : "";
  const scriptEditForm = useMemo(() => {
    if (!selectedScript) {
      return emptyScriptEditForm();
    }
    if (scriptEditDraft?.key === scriptEditKey) {
      return scriptEditDraft.form;
    }
    return scriptToForm(selectedScript, selectedScriptVersion);
  }, [scriptEditDraft, scriptEditKey, selectedScript, selectedScriptVersion]);

  const setScriptEditForm = (form: ScriptEditForm) => {
    if (!selectedScript) {
      setScriptEditDraft(null);
      return;
    }
    setScriptEditDraft({ key: scriptEditKey, form });
  };

  const scriptSceneEditForm = useMemo(() => {
    if (!selectedScriptScene) {
      return emptyScriptSceneEditForm();
    }
    if (scriptSceneEditDraft?.sceneId === selectedScriptScene.id) {
      return scriptSceneEditDraft.form;
    }
    return scriptSceneToForm(selectedScriptScene);
  }, [scriptSceneEditDraft, selectedScriptScene]);

  const setScriptSceneEditForm = (form: ScriptSceneEditForm) => {
    if (!selectedScriptScene) {
      setScriptSceneEditDraft(null);
      return;
    }
    setScriptSceneEditDraft({ sceneId: selectedScriptScene.id, form });
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
      setActiveTab("events");
      invalidate([
        qk.sources(projectId),
        qk.sourceChapters(projectId, payload.sourceId),
        ...payload.chapterIds.map((chapterId) => qk.sourceEvents(projectId, payload.sourceId, chapterId)),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
      if (run.id) {
        toast.message(`工作流已创建：${run.id.slice(0, 8)}`);
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
    mutationFn: (session, data: { sourceId: string; title: string; targetFormat: string }) =>
      studioApi.generateAdaptationPlan(session, projectId, data.sourceId, {
        title: data.title,
        targetFormat: data.targetFormat,
      }),
    onSuccess: (plan) => {
      toast.success("改编计划已创建");
      setSelectedPlanId(plan.id);
      setPlanEditDraft({ planId: plan.id, form: adaptationPlanToForm(plan) });
      invalidate([qk.adaptationPlans(projectId)]);
      setActiveTab("plans");
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

  const saveScriptMutation = useApiMutation({
    mutationFn: async (
      session,
      data: {
        script: Script;
        version: ScriptVersion | null;
        title: string;
        status: string;
        content: string;
        contentFormat: string;
      },
    ) => {
      const updated = await studioApi.updateScript(session, projectId, data.script.id, {
        title: data.title,
        status: data.status,
      });
      const currentContent = data.version?.content ?? "";
      const currentFormat = data.version?.contentFormat ?? "markdown";
      if (data.content !== currentContent || data.contentFormat !== currentFormat) {
        const version = await studioApi.createScriptVersion(session, projectId, data.script.id, {
          content: data.content,
          contentFormat: data.contentFormat,
          sourceType: "manual_edit",
          activate: true,
        });
        return { script: updated, version };
      }
      return { script: updated, version: null };
    },
    onSuccess: ({ script, version }, data) => {
      toast.success(version ? "剧本已保存为新版本" : "剧本信息已保存");
      setSelectedScriptId(script.id);
      const nextVersion = version ?? data.version;
      if (nextVersion) {
        setSelectedScriptVersionId(nextVersion.id);
        setScriptEditDraft({
          key: `${script.id}:${nextVersion.id}`,
          form: {
            title: data.title,
            status: data.status,
            contentFormat: data.contentFormat,
            content: data.content,
          },
        });
      }
      invalidate([
        qk.scripts(projectId),
        qk.script(projectId, script.id),
        qk.scriptVersions(projectId, script.id),
        qk.scriptScenes(projectId, script.id),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("保存失败：" + error.message);
    },
  });

  const activateScriptVersionMutation = useApiMutation({
    mutationFn: (session, data: { scriptId: string; versionId: string }) =>
      studioApi.activateScriptVersion(session, projectId, data.scriptId, data.versionId),
    onSuccess: (script, data) => {
      toast.success("剧本版本已激活");
      setSelectedScriptId(script.id);
      const currentVersion = script.currentVersion ?? null;
      setSelectedScriptVersionId(currentVersion?.id ?? data.versionId);
      if (currentVersion) {
        setScriptEditDraft({
          key: `${script.id}:${currentVersion.id}`,
          form: scriptToForm(script, currentVersion),
        });
      }
      invalidate([
        qk.scripts(projectId),
        qk.script(projectId, script.id),
        qk.scriptVersions(projectId, script.id),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("激活失败：" + error.message);
    },
  });

  const deleteScriptVersionMutation = useApiMutation({
    mutationFn: (session, data: { scriptId: string; versionId: string }) =>
      studioApi.deleteScriptVersion(session, projectId, data.scriptId, data.versionId),
    onSuccess: (_result, data) => {
      toast.success("剧本版本已归档");
      if (selectedScriptVersionId === data.versionId) {
        setSelectedScriptVersionId(null);
      }
      setSelectedScriptSceneId(null);
      setScriptSceneEditDraft(null);
      invalidate([
        qk.scripts(projectId),
        qk.script(projectId, data.scriptId),
        qk.scriptVersions(projectId, data.scriptId),
        qk.scriptScenes(projectId, data.scriptId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("归档失败：" + error.message);
    },
  });

  const parseScriptScenesMutation = useApiMutation({
    mutationFn: (session, data: { scriptId: string; versionId: string; force?: boolean }) =>
      studioApi.parseScriptScenes(session, projectId, data.scriptId, data.versionId, { force: data.force === true }),
    onSuccess: (result) => {
      toast.success(`已解析 ${result.sceneCount} 个场景`);
      setSelectedScriptSceneId(result.scenes[0]?.id ?? null);
      setScriptSceneEditDraft(null);
      invalidate([
        qk.scriptScenes(projectId, result.scriptId, result.versionId),
        qk.scripts(projectId),
        qk.script(projectId, result.scriptId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("解析失败：" + error.message);
    },
  });

  const updateScriptSceneMutation = useApiMutation({
    mutationFn: (session, data: { sceneId: string; body: JsonRecord }) =>
      studioApi.updateScriptScene(session, projectId, data.sceneId, data.body),
    onSuccess: (scene) => {
      toast.success("场景已保存");
      setSelectedScriptSceneId(scene.id);
      setScriptSceneEditDraft({ sceneId: scene.id, form: scriptSceneToForm(scene) });
      invalidate([
        qk.scriptScenes(projectId, scene.scriptId, scene.scriptVersionId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("保存场景失败：" + error.message);
    },
  });

  const deleteScriptSceneMutation = useApiMutation({
    mutationFn: (session, scene: ScriptScene) => studioApi.deleteScriptScene(session, projectId, scene.id).then((result) => ({ result, scene })),
    onSuccess: ({ scene }) => {
      toast.success("场景已归档");
      if (selectedScriptSceneId === scene.id) {
        setSelectedScriptSceneId(null);
      }
      setScriptSceneEditDraft(null);
      invalidate([
        qk.scriptScenes(projectId, scene.scriptId, scene.scriptVersionId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("归档场景失败：" + error.message);
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

  const handleSelectScript = (script: Script) => {
    setSelectedScriptId(script.id);
    setSelectedScriptVersionId(null);
    setSelectedScriptSceneId(null);
    setScriptSceneEditDraft(null);
  };

  const handleSelectScriptVersion = (version: ScriptVersion) => {
    setSelectedScriptVersionId(version.id);
    setSelectedScriptSceneId(null);
    setScriptSceneEditDraft(null);
    if (selectedScript) {
      setScriptEditDraft({
        key: `${selectedScript.id}:${version.id}`,
        form: {
          title: selectedScript.title ?? "",
          status: selectedScript.status ?? "draft",
          contentFormat: version.contentFormat || "markdown",
          content: version.content || "",
        },
      });
    }
  };

  const handleSaveScript = () => {
    if (!selectedScript) {
      toast.error("请先选择剧本");
      return;
    }
    const title = scriptEditForm.title.trim();
    if (!title) {
      toast.error("请填写剧本名称");
      return;
    }
    if (!scriptEditForm.content.trim()) {
      toast.error("请填写剧本内容");
      return;
    }
    saveScriptMutation.mutate({
      script: selectedScript,
      version: selectedScriptVersion,
      title,
      status: scriptEditForm.status || "draft",
      content: scriptEditForm.content,
      contentFormat: scriptEditForm.contentFormat || "markdown",
    });
  };

  const handleActivateScriptVersion = (version: ScriptVersion) => {
    if (!selectedScript) {
      toast.error("请先选择剧本");
      return;
    }
    activateScriptVersionMutation.mutate({ scriptId: selectedScript.id, versionId: version.id });
  };

  const handleDeleteScriptVersion = (version: ScriptVersion) => {
    if (!selectedScript) {
      toast.error("请先选择剧本");
      return;
    }
    if (version.id === selectedScript.currentVersionId) {
      toast.error("当前版本不能归档");
      return;
    }
    deleteScriptVersionMutation.mutate({ scriptId: selectedScript.id, versionId: version.id });
  };

  const handleParseScriptScenes = (force: boolean) => {
    if (!selectedScript || !selectedScriptVersion) {
      toast.error("请先选择剧本版本");
      return;
    }
    parseScriptScenesMutation.mutate({ scriptId: selectedScript.id, versionId: selectedScriptVersion.id, force });
  };

  const handleSelectScriptScene = (scene: ScriptScene) => {
    setSelectedScriptSceneId(scene.id);
    setScriptSceneEditDraft({ sceneId: scene.id, form: scriptSceneToForm(scene) });
  };

  const handleSaveScriptScene = () => {
    if (!selectedScriptScene) {
      toast.error("请先选择场景");
      return;
    }
    const title = scriptSceneEditForm.title.trim();
    if (!title) {
      toast.error("请填写场景标题");
      return;
    }
    updateScriptSceneMutation.mutate({
      sceneId: selectedScriptScene.id,
      body: {
        title,
        summary: scriptSceneEditForm.summary,
        location: scriptSceneEditForm.location,
        timeOfDay: scriptSceneEditForm.timeOfDay,
        atmosphere: scriptSceneEditForm.atmosphere,
        characters: listFromTextarea(scriptSceneEditForm.characters),
        scenes: listFromTextarea(scriptSceneEditForm.scenes),
        props: listFromTextarea(scriptSceneEditForm.props),
        action: scriptSceneEditForm.action,
        dialogue: scriptSceneEditForm.dialogue,
        visualGoal: scriptSceneEditForm.visualGoal,
        emotionalTone: scriptSceneEditForm.emotionalTone,
        conflict: scriptSceneEditForm.conflict,
        outcome: scriptSceneEditForm.outcome,
        content: scriptSceneEditForm.content,
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
        title={activeTab === "scripts" ? "剧本" : "内容"}
        description={activeTab === "scripts" ? "查看、编辑、激活剧本版本" : "添加小说原文、剧本或创意文案并管理分集"}
      />

      <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as SourcesTab)} className="p-4">
        <TabsList className="grid w-full grid-cols-4">
          <TabsTrigger value="sources">
            原文管理
            <Badge variant="secondary" className="ml-2">{sources.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="events">
            小说事件
            <Badge variant="secondary" className="ml-2">{events.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="plans">
            改编计划
            <Badge variant="secondary" className="ml-2">{plans.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="scripts">
            剧本列表
            <Badge variant="secondary" className="ml-2">{scripts.length}</Badge>
          </TabsTrigger>
        </TabsList>

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
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>添加内容</DialogTitle>
              </DialogHeader>
              <div className="grid gap-4">
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
                      className="min-h-48"
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
                            {chapter.errorMessage ? <div className="text-xs text-destructive">{chapter.errorMessage}</div> : null}
                          </div>
                        ))}
                      </div>

                      <div className="grid content-start gap-3 rounded-lg border p-4">
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
                  title: "新改编计划",
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
          <div className="flex justify-end gap-2">
            <Button size="sm" onClick={handleUseAgent}>
              <Wand2 className="h-4 w-4 mr-2" />
              使用助手改写
            </Button>
          </div>

          {scriptsLoading && <Skeleton className="h-32" />}

          {!scriptsLoading && scripts.length === 0 && (
            <div className="rounded-lg border border-dashed p-12 text-center">
              <FileText className="mx-auto h-12 w-12 text-muted-foreground opacity-50" />
              <p className="mt-4 text-sm text-muted-foreground">暂无剧本</p>
              <p className="mt-1 text-xs text-muted-foreground">从改编计划生成或使用助手创建</p>
            </div>
          )}

          {scripts.length > 0 && (
            <div className="grid gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
              <div className="grid content-start gap-3">
                {scripts.map((script) => (
                  <button
                    key={script.id}
                    type="button"
                    onClick={() => handleSelectScript(script)}
                    className={cn(
                      "flex items-start gap-3 rounded-lg border p-4 text-left transition hover:bg-muted/50",
                      selectedScript?.id === script.id && "bg-muted/50 ring-2 ring-primary",
                    )}
                  >
                    <FileText className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">{script.title}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        当前版本 {script.currentVersionId?.slice(0, 8) || "未设置"}
                      </div>
                      <div className="mt-3 flex flex-wrap gap-2">
                        <StatusBadge status={script.status} />
                        {script.updatedAt ? <Badge variant="outline">{formatDateTime(script.updatedAt)}</Badge> : null}
                      </div>
                    </div>
                  </button>
                ))}
              </div>

              <div className="grid content-start gap-4 rounded-lg border p-4">
                {selectedScript ? (
                  <>
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <div className="text-sm text-muted-foreground">剧本详情</div>
                        <h3 className="mt-1 text-lg font-semibold">{selectedScript.title}</h3>
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <StatusBadge status={selectedScript.status} />
                        {selectedScriptVersion ? <Badge variant="outline">版本 {selectedScriptVersion.version}</Badge> : null}
                      </div>
                    </div>

                    <div className="grid gap-3 md:grid-cols-2">
                      <div className="grid gap-2 md:col-span-2">
                        <Label htmlFor="script-title">剧本名称</Label>
                        <Input
                          id="script-title"
                          value={scriptEditForm.title}
                          onChange={(event) => setScriptEditForm({ ...scriptEditForm, title: event.target.value })}
                        />
                      </div>
                      <div className="grid gap-2">
                        <Label>状态</Label>
                        <Select
                          value={scriptEditForm.status}
                          onValueChange={(value) => setScriptEditForm({ ...scriptEditForm, status: value })}
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
                        <Label>正文格式</Label>
                        <Select
                          value={scriptEditForm.contentFormat}
                          onValueChange={(value) => setScriptEditForm({ ...scriptEditForm, contentFormat: value })}
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

                    <div className="grid gap-2">
                      <Label htmlFor="script-content">剧本正文</Label>
                      <Textarea
                        id="script-content"
                        className="min-h-[520px] font-mono text-sm leading-6"
                        value={scriptEditForm.content}
                        onChange={(event) => setScriptEditForm({ ...scriptEditForm, content: event.target.value })}
                      />
                    </div>

                    <div className="flex flex-wrap justify-end gap-2">
                      <Button variant="outline" onClick={handleUseAgent}>
                        <Wand2 className="h-4 w-4" />
                        使用助手改写
                      </Button>
                      <Button onClick={handleSaveScript} disabled={saveScriptMutation.isPending}>
                        <Save className="h-4 w-4" />
                        保存剧本
                      </Button>
                    </div>

                    <div className="grid gap-2 border-t pt-4">
                      <div className="text-sm font-medium">版本记录</div>
                      {scriptVersionsLoading ? <Skeleton className="h-24" /> : null}
                      {!scriptVersionsLoading && scriptVersions.length === 0 ? (
                        <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">暂无版本</div>
                      ) : null}
                      <div className="grid gap-2">
                        {scriptVersions.map((version) => {
                          const active = version.id === selectedScript.currentVersionId;
                          const selected = version.id === selectedScriptVersion?.id;
                          return (
                            <div
                              key={version.id}
                              className={cn(
                                "flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3",
                                selected && "bg-muted/50 ring-1 ring-primary",
                              )}
                            >
                              <button
                                type="button"
                                onClick={() => handleSelectScriptVersion(version)}
                                className="min-w-0 flex-1 text-left"
                              >
                                <div className="font-medium">版本 {version.version}</div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                  {contentFormatLabel(version.contentFormat)} · {version.createdAt ? formatDateTime(version.createdAt) : "未记录时间"}
                                </div>
                              </button>
                              <div className="flex items-center gap-2">
                                {active ? <Badge>当前</Badge> : null}
                                <Button
                                  size="sm"
                                  variant="outline"
                                  onClick={() => handleSelectScriptVersion(version)}
                                >
                                  查看
                                </Button>
                                <Button
                                  size="sm"
                                  variant="outline"
                                  onClick={() => handleActivateScriptVersion(version)}
                                  disabled={active || activateScriptVersionMutation.isPending}
                                >
                                  设为当前
                                </Button>
                                <Button
                                  size="sm"
                                  variant="destructive"
                                  onClick={() => handleDeleteScriptVersion(version)}
                                  disabled={active || deleteScriptVersionMutation.isPending}
                                >
                                  归档
                                </Button>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    </div>

                    <div className="grid gap-3 border-t pt-4">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div>
                          <div className="text-sm font-medium">场景结构</div>
                          <div className="text-xs text-muted-foreground">
                            {selectedScriptVersion ? `版本 ${selectedScriptVersion.version}` : "未选择版本"}
                          </div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleParseScriptScenes(false)}
                            disabled={!selectedScriptVersion || parseScriptScenesMutation.isPending}
                          >
                            <ListChecks className="h-4 w-4" />
                            解析场景
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleParseScriptScenes(true)}
                            disabled={!selectedScriptVersion || parseScriptScenesMutation.isPending}
                          >
                            <RefreshCw className="h-4 w-4" />
                            重新解析
                          </Button>
                        </div>
                      </div>

                      {scriptScenesLoading ? <Skeleton className="h-24" /> : null}
                      {!scriptScenesLoading && scriptScenes.length === 0 ? (
                        <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">暂无场景</div>
                      ) : null}

                      {scriptScenes.length > 0 ? (
                        <div className="grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
                          <div className="grid content-start gap-2">
                            {scriptScenes.map((scene) => (
                              <button
                                key={scene.id}
                                type="button"
                                onClick={() => handleSelectScriptScene(scene)}
                                className={cn(
                                  "rounded-lg border p-3 text-left transition hover:bg-muted/50",
                                  selectedScriptScene?.id === scene.id && "bg-muted/50 ring-1 ring-primary",
                                )}
                              >
                                <div className="font-medium">场景 {scene.sceneNo}</div>
                                <div className="mt-1 line-clamp-2 text-sm">{scene.title}</div>
                                <div className="mt-2 flex flex-wrap gap-2">
                                  <StatusBadge status={scene.reviewStatus} />
                                  {scene.staleState ? <Badge variant="outline">{statusLabel(scene.staleState)}</Badge> : null}
                                </div>
                              </button>
                            ))}
                          </div>

                          {selectedScriptScene ? (
                            <div className="grid gap-3 rounded-lg border p-4">
                              <div className="flex flex-wrap items-start justify-between gap-3">
                                <div>
                                  <div className="text-sm text-muted-foreground">场景 {selectedScriptScene.sceneNo}</div>
                                  <div className="font-medium">{selectedScriptScene.title}</div>
                                </div>
                                <Button
                                  size="sm"
                                  variant="destructive"
                                  onClick={() => deleteScriptSceneMutation.mutate(selectedScriptScene)}
                                  disabled={deleteScriptSceneMutation.isPending}
                                >
                                  <Trash2 className="h-4 w-4" />
                                  归档
                                </Button>
                              </div>

                              <div className="grid gap-3 md:grid-cols-2">
                                <div className="grid gap-2 md:col-span-2">
                                  <Label>标题</Label>
                                  <Input
                                    value={scriptSceneEditForm.title}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, title: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2 md:col-span-2">
                                  <Label>摘要</Label>
                                  <Textarea
                                    className="min-h-20"
                                    value={scriptSceneEditForm.summary}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, summary: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label>地点</Label>
                                  <Input
                                    value={scriptSceneEditForm.location}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, location: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label>时间</Label>
                                  <Input
                                    value={scriptSceneEditForm.timeOfDay}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, timeOfDay: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label>人物</Label>
                                  <Textarea
                                    className="min-h-20"
                                    value={scriptSceneEditForm.characters}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, characters: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label>场景资产</Label>
                                  <Textarea
                                    className="min-h-20"
                                    value={scriptSceneEditForm.scenes}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, scenes: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label>道具</Label>
                                  <Textarea
                                    className="min-h-20"
                                    value={scriptSceneEditForm.props}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, props: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2">
                                  <Label>氛围</Label>
                                  <Input
                                    value={scriptSceneEditForm.atmosphere}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, atmosphere: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2 md:col-span-2">
                                  <Label>动作</Label>
                                  <Textarea
                                    className="min-h-24"
                                    value={scriptSceneEditForm.action}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, action: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2 md:col-span-2">
                                  <Label>对白</Label>
                                  <Textarea
                                    className="min-h-24"
                                    value={scriptSceneEditForm.dialogue}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, dialogue: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2 md:col-span-2">
                                  <Label>视觉目标</Label>
                                  <Textarea
                                    className="min-h-20"
                                    value={scriptSceneEditForm.visualGoal}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, visualGoal: event.target.value })}
                                  />
                                </div>
                                <div className="grid gap-2 md:col-span-2">
                                  <Label>正文</Label>
                                  <Textarea
                                    className="min-h-32 font-mono text-sm"
                                    value={scriptSceneEditForm.content}
                                    onChange={(event) => setScriptSceneEditForm({ ...scriptSceneEditForm, content: event.target.value })}
                                  />
                                </div>
                              </div>
                              <div className="flex justify-end">
                                <Button onClick={handleSaveScriptScene} disabled={updateScriptSceneMutation.isPending}>
                                  <Save className="h-4 w-4" />
                                  保存场景
                                </Button>
                              </div>
                            </div>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                  </>
                ) : (
                  <div className="py-16 text-center text-sm text-muted-foreground">请选择剧本</div>
                )}
              </div>
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

function emptyScriptEditForm(): ScriptEditForm {
  return {
    title: "",
    status: "draft",
    contentFormat: "markdown",
    content: "",
  };
}

function scriptToForm(script: Script, version: ScriptVersion | null): ScriptEditForm {
  return {
    title: script.title || "",
    status: script.status || "draft",
    contentFormat: version?.contentFormat || script.currentVersion?.contentFormat || "markdown",
    content: version?.content || script.currentVersion?.content || "",
  };
}

function emptyScriptSceneEditForm(): ScriptSceneEditForm {
  return {
    title: "",
    summary: "",
    location: "",
    timeOfDay: "",
    atmosphere: "",
    characters: "",
    scenes: "",
    props: "",
    action: "",
    dialogue: "",
    visualGoal: "",
    emotionalTone: "",
    conflict: "",
    outcome: "",
    content: "",
  };
}

function scriptSceneToForm(scene: ScriptScene): ScriptSceneEditForm {
  return {
    title: scene.title || "",
    summary: scene.summary || "",
    location: scene.location || "",
    timeOfDay: scene.timeOfDay || "",
    atmosphere: scene.atmosphere || "",
    characters: listToTextarea(scene.characters),
    scenes: listToTextarea(scene.scenes),
    props: listToTextarea(scene.props),
    action: scene.action || "",
    dialogue: scene.dialogue || "",
    visualGoal: scene.visualGoal || "",
    emotionalTone: scene.emotionalTone || "",
    conflict: scene.conflict || "",
    outcome: scene.outcome || "",
    content: scene.content || "",
  };
}

function listToTextarea(value: unknown) {
  if (!Array.isArray(value)) {
    return "";
  }
  return value.filter((item): item is string => typeof item === "string" && item.trim() !== "").join("\n");
}

function listFromTextarea(value: string) {
  return value
    .split(/\r?\n|[,，]/)
    .map((item) => item.trim())
    .filter(Boolean);
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
