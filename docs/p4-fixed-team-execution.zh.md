# P4：固定多 Agent 执行闭环

## 目标

在可靠 Task Board 和 Durable Mailbox 之上实现实际的、闭合的多 Agent 协作：Lead/Orchestrator 创建唯一固定 DAG，五个固定 worker 通过 Mailbox 领取各自任务，提交结构化结果，依赖成功后才释放下游，最后仅由 Response Agent 产生响应。

Lead 只接受 P3 的可信路径选择，固定拓扑为：

- `simple`：`Evidence → Response`。
- `standard`：`Intake` 与 `Triage` → `Evidence` → `Safety` → `Response`。
- `human_escalation`：只执行 `Triage`，返回结构化 `requires_human`，不启动 Evidence、Safety 或 Response。

它不接受模型或 worker 在运行时创建成员、改变边、选择路径或选取任意工具。

## 私有输入与结果

- Intake、Triage，以及 simple 路径中的 Evidence 只接收原始 JSON 请求。
- Evidence 只接收 Intake 与 Triage 的结构化输出。
- Safety 只接收 Evidence 输出。
- Response 只接收 Safety 输出。
- Lead 负责组装这些受限输入、读取持久化结构化结果、投递 ready 通知及汇总 Response；worker 不获得 DAG、Task Board 或 Mailbox 句柄。

Task 的 JSON input/output 随 Task 持久化。成功完成必须提交 JSON-object 输出；失败/取消必须提交错误码而不能伪造输出。因此重启后的 `Resume` 读取 DAG 与既有结果，只继续未成功的 Task，不重建图或重复执行成功任务。

## 完成与投递

Lead 对每个 ready Task 写入带稳定 Run/Task 幂等键的 Mailbox 通知。目标 worker：领取消息 → 领取 fenced Task → 执行纯 Agent 函数 → 以 lease revision/fencing token 提交结果 → 确认消息。

若进程在 Task 成功而消息确认前终止，过期 Mailbox lease 会重投；再次投递看到 Task 已不是 ready，只确认重复消息，不重复执行。消息确认始终不代表 Task 成功。

## 边界

- 不实现模型驱动的动态委派、Medication/Memory Curator 扩展、真实外部 broker、连接恢复或 LangGraph SDK。
- 当前 TeamAgent 是纯函数边界。未来接入模型、Skill、Tool、HTTP 或 MCP 时，必须逐次通过既有 executor，并在外部副作用前立即 Capability 再授权。
- 不实施旧 MySQL 迁移、双写、CDC、回填或对账。
