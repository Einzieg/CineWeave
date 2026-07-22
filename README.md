# CineWeave

CineWeave 是面向长篇内容改编的 AI 视频生产平台。系统以项目、分集剧本、资产、分镜、镜头媒体和成片为主线，使用 Provider Gateway 统一接入模型，使用 Temporal 承载可恢复的长时间生产任务。

仓库根目录就是当前目录，不要创建嵌套的 `cineweave/` 或 `CineWeave/` 目录。

## 运行要求

- Docker Engine 与 Docker Compose v2
- Go `1.26.5+`
- Node.js `24`
- pnpm `10.32.1`
- Python 3，包含 PyYAML（仅 OpenAPI 校验使用）

## 快速启动

```powershell
pnpm install --frozen-lockfile
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

默认只向宿主机开放浏览器或外部客户端需要访问的服务：

- Web：`http://localhost:19285`
- API：`http://localhost:19288`
- Realtime：`http://localhost:19281/api/realtime/events`
- MinIO API：`http://localhost:19290`

PostgreSQL、Redis、NATS、Temporal、Provider Gateway、Workers、Event Publisher 和 MinIO Console 只在 Docker 网络内通信。

空数据库首次启动时访问 `http://localhost:19285/setup` 创建系统管理员。服务器部署应保持 `CINEWEAVE_ALLOW_PUBLIC_REGISTRATION=false`，并替换 `.env.example` 中的开发密钥。

## 架构边界

- API Server 和 Worker 不得直接调用上游 AI 服务，也不得解密供应商凭据。
- Provider Gateway 负责凭据解密、模型路由、上游调用、错误归一化、并发与额度控制、调用日志、成本记录和异步任务。
- Temporal Workflow 负责任务编排、等待、重试、取消、Continue-As-New 和结果提交。
- Media Worker 负责 FFmpeg、对象存储和媒体处理，不接触供应商凭据。
- PostgreSQL 保存业务状态；对象媒体存入 S3/MinIO；前端通过短时 signed URL 预览。

## 生产主线

当前项目主线是：

```text
原文导入与分卷分集
  -> 忠实分集剧本
  -> Canonical Assets 与衍生资产
  -> 分集分镜与镜头状态
  -> 镜头图、视频提示词与视频
  -> 时间线、审阅与成片
```

图像、视频、音频任务均经 Provider Gateway。长批次必须是可恢复、可取消、可按失败项重试的独立执行单元，前端任务活动通过 Realtime 同步状态。

## Provider 与模型

OpenAI-compatible 是默认渠道类型，可用于 OpenAI official、New API、One API、LiteLLM 及兼容网关。Provider Center 同时支持平台预设、声明式 Manifest、多 API Key、按凭据发现模型和模型能力配置。

模型能力是运行时契约的一部分，覆盖文本流式/推理等级/多模态、图片参考与质量档位、视频异步任务/参考输入/时长/比例/分辨率等。业务模型通过 Model Profile 绑定，路由、优先级、权重、启停和 fallback 均由 Provider Gateway 执行。

## 数据库

应用启动使用嵌入式 Goose 迁移和独立系统 Seed：

```powershell
go run ./cmd/cineweave-migrate validate
go run ./cmd/cineweave-migrate up
go run ./cmd/cineweave-seed apply
```

已应用迁移及其 hash 不得改写。当前迁移链和整合发布基线的说明见 [正式发布准备](docs/release-readiness.md) 与 [ADR 0002](docs/adr/0002-database-migrations-and-system-seeds.md)。

## 验证

日常全仓验证：

```powershell
pnpm run test
```

正式发布检查：

```powershell
pnpm run release:check
```

发布检查包含 Go/Web/OpenAPI/Compose 测试、安全审计、生产构建、隔离迁移往返和整合基线等价性。运行服务更新前还需执行 Provider drain 与配置快照保护，具体命令见 [docs/release-readiness.md](docs/release-readiness.md)。

## 目录

- `apps/api`：公共 API Server。
- `apps/realtime`：Realtime 事件网关。
- `apps/web`：Next.js 项目工作台。
- `services/provider-gateway`：唯一上游 AI 访问边界。
- `workers`：Temporal Worker 入口。
- `internal`：Go 领域模块与运行时实现。
- `packages/openapi`：公共 API 契约。
- `packages/events`：事件目录及生成契约。
- `db/migrations`：不可变升级链。
- `db/baselines/current`：当前整合发布基线。
- `db/seeds`：幂等系统数据。
- `deploy`：Compose/Kubernetes/Helm 运行资产。
- `docs`：架构、运行手册与执行计划。

## 主要文档

- [当前 Codex 执行计划](docs/codex-execution-plan.md)
- [后续开发计划](docs/follow-up-development-plan.md)
- [Provider Gateway](docs/provider-gateway.md)
- [工作流引擎](docs/workflow-engine.md)
- [正式发布准备](docs/release-readiness.md)
