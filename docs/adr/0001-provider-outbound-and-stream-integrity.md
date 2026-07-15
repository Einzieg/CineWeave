# ADR 0001：Provider 安全出站与流完整性

状态：已接受
日期：2026-07-14

## 背景

Provider Gateway 会接收供应商返回的媒体 URL，并消费 OpenAI-compatible SSE。当前 URL 预校验与实际下载分别解析 DNS，SSE 又会把 `unexpected EOF` 当作正常结束，无法保证下载目标和文本输出完整可信。

## 决策

- 在 `internal/provider/outbound` 建立唯一的非可信媒体下载组件。
- 媒体域名只解析一次；连接只能使用已验证地址，并保留原始 Host、TLS SNI 和证书校验。
- 每次重定向重新应用 URL、DNS、端口和网络策略。
- 私网媒体默认拒绝，只允许供应商账号级精确 host/CIDR 白名单。
- 大型媒体边下载边计算 hash 并写入对象存储，不整体缓存在内存中。
- SSE 拆分为有界读取器、通用解码器和 Provider 协议终态适配器。
- `io.ErrUnexpectedEOF` 及传输中断不得提交部分输出。
- OpenAI-compatible 成功必须满足模型声明的 `done_marker`、`finish_reason` 或两者之一。
- 每个 delta 带 attempt 和 sequence；发生重试时不得把两个 attempt 的文本拼接为一个结果。

## 边界

管理员配置的 Provider API base URL 可以是内网地址，以支持 Ollama 和私有网关。public-only 网络策略只适用于供应商响应中的非可信媒体 URL。

## 后果

- 部分不发送明确终态的伪 OpenAI-compatible 服务会被判为不兼容，需要修正上游或显式能力配置。
- 私有媒体 CDN 需要管理员配置精确白名单。
- Provider Gateway 增加少量 DNS、连接和流状态机代码，但移除图片、视频、音频的重复下载逻辑。
