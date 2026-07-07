一版 CineWeave 项目控制型 AI 助手能力开发方案。重点不是重新做一个 Toonflow，而是在 CineWeave 现有的 Provider Gateway、Temporal workflow、RBAC、Prompt Registry、Artifact、Review Center 之上，加一个可规划、可执行、可监督的 Agent Runtime。

---

# 1. 现状判断：CineWeave 已有局部 Agent，但还不是项目控制 Agent

CineWeave 的当前架构已经很适合做“AI 助手控制项目”。README 明确说明它是围绕 Provider Gateway、Temporal workflows、多租户权限、Artifact 存储、可观测 Provider 执行重建的云原生 AI 视频生产平台。 仓库结构也已经分成 `appsapi`、`appsweb`、`servicesprovider-gateway`、`workers`、`internal`、`packages`、`db`、`deploy`、`docs` 等层。

现有能力可以分成几类：

第一，生成与生产 workflow 已经很完整。`POST apiworkflow-runs` 可以启动 `video_production`、`text_to_storyboard`、`script_to_storyboard` 等 workflow，并且 workflow run API 支持状态、节点、shots、artifacts 查询。 后端 `createWorkflowRun` 会校验 `workflowType`、做 idempotency、写 `workflow_runs`，然后启动 Temporal workflow。

第二，Provider Gateway 是模型调用边界。README 要求 API 和 worker 默认通过 `PROVIDER_GATEWAY_URL` 与 `CINEWEAVE_SERVICE_TOKEN` 调 Provider Gateway，生产环境不应启用 direct provider fallback。 Gateway 负责 textimagevideo runtime、媒体下载、S3MinIO 存储、`provider_call_logs`、`cost_records` 等。

第三，Prompt Registry 已经可版本化治理 Prompt。系统 Prompt 会 seed，workflow 调用会解析项目绑定、组织绑定、组织 active version、系统 active version，并把 `promptVersionId`、`promptHash`、`promptTemplateKey`、`promptSource` 带入 Gateway 和 Artifact metadata。

第四，RBAC 已有精细权限模型。CineWeave 的 API 不是靠 membership 粗略判断，而是通过 `role_bindings` 和 `role_permissions`，权限包括 `project.write`、`workflow.run`、`workflow.cancel`、`asset.write`、`provider.manage`、`prompt.manage`、`artifact.read` 等。 代码里也定义了完整权限常量。

第五，已有局部 Agent 数据结构和脚本助手。迁移里已经有 `agent_sessions`、`agent_messages`、`agent_runs` 三张表，支持 `script_agent`、`asset_agent`、`storyboard_agent`、`shot_asset_agent` 等类型。 API 里也有 Script Agent session、message、generate script、rewrite script 等接口。 具体实现里，`generateScriptFromAgent` 和 `rewriteScriptFromAgent` 会调用 Prompt Registry，再通过 Provider Gateway 生成文本，并把结果写入 scriptscript_version。

第六，前端 Agent Drawer 目前更像聊天入口，不是项目控制器。`AgentDrawer` 会根据路由设置 `script_agent  asset_agent  storyboard_agent  shot_asset_agent`，并提供 quick actions。 但 `useAgentChat` 目前主要是拉取消息、发送 user message，并没有执行计划、工具调用、审批时间线。

所以结论是：CineWeave 已有“局部生产 Agent”，但缺少一个统一的 Project Control Agent。 这个 Agent 应该能读取项目状态、拆解任务、调用受控工具、启动取消 workflow、生成并应用修复、调度生产动作，同时被监督层约束。

---

# 2. 推荐总体架构

建议新增一个 Project Agent Runtime，采用类似 Toonflow 的分层，但贴合 CineWeave：

```text
用户自然语言任务
  ↓
决策层 Project Agent Planner
  ↓ 生成结构化计划
监督层 Agent Supervisor
  ↓ 审查权限、成本、风险、状态、是否需人工确认
执行层 Agent Executor
  ↓ 只调用白名单 Tool
CineWeave 现有 API  Service  Temporal  Provider Gateway
  ↓
Verifier + Event Outbox + Agent Timeline
  ↓
前端 Agent Drawer 展示计划、执行、审批、结果
```

至少要有两层：

```text
执行层：Agent Executor
监督层：Agent Supervisor
```

但我建议加一个很薄的“决策层”，只负责把自然语言变成 JSON Plan，不直接执行。

---

# 3. 执行层设计：Agent Executor

执行层的原则是：AI 不直接操作数据库、不直接调 Provider、不直接改 Artifact、不直接启动 Temporal；它只能调用注册过的 Tool。

现有 CineWeave 后端已经有大量业务入口，所以执行层不是重写业务，而是做一层 typed tool registry。

## 3.1 新增后端模块

建议新增：

```text
internalagent
  runtime.go
  planner.go
  supervisor.go
  executor.go
  tool_registry.go
  tool_types.go
  tools
    project_tools.go
    source_tools.go
    script_tools.go
    asset_tools.go
    storyboard_tools.go
    shot_production_tools.go
    timeline_tools.go
    workflow_tools.go
    review_tools.go
    provider_tools.go
    prompt_tools.go
    artifact_tools.go
```

API 层新增：

```text
internalapiagent_control.go
```

前端新增：

```text
appswebsrcfeaturesassistantcontrol
  agent-task-panel.tsx
  agent-plan-timeline.tsx
  agent-tool-call-card.tsx
  agent-approval-modal.tsx
  agent-result-summary.tsx
```

## 3.2 Tool 接口

Go 侧可以做成：

```go
type ToolRisk string

const (
  ToolRiskRead        ToolRisk = read
  ToolRiskDraft       ToolRisk = draft
  ToolRiskWrite       ToolRisk = write
  ToolRiskWorkflow    ToolRisk = workflow
  ToolRiskCosted      ToolRisk = costed
  ToolRiskDestructive ToolRisk = destructive
  ToolRiskAdmin       ToolRisk = admin
)

type AgentTool struct {
  Name        string
  Description string
  Risk        ToolRisk
  Permission  string
  InputSchema json.RawMessage
  DryRun      func(ctx ToolContext, input json.RawMessage) (ToolResult, error)
  Execute     func(ctx ToolContext, input json.RawMessage) (ToolResult, error)
}
```

关键点：

```text
Tool 必须声明：
- name
- required permission
- risk level
- input schema
- dryRun
- execute
- verifier
```

执行层每次调用工具时，必须写入 `agent_tool_invocations` 或 `agent_steps`，并同步写 `event_outbox`。当前 CineWeave 已经有 `insertAPIEvent`，用于把事件写入 `event_outbox`。

## 3.3 首批工具白名单

第一批工具建议只包装已有 APIService：

 工具名                                          风险  对应能力
 ------------------------------  --------------  -------------------------------------------------------------
 `project.read_summary`                     read  读取 project、sources、scripts、assets、storyboard、workflow summary
 `source.list`                              read  读项目源文本
 `script.list`  `script.get`               read  读 scripts 和 versions
 `script.generate_from_source`      writecosted  包装现有 `generateScriptFromAgent`
 `script.rewrite`                   writecosted  包装现有 `rewriteScriptFromAgent`
 `script.create_version`                   write  创建新 script version
 `script.activate_version`                 write  激活版本
 `asset.list`                               read  读 canonical assets
 `asset.update`                            write  修改资产描述、prompt、traits
 `asset.generate_image`                   costed  生成 canonical asset image
 `storyboard.list`                          read  读 storyboard shots
 `storyboard.update_shot`                  write  修改镜头字段
 `storyboard.reorder`                      write  重排镜头
 `shot.status`                              read  包装 `getShotProductionStatus`
 `shot.generate_missing_images`  workflowcosted  包装 `runShotProductionAction`
 `shot.generate_missing_videos`  workflowcosted  包装 `runShotProductionAction`
 `shot.cancel_running_videos`           workflow  包装 cancellation
 `workflow.start`                workflowcosted  包装 `POST apiworkflow-runs`
 `workflow.cancel`                      workflow  包装 cancel
 `workflow.read_nodes`                      read  读 workflow nodes
 `workflow.read_shots`                      read  读 workflow shots
 `review.run`                             costed  跑 deterministic + agent review
 `review.generate_fix`              draftcosted  生成 review fix 草稿
 `review.apply_fix`                        write  应用 review fix
 `review.dismiss_fix`                      write  dismiss review fix
 `artifact.list`                            read  列 artifact
 `artifact.preview_url`                     read  生成短期预览 URL
 `provider.list_status`                     read  读 provider accountsmodelscircuitcost
 `provider.test_model`              costedadmin  测试模型
 `prompt.render_test`                readcosted  测 prompt rendering
 `prompt.create_version`                   admin  创建 prompt version
 `prompt.activate_version`                 admin  激活 prompt version

其中 `shot.generate_missing_images  videos` 可以直接复用现有 shot production action。当前 API 已支持 `generate_missing_images`、`regenerate_stale_images`、`regenerate_failed_images`、`generate_missing_videos`、`regenerate_stale_videos`、`regenerate_failed_videos`、`cancel_running_videos` 等动作。 这些动作会映射到 batch imagevideocancel workflows。

---

# 4. 监督层设计：Agent Supervisor

监督层不是简单的“二次问大模型”。它应该是 确定性规则 + RBAC + 成本门禁 + 状态门禁 + 人工确认 + 输出校验。

## 4.1 监督层职责

监督层负责回答五个问题：

```text
1. 这个用户有没有权限让 Agent 做这件事？
2. 这个动作会不会写数据、花钱、启动 workflow、删除资产、改 providerprompt？
3. 当前项目状态是否允许执行？
4. 是否需要用户确认？
5. 执行后如何验证结果？
```

CineWeave 已经有 `Authorizer.Authorize`，会解析 resource，并检查用户在组织、workspace、project 上的 role binding 与 permission。 Agent Supervisor 应该复用这个 Authorizer。也就是说，Agent 不能绕过 RBAC；它只能“代表当前用户”执行。

## 4.2 风险等级

建议分成 7 档：

 风险             示例                                                  默认处理
 -------------  --------------------------------------------------  -----------
 `read`         list scripts、list artifacts、workflow status         自动允许
 `draft`        generate review fix draft、prompt render test        自动允许或低成本确认
 `write`        update shot、create script version、apply review fix  需要计划确认
 `workflow`     start workflow、cancel workflow、compose timeline     需要确认
 `costed`       调 textimagevideo provider                         需要预算检查
 `destructive`  delete asset、delete final video、delete project      默认阻断或强确认
 `admin`        provider credentials、prompt activate、role binding   默认人工确认，部分禁用

## 4.3 关键监督规则

### A. 权限规则

每个 Tool 映射一个 CineWeave permission：

```text
project.read_summary       - project.read
script.generate_from_source - script.write + workflow.run 或 script.write
asset.generate_image        - asset.generate
shot.generate_missing_videos - workflow.run
workflow.cancel             - workflow.cancel
prompt.activate_version     - prompt.manage
provider.rotate_credential  - provider.manage
role.change                 - role.manage
```

权限不满足直接 block。

### B. 成本规则

所有会调用 Provider Gateway 的动作必须通过成本门禁：

```text
- 预估 task_type：text.generate  image.generate  video.create_task  video.poll_task
- 检查 provider_limit_policies
- 检查当前 orgproject 当日成本
- 检查 provider_circuit_states
- 检查是否已有 provider_leases 或运行中 workflow
- 大成本视频动作必须确认
```

README 已明确 Provider limits 在 Gateway 内执行，可限制并发、每分钟每日请求、每日月预算、失败 circuit 等。 Agent Supervisor 不需要取代 Gateway，但要在用户体验层提前拦截明显超预算或高风险动作。

### C. Workflow 状态规则

启动 workflow 前必须检查：

```text
- 是否已有同类型 running  queued workflow
- 是否已有相同 idempotency key
- project 是否存在且用户有 workflow.run
- maxShots 是否超过当前 workflow 限制
- 视频生成前是否已有 image
- final compose 前所有视频是否 succeeded
```

CineWeave API 目前已经要求 workflow writes 支持 `Idempotency-Key`，同一 key 重放返回 stored response，请求不一致返回 `409 IDEMPOTENCY_CONFLICT`。 Agent Executor 应该为每个写操作生成稳定 idempotency key。

### D. Review Fix 规则

`review.generate_fix` 可以自动执行，因为它只生成 draft fix；`review.apply_fix` 必须通过监督层，因为它会真正 patch 目标对象。现有 `applyReviewFix` 已经做了关键保护：只有 `draft` 状态的 fix 可应用，目标必须支持自动修复，并且会比较当前 snapshot 与生成 fix 时的 `beforeSnapshot`，如果目标已变化则返回 `TARGET_CHANGED`。 Agent Supervisor 应强制保留这些约束，并把 diff 展示给用户。

### E. Provider  Prompt 管理规则

这些默认都要高权限确认：

```text
- 安装 provider catalog preset
- rotate credential
- update provider account
- update provider model
- create model profile binding
- create prompt version
- activate prompt version
```

Provider Catalog 安装会创建 connector、provider account、encrypted credential、provider models、model capabilities 和可选 Model Profile bindings。 这类动作影响全局生产质量和成本，应由 Supervisor 强确认。

---

# 5. 推荐 Agent 分层

CineWeave 可以采用“三层 Agent”，但不要照搬 Toonflow 的短剧本地工作台实现。建议这样分：

## 5.1 决策层：Project Agent Planner

职责：

```text
- 理解用户目标
- 读取项目摘要
- 生成结构化执行计划
- 给每步选择工具
- 标注风险、预期结果、是否需要用户确认
```

输入：

```json
{
  goal 把这个项目从剧本推进到可预览成片,
  projectId ...,
  currentState {
    scripts ...,
    assets ...,
    storyboard ...,
    shotProduction ...,
    workflowRuns ...
  },
  tools [script.generate_from_source, asset.generate_image, ...]
}
```

输出：

```json
{
  summary 项目缺少分镜和镜头视频，建议先生成 storyboard，再生成缺失图片和视频。,
  steps [
    {
      tool workflow.start,
      args { workflowType script_to_storyboard },
      risk workflow,
      requiresApproval true
    },
    {
      tool shot.generate_missing_images,
      args { maxConcurrency 1 },
      risk costed,
      requiresApproval true
    }
  ]
}
```

Planner 调模型也必须走 Provider Gateway。现有 `GatewayClient` 已封装 `internalprovidertextgenerate`、`internalproviderimagegenerate`、`internalprovidervideocreate-task`、`poll-task`、`cancel-task`。

## 5.2 执行层：Agent Executor

职责：

```text
- 读取被 Supervisor 批准的 plan step
- 调用白名单 tool
- 写 agent_steps  tool_invocations  event_outbox
- 对 workflow 类任务返回 workflowRunId
- 对写操作返回 before  after  diff
- 对失败动作返回 structured error
```

它不能自由拼 HTTP；必须调用内部 service 或 API handler 级别封装。

## 5.3 监督层：Agent Supervisor

职责：

```text
- 权限审查
- 风险审查
- 成本审查
- 状态审查
- 人工确认
- 执行后校验
```

高风险动作必须进入：

```text
waiting_approval
```

用户确认后再继续。

## 5.4 验证层：Agent Verifier

建议单独做一个 verifier，不一定算 Agent：

```text
- workflow.start 后：检查 workflow_runs 是否 queuedrunning
- shot.generate_missing_images 后：检查目标 shots image_status 是否 queuedrunningsucceeded
- review.apply_fix 后：重新读取 target，确认 patch 已生效
- prompt.activate_version 后：render-test 验证 active version
- artifact.preview_url 后：确认 URL 过期时间和权限
```

---

# 6. 数据库改造

已有 `agent_sessions  agent_messages  agent_runs`，但 `agent_runs` 当前更像“单次生成任务记录”，不适合记录多步骤项目控制任务。 建议保留现有表，新增控制型表：

```sql
CREATE TABLE agent_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id UUID REFERENCES agent_sessions(id) ON DELETE SET NULL,
  agent_type TEXT NOT NULL DEFAULT 'project_agent',
  user_goal TEXT NOT NULL,
  status TEXT NOT NULL CHECK (
    status IN ('queued','planning','waiting_approval','running','succeeded','failed','blocked','cancelled')
  ),
  plan JSONB NOT NULL DEFAULT '{}',
  summary JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);

CREATE TABLE agent_steps (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
  step_index INT NOT NULL,
  tool_name TEXT NOT NULL,
  risk TEXT NOT NULL,
  status TEXT NOT NULL CHECK (
    status IN ('planned','waiting_approval','approved','running','succeeded','failed','blocked','skipped')
  ),
  input JSONB NOT NULL DEFAULT '{}',
  dry_run_output JSONB NOT NULL DEFAULT '{}',
  supervisor_decision JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_approvals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
  step_id UUID REFERENCES agent_steps(id) ON DELETE CASCADE,
  approval_type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','approved','rejected','expired')),
  requested_payload JSONB NOT NULL DEFAULT '{}',
  decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
  decided_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

也可以给 `agent_runs` 加 `task_id`，把 PlannerSupervisor 的 LLM 调用继续记录为 agent run：

```sql
ALTER TABLE agent_runs
  ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS step_id UUID REFERENCES agent_steps(id) ON DELETE SET NULL;
```

---

# 7. API 设计

新增 API：

```text
GET  apiprojects{projectId}agenttools
POST apiprojects{projectId}agenttasks
GET  apiprojects{projectId}agenttasks
GET  apiprojects{projectId}agenttasks{taskId}
POST apiprojects{projectId}agenttasks{taskId}cancel
POST apiprojects{projectId}agenttasks{taskId}steps{stepId}approve
POST apiprojects{projectId}agenttasks{taskId}steps{stepId}reject
POST apiprojects{projectId}agenttasks{taskId}resume
```

`POST agenttasks` 请求：

```json
{
  sessionId ...,
  goal 检查项目问题，生成可应用修复，但不要直接改项目,
  mode plan_only  supervised  auto_low_risk,
  constraints {
    maxProviderCostCents 300,
    allowVideoGeneration false,
    requireApprovalForWrites true
  }
}
```

返回：

```json
{
  taskId ...,
  status waiting_approval,
  plan {
    steps [...]
  }
}
```

`approve` 请求：

```json
{
  approvalId ...,
  decision approved,
  note 同意生成缺失图片，但不要生成视频
}
```

---

# 8. 前端改造

当前 `AgentDrawer` 只有 session selector、message list、quick actions、message input。 要升级为项目控制助手，建议增加三个区域：

```text
1. Chat
   - 用户自然语言目标
   - 助手解释

2. Plan Timeline
   - 步骤列表
   - 每一步工具名、风险、状态、输入摘要、输出摘要

3. Approval Panel
   - 高风险动作确认
   - 显示 beforeafterdiff
   - 显示预计成本、影响范围、可回滚性
```

Quick Actions 也应该从现在的：

```text
generate-script
rewrite-script
analyze-assets
```

扩展成：

```text
- 检查项目问题
- 生成修复建议
- 只生成缺失图片
- 只生成缺失视频
- 从剧本生成分镜
- 生成最终预览
- 取消当前视频任务
```

但 quick action 本质上只是填入 goal，不能绕过 Supervisor。

---

# 9. 和现有 Review Center 的关系

CineWeave 的 Review Center 已经非常适合当监督层的一部分。

现有 `runProjectReview` 可以执行 deterministic checks，也可以启用 agent review，并把结果写入 `review_runs` 和 `review_items`。 Agent review 会构造项目上下文、渲染 `project_review_agent` prompt、通过 Provider Gateway 生成 JSON，然后规范化成 review items。

因此建议：

```text
Agent Supervisor 在每次关键写操作前后，可以调用 review.run。
```

例如：

```text
用户：帮我把项目生成到成片。
Agent：
1. 读取项目状态
2. 运行 project review
3. 如果有 criticalhigh open items，暂停
4. 生成 review fixes，但不自动应用
5. 用户批准后 apply fixes
6. 再启动 storyboardimagevideo workflows
7. 最后再次 run review
```

`generateReviewFix` 当前已经支持 deterministic 或 agent 两种模式，生成 fix 草稿，不直接应用。 这正好可以作为监督层的“建议修复”机制。

---

# 10. Temporal 集成建议

CineWeave 已经大量使用 Temporal。Agent Task 如果可能跨多个 workflow、等待用户审批、等待视频任务完成，应该也做成 Temporal workflow。

新增：

```text
internalworkflowsproject_agent.go
```

```go
func ProjectAgentWorkflow(ctx workflow.Context, input ProjectAgentInput) error {
   1. plan
   2. supervise each step
   3. if approval required wait signal
   4. execute tool activity
   5. verify
   6. continue or stop
}
```

需要 signal：

```text
agent.approve_step
agent.reject_step
agent.cancel_task
agent.modify_constraints
```

API Server 当前 `temporalClient` interface 已经包含 `ExecuteWorkflow`、`CancelWorkflow`、`SignalWorkflow`。 所以实现“等待审批后继续执行”是顺着现有设计走的。

---

# 11. 分阶段落地路线

## Phase 1：只读项目控制助手

目标：让助手能读懂项目，但不能写。

交付：

```text
- internalagenttool_registry.go
- project.read_summary
- workflow.read_runs
- workflow.read_nodes
- workflow.read_shots
- review.list_items
- artifact.list
- provider.list_status
- 前端 Plan Timeline 只读展示
```

验收：

```text
用户输入：总结这个项目离成片还差什么。
助手能回答：
- 是否有 source
- 是否有 active script
- 是否有 storyboard shots
- 图片视频缺失数量
- 是否有 runningfailed workflow
- 是否有 highcritical review items
- providermodel profile 是否缺失
```

## Phase 2：可生成建议，但不改项目

目标：允许低风险 draft 行为。

交付：

```text
- review.run
- review.generate_fix
- prompt.render_test
- script.rewrite_preview，建议新增，只返回内容不写版本
- agent_tasks  agent_steps  agent_approvals 表
```

验收：

```text
用户输入：检查项目并给修复建议，不要直接修改。
助手能：
- 跑 review
- 生成 review fix draft
- 展示 diff
- 所有修改都停在 waiting_approval
```

## Phase 3：人工确认后执行写操作

目标：让助手能真正控制项目，但必须受监督。

交付：

```text
- review.apply_fix
- script.create_version
- script.activate_version
- storyboard.update_shot
- asset.update
- timeline.update_clip
- Approval Modal
```

验收：

```text
- 未批准不能写
- apply fix 前展示 beforeafter
- target snapshot changed 时阻断
- 写入后 verifier 重新读取目标对象确认成功
```

## Phase 4：可控启动生产 workflow

目标：助手能推进视频生产流程。

交付：

```text
- workflow.start
- workflow.cancel
- shot.generate_missing_images
- shot.generate_missing_videos
- shot.cancel_running_videos
- timeline.compose
- final_video.activate，强确认
```

验收：

```text
用户输入：只生成缺失镜头图片，不要生成视频。
助手只能调用 generate_missing_images。
用户输入：生成缺失视频。
助手先检查 image 是否存在；缺图时不能直接生成视频。
用户输入：生成成片。
助手给出预计 workflow 和成本风险，用户确认后执行。
```

## Phase 5：半自动生产管线

目标：在预算和审批约束内，自动完成“源文本 → 剧本 → 分镜 → 图片 → 视频 → 合成”。

交付：

```text
- ProjectAgentWorkflow
- 执行中状态轮询
- 自动重试策略
- 预算上限
- 每项目并发限制
- 全局 kill switch
- 失败归因与下一步建议
```

验收：

```text
- 可从项目状态自动选择下一步
- 不重复启动同类 workflow
- 不超过预算
- 可取消
- 所有 provider 调用可追溯到 provider_call_logs  prompt_hash  artifact metadata
```

---

# 12. 推荐工具风险策略

具体默认策略建议如下：

```text
自动允许：
- read_summary
- list workflows
- list artifacts
- list review items
- list provider status

自动允许但记录：
- run deterministic review
- generate review fix draft
- render prompt test

需要用户确认：
- create script version
- activate script version
- apply review fix
- update storyboard shot
- update canonical asset
- generate images
- generate videos
- compose timeline
- cancel running workflow

强确认或默认禁止：
- delete project
- delete asset
- delete final video
- rotate provider credential
- activate prompt version
- update role binding
- change provider limit policies
```

---

# 13. 关键工程原则

## 原则一：Agent 不能绕过 Provider Gateway

所有 LLM planning、review、fix、script rewrite、imagevideo generation 都必须走 Gateway。这样才能保留 prompt hash、provider call log、cost record、model profile routing、provider limit policies。

## 原则二：Agent 不能绕过 RBAC

Agent 代表当前用户执行。用户没有 `workflow.run`，Agent 也不能启动 workflow；用户没有 `provider.manage`，Agent 也不能改 provider。

## 原则三：高风险动作必须先 dry-run

例如 `review.apply_fix` 前必须生成：

```json
{
  target storyboard_shot,
  before {},
  patch {},
  after {},
  regeneration {
    recommended true
  }
}
```

用户确认后再 apply。

## 原则四：长任务必须 Temporal 化

项目级 Agent 不要用 HTTP request 一直阻塞。Planning 可以同步，执行应该进入 Temporal，审批用 signal 继续。

## 原则五：前端必须显示“为什么做、做了什么、影响什么”

不要只显示“AI 正在处理”。要展示：

```text
- Step
- Tool
- Risk
- Required Permission
- BeforeAfter
- Cost Estimate
- Supervisor Decision
- Result
```

---


# 14. 最终建议

CineWeave 不需要从零做 Agent 框架。最优路线是：

```text
把现有 Script Agent  Review Agent  Review Fix Agent 升级为统一 Project Agent Runtime。
```

落地优先级：

```text
1. Agent Tool Registry
2. Agent Supervisor
3. Agent Task  Step  Approval 数据模型
4. Agent Drawer 执行时间线
5. Review Center 接入监督层
6. Workflow  Shot Production 工具化
7. Temporal ProjectAgentWorkflow
```

一句话方案：

```text
CineWeave 的 AI 助手应该是“受 RBAC、Provider Gateway、成本预算、Review Center 和人工审批约束的项目生产控制器”，而不是一个能自由调用接口的大模型聊天框。
```
