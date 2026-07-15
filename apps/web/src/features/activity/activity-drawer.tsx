"use client";

import { useMemo, useState } from "react";
import { Activity, AlertCircle, Ban, CheckCircle2, Clock3, Loader2, Radio, RefreshCcw, XCircle } from "lucide-react";
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
import { useActivityStore, type ActivityRealtimeEvent, type ActivityRealtimeStatus } from "@/lib/stores/activity-store";
import { useUiStore } from "@/lib/stores/ui-store";
import { isActiveWorkflowStatus } from "@/lib/workflow-status";
import type { WorkflowNodeRun, WorkflowRun } from "@/lib/types";

const emptyEvents: ActivityRealtimeEvent[] = [];
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
  const [selectedActivityId, setSelectedActivityId] = useState("");
  const liveEvents = useActivityStore((state) => state.eventsByProject[projectId] ?? emptyEvents);
  const connectionStatus = useActivityStore((state) => state.connectionByProject[projectId] ?? "idle");

  const { data: project } = useApiQuery({
    key: qk.project(projectId),
    queryFn: (session) => studioApi.getProject(session, projectId),
    enabled: activityOpen,
  });

  const {
    data: workflowRuns = [],
    isLoading: workflowRunsLoading,
    isFetching: workflowRunsFetching,
    refetch: refetchWorkflowRuns,
  } = useApiQuery({
    key: qk.workflowRuns(projectId),
    queryFn: (session) => studioApi.listWorkflowRuns(session, projectId).then((response) => response.items),
    enabled: activityOpen,
    refetchInterval: (query) =>
      activityOpen && connectionStatus !== "connected" && query.state.data?.some(isActiveWorkflow) ? 5000 : false,
  });

  const activeWorkflowCount = useMemo(() => workflowRuns.filter(isActiveWorkflow).length, [workflowRuns]);
  const selectedRun = useMemo(
    () => {
      if (selectedActivityId.startsWith("workflow:")) {
        const runId = selectedActivityId.slice("workflow:".length);
        return workflowRuns.find((run) => run.id === runId) ?? workflowRuns.find(isActiveWorkflow) ?? workflowRuns[0];
      }
      return workflowRuns.find(isActiveWorkflow) ?? workflowRuns[0];
    },
    [selectedActivityId, workflowRuns],
  );
  const selectedWorkflowRunId = selectedRun?.id ?? "";
  const selectedRunActive = selectedRun ? isActiveWorkflow(selectedRun) : false;

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

  const selectedLiveEvents = useMemo(() => {
    if (!selectedWorkflowRunId) {
      return liveEvents.slice(-30);
    }
    return liveEvents.filter((event) => stringValue(event.payload.workflowRunId) === selectedWorkflowRunId).slice(-40);
  }, [liveEvents, selectedWorkflowRunId]);
  const visibleWorkflowNodes = useMemo(
    () => (selectedRun && isAssetBatchWorkflow(selectedRun) ? workflowNodes : sequentialVisibleNodes(workflowNodes)),
    [selectedRun, workflowNodes],
  );

  const cancelMutation = useApiMutation({
    mutationFn: (session, workflowRunId: string) => studioApi.cancelWorkflowRun(session, workflowRunId, "用户在任务活动面板取消"),
    onSuccess: (run) => {
      toast.success("任务取消请求已提交");
      invalidate([qk.workflowRuns(projectId), qk.workflowNodes(run.id), qk.productionStatus(projectId), qk.shotProductionPrefix(projectId)]);
    },
    onError: (error) => toast.error("取消失败：" + error.message),
  });

  const retryAssetBatchMutation = useApiMutation({
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
        qk.assets(projectId),
        qk.artifacts(projectId),
        qk.productionStatus(projectId),
      ]);
    },
    onError: (error) => toast.error("重试失败：" + error.message),
  });

  const refreshSelected = () => {
    void refetchWorkflowRuns();
    if (selectedWorkflowRunId) {
      void refetchWorkflowNodes();
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
                <span>活动任务 {activeWorkflowCount}</span>
                {(workflowRunsFetching || workflowNodesFetching) && <span>同步中</span>}
              </div>
            </div>
            <Button variant="outline" size="sm" onClick={refreshSelected}>
              <RefreshCcw className="h-3.5 w-3.5" />
              刷新
            </Button>
          </div>
        </SheetHeader>

        <div className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden lg:grid-cols-[320px_minmax(0,1fr)]">
          <aside className="min-h-0 border-b lg:border-b-0 lg:border-r">
            <ScrollArea className="h-full">
              <div className="grid gap-2 p-4">
                {workflowRunsLoading ? (
                  <>
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                    <Skeleton className="h-24" />
                  </>
                ) : workflowRuns.length === 0 ? (
                  <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">暂无任务记录</div>
                ) : (
                  <>
                    {workflowRuns.map((run) => (
                      <button
                        key={run.id}
                        type="button"
                        onClick={() => setSelectedActivityId(`workflow:${run.id}`)}
                        className={cn(
                          "grid gap-2 rounded-lg border p-3 text-left transition hover:bg-muted/50",
                          selectedRun?.id === run.id && "border-primary bg-muted/60",
                        )}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex min-w-0 items-start gap-2">
                            <WorkflowRunStatusIcon status={run.status} />
                            <div className="min-w-0">
                              <div className="truncate text-sm font-medium">{workflowLabel(workflowTypeFromRun(run))}</div>
                              <div className="mt-1 text-xs text-muted-foreground">{formatDate(run.createdAt)}</div>
                            </div>
                          </div>
                          <StatusBadge status={run.status} />
                        </div>
                        <div className="line-clamp-2 text-xs text-muted-foreground">{workflowInputSummary(run)}</div>
                      </button>
                    ))}
                  </>
                )}
              </div>
            </ScrollArea>
          </aside>

          <main className="min-h-0">
            {!selectedRun ? (
              <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">选择任务后查看实时动态</div>
            ) : selectedRun ? (
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
                        {isAssetBatchWorkflow(selectedRun) && !selectedRunActive && selectedRun.failedItems > 0 ? (
                          <Button
                            size="sm"
                            onClick={() => retryAssetBatchMutation.mutate(selectedRun)}
                            disabled={retryAssetBatchMutation.isPending || !project?.revision}
                          >
                            {retryAssetBatchMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5" />}
                            重试失败项（{selectedRun.failedItems}）
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
                    {isAssetBatchWorkflow(selectedRun) && selectedRun.totalItems > 0 ? (
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
                    {selectedRun.errorMessage ? (
                      <div className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                        {localizePlatformError(selectedRun.errorMessage, selectedRun.errorCode)}
                      </div>
                    ) : null}
                  </section>

                  <section className="grid gap-3">
                    <div className="flex items-center justify-between gap-3">
                      <h4 className="text-sm font-semibold">{isAssetBatchWorkflow(selectedRun) ? "资产处理明细" : "Agent 动态与输出"}</h4>
                      <Badge variant="outline">{visibleWorkflowNodes.length} 个节点</Badge>
                    </div>
                    {workflowNodesLoading ? (
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

                  <section className="grid gap-3">
                    <div className="flex items-center justify-between gap-3">
                      <h4 className="text-sm font-semibold">实时事件</h4>
                      <Badge variant="outline">{selectedLiveEvents.length} 条</Badge>
                    </div>
                    {selectedLiveEvents.length === 0 ? (
                      <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">暂无实时事件</div>
                    ) : (
                      <div className="grid gap-2">
                        {selectedLiveEvents.slice().reverse().map((event) => (
                          <RealtimeEventRow key={event.id} event={event} />
                        ))}
                      </div>
                    )}
                  </section>
                </div>
              </ScrollArea>
            ) : null}
          </main>
        </div>
      </SheetContent>
    </Sheet>
  );
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

function RealtimeEventRow({ event }: { event: ActivityRealtimeEvent }) {
  const output = recordValue(event.payload.output);
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-3 rounded-lg border p-3">
      <EventIcon eventType={event.eventType} />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <div className="text-sm font-medium">{activityEventLabel(event)}</div>
          <span className="text-xs text-muted-foreground">{formatDate(event.receivedAt)}</span>
        </div>
        <details className="mt-1">
          <summary className="cursor-pointer text-xs text-muted-foreground">事件详情</summary>
          <div className="mt-1 truncate rounded bg-muted px-2 py-1 text-xs text-muted-foreground">{event.eventType}</div>
        </details>
        {hasMeaningfulValue(output) ? (
          <pre className="mt-2 max-h-40 overflow-auto rounded-md bg-muted p-2 text-xs leading-relaxed">{jsonPreview(output)}</pre>
        ) : null}
      </div>
    </div>
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

function EventIcon({ eventType }: { eventType: string }) {
  if (eventType.endsWith(".failed")) {
    return <AlertCircle className="mt-0.5 h-4 w-4 text-status-danger" />;
  }
  if (eventType.endsWith(".completed") || eventType.endsWith(".succeeded") || eventType.endsWith(".reviewed") || eventType.endsWith(".cancelled")) {
    return <CheckCircle2 className="mt-0.5 h-4 w-4 text-status-success" />;
  }
  if (eventType.endsWith(".started") || eventType.endsWith(".progress")) {
    return <Loader2 className="mt-0.5 h-4 w-4 animate-spin text-status-running" />;
  }
  return <Clock3 className="mt-0.5 h-4 w-4 text-muted-foreground" />;
}

function isActiveWorkflow(run: WorkflowRun) {
  return isActiveWorkflowStatus(run.status);
}

function isAssetBatchWorkflow(run: WorkflowRun) {
  return run.workflowType === "batch_generate_asset_cards" || run.workflowType === "batch_generate_asset_images";
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
  const parts = [
    assetItems.length > 0 ? `资产 ${assetItems.length}` : "",
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

function activityEventLabel(event: ActivityRealtimeEvent) {
  const nodeKey = stringValue(event.payload.nodeKey);
  const workflowType = stringValue(event.payload.workflowType);
  switch (event.eventType) {
    case "workflow.run.queued":
      return `${workflowLabel(workflowType || "任务")}已排队`;
    case "workflow.node.started":
      return `${nodeLabel(nodeKey, "")}开始执行`;
    case "workflow.node.progress":
      return `${nodeLabel(nodeKey, "")}更新进度`;
    case "workflow.node.completed":
      return `${nodeLabel(nodeKey, "")}已完成`;
    case "workflow.node.failed":
      return `${nodeLabel(nodeKey, "")}失败`;
    case "workflow.run.completed":
      return `${workflowLabel(workflowType || "任务")}已完成`;
    case "workflow.run.partial_succeeded":
      return `${workflowLabel(workflowType || "任务")}部分完成`;
    case "workflow.run.failed":
      return `${workflowLabel(workflowType || "任务")}失败`;
    case "workflow.run.cancelled":
      return `${workflowLabel(workflowType || "任务")}已取消`;
    case "workflow.run.cancelling":
      return "任务取消中";
    case "storyboard.shot.render_plan.created":
      return "视频执行计划已生成";
    case "storyboard.segment.queued":
      return "视频片段已排队";
    case "storyboard.segment.running":
      return "视频片段生成中";
    case "storyboard.segment.succeeded":
      return "视频片段已生成";
    case "storyboard.segment.failed":
      return "视频片段生成失败";
    case "storyboard.segment.media.processed":
      return "视频片段媒体已处理";
    case "storyboard.segment.prompt.running":
      return "视频片段提示词生成中";
    case "storyboard.segment.prompt.reviewed":
      return "视频片段提示词已审核";
    case "storyboard.audio.verification.completed":
      return "原生音轨审核已更新";
    default:
      return "任务更新";
  }
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

function truncate(value: string, maxLength: number) {
  return value.length > maxLength ? `${value.slice(0, maxLength)}...` : value;
}
