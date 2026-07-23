# CineWeave 组织与用户模块补全方案

## 1. 目标与边界

目标是把现有 RBAC 数据骨架补成管理员可在 Web 内完成的管理闭环，覆盖组织、成员、邀请、团队、角色绑定、账号资料、用户名登录与安全约束，并与现有 organization/workspace/project 三级资源权限模型保持一致。

本阶段不引入企业 SSO、SCIM、跨组织计费、邮件服务商强绑定或复杂自定义权限表达式。邀请通知先以可复制邀请链接为基础，邮件发送作为可插拔后续能力。

## 2. 当前实现基线

### 已有能力

- 数据库已有 `users`、`organizations`、`organization_members`、`teams`、`team_members`、`roles`、`permissions`、`role_permissions`、`role_bindings`。
- 已有组织创建/读取、团队 CRUD、团队成员增删、角色/权限读取、角色绑定增删 API。
- 已有组织、工作区、项目三级资源作用域，以及用户/团队两类授权主体。
- 已有 `org_owner` 最后所有者保护和后端 `Authorizer` 权限检查。
- OpenAPI 已覆盖当前团队与角色绑定接口，前端已有基础 API client 和权限管理入口。

### 主要缺口

- 没有组织成员列表、成员详情、邀请、接受邀请、停用/移除、恢复等 API 和 UI。
- 当前认证只以邮箱作为用户登录标识，缺少独立、稳定且可用于登录的用户名。
- 团队成员接口只返回用户 ID，缺少姓名、邮箱、头像和有效角色摘要。
- 角色绑定虽有后端接口，但前端没有创建、查看、筛选、撤销闭环。
- 系统角色只能读取，缺少角色权限详情；组织自定义角色缺少 CRUD 与权限配置。
- 组织只支持创建/读取，缺少名称设置和安全退出规则。
- 当前权限页把组织、工作区、团队、角色、权限混在一个静态页面，无法承担真实管理任务。
- 缺少成员与权限变更的可审计事件，以及邀请令牌的生命周期管理。

## 3. 产品信息架构

将现有“权限管理”升级为“组织与权限”，采用稳定的页内分区或子路由：

1. `成员`：成员搜索、状态、加入时间、团队、直接角色、停用/移除。
2. `邀请`：创建邀请、复制链接、查看有效期、撤销、重新生成。
3. `团队`：团队 CRUD、成员管理、团队角色绑定。
4. `角色与权限`：首期提供系统角色详情、权限分组和角色绑定；阶段 E 完成后再显示自定义角色 CRUD，不提供“开发中”占位入口。
5. `组织设置`：组织名称、当前组织切换、退出组织和危险操作；首期不开放 slug 修改。
6. `我的账号`：用户名、姓名、头像等个人资料；邮箱只读，密码修改作为独立安全流程。

普通成员只看到自身可读信息；具有对应管理权限的用户才看到写操作。前端隐藏仅用于体验，所有约束仍由后端授权。

## 4. 数据模型方案

### 4.1 组织邀请

新增 `organization_invitations`：

- `id`、`organization_id`
- `email`（规范化后存储）
- `token_hash`（只存哈希，不存明文）
- `status`: `pending | accepted | revoked`；对外展示的 `expired` 由 `status=pending AND expires_at <= now()` 推导
- `expires_at`、`accepted_at`、`accepted_by`
- `invited_by`、`created_at`、`updated_at`
- 必填 `base_role_id`，首期固定为允许邀请使用的 organization scope 基础角色
- 可选初始资源绑定，至少支持项目角色

约束：同组织同邮箱最多一个有效邀请；邀请阶段不创建 `organization_members` 的伪成员行。`expired` 由 `expires_at` 推导，不作为必须由定时任务维护的持久状态。接受动作必须事务化创建/激活 `organization_members`、创建基础角色及初始资源绑定、消费邀请并签发目标组织 session。邀请发出后角色或资源失效时，接受动作必须拒绝并要求管理员重新生成邀请，不能静默改变邀请权限。

初始资源绑定使用结构化子表 `organization_invitation_bindings`，不使用裸 JSON：

- `id`、`invitation_id`、`role_id`、`resource_type`
- `resource_organization_id`、`resource_workspace_id`、`resource_project_id`
- 与 `role_bindings` 相同的互斥 scope check 和外键约束
- 同一邀请、角色、资源唯一

邀请记录与绑定子表共同构成发出时的授权快照。接受时重新校验角色仍有效、角色 scope 与资源类型一致、资源仍属于邀请组织，然后创建正式 `role_bindings`。

### 4.2 用户账号与用户名

复用 `users` 主体：现有 `display_name`、`avatar_url`、`status` 直接使用，新增 `username`、`username_normalized` 和 `updated_at`。认证标识与展示资料分离；禁止管理员直接读取密码哈希或会话令牌。

用户名规则：

- 用户名是全站唯一登录标识，不随组织变化。
- 输入允许大小写，但唯一性和登录匹配使用 `username_normalized`；首期规范化固定为去除首尾空白、ASCII 格式校验、转 ASCII 小写、保留字校验。
- 为降低同形异义和跨客户端差异，首期用户名只允许 ASCII 字母、数字、下划线和短横线，长度 3 至 32；必须以字母或数字开头和结尾。
- 禁止与邮箱形式混淆的 `@`，并维护系统保留字集合，例如 `admin`、`root`、`system`、`api`、`support`。
- 注册时必须提供用户名；历史用户迁移时允许 `username`/`username_normalized` 暂时为 NULL，用户仍可用邮箱登录，并在首次登录后被引导设置正式用户名。不能从邮箱本地部分直接推断并永久占用。
- 用户名首期设置后不可自行修改，避免审计身份漂移；未来若开放修改，必须保留历史记录和冷却期。
- 登录接口接收统一 `identifier`，后端先判断规范化后的邮箱格式，否则按用户名查找；错误响应不得暴露账号是否存在。
- 数据库迁移允许 `username` 和 `username_normalized` 为 NULL，并创建 `WHERE username_normalized IS NOT NULL` 的部分唯一索引，兼容尚未完成首次设置的历史用户。

### 4.3 组织成员生命周期

组织成员明确区分暂停与移除：

- `active`：正常成员。
- `disabled`：暂停成员；保留团队成员、项目成员和角色绑定，撤销组织 session，恢复后原授权重新生效。
- `removed`：已移除成员；清理团队成员、项目成员和该组织内的用户角色绑定，不允许直接恢复，只能通过新邀请重新加入。

为 `organization_members` 增加 `updated_at`、`disabled_at`、`disabled_by`、`removed_at`、`removed_by`。现有 `invited` 状态不再用于新邀请；邀请只存在于 `organization_invitations`。旧 `invited` 数据在迁移中按实际可用数据清理或转换。

### 4.4 角色管理

复用 `roles` 和 `role_permissions`：

- `is_system=true` 的角色只读，不允许删除或修改权限。
- 组织自定义角色必须带 `organization_id`，`role_key` 在组织内唯一。
- 自定义角色 scope 固定为 `organization | workspace | project` 之一。
- 权限只能从后端允许分配给该 scope 的集合中选择，不能由前端任意提交字符串。

### 4.5 审计

管理审计统一写入现有 `audit_logs`，不复用面向异步业务事件投递的 `event_outbox`，避免把安全审计的查询和保留语义耦合到事件发布生命周期。写操作与审计记录使用同一数据库事务，覆盖邀请创建/撤销/接受、成员停用/恢复/移除/主动退出、团队及团队成员变更、角色创建/修改/删除、角色绑定创建/撤销、组织设置、个人资料和用户名设置。

查询入口为 `GET /api/organizations/{organizationId}/audit-logs`，要求 `audit.read`，支持分页以及 action、actor、resourceType 筛选。首期保留策略为 `organization_lifetime`：审计记录随组织生命周期保留，不做定时裁剪；组织删除时由外键级联清理。metadata 只存展示和追溯所需的非敏感摘要，不记录邀请明文令牌、密码、密码哈希、access/refresh token 或完整请求体。

## 5. API 与权限设计

### 5.1 认证、组织上下文与账号

- `PATCH /api/organizations/{organizationId}`：更新组织设置。
- `POST /api/auth/login`：请求改为 `identifier + password`，identifier 同时支持用户名和邮箱。
- `POST /api/auth/register`：新增必填 `username`；注册成功后保持现有个人组织创建行为。
- `POST /api/system/setup`：首次部署初始化同步增加必填 `username`，与 register 共用用户名校验，并保留当前“只允许创建第一个用户”的并发保护；`GET /api/system/setup-state` 行为保持不变。
- `POST /api/auth/register-with-invitation`：未注册用户凭邀请令牌注册；校验邮箱与邀请一致，接受邀请并直接签发目标组织 session，不额外创建个人组织。
- `POST /api/auth/select-organization`：多组织登录后使用短时一次性组织选择令牌选择目标组织并签发正式 session。
- `POST /api/auth/switch-organization`：校验 active membership，轮换 refresh session 并签发绑定目标组织的新 token；不能仅由前端改本地 organizationId。
- `GET /api/auth/me`：保持现有读取；增加用户名、有效组织列表和当前成员上下文摘要。
- `PATCH /api/auth/me`：只允许更新展示名称和头像；历史账号通过专用的一次性流程设置正式用户名。
- `POST /api/auth/me/username`：仅用于无正式用户名的历史账号首次设置，设置后首期不可修改。
- `POST /api/organizations/{organizationId}/leave`：成员主动退出；最后所有者禁止退出。
- `GET /api/system/organizations`：仅系统管理员可访问的全局组织目录，支持按组织名称或 slug 搜索、分页，并返回有效成员、直接所有者、工作区和项目数量。
- `POST /api/system/organizations`：仅系统管理员可创建组织；必须用精确用户名或邮箱指定一个现有 active 用户作为初始直接所有者，并在同一事务中创建默认工作区和审计记录。普通组织 API 不再提供创建入口。

登录和组织选择采用两阶段模型：

1. 用户名/邮箱与密码验证成功后查询 active organization memberships。
2. 没有有效组织时返回 `NO_ACTIVE_ORGANIZATION`，不签发业务 session。
3. 只有一个有效组织时直接签发该组织的 access/refresh session。
4. 有多个有效组织时返回组织脱敏摘要和 `organizationSelectionToken`，不签发正常 access/refresh token。
5. 客户端调用 `POST /api/auth/select-organization` 后才获得正式 session，不隐式选择创建时间最早的组织。

`organizationSelectionToken` 使用独立 token audience/purpose，只包含 user ID、允许选择的组织 ID 集合、唯一 nonce 和过期时间；有效期 5 至 10 分钟、只能使用一次、服务端保存 nonce 哈希并在使用后消费，不能访问任何业务 API，也不能写入长期浏览器存储。

已有组织 session 的切换与登录后首次选择分开处理。`switch-organization` 请求携带当前 refresh token 和目标组织 ID；后端在事务中锁定并撤销当前 refresh session、校验目标 active membership、创建新 session。切换只影响当前设备，不撤销其他设备 session；同一 refresh token 并发切换只能成功一次。旧 access token 在过期前仍只代表旧组织，并继续受旧组织 active membership 校验。

### 5.2 成员与邀请

- `GET /api/organizations/{organizationId}/members`：分页、搜索、状态筛选，返回用户展示信息、团队和角色摘要。
- `GET /api/organizations/{organizationId}/members/{userId}`：成员详情和有效权限来源。
- `PATCH /api/organizations/{organizationId}/members/{userId}/profile`：具有 `member.manage` 的管理员更新成员显示名称和头像；用户名、邮箱仍是只读登录身份。
- `POST /api/organizations/{organizationId}/members/{userId}/password-reset`：生成 30 分钟有效的一次性密码重置令牌；签发时立即清除旧密码、提升凭据版本并撤销该账号全部 session。
- `POST /api/auth/password-reset/complete`：公开消费一次性令牌并设置新密码；令牌只允许放在 POST body，成功后再次提升凭据版本并撤销残余 session。
- `PATCH /api/organizations/{organizationId}/members/{userId}`：暂停或恢复，只在 `active <-> disabled` 间转换；暂停时撤销该组织 session，但保留授权关系。
- `DELETE /api/organizations/{organizationId}/members/{userId}`：移除成员，将状态改为 `removed`，事务化清理授权关系与 session；不能通过 PATCH 恢复。
- `GET/POST /api/organizations/{organizationId}/invitations`：列表/创建邀请。
- `DELETE /api/organizations/{organizationId}/invitations/{invitationId}`：撤销邀请。
- `POST /api/organization-invitations/resolve`：公开解析邀请，令牌放在请求体，仅返回脱敏组织名、脱敏邮箱、有效性和是否需要注册。
- `POST /api/organization-invitations/accept`：已登录且账号邮箱与邀请一致的用户接受邀请，返回目标组织的新 session。

新增权限 `member.read`、`member.manage`；邀请并入 `member.manage`，避免继续用过宽的 `admin.manage` 承载日常成员管理。系统角色 seed 矩阵：

| 系统角色 | `member.read` | `member.manage` |
| --- | --- | --- |
| `org_owner` | 是 | 是 |
| `org_admin` | 是 | 是 |
| `org_member` | 否；只能通过 `/api/auth/me` 读取自身 | 否 |
| 工作区/项目级系统角色 | 否 | 否 |

成员管理列表中的完整邮箱只对具有 `member.read` 的用户返回；普通成员不能通过成员目录枚举组织邮箱。

### 5.3 团队、角色与绑定

- 保留现有团队与团队成员 API，但响应补齐用户摘要。
- `POST/PATCH/DELETE /api/roles/{roleId}`：组织自定义角色管理，调整到系统角色绑定闭环完成后的独立阶段。
- `GET /api/roles/{roleId}`：返回权限集合和使用中的绑定数量。
- `GET /api/roles/{roleId}/impact`：返回绑定、直接用户、团队、受影响用户和各资源 scope 数量，供修改或删除前确认。
- 现有 `/api/role-bindings` 增加 subject/resource/role 筛选，并在响应中返回可展示名称。
- 删除团队或自定义角色前返回影响摘要；有绑定时采用明确的冲突响应，不静默级联扩大权限变化。团队停用、成员停用和绑定过期必须在有效权限计算中立即生效。

所有新增/变更公开路由同步更新 OpenAPI、Web API client/types 和路由一致性检查。

## 6. 后端安全与一致性规则

- 成员停用后必须立即失去该组织上下文访问；认证中间件和 Authorizer 均校验 active membership，并撤销该用户在该组织的全部 refresh session。已有短期 access token 即使尚未过期也必须因 membership 校验失败而返回 403。
- 组织必须至少保留一个直接绑定到 active 用户的有效 owner；团队 owner 可用于授权，但不能作为唯一的最后所有者保障。所有 owner 变更、停用、退出和移除操作使用同一事务与行锁，防止并发移除最后所有者。
- 给自己绑定角色与给其他用户绑定角色执行完全相同的 `role.manage`、scope 和组织归属校验；不能因目标主体是自己绕过权限。若后续禁止敏感角色自授予，必须显式维护敏感角色集合，不能笼统禁止全部自绑定。
- 禁止跨组织查询用户、团队、角色、绑定或邀请。
- 接受邀请、暂停/恢复/移除成员、变更角色权限使用事务；并发操作通过唯一约束和行锁保证幂等。成员暂停只改变 membership 状态并撤销组织 session；成员移除还必须处理 `team_members`、用户 `role_bindings`、该组织下的 `project_members` 和组织 auth sessions，并保留 created_by 等历史审计引用。
- 邀请令牌使用高熵随机值，API 仅在创建时返回一次明文；数据库保存哈希并强制过期。
- 邀请记录保存发出时的 `binding_count`；解析和接受时重新加载结构化绑定并同时校验数量、角色和资源。资源删除导致绑定行级联消失时，邀请必须整体失效，不能退化为只授予基础角色。
- 邀请链接把令牌放在前端 URL fragment，例如 `/accept-invitation#token=...`；前端读取后立即从地址栏清除，再通过 POST body 调用 resolve、accept 或 register-with-invitation。相关响应使用 `Cache-Control: no-store`，服务端、代理和错误追踪不得记录令牌或请求体。
- 邀请解析、注册和登录使用数据库共享的限流、失败计数和通用错误响应，按动作同时限制 HMAC 哈希后的账号/令牌主体与客户端地址；数据库不保存原始用户名、邮箱、邀请令牌或 IP。登录在账号不存在时仍执行固定 dummy bcrypt 校验，注册冲突统一返回 `REGISTRATION_UNAVAILABLE`，不能通过响应状态、耗时或文案枚举账号。
- 用户名规范化逻辑必须由后端单一实现并有固定测试向量；数据库唯一索引落在 `username_normalized`，不能依赖数据库默认 collation。
- 用户账号是跨组织全局主体。组织管理员只能修改独占当前组织的账号资料和密码；账号同时属于多个未移除组织时返回冲突，避免一个组织接管其他组织仍在使用的账号。普通组织管理员不能管理直接所有者，直接所有者或账号本人仍可按权限操作。
- 密码重置令牌使用高熵随机值，数据库只保存哈希；签发响应和完成响应使用 `Cache-Control: no-store`。Access token 携带凭据版本，认证中间件与数据库当前版本不一致时立即返回 401，确保旧 access token 不等待自然过期。
- 系统管理员身份存储在用户记录中：首次 Setup 创建的账号自动获得该身份，已有部署升级时回填最早创建的账号（优先 active）。系统级 API 必须在每次请求和创建事务内从数据库实时复核 `is_system_admin`，不能信任 JWT 或前端导航状态；创建者不自动成为新组织成员。
- 列表 API 统一分页，不在大组织中一次加载全部成员和绑定。

## 7. 前端交互方案

- 将 `access-page.tsx` 拆为成员、邀请、团队、角色、绑定、组织设置的独立 feature 组件，避免继续扩展单文件。
- 使用表格/列表呈现成员和绑定，支持搜索、状态筛选、分页与空状态。
- 邀请、编辑团队、管理团队成员、创建角色、绑定角色均使用对话框；打开下拉框但未选择时不得误关闭。
- 角色绑定表单按作用域联动资源选择：组织角色固定当前组织，工作区角色选择工作区，项目角色选择项目。
- 登录页字段统一标为“用户名或邮箱”；注册页实时提示用户名格式，但最终可用性以后端校验为准。
- 首次部署 Setup 页面同步增加用户名字段，不能绕过普通注册使用的用户名规则。
- 多组织用户登录后进入组织选择页；组织切换必须调用后端换发 session，并清空上一组织的 React Query 缓存与工作区状态。
- 邀请接受页从 fragment 读取令牌后立即执行 `history.replaceState` 清除地址栏令牌，不把令牌写入 localStorage、日志或分析事件。
- 成员详情展示“直接角色”和“通过团队继承的角色”，避免管理员误判权限来源。
- 成员详情按身份、资料、账号安全和权限来源分区：可管理账号允许编辑显示名称/头像并发起密码重置；多组织账号和受保护所有者显示明确中文原因，资料字段只读且不显示保存/重置入口。
- 密码重置链接只在签发后展示一次；重置页从 URL fragment 读取令牌后立即清除地址栏，不写入 localStorage、日志或分析事件，成功后引导用户使用用户名或邮箱重新登录。
- 危险操作必须二次确认，并明确影响范围；最后所有者等后端冲突使用中文错误。
- 所有角色、权限、成员状态、主体类型和资源类型走 `labels.ts` 集中中文映射，不显示裸英文枚举。
- 查询 key 和 mutation 失效范围独立收口，确保邀请、成员、团队、绑定更新后页面一致。
- 全局导航仅向系统管理员显示“系统组织”；目录页以可搜索、可分页的表格展示全平台组织统计，并通过创建对话框显式填写组织名、默认工作区和初始所有者用户名/邮箱。前端隐藏只改善体验，直接访问仍以服务端 403 为准。

## 8. 分阶段实施

### [x] 阶段 A：用户名登录与组织 session（P0）

1. 为 `users` 增加用户名字段、规范化唯一索引和历史账号过渡状态。
2. 登录改为 identifier，支持用户名或邮箱；普通注册与首次 Setup 同步增加用户名。
3. 实现历史账号首次设置用户名、一次性组织选择令牌和组织 session 切换。
4. 更新 OpenAPI、API client/types、登录/注册/Setup/组织选择页和中文错误。
5. 增加规范化、NULL 历史用户名、重复用户名、保留字、账号枚举防护、选择令牌用途/过期/重放、session 轮换和跨组织切换测试。

完成标准：新用户可以用户名或邮箱登录；历史用户完成一次性用户名设置；多组织用户切换后只能使用新组织上下文。

### [x] 阶段 B：成员与邀请闭环（P0）

1. 增加邀请、邀请绑定、成员生命周期 migration，补齐成员权限 seed 和事务服务。
2. 实现邀请解析、受邀注册、已注册用户接受邀请。
3. 实现成员列表/详情/状态/移除与 session 撤销。
4. 完成成员、邀请和组织选择 UI。
5. 增加跨组织、邮箱匹配、fragment/POST 令牌传输、最后所有者、令牌过期/重放、重复接受、暂停恢复保留授权和移除后必须重新邀请测试。

完成标准：管理员可邀请新用户或已有账号；用户接受后直接进入目标组织；管理员可停用、恢复、移除成员，且权限与 session 实时失效。

### [x] 阶段 C：团队与系统角色绑定闭环（P0）

1. 补齐团队成员展示信息和团队编辑/停用影响检查。
2. 补齐系统角色详情、角色绑定筛选、展示摘要和前端增删交互。
3. 展示直接授权与团队继承来源。
4. 完成组织/工作区/项目三种 scope 的授权测试。

完成标准：管理员可通过用户或团队分配/撤销不同资源层级的系统角色，成员实际 API 权限与 UI 展示一致。

阶段 C 完成后执行一次核心 P0 管理闭环验收：使用三个账号完成登录、邀请、多组织选择、成员暂停/恢复/移除、团队授权和系统角色绑定，并通过 HTTP 验证实际权限。这是组织与用户模块首个可发布基线，不等待阶段 D、E。

### [x] 阶段 D：组织设置、个人资料与审计（P1）

1. 实现组织名称编辑和成员退出；首期不开放 slug 修改或组织删除。
2. 实现个人展示名称和头像编辑。
3. 明确并实现成员、邀请、团队和角色绑定审计查询。
4. 完成危险操作影响摘要与中文错误。

完成标准：管理员无需改数据库即可维护组织基础资料，成员可维护个人资料，关键管理变更可追溯。

### [x] 阶段 E：自定义角色（P1）

1. 实现自定义角色 CRUD、scope 权限白名单和使用中保护。
2. 增加角色权限变更影响预览和审计。
3. 验证系统权限 seed 升级不会意外扩大现有自定义角色权限。

完成标准：管理员可创建组织级自定义角色，但不能修改系统角色或分配不属于该 scope 的权限。

### [x] 阶段 F：完整回归验收（P1）

1. 运行定向 Go API/Authz 测试，再运行 `pnpm run test`。
2. 执行 migration Up/Down/Up 或仓库当前隔离 roundtrip 流程。
3. Compose 重建并检查服务健康。
4. 通过至少三个真实账号在浏览器验证首次 Setup、用户名/邮箱登录、新用户与已有用户邀请、受邀注册、多组织选择/切换、授权、撤权、暂停/恢复、移除后重新邀请和最后所有者保护。
5. 使用 HTTP/API 验证被撤权用户立即收到 403，不能只依赖页面隐藏。

验收记录（2026-07-18）：`pnpm run test` 全量通过；在隔离 Compose 项目中完成 migration Up/Down/Up、最新服务构建与健康检查，并使用三个以上真实账号完成浏览器/API 全流程验收。团队授权、自定义角色权限、停用旧 session、撤权和移除后重新邀请均通过 HTTP 验证即时生效；隔离资源在验收后清理，主环境按并行生产任务约束未重复重建。

### [x] 阶段 G：组织管理员成员账号管理（追加）

1. 为用户增加凭据版本，为密码重置增加只存哈希的一次性令牌表和 30 分钟有效期。
2. 实现成员资料更新、密码重置签发/完成、全 session 撤销、旧 access token 即时失效和重放防护。
3. 明确全局账号边界：多组织账号禁止由单个组织修改全局资料或密码；普通组织管理员禁止管理直接所有者。
4. 在成员详情中加入资料与账号安全区域、危险操作确认、一次性链接展示，以及独立重置密码页面。
5. 补齐 OpenAPI、Web client/types、中文错误和审计动作，并增加服务层、真实数据库、API、浏览器和迁移 roundtrip 验收。

完成标准：具有 `member.manage` 的组织管理员可以维护独占本组织成员的显示名称和头像，并安全发起一次性密码重置；旧密码、旧 access/refresh session 和重复使用令牌均立即失败，跨组织账号和直接所有者受到明确保护。

验收记录（2026-07-19）：`pnpm run test` 全量通过；migration 000034 在独立 PostgreSQL 完成 Up/Down/Up；Auth 集成测试覆盖资料更新、所有者保护、多组织冲突、令牌哈希、session 撤销和重放。隔离 Compose 中使用所有者、组织管理员、普通成员和多组织成员四类真实账号完成浏览器/API 验收：用户名登录成功，资料更新与中文审计可见，重置页清除 fragment，旧 access/refresh/密码均返回 401，重复令牌返回 410，多组织操作返回 409，普通管理员操作直接所有者返回 403；主环境未重建。

### [x] 阶段 H：系统管理员组织管理（追加）

1. migration 000035 为用户增加系统管理员标记；首次 Setup 自动授予，升级时为已有部署确定性回填一个系统管理员。
2. 新增系统级组织目录和创建服务；创建时按精确用户名或邮箱解析 active 初始所有者，事务化创建组织、默认工作区、直接 owner 绑定和审计记录。
3. 收紧创建边界：移除普通 `POST /api/organizations`，组织成员权限不能替代平台系统管理员身份；系统管理员身份在数据库中实时复核。
4. 新增仅系统管理员可见的“系统组织”导航、全局目录、搜索/分页统计和创建对话框，并同步 OpenAPI、Web client/types、查询 key、中文错误与审计动作。
5. 增加服务层、真实 PostgreSQL API 集成、migration Up/Down/Up、Web 静态构建及隔离浏览器验收。

完成标准：系统管理员可以查看全平台组织，并把现有 active 用户指定为新组织初始直接所有者；创建者不会被隐式加入组织，普通用户访问系统 API 返回 403，旧的普通组织创建路由不可用。

验收记录（2026-07-19）：`pnpm run test` 全量通过，包含 Go 全仓、migration/seed validate、Web typecheck/lint、OpenAPI 解析与 317 条路由一致性、Compose 配置检查；migration 000035 在独立 PostgreSQL 完成 Up/Down/Up 和已有用户回填验证。真实数据库 API 集成测试覆盖 Setup 授权、普通用户 403、系统管理员创建/查询、初始所有者绑定、创建者不入组、默认工作区、审计、无效所有者和旧创建路由 405。隔离 Web/API 浏览器验收覆盖系统导航、组织目录统计、创建对话框、按用户名指定所有者、创建后刷新与搜索，浏览器控制台无错误；主环境未重建。

### [x] 阶段 I：系统管理员直接管理组织成员（追加）

1. 新增系统级成员列表、直接新增和编辑 API；系统管理员不需要属于目标组织，但每次读取及写事务仍从数据库实时复核 `is_system_admin`。
2. 直接新增支持创建新账号，或按精确用户名/邮箱添加、恢复已有账号；无需邀请，事务内激活 membership、授予基础组织成员角色、撤销同邮箱未使用邀请并记录审计。
3. 系统管理员可编辑用户名、邮箱、显示名称、头像、密码和 `active/disabled` 成员状态；密码变更提升凭据版本并撤销全部 session。
4. 系统管理员目标账号继续受保护，停用最后一个有效直接所有者仍被拒绝；普通用户调用系统成员 API 返回 403。
5. “系统组织”目录增加成员管理入口，提供新增方式选择、搜索/筛选、成员编辑和受保护状态提示；普通组织邀请流程继续保留。

完成标准：系统管理员可以在任意组织直接创建或关联成员账号并维护其登录资料与启停状态，不再依赖邀请；所有新增账号可立即登录，权限、会话撤销、生命周期和审计边界保持一致。

验收记录（2026-07-23）：独立 PostgreSQL 从空库迁移到 v57 并加载/校验系统 seed 后，真实 API 集成覆盖非目标组织系统管理员的列表、直建、已有账号加入、用户名/邮箱/密码更新、旧凭据失效、启停、基础角色、审计、重复成员冲突、普通用户 403 和系统管理员目标保护；`pnpm run test` 全量通过，OpenAPI 与 Go 路由一致为 400 operations，Commerce contract 保持 72 operations/48 events；主环境未迁移或重建。

## 9. 主要涉及文件

- `db/migrations/*`
- `internal/auth/*`
- `internal/authz/*`
- `internal/api/access.go`
- `internal/api/system_administration.go`
- `internal/api/server.go`
- `internal/api/*_test.go`
- `packages/openapi/openapi.yaml`
- `apps/web/src/features/access/*`
- `apps/web/src/features/system/*`
- `apps/web/app/system/organizations/page.tsx`
- `apps/web/src/lib/api-client.ts`
- `apps/web/src/lib/types.ts`
- `apps/web/src/lib/labels.ts`
- `apps/web/src/lib/query/keys.ts`

## 10. 与当前全流程测试任务的协作约束

指定任务仍在同一工作区修改生产链路、OpenAPI、API client、labels、query keys、server 路由和 migration 注册文件。正式实现前应等待其形成稳定检查点，然后先重新读取这些共享文件并以当前内容为基线。每个阶段都必须自行完成定向后端测试、OpenAPI/API client 对齐、前端 typecheck/lint 和对应浏览器/API 验收；阶段 C 后执行核心 P0 全流程验收，阶段 F 是包含 P1 功能的完整回归，不替代前面阶段的验收。

实施优先从独立的新 migration、独立 API service/handler 和 `features/access` 子组件开始；对 `server.go`、OpenAPI、API client、labels、query keys、migration embed/runner 的合并放在每一阶段末尾集中完成，避免与正在运行的全流程测试任务互相覆盖。

## 11. 暂不纳入本轮

- SSO/SAML/OIDC 企业身份源
- SCIM 自动同步
- 邮件供应商的强制集成
- 系统管理员授权/撤销和平台级账号停用、删除等全局生命周期管理
- 组织停用、删除、所有权转移和跨组织用户目录
- 复杂条件策略、拒绝规则或 ABAC
- 组织计费、席位与配额管理
