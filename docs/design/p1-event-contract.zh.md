# P1 Event Log、Projection 与 Surface 设计

状态：P0 设计冻结，P1 实现输入  
数据库策略：新建空 PostgreSQL，不导入旧 MySQL 数据

## 1. 职责

- `clinical_events` 是会话和 Agent 可审计事实的追加日志。
- 业务表保存当前权威状态；写业务状态与对应 Event/Outbox 必须在同一 PostgreSQL 事务中完成。
- Projection 可以删除并从 Event Log 重建；Redis 只缓存带版本的 Surface。
- LangGraph checkpoint 保存图执行位置，不替代 Event Log、Task 或患者权威记忆。

## 2. 事件信封

```json
{
  "event_id": "uuid",
  "event_type": "conversation.message.appended.v1",
  "schema_version": 1,
  "tenant_id": "default",
  "user_id": "user-id",
  "patient_id": null,
  "session_id": "uuid",
  "stream_seq": 42,
  "request_id": "uuid",
  "idempotency_key": "client-or-operation-key",
  "correlation_id": "run-id",
  "causation_id": "previous-event-id",
  "actor_type": "user",
  "actor_id": "user-id",
  "payload": {},
  "occurred_at": "2026-09-17T12:00:00Z",
  "recorded_at": "2026-09-17T12:00:00Z"
}
```

`payload` 只保存业务输入和结构化结果，不保存模型隐藏推理。事件类型包含版本后缀，破坏性 schema 变化使用新类型或新版本并提供 upcaster。

## 3. 追加协议

1. 开启 PostgreSQL 事务并锁定/创建 `event_streams(session_id)`。
2. 校验 session 的 user/patient scope。
3. 若 `(tenant_id, idempotency_key)` 已提交，返回原事件和业务结果。
4. 原子增加 `next_seq`，写业务状态、`clinical_events` 和需要投递的 `outbox_messages`。
5. 提交后才刷新/失效 Redis Surface。缓存失败不回滚已提交事实。

并发写不靠时间戳排序。`stream_seq` 是单 session 顺序，`event_id` 是全局身份；客户端时间只作为 occurred_at 元数据。

## 4. Projection 与 Surface

Projection worker 按 `(projection_name, shard)` 保存最后 event cursor，使用事件幂等更新读模型。失败重试不会越过未处理事件。

Surface cache key：`surface:{tenant}:{user}:{session}:{agent}:{surface_version}`。值至少包含 `source_seq`、`projection_version`、`expires_at` 和结构化内容。读取时发现 source_seq 落后、版本不匹配或缓存缺失，就从 PostgreSQL Projection/Event 重建。

首个 Surface 仍复用最近消息窗口，目标是先建立正确事实边界；结构化 Compaction 在 P5 实现。

## 5. P1 最小事件

| 类型 | 产生时机 | 最小 payload |
| --- | --- | --- |
| `conversation.session.created.v1` | 建立会话 | title、model preference |
| `conversation.message.appended.v1` | 用户/助手消息完成提交 | message_id、role、content、provider/model |
| `generation.started.v1` | 开始生成 | generation_id、workflow/model version |
| `generation.completed.v1` | 回答通过提交 | generation_id、message_id、usage |
| `generation.failed.v1` | 生成失败 | generation_id、error_code、retryable |
| `document.status.changed.v1` | 文档状态变化 | document_id、old/new status、job_id |
| `memory.record.changed.v1` | 记忆状态变化 | memory_id、old/new status、version |

## 6. 接口草案

```go
type EventStore interface {
    Append(ctx context.Context, request AppendRequest) (AppendResult, error)
    ReadStream(ctx context.Context, scope Scope, sessionID string, afterSeq int64, limit int) ([]Event, error)
}

type SurfaceStore interface {
    Get(ctx context.Context, key SurfaceKey) (Surface, bool, error)
    Put(ctx context.Context, surface Surface, ttl time.Duration) error
    Invalidate(ctx context.Context, key SurfaceKey) error
}
```

`AppendResult` 返回 committed event、stream revision 和是否为幂等重放。冲突、越权、schema 不支持和暂时不可用使用稳定错误码。

## 7. PostgreSQL 空库 schema 清单

P1 使用版本化 SQL migration 创建：

- 现有域：users、refresh_tokens、sessions、messages、consumer_inbox、documents、eval_runs、mcp_jobs、memory_records。
- 事件域：event_streams、clinical_events、projection_checkpoints、outbox_messages。
- 运行域预留：agent_runs、agent_steps；P2 才开始写入。
- LangGraph 使用独立 schema，例如 `langgraph_checkpoint`，由明确版本的 checkpointer 初始化；业务 migration 不引用其内部表。

核心约束：所有 scope 字段非空；sessions/messages/events 的归属可通过索引查询；`clinical_events(session_id, stream_seq)` 唯一；`(tenant_id, idempotency_key)` 唯一；Outbox operation ID 唯一；消息 request ID 在业务定义范围内唯一。

## 8. 初始化、发布与回滚

初始化只面向空 PostgreSQL：创建 database/schema role → migration up → schema verifier → 启动应用。若检测到未知表或非本应用 schema，默认失败，不能自动清空。

P1 发布采用配置开关保持旧 Go 服务路径，但新版本的 repository 只连接 PostgreSQL。切换后不读取 MySQL。回滚到 P1 的前一个 PostgreSQL 兼容应用版本；若尚未开放写入，可停止服务并销毁测试数据库。任何 down migration 必须先证明不会丢失已写数据，否则使用 forward fix。

## 9. P1 验收

- 空库初始化、schema 校验、两个入口编译和真实 PostgreSQL 集成测试通过。
- 创建会话和追加消息可生成连续 Event，重复键返回原结果。
- Redis 数据丢失后可从 PostgreSQL 重建相同窗口 Surface。
- 并发写、越权、提交响应丢失和 Projection 重启用例通过。
- 仓库、配置、compose 和依赖中不再存在运行时 MySQL 路径。
