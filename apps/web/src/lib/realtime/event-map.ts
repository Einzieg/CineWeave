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
  | "commerceProduct"
  | "commerceProject"
  | "commerceDirect"
  | "commerceDerivation"
  | "commerceScript"
  | "commerceStoryboard"
  | "export"
  | "final"
  | "project"
  | "projectDeletion"
  | "progress"
  | "provider"
  | "review"
  | "storyboard"
  | "videoExecution"
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
  "agent.task.workflow_recovery_planned": "agent",
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
  "commerce.final_video.activated": "final",
  "commerce.final_video.completed": "final",
  "commerce.language.confirmation_required": "commerceScript",
  "commerce.language.resolved": "commerceScript",
  "commerce.direct_video.cancelled": "commerceDirect",
  "commerce.direct_video.failed": "commerceDirect",
  "commerce.direct_video.progressed": "commerceDirect",
  "commerce.direct_video.started": "commerceDirect",
  "commerce.direct_video.succeeded": "commerceDirect",
  "commerce.script_derivation.batch.cancelled": "commerceDerivation",
  "commerce.script_derivation.batch.cancelling": "commerceDerivation",
  "commerce.script_derivation.batch.created": "commerceDerivation",
  "commerce.script_derivation.batch.failed": "commerceDerivation",
  "commerce.script_derivation.batch.partial_succeeded": "commerceDerivation",
  "commerce.script_derivation.batch.progressed": "commerceDerivation",
  "commerce.script_derivation.batch.started": "commerceDerivation",
  "commerce.script_derivation.batch.succeeded": "commerceDerivation",
  "commerce.script_derivation.item.cancelled": "commerceDerivation",
  "commerce.script_derivation.item.failed": "commerceDerivation",
  "commerce.script_derivation.item.reviewing": "commerceDerivation",
  "commerce.script_derivation.item.started": "commerceDerivation",
  "commerce.script_derivation.item.succeeded": "commerceDerivation",
  "commerce.product.reference.added": "commerceProduct",
  "commerce.product.reference.archived": "commerceProduct",
  "commerce.product.reference.updated": "commerceProduct",
  "commerce.product.updated": "commerceProduct",
  "commerce.product.version.activated": "commerceProduct",
  "commerce.product.version.created": "commerceProduct",
  "commerce.production.final_compose.completed": "final",
  "commerce.production.run.cancelled": "storyboard",
  "commerce.production.run.completed": "storyboard",
  "commerce.production.run.failed": "storyboard",
  "commerce.production.run.partially_succeeded": "storyboard",
  "commerce.production.video.completed": "storyboard",
  "commerce.production.video_prompt.completed": "storyboard",
  "commerce.project.defaults.updated": "commerceProject",
  "commerce.project_generation.activated": "commerceProject",
  "commerce.reference_pack.created": "commerceProduct",
  "commerce.script.localization.activated": "commerceScript",
  "commerce.script.localization.approved": "commerceScript",
  "commerce.script.localization.created": "commerceScript",
  "commerce.script.version.activated": "commerceScript",
  "commerce.script.version.created": "commerceScript",
  "commerce.script_reference.added": "commerceDirect",
  "commerce.script_reference.archived": "commerceDirect",
  "commerce.script_unit.archived": "commerceScript",
  "commerce.script_unit.created": "commerceScript",
  "commerce.script_unit.generation.archived": "commerceScript",
  "commerce.script_unit.generation.created": "commerceScript",
  "commerce.script_unit.reordered": "commerceScript",
  "commerce.script_unit.updated": "commerceScript",
  "commerce.setup.completed": "commerceProject",
  "commerce.shot.updated": "commerceStoryboard",
  "commerce.timeline.updated": "final",
  "commerce.shot.image_prompt.failed": "storyboard",
  "commerce.shot.image_prompt.succeeded": "storyboard",
  "commerce.shot.reference_image.failed": "storyboard",
  "commerce.shot.reference_image.succeeded": "storyboard",
  "commerce.shot.video.failed": "storyboard",
  "commerce.shot.video.succeeded": "storyboard",
  "commerce.shot.video_prompt.approved": "storyboard",
  "commerce.shot.video_prompt.failed": "storyboard",
  "commerce.storyboard.plan.activated": "commerceStoryboard",
  "commerce.storyboard.plan.cancelled": "commerceStoryboard",
  "commerce.storyboard.creative.generated": "commerceStoryboard",
  "commerce.storyboard.segmentation.previewed": "commerceStoryboard",
  "commerce.storyboard.segmentation.completed": "commerceStoryboard",
  "commerce.storyboard.strategy.selected": "commerceStoryboard",
  "commerce.storyboard.plan.committed": "commerceStoryboard",
  "commerce.storyboard.plan.completed": "commerceStoryboard",
  "commerce.storyboard.plan.failed": "commerceStoryboard",
  "commerce.storyboard.plan.started": "commerceStoryboard",
  "commerce.workflow_binding.created": "commerceProject",
  "derived_asset.batch.completed": "asset",
  "derived_asset.batch.created": "asset",
  "derived_asset.batch.reconciled": "asset",
  "derived_asset.item.discarded": "asset",
  "derived_asset.item.failed": "asset",
  "derived_asset.item.media_ready": "asset",
  "derived_asset.item.provider_succeeded": "asset",
  "derived_asset.item.started": "asset",
  "derived_asset.item.succeeded": "asset",
  "final_video.activated": "final",
  "media.compose.completed": "final",
  "media.compose.failed": "final",
  "novel.events.extracted": "content",
  "project.audio_configuration.invalidated": "project",
  "project.audio_settings.changed": "project",
  "project.deletion.business_data_started": "projectDeletion",
  "project.deletion.completed": "projectDeletion",
  "project.deletion.drain_timeout": "projectDeletion",
  "project.deletion.failed": "projectDeletion",
  "project.deletion.requested": "projectDeletion",
  "project.deletion.storage_progress": "projectDeletion",
  "project.deletion.storage_started": "projectDeletion",
  "project.deletion.tasks_cancelling": "projectDeletion",
  "project.export.completed": "export",
  "project.export.failed": "export",
  "project.frame_rate.changed": "project",
  "project.production_content.cleared": "project",
  "project.review.completed": "review",
  "project.video_ratio.changed": "project",
  "provider.video.task.cancel_failed": "provider",
  "provider.video.task.cancelled": "provider",
  "provider.model_capability.attested": "provider",
  "provider.model_capability.revoked": "provider",
  "provider.webhook.received": "provider",
  "review.fix.applied": "review",
  "script.archived": "content",
  "script.episode.generation.staged": "content",
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
  "storyboard.shot.anchor.completed": "storyboard",
  "storyboard.shot.anchor.failed": "storyboard",
  "storyboard.shot.anchor.reviewed": "storyboard",
  "storyboard.shot.anchor.started": "storyboard",
  "storyboard.shot.continuity.rejected": "storyboard",
  "storyboard.shot.segment_tail_anchor.extracted": "storyboard",
  "storyboard.shot.created": "storyboard",
  "storyboard.shot.deleted": "storyboard",
  "storyboard.shot.image.completed": "storyboard",
  "storyboard.shot.image.failed": "storyboard",
  "storyboard.shot.image.started": "storyboard",
  "storyboard.shot.image_prompt.failed": "storyboard",
  "storyboard.shot.image_prompt.reviewed": "storyboard",
  "storyboard.shot.image_prompt.running": "storyboard",
  "storyboard.shot.media.unlinked": "storyboard",
  "storyboard.shot.observed_exit.reviewed": "storyboard",
  "storyboard.shot.panel_manifest.compiled": "storyboard",
  "storyboard.shot.reference_pack.compiled": "storyboard",
  "storyboard.shot.render_plan.created": "storyboard",
  "storyboard.shot.render_plan.execution_cloned": "storyboard",
  "storyboard.shot.updated": "storyboard",
  "storyboard.shot.state.planned": "storyboard",
  "storyboard.shot.storyboard_sheet.cropped": "storyboard",
  "storyboard.shot.storyboard_sheet.reviewed": "storyboard",
  "storyboard.shot.transition.planned": "storyboard",
  "storyboard.shot.transition.updated": "storyboard",
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
  "video.production.binding.created": "project",
  "video.production.binding.superseded": "project",
  "video.production.blueprint.created": "storyboard",
  "video.production.generation.activated": "project",
  "video.production.generation.superseded": "project",
  "video.production.batch.failed": "videoExecution",
  "video.production.batch.partial_succeeded": "videoExecution",
  "video.production.batch.started": "videoExecution",
  "video.production.checkpoint.committed": "videoExecution",
  "video.production.checkpoint.reconciled": "videoExecution",
  "video.production.checkpoint.failed": "videoExecution",
  "video.production.item.cancelled": "videoExecution",
  "video.production.item.completed": "videoExecution",
  "video.production.item.failed": "videoExecution",
  "video.production.item.started": "videoExecution",
  "video.production.rebuild.completed": "project",
  "video.production.rebuild.failed": "project",
  "video.production.rebuild.item.completed": "storyboard",
  "video.production.rebuild.item.failed": "storyboard",
  "video.production.rebuild.item.started": "storyboard",
  "video.production.rebuild.partial": "project",
  "video.production.rebuild.requested": "project",
  "video.production.rebuild.started": "project",
  "video.production.rebuild.storyboard_required": "project",
  "video.render_plan.compiled": "storyboard",
  "video.render_plan.stale": "storyboard",
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
const terminalWorkflowEventNames = new Set<string>([
  "workflow.run.completed",
  "workflow.run.failed",
  "workflow.run.partial_succeeded",
  "workflow.run.cancelled",
]);

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
  const workflowType = stringPayload(payload, "workflowType");
  const taskId = stringPayload(payload, "agentTaskId");
  const sessionId = stringPayload(payload, "sessionId");
  const scriptEpisodeId = firstStringPayload(payload, "scriptEpisodeId", "episodeId");
  const shotId = firstStringPayload(payload, "storyboardShotId", "shotId");
  const assetId = firstStringPayload(payload, "assetId", "canonicalAssetId");
  const scriptId = stringPayload(payload, "scriptId");
  const providerModelId = stringPayload(payload, "providerModelId");
  const rebuildId = stringPayload(payload, "rebuildId");
  const commerceScriptUnitId = stringPayload(payload, "commerceScriptUnitId");
  const commerceScriptDerivationBatchId = stringPayload(payload, "batchId");
  const outputCommerceScriptUnitId = stringPayload(payload, "outputScriptUnitId");
  const commerceProductionRunId = stringPayload(payload, "commerceProductionRunId");
  const commerceStoryboardPlanId = stringPayload(payload, "commerceStoryboardPlanId");
  const commerceReferencePackId = stringPayload(payload, "referencePackId");
  const commerceSetupSessionId = stringPayload(payload, "setupSessionId");
  const commerceSetupRunId = stringPayload(payload, "setupRunId");
  const projectDeletionRequestId = stringPayload(payload, "projectDeletionRequestId");
  const keys: QueryKey[] = [];

  if (eventType.startsWith("commerce.shot.") || eventType.startsWith("commerce.production.")) {
    return uniqueQueryKeys([
      qk.workflowRuns(projectId),
      qk.artifacts(projectId),
      ...(workflowRunId ? [qk.workflowNodes(workflowRunId)] : []),
      ...(commerceScriptUnitId
        ? [
            qk.commerceScriptUnitsRoot(projectId),
            qk.commerceProjectProductionStatus(projectId),
            qk.commerceUnitProductionStatus(projectId, commerceScriptUnitId),
            qk.commerceStoryboardPlans(projectId, commerceScriptUnitId),
            qk.commerceProductionRuns(projectId, commerceScriptUnitId, "reference_images"),
            qk.commerceProductionRuns(projectId, commerceScriptUnitId, "video_prompts"),
            qk.commerceProductionRuns(projectId, commerceScriptUnitId, "shot_videos"),
            qk.commerceProductionRuns(projectId, commerceScriptUnitId, "final_compose"),
            qk.commerceTimelines(projectId, commerceScriptUnitId),
            qk.commerceFinalVideos(projectId, commerceScriptUnitId),
          ]
        : []),
      ...(commerceScriptUnitId && commerceStoryboardPlanId
        ? [qk.commerceStoryboardPlan(projectId, commerceScriptUnitId, commerceStoryboardPlanId)]
        : []),
      ...(commerceProductionRunId ? [qk.commerceProductionRun(projectId, commerceProductionRunId)] : []),
    ]);
  }

  if (eventType === "commerce.timeline.updated" || eventType.startsWith("commerce.final_video.")) {
    return uniqueQueryKeys([
      qk.commerceScriptUnitsRoot(projectId),
      qk.commerceProjectProductionStatus(projectId),
      ...(commerceScriptUnitId
        ? [
            qk.commerceUnitProductionStatus(projectId, commerceScriptUnitId),
            qk.commerceTimelines(projectId, commerceScriptUnitId),
            qk.commerceFinalVideos(projectId, commerceScriptUnitId),
          ]
        : []),
    ]);
  }

  if (eventType === "project.production_content.cleared") {
    return uniqueQueryKeys([
      qk.project(projectId),
      qk.productionStatus(projectId),
      qk.sources(projectId),
      qk.scripts(projectId),
      qk.scriptDetailsPrefix(projectId),
      qk.scriptVersionsPrefix(projectId),
      qk.scriptEpisodesPrefix(projectId),
      qk.scriptScenesPrefix(projectId),
      qk.assetsRoot(projectId),
      qk.requirements(projectId),
      qk.shotProductionPrefix(projectId),
      qk.artifacts(projectId),
      qk.timelines(projectId),
      qk.finalVideos(projectId),
      qk.exports(projectId),
      qk.reviewRuns(projectId),
      qk.reviewItemsPrefix(projectId),
      qk.workflowRuns(projectId),
    ]);
  }

  if ((eventType === "script.episode.generation.staged" || eventType === "script.episode.generated" || eventType === "script.episode.updated") && scriptId) {
    return uniqueQueryKeys([
      qk.scripts(projectId),
      qk.script(projectId, scriptId),
      qk.scriptVersions(projectId, scriptId),
      qk.scriptEpisodesForScriptPrefix(projectId, scriptId),
      qk.productionStatus(projectId),
    ]);
  }

  switch (handler) {
    case "commerceProduct":
      keys.push(
        qk.commerceProduct(projectId),
        qk.commerceProductVersions(projectId),
        qk.commerceProductReferencesRoot(projectId),
        qk.commerceProductReferencePacksRoot(projectId),
        ...(commerceReferencePackId ? [qk.commerceProductReferencePack(projectId, commerceReferencePackId)] : []),
      );
      if (eventType === "commerce.product.version.activated"
        || eventType === "commerce.reference_pack.created"
        || (eventType === "commerce.product.updated" && payload.activated === true)) {
        keys.push(qk.commerceScriptUnitsRoot(projectId), qk.commerceProjectProductionStatus(projectId));
      }
      break;
    case "commerceDirect":
      keys.push(
        qk.commerceScriptUnitsRoot(projectId),
        qk.commerceDirectVideosRoot(projectId),
        qk.workflowRuns(projectId),
      );
      if (commerceScriptUnitId) {
        keys.push(
          qk.commerceScriptUnit(projectId, commerceScriptUnitId),
          qk.commerceScriptReferencesRoot(projectId, commerceScriptUnitId),
          qk.commerceDirectVideos(projectId, commerceScriptUnitId),
        );
      }
      break;
    case "commerceDerivation":
      keys.push(
        qk.commerceScriptDerivationsRoot(projectId),
        qk.commerceScriptUnitsRoot(projectId),
        qk.workflowRuns(projectId),
        ...(commerceScriptDerivationBatchId
          ? [qk.commerceScriptDerivation(projectId, commerceScriptDerivationBatchId)]
          : []),
        ...(outputCommerceScriptUnitId
          ? [qk.commerceScriptUnit(projectId, outputCommerceScriptUnitId)]
          : []),
      );
      break;
    case "commerceProject":
      keys.push(
        qk.project(projectId),
        qk.commerceProduct(projectId),
        qk.commerceScriptUnitsRoot(projectId),
        qk.commerceProjectProductionStatus(projectId),
        qk.workflowRuns(projectId),
        ...(commerceSetupSessionId ? [qk.commerceSetupSession(projectId, commerceSetupSessionId)] : []),
        ...(commerceSetupRunId ? [qk.commerceSetupRun(projectId, commerceSetupRunId)] : []),
      );
      break;
    case "commerceScript": {
      const listChanged = eventType.startsWith("commerce.script_unit.")
        || eventType === "commerce.script.version.activated"
        || eventType === "commerce.script.localization.activated";
      if (listChanged) keys.push(qk.commerceScriptUnitsRoot(projectId));
      if (commerceScriptUnitId) {
        keys.push(
          qk.commerceScriptUnit(projectId, commerceScriptUnitId),
          qk.commerceScriptVersions(projectId, commerceScriptUnitId),
          qk.commerceLanguageResolution(projectId, commerceScriptUnitId),
          qk.commerceLocalizations(projectId, commerceScriptUnitId),
          qk.commerceScriptUnitRebuild(projectId, commerceScriptUnitId),
          qk.commerceStoryboardPlansRoot(projectId, commerceScriptUnitId),
          qk.commerceUnitProductionStatus(projectId, commerceScriptUnitId),
        );
      }
      if (eventType.startsWith("commerce.script_unit.generation.")) {
        keys.push(qk.commerceProjectProductionStatus(projectId), qk.workflowRuns(projectId));
      }
      break;
    }
    case "commerceStoryboard":
      keys.push(qk.workflowRuns(projectId));
      if (commerceScriptUnitId) {
        keys.push(
          qk.commerceStoryboardPlansRoot(projectId, commerceScriptUnitId),
          qk.commerceUnitProductionStatus(projectId, commerceScriptUnitId),
          qk.commerceProjectProductionStatus(projectId),
          qk.commerceProductionRunsRoot(projectId),
          ...(commerceStoryboardPlanId
            ? [qk.commerceStoryboardPlan(projectId, commerceScriptUnitId, commerceStoryboardPlanId)]
            : []),
        );
      }
      break;
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
      if (terminalWorkflowEventNames.has(eventType)) {
        keys.push(...keysForTerminalWorkflowRun(projectId, workflowType, payload));
      }
      break;
    case "progress":
      if (workflowRunId) keys.push(qk.workflowNodes(workflowRunId));
      break;
    case "artifact":
      keys.push(qk.artifacts(projectId));
      break;
    case "asset":
      keys.push(
        qk.assetsRoot(projectId),
        qk.requirements(projectId),
        qk.artifacts(projectId),
        qk.productionStatus(projectId),
        qk.workflowRuns(projectId),
        ...(workflowRunId ? [qk.workflowDerivedAssetBatch(workflowRunId)] : []),
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
          ? [
              qk.shotDetail(projectId, shotId),
              qk.shotState(projectId, shotId),
              qk.shotTransition(projectId, shotId),
              qk.shotAnchors(projectId, shotId),
              qk.shotReferencePack(projectId, shotId, "anchor"),
              qk.shotReferencePack(projectId, shotId, "video"),
              qk.shotStoryboardSheet(projectId, shotId),
              qk.shotVideoPromptPlan(projectId, shotId),
              qk.shotRenderPlan(projectId, shotId),
              qk.nativeAudioReviews(projectId, shotId),
            ]
          : []),
      );
      break;
    case "videoExecution": {
      if (workflowRunId) keys.push(qk.workflowVideoProduction(workflowRunId));
      const itemTerminal = eventType === "video.production.item.completed"
        || eventType === "video.production.item.failed"
        || eventType === "video.production.item.cancelled";
      const aggregateChanged = eventType !== "video.production.item.started";
      if (aggregateChanged) keys.push(qk.workflowRuns(projectId));
      if (itemTerminal && shotId) {
        keys.push(
          qk.shotDetail(projectId, shotId),
          qk.shotRenderPlan(projectId, shotId),
          qk.shotVideoPromptPlan(projectId, shotId),
        );
      }
      break;
    }
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
      if (providerModelId) keys.push(qk.providerModelVideoCapabilities(providerModelId));
      break;
    case "project":
      keys.push(
        qk.project(projectId),
        qk.projectManualBindings(projectId),
        qk.projectVideoProductionProfile(projectId),
        qk.shotProductionPrefix(projectId),
        qk.workflowRuns(projectId),
        qk.productionStatus(projectId),
      );
      if (rebuildId) {
        keys.push(
          qk.projectVideoProductionRebuild(projectId, rebuildId),
          qk.projectVideoProductionRebuildItems(projectId, rebuildId),
        );
      }
      break;
    case "projectDeletion":
      keys.push(
        qk.projects(),
        ...(projectDeletionRequestId
          ? [qk.projectDeletionRequest(projectId, projectDeletionRequestId)]
          : []),
      );
      break;
  }
  return uniqueQueryKeys(keys);
}

export function keysForTerminalWorkflowRun(
  projectId: string,
  workflowType: string,
  payload: Record<string, unknown> = {},
): QueryKey[] {
  const normalizedWorkflowType = workflowType.trim().toLowerCase();
  if (normalizedWorkflowType !== "batch_generate_asset_cards" && normalizedWorkflowType !== "batch_generate_asset_images") {
    return [];
  }

  const keys: QueryKey[] = [
    qk.assetsRoot(projectId),
    qk.requirements(projectId),
    qk.shotProductionPrefix(projectId),
    qk.productionStatus(projectId),
  ];
  if (normalizedWorkflowType === "batch_generate_asset_images") {
    keys.push(qk.artifacts(projectId));
    for (const assetId of workflowAssetIds(payload)) {
      keys.push(qk.assetReferences(projectId, assetId));
    }
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

function workflowAssetIds(payload: Record<string, unknown>): string[] {
  const items = Array.isArray(payload.items) ? payload.items : [];
  const ids = new Set<string>();
  for (const item of items) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const assetId = stringPayload(item as Record<string, unknown>, "assetId");
    if (assetId) ids.add(assetId);
  }
  return [...ids];
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
    case "project.production_content.cleared":
      return { kind: "success", text: "已保留小说原文并清空生产内容" };
    case "video.production.rebuild.partial":
      return { kind: "success", text: "视频生产方案重建部分完成" };
    case "video.production.rebuild.storyboard_required":
      return { kind: "error", text: "视频生产方案重建后仍有分集待处理" };
    case "video.production.rebuild.completed":
      return { kind: "success", text: "视频生产方案重建完成" };
    case "video.production.rebuild.failed":
      return { kind: "error", text: "视频生产方案重建失败" };
    case "storyboard.shot.anchor.failed":
      return { kind: "error", text: "镜头首帧生成失败" };
    case "storyboard.shot.continuity.rejected":
      return { kind: "error", text: "镜头连续性审核未通过" };
    default:
      return null;
  }
}
