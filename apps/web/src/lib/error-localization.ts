const ERROR_CODE_MESSAGES: Record<string, string> = {
  ACCESS_DENIED: "没有执行此操作的权限",
  ACTIVE_FINAL_VIDEO_REQUIRES_CONFIRMATION: "删除当前启用的成片前需要再次确认",
  ACTIVITY_FAILED: "任务步骤执行失败",
  AUTH_FAILED: "供应商鉴权失败，请检查访问密钥和账号配置",
  ARTIFACT_HAS_NO_STORAGE_OBJECT: "当前文件没有可用的存储对象",
  ASSET_PROMPT_NOT_READY: "资产完整提示词尚未就绪，请先生成或保存提示词",
  ASSET_REVISION_CONFLICT: "资产已被其他操作更新，请刷新后重新编辑",
  BATCH_ALL_FAILED: "批量任务中的所有项目均执行失败",
  CONFLICT: "数据状态已发生变化，请刷新后重试",
  CONTINUITY_DEPENDENCY_FAILED: "前序连续镜头生成失败，当前镜头暂时无法继续",
  CONTENT_REJECTED: "上游安全策略拒绝了本次内容生成，请调整提示词后重试",
  CURRENT_SCRIPT_VERSION: "当前启用的剧本版本不能归档",
  EMAIL_EXISTS: "该邮箱已被使用",
  EXPORT_NOT_READY: "导出文件尚未准备完成",
  FINAL_VIDEO_NOT_READY: "成片文件尚未准备完成",
  FORBIDDEN: "没有执行此操作的权限",
  INTERNAL_ERROR: "服务内部发生错误，请稍后重试",
  INVALID_CREDENTIALS: "邮箱或密码错误",
  INVALID_REQUEST: "请求参数无效，请检查当前设置后重试",
  LAST_OWNER_BINDING: "不能删除当前组织的最后一个所有者权限",
  METHOD_NOT_ALLOWED: "当前请求方式不受支持",
  MEDIA_DOWNLOAD_FAILED: "供应商返回的媒体文件下载或转存失败",
  MODEL_CAPABILITY_UNAVAILABLE: "当前模型不支持所需的生成能力",
  MODEL_NOT_FOUND: "供应商中未找到当前模型",
  MODEL_PROFILE_NOT_CONFIGURED: "当前业务模型尚未配置可用的供应商模型",
  NOT_FOUND: "指定内容不存在或已被删除",
  ORGANIZATION_REQUIRED: "请先选择组织后再执行此操作",
  PROVIDER_OUTPUT_EMPTY: "供应商没有返回有效内容",
  PROVIDER_OUTPUT_INVALID: "供应商返回的内容格式无效",
  PROVIDER_CANCEL_FAILED: "取消供应商任务失败，请稍后重试",
  PROVIDER_CANCEL_OUTCOME_UNKNOWN: "供应商取消结果暂时无法确认，请稍后刷新状态",
  PROVIDER_CIRCUIT_OPEN: "供应商服务连续失败，已暂时停止发送新请求",
  PROVIDER_CONCURRENCY_LIMITED: "供应商并发额度已满，请稍后重试",
  PROVIDER_DAILY_QUOTA_EXCEEDED: "供应商当日额度已用尽",
  PROVIDER_GATEWAY_ERROR: "供应商网关调用失败",
  PROVIDER_GATEWAY_REQUIRED: "当前操作必须通过供应商网关执行",
  PROVIDER_GATEWAY_UNAVAILABLE: "供应商网关当前不可用",
  PROVIDER_IDEMPOTENCY_CONFLICT: "相同请求标识对应了不同内容，请刷新后重试",
  PROVIDER_INSTALL_FAILED: "供应商预设安装失败",
  PROVIDER_LEASE_EXPIRED: "供应商调用凭证租约已过期，请重试",
  PROVIDER_MANIFEST_INVALID: "供应商接口配置无效",
  PROVIDER_MODEL_TEMPLATE_INVALID: "供应商模型模板无效",
  PROVIDER_MONTHLY_BUDGET_EXCEEDED: "供应商本月预算已用尽",
  PROVIDER_PRESET_NOT_FOUND: "未找到指定的供应商预设",
  PROVIDER_RATE_LIMITED: "供应商请求频率已达上限，请稍后重试",
  PROVIDER_REJECTED: "上游供应商拒绝了请求",
  PROVIDER_REQUEST_IN_PROGRESS: "相同供应商请求正在执行，请等待完成",
  PROVIDER_SERVICE_UNAVAILABLE: "供应商服务当前不可用",
  PROVIDER_SETUP_FIELD_MISSING: "供应商配置缺少必填字段",
  PROVIDER_TASK_NOT_FOUND: "未找到对应的供应商异步任务",
  PROVIDER_UNKNOWN_OUTCOME: "供应商调用结果暂时无法确认，请稍后刷新状态",
  PROVIDER_VIDEO_CANCELLED: "视频生成任务已取消",
  PROVIDER_VIDEO_POLLING_TIMEOUT: "等待供应商视频生成结果超时",
  PUBLIC_REGISTRATION_DISABLED: "当前系统未开放公开注册",
  RENDER_PLAN_REPLAN_REQUIRED: "当前视频执行计划已失效，请重新生成视频提示词计划",
  RATE_LIMITED: "请求过于频繁，请稍后重试",
  RESULT_EXPIRED: "供应商生成结果已过期，请重新生成",
  QUOTA_EXCEEDED: "供应商额度已用尽",
  SETUP_ALREADY_COMPLETED: "系统已经完成初始化",
  SHOT_IMAGE_PROMPT_RUNNING: "镜头图片提示词正在生成，完成前不能修改图片提示词设置",
  SHOT_IMAGE_RUNNING: "镜头图片正在生成，完成前不能修改图片生成设置",
  SHOT_VIDEO_PROMPT_RUNNING: "镜头视频提示词正在生成，完成前不能修改视频提示词设置",
  SHOT_VIDEO_RUNNING: "镜头视频正在生成，完成前不能修改视频生成设置",
  SHOT_VIDEOS_REQUIRED: "所有分镜视频生成完成后才能合成成片",
  STORAGE_UNAVAILABLE: "对象存储服务当前不可用",
  STORYBOARD_PLAN_REVISION_REQUIRED: "该操作需要创建新的分镜计划版本",
  STORYBOARD_REPLAN_REQUIRED: "分镜计划已失效，请重新生成本集分镜",
  TEMPORAL_UNAVAILABLE: "工作流服务当前不可用",
  UNAUTHENTICATED: "登录已过期，请重新登录",
  UNSUPPORTED_FILE_TYPE: "当前文件类型不受支持",
  UNSUPPORTED_PREVIEW_TYPE: "当前文件类型不支持在线预览",
  UPSTREAM_TIMEOUT: "供应商请求超时，请稍后重试",
  UPSTREAM_INTERNAL_ERROR: "供应商服务内部错误，请稍后重试",
  UPSTREAM_OUTPUT_MISMATCH: "供应商返回结果与请求类型不匹配",
  UPSTREAM_RATE_LIMITED: "供应商请求频率已达上限，请稍后重试",
  UPSTREAM_STREAM_TRUNCATED: "供应商流式响应意外中断，请重试",
  UNKNOWN_ERROR: "发生未知错误，请稍后重试",
  UNSUPPORTED_CAPABILITY: "当前模型不支持所需的生成能力",
  VALIDATION_FAILED: "请求参数无效，请检查填写内容后重试",
  WEBHOOK_UNAUTHORIZED: "回调签名校验失败",
  WORKFLOW_NOT_TERMINAL: "任务结束后才能重试失败项目",
  WORKFLOW_RESULT_DISCARDED: "任务已结束，本次迟到的执行结果已被丢弃",
  WORKFLOW_RETRY_UNSUPPORTED: "当前任务类型不支持重试失败项目",
  WORKFLOW_FAILED: "工作流执行失败",
  WORKFLOW_INPUT_INVALID: "工作流输入已失效，无法继续执行或重试",
  WORKFLOW_START_HANDLER_UNKNOWN: "当前工作流类型没有可用的启动处理器",
  WORKFLOW_START_INPUT_HASH_MISMATCH: "工作流输入在启动前发生变化，请重新发起任务",
  WORKFLOW_START_PAYLOAD_INVALID: "工作流启动参数无效",
  CANNOT_CANCEL_COMPLETED_TASK: "已结束的供应商任务不能取消",
  POLLING_TIMEOUT: "等待供应商任务结果超时",
  PROMPT_RENDER_FAILED: "提示词渲染失败，请检查模板和输入内容",
  PROMPT_TEMPLATE_NOT_FOUND: "未找到所需的提示词模板",
  PROMPT_VERSION_NOT_FOUND: "未找到所需的提示词版本",
};

const EXACT_MESSAGE_TRANSLATIONS: Record<string, string> = {
  "access denied": "没有执行此操作的权限",
  "authentication is required": "登录已过期，请重新登录",
  "canonical asset was changed by another operation": "资产已被其他操作更新，请刷新后重新编辑",
  "email or password is invalid": "邮箱或密码错误",
  "internal server error": "服务内部发生错误，请稍后重试",
  "invalid email": "邮箱格式无效",
  "invalid input": "输入内容无效",
  "object storage is not configured": "对象存储尚未配置",
  "permission denied": "没有执行此操作的权限",
  "provider rejected the request": "上游供应商拒绝了请求",
  "provider gateway is not configured": "供应商网关尚未配置",
  "provider image media could not be stored": "供应商返回的图片无法转存到对象存储",
  "provider video media could not be stored": "供应商返回的视频无法转存到对象存储",
  "provider audio media could not be stored": "供应商返回的音频无法转存到对象存储",
  "provider request timed out": "供应商请求超时，请稍后重试",
  "request body is invalid": "请求内容格式无效",
  "request is invalid": "请求参数无效",
  "required": "此项为必填项",
  "resource conflict": "数据状态已发生变化，请刷新后重试",
  "resource was not found": "指定内容不存在或已被删除",
  "shot image prompt settings cannot be changed while prompt generation is running":
    "镜头图片提示词正在生成，完成前不能修改图片提示词设置",
  "shot image settings cannot be changed while generation is running":
    "镜头图片正在生成，完成前不能修改图片生成设置",
  "shot video prompt settings cannot be changed while prompt generation is running":
    "镜头视频提示词正在生成，完成前不能修改视频提示词设置",
  "shot video settings cannot be changed while generation is running":
    "镜头视频正在生成，完成前不能修改视频生成设置",
  "unexpected EOF": "连接被上游提前中断，请重试",
  "workflow execution is no longer writable": "任务已结束，不能再写入执行结果",
};

const MESSAGE_PATTERNS: Array<{ pattern: RegExp; message: string }> = [
  {
    pattern: /provider task belongs to a different node execution/i,
    message: "视频执行计划已被旧任务占用，请重新生成视频提示词计划后重试",
  },
  { pattern: /bufio\.Scanner:\s*token too long/i, message: "返回内容过长，处理失败，请缩小单次任务范围后重试" },
  { pattern: /invalid input syntax for type uuid/i, message: "请求缺少有效的资源标识，请刷新页面后重试" },
  { pattern: /^invalid input/i, message: "输入内容无效" },
  { pattern: /^invalid (email|url)/i, message: "输入格式无效" },
  { pattern: /^too small/i, message: "输入内容过短" },
  { pattern: /^too big/i, message: "输入内容过长" },
  {
    pattern: /event .* payload is missing required field workflowRunId/i,
    message: "任务事件缺少工作流标识，无法完成状态同步",
  },
  {
    pattern: /video manifest endpoint was not found/i,
    message: "未找到视频任务状态查询接口，请检查供应商的视频接口配置",
  },
  {
    pattern: /invalid timing analyzer output/i,
    message: "分镜时长分析结果无效，请重新生成本集分镜计划",
  },
  {
    pattern: /shot depicts speech but .* no verbatim script dialogue/i,
    message: "镜头包含对白，但视频提示词没有保留剧本中的原始台词",
  },
  {
    pattern: /structured video prompt output remained invalid/i,
    message: "视频提示词结构校验失败，请重新生成视频提示词",
  },
  {
    pattern: /provider validation failed/i,
    message: "供应商请求校验失败，请检查模型能力和当前生成设置",
  },
  {
    pattern: /may violate our guardrails around violence/i,
    message: "上游安全策略认为生成结果可能涉及暴力内容，请调整相关描述后重试",
  },
  {
    pattern: /provider (image|video|audio) media could not be stored/i,
    message: "供应商返回的媒体文件无法转存到对象存储",
  },
];

const CHINESE_TEXT = /[\u3400-\u9fff]/;
const CODE_PREFIX = /^([A-Z][A-Z0-9_]+)\s*[:：]\s*(.+)$/s;

export function localizePlatformError(
  message?: string | null,
  code?: string | null,
  fallback = "操作失败，请稍后重试",
): string {
  let source = message?.trim() ?? "";
  let normalizedCode = code?.trim().toUpperCase() ?? "";
  const prefixed = source.match(CODE_PREFIX);
  if (prefixed) {
    normalizedCode ||= prefixed[1];
    source = prefixed[2].trim();
  }

  const exact = EXACT_MESSAGE_TRANSLATIONS[source];
  if (exact) {
    return exact;
  }
  const lowerExact = EXACT_MESSAGE_TRANSLATIONS[source.toLowerCase()];
  if (lowerExact) {
    return lowerExact;
  }
  for (const entry of MESSAGE_PATTERNS) {
    if (entry.pattern.test(source)) {
      return entry.message;
    }
  }
  if (CHINESE_TEXT.test(source)) {
    return source;
  }
  if (normalizedCode && ERROR_CODE_MESSAGES[normalizedCode]) {
    return ERROR_CODE_MESSAGES[normalizedCode];
  }
  return normalizedCode ? fallback : source || fallback;
}

export function userFacingErrorMessage(error: unknown, fallback = "操作失败，请稍后重试"): string {
  if (error && typeof error === "object") {
    const candidate = error as { code?: unknown; message?: unknown };
    const code = typeof candidate.code === "string" ? candidate.code : undefined;
    const message = typeof candidate.message === "string" ? candidate.message : undefined;
    return localizePlatformError(message, code, fallback);
  }
  return typeof error === "string" ? localizePlatformError(error, undefined, fallback) : fallback;
}
