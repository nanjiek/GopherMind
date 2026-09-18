# P4 Step 3：工作流 Checkpoint 权威

## 范围

本节点定义 `runtime.Checkpoint` 与 `CheckpointStore`，并由 PostgreSQL `WorkflowCheckpointStore` 实现为一个 Run 的唯一持久化 checkpoint 权威。

每个 checkpoint 必须带可信 tenant/user/patient/session 范围、Run ID、workflow ID/version、Run 状态、当前节点、正 revision 及有界 JSON object 状态。状态不得包含隐藏模型推理或大型工具结果。

迁移扩展既有但尚未使用的 `agent_runs`，使其保存当前 checkpoint；新增 `agent_run_checkpoints` 只追加历史。`Save(expectedRevision, checkpoint)` 在同一事务内以 `agent_runs.revision` CAS 更新当前状态并追加历史。旧 worker、重复或过期写入均不能覆盖新 revision。

## 明确不包含

- 不伪造 LangGraph 集成：仓库当前没有可核验的 LangGraph runtime；Go/Python 图执行协议仍需单独冻结。
- 不将 checkpoint 接入 `FixedWorkflowRunner`，不在此节点实现重启恢复或自动重放节点。
- 不实现 Task DAG、Mailbox、依赖超时、任务幂等、租约、fencing token、队列语义、连接恢复或动态多 Agent 委派。
- 不调用模型、Tool/Skill、HTTP、MCP、队列或数据库以外的外部副作用；不放宽 Capability 授权边界。
- 不迁移、双写、CDC、回填或读取任何旧 MySQL 数据。

## 验收

- 首个 checkpoint 只能为 revision 1；后续保存必须以当前 revision 的 CAS 前进一版。
- 当前 checkpoint 和不可变历史在同一事务写入；重复或过期写被拒绝。
- tenant/user/patient/session 范围必须完全一致，越范围读取不泄露 checkpoint 是否存在。
- 空 PostgreSQL schema 可重复执行 migration；本机若未配置 `POSTGRES_TEST_DSN`，PostgreSQL 集成测试按既有约定跳过。
