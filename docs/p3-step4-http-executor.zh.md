# P3 Step 4：受限 HTTP 执行器

## 范围

`internal/agent/gateway.HTTPExecutor` 只调用注册时声明的固定 HTTP/HTTPS URL。调用输入只能提供受大小限制的 JSON 请求体，不能改变目标 URL、方法或请求头。响应同样受大小限制且必须是 JSON。

每次请求在创建、输入校验、端点查找完成后，都会在 `client.Do` 前立即重新执行 Capability 授权。授权失败、父 context 已取消或请求超时均不会产生成功的外部调用。

HTTP client 会禁用重定向，避免一个已授权端点将请求转发至未注册的外部目标。

## 明确不包含

- 不启动 MCP client/server，不做工具枚举或 MCP 生命周期管理。
- 不允许动态 URL、方法、请求头或未注册目标。
- 不实现凭证注入、预算/限流、审计持久化、重试、熔断、provider 接入或 schema 变更。

## 验收

- 未授权 Capability 不会发出 HTTP 请求。
- 成功调用只能命中固定端点，并保留父 context 取消和请求超时。
- 请求和响应的大小限制、非 JSON 响应均被拒绝。
- 3xx 重定向不会被自动跟随。
