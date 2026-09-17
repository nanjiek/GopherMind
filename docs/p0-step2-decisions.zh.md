# P0 第二步：架构决策结果

日期：2026-09-17。状态：完成。

本步骤冻结三个会影响 P1 实现的边界：

1. PostgreSQL 空库切换：旧 MySQL 数据全部废弃，不迁移、不双写、不回填；新版本只使用 PostgreSQL。详见 [ADR-0001](adr/0001-postgresql-clean-cutover.zh.md)。
2. Go/LangGraph 边界：Go 掌握权限、副作用和发布，Python LangGraph 保存 Workflow checkpoint 并返回结构化 Action。详见 [ADR-0002](adr/0002-go-langgraph-boundary.zh.md)。
3. RAG 主路径：当前固定为 Python + Qdrant；Go 的“Pinecone client”实际为内存实现，不计为已接入能力。详见 [ADR-0003](adr/0003-rag-primary-path.zh.md)。

## 证据和验证

- 静态追踪两个入口，均装配 `ragclient.NewPythonClient`。
- 检查 Go vector client，未发现 Pinecone HTTP/SDK 调用；混合搜索只遍历进程内 map。
- 检查 Python RAG，确认 Qdrant、BM25/RRF 和 BGE 路径存在；实际运行仍需依赖环境验证。
- 对照 LangGraph 官方 Persistence/Memory 文档，确认 checkpoint 与跨 thread store 的职责分离，以及生产数据库 checkpointer 要求。
- 对照 2026-09-17 DeepSeek 官方 Models & Pricing 页面记录当前模型能力；价格与模型参数仍必须配置化。
- 本地 SQLite checkpoint spike 用固定依赖验证关闭并重新打开 checkpointer 后能读取同一 thread 状态。该结果不替代 PostgreSQL 故障恢复测试。

## 对 P1 的约束

- P1 先替换现有关系数据库适配和 compose，建立 PostgreSQL 空库 schema；不编写旧数据导入脚本。
- Event Log 与现有会话写入的过渡必须发生在 PostgreSQL 内部，禁止为了兼容旧数据连接 MySQL。
- LangGraph 暂不进入在线路径；P1 只冻结 run/thread/revision 协议并准备 checkpoint schema 隔离。
- P1 继续调用 Python RAG，不切到 Go 原生 RAG，不把内存 client 描述为 Pinecone。

## 回滚

本步骤以 ADR 和本地 spike 为主，无数据库变化。撤销提交即可恢复文档；`.venv` 与 `tmp` 被忽略，不进入 Git。
