"use client";

import type { LucideIcon } from "lucide-react";
import { ClipboardCheck, Film, Image, Layers3, SearchCheck, SquareStack, XCircle } from "lucide-react";

export type AssistantQuickAction = {
  id: string;
  label: string;
  description: string;
  goal: string;
  mode: "plan_only" | "supervised";
  icon: LucideIcon;
  keywords: string[];
};

export const ASSISTANT_QUICK_ACTIONS: AssistantQuickAction[] = [
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
