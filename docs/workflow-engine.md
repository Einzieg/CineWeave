# CineWeave Workflow Engine

CineWeave 使用 Temporal 承载长时间内容生产。API 只在数据库事务中创建 `workflow_runs` 和 `workflow_start_outbox`；Start Dispatcher 成功提交 Temporal 后才进入实际执行。`workflow.start` 成功只表示子 Workflow 已启动，不表示业务产物已完成。

## 运行边界

- API Server、Worker 不得直接调用上游供应商或解密凭据；所有 AI 调用经 Provider Gateway。
- Media Worker 只负责对象存储、FFmpeg、媒体探测、分镜板裁板和最终合成，不调用 Provider Gateway。
- 每个 Activity 必须具备幂等键、超时、重试策略和持久状态；浏览器不是权威进度存储。
- Workflow、Node、Provider async task 和镜头产物必须携带 `productionGenerationId`。业务提交同时校验 active binding revision、active generation 和 workflow/node writable fence。
- 旧 generation 的延迟 Activity、Provider 回调或媒体转存只能写审计终态，并返回 `WORKFLOW_RESULT_DISCARDED`，不得覆盖当前代。
- 取消必须从父 Workflow 传播到活动 Node 和可取消的 Provider async task；`cancelling` 由 reconciler 收敛，禁止直接改库伪造终态。

## 视频生产领域模型

项目创建时原子绑定一个不可变 `VideoProductionProfileVersion`，并创建第一个 `ProductionGeneration`。当前内置 Profile 为：

- `single_frame_i2v`：当前镜头已审核首帧是唯一硬视觉输入。
- `first_last_frame`：同一镜头使用已审核计划首帧和计划尾帧。
- `multimodal_reference`：使用带媒体类型、语义和角色的多模态引用包。
- `storyboard_sheet`：使用无文字分镜板及 `PanelManifest` 作为语义参考。

只有 `published + available` 的 version 可创建项目、重建或启动生产。Profile version 发布后不可原地修改；切换方案必须执行影响检查和多分集受控重建，旧 binding/generation 进入历史，新 generation 从剧本分集重新生成适配分镜。Canonical assets 保留。

每个 Profile 通过统一 Compiler 插件点提供：

- `AnchorStrategy`
- `ReferenceStrategy`
- `PromptContractStrategy`
- `InputContractAdapter`
- `Reviewer`

新增 Profile 不得复制整套 Workflow，也不得静默降级到其他方案。

## 分层编排

视频长任务采用以下层次：

```text
ProjectVideoProductionRebuildWorkflow
  -> ScriptEpisodeToStoryboardWorkflow

EpisodeBatchGenerateShotVideosWorkflow
  -> EpisodeVideoProductionWorkflow
     -> SceneOrShotBatchWorkflow
        -> 可重试镜头 Activity / 必要时有界 Shot Workflow
```

- Project Rebuild 只编排分集 rebuild items、归档、binding/generation 切换和失败分集重试，不承载所有镜头历史。
- Episode 是长期业务单元；按场景边界、reset boundary 和真实依赖图切分批次。
- Batch 是有界并发与部分失败单元。每个镜头完成后立即写库，不等待整集结束。
- 长分集通过持久 checkpoint 和 Continue-As-New 控制 Event History；Continue-As-New 前停止调度新 Child 并等待当前 Child drain。
- 10 集批任务限制 Episode Child 并发；Provider Gateway 的 lease/quota/circuit breaker 仍是最终上游并发门。

## Prompt 与视频执行分离

“批量生成视频提示词”和“批量生成视频”是两个独立状态机：

1. Prompt 阶段从整集权威剧本、当前场景、相邻摘要、镜头状态、转场、手册、引用和模型限制确定性编译 `PromptContextPlan`。
2. Prompt Agent 与 Reviewer 使用同一个 context hash，输出不可变的 approved `VideoPromptPlan`；当前镜头中文台词逐字保存，图片 Prompt 禁止台词。
3. 视频阶段只加载 approved Prompt Plan、capability attestation、Render Plan 和 canonical Input Contract，不得现场重跑 Prompt Agent。
4. Profile、generation、ShotState、ReferencePack、PromptContextPlan、能力快照或 Prompt hash 变化时，旧 Render Plan 进入 stale，执行返回 `VIDEO_PROMPT_PLAN_STALE` 或 `RENDER_PLAN_REPLAN_REQUIRED`。

首段与同镜头续段分别冻结 Input Contract。不同 Storyboard Shot 之间不得传递硬尾帧；`observed_tail_frame` 只允许同镜头 Render Segment 续接。

## 状态、重试与可观测性

- `workflow_runs` 表示业务 Workflow；`workflow_node_runs` 表示可观测步骤；`provider_requests/provider_async_tasks` 表示上游逻辑请求和异步任务。
- 父任务聚合支持 `succeeded`、`partial_succeeded`、`failed`、`cancelled`；单项失败不回滚成功项。
- 重试只创建失败项的新 attempt，并沿用包含 generation、episode、shot、operation 和 revision 的业务幂等身份。
- Project Agent 在执行依赖步骤前必须等待子 Workflow 有效完成：run 已终态，且不存在 queued/running Node，也不存在 queued/running/cancelling Provider async task。
- Search Attributes 至少包含 `ProjectId`、`ProductionGenerationId`、`EpisodeId`、`ProfileVersionId` 和 `RebuildId`。
- 任务活动通过持久 API 与 Realtime 定向事件更新镜头状态，不通过整页轮询或浏览器内存推断进度。

## Worker 发布

Script、Media、Audio Workflow 使用 Temporal Pinned Worker Deployment。每次不兼容变更发布新 build/release ID，先执行当前版本和 N-1 replay，再逐步 ramp/promote；旧 Worker 只能在其 Pinned Workflow 和 Provider task 全部 drain、`safeToDecommission=true` 后停止。数据库只做前向迁移，回滚仅切换 Worker 路由。

部署、排空和故障处理命令见 `docs/runtime-foundation-hardening-runbook.md`；视频生产完整领域契约见 `docs/video-production-workflow-continuity-refactor-target.md`。
