# 包说明

## 包作用
- 集中定义和上报运行指标。
- 覆盖请求性能、鉴权、消息队列与问答链路核心指标。

## 实现逻辑
1. 启动时注册全部指标。
2. 在请求入口和关键节点上报耗时与计数。
3. 通过标签区分成功状态、模型类型与是否启用检索增强。

## 关键接口
```go
func RegisterAll()
func ObserveHTTPRequest(method string, path string, status string, d time.Duration)
func IncQueryRequest(success bool, modelType string, useRAG bool)
func ObserveStreamFirstToken(d time.Duration)
```

## 协作关系
- 被接口中间件与业务服务共同调用。
- 与追踪模块一起构成观测体系。
