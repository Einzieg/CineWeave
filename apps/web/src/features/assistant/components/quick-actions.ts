"use client";

import type { LucideIcon } from "lucide-react";
import {
  ClipboardCheck,
  FilePlus2,
  Files,
  Film,
  Image,
  Layers3,
  ListVideo,
  PackageSearch,
  PencilLine,
  SearchCheck,
  Sparkles,
  SquareStack,
  Video,
  XCircle,
} from "lucide-react";
import type { ProjectKind } from "@/lib/types";

export type AssistantQuickAction = {
  id: string;
  label: string;
  description: string;
  goal: string;
  mode: "plan_only" | "supervised";
  icon: LucideIcon;
  keywords: string[];
};

export const NARRATIVE_ASSISTANT_QUICK_ACTIONS: AssistantQuickAction[] = [
  {
    id: "inspect-project",
    label: "检查项目",
    description: "总结成片缺口和下一步",
    goal: "总结这个项目离成片还差什么，列出缺口和下一步建议。",
    mode: "plan_only",
    icon: SearchCheck,
    keywords: ["inspect", "status", "project", "检查", "项目", "缺口"],
  },
  {
    id: "review-fixes",
    label: "修复建议",
    description: "检查项目问题并生成建议",
    goal: "检查项目问题并生成修复建议，不要直接修改项目。",
    mode: "supervised",
    icon: ClipboardCheck,
    keywords: ["review", "fix", "建议", "修复", "审阅"],
  },
  {
    id: "missing-images",
    label: "缺失图片",
    description: "只生成缺失镜头图片",
    goal: "只生成缺失镜头图片，不要生成视频。",
    mode: "supervised",
    icon: Image,
    keywords: ["image", "images", "图片", "镜头图", "缺失"],
  },
  {
    id: "missing-videos",
    label: "缺失视频",
    description: "生成缺失镜头视频",
    goal: "生成缺失镜头视频，先检查镜头图片是否已存在。",
    mode: "supervised",
    icon: Film,
    keywords: ["video", "videos", "视频", "镜头视频", "缺失"],
  },
  {
    id: "storyboard",
    label: "生成分镜",
    description: "从当前剧本生成分镜计划",
    goal: "从当前剧本生成分镜计划，缺少剧本时先读取项目状态。",
    mode: "supervised",
    icon: SquareStack,
    keywords: ["storyboard", "shot", "分镜", "镜头"],
  },
  {
    id: "final-preview",
    label: "成片预览",
    description: "检查时间线并生成预览",
    goal: "生成最终预览成片，先检查时间线、镜头视频和成本风险。",
    mode: "supervised",
    icon: Layers3,
    keywords: ["final", "preview", "export", "成片", "预览", "时间线"],
  },
  {
    id: "cancel-running",
    label: "取消任务",
    description: "取消当前项目运行中任务",
    goal: "取消当前项目正在运行的生产任务。",
    mode: "supervised",
    icon: XCircle,
    keywords: ["cancel", "stop", "取消", "停止", "任务"],
  },
];

export const COMMERCE_ASSISTANT_QUICK_ACTIONS: AssistantQuickAction[] = [
  {
    id: "commerce-product",
    label: "查看商品",
    description: "读取商品事实与参考图",
    goal: "读取当前商品配置和活动参考图，简要总结商品事实与可用参考图。",
    mode: "plan_only",
    icon: PackageSearch,
    keywords: ["product", "商品", "参考图"],
  },
  {
    id: "commerce-scripts",
    label: "列出脚本",
    description: "按稳定顺序列出广告脚本",
    goal: "按稳定顺序列出当前项目的活动广告脚本。",
    mode: "plan_only",
    icon: Files,
    keywords: ["script", "scripts", "脚本", "列表"],
  },
  {
    id: "commerce-create-script",
    label: "新增脚本",
    description: "创建独立广告脚本",
    goal: "帮我创建一条新的独立广告脚本；缺少标题、正文或目标时长时先询问我。",
    mode: "supervised",
    icon: FilePlus2,
    keywords: ["create", "script", "新增", "创建", "脚本"],
  },
  {
    id: "commerce-update-script",
    label: "修改脚本",
    description: "修改指定脚本当前正文",
    goal: "帮我修改一条广告脚本；如果无法唯一确定脚本或修改要求，先让我选择或补充。",
    mode: "supervised",
    icon: PencilLine,
    keywords: ["update", "edit", "修改", "编辑", "脚本"],
  },
  {
    id: "commerce-derive-script",
    label: "裂变脚本",
    description: "创建多个独立脚本变体",
    goal: "帮我从指定广告脚本创建多个独立变体；先定位源脚本，再根据我的自然语言要求判断裂变维度和数量。",
    mode: "supervised",
    icon: Sparkles,
    keywords: ["derive", "variant", "裂变", "变体", "场景"],
  },
  {
    id: "commerce-generate-video",
    label: "生成视频",
    description: "为一条脚本生成视频",
    goal: "为我指定的广告脚本生成带货视频；先读取权威视频选项，缺少脚本身份时先询问。",
    mode: "supervised",
    icon: Video,
    keywords: ["video", "generate", "视频", "生成"],
  },
  {
    id: "commerce-batch-generate-video",
    label: "批量生成视频",
    description: "为多条脚本分别生成视频",
    goal: "为我指定的多条广告脚本分别生成独立视频；先列出脚本和视频选项，并明确本次选择范围。",
    mode: "supervised",
    icon: Film,
    keywords: ["batch", "video", "批量", "视频"],
  },
  {
    id: "commerce-video-tasks",
    label: "查看视频任务",
    description: "查看视频任务进度与结果",
    goal: "列出当前项目最近的直生成视频任务、真实状态和失败原因。",
    mode: "plan_only",
    icon: ListVideo,
    keywords: ["tasks", "status", "任务", "状态", "视频"],
  },
  {
    id: "commerce-cancel-video",
    label: "取消视频任务",
    description: "取消指定运行中视频任务",
    goal: "取消我指定的运行中直生成视频任务；存在多个候选时先让我选择。",
    mode: "supervised",
    icon: XCircle,
    keywords: ["cancel", "video", "取消", "视频", "任务"],
  },
];

export const ASSISTANT_QUICK_ACTIONS = NARRATIVE_ASSISTANT_QUICK_ACTIONS;

export function assistantQuickActionsForProjectKind(projectKind: ProjectKind | undefined) {
  return projectKind === "commerce_video"
    ? COMMERCE_ASSISTANT_QUICK_ACTIONS
    : NARRATIVE_ASSISTANT_QUICK_ACTIONS;
}
