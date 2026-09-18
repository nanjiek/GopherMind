# P0 真实依赖冒烟矩阵

本矩阵区分当前已通过的离线检查与 P1 开工后的真实依赖检查。没有执行的项目不得标记为通过。

| ID | 范围 | 前置条件 | 操作 | 通过条件 | P0 状态 |
| --- | --- | --- | --- | --- | --- |
| OFF-01 | Go | Go 1.23.12 | `go test ./...` | 全部包通过 | 通过 |
| OFF-02 | Go 入口 | Go 1.23.12 | `go build ./cmd/...` | API/MCP 均构建 | 通过 |
| OFF-03 | 前端 | Node 20 | `npm ci` 后 `npm run build` | Vite 构建成功 | 通过 |
| OFF-04 | Workflow spike | Python 3.11 + P0 requirements | 运行 checkpoint smoke | 关闭并重开后状态一致 | 通过 |
| PG-01 | PostgreSQL 初始化 | 空 PostgreSQL | 执行全部版本化 migration 两次 | 首次成功；第二次无漂移 | P1 节点 1 通过 |
| PG-02 | PostgreSQL 事务 | PG-01 | 模拟提交成功但响应丢失后重试 | 相同幂等键只产生一份结果 | P1 节点 2 通过 |
| PG-03 | PostgreSQL 并发 | PG-01 | 并发追加同一 session event | seq 唯一且连续，CAS 冲突可识别 | P1 节点 2 通过 |
| CACHE-01 | Redis 恢复 | PostgreSQL + Redis | 写会话、清空 Surface cache、再次读取 | 从 PostgreSQL 事实与 Event 水位重建且内容一致 | P1 节点 2 通过 |
| RAG-01 | Python/Qdrant | Qdrant + 模型依赖 | 摄取固定文档并检索 | user/document scope 正确，返回 trace | P1/P6 执行 |
| RAG-02 | RAG 故障 | 停止 Qdrant | 发起 RAG 请求 | 显式 degradation reason，不返回伪 embedding | P1 执行 |
| GRAPH-01 | LangGraph/PostgreSQL | PostgreSQL checkpointer | 节点后终止服务再恢复 | 已提交节点状态恢复 | P1/P4 执行 |
| AUTH-01 | Scope | 两个测试用户 | 用户 B 请求用户 A 的 session/document | 404/拒绝且有审计事件 | 会话/Event 在 P1 节点 2 通过；文档待补 |
| MQ-01 | RabbitMQ | RabbitMQ + PostgreSQL Inbox | 重复投递同一 message ID | 单次业务副作用 | P1/P4 执行 |
| MODEL-01 | DeepSeek | 测试 API key | JSON、Tool Call、流式、usage、超时 | Schema/usage/error mapping 正确 | P3 执行 |

真实依赖测试使用隔离数据库和测试身份，禁止连接废弃 MySQL 搬运数据。测试产生的数据可以按整个测试 schema 或数据库删除，不对生产表做无条件清理。
