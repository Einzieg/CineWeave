import type { QueryKey } from "@tanstack/react-query";
import { qk } from "@/lib/query/keys";
import { workflowLabel } from "@/lib/routes";
import {
  projectEventNames,
  type ProjectEventName,
} from "@/lib/realtime/generated-events";

export { projectEventNames, type ProjectEventName };

type InvalidationHandler =
  | "agent"
  | "artifact"
  | "asset"
  | "audio"
  | "content"
  | "export"
  | "final"
  | "project"
  | "progress"
  | "provider"
  | "review"
  | "storyboard"
  | "workflow";

// This map intentionally lives outside the generated catalog. Adding a new
// backend event requires an explicit frontend invalidation decision, and the
// satisfies clause makes an omission a TypeScript error.
export const projectEventInvalidation = {
  "agent.asset.prompt_revised": "agent",
  "agent.step.blocked": "agent",
  "agent.step.completed": "agent",
  "agent.step.failed": "agent",
  "agent.step.started": "agent",
  "agent.storyboard.reordered": "agent",
  "agent.target.updated": "agent",
  "agent.task.blocked": "agent",
  "agent.task.child_workflow_failed": "agent",
  "agent.task.continued": "agent",
  "agent.task.question_continued": "agent",
  "agent.task.waiting_workflow": "agent",
  "artifact.created": "artifact",
  "asset.batch.image.completed": "asset",
  "asset.batch.prompt.completed": "asset",
  "asset.card.generated": "asset",
  "asset.card.updated": "asset",
  "asset.reference.archived": "asset",
  "asset.reference.created": "asset",
  "asset.reference.primary_set": "asset",
  "asset.updated": "asset",
  "audio.mix.completed": "audio",
  "audio.mix.discarded": "audio",
  "audio.mix.started": "audio",
  "audio.tts.clip.discarded": "audio",
  "audio.tts.clip.generated": "audio",
  "audio.tts.prepared": "audio",
  "audio.tts.timing_revision.created": "audio",
  "canonical_asset.archived": "asset",
  "final_video.activated": "final",
  "media.compose.completed": "final",
  "media.compose.failed": "final",
  "novel.events.extracted": "content",
  "project.audio_configuration.invalidated": "project",
  "project.audio_settings.changed": "project",
  "project.export.completed": "export",
  "project.export.failed": "export",
  "project.frame_rate.changed": "project",
  "project.review.completed": "review",
  "project.video_ratio.changed": "project",
  "provider.video.task.cancel_failed": "provider",
  "provider.video.task.cancelled": "provider",
  "provider.webhook.received": "provider",
  "review.fix.applied": "review",
  "script.archived": "content",
  "script.episode.generated": "content",
  "script.episode.updated": "content",
  "script.generated": "content",
  "script.generation.prepared": "content",
  "script.scene.archived": "content",
  "script.scene.regenerated": "content",
  "script.scene.reviewed": "content",
  "script.scene.updated": "content",
  "script.scenes.parsed": "content",
  "script.version.activated": "content",
  "script.version.archived": "content",
  "shot_asset_requirement.derived_image.generated": "asset",
  "shot_asset_requirement.skipped": "asset",
  "shot_asset_requirement.updated": "asset",
  "source.archived": "content",
  "source.chapter.deleted": "content",
  "source.updated.downstream_stale": "content",
  "storyboard.audio.review.completed": "storyboard",
  "storyboard.audio.review.discarded": "storyboard",
  "storyboard.audio.review.prepared": "storyboard",
  "storyboard.audio.verification.completed": "storyboard",
  "storyboard.blueprint.completed": "storyboard",
  "storyboard.episode.superseded": "storyboard",
  "storyboard.plan.activated": "storyboard",
  "storyboard.plan.ready": "storyboard",
  "storyboard.plan.review.changes_requested": "storyboard",
  "storyboard.plan.reviewing": "storyboard",
  "storyboard.plan.revision.ready": "storyboard",
  "storyboard.scene.planning.completed": "storyboard",
  "storyboard.scene.planning.failed": "storyboard",
  "storyboard.scene.planning.started": "storyboard",
  "storyboard.segment.cancelled": "storyboard",
  "storyboard.segment.failed": "storyboard",
  "storyboard.segment.media.processed": "storyboard",
  "storyboard.segment.planned": "storyboard",
  "storyboard.segment.prompt.failed": "storyboard",
  "storyboard.segment.prompt.reviewed": "storyboard",
  "storyboard.segment.prompt.running": "storyboard",
  "storyboard.segment.queued": "storyboard",
  "storyboard.segment.retry_planned": "storyboard",
  "storyboard.segment.running": "progress",
  "storyboard.segment.succeeded": "storyboard",
  "storyboard.shot.cancelled": "storyboard",
  "storyboard.shot.continuity_frame.extracted": "storyboard",
  "storyboard.shot.created": "storyboard",
  "storyboard.shot.deleted": "storyboard",
  "storyboard.shot.image.completed": "storyboard",
  "storyboard.shot.image.failed": "storyboard",
  "storyboard.shot.image.started": "storyboard",
  "storyboard.shot.image_prompt.failed": "storyboard",
  "storyboard.shot.image_prompt.reviewed": "storyboard",
  "storyboard.shot.image_prompt.running": "storyboard",
  "storyboard.shot.media.unlinked": "storyboard",
  "storyboard.shot.render_plan.created": "storyboard",
  "storyboard.shot.updated": "storyboard",
  "storyboard.shot.video.completed": "storyboard",
  "storyboard.shot.video.composed": "storyboard",
  "storyboard.shot.video.created": "storyboard",
  "storyboard.shot.video.failed": "storyboard",
  "storyboard.shot.video.polled": "progress",
  "storyboard.shot.video.segment_failed": "storyboard",
  "storyboard.shot.video.stale": "storyboard",
  "storyboard.shot.video_prompt.context_changed": "storyboard",
  "storyboard.shot.video_prompt.failed": "storyboard",
  "storyboard.shot.video_prompt.plan_ready": "storyboard",
  "storyboard.shot.video_prompt.reviewed": "storyboard",
  "storyboard.shot.video_prompt.running": "storyboard",
  "storyboard.shot.video_prompt.segmentation_required": "storyboard",
  "storyboard.shots.created": "storyboard",
  "storyboard.shots.reordered": "storyboard",
  "storyboard.timing.calibration.updated": "storyboard",
  "storyboard.timing.completed": "storyboard",
  "storyboard.timing.reused": "storyboard",
  "storyboard.timing.started": "storyboard",
  "video.production.blueprint.created": "storyboard",
  "workflow.node.cancelled": "workflow",
  "workflow.node.completed": "workflow",
  "workflow.node.failed": "workflow",
  "workflow.node.progress": "progress",
  "workflow.node.started": "workflow",
  "workflow.result.discarded": "workflow",
  "workflow.run.cancel_warning": "workflow",
  "workflow.run.cancelled": "workflow",
  "workflow.run.cancelling": "workflow",
  "workflow.run.completed": "workflow",
  "workflow.run.failed": "workflow",
  "workflow.run.partial_succeeded": "workflow",
  "workflow.run.queued": "workflow",
  "workflow.run.started": "workflow",
} as const satisfies Record<ProjectEventName, InvalidationHandler>;

const knownProjectEventNames = new Set<string>(projectEventNames);

export function keysForProjectEvent(
  eventType: string,
  projectId: string,
  payload: Record<string, unknown> = {},
): QueryKey[] {
  if (!knownProjectEventNames.has(eventType)) {
    return [];
  }
  const handler = projectEventInvalidation[eventType as ProjectEventName];
  const workflowRunId = stringPayload(payload, "workflowRunId");
  const taskId = stringPayload(payload, "agentTaskId");
  const sessionId = stringPayload(payload, "sessionId");
  const scriptEpisodeId = firstStringPayload(payload, "scriptEpisodeId", "episodeId");
  const shotId = firstStringPayload(payload, "storyboardShotId", "shotId");
  const assetId = firstStringPayload(payload, "assetId", "canonicalAssetId");
  const scriptId = stringPayload(payload, "scriptId");
  const keys: QueryKey[] = [];

  if ((eventType === "script.episode.generated" || eventType === "script.episode.updated") && scriptId) {
    return uniqueQueryKeys([
      qk.scripts(projectId),
      qk.script(projectId, scriptId),
      qk.scriptVersions(projectId, scriptId),
      qk.scriptEpisodesForScriptPrefix(projectId, scriptId),
      qk.productionStatus(projectId),
    ]);
  }

  switch (handler) {
    case "agent":
      keys.push(
        qk.agentTasks(projectId),
        ...(sessionId ? [qk.agentTasks(projectId, sessionId)] : []),
        ...(taskId ? [qk.agentTask(projectId, taskId)] : []),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
      );
      break;
    case "workflow":
      keys.push(
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
        ...(workflowRunId ? [qk.workflowNodes(workflowRunId)] : []),
      );
      break;
    case "progress":
      if (workflowRunId) keys.push(qk.workflowNodes(workflowRunId));
      break;
    case "artifact":
      keys.push(qk.artifacts(projectId));
      break;
    case "asset":
      keys.push(
        qk.assets(projectId),
        qk.requirements(projectId),
        qk.artifacts(projectId),
        qk.productionStatus(projectId),
        ...(assetId ? [qk.assetReferences(projectId, assetId)] : []),
      );
      break;
    case "audio":
      keys.push(
        qk.productionStatus(projectId),
        qk.workflowRuns(projectId),
        ...(scriptEpisodeId
          ? [qk.episodeAudio(projectId, scriptEpisodeId), qk.scriptEpisodeTiming(projectId, scriptEpisodeId)]
          : []),
      );
      break;
    case "content":
      keys.push(
        qk.sources(projectId),
        qk.scripts(projectId),
        qk.scriptDetailsPrefix(projectId),
        qk.scriptVersionsPrefix(projectId),
        qk.scriptEpisodesPrefix(projectId),
        qk.scriptScenesPrefix(projectId),
        qk.productionStatus(projectId),
      );
      break;
    case "storyboard":
      keys.push(
        qk.shotProductionPrefix(projectId),
        qk.productionStatus(projectId),
        qk.workflowRuns(projectId),
        qk.artifacts(projectId),
        ...(shotId
          ? [qk.shotDetail(projectId, shotId), qk.shotRenderPlan(projectId, shotId), qk.nativeAudioReviews(projectId, shotId)]
          : []),
      );
      break;
    case "final":
      keys.push(qk.finalVideos(projectId), qk.timelines(projectId), qk.productionStatus(projectId), qk.artifacts(projectId));
      break;
    case "export":
      keys.push(qk.exports(projectId), qk.finalVideos(projectId));
      break;
    case "review":
      keys.push(qk.reviewRuns(projectId), qk.reviewItemsPrefix(projectId), qk.productionStatus(projectId));
      break;
    case "provider":
      keys.push(qk.shotProductionPrefix(projectId), qk.workflowRuns(projectId), qk.productionStatus(projectId));
      break;
    case "project":
      keys.push(qk.project(projectId), qk.projectManualBindings(projectId), qk.productionStatus(projectId));
      break;
  }
  return uniqueQueryKeys(keys);
}

function stringPayload(payload: Record<string, unknown>, key: string): string {
  return typeof payload[key] === "string" ? payload[key] : "";
}

function firstStringPayload(payload: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = stringPayload(payload, key);
    if (value) return value;
  }
  return "";
}

function uniqueQueryKeys(keys: QueryKey[]): QueryKey[] {
  const seen = new Set<string>();
  return keys.filter((key) => {
    const serialized = JSON.stringify(key);
    if (seen.has(serialized)) return false;
    seen.add(serialized);
    return true;
  });
}

type EventToast = { kind: "success" | "error"; text: string };

export function toastForProjectEvent(eventType: string, payload: Record<string, unknown>): EventToast | null {
  const workflowType = typeof payload.workflowType === "string" ? payload.workflowType : "";
  const taskName = workflowType ? workflowLabel(workflowType) : "任务";
  switch (eventType) {
    case "workflow.run.completed":
      return { kind: "success", text: `${taskName}已完成` };
    case "workflow.run.failed":
      return { kind: "error", text: `${taskName}失败` };
    case "workflow.run.partial_succeeded":
      return { kind: "success", text: `${taskName}部分完成` };
    case "workflow.run.cancelled":
      return { kind: "success", text: `${taskName}已取消` };
    case "media.compose.completed":
      return { kind: "success", text: "最终成片合成完成" };
    case "media.compose.failed":
      return { kind: "error", text: "最终成片合成失败" };
    case "project.export.completed":
      return { kind: "success", text: "项目导出完成，可前往下载" };
    case "project.export.failed":
      return { kind: "error", text: "项目导出失败" };
    case "project.review.completed":
      return { kind: "success", text: "项目审阅完成" };
    default:
      return null;
  }
}
