# P4 Step 4：静态纯图 Checkpoint 恢复

## 范围

`runtime.StaticWorkflowRecoveryRunner` 将 P4 的固定图绑定到 Step 3 的 checkpoint CAS。它把输入和每个节点的结构化输出保存为 `StaticWorkflowState`；每完成一个节点即通过 `CheckpointStore.Save` 前进一步。进程重启后，`Resume` 从 checkpoint 的 `next` 节点继续，因此不会重复已经成功保存的 Intake、风险路由、Evidence 或 Safety 节点。

本节点只允许纯进程内节点。它不会执行或包装任何外部副作用；将来需要 Tool、Skill、HTTP、MCP、数据库业务写入或消息发布的节点必须等待 Durable Action/Task 幂等协议，并在效果发生前再次 Capability 授权。

## 明确不包含

- 不实现 LangGraph runtime 或伪称已经与 LangGraph checkpoint 互通。
- 不实现 Task DAG、Mailbox、依赖超时、租约、fencing token、队列语义、动态 Agent 委派、并行 fan-out、重试或连接恢复。
- 不恢复终态 Run，也不重放外部副作用、发布 Response 或接入 QueryService/StreamService。
- 不迁移、双写、CDC、回填或读取旧 MySQL 数据。

## 验收

- 新执行创建 revision 1 checkpoint，随后以 CAS 保存 running 和每个已完成节点；成功时保存 completed Response。
- 从已保存 `next=evidence` 恢复时，只执行 Evidence、Safety、Response。
- 任一节点失败写入 failed checkpoint 和稳定错误码；CAS 冲突不会覆盖其他 worker 的新 checkpoint。
- Scope、输入与所有已保存结构化 handoff 保持 Checkpoint 的范围和 JSON 验证约束。
