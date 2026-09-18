# P4 Step 2：固定图内存执行协议

## 范围

`runtime.FixedWorkflowRunner` 将固定图连接到现有进程内 `Run` 协议：创建一个 Run，经 `loading_context → routing → running` 进入闭合五节点图，随后将已验证的 Response JSON object 写成唯一 `return_result` Action 并调用 `Complete`。

该 Runner 通过既有 `Controller` 推进生命周期、提交最终 Action 和完成 Run，因此已有 Hook Pipeline 仍可包裹这些状态变更。节点执行失败、生命周期失败或完成失败会在已创建的 Run 上留下稳定的失败码与分类，再返回原始错误；不会执行重试。

## 明确不包含

- 不实现 durable Task DAG、Mailbox、依赖超时、租约、fencing token、去重、队列语义、连接恢复或 PostgreSQL Run/Task/checkpoint 持久化。
- 不实现动态多 Agent 委派、模型调用、Tool/Skill、HTTP、MCP、队列、数据库写入、Response 发布或 QueryService/StreamService 接入。
- 不新增或放宽 Capability；未来任何有外部副作用的节点仍须在其 executor 内于副作用前立即重新授权。
- 不实现失败重试、恢复或补偿；本节点只记录内存终态。

## 验收

- 成功路径得到 `completed` Run、最终 `response` 当前节点，且只记录一个 `return_result` Action。
- 节点失败会短路并记录 `failed` Run、当前失败节点、稳定错误码和分类；取消与 deadline 分别保留为 context/timeout 分类。
- 已注册 Hook 仍观察到三个启动状态变更、最终 Action 与完成变更。
- 无效 RunSpec 或缺失固定图被拒绝，且不会产生部分 Run。
