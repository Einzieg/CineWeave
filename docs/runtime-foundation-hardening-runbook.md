# CineWeave 运行时部署与故障处置手册

本文是 `docs/runtime-foundation-hardening-target.md` 的运行手册，适用于 Docker Compose 服务器部署、发布、监控与故障处置。详细实现证据记录在 `docs/runtime-foundation-hardening-progress.md`。

## 1. 不可破坏的边界

- API Server、Script Worker、Agent Worker、Media Worker 和 Audio Worker 不得直接调用上游 AI 供应商，也不得解密供应商凭据。
- 所有文本、图片、音频和视频供应商调用必须经过内部 `provider-gateway:8082`。
- Provider Gateway、PostgreSQL、Redis、NATS、Temporal、Workers 和 MinIO Console 默认不映射宿主机端口。
- Provider Request 的 `unknown_outcome` 不得自动重试。先确认上游是否已执行和是否已计费，再使用显式 retry。
- Workflow Start 只能通过 `workflow_start_outbox` 启动。API 写入 `workflow_runs` 成功不等于 Temporal 已接收。
- 浏览器只查询批任务状态，不拥有并发执行权和权威进度。
- Worker 启动不得修改 Temporal Current/Ramping Version。发布路由只由 `cmd/temporal-release` 修改。
- 生产环境禁止数据库 down/reset；修复 schema 只能新增前向迁移。
- 日志、指标、命令输出和故障记录不得包含凭据明文、Authorization、完整 Prompt 或媒体 base64。

## 2. 拓扑与端口

宿主机默认只开放：

| 服务 | 地址 | 用途 |
| --- | --- | --- |
| Web | `http://localhost:19285` | 用户界面 |
| API | `http://localhost:19288` | 公共 API、`/healthz`、`/readyz`、`/metrics` |
| Realtime | `http://localhost:19281` | 实时事件、`/healthz`、`/readyz`、`/metrics` |
| MinIO API | `http://localhost:19290` | Signed GET 媒体访问 |

Provider Gateway 的 `/metrics` 只在 Docker 网络内提供。不要为了采集指标新增宿主机映射；让监控采集器加入 `cineweave_internal` 网络。

### 2.1 Realtime TLS 与反向代理

生产环境必须由同一站点的 TLS 反向代理公开 Web、API 和 Realtime，浏览器不得直接访问不受信任网络上的 `19281`。代理将 `/api/realtime/events` 转发到 Docker 网络内的 `realtime:8081`，并满足以下契约：

- 转发 `Authorization`、`Last-Event-ID`、`Origin` 和 `X-Request-Id`，不得把 access token 放入 URL。
- 禁用响应缓冲、缓存、压缩和内容转换；保留服务端的 `Cache-Control: no-cache, no-transform` 与 `X-Accel-Buffering: no`。
- 上游读取超时必须大于 Realtime heartbeat 周期，并允许长连接；代理不得把 SSE 响应聚合后一次性发送。
- 只允许配置在 `CINEWEAVE_CORS_ORIGINS` 中的 Web origin。生产环境不得使用 `*`。
- TLS 证书、HSTS 和公网访问控制由反向代理负责；Realtime 服务本身只处理 Bearer 鉴权、租户隔离和 `project.read` 授权。

Nginx 对应 location 的关键配置如下，实际域名和 upstream 由部署环境提供：

```nginx
location = /api/realtime/events {
    proxy_pass http://realtime:8081;
    proxy_http_version 1.1;
    proxy_set_header Authorization $http_authorization;
    proxy_set_header Last-Event-ID $http_last_event_id;
    proxy_set_header Origin $http_origin;
    proxy_set_header X-Request-Id $http_x_request_id;
    proxy_buffering off;
    proxy_cache off;
    gzip off;
    proxy_read_timeout 1h;
}
```

发布后必须从 HTTPS 入口验证 401、403、统一 404、`stream.ready`、heartbeat、`Last-Event-ID` 追赶和 410 cursor expiry；只检查容器内 HTTP 不算生产入口验收。

## 3. 发布前检查

在仓库根目录 `D:\Code\CineWeave` 执行：

```powershell
$ErrorActionPreference = 'Stop'

git status --short
pnpm run test
go vet ./...
go run ./cmd/cineweave-migrate validate
go run ./cmd/cineweave-seed validate
docker compose -f compose.yml --profile app config --quiet
```

生产发布必须使用不可变 Release ID。推荐使用完整 Git commit SHA 或镜像 digest 对应的发布号，不得使用 `latest`、`main`、`local-dev` 等可变值：

```powershell
$ErrorActionPreference = 'Stop'

$env:CINEWEAVE_ENV = 'production'
$env:CINEWEAVE_RELEASE_ID = (git rev-parse HEAD).Trim()
if ([string]::IsNullOrWhiteSpace($env:CINEWEAVE_RELEASE_ID)) {
    throw 'CINEWEAVE_RELEASE_ID is required'
}
```

确认部署环境已通过 Secret 管理注入数据库、S3、JWT、Provider Gateway 内部令牌和凭据加密主密钥。不要把这些值写入 Compose、Git 或命令历史。

## 4. 数据库迁移与系统 Seed

### 4.1 执行顺序

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml --profile app build migrate seed
docker compose -f compose.yml --profile app run --rm migrate /cineweave-migrate up
docker compose -f compose.yml --profile app run --rm seed /cineweave-seed apply
docker compose -f compose.yml --profile app run --rm migrate /cineweave-migrate verify
docker compose -f compose.yml --profile app run --rm seed /cineweave-seed verify
```

`verify` 会校验数据库中已应用 migration/seed 的版本和内容 hash。Seed 会逐字段校验 system-managed 声明内容，仅排除由数据库触发器维护的易变 `updated_at` 审计字段。任何 hash 或声明内容 mismatch 都必须阻断发布，不能改数据库账本绕过。

### 4.2 审计查询

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT version_id, direction, left(content_hash, 12) AS hash, status, duration_ms, release_id, completed_at, error_summary FROM cineweave_migrations.cineweave_migration_audit ORDER BY id DESC LIMIT 20;"
docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT resource_key, resource_version, left(content_hash, 12) AS hash, release_id, applied_at FROM system_seed_versions ORDER BY resource_key;"
```

迁移日志固定包含 `version`、`direction`、`contentHash`、`durationMs`、`releaseId`、`status`；失败时还包含截断后的 `failureStatementSummary`。

## 5. Compose 部署

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

`migrate`、`seed`、`temporal-schema`、`temporal-namespace` 和 `minio-create-bucket` 是 one-shot 服务，成功退出是正常状态。其余应用服务应为 `Up`，带 healthcheck 的服务应为 `healthy`。

### 5.1 健康检查

```powershell
$ErrorActionPreference = 'Stop'

Invoke-RestMethod -Method Get -Uri 'http://127.0.0.1:19288/healthz'
Invoke-RestMethod -Method Get -Uri 'http://127.0.0.1:19288/readyz'
Invoke-RestMethod -Method Get -Uri 'http://127.0.0.1:19281/healthz'
Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:19285/' | Select-Object StatusCode
Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:19290/minio/health/live' | Select-Object StatusCode
```

API `/readyz` 的 `database`、`providerGateway`、`storage`、`temporal` 必须全部为 `ok`。仅容器进程存活不能替代 readiness。

## 6. Temporal Worker 发布

四个 Worker Deployment 默认使用同一个不可变 Build ID：

- `cineweave-script-worker`
- `cineweave-agent-worker`
- `cineweave-media-worker`
- `cineweave-audio-worker`

Script、Media、Audio 使用 Pinned；Agent 使用 AutoUpgrade。对每个 Deployment 独立执行检查和路由切换。

```powershell
$ErrorActionPreference = 'Stop'

$env:CINEWEAVE_RELEASE_ID = '<new-release-id>'
$deployment = 'cineweave-script-worker'

docker compose -f compose.yml --profile ops run --rm temporal-release check --deployment $deployment --release-id $env:CINEWEAVE_RELEASE_ID
docker compose -f compose.yml --profile ops run --rm temporal-release ramp --deployment $deployment --release-id $env:CINEWEAVE_RELEASE_ID --percentage 10
docker compose -f compose.yml --profile ops run --rm temporal-release ramp --deployment $deployment --release-id $env:CINEWEAVE_RELEASE_ID --percentage 100
docker compose -f compose.yml --profile ops run --rm temporal-release promote --deployment $deployment --release-id $env:CINEWEAVE_RELEASE_ID
```

`temporal-release` 是 `ops` profile 的 one-shot 服务，只通过 `cineweave_internal` 连接 `temporal:7233`，不映射宿主端口。

旧版本排空：

```powershell
$ErrorActionPreference = 'Stop'

$previousRelease = '<previous-release-id>'
docker compose -f compose.yml --profile ops run --rm temporal-release drain --deployment $deployment --release-id $previousRelease --stable-checks 3 --poll-interval 10s
```

只有输出 `safeToDecommission=true` 且连续观察稳定后才可停止旧 Worker。若 drainage 长时间不下降，保留旧 Worker，查询 Pinned Workflow，不得删除 Temporal 数据库。

回滚只改变路由，不回滚数据库：

```powershell
$ErrorActionPreference = 'Stop'

$previousRelease = '<previous-release-id>'
docker compose -f compose.yml --profile ops run --rm temporal-release rollback --deployment $deployment --release-id $previousRelease
```

## 7. 指标、日志与告警

### 7.1 指标入口

```powershell
$ErrorActionPreference = 'Stop'

$metrics = (Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:19288/metrics').Content
$metrics | Select-String 'cineweave_(http|provider|workflow|temporal)_'
```

主要指标：

| 指标 | 含义 |
| --- | --- |
| `cineweave_http_requests_total` | 各服务、方法、状态码请求量 |
| `cineweave_http_request_duration_seconds` | HTTP 延迟 |
| `cineweave_provider_request_transitions_total` | Provider Request durable 状态变化 |
| `cineweave_provider_request_attempt_generation` | Provider attempt generation |
| `cineweave_provider_idempotency_outcomes_total` | execute、dedupe、conflict、retry、unknown outcome |
| `cineweave_provider_stream_terminal_total` | terminal mode 与成功、截断、解析失败 |
| `cineweave_provider_media_policy_rejections_total` | blocked address、redirect、MIME、byte limit |
| `cineweave_provider_media_download_bytes_total` | 已接收媒体字节数 |
| `cineweave_workflow_oldest_queued_age_seconds` | Workflow Start 队列最大等待时间 |
| `cineweave_workflow_start_attempts_total` | started、already_started、fenced、retry、failed |
| `cineweave_workflow_active_nodes` | 活动 Workflow 的 queued/running 节点数 |
| `cineweave_workflow_partial_succeeded_runs` | 持久化部分完成任务数 |
| `cineweave_workflow_oldest_cancellation_age_seconds` | 最久取消等待时间 |

Prometheus 告警规则位于 `deploy/observability/cineweave-alerts.yml`。规则覆盖启动队列、取消阻塞、Provider 长时间 running、unknown outcome、SSE 截断、媒体策略拒绝和 API 5xx。

Temporal 旧版本 backlog 由 `temporal-release check/drain` 的 JSON 字段 `deploymentName`、`releaseId`、`currentReleaseId`、`rampingReleaseId`、`drainageStatus`、`safeToDecommission` 监控。运维调度器应在排空窗口内周期执行；超过发布窗口仍非 `drained` 时告警。

迁移 hash 告警通过定时运行 `/cineweave-migrate verify` 实现。非零退出码必须触发 critical 告警并阻断后续容器更新。

### 7.2 结构化日志关联

HTTP 日志包含 `service`、`env`、`requestId`、`method`、`route`、`status`、`durationMs`、`responseBytes`。Provider 与 Workflow 日志进一步包含适用的 `providerRequestId`、`providerCallId`、`artifactIds`、`workflowRunId`、`nodeRunId`、`attempt` 和 `taskQueue`。

排障时从 API `requestId` 开始关联，不要按 Prompt 文本或用户输入全文搜索。

## 8. 持久状态诊断

### 8.1 Workflow Start Outbox

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT status, count(*) AS items, max(EXTRACT(EPOCH FROM now() - created_at))::int AS oldest_seconds, max(attempt_count) AS max_attempt FROM workflow_start_outbox GROUP BY status ORDER BY status;"
docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT id, workflow_run_id, workflow_type, status, attempt_count, max_attempts, next_attempt_at, locked_by, last_error_code, left(last_error_message, 240) AS error FROM workflow_start_outbox WHERE status IN ('pending','processing','failed') ORDER BY created_at LIMIT 50;"
```

判断：

- `pending` 且 `next_attempt_at` 在未来：正常退避。
- `processing` 且 `locked_at` 未超过 lease：正常启动中。
- `processing` 超过 lease：Dispatcher 应重新领取；若不收敛，检查 API 日志和数据库连接。
- `failed`：查看稳定 `last_error_code`，不要直接改为 pending。

### 8.2 Provider Request 与调用记录

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT status, task_type, count(*) AS requests, max(EXTRACT(EPOCH FROM now() - updated_at))::int AS oldest_seconds FROM provider_requests GROUP BY status, task_type ORDER BY status, task_type;"
docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT id, task_type, status, attempt_generation, workflow_run_id, node_run_id, error_code, left(error_message, 240) AS error, updated_at FROM provider_requests WHERE status IN ('running','unknown_outcome','failed') ORDER BY updated_at LIMIT 50;"
docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT id, provider_request_id, attempt_generation, status, error_code, latency_ms, started_at, completed_at FROM provider_call_logs ORDER BY created_at DESC LIMIT 50;"
```

相同 idempotency key 和 request hash 的成功结果应 replay；不同 hash 必须冲突。`unknown_outcome` 需要人工/供应商任务查询确认后，才可通过业务 API 发起显式 retry。

### 8.3 Workflow 进度、部分完成与取消

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT id, workflow_type, status, total_items, completed_items, failed_items, revision, error_code, updated_at FROM workflow_runs WHERE status IN ('queued','running','partial_succeeded','cancelling','failed') ORDER BY updated_at DESC LIMIT 50;"
docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT workflow_run_id, status, count(*) AS nodes FROM workflow_node_runs WHERE status IN ('queued','running','failed','cancelled') GROUP BY workflow_run_id, status ORDER BY workflow_run_id, status;"
docker compose -f compose.yml exec -T postgres psql -U cineweave -d cineweave -c "SELECT id, workflow_run_id, status, provider_task_id, error_code, updated_at FROM provider_async_tasks WHERE status IN ('queued','running','cancelling','failed') ORDER BY updated_at LIMIT 50;"
```

`partial_succeeded` 是终态，不能继续占用任务活动数字。只通过 retry API 创建关联的新 Workflow Run；不要原地覆盖失败节点。

## 9. 故障处置

### 9.1 `UPSTREAM_STREAM_TRUNCATED`

1. 查看 `terminal_mode` 指标和模型 capability 中的 `streamTerminalMode`。
2. 核对上游是否发送 `[DONE]` 或 `finish_reason`。
3. 检查反向代理、负载均衡和供应商是否提前断开连接。
4. 不得把部分 delta 落为成功结果；使用显式 retry，并保留原 Provider Request。

### 9.2 Provider media policy rejection

1. 按 `kind`、`reason` 查看指标。
2. 对 blocked address/redirect，确认最终域名及每一跳解析结果，禁止放行 localhost、Docker service name、metadata endpoint 或任意私网段。
3. 对 MIME/byte limit，先确认模型协议与真实响应，再调整模型/账户级限制。
4. 不得改用普通 `http.Get` 绕过 `MediaFetcher`。

### 9.3 Provider Request 长时间 running 或 unknown outcome

1. 查询 `provider_requests`、`provider_call_logs` 和 `provider_async_tasks`。
2. 若上游有 task ID，先通过 Gateway poll/cancel 路径核对。
3. 若无法确认，保持 `unknown_outcome`，记录供应商工单和计费结果。
4. 确认未产生结果或已安全取消后，才使用显式 retry。

### 9.4 Workflow queued

1. 查询 outbox 状态、lease、attempt 和 error code。
2. 检查 API `/readyz`、Temporal namespace 和目标 task queue poller。
3. 若已被取消，启动栅栏应把 outbox/run/node 收敛为 cancelled；不得再次调用 Temporal。
4. 不直接更新 `temporal_workflow_id` 或清空 outbox 行。

### 9.5 Workflow cancelling

1. 检查非终态 node 和 `provider_async_tasks`。
2. 确认 Provider cancel 是否支持、是否返回不可取消终态。
3. 等待 reconciliation；超过阈值时修复 reconciliation 路径。
4. 只有用户明确要求一次性修复并已留审计记录时，才考虑手工数据修复。

### 9.6 资产批任务部分失败

1. 查看父 run 的 `total_items/completed_items/failed_items` 和失败 node 的稳定错误码。
2. 修正模型能力、Prompt、参考图或媒体策略问题。
3. 使用 retry API 只重试失败项；确认新 run 的 `root_workflow_run_id` 和 `retry_of_workflow_run_id` 正确。
4. 若用户在任务期间编辑资产，旧 revision 应进入 conflict history，不得覆盖当前资产。

### 9.7 Migration/Seed hash mismatch

1. 立即停止发布，不运行 down/reset，不改 Goose 或 Seed 账本。
2. 对比运行镜像、Git commit、嵌入 migration/seed 文件和审计 hash。
3. 若已发布文件被修改，恢复正确不可变镜像；若 schema 需要修复，新增前向 migration。
4. 重新执行 migrate/seed verify，成功后再继续部署。

### 9.8 旧 Worker 无法排空

1. 保留旧 Worker 和数据库/Gateway 的 N-1 contract。
2. 运行 `temporal-release check` 确认该版本不是 current/ramping。
3. 查询旧 Pinned Workflow 的等待 Activity、Timer、Signal 或 Provider async task。
4. 修复阻塞原因或由用户取消；不要删除 Temporal 数据。

## 10. 真实供应商 Smoke

每次运行时边界、Gateway、存储、Workflow 或 Worker 发布机制变化后，至少覆盖：

1. 文本流：收到实时 delta，终态满足模型 `streamTerminalMode`，Provider Request 和 call log succeeded。
2. 图片：真实生成成功，经 Gateway 转存对象存储，Signed GET 返回图片内容。
3. 视频：真实异步 create/poll 成功，经 Gateway 转存，Signed GET 支持 Range；媒体探测记录宽高、时长、帧率和音轨。
4. 批量 Prompt：至少 3 项并发执行，刷新/重新登录后按同一 run ID 恢复进度。
5. 批量图片：构造至少 1 个失败项，父任务为 `partial_succeeded`，成功项不回滚。
6. 失败重试：只创建失败项的新关联 run，成功后不重复提交原成功项。
7. 取消：父 run、queued/running nodes 和可取消 Provider async task 收敛为 cancelled。
8. revision：任务期间编辑资产时，旧结果不能覆盖新 revision。

Smoke 记录至少保存 release ID、时间、项目 ID、Workflow Run ID、Provider Request ID、Provider Call ID、Artifact ID、结果和错误码。不得保存凭据、完整 Prompt 或 base64。

## 11. 收口检查

```powershell
$ErrorActionPreference = 'Stop'

rg -n 'schema_migrations|migrate\.sh|goose -dir' . -g '!docs/**' -g '!go.mod' -g '!go.sum'
rg -n 'runAssetBatch|assetBatchStore|localStorage.*batch' apps/web
rg -n 'SetCurrentVersion|SetRampingVersion' workers internal apps services -g '*.go' -g '!**/*_test.go'
rg -n 'http\.Get\(|http\.DefaultClient' internal/provider -g '*.go' -g '!**/*_test.go'

pnpm run test
go vet ./...
go run ./cmd/cineweave-migrate validate
go run ./cmd/cineweave-seed validate
docker compose -f compose.yml --profile app config --quiet
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

预期结果：旧迁移/批处理/自动 promotion 搜索没有运行时代码命中；Provider 上游 API JSON 请求可存在有界 HTTP 读取，但外部媒体 URL 下载只能进入 `internal/provider/outbound/MediaFetcher`。

### 11.1 隔离迁移与故障矩阵

数据库迁移往返必须使用隔离入口，不要把 `CINEWEAVE_INTEGRATION_TEST=1` 直接指向开发库或生产库：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

pwsh -NoProfile -File scripts/test-migrations.ps1
pwsh -NoProfile -File scripts/test-runtime-hardening.ps1
```

两个脚本会创建随机命名的 Docker 网络和临时 PostgreSQL 容器，不映射宿主机端口，并使用固定 PostgreSQL/Go 镜像执行测试。默认无论成功或失败都会删除临时容器和网络；仅排障时可传 `-KeepEnvironment`，检查完成后必须手动删除输出中列出的容器和网络。

`test-migrations.ps1` 只执行空库 `up/seed/down-to-zero/up/seed` 与规范化 schema 对比。`test-runtime-hardening.ps1` 在此基础上执行 Provider 幂等和 stream 故障、HTTP operation 恢复、Workflow Start Outbox、取消 reconciler、Realtime 500 条持久补发、100 单元长批次、失败项重试和 Linux race。任何子项失败都会返回非零退出码并阻断发布。
