# 包说明

## 包作用
- 封装各模型提供方协议细节，输出统一生成接口。
- 提供重试与稳定性辅助能力。

## 实现逻辑
1. 分别实现多个模型提供方的普通与流式生成。
2. 通过重试策略应对瞬时失败。
3. 通过状态维护处理调用异常和恢复。
4. 提供文本分片能力支撑流式输出。

## 关键接口
```go
type OpenAIProvider struct
type OllamaProvider struct
type BGEProvider struct
func withRetry(ctx context.Context, attempts int, baseDelay time.Duration, fn func(context.Context) error) error
```

## 协作关系
- 被模型工厂聚合并对上游暴露。
- 依赖配置模块获取模型参数。
