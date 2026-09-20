# P5 Step 2：可信路由到 Query Team 入口

本节点在 `internal/core/service.RoutedTeamQueryService` 建立 Query/API 应用层的最小 Team 入口：调用者必须先提供可信 `gateway.Request`、运行范围（含 tenant 与 user）、run ID、deadline 和 JSON object payload。入口每次都调用 P3 Router，再以 `gateway.FixedTeamPath` 把 Router 的 Decision 映射为唯一的 P4 `simple`、`standard` 或 `human_escalation` 路径；调用方不能提交 Decision 或路径来绕过该过程。

该入口只启动固定 Team 并返回**未提交**的结构化数据。它没有 SessionRepository、模型、RAG、队列、缓存或响应写入依赖；因此不会发布助手答复、流片段或其他外部副作用。`RequiresHuman` 会原样标记为人工接管，绝不当作可发布的医疗答复。下一节点必须在具有可信分诊/策略来源的 HTTP 或 API 适配器中构造 `RoutedTeamQueryInput`；不得从客户端 body、模型输出或“默认低风险”推导其 Route。

响应提交屏障、Capability 执行前再授权、generation/event replay、Durable Action/Event-Outbox 和 PostgreSQL 运行时装配均不属于本节点。没有 MySQL 迁移、双写、CDC 或回填。
