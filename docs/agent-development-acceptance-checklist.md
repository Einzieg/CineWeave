# CineWeave 项目控制型 AI 助手验收单

本文档根据 `dev_docs/agent开发.md` 生成，用于跟踪 Project Agent Runtime 的完整开发与验收状态。每完成一项，必须在当前代码、测试或运行结果中找到直接证据后再勾选。

## 判定规则

- `[x]` 表示当前仓库已有实现，并且能从代码或测试中找到直接证据。
- `[ ]` 表示尚未实现，或只是局部雏形，不能满足 `dev_docs/agent开发.md` 的最终验收语义。
- “部分完成”保留未勾选，说明当前已有基础和缺口。
- 本项目仍处于开发阶段，不做旧 demo、旧数据或旧 TypeScript 供应商脚本兼容。
- Agent 不得绕过 RBAC、Provider Gateway、成本预算、Review Center 和人工审批边界。

## 0. 当前基线

- [x] 已读取 `dev_docs/agent开发.md` 并将任务拆入本验收单。
- [x] 当前仓库已有 `agent_sessions`、`agent_messages`、`agent_runs` 三张基础 Agent 表。
- [x] 当前 Script Agent 聊天已通过 Provider Gateway 进行文本规划和最终回复。
- [x] 当前 Script Agent 已有后端白名单工具雏形，包括项目状态、来源、章节、事件、剧本、资产、分镜、工作流读取，以及启动生产动作和取消工作流。
- [x] 当前已实现的 Script Agent 工具会复用现有 RBAC Authorizer 做权限校验。
- [x] 当前前端助手消息列表能展示工具执行记录，不直接显示原始 JSON。
- [x] 当前前端助手可作为工作台右侧常驻面板打开和关闭，会话不会因关闭面板而丢失。
- [x] 当前已具备独立 Project Agent Runtime：`agent_tasks` / `agent_steps` / `agent_approvals`、Project Agent API、正式 Tool Registry 和执行器已脱离旧 `script-agent` 聊天链路。

## 1. Agent Runtime 架构

- [x] 新增 `internal/agent` 包，拆出 registry 和 tool types。
- [x] 定义统一 `ToolRisk` 风险等级：`read`、`draft`、`write`、`workflow`、`costed`、`destructive`、`admin`。
- [x] 定义统一 `AgentTool` 接口，包含名称、描述、风险、所需权限、输入 schema、dry-run、execute、verifier。
- [x] 定义统一 `ToolContext`，携带 principal、organization、project、session、task、step、预算和 idempotency 上下文。
- [x] 已将现有 `internal/api/agent_tools.go` 的工具雏形封装到 Project Agent Tool Registry；API 执行器通过 `AgentTool.Execute` 调度，不再直接绕过 registry switch。
- [x] Planner 只负责把用户目标转成结构化 JSON Plan，不直接执行工具。
- [x] Executor 只调用白名单 Tool；当前实现通过正式 registry 校验工具名，并复用现有 API 核心函数执行项目操作。
- [x] Verifier 单独负责执行后校验，不把成功仅等同于函数返回无错误。
- [x] Agent Runtime 所有模型调用都必须走 Provider Gateway。
- [x] Agent Runtime 所有项目操作都必须代表当前用户执行，不能绕过 RBAC。

## 2. 数据库模型

- [x] 新增 `agent_tasks` 表，记录项目级 Agent 任务、目标、状态、计划、摘要、创建人和生命周期时间。
- [x] 新增 `agent_steps` 表，记录每个计划步骤、工具名、风险、状态、输入、dry-run 输出、监督决策、输出和错误。
- [x] 新增 `agent_approvals` 表，记录审批请求、审批状态、审批人和审批载荷。
- [x] 可选新增 `agent_tool_invocations` 表，或明确由 `agent_steps` 承担工具调用审计。
- [x] `agent_runs` 增加 `task_id`、`step_id`，使 Planner、Supervisor 和工具执行中的 LLM 调用能追溯到任务步骤。
- [x] 为 `agent_tasks`、`agent_steps`、`agent_approvals` 增加必要索引和更新时间触发器。
- [x] 为 Agent 控制表补齐 down migration。
- [x] 编写数据库迁移测试或 API 集成测试证明新增表可写、可查、可关联。

## 3. API 与 OpenAPI

- [x] `GET /api/projects/{projectId}/agent/tools`：返回当前用户可见工具、风险、权限、输入 schema 和是否需要审批。
- [x] `POST /api/projects/{projectId}/agent/tasks`：创建项目控制任务，支持 `plan_only`、`supervised`、`auto_low_risk` 模式。
- [x] `GET /api/projects/{projectId}/agent/tasks`：列出项目 Agent 任务。
- [x] `GET /api/projects/{projectId}/agent/tasks/{taskId}`：返回任务详情、计划、步骤、审批和最近事件。
- [x] `POST /api/projects/{projectId}/agent/tasks/{taskId}/cancel`：取消 Agent 任务，并停止可取消的底层 workflow。
- [x] `POST /api/projects/{projectId}/agent/tasks/{taskId}/steps/{stepId}/approve`：批准等待中的步骤。
- [x] `POST /api/projects/{projectId}/agent/tasks/{taskId}/steps/{stepId}/reject`：拒绝等待中的步骤。
- [x] `POST /api/projects/{projectId}/agent/tasks/{taskId}/resume`：恢复被审批、失败重试或外部条件阻塞的任务。
- [x] 所有 Agent 控制 API 接入现有认证、组织上下文、RBAC 和错误归一化。
- [x] OpenAPI 补齐 Agent 控制 API 的 request、response、错误码和 schema。
- [x] Web API client 补齐 Agent 控制 API 类型和方法。
- [x] OpenAPI 路由一致性检查覆盖新增 Agent 路由。

## 4. Planner

- [x] Planner 输入包含用户目标、项目摘要、当前状态、可用工具、约束和最近对话。
- [x] Planner 输出稳定 JSON，包括 summary、steps、tool、args、risk、requiresApproval、expectedResult。
- [x] Planner 输出 schema 有严格解析和容错，不能让自由文本直接驱动执行。
- [x] Planner 调用 Provider Gateway 时记录 provider call、prompt hash、model profile key 和 agent run。
- [x] Planner 支持 `plan_only`：只生成计划，不执行任何写操作或 costed 操作。
- [x] Planner prompt 已禁止虚构 ID 并要求缺少 ID 时先安排读取类工具，且 ValidatePlan 会按工具 input schema 做参数级强校验。
- [x] Planner 单测覆盖 JSON fenced block、数组工具调用、非法 JSON、未知工具和超量步骤裁剪。

## 5. Supervisor

- [x] 部分完成：当前 Script Agent mutation 工具需要用户文本包含明确执行意图。
- [x] 部分完成：当前已实现工具按项目权限调用 Authorizer。
- [x] 部分完成：已建立基础监督层，覆盖权限、风险、任务级成本约束、状态门禁、审批决策和基础验证；Provider policy 级成本估算仍需深化。
- [x] 每个 Tool 映射一个或多个 CineWeave permission。
- [x] `read` 风险默认自动允许。
- [x] `draft` 风险默认允许或低成本确认，并写入 step/task 审计链路。
- [x] `write` 风险默认需要计划确认。
- [x] `workflow` 风险默认需要确认。
- [x] 部分完成：会产生 Provider 成本的 Agent 步骤会走任务级预算约束；Gateway 仍负责额度、熔断和并发硬门禁。
- [x] `destructive` 风险默认阻断或强确认。
- [x] `admin` 风险默认人工确认，部分能力默认禁用。
- [x] 启动 workflow 前检查是否已有同类 running/queued 任务。
- [x] Provider 文本、模型测试、Project Agent workflow 启动与镜头生产工具使用 task/step 级稳定 idempotency key，重复执行同一 step 不会重复启动同一工作流。
- [x] 视频生成前检查镜头图片状态，缺图时不能直接生成视频。
- [x] 成片合成前检查所需视频是否成功。
- [x] Provider 和 Prompt 管理动作必须强确认，并要求 `provider.manage` 或 `prompt.manage`。
- [x] Review fix apply 前必须展示 before/after/diff，并保留 snapshot changed 阻断。
- [x] Supervisor 决策写入 `agent_steps.supervisor_decision` 并同步给前端。

## 6. Executor 与审计

- [x] Executor 读取已通过监督的 step 后执行工具。
- [x] 每次工具执行写入 `agent_steps` 或 `agent_tool_invocations`。
- [x] 每次工具执行同步写入 `event_outbox`，供前端实时订阅。
- [x] 部分完成：资产、分镜更新和分镜重排写操作会返回 before/patch/after 或明确影响范围；其余写操作仍需继续补齐统一 diff 输出。
- [x] 部分完成：workflow 类工具会返回 workflowRunId、workflowType、status，并由 Verifier 覆盖基础状态；可取消性字段仍需补齐。
- [x] 部分完成：`script.generate_from_source`、`script.rewrite`、`script.rewrite_preview`、`provider.test_model` 会返回 provider call id；成本记录和成本估算仍需接入统一输出。
- [x] 工具失败会写入 normalized error code、message、retryable 和下一步建议。
- [x] Executor 已为 Project Agent 的文本 Provider 调用、模型测试、workflow 启动和普通业务写入生成 task/step 级稳定 idempotency key；剧本/提示词版本创建、版本激活、目标 patch、分镜重排、review fix 和成片激活均覆盖重复执行保护。

## 7. Tool Registry 第一批工具

### 7.1 只读工具

- [x] `project.read_summary` 已进入正式 Tool Registry 并接入执行器。
- [x] `source.list` 已进入正式 Tool Registry 并接入执行器。
- [x] `source.list_chapters` 已进入正式 Tool Registry 并接入执行器。
- [x] `script.list` 已进入正式 Tool Registry 并接入执行器。
- [x] `script.get` 已进入正式 Tool Registry 并接入执行器。
- [x] `asset.list` 已进入正式 Tool Registry 并接入执行器。
- [x] `storyboard.list` 已进入正式 Tool Registry 并接入执行器。
- [x] `workflow.read_runs` 已进入正式 Tool Registry 并接入执行器。
- [x] `workflow.read_nodes` 已进入正式 Tool Registry 并接入执行器。
- [x] `workflow.read_shots` 已进入正式 Tool Registry 并接入执行器。
- [x] `review.list_items` 已进入正式 Tool Registry 并接入执行器。
- [x] `artifact.list` 已进入正式 Tool Registry 并接入执行器。
- [x] `artifact.preview_url` 已进入正式 Tool Registry 并接入执行器。
- [x] `provider.list_status` 已进入正式 Tool Registry 并接入执行器。

### 7.2 草稿与建议工具

- [x] `review.run` 运行 deterministic review 和可选 agent review。
- [x] `review.generate_fix` 生成 review fix 草稿，不直接应用。
- [x] `prompt.render_test` 渲染 prompt 并返回测试输出。
- [x] `script.rewrite_preview` 生成改写预览，不写入版本。

### 7.3 写操作工具

- [x] `script.generate_from_source` 包装现有从来源生成剧本能力，并写入可追溯 agent step。
- [x] `script.rewrite` 包装现有改写剧本能力。
- [x] `script.create_version` 创建剧本版本。
- [x] `script.activate_version` 激活剧本版本。
- [x] `asset.update` 修改资产描述、prompt、traits、审核状态。
- [x] `storyboard.update_shot` 修改镜头字段。
- [x] `storyboard.reorder` 重排镜头。
- [x] `review.apply_fix` 应用已批准的 fix。
- [x] `review.dismiss_fix` 忽略 fix。

### 7.4 Workflow 与生产工具

- [x] `workflow.start` 支持通过正式 Agent step 审批后启动受控 workflow。
- [x] `workflow.cancel` 支持通过正式 Agent step 审批后取消 workflow。
- [x] `shot.status` 读取镜头生产状态。
- [x] `shot.generate_missing_images` 生成缺失镜头图片。
- [x] `shot.generate_missing_videos` 生成缺失镜头视频。
- [x] `shot.cancel_running_videos` 取消运行中视频任务。
- [x] `timeline.compose` 触发时间线合成。
- [x] `final_video.activate` 激活成片版本，强确认。

### 7.5 管理工具

- [x] `provider.test_model` 测试模型，要求 provider 管理权限并复用 Provider Service / Gateway，并接入 Agent 任务级成本门禁。
- [x] `provider.install_catalog_preset` 安装 catalog preset，强确认。
- [x] `provider.update_account` 更新供应商，强确认。
- [x] `provider.update_model` 更新模型，强确认。
- [x] `prompt.create_version` 创建 prompt 版本，强确认。
- [x] `prompt.activate_version` 激活 prompt 版本，强确认。

## 8. Verifier

- [x] `workflow.start` 后确认 workflow_runs 状态为 queued/running 或已进入可接受终态。
- [x] `workflow.cancel` 后确认 workflow_run 进入 cancelling/cancelled 或已是终态。
- [x] `shot.generate_missing_images` 后确认目标镜头 image status 进入 queued/running/succeeded。
- [x] `shot.generate_missing_videos` 后确认目标镜头 video status 进入 queued/running/succeeded。
- [x] `review.apply_fix` 后重新读取目标对象，确认 patch 已生效。
- [x] `script.activate_version` 后重新读取 script，确认 currentVersionId 已切换。
- [x] 部分完成：`prompt.activate_version` 后确认 active version 状态；render-test 二次验证仍需补齐。
- [x] `artifact.preview_url` 后确认 URL 和过期时间存在。

## 9. Temporal 集成

- [x] 新增 `internal/workflows/project_agent.go`。
- [x] `ProjectAgentWorkflow` 支持 planning、supervise、execute、verify 的长任务生命周期。
- [x] 支持 `agent.approve_step` signal。
- [x] 支持 `agent.reject_step` signal。
- [x] 支持 `agent.cancel_task` signal。
- [x] 支持 `agent.modify_constraints` signal。
- [x] Agent Task 执行中状态通过 Temporal 和数据库同步。
- [x] API Server 使用现有 Temporal client 启动、取消、signal Agent workflow。
- [x] HTTP 请求不阻塞等待长任务完成。

## 10. 前端助手控制面

- [x] 已有右侧助手面板、会话选择、消息列表、快捷操作和输入框。
- [x] 已有工具执行消息卡片，展示工具名、成功/失败、摘要和 workflowRunId。
- [x] 新增 Agent Task Panel，展示活动任务、计划、状态和输出。
- [x] 新增 Plan Timeline，展示每个 step 的工具、风险、状态、输入摘要和输出摘要。
- [x] 新增 Tool Call Card，展示权限、风险、影响范围、预计成本、错误和 Verifier 结果。
- [x] 新增 Approval Modal，支持批准、拒绝、填写备注和修改约束。
- [x] 新增 Result Summary，汇总已完成动作、失败动作、Temporal Agent 工作流、项目缺口摘要和下一步建议。
- [x] Quick Actions 扩展为项目控制目标：检查项目问题、生成修复建议、生成缺失图片、生成缺失视频、从剧本生成分镜、生成最终预览、取消当前视频任务。
- [x] Quick Actions 只填入 goal，不能绕过 Supervisor。
- [x] 部分完成：前端通过 Agent realtime 事件和轮询显示 Agent Task 与 Step 动态，并在动作后刷新 Workflow、生产状态和成果缓存；Provider 调用级实时流尚未接入。
- [x] 高风险动作必须先展示确认面板，不能只在聊天里提示。

## 11. Review Center 集成

- [x] Agent Supervisor 在关键生产动作前可运行 project review，并在执行前二次检查 high/critical 阻塞项。
- [x] 有 critical/high open review items 时，Agent 默认暂停并展示问题。
- [x] Agent 可生成 review fix draft，但不会自动应用。
- [x] 用户批准后才能调用 `review.apply_fix`。
- [x] 应用 fix 后再次运行 verifier 或 review，确认问题已解决。

## 12. 分阶段验收

### Phase 1：只读项目控制助手

- [x] 正式 Tool Registry 包含 `project.read_summary`。
- [x] 正式 Tool Registry 包含 `workflow.read_runs`。
- [x] 正式 Tool Registry 包含 `workflow.read_nodes`。
- [x] 正式 Tool Registry 包含 `workflow.read_shots`。
- [x] 正式 Tool Registry 包含 `review.list_items`。
- [x] 正式 Tool Registry 包含 `artifact.list`。
- [x] 正式 Tool Registry 包含 `provider.list_status`。
- [x] 用户输入“总结这个项目离成片还差什么”时，助手能回答 source、active script、storyboard、图片视频缺失、running/failed workflow、high/critical review items 和 provider/model profile 缺口。
- [x] Phase 1 前端能展示只读 Plan Timeline。

### Phase 2：可生成建议，但不改项目

- [x] 支持 `review.run`。
- [x] 支持 `review.generate_fix`。
- [x] 支持 `prompt.render_test`。
- [x] 支持 `script.rewrite_preview`。
- [x] `agent_tasks`、`agent_steps`、`agent_approvals` 可记录计划、草稿和审批。
- [x] 用户输入“检查项目并给修复建议，不要直接修改”时，所有修改都停在 waiting_approval。

### Phase 3：人工确认后执行写操作

- [x] 支持 `review.apply_fix`。
- [x] 支持 `script.create_version`。
- [x] 支持 `script.activate_version`。
- [x] 支持 `storyboard.update_shot`。
- [x] 支持 `asset.update`。
- [x] 支持 `timeline.update_clip`。
- [x] 未批准不能写入业务数据。
- [x] apply fix 前展示 before/after。
- [x] target snapshot changed 时阻断。
- [x] 写入后 verifier 重新读取目标对象确认成功。

### Phase 4：可控启动生产 workflow

- [x] 支持 `workflow.start`。
- [x] 支持 `workflow.cancel`。
- [x] 支持 `shot.generate_missing_images`。
- [x] 支持 `shot.generate_missing_videos`。
- [x] 支持 `shot.cancel_running_videos`。
- [x] 支持 `timeline.compose`。
- [x] 支持 `final_video.activate`，并强确认。
- [x] 用户输入“只生成缺失镜头图片，不要生成视频”时，Agent 只能调用图片生成动作。
- [x] 用户输入“生成缺失视频”时，Agent 先检查图片状态；缺图时不能直接生成视频。
- [x] 用户输入“生成成片”时，Agent 给出 workflow、确认请求和预计成本风险，批准后执行。

### Phase 5：半自动生产管线

- [x] `ProjectAgentWorkflow` 能从项目状态自动选择下一步。
- [x] 不重复启动同类 workflow。
- [x] 执行过程不超过预算上限。
- [x] 支持每项目并发限制。
- [x] 支持全局 kill switch。
- [x] 支持自动重试策略。
- [x] 支持失败归因和下一步建议。
- [x] 所有 Project Agent 触发的 provider 调用可通过 task/step trace 追溯：直接调用记录 providerCallId/promptHash，workflow 调用通过 workflowRunId 关联 provider_call_logs 与 artifact prompt_hash/model metadata。
- [x] 可基于当前状态选择“源文本 -> 剧本 -> 分镜 -> 图片 -> 视频 -> 合成”的下一步；ProjectAgentWorkflow 会等待 Agent 启动的子 workflow 完成，再按最新项目状态追加下一段计划，并在审批、失败或阻塞节点暂停。

## 13. 测试与验收命令

- [x] 后端单测覆盖 Planner JSON 解析、未知工具、权限拒绝、mutation 需要确认、Supervisor 风险判断。
- [x] 后端 API 集成测试覆盖 agent tools、agent tasks、approve、reject、cancel、resume。
- [x] 后端 workflow 测试覆盖 ProjectAgentWorkflow approve、cancel、失败终态、resume、modify constraints 和 activity retry。
- [x] 前端类型检查通过。
- [x] 前端 lint 通过。
- [x] OpenAPI YAML parse 通过。
- [x] OpenAPI 路由一致性检查通过。
- [x] Docker Compose config 通过。
- [x] App profile 容器可重建并健康运行。

基础命令：

```powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
@'
import yaml
with open('packages/openapi/openapi.yaml', 'r', encoding='utf-8') as f:
    yaml.safe_load(f)
print('ok')
'@ | python -
docker compose -f compose.yml config --quiet
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

## 14. 下一步执行顺序

1. 新增 Agent 控制数据库模型：`agent_tasks`、`agent_steps`、`agent_approvals`，并关联 `agent_runs`。
2. 新增 Project Agent Tool Registry，把当前 Script Agent 工具雏形迁入正式 registry。
3. 新增 Agent 控制 API 和 OpenAPI：tools、tasks、detail、cancel、approve、reject、resume。
4. 实装 Supervisor 的权限、风险、审批和基础状态门禁。
5. 前端新增 Agent Task Panel 与 Plan Timeline，接真实 API 和实时状态。
6. 接入 Review Center、Verifier 和 Temporal ProjectAgentWorkflow。
