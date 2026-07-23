# CineWeave 后续开发执行计划

本文档记录当前 MVP 之后需要继续落实的工程任务。`mock-provider` 从 CI 移除、Docker Compose 默认端口与 host 映射收口已在当前修复中处理，不再列入本计划。

## 2026-07-23 Commerce 发布收口

带货视频的详细领域、API、Workflow 和验收标准维护在 `docs/commerce-video-development-plan.md`，本文只保留跨模块发布任务：

- [x] 源码 migration/baseline 扩展至 `000056`，并完成隔离 Up/Down/Up 与 schema 等价验证。
- [x] Commerce 项目、商品、多脚本、多语言、分镜、参考图、视频提示词、镜头视频、时间线、成片和 Project Agent 已接真实 API/Workflow。
- [x] OpenAPI/API client/事件目录收口至 393 条公开路由。
- [x] Playwright Chromium E2E 固化到 CI 和 release check；Provider Gateway 图片请求 contract test 已落地。
- [x] 新增 `scripts/test-runtime-hardening.ps1 -CommerceOnly` 隔离专项入口。
- [x] 新增带显式费用确认的 `scripts/smoke-commerce-real-provider.ps1`，证据默认写入 `tmp/` 且不保存凭据。
- [ ] 在 Provider/Workflow 排空窗口保存配置快照，将主环境从 v44 升级至 v56。
- [ ] 使用真实图片和视频模型完成 3 镜头、失败项重试、非中文原生音频和 provenance 验收。
- [ ] 完成多 ScriptUnit 独立分镜/视频/成片的主环境浏览器 smoke，确认刷新和 Worker 重启后状态可恢复。

以上三项运行态任务完成前，`commerce_video_v1` 不得标记为生产 available；不能用 mock contract 或源码测试替代真实计费链路证据。

## 执行原则

- 每个任务都必须能独立验收，避免只写“优化”或“完善”。
- 优先修复会阻断服务器部署、CI、数据安全和真实生产链路的问题。
- 前端功能必须接真实 API；不新增“开发中”占位入口。
- 不做旧数据兼容；本项目仍处于开发阶段，允许通过迁移或重建开发库收口数据结构。

## P0：验证与发布基线

### P0-1 统一根测试入口

目标：根目录 `pnpm run test` 必须代表项目基础验证，而不是只验证 Web。

涉及文件：

- `package.json`
- `scripts/test.ps1`
- `scripts/test.sh`
- `.github/workflows/ci.yml`

实施步骤：

1. 将根 `test` 脚本改为调用统一测试脚本。
2. 统一测试脚本至少执行：
   - `go test ./...`
   - `pnpm --filter @cineweave/web typecheck`
   - `pnpm --filter @cineweave/web lint`
   - `docker compose -f compose.yml config --quiet`
   - OpenAPI YAML parse
3. CI 改为复用同等命令，避免本地与远端验证不一致。

验收标准：

- 本地 `pnpm run test` 通过。
- CI 中不再重复维护一套不同的验证步骤。
- 失败时能从命令输出直接定位到 Go、Web、OpenAPI 或 Compose。

### P0-2 增加 OpenAPI 与路由一致性检查

目标：避免 API 已实现但 OpenAPI 缺失，或 OpenAPI 描述过期。

涉及文件：

- `internal/api/*`
- `services/provider-gateway/main.go`
- `apps/realtime/main.go`
- `packages/openapi/openapi.yaml`
- `scripts/generate-openapi.*`
- 新增 `scripts/check-openapi-routes.*`

实施步骤：

1. 列出 API Server、Realtime、Provider Gateway 的实际注册路由。
2. 与 `packages/openapi/openapi.yaml` 的 paths 做交叉检查。
3. 明确哪些内部路由不进公开 OpenAPI，但必须在检查脚本中显式 allowlist。
4. 补齐当前已知缺口：
   - `POST /api/projects/{projectId}/assets/{assetId}/generate-image`
   - `/readyz`
   - `/api/realtime/events`
   - Provider Gateway audio/TTS 占位是否删除或标记为未发布内部接口

验收标准：

- 脚本能在 CI 中运行。
- 新增 API 路由时未更新 OpenAPI 会失败。
- OpenAPI server URL 与 Docker Compose 默认 API host 端口一致。

## P1：生产部署硬化

### P1-1 凭据与服务令牌 fail fast

目标：服务器部署时禁止使用开发默认密钥。

涉及文件：

- `internal/provider/vault.go`
- `services/provider-gateway/main.go`
- `apps/api/main.go`
- `compose.yml`
- `.env.example`
- `README.md`

实施步骤：

1. 增加环境判断：`CINEWEAVE_ENV=production` 时必须配置 `CINEWEAVE_CREDENTIAL_MASTER_KEY`。
2. `CINEWEAVE_ENV=production` 时禁止 `CINEWEAVE_SERVICE_TOKEN=dev-service-token`。
3. 明确 JWT/session secret 的生产必填项，如果当前没有独立 secret，需要补齐配置项。
4. 启动失败时输出明确错误，不允许静默退回开发默认值。

验收标准：

- 生产环境缺少 master key 时 API/Gateway 启动失败。
- 生产环境使用默认 service token 时启动失败。
- 开发环境仍可无额外配置快速启动。

### P1-2 应用服务 healthcheck 与 restart 策略

目标：Compose 部署可以准确判断服务是否 ready，并在异常退出时自动恢复。

涉及文件：

- `compose.yml`
- `apps/api/main.go`
- `apps/realtime/main.go`
- `services/provider-gateway/main.go`
- `services/event-publisher/main.go`

实施步骤：

1. 确认 API、Realtime、Provider Gateway 的 `/healthz` 与 `/readyz` 行为。
2. 为 `api`、`realtime`、`provider-gateway` 增加 Compose healthcheck。
3. 为 app profile 服务增加 `restart: unless-stopped`。
4. Worker 类服务增加轻量健康检测或明确使用进程存活作为健康信号。

验收标准：

- `docker compose -f compose.yml --profile app ps` 能显示关键服务 healthy。
- API 在数据库或依赖未 ready 时 `/readyz` 返回非成功状态。
- 依赖恢复后服务能自动恢复到 healthy。

### P1-3 固定基础镜像版本

目标：提高服务器部署可重复性。

涉及文件：

- `compose.yml`
- `apps/web/Dockerfile`
- `deploy/docker-compose/Dockerfile-go`
- `deploy/docker-compose/Dockerfile-media-worker`

实施步骤：

1. 固定 `minio/minio`、`minio/mc` 到明确版本。
2. 评估 `node:24-alpine`、`postgres:16`、`redis:7-alpine`、`nats:2`、`temporalio/auto-setup:1.28` 是否需要 digest pinning。
3. README 说明升级镜像的检查流程。

验收标准：

- Compose 不再使用浮动 `latest`。
- 镜像升级可通过单独 PR 审查。

## P1：Provider Gateway 与模型能力

### P1-4 对齐 Provider OpenAPI 与代码

目标：Provider 管理 API 和 OpenAPI schema 不再偏移。

涉及文件：

- `internal/provider/types.go`
- `internal/api/providers.go`
- `packages/openapi/openapi.yaml`

实施步骤：

1. `ImportProviderConnectorRequest` 补齐 `manifestText`。
2. `ProviderCallLog` schema 补齐实现中已有字段：
   - `projectId`
   - `workflowRunId`
   - `workflowNodeRunId`
   - `modelProfileId`
   - `upstreamStatusCode`
   - `requestSnapshot`
   - `normalizedOutput`
3. 检查 provider limit/circuit/profile/binding 的 request/response 是否与代码一致。

验收标准：

- TypeScript client 类型能覆盖 Provider 页面实际使用字段。
- OpenAPI parse 与路由一致性检查通过。

### P1-5 Declarative Provider 发现与健康测试

目标：Manifest 类供应商不再依赖 OpenAI-compatible `/models` 假设。

涉及文件：

- `internal/provider/gateway.go`
- `internal/provider/manifest.go`
- `internal/provider/service.go`
- `apps/web/src/features/providers/providers-page.tsx`

实施步骤：

1. 为 manifest provider 定义可选 `models` endpoint 或 catalog fallback 行为。
2. 模型发现时按 provider 类型选择：
   - OpenAI-compatible：远程 `/models`
   - Manifest：manifest `models` endpoint 或本地 catalog template
3. 前端区分“远程发现”和“填入预设模型”，避免用户误解。
4. 增加 manifest health test，验证 endpoint、鉴权、responseMapping。

验收标准：

- Volcengine/Kling 这类 manifest provider 点击发现模型时行为明确。
- 发现失败时错误信息指向缺少 models endpoint、鉴权失败或 mapping 错误。

### P1-6 收敛能力声明与运行时支持

目标：模型能力库不能声明当前运行时实际不支持的能力。

涉及文件：

- `internal/provider/model_registry.go`
- `db/migrations/000026_model_capability_registry.up.sql`
- `db/migrations/000027_openrouter_ranked_model_presets.up.sql`
- `internal/provider/openai_compatible.go`
- `packages/openapi/openapi.yaml`

实施步骤：

1. 审核图片能力：
   - reference image
   - edit
   - multi image
   - request mode
2. 当前 runtime 不支持的能力先降级为 false 或 experimental。
3. 如果要支持，补 runtime request builder、tests 和 OpenAPI 示例。
4. 视频能力同样区分 manifest async 与未来 OpenAI-compatible video。

验收标准：

- UI 展示的能力标签与 Gateway 实际可执行能力一致。
- 用户不会因为能力标签误以为某模型可用但运行时报 `UNSUPPORTED_CAPABILITY`。

### P1-7 供应商与模型配置管理闭环

目标：供应商管理页可以完成渠道、模型、能力和限制配置，不需要用户直接编辑 JSON。

涉及文件：

- `apps/web/src/features/providers/providers-page.tsx`
- `apps/web/src/lib/api-client.ts`
- `internal/provider/types.go`
- `internal/api/providers.go`
- `internal/provider/model_registry.go`
- `packages/openapi/openapi.yaml`

实施步骤：

1. 添加供应商和编辑供应商弹窗内补齐模型配置区：
   - 获取可用模型
   - 添加自定义模型
   - 编辑模型
   - 删除模型
   - 启用/禁用模型
2. 模型编辑禁止把 input/output limits 作为默认 JSON 文本框展示，改为结构化表单。
3. 文本模型限制至少支持：
   - 最大输入 token
   - 最大输出 token
   - 是否支持流式输出
   - 是否支持思考等级
   - 思考等级取值范围或枚举
   - 是否多模态
   - 支持的输入文件类型
4. 图片模型限制至少支持：
   - 最大参考图数量
   - 是否支持参考图
   - 是否支持图片编辑
   - prompt 最大长度
   - 支持的请求方式：同步、异步任务、OpenAI-compatible、provider native
   - 支持的图片比例
   - 支持的尺寸或分辨率档位
   - 支持的清晰度档位：标清、高清、其他 provider 原生档位
   - 支持的输入和输出格式
5. 视频模型限制至少支持：
   - prompt 最大长度
   - 最短和最长可生成秒数
   - 可选生成时长枚举
   - 是否支持参考图
   - 是否支持首帧
   - 是否支持尾帧
   - 是否支持视频参考
   - 最大参考图片数量
   - 最大参考视频时长或大小
   - 支持的请求方式：同步、异步任务、轮询、webhook、provider native
   - 是否支持异步任务
   - 支持的比例
   - 支持的清晰度档位：标清、高清、其他 provider 原生档位
   - 支持的输出格式
6. 表单提交时由前端生成结构化 capability/limits payload，后端做 schema 校验并保存。
7. 对仍需要高级字段的 provider native 参数，只允许放入“高级参数”折叠区，并提供字段名、类型、说明和默认值，不使用裸 JSON 作为主编辑方式。
8. 模型发现匹配到内置模型能力库时，自动填充能力和限制；匹配不到时进入自定义模型配置流程。

验收标准：

- 添加或编辑供应商时能完成模型发现、自定义模型添加、模型编辑和模型删除。
- 用户能通过可视化表单配置文本、图片、视频模型限制。
- 保存后的模型能力会影响前端可选项、Gateway 校验和运行时路由。
- 删除或禁用的模型不会出现在默认模型列表、路由候选和测试入口。

### P1-8 扩展常用渠道连接器

目标：在保持 OpenAI-compatible 为首位和默认渠道的前提下，补齐服务器部署常用 AI 服务渠道。

涉及文件：

- `internal/provider/connectors/*`
- `internal/provider/manifest.go`
- `internal/provider/gateway.go`
- `internal/provider/model_registry.go`
- `apps/web/src/features/providers/providers-page.tsx`
- `packages/openapi/openapi.yaml`
- `docs/provider-gateway.md`

实施步骤：

1. 供应商类型列表将 OpenAI-compatible 放在首位，并作为默认添加类型。
2. OpenAI-compatible 覆盖 New API、One API、LiteLLM、OpenAI official 以及其他兼容 `/v1` 协议的网关。
3. 补齐 Ollama：
   - 本地或内网 base URL
   - 模型发现
   - 文本生成
   - 流式输出
   - 多模态能力按模型声明控制
4. 补齐百度文心千帆：
   - credential 字段
   - 模型发现或预设模型
   - 文本能力
   - 图片/视频能力按官方接口实际支持拆分
5. 补齐阿里通义千问：
   - DashScope credential 字段
   - 文本、多模态、图片或视频能力声明
   - 同步和异步任务差异
6. 补齐讯飞星火：
   - credential 字段
   - 文本和流式协议适配
   - 模型预设与能力映射
7. 补齐智谱 GLM：
   - credential 字段
   - 文本、多模态、图片或视频能力声明
   - 模型预设与能力映射
8. 补齐 Google Gemini：
   - credential 字段
   - 文本、多模态、图片能力声明
   - 流式输出和文件输入限制
9. 补齐 OpenRouter：
   - credential 字段
   - `/models` 发现
   - 价格、上下文长度、模态能力映射
   - OpenRouter 排名数据导入到常用模型预设
10. 补齐 MiniMax：
   - credential 字段
   - 文本、语音、图片或视频能力按实际接口声明
   - 异步任务能力和轮询策略
11. 每个渠道都要提供：
   - 新建供应商表单字段
   - credential 加密保存
   - 健康测试
   - 模型发现或预设模型填充
   - provider call log
   - cost record
   - 单元测试或集成测试

验收标准：

- 新建供应商时默认选中 OpenAI-compatible。
- 上述渠道至少能完成创建、编辑、健康测试、模型发现或预设填充。
- 未实现某一模态运行时时，UI 不展示可用能力，Gateway 返回明确 `UNSUPPORTED_CAPABILITY`。

## P1：工作流与生产任务 UX

### P1-9 统一 Workflow 状态与失败详情

目标：用户能看懂生产任务当前在做什么、为什么失败、下一步能做什么。

涉及文件：

- `internal/workflows/*`
- `internal/api/workflows.go`
- `apps/web/src/features/production/production-page.tsx`
- `apps/web/src/features/projects/project-overview-page.tsx`
- `apps/web/src/features/workflows/workflows-page.tsx`

实施步骤：

1. API 返回 workflow run、node run、provider attempt、error code、error message。
2. 前端生产看板展示：
   - 当前步骤
   - 最近错误
   - 可重试动作
   - 取消动作
   - 跳转到详细 run
3. Workflow 页面补节点详情、耗时、输入输出摘要和错误展开。

验收标准：

- 生产失败时用户不需要看日志也能知道失败原因。
- 可取消、可重试的任务在 UI 上有明确动作。

### P1-10 移除硬编码 `maxShots=3`

目标：生产规模由项目或用户输入控制，不在前端 helper 中固定。

涉及文件：

- `apps/web/src/features/production/production-actions.ts`
- `internal/workflows/video.go`
- `internal/api/workflows.go`
- 项目设置相关页面

实施步骤：

1. 在项目设置或开始制作弹窗中提供 `maxShots`。
2. API 校验上限，避免超大任务失控。
3. Workflow 使用 API input 或项目设置，不依赖前端硬编码。

验收标准：

- 前端没有固定 `maxShots: 3`。
- 用户能在受控范围内选择生产镜头数量。

## P2：前端产品闭环

### P2-1 原文到剧本/场景闭环

目标：导入内容后能完成原文、分卷、章节、事件、改编计划、剧本、场景的编辑与审阅。

涉及文件：

- `apps/web/src/features/sources/sources-page.tsx`
- `apps/web/src/lib/api-client.ts`
- `internal/api/sources.go`

实施步骤：

1. 原文支持列表、详情、编辑、删除。
2. 删除原文时同步处理关联导入任务、章节、剧本初始化入口和前端缓存，不保留不可见脏入口。
3. 上传小说原文时自动识别分卷和章节。
4. 章节识别至少支持：
   - 卷标题
   - 章标题
   - 序章、楔子、尾声、番外
   - 中文数字和阿拉伯数字章节号
   - 常见网络小说标题格式
5. 自动切分后提供章节列表预览，允许用户合并、拆分、重命名和重新排序。
6. 上传剧本时识别为剧本格式，不强制按小说章节切分，但需要生成剧本版本和场景初始化入口。
7. 小说事件支持编辑、审核。
8. 改编计划支持编辑、激活、审核。
9. 剧本支持版本列表、创建版本、编辑版本、删除草稿版本、场景列表。
10. 场景支持编辑、审阅、删除、重排、进入 Script Agent。
11. API 增加必要的编辑和删除接口，OpenAPI 与 API client 同步更新。

验收标准：

- 一个用户从文件导入到剧本场景完成，不需要离开系统或手工改数据库。
- 小说上传后能看到分卷和章节结构，并能在 UI 中调整。
- 原文和剧本草稿可编辑、可删除，删除后相关页面不再出现悬空入口。

### P2-2 时间线编辑器最小闭环

目标：时间线不只是查看和合成，能管理 clips。

涉及文件：

- `apps/web/src/features/timeline/timeline-page.tsx`
- `apps/web/src/lib/api-client.ts`
- timeline API handlers

实施步骤：

1. 支持 clip 创建、编辑、删除。
2. 支持拖拽或按钮排序。
3. 支持设置启用状态、入点、出点、转场基础字段。
4. 合成后展示 final video 版本、激活状态和下载入口。

验收标准：

- 用户能用已有镜头视频组装一个可导出的时间线。

### P2-3 资产管理闭环

目标：资产页能完成资产字段编辑、参考图管理和生成。

涉及文件：

- `apps/web/src/features/assets/assets-page.tsx`
- `internal/api/assets.go`
- `internal/api/artifacts.go`

实施步骤：

1. 核心资产字段可编辑。
2. 支持上传参考图。
3. 支持设置主参考图。
4. 支持核心资产审核。
5. 批量生成缺失资产图。

验收标准：

- 资产从剧本解析、人工修订、参考图上传、生成图、审核全部可在 UI 内完成。

### P2-4 分镜页面安全编辑

目标：分镜编辑不会误删，且支持基础管理。

涉及文件：

- `apps/web/src/features/storyboard/storyboard-page.tsx`
- storyboard API handlers

实施步骤：

1. 删除镜头增加确认。
2. 支持新建镜头。
3. 支持重排镜头。
4. 支持编辑标题、描述、visual、duration、shot type。
5. 支持分镜审阅入口。

验收标准：

- 用户能人工整理一组分镜，再触发图像和视频生成。

### P2-5 Prompt 与 RBAC 管理面

目标：管理页面可完成真实后台管理，不只是列表。

涉及文件：

- `apps/web/src/features/prompts/prompts-page.tsx`
- `apps/web/src/features/access/access-page.tsx`
- prompt API handlers
- authz/team/role API handlers

实施步骤：

1. Prompt 支持查看模板、创建版本、编辑草稿、激活版本、项目/组织绑定。
2. RBAC 支持成员列表、邀请/移除、团队成员、角色绑定。
3. 项目设置支持选择模型 profile 和基础生产参数。

验收标准：

- 管理员能不改数据库完成项目生产所需的提示词、权限和模型配置。

### P2-6 全站中文标签映射

目标：前端可见枚举、状态、角色和权限不直接显示英文内部值。

涉及文件：

- `apps/web/src/lib/labels.ts`
- `apps/web/src/features/projects/project-overview-page.tsx`
- `apps/web/src/features/production/production-page.tsx`
- `apps/web/src/features/sources/sources-page.tsx`
- `apps/web/src/features/providers/providers-page.tsx`
- `apps/web/src/features/prompts/prompts-page.tsx`
- `apps/web/src/features/access/access-page.tsx`
- 相关 shared UI badge/status 组件

实施步骤：

1. 建立统一中文映射模块，禁止各页面散落硬编码映射。
2. 项目概览进度标签补齐中文：
   - production status
   - workflow status
   - project status
   - asset status
3. 生产看板卡片补齐中文：
   - 节点类型
   - 运行状态
   - 失败类型
   - 可执行动作
4. 原文与剧本页补齐中文：
   - source type
   - file format
   - script version status
   - scene status
5. 供应商中心补齐中文：
   - 渠道类型
   - 模态
   - 模型状态
   - 能力标签
   - 限制类型
6. 提示词中心补齐中文：
   - prompt scope
   - prompt type
   - version status
   - binding status
7. 权限管理补齐中文：
   - role name
   - permission key
   - membership status
   - team role
8. 对未知枚举提供统一 fallback：显示“未知状态”并保留 tooltip 或调试信息，不把原始英文值直接暴露给普通用户。
9. 增加前端测试或静态检查，扫描关键页面中不应直接渲染的内部枚举值。

验收标准：

- 项目概览、生产看板、原文与剧本、供应商中心、提示词中心、权限管理不再直接显示英文枚举。
- 新增枚举时必须在统一映射模块补中文文案。
- 未知值不会破坏页面布局，也不会显示裸英文内部 key。

## P2：文档收口

### P2-7 更新文档状态

目标：文档准确反映当前代码，不把未来规划写成已实现。

涉及文件：

- `README.md`
- `docs/codex-execution-plan.md`
- `docs/provider-gateway.md`
- `docs/frontend-spec.md`
- `dev_docs/cineweave-technical-spec-codex-reviewed-v4.md`

实施步骤：

1. `dev_docs` 明确标注为历史规格或长期规格。
2. `docs/codex-execution-plan.md` 改成当前任务状态与下一阶段计划。
3. README 移除未落地的 Kubernetes/Helm/ingress 已实现暗示，或补充为未来部署目标。
4. 端口、compose profile、公开服务、内部服务说明保持一致。

验收标准：

- 新开发者只读 README 和 docs 就能正确启动、配置供应商、运行基础验证。
- 文档会准确标记已实现的角色 TTS/ASR 音频链路；K8s、完整 RBAC、完整时间线编辑器仍按实际状态描述。

## P3：未来成本预估与执行审批

### P3-1 RenderPlan 成本与时间预演

目标：在真实批量视频生产前给出可审计的预计请求数、成本范围、排队时间和额度可行性，不改变 Provider Gateway 对真实成本记录的所有权。

实施步骤：

1. Provider Gateway 根据不可变 capability snapshot、RenderSegment 数量、候选模型价格和当前配额计算成本区间。
2. 预演结果记录模型候选、价格版本、预计请求数、预计生成时长、预算和 quota 校验时间。
3. 项目页面与 Agent 在批量视频执行前展示预演；价格、候选模型或镜头计划变化时使预演失效并重新计算。
4. `require_approval` 模式必须人工批准；`auto_approve` 和 `full_access` 仍受项目预算、组织额度和 Provider Gateway 硬限制。
5. 真实执行继续由 `provider_call_logs` 与 `cost_records` 记录实际用量，预演记录不得冒充账单。

验收标准：

- 任何批量视频计划都能返回预计请求数和成本范围。
- 预演失效后不能沿用旧审批直接执行。
- 预算或 quota 不满足时在调用上游前阻断。
