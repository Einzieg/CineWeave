# CineWeave 视频生产方案与锚点式连续性重构目标文档

- 状态：目标规格已实现；逐项证据见 `docs/video-production-workflow-continuity-refactor-progress.md`
- 更新时间：2026-07-17
- 适用仓库：`D:\Code\CineWeave`
- 适用范围：项目设置、剧本到分镜、镜头图片、视频提示词、视频 Render Plan、Provider Gateway、Temporal Workflow、任务活动与视频工作台
- 当前交付：四个 Profile v2 均已完成独立契约、运行时和发布验收

本文档定义 CineWeave 视频生产链路下一阶段的长期目标状态、强制架构决策、领域模型、实施顺序和验收门槛。项目处于开发阶段，本轮允许对项目视频生产数据执行不兼容的数据结构重构，不保留旧 `production_mode`、旧 `continuity_group_id` 或跨镜头无条件尾帧串联行为。已配置的供应商账号、加密凭据、供应商模型、模型能力和业务模型绑定是迁移保护数据，不得因本轮重构被删除、重置或重新 seed 覆盖。

相关文档：

- `docs/script-to-storyboard-timing-refactor-plan.md`：继续负责剧本时长、镜头数量、Timing Unit、Storyboard Shot 与同镜头 Render Segment 的规划。
- `docs/provider-gateway.md`：继续负责 Provider Gateway 边界、上游请求、模型选择、错误归一化、调用日志和成本记录。
- `docs/workflow-engine.md`：继续负责 Workflow、节点状态、取消、重试和恢复约束。
- `docs/runtime-foundation-hardening-target.md`：继续负责长任务、幂等、事件和发布基础设施。
- `docs/codex-execution-plan.md`：作为 Codex 总执行入口。

当其他文档与本文档在“生产方案、跨镜头连续性、尾帧传递、参考图语义”方面冲突时，以本文档为准。

## 1. 结论先行

本轮重构采用以下核心设计：

1. 新建项目页面的“生成方式”直接用以下四项替换旧的“完整视频链路 / 仅生成分镜 / 仅生成资产 / 自定义生产”：
   - 图生视频模式
   - 首尾帧衔接模式
   - 多模态参考模式
   - 分镜板模式
2. 项目创建成功后，生产方案成为不可变项目属性；项目设置只能查看，不提供普通切换控件。
3. 切换方案必须执行独立的“按新生产方案重建分镜”操作：清空当前活动分镜及其镜头级下游产物，保留 canonical assets，再按新方案生成适配分镜。
4. 当前首要发布目标是完整、可靠地落地 `single_frame_i2v`。另外三种方案先建立 Profile、能力契约、Prompt 契约、数据结构和前端卡片占位，在实现完成前不得创建虚假可运行项目或静默降级到图生视频。
5. 四种方案是版本化 `VideoProductionProfile`，不是四份复制的 Temporal Workflow。
6. Workflow 通过统一 `VideoProductionCompiler` 将项目方案、镜头状态、转场、模型能力、Prompt Contract 和参考素材编译为每镜头 `ShotRenderPlan`。
7. 每种生产方案必须有独立、版本化且可审核的图片 Prompt、视频 Prompt 和 Reviewer 适配；结构化状态是 Prompt 的输入和约束，不能用一套通用 Prompt 硬拼四种方案。
8. 跨镜头剪辑不再把上一镜头尾帧直接替换成下一镜头首帧。
9. 每个 `StoryboardShot` 必须拥有自己的权威视觉锚点。上一镜头尾帧只能是受控辅助参考，不能成为新的项目事实源。
10. `previous_last_frame` 和 `video_extension` 只允许用于同一个 `StoryboardShot` 因模型时长限制拆出的连续 `RenderSegment`。
11. 镜头间连续性由结构化 `ShotStateContract` 和 `ShotTransition` 表达，区分需要继承和必须重置的维度。
12. 对只能接收一张图片的视频模型，必须先在图片阶段把当前镜头人物、场景、道具、机位和站位合成为准确的干净首帧。
13. Agent 负责理解剧本和提出视觉决策；确定性代码负责能力匹配、状态差异、引用完整性、依赖图、版本、幂等和合法性校验。
14. API Server 和 Worker 不得直接调用上游供应商；GPT-image-2 和所有视频模型调用仍必须经过 Provider Gateway。
15. 实际视频输出不得静默覆盖计划状态。若用户接受输出偏差，必须创建新的状态修订并显式使下游产物 stale。
16. 每次创建或重建都产生单调递增的 `ProductionGeneration`；旧代任务的延迟结果不能写入当前代。
17. 项目方案重建按分集建立独立 item，支持部分成功和只重试失败分集，不用单一 plan ID 假设项目只有一集。
18. 整集剧本是视频 Prompt 的权威语料，但每镜头请求使用受模型限制约束的 `PromptContextPlan`；逐字中文台词和原生音频要求是结构化硬约束。
19. `RenderSegment 0` 的初始输入契约和后续 Segment 的续接输入契约必须分别规划、分别冻结；不得把 `first_frame`、`video_extension`、`previous_segment_tail` 或普通 `video_reference` 混为同一种输入。
20. `inferred` 模型能力只能通过绑定到精确 capability snapshot hash 的显式审批进入自动生产；能力快照变化后旧审批自动失效，审批不得改写供应商模型配置。
21. 视频 Prompt 的生成/审核和视频执行是两个独立阶段。执行阶段只消费不可变、已审核且未 stale 的 `VideoPromptPlan`，不得隐式调用 Prompt Agent。
22. 长任务的分集、批次和镜头 item 状态必须持久化为 checkpoint；Temporal history 不是唯一业务状态源，Continue-As-New 后必须能只凭 checkpoint 和不可变 ID/hash 恢复。

## 2. 当前实现问题

### 2.1 代码事实

当前实现存在以下结构：

| 位置 | 当前行为 | 问题 |
| --- | --- | --- |
| `storyboard_shots.continuity_group_id` | 把一组镜头放入同一连续组 | 一个 UUID 无法表达换机位、人物出入画、场景重置和继承维度 |
| `PrepareShotVideoExecutionGroups` | 按连续组和 `shot_index` 串行分组 | 只理解顺序，不理解转场语义 |
| `crossShotVideoContinuityGroupWorkflow` | 提取每个视频的尾帧并传入下一镜头 `ContinuityFirstFrame` | 把生成误差递归放大 |
| `storyboard_shot_continuity_frames` | 只允许 `frame_role='tail'` | 无法表达计划首帧、计划尾帧、分镜板和审核状态 |
| `ShotPlannerSuggestion.ContinuityGroupKey` | Agent 输出模糊连续组 key | 未经过场景、人物集合、机位和站位的确定性验证 |
| `StoryboardShot.AssetRequirements` | 有当前镜头资产需求 | 执行时可能被上一镜头尾帧取代，资产需求失去权威性 |

### 2.2 根本概念错误

当前实现混淆了两类完全不同的连续性：

| 连续性类型 | 正确定义 | 正确手段 |
| --- | --- | --- |
| 同一摄影意图内部的时间连续性 | 一个长镜头被供应商时长限制拆成多段 | 视频延长、前段尾帧、首尾帧插值 |
| 不同镜头之间的剪辑连续性 | 发生切镜，但人物身份、服装、道具、空间关系、视线和动作需要合理延续 | 当前镜头锚点、资产引用、站位和转场状态契约 |

切镜本身允许并且经常要求改变机位、景别、构图和角色位置。把切镜前的像素画面强行作为切镜后的首帧，会迫使模型执行不合理的视角变形、场景变形或人物变形。

### 2.3 典型失败场景

- 下一镜头新增人物，但上一镜头尾帧中没有该人物。
- 下一镜头切到反打机位，尾帧仍保持原机位。
- 人物从远景移动到近景，模型只能从旧构图继续猜测。
- 同场景时间、天气或光线改变，但尾帧锁死旧环境。
- 上一视频已经发生脸部、服装或背景漂移，下一视频继续继承漂移。
- 连续组中一个镜头失败，后续所有镜头都被 `CONTINUITY_DEPENDENCY_FAILED` 阻断。
- 当前镜头有正确分镜图和资产引用，但视频请求实际优先使用错误的前序尾帧。

## 3. 目标与非目标

### 3.1 目标

- 为四种生产方案建立稳定、可扩展且可验证的统一执行架构。
- 让每个镜头的视频输入符合该镜头自己的剧本、人物、场景、道具、机位和站位。
- 只在真正连续的同镜头 Render Segment 之间使用硬尾帧依赖。
- 支持换机位、动作匹配剪辑、人物进入退出、场景切换和时间跳转。
- 支持单参考图模型和强多模态模型，而不在 Workflow 内写供应商特例。
- 支持长时间、分集、逐镜头、可并发、可取消、可重试和可恢复的生产。
- 让模型能力、项目生产方案、实际请求和前端展示保持一致。
- 为四种方案分别建立 Prompt Contract、Prompt Registry 模板、结构化渲染输入和审核规则。
- 允许以图生视频模式作为首个垂直切片发布，同时保证其余方案可以在不复制 Workflow 的前提下逐步启用。
- 保存完整 Prompt、参考图、模型能力、Provider Call、审核和状态版本溯源。
- 一个镜头失败时，只阻塞真实依赖链，不阻塞下一个独立视觉锚点之后的镜头。

### 3.2 非目标

- 本轮不实现预计成本 UI；实际 `cost_records` 仍必须正常记录。
- 本轮不把多个 Storyboard Shot 一次生成成一个不可拆分的视频。
- 本轮不把供应商生成结果自动提升为角色或场景的 canonical asset。
- 本轮不依靠 Prompt 文本**单独**替代结构化镜头状态和模型能力；但必须针对四种生产方案调整 Prompt，Prompt 必须消费对应的结构化状态、引用和能力快照。
- 本轮不兼容旧项目的视频生产字段、旧分镜和旧镜头下游产物；允许按项目清空并重建这些数据，但不允许清空整个数据库。
- 本轮不删除、重置或覆盖已配置的供应商账号、凭据、模型、能力和业务模型绑定。
- 本轮不引入浏览器内存队列承载生产任务。

## 4. 术语

| 术语 | 定义 |
| --- | --- |
| `VideoProductionProfile` | 用户选择的版本化生产方案 |
| `VideoProductionProfileVersion` | 某一生产方案可发布、可校验且不可变的配置版本 |
| `ProjectVideoProductionBinding` | 项目创建时写入的不可变生产方案绑定 |
| `ProductionGeneration` | 项目视频生产数据的一代写入空间；所有 Workflow 和业务产物必须携带该代标识 |
| `ProjectVideoProductionRebuild` | 清空活动分镜后切换方案并重新生成适配分镜的受控重建操作 |
| `PromptContextPlan` | 根据整集权威语料和模型限制确定性裁剪出的单镜头 Prompt 上下文计划 |
| `ShotStateContract` | 一个镜头计划入口、计划出口或实际出口的结构化视觉状态 |
| `ShotTransition` | 两个 Storyboard Shot 之间的剪辑关系及继承、重置规则 |
| `VisualAnchor` | 当前镜头自己的首帧、尾帧或分镜板等权威视觉输入 |
| `ReferencePack` | 为一次图片或视频生成解析出的带类型、优先级和来源的参考素材集合 |
| `ShotRenderPlan` | 某个镜头在指定生产方案和模型能力下的不可变执行计划 |
| `RenderSegment` | 一次真实视频供应商请求；多个 Segment 只能组成同一个 Storyboard Shot |
| `PlannedEntryState` | 当前镜头计划开始时的视觉状态 |
| `PlannedExitState` | 当前镜头计划结束时的视觉状态 |
| `ObservedExitState` | 对生成视频实际尾部状态的审核结果 |
| `Hard Reference` | 必须作为供应商首帧、尾帧或视频输入的参考 |
| `Semantic Reference` | 用于身份、场景、风格、动作等引导，但不等于输出首帧 |
| `Reset Boundary` | 不允许继承前序像素状态的切镜边界 |

## 5. 强制领域不变量

1. 一个 `StoryboardShot` 对应一个可独立审核和替换的视频结果。
2. 一个 `StoryboardShot` 可以有多个 `RenderSegment`，但 Segment 不得跨越镜头边界。
3. 不同 `StoryboardShot` 之间禁止使用 `previous_last_frame` 作为无条件硬首帧。
4. 当前镜头的 `PlannedEntryState`、资产需求和视觉锚点优先于任何前序生成结果。
5. 只有同一镜头内部 Segment 才能使用 `previous_segment_tail` 或 `video_extension`。
6. 未分类或低置信度转场默认 `reset`，不得默认继承。
7. 视频生成前必须存在有效、已审核且未 stale 的 `ShotRenderPlan`。
8. 实际输出审核失败时，不得产生可供后续使用的 active continuity anchor。
9. 更换生产方案、模型能力快照、镜头状态、参考素材或视频 Prompt 后，旧 Render Plan 必须 stale。
10. 前端不得手工拼装供应商请求；所有 canonical request 由后端编译。
11. 项目创建后不得直接更新 `ProjectVideoProductionBinding`；切换只能通过受审计的 `ProjectVideoProductionRebuild`。
12. 重建生产方案必须归档当前活动 StoryboardPlan 及镜头级下游状态，但不得物理删除 canonical assets、资产参考历史或已配置供应商和模型。
13. `implementation_state != available` 或 `lifecycle_state != published` 的 Profile version 不得创建项目、启动 Workflow 或被自动降级为其他 Profile。
14. Prompt 必须按 Profile 选择版本化模板和 Reviewer，且不得绕过 ShotState、ReferencePack 或 capability validation。
15. 所有 Workflow、节点、Provider 异步任务和镜头级业务写入必须携带 `productionGenerationId`；只有与项目当前 active generation 一致的写入才可提交。
16. 旧 generation 的延迟 Activity、Provider 回调、媒体转存或重试不得更新新 generation 的活动数据，即使它所属的 Workflow/Node 仍显示可写。
17. 一个项目可以有多个分集；方案重建必须按分集记录归档、新计划、Workflow、checkpoint 和失败状态，不得用单一 plan ID 代表整个项目。
18. 整集剧本是 Prompt 的权威语料库，但不得在每个镜头请求中无界原样注入；上下文必须由 `PromptContextPlan` 按模型限制确定性编译，当前镜头逐字中文台词不得被截断。
19. `RenderSegment 0` 必须满足 Render Plan 冻结的 `initialInputContract`；`RenderSegment > 0` 必须满足独立的 `continuationInputContract`，不能复用首段契约冒充续接能力。
20. 普通 `video_reference` 只表示语义视频参考，不等于供应商的视频延长能力；同镜头续接只能选择经能力验证的 `video_extension`，或抽取 fresh 尾帧后重新满足 `first_frame` 契约。
21. 视频执行阶段不得创建、改写、重新审核或重新裁剪 Prompt；没有 approved `VideoPromptPlan` 时必须返回可操作的重规划错误。
22. capability `verificationStatus=inferred` 且没有当前 snapshot 审批时不得自动路由；`unknown` 永远不能满足强能力要求。
23. Continue-As-New 前必须提交 checkpoint 并 drain 活跃 Child/Provider task；新 run 不得依赖上一 run 内存中的待执行集合。

## 6. 目标架构

~~~mermaid
flowchart TD
    Project["Project + Binding + Active Generation"] --> Episode["ScriptEpisode / StoryboardPlan"]
    Episode --> StatePlanner["Shot State Planner Agent"]
    StatePlanner --> StateValidator["Deterministic State Validator"]
    StateValidator --> Transitions["ShotTransition Graph"]
    Transitions --> RefResolver["Reference Pack Resolver"]
    RefResolver --> AnchorCompiler["Visual Anchor Compiler"]
    AnchorCompiler --> ImageGateway["Provider Gateway Image Runtime"]
    ImageGateway --> AnchorReview["Visual Anchor Review Agent"]
    AnchorReview --> VideoCompiler["Video Production Compiler"]
    VideoCompiler --> PlanGateway["Provider Gateway Video Planner"]
    PlanGateway --> RenderPlan["ShotRenderPlan + Capability Snapshot"]
    RenderPlan --> VideoWorkflow["Temporal Episode / Batch Workflow"]
    VideoWorkflow --> VideoGateway["Provider Gateway Video Runtime"]
    VideoGateway --> Media["Artifact / MediaFile"]
    Media --> ExitReview["Observed Exit State Review"]
    ExitReview --> Timeline["Timeline / Final Video"]
~~~

### 6.1 组件职责

| 组件 | 职责 |
| --- | --- |
| API Server | 项目配置、生产动作、查询、RBAC、OpenAPI，不调用供应商 |
| Script Worker | 结构化剧本和镜头状态规划，通过 Gateway 调文本模型 |
| Image Worker | 锚点和分镜板工作流编排，通过 Gateway 调图片模型 |
| Video Worker | Render Plan 执行、异步任务轮询、同镜头 Segment 依赖 |
| Media Worker | FFmpeg、帧提取、媒体探测和确定性裁切，不调用供应商 |
| Provider Gateway | credential、模型路由、能力匹配、上游调用、媒体转存、日志、成本 |
| Realtime | 可靠事件流和查询失效提示，不承载业务状态 |
| Web | 配置、查看、审批、重试和实时状态，不承载任务队列 |

## 7. 四种视频生产方案

系统 seed 四个内置 `VideoProductionProfile` family 及其首个 `VideoProductionProfileVersion`。项目创建时绑定具体 version；Workflow 和 Render Plan 保存不可变快照和 hash。

### 7.0 创建入口、可用性和不可变规则

新建项目页面原有以下四项必须删除：

~~~text
silent_video    -> 完整视频链路
storyboard_only -> 仅生成分镜
assets_only     -> 仅生成资产
custom          -> 自定义生产
~~~

同一位置直接替换为：

~~~text
single_frame_i2v       -> 图生视频模式
first_last_frame       -> 首尾帧衔接模式
multimodal_reference   -> 多模态参考模式
storyboard_sheet       -> 分镜板模式
~~~

Profile family 不保存会与版本状态冲突的 `status/availability`。可执行性由 version 上两个正交字段表达：

~~~text
lifecycle_state: draft | published | retired
implementation_state: reserved | available
~~~

- `lifecycle_state` 表示版本是否已正式发布；`draft` 不可绑定，`retired` 不接受新绑定。
- `implementation_state` 表示运行时是否已达到该版本的验收门槛；`reserved` 只提供真实占位契约，不可执行。
- 只有 `published + available` 可创建项目、启动 Workflow 或作为重建目标。

首个里程碑状态固定为：

| Profile | lifecycle_state | implementation_state | 前端行为 |
| --- | --- | --- | --- |
| `single_frame_i2v` | `published` | `available` | 默认选中，可创建项目 |
| `first_last_frame` | `published` | `reserved` | 显示方案卡片但禁用选择 |
| `multimodal_reference` | `published` | `reserved` | 显示方案卡片但禁用选择 |
| `storyboard_sheet` | `published` | `reserved` | 显示方案卡片但禁用选择 |

禁用卡片只显示统一状态“暂不可用”，不得显示虚构的兼容模型，不得接受点击后偷偷写入 `single_frame_i2v`。当某方案通过自己的完整验收后，发布新的 `published + available` Profile version，无需复制项目创建逻辑；不得原地修改已被项目绑定的 version。

项目创建事务必须同时写入项目和 immutable binding；任一写入失败则整个事务回滚。项目详情和项目设置只展示已绑定方案，不提供 Select、Radio 或卡片切换。方案切换只通过第 17.1 节定义的重建命令完成。

### 7.1 图生视频模式

内部 key：`single_frame_i2v`

适用：

- 只能接受一张输入图片的视频模型。
- 图片被供应商解释为输出视频的起始帧。
- 例如只支持普通 image-to-video 的模型变体。

执行：

1. 从当前镜头剧本、资产、场景和机位生成一张干净首帧。
2. 审核首帧中的人物集合、场景、站位、服装、道具和构图。
3. 视频请求只发送当前镜头首帧和已审核视频 Prompt。
4. 除同一镜头 Render Segment 外，前序视频尾帧不得替换当前首帧。

必要模型能力：

~~~text
taskType = video.image_to_video
inputContract = first_frame
maxFirstFrames >= 1
~~~

### 7.2 首尾帧衔接模式

内部 key：`first_last_frame`

适用：

- 模型原生支持同时提交首帧和尾帧。
- 镜头需要明确结束构图、角色目标位置或动作终点。

执行：

1. 生成当前镜头 `planned_first_frame`。
2. 生成当前镜头 `planned_last_frame`。
3. 审核两个关键帧中的身份、服装、场景、道具和动作可达性。
4. Provider Gateway 选择支持首尾帧组合的模型 variant。
5. 前序尾帧最多作为生成当前首帧时的语义参考，不直接替换当前首帧。

必要模型能力：

~~~text
inputContract = first_last_frames
supportsFirstFrame = true
supportsLastFrame = true
~~~

### 7.3 多模态参考模式

内部 key：`multimodal_reference`

适用：

- 支持多张角色、场景、道具参考图。
- 可选支持参考视频、音频或语义参考图。
- 视频模型能区分首帧与普通 reference image。

执行：

1. 当前镜头干净首帧为主要构图锚点。
2. 当前镜头要求的角色、场景、道具和衍生资产按类型加入 Reference Pack。
3. 只有转场允许时，前序已审核尾帧才可作为低优先级 `continuity_hint`。
4. 参考视频或音频只有在模型能力明确允许时加入。
5. 超过模型输入上限时，由确定性 Reference Resolver 按优先级裁剪，不能随机截断。

必要模型能力：

~~~text
inputContract = semantic_references 或 first_frame_plus_references
supportsSemanticReferenceImages = true
maxReferenceImages > 0
~~~

### 7.4 分镜板模式

内部 key：`storyboard_sheet`

定义：

- 同一个 Storyboard Shot 的多个时间点关键帧由 GPT-image-2 生成在一张有序分镜板图中。
- 分镜板作为视频模型的语义参考，不得被解释为视频首帧。
- 一个镜头仍只生成一个镜头视频。

执行：

1. `StoryboardSheetPlanner` 根据镜头时长、动作节拍和台词时间轴生成 `PanelManifest`。
2. GPT-image-2 一次生成一张分镜板。
3. Media Worker 确定性裁出各 panel，仅用于审核、预览和可追溯性。
4. 审核 Agent 检查 panel 数量、顺序、人物身份、场景、动作阶段和机位变化。
5. 视频请求发送分镜板、结构化 panel 顺序和视频 Prompt。
6. 若模型还支持独立首帧加语义参考，则同时发送干净首帧；否则模型必须明确支持 `storyboard_sheet_reference`。

分镜板不得包含：

- 编号文字
- 字幕或台词
- 说明文字
- 水印
- 可能被复制进成片的 UI 标记

建议 panel 数量：

| 镜头计划时长 | 默认 panel 数 |
| --- | --- |
| 1 至 5 秒 | 3 |
| 6 至 10 秒 | 4 |
| 11 至 15 秒 | 6 |
| 超过模型单次上限 | 先拆 Render Segment，不无限增加 panel |

必要模型能力：

~~~text
inputContract = storyboard_sheet_reference
supportsStoryboardSheetReference = true
referenceImageSemantics = guidance
~~~

不得把“支持多张图片”直接等价为“支持分镜板”。该能力必须来自官方资料、人工验证或组织级能力配置。

### 7.5 四种方案的 Prompt 适配

“结构化优先”不表示四种生产方案共用一段 Prompt。每个 Profile version 必须绑定自己的 Prompt Contract，并至少包含：

| Profile | 图片 Prompt 目标 | 视频 Prompt 目标 | Reviewer 重点 |
| --- | --- | --- | --- |
| `single_frame_i2v` | 生成可直接作为视频起始帧的单张干净画面 | 从唯一首帧执行镜头运动、动作和中文对白 | 首帧人物/场景/道具完整，动作从首帧可达 |
| `first_last_frame` | 分别生成计划首帧和计划尾帧 | 在两个约束帧之间完成可达运动 | 首尾身份一致、位移合理、结束构图准确 |
| `multimodal_reference` | 生成主构图锚点并整理语义参考 | 显式区分首帧、角色、场景、道具、视频和音频参考 | 引用角色不串位、模型输入数量和语义正确 |
| `storyboard_sheet` | 生成无文字的有序关键帧分镜板 | 按 PanelManifest 和镜头时间轴完成运动 | panel 顺序、动作阶段、机位变化和时间一致 |

Prompt Registry key 建议使用：

~~~text
video_profile.{profile_key}.anchor.plan
video_profile.{profile_key}.anchor.generate
video_profile.{profile_key}.anchor.review
video_profile.{profile_key}.video.generate
video_profile.{profile_key}.video.review
~~~

每次渲染必须保存 `profileKey/profileVersionId/profileSnapshotHash/promptVersionId/promptHash/inputContractVersion/promptContextPlanHash`。模板输入由后端从 `ShotStateContract`、`ShotTransition`、`ReferencePack`、`PromptContextPlan` 和模型能力快照构造，不允许前端拼接，也不允许把结构化 JSON 原样塞入自然语言模板后跳过校验。

`PromptContextPlan` 必须在调用 Prompt Agent 前确定性编译并持久化，至少包含：

~~~text
episodeContinuityDigest       整集人物、目标、伏笔和视觉连续性摘要
currentSceneScript            当前场景完整剧本
adjacentSceneSummaries        相邻场景摘要
currentShotState              当前镜头结构化状态
verbatimDialogueCues          当前镜头逐字中文台词及说话人
modelContextLimit             模型上下文上限
modelPromptLimit              Provider 请求 Prompt 上限
budgetAllocation              各层预算和实际使用量
sourceHashes                  剧本、场景、镜头和摘要来源 hash
~~~

整集剧本仍是编译摘要和检索上下文的权威语料，但不得为每个镜头重复注入整集原文。预算溢出时按“远邻场景摘要、整集摘要细节、当前场景非相关段落”的顺序压缩；`verbatimDialogueCues`、当前镜头动作约束和安全关键限制不得截断。无法在模型限制内保留硬约束时返回 `PROMPT_CONTEXT_LIMIT_EXCEEDED`，不得静默生成不完整 Prompt。

Prompt 组合固定为分层契约，而不是复制四份完整大模板：

1. 公共安全、输出结构和中文台词规则。
2. 项目导演手册和视觉手册的已绑定版本。
3. 当前 Profile version 的策略片段。
4. 当前镜头状态、转场、Reference Pack 和 `PromptContextPlan`。
5. Provider model capability、时长、比例、Prompt 长度和原生音频限制。

每层保存 version/hash，Reviewer 使用同一份上下文计划和硬约束，不得自行重新裁剪语料。

图生视频模式的首个实现还必须满足：

- 镜头图片 Prompt 不包含台词、字幕或说明文字，只描述可见的首帧状态。
- 视频 Prompt 按剧本保留需要说出的中文台词，并按镜头时长校验对白可执行性。
- 已审核且未 stale 的视频 Prompt 在生成视频时直接复用；生成视频不得再次隐式运行 Prompt Agent。
- 更换模型但 Input Contract 兼容时重新验证限制；Profile、结构化状态或引用变化时使 Prompt 和 Render Plan stale。

Prompt 生命周期必须是显式状态机：

~~~text
draft -> generating -> generated -> reviewing -> approved
                                  -> rejected
approved -> stale | superseded
~~~

- “批量生成视频提示词”负责 `PromptContextPlan -> Prompt Agent -> Reviewer -> approved VideoPromptPlan`，支持逐镜头并发、部分完成和只重试失败项。
- “批量生成视频”只接收 approved `video_prompt_plan_id`，然后执行 `Render Plan -> Provider task -> media transfer`；该任务中 Prompt Agent 和 Prompt Reviewer 的 Activity 调用次数必须为 0。
- 如果 Prompt、ReferencePack、ShotState、Profile、模型能力快照或 generation 已变化，视频执行返回 `VIDEO_PROMPT_PLAN_STALE`/`RENDER_PLAN_REPLAN_REQUIRED`，不得现场重生成 Prompt。
- 多 Segment 的 Prompt 只能由确定性 segment materializer 按时间范围派生；每段对白 cue 使用镜头本地整数 tick，并逐字来自 approved plan。

## 8. 生产方案与音频策略正交

视频输入方案和音频策略是两个不同维度。

视频方案：

~~~text
single_frame_i2v
first_last_frame
multimodal_reference
storyboard_sheet
~~~

音频策略继续保持：

~~~text
native_av
hybrid
tts_postdub
~~~

例如：

- 多模态参考 + 原生音视频
- 首尾帧 + 后期 TTS
- 分镜板 + 原生对白

不得再使用 `production_mode='silent_video'` 同时表达视频输入方式和是否有声音。

## 9. 模型能力重构

### 9.1 问题

当前 `supportsFirstFrame`、`supportsLastFrame`、`supportsVideoReference` 和 `referenceModes` 可以表达基础能力，但不足以表达输入角色、组合限制和互斥关系。

例如：

- 有的模型把图片当首帧。
- 有的模型把图片当普通角色参考。
- 有的模型支持首帧或参考图，但不能同时使用。
- 有的模型支持多张参考图，却不理解有序分镜板。
- 有的模型支持视频延长，但不支持跨供应商视频输入。

### 9.2 目标 Input Contract

`VideoGenerationVariant` 新增结构化 `inputContract`：

~~~json
{
  "contractKey": "first_frame_plus_references",
  "requestMode": "async_create",
  "slots": [
    {
      "role": "first_frame",
      "mediaType": "image",
      "semantics": "output_start_frame",
      "min": 1,
      "max": 1,
      "ordered": true
    },
    {
      "role": "semantic_reference",
      "mediaType": "image",
      "semantics": "identity_scene_style_guidance",
      "min": 0,
      "max": 8,
      "ordered": false
    }
  ],
  "mutuallyExclusiveRoles": [],
  "supportsStoryboardSheetReference": false,
  "supportsVideoExtension": false
}
~~~

至少支持的 canonical contract：

~~~text
text_only
first_frame
first_last_frames
semantic_references
first_frame_plus_references
storyboard_sheet_reference
video_reference
video_extension
~~~

Render Plan 必须区分两类契约：

| 契约 | 适用范围 | 图生视频模式要求 |
| --- | --- | --- |
| `initialInputContract` | `RenderSegment 0` | 必须为 `first_frame`，并且恰好携带当前镜头已审核首帧 |
| `continuationInputContract` | 同一镜头的 `RenderSegment > 0` | 优先使用模型明确支持的 `video_extension`；否则抽取前段 fresh 尾帧并再次满足 `first_frame`；两者都不支持时必须缩短镜头、换兼容模型或重规划 |

`video_reference` 是普通语义参考能力，不得被解释为 `video_extension`。Provider Adapter 必须分别映射初始请求和续接请求，不能因为上游字段名称都叫 `video` 就合并语义。

### 9.3 能力来源

每个 variant 必须保存：

~~~text
source
sourceUrl
verifiedAt
capabilityVersion
verificationStatus: official | tested | inferred | unknown
~~~

规则：

1. `official` 和 `tested` 只有在证据未失效且 capability snapshot hash 一致时可进入自动生产。
2. `inferred` 必须由有 `provider_models.manage` 权限的组织管理员显式批准；普通项目用户的临时确认不能提升组织级模型能力。
3. 审批作用域必须包含 `organizationId/providerModelId/variantKey/capabilitySnapshotHash`，能力、Adapter 或请求映射变化后旧审批自动失效。
4. 审批记录是 append-only 审计，支持 revoke；不得通过审批接口回写或覆盖 `provider_models.capabilities`。
5. `unknown` 不得满足强能力要求，也不得通过 `compatible_fallback` 绕过。
6. 不得通过模型名称字符串硬编码能力。
7. Provider Gateway 在 planner 和 create 两次校验 verification/approval；前端筛选不能代替运行时校验。

能力审批使用独立记录，不修改已配置供应商和模型：

~~~text
provider_model_capability_attestations
  id
  organization_id
  provider_model_id
  variant_key
  capability_snapshot_hash
  verification_status
  evidence_type
  evidence_uri / test_run_id
  decision: approved | revoked
  decided_by
  decided_at
  supersedes_attestation_id
~~~

`official/tested` 的自动 attestation 也必须记录证据来源；`inferred` 没有 active approved attestation 时返回 `MODEL_CAPABILITY_APPROVAL_REQUIRED`。

### 9.4 兼容策略

项目新增：

~~~text
compatibilityPolicy:
- strict
- compatible_fallback
~~~

- `strict`：业务模型绑定没有兼容 variant 时阻止执行。
- `compatible_fallback`：只在当前业务模型绑定的启用 fallback 中选择兼容模型。
- 禁止静默把分镜板、多模态或首尾帧降级成单帧图生视频。
- 用户显式更改生产方案后才能发生语义降级。

## 10. 锚点式跨镜头连续性

### 10.1 核心原则

跨镜头连续性不是像素连续，而是选定状态维度的连续。

需要保持的维度可能包括：

- 角色身份
- 当前服装和妆发版本
- 道具持有状态
- 伤势、污渍和破损状态
- 场景时间、天气和光线
- 屏幕方向
- 视线关系
- 动作阶段

通常必须重置的维度可能包括：

- 摄影机位置
- 景别
- 镜头角度
- 构图
- 当前画面人物集合
- 角色在画面内的具体位置
- 背景可见区域

### 10.2 ShotStateContract

每个镜头至少保存计划入口和计划出口两个状态版本：

~~~json
{
  "scene": {
    "assetId": "uuid",
    "variantId": "uuid",
    "timeOfDay": "dusk",
    "weather": "light_rain",
    "lighting": "cool_backlight"
  },
  "characters": [
    {
      "assetId": "uuid",
      "appearanceVersionId": "uuid",
      "costumeVariantId": "uuid",
      "pose": "standing",
      "expression": "guarded",
      "blocking": {
        "horizontal": "left",
        "depth": "foreground",
        "facing": "screen_right",
        "eyelineTargetAssetId": "uuid"
      }
    }
  ],
  "props": [
    {
      "assetId": "uuid",
      "state": "held",
      "holderAssetId": "uuid"
    }
  ],
  "camera": {
    "shotSize": "medium",
    "angle": "eye_level",
    "axisSide": "A",
    "lensIntent": "normal",
    "movement": "slow_dolly_in"
  },
  "action": {
    "entry": "角色刚转向对手",
    "exit": "角色停在对峙姿态"
  },
  "screenDirection": "left_to_right"
}
~~~

结构化枚举由 Go validator 校验；自然语言只用于补充描述。

### 10.3 ShotTransition

镜头之间不再共享模糊连续组，而是创建显式有向边：

| transitionType | 定义 | 前序尾帧策略 | 当前锚点策略 |
| --- | --- | --- | --- |
| `match_action_cut` | 动作在切镜点前后连续 | soft | 新锚点必须匹配动作阶段 |
| `same_scene_cut` | 同场景常规切镜 | soft 或 none | 当前机位新锚点 |
| `camera_cut` | 明确改变机位、景别或轴侧 | none | 当前机位新锚点 |
| `subject_change` | 人物进入、退出或焦点人物变化 | none | 按新人物集合生成 |
| `scene_cut` | 地点发生变化 | none | 新场景锚点 |
| `time_jump` | 时间、天气或主要光线发生跳转 | none | 新环境锚点 |
| `montage_cut` | 蒙太奇或快速并列镜头 | none | 每镜头独立锚点 |
| `unclassified` | 无法可靠判断 | none | 安全重置 |

真正不切镜的连续运动不应建成两个 Storyboard Shot。它应是一个 Storyboard Shot 下的多个 Render Segment。

### 10.4 确定性分类优先

Agent 可以提出 transitionType，但最终分类必须经过规则校验：

1. `scene.assetId` 改变，强制 `scene_cut`。
2. 时间、天气或主要光线状态改变，至少 `time_jump`。
3. 人物集合改变，至少 `subject_change`。
4. camera axis、机位或景别改变，至少 `camera_cut` 或 `same_scene_cut`。
5. 只有动作阶段连续且其他状态允许时，才可 `match_action_cut`。
6. 规则和 Agent 冲突时采用更保守的 reset 类型。

### 10.5 Carry 与 Reset

每条 transition 保存：

~~~json
{
  "carry": [
    "character.identity",
    "character.costume",
    "prop.state",
    "scene.weather"
  ],
  "reset": [
    "camera",
    "character.blocking",
    "frame.composition"
  ],
  "tailPolicy": "soft",
  "confidence": 0.92
}
~~~

`tailPolicy` 只允许：

~~~text
soft
none
~~~

跨 Storyboard Shot 不允许 `hard`。

### 10.6 视觉锚点优先级

当前镜头视频输入优先级固定为：

1. 当前镜头已审核 VisualAnchor。
2. 当前镜头 canonical/derived asset references。
3. 当前场景空间和环境参考。
4. 当前 transition 允许的前序 `continuity_hint`。
5. 风格和导演手册。

前序视频尾帧永远不能排在当前镜头 VisualAnchor 之前。

## 11. 视觉锚点生成与审核

### 11.1 Anchor 类型

~~~text
planned_first_frame
planned_last_frame
storyboard_sheet
storyboard_panel
observed_tail_frame
continuity_hint
~~~

### 11.2 图片生成

对于只能使用单图片的视频模型，图片生成阶段承担多资产合成：

~~~text
当前镜头状态
+ 当前角色参考
+ 当前场景参考
+ 当前道具参考
+ 当前机位和站位
+ 可选前序 continuity_hint
→ GPT-image-2
→ 当前镜头干净首帧
~~~

这一步把“视频模型无法同时接收所有人物和场景参考”的问题转化为可审核的图片合成问题。

### 11.3 Anchor Review

锚点审核输出严格 JSON：

~~~json
{
  "approved": false,
  "checks": {
    "requiredCharacters": "failed",
    "unexpectedCharacters": "passed",
    "scene": "passed",
    "costumeAndProps": "passed",
    "camera": "failed",
    "blockingAndEyelines": "failed",
    "style": "passed",
    "textLeakage": "passed"
  },
  "issues": [
    {
      "code": "MISSING_REQUIRED_CHARACTER",
      "assetId": "uuid",
      "message": "缺少当前镜头要求的角色"
    }
  ]
}
~~~

审核规则：

- 硬失败不得进入视频生成。
- 最多自动修正指定次数，超过后进入人工处理。
- 审核必须引用当前 `ShotStateContract` 和 Reference Pack manifest。
- 审核 Agent 只提出问题；确定性代码决定是否达到执行门槛。

### 11.4 实际输出审核

视频成功转存后：

1. Media Worker 提取代表性中间帧和尾帧。
2. 审核 `ObservedExitState` 是否符合 `PlannedExitState`。
3. 不符合时，当前视频标记 `needs_regeneration`。
4. 不合格尾帧不得成为后续 continuity hint。
5. 若用户接受偏差，则创建新的 ShotState revision，并使受影响下游锚点、Render Plan、视频和成片 stale。

生成结果不能静默修改计划。

## 12. Reference Pack

### 12.1 参考角色

canonical role：

~~~text
first_frame
last_frame
storyboard_sheet
character_identity
character_costume
scene_identity
scene_spatial
prop_identity
continuity_hint
motion_reference
video_reference
video_extension_source
audio_reference
style_reference
~~~

`previous_segment_tail` 是引用来源/provenance，不是新的供应商输入角色：当 continuation contract 为 `first_frame` 时，尾帧 Artifact 仍以 canonical role `first_frame` 发送，并在 item metadata 记录 `sourceRole=previous_segment_tail`。当 continuation contract 为 `video_extension` 时，前一 Segment 视频使用 `video_extension_source`；不得退化为普通 `video_reference`。

### 12.2 解析规则

Reference Resolver 输入：

- 当前 ShotState revision
- 当前 Shot 的 AssetRequirements
- active canonical asset references
- 当前镜头 derived asset references
- transition carry/reset
- VideoProductionProfile snapshot
- 目标模型 input contract

规则：

1. 默认只使用 active、fresh、未 archived 的参考。
2. 不把历史生成版本混入自动选择。
3. 当前镜头明确要求的人物和场景必须存在，否则阻止执行。
4. 超过模型上限时按 required、primary、derived、continuity、style 顺序裁剪。
5. 裁剪后若丢失 required 角色或场景，返回 `REFERENCE_PACK_INCOMPLETE`，不得继续。
6. 每个 item 保存来源资产、artifact、media file、引用角色、优先级和内容 hash。

### 12.3 Manifest

~~~json
{
  "profileKey": "multimodal_reference",
  "shotStateRevision": 3,
  "items": [
    {
      "referenceKey": "shot_first_frame:uuid",
      "role": "first_frame",
      "required": true,
      "priority": 1000,
      "artifactId": "uuid",
      "contentHash": "sha256:..."
    }
  ],
  "manifestHash": "sha256:..."
}
~~~

## 13. 数据模型

本轮不兼容旧项目视频生产数据，但必须通过定向迁移建立新 schema。禁止通过重建整个开发数据库规避迁移，因为供应商、模型和业务模型绑定必须保留。

### 13.1 video_production_profiles

Profile family 只表达稳定身份，不保存版本可用性：

~~~text
id
profile_key
name
strategy_family
description
created_at
updated_at
~~~

`profile_key` 全局唯一。family 不保存 `status`、`availability`、Prompt 或能力配置，避免与 version 形成两份真相。

### 13.2 video_production_profile_versions

~~~text
id
profile_id
version
lifecycle_state             draft | published | retired
implementation_state        reserved | available
configuration jsonb
capability_requirements jsonb
prompt_contract jsonb
input_contract_version
configuration_hash
prompt_contract_hash
created_by
created_at
published_at
retired_at
~~~

唯一约束为 `(profile_id, version)`。已进入 `published` 的 version 内容不可原地修改；配置、能力或 Prompt Contract 变化必须新建 version。只有 `published + available` 可用于项目创建、重建和 Workflow 启动。

### 13.3 project_video_production_bindings

~~~text
id
project_id
profile_version_id
status                       active | superseded
compatibility_policy
overrides jsonb
profile_snapshot jsonb
profile_snapshot_hash
revision
created_by
created_at
superseded_by_rebuild_id
superseded_at
~~~

Binding 是 append-only 记录，不允许普通 `UPDATE profile_version_id`。同一项目只能有一个 `status='active'` 的 binding，历史 binding 标记为 `superseded`。`profile_snapshot` 是 version、项目 overrides、手册绑定和 Prompt Contract 的规范化快照，Workflow 只能引用该快照和 hash，不能在运行中重新读取可变配置。

### 13.4 project_video_production_generations

~~~text
id
organization_id
project_id
binding_id
generation_no                单项目单调递增 bigint
status                       preparing | active | superseded | failed
source_generation_id
rebuild_id
created_at
activated_at
superseded_at
~~~

`(project_id, generation_no)` 唯一，同一项目只能有一个 active generation。`projects.active_video_production_generation_id` 和 `projects.video_production_generation_no` 保存当前写入栅栏；它们只允许在项目创建或重建切换事务中更新。

项目创建时原子创建 binding 和 generation。重建切换时在同一事务内：锁定项目、supersede 旧 binding/generation、归档旧活动计划、创建目标 binding、激活新 generation，并递增 `generation_no`。切换后不得回滚激活旧 generation；新分镜失败时在新 generation 内重试。

### 13.5 project_video_production_rebuilds

这是项目级 Saga header，不保存单一分镜计划 ID：

~~~text
id
organization_id
project_id
source_binding_id
source_generation_id
target_profile_version_id
target_binding_id
target_generation_id
status                       planned | approved | running | partial_succeeded | storyboard_required | succeeded | failed | cancelled
impact_snapshot jsonb
episode_count
retained_asset_count
workflow_run_id
idempotency_key
requested_by
requested_at
started_at
completed_at
failure_code
failure_message
~~~

### 13.6 project_video_production_rebuild_items

每个分集一个 durable item：

~~~text
id
rebuild_id
project_id
script_episode_id
source_storyboard_plan_id
target_storyboard_plan_id
workflow_run_id
status                       pending | running | succeeded | failed | skipped
checkpoint jsonb
attempt_count
started_at
completed_at
failure_code
failure_message
~~~

`(rebuild_id, script_episode_id)` 唯一。Impact 阶段冻结待重建分集集合；每集独立生成、提交和重试。部分分集成功、部分失败时 header 为 `partial_succeeded` 或 `storyboard_required`，已成功分集不得重复调用供应商。项目级重建只有在所有非 skipped item 成功后才能为 `succeeded`。

待重建集合来自项目当前生产使用的 active script version，并按持久化 episode ordinal 冻结 `scriptEpisodeId/revision/contentHash`；不得按标题推断集号，也不得在 rebuild 运行中自动吸收后来新增或改写的分集。剧本发生变化时由新的 source/script stale 规则和显式重建处理。

### 13.7 storyboard_shot_state_versions

~~~text
id
organization_id
project_id
storyboard_shot_id
state_role
revision
status
state jsonb
state_hash
source_type
source_id
prompt_version_id
provider_call_id
model_id
created_by
created_at
approved_at
~~~

`state_role`：

~~~text
planned_entry
planned_exit
observed_exit
~~~

### 13.8 storyboard_shot_transitions

~~~text
id
organization_id
project_id
storyboard_plan_id
source_shot_id
target_shot_id
transition_type
tail_policy
anchor_policy
carry_constraints jsonb
reset_constraints jsonb
confidence
revision
status
review_status
metadata jsonb
created_at
updated_at
~~~

约束：

- source 和 target 不得相同。
- target 在同一 active StoryboardPlan 中最多有一个 active predecessor。
- transition graph 必须无环并符合 shot order。
- tail_policy 只允许 `soft/none`。

### 13.9 shot_visual_anchors

~~~text
id
organization_id
project_id
storyboard_shot_id
shot_state_version_id
anchor_role
revision
status
review_status
artifact_id
media_file_id
storage_key
prompt
prompt_version_id
prompt_hash
provider_call_id
model_id
reference_pack_id
metadata jsonb
created_at
updated_at
~~~

### 13.10 shot_reference_packs

~~~text
id
organization_id
project_id
storyboard_shot_id
profile_snapshot_hash
shot_state_hash
capability_snapshot_hash
manifest jsonb
manifest_hash
status
created_at
~~~

`shot_reference_pack_items` 保存每个引用 item，避免运行时反复解析 JSON。

### 13.11 provider_model_capability_attestations

该表独立于 Provider 配置表，记录某个精确能力快照能否参与自动生产：

~~~text
id
organization_id
provider_model_id
variant_key
capability_snapshot_hash
verification_status
evidence jsonb
decision
decided_by
decided_at
supersedes_attestation_id
revoked_at
~~~

唯一 active 决策按 `organization_id + provider_model_id + variant_key + capability_snapshot_hash` 约束。能力配置发生任何变化都会产生新 snapshot hash，旧 attestation 只保留审计，不自动继承。

### 13.12 video_prompt_plans

每个镜头的 Prompt Agent 输出和审核结果保存为不可变版本：

~~~text
id
organization_id
project_id
production_generation_id
storyboard_shot_id
revision
prompt_context_plan_id
prompt_context_plan_hash
production_profile_version_id
production_profile_snapshot_hash
shot_state_hash
reference_pack_id
reference_pack_hash
input_contract_version
rendered_prompt
rendered_prompt_hash
dialogue_cues jsonb
native_audio_required
review_status
review_output jsonb
status
created_at
approved_at
~~~

`approved` 版本的正文、上下文、对白、hash 和 provenance 不可原地修改；重新生成或人工编辑必须创建新 revision，并把旧版本标记为 superseded/stale。视频执行只接收 `video_prompt_plan_id`，不接收浏览器临时 Prompt。

一个已审核镜头 Prompt 可以对应多个 Render Segment。Segment Prompt 由确定性代码按 segment 时间范围裁剪动作描述和逐字对白 cue，保存 `source_video_prompt_plan_id` 和派生 hash；该步骤不得调用 Prompt Agent，也不得改写台词。

### 13.13 video_render_plans

现有表补充：

~~~text
production_profile_snapshot jsonb
production_profile_snapshot_hash
shot_state_revision
shot_state_hash
transition_snapshot jsonb
reference_pack_id
reference_pack_hash
initial_input_contract_snapshot jsonb
initial_input_contract_hash
continuation_input_contract_snapshot jsonb
continuation_input_contract_hash
capability_attestation_id
video_prompt_plan_id
prompt_context_plan_id
prompt_context_plan_hash
production_generation_id
~~~

Render Plan 的幂等 key 至少包含：

~~~text
shotId
shotStateHash
profileSnapshotHash
referencePackHash
videoPromptHash
capabilitySnapshotHash
initialInputContractHash
continuationInputContractHash
targetDurationTicks
productionGenerationId
~~~

对单 Segment 镜头，`continuation_input_contract_snapshot` 可以为空。对多 Segment 镜头，该字段必须非空，且每个 `video_render_segment` 保存实际采用的 contract key/hash：首段只允许 initial contract，后续段只允许 continuation contract。

### 13.14 episode_video_production_checkpoints

长任务业务进度不能只存在 Temporal history。新增 header/batch/item 三层 checkpoint：

~~~text
episode_video_production_checkpoints
  id, project_id, production_generation_id, binding_id, binding_revision
  episode_id, workflow_id, current_run_id, profile_snapshot_hash
  status, next_batch_ordinal, revision, created_at, updated_at

episode_video_production_batches
  id, checkpoint_id, ordinal, dependency_snapshot_hash
  workflow_id, workflow_run_id, status, attempt
  total_items, succeeded_items, failed_items, cancelled_items
  started_at, completed_at

episode_video_production_items
  id, batch_id, storyboard_shot_id, shot_state_hash
  reference_pack_id, video_prompt_plan_id, video_render_plan_id
  status, attempt, provider_async_task_id, error_code, error_detail
  started_at, completed_at
~~~

所有状态更新使用 revision CAS 和 generation fence。`partial_succeeded` 是可恢复终态；只重试 failed/cancelled item，不重跑 succeeded item。Continue-As-New 提交新 run ID 前，必须先持久化 `next_batch_ordinal`、所有 live Child/Provider task 状态和剩余 item 集合。

### 13.15 项目级 Generation Fence

以下表必须新增不可空的 `production_generation_id`，或在不直接属于项目生产代时通过其父记录强制关联：

~~~text
workflow_runs
storyboard_plans
storyboard_shots
storyboard_shot_state_versions
storyboard_shot_transitions
shot_asset_requirements
shot_visual_anchors
shot_reference_packs
video_prompt_plans
video_render_plans
video_render_segments
provider_async_tasks
镜头级 artifact/media binding
~~~

`provider_call_logs` 和 `cost_records` 增加可空的 `production_generation_id` 用于项目调用溯源；非项目调用允许为空。它们是 append-only 审计，不参与业务 CAS。项目视频 `provider_async_tasks.production_generation_id` 必须非空并参与 fence。

所有 Activity、API、Agent tool、Provider 回调和媒体转存写入必须同时校验：

~~~text
projectId
productionGenerationId
bindingId
bindingRevision
workflowRunId / nodeRunId
业务对象 expected revision
~~~

提交事务必须通过 `projects.active_video_production_generation_id = :productionGenerationId` 和 active binding revision 的 CAS 条件。现有 Workflow/Node writable fence 继续保留，但它只是第二层保护，不能替代项目 generation fence。旧 generation 的 Activity 即使在 Worker 重启、网络恢复或 Provider 回调后成功，也只能写审计终态并返回 `WORKFLOW_RESULT_DISCARDED`，不得更新活动 Storyboard、Anchor、Render Plan、媒体绑定或 ProductionStatus。

Provider Gateway 的 planner/create/cancel 请求和 `provider_async_tasks` 必须保存 generation identity。异步结果转存前、转存后绑定业务对象前各校验一次；已产生的 `provider_call_logs`、`cost_records` 和原始 Artifact 可以保留审计，但不能挂接到新 generation。

### 13.16 资产和镜头产物保留边界

重建中的“清空”统一解释为从活动 generation 归档，不执行跨表硬删除：

- `canonical_assets`、`asset_references`、`asset_versions` 保持 active，可被新 generation 重新解析。
- 旧 `storyboard_plans`、`storyboard_shots`、`shot_asset_requirements`、镜头衍生 Artifact、Visual Anchor、Reference Pack、Prompt、Render Plan、镜头媒体、Timeline 和 Final Video 标记为旧 generation 历史，只从默认查询排除。
- 旧 generation 的衍生资产和镜头参考不得自动进入新 generation 的 Reference Pack；只有 canonical asset 及其当前有效参考可重新解析。
- 不物理删除旧 Storyboard Shot。当前 `shot_asset_requirements.storyboard_shot_id` 使用 `ON DELETE CASCADE`，硬删除会丢失影响分析和历史关联。
- 对象存储文件继续保留，直到独立的 retention/GC 策略根据数据库引用和保留期安全回收；重建 Workflow 不直接删除对象。

### 13.17 删除和替换

删除：

- `projects.production_mode`
- `storyboard_shots.continuity_group_id`
- 尾帧专用的 `storyboard_shot_continuity_frames`
- 把多个 Storyboard Shot 串成硬尾帧链的 Workflow 路径

替换：

- `storyboard_shot_continuity_frames` 替换为通用 `shot_visual_anchors`。
- `continuity_group_id` 替换为 `storyboard_shot_transitions`。
- `previous_last_frame` 仅保留在同镜头 `video_render_segments.continuity_mode` 中，并重命名为 `previous_segment_tail`。

### 13.18 迁移保护数据

以下配置数据是硬保护边界，不属于“不做旧数据兼容”的清理范围：

~~~text
provider_accounts
provider_connectors
provider_credentials
provider_endpoints
provider_models
provider_model_capabilities
provider_limit_policies
model_profiles
model_profile_bindings（包括 priority、weight、enabled、runtime_options）
~~~

迁移窗口冻结 Provider 管理配置写入。对上述配置表，迁移前后必须按组织验证主键集合、行数和规范化非敏感配置 hash 完全一致；密文不解密，只比较主键、key reference 和密文 hash。迁移脚本不得 truncate、重新 seed、更新或通过级联删除触达这些表。

`provider_call_logs`、`cost_records`、`provider_async_tasks` 是 append-only 或运行历史，迁移期间可能新增行，因此验收使用“迁移前主键集合是迁移后集合的子集”，不得要求行数或全表 hash 完全相等。`provider_leases`、`provider_circuit_states` 等瞬态运行表不参与配置等值比较，但发布前必须确认没有 active video task/lease 被误接管。

若 Provider capability schema 为新 Input Contract 增加字段，只允许 additive migration；不得用模型名称或旧布尔字段批量猜测并回填强能力。现有模型缺少新字段时保持原配置不变，其 variant 标记为 `inferred/unknown`，通过受控验证和独立 attestation 进入自动生产，不覆盖管理员自定义值。

## 14. 分镜规划重构

### 14.1 Episode Blueprint

Episode Blueprint 继续分批工作，但输出扩展为：

- 场景入口和出口状态
- 场景内不变量
- 镜头切换建议
- 人物和道具状态演进
- 屏幕方向和视线约束
- reset boundary

### 14.2 Shot Planner

`ShotPlannerSuggestion` 需要输出结构化：

~~~text
plannedEntryState
plannedExitState
transitionFromPrevious
assetRequirements
camera
blocking
imagePromptDirection
videoPromptDirection
~~~

移除 `ContinuityGroupKey`。

### 14.3 Deterministic Canonicalizer

在 Agent 输出后：

1. 使用数据库中的 canonical asset ID 校验人物、场景和道具。
2. 按剧本和前后镜头顺序规范化角色状态。
3. 根据 scene、cast、camera 差异重算 transitionType。
4. 验证所有 required asset 均进入当前镜头。
5. 验证 planned entry/exit 可被当前镜头时长覆盖。
6. 生成稳定 state hash 和 transition hash。

### 14.4 Reviewer

Reviewer 重点检查：

- 剧本事实和中文台词是否忠实。
- 人物出入画是否正确。
- 空间轴、视线和屏幕方向是否合理。
- 镜头入口和出口状态是否闭合。
- 是否错误地把切镜规划成连续变形。
- 是否遗漏 reset boundary。

## 15. Temporal Workflow 设计

### 15.1 分层编排，不复制四套 Workflow

目标层级：

~~~text
ProjectVideoProductionRebuildWorkflow       仅用于项目方案重建
  -> EpisodeVideoProductionWorkflow         每个分集一个长期业务单元
     -> SceneOrShotBatchWorkflow            有界场景/镜头批次
        -> shot Activities                   默认执行单镜头锚点、计划、Provider task 和审核
        -> bounded Shot Workflow             仅在确有独立长生命周期价值时使用
~~~

正常分集生产直接启动 `EpisodeVideoProductionWorkflow`；项目重建 Workflow 只负责编排第 13.6 节的 episode items，不承载所有镜头历史。Profile 只改变 Compiler、Anchor Strategy、Prompt Contract、Input Contract Adapter 和 Reviewer，不复制状态机。

不得为每个镜头无条件创建一个 Anchor Child Workflow 再创建一个 Video Child Workflow。70 分钟分集可能包含数百镜头，双 Child 结构会放大 Parent history、并发和可观测性成本，并接近 Temporal 对单个 Parent Child Workflow 数量的实践边界。

### 15.2 EpisodeVideoProductionWorkflow

阶段：

~~~text
LoadGenerationAndProfileSnapshot
LoadStoryboardAndStateContracts
ValidateModelBindings
CompileShotDependencyGraph
CompilePromptContextPlans
PartitionSceneOrShotBatches
ExecuteReadyBatches
ReconcilePartialResults
FinalizeEpisodeProduction
~~~

Episode Workflow 只传 ID、hash、批次状态和 checkpoint，不传完整 Prompt、剧本或媒体 URL。每个批次必须与场景边界、reset boundary 和真实依赖图对齐；不能只按固定镜头数量粗暴切断依赖。分批算法同时受以下上限约束：

- 单批镜头数量配置上限。
- 预计 Workflow history event 上限。
- 并发 Provider lease 上限。
- 单个 payload 和 memo/search attribute 上限。

### 15.3 SceneOrShotBatchWorkflow

一个 Batch 在自身有界历史内完成：

1. 解析每个镜头的 typed Reference Pack。
2. 生成并审核首帧、尾帧或分镜板，成功后立即写库和事件。
3. `prompt` 类型 Batch 显式编译、生成并审核 Prompt；`video` 类型 Batch 只能加载已审核 Prompt。两种 Batch 使用不同 command/type，不得在同一执行分支中按“缺 Prompt 就顺便生成”降级。
4. 编译不可变 Render Plan。
5. 创建、轮询、取消和转存 Provider 异步任务。
6. 执行同镜头 Render Segment 依赖并写入镜头视频结果。
7. 审核 ObservedExitState，提交批次 checkpoint 和逐镜头终态。

镜头默认通过可重试 Activity 和数据库状态机形成独立失败单元。只有当单镜头生命周期需要独立 Signal、Query、取消域或超长 history 时才启用 bounded `ShotVideoRenderWorkflow`；不得把它作为所有镜头的固定拓扑。无论使用 Activity 还是 Child Workflow，幂等 key 和业务写入都必须包含 `productionGenerationId`。

Batch 调度采用依赖图 ready-set：每轮只调度依赖已满足的 item，受 `maxConcurrentShots` 和 Provider lease 双重限制。每个 item 在 Activity 返回后立即写 checkpoint；单项失败只把 Batch 聚合为 `partial_succeeded`，不会把已成功项回滚成失败。失败重试创建新 attempt，但沿用同一业务 item 和幂等 key。

### 15.4 并发规则

- 无未完成前置锚点依赖的镜头可以并发。
- `camera_cut`、`scene_cut`、`time_jump`、`subject_change` 默认建立新并发边界。
- `match_action_cut` 可等待前序 ObservedExitState，但只能把它用于生成当前锚点，不把尾帧硬塞入视频。
- 同一 Storyboard Shot 的 Render Segment 严格串行。
- 一个镜头失败只阻塞依赖其实际出口状态的镜头；下一个 reset boundary 后继续执行。
- 默认并发由 Workflow options 和 Provider lease 双重限制，Provider Gateway 仍是最终并发控制方。

### 15.5 长任务

- 每集、每镜头、每锚点均是独立可重试单元。
- 每完成一个锚点、Render Plan 或视频立即写库，不依赖 Workflow 最终返回。
- Workflow ID 包含项目、`productionGenerationId`、分集、批次或镜头、状态 revision 和 profile hash；重试沿用业务幂等 key，不依赖随机 ID 去重。
- Continue-As-New 由 `workflow.GetInfo(ctx).GetContinueAsNewSuggested()`、当前 history 大小和剩余批次共同决定，不使用固定运行分钟数或固定总镜头数作为唯一阈值。
- Parent 在 Continue-As-New 前必须停止调度新 Child，等待当前 Child 全部终态或按取消策略完成 drain，并把 Child Workflow ID、episode item、批次和 Provider task 状态持久化。禁止带着仍运行的 Child 直接 Continue-As-New。
- 新 run 只加载 checkpoint ID、generation/binding snapshot hash 和剩余 item ID，不携带大 Prompt、剧本或媒体 URL。
- 取消时只取消当前活跃 Provider task，不删除已完成产物。
- 重试时复用 idempotent 成功节点，不重复调用供应商。
- 项目重建支持分集级 `partial_succeeded`；只重试 failed item，不重跑成功分集。

### 15.6 Worker Versioning 与升级

当前基线为 Temporal Go SDK `1.37.0` 和 Temporal Server `1.31.2`。视频生产和生产方案重建属于小时级长任务，必须纳入 Worker Deployment Versioning：

- `ProjectVideoProductionRebuildWorkflow`、`EpisodeVideoProductionWorkflow` 和 Batch Workflow 使用 `Pinned` 行为，固定到启动它们的 Worker Deployment Version。
- 每次不兼容 Workflow 代码变更发布新的 deployment/build ID，旧版本 Worker 保留到对应 Workflow 和 Provider task 全部 drain。
- 不允许新旧 Worker 同时写同一 generation 的不同状态机版本；generation fence 和 binding snapshot 是最后防线。
- 发布前对当前版本和 N-1 版本执行 replay tests；升级不能依赖开发库不兼容作为跳过 Workflow replay 的理由。
- Workflow search attributes 至少包含 `ProjectId`、`ProductionGenerationId`、`EpisodeId`、`ProfileVersionId` 和 `RebuildId`，便于部署排空和故障恢复。

## 16. Provider Gateway 边界

### 16.1 Planner 请求

Workflow 向 Provider Gateway 提交语义需求，不提交供应商特定字段：

~~~json
{
  "projectId": "uuid",
  "productionGenerationId": "uuid",
  "bindingId": "uuid",
  "bindingRevision": 1,
  "workflowRunId": "uuid",
  "productionProfileKey": "multimodal_reference",
  "productionProfileVersionId": "uuid",
  "storyboardShotId": "uuid",
  "shotStateRevision": 3,
  "shotStateHash": "sha256:...",
  "transitionHash": "sha256:...",
  "referencePackId": "uuid",
  "referencePackHash": "sha256:...",
  "videoPromptPlanId": "uuid",
  "videoPromptPlanHash": "sha256:...",
  "promptContextPlanId": "uuid",
  "promptContextPlanHash": "sha256:...",
  "requiredInitialInputContract": "first_frame",
  "allowedContinuationInputContracts": ["video_extension", "first_frame"],
  "compatibilityPolicy": "strict",
  "targetDurationTicks": 720000,
  "timelineTimebase": 90000,
  "aspectRatio": "16:9",
  "audioStrategy": "native_av",
  "audioRequirement": "required",
  "nativeAudioRequired": true,
  "dialogueCues": [
    {
      "speaker": "角色名",
      "text": "必须逐字保留的中文台词",
      "startTick": 90000,
      "endTick": 360000
    }
  ]
}
~~~

Gateway 负责：

- 按 ID 从数据库重新加载 active generation、binding、Profile、ShotState、Transition、ReferencePack、PromptContextPlan 和 approved VideoPromptPlan；客户端 hash 只用于并发冲突检测，不作为权威内容。
- 选择业务模型绑定。
- 选择兼容 provider model variant。
- 验证 capability verification/attestation，并冻结 attestation ID 和 capability snapshot。
- 验证 duration、分辨率、比例和音频能力。
- 分别验证 initial/continuation 输入 slot、数量、媒体 MIME、角色和互斥规则。
- 验证 Prompt/context 限制、`dialogueCues` 时长和 `nativeAudioRequired`；需要原生音频时不得路由到无音频输出能力的 variant。
- 生成 capability snapshot。
- 返回 canonical Render Plan。

Planner 不能假定“模型支持首帧”就必然支持长镜头续接。目标时长超过单次模型时长时，必须选择并冻结以下一种合法策略：

1. `video_extension`：上游明确支持前一 Segment 视频延长，且 Adapter 有已验证映射。
2. `first_frame`：Media Worker 从前一 Segment 提取 fresh 尾帧，后续 Segment 作为新的单首帧请求。
3. 无合法 continuation contract：返回 `RENDER_PLAN_REPLAN_REQUIRED`，由上层缩短镜头、调整分镜或切换兼容模型。

不得把前一 Segment 视频以普通 `video_reference` 发送并声称完成了时间续接。

### 16.2 Create 请求

Gateway create runtime 接收已编译的 canonical reference roles，并由 provider adapter 转换：

~~~json
{
  "renderPlanId": "uuid",
  "projectId": "uuid",
  "productionGenerationId": "uuid",
  "bindingId": "uuid",
  "bindingRevision": 1,
  "workflowRunId": "uuid",
  "storyboardShotId": "uuid",
  "segmentIndex": 0,
  "videoPromptPlanId": "uuid",
  "promptContextPlanId": "uuid",
  "prompt": "...",
  "promptHash": "sha256:...",
  "promptContextPlanHash": "sha256:...",
  "inputContractHash": "sha256:...",
  "referencePackId": "uuid",
  "referencePackHash": "sha256:...",
  "audioStrategy": "native_av",
  "nativeAudioRequired": true,
  "dialogueCues": [
    {
      "speaker": "角色名",
      "text": "必须逐字保留的中文台词"
    }
  ],
  "references": [
    {
      "role": "first_frame",
      "artifactId": "uuid"
    }
  ]
}
~~~

后续 Segment 的请求示例：

~~~json
{
  "renderPlanId": "uuid",
  "segmentIndex": 1,
  "videoPromptPlanId": "uuid",
  "prompt": "由 approved shot prompt 确定性派生的本段提示词",
  "promptHash": "sha256:...",
  "inputContractHash": "sha256:...",
  "references": [
    {
      "role": "video_extension_source",
      "artifactId": "previous-segment-video-uuid"
    }
  ]
}
~~~

`dialogueCues` 是 canonical request 的结构化硬约束，不只是 Prompt 中的一段可丢弃文本。Adapter 必须按上游协议映射原生音频、对白和参考输入；如果上游只能从 Prompt 接收对白，Adapter 负责使用经过 contract test 的格式注入，并验证实际发送文本仍逐字包含所有 cue。任何丢失、改写或截断返回 `VIDEO_DIALOGUE_CONTRACT_VIOLATION`。

Gateway 在创建 `provider_async_tasks` 前再次校验项目 generation；异步结果完成后只写 Provider 审计和媒体暂存，业务挂接由带 generation CAS 的提交步骤完成。旧 generation 的完成回调不得覆盖新分镜或新视频状态。

Create runtime 必须从 Render Plan 重新加载该 `segmentIndex` 的权威 Prompt/hash、dialogue cues、Input Contract 和引用角色，并与请求逐字段核对：

- `segmentIndex=0` 只允许 initial contract，图生视频模式必须恰好一个 `first_frame`。
- `segmentIndex>0` 只允许 Render Plan 冻结的 continuation contract；`video_extension_source` 必须来自同一镜头前一成功 Segment；以 `first_frame` 续接时，该引用的 `sourceRole=previous_segment_tail` 且必须来自前一 Segment 的 fresh 尾帧 Artifact。
- 实际发送的媒体 MIME 必须符合 slot 的 `mediaType`，不得只按引用数量校验。
- 实际 outbound Prompt 必须与已保存 segment Prompt hash 一致，并逐字包含该 Segment 的全部中文对白 cue。
- 任一 ID、revision、hash、角色、媒体来源或审批失配都在创建上游任务前失败，不产生供应商费用。

Worker 不得理解火山、xAI、OpenAI、Google 或其他供应商的字段名。

### 16.3 Adapter

每个 adapter 必须显式映射：

- canonical role 到上游字段
- image-as-first-frame 与 semantic reference 的区别
- mutually exclusive modes
- async create、poll、cancel
- output URL 转存
- 上游错误到平台错误

无法表达 canonical request 时返回 `MODEL_INPUT_CONTRACT_UNSUPPORTED`，不得丢弃引用后继续调用。

## 17. Public API 与 OpenAPI

### 17.1 项目生产方案

项目创建契约修改为：

~~~text
POST /api/projects
body.videoProductionProfileKey
body.videoProductionProfileVersion 可选；默认解析该 key 最新的 published + available version

response.videoProductionBinding.id
response.videoProductionBinding.profileVersionId
response.videoProductionBinding.profileSnapshotHash
response.productionGeneration.id
response.productionGeneration.generationNo
~~~

查询和重建接口：

~~~text
GET /api/video-production-profiles
GET /api/projects/{projectId}/video-production-profile
GET /api/projects/{projectId}/video-production-profile/compatibility
GET /api/projects/{projectId}/video-production-profile/rebuild-impact?targetProfileKey=...
POST /api/projects/{projectId}/video-production-profile/rebuild
GET /api/projects/{projectId}/video-production-profile/rebuilds/{rebuildId}
GET /api/projects/{projectId}/video-production-profile/rebuilds/{rebuildId}/items
POST /api/projects/{projectId}/video-production-profile/rebuilds/{rebuildId}/retry-failed
~~~

不得提供直接更新 Profile 的 `PUT/PATCH` 路由。`POST .../rebuild` 必须携带 `expectedProjectRevision`、`targetProfileKey`、可选 `targetProfileVersion`、确认过的 impact token 和 `Idempotency-Key`，并要求具备项目 destructive production 权限。

重建 Workflow 按固定顺序执行：

1. 锁定项目生产配置，拒绝新的分镜、镜头图片和镜头视频任务。
2. 再次校验 impact token、目标 Profile version 为 `published + available` 和模型兼容性。
3. 取消或等待当前项目的视频生产任务进入终态。
4. 按 Impact 快照为每个分集创建 rebuild item，并将各分集当前 active StoryboardPlan、shots、shot requirements、anchors、reference packs、prompts、render plans、shot media bindings、timeline 和 final video 归档到旧 generation。
5. 保留 canonical assets、asset references、asset versions、原文、剧本、供应商和模型配置；旧镜头衍生数据只保留历史，不作为新引用候选。
6. 在一个切换事务中 supersede 旧 binding/generation，创建目标 Profile 的新 active binding/generation，并递增项目 generation fence。
7. 将项目置为 `storyboard_required`，按 episode item 启动针对目标 Profile 的新分镜 Workflow。
8. 每集新分镜发布成功后提交该 item；部分失败时只重试失败分集。所有 item 成功后解锁生产；失败时保留目标 binding/generation，不恢复旧分镜。

“清空旧分镜”是活动数据语义：旧分镜不得继续出现在默认列表、ProductionStatus、Agent 上下文或生产入口中。审计记录可以归档保留，但不得与新方案混用。

### 17.2 镜头状态和转场

新增：

~~~text
GET /api/projects/{projectId}/storyboard-shots/{shotId}/state
GET /api/projects/{projectId}/storyboard-shots/{shotId}/transition
PATCH /api/projects/{projectId}/storyboard-shots/{shotId}/transition
POST /api/projects/{projectId}/storyboard-shots/{shotId}/state/replan
~~~

### 17.3 Visual Anchor

新增：

~~~text
GET /api/projects/{projectId}/storyboard-shots/{shotId}/anchors
POST /api/projects/{projectId}/storyboard-shots/{shotId}/anchors/generate
POST /api/projects/{projectId}/storyboard-shots/{shotId}/anchors/{anchorId}/approve
POST /api/projects/{projectId}/storyboard-shots/{shotId}/anchors/{anchorId}/reject
~~~

### 17.4 Reference Pack 和 Render Plan

新增只读诊断：

~~~text
GET /api/projects/{projectId}/storyboard-shots/{shotId}/reference-pack
GET /api/projects/{projectId}/storyboard-shots/{shotId}/video-prompt-plan
GET /api/projects/{projectId}/storyboard-shots/{shotId}/video-render-plan
POST /api/projects/{projectId}/video-prompts/generate-batch
POST /api/projects/{projectId}/video-prompts/{promptPlanId}/approve
POST /api/projects/{projectId}/video-prompts/{promptPlanId}/reject
POST /api/projects/{projectId}/shot-videos/generate-batch
~~~

正常用户看到可视化引用；技术字段放在折叠详情。

`generate-batch` 的 Prompt 与 Video 是两个不同命令：Prompt Batch 返回逐镜头 Prompt plan 状态；Video Batch 必须显式携带或解析 approved `videoPromptPlanId`，缺失/过期时按镜头返回失败项，不自动启动 Prompt Agent。

### 17.5 模型能力审批

新增组织级能力审批接口：

~~~text
GET /api/providers/models/{modelId}/video-capability-attestations
POST /api/providers/models/{modelId}/video-capability-attestations
POST /api/providers/models/{modelId}/video-capability-attestations/{attestationId}/revoke
POST /api/providers/models/{modelId}/video-capabilities/verify
~~~

`verify` 执行 Adapter fixture/受控探测并生成新的 capability snapshot 和证据，但不改写账号、密钥、模型启停状态或业务模型绑定。`attest/revoke` 要求 `provider_models.manage`，请求必须带当前 `capabilitySnapshotHash` 和理由；服务端拒绝为 stale hash 审批。

### 17.6 Agent 工具与 RBAC

新增高风险权限和工具：

~~~text
permission: project.video_production.rebuild
tool: project.video_production.rebuild
tool: project.video_production.rebuild.retry_failed
~~~

- Agent 只能通过公开的 impact、rebuild、status 和 retry API 执行，不得拥有直接更新 binding、generation 或 `projects.production_mode` 的工具。
- `require_approval` 模式必须在拿到最新 impact token 后向用户展示影响并等待批准；批准记录保存 target Profile version、项目 revision、generation 和 impact hash。
- `auto_approve` 只能在组织 supervision policy 明确允许该 destructive tool 时执行，仍需 impact token、RBAC 和项目锁。
- `full_access` 可以免去交互确认，但不能绕过 RBAC、impact token、expected revision、generation fence 或活动任务排空。
- 重建已进入 `running` 后，同一项目新的重建命令返回 `PRODUCTION_PROFILE_REBUILD_CONFLICT`；Agent 不得通过重复调用制造并行重建。

## 18. 前端产品设计

### 18.1 新建项目和项目设置

新建项目的“生成方式”移除原有四张旧卡片，使用以下四张 Profile 卡片：

- 图生视频
- 首尾帧衔接
- 多模态参考
- 分镜板

每张卡展示：

- 核心输入方式
- 适合的模型能力
- 当前业务模型是否兼容
- 原生音频状态
- 是否需要额外图片生成步骤

首个里程碑只允许选择图生视频；其余三张卡由服务端 Profile version 的 `lifecycleState/implementationState` 驱动为禁用占位。前端不得硬编码“已实现”，也不得让 reserved Profile 提交创建请求。没有兼容图生视频模型时允许保存项目草稿，但禁止启动视频生产，并提供“配置兼容模型”入口。

创建按钮提交 Profile key 后，服务端必须回显实际 binding；前端只有在回显与选择一致时才进入项目工作台。

项目设置中的生产方案改为只读摘要，显示名称、版本、可用模型和创建时间，不再显示可编辑 Input 或 Select。需要切换时使用单独的“重建生产方案”操作：

1. 先展示影响统计，包括将清空的分镜、镜头图、镜头视频、时间线和成片数量，以及明确保留的资产数量。
2. 用户选择一个 `available` 目标方案并二次确认。
3. 前端提交重建 Workflow，并在任务活动中持续展示清理、绑定、分镜重建和发布进度。
4. 重建期间项目相关生产按钮禁用，但内容、剧本和资产仍可查看。
5. 失败后提供“重试重建”，不提供绕过重建直接切换字段的入口。

### 18.2 分镜页面

每个镜头显示：

- 当前镜头入口锚点
- 当前镜头出口锚点或分镜板
- 与上一镜头的转场类型
- 继承维度和重置维度
- required 角色、场景和道具
- 锚点审核状态

允许用户修改：

- 转场类型
- 前序尾帧是否作为软参考
- 当前镜头参考素材
- 当前镜头入口和出口状态

修改后按 stale 规则重建下游。

### 18.3 视频页面

视频弹窗展示：

- 生产方案
- Render Plan variant
- 首帧、尾帧或分镜板
- Reference Pack
- 当前镜头状态契约
- 模型能力快照
- approved Prompt plan、PromptContextPlan 和审核版本
- initial/continuation Input Contract 与各 Segment 实际输入来源
- 实际出口状态审核
- 历史生成版本

视频页提供两个独立批量动作：“生成/审核视频提示词”和“生成视频”。后者只对 Prompt 状态为“已审核且最新”的镜头可用；缺失或 stale 的卡片展示明确中文原因，并允许回到 Prompt Batch 重试，不在生成视频按钮内隐式补跑 Agent。

### 18.4 任务活动

节点按真实顺序显示：

~~~text
状态规划
参考解析
锚点生成
锚点审核
提示词生成
提示词审核
视频计划
视频生成
媒体转存
出口状态审核
~~~

只有前一个真实依赖节点完成后才显示后续节点。并行镜头各自显示子任务，不伪装成单一串行进度。

### 18.5 供应商模型能力验证

供应商模型编辑页以可视化方式展示每个视频 variant 的 initial/continuation Input Contract、原生音频、时长、比例和验证状态：

- `official/tested` 显示证据来源和最近验证时间。
- `inferred` 显示“待组织管理员批准”，可查看快照差异后批准或拒绝。
- `unknown` 显示“不满足自动生产”，不能通过普通开关强行启用。
- 能力变化后旧审批显示为“已失效”，但历史调用和审批记录仍可查看。

批准、撤销和受控验证操作都使用独立 API；编辑模型表单不得因批准能力而覆盖供应商账号、模型 ID、优先级、权重或业务绑定。

## 19. Stale 与版本传播

### 19.1 Profile version 改变

Profile version 不允许原地修改。项目通过受控 rebuild 切换 version 时，不在旧 generation 内逐项打 stale，而是：

1. 把旧 generation 的 StoryboardPlan、Storyboard Shot、shot requirements、Reference Pack、Visual Anchor、视频 Prompt、Render Plan、镜头媒体、Timeline 和 Final Video 整体归档为历史。
2. 创建新的 binding/generation，并从剧本分集重新生成适配目标 Profile 的 StoryboardPlan 和 ShotState。
3. 原文、剧本、canonical assets、asset references 和 asset versions 保持活动状态。
4. 旧 generation 的衍生图、历史 Anchor 和媒体不自动进入新 Reference Pack。

因此“切换 Profile 但沿用旧 Storyboard Shot，只让视频 stale”不是允许的状态。相同 Profile version 内修改 ShotState、资产引用、Prompt 或模型能力时，继续按以下小节执行局部 stale 传播。

### 19.2 ShotState 改变

修改人物集合、场景、服装、道具、站位、机位、动作入口或出口后：

- 当前镜头所有锚点和视频 stale。
- 所有依赖该状态 carry 的后续 transition 重新校验。
- 只影响到下一个 reset boundary。

### 19.3 Asset 参考改变

- Reference Pack hash 改变。
- 依赖该引用的锚点 stale。
- 依赖锚点的 Render Plan 和视频 stale。
- 锁定的历史视频版本不删除，只标记来源过期。

### 19.4 接受实际偏差

用户点击“接受为新状态”时：

1. 从 ObservedExitState 创建新的 planned state revision。
2. 写审计事件和用户 ID。
3. 重算后续 transition。
4. 使受影响锚点和视频 stale。
5. 不修改 canonical asset。

## 20. 错误码、事件和可观测性

### 20.1 新错误码

~~~text
PRODUCTION_PROFILE_NOT_AVAILABLE
PRODUCTION_PROFILE_INCOMPATIBLE
PRODUCTION_PROFILE_IMMUTABLE
PRODUCTION_PROFILE_REBUILD_REQUIRED
PRODUCTION_PROFILE_REBUILD_CONFLICT
MODEL_INPUT_CONTRACT_UNSUPPORTED
MODEL_CAPABILITY_APPROVAL_REQUIRED
SHOT_STATE_INVALID
SHOT_TRANSITION_INVALID
REFERENCE_PACK_INCOMPLETE
VISUAL_ANCHOR_MISSING
VISUAL_ANCHOR_STALE
VISUAL_ANCHOR_REVIEW_FAILED
STORYBOARD_SHEET_INVALID
CONTINUITY_STATE_MISMATCH
OBSERVED_EXIT_REJECTED
RENDER_PLAN_STALE
RENDER_PLAN_REPLAN_REQUIRED
VIDEO_PROMPT_PLAN_MISSING
VIDEO_PROMPT_PLAN_STALE
VIDEO_CONTINUATION_CONTRACT_UNSUPPORTED
PRODUCTION_GENERATION_MISMATCH
PROMPT_CONTEXT_LIMIT_EXCEEDED
VIDEO_DIALOGUE_CONTRACT_VIOLATION
~~~

所有平台错误必须在 `error-localization.ts` 有中文映射；上游真实错误保留在技术信息和 Provider Call Log。

### 20.2 新事件

~~~text
project.video_production_binding.created
project.video_production_binding.superseded
project.video_production_generation.activated
project.video_production_generation.superseded
project.video_production_rebuild.requested
project.video_production_rebuild.started
project.video_production_rebuild.storyboard_required
project.video_production_rebuild.item.started
project.video_production_rebuild.item.completed
project.video_production_rebuild.item.failed
project.video_production_rebuild.partial_succeeded
project.video_production_rebuild.completed
project.video_production_rebuild.failed
storyboard.shot.state.planned
storyboard.shot.transition.planned
storyboard.shot.transition.updated
storyboard.shot.reference_pack.compiled
storyboard.shot.anchor.started
storyboard.shot.anchor.completed
storyboard.shot.anchor.failed
storyboard.shot.anchor.reviewed
storyboard.shot.observed_exit.reviewed
storyboard.shot.continuity.rejected
video.render_plan.compiled
video.render_plan.stale
provider.model_capability.attested
provider.model_capability.revoked
video.prompt_plan.generated
video.prompt_plan.approved
video.prompt_plan.rejected
video.prompt_plan.stale
video.production.batch.started
video.production.item.started
video.production.item.completed
video.production.item.failed
video.production.batch.partial_succeeded
video.production.checkpoint.committed
~~~

事件不得只定义名称。所有新事件必须先加入 `packages/events/catalog.yaml`，再生成类型化 catalog/validator；生产者和消费者不得各自维护匿名 payload。

统一 envelope 至少包含：

~~~text
schemaVersion
eventId
eventType
occurredAt
organizationId
projectId
aggregateType
aggregateId
status
~~~

视频生产事件按范围增加以下 required fields：

| 事件范围 | 必填字段 |
| --- | --- |
| binding/generation | `bindingId`、`bindingRevision`、`productionGenerationId` |
| rebuild header | 上述字段、`rebuildId`、`workflowRunId` |
| rebuild item | 上述字段、`rebuildId`、`rebuildItemId`、`episodeId`、`workflowRunId` |
| shot/anchor/render | `bindingId`、`bindingRevision`、`productionGenerationId`、`episodeId`、`storyboardShotId`、`workflowRunId` |

不存在 Workflow 的纯事务事件可显式把 `workflowRunId` 定义为 optional，但 catalog 必须逐事件声明，生产者不能传空 UUID 字符串。payload 缺少 required field 时事务 outbox 写入失败，不能等到 Event Publisher 才报错。事件 contract tests 必须覆盖 schemaVersion、UUID/null 规则和终态 status。

进度事件只刷新对应 rebuild item、节点或镜头，终态事件才刷新生产状态和媒体列表。消费者必须忽略非 active `productionGenerationId` 的 UI 失效信号，但仍可在历史任务详情中展示。

### 20.3 指标

~~~text
video_production_rebuild_total
video_production_rebuild_failure_total
anchor_generation_attempts_total
anchor_review_rejection_total
reference_pack_incomplete_total
transition_reset_total
transition_soft_reference_total
observed_exit_rejection_total
render_plan_replan_total
video_prompt_plan_stale_total
video_continuation_contract_rejection_total
model_capability_approval_required_total
video_production_continue_as_new_total
video_production_checkpoint_recovery_total
video_generation_success_total
video_generation_failure_total
continuity_failure_total
~~~

按 profile、profile version、provider model、transition type 和失败原因分维度统计。

## 21. 实施顺序

### P0：数据保护和错误止损

1. 冻结 Provider 管理配置写入，并对第 13.18 节配置表生成迁移前主键集合与规范化非敏感 hash；对历史表生成迁移前主键集合。
2. 增加迁移保护和已应用 migration hash 测试，禁止本轮 migration/seed truncate、覆盖 Provider 数据或修改 `000001_current_schema.sql`。
3. 跨 Storyboard Shot 默认不再传 `ContinuityFirstFrame`；只有同镜头 Render Segment 可以使用前段尾帧。
4. 当前镜头已有 fresh 图片时始终作为图生视频首帧，未分类转场默认 reset。
5. 一个旧连续组失败不再阻塞下一个独立锚点后的镜头。
6. 暂停新的跨镜头硬尾帧任务，排空或取消活动 Provider video task，为正向迁移建立可验证基线。

P0 可以暂时读取现有视频字段完成止损，但不得写新的旧模式数据，也不得重建整个数据库。

### P1：不可变项目方案和受控重建

1. 新建正向迁移 `000009_video_production_profiles_and_generation_fence.sql`，建立 Profile、binding、generation 和 rebuild schema；只为迁移后创建或显式重建的项目写 binding/generation，不为旧项目建立兼容默认值。
2. 新建正向迁移 `000010_video_production_prompt_contracts.sql`，建立 PromptContextPlan、不可变 VideoPromptPlan、结构化对白/音频 contract、generation 关联和系统 Prompt Contract/version。
3. 通过 `000011_video_production_generation_guards.sql` 和 `000012_single_frame_prompt_contract_runtime.sql` 收紧 generation fence 与图生视频 Prompt runtime；后续能力审批、双 Input Contract 和 checkpoint 继续使用新编号 migration，禁止回改已验证文件。
4. 所有 migration 的系统 seed 只操作本轮新增的 Profile/Prompt 数据，使用固定 key/version/hash 并可重复校验；不得调用会覆盖 Provider 或用户 Prompt 的全量 seed。
5. 创建项目时原子写入 active binding/generation；reserved version 和缺少 binding 的旧项目服务端拒绝生产，并在 feature flag 关闭时先验证 API。
6. 实现 rebuild impact、审批、锁定、多分集 items、旧 generation 归档、binding/generation 切换、资产保留和新分镜重建 Saga。
7. 为所有镜头生产写入接入 generation CAS，验证旧 Workflow、延迟 Activity 和 Provider 回调不能污染新 generation；删除直接更新 `productionMode` 的 API/Agent 工具并补齐 RBAC、事件、OpenAPI 和恢复测试。

本阶段所有新增 migration 都必须包含可执行的 Goose Up/Down；Down 只用于空库/开发 dry-run 回退本轮新增对象，并在存在新 generation 业务数据时明确拒绝破坏性回退，不得通过级联触达 Provider 保护表。正式环境回滚使用数据库备份或前向修复 migration，不以长期保留旧运行时代码为代价。

### P2：图生视频分镜、锚点和 Prompt Contract

1. 先只为 `single_frame_i2v` 实现 ShotState、Transition、typed ReferencePack 和 `planned_first_frame`。
2. 扩展 Shot Planner，使分镜从生成时就满足单首帧视频模型的构图和动作可达性。
3. 实现确定性 transition classifier、ShotState validator/hash 和独立 Reviewer。
4. 实现 `PromptContextPlan` 编译器，用整集权威语料生成 continuity digest，并按当前场景、相邻摘要、当前镜头和模型限制分层预算。
5. 建立公共规则 + 手册版本 + 图生视频策略片段 + 镜头状态/引用 + 模型限制的 Prompt Registry 契约、输入/输出 schema 和 Reviewer。
6. 图片 Prompt 禁止台词和可见文字；视频 Prompt 结构化携带并逐字保留本镜头中文台词，校验时长和 native audio 能力。
7. 保存 profile、Prompt、PromptContextPlan、模型能力、引用和审核的完整 provenance。

### P3：图生视频 Gateway 与长任务 Workflow

1. 重构 `VideoGenerationVariant`，先完整实现 canonical `first_frame` initial Input Contract，并为多 Segment 明确选择 `video_extension` 或 fresh tail `first_frame` continuation contract。
2. 对当前已配置视频模型做能力校验和 adapter 映射，不删除或重建供应商、模型及绑定；为 `inferred` 快照建立独立 attestation 审批/撤销闭环。
3. 扩展 Gateway canonical planner/create contract，加入 generation、binding revision、ShotState/Transition、ReferencePack、approved VideoPromptPlan、PromptContextPlan、initial/continuation contract、dialogue cues 和 native audio requirements。
4. 实现 `ProjectRebuild -> Episode -> SceneOrShotBatch -> Activity/有界 Shot Workflow` 分层编排，不为每镜头固定创建两个 Child Workflow。
5. 将 Prompt Batch 与 Video Batch 分离；已审核 Prompt 直接执行视频生成，视频 Batch 的 Prompt Agent/Reviewer Activity 调用数必须为 0。
6. 增加分集/批次/item checkpoint、ready-set 调度、Child drain 后 Continue-As-New、Pinned Worker Versioning、并发、取消、幂等、部分完成和失败 item 重试。
7. Provider 结果、真实错误、媒体转存、generation fence、call log 和 cost record 全链路验收。

### P4：图生视频前端闭环和首个发布门槛

1. 在 P1-P3 contract 和 migration 验收通过后开启 feature flag；新建项目显示四张卡，只有图生视频可用，可用性完全由 API 驱动。
2. 分镜页显示首帧状态、转场、锚点、引用和审核；视频页显示 Render Plan、Prompt 和历史版本。
3. 任务活动实时展示逐镜头 Prompt、锚点、审核、Provider 异步任务和媒体转存状态。
4. 实现单镜头和批量生成、最多配置并发、取消、部分完成、只重试失败项。
5. 项目设置只读展示 binding/version/generation；重建对话框和 Agent 都使用同一 impact/rebuild API。
6. 供应商模型页展示 capability evidence/snapshot，并完成 `inferred` 审批、撤销和失效提示。
7. 完成第 29.1 节全部验收后，才允许把图生视频标记为首个生产可用里程碑。

### P5：共用扩展基础

1. 将图生视频垂直切片中已验证的代码抽取为统一 Profile Compiler、Reference Resolver 和 Workflow 骨架。
2. 新增可插拔 Prompt Contract、Anchor Strategy、Input Contract Adapter 和 Reviewer 接口。
3. 用 contract tests 保证扩展新 Profile 不会改变图生视频行为。
4. 删除 `ContinuityGroupKey`、旧尾帧专用表和旧 CrossShotTailFrame Workflow。

### P6：首尾帧衔接模式

1. 为 `first_last_frame` 完成首尾帧规划、生成、审核和 Gateway 适配；不原地修改 reserved version。
2. 验证首尾人物身份、站位、动作可达性和模型时长限制。
3. 达到独立验收后发布新的 `published + available` Profile version。

### P7：多模态参考模式

1. 实现多类型引用打包、确定性裁剪、引用语义和 provider adapter。
2. 验证角色、场景、道具、图片、视频和音频引用不会串位。
3. 达到独立验收后发布新的 `published + available` Profile version。

### P8：分镜板模式

1. 实现 GPT-image-2 分镜板、PanelManifest、裁板审核和 storyboard sheet Input Contract。
2. 验证分镜板无文字、panel 顺序正确且模型不会把整张板误当首帧。
3. 达到独立验收后发布新的 `published + available` Profile version。

### P9：清理、压测和全目标发布

1. 删除旧字段、旧 Prompt、旧 API、旧 Agent 工具和旧测试夹具。
2. 删除任何宫格图模式残留。
3. 全仓测试、Workflow replay 和 Provider contract tests。
4. 使用 10 集和长分集样本压测四种已启用方案。
5. Docker Compose 重建并执行浏览器 smoke。
6. 更新进度文档和运维手册。

## 22. 测试计划

### 22.1 领域与数据库

- 已应用的 `000001_current_schema.sql` 内容和 migration hash 不变；新 schema 只通过从 `000009` 开始的连续正向 migration 建立，不回改已验证 migration。
- Profile family key 唯一，version 使用 `(profile_id, version)` 唯一；首个里程碑只有 `single_frame_i2v` 为 `published + available`。
- reserved/draft/retired Profile version 无法创建项目或启动 Workflow，也不会自动降级到可用 Profile。
- 项目创建与 binding/generation 写入同一事务，binding 保存 version ID、revision、snapshot/hash 和 compatibility policy。
- 创建后直接修改 active binding 被数据库约束和 API 同时拒绝。
- 多分集项目的重建为每个 `script_episode_id` 建立 item；允许部分成功、只重试失败 item，成功分集不重复生成。
- 重建方案会归档旧活动分镜及镜头级下游数据、创建新 binding/generation，并保留 canonical assets、asset references 和 asset versions。
- 旧 generation 的延迟 Activity、Provider 回调和媒体转存无法写入新 generation；返回 `WORKFLOW_RESULT_DISCARDED` 且保留审计。
- 迁移前后的 Provider 配置表主键集合、行数及配置 hash 完全一致；历史表迁移前主键集合是迁移后集合的子集。
- 旧分镜、shot requirements 和衍生媒体只归档不硬删；对象存储不由 rebuild 直接删除。
- capability attestation 只作用于精确 snapshot hash；修改能力、variant 或 Adapter version 后旧审批不再生效，Provider 配置字段保持不变。
- `inferred` 无审批、已 revoke 审批和 `unknown` 能力均不能进入自动生产；`official/tested` 必须保存可追溯证据。
- approved `video_prompt_plans` 不可原地改正文、对白或 provenance；人工编辑创建新 revision 并使旧 Render Plan stale。
- episode/batch/item checkpoint revision CAS、partial、失败项重试和 Continue-As-New 恢复在真实 PostgreSQL 约束下通过。
- ShotState 严格校验人物、场景、机位、站位和状态枚举。
- Transition graph 无环、顺序合法、target predecessor 唯一。
- 跨镜头 tail policy 无法写入 hard。
- 修改 state 后 stale 传播只到下一个 reset boundary。

### 22.2 分镜规划

- 场景改变必定分类为 `scene_cut`。
- 人物集合改变必定分类为 `subject_change`。
- 机位改变不能分类为同镜头连续 Segment。
- 未分类转场默认 reset。
- Planner 输出漏掉当前人物时确定性校验失败。
- Reviewer 只重跑有问题的场景或镜头。

### 22.3 Reference Pack

- 不选择 archived、disabled、stale 或历史参考。
- 不选择旧 `productionGenerationId` 的镜头衍生资产、Visual Anchor 或媒体；新 generation 只能从 canonical 当前参考重新解析。
- required 角色和场景不可被数量裁剪。
- 单参考图模型只得到当前镜头首帧。
- 多模态模型得到带正确 role 的引用。
- 不支持 reference + first frame 组合的模型被 Gateway 拒绝。
- 分镜板不会被映射成 first frame。

### 22.4 Visual Anchor

- 图生视频模式生成干净首帧。
- 图生视频图片 Prompt 不包含台词、字幕和说明文字。
- 图生视频视频 Prompt 使用专属模板，逐字保留需要说出的中文台词。
- `PromptContextPlan` 使用整集 continuity digest、当前场景全文、相邻场景摘要和当前镜头状态；不会为每个镜头原样注入整集剧本。
- 上下文超限按确定性预算压缩，逐字台词永不截断；硬约束无法容纳时返回 `PROMPT_CONTEXT_LIMIT_EXCEEDED`。
- Prompt 生成和 Reviewer 使用同一 `PromptContextPlan` hash，不发生二次无记录裁剪。
- Profile key/version、Prompt version/hash 和 Input Contract version 完整入库。
- 首尾帧模式生成两个独立锚点。
- 新人物入画时锚点包含该人物。
- 换机位时锚点采用当前机位，而不是上一尾帧构图。
- 分镜板 panel 数量和顺序与 manifest 一致。
- 分镜板存在文字、台词、编号或缺失 panel 时审核失败。

### 22.5 Workflow

- 图生视频模式从分镜、首帧、Prompt、审核、Provider 异步任务到媒体转存形成完整可恢复链路。
- Prompt Batch 可以调用 Prompt Agent/Reviewer；Video Batch 对已审核 Prompt 的 Prompt Agent/Reviewer Activity 调用次数严格为 0。
- 没有 approved Prompt、Prompt stale 或 hash 不一致时 Video Batch 直接失败，不隐式补生成。
- 不同镜头可以在 reset boundary 后并发。
- 同镜头 Render Segment 严格串行。
- 首段只满足 initial contract；后续段只满足 continuation contract，二者的 snapshot/hash 均写入 Render Plan。
- continuation=`video_extension` 时引用必须是同镜头前一成功 Segment 视频；continuation=`first_frame` 时 previous tail source artifact 必须 fresh。
- 仅支持 `first_frame` 且无法抽取 fresh tail、或仅有普通 `video_reference` 的模型不能伪装为支持视频延长。
- 一个镜头失败不阻塞后续独立锚点。
- 取消只取消活跃 Provider task。
- Worker 重启后从持久 checkpoint 恢复。
- Parent 在所有 Child 终态或完成 drain 后才 Continue-As-New；新 run 不携带大媒体 URL、完整 Prompt 或整集剧本。
- `GetContinueAsNewSuggested()` 触发路径、手动 history 阈值路径和无 live Child 断言均有测试。
- Project Rebuild Parent 只直接管理 episode children；Episode 只管理有界 batch children，不出现每镜头固定两个 Child 的历史膨胀。
- Pinned Worker Deployment Version 下当前 Build ID 和 N-1 Build ID replay/drain 测试通过。
- 多分集重建出现一个失败 item 时 header 为 `partial_succeeded/storyboard_required`，重试只调度失败 item。

### 22.6 Provider Gateway

- 能力不兼容返回 `PRODUCTION_PROFILE_INCOMPATIBLE`。
- `inferred` capability snapshot 未审批时返回 `MODEL_CAPABILITY_APPROVAL_REQUIRED`，审批后 planner/create 均冻结同一 attestation ID。
- Adapter 不得静默丢弃 reference role。
- 互斥输入组合返回 `MODEL_INPUT_CONTRACT_UNSUPPORTED`。
- Adapter contract tests 分别覆盖 `first_frame`、`video_extension_source`、普通 `video_reference`，保证三者不会互相代替。
- Create 校验实际媒体 MIME、引用来源、segment index、Prompt/hash、dialogue cues 和 initial/continuation contract；失败发生在上游调用前且不产生 cost record。
- Provider call logs、async tasks、artifacts、media files 和 cost records 完整。
- Planner/create/task 均保存 `productionGenerationId`、binding revision 和 PromptContextPlan hash。
- `nativeAudioRequired=true` 时无原生音频能力的 variant 被拒绝；Adapter contract test 验证中文 `dialogueCues` 未被改写或截断。
- Provider 成功但 generation 已 superseded 时只保留审计与暂存媒体，不挂接活动镜头。
- 图片和视频媒体都转存到 CineWeave 对象存储。
- 使用 `httptest.Server` 和协议 fixture，不重新引入 CI `mock-provider` 服务。

### 22.7 前端

- 新建项目的旧四项被四种新 Profile 卡片完整替换。
- 图生视频卡片可选，三个 reserved 卡片可见但不可提交。
- 项目设置只读展示已绑定方案，不提供直接切换控件。
- 重建生产方案先展示影响，任务活动实时展示清理和新分镜生成进度。
- 宫格图模式不出现。
- 不兼容模型有明确中文阻断信息。
- 分镜和视频弹窗展示当前锚点和实际引用。
- 关闭子弹窗不关闭父弹窗，不重置页面滚动。
- 活动任务通过 SSE 更新，不发生整页重复刷新。

### 22.8 Event、Agent 与权限

- `packages/events/catalog.yaml` 包含所有新增事件及 `schemaVersion`；生成代码与 catalog 一致。
- 缺少 `workflowRunId`、`productionGenerationId`、`bindingId`、`bindingRevision`、`rebuildId` 或 `episodeId` 的适用事件在 outbox 写入前失败。
- Event payload 不接受空字符串 UUID，允许 optional 的字段必须在 catalog 逐事件声明。
- `project.video_production.rebuild` 权限未授予时 API 和 Agent 均拒绝执行。
- `require_approval` 保存影响快照和用户批准；`full_access` 仍无法绕过 RBAC、impact token、expected revision 和 generation lock。
- 不存在直接修改 binding/profile/generation 的 Agent 工具。

## 23. 验收场景

### 场景 A：同场景反打

镜头 1 拍角色 A，镜头 2 反打角色 B。

预期：

- transition 为 `camera_cut` 或 `same_scene_cut`。
- 镜头 2 使用角色 B 和反打机位锚点。
- 镜头 1 尾帧不作为镜头 2 硬首帧。
- 服装、场景时间和光线可继续 carry。

### 场景 B：新人物入画

镜头 1 只有角色 A，镜头 2 增加角色 C。

预期：

- transition 为 `subject_change`。
- 镜头 2 Reference Pack 必须包含角色 A、角色 C 和场景。
- 锚点缺少角色 C 时阻止视频生成。

### 场景 C：角色位移

镜头 1 角色在远景左侧，镜头 2 角色已到近景右侧。

预期：

- 新锚点按计划站位生成。
- 尾帧只能作为服装、环境或动作阶段提示。
- 不要求视频模型从旧位置连续变形成新位置。

### 场景 D：场景切换

镜头从山崖切到雨夜山寨。

预期：

- transition 为 `scene_cut`。
- 无前序尾帧引用。
- 新场景锚点独立生成。
- 前一镜头失败不阻塞新场景镜头。

### 场景 E：同镜头超过模型时长

一个 18 秒长镜头，模型单次最多 10 秒。

预期：

- 保持一个 Storyboard Shot。
- 编译为两个 Render Segment。
- 优先使用 video extension；其次 previous segment tail。
- Segment 严格串行。
- 不把它错误拆成两个跨镜头 transition。

### 场景 F：分镜板模式

一个 12 秒复杂动作镜头。

预期：

- GPT-image-2 生成 6 panel 单张分镜板。
- panel 无文字，顺序和动作阶段通过审核。
- 视频模型把分镜板作为语义参考，不把分格画面作为首帧。
- 生成结果仍是一个镜头视频。

### 场景 G：项目创建后切换生产方案

一个已使用图生视频模式生成分镜和镜头视频的项目，用户在未来切换到已经 available 的多模态参考模式。

预期：

- 项目设置不存在直接修改 Profile 的控件和 API。
- 影响检查准确列出将清空的分镜、镜头媒体、时间线和成片，以及将保留的 canonical assets。
- 用户确认后旧活动分镜不再出现在默认查询和 Agent 上下文。
- canonical assets、原文、剧本、供应商和模型配置保持不变。
- 新 binding 激活并按多模态参考模式重新生成分镜；失败时可从重建 checkpoint 重试。

### 场景 H：图生视频首个发布里程碑

新建项目选择图生视频模式，使用一个已配置且支持单首帧输入的视频模型生成完整一集。

预期：

- 每个镜头使用自己的已审核首帧，跨镜头不硬传上一镜头尾帧。
- 图片 Prompt 无台词，视频 Prompt 忠实保留对应剧本中文台词。
- 批量生成可并发、可取消、可部分完成并只重试失败镜头。
- 每个镜头可追溯到 Profile、Prompt、ShotState、ReferencePack、RenderPlan、模型能力和 Provider Call。
- 另外三种 reserved 方案不能被创建请求绕过启用。

### 场景 I：多分集生产方案重建

一个项目有 10 集，其中 7 集已有活动分镜和镜头视频。用户切换到未来已 available 的目标 Profile，第 4 集新分镜首次生成失败。

预期：

- Impact 创建 10 个 episode item，并逐集记录 source/target StoryboardPlan。
- 旧 generation 的 7 集分镜和镜头级产物归档，canonical assets 全部保留。
- 其余 9 集成功后 header 为 `partial_succeeded/storyboard_required`，第 4 集可独立重试。
- 重试第 4 集不会再次调用其余成功分集的供应商任务。
- 全部 item 成功前，项目保持新 binding/generation，不恢复旧方案。

### 场景 J：旧任务延迟完成

重建切换 generation 后，旧 generation 的视频 Provider task 才返回成功并完成媒体转存。

预期：

- Provider call、cost、async task 和暂存 Artifact 作为历史审计保留。
- 业务提交因 generation CAS 失败并记录 `WORKFLOW_RESULT_DISCARDED/PRODUCTION_GENERATION_MISMATCH`。
- 新 generation 的镜头状态、媒体绑定、ProductionStatus 和事件流不被覆盖。
- 用户可在旧任务技术详情中查看结果，但默认视频列表不显示它。

## 24. 验收命令

~~~powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
python -c "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))"
python scripts/check-openapi-routes.py
pwsh -File scripts/test-migrations.ps1
git diff --exit-code -- db/migrations/000001_current_schema.sql
rg -n 'production_mode|ProductionMode|productionMode|silent_video|storyboard_only|assets_only' apps/web/src internal packages/openapi db/migrations
docker compose -f compose.yml config --quiet
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
~~~

`rg` 验收在 P9 后只能命中明确标注的 migration/history 读取或删除迁移，不得再命中活动 API、Workflow、Agent、前端表单或业务导出路径。

运行时 smoke 必须覆盖：

- 首个里程碑完整覆盖 `single_frame_i2v`，并验证三个 reserved Profile 的前端禁用与服务端拒绝；后续每个 Profile 变为 available 前分别执行完整 smoke。
- 换机位。
- 新人物入画。
- 角色位移。
- 场景切换。
- 同镜头多 Segment。
- 多分集重建的部分成功和失败 item 重试。
- generation 切换后旧 Provider task 延迟完成。
- 原生音频与逐字中文 dialogue cues。
- 分镜板方案发布阶段的分镜板生成和审核。
- 单镜头失败后的 reset boundary 恢复。

## 25. 风险与控制

| 风险 | 控制 |
| --- | --- |
| 数据模型大幅变化 | 只追加从 `000009` 开始的新编号定向迁移，保护已应用 migration hash 和 Provider 配置，不维护旧视频字段双写或旧项目兼容 backfill |
| Prompt 和状态契约过大 | `PromptContextPlan` 按整集摘要、场景和镜头分层预算，Workflow 只传 ID 和 hash |
| Agent transition 判断不稳定 | 确定性规则采用更保守分类 |
| 锚点生成增加调用成本 | 复用 fresh 锚点，按 hash 幂等，只重试失败镜头 |
| 图片审核误判 | 结构化检查、置信度门槛、人工审批和可追溯版本 |
| 模型能力错误 | capability provenance、人工验证、Gateway 最终校验 |
| 分镜板被模型当首帧 | input contract 明确区分 semantic reference 与 first frame |
| 长任务升级导致 replay 问题 | Pinned Worker Deployment Version、N-1 drain 和 replay tests |
| Child 数量与 history 膨胀 | Project/Episode/Batch 分层、有界批次、Child drain 后 Continue-As-New |
| 一个失败阻塞整集 | reset boundary 切断依赖，镜头状态机与 rebuild item 独立重试 |
| 旧任务污染重建后的项目 | 项目级 generation fence、业务 CAS、Provider 回调二次校验 |
| 原生音频遗漏或台词改写 | 结构化 dialogue cues、native audio capability gate 和 Adapter contract tests |
| 实际输出偏差被继续传播 | ObservedExitState 审核失败即禁止成为 continuity hint |

## 26. 迁移与发布规则

虽然不兼容旧项目视频生产数据，但 migration history、Provider 配置和历史审计必须保留。执行 schema 变更前必须：

1. 关闭新视频生产和 Profile rebuild 入口，冻结 Provider 管理配置写入。
2. 等待或取消正在运行的视频 Workflow，确认没有 active Provider async video task/lease。
3. 记录 `schema_migrations` 已应用版本和 hash，特别确认 `000001_current_schema.sql` 的仓库内容与已应用 hash 不变。
4. 导出第 13.18 节 Provider 配置表的主键集合、行数和规范化非敏感 hash；导出历史表的迁移前主键集合。
5. 备份数据库，并在备份副本顺序执行本目标新增的 `000009` 及后续 migration dry-run，验证 DDL/seed 不触达 Provider 保护表。
6. 停止旧视频 Worker，保持 API 的 feature flag 关闭。
7. 顺序应用 `000009_video_production_profiles_and_generation_fence.sql`、`000010_video_production_prompt_contracts.sql`、`000011_video_production_generation_guards.sql`、`000012_single_frame_prompt_contract_runtime.sql` 及后续经过 dry-run 的执行契约/checkpoint migration；系统 Profile/Prompt seed 在各 migration 的 DDL 之后执行，不额外运行全量 seed。
8. 对配置表执行精确主键/行数/hash 等值检查；对 `provider_call_logs`、`cost_records`、`provider_async_tasks` 执行旧主键子集检查。不一致立即停止发布。
9. 使用新的 Pinned Worker Deployment Version 启动 Worker，在 feature flag 关闭状态执行 migration、generation fence、event catalog 和 replay 验收。
10. 不为旧项目执行兼容 backfill，也不保留旧 `production_mode` 到新 Profile 的运行时翻译。缺少新 binding/generation 的开发期旧项目不能启动视频生产；用户通过重新创建项目，或在可识别其分集/资产时执行显式受控 rebuild 进入新模型。两种路径都必须保留 canonical assets 和 Provider 配置。
11. 完成图生视频 API/Workflow smoke 后开启后端入口，再启用前端 Profile 卡片。
12. 解除 Provider 配置冻结，监控 generation mismatch、discarded result、rebuild partial 和 Provider task 指标。

禁止删除数据库 volume、执行全库 reset、修改已应用 migration、重新运行会覆盖用户供应商配置的全量 seed，或在新 schema 上让旧 Worker 继续写旧状态。允许清理明确属于旧项目 generation 的视频生产数据，但必须通过有范围、有审计且不触达 Provider/canonical asset 的脚本或 rebuild。回滚采用关闭 feature flag、停止新 Worker 和恢复备份/前向修复 migration，不通过修改旧 migration 伪造回滚，也不重新引入旧兼容代码。

## 27. 预计改动位置

后端核心：

- `internal/storyboard/contracts.go`
- `internal/storyboard/revisions.go`
- `internal/workflows/storyboard_blueprint_activities.go`
- `internal/workflows/storyboard_scene_planning.go`
- `internal/workflows/video_long_running.go`
- `internal/workflows/shot_continuity_frame.go`
- `internal/workflows/video_activities.go`
- `internal/workflows/project_agent.go`
- `internal/workflows/asset_batch.go`
- `internal/workflows/image_prompt_agent.go`
- `internal/workflows/video_prompt_agent.go`
- `internal/workflows/script_driven.go`
- `internal/workflows/script_driven_storage.go`
- `internal/provider/video_capabilities.go`
- `internal/provider/video_planner.go`
- `internal/provider/gateway_video.go`
- Provider Gateway planner/create/cancel request structs、async task repository 和 media commit 路径
- `internal/api/storyboard_shots.go`
- `internal/api/shot_production.go`
- `internal/api/agent_control.go`
- `internal/api/agent_executor.go`
- `internal/api/agent_tools.go`
- `internal/api/asset_batches.go`
- `internal/api/production_assets.go`
- `internal/api/scripts.go`
- `internal/api/server.go`
- `internal/workflows/node_runs.go`
- `internal/exporter/documents.go`
- `internal/review/service.go`

`productionMode` 目前还会进入项目创建/更新、Agent 上下文、资产 Prompt、剧本驱动 Workflow、导出和审阅。实施时必须以第 24 节 `rg` 命令生成实时清单，不得只修改新建项目页和数据库字段。

前端核心：

- `apps/web/src/features/projects/new-project-page.tsx`
- `apps/web/src/features/project-settings/settings-page.tsx`
- `apps/web/src/features/projects/*`
- `apps/web/src/features/storyboard/*`
- `apps/web/src/features/video/*`
- `apps/web/src/features/activity/*`
- `apps/web/src/lib/types.ts`
- `apps/web/src/lib/api-client.ts`
- `apps/web/src/lib/labels.ts`
- `apps/web/src/lib/error-localization.ts`

契约和数据：

- `packages/openapi/openapi.yaml`
- `packages/events/catalog.yaml`
- event catalog 生成代码和 payload validators
- `db/migrations/000009_video_production_profiles_and_generation_fence.sql`（新增）
- `db/migrations/000010_video_production_prompt_contracts.sql`（新增）
- `db/migrations/000011_video_production_generation_guards.sql`（新增）
- `db/migrations/000012_single_frame_prompt_contract_runtime.sql`（新增）
- 后续 capability attestation、initial/continuation contract 和 episode/batch/item checkpoint migration（新增，不修改上述已应用 migration）
- `db/seeds/*`

`db/migrations/000001_current_schema.sql` 仅作为当前 schema 事实和 migration hash 验证输入，不允许编辑。

文档：

- `docs/script-to-storyboard-timing-refactor-plan.md`
- `docs/provider-gateway.md`
- `docs/workflow-engine.md`
- `docs/codex-execution-plan.md`

## 28. 外部能力依据

- [Google Veo 3.1](https://ai.google.dev/gemini-api/docs/veo) 将视频延长、首尾帧生成和 reference image guidance 定义为不同能力，支持本方案将“同镜头延长”和“跨镜头语义连续”拆开处理。
- [xAI 视频接口](https://docs.x.ai/developers/model-capabilities/video/generation)将 text-to-video、image-to-video、reference-to-video、edit-video 和 extend-video 定义为互斥请求模式，说明 Gateway 必须按 Input Contract 显式适配。
- [Seedance 2.0 官方资料](https://developer.volcengine.com/articles/7628567056649125942)明确支持文字、图片、音频和视频多模态参考，并给出关键帧、分镜图和参考视频长镜头实践，适合作为多模态参考和分镜板方案的目标模型之一。
- [Temporal Go Continue-As-New](https://docs.temporal.io/develop/go/workflows/continue-as-new)要求通过新的 Workflow run 控制 Event History，并提供 `GetContinueAsNewSuggested()` 判断；本方案据此使用动态 history 门槛而非固定时长。
- [Temporal Child Workflows](https://docs.temporal.io/child-workflows)建议单个 Parent Workflow 不要启动超过约 1,000 个 Child Workflow；本方案据此采用 Project/Episode/Batch 分层，而不是每镜头固定两个 Child。
- [Temporal Worker Versioning](https://docs.temporal.io/production-deployment/worker-deployments/worker-versioning)提供 Pinned Workflow 行为和 Worker Deployment Version 管理；本方案据此固定小时级生产 Workflow 的执行版本并保留 N-1 drain。

这些资料只证明能力类别和接口差异，不作为所有渠道、模型别名或第三方代理都支持同样能力的依据。最终以 Provider Gateway 中对应 provider model variant 的已验证 capability snapshot 为准。

## 29. 完成定义

### 29.1 首个生产可用里程碑

当前阶段只在同时满足以下条件时标记完成：

1. 新建项目的旧四项已被四种新 Profile 卡片替换。
2. `single_frame_i2v` 为 `published + available` 且默认选中，其余三个 Profile 为 `published + reserved` 真实占位并由后端拒绝运行。
3. 项目创建后 Profile 不可直接修改；重建方案必须经过影响检查、多分集 rebuild items、归档活动分镜和重新生成适配分镜。
4. 项目 binding/version/generation 模型唯一且可审计，旧 generation 的延迟结果无法写入当前活动数据。
5. 重建操作保留 canonical assets、asset references 和 asset versions；旧镜头衍生数据不进入新 Reference Pack。
6. `000001_current_schema.sql` 和已应用 migration hash 不变；Provider 配置精确一致，Provider 历史满足迁移前主键子集关系。
7. 图生视频从分镜规划、首帧、专属 Prompt、审核、Render Plan、Provider 调用、媒体转存到前端预览完整可靠。
8. `PromptContextPlan` 在模型限制内使用整集权威语料，逐字中文 dialogue cues 不截断；需要原生音频时模型和 Adapter 均通过能力校验。
9. 不同 Storyboard Shot 之间不再存在硬尾帧串联；同镜头首段/续段使用分别冻结的 Input Contract，`video_extension`、fresh previous segment tail 和普通 `video_reference` 不会混用。
10. 换机位、新人物、角色位移和场景切换的图生视频 smoke 全部通过。
11. 每个视频可追溯到 Profile version、generation、ShotState、Transition、VisualAnchor、ReferencePack、PromptContextPlan、Prompt、RenderPlan、模型能力和 Provider Call。
12. 一个镜头失败不会阻塞下一个 reset boundary 后的镜头，批量任务和多分集 rebuild 支持部分完成与失败项重试。
13. Prompt Batch 与 Video Batch 已拆分；视频执行只消费不可变 approved Prompt，Prompt Agent/Reviewer 调用次数为 0，缺失或 stale Prompt 逐镜头失败且可重试。
14. Project/Episode/Batch Workflow 分层、持久 checkpoint、partial/failed item retry、Child drain、Continue-As-New、Pinned Worker Versioning 和 N-1 replay 验收通过。
15. `official/tested/inferred/unknown` 能力状态和 attestation 闭环完成；`inferred` 无当前 snapshot 审批时无法自动生产，且 Provider 配置未被审批流程改写。
16. 所有新事件经过 catalog payload 校验，Agent rebuild 经过 RBAC、impact token、审批策略和项目锁。
17. 前端实时展示任务状态且不重复刷新整个页面；全仓测试、OpenAPI、事件 catalog、Compose 和浏览器 smoke 通过。

### 29.2 长期全目标完成

在首个里程碑之后，还必须满足以下条件才能标记本目标文档全部完成：

1. 首尾帧、多模态参考和分镜板模式分别通过独立端到端验收并发布 `published + available` Profile version。
2. 四种方案都只能在新建项目时选择，或通过受控重建切换；项目设置始终没有直接切换字段。
3. 四种方案均使用自己的 Prompt Contract、Anchor Strategy、Input Contract Adapter 和 Reviewer，没有复制 Workflow 或静默降级。
4. 旧字段、旧 Workflow、旧 Prompt、旧 API、旧 Agent 工具和旧兼容代码已删除。
5. 四种方案的长分集压测、Workflow replay、Provider contract tests 和浏览器 smoke 全部通过。

## 30. 文档维护规则

- 本文档只维护目标架构、强制决策、阶段顺序和验收门槛。
- 实施进度应新建 `docs/video-production-workflow-continuity-refactor-progress.md`，不在本文档中把计划项改写成完成项。
- 每个阶段实施前先把任务拆成可独立提交、验证和回滚的工作单元。
- 在第 29.1 节完成前，不得为了并行开发另外三种方案而推迟或削弱图生视频模式的可靠性验收。
- 若模型能力或供应商协议发生变化，先更新 capability schema 和本文档，再修改 Workflow。
- 任何重新引入跨镜头无条件尾帧链的修改都必须经过新的 ADR，不得作为局部修复直接合入。
