# 包说明

## 包作用
- 负责请求参数解析、服务调用和响应封装。
- 统一处理普通响应与流式响应输出。

## 实现逻辑
1. 查询处理器完成问答请求校验与调用。
2. 会话处理器负责读取会话详情和消息列表。
3. 流式处理器根据协商选择不同流式通道。
4. 鉴权和附件处理器负责账号与文件相关流程。

## 关键接口
```go
type QueryHandler struct
func (h *QueryHandler) Handle(c *gin.Context)
type StreamHandler struct
func (h *StreamHandler) Handle(c *gin.Context)
type AuthHandler struct
```

## 协作关系
- 依赖业务服务层提供核心能力。
- 依赖接口契约模块定义响应结构。
