# CineWeave 带货视频 v1 实现审计

- 审计日期：2026-07-23
- 目标文档：`docs/commerce-video-development-plan.md`
- 源码 migration head：`000057_commerce_script_contracts_and_unit_rebuilds.sql`
- 主环境 migration head：`000057_commerce_script_contracts_and_unit_rebuilds.sql`
- OpenAPI 基线：400 条公开路由

## 1. 结论

Phase A-E 已完成实现并通过自动化、主环境部署、真实供应商和运行态浏览器验收。主环境已从 v44 前向迁移至 v57，Commerce 付费终验项目完成 3 张参考图和 3 个镜头视频，所有业务项、Workflow、Artifact、MediaFile、Provider Async Task 与 Cost Record 均可追溯。

`pnpm run release:check` 已在 2026-07-23 完整通过；最终增量又通过 `pnpm run test`、Commerce Chromium 9/9、Web production build、定向 Compose 重建和浏览器 smoke。真实付费调用只通过 API、Workflow 和 Provider Gateway 发起，没有绕过 Gateway 或把凭据写入证据。

## 2. Phase A：领域边界和数据库

| 任务 | 状态 | 主要实现证据 | 自动化证据 |
| --- | --- | --- | --- |
| A1 ProjectKind 与类型组合 | 已完成 | `internal/commerce/project_kind.go`、`db/migrations/000045_commerce_project_identity.sql`、`internal/api/project_kind_guard.go` | `internal/commerce/project_kind_test.go`、`internal/api/project_kind_guard_test.go`、`internal/dbmigrate/commerce_project_identity_test.go` |
| A2 Commerce 核心 schema | 已完成 | `000045-000048`，包含 ProductVersion、ScriptUnit、UnitGeneration、Segment、Storyboard、Timeline/Final 和 checkpoint | 对应 `internal/dbmigrate/commerce_*_test.go` 与隔离 Up/Down/Up |
| A3 Template/Binding/Setup/Repository | 已完成 | `internal/commerce/catalog_service.go`、`repository.go`、`setup_repository.go`、`000049-000052` | `catalog_integration_test.go`、`commerce_setup_runs_integration_test.go` |
| A4 Binding 对齐、Fence、换代和 stale | 已完成 | `internal/commerce/runtime.go`、`rebuild.go`、`script_unit_rebuild.go`、API ProjectKind guard | `repository_integration_test.go`、`commerce_script_unit_rebuild_integration_test.go` |
| A5 typed subject 与 Run/Item 状态机 | 已完成 | `internal/commerce/production_runs.go`、`production_run_repository.go`、`000048` | `production_runs_test.go`、`commerce_production_runs_test.go` |
| A6 多语言模型能力契约 | 已完成 | `internal/provider/language_capabilities.go`、Provider catalog/routing、前端可视化模型编辑；视频只把时长与分辨率作为能力硬门槛，不设置人工能力审批 | `language_capabilities_test.go`、Commerce Gateway contract tests |
| A7 OpenAPI、幂等与数据库集成 | 已完成 | `packages/openapi/openapi.yaml`、Commerce API client/types、幂等请求契约 | OpenAPI 400 路由检查、`commerce_projects_integration_test.go`、`idempotency_integration_test.go` |

## 3. Phase B：新建项目和商品脚本闭环

| 任务 | 状态 | 主要实现证据 | 自动化证据 |
| --- | --- | --- | --- |
| B1 Commerce 项目入口和专用表单 | 已完成 | `new-project-page.tsx`、`projects-page.tsx`、Commerce labels/routes | Playwright 创建表单用例 |
| B2 可恢复 Setup 与 abandon | 已完成 | `commerce_setup_runs`、retry attempts、Setup API/Workflow | `commerce_setup_runs_integration_test.go`、`commerce_setup_workflow_test.go` |
| B3 ProductVersion、Product Rebuild 与安全图片 | 已完成 | `product_service.go`、`product_upload_repository.go`、`product_rebuild.go`、Commerce catalog API | product/service、API 与 migration 集成测试 |
| B4 多 ScriptUnit 管理 | 已完成 | `script_service.go`、`commerce_scripts.go`、`commerce-materials-page.tsx` | repository/service/API tests；分页列表使用 cursor 与浏览器原生虚拟化 |
| B5 语言解析与 Localization | 已完成 | `commerce_generation_runtime.go`、自动语言免确认策略、Localization API/UI | Setup/Workflow tests 和语言能力 contract tests |
| B6 时长建议与付费前模型能力预检 | 已完成 | `project_options.go`、`preparation.go`；用户选择的目标时长为生产权威值，旁白估算仅持久化节奏建议且不阻断；分镜编辑时长精确组成用户目标，Provider Gateway 再映射到可覆盖的供应商请求档位并按 Render Plan 裁入时间线；视频模型时长/分辨率仍 fail closed，语言、参考模式、画幅和原生音频作为路由与结果提示信号 | `service_test.go`、`commerce_setup_workflow_test.go`、`commerce_sales_script_workflow_test.go`、`commerce_video_workflows_test.go`、Provider/Workflow contract tests |
| B7 ProjectKind 导航分流 | 已完成 | 项目 layout、`routes.ts`、Commerce 专用页面 | Playwright Commerce-only navigation |
| B8 Sales Script Organizer 持久合约 | 已完成 | `000057`、`commerce_sales_script_runtime.go`、`commerce_sales_script_workflow.go` | Organizer replay/claim/reviewer tests |
| B9 ScriptUnit 候选版本与原子换代 | 已完成 | `script_unit_rebuild.go`、repository、Preparation Workflow | 真实 PostgreSQL 回滚/提交测试、Fence 测试 |
| B10 revision/hash 语言结果保护 | 已完成 | Setup/Generation runtime 的输入 hash、自动解析结果校验与重放保护 | `commerce_setup_workflow_test.go` |

## 4. Phase C：分镜规划与参考图

| 任务 | 状态 | 主要实现证据 | 自动化证据 |
| --- | --- | --- | --- |
| C1 Prompt Registry 与 Workflow Template | 已完成 | `000049_commerce_prompt_registry.sql`、`000050_commerce_workflow_v1_seed.sql` | `commerce_seed_test.go` |
| C2 多语言 Agent Contract 与分镜 Workflow | 已完成 | `commerce_contracts.go`、Setup/Generation workflows | Setup、Sales Script 和 storyboard workflow tests |
| C3 Shot/Segment/Product Reference 投影 | 已完成 | `storyboard_service.go`、`000047_commerce_storyboard_contracts.sql` | `commerce_storyboard_contracts_test.go` |
| C4 单元分镜筛选、编辑、排序和激活 | 已完成 | `commerce_storyboards.go`、`commerce-storyboard-page.tsx` | edit revision migration tests、API tests、ScriptUnit E2E |
| C5 Prompt、并发参考图和 Fidelity Review | 已完成 | reference-image runtime/persistence/workflows、batch coordinator | reference image workflow/provider contract tests |

## 5. Phase D：视频提示词与镜头视频

| 任务 | 状态 | 主要实现证据 | 自动化证据 |
| --- | --- | --- | --- |
| D1 Video Prompt Agent/Reviewer | 已完成 | `commerce_video_runtime.go`、Prompt/Reviewer contract 与 3 轮上限 | `commerce_video_workflows_test.go` |
| D2 immutable approved VideoPromptPlan | 已完成 | `commerce_video_persistence.go`、`000055_commerce_video_runtime.sql` | migration 与 Workflow contract tests |
| D3 编译到既有 Render Plan | 已完成 | Provider video production contract/planner 与 Commerce execution identity | Provider Gateway mock contract tests |
| D4 并发、父协调器、checkpoint、取消和恢复 | 已完成 | `commerce_batch_coordinator.go`、production run repository/workflows | coordinator、production run、video workflow tests |
| D5 ScriptUnit 视频页与详情 | 已完成 | `commerce-video-page.tsx` | TypeScript/lint/build 与 ScriptUnit 页面隔离 E2E |

## 6. Phase E：成片、助手和发布

| 任务 | 状态 | 主要实现证据 | 自动化证据 |
| --- | --- | --- | --- |
| E1 单元时间线、叠加层和成片 | 已完成 | `commerce_final_persistence.go`、`commerce_final_workflow.go`、`commerce-final-page.tsx` | timeline migration、media text overlay 和 final workflow tests |
| E2 Project Agent Commerce 工具 | 已完成 | `internal/agent/commerce_tools.go`、`internal/api/agent_commerce_tools.go` | Agent Commerce tool/API tests |
| E3 事件、实时失效和任务活动 | 已完成 | `packages/events/catalog.yaml`、Commerce API/Workflow event helpers、`event-map.ts` | Event Catalog 全 payload 测试、API helper 合约测试、events-gen check |
| E4 E2E、Provider mock、真实 smoke | 已完成 | Playwright、mock、零费用预检、显式费用门控、Run Item Provider provenance 和运行态浏览器验收 | Playwright 9/9；真实参考图 3/3、镜头视频 3/3；浏览器媒体 `readyState=4`；最终证据 `tmp/commerce-real-provider-smoke-final-20260723-204053.json` |
| E5 文档、CI、release baseline | 已完成 | 执行计划、release readiness、CI、release scripts、migration baseline | `pnpm run release:check` |
| E6 Commerce 专项门禁 | 已完成 | `test-runtime-hardening.ps1 -CommerceOnly`、migration bundle 和真实 PostgreSQL测试 | CommerceOnly、Up/Down/Up、baseline 等价全部通过 |

## 7. 当前验证记录

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `pnpm --filter @cineweave/web test` | 15/15 通过 |
| `pnpm --filter @cineweave/web typecheck` | 通过 |
| `pnpm --filter @cineweave/web lint` | 通过 |
| `go run ./cmd/events-gen --check` | 通过 |
| OpenAPI YAML 与 Go route check | 400 条路由一致 |
| `pwsh -NoProfile -File scripts/test-runtime-hardening.ps1 -CommerceOnly` | 通过 |
| `pnpm run test:commerce:e2e` | Chromium 9/9 通过 |
| `pwsh -NoProfile -File scripts/test-commerce-smoke-script.ps1` | 通过；语法、零费用预检、费用 fail-closed 和 provenance 证据字段完整 |
| `pnpm run release:check` | 通过；含 vet、漏洞/依赖审计、隔离迁移、生产构建和镜像构建 |
| `pnpm run test`（最终增量） | 通过；Go 全仓、migration/seed/baseline、Web 15/15、typecheck、lint、OpenAPI 400、Commerce 72 operations/48 events 和 Compose config 全部通过 |
| 主环境部署 | migration v57；API、Web、Provider Gateway、Script Worker、Agent Worker 及其余常驻服务健康；API/Web/MinIO 均为 HTTP 200 |
| 真实参考图 | 3/3 成功，证据 `tmp/commerce-real-provider-smoke-20260723-103918.json` |
| 真实镜头视频 | Run `cefc3751-d491-47a8-abd6-7d57b7f628af`、Workflow `c8345352-479b-40a1-809f-8efd3d087a42` 均 succeeded；3/3 Artifact/MediaFile/Render Plan 完整 |
| Provider 重试与成本 | 3 个最终异步任务成功；4 个 `UPSTREAM_TIMEOUT` 尝试被隔离并仅重试对应镜头；3 条 `video.generate` Cost Record 存在 |
| 运行态浏览器 | 商品与脚本、分镜、视频、成片、设置和任务活动均可打开；分镜 3 条、视频 3/3，媒体实际可播放，无错误边界、旧失败通知、内部工作流枚举或能力审批文本 |

## 8. 发布结果与已知限制

本次授权窗口已完成以下发布动作：

1. 排空 Workflow、Provider async task 和 Commerce production run 后完成 v44→v57 前向迁移及 Compose 重建。
2. 完成零费用预检、真实参考图、视频提示词、三镜头视频和失败镜头重试。
3. 保存不含凭据、Prompt、签名 URL 或媒体字节的预检、参考图和最终视频证据。
4. 在浏览器复核 Commerce 专用导航、商品与脚本、分镜、视频媒体、项目设置和任务活动。
5. 最终代码再次通过全仓测试、Web 构建和 Commerce E2E。

当前真实项目为 `zh-CN`，视频结果准确标记 `audio_unverified`。若要对外声明某个非中文 locale 的原生音频已完成付费实测，仍需按 `docs/release-readiness.md` 另开费用确认窗口执行对应 locale smoke；该项不阻断当前简体中文 MVP。当前 Provider 价格元数据为 0，因此 Cost Record 已记录调用和数量，但金额为 0，后续配置价格后才可用于真实成本汇总。
