# V3 P0.1：首次工作区基线核验

日期：2026-09-17。节点状态：基线记录完成，P0 整体验收尚未通过。

## 版本边界

- 远程 `develop`：`3cd4b4a8f7a0a693a3e1241fa0acf5a2c872f334`。
- 规划提交：`bbf6a3b`，PR #1，`codex/v3-upgrade-plan` → `develop`。
- 本次检查对象：上述规划提交加现有未提交工作区；测试结果不代表远程 develop 可复现状态。
- 业务目录存在大量先前修改；frontend、Go 原生 RAG、MemoryService 等尚未纳入 Git。未读取或提交本地 `.env`，未打包 tmp、缓存或附件。
- 本节点仅新增报告，不修复依赖、不修改业务实现、不声明端到端通过。

## 执行结果

环境：Windows amd64；Go 1.27.1；Node 20.17.0；Python 3.9.13。go.mod 声明 Go 1.23.0，因此后续 CI 还需要使用项目支持版本核验。

| 检查 | 命令/方式 | 实际结果 |
| --- | --- | --- |
| Go 全量测试 | 项目根目录 `go test ./...` | 退出码 1；多个包 setup failed，缺少 `github.com/tmc/langchaingo/textsplitter` |
| Redis 包测试 | 上述全量测试输出 | `internal/repo/redis` 通过，1.114s |
| 其余 Go 包 | 上述全量测试输出 | 部分显示 no test files；不能作为完整服务通过的依据 |
| 前端构建 | frontend 下 `npm run build` | 退出码 1；vite 命令不存在；没有 node_modules 或 lockfile，尚未安装依赖 |
| Python 语法 | 对 `python/rag_service/app/*.py` 用 `ast.parse` 检查 | 5 个文件通过；不代表导入依赖、模型加载或服务运行通过 |
| 远程基线 | `git ls-remote origin refs/heads/develop` | 与上述 develop SHA 一致 |

Python 语法检查可复现命令（不产生 pycache）：

```powershell
python -c "import ast,pathlib; files=list(pathlib.Path('python/rag_service/app').glob('*.py')); [ast.parse(p.read_text(encoding='utf-8-sig'), filename=str(p)) for p in files]; print('AST parse passed:', len(files), 'files')"
```

## 静态发现

1. `internal/rag/langchain/engine.go` 导入 textsplitter，但 go.mod 未声明对应依赖；这是本次实际测试的首个阻断点。
2. `cmd/server/main.go` 调用 ModelFactory 时少传 Qwen provider；QueryService/StreamService 的调用也未提供当前签名新增的 memories、traces、evals。此项来自静态比对，当前全量测试尚未越过依赖阻断来验证这些编译错误。
3. `cmd/server/main.go` 实际装配 `ragclient.NewPythonClient`，不是 Go 原生 RAG Engine。目录存在不等于启动链路已经启用。
4. e2e/perf 文件使用 smokeRepo/benchRepo 等替身；应区分 API 契约测试和真实 MySQL/Redis/模型联调，不能用替身性能代表线上延迟。
5. 项目根目录没有 .gitignore。后续提交业务基线前应添加精确忽略规则，保护本地配置并排除构建缓存，同时单独处理已跟踪 pycache。

## 后续 P0 节点

- P0.2：整理可提交业务基线；补忽略规则，锁定依赖并修复构造函数装配，在保留既有功能的前提下运行 Go 测试与前端构建。修复后独立 PR，记录真实通过项与剩余失败。
- P0.3：完成数据库迁移、LangGraph/Go 边界、RAG 主路径三份 ADR；外部 SDK/模型能力在选型时核验。
- P0.4：建立最小评测数据和真实依赖冒烟清单，输出 P1 事件接口、表结构、迁移及回滚设计。

进入 P1 前必须完成 P0 剩余项；本报告不构成 P0 完成声明。

## 后续提交约定

按用户要求，每个独立节点完成后必须提交 commit、push 远程分支并创建 PR（GitHub 对应 MR）。默认目标为 develop；前一个节点尚未合并且后一个依赖它时，使用堆叠 PR，以前一个节点分支为 base，并在描述中标明合并顺序。前置 PR 合并后再调整后续 base。未经请求不自动合并 PR。

每个 PR 描述需给出节点范围、验证结果、现有阻断、数据变化与回滚方式。未通过的阶段不能标记完成，远程分支必须包含可审查的实际交付物。本节点回滚只需撤销报告提交，无数据库变更。
