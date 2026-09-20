# P5 Step 3：确定性可信 Query 路由策略

`TrustedQueryPolicy` 是 P5.2 Routed-Team Query 入口的服务端前置适配器。它只从已认证且经会话归属校验的 `tenant/user/session` 身份，以及普通的 `question/document_id` 内容构造 `RoutedTeamQueryInput`；请求体、模型输出都没有风险等级、任务、租户、用户、Decision 或 Team 路径字段。

策略仅接受启动时验证的确定性规则。规则指定短语、优先级和 P3 `gateway.Request`；最高优先级的唯一匹配才会产生路由。无规则命中或同优先级多规则命中都会失败关闭，绝不把未分类问题默认降为低风险。红旗词和医学分流阈值不是本节点内建的临床判断：部署者必须用经医疗安全治理的配置规则提供它们，且应让红旗规则产生 `l3/red_flag_triage`，由既有 Router 导向人工升级。

该节点不挂载公共 HTTP handler、不运行 Team、不调用模型、RAG、队列或数据库，也不写入 assistant message。响应提交屏障、Capability 再授权、结构化人工升级事件、PostgreSQL 装配与 Outbox 保持在后续节点；没有 MySQL 迁移、双写、CDC 或回填。
