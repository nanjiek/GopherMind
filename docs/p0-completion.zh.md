# P0 完成报告

日期：2026-09-17  
结论：P0 三步完成，可以开始 P1 实现。

## 三步结果

| 步骤 | 交付 | 结果 |
| --- | --- | --- |
| 1. 构建基线 | 依赖锁定、入口修复、CI、缓存/流式回归 | Go test/build/vet 与前端构建通过 |
| 2. 架构决策 | PostgreSQL 空库切换、Go/LangGraph、RAG/DeepSeek 三份 ADR | 边界冻结；LangGraph checkpoint reopen spike 通过 |
| 3. P1 准备 | 12 条最小 Gold Set、schema 校验测试、真实依赖矩阵、P1 Event/Surface 设计 | 交付可机器验证输入和 P1 验收门槛 |

## 已接受约束

- PostgreSQL 是新架构唯一关系数据库。
- 原 MySQL 数据全部废弃，不实施任何数据迁移、双写、回填或对账。
- 当前在线 RAG 主路径为 Python + Qdrant；Go 内存 vector client 不计作 Pinecone 接入。
- LangGraph 在 PostgreSQL 恢复验证通过前不进入在线主链路。
- 规划中的 DeepSeek 型号和价格已于 2026-09-17 对照官方页面核验，但实现必须配置化并在接入时重新核验。

## 验证边界

P0 的离线验证已通过。Docker 引擎在执行时未运行，Python RAG 完整模型依赖也未安装，因此 PostgreSQL、Redis、RabbitMQ、Qdrant 和 DeepSeek 的真实联调没有伪装成已通过；它们已进入冒烟矩阵，按相应阶段执行。

## P1 首个闭环

1. 用版本化 SQL 初始化空 PostgreSQL 并替换所有 MySQL runtime 依赖。
2. 完成 `EventStore.Append/ReadStream` 及事务幂等。
3. 将会话/消息写入与 Event 放在同一事务。
4. 基于事件重建最近消息 Surface，并验证清空 Redis 后恢复。
5. 执行并记录真实 PostgreSQL 并发、重复请求、越权和恢复测试。

P1 不包含旧数据导入，也不包含完整多 Agent、Compaction 或长期记忆治理。
