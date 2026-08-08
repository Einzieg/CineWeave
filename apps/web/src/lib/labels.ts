export function statusLabel(status?: string) {
  switch ((status ?? "").toLowerCase()) {
    case "ready":
      return "就绪";
    case "enabled":
      return "已启用";
    case "disabled":
      return "已禁用";
    case "removed":
      return "已移除";
    case "scenes_ready":
      return "分场就绪";
    case "imported":
      return "已导入";
    case "active":
      return "启用";
    case "archived":
      return "已归档";
    case "running":
      return "运行中";
    case "analyzing":
      return "分析中";
    case "planning":
      return "规划中";
    case "reviewing":
      return "审核中";
    case "changes_requested":
      return "需调整";
    case "processing":
      return "处理中";
    case "prepared":
      return "已准备";
    case "leased":
      return "已领取";
    case "provider_running":
      return "供应商生成中";
    case "committing":
      return "提交结果中";
    case "unknown_outcome":
      return "结果未知";
    case "queued":
      return "排队中";
    case "waiting_workflow":
      return "后台任务运行中";
    case "waiting_input":
      return "等待用户确认";
    case "uploading":
      return "上传中";
    case "waiting_user_confirmation":
      return "等待确认";
    case "abandoned":
      return "已放弃";
    case "cancelling":
      return "取消中";
    case "draft":
      return "草稿";
    case "pending":
      return "等待中";
    case "accepted":
      return "已接受";
    case "revoked":
      return "已撤销";
    case "expired":
      return "已过期";
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
    case "source_changed":
      return "原文已变更";
    case "stale":
      return "已过期";
    case "upstream_changed":
      return "上游已变更";
    case "skipped":
      return "已跳过";
    case "approved":
      return "已确认";
    case "passed":
      return "审核通过";
    case "manual_override":
      return "人工通过";
    case "fresh":
      return "最新";
    case "rejected":
      return "已拒绝";
    case "partial":
      return "部分完成";
    case "partial_succeeded":
    case "partially_succeeded":
      return "部分完成";
    case "failed_retryable":
      return "失败，可重试";
    case "failed_terminal":
      return "失败，不可重试";
    case "planned":
      return "已规划";
    case "blocked":
      return "未就绪";
    case "reconfiguration_required":
      return "需要重新配置";
    case "replan_required":
      return "需要重新规划";
    case "preview_only":
      return "仅可预览";
    case "not_requested":
      return "未请求音频";
    case "native_audio_unavailable":
      return "无可用原生音频";
    case "audio_unverified":
      return "音轨待审核";
    case "audio_verified":
      return "音频已验证";
    case "needs_audio_retry":
      return "音频需重试";
    case "none":
      return "不使用参考";
    case "first_frame":
      return "首帧参考";
    case "last_frame":
      return "尾帧参考";
    case "video_reference":
      return "视频参考";
    case "previous_segment_tail":
      return "前片段尾帧续接";
    case "video_extension":
      return "延长任务";
    case "succeeded":
    case "completed":
      return "已完成";
    case "processed":
      return "已处理";
    case "stored":
      return "已入库";
    case "transferring":
      return "媒体转存中";
    case "discarded":
      return "结果已丢弃";
    case "generating":
      return "生成中";
    case "polling":
      return "查询上游状态中";
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
    case "prompt_ready":
      return "提示词就绪";
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
    case "brief":
      return "创意文案";
    default:
      return value || "未知";
  }
}

export function commerceLanguageModeLabel(value?: string) {
  switch (value) {
    case "auto": return "自动判断";
    case "explicit": return "用户指定";
    default: return value || "未设置";
  }
}

export function commerceDerivationKindLabel(value?: string) {
  switch (value) {
    case "copy": return "复制脚本";
    case "language_variant": return "语言版本";
    case "agent_idea": return "助手创意";
    default: return value || "原始脚本";
  }
}

export function shotStateRoleLabel(value?: string) {
  switch (value) {
    case "planned_entry": return "计划首帧状态";
    case "planned_exit": return "计划尾帧状态";
    case "observed_exit": return "实际尾帧状态";
    default: return value || "未知状态";
  }
}

export function shotTransitionTypeLabel(value?: string) {
  switch (value) {
    case "match_action_cut": return "动作匹配切换";
    case "same_scene_cut": return "同场景切换";
    case "camera_cut": return "机位切换";
    case "subject_change": return "主体变化";
    case "scene_cut": return "场景切换";
    case "time_jump": return "时间跳转";
    case "montage_cut": return "蒙太奇切换";
    case "unclassified": return "独立镜头";
    default: return value || "未分类";
  }
}

export function shotAnchorRoleLabel(value?: string) {
  switch (value) {
    case "planned_first_frame": return "计划首帧";
    case "planned_last_frame": return "计划尾帧";
    case "storyboard_sheet": return "分镜板";
    case "storyboard_panel": return "分镜板画格";
    case "observed_tail_frame": return "实际尾帧";
    case "continuity_hint": return "连续性参考";
    default: return value || "视觉锚点";
  }
}

export function shotReferenceRoleLabel(value?: string) {
  switch (value) {
    case "first_frame": return "首帧";
    case "last_frame": return "尾帧";
    case "storyboard_sheet": return "分镜板";
    case "character_identity": return "角色身份";
    case "character_costume": return "角色服装";
    case "scene_identity": return "场景身份";
    case "scene_spatial": return "场景空间";
    case "prop_identity": return "道具身份";
    case "continuity_hint": return "连续性提示";
    case "motion_reference": return "动作参考";
    case "video_reference": return "视频参考";
    case "audio_reference": return "音频参考";
    case "style_reference": return "风格参考";
    default: return value || "参考";
  }
}

export function shotReferenceSemanticsLabel(value?: string) {
  switch (value) {
    case "output_start_frame": return "输出起始画面";
    case "output_end_frame": return "输出结束画面";
    case "ordered_keyframe_sheet": return "有序关键帧指导";
    case "character_identity": return "角色身份约束";
    case "character_costume": return "角色服装约束";
    case "scene_identity": return "场景身份约束";
    case "scene_spatial_layout": return "场景空间约束";
    case "prop_identity": return "道具身份约束";
    case "cross_shot_continuity_hint": return "跨镜头软连续性";
    case "motion_guidance": return "动作运动指导";
    case "video_guidance": return "视频语义指导";
    case "audio_guidance": return "音频语义指导";
    case "visual_style_guidance": return "视觉风格指导";
    case "identity_scene_style_guidance": return "身份、场景与风格指导";
    default: return value || "语义参考";
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
    case "video.text_to_video":
      return "文生视频";
    case "video.image_to_video":
      return "图生视频";
    case "video.create_task":
      return "创建视频任务";
    case "video.poll":
    case "video.poll_task":
      return "视频任务轮询";
    case "video.cancel_task":
      return "取消视频任务";
    case "audio.generate":
      return "音频生成";
    case "audio.tts":
      return "语音合成";
    case "audio.transcribe":
      return "语音识别";
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
    case "asset_reference_image":
      return "资产参考";
    case "generated_image":
      return "生成图片";
    case "generated_video":
      return "生成视频";
    default:
      return type || "媒体";
  }
}

export function assetReferenceTypeLabel(type?: string) {
  switch (type) {
    case "generated":
      return "生成图";
    case "uploaded":
      return "上传图";
    case "derived":
      return "派生图";
    case "selected":
      return "选用图";
    default:
      return type || "参考图";
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

export function reasoningLevelLabel(value?: string) {
  switch (value?.toLowerCase()) {
    case "none":
      return "关闭思考";
    case "minimal":
      return "极低";
    case "low":
      return "低";
    case "medium":
      return "中";
    case "high":
      return "高";
    case "xhigh":
    case "max":
      return "最高";
    default:
      return value || "供应商默认";
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
    case "short_film":
      return "短片";
    case "comic_drama":
      return "漫剧";
    case "brand_ad":
      return "品牌广告";
    case "character_ip":
      return "角色 IP";
    case "other":
      return "其他";
    case "commerce_video":
      return "带货视频";
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
    case "storyboard_first":
      return "分镜先行";
    default:
      return value || "未设置";
  }
}

export function commerceSalesBeatLabel(value?: string | null) {
  const labels: Record<string, string> = {
    hook: "开场钩子",
    problem: "需求痛点",
    pain_point: "需求痛点",
    benefit: "核心卖点",
    feature: "核心卖点",
    demonstration: "使用演示",
    proof: "效果证明",
    cta: "购买引导",
  };
  return labels[value || ""] || value || "销售镜头";
}

export function localeLabel(
  value?: string | null,
  available: Array<{ locale: string; label: string }> = [],
) {
  const normalized = (value || "").trim();
  if (!normalized) return "未确认";
  if (normalized.toLowerCase() === "auto") return "自动判断";
  const configured = available.find((item) => item.locale.toLowerCase() === normalized.toLowerCase());
  if (configured?.label) return configured.label;
  const labels: Record<string, string> = {
    "zh-cn": "简体中文",
    "zh-tw": "繁体中文",
    "en-us": "英语（美国）",
    "en-gb": "英语（英国）",
    "ms-my": "马来语",
    "id-id": "印度尼西亚语",
    "ja-jp": "日语",
    "ko-kr": "韩语",
    "th-th": "泰语",
    "vi-vn": "越南语",
    "es-es": "西班牙语（西班牙）",
    "es-mx": "西班牙语（墨西哥）",
    "pt-br": "葡萄牙语（巴西）",
    "fr-fr": "法语",
    "de-de": "德语",
    "it-it": "意大利语",
    "ru-ru": "俄语",
    "ar-sa": "阿拉伯语",
    "hi-in": "印地语",
    "tr-tr": "土耳其语",
  };
  return labels[normalized.toLowerCase()] || normalized;
}

export function commerceReferenceRoleLabel(value?: string | null) {
  const labels: Record<string, string> = {
    primary: "主商品图",
    detail: "产品细节",
    logo: "品牌标识",
    usage: "使用场景",
    context: "环境参考",
  };
  return labels[value || ""] || value || "商品参考";
}

export function commerceSegmentUsageLabel(value?: string | null) {
  const labels: Record<string, string> = {
    visual: "画面依据",
    voiceover: "旁白依据",
    onscreen: "屏幕文字",
    cta: "行动引导",
    context: "上下文",
  };
  return labels[value || ""] || value || "来源片段";
}

export function commerceTimingAdvisoryLabel(value?: string | null) {
  const labels: Record<string, string> = {
    none: "时长匹配",
    info: "时长提示",
    warning: "旁白偏长",
    critical: "旁白超出",
  };
  return labels[value || ""] || "时长待评估";
}

export function projectKindLabel(value?: string | null) {
  switch (value) {
    case "narrative":
      return "叙事项目";
    case "commerce_video":
      return "带货视频";
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
    case "organization_admin":
      return "组织管理员";
    case "org_member":
    case "organization_member":
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
    create: "创建",
    update: "更新",
    test: "测试",
    rotate: "轮换",
    retry: "重试",
    audit: "审计",
    rebuild: "重建",
  };
  const resourceLabels: Record<string, string> = {
    organization: "组织",
    "organization.members": "组织成员",
    workspace: "工作区",
    project: "项目",
    "project.members": "项目成员",
    "project.video_production": "项目视频生产",
    source: "原文",
    novel_event: "小说事件",
    adaptation_plan: "改编计划",
    script: "剧本",
    asset: "资产",
    storyboard: "分镜",
    artifact: "产物",
    media: "媒体",
    provider: "供应商",
    "provider.credentials": "供应商凭据",
    "provider.models": "供应商模型",
    model_profiles: "模型档案",
    prompt: "提示词",
    workflow: "工作流",
    team: "团队",
    member: "成员",
    role: "角色",
    audit: "审计",
    admin: "系统管理",
  };
  const actionLabel = action ? actionLabels[action] : "";
  const resourceLabel = resourceLabels[resource] || resource;
  return actionLabel ? `${resourceLabel || "资源"}${actionLabel}` : value;
}

export function auditActionLabel(value?: string) {
  const labels: Record<string, string> = {
    "organization.invitation.created": "创建组织邀请",
    "organization.invitation.revoked": "撤销组织邀请",
    "organization.invitation.accepted": "接受组织邀请",
    "organization.member.disabled": "停用组织成员",
    "organization.member.restored": "恢复组织成员",
    "organization.member.removed": "移除组织成员",
    "organization.member.left": "退出组织",
    "organization.member.profile_updated": "更新成员资料",
    "organization.member.password_reset_requested": "发起成员密码重置",
    "organization.member.password_reset_completed": "完成成员密码重置",
    "organization.updated": "更新组织资料",
    "system.organization.created": "系统管理员创建组织",
    "system.organization.member.created": "系统管理员直接新增成员",
    "system.organization.member.updated": "系统管理员编辑成员",
    "team.created": "创建团队",
    "team.updated": "更新团队",
    "team.disabled": "停用团队",
    "team.member.added": "添加团队成员",
    "team.member.removed": "移除团队成员",
    "role_binding.created": "创建角色绑定",
    "role_binding.revoked": "撤销角色绑定",
    "role.created": "创建自定义角色",
    "role.updated": "更新自定义角色",
    "role.deleted": "删除自定义角色",
    "user.profile.updated": "更新个人资料",
    "user.username.set": "设置用户名",
  };
  return labels[value || ""] || "管理操作";
}

export function auditResourceTypeLabel(value?: string) {
  const labels: Record<string, string> = {
    organization: "组织",
    organization_invitation: "组织邀请",
    user: "用户",
    team: "团队",
    role: "角色",
    role_binding: "角色绑定",
  };
  return labels[value || ""] || "资源";
}

export function projectDeletionStatusLabel(value?: string) {
  const labels: Record<string, string> = {
    requested: "等待启动",
    cancelling_tasks: "正在取消任务",
    waiting_for_terminal: "等待任务结束",
    deleting_storage: "正在删除文件",
    deleting_business_data: "正在删除项目数据",
    completed: "删除完成",
    failed_retryable: "删除失败，可重试",
    failed_terminal: "删除失败",
  };
  return labels[value || ""] || "删除中";
}

export function entitlementDenialLabel(value?: string) {
  const labels: Record<string, string> = {
    feature_unknown: "商业功能未登记",
    feature_not_compiled: "当前发行版未包含此功能",
    internal_release_mismatch: "内部发行身份不一致",
    commercial_writes_frozen: "商业写入已由运维冻结",
    plan_entitlement_required: "当前组织套餐未包含此功能",
    billing_account_suspended: "付费账户已停用",
    permission_denied: "当前账号权限不足",
    billing_binding_invalid: "项目付费账户绑定无效",
    billing_account_scope_mismatch: "付费账户不属于当前组织",
    billing_authority_mismatch: "付费账户所属计费服务不匹配",
    billing_sponsorship_required: "需要钱包所有者授权",
    billing_routing_candidate_missing: "没有可用的计费凭据",
    billing_insufficient_balance: "付费账户余额不足",
    billing_credential_unavailable: "付费凭据当前不可用",
    billing_model_forbidden: "当前套餐不可使用该模型",
    billing_upstream_unavailable: "计费服务暂时不可用",
  };
  return labels[value || ""] || "商业权限校验未通过";
}
