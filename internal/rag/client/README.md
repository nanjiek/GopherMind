# 包说明

## 包作用
- 提供检索增强远程调用客户端，屏蔽下游服务细节。
- 向业务层暴露统一检索增强能力接口。

## 实现逻辑
1. 初始化时读取地址和超时等配置。
2. 通过统一请求函数处理编解码与错误转换。
3. 分别实现向量化、召回、重排和知识图谱占位调用。

## 关键接口
```go
type PythonClient struct
func (c *PythonClient) Embed(ctx context.Context, text string) ([]float64, error)
func (c *PythonClient) Retrieve(ctx context.Context, userID string, query string, topK int) ([]model.RAGDocument, error)
func (c *PythonClient) Rerank(ctx context.Context, query string, docs []model.RAGDocument, topN int) ([]model.RAGDocument, error)
```

## 协作关系
- 被业务服务层调用构造增强上下文。
- 依赖检索契约模块定义输入输出结构。
