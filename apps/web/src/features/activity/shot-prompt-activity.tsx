"use client";

import { AlertCircle, CheckCircle2, Clock3, Loader2, XCircle } from "lucide-react";
import { StatusBadge } from "@/components/shared/status-badge";
import { Skeleton } from "@/components/ui/skeleton";
import { localizePlatformError } from "@/lib/error-localization";
import type { WorkflowNodeRun } from "@/lib/types";

type ShotPromptStagePair = {
  key: string;
  logicalKey: string;
  label: string;
  activityAttempt: number;
  attempt: number;
  generation?: WorkflowNodeRun;
  review?: WorkflowNodeRun;
};

export type ShotPromptActivityItem = {
  key: string;
  shotId: string;
  shotNo: number;
  status: string;
  stages: ShotPromptStagePair[];
};

type MutableShotPromptItem = Omit<ShotPromptActivityItem, "status" | "stages"> & {
  stages: Map<string, ShotPromptStagePair>;
};

type ShotPromptNodePhase = "generation" | "review";

export function buildShotPromptActivityItems(nodes: WorkflowNodeRun[]): ShotPromptActivityItem[] {
  const items = new Map<string, MutableShotPromptItem>();
  for (const node of nodes) {
    const phase = shotPromptNodePhase(node);
    if (!phase) continue;

    const input = recordValue(node.input);
    const shotId = stringValue(input.shotId);
    const shotNo = numberValue(input.shotNo);
    const itemKey = shotId || (shotNo > 0 ? `shot-no:${shotNo}` : `node:${node.id}`);
    const item = items.get(itemKey) ?? {
      key: itemKey,
      shotId,
      shotNo,
      stages: new Map<string, ShotPromptStagePair>(),
    };
    if (!item.shotId && shotId) item.shotId = shotId;
    if (!item.shotNo && shotNo) item.shotNo = shotNo;

    const stageKey = shotPromptStageKey(node);
    const stage = item.stages.get(stageKey) ?? {
      key: stageKey,
      logicalKey: shotPromptLogicalStageKey(node, input),
      label: shotPromptStageLabel(node, input),
      activityAttempt: numberValue(input.activityAttempt) || 1,
      attempt: numberValue(input.attempt) || 1,
    };
    stage[phase] = node;
    item.stages.set(stageKey, stage);
    items.set(itemKey, item);
  }

  return Array.from(items.values())
    .map((item) => {
      const stages = Array.from(item.stages.values()).sort(compareShotPromptStages);
      return {
        key: item.key,
        shotId: item.shotId,
        shotNo: item.shotNo,
        status: shotPromptItemStatus(stages),
        stages,
      };
    })
    .sort((left, right) => {
      if (left.shotNo > 0 && right.shotNo > 0 && left.shotNo !== right.shotNo) return left.shotNo - right.shotNo;
      if (left.shotNo > 0) return -1;
      if (right.shotNo > 0) return 1;
      return left.key.localeCompare(right.key);
    });
}

export function shotPromptActivityProgress(items: ShotPromptActivityItem[], configuredTotal: number) {
  const completedItems = items.filter((item) => item.status === "succeeded").length;
  const failedItems = items.filter((item) => item.status === "failed").length;
  const totalItems = Math.max(configuredTotal, items.length);
  return {
    totalItems,
    completedItems,
    failedItems,
    processedItems: Math.min(totalItems, completedItems + failedItems),
  };
}

export function ShotPromptActivityList({
  items,
  loading,
  active,
  totalItems,
}: {
  items: ShotPromptActivityItem[];
  loading: boolean;
  active: boolean;
  totalItems: number;
}) {
  if (loading) {
    return (
      <div className="grid gap-2">
        <Skeleton className="h-36" />
        <Skeleton className="h-36" />
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <div className="rounded-lg border border-dashed p-6 text-sm text-muted-foreground">
        {active ? "正在准备分镜提示词任务" : "该任务没有分镜提示词记录"}
      </div>
    );
  }
  return (
    <div className="grid gap-2">
      {items.map((item, index) => (
        <article key={item.key} className="overflow-hidden rounded-lg border bg-background">
          <div className="flex items-center justify-between gap-3 px-3 py-2.5">
            <div className="flex min-w-0 items-center gap-2">
              <PromptStatusIcon status={item.status} />
              <div className="truncate text-sm font-medium">
                {item.shotNo > 0 ? `第 ${item.shotNo} 个分镜` : `第 ${index + 1} 个分镜`}
              </div>
            </div>
            <StatusBadge status={item.status} />
          </div>
          <div className="border-t">
            {item.stages.map((stage) => (
              <div
                key={stage.key}
                className="grid border-b last:border-b-0 sm:grid-cols-[104px_minmax(0,1fr)_minmax(0,1fr)]"
              >
                <div className="flex items-center bg-muted/35 px-3 py-2 text-xs font-medium text-muted-foreground sm:items-start sm:pt-3">
                  {stage.label}
                </div>
                <PromptStageCell label="提示词生成" node={stage.generation} waitingText="等待开始" />
                <PromptStageCell label="提示词审核" node={stage.review} waitingText="等待生成完成" />
              </div>
            ))}
          </div>
        </article>
      ))}
      {active && totalItems > items.length ? (
        <div className="flex items-center gap-2 px-1 py-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          其余 {totalItems - items.length} 个分镜等待调度
        </div>
      ) : null}
    </div>
  );
}

function PromptStageCell({ label, node, waitingText }: { label: string; node?: WorkflowNodeRun; waitingText: string }) {
  const status = node ? shotPromptNodeStatus(node) : "pending";
  const preview = node ? shotPromptNodePreview(node) : "";
  const hasOutput = node ? Object.keys(recordValue(node.output)).length > 0 : false;
  return (
    <div className="min-w-0 border-t px-3 py-2.5 first:border-t-0 sm:min-h-28 sm:border-l sm:border-t-0">
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <PromptStatusIcon status={status} />
          <span className="truncate text-xs font-medium">{label}</span>
        </div>
        <StatusBadge status={status} />
      </div>
      {node?.errorMessage ? (
        <div className="mt-2 line-clamp-3 text-xs leading-relaxed text-destructive">
          {localizePlatformError(node.errorMessage, node.errorCode)}
        </div>
      ) : preview ? (
        <div className="mt-2 line-clamp-3 text-xs leading-relaxed text-muted-foreground">{preview}</div>
      ) : (
        <div className="mt-2 text-xs text-muted-foreground">{waitingText}</div>
      )}
      {hasOutput ? (
        <details className="mt-2">
          <summary className="cursor-pointer text-xs text-muted-foreground">查看输出</summary>
          <pre className="mt-2 max-h-52 overflow-auto whitespace-pre-wrap break-words bg-muted px-2 py-1.5 text-xs leading-relaxed text-foreground">
            {jsonPreview(node?.output)}
          </pre>
        </details>
      ) : null}
    </div>
  );
}

function shotPromptNodePhase(node: WorkflowNodeRun): ShotPromptNodePhase | null {
  if (node.nodeType === "agent.image_prompt.generate" || node.nodeType === "agent.video_prompt.generate") return "generation";
  if (node.nodeType === "agent.image_prompt.review" || node.nodeType === "agent.video_prompt.review") return "review";
  if (node.nodeKey.startsWith("generate_shot_image_prompt_") || node.nodeKey.startsWith("generate_shot_video_prompt_")) return "generation";
  if (node.nodeKey.startsWith("review_shot_image_prompt_") || node.nodeKey.startsWith("review_shot_video_prompt_")) return "review";
  return null;
}

function shotPromptStageKey(node: WorkflowNodeRun) {
  return node.nodeKey
    .replace(/^generate_shot_image_prompt/, "shot_image_prompt")
    .replace(/^review_shot_image_prompt/, "shot_image_prompt")
    .replace(/^generate_shot_video_prompt/, "shot_video_prompt")
    .replace(/^review_shot_video_prompt/, "shot_video_prompt");
}

function shotPromptStageLabel(node: WorkflowNodeRun, input: Record<string, unknown>) {
  const anchorRole = stringValue(input.anchorRole);
  const anchorLabels: Record<string, string> = {
    planned_first_frame: "计划首帧",
    planned_last_frame: "计划尾帧",
    storyboard_sheet: "分镜板",
    storyboard_panel: "分镜画格",
  };
  const attempt = numberValue(input.attempt) || 1;
  const activityAttempt = numberValue(input.activityAttempt) || 1;
  const suffix = [
    activityAttempt > 1 ? `任务重试 ${activityAttempt}` : "",
    attempt > 1 ? `第 ${attempt} 轮修正` : "",
  ]
    .filter(Boolean)
    .join(" · ");
  if (anchorRole) return `${anchorLabels[anchorRole] || anchorRole}${suffix ? ` · ${suffix}` : ""}`;
  const segmentMatch = node.nodeKey.match(/_segment_(\d+)/);
  if (segmentMatch) return `视频片段 ${Number(segmentMatch[1]) + 1}${suffix ? ` · ${suffix}` : ""}`;
  const base = node.nodeType.includes("video_prompt") || node.nodeKey.includes("video_prompt") ? "视频提示词" : "计划画面";
  return `${base}${suffix ? ` · ${suffix}` : ""}`;
}

function shotPromptLogicalStageKey(node: WorkflowNodeRun, input: Record<string, unknown>) {
  const anchorRole = stringValue(input.anchorRole);
  if (anchorRole) return `${node.nodeType.includes("video_prompt") ? "video" : "image"}:${anchorRole}`;
  const segmentMatch = node.nodeKey.match(/_segment_(\d+)/);
  if (segmentMatch) return `video:segment:${segmentMatch[1]}`;
  return node.nodeType.includes("video_prompt") || node.nodeKey.includes("video_prompt") ? "video:prompt" : "image:prompt";
}

function compareShotPromptStages(left: ShotPromptStagePair, right: ShotPromptStagePair) {
  const order = ["计划首帧", "计划尾帧", "分镜板", "分镜画格", "视频提示词"];
  const leftIndex = order.indexOf(left.label);
  const rightIndex = order.indexOf(right.label);
  if (leftIndex >= 0 || rightIndex >= 0) {
    if (leftIndex < 0) return 1;
    if (rightIndex < 0) return -1;
    if (leftIndex !== rightIndex) return leftIndex - rightIndex;
  }
  if (left.logicalKey === right.logicalKey) {
    if (left.activityAttempt !== right.activityAttempt) return left.activityAttempt - right.activityAttempt;
    if (left.attempt !== right.attempt) return left.attempt - right.attempt;
  }
  return left.label.localeCompare(right.label, "zh-CN");
}

function shotPromptItemStatus(stages: ShotPromptStagePair[]) {
  const latestByLogicalKey = new Map<string, ShotPromptStagePair>();
  for (const stage of stages) {
    const current = latestByLogicalKey.get(stage.logicalKey);
    if (
      !current ||
      stage.activityAttempt > current.activityAttempt ||
      (stage.activityAttempt === current.activityAttempt && stage.attempt > current.attempt)
    ) {
      latestByLogicalKey.set(stage.logicalKey, stage);
    }
  }
  const latestStages = Array.from(latestByLogicalKey.values());
  const statuses = latestStages.flatMap((stage) => [stage.generation, stage.review]).filter(Boolean).map((node) => shotPromptNodeStatus(node));
  if (statuses.some((status) => status === "running" || status === "processing" || status === "queued")) return "running";
  if (statuses.some((status) => status === "failed")) return "failed";
  if (
    latestStages.length > 0 &&
    latestStages.every(
      (stage) => shotPromptNodeStatus(stage.generation) === "succeeded" && shotPromptNodeStatus(stage.review) === "succeeded",
    )
  ) {
    return "succeeded";
  }
  if (statuses.some((status) => status === "changes_requested")) return "changes_requested";
  return statuses.length > 0 ? "running" : "queued";
}

function shotPromptNodePreview(node: WorkflowNodeRun) {
  const output = recordValue(node.output);
  return firstNonEmptyString(
    stringValue(output.partialText),
    stringValue(output.reviewSummary),
    agentListPreview(output.issues),
    stringValue(output.finalPrompt),
    stringValue(output.prompt),
    stringValue(output.message),
    stringValue(output.summary),
  );
}

function PromptStatusIcon({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  if (normalized === "succeeded" || normalized === "completed") {
    return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-status-success" />;
  }
  if (normalized === "failed") {
    return <XCircle className="h-3.5 w-3.5 shrink-0 text-status-danger" />;
  }
  if (normalized === "running" || normalized === "processing") {
    return <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-status-running" />;
  }
  if (normalized === "partial_succeeded") {
    return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-status-warning" />;
  }
  if (normalized === "changes_requested") {
    return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-status-warning" />;
  }
  return <Clock3 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />;
}

function shotPromptNodeStatus(node?: WorkflowNodeRun) {
  if (!node) return "pending";
  const output = recordValue(node.output);
  if (stringValue(output.status) === "changes_requested" || output.approved === false) return "changes_requested";
  return node.status || "pending";
}

function agentListPreview(value: unknown) {
  if (!Array.isArray(value) || value.length === 0) return "";
  const first = value[0];
  if (typeof first === "string") return first.trim();
  const record = recordValue(first);
  return firstNonEmptyString(
    stringValue(record.message),
    stringValue(record.detail),
    stringValue(record.reason),
    stringValue(record.description),
  );
}

function recordValue(value: unknown): Record<string, unknown> {
  if (typeof value === "string") {
    try {
      return recordValue(JSON.parse(value) as unknown);
    } catch {
      return {};
    }
  }
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function firstNonEmptyString(...values: string[]) {
  return values.find(Boolean) || "";
}

function jsonPreview(value: unknown) {
  try {
    const text = JSON.stringify(value, null, 2);
    return text.length > 4000 ? `${text.slice(0, 4000)}...` : text;
  } catch {
    return String(value);
  }
}
