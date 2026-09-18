# P3 Step 1：Gateway 模型与工作流路由

## 范围

本节点新增进程内 `internal/agent/gateway` 路由契约：由可信风险等级与受限任务类型确定 Workflow 和逻辑模型路由。红旗请求路由到人工升级，绝不把 LLM 作为唯一分流权威。

## 明确不包含

- 不调用或替换现有模型 Provider；`ModelType` 仅使用应用配置已有的逻辑别名。
- 不实现 Capability、Skill、Tool、MCP、预算、熔断、审计或持久化。
- 不接入 HTTP、QueryService、StreamService 或 LangGraph。

## 验收

- L0/L1 普通任务走配置的快速单 Agent 工作流。
- L2 或复杂/用药/最终复核任务走高级临床复核工作流。
- L3 或红旗任务只产生 `manual-escalation`，不选择模型。
- 无效风险、任务或模型路由配置被拒绝。
