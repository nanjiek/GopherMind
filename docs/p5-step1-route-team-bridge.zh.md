# P5.1：可信路由到固定 Team 的桥接

## 目标

将 P3 Gateway 的可信 `Decision` 映射为 P4 的唯一固定 Team 路径：

| Gateway workflow | 条件 | Team path |
| --- | --- | --- |
| `single-agent-query` | 有已配置的模型路由，且不要求人工 | `simple`：Evidence → Safety → Response |
| `clinical-review` | 有已配置的模型路由，且不要求人工 | `standard`：Intake/Triage → Evidence → Safety → Response |
| `manual-escalation` | `RequiresHuman=true`，且没有模型路由 | `human_escalation`：Triage only |

桥接器不接受调用方直接指定 Team path，也不使用模型输出。缺模型路由、人工标志与 workflow 矛盾、未知 workflow 均在启动 Team 前被拒绝。

## 验收

- 三类合法 P3 Decision 只能映射到一条预定义 P4 路径。
- 伪造的人工升级、手工 escalation + model、缺失 model、未知 workflow 被拒绝。
- 该模块不调用模型、不会创建 Task、不会启动 worker、不会写入数据库或发布响应。

## 明确不在本节点

- 不接入 Query/API/Stream 入口；这将在下一 P5 节点把已验证的可信 Decision 传给 FixedTeamCoordinator。
- 不实现人工接管通知、Response 提交屏障、generation/event-seq、Event-Outbox、Compaction 或真实外部副作用。
- 不改旧 MySQL，也不做迁移、双写、CDC、回填或对账。
