# CineWeave 运行时基础设施评审修复建议

> 状态：已完成
> 评审日期：2026-07-15
> 修订版本：v2
> 评审基线：`main@182d7a3a255c` 与当前未提交更改
> 关联文档：`docs/runtime-foundation-hardening-target.md`、`docs/runtime-foundation-hardening-progress.md`、`docs/provider-gateway.md`

## 1. 文档目的

本文件针对当前未提交更改中发现的 7 个运行时问题，给出长期可维护的修复方案。重点不是逐点打补丁，而是收敛以下基础契约：

1. Workflow 终态不可回退，取消后不得继续产生业务写入。
2. 项目修订号、配置快照和任务创建必须位于同一个一致性边界内。
3. 重试依靠稳定幂等键和状态机保证，而不是通过禁用重试规避重复调用。
4. 流式事件必须明确区分尝试代次、路由尝试和分片序号。
5. 后端事件与前端订阅必须来自同一个可校验的事件目录。
6. HTTP 幂等记录失败后必须可安全重试，不能长期占用幂等键。
7. Provider 请求哈希只能剔除明确的执行控制字段，不得改写业务输入。
8. Realtime 连接必须经过身份验证和项目级授权，不能依赖可猜测的 `projectId`。
9. SSE 断线恢复必须使用持久游标；事件目录不能替代事件传输和补发协议。

本轮允许调整内部协议、数据库结构和 Workflow 实现。项目仍在开发阶段，不要求兼容旧业务数据；但需要考虑已经启动的 Temporal Workflow 的版本确定性和部署切换安全。

## 2. 评审结论

当前实现的方向正确，但取消、重试、快照和实时同步仍存在跨层契约缺口。问题集中在“状态已经显示为终态，但后台仍可继续写入”和“同一逻辑请求无法安全重试”两类风险。若不先修复，长时间批量生产任务会出现难以复现的数据漂移、重复计费、前端状态滞后和错误结果回放。

| 编号 | 优先级 | 问题 | 当前证据 | 主要风险 | 修复主线 |
| --- | --- | --- | --- | --- | --- |
| R8 | P0 | Realtime 端点缺少鉴权、租户隔离和持久补发游标 | `apps/realtime/main.go:46` | 未授权读取项目事件，断线期间事件丢失 | Realtime 安全与持久事件流 |
| R1 | P1 | 批量资产任务取消后未等待全部子任务收敛 | `internal/workflows/asset_batch.go:214` | 已取消任务继续写资产，节点终态回退或永久 cancelling | Workflow 状态机与写入栅栏 |
| R2 | P1 | 项目 revision 校验与快照创建存在 TOCTOU | `internal/api/asset_batches.go:157` | 任务快照混合新旧项目配置 | 事务隔离、统一 revision 与 CAS |
| R3 | P1 | 分集剧本 Provider 请求缺少稳定幂等键 | `internal/workflows/source_to_script.go:463` | 重试重复调用、重复计费或任务过度脆弱 | 逻辑请求幂等协议 |
| R4 | P1 | Text stream 的 live/replay attempt 含义不一致 | `internal/provider/gateway.go:453` | 重连后分片归属错误、重复或丢失文本 | 流式事件协议 |
| R6 | P1 | 失败的 HTTP 幂等记录无法安全恢复 | `internal/api/asset_batches.go:231`、`internal/api/idempotency.go` | 请求永久占用或未知副作用被重复执行 | HTTP 幂等状态机 |
| R5 | P2 | 后端事件与前端 SSE 白名单不一致 | `apps/web/src/lib/realtime/event-map.ts:9` | 分集结果已入库但页面不实时更新 | 实时事件契约 |
| R7 | P2 | Provider 请求哈希递归删除业务字段 | `internal/provider/provider_requests.go:467` | 不同请求哈希冲突并错误回放结果 | 路径感知哈希规范化 |

## 3. 目标架构原则

### 3.1 终态单调性

Workflow、节点和批次状态只能按显式转换表变化。`succeeded`、`failed`、`cancelled`、`skipped` 均为终态，任何迟到回调都不能将终态重新改为运行中或成功。

推荐统一转换：

```text
queued -> running -> succeeded
                  -> failed
                  -> cancelling -> cancelled
queued -------------------------> cancelled
```

所有状态写入使用带前置状态的 CAS 更新，例如：

```sql
UPDATE workflow_node_runs
SET status = 'succeeded', completed_at = now()
WHERE id = $1
  AND status = 'running'
  AND attempt_generation = $2;
```

更新行数为 0 时表示结果已经过期，应记录为被丢弃，而不是覆盖现有终态。

### 3.2 业务写入必须携带稳定执行令牌

所有异步结果写入至少绑定：

- `workflow_run_id`
- `node_run_id`
- `attempt_generation`
- `execution_token`
- 目标实体 ID
- 可选的 `project_revision`

这里的 `workflow_run_id` 明确指 CineWeave 数据库中的 `workflow_runs.id`，不是 Temporal Run ID。它必须在 Temporal retry 和 Continue-As-New 之间保持稳定。

`execution_token` 在 API 创建 Workflow Run 和节点的数据库事务中生成并持久化，随后作为 Workflow input 传入。禁止在 Workflow 代码中直接调用随机 UUID 或当前时间生成令牌。Activity 在事务中锁定对应 run/node，并校验其仍处于可写状态后，才允许修改资产、剧本、分镜或媒体记录。

### 3.3 重试与幂等分离

Temporal 的 activity retry、用户点击重试、Provider Gateway fallback 是三种不同维度：

- Activity retry：同一个逻辑请求，幂等键不变。
- 用户重试：创建新的 `retry_generation`，幂等键变化。
- Provider fallback：同一逻辑请求内的不同上游尝试，`attempt_sequence` 变化，但逻辑幂等键不变。

禁止通过 `MaximumAttempts=1` 规避幂等问题。该做法会让 70 分钟级生产任务对瞬时故障过度敏感。

任何“调用可能已经产生副作用，但本地无法确认结果”的情况都必须进入 `unknown_outcome`，不得因 lease 过期或请求 context 取消而自动重发。自动重试只适用于能够证明业务事务已回滚、上游请求未发送，或上游提供强幂等保证的情况。

### 3.4 单一事件目录与前端解耦

事件名称、载荷版本、作用域和必填字段应进入统一事件目录。后端发出但未登记的事件必须在 CI 中失败。React Query cache key 属于前端实现细节，不写入共享事件目录；前端维护类型安全、穷尽检查的 invalidation map。

### 3.5 快照不可混合版本

任务启动时读取的项目 revision、手册绑定、Prompt version、模型绑定和输入实体版本必须组成原子快照。快照事务明确使用 `REPEATABLE READ` 或更强隔离级别；所有项目级配置变更必须锁定项目行并增加 project revision。快照落库和 outbox 事件创建必须属于同一个事务。

### 3.6 Realtime 是受保护的持久读取模型

Realtime 事件不是公开广播，也不能直接把临时 outbox 当成无限期事件历史。每个连接必须验证 Bearer token、组织身份和项目读取权限；每个事件必须具有可排序的持久 stream position。断线游标过期时，服务端返回明确错误，前端重新获取权威 REST 状态后从新水位继续订阅。

## 4. 详细修复方案

### 4.1 R1：批量资产任务取消屏障与写入栅栏

#### 现状

父 Workflow 收到第一个子任务取消结果后，会提前完成批次并把节点标记为取消，但其余子 Workflow 仍可能返回。迟到的 Activity 可以继续执行 `applyAsset*Result` 和 `CompleteNodeRun`，从而在批次已经取消后写入资产并把节点改回成功。反过来，如果父 Workflow 无期限等待不响应取消的子任务，又会重新产生永久 `cancelling`。

#### 目标状态机

1. API 接收取消请求后，在数据库事务中把 run 从活动状态 CAS 为 `cancelling`，记录 `cancellation_requested_at` 和确定的 `cancellation_deadline_at`。
2. 从该事务提交开始，所有结果 Activity 都必须因 run 已为 `cancelling` 而失去业务写权限；取消收敛不能依赖子任务是否及时响应。
3. 父 Workflow 停止派发新任务，并向所有已启动 child Workflow 发出取消。
4. 父 Workflow 使用 `workflow.NewDisconnectedContext` 执行取消清理，不能继续使用已经取消的主 context。
5. 使用 Temporal 确定性 timer 形成有限宽限期；禁止用本地 `time.After`、随机抖动或无限等待 child future。
6. 宽限期内周期执行 `ReconcileWorkflowCancellation` Activity，以数据库为权威检查：
   - `workflow_runs` 是否仍为 `cancelling`；
   - 是否仍有 `workflow_node_runs` 处于 `queued/running/waiting_review`；
   - 是否仍有 `provider_async_tasks` 处于 `queued/running/cancelling`；
   - Provider Gateway 的取消、轮询或查询结果是否已经收敛。
7. 宽限期结束后，仍无法向上游确认结果的 Provider task 进入终态 `unknown_outcome` 或 `cancel_failed`，记录潜在成本告警；不得继续留在活动状态，也不得自动重发。
8. 当数据库中不再存在有效活动节点或 Provider task 后，父 Workflow 才汇总为 `cancelled`。如果清理轮询较多，使用 Continue-As-New 控制 history。
9. 迟到结果只写结构化日志和事件 `workflow.result.discarded`，不得修改业务实体，不作为系统错误重试。

#### 写入栅栏

每个结果 Activity 在同一数据库事务中：

1. `SELECT ... FOR UPDATE` 锁定 run 和 node。
2. 校验 run 仍为可写状态，node 仍为 `running`。
3. 校验 `attempt_generation` 和 `execution_token` 与输入一致。
4. 执行业务写入和 node 终态 CAS。
5. 更新行数为 0 时返回明确的 stale/discarded 结果，不抛出可重试错误。

`CompleteNodeRun`、`FailNodeRun`、资产结果写入、剧本分集写入、分镜和媒体写入都必须复用这一栅栏，不能只修资产批次。

#### 数据结构建议

若现有表没有等价字段，增加：

```text
workflow_node_runs.attempt_generation integer not null default 1
workflow_node_runs.execution_token uuid not null
workflow_runs.cancellation_requested_at timestamptz null
workflow_runs.cancellation_deadline_at timestamptz null
workflow_runs.settled_at timestamptz null
provider_async_tasks.execution_token uuid null
provider_async_tasks.attempt_generation integer not null default 1
```

`execution_token` 在 API/数据库创建节点时生成并写入 Workflow input；同一 Activity retry 沿用，用户创建新的失败项重试任务时更换。

#### 关键测试

- 启动 5 个子任务，取消后让其中 2 个延迟成功；批次最终为 `cancelled`，迟到资产不得写入。
- 节点先 cancelled 后收到 succeeded 回调；CAS 更新行数必须为 0。
- 使用已取消 context 等待 child 的测试必须失败；使用 disconnected cleanup 后可正确收敛。
- 子 Workflow 永不响应取消时，确定性宽限期结束后不得永久停留在 `cancelling`。
- Provider async task 长期停留在 `cancelling` 时，经查询和截止时间进入 `unknown_outcome/cancel_failed`，并产生潜在成本告警。
- 取消期间 Worker 重启；Workflow replay 后继续同一宽限期和收敛流程。
- 部分子任务已成功再取消；保留取消前已提交结果，但取消后的迟到结果不得写入。

### 4.2 R2：项目 revision、配置快照与 outbox 的事务一致性

#### 现状

请求入口先在事务外检查 `expectedProjectRevision`，随后才创建快照并写 outbox。检查与提交之间，项目设置或手册绑定可能发生变化，导致快照混合两个 revision 的配置。仅仅把查询放入默认 `pgx.Tx` 仍不够，因为 PostgreSQL `READ COMMITTED` 下每条语句都可能看到不同提交。

#### 建议实现

新增只接受事务查询接口的统一入口，例如：

```go
type DBTX interface {
    Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
    Query(context.Context, string, ...any) (pgx.Rows, error)
    QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) CreateAssetBatchTx(
    ctx context.Context,
    tx pgx.Tx,
    req CreateAssetBatchRequest,
) (AssetBatch, error)
```

事务通过 `pgx.BeginTx` 明确使用 `pgx.RepeatableRead`；项目级配置写入和任务快照创建都先执行 `SELECT id, revision FROM projects ... FOR UPDATE`，形成统一串行点。若后续发现跨项目全局配置竞争需要更强保证，再升级为 `Serializable`，并对 serialization failure 做最多 3 次有界重试。

事务步骤固定为：

1. 锁定项目行并读取当前 `revision`。
2. 对比 `expectedProjectRevision`，不一致返回 `409 PROJECT_REVISION_CONFLICT`。
3. 在同一固定 snapshot 中读取手册绑定、明确的 Prompt active version、模型 profile/binding、项目比例及目标实体版本。
4. 所有 resolver 接受 `DBTX`，禁止在内部回退到连接池。
5. 写入不可变任务 snapshot，记录每项配置的 ID、revision/version 和内容哈希。
6. 创建 workflow run、node runs、HTTP idempotency completion 和 outbox。
7. 提交事务后由 event publisher 启动 Temporal Workflow；事务中禁止网络调用。

项目设置、视觉/导演手册绑定、项目级 Prompt override、业务模型绑定、比例和其它会影响生成结果的设置，必须复用统一的项目配置 mutation helper：锁项目行、修改配置、`revision = revision + 1`。组织级 Prompt 或模型目录变化不批量修改所有项目 revision，但任务 snapshot 必须固定实际解析到的 version/binding ID 与哈希。

#### 快照验收字段

```text
projectRevision
visualManualBindingId
visualManualVersionId
directorManualBindingId
directorManualVersionId
promptVersionIds
modelProfileBindingIds
sourceEntityVersions
aspectRatio
snapshotHash
createdAt
```

#### 关键测试

- 在 revision 校验后并发修改视觉手册；结果只能是完整旧快照、完整新快照或 409，不能混合。
- 在快照读取多个配置之间并发激活 Prompt version，验证固定 snapshot 不混合版本。
- 并发修改业务模型绑定和项目比例，验证所有 mutation 都增加项目 revision。
- helper 误用连接池查询时，事务接口测试或静态检查必须失败。
- outbox 写入失败时 workflow run、nodes、snapshot 和幂等完成记录一并回滚。
- 相同 revision 下重复请求通过 HTTP 幂等层返回同一 operation resource。

### 4.3 R3：分集剧本生成的逻辑幂等与独立重试单元

#### 现状

每集 Provider 请求带有 Workflow/Node 标识，但没有稳定的 `IdempotencyKey`。当前通过 Activity `MaximumAttempts=1` 降低重复调用概率，这会导致临时网络错误直接让长任务失败。

#### 稳定逻辑请求身份

建立统一幂等键构造器，禁止各 Workflow 自行拼接字符串：

```text
operation=script.episode.generate
rootOperationId=<workflow_runs.root_workflow_run_id 或首代 workflow_runs.id>
nodeKey=episode:<source-chapter-id>
entityVersion=<source-chapter-version-or-content-hash>
retryGeneration=<integer>
requestSchemaVersion=1
```

字段含义：

- `rootOperationId` 是 CineWeave 数据库中的稳定业务 operation ID，不是 Temporal Workflow ID 或 Run ID。
- Activity retry、Worker 重启、Temporal replay 和 Continue-As-New 都沿用同一 key。
- 用户显式重试失败分集时增加 `retryGeneration`，创建关联的新 Workflow Run 和 execution token。
- 输入发生变化时必须增加实体 version/hash；如果复用旧 key，Provider Gateway 应返回 request hash conflict。
- Activity attempt、Worker identity、时间戳和路由 fallback 次序不得进入逻辑幂等键。

#### 独立可重试分集单元

- 主 Workflow 只负责分集规划、并发窗口、取消传播和最终汇总。
- 每集使用稳定 Workflow ID 的 child Workflow 或独立 durable node。
- 每集生成后立即写入独立 episode/version 行，并使用执行栅栏防止迟到覆盖。
- 单集失败不回滚已成功分集。
- 支持只选择失败分集创建新一代重试任务；父批次最终状态使用现有 `partial_succeeded`。
- 每集保存 source chapter IDs、输入哈希、Prompt version、Provider request/call ID、retry generation 和 execution token。
- 父 Workflow 根据 history event 数量或处理单元阈值 Continue-As-New，不能依赖固定运行时长判断。

恢复有界 Activity retry，例如可重试瞬时错误最多 3 次，并按 Gateway 标准错误码分类。认证错误、参数错误、内容策略拒绝和 `unknown_outcome` 不得自动重试。Provider Gateway 命中同一逻辑请求时返回已完成结果或 in-progress 状态，而不是再次调用上游。

#### 关键测试

- Provider 已处理请求但 Worker 在保存结果前崩溃；Activity retry 命中同一 Provider request，不产生第二笔上游调用或确定成本记录。
- Provider 调用结果未知时进入 `unknown_outcome`，不会被 Temporal retry 静默重发。
- 第 3 集失败时，第 1、2、4、5 集保持成功；只重试第 3 集，新父任务为 `partial_succeeded` 或成功。
- 用户修改第 3 集输入后重试，entity version、generation 和幂等键必须变化。
- Continue-As-New 前后幂等键保持不变。
- Workflow replay 不得重新生成随机幂等键或 execution token。

### 4.4 R4：Text stream 尝试代次和分片序号协议

#### 现状

实时 delta 使用 fallback 序号作为 attempt，而 replay 使用 `AttemptGeneration`。第二代执行在 live 阶段可能显示 attempt 1，重放后却变成 attempt 2，消费者无法可靠去重。更重要的是，已经向消费者发送部分文本后再 fallback，没有安全的撤回语义。

#### 协议决策

统一 `GatewayTextDelta`：

```json
{
  "schemaVersion": 2,
  "providerRequestId": "...",
  "providerCallId": "...",
  "attemptGeneration": 2,
  "attemptSequence": 1,
  "sequence": 37,
  "text": "...",
  "finishReason": null
}
```

字段语义：

- `attemptGeneration`：用户显式重试代次。
- `attemptSequence`：该 generation 内的模型路由/fallback 次序，从 1 开始。
- `sequence`：单次上游 stream 内严格递增的分片序号。
- `providerRequestId`：整个逻辑 Provider 请求稳定不变。
- `providerCallId`：每个实际上游调用唯一。

事件类型固定为：

```text
provider.attempt.started
provider.delta
provider.attempt.failed
provider.completed
provider.failed
provider.replayed
```

#### Fallback 规则

- 在尚未向外发送任何非空 delta 时发生可重试错误，可以自动 fallback。
- 一旦向消费者发送首个非空 delta，本 generation 禁止自动 fallback。
- 首个 delta 后发生截断或连接错误，当前请求进入 `failed` 或 `unknown_outcome`，标准错误为 `UPSTREAM_STREAM_TRUNCATED`；部分文本只用于诊断，不得标记为完整成功。
- 用户显式重试时增加 `attemptGeneration`，前端用新 generation 替换或并列展示，不把两个模型输出拼接。
- 如果未来需要“部分输出后无缝 fallback”，必须先设计支持撤销和提交的 per-attempt buffer 协议，本版本不实现。

消费者仅接收当前 generation，并以 `(providerCallId, sequence)` 去重。成功请求被幂等重放时，不伪造原始 delta 边界；Gateway 发出单个 `provider.replayed`，携带最终文本 snapshot、原 accepted providerCallId 和 attempt identity。

#### 兼容策略

内部 stream event 使用 `schemaVersion: 2`。Provider Gateway、Worker 和消费者同批部署；部署前排空正在执行的 text stream，或短期双读 v1/v2，确认无旧消费者后删除 v1。项目不为旧业务数据长期保留兼容分支。

#### 关键测试

- generation 2 的 live、日志和最终 snapshot identity 完全一致。
- 第一个候选在输出 delta 前失败，可以 fallback，第二次尝试拥有新的 `providerCallId` 和 `attemptSequence`。
- 第一个候选发送一个 delta 后失败，不得自动调用第二候选，结果为 `UPSTREAM_STREAM_TRUNCATED`。
- 成功请求幂等重放只发 `provider.replayed`，不会重复拼接 delta。
- 相同 `(providerCallId, sequence)` 重复到达时消费者只处理一次。

### 4.5 R5：后端事件目录与前端实时失效映射

#### 现状

后端新增了分集剧本、Workflow 和分镜规划事件，但前端只注册静态白名单。未注册的 named event 不会触发处理；部分页面查询又没有轮询，导致数据已入库但页面直到任务结束或重新进入后才更新。

#### 事件目录

建立版本化领域事件目录，至少包含：

```text
eventName
schemaVersion
scopeType
aggregateType
requiredPayloadFields
terminal
deprecatedAt
```

采用仓库内单一机器可读文件生成 Go 常量/校验器和 TypeScript event union：

```text
packages/events/catalog.yaml
internal/events/generated_catalog.go
apps/web/src/lib/realtime/generated-events.ts
```

共享目录不包含 React Query key。前端单独维护：

```ts
const projectEventInvalidation = {
  // 每个 ProjectEventName 都必须显式处理
} satisfies Record<ProjectEventName, EventInvalidationHandler>;
```

后端只能通过统一的 `events.AppendTx` 或等价 generated emitter 写领域事件，禁止业务代码直接拼接 event name 后插入 outbox。CI 检查 generated 文件无漂移，并禁止 catalog 外事件。

当前应补录至少以下事件：

```text
script.generation.prepared
script.episode.generated
script.episode.updated
script.version.activated
workflow.run.started
workflow.run.updated
workflow.run.partial_succeeded
workflow.run.completed
workflow.run.failed
workflow.run.cancelled
workflow.node.updated
storyboard.plan.prepared
storyboard.scene.generated
storyboard.scene.updated
workflow.result.discarded
```

前端失效映射需覆盖：

- 分集剧本列表、剧本版本和当前剧本详情。
- Workflow run、node runs 和任务活动计数。
- 分镜计划、当前分集分镜和 production status。
- 资产批次、资产卡和失败重试状态。

事件负责低延迟失效，REST/数据库仍是权威状态。连接建立、游标过期或重新登录后必须重新获取 active/recent runs 和当前页面数据；运行中查询保留低频、仅活动状态启用的兜底 polling。

#### 关键测试

- 后端尝试发出 catalog 外事件时编译或测试失败。
- catalog 生成物保持干净，禁止手改 generated 文件。
- TypeScript invalidation map 缺少任意事件时 typecheck 失败。
- 收到 `script.episode.generated` 后只失效对应 episode、版本和相关 production status。
- `partial_succeeded`、终态事件和迟到丢弃事件不会错误增加活动任务计数。
- 连接建立或游标过期后先恢复权威 REST 状态，再继续处理增量事件。

### 4.6 R6：HTTP 幂等状态机与未知结果恢复

#### 现状

失败请求将幂等记录标记为 `failed`，但后续准备逻辑把所有非成功状态都视为处理中。同一个幂等键可能在 TTL 内持续返回 `IDEMPOTENCY_IN_PROGRESS`。另一方面，简单允许所有 failed 或过期 processing 立即重占用，会在业务副作用已经提交但响应丢失时重复创建任务。

#### 建议状态机

```text
processing -> succeeded
processing -> failed_retryable
processing -> failed_terminal
processing -> unknown_outcome
failed_retryable -> processing    同 request hash 原子重占用
unknown_outcome -> succeeded      reconciler 找到已提交 operation
unknown_outcome -> failed_terminal
```

推荐字段：

```text
status
request_hash
hash_schema_version
operation_type
operation_id
response_status
response_body
lease_owner
lease_expires_at
attempt_count
last_error_code
last_error_message
outcome_checked_at
created_at
updated_at
```

处理规则：

1. 同 key、不同 request hash 始终返回 `409 IDEMPOTENCY_KEY_CONFLICT`。
2. `succeeded` 返回缓存响应和相同 operation resource。
3. 未过期的 `processing` 返回 `IDEMPOTENCY_IN_PROGRESS`，包含 operation ID 和建议重试时间。
4. 只有能够证明业务事务已回滚、上游未调用的错误才进入 `failed_retryable`。
5. 参数、权限和其它确定性错误进入 `failed_terminal`，相同 key 重放相同错误。
6. lease 过期、请求 context 取消、进程崩溃或提交结果不明时进入 `unknown_outcome`，不能直接接管重跑。
7. reconciler 通过 `operation_type/operation_id` 查询 workflow run、outbox 或领域资源：
   - 找到已提交 operation 时补全为 `succeeded`；
   - 确认没有副作用且事务已回滚时转为 `failed_retryable`；
   - 无法确认时保持 `unknown_outcome` 并告警。
8. 用户对 `unknown_outcome` 的显式重试通过专用 retry API 创建新 generation 和新幂等键；原记录保留审计链。
9. 对资产批次等命令，operation resource、outbox 和幂等成功 snapshot 必须在同一事务提交。
10. 请求 context 取消后不得盲目调用 `failIdempotency`；由独立 finalizer/reconciler 根据 operation 状态决定。

#### 关键测试

- 业务事务明确回滚后，同 key 同 body 可原子重占用并成功。
- 同 key 不同 body 始终冲突。
- operation 已提交但响应前进程崩溃时，重试返回同一 operation，不创建第二个 Workflow Run。
- 提交结果无法确认时进入 `unknown_outcome`，lease 过期也不会自动执行。
- 两个请求同时重占用 `failed_retryable`，只有一个获得 lease。
- context 取消发生在事务提交前、提交中和提交后三个故障点时，均不会重复副作用。

### 4.7 R7：Provider 请求哈希的路径感知规范化

#### 现状

当前规范化逻辑递归删除所有名为 `retry` 或 `idempotencyKey` 的字段。若原始输入、工具 schema 或 metadata 本身包含这些业务字段，不同请求可能得到相同哈希，并回放错误结果。

#### 建议实现

取消按字段名递归删除，改为明确路径白名单。只剔除 Provider Gateway 自身的执行控制字段，例如：

```text
$.idempotencyKey
$.retry
$.options.retry
$.options.transportRetry
$.requestContext.traceId
```

以下内容必须原样参与哈希：

- `input` 中的全部用户消息和结构化内容。
- tool/function schema。
- Provider 请求参数、模型参数和媒体引用。
- 影响输出的 metadata。

哈希流程建议：

1. 将公开请求 DTO 转成专用的 `LogicalProviderRequest`。
2. 显式不复制执行控制字段，而不是复制后递归删除。
3. 使用稳定 JSON canonicalization，保留数组顺序并稳定对象键顺序。
4. 对大媒体内容使用内容哈希或不可变 media ID，不直接把临时 signed URL 作为逻辑输入。
5. 将 `hashSchemaVersion` 纳入哈希前缀并持久化，未来算法变化时不得跨版本误命中。
6. 媒体 ID 只有在内容不可变时才可直接参与哈希；否则必须同时包含 media version 或 SHA-256。
7. 影响模型选择和输出的 profile/binding snapshot、能力参数和路由约束必须参与逻辑请求 DTO。

#### 关键测试

- `input.toolSchema.properties.retry` 不同的两个请求必须得到不同哈希。
- 顶层执行控制 `retry` 不同但业务输入相同，应得到相同逻辑哈希。
- 数组顺序变化应改变哈希；对象键顺序变化不应改变哈希。
- 临时 signed URL 刷新但指向同一不可变 media version 时，逻辑哈希保持稳定。
- 相同 media ID 的内容 version 改变后，哈希必须变化。
- hash schema version 改变时不得命中旧算法结果。

### 4.8 R8：Realtime 鉴权、租户隔离与持久游标

#### 现状

`apps/realtime/main.go` 当前只读取 URL 中的 `projectId`，没有验证用户身份、组织成员关系或项目权限；Compose 又把 Realtime 映射到宿主机端口。任何知道项目 ID 的客户端都可能读取事件。当前实现每次连接只查询最近 200 条 `event_outbox`，不处理 `Last-Event-ID`，断线期间超过窗口的事件会永久丢失。

#### 传输决策

前端不再使用无法设置 Authorization header 的原生 `EventSource`。实现基于标准 `fetch`、`ReadableStream` 和 SSE framing 的客户端，要求：

- 请求携带 `Authorization: Bearer <access-token>`。
- 重连携带 `Last-Event-ID: <stream-position>`。
- 支持 AbortController、指数退避、服务端 retry hint、心跳和最大 event size。
- token 刷新后使用新 token 重新建连，不把 access token 放入 URL。
- 默认不新增外部依赖；若引入 fetch-SSE 库，先写 ADR，检查维护状态，锁定精确版本并保留协议级集成测试。

Realtime 服务复用 API 的认证与 RBAC Authorizer。服务端从 principal 推导 organization ID，并验证 `project.read`；不能相信客户端传入的 organization ID。生产环境只允许经 TLS 反向代理访问，CORS 使用明确 origin，禁止通配符 credentials。

#### 持久事件读取模型

临时 `event_outbox` 继续负责发布投递，但不再承担浏览器事件历史。新增 `project_event_log`：

```text
stream_position bigint generated always as identity
event_id uuid unique not null
organization_id uuid not null
project_id uuid not null
event_type text not null
schema_version integer not null
aggregate_type text not null
aggregate_id uuid null
aggregate_revision bigint null
payload jsonb not null
created_at timestamptz not null
```

建立唯一键和索引：

```text
PRIMARY KEY (stream_position)
INDEX (organization_id, project_id, stream_position)
UNIQUE (event_id)
```

统一 `events.AppendTx` 在领域事务中同时写入 `project_event_log` 与引用同一 `event_id` 的 outbox，避免事件日志和投递记录分叉。

#### 游标与恢复协议

1. SSE 的 `id:` 使用十进制 `stream_position`，不再使用无序 UUID。
2. 带 cursor 的请求按 `stream_position > cursor ORDER BY stream_position LIMIT n` 分页追赶，然后进入 tail。
3. 初次连接返回 `stream.ready` 和当前 high watermark；前端在连接建立后重新获取权威 REST 状态，再处理后续增量。
4. 事件至少投递一次，前端按 stream position 去重，并忽略 aggregate revision 更旧的事件。
5. 事件保留期通过环境变量配置，默认 7 天。清理任务不得删除任何仍可能被合法 cursor 使用的事件。
6. cursor 早于最早保留位置时返回 `410 EVENT_CURSOR_EXPIRED` 和当前 watermark；前端清空 cursor、全量 refetch 后重新订阅。
7. 每次查询校验 `organization_id/project_id`，不能跨项目按全局 position 读取数据。

#### 关键测试

- 未登录或 token 过期返回 401；同租户缺少 `project.read` 返回 403；跨租户和不存在的 project ID 返回相同 404 响应，不能泄露项目是否存在。
- 合法用户只能读取自己的项目事件。
- 断线期间产生超过 200 个事件后，按 cursor 分页补发且无遗漏。
- 重复事件不会重复增加任务活动计数。
- cursor 过期后收到 410，前端完成 REST refetch 并从新 watermark 恢复。
- token 刷新、网络抖动和 Realtime 实例重启后能够继续订阅。
- outbox 发布失败不影响已提交的 project event log，恢复后可继续投递。

## 5. 建议的接口与结构调整

### 5.1 内部接口

建议新增或统一以下内部类型：

```text
ExecutionIdentity
  workflowRunRecordId
  rootOperationId
  nodeRunId
  attemptGeneration
  executionToken

CancellationIdentity
  requestedAt
  deadlineAt
  executionToken
  reconciliationGeneration

LogicalRequestIdentity
  operation
  rootOperationId
  nodeKey
  entityVersion
  retryGeneration
  requestSchemaVersion
  idempotencyKey

ProviderAttemptIdentity
  providerRequestId
  providerCallId
  attemptGeneration
  attemptSequence

ProjectEventEnvelope
  streamPosition
  eventId
  eventName
  schemaVersion
  organizationId
  projectId
  aggregateType
  aggregateId
  aggregateRevision
  payload
  createdAt
```

JSON/API 中可以继续使用 `workflowRunId`，但 Go 类型和文档必须明确它表示数据库 `workflow_runs.id`。Temporal Workflow ID/Run ID 只用于调度和诊断，不参与业务幂等身份。

这些结构应在 API、Workflow、Provider Gateway、Realtime、事件和日志中保持同名同义，避免继续复用含义模糊的 `attempt`、`runId` 或 `revision` 字段。

### 5.2 数据库迁移

按仓库当前迁移编号和双向迁移模式完成：

- `workflow_node_runs` 增加 attempt generation 和 execution token。
- `workflow_runs` 增加 cancellation requested/deadline/settled 字段。
- `provider_async_tasks` 增加 execution identity，并允许明确的 `unknown_outcome/cancel_failed` 终态。
- `idempotency_keys` 扩展为 processing/succeeded/failed_retryable/failed_terminal/unknown_outcome 状态机，并增加 operation、lease、attempt 和 outcome 字段。
- `provider_requests` 增加或校验 hash schema version。
- 新增 `project_event_log` 和 `(organization_id, project_id, stream_position)` 索引。
- `event_outbox` 增加与 project event log 共享的 `event_id`，并保证同一领域事务写入。
- 根据查询计划补充 cancelling、unknown outcome、cursor catch-up 和 reconciler 索引。

项目不要求保留旧业务数据，可重建开发数据库；但迁移仍需提供 `.up.sql` 和 `.down.sql`，保证：

```text
empty database -> migrate up -> seed -> normalized schema snapshot
down to zero -> migrate up -> seed -> normalized schema snapshot
两个 snapshot 完全一致
```

迁移不得依赖手工修改现有数据。Temporal 正在运行实例的兼容问题由发布策略处理，不能混同于旧业务数据兼容。

### 5.3 OpenAPI 与协议

仓库现有部分完成状态统一为 `partial_succeeded`，不得新增第二套状态拼写。

公开 API 和 OpenAPI 需要覆盖：

- Workflow Run/Node execution generation、cancel deadline 和 unknown outcome。
- 失败项 retry API 返回新的 Workflow Run、root operation 和 retry generation。
- HTTP idempotency 的 in-progress、terminal failure、unknown outcome 和 conflict 响应。
- `GET /api/realtime/events?projectId=...` 的 Bearer security、`Last-Event-ID` 请求头、SSE event envelope 和 `410 EVENT_CURSOR_EXPIRED`。
- Realtime 项目读取权限要求。
- 所有新增标准错误码及中文前端映射。

同步更新：

- `packages/openapi/openapi.yaml`
- Go handler 与内部 DTO
- `apps/web/src/lib` API client/types
- `apps/web/src/lib/labels.ts`
- route consistency check
- `docs/provider-gateway.md`
- Realtime 协议文档和事件 catalog

Text stream v2 属内部协议，也必须记录字段语义、fallback 边界、replay 事件和版本切换方式。

### 5.4 外部依赖准入

本方案默认使用浏览器标准 `fetch/ReadableStream` 实现鉴权 SSE，不强制新增依赖。若实现阶段决定引入 SSE、JSON canonicalization、事件 schema generation 或状态机库，必须：

1. 写 ADR 说明自研与引入的维护成本比较。
2. 检查许可证、维护活跃度、安全公告和浏览器/Go 版本支持。
3. 锁定精确版本并进入 lockfile/SBOM。
4. 保留仓库自己的协议级测试，不能把正确性完全委托给第三方库。
5. 提供升级和移除路径。

## 6. 实施顺序

### 6.0 执行状态

本表是本计划唯一的阶段完成标记。只有对应阶段的完成标准和测试全部通过后，才能把状态改为“已完成”；代码已经开始修改但验收未通过时保持“进行中”。

| 阶段 | 状态 | 覆盖问题 | 完成证据 |
| --- | --- | --- | --- |
| 阶段 0：冻结契约并建立失败测试 | 已完成 | R1-R8 | R1-R8 单元/集成回归、并发快照、stream v2、operation reconcile、空库 up/down/up 均通过 |
| 阶段 1：Realtime 安全与持久事件流 | 已完成 | R8 | 鉴权/跨租户/CORS/代理头、500 条持久补发、事务回滚与容器 smoke 通过 |
| 阶段 2：Workflow 状态机与快照一致性 | 已完成 | R1、R2 | generation/token 写栅栏、终态 CAS、取消 reconciler、统一 project revision 和串行化快照事务已通过 PostgreSQL 集成验证 |
| 阶段 3：统一幂等与未知结果协议 | 已完成 | R3、R6、R7 | 分集独立执行/重试、Provider 请求幂等、unknown outcome、HTTP operation 均通过真实 PostgreSQL 集成测试 |
| 阶段 4：流式协议与事件消费 | 已完成 | R4、R5 | stream v2、领域事件 catalog、Realtime v2 envelope、前端穷尽精确失效及活动态兜底轮询已通过单元、竞态和 PostgreSQL 集成验证 |
| 阶段 5：迁移、故障注入与长任务验证 | 已完成 | 全部 | 隔离迁移往返、100 单元长批次、故障矩阵、完整 Linux race、Compose 重建和进程重启恢复均通过 |

### 阶段 0：冻结契约并建立失败测试

在修改实现前，为 R1-R8 建立稳定失败测试，并先合并事件名、状态名和 identity 术语修正。

交付物：

- Realtime 未授权和跨租户读取测试。
- 超过 200 条事件的断线游标追赶测试。
- 取消后迟到写入、永不响应 child 和 Provider cancelling 超时测试。
- revision、Prompt active version、模型绑定并发变更测试。
- 分集 Provider Activity retry/Continue-As-New 幂等测试。
- stream pre-delta fallback 和 post-delta 禁止 fallback 测试。
- 事件 catalog 与前端穷尽映射测试。
- HTTP unknown outcome 和 operation reconcile 测试。
- nested semantic field/hash schema version 测试。
- 空库迁移 up/down/up schema snapshot 测试。

完成记录（2026-07-15）：

- `go test -count=1 ./apps/realtime ./cmd/events-gen ./internal/events ./internal/provider ./internal/workflows ./internal/api ./internal/dbmigrate` 通过。
- PostgreSQL 集成验证通过：批次 revision/快照一致性、Prompt/手册/模型/比例并发切换、operation 对账、取消收敛、stream fallback/replay identity。
- 空库 `up -> down -> up` 规范化 schema snapshot 一致。

### 阶段 1：P0 Realtime 安全与持久事件流

先实施 R8，未完成前不得继续扩大 Realtime payload 或把 Realtime 端口直接暴露到不可信网络。

完成标准：

- fetch-SSE 携带 Bearer 和 Last-Event-ID。
- Realtime 复用认证和 `project.read` Authorizer。
- project event log 与 outbox 同事务写入。
- 按 stream position 补发、去重和 cursor expiry 恢复。
- CORS、TLS 代理和跨租户测试通过。

完成记录（2026-07-15）：

- Realtime Bearer 鉴权、`project.read`、同租户无权限 403、跨租户/不存在项目统一 404 测试通过。
- PostgreSQL 集成测试验证 500 条事件按持久 stream position 分页补发，outbox 与 project event log 在同一事务内提交和回滚，回滚不消耗水位。
- `http://localhost:19285` CORS 预检允许 `Authorization` 与 `Last-Event-ID`，未知 origin 不返回授权头；SSE 禁缓存、禁代理缓冲和高水位头测试通过。
- Web fetch-SSE 类型检查通过；Realtime 容器重建后 `/readyz` 健康、匿名订阅返回 401、运行时预检返回 204。
- 生产 TLS 终止、反向代理长连接、禁缓冲及 HTTPS smoke 要求已写入运行手册。

### 阶段 2：Workflow 状态机与快照一致性

实施 R1 和 R2。

完成标准：

- 所有业务结果写入都有 generation/token 栅栏。
- run/node 终态更新均为 CAS。
- 取消使用 disconnected cleanup、确定性 deadline 和 reconciler。
- Provider async task 不会永久占用 cancelling。
- 项目配置 mutation 统一增加 revision。
- revision、snapshot、workflow run、nodes、idempotency completion 和 outbox 位于固定隔离事务。

完成记录（2026-07-15）：

- Workflow Run/Node 使用 execution token 与 attempt generation 写栅栏，业务结果写入与节点完成位于同一事务；取消或重启后的迟到结果统一丢弃并写入 `workflow.result.discarded` 审计事件。
- Run/Node 终态转换统一使用 CAS；取消流程持久化确定性 deadline，独立 reconciler 可将卡住的 Workflow、Node、Provider request/call/async task 收敛至受控终态，不会永久停留在 `cancelling`。
- 项目设置、Prompt/手册绑定、模型绑定和音频配置 mutation 统一推进 project revision；资产批次在 serializable 事务内固定 revision、配置快照、Workflow Run、Node、幂等完成和 outbox。
- Docker 网络内真实 PostgreSQL 集成测试通过：取消节点迟到完成、重启节点旧 token 写入、取消 Workflow 迟到终态、终态幂等、Provider task 取消超时收敛、批次 revision 冲突及 Prompt/手册/模型/比例并发快照一致性。
- `go test -count=1 ./internal/workflows ./internal/provider ./internal/api`、OpenAPI YAML parse 和 `python scripts/check-openapi-routes.py` 通过；operation 查询/对账路由已纳入公共 OpenAPI，Realtime 独立服务路由可被一致性检查器正确识别。

### 阶段 3：统一幂等与未知结果协议

合并实施 R3、R6、R7。

完成标准：

- 分集任务恢复有界 Activity retry 和 Continue-As-New。
- 同一逻辑请求只产生一次可确认的 Provider 副作用和成本记录。
- `unknown_outcome` 不会被 lease 过期或 Temporal retry 自动重发。
- HTTP operation resource 与幂等记录可恢复。
- 业务输入不会被哈希规范化误删。
- 失败分集可独立创建新 generation 重试任务。

完成记录（2026-07-15）：

- Source-to-Script 改为父 Workflow 编排独立分集子 Workflow；单集 Activity 最多重试 3 次，单集失败不会阻断其余分集，长任务使用显式 workset 和 Continue-As-New 继续执行。
- 分集 Provider 调用使用由 root Workflow、attempt generation、分集节点和 prompt hash 组成的稳定逻辑键；Temporal Activity retry 保持同一键，只有用户显式重试才创建新 generation。
- Provider Gateway 在调用前持久化 `provider_requests`/`provider_call_logs`，并发重复请求返回 in-progress，成功重放复用同一 Provider request/call/cost；运行记录过期只会进入 `unknown_outcome`，不会自动接管重发。
- Workflow 创建、runtime operation、输入快照、Temporal start outbox 与幂等结果在同一 Repeatable Read 事务提交；响应丢失后可用同一 Idempotency-Key 重放，也可通过 operation 查询/对账恢复权威结果。
- 请求哈希采用带版本的顶层执行字段剥离，只移除 Gateway 自身的 idempotency/retry 控制字段；嵌套业务对象中同名字段继续参与哈希。
- 通用失败重试入口已支持 `source_to_script`：只读取失败分集，创建新 Workflow Run、递增 `attempt_generation`、保留 root/retry 链并持久化新的输入快照和 operation。
- Docker 网络内真实 PostgreSQL 集成测试通过：文本 Provider 幂等/并发/成本唯一性、stale request unknown outcome、stream v2 generation/replay、视频任务创建幂等、HTTP idempotency/operation reconcile、失败分集新 generation 重试；相关 Workflow/API 单元测试、Web typecheck、OpenAPI parse 与路由一致性检查通过。

### 阶段 4：流式协议与事件消费

实施 R4、R5，并完成 R8 的前端消费收口。

完成标准：

- stream v2 使用明确 generation/attempt sequence。
- 首个 delta 后不自动 fallback。
- 成功幂等重放使用 `provider.replayed`。
- 所有领域事件都通过 catalog/generated emitter。
- 前端 invalidation map 穷尽且只刷新受影响查询。
- 连接建立和 cursor expiry 后恢复权威 REST 状态。

完成记录（2026-07-15）：

- Provider text stream 统一使用 `schemaVersion=2`、稳定 `providerRequestId`、唯一 `providerCallId`、`attemptGeneration`、`attemptSequence` 和分片 `sequence`；首个非空 delta 之后发生截断会返回 `UPSTREAM_STREAM_TRUNCATED`，不再尝试后备模型。
- 成功幂等重放只发送一个 `provider.replayed` 最终快照；Gateway client 会按 `(providerCallId, sequence)` 去重并拒绝分片缺口或尝试身份漂移。
- 所有项目领域事件统一通过 `internal/events` catalog emitter 写入；生成器检查会拒绝生产代码直接写 `event_outbox`、缺失 catalog 事件、aggregate 类型漂移和过期生成物。
- `event_outbox` 与 `project_event_log` 持久化 `schema_version` 和可选 `aggregate_revision`；Realtime `project-events.v2` envelope 统一补充 event、stream、aggregate 和常用业务实体身份。
- 前端事件映射由生成类型强制穷尽；分集剧本等高频事件只失效对应 script/episode 查询，未知新事件仍推进 cursor 并触发权威 REST 重同步，schema 不匹配、连接 ready 和 cursor expiry 均执行全量权威恢复。
- 顶部栏、任务抽屉、资产、内容、分镜和 AI 助手查询只在存在运行态对象或面板处于活动态时保留低频轮询，终态后停止空转；Realtime 事件和 mutation 继续负责即时失效。
- `go run ./cmd/events-gen --check`、`go test -count=1 ./cmd/events-gen ./internal/events ./apps/realtime ./internal/provider`、Web typecheck/lint 通过；Linux Go 1.26 容器 `-race` 通过。Docker 网络内真实 PostgreSQL 集成验证通过：持久事件事务/补发、stream pre-delta fallback、post-delta 禁止 fallback、generation/replay identity。

### 阶段 5：迁移、故障注入与长任务验证

构造至少 100 个独立单元的长任务，覆盖 Worker/Realtime 重启、Provider 超时、响应丢失、数据库提交不明、单项拒绝、用户取消、SSE 重连和失败重试。执行迁移往返和空库启动，确认部分失败不会污染成功项，恢复后不会重复调用或重复计费。

完成记录（2026-07-15）：

- 新增 `scripts/test-migrations.ps1` 和 `scripts/test-runtime-hardening.ps1`。脚本使用固定镜像启动无宿主机端口的临时 PostgreSQL 与独立 Docker 网络，测试结束自动销毁；不会读取或修改开发/生产数据库。
- 隔离迁移测试完成空库 `up -> seed -> snapshot -> down-to-zero -> up -> seed -> snapshot`，规范化 schema 一致；迁移与五类 system seed 的 apply/verify 均通过。
- 新增 100 资产长批次测试：并发峰值为 5，每代最多处理 50 项并通过 Continue-As-New 传递紧凑 checkpoint；第一代结果为 90 成功、10 拒绝、`partial_succeeded`，第二代只重试 10 个失败项，原成功项调用次数保持 1。
- 隔离故障矩阵覆盖 Realtime 在第 200/500 条事件后重建数据库连接、Provider 请求并发幂等与成本唯一性、stream 截断/重放、视频创建幂等、HTTP 响应丢失重放、operation reconcile、Workflow Start lease/AlreadyStarted 恢复、节点重启 token 栅栏、取消 deadline reconciler、单项拒绝和失败项新 generation 重试。
- 修复共享集成 fixture 只删除组织、不删除测试用户的问题；清除开发库中精确匹配 `workflow-gateway-*`、`preview-*` 的测试残留后，活动 Workflow、活动 Provider async task 和 `unknown_outcome` Provider request 均为 0。
- Linux Go 1.26 容器执行 `go test -race -count=1 ./apps/realtime ./internal/workflows ./internal/provider ./internal/api` 通过；`pnpm run test` 全仓通过，Web lint 仅保留 3 条既有 `<img>` warning，OpenAPI 257 条路由一致。
- `docker compose -f compose.yml --profile app up -d --build` 成功；Web、API、Realtime、Provider Gateway、Workers、PostgreSQL、Redis、Temporal、MinIO 均 Up/healthy，API readiness 四项为 `ok`，匿名 Realtime 订阅返回 401。无活动任务时受控重启 Realtime 与 Script Worker，二者均恢复 healthy。

## 7. 测试矩阵

| 场景 | 预期结果 | 主要层次 |
| --- | --- | --- |
| 未登录、同租户无权限或跨租户订阅 Realtime | 分别返回 401、403、统一 404，不泄露跨租户项目存在性 | Realtime + Auth |
| SSE 断线期间产生 500 个事件 | 按 cursor 分页补发，无遗漏、可去重 | Realtime + DB + Web |
| cursor 超出保留期 | 返回 410，前端 refetch 后从新水位恢复 | Realtime + Web |
| 批次运行中取消，子任务延迟成功 | 迟到结果被丢弃，批次保持 cancelled | Workflow + DB |
| child 永不响应取消 | deadline 后由 reconciler 收敛，不永久 cancelling | Temporal + DB |
| Provider task 取消结果未知 | 进入 unknown_outcome/cancel_failed，不自动重发 | Gateway + Workflow |
| 节点终态后重复完成 | CAS 更新 0 行，无状态回退 | DB |
| 创建任务同时修改项目手册 | 完整旧快照、完整新快照或 409，不得混合 | API + DB |
| 快照期间切换 Prompt/模型绑定 | snapshot IDs/hash 内部一致 | API + DB |
| Provider 成功后 Worker 崩溃 | retry 命中同一逻辑结果，不重复计费 | Workflow + Gateway |
| Provider 结果无法确认 | unknown_outcome，必须显式 reconcile/retry | Gateway |
| 单集失败、其余成功 | 批次 partial_succeeded，可只重试失败集 | Workflow + API |
| stream 在首个 delta 前失败 | 可按策略 fallback | Gateway |
| stream 在首个 delta 后失败 | 不 fallback，返回 UPSTREAM_STREAM_TRUNCATED | Gateway + consumer |
| 成功流式请求幂等重放 | 单个 provider.replayed，不重复拼接 | Gateway + consumer |
| 新后端事件未登记 catalog | CI 失败 | Event contract |
| 前端遗漏事件 invalidation | TypeScript typecheck 失败 | Web |
| HTTP operation 已提交但响应丢失 | 返回同一 operation，不重复创建 | API + DB |
| 同 key 不同 request body | 返回 409 conflict | API |
| tool schema 内含 retry 字段 | 字段参与哈希，不发生误回放 | Gateway |
| hash schema version 改变 | 不命中旧算法结果 | Gateway |
| 空库 up/down/up | 规范化 schema snapshot 一致 | Migration |
| Worker 在 cancelling 阶段重启 | replay 后继续原 deadline 和收敛流程 | Temporal |

## 8. 验收命令

先运行窄范围测试：

```powershell
$ErrorActionPreference = 'Stop'
go test -count=1 ./apps/realtime ./internal/workflows ./internal/provider ./internal/api
go test -race -count=1 ./apps/realtime ./internal/workflows ./internal/provider ./internal/api
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
```

启用数据库、Temporal 和 Realtime 集成测试；环境变量沿用仓库测试约定：

```powershell
$ErrorActionPreference = 'Stop'
$env:CINEWEAVE_INTEGRATION_TEST = '1'
go test -count=1 ./apps/realtime ./internal/workflows ./internal/provider ./internal/api
```

本计划需要新增统一迁移验证脚本：

```powershell
$ErrorActionPreference = 'Stop'
pwsh -NoProfile -File scripts/test-migrations.ps1
```

该脚本必须自动创建隔离测试数据库，执行 migrate up、seed、schema snapshot、down-to-zero、再次 up/seed/snapshot 并比较结果；不得操作开发或生产数据库。

全仓验证：

```powershell
$ErrorActionPreference = 'Stop'
pnpm run test
python -c "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))"
python scripts/check-openapi-routes.py
docker compose -f compose.yml config --quiet
```

运行环境验证：

```powershell
$ErrorActionPreference = 'Stop'
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

## 9. 运行时 Smoke Test

1. 未携带 token 请求 Realtime 返回 401；同租户无权限返回 403；跨租户和不存在项目返回相同 404。
2. 登录后建立 fetch-SSE，确认服务端只推送当前组织和项目事件。
3. 断网后产生超过 200 个事件，恢复网络后从 Last-Event-ID 完整追赶且任务数字不重复。
4. 启动至少 20 个资产或分集单元的批次，运行中取消，确认 deadline 内收敛且取消后没有新增业务产物。
5. 模拟一个不响应取消的 child 和一个长期 cancelling Provider task，确认最终进入受控终态并产生告警。
6. 启动分集剧本生产，生成中重启 Worker，确认已完成分集不重复，未完成分集继续执行。
7. 在 Provider 已接收请求后中断 Worker，确认结果可恢复或进入 unknown_outcome，不发生静默重复计费。
8. 让一个分集因参数错误失败，确认父任务为 partial_succeeded，并可只重试失败分集。
9. 在任务创建期间并发修改视觉手册、Prompt version 和业务模型绑定，确认 snapshot 不混合。
10. 模拟 stream 在首个 delta 前后失败，确认 fallback 边界与错误展示符合协议。
11. 打开 AI 助手和任务活动，确认每集/每镜头完成后立即出现动态输出。
12. 重启 Realtime 实例并刷新 access token，确认连接恢复、cursor 连续且页面状态不丢失。

## 10. 进度文档修订建议

`docs/runtime-foundation-hardening-progress.md` 当前把相关基础项标记为完成。实施本计划时应重新打开：

- P0 Realtime security/durable cursor：新增 R8。
- T3 Provider 幂等：重新打开 R3、R6、R7。
- T5 Durable asset batch cancellation/CAS/realtime：重新打开 R1、R5、R8。
- T6 streaming/replay contract：重新打开 R4。
- 任务快照一致性：补充 R2 独立条目。
- Migration gate：增加空库 up/down/up schema snapshot 条目。

只有失败测试、实现、数据库/Temporal 集成测试、迁移往返和运行 smoke 全部通过后，才重新标记完成。不能仅依据代码路径存在或普通单元测试通过判定完成。

## 11. 风险与发布策略

### 11.1 Temporal 确定性

修改已部署 Workflow 的控制流可能破坏历史 replay。使用 Temporal version marker 或创建新 Workflow type/version；开发环境如果不保留运行实例，可以在部署前明确清理。不能只因为不兼容旧业务数据，就忽略 Temporal history 兼容。

`execution_token`、deadline 和幂等身份在 Workflow 启动前生成并持久化。Workflow 内只使用确定性 API。Continue-As-New 必须传递 root operation、generation、deadline 和已完成单元摘要。

### 11.2 协议切换

Provider Gateway、Worker、API、Realtime 和 Web 按兼容顺序部署。stream v2 和 project event envelope 可短期双读；生产者确认全部消费者升级后删除 v1。

Realtime 鉴权属于阻断性安全修复，服务端不能在新 payload 上线后继续接受匿名连接。Web fetch-SSE 和 Realtime Bearer 验证应同一发布窗口切换。

### 11.3 事件日志容量

`project_event_log` 是增量恢复日志，不是永久审计仓库。默认保留 7 天并提供容量、最早/最新 position、清理延迟指标。审计事件若需长期保存，应进入独立审计表或归档存储，不能无限延长 Realtime 表。

### 11.4 并发与锁竞争

项目行锁只覆盖配置 mutation 和快照创建短事务，不能跨 Provider 调用持锁。所有外部调用在事务外执行，结果写入时重新进行短事务栅栏校验。监控项目锁等待和 serialization retry，出现热点时再按配置域拆分 revision。

### 11.5 Unknown outcome 运维

`unknown_outcome` 是真实业务状态，不应自动伪装为失败或零成本。任务活动需要显示“结果待确认”，运维面板提供 operation、Provider request/call 和潜在成本查询。显式重试必须保留原记录并建立 generation 链。

### 11.6 外部依赖与供应链

允许引入外部库，但每个运行时依赖都需要 ADR、许可证和安全检查、精确版本、协议测试及移除路径。仅为少量 SSE framing 或 JSON canonicalization 引入大型框架时，应优先使用小型、可审计实现。

### 11.7 可观测性

新增以下指标和结构化日志：

```text
workflow_late_result_discarded_total
workflow_terminal_cas_conflict_total
workflow_cancellation_deadline_exceeded_total
workflow_cancellation_reconcile_seconds
idempotency_reclaim_total
idempotency_unknown_outcome_total
idempotency_lease_expired_total
provider_logical_request_replay_total
provider_stream_truncated_total
realtime_auth_denied_total
realtime_cursor_expired_total
realtime_catchup_events_total
realtime_unknown_event_total
project_snapshot_revision_conflict_total
project_snapshot_serialization_retry_total
```

告警至少覆盖：取消超过 deadline、unknown outcome 长时间未处理、Realtime 401/403 异常突增、cursor expiry 过高、事件日志清理停滞和项目快照冲突激增。

## 12. Definition of Done

本计划只有同时满足以下条件才算完成：

- Realtime 所有项目事件都经过认证、项目权限校验和租户隔离。
- 断线期间超过旧 200 条窗口的事件可按持久 cursor 恢复；cursor 过期后能安全 refetch。
- 已取消或已失败的批次不会再产生业务数据写入。
- 不响应取消的 child/Provider task 不会让任务永久停留在 cancelling。
- Workflow 和 node 终态不会被迟到回调覆盖。
- 项目快照不存在混合 revision/version，所有相关配置 mutation 遵守统一 revision 纪律。
- 同一分集逻辑请求在 Worker 重启、Activity retry、Temporal replay 和 Continue-As-New 下只产生一次可确认上游副作用。
- HTTP 幂等能够区分 retryable、terminal 和 unknown outcome，不会因 lease 过期重复副作用。
- Provider 请求哈希保留全部语义业务字段并具有 schema version。
- Text stream 在首个 delta 后不自动 fallback，replay 不伪造原始 delta。
- 后端新增事件无法绕过 catalog，前端 invalidation map 必须穷尽。
- 长任务支持 partial_succeeded、失败单元重试、取消恢复和实时进度。
- 空库 migrate up/down/up 的规范化 schema snapshot 一致。
- 后端、前端、OpenAPI、Compose、数据库/Temporal 集成测试、故障注入和运行 smoke 全部通过。
