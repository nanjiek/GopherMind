# 包说明

## 包作用
- 实现异步消息生产消费、重试与死信处理。
- 结合幂等控制避免重复消费副作用。

## 实现逻辑
1. 生产者发布任务消息、结果消息、重试消息和死信消息。
2. 消费者解析投递并执行业务处理。
3. 失败按可重试与不可重试分流到不同通道。
4. 通过幂等标记和收件箱状态保证重复消息可控。

## 关键接口
```go
type AMQPProducer struct
func (p *AMQPProducer) PublishTask(ctx context.Context, message events.TaskMessage) error
type Consumer struct
func (c *Consumer) Start(ctx context.Context) error
```

## 协作关系
- 依赖消息契约模块定义载荷结构。
- 依赖缓存和数据库模块记录处理状态。
