package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"gophermind/internal/config"
	"gophermind/internal/core/model"
)

type fallbackItem struct {
	Value     string
	ExpiresAt time.Time
}

// SessionCache 实现 Redis + 内存退化缓存。
type SessionCache struct {
	client *redis.ClusterClient
	logger *zap.Logger

	degraded atomic.Bool
	mu       sync.RWMutex
	memKV    map[string]fallbackItem
}

// NewSessionCache 初始化缓存层。Redis 不可用时自动退化到内存。
func NewSessionCache(cfg config.RedisConfig, logger *zap.Logger) *SessionCache {
	cli := NewClusterClient(cfg)
	c := &SessionCache{
		client: cli,
		logger: logger,
		memKV:  make(map[string]fallbackItem),
	}
	if err := Ping(context.Background(), cli); err != nil {
		c.MarkDegraded(err)
	}
	return c
}

func (c *SessionCache) summaryKey(userID string, sessionID string) string {
	return fmt.Sprintf("sess:%s:%s:summary", userID, sessionID)
}

func (c *SessionCache) streamKey(requestID string) string {
	return fmt.Sprintf("chat:stream:%s", requestID)
}

func (c *SessionCache) windowKey(userID string, sessionID string) string {
	return fmt.Sprintf("sess:%s:%s:window", userID, sessionID)
}

func (c *SessionCache) idempotencyKey(consumer string, messageID string) string {
	return fmt.Sprintf("idempotency:%s:%s", consumer, messageID)
}

// GetSummary 获取会话摘要，优先读 Redis，失败回退内存。
func (c *SessionCache) GetSummary(ctx context.Context, userID string, sessionID string) (string, bool, error) {
	key := c.summaryKey(userID, sessionID)
	if !c.degraded.Load() {
		val, err := c.client.Get(ctx, key).Result()
		if err == nil {
			return val, true, nil
		}
		if !errors.Is(err, redis.Nil) {
			c.MarkDegraded(err)
		}
	}
	return c.readFallback(key)
}

// SetSummary 写入摘要，退化模式下写内存。
func (c *SessionCache) SetSummary(ctx context.Context, userID string, sessionID string, summary string, ttl time.Duration) error {
	key := c.summaryKey(userID, sessionID)
	c.writeFallback(key, summary, ttl)

	if c.degraded.Load() {
		return nil
	}
	if err := c.client.Set(ctx, key, summary, ttl).Err(); err != nil {
		c.MarkDegraded(err)
		return err
	}
	return nil
}

// GetWindow returns the cached recent message window.
func (c *SessionCache) GetWindow(ctx context.Context, userID string, sessionID string) ([]model.Message, bool, error) {
	key := c.windowKey(userID, sessionID)
	if !c.degraded.Load() {
		items, err := c.client.LRange(ctx, key, 0, -1).Result()
		if err == nil {
			out := make([]model.Message, 0, len(items))
			for _, raw := range items {
				var item model.Message
				if json.Unmarshal([]byte(raw), &item) == nil {
					out = append(out, item)
				}
			}
			return out, len(out) > 0, nil
		}
		if !errors.Is(err, redis.Nil) {
			c.MarkDegraded(err)
		}
	}
	return c.readWindowFallback(key)
}

// SetWindow overwrites the cached recent message window.
func (c *SessionCache) SetWindow(ctx context.Context, userID string, sessionID string, messages []model.Message, ttl time.Duration) error {
	key := c.windowKey(userID, sessionID)
	c.writeWindowFallback(key, messages, ttl)
	if c.degraded.Load() {
		return nil
	}
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, key)
	for _, item := range messages {
		raw, _ := json.Marshal(item)
		pipe.RPush(ctx, key, raw)
	}
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		c.MarkDegraded(err)
		return err
	}
	return nil
}

// AppendStreamChunk 保存可重放的流式分片。
func (c *SessionCache) AppendStreamChunk(ctx context.Context, requestID string, chunk string, ttl time.Duration) error {
	key := c.streamKey(requestID)
	c.writeStreamFallback(key, chunk, ttl)
	if c.degraded.Load() {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"chunk": chunk})
	pipe := c.client.TxPipeline()
	pipe.RPush(ctx, key, payload)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		c.MarkDegraded(err)
		return err
	}
	return nil
}

// GetStreamChunks 读取分片，优先 Redis，失败回退内存。
func (c *SessionCache) GetStreamChunks(ctx context.Context, requestID string) ([]string, error) {
	key := c.streamKey(requestID)
	if !c.degraded.Load() {
		items, err := c.client.LRange(ctx, key, 0, -1).Result()
		if err == nil {
			out := make([]string, 0, len(items))
			for _, raw := range items {
				var item map[string]string
				if json.Unmarshal([]byte(raw), &item) == nil {
					out = append(out, item["chunk"])
				}
			}
			return out, nil
		}
		c.MarkDegraded(err)
	}
	return c.readStreamFallback(key), nil
}

// IsIdempotent 检查消息是否已经处理。
func (c *SessionCache) IsIdempotent(ctx context.Context, consumer string, messageID string) (bool, error) {
	key := c.idempotencyKey(consumer, messageID)
	if !c.degraded.Load() {
		n, err := c.client.Exists(ctx, key).Result()
		if err == nil {
			return n > 0, nil
		}
		c.MarkDegraded(err)
	}
	_, ok, err := c.readFallback(key)
	return ok, err
}

// MarkIdempotent 打幂等标记。
func (c *SessionCache) MarkIdempotent(ctx context.Context, consumer string, messageID string, ttl time.Duration) error {
	key := c.idempotencyKey(consumer, messageID)
	c.writeFallback(key, "1", ttl)
	if c.degraded.Load() {
		return nil
	}
	if err := c.client.Set(ctx, key, "1", ttl).Err(); err != nil {
		c.MarkDegraded(err)
		return err
	}
	return nil
}

// MarkDegraded 标记 Redis 退化状态并输出日志。
func (c *SessionCache) MarkDegraded(err error) {
	c.degraded.Store(true)
	if c.logger != nil {
		c.logger.Warn("redis degraded, switch to memory fallback", zap.Error(err))
	}
}

// IsDegraded 返回当前退化状态。
func (c *SessionCache) IsDegraded() bool {
	return c.degraded.Load()
}

func (c *SessionCache) writeFallback(key string, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memKV[key] = fallbackItem{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (c *SessionCache) readFallback(key string) (string, bool, error) {
	c.mu.RLock()
	item, ok := c.memKV[key]
	c.mu.RUnlock()
	if !ok {
		return "", false, nil
	}
	if time.Now().After(item.ExpiresAt) {
		c.mu.Lock()
		delete(c.memKV, key)
		c.mu.Unlock()
		return "", false, nil
	}
	return item.Value, true, nil
}

func (c *SessionCache) writeStreamFallback(key string, chunk string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.memKV[key]
	var parts []string
	if old.Value != "" {
		parts = append(parts, old.Value)
	}
	parts = append(parts, chunk)
	c.memKV[key] = fallbackItem{
		Value:     strings.Join(parts, "\n"),
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (c *SessionCache) readStreamFallback(key string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.memKV[key]
	if !ok || item.Value == "" || time.Now().After(item.ExpiresAt) {
		return nil
	}
	return strings.Split(item.Value, "\n")
}

func (c *SessionCache) writeWindowFallback(key string, messages []model.Message, ttl time.Duration) {
	raw, _ := json.Marshal(messages)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memKV[key] = fallbackItem{
		Value:     string(raw),
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (c *SessionCache) readWindowFallback(key string) ([]model.Message, bool, error) {
	raw, ok, err := c.readFallback(key)
	if err != nil || !ok || raw == "" {
		return nil, false, err
	}
	var items []model.Message
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, false, err
	}
	return items, true, nil
}
