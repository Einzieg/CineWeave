import { studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import type { ProductionStatus, StudioSession } from "@/lib/types";

export function nextProductionAction(status: ProductionStatus) {
  switch (status.overall.stage) {
    case "source":
      if (status.stages.source.status === "scenes_pending_parse") {
        return "parse_script_scenes";
      }
      if (status.stages.source.status === "scenes_pending_review") {
        return "";
      }
      if (status.stages.source.status === "events_pending_extraction") {
        return "extract_events";
      }
      if (status.stages.source.status === "events_pending_review") {
        return "";
      }
      if (status.stages.source.status === "adaptation_plan_pending") {
        return "generate_adaptation_plan";
      }
      if (status.stages.source.activeAdaptationPlanId && !status.stages.source.activeScriptId) {
        return "generate_script_from_plan";
      }
      return status.stages.source.novelSourceCount + status.stages.source.scriptSourceCount + status.stages.source.briefSourceCount > 0 ? "generate_script" : "";
    case "assets":
      return status.stages.assets.missingReferenceImageCount > 0 && status.stages.assets.pendingReviewCount === 0 ? "generate_asset_images" : "analyze_assets";
    case "storyboard":
      return "generate_storyboard";
    case "shot_assets":
      return status.stages.shotAssets.missingDerivedImageCount > 0 && status.stages.shotAssets.pendingReviewCount === 0 ? "generate_derived_asset_images" : "analyze_shot_assets";
    case "shot_images":
      return "generate_shot_images";
    case "shot_videos":
      return "generate_shot_videos";
    default:
      return "";
  }
}

export function productionActionLabel(action: string) {
  switch (action) {
    case "extract_events":
      return "提取事件";
    case "generate_adaptation_plan":
      return "生成改编计划";
    case "generate_script_from_plan":
      return "从计划生成剧本";
    case "generate_script":
      return "生成剧本";
    case "parse_script_scenes":
      return "解析分场";
    case "analyze_assets":
      return "分析资产";
    case "generate_asset_images":
      return "生成资产图像";
    case "generate_storyboard":
      return "生成分镜";
    case "analyze_shot_assets":
      return "分析镜头资产";
    case "generate_derived_asset_images":
      return "生成派生资产图像";
    case "generate_shot_images":
      return "生成镜头图片";
    case "generate_shot_videos":
      return "生成镜头视频";
    default:
      return action;
  }
}

export function productionRefreshKeys(projectId: string) {
  return [
    qk.project(projectId),
    qk.productionStatus(projectId),
    qk.workflowRuns(projectId),
    qk.assets(projectId),
    qk.requirements(projectId),
    qk.artifacts(projectId),
    qk.timelines(projectId),
    qk.finalVideos(projectId),
    qk.exports(projectId),
  ];
}

export function runProductionAction(session: StudioSession, projectId: string, action: string) {
  return studioApi.runProductionAction(session, projectId, {
    action,
    options: {
      maxShots: 3,
    },
  });
}
