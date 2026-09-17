# 包说明

## 包作用
- 定义核心领域数据结构，统一业务层输入输出语义。
- 覆盖会话、消息、引用、用量与鉴权相关实体。

## 实现逻辑
1. 以结构体表达问答主链路中的状态与数据。
2. 保持跨层字段一致，减少重复转换。
3. 为检索增强和模型调用提供通用数据对象。

## 关键接口
```go
type Session struct
type Message struct
type QueryInput struct
type QueryOutput struct
type AuthUser struct
```

## 协作关系
- 被业务服务层直接使用。
- 被存储层用于映射持久化对象。
- 被模型与检索客户端用于数据封装。
