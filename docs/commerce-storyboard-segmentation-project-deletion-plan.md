# CineWeave 带货视频分镜切分与项目删除改造计划

- 文档状态：已完成
- 适用仓库：`D:\Code\CineWeave`
- 制定日期：2026-07-25
- 关联文档：
  - `docs/commerce-video-development-plan.md`
  - `docs/script-to-storyboard-timing-refactor-plan.md`
  - `docs/video-production-workflow-continuity-refactor-target.md`
  - `docs/provider-gateway.md`

## 1. 结论

本轮改造包含两个相互独立但都需要按生产级标准实施的目标：

1. 重构带货视频分镜切分，使用户选择的成片目标时长、旁白节奏、叙事节拍、分镜镜头和视频模型单次请求时长不再混为同一概念。
2. 将当前没有前端入口、直接同步删除数据库项目行的接口，升级为带影响分析、任务取消、存储清理和失败重试的异步项目删除流程。

核心决策如下：

1. 用户选择的目标总时长是最终权威值，不因旁白估算或模型离散时长档位而改变。
2. 旁白时长分析只生成建议和风险提示，不阻断 Setup、分镜生成、视频提示词或视频生产提交。
3. 视频模型支持的时长和分辨率是供应商执行硬约束，但不直接成为叙事镜头必须等长的约束。
4. 分镜切分方式按 `CommerceScriptUnitGeneration` 保存，同一项目中的不同广告脚本可以选择不同策略。
5. 第一批支持“智能切分”和“单段生成”；“手动切分”在底层契约稳定后补充。
6. 当前 `single_frame_i2v` 是优先落地的可执行 Profile，其分镜规划必须减少低于模型最小时长的碎片镜头。
7. 不再要求 Agent 精确完成时长量化、模型档位组合和来源覆盖；现有 Sales Script Contract 与 Localization Segment 提供稳定语义输入，确定性规划器负责分组、时间分配和模型能力适配，Agent 只补充镜头创意字段。
8. 项目删除采用异步硬删除。项目立即从默认列表隐藏，删除请求只保存完成删除所需的短期运行状态；内部成本归档、供应商调用历史关联和长期删除审计不在本轮范围内，计费以 New API 平台记录为准。
9. 项目仍处于开发阶段，不实现旧分镜运行时兼容分支；已有带货分镜在新策略上线后标记为需要重建，商品、脚本和供应商配置保留。

## 2. 当前问题

### 2.1 分镜镜头和供应商请求混用

当前带货视频数据链路近似为：

```text
广告脚本
  -> Agent 直接输出 storyboard_shots
  -> 每个 storyboard_shot 生成一张首帧图
  -> 每个 storyboard_shot 独立创建视频任务
  -> Provider Gateway 把镜头时长向上适配到模型可用时长
```

这会把以下三个概念错误地压在同一个镜头上：

```text
叙事节拍：一句旁白、一次商品动作或一次镜头意图
分镜镜头：一个连续摄影和视频生成意图
供应商请求：模型实际接受的 6 / 10 / 12 / 16 秒请求
```

当前样本目标时长为 15 秒，分镜时长是：

```text
2 + 3 + 4 + 3 + 3 = 15 秒
```

当前视频模型支持：

```text
6 / 10 / 12 / 16 秒
```

如果五个镜头分别生成视频，Provider Gateway 会为每个短镜头至少请求 6 秒：

```text
供应商总请求时长 = 6 * 5 = 30 秒
最终成片时长     = 15 秒
```

这不仅增加费用，还会让每段视频生成大量最终被裁掉的内容。

### 2.2 旁白分配不具备容量意识

当前系统已经正确地把旁白超出目标时长改为非阻断建议，但分镜 Agent 仍可能把多段旁白集中放进一个 3 秒镜头。

必须区分：

```text
不阻断生成 != 忽略旁白时长
```

旁白时长应当参与规划评分、镜头分配和前端提示，但不能成为提交门禁。系统必须避免在存在更合理分配方式时，把完整长旁白集中到极短镜头。

### 2.3 模型时长只在执行阶段适配

当前 Provider Gateway 已能把编辑时长映射到离散模型时长并生成裁切信息，但分镜规划阶段只把时长集合当作提示。

结果是：

1. 分镜 Agent 可以生成大量低于模型最小时长的碎片镜头。
2. Gateway 虽能让每个请求合法，却无法跨镜头合并请求。
3. 单镜头合法不代表整条广告的请求组合在成本和连续性上合理。

### 2.4 项目删除接口不完整

当前已有：

```text
DELETE /api/projects/{projectId}
```

但实现只是：

```sql
DELETE FROM projects WHERE id = $1
```

当前缺失：

- 前端入口。
- 删除影响分析。
- 活动 Workflow 和 Provider 异步任务取消。
- 对象存储媒体清理。
- 删除进度与失败重试。
- 删除中的项目状态和并发保护。

## 3. 目标与非目标

### 3.1 目标

1. 用户可为每个广告脚本选择视频切分方式。
2. 智能切分同时考虑脚本语义、旁白句子边界、商品动作、目标时长和模型可用时长；不能因为减少一次供应商请求而把不适合连续生成的语义和动作强行合并。
3. 用户选择的目标总时长严格守恒。
4. 每个供应商请求必须使用当前冻结视频模型支持的时长和分辨率。
5. 规划结果明确展示编辑时长、供应商请求时长和预计裁切时长。
6. 不再出现“3 秒镜头承载整段长旁白但没有显著风险提示”的结果。
7. 模型配置变化后，旧能力快照不会被静默复用。
8. 项目可以从项目列表和项目设置中安全删除。
9. 删除流程可取消活动任务、隔离晚到写入并重试存储清理。

### 3.2 非目标

1. 本轮不自动改写、压缩或翻译用户旁白来适配目标时长。
2. 本轮不要求用户确认旁白时长建议。
3. 本轮不恢复视频模型能力审批步骤。
4. 本轮不把所有模型能力都升级为强匹配；硬约束仍只保留时长和分辨率。
5. 本计划中的“裁切”仅指时间轴尾部裁短，不允许对画面做本地空间裁边、拉伸或改画幅；画幅和分辨率必须在上游请求阶段正确指定。
6. 本轮不优先实现多模态参考、首尾帧或分镜板 Profile 的完整切分器，只预留策略接口。
7. 本轮不实现跨项目回收站。
8. 本轮不保留旧带货分镜的运行时兼容逻辑。
9. 本轮不建设 CineWeave 内部项目删除计费归档、Provider 调用历史关联或长期删除审计；New API 平台是当前计费依据。

## 4. 目标分层模型

带货视频链路调整为：

```text
CommerceScriptUnitGeneration
  -> CommerceSalesScriptContract
  -> CommerceLocalizationSegment
  -> StoryboardBeatInput
  -> DeterministicShotGroup
  -> StoryboardShot
  -> Provider RenderPlan
  -> Provider RenderSegment
  -> Timeline Clip
```

### 4.1 StoryboardBeatInput

`StoryboardBeatInput` 是确定性切分器使用的不可变值对象，不新增一套与 Localization 平行的业务来源表。它由当前 UnitGeneration 已冻结的 Sales Script Contract 和 `commerce_localization_segments` 构造，至少包含：

- `localization_segment_id` 和 `source_segment_id`。
- 一句完整旁白或无旁白视觉节拍。
- 商品展示目的。
- 销售节拍。
- 已冻结的视觉意图和商品特征。
- 屏幕文字、音效和音乐提示的独立字段。
- 预计旁白时长。
- `required`、原始顺序及内容 hash。

BeatInput 不写库为第二套来源数据，也不直接创建供应商视频任务。分镜与来源之间继续使用现有 `commerce_shot_segment_links`，包括 `usage` 和 `verbatim_start/verbatim_end`，禁止再增加可独立漂移的 Shot-to-Beat 映射。

### 4.2 StoryboardShot

`StoryboardShot` 是连续摄影和视频生成意图：

- 包含一个或多个按原顺序排列的 BeatInput。
- 有一个首帧参考图入口。
- 有一个连续动作和机位意图。
- 有明确编辑时长。
- 是视频提示词和视频任务的用户可见单元。

对于当前 `single_frame_i2v`，应优先让一个 Shot 对应一次 Provider 请求，避免依赖模型不支持的跨片段连续生成。

### 4.3 RenderPlan 和 RenderSegment

Provider Gateway 继续负责：

- 按冻结的候选路由集合选择实际模型。
- 冻结候选模型、variant、时长、分辨率和能力 snapshot hash。
- 将编辑时长映射为模型请求时长。
- 必要时建立多个 Render Segment。
- 生成 `trim_end_tick`。
- 拒绝不满足时长或分辨率的请求。

Storyboard Planner 不写死供应商参数，只消费冻结的执行能力摘要。每个规划边必须携带至少一个可以执行该请求时长和分辨率的候选路由；RenderPlan 创建后，Gateway 只能在该边冻结的候选集合内选择，不得回退到能力不匹配的新模型。

## 5. 视频切分方式

### 5.1 智能切分

前端名称：

```text
智能切分
```

说明：

```text
根据脚本语义、旁白句子和当前视频模型时长自动规划视频段。
```

行为：

1. 从已冻结的 Sales Script Contract 和 Localization Segment 确定性构造 BeatInput。
2. 确定性切分器根据目标时长、语义可合并性和能力快照组合 BeatInput。
3. 优先在完整句子、销售节拍和动作结束位置切分。
4. 场景变化、独立商品动作、无法共享首帧状态或镜头复杂度超过策略上限时，不得为了减少请求数强行合并。
5. 在语义和连续性可行的候选中，综合优化请求成本、裁切时长、旁白容量和镜头复杂度。
6. 确定 Shot 分组后再由 Agent 生成景别、机位、动作起止状态和画面创意，Agent 不得改变分组、来源覆盖和时长。

### 5.2 单段生成

前端名称：

```text
单段生成
```

说明：

```text
整条广告只生成一个连续视频段，适合固定机位口播和简单商品演示。
```

行为：

1. 仍创建一个 StoryboardPlan 和一个 StoryboardShot。
2. 所有 BeatInput 作为该 Shot 的内部动作与旁白节拍。
3. 用户目标 15 秒、模型支持 16 秒时，编辑时长为 15 秒，供应商请求为 16 秒，时间线裁切 1 秒。
4. 如果目标时长超过模型单次最大时长且 Profile 不支持连续扩展，前端提示使用智能切分。

### 5.3 手动切分

状态：

```text
第二阶段实现
```

行为：

1. 用户在脚本句子和 Beat 边界上添加切点。
2. 系统实时显示每段编辑时长、请求时长、裁切时长和旁白风险。
3. 用户不能在一句完整旁白中间切分，除非显式选择“允许跨段旁白”。
4. 确认后仍由确定性校验器生成正式计划。

## 6. 智能切分算法

### 6.1 输入

```text
用户目标总时长 T
视频切分方式
UnitGeneration ID、script_unit_revision 与冻结输入 hash
Sales Script Contract 和 Localization Segment
旁白句子和预计时长
商品动作与销售节拍
项目画幅和分辨率
冻结候选路由及各自的视频时长集合 D
Profile 连续性能力
```

当前示例：

```text
T = 15
D = {6, 10, 12, 16}
```

### 6.2 候选切点

允许优先切分的位置：

1. 旁白完整句子结束。
2. 销售节拍切换。
3. 商品动作完成。
4. 机位或景别明显变化。
5. 场景变化。
6. CTA 开始前。

禁止默认切分的位置：

1. 单词、短语或完整句子中间。
2. 商品动作尚未结束的位置。
3. 无法提供新首帧状态的位置。
4. 会破坏来源顺序的位置。

相邻 BeatInput 只有同时满足以下条件时才允许成为同一个智能切分候选边：

1. 不跨越强制场景切换。
2. 不包含互相冲突的机位或首帧状态。
3. 独立商品动作数量不超过版本化策略上限。
4. 视觉复杂度和主体切换次数不超过版本化策略上限。
5. 至少一个冻结候选路由可以覆盖该边的编辑时长和分辨率。

这些上限属于 `segmentation_policy_version`，不能散落为代码常量。`single_take` 由用户显式选择时可以越过语义复杂度建议，但必须返回可见风险，仍不能越过模型时长和分辨率硬约束。

### 6.3 规划硬约束

1. 所有编辑 Shot 时长为正整数秒。
2. 所有 Shot 编辑时长之和严格等于用户目标总时长。
3. 所有来源段落必须且只能按规则覆盖。
4. 旁白文本必须逐字保留并保持顺序。
5. 每个 Shot 的供应商请求时长必须来自冻结时长集合。
6. 每个 Shot 的供应商请求时长必须大于或等于编辑时长。
7. 分辨率必须被冻结模型能力支持。
8. `single_frame_i2v` 的一个 Shot 不能依赖未声明的跨请求连续扩展。
9. 每个 Shot 必须保存至少一个支持其请求时长和分辨率的冻结候选路由集合及 hash。
10. 智能切分不能越过强制场景、动作状态或首帧状态边界。

### 6.4 规划软目标

只在满足全部硬约束的候选图上进行加权优化：

```text
1. 最少语义边界破坏
2. 最低动作和首帧连续性风险
3. 最少旁白容量超出
4. 合理的镜头复杂度
5. 较少供应商请求成本
6. 较少总裁切时长
```

建议评分函数：

```text
score =
  semanticBoundaryPenalty
  + continuityPenalty
  + voiceoverOverflowWeight * totalVoiceoverOverflow
  + complexityPenalty
  + requestCostWeight * estimatedProviderRequestCost
  + requestCountWeight * providerRequestCount
  + paddingWeight * totalTrimSeconds
```

权重、复杂度阈值和稳定 tie-break 规则全部由 `segmentation_policy_version` 冻结。确定性规划器使用动态规划或有向无环图最短路径求解，不使用 Agent 自由计算最终时长组合。请求数不再作为凌驾于语义可行性之上的绝对第一优先级。

### 6.5 15 秒示例

模型时长：

```text
6 / 10 / 12 / 16
```

单段生成：

```text
编辑：15
请求：16
裁切：1
请求数：1
```

当脚本存在两个不可合并的场景或独立动作组时，智能切分可以选择：

```text
编辑：6 + 9
请求：6 + 10
裁切：0 + 1
请求数：2
```

或者：

```text
编辑：5 + 10
请求：6 + 10
裁切：1 + 0
请求数：2
```

不得再默认生成：

```text
编辑：2 + 3 + 4 + 3 + 3
请求：6 + 6 + 6 + 6 + 6
```

当脚本是固定机位、单一连续动作且全部 BeatInput 可合法合并时，智能切分也允许选择一个 15 秒编辑 Shot 和一个 16 秒请求；这与用户显式选择 `single_take` 的区别在于，智能切分必须先通过语义和连续性可合并判断。

## 7. 旁白时长策略

### 7.1 总时长建议

继续保留当前规则：

```text
用户目标时长是权威值
旁白估算只提供建议
超出不阻断
```

例如：

```text
目标时长：15 秒
预计旁白：21.2 秒
风险差值：+6.2 秒
建议：选择 30 秒或缩短脚本
```

用户仍可直接生成 15 秒版本。

### 7.2 单镜头容量

每个 BeatInput 在预览计算中携带预计旁白时长；每个正式 Shot 在 `commerce_shot_contracts` 持久化：

- `estimated_voiceover_ticks`
- `voiceover_overflow_ticks`
- `timing_advisory_level`

Shot 编辑时长由 `storyboard_shots.start_tick/end_tick` 计算，不重复保存秒数字段。分镜规划必须优先让旁白分配与镜头容量接近。只有当用户目标总时长本身不足时，才允许保留镜头级超出警告。

### 7.3 不允许的行为

1. 为了通过校验而删除或改写旁白。
2. 把多段旁白集中放入一个极短镜头，但其他镜头没有旁白。
3. 通过高于项目语言策略允许值的语速假设隐藏超时。
4. 把音效、BGM 或制作说明放入旁白。
5. 因旁白超时阻止用户提交。

## 8. 模型能力快照

### 8.1 能力来源

分镜生成前由 Provider Gateway 或统一模型路由服务返回：

```text
VideoExecutionEnvelope
```

包含：

- 当前业务模型 Profile、Binding ID 和 binding revision。
- 按优先级冻结的候选路由，每个候选包含 Provider Model ID、variant key 和 capability snapshot hash。
- 每个候选独立的整数时长集合和支持分辨率。
- 规划器可用时长并集，以及每个时长对应的可执行候选集合。
- 当前 Production Profile。
- 是否支持单次连续扩展。
- 整个候选路由集合的 canonical hash。
- 模型配置和路由 revision。

规划器不能把不同候选的字段拼成一个不存在的“超级模型”。每个候选边必须由某个完整候选同时满足请求时长和分辨率。RenderPlan 为每个 Segment 冻结 `eligible_route_set` 和 `eligible_route_set_hash`，Gateway 回退只能发生在该集合内。

### 8.2 强约束范围

本轮只对以下能力执行硬约束：

1. 时长。
2. 分辨率。

语言、音频、参考模式、画幅和其他能力继续作为路由提示、结果提示或 Profile 输入契约，不恢复能力审批步骤。

### 8.3 能力变化

以下变化会使未执行的 StoryboardPlan 或 RenderPlan 进入 `needs_regeneration`：

- 视频模型绑定变化。
- 模型时长集合变化。
- 分辨率能力变化。
- Production Profile 变化。

已经生成的媒体不删除，但不得静默使用新的能力快照继续旧 RenderPlan。

## 9. 数据模型

### 9.1 ScriptUnitGeneration 配置

`commerce_script_unit_generations.unit_configuration_snapshot` 从当前 v2 升级为版本化 v3，新增：

```text
storyboardStrategy
segmentationPolicyVersion
```

`storyboardStrategy`：

```text
smart
single_take
manual
```

`unit_configuration_hash` 必须覆盖这两个字段。需要列表查询时，可增加从 JSONB 派生的 stored generated column 和索引，但 JSON snapshot 是唯一配置来源，禁止再维护一套可独立修改的平行字段。

修改切分策略必须走现有 ScriptUnit rebuild，创建新的 UnitGeneration；不得原地修改活动 UnitGeneration。

### 9.2 StoryboardPlan 执行与切分快照

扩展现有 `commerce_storyboard_plans`：

```text
segmentation_policy_version
segmentation_plan
segmentation_plan_hash
video_execution_envelope
video_execution_envelope_hash
timing_advisory
preview_hash
```

现有 `allowed_shot_durations` 保留为 `video_execution_envelope` 的查询投影，不再从其他位置单独推导。`plan_hash`、`projection_hash` 和 `preview_hash` 的 canonical 输入必须包含：

```text
project_production_generation_id
script_unit_generation_id
script_unit_revision
source_script_version_id
localization_id + localized_contract_hash
reference_pack_id + pack_hash
commerce_workflow_binding_id + revision
storyboard_strategy
segmentation_policy_version
video_execution_envelope_hash
target_duration_seconds
aspect_ratio
timeline_timebase + fps
```

### 9.3 Beat 来源与 Shot 映射

不新增 `commerce_storyboard_beats` 和 `commerce_storyboard_shot_beats`。

1. BeatInput 由已冻结的 `commerce_localization_segments` 和 Sales Script Contract 确定性构造。
2. Shot 到来源段落、旁白范围和用途继续写入 `commerce_shot_segment_links`。
3. Agent 生成的景别、机位、动作起止状态和商品展示创意写入 `commerce_shot_contracts` 的版本化结构化字段或 `creative_direction` JSONB。
4. 数据库约束继续保证来源顺序、Localization 身份和 `verbatim_start/verbatim_end` 不越界。
5. 一个 Localization Segment 可以按连续、不重叠的逐字范围跨越多个 Shot；不允许再创建第二套无法与 `commerce_shot_segment_links` 对账的映射。

### 9.4 Shot 规划摘要

在 `commerce_shot_contracts` 保存规划提示：

```text
estimated_voiceover_ticks
voiceover_overflow_ticks
timing_advisory_level
recommended_request_duration_seconds
eligible_route_set_hash
segmentation_policy_version
```

编辑时长继续由 `storyboard_shots.start_tick/end_tick` 表达，避免重复保存 `planned_duration_ticks`。实际请求时长、裁切和最终候选路由以 Provider Gateway 创建的 RenderPlan/RenderSegment 为权威；Shot 上的请求时长仅用于预览和变更检测。

### 9.5 旧数据处理

不增加运行时兼容分支。

迁移后：

1. 旧带货 StoryboardPlan 标记为 `stale`。
2. 旧镜头、首帧图和视频保留历史查看。
3. 当前 ScriptUnitGeneration 保持 active，但 Production Status 投影为 `storyboard_required`。
4. 商品、脚本、Localization、供应商账号和模型配置保留。
5. 用户重新选择切分方式并生成新分镜。

## 10. Workflow 改造

### 10.1 Storyboard Planning Workflow

调整为：

```text
Load Frozen Inputs
  -> Load VideoExecutionEnvelope
  -> Analyze Timing
  -> Build BeatInput From Frozen Localization
  -> Deterministic Segmentation
  -> Validate Preview Hash
  -> Generate Shot Creative Fields
  -> Review Storyboard
  -> Commit Shots + Segment Links + Planning Summary
```

规划预览接口执行相同的 `Load Frozen Inputs -> Build BeatInput -> Deterministic Segmentation`，但不执行 `Generate Shot Creative Fields` 和 Reviewer，因此不产生供应商费用。正式 Workflow 必须重新计算并比较同一个 canonical `preview_hash`，不能信任客户端回传的镜头数组。

### 10.2 Agent 边界

`commerce_storyboard_planner` 负责：

- 在已冻结 Shot 分组内生成商品动作。
- 景别、机位和画面意图。
- 动作起止状态。
- 商品展示创意。

Planner 不得改变 Shot 数量、Shot 时长、Localization Segment 分组、逐字旁白范围或来源顺序。

确定性切分器负责：

- 从现有 Localization Segment 构造 BeatInput。
- 最终 Shot 数量。
- 每个 Shot 的编辑时长。
- 每个 Shot 的可执行候选路由和模型请求时长建议。
- 裁切时长。
- 旁白分配。
- 来源覆盖。

`commerce_storyboard_reviewer` 负责：

- 商品事实与参考图一致性。
- 旁白逐字一致。
- 动作和销售节拍完整。
- 画面连续性风险。
- 节奏建议。

Reviewer 不得因为旁白总时长超出用户目标而拒绝计划。

### 10.3 幂等与重试

1. 确定性预览、镜头创意生成和审核各自保存独立 attempt。
2. 确定性切分不产生供应商费用。
3. Agent 第 2、3 轮只接收结构化审核问题。
4. 最多 3 轮，禁止无限修正。
5. 相同 UnitGeneration、revision、preview hash 和输入 hash 的已完成镜头创意 Contract 可复用，不因 Workflow 重放再次调用 Agent。
6. Workflow commit 前在同一事务中锁定活动 UnitGeneration，校验项目代、UnitGeneration、`script_unit_revision`、binding revision、Localization hash、ReferencePack hash 和能力快照均未变化。

## 11. 前端改造

### 11.1 生成分镜弹窗

弹窗显示：

```text
视频切分方式
  [智能切分] [单段生成]

当前视频模型
  模型名称
  可用时长：6 / 10 / 12 / 16 秒
  分辨率：当前项目分辨率

规划预览
  用户目标：15 秒
  预计旁白：21.2 秒
  建议视频段：2 段
  预计请求：6 + 10 秒
  预计裁切：1 秒
```

切分方式选择值属于目标 UnitGeneration 配置：

1. 与当前 UnitGeneration 一致时，直接请求确定性预览。
2. 与当前配置不一致时，先展示现有 script-unit rebuild impact。
3. 用户确认后创建并激活目标 UnitGeneration，再对新 generation 请求预览和启动分镜。
4. 前端可以把这三步编排为一个连续弹窗，但后端不能把策略修改和旧 generation 的分镜提交混在同一事务身份中。

旁白超时使用警告样式，并提供：

```text
继续生成
返回修改脚本
```

不增加确认步骤。

### 11.2 分镜列表

卡片显示：

```text
镜头 01
编辑时长：6 秒
模型请求：6 秒
包含 2 个叙事节拍
旁白预计：5.4 秒
```

存在裁切时：

```text
编辑时长：9 秒
模型请求：10 秒
预计裁切：1 秒
```

存在旁白风险时：

```text
旁白预计 11.2 秒，超过镜头 9 秒
```

### 11.3 分镜详情

详情弹窗增加：

- 来源节拍列表。
- 来源脚本段落。
- 旁白及预计时长。
- 编辑时长。
- 模型请求建议。
- 当前能力快照摘要。
- 参考图。
- 图片提示词和视频提示词状态。

### 11.4 脚本详情

每个 ScriptUnit 显示当前活动 UnitGeneration 的切分方式。修改切分方式时：

1. 展示受影响分镜、参考图、视频和成片。
2. 创建新的 UnitGeneration。
3. 保留旧产物。
4. 不自动生成图片和视频。

## 12. 项目删除设计

### 12.1 用户入口

增加两个入口：

1. 项目列表卡片的更多菜单：`删除项目`。
2. 项目设置底部危险操作区：`永久删除项目`。

### 12.2 确认弹窗

显示：

- 项目名称。
- 商品、脚本、分镜、媒体和成片数量。
- 活动任务数量。
- 对象存储预计文件数和大小。
- 删除后 CineWeave 不提供项目恢复和项目级历史查询。
- 供应商计费以 New API 平台记录为准。

用户必须输入完整项目名称。

### 12.3 删除状态机

```text
requested
  -> cancelling_tasks
  -> waiting_for_terminal
  -> deleting_storage
  -> deleting_business_data
  -> completed

任一阶段失败 -> failed_retryable / failed_terminal
```

项目进入 `requested` 后：

1. 默认项目列表立即隐藏。
2. 禁止创建新的 Workflow、Provider Request 和上传。
3. 允许查询删除进度。
4. 删除完成前不重复创建第二个删除请求。
5. 原子写入 `lifecycle_status='deleting'` 并递增 `deletion_revision`，作为所有晚到写入的 fencing token。

### 12.4 删除 Workflow

```text
Analyze Impact
  -> Mark Project Deleting
  -> Cancel Project Workflows
  -> Cancel Provider Async Tasks
  -> Wait Until Terminal With Deadline
  -> Build Storage Deletion Manifest
  -> Delete Object Storage Media
  -> Delete Business Data
  -> Mark Deletion Request Completed
```

等待活动任务必须有可配置截止时间，默认 15 分钟。截止时间内仍存在 `queued/running/cancelling` 任务时，请求进入 `failed_retryable`，不得继续删除业务数据；重试继续复用同一个删除请求和 manifest。

### 12.5 运行状态与写入隔离

新增 `project_deletion_requests`，它是短期运维状态，不是计费或长期审计表。核心字段：

```text
id
project_id
organization_id
workspace_id
project_name
status
deletion_revision
manifest
manifest_cursor
error_code
error_message
requested_by
requested_at
updated_at
completed_at
expires_at
```

`project_id` 只保存被删除项目的 UUID，不建立指向 `projects` 的外键，避免项目行删除时级联删除仍需完成的运行状态。完成或终止记录按 TTL 清理，不建设 `deleted_project_tombstones`。

所有项目写入口必须复用统一的 `requireProjectWritable(projectId, expectedDeletionRevision)`：

1. API 新建、修改和上传入口在项目为 `deleting` 时返回 `PROJECT_DELETION_IN_PROGRESS`。
2. Workflow Activity、Provider 回调、媒体入库和事件投影在 commit 前校验 `lifecycle_status` 与 `deletion_revision`。
3. 删除开始前启动的晚到结果返回 `WORKFLOW_RESULT_DISCARDED`，不得重新创建业务数据或媒体引用。
4. 不能依靠在每个前端按钮上分散禁用来保证隔离。

项目删除允许同步清理或解除 CineWeave 内部的项目进度、任务展示、事件投影和审计上下文，不为这些数据建设 tombstone、计费归档或删除后的项目级查询链路。`provider_call_logs`、`cost_records` 等历史表如因外键策略保留记录，也不作为项目恢复或内部计费凭据；供应商计费、余额与扣费核对统一以 New API 平台记录为准。

### 12.6 对象存储清理

1. 删除前从 `media_files`、`media_variants`、`artifacts` 和业务引用表冻结完整 storage key manifest。
2. manifest 只包含归属于目标项目且没有被其他活动项目引用的对象；共享或无法证明所有权的对象只解除目标项目引用，不直接删除。
3. 逐批删除，保存 cursor 和失败对象。
4. 重试必须幂等，`NoSuchKey` 视为已完成。
5. 对象删除失败时不得把请求标为 completed。
6. 数据库业务行只在存储清理完成后进入最终删除。
7. MinIO/S3 不可用时项目保持 `deleting_storage` 或 `failed_retryable`。

## 13. Public API

### 13.1 分镜规划预览

新增：

```text
POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/generations/{scriptUnitGenerationId}/storyboard-planning-preview
```

请求：

```json
{
  "expectedScriptUnitRevision": 7,
  "expectedProjectProductionGenerationId": "uuid",
  "clientRequestId": "uuid"
}
```

响应：

```json
{
  "scriptUnitGenerationId": "uuid",
  "scriptUnitRevision": 7,
  "strategy": "smart",
  "segmentationPolicyVersion": "commerce-smart-v2",
  "targetDurationSeconds": 15,
  "estimatedVoiceoverSeconds": 21.2,
  "voiceoverExceeded": true,
  "providerDurationOptions": [6, 10, 12, 16],
  "recommendedShotCount": 2,
  "plannedDurations": [6, 9],
  "requestedDurations": [6, 10],
  "estimatedTrimSeconds": 1,
  "videoExecutionEnvelopeHash": "64-char-lowercase-hex",
  "previewHash": "64-char-lowercase-hex"
}
```

该接口只读取指定 UnitGeneration 的冻结 Sales Script Contract、Localization、ReferencePack、Binding 和能力快照，执行确定性预检，不调用供应商。`previewHash` 按 9.2 节列出的完整 canonical 输入计算。Commerce 契约 hash 统一使用 64 位小写十六进制，不使用带 `sha256:` 前缀的另一种编码。

用户在弹窗中选择了与当前 UnitGeneration 不同的策略时，前端先复用现有 script-unit rebuild impact/rebuild API 创建目标 UnitGeneration，再对新 generation 调用本接口；规划接口不得隐式修改活动 UnitGeneration。

### 13.2 创建分镜

扩展：

```text
POST /api/projects/{projectId}/commerce/script-units/{scriptUnitId}/generations/{scriptUnitGenerationId}/storyboard-plans
```

请求增加：

```json
{
  "expectedScriptUnitRevision": 7,
  "expectedProjectProductionGenerationId": "uuid",
  "previewHash": "64-char-lowercase-hex",
  "videoExecutionEnvelopeHash": "64-char-lowercase-hex",
  "clientRequestId": "uuid"
}
```

服务端在事务中锁定并校验 project、UnitGeneration、`script_unit_revision`、binding revision、Localization hash、ReferencePack hash、策略、切分策略版本和能力快照。任一身份或预览发生变化时返回 `409 COMMERCE_STORYBOARD_PREVIEW_STALE`，响应携带当前 generation/revision/hash，要求重新预览；不得把旧预览提交到新的 UnitGeneration。

### 13.3 项目删除

新增：

```text
GET  /api/projects/{projectId}/deletion-impact
POST /api/projects/{projectId}/deletion-requests
GET  /api/projects/{projectId}/deletion-requests/{requestId}
POST /api/projects/{projectId}/deletion-requests/{requestId}/retry
```

移除当前同步硬删除语义。旧 `DELETE /api/projects/{projectId}` 不保留兼容分支。

### 13.4 错误码

新增：

```text
COMMERCE_STORYBOARD_STRATEGY_INVALID
COMMERCE_STORYBOARD_PREVIEW_STALE
VIDEO_EXECUTION_ENVELOPE_UNAVAILABLE
VIDEO_DURATION_PLAN_UNAVAILABLE
PROJECT_DELETION_ALREADY_RUNNING
PROJECT_DELETION_IN_PROGRESS
PROJECT_DELETION_BLOCKED
PROJECT_DELETION_DRAIN_TIMEOUT
PROJECT_DELETION_STORAGE_FAILED
PROJECT_DELETION_FAILED
```

## 14. 实时事件

新增：

```text
commerce.storyboard.segmentation.previewed
commerce.storyboard.strategy.selected
commerce.storyboard.segmentation.completed
commerce.storyboard.creative.generated
commerce.storyboard.plan.committed

project.deletion.requested
project.deletion.tasks_cancelling
project.deletion.drain_timeout
project.deletion.storage_started
project.deletion.storage_progress
project.deletion.business_data_started
project.deletion.completed
project.deletion.failed
```

只有服务端持久化了带 `preview_hash` 的预览 attempt 时才发布 preview 事件，前端输入变化触发的临时本地预估不进入事件总线。

事件必须同步到：

- `packages/events/catalog.yaml`
- 生成事件类型
- 前端 Event Map
- React Query 失效规则
- 任务活动列表

## 15. 数据库迁移

建议使用新的连续 migration，不能修改已部署 migration。

迁移内容：

1. 升级 ScriptUnitGeneration `unit_configuration_snapshot` schema，并增加可选 generated strategy column/index。
2. 扩展 `commerce_storyboard_plans` 的切分计划、VideoExecutionEnvelope、preview hash 和 timing advisory。
3. 扩展 `commerce_shot_contracts` 的创意方向、旁白时长、请求时长提示和 eligible route set hash。
4. 继续复用 `commerce_localization_segments` 与 `commerce_shot_segment_links`，不新增平行 Beat 映射表。
5. 新增 `project_deletion_requests`；其 `project_id` 不建立到 `projects` 的外键，记录按 TTL 清理。
6. 增加项目 `lifecycle_status`、`deletion_revision`、默认列表过滤和统一写入 fencing。
7. 将已有带货分镜标记为 stale，不转换旧分镜结构。
8. 不新增 tombstone；允许清理或解除项目进度、任务展示、事件投影和审计上下文，不以 CineWeave 历史表承担计费追溯。

迁移必须通过：

```text
空库 Up
Up -> Down -> Up
当前 baseline 校验
Provider 配置保护校验
真实外键删除路径集成测试
```

## 16. 实施顺序

### P0：契约与数据模型

- [x] 定义 `StoryboardStrategy`、确定性 BeatInput、SegmentationPlan 和 VideoExecutionEnvelope。
- [x] 新增数据库 migration、模型、OpenAPI schema 和前端类型。
- [x] 增加旧 Commerce StoryboardPlan stale 规则。
- [x] 补齐候选路由、模型时长集合、分辨率和 eligible route set 快照读取。
- [x] 明确复用现有 Localization Segment 与 ShotSegmentLink，不新增平行来源映射。

验收：

- 新旧概念在类型和数据库中不再混用。
- 当前 `{6,10,12,16}` 能力和每个时长的候选路由集合可稳定冻结。

### P1：确定性切分器

- [x] 从 Sales Script Contract 和 Localization 构造 Beat 输入。
- [x] 实现语义、动作、场景和首帧状态可合并判定。
- [x] 实现候选切点和 candidate edge eligible route set。
- [x] 实现版本化加权动态规划切分和稳定 tie-break。
- [x] 实现旁白容量评分。
- [x] 实现单段生成。
- [x] 生成 Shot 和 Provider 请求建议摘要。

验收：

- 15 秒目标和 `{6,10,12,16}` 不再产生五个 6 秒请求。
- 用户目标时长严格为 15 秒。
- 旁白超时只生成建议。

### P2：Storyboard Workflow 和 Reviewer

- [x] 后端从冻结 Localization 确定性生成 BeatInput 和最终 Shot 分组。
- [x] Planner 只为冻结 Shot 分组生成创意字段。
- [x] Reviewer 审核商品事实、旁白和连续性。
- [x] 保存 Shot、现有 SegmentLink、切分计划和能力 hash。
- [x] 保持最多 3 轮审核。
- [x] commit 前锁定并校验 UnitGeneration 和完整 preview identity。

验收：

- Agent 不再自行计算最终模型时长组合。
- 必需来源段落 100% 覆盖；逐字旁白区间连续、无重叠，允许同一 Segment 以不同合法 usage 关联多个 Shot。

### P3：前端

- [x] 新增切分方式卡片。
- [x] 新增无供应商费用的规划预览。
- [x] 分镜卡显示编辑/请求/裁切/旁白时长。
- [x] 分镜详情展示来源节拍和 SegmentLink。
- [x] 切分方式变化接入 UnitGeneration 重建。

验收：

- 用户可明确理解为什么 15 秒会请求 16 秒。
- 不再显示 3 秒镜头承载整段旁白而无警告。

### P4：项目异步删除

- [x] 新增删除影响 API。
- [x] 新增删除请求状态机和 Workflow。
- [x] 接入 Workflow/Provider Task 取消。
- [x] 增加项目 lifecycle/deletion revision 和统一写入 fencing。
- [x] 接入有截止时间的任务 drain。
- [x] 接入有所有权检查的对象存储 manifest 和分批删除。
- [x] 增加项目列表和项目设置入口。
- [x] 增加完成/终止删除请求 TTL 清理，不建设 tombstone 和内部计费归档。

验收：

- 有活动视频任务时可安全发起删除。
- 删除失败可重试。
- 晚到 Workflow、Provider 回调和媒体入库不能恢复已进入删除流程的项目数据。
- 不残留孤立媒体对象。

### P5：全量验证与部署

- [x] 根级测试。
- [x] 隔离 PostgreSQL migration 验证。
- [x] Compose 构建。
- [x] 浏览器端真实项目 smoke。
- [x] 真实供应商付费 smoke 使用显式门禁。
- [x] 分阶段发布 Worker、API、Web。

验收记录（2026-07-25）：

1. `pnpm run test` 全量通过，包含 Go 全仓、migration/seed 校验、Web 测试/typecheck/lint、OpenAPI 405 条路由一致性、Commerce 73 operations/53 events 契约检查和 Compose config。
2. `scripts/test-migrations.ps1` 在独立 PostgreSQL 中完成空库 `Up -> Down -> Up` 与 consolidated baseline 等价验证。
3. `docker compose -f compose.yml --profile app up -d --build` 完成全量构建；随后按 Provider Gateway/Worker、API、Web 顺序重新发布，全部常驻服务 healthy，Web、API 和 MinIO 健康探针均返回 200。
4. 浏览器真实项目 smoke 验证分镜页可展示当前启用方案、编辑时长、供应商请求时长、旁白预计时长、实时提示词任务状态和项目删除入口。
5. 使用 `scripts/smoke-commerce-real-provider.ps1 -Stage reference-prompts -ConfirmProviderSpend` 完成显式授权的真实供应商调用；分镜 Planner/Reviewer Workflow `151757b7-3dae-4efa-9253-5482940a0048` 成功提交，参考图提示词 Workflow `c97bd697-ffb4-4767-a421-b84d23f434b1` 成功，Provider Request `5c351160-0970-4124-bc04-70cc2dd2ef77` 与 Provider Call `0f5c5e33-bb12-4d5e-b668-93dd045d566a` 均具备完整溯源。

## 17. 测试计划

### 17.1 切分器

1. 固定机位、单一连续动作且 `T=15, D={6,10,12,16}, smart` 允许产生一个 16 秒请求。
2. 两个不可合并场景且 `T=15, D={6,10,12,16}, smart` 必须产生两个合法 Shot，不得被请求数权重压成一个 Shot。
3. `T=15, D={6,10,12,16}, single_take` 产生一个 16 秒请求和 1 秒裁切。
4. 五个短 Beat 只有在语义确实不可合并时才产生五个 Shot，不能仅按 Agent 原始分段一对一创建请求。
5. 旁白预计 21.2 秒、目标 15 秒时预览成功并返回警告。
6. 不在完整旁白句子中间切分。
7. 来源段落及逐字旁白区间全部覆盖、连续且无重叠。
8. 输出编辑时长总和严格等于目标。
9. 能力快照、UnitGeneration、script unit revision、Localization 或 ReferencePack 变化后旧 previewHash 失效。
10. 候选模型能力不同的情况下，每个候选边和 Render Segment 只包含完整满足时长与分辨率的路由。
11. 相同输入、策略版本和候选路由顺序始终生成相同计划及 hash。
12. 使用 property-based test 覆盖无序/重复时长集合、无精确组合、单一档位、边界总时长和无合法路由。
13. 分辨率不支持时在任何付费调用前失败。

### 17.2 Workflow

1. Planner 创意字段输出错误最多重试 3 轮。
2. 切分器重试不调用 Planner；已完成的相同 input/preview hash Planner Contract 可复用。
3. Reviewer 不能因为旁白总时长超出而拒绝。
4. Workflow 刷新、重放和 Worker 重启不产生重复计划。
5. 一个脚本失败不影响同项目其他脚本单元。
6. 旧 UnitGeneration、旧 script unit revision 或旧 preview hash 均不能提交到新代。

### 17.3 前端

1. 智能切分和单段生成可切换。
2. 预览准确显示模型时长和裁切。
3. 提交期间按钮显示运行状态。
4. Realtime 完成后分镜列表自动刷新。
5. 页面刷新后可恢复任务状态。

### 17.4 项目删除

1. 无权限用户不能查看影响或发起删除。
2. 项目名称确认错误时拒绝。
3. 活动 Workflow 和 Provider Task 被取消并进入终态。
4. 删除请求幂等。
5. drain 超时后进入 `failed_retryable`，不得继续删除业务数据。
6. S3 删除部分失败后可续传。
7. 共享 storage key 不被误删，`NoSuchKey` 重试可成功。
8. 项目业务数据最终清理。
9. 删除中的项目不能启动新任务。
10. 晚到 Workflow、Provider 回调和媒体入库因 deletion revision 不匹配被拒绝。
11. 默认项目列表不返回 deleting 项目。
12. 完成后的短期 deletion request 可查询并最终按 TTL 清理。

## 18. 验收命令

```powershell
go test ./...
pnpm --filter @cineweave/web test
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
python -c "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))"
python scripts/check-openapi-routes.py
python scripts/check-commerce-development-contract.py
docker compose -f compose.yml config --quiet
pnpm run test
```

运行态验证：

```powershell
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

## 19. 完成定义

本计划完成需要同时满足：

1. 用户可以按广告脚本选择智能切分或单段生成。
2. 用户目标时长始终是最终编辑时长。
3. 旁白时长只提示，不阻断。
4. 当前视频模型时长集合实际参与切分规划。
5. 15 秒广告不会再默认创建五个最少 6 秒的供应商任务。
6. 分镜页面可解释显示编辑时长、请求时长和裁切。
7. 项目可通过前端发起安全异步删除。
8. 删除过程可观测、可重试，且删除开始后的晚到结果不能恢复项目数据。
9. 全仓测试、迁移、Compose 和真实 smoke 均通过。

## 20. 实施假设

1. 切分方式按 ScriptUnitGeneration 保存，不是项目级永久设置。
2. 默认策略为 `smart`。
3. 当前优先保证 `single_frame_i2v` 完整可靠。
4. Provider Gateway 可以按已冻结 RenderPlan 执行时间轴尾部裁短，但不得对画面做空间裁边、拉伸或改画幅，也不得改变用户目标总时长。
5. 项目删除为不可恢复的异步硬删除，不提供跨项目回收站。
6. 项目删除可清理 CineWeave 内部进度与审计上下文，不保留内部计费归档或长期项目关联；供应商计费以 New API 平台为准。
7. RenderPlan 只能在每个 Segment 冻结的 eligible route set 内回退，不能临时切换到时长或分辨率不匹配的模型。
8. 不做旧带货分镜兼容；旧计划标记 stale 后重新生成。
