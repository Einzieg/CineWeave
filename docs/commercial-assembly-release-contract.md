# Commercial Assembly 迁移与组合发行契约

本文档定义 CineWeave Community Core 与私有 Internal Commercial Assembly 的数据库迁移、DDL 所有权和组合 Release Manifest 接口。它是公共装配契约，不包含商业业务实现、私有迁移或客户 License 签发能力。

## 1. 固定边界

- Community Core 迁移仍由 `cmd/cineweave-migrate` 执行，只读取公共 `db/migrations`。
- Commercial Assembly 提供自己的私有嵌入式迁移文件和独立一次性迁移命令。
- 两个迁移流使用不同的 FS、编号空间、控制 schema、ledger、audit table 和 advisory lock。
- CE 迁移命令不加载、枚举或探测 Commercial migration FS。缺少私有仓库时，CE 的构建、迁移和运行不受影响。
- Commercial migration 只能修改 `cineweave_commercial` schema 中的对象。`cineweave_commercial_migrations` 只由迁移执行器管理。
- Core 的 `public`、`cineweave_migrations` 及其中未来新增对象都由 Core 所有。Commercial migration 不得修改这些对象。
- Commercial 对 Core 主键的引用只能从 Commercial 表一侧建立。需要 Core 中性字段时，必须先提交公共前向 Core migration。

权威机器清单：

- `packages/edition/ddl-owners.v1.json`
- `internal/editionmigration`
- `internal/migrationstream`

固定数据库身份：

| 迁移流 | 控制 schema | ledger | audit table |
| --- | --- | --- | --- |
| Core | `cineweave_migrations` | `cineweave_schema_versions` | `cineweave_migration_audit` |
| Commercial | `cineweave_commercial_migrations` | `schema_versions` | `migration_audit` |

## 2. `core.lock` 与临时 Overlay 装配

私有仓库必须在根目录维护：

- `core.lock`：符合 `packages/edition/core-lock.schema.json`，固定 Core remote、完整 commit、迁移头和关键公共契约 hash。
- `overlay-allowlist.v1.json`：符合 `packages/edition/overlay-allowlist.schema.json`，逐文件声明私有 source、最终 destination、`add`/`replace` 和 SHA-256。

公共 `packages/edition/overlay-slots.v1.json` 是唯一可替换文件清单。目前只允许替换 `apps/web/src/edition/selected-entry.ts` 这个 Web Edition 选择槽。其他商业 Go 服务、迁移、路由和前端代码必须以新文件添加，通过公共接口、Go build tag 或显式注册点接入，不能覆盖 Core 安全边界。

Overlay allowlist 不在自身内容中保存 Commercial commit，避免“文件包含自身 commit”形成不可能稳定的循环。装配命令显式接收私有仓库完整 commit，验证私有 checkout clean 且 HEAD 完全一致，再把该 commit 写入装配证据和最终 Release Manifest。

临时装配命令：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
./scripts/assemble-commercial-release.ps1 `
  -CommercialRepositoryPath <private-repository> `
  -CommercialCommit <full-commercial-commit> `
  -OutputDirectory <new-empty-output-directory>
```

装配器从 `core.lock` 固定的 commit 执行 `git archive`，不会复制 Core 工作树；随后只复制 allowlist 中逐个列出的私有普通文件。Core/Commercial 任一仓库 dirty、commit/hash 漂移、路径穿越、大小写目标冲突、符号链接、覆盖保护文件、`add` 命中已有文件或未声明的 `replace` 都会在创建输出前失败。

成功后，临时树的 `.cineweave/assembly-inputs.json` 记录 Core SHA、Commercial SHA、lock/allowlist/slot/装配脚本路径与 hash、两个仓库的 clean 检查和每个 Overlay 文件。它必须原样进入组合源码归档根目录，作为生成最终 Release Manifest 的输入；它本身不是部署授权。

## 3. 商业 API 扩展槽与最终契约

公共 API Server 只通过 `CommercialModuleRegistry.APIModules` 加载商业路由。每条 `APIModuleRegistration` 必须：

- 在 Edition Manifest 的 `compiledModules` 中声明相同 module key 和 feature key。
- 使用集中 Feature Registry 中需要租户权益的商业 feature。
- 声明规范化 HTTP method、`/api/` path、唯一 `operationId` 和运行操作策略类型。
- 声明至少一个属于该 feature 的 Core RBAC permission。
- 使用 Core 内置的 organization/workspace/project 资源作用域和真实 path parameter。
- 只提供业务 Handler，不能替换或绕过 Core authentication、Entitlement、RBAC 或资源解析。

API Server 固定按“Core 身份认证 -> 内部发行身份/tenant Entitlement -> Core RBAC -> 私有 Handler 内资源状态”执行。Entitlement 响应的 subject、contract version 和 allow flags 不一致时 fail-closed。CE registry 永远为空，所以商业 URL 在 CE 返回 `404`。

私有 API 模块同时生成 `cineweave.edition-api-routes.v1` route list。Assembly 使用以下命令合并公共与私有契约：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
python scripts/assemble-commercial-contracts.py `
  --openapi-extension <private-openapi-extension.yaml> `
  --event-extension <private-event-extension.yaml> `
  --route-list <private-edition-api-routes.json> `
  --output-directory .release/contracts
python scripts/check-openapi-routes.py `
  --contract .release/contracts/openapi.yaml `
  --route-source-manifest .release/contracts/route-sources.combined.json
```

合并器检测 `method + path`、`operationId`、所有 component 名、tag 和 event key 冲突；只有 canonical hash 完全相同的重复定义可以通过。新增的 OpenAPI 路由必须与私有 route list 的 method/path/operationId 完全一致。输出目录必须不存在，写入使用同卷临时目录后原子替换，并生成 `assembly-evidence.json` 记录全部输入、输出 hash 与数量。

机器契约和回归入口：

- `packages/edition/edition.v2.json`
- `scripts/assemble-commercial-contracts.py`
- `scripts/check-openapi-routes.py`
- `scripts/test-commercial-contract-assembly.py`

## 4. 私有迁移命令的装配方式

Commercial Assembly 只需要提供私有 `embed.FS`，使用公共工厂取得不可变流身份：

```go
definition := editionmigration.CommercialDefinition(privateMigrationFS)
runner, err := migrationstream.Open(ctx, migrationstream.Config{
    DatabaseURL: databaseURL,
    Environment: environment,
    ReleaseID:   releaseID,
}, definition)
```

私有命令必须实现与 Core 相同的 `up`、`verify`、`status`、`version`、`down`、`down-to` 和 `reset` 语义。生产环境自动拒绝破坏性命令；生产 `ReleaseID` 必须来自组合发行清单。

Commercial migration 文件必须从 `000001` 开始连续编号，同时包含 Goose Up/Down 段。它们不得复制 Core 编号、插入 Core ledger 行或进入公共 migration embed。

在装配流水线中，对私有目录先执行：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
go run ./cmd/cineweave-ddl-owner-check <private-migration-directory>
```

检查器 fail-closed 拒绝：

- 对 Core 或任意非 Commercial schema 的 `CREATE`、`ALTER`、`DROP`、`TRUNCATE`、索引、注释和授权 DDL。
- Core 表上的 trigger、policy、rule 和反向外键。
- 未限定 schema 的 DDL 和 `SET search_path`。
- 无法静态审计的动态 `EXECUTE`。
- 迁移 SQL 对 Core 或 Commercial ledger 的修改。

允许的跨边界行为是：Commercial 自有表或视图读取 Core 数据，以及 Commercial 表一侧显式引用 Core 主键。删除动作由私有 `contracts/core-foreign-key-actions.v1.json` 固定，并由 `scripts/check-core-fk-actions.py` 对迁移后的 PostgreSQL 系统目录逐项核对：财务、授权、计费主体和调用证据只允许 `ON DELETE SET NULL`；仅 `billing_ui_preferences` 这类纯 UI 偏好允许 `ON DELETE CASCADE`。任何新增、缺失或动作漂移的跨边界外键都会阻断测试与发行。

## 5. 部署顺序

组合发行固定执行：

1. Core migrate。
2. Core verify。
3. Commercial migrate。
4. Commercial verify。
5. Core seed。
6. Core seed verify。
7. Commercial seed。
8. Commercial seed verify。

任一步失败都停止发布。不得通过手工修改任一 ledger、跳过 hash audit 或重写已应用 SQL 继续。

内部发行身份漂移或切换回 CE 不会自动回滚或删除 Commercial 表。切换必须先冻结新的商业写操作、完成在途安全收尾、导出商业数据，并确认 Core 数据可以独立运行；不得通过运行时开关把同一实例动态降级。

## 6. 组合 Release Manifest

内部 Commercial 的最终产物必须生成符合以下 Schema 的 JSON：

- `packages/edition/release-manifest.schema.json`

流水线验证命令：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
python scripts/check-release-manifest.py `
  --manifest <combined-release-manifest.json> `
  --require-assembly-evidence `
  --assembly-inputs <assembled-tree>/.cineweave/assembly-inputs.json `
  --core-lock <private-repository>/core.lock `
  --overlay-allowlist <private-repository>/overlay-allowlist.v1.json `
  --assembly-script scripts/assemble-commercial-release.ps1 `
  --source-archive <combined-source.tar-or-zip> `
  --core-fk-actions <private-repository>/contracts/core-foreign-key-actions.v1.json `
  --verify-local-core
```

正式候选使用默认 fail-closed 模式，命令中的 `--require-assembly-evidence` 用于显式记录发行意图；即使遗漏该标记，检查器也仍会要求全部证据。检查器会把 Manifest 中的两个完整 commit、clean 标记和所有 hash 与真实 lock、allowlist、装配脚本及 tar/zip 源码归档逐项核对；归档根目录必须包含字节完全一致的 `.cineweave/assembly-inputs.json`，归档内的装配脚本和每个 Overlay destination 也必须与已核验 hash 一致。缺少任一输入、传入部分证据、归档内证据被重写或归档文件被替换都会阻断发行。只有 Schema fixture 和不代表发行候选的单元测试可以显式使用 `--contract-only`；该模式拒绝任何部分或真实装配证据。

生产切流前还必须从运行中的 New API 容器生成仓库外证明，并把四个身份同时核对：容器创建时的 `Config.Image`、运行镜像的 `RepoDigests`、固定上游证据与契约 fixture、组合 Release Manifest。`Config.Image` 必须直接使用 `repository@sha256:<digest>`；即使 `RepoDigests` 当前恰好命中，`latest` 或其他可移动 tag 也会被拒绝。

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
./scripts/capture-new-api-runtime-image.ps1 `
  -ContainerName <new-api-container> `
  -OutputPath <outside-both-repositories/new-api-runtime-image.json> `
  -UpstreamEvidencePath <signed-new-api-upstream-evidence.json> `
  -ContractManifestPath <private-new-api-fixture-manifest.json> `
  -ReleaseManifestPath <combined-release-manifest.json>
```

脚本只执行 Docker inspect，不读取环境变量、挂载文件或业务数据；输出路径位于 Core 或 Commercial 仓库内时会在访问 Docker 前失败。发行证据必须在部署候选容器创建后重新生成，旧运行实例的证明不能授权新发行。

Manifest 至少绑定：

- 不可变 Core commit、Commercial Assembly commit、`core.lock`、Overlay allowlist 和装配脚本 hash。
- 最终源码归档、Edition Manifest、内部运行范围（同一主体自营、禁止外部分发、不启用客户软件授权）、部署 ID 和 DDL owner manifest。
- Core/Commercial migration head 和各自 ledger identity。
- Core/Commercial seed、合并 OpenAPI/Event Catalog、webhook 和 Core FK action manifest。
- 完整服务镜像 tag/digest、Web build ID、四个 Temporal Worker Build ID、SBOM 和签名。
- Community Core 源码与第三方依赖 inventory 及报告 hash。
- New API 镜像 digest、源码 commit/tag、固定上游证据、修改状态和补丁 hash。
- API、Web、Gateway、Worker、双 migrator、双 seed 和 Billing Bridge 的统一 `releaseId`。

校验器拒绝可移动 ID、`enterprise` Edition、允许外部分发或客户软件授权、部署 ID 漂移、缺失或重复镜像、组件 Release ID 漂移、Worker Build ID 漂移、DDL owner hash 漂移、Core FK action manifest hash 漂移、双 ledger 身份漂移、源码依赖报告 hash 漂移、New API 镜像/源码身份漂移、未验证修改状态及修改无 patch hash。

仓库中的 `packages/edition/fixtures/combined-release-manifest.valid.json` 只用于契约回归，所有 commit、digest、内部运营记录和 URL 都是非生产示例，不能用于发布。

## 7. 验证证据

单元测试覆盖：

- `core.lock`、Overlay allowlist、受保护路径、大小写碰撞和 dirty tree。
- 临时 Core archive + allowlisted add/replace 的端到端装配及双 commit 证据。
- 两个迁移流的数据库身份完全独立。
- Commercial DDL 允许/拒绝矩阵。
- DDL owner manifest 与编译常量一致。
- Release Manifest Schema、owner hash、跨组件一致性，以及默认拒绝缺失/部分装配证据。
- Core/Commercial commit、clean 状态、lock/allowlist/装配脚本、tar/zip 归档、归档内证据和 Overlay 内容的 tar/zip 正向及 30 个负向回归。
- New API 容器配置引用、运行 RepoDigest、固定契约、上游证据和 Release Manifest 完全一致，且证据不能写入源码仓库。

隔离 PostgreSQL Up/Down/Up：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
./scripts/test-runtime-hardening.ps1 -MigrationOnly
```

该测试先同时应用 Core 和 Commercial fixture，单独 reset Commercial，验证 Core ledger 与 Core 表不变，再重新应用 Commercial。测试只使用临时 Docker network 和临时 PostgreSQL，不连接主环境。
