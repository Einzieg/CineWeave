"use client";

import { useState } from "react";
import { useApiQuery, useApiMutation, useInvalidateKeys } from "@/lib/query/use-api";
import { qk } from "@/lib/query/keys";
import { studioApi } from "@/lib/api-client";
import { Surface, SectionTitle } from "@/components/layout/app-shell";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import {
  FileText,
  Upload,
  Wand2,
  Clock,
  AlertCircle
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { useAgentDrawerStore } from "@/lib/stores/agent-drawer-store";

type ImportSourceType = "novel" | "script";

export function SourcesPage({
  projectId,
}: {
  projectId: string;
  initialSceneId?: string;
}) {
  const [activeTab, setActiveTab] = useState("sources");
  const [selectedSourceId, setSelectedSourceId] = useState<string | null>(null);
  const [selectedScriptId, setSelectedScriptId] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importTitle, setImportTitle] = useState("");
  const [importSourceType, setImportSourceType] = useState<ImportSourceType>("novel");
  const [splitChapters, setSplitChapters] = useState(true);
  const [createScript, setCreateScript] = useState(false);
  const invalidate = useInvalidateKeys();
  const { open: openAgent, setContext } = useAgentDrawerStore();

  // 获取原文列表
  const { data: sources = [], isLoading: sourcesLoading } = useApiQuery({
    key: qk.sources(projectId),
    queryFn: (session) => studioApi.listSources(session, projectId).then(r => r.items || []),
  });

  // 获取剧本列表
  const { data: scripts = [], isLoading: scriptsLoading } = useApiQuery({
    key: qk.scripts(projectId),
    queryFn: (session) => studioApi.listScripts(session, projectId).then(r => r.items || []),
  });

  // 获取选中原文的事件
  const { data: eventsData } = useApiQuery({
    key: qk.sourceEvents(projectId, selectedSourceId || ""),
    queryFn: (session) =>
      studioApi.listSourceNovelEvents(session, projectId, selectedSourceId!),
    enabled: !!selectedSourceId,
  });

  const events = eventsData?.items || [];

  // 获取改编计划
  const { data: plans = [] } = useApiQuery({
    key: qk.adaptationPlans(projectId),
    queryFn: (session) =>
      studioApi.listAdaptationPlans(session, projectId).then(r => r.items || []),
  });

  // 提取事件
  const extractEventsMutation = useApiMutation({
    mutationFn: (session, sourceId: string) =>
      studioApi.extractNovelEvents(session, projectId, sourceId, {}),
    onSuccess: () => {
      toast.success("事件提取已启动");
      invalidate([qk.sources(projectId), qk.sourceEvents(projectId, selectedSourceId || "")]);
    },
    onError: (error) => {
      toast.error("提取失败：" + error.message);
    },
  });

  // 生成改编计划
  const createPlanMutation = useApiMutation({
    mutationFn: (session, data: { sourceId: string; title: string; targetFormat: string }) =>
      studioApi.generateAdaptationPlan(session, projectId, data.sourceId, {
        title: data.title,
        targetFormat: data.targetFormat,
      }),
    onSuccess: () => {
      toast.success("改编计划已创建");
      invalidate([qk.adaptationPlans(projectId)]);
      setActiveTab("plans");
    },
    onError: (error) => {
      toast.error("创建失败：" + error.message);
    },
  });

  // 从计划生成剧本
  const generateScriptMutation = useApiMutation({
    mutationFn: (session, planId: string) =>
      studioApi.generateScriptFromAdaptationPlan(session, projectId, planId, {}),
    onSuccess: () => {
      toast.success("剧本生成已启动");
      invalidate([qk.scripts(projectId)]);
      setActiveTab("scripts");
    },
    onError: (error) => {
      toast.error("生成失败：" + error.message);
    },
  });

  const importFileMutation = useApiMutation({
    mutationFn: (session) => {
      if (!importFile) {
        throw new Error("请选择文件");
      }
      const form = new FormData();
      form.append("file", importFile);
      form.append("sourceType", importSourceType);
      form.append("title", importTitle.trim() || importFile.name.replace(/\.[^.]+$/, ""));
      form.append("splitChapters", splitChapters ? "true" : "false");
      form.append("createScript", createScript ? "true" : "false");
      return studioApi.importSourceFile(session, projectId, form);
    },
    onSuccess: (response) => {
      toast.success("文件已导入");
      setSelectedSourceId(response.source.id);
      if (response.script?.id) {
        setSelectedScriptId(response.script.id);
        setActiveTab("scripts");
      } else {
        setActiveTab("sources");
      }
      setImportOpen(false);
      setImportFile(null);
      setImportTitle("");
      invalidate([
        qk.sources(projectId),
        qk.scripts(projectId),
        qk.productionStatus(projectId),
        qk.project(projectId),
      ]);
    },
    onError: (error) => {
      toast.error("导入失败：" + error.message);
    },
  });

  const updateImportSourceType = (value: string) => {
    const next = value === "script" ? "script" : "novel";
    setImportSourceType(next);
    setSplitChapters(next === "novel");
    setCreateScript(next === "script");
  };

  const handleUseAgent = () => {
    setContext({
      projectId,
      sourceId: selectedSourceId,
      scriptId: selectedScriptId,
    });
    openAgent();
  };

  return (
    <Surface>
      <SectionTitle
        title="原文与剧本"
        description="管理小说原文、提取事件、创建改编计划、生成剧本"
      />

      <Tabs value={activeTab} onValueChange={setActiveTab} className="p-4">
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
              导入文件
            </Button>
            <Button size="sm" onClick={handleUseAgent}>
              <Wand2 className="h-4 w-4 mr-2" />
              使用助手
            </Button>
          </div>

          <Dialog open={importOpen} onOpenChange={setImportOpen}>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>导入文件</DialogTitle>
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
                    </SelectContent>
                  </Select>
                </div>
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox checked={splitChapters} onCheckedChange={(checked) => setSplitChapters(checked === true)} />
                  拆分章节
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox checked={createScript} onCheckedChange={(checked) => setCreateScript(checked === true)} />
                  创建剧本
                </label>
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setImportOpen(false)} type="button">
                  取消
                </Button>
                <Button disabled={!importFile || importFileMutation.isPending} onClick={() => importFileMutation.mutate()} type="button">
                  导入
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

          <div className="grid gap-3">
            {sources.map((source) => (
              <button
                key={source.id}
                onClick={() => setSelectedSourceId(source.id)}
                className={cn(
                  "flex items-start gap-4 rounded-lg border p-4 text-left transition hover:bg-muted/50",
                  selectedSourceId === source.id && "bg-muted/50 ring-2 ring-primary"
                )}
              >
                <FileText className="h-5 w-5 shrink-0 text-muted-foreground" />
                <div className="flex-1 min-w-0">
                  <div className="font-medium">{source.title}</div>
                  <div className="text-sm text-muted-foreground">
                    类型: {source.sourceType} · 格式: {source.contentFormat || "未知"}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge>{source.status}</Badge>
                  {source.status === "imported" && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={(e) => {
                        e.stopPropagation();
                        extractEventsMutation.mutate(source.id);
                      }}
                      disabled={extractEventsMutation.isPending}
                    >
                      提取事件
                    </Button>
                  )}
                </div>
              </button>
            ))}
          </div>
        </TabsContent>

        {/* 小说事件 */}
        <TabsContent value="events" className="space-y-4">
          {!selectedSourceId && (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <AlertCircle className="mx-auto h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-sm text-muted-foreground">请先选择一个原文</p>
            </div>
          )}

          {selectedSourceId && events.length === 0 && (
            <div className="rounded-lg border border-dashed p-8 text-center">
              <Clock className="mx-auto h-8 w-8 text-muted-foreground" />
              <p className="mt-2 text-sm text-muted-foreground">暂无事件，请先提取</p>
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
                  {event.reviewStatus}
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
                if (!selectedSourceId) {
                  toast.error("请先选择原文");
                  return;
                }
                createPlanMutation.mutate({
                  sourceId: selectedSourceId,
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

          <div className="grid gap-3">
            {plans.map((plan) => (
              <div
                key={plan.id}
                className="flex items-start gap-4 rounded-lg border p-4"
              >
                <div className="flex-1">
                  <div className="font-medium">{plan.title}</div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    格式: {plan.targetFormat} · 状态: {plan.status}
                  </div>
                </div>
                <Button
                  size="sm"
                  onClick={() => generateScriptMutation.mutate(plan.id)}
                  disabled={generateScriptMutation.isPending}
                >
                  生成剧本
                </Button>
              </div>
            ))}
          </div>
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

          <div className="grid gap-3">
            {scripts.map((script) => (
              <button
                key={script.id}
                onClick={() => setSelectedScriptId(script.id)}
                className={cn(
                  "flex items-start gap-4 rounded-lg border p-4 text-left transition hover:bg-muted/50",
                  selectedScriptId === script.id && "bg-muted/50 ring-2 ring-primary"
                )}
              >
                <FileText className="h-5 w-5 shrink-0 text-muted-foreground" />
                <div className="flex-1">
                  <div className="font-medium">{script.title}</div>
                  <div className="text-sm text-muted-foreground">
                    版本: {script.currentVersionId?.slice(0, 8) || "未知"} · 状态: {script.status}
                  </div>
                </div>
                <Badge>{script.status}</Badge>
              </button>
            ))}
          </div>
        </TabsContent>
      </Tabs>
    </Surface>
  );
}
