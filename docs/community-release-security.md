# Community Edition 独立构建与泄漏审计

## 1. 目的与边界

本门禁证明 Community Edition（CE）可以只使用公共 Core 源码构建，并阻止私有商业实现、商业迁移、内部发布签名材料、管理凭据和本地构建产物进入公共历史或发行物。

它不替代以下操作控制：

- 公共仓库可见性、分支保护、制品签名和 Registry 发布操作。
- Internal Commercial Assembly 的独立装配与商业迁移审计。

技术审计通过只表示候选满足机器门禁，不会自动执行公开发布。

## 2. 门禁覆盖范围

规则由 `packages/edition/ce-release-policy.v1.json` 统一维护，执行器为 `scripts/ce_release_audit.py` 和 `scripts/check-ce-release.ps1`。完整门禁依次验证：

1. 对本地 `refs/heads`、`refs/remotes` 和 `refs/tags` 下所有可达对象执行完整扫描，包括 blob、commit message 和 annotated tag，不只检查当前工作树；Codex 等开发工具的内部快照 ref 不属于可发布历史，只有同一对象进入正式分支、远端分支或 tag 时才阻断。
2. 从固定 Git tree 生成不可变源码归档，检查禁止路径、私有模块引用、Secret 和生成物。
3. 在归档解压目录中使用 `community` build tag 执行全量 Go 测试、构建全部 main package，并扫描所有二进制。
4. 分别使用 `CINEWEAVE_EDITION=cloud` 和遗留 `enterprise` 值启动 CE API 与 Agent Worker，要求均在连接数据库前以 `feature_not_compiled` 失败。
5. 在归档目录中独立安装依赖并构建 Web，逐文件扫描 chunk、server bundle 和 source map。
6. 校验 Compose build service 与策略矩阵无遗漏，使用 digest 固定的基础镜像构建 12 个第一方镜像，并要求 `org.cineweave.edition=community` 标签存在。
7. 解析 `docker image save` 的 `manifest.json`，展开每一个声明的 OCI/Docker layer，扫描层内路径和内容。`layers=0` 会 fail-closed。
8. 为每个镜像生成 SPDX JSON SBOM，并扫描全部 SBOM。
9. 输出包含源码归档 hash、策略 hash、工具版本、镜像 ID、镜像归档 hash 和 SBOM hash 的 JSON 证据。

source map 不因存在而自动失败；它与其他 Web 产物使用同一私有代码和 Secret 规则逐文件扫描，并在日志中记录数量。

## 3. 发布前准备

完整历史扫描只覆盖本地已经取得的引用。公共发布前必须先取得公共 remote 的全部 heads 和 tags：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
git fetch --prune --tags origin '+refs/heads/*:refs/remotes/origin/*'
git status --short
git diff --check
```

工作树必须为空。不要把 `.env`、许可证、凭据导出、数据库备份、登录记录或私有 Overlay 放入公共 checkout。

运行环境需要：

- PowerShell 7
- Python 3
- Go
- Node.js 与 pnpm
- Docker Engine/BuildKit
- `docker sbom` 插件

脚本会为旧版 `docker-sbom 0.6` 显式选择 Docker API 1.44，以兼容要求最低 API 1.44 的当前 Docker Desktop；插件不存在或 SBOM 生成失败时，完整门禁直接失败。

## 4. 正式 CE 发布审计

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
pnpm run release:ce
```

`release:ce` 的固定行为：

- 要求工作树干净，只使用 `HEAD`。
- 运行历史、归档、Go、Web、镜像 layer 和 SBOM 全部门禁。
- 将证据复制到 `tmp/ce-release-audit.json`。
- 保留临时源码归档、镜像归档、SBOM 和已审计的 `cineweave-ce-audit-*` 镜像标签。

后续签名、重标记和推送必须使用证据文件记录的精确 image ID；不得重新构建后宣称是同一批已审计镜像。保留物的临时根路径会写入命令输出，发布证据应复制到 Git 之外的不可变制品存储。

## 5. 开发期验证与 CI

开发期可以审计未提交工作树，但该结果不能作为正式发布证据：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
pwsh -NoProfile -File scripts/check-ce-release.ps1 `
  -IncludeWorkingTree `
  -EvidencePath tmp/ce-release-audit-working-tree.json
```

`-IncludeWorkingTree` 使用独立 `GIT_INDEX_FILE` 生成临时 tree，不修改真实 Git index。

常规 CI 使用完整 Git history、源码归档、Go 和 Web 独立构建门禁，并上传 `tmp/ce-release-audit-ci.json`。为控制每次提交的耗时，CI 快速门禁跳过镜像；任何公共 Release 仍必须额外运行不带 `-SkipImageBuild` 的完整 `release:ce`。

### 5.1 CE 全新安装工程验收

发行泄漏审计之外，开发期还必须证明公共 Core 能在没有私有仓库、Commercial 运行配置和 New API 管理凭据时从空库完整启动：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
pnpm run test:ce:fresh
```

该门禁会：

1. 为 PostgreSQL、MinIO、Compose 网络和所有宿主端口创建随机隔离资源，不停止或重建已有 `cineweave` 栈。
2. 清除 Commercial、New API 管理和 Billing 环境变量，强制使用 `community` Edition 与唯一 Release ID。
3. 从当前 Core 源状态构建完整 app profile，并等待 API、Web、Realtime、Provider Gateway、四类 Worker、Event Publisher 及基础设施共 14 个长期服务健康。
4. 校验容器不存在商业服务或商业凭据环境变量，`/api/system/edition` 只报告 Community，商业计费路由返回 404。
5. 在全新 migration 空库上执行零费用 `TestWorkflowGatewayIntegration` 文本生产链路，要求 Provider mock 成功且不产生真实计费调用。
6. 无论成功或失败都删除该随机 Compose 项目、网络和卷；`-KeepStack` 仅用于显式诊断。

该命令证明当前源状态的工程可安装性，不执行公开发布。正式 CE Release 仍必须使用干净不可变 commit、取得全部 public remote 引用，并通过第 4 节的完整历史、源码与依赖清单、layer 和 SBOM 门禁。

## 6. 规则变更

禁止为使构建“变绿”而增加宽泛目录白名单。每个例外必须：

1. 精确到单个已知文件或明确扫描 scope。
2. 说明为什么命中不是 Secret 或私有实现。
3. 增加一个误报回归和一个真实泄漏正向回归。
4. 重新执行完整发行门禁。

扫描器只输出规则 ID 和文件路径，不输出匹配到的 Secret 值。

## 7. 发现泄漏后的处理

任一门禁失败时：

1. 立即停止公开推送、镜像发布和下载链接开放。
2. 若内容可能是真实凭据，先在上游撤销或轮换，不等待 Git 历史清理完成。
3. 定位泄漏首次进入的 commit、tag、归档和镜像 digest，登记所有受影响制品。
4. 删除未发布制品；已发布制品标记撤回并通知相关方。
5. Git 历史重写必须经过单独评审，不能只删除当前工作树文件。
6. 清理后重新 fetch 全部引用，并重新运行完整门禁。

## 8. 安全清理

发布完成或取消后，只按证据文件中的精确标签清理审计镜像：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$evidence = Get-Content -LiteralPath 'tmp/ce-release-audit.json' -Encoding UTF8 -Raw |
  ConvertFrom-Json
foreach ($image in $evidence.images) {
  if (-not $image.tag.StartsWith('cineweave-ce-audit-', [System.StringComparison]::Ordinal)) {
    throw "拒绝清理非审计镜像：$($image.tag)"
  }
  docker image rm $image.tag
}
```

不得使用通配符删除运行中的 CineWeave 镜像。源码归档、SBOM 和证据的删除服从发布证据保留策略。
