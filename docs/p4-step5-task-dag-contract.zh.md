# P4 Step 5：Durable Task/DAG 最小契约

## 目标

为一个既有 Agent Run 定义可持久化的静态 Task DAG：每个任务有稳定 UUID、类型、静态 owner、Run 内幂等键、revision 1 和 `blockedBy` 依赖。根任务初始为 `ready`，有依赖的任务初始为 `blocked`。

`runtime.NewTaskDAG` 是进入存储前的唯一结构校验：拒绝空或重复任务 ID、重复 Run 内幂等键、缺失/重复/自指依赖、非初始 revision、伪造初始状态和依赖环。它复制输入和依赖数组，避免调用方在验证后篡改图。

PostgreSQL migration 3 新增：

- `agent_tasks`：以 `agent_runs` 为父记录的任务身份、初始状态和 revision；`(run_id, idempotency_key)` 唯一。
- `agent_task_dependencies`：任务与其 `blockedBy` 边；外键保持任务存在，且禁止自边。

`TaskDAGStore.Create` 在同一事务中验证完整图、验证父 Run 的精确 tenant/user/patient/session scope，并写入任务和依赖。`Load` 也只经该父 Run 的精确可信 scope 读取；没有匹配 Run 或没有图时返回 not found。

## 验收

- 根节点和依赖节点分别归一为 `ready` 与 `blocked`。
- 重复 ID/幂等键、未知/重复/自依赖、循环依赖和非可信 scope 均被拒绝。
- 任务与依赖边同时提交，不能形成部分持久图。
- 新 PostgreSQL 空库迁移包含 Task/DAG 表；不触碰 MySQL。

## 明确不在本节点

- 不实现动态多 Agent 委派、实际 Agent 执行或调度。
- 不实现 Task 状态推进、CAS 更新、完成/失败/重试、依赖超时、租约或 fencing token。
- 不实现 Durable Mailbox、消息去重、队列投递或连接恢复。
- 不改变固定工作流、checkpoint 恢复、模型/Tool/Skill/HTTP/MCP 调用或其 Capability 再授权边界。
- 不实施旧 MySQL 迁移、双写、CDC 或回填。
