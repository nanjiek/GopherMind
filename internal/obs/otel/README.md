# 包说明

## 包作用
- 负责链路追踪初始化与关闭钩子管理。

## 实现逻辑
1. 构建追踪提供者并返回释放函数。
2. 支持按环境切换追踪输出目标。

## 关键接口
```go
func InitTracerProvider(serviceName string, out io.Writer) (*sdktrace.TracerProvider, func(context.Context) error, error)
```

## 协作关系
- 由启动入口初始化，并与日志和指标能力联动。
