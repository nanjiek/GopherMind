# P3 Step 3：受限 Tool/Skill 执行器

## 范围

`internal/agent/gateway.Executor` 注册明确的 Tool 或 Skill manifest，校验 JSON 输入、输出大小和 JSON 输出，并在调用 handler 前立即执行 Capability 授权。授权失败时 handler 不会运行。

## 明确不包含

- 不启动 MCP client/server，不建立外部连接或 HTTP 路由。
- 不实现 JSON Schema registry、预算、限流、审计持久化、重试或熔断。
- 不允许模型直接执行 handler；调用方必须携带可信 Scope、Agent、目标和 Purpose。

## 验收

- 未授权 Capability 不会触发 handler。
- 患者绑定 Tool 只有可信三元 Scope 才能执行。
- 未注册目标、无效输入和超大/非 JSON 输出被拒绝。
