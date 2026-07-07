export function statusLabel(status?: string) {
  switch ((status ?? "").toLowerCase()) {
    case "ready":
      return "就绪";
    case "enabled":
      return "已启用";
    case "disabled":
      return "已禁用";
    case "scenes_ready":
      return "分场就绪";
    case "imported":
      return "已导入";
    case "active":
      return "启用";
    case "running":
      return "运行中";
    case "processing":
      return "处理中";
    case "queued":
      return "排队中";
    case "cancelling":
      return "取消中";
    case "draft":
      return "草稿";
    case "pending":
      return "等待中";
    case "open":
      return "待处理";
    case "resolved":
      return "已解决";
    case "ignored":
      return "已忽略";
    case "not_started":
      return "未开始";
    case "needs_review":
      return "待确认";
    case "events_pending_extraction":
      return "待提取事件";
    case "events_pending_review":
      return "事件待确认";
    case "adaptation_plan_pending":
      return "待生成改编计划";
    case "scenes_pending_parse":
      return "待解析分场";
    case "scenes_pending_review":
      return "分场待确认";
    case "needs_edit":
      return "需修改";
    case "needs_regeneration":
      return "需重生成";
    case "upstream_changed":
      return "上游已变更";
    case "approved":
      return "已确认";
    case "fresh":
      return "最新";
    case "rejected":
      return "已拒绝";
    case "partial":
      return "部分完成";
    case "succeeded":
    case "completed":
      return "已完成";
    case "processed":
      return "已处理";
    case "failed":
      return "失败";
    case "cancelled":
      return "已取消";
    case "image_succeeded":
      return "参考图完成";
    case "image_running":
      return "生成图片中";
    case "image_failed":
      return "图片失败";
    case "storyboard_ready":
      return "分镜就绪";
    case "video_succeeded":
      return "视频完成";
    case "video_running":
      return "生成视频中";
    case "video_failed":
      return "视频失败";
    default:
      return status || "未知";
  }
}

export function sourceTypeLabel(value?: string) {
  switch (value) {
    case "novel":
      return "小说";
    case "script":
      return "剧本";
    default:
      return value || "未知";
  }
}

export function contentFormatLabel(value?: string) {
  switch (value) {
    case "plain_text":
      return "纯文本";
    case "markdown":
      return "Markdown";
    default:
      return value || "未知";
  }
}

export function targetFormatLabel(value?: string) {
  switch (value) {
    case "short_video":
      return "短视频";
    case "feature":
      return "长片";
    case "episode":
      return "剧集";
    case "outline":
      return "大纲";
    default:
      return value || "未设置";
  }
}

export function modalityLabel(value?: string) {
  switch (value) {
    case "text":
      return "文本";
    case "image":
      return "图片";
    case "video":
      return "视频";
    case "audio":
      return "音频";
    case "multimodal":
      return "多模态";
    default:
      return value || "未知";
  }
}

export function promptCategoryLabel(value?: string) {
  switch (value) {
    case "script":
    case "script_generation":
      return "剧本生成";
    case "asset":
    case "asset_analysis":
      return "资产分析";
    case "storyboard":
      return "分镜设计";
    case "shot":
    case "shot_generation":
      return "镜头生成";
    case "review":
      return "审查修复";
    case "prompt":
      return "通用提示词";
    default:
      return value ? modalityLabel(value) : "通用提示词";
  }
}

export function taskTypeLabel(value?: string) {
  switch (value) {
    case "text.generate":
      return "文本生成";
    case "text.stream":
      return "文本流式生成";
    case "image.generate":
      return "图片生成";
    case "video.generate":
      return "视频生成";
    case "video.poll":
      return "视频任务轮询";
    case "audio.generate":
      return "音频生成";
    default:
      return promptCategoryLabel(value);
  }
}

export function providerKeyLabel(value?: string) {
  switch (value) {
    case "openai_compatible_custom":
      return "OpenAI 兼容";
    case "openrouter":
      return "OpenRouter";
    case "ollama":
      return "Ollama";
    case "google_gemini":
      return "Google Gemini";
    case "alibaba_dashscope":
      return "阿里通义千问";
    case "zhipu_glm":
      return "智谱 GLM";
    case "baidu_qianfan":
      return "百度文心千帆";
    case "xunfei_spark":
      return "讯飞星火";
    case "minimax":
      return "MiniMax";
    case "volcengine_ark":
      return "火山方舟";
    default:
      return value || "供应商";
  }
}

export function assetTypeLabel(type?: string) {
  switch (type) {
    case "character":
      return "角色";
    case "scene":
      return "场景";
    case "prop":
      return "道具";
    default:
      return type || "资产";
  }
}

export function artifactTypeLabel(type?: string) {
  switch (type) {
    case "image":
      return "图片";
    case "video":
      return "视频";
    case "audio":
      return "音频";
    case "document":
      return "文档";
    case "final_video":
      return "最终成片";
    case "asset_reference":
      return "资产参考";
    default:
      return type || "媒体";
  }
}

export function requirementTypeLabel(value?: string) {
  switch (value) {
    case "primary_reference":
      return "主参考";
    case "derived_reference":
      return "派生参考";
    case "shot_reference":
      return "镜头参考";
    default:
      return value || "素材需求";
  }
}

export function reviewSeverityLabel(value?: string) {
  switch (value) {
    case "critical":
      return "严重";
    case "high":
      return "高";
    case "medium":
      return "中";
    case "low":
      return "低";
    default:
      return value || "未知";
  }
}

export function reviewCategoryLabel(value?: string) {
  switch (value) {
    case "visual_continuity":
      return "视觉连续性";
    case "script":
      return "剧本";
    case "asset":
      return "资产";
    case "storyboard":
      return "分镜";
    case "timeline":
      return "时间线";
    case "export":
      return "导出";
    default:
      return value || "审阅";
  }
}

export function productionFieldLabel(value: string) {
  switch (value) {
    case "novelSourceCount":
      return "小说原文";
    case "scriptSourceCount":
      return "剧本原文";
    case "chapterCount":
      return "分集/章节";
    case "eventCount":
      return "事件";
    case "approvedEventCount":
      return "已确认事件";
    case "pendingEventReviewCount":
      return "待确认事件";
    case "adaptationPlanCount":
      return "改编计划";
    case "scriptCount":
      return "剧本";
    case "scriptVersionCount":
      return "剧本版本";
    case "scriptSceneCount":
      return "分场";
    case "reviewedSceneCount":
      return "已审分场";
    case "assetCount":
      return "资产";
    case "approvedAssetCount":
      return "已确认资产";
    case "assetCardCount":
      return "资产卡片";
    case "requirementCount":
      return "镜头资产需求";
    case "approvedRequirementCount":
      return "已确认需求";
    case "shotCount":
    case "storyboardShotCount":
      return "镜头";
    case "imageSucceeded":
      return "图片完成";
    case "imageRunning":
      return "图片生成中";
    case "imageFailed":
      return "图片失败";
    case "videoSucceeded":
      return "视频完成";
    case "videoRunning":
      return "视频生成中";
    case "videoFailed":
      return "视频失败";
    case "finalVideoCount":
      return "成片版本";
    default:
      return value;
  }
}

export function productionStageLabel(value?: string) {
  switch (value) {
    case "source":
      return "原文与事件";
    case "assets":
      return "资产";
    case "storyboard":
      return "分镜";
    case "shotAssets":
      return "镜头资产";
    case "shotImages":
      return "镜头图片";
    case "shotVideos":
      return "镜头视频";
    case "finalVideo":
      return "最终成片";
    case "not_started":
    case "":
    case undefined:
      return "未开始";
    default:
      return value;
  }
}

export function projectTypeLabel(value?: string | null) {
  switch (value) {
    case "silent_video":
      return "无对白视频";
    case "short_video":
      return "短视频";
    case "film":
      return "影视项目";
    default:
      return value || "未设置";
  }
}

export function contentTypeLabel(value?: string | null) {
  switch (value) {
    case "novel":
      return "小说";
    case "script":
      return "剧本";
    case "original":
      return "原创";
    default:
      return value || "未设置";
  }
}

export function roleKeyLabel(value?: string) {
  switch (value) {
    case "org_owner":
    case "organization_owner":
      return "组织所有者";
    case "org_admin":
      return "组织管理员";
    case "org_member":
      return "组织成员";
    case "provider_admin":
      return "供应商管理员";
    case "project_owner":
      return "项目所有者";
    case "project_editor":
      return "项目编辑";
    case "project_viewer":
      return "项目查看者";
    case "owner":
      return "所有者";
    case "admin":
      return "管理员";
    case "producer":
      return "制片";
    case "editor":
      return "编辑";
    case "viewer":
      return "查看者";
    default:
      return value || "角色";
  }
}

export function permissionKeyLabel(value?: string) {
  if (!value) {
    return "权限";
  }
  const action = value.split(".").pop();
  const resource = value.split(".").slice(0, -1).join(".");
  const actionLabels: Record<string, string> = {
    read: "查看",
    write: "编辑",
    delete: "删除",
    manage: "管理",
    execute: "执行",
    approve: "确认",
    analyze: "分析",
    generate: "生成",
    run: "运行",
    cancel: "取消",
  };
  const resourceLabels: Record<string, string> = {
    organization: "组织",
    workspace: "工作区",
    project: "项目",
    source: "原文",
    novel_event: "小说事件",
    adaptation_plan: "改编计划",
    script: "剧本",
    asset: "资产",
    storyboard: "分镜",
    artifact: "产物",
    media: "媒体",
    provider: "供应商",
    prompt: "提示词",
    workflow: "工作流",
    team: "团队",
    role: "角色",
    audit: "审计",
    admin: "系统管理",
  };
  const actionLabel = action ? actionLabels[action] : "";
  const resourceLabel = resourceLabels[resource] || resource;
  return actionLabel ? `${resourceLabel || "资源"}${actionLabel}` : value;
}
