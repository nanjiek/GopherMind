# P5：受控执行主链、提交回放与 Compaction

P5 将公共问答收紧为唯一的固定 Team 执行边界：

`认证身份 + 配置化可信规则 → 固定 Task DAG → Evidence → Safety → Response → 原子提交 → 只读回放`

## 公开 HTTP 契约

- `POST /query` 只接受普通问题、会话和文档字段；租户、用户、风险、任务、模型路由和 Team 路径均由服务端认证与 `TEAM_QUERY_RULES_JSON` 决定。
- 未完成可信配置时，`POST /query` 返回 `50321`，不会回落到旧 `QueryService`。`GET /stream/:session` 同样返回 `50322`；P5 没有在 Safety 前流式输出的协议。
- 成功回复必须经过固定 Team 的持久化 `safety` Task，且输出显式为 `{"approved":true}`；Response 提交前再次进行 `response.commit` Capability 授权。
- `GET /query/:run/replay?session_id=...` 只读取同一 tenant/user/session 范围内已提交的回复，返回稳定 `run_id`、`event_seq` 和 JSON 回复；它绝不重新运行 worker、模型或 Tool。不存在或范围不匹配统一返回 `40441`。

## 启用配置

默认关闭。启用必须同时设置：

```text
TEAM_QUERY_ENABLED=true
TEAM_QUERY_TENANT_ID=<服务端租户>
TEAM_QUERY_FAST_MODEL_TYPE=<已注册模型类型>
TEAM_QUERY_ADVANCED_MODEL_TYPE=<已注册模型类型>
TEAM_QUERY_RULES_JSON=[{"id":"general","priority":1,"phrases":["..."],"risk":"l1","task":"general-qa","workflow_version":"v1"}]
TEAM_QUERY_DEADLINE=30s
```

规则不匹配、同优先级冲突、配置缺失或错误都 fail closed。没有内置医学关键词；部署者必须显式提供经审核的规则。新 PostgreSQL 空库是唯一状态源，不迁移、双写、CDC 或回填旧 MySQL 数据。

## 提交、Outbox 与恢复

每个 Run 最多一个不可变 `agent_committed_responses` 行和一个同事务创建的 `outbox_messages` 意图（`operation_id=<run_id>:response`）。Outbox 本身不投递；未来投递器仍必须在外部调用前重新做 Capability 授权。进程在提交后崩溃时，回放读取该同一已提交结果，不会生成第二个答复。

## Compaction

`session_compactions` 是版本化 PostgreSQL 摘要权威表。`CompactionService` 对 Event Stream 的已提交水位做快照，给受限 `Summarizer` 提供结构化 JSON 输入，并以 revision CAS 发布：摘要只覆盖 `source_seq` 之前的事件，压缩期间及之后追加的消息留在摘要之后。输入预算预留输出 token；超过 1000 个事件或预算的快照明确失败，不静默截断，也不将超大结果装入内存。

Compaction 没有公开 HTTP 触发器。任何将来接入的模型 summarizer 都必须经过独立 Capability 执行边界；缓存式旧摘要不能充当该权威记录。
