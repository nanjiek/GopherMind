# ADR-0003：RAG 主路径与 DeepSeek 适配

状态：接受  
日期：2026-09-17

## 现状

- `cmd/server` 和 `cmd/mcp-server` 实际装配 Python RAG HTTP Client。
- Python 服务使用 Qdrant、LangChain text splitter、Dense retrieval、可选混合/BM25 和 BGE rerank。
- Go `internal/rag/langchain` 和名为 Pinecone 的 client 未接入启动路径；该 client 当前只使用进程内 map，没有远程 Pinecone 请求。
- Go 与 Python 的查询重写目前都是标准化空白，尚未实现医学语义重写。
- Python Client 在 embed/retrieve 失败时返回伪向量或空结果，缺少可区分的降级原因。

## 决策

- P1～P5 以 Python + Qdrant 为唯一在线 RAG 主路径，先消除双实现歧义。
- Go 原生 RAG 保留为实验代码，不能标注为生产 Pinecone；在 P6 版本化知识治理选型完成前不接入主链路。
- RAG 内部协议补 `document_id`、tenant/user scope、knowledge version、trace ID、rewrite result 和 degradation reason。
- 检索失败、零命中和 rerank 降级必须可区分；禁止使用伪 embedding 冒充成功结果。
- 查询重写先用确定性保护规则和评测集验证否定、时间、剂量、年龄、孕哺和多轮指代，再决定是否调用模型。
- DeepSeek 使用独立 Provider 适配现有 ModelProvider，不把 Kimi/OpenAI 配置改名冒充。型号、价格、上下文、视觉和并发均来自配置，并记录核验日期。

2026-09-17 官方页面确认 `deepseek-flash` 与 `deepseek-v4-pro`、OpenAI 格式 base URL、1M context、JSON/Tool Calls，以及 Flash 视觉支持；价格可能变化，运行时配置和成本报表不得依赖文档常量。[DeepSeek Models & Pricing](https://api-docs.deepseek.com/quick_start/pricing/)

## 验收

- 从入口可以唯一追踪到 Python RAG 路径，不存在按环境隐式切换实现。
- document/user 范围贯穿摄取和检索，跨范围结果为零并留下审计字段。
- 离线集分别报告 rewrite、Dense/BM25 候选、融合、rerank 和最终 evidence 指标。
- 服务不可用时返回显式降级状态；医疗高风险回答不能把“检索失败”当作“没有相关风险”。
- DeepSeek Provider 使用测试账户完成 JSON、Tool Call、usage、超时和错误映射验证后才能启用。
