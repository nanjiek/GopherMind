# P0 第一步：构建与测试基线

2026-09-17；基于升级前快照 `27adfc5`。本步骤完成离线可复现基线，未宣称生产联调完成。

## 改动

- 固定 LangChainGo v0.1.13，保持 Go 1.23.0 声明；其传递依赖将 MySQL driver 从 1.7.0 升至 1.7.1。v0.1.14 要求更高 Go 版本，未采用。[上游源码](https://github.com/tmc/langchaingo/tree/v0.1.13)
- HTTP/MCP 入口补 Qwen provider 和新增构造参数；修正 handler 装配和缺失 context import。
- Memory、Trace、Judge、DocumentService 在当前入口仍未启用，显式传 nil，保持现有启动范围；相关配置存在不代表功能已接通。
- 更新测试替身的窗口和 document ID 接口，保留原断言；增加历史窗口重建、流式成功/断线回归。
- 前端生成 package-lock.json；增加 Go/前端 PR CI。
- 将已跟踪 pycache 从 Git 索引移除，保留本地文件，由已有 .gitignore 排除。

## 验证

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过，含服务、HTTP、队列和 Redis 降级测试 |
| `go build ./cmd/...` | 两个入口通过 |
| `go vet ./...` | 通过 |
| `GOTOOLCHAIN=go1.23.12 go test ./...` | 通过（PowerShell 使用环境变量赋值） |
| frontend：`npm ci --no-audit --no-fund`、`npm run build` | 通过，Vite 5.4.21，10 modules |
| `go test ./test/perf -run '^$' -bench BenchmarkQueryEndpoint -benchtime=100x -benchmem` | 100 次，9937 ns/op、16205 B/op、80 allocs/op |

性能数字来自 Windows / i7-13650HX 的替身 HTTP benchmark，只用于同机回归；不是模型延迟、线上 P95、Token 或医疗准确率。

Python 当前解释器没有 fastapi/pydantic 等服务依赖，5 文件 AST 检查已在 P0.1 通过；真实 RAG 服务未启动。Docker CLI 存在，但引擎 pipe 不存在，因此未执行真实数据库/队列/模型联调。第二、三步会提供独立验证和明确的后续接入门禁。

## 回滚

无数据库变化。撤销本节点提交恢复旧源码及依赖；前端依赖以相应 lockfile 重新安装。CI 是新增工作流，本地通过不等于远程 CI 已通过。
