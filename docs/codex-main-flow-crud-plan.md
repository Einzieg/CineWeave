# CineWeave 主流程简化与产物 CRUD 开发计划

本文档用于交给 Codex 执行，目标是在当前 CineWeave MVP 的最后修缮阶段，将产品流程收敛为创作者可理解的主线，并确保每一步产生的业务内容都支持查看、修改、删除、重生成和溯源。

## 0. 总目标

将用户可见流程简化为：

```text
新建项目
→ 上传 / 添加：小说原文、剧本、创意文案
→ 生成剧本
→ 提取资产
→ 生成资产
→ 根据剧本生成衍生资产与分镜表
→ 生成视频
```

核心原则：

1. 用户主流程只暴露“内容、剧本、资产、分镜、视频、成片”。
2. CRUD 围绕被生成的创作内容，不围绕 workflow、provider task、artifact log 等技术记录。
3. 所有 AI 调用继续走 Provider Gateway，不允许 API Server 或 Worker 直接调用上游模型。
4. Toonflow 的导演手册、视觉手册、资产提示词策略可以迁移，但要落入 CineWeave 的 Prompt Registry、RBAC、Provider Gateway、Artifact 体系，不迁移本地文件技能目录模式。
5. 业务产物可编辑、可删除或归档；执行记录、成本记录、provider_call_logs、prompt hash、workflow run 默认只读。

---

## 1. 用户可见信息架构

### 1.1 顶层导航调整

项目内主导航收敛为 6 个模块：

```text
项目概览
内容
剧本
资产
分镜
视频
成片
```

可保留但不作为主流程入口的模块：

```text
审阅中心
工作流记录
素材库 / Vault
供应商日志
Prompt 调试
Agent 任务
```

这些模块应放在“高级”或“更多”分组中，避免普通用户误以为它们是创作步骤。

### 1.2 项目概览的流程条

项目首页展示一条固定流程条：

```text
内容 → 剧本 → 资产 → 分镜 → 视频 → 成片
```

每个阶段展示：

- 状态：未开始、进行中、可继续、有问题、已完成。
- 数量摘要。
- 主按钮。
- 最近错误或阻断原因。
- 下一步建议。

阶段状态映射建议：

| 阶段 | 未开始 | 进行中 | 有问题 | 已完成 |
| --- | --- | --- | --- | --- |
| 内容 | 无 source | source.status=processing | source.status=failed | 至少一个 ready source |
| 剧本 | 无 active script | 生成/解析运行中 | 剧本生成失败或场景为空 | 有 active script 且有 version |
| 资产 | 无 canonical assets | 提取/生成运行中 | 资产缺字段或生成失败 | 角色/场景/道具至少有可用项 |
| 分镜 | 无 storyboard shots | 分镜生成运行中 | shots 缺 visual 或 stale/failed | 有 storyboard shots |
| 视频 | 无 shot video | 视频生成运行中 | 有 failed/missing | 所需镜头视频成功 |
| 成片 | 无 final video | compose 运行中 | compose failed | 有 active final video |

---

## 2. 主流程分解

### 2.1 新建项目

用户新建项目时只需要做必要选择：

- 项目名。
- 内容类型，默认可为空，添加内容后自动推断。
- 视频比例，默认 `16:9` 或用户最近使用值。
- 视觉风格。
- 导演手册，默认模板。
- 视觉手册，默认模板。
- 文本 / 图片 / 视频模型，默认绑定到 model profile。

项目创建页不要要求用户理解 Prompt Registry、Provider Account、Model Profile Binding。若缺少模型绑定，在项目概览阶段给出“配置模型”引导。

后端字段继续使用项目上的：

```text
art_style
director_manual
visual_manual
script_model_profile_key
image_model_profile_key
video_model_profile_key
production_mode
```

### 2.2 添加内容

用户入口统一为“添加内容”，支持三类：

```text
小说原文
剧本
创意文案
```

推荐后端枚举：

```text
source_type: novel | script | brief
```

若短期不改 enum，则使用：

```json
{
  "sourceType": "script",
  "metadata": { "inputKind": "brief" }
}
```

但最终应新增 `brief`，避免长期用 metadata 伪装业务类型。

#### 2.2.1 小说原文

小说原文导入时：

- 支持粘贴文本。
- 支持上传 `.txt`、`.md`、`.markdown`。
- 默认拆分章节。
- 持久化 volume/chapter/section ordinal 字段。
- 不在主流程显示“事件提取”按钮，事件提取作为“生成剧本”内部动作或高级选项。

#### 2.2.2 剧本

剧本导入时：

- 直接创建 `scripts`。
- 创建 `script_versions`。
- 默认触发或引导解析场景。
- 不走小说章节拆分逻辑。

#### 2.2.3 创意文案

创意文案导入时：

- 作为轻量 source。
- `生成剧本` 时走 brief-to-script prompt。
- 不展示章节拆分。

### 2.3 生成剧本

统一按钮名：

```text
生成剧本
```

内部按来源类型分流：

| 来源类型 | 内部策略 |
| --- | --- |
| novel | 章节切分 → 可选事件提取 → 改编策略 → 剧本 |
| script | 已有剧本初始化版本与场景；可选择“润色/改写” |
| brief | 创意扩写 → 剧本 |

用户不需要在主流程理解“事件图谱”“改编计划”。这些可进入剧本详情的“生成依据”里。

生成剧本产物：

- Script。
- ScriptVersion。
- ScriptScene。
- 生成来源 metadata。
- provider_call_id / prompt_hash 溯源。

CRUD：

- 查看剧本。
- 编辑剧本，保存为新版本。
- 激活某版本。
- 删除/归档非当前版本。
- 重新解析场景。
- 编辑/删除场景。
- 从某场景继续生成资产或分镜。

### 2.4 提取资产

统一按钮名：

```text
提取资产
```

资产类型统一为：

```text
character / scene / prop
```

如果当前数据库或前端仍使用：

```text
role / scene / tool
```

需要在边界层统一映射：

```text
role ↔ character
tool ↔ prop
scene ↔ scene
```

不要在用户界面展示 `role`、`tool` 等内部旧名。

#### 2.4.1 提取规则

迁移 Toonflow 资产提取的核心规则：

1. 新资产与已有资产引用分开返回。
2. 新资产需要名称、描述、类型、出现的剧本/场景 ID。
3. 已有资产只返回精确名称与出现位置，不重复创建。
4. 角色资产名称必须是标准人名或稳定称呼，不把服装、阶段、姿态、状态当角色名。
5. 同一角色的不同年龄、服装、战损、阶段合并到同一角色资产描述或衍生资产，不创建多个主角色资产。
6. 道具必须是有独立视觉形态或剧情功能的可见物件。
7. 场景必须是可拍摄地点或空间，不使用抽象概念。
8. 资产出现位置必须可追溯到 script_scene 或 script_version。

### 2.5 生成资产

统一按钮名：

```text
生成资产
```

此阶段生成主资产图：

```text
角色设定图
场景设定图
道具设定图
```

每个资产应有：

- 名称。
- 类型。
- 描述。
- 生成提示词。
- 主图。
- 参考图。
- 状态。
- 出现在哪些场景或镜头。
- 溯源信息。

CRUD：

- 查看资产详情。
- 编辑资产名称、描述、提示词。
- 上传参考图。
- 设置主参考图。
- 删除参考图。
- 删除/归档资产。
- 重新生成资产图。
- 从当前资产生成衍生资产。

### 2.6 生成衍生资产与分镜表

统一按钮名：

```text
生成分镜
```

内部动作：

```text
剧本场景 → 分镜表
分镜表 → 镜头资产需求
镜头资产需求 → 衍生资产
```

用户不需要先理解“衍生资产”。在分镜页中展示每个镜头需要的资产状态：

```text
主角色图已完成
当前镜头角色状态图待生成
场景图已完成
道具图缺失
```

分镜产物：

- StoryboardShot。
- ShotAssetRequirement。
- DerivedAssetImage。
- Shot image prompt。
- Shot video prompt。

CRUD：

- 查看分镜表。
- 编辑镜头描述、景别、动作、对白、运镜、时长、prompt。
- 删除镜头。
- 重排镜头。
- 重新生成镜头图。
- 重新生成镜头视频。
- 查看或编辑镜头资产需求。
- 跳过某个非必要衍生资产。

### 2.7 生成视频

统一按钮名：

```text
生成视频
```

视频页主动作：

- 生成缺失镜头图片。
- 生成缺失镜头视频。
- 重生成失败项。
- 合成成片。
- 取消运行中任务。

用户不需要选择底层 action：

```text
generate_missing_images
regenerate_failed_images
generate_missing_videos
regenerate_failed_videos
cancel_running_videos
```

这些保持在后端和 Agent 工具内部。

视频产物：

- Shot image。
- Shot video。
- Timeline。
- Timeline clip。
- Final video version。
- Export。

CRUD：

- 查看镜头图片和视频。
- 修改镜头 prompt。
- 删除/解绑镜头图片。
- 删除/解绑镜头视频。
- 重新生成镜头图片。
- 重新生成镜头视频。
- 编辑时间线 clip。
- 删除 clip。
- 重排 clip。
- 合成 final video。
- 设置 active final video。
- 删除非当前 final video。
- 下载 final video。

---

## 3. Toonflow 迁移范围

### 3.1 可直接迁移的内容

从 Toonflow(D:\Code\Toonflow) 迁移以下概念和模板内容：

#### 导演手册字段

```text
README
director_planning_narrative
director_storyboard_table_narrative
```

#### 视觉手册字段

```text
README
prefix
art_character
art_character_derivative
art_prop
art_prop_derivative
art_scene
art_scene_derivative
director_storyboard
art_storyboard_video
director_planning_style
director_storyboard_table_style
```

#### 资产提示词构建策略

迁移下列逻辑：

- 角色、场景、道具分流。
- 主资产与衍生资产分流。
- 风格摘要。
- 风格硬约束。
- 历史 prompt 锚点提取。
- 模板片段过滤。
- 角色非人化保护。
- 场景禁止人物。
- 道具禁止手持/人物。
- 分镜图片风格提示词。
- 分镜视频提示词。

### 3.2 不迁移的内容

不要迁移 Toonflow 的以下实现方式：

- 本地 `skills/art_skills` 文件目录作为运行时真实来源。
- 本地 `skills/story_skills` 文件目录作为运行时真实来源。
- Express 路由结构。
- SQLite 表结构。
- 直接调用本地 `u.Ai.Text` 的方式。
- 任何绕过 Provider Gateway 的模型调用。

### 3.3 CineWeave 中的目标实现

推荐新增或复用：

```text
internal/manuals
internal/creative/prompts
internal/assets/prompt_builder.go
internal/storyboard/prompt_builder.go
```

模板存储建议优先用 Prompt Registry：

```text
prompt_templates
prompt_versions
prompt_bindings
```

新增 template key 建议：

```text
director.planning_narrative
director.storyboard_table_narrative
visual.prefix
visual.character
visual.character_derivative
visual.prop
visual.prop_derivative
visual.scene
visual.scene_derivative
visual.storyboard
visual.storyboard_video
visual.director_planning_style
visual.director_storyboard_table_style
asset.prompt.character
asset.prompt.character_derivative
asset.prompt.prop
asset.prompt.prop_derivative
asset.prompt.scene
asset.prompt.scene_derivative
storyboard.image_prompt
storyboard.video_prompt
brief_to_script
novel_to_script
script_rewrite
script_asset_extraction
```

如现有 key 已存在，应优先兼容现有 key，不重复制造近义模板。

### 3.4 安全与默认值

如果迁移 Toonflow 的成人化或风格预设：

1. 默认不启用成人化预设。
2. 仅作为管理员可配置模板。
3. 必须保留年龄、未成年、非人角色、衣着完整、风格冲突保护。
4. 普通用户界面不要暴露敏感细节模板文本。
5. 资产 prompt builder 输出前必须做安全清理和冲突检测。

---

## 4. 后端开发任务

### 4.1 Source 类型与导入收口

涉及文件：

```text
internal/api/sources.go
internal/sources/*
db/migrations/*
packages/openapi/openapi.yaml
apps/web/src/lib/api-client.ts
apps/web/src/lib/types.ts
```

任务：

1. 新增或正式支持 `brief` source type。
2. `novel` 导入默认 split chapters。
3. `script` 导入默认 create script/version，可选自动 parse scenes。
4. `brief` 生成剧本时走 brief-to-script prompt。
5. Source 编辑后标记下游产物 stale：script、events、adaptation plan、assets、storyboard、shot images/videos。
6. Source 删除前返回 impact summary。

新增建议 API：

```text
GET  /api/projects/{projectId}/sources/{sourceId}/impact
POST /api/projects/{projectId}/sources/{sourceId}/archive
```

若继续使用 DELETE，应要求 body 或 query 明确 delete mode：

```text
mode=archive | unlink | hard_delete
```

### 4.2 剧本生成统一入口

涉及文件：

```text
internal/api/scripts.go
internal/workflows/source_to_script.go
internal/workflows/script_driven_storage.go
internal/prompts/*
```

任务：

1. 新增统一业务入口：`POST /api/projects/{projectId}/scripts/generate`。
2. 入参只暴露 sourceId、sourceKind、instruction、options。
3. 内部分流：novel/script/brief。
4. 剧本生成必须输出 script + version。
5. 生成后可自动 parse scenes。
6. 编辑剧本不覆盖当前版本，默认创建新版本。
7. 激活版本后标记下游 stale。

建议响应：

```json
{
  "scriptId": "...",
  "versionId": "...",
  "sceneCount": 12,
  "sourceId": "...",
  "providerCallId": "...",
  "promptHash": "..."
}
```

### 4.3 资产提取收口

涉及文件：

```text
internal/api/assets.go
internal/workflows/script_to_assets.go
internal/workflows/script_driven_storage.go
internal/creative/prompts/*
```

任务：

1. 资产提取结果拆分为 `newAssets` 与 `existingAssetRefs`。
2. 按 script scene 或 script version 建立引用关系。
3. 不因一次模型漏提而删除旧资产引用。
4. 新增资产命名规范校验。
5. 重复资产合并策略：同名同类型合并，不同类型提示人工确认。
6. 提取后资产卡片可编辑、删除、归档、重新提取。

### 4.4 资产图片生成迁移 Toonflow 策略

涉及文件：

```text
internal/assets/prompt_builder.go
internal/api/assets.go
internal/workflows/asset_activities.go
internal/provider/*
```

任务：

1. 新增 AssetPromptBuilder。
2. 支持 character / scene / prop。
3. 支持 main / derivative。
4. 读取项目 visual manual 和 art style。
5. 生成 prompt 后先保存到资产，不立即强制生成图，除非用户选择“一键生成”。
6. 图片生成走 Provider Gateway image.generate。
7. 成功后写 media_files、artifacts，并绑定到 asset 或 requirement。
8. 失败时保存错误 code/message，前端能重试。

### 4.5 衍生资产与分镜合并入口

涉及文件：

```text
internal/api/storyboard.go
internal/api/shot_assets.go
internal/workflows/script_to_storyboard.go
internal/workflows/script_driven_storage.go
```

任务：

1. 用户按钮“生成分镜”触发 script_to_storyboard。
2. workflow 输出 storyboard_shots。
3. 同步生成 shot_asset_requirements。
4. requirement 默认显示在镜头详情中，不作为主流程独立入口。
5. requirement 支持编辑、跳过、删除/归档、生成衍生图。
6. 修改 storyboard shot 后，对相关 shot image/video 标记 stale。

### 4.6 视频生成入口收口

涉及文件：

```text
internal/api/shot_production.go
internal/workflows/batch_generate_shot_images.go
internal/workflows/batch_generate_shot_videos.go
internal/workflows/compose_timeline.go
```

任务：

1. 前端只暴露简化按钮。
2. 后端保留细粒度 action。
3. 生成视频前检查镜头图是否存在。
4. 合成成片前检查镜头视频是否完成。
5. 生成/重生成时保留旧 artifact，成功后切换绑定。
6. 删除镜头图/视频默认 unlink，不物理删除 media file。

### 4.7 产物影响检查

新增统一影响检查服务：

```text
internal/outputs/impact.go
internal/api/output_impact.go
```

建议 API：

```text
GET /api/projects/{projectId}/outputs/{entityType}/{entityId}/impact
```

支持 entityType：

```text
source
script
script_version
script_scene
canonical_asset
asset_reference
storyboard_shot
shot_asset_requirement
shot_image
shot_video
timeline
timeline_clip
final_video_version
review_fix
```

返回：

```json
{
  "entityType": "storyboard_shot",
  "entityId": "...",
  "canDelete": true,
  "recommendedMode": "archive",
  "deleteModes": ["unlink", "archive"],
  "affected": [
    { "entityType": "timeline_clip", "count": 1 },
    { "entityType": "shot_video", "count": 1 }
  ],
  "warnings": ["该镜头已有视频，删除镜头将从当前分镜表移除视频绑定。"]
}
```

### 4.8 Agent 与主流程对齐

当前 Project Agent Runtime 已存在，后续调整：

1. Agent 只调用主流程工具，不再暴露过细内部工具给普通用户。
2. Agent 输出必须关联到主流程阶段。
3. Agent step output 必须尽可能转换成业务产物卡。
4. Agent 仍然必须遵守 RBAC、监督层、state gate、cost gate、workflow active gate。
5. 默认 `permissionMode=require_approval`。
6. `full_access` 只对 project owner 或 admin 可见。

---

## 5. 前端开发任务

### 5.1 项目概览页

新增或改造：

```text
apps/web/src/features/projects/project-overview.tsx
apps/web/src/features/production/production-overview.tsx
```

任务：

1. 展示 6 阶段流程条。
2. 每阶段展示数量、状态、主按钮。
3. 主按钮进入对应页面或触发对应受控动作。
4. 展示阻断原因：缺模型、缺内容、缺图片、缺视频、review high/critical。
5. 不展示内部英文 enum。

### 5.2 内容页

任务：

1. 三种添加入口：小说原文、剧本、创意文案。
2. 同一列表中显示类型标签。
3. 支持查看、编辑、删除/归档。
4. 小说支持章节列表。
5. 剧本导入后显示已创建的 script。
6. 创意文案显示“生成剧本”按钮。

### 5.3 剧本页

任务：

1. 当前剧本阅读器。
2. 版本列表。
3. 新版本编辑器。
4. 场景列表。
5. 场景编辑。
6. 删除/归档版本确认。
7. 激活版本确认。
8. 重新解析场景。

### 5.4 资产页

任务：

1. 角色/场景/道具 tabs 或三列。
2. 资产卡片统一组件。
3. 编辑资产弹窗。
4. prompt 编辑区。
5. 参考图管理。
6. 主图设置。
7. 生成/重生成按钮。
8. 删除/归档影响提示。

### 5.5 分镜页

任务：

1. 分镜表列表。
2. 镜头详情 drawer。
3. 镜头字段编辑。
4. 镜头删除确认。
5. 镜头排序。
6. 镜头资产需求显示与编辑。
7. 衍生资产生成入口。
8. 镜头图/视频状态显示。

### 5.6 视频页

任务：

1. 镜头视频矩阵。
2. 缺失、失败、运行中筛选。
3. 生成缺失图片。
4. 生成缺失视频。
5. 重生成失败项。
6. 取消运行中。
7. 合成成片。
8. 最终视频版本列表。
9. 设置当前版本。
10. 下载。
11. 删除非当前版本。

### 5.7 通用组件

新增通用组件：

```text
OutputCard
OutputDetailDrawer
OutputEditDialog
DeleteImpactDialog
RegenerateButton
ProvenancePanel
StageStatusBadge
ChineseEnumLabel
```

`DeleteImpactDialog` 统一负责：

- 调 impact API。
- 展示影响范围。
- 选择删除模式。
- 执行删除或归档。
- 操作后刷新 React Query keys。

---

## 6. OpenAPI / 类型 / 标签要求

每新增或修改公开 API，必须同步：

```text
packages/openapi/openapi.yaml
apps/web/src/lib/api-client.ts
apps/web/src/lib/types.ts
scripts/check-openapi-routes.py allowlist if needed
```

所有用户可见 label 必须走集中映射：

```text
apps/web/src/lib/labels.ts
```

不得在页面中散落 enum 文案。

必须中文化的枚举：

```text
sourceType
contentFormat
script status
asset type
asset status
storyboard status
image/video status
workflow status
agent task status
agent risk
provider modality
permission key
role key
review severity
review status
```

---

## 7. 测试与验证计划

### 7.1 后端测试

新增或补充测试：

```text
internal/api/sources_test.go
internal/api/scripts_test.go
internal/api/assets_test.go
internal/api/storyboard_shots_test.go
internal/api/shot_production_test.go
internal/api/output_impact_test.go
internal/workflows/script_to_assets_test.go
internal/workflows/script_to_storyboard_test.go
internal/assets/prompt_builder_test.go
```

重点覆盖：

- brief source 导入。
- script source 导入自动建 script/version。
- novel source 章节 ordinal 保留。
- source 编辑后下游 stale 标记。
- asset 提取不重复创建同名资产。
- asset prompt builder 对 character/scene/prop/main/derivative 的输出差异。
- 删除影响检查。
- 分镜删除影响。
- 视频生成前镜头图依赖检查。
- final video 当前版本删除阻断。

### 7.2 前端验证

需要手动或 E2E 验证：

1. 新建项目后进入项目概览。
2. 添加创意文案。
3. 生成剧本。
4. 编辑剧本并保存新版本。
5. 激活新版本。
6. 提取资产。
7. 编辑资产描述。
8. 生成资产图。
9. 生成分镜。
10. 编辑分镜镜头。
11. 删除镜头并确认影响提示。
12. 生成缺失图片。
13. 生成缺失视频。
14. 合成成片。
15. 下载成片。
16. 查看 Agent 任务产物卡。

### 7.3 基础验证命令

每个阶段完成后运行相关子集，最终合并前运行：

```powershell
pnpm run test
```

等价基础检查应覆盖：

```powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
python -c "import pathlib, yaml; yaml.safe_load(pathlib.Path('packages/openapi/openapi.yaml').read_text(encoding='utf-8'))"
python scripts/check-openapi-routes.py
docker compose -f compose.yml config --quiet
```

涉及运行时服务后，验证：

```powershell
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
```

---

## 8. 验收清单

### 8.1 主流程验收

- [ ] 项目首页只展示“内容、剧本、资产、分镜、视频、成片”主流程。
- [ ] 用户可以从项目首页按顺序完成完整链路。
- [ ] 普通用户不需要理解 workflow run、provider task、artifact id、prompt hash 才能操作。
- [ ] 每个阶段都有明确主按钮。
- [ ] 每个阶段都有“缺什么、下一步做什么”的提示。
- [ ] 所有用户可见状态均为中文。

### 8.2 添加内容验收

- [ ] 可添加小说原文。
- [ ] 可上传小说原文文件。
- [ ] 小说原文会持久化章节、卷序、节序、章节序。
- [ ] 可添加剧本。
- [ ] 剧本导入会创建 script 和 script_version。
- [ ] 剧本导入不会被当作小说章节拆分。
- [ ] 可添加创意文案。
- [ ] 创意文案可生成剧本。
- [ ] 原始内容可查看。
- [ ] 原始内容可编辑。
- [ ] 原始内容可删除或归档。
- [ ] 删除前显示影响范围。

### 8.3 剧本验收

- [ ] 小说原文可以生成剧本。
- [ ] 创意文案可以生成剧本。
- [ ] 已有剧本可以改写为新版本。
- [ ] 剧本编辑保存为新版本，不覆盖旧版本。
- [ ] 剧本版本可查看。
- [ ] 剧本版本可激活。
- [ ] 非当前剧本版本可删除或归档。
- [ ] 剧本场景可查看。
- [ ] 剧本场景可编辑。
- [ ] 剧本场景可删除。
- [ ] 重新解析场景可用。
- [ ] 激活新剧本版本后，下游资产/分镜/视频状态被标记 stale 或提示重生成。

### 8.4 资产提取验收

- [ ] 可从剧本提取角色。
- [ ] 可从剧本提取场景。
- [ ] 可从剧本提取道具。
- [ ] 已有资产不会重复创建。
- [ ] 同名同类型资产能复用。
- [ ] 服装、姿态、阶段不会被误提为主角色名称。
- [ ] 资产与剧本场景或剧本版本存在可追溯关系。
- [ ] 提取失败时可查看错误。
- [ ] 可重新提取资产。

### 8.5 资产生成验收

- [ ] 角色资产可生成图片。
- [ ] 场景资产可生成图片。
- [ ] 道具资产可生成图片。
- [ ] 主资产和衍生资产使用不同提示词模板。
- [ ] 资产 prompt 可查看。
- [ ] 资产 prompt 可编辑。
- [ ] 资产图片可查看。
- [ ] 资产图片可删除或解绑。
- [ ] 资产图片可重生成。
- [ ] 资产可上传参考图。
- [ ] 参考图可设为主图。
- [ ] 参考图可删除。
- [ ] 图片生成必须经过 Provider Gateway。
- [ ] provider_call_logs、cost_records、artifacts、media_files 正确写入。

### 8.6 Toonflow 手册迁移验收

- [ ] 已 seed 默认导演手册。
- [ ] 默认导演手册包含 README、导演规划、分镜表设计。
- [ ] 已 seed 默认视觉手册。
- [ ] 默认视觉手册包含角色、角色衍生、场景、场景衍生、道具、道具衍生、分镜、分镜视频相关模板。
- [ ] 手册模板通过 Prompt Registry 版本化。
- [ ] 项目可绑定导演手册。
- [ ] 项目可绑定视觉手册。
- [ ] Prompt 调用记录 promptVersionId 与 promptHash。
- [ ] 未绕过 Provider Gateway。
- [ ] 普通用户不会看到本地 skills 目录概念。

### 8.7 分镜验收

- [ ] 可从剧本生成分镜表。
- [ ] 分镜表可查看。
- [ ] 镜头可查看详情。
- [ ] 镜头可编辑。
- [ ] 镜头可删除。
- [ ] 镜头可重排。
- [ ] 镜头删除前显示影响范围。
- [ ] 镜头资产需求可查看。
- [ ] 镜头资产需求可编辑。
- [ ] 镜头资产需求可跳过或删除。
- [ ] 衍生资产图可生成。
- [ ] 修改镜头后相关镜头图/视频标记 stale。

### 8.8 视频验收

- [ ] 可生成缺失镜头图片。
- [ ] 可生成缺失镜头视频。
- [ ] 生成镜头视频前会检查镜头图片。
- [ ] 可重生成失败镜头图片。
- [ ] 可重生成失败镜头视频。
- [ ] 可取消运行中镜头视频任务。
- [ ] 可查看镜头图片。
- [ ] 可查看镜头视频。
- [ ] 可删除或解绑镜头图片。
- [ ] 可删除或解绑镜头视频。
- [ ] 删除媒体绑定不会默认物理删除对象存储文件。
- [ ] 可合成最终视频。
- [ ] 合成前检查镜头视频完成状态。
- [ ] 可查看 final video。
- [ ] 可设置 active final video。
- [ ] 可删除非当前 final video。
- [ ] 当前 final video 删除需要强确认或先切换版本。
- [ ] 可下载 final video。

### 8.9 Agent 验收

- [ ] Agent 工具列表只展示用户有权限的工具。
- [ ] Agent 默认 require_approval。
- [ ] Agent 不绕过 RBAC。
- [ ] Agent 不绕过 supervision。
- [ ] Agent 不绕过 Provider Gateway。
- [ ] workflow.start step 成功只表示启动成功，不表示子工作流完成。
- [ ] Agent 不会在依赖 workflow 未完成时继续执行后续生产步骤。
- [ ] Agent step 显示 dryRunOutput。
- [ ] Agent step 显示 supervisorDecision。
- [ ] Agent step 显示 output。
- [ ] Agent step 显示 verifierOutput。
- [ ] Agent step 输出能关联到业务产物卡。
- [ ] 审批步骤可批准。
- [ ] 审批步骤可拒绝。
- [ ] 拒绝后不会执行该 step。
- [ ] 取消 Agent task 可用。

### 8.10 OpenAPI / CI 验收

- [ ] 新增 API 已写入 OpenAPI。
- [ ] 前端 api-client 已同步。
- [ ] 前端 types 已同步。
- [ ] route consistency check 通过。
- [ ] Go tests 通过。
- [ ] Web typecheck 通过。
- [ ] Web lint 通过。
- [ ] OpenAPI YAML parse 通过。
- [ ] Docker Compose config 通过。
- [ ] App profile build 后关键服务 healthy。

---

## 9. 推荐执行顺序

### 第 1 批：流程收口与文案

1. 项目概览流程条。
2. 主导航收敛。
3. 中文标签集中映射。
4. 内容/剧本/资产/分镜/视频/成片页面入口整理。

### 第 2 批：内容与剧本 CRUD

1. 支持 brief。
2. 剧本导入初始化 script/version/scene。
3. 剧本版本编辑与激活。
4. source/script delete impact。

### 第 3 批：Toonflow 手册与资产 prompt 迁移

1. Seed 默认导演手册。
2. Seed 默认视觉手册。
3. 实现 AssetPromptBuilder。
4. 接入资产图片生成。

### 第 4 批：资产与分镜 CRUD

1. 资产卡片 CRUD。
2. 参考图 CRUD。
3. 分镜镜头 CRUD。
4. 衍生资产需求 CRUD。

### 第 5 批：视频与成片 CRUD

1. 镜头图片/视频 unlink 与重生成。
2. 时间线 clip CRUD。
3. final video 版本管理。
4. 下载与 active 版本切换。

### 第 6 批：Agent 对齐与整体验收

1. Agent step 输出产物卡。
2. Agent 只暴露主流程工具。
3. 补齐审批 UX。
4. 全链路手动验收。
5. `pnpm run test`。

---

## 10. 完成定义

本计划完成时，用户应能在不理解任何内部技术名词的情况下完成：

```text
新建项目
→ 添加创意文案
→ 生成剧本
→ 提取角色/场景/道具
→ 生成资产图
→ 生成分镜和衍生资产
→ 生成镜头视频
→ 合成成片
→ 下载成片
```

并且每个阶段生成的业务内容都能：

```text
查看
修改
删除或归档
重生成
查看来源与失败原因
```
