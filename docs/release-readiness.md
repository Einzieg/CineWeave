# CineWeave 正式发布准备

## 当前发布基线

- Go 最低工具链：`1.26.5`。
- Node.js：`24`；pnpm：`10.32.1`。
- PostgreSQL：`16`。
- 当前源码迁移头：`000076_provider_billing_context_trigger_table_guard.sql`。
- 当前主环境迁移头：`000076_provider_billing_context_trigger_table_guard.sql`。
- 当前公开 OpenAPI 基线：436 条路由。
- Core Compose 发布辅助入口：`pwsh -NoProfile -File scripts/deploy-commerce-release.ps1`，默认只执行 Prepare，不触碰运行环境；Internal Commercial 生产发行仍必须先按 `docs/commercial-assembly-release-contract.md` 完成双仓不可变装配。

## 历史完整验证记录

2026-07-23 在仓库根目录执行 `pnpm run release:check`，Commerce 发布候选完整门禁通过：

- `go vet`、`govulncheck`、`pnpm audit --audit-level moderate` 均通过；可调用代码和 Node 依赖未发现已知漏洞。
- Go 全仓、migration/seed/baseline、Web 单测/typecheck/lint、Compose config 全部通过，400 条 OpenAPI 路由与 Go 注册一致。
- `pwsh -NoProfile -File scripts/test-runtime-hardening.ps1 -CommerceOnly` 通过，覆盖 migration/baseline、真实数据库 API 幂等、ScriptUnit 换代、Sales Script Contract、Project Agent、Commerce Workflow、Gateway 请求合同和语言能力。
- Web production standalone 构建和 `pnpm run test:commerce:e2e` 通过，9 条 Chromium 用例全部成功。
- `000001-000057` 在隔离 PostgreSQL 完成 Up/Down/Up，并与 consolidated baseline 等价。
- 全部 app profile Docker 镜像构建成功；授权部署窗口内又完成主环境 v44→v57 迁移和受影响服务重建。
- Provider mock contract 已验证商品引用、模型/Profile、语言、幂等键、画幅、质量和输出结构。
- 真实供应商 smoke 已完成：3 张参考图和 3 个镜头视频成功，失败尝试按镜头隔离重试，运行态浏览器验收通过。证据位于 `tmp/commerce-real-provider-smoke-20260723-103918.json` 和 `tmp/commerce-real-provider-smoke-final-20260723-204053.json`。

## 数据库迁移策略

`db/migrations/000001-000076` 已经写入主环境数据库的版本与内容 hash，必须保持不可变。不得为了缩短目录而重编号、改写或删除这些文件，否则已部署数据库会拒绝启动，也会破坏 Provider 配置、Commerce 生产记录和历史审计链。后续 schema 变更必须使用新的前向迁移，并保持 migration bundle、consolidated baseline 和隔离数据库等价验证一致。

整合产物位于：

- `db/baselines/current/consolidated-up.sql`：按版本顺序合并全部 Up 段。
- `db/baselines/current/manifest.json`：记录迁移头、文件名、逐文件 SHA-256 和整合文件 SHA-256。

生成与校验：

```powershell
go run ./cmd/cineweave-migration-bundle generate
go run ./cmd/cineweave-migration-bundle verify
pwsh -NoProfile -File scripts/test-migrations.ps1
```

隔离迁移测试会分别执行完整迁移链和整合基线，并比较 public schema 的表、列、约束、索引、函数和触发器。整合基线当前只用于发布审计和空库等价性验证；现有环境仍由 `cmd/cineweave-migrate` 升级。

## 一键发布检查

```powershell
$ErrorActionPreference = 'Stop'
pnpm run release:check
```

默认检查内容：

1. Go 工具链版本与跟踪文件密钥扫描。
2. `go vet`、`govulncheck`、完整 pnpm dependency audit。
3. 全仓测试、OpenAPI/路由检查、Web typecheck/lint。
4. Web production build和 Commerce Chromium E2E。
5. Compose config、隔离迁移 Up/Down/Up 与整合基线等价性。
6. 全部 app profile 镜像构建。

可选参数：

```powershell
$ErrorActionPreference = 'Stop'
pwsh -NoProfile -File scripts/release-check.ps1 -SkipMigrationIntegration
pwsh -NoProfile -File scripts/release-check.ps1 -SkipCommerceBrowserE2E
pwsh -NoProfile -File scripts/release-check.ps1 -SkipImageBuild
pwsh -NoProfile -File scripts/release-check.ps1 -RunCommerceRealProviderSmoke
pwsh -NoProfile -File scripts/release-check.ps1 -CheckProviderDrain
pwsh -NoProfile -File scripts/release-check.ps1 -RequireClean
pwsh -NoProfile -File scripts/release-check.ps1 -RequireClean -ReleaseId '<immutable-release-id>' -EvidencePath 'tmp/release-prepare-evidence.json'
```

完整检查会把每一步的开始时间、结束时间、耗时、结果及已构建 CineWeave 镜像的 `sha256` ID 原子写入 Prepare evidence。Evidence 必须位于仓库外或 Git 已忽略的目录（默认 `tmp/`）。Deploy 只接受与当前 Core commit、Release ID、干净工作树和本地镜像 ID 完全一致且未过期的成功 evidence，不能通过参数跳过。

首次执行浏览器 E2E 前安装固定版本 Chromium：

```powershell
$ErrorActionPreference = 'Stop'
pnpm --filter @cineweave/web exec playwright install chromium
```

`-RunCommerceRealProviderSmoke` 不读取仓库密钥，只读取 `CINEWEAVE_SMOKE_ACCESS_TOKEN`、`CINEWEAVE_SMOKE_ORGANIZATION_ID`、`CINEWEAVE_SMOKE_PROJECT_ID` 和 `CINEWEAVE_SMOKE_SCRIPT_UNIT_ID` 环境变量，并仍要求脚本内部的 `-ConfirmProviderSpend`。普通 CI 不运行任何计费供应商调用。

## 部署窗口

Core Compose 常规发布拆成 Prepare、Deploy、Finalize 三阶段。Prepare 完成测试、浏览器 E2E、迁移往返和 app profile 镜像构建，整个阶段不冻结 Provider、不迁移数据库，也不替换容器。Commercial 组合发行需要以相同不可变 Release ID 绑定 Core/Commercial commit、装配证据和最终 Manifest，不能把下列 Core Compose 命令当成 Commercial 装配替代品：

```powershell
$ErrorActionPreference = 'Stop'
$releaseId = (git rev-parse HEAD).Trim()
pwsh -NoProfile -File scripts/deploy-commerce-release.ps1 `
  -Phase Prepare `
  -ReleaseId $releaseId `
  -PrepareEvidencePath 'tmp/release-prepare-evidence.json'
```

Prepare 通过后再进入维护窗口。Deploy 会重新校验 evidence 与当前源码身份，完成 drain、冻结、快照，然后直接使用 Prepare 已构建的镜像执行迁移和容器切换，不会在停写窗口内重复测试或构建：

```powershell
$ErrorActionPreference = 'Stop'
pwsh -NoProfile -File scripts/deploy-commerce-release.ps1 `
  -Phase Deploy `
  -ReleaseId $releaseId `
  -PrepareEvidencePath 'tmp/release-prepare-evidence.json' `
  -ConfirmMainEnvironmentMigration
```

部署阶段默认拒绝执行主环境迁移。快照精确保护账号、凭据、凭据-模型可用性、模型能力、Profile/Binding、Provider 目录和能力预设；历史 Provider Request/Call/Task/Test、成本、能力证明与模型删除墓碑按行验证既有 ID 和内容哈希，同时允许发布后追加新记录。迁移开始后任一步骤失败，API 会保持 Provider 配置冻结，必须人工排查后再恢复；全部步骤通过才恢复发布前的冻结状态。

部署成功后，通过 UI 或 API 创建 Commerce 验收项目、商品、脚本单元和分镜，再配置短期令牌与目标身份：

```powershell
$ErrorActionPreference = 'Stop'
$releaseId = (git rev-parse HEAD).Trim()
$env:CINEWEAVE_SMOKE_ACCESS_TOKEN = '<short-lived-access-token>'
$env:CINEWEAVE_SMOKE_ORGANIZATION_ID = '<organization-uuid>'
$env:CINEWEAVE_SMOKE_PROJECT_ID = '<commerce-project-uuid>'
$env:CINEWEAVE_SMOKE_SCRIPT_UNIT_ID = '<script-unit-uuid>'
pwsh -NoProfile -File scripts/deploy-commerce-release.ps1 -Phase Finalize -ReleaseId $releaseId -ShotCount 3
```

零费用预检通过后，执行已授权的三镜头真实供应商付费 smoke：

```powershell
$ErrorActionPreference = 'Stop'
$releaseId = (git rev-parse HEAD).Trim()
pwsh -NoProfile -File scripts/deploy-commerce-release.ps1 -Phase Finalize -ReleaseId $releaseId -RunPaidSmoke -ConfirmProviderSpend -ShotCount 3
```

`-Phase Full` 会按 Prepare → Deploy → Finalize 顺序一次执行，适用于数据库中已经存在可复用 Commerce 验收项目的升级。三阶段都绑定同一份 evidence；脚本不提供跳过 Prepare 的开关，也不会把访问令牌或供应商密钥写入仓库。

随后验证：

- `http://localhost:19288/readyz`
- `http://localhost:19285`
- 登录、组织切换、供应商/模型 CRUD。
- 小说导入到分集剧本、资产、分镜、镜头图、镜头视频和成片的主链路。
- 任务活动实时状态、失败重试、取消与页面刷新恢复。
- 带货项目商品与多脚本、单元独立分镜/视频/成片和脚本单元切换隔离。

Commerce 主环境升级后，使用短期访问令牌执行真实 Provider smoke：

```powershell
$env:CINEWEAVE_SMOKE_ACCESS_TOKEN = '<short-lived-access-token>'
$env:CINEWEAVE_SMOKE_ORGANIZATION_ID = '<organization-uuid>'
$env:CINEWEAVE_SMOKE_PROJECT_ID = '<commerce-project-uuid>'
$env:CINEWEAVE_SMOKE_SCRIPT_UNIT_ID = '<script-unit-uuid>'
pwsh -NoProfile -File scripts/smoke-commerce-real-provider.ps1 -PreflightOnly -ShotCount 3
pwsh -NoProfile -File scripts/smoke-commerce-real-provider.ps1 -Stage full -ShotCount 3 -RetryFailedOnce -ConfirmProviderSpend
```

先通过 `-PreflightOnly` 零费用校验项目、模板/模型能力、ScriptUnit、分镜和语言身份，再运行付费阶段。需要验证非中文原生音频时再加 `-RequireNonChineseTargetLanguage`。证据写入 `tmp/commerce-real-provider-*.json`，包含业务/运行/产物和 Provider provenance ID，不包含访问令牌、Prompt、签名 URL 或媒体字节。

## 发布阻断条件

- 任一安全扫描、测试、迁移等价性、生产构建或健康检查失败。
- Provider 配置快照前后不一致。
- 存在运行中的 Provider 异步任务或无法终结的 workflow。
- OpenAPI 与路由不一致，或 Web 出现未汉化内部状态。
- 工作树包含本地密钥、数据库 dump、日志或临时可执行文件。
