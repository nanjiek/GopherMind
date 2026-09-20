# P5 Step 4：响应提交屏障

`ResponseCommitBarrier` 将 P4/P5 Team 返回的未提交数据和可产生业务效果的已提交响应分离。它要求：可信 run 与 tenant/user scope、完整的 `{"answer":"..."}` JSON 输出、独立 `ResponseReviewVerifier` 的审核证明，以及提交前**紧邻** `ResponseCommitAuthorizer.Authorize` 的 Capability 再授权。只有这些条件都成立，才调用 `ReviewedResponseCommitter`。

`requires_human` 是提交屏障的终止状态，不能写成普通 assistant response。审核失败、授权失效、无效 schema 或缺少可信范围时，Committer 不会被调用。该节点不相信调用方传入的“已审核”布尔值；下一节点将用 PostgreSQL 中的固定 Safety Task 成功结果实现 verifier，并持久化 committed response / generation replay。Action/Event-Outbox、HTTP/SSE 公开、队列发布和旧 Query 路径迁移均不在本节点内。

没有 MySQL 迁移、双写、CDC、回填或新的外部连接。Committer 目前仅是测试/后续持久化适配器接口；每一个真正外部效果仍须在本屏障调用时即时 Capability 授权。
