# 剧本到分镜时长重构执行进度

- 执行方案：`docs/script-to-storyboard-timing-refactor-plan.md`
- 当前范围：P0-P5
- 延后范围：P6 成本预估与执行审批
- 状态：P0-P5 已完成，P6 成本估算已进入后续计划
- 最近更新：2026-07-13

状态说明：

- `[ ]` 未开始或未完成
- `[~]` 执行中
- `[x]` 已完成且已有验证证据

## P0 基线与观测

- [x] P0-1 建立 6,355 字、16 镜头、410 秒固定回归 fixture。
- [x] P0-2 建立旧逻辑 410 秒被截成 240 秒的错误表征测试。
- [x] P0-3 记录 raw/planned/stored duration 指标。
- [x] P0-4 记录模型请求时长、实际媒体时长、帧率、帧数和音频流观测。

## P1 时间模型与确定性算法

- [x] P1-1 实现 `000057_storyboard_timing_model` 及 down migration。
- [x] P1-2 新建 `internal/storyboard` 纯算法包。
- [x] P1-3 实现 3/3.5/4 字每秒对白、动作、停顿和并行 TimingBlock 计算。
- [x] P1-4 实现 90k ticks、默认 24 FPS 和帧边界量化。
- [x] P1-5 实现合法切点、动态规划拆镜和跨镜头区间覆盖校验。
- [x] P1-6 实现 active StoryboardPlan 唯一约束、原子激活和 stale 传导。
- [x] P1-7 移除字符镜头估算、24 镜头上限、15 秒截断和旧创作时长真值。

## P2 Agent 与计划版本

- [x] P2-1 实现 Timing Analyzer 结构化合约。
- [x] P2-2 实现 Episode Continuity Blueprint 合约与校验。
- [x] P2-3 实现分场景/分 TimingBlock Shot Planner。
- [x] P2-4 实现 Reviewer 与确定性修正闭环。
- [x] P2-5 实现 StoryboardPlan draft/review/activate/archive 版本流程。
- [x] P2-6 每个场景完成后独立写库并支持独立重试。
- [x] P2-7 前端接入时间分析、计划版本和帧级时长。

## P3 Provider Gateway 视频执行计划

- [x] P3-1 实现条件化 `videoGenerationVariants[]` 能力结构与可视化配置。
- [x] P3-2 实现 `/internal/provider/video/plan`。
- [x] P3-3 实现连续范围、离散时长量化和 capability snapshot。
- [x] P3-4 实现 `RENDER_PLAN_REPLAN_REQUIRED` 与 `STORYBOARD_REPLAN_REQUIRED`。
- [x] P3-5 实现 `000058_video_render_plans` 及 down migration。
- [x] P3-6 视频创建、轮询、取消、转存按 RenderSegment 执行。
- [x] P3-7 跨模型家族 fallback 以整个镜头为最小重生成单元。

## P4 原生音视频与长任务

- [x] P4-1 默认启用 `native_av` 和 `audioRequirement=preferred`。
- [x] P4-2 视频 Prompt 按片段携带准确中文对白、表演和音效。
- [x] P4-3 FFprobe 记录音视频流、时长、帧率、帧数、sample rate/count。
- [x] P4-4 保存原始 AV、无音轨 mezzanine 和提取音轨。
- [x] P4-5 实现 native audio 状态与预览/成片门。
- [x] P4-6 实现全局 Blueprint、依赖分组 Child Workflow 和安全 Continue-As-New。
- [x] P4-7 实现 Temporal Worker Deployment Versioning、取消传导和状态恢复。
- [x] P4-8 实现部分完成、同家族失败项重试和跨家族整镜重试。
- [x] P4-9 活动任务、助手和页面接入持久实时事件。

## P5 TTS 与质量校准

- [x] P5-1 实现角色声音库和 TTS Provider Gateway runtime。
- [x] P5-2 使用 TTS 实际音频时长创建 timing revision，不应用当前语速限制。
- [x] P5-3 实现对白、环境声、音效和音乐轨混音。
- [x] P5-4 实现 ASR/强制对齐原生中文台词审核。
- [x] P5-5 用实际结果校准标点停顿、动作时长和镜头节奏。
- [x] P5-6 实现项目级音频配置 revision、完整 stale 传导和晚到 TTS/ASR/混音结果隔离。

## 全量验收

- [x] V-1 `go test ./...`
- [x] V-2 Web typecheck 与 lint。
- [x] V-3 OpenAPI YAML 与路由一致性检查。
- [x] V-4 Compose config、镜像重建和服务健康。
- [x] V-5 410 秒 fixture、跨模型时长、原生音频门和 70 分钟长任务 smoke。

## 验证记录

| 时间 | 项目 | 证据 |
| --- | --- | --- |
| 2026-07-12 | 执行前基线 | `go test ./internal/workflows ./internal/provider` 通过 |
| 2026-07-12 | P0-1/P0-2 | `go test ./internal/workflows -run TestStoryboardDurationRegressionFixturePreservesLongShots -count=1` 通过；fixture 断言 410 秒长镜头时长在规范化和入库度量中无损保留 |
| 2026-07-12 | P0-3 | Storyboard artifact metadata、事件和 node output 写入 raw/planned/stored duration；定向 workflow 测试通过 |
| 2026-07-12 | P0-4 | `000056_video_media_observability` 增加结构化观测字段；Gateway 使用 FFprobe 记录 requested/provider/actual duration、帧率、帧数与音频流；`go test ./internal/media ./internal/provider ./internal/workflows` 和真实 PostgreSQL Gateway 集成测试通过 |
| 2026-07-12 | P1-2 至 P1-5 | 新增 `internal/storyboard`；覆盖普通 3.5、慢 3、快 4 字/秒、显式停顿、动作范围、并行块取最大值、90k/24 FPS、合法切点、DP 拆镜和跨镜头 span 守恒；`go test ./internal/storyboard -count=1` 通过 |
| 2026-07-12 | P1-6 | 新增 `ActivateStoryboardPlanTx`；激活前校验帧对齐、镜头连续区间、Timing Unit 精确 span 覆盖，原子归档旧计划并传导 shot/timeline/clip/final stale；在独立 PostgreSQL 迁移库运行真实集成测试通过 |
| 2026-07-12 | P1-1/P1-7 | `000057` up/down 已通过独立库事务验证；API、审阅修复、Media Worker、导出与前端/OpenAPI 全部改用 tick 创作真值，时间线与合成真实 PostgreSQL 集成测试通过；媒体 FFprobe 秒数和改编目标秒数按用途保留 |
| 2026-07-13 | P2-1 至 P2-4 | 四类严格 Agent JSON 合约、确定性语义时长、连续性 DAG、分场景 DP Planner、图片台词隔离、资产 ID 校验和 Reviewer 定点修正均已实现；Temporal 虚拟时间测试验证依赖场景等待、独立场景并行、审核只重跑指定场景 |
| 2026-07-13 | P2-5/P2-6 | 新增按集 Parent/按场景 Child Workflow、场景原子写库、计划查询/激活与 split/merge/timing 派生 revision；独立 PostgreSQL 从 000001 全量迁移到 000057 后，真实测试验证 revision 2/3/4、精确 Timing Span 覆盖和唯一 active plan；完整活动链路验证 Timing/Blueprint/Scene Plans/Review/Artifact/资产需求入库 |
| 2026-07-13 | P2-7 | 分镜页接入时间分析、计划版本、激活、帧对齐拆镜/合镜/时长 revision 和场景进度；历史版本只读；Web typecheck/lint 通过，真实 PostgreSQL API 测试验证计划镜头无法被旧 PATCH/DELETE/reorder 接口绕过版本机制 |
| 2026-07-12 | P3 | `videoGenerationVariants` 可视化配置、Gateway RenderPlan、连续/离散时长量化、不可变能力快照、RenderSegment 执行和同家族/跨家族重试边界完成；Provider/Workflow 定向测试通过 |
| 2026-07-12 | P4 | 原生 AV、逐片段中文台词、FFprobe、raw/mezzanine/audio 三类产物、音频审核门、长任务 Child Workflow/Continue-As-New、Worker Build ID、持久实时事件完成；激活与导出门测试通过 |
| 2026-07-12 | P5 | `000060_audio_postproduction`、OpenAI-compatible TTS/ASR Gateway、角色声音库、实际 TTS timing revision、48k 双轨混音、ASR 对齐审核和校准 profile 完成；全新 PostgreSQL 迁移与 down/up、Gateway/API 集成、FFmpeg 和 Temporal 部分成功测试通过 |
| 2026-07-12 | P5 收口 | 项目音频策略继承统一；默认旁白自动建立、切换和归档接替通过真实 PostgreSQL API 测试；ASR 调用失败会原子阻断 segment/plan/shot 并写实时事件；额外音轨范围校验和 OpenAPI 枚举已对齐 |
| 2026-07-13 | P5-6 音频配置一致性 | 新增 `000061_audio_configuration_revisions`；声音、TTS/ASR 模型或音频策略变化会递增项目 revision，并事务性失效旧 TTS、混音、TTS timing、分镜计划、原生音轨审核与最终成片；真实 PostgreSQL 测试验证显示名修改不误触发、生成参数修改完整传导 stale、晚到 TTS 媒体仅保留溯源且不能 active 或进入校准/混音 |
| 2026-07-12 | V-1/V-2/V-3 | `pnpm run test` 通过：全仓 Go 测试、Web typecheck、Web lint（0 error，3 条既有 `<img>` warning）、OpenAPI YAML parse、252 条路由一致性与 Compose config 全部成功 |
| 2026-07-12 | 迁移可靠性 | 本地 PowerShell/Shell 迁移入口统一使用 `--single-transaction` 并严格传播失败；空数据库从 000001 至 000060 成功执行，000060 down/up 成功 |
| 2026-07-13 | 迁移 UTF-8 与 000061 | PowerShell 入口固定 UTF-8 无 BOM 原生管道，消除中文 seed 在 Windows 下被破坏的问题；空数据库从 000001 至 000061 成功执行，000061 down/up 和 6 个 revision 字段检查通过 |
| 2026-07-12 | V-4 | `docker compose -f compose.yml --profile app up -d --build` 成功；Web/API/Realtime/Provider Gateway/Script Worker/Media Worker/Audio Worker 均 Up/healthy；公开端口 Web/API/Realtime/MinIO HTTP 200 |
| 2026-07-12 | Temporal Worker Deployment | 移除已被 Temporal 1.28 禁用的 Build ID Version Sets API，迁移到 Deployment Versioning；三个 worker 独立 deployment 的 `cineweave-dev` 均确认为 current，启动不再重启 |
| 2026-07-12 | V-5 | 410 秒 fixture、连续/离散模型时长、对白切分保护、同家族片段重试、跨家族整镜重做、70 分钟 Continue-As-New、真实 PostgreSQL 原生音频预览/激活/导出门全部通过；项目设置页面加载及默认旁白弹窗通过浏览器 smoke，控制台 0 error |
| 2026-07-13 | 最终收口 | `pnpm run test` 全部通过；Compose 全量重建成功，Web/API/Realtime/Provider Gateway/三个 Worker 均 healthy 且 restart=0，四个公开 HTTP 健康检查均为 200；Temporal 三个 Worker Deployment 的 `cineweave-dev` 均为 current；浏览器确认项目音频策略、TTS、ASR 和角色声音配置正常渲染，控制台 0 error |
