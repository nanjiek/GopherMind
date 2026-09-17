# 包说明

## 包作用
- 提供请求级横切处理，包括追踪、恢复、鉴权和指标采集。

## 实现逻辑
1. 为每个请求注入唯一标识便于链路排障。
2. 捕获异常并转换为稳定错误响应。
3. 解析令牌并执行角色授权。
4. 统计请求耗时和状态码指标。

## 关键接口
```go
func RequestID() gin.HandlerFunc
func Recovery(logger *zap.Logger) gin.HandlerFunc
func Auth(cfg config.AuthConfig, tokenM *token.Manager, logger *zap.Logger) gin.HandlerFunc
func HTTPMetrics() gin.HandlerFunc
```

## 协作关系
- 由路由模块统一注册。
- 依赖鉴权和观测模块提供底层能力。
