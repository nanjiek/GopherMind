# 包说明

## 包作用
- 承载核心业务编排逻辑，连接存储、缓存、模型与检索能力。
- 实现查询、流式返回、会话读取、鉴权和附件处理。

## 实现逻辑
1. 查询流程先落库用户消息，再构造提示并调用模型生成答案。
2. 流式流程在推送分片时同步写入缓存以支持重连恢复。
3. 会话流程优先读取摘要缓存，未命中则回源并重建摘要。
4. 鉴权流程处理注册登录刷新和登出，维护令牌状态。
5. 附件流程执行大小类型校验并安全落盘。

## 关键接口
```go
type QueryService struct
func (s *QueryService) Query(ctx context.Context, in model.QueryInput) (model.QueryOutput, error)
type StreamService struct
func (s *StreamService) Stream(ctx context.Context, in model.QueryInput, onToken func(string) error) (model.QueryOutput, error)
type AuthService struct
```

## 协作关系
- 依赖接口抽象隔离外部实现。
- 调用存储与缓存模块完成数据读写。
- 调用模型与检索模块完成生成增强。
