# P1 节点 2：Event Log、Projection checkpoint 与 Surface 闭环

状态：已实现并通过真实 PostgreSQL/Redis 验证

## 范围

- EVT-001：定义版本化 Event 信封、Scope、Append/ReadStream 协议和稳定错误。
- EVT-002：会话创建、用户消息和助手消息与对应 Event 在同一 PostgreSQL 事务提交。
- EVT-003：单 session 使用 `stream_seq` 排序；重复幂等键返回已提交 Event/业务结果。
- PRJ-001：Projection checkpoint 可保存并在 store 重新创建后恢复。
- SFC-001：最近消息 Surface 使用 `surface:{tenant}:{user}:{session}:{agent}:{version}` 键，保存 source seq、projection version 和过期时间。
- SFC-002：缓存缺失、版本不匹配或事件水位落后时从 PostgreSQL 重建。

本节点不实现结构化 Compaction、LangGraph PostgreSQL checkpointer、完整 Agent Runtime 或旧 MySQL 数据导入。

## 事务与重放

创建会话时依次提交 `conversation.session.created.v1` 和首个 `conversation.message.appended.v1`。追加消息会先校验 session scope，再写消息和 Event。所有步骤位于同一事务；任一步失败会同时回滚业务表和 Event Log。

幂等键在 tenant 内唯一。会话、用户消息和助手消息分别使用同一 request ID 的稳定派生键。提交响应丢失后重试会查询原 Event，并返回原 session 或成功结果，不会新增消息或推进 stream seq。

## Surface 恢复

`SessionService.LoadWindow` 先读取 PostgreSQL 当前事件水位，再验证缓存 Surface。缓存水位落后或缓存被删除时，repository 在只读 repeatable-read 事务内读取当前水位和最近消息，并写回 Redis。Redis 更新失败不回滚 PostgreSQL 已提交事实。

Redis client 使用 Universal Client：单地址连接普通 Redis，多地址可连接 Cluster。旧摘要与窗口接口暂时保留，用于现有测试替身和兼容路径；真实 PostgreSQL repository 与 Redis cache 自动使用新 Surface 接口。

## 验证

- 空库 migration、重复 migration、未知 schema 和缺表拒绝。
- 会话/消息/Event 同事务提交，跨用户读取和追加被拒绝。
- 提交后相同键重试返回同一结果。
- 同 session 并发追加的 seq 从 1 连续递增；同键并发只生成一条消息和 Event。
- Event 按 `afterSeq` 顺序读取，Projection checkpoint 在 store 重新创建后保持一致。
- Redis Surface 水位落后时刷新；删除缓存后重建内容和 source seq 与删除前一致。

验证命令：

```powershell
$env:GOTOOLCHAIN='go1.23.12'
$env:POSTGRES_TEST_DSN='postgres://gophermind:gophermind@127.0.0.1:55432/gophermind_test?sslmode=disable'
$env:REDIS_TEST_ADDR='127.0.0.1:56379'
go test ./...
go build ./cmd/...
go vet ./...
```

## 回滚

本节点不新增 migration，依赖 P1 节点 1 的初始 schema。应用可以回滚到节点 1 代码；已写入的 Event 表不会被旧代码读取，也不执行 destructive down migration。重新启用节点 2 后会继续使用已有 stream seq。
