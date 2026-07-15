# CineWeave 运行时基础设施重构进度

目标规格：`docs/runtime-foundation-hardening-target.md`
基线日期：2026-07-14
基线分支与提交：`main@182d7a3`

本文件只记录完成证据。任务只有在代码、测试和目标规格验收同时满足后才能标记为完成。

## T0：基线与决策锁定

- [x] 创建运行时基础设施重构目标文档。
- [x] 记录当前工作树基线：308 个变更条目、173 个未跟踪条目、0 个 staged 条目。
- [x] 新增 Provider 安全出站与流完整性 ADR。
- [x] 新增数据库迁移与系统 Seed ADR。
- [x] 新增 Workflow Start Outbox 与持久批任务 ADR。
- [x] 新增 Temporal Worker 发布版本 ADR。
- [x] 运行根基线测试：`pnpm run test` 通过；Web lint 有 3 条既有 `<img>` warning，无 error。

## T1：Provider 安全和流完整性

- [x] 实现统一 MediaFetcher、NetworkPolicy、Resolver 和固定 IP dialer；逐跳重验重定向且不继承通用代理。
- [x] 替换图片、视频、外部参考图下载，并将 TTS 二进制响应改为同一有界 MIME/hash 临时文件转存路径；大型视频不再整体驻留内存。
- [x] 实现有界 SSE 行读取和事件大小限制，不再吞掉 `io.ErrUnexpectedEOF`。
- [x] 实现 OpenAI-compatible 明确终态检查、模型级 `streamTerminalMode`、`provider.failed` 和 `UPSTREAM_STREAM_TRUNCATED`。
- [x] 增加 DNS rebinding、重定向、私网、MIME、字节上限、截断、缺失终态和超长事件测试。
- [x] 通过 Provider 定向测试和 `pnpm run test`；OpenAPI 253 条路由一致，Compose 配置有效，Web lint 仅 3 条既有 warning。

## T2：新数据库基线与 Seed

- [x] 引入固定版 Goose、嵌入式 `cmd/cineweave-migrate`、advisory lock 和独立迁移审计 schema。
- [x] 建立新的 `000001_current_schema.sql` 与 `000002_runtime_hardening.sql` schema 基线。
- [x] 拆分 Provider Catalog、模型能力、Prompt Registry、项目手册和 RBAC 五类版本化 Seed。
- [x] 实现 `cmd/cineweave-seed`、稳定 key、`managed_by=system`、整行 verify 和内容 hash 校验。
- [x] 删除 134 个旧 up/down 文件、旧 `schema_migrations` 所有权和两个 shell runner；PowerShell 入口调用同一 Go 二进制。
- [x] 受控取消旧 Agent、创建临时 custom dump 后重建开发数据库；空库 up/seed、down-to-zero、再次 up/seed 的规范化 schema SHA-256 一致（`D273586C...EE160`）。
- [x] 验证生产 down 返回非零、已应用迁移 hash 篡改返回非零；Compose migrate/seed Exit 0，五类 Seed 行数与导出基线一致。
- [x] `pnpm run test` 通过；Web lint 仅 3 条既有 `<img>` warning。

## T3：Provider 幂等与 Workflow Start Outbox

- [x] 新增 `provider_requests` 和真实逻辑幂等；同 key/同 hash 重放结果，同 key/异 hash 冲突，显式 `retry` 才增加 attempt generation。
- [x] 实现 running call 预写入和 `unknown_outcome`；文本、流、图片、音频、视频创建/轮询/取消与模型发现均使用逻辑请求状态机。
- [x] 新增 `workflow_start_outbox`、集中式 Workflow 注册表、API 内 Starter/queued reconciler；通用、生产、音频、导出和 Project Agent 启动均改为事务入队并返回 `202`。
- [x] 增加崩溃 lease 恢复、AlreadyStarted 对账、并发领取、输入 hash 防篡改、HTTP 幂等原子快照、Provider 并发幂等和重复计费防护测试。
- [x] 更新 OpenAPI、Web Provider 调用日志类型、中文 `unknown_outcome` 标签、Provider Gateway 文档和 Compose Dispatcher 环境变量。
- [x] 验证 `go test ./internal/api ./internal/provider`、`go vet ./internal/api ./internal/provider`、OpenAPI parse/253 路由一致、Web typecheck；数据库专项测试与受影响 API 精确集成集通过。
- [x] 重建 API/Provider Gateway 容器并验证 healthy；运行态 Outbox smoke 在一次领取后由 `queued/pending` 明确收敛为 `failed/WORKFLOW_START_HANDLER_UNKNOWN`，测试记录已清理。

## T4：Temporal 发布体系

- [x] Temporal Server 升级为 `1.31.2@sha256:b5ec...a1da`；显式 Schema one-shot 将主库升级至 `1.19`、可见性库升级至 `1.14`，Server 不再运行 `auto-setup`。
- [x] Worker Build ID 统一来自不可变 `CINEWEAVE_RELEASE_ID`；生产缺失、禁用 Versioning 或使用 `local-dev/latest` 等可变值时 fail fast。
- [x] `RunTemporalWorker` 不再接收 Temporal Client，启动路径只注册 Deployment Version，所有自动 promotion 代码与超时配置已删除。
- [x] 新增 `cmd/temporal-release`，实现 `check/ramp/promote/drain/rollback`、冲突令牌、安全门禁和稳定排空观察；定向测试覆盖率 78.3%。
- [x] Project Agent 拆到 `cineweave-agent` 独立队列与 `agent-worker` AutoUpgrade Deployment；Script/Media/Audio 使用 Pinned，并共享同一 release ID。
- [x] 基础 Compose 使用显式 `cineweave_internal` 网络；新增独立 `worker-release.compose.yml`，要求唯一 project name、digest 镜像和生产 secrets，且不映射宿主端口。
- [x] 新增发布 Canary Workflow、`workflow.GetVersion` 标记与 7.6 KB history fixture；真实 `1.31.2` 集成测试验证旧 Pinned 任务跨晋升仍由旧 Build ID 完成、新任务进入新 Build ID并成功 replay。
- [x] 验证 `go test/go vet` 覆盖 release/schema/namespace、全部 Worker 与 workflows；四个 `local-dev` Deployment Version 均注册并由独立 CLI 晋升为 Current，API `/readyz` 的 database/providerGateway/storage/temporal 全部为 `ok`。

## T5：持久化资产批处理与前端切换

- [x] 实现资产 Prompt 与图片父子 Workflow；每项使用确定性 child ID、独立节点、默认并发 5 和 Provider 幂等键。
- [x] 增加 `partial_succeeded`、失败项链式重试和取消传播；修复 outbox 已领取但 Temporal 尚未注册时的取消竞态，并增加启动/取消行锁栅栏测试。
- [x] 增加项目、资产与 prompt revision，快照视觉手册/Prompt/模型 profile/比例；旧结果通过 CAS 进入冲突历史而不覆盖用户编辑。
- [x] 新增批处理、run/nodes、retry/cancel API，OpenAPI 与前端 client/types 同步，数据库专项集成测试通过。
- [x] 删除浏览器本地批处理执行器和 Zustand 权威任务状态；任务活动统一查询服务端 Workflow 投影并由 Realtime 失效缓存。
- [x] 真实运行 smoke：3 项 Prompt 并发全部成功；3 项图片批次得到 2 成功/1 失败的 `partial_succeeded`，失败项创建两代关联重试并最终成功。
- [x] 重新登录后按 run ID 恢复同一进度；取消 smoke 收敛为 `cancelled`，三个节点均为 `USER_CANCELLED`，不再停留在 `cancelling`。
- [x] 修复角色四视图将 3:2 尺寸误标为 16:9 的契约错误，改为原生 `2048x1152`，不做本地裁切；真实重试通过 Gateway 输出比例校验。

## T6：清理与完整验收

- [x] 完成重复实现审计：外部媒体统一由 `outbound.MediaFetcher` 下载；旧迁移和 shell runner 已删除；浏览器批处理执行器、Zustand 权威任务状态和 Worker 启动自动 promotion 均不存在。
- [x] 补齐 Prometheus 指标、请求/Workflow/Provider 关联结构化日志、告警规则和 `docs/runtime-foundation-hardening-runbook.md`；`temporal-release` 以内部网络 one-shot `ops` 服务运行。
- [x] `pnpm run test` 与 `go vet ./...` 通过；Go、Web typecheck、Web lint、OpenAPI route check 全部成功，lint 仅有 3 条既有 `<img>` warning。
- [x] Migration/Seed 容器 `verify`、OpenAPI YAML parse、255 条路由一致性和 Compose config 均通过；修复 Seed 将触发器维护的 `updated_at` 误判为内容漂移的问题，重复 apply 不再产生无效更新。
- [x] `docker compose -f compose.yml --profile app up -d --build` 后 Web、API、Realtime、Provider Gateway、Workers、PostgreSQL、Redis、Temporal、MinIO 均为 Up/healthy；API readiness 的 database/providerGateway/storage/temporal 全部为 `ok`。
- [x] 真实文本流成功并记录 Provider 请求/调用/终态指标；既有真实 PNG、MP4 通过 Signed GET 与 Range 读取；Prompt 批次 3/3 成功、图片批次 2/3 `partial_succeeded`、失败项链式重试最终成功、取消批次三个节点均 `USER_CANCELLED`，重新登录后可恢复同一批次及节点进度。
- [x] 修复 31 个集成测试在 `t.Cleanup` 前关闭数据库连接导致清理静默失败的问题；清除精确识别的测试残留后，活动 Workflow Start Outbox、Workflow Run、Provider Request 和 Provider Async Task 均为 0。

## T7：运行时评审修复收口

- [x] Realtime 改为鉴权、项目授权和持久 cursor 的 `project-events.v2`；500 条事件分页补发、游标过期、跨租户隐藏、schema/revision envelope 和服务重启后续传均通过。
- [x] Workflow Run/Node 增加 generation/token 写栅栏、终态 CAS、确定性取消 deadline 和 reconciler；项目 revision、配置快照、运行创建、幂等结果及 outbox 位于统一事务边界。
- [x] Source-to-Script 使用独立分集 child、稳定 Provider 逻辑键、有界 Activity retry、Continue-As-New 和失败分集新 generation 重试；Provider/HTTP unknown outcome 不会被自动重发。
- [x] Provider text stream v2 明确 generation/attempt/sequence，首个 delta 后禁止 fallback，成功重放只发 `provider.replayed`；所有项目事件通过 catalog emitter，前端 invalidation map 由生成类型强制穷尽。
- [x] 前端常态更新由 Realtime 驱动，Workflow/计划/Agent 查询只在运行态或活动面板中保留低频兜底；连接 ready、schema 不匹配和 cursor expiry 会恢复权威 REST 状态。
- [x] 新增隔离的 `scripts/test-migrations.ps1` 与 `scripts/test-runtime-hardening.ps1`；100 资产、并发 5、90/10 部分完成、只重试失败项、Worker/Realtime 恢复、响应丢失、取消和 SSE 重连矩阵通过。
- [x] 完整 Linux race、`pnpm run test`、OpenAPI 257 路由一致性、Compose 重建和真实容器重启恢复通过；修复共享测试 fixture 的孤立账号泄漏并清除精确测试残留。
