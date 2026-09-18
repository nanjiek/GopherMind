# MCP 传输层说明

本目录是 GopherMind 的 MCP 适配层，当前实现为 `stdio` 传输。

## 1. 当前能力
- 传输：`stdio`
- 工具：`gm.query`、`gm.get_session`、`gm.stream_query`
- 鉴权策略（stdio）：优先使用输入 `user_id`，否则回退 `MCP_DEFAULT_USER_ID`

## 2. 启动前提
`cmd/mcp-server` 会复用后端服务装配，因此依赖以下组件可用：
- Postgres
- Redis
- RAG Python 服务（用于 `use_rag=true` 场景）

## 3. 启动命令（项目根目录）

### Linux/macOS
```bash
MCP_ENABLED=true \
MCP_TRANSPORT=stdio \
MCP_DEFAULT_USER_ID=mcp-user \
go run ./cmd/mcp-server
```

### Windows CMD
```cmd
set MCP_ENABLED=true
set MCP_TRANSPORT=stdio
set MCP_DEFAULT_USER_ID=mcp-user
go run ./cmd/mcp-server
```

## 4. 配置项
- `MCP_ENABLED`：是否启用 MCP 进程（`cmd/mcp-server` 启动时必须为 `true`）
- `MCP_TRANSPORT`：当前仅支持 `stdio`
- `MCP_DEFAULT_USER_ID`：stdio 下默认用户 ID（当工具调用未传 `user_id` 时使用）

## 5. 工具契约

### 5.1 `gm.query`
输入：
- `user_id`（可选）
- `session_id`（可选）
- `question`（必填）
- `model_type`（可选）
- `use_rag`（可选）

输出：
- `request_id`
- `session_id`
- `answer`
- `citations`
- `usage`

### 5.2 `gm.get_session`
输入：
- `user_id`（可选）
- `session_id`（必填）

输出：
- `session_id`
- `title`
- `messages`

### 5.3 `gm.stream_query`
输入：
- `user_id`（可选）
- `session_id`（可选）
- `question`（必填）
- `model_type`（可选）
- `use_rag`（可选）

输出：
- `request_id`
- `session_id`
- `answer`
- `citations`
- `usage`
- `tokens`（首版返回聚合后的 token 列表）

## 6. 快速检查
启动后如果进程没有报错并保持运行，表示 MCP stdio server 已就绪。
