import {
  Boxes,
  FileCode2,
  Download,
  ClipboardCheck,
  FolderKanban,
  KeyRound,
  Library,
  ListChecks,
  Settings2,
  ShieldCheck,
  Workflow,
  Clapperboard,
  FileText,
  Film,
  PlaySquare,
} from "lucide-react";

export const globalNavItems = [
  { label: "项目", href: "/projects", icon: FolderKanban, section: "projects" },
  { label: "供应商中心", href: "/providers", icon: KeyRound, section: "providers" },
  { label: "提示词中心", href: "/prompts", icon: FileCode2, section: "prompts" },
  { label: "权限管理", href: "/access", icon: ShieldCheck, section: "access" },
  { label: "设置", href: "/settings", icon: Settings2, section: "settings" },
] as const;

export const projectNavItems = [
  { label: "项目概览", segment: "", icon: FolderKanban },
  { label: "内容", segment: "content", icon: FileText },
  { label: "剧本", segment: "scripts", icon: FileCode2 },
  { label: "资产", segment: "assets", icon: Boxes },
  { label: "分镜", segment: "storyboard", icon: Clapperboard },
  { label: "视频", segment: "video", icon: PlaySquare },
  { label: "成片", segment: "final", icon: Film },
] as const;

export const projectAdvancedNavItems = [
  { label: "生产看板", segment: "production", icon: ListChecks },
  { label: "审阅", segment: "review", icon: ClipboardCheck },
  { label: "工作流", segment: "workflows", icon: Workflow },
  { label: "素材库", segment: "vault", icon: Library },
  { label: "时间线", segment: "timeline", icon: Film },
  { label: "导出", segment: "export", icon: Download },
  { label: "项目设置", segment: "settings", icon: Settings2 },
] as const;

export type GlobalSection = "dashboard" | (typeof globalNavItems)[number]["section"];
export type ProjectSection = (typeof projectNavItems)[number]["segment"] | (typeof projectAdvancedNavItems)[number]["segment"];

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
    default:
      return value;
  }
}
