# CineWeave Codex 当前执行计划

本文档是 Codex 执行入口，只记录当前状态、优先级和验收入口。详细通用任务拆解维护在 `docs/follow-up-development-plan.md`；视频生产重构的目标和完成证据分别维护在 `docs/video-production-workflow-continuity-refactor-target.md` 与 `docs/video-production-workflow-continuity-refactor-progress.md`。

## 根决策

- 仓库根目录固定为 `D:\Code\CineWeave`，不创建嵌套项目目录。
- 项目处于开发阶段，不兼容旧 demo、旧项目生产数据或旧 TypeScript 供应商脚本。
- 已配置的供应商、凭据、模型、能力、业务模型绑定和 Provider 历史必须保留。
- API Server 和 Worker 不得直接调用上游供应商或解密凭据；所有 AI 调用必须经 Provider Gateway。
- 服务器部署优先使用 `docker compose -f compose.yml --profile app up -d --build`，不优先制作 `.exe`。
- 数据库只新增前向 migration；生产环境不执行 down/reset。

## 当前状态

- Monorepo、Compose、API、Provider Gateway、Temporal Worker、Realtime、Web、MinIO 和 Event Publisher 已进入可运行 MVP 阶段。
- 根测试入口覆盖 Go、Web typecheck/lint、OpenAPI YAML/route、Event catalog 和 Compose config；`mock-provider` 已从 CI 移除。
- Compose 默认只映射 Web `19285`、API `19288`、Realtime `19281` 和 MinIO API `19290`；PostgreSQL、Redis、NATS、Temporal、Provider Gateway、Workers、Event Publisher 和 MinIO Console 只走 Docker 网络。
- 当前未提交变更的 15 项缺陷修复已完成实现与自动化验证；详细证据见 `docs/uncommitted-change-bug-verification-fix-checklist.md`。
- 工作树中前向 migration 已编号至 `000044`。`000040` 视频执行身份、`000042` 原文到剧本分集 staging、`000043` 镜头衍生资产持久执行和 `000044` 供应商模型删除历史能力证明保留均已通过隔离数据库验证，并已应用到主数据库。
- Prompt Registry seed 为 v2。旧 `projects.production_mode`、旧通用视频 Prompt 和跨镜头硬尾帧链已删除。
- 四个最新视频生产 Profile v2 均为 `published + available`：图生视频、首尾帧衔接、多模态参考和分镜板。
- 项目创建原子写入不可变 Profile binding 和 active production generation；切换方案只能经过影响检查、多分集受控重建和 generation 切换。
- 视频生产使用 Profile Compiler、ShotState、Transition、VisualAnchor、typed ReferencePack、PromptContextPlan、approved VideoPromptPlan、RenderPlan 和 capability attestation；旧 generation 晚到结果不能写入当前代。
- 视频 Prompt 生成/审核与视频执行已经拆分。视频执行只消费已审核 Prompt Plan，不会逐镜头重复运行 Prompt Agent。
- Project/Episode/Batch 分层 Workflow、持久 checkpoint、部分完成、失败项重试、取消、Continue-As-New 和 Pinned Worker Deployment 已落地。
- 10 集并发和 70 分钟长分集压测、四 Profile contract load、Workflow replay、Provider runtime contract、migration Up/Down/Up 和 Provider 数据保护测试已通过。
- 根入口 `pnpm run test` 已通过：Go 全仓、migration/seed validate、Web test/typecheck/lint、OpenAPI 323 条路由、Event catalog 和 Compose config 均通过。
- 2026-07-22 已在排空活动 Workflow/视频任务/租约后重建主环境 Compose，主数据库迁移至 v44，Provider 发布前后快照等值，所有常驻服务健康，Web/API/Realtime/MinIO 端点返回 200。真实供应商与完整业务页浏览器 smoke 尚未执行。

## 下一阶段顺序

### P0：本轮运行态验收收尾

1. 执行真实文本/图片/视频供应商 smoke，确认多凭据选择、任务轮询、媒体入库和错误归一化。
2. 通过浏览器验证供应商多凭据、分集剧本、镜头衍生资产、分镜视频与任务活动投影。
3. 保留旧 Pinned Worker，直到 v1 任务排空指标达到 `safeToDecommission=true`。

### P1：主流程产品闭环

1. 完成原文、剧本、资产、分镜、视频、时间线和成片之间的状态传播与可恢复操作。
2. 收口真实供应商 capability probe、精确 snapshot attestation、模型限制可视化和错误中文化。
3. 补齐真实音视频供应商 smoke、Range 播放、媒体缓存和长任务故障恢复演练。
4. 继续强化 Project Agent 的工具覆盖、审批模式、交互提问、子 Agent 和长任务动态输出。

### P2：管理面与运营能力

1. 完成 Prompt Registry、项目手册、RBAC、成员/角色/权限的可视化管理闭环。
2. 完成全站中文枚举审计，禁止正常用户界面直接显示内部英文值。
3. 实现 RenderPlan 成本/时间预演、预算审批和发布后的可观测性看板。
4. 详细步骤按 `docs/follow-up-development-plan.md` 逐项更新，不在本文复制长任务清单。

## 验收命令

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

pnpm run test
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
python -c "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))"
python scripts/check-openapi-routes.py
go run ./cmd/events-gen --check
docker compose -f compose.yml config --quiet
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

视频生产或 Provider 相关发布还必须执行：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode DrainCheck
pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode Snapshot -SnapshotPath tmp/provider-protection-before-release.json
# 部署完成且 API 仍处于 Provider 配置冻结状态时执行
pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode Verify -SnapshotPath tmp/provider-protection-before-release.json
```

## 维护规则

- 本文只写当前执行总览、优先级和验收入口。
- 详细任务步骤维护在 `docs/follow-up-development-plan.md`，专项架构与证据维护在对应 target/progress 文档。
- 每完成一个阶段，同步更新“当前状态”和“下一阶段顺序”；不得把未来能力写成已实现。
