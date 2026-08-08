import {
  Activity,
  Boxes,
  FileCode2,
  FolderKanban,
  KeyRound,
  Landmark,
  Settings2,
  ShieldCheck,
  Clapperboard,
  FileText,
  Film,
  PlaySquare,
  ShoppingBag,
} from "lucide-react";
import { editionEntry } from "@cineweave/edition-entry";
import type { ProjectKind } from "./types";

const coreGlobalNavItems = [
  { label: "项目", href: "/projects", icon: FolderKanban, section: "projects", systemOnly: false },
  { label: "供应商中心", href: "/providers", icon: KeyRound, section: "providers", systemOnly: false },
  { label: "提示词中心", href: "/prompts", icon: FileCode2, section: "prompts", systemOnly: false },
  { label: "组织与权限", href: "/access", icon: ShieldCheck, section: "access", systemOnly: false },
  { label: "系统组织", href: "/system/organizations", icon: Landmark, section: "system-organizations", systemOnly: true },
  { label: "控制诊断", href: "/system/project-control", icon: Activity, section: "system-project-control", systemOnly: true },
  { label: "设置", href: "/settings", icon: Settings2, section: "settings", systemOnly: false },
] as const;

export const globalNavItems = [
  ...coreGlobalNavItems,
  ...editionEntry.navigation,
] as const;

export const narrativeProjectNavItems = [
  { label: "项目概览", segment: "", icon: FolderKanban },
  { label: "内容", segment: "content", icon: FileText },
  { label: "剧本", segment: "scripts", icon: FileCode2 },
  { label: "资产", segment: "assets", icon: Boxes },
  { label: "分镜", segment: "storyboard", icon: Clapperboard },
  { label: "视频", segment: "video", icon: PlaySquare },
  { label: "成片", segment: "final", icon: Film },
  { label: "项目设置", segment: "settings", icon: Settings2 },
] as const;

export const commerceProjectNavItems = [
  { label: "商品配置", segment: "commerce/materials", icon: ShoppingBag },
  { label: "视频生成", segment: "commerce/video", icon: PlaySquare },
  { label: "项目设置", segment: "settings", icon: Settings2 },
] as const;

// 保留现有导出，避免叙事页面的静态引用被业务导航分流影响。
export const projectNavItems = narrativeProjectNavItems;

export type GlobalSection = "dashboard" | (typeof globalNavItems)[number]["section"];
export type ProjectSection =
  | (typeof narrativeProjectNavItems)[number]["segment"]
  | (typeof commerceProjectNavItems)[number]["segment"];

export function projectNavItemsForKind(projectKind: ProjectKind | undefined) {
  return projectKind === "commerce_video" ? commerceProjectNavItems : narrativeProjectNavItems;
}

export function projectHref(projectId: string, segment = "") {
  return segment ? `/projects/${projectId}/${segment}` : `/projects/${projectId}`;
}

export function isProjectNavActive(currentSegment: string, itemSegment: ProjectSection) {
  if (currentSegment === itemSegment) {
    return true;
  }
  if (currentSegment === "sources") {
    return itemSegment === "content";
  }
  if (currentSegment === "timeline" || currentSegment === "export") {
    return itemSegment === "final";
  }
  return false;
}

export function workflowLabel(value: string) {
  switch (value) {
    case "extract_novel_events":
      return "提取小说事件";
    case "generate_adaptation_plan":
      return "生成改编计划";
    case "adaptation_plan_to_script":
      return "改编计划生成剧本";
    case "source_to_script":
      return "从原文生成剧本";
    case "script_to_assets":
      return "分析剧本资产";
    case "script_to_storyboard":
      return "生成分镜";
    case "batch_generate_derived_asset_images":
      return "批量生成镜头衍生资产";
    case "script_to_video":
      return "剧本生成视频";
    case "full_production":
      return "完整生产";
    case "video_production":
      return "兼容视频生产";
    case "text_to_storyboard":
      return "文本生成分镜";
    case "regenerate_canonical_asset_image":
      return "重新生成资产参考图";
    case "regenerate_derived_asset_image":
      return "重新生成派生资产图";
    case "regenerate_shot_image":
      return "重新生成镜头图片";
    case "regenerate_shot_video":
      return "重新生成镜头视频";
    case "regenerate_final_video":
      return "重新合成最终成片";
    case "batch_generate_shot_images":
      return "批量生成镜头图片";
    case "batch_generate_shot_image_prompts":
      return "批量生成镜头图片提示词";
    case "batch_generate_shot_video_prompts":
      return "批量生成镜头视频提示词";
    case "batch_generate_shot_videos":
      return "批量生成镜头视频";
    case "batch_generate_asset_cards":
      return "批量生成资产提示词";
    case "batch_generate_asset_images":
      return "批量生成资产图片";
    case "batch_cancel_shot_videos":
      return "批量取消镜头视频";
    case "compose_timeline":
      return "时间线合成成片";
    case "export_project":
      return "项目导出";
    case "commerce_project_setup":
      return "初始化带货项目";
    case "commerce_direct_video":
      return "生成带货视频";
    case "commerce_script_unit_preparation":
      return "准备广告脚本";
    case "commerce_script_organization":
      return "整理广告脚本";
    case "commerce_storyboard_planning":
      return "生成带货分镜";
    case "commerce_reference_image_prompts":
      return "生成商品分镜提示词";
    case "commerce_reference_images":
      return "生成商品分镜参考图";
    case "commerce_video_prompts":
      return "生成带货视频提示词";
    case "commerce_shot_videos":
      return "生成带货镜头视频";
    case "commerce_final_compose":
      return "合成带货成片";
    case "commerce_script_unit_batch_coordinator":
      return "批量处理广告脚本";
    case "commerce_reference_image_batch":
      return "批量生成商品分镜参考图";
    case "commerce_video_prompt_batch":
      return "批量生成带货视频提示词";
    case "commerce_shot_video_batch":
      return "批量生成带货镜头视频";
    default:
      return value;
  }
}
