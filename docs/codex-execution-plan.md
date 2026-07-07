# CineWeave Codex 当前执行计划

本文档是 Codex 执行入口，用于记录当前项目状态、下一阶段任务顺序和验收命令。详细任务拆解维护在 `docs/follow-up-development-plan.md`。

## 根决策

- 仓库根目录固定为 `D:\Code\CineWeave`。
- 不创建嵌套 `cineweave/` 目录。
- 项目处于开发阶段，不做旧 demo、旧数据、旧 TypeScript 供应商脚本兼容。
- Worker、API Server 不得直接调用上游供应商；所有 AI 调用必须经 Provider Gateway。
- 当前优先部署方式为 Docker Compose：`docker compose -f compose.yml --profile app up -d --build`。

## 当前状态

- 基础 monorepo、Docker Compose、API Server、Provider Gateway、Worker 和 Web 已进入 MVP 开发阶段。
- `mock-provider` 已从 CI 构建流程移除。
- `pnpm run test` 已统一覆盖 Go tests、Web typecheck/lint、OpenAPI YAML parse、OpenAPI route check 和 Compose config。
- OpenAPI route check 已接入 CI，公开 API / Realtime 路由必须与 Go mux 注册保持一致。
- `CINEWEAVE_ENV=production` 下，API 和 Provider Gateway 已对 JWT secret、service token、credential master key 做启动时 fail fast 校验。
- API、Realtime、Provider Gateway、Web、Worker 和 Event Publisher 已配置 Compose healthcheck 与 `restart: unless-stopped`。
- API `/readyz` 会检查数据库、存储、Temporal 客户端和 Provider Gateway `/readyz`；Realtime 与 Provider Gateway `/readyz` 会检查自身关键依赖。
- Compose 外部运行时镜像已使用 tag+digest 固定，避免服务器部署拉取浮动镜像。
- Provider 管理 API 的 OpenAPI schema 与前端 API client 已补齐到当前后端路由：connector import、provider call logs、usage summary、limit policies、circuit states、model profile bindings。
- Provider 模型能力写入路径已统一规范化：手工创建、Catalog 安装和远程发现同步都会补齐 `xCapabilities` 的流式、思考、多模态、异步任务、参考图/视频、请求方式、响应格式和分辨率等默认声明。
- 供应商中心的模型添加/编辑弹窗已改为结构化能力与限制编辑，不再把输入限制、输出限制、质量档位和 provider options 作为可见 JSON 文本框直接暴露给用户。
- 供应商 catalog 已将 OpenAI 兼容入口固定为首位和默认新增类型，并补充 OpenRouter、Ollama、Google Gemini、阿里通义千问、智谱 GLM、百度文心千帆、讯飞星火、MiniMax 的基础接入模板。
- 原文管理已接入编辑和删除；小说原文编辑时默认重新分卷分章节，章节识别已覆盖常见中文网文卷/部/章/节/回/幕和英文 Part/Book/Chapter/Scene 标题。
- Docker Compose 默认只开放浏览器或本机调试需要访问的服务，其余服务优先走 Docker 网络。
- 当前公开宿主端口：
  - Web：`http://localhost:19285`
  - API：`http://localhost:19288`
  - Realtime：`http://localhost:19281`
  - MinIO API：`http://localhost:19290`
- PostgreSQL、Redis、NATS、Temporal、Provider Gateway、Worker、Event Publisher、MinIO Console 默认不映射宿主端口。

## 执行顺序

### P0：验证与发布基线

1. 统一根测试入口，让 `pnpm run test` 覆盖 Go、Web、OpenAPI 和 Compose 基础验证。
2. 增加 OpenAPI 与实际路由一致性检查，避免接口实现和文档继续偏移。

### P1：核心服务与生产链路

1. 完成生产部署硬化：生产密钥 fail fast、healthcheck、restart 策略和基础镜像版本固定。
2. 对齐 Provider 管理 API、OpenAPI schema 和前端 API client。
3. 收敛模型能力声明，确保 UI 展示能力、Gateway 校验和运行时实际支持一致。
4. 完成供应商与模型配置闭环：添加/编辑供应商、发现模型、自定义模型、编辑模型、删除模型、可视化能力和限制配置。
5. 扩展常用渠道连接器，OpenAI-compatible 放首位并作为默认渠道，后续补齐 Ollama、百度文心千帆、阿里通义千问、讯飞星火、智谱 GLM、Google Gemini、OpenRouter、MiniMax。
6. 优化 Workflow 状态和失败详情，让生产看板、Workflow 页面能展示当前步骤、错误、重试和取消动作。

### P2：前端产品闭环

1. 完成原文与剧本管理：原文可编辑可删除，小说上传自动分卷分章节，剧本版本和场景可编辑管理。
2. 完成时间线最小编辑闭环：clip 创建、编辑、删除、排序、合成版本和下载入口。
3. 完成资产管理闭环：资产字段编辑、参考图上传、主参考图、审核和批量生成。
4. 完成分镜安全编辑：新建、编辑、删除确认、重排和审阅入口。
5. 完成 Prompt 与 RBAC 管理面：提示词版本、绑定、成员、团队、角色和权限配置。
6. 完成全站中文标签映射，项目概览、生产看板、原文与剧本、供应商中心、提示词中心、权限管理不直接显示英文内部枚举。
7. 收口文档状态，避免 README、docs 和 dev_docs 把未来规划写成已实现能力。

## 下一批 Codex 任务

1. 先做全站中文标签映射。
2. 再做 Workflow 状态和失败详情 UX。
3. 再做时间线、资产、分镜的编辑闭环。

## 验收命令

每个实现任务完成后按改动范围运行必要验证。基础验证命令如下：

```powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
@'
import yaml
with open('packages/openapi/openapi.yaml', 'r', encoding='utf-8') as f:
    yaml.safe_load(f)
print('ok')
'@ | python -
docker compose -f compose.yml config --quiet
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

## 维护规则

- `docs/codex-execution-plan.md` 只写当前执行总览、阶段顺序和下一批任务。
- 详细任务步骤、涉及文件和验收标准维护在 `docs/follow-up-development-plan.md`。
- 每完成一个阶段后，同步更新本文档的“当前状态”和“下一批 Codex 任务”。
