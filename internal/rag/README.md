# 包说明

## 包作用
- 定义检索增强相关请求响应结构，作为跨进程契约层。

## 实现逻辑
1. 抽象向量化、召回、重排和知识图谱占位的数据模型。
2. 保持字段稳定以保障调用兼容性。

## 关键接口
```go
type EmbedRequest struct
type RetrieveRequest struct
type RerankRequest struct
type KGRequest struct
```

## 协作关系
- 被检索客户端模块用于序列化和反序列化。
