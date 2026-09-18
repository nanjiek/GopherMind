# P3 Step 5：受限 MCP Gateway

## 范围

`internal/agent/gateway.MCPGateway` 将预先连接并注册的 MCP peer 适配为固定内部 executable。每个 manifest 固定 remote tool、Capability、超时以及输入/结果上限；调用方不能枚举 peer/tool 或选择不同的远端工具。

`Health` 使用 MCP `Ping`，`Call` 使用 MCP `CallTool`。两条出站路径均在输入校验、目标查找和 timeout context 创建后，于 SDK 调用前立即重新执行 Capability 授权。MCP 返回值视为不可信结构化 JSON，并受输出大小限制。

## 明确不包含

- 不建立或持久化 MCP 连接，不做 `ListTools`、动态发现、凭证注入或多租户连接池。
- 不实现预算、限流、审计持久化、重试、熔断、异步 Task/Mailbox、Schema registry 或 provider 接入。
- 不改动现有 stdio MCP Server、HTTP executor、Run/Action/Observation 状态机或数据库 schema。

## 验收

- 未授权调用不会触发 `CallTool` 或 `Ping`。
- 只能调用 manifest 固定的 MCP tool；arguments 必须是受限大小的 JSON object。
- 患者绑定 Capability 对 health 和 tool call 都要求可信 Scope。
- 父 context 取消、超大结果和无效参数会被拒绝。
