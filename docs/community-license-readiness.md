# Community Core 许可证就绪与法律阻断清单

本文记录 CineWeave Community Core 公开发行前的工程证据和法律阻断。它不是法律意见，也不选择、授予或修改任何软件许可证。

## 当前结论

截至 2026-07-29，只读核验确认 `Einzieg/CineWeave` GitHub 仓库是 public，默认分支是 `main`。仓库根目录仍没有 `LICENSE`、`NOTICE`、版权声明、商标政策、贡献政策或 CLA。源码公开可见不等于公众已经获得复制、修改或再分发授权，因此当前状态不能被标记为“CE 许可证已发布”，也不能据此宣称公共 Core 已获准进入同一主体的内部 Commercial 组合。

工程侧已经提供可重复清单，但以下决定必须由有资质的律师和权利人完成：

1. 确认所有历史贡献的版权归属、雇佣/委托关系，以及公共 AGPL 发行和同一主体内部 Commercial 组合所需的授权链。
2. 确认 CE 使用的准确 SPDX 表达式和许可证正文版本。
3. 确认内部 Commercial 使用权、CLA/DCO、NOTICE、第三方归属和商标政策；当前范围不要求客户商业软件许可。
4. 审核强/弱 copyleft、数据/文档许可证、二进制资产和容器镜像的组合与交付义务。
5. 生成受控法律事项编号或签名批准记录；公共仓库只保存最小批准元数据，不保存律师特权内容。

在这些事项完成前，不应擅自复制一份 AGPL 模板到根目录，也不能勾选目标文档中的法律验收项。

## 可重复工程清单

执行：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
python scripts/audit-source-licensing.py `
  --output tmp/source-licensing-audit.json
```

报告会固定并哈希以下证据：

- 全部 Git ref 的 commit、作者身份哈希、贡献时间范围、DCO trailer 和签名状态。
- 根目录法律文件是否存在及其 SHA-256。
- `go mod download` 后的全部 Go module、主许可证/补充许可证/NOTICE 文件和内容 hash。
- `pnpm licenses list --json --prod` 的生产 Node 依赖、版本和许可证表达式。
- Dockerfile 与 Compose 使用的外部基础镜像及 digest 固定状态。
- Git 跟踪的图片、字体、音视频、压缩包和其他二进制资产 hash。

报告默认允许工程清单完成，但状态保持 `blocked_legal_review`。发布门禁必须显式使用：

```powershell
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
python scripts/audit-source-licensing.py `
  --output tmp/source-licensing-audit.json `
  --approval <controlled-source-license-approval.v2.json> `
  --require-legal-approval
```

批准文件必须符合 `packages/edition/source-license-approval.schema.json`，绑定当前 `inventorySha256`，并由 `reviewerRole=qualified_counsel` 的受控记录确认公共 AGPL 许可证、同一主体内部 Commercial 组合使用权、贡献授权、第三方 NOTICE 和商标政策。批准文件作为受控 CI/法务输入，不提交到公共 Core，避免其自身新增 commit 造成循环 hash；Release Manifest 只记录批准记录 hash 和不可变证据引用。任何依赖、资产、Git 历史或法律文件变化都会改变 inventory hash，使旧批准自动失效。

## 当前需重点复核的类别

每次候选发行都以生成报告为准；当前工程清单已经识别出至少以下非纯宽松类别：

- Go 依赖中存在 AGPL 许可证表达式。
- Node 生产依赖中存在 `Apache-2.0 AND LGPL-3.0-or-later`。
- Node 数据依赖中存在 `CC-BY-4.0`。
- Go 依赖文档中存在 `CC-BY-SA-4.0`。
- 存在单独的 logo/资产授权文件，自动分类器只记录 hash，要求人工确认。
- Git 跟踪的二进制资产需要逐项建立来源和再分发证据。

这些类别不自动等于“禁止使用”，也不自动等于“适合公共 AGPL 发行或内部 Commercial 组合”；它们必须结合链接方式、网络交互、分发形式、修改、NOTICE、源代码提供方式和内部运行范围由律师判断。

## 贡献治理切换

律师确认策略后再执行以下切换：

1. 将经批准的 `LICENSE`、`NOTICE`、版权、商标和贡献政策加入公共仓库。
2. 启用经批准的 CLA 或 DCO 流程，并明确机器人、员工和外部贡献者的处理规则。
3. 对历史提交取得必要的追认、转让或许可；不能用新政策自动覆盖过去权利缺口。
4. 生成与当前 inventory hash 绑定的批准元数据。
5. 在公共发布流水线启用 `--require-legal-approval`。
6. 将第三方 NOTICE/SBOM 和对应 hash 写入 CE 与组合 Commercial Release Manifest。

如果律师要求替换依赖、移除资产或改变分发方式，应先修改源码与构建，再重新生成清单和批准；不得手工复用旧报告 hash。
