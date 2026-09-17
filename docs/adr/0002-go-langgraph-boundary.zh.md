# ADR-0002：Go 控制面与 Python LangGraph 执行边界

状态：接受，P1 前需完成 PostgreSQL 恢复验证
日期：2026-09-17

## 背景

当前在线链路由 Go 直接编排 QueryService/StreamService；仓库没有 LangGraph 依赖。V3 需要可恢复 Workflow，同时 Go 仍负责 HTTP/MCP 接入、权限、预算、工具副作用和事实写入。

LangGraph 官方文档区分 thread checkpoint 与跨 thread store，并建议生产使用数据库 checkpointer。Checkpoint 适合恢复图状态，不应成为患者事实权威库。[Persistence](https://docs.langchain.com/oss/python/langgraph/persistence) [Memory](https://docs.langchain.com/oss/python/langgraph/add-memory)

## 决策

- LangGraph 作为独立 Python 执行服务，Go 通过版本化内部协议调用。
- Go 是 Run、权限、预算、工具调用、幂等副作用和对外发布的权威；Python 只执行图节点并保存可恢复 checkpoint。
- `run_id` 由 Go 生成；`thread_id` 使用稳定 UUID，长度不超过 255；每次请求携带 `workflow_version`、`surface_version`、`revision` 和 deadline。
- Python 节点不直接写患者权威事实，不直接访问高风险外部工具。节点返回结构化 Action，由 Go 校验并执行，再以 Observation 继续图。
- LangGraph checkpointer 使用同一 PostgreSQL 实例的独立 schema/表；业务表不依赖 LangGraph 私有表结构。
- 流式事件包含 `run_id + generation_id + event_seq`；只有 Go 发布屏障能提交最终回答。

## 最小协议

`POST /internal/v1/runs/{run_id}:invoke` 接受 thread、workflow、surface、revision 和输入；返回状态、下一步 Action、checkpoint 引用和事件序号。恢复使用相同 run/thread 和期望 revision，重复请求必须返回已提交步骤或产生明确冲突。

## 验收门槛

P0 使用 SQLite checkpointer 验证 checkpoint 在 checkpointer 关闭并重新打开后仍可读取，脚本位于 `python/workflow_service/checkpoint_smoke.py`。SQLite 只用于本地 spike，不进入产品架构。

进入主链路前还需要使用 PostgreSQL 独立验证：节点完成后进程终止并恢复不会重跑已提交副作用；相同请求重放结果一致；过期 revision 被拒绝；取消和 deadline 能停止节点；checkpoint 清理有保留策略。未通过时继续使用 Go 单 Agent 路径。

## 降级

LangGraph 服务不可用时，只允许配置明确的低风险单 Agent 降级。L2/L3 请求不得静默绕过安全节点；应返回受控提示或转人工。
