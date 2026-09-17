# 包说明

## 包作用
- 定义接口请求响应结构，统一返回格式。
- 减少处理器重复拼装响应的工作量。

## 实现逻辑
1. 按场景拆分问答、会话、鉴权和附件数据结构。
2. 使用统一响应结构封装成功和失败结果。
3. 保证字段命名与接口处理逻辑一致。

## 关键接口
```go
type APIResponse struct
func OK(data interface{}) APIResponse
func Err(code int, message string) APIResponse
type QueryRequest struct
type SessionData struct
```

## 协作关系
- 被接口处理器直接引用。
