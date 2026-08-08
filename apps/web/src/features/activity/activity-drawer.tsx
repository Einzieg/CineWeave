"use client";

import { useMemo, useState } from "react";
import { Activity, AlertCircle, Ban, CheckCircle2, Clock3, Loader2, Radio, RefreshCcw, Trash2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/shared/status-badge";
import { studioApi } from "@/lib/api-client";
import { cn } from "@/lib/cn";
import { localizePlatformError } from "@/lib/error-localization";
import { statusLabel } from "@/lib/labels";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import { workflowLabel } from "@/lib/routes";
import { useActivityStore, type ActivityRealtimeStatus } from "@/lib/stores/activity-store";
import { useUiStore } from "@/lib/stores/ui-store";
import { isActiveWorkflowStatus } from "@/lib/workflow-status";
import {
  buildShotPromptActivityItems,
  shotPromptActivityProgress,
  ShotPromptActivityList,
} from "./shot-prompt-activity";
import {
  isActiveProjectControlCommand,
  ProjectControlCommandDetail,
  ProjectControlCommandListButton,
} from "./project-control-command-activity";
import type {
  DerivedAssetBatchProjection,
  DerivedAssetRequestItemProjection,
  EpisodeVideoProductionItem,
  ListEnvelope,
  ProjectControlCommand,
  ProjectControlCommandStatus,
  WorkflowNodeRun,
  WorkflowRun,
  WorkflowVideoProductionActivity,
} from "@/lib/types";

const emptyWorkflowRuns: WorkflowRun[] = [];
const emptyProjectControlCommands: ProjectControlCommand[] = [];
const activeWorkflowListShape = { status: "active", view: "activity", limit: 100 } as const;
const terminalWorkflowListShape = { status: "terminal", view: "activity", limit: 20 } as const;
const activeProjectControlStatuses: ProjectControlCommandStatus[] = ["queued", "running", "waiting_workflow", "waiting_input"];
const terminalProjectControlStatuses: ProjectControlCommandStatus[] = ["succeeded", "partial_succeeded", "failed", "cancelled"];
const activeProjectControlListShape = { statuses: activeProjectControlStatuses, view: "activity", limit: 50 } as const;
const terminalProjectControlListShape = { statuses: terminalProjectControlStatuses, view: "activity", limit: 20 } as const;
const nodeLabels: Record<string, string> = {
  extract_novel_events: "小说事件提取 Agent",
  generate_adaptation_plan: "改编计划 Agent",
  adaptation_plan_to_script: "剧本生成 Agent",
  source_to_script: "原文转剧本 Agent",
  parse_script_scenes: "分场解析 Agent",
  script_to_assets: "资产分析 Agent",
  script_to_storyboard: "分镜生成 Agent",
  prepare_storyboard_episodes: "准备分集分镜",
  workflow_storyboard_prepare_episodes: "准备分集分镜",
  generate_storyboard: "分镜生成 Agent",
  generate_shot_image: "镜头图片生成",
  create_shot_video_task: "视频任务创建",
  poll_shot_video_task: "视频任务轮询",
  compose_final_video: "最终成片合成",
  compose_timeline: "时间线合成",
};

export function ActivityDrawer({ projectId }: { projectId: string }) {
  const activityOpen = useUiStore((state) => state.activityOpen);
  const setActivityOpen = useUiStore((state) => state.setActivityOpen);
  const invalidate = useInvalidateKeys();
  const [selectedActivity, setSelectedActivity] = useState({ projectId: "", id: "" });
  const [additionalTerminalPageState, setAdditionalTerminalPageState] = useState<{
    projectId: string;
    firstPageKey: string;
    pages: ListEnvelope<WorkflowRun>[];
  }>({ projectId: "", firstPageKey: "", pages: [] });
  const [additionalTerminalCommandPageState, setAdditionalTerminalCommandPageState] = useState<{
    projectId: string;
    firstPageKey: string;
    pages: ListEnvelope<ProjectControlCommand>[];
  }>({ projectId: "", firstPageKey: "", pages: [] });
  const connectionStatus = useActivityStore((state) => state.connectionByProject[projectId] ?? "idle");
  const selectedActivityId = selectedActivity.projectId === projectId ? selectedActivity.id : "";
  const setSelectedActivityId = (id: string) => setSelectedActivity({ projectId, id });

  const { data: project } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
    enabled: activityOpen,
  });

  const {
    data: activeWorkflowPage,
    isLoading: activeWorkflowRunsLoading,
    isFetching: activeWorkflowRunsFetching,
    refetch: refetchActiveWorkflowRuns,
  } = useApiQuery({
    key: qk.workflowRuns(projectId, activeWorkflowListShape),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId, activeWorkflowListShape),
    enabled: activityOpen,
    refetchInterval: (query) =>
      activityOpen && connectionStatus !== "connected" && query.state.data?.items.some(isActiveWorkflow) ? 5000 : false,
  });

  const {
    data: terminalWorkflowPage,
    isLoading: terminalWorkflowRunsLoading,
    isFetching: terminalWorkflowRunsFetching,
    refetch: refetchTerminalWorkflowRuns,
  } = useApiQuery({
    key: qk.workflowRuns(projectId, terminalWorkflowListShape),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId, terminalWorkflowListShape),
    enabled: activityOpen,
  });

  const {
    data: activeCommandPage,
    isLoading: activeCommandsLoading,
    isFetching: activeCommandsFetching,
    refetch: refetchActiveCommands,
  } = useApiQuery({
    key: qk.projectControlCommands(projectId, activeProjectControlListShape),
    queryFn: (session) => studioApi.listProjectControlCommands(session, projectId, activeProjectControlListShape),
    enabled: activityOpen,
    refetchInterval: (query) =>
      activityOpen && connectionStatus !== "connected" && query.state.data?.items.some(isActiveProjectControlCommand) ? 5000 : false,
  });
  const {
    data: terminalCommandPage,
    isLoading: terminalCommandsLoading,
    isFetching: terminalCommandsFetching,
    refetch: refetchTerminalCommands,
  } = useApiQuery({
    key: qk.projectControlCommands(projectId, terminalProjectControlListShape),
    queryFn: (session) => studioApi.listProjectControlCommands(session, projectId, terminalProjectControlListShape),
    enabled: activityOpen,
  });
  const activeCommands = activeCommandPage?.items ?? emptyProjectControlCommands;
  const firstTerminalCommandPageKey = `${terminalCommandPage?.items[0]?.id ?? "empty"}:${terminalCommandPage?.nextCursor ?? "end"}`;
  const additionalTerminalCommandPages = useMemo(
    () =>
      additionalTerminalCommandPageState.projectId === projectId
      && additionalTerminalCommandPageState.firstPageKey === firstTerminalCommandPageKey
        ? additionalTerminalCommandPageState.pages
        : [],
    [additionalTerminalCommandPageState, firstTerminalCommandPageKey, projectId],
  );
  const terminalCommands = useMemo(
    () => [
      ...(terminalCommandPage?.items ?? emptyProjectControlCommands),
      ...additionalTerminalCommandPages.flatMap((page) => page.items),
    ],
    [additionalTerminalCommandPages, terminalCommandPage?.items],
  );
  const projectControlCommands = useMemo(
    () => [...activeCommands, ...terminalCommands],
    [activeCommands, terminalCommands],
  );
  const linkedWorkflowRunIds = useMemo(
    () => new Set(projectControlCommands.flatMap((command) => command.workflowRunIds ?? [])),
    [projectControlCommands],
  );
  const activeWorkflowRuns = useMemo(
    () => (activeWorkflowPage?.items ?? emptyWorkflowRuns).filter((run) => !linkedWorkflowRunIds.has(run.id)),
    [activeWorkflowPage?.items, linkedWorkflowRunIds],
  );
  const firstTerminalPageKey = `${terminalWorkflowPage?.items[0]?.id ?? "empty"}:${terminalWorkflowPage?.nextCursor ?? "end"}`;
  const additionalTerminalPages = useMemo(
    () =>
      additionalTerminalPageState.projectId === projectId
      && additionalTerminalPageState.firstPageKey === firstTerminalPageKey
        ? additionalTerminalPageState.pages
        : [],
    [additionalTerminalPageState, firstTerminalPageKey, projectId],
  );
  const terminalWorkflowRuns = useMemo(
    () => [
      ...(terminalWorkflowPage?.items ?? emptyWorkflowRuns),
      ...additionalTerminalPages.flatMap((page) => page.items),
    ].filter((run) => !linkedWorkflowRunIds.has(run.id)),
    [additionalTerminalPages, linkedWorkflowRunIds, terminalWorkflowPage?.items],
  );
  const workflowRuns = useMemo(
    () => [...activeWorkflowRuns, ...terminalWorkflowRuns],
    [activeWorkflowRuns, terminalWorkflowRuns],
  );
  const activityEntries = useMemo(() => {
    const entries = [
      ...projectControlCommands.map((command) => ({ kind: "command" as const, command })),
      ...workflowRuns.map((run) => ({ kind: "workflow" as const, run })),
    ];
    return entries.sort((left, right) => {
      const leftActive = left.kind === "command" ? isActiveProjectControlCommand(left.command) : isActiveWorkflow(left.run);
      const rightActive = right.kind === "command" ? isActiveProjectControlCommand(right.command) : isActiveWorkflow(right.run);
      if (leftActive !== rightActive) return leftActive ? -1 : 1;
      const leftCreatedAt = left.kind === "command" ? left.command.createdAt : left.run.createdAt;
      const rightCreatedAt = right.kind === "command" ? right.command.createdAt : right.run.createdAt;
      return Date.parse(rightCreatedAt ?? "") - Date.parse(leftCreatedAt ?? "");
    });
  }, [projectControlCommands, workflowRuns]);
  const activeActivityCount = activeCommands.length + activeWorkflowRuns.length;
  const lastTerminalPage = additionalTerminalPages.at(-1) ?? terminalWorkflowPage;
  const terminalNextCursor = lastTerminalPage?.nextCursor ?? "";
  const terminalHasMore = lastTerminalPage?.hasMore === true && terminalNextCursor !== "";
  const lastTerminalCommandPage = additionalTerminalCommandPages.at(-1) ?? terminalCommandPage;
  const terminalCommandNextCursor = lastTerminalCommandPage?.nextCursor ?? "";
  const terminalCommandHasMore = lastTerminalCommandPage?.hasMore === true && terminalCommandNextCursor !== "";
  const activityListLoading = activeWorkflowRunsLoading || terminalWorkflowRunsLoading || activeCommandsLoading || terminalCommandsLoading;
  const activityListFetching = activeWorkflowRunsFetching || terminalWorkflowRunsFetching || activeCommandsFetching || terminalCommandsFetching;

  const selectedCommand = useMemo(() => {
    if (selectedActivityId.startsWith("command:")) {
      const commandId = selectedActivityId.slice("command:".length);
      return projectControlCommands.find((command) => command.id === commandId)
        ?? projectControlCommands.find(isActiveProjectControlCommand)
        ?? projectControlCommands[0];
    }
    if (selectedActivityId.startsWith("workflow:")) return undefined;
    return projectControlCommands.find(isActiveProjectControlCommand)
      ?? (workflowRuns.some(isActiveWorkflow) ? undefined : projectControlCommands[0]);
  }, [projectControlCommands, selectedActivityId, workflowRuns]);
  const selectedRun = useMemo(
    () => {
      if (selectedCommand) return undefined;
      if (selectedActivityId.startsWith("workflow:")) {
        const runId = selectedActivityId.slice("workflow:".length);
        return workflowRuns.find((run) => run.id === runId) ?? workflowRuns.find(isActiveWorkflow) ?? workflowRuns[0];
      }
      return workflowRuns.find(isActiveWorkflow) ?? workflowRuns[0];
    },
    [selectedActivityId, selectedCommand, workflowRuns],
  );
  const selectedCommandId = selectedCommand?.id ?? "";
  const selectedCommandActive = selectedCommand ? isActiveProjectControlCommand(selectedCommand) : false;
  const selectedWorkflowRunId = selectedRun?.id ?? "";
  const selectedRunActive = selectedRun ? isActiveWorkflow(selectedRun) : false;
  const selectedVideoBatchWorkflow = selectedRun ? isVideoBatchWorkflow(selectedRun) : false;
  const selectedDerivedAssetBatchWorkflow = selectedRun ? isDerivedAssetBatchWorkflow(selectedRun) : false;
  const selectedShotPromptBatchWorkflow = selectedRun ? isShotPromptBatchWorkflow(selectedRun) : false;
  const selectedSourceToScriptWorkflow = selectedRun ? isSourceToScriptWorkflow(selectedRun) : false;
  const selectedRunOutput = recordValue(selectedRun?.output);
  const sourceToScriptFailedEpisodes = arrayValue(selectedRunOutput.failedEpisodes)
    .map(numberValue)
    .filter((value) => value > 0);
  const sourceToScriptMissingItems = numberValue(selectedRunOutput.missingItems);
  const sourceToScriptActivated = selectedRunOutput.activated === true;

  const {
    data: selectedCommandSnapshot,
    isLoading: selectedCommandLoading,
    isFetching: selectedCommandFetching,
    refetch: refetchSelectedCommand,
  } = useApiQuery({
    key: qk.projectControlCommand(selectedCommandId || "none"),
    queryFn: (session) => studioApi.getProjectControlCommand(session, selectedCommandId),
    enabled: activityOpen && !!selectedCommandId,
    refetchInterval: activityOpen && connectionStatus !== "connected" && selectedCommandActive ? 3000 : false,
  });
  const {
    data: selectedCommandEvents,
    isFetching: selectedCommandEventsFetching,
    refetch: refetchSelectedCommandEvents,
  } = useApiQuery({
    key: qk.projectControlCommandEvents(selectedCommandId || "none"),
    queryFn: (session) => studioApi.listProjectControlCommandEvents(session, selectedCommandId, "", 100),
    enabled: activityOpen && !!selectedCommandId,
    refetchInterval: activityOpen && connectionStatus !== "connected" && selectedCommandActive ? 3000 : false,
  });

  const {
    data: workflowNodes = [],
    isLoading: workflowNodesLoading,
    isFetching: workflowNodesFetching,
    refetch: refetchWorkflowNodes,
  } = useApiQuery({
    key: qk.workflowNodes(selectedWorkflowRunId || "none"),
    queryFn: (session) => studioApi.listWorkflowNodes(session, selectedWorkflowRunId).then((response) => response.items),
    enabled: activityOpen && !!selectedWorkflowRunId,
    refetchInterval: activityOpen && connectionStatus !== "connected" && selectedRunActive ? 3000 : false,
  });

  const {
    data: videoProductionActivity,
    isLoading: videoProductionActivityLoading,
    isFetching: videoProductionActivityFetching,
    refetch: refetchVideoProductionActivity,
  } = useApiQuery({
    key: qk.workflowVideoProduction(selectedWorkflowRunId || "none"),
    queryFn: (session) => studioApi.getWorkflowVideoProductionActivity(session, selectedWorkflowRunId),
    enabled: activityOpen && selectedVideoBatchWorkflow && !!selectedWorkflowRunId,
    refetchInterval: activityOpen && connectionStatus !== "connected" && selectedRunActive ? 3000 : false,
  });

  const {
    data: derivedAssetBatch,
    isLoading: derivedAssetBatchLoading,
    isFetching: derivedAssetBatchFetching,
    refetch: refetchDerivedAssetBatch,
  } = useApiQuery({
    key: qk.workflowDerivedAssetBatch(selectedWorkflowRunId || "none"),
    queryFn: (session) => studioApi.getWorkflowDerivedAssetBatch(session, selectedWorkflowRunId),
    enabled: activityOpen && selectedDerivedAssetBatchWorkflow && !!selectedWorkflowRunId,
    refetchInterval: activityOpen && connectionStatus !== "connected" && selectedRunActive ? 3000 : false,
  });

  const visibleWorkflowNodes = useMemo(
    () => (selectedRun && isItemizedWorkflow(selectedRun) ? workflowNodes : sequentialVisibleNodes(workflowNodes)),
    [selectedRun, workflowNodes],
  );
  const shotPromptItems = useMemo(() => buildShotPromptActivityItems(workflowNodes), [workflowNodes]);
  const shotPromptProgress = useMemo(
    () => shotPromptActivityProgress(shotPromptItems, selectedRun?.totalItems ?? 0),
    [selectedRun?.totalItems, shotPromptItems],
  );

  const cancelMutation = useApiMutation({
    mutationFn: (session, workflowRunId: string) => studioApi.cancelWorkflowRun(session, workflowRunId, "用户在任务活动面板取消"),
    onSuccess: (run) => {
      toast.success("任务取消请求已提交");
      invalidate([qk.workflowRuns(projectId), qk.workflowNodes(run.id), qk.productionStatus(projectId), qk.shotProductionPrefix(projectId)]);
    },
    onError: (error) => toast.error("取消失败：" + error.message),
  });

  const cancelCommandMutation = useApiMutation({
    mutationFn: (session, command: ProjectControlCommand) => studioApi.cancelProjectControlCommand(session, command.id, {
      expectedCommandRevision: command.revision,
      idempotencyKey: activityIdempotencyKey("cancel", command.id),
      reason: "用户在任务活动面板取消",
    }),
    onSuccess: (command) => {
      toast.success("任务取消请求已提交");
      invalidate([
        qk.projectControlCommands(projectId),
        qk.projectControlCommand(command.id),
        qk.projectControlCommandEvents(command.id),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("取消失败：" + error.message),
  });

  const retryCommandMutation = useApiMutation({
    mutationFn: (session, command: ProjectControlCommand) => studioApi.retryProjectControlCommand(session, command.id, {
      expectedCommandRevision: command.revision,
      idempotencyKey: activityIdempotencyKey("retry", command.id),
    }),
    onSuccess: (command) => {
      toast.success("已创建失败项重试任务");
      setSelectedActivityId(`command:${command.id}`);
      invalidate([
        qk.projectControlCommands(projectId),
        qk.projectControlCommand(command.id),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("重试失败：" + error.message),
  });

  const retryFailedItemsMutation = useApiMutation({
    mutationFn: (session, run: WorkflowRun) => {
      if (!project?.revision) {
        throw new Error("项目状态尚未加载，请稍后重试");
      }
      return studioApi.retryFailedWorkflowRun(session, run.id, {
        expectedProjectRevision: project.revision,
        maxConcurrency: 5,
      });
    },
    onSuccess: (retryRun) => {
      toast.success("失败项已创建新的重试任务");
      setSelectedActivityId(`workflow:${retryRun.id}`);
      invalidate([
        qk.workflowRuns(projectId),
        qk.workflowNodes(retryRun.id),
        qk.workflowDerivedAssetBatch(retryRun.id),
        qk.assetsRoot(projectId),
        qk.artifacts(projectId),
        qk.sources(projectId),
        qk.scripts(projectId),
        qk.scriptDetailsPrefix(projectId),
        qk.scriptVersionsPrefix(projectId),
        qk.scriptEpisodesPrefix(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("重试失败：" + error.message),
  });

  const loadMoreTerminalMutation = useApiMutation({
    mutationFn: (session, cursor: string) => studioApi.listWorkflowRuns(session, projectId, {
      ...terminalWorkflowListShape,
      cursor,
    }),
    onSuccess: (page) => {
      setAdditionalTerminalPageState((current) => ({
        projectId,
        firstPageKey: firstTerminalPageKey,
        pages: [
          ...(current.projectId === projectId && current.firstPageKey === firstTerminalPageKey ? current.pages : []),
          page,
        ],
      }));
    },
    onError: (error) => toast.error("加载更多任务失败：" + error.message),
  });

  const loadMoreTerminalCommandsMutation = useApiMutation({
    mutationFn: (session, cursor: string) => studioApi.listProjectControlCommands(session, projectId, {
      ...terminalProjectControlListShape,
      cursor,
    }),
    onSuccess: (page) => {
      setAdditionalTerminalCommandPageState((current) => ({
        projectId,
        firstPageKey: firstTerminalCommandPageKey,
        pages: [
          ...(current.projectId === projectId && current.firstPageKey === firstTerminalCommandPageKey ? current.pages : []),
          page,
        ],
      }));
    },
    onError: (error) => toast.error("加载更多任务失败：" + error.message),
  });

  const clearCompletedMutation = useApiMutation({
    mutationFn: (session) => studioApi.clearCompletedWorkflowActivity(session, projectId),
    onSuccess: (result) => {
      setSelectedActivityId("");
      setAdditionalTerminalPageState({ projectId, firstPageKey: firstTerminalPageKey, pages: [] });
      setAdditionalTerminalCommandPageState({ projectId, firstPageKey: firstTerminalCommandPageKey, pages: [] });
      invalidate([qk.workflowRuns(projectId), qk.projectControlCommands(projectId)]);
      toast.success(result.clearedCount > 0 ? `已清空 ${result.clearedCount} 条已结束任务` : "没有需要清空的已结束任务");
    },
    onError: (error) => toast.error("清空失败：" + error.message),
  });

  const retryVideoItemsMutation = useApiMutation({
    mutationFn: async (session, activity: WorkflowVideoProductionActivity) => {
      const groups = failedVideoItemsByEpisode(activity);
      if (groups.length === 0) {
        throw new Error("没有可重试的失败镜头");
      }
      return Promise.all(groups.map((group) => studioApi.generateShotVideosBatch(session, projectId, {
        scriptEpisodeId: group.scriptEpisodeId,
        shotIds: group.shotIds,
        force: true,
        maxConcurrency: 5,
      })));
    },
    onSuccess: (runs) => {
      toast.success(`已为 ${runs.reduce((total, run) => total + run.targetShotIds.length, 0)} 个失败镜头创建重试任务`);
      const firstRun = runs[0];
      if (firstRun) {
        setSelectedActivityId(`workflow:${firstRun.workflowRunId}`);
      }
      invalidate([
        qk.workflowRuns(projectId),
        qk.shotProductionPrefix(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("重试失败：" + error.message),
  });

  const refreshSelected = () => {
    setAdditionalTerminalPageState({ projectId, firstPageKey: firstTerminalPageKey, pages: [] });
    setAdditionalTerminalCommandPageState({ projectId, firstPageKey: firstTerminalCommandPageKey, pages: [] });
    void refetchActiveWorkflowRuns();
    void refetchTerminalWorkflowRuns();
    void refetchActiveCommands();
    void refetchTerminalCommands();
    if (selectedCommandId) {
      void refetchSelectedCommand();
      void refetchSelectedCommandEvents();
    }
    if (selectedWorkflowRunId) {
      void refetchWorkflowNodes();
      if (selectedVideoBatchWorkflow) {
        void refetchVideoProductionActivity();
      }
      if (selectedDerivedAssetBatchWorkflow) {
        void refetchDerivedAssetBatch();
      }
    }
  };

  return (
    <Sheet open={activityOpen} onOpenChange={setActivityOpen}>
      <SheetContent
        side="right"
        className="data-[side=right]:w-[96vw] data-[side=right]:max-w-none data-[side=right]:sm:max-w-[960px] gap-0 p-0"
      >
        <SheetHeader className="shrink-0 border-b px-6 py-5">
          <div className="flex items-start justify-between gap-10 pr-10">
            <div>
              <SheetTitle className="flex items-center gap-2">
                <Activity className="h-5 w-5" />
                任务活动
              </SheetTitle>
              <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <ConnectionBadge status={connectionStatus} />
                <span>活动任务 {activeActivityCount}</span>
                {(activityListFetching || selectedCommandFetching || selectedCommandEventsFetching || workflowNodesFetching || videoProductionActivityFetching || derivedAssetBatchFetching) && <span>同步中</span>}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => clearCompletedMutation.mutate()}
                disabled={clearCompletedMutation.isPending || (terminalWorkflowRuns.length === 0 && terminalCommands.length === 0)}
              >
                {clearCompletedMutation.isPending
                  ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  : <Trash2 className="h-3.5 w-3.5" />}
                清空已结束
              </Button>
              <Button variant="outline" size="sm" onClick={refreshSelected}>
                <RefreshCcw className="h-3.5 w-3.5" />
                刷新
              </Button>
            </div>
          </div>
        </SheetHeader>

        <div className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden lg:grid-cols-[320px_minmax(0,1fr)]">
          <aside className="min-h-0 border-b lg:border-b-0 lg:border-r">
            <ScrollArea className="h-full">
              <div className="grid gap-2 p-4">
                {activityListLoading ? (
                  <>
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                  </>
                ) : activityEntries.length === 0 ? (
                  <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">暂无任务记录</div>
                ) : (
                  <>
                    {activityEntries.map((entry) => entry.kind === "command" ? (
                      <ProjectControlCommandListButton
                        key={`command:${entry.command.id}`}
                        command={entry.command}
                        selected={selectedCommand?.id === entry.command.id}
                        onSelect={() => setSelectedActivityId(`command:${entry.command.id}`)}
                      />
                    ) : (
                      <button
                        key={`workflow:${entry.run.id}`}
                        type="button"
                        onClick={() => setSelectedActivityId(`workflow:${entry.run.id}`)}
                        className={cn(
                          "grid gap-2 rounded-lg border p-3 text-left transition hover:bg-muted/50",
                          selectedRun?.id === entry.run.id && "border-primary bg-muted/60",
                        )}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex min-w-0 items-start gap-2">
                            <WorkflowRunStatusIcon status={entry.run.status} />
                            <div className="min-w-0">
                              <div className="truncate text-sm font-medium">{workflowLabel(workflowTypeFromRun(entry.run))}</div>
                              <div className="mt-1 text-xs text-muted-foreground">{formatDate(entry.run.createdAt)}</div>
                            </div>
                          </div>
                          <StatusBadge status={entry.run.status} />
                        </div>
                        <div className="line-clamp-2 text-xs text-muted-foreground">{workflowInputSummary(entry.run)}</div>
                      </button>
                    ))}
                    {terminalCommandHasMore ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => loadMoreTerminalCommandsMutation.mutate(terminalCommandNextCursor)}
                        disabled={loadMoreTerminalCommandsMutation.isPending}
                      >
                        {loadMoreTerminalCommandsMutation.isPending
                          ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          : <RefreshCcw className="h-3.5 w-3.5" />}
                        加载更多命令
                      </Button>
                    ) : null}
                    {terminalHasMore ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => loadMoreTerminalMutation.mutate(terminalNextCursor)}
                        disabled={loadMoreTerminalMutation.isPending}
                      >
                        {loadMoreTerminalMutation.isPending
                          ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          : <RefreshCcw className="h-3.5 w-3.5" />}
                        加载更多旧任务
                      </Button>
                    ) : null}
                  </>
                )}
              </div>
            </ScrollArea>
          </aside>

          <main className="min-h-0">
            {selectedCommand ? (
              <ScrollArea className="h-full">
                <ProjectControlCommandDetail
                  snapshot={selectedCommandSnapshot}
                  events={selectedCommandEvents?.items ?? []}
                  loading={selectedCommandLoading}
                  active={selectedCommandActive}
                  cancelling={cancelCommandMutation.isPending}
                  retrying={retryCommandMutation.isPending}
                  onCancel={() => selectedCommandSnapshot && cancelCommandMutation.mutate(selectedCommandSnapshot.command)}
                  onRetry={() => selectedCommandSnapshot && retryCommandMutation.mutate(selectedCommandSnapshot.command)}
                />
              </ScrollArea>
            ) : !selectedRun ? (
              <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">选择任务后查看实时动态</div>
            ) : (
              <ScrollArea className="h-full">
                <div className="grid gap-5 p-5">
                  <section className="rounded-lg border p-4">
                    <div className="flex flex-wrap items-start justify-between gap-4">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <WorkflowRunStatusIcon status={selectedRun.status} />
                          <h3 className="text-base font-semibold">{workflowLabel(workflowTypeFromRun(selectedRun))}</h3>
                          <StatusBadge status={selectedRun.status} />
                        </div>
                        <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
                          <span>创建时间：{formatDate(selectedRun.createdAt)}</span>
                          {selectedRun.startedAt ? <span>开始时间：{formatDate(selectedRun.startedAt)}</span> : null}
                          {selectedRun.completedAt ? <span>完成时间：{formatDate(selectedRun.completedAt)}</span> : null}
                        </div>
                        <details className="mt-2">
                          <summary className="cursor-pointer text-xs text-muted-foreground">技术信息</summary>
                          <div className="mt-1 grid gap-1 rounded bg-muted px-2 py-1 text-xs text-muted-foreground">
                            <span>任务编号：{shortId(selectedRun.id)}</span>
                            {selectedRun.temporalWorkflowId ? <span>后台任务：{shortId(selectedRun.temporalWorkflowId)}</span> : null}
                          </div>
                        </details>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        {usesFailedItemRetry(selectedRun) && !selectedRunActive && (
                          selectedDerivedAssetBatchWorkflow
                            ? derivedAssetRetryableItemCount(derivedAssetBatch) > 0
                            : selectedRun.failedItems > 0
                        ) ? (
                          <Button
                            size="sm"
                            onClick={() => retryFailedItemsMutation.mutate(selectedRun)}
                            disabled={retryFailedItemsMutation.isPending || !project?.revision}
                          >
                            {retryFailedItemsMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5" />}
                            {selectedSourceToScriptWorkflow ? "重试失败分集" : "重试失败项"}（{selectedDerivedAssetBatchWorkflow ? derivedAssetRetryableItemCount(derivedAssetBatch) : selectedRun.failedItems}）
                          </Button>
                        ) : null}
                        {selectedVideoBatchWorkflow && !selectedRunActive && (videoProductionActivity?.failedItems ?? 0) > 0 ? (
                          <Button
                            size="sm"
                            onClick={() => videoProductionActivity && retryVideoItemsMutation.mutate(videoProductionActivity)}
                            disabled={retryVideoItemsMutation.isPending || !videoProductionActivity}
                          >
                            {retryVideoItemsMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5" />}
                            重试失败镜头（{videoProductionActivity?.failedItems ?? 0}）
                          </Button>
                        ) : null}
                        {selectedRunActive ? (
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => cancelMutation.mutate(selectedRun.id)}
                            disabled={cancelMutation.isPending}
                          >
                            <Ban className="h-3.5 w-3.5" />
                            取消任务
                          </Button>
                        ) : null}
                      </div>
                    </div>
                    {usesFailedItemRetry(selectedRun) && selectedRun.totalItems > 0 ? (
                      <div className="mt-4 grid gap-2">
                        <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                          <span>已完成 {selectedRun.completedItems}/{selectedRun.totalItems}</span>
                          {selectedRun.failedItems > 0 ? <span>失败 {selectedRun.failedItems}</span> : null}
                          <span>已处理 {Math.min(selectedRun.totalItems, selectedRun.completedItems + selectedRun.failedItems)}/{selectedRun.totalItems}</span>
                        </div>
                        <Progress
                          value={Math.round((Math.min(selectedRun.totalItems, selectedRun.completedItems + selectedRun.failedItems) / selectedRun.totalItems) * 100)}
                          className="h-2"
                        />
                      </div>
                    ) : null}
                    {selectedSourceToScriptWorkflow && selectedRun.failedItems > 0 ? (
                      <div className="mt-4 rounded-md border border-status-warning/30 bg-status-warning/10 p-3 text-sm text-status-warning">
                        <div className="font-medium">
                          {sourceToScriptFailedEpisodes.length > 0
                            ? `第 ${sourceToScriptFailedEpisodes.join("、")} 集生成失败`
                            : `${selectedRun.failedItems} 个分集生成失败`}
                        </div>
                        <div className="mt-1 text-xs leading-relaxed">
                          {sourceToScriptMissingItems > 0
                            ? `其中 ${sourceToScriptMissingItems} 集没有旧内容可回退，部分版本未激活，当前剧本保持不变。`
                            : sourceToScriptActivated
                              ? "失败分集已保留旧版本内容并标记为需要重新生成，其余分集已组成并激活新版本。"
                              : "当前剧本未切换；可使用上方按钮仅重试失败分集。"}
                        </div>
                      </div>
                    ) : null}
                    {selectedShotPromptBatchWorkflow && shotPromptProgress.totalItems > 0 ? (
                      <div className="mt-4 grid gap-2">
                        <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                          <span>已完成 {shotPromptProgress.completedItems}/{shotPromptProgress.totalItems}</span>
                          {shotPromptProgress.failedItems > 0 ? <span>失败 {shotPromptProgress.failedItems}</span> : null}
                          <span>已处理 {shotPromptProgress.processedItems}/{shotPromptProgress.totalItems}</span>
                        </div>
                        <Progress
                          value={Math.round((shotPromptProgress.processedItems / shotPromptProgress.totalItems) * 100)}
                          className="h-2"
                        />
                      </div>
                    ) : null}
                    {selectedVideoBatchWorkflow && videoProductionActivity && videoProductionActivity.totalItems > 0 ? (
                      <div className="mt-4 grid gap-2">
                        <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                          <span>已完成 {videoProductionActivity.succeededItems}/{videoProductionActivity.totalItems}</span>
                          {videoProductionActivity.failedItems > 0 ? <span>失败 {videoProductionActivity.failedItems}</span> : null}
                          {videoProductionActivity.activeItems > 0 ? <span>运行中 {videoProductionActivity.activeItems}</span> : null}
                        </div>
                        <Progress
                          value={Math.round(((videoProductionActivity.succeededItems + videoProductionActivity.failedItems) / videoProductionActivity.totalItems) * 100)}
                          className="h-2"
                        />
                      </div>
                    ) : null}
                    {selectedRun.errorMessage ? (
                      <div className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                        {localizePlatformError(selectedRun.errorMessage, selectedRun.errorCode)}
                      </div>
                    ) : null}
                  </section>

                  {selectedVideoBatchWorkflow ? (
                    <VideoProductionActivitySection
                      activity={videoProductionActivity}
                      loading={videoProductionActivityLoading}
                      active={selectedRunActive}
                    />
                  ) : null}

                  {selectedDerivedAssetBatchWorkflow ? (
                    <DerivedAssetActivitySection
                      batch={derivedAssetBatch}
                      loading={derivedAssetBatchLoading}
                      active={selectedRunActive}
                    />
                  ) : null}

                  <section className="grid gap-3">
                    <div className="flex items-center justify-between gap-3">
                      <h4 className="text-sm font-semibold">
                        {isAssetBatchWorkflow(selectedRun)
                          ? "资产处理明细"
                          : selectedDerivedAssetBatchWorkflow
                            ? "执行节点与输出"
                          : selectedShotPromptBatchWorkflow
                            ? "分镜提示词明细"
                            : selectedVideoBatchWorkflow
                              ? "工作流节点与输出"
                              : "Agent 动态与输出"}
                      </h4>
                      <Badge variant="outline">
                        {selectedShotPromptBatchWorkflow
                          ? `${shotPromptItems.length}/${shotPromptProgress.totalItems} 个分镜`
                          : `${visibleWorkflowNodes.length} 个节点`}
                      </Badge>
                    </div>
                    {selectedShotPromptBatchWorkflow ? (
                      <ShotPromptActivityList
                        items={shotPromptItems}
                        loading={workflowNodesLoading}
                        active={selectedRunActive}
                        totalItems={shotPromptProgress.totalItems}
                      />
                    ) : workflowNodesLoading ? (
                      <div className="grid gap-2">
                        <Skeleton className="h-24" />
                        <Skeleton className="h-24" />
                      </div>
                    ) : visibleWorkflowNodes.length === 0 ? (
                      <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">
                        {selectedRunActive ? "等待 Worker 接收任务" : "该任务没有节点记录"}
                      </div>
                    ) : (
                      <div className="grid gap-3">
                        {visibleWorkflowNodes.map((node) => (
                          <NodeRunCard key={node.id} node={node} />
                        ))}
                      </div>
                    )}
                  </section>
                </div>
              </ScrollArea>
            )}
          </main>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function DerivedAssetActivitySection({
  batch,
  loading,
  active,
}: {
  batch?: DerivedAssetBatchProjection;
  loading: boolean;
  active: boolean;
}) {
  if (loading) {
    return (
      <section className="grid gap-3">
        <h4 className="text-sm font-semibold">镜头衍生资产明细</h4>
        <Skeleton className="h-28" />
        <Skeleton className="h-28" />
      </section>
    );
  }
  if (!batch) {
    return (
      <section className="grid gap-3">
        <h4 className="text-sm font-semibold">镜头衍生资产明细</h4>
        <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">
          {active ? "正在建立持久化工作集" : "该任务没有衍生资产工作集"}
        </div>
      </section>
    );
  }
  const processed = Math.max(0, batch.totalItems - batch.pendingItems - batch.queuedItems - batch.runningItems);
  return (
    <section className="grid gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="text-sm font-semibold">镜头衍生资产明细</h4>
          <div className="mt-1 text-xs text-muted-foreground">
            已处理 {processed}/{batch.totalItems} · 成功 {batch.succeededItems} · 可执行 {batch.executableItems}
          </div>
        </div>
        <StatusBadge status={batch.status} />
      </div>
      {batch.totalItems > 0 ? (
        <Progress value={Math.round((processed / batch.totalItems) * 100)} className="h-2" />
      ) : null}
      <div className="flex flex-wrap gap-2 text-xs">
        {batch.reviewRequiredItems > 0 ? <Badge variant="outline">待审核 {batch.reviewRequiredItems}</Badge> : null}
        {batch.generationMismatchItems > 0 ? <Badge variant="outline">旧生产代 {batch.generationMismatchItems}</Badge> : null}
        {batch.notFoundItems > 0 ? <Badge variant="outline">未找到 {batch.notFoundItems}</Badge> : null}
        {batch.alreadyRunningItems > 0 ? <Badge variant="outline">已在运行 {batch.alreadyRunningItems}</Badge> : null}
        {batch.duplicateItems > 0 ? <Badge variant="outline">重复输入 {batch.duplicateItems}</Badge> : null}
        {batch.failedRetryableItems > 0 ? <Badge variant="outline">可重试失败 {batch.failedRetryableItems}</Badge> : null}
        {batch.failedTerminalItems > 0 ? <Badge variant="outline">终态失败 {batch.failedTerminalItems}</Badge> : null}
        {batch.discardedItems > 0 ? <Badge variant="outline">已丢弃 {batch.discardedItems}</Badge> : null}
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        {batch.items.map((item) => <DerivedAssetRequestItemCard key={item.id} item={item} />)}
      </div>
      <details>
        <summary className="cursor-pointer text-xs text-muted-foreground">批次技术信息</summary>
        <div className="mt-2 grid gap-1 rounded bg-muted px-3 py-2 text-xs text-muted-foreground">
          <span>批次：{shortId(batch.id)}</span>
          <span>生产代次：{shortId(batch.productionGenerationId)}</span>
          <span>配置绑定：{shortId(batch.videoProductionBindingId)} · 第 {batch.videoProductionBindingRevision} 版</span>
          <span>工作集模式：{derivedAssetRequestModeLabel(batch.requestMode)}</span>
          {batch.retryOfBatchId ? <span>重试来源：{shortId(batch.retryOfBatchId)}</span> : null}
        </div>
      </details>
    </section>
  );
}

function DerivedAssetRequestItemCard({ item }: { item: DerivedAssetRequestItemProjection }) {
  const errorMessage = item.errorMessage ?? item.execution?.errorMessage;
  const errorCode = item.errorCode ?? item.execution?.errorCode;
  return (
    <article className="grid content-start gap-3 rounded-lg border p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm font-medium">第 {item.inputOrdinal} 项</div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            需求 {shortId(item.requirementId || item.originalId)} · {derivedAssetDispositionLabel(item.disposition)}
          </div>
        </div>
        <StatusBadge status={item.status} />
      </div>
      {errorMessage || errorCode ? (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 p-2 text-xs text-destructive">
          {localizePlatformError(errorMessage, errorCode)}
        </div>
      ) : null}
      <div className="grid gap-1 text-xs text-muted-foreground">
        {item.execution ? <span>执行尝试：第 {item.execution.attemptNo} 次 · {statusLabel(item.execution.status)}</span> : <span>未创建供应商执行尝试</span>}
        {item.retryable ? <span>该项满足条件后可单独重试</span> : null}
        {item.duplicateOfRequestItemId ? <span>重复于工作集中的较早输入</span> : null}
      </div>
      {item.execution ? (
        <details>
          <summary className="cursor-pointer text-xs text-muted-foreground">执行技术信息</summary>
          <div className="mt-2 grid gap-1 rounded bg-muted px-2 py-1 text-xs text-muted-foreground">
            <span>执行项：{shortId(item.execution.id)}</span>
            <span>节点：{shortId(item.execution.nodeRunId)}</span>
            {item.execution.providerCallId ? <span>供应商调用：{shortId(item.execution.providerCallId)}</span> : null}
            {item.execution.artifactId ? <span>产物：{shortId(item.execution.artifactId)}</span> : null}
            {item.execution.lateResultCount > 0 ? <span>迟到结果：{item.execution.lateResultCount}</span> : null}
          </div>
        </details>
      ) : null}
    </article>
  );
}

function VideoProductionActivitySection({
  activity,
  loading,
  active,
}: {
  activity?: WorkflowVideoProductionActivity;
  loading: boolean;
  active: boolean;
}) {
  if (loading) {
    return (
      <section className="grid gap-3">
        <h4 className="text-sm font-semibold">镜头视频生产明细</h4>
        <Skeleton className="h-32" />
        <Skeleton className="h-32" />
      </section>
    );
  }
  if (!activity || activity.checkpoints.length === 0) {
    return (
      <section className="grid gap-3">
        <h4 className="text-sm font-semibold">镜头视频生产明细</h4>
        <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">
          {active ? "正在准备分集执行检查点" : "该任务没有分集视频执行记录"}
        </div>
      </section>
    );
  }
  return (
    <section className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <h4 className="text-sm font-semibold">镜头视频生产明细</h4>
        <Badge variant="outline">{activity.totalItems} 个镜头</Badge>
      </div>
      {activity.checkpoints.map((checkpoint) => (
        <div key={checkpoint.id} className="grid gap-3 border-t pt-4 first:border-t-0 first:pt-0">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="text-sm font-medium">第 {checkpoint.episodeIndex} 集 · {checkpoint.episodeTitle}</div>
              <div className="mt-1 text-xs text-muted-foreground">
                已提交 {checkpoint.batches.length} 个批次 · 下一批次 {checkpoint.nextBatchOrdinal}
              </div>
            </div>
            <StatusBadge status={checkpoint.status} />
          </div>
          {checkpoint.batches.length === 0 ? (
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {active ? "正在创建首个镜头批次" : "该分集没有已提交批次"}
            </div>
          ) : checkpoint.batches.map((batch) => (
            <div key={batch.id} className="grid gap-3">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-2">
                <div className="text-sm font-medium">批次 {batch.ordinal} · 第 {batch.attempt} 次执行</div>
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span>完成 {batch.succeededItems}/{batch.totalItems}</span>
                  {batch.failedItems > 0 ? <span>失败 {batch.failedItems}</span> : null}
                  <StatusBadge status={batch.status} />
                </div>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                {batch.items.map((item) => <VideoProductionItemCard key={item.id} item={item} />)}
              </div>
            </div>
          ))}
          <details>
            <summary className="cursor-pointer text-xs text-muted-foreground">分集执行技术信息</summary>
            <div className="mt-2 grid gap-1 rounded bg-muted px-3 py-2 text-xs text-muted-foreground">
              <span>检查点：{shortId(checkpoint.id)}</span>
              <span>生产代次：{shortId(checkpoint.productionGenerationId)}</span>
              <span>方案版本：{shortId(checkpoint.profileVersionId)}</span>
            </div>
          </details>
        </div>
      ))}
    </section>
  );
}

function VideoProductionItemCard({ item }: { item: EpisodeVideoProductionItem }) {
  const error = videoProductionItemError(item);
  return (
    <article className="grid content-start gap-3 rounded-lg border p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm font-medium">镜头 {item.shotNo}</div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">{item.shotTitle || "未命名镜头"}</div>
        </div>
        <StatusBadge status={item.status} />
      </div>
      <div className="grid gap-2">
        <VideoProductionStage label="计划首帧" status={anchorStageStatus(item)} detail={item.anchorReviewStatus ? statusLabel(item.anchorReviewStatus) : "等待生成"} />
        <VideoProductionStage label="参考输入包" status={referencePackStageStatus(item)} detail={item.referencePackStatus ? statusLabel(item.referencePackStatus) : "等待编译"} />
        <VideoProductionStage label="已审核提示词" status={promptPlanStageStatus(item)} detail={item.videoPromptPlanStatus ? `第 ${item.videoPromptPlanRevision ?? 1} 版 · ${statusLabel(item.videoPromptPlanStatus)}` : "等待已审核计划"} />
        <VideoProductionStage label="上游视频任务" status={providerTaskStageStatus(item)} detail={providerTaskStageDetail(item)} />
        <VideoProductionStage label="媒体入库" status={item.mediaStatus} detail={statusLabel(item.mediaStatus)} />
      </div>
      {error ? (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 p-2 text-xs text-destructive">{error}</div>
      ) : null}
      <details>
        <summary className="cursor-pointer text-xs text-muted-foreground">镜头执行技术信息</summary>
        <div className="mt-2 grid gap-1 rounded bg-muted px-2 py-1 text-xs text-muted-foreground">
          <span>执行项：{shortId(item.id)}</span>
          <span>尝试次数：{item.attempt}</span>
          {item.externalTaskId ? <span>上游任务：{shortId(item.externalTaskId)}</span> : null}
          {item.providerPollCount !== undefined ? <span>轮询次数：{item.providerPollCount}</span> : null}
        </div>
      </details>
    </article>
  );
}

function VideoProductionStage({ label, status, detail }: { label: string; status: string; detail: string }) {
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-2 text-xs">
      <ProductionStageIcon status={status} />
      <div className="min-w-0">
        <div className="font-medium">{label}</div>
        <div className="truncate text-muted-foreground">{detail}</div>
      </div>
    </div>
  );
}

function ProductionStageIcon({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  if (["succeeded", "completed", "approved", "active", "ready", "stored"].includes(normalized)) {
    return <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 text-status-success" />;
  }
  if (["failed", "rejected", "discarded", "stale"].includes(normalized)) {
    return <XCircle className="mt-0.5 h-3.5 w-3.5 text-status-danger" />;
  }
  if (["running", "processing", "generating", "polling", "transferring", "cancelling"].includes(normalized)) {
    return <Loader2 className="mt-0.5 h-3.5 w-3.5 animate-spin text-status-running" />;
  }
  return <Clock3 className="mt-0.5 h-3.5 w-3.5 text-muted-foreground" />;
}

function NodeRunCard({ node }: { node: WorkflowNodeRun }) {
  const summary = nodeOutputSummary(node.output);
  const hasOutput = hasMeaningfulValue(node.output);
  const partialText = stringValue(recordValue(node.output).partialText);
  return (
    <article className="rounded-lg border p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <NodeStatusIcon status={node.status} />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{nodeLabel(node.nodeKey, node.nodeType, node.input)}</div>
            <div className="mt-1 text-xs text-muted-foreground">{formatDate(node.startedAt || node.createdAt)}</div>
          </div>
        </div>
        <StatusBadge status={node.status} />
      </div>
      {summary ? <div className="mt-3 rounded-md bg-muted/60 p-2 text-xs text-muted-foreground">{summary}</div> : null}
      {partialText ? (
        <pre className="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-xs leading-relaxed text-foreground">
          {partialText}
        </pre>
      ) : null}
      {node.errorMessage ? (
        <div className="mt-3 rounded-md border border-destructive/20 bg-destructive/10 p-2 text-xs text-destructive">
          {localizePlatformError(node.errorMessage, node.errorCode)}
        </div>
      ) : null}
      {hasOutput ? (
        <details className="mt-3">
          <summary className="cursor-pointer text-xs font-medium text-muted-foreground hover:text-foreground">查看输出内容</summary>
          <pre className="mt-2 max-h-72 overflow-auto rounded-md bg-muted p-3 text-xs leading-relaxed text-foreground">
            {jsonPreview(node.output)}
          </pre>
        </details>
      ) : null}
    </article>
  );
}

function ConnectionBadge({ status }: { status: ActivityRealtimeStatus }) {
  const connected = status === "connected";
  const reconnecting = status === "reconnecting";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-0.5",
        connected && "border-status-success/30 bg-status-success/10 text-status-success",
        reconnecting && "border-status-warning/30 bg-status-warning/10 text-status-warning",
        !connected && !reconnecting && "border-border bg-muted text-muted-foreground",
      )}
    >
      <Radio className="h-3 w-3" />
      {connected ? "实时已连接" : reconnecting ? "实时重连中" : "实时未连接"}
    </span>
  );
}

function WorkflowRunStatusIcon({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  if (normalized === "succeeded" || normalized === "completed") {
    return <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-status-success" />;
  }
  if (normalized === "partial_succeeded") {
    return <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-status-warning" />;
  }
  if (normalized === "failed") {
    return <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-status-danger" />;
  }
  if (normalized === "cancelled") {
    return <Ban className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />;
  }
  if (normalized === "running" || normalized === "processing" || normalized === "cancelling") {
    return <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-status-running" />;
  }
  return <Clock3 className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />;
}

function NodeStatusIcon({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  if (normalized === "succeeded" || normalized === "completed") {
    return <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-status-success" />;
  }
  if (normalized === "failed") {
    return <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-status-danger" />;
  }
  if (normalized === "cancelled") {
    return <Ban className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />;
  }
  if (normalized === "running" || normalized === "processing") {
    return <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-status-running" />;
  }
  return <Clock3 className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />;
}

function sequentialVisibleNodes(nodes: WorkflowNodeRun[]) {
  const visible: WorkflowNodeRun[] = [];
  for (const node of nodes) {
    visible.push(node);
    if (!isTerminalNodeStatus(node.status)) {
      break;
    }
  }
  return visible;
}

function isTerminalNodeStatus(status: string) {
  const normalized = status.toLowerCase();
  return normalized === "succeeded" || normalized === "completed" || normalized === "failed" || normalized === "cancelled";
}

function isActiveWorkflow(run: WorkflowRun) {
  return isActiveWorkflowStatus(run.status);
}

function isAssetBatchWorkflow(run: WorkflowRun) {
  return run.workflowType === "batch_generate_asset_cards" || run.workflowType === "batch_generate_asset_images";
}

function isSourceToScriptWorkflow(run: WorkflowRun) {
  return workflowTypeFromRun(run) === "source_to_script";
}

function isDerivedAssetBatchWorkflow(run: WorkflowRun) {
  return workflowTypeFromRun(run) === "batch_generate_derived_asset_images";
}

function usesFailedItemRetry(run: WorkflowRun) {
  return isAssetBatchWorkflow(run) || isSourceToScriptWorkflow(run) || isDerivedAssetBatchWorkflow(run);
}

function isVideoBatchWorkflow(run: WorkflowRun) {
  const workflowType = workflowTypeFromRun(run);
  return workflowType === "batch_generate_shot_videos"
    || workflowType === "episode_batch_generate_shot_videos"
    || workflowType === "episode_video_production";
}

function isShotPromptBatchWorkflow(run: WorkflowRun) {
  const workflowType = workflowTypeFromRun(run);
  return workflowType === "batch_generate_shot_image_prompts" || workflowType === "batch_generate_shot_video_prompts";
}

function isItemizedWorkflow(run: WorkflowRun) {
  return isAssetBatchWorkflow(run) || isDerivedAssetBatchWorkflow(run) || isShotPromptBatchWorkflow(run) || isVideoBatchWorkflow(run);
}

function derivedAssetRetryableItemCount(batch?: DerivedAssetBatchProjection) {
  return batch?.items.filter((item) => item.status === "failed_retryable" || (item.status === "blocked" && item.retryable)).length ?? 0;
}

function derivedAssetDispositionLabel(disposition: string) {
  const labels: Record<string, string> = {
    executable: "可执行",
    review_required: "需要审核",
    not_found: "未找到",
    generation_mismatch: "不属于当前生产代",
    already_running: "已在运行",
    duplicate: "重复输入",
    skipped: "已跳过",
  };
  return labels[disposition] ?? statusLabel(disposition);
}

function derivedAssetRequestModeLabel(mode: string) {
  const labels: Record<string, string> = { explicit: "显式选择", select_all: "筛选全部", retry: "失败重试" };
  return labels[mode] ?? mode;
}

function failedVideoItemsByEpisode(activity: WorkflowVideoProductionActivity) {
  return activity.checkpoints.flatMap((checkpoint) => {
    const shotIds = Array.from(new Set(checkpoint.batches.flatMap((batch) => batch.items)
      .filter((item) => item.status === "failed" || item.status === "discarded")
      .map((item) => item.storyboardShotId)));
    return shotIds.length > 0 ? [{ scriptEpisodeId: checkpoint.scriptEpisodeId, shotIds }] : [];
  });
}

function anchorStageStatus(item: EpisodeVideoProductionItem) {
  if (item.anchorReviewStatus === "rejected") return "rejected";
  if (item.anchorStatus === "ready" && item.anchorReviewStatus === "approved") return "approved";
  return item.anchorStatus || "pending";
}

function referencePackStageStatus(item: EpisodeVideoProductionItem) {
  return item.referencePackStatus || "pending";
}

function promptPlanStageStatus(item: EpisodeVideoProductionItem) {
  return item.videoPromptPlanStatus || "pending";
}

function providerTaskStageStatus(item: EpisodeVideoProductionItem) {
  if (item.providerAsyncTaskStatus) return item.providerAsyncTaskStatus;
  if (item.status === "failed" || item.status === "discarded") return "failed";
  return item.status === "running" ? "running" : "pending";
}

function providerTaskStageDetail(item: EpisodeVideoProductionItem) {
  if (!item.providerAsyncTaskStatus) {
    return item.status === "running" ? "正在创建上游任务" : "等待提交";
  }
  const pollText = item.providerPollCount !== undefined && item.providerPollCount > 0 ? ` · 已轮询 ${item.providerPollCount} 次` : "";
  return `${statusLabel(item.providerAsyncTaskStatus)}${pollText}`;
}

function videoProductionItemError(item: EpisodeVideoProductionItem) {
  const detail = recordValue(item.errorDetail);
  const cause = recordValue(detail.cause);
  const message = item.providerErrorMessage
    || stringValue(detail.message)
    || stringValue(detail.detail)
    || stringValue(detail.error)
    || stringValue(cause.message);
  const code = item.providerErrorCode || item.errorCode || stringValue(detail.code) || stringValue(cause.code);
  return message ? localizePlatformError(message, code) : "";
}

function workflowTypeFromRun(run: WorkflowRun) {
  const input = recordValue(run.input);
  const nestedInput = recordValue(input.input);
  return run.workflowType || stringValue(input.workflowType) || stringValue(nestedInput.workflowType) || stringValue(input.prompt) || "workflow";
}

function workflowInputSummary(run: WorkflowRun) {
  const input = recordValue(run.input);
  const nestedInput = recordValue(input.input);
  const chapterIds = arrayValue(nestedInput.chapterIds);
  const assetItems = arrayValue(input.items);
  const shotIds = arrayValue(nestedInput.shotIds);
  const parts = [
    assetItems.length > 0 ? `资产 ${assetItems.length}` : "",
    shotIds.length > 0 ? `分镜 ${shotIds.length}` : "",
    stringValue(nestedInput.sourceId) ? "已选择原文" : "",
    chapterIds.length > 0 ? `分集 ${chapterIds.length}` : "",
    stringValue(nestedInput.scriptId) ? "已选择剧本" : "",
    stringValue(nestedInput.timelineId) ? "已选择时间线" : "",
    numberValue(nestedInput.maxShots) ? `镜头 ${numberValue(nestedInput.maxShots)}` : "",
  ].filter(Boolean);
  return parts.join(" · ") || statusLabel(run.status);
}

function nodeLabel(nodeKey: string, nodeType: string, input?: unknown) {
  const values = recordValue(input);
  const assetName = stringValue(values.name) || "未命名资产";
  const episodeIndex = numberValue(values.episodeIndex);
  const episodeTotal = numberValue(values.episodeTotal);
  if (nodeKey.startsWith("generate_storyboard_from_script_")) {
    return episodeIndex > 0 ? `生成第 ${episodeIndex}/${Math.max(episodeIndex, episodeTotal)} 集分镜` : "生成分集分镜";
  }
  if (nodeType === "asset.prompt.generate" || nodeKey.startsWith("asset_prompt:")) {
    return `${assetName} · 生成提示词`;
  }
  if (nodeType === "asset.image.generate" || nodeKey.startsWith("asset_image:")) {
    return `${assetName} · 生成图片`;
  }
  if (nodeKey.startsWith("generate_shot_video_prompt_")) {
    return "视频提示词生成 Agent";
  }
  if (nodeKey.startsWith("review_shot_video_prompt_")) {
    return "视频提示词审核 Agent";
  }
  if (nodeKey.startsWith("generate_shot_image_prompt_")) {
    return "图片提示词生成 Agent";
  }
  if (nodeKey.startsWith("review_shot_image_prompt_")) {
    return "图片提示词审核 Agent";
  }
  if (nodeKey.startsWith("create_shot_video_")) {
    return "视频任务创建";
  }
  return nodeLabels[nodeKey] ?? nodeLabels[nodeType.replaceAll(".", "_")] ?? nodeLabels[nodeType] ?? "任务节点";
}

function nodeOutputSummary(output: unknown) {
  const record = recordValue(output);
  const parts = [
    arrayValue(record.eventIds).length > 0 ? `事件 ${arrayValue(record.eventIds).length}` : "",
    arrayValue(record.events).length > 0 ? `事件 ${arrayValue(record.events).length}` : "",
    numberValue(record.linkCount) ? `关联 ${numberValue(record.linkCount)}` : "",
    numberValue(record.shotCount) ? `镜头 ${numberValue(record.shotCount)}` : "",
    numberValue(record.receivedChars) ? `实时输出 ${numberValue(record.receivedChars)} 字` : "",
    stringValue(record.scriptId) ? "剧本已生成" : "",
    stringValue(record.artifactId) ? "产物已生成" : "",
    stringValue(record.status) ? `状态 ${statusLabel(stringValue(record.status))}` : "",
  ].filter(Boolean);
  if (parts.length > 0) {
    return parts.join(" · ");
  }
  const message = stringValue(record.message) || stringValue(record.summary) || stringValue(record.content);
  return message ? truncate(message, 180) : "";
}

function hasMeaningfulValue(value: unknown): boolean {
  if (value === null || value === undefined) {
    return false;
  }
  if (Array.isArray(value)) {
    return value.length > 0;
  }
  if (typeof value === "object") {
    return Object.keys(value).length > 0;
  }
  if (typeof value === "string") {
    return value.trim().length > 0;
  }
  return true;
}

function jsonPreview(value: unknown) {
  if (typeof value === "string") {
    return truncate(value, 4000);
  }
  try {
    return truncate(JSON.stringify(value, null, 2), 4000);
  } catch {
    return String(value);
  }
}

function recordValue(value: unknown): Record<string, unknown> {
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value) as unknown;
      return recordValue(parsed);
    } catch {
      return {};
    }
  }
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function shortId(value: string) {
  return value.length > 8 ? value.slice(0, 8) : value;
}

function formatDate(value?: string) {
  if (!value) {
    return "未记录";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString("zh-CN", { hour12: false });
}

function activityIdempotencyKey(action: "cancel" | "retry", commandId: string) {
  return `activity:${action}:${commandId}`;
}

function truncate(value: string, maxLength: number) {
  return value.length > maxLength ? `${value.slice(0, maxLength)}...` : value;
}
