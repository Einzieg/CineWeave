# ADR 0002：数据库迁移与系统 Seed 分离

状态：已接受
日期：2026-07-14

## 背景

现有 SQL 文件自行写入 `schema_migrations`，不同迁移的 up/down 行为不一致。Prompt、模型目录和 Toonflow 手册也被放进 schema migration，导致迁移体积大、回滚含义混乱且无法可靠判断数据库版本。

## 决策

- 使用固定版本的 `github.com/pressly/goose/v3` 作为唯一 schema migration 引擎。
- 开发阶段不兼容旧迁移账本，重建应用数据库并建立新的 `000001/000002` 基线。
- 迁移通过 `go:embed` 编入应用镜像，由 `cmd/cineweave-migrate` 执行。
- Goose 独占版本表；业务 SQL 不得直接修改迁移账本。
- 在独立 `cineweave_migrations` schema 中记录版本、方向、内容 hash、耗时、release ID 和失败摘要；发现已应用版本内容漂移时拒绝启动。
- Prompt Registry、Provider Catalog、模型能力、项目手册和 RBAC 默认值由 `cmd/cineweave-seed` 幂等加载。
- 生产环境禁止 destructive down；生产回滚使用前向修复和 expand/contract。

## 后果

- T2 已通过受控快照后重建开发数据库完成切换。
- 旧迁移文件不再是运行时输入，但可通过 Git 历史审计。
- 系统内置内容可以独立于 schema 版本更新，并通过稳定 key 和 content hash 管理。
