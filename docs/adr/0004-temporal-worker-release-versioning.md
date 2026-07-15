# ADR 0004：Temporal Worker 不可变版本与蓝绿发布

状态：已接受
日期：2026-07-14

## 背景

当前 Worker Versioning 默认启用，但所有 Compose 构建使用固定 `cineweave-dev` Build ID，且 Worker 启动后自动晋级为 Current Version。不同代码可能被 Temporal 视为同一版本，旧容器重启也可能改变路由。

## 决策

- 每个发布使用不可变 release ID 作为 Build ID，生产环境禁止默认值。
- Worker 启动只注册 Deployment Version，不负责 ramp 或 promotion。
- 使用独立 `cmd/temporal-release` 执行注册检查、导流、晋级、排空和回滚。
- Agent Workflow 和长生产 Workflow 使用不同 task queue 与版本行为。
- 长生产 Workflow 使用 Pinned，并保留旧 Worker 直到对应版本不可达。
- Worker release 采用独立 Compose project，并加入共享 external internal network。
- Temporal schema 由显式 one-shot service 管理，生产不依赖 auto-setup 隐式升级。
- 旧 Worker 排空期间，数据库和 Provider Gateway 内部协议至少保持 N-1 运行时兼容。

## 后果

- 发布会临时并存两套 Worker，增加少量资源占用。
- Workflow 代码修改需要 patch/version 和 history replay 测试。
- 不兼容旧业务数据仍被允许，但不能以此破坏正在运行的旧 Worker。
