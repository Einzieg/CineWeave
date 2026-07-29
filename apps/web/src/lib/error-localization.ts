const ERROR_CODE_MESSAGES: Record<string, string> = {
  ACCESS_DENIED: "没有执行此操作的权限",
  ACCOUNT_SHARED_ACROSS_ORGANIZATIONS: "该账号同时属于多个组织，当前组织管理员不能修改其全局账号资料或密码",
  ACTIVE_FINAL_VIDEO_REQUIRES_CONFIRMATION: "删除当前启用的成片前需要再次确认",
  ACTIVITY_FAILED: "任务步骤执行失败",
  AUTH_FAILED: "供应商鉴权失败，请检查访问密钥和账号配置",
  ARTIFACT_HAS_NO_STORAGE_OBJECT: "当前文件没有可用的存储对象",
  ASSET_PROMPT_NOT_READY: "资产完整提示词尚未就绪，请先生成或保存提示词",
  ASSET_REVISION_CONFLICT: "资产已被其他操作更新，请刷新后重新编辑",
  BATCH_ALL_FAILED: "批量任务中的所有项目均执行失败",
  BILLING_ACCOUNT_NOT_FOUND: "付费账户不存在或当前账号无权访问",
  BILLING_ACCOUNT_PROVISIONING_CONFLICT: "付费账户开户请求与已有记录冲突",
  BILLING_ACCOUNT_SCOPE_MISMATCH: "付费账户不属于当前组织或项目范围",
  BILLING_ACCOUNT_SUSPENDED: "付费账户当前不可用",
  BILLING_AUTHORITY_INCOMPATIBLE: "计费服务版本与当前系统不兼容",
  BILLING_AUTHORITY_NOT_FOUND: "未找到当前付费账户对应的计费服务",
  BILLING_CONTEXT_INVALID: "计费上下文无效，请刷新后重试",
  BILLING_CONTRACT_INCOMPATIBLE: "当前 New API 版本不支持该安全计费操作",
  BILLING_CREDENTIAL_PROVISIONING_UNCERTAIN: "计费凭据状态暂时无法确认，请稍后重试",
  BILLING_CREDENTIAL_SECRET_LOST: "计费凭据已失效，请联系管理员重新开户",
  BILLING_INSUFFICIENT_BALANCE: "New API 账户余额不足",
  BILLING_INTERNAL_ERROR: "计费服务处理失败，请稍后重试",
  BILLING_ORDER_ACCOUNT_MISMATCH: "订单与当前付费账户不匹配",
  BILLING_ORDER_CONFLICT: "同一请求标识已用于不同订单，请勿重复修改提交",
  BILLING_ORDER_NOT_FOUND: "订单不存在或当前账号无权访问",
  BILLING_PAYMENT_FAILED: "支付处理失败，请查看订单状态后重试",
  BILLING_PERMISSION_DENIED: "没有执行该计费操作的权限",
  BILLING_PROJECT_NOT_FOUND: "项目不存在或当前账号无权访问",
  BILLING_REFUND_NOT_ALLOWED: "当前订单或 New API 版本不支持退款",
  BILLING_REVISION_CONFLICT: "计费配置已被更新，请刷新后重试",
  BILLING_SPONSORSHIP_NOT_FOUND: "个人钱包授权不存在或已失效",
  BILLING_SPONSORSHIP_REQUIRED: "使用个人钱包前需要钱包所有者授权",
  BILLING_STEP_UP_INVALID: "近期身份验证失败",
  BILLING_STEP_UP_RATE_LIMITED: "身份验证尝试过于频繁，请稍后重试",
  BILLING_STEP_UP_REQUIRED: "此操作需要重新验证当前登录密码",
  BILLING_SUBSCRIPTION_INACTIVE: "当前订阅不可用",
  BILLING_TOKEN_DISABLED: "New API Token 已停用",
  BILLING_TOKEN_MODEL_FORBIDDEN: "New API Token 不允许使用当前模型",
  BILLING_UPSTREAM_UNAVAILABLE: "New API 计费服务暂时不可用",
  BILLING_WEBHOOK_INVALID: "计费事件签名或内容无效",
  CHILD_WORKFLOW_FAILED: "子工作流执行失败，请查看失败步骤",
  CONFLICT: "数据状态已发生变化，请刷新后重试",
  CONTINUITY_DEPENDENCY_FAILED: "前序连续镜头生成失败，当前镜头暂时无法继续",
  COMMERCE_BINDING_MISMATCH: "带货工作流配置已变化，请刷新后重试",
  COMMERCE_DIRECT_VIDEO_INVALID: "所选时长、分辨率或参考图不符合当前视频模型配置",
  COMMERCE_DIRECT_VIDEO_NOT_FOUND: "视频生成记录不存在或已被删除",
  COMMERCE_DIRECT_VIDEO_STATE_CONFLICT: "视频生成状态已变化，请刷新后重试",
  COMMERCE_DIRECT_VIDEO_UNAVAILABLE: "当前没有可直接生成商品视频的模型路由",
  COMMERCE_IMAGE_FIDELITY_REJECTED: "商品外观保真审核未通过，请调整提示词或参考图后重新生成",
  COMMERCE_IMAGE_FIDELITY_REVIEW_FAILED: "参考图已生成并入库，但商品保真审核未完成；重试时不会重复生成图片",
  COMMERCE_LANGUAGE_CONFIRMATION_REQUIRED: "需要先确认视频语言",
  COMMERCE_LANGUAGE_REQUIRED: "请选择视频语言或使用自动判断",
  COMMERCE_LANGUAGE_UNSUPPORTED: "当前模板或模型不支持所选视频语言",
  COMMERCE_LOCALIZATION_CONTRACT_INVALID: "本地化脚本结构无效，请重试并查看任务中的具体校验信息",
  COMMERCE_LOCALIZATION_REVIEW_EXHAUSTED: "本地化审核三轮后仍发现会改变商品事实或内容通道的问题，请查看任务详情",
  COMMERCE_PRODUCT_PRIMARY_IMAGE_REQUIRED: "请先设置产品主图",
  COMMERCE_PRODUCT_REFERENCE_REQUIRED: "请至少上传一张产品图片",
  COMMERCE_PRODUCT_RECONFIGURATION_REQUIRED: "商品资料已用于生产，请确认影响后创建新商品版本",
  COMMERCE_PRODUCT_REQUIRED: "请先填写商品资料",
  COMMERCE_PRODUCT_VERSION_STALE: "商品资料已变化，请刷新后重试",
  COMMERCE_PROJECT_LOCKED: "项目生产配置正在切换，请稍后重试",
  COMMERCE_PROJECT_NOT_CONFIGURED: "带货项目尚未完成生产配置",
  COMMERCE_PROJECT_REBUILD_BLOCKED: "当前项目换代存在阻断项，请先处理后重试",
  COMMERCE_REVISION_CONFLICT: "带货项目数据已被其他操作更新，请刷新后重试",
  COMMERCE_RUN_STATE_CONFLICT: "任务状态已变化，请刷新后重试",
  COMMERCE_SCRIPT_DURATION_EXCEEDED: "成片目标时长设置无效，请重新选择",
  COMMERCE_SCRIPT_DERIVATION_INVALID: "脚本裂变参数或模型输出无效，请查看具体原因后重试",
  COMMERCE_SCRIPT_DERIVATION_MODEL_UNAVAILABLE: "当前项目没有可用的脚本文本模型，请先配置业务模型",
  COMMERCE_SCRIPT_DERIVATION_NOT_FOUND: "脚本裂变任务不存在或已被清理",
  COMMERCE_SCRIPT_DERIVATION_SOURCE_EMPTY: "源广告脚本正文为空，请先填写脚本",
  COMMERCE_SCRIPT_DERIVATION_STATE_CONFLICT: "脚本裂变任务状态已变化，请刷新后重试",
  COMMERCE_SCRIPT_ORGANIZATION_IN_PROGRESS: "销售脚本正在整理，请稍后查看任务进度",
  COMMERCE_SCRIPT_ORGANIZATION_INVALID: "销售脚本整理结果未通过结构校验，请重试",
  COMMERCE_SCRIPT_ORGANIZATION_REQUIRED: "请先完成销售脚本整理",
  COMMERCE_SCRIPT_PROMPT_TOO_LONG: "广告脚本超过当前视频模型允许的长度，请删减后重试",
  COMMERCE_SCRIPT_REQUIRED: "请先填写广告脚本",
  COMMERCE_SCRIPT_UNIT_REBUILD_BLOCKED: "当前脚本换代存在阻断项，请先处理后重试",
  COMMERCE_SCRIPT_UNIT_REBUILD_REQUIRED: "当前脚本已有生产结果，请确认影响后换代",
  COMMERCE_SCRIPT_UNIT_REBUILD_STALE: "脚本换代影响已失效，请重新检查影响",
  COMMERCE_SCRIPT_UNIT_ARCHIVED: "该广告脚本已归档",
  COMMERCE_SCRIPT_UNIT_GENERATION_MISMATCH: "该操作属于脚本旧生产代，请刷新后重试",
  COMMERCE_SCRIPT_UNIT_REQUIRED: "请选择广告脚本",
  COMMERCE_SCRIPT_UNIT_REVISION_CONFLICT: "广告脚本已被其他操作更新，请刷新后重试",
  COMMERCE_SCRIPT_VERSION_STALE: "广告脚本版本已变化，请刷新后重试",
  COMMERCE_SETUP_ALREADY_ABANDONED: "该创建草稿已放弃，不能继续提交",
  COMMERCE_SETUP_INCOMPLETE: "带货项目创建资料尚未准备完整",
  COMMERCE_SETUP_REVISION_CONFLICT: "创建进度已变化，请刷新后继续",
  COMMERCE_WORKFLOW_TEMPLATE_UNAVAILABLE: "带货视频系统流程当前不可用，请联系管理员检查系统初始化",
  CONTENT_REJECTED: "上游安全策略拒绝了本次内容生成，请调整提示词后重试",
  CURRENT_SCRIPT_VERSION: "当前启用的剧本版本不能归档",
  CUSTOM_ROLE_VALIDATION_FAILED: "自定义角色资料无效，请检查名称、标识和作用范围",
  EMAIL_EXISTS: "该邮箱已被使用",
  EXPORT_NOT_READY: "导出文件尚未准备完成",
  FINAL_VIDEO_NOT_READY: "成片文件尚未准备完成",
  FORBIDDEN: "没有执行此操作的权限",
  INTERNAL_ERROR: "服务内部发生错误，请稍后重试",
  INVALID_CREDENTIALS: "用户名、邮箱或密码错误",
  AUTH_RATE_LIMITED: "尝试次数过多，请稍后再试",
  INVALID_USERNAME: "用户名格式无效或属于系统保留名称",
  INVITATION_CONFLICT: "该邮箱已有有效成员身份或待处理邀请",
  INVITATION_INVALID_OR_EXPIRED: "邀请链接无效、已过期或已被使用",
  INVITATION_VALIDATION_FAILED: "邀请设置无效，请检查角色、资源和有效期",
  INVALID_REQUEST: "请求参数无效，请检查当前设置后重试",
  IMAGE_PROMPT_CONTEXT_CONFLICT: "镜头状态、锁定事实或参考资产存在冲突，请调整镜头或参考资产后重试",
  IMAGE_PROMPT_DIALOGUE_LEAK: "图片提示词包含剧本台词或对白字段，系统已阻止提交",
  IMAGE_PROMPT_OUTPUT_INVALID: "图片提示词 Agent 返回的结构化结果无法解析，请重试失败项",
  IMAGE_PROMPT_REVIEW_EXHAUSTED: "图片提示词经过多轮修正后仍未通过审核，请查看审核意见",
  IDEMPOTENCY_KEY_INVALID: "浏览器无法生成请求标识，请刷新页面后重试",
  LAST_OWNER_BINDING: "不能删除当前组织的最后一个所有者权限",
  LAST_OWNER_REQUIRED: "组织必须保留至少一名有效的直接所有者",
  MEMBER_LIFECYCLE_INVALID: "当前成员状态不支持此操作，请刷新后重试",
  MEMBER_ACCOUNT_PROTECTED: "该成员账号受系统保护，不能通过组织级操作修改",
  MEMBER_PROFILE_VALIDATION_FAILED: "成员资料格式无效，请检查显示名称和头像地址",
  METHOD_NOT_ALLOWED: "当前请求方式不受支持",
  MEDIA_DOWNLOAD_FAILED: "供应商返回的媒体文件下载或转存失败",
  MODEL_CAPABILITY_APPROVAL_REQUIRED: "当前视频模型缺少可执行的时长或分辨率配置，请检查模型配置后重试",
  MODEL_CAPABILITY_UNAVAILABLE: "当前模型不支持所需的生成能力",
  MODEL_NOT_FOUND: "供应商中未找到当前模型",
  MODEL_PROFILE_NOT_CONFIGURED: "当前业务模型尚未配置可用的供应商模型",
  NETWORK_ERROR: "无法连接 CineWeave 服务，请检查网络后重试",
  NOT_FOUND: "指定内容不存在或已被删除",
  ORGANIZATION_REQUIRED: "请先选择组织后再执行此操作",
  ORGANIZATION_SELECTION_INVALID: "组织选择已失效，请重新登录",
  PROFILE_VALIDATION_FAILED: "个人资料格式无效，请检查姓名和头像地址",
  REGISTRATION_UNAVAILABLE: "无法使用这些信息完成注册",
  NO_ACTIVE_ORGANIZATION: "当前账号没有可用组织",
  PROVIDER_OUTPUT_EMPTY: "供应商没有返回有效内容",
  PROVIDER_OUTPUT_INVALID: "供应商返回的内容格式无效",
  PROVIDER_CANCEL_FAILED: "取消供应商任务失败，请稍后重试",
  PROVIDER_CANCEL_OUTCOME_UNKNOWN: "供应商取消结果暂时无法确认，请稍后刷新状态",
  PROVIDER_CIRCUIT_OPEN: "供应商服务连续失败，已暂时停止发送新请求",
  PROVIDER_CONCURRENCY_LIMITED: "供应商并发额度已满，请稍后重试",
  PROVIDER_DAILY_QUOTA_EXCEEDED: "供应商当日请求次数已达上限",
  PROVIDER_GATEWAY_ERROR: "供应商网关调用失败",
  PROVIDER_GATEWAY_REQUIRED: "当前操作必须通过供应商网关执行",
  PROVIDER_GATEWAY_UNAVAILABLE: "供应商网关当前不可用",
  PROVIDER_IDEMPOTENCY_CONFLICT: "相同请求标识对应了不同内容，请刷新后重试",
  PROVIDER_INSTALL_FAILED: "供应商预设安装失败",
  PROVIDER_LEASE_EXPIRED: "供应商调用凭证租约已过期，请重试",
  PROVIDER_MANIFEST_INVALID: "供应商接口配置无效",
  PROVIDER_MODEL_TEMPLATE_INVALID: "供应商模型模板无效",
  PROVIDER_MONTHLY_BUDGET_EXCEEDED: "旧版本地金额门禁已停用，请重新提交任务",
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
  PASSWORD_RESET_INVALID_OR_EXPIRED: "密码重置链接无效、已过期或已被使用",
  PASSWORD_RESET_VALIDATION_FAILED: "新密码不符合要求，请设置 8 至 72 个字符",
  RENDER_PLAN_REPLAN_REQUIRED: "没有可执行的已审核视频提示词，请先批量生成并通过审核后再生成视频",
  PRODUCTION_GENERATION_MISMATCH: "当前操作属于旧的视频生产代，请刷新后在当前生产代重新执行",
  PROJECT_KIND_CONFIGURATION_INVALID: "项目类型配置无效",
  PROJECT_KIND_MISMATCH: "当前项目类型不支持此操作",
  AGENT_IMAGE_ATTACHMENT_EXPIRED: "助手图片上传凭据已失效，请重新选择图片",
  AGENT_IMAGE_ATTACHMENT_NOT_FOUND: "助手图片不存在或已被清理",
  AGENT_IMAGE_ATTACHMENT_NOT_READY: "助手图片尚未完成入库",
  AGENT_IMAGE_ATTACHMENTS_INVALID: "助手图片附件格式无效",
  AGENT_IMAGE_ATTACHMENTS_LIMIT_EXCEEDED: "一次最多附加 8 张图片",
  PROJECT_DELETION_ALREADY_RUNNING: "该项目已有删除任务正在执行",
  PROJECT_DELETION_BLOCKED: "项目状态已变化，请重新确认删除",
  PROJECT_DELETION_IMPACT_STALE: "项目删除影响已变化，请重新加载后确认",
  PROJECT_DELETION_IN_PROGRESS: "项目正在删除，不能继续操作",
  PROJECT_DELETION_RETRY_NOT_ALLOWED: "当前删除任务不能重试",
  PROJECT_NAME_CONFIRMATION_MISMATCH: "输入的项目名称不匹配",
  VIDEO_PRODUCTION_RECONFIGURATION_REQUIRED: "该设置会影响视频生产，请先分析影响并确认换代",
  RATE_LIMITED: "请求过于频繁，请稍后重试",
  ROLE_IN_USE: "该角色仍有有效绑定，请先撤销全部绑定",
  ROLE_KEY_EXISTS: "该角色标识已存在或属于系统保留标识",
  ROLE_PERMISSION_NOT_ALLOWED: "所选权限不能分配给当前角色范围",
  ROLE_SCOPE_IN_USE: "角色仍有绑定，不能修改作用范围",
  SYSTEM_ROLE_IMMUTABLE: "系统角色为只读，不能修改或删除",
  SYSTEM_ADMINISTRATOR_REQUIRED: "仅系统管理员可以执行此操作",
  SYSTEM_MEMBER_CONFLICT: "该账号已经是此组织的成员",
  SYSTEM_MEMBER_NOT_FOUND: "未找到该用户名或邮箱对应的有效账号",
  SYSTEM_MEMBER_VALIDATION_FAILED: "成员资料无效，请检查账号、密码和成员状态",
  SYSTEM_ORGANIZATION_VALIDATION_FAILED: "组织资料无效，请检查组织名称、默认工作区和初始所有者",
  SYSTEM_OWNER_NOT_FOUND: "未找到该用户名或邮箱对应的有效用户",
  PROVIDER_MODEL_ALREADY_EXISTS: "同一供应商账号下已存在该模型 ID，请编辑已有模型或使用其他 ID",
  PROVIDER_MODEL_IN_USE: "该模型仍有运行中的请求或任务，请等待任务结束或取消后再删除",
  RESULT_EXPIRED: "供应商生成结果已过期，请重新生成",
  QUOTA_EXCEEDED: "供应商额度已用尽",
  SETUP_ALREADY_COMPLETED: "系统已经完成初始化",
  USERNAME_EXISTS: "该用户名已被使用",
  SHOT_IMAGE_PROMPT_RUNNING: "镜头图片提示词正在生成，完成前不能修改图片提示词设置",
  SHOT_IMAGE_RUNNING: "镜头图片正在生成，完成前不能修改图片生成设置",
  SHOT_ASSET_REQUIREMENT_REVIEW_REQUIRED: "镜头资产需求尚未完成结构化校验和确认，请先在资产页批量确认",
  SHOT_ASSET_REQUIREMENT_TYPE_MISMATCH: "镜头资产需求类型与关联核心资产不匹配，请修正资产关联或需求类型",
  CANONICAL_ASSET_ARCHIVED: "关联的核心资产已归档，请改用当前可用资产",
  CANONICAL_ASSET_STALE: "关联的核心资产已过期，请先更新资产后再继续",
  STORYBOARD_REGENERATION_REQUIRED: "上游分镜已变化，请先重新生成或确认分镜后再处理镜头资产需求",
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
  UPLOAD_NETWORK_ERROR: "无法连接对象存储，请检查网络或存储域名配置后重试",
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
  DERIVED_ASSET_BASE_REFERENCE_REQUIRED: "基础资产没有可用参考图，不能生成镜头衍生资产",
  DERIVED_ASSET_REQUIREMENT_NOT_FOUND: "未找到对应的镜头衍生资产需求",
  DERIVED_ASSET_ALREADY_RUNNING: "该镜头衍生资产正在生成，请等待当前任务结束",
  DERIVED_ASSET_SOURCE_CHANGED: "衍生资产生成输入已变化，请重新创建任务",
  DERIVED_ASSET_TARGET_CHANGED: "衍生资产目标已被其他操作修改，本次结果未写入",
  DERIVED_ASSET_RESULT_DISCARDED: "衍生资产执行结果已过期，本次结果未写入",
  DERIVED_ASSET_WORKSET_EMPTY: "没有符合条件的镜头衍生资产需求",
  DERIVED_ASSET_WORKSET_TOO_LARGE: "单次镜头衍生资产任务数量过多，请缩小选择范围",
  DERIVED_ASSET_RETRY_EMPTY: "该任务没有可重试的失败或阻塞项",
  INVALID_DERIVED_ASSET_BATCH: "镜头衍生资产批量任务参数无效",
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
  "identifier or password is invalid": "用户名、邮箱或密码错误",
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
    pattern: /image prompt agent returned invalid JSON/i,
    message: "图片提示词 Agent 返回的结构化结果无法解析，请重试失败项",
  },
  {
    pattern: /image prompt review agent returned invalid JSON/i,
    message: "图片提示词审核 Agent 返回的结构化结果无法解析，请重试失败项",
  },
  {
    pattern: /image prompt review did not approve/i,
    message: "图片提示词未通过审核，请查看审核意见并重试失败项",
  },
  {
    pattern: /image prompt must (not contain script dialogue|contain visual instructions only)/i,
    message: "图片提示词包含剧本台词或对白元数据，必须改写为纯视觉描述",
  },
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
  {
    pattern: /activity (StartToClose|ScheduleToStart|ScheduleToClose|Heartbeat) timeout/i,
    message: "任务步骤等待响应超时，请重试",
  },
];

const CHINESE_TEXT = /[\u3400-\u9fff]/;
const CODE_PREFIX = /^([A-Z][A-Z0-9_]+)\s*[:：]\s*(.+)$/s;
const TEMPORAL_ERROR_WRAPPER = /activity error|child workflow error|workflow execution error|scheduledEventID|startedEventID|\(type:\s*[A-Z][A-Z0-9_]+/i;

function embeddedPlatformErrorCode(message: string): string | undefined {
  if (!TEMPORAL_ERROR_WRAPPER.test(message)) {
    return undefined;
  }
  return Object.keys(ERROR_CODE_MESSAGES)
    .sort((left, right) => right.length - left.length)
    .find((candidate) => new RegExp(`\\b${candidate}\\b`).test(message));
}

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

  const embeddedCode = embeddedPlatformErrorCode(source);
  if (embeddedCode) {
    return ERROR_CODE_MESSAGES[embeddedCode];
  }

  const videoTaskTimeout = source.match(/^Video task exceeded total timeout after (\d+) seconds$/i);
  if (videoTaskTimeout) {
    return `视频任务在上游排队超过 ${videoTaskTimeout[1]} 秒后超时`;
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
