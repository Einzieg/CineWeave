import type { QueryKey } from "@tanstack/react-query";
import { qk } from "@/lib/query/keys";
import { workflowLabel } from "@/lib/routes";

/**
 * realtime SSE 为具名事件(event: xxx),EventSource 必须逐个 addEventListener,
 * 因此这里穷举后端 event_outbox 会写入的全部 event_type(源:internal/workflows、internal/api、internal/provider)。
 */
export const projectEventNames = [
  "artifact.created",
  "agent.step.blocked",
  "agent.step.completed",
  "agent.step.failed",
  "agent.step.started",
  "agent.task.blocked",
  "agent.task.continued",
  "agent.task.waiting_workflow",
  "asset.card.generated",
  "asset.card.updated",
  "asset.reference.created",
  "asset.updated",
  "media.compose.completed",
  "media.compose.failed",
  "project.export.completed",
  "project.export.failed",
  "project.review.completed",
  "provider.completed",
  "provider.webhook.received",
  "provider.video.task.cancelled",
  "provider.video.task.cancel_failed",
  "review.fix.applied",
  "script.generated",
  "script.scene.updated",
  "script.scenes.parsed",
  "shot_asset_requirement.updated",
  "storyboard.shot.cancelled",
  "storyboard.shot.created",
  "storyboard.shot.deleted",
  "storyboard.shot.image.completed",
  "storyboard.shot.image.failed",
  "storyboard.shot.image.started",
  "storyboard.shot.updated",
  "storyboard.shot.video.completed",
  "storyboard.shot.video.created",
  "storyboard.shot.video.failed",
  "storyboard.shots.created",
  "workflow.node.cancelled",
  "workflow.node.completed",
  "workflow.node.failed",
  "workflow.node.progress",
  "workflow.node.started",
  "workflow.run.cancelled",
  "workflow.run.cancelling",
  "workflow.run.cancel_warning",
  "workflow.run.completed",
  "workflow.run.failed",
  "workflow.run.queued",
] as const;

export type ProjectEventName = (typeof projectEventNames)[number];

/** 事件到期望失效的 query key(不含组织前缀)。 */
export function keysForProjectEvent(eventType: string, projectId: string, payload: Record<string, unknown> = {}): QueryKey[] {
  if (eventType.startsWith("agent.")) {
    const taskId = typeof payload.agentTaskId === "string" ? payload.agentTaskId : "";
    const sessionId = typeof payload.sessionId === "string" ? payload.sessionId : "";
    return [
      qk.agentTasks(projectId),
      ...(sessionId ? [qk.agentTasks(projectId, sessionId)] : []),
      qk.workflowRuns(projectId),
      qk.productionStatus(projectId),
      qk.shotProduction(projectId),
      qk.artifacts(projectId),
      ...(taskId ? [qk.agentTask(projectId, taskId)] : []),
    ];
  }
  if (eventType.startsWith("workflow.run.") || eventType.startsWith("workflow.node.")) {
    const workflowRunId = typeof payload.workflowRunId === "string" ? payload.workflowRunId : "";
    return [
      qk.workflowRuns(projectId),
      qk.productionStatus(projectId),
      qk.shotProduction(projectId),
      ...(workflowRunId ? [qk.workflowNodes(workflowRunId)] : []),
    ];
  }
  if (eventType.startsWith("storyboard.shot")) {
    return [qk.shotProduction(projectId), qk.productionStatus(projectId), qk.artifacts(projectId)];
  }
  switch (eventType) {
    case "artifact.created":
      return [qk.artifacts(projectId)];
    case "asset.card.generated":
    case "asset.card.updated":
    case "asset.reference.created":
    case "asset.updated":
      return [qk.assets(projectId), qk.productionStatus(projectId)];
    case "shot_asset_requirement.updated":
      return [qk.requirements(projectId), qk.productionStatus(projectId)];
    case "script.generated":
      return [qk.scripts(projectId), qk.productionStatus(projectId)];
    case "script.scene.updated":
    case "script.scenes.parsed":
      return [qk.scripts(projectId), qk.productionStatus(projectId)];
    case "media.compose.completed":
    case "media.compose.failed":
      return [qk.finalVideos(projectId), qk.timelines(projectId), qk.productionStatus(projectId), qk.artifacts(projectId)];
    case "project.export.completed":
    case "project.export.failed":
      return [qk.exports(projectId)];
    case "project.review.completed":
    case "review.fix.applied":
      return [qk.reviewRuns(projectId), qk.reviewItemsPrefix(projectId), qk.productionStatus(projectId)];
    case "provider.completed":
    case "provider.webhook.received":
    case "provider.video.task.cancelled":
    case "provider.video.task.cancel_failed":
      return [qk.shotProduction(projectId), qk.workflowRuns(projectId)];
    default:
      return [];
  }
}

type EventToast = { kind: "success" | "error"; text: string };

/** 需要弹 toast 的事件(仅新事件,重放不弹)。 */
export function toastForProjectEvent(eventType: string, payload: Record<string, unknown>): EventToast | null {
  const workflowType = typeof payload.workflowType === "string" ? payload.workflowType : "";
  const taskName = workflowType ? workflowLabel(workflowType) : "任务";
  switch (eventType) {
    case "workflow.run.completed":
      return { kind: "success", text: `${taskName}已完成` };
    case "workflow.run.failed":
      return { kind: "error", text: `${taskName}失败` };
    case "workflow.run.cancelled":
      return { kind: "success", text: `${taskName}已取消` };
    case "media.compose.completed":
      return { kind: "success", text: "最终成片合成完成" };
    case "media.compose.failed":
      return { kind: "error", text: "最终成片合成失败" };
    case "project.export.completed":
      return { kind: "success", text: "项目导出完成,可前往下载" };
    case "project.export.failed":
      return { kind: "error", text: "项目导出失败" };
    case "project.review.completed":
      return { kind: "success", text: "项目审阅完成" };
    default:
      return null;
  }
}
