# P4：可恢复工作流与可靠协作完成契约

## 范围

P4 在既有固定图 `Intake → 风险路由 → Evidence → Safety → Response`、checkpoint CAS 与纯节点恢复之上，补齐 PostgreSQL 中的 Task Board 和 Durable Mailbox。它是可恢复协调基础，不是动态多 Agent 执行器，也不把固定工作流变成另一个调度权威。

## Task Board

- 每个 Task 绑定一个既有 Run、可信 tenant/user/patient/session scope、静态 owner、Run 内幂等键和 deadline。
- `blockedBy` 边在建图时拒绝未知、重复、自引用和循环；根节点为 `ready`，依赖节点为 `blocked`。
- `Claim(expectedRevision, worker, ttl)` 只接受 `ready` 或已过期的 `running` Task，并验证全部依赖已经 `succeeded`。成功领取使 revision 与单调 lease epoch 同时递增。
- `Complete(expectedRevision, fencingToken, status)` 同时检查 revision、`running` 状态、未过期 lease 和 fencing token。旧 worker 不能提交；成功才释放所有依赖均成功的后继节点。失败或取消不释放后继。
- `ExpireBlocked` 将过期仍阻塞的依赖持久化为 `cancelled` / `dependency_timeout`，不会无限等待。
- `RecoverExpired` 只把过期 `running` lease 放回 `ready`。任务成功状态始终来自 Task Board，而非消息是否发送。

## Durable Mailbox 与队列语义

`agent_mailbox_messages` 是 PostgreSQL 内的持久逻辑队列，而不是一次性内存 channel 或外部 broker 的成功回执：

- `Enqueue` 在可信 scope 中持久化 UUID、Run、可选 Task、发送/目标 Agent、Run 内幂等键及有界 JSON-object payload。
- `Claim` 使用 `FOR UPDATE SKIP LOCKED` 挑选 queued 或 lease 已过期的目标消息，递增 revision 和 fencing token；因此语义为 at-least-once。
- `Acknowledge` 仅在当前未过期 fencing lease 下将消息写为 `delivered`。它绝不改变 Task 的完成状态。
- `CoordinationRecovery` 在重启后回收 Task 与 Mailbox 的过期 lease，并执行依赖 deadline 清理；它不会执行 Agent、自动确认消息，或重跑已成功 Task。

## 存储与边界

PostgreSQL migration 4 扩展 `agent_tasks` 的 lease、attempt、deadline、错误与完成字段，并新增 `agent_mailbox_messages`。所有读取、领取、完成和恢复均经父 `agent_runs` 的精确可信 scope 限制；任务、消息、依赖边均在事务中更新。

本节点不实现：Lead 动态委派、实际 worker/Agent 执行、真实外部 broker、连接恢复、LangGraph SDK 集成、模型/Tool/Skill/HTTP/MCP 调用或任何外部副作用。若将来 worker 触发副作用，仍必须通过既有 executor 并在效果发生前立即重新 Capability 授权。

不实施旧 MySQL 迁移、双写、CDC、回填或对账。

## 验收覆盖

- 静态依赖环及非法身份/状态在写库前被拒绝。
- 成功依赖释放后继；过期阻塞依赖被终止。
- 旧 revision 或 fencing token 不能完成 Task 或确认消息。
- Mailbox 确认不等于 Task 成功。
- 重启恢复只回收已过期 lease。
