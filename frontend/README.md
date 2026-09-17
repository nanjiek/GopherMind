# Frontend (Vue)

## 运行

```bash
npm install
npm run dev
```

默认地址 `http://localhost:5173`，并将以下接口代理到后端 `http://localhost:9090`：
- `/auth/*`
- `/query`
- `/session/:id`
- `/attachments`
- `/stream/*`

## 已对齐能力

- 登录 / 注册
- 聊天问答（`/query`）
- 会话读取（`/session/:id`）
- 文件上传（`/attachments`）
- 输入时工具提示（`gm.query` / `gm.get_session` / `gm.stream_query`）

## Docker 启动（不重启其他服务）

```bash
docker compose up -d frontend
docker compose logs -f frontend --tail 80
```

只会新增/启动 `frontend` 容器，不会重启当前运行中的导入任务容器。
