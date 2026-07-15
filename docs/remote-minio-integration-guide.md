# 远程 MinIO 跨服务器项目接入指南

## 1. 文档目的

本文面向部署在其他服务器上的项目，说明如何接入 `137.175.67.220` 上的 MinIO S3 兼容对象存储。

本文覆盖：

- MinIO 管理员需要准备的 bucket、应用账号和权限策略。
- 接入项目需要配置的 endpoint、region、bucket 和凭据。
- 服务端访问与浏览器预签名 URL 的差异。
- 连通性、鉴权、上传下载和预签名 URL 的验收方法。
- HTTPS、防火墙、CORS、密钥管理和常见故障。

本文不会记录任何真实 Access Key、Secret Key 或 MinIO root 凭据。

## 2. 当前服务信息

| 项目 | 当前值 |
| --- | --- |
| MinIO 服务器 | `137.175.67.220` |
| 当前 S3 API | `http://137.175.67.220:19290` |
| 健康检查 | `http://137.175.67.220:19290/minio/health/ready` |
| S3 Region | `us-east-1` |
| 寻址方式 | Path-style |
| MinIO Console | 未映射到公网 |
| 当前 CineWeave bucket | `cineweave` |

截至 2026-07-14，MinIO 容器健康，公网健康检查返回 HTTP 200。当前 `19290` 直接暴露在公网并使用明文 HTTP，只适合临时联调；正式项目必须优先改用 HTTPS 域名或加密的私有网络。

## 3. 推荐接入模型

每个项目使用独立的：

- bucket，例如 `other-project-prod`；
- MinIO 应用账号；
- Access Key 和 Secret Key；
- bucket-scoped 读写策略；
- 密钥管理记录和轮换周期。

不要把 MinIO root 凭据交给业务项目，也不要让无关项目复用 CineWeave 的 `cineweave` bucket 或应用账号。

推荐的正式访问入口为：

```text
https://s3.example.com
```

推荐链路：

```text
目标项目后端 ──HTTPS──> 反向代理 ──> MinIO :9000
浏览器/外部供应商 ──预签名 HTTPS URL──> 反向代理 ──> MinIO :9000
```

如果项目只有后端服务器访问对象存储，可通过站点到站点 VPN、WireGuard 等私有网络访问，并在防火墙中只允许目标服务器来源地址。

### 3.1 共享实例的隔离边界

独立 bucket、账号和策略只提供 S3 IAM 层面的逻辑隔离，不提供以下隔离：

- MinIO root 或宿主机管理员仍可访问全部项目数据；
- 所有项目共享磁盘容量、磁盘 I/O、网络带宽和 MinIO 进程资源；
- 单个 MinIO 实例、宿主机或数据盘故障会同时影响所有项目；
- 当前单节点目录存储不等同于高可用、异地备份或灾难恢复；
- bucket 版本控制、生命周期、配额、监控和备份不会因创建账号而自动生效。

MinIO 管理员必须另行负责容量告警、配额或生命周期策略、备份、恢复演练、监控和升级。需要强故障隔离、独立合规边界或高可用保证的项目，应使用独立 MinIO 实例或专用对象存储集群。

## 4. 接入前需要申请的信息

接入项目应向 MinIO 管理员提供：

| 字段 | 示例 | 说明 |
| --- | --- | --- |
| 项目标识 | `other-project` | 用于 bucket、账号和策略命名 |
| 环境 | `dev` / `staging` / `prod` | 不同环境建议使用不同 bucket 和账号 |
| 是否需要写入 | `true` | 只读项目不应获得 Put/Delete 权限 |
| 是否需要删除 | `false` | 非必要不要授予 DeleteObject |
| 是否浏览器直传 | `true` | 决定是否需要 bucket CORS |
| 前端 Origin | `https://app.example.com` | CORS 必须使用精确来源 |
| 是否生成预签名 URL | `true` | 决定 public endpoint 配置 |
| 目标服务器公网 IP | `203.0.113.10` | 后端专用接入时可用于防火墙白名单 |

MinIO 管理员应返回：

- S3 endpoint；
- S3 public endpoint；
- region；
- bucket；
- 应用 Access Key；
- 应用 Secret Key；
- 是否强制 path-style；
- 凭据有效期或轮换要求。

Secret Key 必须通过密码管理器或其他受控渠道交付，不得写入工单、聊天记录、仓库或镜像。

## 5. MinIO 管理员准备工作

### 5.1 创建独立 bucket

为不同项目和环境创建独立 bucket，例如：

```text
other-project-dev
other-project-prod
```

仅修改业务项目中的 `S3_BUCKET` 不会自动创建 bucket。

### 5.2 创建独立应用账号

为项目生成独立 Access Key 和高强度 Secret Key。应用账号不得具有 MinIO 管理权限，也不得使用 root 凭据。

### 5.3 创建 bucket-scoped 策略

以下示例允许项目列举 bucket，并读、写、删除 `other-project-prod` 中的对象。若项目不需要删除能力，应移除 `s3:DeleteObject`。

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetBucketLocation",
        "s3:ListBucket",
        "s3:ListBucketMultipartUploads"
      ],
      "Resource": [
        "arn:aws:s3:::other-project-prod"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:AbortMultipartUpload",
        "s3:ListMultipartUploadParts"
      ],
      "Resource": [
        "arn:aws:s3:::other-project-prod/*"
      ]
    }
  ]
}
```

将该策略只附加给对应项目的应用账号。仓库中的 [现有策略模板](../deploy/remote-minio/cineweave-policy.json) 可作为结构参考，但其中的 `cineweave` 资源名必须全部替换。

只读账号应仅保留 `s3:GetBucketLocation`、`s3:ListBucket` 和 `s3:GetObject`；不要授予 `PutObject`、`DeleteObject`、`AbortMultipartUpload` 或 multipart 写入权限。

现有 [远程 MinIO Compose](../deploy/remote-minio/compose.yml) 固定使用 `cineweave-policy.json`、策略名 `cineweave-rw` 和 `cineweave` bucket。为其他项目配置权限时：

1. 复制为独立的 `<project>-policy.json`，不要覆盖 `cineweave-policy.json`。
2. 替换策略中的 bucket ARN。
3. 使用唯一策略名，例如 `other-project-prod-rw`。
4. 使用唯一应用账号，不要复用 CineWeave 账号。
5. 不要仅修改远端 `.env` 后重跑现有 bootstrap；这会造成 bucket 与硬编码策略不匹配。

以下命令供已安全配置 MinIO 管理别名的管理员使用。管理别名的 root 凭据应由受保护的 `mc` 配置或密钥管理系统提供，不得写入命令、脚本或历史记录。

```powershell
$ErrorActionPreference = 'Stop'

$minioAlias = 'admin'
$bucket = 'other-project-prod'
$policyName = 'other-project-prod-rw'
$policyPath = 'C:\secure\minio-policies\other-project-prod-policy.json'
$accessKey = $env:NEW_MINIO_ACCESS_KEY
$secretKey = $env:NEW_MINIO_SECRET_KEY

if (-not $accessKey -or -not $secretKey) {
    throw '请先通过受控密钥系统注入 NEW_MINIO_ACCESS_KEY 和 NEW_MINIO_SECRET_KEY'
}

mc mb --ignore-existing "${minioAlias}/${bucket}"
if ($LASTEXITCODE -ne 0) {
    throw '创建 bucket 失败'
}

mc admin user info $minioAlias $accessKey *> $null
$userExists = $LASTEXITCODE -eq 0
if ($userExists) {
    mc admin user enable $minioAlias $accessKey
} else {
    mc admin user add $minioAlias $accessKey $secretKey
}
if ($LASTEXITCODE -ne 0) {
    throw '创建或启用应用账号失败'
}

mc admin policy create $minioAlias $policyName $policyPath
if ($LASTEXITCODE -ne 0) {
    throw '创建项目策略失败'
}

mc admin policy attach $minioAlias $policyName --user $accessKey
if ($LASTEXITCODE -ne 0) {
    throw '绑定项目策略失败'
}
```

执行完成后，应清除临时环境变量并通过项目账号完成越权验证。

### 5.4 保持 bucket 私有

不要为了让浏览器或外部供应商下载对象而把 bucket 设置为 public。应由可信后端生成短期预签名 URL。

## 6. 接入项目配置

本文使用的 `S3_ENDPOINT`、`S3_PUBLIC_ENDPOINT`、`S3_BUCKET` 和 `S3_USE_PATH_STYLE` 是 CineWeave 和本文示例采用的项目配置名，不是 MinIO 或所有 AWS SDK 会自动读取的通用环境变量。接入项目必须在自己的配置层中把这些值显式映射到所用 SDK。

多数 AWS SDK 默认识别以下凭据变量：

```dotenv
AWS_ACCESS_KEY_ID=<PROJECT_ACCESS_KEY>
AWS_SECRET_ACCESS_KEY=<PROJECT_SECRET_KEY>
AWS_REGION=us-east-1
```

endpoint、public/presign endpoint 和 path-style 通常仍需在 SDK 客户端构造代码或框架配置中显式指定。不要在同一项目中维护两套不一致的 S3 凭据。

### 6.1 当前 HTTP 入口，仅用于临时联调

```dotenv
S3_ENDPOINT=http://137.175.67.220:19290
S3_PUBLIC_ENDPOINT=http://137.175.67.220:19290
S3_REGION=us-east-1
S3_BUCKET=other-project-prod
S3_ACCESS_KEY_ID=<PROJECT_ACCESS_KEY>
S3_SECRET_ACCESS_KEY=<PROJECT_SECRET_KEY>
S3_USE_PATH_STYLE=true
```

### 6.2 推荐的 HTTPS 正式配置

```dotenv
S3_ENDPOINT=https://s3.example.com
S3_PUBLIC_ENDPOINT=https://s3.example.com
S3_REGION=us-east-1
S3_BUCKET=other-project-prod
S3_ACCESS_KEY_ID=<PROJECT_ACCESS_KEY>
S3_SECRET_ACCESS_KEY=<PROJECT_SECRET_KEY>
S3_USE_PATH_STYLE=true
```

`.env` 只能保存在目标服务器或密钥管理系统中，不得提交到 Git。

### 6.3 Docker Compose 示例

目标项目位于另一台服务器时，不要使用 `minio:9000`、`localhost`、`host.docker.internal` 或 CineWeave 的 Docker network。

```yaml
services:
  api:
    image: your-project-api
    environment:
      S3_ENDPOINT: ${S3_ENDPOINT}
      S3_PUBLIC_ENDPOINT: ${S3_PUBLIC_ENDPOINT}
      S3_REGION: ${S3_REGION:-us-east-1}
      S3_BUCKET: ${S3_BUCKET}
      S3_ACCESS_KEY_ID: ${S3_ACCESS_KEY_ID}
      S3_SECRET_ACCESS_KEY: ${S3_SECRET_ACCESS_KEY}
      S3_USE_PATH_STYLE: ${S3_USE_PATH_STYLE:-true}
```

不要把真实密钥直接写进 Compose YAML 或 Dockerfile。Compose 变量来自部署服务器上的 `.env`、Docker secret 或外部密钥管理系统。

上述 Compose 只负责把环境变量注入容器；业务程序仍必须读取这些变量并配置对应 S3 SDK。

## 7. Endpoint 语义

### 7.1 S3_ENDPOINT

`S3_ENDPOINT` 用于后端执行 HeadBucket、ListObjects、GetObject、PutObject 和 DeleteObject 等 S3 API 请求。

目标项目位于另一台服务器时，应填写目标服务器实际可访问的公网 HTTPS 地址或私网地址，不能填写 Docker 内部服务名。

### 7.2 S3_PUBLIC_ENDPOINT

`S3_PUBLIC_ENDPOINT` 用于生成浏览器或外部供应商访问的预签名 URL，必须是最终访问者能够解析并连接的地址。

预签名 URL 的签名包含请求 Host。以下替换通常会导致 `SignatureDoesNotMatch`：

- 使用 `137.175.67.220:19290` 签名后改成另一个域名访问；
- 使用 `minio:9000` 签名后交给公网浏览器；
- 反向代理改写对象路径或 Host；
- 签名使用 HTTPS，但客户端绕过代理改用其他 Host。

如果项目使用的 SDK 不支持独立的 public endpoint，应为普通 S3 客户端和预签名客户端都使用最终的 HTTPS 域名。

### 7.3 SDK 参数映射

| 配置概念 | 常见 SDK 映射 |
| --- | --- |
| Access Key | 标准变量 `AWS_ACCESS_KEY_ID` 或 SDK static credentials |
| Secret Key | 标准变量 `AWS_SECRET_ACCESS_KEY` 或 SDK static credentials |
| Region | 标准变量 `AWS_REGION`，本服务使用 `us-east-1` |
| S3 endpoint | SDK 的 `endpoint`、`endpoint_url` 或 `BaseEndpoint` |
| Public endpoint | 单独构造的 presign client endpoint；不支持分离时使用最终公网 endpoint |
| JavaScript v3 path-style | `forcePathStyle: true` |
| Go v2 path-style | `UsePathStyle = true` |
| boto3 / botocore path-style | `s3.addressing_style = path` |
| Java path-style | `pathStyleAccessEnabled(true)` |

使用 IP 地址时必须采用 path-style，不能依赖 `bucket.137.175.67.220` 形式的虚拟主机寻址。

### 7.4 预签名 URL 安全边界

预签名 URL 是限定对象、方法和有效期的短期 bearer credential。任何获得完整 URL 的人，在过期前都可能执行其授权操作。

- TTL 应按业务需要设置为尽可能短，避免生成长期 URL；
- 不要把完整 query、`X-Amz-Signature` 或预签名 URL 写入日志、工单、分析系统或聊天记录；
- 不要把预签名 URL 当作永久对象地址长期缓存；
- URL 必须限定到单个对象和明确方法，不能替代 bucket 权限隔离；
- 预签名 PUT 如果签入 `Content-Type`、checksum 或其他 header，实际上传请求必须使用完全一致的 header；
- 预签名 URL 必须通过 HTTPS 或加密私网传输。

## 8. 网络和 HTTPS 要求

以下是 2026-07-14 的一次性现场检查结果，不是持续有效的安全保证；每次上线、扩容或网络变更前都必须重新核实：

- `19290` 监听 `0.0.0.0` 和 `[::]`；
- UFW 未启用；
- MinIO API 当前通过明文 HTTP 对公网开放；
- Console `9001` 未对公网开放。

如果该仓库会公开发布，应将具体监听、防火墙和服务器安全状态迁移到受限运维文档，只保留通用接入要求。

正式接入前应完成：

1. 为 MinIO 配置独立 HTTPS 域名和有效证书。
2. 让反向代理保留原始 Host，不改写 bucket/object 路径。
3. 允许 `GET`、`HEAD`、`PUT`、`POST`、`DELETE` 和 `OPTIONS` 中项目实际需要的方法。
4. 放行 `Authorization`、`Range`、`Content-Type` 和 `X-Amz-*` 请求头。
5. 为大文件调整请求体大小、读取超时和流式转发设置。
6. 将 MinIO 宿主端口限制为反向代理或可信网络可访问。
7. 保持 Console `9001` 不对公网发布。

如果只有一个固定后端服务器访问 MinIO，可以将 S3 API 限制为只允许该服务器公网 IP，或通过 VPN 使用私网地址。如果浏览器或第三方 AI 供应商也需要访问预签名 URL，则应公开 HTTPS 域名的 `443`，但 bucket 仍保持私有。

## 9. 连通性验证

以下命令使用跨平台 PowerShell 7 执行，不依赖 Windows 专有的 `Test-NetConnection`。

```powershell
$ErrorActionPreference = 'Stop'

$minioHost = '137.175.67.220'
$minioPort = 19290
$healthUrl = "http://${minioHost}:${minioPort}/minio/health/ready"

$tcpClient = [System.Net.Sockets.TcpClient]::new()
try {
    $connectTask = $tcpClient.ConnectAsync($minioHost, $minioPort)
    if (-not $connectTask.Wait(10000)) {
        throw "连接 MinIO TCP 端口超时：${minioHost}:${minioPort}"
    }
    $connectTask.GetAwaiter().GetResult()
    $tcpConnected = $tcpClient.Connected
} finally {
    $tcpClient.Dispose()
}

if (-not $tcpConnected) {
    throw "无法连接 MinIO TCP 端口：${minioHost}:${minioPort}"
}

$response = Invoke-WebRequest -Uri $healthUrl -Method Get -TimeoutSec 15
if ($response.StatusCode -ne 200) {
    throw "MinIO 健康检查失败，HTTP $($response.StatusCode)"
}

[PSCustomObject]@{
    TcpConnected = $tcpConnected
    HealthStatus = $response.StatusCode
}
```

健康检查只证明 MinIO 服务可达，不能证明 bucket、账号和权限正确。

## 10. S3 权限验收

接入方应根据实际授权档位使用项目 SDK 或 AWS CLI 完成验收：

- 只读：List/Get 成功，Put/Delete 返回 `AccessDenied`；
- 可写不可删：List/Get/Put 成功，Delete 返回 `AccessDenied`；
- 完整读写：List/Get/Put/Delete 均成功。

通用验收步骤：

1. `HeadBucket` 成功。
2. 具有写权限时，上传一个带唯一前缀的测试对象。
3. `HeadObject` 返回正确大小和 Content-Type。
4. 下载对象并校验内容或 checksum；只读账号使用管理员预置对象。
5. 生成短期预签名 GET URL，并从实际访问端下载成功。
6. 需要浏览器直传时，生成预签名 PUT URL 并从目标 Origin 上传成功。
7. 需要删除权限时，删除测试对象并确认不存在；不需要删除权限时，验证 DeleteObject 返回 `AccessDenied`。
8. 尝试访问其他项目 bucket，应返回 `AccessDenied`。

对于不可删除账号，测试对象应由管理员清理或由生命周期规则回收。

以下 AWS CLI 示例只验证 endpoint、凭据和 bucket 访问；上传、下载、删除和预签名验收仍需按上述权限档位执行。凭据应先由部署系统注入环境变量：

```powershell
$ErrorActionPreference = 'Stop'

if (-not $env:AWS_ACCESS_KEY_ID -or -not $env:AWS_SECRET_ACCESS_KEY) {
    throw '请先通过部署系统注入项目级 AWS_ACCESS_KEY_ID 和 AWS_SECRET_ACCESS_KEY'
}

$endpoint = 'http://137.175.67.220:19290'
$region = 'us-east-1'
$bucket = 'other-project-prod'

aws --endpoint-url $endpoint --region $region s3api head-bucket --bucket $bucket
if ($LASTEXITCODE -ne 0) {
    throw 'HeadBucket 验证失败'
}
```

验证完成后应从当前进程移除临时注入的凭据。

## 11. 浏览器直传和 CORS

浏览器通过跨域 `fetch`/XHR 读取响应、使用预签名 URL 直传，或由 JavaScript 处理跨域对象响应时，需要配置 MinIO bucket CORS。普通页面导航以及只用于展示的 `<img>`/`<video>` 通常不等同于 JavaScript 跨域读取；Canvas 像素读取等场景仍可能要求 CORS。纯后端 S3 访问不需要 CORS。

MinIO bucket CORS 与 CineWeave API 的 `CINEWEAVE_CORS_ORIGINS` 是两套独立配置，修改 API CORS 不会自动放行 MinIO 对象请求。

CORS 应遵循：

- `AllowedOrigin` 使用精确前端 Origin，不使用无约束通配符；
- 只允许实际需要的 `GET`、`HEAD`、`PUT` 或 `POST`；
- 允许预签名请求所需的请求头；
- 按需暴露 `ETag`；
- CORS 不能替代应用账号策略或预签名鉴权。

修改 CORS 后，应从真实前端域名完成一次 OPTIONS 预检和上传/下载验收。

## 12. 对象键约定

即使每个项目使用独立 bucket，也建议使用稳定的对象键结构：

```text
<environment>/<tenant-or-org>/<resource-type>/<resource-id>/<filename>
```

示例：

```text
prod/org-123/videos/video-456/source.mp4
```

对象键应避免：

- 用户输入直接拼接产生的 `../` 或路径混淆；
- 把 Access Key、用户隐私或数据库连接信息写进对象键；
- 依赖原始文件名作为唯一标识；
- 多项目共用相同前缀而没有权限隔离。

## 13. 常见故障

| 现象 | 常见原因 | 处理方法 |
| --- | --- | --- |
| 连接超时 | 端口、防火墙、路由或安全组未放行 | 检查 TCP `19290` 或 HTTPS `443` 连通性 |
| `AccessDenied` | 用户未绑定策略、bucket 不匹配、操作超出授权 | 核对账号、bucket ARN 和动作列表 |
| `NoSuchBucket` | bucket 尚未创建或名称配置错误 | 由 MinIO 管理员创建 bucket 并核对环境变量 |
| `SignatureDoesNotMatch` | endpoint/Host 被替换、path-style 未启用、代理改写路径 | 固定签名和访问域名，启用 path-style，检查代理 |
| `RequestTimeTooSkewed` | 项目服务器系统时间不准确 | 启用 NTP/chrony 并校准时钟 |
| 预签名 URL 后端可用、浏览器失败 | CORS 或浏览器访问不到 public endpoint | 检查 `S3_PUBLIC_ENDPOINT` 和 bucket CORS |
| 小文件成功、大文件失败 | 代理请求体限制、超时或 multipart 权限不完整 | 调整代理限制并补齐 multipart 权限 |
| URL 很快失效 | 预签名有效期过短或客户端缓存旧 URL | 调整有效期并按过期时间刷新 |

## 14. 上线验收清单

- [ ] 项目使用独立 bucket。
- [ ] 项目使用独立应用账号，不使用 root 凭据。
- [ ] 策略只允许目标 bucket 和必要动作。
- [ ] Secret Key 未进入 Git、镜像、日志或工单。
- [ ] 后端按授权档位完成 `HeadBucket`、上传、下载和条件化删除验收。
- [ ] 无权访问其他项目 bucket。
- [ ] 预签名 URL 使用最终可访问的域名。
- [ ] 预签名 URL 使用短 TTL，且日志会脱敏 query 和签名。
- [ ] 正式环境使用 HTTPS 或加密私有网络。
- [ ] 反向代理支持大文件、Range 和长连接传输。
- [ ] 浏览器直传场景完成 CORS 验收。
- [ ] 项目服务器系统时间已同步。
- [ ] 已重新核实 MinIO 监听、防火墙和健康状态，而非依赖历史快照。
- [ ] 管理员已确认容量、监控、备份、恢复、配额或生命周期责任。
- [ ] 项目接受共享实例的资源与故障域；否则使用独立实例。
- [ ] 已定义密钥轮换、停用和事故处置流程。

## 15. 仓库参考

- [远程 MinIO 部署说明](../deploy/remote-minio/README.md)
- [远程 MinIO Compose](../deploy/remote-minio/compose.yml)
- [CineWeave bucket 策略模板](../deploy/remote-minio/cineweave-policy.json)
- [CineWeave S3 环境变量示例](../.env.example)
- [Provider Gateway 对 public endpoint 的要求](provider-gateway.md)
