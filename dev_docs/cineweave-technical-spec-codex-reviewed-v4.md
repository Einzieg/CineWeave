# 影织 CineWeave：云原生 AI 视频生产平台技术规格书

> 目标读者：Codex / 工程实现代理 / 后端、前端、基础设施工程师  
> 文档用途：作为影织 / CineWeave 全量重构的执行规格；该方案基于旧 Toonflow 项目重建，不以兼容旧 TypeScript 供应商脚本为目标。  
> 总原则：以工作流为核心，以 Provider Gateway 为 AI 供应商接入中心，以多租户、可观测、高并发、可恢复为平台级基础能力。
Toonflow：D:\Code\Toonflow
ai-fusion-video:D:\Code\ai-fusion-video
---

## 0. 执行摘要

影织 / CineWeave 不应只是旧 Toonflow 的增量演进，而应重建为一套 **云原生 AI 视频生产平台**。核心架构如下：

```text
Frontend:
  Next.js / React / TypeScript / shadcn/ui / React Flow

Control Plane:
  Go API Server
  Go Realtime Gateway
  OpenAPI + gRPC internal API

Workflow Plane:
  Temporal

Provider Plane:
  Provider Gateway
  Official Provider Adapters
  Declarative HTTP Connector
  External Connector Service Protocol

Execution Plane:
  Script Worker
  Image Worker
  Video Worker
  Audio Worker
  Media / FFmpeg Worker
  Quality Worker

Data Plane:
  PostgreSQL
  Redis
  S3 / MinIO
  NATS JetStream

Infra:
  Docker Compose for local dev
  Kubernetes + Helm for production
  OpenTelemetry + Prometheus + Grafana + Loki + Tempo
```

CineWeave 不要求兼容旧 Toonflow 的 TypeScript 供应商脚本。新版供应商接入应彻底改为：

```text
普通用户：表单 + 官方模板
高级用户：声明式 YAML / JSON Manifest
企业用户：外部 Connector Service Protocol
```

所有 AI 调用必须通过 Provider Gateway，不允许业务代码直接调用具体供应商。

---

## 0.1 复核修订记录

本版本已按 Codex 反馈进行第三次复核，Codex 执行时以本节后的正文为准：

```text
1. 统一租户字段：所有业务表、API 示例、事件和限流语义统一使用 organization_id / organizationId。
2. Provider Gateway 是唯一上游模型访问出口：Worker / Activity / API 不得直接调用供应商 API。
3. 统一 Provider Gateway 边界：Gateway 负责供应商调用、媒体下载、S3/MinIO 转存、provider_call_logs、cost_records 和 Gateway 侧 Artifact 写入；Worker 只编排 Workflow 并调用 Gateway。
4. 修正 Provider Lease 流程：Lease 由 Provider Gateway 申请、消费、释放；Worker 只把 leaseId 作为 Gateway 调用上下文传入，不持有供应商访问权。
5. 补齐 provider_credentials / provider_call_logs 字段：credential_id、model_profile_id、prompt_version_id、prompt_hash、request_snapshot、normalized_output 等必须落库。
6. 补齐 RBAC：organization_members / project_members 只表达成员关系，权限统一由 role_bindings + role_permissions 计算，不再把 role_key 直接写入 membership 表。
7. 补齐 teams / team_members / role_bindings，支持用户和团队在 organization / workspace / project 范围绑定角色。
8. 补齐 workflow_templates / workflow_template_nodes，否则 workflow_runs.template_id 无法落库。
9. 补齐 event_outbox，用于 DB 事务和 NATS/SSE 事件之间的可靠投递。
10. 补齐 auth_sessions / refresh token schema、idempotency_keys、provider_endpoints、provider_test_runs、provider_async_tasks、cost_records。
11. 明确异步 Provider 边界：Temporal Worker 持有 durable polling loop；Provider Gateway 每次执行 create / poll / cancel / webhook normalization，不在内存保存长任务状态。
12. 补充 API 固定风格：统一 envelope、分页、过滤、排序、错误码、关键接口 request/response 示例。
13. 明确首个真实 Provider 目标：第一版以 New API 作为主要验收目标，用 OpenAI-compatible Adapter 兼容 New API / One API / LiteLLM / OpenAI 官方。
14. 修正 Model Profile 示例中 priority_with_fallback 未列入枚举的问题。
```

---

## 0.2 品牌与命名规范

本项目新品牌统一为 **影织 / CineWeave**。旧项目名 Toonflow 只用于描述历史系统或迁移来源，不再作为新平台名称。Codex 生成代码、目录、Docker 镜像、数据库、前端标题和 API 文档时，应遵守以下命名：

```text
中文产品名：影织
英文产品名：CineWeave
仓库 / monorepo 目录：cineweave
Go module 示例：github.com/Einzieg/cineweave
前端应用：CineWeave Studio
云端产品：CineWeave Cloud
Provider Gateway 产品名：CineWeave Gateway
工作流模块：CineWeave Flow
素材与产物库：CineWeave Vault
管理后台：CineWeave Console
外部连接器协议：CineWeave Connector Protocol
本地开发数据库：cineweave
Docker image 前缀：cineweave/*
Kubernetes Helm release 示例：cineweave
```

命名约束：

```text
1. 新代码、包名、服务名、环境变量前缀、Docker 镜像名不得继续使用 toonflow-next。
2. 数据库、对象存储 bucket、NATS subject、Temporal namespace 建议使用 cineweave。
3. 文档中出现 Toonflow 时，只表示旧系统、历史上下文或迁移来源。
4. Provider Gateway 是技术通用名；对外产品名使用 CineWeave Gateway。
5. Workflow Board / 工作流模块对外产品名使用 CineWeave Flow。
6. Asset Library / Artifact Store 对外产品名使用 CineWeave Vault。
```

## 1. 背景与当前问题

### 1.1 当前系统形态

当前 Toonflow 是 Node.js / TypeScript / Express / SQLite / 本地文件存储 / Electron 混合架构。现有系统适合本地创作工具，但不适合作为高并发、多用户、云端 AI 视频生产平台。

当前供应商机制的核心问题：

```text
1. 供应商逻辑由用户或系统写 TypeScript 脚本。
2. 脚本被保存到 vendor/{id}.ts。
3. 运行时读取脚本，转译后放入 VM 执行。
4. VM 中导出 vendor.models / textRequest / imageRequest / videoRequest / ttsRequest。
5. 业务代码直接依赖这些函数执行 AI 调用。
```

这类机制的优点是灵活，但缺点非常明显：

```text
- 安全边界弱
- 多租户隔离困难
- 普通用户门槛高
- 供应商能力无法标准化
- 错误无法标准化
- 调用日志难统一
- 成本统计难准确
- 限流、熔断、降级难集中处理
- 无法形成可测试、可导入、可版本化的供应商配置资产
```

### 1.2 新系统目标

影织 / CineWeave 的目标是：

```text
1. 支持高并发、多租户、多用户协作。
2. 支持长任务可恢复、可重试、可暂停、可取消、可追踪。
3. 支持文本、图片、视频、音频、多模态模型的统一接入。
4. 支持普通用户无代码接入 API 供应商。
5. 支持高级用户通过声明式 Manifest 接入自定义 HTTP API。
6. 支持企业用户通过外部 Connector Service 接入复杂私有服务。
7. 支持项目级成本、供应商级成本、模型级成本统计。
8. 支持模型能力建模、路由、降级、限流、熔断。
9. 支持完整的工作流编排和可视化执行。
10. 支持云原生部署和可观测性。
```

### 1.3 非目标

以下内容不是本阶段目标：

```text
- 不兼容旧 TypeScript 供应商脚本执行机制。
- 不保留 Electron 作为核心运行形态。
- 不继续使用 SQLite 作为生产数据库。
- 不允许业务模块直接调用供应商 API。
- 不在平台进程内执行用户自定义代码。
- 不优先兼容旧前端 UI 结构。
```

---

## 2. 总体架构

### 2.1 架构图

```text
                         ┌────────────────────┐
                         │        CDN         │
                         └─────────┬──────────┘
                                   │
                         ┌─────────▼──────────┐
                         │   Ingress / WAF    │
                         └─────────┬──────────┘
                                   │
          ┌────────────────────────┼────────────────────────┐
          │                        │                        │
┌─────────▼─────────┐    ┌─────────▼─────────┐    ┌─────────▼─────────┐
│  Next.js Frontend │    │   Go API Server   │    │ Realtime Gateway  │
└───────────────────┘    └─────────┬─────────┘    └─────────┬─────────┘
                                   │                        │
                                   │                        │
          ┌────────────────────────┼────────────────────────┘
          │                        │
┌─────────▼─────────┐    ┌─────────▼─────────┐    ┌───────────────────┐
│    PostgreSQL     │    │       Redis       │    │  NATS JetStream   │
└───────────────────┘    └───────────────────┘    └───────────────────┘
          │                        │
          │                        │
┌─────────▼─────────┐    ┌─────────▼─────────┐
│     Temporal      │    │     S3 / MinIO    │
└─────────┬─────────┘    └───────────────────┘
          │
          │
┌─────────▼─────────────────────────────────────────────────────────────┐
│                            Worker Mesh                                │
│                                                                       │
│  Script Worker   Image Worker   Video Worker   Audio Worker           │
│  Media Worker    Quality Worker                                       │
│                                                                       │
│  所有 Worker 通过 Provider Gateway Service 调用上游模型，不直接访问供应商 API。 │
└───────────────────────────────────────────────────────────────────────┘
```

### 2.2 核心分层

| 层 | 名称 | 职责 |
|---|---|---|
| Frontend | Web App | 创作工作台、供应商接入、工作流可视化、素材管理 |
| Control Plane | Go API Server | 用户、组织、项目、权限、资产、工作流创建、配置管理 |
| Realtime Plane | Realtime Gateway | SSE / WebSocket 推送工作流进度、协作状态、队列状态 |
| Workflow Plane | Temporal | 长流程编排、重试、恢复、取消、补偿、状态历史 |
| Provider Plane | Provider Gateway | 供应商统一接入、模型能力、限流、熔断、成本、错误归一化 |
| Execution Plane | Workers | 执行剧本、图片、视频、音频、合成、质量检测等任务 |
| Data Plane | PostgreSQL / Redis / S3 / NATS | 持久化、缓存、对象存储、事件流 |
| Observability | OTel / Prometheus / Grafana | traces、metrics、logs、alerts |

### 2.3 状态源与模块边界

为避免 Codex 在实现时把多个基础设施职责混用，必须遵守以下边界：

```text
1. Temporal 是工作流执行状态源。
   - 负责 Activity 调度、重试、取消、恢复、历史事件。
   - 不作为业务查询数据库直接给前端使用。

2. PostgreSQL 是业务读模型和审计状态源。
   - 保存组织、项目、素材、WorkflowRun、NodeRun、Artifact、Provider 配置、成本、审计。
   - 所有前端列表、详情页优先查询 PostgreSQL。

3. NATS JetStream 是事件分发与实时推送通道。
   - 用于 workflow.node.completed、artifact.created、provider.call.failed 等事件。
   - 不作为主任务队列，也不代替 Temporal。

4. Redis 是缓存、短期状态、限流计数器。
   - 可用于 access token session、Provider lease 计数、幂等短缓存。
   - 不保存不可丢失的业务状态。

5. S3 / MinIO 是媒体与中间产物唯一对象存储。
   - 图片、视频、音频、字幕、缩略图、导出文件都必须先落 Artifact。
   - API 与 Worker 不共享本地磁盘作为生产数据源。

6. Provider Gateway 是唯一上游模型调用出口。
   - Worker、Workflow Activity、API Server 均不得直接调用供应商 API。
   - Provider Gateway 负责密钥、能力校验、请求编译、响应归一化、限流、成本、日志、媒体下载、S3 / MinIO 转存、Provider 侧 Artifact / media_file 写入。
```

所有表字段统一使用 `organization_id` 表示租户边界；文档中“tenant”仅作为多租户概念，不作为数据库字段名。

---

## 3. 推荐仓库结构

Codex 应按以下目录结构创建新平台。本仓库实际根目录为 `D:\Code\CineWeave`；下方 `cineweave/` 只表示仓库根，不得再创建嵌套的 `cineweave` 子目录。

```text
D:\Code\CineWeave\
  apps/
    web/                         # Next.js 前端
    api/                         # Go API Server
    realtime/                    # Go Realtime Gateway

  services/
    provider-gateway/            # AI Provider Gateway

  workers/
    script-worker/               # 文本、小说、剧本、分镜脚本 Worker
    image-worker/                # 图片生成、图片编辑、缩略图 Worker
    video-worker/                # 视频工作流编排 Worker；只调用 Provider Gateway，不直接调用供应商、不下载转存媒体
    audio-worker/                # TTS / 音频处理 Worker
    media-worker/                # FFmpeg 合成、转码、抽帧 Worker
    quality-worker/              # 视频/图片质量检测 Worker

  packages/
    proto/                       # gRPC / protobuf 定义
    openapi/                     # OpenAPI specs
    provider-manifest-schema/    # Provider Manifest JSON Schema
    shared-types/                # 前后端共享类型生成产物

  internal/
    auth/
    rbac/
    organizations/
    users/
    projects/
    assets/
    workflows/
    providers/
    storage/
    billing/
    audit/
    events/
    observability/
    config/

  db/
    migrations/
    seeds/

  deploy/
    docker-compose/
    helm/
    k8s/
    nginx/

  docs/
    architecture.md
    provider-gateway.md
    workflow-engine.md
    database-schema.md
    api-spec.md
    frontend-spec.md
    codex-execution-plan.md

  scripts/
    dev.sh
    test.sh
    migrate.sh
    generate-openapi.sh

  .github/
    workflows/
```

---

## 4. 技术选型

### 4.1 后端

```text
Language: Go
API Framework: chi 或 gin，优先 chi
Internal RPC: gRPC
API Contract: OpenAPI 3.1
DB Access: sqlc 或 ent，优先 sqlc
Workflow SDK: temporalio/sdk-go
Cache / Rate Limit: redis/go-redis
Event Bus: nats.go
Object Storage: AWS S3 compatible SDK
Observability: OpenTelemetry Go SDK
```

选择原则：

```text
- Go 用于高并发控制面和 Worker。
- Temporal 用于长任务状态机，避免手写复杂任务恢复逻辑。
- PostgreSQL 用于业务状态。
- Redis 用于缓存、短状态、限流。
- S3 / MinIO 用于媒体文件。
- NATS JetStream 用于事件分发，不作为主任务状态源。
```

### 4.2 前端

```text
Framework: Next.js
Language: TypeScript
UI: shadcn/ui + Tailwind CSS
State: Zustand
Server State: TanStack Query
Table: TanStack Table
Workflow UI: React Flow / xyflow
Editor: Monaco Editor
API Client: OpenAPI generated TypeScript client
Realtime: SSE first, WebSocket optional
```

### 4.3 基础设施

```text
Local Dev:
  Docker Compose

Production:
  Kubernetes
  Helm
  HPA / KEDA
  Ingress + TLS
  External PostgreSQL or managed DB
  External S3 or MinIO cluster
```

---

## 5. Provider Gateway 设计

Provider Gateway 是影织 / CineWeave 的核心模块之一。产品命名为 **CineWeave Gateway**；技术文档中仍使用 Provider Gateway 指代该模块。所有 AI 模型调用必须通过 Provider Gateway。

### 5.1 Provider Gateway 职责

```text
1. 管理供应商账户。
2. 管理供应商密钥。
3. 管理模型列表。
4. 管理模型能力。
5. 编译统一内部请求为供应商请求。
6. 归一化供应商响应。
7. 处理同步调用、异步任务、单次轮询、Webhook 入站适配。
8. 统一限流、配额、熔断。
9. 统一错误分类。
10. 统一成本统计。
11. 统一健康检查。
12. 支持模型路由和降级。
13. 下载供应商返回的媒体结果并转存 S3 / MinIO。
14. 写入 provider_call_logs、cost_records，以及 Gateway 侧 Artifact / media_file 记录。
```

边界硬约束：

```text
- Worker / Activity / API Server 不得直接调用任何供应商 API。
- Worker / Activity / API Server 不得直接读取供应商密钥。
- Worker / Activity 不负责下载上游媒体临时 URL，不负责转存 S3 / MinIO。
- Worker / Activity 只调用 Provider Gateway 内部接口，并消费 Gateway 返回的 artifactId / mediaFileId / providerCallId。
- Provider Gateway 不负责业务工作流编排；Temporal Workflow / Worker 负责 durable loop、重试、超时和取消。
- Provider Gateway 不在内存保存长任务状态；可恢复状态必须落在 provider_call_logs / provider_async_tasks / Temporal History / 业务表中。
```

### 5.2 Provider 类型

#### 5.2.1 Official Provider

官方内置适配器。用于主流供应商。

```text
- OpenAI-compatible
- Anthropic
- Google Gemini
- DeepSeek
- 通义千问
- 火山 / 豆包 / Seedance
- MiniMax
- xAI
- Ollama / Local
```

Official Provider 可以在 Go 里用原生 Adapter 实现。

#### 5.2.2 OpenAI-Compatible Provider

第一版真实 Provider 目标为 **New API**，实现形态为 OpenAI-compatible Adapter。该 Adapter 必须同时兼容 New API / One API / LiteLLM / OpenAI 官方的基础 Chat Completions 协议。

Base URL 规范：

```text
1. 用户可以填写 https://newapi.example.com 或 https://newapi.example.com/v1。
2. Provider Gateway 必须规范化 baseUrl，避免重复拼接 /v1。
3. 模型发现默认使用 GET {baseUrl}/models。
4. 文本生成默认使用 POST {baseUrl}/chat/completions。
5. 流式文本测试默认使用 POST {baseUrl}/chat/completions 且 stream=true。
6. Images / Embeddings / Responses API 不是第一版必需能力，可作为 capability optional。
```

第一版模型测试目标：

```text
- connection_test：检查 baseUrl 可访问。
- auth_test：使用 API Key 调用 /models 或最小 chat completion。
- model_discovery_test：GET /models，若供应商不支持则允许手动添加模型。
- text_generation_test：POST /chat/completions，messages=[{role:"user", content:"ping"}]。
- streaming_test：POST /chat/completions stream=true，能收到增量 delta 即通过。
- error_normalization_test：模拟 401 / 429 / 5xx，必须映射为标准错误码。
```

用户只需要配置：

```text
Provider type: OpenAI-compatible
Base URL
API Key
Models endpoint optional
Chat completions endpoint optional
```

#### 5.2.3 Declarative HTTP Provider

用于普通 HTTP API 的无代码接入。通过 YAML / JSON Manifest 描述请求和响应映射。

#### 5.2.4 External Connector Service

用于复杂供应商。CineWeave 不执行用户代码，只调用用户自建的 Connector Service。

### 5.3 内部统一接口

内部接口使用 `modelProfileKey` 作为业务稳定键，例如 `script_agent_default`。Provider Gateway 根据 `organizationId + modelProfileKey` 解析到 `model_profiles.id`，再根据路由策略选择具体 `provider_models.id`。外部管理 API 可以使用 UUID，但 Workflow 模板、项目设置和前端绑定应优先保存稳定 key。

以下 JSON 中的 `org_001 / ws_001 / project_001` 是便于阅读的示例占位符；实际实现中数据库主键使用 UUID，前端展示可另行使用 slug 或短 ID。

#### Text Generate

```http
POST /internal/provider/text/generate
```

Request:

```json
{
  "organizationId": "org_001",
  "workspaceId": "ws_001",
  "projectId": "project_001",
  "workflowRunId": "wf_001",
  "nodeRunId": "node_001",
  "modelProfileKey": "script_agent_default",
  "input": {
    "messages": [
      { "role": "system", "content": "..." },
      { "role": "user", "content": "..." }
    ],
    "temperature": 0.7,
    "maxOutputTokens": 4096,
    "responseFormat": "text"
  },
  "options": {
    "timeoutMs": 120000,
    "idempotencyKey": "hash"
  }
}
```

Response:

```json
{
  "providerCallId": "call_001",
  "modelId": "model_001",
  "status": "succeeded",
  "output": {
    "text": "...",
    "raw": {}
  },
  "usage": {
    "inputTokens": 1234,
    "outputTokens": 567,
    "estimatedCost": "0.0123"
  }
}
```

#### Image Generate

```http
POST /internal/provider/image/generate
```

Request:

```json
{
  "organizationId": "org_001",
  "workspaceId": "ws_001",
  "projectId": "project_001",
  "workflowRunId": "wf_001",
  "nodeRunId": "node_002",
  "modelProfileKey": "image_generation_default",
  "input": {
    "prompt": "...",
    "references": [
      {
        "type": "image",
        "assetId": "asset_001",
        "storageKey": "org/org_001/workspace/ws_001/project/proj_001/ref.jpg"
      }
    ],
    "aspectRatio": "9:16",
    "size": "2K",
    "count": 1
  },
  "options": {
    "timeoutMs": 300000,
    "idempotencyKey": "hash"
  }
}
```

Response:

```json
{
  "providerCallId": "call_002",
  "status": "succeeded",
  "artifacts": [
    {
      "type": "image",
      "storageKey": "org/org_001/workspace/ws_001/project/proj_001/generated/img_001.webp",
      "mimeType": "image/webp",
      "width": 1024,
      "height": 1792
    }
  ],
  "usage": {
    "imageCount": 1,
    "estimatedCost": "0.0400"
  }
}
```

#### Video Generate

```http
POST /internal/provider/video/generate
```

Request:

```json
{
  "organizationId": "org_001",
  "workspaceId": "ws_001",
  "projectId": "project_001",
  "workflowRunId": "wf_001",
  "nodeRunId": "node_003",
  "modelProfileKey": "video_generation_default",
  "input": {
    "prompt": "...",
    "references": [
      {
        "type": "image",
        "assetId": "asset_001",
        "storageKey": "org/org_001/workspace/ws_001/project/proj_001/storyboard.jpg"
      }
    ],
    "taskType": "video.image_to_video",
    "aspectRatio": "9:16",
    "duration": 10,
    "resolution": "1080p",
    "audio": false
  },
  "options": {
    "timeoutMs": 1800000,
    "idempotencyKey": "hash"
  }
}
```

Response for async provider:

```json
{
  "providerCallId": "call_003",
  "status": "running",
  "externalTaskId": "upstream_task_123",
  "pollAfterMs": 5000
}
```

Final normalized response:

```json
{
  "providerCallId": "call_003",
  "status": "succeeded",
  "artifacts": [
    {
      "type": "video",
      "storageKey": "org/org_001/workspace/ws_001/project/proj_001/video/clip_001.mp4",
      "mimeType": "video/mp4",
      "duration": 10,
      "width": 1080,
      "height": 1920
    }
  ],
  "usage": {
    "videoSeconds": 10,
    "estimatedCost": "0.8000"
  }
}
```

#### Audio / TTS Generate

```http
POST /internal/provider/audio/tts
```

Request:

```json
{
  "organizationId": "org_001",
  "workspaceId": "ws_001",
  "projectId": "project_001",
  "workflowRunId": "wf_001",
  "nodeRunId": "node_004",
  "modelProfileKey": "tts_default",
  "input": {
    "text": "台词内容",
    "voice": "female_young_01",
    "language": "zh-CN",
    "speed": 1.0,
    "format": "mp3"
  },
  "options": {
    "timeoutMs": 300000,
    "idempotencyKey": "hash"
  }
}
```

Response:

```json
{
  "providerCallId": "call_004",
  "status": "succeeded",
  "artifacts": [
    {
      "type": "audio",
      "storageKey": "org/org_001/workspace/ws_001/project/proj_001/audio/line_001.mp3",
      "mimeType": "audio/mpeg",
      "duration": 4.2
    }
  ],
  "usage": {
    "characters": 38,
    "estimatedCost": "0.0060"
  }
}
```

#### Provider Task Status / Cancel

所有异步供应商任务的轮询和取消必须经由 Provider Gateway。Worker 不得直接调用上游轮询 API。

```http
GET  /internal/provider/tasks/{providerCallId}
POST /internal/provider/tasks/{providerCallId}/cancel
```

Task status response:

```json
{
  "providerCallId": "call_003",
  "status": "running",
  "externalTaskId": "upstream_task_123",
  "pollAfterMs": 5000,
  "progress": 0.42,
  "artifacts": [],
  "error": null
}
```

### 5.4 模型能力注册表

每个模型必须有能力描述。

```json
{
  "modelKey": "seedance-v1",
  "displayName": "Seedance V1",
  "modality": "video",
  "taskTypes": [
    "video.text_to_video",
    "video.image_to_video",
    "video.first_last_frame"
  ],
  "inputLimits": {
    "maxPromptChars": 3000,
    "maxReferenceImages": 2,
    "maxReferenceVideos": 0,
    "maxSingleImageBytes": 2097152,
    "maxTotalReferenceBytes": 8388608
  },
  "outputOptions": {
    "aspectRatios": ["16:9", "9:16", "1:1"],
    "durations": [5, 10],
    "resolutions": ["720p", "1080p"],
    "audioSupported": false
  },
  "executionMode": "async_polling",
  "timeoutPolicy": {
    "createTimeoutMs": 60000,
    "resultTimeoutMs": 1800000
  },
  "retryPolicy": {
    "maxAttempts": 3,
    "backoff": "exponential"
  },
  "rateLimitPolicy": {
    "maxConcurrency": 3,
    "requestsPerMinute": 20
  },
  "pricingPolicy": {
    "unit": "video_second",
    "price": "0.08",
    "currency": "USD"
  }
}
```

### 5.5 Model Profile

业务模块不得直接绑定具体供应商模型。应绑定 Model Profile。

```text
script_agent_default
storyboard_agent_default
image_generation_default
video_generation_default
tts_default
quality_check_default
```

Model Profile 可以配置多个真实模型：

```yaml
id: video_generation_default
routingStrategy: priority_with_fallback
bindings:
  - providerModelId: model_fast
    priority: 1
    weight: 100
    enabled: true
  - providerModelId: model_quality
    priority: 2
    weight: 100
    enabled: true
  - providerModelId: model_fallback
    priority: 3
    weight: 100
    enabled: true
```

支持路由策略：

```text
priority
priority_with_fallback
weighted_random
least_cost
least_latency
health_based
organization_plan_based
manual
```

### 5.6 声明式 HTTP Provider Manifest

Manifest 示例：

```yaml
kind: ProviderConnector
version: v1
id: custom-video-api
name: 自定义视频 API
transport: http

baseUrl: "https://api.example.com"

auth:
  type: bearer
  header: Authorization
  valueTemplate: "Bearer {{secret.apiKey}}"

models:
  - id: video-model-v1
    displayName: Video Model V1
    modality: video
    capabilities:
      taskTypes:
        - video.image_to_video
      aspectRatios:
        - "16:9"
        - "9:16"
      durations:
        - 5
        - 10
      resolutions:
        - "720p"
        - "1080p"
      maxReferenceImages: 1
      executionMode: async_polling

endpoints:
  createVideo:
    method: POST
    path: /v1/video/generations
    headers:
      Content-Type: application/json
    body:
      model: "{{model.id}}"
      prompt: "{{input.prompt}}"
      image_url: "{{input.references[0].signedUrl}}"
      duration: "{{input.duration}}"
      aspect_ratio: "{{input.aspectRatio}}"
      resolution: "{{input.resolution}}"
    response:
      taskId: "$.data.task_id"

  pollVideo:
    method: GET
    path: /v1/video/generations/{{task.taskId}}
    response:
      status: "$.data.status"
      successWhen: "succeeded"
      failedWhen: "failed"
      videoUrl: "$.data.video_url"
      errorMessage: "$.data.error.message"
```

Manifest 必须通过 JSON Schema 校验。禁止执行脚本表达式，只允许安全模板变量和 JSONPath 映射。

### 5.7 External Connector Service Protocol

复杂供应商使用外部 Connector Service。

接口：

```http
GET  /metadata
GET  /models
POST /invoke/text
POST /invoke/image
POST /invoke/video
POST /invoke/audio
GET  /tasks/{id}
POST /tasks/{id}/cancel
GET  /health
```

`GET /metadata` Response:

```json
{
  "connectorId": "enterprise-custom-provider",
  "name": "Enterprise Custom Provider",
  "version": "1.0.0",
  "supportedModalities": ["text", "image", "video"],
  "authSchemes": ["bearer", "mTLS"]
}
```

CineWeave 不负责执行该服务内部代码，只负责调用标准协议。

### 5.8 Provider 错误归一化

所有上游错误必须转换为标准错误码：

```text
AUTH_FAILED
QUOTA_EXCEEDED
RATE_LIMITED
MODEL_NOT_FOUND
INVALID_REQUEST
UNSUPPORTED_CAPABILITY
UPSTREAM_TIMEOUT
UPSTREAM_INTERNAL_ERROR
POLLING_TIMEOUT
RESULT_EXPIRED
MEDIA_DOWNLOAD_FAILED
CONTENT_REJECTED
UNKNOWN_ERROR
```

标准错误结构：

```json
{
  "code": "RATE_LIMITED",
  "message": "供应商请求过于频繁",
  "retryable": true,
  "retryAfterMs": 30000,
  "upstreamStatus": 429,
  "upstreamCode": "TooManyRequests"
}
```

### 5.9 Provider Lease 限流

视频生成等昂贵任务执行前必须申请 lease。

```text
1. Worker / Activity 请求 Provider Gateway acquire lease。
2. Provider Gateway 检查 organization / provider / model / task_type 限流。
3. 通过后返回 leaseId。
4. Worker / Activity 携带 leaseId 调用 Provider Gateway 的 generate / create-task / poll / cancel 接口。
5. Provider Gateway 使用 lease 执行供应商调用、媒体下载、S3 / MinIO 转存、provider_call_logs 与 cost_records 写入。
6. Provider Gateway 在调用完成、失败、取消后 release lease。
7. lease 超时由 Provider Gateway 自动回收。
```

Lease 表：

```sql
CREATE TABLE provider_leases (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID NOT NULL REFERENCES provider_models(id),
  task_type TEXT NOT NULL,
  workflow_run_id UUID REFERENCES workflow_runs(id),
  node_run_id UUID REFERENCES workflow_node_runs(id),
  provider_call_id UUID REFERENCES provider_call_logs(id),
  acquired_by_service TEXT NOT NULL DEFAULT 'provider-gateway',
  status TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  released_at TIMESTAMPTZ
);
```

### 5.10 Provider Adapter Contract

Official Provider 与 Declarative Provider 最终都必须适配到同一组内部接口。Codex 实现时先定义领域接口，再实现具体 Provider。

```go
type ProviderAdapter interface {
    ConnectorKey() string
    DiscoverModels(ctx context.Context, account ProviderAccount) ([]ProviderModel, error)
    HealthCheck(ctx context.Context, account ProviderAccount) error
}

type TextProvider interface {
    GenerateText(ctx context.Context, req TextGenerateRequest) (TextGenerateResult, error)
    StreamText(ctx context.Context, req TextGenerateRequest) (<-chan TextStreamEvent, error)
}

type ImageProvider interface {
    GenerateImage(ctx context.Context, req ImageGenerateRequest) (ImageGenerateResult, error)
}

type VideoProvider interface {
    CreateVideoTask(ctx context.Context, req VideoGenerateRequest) (ProviderTask, error)
    GetTask(ctx context.Context, task ProviderTask) (ProviderTaskStatus, error)
    CancelTask(ctx context.Context, task ProviderTask) error
}

type AudioProvider interface {
    TextToSpeech(ctx context.Context, req TTSRequest) (TTSResult, error)
}
```

实现要求：

```text
- Adapter 不读取明文密钥，只通过 Credential Vault 获取短期解密结果。
- Adapter 返回 domain error，不返回未归一化的上游错误。
- Adapter 不直接写数据库；Provider Gateway service 统一写 provider_call_logs。
- Adapter 不直接决定业务路由；Routing Engine 根据 Model Profile 决定模型。
- Adapter 产出的媒体结果必须由 Provider Gateway 下载并转存 S3 / MinIO，不能只返回上游临时 URL。
```

### 5.11 Provider Gateway 执行流程

同步调用流程：

```text
1. Worker / Activity 调用 Provider Gateway。
2. Provider Gateway 校验 organization、service identity、Model Profile、模型能力。
3. Routing Engine 选择 provider_model，并确定 model_profile_id / model_profile_binding_id。
4. Credential Vault 解密本次调用所需 credential_id。
5. Rate Limiter / Lease Manager 申请并消费调用额度。
6. Request Compiler 转换为上游请求。
7. Adapter 执行上游调用。
8. Response Normalizer 归一化输出和错误。
9. 如果输出包含上游媒体 URL，Provider Gateway 下载并转存 S3 / MinIO。
10. Provider Gateway 写 provider_call_logs、cost_records、media_files / artifacts。
11. Provider Gateway 释放 lease，发布 NATS / event_outbox 事件。
12. Worker / Activity 只接收 normalized result、providerCallId、artifactId / mediaFileId。
```

异步调用流程：

```text
1. Worker / Activity 调用 Provider Gateway create-task。
2. Provider Gateway 申请 lease、调用上游 create、写 provider_call_logs 和 provider_async_tasks。
3. Provider Gateway 返回 providerCallId、externalTaskId、pollAfterMs。
4. Temporal Workflow / Worker 使用 durable timer 等待 pollAfterMs。
5. Worker / Activity 调用 Provider Gateway poll(providerCallId)。
6. Provider Gateway 执行一次上游 poll，归一化状态，并更新 provider_async_tasks / provider_call_logs。
7. 如果状态为 running，Gateway 返回 pollAfterMs，Worker 继续 durable loop。
8. 如果状态为 succeeded 且包含媒体 URL，Gateway 下载并转存 S3 / MinIO，写 media_files / artifacts。
9. 如果状态为 failed / cancelled，Gateway 写标准错误和日志。
10. Worker / Activity 根据 Gateway 返回结果更新 Workflow Node 状态。
```

取消流程：

```text
1. Workflow 收到 cancel signal。
2. Worker / Activity 调用 Provider Gateway cancel(providerCallId)。
3. Provider Gateway 调用上游 cancel，如果供应商不支持 cancel，则记录 best-effort cancellation。
4. Provider Gateway 释放 lease，更新 provider_call_logs / provider_async_tasks。
5. Worker / Activity 更新 Temporal Workflow 状态。
```

### 5.12 异步 Provider 边界

Provider Gateway 负责供应商协议适配，但不负责业务工作流编排。

```text
- Provider Gateway 负责：create / single poll / cancel / webhook signature verification / response normalization / media download / S3 transfer / provider logs。
- Temporal Workflow / Worker 负责：durable polling loop、重试、超时、取消、业务状态流转。
- Provider Gateway 可返回 externalTaskId、pollAfterMs、normalized status、artifactId、mediaFileId。
- Worker 根据 Temporal retry / timer 机制再次调用 Provider Gateway poll 接口。
- 如果供应商支持 webhook，Provider Gateway 接收 webhook 后写 event_outbox，并 Signal 对应 Temporal Workflow。
```

禁止让 Provider Gateway 在自身内存中保存长任务状态。所有长任务状态必须在 Temporal History、provider_async_tasks、provider_call_logs 和业务库中可恢复。

### 5.13 Provider Webhook Ingress

需要支持异步供应商回调：

```http
POST /api/provider-webhooks/{providerAccountId}/{webhookSecret}
```

要求：

```text
- 校验 webhookSecret 或供应商签名。
- 解析 externalTaskId。
- 更新 provider_async_tasks 和 provider_call_logs 的最新状态。
- 写入 event_outbox。
- Signal Temporal Workflow。
- Webhook HTTP 请求必须快速返回；不得在 webhook 请求生命周期内下载大文件。
- 如 webhook payload 含最终媒体 URL，Provider Gateway 记录待转存状态；后续由 Gateway 的 poll/finalize 调用完成下载和 S3 / MinIO 转存。
```

---

## 6. Workflow Engine 设计

### 6.1 为什么使用 Temporal

影织 / CineWeave 的核心生产链路天然是长流程：

```text
小说 → 事件图谱 → 剧本 → 分镜 → 图片 → 视频 → 配音 → 合成 → 导出
```

这些流程需要：

```text
- 持久化状态
- 失败恢复
- 自动重试
- 人工审核
- 取消
- 暂停
- 版本化
- 可观测历史
```

因此应使用 Temporal 作为工作流运行时。

### 6.2 Workflow 类型

第一阶段实现以下 Workflow：

```text
TextToStoryboardWorkflow
NovelToScriptWorkflow
ScriptToStoryboardWorkflow
StoryboardToImageWorkflow
StoryboardToVideoWorkflow
VideoComposeWorkflow
NovelToVideoWorkflow
```

### 6.3 NovelToVideoWorkflow 示例

```text
NovelToVideoWorkflow
  1. ValidateProjectActivity
  2. LoadNovelActivity
  3. CleanNovelActivity
  4. ExtractEventsActivity
  5. GenerateScriptActivity
  6. GenerateStoryboardActivity
  7. GenerateImageBatchActivity
  8. GenerateVideoBatchActivity
  9. GenerateAudioBatchActivity optional
  10. ComposeTimelineActivity
  11. QualityCheckActivity
  12. ExportFinalVideoActivity
```

### 6.4 Activity 规则

每个 Activity 必须满足：

```text
- 幂等
- 有 timeout
- 有 retry policy
- 可记录 provider_call_id
- 可记录 artifact_id
- 可记录 cost
- 不直接依赖前端状态
- 输入输出可 JSON 序列化
```

Activity 输出示例：

```json
{
  "status": "succeeded",
  "artifacts": ["artifact_001"],
  "providerCalls": ["call_001"],
  "cost": "0.1234",
  "metadata": {}
}
```

### 6.5 WorkflowRun 与 NodeRun

Temporal 是执行状态源，但业务库也要持久化可查询状态。

```sql
CREATE TABLE workflow_runs (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  template_id UUID REFERENCES workflow_templates(id),
  temporal_workflow_id TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  input JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  error_code TEXT,
  error_message TEXT,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  cancelled_at TIMESTAMPTZ
);

CREATE TABLE workflow_node_runs (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  workflow_run_id UUID NOT NULL REFERENCES workflow_runs(id),
  node_key TEXT NOT NULL,
  node_type TEXT NOT NULL,
  status TEXT NOT NULL,
  input JSONB NOT NULL DEFAULT '{}',
  output JSONB NOT NULL DEFAULT '{}',
  retry_count INT NOT NULL DEFAULT 0,
  error_code TEXT,
  error_message TEXT,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(workflow_run_id, node_key)
);
```

状态枚举：

```text
pending
queued
running
succeeded
failed
cancelled
skipped
waiting_review
```

---

## 7. Artifact 设计

所有中间产物统一使用 Artifact 模型。

```sql
CREATE TABLE artifacts (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  workflow_run_id UUID REFERENCES workflow_runs(id),
  node_run_id UUID REFERENCES workflow_node_runs(id),
  type TEXT NOT NULL,
  storage_key TEXT,
  mime_type TEXT,
  content_hash TEXT,
  prompt_hash TEXT,
  model_id UUID REFERENCES provider_models(id),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Artifact 类型：

```text
novel_raw_text
novel_clean_text
event_graph_json
script_markdown
script_json
storyboard_json
shot_image
grid_image
video_clip
audio_clip
timeline_json
final_video
thumbnail
quality_report
```

存储路径规范：

```text
org/{orgId}/workspace/{workspaceId}/project/{projectId}/artifacts/{artifactId}/{filename}
```

示例：

```text
org/org_001/workspace/ws_001/project/proj_001/artifacts/art_001/video.mp4
```

---

## 8. 数据库核心 Schema

### 8.0 Migration 顺序

Codex 生成 migrations 时必须按依赖顺序拆分，不要把以下 SQL 片段机械复制到一个文件后直接执行。推荐顺序：

```text
1. organizations / users / auth_sessions
2. roles / permissions / role_permissions
3. organization_members / teams / team_members / workspaces
4. projects / project_members / role_bindings
5. provider_connectors / provider_accounts / provider_credentials / provider_models / provider_model_capabilities
6. model_profiles / model_profile_bindings / provider_endpoints / provider_test_runs
7. workflow_templates / workflow_template_nodes
8. workflow_runs / workflow_node_runs / artifacts
9. prompt_templates / prompt_versions
10. provider_call_logs / provider_async_tasks / cost_records / provider_leases / event_outbox
11. novels / scripts / storyboards / storyboard_shots / assets / media_files
12. indexes / constraints / seed roles / seed permissions / seed default model profiles
```

如果数据库不允许跨顺序引用外键，则必须确保 prompt_versions、model_profiles、provider_credentials 在 provider_call_logs 前创建。

### 8.1 多租户与权限

权限模型硬约束：

```text
- organization_members / project_members 只表达成员关系，不表达权限。
- 权限统一由 role_bindings + role_permissions 计算。
- role_bindings 支持 user / team 两类 subject。
- role_bindings 支持 organization / workspace / project 三类 resource scope。
- Service-to-service 权限不走用户 RBAC，使用 service identity + internal policy。
```

```sql
CREATE TABLE organizations (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id UUID PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT,
  display_name TEXT,
  avatar_url TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE auth_sessions (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  organization_id UUID REFERENCES organizations(id),
  refresh_token_hash TEXT NOT NULL UNIQUE,
  user_agent TEXT,
  ip_address TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE roles (
  id UUID PRIMARY KEY,
  organization_id UUID REFERENCES organizations(id),
  role_key TEXT NOT NULL,
  name TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('organization', 'workspace', 'project')),
  is_system BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, role_key, scope)
);

CREATE TABLE permissions (
  permission_key TEXT PRIMARY KEY,
  description TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE role_permissions (
  role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission_key TEXT NOT NULL REFERENCES permissions(permission_key) ON DELETE CASCADE,
  PRIMARY KEY (role_id, permission_key)
);

CREATE TABLE organization_members (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE teams (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, slug)
);

CREATE TABLE team_members (
  team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, user_id)
);

CREATE TABLE workspaces (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 8.2 项目与角色绑定

```sql
CREATE TABLE projects (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  project_type TEXT,
  aspect_ratio TEXT,
  settings JSONB NOT NULL DEFAULT '{}',
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_members (
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, user_id)
);

CREATE TABLE role_bindings (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'team')),
  subject_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  subject_team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('organization', 'workspace', 'project')),
  resource_organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  resource_workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
  resource_project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
  created_by UUID REFERENCES users(id),
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    (subject_type = 'user' AND subject_user_id IS NOT NULL AND subject_team_id IS NULL)
    OR
    (subject_type = 'team' AND subject_team_id IS NOT NULL AND subject_user_id IS NULL)
  ),
  CHECK (
    (resource_type = 'organization' AND resource_organization_id IS NOT NULL AND resource_workspace_id IS NULL AND resource_project_id IS NULL)
    OR
    (resource_type = 'workspace' AND resource_workspace_id IS NOT NULL AND resource_organization_id IS NULL AND resource_project_id IS NULL)
    OR
    (resource_type = 'project' AND resource_project_id IS NOT NULL AND resource_organization_id IS NULL AND resource_workspace_id IS NULL)
  )
);
```

RBAC 计算规则：

```text
1. 先确认用户是 organization_members.active 成员。
2. 如果访问 project 资源，确认用户是 project_members.active 成员，或通过 team_members 间接属于拥有项目角色的团队。
3. 收集用户直接 role_bindings 与其 team 的 role_bindings。
4. 只使用 resource_type 与目标资源匹配或上级资源匹配的绑定。
5. role.scope 必须与绑定 resource_type 匹配。
6. 通过 role_permissions 得到 permission_key 集合。
7. deny 规则第一版不实现；后续需要时新增 permission_effect。
```

### 8.3 Provider

```sql
CREATE TABLE provider_connectors (
  id UUID PRIMARY KEY,
  connector_key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  is_official BOOLEAN NOT NULL DEFAULT false,
  manifest JSONB,
  version TEXT NOT NULL DEFAULT 'v1',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_accounts (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  connector_id UUID NOT NULL REFERENCES provider_connectors(id),
  name TEXT NOT NULL,
  base_url TEXT,
  auth_type TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  config JSONB NOT NULL DEFAULT '{}',
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_credentials (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
  credential_key TEXT NOT NULL DEFAULT 'default',
  credential_type TEXT NOT NULL DEFAULT 'api_key',
  secret_ref TEXT,
  encrypted_payload BYTEA,
  masked_preview TEXT,
  encryption_key_id TEXT,
  encryption_version INT NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'active',
  is_active BOOLEAN NOT NULL DEFAULT true,
  rotated_from_credential_id UUID REFERENCES provider_credentials(id),
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  rotated_at TIMESTAMPTZ,
  UNIQUE(provider_account_id, credential_key, is_active)
);

CREATE TABLE provider_models (
  id UUID PRIMARY KEY,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  model_key TEXT NOT NULL,
  display_name TEXT NOT NULL,
  modality TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider_account_id, model_key)
);

CREATE TABLE provider_model_capabilities (
  id UUID PRIMARY KEY,
  provider_model_id UUID NOT NULL REFERENCES provider_models(id),
  task_types JSONB NOT NULL DEFAULT '[]',
  input_limits JSONB NOT NULL DEFAULT '{}',
  output_options JSONB NOT NULL DEFAULT '{}',
  execution_mode TEXT NOT NULL,
  retry_policy JSONB NOT NULL DEFAULT '{}',
  rate_limit_policy JSONB NOT NULL DEFAULT '{}',
  pricing_policy JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_endpoints (
  id UUID PRIMARY KEY,
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  endpoint_key TEXT NOT NULL,
  endpoint_type TEXT NOT NULL,
  method TEXT NOT NULL,
  path_template TEXT NOT NULL,
  headers_template JSONB NOT NULL DEFAULT '{}',
  request_template JSONB NOT NULL DEFAULT '{}',
  response_mapping JSONB NOT NULL DEFAULT '{}',
  timeout_ms INT NOT NULL DEFAULT 120000,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider_account_id, endpoint_key)
);

CREATE TABLE provider_test_runs (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID REFERENCES provider_models(id),
  test_type TEXT NOT NULL,
  status TEXT NOT NULL,
  request_snapshot JSONB NOT NULL DEFAULT '{}',
  response_snapshot JSONB,
  normalized_output JSONB,
  error_code TEXT,
  error_message TEXT,
  latency_ms INT,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE model_profiles (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  profile_key TEXT NOT NULL,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL,
  routing_strategy TEXT NOT NULL DEFAULT 'priority',
  fallback_strategy JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, profile_key),
  UNIQUE(organization_id, purpose)
);

CREATE TABLE model_profile_bindings (
  id UUID PRIMARY KEY,
  model_profile_id UUID NOT NULL REFERENCES model_profiles(id),
  provider_model_id UUID NOT NULL REFERENCES provider_models(id),
  priority INT NOT NULL DEFAULT 100,
  weight INT NOT NULL DEFAULT 100,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 8.4 Provider 调用日志与成本

```sql
CREATE TABLE provider_call_logs (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID REFERENCES projects(id),
  workflow_run_id UUID REFERENCES workflow_runs(id),
  node_run_id UUID REFERENCES workflow_node_runs(id),
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID REFERENCES provider_models(id),
  credential_id UUID REFERENCES provider_credentials(id),
  model_profile_id UUID REFERENCES model_profiles(id),
  model_profile_binding_id UUID REFERENCES model_profile_bindings(id),
  model_profile_key TEXT,
  prompt_version_id UUID REFERENCES prompt_versions(id),
  prompt_hash TEXT,
  input_hash TEXT,
  output_hash TEXT,
  task_type TEXT NOT NULL,
  execution_mode TEXT NOT NULL DEFAULT 'sync',
  status TEXT NOT NULL,
  upstream_request_id TEXT,
  external_task_id TEXT,
  lease_id UUID,
  idempotency_key TEXT,
  latency_ms INT,
  input_tokens INT,
  output_tokens INT,
  media_count INT,
  duration_seconds NUMERIC,
  estimated_cost NUMERIC(18, 8),
  currency TEXT DEFAULT 'USD',
  error_code TEXT,
  error_message TEXT,
  upstream_status INT,
  upstream_error_code TEXT,
  request_hash TEXT,
  request_snapshot JSONB NOT NULL DEFAULT '{}',
  response_snapshot JSONB,
  normalized_output JSONB,
  artifact_ids JSONB NOT NULL DEFAULT '[]',
  media_file_ids JSONB NOT NULL DEFAULT '[]',
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE provider_async_tasks (
  id UUID PRIMARY KEY,
  provider_call_id UUID NOT NULL REFERENCES provider_call_logs(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  provider_account_id UUID NOT NULL REFERENCES provider_accounts(id),
  provider_model_id UUID REFERENCES provider_models(id),
  external_task_id TEXT NOT NULL,
  status TEXT NOT NULL,
  poll_after TIMESTAMPTZ,
  result_expires_at TIMESTAMPTZ,
  raw_status JSONB NOT NULL DEFAULT '{}',
  last_poll_at TIMESTAMPTZ,
  finalized_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(provider_account_id, external_task_id)
);

CREATE TABLE cost_records (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID REFERENCES projects(id),
  workflow_run_id UUID REFERENCES workflow_runs(id),
  node_run_id UUID REFERENCES workflow_node_runs(id),
  provider_call_id UUID REFERENCES provider_call_logs(id),
  provider_model_id UUID REFERENCES provider_models(id),
  credential_id UUID REFERENCES provider_credentials(id),
  model_profile_id UUID REFERENCES model_profiles(id),
  cost_type TEXT NOT NULL,
  amount NUMERIC(18, 8) NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  unit TEXT,
  quantity NUMERIC(18, 6),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

追溯要求：

```text
- 每次 Provider 调用必须记录 credential_id，用于密钥轮换后的审计和问题定位。
- 每次 Provider 调用必须记录 model_profile_id / model_profile_key / provider_model_id，用于模型路由追踪。
- 由 Prompt Template 触发的调用必须记录 prompt_version_id 与 prompt_hash。
- 不得在 request_snapshot / response_snapshot 中保存明文 API Key、Authorization、Cookie、access_token、refresh_token。
- artifact_ids / media_file_ids 只保存内部 ID，不保存上游临时 URL。
```


### 8.5 审计日志

```sql
CREATE TABLE audit_logs (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  actor_user_id UUID REFERENCES users(id),
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id UUID,
  metadata JSONB NOT NULL DEFAULT '{}',
  ip_address TEXT,
  user_agent TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 8.6 Workflow 模板、事件、幂等与人工审核

以下表必须和 6.5、7 里的 `workflow_runs`、`workflow_node_runs`、`artifacts` 一起进入正式 migrations。

```sql
CREATE TABLE workflow_templates (
  id UUID PRIMARY KEY,
  organization_id UUID REFERENCES organizations(id),
  template_key TEXT NOT NULL,
  name TEXT NOT NULL,
  version TEXT NOT NULL DEFAULT 'v1',
  definition JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, template_key, version)
);

CREATE TABLE workflow_template_nodes (
  id UUID PRIMARY KEY,
  template_id UUID NOT NULL REFERENCES workflow_templates(id),
  node_key TEXT NOT NULL,
  node_type TEXT NOT NULL,
  config JSONB NOT NULL DEFAULT '{}',
  depends_on JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(template_id, node_key)
);

CREATE TABLE event_outbox (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID REFERENCES projects(id),
  event_type TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID,
  payload JSONB NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);

CREATE TABLE idempotency_keys (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  key TEXT NOT NULL,
  scope TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_snapshot JSONB,
  status TEXT NOT NULL DEFAULT 'processing',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, scope, key)
);

CREATE TABLE review_tasks (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  workflow_run_id UUID REFERENCES workflow_runs(id),
  node_run_id UUID REFERENCES workflow_node_runs(id),
  status TEXT NOT NULL DEFAULT 'pending',
  review_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}',
  assigned_to UUID REFERENCES users(id),
  resolved_by UUID REFERENCES users(id),
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`event_outbox` 是 PostgreSQL 与 NATS / SSE 之间的可靠投递边界。业务事务提交时写入 outbox，独立 publisher 负责投递到 NATS JetStream，并在成功后标记 `published_at`。

### 8.7 创作域核心 Schema

Artifact 是工作流输出产物；Asset 是用户在项目中可管理、可复用的素材。二者必须区分。

```sql
CREATE TABLE novels (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL,
  source_type TEXT,
  raw_artifact_id UUID REFERENCES artifacts(id),
  clean_artifact_id UUID REFERENCES artifacts(id),
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE novel_chapters (
  id UUID PRIMARY KEY,
  novel_id UUID NOT NULL REFERENCES novels(id),
  chapter_index INT NOT NULL,
  title TEXT,
  content_artifact_id UUID REFERENCES artifacts(id),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(novel_id, chapter_index)
);

CREATE TABLE novel_events (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  novel_id UUID REFERENCES novels(id),
  chapter_id UUID REFERENCES novel_chapters(id),
  event_index INT NOT NULL,
  event_type TEXT,
  summary TEXT NOT NULL,
  characters JSONB NOT NULL DEFAULT '[]',
  scenes JSONB NOT NULL DEFAULT '[]',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE scripts (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL,
  current_version_id UUID,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE script_versions (
  id UUID PRIMARY KEY,
  script_id UUID NOT NULL REFERENCES scripts(id),
  version_no INT NOT NULL,
  content_artifact_id UUID REFERENCES artifacts(id),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(script_id, version_no)
);

CREATE TABLE storyboards (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  script_id UUID REFERENCES scripts(id),
  title TEXT NOT NULL,
  current_version_no INT NOT NULL DEFAULT 1,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE storyboard_shots (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  storyboard_id UUID NOT NULL REFERENCES storyboards(id),
  script_version_id UUID REFERENCES script_versions(id),
  shot_index INT NOT NULL,
  duration_seconds NUMERIC,
  shot_size TEXT,
  camera_move TEXT,
  action TEXT,
  dialogue TEXT,
  asset_bindings JSONB NOT NULL DEFAULT '[]',
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(storyboard_id, shot_index)
);

CREATE TABLE assets (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  asset_type TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  current_artifact_id UUID REFERENCES artifacts(id),
  metadata JSONB NOT NULL DEFAULT '{}',
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE asset_relations (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID NOT NULL REFERENCES projects(id),
  source_asset_id UUID NOT NULL REFERENCES assets(id),
  target_asset_id UUID NOT NULL REFERENCES assets(id),
  relation_type TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE media_files (
  id UUID PRIMARY KEY,
  organization_id UUID NOT NULL REFERENCES organizations(id),
  project_id UUID REFERENCES projects(id),
  artifact_id UUID REFERENCES artifacts(id),
  storage_key TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  byte_size BIGINT,
  width INT,
  height INT,
  duration_seconds NUMERIC,
  checksum TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE media_variants (
  id UUID PRIMARY KEY,
  media_file_id UUID NOT NULL REFERENCES media_files(id),
  variant_type TEXT NOT NULL,
  storage_key TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 8.8 Prompt / Skill 版本化

所有生成结果必须能追溯 prompt 模板和版本。

```sql
CREATE TABLE prompt_templates (
  id UUID PRIMARY KEY,
  organization_id UUID REFERENCES organizations(id),
  template_key TEXT NOT NULL,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(organization_id, template_key)
);

CREATE TABLE prompt_versions (
  id UUID PRIMARY KEY,
  prompt_template_id UUID NOT NULL REFERENCES prompt_templates(id),
  version_no INT NOT NULL,
  content TEXT NOT NULL,
  variables_schema JSONB NOT NULL DEFAULT '{}',
  content_hash TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(prompt_template_id, version_no)
);
```

`artifacts.metadata` 和 `provider_call_logs` 必须记录使用过的 `prompt_version_id`、`prompt_hash`、`model_profile_id`、`provider_model_id`、`credential_id`。


### 8.9 索引与一致性要求

Codex 实现 migrations 时必须补充索引。最低要求：

```text
- 所有 organization_id / project_id 字段建索引。
- 所有 workflow_run_id / node_run_id / provider_call_id 建索引。
- provider_call_logs 按 organization_id + created_at、provider_model_id + created_at 建组合索引。
- artifacts 按 project_id + type + created_at 建组合索引。
- event_outbox 按 status + next_attempt_at 建索引。
- provider_leases 按 provider_model_id + status + expires_at 建索引。
- 需要软删除的表必须有 deleted_at，并在查询层默认过滤。
```

---

## 9. API 设计

### 9.0 固定接口风格

所有公网 API 使用 JSON，所有时间使用 ISO 8601 UTC 字符串，所有主键在 API 中使用 UUID 字符串。内部服务可使用 gRPC，但必须从同一份 OpenAPI / protobuf 契约生成类型。

成功响应 envelope：

```json
{
  "requestId": "req_01HZZ...",
  "data": {},
  "meta": {}
}
```

错误响应 envelope：

```json
{
  "requestId": "req_01HZZ...",
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "请求参数不合法",
    "details": [
      { "field": "baseUrl", "message": "baseUrl is required" }
    ],
    "retryable": false
  }
}
```

分页规范：

```text
- 默认使用 cursor pagination。
- Query 参数：limit、cursor、sort、filter[field]。
- limit 默认 20，最大 100。
- sort 示例：sort=-createdAt 或 sort=name。
- 过滤示例：filter[status]=active&filter[modality]=video。
```

分页响应 meta：

```json
{
  "meta": {
    "pagination": {
      "limit": 20,
      "nextCursor": "eyJjcmVhdGVkQXQiOiIyMDI2...",
      "hasMore": true
    }
  }
}
```

全局 HTTP 状态与错误码：

```text
400 BAD_REQUEST
401 UNAUTHENTICATED
403 FORBIDDEN
404 NOT_FOUND
409 CONFLICT
422 VALIDATION_FAILED
429 RATE_LIMITED
500 INTERNAL_ERROR
502 UPSTREAM_ERROR
503 SERVICE_UNAVAILABLE
504 UPSTREAM_TIMEOUT
```

业务错误码必须使用稳定字符串，不得直接把数据库错误或上游错误暴露给前端。

### 9.1 Auth

```http
POST /api/auth/register
POST /api/auth/login
POST /api/auth/refresh
POST /api/auth/logout
GET  /api/auth/me
```

`POST /api/auth/login` request：

```json
{
  "email": "user@example.com",
  "password": "password"
}
```

response：

```json
{
  "requestId": "req_001",
  "data": {
    "accessToken": "eyJ...",
    "expiresIn": 7200,
    "refreshToken": "rt_...",
    "user": {
      "id": "00000000-0000-0000-0000-000000000001",
      "email": "user@example.com",
      "displayName": "User"
    }
  }
}
```

Access Token 短期有效。Refresh Token 轮换。密码使用 Argon2id 或 bcrypt。

### 9.2 Organization / Workspace

```http
GET  /api/organizations
POST /api/organizations
GET  /api/organizations/{organizationId}
POST /api/organizations/{organizationId}/members
DELETE /api/organizations/{organizationId}/members/{userId}
GET  /api/organizations/{organizationId}/role-bindings
POST /api/organizations/{organizationId}/role-bindings
DELETE /api/organizations/{organizationId}/role-bindings/{bindingId}

GET  /api/workspaces
POST /api/workspaces
GET  /api/workspaces/{workspaceId}
```

`POST /api/organizations/{organizationId}/role-bindings` request：

```json
{
  "subjectType": "user",
  "subjectUserId": "00000000-0000-0000-0000-000000000002",
  "roleId": "00000000-0000-0000-0000-000000000003",
  "resourceType": "project",
  "resourceProjectId": "00000000-0000-0000-0000-000000000004"
}
```

### 9.3 Projects

```http
GET    /api/projects?filter[workspaceId]={workspaceId}&limit=20&cursor={cursor}
POST   /api/projects
GET    /api/projects/{projectId}
PATCH  /api/projects/{projectId}
DELETE /api/projects/{projectId}
GET    /api/projects/{projectId}/members
POST   /api/projects/{projectId}/members
```

`POST /api/projects` request：

```json
{
  "workspaceId": "00000000-0000-0000-0000-000000000010",
  "name": "短剧项目 A",
  "projectType": "short_drama",
  "aspectRatio": "9:16",
  "settings": {
    "language": "zh-CN"
  }
}
```

response：

```json
{
  "requestId": "req_002",
  "data": {
    "id": "00000000-0000-0000-0000-000000000011",
    "organizationId": "00000000-0000-0000-0000-000000000001",
    "workspaceId": "00000000-0000-0000-0000-000000000010",
    "name": "短剧项目 A",
    "aspectRatio": "9:16",
    "createdAt": "2026-01-01T00:00:00Z"
  }
}
```

### 9.4 Assets

```http
GET    /api/projects/{projectId}/assets?filter[type]=character&limit=20&cursor={cursor}
POST   /api/projects/{projectId}/assets
GET    /api/projects/{projectId}/assets/{assetId}
PATCH  /api/projects/{projectId}/assets/{assetId}
DELETE /api/projects/{projectId}/assets/{assetId}
POST   /api/projects/{projectId}/assets/upload-url
POST   /api/projects/{projectId}/assets/{assetId}/variants
```

### 9.5 Workflows

```http
GET    /api/workflow-templates
POST   /api/projects/{projectId}/workflow-runs
GET    /api/projects/{projectId}/workflow-runs?filter[status]=running&limit=20&cursor={cursor}
GET    /api/projects/{projectId}/workflow-runs/{runId}
POST   /api/projects/{projectId}/workflow-runs/{runId}/cancel
POST   /api/projects/{projectId}/workflow-runs/{runId}/retry
GET    /api/projects/{projectId}/workflow-runs/{runId}/nodes
GET    /api/projects/{projectId}/workflow-runs/{runId}/events
```

`POST /api/projects/{projectId}/workflow-runs` request：

```json
{
  "templateKey": "novel_to_video_v1",
  "idempotencyKey": "project-001-run-001",
  "inputs": {
    "novelAssetId": "00000000-0000-0000-0000-000000000020",
    "targetDuration": 60,
    "modelProfiles": {
      "script": "script_agent_default",
      "image": "image_generation_default",
      "video": "video_generation_default"
    }
  }
}
```

response：

```json
{
  "requestId": "req_003",
  "data": {
    "id": "00000000-0000-0000-0000-000000000030",
    "status": "queued",
    "templateKey": "novel_to_video_v1",
    "createdAt": "2026-01-01T00:00:00Z"
  }
}
```

### 9.6 Provider Center

```http
GET    /api/providers/connectors
POST   /api/providers/connectors/import
GET    /api/providers/accounts?filter[status]=active&limit=20&cursor={cursor}
POST   /api/providers/accounts
GET    /api/providers/accounts/{accountId}
PATCH  /api/providers/accounts/{accountId}
DELETE /api/providers/accounts/{accountId}
POST   /api/providers/accounts/{accountId}/test-connection
POST   /api/providers/accounts/{accountId}/credentials/rotate
GET    /api/providers/accounts/{accountId}/health
POST   /api/providers/accounts/{accountId}/discover-models
GET    /api/providers/accounts/{accountId}/models
POST   /api/providers/accounts/{accountId}/models
PATCH  /api/providers/models/{modelId}
POST   /api/providers/models/{modelId}/test
POST   /api/providers/manifests/validate
POST   /api/providers/manifests/test-run
GET    /api/model-profiles
POST   /api/model-profiles
PATCH  /api/model-profiles/{profileId}
POST   /api/model-profiles/{profileId}/bindings
DELETE /api/model-profiles/{profileId}/bindings/{bindingId}
GET    /api/provider-call-logs?filter[projectId]={projectId}&filter[status]=failed&limit=20&cursor={cursor}
GET    /api/provider-usage/summary
```

`POST /api/providers/accounts` request for New API / OpenAI-compatible：

```json
{
  "connectorKey": "openai_compatible",
  "name": "New API Production",
  "baseUrl": "https://newapi.example.com/v1",
  "authType": "bearer",
  "credential": {
    "apiKey": "sk-xxxx"
  },
  "config": {
    "modelsEndpoint": "/models",
    "chatCompletionsEndpoint": "/chat/completions"
  }
}
```

response：

```json
{
  "requestId": "req_004",
  "data": {
    "id": "00000000-0000-0000-0000-000000000040",
    "connectorKey": "openai_compatible",
    "name": "New API Production",
    "baseUrl": "https://newapi.example.com/v1",
    "authType": "bearer",
    "status": "active",
    "credentialPreview": "sk-****abcd"
  }
}
```

`POST /api/providers/accounts/{accountId}/discover-models` response：

```json
{
  "requestId": "req_005",
  "data": {
    "models": [
      {
        "modelKey": "gpt-4o-mini",
        "displayName": "gpt-4o-mini",
        "modality": "text",
        "status": "active"
      }
    ],
    "unsupported": []
  }
}
```

`POST /api/providers/models/{modelId}/test` request：

```json
{
  "testType": "text_generation_test",
  "input": {
    "prompt": "ping"
  }
}
```

response：

```json
{
  "requestId": "req_006",
  "data": {
    "testRunId": "00000000-0000-0000-0000-000000000050",
    "status": "succeeded",
    "latencyMs": 823,
    "normalizedOutput": {
      "text": "pong"
    }
  }
}
```

### 9.7 内部 Provider Gateway API

内部 Provider Gateway API 不暴露公网，只允许 Worker / API Server 通过 service identity 调用。

```http
POST /internal/provider/text/generate
POST /internal/provider/text/stream
POST /internal/provider/image/generate
POST /internal/provider/video/create-task
GET  /internal/provider/tasks/{providerCallId}
POST /internal/provider/tasks/{providerCallId}/cancel
POST /internal/provider/audio/tts
```

所有内部请求必须包含：

```json
{
  "organizationId": "00000000-0000-0000-0000-000000000001",
  "projectId": "00000000-0000-0000-0000-000000000011",
  "workflowRunId": "00000000-0000-0000-0000-000000000030",
  "nodeRunId": "00000000-0000-0000-0000-000000000031",
  "modelProfileKey": "script_agent_default",
  "promptVersionId": "00000000-0000-0000-0000-000000000060",
  "idempotencyKey": "node-031-attempt-1",
  "input": {}
}
```

---

## 10. Realtime 事件设计

使用 SSE 优先，WebSocket 可选。

Endpoint:

```http
GET /api/realtime/events?projectId={projectId}
```

事件类型：

```text
workflow.run.started
workflow.run.completed
workflow.run.failed
workflow.node.started
workflow.node.completed
workflow.node.failed
artifact.created
provider.call.started
provider.call.completed
provider.call.failed
cost.recorded
queue.updated
```

事件结构：

```json
{
  "id": "evt_001",
  "type": "workflow.node.completed",
  "organizationId": "org_001",
  "projectId": "proj_001",
  "workflowRunId": "wf_001",
  "nodeRunId": "node_001",
  "payload": {},
  "createdAt": "2026-01-01T00:00:00Z"
}
```

---

## 11. 前端规格

### 11.1 页面结构

```text
Dashboard
  - 今日生成量
  - 队列压力
  - 失败率
  - 成本
  - Provider 健康度

Project Studio
  - 项目设置
  - 风格设定
  - 角色设定
  - 世界观设定

Novel / Script Studio
  - 小说章节
  - 事件图谱
  - 剧本版本
  - AI 修改历史

Storyboard Studio
  - 分镜表格
  - 镜头卡片
  - 角色/场景/道具绑定
  - 镜头节奏检查

Workflow Board
  - 可视化 DAG
  - 每个节点状态
  - 中间产物预览
  - 失败重试
  - 人工审核

Asset Library
  - 角色
  - 场景
  - 道具
  - 图片
  - 视频
  - 音频

Provider Center
  - 我的供应商
  - 添加供应商
  - 模型列表
  - 能力配置
  - 测试中心
  - 用量统计
  - 错误日志
  - 模型绑定

Admin
  - 用户
  - 团队
  - 权限
  - 审计
  - 系统健康
```

### 11.2 Provider Center 用户流程

```text
1. 点击“添加供应商”。
2. 选择类型：官方供应商 / OpenAI-compatible / 自定义 HTTP / 外部 Connector。
3. 填写连接信息。
4. 输入 API Key，前端只提交，不再展示完整 Key。
5. 发现模型或手动添加模型。
6. 配置模型能力。
7. 运行连接测试、鉴权测试、生成测试。
8. 绑定模型到 Model Profile。
9. 在项目工作流中使用该 Profile。
```

### 11.3 Provider Manifest 编辑器

前端应提供 Manifest 编辑器：

```text
- Monaco Editor
- YAML / JSON 切换
- JSON Schema 实时校验
- 可视化表单与代码双向编辑
- Test Run 面板
- 导入 / 导出
```

---

## 12. Worker 规格

### 12.1 Script Worker

职责：

```text
- 小说清洗
- 事件提取
- 剧本生成
- 分镜脚本生成
- Prompt 编译
- Prompt 版本记录
```

### 12.2 Image Worker

职责：

```text
- 编排图片生成 / 图生图 Workflow Activity。
- 对输入参考图进行业务级选择、裁剪策略、压缩策略计算。
- 调用 Provider Gateway image generate / image edit。
- 根据 Provider Gateway 返回的 artifactId / mediaFileId 更新 workflow_node_runs。
- 处理 Temporal 层面的失败重试、超时、取消和补偿。
```

禁止事项：

```text
- 不得直接调用供应商 API。
- 不得直接读取 provider_credentials。
- 不得下载上游临时图片 URL。
- 不得直接把供应商图片结果转存 S3 / MinIO。
- 不得直接写 provider_call_logs；日志由 Provider Gateway 写入。
```

### 12.3 Video Worker

职责：

```text
- 编排视频生成 Workflow Activity。
- 调用 Provider Gateway create video task。
- 根据 Provider Gateway 返回的 pollAfterMs 使用 Temporal durable timer 等待。
- 调用 Provider Gateway poll / cancel。
- 根据 Provider Gateway 返回的 artifactId / mediaFileId 更新 workflow_node_runs。
- 处理 Temporal 层面的失败重试、超时、取消和补偿。
```

禁止事项：

```text
- 不得直接调用供应商 API。
- 不得直接读取 provider_credentials。
- 不得下载上游临时视频 URL。
- 不得直接把视频转存 S3 / MinIO。
- 不得直接写 provider_call_logs；日志由 Provider Gateway 写入。
```

### 12.4 Audio Worker

职责：

```text
- 编排 TTS / 音频生成 Workflow Activity。
- 调用 Provider Gateway audio tts。
- 根据 Provider Gateway 返回的 artifactId / mediaFileId 更新 workflow_node_runs。
- 处理 Temporal 层面的失败重试、超时、取消和补偿。
```

禁止事项：

```text
- 不得直接调用供应商 API。
- 不得直接读取 provider_credentials。
- 不得下载上游临时音频 URL。
- 不得直接把供应商音频结果转存 S3 / MinIO。
- 不得直接写 provider_call_logs；日志由 Provider Gateway 写入。
```

### 12.5 Media Worker

职责：

```text
- FFmpeg 合成
- 视频抽帧
- 音频合成
- 转码
- 封面生成
- 最终导出
- 写入由 CineWeave 自身生成的非供应商媒体 Artifact，例如合成视频、转码文件、封面图
```

### 12.6 Quality Worker

职责：

```text
- 黑屏检测
- 时长检测
- 分辨率检测
- 参考图误入检测
- 字幕/水印/文字检测
- 音画同步检测
- 角色一致性评分
```


---

## 13. 安全要求

### 13.1 密钥安全

```text
- API Key 不进入前端持久化状态。
- API Key 不写入普通日志。
- 数据库只保存 encrypted_payload 或 secret_ref。
- 生产环境优先使用 KMS / Vault。
- 开发环境使用 master key + AES-GCM。
```

### 13.2 SSRF 防护

自定义 HTTP Provider 必须限制目标地址：

```text
- 默认禁止 localhost
- 默认禁止 127.0.0.1
- 默认禁止私有网段
- 默认禁止 metadata IP
- 默认禁止 file:// 等非 http/https 协议
- 企业私有部署可由管理员显式允许私有网段
```

### 13.3 日志脱敏

必须脱敏字段：

```text
Authorization
api_key
access_token
refresh_token
secret
password
cookie
set-cookie
```

### 13.4 权限

Provider 相关权限：

```text
provider.read
provider.create
provider.update
provider.delete
provider.test
provider.credentials.rotate
provider.models.manage
model_profiles.manage
```

Workflow 相关权限：

```text
workflow.run
workflow.cancel
workflow.retry
workflow.read
workflow.audit
```


### 13.5 服务间安全

内部服务调用必须区分 public API 和 internal API。

```text
- Public API 使用用户 access token。
- Internal API 使用 mTLS 或 service token。
- Provider Gateway internal endpoints 不允许公网直接访问。
- Worker 调用 Provider Gateway 必须携带 service identity。
- Webhook endpoint 是唯一允许外部供应商调用的 Provider 入口，并必须校验签名或 secret。
```

### 13.6 密钥轮换

Provider Credential 必须支持轮换：

```text
- 创建新 credential 后先 test。
- test 通过后切换 active credential。
- 旧 credential 标记 rotated_at，不立即硬删除。
- 所有 provider_call_logs 只记录 credential_id，不记录密钥内容。
```

---

## 14. 可观测性

### 14.1 Trace

必须打通以下 trace：

```text
API request
Workflow run
Workflow node
Provider call
S3 upload/download
Worker activity
FFmpeg job
```

### 14.2 Metrics

必须采集：

```text
http_request_duration_ms
workflow_run_count
workflow_node_duration_ms
provider_call_duration_ms
provider_call_error_count
provider_rate_limited_count
provider_lease_active_count
worker_activity_duration_ms
artifact_created_count
storage_upload_bytes
cost_estimated_total
```

### 14.3 Logs

日志要求：

```text
- JSON structured logs
- 带 request_id / workflow_run_id / node_run_id / provider_call_id
- 默认不记录完整 prompt，可配置采样
- 密钥必须脱敏
```

---

## 15. 本地开发 Docker Compose

必须提供本地开发环境：

```text
postgres
redis
minio
temporal
nats
api
realtime
provider-gateway
workers
web
```

示例只展示基础设施。Codex 必须继续补齐 api / realtime / provider-gateway / worker / web 服务，并可用 Compose profiles 区分 `infra` 与 `full`。

```text
Required profiles:
  infra: postgres / redis / minio / temporal / nats
  app: api / realtime / provider-gateway / workers / web
  full: infra + app
```

示例：

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: cineweave
      POSTGRES_USER: cineweave
      POSTGRES_PASSWORD: cineweave_dev_password
    ports:
      - "5432:5432"

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  minio:
    image: minio/minio
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: minio
      MINIO_ROOT_PASSWORD: minio123
    ports:
      - "9000:9000"
      - "9001:9001"

  nats:
    image: nats:2
    command: ["-js"]
    ports:
      - "4222:4222"

  temporal:
    image: temporalio/auto-setup
    ports:
      - "7233:7233"
```

---

## 16. 首个真实 Provider 验收目标

第一版真实 Provider 以 **New API** 为主要验收目标，但实现必须抽象为 OpenAI-compatible Adapter，不把 New API 写死在业务层。

```text
Primary target: New API
Compatibility targets: One API, LiteLLM, OpenAI official
Required endpoints: /v1/models, /v1/chat/completions
Required capabilities: text.generate, text.stream
Optional first-release capabilities: image.generate, embeddings
Not required in first Provider: video generation
```

验收环境建议：

```text
1. 一个 New API 实例或 New API 兼容测试服务。
2. 一个 LiteLLM 本地代理作为兼容性回归。
3. OpenAI 官方 API 作为协议基准。
4. Mock server 覆盖 401 / 429 / 500 / timeout / malformed JSON。
```

第一版 Provider 测试必须通过：

```text
- connection_test
- auth_test
- model_discovery_test
- text_generation_test
- streaming_test
- error_normalization_test
- provider_call_logs 字段完整性测试
- credential_id / model_profile_id / prompt_version_id 追溯测试
```

## 17. 测试策略

### 17.1 单元测试

必须覆盖：

```text
- Provider Manifest schema validation
- Request template rendering
- JSONPath response mapping
- Error normalization
- Model capability validation
- Model routing
- Rate limit lease
- Credential encryption / masking
```

### 17.2 集成测试

必须覆盖：

```text
- OpenAI-compatible Provider 测试
- Declarative HTTP Provider mock server 测试
- Async polling Provider mock server 测试
- Provider lease 并发测试
- Workflow end-to-end 测试
- S3 artifact 写入测试
```

### 17.3 E2E 测试

必须覆盖：

```text
1. 用户登录。
2. 创建组织和项目。
3. 添加 New API / OpenAI-compatible 供应商。
4. 添加自定义 HTTP 图片供应商。
5. 发现或手动添加模型。
6. 运行模型测试。
7. 绑定模型 Profile。
8. 创建工作流。
9. 查看工作流执行进度。
10. 查看 Artifact 和成本记录。
```

---

## 18. Codex 执行计划

### Phase 0：创建新仓库骨架

任务：

```text
1. 在仓库根目录创建 apps / services / workers / packages / internal / db / deploy / docs / scripts 等目录结构。
2. 初始化 Go workspace。
3. 初始化 Next.js web app。
4. 添加 docker-compose 本地基础设施。
5. 添加 Makefile 或 Taskfile。
6. 添加基础 CI。
```

验收标准：

```text
- docker compose up 能启动 postgres / redis / minio / temporal / nats。
- go test ./... 可运行。
- web 能 pnpm dev 启动。
- repo 有 docs/architecture.md。
```

### Phase 1：认证、多租户、项目

任务：

```text
1. 实现 users / organizations / workspaces / projects schema。
2. 实现 migrations。
3. 实现 register / login / refresh / logout / me。
4. 实现 organization / workspace / project CRUD。
5. 实现 RBAC middleware。
```

验收标准：

```text
- 用户能注册登录。
- 用户能创建组织、工作区、项目。
- 未授权请求返回 401。
- 无权限请求返回 403。
- 所有接口有 OpenAPI 定义。
```

### Phase 2：Provider Gateway 核心

任务：

```text
1. 创建 provider_connectors / provider_accounts / credentials / models / capabilities 表。
2. 实现 Credential Vault。
3. 实现 Provider Account CRUD。
4. 实现 Model CRUD。
5. 实现 Model Capability CRUD。
6. 实现 Provider Call Logs。
7. 实现标准错误码。
```

验收标准：

```text
- 能创建 Provider Account。
- API Key 加密存储，前端不可读完整密钥。
- 能添加模型和能力。
- 能记录 provider_call_logs。
```

### Phase 3：New API / OpenAI-compatible Provider

任务：

```text
1. 实现 New API 优先验收的 OpenAI-compatible connector。
2. 支持 chat completions。
3. 支持 model discovery optional。
4. 支持 text generation test。
5. 支持 model profile binding。
```

验收标准：

```text
- 用户可通过 Base URL + API Key 添加 New API / One API / LiteLLM / OpenAI 官方兼容供应商。
- 能测试文本生成。
- 能绑定到 script_agent_default。
```

### Phase 4：Declarative HTTP Provider

任务：

```text
1. 定义 Provider Manifest JSON Schema。
2. 实现 YAML / JSON import。
3. 实现 request template renderer。
4. 实现 response JSONPath mapper。
5. 实现 sync endpoint。
6. 实现 async polling endpoint。
7. 实现 Provider Test Runner。
```

验收标准：

```text
- 能导入 Manifest。
- Manifest schema 校验失败时返回明确错误。
- 能调用 mock image provider。
- 能调用 mock async video provider 并轮询结果。
- 所有调用均产生 provider_call_logs。
```

### Phase 5：Workflow + Artifact

任务：

```text
1. 接入 Temporal Go SDK。
2. 创建 workflow_runs / workflow_node_runs / artifacts 表。
3. 实现基础 TextToStoryboardWorkflow。
4. 实现 Provider Activity。
5. 实现 Artifact 写入 S3 / MinIO。
6. 实现 Realtime 事件推送。
```

验收标准：

```text
- 能从 API 创建 WorkflowRun。
- Temporal 中能执行 Workflow。
- 节点状态能写入 DB。
- Artifact 能写入 MinIO。
- 前端能看到实时状态。
```

### Phase 6：前端 Provider Center

任务：

```text
1. 实现 Provider Center 页面。
2. 实现添加供应商向导。
3. 实现模型列表与能力编辑。
4. 实现测试中心。
5. 实现 Model Profile 绑定页面。
6. 实现调用日志和用量统计页面。
```

验收标准：

```text
- 用户能通过 UI 添加供应商。
- 用户能通过 UI 导入 Manifest。
- 用户能测试模型。
- 用户能绑定模型用途。
- 用户能查看错误日志和成本。
```

### Phase 7：视频生产 MVP

任务：

```text
1. 实现 ScriptToStoryboardWorkflow。
2. 实现 StoryboardToImageWorkflow。
3. 实现 StoryboardToVideoWorkflow。
4. 实现 VideoComposeWorkflow。
5. 实现基础 QualityCheckActivity。
6. 实现 Project Studio / Workflow Board MVP。
```

验收标准：

```text
- 输入文本可生成分镜。
- 分镜可生成图片。
- 图片可生成视频。
- 视频片段可合成最终视频。
- 全流程可在 Workflow Board 查看。
- 任一节点失败可 retry。
```

---

## 19. 编码规范

### 19.1 Go

```text
- 所有 handler 必须接受 context.Context。
- 所有 DB 查询必须有 organization scope。
- 所有外部 HTTP 请求必须有 timeout。
- 所有错误必须转换为 domain error。
- 不允许 panic 作为业务控制流。
- 所有 API response 使用统一 envelope。
```

统一响应：

```json
{
  "requestId": "req_001",
  "data": {},
  "meta": {}
}
```

错误响应：

```json
{
  "requestId": "req_001",
  "error": {
    "code": "UNAUTHORIZED",
    "message": "未登录或登录已过期",
    "details": {},
    "retryable": false
  }
}
```

### 19.2 TypeScript / Frontend

```text
- 所有 API 类型从 OpenAPI 生成。
- 不手写重复 DTO。
- Server State 使用 TanStack Query。
- Local UI State 使用 Zustand。
- 表单使用 react-hook-form + zod。
- Provider Manifest 编辑器必须实时 schema validation。
```

---

## 20. Definition of Done

一个模块完成必须满足：

```text
1. 有数据库 migration。
2. 有 API spec。
3. 有后端实现。
4. 有单元测试。
5. 有集成测试或 mock 测试。
6. 有前端入口。
7. 有错误处理。
8. 有日志与 trace。
9. 有权限校验。
10. 有文档。
```

Provider 相关模块额外要求：

```text
1. 密钥脱敏。
2. 支持测试运行。
3. 支持错误归一化。
4. 支持调用日志。
5. 支持成本估算。
6. 支持限流配置。
```

Workflow 相关模块额外要求：

```text
1. Activity 幂等。
2. 支持 retry。
3. 支持 cancel。
4. 支持状态持久化。
5. 支持实时事件。
6. 支持 Artifact 输出。
```

---

## 21. 给 Codex 的最高优先级指令

Codex 在执行时必须遵守以下优先级：

```text
1. 先搭平台骨架，不要先写复杂业务 UI。
2. 先实现 Provider Gateway，再接具体业务生成。
3. 所有 AI 调用必须通过 Provider Gateway。
4. 不实现旧 TS 供应商脚本兼容层。
5. 所有长任务必须走 Temporal。
6. 所有媒体文件必须走 S3 / MinIO Artifact。
7. 所有业务数据必须带 organization_id；项目级数据必须带 project_id，workspace 从 project 派生，确需高频过滤时可冗余 workspace_id。
8. 所有接口必须有权限校验。
9. 所有供应商密钥必须加密存储。
10. 所有 Provider 调用必须写 provider_call_logs。
```

---

## 22. 第一批 Codex 任务清单

建议直接把以下任务按顺序交给 Codex：

```text
Task 001: Initialize cineweave monorepo structure.
Task 002: Add docker-compose for postgres, redis, minio, temporal, nats.
Task 003: Implement Go API skeleton with health check and config loader.
Task 004: Add PostgreSQL migrations for users, orgs, workspaces, projects.
Task 005: Implement auth register/login/refresh/logout/me.
Task 006: Implement RBAC middleware and organization/workspace/project context.
Task 007: Add provider schema migrations.
Task 008: Implement Credential Vault encryption.
Task 009: Implement Provider Account CRUD.
Task 010: Implement Provider Model and Capability CRUD.
Task 011: Implement New API-first OpenAI-compatible text provider.

## Implementation Note: Provider Gateway Image Runtime v1

- `POST /internal/provider/image/generate` is implemented as an internal service-token API.
- The first adapter target is OpenAI-compatible `/v1/images/generations`, including New API / One API / LiteLLM / OpenAI official style responses.
- The Gateway accepts upstream `url` and `b64_json` image results, then downloads or decodes media inside Provider Gateway.
- The Gateway writes generated image objects to S3 / MinIO and records `media_files`, `artifacts`, `provider_call_logs`, and `cost_records`.
- API Server / Worker code must call Provider Gateway and must not call image providers, download upstream media, write provider call logs, or write cost records directly.
- `CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=false` is the default; set it to `true` only for local mock provider media URLs.

## Implementation Note: Provider Gateway Video Runtime v1

- Provider Gateway now owns asynchronous video task runtime through `/internal/provider/video/create-task`, `/internal/provider/video/poll-task`, and `/internal/provider/video/cancel-task`.
- Video providers are integrated first through declarative HTTP Provider Manifests, not hardcoded vendor adapters.
- Manifest video templates can use `input`, `references`, `credential`, `endpoint`, `model`, `account`, and `task`; for example `{{ references[0].url }}` and `{{ task.externalTaskId }}`.
- `provider_async_tasks` is the durable async task state source. Provider Gateway does not keep long-running video task state in memory.
- `create-task` writes `provider_call_logs` with `task_type=video.create_task` and `execution_mode=async_create`, then writes `provider_async_tasks`.
- `poll-task` writes `provider_call_logs` with `task_type=video.poll_task` and `execution_mode=async_poll`. Running polls update `provider_async_tasks` only; succeeded polls download video media, store it in S3 / MinIO, write `media_files`, `generated_video` artifacts, and final `cost_records`.
- `cancel-task` calls a manifest cancel endpoint when configured; otherwise it marks the local async task cancelled.
- Private video media URLs are blocked by default. Development can set `CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=true`.
- Video download size defaults to `CINEWEAVE_PROVIDER_VIDEO_MAX_BYTES=536870912`.
- `video_generation_test` executes via Provider Gateway create/poll and returns `providerAsyncTaskId`; completed tests also return `artifactId`, `mediaFileId`, and `storageKey`.

## Implementation Note: Temporal Workflow to Provider Gateway Runtime

- `POST /api/workflow-runs` with `workflowType=text_to_storyboard` starts the real Temporal `TextToStoryboardWorkflow` on the script task queue.
- The script worker constructs `GatewayClient` from `PROVIDER_GATEWAY_URL` and `CINEWEAVE_SERVICE_TOKEN`; Worker and API code must not call upstream model providers directly.
- `GenerateStoryboardText` creates a `generate_storyboard_text` node run, verifies an active `script_agent_default` binding, calls `/internal/provider/text/generate`, stores a `storyboard_json` artifact, and emits `workflow.node.started`, `artifact.created`, and `workflow.node.completed`.
- `GenerateStoryboardImage` creates a `generate_storyboard_image` node run, verifies an active `image_generation_default` binding, calls `/internal/provider/image/generate`, and uses the Gateway-returned `artifactId`, `mediaFileId`, `storageKey`, and `providerCallId`. The worker does not download media and does not write `provider_call_logs` or `cost_records`.
- On success, `workflow_runs.output` contains `storyboardArtifactId`, `imageArtifactId`, `imageMediaFileId`, `imageStorageKey`, and `providerCalls.storyboard/image`; the run emits `workflow.run.completed`.
- If either profile has no active binding, the activity fails with `MODEL_PROFILE_NOT_CONFIGURED` so operators know to bind `script_agent_default` or `image_generation_default`.
- Local verification commands:
  - `go test ./...`
  - `CINEWEAVE_INTEGRATION_TEST=1 go test ./internal/workflows -run TestWorkflowGatewayIntegration -count=1`
  - `pnpm --filter @cineweave/web typecheck`
  - `pnpm --filter @cineweave/web lint`
  - `docker compose -f compose.yml config --quiet`
  - `docker compose -f compose.yml build api provider-gateway script-worker`

## Implementation Note: Temporal to Provider Gateway Video Production v1

- `POST /api/workflow-runs` with `workflowType=video_production` now runs the minimum real chain: Provider Gateway `text.generate` creates `storyboard_json`, Provider Gateway `image.generate` creates `generated_image`, and Provider Gateway `video.create-task` / `video.poll-task` creates `generated_video`.
- The script worker only calls `provider.GatewayClient`; it does not decrypt provider credentials, call upstream providers directly, download upstream video media, or write `provider_call_logs` / `cost_records`.
- `CreateStoryboardVideoTask` writes the `generate_storyboard_video` node and calls `/internal/provider/video/create-task` with `modelProfileKey=video_generation_default`, `mode=image_to_video`, a stable idempotency key, and the generated image reference.
- `PollStoryboardVideoTask` performs one Gateway poll per activity execution. The durable loop lives in `VideoProductionWorkflow` and uses `workflow.Sleep`, default `pollIntervalSeconds=5`, and default `maxPolls=120`.
- On success, `workflow_runs.output` contains `storyboardArtifactId`, `imageArtifactId`, `imageMediaFileId`, `imageStorageKey`, `videoArtifactId`, `videoMediaFileId`, `videoStorageKey`, `providerAsyncTaskId`, `externalTaskId`, and `providerCalls.storyboard/image/videoCreate/videoPoll`.
- Configure active model profile bindings for `script_agent_default`, `image_generation_default`, and `video_generation_default` before running `video_production`.
- Local verification command:
  - `CINEWEAVE_INTEGRATION_TEST=1 go test ./internal/workflows -run TestVideoProductionWorkflowGatewayIntegration -count=1`
Task 012: Implement provider call logging.
Task 013: Implement model profile and binding.
Task 014: Implement Manifest JSON Schema.
Task 015: Implement Declarative HTTP Provider renderer and JSONPath mapper.
Task 016: Implement Provider Test Runner.
Task 017: Add Temporal worker skeleton.
Task 018: Add workflow_runs, node_runs, artifacts schema.
Task 019: Implement simple TextGenerationWorkflow.
Task 020: Implement Next.js Provider Center MVP.
Task 021: Add workflow_templates and workflow_template_nodes schema.
Task 022: Add event_outbox publisher to NATS JetStream.
Task 023: Add provider webhook ingress and Temporal Signal handling.
Task 024: Add novels, scripts, storyboard_shots, assets, media_files schema.
Task 025: Add prompt_templates and prompt_versions schema.
Task 026: Add idempotency_keys support for write APIs and Provider calls.
```

---

## 23. 最终目标状态

当本技术规格完成后，影织 / CineWeave 应达到：

```text
- 用户可以通过 UI 无代码接入 API 供应商。
- 高级用户可以导入 Provider Manifest。
- 企业用户可以接入外部 Connector Service。
- 所有模型有能力描述。
- 所有业务绑定 Model Profile，而不是直接绑定供应商模型。
- 所有 AI 调用可追踪、可计费、可限流、可降级。
- 所有长流程可恢复、可重试、可取消。
- 所有产物作为 Artifact 存储和版本化。
- 前端能实时展示工作流状态和失败原因。
- 平台可本地 Docker Compose 开发，也可 Kubernetes 生产部署。
```
