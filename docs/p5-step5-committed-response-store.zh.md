# P5 Step 5：持久化提交响应与 Safety 证明

迁移 6 在全新的 PostgreSQL schema 新增 `agent_committed_responses`。每个 Run 只有一条不可覆盖的已提交响应：Run ID 同时是稳定 generation ID，`event_seq=1` 是唯一可重放事件，重复提交因主键冲突失败。读取始终通过 `agent_runs` 的 tenant/user/patient/session 精确范围过滤，重放只读已提交数据，不会重新运行 Team、Tool 或外部调用。

`PostgresSafetyReviewVerifier` 仅在同一 Run 的持久化 `safety` Task 已成功且输出包含 `{"approved":true}` 时才给 P5.4 提交屏障放行；调用方提供的布尔标记或 `{"safe":true}` 不足以证明审核。公开 HTTP、SSE、队列、Outbox 和 MySQL 路径均未改变。
