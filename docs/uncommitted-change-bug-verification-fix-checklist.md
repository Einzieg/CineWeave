# CineWeave 未提交变更缺陷收口与运行时重构清单

> 状态：实施与自动化验证完成 15/15；部署和运行态验收另行记录  
> 修订版本：v3（长期可维护方案）  
> 验证日期：2026-07-21  
> 验证基线：`main@fb42897f1b694532aed8ecdd6eee7d04c6295f9a` 与当前未提交更改  
> 适用范围：认证与组织、Provider、视频生产、剧本分集、衍生资产、Web 状态与权限 UI

## 1. 文档目的

本文件对当前未提交代码审查中提出的 15 个问题逐项验证，并将修复工作组织为可独立发布、可观测、可重试、可回滚的长期实施方案。目标不是在现有调用点继续增加条件判断，而是统一修正执行身份、耐久状态、并发写入、失败恢复和版本升级边界。

本清单同时承担三项职责：

1. 记录每个缺陷的可复现证据和业务风险。
2. 固化跨模块必须共同遵守的数据不变量和状态机。
3. 给出按依赖拆分的发布批次、验收门槛和旧路径退役条件。

本清单不替代详细 API 设计或数据库 migration 说明。每个发布批次仍应在对应代码或 migration 中写清输入契约、唯一键、状态转换和回滚条件。

本文件记录当前确认的问题、修复顺序和实施证据。实施与部署必须遵守以下边界：

1. API Server 和 Worker 不得直接调用上游供应商，所有供应商请求仍经 Provider Gateway。
2. 不应用主数据库迁移、不重建主环境，除非获得明确部署窗口。
3. `000036_provider_model_hard_delete.sql` 和 `000037_provider_model_deletion_rollback.sql` 视为已形成历史，不得原地重写；迁移问题使用新的前向保护或迁移执行器检查处理。
4. 不回滚或覆盖共享工作区中的组织/用户、Provider、视频生产及前端改动。
5. 项目处于开发阶段，不为旧业务数据增加兼容分支；安全令牌和正在运行的 Temporal Workflow 仍必须有明确升级或终止策略。
6. 不做一次性大爆炸切换。数据库采用 expand → dual-run/切流 → contract，Temporal 采用 v1/v2 并存 → 排空 → 退役。
7. 不引入万能 JSON 任务表。跨域只共享执行身份、幂等、状态和可观测性约定；视频、剧本、资产继续使用强类型领域表。

### 1.1 状态定义

每个 B 编号分别记录以下四层状态，避免把“代码已写”误报为“线上已修复”：

| 层级 | 完成条件 |
| --- | --- |
| 实现 | 代码、迁移、契约和事件投影已完成 |
| 自动化验证 | 反例测试、正常路径和故障注入通过 |
| 部署 | 在明确窗口完成 migration/服务切流，部署检查通过 |
| 运行态验收 | 主环境 smoke、指标和审计查询证明缺陷不再出现 |

文中的 `[x]` 只表示该条实现或测试已在当前工作区完成，不自动代表已经部署。B01 至 B15 当前均处于“实现 + 自动化验证完成”，主环境部署状态不得从本文件推断。

### 1.2 v3 关键修正

相对 v2，本版作出以下架构修正：

1. Render Plan 创建与 episode item 绑定改为同一数据库事务，删除中间崩溃窗口。
2. 公共执行身份改为最小 base + 领域强类型扩展，不使用大量 nullable 字段或万能任务 JSON。
3. terminal 状态处理拆成显式 reconcile 命令和只读 output projection，避免 loader 产生隐式副作用。
4. item 被定义为不可变 attempt；retry 创建新 attempt，旧计划、供应商任务和产物 provenance 不覆盖。
5. 小说继续直接使用 durable `source_chapter_id`，不为当前一章一集流程提前创建重复抽象。
6. 批量请求区分 request item 和 execution attempt，预检失败项也进入完整 workset 和最终结果。
7. 完成状态拆为实现、自动化验证、部署、运行态验收四层。
8. 实施顺序改为可独立发布的 Release Train，并明确 expand、切流、观察、排空、contract 门禁。

## 2. 验证方法与结论

### 2.1 验证方法

每项问题至少使用以下一种方式验证：

- 对当前磁盘代码执行完整调用链检查，确认输入、查询条件、状态变化和最终写入之间存在可达路径。
- 对照 `HEAD` diff，确认问题位于当前未提交或未跟踪代码中。
- 检查现有测试是否包含对应反例。
- 运行受影响 Go 包、Web typecheck 和 lint，确认现有绿灯是否能够发现该问题。

本轮没有修改主数据库、没有调用真实供应商、没有重建 Docker Compose。

### 2.2 已执行验证

```powershell
go test ./internal/auth ./internal/provider ./internal/workflows ./internal/api -count=1
pwsh -NoProfile -File scripts/test-runtime-hardening.ps1 -DerivedAssetOnly
pnpm run test
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
git diff --check
```

结果：

- 四个 Go 包测试全部通过。
- Web typecheck 通过。
- Web lint 无错误，有 3 条现存 `<img>` 性能警告和 1 条现存 Hook 依赖警告。
- `000043` 隔离 PostgreSQL migration 与 B06/B13 API/Workflow 故障注入矩阵通过。
- 根测试通过，OpenAPI 与 Go 注册的 323 条路由一致，Compose 配置有效。
- `git diff --check` 无 whitespace error，仅有 Windows LF/CRLF 转换警告。
- 这些结果不能否定下表问题，反而说明现有测试没有覆盖对应竞态、失败分支和跨身份反例。

### 2.3 验证状态定义

- `已确认`：单线程确定性代码路径即可触发，不依赖外部时序。
- `已确认（条件触发）`：需要并发、重试、部分失败、旧 token 或特定访问顺序，但当前代码没有阻止条件成立。
- `降级确认`：问题成立，但影响面小于初审判断，已调整优先级。
- `未确认`：当前代码无法形成所描述的行为。本轮没有此类项目。

## 3. 验证总表

| 编号 | 优先级 | 验证结论 | 当前交付状态 | 问题 | 主要影响 |
| --- | --- | --- | --- | --- | --- |
| B01 | P0 | 已确认 | 实现/自动化验证完成 | 组织 owner 可为同组织的系统管理员签发密码重置令牌 | 系统管理员账号可被租户级权限接管 |
| B02 | P1 | 已确认（竞态可达） | 实现/自动化验证完成 | 恢复出的 Render Plan ID 在最终加载时丢失 | workflow 可能执行另一 workflow 的活动计划 |
| B03 | P1 | 已确认 | 实现/自动化验证完成 | 分集视频 item 固化执行前的旧 Render Plan | 审计、查询、清理和状态投影指向错误计划 |
| B04 | P1 | 已确认 | 实现/自动化验证完成 | 剧本部分成功时失败分集从新版本消失 | 当前剧本产生静默数据缺失 |
| B05 | P1 | 已确认 | 实现/自动化验证完成 | 删除前置章节并重排后会误排除另一集 | 稳定章节身份与可变集序混用导致错集 |
| B06 | P1 | 已确认 | 实现/自动化验证完成 | 旧 generation 的衍生资产需求仍可直接执行和回写 | 过期生产数据产生供应商成本并污染历史代 |
| B07 | P1 | 已确认（生命周期触发） | 实现/自动化验证完成 | 被移除成员重新受邀后，移除前 access token 恢复有效 | 已撤销的组织访问能力重新生效 |
| B08 | P1 | 已确认 | 实现/自动化验证完成 | `first_frame_plus_references` 契约无法执行 | 已声明支持的续接模式必然失败 |
| B09 | P1 | 已确认 | 实现/自动化验证完成 | 首批提交前失败/取消的 checkpoint 可被恢复为成功 | 任务终态与真实失败状态相反 |
| B10 | P2 | 降级确认 | 实现/自动化验证完成 | `000037` Down 可把历史计划改绑到同 key 的新模型 | 开发回滚时历史 provenance 被静默篡改 |
| B11 | P2 | 已确认（访问顺序触发） | 实现/自动化验证完成 | 带预览与不带预览的资产查询共用缓存 key | 资产缩略图依赖访问顺序，最多 30 秒不显示 |
| B12 | P2 | 已确认 | 实现/自动化验证完成 | Web 权限状态缺失时 fail-open | 只读用户看到写入口并在提交时收到 403；后端未被绕过 |
| B13 | P2 | 已确认 | 实现/自动化验证完成 | 显式选择的未审核衍生资产需求被批次静默丢弃 | 批次错误显示成功且缺少逐项结果 |
| B14 | P2 | 已确认（信息泄露） | 实现/自动化验证完成 | 视频模型测试按 project ID 跨组织读取生产上下文 | 可通过错误差异探测其他组织项目状态；Gateway 会阻止上游调用 |
| B15 | P2 | 已确认（请求竞态） | 实现/自动化验证完成 | 项目设置保存成功会清除请求期间的新编辑 | 用户输入被静默覆盖 |

所有行的“部署”和“运行态验收”当前均未在本文确认；进入部署窗口后必须逐项补记，不得批量假定成功。

### 3.1 长期实现架构原则

后续实现必须同时满足以下原则，不能再为单个调用点增加互相独立的补丁：

1. **身份先于执行**：任何供应商调用开始前，必须固化并校验 organization、project、production generation、binding revision、workflow run、业务对象、operation item、attempt 和执行计划身份。运行中不得通过“当前活动记录”重新猜测身份。
2. **单一事实来源**：Temporal 负责确定性编排，PostgreSQL 负责可查询的耐久业务状态，Provider Gateway 负责供应商调用状态。React Query、事件流、metadata JSON 和 Temporal memo 都只能是投影或快照，不能成为唯一事实来源。
3. **长任务全部耐久化**：可能跨 HTTP 超时、进程重启或供应商异步轮询的操作必须由 Temporal Workflow 承载。API 只在事务中创建命令、身份快照、workflow run 和 outbox，并返回 `202 Accepted`。
4. **领域表强类型化**：共享 `ExecutionIdentity`、状态转换、错误 envelope 和幂等约定；视频 item/segment、剧本 staging、资产 work item 继续使用独立表和外键，不把领域状态塞入一个万能任务 JSON。
5. **子任务只写 staging，单一 finalizer 写正式版本**：可并发、可部分失败的剧本分集生成不得直接修改最终 `script_episodes`。子任务写独立结果，finalizer 在锁内一次性组装不可变版本。
6. **状态只允许单调前进**：所有状态更新必须是带 expected status/revision 的 CAS；terminal 状态不可被普通执行路径恢复为 running，也不可从 failed/cancelled 推导成 succeeded。
7. **读投影与修复命令分离**：只读 loader 不隐式修改业务状态。状态不一致由显式、幂等的 reconcile Activity 修复；修复后再由纯投影 loader 生成输出。
8. **授权使用单调代次**：access token、refresh session、组织选择 token 和 nonce 必须绑定同一个 membership authorization version。成员生命周期变化后，旧授权材料不能因重新邀请而复活。
9. **Temporal 契约只能增量升级**：已有 Activity 的必填字段和语义不得原地破坏。重大输入或语义变更使用 v2 Activity/Workflow 名称；`workflow.GetVersion` 只用于同一 workflow definition 内可证明可 replay 的分支。
10. **旧实现延迟退役**：新版本上线后先把新任务路由到 v2，同时保留 v1 worker 注册，直到旧 workflow 和 provider task 清零或被明确终止，再通过独立 contract 发布删除旧契约和字段。
11. **Workflow 保持确定性**：Workflow 代码不直接访问数据库、对象存储、网络、随机数或系统时钟；所有 I/O 进入 Activity。Continue-As-New 只携带版本化 identity、cursor 和小型快照，不携带无限增长的完整结果。
12. **启动与事件可幂等**：outbox dispatcher 使用稳定 Temporal workflow ID，重复投递只确认同一执行；实时事件至少一次投递，Web projection 按 event ID/revision 去重，不能把事件到达顺序当作业务真相。

### 3.2 统一执行身份信封

跨 API、Workflow、Activity 和数据库边界使用最小公共身份；各领域通过强类型扩展补齐自己的必填字段，避免一个结构充满 nullable 字段：

```text
ExecutionIdentityBase
  schemaVersion
  organizationId
  projectId
  workflowRunId
  temporalWorkflowId
  operationId
  operationItemId
  attempt

VideoExecutionIdentityV2
  ExecutionIdentityBase
  productionGenerationId
  videoProductionBindingId
  videoProductionBindingRevision
  storyboardShotId
  configurationSnapshotHash
  executionPlanId / segmentId（按阶段必填）

ScriptGenerationIdentityV2
  ExecutionIdentityBase
  sourceId
  sourceRevisionHash
  sourceChapterId
  baseScriptVersionId

DerivedAssetExecutionIdentityV2
  ExecutionIdentityBase
  productionGenerationId
  videoProductionBindingId
  videoProductionBindingRevision
  shotAssetRequirementId
```

规则：

- 身份结构是版本化 Go 类型和关系字段集合，不是任意 JSON。
- 身份字段在 operation/item 创建后不可更新；重试创建新 attempt/item，不覆盖旧 attempt。
- 每个 Activity 使用阶段专用输入类型，构造时验证全部必填字段；不接受零值 UUID 作为“自动查找当前记录”。
- Provider Gateway 只接收已固化的 Provider execution identity，并在解密凭据或发起 HTTP 前完成租户、generation、binding、workflow、plan 和 segment 联合校验。
- `active_*` 指针只用于 UI 默认选择和新命令规划，不得用于恢复正在执行的任务。

### 3.3 统一状态机与错误契约

长任务沿用领域表，但共同遵守下列状态语义：

```text
queued -> running -> succeeded
                  -> failed
                  -> cancelling -> cancelled
                  -> discarded
```

- `partial_succeeded` 只允许出现在 batch/checkpoint 聚合层，item 不使用该状态。
- terminal 状态集合为 `succeeded|failed|cancelled|discarded|partial_succeeded`；普通执行 Activity 不得离开 terminal 状态。
- 每次转换写入 `revision`、`started_at/completed_at`、规范化 `error_code/error_detail` 和 outbox 事件。
- 可重试性由稳定错误码决定，不根据 Temporal 包装文本猜测。领域拒绝、身份不匹配和 replan 均为 non-retryable；网络或短暂存储错误才允许有界重试。
- UI 只展示规范化中文错误和可执行的下一步；原始 Activity/Temporal/provider 诊断保留在技术详情与日志。

### 3.4 事务与幂等边界

- API command：业务预检、operation/work item、workflow run、idempotency key 和 outbox 必须同事务提交。
- 计划物化：锁定 item、创建 Render Plan/segments、绑定 item 必须在同一个 Activity 的同一数据库事务完成，不能先创建计划再由另一个 Activity 补 FK。
- Provider submit：以 `segment_id + attempt + request_hash` 为唯一幂等键；重放必须返回同一 provider request/task，不得重复计费。
- 媒体提交：以 `provider_task_id + output_index + content_hash` 去重；数据库提交成功后才允许投影“媒体已入库”。
- Finalizer：使用 expected revision/current version 的 CAS；重复执行返回第一次提交的结果，不能生成第二个正式版本。

### 3.5 可观测性最低要求

每个 operation、item、plan、segment 和 provider task 必须可通过 ID 正反向追踪，并至少提供：

- 状态、attempt、revision、创建/开始/结束时间。
- workflow/Temporal run、generation/binding、prompt/model/capability snapshot hash。
- provider request/task/call log/cost record 与最终 artifact/media file。
- 状态转换事件、重试原因、reconcile 结果和 discarded write 计数。
- 基础指标：排队时长、执行时长、成功/部分成功/失败率、重试次数、重复提交抑制数、身份拒绝数、卡住任务数。
- stuck 判定按 task type 的 lease/heartbeat/provider poll SLA 配置，不能只因长视频运行超过固定墙钟时间就判死；只要合法 heartbeat 或 provider progress 在推进，就保持 running。

## 4. P0 安全修复

### B01：保护系统管理员账号免受组织级密码重置

#### 验证证据

- `internal/auth/member_administration.go` 的 `ensureOrganizationCanManageMemberAccount` 只读取 membership/user 状态、账号所属组织数量和双方 owner 身份，没有读取 `users.is_system_admin`。
- owner 操作另一个 owner 时不会触发 `ErrMemberAccountProtected`。
- `IssueOrganizationMemberPasswordReset` 随后清空目标密码、递增 `credential_version`、撤销全部 session，并将一次性重置令牌直接返回调用方。
- `internal/api/organization_access.go` 只要求组织级 `member.manage`，没有系统级授权。

#### 修复清单

- [x] 在账户管理锁定查询中读取 `u.is_system_admin`。
- [x] 所有组织级资料修改、密码重置和账号生命周期入口拒绝操作系统管理员目标。
- [x] 返回统一的 `MEMBER_ACCOUNT_PROTECTED`，不要泄露目标是系统管理员的具体原因。
- [x] 本批次不新增系统管理员恢复入口；只收紧组织级接口，避免把新的高风险恢复面混入缺陷修复。
- [x] 将系统管理员恢复列为独立安全设计：至少明确近期再认证、双管理员/应急恢复策略、令牌交付渠道、审计告警和“系统仅剩一名管理员”处理后，才允许实施。

#### 必须新增测试

- [x] 系统管理员只属于一个组织，另一 owner 仍不能修改其资料。
- [x] 另一 owner 不能签发其密码重置令牌。
- [x] 普通 owner/member 的现有管理行为不回归。
- [x] API 返回 403 和稳定错误码，不返回 reset token。
- [x] 当前公开和组织级路由中不存在可绕过保护的系统管理员密码恢复入口。

#### 完成证据（2026-07-21）

- `internal/auth/member_administration.go` 和 `internal/auth/members.go` 已统一保护系统管理员目标，成员查询同步关闭 `accountManagementAllowed`。
- `internal/api/member_administration_errors_test.go` 验证 403 与 `MEMBER_ACCOUNT_PROTECTED` 通用错误契约。
- `go test ./internal/auth ./internal/api -count=1`、Web typecheck 通过。
- 隔离 PostgreSQL 完成 migration/seed 后，`TestOrganizationMemberAccountManagement` 通过；未连接或修改主数据库。

## 5. P1 视频生产一致性修复

### B02：Render Plan 必须绑定精确 workflow 身份

#### 验证证据

- `recoverableWorkflowShotVideoExecutionPlan` 返回精确 `ExecutionPlanID`。
- `EnsurePreparedShotVideoPlan` 在物化提示词后调用 `loadPreparedShotVideoPlan(loadInput)`，但 `LoadPreparedShotVideoPlanInput` 没有 `ExecutionPlanID`。
- 最终加载重新连接 `storyboard_shots.active_video_render_plan_id`；另一个 workflow 可以在两次操作之间激活新计划。
- Provider Gateway 的执行查询校验 organization/project/generation/binding/shot，但没有校验 `plan.workflow_run_id = req.WorkflowRunID`。

#### 修复清单

- [x] 不给现有 `LoadPreparedShotVideoPlanInput` 原地增加必填字段；它仍可能被旧 history 和 PromptOnly 路径重放。
- [x] 将 v2 契约拆成两个明确入口：`LoadApprovedShotVideoPromptPlanV2` 只读取已审核提示词契约，不产生可执行计划；`LoadExecutableShotVideoPlanV2` 必须携带完整 `VideoExecutionIdentityV2` 和精确 `ExecutionPlanID`。
- [x] 新增 `MaterializeAndBindExecutableShotVideoPlanV2`：在同一事务中锁定 episode item，验证 prompt/reference/capability/configuration snapshots，创建 Render Plan 与 segments，并把精确 plan ID 绑定到 item；Activity 返回完整执行身份。
- [x] 后续加载只按 plan ID、item ID、workflow run、generation、binding revision、shot 联合查询，不再连接 `storyboard_shots.active_video_render_plan_id` 推断目标。`active_video_render_plan_id` 只作为 UI/下一次规划指针。
- [x] 精确计划已 supersede、归档或身份不符时返回 `RENDER_PLAN_REPLAN_REQUIRED`，不得回退到当前活动计划。
- [x] Gateway 在凭据解密、lease 获取和真实上游调用前强制校验 `video_render_plans.workflow_run_id`、operation item、generation/binding 和 segment，并让 provider request、async task、call log 和 cost record 继承同一执行身份。
- [x] 新任务改用 v2 Activity/Workflow type；v1 注册保留到旧 history 排空，防止旧 payload 被新必填校验破坏。

#### 必须新增测试

- [x] workflow A 恢复 plan A，workflow B 激活 plan B 后，A 不能加载或执行 B。
- [x] 身份不匹配时上游 HTTP 请求计数必须为 0。
- [x] 同 workflow 的 retry/Continue-As-New 仍可恢复自己的计划。
- [x] PromptOnly 路径不需要伪造 execution plan ID，且其结果不能直接用于视频执行。
- [x] 在计划物化事务提交前注入崩溃时，plan 与 item 均不落库；提交后重放返回同一 plan，不产生第二个 plan/segment。
- [x] 用旧 history 做 replay 测试，确认部署 v2 后 v1 workflow 仍可确定性重放。

#### 完成证据（2026-07-21）

- 新增 v2 prompt-plan、原子物化绑定和精确 executable-plan Activity；v1 Activity/Workflow 保留并通过 Temporal history replay。
- Provider Gateway 在上游前校验 workflow/item/attempt/plan/segment，并把同一身份写入 provider request、async task、call log 与 cost record。
- 隔离 PostgreSQL 通过身份错配上游请求计数为 0、物化事务回滚/幂等重放和跨表 provenance 测试。

### B03：episode item 记录实际执行 Render Plan

#### 验证证据

- `PrepareEpisodeVideoProductionBatch` 把 prepare 阶段读取的 `shot.VideoRenderPlanID` 写入 item。
- 子 workflow 随后可能创建新的 Render Plan，并通过 `ShotVideoOutputs.ExecutionPlanID` 返回。
- `CommitEpisodeVideoProductionBatch` 只更新状态、错误和 `provider_async_task_id`，没有更新 `video_render_plan_id`。

#### 修复清单

- [x] 明确 `episode_video_production_items` 是一次不可变执行 attempt，而不是可被覆盖的“当前镜头行”；prepare 阶段将其 `video_render_plan_id` 留空，前序计划只写独立 predecessor provenance。
- [x] 使用 B02 的 `MaterializeAndBindExecutableShotVideoPlanV2` 原子建立 item → plan 身份；禁止“先建 plan、后补 item FK”的两个 Activity 事务。
- [x] 为非空 `video_render_plan_id` 增加唯一约束，确保一个 Render Plan 只能属于一个 item；绑定时验证同一 workflow、generation、binding revision、shot 和 attempt，重复执行仅允许同一 plan ID。
- [x] item 只记录一个 Render Plan；一个计划内多个 segment/provider task 的事实来源统一为 `video_render_segments.provider_async_task_id`，并对 segment/provider task 建立 request hash 唯一约束。
- [x] API、取消、任务活动和清理逻辑通过 item → plan → segments → provider tasks 查询，不再依赖 item 上单一 `provider_async_task_id`，该旧字段在完成读路径迁移后通过后续前向迁移移除。
- [x] retry 创建新的 item attempt，旧 item/plan/segments 保持只读历史；不得在同一 item 上换绑另一个 plan。
- [x] 缺少、重复或身份不匹配的计划/输出不得标记 item 成功。commit 只使用既有执行身份提交终态和媒体结果，不负责第一次建立计划身份。

#### 必须新增测试

- [x] prepare plan 与执行 plan 不同时，item 最终引用执行 plan。
- [x] Provider task、Render Plan、item 和媒体 artifact 的身份完全一致。
- [x] 重试产生新计划时，旧 item 保持历史，新 attempt 指向新计划。
- [x] 计划物化事务提交后、上游提交前 Worker 崩溃，恢复后 item 仍指向同一计划且不会创建第二个计划。
- [x] 多 segment 计划的查询和取消能覆盖全部 provider task，不丢失后续 segment。
- [x] 数据库拒绝两个 item 绑定同一个非空 plan，且拒绝同一 segment attempt 的重复 request hash。

#### 完成证据（2026-07-21）

- `000040_video_execution_identity_v2.sql` 固化 operation/item/attempt/plan/segment 身份、非空 plan 唯一约束及 segment request hash 唯一约束。
- 任务活动 API 按 item → plan → segments 返回每个 segment 的全部 provider task；取消测试验证只取消当前活动 segment，不误取消已完成 segment。
- 隔离 PostgreSQL 验证跨 item、plan、provider request/task/call/cost 和媒体 generation 的一致身份。

### B08：完整支持 `first_frame_plus_references`

#### 验证证据

- Workflow 只在 contract 精确等于 `first_frame` 时提取前一片段尾帧。
- Activity 已有 `first_frame_plus_references` 分支，因此会因缺少 fresh 尾帧而失败。
- Gateway contract switch 只有 `video_extension` 和 `first_frame`，没有 plus-references 分支。

#### 修复清单

- [x] 在独立 `videocontracts` 包定义唯一的 contract enum、capability predicate 和 `ReferenceManifestV2`；Planner、Workflow、Gateway、OpenAPI 和 Web 类型从同一语义生成或映射，禁止各自维护 switch。
- [x] 增加统一 `RequiresPreviousTailFrame(contractKey)`，覆盖 `first_frame` 与 `first_frame_plus_references`；提取结果必须记录来源 segment、artifact/media、content hash 和生成时间。
- [x] plus-references 同时携带 fresh 尾帧和计划冻结的语义参考图；reference role、稳定顺序、来源资产版本和内容 hash 全部进入 manifest 与 request hash。
- [x] Gateway 按模型已批准 capability snapshot 校验尾帧来源、reference role、数量、类型和顺序；能力不足或快照未批准在 lease/上游前失败。
- [x] 合约快照不可在运行中根据当前模型配置重算；模型配置变化只影响新 Render Plan。

#### 必须新增测试

- [x] 多片段 plus-references 成功提取尾帧并创建下一段。
- [x] 缺尾帧、尾帧来源错误、语义参考超限均在上游前失败。
- [x] `first_frame` 现有单参考行为不回归。
- [x] 相同 reference manifest 重放得到相同 request hash；更换任一参考图版本后 hash 必须变化。

#### 完成证据（2026-07-21）

- `internal/videocontracts` 统一了 contract key、续接 predicate、`ReferenceManifestV2`、契约角色顺序和 manifest hash；Provider、Workflow、OpenAPI 与 Web contract key 类型均映射到同一枚举集合。
- `first_frame` 与 `first_frame_plus_references` 多片段执行都会提取带 source segment、artifact/media、content hash、anchor ID 和生成时间的 fresh 尾帧；plus 模式继续携带冻结语义引用。
- Workflow 与 Gateway 使用同一 canonical reference order。隔离 PostgreSQL 真实 reference pack 回归发现并修复了首帧可能被通用 priority 排到语义引用之后的问题。
- Gateway 在模型选择、lease 和上游调用前校验批准的 capability snapshot、Render Plan、尾帧来源与完整冻结 reference pack；不完整 manifest 仅形成失败的幂等 provider request，lease、async task、call log、cost record 和 HTTP 上游调用增量均为 0。
- 多片段两种首帧契约、缺失/篡改/超限引用、manifest/request hash 稳定性和参考版本漂移测试通过；`pnpm run test`、OpenAPI 322 路由检查及隔离 PostgreSQL 集成测试通过。

### B09：terminal checkpoint 必须从 durable 状态恢复

#### 验证证据

- terminal checkpoint 分支调用 `loadEpisodeVideoProductionOutputTx`。
- 该函数只汇总 batch metadata 中的 `batchOutput`，没有读取 checkpoint status、failure metadata 或 durable item 状态。
- 首个 batch commit 前失败时没有 batch output；空的 success/fail/cancel 集合被 `batchShotOutputStatus` 判为 `succeeded`。

#### 修复清单

- [x] 将现有 loader 拆成 `ReconcileEpisodeVideoCheckpointV2` 和纯读取的 `LoadEpisodeVideoCheckpointOutputV2`；查询输出不得隐式修改状态。
- [x] reconcile 在同一事务中锁定 checkpoint，并读取其固化目标 shot、batch、item、plan、segment 和 provider task 终态；按 checkpoint snapshot 重建 succeeded/failed/cancelled 集合，禁止只依赖 batch JSON。
- [x] checkpoint 在建 batch 前失败时，将所有未完成目标归入 failed；取消 checkpoint 则归入 cancelled。未知或缺失 item 必须按 checkpoint 的保守终态归类，并记录诊断原因。
- [x] 顶层状态以 checkpoint 的 durable terminal 状态为上限，再结合 item 聚合计算 `failed`、`cancelled` 或 `partial_succeeded`；空 workset 只有在命令本身明确为空且成功结束时才可为 succeeded。
- [x] reconcile 使用 revision CAS，修正可推导的 item/batch/checkpoint 投影并写 outbox 事件和 metric；重复执行必须返回同一结果。
- [x] 业务聚合不一致返回可解释的保守 terminal output，不抛出可重试 Activity error；只有数据库、网络等基础设施错误才按有界策略重试。
- [x] 增加定时 stuck-task reconciler，只扫描超过租约/心跳阈值的非终态记录；不得与正常 Workflow 同时无条件改写同一 item。

#### 必须新增测试

- [x] prepare 前失败、prepare 后 commit 前失败、取消三个场景恢复正确。
- [x] 空 item 集合但 checkpoint=failed 时输出仍为 failed。
- [x] 已成功 item 保留成功，其余失败时输出 partial_succeeded。
- [x] 人为制造 batch/item/checkpoint 不一致时，reconcile 幂等修复一次，loader 返回可解释的保守终态，不发生 Activity 重试循环。
- [x] 正常 Workflow 持有有效租约时，定时 reconciler 不抢占或篡改其状态。

#### 完成证据（2026-07-21）

- v2 reconcile 与纯 output projection 已拆分，revision CAS 和 `video.production.checkpoint.reconciled` 事件覆盖可修复投影。
- 周期 reconciler 仅处理超过 SLA 且无有效进展的任务；活动租约/heartbeat 测试验证不会抢占正常 Workflow。
- 隔离 PostgreSQL 通过 prepare 前失败、commit 前失败、取消、部分成功、空 workset 失败和幂等恢复测试。

## 6. P1 剧本与资产数据完整性修复

### B04：部分失败不得删除旧剧本分集

#### 验证证据

- `forkSourceScriptVersionTx` 在新版本中预先排除全部目标 episode index 和 source chapter ID。
- 子 workflow 失败时只增加失败计数，不向新版本写入 episode。
- 只要 `CompletedEpisodeCount > 0`，finalize 就激活新版本。
- 因此混合成功/失败时，失败目标既未复制旧集，也没有新集。

#### 修复清单

- [x] 新增专用 `script_episode_generation_results` staging 表；小说子 Agent 只按 `workflow_run_id + attempt_generation + source_chapter_id` 幂等写入成功内容、错误、content hash、provider/prompt/model provenance，不直接写 `script_episodes`。
- [x] workflow 开始时创建不可变 generation manifest，固化 source content revision/hash、章节 ID 与顺序、base script/version、目标章节集合、提示词/模型/手册快照；finalizer 发现任一基线变化时返回稳定 replan 错误。
- [x] finalizer 在 project active script、script 和 version 锁内一次性创建新的不可变 script version，并按 manifest 完整组装：成功目标使用 staging 结果，失败目标有旧集时保留旧内容并标记 stale，未触碰目标沿用旧内容，已删除 source unit 不复制。
- [x] 新章节失败且没有旧集可回退时，新版本只保留 draft/partial 状态且不得激活；current version 保持不变，UI 明确显示缺失章节。
- [x] 所有正式 episode 在同一事务中按最终章节顺序插入，避免先复制再改序触发 `(script_version_id, episode_index)` 唯一约束冲突。
- [x] 激活使用 expected active script/current version/revision 的 CAS；失败重试创建新的 attempt generation，只重跑失败 staging 单元，再由同一 finalizer 重组装。
- [x] staging 设置 retention 与清理规则：被正式版本引用的 provenance 永久保留摘要，未引用 payload 在可配置期限后清理，不让百万字项目无限膨胀。

#### 必须新增测试

- [x] 重生成两集，一集成功一集失败；失败集内容仍等于旧版本。
- [x] 新章节失败时不会伪造旧内容，但失败集在状态和 UI 中可见。
- [x] 全部失败时不切换 current version。
- [x] 子 Agent 重试、Worker 重启和重复 finalizer 不会产生重复 episode 或第二次激活。
- [x] 删除/插入章节导致 episode index 平移时，finalizer 不触发唯一约束冲突。
- [x] finalizer 与用户切换 active script/version 并发时，只有匹配 expected revision 的一方成功，另一方进入可重试 replan。

#### 完成证据（2026-07-21）

- `000042_source_to_script_generation_staging.sql` 增加不可变 generation manifest、staging result、source/script revision CAS、正式版本 provenance 和可配置 payload retention；script-worker 周期清理只清除过期大正文，保留身份、hash、模型/提示词 provenance 与正式剧本内容。
- Source→Script 子任务不再直接写 `script_episodes`；finalizer 在同一事务中按 manifest 组装完整版本，旧的直接生成 Activity 已停止注册新流量。
- 任务活动抽屉可区分“失败集回退后激活”和“新集缺失未激活”，并通过失败重试 API 只重跑失败章节 UUID。
- `pwsh -NoProfile -File scripts/test-runtime-hardening.ps1 -SourceToScriptOnly` 已通过迁移 Up/Down/Up、API retry 与全部 Source→Script 集成测试；workflow、script-worker、Web typecheck/lint 均通过（lint 仅现存 warning）。

### B05：小说分集只使用稳定章节身份

#### 验证证据

- 删除章节后 API 会压缩后续 `chapter_index`。
- fork 同时用 target chapter ID 和新的 episode index 排除旧版本内容。
- 删除 A 后 B/C 的新序号变化；只重生成 C 时，新序号可能等于旧 B 的 episode index，从而错误排除 B。

#### 修复清单

- [x] 小说直接使用已有 durable `source_chapter_id` 作为稳定身份，不为当前一章一集流程新增重复的 source unit 表；标题和序号永不作为身份。
- [x] work item、staging、去重、retry 和 provenance 全部使用 `source_chapter_id`，同时保存章节 content hash/revision 快照。
- [x] `episode_index` 只在 finalizer 创建新版本时由 generation manifest 的顺序派生，绝不参与跨版本 identity、排除或 upsert。
- [x] finalizer 只组装 manifest 内仍有效的 chapter ID；章节集合或内容 revision 变化必须重新规划，不能在运行中静默套用新顺序。
- [x] 不在已有版本上执行逐行重排；每个版本的 episode 顺序在创建事务中一次确定并保持不可变。
- [x] 若未来支持多章节合并为一集，新增显式 `script_episode_source_chapters` 关联表表达多对一 provenance，不改变现有 chapter ID；非小说 source 使用已有持久化 segment/episode ID，不得退回标题匹配或数组位置匹配。

#### 必须新增测试

- [x] A/B/C 删除 A，再重生成 C，B 必须保留且顺序变为 1。
- [x] 删除中间章节、插入章节和批量重生成组合不会产生重复 episode index。
- [x] retry 仍精确定位原失败 chapter ID。
- [x] 生成期间 source revision 改变时，旧 attempt 不写正式版本，并返回可重新规划状态。
- [x] 相同标题、跨卷同名章节和百万字导入场景仍按稳定 ID 正确组装，不发生标题匹配。

#### 完成证据（2026-07-21）

- 小说 generation item、staging、retry 和正式 episode provenance 全部以 `source_chapter_id` 为身份；manifest ordinal 仅用于最终版本展示顺序。
- 原文章节更新改为 ID 优先的 reconcile；未显式传 ID 时只按精确内容与卷/节身份复用，重复标题或歧义不会猜测绑定。
- 隔离 PostgreSQL 覆盖删除首章后重生成 C、删除中间章并插入同名新章后批量生成、跨卷同名章节、source revision 变化和精确失败章节重试。

### B06：衍生资产生成统一进入 durable Workflow

#### 验证证据

- `shotAssetRequirement` 只按 project ID 和 requirement ID 查询。
- 返回结构没有携带 `production_generation_id`。
- 进入 `image_running` 和写入成功结果的 UPDATE 只按 requirement ID。
- Provider 调用发生前没有验证 requirement 属于项目活动 generation。

#### 修复清单

- [x] 单项和批量生成共用同一个 versioned derived-asset Workflow；单项只是 workset 大小为 1，不再保留 API 同步调用 Provider Gateway 的第二套实现。
- [x] 使用领域专用 batch/item 表持久化完整请求 workset。API 在一个事务中验证 active generation，创建 immutable request snapshot、workflow run、batch/items、idempotency key 和 outbox，随后返回 `202 Accepted` 及可追踪任务 ID。
- [x] item 是一次不可变 attempt，至少固化 requirement、shot、canonical asset/source reference、generation/binding revision、prompt/reference/model/capability snapshots 与 request hash；retry 创建新 attempt，不覆盖历史。
- [x] Worker 在供应商调用前再次验证完整 `DerivedAssetExecutionIdentityV2`，并通过 status + revision + lease CAS 领取 item；领取失败不得调用 Gateway。
- [x] Provider Gateway 调用、媒体转存和结果提交由 Workflow 分步承载；成功/失败 commit 均携带 generation、revision、attempt 和 write fence，迟到结果只记诊断，不污染业务状态。
- [x] 增加显式 reconcile Activity 与定时 stuck-task 扫描，按 provider call/task 和媒体提交的 durable 状态修复 Worker 崩溃后长期停留在 `image_running` 的 item；正常租约未过期时不得抢占。
- [x] 旧 generation requirement 只允许读取历史，不允许生成、审核或修改；切代后未提交的旧 work item进入 discarded/cancelled 终态。

#### 必须新增测试

- [x] 换代后调用旧 requirement ID 返回 `PRODUCTION_GENERATION_MISMATCH`。
- [x] 该场景 Provider Gateway 调用次数为 0。
- [x] 运行期间 generation 切换后，迟到结果被丢弃且不修改旧/新代业务状态。
- [x] API 创建任务后、Worker 领取后、Gateway 成功后和媒体提交前分别模拟进程崩溃，任务均可恢复到唯一终态。
- [x] 单项和批量路径产生相同的状态机、事件、任务活动投影和错误码。
- [x] 同一 idempotency key 重复提交只产生一个 batch；同一 item request hash 只产生一次计费调用和一个媒体提交。

#### 完成证据（2026-07-21）

- `000043_derived_asset_execution_v2.sql` 建立领域专用 batch、完整 request workset 和不可变 execution attempt，并用数据库约束维护身份、lineage、状态聚合、失败结果和终态不可变性。
- 单项生成、批量生成、重新生成、生产动作和 Project Agent 均进入 `createDerivedAssetBatchRun`；旧 Workflow/Activity 只保留注册以排空历史执行，不再接收公开新流量。
- Worker 在 Gateway 前验证冻结 generation/binding/requirement/shot/asset/model/capability/request 身份；有效租约不可抢占，迟到结果只写诊断，旧代业务行不被回写。
- 周期 Reconciler 覆盖命令已提交、Worker 已领取、Gateway 已成功、媒体已校验四个崩溃点；已有 provider result 会在新租约下恢复到 transferring 后幂等提交。
- 隔离 PostgreSQL Up/Down/Up、失败同步 SQLSTATE 23514 回归、换代前零调用、调用中换代、租约 CAS、迟到结果、幂等提交和四阶段故障注入全部通过。

### B13：显式批次必须逐项返回结果

#### 验证证据

- 预检只有在 `candidateCount > 0 && approvedCandidateCount == 0` 时整体拒绝。
- 混合已审核和未审核 ID 时预检通过。
- 实际查询只选择 `review_status='approved'`，未审核 ID 被静默过滤，最终 total 也只统计实际选择项。

#### 修复清单

- [x] 对显式 `requirementIds` 在 API command 事务内构造并持久化完整 request items，保留输入序号、去重关系和调用者可见的原始 workset。
- [x] request item 先分类为 executable、review_required、not_found、generation_mismatch、already_running、duplicate 或 skipped；只有 executable 项创建 execution attempt，但所有 request item 都进入最终响应和任务活动投影。
- [x] 不可执行项保存稳定 disposition/error code，不得从 total 中消失；跨租户 not-found 与真实不存在使用相同外部结果。
- [x] 可执行项继续运行，batch 按全部 request item 聚合为 succeeded/partial_succeeded/failed；`total = succeeded + failed + cancelled + skipped/blocked` 必须由数据库约束或测试保证。
- [x] “生成全部可用项”模式允许预检过滤，但必须持久化选择条件、candidate count 和各类 skipped count，保证重放和审计可解释。
- [x] “重试失败项”创建新 batch 并引用原 request item/attempt，只选择 retryable failure；原 batch 保持不可变。

#### 必须新增测试

- [x] 两个显式 ID，一项 approved、一项 pending，最终 total=2 且 partial_succeeded。
- [x] 不存在或属于旧 generation 的 ID 不泄露跨项目信息，并有稳定失败码。
- [x] 失败重试只重试失败 item。
- [x] 输入包含重复 ID、already_running 和不可重试失败时，总数与逐项 disposition 仍保持一致。

#### 完成证据（2026-07-21）

- 显式模式按原输入序号持久化 malformed、重复、跨项目、旧 generation、未审核和可执行项；跨项目 ID 与随机不存在 ID 对外均为 `DERIVED_ASSET_REQUIREMENT_NOT_FOUND`。
- `select_all` 持久化标准化 filters、filters hash、候选总数和不可执行计数；任务活动抽屉展示每个 request item 的 disposition、错误、attempt 和输出。
- mixed approved/pending 聚合为 `partial_succeeded`；duplicate、not_found、skipped 和 already_running 均计入 total，不再静默消失。
- retry 只读取 failed_retryable 或 retryable blocked 项，创建新 batch/request/attempt lineage；原 request item 与终态错误不可修改。

## 7. P1 认证生命周期修复

### B07：成员授权版本必须进入 access token

#### 验证证据

- 移除成员会删除角色绑定、撤销组织 session，并把 membership 置为 removed。
- access token 校验不读取 session，只校验用户全局 `credential_version` 和当前 membership 是否 active。
- 重新接受邀请会复用原 membership 行并置回 active，但不改变任何 token 中可验证的成员版本。
- 因此移除前尚未过期的 access token 在 membership 恢复后再次通过校验。

#### 修复清单

- [x] 使用新的前向迁移为 `organization_members` 增加单调递增的 `authorization_version`。
- [x] access token claims 增加 membership authorization version；签发和 `ValidatePrincipalActive` 同时校验 organization、membership status 和 version。
- [x] `auth_sessions` 保存目标 membership authorization version；refresh 必须校验当前 membership 仍 active 且版本完全一致，组织外 session 可使用明确的 nullable 语义。
- [x] 组织选择 token/nonce 保存候选组织及各自 authorization version 快照；选择组织时重新比较当前版本，不能只相信旧 nonce 中的 organization ID。
- [x] `switch-organization` 验证目标 membership 当前版本，并让新 session/token 绑定该版本。
- [x] disable、remove、reactivate、重新邀请等生命周期切换在同一事务中递增 version，并撤销该组织 session 和用户尚未消费的组织选择 nonce；事件/outbox 与状态修改一起提交。
- [x] 旧 claim、旧 refresh session 和没有版本快照的旧组织选择 token 不做兼容读取；部署后统一要求重新登录。

#### 必须新增测试

- [x] token T 在 remove 后失败，重新邀请后仍失败。
- [x] 重新登录签发的 token T2 成功。
- [x] 多组织用户只使目标组织 membership version 失效，不误伤其他组织 token。
- [x] refresh token 和 access token 使用相同成员版本边界。
- [x] remove 前签发的组织选择 token/nonce 在重新邀请后仍不可消费。
- [x] remove 前的 refresh session 不能在重新邀请后换取新 access token。

#### 完成证据（2026-07-21）

- 新增前向迁移 `000039_membership_authorization_version.sql`，旧组织 session 与 selection nonce 在升级时失效。
- access、refresh、select organization、switch organization 和成员生命周期均校验同一授权代次。
- `go test ./internal/auth ./internal/api ./internal/dbmigrate -count=1` 与 migration validate 通过。
- 隔离 PostgreSQL 中三个认证集成流程通过；migration 38→39 旧数据升级及 39 Down/Up 重放通过，未连接主数据库。

## 8. P2 Provider 与迁移安全修复

### B10：禁止 `000037` Down 静默替换模型身份

#### 验证证据

- 删除 M1 后若同 account/model_key 已创建 M2，Down 无法按原 UUID 恢复 M1。
- restore map 按 account/model_key 连接当前模型，随后把历史 Render Plan 的 `provider_model_id` 更新为 M2。
- SQL 行为确定成立，但注释和 migration runner 已表明生产环境禁用回滚，因此优先级从 P1 调整为 P2。

#### 修复清单

- [x] 不修改已形成历史的 `000036` 和 `000037`。
- [x] 将“跨越 37 的数据回滚不保证可逆”固化为 migration runner 策略，而不是继续扩展复杂恢复 SQL：只要存在 Provider 模型删除 tombstone、NULL 历史引用或同 key 不同 UUID，Down preflight 必须在开启 migration 事务前拒绝。
- [x] 任何环境都禁止按 account/model key 把历史 provenance 重新绑定到另一个 UUID；没有原 UUID 的精确恢复就视为不可逆。
- [x] 开发环境遇到该边界使用新数据库重建；生产模式继续拒绝所有 Down。文档和命令输出必须给出明确修复建议。
- [x] runner 的 preflight 结果结构化记录 migration range、阻断原因和数量，但不得输出凭据或敏感 provider metadata。

#### 必须新增测试

- [x] 删除 M1、创建同 key M2 后 down-to-36 必须在执行前失败，历史计划不得改绑 M2。
- [x] 无 tombstone、无 NULL 引用时，开发数据库仍可正常完成 Down/Up roundtrip。
- [x] 数据库停在 version 36 且已有 NULL provider model 引用时，preflight 在执行 `SET NOT NULL` 前失败，schema version 不变化。
- [x] 生产模式继续拒绝所有 Down。

#### 完成证据（2026-07-21）

- `Runner.Run` 在任何 Down SQL 前执行 Provider rollback preflight；跨越 37 检查 tombstone、同 key 不同 UUID 和 NULL Render Plan，跨越 36 单独检查 NULL Render Plan。
- 拒绝错误实现 `errors.Is/As` 稳定契约，日志只记录 migration range 与分类计数，并明确建议开发数据库重建或前向 migration。
- 未修改 `000036_provider_model_hard_delete.sql` 或 `000037_provider_model_deletion_rollback.sql`。
- `go test ./internal/dbmigrate -count=1` 通过。
- 独立 PostgreSQL 验证同 key 替代模型和 version 36 NULL 引用均在 schema version 变化前失败；清理风险数据后完整 Up/Down/Up 通过，测试容器已删除，主数据库未连接。

### B14：视频模型测试必须在服务层完成租户约束

#### 验证证据

- `video_generation_test` 使用 `videoproduction.LoadActiveContext(ctx, db, projectID)`，只按 project ID 查询。
- 构造 Gateway 请求时使用调用者 organization ID 和目标项目的 generation/binding。
- Gateway 会检测组织不匹配并阻止上游调用，因此没有确认跨租户计费或媒体写入。
- 但不存在项目、存在但无活动生产代、存在于其他组织等路径会产生不同错误，形成项目状态探测。

#### 修复清单

- [x] 增加按 `project_id + organization_id` 加载活动生产上下文的方法。
- [x] Provider service 在构造 Gateway 请求前完成租户断言。
- [x] 跨组织和不存在项目统一返回 not found，不返回 generation 细节。
- [x] Gateway 保留现有纵深身份校验。

#### 必须新增测试

- [x] 组织 A 使用组织 B 的 project ID，返回统一 404/NOT_FOUND。
- [x] Gateway mock 收到的请求数为 0。
- [x] 同组织合法测试行为不回归。

#### 完成证据（2026-07-21）

- `videoproduction.LoadActiveContextForOrganization` 在单次查询中约束 organization 与 project。
- `video_generation_test` 在构造 Gateway 请求前使用租户约束加载器，跨组织与不存在项目均返回 `pgx.ErrNoRows`，由 API 统一映射为 `NOT_FOUND`。
- Provider/videoproduction/API 定向测试通过；隔离 PostgreSQL 的跨组织集成测试确认 Gateway HTTP 调用数为 0。

## 9. P2 Web 修复

### B11：React Query key 必须包含资产查询形状

#### 验证证据

- 资产页请求 `includePreviewUrl=true`，项目设置请求默认不含预览 URL。
- 两者都使用 `qk.assets(projectId)`。
- Query Client 默认 `staleTime=30_000`，因此访问项目设置后立即进入资产页会复用无预览数据。

#### 修复清单

- [x] 先修复当前错误：由统一 query-key factory 规范化 status/filter/include/sort/page 等参数，preview 模式必须进入 key；对象属性顺序不能产生不同 key。
- [x] 提供稳定的 assets root/list/detail/media-preview 层级，用 prefix invalidation 精确失效所有受影响形状，禁止页面手写数组 key。
- [x] 长期将稳定资产数据与短期 signed preview URL 分离：资产 query 只缓存 media identity/version，`useMediaPreview` 按 media ID、variant 和 content version 获取 URL，并依据 `expiresAt` 在过期前刷新。
- [x] 图片加载失败只使 preview query 失效并重签，不清空资产主查询；关闭详情弹窗不得改变列表缓存中的 preview identity。
- [x] 对全仓 query key 做一次静态审计，修复其它同 key 不同 queryFn、遗漏 filter 和 mutation 只失效单一 shape 的路径。

#### 必须新增测试

- [x] 先访问设置再访问资产页，资产页仍发起带预览请求。
- [x] 反向访问不污染无预览消费者。
- [x] 资产生成/上传/设为主图后两种缓存都失效。
- [x] signed URL 过期或返回 403 后只重签一次，资产卡不会闪烁消失或形成刷新循环。
- [x] 打开/关闭资产详情和大图预览不会改变列表缩略图可见性。

#### 完成证据（2026-07-21）

- `query/keys.ts` 将资产 root、稳定列表、详情、引用和 signed preview 投影拆成规范化 primitive tuple；所有 mutation、实时事件和任务活动统一按 root prefix 失效。
- 资产页分别缓存稳定 metadata 与短时预览投影，图片加载失败只重签 preview query；API/OpenAPI 增加 `previewExpiresSeconds` 与 `previewExpiresAt`，不会再由弹窗生命周期改写列表缓存。
- 新增 query-key 顺序隔离和失效契约测试；Web test/typecheck/lint/build、API 定向测试、OpenAPI YAML 与路由检查通过。主环境浏览器 smoke 等待部署窗口，不计入本项自动化完成证据。

### B12：权限状态改为 fail-closed

#### 验证证据

- `sessionFromAuthResponse` 初始不写 permissions。
- `/me` 非 401 失败时 AuthGuard 会结束 checking 并继续渲染页面。
- `sessionHasPermission` 在 permissions 缺失时返回 true。
- Provider 能力验证、批准、拒绝、撤销等按钮没有完整的 `provider.manage` UI 守卫。
- 后端仍执行 RBAC，所以当前问题是错误暴露操作入口，不是后端权限绕过。

#### 修复清单

- [x] 持久化 session 只保存 token、当前 organization/workspace/project 等会话定位信息，不把 permissions 或 membership 当作 localStorage 权威数据。
- [x] 建立独立的认证 bootstrap query/state，通过 `/me` 加载用户、membership 和 PermissionSet，状态明确为 loading/ready/error。
- [x] AuthGuard 在 bootstrap 进入 ready 前不渲染业务路由；401 清理 session，其它错误进入可重试错误页，不能继续渲染默认全权限页面。
- [x] access token refresh 成功后使旧 bootstrap 失效并重新加载；组织切换也必须完成新组织 bootstrap 后才开放业务 UI。
- [x] `sessionHasPermission` 改为只接受已加载 PermissionSet，undefined/unavailable 一律 deny；所有 Provider 写操作统一由 `provider.manage` 控制。
- [x] 服务端 RBAC 保持最终权威，并对全站 permission-gated route、menu、button 和 mutation 做一次入口审计。

#### 必须新增测试

- [x] permissions 未加载时不显示写按钮。
- [x] `/me` 网络失败时不进入默认全权限页面。
- [x] provider.read 用户可查看但不能看到验证、审批、编辑或删除入口。
- [x] token refresh 或组织切换期间不会短暂显示上一身份的写入口。
- [x] 清空/篡改 localStorage permissions 不能提升 UI 权限或影响服务端授权。

#### 完成证据（2026-07-21）

- `apps/web/src/lib/session.ts` 与 `session-policy.ts` 已把授权快照从持久化会话中移除，建立按 access token + organization 绑定的 loading/ready/error bootstrap；身份变化立即使旧快照失效。
- `AuthGuard` 只在 `/me` 成功后渲染业务页面；非 401 失败进入中文可重试错误页，401 刷新 token 后必须重新 bootstrap。
- `useApiQuery` 只在 authorization ready 后启用；`useApiMutation` 支持 `requiredPermission` 前置校验，Provider 页 16 个 mutation 全部要求 `provider.manage`，并隐藏只读用户的新增、发现、测试、审批、编辑、删除和绑定写入口。
- 新增 5 个 Node 原生策略测试，覆盖权限未加载、provider.read 只读、localStorage 伪造、身份切换和错误态 fail-closed；`pnpm --filter @cineweave/web test/typecheck/lint/build` 全部通过，lint 仅保留 3 条既有 `<img>` 警告。
- 在临时 `29285` Web 实例制造 `/me` 网络失败，页面只显示“账号权限加载失败”及重试/重新登录，不显示业务内容；临时实例已关闭，主环境未重建。

### B15：保存回调不得覆盖请求期间的新 draft

#### 验证证据

- `saveBasicMutation.onSuccess` 无条件从当前 draft 删除 name/description。
- 用户提交 A 后、响应前编辑 B，成功回调会清除 B 并回退到服务端返回的 A。

#### 修复清单

- [x] 把表单状态改为明确的 `baseSnapshot + draft + dirtyFields + inFlightSubmission`，不要在 mutation callback 中临时猜测哪些字段属于哪次请求。
- [x] 每次提交固化 client mutation ID、base revision 和 submitted values；成功后只清除“当前值仍等于本次 submitted value”的 dirty 字段，请求期间的新输入继续保留。
- [x] API 对可编辑项目设置使用 revision/`If-Match` CAS；服务端响应先合并到 base/query cache，再把仍然 dirty 的本地字段覆盖到视图。409 返回最新 snapshot 和稳定冲突码，不清空 draft。
- [x] 同一设置资源的保存请求串行化或取消尚未发出的旧请求；不得允许两个响应乱序覆盖。生产配置字段继续走影响分析/换代，不与基本信息 PATCH 混合。
- [x] 保存期间按钮显示运行状态，离开页面前对未保存 draft 提示；输入框是否禁用可由产品决定，但不能静默丢值。

#### 必须新增测试

- [x] 保存 A 期间输入 B，A 成功后输入框仍为 B 且保持 dirty。
- [x] 未继续编辑时成功保存会正确清除 dirty 状态。
- [x] 409 revision conflict 不清空 draft。
- [x] 连续提交 A/B 且响应乱序时，最终 base/draft 仍对应最高已确认 revision 和用户最新输入。
- [x] 基本信息保存与生产配置换代并发时互不覆盖，生产配置仍必须经过确认流程。

#### 完成证据（2026-07-21）

- `project-basic-form-state.ts` 实现显式 base/draft/dirty/in-flight 状态机，提交使用 client mutation ID 与 base revision，成功仅确认本次仍未被继续编辑的字段。
- API 的 `PROJECT_REVISION_CONFLICT` 返回当前基本信息 snapshot；前端保留 draft、串行化提交，并把生产配置换代状态与基本信息草稿分离。
- 六组竞态测试覆盖请求期间继续输入、普通成功、409、乱序响应、重复提交和生产配置并发；Web test/typecheck/lint/build 与 API revision 定向测试通过。主环境浏览器 smoke 等待部署窗口。

## 10. 实施顺序、依赖与发布批次

本计划不要求所有改动在一个分支或一个部署中同时上线。每个 Release Train 必须能独立构建、验证和回滚；跨 Train 的共享契约先 expand，消费者切流后才 contract。

### Gate 0：冻结基线与建立护栏

- [ ] 实施开始时重新读取最新 migration 号并在共享工作区声明占用；当前观察到的最新版本是 `000039`，但文档不永久预留 `000040`。
- [x] 先完成 B10 migration Down preflight，确保后续隔离 Up/Down/Up 不会触发已知 provenance 改绑。
- [ ] 保存至少一份正在使用的 v1 视频 workflow history/replay fixture，并记录当前 Activity/Workflow type 注册表。
- [ ] 为 execution identity 类型族、状态集合、错误码、request hash 和 event envelope 建立共享包及契约测试；领域表不迁入万能任务表。
- [ ] 建立只读盘点命令/查询：migration version、v1/v2 workflow 数、非终态 node/item/segment/provider task、stuck checkpoint、discarded write 和重复提交抑制数。
- [ ] 每个后续 migration 只做 expand；本阶段和各功能切流发布都不删除旧列、不停止 v1 注册。

完成门槛：基线可重复采集；v1 replay fixture 可运行；迁移冲突会在写数据库前失败；共享身份与错误契约有测试。

### Release A：已完成安全项与独立 Web 一致性

- [x] B01 系统管理员账号保护。
- [x] B07 access/refresh/selection 全链路 membership authorization version。
- [x] B12 Web 认证 bootstrap 和权限 fail-closed。
- [x] B14 Provider 模型测试租户约束。
- [x] B11 资产 query shape 与媒体预览缓存分离。
- [x] B15 项目设置 draft/revision 保存模型。

说明：B11/B15 与视频运行时无数据依赖，可和后续 Release 并行开发，但必须单独做浏览器回归。B01/B07/B12/B14 当前仅确认实现与自动化验证，部署和主环境 smoke 仍需窗口。

完成门槛：安全边界通过隔离数据库/API 测试；前端权限和草稿 fail-closed；资产预览不依赖访问顺序；主环境部署状态另行记录。

### Release B：视频执行内核 v2

- [x] B02 PromptOnly 与 executable Render Plan 分离，并原子物化精确计划。
- [x] B03 episode item attempt 与 Render Plan 原子绑定，segment 成为 provider task 事实来源。
- [x] B09 checkpoint 显式 reconcile 与纯终态投影。
- [x] 新任务通过 workflow type/version routing 进入 v2；v1 继续注册且不得调用 v2 必填 Activity。
- [x] 数据库 expand migration 增加必要的 execution identity、唯一索引、diagnostic 字段；旧列继续双读但停止作为新任务的事实来源。

这三项必须同批切流：只上线 B02 而未修 B03 会保留错误 item provenance，只上线 B09 而没有精确 identity 会把错误计划聚合成“正确终态”。

完成门槛：任何视频 provider task 都可追溯到唯一 item attempt、workflow、generation、binding、plan、segment 和 shot；并发 workflow 不互借计划；失败终态不会变成功；错误身份上游请求数为 0。

### Release C：视频输入契约能力

- [x] B08 `first_frame_plus_references` 使用共享 contract registry 与 `ReferenceManifestV2` 完整闭环。
- [x] 能力快照、Planner、Workflow、Gateway、OpenAPI 和 Web 显示使用同一契约语义。
- [x] 仅在 Release B 的精确 plan/segment identity 已切流并稳定后启用该模式。

完成门槛：多 segment 续接、参考图顺序和 request hash 可重复；能力或参考不匹配在上游前失败；现有 `first_frame` 行为不回归。

### Release D：剧本不可变组装

- [x] B04 子 Agent 结果写 staging，finalizer 统一组装和条件激活。
- [x] B05 以稳定 source chapter ID 为小说身份，显示序号只由 manifest 派生。
- [x] expand migration 增加 generation manifest、staging 和必要索引；只有确有多章节合并需求时才新增 source binding 表，旧直接写路径停止新流量。

B04/B05 必须一起实施。只增加 staging 而继续按 episode index 匹配，仍会在章节删除/插入后生成错集。

完成门槛：部分失败不删除已有内容；新增集失败不激活不完整版本；章节增删/重排不触发错集或唯一约束冲突；重试只处理失败 source unit。

### Release E：衍生资产耐久执行

- [x] B06 单项/批量统一进入 derived-asset Workflow，使用 generation fence、item lease 和幂等媒体提交。
- [x] B13 完整持久化 request workset、逐项 disposition、部分完成与失败重试。
- [x] 单项同步 Gateway 路径停止接收新流量，但保留到旧请求排空。

B06/B13 必须共享同一个 batch/item 状态机和 UI 投影，不能再维护“单项同步、批量异步”两套语义。

完成门槛：HTTP API 不承载供应商生命周期；换代后旧任务不产生新成本或回写；故障注入后恢复到唯一终态；batch 总数严格等于持久化 request workset。

### Release F：旧契约排空与收缩

- [ ] 连续两个观察窗口确认 v1 workflow type、旧 Activity type、旧 provider task 和旧业务读路径均为 0；观察窗口长度必须大于最长允许 workflow 运行时间与最大重试窗口。
- [ ] 对无法自然结束的旧任务执行显式取消/归档并保留审计，不直接改库伪造终态。
- [ ] 先停止新任务写旧列，再以读指标证明旧列无人使用；随后在独立发布停止 v1 注册。
- [ ] 最后一个 contract migration 才移除 item 单一 `provider_async_task_id`、旧索引/字段或收紧 `NOT NULL`。
- [ ] contract 发布前运行数据一致性审计和隔离数据库 Up/Down/Up；发布后仍保留应用版本回滚能力，但不承诺回滚已删除的数据结构。

完成门槛：主环境不存在需要 v1 worker 的可执行 history，所有新路径只依赖 v2 durable identity，收缩发布可独立停止且不影响前一应用版本。

### 10.1 并行开发规则

- Release A 可与 B/D/E 并行；B 是 C 的硬依赖。
- D 与 E 可并行，但 migration 编号、OpenAPI、事件 catalog 和共享 API/Web 文件必须由单一合并者协调。
- 每个 Release 使用独立 feature flag 或 workflow routing version；不要用“部署了新代码”代替显式切流。
- 任一 Release 未达到自动化门槛时，不阻塞其它独立 Release 合并，但不得进入其部署批次。

## 11. 接口与数据结构影响

### 11.1 公共运行时契约

- 新增版本化 execution identity 类型族及统一基础校验器；各领域使用自己的强类型扩展，不在核心代码中传递松散 UUID 集合。
- 新增/扩展领域 operation/item 表时使用关系字段承载 organization/project/generation/binding/workflow/attempt 身份，metadata 只存扩展诊断，不存决定执行目标的唯一值。
- 统一错误 envelope：`code`、`messageKey`、`retryable`、`stage`、`operationId`、`itemId`、`diagnosticId`；OpenAPI 暴露稳定字段，原始链路错误不直接作为用户文案。
- 统一 outbox event envelope 和状态转换函数，所有 terminal 事件与业务状态在同一数据库事务提交。
- migration 文件编号在实际创建时从最新磁盘状态分配；不在设计文档预留固定编号。

### 11.2 认证

- `organization_members.authorization_version`：单调递增授权代次。
- `auth_sessions`：保存预期 organization membership authorization version。
- 组织选择 token/nonce：保存候选组织的 authorization version 快照，不再只保存 organization ID。
- access token claims：携带 membership authorization version；旧 token/session/nonce 部署后失效并要求重新登录。
- Web session：不再持久化权限作为权威；新增独立 `/me` bootstrap 状态和 PermissionSet。

### 11.3 剧本生成

- 新增 generation manifest，固化 source revision/hash、source chapter IDs/order、base script/version、目标集合和 prompt/model/manual snapshots。
- 新增 `script_episode_generation_results`（最终名称由 migration 评审确定），至少包含执行身份、source chapter、status、content/content hash、错误和 provider/prompt/model provenance。
- staging 唯一键使用 `(workflow_run_id, attempt_generation, source_chapter_id)`；正式 `script_episodes` 只由 finalizer 写入。
- `script_episodes.source_chapter_id`（或现有等价 provenance）保存稳定章节身份；`episode_index` 仅为版本内显示顺序。未来多章节合并通过显式关联表扩展。
- script version 增加 source manifest hash、base version、generation result summary 和 activation CAS provenance。

### 11.4 视频生产

- 新增 v2 `LoadApprovedShotVideoPromptPlanV2`、`MaterializeAndBindExecutableShotVideoPlanV2` 和 `LoadExecutableShotVideoPlanV2`，后两者强制完整 execution identity 与 exact plan ID。
- episode item 被定义为不可变 attempt；非空 execution plan FK 唯一，计划和 item 在同一事务建立关系。
- Provider Gateway 视频执行校验增加 item/plan/workflow/generation/binding/segment identity，并在 credential、lease 和上游请求前完成。
- provider async task 归属以 render segment 为准，segment submit 使用 request hash 幂等。
- 旧 item `provider_async_task_id` 先停止作为事实来源，待 Release F contract migration 移除。
- terminal reconcile result 与只读 output 分离；输出包含 reconciliation/diagnostic 字段，使 UI 能区分业务失败、状态修复和 discarded write。
- `ReferenceManifestV2` 固化参考图 role、顺序、来源版本和 content hash。

### 11.5 衍生资产与 Web

- 单项/批量衍生资产生成 API 统一返回 `202 Accepted`、workflow run/operation ID 和完整 workset summary，不再同步等待图片供应商。
- derived-asset batch/request item/execution attempt 分别表达原始请求、预检 disposition 和实际执行；所有显式输入都能出现在最终结果中。
- `ShotAssetRequirement` 与 execution item 携带 production generation、binding revision、attempt、execution token 和 prompt/reference/model snapshots。
- 批量输出包含所有显式请求项、稳定 disposition/error code 和 retryable 标记；重试创建新 batch/attempt。
- React Query assets key 包含规范化 preview/status/filter/sort/page 形状，同时保留统一 root prefix 失效能力。
- 媒体预览使用独立 query/hook，以 media identity/version 和 URL expiry 管理短期 signed URL。
- 项目设置 API 使用 revision CAS；Web draft 使用 base/draft/submission 三层状态。
- 本计划不新增系统管理员密码恢复公开接口；该能力进入独立安全设计评审。

## 12. 测试与发布验收

### 12.1 分层自动化测试

- [ ] 为每个 B 编号先添加能够在旧实现失败的反例，再实现修复。
- [ ] 共享状态机、错误分类、identity validation 和 request hash 使用 table-driven/property tests，覆盖非法转换、零值身份、字段顺序和重复重放。
- [ ] 所有关键唯一键、外键和 CAS 在隔离 PostgreSQL 中验证；只使用 mock repository 的测试不能作为数据一致性完成证据。
- [ ] 认证测试覆盖 access、refresh、organization selection、switch organization、remove/reinvite 的完整版本矩阵。
- [ ] 视频测试覆盖 PromptOnly、原子 plan/item 绑定、并发激活、multi-segment task、terminal reconcile 和上游零调用断言。
- [ ] 使用保存的 v1 histories 或 Temporal replay fixture 验证 v1 worker；新 v2 workflow 做 determinism/Continue-As-New 测试。
- [ ] 剧本测试覆盖 staging 幂等、并发子结果、章节增删重排、部分失败 fallback、无 fallback 不激活和 finalizer 重试。
- [ ] 衍生资产使用 fault injection 覆盖 API commit、Worker claim、Gateway 返回、媒体转存和 final commit 各崩溃点。
- [ ] Web 测试覆盖 bootstrap loading/error、token refresh、组织切换、React Query shape 和 mutation 请求竞态。
- [ ] 新迁移在隔离 PostgreSQL 执行 Up/Down/Up；任何 migration runner preflight 必须在修改数据前失败。
- [ ] 并发测试至少使用两个独立数据库连接/事务和可控 barrier，不能用串行调用声称覆盖竞态。
- [ ] Provider 零调用断言同时检查 HTTP server 计数、`provider_requests`、`provider_async_tasks`、`provider_call_logs` 和 `cost_records` 均未新增。

每个 Release Train 完成时先运行定向测试，再运行根测试入口；以下命令全部通过才可进入部署候选：

```powershell
pnpm run test
docker compose -f compose.yml config --quiet
```

`pnpm run test` 已覆盖 Go、Web test/typecheck/lint、OpenAPI YAML 和 route consistency；单独命令只用于定位失败，不作为替代。

### 12.2 部署顺序

1. 在只读模式记录 migration version、运行中 workflow/type、node/item/segment/provider task、失败 checkpoint 和关键指标基线。
2. 在明确窗口备份数据库并先应用 expand migration；迁移后先运行 schema/invariant probes，不立即切新流量。
3. 先部署同时注册 v1/v2 的 Worker，确认 task queue poller 正常；再部署带 feature flag/version routing 的 API/Agent；最后部署 Web。
4. 先对测试项目或受控组织开启 v2，运行真实但限额的 canary；确认 identity、计费、媒体和终态后逐步扩大。
5. 观察 nondeterminism、Activity not registered、重复 provider call、write-fence discard、stuck item、reconcile 次数和错误率；超过阈值立即停止新任务切流。
6. 回滚优先关闭 routing flag 并回退应用镜像；expand schema 保留。不要通过 migration Down 恢复业务流量。
7. v1 清零前不停止旧 worker 注册；Release F 必须作为独立发布执行。

获得部署窗口后再执行：

```powershell
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

未经部署窗口，不执行上述命令、不应用主数据库 migration。局部前端验证可使用临时非主端口，但必须在结束后关闭。

### 12.3 运行态 smoke

- [ ] 系统管理员账号无法由组织 owner 修改或重置，响应不泄露管理员身份。
- [ ] 成员移除、重新邀请后旧 access/refresh/selection 材料仍为 401/403。
- [ ] 两个并发视频 workflow 不会互相执行 Render Plan；身份错误时 Gateway 上游请求为 0。
- [ ] multi-segment 视频可以从 item 查询和取消全部 provider task。
- [ ] 分集视频失败恢复后仍显示失败/部分完成，可创建新 checkpoint 重试且不循环报错。
- [ ] Worker 在 plan 物化提交前/后崩溃，两种情况都不会留下 plan/item 半绑定或重复上游请求。
- [ ] 剧本两集重生成一成一败时旧集不丢失；新增集失败时不激活不完整版本。
- [ ] 删除小说前置章节后，剩余剧本分集身份和顺序正确。
- [ ] 换代后旧衍生资产需求不产生 Provider call；单项与批量任务状态一致。
- [ ] 显式资产批次的成功、失败、跳过数量之和等于请求去重后的 workset。
- [ ] 资产页无论访问顺序都显示预览图，只读用户看不到供应商管理操作。
- [ ] 项目设置保存期间继续输入不会丢失。
- [ ] v1 运行任务和 v2 新任务在同一部署中均能到达可解释终态。
- [ ] dashboard/诊断查询可以从 operation ID 定位到 workflow、item、plan、segment、provider task、cost 和 media，且不存在孤儿关系。

### 12.4 发布停止条件

出现以下任一情况时停止扩大流量，保留 expand schema 并回退 routing/application：

- Temporal nondeterminism、Activity 未注册或 v1 replay 失败。
- 同一 request hash 出现重复 provider task/cost record。
- plan/item/segment identity 不一致或跨 generation write 未被 fence 拒绝。
- failed/cancelled checkpoint 被投影为 succeeded，或 terminal task 再次进入 running。
- stuck task、reconcile、discarded write 或错误率显著高于部署前基线。
- Web 出现权限 fail-open、草稿丢失、预览刷新循环或 API contract drift。

## 13. 完成规则

1. 每个编号分别记录实现、自动化验证、部署和运行态验收；不得仅因代码合并就写“线上已完成”。
2. 只有对应反例、正常路径、并发/故障测试和必要契约检查全部通过后，才能勾选实现/测试条目。
3. 不使用查询“最新记录”掩盖错误 durable FK；执行前必须持有精确且不可变的 execution identity。
4. 创建 Render Plan 与绑定执行 item 必须原子提交；不接受靠后续 reconcile 修补正常事务边界。
5. 剧本子 Agent 不直接写最终 script version；供应商长调用不运行在同步 HTTP handler 生命周期中。
6. 不通过前端隐藏替代后端租户、generation、authorization version、revision 和权限校验。
7. 不通过业务聚合错误触发 terminal Activity 无限重试；reconcile 是显式幂等命令，loader 保持只读。
8. 不原地破坏正在运行的 Temporal Activity/Workflow 契约；v1 未排空前不得删除注册或必填化旧输入。
9. 不改写 `000036/000037`；新问题使用前向 migration 和 runner preflight，migration 编号以实施时最新磁盘状态为准。
10. 不在同一发布中同时上线新契约和删除旧契约；expand、切流、观察、排空、contract 分阶段执行。
11. 新抽象必须至少被两个真实调用点采用并减少重复状态逻辑；否则保留领域内实现，避免过度框架化。
12. 每个 Release Train 完成后更新本文件的四层状态，并在 `docs/codex-execution-plan.md` 同步当前阶段、部署状态和下一步任务。
