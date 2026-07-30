# Community 源码与依赖清单

公共 Core 候选使用可重复脚本生成源码、依赖、容器和二进制资产清单。该门禁
只检查仓库中可以机器验证的事实，不要求外部律师或法律顾问批准。

## 运行

```powershell
$ErrorActionPreference = 'Stop'
python scripts/audit-source-licensing.py `
  --output 'tmp\source-inventory.json'
```

正式 CE 候选：

```powershell
$ErrorActionPreference = 'Stop'
pwsh -NoProfile -File scripts/check-ce-release.ps1 `
  -RequireClean `
  -EvidencePath 'D:\release-evidence\cineweave-ce.json'
```

## 清单内容

- Git commit 与贡献者摘要；
- Go module 与 Node package 版本、许可证表达式和 lockfile hash；
- Compose 外部镜像及 digest 固定状态；
- 仓库内二进制资产的路径、大小和 SHA-256；
- `LICENSE`、`NOTICE`、`COPYRIGHT`、`TRADEMARKS.md` 等发行文件状态；
- 完整 inventory hash 与报告 hash。

缺少发行文件、依赖许可证未知、容器镜像未固定或二进制来源未记录时，报告返回
`attention_required`。修复源码或依赖后重新生成清单；不得手工修改 hash 或
伪造通过状态。单独生成清单时该状态仅用于开发期反馈；正式
`check-ce-release.ps1` 会使用 `--require-ready` 将其作为发行阻断。
