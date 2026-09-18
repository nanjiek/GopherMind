# P3 Step 2：Capability 策略

## 范围

本节点实现进程内静态 allow-list：指定 Agent 只能请求显式注册的 Runtime Capability。患者绑定能力必须携带认证链路提供的 tenant、user 和 patient Scope。

## 明确不包含

- 不注册或执行 Tool、Skill、MCP；未来执行器必须在副作用前再次调用 `Authorize`。
- 不接入 HTTP、模型、预算、审计、数据库或事件日志。
- 不允许模型或 Prompt 自行增加 Capability、患者范围或 Purpose。

## 验收

- 未注册 Agent/Capability 被拒绝。
- 重复 grant 和缺少必填字段被拒绝。
- 患者绑定能力没有可信三元 Scope 时被拒绝。
