# P4 Step 1：固定工作流图

## 范围

`internal/agent/runtime.FixedWorkflow` 提供一个进程内、闭合的固定图：

`Intake → 风险路由 → Evidence → Safety → Response`

五个节点使用不同的强类型函数签名，因而只能将上游已结构化的输出交给指定的下游节点。执行器在每次调用节点前写入既有内存 `Run.CurrentNode`，便于运行中检查当前节点；它不改变 `Run` 的生命周期状态，也不产生外部副作用。

风险节点可通过 `gateway.FixedWorkflowRiskRoutingNode` 适配既有 `gateway.Router`。运行时侧的 `RiskRoutingInput` 与 `RiskRoutingOutput` 是独立强类型契约，避免 `runtime` 反向依赖 `gateway`；适配仅做原有的确定性路由，不调用模型或连接外部服务。风险输入仍必须来自可信上游策略或确定性分诊规则，模型不能选择风险等级。

输入及 Intake/Evidence/Safety/Response 的数据必须是 JSON object，并在节点交接处复制，避免调用方通过共享缓冲区修改已记录的结果。

## 明确不包含

- 不实现 durable Task DAG、Mailbox、依赖、租约、fencing token、消息去重、队列成功语义或连接恢复。
- 不实现动态多 Agent 委派；没有运行时注册、插入、删除或重排图节点的 API。
- 不实现 PostgreSQL Run/Task 状态持久化、checkpoint、迁移、旧 MySQL 迁移、双写、CDC 或回填。
- 不调用模型、Tool/Skill、HTTP、MCP、队列或数据库，也不接入 QueryService/StreamService。
- 不放宽既有 Capability 边界：未来任何会产生外部副作用的节点仍必须经现有 executor，在效果发生前立即再次授权。

## 验收

- 只会按 Intake、风险路由、Evidence、Safety、Response 的顺序执行，并把最终 `Run.CurrentNode` 写为 `response`。
- 任一节点出错立即停止，后续节点不执行。
- 缺失节点、非运行态 Run、空 Run 或非法 JSON object 输入/输出会被拒绝。
- Gateway 路由适配保留既有 L0–L3、受限任务类型与模型别名的确定性决策。
