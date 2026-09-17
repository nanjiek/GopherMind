# 包说明

## 包作用
- 提供缓存集群访问与会话缓存实现。
- 在缓存异常场景提供内存降级以保护主流程。

## 实现逻辑
1. 初始化集群客户端并进行连通性检查。
2. 管理摘要缓存、流式分片缓存和幂等键。
3. 远端失败时标记降级并写入本地回退缓存。
4. 支持流式分片追加与回读。

## 关键接口
```go
func NewClusterClient(cfg config.RedisConfig) *redis.ClusterClient
type SessionCache struct
func (c *SessionCache) GetSummary(ctx context.Context, userID string, sessionID string) (string, bool, error)
func (c *SessionCache) AppendStreamChunk(ctx context.Context, requestID string, chunk string, ttl time.Duration) error
```

## 协作关系
- 被业务服务用于摘要和流式状态缓存。
- 被消息消费模块用于幂等标记。
