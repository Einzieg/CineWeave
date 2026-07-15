# ADR 0003：Workflow Start Outbox 与持久批任务

状态：已接受
日期：2026-07-14

## 背景

API 当前先插入 `workflow_runs`，再直接调用 Temporal。进程在双写之间退出会留下永久 queued 任务。资产批处理又由浏览器内存并发执行，刷新页面后无法恢复排队项和权威进度。

## 决策

- API 在同一数据库事务中写入 `workflow_runs` 和 `workflow_start_outbox`。
- Starter/Reconciler 使用确定性 Temporal workflow ID 启动任务，并修复超时 queued 记录。
- Temporal 是执行事实来源，`workflow_runs/workflow_node_runs` 是可查询投影。
- 资产 Prompt 和图片批处理使用父 Workflow 与每资产 child Workflow，默认并发 5。
- 单项失败聚合为 `partial_succeeded`，不使整个批次丢失成功结果。
- 失败重试创建关联的新 Workflow Run，不改写原任务历史。
- 前端通过 API 和 Realtime 查询服务端状态；Zustand 不保存权威任务队列。
- 资产写入使用 revision 和 expected revision，防止后台任务覆盖用户的新编辑。

## 后果

- 需要新增 outbox dispatcher、进度 revision 和批处理 API。
- 页面刷新、换设备和 API 重启不再影响任务执行。
- Temporal history 需要并发窗口、child Workflow 和 Continue-As-New 控制。
