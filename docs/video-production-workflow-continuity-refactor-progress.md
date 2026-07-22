# 视频生产方案与锚点式连续性重构执行进度

- 目标文档：`docs/video-production-workflow-continuity-refactor-target.md`
- 当前里程碑：P9 清理、压测和发布已完成
- 状态：已完成
- 最近更新：2026-07-17

状态说明：

- `[ ]` 未开始或未完成
- `[~]` 执行中
- `[x]` 已完成且已有验证证据

## P0 数据保护和错误止损

- [x] P0-1 冻结/快照 Provider 配置数据，并区分配置等值检查与历史主键子集检查。
- [x] P0-2 增加 migration hash 与 Provider 数据保护测试，禁止修改 `000001_current_schema.sql` 或覆盖供应商配置。
- [x] P0-3 禁止跨 Storyboard Shot 传递硬 `ContinuityFirstFrame`，仅允许同镜头 Render Segment 续接。
- [x] P0-4 图生视频优先使用当前镜头 fresh 图片，未分类转场默认 reset。
- [x] P0-5 单个旧连续组失败不再阻塞下一个独立锚点之后的镜头。
- [x] P0-6 排空/取消旧跨镜头任务并建立运行时发布门。

## P1 不可变项目方案和受控重建

- [x] P1-1 新增 `000009_video_production_profiles_and_generation_fence.sql`，建立 Profile family/version、Binding、Generation 和多分集 Rebuild schema；移除旧项目自动兼容 backfill。
- [x] P1-2 新增 `000010_video_production_prompt_contracts.sql`，建立 PromptContextPlan、对白/音频契约和 generation 关联。
- [x] P1-3 写入四个系统 Profile：图生视频 `published + available`，其余三个 `published + reserved`。
- [x] P1-4 项目创建原子写入 active binding/generation，reserved version 服务端拒绝运行。
- [x] P1-5 实现多分集 rebuild impact、审批、归档、generation 切换、部分完成和失败分集重试。
- [x] P1-6 所有镜头生产写入接入 generation CAS，晚到结果只能保留审计。
- [x] P1-7 删除直接更新 `productionMode` 的 API/Agent 工具，补齐 OpenAPI、RBAC 与事件契约。

## P2 图生视频分镜、锚点和 Prompt Contract

- [x] P2-1 实现 `single_frame_i2v` ShotState、Transition、typed ReferencePack 和 `planned_first_frame`。
- [x] P2-2 Shot Planner 生成符合单首帧模型构图与动作可达性的分镜。
- [x] P2-3 实现确定性 transition classifier、ShotState validator/hash 和 Reviewer。
- [x] P2-4 实现 PromptContextPlan 分层上下文与确定性预算。
- [x] P2-5 实现公共规则、手册、Profile 策略、镜头状态和模型限制的 Prompt Registry 契约。
- [x] P2-6 图片 Prompt 禁止台词；视频 Prompt 逐字保留中文台词并校验原生音频能力。
- [x] P2-7 保存 Profile、PromptContextPlan、能力、引用和审核完整 provenance。

## P3 Gateway 与长任务 Workflow

- [x] P3-1 完整实现 canonical `first_frame` initial Input Contract，并补齐同镜头 continuation contract。
- [x] P3-2 校验现有视频模型能力和 Adapter 映射，不修改供应商配置语义；补齐 capability attestation。
- [x] P3-3 Gateway planner/create 加入 generation、binding、PromptContextPlan、dialogue cues、原生音频和首段/续段契约。
- [x] P3-4 实现 Project Rebuild → Episode → Scene/Shot Batch → Activity/有界 Shot Workflow 分层编排。
- [x] P3-5 视频生成只执行已审核 Prompt，不隐式重跑 Prompt Agent。
- [x] P3-6 实现 checkpoint、Child drain、Continue-As-New、Pinned Worker、取消、幂等和失败项重试。
- [x] P3-7 验收 Provider 错误、媒体转存、generation fence、call log 和 cost record 全链路。

## P4 图生视频前端闭环

- [x] P4-1 新建项目用四张 Profile 卡替换旧四项，只有图生视频可选。
- [x] P4-2 分镜页展示首帧状态、转场、锚点、引用和审核。
- [x] P4-3 视频页展示 Render Plan、PromptContextPlan、Prompt、引用和历史版本。
- [x] P4-4 任务活动实时展示逐镜头状态，不整页重复刷新。
- [x] P4-5 单镜头/批量任务支持并发、取消、部分完成和只重试失败项。
- [x] P4-6 项目设置只读展示 binding/version/generation，并通过受控重建切换方案。
- [x] P4-7 供应商模型页展示 capability evidence，并支持 `inferred` snapshot 审批、撤销和失效提示。

## P5 共用扩展基础

- [x] P5-1 抽取统一 Profile Compiler、Reference Resolver 和 Workflow 骨架。
- [x] P5-2 建立可插拔 Prompt Contract、Anchor Strategy、Input Contract Adapter 和 Reviewer。
- [x] P5-3 用 contract tests 锁定图生视频行为。
- [x] P5-4 删除旧连续组、尾帧专用表和跨镜头硬尾帧 Workflow。

## P6 首尾帧衔接模式

- [x] P6-1 实现首尾帧规划、生成、审核和 Gateway 适配。
- [x] P6-2 验证人物身份、站位、动作可达性和模型时长限制。
- [x] P6-3 发布新的 `published + available` Profile version。

## P7 多模态参考模式

- [x] P7-1 实现多类型引用打包、裁剪、语义和 Provider Adapter。
- [x] P7-2 验证角色、场景、道具、图片、视频和音频引用不会串位。
- [x] P7-3 发布新的 `published + available` Profile version。

## P8 分镜板模式

- [x] P8-1 实现 GPT-image-2 分镜板、PanelManifest、审核和 Input Contract。
- [x] P8-2 验证分镜板无文字、顺序正确且不被误用为首帧。
- [x] P8-3 发布新的 `published + available` Profile version。

## P9 清理、压测和发布

- [x] P9-1 删除旧字段、Prompt、API、Agent 工具、兼容代码和测试夹具。
- [x] P9-2 确认无宫格图模式残留。
- [x] P9-3 全仓测试、Workflow replay、Provider contract tests 全部通过。
- [x] P9-4 使用 10 集和 70 分钟长分集样本压测所有 available Profile。
- [x] P9-5 Docker Compose 重建并执行 API、Provider、Worker、Web 浏览器 smoke。
- [x] P9-6 更新执行计划、运维文档和最终验证记录。

## 全量验收

- [x] V-1 `go test ./...`
- [x] V-2 Web typecheck 与 lint。
- [x] V-3 OpenAPI YAML、路由一致性和 Event catalog 校验。
- [x] V-4 Migration Up/Down、`000001` hash 和 Provider 数据保护校验。
- [x] V-5 Compose config、镜像重建和服务健康。
- [x] V-6 图生视频、多分集重建、旧 generation 晚到结果和原生音频浏览器/API smoke。

## 验证记录

| 时间 | 项目 | 证据 |
| --- | --- | --- |
| 2026-07-16 | 执行基线 | 当前仓库最高 migration 为 `000008`；跨镜头 `ContinuityFirstFrame`、连续组 Workflow、`continuity_group_id` 和尾帧专用表仍存在，P0 尚未完成 |
| 2026-07-16 | P0 Provider 数据保护 | API 冻结写入门已启用；`tmp/provider-protection-snapshot.json` 已生成；9 张配置表等值校验与 3 张历史表主键子集校验通过 |
| 2026-07-16 | P0 migration 保护 | `000001_current_schema.sql` 固定 hash、受保护 Provider 表 SQL guard、API 冻结门单测通过；Compose config 有效 |
| 2026-07-16 | P0 视频止损 | `go test ./internal/workflows ./internal/dbmigrate ./internal/api` 通过；新 Workflow 每个 Storyboard Shot 独立执行，fresh 当前镜头图优先，失败不阻断后续镜头，同镜头 Segment 续接测试保留 |
| 2026-07-16 | P0 运行发布门 | 实际库 `activeWorkflowRuns=0`、`activeVideoTasks=0`、`activeVideoLeases=0`；新 `script-worker` 已重建并健康 |
| 2026-07-16 | P1 migrations | 从实际库备份恢复 `cineweave_refactor_dryrun`；`000009/000010` Up 到 v10、Down 回 v8 均成功；四个 Profile 和 20 个 active Prompt Contract version 校验通过 |
| 2026-07-16 | P1 Provider 保护 | migration Up 前后及 Down 后，9 张 Provider 配置表主键/行数/hash 完全一致，3 张历史表基线主键集合无丢失 |
| 2026-07-16 | P1 项目初始化 | `go test ./internal/videoproduction ./internal/api ./internal/authz` 通过；dry-run v10 库中项目、active binding、active generation 同事务写入，reserved 方案返回 `VIDEO_PRODUCTION_PROFILE_UNAVAILABLE`，故障注入后无项目行泄漏 |
| 2026-07-16 | P1 多分集受控重建 | Temporal 状态机单测与 dry-run v10 数据库集成测试通过；impact token、旧代归档、资产保留、generation 切换、partial、attempt 状态和失败分集重试均已验证 |
| 2026-07-16 | P1 generation fence | Node/Workflow/Provider 写入均携带 generation、binding 和 revision；v11 数据库约束与 active-generation trigger 已启用；旧代晚到结果集成测试验证应用 CAS、数据库拒绝、零业务写入和带新旧代信息的 `workflow.result.discarded` 审计事件；多分集重建回归通过 |
| 2026-07-16 | P1 v11 可逆性与 Provider 保护 | dry-run 数据库 `000011` Down 到 v10 再 Up 到 v11 成功；9 张 Provider 配置表等值、3 张历史表基线主键子集校验通过 |
| 2026-07-16 | P1 API/RBAC/Event 收口 | OpenAPI 266 条路由与 Go 注册一致；Web typecheck/lint、Event catalog、定向 Go 测试通过；v11 实库验证旧 `productionMode` 写入被拒绝且生产身份不变，首次重建及失败重试事件 payload 完整 |
| 2026-07-16 | P2 分镜状态契约 | Shot Planner 强制输出单首帧 ShotState/Transition；确定性 canonicalizer、classifier、hash 和 reviewer 单测通过；v11 实库集成测试验证每镜头 2 个 approved state、1 条 active transition、1 个 draft planned-first-frame anchor 及对应事件 |
| 2026-07-16 | P2 typed ReferencePack | Gateway 内部模型约束返回参考图能力/上限；Resolver 仅使用 active/fresh/current 引用并确定性裁剪；v11 实库验证每镜头 active pack、required items、planned-first-frame anchor 绑定及 generation fence |
| 2026-07-16 | P2 PromptContextPlan 与 Prompt Contract | `000012` 写入图生视频 v2 Prompt Registry 契约；图片/视频生成与审核共享确定性上下文预算、镜头状态、转场和引用 hash；定向 Go 测试通过 |
| 2026-07-16 | P2 视频提示词与原生音频 provenance | v12 实库集成测试验证每镜头仅解析 1 张 approved first frame，逐字保留中文台词，`native_av + required` 能力门通过，并原子写入 approved `video_prompt_plans`、逐条 dialogue cues、active audio contract 和完整 SHA-256 provenance |
| 2026-07-16 | P3 canonical planner/create（进行中） | `VideoGenerationVariant`、Gateway planner/create、OpenAI-compatible/OpenRouter adapter 已接 canonical role、generation/binding、PromptContextPlan、ReferencePack、VideoPromptPlan、dialogue cues 与 native audio 校验；`go test ./internal/provider ./internal/workflows` 通过 |
| 2026-07-16 | P3 v12 真实数据库计划验证（进行中） | `TestStoryboardEpisodeV2ActivitiesPersistCompletePlan` 在 dry-run v12 PostgreSQL 通过，验证 approved first-frame contract 与 Prompt/Reference provenance 入 Render Plan；continuation contract、capability attestation、长任务 checkpoint 和完整 create/cost/media 链路仍未完成 |
| 2026-07-16 | 目标文档评审修正 | 明确 initial/continuation 双 Input Contract、capability snapshot attestation、Prompt/Video Batch 严格拆分和长任务 checkpoint；发现 `000009` 仍含旧项目自动 backfill，与“不做旧数据兼容”冲突，P1-1 重新标记为进行中 |
| 2026-07-16 | P1/P3 v13 迁移收口 | 从应用 v8 基线重建 `cineweave_refactor_v13_clean`，迁移到 v13；旧项目为 unconfigured，`000013` hash 为 `009dc37bf306209d075382ef9b70cca31039ccbb7113ee26ff6b05f375c7afbd`；Up/Down/Up schema 稳定，9 张 Provider 配置表与 3 张历史表保护校验通过 |
| 2026-07-16 | P3 双 Input Contract 与能力审批 | initial/continuation contract、Adapter fixture/controlled probe、approve/reject/revoke、stale capability hash fence、OpenAI-compatible 显式 extension 映射及 v13 真实分镜计划集成测试通过 |
| 2026-07-16 | P3 分层长任务 Workflow | `EpisodeBatchGenerateShotVideosWorkflow → EpisodeVideoProductionWorkflow → SceneOrShotBatchWorkflow → bounded shot activities` 已接生产入口；批次按场景/reset 边界切分，单 run 最多 4 批且支持服务端建议 Continue-As-New；单元测试验证分集并发、Child drain、紧凑恢复输入和 Video Batch 不调用 Prompt Agent |
| 2026-07-16 | P3 checkpoint 与失败重试 | v13 PostgreSQL 集成测试验证 checkpoint/batch/item 准备幂等、活动冲突拒绝、partial 聚合、逐 item 事件和只重试失败镜头；修复 checkpoint 外键所有权及集成 fixture Provider connector 泄漏，测试后 Provider 数据仍完全等值 |
| 2026-07-16 | P3 Gateway 真链路 | `TestGatewayVideoRuntimeIntegration` 验证 create/poll、媒体转存与探测、artifact/media、provider call log 和 cost record；锁定项目后晚到 poll 返回 `PRODUCTION_GENERATION_MISMATCH` 且 provider task 不变；模型视频测试自动携带 active generation/binding |
| 2026-07-16 | P3 Worker 可观测性 | script-worker 使用 Pinned Deployment Version；Temporal namespace 幂等注册 `ProjectId/ProductionGenerationId/EpisodeId/ProfileVersionId/RebuildId`，Workflow outbox 写入 Search Attributes 与 Memo，便于版本排空和恢复定位 |
| 2026-07-16 | P4 镜头诊断闭环 | 分镜图弹窗展示 ShotState/Transition/VisualAnchor/ReferencePack 并支持审核与分集重规划；视频弹窗展示 PromptContextPlan/VideoPromptPlan/RenderPlan 全量 provenance，人工修改创建不可变 Prompt revision |
| 2026-07-16 | P4 任务活动 | 新增 `/api/workflow-runs/{workflowRunId}/video-production`，按分集 checkpoint、批次和镜头 item 返回锚点、Prompt、Provider 异步任务、媒体转存与真实错误；活动抽屉支持逐镜头动态、部分完成和按分集重试失败项，实时事件仅定向失效相关查询 |
| 2026-07-16 | P4 首帧锚点运行态 | v13 PostgreSQL 集成测试验证镜头图片成功后创建版本化 approved `planned_first_frame`，旧锚点及 ReferencePack/PromptContextPlan/VideoPromptPlan/RenderPlan 原子 stale，同一结果重放保持幂等 |
| 2026-07-16 | P4 受控重建与能力管理 | 新建项目 Profile 卡可用性改由 API 驱动；项目设置只读展示 binding revision/profile version/generation，并通过 impact token/rebuild/items/retry API 重建；供应商模型页可查看 evidence、审批或拒绝 inferred snapshot、撤销当前 attestation |
| 2026-07-16 | P4 验证 | `go test ./internal/api ./internal/workflows`、P4 两项 v13 集成测试、Web typecheck/lint、OpenAPI YAML 与 287 条路由一致性通过；浏览器与 Compose 全量 smoke 留到 P9 发布门执行 |
| 2026-07-17 | P5 Profile 插件运行时 | 建立统一 `ProfileCompiler`、`ProfileStrategy`、`AnchorStrategy`、`ReferenceStrategy`、`InputContractAdapter` 和 `PromptContractStrategy`；四个内置 Profile contract tests 通过，图生视频 Prompt 安全规则、canonical 引用角色和 available/reserved 执行门保持不变 |
| 2026-07-17 | P5 旧连续性模型清除 | `000014` 将 ReferencePack 拆为 `anchor/video` 两种用途，删除 `continuity_group_id`、旧尾帧专用表和跨镜头尾帧 Workflow；同镜头 RenderSegment 续接改为版本化 approved `observed_tail_frame` VisualAnchor，真实 FFmpeg 提取、事件和幂等重放集成测试通过 |
| 2026-07-17 | P5 migration 与 Provider 保护 | `000014` 空库 Up/Down/Up 结构稳定；隔离数据库 v13→v14 成功，迁移前后 9 张 Provider 配置表完全等值、3 张历史表基线主键无丢失；运行应用库未迁移 |
| 2026-07-17 | P5 验证 | `go test` 覆盖 videoproduction/provider/workflows/api/events/media/dbmigrate/media-worker；Web typecheck/lint、OpenAPI YAML、287 条路由一致性、Event catalog 与 `git diff --check` 通过；lint 仅保留 3 条既有 `<img>` warning |
| 2026-07-17 | P6 首尾帧运行时 | Shot Planner 按 Profile 同时建立绑定 `planned_entry/planned_exit` 的首帧与尾帧锚点；Prompt、图片生成和批处理按锚点角色独立执行，双帧通过尺寸、人物身份、服装、道具、站位、空间轴线、机位可达性和时长审核后才成对批准 |
| 2026-07-17 | P6 Gateway 与 Profile 发布 | `000015` 发布 `first_last_frame` v2 为 `published + available`，v1 保持 `reserved`；Gateway 只选择原生 `first_last_frames` 模型并将首帧/尾帧映射为两个有序输入，超过单次模型时长时返回非重试 `STORYBOARD_REPLAN_REQUIRED`，不复用同一帧对分段生成 |
| 2026-07-17 | P6 集成与前端验证 | v15 PostgreSQL 集成测试验证 Planner→双 Prompt→双图片→成对审核→双引用 VideoPromptPlan→单段 RenderPlan 全链路及 31 秒重规划门；修复尾帧 ReferencePack 错绑 entry state 和图片完成 SQL 参数错误；首尾帧弹窗支持分角色查看与独立重建；模块 Go 测试、Web typecheck/lint 通过 |
| 2026-07-17 | P7 typed ReferencePack 与能力门 | ReferencePack item 增加 `mediaType/semantics`，按图片、视频、音频独立上限确定性裁剪；视频模型引用能力只从标准化 Input Contract 推导，通用参考图字段不再把首帧模型误判为多模态模型；角色、场景、道具、动作视频和音频角色单元测试通过 |
| 2026-07-17 | P7 Gateway 与 Profile 发布 | OpenAI-compatible/OpenRouter Adapter 保留 canonical role 并分别映射图片、视频、音频引用；同镜头多段支持 fresh 尾帧加原语义引用；`000016` 发布 `multimodal_reference` v2 为 `published + available`，v1 保持 `reserved` |
| 2026-07-17 | P7 集成与前端验证 | v16 PostgreSQL 集成测试验证 Planner→首帧 Prompt/图片→角色、场景、道具 typed ReferencePack→视频 Prompt/RenderPlan；单帧、首尾帧、多模态三条实库回归和诊断 API 通过；图片/视频弹窗以中文展示引用媒体类型与语义；Provider 模块、Workflow/API、Web typecheck/lint、OpenAPI 287 路由一致性通过 |
| 2026-07-17 | P8 PanelManifest 与媒体链路 | 建立 3/4/6 画格确定性 PanelManifest、`storyboard_sheet` 与 `storyboard_panel` VisualAnchor、整板生图、Media Worker 确定性裁板和多模态实际成图审核；画格数、顺序、无文字、身份、场景与动作序列任一失败即拒绝 |
| 2026-07-17 | P8 Gateway 与 Profile 发布 | `000017` 发布 `storyboard_sheet` v2 为 `published + available`，锁定 `gpt-image-2` 和 `storyboard_sheet_reference`；OpenAI-compatible/New API/OpenRouter 显式映射 `storyboard_sheet` 为 `image_url`，不能冒充 `first_frame`；migration hash 为 `82f6fc919842f094fcaceddc6b6695bfe5c252be9db551108b1a673062738575` |
| 2026-07-17 | P8 集成与前端验证 | v17 PostgreSQL 集成测试验证 Planner→Manifest→GPT-image-2 Prompt/图片→裁板→成图审核→视频 Prompt→带 Manifest provenance 的 ReferencePack→单段 RenderPlan；首尾帧、多模态、分镜板三条实库回归和分镜板诊断 API 通过；前端展示整板、按序画格和审核溯源；Provider 9 张配置表完全等值、3 张历史表身份集合保留；模块 Go 测试、Web typecheck/lint、OpenAPI 288 路由和 Event catalog 通过 |
| 2026-07-17 | P9 旧运行时与宫格模式清理 | 新增 `000018_remove_legacy_video_production_mode.sql` 并完成 Up/Down/Up；移除 `projects.production_mode`、旧通用视频 Prompt、内联视频 Prompt Agent、旧能力推导、跨镜头连续组和媒体状态回退；全仓活动代码无 `grid_image`、`previous_last_frame` 或旧 Workflow 残留；migration hash `1125d160c8017a69c16e7d28265d27d0bff5977a89fcddc48b64a87a7f226e0f` |
| 2026-07-17 | P9 全仓与 replay | `go test ./...`、Web typecheck/lint、OpenAPI YAML 与 288 路由、Event catalog、Compose config 和 diff check 通过；JSON replay、真实 Temporal Pinned 版本 promotion 后 N-1 history replay、Gateway video runtime、capability attestation、generation/node fence 与取消回归通过 |
| 2026-07-17 | P9 Profile/长任务压测 | `TestEpisodeBatchGenerateShotVideosWorkflowBoundsTenEpisodeConcurrency` 验证 10 集只保留 2 个并发 Episode Child；`TestEpisodeVideoProductionWorkflowSeventyMinuteLoadUsesBoundedContinueAsNew` 以 840 镜头验证 84 个 batch 分 21 个 run 且每 run 最多 4 batch；真实 PostgreSQL 读取四个最新 available v2 Profile，逐方案完成 10 集常规镜头与 70 分钟长分集的 1080 镜头契约循环 |
| 2026-07-17 | P9 migration 与 Provider 数据保护 | 空库全量 Up→Down→Up 到 v18 通过；正式库部署前确认 active Workflow/video task/lease 均为 0，部署前后 9 张 Provider 配置表主键/行数/hash 完全一致，3 张历史表基线主键无丢失；Prompt Registry seed v2，旧视频 Prompt active 数为 0 |
| 2026-07-17 | P9 Compose 发布 | `docker compose -f compose.yml --profile app up -d --build` 成功；正式库 migration v18，四个最新 v2 Profile 均 `published + available`；API、Realtime、Provider Gateway、Web、MinIO 和所有长期服务健康，Provider Gateway 内网 `/healthz`、`/readyz` 通过 |
| 2026-07-17 | P9 浏览器与文档 smoke | 浏览器验证四张 Profile 卡、图生视频项目创建回显、不可变 binding v2/generation、受控重建四方案入口、分镜/视频页面、图片质量中文标签和零前端控制台错误；冒烟项目随后通过公共 API 删除；执行计划、Workflow、Provider Gateway 和运行运维手册已更新 |
