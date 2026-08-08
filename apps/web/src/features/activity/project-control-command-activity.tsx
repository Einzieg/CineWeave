"use client";

import {
  Ban,
  Bot,
  CheckCircle2,
  CircleUserRound,
  Clock3,
  Code2,
  Loader2,
  RefreshCcw,
  XCircle,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/shared/status-badge";
import { cn } from "@/lib/cn";
import { localizePlatformError } from "@/lib/error-localization";
import { statusLabel } from "@/lib/labels";
import { workflowLabel } from "@/lib/routes";
import type {
  ProjectControlCommand,
  ProjectControlCommandEvent,
  ProjectControlCommandItem,
  ProjectControlCommandSnapshot,
} from "@/lib/types";

export function ProjectControlCommandListButton({
  command,
  selected,
  onSelect,
}: {
  command: ProjectControlCommand;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "grid gap-2 rounded-lg border p-3 text-left transition hover:bg-muted/50",
        selected && "border-primary bg-muted/60",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-2">
          <ProjectControlStatusIcon status={command.status} />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{projectControlActionLabel(command)}</div>
            <div className="mt-1 text-xs text-muted-foreground">{formatDate(command.createdAt)}</div>
          </div>
        </div>
        <StatusBadge status={command.status} />
      </div>
      <div className="flex items-center justify-between gap-2">
        <ControllerBadge controllerType={command.controllerType} />
        {command.workflowRunIds?.length ? (
          <span className="text-xs text-muted-foreground">{command.workflowRunIds.length} 个后台任务</span>
        ) : null}
      </div>
      {command.errorMessage ? (
        <div className="line-clamp-2 text-xs text-destructive">
          {localizePlatformError(command.errorMessage, command.errorCode)}
        </div>
      ) : null}
    </button>
  );
}

export function ProjectControlCommandDetail({
  snapshot,
  events,
  loading,
  active,
  cancelling,
  retrying,
  onCancel,
  onRetry,
}: {
  snapshot?: ProjectControlCommandSnapshot;
  events: ProjectControlCommandEvent[];
  loading: boolean;
  active: boolean;
  cancelling: boolean;
  retrying: boolean;
  onCancel: () => void;
  onRetry: () => void;
}) {
  if (loading && !snapshot) {
    return (
      <div className="grid gap-4 p-5">
        <Skeleton className="h-36" />
        <Skeleton className="h-52" />
      </div>
    );
  }
  if (!snapshot) {
    return <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">任务详情不可用</div>;
  }
  const { command, items, childCommands, workflowRuns, pendingPrompt } = snapshot;
  const counts = projectControlItemCounts(items);
  const retryableCount = items.filter((item) => item.status === "failed" && item.retryable).length;
  const processed = counts.succeeded + counts.failed + counts.cancelled + counts.skipped;
  return (
    <div className="grid gap-5 p-5">
      <section className="rounded-lg border p-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <ProjectControlStatusIcon status={command.status} />
              <h3 className="text-base font-semibold">{projectControlActionLabel(command)}</h3>
              <StatusBadge status={command.status} />
              <ControllerBadge controllerType={command.controllerType} />
            </div>
            <div className="mt-2 grid gap-1 text-xs text-muted-foreground">
              <span>创建时间：{formatDate(command.createdAt)}</span>
              {command.startedAt ? <span>开始时间：{formatDate(command.startedAt)}</span> : null}
              {command.completedAt ? <span>完成时间：{formatDate(command.completedAt)}</span> : null}
              {command.cancellationRequestedAt ? <span>已于 {formatDate(command.cancellationRequestedAt)} 请求取消</span> : null}
            </div>
            <details className="mt-2">
              <summary className="cursor-pointer text-xs text-muted-foreground">技术信息</summary>
              <div className="mt-1 grid gap-1 rounded bg-muted px-2 py-1 text-xs text-muted-foreground">
                <span>命令：{shortId(command.id)}</span>
                <span>动作：{command.actionName} v{command.actionVersion}</span>
                <span>执行模式：{executionModeLabel(command.executionMode)}</span>
                <span>修订号：{command.revision}</span>
              </div>
            </details>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {!active && (command.status === "failed" || command.status === "partial_succeeded") ? (
              <Button size="sm" onClick={onRetry} disabled={retrying || (items.length > 0 && retryableCount === 0)}>
                {retrying ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5" />}
                {items.length > 0 ? `重试失败项（${retryableCount}）` : "重试任务"}
              </Button>
            ) : null}
            {active ? (
              <Button variant="destructive" size="sm" onClick={onCancel} disabled={cancelling || Boolean(command.cancellationRequestedAt)}>
                {cancelling ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Ban className="h-3.5 w-3.5" />}
                {command.cancellationRequestedAt ? "取消处理中" : "取消任务"}
              </Button>
            ) : null}
          </div>
        </div>
        {items.length > 0 ? (
          <div className="mt-4 grid gap-2">
            <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
              <span>已处理 {processed}/{items.length}</span>
              <span>成功 {counts.succeeded}</span>
              {counts.failed > 0 ? <span>失败 {counts.failed}</span> : null}
              {counts.running > 0 ? <span>运行中 {counts.running}</span> : null}
              {counts.queued > 0 ? <span>等待中 {counts.queued}</span> : null}
            </div>
            <Progress value={Math.round((processed / items.length) * 100)} className="h-2" />
          </div>
        ) : null}
        {command.errorMessage ? (
          <div className="mt-4 rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
            {localizePlatformError(command.errorMessage, command.errorCode)}
          </div>
        ) : null}
        {pendingPrompt ? (
          <div className="mt-4 rounded-md border border-status-warning/30 bg-status-warning/10 p-3 text-sm">
            <div className="font-medium">等待用户确认</div>
            <div className="mt-1 whitespace-pre-wrap text-muted-foreground">{pendingPrompt.prompt}</div>
          </div>
        ) : null}
      </section>

      {items.length > 0 ? (
        <section className="grid gap-3">
          <div className="flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold">独立执行项</h4>
            <Badge variant="outline">{items.length} 项</Badge>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            {items.map((item) => <ProjectControlItemCard key={item.id} item={item} />)}
          </div>
        </section>
      ) : null}

      {childCommands.length > 0 ? (
        <section className="grid gap-3">
          <div className="flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold">子任务</h4>
            <Badge variant="outline">{childCommands.length} 项</Badge>
          </div>
          <div className="grid gap-2">
            {childCommands.map((child) => (
              <div key={child.id} className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">{projectControlActionLabel(child)}</div>
                  <div className="mt-0.5 text-xs text-muted-foreground">{formatDate(child.createdAt)}</div>
                </div>
                <StatusBadge status={child.status} />
              </div>
            ))}
          </div>
        </section>
      ) : null}

      {workflowRuns.length > 0 ? (
        <section className="grid gap-3">
          <div className="flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold">关联后台任务</h4>
            <Badge variant="outline">{workflowRuns.length} 个</Badge>
          </div>
          <div className="grid gap-2">
            {workflowRuns.map((run) => (
              <div key={run.id} className="grid gap-2 rounded-lg border p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <ProjectControlStatusIcon status={run.status} />
                    <span className="truncate text-sm font-medium">{workflowLabel(run.workflowType)}</span>
                  </div>
                  <StatusBadge status={run.status} />
                </div>
                {run.totalItems > 0 ? (
                  <div className="text-xs text-muted-foreground">
                    已完成 {run.completedItems}/{run.totalItems}{run.failedItems > 0 ? ` · 失败 ${run.failedItems}` : ""}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        </section>
      ) : null}

      <section className="grid gap-3">
        <div className="flex items-center justify-between gap-3">
          <h4 className="text-sm font-semibold">实时动态</h4>
          <Badge variant="outline">{events.length} 条</Badge>
        </div>
        {events.length === 0 ? (
          <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">
            {active ? "等待任务更新" : "该任务没有动态记录"}
          </div>
        ) : (
          <div className="grid gap-2">
            {events.slice().reverse().map((event) => (
              <div key={`${event.commandId}:${event.sequence}`} className="rounded-lg border px-3 py-2">
                <div className="flex items-start justify-between gap-3">
                  <div className="text-sm font-medium">{projectControlEventLabel(event.eventType)}</div>
                  <span className="shrink-0 text-xs text-muted-foreground">{formatDate(event.createdAt)}</span>
                </div>
                {eventPayloadMessage(event) ? (
                  <div className="mt-1 whitespace-pre-wrap text-xs text-muted-foreground">{eventPayloadMessage(event)}</div>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

export function isActiveProjectControlCommand(command: ProjectControlCommand) {
  return ["queued", "running", "waiting_workflow", "waiting_input"].includes(command.status);
}

function ProjectControlItemCard({ item }: { item: ProjectControlCommandItem }) {
  return (
    <article className="grid content-start gap-2 rounded-lg border p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{projectControlItemLabel(item)}</div>
          <div className="mt-0.5 truncate text-xs text-muted-foreground">{targetTypeLabel(item.targetType)}</div>
        </div>
        <StatusBadge status={item.status} />
      </div>
      {item.errorMessage || item.errorCode ? (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 p-2 text-xs text-destructive">
          {localizePlatformError(item.errorMessage, item.errorCode)}
        </div>
      ) : null}
      <div className="text-xs text-muted-foreground">
        {item.retryable && item.status === "failed" ? "该项可单独重试" : statusLabel(item.status)}
      </div>
    </article>
  );
}

function ControllerBadge({ controllerType }: { controllerType: ProjectControlCommand["controllerType"] }) {
  const values = {
    codex_mcp: { label: "Codex", Icon: Code2 },
    embedded_agent: { label: "项目助手", Icon: Bot },
    manual: { label: "手动操作", Icon: CircleUserRound },
  } as const;
  const value = values[controllerType];
  return (
    <Badge variant="outline" className="gap-1 font-normal">
      <value.Icon className="h-3 w-3" />
      {value.label}
    </Badge>
  );
}

function ProjectControlStatusIcon({ status }: { status: string }) {
  if (["queued", "running", "waiting_workflow"].includes(status)) {
    return <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-status-running" />;
  }
  if (status === "waiting_input") {
    return <Clock3 className="mt-0.5 h-4 w-4 shrink-0 text-status-warning" />;
  }
  if (["succeeded", "partial_succeeded", "completed"].includes(status)) {
    return <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-status-success" />;
  }
  return <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />;
}

function projectControlActionLabel(command: ProjectControlCommand) {
  return command.actionLabel?.trim() || "项目操作";
}

function projectControlItemLabel(item: ProjectControlCommandItem) {
  if (item.stableOrdinal) return `第 ${item.stableOrdinal} 项`;
  return item.itemKey || "执行项";
}

function targetTypeLabel(value: string) {
  const labels: Record<string, string> = {
    source_chapter: "原文分集",
    script_episode: "剧本分集",
    canonical_asset: "项目资产",
    storyboard_shot: "分镜镜头",
    commerce_script_unit: "广告脚本",
    project_source: "项目内容",
  };
  return labels[value] ?? "项目对象";
}

function executionModeLabel(value: ProjectControlCommand["executionMode"]) {
  switch (value) {
    case "sync":
      return "同步执行";
    case "workflow":
      return "工作流";
    default:
      return "后台命令";
  }
}

function projectControlEventLabel(eventType: string) {
  const labels: Record<string, string> = {
    "project.control.command.created": "任务已创建",
    "project.control.command.running": "任务开始执行",
    "project.control.command.waiting_workflow": "后台任务运行中",
    "project.control.command.waiting_input": "等待用户确认",
    "project.control.command.cancellation_requested": "已请求取消",
    "project.control.command.succeeded": "任务已完成",
    "project.control.command.partial_succeeded": "任务部分完成",
    "project.control.command.failed": "任务执行失败",
    "project.control.command.cancelled": "任务已取消",
  };
  return labels[eventType] ?? "任务状态已更新";
}

function eventPayloadMessage(event: ProjectControlCommandEvent) {
  const payload = event.payload ?? {};
  for (const key of ["userMessage", "message", "summary", "errorMessage"]) {
    const value = payload[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}

function projectControlItemCounts(items: ProjectControlCommandItem[]) {
  const counts = { queued: 0, running: 0, succeeded: 0, failed: 0, cancelled: 0, skipped: 0 };
  for (const item of items) {
    if (item.status === "waiting_workflow") counts.running += 1;
    else if (item.status in counts) counts[item.status as keyof typeof counts] += 1;
  }
  return counts;
}

function shortId(value?: string) {
  if (!value) return "-";
  return value.length > 12 ? value.slice(0, 8) : value;
}

function formatDate(value?: string) {
  if (!value) return "-";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(value));
}
