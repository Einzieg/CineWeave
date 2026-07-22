# CineWeave 正式发布准备

## 当前发布基线

- Go 最低工具链：`1.26.5`。
- Node.js：`24`；pnpm：`10.32.1`。
- PostgreSQL：`16`。
- 当前数据库迁移头：`000044_provider_model_attestation_history.sql`。
- 发布部署入口：`docker compose -f compose.yml --profile app up -d --build`。

## 最近一次完整验证

2026-07-22 在仓库根目录执行 `pnpm run release:check`，结果通过：

- `go vet`、全仓 Go 测试、Web 单测/typecheck/lint 和 production build 全部通过，lint 零警告。
- `govulncheck` 未发现代码可调用漏洞；`pnpm audit --audit-level moderate` 未发现已知漏洞。
- OpenAPI YAML 可解析，323 条公开路由与 Go 注册一致。
- `000001-000044` 在隔离 PostgreSQL 中完成 Up/Down/Up，逐版本迁移与 consolidated baseline 的 schema 等价检查通过。
- app profile 全部 Docker 镜像构建成功；该命令只构建镜像，没有重启当前运行服务。
- 当前运行栈全部 healthy，API `/readyz` 与 Web 登录页均返回 HTTP 200，主库迁移头为 44。
- Provider drain 检查结果为 0 个活动视频 workflow、0 个活动视频异步任务、0 个活动 lease。

## 数据库迁移策略

`db/migrations/000001-000044` 已经写入现有数据库的版本与内容 hash，必须保持不可变。不得为了缩短目录而重编号、改写或删除这些文件，否则已部署数据库会拒绝启动，也会破坏 Provider 配置和历史生产记录的审计链。

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
pnpm run release:check
```

默认检查内容：

1. Go 工具链版本与跟踪文件密钥扫描。
2. `go vet`、`govulncheck`、完整 pnpm dependency audit。
3. 全仓测试、OpenAPI/路由检查、Web typecheck/lint。
4. Web production build、Compose config。
5. 隔离迁移 Up/Down/Up 与整合基线等价性。
6. 全部 app profile 镜像构建。

可选参数：

```powershell
pnpm run release:check -- -SkipMigrationIntegration
pnpm run release:check -- -SkipImageBuild
pnpm run release:check -- -CheckProviderDrain
pnpm run release:check -- -RequireClean
```

## 部署窗口

正式部署前先确认 Provider 调用和异步任务已排空，并保存配置保护快照：

```powershell
pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode DrainCheck
pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode Snapshot -SnapshotPath tmp/provider-protection-before-release.json
docker compose -f compose.yml --profile app up -d --build
docker compose -f compose.yml --profile app ps
pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode Verify -SnapshotPath tmp/provider-protection-before-release.json
```

随后验证：

- `http://localhost:19288/readyz`
- `http://localhost:19285`
- 登录、组织切换、供应商/模型 CRUD。
- 小说导入到分集剧本、资产、分镜、镜头图、镜头视频和成片的主链路。
- 任务活动实时状态、失败重试、取消与页面刷新恢复。

## 发布阻断条件

- 任一安全扫描、测试、迁移等价性、生产构建或健康检查失败。
- Provider 配置快照前后不一致。
- 存在运行中的 Provider 异步任务或无法终结的 workflow。
- OpenAPI 与路由不一致，或 Web 出现未汉化内部状态。
- 工作树包含本地密钥、数据库 dump、日志或临时可执行文件。
